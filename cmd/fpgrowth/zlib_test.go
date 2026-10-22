package main

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// manyTab writes a dataset whose item set output (-s-1) is larger than
// two 64 KiB output buffers, so that compressed output is written while
// mining is still running.
func manyTab(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	for i := range 300 {
		fmt.Fprintf(&b, "i%d i%d i%d i%d i%d i%d\n", i%13, i%17, i%19, i%23, i%29, i%31)
	}
	return writeFile(t, dir, "many.tab", b.String())
}

// inflate decompresses one zlib stream (RFC 1950) from the start of b and
// returns the decompressed data and the bytes after the stream. It
// checks the zlib header: deflate with a 32 KiB window (CMF 0x78), the
// FCHECK bits, no preset dictionary, and that it is not gzip.
func inflate(t *testing.T, b []byte) (data, rest []byte) {
	t.Helper()
	if len(b) < 2 {
		t.Fatalf("zlib stream too short: %q", b)
	}
	if b[0] != 0x78 || (uint(b[0])<<8|uint(b[1]))%31 != 0 || b[1]&0x20 != 0 {
		t.Fatalf("not a zlib (RFC 1950) header: % x", b[:2])
	}
	r := bytes.NewReader(b) // an io.ByteReader: the decoder does not read ahead
	zr, err := zlib.NewReader(r)
	if err != nil {
		t.Fatalf("zlib.NewReader: %v", err)
	}
	data, err = io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if err := zr.Close(); err != nil {
		t.Fatal(err)
	}
	return data, b[len(b)-r.Len():]
}

// TestZlibRoundTrip checks -z for item sets and rules, to a file and to
// standard output: the output is one zlib stream whose decompression is
// byte for byte the output without -z, and the log and the other
// outputs are unchanged.
func TestZlibRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	t1 := writeFile(t, dir, "t1.tab", test1)
	many := manyTab(t, dir)
	for _, opts := range [][]string{
		{},
		{"-tc"},
		{"-tm"},
		{"-tg"},
		{"-tr"},
		{"-tr", "-c0", "-k,", "-I", " => ", "-v", " %a %b %h %c %l"},
		{"-s-1", "-v", " %a"}, // absolute-support format
		{"-eb", "-d0", "-v", " %e"},
		{"-g", "-h", "#", "-v", " %S %Q"},
		{"-m5"},                // no item sets: an empty stream
		{"-s-1", "-As"},        // large output
		{"-s-1", "-tr", "-c0"}, // large rule output
	} {
		for _, in := range []string{t1, many} {
			if in == many && !slices.Contains(opts, "-s-1") {
				continue
			}
			name := strings.Join(append(slices.Clone(opts), filepath.Base(in)), " ")
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				args := append(slices.Clone(opts), in, "-")
				want, wantLog, code := invocation{args: args}.run(t)
				if code != 0 {
					t.Fatalf("exit %d: %s", code, wantLog)
				}
				// standard output, also clustered with other options
				for _, z := range [][]string{{"-z"}, {"-zN"}} {
					got, log, code := invocation{args: append(slices.Clone(z), args...)}.run(t)
					if code != 0 || log != wantLog {
						t.Fatalf("%v: exit %d, log %q, want %q", z, code, log, wantLog)
					}
					data, rest := inflate(t, []byte(got))
					if string(data) != want || len(rest) != 0 {
						t.Errorf("%v: decompressed output differs (%d bytes, want %d; %d bytes after the stream)",
							z, len(data), len(want), len(rest))
					}
				}
				// a file
				out := filepath.Join(t.TempDir(), "out.z")
				stdout, log, code := invocation{args: append([]string{"-z"}, append(slices.Clone(opts), in, out)...)}.run(t)
				if code != 0 || stdout != "" || log != strings.ReplaceAll(wantLog, "<stdout>", out) {
					t.Fatalf("file: exit %d, stdout %q, log %q", code, stdout, log)
				}
				b, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				if data, rest := inflate(t, b); string(data) != want || len(rest) != 0 {
					t.Errorf("file: decompressed output differs")
				}
			})
		}
	}
}

