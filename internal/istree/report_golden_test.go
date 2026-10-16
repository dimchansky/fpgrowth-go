package istree

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// The golden files under testdata/golden are produced by the C reference
// (reference/harness/istree/gen.sh) for the cases in testdata/cases.txt:
// the item sets and rules of the fpgrowth program on the item set tree
// path in output order, with supports and measure values printed with 17
// significant digits. Cases of build "p" come from a C build with the
// corrected -p call order (reference/patches/istree/fpgrowth-p-order.patch).
// The Go side fills the tree from brute-force supports in rec_tree's order
// and must reproduce every line in the same order.

// TestGolden_WriteData regenerates the random datasets of the golden
// cases (testdata/data/r*.tab) when ISTREE_UPDATE_DATA is set; then run
// reference/harness/istree/gen.sh to regenerate the golden outputs.
func TestGolden_WriteData(t *testing.T) {
	if os.Getenv("ISTREE_UPDATE_DATA") == "" {
		t.Skip("set ISTREE_UPDATE_DATA=1 to regenerate testdata/data/r*.tab")
	}
	for i, cfg := range []oracle.GenConfig{
		{Seed: 101, NumTransactions: 60, NumItems: 8, MinLen: 1, MaxLen: 5, Skew: 1, Patterns: 2, PatternLen: 3, PatternProb: 0.4},
		{Seed: 102, NumTransactions: 120, NumItems: 10, MinLen: 2, MaxLen: 6, Skew: 2, Patterns: 3, PatternLen: 4, PatternProb: 0.5, DupTxProb: 0.1},
		{Seed: 103, NumTransactions: 80, NumItems: 9, MinLen: 1, MaxLen: 6, Patterns: 2, PatternLen: 4, PatternProb: 0.6, Weights: oracle.WeightsSmall},
		{Seed: 104, NumTransactions: 150, NumItems: 12, MinLen: 2, MaxLen: 7, Skew: 1, Patterns: 4, PatternLen: 3, PatternProb: 0.5, EmptyProb: 0.05},
	} {
		txs, err := oracle.Generate(cfg)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join("testdata", "data", fmt.Sprintf("r%d.tab", i+1)))
		if err != nil {
			t.Fatal(err)
		}
		if err := oracle.WriteTable(f, txs, cfg.Weights != oracle.WeightsUnit); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// goldCase is one line of testdata/cases.txt.
type goldCase struct {
	name, build, data string
	args              []string
}

func readCases(t *testing.T) []goldCase {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "cases.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var cases []goldCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) == 0 || strings.HasPrefix(fl[0], "#") {
			continue
		}
		if len(fl) < 3 {
			t.Fatalf("bad case line %q", sc.Text())
		}
		cases = append(cases, goldCase{name: fl[0], build: fl[1], data: fl[2], args: fl[3:]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}

// readTab reads a dataset in upstream's default format (blank separated
// items, one record per line, the weight last with -w).
func readTab(t *testing.T, path string, weighted bool) []oracle.Transaction {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	txs := make([]oracle.Transaction, 0, len(lines))
	for _, l := range lines {
		f := strings.Fields(l)
		w := tract.Supp(1)
		if weighted {
			v, err := strconv.ParseInt(f[len(f)-1], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			w, f = tract.Supp(v), f[:len(f)-1]
		}
		txs = append(txs, oracle.Transaction{Items: f, Weight: w})
	}
	return txs
}

// readApp reads an appearance file: the default indicator first, then
// item/indicator records.
func readApp(t *testing.T, path string) []appEntry {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := []appEntry{}
	for i, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Fields(l)
		if i == 0 {
			if a, ok := tract.ParseAppearance(f[0]); !ok || a != tract.AppearBoth {
				t.Fatalf("%s: default appearance must be both", path)
			}
			continue
		}
		a, ok := tract.ParseAppearance(f[1])
		if !ok {
			t.Fatalf("%s: bad indicator %q", path, f[1])
		}
		app = append(app, appEntry{item: f[0], app: a})
	}
	return app
}

// goldOpts are the parsed fpgrowth options of a case.
type goldOpts struct {
	target         byte
	smin, smax     float64
	conf           float64
	zmin, zmax     int
	measure        byte
	agg            byte
	thresh         float64
	invalidate     bool
	orig, weighted bool
	sort           int
	prune          int
	appFile        string
}

func parseOpts(t *testing.T, args []string) goldOpts {
	t.Helper()
	o := goldOpts{target: 's', smin: 10, smax: 100, conf: 80, zmin: 1, zmax: report.MaxSize,
		measure: 'x', agg: 'x', thresh: 10, sort: 2, prune: noPrune}
	num := func(s string) float64 {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatalf("option value %q: %v", s, err)
		}
		return v
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) < 2 || a[0] != '-' {
			t.Fatalf("bad option %q", a)
		}
		v := a[2:]
		switch a[1] {
		case 't':
			o.target = v[0]
		case 's':
			o.smin = num(v)
		case 'S':
			o.smax = num(v)
		case 'c':
			o.conf = num(v)
		case 'm':
			o.zmin = int(num(v))
		case 'n':
			o.zmax = int(num(v))
		case 'e':
			o.measure = v[0]
		case 'a':
			o.agg = v[0]
		case 'd':
			o.thresh = num(v)
		case 'i':
			o.invalidate = true
		case 'o':
			o.orig = true
		case 'w':
			o.weighted = true
		case 'q':
			o.sort = int(num(v))
		case 'p':
			o.prune = int(num(v))
		case 'R':
			i++
			o.appFile = args[i]
		default:
			t.Fatalf("unsupported option %q", a)
		}
	}
	return o
}

