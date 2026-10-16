package fpgrowth

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// testCase is a random dataset with random options in two notations:
// upstream's (percentages, negative absolute supports) for the internal
// miner and the oracle, and the public API's.
type testCase struct {
	seed uint64
	txs  []oracle.Transaction
	// upstream notation
	smin, smax float64
	border     []float64
	// public notation
	minSupp, maxSupp Support
	pubBorder        []Support

	zmin, zmax   int // zmax: fpg.MaxSize = no limit
	includeEmpty bool
	tuning       Tuning
}

// genTestCase draws a random dataset over at most maxItems items and
// random thresholds.
func genTestCase(seed uint64, maxItems int) (testCase, error) {
	r := rand.New(rand.NewPCG(seed, 0x51ed270b27f1a3c9))
	gc := oracle.GenConfig{
		Seed:            seed,
		NumTransactions: 1 + r.IntN(24),
		NumItems:        1 + r.IntN(maxItems),
		MinLen:          r.IntN(3),
		Skew:            r.IntN(3),
		Patterns:        r.IntN(3),
		PatternProb:     r.Float64() * 0.7,
		DupItemProb:     0.05,
		DupTxProb:       r.Float64() * 0.3,
		EmptyProb:       r.Float64() * 0.1,
		Weights:         oracle.WeightDist(r.IntN(3)),
		Names:           oracle.NamesLetters,
	}
	gc.MaxLen = gc.MinLen + 1 + r.IntN(6)
	gc.PatternLen = 1 + r.IntN(min(4, gc.NumItems))
	txs, err := oracle.Generate(gc)
	if err != nil {
		return testCase{}, err
	}
	c := testCase{seed: seed, txs: txs, smax: 100, zmin: 1, zmax: fpg.MaxSize}
	switch r.IntN(4) {
	case 0, 1:
		n := int64(1 + r.IntN(4))
		c.smin, c.minSupp = float64(-n), Count(n)
	default:
		k := 1 + r.IntN(60)
		c.smin, c.minSupp = float64(k), Fraction(float64(k)/100)
	}
	switch r.IntN(6) {
	case 0:
		n := int64(2 + r.IntN(10))
		c.smax, c.maxSupp = float64(-n), Count(n)
	case 1:
		k := 30 + r.IntN(71)
		c.smax, c.maxSupp = float64(k), Fraction(float64(k)/100)
	}
	switch r.IntN(5) {
	case 0:
		c.zmin, c.includeEmpty = 0, true
	case 1:
		c.zmin = 2 + r.IntN(2)
	}
	if r.IntN(3) == 0 {
		c.zmax = max(c.zmin, 1) + r.IntN(4)
	}
	if r.IntN(6) == 0 {
		for range 2 {
			if r.IntN(2) == 0 {
				n := int64(1 + r.IntN(4))
				c.border = append(c.border, float64(-n))
				c.pubBorder = append(c.pubBorder, Count(n))
			} else {
				k := 1 + r.IntN(50)
				c.border = append(c.border, float64(k))
				c.pubBorder = append(c.pubBorder, Fraction(float64(k)/100))
			}
		}
	}
	c.tuning = Tuning{
		NoPerfectExtensions: r.IntN(3) == 0,
		NoHeadUnionTail:     r.IntN(3) == 0,
		ItemOrder:           ItemOrder(r.IntN(6)),
	}
	return c, nil
}

// options returns the public options of c.
func (c testCase) options(target Target, algo Algorithm, tun Tuning) Options[string] {
	return Options[string]{
		Target:       target,
		MinSupport:   c.minSupp,
		MaxSupport:   c.maxSupp,
		MinSize:      map[bool]int{true: 0, false: c.zmin}[c.includeEmpty],
		MaxSize:      map[bool]int{true: 0, false: c.zmax}[c.zmax == fpg.MaxSize],
		IncludeEmpty: c.includeEmpty,
		Border:       c.pubBorder,
		Algorithm:    algo,
		Tuning:       tun,
	}
}

