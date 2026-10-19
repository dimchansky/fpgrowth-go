package ruleval

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Tests of the Fisher tests for base > maxFact (fisherlarge.go).

var fisherMeasures = [4]Measure{FisherProb, FisherChi2, FisherInfo, FisherSupport}

// perTermFisher is the test oracle of the walk: it sums exp(lnP(x)) over
// every table x of the normalized table that belongs to the region of m,
// with a compensated sum. Membership is decided exactly (cmpWeight without
// a distance limit, the deviation |x·n - b·h| in big integers, cmpXLogX,
// x >= supp) unless the computed values differ by more than 2^16 times
// the implementation's error bounds. It shares only lnP and infoNats with
// the implementation: no ratios, bisection, truncation or tie limit.
func perTermFisher(m Measure, supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 1
	}
	t := newHyperTable(supp, body, head, base)
	s, b, h, n := t.ft.supp, t.ft.body, t.ft.head, t.ft.base
	dev := func(x tract.Supp) *big.Int { // |x·n - b·h|
		d := new(big.Int).Mul(big.NewInt(int64(x)), big.NewInt(int64(n)))
		d.Sub(d, new(big.Int).Mul(big.NewInt(int64(b)), big.NewInt(int64(h))))
		return d.Abs(d)
	}
	ds := dev(s)
	ls, ss := t.lnP(s)
	var acc kahan
	for x := tract.Supp(0); x <= b; x++ {
		var in bool
		switch m {
		case FisherProb:
			lx, sx := t.lnP(x)
			wide := 0x1p16 * (lnPErr(sx) + lnPErr(ss))
			in = x == s || lx < ls-wide || !(lx > ls+wide) && t.ft.cmpWeight(x) >= 0
		case FisherChi2:
			in = dev(x).Cmp(ds) >= 0
		case FisherInfo:
			sx := infoNats(x, b, h, n)
			wide := 0x1p-29 * (sx + ss)
			in = x == s || sx > ss+wide || !(sx < ss-wide) && cmpXLogX(t.ft.cells(x), t.ft.cells(s)) >= 0
		case FisherSupport:
			in = x >= s
		}
		if in {
			lp, _ := t.lnP(x)
			acc.add(math.Exp(lp))
		}
	}
	return clampUnit(acc.value())
}

// largeFisherTables returns tables with base > maxFact and at most maxT
// tables: random ones (also with tiny tails, near independence and the
// observed table far beyond the mode) and fixed regression cases.
func largeFisherTables(r *rand.Rand, count int, maxT int64) []table {
	tables := []table{
		// review of #16 (M1): the summed range [0, s-1] of upstream's
		// complement contains the mode; walking it from s-1 underflows.
		{523678, 524215, 524621, 525540},
		// review of #16 (M3): tiny upper tails, which a complement 1 - Σ
		// gets only to about 1e-16 absolutely.
		{400, 1000, 100000, 1000000},
		{300, 1000, 100000000000, 1000000000000},
		{60, 200, 10000, 100000},
		// the audit's ALG-1 table and perfect associations near 2^63
		{2999999999999998, 3e15, 8999999999999998, 9e15},
		{9223372036854775800, 9223372036854775800, 9223372036854775800, 9223372036854775803},
		{2, 2, 9223372036854775798, 9223372036854775800},
		// small n just above maxFact, symmetric margins (exact ties)
		{40, 85, 86, 171}, {20, 40, 86, 172}, {30, 60, 120, 240}, {0, 3, 4, 171},
	}
	for range count {
		n := int64(1) << (8 + r.IntN(55))
		n += r.Int64N(n)
		n = min(n, math.MaxInt64-4)
		k := 1 + r.Int64N(min(maxT, n)-1) // number of tables - 1 = smallest margin
		var b, h int64
		switch r.IntN(3) {
		case 0: // small body
			b, h = k, k+r.Int64N(n-k+1)
		case 1: // body and head near n (complemented)
			b, h = n-k, n-k-r.Int64N(n-k+1)
		default: // head = n - k
			h = n - k
			b = h - r.Int64N(h+1)
		}
		if b > n || h > n || b < 0 || h < 0 {
			continue
		}
		lo, hi := max(0, b+h-n), min(b, h)
		var s int64
		switch r.IntN(4) {
		case 0: // near the expected support
			e := float64(b) * (float64(h) / float64(n))
			s = min(max(lo, int64(e)+r.Int64N(5)-2), hi)
		case 1: // an end of the range
			s = []int64{lo, hi}[r.IntN(2)]
		default:
			s = lo + r.Int64N(hi-lo+1)
		}
		tables = append(tables, table{s, b, h, n})
	}
	return tables
}