// eval returns the tree evaluation of the options.
func (o goldOpts) eval(t *testing.T) Eval {
	t.Helper()
	m := ruleval.None
	if o.measure != 'x' {
		var ok bool
		if m, ok = ruleval.ByLetter(o.measure); !ok || o.measure == 'b' {
			t.Fatalf("measure %q not usable in golden cases", o.measure)
		}
	}
	agg := map[byte]Aggregation{'x': AggFirst, 'm': AggMin, 'n': AggMax, 'a': AggAvg}[o.agg]
	return Eval{Measure: m, Agg: agg, Invalidate: o.invalidate, Threshold: o.thresh / 100}
}

// goldRec is one output line: items (rules: head, "<-", body) and values.
type goldRec struct {
	items []string
	nums  []float64
}

func (r goldRec) String() string {
	return fmt.Sprintf("%s %v", strings.Join(r.items, " "), r.nums)
}

func readGolden(t *testing.T, path string, rules bool) []goldRec {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var recs []goldRec
	for l := range strings.Lines(string(b)) {
		l = strings.TrimSuffix(l, "\n")
		var items, f []string
		k := 2
		if rules {
			head, rest, ok := strings.Cut(l, " <- ")
			if !ok {
				t.Fatalf("%s: bad rule line %q", path, l)
			}
			items, f, k = []string{head, "<-"}, strings.Fields(rest), 4
		} else {
			f = strings.Fields(l)
		}
		if len(f) < k {
			t.Fatalf("%s: bad line %q", path, l)
		}
		rec := goldRec{items: append(items, f[:len(f)-k]...)}
		for _, s := range f[len(f)-k:] {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				t.Fatalf("%s: bad number in %q", path, l)
			}
			rec.nums = append(rec.nums, v)
		}
		recs = append(recs, rec)
	}
	return recs
}

