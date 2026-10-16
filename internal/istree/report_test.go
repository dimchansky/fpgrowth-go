package istree

import (
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

var thresholds = []float64{0, 0.05, 0.1, 0.5, 0.9, 1, 1.2, 3}

// randMeasure returns a random valid measure (None included if withNone).
func randMeasure(r *rand.Rand, withNone bool) ruleval.Measure {
	lo := 1
	if withNone {
		lo = 0
	}
	return ruleval.Measure(lo + r.IntN(int(ruleval.NumMeasures)-lo))
}

// oracleMeasure converts an evaluation to the oracle's form.
func oracleMeasure(e Eval) *oracle.Measure {
	if e.Measure == ruleval.None {
		return nil
	}
	return &oracle.Measure{
		Func:                    oracle.MeasureFunc(e.Measure.Func()),
		Dir:                     oracle.Direction(e.Measure.Dir()),
		Threshold:               e.Threshold,
		InvalidateBelowExpected: e.Invalidate,
	}
}

func oracleAppear(a tract.Appearance) oracle.Appearance {
	switch a {
	case tract.AppearBody:
		return oracle.AppearBody
	case tract.AppearHead:
		return oracle.AppearHead
	case tract.AppearNone:
		return oracle.AppearNone
	default:
		return oracle.AppearBoth
	}
}

func oracleMax(z int) int {
	if z >= report.MaxSize {
		return oracle.NoMaxSize
	}
	return z
}

func oracleTarget(tg report.Target) oracle.Target {
	switch tg {
	case report.TargetClosed:
		return oracle.Closed
	case report.TargetMaximal:
		return oracle.Maximal
	case report.TargetGenerators:
		return oracle.Generators
	default:
		return oracle.Frequent
	}
}

// oracleRules computes the expected rules of c.
func oracleRules(t *testing.T, d *dataset, c ruleConfig) []oracle.Rule {
	t.Helper()
	p := oracle.NewRuleParams(c.bodyMin, c.conf)
	p.OrigSupp = c.orig
	if c.smax > 0 && c.smax < report.MaxSupp {
		p.MaxSupp = c.smax
	}
	p.MinSize, p.MaxSize = c.zmin, oracleMax(c.zmax)
	if c.app != nil {
		p.Appear = map[string]oracle.Appearance{}
		for _, e := range c.app {
			p.Appear[e.item] = oracleAppear(e.app)
		}
	}
	p.Eval = oracleMeasure(c.eval)
	want, err := d.db.Rules(p)
	if err != nil {
		t.Fatal(err)
	}
	return want
}

// randRuleConfig draws random rule options; items are the item names.
func randRuleConfig(r *rand.Rand, items []string) ruleConfig {
	c := ruleConfig{
		bodyMin: tract.Supp(1 + r.IntN(5)),
		conf:    []float64{0, 0.3, 0.5, 0.8, 1}[r.IntN(5)],
		orig:    r.IntN(3) == 0,
		smax:    report.MaxSupp,
		zmin:    r.IntN(3),
		zmax:    []int{report.MaxSize, 1, 2, 3, 4}[r.IntN(5)],
		order:   r.IntN(5) - 2,
	}
	if r.IntN(3) == 0 {
		c.smax = tract.Supp(2 + r.IntN(12))
	}
	if r.IntN(2) == 0 {
		c.eval = Eval{
			Measure:    randMeasure(r, false),
			Threshold:  thresholds[r.IntN(len(thresholds))],
			Invalidate: r.IntN(3) == 0,
		}
	}
	if r.IntN(3) == 0 {
		c.app = []appEntry{}
		for _, it := range items {
			if r.IntN(2) == 0 {
				c.app = append(c.app, appEntry{item: it, app: tract.Appearance(r.IntN(4))})
			}
		}
	}
	return c
}

func itemNames(txs []oracle.Transaction) []string {
	var names []string
	for _, tx := range txs {
		for _, it := range tx.Items {
			if !slices.Contains(names, it) {
				names = append(names, it)
			}
		}
	}
	slices.Sort(names)
	return names
}

// TestReport_RulesOracle compares the reported rules (head, body, all three
// supports and the measure value) with the brute-force definition,
// including appearances (B3 fixed), -o, -S (B8 fixed), confidence, size
// limits, every ruleval measure (Imbalance for 'b', B1) and -i.
func TestReport_RulesOracle(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	n := 600
	if testing.Short() {
		n = 100
	}
	total, evaluated := 0, 0
	defer func() { t.Logf("%d rules compared (%d with a measure)", total, evaluated) }()
	for seed := range uint64(n) {
		txs := genTxs(t, seed, 4+int(seed%7))
		c := randRuleConfig(r, itemNames(txs))
		got, d := runRules(t, txs, c, false)
		if len(d.names) == 0 {
			continue
		}
		want := oracleRules(t, d, c)
		total += len(want)
		if c.eval.Measure != ruleval.None {
			evaluated += len(want)
		}
		oracle.SortRules(got.rules)
		if diff := oracle.DiffRules(want, got.rules, oracle.DiffOptions{}); diff != "" {
			t.Fatalf("seed %d config %+v:\n%s", seed, c, diff)
		}
	}
}

// TestReport_SetsOracle compares item sets evaluated with a rule measure
// (all targets, aggregation modes, -i) with the brute-force definition.
func TestReport_SetsOracle(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	targets := []report.Target{report.TargetAll, report.TargetClosed, report.TargetMaximal, report.TargetGenerators}
	n := 800
	if testing.Short() {
		n = 100
	}
	total := 0
	defer func() { t.Logf("%d item sets compared", total) }()
	for seed := range uint64(n) {
		txs := genTxs(t, seed, 4+int(seed%8))
		c := setConfig{
			target: targets[r.IntN(len(targets))],
			smin:   tract.Supp(1 + r.IntN(5)),
			zmin:   r.IntN(3),
			zmax:   []int{report.MaxSize, 1, 2, 3}[r.IntN(4)],
			prune:  noPrune,
			order:  r.IntN(5) - 2,
			eval: Eval{
				Measure:    randMeasure(r, true),
				Agg:        Aggregation(r.IntN(4)),
				Invalidate: r.IntN(3) == 0,
				Threshold:  thresholds[r.IntN(len(thresholds))],
			},
		}
		if r.IntN(4) == 0 {
			c.smax = tract.Supp(2 + r.IntN(12))
		}
		got, d := runSets(t, txs, c, false)
		if len(d.names) == 0 {
			continue
		}
		p := oracle.NewSetParams(oracleTarget(c.target), c.smin)
		p.MinSize, p.MaxSize = c.zmin, oracleMax(c.zmax)
		if c.smax > 0 {
			p.MaxSupp = c.smax
		}
		if m := oracleMeasure(c.eval); m != nil {
			p.Eval = &oracle.SetEval{Measure: *m, Agg: oracle.Aggregation(c.eval.Agg), Order: d.names}
		}
		want, err := d.db.Mine(p)
		if err != nil {
			t.Fatal(err)
		}
		total += len(want)
		if diff := oracle.DiffSets(want, got.sets, oracle.DiffOptions{}); diff != "" {
			t.Fatalf("seed %d config %+v codes %v:\n%s", seed, c, d.names, diff)
		}
	}
}

// TestReport_RuleOrder pins the rule order of rules-eval.md §3.2 on t1.tab
// (codes d=0 c=1 b=2 a=3 e=4 with -q1 coding).
func TestReport_RuleOrder(t *testing.T) {
	c := ruleConfig{bodyMin: 2, conf: 0.5, smax: report.MaxSupp, zmin: 1, zmax: report.MaxSize, order: 1}
	got, d := runRules(t, t1, c, false)
	if strings.Join(d.names, "") != "dcbae" {
		t.Fatalf("codes %v, want d c b a e", d.names)
	}
	var heads []string
	for _, ru := range got.rules[:12] {
		heads = append(heads, ru.Head+" <- "+strings.Join(ru.Body, " "))
	}
	want := []string{"d <- ", "d <- c", "c <- d", "c <- ", "d <- b", "b <- d", "d <- b c",
		"c <- b d", "b <- c d", "c <- b", "b <- c", "b <- "}
	if !slices.Equal(heads, want) {
		t.Errorf("rule order\n got %q\nwant %q", heads, want)
	}
	if len(got.rules) != 33 {
		t.Errorf("%d rules, want 33 (rules-eval.md B1 reproducer)", len(got.rules))
	}
}

// TestReport_HeadOnlyB3 is the regression test for B3: with "a" head-only,
// upstream reports "c <- a d", "b <- a d", "c <- a b d", "b <- a c d" and
// "b <- a c"; here no rule has a head-only item in its body.
func TestReport_HeadOnlyB3(t *testing.T) {
	app := []appEntry{{item: "a", app: tract.AppearHead}}
	c := ruleConfig{bodyMin: 2, conf: 0.5, smax: report.MaxSupp, zmin: 1, zmax: report.MaxSize, order: 1, app: app}
	got, d := runRules(t, t1, c, false)
	if len(got.rules) == 0 {
		t.Fatal("no rules")
	}
	sawHead := false
	for _, ru := range got.rules {
		if ru.Body.Contains("a") {
			t.Errorf("head-only item a in the body of %v", ru)
		}
		sawHead = sawHead || ru.Head == "a"
	}
	if !sawHead {
		t.Error("no rule with head a")
	}
	// body-only and ignored items
	app = []appEntry{{item: "a", app: tract.AppearBody}, {item: "e", app: tract.AppearNone}, {item: "b", app: tract.AppearHead}}
	c.app = app
	got, _ = runRules(t, t1, c, false)
	for _, ru := range got.rules {
		if ru.Head == "a" || ru.Head == "e" || ru.Body.Contains("e") || ru.Body.Contains("b") {
			t.Errorf("appearance violated by %v", ru)
		}
	}
	if diff := oracle.DiffRules(oracleRules(t, d, c), got.rules, oracle.DiffOptions{}); diff != "" {
		t.Error(diff)
	}
}

// TestReport_MaxSuppB8 is the regression test for B8: "-tr -s20 -S50 -c50"
// on t1 upstream still reports "d <- c" (body support 7 > 5) and drops the
// empty-body rules only because of their set support; here -S bounds the
// body support (and the set support with -o).
func TestReport_MaxSuppB8(t *testing.T) {
	c := ruleConfig{bodyMin: 2, conf: 0.5, smax: 5, zmin: 1, zmax: report.MaxSize, order: 1}
	got, d := runRules(t, t1, c, false)
	if len(got.rules) == 0 {
		t.Fatal("no rules")
	}
	for _, ru := range got.rules {
		if ru.BodySupp > 5 {
			t.Errorf("rule %v exceeds -S on the body support", ru)
		}
	}
	if diff := oracle.DiffRules(oracleRules(t, d, c), got.rules, oracle.DiffOptions{}); diff != "" {
		t.Error(diff)
	}
	c.orig = true
	got, d = runRules(t, t1, c, false)
	sawLargeBody := false
	for _, ru := range got.rules {
		if ru.Supp > 5 {
			t.Errorf("rule %v exceeds -S on the set support (-o)", ru)
		}
		sawLargeBody = sawLargeBody || ru.BodySupp > 5
	}
	if !sawLargeBody {
		t.Error("with -o the body support must not be bounded")
	}
	if diff := oracle.DiffRules(oracleRules(t, d, c), got.rules, oracle.DiffOptions{}); diff != "" {
		t.Error(diff)
	}
}

// TestReport_ImbalanceB1 checks that rules evaluated with ruleval.Imbalance
// (fpgrowth letter 'b' for rules after the B1 fix) carry the measure value
// and are filtered by it; upstream evaluates them to 0 and ignores -d.
func TestReport_ImbalanceB1(t *testing.T) {
	c := ruleConfig{bodyMin: 2, conf: 0.5, smax: report.MaxSupp, zmin: 1, zmax: report.MaxSize, order: 1,
		eval: Eval{Measure: ruleval.Imbalance, Threshold: 0.3}}
	got, d := runRules(t, t1, c, false)
	all, _ := runRules(t, t1, ruleConfig{bodyMin: 2, conf: 0.5, smax: report.MaxSupp, zmin: 1, zmax: report.MaxSize, order: 1}, false)
	if len(got.rules) == 0 || len(got.rules) >= len(all.rules) {
		t.Fatalf("%d rules with -eb -d30, %d without", len(got.rules), len(all.rules))
	}
	for _, ru := range got.rules {
		if want := ruleval.Eval(ruleval.Imbalance, ru.Supp, ru.BodySupp, ru.HeadSupp, d.total); ru.Eval != want || ru.Eval < 0.3 {
			t.Errorf("%v: eval %v, want %v >= 0.3", ru, ru.Eval, want)
		}
	}
	if diff := oracle.DiffRules(oracleRules(t, d, c), got.rules, oracle.DiffOptions{}); diff != "" {
		t.Error(diff)
	}
}

// TestInit_LargeZmin is the regression test for ist_init's read past the
// level array when zmin exceeds the number of items: nothing is reported
// and nothing panics.
func TestInit_LargeZmin(t *testing.T) {
	for _, m := range []ruleval.Measure{ruleval.None, ruleval.Lift, ruleval.Chi2PValue} {
		c := setConfig{target: report.TargetAll, smin: 2, zmin: 50, zmax: report.MaxSize, prune: noPrune, order: 1,
			eval: Eval{Measure: m, Threshold: 0}}
		got, _ := runSets(t, t1, c, false)
		if len(got.sets) != 0 {
			t.Errorf("measure %v: %d sets with zmin 50", m, len(got.sets))
		}
	}
}

// TestReport_EmptySet checks the empty set and singleton constants: with a
// (+) measure and threshold 0 they are reported with value 0; with a (-)
// measure they get 1 and pass only a threshold of at least 1.
func TestReport_EmptySet(t *testing.T) {
	c := setConfig{target: report.TargetAll, smin: 2, zmin: 0, zmax: 1, prune: noPrune, order: 1,
		eval: Eval{Measure: ruleval.Lift, Threshold: 0}}
	got, _ := runSets(t, t1, c, false)
	if len(got.sets) != 6 || len(got.sets[0].Items) != 0 || got.sets[0].Supp != 10 {
		t.Errorf("sets %v, want the empty set first and 5 singletons", got.sets)
	}
	c.eval = Eval{Measure: ruleval.Chi2PValue, Threshold: 0.99}
	if got, _ := runSets(t, t1, c, false); len(got.sets) != 0 {
		t.Errorf("(-) measure with threshold 0.99: %v", got.sets)
	}
	c.eval.Threshold = 1
	if got, _ := runSets(t, t1, c, false); len(got.sets) != 6 || got.sets[0].Eval != 1 {
		t.Errorf("(-) measure with threshold 1: %v", got.sets)
	}
}

// errSink fails after n records.
type errSink struct{ n int }

var errStop = errors.New("stop")

func (s *errSink) ItemSet(report.ItemSet) error { return s.step() }
func (s *errSink) Rule(report.Rule) error       { return s.step() }
func (s *errSink) step() error {
	s.n--
	if s.n < 0 {
		return errStop
	}
	return nil
}

// TestReport_SinkError checks that a sink error deep in the recursion
// aborts the report and is returned (upstream loses it).
func TestReport_SinkError(t *testing.T) {
	d := newDataset(t, t1, 2, 1, nil)
	for _, target := range []report.Target{report.TargetAll, report.TargetRules} {
		for n := range 8 {
			tr := New(Params{Freqs: d.freqs, Total: d.total, MinSupp: 2, MinBody: 2, MinConf: 0.5})
			tr.SetSize(1, d.maxLen)
			fillRecTree(tr, d.supp, 2)
			tr.Init()
			rep := report.New(len(d.names), d.total, d.freqs)
			rep.SetTarget(target, report.ModeNoFilter, -1)
			rep.SetSupport(2, report.MaxSupp)
			rep.Setup()
			s := &errSink{n: n}
			rep.SetSink(s)
			if err := tr.Report(rep, target); !errors.Is(err, errStop) {
				t.Errorf("target %d, error after %d: got %v", target, n, err)
			}
			if rep.Len() != 0 && s.n >= 0 {
				t.Errorf("reporter not empty")
			}
		}
	}
}

// BenchmarkReport measures reporting from a mid-size tree (rules and
// evaluated item sets); the tree is filled once.
func BenchmarkReport(b *testing.B) {
	txs, err := oracle.Generate(oracle.GenConfig{
		Seed: 7, NumTransactions: 2000, NumItems: 18, MinLen: 2, MaxLen: 9, Skew: 1,
		Patterns: 6, PatternLen: 5, PatternProb: 0.5,
	})
	if err != nil {
		b.Fatal(err)
	}
	d := newDataset(b, txs, 40, 1, nil)
	for _, bc := range []struct {
		name   string
		target report.Target
		eval   Eval
	}{
		{name: "rules", target: report.TargetRules},
		{name: "rules lift", target: report.TargetRules, eval: Eval{Measure: ruleval.Lift, Threshold: 1.1}},
		{name: "sets lift avg", target: report.TargetAll, eval: Eval{Measure: ruleval.Lift, Agg: AggAvg, Threshold: 1}},
		{name: "closed chi2 min", target: report.TargetClosed, eval: Eval{Measure: ruleval.Chi2, Agg: AggMin, Threshold: 0.01}},
	} {
		b.Run(bc.name, func(b *testing.B) {
			tr := New(Params{Freqs: d.freqs, Total: d.total, MinSupp: 40, MinBody: 40, MinConf: 0.6})
			tr.SetSize(1, d.maxLen)
			fillRecTree(tr, d.supp, 40)
			tr.SetEval(bc.eval)
			if bc.target == report.TargetClosed {
				tr.CloMax(bc.target, false)
			}
			tr.Init()
			rep := report.New(len(d.names), d.total, d.freqs)
			rep.SetTarget(bc.target, report.ModeNoFilter, -1)
			rep.SetSupport(40, report.MaxSupp)
			rep.Setup()
			rep.SetSink(&errSink{n: 1 << 60})
			b.ReportAllocs()
			for b.Loop() {
				if err := tr.Report(rep, bc.target); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(rep.Count())/float64(b.N), "records/op")
		})
	}
}
