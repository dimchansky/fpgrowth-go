package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The golden files are produced by the C oracle, upstream fpgpsp with the
// patches of reference/patches/fpgpsp (reference/harness/fpgpsp/gen-cli.sh):
//
//	testdata/cases.txt        "<name> <words>" per case
//	testdata/data/            datasets (written by the harness)
//	testdata/golden/          C outputs: <name>.stdout, .stderr, .exit
//	                          and .file (the output file out.txt)
//	testdata/deviations.txt   cases whose Go output differs by design
//	testdata/golden-go/       the Go outputs of those cases (go test -update)
//
// Every case is deterministic: surrogate cases fix -S and -Z, and the
// estimation is not compared here (difftest's TestFpgpsp compares it with
// a tolerance; its last bits depend on the platform's log and exp).
var update = flag.Bool("update", false, "rewrite testdata/golden-go for the deviation cases")

// cliCase is one line of cases.txt.
type cliCase struct {
	name  string
	args  []string
	stdin string // file for stdin, "" for an empty stdin
}

func readCases(t *testing.T) []cliCase { return readCasesFile(t, "cases.txt") }

// readCasesFile parses a case file of testdata.
func readCasesFile(t *testing.T, name string) []cliCase {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var cases []cliCase
	for line := range strings.Lines(string(b)) {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		c := cliCase{name: f[0], args: []string{}}
		for _, w := range f[1:] {
			if in, ok := strings.CutPrefix(w, "<"); ok {
				c.stdin = in
			} else {
				c.args = append(c.args, w)
			}
		}
		cases = append(cases, c)
	}
	return cases
}

// readDeviations parses deviations.txt: "<name> <id> <comment>".
func readDeviations(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "deviations.txt"))
	if err != nil {
		t.Fatal(err)
	}
	devs := map[string]string{}
	for line := range strings.Lines(string(b)) {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		devs[f[0]] = f[1]
	}
	return devs
}

// result is the observable outcome of a run.
type result struct {
	stdout, stderr, file string
	exit                 int
}

var timeRE = regexp.MustCompile(`\[[0-9]+\.[0-9]{2}s\]`)

// normalize replaces the times in log messages like the harness does.
func normalize(stderr string) string { return timeRE.ReplaceAllString(stderr, "[T]") }

// runCase runs a case in the current directory (testdata/data).
func runCase(t *testing.T, c cliCase) result {
	t.Helper()
	var stdin io.Reader = strings.NewReader("")
	if c.stdin != "" {
		b, err := os.ReadFile(c.stdin)
		if err != nil {
			t.Fatal(err)
		}
		stdin = bytes.NewReader(b)
	}
	os.Remove("out.txt")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), env{
		args:    append([]string{"fpgpsp"}, c.args...),
		stdin:   stdin,
		stdout:  &stdout,
		stderr:  &stderr,
		tempDir: t.TempDir(),
	})
	r := result{stdout: stdout.String(), stderr: normalize(stderr.String()), exit: code}
	if b, err := os.ReadFile("out.txt"); err == nil {
		r.file = string(b)
		os.Remove("out.txt")
	}
	return r
}

func readResult(t *testing.T, dir, name string) result {
	t.Helper()
	read := func(ext string, must bool) string {
		b, err := os.ReadFile(filepath.Join(dir, name+ext))
		if err != nil && must {
			t.Fatal(err)
		}
		return string(b)
	}
	code, err := strconv.Atoi(strings.TrimSpace(read(".exit", true)))
	if err != nil {
		t.Fatal(err)
	}
	return result{stdout: read(".stdout", true), stderr: read(".stderr", true), file: read(".file", false), exit: code}
}

func writeResult(t *testing.T, dir, name string, r result) {
	t.Helper()
	files := map[string]string{".stdout": r.stdout, ".stderr": r.stderr, ".exit": strconv.Itoa(r.exit) + "\n"}
	if r.file != "" {
		files[".file"] = r.file
	}
	for ext, s := range files {
		if err := os.WriteFile(filepath.Join(dir, name+ext), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func diffResult(want, got result) string {
	var b strings.Builder
	if want.exit != got.exit {
		fmt.Fprintf(&b, "exit: want %d, got %d\n", want.exit, got.exit)
	}
	for _, f := range []struct{ name, w, g string }{
		{"stdout", want.stdout, got.stdout}, {"stderr", want.stderr, got.stderr}, {"file", want.file, got.file},
	} {
		if f.w != f.g {
			fmt.Fprintf(&b, "%s:\nwant %q\ngot  %q\n", f.name, f.w, f.g)
		}
	}
	return b.String()
}

// TestGolden runs every case of cases.txt and requires stdout, stderr
// (times normalized), the output file and the exit status to equal the C
// oracle, except for the documented deviations: those must differ from it
// and equal the reviewed Go outputs in testdata/golden-go.
func TestGolden(t *testing.T) {
	cases := readCases(t)
	devs := readDeviations(t)
	cDir, _ := filepath.Abs(filepath.Join("testdata", "golden"))
	goDir, _ := filepath.Abs(filepath.Join("testdata", "golden-go"))
	t.Chdir(filepath.Join("testdata", "data"))
	if *update {
		if err := os.MkdirAll(goDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	same, differ := 0, 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runCase(t, c)
			want := readResult(t, cDir, c.name)
			id, isDev := devs[c.name]
			seen[c.name] = true
			if !isDev {
				same++
				if d := diffResult(want, got); d != "" {
					t.Errorf("fpgpsp %q differs from C:\n%s", c.args, d)
				}
				return
			}
			differ++
			if diffResult(want, got) == "" {
				t.Errorf("deviation %s: fpgpsp %q matches C, expected a difference", id, c.args)
			}
			if *update {
				writeResult(t, goDir, c.name, got)
				return
			}
			if d := diffResult(readResult(t, goDir, c.name), got); d != "" {
				t.Errorf("deviation %s: fpgpsp %q differs from the reviewed Go output:\n%s", id, c.args, d)
			}
		})
	}
	for name := range devs {
		if !seen[name] {
			t.Errorf("deviation %q matches no case", name)
		}
	}
	t.Logf("%d cases: %d identical to C, %d documented deviations", len(cases), same, differ)
}
