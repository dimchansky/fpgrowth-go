package ruleval

import (
	"math"
	"math/big"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// TestUpstreamBugRegressions covers the reproducers of
// docs/upstream/rules-eval.md for the deviations outside the Fisher tests.
func TestUpstreamBugRegressions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		m          Measure
		s, b, h, n tract.Supp
		want       float64
	}{
		// B4: rule b <- a c d of t1.tab (confidence 1); upstream gives 0.
		{"conviction with confidence 1", Conviction, 2, 2, 7, 10, math.Inf(1)},
		{"conviction diff with confidence 1", ConvictionDiff, 2, 2, 7, 10, math.Inf(1)},
		{"conviction with empty body support", Conviction, 0, 0, 7, 10, 0},
		{"conviction with head in all", Conviction, 7, 7, 10, 10, math.Inf(1)},
		// B5: rule z <- d of t2.tab; upstream gives NaN.
		{"certainty with head in all", Certainty, 7, 7, 10, 10, 0},
		{"certainty empty body, head in all", Certainty, 10, 10, 10, 10, 0},
		// B10: the independent table of the audit; upstream 0.0417 and 0.519.
		{"yates independent", Yates, 2, 4, 5, 10, 0},
		{"yates p-value independent", YatesPValue, 2, 4, 5, 10, 1},
		// B7: rule B <- A of neg.tab (independent); upstream -3.2e-16.
		{"info independent", Info, 1, 2, 3, 6, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Eval(tt.m, tt.s, tt.b, tt.h, tt.n)
			if got != tt.want {
				t.Errorf("Eval(%v, %d, %d, %d, %d) = %v, want %v", tt.m, tt.s, tt.b, tt.h, tt.n, got, tt.want)
			}
		})
	}
	// B7 and ALG-6: the information of the independent table is exactly 0
	// and the G test p-value exactly 1 (upstream: -3.2e-16 and 0.99999996).
	if got := Eval(InfoPValue, 1, 2, 3, 6); got != 1 {
		t.Errorf("InfoPValue(1, 2, 3, 6) = %v, want 1", got)
	}
	for _, tb := range [][4]tract.Supp{{1, 2, 3, 6}, {2, 4, 5, 10}, {3, 6, 5, 10}, {1319510, 3112642, 2356964, 5559933}} {
		if v := Eval(Info, tb[0], tb[1], tb[2], tb[3]); v < 0 {
			t.Errorf("Info%v = %v < 0", tb, v)
		}
	}
}

