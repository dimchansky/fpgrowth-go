package fpg

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// propCase is one random dataset with random mining options.
type propCase struct {
	seed   uint64
	txs    []oracle.Transaction
	smin   float64 // upstream notation
	smax   float64
	zmin   int
	zmax   int
	noPerf bool // -x
	noTail bool // -u
	sort   int  // -q
	border []float64
}

func genCase(seed uint64, maxItems int) (propCase, error) {
	r := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
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
		return propCase{}, err
	}
	c := propCase{seed: seed, txs: txs, zmax: MaxSize, smax: 100}
	switch r.IntN(4) {
	case 0:
		c.smin = float64(-(1 + r.IntN(4)))
	case 1:
		c.smin = float64(r.IntN(60)) // percentage, includes 0 (B2)
	default:
		c.smin = -1
	}
	if r.IntN(4) == 0 {
		c.zmin = r.IntN(4)
	} else {
		c.zmin = 1
	}
	if r.IntN(3) == 0 {
		c.zmax = c.zmin + r.IntN(4)
	}
	if r.IntN(5) == 0 {
		c.smax = float64(-(2 + r.IntN(10)))
	}
	c.noPerf = r.IntN(3) == 0
	c.noTail = r.IntN(3) == 0
	c.sort = []int{2, 2, 1, 0, -1, -2}[r.IntN(6)]
	if r.IntN(6) == 0 {
		c.border = []float64{float64(-(1 + r.IntN(3))), float64(-(1 + r.IntN(4)))}
	}
	return c, nil
}

func (c propCase) config(v variant, target report.Target) Config {
	cfg := DefaultConfig()
	cfg.Target = target
	cfg.Algo = v.algo
	cfg.Pack = v.pack
	cfg.Mode &^= v.clear
	if c.noPerf {
		cfg.Mode &^= ModePerfect
	}
	if c.noTail {
		cfg.Mode &^= ModeTail
	}
	cfg.SMin, cfg.SMax = c.smin, c.smax
	cfg.ZMin, cfg.ZMax = c.zmin, c.zmax
	cfg.Sort = c.sort
	return cfg
}

// checkCase compares every variant and target on c with the oracle and
// returns the number of comparisons.
func checkCase(t *testing.T, c propCase) int {
	t.Helper()
	db, err := oracle.New(c.txs, oracle.Options{})
	if err != nil {
		t.Fatalf("seed %d: oracle: %v", c.seed, err)
	}
	total := db.Total()
	var border []tract.Supp
	if c.border != nil {
		border = oracle.AbsBorder(c.border, c.zmin, total)
	}
	n := 0
	for _, tg := range targets {
		p := oracle.NewSetParams(tg.oracle, oracle.AbsMinSupp(c.smin, total))
		p.MaxSupp = oracle.AbsMaxSupp(c.smax, total)
		p.MinSize = c.zmin
		if c.zmax < MaxSize {
			p.MaxSize = c.zmax
		}
		p.Border = border
		want, err := db.Mine(p)
		if err != nil {
			t.Fatalf("seed %d: oracle mine: %v", c.seed, err)
		}
		for _, v := range variants {
			cfg := c.config(v, tg.target)
			res, err := run(scanOf(c.txs), cfg, runOpts{border: border})
			n++
			if errors.Is(err, ErrNoItems) {
				for _, s := range want {
					if len(s.Items) > 0 {
						t.Errorf("seed %d %s/%s %s: ErrNoItems, oracle has %v",
							c.seed, tg.name, v.name, describe(cfg), s)
						break
					}
				}
				continue
			}
			if err != nil {
				t.Fatalf("seed %d %s/%s: %v", c.seed, tg.name, v.name, err)
			}
			if d := oracle.DiffSets(want, res.sink.sets, oracle.DiffOptions{IgnoreEval: true}); d != "" {
				t.Errorf("seed %d %s/%s %s border=%v:\n%s", c.seed, tg.name, v.name,
					describe(cfg), c.border, d)
			}
		}
	}
	return n
}

// TestOracle compares all variants, targets and options with the
// brute-force oracle on random datasets. FPG_ORACLE_CASES sets the number
// of datasets (default 1000, 150 with -short).
func TestOracle(t *testing.T) {
	t.Parallel()
	cases := 1000
	if testing.Short() {
		cases = 150
	}
	if s := os.Getenv("FPG_ORACLE_CASES"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("FPG_ORACLE_CASES: %v", err)
		}
		cases = n
	}
	runs := 0
	for seed := range uint64(cases) {
		maxItems := 10
		if seed%25 == 24 {
			maxItems = 20 // more than 16 items (packing and tree paths)
		}
		c, err := genCase(seed, maxItems)
		if err != nil {
			t.Fatal(err)
		}
		runs += checkCase(t, c)
		if t.Failed() {
			break
		}
	}
	t.Logf("%d datasets, %d oracle comparisons", cases, runs)
}

