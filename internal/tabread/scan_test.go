package tabread

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// collect runs one pass of scan and returns its transactions.
func collect(scan tract.ScanFunc[string]) ([]record, error) {
	recs := []record{}
	err := scan(func(items []string, weight tract.Supp) bool {
		recs = append(recs, record{items: slices.Clone(items), weight: weight})
		return true
	})
	return recs, err
}

// trackingOpener opens src and counts opens and closes.
type trackingOpener struct {
	src      string
	err      error // error returned by Read after src
	closeErr error
	opens    atomic.Int32
	closes   atomic.Int32
}

type trackedReader struct {
	io.Reader
	o *trackingOpener
}

func (r trackedReader) Close() error {
	r.o.closes.Add(1)
	return r.o.closeErr
}

func (o *trackingOpener) open() (io.ReadCloser, error) {
	o.opens.Add(1)
	var r io.Reader = strings.NewReader(o.src)
	if o.err != nil {
		r = &errAfterReader{data: o.src, err: o.err}
	}
	return trackedReader{Reader: r, o: o}, nil
}

func TestScanFile(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.Weighted = true
	scan := ScanFile(filepath.Join("testdata", "ex", "test3.tab"), cfg)
	first, err := collect(scan)
	if err != nil {
		t.Fatal(err)
	}
	want := []record{
		{items: []string{"a", "b", "c"}, weight: 2}, {items: []string{"a", "d", "e"}, weight: 1},
		{items: []string{"b", "c", "d"}, weight: 1}, {items: []string{"a", "b", "c", "d"}, weight: 2},
		{items: []string{"b", "c"}, weight: 1}, {items: []string{"a", "b", "d"}, weight: 1},
		{items: []string{"d", "e"}, weight: 1}, {items: []string{"c", "d", "e"}, weight: 1},
	}
	if !sameRecords(first, want, false) {
		t.Errorf("first pass = %v, want %v", first, want)
	}
	// Every pass yields the same transactions in the same order.
	for range 3 {
		again, err := collect(scan)
		if err != nil || !sameRecords(again, first, false) {
			t.Fatalf("later pass = %v (%v), want %v", again, err, first)
		}
	}
}

func TestScanClosesInput(t *testing.T) {
	t.Parallel()
	errBoom := errors.New("boom")
	tests := []struct {
		name    string
		opener  *trackingOpener
		stopAt  int // stop after this many transactions (0: never)
		wantErr error
		wantN   int
	}{
		{name: "complete pass", opener: &trackingOpener{src: "a\nb\nc\n"}, wantN: 3},
		{name: "early stop", opener: &trackingOpener{src: "a\nb\nc\n"}, stopAt: 1, wantN: 1},
		{name: "syntax error", opener: &trackingOpener{src: "a\nb,,c\n"}, wantErr: ErrItemExpected, wantN: 1},
		{name: "read error", opener: &trackingOpener{src: "a\nb", err: errBoom}, wantErr: errBoom, wantN: 1},
		{name: "close error after a complete pass", opener: &trackingOpener{src: "a\n", closeErr: errBoom}, wantErr: ErrRead, wantN: 1},
		{name: "close error after an early stop is ignored", opener: &trackingOpener{src: "a\nb\n", closeErr: errBoom}, stopAt: 1, wantN: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			scan := Scan(tt.opener.open, "in", DefaultConfig())
			for pass := range 2 {
				n := 0
				err := scan(func([]string, tract.Supp) bool {
					n++
					return tt.stopAt == 0 || n < tt.stopAt
				})
				if tt.wantErr == nil && err != nil || tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Errorf("pass %d: error = %v, want %v", pass, err, tt.wantErr)
				}
				if n != tt.wantN {
					t.Errorf("pass %d: %d transactions, want %d", pass, n, tt.wantN)
				}
				opens, closes := tt.opener.opens.Load(), tt.opener.closes.Load()
				if opens != int32(pass+1) || closes != opens {
					t.Errorf("pass %d: %d opens, %d closes", pass, opens, closes)
				}
			}
		})
	}
}

func TestScanFileOpenError(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing.tab")
	err := ScanFile(path, DefaultConfig())(func([]string, tract.Supp) bool { return true })
	if !errors.Is(err, ErrOpen) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error = %v, want ErrOpen wrapping fs.ErrNotExist", err)
	}
	if !strings.HasPrefix(err.Error(), "cannot open file "+path+": ") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestScanFileReadError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // opening a directory succeeds, reading it fails
	err := ScanFile(dir, DefaultConfig())(func([]string, tract.Supp) bool { return true })
	if err == nil {
		t.Skip("reading a directory does not fail on this platform")
	}
	if !errors.Is(err, ErrRead) || !strings.HasPrefix(err.Error(), "read error on file "+dir+": ") {
		t.Fatalf("error = %v, want a read error naming %s", err, dir)
	}
}

func TestScanFileSyntaxErrorNamesFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join("testdata", "ex", "test1.csv")
	err := ScanFile(path, DefaultConfig())(func([]string, tract.Supp) bool { return true })
	if want := path + ":5(3): item expected"; err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

// openDescriptors counts the open file descriptors of the process.
func openDescriptors(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skipf("cannot list /dev/fd: %v", err)
	}
	return len(entries)
}

// TestScanFileClosesFiles checks for leaked descriptors after complete,
// stopped and failed passes of ScanFile itself. It must not run in
// parallel with tests that open files.
func TestScanFileClosesFiles(t *testing.T) {
	good := ScanFile(filepath.Join("testdata", "ex", "test1.tab"), DefaultConfig())
	bad := ScanFile(filepath.Join("testdata", "ex", "test1.csv"), DefaultConfig())
	before := openDescriptors(t)
	for range 50 {
		if _, err := collect(good); err != nil {
			t.Fatal(err)
		}
		if err := good(func([]string, tract.Supp) bool { return false }); err != nil {
			t.Fatal(err)
		}
		if _, err := collect(bad); err == nil {
			t.Fatal("expected an error")
		}
	}
	if after := openDescriptors(t); after > before {
		t.Errorf("open descriptors grew from %d to %d", before, after)
	}
}

func TestScanConcurrentPasses(t *testing.T) {
	t.Parallel()
	scan := ScanFile(filepath.Join("testdata", "ex", "test1.tab"), DefaultConfig())
	want, err := collect(scan)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			got, err := collect(scan)
			if err != nil || !sameRecords(got, want, false) {
				t.Errorf("concurrent pass = %v (%v)", got, err)
			}
		})
	}
	wg.Wait()
}

func TestScanInternsNames(t *testing.T) {
	t.Parallel()
	o := &trackingOpener{src: "a b\nb a\n"}
	ptrs := map[string]map[*byte]bool{}
	err := Scan(o.open, "in", DefaultConfig())(func(items []string, _ tract.Supp) bool {
		for _, it := range items {
			if ptrs[it] == nil {
				ptrs[it] = map[*byte]bool{}
			}
			ptrs[it][unsafe.StringData(it)] = true
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range ptrs {
		if len(p) != 1 {
			t.Errorf("item %q is backed by %d strings, want 1", name, len(p))
		}
	}
}
