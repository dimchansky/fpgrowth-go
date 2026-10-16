package ruleval

import (
	"math"
	"math/big"
	"math/bits"
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// fisherTable is a 2x2 contingency table in the normalized form that
// upstream's Fisher tests work on (the setup code repeated in re_fetprob,
// re_fetchi2, re_fetinfo and re_fetsupp). If rest = base-head-body is
// negative, both margins are complemented; then body and head are swapped
// so that body <= head. The tables with these margins are indexed by the
// support x in [0, body], and their probabilities are hypergeometric:
//
//	ln P(x) = com - lnG(body-x+1) - lnG(head-x+1) - lnG(x+1) - lnG(rest+x+1)
//
// where lnG is logGamma and com does not depend on x. The four cells of
// table x are x, body-x, head-x and rest+x.
type fisherTable struct {
	supp, body, head, base, rest tract.Supp
	com                          float64 // common log-probability term
}

func newFisherTable(supp, body, head, base tract.Supp) fisherTable {
	rest := base - head - body
	if rest < 0 { // complement (exchange) rows and columns
		rest = -rest
		supp -= rest
		body = base - body
		head = base - head
	}
	if head < body { // ensure that body <= head
		head, body = body, head
	}
	com := logGamma(float64(head+1)) +
		logGamma(float64(body+1)) +
		logGamma(float64(base-head+1)) +
		logGamma(float64(base-body+1)) -
		logGamma(float64(base+1))
	return fisherTable{supp: supp, body: body, head: head, base: base, rest: rest, com: com}
}

// logProb returns ln P(x) with upstream's evaluation order.
func (t *fisherTable) logProb(x tract.Supp) float64 {
	return t.com -
		logGamma(float64(t.body-x+1)) -
		logGamma(float64(t.head-x+1)) -
		logGamma(float64(x+1)) -
		logGamma(float64(t.rest+x+1))
}

// cells returns the cell counts of table x, sorted.
func (t *fisherTable) cells(x tract.Supp) [4]int64 {
	c := [4]int64{int64(x), int64(t.body - x), int64(t.head - x), int64(t.rest + x)}
	slices.Sort(c[:])
	return c
}

// clampUnit clamps a probability to [0, 1].
//
// Deviation from upstream: upstream returns the Fisher sums unclamped. The
// complement forms 1 - sum of re_fetchi2 and re_fetsupp cancel
// catastrophically for large tables and can go negative (bug B9), and a sum
// over all tables can exceed 1 by a few ulps (re_fetprob gives
// 1.0000000000000004 for (1, 2, 3, 6)).
func clampUnit(p float64) float64 {
	return min(max(p, 0), 1)
}

// reFetProb ports re_fetprob: Fisher's exact test, two-sided by table
// probability. It sums P(x) over all tables x with P(x) <= P(supp).
//
// Deviation from upstream: tied tables are decided exactly (bug B6).
// Upstream compares log-probabilities computed from four logGamma values
// each and allows a relative tolerance of one DBL_EPSILON of the result,
// but the rounding error grows with the size of the summed logGamma terms.
// It therefore drops tables whose probability equals that of the observed
// table (and sometimes the observed table itself), which makes the p-value
// too small: re_fetprob(1, 2, 5, 7) is 0.5238 instead of 1. Here the
// computed log-probabilities only decide tables that differ from the
// observed one by more than a bound on their rounding error
// (fisherLogProbTol); the rest are compared exactly in integer arithmetic
// (fisherTable.cmpWeight). The probabilities that are summed and the
// summation order are upstream's.
func reFetProb(supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 1
	}
	t := newFisherTable(supp, body, head, base)
	ps := t.logProb(t.supp)
	tol := fisherLogProbTol(base)
	var sum float64
	for x := tract.Supp(0); x <= t.body; x++ {
		p := t.logProb(x)
		if t.probAtMost(x, p, ps, tol) {
			sum += math.Exp(p)
		}
	}
	return clampUnit(sum)
}

