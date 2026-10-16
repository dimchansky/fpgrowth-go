package fpg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/isrout"
	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tabread"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Golden files of the rules path (see reference/harness/fpg/README.md):
//
//	testdata/golden/tree_cases.txt       "<dataset> <options>" per line
//	testdata/golden/tree_expected.txt    output of the C reference per case
//	testdata/golden/tree_deviations.txt  cases that differ by design
//
// Rules are written with the info format " [%a %b %h %e]" and item sets
// with " [%a %e]" (the C side gets the same -v), through isrout like the
// fpgrowth program.

// treeInfo returns the -v format of a tree golden case.
func treeInfo(opts string) string {
	if slices.Contains(strings.Fields(opts), "-tr") {
		return " [%a %b %h %e]"
	}
	return " [%a %e]"
}

// treeOpts holds a parsed tree golden case.
type treeOpts struct {
	cfg Config
	app string // -R appearance file
}

// parseTreeOpts translates the fpgrowth options of tree_cases.txt into a
// Config like the fpgrowth program's main (each option is one word with
// an attached value).
func parseTreeOpts(opts string) (treeOpts, error) {
	o := treeOpts{cfg: DefaultConfig()}
	c := &o.cfg
	for _, w := range strings.Fields(opts) {
		if len(w) < 2 || w[0] != '-' {
			return o, fmt.Errorf("bad option %q", w)
		}
		arg := w[2:]
		num := func() (float64, error) { return strconv.ParseFloat(arg, 64) }
		var err error
		var v float64
		switch w[1] {
		case 't':
			tg, ok := map[string]report.Target{
				"s": report.TargetAll, "c": report.TargetClosed, "m": report.TargetMaximal,
				"g": report.TargetGenerators, "r": report.TargetRules,
			}[arg]
			if !ok {
				return o, fmt.Errorf("bad target %q", w)
			}
			c.Target = tg
		case 'A':
			c.Algo = map[string]Algorithm{"s": AlgoSimple, "c": AlgoComplex, "d": AlgoSingle, "t": AlgoTopDown}[arg]
		case 'e':
			switch {
			case arg == "b":
				c.Eval = EvalLogRatio
			case len(arg) == 1:
				m, ok := ruleval.ByLetter(arg[0])
				if !ok {
					return o, fmt.Errorf("bad measure %q", w)
				}
				c.Eval = int(m)
			default:
				return o, fmt.Errorf("bad measure %q", w)
			}
		case 'a':
			c.Agg = map[string]int{"x": AggNone, "m": AggMin, "n": AggMax, "a": AggAvg}[arg]
		case 'R':
			o.app = arg
		case 'o':
			c.Mode |= ModeOrigSupp
		case 'i':
			c.InvalidateBelowExpected = true
		case 'j':
			c.Mode &^= ModeReorder
		case 'x':
			c.Mode &^= ModePerfect
		case 's', 'S', 'c', 'd', 'm', 'n', 'q', 'p', 'l':
			if v, err = num(); err != nil {
				return o, fmt.Errorf("bad option %q", w)
			}
			switch w[1] {
			case 's':
				c.SMin = v
			case 'S':
				c.SMax = v
			case 'c':
				c.Conf = v
			case 'd':
				c.Thresh = v
			case 'm':
				c.ZMin = int(v)
			case 'n':
				c.ZMax = int(v)
			case 'q':
				c.Sort = int(v)
			case 'p':
				c.Prune = int(v)
			case 'l':
				c.Pack = max(0, min(int(v), 16))
			}
		default:
			return o, fmt.Errorf("unsupported option %q", w)
		}
	}
	return o, nil
}

// teeSink forwards to the text writer and records typed results.
type teeSink struct {
	w *isrout.Writer
	t *treeSink
}

func (s teeSink) ItemSet(is report.ItemSet) error {
	if err := s.t.ItemSet(is); err != nil {
		return err
	}
	return s.w.ItemSet(is)
}

func (s teeSink) Rule(r report.Rule) error {
	if err := s.t.Rule(r); err != nil {
		return err
	}
	return s.w.Rule(r)
}

