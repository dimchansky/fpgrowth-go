package fpgrowth

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// readPass collects one pass of src (items cloned).
func readPass[T comparable](src Source[T]) ([]Transaction[T], error) {
	var out []Transaction[T]
	for tx, err := range src.Transactions() {
		if err != nil {
			return out, err
		}
		tx.Items = slices.Clone(tx.Items)
		out = append(out, tx)
	}
	return out, nil
}

func txString[T comparable](txs []Transaction[T]) string {
	var b strings.Builder
	for _, tx := range txs {
		fmt.Fprintf(&b, "%q:%d;", any(tx.Items), tx.Weight)
	}
	return b.String()
}

// TestMemorySources checks FromSlice, FromItems and Generate, including
// early stop and buffer reuse.
func TestMemorySources(t *testing.T) {
	t.Parallel()
	slice := []Transaction[int]{{Items: []int{1, 2}, Weight: 2}, {Items: nil, Weight: 0}, {Items: []int{3}, Weight: 1}}
	got, err := readPass(FromSlice(slice))
	if err != nil || txString(got) != txString(slice) {
		t.Errorf("FromSlice: %v %v", txString(got), err)
	}
	got, err = readPass(FromItems([][]int{{1, 2}, {}, {3}}))
	if err != nil || txString(got) != `['\x01' '\x02']:1;[]:1;['\x03']:1;` {
		t.Errorf("FromItems: %v %v", txString(got), err)
	}
	var caps []int
	gen := Generate(4, func(i int, buf []int) Transaction[int] {
		caps = append(caps, cap(buf))
		if len(buf) != 0 {
			t.Errorf("buf not empty: %v", buf)
		}
		for j := range i + 1 {
			buf = append(buf, j)
		}
		return Transaction[int]{Items: buf, Weight: int64(i)}
	})
	got, err = readPass(gen)
	if err != nil || len(got) != 4 || got[3].Weight != 3 || !slices.Equal(got[3].Items, []int{0, 1, 2, 3}) {
		t.Errorf("Generate: %v %v", got, err)
	}
	if caps[0] != 0 || caps[2] == 0 {
		t.Errorf("buffer not reused: capacities %v", caps)
	}
	if got, _ := readPass(Generate(-3, func(int, []int) Transaction[int] { panic("called") })); len(got) != 0 {
		t.Error("negative n")
	}
	for _, src := range []Source[int]{FromSlice(slice), FromItems([][]int{{1}, {2}}), gen} {
		n := 0
		for range src.Transactions() {
			n++
			break
		}
		if n != 1 {
			t.Error("early stop")
		}
	}
}

// TestTableFile reads the testdata files and mines them.
func TestTableFile(t *testing.T) {
	t.Parallel()
	got, err := readPass(TableFile("testdata/basket.txt", DefaultTableFormat()))
	if err != nil {
		t.Fatal(err)
	}
	want := `["bread" "milk"]:1;["bread" "diapers" "beer" "eggs"]:1;["milk" "diapers" "beer" "cola"]:1;` +
		`["bread" "milk" "diapers" "beer"]:1;["bread" "milk" "diapers" "cola"]:1;`
	if txString(got) != want {
		t.Errorf("got %s", txString(got))
	}
	wf := DefaultTableFormat()
	wf.Weighted = true
	got, err = readPass(TableFile("testdata/basket-weighted.txt", wf))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got[0].Weight != 3 || got[4].Weight != 2 || len(got[4].Items) != 4 {
		t.Errorf("weighted: %s", txString(got))
	}
	sets, _, err := CollectItemsets(context.Background(), TableFile("testdata/basket.txt", DefaultTableFormat()),
		Options[string]{MinSupport: Fraction(0.6), Target: TargetClosed})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sets) != "[{[beer diapers] 3 NaN} {[bread milk] 3 NaN} {[bread diapers] 3 NaN} {[bread] 4 NaN} {[milk diapers] 3 NaN} {[milk] 4 NaN} {[diapers] 4 NaN}]" {
		t.Errorf("sets %v", sets)
	}
}

