package isrout

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// The golden cases (testdata/golden/cases.txt) were run through the C
// reference fpgrowth twice by reference/harness/isrout/gen.sh: once with
// the case's format options (NAME.c.out) and once with the same mining
// options and a machine-readable format (NAME.m.out, with the -Z output
// NAME.z and the -P output NAME.psp). The tests feed the values of the
// machine-readable run through a Writer configured like the first run and
// require identical bytes.

const goldenDir = "testdata/golden"

type goldenCase struct {
	name      string
	input     string
	deviation string // deviation id, "" if Go must equal C
	args      []string
}

func readGoldenCases(t *testing.T) []goldenCase {
	t.Helper()
	f, err := os.Open(filepath.Join(goldenDir, "cases.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var cases []goldenCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "case "):
			fields := strings.Fields(line)
			c := goldenCase{name: fields[1], input: fields[2]}
			if len(fields) > 3 {
				c.deviation = fields[3]
			}
			cases = append(cases, c)
		case strings.HasPrefix(line, "arg "):
			c := &cases[len(cases)-1]
			c.args = append(c.args, strings.TrimPrefix(line, "arg "))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}

// format returns the Writer format of the case's arguments with
// fpgrowth's defaults, and whether the target is association rules.
func (c goldenCase) format(t *testing.T) (f Format, rules bool) {
	t.Helper()
	f = Format{Sep: DefaultSep, Imp: DefaultImp}
	hasInfo, absolute := false, false
	for _, a := range c.args {
		switch {
		case a == "-tr":
			rules = true
		case a == "-g":
			f.Scan = true
		case strings.HasPrefix(a, "-v"):
			f.Info, hasInfo = a[2:], true
		case strings.HasPrefix(a, "-k"):
			f.Sep = a[2:]
		case strings.HasPrefix(a, "-I"):
			f.Imp = a[2:]
		case strings.HasPrefix(a, "-h"):
			f.Header = a[2:]
		case strings.HasPrefix(a, "-s"):
			s, err := strconv.ParseFloat(a[2:], 64)
			if err != nil {
				t.Fatalf("bad support argument %q", a)
			}
			absolute = s < 0
		}
	}
	if !hasInfo {
		f.Info = DefaultInfo(rules, absolute)
	}
	return f, rules
}

// machineRun holds the parsed machine-readable output of a case.
type machineRun struct {
	names []string
	total tract.Supp
	sets  []report.ItemSet
	rules []report.Rule
}

// parseMachine parses NAME.m.out: item names separated by '|' (rules:
// head " => " body), then " # " and the values of
// " # %a %17w %17e %Q" (sets) or " # %a %b %h %17e %Q" (rules).
func parseMachine(t *testing.T, path string, rules bool) machineRun {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m machineRun
	codes := map[string]tract.Item{}
	code := func(name string) tract.Item {
		c, ok := codes[name]
		if !ok {
			c = tract.Item(len(m.names))
			codes[name] = c
			m.names = append(m.names, name)
		}
		return c
	}
	items := func(s string) []tract.Item {
		if s == "" {
			return nil
		}
		var out []tract.Item
		for name := range strings.SplitSeq(s, "|") {
			out = append(out, code(name))
		}
		return out
	}
	num := func(s string) float64 {
		x, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatalf("%s: bad number %q", path, s)
		}
		return x
	}
	supp := func(s string) tract.Supp {
		x, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatalf("%s: bad integer %q", path, s)
		}
		return tract.Supp(x)
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSuffix(line, "\n")
		i := strings.LastIndex(line, " # ")
		if i < 0 {
			t.Fatalf("%s: malformed line %q", path, line)
		}
		left, vals := line[:i], strings.Fields(line[i+3:])
		if rules {
			head, body, ok := strings.Cut(left, " => ")
			if !ok || len(vals) != 5 {
				t.Fatalf("%s: malformed rule %q", path, line)
			}
			m.rules = append(m.rules, report.Rule{
				Head:     code(head),
				Body:     items(body),
				Supp:     supp(vals[0]),
				BodySupp: supp(vals[1]),
				HeadSupp: supp(vals[2]),
				Eval:     num(vals[3]),
			})
			m.total = supp(vals[4])
			continue
		}
		if len(vals) != 4 {
			t.Fatalf("%s: malformed set %q", path, line)
		}
		m.sets = append(m.sets, report.ItemSet{
			Items:  items(left),
			Supp:   supp(vals[0]),
			Weight: num(vals[1]),
			Eval:   num(vals[2]),
		})
		m.total = supp(vals[3])
	}
	return m
}

