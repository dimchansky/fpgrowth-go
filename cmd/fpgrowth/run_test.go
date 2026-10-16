package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

const test1 = "a b c\na d e\nb c d\na b c d\nb c\na b d\nd e\na b c d\nc d e\na b c\n"

// invocation runs the program with the given stdin, stdout and context.
type invocation struct {
	args    []string
	stdin   io.Reader
	stdout  io.Writer
	ctx     context.Context
	tempDir string
}

func (inv invocation) run(t *testing.T) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	e := env{
		args:    append([]string{"fpgrowth"}, inv.args...),
		stdin:   inv.stdin,
		stdout:  inv.stdout,
		stderr:  &errOut,
		tempDir: inv.tempDir,
	}
	if e.stdin == nil {
		e.stdin = strings.NewReader("")
	}
	if e.stdout == nil {
		e.stdout = &out
	}
	ctx := inv.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	code = run(ctx, e)
	return out.String(), normalize(errOut.String()), code
}

func writeFile(t *testing.T, dir, name, data string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// assertEmptyDir fails if the spool directory is not empty.
func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		t.Errorf("leftover temporary file %s", e.Name())
	}
}

// blockingReader blocks every Read until it is closed.
type blockingReader struct{ done chan struct{} }

func (b blockingReader) Read([]byte) (int, error) {
	<-b.done
	return 0, io.EOF
}

// failingReader returns data, then an error.
type failingReader struct {
	data string
	err  error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.data == "" {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

// limitWriter accepts n bytes, then fails.
type limitWriter struct {
	n   int
	buf bytes.Buffer
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.n {
		return 0, errClosed
	}
	return w.buf.Write(p)
}

// TestSpoolRemoved checks that the spool file of standard input is
// removed after successful and failed runs.
func TestSpoolRemoved(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		args  []string
		stdin string
		code  int
	}{
		{"success", []string{"-", "-"}, test1, exitOK},
		{"rules", []string{"-tr", "-", "-"}, test1, exitOK},
		{"syntax error", []string{"-w", "-", "-"}, "a b x\n", exitTaWgt},
		{"no items", []string{"-s90", "-", "-"}, test1, exitNoItems},
		{"empty", []string{"-", "-"}, "", exitNoItems},
		{"bad output", []string{"-", "nodir/out"}, test1, exitOpen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			_, stderr, code := invocation{args: tt.args, stdin: strings.NewReader(tt.stdin), tempDir: dir}.run(t)
			if code != tt.code {
				t.Errorf("exit %d, want %d; stderr %q", code, tt.code, stderr)
			}
			assertEmptyDir(t, dir)
		})
	}
}

// TestStdinMatchesFile checks that reading from stdin gives the same
// result as reading the file.
func TestStdinMatchesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := writeFile(t, dir, "t1.tab", test1)
	for _, args := range [][]string{{"-tr"}, {"-tc", "-el", "-d0"}, {"-s-2", "-Z"}} {
		fromFile, _, c1 := invocation{args: append(cloneArgs(args), in, "-")}.run(t)
		fromStdin, stderr, c2 := invocation{args: append(cloneArgs(args), "-", "-"),
			stdin: strings.NewReader(test1), tempDir: t.TempDir()}.run(t)
		if c1 != 0 || c2 != 0 || fromFile != fromStdin {
			t.Errorf("%q: file %d %q, stdin %d %q", args, c1, fromFile, c2, fromStdin)
		}
		if !strings.Contains(stderr, "reading <stdin> ... [5 item(s), 10 transaction(s)]") {
			t.Errorf("stderr %q", stderr)
		}
	}
}

func cloneArgs(s []string) []string { return append([]string{}, s...) }

