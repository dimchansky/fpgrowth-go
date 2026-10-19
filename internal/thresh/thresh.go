// Package thresh converts relative thresholds (fractions of a reference
// support) to absolute supports. It implements the threshold contract of
// docs/differences.md (N2, issue #17) for minimum and maximum supports,
// the per-size border and the rule confidence.
//
// # Contract
//
// A threshold is a float64 t >= 0 and N a reference support (the total
// transaction weight for supports, the body support for confidence). Let
// x = t·N be the exact rational product of t's binary value and N, and
// ε = 2^-52 (C's DBL_EPSILON). The contract is a hybrid:
//
//   - Below the switch (x < 2^52 for minima and confidence, x <= 2^52 for
//     maxima) the result is upstream's double formula, bit for bit the
//     decision of v0.1.0 and of the C program: a minimum support
//     ⌈RN(RN(t·RN(N))·(1−ε))⌉ (fpg_data, setbdr), a maximum
//     ⌊RN(RN(t·RN(N))·(1+ε))⌋ (fpg_report), and a rule meets the
//     confidence t iff RN(s) >= RN(RN(N)·RN(t·(1−ε))) (istree), where RN
//     rounds to float64 in that evaluation order.
//   - At and above the switch the formula is evaluated exactly with its
//     tolerance capped below one unit of support: s meets a minimum (or
//     the confidence) iff s >= x·(1−ε) and s > x−1, i.e. the minimum is
//     max(⌈x·(1−ε)⌉, ⌊x⌋) = ⌊x⌋; s meets a maximum iff s <= x·(1+ε) and
//     s < x+1, i.e. the maximum is min(⌊x·(1+ε)⌋, ⌈x⌉) = ⌈x⌉.
//
// The branch is decided by the exact x, not by a rounded value. Above the
// switch upstream's relative tolerance would exceed one unit (audit
// ALG-2); below it the double formula is kept unchanged so that no
// result at ordinary weights changes.
//
// # Monotonicity across the switch
//
// Each branch is nondecreasing in t and in N (RN, products, floor and
// ceil are monotone). Across the switch:
//
//   - minimum: for x < 2^52, t·RN(N) <= x·(1+2^-53) < 2^52+1/2, so
//     RN(t·RN(N)) <= 2^52 and the double minimum is at most
//     ⌈RN(2^52·(1−ε))⌉ = 2^52−1, while the exact minimum for x >= 2^52 is
//     ⌊x⌋ >= 2^52;
//   - maximum: for x <= 2^52 likewise RN(t·RN(N)) <= 2^52 and the double
//     maximum is at most ⌊2^52·(1+ε)⌋ = 2^52+1, while the exact maximum
//     for x > 2^52 is ⌈x⌉ >= 2^52+1 (switching at x >= 2^52 would break
//     this: for t = 1/8 the double maximum of N = 2^55−1 is 2^52+1, as
//     RN(N) = 2^55, while the exact one of N = 2^55 would be 2^52);
//   - confidence: for x < 2^52, RN(N)·RN(t·(1−ε)) <= x·(1−ε)·(1+2^-53)^2
//     < 2^52, so the double minimum ⌈RN(...)⌉ is at most 2^52, the exact
//     minimum for x >= 2^52 at least 2^52.
//
// So every minimum, maximum and confidence minimum is nondecreasing in t
// and in N over the whole range: a larger threshold never admits a
// support that a smaller one rejects, and no rule with a body support of
// at least bodyMin has a support below ConfMin(bodyMin) >=
// RuleSetMin(bodyMin), the rule set minimum. For the same t, every
// support meets the minimum or the maximum (Min(N) <= Max(N)+1) in each
// branch, and at x = 2^52 the exact minimum 2^52 is at most the double
// maximum (>= 2^52).
//
// Results saturate at math.MaxInt64. t <= 0 (also −0) gives 0, +Inf gives
// math.MaxInt64 (0 for N = 0, where C's Inf·0 is NaN); NaN panics (callers
// validate it).
package thresh

import (
	"math"
	"math/bits"
)

// eps is C's DBL_EPSILON, the relative tolerance of upstream's formulas.
const eps = 0x1p-52

// kind classifies a Frac.
type kind uint8

const (
	kindZero kind = iota // t <= 0
	kindFinite
	kindInf // t = +Inf
)

// Frac is a relative threshold t >= 0: its float64 value for upstream's
// double formulas and an exact form t = m·2^-k (odd m < 2^53, k may be
// negative) for the exact branch. The zero value is t = 0.
type Frac struct {
	kind kind
	m    uint64
	k    int
	t    float64 // t itself (0 for t <= 0)
	adj  float64 // RN(t·(1−ε)), istree's confidence factor
	// small reports t <= 1 (and t finite): then n < 2^52 implies
	// x = t·n < 2^52, which Quick uses without the exact comparison.
	small bool
}

