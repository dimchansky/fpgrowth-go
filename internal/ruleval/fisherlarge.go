package ruleval

import (
	"math"
	"math/bits"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Fisher's exact tests for total weights above maxFact.
//
// Deviation from upstream (ALG-1): upstream computes every table
// probability as exp(com - 4 lnΓ) from log-gamma values of size up to
// lnΓ(n+1) ≈ n·ln n, so its relative error grows like 2^-52·n·ln n
// (about 1e-8 at n = 10^6, 1e-5 at 10^9, meaningless beyond 10^14), and it
// sums all min(body, head)+1 tables. For base > maxFact the tests are
// computed here instead, with an error bound that does not depend on n:
//
//   - The log-probability of a table comes from Stirling's formula with
//     the exact deviation D of the table (hyperTable.lnP): the m·ln m
//     terms of the log-factorials sum to -n·I, n times the mutual
//     information in nats, which infoNats computes without cancellation.
//   - The tables of a summed range are visited from the one closest to the
//     mode outwards, with the exact ratio of consecutive probabilities
//     (the hypergeometric distribution is unimodal and log-concave), and
//     re-anchored with lnP every fisherAnchorEvery tables. A side stops
//     when a geometric bound on the rest of the range falls below 2^-60
//     of the running sum, so the cost is about 9 standard deviations of
//     the distribution per side instead of min(body, head)+1 tables.
//   - The tables are summed directly for all four tests (no complement
//     1 - Σ as upstream uses for FisherChi2 and FisherSupport), so every
//     p-value has a relative error bound, also tiny ones.
//   - The ranges of FisherProb and FisherInfo are found by bisection on
//     the side opposite to the observed table; near-ties are decided
//     exactly or at 320 bits with a bounded cost (fisherTieExact,
//     cmpWeightBig).
//
// Values for base <= maxFact (where upstream's log-gamma values are table
// lookups) are computed as before, bit-identical to upstream's apart from
// the documented tie and clamp fixes.

// fisherAnchorEvery is the number of ratio steps between two exact
// anchors of a walk. Each step has a relative rounding error of at most
// 9u (u = 2^-53), so the walk adds at most 576u between anchors.
const fisherAnchorEvery = 64

// fisherTailRel is the relative bound on the neglected rest of a walk.
const fisherTailRel = 0x1p-60

// fisherTieExact is the largest distance |x - supp| for which FisherProb
// decides a near-tie of P(x) and P(supp) exactly (cmpWeight, big integers
// of up to fisherTieExact·63 bits). Beyond it the logarithms of the
// weights are compared at 320 bits (cmpWeightBig, bounded cost); only
// tables whose probabilities agree within a relative 2^-200 then count
// as ties (summed, like exact ties).
const fisherTieExact = 1024

// hyperTable is a normalized table (see fisherTable) for base > maxFact
// with the per-evaluation parts of its log-probabilities.
type hyperTable struct {
	ft   fisherTable
	mode tract.Supp // a mode of the distribution of the support
	m    float64    // b·(n-b)·h·(n-h)/n (rounded, see lnP)
	sigM float64    // σ(b) + σ(n-b) + σ(h) + σ(n-h) - σ(n)
}

// validTable reports whether the arguments form a valid 2x2 table (see
// the package documentation).
func validTable(supp, body, head, base tract.Supp) bool {
	return base > 0 && supp >= 0 && supp <= body && supp <= head &&
		body <= base && head <= base && body-supp <= base-head
}

func newHyperTable(supp, body, head, base tract.Supp) hyperTable {
	ft := newFisherTable(supp, body, head, base)
	b, h, n := ft.body, ft.head, ft.base
	// mode: floor((b+1)·(h+1)/(n+2)), exactly (the quotient is at most
	// min(b, h) + 1 < 2^63, so Div64 cannot overflow).
	hi, lo := bits.Mul64(uint64(b)+1, uint64(h)+1)
	q, _ := bits.Div64(hi, lo, uint64(n)+2)
	m := min(tract.Supp(q), b)
	nb, nh := float64(n-b), float64(n-h)
	prod := float64(float64(float64(b)*nb)/float64(n)) * float64(float64(h)*nh)
	return hyperTable{
		ft:   ft,
		mode: m,
		m:    prod,
		sigM: stirlerr(b) + stirlerr(n-b) + stirlerr(h) + stirlerr(n-h) - stirlerr(n),
	}
}

// lnP returns ln P(x), the log-probability of the table with support x,
//
//	ln P(x) = -S(x) + (3-k)/2·ln(2π) + ½·ln(M/Π_{c>0} c) + σM - Σ_{c>0} σ(c),
//
// from ln m! = m·ln m - m + ½·ln(2πm) + σ(m) (σ = stirlerr) for the four
// margins, the total and the k non-empty cells c: the m·ln m terms sum to
// -S(x) = -n·I(x) (infoNats), the linear terms cancel, and M is the
// product of the margins divided by n. It also returns S(x).
//
// Error (u = 2^-53): S(x) has a relative error below 65u (infoNats); the
// float quotient M/Πc below 15u, so its logarithm adds 15u + u·|ln|
// (<= 200u); the σ values are accurate to about 20u. So the absolute
// error of ln P(x) is below 65u·S(x) + 256u, and lnPErr bounds it with
// almost a factor of two to spare.
func (t *hyperTable) lnP(x tract.Supp) (lp, s float64) {
	b, h, r := t.ft.body, t.ft.head, t.ft.rest
	s = infoNats(x, b, h, t.ft.base)
	cells := [4]tract.Supp{x, b - x, h - x, r + x}
	pc, sig := 1.0, t.sigM
	k := 0
	for _, c := range cells {
		if c > 0 {
			k++
			pc = float64(pc * float64(c))
			sig -= stirlerr(c)
		}
	}
	lp = -s + float64(float64(3-k)*(0.5*math.Log(2*math.Pi))) + float64(0.5*math.Log(t.m/pc)) + sig
	return lp, s
}

// lnPErr bounds the absolute error of lnP for a table with S(x) = s.
func lnPErr(s float64) float64 { return 0x1p-46 * (s + 4) }

// ratioUp returns P(x+1)/P(x) for x < body, from the exact cell counts.
func (t *hyperTable) ratioUp(x tract.Supp) float64 {
	b, h, r := t.ft.body, t.ft.head, t.ft.rest
	return float64(float64(b-x)*float64(h-x)) / float64(float64(x+1)*float64(r+x+1))
}

// ratioDown returns P(x-1)/P(x) for x > 0.
func (t *hyperTable) ratioDown(x tract.Supp) float64 {
	b, h, r := t.ft.body, t.ft.head, t.ft.rest
	return float64(float64(x)*float64(r+x)) / float64(float64(b-x+1)*float64(h-x+1))
}

// kahan is a compensated (Neumaier) sum of non-negative terms.
type kahan struct{ sum, c float64 }

func (k *kahan) add(v float64) {
	t := k.sum + v
	if k.sum >= v {
		k.c += (k.sum - t) + v
	} else {
		k.c += (v - t) + k.sum
	}
	k.sum = t
}

func (k *kahan) value() float64 { return k.sum + k.c }

// sumRange returns Σ_{x=lo..hi} P(x) as exp(lnA)·q (q >= 1), walking
// from x0 = clamp(mode, lo, hi) outwards: on both sides of x0 the terms
// do not increase (the ratio towards the mode is at least 1 on each side
// of it), so a side stops once its next term times r/(1-r), where r < 1
// is the last ratio (the ratios decrease away from the mode, so this
// bounds the rest of the side geometrically), is below fisherTailRel of
// the running sum.
func (t *hyperTable) sumRange(lo, hi tract.Supp, st *stepper) (lnA, q float64, err error) {
	x0 := min(max(t.mode, lo), hi)
	lnA, _ = t.lnP(x0)
	var acc kahan
	acc.add(1)
	p := 1.0
	for x, k := x0, 1; x < hi; k++ { // upwards
		if err := st.step(); err != nil {
			return 0, 0, err
		}
		r := t.ratioUp(x)
		x++
		if k%fisherAnchorEvery == 0 {
			lp, _ := t.lnP(x)
			p = math.Exp(lp - lnA)
		} else {
			p = float64(p * r)
		}
		acc.add(p)
		if r < 1 && float64(p*r) <= float64(fisherTailRel*float64((1-r)*acc.sum)) {
			break
		}
	}
	p = 1
	for x, k := x0, 1; x > lo; k++ { // downwards
		if err := st.step(); err != nil {
			return 0, 0, err
		}
		r := t.ratioDown(x)
		x--
		if k%fisherAnchorEvery == 0 {
			lp, _ := t.lnP(x)
			p = math.Exp(lp - lnA)
		} else {
			p = float64(p * r)
		}
		acc.add(p)
		if r < 1 && float64(p*r) <= float64(fisherTailRel*float64((1-r)*acc.sum)) {
			break
		}
	}
	return lnA, acc.value(), nil
}

// fisherSum adds the probability sums of the ranges [lo_i, hi_i] (empty
// if lo_i > hi_i) and clamps the result to [0, 1].
func (t *hyperTable) fisherSum(st *stepper, ranges ...[2]tract.Supp) (float64, error) {
	var p float64
	for _, rg := range ranges {
		if rg[0] > rg[1] {
			continue
		}
		lnA, q, err := t.sumRange(rg[0], rg[1], st)
		if err != nil {
			return math.NaN(), err
		}
		a := math.Exp(lnA)
		if a < 0x1p-1022 {
			// exp(lnA) is subnormal or 0 although q (>= 1) can be large:
			// scale in the log domain (review of #16, L1). Normal anchors
			// keep the product, so their values are unchanged.
			a, q = math.Exp(lnA+math.Log(q)), 1
		}
		p += float64(a * q)
	}
	return clampUnit(p), nil
}

// probAtMost reports whether P(x) <= P(supp), given lnP(supp) = ls and
// S(supp) = ss: from the computed log-probabilities when they differ by
// more than their error bounds, else exactly (equal cells, or cmpWeight
// for |x - supp| <= fisherTieExact), else at 320 bits (cmpWeightBig;
// true for a tie within 2^-200).
func (t *hyperTable) probAtMost(x tract.Supp, ls, ss float64) bool {
	lx, sx := t.lnP(x)
	tol := lnPErr(sx) + lnPErr(ss)
	switch {
	case lx < ls-tol:
		return true
	case lx > ls+tol:
		return false
	case t.ft.cells(x) == t.ft.cells(t.ft.supp):
		return true
	case max(x-t.ft.supp, t.ft.supp-x) <= fisherTieExact:
		return t.ft.cmpWeight(x) >= 0
	}
	c, _ := t.ft.cmpWeightBig(x) // 0 (a tie) when undecided
	return c >= 0
}

// infoAtLeast reports whether I(x) >= I(supp), given S(supp) = ss: from
// the computed values of S when they differ by more than their error
// bounds (65u each), else exactly (cmpXLogX, which is bounded and
// counts undecidable differences below 2^-65000 relative as ties).
func (t *hyperTable) infoAtLeast(x tract.Supp, ss float64) bool {
	sx := infoNats(x, t.ft.body, t.ft.head, t.ft.base)
	tol := 0x1p-45 * (sx + ss)
	switch {
	case sx > ss+tol:
		return true
	case sx < ss-tol:
		return false
	}
	return cmpXLogX(t.ft.cells(x), t.ft.cells(t.ft.supp)) >= 0
}

// firstTrue returns the smallest x in [lo, hi] with pred(x), or hi+1 if
// there is none, for a predicate that is false up to some point and true
// from there on (bisection; at most 64 evaluations).
func firstTrue(lo, hi tract.Supp, st *stepper, pred func(tract.Supp) bool) (tract.Supp, error) {
	for lo <= hi {
		if err := st.step(); err != nil {
			return 0, err
		}
		mid := lo + (hi-lo)/2
		if pred(mid) {
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	return lo, nil
}

// lastTrue returns the largest x in [lo, hi] with pred(x), or lo-1 if
// there is none, for a predicate that is true up to some point and false
// from there on.
func lastTrue(lo, hi tract.Supp, st *stepper, pred func(tract.Supp) bool) (tract.Supp, error) {
	for lo <= hi {
		if err := st.step(); err != nil {
			return 0, err
		}
		mid := lo + (hi-lo)/2
		if pred(mid) {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return hi, nil
}

// lnHalfMinFloat is below ln(2^-1075) = -745.1332, the logarithm of half
// the smallest positive (subnormal) float64: every value below 2^-1075
// rounds to 0.
const lnHalfMinFloat = -745.2

// probUnderflows reports whether the FisherProb p-value certainly rounds
// to 0 in float64, given the computed lnP(supp) = ls and S(supp) = ss.
// The p-value sums P(x) over tables with P(x) <= P(supp) (and near-ties
// counted as ties agree with P(supp) within a relative 2^-200), so it is
// at most (body+1)·P(supp)·(1+2^-200) for the body+1 tables of the
// normalized table, and ln P(supp) <= ls + lnPErr(ss). If that bound is
// below lnHalfMinFloat, the exact p-value is below 2^-1075 and its
// correctly rounded value is 0 whatever the region is, so no tie decision
// (cmpWeight, cmpWeightBig) can change the result. Strongly associated
// tables at large totals hit this case, and the shortcut keeps them at
// the cost of one lnP (review of #16, P-6).
func (t *hyperTable) probUnderflows(ls, ss float64) bool {
	return ls+lnPErr(ss)+math.Log(float64(t.ft.body)+1)+1e-15 < lnHalfMinFloat
}

// fetProbLarge is FisherProb for base > maxFact: the tables with
// P(x) <= P(supp). On the observed table's side of the mode these are
// all tables from supp outwards (P is monotone there); on the other side
// they form the range beyond the first table with P(x) <= P(supp).
func fetProbLarge(supp, body, head, base tract.Supp, abort func() error) (float64, error) {
	t := newHyperTable(supp, body, head, base)
	st := stepper{abort: abort}
	s, b := t.ft.supp, t.ft.body
	ls, ss := t.lnP(s)
	if t.probUnderflows(ls, ss) {
		return 0, nil
	}
	le := func(x tract.Supp) bool { return t.probAtMost(x, ls, ss) }
	if s <= t.mode {
		c, err := firstTrue(s+1, b, &st, le)
		if err != nil {
			return math.NaN(), err
		}
		return t.fisherSum(&st, [2]tract.Supp{0, s}, [2]tract.Supp{c, b})
	}
	a, err := lastTrue(0, s-1, &st, le)
	if err != nil {
		return math.NaN(), err
	}
	return t.fisherSum(&st, [2]tract.Supp{0, a}, [2]tract.Supp{s, b})
}

// infoUnderflows reports whether the FisherInfo p-value certainly rounds
// to 0 in float64, given the computed S(supp) = ss (issue #40, review of
// #16, P-9), for n = base. It is true when S(supp) > 6·ln(n+1) + 800:
// the computed ss has a relative error below 65u (infoNats), and the
// factor 1 + 2^-40 covers it and the rounding of the cutoff.
//
// Every table x that infoSum sums has S(x) >= S(supp) - 2^-65000 (its
// region; cmpXLogX counts only smaller differences as ties), and every
// table has P(x) <= (n+1)·e^-S(x) (entropy bounds of the binomial
// coefficients), so the exact p-value is below
// (body+1)·(n+1)·e^-S(supp)·2 < e^-750, whose correctly rounded value is
// 0. The cutoff also makes infoSum itself return 0: the observed table
// is then far from the mode, so each summed range lies on one side of
// the expected support, its walk starts at the end nearest to it with
// ln P below -799, and, by the error bound of lnP (lnPErr), the scaled
// sum q is at most 2^60·e^(3·ln(n+1) + 2^-45·S + 1), so
// exp(lnA + ln q) is below the smallest subnormal. That bound on q
// holds for S(supp) up to 1.88·10^16; beyond it the identity with
// infoSum rests on the actual error of lnP (scans in the design record).
// Proof: docs/reviews/post-v0.1.0/issue-40-design.md.
func (t *hyperTable) infoUnderflows(ss float64) bool {
	return ss > infoCutoff(t.ft.base)
}

// infoCutoff is the threshold of infoUnderflows for total weight n:
// (6·ln(n+1) + 800)·(1 + 2^-40), at least 800.
func infoCutoff(n tract.Supp) float64 {
	return float64(float64(6*math.Log(float64(n)+1))+800) * (1 + 0x1p-40)
}

// fetInfoLarge is FisherInfo for base > maxFact: the tables with
// I(x) >= I(supp). It returns 0 without summing when the p-value
// certainly underflows (infoUnderflows), else infoSum.
func fetInfoLarge(supp, body, head, base tract.Supp, abort func() error) (float64, error) {
	t := newHyperTable(supp, body, head, base)
	st := stepper{abort: abort}
	ss := infoNats(t.ft.supp, t.ft.body, t.ft.head, t.ft.base)
	// The cutoff is at least 800: ordinary tables skip its logarithm.
	if ss > 800 && t.infoUnderflows(ss) {
		return 0, nil
	}
	return t.infoSum(ss, &st)
}

// infoSum sums the FisherInfo region given S(supp) = ss. I decreases
// strictly up to the expected support E = body·head/base and increases
// beyond it, so the region holds the tables from supp outwards on its
// side of E and a range beyond the first table with I(x) >= I(supp) on
// the other side.
func (t *hyperTable) infoSum(ss float64, st *stepper) (float64, error) {
	s, b := t.ft.supp, t.ft.body
	neg, hi, lo := crossDiff(s, b, t.ft.head, t.ft.base)
	if hi|lo == 0 { // independent: every table has I >= 0 = I(supp)
		return t.fisherSum(st, [2]tract.Supp{0, b})
	}
	ge := func(x tract.Supp) bool { return t.infoAtLeast(x, ss) }
	if neg { // supp < E
		c, err := firstTrue(s+1, b, st, ge)
		if err != nil {
			return math.NaN(), err
		}
		return t.fisherSum(st, [2]tract.Supp{0, s}, [2]tract.Supp{c, b})
	}
	a, err := lastTrue(0, s-1, st, ge)
	if err != nil {
		return math.NaN(), err
	}
	return t.fisherSum(st, [2]tract.Supp{0, a}, [2]tract.Supp{s, b})
}

// fetChi2Large is FisherChi2 for base > maxFact: the tables with
// |x - E| >= |supp - E|, E = body·head/base, i.e. x <= supp and
// x >= ceil(2E - supp) if supp < E, else x >= supp and
// x <= floor(2E - supp). The mirror bound is computed exactly as
// (2·body·head - supp·base)/base in 128-bit integers; upstream computes
// it in double precision, which can misplace it at large totals.
func fetChi2Large(supp, body, head, base tract.Supp, abort func() error) (float64, error) {
	t := newHyperTable(supp, body, head, base)
	st := stepper{abort: abort}
	s, b, h, n := t.ft.supp, t.ft.body, t.ft.head, t.ft.base
	// 2·b·h and s·n are below 2^127.
	bh1, bh0 := bits.Mul64(uint64(b), uint64(h))
	bh1, bh0 = bh1<<1|bh0>>63, bh0<<1
	sn1, sn0 := bits.Mul64(uint64(s), uint64(n))
	neg, hi, lo := crossDiff(s, b, h, n)
	if hi|lo == 0 { // supp = E: every table qualifies
		return t.fisherSum(&st, [2]tract.Supp{0, b})
	}
	if neg { // supp < E: 2bh - sn > bh >= 0
		m0, br := bits.Sub64(bh0, sn0, 0)
		m1, _ := bits.Sub64(bh1, sn1, br)
		q, r := bits.Div64(m1, m0, uint64(n)) // q <= 2·min(b, h) < 2^64
		if r != 0 {
			q++ // ceil
		}
		c := tract.Supp(min(q, uint64(b)+1))
		return t.fisherSum(&st, [2]tract.Supp{0, s}, [2]tract.Supp{c, b})
	}
	// supp > E: the lower range is x <= floor((2bh - sn)/n), empty if
	// 2bh < sn.
	a := tract.Supp(-1)
	if bh1 > sn1 || bh1 == sn1 && bh0 >= sn0 {
		m0, br := bits.Sub64(bh0, sn0, 0)
		m1, _ := bits.Sub64(bh1, sn1, br)
		q, _ := bits.Div64(m1, m0, uint64(n))
		a = tract.Supp(q) // < supp
	}
	return t.fisherSum(&st, [2]tract.Supp{0, a}, [2]tract.Supp{s, b})
}

// fetSuppLarge is FisherSupport for base > maxFact: the tables with
// x >= supp (upper tail of the normalized table), summed directly.
func fetSuppLarge(supp, body, head, base tract.Supp, abort func() error) (float64, error) {
	t := newHyperTable(supp, body, head, base)
	st := stepper{abort: abort}
	return t.fisherSum(&st, [2]tract.Supp{t.ft.supp, t.ft.body})
}

// stirlerr returns σ(m) = ln m! - (m·ln m - m + ½·ln(2πm)), the error of
// Stirling's formula, for m >= 1 (σ(0) is not used).
func stirlerr(m tract.Supp) float64 {
	if m < tract.Supp(len(stirTable)) {
		return stirTable[m]
	}
	return stirSeries(float64(m))
}

// stirSeries is the asymptotic series of σ(m) up to m^-9; for m >= 16 its
// truncation error is below 1.2e-16 (the next term, 691/(360360·m^11)).
func stirSeries(x float64) float64 {
	const s0, s1, s2, s3, s4 = 1.0 / 12, 1.0 / 360, 1.0 / 1260, 1.0 / 1680, 1.0 / 1188
	x2 := 1 / float64(x*x)
	v := s3 - float64(s4*x2)
	v = s2 - float64(v*x2)
	v = s1 - float64(v*x2)
	v = s0 - float64(v*x2)
	return v / x
}

// stirTable holds σ(m) for m < 16, computed downwards from the series at
// 16 with σ(m) = σ(m+1) + (m+½)·ln(1+1/m) - 1 (absolute error about 2^-49).
var stirTable = func() (t [16]float64) {
	next := stirSeries(16)
	for m := 15; m >= 1; m-- {
		fm := float64(m)
		next = next + (float64(float64(fm+0.5)*math.Log1p(1/fm)) - 1)
		t[m] = next
	}
	return t
}()
