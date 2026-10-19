package fpgrowth

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

var confidences = []float64{0, 30, 50, 80, 100} // percent

// TestRulesEquivalenceCLI compares rules (with -o, -S, sizes, every rule
// measure, -i) with the command line program's pipeline, in order and
// with bit-identical values.
func TestRulesEquivalenceCLI(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(41, 42))
	thresholds := []float64{-100, 0, 1, 10, 50, 90, 100, 150, 1000}
	for seed := range uint64(numCases(400)) {
		c, err := genTestCase(seed+5000, 8)
		if err != nil {
			t.Fatal(err)
		}
		v := algorithms[r.IntN(len(algorithms))]
		conf := confidences[r.IntN(len(confidences))]
		opt := RuleOptions[string]{
			MinSupport:      c.minSupp,
			MaxSupport:      c.maxSupp,
			MinConfidence:   conf / 100,
			MinSize:         max(c.zmin, 1),
			OriginalSupport: r.IntN(3) == 0,
			Algorithm:       v.algo,
			Tuning:          merge(v.tun, c.tuning),
		}
		zmax := fpg.MaxSize
		if r.IntN(3) == 0 {
			opt.MaxSize = opt.MinSize + r.IntN(3)
			zmax = opt.MaxSize
		}
		cfg := cliConfig(report.TargetRules, v.algo, opt.Tuning, c.smin, c.smax, opt.MinSize, zmax)
		cfg.Conf = conf
		if opt.OriginalSupport {
			cfg.Mode |= fpg.ModeOrigSupp
		}
		if r.IntN(2) == 0 {
			m := Measure(1 + r.IntN(int(LogSupportRatio)-1))
			d := thresholds[r.IntN(len(thresholds))]
			opt.Evaluation = &Evaluation{Measure: m, Filter: true, Threshold: d / 100, InvalidateBelowExpected: r.IntN(4) == 0}
			cfg.Eval, cfg.Thresh, cfg.InvalidateBelowExpected = int(m), d, opt.Evaluation.InvalidateBelowExpected
		}
		want, _, werr := runInternal(t, c.txs, cfg, nil, nil)
		got, sum, gerr := ruleRecords(t, sourceOf(c.txs), opt)
		if gerr != nil {
			t.Fatal(gerr)
		}
		if errors.Is(werr, fpg.ErrNoItems) {
			if len(got) != 0 || sum.FrequentItems != 0 {
				t.Fatalf("seed %d: CLI finds no items, API reports %d rules", seed, len(got))
			}
			continue
		}
		if werr != nil {
			t.Fatal(werr)
		}
		if d := diffRecords(want, got, opt.Evaluation != nil); d != "" {
			t.Fatalf("seed %d %s opt %+v: %s", seed, v.name, opt, d)
		}
	}
}

