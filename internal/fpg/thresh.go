package fpg

import (
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// dblEpsilon is C's DBL_EPSILON (2^-52).
const dblEpsilon = 0x1p-52

// minSuppValue computes fpg_data's absolute minimum support before
// rounding: -smin if smin < 0, else (smin/100)*w*(1-DBL_EPSILON), in
// upstream's evaluation order (left to right; no additions, so no fused
// multiply-add can change the result).
func minSuppValue(smin float64, w tract.Supp) float64 {
	if smin < 0 {
		return -smin
	}
	return smin / 100.0 * float64(w) * (1 - dblEpsilon)
}

// maxSuppValue computes fpg_report's absolute maximum support before
// flooring: -smax if smax < 0, else (smax/100)*w*(1+DBL_EPSILON).
func maxSuppValue(smax float64, w tract.Supp) float64 {
	if smax < 0 {
		return -smax
	}
	return smax / 100.0 * float64(w) * (1 + dblEpsilon)
}

// suppOf converts an integral floating-point support to tract.Supp.
//
// Deviation from upstream (fpgrowth-core.md B9): the C casts (SUPP)x are
// undefined behavior for values outside the int range (arm64 saturates,
// x86-64 yields INT_MIN). Here the conversion saturates explicitly at 0
// and math.MaxInt64; NaN gives 0.
func suppOf(x float64) tract.Supp {
	switch {
	case !(x > 0): // also NaN
		return 0
	case x >= 0x1p63:
		return math.MaxInt64
	default:
		return tract.Supp(x)
	}
}

// thresholds are the absolute supports derived by fpg_data and
// fpg_report.
type thresholds struct {
	supp tract.Supp // minimum support of an item set (fpg->supp)
	body tract.Supp // minimum support of a rule body (fpg->body)
	smax tract.Supp // maximum support passed to the reporter
}

// computeThresholds ports the threshold arithmetic of fpg_data and
// fpg_report for the total transaction weight w.
func computeThresholds(c *Config, w tract.Supp) thresholds {
	smin := math.Ceil(minSuppValue(c.SMin, w))
	var t thresholds
	t.body = suppOf(smin)
	if c.Target == report.TargetRules && c.Mode&ModeOrigSupp == 0 {
		smin *= c.Conf / 100.0 * (1 - dblEpsilon)
	}
	t.supp = suppOf(math.Ceil(smin))
	// Deviation from upstream (fpgrowth-core.md B2): fpg_data does not
	// clamp, so -s0 gives a minimum support of 0 and the miners
	// dereference empty node lists or report support-0 sets. The
	// effective minimum support (and body support) is at least 1.
	t.supp = max(t.supp, 1)
	t.body = max(t.body, 1)
	t.smax = suppOf(math.Floor(maxSuppValue(c.SMax, w)))
	return t
}
