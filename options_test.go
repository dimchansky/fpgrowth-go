package fpgrowth

import (
	"context"
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

func intPtr(n int) *int { return &n }

// TestOptionsValidation checks that invalid item set options are
// rejected with ErrInvalidOptions naming the field, and valid ones pass.
func TestOptionsValidation(t *testing.T) {
	t.Parallel()
	ok := Options[string]{MinSupport: Count(1)}
	// math.MaxInt exceeds the size limit fpg.MaxSize (math.MaxInt32) only
	// on 64-bit platforms; on 32-bit ones it is the limit and valid.
	hugeMinSize := "MinSize"
	if math.MaxInt <= fpg.MaxSize {
		hugeMinSize = ""
	}
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
		{"max support count zero", func(o *Options[string]) { o.MaxSupport = Count(0) }, "MaxSupport"},
		{"max count below min count", func(o *Options[string]) { o.MinSupport, o.MaxSupport = Count(5), Count(2) }, "MaxSupport"},
		{"max count equal to min count", func(o *Options[string]) { o.MinSupport, o.MaxSupport = Count(5), Count(5) }, ""},
		{"max count 1 with min count 0", func(o *Options[string]) { o.MinSupport, o.MaxSupport = Count(0), Count(1) }, ""},
		{"max count 0 with min count 0", func(o *Options[string]) { o.MinSupport, o.MaxSupport = Count(0), Count(0) }, "MaxSupport"},
		{"max fraction below min fraction", func(o *Options[string]) { o.MinSupport, o.MaxSupport = Fraction(0.5), Fraction(0.4) }, "MaxSupport"},
		{"max fraction equal to min fraction", func(o *Options[string]) { o.MinSupport, o.MaxSupport = Fraction(0.5), Fraction(0.5) }, ""},
		{"max fraction within the tolerance below min", func(o *Options[string]) {
			o.MinSupport, o.MaxSupport = Fraction(math.Nextafter(0.3, 1)), Fraction(0.3)
		}, ""},
		{"max fraction just beyond the tolerance below min", func(o *Options[string]) {
			o.MinSupport, o.MaxSupport = Fraction(0.3), Fraction(0.3*(1-1e-14))
		}, "MaxSupport"},
		{"mixed kinds unchecked", func(o *Options[string]) { o.MinSupport, o.MaxSupport = Fraction(0.5), Count(1) }, ""},
		{"border negative", func(o *Options[string]) { o.Border = []Support{Count(1), Count(-2)} }, "Border[1]"},
		{"border zero entry", func(o *Options[string]) { o.Border = []Support{{}, Fraction(0.5)} }, ""},
		{"target", func(o *Options[string]) { o.Target = TargetGenerators + 1 }, "Target"},
		{"target negative", func(o *Options[string]) { o.Target = -1 }, "Target"},
		{"algorithm", func(o *Options[string]) { o.Algorithm = AlgorithmTopDown + 1 }, "Algorithm"},
		{"negative min size", func(o *Options[string]) { o.MinSize = -1 }, "MinSize"},
		{"negative max size", func(o *Options[string]) { o.MaxSize = -1 }, "MaxSize"},
		{"max below min", func(o *Options[string]) { o.MinSize, o.MaxSize = 3, 2 }, "MaxSize"},
		{"max equals min", func(o *Options[string]) { o.MinSize, o.MaxSize = 3, 3 }, ""},
		{"huge min size", func(o *Options[string]) { o.MinSize = math.MaxInt }, hugeMinSize},
		{"huge max size", func(o *Options[string]) { o.MaxSize = math.MaxInt }, ""},
		{"include empty", func(o *Options[string]) { o.IncludeEmpty = true }, ""},
		{"include empty min 1", func(o *Options[string]) { o.IncludeEmpty, o.MinSize = true, 1 }, ""},
		{"include empty min 2", func(o *Options[string]) { o.IncludeEmpty, o.MinSize = true, 2 }, "IncludeEmpty"},
		{"item machine 16", func(o *Options[string]) { o.Tuning.ItemMachine = 16 }, ""},
		{"item machine 17", func(o *Options[string]) { o.Tuning.ItemMachine = 17 }, "Tuning.ItemMachine"},
		{"item order", func(o *Options[string]) { o.Tuning.ItemOrder = ItemOrderSizeSumDescending + 1 }, "Tuning.ItemOrder"},
		{"evaluation without measure", func(o *Options[string]) { o.Evaluation = &Evaluation{} }, "Evaluation.Measure"},
		{"evaluation bad measure", func(o *Options[string]) { o.Evaluation = &Evaluation{Measure: LogSupportRatio + 1} }, "Evaluation.Measure"},
		{"evaluation nan", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: Lift, Filter: true, Threshold: math.NaN()}
		}, "Evaluation.Threshold"},
		{"evaluation inf", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: Lift, Filter: true, Threshold: math.Inf(1)}
		}, ""},
		{"evaluation log ratio", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: LogSupportRatio, Filter: true, Threshold: 0.5}
		}, ""},
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
			o.Evaluation = &Evaluation{Measure: Chi2PValue, Aggregate: AggregateAverage, InvalidateBelowExpected: true,
				Filter: true, Threshold: 0.01, PruneSize: intPtr(-1)}
		}, ""},
		{"values only", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: Chi2PValue, Aggregate: AggregateAverage, InvalidateBelowExpected: true}
		}, ""},
		{"threshold without filter", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: Lift, Threshold: 1.5}
		}, "Evaluation.Threshold"},
		{"prune without filter", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: Lift, PruneSize: intPtr(0)}
		}, "Evaluation.PruneSize"},
		{"filter with zero threshold", func(o *Options[string]) {
			o.Evaluation = &Evaluation{Measure: Lift, Filter: true}
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
		{"max fraction below min", func(o *RuleOptions[string]) { o.MaxSupport = Fraction(0.25) }, "MaxSupport"},
		{"max fraction equal to min", func(o *RuleOptions[string]) { o.MaxSupport = Fraction(0.5) }, ""},
		{"max count below min count", func(o *RuleOptions[string]) { o.MinSupport, o.MaxSupport = Count(3), Count(2) }, "MaxSupport"},
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
		{"rule threshold without filter", func(o *RuleOptions[string]) {
			o.Evaluation = &Evaluation{Measure: Lift, Threshold: -1}
		}, "Evaluation.Threshold"},
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

// TestNilSource checks the nil source error, also for a nil SourceFunc,
// which passes a plain nil check and would panic when called.
func TestNilSource(t *testing.T) {
	t.Parallel()
	var nilFunc SourceFunc[string]
	for name, src := range map[string]Source[string]{"nil": nil, "nil SourceFunc": nilFunc} {
		if _, err := MineItemsets(context.Background(), src, Options[string]{MinSupport: Count(1)}, nil); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("%s: item sets: %v", name, err)
		}
		if _, err := MineRules(context.Background(), src, RuleOptions[string]{MinSupport: Count(1)}, nil); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("%s: rules: %v", name, err)
		}
	}
}