// TestRulesOracle compares rules, including appearances, with the
// brute-force oracle; confidence and lift must equal the oracle's values.
// With an evaluation, the rules are compared twice: with the
// implementation's measure function (bit-identical values and threshold
// decisions) and with the independent reference measure (values within
// their tolerances), which catches formula errors.
func TestRulesOracle(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(51, 52))
	pubApp := []Appearance{AppearBoth, AppearBody, AppearHead, AppearNone}
	oraApp := []oracle.Appearance{oracle.AppearBoth, oracle.AppearBody, oracle.AppearHead, oracle.AppearNone}
	rules := 0
	for seed := range uint64(numCases(500)) {
		c, err := genTestCase(seed+6000, 8)
		if err != nil {
			t.Fatal(err)
		}
		db, err := oracle.New(c.txs, oracle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		total := db.Total()
		conf := confidences[r.IntN(len(confidences))] / 100
		opt := RuleOptions[string]{
			MinSupport:      c.minSupp,
			MaxSupport:      c.maxSupp,
			MinConfidence:   conf,
			MinSize:         r.IntN(3),
			OriginalSupport: r.IntN(3) == 0,
			Algorithm:       Algorithm(r.IntN(5)),
		}
		p := oracle.NewRuleParams(oracle.EffectiveMinSupp(oracle.AbsMinSupp(c.smin, total)), conf)
		p.MaxSupp = oracle.AbsMaxSupp(c.smax, total)
		p.OrigSupp = opt.OriginalSupport
		p.MinSize = opt.MinSize
		if r.IntN(3) == 0 {
			opt.MaxSize = max(opt.MinSize, 1) + r.IntN(3)
			p.MaxSize = opt.MaxSize
		}
		if r.IntN(3) == 0 {
			k := r.IntN(4)
			opt.DefaultAppearance, p.DefaultAppear = pubApp[k], oraApp[k]
			opt.Appearance = map[string]Appearance{}
			p.Appear = map[string]oracle.Appearance{}
			for _, it := range append(db.Items(), "never-occurs") {
				if r.IntN(2) == 0 {
					k := r.IntN(4)
					opt.Appearance[it], p.Appear[it] = pubApp[k], oraApp[k]
				}
			}
		}
		if r.IntN(2) == 0 {
			m := Measure(1 + r.IntN(int(LogSupportRatio)-1))
			opt.Evaluation = &Evaluation{Measure: m, Filter: true, Threshold: []float64{0, 0.1, 0.5, 1, 2}[r.IntN(5)],
				InvalidateBelowExpected: r.IntN(4) == 0}
			if r.IntN(5) == 0 {
				opt.Evaluation.Filter, opt.Evaluation.Threshold = false, 0 // values only
			}
			rm := ruleval.Measure(m)
			p.Eval = &oracle.Measure{Func: oracle.MeasureFunc(rm.Func()), Dir: oracle.Direction(rm.Dir()),
				Threshold: refMeasure(t, opt.Evaluation).Threshold, InvalidateBelowExpected: opt.Evaluation.InvalidateBelowExpected}
		}
		want, err := db.Rules(p)
		if err != nil {
			t.Fatal(err)
		}
		got, sum, err := CollectRules(context.Background(), sourceOf(c.txs), opt)
		if err != nil {
			t.Fatal(err)
		}
		if sum.FrequentItems == 0 {
			if len(want) > 0 {
				t.Fatalf("seed %d: no frequent items, oracle has %d rules", seed, len(want))
			}
			continue
		}
		rules += len(got)
		if d := oracle.DiffRules(want, toOracleRules(got), oracle.DiffOptions{IgnoreEval: opt.Evaluation == nil}); d != "" {
			t.Fatalf("seed %d opt %+v:\n%s", seed, opt, d)
		}
		if opt.Evaluation != nil {
			p.Eval = refMeasure(t, opt.Evaluation)
			ref, err := db.Rules(p)
			if err != nil {
				t.Fatal(err)
			}
			if d := oracle.DiffRules(ref, toOracleRules(got), oracle.DiffOptions{}); d != "" {
				t.Fatalf("seed %d opt %+v eval %+v: reference measure:\n%s", seed, opt, *opt.Evaluation, d)
			}
		}
		byKey := map[string]oracle.Rule{}
		for _, w := range want {
			byKey[oracle.RuleKey(w.Head, w.Body)] = w
		}
		for _, g := range toOracleRules(got) {
			w := byKey[oracle.RuleKey(g.Head, g.Body)]
			if math.Float64bits(w.Conf) != math.Float64bits(g.Conf) || math.Float64bits(w.Lift) != math.Float64bits(g.Lift) {
				t.Fatalf("seed %d %v: conf/lift %v/%v, oracle %v/%v", seed, g, g.Conf, g.Lift, w.Conf, w.Lift)
			}
			if opt.Evaluation == nil && !math.IsNaN(g.Eval) {
				t.Fatalf("seed %d %v: value without evaluation", seed, g)
			}
		}
	}
	t.Logf("%d rules checked", rules)
}