// fisherLogProbTol bounds the absolute error of the difference of two
// computed log-probabilities of tables with total weight base. Every
// logGamma argument is at most base+1, so lnG(base+1) bounds the
// magnitude of each of the nine terms and of every partial sum. Table
// lookups, the Lanczos formula and the summation each contribute a few
// ulps of these magnitudes; the bound allows 64 ulps of their total.
func fisherLogProbTol(base tract.Supp) float64 {
	return 64 * epsilon * (9*logGamma(float64(base+1)) + 1)
}

// probAtMost reports whether P(x) <= P(supp), given the computed
// log-probabilities p of table x and ps of the observed table.
func (t *fisherTable) probAtMost(x tract.Supp, p, ps, tol float64) bool {
	switch {
	case x == t.supp:
		return true
	case p < ps-tol:
		return true
	case p > ps+tol:
		return false
	}
	return t.cmpWeight(x) >= 0
}

// cmpWeight compares the tables x and supp exactly. With
// W(x) = x!·(body-x)!·(head-x)!·(rest+x)!, P(x) is proportional to 1/W(x),
// so cmpWeight returns the sign of W(x) - W(supp): +1 if P(x) < P(supp),
// 0 if they are equal and -1 if P(x) > P(supp).
func (t *fisherTable) cmpWeight(x tract.Supp) int {
	s := int64(t.supp)
	b, h, r := int64(t.body), int64(t.head), int64(t.rest)
	xx := int64(x)
	if xx > s {
		// W(x)/W(s) = prod_{k=s+1..x} k·(r+k) / ((b-k+1)·(h-k+1))
		return cmpRangeProducts(xx-s, s+1, r+s+1, b-xx+1, h-xx+1)
	}
	// W(x)/W(s) = prod_{k=x+1..s} (b-k+1)·(h-k+1) / (k·(r+k))
	return cmpRangeProducts(s-xx, b-s+1, h-s+1, xx+1, r+xx+1)
}

// cmpRangeProducts returns the sign of R(a1)·R(a2) - R(c1)·R(c2), where
// R(a) = a·(a+1)···(a+l-1) is a product of l >= 1 consecutive integers and
// all starts are positive. R is strictly increasing in its start, so the
// sign follows from the starts unless one pair of ranges lies strictly
// inside the other; only then are big integers needed, and then only for
// the parts of the ranges that do not cancel.
func cmpRangeProducts(l, a1, a2, c1, c2 int64) int {
	if a1 > a2 {
		a1, a2 = a2, a1
	}
	if c1 > c2 {
		c1, c2 = c2, c1
	}
	switch {
	case a1 == c1 && a2 == c2:
		return 0
	case a1 >= c1 && a2 >= c2:
		return 1
	case a1 <= c1 && a2 <= c2:
		return -1
	case a1 > c1: // c1 < a1 <= a2 < c2
		return -cmpRangeProducts(l, c1, c2, a1, a2)
	}
	// a1 < c1 <= c2 < a2. With m1 = min(c1-a1, l) and m2 = min(a2-c2, l):
	// R(a1)/R(c1) = P(a1, m1) / P(c1+l-m1, m1) and
	// R(a2)/R(c2) = P(a2+l-m2, m2) / P(c2, m2), P(a, m) = R(a) with l = m.
	m1 := min(c1-a1, l)
	m2 := min(a2-c2, l)
	var num, den, f big.Int
	num.MulRange(a1, a1+m1-1)
	num.Mul(&num, f.MulRange(a2+l-m2, a2+l-1))
	den.MulRange(c1+l-m1, c1+l-1)
	den.Mul(&den, f.MulRange(c2, c2+m2-1))
	return num.Cmp(&den)
}