// TestZlibOtherOutputs checks that -z compresses only the item set
// output: the statistics (-Z), the pattern spectrum (-P), the log and
// count-only mode are unchanged.
func TestZlibOtherOutputs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	in := writeFile(t, dir, "t1.tab", test1)
	const stats = "all: 21\n  0: 0\n  1: 5\n  2: 9\n  3: 6\n  4: 1\n"
	sets, _, _ := invocation{args: []string{in, "-"}}.run(t)
	psp, _, _ := invocation{args: []string{"-P", "", in}}.run(t)

	// count only: no output file, -z has no effect
	for _, opts := range [][]string{{}, {"-Z"}, {"-tr"}, {"-P", ""}, {"-Z", "-P", ""}} {
		args := append(slices.Clone(opts), in)
		wantOut, wantLog, wantCode := invocation{args: args}.run(t)
		got, log, code := invocation{args: append([]string{"-z"}, args...)}.run(t)
		if got != wantOut || log != wantLog || code != wantCode {
			t.Errorf("count only %v: exit %d stdout %q log %q, want %d %q %q",
				opts, code, got, log, wantCode, wantOut, wantLog)
		}
	}

	// -Z and -P to stdout, item sets to a file
	out := filepath.Join(dir, "out.z")
	got, _, code := invocation{args: []string{"-z", "-Z", "-P", "", in, out}}.run(t)
	if code != 0 || got != stats+psp {
		t.Errorf("-Z -P with a file: exit %d stdout %q", code, got)
	}
	b, _ := os.ReadFile(out)
	if data, _ := inflate(t, b); string(data) != sets {
		t.Errorf("-Z -P with a file: item sets %q", data)
	}

	// -P to a file
	pspFile := filepath.Join(dir, "psp.txt")
	got, _, code = invocation{args: []string{"-z", "-P", pspFile, in, "-"}}.run(t)
	if b, _ := os.ReadFile(pspFile); code != 0 || string(b) != psp {
		t.Errorf("-P file: exit %d, spectrum %q", code, b)
	}
	if data, rest := inflate(t, []byte(got)); string(data) != sets || len(rest) != 0 {
		t.Errorf("-P file: stdout %q", got)
	}

	// everything to stdout: the complete zlib stream, then the plain
	// statistics (O6, O12), then the plain pattern spectrum
	got, log, code := invocation{args: []string{"-z", "-Z", "-P", "", in, "-"}}.run(t)
	_, wantLog, _ := invocation{args: []string{"-Z", "-P", "", in, "-"}}.run(t)
	if code != 0 || log != wantLog {
		t.Fatalf("stdout: exit %d log %q", code, log)
	}
	data, rest := inflate(t, []byte(got))
	if string(data) != sets || string(rest) != stats+psp {
		t.Errorf("stdout: sets %q, after the stream %q", data, rest)
	}
}

// failFile is an output file that fails writes after n bytes and,
// optionally, its Close. It records whether Close was called.
type failFile struct {
	n        int
	closeErr error
	buf      bytes.Buffer
	closed   bool
}

func (f *failFile) Write(p []byte) (int, error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	if f.buf.Len()+len(p) > f.n {
		return 0, errClosed
	}
	return f.buf.Write(p)
}

func (f *failFile) Close() error {
	if f.closed {
		return os.ErrClosed
	}
	f.closed = true
	return f.closeErr
}