// txsOf parses transactions: one per line, items separated by spaces;
// with weighted, the last field is the weight.
func txsOf(t *testing.T, data string, weighted bool) []oracle.Transaction {
	t.Helper()
	var txs []oracle.Transaction
	for line := range strings.Lines(data) {
		f := strings.Fields(line)
		w := tract.Supp(1)
		if weighted {
			n, err := strconv.ParseInt(f[len(f)-1], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			w, f = tract.Supp(n), f[:len(f)-1]
		}
		txs = append(txs, oracle.Transaction{Items: f, Weight: w})
	}
	return txs
}

// oracleSets mines txs with the oracle using cfg's thresholds.
func oracleSets(t *testing.T, txs []oracle.Transaction, cfg Config) []oracle.Set {
	t.Helper()
	db, err := oracle.New(txs, oracle.Options{})
	if err != nil {
		t.Fatal(err)
	}
	n := cfg
	n.normalize()
	tg := map[report.Target]oracle.Target{
		report.TargetAll: oracle.Frequent, report.TargetClosed: oracle.Closed,
		report.TargetMaximal: oracle.Maximal, report.TargetGenerators: oracle.Generators,
	}[n.Target]
	p := oracle.NewSetParams(tg, oracle.AbsMinSupp(cfg.SMin, db.Total()))
	p.MaxSupp = oracle.AbsMaxSupp(cfg.SMax, db.Total())
	p.MinSize = cfg.ZMin
	if cfg.ZMax < MaxSize {
		p.MaxSize = cfg.ZMax
	}
	sets, err := db.Mine(p)
	if err != nil {
		t.Fatal(err)
	}
	return sets
}

func set(supp tract.Supp, items ...string) oracle.Set {
	return oracle.Set{Items: oracle.NewItemSet(items...), Supp: supp}
}

func hasSet(sets []oracle.Set, s oracle.Set) bool {
	for _, x := range sets {
		if x.Items.Equal(s.Items) && x.Supp == s.Supp {
			return true
		}
	}
	return false
}

// TestDeviations pins the behavior at every fixed upstream bug: the
// result equals the oracle, and the set upstream gets wrong is (or is
// not) reported.
func TestDeviations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		data     string
		weighted bool
		opts     string
		want     oracle.Set
		present  bool // whether want must be reported
	}{
		// B1: -At crashes (SIGSEGV) upstream; x is a top-level perfect extension.
		{"B1 topdown item map", "x a b\nx a\nx b c\nx a b c\nx c\n", false,
			"-ts -At -s20", set(2, "a", "b", "x"), true},
		{"B1 topdown zmin filter", "a b\na c\nb c\nd\nd\nd\n", false,
			"-ts -At -m2 -q1 -s10", set(1, "a", "b"), true},
		// B2: -s0 crashes -As/-Ad upstream and reports support-0 sets with -Ac.
		{"B2 smin 0 simple", "a b\na c\nb c\nd\nd\nd\n", false,
			"-ts -As -s0", set(0, "a", "b", "c"), false},
		{"B2 smin 0 complex", "a b c\na d e\nb c d\na b c d\nb c\n", false,
			"-ts -Ac -s0", set(0, "a", "c", "d", "e"), false},
		{"B2 smin 0 single", "a b\na c\nb c\nd\nd\nd\n", false,
			"-ts -Ad -s-0", set(1, "d"), false},
		// B3: upstream prints nothing for -m2, but {a, b} is a generator.
		{"B3 generators zmin", "a\na b\na b\nb c\n", false,
			"-tg -s-1 -m2", set(2, "a", "b"), true},
		{"B3 generators zmin topdown", "a\na b\na b\nb c\n", false,
			"-tg -At -s-1 -m2", set(2, "a", "b"), true},
		// B4 (fixed in internal/report): maximal set of top-level pex only.
		{"B4 maximal top-level pex", "a\na b\na c\na\n", false,
			"-tm -s-2", set(4, "a"), true},
		// M9: -x with the 16-items machine, maximal set made of items in
		// every transaction (the machine registers them as perfect
		// extensions of the empty set, then B4 applies).
		{"M9 maximal -x simple", "a\n", false, "-tm -x -As -s-1", set(1, "a"), true},
		{"M9 maximal -x single", "a\n", false, "-tm -x -Ad -s-1", set(1, "a"), true},
		{"M9 maximal -x two items", "a b\na b c\na b\n", false,
			"-tm -x -As -s-2", set(3, "a", "b"), true},
		// B5: upstream reports {i1, i2} (not closed: {i0..i3} has support 4).
		{"B5 closed zmax simple", "i0 i3 i2 i1 x 2\ni0 i3 i2 i1 y 2\ni0 i3 5\ni0 3\ni2 z 1\n", true,
			"-tc -s-1 -n2 -q-1 -As -l0", set(4, "i1", "i2"), false},
		{"B5 closed zmax topdown", "i0 i3 i2 i1 x 2\ni0 i3 i2 i1 y 2\ni0 i3 5\ni0 3\ni2 z 1\n", true,
			"-tc -s-1 -n2 -q-1 -At", set(4, "i1", "i2"), false},
		// B5: upstream reports {i3, i7}, which is not maximal.
		{"B5 maximal zmax", "\ni5 i2 i0 i7 i3\ni2\ni1\ni1 i3 i7 i8\ni0 i5 i6\n", false,
			"-tm -s-1 -n2 -At -x", set(2, "i3", "i7"), false},
		{"B5 closed zmax -x complex", "a d 1\nd a 4\ne 4\nd a 5\na 4\na d b 2\ne a d 3\nd 4\n" +
			"e 1\ne 3\na e a 2\nc e d a 3\nc a d b a c 4\nc 1\ne d 3\na d e 2\n", true,
			"-tc -s-3 -n2 -q-1 -Ac -l0 -x", set(4, "b", "c"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			txs := txsOf(t, tt.data, tt.weighted)
			cfg, err := parseOpts(tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			res, err := run(scanOf(txs), cfg, runOpts{})
			if err != nil {
				t.Fatal(err)
			}
			want := oracleSets(t, txs, cfg)
			if d := oracle.DiffSets(want, res.sink.sets, oracle.DiffOptions{IgnoreEval: true}); d != "" {
				t.Errorf("differs from the oracle:\n%s", d)
			}
			if got := hasSet(res.sink.sets, tt.want); got != tt.present {
				t.Errorf("set %v reported: %v, want %v", tt.want, got, tt.present)
			}
		})
	}
}