// TestTableFileErrors checks the error mapping of the table reader.
func TestTableFileErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, data string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	wf := DefaultTableFormat()
	wf.Weighted = true
	_, err := readPass(TableFile(filepath.Join(dir, "missing"), DefaultTableFormat()))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
	_, err = readPass(TableFile(write("neg", "a b 1\nc -2\n"), wf))
	if !errors.Is(err, ErrNegativeWeight) || !strings.Contains(err.Error(), ":2(2): ") {
		t.Errorf("negative weight: %v", err)
	}
	_, err = readPass(TableFile(write("bad", "a b x\n"), wf))
	if !errors.Is(err, ErrSyntax) {
		t.Errorf("bad weight: %v", err)
	}
	_, err = MineItemsets(context.Background(), TableFile(write("bad2", "a\nb x\n"), wf), Options[string]{MinSupport: Count(1)}, nil)
	if !errors.Is(err, ErrSyntax) || !strings.Contains(err.Error(), "pass 1") {
		t.Errorf("mining: %v", err)
	}
}

// TestTableFormatValidation checks that a non-zero TableFormat without
// record separators is rejected naming the field (instead of reading the
// whole input as one transaction), by TableFile, TableReader and
// SpoolTable, while the zero format still means the default.
func TestTableFormatValidation(t *testing.T) {
	t.Parallel()
	const data = "a b 2\na c 3\n"
	open := func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(data)), nil }
	isFieldErr := func(err error) bool {
		return errors.Is(err, ErrInvalidOptions) && strings.Contains(err.Error(), "TableFormat.RecordSeparators")
	}
	for _, f := range []TableFormat{{FieldSeparators: ";"}, {Weighted: true}, {RecordSeparators: "", Comments: "#"}} {
		if _, err := readPass(TableReader(open, "memory", f)); !isFieldErr(err) {
			t.Errorf("%+v: reader: %v", f, err)
		}
		if _, err := readPass(TableFile("testdata/basket.txt", f)); !isFieldErr(err) {
			t.Errorf("%+v: file: %v", f, err)
		}
		if _, err := MineItemsets(context.Background(), TableReader(open, "memory", f), Options[string]{MinSupport: Count(1)}, nil); !isFieldErr(err) {
			t.Errorf("%+v: mining: %v", f, err)
		}
		dir := t.TempDir()
		reads := 0
		r := readerFunc(func(p []byte) (int, error) { reads++; return 0, io.EOF })
		if s, c, err := SpoolTable(context.Background(), r, dir, f); !isFieldErr(err) || s != nil || c != nil {
			t.Errorf("%+v: spool: %v", f, err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 || reads != 0 {
			t.Errorf("%+v: spool read %d times, left %v", f, reads, entries)
		}
	}
	got, err := readPass(TableReader(open, "memory", TableFormat{}))
	if err != nil || len(got) != 2 {
		t.Errorf("zero format: %s %v", txString(got), err)
	}
}

// TestTableReader checks reader-backed table sources: an fs.FS, gzip,
// the zero format, and the open errors.
func TestTableReader(t *testing.T) {
	t.Parallel()
	data := "# comment\na b\nb c\n\na b c\n"
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{"db.txt": {Data: []byte(data)}, "db.txt.gz": {Data: gz.Bytes()}}
	want := `["a" "b"]:1;["b" "c"]:1;[]:1;["a" "b" "c"]:1;`
	sources := map[string]Source[string]{
		"fs": TableReader(func() (io.ReadCloser, error) { return fsys.Open("db.txt") }, "db.txt", DefaultTableFormat()),
		"gzip": TableReader(func() (io.ReadCloser, error) {
			f, err := fsys.Open("db.txt.gz")
			if err != nil {
				return nil, err
			}
			zr, err := gzip.NewReader(f)
			if err != nil {
				_ = f.Close()
				return nil, err
			}
			return readCloser{zr, f}, nil
		}, "db.txt.gz", DefaultTableFormat()),
		"zero format": TableReader(func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(data)), nil
		}, "memory", TableFormat{}),
	}
	for name, src := range sources {
		got, err := readPass(src)
		if err != nil || txString(got) != want {
			t.Errorf("%s: %s %v", name, txString(got), err)
		}
		sets, _, err := CollectItemsets(context.Background(), src, Options[string]{MinSupport: Count(2)})
		if err != nil || len(sets) != 5 {
			t.Errorf("%s: mining %v %v", name, sets, err)
		}
	}
	errGone := errors.New("gone")
	_, err := readPass(TableReader(func() (io.ReadCloser, error) { return nil, errGone }, "remote", TableFormat{}))
	if !errors.Is(err, errGone) || err.Error() != "cannot open file remote: gone" {
		t.Errorf("open error: %v", err)
	}
	_, err = readPass(TableReader(func() (io.ReadCloser, error) { return nil, nil }, "remote", TableFormat{}))
	if err == nil {
		t.Error("nil reader without error accepted")
	}
	_, err = MineItemsets(context.Background(), TableReader(nil, "x", TableFormat{}), Options[string]{MinSupport: Count(1)}, nil)
	if !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("nil open: %v", err)
	}
	// a field cleared on purpose is taken literally: no comments
	noComments := DefaultTableFormat()
	noComments.Comments = ""
	got, err := readPass(TableReader(func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("#a b\n")), nil
	}, "memory", noComments))
	if err != nil || txString(got) != `["#a" "b"]:1;` {
		t.Errorf("no comments: %s %v", txString(got), err)
	}
}

