package ruleval

import (
	"math"
	"math/bits"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Exact integer arithmetic for the contingency table measures.
//
// The statistical measures depend on D = supp·base - head·body, the
// deviation of the table from independence (n times the difference of
// the observed and the expected rule support). For valid arguments every
// support is in [0, 2^63), so both products are below 2^126 and D, 2|D|
// and 2|D| - base fit in 128 unsigned bits. Computing D in float64, as
// upstream does, rounds the products once they exceed 2^53 and loses all
// precision of D when it is small relative to them (audit ALG-1).

// crossDiff returns D = supp·base - head·body exactly, as its sign (neg
// is true for D < 0) and its magnitude hi·2^64 + lo. The arguments are
// treated as unsigned; for valid arguments they are non-negative.
func crossDiff(supp, body, head, base tract.Supp) (neg bool, hi, lo uint64) {
	h1, l1 := bits.Mul64(uint64(supp), uint64(base))
	h2, l2 := bits.Mul64(uint64(head), uint64(body))
	if h1 < h2 || h1 == h2 && l1 < l2 {
		h1, l1, h2, l2 = h2, l2, h1, l1
		neg = true
	}
	var b uint64
	lo, b = bits.Sub64(l1, l2, 0)
	hi, _ = bits.Sub64(h1, h2, b)
	return neg, hi, lo
}

// u128Float returns hi·2^64 + lo correctly rounded to float64 (round to
// nearest, ties to even). It makes no calls, so that the measures using it
// stay leaf functions.
func u128Float(hi, lo uint64) float64 {
	if hi == 0 {
		return float64(lo) // correctly rounded by the conversion
	}
	// Shift right by s so that the value fits in 64 bits, with the bits
	// shifted out folded into a sticky bit 0. The top bit of m is then
	// bit 63, so the 11 bits below the 53 significant ones include the
	// rounding bit (bit 10) above the sticky bit: rounding m to float64
	// rounds the full value correctly, and the scaling by the power of
	// two 2^s (1 <= s <= 64) is exact.
	s := uint(bits.Len64(hi))
	m := hi<<(64-s) | lo>>s
	if lo<<(64-s) != 0 {
		m |= 1
	}
	return float64(float64(m) * math.Float64frombits(uint64(1023+s)<<52))
}

// crossDiffFloat returns D = supp·base - head·body (see crossDiff)
// correctly rounded to float64. If all four arguments are below 2^26 the
// products are below 2^52, so upstream's double expression is exact and
// is used as it is (as fast as upstream's, and trivially bit-identical);
// otherwise D is computed in integers (crossDiffWide).
func crossDiffFloat(supp, body, head, base tract.Supp) float64 {
	if (supp|body|head|base)>>26 == 0 {
		return float64(float64(supp)*float64(base)) - float64(float64(head)*float64(body))
	}
	return crossDiffWide(supp, body, head, base)
}

// crossDiffWide is crossDiffFloat for large arguments: in int64 if all
// four are below 2^31 (the products are then below 2^62), else in 128-bit
// integers. It is kept separate so that crossDiffFloat can be inlined.
func crossDiffWide(supp, body, head, base tract.Supp) float64 {
	if (supp|body|head|base)>>31 == 0 {
		return float64(supp*base - head*body)
	}
	neg, hi, lo := crossDiff(supp, body, head, base)
	d := u128Float(hi, lo)
	if neg {
		d = -d
	}
	return d
}

// AboveExpected reports whether the rule support exceeds the support
// expected if body and head were independent: supp·base > head·body,
// decided exactly (upstream -i, "invalidate evaluation below expected
// support"). Upstream evaluates the products in double precision, which
// rounds them beyond 2^53; the result is the same whenever both products
// are at most 2^53.
func AboveExpected(supp, body, head, base tract.Supp) bool {
	neg, hi, lo := crossDiff(supp, body, head, base)
	return !neg && hi|lo != 0
}