// TestThresholds checks the threshold conversion against the oracle's
// independent implementation of upstream's formulas, the B2 floor and the
// B9 saturation.
func TestThresholds(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(7, 7))
	for range 20000 {
		w := tract.Supp(r.Int64N(1 << uint(r.IntN(40)+1)))
		var smin, smax float64
		switch r.IntN(3) {
		case 0:
			smin = -float64(r.IntN(100))
		case 1:
			smin = float64(r.IntN(10001)) / 100
		default:
			smin = r.Float64() * 100
		}
		smax = r.Float64() * 120
		if r.IntN(3) == 0 {
			smax = -float64(r.IntN(1000)) - r.Float64()
		}
		c := DefaultConfig()
		c.SMin, c.SMax = smin, smax
		th := computeThresholds(&c, w)
		if want := oracle.EffectiveMinSupp(oracle.AbsMinSupp(smin, w)); th.supp != want {
			t.Fatalf("smin %v w %d: supp %d, want %d", smin, w, th.supp, want)
		}
		if want := oracle.AbsMaxSupp(smax, w); th.smax != want {
			t.Fatalf("smax %v w %d: smax %d, want %d", smax, w, th.smax, want)
		}
		c.Target = report.TargetRules
		c.Conf = float64(r.IntN(101))
		th = computeThresholds(&c, w)
		body := oracle.AbsMinSupp(smin, w)
		if want := oracle.EffectiveMinSupp(oracle.RuleSetMinSupp(body, c.Conf/100)); th.supp != want {
			t.Fatalf("rules smin %v conf %v w %d: supp %d, want %d", smin, c.Conf, w, th.supp, want)
		}
		if th.body != oracle.EffectiveMinSupp(body) {
			t.Fatalf("rules smin %v w %d: body %d, want %d", smin, w, th.body, body)
		}
	}
	tests := []struct {
		name       string
		smin, smax float64
		w          tract.Supp
		supp, max  tract.Supp
	}{
		{"zero minimum (B2)", 0, 100, 10, 1, 10},
		{"negative zero minimum (B2)", math.Copysign(0, -1), 100, 10, 1, 10},
		{"huge absolute minimum (B9)", -1e300, 100, 10, math.MaxInt64, 10},
		{"beyond int32 (B9)", -3e9, 100, 10, 3e9, 10},
		{"huge absolute maximum (B9)", -1, -1e300, 10, 1, math.MaxInt64},
		{"huge percentage maximum (B9)", -1, 1e300, 10, 1, math.MaxInt64},
		{"infinite maximum", -1, math.Inf(1), 10, 1, math.MaxInt64},
		{"fraction absolute maximum", -1, -2.5, 10, 1, 2},
		{"tiny percentage", 1e-9, 100, 10, 1, 10},
	}
	for _, tt := range tests {
		c := DefaultConfig()
		c.SMin, c.SMax = tt.smin, tt.smax
		th := computeThresholds(&c, tt.w)
		if th.supp != tt.supp || th.smax != tt.max {
			t.Errorf("%s: supp %d smax %d, want %d %d", tt.name, th.supp, th.smax, tt.supp, tt.max)
		}
	}
}