// TestFisherLargeMatchesPerTerm compares the four Fisher tests for
// base > maxFact with perTermFisher (all tables summed, decisions exact)
// on tables with up to 3000 tables: the walk, its truncation, the
// bisection and the tie handling may change the sum by at most 2^-40
// relative (plus 2^-1000 absolute).
func TestFisherLargeMatchesPerTerm(t *testing.T) {
	t.Parallel()
	count := 400
	if testing.Short() || raceEnabled {
		count = 60
	}
	r := rand.New(rand.NewPCG(16, 1))
	worst := 0.0
	for _, tb := range largeFisherTables(r, count, 3000) {
		if tb.fisherTables() > 3000 {
			continue
		}
		s, b, h, n := tract.Supp(tb.s), tract.Supp(tb.b), tract.Supp(tb.h), tract.Supp(tb.n)
		for _, m := range fisherMeasures {
			got, want := Eval(m, s, b, h, n), perTermFisher(m, s, b, h, n)
			d := math.Abs(got - want)
			if d > 0x1p-40*want+0x1p-1000 {
				t.Errorf("%v %v: walk %.17g, per-term sum %.17g", m, tb, got, want)
			}
			if want > 0x1p-1000 {
				worst = max(worst, d/want)
			}
		}
	}
	t.Logf("largest relative difference %.3g (%.3g ulps)", worst, worst/epsilon)
}

// TestFisherLargeMatchesReference compares the four Fisher tests for
// base > maxFact with the exact rational reference (internal/oracle) on
// tables with up to oracle.FisherMaxTables tables and totals up to 2^63,
// within the documented bound 2^-36 relative (+2^-1000).
func TestFisherLargeMatchesReference(t *testing.T) {
	t.Parallel()
	count, maxT := 300, int64(oracle.FisherMaxTables)
	if testing.Short() || raceEnabled {
		count, maxT = 40, 200
	}
	r := rand.New(rand.NewPCG(16, 2))
	worst := 0.0
	checked := 0
	for _, tb := range largeFisherTables(r, count, maxT) {
		s, b, h, n := tract.Supp(tb.s), tract.Supp(tb.b), tract.Supp(tb.h), tract.Supp(tb.n)
		for _, m := range fisherMeasures {
			rm, _ := oracle.RefByName(m.String())
			want, err := rm.Eval(s, b, h, n)
			if errors.Is(err, oracle.ErrOutOfScope) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			checked++
			got := Eval(m, s, b, h, n)
			if !want.Matches(got) {
				t.Errorf("%v %v: got %.17g, reference %v", m, tb, got, want)
			}
			if want.V > 0x1p-1000 {
				worst = max(worst, math.Abs(got-want.V)/want.V)
			}
		}
	}
	t.Logf("%d comparisons, largest relative error %.3g (%.3g ulps)", checked, worst, worst/epsilon)
}

// TestHyperLnP compares exp(lnP(x)) with the exact hypergeometric
// probability C(h, x)·C(n-h, b-x)/C(n, b) for small bodies and totals up
// to 2^63: within the bound lnPErr(S(x)), and 2^-42 relative for
// probabilities of at least 2^-60.
func TestHyperLnP(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(16, 3))
	worst := 0.0
	for range 300 {
		n := int64(1) << (8 + r.IntN(55))
		n += r.Int64N(n)
		n = min(n, math.MaxInt64-1)
		b := 1 + r.Int64N(24)
		h := b + r.Int64N(n-2*b)
		if r.IntN(2) == 0 {
			h = n - 1 - r.Int64N(3*b)
		}
		ht := newHyperTable(0, tract.Supp(b), tract.Supp(h), tract.Supp(n))
		if ht.ft.body != tract.Supp(b) || ht.ft.rest < 0 {
			continue // normalization changed the table; covered elsewhere
		}
		den := new(big.Int).Binomial(n, b)
		for x := int64(0); x <= b; x++ {
			num := new(big.Int).Binomial(h, x)
			num.Mul(num, new(big.Int).Binomial(n-h, b-x))
			exact, _ := new(big.Rat).SetFrac(num, den).Float64()
			if exact < 0x1p-1000 {
				continue
			}
			lp, sx := ht.lnP(tract.Supp(x))
			got := math.Exp(lp)
			rel := math.Abs(got-exact) / exact
			worst = max(worst, rel)
			// the documented bound of lnP (lnPErr), and 2^-42 for the
			// tables that matter in a sum (P >= 2^-60)
			if rel > lnPErr(sx) || exact >= 0x1p-60 && rel > 0x1p-42 {
				t.Errorf("P(%d; b=%d h=%d n=%d) = %.17g, exact %.17g (rel %.3g)", x, b, h, n, got, exact, rel)
			}
		}
	}
	t.Logf("largest relative error %.3g (%.3g ulps)", worst, worst/epsilon)
}