// TestZlibWriteErrors checks that write errors of the compressed output
// at every stage (zlib header, while mining, the final block and
// checksum at close, closing the file) give upstream's message and exit
// status 4, never a silent success, and that the file is closed.
func TestZlibWriteErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	t1 := writeFile(t, dir, "t1.tab", test1)
	many := manyTab(t, dir)
	full, _, _ := invocation{args: []string{"-z", "-s-1", many, "-"}}.run(t)
	small, _, _ := invocation{args: []string{"-z", t1, "-"}}.run(t)
	tail := regexp.MustCompile(`writing (\S+) \.\.\. \[(\d+) (set|rule)\(s\)\] done \[T\]\.\n\nfpgrowth: write error on file (\S+)\n$`)
	check := func(t *testing.T, stderr string, code int, name string) int {
		t.Helper()
		m := tail.FindStringSubmatch(stderr)
		if code != exitWrite || m == nil || m[1] != name || m[4] != name {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		n, _ := strconv.Atoi(m[2])
		return n
	}
	for _, tc := range []struct {
		name  string
		in    string
		args  []string
		limit int
		early bool // the error surfaces while mining (fewer sets logged)
	}{
		{"header", t1, nil, 0, false},
		{"header many", many, []string{"-s-1"}, 0, true},
		{"first block", many, []string{"-s-1"}, 2, true},
		{"mid stream", many, []string{"-s-1"}, len(full) / 2, true},
		{"mid stream rules", many, []string{"-s-1", "-tr", "-c0"}, 1000, false},
		{"final block", t1, nil, 2, false},
		{"checksum", t1, nil, len(small) - 1, false},
		{"stats after the stream", t1, []string{"-Z"}, len(small), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := append(append([]string{"-z"}, tc.args...), tc.in)
			// standard output
			w := &limitWriter{n: tc.limit}
			_, stderr, code := invocation{args: append(slices.Clone(args), "-"), stdout: w}.run(t)
			n := check(t, stderr, code, "<stdout>")
			if tc.early && n == 0 {
				t.Errorf("no sets logged")
			}
			if !strings.HasPrefix(full, w.buf.String()) && !strings.HasPrefix(small, w.buf.String()) &&
				!slices.Contains(tc.args, "-tr") {
				t.Errorf("written bytes are not a prefix of the compressed output")
			}
			if tc.name == "stats after the stream" {
				return // the item set file is not involved
			}
			// a file
			out := filepath.Join(dir, "never-created")
			f := &failFile{n: tc.limit}
			_, stderr, code = invocation{args: append(slices.Clone(args), out),
				create: func(string) (io.WriteCloser, error) { return f, nil }}.run(t)
			check(t, stderr, code, out)
			if !f.closed {
				t.Errorf("output file not closed")
			}
		})
	}
	// closing the file fails after a complete stream
	f := &failFile{n: 1 << 30, closeErr: errors.New("close failed")}
	out := filepath.Join(dir, "out.z")
	_, stderr, code := invocation{args: []string{"-z", t1, out},
		create: func(string) (io.WriteCloser, error) { return f, nil }}.run(t)
	if check(t, stderr, code, out); !f.closed || f.buf.String() != small {
		t.Errorf("closed %v, written %q", f.closed, f.buf.String())
	}
	// the output file cannot be created
	_, stderr, code = invocation{args: []string{"-z", t1, filepath.Join(dir, "no", "out")}}.run(t)
	if code != exitOpen || !strings.HasSuffix(stderr, "cannot open file "+filepath.Join(dir, "no", "out")+"\n") {
		t.Errorf("open: exit %d stderr %q", code, stderr)
	}
}

// TestZlibClosedPipe writes compressed output into a pipe whose reader
// is gone: exit status 4, "write error on file <stdout>".
func TestZlibClosedPipe(t *testing.T) {
	t.Parallel()
	in := manyTab(t, t.TempDir())
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	defer w.Close()
	_, stderr, code := invocation{args: []string{"-z", "-s-1", in, "-"}, stdout: w}.run(t)
	if code != exitWrite || !strings.HasSuffix(stderr, "fpgrowth: write error on file <stdout>\n") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

// onceFailWriter fails only its write number failAt (1-based) and
// accepts all others.
type onceFailWriter struct {
	failAt, n int
	buf       bytes.Buffer
}

func (w *onceFailWriter) Write(p []byte) (int, error) {
	w.n++
	if w.n == w.failAt {
		return 0, errClosed
	}
	return w.buf.Write(p)
}

// TestZlibStatsAfterFailedClose checks -z -Z with the item sets on
// standard output when only finishing the zlib stream fails: the
// statistics written after it succeed, but the truncated stream must
// still give exit status 4.
func TestZlibStatsAfterFailedClose(t *testing.T) {
	t.Parallel()
	in := writeFile(t, t.TempDir(), "t1.tab", test1)
	// write 1: the zlib header; write 2: the compressed data, written
	// when the stream is closed; then the checksum and the statistics
	w := &onceFailWriter{failAt: 2}
	_, stderr, code := invocation{args: []string{"-z", "-Z", in, "-"}, stdout: w}.run(t)
	if code != exitWrite || !strings.HasSuffix(stderr, "fpgrowth: write error on file <stdout>\n") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if w.n <= w.failAt || !strings.Contains(w.buf.String(), "all: 21\n") {
		t.Errorf("the statistics were not written after the failed close (%d writes, %q)", w.n, w.buf.String())
	}
}