// reFetChi2 ports re_fetchi2: Fisher's exact test, two-sided by the
// distance |x - E| to the expected support E = head·body/base. It sums
// either the more extreme tables or 1 minus the less extreme ones,
// whichever range is shorter. The result is clamped to [0, 1] (clampUnit).
func reFetChi2(supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 1
	}
	t := newFisherTable(supp, body, head, base)
	s := t.supp
	exs := float64(t.head) * float64(t.body) / float64(t.base) // expected support
	var n tract.Supp
	if float64(s) < exs {
		n = tract.Supp(math.Ceil(exs + (exs - float64(s))))
	} else {
		n = s
		s = tract.Supp(math.Floor(exs - (float64(s) - exs)))
	}
	if n > t.body { // clamp the range to the possible maximum
		n = t.body + 1
	}
	if s < 0 {
		s = -1
	}
	var sum float64
	if n-s-4 < s+t.body-n { // fewer less extreme tables
		sum = 1
		for s++; s < n; s++ {
			sum -= math.Exp(t.logProb(s))
		}
		return clampUnit(sum)
	}
	for ; s >= 0; s-- { // fewer more extreme tables
		sum += math.Exp(t.logProb(s))
	}
	for s = n; s <= t.body; s++ {
		sum += math.Exp(t.logProb(s))
	}
	return clampUnit(sum)
}

// reFetSupp ports re_fetsupp: Fisher's exact test, one-sided by support.
// It sums P(x) over the tables with x >= supp (the upper tail of the
// normalized table), or computes 1 minus the lower tail when that is
// shorter. The result is clamped to [0, 1] (clampUnit).
func reFetSupp(supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 1
	}
	t := newFisherTable(supp, body, head, base)
	s := t.supp
	var sum float64
	if s <= t.body-s { // fewer lesser support values
		sum = 1
		for s--; s >= 0; s-- {
			sum -= math.Exp(t.logProb(s))
		}
		return clampUnit(sum)
	}
	for ; s <= t.body; s++ { // fewer greater support values
		sum += math.Exp(t.logProb(s))
	}
	return clampUnit(sum)
}

// reFetInfo ports re_fetinfo: Fisher's exact test, two-sided by
// information gain. It sums P(x) over all tables x whose mutual
// information is at least that of the observed table (computed with
// infoRaw on the normalized table, like upstream).
//
// Deviation from upstream: ties are decided exactly (bug B6, and B7 for
// the observed table). Upstream compares with a cutoff info·(1-DBL_EPSILON),
// so tables with the same information as the observed one are dropped
// whenever rounding makes them a little smaller, and a slightly negative
// observed value makes the observed table exclude itself:
// re_fetinfo(1, 2, 3, 6) is 0.4 instead of 1. Here computed values decide
// only tables that differ by more than a bound on the rounding error
// (fisherInfoTol); the rest are compared exactly (cmpXLogX).
func reFetInfo(supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 1
	}
	t := newFisherTable(supp, body, head, base)
	is := infoRaw(t.supp, t.body, t.head, t.base)
	tol := fisherInfoTol(base)
	var sum float64
	for x := tract.Supp(0); x <= t.body; x++ {
		if t.infoAtLeast(x, infoRaw(x, t.body, t.head, t.base), is, tol) {
			sum += math.Exp(t.logProb(x))
		}
	}
	return clampUnit(sum)
}

// fisherInfoTol bounds the absolute error of the difference of two values
// of infoRaw for tables with total weight base. With n = base, every
// log(t/(r·c)) term has magnitude at most 2·ln n and the cell counts sum
// to n, so the sum divided by n is at most 2·ln n and each step adds at
// most a few ulps of that; the bound allows 128 ulps of 1 + ln n bits.
func fisherInfoTol(base tract.Supp) float64 {
	return 128 * epsilon * (1 + math.Log(float64(base)))
}

// infoAtLeast reports whether the information of table x is at least that
// of the observed table, given the computed values ix and is.
func (t *fisherTable) infoAtLeast(x tract.Supp, ix, is, tol float64) bool {
	switch {
	case x == t.supp:
		return true
	case ix > is+tol:
		return true
	case ix < is-tol:
		return false
	}
	return cmpXLogX(t.cells(x), t.cells(t.supp)) >= 0
}