// TestRulesAppearanceEquivalence compares rules with appearances with the
// CLI pipeline (appearance file registered up front) as sets: the order
// may differ only among items of equal frequency.
func TestRulesAppearanceEquivalence(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(61, 62))
	for seed := range uint64(numCases(200)) {
		c, err := genTestCase(seed+7000, 8)
		if err != nil {
			t.Fatal(err)
		}
		db, err := oracle.New(c.txs, oracle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		def := Appearance(r.IntN(4))
		app := map[string]Appearance{}
		var names []string
		for _, it := range db.Items() {
			if r.IntN(2) == 0 {
				app[it] = Appearance(r.IntN(4))
				names = append(names, it)
			}
		}
		cfg := cliConfig(report.TargetRules, AlgorithmAuto, Tuning{}, c.smin, c.smax, 1, fpg.MaxSize)
		cfg.Conf = 50
		want, _, werr := runInternal(t, c.txs, cfg, nil, func(ib *tract.ItemBase[string]) error {
			ib.SetDefaultAppearance(def.internal())
			for _, it := range names {
				if err := ib.AddAppearance(it, app[it].internal()); err != nil {
					return err
				}
			}
			return nil
		})
		got, _, gerr := CollectRules(context.Background(), sourceOf(c.txs), RuleOptions[string]{
			MinSupport: c.minSupp, MaxSupport: c.maxSupp, MinConfidence: 0.5,
			Appearance: app, DefaultAppearance: def,
		})
		if gerr != nil {
			t.Fatal(gerr)
		}
		if errors.Is(werr, fpg.ErrNoItems) {
			if len(got) != 0 {
				t.Fatalf("seed %d: CLI finds no items, API reports %d rules", seed, len(got))
			}
			continue
		}
		if werr != nil {
			t.Fatal(werr)
		}
		if len(want) != len(got) {
			t.Fatalf("seed %d: %d rules, CLI %d", seed, len(got), len(want))
		}
	}
}

// TestRulesDeterministicAppearance checks that the output order does not
// depend on the iteration order of the appearance map.
func TestRulesDeterministicAppearance(t *testing.T) {
	t.Parallel()
	var txs [][]string
	for i := range 40 {
		var tx []string
		for j := range 12 {
			if (i+j)%3 != 0 { // many equal frequencies
				tx = append(tx, fmt.Sprintf("i%02d", j))
			}
		}
		txs = append(txs, tx)
	}
	app := map[string]Appearance{}
	for j := range 12 {
		app[fmt.Sprintf("i%02d", j)] = Appearance(j % 3)
	}
	var first string
	for range 20 {
		got, _, err := CollectRules(context.Background(), FromItems(txs), RuleOptions[string]{
			MinSupport: Fraction(0.5), MinConfidence: 0.8, MaxSize: 3, Appearance: app,
		})
		if err != nil {
			t.Fatal(err)
		}
		if s := fmt.Sprint(got); first == "" {
			first = s
		} else if s != first {
			t.Fatal("output order depends on the map iteration order")
		}
	}
}

// TestRuleValues pins confidence, lift and the evaluation value of a
// small example.
func TestRuleValues(t *testing.T) {
	t.Parallel()
	src := FromItems([][]string{{"a", "b"}, {"a", "b"}, {"a"}, {"b"}, {"c"}})
	got, _, err := CollectRules(context.Background(), src, RuleOptions[string]{
		MinSupport: Count(2), MinConfidence: 0.6, MinSize: 2,
		Evaluation: &Evaluation{Measure: Lift, Filter: true, Threshold: 1.1},
	})
	if err != nil {
		t.Fatal(err)
	}
	// b <- a and a <- b: supp 2, body 3, head 3, conf 2/3, lift 2*5/9
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	for _, r := range got {
		if r.Support != 2 || r.BodySupport != 3 || r.HeadSupport != 3 || r.Confidence != 2.0/3 ||
			r.Lift != 10.0/9 || r.Value != 10.0/9 || len(r.Body) != 1 {
			t.Errorf("rule %+v", r)
		}
	}
	got, _, err = CollectRules(context.Background(), src, RuleOptions[string]{
		MinSupport: Count(2), MinConfidence: 0.6, MinSize: 2,
		Evaluation: &Evaluation{Measure: Lift, Filter: true, Threshold: 1.12},
	})
	if err != nil || len(got) != 0 {
		t.Errorf("threshold above lift: %v %v", got, err)
	}
}