// TestHugeMinSupport: an absolute minimum support beyond the int range is
// "no frequent items", not undefined behavior (B9).
func TestHugeMinSupport(t *testing.T) {
	t.Parallel()
	txs := txsOf(t, "a b\na\n", false)
	for _, smin := range []float64{-3e9, -1e300, math.Inf(-1)} {
		cfg := DefaultConfig()
		cfg.SMin = smin
		if _, err := run(scanOf(txs), cfg, runOpts{}); !errors.Is(err, ErrNoItems) {
			t.Errorf("smin %g: err %v, want ErrNoItems", smin, err)
		}
	}
}

// midDataset returns a deterministic dataset large enough for deep
// recursions, many item sets and all code paths of the variants.
func midDataset(tb testing.TB, n int) []oracle.Transaction {
	tb.Helper()
	txs, err := oracle.Generate(oracle.GenConfig{
		Seed: 42, NumTransactions: n, NumItems: 40, MinLen: 3, MaxLen: 12,
		Skew: 1, Patterns: 8, PatternLen: 6, PatternProb: 0.6,
		DupTxProb: 0.05, Weights: oracle.WeightsSmall, Names: oracle.NamesLetters,
	})
	if err != nil {
		tb.Fatal(err)
	}
	return txs
}

// TestCancel checks that every variant stops with ctx.Err() when the
// context is canceled before or during mining.
func TestCancel(t *testing.T) {
	t.Parallel()
	txs := midDataset(t, 300)
	for _, tg := range targets {
		for _, v := range variants {
			cfg := propCase{smin: -3, smax: 100, zmin: 1, zmax: MaxSize, sort: 2}.config(v, tg.target)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := run(scanOf(txs), cfg, runOpts{ctx: ctx})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("%s/%s: canceled before: err %v", tg.name, v.name, err)
			}
			ctx, cancel = context.WithCancel(context.Background())
			sink := &collectSink{onSet: cancel}
			_, err = run(scanOf(txs), cfg, runOpts{ctx: ctx, sink: sink})
			cancel()
			if !errors.Is(err, context.Canceled) {
				t.Errorf("%s/%s: canceled during mining: err %v after %d sets",
					tg.name, v.name, err, len(sink.sets))
			}
		}
	}
}

// TestSinkError checks that a sink error stops every variant and is
// returned unchanged.
func TestSinkError(t *testing.T) {
	t.Parallel()
	txs := midDataset(t, 300)
	for _, tg := range targets {
		for _, v := range variants {
			cfg := propCase{smin: -3, smax: 100, zmin: 1, zmax: MaxSize, sort: 2}.config(v, tg.target)
			for _, at := range []int{1, 2, 17} {
				sink := &collectSink{failAt: at}
				_, err := run(scanOf(txs), cfg, runOpts{sink: sink})
				if err != errSink { //nolint:errorlint // must be returned unchanged
					t.Errorf("%s/%s fail at %d: err %v", tg.name, v.name, at, err)
				}
				if sink.cnt != at {
					t.Errorf("%s/%s fail at %d: sink called %d times", tg.name, v.name, at, sink.cnt)
				}
			}
		}
	}
}

