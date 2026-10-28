package psp

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// The reference functions below are the straightforward ports of
// upstream's dbl_select and samplelp that estim.go used before #47:
// one RNG.Float64 call per draw, slices.Sort, a physical slices.Reverse
// and one permProb per order. The optimized functions must reproduce
// them bit for bit, including the array order and the generator state
// they leave behind, which the next sample depends on.

// dblSelectRef is the reference port of dbl_select(a, len(a), k, drand).
func dblSelectRef(a []float64, k int, rng *RNG) {
	n := len(a)
	if k < n {
		k++
	} else {
		k = n
	}
	for off := 0; ; off++ {
		k--
		if k <= 0 {
			return
		}
		i := int(rng.Float64() * float64(n))
		n--
		if i > n {
			i = n
		}
		a[off+i], a[off] = a[off], a[off+i]
	}
}

// permProbRef is the reference probability (times n^k) of one order.
func permProbRef(s []float64, n float64) float64 {
	t, r := 1.0, 1.0
	for _, v := range s {
		t *= n * v / r
		r -= v
	}
	return t
}

// samplelpRef is the reference port of samplelp.
func samplelpRef(probs []float64, n, k int, rng *RNG) float64 {
	if k <= 0 {
		return 0.0
	}
	k = min(k, n)
	dblSelectRef(probs[:n], k, rng)
	if k <= 4 {
		return smallSampleLP(probs, k) // unchanged by #47
	}
	p := 0.0
	nf := float64(n)
	s := probs[:k]
	for i := range 8 {
		if i == 0 {
			slices.Sort(s)
		} else {
			dblSelectRef(s, k-1, rng)
		}
		p += permProbRef(s, nf)
		slices.Reverse(s)
		p += permProbRef(s, nf)
	}
	return mulAdd(-float64(k), math.Log(nf), math.Log(p/16)) + lg(k+1)
}

func sameBits(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Float64bits(a[i]) != math.Float64bits(b[i]) {
			return false
		}
	}
	return true
}

// randomProbs returns n values in (0, 1/n·2], with some duplicates.
func randomProbs(r *rand.Rand, n int) []float64 {
	a := make([]float64, n)
	for i := range a {
		if i > 0 && r.IntN(5) == 0 {
			a[i] = a[r.IntN(i)]
			continue
		}
		a[i] = (r.Float64() + 1e-9) * 2 / float64(n)
	}
	return a
}

// TestDblSelect_matchesReference compares dblSelect, and dblSelectBack on
// the reversed array, with the reference loop: the same elements in the
// same places and the same generator state afterwards, for every length
// up to 70 and every k from 0 to n+2 (k >= n selects n-1 elements).
func TestDblSelect_matchesReference(t *testing.T) {
	r := rand.New(rand.NewPCG(47, 1))
	for n := 0; n <= 70; n++ {
		for k := 0; k <= n+2; k++ {
			seed := r.Uint32()
			a := randomProbs(r, n)

			want := slices.Clone(a)
			rw := NewRNG(seed)
			dblSelectRef(want, k, rw)

			got := slices.Clone(a)
			rg := NewRNG(seed)
			dblSelect(got, k, rg)
			if !sameBits(got, want) || rg.s != rw.s {
				t.Fatalf("dblSelect n=%d k=%d seed=%d: got %v state %v, want %v state %v",
					n, k, seed, got, rg.s, want, rw.s)
			}

			// dblSelectBack(a) == reverse(dblSelectRef(reverse(a))).
			back := slices.Clone(a)
			slices.Reverse(back)
			rb := NewRNG(seed)
			dblSelectBack(back, k, rb)
			slices.Reverse(back)
			if !sameBits(back, want) || rb.s != rw.s {
				t.Fatalf("dblSelectBack n=%d k=%d seed=%d: got %v state %v, want %v state %v",
					n, k, seed, back, rb.s, want, rw.s)
			}
		}
	}
}

