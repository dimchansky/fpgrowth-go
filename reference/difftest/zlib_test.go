package difftest

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// defaultZlibCases is the number of random cases of TestZlib
// (DIFFTEST_ZLIB_CASES overrides it; -short: at most 100).
const defaultZlibCases = 1000

var (
	zlibOnce sync.Once
	zlibBin  string
	zlibSkip string
	zlibErr  error
)

// zlibReference returns upstream's program built with USE_ZLIB (option
// -z): the path in DIFFTEST_C_ZLIB, else reference/build/zlib (built
// with ../build.sh zlib if it is missing or DIFFTEST_REBUILD is set).
// Without a C compiler or the zlib development files the test is
// skipped.
func zlibReference(t *testing.T) string {
	t.Helper()
	zlibOnce.Do(func() {
		if p := os.Getenv("DIFFTEST_C_ZLIB"); p != "" {
			zlibBin = p
			return
		}
		refDir, err := filepath.Abs("..")
		if err != nil {
			zlibErr = err
			return
		}
		path := filepath.Join(refDir, "build", "zlib", "fpgrowth", "src", "fpgrowth")
		if _, err := os.Stat(path); err == nil && os.Getenv("DIFFTEST_REBUILD") == "" {
			zlibBin = path
			return
		}
		cc := strings.Fields(os.Getenv("CC") + " cc")[0]
		if _, err := exec.LookPath(cc); err != nil {
			zlibSkip = fmt.Sprintf("no C compiler %q to build the zlib reference (set CC, or DIFFTEST_C_ZLIB to a built fpgrowth)", cc)
			return
		}
		cmd := exec.Command("./build.sh", "zlib")
		cmd.Dir = refDir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			if log, _ := os.ReadFile(filepath.Join(refDir, "build", "zlib", "build.log")); bytes.Contains(log, []byte("zlib.h")) ||
				bytes.Contains(log, []byte("-lz")) {
				zlibSkip = "the zlib development files (zlib.h, libz) are not installed: " + lastLine(log)
				return
			}
			zlibErr = fmt.Errorf("build.sh zlib: %v\n%s", err, stderr.Bytes())
			return
		}
		for _, l := range strings.Split(string(out), "\n") {
			if p, ok := strings.CutPrefix(l, "zlib "); ok {
				zlibBin = p
				return
			}
		}
		zlibErr = fmt.Errorf("build.sh zlib printed no binary path: %q", out)
	})
	if zlibSkip != "" {
		t.Skip(zlibSkip)
	}
	if zlibErr != nil {
		t.Fatal(zlibErr)
	}
	return zlibBin
}

// inflate decompresses the zlib stream at the start of b. It returns the
// data decompressed until an error, the bytes after the stream and the
// error (nil for a complete stream with a valid checksum).
func inflate(b []byte) (data, rest []byte, err error) {
	r := bytes.NewReader(b) // an io.ByteReader: the decoder does not read ahead
	zr, err := zlib.NewReader(r)
	if err != nil {
		return nil, b, err
	}
	data, err = io.ReadAll(zr)
	if err == nil {
		err = zr.Close()
	}
	return data, b[len(b)-r.Len():], err
}

// zrun runs a program with -z prepended to args and replaces the item
// set file of the result by its decompression. zerr is the error of
// decompressing it (a missing, incomplete or invalid stream, or bytes
// after it).
func zrun(bin, dir string, args []string) (r *runResult, zerr error) {
	r = run(bin, dir, append([]string{"-z"}, args...))
	if !r.hasOut {
		return r, nil
	}
	data, rest, err := inflate(r.out)
	if err == nil && len(rest) > 0 {
		err = fmt.Errorf("%d bytes after the zlib stream", len(rest))
	}
	r.out = data
	return r, err
}

// TestZlib compares option -z of the Go program with upstream's program
// built with USE_ZLIB (build.sh zlib). The compressed bytes depend on the
// deflate implementation (Go's compress/flate vs zlib, both at the
// default level), so the decompressed outputs are compared: on fixed
// cases byte for byte (including the usage message, which lists -z in
// this build), and on random cases of the differential test (newCase)
// with -z added, by the same evaluation as TestDifferential.
func TestZlib(t *testing.T) {
	zc := zlibReference(t)
	b := getBinaries(t)
	t.Run("fixed", func(t *testing.T) { testZlibFixed(t, zc, b.goBin) })
	t.Run("random", func(t *testing.T) { testZlibRandom(t, zc, b.goBin) })
}