// treeRun is the result of a tree golden case.
type treeRun struct {
	lines []string
	sink  *treeSink
	order []string // items by code
	err   error
}

func runTreeGolden(t *testing.T, d *goldenData, data string, o treeOpts, info string) treeRun {
	t.Helper()
	ib := tract.NewItemBase[string]()
	if o.app != "" {
		f, err := os.Open(filepath.Join(goldenDir, "data", o.app))
		if err != nil {
			t.Fatal(err)
		}
		def, items, err := tabread.ReadAppearances(f, o.app, tabread.DefaultConfig())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		ib.SetDefaultAppearance(tract.Appearance(def))
		for _, it := range items {
			if err := ib.AddAppearance(it.Name, tract.Appearance(it.App)); err != nil {
				t.Fatal(err)
			}
		}
	}
	m, err := New[string](o.cfg)
	if err != nil {
		t.Fatalf("%s: %v", data, err)
	}
	if err := ib.Count(d.scan); err != nil {
		t.Fatal(err)
	}
	if err := m.Data(ib, d.scan); err != nil {
		return treeRun{err: err}
	}
	rep, err := m.Reporter()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, ib.Len())
	for i := range names {
		names[i] = ib.ID(tract.Item(i))
	}
	var buf bytes.Buffer
	w := isrout.New(&buf, isrout.Config{
		Names: names, Total: m.Bag().TotalWeight(),
		Format: isrout.Format{Sep: isrout.DefaultSep, Imp: isrout.DefaultImp, Info: info},
	})
	ts := &treeSink{ib: ib}
	rep.SetSink(teeSink{w: w, t: ts})
	if err := m.Mine(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(buf.String(), "\n")
	return treeRun{lines: lines[:len(lines)-1], sink: ts, order: names}
}

// treeOracleDiff compares a Go run with the brute-force definitions of
// rules and evaluated item sets (no prune filtering).
func treeOracleDiff(t *testing.T, d *goldenData, o treeOpts, r treeRun) string {
	t.Helper()
	db, err := oracle.New(d.txs, oracle.Options{})
	if err != nil {
		t.Fatal(err)
	}
	c := o.cfg
	total := db.Total()
	if c.Target == report.TargetRules {
		p := oracle.NewRuleParams(oracle.EffectiveMinSupp(oracle.AbsMinSupp(c.SMin, total)), c.Conf/100)
		p.OrigSupp = c.Mode&ModeOrigSupp != 0
		p.MaxSupp = oracle.AbsMaxSupp(c.SMax, total)
		p.MinSize = c.ZMin
		if c.ZMax < MaxSize {
			p.MaxSize = c.ZMax
		}
		p.Eval = oracleMeasure(c)
		if o.app != "" {
			ib := r.sink.ib
			p.Appear = map[string]oracle.Appearance{}
			p.DefaultAppear = oracleAppear(ib.DefaultAppearance())
			for _, it := range db.Items() {
				if code, ok := ib.Code(it); ok && code >= 0 {
					p.Appear[it] = oracleAppear(ib.Appearance(code))
				} else {
					p.Appear[it] = oracle.AppearNone // removed by the recoding
				}
			}
		}
		want, err := db.Rules(p)
		if err != nil {
			t.Fatal(err)
		}
		return oracle.DiffRules(want, r.sink.rules, oracle.DiffOptions{})
	}
	tg := map[report.Target]oracle.Target{
		report.TargetAll: oracle.Frequent, report.TargetClosed: oracle.Closed,
		report.TargetMaximal: oracle.Maximal, report.TargetGenerators: oracle.Generators,
	}[c.Target]
	p := oracle.NewSetParams(tg, oracle.AbsMinSupp(c.SMin, total))
	p.MaxSupp = oracle.AbsMaxSupp(c.SMax, total)
	p.MinSize = c.ZMin
	if c.ZMax < MaxSize {
		p.MaxSize = c.ZMax
	}
	p.Eval = &oracle.SetEval{Measure: *oracleMeasure(c), Agg: oracle.Aggregation(c.Agg), Order: r.order}
	want, err := db.Mine(p)
	if err != nil {
		t.Fatal(err)
	}
	return oracle.DiffSets(want, r.sink.sets, oracle.DiffOptions{})
}

