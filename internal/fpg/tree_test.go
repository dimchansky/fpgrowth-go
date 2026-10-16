package fpg

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/istree"
	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// treeSink records reported rules and item sets with item names, in
// report order.
type treeSink struct {
	ib    *tract.ItemBase[string]
	rules []oracle.Rule
	sets  []oracle.Set
	keys  []string // report order, for order comparisons
}

func (s *treeSink) names(items []tract.Item) []string {
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = s.ib.ID(it)
	}
	return names
}

func (s *treeSink) ItemSet(is report.ItemSet) error {
	names := s.names(is.Items)
	s.sets = append(s.sets, oracle.Set{Items: oracle.NewItemSet(names...), Supp: is.Supp, Eval: is.Eval})
	s.keys = append(s.keys, fmt.Sprintf("%v %d %v", names, is.Supp, is.Eval))
	return nil
}

func (s *treeSink) Rule(r report.Rule) error {
	body := s.names(r.Body)
	head := s.ib.ID(r.Head)
	s.rules = append(s.rules, oracle.Rule{
		Head: head, Body: oracle.NewItemSet(body...),
		Supp: r.Supp, BodySupp: r.BodySupp, HeadSupp: r.HeadSupp, Eval: r.Eval,
	})
	s.keys = append(s.keys, fmt.Sprintf("%s <- %v %d %d %d %v", head, body, r.Supp, r.BodySupp, r.HeadSupp, r.Eval))
	return nil
}

// appSpec is an item appearance file: a default and per-item indicators.
type appSpec struct {
	def   tract.Appearance
	items []string
	apps  []tract.Appearance
}

