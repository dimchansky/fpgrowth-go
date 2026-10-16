package main

import (
	"bytes"
	"context"
	"errors"
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

// The golden files are produced by the C reference (see
// reference/harness/cli/README.md):
//
//	testdata/cases.txt        "<name> <shell words>" per case
//	testdata/data/            datasets (written by the harness)
//	testdata/golden/          C outputs: <name>.stdout, .stderr, .exit
//	testdata/deviations.txt   cases whose Go output differs by design
//	testdata/golden-go/       the Go outputs of those cases (go test -update)
var update = flag.Bool("update", false, "rewrite testdata/golden-go for the deviation cases")

// errClosed is the write error of a closed stdout (">&-").
var errClosed = errors.New("write: bad file descriptor")

type closedWriter struct{}

func (closedWriter) Write([]byte) (int, error) { return 0, errClosed }

// cliCase is one line of cases.txt.
type cliCase struct {
	name      string
	args      []string
	stdin     string // file for stdin, "" for an empty stdin
	closedOut bool
}

// splitWords splits a line into shell words: blanks separate words,
// '...' quotes literally, "..." quotes with \", \\, \$ and \` escapes,
// and a backslash outside quotes escapes the next byte. It reports for
// every word whether any part of it was quoted.
func splitWords(s string) (words []string, quoted []bool, err error) {
	var cur strings.Builder
	inWord, isQuoted := false, false
	flush := func() {
		if inWord {
			words = append(words, cur.String())
			quoted = append(quoted, isQuoted)
		}
		cur.Reset()
		inWord, isQuoted = false, false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			flush()
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, nil, fmt.Errorf("unterminated ' in %q", s)
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
			inWord, isQuoted = true, true
		case c == '"':
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0 {
					i++
				}
				cur.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, nil, fmt.Errorf("unterminated \" in %q", s)
			}
			inWord, isQuoted = true, true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord, isQuoted = true, true
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	flush()
	return words, quoted, nil
}

func readCases(t *testing.T) []cliCase {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "cases.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []cliCase
	for line := range strings.Lines(string(b)) {
		line = strings.TrimRight(line, "\n")
		if line == "" || line[0] == '#' {
			continue
		}
		words, quoted, err := splitWords(line)
		if err != nil {
			t.Fatal(err)
		}
		c := cliCase{name: words[0], args: []string{}}
		for i, w := range words[1:] {
			switch {
			case !quoted[i+1] && w == ">&-":
				c.closedOut = true
			case !quoted[i+1] && strings.HasPrefix(w, "<"):
				c.stdin = w[1:]
			default:
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
		if len(f) < 2 {
			t.Fatalf("bad deviation line %q", line)
		}
		devs[f[0]] = f[1]
	}
	return devs
}

// result is the observable outcome of a run.
type result struct {
	stdout, stderr string
	exit           int
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
	var stdout, stderr bytes.Buffer
	var out io.Writer = &stdout
	if c.closedOut {
		out = closedWriter{}
	}
	code := run(context.Background(), env{
		args:    append([]string{"fpgrowth"}, c.args...),
		stdin:   stdin,
		stdout:  out,
		stderr:  &stderr,
		tempDir: t.TempDir(),
	})
	return result{stdout: stdout.String(), stderr: normalize(stderr.String()), exit: code}
}

func readResult(t *testing.T, dir, name string) result {
	t.Helper()
	read := func(ext string) string {
		b, err := os.ReadFile(filepath.Join(dir, name+ext))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	code, err := strconv.Atoi(strings.TrimSpace(read(".exit")))
	if err != nil {
		t.Fatal(err)
	}
	return result{stdout: read(".stdout"), stderr: read(".stderr"), exit: code}
}

func writeResult(t *testing.T, dir, name string, r result) {
	t.Helper()
	for ext, s := range map[string]string{
		".stdout": r.stdout, ".stderr": r.stderr, ".exit": strconv.Itoa(r.exit) + "\n",
	} {
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
	if want.stdout != got.stdout {
		fmt.Fprintf(&b, "stdout:\nwant %q\ngot  %q\n", want.stdout, got.stdout)
	}
	if want.stderr != got.stderr {
		fmt.Fprintf(&b, "stderr:\nwant %q\ngot  %q\n", want.stderr, got.stderr)
	}
	return b.String()
}

// TestGolden runs every case of cases.txt and requires stdout, stderr
// (times normalized) and the exit status to equal the C reference, except
// for the documented deviations: those must differ from the C reference
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
					t.Errorf("fpgrowth %q differs from C:\n%s", c.args, d)
				}
				return
			}
			differ++
			if diffResult(want, got) == "" {
				t.Errorf("deviation %s: fpgrowth %q matches C, expected a difference", id, c.args)
			}
			if *update {
				writeResult(t, goDir, c.name, got)
				return
			}
			if d := diffResult(readResult(t, goDir, c.name), got); d != "" {
				t.Errorf("deviation %s: fpgrowth %q differs from the reviewed Go output:\n%s", id, c.args, d)
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
