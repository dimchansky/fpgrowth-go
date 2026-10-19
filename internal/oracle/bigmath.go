package oracle

import (
	"math"
	"math/big"
	"sync"
)

// High-precision helpers of the reference measures (reference.go). They
// use only math/big; nothing here is shared with the implementation.

// refPrec is the default working precision in bits of the big.Float
// computations (the mutual information uses more, see infoPrec). The
// reference values are rounded to float64 (53 bits) at the end; with 128
// bits the error of every computed logarithm is far below 2^-100
// relative, so the rounded value is the float64 nearest to the exact
// value or one of its two neighbours.
const refPrec = 128

// maxPrec is the largest supported working precision.
const maxPrec = 448

// newFloat returns a zero big.Float with precision prec.
func newFloat(prec uint) *big.Float { return new(big.Float).SetPrec(prec) }

// floatFromRat returns r rounded to precision prec.
func floatFromRat(r *big.Rat, prec uint) *big.Float { return newFloat(prec).SetRat(r) }

// ln2Max is ln(2) = 2·atanh(1/3) to maxPrec+64 bits.
var ln2Max = sync.OnceValue(func() *big.Float {
	const p = maxPrec + 64
	third := newFloat(p).Quo(newFloat(p).SetInt64(1), newFloat(p).SetInt64(3))
	return atanh2(third)
})

// ln2Big returns ln(2) rounded to precision prec <= maxPrec.
func ln2Big(prec uint) *big.Float { return newFloat(prec).Set(ln2Max()) }

// atanh2 returns 2·atanh(z) = ln((1+z)/(1-z)) for |z| <= 1/3 by its power
// series 2·(z + z^3/3 + z^5/5 + ...) at the precision of z. The terms
// decrease at least by the factor z^2 <= 1/9; the loop stops when a term
// no longer changes the sum at that precision.
func atanh2(z *big.Float) *big.Float {
	prec := z.Prec()
	z2 := newFloat(prec).Mul(z, z)
	pow := newFloat(prec).Set(z) // z^(2k+1)
	sum := newFloat(prec).Set(z)
	term := newFloat(prec)
	for k := int64(1); ; k++ {
		pow.Mul(pow, z2)
		term.Quo(pow, newFloat(prec).SetInt64(2*k+1))
		if term.Sign() == 0 || term.MantExp(nil) < sum.MantExp(nil)-int(prec)-8 {
			break
		}
		sum.Add(sum, term)
	}
	return sum.Mul(sum, newFloat(prec).SetInt64(2))
}

// lnBig returns the natural logarithm of x > 0 at the precision of x. With
// x = m·2^e and m in [1/√2, √2), ln x = e·ln 2 + 2·atanh((m-1)/(m+1)),
// where |(m-1)/(m+1)| < 0.172.
func lnBig(x *big.Float) *big.Float {
	if x.Sign() <= 0 {
		panic("oracle: lnBig of a non-positive value")
	}
	prec := x.Prec()
	m := newFloat(prec)
	e := x.MantExp(m) // x = m·2^e, 0.5 <= m < 1
	// m < 1/√2 <=> 2·m² < 1
	if sq := newFloat(prec).Mul(m, m); sq.Mul(sq, newFloat(prec).SetInt64(2)).Cmp(newFloat(prec).SetInt64(1)) < 0 {
		m.SetMantExp(m, 1)
		e--
	}
	one := newFloat(prec).SetInt64(1)
	z := newFloat(prec).Quo(newFloat(prec).Sub(m, one), newFloat(prec).Add(m, one))
	r := atanh2(z)
	if e != 0 {
		r.Add(r, newFloat(prec).Mul(newFloat(prec).SetInt64(int64(e)), ln2Big(prec)))
	}
	return r
}

// lnRat returns ln(r) for a rational r > 0 at precision prec.
func lnRat(r *big.Rat, prec uint) *big.Float { return lnBig(floatFromRat(r, prec)) }

// log1pMinus returns ln(1+eps) - eps for a rational eps > -1 at precision
// prec, with a small relative error also when eps is tiny (where
// ln(1+eps) and eps cancel). For |eps| <= 1/2 it uses u = eps/(2+eps),
// |u| <= 1/3, and
//
//	ln(1+eps) - eps = 2·atanh(u) - eps = -eps²/(2+eps) + 2·(u³/3 + u⁵/5 + ...),
//
// whose first term dominates and has no cancellation; otherwise
// |ln(1+eps) - eps| > 0.09 and the direct difference is accurate.
func log1pMinus(eps *big.Rat, prec uint) *big.Float {
	half := big.NewRat(1, 2)
	if new(big.Rat).Abs(eps).Cmp(half) > 0 {
		r := lnRat(new(big.Rat).Add(eps, big.NewRat(1, 1)), prec)
		return r.Sub(r, floatFromRat(eps, prec))
	}
	twoPlus := new(big.Rat).Add(eps, big.NewRat(2, 1))
	lead := new(big.Rat).Mul(eps, eps)
	lead.Quo(lead, twoPlus)
	lead.Neg(lead) // -eps²/(2+eps)
	u := floatFromRat(new(big.Rat).Quo(eps, twoPlus), prec)
	u2 := newFloat(prec).Mul(u, u)
	pow := newFloat(prec).Mul(u2, u) // u^(2k+1), k = 1
	tail := newFloat(prec)
	term := newFloat(prec)
	for k := int64(1); pow.Sign() != 0; k++ {
		term.Quo(pow, newFloat(prec).SetInt64(2*k+1))
		if tail.Sign() != 0 && term.MantExp(nil) < tail.MantExp(nil)-int(prec)-8 {
			break
		}
		tail.Add(tail, term)
		pow.Mul(pow, u2)
		if k > 4*int64(prec) { // cannot happen for |u| <= 1/3; a safety net
			break
		}
	}
	tail.Mul(tail, newFloat(prec).SetInt64(2))
	return tail.Add(tail, floatFromRat(lead, prec))
}

// float64Of returns x rounded to the nearest float64.
func float64Of(x *big.Float) float64 {
	f, _ := x.Float64()
	return f
}

// ratFloat64 returns r rounded to the nearest float64.
func ratFloat64(r *big.Rat) float64 {
	f, _ := r.Float64()
	return f
}

// q1 returns the upper tail probability P(X > x) of the chi² distribution
// with one degree of freedom, erfc(√(x/2)), for x >= 0. It uses Go's
// math.Erfc (a port of FreeBSD's s_erf.c, error below one ulp), which is
// independent of the implementation's incomplete gamma function. For an
// argument x with relative error δ the result has a relative error of
// about (1 + x)·δ plus one ulp: d ln erfc(y)/d ln y = -2y·exp(-y²)/(√π·
// erfc(y)), which is at most 2y² + 1 = x + 1 in magnitude.
func q1(x float64) float64 {
	if x <= 0 {
		return 1
	}
	return math.Erfc(math.Sqrt(x / 2))
}
