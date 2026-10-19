package thresh

import (
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
)

// The reference evaluates the hybrid contract independently: the branch
// by the exact x = t·n (math/big), upstream's double formulas (restated
// here from fpg_data, fpg_report and istree) below the switch, and the
// exact capped formulas with math/big above it.

var (
	refOneMinus = new(big.Rat).SetFrac(big.NewInt(1<<52-1), big.NewInt(1<<52))
	refOnePlus  = new(big.Rat).SetFrac(big.NewInt(1<<52+1), big.NewInt(1<<52))
	refMaxInt64 = big.NewInt(math.MaxInt64)
	refSwitch   = new(big.Rat).SetInt64(1 << 52)
)

func refFloor(r *big.Rat) *big.Int {
	q, m := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if m.Sign() < 0 {
		q.Sub(q, big.NewInt(1))
	}
	return q
}

func refCeil(r *big.Rat) *big.Int {
	q, m := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if m.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}

func refSat(x *big.Int) int64 {
	if x.Cmp(refMaxInt64) > 0 {
		return math.MaxInt64
	}
	if x.Sign() < 0 {
		return 0
	}
	return x.Int64()
}

// refSatF is the port's saturating float-to-support conversion.
func refSatF(v float64) int64 {
	switch {
	case math.IsNaN(v) || v <= 0:
		return 0
	case v >= 0x1p63:
		return math.MaxInt64
	}
	return int64(v)
}

// refRes is the reference result for (t, n).
type refRes struct {
	min, max, confMin int64
	below, belowMax   bool    // branch: x < 2^52, x <= 2^52
	confZ             float64 // double branch: RN(RN(n)·RN(t·(1−ε)))
}

func refBig(t float64, n int64) refRes {
	n = max(n, 0)
	const e = 0x1p-52
	var r refRes
	var x *big.Rat
	switch {
	case !(t > 0):
		x = new(big.Rat)
	case math.IsInf(t, 1):
		x = nil
	default:
		x = new(big.Rat).Mul(new(big.Rat).SetFloat64(t), new(big.Rat).SetInt64(n))
	}
	if x == nil { // +Inf: x = +Inf unless n = 0 (Inf·0 is NaN in C: 0)
		if n == 0 {
			return refRes{below: true, belowMax: true, confZ: math.NaN()}
		}
		return refRes{min: math.MaxInt64, max: math.MaxInt64, confMin: math.MaxInt64}
	}
	c := x.Cmp(refSwitch)
	r.below, r.belowMax = c < 0, c <= 0
	tf := max(t, 0)
	r.confZ = float64(n) * (tf * (1 - e))
	exMin := refCeil(new(big.Rat).Mul(x, refOneMinus))
	if f := refFloor(x); f.Cmp(exMin) > 0 {
		exMin = f
	}
	exMax := refFloor(new(big.Rat).Mul(x, refOnePlus))
	if cc := refCeil(x); cc.Cmp(exMax) < 0 {
		exMax = cc
	}
	if r.below {
		r.min = refSatF(math.Ceil(tf * float64(n) * (1 - e)))
		r.confMin = refSatF(math.Ceil(r.confZ))
	} else {
		r.min, r.confMin = refSat(exMin), refSat(exMin)
	}
	if r.belowMax {
		r.max = refSatF(math.Floor(tf * float64(n) * (1 + e)))
	} else {
		r.max = refSat(exMax)
	}
	return r
}

// refMeets is the reference confidence decision.
func refMeets(r refRes, s int64) bool {
	if r.below {
		return float64(s) >= r.confZ
	}
	return s >= r.confMin
}

