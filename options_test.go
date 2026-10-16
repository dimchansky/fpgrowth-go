package fpgrowth

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/report"
)

func intPtr(n int) *int { return &n }

// TestOptionsValidation checks that invalid item set options are
// rejected with ErrInvalidOptions naming the field, and valid ones pass.
func TestOptionsValidation(t *testing.T) {
	t.Parallel()
	ok := Options[string]{MinSupport: Count(1)}
	tests := []struct {
		name  string
		edit  func(o *Options[string])
		field string // "" means valid
	}{
		{"minimal", func(*Options[string]) {}, ""},
		{"missing min support", func(o *Options[string]) { o.MinSupport = Support{} }, "MinSupport"},
		{"negative count", func(o *Options[string]) { o.MinSupport = Count(-1) }, "MinSupport"},
		{"count zero", func(o *Options[string]) { o.MinSupport = Count(0) }, ""},
		{"fraction zero", func(o *Options[string]) { o.MinSupport = Fraction(0) }, "MinSupport"},
		{"fraction above one", func(o *Options[string]) { o.MinSupport = Fraction(1.0000001) }, "MinSupport"},
		{"fraction one", func(o *Options[string]) { o.MinSupport = Fraction(1) }, ""},
		{"fraction nan", func(o *Options[string]) { o.MinSupport = Fraction(math.NaN()) }, "MinSupport"},
		{"fraction negative", func(o *Options[string]) { o.MinSupport = Fraction(-0.1) }, "MinSupport"},
		{"max support nan", func(o *Options[string]) { o.MaxSupport = Fraction(math.NaN()) }, "MaxSupport"},
		{"max support count zero", func(o *Options[string]) { o.MaxSupport = Count(0) }, ""},
		{"border negative", func(o *Options[string]) { o.Border = []Support{Count(1), Count(-2)} }, "Border[1]"},
		{"border zero entry", func(o *Options[string]) { o.Border = []Support{{}, Fraction(0.5)} }, ""},
		{"target", func(o *Options[string]) { o.Target = Generators + 1 }, "Target"},
		{"target negative", func(o *Options[string]) { o.Target = -1 }, "Target"},
		{"algorithm", func(o *Options[string]) { o.Algorithm = TopDown + 1 }, "Algorithm"},
		{"negative min size", func(o *Options[string]) { o.MinSize = -1 }, "MinSize"},
		{"negative max size", func(o *Options[string]) { o.MaxSize = -1 }, "MaxSize"},
		{"max below min", func(o *Options[string]) { o.MinSize, o.MaxSize = 3, 2 }, "MaxSize"},
		{"max equals min", func(o *Options[string]) { o.MinSize, o.MaxSize = 3, 3 }, ""},
		{"huge min size", func(o *Options[string]) { o.MinSize = math.MaxInt }, "MinSize"},
		{"huge max size", func(o *Options[string]) { o.MaxSize = math.MaxInt }, ""},
		{"include empty", func(o *Options[string]) { o.IncludeEmpty = true }, ""},
		{"include empty min 1", func(o *Options[string]) { o.IncludeEmpty, o.MinSize = true, 1 }, ""},
		{"include empty min 2", func(o *Options[string]) { o.IncludeEmpty, o.MinSize = true, 2 }, "IncludeEmpty"},
		{"item machine 16", func(o *Options[string]) { o.Tuning.ItemMachine = 16 }, ""},
		{"item machine 17", func(o *Options[string]) { o.Tuning.ItemMachine = 17 }, "Tuning.ItemMachine"},
		{"item order", func(o *Options[string]) { o.Tuning.ItemOrder = ItemOrderSizeSumDescending + 1 }, "Tuning.ItemOrder"},
		{"evaluation without measure", func(o *Options[string]) { o.Evaluation = &Evaluation{} }, "Evaluation.Measure"},
		{"evaluation bad measure", func(o *Options[string]) { o.Evaluation = &Evaluation{Measure: LogSupportRatio + 1} }, "Evaluation.Measure"},
		{"evaluation nan", func(o *Options[string]) { o.Evaluation = &Evaluation{Measure: Lift, Threshold: math.NaN()} }, "Evaluation.Threshold"},
		{"evaluation inf", func(o *Options[string]) { o.Evaluation = &Evaluation{Measure: Lift, Threshold: math.Inf(1)} }, ""},
		{"evaluation log ratio", func(o *Options[string]) { o.Evaluation = &Evaluation{Measure: LogSupportRatio, Threshold: 0.5} }, ""},
		{"log ratio with aggregation", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: LogSupportRatio, Aggregate: AggregateMax}
		}, "Evaluation.Aggregate"},
		{"log ratio with invalidate", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: LogSupportRatio, InvalidateBelowExpected: true}
		}, "Evaluation.InvalidateBelowExpected"},
		{"log ratio with prune", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: LogSupportRatio, PruneSize: intPtr(2)}
		}, "Evaluation.PruneSize"},
		{"bad aggregation", func(o *Options[string]) { o.Evaluation = &Evaluation{Measure: Lift, Aggregate: 4} }, "Evaluation.Aggregate"},
		{"rule measure full", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: Chi2PValue, Aggregate: AggregateAverage, InvalidateBelowExpected: true, PruneSize: intPtr(-1)}
		}, ""},
		{"prune size min int", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: Lift, PruneSize: intPtr(math.MinInt32)}
		}, "Evaluation.PruneSize"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := ok
			tt.edit(&o)
			_, err := MineItemsets(context.Background(), FromItems([][]string{{"a"}}), o, nil)
			if tt.field == "" {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidOptions) || !strings.Contains(err.Error(), ": "+tt.field+": ") {
				t.Fatalf("got %v, expected ErrInvalidOptions for %s", err, tt.field)
			}
		})
	}
}