// algorithms are all public variants with the tuning that varies within
// a variant.
var algorithms = []struct {
	name string
	algo Algorithm
	tun  Tuning
}{
	{"auto", Auto, Tuning{}},
	{"s-l0", Simple, Tuning{ItemMachine: -1}},
	{"s", Simple, Tuning{}},
	{"c", Complex, Tuning{}},
	{"c-j", Complex, Tuning{NoReorder: true}},
	{"c-l0", Complex, Tuning{ItemMachine: -1}},
	{"d", SingleTree, Tuning{}},
	{"d-l4", SingleTree, Tuning{ItemMachine: 4}},
	{"t", TopDown, Tuning{}},
}

var targets = []struct {
	name   string
	target Target
	oracle oracle.Target
	cli    report.Target
}{
	{"frequent", Frequent, oracle.Frequent, report.TargetAll},
	{"closed", Closed, oracle.Closed, report.TargetClosed},
	{"maximal", Maximal, oracle.Maximal, report.TargetMaximal},
	{"generators", Generators, oracle.Generators, report.TargetGenerators},
}

// merge combines a variant's tuning with the case's tuning flags.
func merge(variant, c Tuning) Tuning {
	t := variant
	t.NoPerfectExtensions = t.NoPerfectExtensions || c.NoPerfectExtensions
	t.NoHeadUnionTail = t.NoHeadUnionTail || c.NoHeadUnionTail
	t.ItemOrder = c.ItemOrder
	return t
}

// cliConfig builds the internal configuration the way the command line
// program does for the same settings (independently of plan).
func cliConfig(target report.Target, algo Algorithm, tun Tuning, smin, smax float64, zmin, zmax int) fpg.Config {
	c := fpg.DefaultConfig()
	c.Target = target
	c.SMin, c.SMax = smin, smax
	c.ZMin, c.ZMax = zmin, zmax
	c.Algo = map[Algorithm]fpg.Algorithm{
		Auto: fpg.AlgoAuto, Simple: fpg.AlgoSimple, Complex: fpg.AlgoComplex,
		SingleTree: fpg.AlgoSingle, TopDown: fpg.AlgoTopDown,
	}[algo]
	if tun.NoPerfectExtensions {
		c.Mode &^= fpg.ModePerfect
	}
	if tun.NoReorder {
		c.Mode &^= fpg.ModeReorder
	}
	if tun.NoHeadUnionTail {
		c.Mode &^= fpg.ModeTail
	}
	switch {
	case tun.ItemMachine < 0:
		c.Pack = 0
	case tun.ItemMachine > 0:
		c.Pack = tun.ItemMachine
	}
	c.Sort = map[ItemOrder]int{
		ItemOrderDefault: 2, ItemOrderSizeSumAscending: 2, ItemOrderFrequencyAscending: 1,
		ItemOrderNone: 0, ItemOrderFrequencyDescending: -1, ItemOrderSizeSumDescending: -2,
	}[tun.ItemOrder]
	return c
}

// cliBorder is the command line program's -F conversion (setbdr).
func cliBorder(v float64, w tract.Supp) tract.Supp {
	s := -v
	if v >= 0 {
		s = v / 100.0 * float64(w) * (1 - 0x1p-52)
	}
	return tract.Supp(math.Ceil(s))
}

// scanOf returns an internal scan over txs.
func scanOf(txs []oracle.Transaction) tract.ScanFunc[string] {
	return func(yield func([]string, tract.Supp) bool) error {
		for _, tx := range txs {
			if !yield(tx.Items, tx.Weight) {
				return nil
			}
		}
		return nil
	}
}

// sourceOf returns a public source over txs.
func sourceOf(txs []oracle.Transaction) Source[string] {
	pub := make([]Transaction[string], len(txs))
	for i, tx := range txs {
		pub[i] = Transaction[string]{Items: tx.Items, Weight: int64(tx.Weight)}
	}
	return FromSlice(pub)
}

// record is one reported item set or rule in a comparable form.
type record struct {
	text string
	eval float64
}

func (r record) String() string { return fmt.Sprintf("%s eval=%v", r.text, r.eval) }

// internalSink collects the results of the internal miner.
type internalSink struct {
	ib   *tract.ItemBase[string]
	recs []record
}

func (s *internalSink) names(items []tract.Item) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = s.ib.ID(it)
	}
	return strings.Join(parts, " ")
}

func (s *internalSink) ItemSet(is report.ItemSet) error {
	s.recs = append(s.recs, record{text: fmt.Sprintf("{%s}:%d", s.names(is.Items), is.Supp), eval: is.Eval})
	return nil
}

