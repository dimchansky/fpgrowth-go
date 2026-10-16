package fpgrowth

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// TestEquivalenceCLI checks that the public API reports exactly what the
// command line program's pipeline (internal/fpg with the option values
// written in upstream notation) reports for the same settings, in the
// same order, for every target, variant and tuning.
func TestEquivalenceCLI(t *testing.T) {
	t.Parallel()
	cases, runs := numCases(300), 0
	for seed := range uint64(cases) {
		maxItems := 10
		if seed%20 == 19 {
			maxItems = 20 // more than 16 items: k-items machine paths
		}
		c, err := genTestCase(seed, maxItems)
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range targets {
			for _, v := range algorithms {
				tun := merge(v.tun, c.tuning)
				cfg := cliConfig(tg.cli, v.algo, tun, c.smin, c.smax, c.zmin, c.zmax)
				want, _, werr := runInternal(t, c.txs, cfg, c.border, nil)
				got, sum, gerr := setRecords(t, sourceOf(c.txs), c.options(tg.target, v.algo, tun))
				runs++
				if gerr != nil {
					t.Fatalf("seed %d %s/%s: %v", c.seed, tg.name, v.name, gerr)
				}
				if errors.Is(werr, fpg.ErrNoItems) {
					if sum.FrequentItems != 0 || len(got) != 0 {
						t.Fatalf("seed %d %s/%s: CLI finds no items, API reports %d items, %d sets",
							c.seed, tg.name, v.name, sum.FrequentItems, len(got))
					}
					continue
				}
				if werr != nil {
					t.Fatalf("seed %d %s/%s: internal: %v", c.seed, tg.name, v.name, werr)
				}
				if d := diffRecords(want, got, false); d != "" {
					t.Fatalf("seed %d %s/%s cfg %+v: %s", c.seed, tg.name, v.name, cfg, d)
				}
				if sum.Reported != uint64(len(got)) {
					t.Fatalf("seed %d: Summary.Reported %d, %d sets", c.seed, sum.Reported, len(got))
				}
			}
		}
	}
	t.Logf("%d datasets, %d comparisons", cases, runs)
}

// TestEquivalenceCLIEvaluation compares evaluated item sets (-eb and rule
// measures with aggregation, -i, -p) with the CLI pipeline, including
// the evaluation values bit for bit.
func TestEquivalenceCLIEvaluation(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(21, 22))
	thresholds := []float64{-100, 0, 1, 10, 50, 90, 100, 150, 1000} // percent
	cases := numCases(300)
	for seed := range uint64(cases) {
		c, err := genTestCase(seed+1000, 8)
		if err != nil {
			t.Fatal(err)
		}
		tg := targets[r.IntN(len(targets))]
		v := algorithms[r.IntN(len(algorithms))]
		m := Measure(1 + r.IntN(int(LogSupportRatio)))
		d := thresholds[r.IntN(len(thresholds))]
		ev := &Evaluation{Measure: m, Filter: true, Threshold: d / 100}
		cfg := cliConfig(tg.cli, v.algo, v.tun, c.smin, c.smax, c.zmin, c.zmax)
		cfg.Eval = int(m)
		cfg.Thresh = d
		if m != LogSupportRatio {
			ev.Aggregate = Aggregation(r.IntN(4))
			ev.InvalidateBelowExpected = r.IntN(4) == 0
			cfg.Agg, cfg.InvalidateBelowExpected = int(ev.Aggregate), ev.InvalidateBelowExpected
			if r.IntN(3) == 0 {
				p := r.IntN(5) - 2
				ev.PruneSize, cfg.Prune = &p, p
			}
		}
		opt := c.options(tg.target, v.algo, v.tun)
		opt.Evaluation = ev
		want, _, werr := runInternal(t, c.txs, cfg, c.border, nil)
		got, _, gerr := setRecords(t, sourceOf(c.txs), opt)
		if gerr != nil {
			t.Fatalf("seed %d: %v", seed, gerr)
		}
		if errors.Is(werr, fpg.ErrNoItems) {
			if len(got) != 0 {
				t.Fatalf("seed %d: CLI finds no items, API reports %d sets", seed, len(got))
			}
			continue
		}
		if werr != nil {
			t.Fatal(werr)
		}
		if d := diffRecords(want, got, true); d != "" {
			t.Fatalf("seed %d %s/%s eval %+v: %s", seed, tg.name, v.name, *ev, d)
		}
	}
}