// TestRuleOptionsValidation is TestOptionsValidation for rules.
func TestRuleOptionsValidation(t *testing.T) {
	t.Parallel()
	ok := RuleOptions[string]{MinSupport: Fraction(0.5)}
	tests := []struct {
		name  string
		edit  func(o *RuleOptions[string])
		field string
	}{
		{"minimal", func(*RuleOptions[string]) {}, ""},
		{"missing min support", func(o *RuleOptions[string]) { o.MinSupport = Support{} }, "MinSupport"},
		{"max support", func(o *RuleOptions[string]) { o.MaxSupport = Count(-5) }, "MaxSupport"},
		{"confidence one", func(o *RuleOptions[string]) { o.MinConfidence = 1 }, ""},
		{"confidence above one", func(o *RuleOptions[string]) { o.MinConfidence = 1.5 }, "MinConfidence"},
		{"confidence negative", func(o *RuleOptions[string]) { o.MinConfidence = -0.1 }, "MinConfidence"},
		{"confidence nan", func(o *RuleOptions[string]) { o.MinConfidence = math.NaN() }, "MinConfidence"},
		{"sizes", func(o *RuleOptions[string]) { o.MinSize, o.MaxSize = 3, 1 }, "MaxSize"},
		{"algorithm", func(o *RuleOptions[string]) { o.Algorithm = -1 }, "Algorithm"},
		{"tuning", func(o *RuleOptions[string]) { o.Tuning.ItemOrder = -1 }, "Tuning.ItemOrder"},
		{"default appearance", func(o *RuleOptions[string]) { o.DefaultAppearance = AppearNone + 1 }, "DefaultAppearance"},
		{"appearance", func(o *RuleOptions[string]) { o.Appearance = map[string]Appearance{"a": -1} }, "Appearance"},
		{"log ratio", func(o *RuleOptions[string]) { o.Evaluation = &Evaluation{Measure: LogSupportRatio} }, "Evaluation.Measure"},
		{"imbalance", func(o *RuleOptions[string]) { o.Evaluation = &Evaluation{Measure: Imbalance} }, ""},
		{"aggregation", func(o *RuleOptions[string]) {
			o.Evaluation = &Evaluation{Measure: Lift, Aggregate: AggregateMin}
		}, "Evaluation.Aggregate"},
		{"prune", func(o *RuleOptions[string]) { o.Evaluation = &Evaluation{Measure: Lift, PruneSize: intPtr(1)} }, "Evaluation.PruneSize"},
		{"invalidate", func(o *RuleOptions[string]) {
			o.Evaluation = &Evaluation{Measure: Lift, InvalidateBelowExpected: true}
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := ok
			tt.edit(&o)
			_, err := MineRules(context.Background(), FromItems([][]string{{"a", "b"}}), o, nil)
			if tt.field == "" {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidOptions) || !strings.Contains(err.Error(), ": "+tt.field+": ") {
				t.Fatalf("got %v, expected ErrInvalidOptions for %s", err, tt.field)
			}
		})
	}
}

