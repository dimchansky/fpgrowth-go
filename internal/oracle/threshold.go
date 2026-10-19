package oracle

import (
	"math"
	"math/big"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Limits meaning "no limit" for SetParams and RuleParams.
const (
	NoMaxSupp tract.Supp = math.MaxInt64
	NoMaxSize int        = math.MaxInt
)

// The threshold contract (docs/differences.md N2, issue #17), evaluated
// here independently of the miner with math/big only: a threshold t >= 0
// (a float64 fraction, taken at its exact binary value) of a reference
// support n defines x = t·n exactly; with ε = 2^-52 a support s meets
//
//   - the minimum t iff s >= x·(1−ε) and s > x−1;
//   - the maximum t iff s <= x·(1+ε) and s < x+1.
//
// t <= 0 is met by every support as a minimum and by none above 0 as a
// maximum; t = +Inf by none as a minimum and by all as a maximum. The
// absolute conversions saturate at math.MaxInt64.

var (
	ratOneMinusEps = new(big.Rat).SetFrac(big.NewInt(1<<52-1), big.NewInt(1<<52))
	ratOnePlusEps  = new(big.Rat).SetFrac(big.NewInt(1<<52+1), big.NewInt(1<<52))
	ratOne         = big.NewRat(1, 1)
	bigMaxSupp     = big.NewInt(math.MaxInt64)
)

// product returns t·n as an exact rational (t > 0 finite, n >= 0).
func product(t float64, n tract.Supp) *big.Rat {
	return new(big.Rat).Mul(new(big.Rat).SetFloat64(t), new(big.Rat).SetInt64(int64(max(n, 0))))
}

// MeetsMin reports whether the support s meets the minimum threshold t of
// the reference support n under the contract above.
func MeetsMin(s tract.Supp, t float64, n tract.Supp) bool {
	switch {
	case !(t > 0):
		return true
	case math.IsInf(t, 1):
		return false
	}
	x := product(t, n)
	rs := new(big.Rat).SetInt64(int64(s))
	if rs.Cmp(new(big.Rat).Mul(x, ratOneMinusEps)) < 0 {
		return false
	}
	return rs.Cmp(new(big.Rat).Sub(x, ratOne)) > 0
}

// MeetsMax reports whether the support s meets the maximum threshold t of
// the reference support n under the contract above.
func MeetsMax(s tract.Supp, t float64, n tract.Supp) bool {
	switch {
	case !(t > 0):
		return s <= 0
	case math.IsInf(t, 1):
		return true
	}
	x := product(t, n)
	rs := new(big.Rat).SetInt64(int64(s))
	if rs.Cmp(new(big.Rat).Mul(x, ratOnePlusEps)) > 0 {
		return false
	}
	return rs.Cmp(new(big.Rat).Add(x, ratOne)) < 0
}

// FracMinSupp returns the smallest support that meets the minimum t of n:
// max(⌈x·(1−ε)⌉, ⌊x⌋), saturating; 0 for t <= 0, math.MaxInt64 for +Inf
// and NaN gives 0.
func FracMinSupp(t float64, n tract.Supp) tract.Supp {
	switch {
	case !(t > 0): // also NaN
		return 0
	case math.IsInf(t, 1):
		return math.MaxInt64
	}
	x := product(t, n)
	v := ratCeil(new(big.Rat).Mul(x, ratOneMinusEps))
	if f := ratFloor(x); f.Cmp(v) > 0 {
		v = f
	}
	return saturate(v)
}

// FracMaxSupp returns the largest support that meets the maximum t of n:
// min(⌊x·(1+ε)⌋, ⌈x⌉), saturating; 0 for t <= 0 and NaN, math.MaxInt64
// for +Inf.
func FracMaxSupp(t float64, n tract.Supp) tract.Supp {
	switch {
	case !(t > 0):
		return 0
	case math.IsInf(t, 1):
		return math.MaxInt64
	}
	x := product(t, n)
	v := ratFloor(new(big.Rat).Mul(x, ratOnePlusEps))
	if c := ratCeil(x); c.Cmp(v) < 0 {
		v = c
	}
	return saturate(v)
}

// AbsMinSupp converts an upstream minimum support argument (-s, and each
// value of -F) to an absolute support: a negative arg is an absolute
// count, ceil(-arg) (like fpg_data and setbdr); otherwise arg is a
// percentage of the total weight, the fraction arg/100 (a float64
// division, like upstream) under the threshold contract (FracMinSupp).
// The result is not raised to 1 (see EffectiveMinSupp); out-of-range
// values saturate and NaN gives 0.
func AbsMinSupp(arg float64, total tract.Supp) tract.Supp {
	if arg < 0 {
		return suppFromFloat(math.Ceil(-arg))
	}
	return FracMinSupp(arg/100, total)
}

// AbsMaxSupp converts an upstream maximum support argument (-S) to an
// absolute support: a negative arg is an absolute count, floor(-arg)
// (like fpg_report); otherwise the fraction arg/100 under the threshold
// contract (FracMaxSupp). Out-of-range values saturate (upstream:
// undefined behavior, report.md B18) and NaN gives 0.
func AbsMaxSupp(arg float64, total tract.Supp) tract.Supp {
	if arg < 0 {
		return suppFromFloat(math.Floor(-arg))
	}
	return FracMaxSupp(arg/100, total)
}

// RuleSetMinSupp returns the minimum support of body ∪ head under the
// default (body) support definition: the smallest support that meets the
// confidence conf (a fraction) for the minimum body support bodyMin. Since
// that minimum is nondecreasing in the body support, no rule with a body
// support of at least bodyMin has a smaller support (fpg_data derives
// ceil(bodyMin*(conf*(1-DBL_EPSILON))) in double arithmetic). Under
// OrigSupp the set minimum is bodyMin itself.
func RuleSetMinSupp(bodyMin tract.Supp, conf float64) tract.Supp {
	return FracMinSupp(conf, bodyMin)
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

// ratFloor returns ⌊r⌋.
func ratFloor(r *big.Rat) *big.Int {
	q, m := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if m.Sign() < 0 {
		q.Sub(q, big.NewInt(1))
	}
	return q
}

// ratCeil returns ⌈r⌉.
func ratCeil(r *big.Rat) *big.Int {
	q, m := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if m.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}

// saturate converts v to tract.Supp, clamped to [0, math.MaxInt64].
func saturate(v *big.Int) tract.Supp {
	switch {
	case v.Sign() < 0:
		return 0
	case v.Cmp(bigMaxSupp) > 0:
		return math.MaxInt64
	}
	return tract.Supp(v.Int64())
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