func check(t *testing.T, tv float64, n int64) {
	t.Helper()
	f := New(tv)
	r := refBig(tv, n)
	if got := f.Min(n); got != r.min {
		t.Fatalf("New(%v).Min(%d) = %d, want %d", tv, n, got, r.min)
	}
	if got := f.Max(n); got != r.max {
		t.Fatalf("New(%v).Max(%d) = %d, want %d", tv, n, got, r.max)
	}
	if got := f.ConfMin(n); got != r.confMin {
		t.Fatalf("New(%v).ConfMin(%d) = %d, want %d", tv, n, got, r.confMin)
	}
	// Meets and Quick near the boundary and at the ends; ConfMin is the
	// smallest support that meets the confidence
	for _, s := range []int64{0, 1, r.confMin - 2, r.confMin - 1, r.confMin, r.confMin + 1, n - 1, n} {
		if s < 0 || s > max(n, 0) {
			continue
		}
		want := refMeets(r, s)
		if got := f.Meets(s, n); got != want {
			t.Fatalf("New(%v).Meets(%d, %d) = %v, want %v", tv, s, n, got, want)
		}
		if want != (s >= r.confMin) && n > 0 && !math.IsInf(tv, 1) {
			t.Fatalf("New(%v): ConfMin(%d) = %d is not the smallest support meeting it (s=%d)", tv, n, r.confMin, s)
		}
		if meets, sure := f.Quick(s, n); sure && meets != want {
			t.Fatalf("New(%v).Quick(%d, %d) = %v (sure), want %v", tv, s, n, meets, want)
		}
	}
}

var specials = []float64{
	1, math.Nextafter(1, 0), math.Nextafter(1, 2), 0.5, 0.1, 0.3, math.Nextafter(0.3, 1),
	0.8, 1.5, 2, 100, 229388.4, 1e300, math.MaxFloat64, 1e-300, 5e-324, 0x1p-60, 0x1p-70,
	0x1p-127, 0x1p-128, 0x1p-129, 0x1p63, 0x1p64, 0, math.Copysign(0, -1), -1, math.Inf(1),
}

var refN = []int64{0, 1, 2, 3, 10, 1000, 1<<52 - 1, 1 << 52, 1<<52 + 1, 1<<53 - 1, 1<<53 + 1,
	1<<61 - 1, 1<<61 + 1, 1 << 62, math.MaxInt64 - 1, math.MaxInt64}

// scaled returns n, or n/10 under the race detector.
func scaled(n int) int {
	if raceEnabled {
		return max(n/10, 1)
	}
	return n
}

// TestAgainstBig compares Min and Max with the big.Rat evaluation of the
// contract for special values (zero, −0, negative, +Inf, subnormal, values
// above 1) and random thresholds and supports over the whole int64 range.
func TestAgainstBig(t *testing.T) {
	t.Parallel()
	for _, tv := range specials {
		for _, n := range refN {
			check(t, tv, n)
		}
	}
	r := rand.New(rand.NewPCG(17, 1))
	for i := range scaled(200000) {
		var tv float64
		switch i % 6 {
		case 0:
			tv = r.Float64()
		case 1:
			tv = specials[r.IntN(len(specials))]
		case 2:
			tv = math.Ldexp(r.Float64(), -r.IntN(140))
		case 3:
			tv = float64(1+r.IntN(100000)) / 1000 / 100
		case 4:
			tv = 1 + r.Float64()*r.Float64()*1000 // above 1: CLI -S/-F beyond 100
		default:
			tv = float64(1+r.IntN(1000)) / float64(1+r.IntN(1000))
		}
		var n int64
		switch r.IntN(4) {
		case 0:
			n = r.Int64N(1000)
		case 1:
			n = r.Int64N(math.MaxInt64)
		case 2:
			n = math.MaxInt64 - r.Int64N(1000)
		default:
			n = r.Int64N(int64(1) << uint(r.IntN(62)+1))
		}
		check(t, tv, n)
	}
}

// pct3091 is a variable so that pct3091/100 is a run-time float64
// division, as on the command line.
var pct3091 = 3.091