// readCloser closes a decompressing reader and its file.
type readCloser struct {
	io.ReadCloser
	file io.Closer
}

func (r readCloser) Close() error { return errors.Join(r.ReadCloser.Close(), r.file.Close()) }

// TestWeightedEmptyLine pins upstream's behavior: with weights, an empty
// line is a syntax error, without weights an empty transaction.
func TestWeightedEmptyLine(t *testing.T) {
	t.Parallel()
	open := func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("a 2\n\nb 1\n")), nil }
	wf := DefaultTableFormat()
	wf.Weighted = true
	if _, err := readPass(TableReader(open, "w", wf)); !errors.Is(err, ErrSyntax) ||
		!strings.Contains(err.Error(), "transaction weight expected") {
		t.Errorf("weighted: %v", err)
	}
	got, err := readPass(TableReader(open, "u", DefaultTableFormat()))
	if err != nil || len(got) != 3 || len(got[1].Items) != 0 {
		t.Errorf("unweighted: %s %v", txString(got), err)
	}
}

// TestOpenErrors checks that both file sources report a missing file the
// same way: naming the file and wrapping the *fs.PathError.
func TestOpenErrors(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing.txt")
	for name, src := range map[string]Source[string]{
		"table": TableFile(path, DefaultTableFormat()),
		"csv":   CSVFile(path, CSVFormat{}),
	} {
		_, err := readPass(src)
		pe, ok := errors.AsType[*fs.PathError](err)
		if !ok || !errors.Is(err, fs.ErrNotExist) || pe.Path != path ||
			!strings.HasPrefix(err.Error(), "cannot open file "+path+": ") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestReplayCheck checks that file sources detect a file that changes
// between the passes of a mining run (size or modification time), and
// that separate runs do not share the check.
func TestReplayCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "db.txt")
	write := func(data string, mtime time.Time) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	t0 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name  string
		data  string // written before the second pass
		mtime time.Time
		ok    bool
	}{
		{"unchanged", "a b\nb\n", t0, true},
		{"same size, new time", "a c\nb\n", t0.Add(time.Second), false},
		{"new size, same time", "a b\nb\nb\n", t0, false},
	}
	for _, tt := range tests {
		for _, kind := range []string{"table", "csv"} {
			write("a b\nb\n", t0)
			calls := 0
			open := func() (io.ReadCloser, error) {
				if calls++; calls == 2 {
					write(tt.data, tt.mtime)
				}
				return openFile(path)()
			}
			src := TableReader(open, path, DefaultTableFormat())
			if kind == "csv" {
				src = CSVReader(open, path, CSVFormat{Comma: ' '})
			}
			_, err := MineItemsets(context.Background(), src, Options[string]{MinSupport: Count(1)}, nil)
			if tt.ok != (err == nil) || !tt.ok && (!errors.Is(err, ErrNotReplayable) ||
				!strings.Contains(err.Error(), "changed between passes")) {
				t.Errorf("%s/%s: %v", tt.name, kind, err)
			}
			// a new run starts a new check
			if _, err := MineItemsets(context.Background(), src, Options[string]{MinSupport: Count(1)}, nil); err != nil {
				t.Errorf("%s/%s: second run: %v", tt.name, kind, err)
			}
		}
	}
}

// statReader is an input whose Stat fails if fail is set and otherwise
// reports size and a zero modification time.
type statReader struct {
	io.Reader
	fail bool
	size int64
}

func (r *statReader) Close() error { return nil }