// TestStirlerr checks σ(m) = ln m! - (m·ln m - m + ½·ln(2πm)) against
// math.Lgamma (accurate to a few ulps of ln m!, so the comparison is
// absolute in units of ln m!) for m up to 1000, and the continuity of
// table and series.
func TestStirlerr(t *testing.T) {
	t.Parallel()
	for m := 1; m <= 1000; m++ {
		fm := float64(m)
		lg, _ := math.Lgamma(fm + 1)
		want := lg - (fm*math.Log(fm) - fm + 0.5*math.Log(2*math.Pi*fm))
		if d := math.Abs(stirlerr(tract.Supp(m)) - want); d > 8*epsilon*(lg+fm*math.Log(fm)+fm+1) {
			t.Errorf("stirlerr(%d) = %.17g, from ln m! %.17g", m, stirlerr(tract.Supp(m)), want)
		}
	}
	if d := math.Abs(stirTable[15] - stirSeries(15)); d > 1e-15 {
		t.Errorf("stirlerr(15): table %.17g, series %.17g", stirTable[15], stirSeries(15))
	}
}

// TestFisherLargeCost checks that the walk stops after about 9 standard
// deviations per side: at n = 10^12 with balanced margins (σ = 2.5·10^5)
// at independence, every test visits fewer than 30σ tables in total
// (counted with the abort function), instead of 5·10^11.
func TestFisherLargeCost(t *testing.T) {
	t.Parallel()
	const n, b, h, s = 1_000_000_000_000, 500_000_000_000, 500_000_000_000, 250_000_000_000
	sigma := math.Sqrt(float64(b) * (float64(h) / n) * (1 - float64(h)/n) * (1 - float64(b)/n))
	for _, m := range fisherMeasures {
		calls := 0
		abort := func() error { calls++; return nil }
		start := time.Now()
		v, err := m.AbortFunc()(s, b, h, n, abort)
		lo := 0.99 // two-sided tests at the expected support
		if m == FisherSupport {
			lo = 0.5 // one-sided: about 1/2
		}
		if err != nil || !(v > lo && v <= 1) {
			t.Errorf("%v: %v, %v", m, v, err)
		}
		steps := float64(calls * fisherAbortEvery)
		if steps > 30*sigma {
			t.Errorf("%v: about %.0f steps, more than 30σ = %.0f", m, steps, 30*sigma)
		}
		t.Logf("%v: %.3g, about %.0f steps (%.1fσ) in %v", m, v, steps, steps/sigma, time.Since(start))
	}
}

// TestFisherLargeNearTie checks that a near-tie of FisherProb far from the
// observed table is decided without big integers: with the computed
// log-probabilities forced equal, probAtMost must count the table as tied
// at once (fisherTieExact), even at n = 10^18 where the exact comparison
// would multiply about 10^8 63-bit numbers.
func TestFisherLargeNearTie(t *testing.T) {
	t.Parallel()
	const n, b, h = 1_000_000_000_000_000_000, 400_000_000_000_000_000, 300_000_000_000_000_000
	s := tract.Supp(120_000_000_000_000_000 - 50_000_000)
	ht := newHyperTable(s, b, h, n)
	x := ht.ft.supp + 100_000_000 // other side of the mode, distance 10^8
	lx, sx := ht.lnP(x)
	start := time.Now()
	if !ht.probAtMost(x, lx, sx) {
		t.Error("forced near-tie not counted as a tie")
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("near-tie decision took %v", d)
	}
	// an exact tie of a symmetric table (equal cells) far away
	const m2 = 1 << 61
	hs := newHyperTable(m2/2-100, m2, m2, 2*m2) // mode m2/2, mirror m2/2+100
	ls, ss := hs.lnP(hs.ft.supp)
	if !hs.probAtMost(m2/2+100, ls, ss) || hs.probAtMost(m2/2+99, ls, ss) {
		t.Error("symmetric tie decided wrongly")
	}
}