// testZlibFixed runs a fixed option matrix with output to a file and to
// standard output, and the usage and help messages.
func testZlibFixed(t *testing.T, zc, goBin string) {
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "..", "cmd", "fpgrowth", "testdata", "data", "test1.tab"))
	if err != nil {
		t.Fatal(err)
	}
	var many strings.Builder // an output of several 64 KiB buffers
	for i := range 300 {
		fmt.Fprintf(&many, "i%d i%d i%d i%d i%d i%d\n", i%13, i%17, i%19, i%23, i%29, i%31)
	}
	files := map[string]string{"test1.tab": string(data), "many.tab": many.String(),
		"test3.tab": "a b c 2\nb c d 1\na c d 3\nc d e 1\na b d 2\n"}
	for name, s := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// usage and help: identical, with the -z line in the usage
	for _, args := range [][]string{nil, {"-!"}} {
		cr, gr := run(zc, dir, args), run(goBin, dir, args)
		if cr.exit != gr.exit || !bytes.Equal(cr.stdout, gr.stdout) || !bytes.Equal(cr.stderr, gr.stderr) {
			t.Errorf("%q: C and Go differ:\nC:  %q\nGo: %q", args, cr.stdout, gr.stdout)
		}
		if args == nil && !bytes.Contains(gr.stdout, []byte("\n-z       compress output with zlib (deflate)      (default: plain text)\n")) {
			t.Errorf("usage without -z line")
		}
	}
	var cases [][]string
	for _, opts := range [][]string{
		{}, {"-tc"}, {"-tm"}, {"-tg"}, {"-tr"}, {"-tr", "-c50", "-k,", "-I", " => "},
		{"-s-2", "-v", " %a"}, {"-tr", "-s-2", "-v", " %b %h %c"}, {"-Ad"}, {"-As", "-l0"}, {"-At"},
		{"-m5"}, {"-Z"}, {"-P", pspFile}, {"-Z", "-P", ""}, {"-k,", "-v", " %a %s %Q"}, // (-h and -g: deviations O1, O2)
	} {
		cases = append(cases, append(slices.Clone(opts), "test1.tab", outFile))
		if !slices.Contains(opts, "-Z") && !slices.Contains(opts, "") {
			// to stdout (-Z to stdout differs by design, O12)
			cases = append(cases, append(slices.Clone(opts), "test1.tab", "-"))
		}
	}
	cases = append(cases,
		[]string{"-w", "test3.tab", outFile}, []string{"-w", "-tr", "-c0", "test3.tab", "-"},
		[]string{"-s-1", "many.tab", outFile}, []string{"-s-1", "many.tab", "-"},
		[]string{"-s-1", "-tr", "-c0", "many.tab", outFile}, []string{"-s-1", "-tc", "-v", " %a", "many.tab", "-"},
		[]string{"test1.tab"}, []string{"-Z", "-tr", "test1.tab"}, // count only: no effect
	)
	for _, args := range cases {
		cr, cerr := zrun(zc, dir, args)
		gr, gerr := zrun(goBin, dir, args)
		if cerr != nil || gerr != nil {
			t.Errorf("%q: decompressing the item set file: C %v, Go %v", args, cerr, gerr)
			continue
		}
		if args[len(args)-1] == "-" {
			for _, r := range []*runResult{cr, gr} {
				d, rest, err := inflate(r.stdout)
				if err != nil || len(rest) > 0 {
					t.Errorf("%q: decompressing stdout: %v, %d bytes after the stream", args, err, len(rest))
				}
				r.stdout = d
			}
		}
		for _, f := range []struct {
			name string
			c, g []byte
		}{{"stdout", cr.stdout, gr.stdout}, {"stderr", cr.stderr, gr.stderr}, {"item sets", cr.out, gr.out}, {"spectrum", cr.psp, gr.psp}} {
			if !bytes.Equal(f.c, f.g) {
				t.Errorf("%q: %s differs:\nC:  %q\nGo: %q", args, f.name, trunc(f.c), trunc(f.g))
			}
		}
		if cr.exit != 0 || gr.exit != 0 || cr.hasOut != gr.hasOut || cr.hasPsp != gr.hasPsp {
			t.Errorf("%q: exit C %d Go %d, item set file C %v Go %v", args, cr.exit, gr.exit, cr.hasOut, gr.hasOut)
		}
		// the decompressed output is the output without -z
		plain := run(goBin, dir, args)
		if want := plain.out; args[len(args)-1] == "-" {
			if !bytes.Equal(gr.stdout, plain.stdout) {
				t.Errorf("%q: decompressed stdout differs from the output without -z", args)
			}
		} else if !bytes.Equal(gr.out, want) || !bytes.Equal(gr.stdout, plain.stdout) {
			t.Errorf("%q: decompressed output differs from the output without -z", args)
		}
	}
	// -Z with the item sets on stdout (O12): upstream prints the plain
	// statistics with fprintf while the compressed sets are still
	// buffered, here before the stream (with more output, inside it);
	// Go writes them after the complete stream. The item sets agree.
	args := []string{"-Z", "test1.tab", "-"}
	plain := run(goBin, dir, []string{"-Z", "test1.tab", outFile})
	cr, gr := run(zc, dir, append([]string{"-z"}, args...)), run(goBin, dir, append([]string{"-z"}, args...))
	cSets, cRest, cErr := inflate(bytes.TrimPrefix(cr.stdout, plain.stdout))
	gSets, gRest, gErr := inflate(gr.stdout)
	if !bytes.HasPrefix(cr.stdout, plain.stdout) || cErr != nil || len(cRest) != 0 ||
		gErr != nil || !bytes.Equal(gRest, plain.stdout) || !bytes.Equal(cSets, gSets) || !bytes.Equal(gSets, plain.out) {
		t.Errorf("-z -Z to stdout (O12): C %q, Go %q", trunc(cr.stdout), trunc(gr.stdout))
	}
	t.Logf("%d fixed cases", len(cases)+3)
}