func (r *statReader) Stat() (fs.FileInfo, error) {
	if r.fail {
		return nil, errors.New("stat not supported")
	}
	return fakeInfo{size: r.size}, nil
}

type fakeInfo struct{ size int64 }

func (fakeInfo) Name() string       { return "input" }
func (i fakeInfo) Size() int64      { return i.size }
func (fakeInfo) Mode() fs.FileMode  { return 0 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (fakeInfo) IsDir() bool        { return false }
func (fakeInfo) Sys() any           { return nil }

// TestReplayCheckStatFails checks that a failing Stat skips the metadata
// check instead of failing the run: at the first pass (nothing to compare
// with, even if a later Stat works and differs) and at a later pass;
// a working Stat on both passes still detects a change.
func TestReplayCheckStatFails(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		fail  [2]bool  // Stat fails at pass 1, 2
		size  [2]int64 // reported sizes
		valid bool
	}{
		{"first pass fails", [2]bool{true, false}, [2]int64{1, 2}, true},
		{"second pass fails", [2]bool{false, true}, [2]int64{1, 2}, true},
		{"both fail", [2]bool{true, true}, [2]int64{1, 2}, true},
		{"both work, size differs", [2]bool{false, false}, [2]int64{1, 2}, false},
	}
	for _, tt := range tests {
		for _, kind := range []string{"table", "csv"} {
			pass := 0
			open := func() (io.ReadCloser, error) {
				p := pass % 2
				pass++
				return &statReader{Reader: strings.NewReader("a b\nb\n"), fail: tt.fail[p], size: tt.size[p]}, nil
			}
			src := TableReader(open, "input", DefaultTableFormat())
			if kind == "csv" {
				src = CSVReader(open, "input", CSVFormat{Comma: ' '})
			}
			_, err := MineItemsets(context.Background(), src, Options[string]{MinSupport: Count(1)}, nil)
			if tt.valid != (err == nil) || !tt.valid && !errors.Is(err, ErrNotReplayable) {
				t.Errorf("%s/%s: %v", tt.name, kind, err)
			}
		}
	}
}