// TestChi2Exact compares Chi2 and Yates with exact rational arithmetic:
// chi2 = (hb - sn)^2 / (h(n-h)b(n-b)) and
// yates = max(0, |hb - sn| - n/2)^2 / (h(n-h)b(n-b)). Each is computed with
// at most eight roundings, so the relative error is below 8 ε.
func TestChi2Exact(t *testing.T) {
	t.Parallel()
	check := func(s, b, h, n tract.Supp) {
		if marginsDegenerate(b, h, n) {
			for _, m := range []Measure{Chi2, Yates} {
				if v := Eval(m, s, b, h, n); v != 0 {
					t.Errorf("Eval(%v, %d, %d, %d, %d) = %v, want 0", m, s, b, h, n, v)
				}
			}
			return
		}
		bi := func(v tract.Supp) *big.Int { return big.NewInt(int64(v)) }
		d := new(big.Int).Sub(new(big.Int).Mul(bi(h), bi(b)), new(big.Int).Mul(bi(s), bi(n)))
		den := new(big.Int).Mul(new(big.Int).Mul(bi(h), bi(n-h)), new(big.Int).Mul(bi(b), bi(n-b)))
		chi2 := new(big.Rat).SetFrac(new(big.Int).Mul(d, d), den)
		yd := new(big.Rat).Sub(new(big.Rat).SetInt(new(big.Int).Abs(d)), big.NewRat(int64(n), 2))
		if yd.Sign() < 0 {
			yd.SetInt64(0)
		}
		yates := new(big.Rat).Quo(new(big.Rat).Mul(yd, yd), new(big.Rat).SetInt(den))
		// Above 2^53 the products hb and sn are rounded before they are
		// subtracted, so the difference has an absolute error of up to
		// ε·(hb+sn); squared, that is a relative error of 2·ε·(hb+sn)/|d|.
		mag := float64(h)*float64(b) + float64(s)*float64(n)
		for _, c := range []struct {
			m    Measure
			want *big.Rat
			diff float64 // |d| or |d| - n/2
		}{{Chi2, chi2, math.Abs(float64(d.Int64()))}, {Yates, yates, ratFloat(yd)}} {
			want := ratFloat(c.want)
			tol := 8 * epsilon * want
			if mag >= 1<<53 {
				tol += 4 * epsilon * mag / max(c.diff, 1) * want
				tol += 4 * epsilon * epsilon * mag * mag / ratFloat(new(big.Rat).SetInt(den))
			}
			if got := Eval(c.m, s, b, h, n); math.Abs(got-want) > tol {
				t.Errorf("Eval(%v, %d, %d, %d, %d) = %.17g, exact %.17g", c.m, s, b, h, n, got, want)
			}
		}
		// The p-values follow from the statistics: Q(1/2, x/2) = erfc(sqrt(x/2)).
		// (From the computed statistic, whose error is checked above; GammaQ
		// adds a relative error of a few ε·x.)
		x := float64(n) * Eval(Chi2, s, b, h, n)
		if got, want := Eval(Chi2PValue, s, b, h, n), math.Erfc(math.Sqrt(x/2)); math.Abs(got-want) > 8*epsilon*(2+x)*want+1e-300 {
			t.Errorf("Chi2PValue(%d, %d, %d, %d) = %.17g, erfc %.17g", s, b, h, n, got, want)
		}
	}
	forEachValidTable(20, check)
	for _, tb := range randomTables(2000, 31, 3) {
		check(tb[0], tb[1], tb[2], tb[3])
	}
}

// TestSymmetry checks the measures that are symmetric in body and head,
// and the Fisher tests' invariance under complementing both margins.
func TestSymmetry(t *testing.T) {
	t.Parallel()
	bitwise := []Measure{Lift, LiftDiff, LiftQuotient, Imbalance, Kulczynski, KulczynskiDiff,
		FisherProb, FisherChi2, FisherInfo, FisherSupport}
	close := []Measure{Chi2, Chi2PValue, Yates, YatesPValue, Info, InfoPValue}
	forEachValidTable(18, func(s, b, h, n tract.Supp) {
		for _, m := range bitwise {
			if x, y := Eval(m, s, b, h, n), Eval(m, s, h, b, n); x != y && !(math.IsNaN(x) && math.IsNaN(y)) {
				t.Errorf("%v(%d, %d, %d, %d) = %v, swapped %v", m, s, b, h, n, x, y)
			}
		}
		for _, m := range close {
			if x, y := Eval(m, s, b, h, n), Eval(m, s, h, b, n); math.Abs(x-y) > 1e-14*max(math.Abs(x), 1) {
				t.Errorf("%v(%d, %d, %d, %d) = %v, swapped %v", m, s, b, h, n, x, y)
			}
		}
		// Complementing body and head maps the table onto itself with
		// rows and columns exchanged; the two-sided tests do not change,
		// and the upper tail of x stays the upper tail.
		sc, bc, hc := n-b-h+s, n-b, n-h
		for _, m := range []Measure{FisherProb, FisherChi2, FisherInfo, FisherSupport, Chi2, Yates, Info} {
			if x, y := Eval(m, s, b, h, n), Eval(m, sc, bc, hc, n); math.Abs(x-y) > 1e-12 {
				t.Errorf("%v(%d, %d, %d, %d) = %v, complemented %v", m, s, b, h, n, x, y)
			}
		}
	})
}

