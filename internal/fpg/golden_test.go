package fpg

import (
	"bufio"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tabread"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Golden files (see reference/harness/fpg/README.md):
//
//	testdata/golden/data/*.tab   input datasets (*.w.tab are weighted, -w)
//	testdata/golden/cases.txt    "<dataset> <options>" per line
//	testdata/golden/expected.txt output of the C reference per case
//	testdata/golden/deviations.txt  cases whose output differs by design
const goldenDir = "testdata/golden"

// goldenCase is one section of expected.txt.
type goldenCase struct {
	data  string
	opts  string
	exit  int
	lines []string
}

func (c goldenCase) key() string { return c.data + " | " + c.opts }

// parseOpts translates the fpgrowth options used in cases.txt into a
// Config, like the fpgrowth program's main.
func parseOpts(opts string) (Config, error) {
	cfg := DefaultConfig()
	for _, o := range strings.Fields(opts) {
		if len(o) < 2 || o[0] != '-' {
			return cfg, fmt.Errorf("bad option %q", o)
		}
		arg := o[2:]
		num := func() float64 {
			v, err := strconv.ParseFloat(arg, 64)
			if err != nil {
				panic(fmt.Sprintf("bad option %q", o))
			}
			return v
		}
		switch o[1] {
		case 't':
			cfg.Target = map[string]report.Target{
				"s": report.TargetAll, "f": report.TargetAll, "c": report.TargetClosed,
				"m": report.TargetMaximal, "g": report.TargetGenerators,
				"r": report.TargetRules,
			}[arg]
		case 'A':
			cfg.Algo = map[string]Algorithm{
				"s": AlgoSimple, "c": AlgoComplex, "d": AlgoSingle, "t": AlgoTopDown,
			}[arg]
		case 'l':
			cfg.Pack = max(0, min(int(num()), 16))
		case 's':
			cfg.SMin = num()
		case 'S':
			cfg.SMax = num()
		case 'm':
			cfg.ZMin = int(num())
		case 'n':
			cfg.ZMax = int(num())
		case 'q':
			cfg.Sort = int(num())
		case 'x':
			cfg.Mode &^= ModePerfect
		case 'j':
			cfg.Mode &^= ModeReorder
		case 'u':
			cfg.Mode &^= ModeTail
		case 'e':
			switch arg {
			case "x":
				cfg.Eval = EvalNone
			case "b":
				cfg.Eval = EvalLogRatio
			default:
				return cfg, fmt.Errorf("unsupported measure %q", o)
			}
		case 'd':
			cfg.Thresh = num()
		default:
			return cfg, fmt.Errorf("unsupported option %q", o)
		}
	}
	return cfg, nil
}

// readGolden parses expected.txt.
func readGolden(t *testing.T) []goldenCase {
	t.Helper()
	f, err := os.Open(filepath.Join(goldenDir, "expected.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var cases []goldenCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if rest, ok := strings.CutPrefix(line, "### "); ok {
			data, opts, ok := strings.Cut(rest, " | ")
			if !ok {
				t.Fatalf("bad header %q", line)
			}
			cases = append(cases, goldenCase{data: data, opts: opts})
			continue
		}
		if len(cases) == 0 {
			t.Fatalf("line before first header: %q", line)
		}
		c := &cases[len(cases)-1]
		if rest, ok := strings.CutPrefix(line, "exit "); ok && c.lines == nil {
			c.exit, err = strconv.Atoi(rest)
			if err != nil {
				t.Fatalf("bad exit line %q", line)
			}
			c.lines = []string{}
			continue
		}
		c.lines = append(c.lines, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}

// readDeviations parses deviations.txt: "<dataset> | <options> | <id>".
func readDeviations(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(goldenDir, "deviations.txt"))
	if err != nil {
		t.Fatal(err)
	}
	dev := map[string]string{}
	for line := range strings.Lines(string(b)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.LastIndex(line, " | ")
		if i < 0 {
			t.Fatalf("bad deviation line %q", line)
		}
		dev[line[:i]] = line[i+3:]
	}
	return dev
}

// goldenData caches the scan and the oracle transactions of a dataset.
type goldenData struct {
	scan tract.ScanFunc[string]
	txs  []oracle.Transaction
}

func loadGoldenData(t *testing.T, cache map[string]*goldenData, name string) *goldenData {
	t.Helper()
	if d := cache[name]; d != nil {
		return d
	}
	cfg := tabread.DefaultConfig()
	cfg.Weighted = strings.HasSuffix(name, ".w.tab")
	scan := tabread.ScanFile(filepath.Join(goldenDir, "data", name), cfg)
	d := &goldenData{scan: scan}
	err := scan(func(items []string, w tract.Supp) bool {
		d.txs = append(d.txs, oracle.Transaction{Items: slices.Clone(items), Weight: w})
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	cache[name] = d
	return d
}

// oracleLines checks the item sets of a Go run against the oracle.
func oracleCheck(t *testing.T, d *goldenData, cfg Config, got []oracle.Set) string {
	t.Helper()
	db, err := oracle.New(d.txs, oracle.Options{})
	if err != nil {
		t.Fatal(err)
	}
	tg := map[report.Target]oracle.Target{
		report.TargetAll: oracle.Frequent, report.TargetClosed: oracle.Closed,
		report.TargetMaximal: oracle.Maximal, report.TargetGenerators: oracle.Generators,
	}
	n := cfg
	n.normalize()
	p := oracle.NewSetParams(tg[n.Target], oracle.AbsMinSupp(cfg.SMin, db.Total()))
	p.MaxSupp = oracle.AbsMaxSupp(cfg.SMax, db.Total())
	p.MinSize = cfg.ZMin
	if cfg.ZMax < MaxSize {
		p.MaxSize = cfg.ZMax
	}
	want, err := db.Mine(p)
	if err != nil {
		t.Fatal(err)
	}
	return oracle.DiffSets(want, got, oracle.DiffOptions{IgnoreEval: true})
}

// isSubsequence reports whether a is a proper subsequence of b.
func isSubsequence(a, b []string) bool {
	if len(a) >= len(b) {
		return false
	}
	j := 0
	for _, x := range b {
		if j < len(a) && a[j] == x {
			j++
		}
	}
	return j == len(a)
}

// TestGolden replays the C reference outputs: every case must produce
// the identical line sequence, except the documented deviations.
func TestGolden(t *testing.T) {
	t.Parallel()
	cases := readGolden(t)
	devs := readDeviations(t)
	cache := map[string]*goldenData{}
	used := map[string]bool{}
	same, differ := 0, 0
	for _, gc := range cases {
		cfg, err := parseOpts(gc.opts)
		if err != nil {
			t.Fatalf("%s: %v", gc.key(), err)
		}
		d := loadGoldenData(t, cache, gc.data)
		res, err := run(d.scan, cfg, runOpts{})
		var got []string
		switch {
		case errors.Is(err, ErrNoItems):
			got = nil // upstream exits with status 15
			if gc.exit != 15 {
				t.Errorf("%s: ErrNoItems, C exit %d", gc.key(), gc.exit)
			}
		case err != nil:
			t.Fatalf("%s: %v", gc.key(), err)
		default:
			got = res.sink.lines
		}
		dev, isDev := devs[gc.key()]
		used[gc.key()] = isDev
		if !isDev {
			if gc.exit != 0 && gc.exit != 15 {
				t.Errorf("%s: C exit %d, not listed as a deviation", gc.key(), gc.exit)
			}
			if !slices.Equal(got, gc.lines) {
				t.Errorf("%s: output differs from C\nC:  %q\nGo: %q", gc.key(), gc.lines, got)
			}
			same++
			continue
		}
		differ++
		if diff := oracleCheck(t, d, cfg, res.sink.sets); diff != "" {
			t.Errorf("%s (%s): Go differs from the oracle:\n%s", gc.key(), dev, diff)
		}
		cOK := gc.exit == 0
		switch dev {
		case "B1": // upstream crashes or prints wrong items
			if cOK && slices.Equal(got, gc.lines) {
				t.Errorf("%s (B1): expected a difference", gc.key())
			}
		case "B3", "B4": // upstream misses sets
			if !cOK || !isSubsequence(gc.lines, got) {
				t.Errorf("%s (%s): C output is not a proper subsequence of Go's\nC:  %q\nGo: %q",
					gc.key(), dev, gc.lines, got)
			}
		case "B5": // upstream reports extra sets
			if !cOK || !isSubsequence(got, gc.lines) {
				t.Errorf("%s (B5): Go output is not a proper subsequence of C's\nC:  %q\nGo: %q",
					gc.key(), gc.lines, got)
			}
		default:
			t.Errorf("%s: unknown deviation %q", gc.key(), dev)
		}
	}
	for k := range devs {
		if _, ok := used[k]; !ok {
			t.Errorf("deviation %q matches no case", k)
		}
	}
	t.Logf("%d golden cases: %d identical, %d documented deviations", len(cases), same, differ)
}

// TestGoldenGenerate writes the random datasets and cases.txt (run with
// FPG_GOLDEN_GEN=1, then reference/harness/fpg/gen.sh).
func TestGoldenGenerate(t *testing.T) {
	if os.Getenv("FPG_GOLDEN_GEN") == "" {
		t.Skip("set FPG_GOLDEN_GEN=1 to regenerate the golden inputs")
	}
	dir := filepath.Join(goldenDir, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	variantOpts := []string{
		"-As -l0", "-As -l3", "-As", "-Ac", "-Ac -j", "-Ac -l0", "-Ac -j -l0",
		"-Ad -l0", "-Ad -l3", "-Ad", "-At",
	}
	extra := []string{
		"", "-x", "-q1", "-q0", "-q-1", "-q-2", "-m2", "-m0", "-n2", "-x -n3",
		"-u", "-S60", "-eb -d20", "-x -q1", "-m3 -q0",
	}
	type dataset struct {
		name string
		supp string
	}
	sets := []dataset{{"test1.tab", "-s-2"}, {"test3.w.tab", "-s-3"}}
	r := rand.New(rand.NewPCG(2026, 10))
	for i := range 16 {
		gc := oracle.GenConfig{
			Seed:            uint64(1000 + i),
			NumTransactions: 4 + r.IntN(16),
			NumItems:        2 + r.IntN(9),
			MinLen:          r.IntN(2),
			Skew:            r.IntN(3),
			Patterns:        1 + r.IntN(2),
			PatternProb:     0.5,
			DupTxProb:       0.2,
			EmptyProb:       0.05,
			Names:           oracle.NamesLetters,
		}
		gc.MaxLen = gc.MinLen + 2 + r.IntN(4)
		gc.PatternLen = 1 + r.IntN(min(3, gc.NumItems))
		weighted := i%3 == 2
		if weighted {
			gc.Weights = oracle.WeightsSmall
		}
		if i == 15 { // more than 16 items: packing and tree paths of -Ac
			gc.NumItems, gc.NumTransactions, gc.MaxLen = 19, 14, 12
			gc.PatternLen = 6
		}
		txs, err := oracle.Generate(gc)
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("r%02d.tab", i)
		if weighted {
			name = fmt.Sprintf("r%02d.w.tab", i)
		}
		var b strings.Builder
		if err := oracle.WriteTable(&b, txs, weighted); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		supp := []string{"-s-2", "-s-2", "-s-3", "-s30"}[r.IntN(4)]
		sets = append(sets, dataset{name, supp})
	}
	var out strings.Builder
	seen := map[string]bool{}
	out.WriteString("# <dataset> <options>; generated by TestGoldenGenerate\n")
	for k, ds := range sets {
		for vi, v := range variantOpts {
			for ti, tg := range []string{"-ts", "-tc", "-tm", "-tg"} {
				// a deterministic rotation of 4 option sets per variant
				for j := range 4 {
					e := extra[(k*7+vi*3+ti*5+j*4)%len(extra)]
					if j == 0 {
						e = ""
					}
					opts := strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s", tg, v, ds.supp, e)), " ")
					line := ds.name + " " + opts
					if !seen[line] {
						seen[line] = true
						out.WriteString(line + "\n")
					}
				}
			}
		}
	}
	// Reproducers of the upstream bugs fixed by the port
	// (docs/upstream/fpgrowth-core.md §6, reference-build.md F3).
	all := []string{"-As -l0", "-As", "-Ac", "-Ac -l0", "-Ad", "-Ad -l0", "-At"}
	for _, h := range []struct {
		name, data string
		opts       []string
	}{
		{"b1a.tab", "x a b\nx a\nx b c\nx a b c\nx c\n", []string{"-ts -s20", "-ts -s20 -x", "-tc -s20"}},
		{"b1b.tab", "a b\na c\nb c\nd\nd\nd\n", []string{"-ts -m2 -q1 -s10"}},
		{"b3.tab", "a\na b\na b\nb c\n", []string{"-tg -s-1 -m1", "-tg -s-1 -m2"}},
		{"b4.tab", "a\na b\na c\na\n", []string{"-tm -s-2", "-tm -s-2 -x", "-tc -s-2"}},
		{"m9.tab", "a\n", []string{"-tm -s-1 -x", "-tm -s-1"}},
		{"b5c.w.tab", "i0 i3 i2 i1 x 2\ni0 i3 i2 i1 y 2\ni0 i3 5\ni0 3\ni2 z 1\n",
			[]string{"-tc -s-1 -n2 -q-1", "-tm -s-1 -n2 -q-1"}},
		{"b5m.tab", "\ni5 i2 i0 i7 i3\ni2\ni1\ni1 i3 i7 i8\ni0 i5 i6\n",
			[]string{"-tm -s-1 -n2 -x", "-tm -s-1 -n2", "-tc -s-1 -n2 -x"}},
		{"b5x.w.tab", "a d 1\nd a 4\ne 4\nd a 5\na 4\na d b 2\ne a d 3\nd 4\ne 1\ne 3\n" +
			"a e a 2\nc e d a 3\nc a d b a c 4\nc 1\ne d 3\na d e 2\n",
			[]string{"-tc -s-3 -n2 -q-1", "-tc -s-3 -n2 -q-1 -x", "-tm -s-3 -n2 -q-1 -x"}},
	} {
		if err := os.WriteFile(filepath.Join(dir, h.name), []byte(h.data), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, o := range h.opts {
			for _, v := range all {
				fmt.Fprintf(&out, "%s %s %s\n", h.name, strings.Replace(o, " ", " "+v+" ", 1), "")
			}
		}
	}
	text := strings.ReplaceAll(out.String(), " \n", "\n")
	if err := os.WriteFile(filepath.Join(goldenDir, "cases.txt"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