// TestStateAndPaths covers the call order checks, the rules hook and the
// convenience function.
func TestStateAndPaths(t *testing.T) {
	t.Parallel()
	txs := txsOf(t, "a b c\na d e\nb c d\na b c d\nb c\na b d\nd e\na b c d\nc d e\na b c\n", false)
	scan := scanOf(txs)
	m, err := New[string](DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Mine(context.Background()); !errors.Is(err, ErrState) {
		t.Errorf("Mine before Data: %v", err)
	}
	if _, err := m.Reporter(); !errors.Is(err, ErrState) {
		t.Errorf("Reporter before Data: %v", err)
	}
	ib := tract.NewItemBase[string]()
	if err := ib.Count(scan); err != nil {
		t.Fatal(err)
	}
	if err := m.Data(ib, scan); err != nil {
		t.Fatal(err)
	}
	if err := m.Data(ib, scan); !errors.Is(err, ErrState) {
		t.Errorf("Data twice: %v", err)
	}
	if m.Bag() == nil || m.MinSupport() != 1 || m.BodySupport() != 1 || m.MaxSupport() != 10 {
		t.Errorf("bag %v supp %d body %d smax %d", m.Bag(), m.MinSupport(), m.BodySupport(), m.MaxSupport())
	}
	rep, err := m.Reporter()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Mine(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Mine(context.Background()); !errors.Is(err, ErrState) {
		t.Errorf("Mine twice: %v", err)
	}
	cfg := DefaultConfig()
	cfg.SMin = -2
	sink := &collectSink{ib: tract.NewItemBase[string]()}
	ib2 := tract.NewItemBase[string]()
	sink.ib = ib2
	rep2, err := Mine(context.Background(), cfg, ib2, scan, sink)
	if err != nil {
		t.Fatal(err)
	}
	if d := oracle.DiffSets(oracleSets(t, txs, cfg), sink.sets, oracle.DiffOptions{}); d != "" {
		t.Errorf("Mine:\n%s", d)
	}
	if rep2.Count() != uint64(len(sink.sets)) || rep.Count() == 0 {
		t.Errorf("counts %d (sets %d), %d", rep2.Count(), len(sink.sets), rep.Count())
	}
	for _, c := range []Config{
		func() Config { c := DefaultConfig(); c.Target = report.TargetRules; return c }(),
		func() Config { c := DefaultConfig(); c.Eval = 1; return c }(),
		func() Config {
			c := DefaultConfig()
			c.Eval = 25
			c.Thresh = 100
			c.Target = report.TargetClosed
			return c
		}(),
	} {
		rep, err := Mine(context.Background(), c, tract.NewItemBase[string](), scan, nil)
		if err != nil || rep.Count() == 0 {
			t.Errorf("rules path %+v: %v", c, err)
		}
	}
	if _, err := Mine(context.Background(), Config{SMin: math.NaN()}, tract.NewItemBase[string](), scan, nil); !errors.Is(err, ErrConfig) {
		t.Errorf("invalid config: %v", err)
	}
	empty := scanOf(nil)
	if _, err := Mine(context.Background(), DefaultConfig(), tract.NewItemBase[string](), empty, nil); !errors.Is(err, ErrNoItems) {
		t.Errorf("empty database: %v", err)
	}
	failing := func(func([]string, tract.Supp) bool) error { return errSink }
	if _, err := Mine(context.Background(), DefaultConfig(), tract.NewItemBase[string](), failing, nil); !errors.Is(err, errSink) {
		t.Errorf("scan error: %v", err)
	}
}

// TestLogRatio checks the -eb evaluation path (reporter-side evaluation).
func TestLogRatio(t *testing.T) {
	t.Parallel()
	txs := midDataset(t, 200)
	cfg := DefaultConfig()
	cfg.SMin, cfg.Eval, cfg.Thresh = -5, EvalLogRatio, 50
	all, err := run(scanOf(txs), func() Config { c := cfg; c.Eval = EvalNone; return c }(), runOpts{})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := run(scanOf(txs), cfg, runOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.sink.sets) == 0 || len(ev.sink.sets) >= len(all.sink.sets) {
		t.Errorf("evaluation kept %d of %d sets", len(ev.sink.sets), len(all.sink.sets))
	}
	if !isSubsequence(ev.sink.lines, all.sink.lines) {
		t.Errorf("evaluated sets are not a subsequence of all sets")
	}
}

// TestLogRatioOracle compares the -eb evaluation (values and filter) of
// every target and variant with the oracle's independent reference
// (oracle.LogRatioEval, issue #15).
func TestLogRatioOracle(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(15, 26))
	cases := 300
	if testing.Short() {
		cases = 60
	}
	thresholds := []float64{-1000, -50, 0, 10, 50, 100, 300}
	runs := 0
	for seed := range uint64(cases) {
		c, err := genCase(seed+12000, 8)
		if err != nil {
			t.Fatal(err)
		}
		db, err := oracle.New(c.txs, oracle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		tg := targets[r.IntN(len(targets))]
		v := variants[r.IntN(len(variants))]
		cfg := c.config(v, tg.target)
		cfg.Eval = EvalLogRatio
		cfg.Thresh = thresholds[r.IntN(len(thresholds))]
		noFilter := r.IntN(4) == 0
		cfg.EvalNoFilter = noFilter
		total := db.Total()
		p := oracle.NewSetParams(tg.oracle, oracle.AbsMinSupp(cfg.SMin, total))
		p.MaxSupp = oracle.AbsMaxSupp(cfg.SMax, total)
		p.MinSize = cfg.ZMin
		if cfg.ZMax < MaxSize {
			p.MaxSize = cfg.ZMax
		}
		p.LogRatio = &oracle.LogRatioEval{Threshold: cfg.Thresh / 100}
		if noFilter {
			p.LogRatio.Threshold = math.Inf(-1)
		}
		want, err := db.Mine(p)
		if err != nil {
			t.Fatal(err)
		}
		res, err := run(scanOf(c.txs), cfg, runOpts{})
		runs++
		if errors.Is(err, ErrNoItems) {
			continue // the unevaluated sets are checked by TestOracle
		}
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if d := oracle.DiffSets(want, res.sink.sets, oracle.DiffOptions{}); d != "" {
			t.Fatalf("seed %d %s/%s %s -eb -d%g:\n%s", seed, tg.name, v.name, describe(cfg), cfg.Thresh, d)
		}
	}
	t.Logf("%d runs", runs)
}

// evalSink records the evaluation of every reported item set by its
// collectSink line ("names [supp]").
type evalSink struct {
	collectSink
	evals map[string]float64
}

func (s *evalSink) ItemSet(is report.ItemSet) error {
	if err := s.collectSink.ItemSet(is); err != nil {
		return err
	}
	s.evals[s.lines[len(s.lines)-1]] = is.Eval
	return nil
}

// TestLogRatioIndependenceBoundary pins the -eb value of an item set
// whose items are exactly independent (docs/differences.md F-1, stage 1
// review finding F1). In "a b / a / b / b / x / x" the set {a, b} has
// supp(ab)*N = 1*6 = supp(a)*supp(b) = 2*3, so its binary log support
// quotient is exactly 0 and -d0 (threshold 0, "must meet or exceed")
// keeps it. The port computes it as log2(1/6) - (log2(2)-log2(6)) -
// (log2(3)-log2(6)) with Go's math.Log, which gives exactly +0 here.
//
// Upstream's result depends on the C library: Apple's libm rounds
// log(3) correctly to 0x1.193ea7aad030bp+0, Go's math.Log returns the
// value one ulp below (0x1.193ea7aad030ap+0, within its documented
// error), and with Apple's value the same expression is -2^-52, so the C
// reference (macOS/arm64) omits {a, b} (fpgrowth -ts -s-1 -eb -d0). The
// port keeps the standard math package; this test fixes its behavior so
// that a change of the computation or of math.Log shows up here.
func TestLogRatioIndependenceBoundary(t *testing.T) {
	t.Parallel()
	if got := math.Float64bits(math.Log(3)); got != 0x3ff193ea7aad030a {
		t.Errorf("math.Log(3) = %#x, the test assumes 0x3ff193ea7aad030a", got)
	}
	txs := txsOf(t, "a b\na\nb\nb\nx\nx\n", false)
	mine := func(thresh float64) *evalSink {
		t.Helper()
		cfg := DefaultConfig()
		cfg.SMin, cfg.Eval, cfg.Thresh = -1, EvalLogRatio, thresh
		ib := tract.NewItemBase[string]()
		m, err := New[string](cfg)
		if err != nil {
			t.Fatal(err)
		}
		scan := scanOf(txs)
		if err := ib.Count(scan); err != nil {
			t.Fatal(err)
		}
		sink := &evalSink{collectSink: collectSink{ib: ib}, evals: map[string]float64{}}
		if err := m.Data(ib, scan); err != nil {
			t.Fatal(err)
		}
		rep, err := m.Reporter()
		if err != nil {
			t.Fatal(err)
		}
		rep.SetSink(sink)
		if err := m.Mine(context.Background()); err != nil {
			t.Fatal(err)
		}
		return sink
	}

	tests := []struct {
		name   string
		thresh float64 // -d, percent
		want   []string
	}{
		{name: "threshold 0 keeps the independent pair", thresh: 0, want: []string{"b [3]", "a b [1]", "a [2]", "x [2]"}},
		// single items have the value 0 as well: every set is at the boundary
		{name: "smallest positive threshold drops every set", thresh: 5e-322, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sink := mine(tt.thresh)
			if !slices.Equal(sink.lines, tt.want) {
				t.Errorf("-d%g: sets %q, want %q", tt.thresh, sink.lines, tt.want)
			}
			if v, ok := sink.evals["a b [1]"]; ok && math.Float64bits(v) != 0 {
				t.Errorf("-eb value of {a, b} = %v (%#x), want exactly +0", v, math.Float64bits(v))
			}
		})
	}
}

// fuzzBytes reads a fuzz input byte by byte; past the end it yields 0.
type fuzzBytes struct {
	b []byte
	i int
}

func (r *fuzzBytes) next() int {
	if r.i >= len(r.b) {
		return 0
	}
	v := r.b[r.i]
	r.i++
	return int(v)
}

// decodeFuzzCase decodes a small dataset (at most 10 items, 24
// transactions, weights 0-3) and the item set options of propCase from
// fuzz input.
func decodeFuzzCase(r *fuzzBytes) propCase {
	numItems := 1 + r.next()%10
	numTx := r.next() % 25
	weighted := r.next()%2 == 1
	names := make([]string, numItems)
	for i := range names {
		names[i] = string(rune('a' + i))
	}
	c := propCase{zmax: MaxSize, smax: 100, zmin: 1}
	for range numTx {
		mask := r.next() | r.next()<<8
		var items []string
		for i, n := range names {
			if mask&(1<<i) != 0 {
				items = append(items, n)
			}
		}
		if items == nil {
			items = []string{}
		}
		w := tract.Supp(1)
		if weighted {
			w = tract.Supp(r.next() % 4)
		}
		c.txs = append(c.txs, oracle.Transaction{Items: items, Weight: w})
	}
	switch r.next() % 4 {
	case 0:
		c.smin = float64(-(1 + r.next()%5))
	case 1:
		c.smin = float64(r.next() % 101) // percentage, includes 0 (B2)
	default:
		c.smin = -1
	}
	if k := r.next(); k%4 == 0 {
		c.zmin = k / 4 % 4
	}
	if k := r.next(); k%3 == 0 {
		c.zmax = c.zmin + k/3%4
	}
	if k := r.next(); k%5 == 0 {
		c.smax = float64(-(2 + k/5%10))
	}
	flags := r.next()
	c.noPerf = flags&1 != 0
	c.noTail = flags&2 != 0
	c.sort = []int{2, 2, 1, 0, -1, -2}[r.next()%6]
	if flags&4 != 0 {
		c.border = []float64{float64(-(1 + r.next()%3)), float64(-(1 + r.next()%4))}
	}
	return c
}

// checkFuzzRules mines association rules of c's data with options read
// from r on every item set tree variant and compares them with the
// oracle.
func checkFuzzRules(t *testing.T, c propCase, r *fuzzBytes) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Target = report.TargetRules
	cfg.SMin = c.smin
	cfg.SMax = []float64{100, 100, 60, -3, -1, 0}[r.next()%6]
	cfg.Conf = []float64{0, 30, 50, 80, 100}[r.next()%5]
	cfg.ZMin = r.next() % 3
	cfg.ZMax = []int{MaxSize, 1, 2, 3}[r.next()%4]
	cfg.Sort = c.sort
	flags := r.next()
	if flags&1 != 0 {
		cfg.Mode |= ModeOrigSupp
	}
	cfg.InvalidateBelowExpected = flags&2 != 0
	switch k := r.next(); k % 6 {
	case 0:
		cfg.Eval = EvalNone
	case 1:
		cfg.Eval = EvalLogRatio // the imbalance ratio for rules (R1)
	default:
		cfg.Eval = 1 + k/6%(int(ruleval.NumMeasures)-1)
	}
	cfg.Thresh = treeThresholds[r.next()%len(treeThresholds)]
	db, err := oracle.New(c.txs, oracle.Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := oracle.NewRuleParams(oracle.EffectiveMinSupp(oracle.AbsMinSupp(cfg.SMin, db.Total())), cfg.Conf/100)
	var app *appSpec
	if flags&4 != 0 {
		app = &appSpec{def: tract.Appearance(r.next() % 4)}
		p.DefaultAppear = oracleAppear(app.def)
		p.Appear = map[string]oracle.Appearance{}
		for _, it := range db.Items() {
			if k := r.next(); k%2 == 0 {
				a := tract.Appearance(k / 2 % 4)
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
	for _, v := range treeVariants {
		vc := cfg
		vc.Algo, vc.Pack = v.algo, v.pack
		vc.Mode &^= v.clear
		sink, _, err := runTree(t, c.txs, vc, app)
		switch {
		case errors.Is(err, ErrNoItems):
			if len(want) > 0 {
				t.Fatalf("rules %s: ErrNoItems, oracle has %d rules", v.name, len(want))
			}
		case err != nil:
			t.Fatalf("rules %s: %v", v.name, err)
		default:
			if d := oracle.DiffRules(want, sink.rules, oracle.DiffOptions{}); d != "" {
				t.Fatalf("rules %s config %+v app %+v:\n%s", v.name, vc, app, d)
			}
		}
	}
}

// FuzzMine mines a small dataset decoded from the fuzz input with every
// target (frequent, closed, maximal, generators: all variants of
// checkCase; rules: all item set tree variants) and compares the results
// with the brute-force oracle.
//
//	go test ./internal/fpg/ -run '^$' -fuzz FuzzMine -fuzztime 60s
func FuzzMine(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("\x04\x06\x00\x03\x00\x01\x00\x05\x00\x07\x00\x0f\x00\x02\x00\x00\x00\x00\x00\x00\x00"))
	f.Add([]byte("\x09\x18\x01\xff\x01\x02\x13\x02\x01\x37\x00\x03\x01\x00\x00\x01\x02\x03\x04\x05\x06\x07"))
	f.Add([]byte("independence and ties: a b / a / b / b / x / x"))
	for seed := range uint64(8) {
		r := rand.New(rand.NewPCG(seed, 99))
		b := make([]byte, 64)
		for i := range b {
			b[i] = byte(r.IntN(256))
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		r := &fuzzBytes{b: data}
		c := decodeFuzzCase(r)
		checkCase(t, c)
		checkFuzzRules(t, c, r)
	})
}

// TestCountThresholds checks the exact absolute thresholds of
// Config.HasCounts: equal to the float notation where that is exact,
// exact beyond 2^53, and never above the body minimum for rules.
func TestCountThresholds(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(8, 8))
	for range 20000 {
		w := tract.Supp(r.Int64N(1 << 40))
		n := tract.Supp(r.Int64N(1 << uint(r.IntN(40)+1)))
		x := tract.Supp(r.Int64N(1 << uint(r.IntN(40)+1)))
		for _, rules := range []bool{false, true} {
			c := DefaultConfig()
			if rules {
				c.Target = report.TargetRules
				c.Conf = float64(r.IntN(101))
			}
			c.SMin, c.SMax = -float64(n), -float64(x)
			want := computeThresholds(&c, w)
			c.HasCounts, c.MinCount, c.MaxCount = true, n, x
			if got := computeThresholds(&c, w); got != want {
				t.Fatalf("rules=%v n %d x %d conf %v: counts %+v, float %+v", rules, n, x, c.Conf, got, want)
			}
		}
	}
	for _, n := range []tract.Supp{1<<53 + 1, 1<<60 + 1, math.MaxInt64 - 1, math.MaxInt64} {
		c := DefaultConfig()
		c.HasCounts, c.MinCount, c.MaxCount = true, n, n
		if th := computeThresholds(&c, n); th.supp != n || th.body != n || th.smax != n {
			t.Errorf("count %d: %+v", n, th)
		}
		c.Target, c.Conf = report.TargetRules, 100
		th := computeThresholds(&c, n)
		// the set minimum is a lower bound of the support of a rule with
		// body support >= n and confidence 1: n itself, minus the float
		// tolerance
		if th.body != n || th.supp > n || float64(th.supp) < float64(n)*(1-0x1p-50) || th.smax != n {
			t.Errorf("rules count %d: %+v", n, th)
		}
	}
}

// TestEvalNoFilter checks that Config.EvalNoFilter reports the item sets
// of the unevaluated run, with a rule measure and with LogRatio.
func TestEvalNoFilter(t *testing.T) {
	t.Parallel()
	txs := midDataset(t, 200)
	base := DefaultConfig()
	base.SMin = -20
	plain, err := run(scanOf(txs), base, runOpts{})
	if err != nil {
		t.Fatal(err)
	}
	// thresholds that no value passes (p-values are a maximum)
	for eval, thresh := range map[int]float64{int(ruleval.Lift): 1e300, int(ruleval.Chi2PValue): -100, EvalLogRatio: 1e300} {
		cfg := base
		cfg.Eval, cfg.Thresh, cfg.EvalNoFilter = eval, thresh, true
		got, err := run(scanOf(txs), cfg, runOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if d := oracle.DiffSets(plain.sink.sets, got.sink.sets, oracle.DiffOptions{IgnoreEval: true}); d != "" {
			t.Errorf("eval %d: %s", eval, d)
		}
		cfg.EvalNoFilter = false
		filtered, err := run(scanOf(txs), cfg, runOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(filtered.sink.sets) >= len(plain.sink.sets) {
			t.Errorf("eval %d: the threshold filters nothing (%d sets)", eval, len(filtered.sink.sets))
		}
	}
}