// TestTreeGolden replays the C reference outputs of the rules path: every
// case must produce the identical lines (items, supports, measure values
// with 6 significant digits) in the identical order, except the
// documented deviations, which must differ in their documented way:
//
//	R1  -tr -eb: C prints 0 as the evaluation (no rule measure); Go
//	    evaluates the imbalance ratio. Same rules, Go equals the oracle.
//	R2  -p with a measure: C ignores the evaluation when filtering; for
//	    frequent item sets Go's output is a proper subsequence of C's (for
//	    closed/maximal sets the filtered family has other closed/maximal
//	    members, so only the difference is required).
//	R3  -R with head-only items: C puts them into bodies; Go equals the
//	    oracle and its lines are a proper subsequence of C's.
//	R4  conviction of a rule with confidence 1 is +Inf (C: 0), so Go
//	    reports more rules; C's lines are a proper subsequence of Go's.
//	R8  -tr -S: C limits the support of body and head, Go the body
//	    support (equal to the oracle).
//	R9  Yates' correction is clamped at 0 (C: spurious positive values);
//	    Go equals the oracle and is a proper subsequence of C.
func TestTreeGolden(t *testing.T) {
	t.Parallel()
	cases := readGoldenFile(t, "tree_expected.txt")
	devs := readDeviationsFile(t, "tree_deviations.txt")
	cache := map[string]*goldenData{}
	used := map[string]bool{}
	same, differ := 0, 0
	for _, gc := range cases {
		o, err := parseTreeOpts(gc.opts)
		if err != nil {
			t.Fatalf("%s: %v", gc.key(), err)
		}
		d := loadGoldenData(t, cache, gc.data)
		r := runTreeGolden(t, d, gc.data, o, treeInfo(gc.opts))
		var got []string
		switch {
		case errors.Is(r.err, ErrNoItems):
			if gc.exit != 15 {
				t.Errorf("%s: ErrNoItems, C exit %d", gc.key(), gc.exit)
			}
		case r.err != nil:
			t.Fatalf("%s: %v", gc.key(), r.err)
		default:
			got = r.lines
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
		if gc.exit != 0 || r.err != nil {
			t.Errorf("%s (%s): C exit %d, Go error %v", gc.key(), dev, gc.exit, r.err)
			continue
		}
		if slices.Equal(got, gc.lines) {
			t.Errorf("%s (%s): expected a difference", gc.key(), dev)
		}
		oracleOK := func() {
			if diff := treeOracleDiff(t, d, o, r); diff != "" {
				t.Errorf("%s (%s): Go differs from the oracle:\n%s", gc.key(), dev, diff)
			}
		}
		goInC := func() {
			if !isSubsequence(got, gc.lines) {
				t.Errorf("%s (%s): Go output is not a proper subsequence of C's\nC:  %q\nGo: %q",
					gc.key(), dev, gc.lines, got)
			}
		}
		switch dev {
		case "R1", "R8":
			oracleOK()
		case "R3", "R9":
			oracleOK()
			goInC()
		case "R4":
			oracleOK()
			if !isSubsequence(gc.lines, got) {
				t.Errorf("%s (R4): C output is not a proper subsequence of Go's\nC:  %q\nGo: %q",
					gc.key(), gc.lines, got)
			}
		case "R2":
			if o.cfg.Target == report.TargetAll {
				goInC() // pruning only removes sets
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
	t.Logf("%d tree golden cases: %d identical, %d documented deviations", len(cases), same, differ)
}

// readGoldenFile parses an expected-output file like expected.txt.
func readGoldenFile(t *testing.T, name string) []goldenCase {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(goldenDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	for line := range strings.Lines(string(b)) {
		line = strings.TrimSuffix(line, "\n")
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
			if c.exit, err = strconv.Atoi(rest); err != nil {
				t.Fatalf("bad exit line %q", line)
			}
			c.lines = []string{}
			continue
		}
		c.lines = append(c.lines, line)
	}
	return cases
}

// readDeviationsFile parses "<dataset> | <options> | <id>" lines.
func readDeviationsFile(t *testing.T, name string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(goldenDir, name))
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

// TestTreeGoldenGenerate writes tree_cases.txt and the appearance files
// (run with FPG_GOLDEN_GEN=1, then reference/harness/fpg/gen.sh).
func TestTreeGoldenGenerate(t *testing.T) {
	if os.Getenv("FPG_GOLDEN_GEN") == "" {
		t.Skip("set FPG_GOLDEN_GEN=1 to regenerate the golden inputs")
	}
	dir := filepath.Join(goldenDir, "data")
	for name, text := range map[string]string{
		// a head only, e body only, b ignored
		"app1.app": "both\na out\ne in\nb -\n",
		// default body only, two head-only items
		"app2.app": "in\nc out\nd out\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out strings.Builder
	out.WriteString("# <dataset> <options>; generated by TestTreeGoldenGenerate\n")
	add := func(data, opts string) { fmt.Fprintf(&out, "%s %s\n", data, opts) }
	type ds struct{ name, supp string }
	all := []ds{{"test1.tab", "-s-2"}, {"test3.w.tab", "-s-3"}, {"r00.tab", "-s-2"},
		{"r02.w.tab", "-s-3"}, {"r06.tab", "-s20"}, {"r09.tab", "-s-2"}}
	letters := "acdeghijklmnopqrstuvwyz" // rule measures except b (R1) and x
	for k, d := range all {
		s := d.supp
		// rules: variants, sort orders, confidence, sizes, -o
		for _, o := range []string{"", "-c0", "-c50", "-As", "-Ad", "-At", "-Ac -j",
			"-q0", "-q1", "-q-1", "-q-2", "-c0 -m2 -n3", "-c0 -m0", "-o -c60"} {
			add(d.name, strings.TrimSpace("-tr "+s+" "+o))
		}
		// rule measures (two of every letter per dataset, rotated)
		for i, l := range letters {
			if (i+k)%3 != 0 {
				continue
			}
			thr := []string{"-d1", "-d20", "-d60"}[(i+k)%3]
			add(d.name, fmt.Sprintf("-tr %s -c0 -e%c %s", s, l, thr))
			add(d.name, fmt.Sprintf("-tr %s -c0 -e%c -d10 -i", s, l))
		}
		// item sets with rule measures: targets, aggregation modes, -i
		for i, l := range letters {
			if (i+2*k)%2 != 0 {
				continue
			}
			tg := []string{"-ts", "-tc", "-tm", "-tg"}[(i+k)%4]
			agg := []string{"-ax", "-am", "-an", "-aa"}[(i/2+k)%4]
			add(d.name, fmt.Sprintf("%s %s -e%c -d5 %s", tg, s, l, agg))
		}
		for _, o := range []string{"-ts -el -d110", "-tc -el -d110 -n2", "-tm -el -d110 -n2",
			"-tg -el -d110 -m2", "-ts -ez -d0 -i -aa", "-tc -ec -d50 -m0", "-tm -ea -d10 -As"} {
			add(d.name, o[:3]+" "+s+o[3:])
		}
		// deviations: R1 (-eb), R8 (-S), R2/R10 (-p)
		add(d.name, "-tr "+s+" -c0 -eb -d0")
		add(d.name, "-tr "+s+" -c0 -S60")
		add(d.name, "-tr "+s+" -c0 -S-3 -o")
		for _, o := range []string{"-ts -el -d120 -p-2", "-ts -el -d120 -p0", "-ts -el -d120 -p1",
			"-tc -el -d120 -p0", "-tm -el -d120 -p-1", "-tg -el -d120 -p0"} {
			add(d.name, o[:3]+" "+s+o[3:])
		}
	}
	for _, d := range []ds{{"test1.tab", "-s-2"}, {"r09.tab", "-s-2"}} {
		add(d.name, "-tr "+d.supp+" -c0 -Rapp1.app")
		add(d.name, "-tr "+d.supp+" -c0 -Rapp2.app")
	}
	if err := os.WriteFile(filepath.Join(goldenDir, "tree_cases.txt"), []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
