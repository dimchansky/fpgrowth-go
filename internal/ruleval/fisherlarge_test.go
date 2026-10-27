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
// tables with up to 256 tables (oracle.FisherMaxTables with
// FPG_MEASURE_CASES) and totals up to 2^63,
// within the documented bound 2^-36 relative (+2^-1000).
func TestFisherLargeMatchesReference(t *testing.T) {
	t.Parallel()
	count, maxT := 80, int64(256) // FPG_MEASURE_CASES: 300 tables up to the reference's limit
	if testing.Short() || raceEnabled {
		count, maxT = 40, 200
	}
	if measureCases(t) > 0 {
		count, maxT = 300, oracle.FisherMaxTables
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

// TestFisherLargeNearTie checks the near-ties of FisherProb far from the
// observed table (review of #16, F3): they are decided at 320 bits
// (cmpWeightBig) within a bounded time, also at n = 10^18 where the exact
// big-integer comparison would multiply about 10^8 63-bit numbers.
func TestFisherLargeNearTie(t *testing.T) {
	t.Parallel()
	// The review's table: x lies 696,676 tables from supp on the other
	// side of the mode, and ln P(x) - ln P(supp) = +1.35e-14, within the
	// float error bound. x must not be summed: FisherProb then equals
	// FisherChi2 and FisherInfo, whose regions are the same two tails
	// (counting x as a tie made it larger by P(x) = 6.2e-7).
	const s, b, h, n = 814290028136, 1744408929115, 2100559551280, 4499911755383
	ht := newHyperTable(s, b, h, n)
	x := tract.Supp(814290724812)
	ls, ss := ht.lnP(ht.ft.supp)
	if ht.probAtMost(x, ls, ss) {
		t.Errorf("table %d counted with P(x) <= P(supp)", x)
	}
	if c, ok := ht.ft.cmpWeightBig(x); !ok || c >= 0 {
		t.Errorf("cmpWeightBig(%d) = %d, %v; want -1 (P(x) > P(supp))", x, c, ok)
	}
	f, c := Eval(FisherProb, s, b, h, n), Eval(FisherChi2, s, b, h, n)
	if math.Abs(f-c) > 0x1p-36*c {
		t.Errorf("FisherProb %.17g, FisherChi2 %.17g", f, c)
	}
	// a far comparison at n = 10^18 (σ ≈ 2·10^8) is cheap
	const n2, b2, h2 = 1_000_000_000_000_000_000, 400_000_000_000_000_000, 300_000_000_000_000_000
	s2 := tract.Supp(120_000_000_000_000_000 - 50_000_000)
	h18 := newHyperTable(s2, b2, h2, n2)
	c18, ok := h18.ft.cmpWeightBig(h18.ft.supp + 100_000_000)
	lx, _ := h18.lnP(h18.ft.supp + 100_000_000)
	l2, _ := h18.lnP(h18.ft.supp)
	if want := -cmpFloat(lx, l2); !ok || c18 != want { // W ∝ 1/P
		t.Errorf("cmpWeightBig at 1e18 = %d, %v; float ln P says %d", c18, ok, want)
	}
	// an exact tie of a symmetric table (equal cells) far away
	const m2 = 1 << 61
	hs := newHyperTable(m2/2-100, m2, m2, 2*m2) // mode m2/2, mirror m2/2+100
	ls, ss = hs.lnP(hs.ft.supp)
	if !hs.probAtMost(m2/2+100, ls, ss) || hs.probAtMost(m2/2+99, ls, ss) {
		t.Error("symmetric tie decided wrongly")
	}
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// TestCmpWeightBig checks the 320-bit comparison against the exact
// big-integer one (cmpWeight) on tables at most 1024 apart, including
// exact and near ties, and ln m! against the exact factorial.
func TestCmpWeightBig(t *testing.T) {
	t.Parallel()
	ln2 := bigConsts()[0]
	for _, m := range []tract.Supp{0, 1, 2, 10, 255, 256, 257, 300, 600} {
		want := bigLog(newBig().SetInt(new(big.Int).MulRange(1, int64(max(m, 1)))), ln2)
		if m < 2 {
			want = newBig()
		}
		d := newBig().Sub(bigLnFact(m), want)
		if d.Abs(d).Cmp(big.NewFloat(0x1p-220)) > 0 {
			t.Errorf("bigLnFact(%d) off by %v", m, d)
		}
	}
	r := rand.New(rand.NewPCG(16, 5))
	decided, ties := 0, 0
	for range 400 {
		n := int64(200) + r.Int64N(1<<40)
		b := 1 + r.Int64N(n-1)
		h := 1 + r.Int64N(n-1)
		if r.IntN(3) == 0 { // symmetric margins: exact ties
			n &^= 1
			b, h = n/2, n/2
		}
		lo, hi := max(0, b+h-n), min(b, h)
		s := lo + r.Int64N(hi-lo+1)
		ft := newFisherTable(tract.Supp(s), tract.Supp(b), tract.Supp(h), tract.Supp(n))
		for range 5 {
			x := ft.supp + tract.Supp(r.Int64N(2049)) - 1024
			if x < 0 || x > ft.body || x == ft.supp {
				continue
			}
			want := ft.cmpWeight(x)
			got, ok := ft.cmpWeightBig(x)
			if !ok {
				ties++
				if want != 0 && ft.cells(x) != ft.cells(ft.supp) {
					t.Logf("undecided near-tie (exact sign %d) at %v x=%d", want, ft, x)
				}
				continue
			}
			decided++
			if got != want {
				t.Errorf("cmpWeightBig(%d) on %v = %d, exact %d", x, ft, got, want)
			}
		}
	}
	t.Logf("%d decided, %d ties", decided, ties)
}

// TestFisherLongWalk compares FisherSupport and FisherChi2 on tables with
// σ = 10^5 (walks of about 10^6 tables, where the re-anchoring and the
// compensated sum matter) with an independent sum: exp(lnP(x)) of every
// table within 14σ of the tail start, accumulated exactly in big.Float.
// The tolerance 2^-48 is far below the contract (2^-36) but well above the
// walk's actual error (about 2e-16 here): without re-anchoring the walk
// drifts by 6e-15 to 3e-14, and a plain instead of a compensated sum is
// off by 5e-13, so both are caught.
func TestFisherLongWalk(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("sums 2.8·10^6 tables")
	}
	const n, b, h = 160_000_000_000, 80_000_000_000, 80_000_000_000 // σ = 10^5
	const sigma = 100_000
	for _, s := range []tract.Supp{40_000_000_000 + 1, 40_000_000_000 + 2*sigma, 40_000_000_000 + 40_000} {
		ht := newHyperTable(s, b, h, n)
		sum := func(lo, hi tract.Supp) *big.Float {
			acc := new(big.Float).SetPrec(256)
			for x := max(lo, 0); x <= min(hi, ht.ft.body); x++ {
				lp, _ := ht.lnP(x)
				acc.Add(acc, new(big.Float).SetFloat64(math.Exp(lp)))
			}
			return acc
		}
		want, _ := sum(s, s+14*sigma).Float64() // the upper tail from s
		if got := Eval(FisherSupport, s, b, h, n); math.Abs(got-want) > 0x1p-48*want {
			t.Errorf("FisherSupport(%d) = %.17g, sum %.17g (rel %.3g)", s, got, want, math.Abs(got-want)/want)
		}
		mirror := 2*40_000_000_000 - s // E = 4·10^10 exactly
		w2 := sum(s, s+14*sigma)
		w2.Add(w2, sum(mirror-14*sigma, mirror))
		want2, _ := w2.Float64()
		if got := Eval(FisherChi2, s, b, h, n); math.Abs(got-want2) > 0x1p-48*want2 {
			t.Errorf("FisherChi2(%d) = %.17g, sum %.17g (rel %.3g)", s, got, want2, math.Abs(got-want2)/want2)
		}
	}
}

// TestFisherProbUnderflow checks the underflow shortcut of FisherProb
// (review of #16, P-6): strongly associated tables at large totals, whose
// p-value underflows to 0, are evaluated without the near-tie machinery,
// in microseconds and without allocations (not parallel: AllocsPerRun).
func TestFisherProbUnderflow(t *testing.T) {
	for _, n := range []tract.Supp{1e15, 1e16, 1e18, 9e18} {
		for _, f := range []float64{0.05, 0.2, 0.24} {
			s, b, h := tract.Supp(f*float64(n)), n/2, n/2+1
			ht := newHyperTable(s, b, h, n)
			ls, ss := ht.lnP(ht.ft.supp)
			if !ht.probUnderflows(ls, ss) {
				t.Errorf("n=%d s=%d: shortcut not taken (ln P(supp) = %v)", n, s, ls)
			}
			if v := Eval(FisherProb, s, b, h, n); v != 0 {
				t.Errorf("n=%d s=%d: FisherProb = %v, want 0", n, s, v)
			}
			if a := testing.AllocsPerRun(10, func() { Eval(FisherProb, s, b, h, n) }); a != 0 {
				t.Errorf("n=%d s=%d: %v allocations", n, s, a)
			}
		}
	}
}

// TestFisherProbUnderflowCutoff checks that the shortcut fires only when
// the exact p-value rounds to 0: tables whose observed probability is
// around e^-745 (b·ln(h/n) from -760 to -730 with supp = body), compared
// with the exact reference. A table for which the shortcut fires must
// have the exact value 0 in float64, and every value must match.
func TestFisherProbUnderflowCutoff(t *testing.T) {
	t.Parallel()
	ref, _ := oracle.RefByLetter('f')
	r := rand.New(rand.NewPCG(16, 6))
	count := 60
	if testing.Short() || raceEnabled {
		count = 15
	}
	fired, positive := 0, 0
	for range count {
		b := int64(200 + r.IntN(300))
		n := int64(1e9) + r.Int64N(int64(1e18))
		l := -760 + 30*r.Float64()
		h := int64(float64(n) * math.Exp(l/float64(b)))
		if h <= b || h >= n-b {
			continue
		}
		s := b
		want, err := ref.Eval(tract.Supp(s), tract.Supp(b), tract.Supp(h), tract.Supp(n))
		if err != nil {
			t.Fatal(err)
		}
		ht := newHyperTable(tract.Supp(s), tract.Supp(b), tract.Supp(h), tract.Supp(n))
		ls, ss := ht.lnP(ht.ft.supp)
		if ht.probUnderflows(ls, ss) {
			fired++
			if want.V != 0 {
				t.Errorf("shortcut for s=%d b=%d h=%d n=%d, exact p-value %v", s, b, h, n, want)
			}
		}
		if want.V > 0 {
			positive++
		}
		if got := Eval(FisherProb, tract.Supp(s), tract.Supp(b), tract.Supp(h), tract.Supp(n)); !want.Matches(got) {
			t.Errorf("FisherProb(%d, %d, %d, %d) = %v, reference %v", s, b, h, n, got, want)
		}
	}
	t.Logf("shortcut fired %d times, %d tables with a positive p-value", fired, positive)
	if fired == 0 || positive == 0 {
		t.Error("the sweep does not straddle the cutoff")
	}
}

// TestFisherSubnormalAnchor checks tiny FisherProb and FisherChi2 values
// whose walk anchor exp(ln P) underflows while the summed range does not
// (review of #16, L1: FisherProb(2748994426, 5500000000, 5500000007,
// 11000000000) is 8.6e-322 but was 0). σ ≈ 2.6·10^4; the reference sums
// exp(ln P(x)) over both tails at 256 bits, scaled by e^-700 before
// rounding so that subnormal terms keep their weight.
func TestFisherSubnormalAnchor(t *testing.T) {
	t.Parallel()
	const b, h, n tract.Supp = 5500000000, 5500000007, 11000000000
	for _, s := range []tract.Supp{2748994426, 2748994000, 2748995000, 2748996000, 2749000000} {
		ht := newHyperTable(s, b, h, n)
		ls, _ := ht.lnP(ht.ft.supp)
		scale := new(big.Float).SetPrec(256)
		add := func(lp float64) { scale.Add(scale, new(big.Float).SetFloat64(math.Exp(lp+700))) }
		// lower tail: x <= supp (supp is below the mode)
		for x := ht.ft.supp; x >= 0; x-- {
			lp, _ := ht.lnP(x)
			if lp < ls-60 {
				break
			}
			add(lp)
		}
		// upper tail: from the first table beyond the mode with ln P <= ls
		x := ht.mode
		for step := tract.Supp(1 << 30); step > 0; step /= 2 { // bisection on ln P
			if y := x + step; y <= ht.ft.body {
				if lp, _ := ht.lnP(y); lp > ls {
					x = y
				}
			}
		}
		for x++; x <= ht.ft.body; x++ {
			lp, _ := ht.lnP(x)
			if lp < ls-60 {
				break
			}
			add(lp)
		}
		// want = scale·e^-700, rounded to float64 (subnormal if small)
		want, _ := new(big.Float).Mul(scale, new(big.Float).SetFloat64(math.Exp(-700))).Float64()
		got := Eval(FisherProb, s, b, h, n)
		if want == 0 || math.Abs(got-want) > 0x1p-36*want+0x1p-1070 {
			t.Errorf("FisherProb(%d, %d, %d, %d) = %v, reference %v (ln P(supp) = %v)", s, b, h, n, got, want, ls)
		}
	}
	if got := Eval(FisherProb, 2748994426, b, h, n); got < 8.5e-322 || got > 8.7e-322 {
		t.Errorf("FisherProb(2748994426, ...) = %v, want 8.6e-322", got)
	}
}

// TestFisherNearTieCost bounds the cost of a far near-tie comparison at
// n = 10^18 by counting allocations rather than by wall time, which
// hosted CI runners exceed under load (124 ms against a 100 ms bound on
// macos-latest). One such comparison makes 1,138 allocations (measured on
// arm64, amd64 and with -race). The bound catches regressions that
// allocate more, such as a higher working precision or more big-float
// calls per comparison; it does not see slower non-allocating loops.
// Not parallel: testing.AllocsPerRun requires it.
func TestFisherNearTieCost(t *testing.T) {
	const n2, b2, h2 = 1_000_000_000_000_000_000, 400_000_000_000_000_000, 300_000_000_000_000_000
	s2 := tract.Supp(120_000_000_000_000_000 - 50_000_000)
	h18 := newHyperTable(s2, b2, h2, n2)
	if a := testing.AllocsPerRun(3, func() {
		h18.ft.cmpWeightBig(h18.ft.supp + 100_000_000)
	}); a > 2500 {
		t.Errorf("far comparison made %.0f allocations, want at most 2500", a)
	}
}

// infoCutoff is the threshold of infoUnderflows for total weight n.
func infoCutoff(n tract.Supp) float64 {
	return float64(float64(6*math.Log(float64(n)+1))+800) * (1 + 0x1p-40)
}

// slowFisherInfo is FisherInfo for base > maxFact without the underflow
// shortcut: the region sum that fetInfoLarge computes otherwise.
func slowFisherInfo(supp, body, head, base tract.Supp) float64 {
	ht := newHyperTable(supp, body, head, base)
	var st stepper
	v, _ := ht.infoSum(infoNats(ht.ft.supp, ht.ft.body, ht.ft.head, ht.ft.base), &st)
	return v
}

// TestFisherInfoUnderflow checks the underflow shortcut of FisherInfo
// (issue #40, review of #16, P-9) on the issue's table scan: strongly
// associated tables at large totals, whose p-value underflows to 0.
// Without the shortcut each evaluation decided its near-ties with
// cmpXLogX and made 542 to 11,369 allocations (n = 10^15 to 9·10^18,
// measured on arm64); with it, none. The value must be the one of the
// region sum, bit for bit. Not parallel: AllocsPerRun.
func TestFisherInfoUnderflow(t *testing.T) {
	for _, n := range []tract.Supp{1e15, 1e16, 1e18, 9e18} {
		for _, f := range []float64{0.05, 0.2, 0.24, 0.7} {
			s, b, h := tract.Supp(f*float64(n)), n/2, n/2+1
			if f > 0.5 { // positive association: supp > E = n/4
				s, h = tract.Supp(f*float64(n/2)), n/2
			}
			ht := newHyperTable(s, b, h, n)
			if ss := infoNats(ht.ft.supp, ht.ft.body, ht.ft.head, ht.ft.base); !ht.infoUnderflows(ss) {
				t.Errorf("n=%d s=%d: shortcut not taken (S(supp) = %v)", n, s, ss)
			}
			v := Eval(FisherInfo, s, b, h, n)
			if slow := slowFisherInfo(s, b, h, n); math.Float64bits(v) != math.Float64bits(slow) || v != 0 {
				t.Errorf("n=%d s=%d: FisherInfo = %v, region sum %v, want 0 for both", n, s, v, slow)
			}
			if a := testing.AllocsPerRun(10, func() { Eval(FisherInfo, s, b, h, n) }); a != 0 {
				t.Errorf("n=%d s=%d: %v allocations", n, s, a)
			}
		}
	}
}

// TestFisherInfoUnderflowCutoff checks the shortcut around its cutoff
// against the exact reference: tables with at most 1024 tables (body
// from 200 to 1000, supp near body) and S(supp) from 0.5 to 1.25 times
// the cutoff, at totals from 10^13 to 9·10^18. A table for which the
// shortcut fires must have the exact value 0 and the region sum 0 (bit
// for bit); every value must match the reference.
func TestFisherInfoUnderflowCutoff(t *testing.T) {
	t.Parallel()
	ref, _ := oracle.RefByLetter('m')
	r := rand.New(rand.NewPCG(40, 1))
	count := 40
	if testing.Short() || raceEnabled {
		count = 15
	}
	fired, unfired, positive := 0, 0, 0
	for i := range count {
		b := tract.Supp(200 + r.IntN(800))
		n := tract.Supp(math.Exp(math.Log(1e13) + r.Float64()*(math.Log(9e18)-math.Log(1e13))))
		// three bands: positive p-values, around the cutoff, beyond it
		lo, w := [3]float64{0.5, 0.95, 1.05}[i%3], [3]float64{0.2, 0.1, 0.2}[i%3]
		target := infoCutoff(n) * (lo + w*r.Float64())
		s := b - tract.Supp(r.IntN(4))
		// S(supp) decreases as head grows: bisect head in [b, n/2] for
		// the first head with S(supp) <= target.
		hl, hh := b, n/2
		for hl < hh {
			mid := hl + (hh-hl)/2
			if infoNats(s, b, mid, n) > target {
				hl = mid + 1
			} else {
				hh = mid
			}
		}
		h := hl
		ht := newHyperTable(s, b, h, n)
		ss := infoNats(ht.ft.supp, ht.ft.body, ht.ft.head, ht.ft.base)
		want, err := ref.Eval(s, b, h, n)
		if err != nil {
			t.Fatal(err)
		}
		got := Eval(FisherInfo, s, b, h, n)
		if ht.infoUnderflows(ss) {
			fired++
			if slow := slowFisherInfo(s, b, h, n); want.V != 0 || math.Float64bits(got) != math.Float64bits(slow) {
				t.Errorf("shortcut for (%d, %d, %d, %d): exact %v, region sum %v, got %v", s, b, h, n, want, slow, got)
			}
		} else {
			unfired++
		}
		if want.V > 0 {
			positive++
		}
		if !want.Matches(got) {
			t.Errorf("FisherInfo(%d, %d, %d, %d) = %v, reference %v", s, b, h, n, got, want)
		}
	}
	t.Logf("shortcut fired %d times, not fired %d times (%d positive values)", fired, unfired, positive)
	if fired == 0 || unfired == 0 || positive == 0 {
		t.Error("the sweep does not straddle the cutoff")
	}
}
