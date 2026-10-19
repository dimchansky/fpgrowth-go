package ruleval

import (
	"math"
	"math/bits"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// ln2 is upstream's LN_2.
const ln2 = 0.69314718055994530942

// marginsDegenerate reports whether a margin of the contingency table
// vanishes (head or body is 0 or base). The chi^2, information and Fisher
// measures return a constant for such tables.
func marginsDegenerate(body, head, base tract.Supp) bool {
	return head <= 0 || head >= base || body <= 0 || body >= base
}

// reNone ports re_none: no measure, constant 0.
func reNone(_, _, _, _ tract.Supp) float64 { return 0 }

// reSupp ports re_supp: the rule support (body and head).
func reSupp(supp, _, _, _ tract.Supp) float64 { return float64(supp) }

// reConf ports re_conf: the rule confidence supp/body.
func reConf(supp, body, _, _ tract.Supp) float64 {
	if body > 0 {
		return float64(supp) / float64(body)
	}
	return 0
}

// reConfDiff ports re_confdiff: |supp/body - head/base|.
func reConfDiff(supp, body, head, base tract.Supp) float64 {
	if body <= 0 || base <= 0 {
		return 0
	}
	return math.Abs(float64(supp)/float64(body) - float64(head)/float64(base))
}

// reLift ports re_lift: (supp*base)/(body*head).
func reLift(supp, body, head, base tract.Supp) float64 {
	if body <= 0 || head <= 0 {
		return 0
	}
	return (float64(supp) * float64(base)) / (float64(body) * float64(head))
}

// reLiftDiff ports re_liftdiff: |lift - 1|.
func reLiftDiff(supp, body, head, base tract.Supp) float64 {
	if body <= 0 || head <= 0 {
		return 0
	}
	return math.Abs((float64(supp)*float64(base))/(float64(body)*float64(head)) - 1)
}

// reLiftQuot ports re_liftquot: 1 - min(lift, 1/lift).
func reLiftQuot(supp, body, head, base tract.Supp) float64 {
	if body <= 0 || head <= 0 {
		return 0
	}
	t := (float64(supp) * float64(base)) / (float64(body) * float64(head))
	if t > 1 {
		t = 1 / t
	}
	return 1 - t
}

// reCvct ports re_cvct: the conviction (body*(base-head))/((body-supp)*base).
//
// Deviation from upstream: upstream returns 0 when body <= supp, i.e. for
// every rule with confidence 1 (bug B4). The conviction of such a rule is
// +Inf (the limit of (1-P(h))/(1-conf) as conf -> 1, and the value Brin et
// al. assign to 100% rules); ConvictionDiff and ConvictionQuotient already
// return +Inf in this case. +Inf meets every (+) threshold, both in
// upstream's rule filter (reject if dir*v < thresh) and in its item set
// filter (accept if v*dir >= thresh), and makes an average aggregate +Inf.
// An empty body support (body = 0, confidence undefined) still gives 0,
// like Confidence and Lift.
func reCvct(supp, body, head, base tract.Supp) float64 {
	if base <= 0 || body <= 0 {
		return 0
	}
	if body <= supp {
		return math.Inf(1)
	}
	return (float64(body) * float64(base-head)) / (float64(body-supp) * float64(base))
}

// reCvctDiff ports re_cvctdiff: |conviction - 1|, +Inf for confidence 1.
func reCvctDiff(supp, body, head, base tract.Supp) float64 {
	if base <= 0 {
		return 0
	}
	if body <= supp {
		return math.Inf(1)
	}
	return math.Abs((float64(body)*float64(base-head))/(float64(body-supp)*float64(base)) - 1)
}

// reCvctQuot ports re_cvctquot: 1 - min(conviction, 1/conviction), +Inf for
// confidence 1 (as upstream, although the measure is otherwise in [0, 1]).
func reCvctQuot(supp, body, head, base tract.Supp) float64 {
	if base <= 0 {
		return 0
	}
	if body <= supp {
		return math.Inf(1)
	}
	t := (float64(body) * float64(base-head)) / (float64(body-supp) * float64(base))
	if t > 1 {
		t = 1 / t
	}
	return 1 - t
}

// reImbal ports re_imbal: the imbalance ratio |head-body|/(body+head-supp).
// The integer sum is evaluated as body+(head-supp), which equals upstream's
// body+head-supp but cannot overflow for valid arguments.
func reImbal(supp, body, head, _ tract.Supp) float64 {
	d := float64(body + (head - supp)) // support of the disjunction
	if d <= 0 {
		return math.Inf(1)
	}
	diff := head - body
	if diff < 0 {
		diff = -diff
	}
	return float64(diff) / d
}

// reKulc ports re_kulc: the Kulczynski measure supp/2*(1/body + 1/head).
func reKulc(supp, body, head, _ tract.Supp) float64 {
	if body <= 0 || head <= 0 {
		return math.Inf(1)
	}
	return 0.5 * float64(supp) * (1.0/float64(body) + 1.0/float64(head))
}

// reKulcDiff ports re_kulcdiff: |supp*(1/body + 1/head) - 1|, which is
// twice the difference of the Kulczynski measure to 0.5 (note N3).
func reKulcDiff(supp, body, head, _ tract.Supp) float64 {
	if body <= 0 || head <= 0 {
		return math.Inf(1)
	}
	return math.Abs(float64(float64(supp)*(1.0/float64(body)+1.0/float64(head))) - 1)
}

// reCProb ports re_cprob: the conditional probability ratio
// P(h|B)/P(h|not B) = (supp*(base-body))/(body*(head-supp)).
func reCProb(supp, body, head, base tract.Supp) float64 {
	if supp <= 0 || body <= 0 || base <= body {
		return 0
	}
	if head <= supp {
		return math.Inf(1)
	}
	return (float64(supp) * float64(base-body)) / (float64(body) * float64(head-supp))
}

// reImport ports re_import: the importance, log2 of the conditional
// probability ratio (computed as log(r)/LN_2 like upstream).
func reImport(supp, body, head, base tract.Supp) float64 {
	if supp <= 0 || body <= 0 || base <= body {
		return 0
	}
	if head <= supp {
		return math.Inf(1)
	}
	r := (float64(supp) * float64(base-body)) / (float64(body) * float64(head-supp))
	if r > 0 {
		return math.Log(r) / ln2
	}
	return 0
}

// reCert ports re_cert: the certainty factor, the change of the confidence
// relative to the prior p = head/base: (conf-p)/(1-p) if conf >= p, else
// (conf-p)/p.
//
// Deviation from upstream: if the head occurs in every transaction
// (head = base, p = 1), every rule has confidence 1 and upstream computes
// 0/(1-1) = NaN (bug B5), which its rule filter lets through for any
// threshold. The confidence does not change at all in this case, so the
// certainty factor is 0.
//
// Deviation from upstream (ALG-1): upstream's formula divides the rounded
// difference conf-p by 1-p, which amplifies its absolute rounding error
// of a few ulps by base/(base-head), and the rounding can also flip the
// sign of conf-p near independence (a table with conf < p, or an exactly
// independent one, then gets a value up to 1). Once base-head is below
// about base·2^-53, p rounds to 1 and a rule with confidence 1 gets 0
// instead of 1. For base/(base-head) > certGate the value is therefore
// computed from the exact D = supp·base - head·body, whose sign decides
// the branch: D/(body·(base-head)) for D > 0, D/(body·head) for D < 0
// (= (conf-p)/p) and exactly 0 for D = 0, each with a relative error of a
// few ulps. Below the gate upstream's formula is kept (bit-identical): a
// wrong sign of the rounded conf-p, whose absolute error is at most 3u
// (u = 2^-53; both quotients are at most 1), costs at most
// 3u·base/(base-head) <= 3u·certGate on the conf >= p branch, and at most
// 3u·(conf+p)/p <= 6u on the other, so the absolute error stays below
// 4·2^-53·certGate + 6·2^-53 < 2^-40.
func reCert(supp, body, head, base tract.Supp) float64 {
	if body <= 0 || base <= 0 {
		return 0
	}
	if head < base && base-head < base/certGate {
		neg, hi, lo := crossDiff(supp, body, head, base)
		d := u128Float(hi, lo)
		switch {
		case hi|lo == 0:
			return 0 // independent: conf = p
		case neg:
			// |D| <= body·head exactly (supp >= 0): at least -1
			return max(-d/(float64(body)*float64(head)), -1)
		}
		// D <= body·(base-head) exactly, so the quotient is at most 1 up
		// to rounding; clamp it like the exact value.
		return min(d/(float64(body)*float64(base-head)), 1)
	}
	p := float64(head) / float64(base)
	n := float64(supp)/float64(body) - p
	if n < 0 {
		return n / p
	}
	if p >= 1 {
		return 0
	}
	return n / (1 - p)
}

// certGate is the largest base/(base-head) for which reCert keeps
// upstream's formula (see there).
const certGate = 1 << 10

// reChi2 ports re_chi2: the chi^2 measure normalized by base,
// (head*body - supp*base)^2 / (head*(base-head)*body*(base-body)).
//
// Deviation from upstream (ALG-1): the difference head*body - supp*base
// is computed exactly in 128-bit integer arithmetic (crossDiff) and then
// rounded once, so the relative error of the result is a few ulps for
// every valid table. Upstream rounds both products to double first,
// which cancels catastrophically when they exceed 2^53 (a perfect
// association with weights near 2^63 gets 0). When both products are at
// most 2^53 they are exact in double and the result is bit-identical to
// upstream's.
func reChi2(supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 0
	}
	// upstream's expression, exact for products up to 2^52; beyond, |D|
	// in 128 bits (crossDiff and u128Float are inlined, so reChi2 stays a
	// leaf function)
	t := float64(float64(head)*float64(body)) - float64(float64(supp)*float64(base))
	if (supp|body|head|base)>>26 != 0 {
		_, hi, lo := crossDiff(supp, body, head, base)
		t = u128Float(hi, lo)
	}
	return (t * t) / (float64(head) * float64(base-head) * float64(body) * float64(base-body))
}

