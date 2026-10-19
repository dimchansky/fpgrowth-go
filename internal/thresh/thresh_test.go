package thresh

import (
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
)

// The reference evaluates the contract with math/big: x = t·n exactly,
// min = max(⌈x(1−ε)⌉, ⌊x⌋), max = min(⌊x(1+ε)⌋, ⌈x⌉), saturated.

var (
	refOneMinus = new(big.Rat).SetFrac(big.NewInt(1<<52-1), big.NewInt(1<<52))
	refOnePlus  = new(big.Rat).SetFrac(big.NewInt(1<<52+1), big.NewInt(1<<52))
	refMaxInt64 = big.NewInt(math.MaxInt64)
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

func refBig(t float64, n int64) (lo, hi int64) {
	switch {
	case !(t > 0):
		return 0, 0
	case math.IsInf(t, 1):
		return math.MaxInt64, math.MaxInt64
	}
	x := new(big.Rat).Mul(new(big.Rat).SetFloat64(t), new(big.Rat).SetInt64(max(n, 0)))
	minV := refCeil(new(big.Rat).Mul(x, refOneMinus))
	if f := refFloor(x); f.Cmp(minV) > 0 {
		minV = f
	}
	maxV := refFloor(new(big.Rat).Mul(x, refOnePlus))
	if c := refCeil(x); c.Cmp(maxV) < 0 {
		maxV = c
	}
	return refSat(minV), refSat(maxV)
}

func check(t *testing.T, tv float64, n int64) {
	t.Helper()
	f := New(tv)
	wantMin, wantMax := refBig(tv, n)
	if got := f.Min(n); got != wantMin {
		t.Fatalf("New(%v).Min(%d) = %d, want %d", tv, n, got, wantMin)
	}
	if got := f.Max(n); got != wantMax {
		t.Fatalf("New(%v).Max(%d) = %d, want %d", tv, n, got, wantMax)
	}
}

var specials = []float64{
	1, math.Nextafter(1, 0), math.Nextafter(1, 2), 0.5, 0.1, 0.3, math.Nextafter(0.3, 1),
	0.8, 1.5, 2, 100, 229388.4, 1e300, math.MaxFloat64, 1e-300, 5e-324, 0x1p-60, 0x1p-70,
	0x1p-127, 0x1p-128, 0x1p-129, 0x1p63, 0x1p64, 0, math.Copysign(0, -1), -1, math.Inf(1),
}

var refN = []int64{0, 1, 2, 3, 10, 1000, 1<<52 - 1, 1 << 52, 1<<52 + 1, 1<<53 - 1, 1<<53 + 1,
	1<<61 - 1, 1<<61 + 1, 1 << 62, math.MaxInt64 - 1, math.MaxInt64}

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
	for i := range 200000 {
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
		{"infinity of 0", math.Inf(1), 0, math.MaxInt64, math.MaxInt64},
		{"tiny", 5e-324, math.MaxInt64, 1, 0},
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
