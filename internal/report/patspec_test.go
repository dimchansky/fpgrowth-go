package report

import (
	"math"
	"runtime"
	"slices"
	"testing"
)

func TestSpectrum_AddAndOrder(t *testing.T) {
	t.Parallel()
	p := NewSpectrum(1, 3, 2, 100)
	p.Add(2, 5, 1)
	p.Add(1, 7, 2)
	p.Add(2, 3, 1)
	p.Add(2, 5, 4)
	p.Add(0, 5, 1)   // size below the limit
	p.Add(4, 5, 1)   // size above
	p.Add(1, 1, 1)   // support below
	p.Add(1, 101, 1) // support above
	p.Add(3, 9, 0)   // zero frequency
	got := slices.Collect(p.All())
	want := []Cell{{1, 7, 2}, {2, 3, 1}, {2, 5, 5}}
	if !slices.Equal(got, want) {
		t.Fatalf("All = %v, want %v", got, want)
	}
	if p.SigCount() != 3 || p.Total() != 8 {
		t.Fatalf("SigCount %d Total %d, want 3 8", p.SigCount(), p.Total())
	}
	if p.Frequency(2, 5) != 5 || p.Frequency(3, 9) != 0 || p.Frequency(0, 5) != 0 {
		t.Fatal("Frequency mismatch")
	}
	for range p.All() {
		break // early stop must be allowed
	}
}

func TestSpectrum_Limits(t *testing.T) {
	t.Parallel()
	p := NewSpectrum(0, -1, 0, -1) // negative maxima mean "no limit"
	if p.maxSize != MaxSize-1 || p.maxSupp != MaxSupp-1 {
		t.Fatalf("limits %d %d", p.maxSize, p.maxSupp)
	}
	p = NewSpectrum(0, MaxSize, 0, MaxSupp)
	p.Add(5, MaxSupp, 1) // RSUPP_MAX itself is outside the clamped range
	p.Add(5, MaxSupp-1, 1)
	if p.SigCount() != 1 {
		t.Fatalf("SigCount %d, want 1", p.SigCount())
	}
}

func TestSpectrum_Saturation(t *testing.T) {
	t.Parallel()
	p := NewSpectrum(0, 10, 0, 10)
	p.Add(1, 1, math.MaxUint64-1)
	if p.Saturated() {
		t.Fatal("saturated too early")
	}
	p.Add(1, 1, 5)
	if !p.Saturated() || p.Frequency(1, 1) != math.MaxUint64 || p.Total() != math.MaxUint64 {
		t.Fatalf("saturation: %v %d %d", p.Saturated(), p.Frequency(1, 1), p.Total())
	}
}

// TestSpectrum_SparseMemory is the regression test for the dense support
// rows of patspec.c (tract.md T9/R10: supports 1 and 2·10⁷ cost 162 MB).
func TestSpectrum_SparseMemory(t *testing.T) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	p := NewSpectrum(1, MaxSize, 1, MaxSupp)
	p.Add(1, 1, 1)
	p.Add(1, 20_000_000, 1)
	p.Add(1, 1<<60, 1)
	runtime.ReadMemStats(&after)
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 64<<10 {
		t.Fatalf("spectrum with 3 cells allocated %d bytes", grown)
	}
	if got := slices.Collect(p.All()); len(got) != 3 || got[2].Supp != 1<<60 {
		t.Fatalf("cells %v", got)
	}
}

// TestSpectrum_Merge checks the port of psp_addpsp and the accessors used
// by fpgpsp's decision border.
func TestSpectrum_Merge(t *testing.T) {
	t.Parallel()
	a := NewSpectrum(1, 3, 2, 100)
	a.Add(1, 5, 2)
	a.Add(2, 3, 1)
	b := NewSpectrum(0, 10, 0, 1000)
	b.Add(1, 5, 3)
	b.Add(3, 9, 1)
	b.Add(4, 9, 1) // beyond a's maximum size: ignored
	b.Add(2, 1, 1) // below a's minimum support: ignored
	a.Merge(b)
	want := []Cell{{1, 5, 5}, {2, 3, 1}, {3, 9, 1}}
	if got := slices.Collect(a.All()); !slices.Equal(got, want) || a.SigCount() != 3 || a.Total() != 7 {
		t.Fatalf("merged %v (sig %d, total %d), want %v", got, a.SigCount(), a.Total(), want)
	}
	if s, ok := a.MaxSupport(3); !ok || s != 9 {
		t.Errorf("MaxSupport(3) = %d, %v", s, ok)
	}
	if _, ok := a.MaxSupport(0); ok {
		t.Errorf("MaxSupport below the minimum size")
	}
	if a.MaxSize() != 3 || NewSpectrum(2, 5, 1, 9).MaxSize() != 1 || a.MinSupp() != 2 {
		t.Errorf("MaxSize %d, empty %d, MinSupp %d", a.MaxSize(), NewSpectrum(2, 5, 1, 9).MaxSize(), a.MinSupp())
	}
}