// TestNilSource checks the nil source error.
func TestNilSource(t *testing.T) {
	t.Parallel()
	if _, err := MineItemsets[string](context.Background(), nil, Options[string]{MinSupport: Count(1)}, nil); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("item sets: %v", err)
	}
	if _, err := MineRules[string](context.Background(), nil, RuleOptions[string]{MinSupport: Count(1)}, nil); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("rules: %v", err)
	}
}

// TestPlanMapping checks the translation of public options to the
// internal configuration field by field.
func TestPlanMapping(t *testing.T) {
	t.Parallel()
	o := Options[string]{
		Target: Maximal, MinSupport: Fraction(0.25), MinSize: 2, MaxSize: 5, Algorithm: SingleTree,
		Tuning: Tuning{NoPerfectExtensions: true, NoReorder: true, NoHeadUnionTail: true, ItemMachine: 8,
			ItemOrder: ItemOrderFrequencyDescending},
		Evaluation: &Evaluation{Measure: Chi2PValue, Aggregate: AggregateMax, Threshold: 0.05,
			InvalidateBelowExpected: true, PruneSize: intPtr(3)},
	}
	p, err := o.plan()
	if err != nil {
		t.Fatal(err)
	}
	p.setThresholds(1000)
	c := p.cfg
	want := fpg.DefaultConfig()
	want.Target = report.TargetMaximal
	want.SMin, want.SMax = -250, 100
	want.ZMin, want.ZMax = 2, 5
	want.Algo = fpg.AlgoSingle
	want.Mode = 0
	want.Pack, want.Sort = 8, -1
	want.Eval, want.Agg, want.InvalidateBelowExpected, want.Prune = int(Chi2PValue), fpg.AggMax, true, 3
	want.Thresh = c.Thresh
	if c != want {
		t.Errorf("config\n %+v, expected\n %+v", c, want)
	}
	if c.Thresh/100 != 0.05 {
		t.Errorf("threshold %v/100 != 0.05", c.Thresh)
	}
	rp, err := (&RuleOptions[string]{MinSupport: Count(7), MaxSupport: Fraction(0.5), MinConfidence: 0.8,
		OriginalSupport: true}).plan()
	if err != nil {
		t.Fatal(err)
	}
	rp.setThresholds(10)
	if rp.cfg.Target != report.TargetRules || rp.cfg.SMin != -7 || rp.cfg.SMax != -5 || rp.cfg.Conf/100 != 0.8 ||
		rp.cfg.Mode&fpg.ModeOrigSupp == 0 || rp.cfg.ZMin != 1 || rp.cfg.ZMax != fpg.MaxSize {
		t.Errorf("rule config %+v", rp.cfg)
	}
}

// TestPercentOf checks that fractions reach the internal code unchanged
// whenever a percentage can represent them, and otherwise on the
// permissive side.
func TestPercentOf(t *testing.T) {
	t.Parallel()
	// upstream notation k% must equal the fraction k/100 (CLI equivalence)
	for k := -1000; k <= 1000; k++ {
		f := float64(k) / 100
		for _, dir := range []int{1, -1} {
			if p := percentOf(f, dir); p/100 != float64(k)/100.0 {
				t.Fatalf("percentOf(%v): %v/100 != %v", f, p, f)
			}
		}
	}
	r := rand.New(rand.NewPCG(1, 2))
	exact := 0
	const n = 200000
	for range n {
		f := r.Float64() * math.Pow(10, float64(r.IntN(9)-4))
		if r.IntN(2) == 0 {
			f = -f
		}
		up, down := percentOf(f, -1), percentOf(f, 1)
		switch {
		case up/100 == f && down/100 == f:
			exact++
		case !(down/100 < f && up/100 > f):
			t.Fatalf("f=%v: down %v up %v", f, down/100, up/100)
		case math.Nextafter(down, math.Inf(1))/100 <= f || math.Nextafter(up, math.Inf(-1))/100 >= f:
			t.Fatalf("f=%v: not the nearest neighbors", f)
		}
	}
	t.Logf("%.1f%% of random fractions are exact", 100*float64(exact)/n)
	for _, f := range []float64{0, math.Inf(1), math.Inf(-1), math.MaxFloat64} {
		_ = percentOf(f, 1) // must terminate
	}
}