// TestInterrupt checks cancellation (SIGINT): while blocked on standard
// input, while reading a file, and while mining.
func TestInterrupt(t *testing.T) {
	t.Parallel()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	var big strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&big, "a%d b%d c\n", i%7, i%11)
	}
	bigFile := writeFile(t, dir, "big.tab", big.String())
	small := writeFile(t, dir, "small.tab", test1)
	block := blockingReader{done: make(chan struct{})}
	t.Cleanup(func() { close(block.done) })
	tests := []struct {
		name  string
		args  []string
		stdin io.Reader
	}{
		{"blocked stdin", []string{"-", "-"}, block},
		{"blocked selection", []string{"-R", "", small, "-"}, block},
		{"reading", []string{bigFile, "-"}, nil},
		{"mining", []string{small, "-"}, nil},
		{"mining rules", []string{"-tr", small, "-"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spool := t.TempDir()
			_, stderr, code := invocation{args: tt.args, stdin: tt.stdin, ctx: canceled, tempDir: spool}.run(t)
			if code != exitInterrupt || !strings.HasSuffix(stderr, "\nfpgrowth: interrupted\n") {
				t.Errorf("exit %d, stderr %q", code, stderr)
			}
			assertEmptyDir(t, spool)
		})
	}
}

// TestWriteError checks write errors of the item set output: exit status
// 4 with upstream's message after the "writing" log line. Mining stops at
// the first failed write, so the log reports the sets written until then.
func TestWriteError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var b strings.Builder
	for i := range 300 {
		fmt.Fprintf(&b, "i%d i%d i%d i%d i%d i%d\n", i%13, i%17, i%19, i%23, i%29, i%31)
	}
	in := writeFile(t, dir, "many.tab", b.String())
	full, stderr, code := invocation{args: []string{"-s-1", in, "-"}}.run(t)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	total := strings.Count(full, "\n")
	if len(full) < 135000 { // more than two 64 KiB buffers
		t.Fatalf("output too small: %d bytes", len(full))
	}
	for _, args := range [][]string{{"-s-1", in, "-"}, {"-s-1", "-tr", "-c0", in, "-"}} {
		w := &limitWriter{n: 100000}
		_, stderr, code := invocation{args: args, stdout: w}.run(t)
		m := regexp.MustCompile(`writing <stdout> \.\.\. \[(\d+) (set|rule)\(s\)\] done \[T\]\.\n\nfpgrowth: write error on file <stdout>\n$`).FindStringSubmatch(stderr)
		if code != exitWrite || m == nil {
			t.Fatalf("%q: exit %d, stderr %q", args, code, stderr)
		}
		if n, _ := strconv.Atoi(m[1]); m[2] == "set" && (n >= total || n == 0) {
			t.Errorf("reported %d of %d sets", n, total)
		}
		if !strings.HasPrefix(full, w.buf.String()) && m[2] == "set" {
			t.Errorf("written output is not a prefix of the full output")
		}
	}
	// small output: the error surfaces at close, after mining completed
	_, stderr, code = invocation{args: []string{"-Z", writeFile(t, dir, "t1.tab", test1), "-"}, stdout: closedWriter{}}.run(t)
	if code != exitWrite || !strings.HasSuffix(stderr,
		"writing <stdout> ... [21 set(s)] done [T].\n\nfpgrowth: write error on file <stdout>\n") {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
}

// TestOutputFiles checks item set, statistics and pattern spectrum
// output to files.
func TestOutputFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := writeFile(t, dir, "t1.tab", test1)
	want, _, code := invocation{args: []string{in, "-"}}.run(t)
	if code != 0 {
		t.Fatal(code)
	}
	wantPsp, _, _ := invocation{args: []string{"-P", "", in}}.run(t)
	out := filepath.Join(dir, "out.txt")
	psp := filepath.Join(dir, "psp.txt")
	stdout, stderr, code := invocation{args: []string{"-Z", "-P", psp, in, out}}.run(t)
	if code != 0 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	if stdout != "all: 21\n  0: 0\n  1: 5\n  2: 9\n  3: 6\n  4: 1\n" {
		t.Errorf("stats %q", stdout)
	}
	for file, w := range map[string]string{out: want, psp: wantPsp} {
		got, err := os.ReadFile(file)
		if err != nil || string(got) != w {
			t.Errorf("%s: %q (%v), want %q", file, got, err, w)
		}
	}
	if !strings.Contains(stderr, "writing "+out+" ... [21 set(s)]") ||
		!strings.Contains(stderr, "writing "+psp+" ... [13 signature(s)]") {
		t.Errorf("stderr %q", stderr)
	}
	// unwritable -P: reported after the item sets were written (exit 2)
	os.Remove(out)
	_, stderr, code = invocation{args: []string{"-P", filepath.Join(dir, "no", "p"), in, out}}.run(t)
	if got, _ := os.ReadFile(out); code != exitOpen || string(got) != want ||
		!strings.HasSuffix(stderr, "cannot open file "+filepath.Join(dir, "no", "p")+"\n") {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
	// statistics to a failing stdout (deviation: reported, exit 4)
	_, stderr, code = invocation{args: []string{"-Z", in, out}, stdout: closedWriter{}}.run(t)
	if code != exitWrite || !strings.HasSuffix(stderr, "write error on file <stdout>\n") {
		t.Errorf("stats: exit %d stderr %q", code, stderr)
	}
	// pattern spectrum to a failing stdout (deviation O7)
	_, stderr, code = invocation{args: []string{"-P", "", in, out}, stdout: closedWriter{}}.run(t)
	if code != exitWrite || !strings.HasSuffix(stderr, "write error on file <stdout>\n") {
		t.Errorf("psp: exit %d stderr %q", code, stderr)
	}
	// count only: no output file
	_, stderr, code = invocation{args: []string{"-tr", in}}.run(t)
	if code != 0 || !strings.Contains(stderr, "writing <null> ... [9 rule(s)]") {
		t.Errorf("count only: exit %d stderr %q", code, stderr)
	}
}