// TestMonotonicity checks monotonicity in the rule support for fixed
// margins: confidence, lift and the support test's upper tail behave
// strictly; chi^2 and its p-value move away from independence.
func TestMonotonicity(t *testing.T) {
	t.Parallel()
	for n := tract.Supp(2); n <= 30; n++ {
		for b := tract.Supp(1); b < n; b++ {
			for h := tract.Supp(1); h < n; h++ {
				lo, hi := max(0, b+h-n), min(b, h)
				for s := lo; s < hi; s++ {
					if Eval(Confidence, s, b, h, n) >= Eval(Confidence, s+1, b, h, n) ||
						Eval(Lift, s, b, h, n) >= Eval(Lift, s+1, b, h, n) {
						t.Fatalf("confidence or lift not increasing at (%d, %d, %d, %d)", s, b, h, n)
					}
					if Eval(FisherSupport, s, b, h, n) < Eval(FisherSupport, s+1, b, h, n) {
						t.Fatalf("FisherSupport increases at (%d, %d, %d, %d)", s, b, h, n)
					}
					above := float64(s)*float64(n) >= float64(h)*float64(b)
					c0, c1 := Eval(Chi2, s, b, h, n), Eval(Chi2, s+1, b, h, n)
					p0, p1 := Eval(Chi2PValue, s, b, h, n), Eval(Chi2PValue, s+1, b, h, n)
					if above && (c1 < c0 || p1 > p0) {
						t.Fatalf("chi^2 not moving away from independence at (%d, %d, %d, %d)", s, b, h, n)
					}
					if Eval(Yates, s, b, h, n) > c0 {
						t.Fatalf("Yates exceeds chi^2 at (%d, %d, %d, %d)", s, b, h, n)
					}
				}
			}
		}
	}
}

func TestEvalDoesNotAllocate(t *testing.T) {
	const big = 1 << 62
	for _, tb := range [][4]tract.Supp{
		{35, 60, 70, 200},                // Fisher: upstream's algorithm (base <= 170 below)
		{35, 60, 70, 150},                //
		{3500, 6000, 7000, 20000},        // Fisher: walk (base > 170)
		{big / 8, big / 2, big / 4, big}, // 128-bit paths
		{2, 2, big - 2, big},             // certainty: exact form
		{9223372036854775800, 9223372036854775800, 9223372036854775800, 9223372036854775803},
	} {
		for m := range NumMeasures {
			if isFisher(m) && fisherCost(tb[1], tb[2], tb[3]) > 1e6 {
				continue
			}
			allocs := testing.AllocsPerRun(100, func() {
				Eval(m, tb[0], tb[1], tb[2], tb[3])
			})
			if allocs != 0 {
				t.Errorf("Eval(%v, %v) allocates %v times", m, tb, allocs)
			}
		}
	}
}

func BenchmarkEval(b *testing.B) {
	for _, bc := range []struct {
		name        string
		m           Measure
		s, bo, h, n tract.Supp
	}{
		{"", Lift, 350, 600, 700, 10000},
		{"", Chi2PValue, 350, 600, 700, 10000},
		{"", Info, 350, 600, 700, 10000},
		{"", FisherProb, 35, 60, 70, 1000},
		// added for issue #16
		{"", Chi2, 350, 600, 700, 10000},
		{"", Yates, 350, 600, 700, 10000},
		{"", YatesPValue, 350, 600, 700, 10000},
		{"", Certainty, 350, 600, 700, 10000},
		{"", InfoPValue, 350, 600, 700, 10000},
		{"Info/nearIndep", Info, 3500000, 6000000, 7000000, 100000000},
		{"InfoPValue/nearIndep", InfoPValue, 3500000, 6000000, 7000000, 100000000},
		{"Chi2PValue/2^42", Chi2PValue, 1 << 40, 1 << 41, 1 << 41, 1 << 42}, // 128-bit path
		{"Info/2^42", Info, 1 << 40, 1 << 41, 1 << 41, 1 << 42},
		{"FisherProb/150", FisherProb, 35, 60, 70, 150}, // upstream's algorithm
		{"", FisherChi2, 35, 60, 70, 1000},
		{"", FisherInfo, 35, 60, 70, 1000},
		{"", FisherSupport, 35, 60, 70, 1000},
		{"FisherProb/1e6", FisherProb, 35000, 60000, 70000, 1000000},
		// issue #40: strong association at 10^16 (underflow shortcut)
		{"FisherInfo/1e16", FisherInfo, 5e14, 5e15, 5e15 + 1, 1e16},
	} {
		name := bc.name
		if name == "" {
			name = bc.m.String()
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			var sink float64
			for b.Loop() {
				sink += Eval(bc.m, bc.s, bc.bo, bc.h, bc.n)
			}
			_ = sink
		})
	}
}
