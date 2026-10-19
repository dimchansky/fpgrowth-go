// Package thresh converts relative thresholds (fractions of a reference
// support) to exact absolute supports. It implements the threshold
// contract of docs/differences.md (N2, issue #17) for minimum and maximum
// supports, the per-size border and the rule confidence.
//
// # Contract
//
// A threshold is a float64 t >= 0, taken at its exact binary value, and N
// is a reference support (the total transaction weight for supports, the
// body support for confidence). Let x = t·N be the exact rational
// product and ε = 2^-52 (C's DBL_EPSILON). An integer support s
//
//   - meets the minimum t iff s >= x·(1−ε) and s > x−1, so the minimum
//     support is max(⌈x·(1−ε)⌉, ⌊x⌋);
//   - meets the maximum t iff s <= x·(1+ε) and s < x+1, so the maximum
//     support is min(⌊x·(1+ε)⌋, ⌈x⌉).
//
// This is upstream's relative tolerance (fpg_data, fpg_report, istree's
// confidence test), evaluated exactly instead of in rounded double
// arithmetic, and capped below one unit of support. For x < 2^52 the cap
// never applies (⌊x⌋ > ⌈x·(1−ε)⌉ needs x·ε >= 1), so the result is
// exactly ⌈x·(1−ε)⌉ (or ⌊x·(1+ε)⌋). For an integer x the result is x
// itself, at every N: t = 1 means exactly N.
//
// Results saturate at math.MaxInt64. t <= 0 (also −0) gives 0, +Inf gives
// math.MaxInt64; NaN panics (callers validate it).
package thresh

import (
	"math"
	"math/bits"
)

// kind classifies a Frac.
type kind uint8

const (
	kindZero kind = iota // t <= 0
	kindFinite
	kindInf // t = +Inf
)

// Frac is a relative threshold t >= 0 in an exact form: t = m·2^-k with
// an odd m < 2^53 (k may be negative). The zero value is t = 0.
type Frac struct {
	kind kind
	m    uint64
	k    int
	// lo and hi are t·(1∓2^-50), rounded, for the fast path of Meets
	// where it applies (2^-60 <= t <= 1, where t·n does not exceed n, so
	// Min does not saturate); otherwise −Inf and +Inf, which make both
	// fast comparisons false (also for n = 0: 0·Inf is NaN). For t = 0
	// both are 0 (every s >= 0 meets it).
	lo, hi float64
}

// New returns the threshold t. It panics if t is NaN.
func New(t float64) Frac {
	switch {
	case math.IsNaN(t):
		panic("thresh: NaN threshold")
	case !(t > 0): // also −0 and negative values
		return Frac{}
	case math.IsInf(t, 1):
		return Frac{kind: kindInf, lo: math.Inf(-1), hi: math.Inf(1)}
	}
	fr, e := math.Frexp(t) // t = fr·2^e, fr in [0.5, 1), also for subnormals
	m := uint64(fr * (1 << 53))
	k := 53 - e
	tz := bits.TrailingZeros64(m)
	f := Frac{kind: kindFinite, m: m >> tz, k: k - tz, lo: math.Inf(-1), hi: math.Inf(1)}
	if t >= 0x1p-60 && t <= 1 {
		f.lo, f.hi = t*(1-0x1p-50), t*(1+0x1p-50)
	}
	return f
}

// IsZero reports whether the threshold is 0 (every support meets it as a
// minimum).
func (f Frac) IsZero() bool { return f.kind == kindZero }

// u128 is an unsigned 128-bit integer.
type u128 struct{ hi, lo uint64 }

// shr returns x >> k.
func (x u128) shr(k uint) u128 {
	switch {
	case k == 0:
		return x
	case k >= 128:
		return u128{}
	case k >= 64:
		return u128{0, x.hi >> (k - 64)}
	}
	return u128{x.hi >> k, x.lo>>k | x.hi<<(64-k)}
}

// mod returns x mod 2^k for k < 128.
func (x u128) mod(k uint) u128 {
	if k >= 64 {
		return u128{x.hi & (1<<(k-64) - 1), x.lo}
	}
	return u128{0, x.lo & (1<<k - 1)}
}

// less reports whether x < y.
func (x u128) less(y u128) bool { return x.hi < y.hi || x.hi == y.hi && x.lo < y.lo }

// isZero reports whether x == 0.
func (x u128) isZero() bool { return x.hi == 0 && x.lo == 0 }

// sat converts x to int64, saturating at math.MaxInt64.
func (x u128) sat() int64 {
	if x.hi != 0 || x.lo > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(x.lo)
}