// runTree mines txs with cfg (which must take the rules path) and returns
// the sink and the miner.
func runTree(t *testing.T, txs []oracle.Transaction, cfg Config, app *appSpec) (*treeSink, *Miner[string], error) {
	t.Helper()
	ib := tract.NewItemBase[string]()
	if app != nil {
		ib.SetDefaultAppearance(app.def)
		for i, it := range app.items {
			if err := ib.AddAppearance(it, app.apps[i]); err != nil {
				t.Fatal(err)
			}
		}
	}
	m, err := New[string](cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !m.UsesTree() {
		t.Fatalf("config %+v does not use the item set tree", cfg)
	}
	scan := scanOf(txs)
	if err := ib.Count(scan); err != nil {
		t.Fatal(err)
	}
	sink := &treeSink{ib: ib}
	if err := m.Data(ib, scan); err != nil {
		return sink, m, err
	}
	rep, err := m.Reporter()
	if err != nil {
		t.Fatal(err)
	}
	rep.SetSink(sink)
	return sink, m, m.Mine(context.Background())
}

// treeVariants are the -A settings; the rules path ignores the variant
// except through the item coding of -Ac with reordering.
var treeVariants = []variant{
	{name: "s", algo: AlgoSimple, pack: 16},
	{name: "c", algo: AlgoComplex, pack: 16},
	{name: "c-j", algo: AlgoComplex, pack: 16, clear: ModeReorder},
	{name: "d", algo: AlgoSingle, pack: 16},
	{name: "t", algo: AlgoTopDown, pack: 0},
}

// oracleAppear converts an item appearance.
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

// oracleMeasure returns the oracle form of the measure selected by cfg
// (after normalization), or nil.
func oracleMeasure(cfg Config) *oracle.Measure {
	n := cfg
	n.normalize()
	if !isRuleEval(n.Eval) {
		return nil
	}
	m := ruleval.Measure(n.Eval)
	return &oracle.Measure{
		Func:                    oracle.MeasureFunc(m.Func()),
		Dir:                     oracle.Direction(m.Dir()),
		Threshold:               n.Thresh / 100,
		InvalidateBelowExpected: n.InvalidateBelowExpected,
	}
}

// randEval draws an -e code: none, -eb (EvalLogRatio) or any rule measure.
func randEval(r *rand.Rand) int {
	switch r.IntN(6) {
	case 0:
		return EvalNone
	case 1:
		return EvalLogRatio
	default:
		return 1 + r.IntN(int(ruleval.NumMeasures)-1)
	}
}

var treeThresholds = []float64{-100, 0, 1, 10, 50, 90, 100, 150, 1000}

// TestTreeRulesOracle compares the association rules of every -A letter
// with the brute-force definition on random data, with random supports,
// confidence, sizes, -o, -S (B8 fixed), appearances (B3 fixed), every
// measure including 'b' (R1: imbalance ratio) and -i.
func TestTreeRulesOracle(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(7, 11))
	n := 1000
	if testing.Short() {
		n = 200
	}
	rules, runs := 0, 0
	for seed := range uint64(n) {
		c, err := genCase(seed+5000, 8)
		if err != nil {
			t.Fatal(err)
		}
		cfg := DefaultConfig()
		cfg.Target = report.TargetRules
		cfg.SMin = c.smin
		cfg.SMax = []float64{100, 100, 60, -3, -1, 0}[r.IntN(6)]
		cfg.Conf = []float64{0, 30, 50, 80, 100}[r.IntN(5)]
		cfg.ZMin = r.IntN(3)
		cfg.ZMax = []int{MaxSize, 1, 2, 3}[r.IntN(4)]
		cfg.Sort = c.sort
		if r.IntN(3) == 0 {
			cfg.Mode |= ModeOrigSupp
		}
		cfg.Eval = randEval(r)
		cfg.Thresh = treeThresholds[r.IntN(len(treeThresholds))]
		cfg.InvalidateBelowExpected = r.IntN(4) == 0
		if r.IntN(3) == 0 {
			cfg.Prune = r.IntN(5) - 2 // no effect on rules
		}
		db, err := oracle.New(c.txs, oracle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		var app *appSpec
		p := oracle.NewRuleParams(oracle.EffectiveMinSupp(oracle.AbsMinSupp(cfg.SMin, db.Total())), cfg.Conf/100)
		if r.IntN(3) == 0 {
			app = &appSpec{def: tract.Appearance(r.IntN(4))}
			p.DefaultAppear = oracleAppear(app.def)
			p.Appear = map[string]oracle.Appearance{}
			for _, it := range db.Items() {
				if r.IntN(2) == 0 {
					a := tract.Appearance(r.IntN(4))
					app.items = append(app.items, it)
					app.apps = append(app.apps, a)
					p.Appear[it] = oracleAppear(a)
				}
			}
		}
		p.OrigSupp = cfg.Mode&ModeOrigSupp != 0
		p.MaxSupp = oracle.AbsMaxSupp(cfg.SMax, db.Total())
		p.MinSize = cfg.ZMin
		if cfg.ZMax < MaxSize {
			p.MaxSize = cfg.ZMax
		}
		p.Eval = oracleMeasure(cfg)
		want, err := db.Rules(p)
		if err != nil {
			t.Fatal(err)
		}
		rules += len(want)
		for _, v := range treeVariants {
			vc := cfg
			vc.Algo, vc.Pack = v.algo, v.pack
			vc.Mode &^= v.clear
			sink, _, err := runTree(t, c.txs, vc, app)
			runs++
			switch {
			case errors.Is(err, ErrNoItems):
				if len(want) > 0 {
					t.Fatalf("seed %d %s: ErrNoItems, oracle has %d rules", seed, v.name, len(want))
				}
			case err != nil:
				t.Fatalf("seed %d %s: %v", seed, v.name, err)
			default:
				if d := oracle.DiffRules(want, sink.rules, oracle.DiffOptions{}); d != "" {
					t.Fatalf("seed %d %s config %+v app %+v:\n%s", seed, v.name, vc, app, d)
				}
			}
		}
	}
	t.Logf("%d runs, %d expected rules", runs, rules)
}

// TestTreeSetsOracle compares item sets evaluated with a rule measure
// (all targets, -A letters, aggregation modes, -i, -S) with the
// brute-force definition.
func TestTreeSetsOracle(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(13, 17))
	n := 1000
	if testing.Short() {
		n = 200
	}
	sets, runs := 0, 0
	for seed := range uint64(n) {
		c, err := genCase(seed+9000, 8)
		if err != nil {
			t.Fatal(err)
		}
		tg := targets[r.IntN(len(targets))]
		cfg := c.config(treeVariants[0], tg.target)
		cfg.Eval = 1 + r.IntN(int(ruleval.NumMeasures)-1)
		cfg.Agg = r.IntN(4)
		cfg.Thresh = treeThresholds[r.IntN(len(treeThresholds))]
		cfg.InvalidateBelowExpected = r.IntN(4) == 0
		if r.IntN(4) == 0 {
			cfg.Prune = 0 // backward pruning: only the evaluation itself
			if tg.target != report.TargetAll {
				cfg.Prune = NoPrune // safe filtering is not modeled by the oracle
			}
		}
		db, err := oracle.New(c.txs, oracle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		p := oracle.NewSetParams(tg.oracle, oracle.AbsMinSupp(cfg.SMin, db.Total()))
		p.MaxSupp = oracle.AbsMaxSupp(cfg.SMax, db.Total())
		p.MinSize = cfg.ZMin
		if cfg.ZMax < MaxSize {
			p.MaxSize = cfg.ZMax
		}
		for _, v := range treeVariants {
			vc := cfg
			vc.Algo, vc.Pack = v.algo, v.pack
			vc.Mode &^= v.clear
			sink, m, err := runTree(t, c.txs, vc, nil)
			runs++
			if errors.Is(err, ErrNoItems) {
				continue // checked by TestOracle for the unevaluated sets
			}
			if err != nil {
				t.Fatalf("seed %d %s: %v", seed, v.name, err)
			}
			order := make([]string, m.ib.Len())
			for i := range order {
				order[i] = m.ib.ID(tract.Item(i))
			}
			pe := p
			pe.Eval = &oracle.SetEval{Measure: *oracleMeasure(vc), Agg: oracle.Aggregation(vc.Agg), Order: order}
			want, err := db.Mine(pe)
			if err != nil {
				t.Fatal(err)
			}
			sets += len(want)
			if d := oracle.DiffSets(want, sink.sets, oracle.DiffOptions{}); d != "" {
				t.Fatalf("seed %d %s/%s config %+v codes %v:\n%s", seed, tg.name, v.name, vc, order, d)
			}
		}
	}
	t.Logf("%d runs, %d expected item sets", runs, sets)
}

// TestTreeVariantOrder checks the output order relations of the rules
// path (fpgrowth-core.md §5.3): -As, -Ad, -At and -Ac -j give identical
// output, and -Ac (reordering kept, which only changes the item coding)
// equals -As -q1; closed sets clear the reordering, so there -Ac equals
// -As.
func TestTreeVariantOrder(t *testing.T) {
	t.Parallel()
	for seed := range uint64(40) {
		c, err := genCase(seed+700, 9)
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range []report.Target{report.TargetRules, report.TargetAll, report.TargetClosed} {
			base := DefaultConfig()
			base.Target = tg
			base.SMin = -1
			base.Conf = 0
			base.Eval = int(ruleval.Lift)
			base.Thresh = 0
			var ref []string
			for _, v := range []variant{
				{name: "s", algo: AlgoSimple}, {name: "d", algo: AlgoSingle},
				{name: "t", algo: AlgoTopDown}, {name: "c-j", algo: AlgoComplex, clear: ModeReorder},
			} {
				cfg := base
				cfg.Algo = v.algo
				cfg.Mode &^= v.clear
				sink, _, err := runTree(t, c.txs, cfg, nil)
				if errors.Is(err, ErrNoItems) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if ref == nil {
					ref = sink.keys
				} else if !slices.Equal(ref, sink.keys) {
					t.Errorf("seed %d target %d: -A%s order differs from -As", seed, tg, v.name)
				}
			}
			if ref == nil {
				continue
			}
			cfg := base
			cfg.Algo = AlgoComplex
			ac, _, err := runTree(t, c.txs, cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Algo = AlgoSimple
			if tg != report.TargetClosed { // closed sets clear the reordering
				cfg.Sort = 1
			}
			sq, _, err := runTree(t, c.txs, cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ac.keys, sq.keys) {
				t.Errorf("seed %d target %d: -Ac differs from -As -q1", seed, tg)
			}
		}
	}
}

// TestTreeImbalanceR1 pins deviation R1: for rules, -eb (EvalLogRatio)
// evaluates the imbalance ratio; for item sets it stays the reporter's log
// support quotient and does not use the item set tree.
func TestTreeImbalanceR1(t *testing.T) {
	t.Parallel()
	txs := txsOf(t, "a b c\na d e\nb c d\na b c d\nb c\na b d\nd e\na b c d\nc d e\na b c\n", false)
	cfg := DefaultConfig()
	cfg.Target = report.TargetRules
	cfg.Eval, cfg.Thresh = EvalLogRatio, 0
	sink, m, err := runTree(t, txs, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Config().Eval; got != int(ruleval.Imbalance) {
		t.Fatalf("normalized eval %d, want Imbalance", got)
	}
	nonzero := 0
	for _, r := range sink.rules {
		want := ruleval.Eval(ruleval.Imbalance, r.Supp, r.BodySupp, r.HeadSupp, 10)
		if r.Eval != want {
			t.Errorf("%v: eval %v, want %v", r, r.Eval, want)
		}
		if r.Eval != 0 {
			nonzero++
		}
	}
	if len(sink.rules) == 0 || nonzero == 0 {
		t.Errorf("%d rules, %d with a non-zero imbalance ratio", len(sink.rules), nonzero)
	}
	sets := DefaultConfig()
	sets.Eval = EvalLogRatio
	ms, err := New[string](sets)
	if err != nil {
		t.Fatal(err)
	}
	if ms.UsesTree() || ms.Config().Eval != EvalLogRatio {
		t.Errorf("item sets with -eb: tree %v eval %d", ms.UsesTree(), ms.Config().Eval)
	}
}

// TestTreePruneR2 pins deviation R2: the evaluation filter (-p) runs after
// the measure is set, so weak forward pruning removes item sets whose
// subsets all fail the evaluation, while upstream's -p has no effect.
func TestTreePruneR2(t *testing.T) {
	t.Parallel()
	for seed := range uint64(200) {
		c, err := genCase(seed+300, 8)
		if err != nil {
			t.Fatal(err)
		}
		cfg := DefaultConfig()
		cfg.SMin = -1
		cfg.Eval, cfg.Thresh = int(ruleval.Lift), 120
		plain, _, err := runTree(t, c.txs, cfg, nil)
		if errors.Is(err, ErrNoItems) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, prune := range []int{0, -1, -2, 1, 2} {
			cfg.Prune = prune
			pruned, _, err := runTree(t, c.txs, cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			if prune == 0 && !slices.Equal(pruned.keys, plain.keys) {
				t.Fatalf("seed %d: backward pruning changed the result", seed)
			}
			if !isSubsequence(pruned.keys, plain.keys) && !slices.Equal(pruned.keys, plain.keys) {
				t.Fatalf("seed %d -p%d: pruned sets are not a subsequence", seed, prune)
			}
			for _, s := range pruned.sets {
				if len(s.Items) < 2 {
					continue
				}
				if s.Eval < 1.2 {
					t.Fatalf("seed %d -p%d: %v fails the threshold", seed, prune, s)
				}
			}
		}
	}
	// a set that passes the evaluation but whose subsets all fail is
	// removed by weak forward pruning from size 3 on (-p-2)
	txs := txsOf(t, "a b c\na b c\na\nb\nc\nd\nd\nd\nd\nd\n", false)
	cfg := DefaultConfig()
	cfg.SMin, cfg.Eval, cfg.Thresh = -2, int(ruleval.Lift), 250
	plain, _, err := runTree(t, txs, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Prune = -2
	pruned, _, err := runTree(t, txs, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSet(plain.sets, set(2, "a", "b", "c")) || hasSet(pruned.sets, set(2, "a", "b", "c")) {
		t.Errorf("plain %v, pruned %v", plain.sets, pruned.sets)
	}
}

// TestTreeMaxSupp checks -S on the rules path: it limits the body support
// (B8), and an absolute maximum below 1 reports nothing.
func TestTreeMaxSupp(t *testing.T) {
	t.Parallel()
	txs := txsOf(t, "a b\na b\na b\na\nb\n", false)
	cfg := DefaultConfig()
	cfg.Target = report.TargetRules
	cfg.SMin, cfg.Conf = -1, 0
	for _, tc := range []struct {
		smax float64
		want int
	}{
		{100, 4}, // a, b, a<-b, b<-a
		{-4, 2},  // bodies of support 4: a<-b, b<-a
		{-3, 0},
		{0, 0},
	} {
		cfg.SMax = tc.smax
		sink, _, err := runTree(t, txs, cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(sink.rules) != tc.want {
			t.Errorf("-S%g: %d rules %v, want %d", tc.smax, len(sink.rules), sink.rules, tc.want)
		}
	}
}

// TestTreeProgressAndCancel checks the phase callbacks and cancellation.
func TestTreeProgressAndCancel(t *testing.T) {
	t.Parallel()
	txs := midDataset(t, 300)
	for _, tc := range []struct {
		target report.Target
		prune  int
		want   []Phase
	}{
		{report.TargetRules, NoPrune, []Phase{PhaseTree, PhaseReport}},
		{report.TargetClosed, 0, []Phase{PhaseTree, PhaseFilter, PhaseCloMax, PhaseReport}},
		{report.TargetAll, NoPrune, []Phase{PhaseTree, PhaseReport}},
	} {
		cfg := DefaultConfig()
		cfg.Target, cfg.SMin, cfg.Eval, cfg.Prune = tc.target, -5, int(ruleval.Lift), tc.prune
		ib := tract.NewItemBase[string]()
		m, err := New[string](cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := ib.Count(scanOf(txs)); err != nil {
			t.Fatal(err)
		}
		if err := m.Data(ib, scanOf(txs)); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Reporter(); err != nil {
			t.Fatal(err)
		}
		var got []Phase
		open := Phase(0)
		m.SetProgress(func(p Phase, done bool) {
			switch {
			case !done && open == 0:
				open = p
			case done && open == p:
				got = append(got, p)
				open = 0
			default:
				t.Errorf("unbalanced phase %v done=%v (open %v)", p, done, open)
			}
		})
		if err := m.Mine(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("target %d: phases %v, want %v", tc.target, got, tc.want)
		}
		if s := got[0].String(); s != "tree" || Phase(99).String() != "Phase(99)" {
			t.Errorf("phase names %q", s)
		}
	}
	cfg := DefaultConfig()
	cfg.Target, cfg.SMin = report.TargetRules, -5
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ib := tract.NewItemBase[string]()
	_, err := Mine(ctx, cfg, ib, scanOf(txs), nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
}

// TestArenaOverflow checks that a node arena overflow is an error
// (ErrTooLarge, wrapping tract.ErrTooLarge) for every variant and the
// rules path, not a crash. It lowers the arena limit and must not run in
// parallel.
func TestArenaOverflow(t *testing.T) {
	txs := midDataset(t, 200)
	old := maxNodes
	maxNodes = 50
	defer func() { maxNodes = old }()
	cfgs := []Config{}
	for _, v := range variants {
		cfg := DefaultConfig()
		cfg.SMin, cfg.Algo, cfg.Pack = -2, v.algo, v.pack
		cfg.Mode &^= v.clear
		cfgs = append(cfgs, cfg)
	}
	rules := DefaultConfig()
	rules.Target, rules.SMin = report.TargetRules, -2
	cfgs = append(cfgs, rules)
	for _, cfg := range cfgs {
		_, err := Mine(context.Background(), cfg, tract.NewItemBase[string](), scanOf(txs), nil)
		if !errors.Is(err, ErrTooLarge) || !errors.Is(err, tract.ErrTooLarge) {
			t.Errorf("%s: %v, want ErrTooLarge", describe(cfg), err)
		}
	}
	if got := presize(1 << 40); got != 50 {
		t.Errorf("presize capped at %d", got)
	}
	maxNodes = old
	if got := presize(1 << 40); got != maxPresize {
		t.Errorf("presize %d, want %d", got, maxPresize)
	}
	// other panics are not swallowed
	defer func() {
		if r := recover(); r != "boom" {
			t.Errorf("recovered %v", r)
		}
	}()
	func() {
		var err error
		defer recoverOverflow(&err)
		panic("boom")
	}()
}

// TestTooLarge checks that Miner.Mine maps the capacity errors of the
// reporter's closed/maximal repository and of the item set tree (provoked
// with small limits in their own packages) to ErrTooLarge and leaves other
// errors alone.
func TestTooLarge(t *testing.T) {
	t.Parallel()
	errSink := errors.New("sink failed")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"nil", nil, nil},
		{"repository", report.ErrTooLarge, ErrTooLarge},
		{"wrapped repository", fmt.Errorf("x: %w", report.ErrTooLarge), ErrTooLarge},
		{"item set tree", istree.ErrTooLarge, ErrTooLarge},
		{"own arena", ErrTooLarge, ErrTooLarge},
		{"sink", errSink, errSink},
		{"canceled", context.Canceled, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tooLarge(tt.err)
			if got != tt.want { //nolint:errorlint // identity is the contract
				t.Fatalf("tooLarge(%v) = %v, want %v", tt.err, got, tt.want)
			}
			if tt.want == ErrTooLarge && !errors.Is(got, tract.ErrTooLarge) {
				t.Fatalf("%v does not wrap tract.ErrTooLarge", got)
			}
		})
	}
}

// treeCountSink counts results and calls onResult for each.
type treeCountSink struct {
	n        int
	onResult func(n int)
}

func (s *treeCountSink) ItemSet(report.ItemSet) error { s.n++; s.onResult(s.n); return nil }
func (s *treeCountSink) Rule(report.Rule) error       { s.n++; s.onResult(s.n); return nil }

// TestTreeCancelPhases cancels the rules path at the start of every
// phase and while reporting, for rules and for item sets with a rule
// measure: Mine must return context.Canceled without finishing the
// phase, and only a bounded number of results may follow a cancellation
// during reporting.
func TestTreeCancelPhases(t *testing.T) {
	t.Parallel()
	txs := midDataset(t, 3000)
	configs := map[string]Config{}
	rules := DefaultConfig()
	rules.Target, rules.SMin, rules.Conf = report.TargetRules, -60, 0
	configs["rules"] = rules
	sets := DefaultConfig()
	sets.Target, sets.SMin, sets.Eval, sets.Thresh, sets.Prune = report.TargetClosed, -60, int(ruleval.Lift), 0, 0
	configs["closed with lift"] = sets
	for name, cfg := range configs {
		for _, phase := range []Phase{PhaseTree, PhaseFilter, PhaseCloMax, PhaseReport} {
			if cfg.Target == report.TargetRules && (phase == PhaseFilter || phase == PhaseCloMax) {
				continue
			}
			t.Run(fmt.Sprintf("%s/%v", name, phase), func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				ib := tract.NewItemBase[string]()
				m, err := New[string](cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := ib.Count(scanOf(txs)); err != nil {
					t.Fatal(err)
				}
				if err := m.Data(ib, scanOf(txs)); err != nil {
					t.Fatal(err)
				}
				rep, err := m.Reporter()
				if err != nil {
					t.Fatal(err)
				}
				const cancelAt = 1000
				canceledAt := -1
				sink := &treeCountSink{onResult: func(n int) {
					if phase == PhaseReport && n == cancelAt {
						cancel()
						canceledAt = n
					}
				}}
				rep.SetSink(sink)
				finished := false
				m.SetProgress(func(p Phase, done bool) {
					switch {
					case p == phase && !done && phase != PhaseReport:
						cancel()
					case p == phase && done:
						finished = true
					}
				})
				err = m.Mine(ctx)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v after %d results", err, sink.n)
				}
				if finished {
					t.Error("the canceled phase finished")
				}
				switch {
				case phase != PhaseReport && sink.n != 0:
					t.Errorf("%d results reported", sink.n)
				case phase == PhaseReport && canceledAt < 0:
					t.Fatalf("only %d results; the test needs more", sink.n)
				case phase == PhaseReport && sink.n-canceledAt > 256*m.Bag().MaxSize():
					t.Errorf("%d results after the cancellation", sink.n-canceledAt)
				}
			})
		}
	}
}