// reChi2PVal ports re_chi2pval: the p-value of the (unnormalized) chi^2
// statistic base*reChi2 with one degree of freedom.
func reChi2PVal(supp, body, head, base tract.Supp) float64 {
	// The common case of reChi2 is repeated here because reChi2 is too
	// large to be inlined; the expression is the same.
	if (supp|body|head|base)>>26 == 0 && !marginsDegenerate(body, head, base) {
		t := float64(float64(head)*float64(body)) - float64(float64(supp)*float64(base))
		return chi2cdfQ(float64(base)*((t*t)/(float64(head)*float64(base-head)*float64(body)*float64(base-body))), 1)
	}
	return chi2cdfQ(float64(base)*reChi2(supp, body, head, base), 1)
}

// reYates ports re_yates: the normalized chi^2 measure with Yates'
// continuity correction, (|head*body - supp*base| - base/2)^2 / (...).
//
// Deviation from upstream: the corrected difference is clamped at 0 (bug
// B10). Upstream squares a negative difference when the table is closer to
// independence than the correction, so an independent table (chi^2 = 0)
// gets a positive value. With the clamp the measure is the usual
// max(0, |ad-bc| - n/2)^2 form (as in R's chisq.test), never exceeds the
// uncorrected Chi2 and is 0 for independent tables.
//
// Deviation from upstream (ALG-1): the corrected difference is computed
// exactly as (2|D| - base)/2 in 128-bit integers (see reChi2), so the
// clamp is decided exactly. The result is bit-identical to upstream's
// whenever both products are at most 2^53: then |D| and base/2 are exact
// in double and upstream's subtraction rounds the same exact value.
func reYates(supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 0
	}
	var t float64
	if (supp|body|head|base)>>31 == 0 {
		// the products and 2|D| are below 2^63
		d := supp*base - head*body
		if d < 0 {
			d = -d
		}
		if d2 := 2*d - base; d2 > 0 {
			t = 0.5 * float64(d2)
		} else {
			return 0 // 2|D| <= base: clamped to 0
		}
	} else {
		_, hi, lo := crossDiff(supp, body, head, base)
		hi, lo = hi<<1|lo>>63, lo<<1 // 2|D| < 2^127
		var b uint64
		lo, b = bits.Sub64(lo, uint64(base), 0)
		hi, b = bits.Sub64(hi, 0, b)
		if b != 0 || hi|lo == 0 { // 2|D| <= base: clamped to 0
			return 0
		}
		t = 0.5 * u128Float(hi, lo)
	}
	return (t * t) / (float64(head) * float64(base-head) * float64(body) * float64(base-body))
}