// inc returns x+1, saturating at math.MaxInt64 like sat.
func inc(v int64) int64 {
	if v == math.MaxInt64 {
		return v
	}
	return v + 1
}

// parts splits x = t·n = X·2^-k into q = ⌊x⌋ (saturated), the remainder
// r = X mod 2^k (x is an integer iff r == 0) and X itself. For k <= 0, x
// is an integer and r is 0.
func (f Frac) parts(n int64) (q int64, r, x u128, k uint, integer bool) {
	hi, lo := bits.Mul64(f.m, uint64(n)) // m < 2^53, n < 2^63: X < 2^116
	x = u128{hi, lo}
	if f.k <= 0 {
		s := uint(-f.k)
		if x.isZero() {
			return 0, u128{}, x, 0, true
		}
		// X·2^s >= 2^63 iff X >= 2^(63-s)
		if s >= 63 || x.hi != 0 || x.lo >= 1<<(63-s) {
			return math.MaxInt64, u128{}, x, 0, true
		}
		return int64(x.lo << s), u128{}, x, 0, true
	}
	k = uint(f.k)
	q = x.shr(k).sat()
	if k >= 128 {
		r = x
	} else {
		r = x.mod(k)
	}
	return q, r, x, k, r.isZero()
}

// Min returns the minimum support for the reference support n >= 0:
// max(⌈t·n·(1−ε)⌉, ⌊t·n⌋), saturating at math.MaxInt64. A negative n
// counts as 0.
func (f Frac) Min(n int64) int64 {
	switch f.kind {
	case kindZero:
		return 0
	case kindInf:
		return math.MaxInt64
	}
	n = max(n, 0)
	q, r, x, _, integer := f.parts(n)
	// ⌊x⌋ >= x·(1−ε) iff (x−⌊x⌋)·2^52 <= x iff r·2^52 <= X iff
	// r <= ⌊X/2^52⌋ (r is an integer).
	if integer || !x.shr(52).less(r) {
		return q
	}
	return inc(q)
}

// Max returns the maximum support for the reference support n >= 0:
// min(⌊t·n·(1+ε)⌋, ⌈t·n⌉), saturating at math.MaxInt64. A negative n
// counts as 0.
func (f Frac) Max(n int64) int64 {
	switch f.kind {
	case kindZero:
		return 0
	case kindInf:
		return math.MaxInt64
	}
	n = max(n, 0)
	q, r, x, k, integer := f.parts(n)
	if integer || k >= 128 {
		// k >= 128: x < 2^-12, and ⌈x⌉ = 1 > x·(1+ε)
		return q
	}
	// ⌈x⌉ <= x·(1+ε) iff (⌈x⌉−x)·2^52 <= x iff (2^k−r)·2^52 <= X iff
	// 2^k−r <= ⌊X/2^52⌋.
	var p u128 // 2^k
	if k >= 64 {
		p.hi = 1 << (k - 64)
	} else {
		p.lo = 1 << k
	}
	lo, borrow := bits.Sub64(p.lo, r.lo, 0)
	hi, _ := bits.Sub64(p.hi, r.hi, borrow)
	if !x.shr(52).less(u128{hi, lo}) {
		return inc(q)
	}
	return q
}

// Meets reports whether the support s meets the minimum t of the
// reference support n >= 0, i.e. s >= f.Min(n), for 0 <= s <= n. It is
// the per-rule confidence test, so it first decides in float64 and falls
// back to Min only near the boundary.
//
// Fast path: a = float64(s) and b = float64(n)·hi (or ·lo) carry at most
// four roundings of a relative 2^-53 each (s, n, t·(1±2^-50) and the
// product); with x = t·n, a >= b implies s >= x·(1+2^-50)·(1−2^-51) > x,
// which meets the minimum, and a < float64(n)·lo implies
// s < x·(1−2^-50)·(1+2^-51) < x·(1−2^-52), which does not. All values are
// normal floats for 2^-60 <= t <= 1 and n >= 1 (n = 0 gives x = 0, met
// by every s >= 0), and x <= n, so Min does not saturate; other t use
// Min.
func (f *Frac) Meets(s, n int64) bool {
	if meets, sure := f.Quick(s, n); sure {
		return meets
	}
	return s >= f.Min(n)
}

// Quick is the float64 part of Meets: if sure, meets is the result of
// Meets(s, n); otherwise s is near the boundary and only Min decides. It
// is small enough to be inlined into the per-rule confidence test.
func (f *Frac) Quick(s, n int64) (meets, sure bool) {
	a, b := float64(s), float64(n)
	if a >= b*f.hi {
		return true, true
	}
	return false, a < b*f.lo
}