// cmpXLogX returns the sign of F(u) - F(v), where F(u) = sum u_i·ln(u_i)
// (with 0·ln 0 = 0) over the sorted cell counts u and v of two tables with
// the same margins. The mutual information of such a table is
// (F + const)/n, so this compares the information exactly.
//
// Equal multisets of cells tie. Otherwise the sign of
// prod u_i^u_i - prod v_i^v_i is computed with big.Float at increasing
// precision until a bound on the rounding error separates it from 0. The
// result is exact once the precision covers the exact products (always
// for small tables); beyond maxSelfPowerPrec bits the tables are treated
// as tied, which errs on the conservative side (larger p-value) only for
// relative differences below 2^-65000.
func cmpXLogX(u, v [4]int64) int {
	if u == v {
		return 0
	}
	need := max(selfPowerBits(u), selfPowerBits(v)) // exact if prec >= need
	for prec := uint(minSelfPowerPrec); ; prec *= 2 {
		mu, eu, wu := selfPowerProduct(u, prec)
		mv, ev, wv := selfPowerProduct(v, prec)
		exact := uint64(prec) >= need
		if c, ok := cmpScaled(mu, eu, mv, ev, wu+wv, prec, exact); ok {
			return c
		}
		if prec >= maxSelfPowerPrec {
			return 0
		}
	}
}

const (
	minSelfPowerPrec = 128
	maxSelfPowerPrec = 1 << 16
)

// selfPowerBits returns an upper bound of the bit length of
// prod u_i^u_i (and of every intermediate value of selfPowerProduct),
// saturated at MaxUint64.
func selfPowerBits(u [4]int64) uint64 {
	total := uint64(2)
	for _, t := range u {
		if t <= 1 {
			continue
		}
		hi, lo := bits.Mul64(uint64(t), uint64(bits.Len64(uint64(t))))
		var carry uint64
		total, carry = bits.Add64(total, lo, 0)
		if hi != 0 || carry != 0 {
			return math.MaxUint64
		}
	}
	return total
}

// selfPowerProduct computes prod u_i^u_i as m·2^e with 0.5 <= m < 1 at
// precision prec, by binary exponentiation with the exponent kept in an
// int64. It also returns w such that the relative error of the result is
// at most w·2^-prec: a rounding error in base^(2^j) is raised to at most
// the power u_i/2^j, so the errors of one power add up to less than
// 4·u_i roundings.
func selfPowerProduct(u [4]int64, prec uint) (m *big.Float, e int64, w uint64) {
	m = new(big.Float).SetPrec(prec).SetInt64(1)
	base := new(big.Float).SetPrec(prec)
	for _, t := range u {
		if t <= 1 { // 0^0 = 1^1 = 1
			continue
		}
		base.SetInt64(t) // exact: t has at most 63 bits
		be := int64(base.MantExp(base))
		for k := t; ; {
			if k&1 != 0 {
				m.Mul(m, base)
				e += be + int64(m.MantExp(m))
			}
			k >>= 1
			if k == 0 {
				break
			}
			base.Mul(base, base)
			be = 2*be + int64(base.MantExp(base))
		}
		w += 4*uint64(t) + 4
	}
	return m, e, w
}

// cmpScaled compares mu·2^eu with mv·2^ev (0.5 <= mu, mv < 1), whose
// relative errors together are at most w·2^-prec. It returns the sign and
// whether it is certain; with exact set the inputs carry no error.
func cmpScaled(mu *big.Float, eu int64, mv *big.Float, ev int64, w uint64, prec uint, exact bool) (int, bool) {
	d := eu - ev
	switch {
	case d >= 2:
		return 1, true
	case d <= -2:
		return -1, true
	}
	r := new(big.Float).SetMantExp(mu, int(d)) // exact scaling, 1/4 <= r < 2
	r.SetPrec(prec + 8)
	r.Sub(r, mv) // exact at prec+8 bits
	if exact {
		return r.Sign(), true
	}
	if r.Sign() == 0 {
		return 0, false
	}
	// |r| >= 2^(er-1); the error is at most w·2^-prec·(|r1| + |mv|) < 4·w·2^-prec.
	er := r.MantExp(nil)
	if er-1 > bits.Len64(w)+2-int(prec) {
		return r.Sign(), true
	}
	return 0, false
}