// reYatesPVal ports re_yatespval: the p-value of the Yates-corrected chi^2
// statistic base*reYates with one degree of freedom.
func reYatesPVal(supp, body, head, base tract.Supp) float64 {
	return chi2cdfQ(float64(base)*reYates(supp, body, head, base), 1)
}

// reInfo ports re_info: the information difference to the prior (the
// mutual information of body and head) in bits.
//
// Deviation from upstream: the result is never negative (bug B7).
// Upstream's sum cancels to values like -3.2e-16 for independent tables;
// such values abort upstream's debug build in InfoPValue (GammaQ asserts
// x >= 0).
//
// Deviation from upstream (ALG-1, ALG-6): upstream computes
// (ln n + Σ t·ln(t/(r·k))/n)/ln 2, whose terms of size ln n cancel to
// the result. Its absolute error of a few ulps of ln n makes the relative
// error unbounded near independence (where the G-test p-value amplifies
// it) and destroys the value for totals beyond about 10^13. Here the
// information is computed from infoNats, a sum of non-negative terms with
// a relative error below 32 ulps for every valid table, and it is exactly
// 0 for independent tables. The values differ from upstream's in the
// last bits on ordinary data.
func reInfo(supp, body, head, base tract.Supp) float64 {
	return infoNats(supp, body, head, base) / float64(base) / ln2
}