// FuzzMineRules compares MineRules with the brute-force oracle for
// fuzzed databases and random options: thresholds, sizes, the support
// definition, appearances, and evaluations with and without filtering.
func FuzzMineRules(f *testing.F) {
	fuzzSeeds(f)
	pubApp := []Appearance{AppearBoth, AppearBody, AppearHead, AppearNone}
	oraApp := []oracle.Appearance{oracle.AppearBoth, oracle.AppearBody, oracle.AppearHead, oracle.AppearNone}
	f.Fuzz(func(t *testing.T, word uint64, data []byte) {
		txs := decodeFuzzTxs(data)
		b := fuzzBits(word)
		db, err := oracle.New(txs, oracle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		total := db.Total()
		var smin, smax float64 = 0, 100
		var minSupp, maxSupp Support
		if b.take(1) == 0 {
			n := int64(1 + b.take(2))
			smin, minSupp = float64(-n), Count(n)
		} else {
			k := 1 + b.take(6)%60
			smin, minSupp = float64(k), Fraction(float64(k)/100)
		}
		switch b.take(2) {
		case 1:
			n := int64(1 + b.take(3))
			smax, maxSupp = float64(-n), Count(n)
		case 2:
			k := 30 + b.take(6)
			smax, maxSupp = float64(k), Fraction(float64(k)/100)
		}
		conf := confidences[b.take(3)%len(confidences)] / 100
		opt := RuleOptions[string]{
			MinSupport:      minSupp,
			MaxSupport:      maxSupp,
			MinConfidence:   conf,
			MinSize:         b.take(2) % 3,
			OriginalSupport: b.take(1) == 1,
			Algorithm:       Algorithm(b.take(3) % 5),
		}
		p := oracle.NewRuleParams(oracle.EffectiveMinSupp(oracle.AbsMinSupp(smin, total)), conf)
		p.MaxSupp = oracle.AbsMaxSupp(smax, total)
		p.OrigSupp = opt.OriginalSupport
		p.MinSize = opt.MinSize
		if b.take(1) == 1 {
			opt.MaxSize = max(opt.MinSize, 1) + b.take(2)
			p.MaxSize = opt.MaxSize
		}
		if b.take(1) == 1 {
			k := b.take(2)
			opt.DefaultAppearance, p.DefaultAppear = pubApp[k], oraApp[k]
			opt.Appearance = map[string]Appearance{}
			p.Appear = map[string]oracle.Appearance{}
			for _, it := range db.Items() {
				if b.take(1) == 1 {
					k := b.take(2)
					opt.Appearance[it], p.Appear[it] = pubApp[k], oraApp[k]
				}
			}
		}
		evalMode := b.take(2) // 0, 1: none, 2: values only, 3: filter
		if evalMode >= 2 {
			m := Measure(1 + b.take(5)%(int(LogSupportRatio)-1))
			opt.Evaluation = &Evaluation{Measure: m, InvalidateBelowExpected: b.take(1) == 1}
			if evalMode == 3 {
				opt.Evaluation.Filter = true
				opt.Evaluation.Threshold = []float64{0, 0.1, 0.5, 1, 2, -1, 0.01, 10}[b.take(3)]
				rm := ruleval.Measure(m)
				p.Eval = &oracle.Measure{Func: oracle.MeasureFunc(rm.Func()), Dir: oracle.Direction(rm.Dir()),
					Threshold: opt.Evaluation.Threshold, InvalidateBelowExpected: opt.Evaluation.InvalidateBelowExpected}
			}
		}
		got, sum, err := CollectRules(context.Background(), sourceOf(txs), opt)
		if invertedRange(minSupp, maxSupp) {
			checkInvertedRange(t, minSupp, maxSupp, err)
			return
		}
		if err != nil {
			t.Fatalf("%+v: %v", opt, err)
		}
		if sum.Reported != uint64(len(got)) {
			t.Fatalf("reported %d, got %d", sum.Reported, len(got))
		}
		want, err := db.Rules(p)
		if err != nil {
			t.Fatal(err)
		}
		if sum.FrequentItems == 0 {
			if len(want) > 0 || len(got) > 0 {
				t.Fatalf("no frequent items: oracle %d rules, got %d", len(want), len(got))
			}
			return
		}
		if d := oracle.DiffRules(want, toOracleRules(got), oracle.DiffOptions{IgnoreEval: evalMode != 3}); d != "" {
			t.Fatalf("opt %+v eval %+v:\n%s", opt, opt.Evaluation, d)
		}
		if opt.Evaluation != nil {
			p.Eval = refMeasure(t, opt.Evaluation)
			ref, err := db.Rules(p)
			if err != nil {
				t.Fatal(err)
			}
			if d := oracle.DiffRules(ref, toOracleRules(got), oracle.DiffOptions{}); d != "" {
				t.Fatalf("opt %+v eval %+v: reference measure:\n%s", opt, *opt.Evaluation, d)
			}
		}
		for _, r := range got {
			if opt.Evaluation == nil && !math.IsNaN(r.Value) {
				t.Fatalf("%v: value without evaluation", r)
			}
		}
	})
}