// TestPinned checks values stated in the documentation.
func TestPinned(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		t        float64
		n        int64
		min, max int64
	}{
		{"1 of 2^60+1 is exact", 1, 1<<60 + 1, 1<<60 + 1, 1<<60 + 1},
		{"1 of MaxInt64", 1, math.MaxInt64, math.MaxInt64, math.MaxInt64},
		{"1/2 of 2^62 is exact", 0.5, 1 << 62, 1 << 61, 1 << 61},
		{"0.3 of 10", 0.3, 10, 3, 3},
		{"0.1 of 10", 0.1, 10, 1, 1},
		{"nextafter(0.3) of 10", math.Nextafter(0.3, 1), 10, 3, 3},
		{"0.25 of 10", 0.25, 10, 3, 2},
		{"0.1 of 10·2^59 is only as precise as float64(0.1)", 0.1, 10 << 59, 1<<59 + 32, 1<<59 + 32},
		{"1.0000000000000002 of MaxInt64 saturates", math.Nextafter(1, 2), math.MaxInt64, math.MaxInt64, math.MaxInt64},
		{"1.5 of 3·2^61+1 saturates", 1.5, 3<<61 + 1, math.MaxInt64, math.MaxInt64},
		{"zero", 0, 5, 0, 0},
		{"negative zero", math.Copysign(0, -1), 5, 0, 0},
		{"negative", -3, 5, 0, 0},
		{"infinity", math.Inf(1), 5, math.MaxInt64, math.MaxInt64},
		{"infinity of 0 (C: Inf·0 is NaN, 0)", math.Inf(1), 0, 0, 0},
		{"tiny", 5e-324, math.MaxInt64, 1, 0},
		{"1 of 2^52-1 keeps upstream's tolerance", 1, 1<<52 - 1, 1<<52 - 2, 1 << 52},
		{"1 of 2^52 is exact (minimum), upstream's maximum", 1, 1 << 52, 1 << 52, 1<<52 + 1},
		{"1 of 2^52+1 is exact", 1, 1<<52 + 1, 1<<52 + 1, 1<<52 + 1},
		// documented upstream behavior kept below the switch: a full unit off
		// x from about 2^51.6 (x ≈ 3377705443070997.11 and 3377722064612683.82)
		{"0.7677 of 4399772623513087 (upstream, below the switch)", 0.7677, 4399772623513087, 3377705443070996, 3377705443070998},
		{"0.3214 of 10509402814600758 (upstream, below the switch)", 0.3214, 10509402814600758, 3377722064612683, 3377722064612685},
		// the reason maxima switch only above 2^52: RN(2^55-1) = 2^55
		{"1/8 of 2^55-1 (upstream)", 0.125, 1<<55 - 1, 1<<52 - 1, 1<<52 + 1},
		{"1/8 of 2^55 (x = 2^52: upstream's maximum)", 0.125, 1 << 55, 1 << 52, 1<<52 + 1},
		{"1/8 of 2^55+8", 0.125, 1<<55 + 8, 1<<52 + 1, 1<<52 + 1},
		{"-S3.091 of 833815673989: upstream's maximum below 2^52", pct3091 / 100, 833815673989, 25773242483, 25773242483},
	}
	for _, tt := range tests {
		f := New(tt.t)
		if got := f.Min(tt.n); got != tt.min {
			t.Errorf("%s: Min = %d, want %d", tt.name, got, tt.min)
		}
		if got := f.Max(tt.n); got != tt.max {
			t.Errorf("%s: Max = %d, want %d", tt.name, got, tt.max)
		}
	}
	if !New(0).IsZero() || !New(-1).IsZero() || New(1e-300).IsZero() || !(Frac{}).IsZero() {
		t.Error("IsZero")
	}
	if New(1).Min(-5) != 0 || New(1).Max(-5) != 0 {
		t.Error("negative reference support")
	}
}

// TestNaNPanics checks that New rejects NaN.
func TestNaNPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New(NaN) did not panic")
		}
	}()
	New(math.NaN())
}

