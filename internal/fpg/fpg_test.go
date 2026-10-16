package fpg

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
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
		func() Config { c := DefaultConfig(); c.Eval = 25; c.Target = report.TargetClosed; return c }(),
	} {
		_, err := Mine(context.Background(), c, tract.NewItemBase[string](), scan, nil)
		if !errors.Is(err, ErrRulesNotImplemented) {
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
