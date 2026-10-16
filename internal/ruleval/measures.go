package ruleval

import (
	"math"

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
func reCert(supp, body, head, base tract.Supp) float64 {
	if body <= 0 || base <= 0 {
		return 0
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

// reChi2 ports re_chi2: the chi^2 measure normalized by base,
// (head*body - supp*base)^2 / (head*(base-head)*body*(base-body)).
func reChi2(supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 0
	}
	t := float64(float64(head)*float64(body)) - float64(float64(supp)*float64(base))
	return (t * t) / (float64(head) * float64(base-head) * float64(body) * float64(base-body))
}

// reChi2PVal ports re_chi2pval: the p-value of the (unnormalized) chi^2
// statistic base*reChi2 with one degree of freedom.
func reChi2PVal(supp, body, head, base tract.Supp) float64 {
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
func reYates(supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 0
	}
	t := math.Abs(float64(float64(head)*float64(body))-float64(float64(supp)*float64(base))) -
		float64(0.5*float64(base))
	if t < 0 {
		t = 0
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
// Deviation from upstream: the result is clamped at 0 (bug B7). Mutual
// information is never negative, but upstream's sum cancels to values like
// -3.2e-16 for independent tables. Such values abort upstream's debug build
// in InfoPValue (GammaQ asserts x >= 0); the release build returns 1 there,
// which is also what the clamped value gives.
func reInfo(supp, body, head, base tract.Supp) float64 {
	return max(infoRaw(supp, body, head, base), 0)
}

// infoRaw is re_info without the clamp at 0.
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

// reInfoPVal ports re_infopval: the p-value of the G statistic
// 2*ln(2)*base*reInfo with one degree of freedom.
func reInfoPVal(supp, body, head, base tract.Supp) float64 {
	return chi2cdfQ(2*ln2*float64(base)*reInfo(supp, body, head, base), 1)
}
