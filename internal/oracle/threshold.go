package oracle

import (
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// dblEpsilon is C's DBL_EPSILON. The factors 1-dblEpsilon and 1+dblEpsilon
// are exactly representable, so the untyped constant expressions below
// have the same float64 values as upstream's double expressions.
const dblEpsilon = 0x1p-52

// Limits meaning "no limit" for SetParams and RuleParams.
const (
	NoMaxSupp tract.Supp = math.MaxInt64
	NoMaxSize int        = math.MaxInt
)

// AbsMinSupp converts an upstream minimum support argument (-s, and each
// value of -F) to an absolute support, exactly like fpg_data and setbdr:
// a negative arg is an absolute count, ceil(-arg); otherwise arg is a
// percentage of the total weight, ceil(((arg/100)*total)*(1-DBL_EPSILON)).
// The result is not raised to 1 (see EffectiveMinSupp); out-of-range
// values saturate and NaN gives 0.
func AbsMinSupp(arg float64, total tract.Supp) tract.Supp {
	if arg < 0 {
		return suppFromFloat(math.Ceil(-arg))
	}
	return suppFromFloat(math.Ceil(arg / 100 * float64(total) * (1 - dblEpsilon)))
}

// AbsMaxSupp converts an upstream maximum support argument (-S) to an
// absolute support, exactly like fpg_report: a negative arg is an absolute
// count, floor(-arg); otherwise floor(((arg/100)*total)*(1+DBL_EPSILON)).
// Out-of-range values saturate (upstream: undefined behavior, report.md
// B18) and NaN gives 0.
func AbsMaxSupp(arg float64, total tract.Supp) tract.Supp {
	if arg < 0 {
		return suppFromFloat(math.Floor(-arg))
	}
	return suppFromFloat(math.Floor(arg / 100 * float64(total) * (1 + dblEpsilon)))
}

// RuleSetMinSupp returns the minimum support of body ∪ head that upstream
// derives for rules under the default (body) support definition:
// ceil(bodyMin * (conf*(1-DBL_EPSILON))) (fpg_data, with conf = -c/100).
// Under OrigSupp the set minimum is bodyMin itself.
func RuleSetMinSupp(bodyMin tract.Supp, conf float64) tract.Supp {
	return suppFromFloat(math.Ceil(float64(bodyMin) * (conf * (1 - dblEpsilon))))
}

// EffectiveMinSupp returns max(s, 1): the effective absolute minimum
// support used for mining (the port's integer policy; upstream's item set
// tree uses the same floor).
func EffectiveMinSupp(s tract.Supp) tract.Supp {
	return max(s, 1)
}

// AbsBorder converts upstream -F values to a support border indexed by
// item set size, as SetParams.Border expects: value i applies to size
// zmin+i and is converted with AbsMinSupp; sizes below zmin get 0 (no
// restriction). A nil or empty args gives nil (no border).
func AbsBorder(args []float64, zmin int, total tract.Supp) []tract.Supp {
	if len(args) == 0 {
		return nil
	}
	border := make([]tract.Supp, zmin+len(args))
	for i, arg := range args {
		border[zmin+i] = AbsMinSupp(arg, total)
	}
	return border
}

// suppFromFloat converts an integral float64 to tract.Supp, saturating at
// the type's limits; NaN converts to 0.
func suppFromFloat(x float64) tract.Supp {
	switch {
	case math.IsNaN(x):
		return 0
	case x >= 0x1p63:
		return math.MaxInt64
	case x < -0x1p63:
		return math.MinInt64
	default:
		return tract.Supp(x)
	}
}