// TestDblSelect_maxDraw pins the largest draw, u = 2^32-1: the index
// (u·2^-32)·n truncates to n-1, as in the reference.
func TestDblSelect_maxDraw(t *testing.T) {
	// With s0 = 0 and s2 = 0 the next output is the new s4, which is
	// s4 ^ (s4 << 6); invert that map to make the output 2^32-1.
	const y = math.MaxUint32
	var x uint32 = y
	for range 6 {
		x = y ^ (x << 6)
	}
	state := [5]uint32{0, 1, 0, 3, x}
	if probe := (&RNG{s: state}).Uint32(); probe != y {
		t.Fatalf("constructed state gives %#x, want %#x", probe, uint32(y))
	}
	for _, n := range []int{2, 3, 1000, 1 << 20} {
		a := make([]float64, n)
		for i := range a {
			a[i] = float64(i)
		}
		want := slices.Clone(a)
		rw := &RNG{s: state}
		dblSelectRef(want, 1, rw)
		if want[0] != float64(n-1) {
			t.Fatalf("n=%d: reference moved %v to the front, want %d", n, want[0], n-1)
		}
		got := slices.Clone(a)
		rg := &RNG{s: state}
		dblSelect(got, 1, rg)
		if !sameBits(got, want) || rg.s != rw.s {
			t.Fatalf("dblSelect n=%d: front %v state %v, want %v state %v", n, got[0], rg.s, want[0], rw.s)
		}
		back := slices.Clone(a)
		slices.Reverse(back)
		rb := &RNG{s: state}
		dblSelectBack(back, 1, rb)
		slices.Reverse(back)
		if !sameBits(back, want) || rb.s != rw.s {
			t.Fatalf("dblSelectBack n=%d: front %v state %v, want %v state %v", n, back[0], rb.s, want[0], rw.s)
		}
	}
}

// TestSortAsc_matchesSlicesSort compares the insertion sort with
// slices.Sort on random slices with duplicates, zeros, infinities and
// NaNs (negative zero is left out: slices.Sort may order -0 and +0
// either way, and probabilities are never -0).
func TestSortAsc_matchesSlicesSort(t *testing.T) {
	r := rand.New(rand.NewPCG(47, 2))
	special := []float64{0, 1, math.Inf(1), math.Inf(-1), math.NaN(), math.SmallestNonzeroFloat64, -1}
	for round := range 20000 {
		n := r.IntN(40)
		a := make([]float64, n)
		for i := range a {
			switch x := r.IntN(10); {
			case x == 0:
				a[i] = special[r.IntN(len(special))]
			case x == 1 && i > 0:
				a[i] = a[r.IntN(i)]
			default:
				a[i] = r.Float64()
			}
		}
		want := slices.Clone(a)
		slices.Sort(want)
		got := slices.Clone(a)
		sortAsc(got)
		if !sameBits(got, want) {
			t.Fatalf("round %d: sortAsc(%v) = %v, want %v", round, a, got, want)
		}
	}
}

// TestLogTable checks that the table returns math.Log(float64(c)) bit for
// bit, for supports reached in increasing and in random order.
func TestLogTable(t *testing.T) {
	var lt logTable
	if got := lt.at(0); !math.IsInf(got, -1) {
		t.Fatalf("at(0) = %v, want -Inf", got)
	}
	r := rand.New(rand.NewPCG(47, 3))
	for range 5000 {
		c := tract.Supp(r.IntN(3000))
		if r.IntN(4) == 0 {
			c = tract.Supp(len(lt.v)) // the next support, as in the loop
		}
		got, want := lt.at(c), math.Log(float64(c))
		if math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("at(%d) = %v, want %v", c, got, want)
		}
	}
	for c := range tract.Supp(len(lt.v)) {
		if math.Float64bits(lt.v[c]) != math.Float64bits(math.Log(float64(c))) {
			t.Fatalf("v[%d] = %v, want %v", c, lt.v[c], math.Log(float64(c)))
		}
	}
	if n := testing.AllocsPerRun(100, func() { lt.at(2999) }); n != 0 {
		t.Fatalf("at on a covered support allocates %v times", n)
	}
}

// TestSamplelp_matchesReference runs chains of samples on one array and
// one generator, as cplxest does, with samplelp and with the reference:
// every result, the array order after every call and the generator state
// must be the same bit for bit. This covers the fused order
// probabilities, the insertion sort and the shuffles of the unreversed
// array (k >= 5), and the selection of the closed forms (k <= 4).
func TestSamplelp_matchesReference(t *testing.T) {
	r := rand.New(rand.NewPCG(47, 4))
	for round := range 400 {
		n := 1 + r.IntN(60)
		probs := randomProbs(r, n)
		seed := r.Uint32()
		want, got := slices.Clone(probs), slices.Clone(probs)
		rw, rg := NewRNG(seed), NewRNG(seed)
		for call := range 30 {
			k := r.IntN(n + 2)
			lw := samplelpRef(want, n, k, rw)
			lg := samplelp(got, n, k, rg)
			if math.Float64bits(lg) != math.Float64bits(lw) {
				t.Fatalf("round %d call %d (n=%d k=%d): samplelp = %v, want %v", round, call, n, k, lg, lw)
			}
			if !sameBits(got, want) || rg.s != rw.s {
				t.Fatalf("round %d call %d (n=%d k=%d): array or generator state differs", round, call, n, k)
			}
		}
	}
}