// reInfoPVal ports re_infopval: the p-value of the G statistic
// 2*ln(2)*base*reInfo with one degree of freedom. The statistic is
// computed as 2·infoNats (see reInfo), which is exactly 0, and the
// p-value exactly 1, for independent tables.
func reInfoPVal(supp, body, head, base tract.Supp) float64 {
	return chi2cdfQ(2*infoNats(supp, body, head, base), 1)
}

// infoRaw is upstream's re_info without the clamp at 0, evaluated in
// upstream's order. Only the Fisher tests for small totals use it
// (fetInfo, fisherSmall), where its values merely order the tables and
// ties are decided exactly.
func infoRaw(supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 0
	}
	var sum float64
	t := float64(supp) // support of head and body
	if t > 0 {
		sum += float64(t * math.Log(t/(float64(head)*float64(body))))
	}
	t = float64(body - supp) // support of not head and body
	if t > 0 {
		sum += float64(t * math.Log(t/(float64(base-head)*float64(body))))
	}
	t = float64(head - supp) // support of head and not body
	if t > 0 {
		sum += float64(t * math.Log(t/(float64(head)*float64(base-body))))
	}
	t = float64(base - head - body + supp) // support of not head and not body
	if t > 0 {
		sum += float64(t * math.Log(t/(float64(base-head)*float64(base-body))))
	}
	return (math.Log(float64(base)) + sum/float64(base)) / ln2
}