// toRecs converts the collected output.
func toRecs(c *collect, rules bool) []goldRec {
	var recs []goldRec
	if rules {
		for _, r := range c.ordered {
			ru := r.(report.Rule)
			items := []string{c.names[ru.Head], "<-"}
			for _, it := range ru.Body {
				items = append(items, c.names[it])
			}
			recs = append(recs, goldRec{items: items, nums: []float64{
				float64(ru.Supp), float64(ru.BodySupp), float64(ru.HeadSupp), ru.Eval}})
		}
		return recs
	}
	for _, r := range c.ordered {
		s := r.(report.ItemSet)
		items := []string{}
		for _, it := range s.Items {
			items = append(items, c.names[it])
		}
		recs = append(recs, goldRec{items: items, nums: []float64{float64(s.Supp), s.Eval}})
	}
	return recs
}

// closeEnough compares measure values: exactly, or within a relative
// 1e-12 for measures that use log/exp (libm vs Go math differences).
func closeEnough(a, b float64, isTol bool) bool {
	if a == b || (math.IsNaN(a) && math.IsNaN(b)) {
		return true
	}
	if !isTol || math.IsInf(a, 0) || math.IsInf(b, 0) {
		return false
	}
	return math.Abs(a-b) <= 1e-12*max(1, math.Abs(a), math.Abs(b))
}

// TestGolden compares the output order and values with the C reference.
func TestGolden(t *testing.T) {
	cases := readCases(t)
	if len(cases) == 0 {
		t.Fatal("no golden cases")
	}
	lines := 0
	defer func() { t.Logf("%d cases, %d lines compared", len(cases), lines) }()
	for _, gc := range cases {
		t.Run(gc.name, func(t *testing.T) {
			o := parseOpts(t, gc.args)
			txs := readTab(t, filepath.Join("testdata", "data", gc.data), o.weighted)
			var app []appEntry
			if o.appFile != "" {
				app = readApp(t, filepath.Join("testdata", "data", o.appFile))
			}
			total := tract.Supp(0)
			for _, tx := range txs {
				total += tx.Weight
			}
			isRules := o.target == 'r'
			body := oracle.AbsMinSupp(o.smin, total)
			e := o.eval(t)
			var got *collect
			if isRules {
				c := ruleConfig{bodyMin: body, conf: o.conf / 100, orig: o.orig, smax: report.MaxSupp,
					zmin: o.zmin, zmax: o.zmax, eval: e, order: sign(o.sort), app: app}
				if o.smax < 100 {
					t.Fatal("-S with rules differs from upstream (B8); not a golden case")
				}
				got, _ = runRules(t, txs, c, false)
			} else {
				target := map[byte]report.Target{'s': report.TargetAll, 'f': report.TargetAll,
					'c': report.TargetClosed, 'm': report.TargetMaximal, 'g': report.TargetGenerators}[o.target]
				order := sign(o.sort)
				if target == report.TargetClosed || target == report.TargetMaximal {
					order = o.sort // fpg_create clears FPG_REORDER
				}
				if gc.build != "p" && o.prune != noPrune {
					t.Fatal("-p needs the patched build (B2)")
				}
				c := setConfig{target: target, smin: body, smax: oracle.AbsMaxSupp(o.smax, total),
					zmin: o.zmin, zmax: o.zmax, eval: e, prune: o.prune, order: order}
				got, _ = runSets(t, txs, c, false)
			}
			want := readGolden(t, filepath.Join("testdata", "golden", gc.name+".out"), isRules)
			have := toRecs(got, isRules)
			lines += len(want)
			isTol := strings.IndexByte("jptigfhms", o.measure) >= 0
			for i := range max(len(want), len(have)) {
				if i >= len(want) || i >= len(have) {
					t.Fatalf("line %d: %d lines from C, %d from Go", i+1, len(want), len(have))
				}
				w, h := want[i], have[i]
				ok := strings.Join(w.items, " ") == strings.Join(h.items, " ") && len(w.nums) == len(h.nums)
				for k := 0; ok && k < len(w.nums); k++ {
					ok = closeEnough(w.nums[k], h.nums[k], isTol && k == len(w.nums)-1)
				}
				if !ok {
					t.Fatalf("line %d:\n   C: %v\n  Go: %v", i+1, w, h)
				}
			}
		})
	}
}

func sign(x int) int {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return 0
}
