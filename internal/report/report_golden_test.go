package report

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestGolden replays every script testdata/**/*.isr through the Go
// reporter and compares the output with <name>.c.out, produced by the C
// harness reference/harness/report (upstream report.c, clomax.c and
// patspec.c). A script that exercises a documented deviation declares it
// with a "# deviation: <id>" line and has a reviewed <name>.go.out with the
// expected Go output; the test then requires the Go output to equal
// <name>.go.out and to differ from the C output.
func TestGolden(t *testing.T) {
	t.Parallel()
	var scripts []string
	err := filepath.WalkDir("testdata", func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".isr") {
			scripts = append(scripts, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) == 0 {
		t.Fatal("no scripts found")
	}
	deviation := regexp.MustCompile(`(?m)^# deviation: (\S+)`)
	for _, path := range scripts {
		name := strings.TrimSuffix(path, ".isr")
		t.Run(strings.TrimPrefix(name, "testdata/"), func(t *testing.T) {
			t.Parallel()
			src := readFile(t, path)
			cOut := readFile(t, name+".c.out")
			got, err := runScript(src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			devs := deviation.FindAllStringSubmatch(src, -1)
			if len(devs) == 0 {
				compareOutput(t, got, cOut, "C harness")
				return
			}
			goOut := readFile(t, name+".go.out")
			compareOutput(t, got, goOut, "reviewed Go output")
			if outputsMatch(got, cOut) {
				t.Errorf("script declares deviation %s but Go and C outputs agree", devs[0][1])
			}
		})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func outputsMatch(got, want string) bool {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	if len(g) != len(w) {
		return false
	}
	for i := range g {
		if !sameOutputLine(g[i], w[i]) {
			return false
		}
	}
	return true
}

func compareOutput(t *testing.T, got, want, what string) {
	t.Helper()
	if outputsMatch(got, want) {
		return
	}
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if !sameOutputLine(gl, wl) {
			t.Fatalf("output differs from %s at line %d:\n got: %q\nwant: %q\n(%d vs %d lines)",
				what, i+1, gl, wl, len(g), len(w))
		}
	}
}