// infoNats returns S = n·I, the mutual information I of body and head in
// nats times the total weight n = base (half the G statistic), for a
// valid table; 0 for degenerate margins.
//
// With the cells c, their row and column margins r and k and the expected
// counts E = r·k/n,
//
//	S = Σ c·ln(c/E) = Σ E·g(δ),  g(δ) = (1+δ)·ln(1+δ) - δ >= 0,
//
// where 1+δ = c/E; the second form adds Σ (E - c) = 0. Every term is
// non-negative, so the sum does not cancel, and δ = ±D/(r·k) (signs +,
// -, -, + for the cells supp, body-supp, head-supp and the rest) comes
// from the exact D (crossDiff) with a relative error of at most 5u
// (u = 2^-53: the rounding of D, of r·k and of the quotient). infoCell
// evaluates E·g(δ) as r·k·g(δ) (the sum is divided by n once).
//
// Error bound (first order, in u): δ carries 5u, which g amplifies by
// |δ·g'(δ)/g(δ)| <= 2.1 (10.5u) in the series range and the direct range
// alike. The series form of g adds at most 9u, the direct form at most
// 4u·|(1+δ)·ln(1+δ)|/g(δ) + u <= 40u (the cancellation factor is largest,
// 9.7, at |δ| = 1/4) and the form for δ < -1/2 at most 22u (no
// cancellation: g >= 0.15 there). The product r·k·g adds 4u, the sum of
// four non-negative terms 3u and the final division 1.5u, so S has a
// relative error below 59u (29.5 ulps); InfoPValue's statistic 2·S has
// the same, and Info adds 1.5u for the division by n·ln 2.
func infoNats(supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 0
	}
	d := float64(float64(supp)*float64(base)) - float64(float64(head)*float64(body))
	if (supp|body|head|base)>>26 != 0 { // products beyond 2^52
		d = crossDiffWide(supp, body, head, base)
	}
	if d == 0 {
		return 0 // independent: every δ is 0
	}
	n := float64(base)
	b, h, nb, nh := float64(body), float64(head), float64(base-body), float64(base-head)
	// The four cells, unrolled (a loop over arrays costs about 10 %).
	sum := infoCell(supp, float64(b*h), d, n) +
		infoCell(body-supp, float64(b*nh), -d, n) +
		infoCell(head-supp, float64(nb*h), -d, n) +
		infoCell(base-head-body+supp, float64(nb*nh), d, n)
	return sum / n
}

// infoCell returns r·k·g(δ) for a cell with count c, margin product
// rk = r·k and deviation d = ±D, δ = d/rk (see infoNats). The result is
// rounded explicitly so that the caller's sum cannot be fused with it.
func infoCell(c tract.Supp, rk, d, n float64) float64 {
	if c == 0 {
		return rk // g(-1) = 1
	}
	inv := 1 / rk
	delta := float64(d * inv)
	var g float64
	switch {
	case delta < -0.5:
		// x = 1+δ = c·n/(r·k) in (0, 1/2]: x·ln x + (1-x), where x·ln x
		// is in [-1/e, 0] and 1-x >= 1/2: nothing cancels.
		x := float64(float64(float64(c)*n) * inv)
		g = float64(x*math.Log(x)) + (1 - x)
	case delta > 0.25 || delta < -0.25:
		g = float64(float64(1+delta)*math.Log1p(delta)) - delta
	default:
		g = gSeries(delta)
	}
	return float64(rk * g)
}

// gSeries returns g(δ) = (1+δ)·ln(1+δ) - δ for |δ| <= 1/4 without
// cancellation. With v = δ/(2+δ), ln(1+δ) = 2·atanh(v) gives
//
//	g(δ) = δ²·(B(v²) + v·C(v²))/(2+δ),
//	B(w) = Σ_{j>=0} w^j/(2j+1),  C(w) = Σ_{j>=0} w^j/(2j+3).
//
// For |δ| <= 1/4, v is in [-1/7, 1/9], so w = v² <= 1/49 and the series
// truncated after w^8 have a relative error below 2^-55; B + v·C is at
// least 0.95, so the sum does not cancel. Every product is rounded
// explicitly (float64) to keep the result independent of FMA
// contraction.
func gSeries(delta float64) float64 {
	t := 1 / (2 + delta)
	v := float64(delta * t)
	w := float64(v * v)
	// Estrin's scheme (shorter dependency chains than Horner's); all
	// terms are positive, so the rounding errors stay at a few u.
	w2 := float64(w * w)
	w4 := float64(w2 * w2)
	w8 := float64(w4 * w4)
	b := (1 + float64(1.0/3*w)) + float64(w2*(1.0/5+float64(1.0/7*w))) +
		float64(w4*((1.0/9+float64(1.0/11*w))+float64(w2*(1.0/13+float64(1.0/15*w))))) +
		float64(w8*(1.0/17))
	c := (1.0/3 + float64(1.0/5*w)) + float64(w2*(1.0/7+float64(1.0/9*w))) +
		float64(w4*((1.0/11+float64(1.0/13*w))+float64(w2*(1.0/15+float64(1.0/17*w))))) +
		float64(w8*(1.0/19))
	p := b + float64(v*c)
	return float64(float64(float64(delta*delta)*p) * t)
}