// TestMonotone checks that Min and Max are nondecreasing in the reference
// support, which the rule set minimum relies on.
func TestMonotone(t *testing.T) {
	t.Parallel()
	for _, tv := range []float64{0.8, 0.3, 1, 0.333, math.Nextafter(0.5, 0)} {
		f := New(tv)
		prevMin, prevMax := int64(0), int64(0)
		for n := int64(0); n <= 5000; n++ {
			lo, hi := f.Min(n), f.Max(n)
			if lo < prevMin || hi < prevMax {
				t.Fatalf("t=%v: not monotone at n=%d", tv, n)
			}
			prevMin, prevMax = lo, hi
		}
	}
}

// TestMonotoneAcrossSwitch checks the monotonicity argument of the
// package documentation where it matters, at x = t·n near 2^52: Min, Max
// and ConfMin are nondecreasing in n (fixed t) and in t (fixed n), and
// Min <= Max+1 (every support meets the minimum or the maximum).
func TestMonotoneAcrossSwitch(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(17, 8))
	type val struct{ lo, hi, conf int64 }
	get := func(f Frac, n int64) val { return val{f.Min(n), f.Max(n), f.ConfMin(n)} }
	checkStep := func(what string, prev, cur val) {
		t.Helper()
		if cur.lo < prev.lo || cur.hi < prev.hi || cur.conf < prev.conf {
			t.Fatalf("%s: not monotone: %+v then %+v", what, prev, cur)
		}
		if cur.lo > cur.hi+1 {
			t.Fatalf("%s: minimum %d above maximum %d + 1", what, cur.lo, cur.hi)
		}
	}
	for i := range scaled(300) {
		// fixed t, n across 2^52/t
		var tv float64
		switch i % 4 {
		case 0:
			tv = r.Float64()
		case 1:
			tv = math.Nextafter(float64(1+r.IntN(100))/100, float64(r.IntN(2)))
		case 2:
			tv = 1 + r.Float64()*1000
		default:
			tv = math.Ldexp(1+r.Float64(), -r.IntN(10)) // t in [2^-9, 2)
		}
		f := New(tv)
		n0 := int64(0x1p52 / tv)
		prev := get(f, max(n0-2000, 0))
		for n := max(n0-1999, 1); n <= n0+2000; n++ {
			cur := get(f, n)
			checkStep(fmt.Sprintf("t=%v n=%d", tv, n), prev, cur)
			prev = cur
		}
	}
	for range scaled(300) {
		// fixed n >= 2^52, t across 2^52/n by single ulps
		n := int64(1)<<52 + r.Int64N(math.MaxInt64-1<<52)
		tv := 0x1p52 / float64(n)
		for range 2000 {
			tv = math.Nextafter(tv, 0)
		}
		prev := get(New(tv), n)
		for range 4000 {
			tv = math.Nextafter(tv, 2)
			cur := get(New(tv), n)
			checkStep(fmt.Sprintf("t=%v n=%d", tv, n), prev, cur)
			prev = cur
		}
	}
}

// FuzzFrac compares Min and Max with the big.Rat reference.
func FuzzFrac(f *testing.F) {
	for _, tv := range specials {
		f.Add(math.Float64bits(tv), uint64(1000))
		f.Add(math.Float64bits(tv), uint64(math.MaxInt64))
	}
	f.Fuzz(func(t *testing.T, tb, nb uint64) {
		tv := math.Float64frombits(tb)
		if math.IsNaN(tv) {
			return
		}
		check(t, tv, int64(nb&math.MaxInt64))
	})
}

// BenchmarkMeets measures the per-rule confidence test.
func BenchmarkMeets(b *testing.B) {
	f := New(0.8)
	k := 0
	for i := range b.N {
		n := int64(i) | 1<<40
		if f.Meets(n/2+int64(i&7), n) {
			k++
		}
	}
	if k == 42 {
		b.Log(k)
	}
}

// BenchmarkMin measures the per-rule confidence conversion.
func BenchmarkMin(b *testing.B) {
	f := New(0.8)
	var s int64
	for i := range b.N {
		s += f.Min(int64(i) | 1<<40)
	}
	if s == 42 {
		b.Log(s)
	}
}