// trunc shortens a difference for the log.
func trunc(b []byte) []byte {
	if len(b) > 300 {
		return append(slices.Clone(b[:300]), "..."...)
	}
	return b
}

// testZlibRandom runs random cases of the differential test with -z.
// Both programs must write a complete zlib stream when they succeed; the
// decompressed outputs are evaluated like TestDifferential's.
func testZlibRandom(t *testing.T, zc, goBin string) {
	n := envInt("DIFFTEST_ZLIB_CASES", defaultZlibCases)
	if testing.Short() {
		n = min(n, 100)
	}
	seed := baseSeed()
	sum := newSummary(fmt.Sprintf("zlib difftest (base seed %d)", seed),
		fmt.Sprintf("DIFFTEST_SEED=%d go test ./difftest/ -run TestZlib/random -v  # case %%d", seed))
	root := t.TempDir()
	work := make(chan int)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range work {
				c, err := newCase(seed, i)
				if err != nil {
					t.Errorf("case %d: %v", i, err)
					continue
				}
				dir := filepath.Join(root, "case-"+strconv.Itoa(i))
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Error(err)
					continue
				}
				if err := c.writeFiles(dir); err != nil {
					t.Errorf("case %d: %v", i, err)
					continue
				}
				var zerrs []string
				zpair := func(c *tcase) (cr, gr *runResult) {
					args := c.args()
					cr, cerr := zrun(zc, dir, args)
					if c.cOnly {
						if cerr != nil && !cr.signaled && cr.exit == 0 {
							zerrs = append(zerrs, "C: "+cerr.Error())
						}
						return cr, nil
					}
					gr, gerr := zrun(goBin, dir, args)
					// Go always finishes the stream it opened; C does so
					// when it succeeds (a crash or an error exit leaves a
					// truncated stream).
					if cerr != nil && !cr.signaled && cr.exit == 0 {
						zerrs = append(zerrs, "C: "+cerr.Error())
					}
					if gerr != nil {
						zerrs = append(zerrs, "Go: "+gerr.Error())
					}
					return cr, gr
				}
				cr, gr := zpair(c)
				o := evaluate(c, cr, gr, zpair)
				if len(zerrs) > 0 {
					o.fail("zlib stream: %s", strings.Join(zerrs, "; "))
					o.verdict = verdictFail
				}
				sum.add(c, o, gr, "")
				os.RemoveAll(dir)
			}
		})
	}
	for i := range n {
		work <- i
	}
	close(work)
	wg.Wait()
	t.Log("\n" + sum.String())
	slices.Sort(sum.failures)
	for k, f := range sum.failures {
		if k == 20 {
			t.Errorf("... and %d more failures", len(sum.failures)-k)
			break
		}
		t.Errorf("%s", f)
	}
}