func TestGolden(t *testing.T) {
	cases := readGoldenCases(t)
	if len(cases) < 30 {
		t.Fatalf("only %d golden cases", len(cases))
	}
	update := os.Getenv("ISROUT_UPDATE") != ""
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, rules := c.format(t)
			base := filepath.Join(goldenDir, c.name)
			m := parseMachine(t, base+".m.out", rules)
			if len(m.sets)+len(m.rules) == 0 {
				t.Fatal("no output in the machine-readable run")
			}
			var out bytes.Buffer
			w := New(&out, Config{Names: m.names, Total: m.total, Format: f})
			for _, s := range m.sets {
				if err := w.ItemSet(s); err != nil {
					t.Fatal(err)
				}
			}
			for _, r := range m.rules {
				if err := w.Rule(r); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}

			cOut := readFile(t, base+".c.out")
			if c.deviation == "" {
				if got := out.String(); got != cOut {
					t.Errorf("output differs from C\n got: %q\nwant: %q", got, cOut)
				}
			} else {
				goPath := base + ".go.out"
				if update {
					if err := os.WriteFile(goPath, out.Bytes(), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if got, want := out.String(), readFile(t, goPath); got != want {
					t.Errorf("output differs from %s\n got: %q\nwant: %q", goPath, got, want)
				}
				if out.String() == cOut {
					t.Errorf("deviation %s: output equals C", c.deviation)
				}
			}

			checkGoldenStats(t, base, m)
			if !rules {
				checkGoldenSpectrum(t, base, m)
			}
		})
	}
}

// TestGoldenHeader checks the header deviation independently of the
// reviewed .go.out files: every line is the C line with the header in
// front.
func TestGoldenHeader(t *testing.T) {
	for _, c := range readGoldenCases(t) {
		if c.deviation != "O1" {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			f, _ := c.format(t)
			base := filepath.Join(goldenDir, c.name)
			var want strings.Builder
			for line := range strings.Lines(readFile(t, base+".c.out")) {
				want.WriteString(f.Header + line)
			}
			if got := readFile(t, base+".go.out"); got != want.String() {
				t.Errorf("got %q\nwant %q", got, want.String())
			}
		})
	}
}

// checkGoldenStats compares the -Z output for the per-size counts of the
// machine-readable run with C's.
func checkGoldenStats(t *testing.T, base string, m machineRun) {
	t.Helper()
	var stats []uint64
	count := func(size int) {
		for len(stats) <= size {
			stats = append(stats, 0)
		}
		stats[size]++
	}
	for _, s := range m.sets {
		count(len(s.Items))
	}
	for _, r := range m.rules {
		count(len(r.Body) + 1)
	}
	n := uint64(len(m.sets) + len(m.rules))
	if got, want := string(AppendStats(nil, n, stats)), readFile(t, base+".z"); got != want {
		t.Errorf("-Z output differs from C\n got: %q\nwant: %q", got, want)
	}
}

// checkGoldenSpectrum compares the -P output for the sets of the
// machine-readable run with C's.
func checkGoldenSpectrum(t *testing.T, base string, m machineRun) {
	t.Helper()
	p := report.NewSpectrum(0, report.MaxSize, 0, report.MaxSupp)
	for _, s := range m.sets {
		p.Add(len(s.Items), s.Supp, 1)
	}
	var out bytes.Buffer
	if err := WriteSpectrum(&out, p); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), readFile(t, base+".psp"); got != want {
		t.Errorf("-P output differs from C\n got: %q\nwant: %q", got, want)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