// TestCSVFile checks the CSV semantics on edge cases.
func TestCSVFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name     string
		data     string
		format   CSVFormat
		expected string // txString, or "error: <substring>"
	}{
		{"plain", "a,b\nc\n", CSVFormat{}, `["a" "b"]:1;["c"]:1;`},
		{"quotes, commas, newlines", "\"a,b\",\"c\"\"d\",\"e\nf\"\n", CSVFormat{}, `["a,b" "c\"d" "e\nf"]:1;`},
		{"crlf", "a,b\r\nc\r\n", CSVFormat{}, `["a" "b"]:1;["c"]:1;`},
		{"no final newline", "a,b\nc", CSVFormat{}, `["a" "b"]:1;["c"]:1;`},
		{"header", "x,y\na,b\n", CSVFormat{HasHeader: true}, `["a" "b"]:1;`},
		{"header only", "x,y\n", CSVFormat{HasHeader: true}, ``},
		{"comment before header", "#c\nx,y\na\n", CSVFormat{HasHeader: true, Comment: '#'}, `["a"]:1;`},
		{"blank lines skipped", "a\n\n\nb\n", CSVFormat{}, `["a"]:1;["b"]:1;`},
		{"empty fields skipped", "a,,b,\n,\n", CSVFormat{}, `["a" "b"]:1;[]:1;`},
		{"empty fields kept", "a,,b\n", CSVFormat{KeepEmptyFields: true}, `["a" "" "b"]:1;`},
		{"quoted empty field", "a,\"\"\n", CSVFormat{}, `["a"]:1;`},
		{"spaces kept", " a , b \n", CSVFormat{}, `[" a " " b "]:1;`},
		{"trim space", " a ,  \" b\",  \n", CSVFormat{TrimSpace: true}, `["a" "b"]:1;`},
		{"trim space after quote", "\"a\" ,b\n", CSVFormat{TrimSpace: true}, "error: extraneous or missing"},
		{"bom", "\xef\xbb\xbfa,b\n", CSVFormat{}, `["a" "b"]:1;`},
		{"bom only once", "\xef\xbb\xbf\xef\xbb\xbfa\n", CSVFormat{}, `["\ufeffa"]:1;`},
		{"semicolon", "a;b,c\n", CSVFormat{Comma: ';'}, `["a" "b,c"]:1;`},
		{"tab", "a\tb\n", CSVFormat{Comma: '\t'}, `["a" "b"]:1;`},
		{"weight first", "3,a,b\n0,c\n", CSVFormat{WeightColumn: 1}, `["a" "b"]:3;["c"]:0;`},
		{"weight last", "a,b,3\n7\n", CSVFormat{WeightColumn: -1}, `["a" "b"]:3;[]:7;`},
		{"weight middle", "a,2,b\n", CSVFormat{WeightColumn: 2}, `["a" "b"]:2;`},
		{"weight spaces and sign", "a, +4 \n", CSVFormat{WeightColumn: -1}, `["a"]:4;`},
		{"weight header", "item,w\na,5\n", CSVFormat{HasHeader: true, WeightColumn: 2}, `["a"]:5;`},
		{"weight missing column", "a,1\nb\n", CSVFormat{WeightColumn: 2}, "error: :2: transaction weight expected"},
		{"weight empty", "a,\n", CSVFormat{WeightColumn: 2}, "error: :1:3: transaction weight expected"},
		{"weight invalid", "a,x1\n", CSVFormat{WeightColumn: 2}, `error: :1:3: invalid transaction weight "x1"`},
		{"weight not decimal", "a,0x10\n", CSVFormat{WeightColumn: 2}, `error: invalid transaction weight`},
		{"weight overflow", "a,99999999999999999999\n", CSVFormat{WeightColumn: 2}, `error: invalid transaction weight`},
		{"weight negative", "a,-1\n", CSVFormat{WeightColumn: 2}, "error: must not be negative"},
		{"bare quote", "a,b\"c\n", CSVFormat{}, "error: bare \""},
		{"lazy quotes", "a,b\"c\n", CSVFormat{LazyQuotes: true}, `["a" "b\"c"]:1;`},
		{"unterminated quote", "a,\"b\n", CSVFormat{}, "error: extraneous or missing"},
		{"empty file", "", CSVFormat{}, ``},
		{"duplicates kept by source", "a,a,b\n", CSVFormat{}, `["a" "a" "b"]:1;`},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(dir, fmt.Sprintf("%d.csv", i))
			if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := readPass(CSVFile(path, tt.format))
			if want, ok := strings.CutPrefix(tt.expected, "error: "); ok {
				if err == nil || !strings.Contains(err.Error(), want) || !strings.HasPrefix(err.Error(), path+":") {
					t.Fatalf("got %v, %s; expected error containing %q", err, txString(got), want)
				}
				if !errors.Is(err, ErrSyntax) && !errors.Is(err, ErrNegativeWeight) {
					t.Errorf("error %v wraps neither ErrSyntax nor ErrNegativeWeight", err)
				}
				if perr := (*csv.ParseError)(nil); strings.Contains(tt.name, "quote") && !errors.As(err, &perr) {
					t.Errorf("error %v does not wrap the *csv.ParseError", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s := txString(got); s != tt.expected {
				t.Errorf("got %s, expected %s", s, tt.expected)
			}
		})
	}
	if _, err := readPass(CSVFile(filepath.Join(dir, "missing.csv"), CSVFormat{})); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := readPass(CSVFile(filepath.Join(dir, "0.csv")+"x", CSVFormat{})); err == nil {
		t.Error("expected error")
	}
	for _, f := range []CSVFormat{{Comma: '"'}, {Comma: '\n'}, {Comment: '\r'}, {Comma: ';', Comment: ';'}, {Comma: -1}} {
		if _, err := readPass(CSVFile(filepath.Join(dir, "missing.csv"), f)); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("format %+v: %v", f, err)
		}
	}
}

// TestCSVFileMining mines the CSV testdata file and compares it with the
// weighted table file of the same baskets.
func TestCSVFileMining(t *testing.T) {
	t.Parallel()
	csvSets, _, err := CollectItemsets(context.Background(),
		CSVFile("testdata/basket.csv", CSVFormat{HasHeader: true, WeightColumn: 1}),
		Options[string]{MinSupport: Count(3)})
	if err != nil {
		t.Fatal(err)
	}
	wf := DefaultTableFormat()
	wf.Weighted = true
	tabSets, _, err := CollectItemsets(context.Background(), TableFile("testdata/basket-weighted.txt", wf),
		Options[string]{MinSupport: Count(3)})
	if err != nil {
		t.Fatal(err)
	}
	norm := func(sets []Itemset[string]) []string {
		var out []string
		for _, s := range sets {
			items := slices.Clone(s.Items)
			for i, it := range items {
				if it == "cola, diet" {
					items[i] = "cola"
				}
			}
			slices.Sort(items)
			out = append(out, fmt.Sprint(items, s.Support))
		}
		slices.Sort(out)
		return out
	}
	if a, b := norm(csvSets), norm(tabSets); !slices.Equal(a, b) {
		t.Errorf("csv %v\ntable %v", a, b)
	}
}