// TestPlanMapping checks the translation of public options to the
// internal configuration field by field.
func TestPlanMapping(t *testing.T) {
	t.Parallel()
	o := Options[string]{
		Target: TargetMaximal, MinSupport: Fraction(0.25), MinSize: 2, MaxSize: 5, Algorithm: AlgorithmSingleTree,
		Tuning: Tuning{NoPerfectExtensions: true, NoReorder: true, NoHeadUnionTail: true, ItemMachine: 8,
			ItemOrder: ItemOrderFrequencyDescending},
		Evaluation: &Evaluation{Measure: Chi2PValue, Aggregate: AggregateMax, Filter: true, Threshold: 0.05,
			InvalidateBelowExpected: true, PruneSize: intPtr(3)},
	}
	p, err := o.plan()
	if err != nil {
		t.Fatal(err)
	}
	st := Stats{TotalWeight: 1000}
	p.setThresholds(&st)
	if st.MinSupport != 250 || st.MaxSupport != math.MaxInt64 {
		t.Errorf("resolved thresholds %+v", st)
	}
	c := p.cfg
	want := fpg.DefaultConfig()
	want.Target = report.TargetMaximal
	want.HasCounts, want.MinCount, want.MaxCount = true, 250, math.MaxInt64
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
	rp.setThresholds(&Stats{TotalWeight: 10})
	if rp.cfg.Target != report.TargetRules || !rp.cfg.HasCounts || rp.cfg.MinCount != 7 || rp.cfg.MaxCount != 5 ||
		rp.cfg.ConfFrac != 0.8 || rp.cfg.EvalNoFilter ||
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

// TestSupportSaturation checks the conversion of thresholds at the ends
// of the int64 range.
func TestSupportSaturation(t *testing.T) {
	t.Parallel()
	for _, n := range []int64{0, 1, 1 << 53, 1<<53 + 1, 1<<62 + 1, math.MaxInt64 - 1, math.MaxInt64} {
		if got := Count(n).minAbs(7); got != n {
			t.Errorf("Count(%d).minAbs = %d", n, got)
		}
		if got := Count(n).maxAbs(7); got != n {
			t.Errorf("Count(%d).maxAbs = %d", n, got)
		}
	}
	if got := Fraction(1).maxAbs(math.MaxInt64); got != math.MaxInt64 {
		t.Errorf("Fraction(1).maxAbs(MaxInt64) = %d", got)
	}
	if got := (Support{}).maxAbs(5); got != math.MaxInt64 {
		t.Errorf("unset maximum %d", got)
	}
	for _, w := range []int64{1<<52 + 1, 1<<60 + 1, math.MaxInt64} {
		if got := Fraction(1).minAbs(w); got != w {
			t.Errorf("Fraction(1).minAbs(%d) = %d", w, got)
		}
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
		Count(3).String():                                  "Count(3)",
		Fraction(0.25).String():                            "Fraction(0.25)",
		Support{}.String():                                 "unset",
		TargetClosed.String():                              "TargetClosed",
		Target(9).String():                                 "Target(9)",
		AlgorithmTopDown.String():                          "AlgorithmTopDown",
		Algorithm(9).String():                              "Algorithm(9)",
		Lift.String():                                      "Lift",
		LogSupportRatio.String():                           "LogSupportRatio",
		Measure(0).String():                                "Measure(0)",
		AggregateAverage.String():                          "AggregateAverage",
		Aggregation(7).String():                            "Aggregation(7)",
		AppearHead.String():                                "AppearHead",
		Appearance(7).String():                             "Appearance(7)",
		ItemOrderDefault.String():                          "ItemOrderDefault",
		ItemOrderNone.String():                             "ItemOrderNone",
		ItemOrderSizeSumDescending.String():                "ItemOrderSizeSumDescending",
		ItemOrder(9).String():                              "ItemOrder(9)",
		TargetFrequent.String() + AlgorithmSimple.String(): "TargetFrequentAlgorithmSimple",
	}
	for got, want := range checks {
		if got != want {
			t.Errorf("got %q, expected %q", got, want)
		}
	}
	for o := ItemOrderDefault; o <= ItemOrderSizeSumDescending; o++ {
		if strings.HasPrefix(o.String(), "ItemOrder(") {
			t.Errorf("item order %d has no name", int(o))
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

// TestLibraryThresholdsUnchanged compares the threshold contract (issue
// #17, N2) with the library's former double arithmetic at ordinary
// weights (W < 2^31): Fraction converted with ceil(f·W·(1-ε)) and
// floor(f·W·(1+ε)), MinConfidence mapped through percentOf and tested as
// !(supp < body·(p/100·(1-ε))). Short decimals and fractions k/d with
// d <= 1000 cannot change there (the double rounding would have to cross
// an integer within about 4.5·2^-53·x of x, which needs x >= about
// 2^53/(4.5·d)). Fractions with many digits (random values, one ulp off a
// decimal) can change in rare cases, and then the new result is the exact
// one for the shortest decimal of f (what fmt prints): such a change
// corrects the double rounding.
func TestLibraryThresholdsUnchanged(t *testing.T) {
	t.Parallel()
	const eps = 0x1p-52
	oldMin := func(f float64, w int64) int64 { return int64(math.Ceil(f * float64(w) * (1 - eps))) }
	oldMax := func(f float64, w int64) int64 { return int64(math.Floor(f * float64(w) * (1 + eps))) }
	oldConf := func(supp, body int64, f float64) bool {
		c := percentOf(f, +1) / 100
		return !(float64(supp) < float64(body)*(c*(1-eps)))
	}
	newConf := func(supp, body int64, f float64) bool { return supp >= Fraction(f).minAbs(body) }
	// decimal returns ⌈dec·w⌉ and ⌊dec·w⌋ for the shortest decimal dec of f.
	decimal := func(f float64, w int64) (lo, hi int64) {
		d, _ := new(big.Rat).SetString(strconv.FormatFloat(f, 'g', -1, 64))
		x := new(big.Rat).Mul(d, new(big.Rat).SetInt64(w))
		q, m := new(big.Int).QuoRem(x.Num(), x.Denom(), new(big.Int))
		lo, hi = q.Int64(), q.Int64()
		if m.Sign() > 0 {
			lo++
		}
		return lo, hi
	}
	r := rand.New(rand.NewPCG(17, 5))
	changes, randomChanges := 0, 0
	for range 300000 {
		var f float64
		guaranteed := true
		switch r.IntN(4) {
		case 0:
			f, guaranteed = r.Float64(), false
		case 1:
			f = float64(1+r.IntN(100000)) / 100000
		case 2:
			d := 1 + r.IntN(1000)
			f = float64(1+r.IntN(d)) / float64(d)
		default:
			f, guaranteed = math.Nextafter(float64(1+r.IntN(100))/100, float64(r.IntN(2))), false
		}
		if f == 0 {
			continue
		}
		w := 1 + r.Int64N(1<<31-1)
		lo, hi := Fraction(f).minAbs(w), Fraction(f).maxAbs(w)
		if a, b := oldMin(f, w), oldMax(f, w); a != lo || b != hi {
			changes++
			if f != math.Nextafter(math.Round(f*100)/100, f) {
				randomChanges++ // not one ulp off a 2-dp decimal
			}
			if dlo, dhi := decimal(f, w); guaranteed || lo != dlo || hi != dhi {
				t.Fatalf("Fraction(%v) of %d: was %d..%d, now %d..%d (shortest decimal: %d..%d)", f, w, a, b, lo, hi, dlo, dhi)
			}
		}
		if !guaranteed {
			continue
		}
		body := w
		s := int64(math.Round(f * float64(body)))
		for _, supp := range []int64{s - 1, s, s + 1} {
			if supp >= 0 && supp <= body && oldConf(supp, body, f) != newConf(supp, body, f) {
				t.Fatalf("MinConfidence %v supp %d body %d: was %v", f, supp, body, oldConf(supp, body, f))
			}
		}
	}
	t.Logf("%d changes (%d not one ulp off a 2-dp decimal), all exact for the shortest decimal", changes, randomChanges)
	for k := 0; k <= 1000; k++ {
		f := float64(k) / 1000
		for body := int64(1); body <= 200; body++ {
			for supp := int64(0); supp <= body; supp++ {
				if oldConf(supp, body, f) != newConf(supp, body, f) {
					t.Fatalf("MinConfidence %v supp %d body %d: was %v", f, supp, body, oldConf(supp, body, f))
				}
			}
		}
	}
}

// TestFractionMatchesCLI checks Fraction(k/100) against the command line's
// -sk and -Sk (the oracle's independent conversion of the percentage, a
// float64 division by 100) over the whole int64 range.
func TestFractionMatchesCLI(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(17, 6))
	for range 20000 {
		k := 1 + r.IntN(100)
		w := r.Int64N(math.MaxInt64)
		if r.IntN(2) == 0 {
			w = math.MaxInt64 - r.Int64N(1<<20)
		}
		f := float64(k) / 100
		if a, b := Fraction(f).minAbs(w), int64(oracle.AbsMinSupp(float64(k), tract.Supp(w))); a != b {
			t.Fatalf("Fraction(%v).minAbs(%d) = %d, -s%d %d", f, w, a, k, b)
		}
		if a, b := Fraction(f).maxAbs(w), int64(oracle.AbsMaxSupp(float64(k), tract.Supp(w))); a != b {
			t.Fatalf("Fraction(%v).maxAbs(%d) = %d, -S%d %d", f, w, a, k, b)
		}
	}
}

// TestCheckRangeSound checks that checkRange never rejects a pair of
// fractions whose range is non-empty for some total weight: for every
// rejected pair, minAbs > maxAbs for all W up to 2000 and random large W.
func TestCheckRangeSound(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(17, 7))
	rejected := 0
	for range 20000 {
		f := r.Float64()
		if f == 0 {
			continue
		}
		g := f
		for range r.IntN(12) {
			g = math.Nextafter(g, 0)
		}
		if g == 0 {
			continue
		}
		if checkRange(Fraction(f), Fraction(g)) == nil {
			continue
		}
		rejected++
		for w := int64(1); w <= 2000; w++ {
			if max(Fraction(f).minAbs(w), 1) <= Fraction(g).maxAbs(w) {
				t.Fatalf("rejected %v..%v, but W=%d admits [%d, %d]", f, g, w, Fraction(f).minAbs(w), Fraction(g).maxAbs(w))
			}
		}
		for range 200 {
			w := 1 + r.Int64N(math.MaxInt64-1)
			if max(Fraction(f).minAbs(w), 1) <= Fraction(g).maxAbs(w) {
				t.Fatalf("rejected %v..%v, but W=%d admits [%d, %d]", f, g, w, Fraction(f).minAbs(w), Fraction(g).maxAbs(w))
			}
		}
	}
	if rejected == 0 {
		t.Error("no pair rejected")
	}
	// within the tolerance: accepted and non-empty for W = 10
	if err := checkRange(Fraction(math.Nextafter(0.3, 1)), Fraction(0.3)); err != nil {
		t.Error(err)
	}
	if Fraction(math.Nextafter(0.3, 1)).minAbs(10) != 3 || Fraction(0.3).maxAbs(10) != 3 {
		t.Error("0.30000000000000004..0.3 of 10 is not 3..3")
	}
}