// New returns the threshold t. It panics if t is NaN.
func New(t float64) Frac {
	switch {
	case math.IsNaN(t):
		panic("thresh: NaN threshold")
	case !(t > 0): // also −0 and negative values
		return Frac{small: true}
	case math.IsInf(t, 1):
		return Frac{kind: kindInf, t: t, adj: t}
	}
	fr, e := math.Frexp(t) // t = fr·2^e, fr in [0.5, 1), also for subnormals
	m := uint64(fr * (1 << 53))
	k := 53 - e
	tz := bits.TrailingZeros64(m)
	return Frac{kind: kindFinite, m: m >> tz, k: k - tz, t: t, adj: t * (1 - eps), small: t <= 1}
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

// cmpSwitch returns the sign of x − 2^52 for x = t·n (n >= 0), exactly.
func (f Frac) cmpSwitch(n int64) int {
	switch {
	case f.kind == kindZero || n <= 0:
		return -1
	case f.kind == kindInf:
		return 1
	}
	hi, lo := bits.Mul64(f.m, uint64(n)) // X = m·n; x = X·2^-k
	x := u128{hi, lo}
	e := 52 + f.k // x < 2^52 iff X < 2^e
	switch {
	case e < 0:
		return 1 // X >= 1 > 2^e
	case e >= 128:
		return -1 // X < 2^116
	}
	var p u128 // 2^e
	if e >= 64 {
		p.hi = 1 << (e - 64)
	} else {
		p.lo = 1 << e
	}
	switch {
	case x.less(p):
		return -1
	case p.less(x):
		return 1
	}
	return 0
}

// satFloat converts an integral float64 to int64: 0 for values <= 0 and
// NaN, math.MaxInt64 from 2^63 on.
func satFloat(v float64) int64 {
	switch {
	case !(v > 0):
		return 0
	case v >= 0x1p63:
		return math.MaxInt64
	}
	return int64(v)
}

// Min returns the minimum support for the reference support n >= 0 (a
// negative n counts as 0): upstream's ⌈t·n·(1−ε)⌉ in double arithmetic
// for x = t·n < 2^52, the exact ⌊x⌋ = max(⌈x·(1−ε)⌉, ⌊x⌋) from 2^52 on.
func (f Frac) Min(n int64) int64 {
	n = max(n, 0)
	if f.cmpSwitch(n) < 0 {
		// fpg_data: ceil((smin/100)*w*(1-DBL_EPSILON)), left to right
		return satFloat(math.Ceil(f.t * float64(n) * (1 - eps)))
	}
	return f.exactMin(n)
}

// Max returns the maximum support for the reference support n >= 0 (a
// negative n counts as 0): upstream's ⌊t·n·(1+ε)⌋ in double arithmetic
// for x = t·n <= 2^52, the exact ⌈x⌉ = min(⌊x·(1+ε)⌋, ⌈x⌉) above.
func (f Frac) Max(n int64) int64 {
	n = max(n, 0)
	if f.cmpSwitch(n) <= 0 {
		// fpg_report: floor((smax/100)*w*(1+DBL_EPSILON))
		return satFloat(math.Floor(f.t * float64(n) * (1 + eps)))
	}
	return f.exactMax(n)
}

// ConfMin returns the smallest support that meets the confidence t for
// the body support n >= 0 (see Meets): ⌈RN(RN(n)·RN(t·(1−ε)))⌉ for
// x = t·n < 2^52, the exact ⌊x⌋ from 2^52 on. It is nondecreasing in n.
func (f Frac) ConfMin(n int64) int64 {
	n = max(n, 0)
	if f.cmpSwitch(n) < 0 {
		// below 2^52 every integer is a float64, so the smallest s with
		// RN(s) >= z is ⌈z⌉
		return satFloat(math.Ceil(float64(n) * f.adj))
	}
	return f.exactMin(n)
}

// RuleSetMin returns the rule set minimum for the minimum body support
// n >= 0: like ConfMin, but in the double branch with n rounded toward
// zero to a float64 (fpg_data multiplies the double smin, which is n
// itself below 2^53; v0.1.0's library rounded the exact count down). It is
// at most ConfMin(n') for every n' >= n, so no rule with a body support of
// at least n has a smaller support.
func (f Frac) RuleSetMin(n int64) int64 {
	n = max(n, 0)
	if f.cmpSwitch(n) < 0 {
		v := float64(n)
		if v >= 0x1p63 || int64(v) > n {
			v = math.Nextafter(v, 0)
		}
		return satFloat(math.Ceil(v * f.adj))
	}
	return f.exactMin(n)
}

// Meets reports whether a rule with support s and body support n >= 0
// meets the confidence t: for x = t·n < 2^52 upstream's test
// !(float64(s) < float64(n)·RN(t·(1−ε))) (istree), from 2^52 on
// s >= ⌊x⌋ = ConfMin(n). Equivalently s >= ConfMin(n).
func (f *Frac) Meets(s, n int64) bool {
	if f.cmpSwitch(n) < 0 {
		return float64(s) >= float64(n)*f.adj
	}
	return s >= f.exactMin(n)
}

// Quick is the inlinable part of Meets for the per-rule test: for t <= 1
// and n < 2^52, x = t·n < 2^52, so the result is upstream's double test
// itself and sure is true; otherwise sure is false and Meets decides.
func (f *Frac) Quick(s, n int64) (meets, sure bool) {
	if f.small && n < 1<<52 {
		return float64(s) >= float64(n)*f.adj, true
	}
	return false, false
}

// exactMin returns max(⌈x·(1−ε)⌉, ⌊x⌋) for x = t·n, saturating.
func (f Frac) exactMin(n int64) int64 {
	switch f.kind {
	case kindZero:
		return 0
	case kindInf:
		return math.MaxInt64
	}
	q, r, x, _, integer := f.parts(n)
	// ⌊x⌋ >= x·(1−ε) iff (x−⌊x⌋)·2^52 <= x iff r·2^52 <= X iff
	// r <= ⌊X/2^52⌋ (r is an integer).
	if integer || !x.shr(52).less(r) {
		return q
	}
	return inc(q)
}

// exactMax returns min(⌊x·(1+ε)⌋, ⌈x⌉) for x = t·n, saturating.
func (f Frac) exactMax(n int64) int64 {
	switch f.kind {
	case kindZero:
		return 0
	case kindInf:
		return math.MaxInt64
	}
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