// openFiles counts the open file descriptors of the process (-1 if
// unsupported).
func openFiles(t *testing.T) int {
	t.Helper()
	for _, dir := range []string{"/proc/self/fd", "/dev/fd"} {
		if entries, err := os.ReadDir(dir); err == nil {
			return len(entries)
		}
	}
	return -1
}

// TestFileHandlesClosed checks that file sources close their files on
// success, on errors, on early stop and when mining fails. It is not
// parallel, so no other test opens files meanwhile.
func TestFileHandlesClosed(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	bad := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(good, []byte(strings.Repeat("a b c\nb c\n", 5000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("a 1\nb x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wf := DefaultTableFormat()
	wf.Weighted = true
	sources := map[string]Source[string]{
		"table":         TableFile(good, DefaultTableFormat()),
		"table error":   TableFile(bad, wf),
		"csv":           CSVFile(good, CSVFormat{Comma: ' '}),
		"csv error":     CSVFile(bad, CSVFormat{Comma: ' ', WeightColumn: -1}),
		"table missing": TableFile(filepath.Join(dir, "none"), wf),
	}
	runtime.GC()
	before := openFiles(t)
	if before < 0 {
		t.Skip("cannot count open files on this platform")
	}
	for name, src := range sources {
		_, _ = readPass(src)
		for range src.Transactions() {
			break
		}
		_, _ = MineItemsets(context.Background(), src, Options[string]{MinSupport: Count(1)}, nil)
		for range Itemsets(context.Background(), src, Options[string]{MinSupport: Count(1)}) {
			break
		}
		errStop := errors.New("stop")
		_, _ = MineRules(context.Background(), src, RuleOptions[string]{MinSupport: Count(1)},
			func(Rule[string]) error { return errStop })
		if after := openFiles(t); after != before {
			t.Errorf("%s: %d open files before, %d after", name, before, after)
		}
	}
}

// errReader fails after n bytes.
type errReader struct {
	n   int
	err error
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, r.err
	}
	k := min(len(p), r.n)
	for i := range k {
		p[i] = 'a'
	}
	r.n -= k
	return k, nil
}

// TestSpool checks the spooled sources and the removal of their files.
func TestSpool(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src, cleanup, err := SpoolTable(context.Background(), strings.NewReader("a b\nb c\na b c\n"), dir, DefaultTableFormat())
	if err != nil {
		t.Fatal(err)
	}
	sets, _, err := CollectItemsets(context.Background(), src, Options[string]{MinSupport: Count(2)})
	if err != nil || len(sets) != 5 {
		t.Fatalf("table: %v %v", sets, err)
	}
	csrc, ccleanup, err := SpoolCSV(context.Background(), strings.NewReader("w,i\n2,a\n1,\"b,c\"\n"), dir, CSVFormat{HasHeader: true, WeightColumn: 1})
	if err != nil {
		t.Fatal(err)
	}
	got, err := readPass(csrc)
	if err != nil || txString(got) != `["a"]:2;["b,c"]:1;` {
		t.Fatalf("csv: %s %v", txString(got), err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("%d spool files, expected 2", len(entries))
	}
	for _, c := range []func() error{cleanup, ccleanup, cleanup} {
		if err := c(); err != nil {
			t.Fatal(err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("spool files left: %v", entries)
	}
	if _, err := readPass(src); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("source after cleanup: %v", err)
	}
	errBroken := errors.New("broken pipe")
	if s, c, err := SpoolCSV(context.Background(), &errReader{n: 100000, err: errBroken}, dir, CSVFormat{}); !errors.Is(err, errBroken) || s != nil || c != nil {
		t.Errorf("failing reader: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("spool files left after error: %v", entries)
	}
	if _, _, err := SpoolTable(context.Background(), strings.NewReader("a"), filepath.Join(dir, "no", "such"), DefaultTableFormat()); err == nil {
		t.Error("expected error for a missing directory")
	}
	if _, _, err := SpoolTable(context.Background(), nil, dir, DefaultTableFormat()); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("nil reader: %v", err)
	}
	formatReads := 0
	invalidFormat := readerFunc(func(p []byte) (int, error) { formatReads++; return copy(p, "a\n"), io.EOF })
	if s, c, err := SpoolCSV(context.Background(), invalidFormat, dir, CSVFormat{Comma: '\n'}); !errors.Is(err, ErrInvalidOptions) || s != nil || c != nil {
		t.Errorf("invalid CSV format: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 || formatReads != 0 {
		t.Errorf("invalid CSV format: input read %d times, spool files left: %v", formatReads, entries)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s, c, err := SpoolCSV(ctx, strings.NewReader("a"), dir, CSVFormat{}); err != context.Canceled || s != nil || c != nil {
		t.Errorf("canceled before: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	reads := 0
	r := readerFunc(func(p []byte) (int, error) {
		if reads++; reads == 3 {
			cancel()
		}
		return copy(p, "a b\n"), nil // endless
	})
	if s, c, err := SpoolTable(ctx, r, dir, DefaultTableFormat()); err != context.Canceled || s != nil || c != nil || reads != 3 {
		t.Errorf("canceled while copying: %v after %d reads", err, reads)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("spool files left after cancellation: %v", entries)
	}
}

// readerFunc adapts a function to io.Reader.
type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// genReader produces a deterministic transaction file of about size
// bytes, comma-separated, lazily (nothing is held in memory). Item i0 is
// in 2/3 of the transactions, i1 in 1/2; the others are rare.
type genReader struct {
	r    *rand.Rand
	left int
	buf  []byte
}

func newGenReader(size int) *genReader {
	return &genReader{r: rand.New(rand.NewPCG(7, 8)), left: size}
}

func (g *genReader) Read(p []byte) (int, error) {
	for len(g.buf) < len(p) && g.left > 0 {
		n := len(g.buf)
		if g.r.IntN(3) > 0 {
			g.buf = append(g.buf, "i0,"...)
		}
		if g.r.IntN(2) > 0 {
			g.buf = append(g.buf, "i1,"...)
		}
		for range 1 + g.r.IntN(6) {
			k := 2 + g.r.IntN(500)
			g.buf = append(g.buf, 'i', 't', 'e', 'm', byte('0'+k/100), byte('0'+k/10%10), byte('0'+k%10), ',')
		}
		g.buf[len(g.buf)-1] = '\n'
		g.left -= len(g.buf) - n
	}
	if len(g.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, g.buf)
	g.buf = append(g.buf[:0], g.buf[n:]...)
	return n, nil
}

// heapProbe wraps a source and samples the heap every 50000 transactions.
type heapProbe[T comparable] struct {
	src  Source[T]
	peak uint64
}

func (h *heapProbe[T]) sample() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	h.peak = max(h.peak, ms.HeapAlloc)
}

func (h *heapProbe[T]) Transactions() iter.Seq2[Transaction[T], error] {
	return func(yield func(Transaction[T], error) bool) {
		n := 0
		for tx, err := range h.src.Transactions() {
			if n++; n%50000 == 0 {
				h.sample()
			}
			if !yield(tx, err) {
				return
			}
		}
	}
}

// TestLargeFileStreaming mines a generated file of about 50 MB (20 MB
// with -short or -race) through TableFile, CSVFile and SpoolCSV and checks that the
// heap stays below 8 MB while the passes read the file, while mining
// reports results and after mining.
func TestLargeFileStreaming(t *testing.T) {
	size := 50 << 20
	if testing.Short() || raceEnabled {
		size = 20 << 20
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriter(f)
	if _, err := io.Copy(w, newGenReader(size)); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	fileSize := uint64(st.Size())
	// The heap does not grow with the file: a fixed bound well below the
	// file size (the GC alone lets the heap reach 4 MB).
	const limit = 8 << 20
	var want string
	check := func(name string, src Source[string]) {
		t.Helper()
		runtime.GC()
		probe := &heapProbe[string]{src: src}
		probe.sample()
		base := probe.peak
		probe.peak = 0
		var sets []Itemset[string]
		var mining heapProbe[string] // heap while the trees exist
		sum, err := MineItemsets(context.Background(), probe, Options[string]{MinSupport: Fraction(0.3)},
			func(s Itemset[string]) error {
				mining.sample()
				sets = append(sets, s.Clone())
				return nil
			})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var after heapProbe[string] // heap after mining, before a GC
		after.sample()
		got := fmt.Sprint(sets, sum.Transactions)
		if want == "" {
			want = got
		} else if got != want {
			t.Errorf("%s: results differ: %s vs %s", name, got, want)
		}
		t.Logf("%s: file %.1f MB, %d transactions, heap peak while reading %.1f MB, while mining %.1f MB, "+
			"after %.1f MB (baseline %.1f MB), %d sets", name, float64(fileSize)/(1<<20), sum.Transactions,
			float64(probe.peak)/(1<<20), float64(mining.peak)/(1<<20), float64(after.peak)/(1<<20),
			float64(base)/(1<<20), len(sets))
		if len(sets) == 0 || mining.peak == 0 {
			t.Fatalf("%s: no item sets, no samples while mining", name)
		}
		for phase, peak := range map[string]uint64{"reading": probe.peak, "mining": mining.peak, "after mining": after.peak} {
			if peak > limit {
				t.Errorf("%s: heap %d bytes %s exceeds %d", name, peak, phase, limit)
			}
		}
	}
	check("table", TableFile(path, DefaultTableFormat()))
	check("csv", CSVFile(path, CSVFormat{}))
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	src, cleanup, err := SpoolCSV(context.Background(), newGenReader(size), dir, CSVFormat{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	}()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	grown := after.TotalAlloc - before.TotalAlloc
	t.Logf("spooling %d bytes allocated %d bytes (generator included)", fileSize, grown)
	if grown > limit {
		t.Errorf("spooling allocated %d bytes for a %d byte input", grown, fileSize)
	}
	check("spooled csv", src)
}

// FuzzCSVFile compares the CSV source with a direct encoding/csv parse of
// the same bytes.
func FuzzCSVFile(f *testing.F) {
	f.Add([]byte("a,b\nc\n"), false, int8(0), false, false)
	f.Add([]byte("\"a,b\",\"c\"\"d\"\n\n,\n"), true, int8(-1), true, false)
	f.Add([]byte("\xef\xbb\xbfx,1\ny,2\n"), true, int8(2), false, true)
	f.Add([]byte("a, -1\n"), false, int8(-1), true, false)
	f.Add([]byte("a,\"b\nc\",3\r\n"), false, int8(3), false, false)
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, data []byte, header bool, wcol int8, trim, keep bool) {
		path := filepath.Join(dir, "fuzz.csv")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		format := CSVFormat{HasHeader: header, WeightColumn: int(wcol), TrimSpace: trim, KeepEmptyFields: keep}
		got, gerr := readPass(CSVFile(path, format))
		// reference: encoding/csv record by record, then the rules of
		// CSVFormat; stop at the first error
		r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), utf8BOM)))
		r.FieldsPerRecord = -1
		r.TrimLeadingSpace = trim
		var want []Transaction[string]
		wantErr := false
		for first := true; ; first = false {
			rec, err := r.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				wantErr = true
				break
			}
			if first && header {
				continue
			}
			tx := Transaction[string]{Weight: 1}
			col := -1
			if wcol != 0 {
				col = int(wcol) - 1
				if wcol < 0 {
					col = len(rec) + int(wcol)
				}
				if col < 0 || col >= len(rec) {
					wantErr = true
					break
				}
				w, err := strconv.ParseInt(strings.TrimSpace(rec[col]), 10, 64)
				if err != nil || w < 0 {
					wantErr = true
					break
				}
				tx.Weight = w
			}
			for j, field := range rec {
				if j == col {
					continue
				}
				if trim {
					field = strings.TrimSpace(field)
				}
				if field == "" && !keep {
					continue
				}
				tx.Items = append(tx.Items, field)
			}
			want = append(want, tx)
		}
		if wantErr != (gerr != nil) {
			t.Fatalf("source error %v, reference error %v", gerr, wantErr)
		}
		if gerr != nil && !errors.Is(gerr, ErrSyntax) && !errors.Is(gerr, ErrNegativeWeight) {
			t.Fatalf("unexpected error %v", gerr)
		}
		if a, b := txString(got), txString(want); a != b {
			t.Fatalf("source %s\nreference %s", a, b)
		}
	})
}
