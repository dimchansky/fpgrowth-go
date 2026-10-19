package fpg

import (
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/thresh"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// minSupp converts fpg_data's minimum support argument: -smin if
// smin < 0 (rounded up), else the minimum of the fraction smin/100 of w
// under the threshold contract of package thresh.
//
// Deviation from upstream (differences.md N2, issue #17): fpg_data
// computes ceil((smin/100)*w*(1-DBL_EPSILON)) in double arithmetic, whose
// relative tolerance exceeds one unit of support beyond about 2^52 and
// whose rounding can cross an integer before that. Here the same formula
// is evaluated exactly and its tolerance is capped below one unit.
func minSupp(smin float64, w tract.Supp) tract.Supp {
	if smin < 0 {
		return suppOf(math.Ceil(-smin))
	}
	return tract.Supp(thresh.New(smin / 100.0).Min(int64(w)))
}

// maxSupp converts fpg_report's maximum support argument: -smax if
// smax < 0 (rounded down), else the maximum of the fraction smax/100 of
// w under the threshold contract (deviation N2, as minSupp).
func maxSupp(smax float64, w tract.Supp) tract.Supp {
	if smax < 0 {
		return suppOf(math.Floor(-smax))
	}
	return tract.Supp(thresh.New(smax / 100.0).Max(int64(w)))
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
	if c.HasCounts {
		return derive(c, c.MinCount, c.MaxCount)
	}
	return derive(c, minSupp(c.SMin, w), maxSupp(c.SMax, w))
}

// derive completes the thresholds from the absolute minimum and maximum
// support. For rules with the body support definition, the minimum set
// support is the minimum ConfFrac of the minimum body support (fpg_data's
// smin *= conf*(1-DBL_EPSILON), here under the exact threshold contract):
// a rule's support must meet the minimum ConfFrac of its body support,
// which is nondecreasing in the body support, so this is the smallest
// support any rule can have.
//
// Deviation from upstream (fpgrowth-core.md B2): fpg_data does not clamp,
// so -s0 gives a minimum support of 0 and the miners dereference empty
// node lists or report support-0 sets. The effective minimum support (and
// body support) is at least 1.
func derive(c *Config, minimum, maximum tract.Supp) thresholds {
	var t thresholds
	t.body = max(minimum, 1)
	t.supp = t.body
	if c.Target == report.TargetRules && c.Mode&ModeOrigSupp == 0 {
		t.supp = min(max(tract.Supp(thresh.New(c.ConfFrac).Min(int64(t.body))), 1), t.body)
	}
	t.smax = maximum
	return t
}