func (s *internalSink) Rule(r report.Rule) error {
	s.recs = append(s.recs, record{
		text: fmt.Sprintf("%s <- {%s}:%d/%d/%d", s.ib.ID(r.Head), s.names(r.Body), r.Supp, r.BodySupp, r.HeadSupp),
		eval: r.Eval,
	})
	return nil
}

// runInternal runs the internal pipeline like the command line program
// and returns the results in report order and the reporter.
func runInternal(t *testing.T, txs []oracle.Transaction, cfg fpg.Config, border []float64,
	prepare func(ib *tract.ItemBase[string]) error) ([]record, *report.Reporter, error) {
	t.Helper()
	ib := tract.NewItemBase[string]()
	if prepare != nil {
		if err := prepare(ib); err != nil {
			t.Fatal(err)
		}
	}
	scan := scanOf(txs)
	m, err := fpg.New[string](cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ib.Count(scan); err != nil {
		return nil, nil, err
	}
	if ib.Len() == 0 || ib.Records() == 0 {
		return nil, nil, fpg.ErrNoItems
	}
	if err := m.Data(ib, scan); err != nil {
		return nil, nil, err
	}
	rep, err := m.Reporter()
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range border {
		rep.SetBorder(cfg.ZMin+i, cliBorder(v, ib.TotalWeight()))
	}
	sink := &internalSink{ib: ib}
	rep.SetSink(sink)
	err = m.Mine(context.Background())
	return sink.recs, rep, err
}

// setRecords mines with the public API and returns the records.
func setRecords(t *testing.T, src Source[string], opt Options[string]) ([]record, Summary, error) {
	t.Helper()
	var recs []record
	sum, err := MineItemsets(context.Background(), src, opt, func(s Itemset[string]) error {
		recs = append(recs, record{text: fmt.Sprintf("{%s}:%d", strings.Join(s.Items, " "), s.Support), eval: s.Value})
		return nil
	})
	return recs, sum, err
}

// ruleRecords mines rules with the public API and returns the records.
func ruleRecords(t *testing.T, src Source[string], opt RuleOptions[string]) ([]record, Summary, error) {
	t.Helper()
	var recs []record
	sum, err := MineRules(context.Background(), src, opt, func(r Rule[string]) error {
		recs = append(recs, record{
			text: fmt.Sprintf("%s <- {%s}:%d/%d/%d", r.Head, strings.Join(r.Body, " "), r.Support, r.BodySupport, r.HeadSupport),
			eval: r.Value,
		})
		return nil
	})
	return recs, sum, err
}

// diffRecords compares two result sequences including order; with
// evals, the evaluation values must be bit-identical.
func diffRecords(want, got []record, evals bool) string {
	n := max(len(want), len(got))
	for i := range n {
		var w, g record
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w.text != g.text || (evals && math.Float64bits(w.eval) != math.Float64bits(g.eval)) {
			return fmt.Sprintf("first difference at #%d of %d/%d: want %v, got %v", i, len(want), len(got), w, g)
		}
	}
	return ""
}

// toOracleSets converts public item sets.
func toOracleSets(sets []Itemset[string]) []oracle.Set {
	out := make([]oracle.Set, len(sets))
	for i, s := range sets {
		out[i] = oracle.Set{Items: oracle.NewItemSet(s.Items...), Supp: tract.Supp(s.Support), Eval: s.Value}
	}
	return out
}

// toOracleRules converts public rules.
func toOracleRules(rules []Rule[string]) []oracle.Rule {
	out := make([]oracle.Rule, len(rules))
	for i, r := range rules {
		out[i] = oracle.Rule{
			Head: r.Head, Body: oracle.NewItemSet(r.Body...),
			Supp: tract.Supp(r.Support), BodySupp: tract.Supp(r.BodySupport), HeadSupp: tract.Supp(r.HeadSupport),
			Conf: r.Confidence, Lift: r.Lift, Eval: r.Value,
		}
	}
	return out
}

// numCases returns n, or n/5 with -short or -race.
func numCases(n int) int {
	if testing.Short() || raceEnabled {
		return max(n/5, 1)
	}
	return n
}