// TestEquivalenceCLISelection compares Options.Items with -R selection.
func TestEquivalenceCLISelection(t *testing.T) {
	t.Parallel()
	for seed := range uint64(numCases(100)) {
		c, err := genTestCase(seed+2000, 10)
		if err != nil {
			t.Fatal(err)
		}
		db, err := oracle.New(c.txs, oracle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		r := rand.New(rand.NewPCG(seed, 5))
		var sel []string
		for _, it := range db.Items() {
			if r.IntN(2) == 0 {
				sel = append(sel, it)
			}
		}
		sel = append(sel, "never-occurs")
		r.Shuffle(len(sel), func(i, j int) { sel[i], sel[j] = sel[j], sel[i] })
		cfg := cliConfig(targets[0].cli, AlgorithmAuto, Tuning{}, c.smin, c.smax, c.zmin, c.zmax)
		want, _, werr := runInternal(t, c.txs, cfg, c.border, func(ib *tract.ItemBase[string]) error {
			return ib.Select(sel...)
		})
		opt := c.options(TargetFrequent, AlgorithmAuto, Tuning{})
		opt.Items = sel
		got, _, gerr := setRecords(t, sourceOf(c.txs), opt)
		if gerr != nil {
			t.Fatal(gerr)
		}
		if errors.Is(werr, fpg.ErrNoItems) {
			if len(got) != 0 {
				t.Fatalf("seed %d: CLI finds no items, API reports %d sets", seed, len(got))
			}
			continue
		}
		if d := diffRecords(want, got, false); d != "" {
			t.Fatalf("seed %d selection %v: %s", seed, sel, d)
		}
	}
}

// TestOracleItemsets compares every target and variant with the
// brute-force oracle.
func TestOracleItemsets(t *testing.T) {
	t.Parallel()
	cases, runs := numCases(500), 0
	for seed := range uint64(cases) {
		maxItems := 10
		if seed%25 == 24 {
			maxItems = 18
		}
		c, err := genTestCase(seed+3000, maxItems)
		if err != nil {
			t.Fatal(err)
		}
		db, err := oracle.New(c.txs, oracle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		total := db.Total()
		for _, tg := range targets {
			p := oracle.NewSetParams(tg.oracle, oracle.AbsMinSupp(c.smin, total))
			p.MaxSupp = oracle.AbsMaxSupp(c.smax, total)
			p.MinSize = c.zmin
			if c.zmax < fpg.MaxSize {
				p.MaxSize = c.zmax
			}
			p.Border = oracle.AbsBorder(c.border, c.zmin, total)
			want, err := db.Mine(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range algorithms {
				opt := c.options(tg.target, v.algo, merge(v.tun, c.tuning))
				got, sum, err := CollectItemsets(context.Background(), sourceOf(c.txs), opt)
				runs++
				if err != nil {
					t.Fatalf("seed %d: %v", seed, err)
				}
				if sum.FrequentItems == 0 {
					for _, s := range want {
						if len(s.Items) > 0 {
							t.Fatalf("seed %d %s/%s: no frequent items, oracle has %v", seed, tg.name, v.name, s)
						}
					}
					continue
				}
				if d := oracle.DiffSets(want, toOracleSets(got), oracle.DiffOptions{IgnoreEval: true}); d != "" {
					t.Fatalf("seed %d %s/%s %+v:\n%s", seed, tg.name, v.name, opt, d)
				}
			}
		}
	}
	t.Logf("%d datasets, %d oracle comparisons", cases, runs)
}

// itemOrder returns the internal item codes (code order) the public API
// uses for opt on txs, reproduced with the same plan.
func itemOrder(t *testing.T, txs []oracle.Transaction, opt Options[string]) []string {
	t.Helper()
	p, err := opt.plan()
	if err != nil {
		t.Fatal(err)
	}
	ib := tract.NewItemBase[string]()
	if err := ib.Count(scanOf(txs)); err != nil {
		t.Fatal(err)
	}
	p.setThresholds(&Stats{TotalWeight: int64(ib.TotalWeight())})
	m, err := fpg.New[string](p.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Data(ib, scanOf(txs)); err != nil {
		t.Fatal(err)
	}
	order := make([]string, ib.Len())
	for i := range order {
		order[i] = ib.ID(tract.Item(i))
	}
	return order
}

// TestOracleEvaluatedItemsets compares item sets filtered by a rule
// measure with the oracle, including the values.
func TestOracleEvaluatedItemsets(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(31, 32))
	thresholds := []float64{-1, 0, 0.01, 0.1, 0.5, 0.9, 1, 1.5, 10}
	for seed := range uint64(numCases(500)) {
		c, err := genTestCase(seed+4000, 8)
		if err != nil {
			t.Fatal(err)
		}
		tg := targets[r.IntN(len(targets))]
		v := algorithms[r.IntN(len(algorithms))]
		ev := &Evaluation{
			Measure:                 Measure(1 + r.IntN(int(LogSupportRatio)-1)),
			Aggregate:               Aggregation(r.IntN(4)),
			Filter:                  true,
			Threshold:               thresholds[r.IntN(len(thresholds))],
			InvalidateBelowExpected: r.IntN(4) == 0,
		}
		if tg.target == TargetFrequent && r.IntN(4) == 0 {
			zero := 0
			ev.PruneSize = &zero // backward pruning: the evaluation filter only
		}
		opt := c.options(tg.target, v.algo, v.tun)
		opt.Evaluation = ev
		got, sum, err := CollectItemsets(context.Background(), sourceOf(c.txs), opt)
		if err != nil {
			t.Fatal(err)
		}
		if sum.FrequentItems == 0 {
			continue
		}
		db, err := oracle.New(c.txs, oracle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		total := db.Total()
		p := oracle.NewSetParams(tg.oracle, oracle.AbsMinSupp(c.smin, total))
		p.MaxSupp = oracle.AbsMaxSupp(c.smax, total)
		p.MinSize = c.zmin
		if c.zmax < fpg.MaxSize {
			p.MaxSize = c.zmax
		}
		p.Border = oracle.AbsBorder(c.border, c.zmin, total)
		rm := ruleval.Measure(ev.Measure)
		p.Eval = &oracle.SetEval{
			Measure: oracle.Measure{
				Func: oracle.MeasureFunc(rm.Func()), Dir: oracle.Direction(rm.Dir()),
				Threshold: ev.Threshold, InvalidateBelowExpected: ev.InvalidateBelowExpected,
			},
			Agg:   oracle.Aggregation(ev.Aggregate),
			Order: itemOrder(t, c.txs, opt),
		}
		want, err := db.Mine(p)
		if err != nil {
			t.Fatal(err)
		}
		if d := oracle.DiffSets(want, toOracleSets(got), oracle.DiffOptions{}); d != "" {
			t.Fatalf("seed %d %s/%s eval %+v:\n%s", seed, tg.name, v.name, *ev, d)
		}
	}
}

// TestThresholdBoundaries pins the conversion of Fraction and Count
// thresholds at exact boundaries.
func TestThresholdBoundaries(t *testing.T) {
	t.Parallel()
	// 10 transactions: a in 3, b in 7, c in 10.
	var txs [][]string
	for i := range 10 {
		tx := []string{"c"}
		if i < 3 {
			tx = append(tx, "a")
		}
		if i < 7 {
			tx = append(tx, "b")
		}
		txs = append(txs, tx)
	}
	src := FromItems(txs)
	tests := []struct {
		name     string
		min, max Support
		expected []string // single items reported
	}{
		{"fraction exactly 3 of 10", Fraction(0.3), Support{}, []string{"a", "b", "c"}},
		{"fraction just above 3 of 10", Fraction(math.Nextafter(0.3, 1)), Support{}, []string{"a", "b", "c"}},
		{"fraction 0.31", Fraction(0.31), Support{}, []string{"b", "c"}},
		{"fraction 0.7", Fraction(0.7), Support{}, []string{"b", "c"}},
		{"fraction 0.7000001", Fraction(0.7000001), Support{}, []string{"c"}},
		{"fraction 1", Fraction(1), Support{}, []string{"c"}},
		{"count 3", Count(3), Support{}, []string{"a", "b", "c"}},
		{"count 4", Count(4), Support{}, []string{"b", "c"}},
		{"count 0 acts as 1", Count(0), Support{}, []string{"a", "b", "c"}},
		{"count beyond total", Count(11), Support{}, nil},
		{"max fraction exactly 7 of 10", Count(1), Fraction(0.7), []string{"a", "b"}},
		{"max fraction 0.69", Count(1), Fraction(0.69), []string{"a"}},
		{"max count 3", Count(1), Count(3), []string{"a"}},
		{"max count 0", Count(1), Count(0), nil},
		{"max fraction 1", Count(1), Fraction(1), []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sets, _, err := CollectItemsets(context.Background(), src, Options[string]{
				MinSupport: tt.min, MaxSupport: tt.max, MaxSize: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, s := range sets {
				got = append(got, s.Items[0])
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.expected) {
				t.Errorf("got %v, expected %v", got, tt.expected)
			}
		})
	}
}

// TestNoFrequentItems checks that an empty database, a database of empty
// transactions and an unreachable minimum support are not errors.
func TestNoFrequentItems(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		txs      [][]string
		opt      Options[string]
		expected Stats
	}{
		{"empty database", nil, Options[string]{MinSupport: Count(1)},
			Stats{MinSupport: 1, MaxSupport: math.MaxInt64}},
		{"empty transactions", [][]string{{}, {}}, Options[string]{MinSupport: Count(1), IncludeEmpty: true},
			Stats{Transactions: 2, TotalWeight: 2, MinSupport: 1, MaxSupport: math.MaxInt64}},
		{"support too high", [][]string{{"a"}, {"b"}}, Options[string]{MinSupport: Count(2), MaxSupport: Fraction(1)},
			Stats{Transactions: 2, TotalWeight: 2, MinSupport: 2, MaxSupport: 2}},
		{"selection matches nothing", [][]string{{"a"}}, Options[string]{MinSupport: Fraction(0.5), Items: []string{"z"}},
			Stats{Transactions: 1, TotalWeight: 1, MinSupport: 1, MaxSupport: math.MaxInt64}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var started []Stats
			opt := tt.opt
			opt.OnStart = func(st Stats) error {
				started = append(started, st)
				return nil
			}
			sets, sum, err := CollectItemsets(context.Background(), FromItems(tt.txs), opt)
			if err != nil {
				t.Fatal(err)
			}
			if len(sets) != 0 || sum.Stats != tt.expected || sum.Reported != 0 {
				t.Errorf("got %v, %+v; expected nothing, %+v", sets, sum, tt.expected)
			}
			if len(started) != 1 || started[0] != tt.expected {
				t.Errorf("OnStart calls %+v, expected one with %+v", started, tt.expected)
			}
		})
	}
}

// TestSummary checks the statistics and the pattern spectrum.
func TestSummary(t *testing.T) {
	t.Parallel()
	src := FromSlice([]Transaction[string]{
		{Items: []string{"a", "b", "c"}, Weight: 2},
		{Items: []string{"a", "b"}, Weight: 1},
		{Items: []string{"a", "d"}, Weight: 0},
		{Items: nil, Weight: 1},
	})
	sets, sum, err := CollectItemsets(context.Background(), src, Options[string]{
		MinSupport: Count(2), PatternSpectrum: true, IncludeEmpty: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// {}:4 a:3 b:3 c:2 ab:3 ac:2 bc:2 abc:2
	if sum.Transactions != 4 || sum.TotalWeight != 4 || sum.FrequentItems != 3 || sum.Reported != 8 ||
		!slices.Equal(sum.BySize, []uint64{1, 3, 3, 1}) || len(sets) != 8 {
		t.Fatalf("summary %+v, %d sets", sum, len(sets))
	}
	wantSpectrum := []SpectrumCell{{0, 4, 1}, {1, 2, 1}, {1, 3, 2}, {2, 2, 2}, {2, 3, 1}, {3, 2, 1}}
	if !slices.Equal(sum.Spectrum, wantSpectrum) {
		t.Errorf("spectrum %v, expected %v", sum.Spectrum, wantSpectrum)
	}
	for _, s := range sets {
		if !math.IsNaN(s.Value) {
			t.Errorf("%v: value without evaluation", s)
		}
	}
	// count only
	sum2, err := MineItemsets(context.Background(), src, Options[string]{MinSupport: Count(2), IncludeEmpty: true}, nil)
	if err != nil || sum2.Reported != 8 || sum2.Spectrum != nil {
		t.Errorf("count only: %+v, %v", sum2, err)
	}
}

// TestIntItems mines non-string items.
func TestIntItems(t *testing.T) {
	t.Parallel()
	type sku int32
	src := FromItems([][]sku{{1, 2, 3}, {1, 2}, {2, 3}, {1, 2, 3}})
	sets, _, err := CollectItemsets(context.Background(), src, Options[sku]{MinSupport: Count(3), Target: TargetClosed})
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(sets)
	if got != "[{[3 2] 3 NaN} {[1 2] 3 NaN} {[2] 4 NaN}]" {
		t.Errorf("got %s", got)
	}
}

// failingSource yields n transactions per pass and then the error err on
// pass failPass (1-based); other passes end normally.
func failingSource(txs [][]string, failPass, n int, err error) Source[string] {
	var mu sync.Mutex
	pass := 0
	return SourceFunc[string](func() iter.Seq2[Transaction[string], error] {
		mu.Lock()
		pass++
		p := pass
		mu.Unlock()
		return func(yield func(Transaction[string], error) bool) {
			for i, items := range txs {
				if p == failPass && i == n {
					yield(Transaction[string]{}, err)
					return
				}
				if !yield(Transaction[string]{Items: items, Weight: 1}, nil) {
					return
				}
			}
		}
	})
}

// TestSourceErrors checks that a failing source stops mining with its
// error, in either pass, before anything is reported.
func TestSourceErrors(t *testing.T) {
	t.Parallel()
	txs := [][]string{{"a", "b"}, {"a"}, {"b"}, {"a", "b"}, {"a"}}
	errBoom := errors.New("boom")
	for _, pass := range []int{1, 2} {
		for _, n := range []int{0, 3} {
			t.Run(fmt.Sprintf("pass %d at %d", pass, n), func(t *testing.T) {
				t.Parallel()
				calls := 0
				_, err := MineItemsets(context.Background(), failingSource(txs, pass, n, errBoom),
					Options[string]{MinSupport: Count(1)}, func(Itemset[string]) error { calls++; return nil })
				if !errors.Is(err, errBoom) || calls != 0 {
					t.Fatalf("err %v, %d calls", err, calls)
				}
				if want := fmt.Sprintf("pass %d", pass); !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			})
		}
	}
}

// TestNotReplayable checks the replay consistency check.
func TestNotReplayable(t *testing.T) {
	t.Parallel()
	pass1 := []Transaction[string]{{Items: []string{"a", "b"}, Weight: 1}, {Items: []string{"a"}, Weight: 2}}
	tests := []struct {
		name  string
		pass2 []Transaction[string]
	}{
		{"fewer transactions", pass1[:1]},
		{"more transactions", append(slices.Clone(pass1), Transaction[string]{Items: []string{"a"}, Weight: 1})},
		{"other weight", []Transaction[string]{pass1[0], {Items: []string{"a"}, Weight: 3}}},
		{"new item", []Transaction[string]{{Items: []string{"a", "z"}, Weight: 1}, pass1[1]}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			pass := 0
			src := SourceFunc[string](func() iter.Seq2[Transaction[string], error] {
				mu.Lock()
				defer mu.Unlock()
				pass++
				if pass == 1 {
					return FromSlice(pass1).Transactions()
				}
				return FromSlice(tt.pass2).Transactions()
			})
			_, err := MineItemsets(context.Background(), src, Options[string]{MinSupport: Count(1)}, nil)
			if !errors.Is(err, ErrNotReplayable) {
				t.Fatalf("got %v, expected ErrNotReplayable", err)
			}
		})
	}
}

// TestWeights checks negative weights and weight overflow.
func TestWeights(t *testing.T) {
	t.Parallel()
	_, err := MineItemsets(context.Background(), FromSlice([]Transaction[string]{{Items: []string{"a"}, Weight: 1}, {Items: []string{"a"}, Weight: -1}}),
		Options[string]{MinSupport: Count(1)}, nil)
	if !errors.Is(err, ErrNegativeWeight) {
		t.Errorf("negative weight: %v", err)
	}
	_, err = MineItemsets(context.Background(), FromSlice([]Transaction[string]{{Items: []string{"a"}, Weight: math.MaxInt64}, {Items: []string{"a"}, Weight: 1}}),
		Options[string]{MinSupport: Count(1)}, nil)
	if !errors.Is(err, ErrWeightOverflow) {
		t.Errorf("overflow: %v", err)
	}
	sets, sum, err := CollectItemsets(context.Background(), FromSlice([]Transaction[string]{{Items: []string{"a"}, Weight: math.MaxInt64}}),
		Options[string]{MinSupport: Fraction(1)})
	if err != nil || len(sets) != 1 || sets[0].Support != math.MaxInt64 || sum.TotalWeight != math.MaxInt64 {
		t.Errorf("max weight: %v %+v %v", sets, sum, err)
	}
}

// bigSource returns a generated source large enough for cancellation
// and early-stop tests.
func bigSource(n int) Source[int] {
	return Generate(n, func(i int, buf []int) Transaction[int] {
		r := rand.New(rand.NewPCG(uint64(i), 1))
		for j := range 30 {
			if r.IntN(3) > 0 {
				buf = append(buf, j)
			}
		}
		return Transaction[int]{Items: buf, Weight: 1}
	})
}

// TestEarlyStop checks iterator break and callback errors.
func TestEarlyStop(t *testing.T) {
	t.Parallel()
	src := bigSource(2000)
	opt := Options[int]{MinSupport: Fraction(0.2)}
	n := 0
	for s, err := range Itemsets(context.Background(), src, opt) {
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Items) == 0 {
			t.Fatal("empty set")
		}
		n++
		if n == 10 {
			break
		}
	}
	if n != 10 {
		t.Fatalf("%d sets", n)
	}
	errEnough := errors.New("enough")
	calls := 0
	sum, err := MineItemsets(context.Background(), src, opt, func(Itemset[int]) error {
		calls++
		if calls == 5 {
			return errEnough
		}
		return nil
	})
	if err != errEnough || calls != 5 || sum.Reported != 5 {
		t.Fatalf("err %v, %d calls, %d reported", err, calls, sum.Reported)
	}
	rcalls := 0
	for _, err := range Rules(context.Background(), src, RuleOptions[int]{MinSupport: Fraction(0.2), MinConfidence: 0.5}) {
		if err != nil {
			t.Fatal(err)
		}
		rcalls++
		if rcalls == 3 {
			break
		}
	}
	_, err = MineRules(context.Background(), src, RuleOptions[int]{MinSupport: Fraction(0.2)},
		func(Rule[int]) error { return errEnough })
	if err != errEnough {
		t.Fatalf("rules: %v", err)
	}
}

// TestCancel cancels before, during reading and during mining.
func TestCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MineItemsets(ctx, bigSource(10), Options[int]{MinSupport: Count(1)}, nil); err != context.Canceled {
		t.Errorf("canceled before: %v", err)
	}
	for _, at := range []string{"reading", "mining"} {
		t.Run(at, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			src := bigSource(5000)
			if at == "reading" {
				inner := src
				read := 0
				src = SourceFunc[int](func() iter.Seq2[Transaction[int], error] {
					return func(yield func(Transaction[int], error) bool) {
						for tx, err := range inner.Transactions() {
							read++
							if read == 3000 {
								cancel()
							}
							if !yield(tx, err) {
								return
							}
						}
					}
				})
			}
			calls := 0
			_, err := MineItemsets(ctx, src, Options[int]{MinSupport: Fraction(0.05)}, func(Itemset[int]) error {
				calls++
				if calls == 100 {
					cancel()
				}
				return nil
			})
			if err != context.Canceled {
				t.Fatalf("got %v", err)
			}
			if at == "reading" && calls != 0 {
				t.Errorf("%d sets reported after canceled reading", calls)
			}
		})
	}
	// iterator: the error is the last element
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	var last error
	n := 0
	for _, err := range Itemsets(ctx2, bigSource(3000), Options[int]{MinSupport: Fraction(0.05)}) {
		n++
		if n == 50 {
			cancel2()
		}
		last = err
	}
	if last != context.Canceled {
		t.Errorf("iterator: last error %v", last)
	}
}

// TestConcurrent runs mining calls concurrently on shared sources (run
// with -race).
func TestConcurrent(t *testing.T) {
	t.Parallel()
	gen := bigSource(500)
	slice := FromItems([][]string{{"a", "b"}, {"b", "c"}, {"a", "b", "c"}, {"a"}})
	wantInt, _, err := CollectItemsets(context.Background(), gen, Options[int]{MinSupport: Fraction(0.3), Target: TargetClosed})
	if err != nil {
		t.Fatal(err)
	}
	wantRules, _, err := CollectRules(context.Background(), slice, RuleOptions[string]{MinSupport: Count(1)})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := range 8 {
		wg.Go(func() {
			if i%2 == 0 {
				got, _, err := CollectItemsets(context.Background(), gen, Options[int]{MinSupport: Fraction(0.3), Target: TargetClosed})
				if err == nil && fmt.Sprint(got) != fmt.Sprint(wantInt) {
					err = errors.New("different item sets")
				}
				errs <- err
				return
			}
			got, _, err := CollectRules(context.Background(), slice, RuleOptions[string]{MinSupport: Count(1)})
			if err == nil && fmt.Sprint(got) != fmt.Sprint(wantRules) {
				err = errors.New("different rules")
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

// TestDeterministic checks that repeated runs give the same order.
func TestDeterministic(t *testing.T) {
	t.Parallel()
	src := bigSource(300)
	var first string
	for range 3 {
		sets, _, err := CollectItemsets(context.Background(), src, Options[int]{MinSupport: Fraction(0.3), Target: TargetMaximal})
		if err != nil {
			t.Fatal(err)
		}
		if s := fmt.Sprint(sets); first == "" {
			first = s
		} else if s != first {
			t.Fatal("different output order")
		}
	}
}

// TestItemsReuseAndClone checks the ownership contract of Itemset.Items.
func TestItemsReuseAndClone(t *testing.T) {
	t.Parallel()
	src := FromItems([][]string{{"a", "b", "c"}, {"a", "b", "c"}})
	var kept, cloned []Itemset[string]
	_, err := MineItemsets(context.Background(), src, Options[string]{MinSupport: Count(1)}, func(s Itemset[string]) error {
		if cap(s.Items) != len(s.Items) {
			t.Errorf("Items has spare capacity %d > %d", cap(s.Items), len(s.Items))
		}
		kept = append(kept, s)
		cloned = append(cloned, s.Clone())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range cloned {
		k := strings.Join(slices.Sorted(slices.Values(s.Items)), "")
		if seen[k] {
			t.Errorf("clone %v duplicated", s.Items)
		}
		seen[k] = true
	}
	if len(seen) != 7 {
		t.Errorf("%d distinct clones, expected 7", len(seen))
	}
	_ = kept // the retained slices alias the library's buffer; nothing to assert
}

// TestMappedErrors checks that internal capacity errors keep their
// details and map to the public sentinels.
func TestMappedErrors(t *testing.T) {
	t.Parallel()
	err := mapInternal(fmt.Errorf("fpg: record 7: item z not seen in pass 1: %w", tract.ErrNotReplayable))
	if !errors.Is(err, ErrNotReplayable) || err.Error() != "fpgrowth: source is not replayable: record 7: item z not seen in pass 1" {
		t.Errorf("got %q", err)
	}
	err = mapInternal(fpg.ErrTooLarge)
	if !errors.Is(err, ErrTooLarge) || !strings.HasPrefix(err.Error(), "fpgrowth: database too large: more than") {
		t.Errorf("got %q", err)
	}
	if err := mapInternal(tract.ErrTooLarge); err != ErrTooLarge {
		t.Errorf("got %q", err)
	}
	other := errors.New("other")
	if mapInternal(other) != other || mapInternal(nil) != nil {
		t.Error("other errors must pass unchanged")
	}
}

// TestCancelTreePhases cancels rule mining and item set mining with a
// rule measure (the item set tree path) before the tree is filled (from
// OnStart) and while results are reported: mining must return
// context.Canceled, with only a bounded number of results after the
// cancellation.
func TestCancelTreePhases(t *testing.T) {
	t.Parallel()
	src := bigSource(1000)
	const cancelAt = 1000
	// at most a few hundred item sets of up to 30 items after the check
	const bound = 256 * 30
	type run func(ctx context.Context, onStart func(Stats) error, fn func() error) error
	runs := map[string]run{
		"rules": func(ctx context.Context, onStart func(Stats) error, fn func() error) error {
			_, err := MineRules(ctx, src, RuleOptions[int]{MinSupport: Fraction(0.2), OnStart: onStart},
				func(Rule[int]) error { return fn() })
			return err
		},
		"rules iterator": func(ctx context.Context, onStart func(Stats) error, fn func() error) error {
			for _, err := range Rules(ctx, src, RuleOptions[int]{MinSupport: Fraction(0.2), OnStart: onStart}) {
				if err != nil {
					return err
				}
				if err := fn(); err != nil {
					return err
				}
			}
			return nil
		},
		"item sets with a rule measure": func(ctx context.Context, onStart func(Stats) error, fn func() error) error {
			_, err := MineItemsets(ctx, src, Options[int]{MinSupport: Fraction(0.2), OnStart: onStart,
				Evaluation: &Evaluation{Measure: Lift}}, func(Itemset[int]) error { return fn() })
			return err
		},
		"closed sets filtered by a rule measure": func(ctx context.Context, onStart func(Stats) error, fn func() error) error {
			_, err := MineItemsets(ctx, src, Options[int]{MinSupport: Fraction(0.2), OnStart: onStart, Target: TargetClosed,
				Evaluation: &Evaluation{Measure: Lift, Filter: true, Threshold: 0.5, PruneSize: intPtr(0)}},
				func(Itemset[int]) error { return fn() })
			return err
		},
	}
	for name, mine := range runs {
		t.Run(name+"/before the tree", func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			n := 0
			err := mine(ctx, func(Stats) error { cancel(); return nil }, func() error { n++; return nil })
			if err != context.Canceled || n != 0 {
				t.Errorf("got %v after %d results", err, n)
			}
		})
		t.Run(name+"/while reporting", func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			n := 0
			err := mine(ctx, nil, func() error {
				if n++; n == cancelAt {
					cancel()
				}
				return nil
			})
			if err != context.Canceled {
				t.Fatalf("got %v after %d results", err, n)
			}
			t.Logf("%d results after the cancellation", n-cancelAt)
			if n < cancelAt || n-cancelAt > bound {
				t.Errorf("%d results, %d after the cancellation (bound %d)", n, n-cancelAt, bound)
			}
		})
	}
}

// TestExactCountThresholds checks absolute thresholds beyond 2^53, where
// float64 cannot represent every support: Count bounds are exact and
// inclusive, Fraction(1) includes the total weight.
func TestExactCountThresholds(t *testing.T) {
	t.Parallel()
	for _, w := range []int64{1<<53 + 1, 1<<60 + 1, math.MaxInt64} {
		// a: support w, b and {a, b}: support w-1
		src := FromSlice([]Transaction[string]{
			{Items: []string{"a", "b"}, Weight: w - 1},
			{Items: []string{"a"}, Weight: 1},
		})
		a, b, ab := fmt.Sprintf("a:%d", w), fmt.Sprintf("b:%d", w-1), fmt.Sprintf("a b:%d", w-1)
		tests := []struct {
			name     string
			min, max Support
			expected []string // sorted items and supports
		}{
			{"min count w", Count(w), Support{}, []string{a}},
			{"min count w-1", Count(w - 1), Support{}, []string{ab, a, b}},
			{"min count w+1", Count(w + 1), Support{}, nil},
			{"max count w-1", Count(1), Count(w - 1), []string{ab, b}},
			{"max count w", Count(1), Count(w), []string{ab, a, b}},
			{"min and max count w", Count(w), Count(w), []string{a}},
			{"max fraction 1", Count(w), Fraction(1), []string{a}},
			// the relative tolerance of Fraction exceeds 1 beyond 2^53
			{"min fraction 1", Fraction(1), Count(w), []string{ab, a, b}},
		}
		for _, tt := range tests {
			if tt.name == "min count w+1" && w == math.MaxInt64 {
				continue
			}
			t.Run(fmt.Sprintf("%s of %d", tt.name, w), func(t *testing.T) {
				t.Parallel()
				sets, sum, err := CollectItemsets(context.Background(), src, Options[string]{
					MinSupport: tt.min, MaxSupport: tt.max})
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, s := range sets {
					got = append(got, fmt.Sprintf("%s:%d", strings.Join(slices.Sorted(slices.Values(s.Items)), " "), s.Support))
				}
				slices.Sort(got)
				if !slices.Equal(got, tt.expected) {
					t.Errorf("got %q, expected %q (stats %+v)", got, tt.expected, sum.Stats)
				}
				if tt.min == Fraction(1) && (sum.MinSupport > w || sum.MinSupport < w-w>>50) {
					t.Errorf("Fraction(1) resolved to %d of %d", sum.MinSupport, w)
				}
			})
		}
		t.Run(fmt.Sprintf("rules of %d", w), func(t *testing.T) {
			t.Parallel()
			tests := map[int64][]string{
				w:     {"a <- {}"},
				w - 1: {"a <- {b}", "a <- {}", "b <- {a}", "b <- {}"},
			}
			for minSupp, expected := range tests {
				rules, sum, err := CollectRules(context.Background(), src, RuleOptions[string]{
					MinSupport: Count(minSupp), OriginalSupport: true})
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, r := range rules {
					got = append(got, fmt.Sprintf("%s <- {%s}", r.Head, strings.Join(r.Body, " ")))
				}
				slices.Sort(got)
				if !slices.Equal(got, expected) {
					t.Errorf("min %d: got %q, expected %q", minSupp, got, expected)
				}
				if sum.MinSupport != minSupp || sum.MaxSupport != math.MaxInt64 {
					t.Errorf("min %d: resolved %+v", minSupp, sum.Stats)
				}
			}
		})
	}
}

// TestOnStart checks that OnStart is called once, before the first
// result, with the Stats that the Summary also has, and that its error
// stops mining unchanged.
func TestOnStart(t *testing.T) {
	t.Parallel()
	src := FromSlice([]Transaction[string]{
		{Items: []string{"a", "b"}, Weight: 3},
		{Items: []string{"a", "c"}, Weight: 1},
		{Items: nil, Weight: 6},
	})
	expected := Stats{Transactions: 3, TotalWeight: 10, FrequentItems: 2, MinSupport: 3, MaxSupport: 5}
	var calls []Stats
	results := 0
	onStart := func(st Stats) error {
		if results != 0 {
			t.Errorf("OnStart after %d results", results)
		}
		calls = append(calls, st)
		return nil
	}
	sum, err := MineItemsets(context.Background(), src,
		Options[string]{MinSupport: Fraction(0.3), MaxSupport: Count(5), OnStart: onStart},
		func(Itemset[string]) error { results++; return nil })
	if err != nil || results != 3 || len(calls) != 1 || calls[0] != expected || sum.Stats != expected {
		t.Errorf("item sets: err %v, %d results, calls %+v, summary %+v", err, results, calls, sum.Stats)
	}
	calls, results = nil, 0
	sum, err = MineRules(context.Background(), src,
		RuleOptions[string]{MinSupport: Fraction(0.3), MaxSupport: Count(5), OnStart: onStart},
		func(Rule[string]) error { results++; return nil })
	// b <- a, c <- a, a <- b (body supports 4, 4, 3; empty bodies have 10);
	// with confidence 0 every item is frequent for rules
	expected.FrequentItems = 3
	if err != nil || results != 3 || len(calls) != 1 || calls[0] != expected || sum.Stats != expected {
		t.Errorf("rules: err %v, %d results, calls %+v, summary %+v", err, results, calls, sum.Stats)
	}
	errNo := errors.New("not today")
	sum, err = MineItemsets(context.Background(), src,
		Options[string]{MinSupport: Count(1), OnStart: func(Stats) error { return errNo }},
		func(Itemset[string]) error { results++; return nil })
	if err != errNo || sum.Reported != 0 || sum.FrequentItems != 3 {
		t.Errorf("error: %v, %+v", err, sum)
	}
	_, err = MineRules(context.Background(), src,
		RuleOptions[string]{MinSupport: Count(100), OnStart: func(Stats) error { return errNo }}, nil)
	if err != errNo {
		t.Errorf("rules without frequent items: %v", err)
	}
	failing := failingSource([][]string{{"a"}}, 2, 0, errNo)
	called := false
	_, err = MineItemsets(context.Background(), failing,
		Options[string]{MinSupport: Count(1), OnStart: func(Stats) error { called = true; return nil }}, nil)
	if !errors.Is(err, errNo) || called {
		t.Errorf("failing second pass: %v, OnStart called %v", err, called)
	}
}

// TestInvalidItems checks that items that cannot be map keys or never
// equal themselves are errors, not panics or misleading replay errors.
func TestInvalidItems(t *testing.T) {
	t.Parallel()
	opt := Options[any]{MinSupport: Count(1)}
	tests := []struct {
		name  string
		items [][]any
		ok    bool
	}{
		{"comparable dynamic types", [][]any{{1, "a", nil, struct{ X any }{2}}, {1}}, true},
		{"slice", [][]any{{1}, {[]int{1}}}, false},
		{"map in a struct", [][]any{{struct{ X any }{map[int]int{}}}}, false},
		{"nan", [][]any{{1}, {math.NaN()}}, false},
		{"nan in an array", [][]any{{[2]float64{1, math.NaN()}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := MineItemsets(context.Background(), FromItems(tt.items), opt, nil)
			if tt.ok != (err == nil) || !tt.ok && !errors.Is(err, ErrInvalidItem) {
				t.Errorf("got %v", err)
			}
			_, err = MineRules(context.Background(), FromItems(tt.items), RuleOptions[any]{MinSupport: Count(1)}, nil)
			if tt.ok != (err == nil) || !tt.ok && !errors.Is(err, ErrInvalidItem) {
				t.Errorf("rules: got %v", err)
			}
		})
	}
	_, err := MineItemsets(context.Background(), FromItems([][]float64{{1, math.NaN()}}), Options[float64]{MinSupport: Count(1)}, nil)
	if !errors.Is(err, ErrInvalidItem) || !strings.Contains(err.Error(), "transaction 1 (pass 1)") {
		t.Errorf("float NaN: %v", err)
	}
	_, err = MineItemsets(context.Background(), FromItems([][]float64{{1}}),
		Options[float64]{MinSupport: Count(1), Items: []float64{1, math.NaN()}}, nil)
	if !errors.Is(err, ErrInvalidOptions) || !strings.Contains(err.Error(), "Items: item 1:") {
		t.Errorf("NaN in Items: %v", err)
	}
	_, err = MineItemsets(context.Background(), FromItems([][]any{{1}}),
		Options[any]{MinSupport: Count(1), Items: []any{[]byte("x")}}, nil)
	if !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("slice in Items: %v", err)
	}
}

// FuzzMineItemsets compares MineItemsets with the brute-force oracle for
// fuzzed databases and random options: every target, variant and tuning,
// support and size limits, borders, and evaluations with and without
// filtering.
func FuzzMineItemsets(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, word uint64, data []byte) {
		txs := decodeFuzzTxs(data)
		b := fuzzBits(word)
		c := testCase{txs: txs, smax: 100, zmin: 1, zmax: fpg.MaxSize}
		if b.take(1) == 0 {
			n := int64(1 + b.take(2))
			c.smin, c.minSupp = float64(-n), Count(n)
		} else {
			k := 1 + b.take(6)%60
			c.smin, c.minSupp = float64(k), Fraction(float64(k)/100)
		}
		switch b.take(2) {
		case 1:
			n := int64(1 + b.take(3))
			c.smax, c.maxSupp = float64(-n), Count(n)
		case 2:
			k := 30 + b.take(6)
			c.smax, c.maxSupp = float64(k), Fraction(float64(k)/100)
		}
		switch b.take(2) {
		case 1:
			c.zmin, c.includeEmpty = 0, true
		case 2:
			c.zmin = 2 + b.take(1)
		}
		if b.take(1) == 1 {
			c.zmax = max(c.zmin, 1) + b.take(2)
		}
		if b.take(2) == 0 {
			n := int64(1 + b.take(2))
			c.border = []float64{float64(-n), 20}
			c.pubBorder = []Support{Count(n), Fraction(0.2)}
		}
		tg := targets[b.take(2)]
		v := algorithms[b.take(4)%len(algorithms)]
		tun := merge(v.tun, Tuning{
			NoPerfectExtensions: b.take(1) == 1,
			NoHeadUnionTail:     b.take(1) == 1,
			ItemOrder:           ItemOrder(b.take(3) % 6),
		})
		opt := c.options(tg.target, v.algo, tun)
		evalMode := b.take(2) // 0, 1: none, 2: values only, 3: filter
		if evalMode >= 2 {
			opt.Evaluation = &Evaluation{
				Measure:                 Measure(1 + b.take(5)%(int(LogSupportRatio)-1)),
				Aggregate:               Aggregation(b.take(2)),
				InvalidateBelowExpected: b.take(1) == 1,
			}
			if evalMode == 3 {
				opt.Evaluation.Filter = true
				opt.Evaluation.Threshold = []float64{-1, 0, 0.01, 0.1, 0.5, 1, 1.5, 10}[b.take(3)]
				if tg.target == TargetFrequent && b.take(1) == 1 {
					opt.Evaluation.PruneSize = intPtr(0) // backward pruning: the filter only
				}
			}
		}
		got, sum, err := CollectItemsets(context.Background(), sourceOf(txs), opt)
		if err != nil {
			t.Fatalf("%+v: %v", opt, err)
		}
		if sum.Reported != uint64(len(got)) {
			t.Fatalf("reported %d, got %d", sum.Reported, len(got))
		}
		db, err := oracle.New(txs, oracle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		total := db.Total()
		p := oracle.NewSetParams(tg.oracle, oracle.AbsMinSupp(c.smin, total))
		p.MaxSupp = oracle.AbsMaxSupp(c.smax, total)
		p.MinSize = c.zmin
		if c.zmax < fpg.MaxSize {
			p.MaxSize = c.zmax
		}
		p.Border = oracle.AbsBorder(c.border, c.zmin, total)
		if sum.FrequentItems == 0 {
			if len(got) > 0 {
				t.Fatalf("no frequent items, %d sets", len(got))
			}
			want, err := db.Mine(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range want {
				if len(s.Items) > 0 {
					t.Fatalf("no frequent items, oracle has %v", s)
				}
			}
			return
		}
		if evalMode == 3 {
			e := opt.Evaluation
			rm := ruleval.Measure(e.Measure)
			p.Eval = &oracle.SetEval{
				Measure: oracle.Measure{
					Func: oracle.MeasureFunc(rm.Func()), Dir: oracle.Direction(rm.Dir()),
					Threshold: e.Threshold, InvalidateBelowExpected: e.InvalidateBelowExpected,
				},
				Agg:   oracle.Aggregation(e.Aggregate),
				Order: itemOrder(t, txs, opt),
			}
		}
		want, err := db.Mine(p)
		if err != nil {
			t.Fatal(err)
		}
		if d := oracle.DiffSets(want, toOracleSets(got), oracle.DiffOptions{IgnoreEval: evalMode != 3}); d != "" {
			t.Fatalf("%s/%s %+v eval %+v:\n%s", tg.name, v.name, opt, opt.Evaluation, d)
		}
	})
}
