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
					if sum.Items != 0 || len(got) != 0 {
						t.Fatalf("seed %d %s/%s: CLI finds no items, API reports %d items, %d sets",
							c.seed, tg.name, v.name, sum.Items, len(got))
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
		ev := &Evaluation{Measure: m, Threshold: d / 100}
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
		cfg := cliConfig(targets[0].cli, Auto, Tuning{}, c.smin, c.smax, c.zmin, c.zmax)
		want, _, werr := runInternal(t, c.txs, cfg, c.border, func(ib *tract.ItemBase[string]) error {
			return ib.Select(sel...)
		})
		opt := c.options(Frequent, Auto, Tuning{})
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
				if sum.Items == 0 {
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
	p.setThresholds(int64(ib.TotalWeight()))
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
			Threshold:               thresholds[r.IntN(len(thresholds))],
			InvalidateBelowExpected: r.IntN(4) == 0,
		}
		if tg.target == Frequent && r.IntN(4) == 0 {
			zero := 0
			ev.PruneSize = &zero // backward pruning: the evaluation filter only
		}
		opt := c.options(tg.target, v.algo, v.tun)
		opt.Evaluation = ev
		got, sum, err := CollectItemsets(context.Background(), sourceOf(c.txs), opt)
		if err != nil {
			t.Fatal(err)
		}
		if sum.Items == 0 {
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
		name string
		txs  [][]string
		opt  Options[string]
		sum  Summary
	}{
		{"empty database", nil, Options[string]{MinSupport: Count(1)}, Summary{}},
		{"empty transactions", [][]string{{}, {}}, Options[string]{MinSupport: Count(1), IncludeEmpty: true},
			Summary{Transactions: 2, TotalWeight: 2}},
		{"support too high", [][]string{{"a"}, {"b"}}, Options[string]{MinSupport: Count(2)},
			Summary{Transactions: 2, TotalWeight: 2}},
		{"selection matches nothing", [][]string{{"a"}}, Options[string]{MinSupport: Count(1), Items: []string{"z"}},
			Summary{Transactions: 1, TotalWeight: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sets, sum, err := CollectItemsets(context.Background(), FromItems(tt.txs), tt.opt)
			if err != nil {
				t.Fatal(err)
			}
			if len(sets) != 0 || sum.Transactions != tt.sum.Transactions || sum.TotalWeight != tt.sum.TotalWeight ||
				sum.Items != 0 || sum.Reported != 0 {
				t.Errorf("got %v, %+v; expected nothing, %+v", sets, sum, tt.sum)
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
	if sum.Transactions != 4 || sum.TotalWeight != 4 || sum.Items != 3 || sum.Reported != 8 ||
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
	sets, _, err := CollectItemsets(context.Background(), src, Options[sku]{MinSupport: Count(3), Target: Closed})
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
	wantInt, _, err := CollectItemsets(context.Background(), gen, Options[int]{MinSupport: Fraction(0.3), Target: Closed})
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
				got, _, err := CollectItemsets(context.Background(), gen, Options[int]{MinSupport: Fraction(0.3), Target: Closed})
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
		sets, _, err := CollectItemsets(context.Background(), src, Options[int]{MinSupport: Fraction(0.3), Target: Maximal})
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