// TestSupportArgs checks the conservative int64 -> float64 conversion of
// absolute supports beyond 2^53.
func TestSupportArgs(t *testing.T) {
	t.Parallel()
	for _, n := range []int64{0, 1, 1 << 53, 1<<53 + 1, 1<<62 + 1, math.MaxInt64 - 1, math.MaxInt64} {
		lo := -minSuppArg(n)
		if lo < float64(n) || (lo < 0x1p63 && int64(lo) < n) {
			t.Errorf("minSuppArg(%d) = %v below n", n, lo)
		}
		hi := -maxSuppArg(n)
		if hi >= 0x1p63 || int64(hi) > n {
			t.Errorf("maxSuppArg(%d) = %v above n", n, hi)
		}
	}
	if minSuppArg(12345) != -12345 || maxSuppArg(12345) != -12345 {
		t.Error("small values must be exact")
	}
	if suppFromFloat(math.NaN()) != 0 || suppFromFloat(1e300) != math.MaxInt64 || suppFromFloat(-3) != 0 {
		t.Error("suppFromFloat saturation")
	}
}

// TestFractionFormula checks Fraction thresholds against upstream's
// formula with the fraction itself (not a percentage round trip), at
// totals where f·W is an integer.
func TestFractionFormula(t *testing.T) {
	t.Parallel()
	for d := int64(1); d <= 200; d++ {
		for k := int64(1); k <= d; k++ {
			f := float64(k) / float64(d)
			for _, m := range []int64{1, 3, 1000, 1 << 40} {
				w := d * m
				if got := Fraction(f).minAbs(w); got != k*m {
					t.Fatalf("Fraction(%d/%d).minAbs(%d) = %d, expected %d", k, d, w, got, k*m)
				}
				if got := Fraction(f).maxAbs(w); got != k*m {
					t.Fatalf("Fraction(%d/%d).maxAbs(%d) = %d, expected %d", k, d, w, got, k*m)
				}
			}
		}
	}
}

// TestStringers covers the String methods.
func TestStringers(t *testing.T) {
	t.Parallel()
	checks := map[string]string{
		Count(3).String():                   "Count(3)",
		Fraction(0.25).String():             "Fraction(0.25)",
		Support{}.String():                  "unset",
		Closed.String():                     "Closed",
		Target(9).String():                  "Target(9)",
		TopDown.String():                    "TopDown",
		Algorithm(9).String():               "Algorithm(9)",
		Lift.String():                       "Lift",
		LogSupportRatio.String():            "LogSupportRatio",
		Measure(0).String():                 "Measure(0)",
		AggregateAverage.String():           "AggregateAverage",
		Aggregation(7).String():             "Aggregation(7)",
		AppearHead.String():                 "AppearHead",
		Appearance(7).String():              "Appearance(7)",
		Frequent.String() + Simple.String(): "FrequentSimple",
	}
	for got, want := range checks {
		if got != want {
			t.Errorf("got %q, expected %q", got, want)
		}
	}
	for m := RuleSupport; m < LogSupportRatio; m++ {
		if m.String() == "" || strings.HasPrefix(m.String(), "Measure(") {
			t.Errorf("measure %d has no name", int(m))
		}
	}
	if !Lift.HigherIsBetter() || Chi2PValue.HigherIsBetter() || !LogSupportRatio.HigherIsBetter() || Measure(0).HigherIsBetter() {
		t.Error("HigherIsBetter")
	}
}