// TestInputErrors checks errors of reading the input that the golden
// cases cannot provoke.
func TestInputErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name  string
		args  []string
		stdin io.Reader
		temp  string
		code  int
		tail  string
	}{
		{"read error", []string{"-", "-"}, &failingReader{data: "a b\n", err: errors.New("EIO")}, "",
			exitRead, "reading <stdin> ... \nfpgrowth: read error on file <stdin>\n"},
		{"selection read error", []string{"-R", "", writeFile(t, dir, "x.tab", test1)},
			&failingReader{err: errors.New("EIO")}, "", exitRead, "\nfpgrowth: read error on file <stdin>\n"},
		{"no spool directory", []string{"-", "-"}, strings.NewReader(test1), filepath.Join(dir, "missing"),
			exitOpen, "reading <stdin> ... \nfpgrowth: cannot create temporary file for <stdin>\n"},
		{"weight overflow", []string{"-w", writeFile(t, dir, "w.tab", "a 9000000000000000000\nb 9000000000000000000\n"), "-"},
			nil, "", exitTaWgt, "total transaction weight too large\n"},
		{"selection from stdin", []string{"-R", "", writeFile(t, dir, "y.tab", test1), "-"},
			strings.NewReader("a c\nd a\n"), "", exitOK, "writing <stdout> ... [7 set(s)] done [T].\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			temp := tt.temp
			if temp == "" {
				temp = t.TempDir()
			}
			_, stderr, code := invocation{args: tt.args, stdin: tt.stdin, tempDir: temp}.run(t)
			if code != tt.code || !strings.HasSuffix(stderr, tt.tail) {
				t.Errorf("exit %d, stderr %q; want %d, suffix %q", code, stderr, tt.code, tt.tail)
			}
		})
	}
}

// TestSplitWords checks the shell word splitter of the golden test.
func TestSplitWords(t *testing.T) {
	t.Parallel()
	words, quoted, err := splitWords(`a -v' (%a)|' "x\"y" '' b\ c <in >&-`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "-v (%a)|", `x"y`, "", "b c", "<in", ">&-"}
	if fmt.Sprint(words) != fmt.Sprint(want) || fmt.Sprint(quoted) != "[false true true true true false false]" {
		t.Errorf("%q %v", words, quoted)
	}
	for _, bad := range []string{`'x`, `"x`} {
		if _, _, err := splitWords(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestBorderSupp(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		v    float64
		w    int64
		want int64
	}{
		{60, 10, 6}, {50, 10, 5}, {-6, 10, 6}, {-5.5, 10, 6}, {0, 10, 0},
		{math.Inf(-1), 10, math.MaxInt64}, {math.Inf(1), 10, math.MaxInt64}, {-1e300, 10, math.MaxInt64},
	} {
		if got := borderSupp(tc.v, tract.Supp(tc.w)); int64(got) != tc.want {
			t.Errorf("borderSupp(%g, %d) = %d, want %d", tc.v, tc.w, got, tc.want)
		}
	}
}
