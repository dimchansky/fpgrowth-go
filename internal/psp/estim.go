package psp

import (
	"context"
	"math"
	"math/bits"
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Floating-point notes for this file. Upstream's oracle is compiled with
// -ffp-contract=off, so no product is fused with an addition. The Go
// compiler fuses x*y + z into one FMA instruction on arm64 and other
// platforms unless the product is converted explicitly; every product
// that feeds an addition or subtraction therefore goes through mulAdd
// (TestMulAdd_noFusion). math.Log and math.Exp may differ from the
// platform's libm in the last bit, so estimated counts can differ by one
// where a value lies at a rounding boundary; see the tolerance in
// docs/upstream/fpgpsp.md (Estimation).

// mulAdd returns a*b + c with the product rounded before the addition,
// like C compiled with -ffp-contract=off (never a fused multiply-add).
func mulAdd(a, b, c float64) float64 { return float64(a*b) + c }

// EstimateConfig holds the parameters of the estimation (fpg_estpsp).
type EstimateConfig struct {
	// Equiv is the number of surrogates the estimate is equivalent to
	// (fpgpsp's -c): the estimated counts are expected counts times
	// Equiv, rounded.
	Equiv uint64
	// Alpha is the probability dispersion factor (-e): <= 0 selects the
	// simple estimation with equal item probabilities.
	Alpha float64
	// Samples is the number of item set samples per size (-z), > 0.
	Samples uint64
	// Seed seeds the generator of the samples (rseed).
	Seed uint32
}

// Estimate is a port of psp_tbgest (with oracle patches P1 and P5): it
// estimates the pattern spectrum of surrogates of d and adds it to psp,
// whose limits are fpg_estpsp's: sizes [zmin, zmax], supports [minimum
// support, d.Len()]. Sizes are estimated from psp's minimum size up to
// d.NItems-1 (a size equal to the number of items is never estimated,
// D10). Cancellation of ctx is checked for every size and every few
// samples; the error is then ctx.Err() and psp is incomplete.
func Estimate(ctx context.Context, d *Data, psp *report.Spectrum, zmin int, c EstimateConfig) error {
	cnts := taslots(d)
	n := d.NItems
	smax := tract.Supp(d.Len())
	if c.Alpha <= 0 { // equal item probabilities
		return simpest(ctx, psp, cnts, zmin, n, smax, c.Equiv)
	}
	probs := make([]float64, n)
	x := 1.0 / float64(d.Extent())
	for z, f := range d.Frequencies() { // P5: frequencies of the filtered array
		probs[z] = x * float64(f)
	}
	if c.Alpha != 1.0 { // contract the rate distribution
		x = 1.0 / float64(n)
		for z := range probs {
			probs[z] = mulAdd(probs[z]-x, c.Alpha, x)
		}
	}
	rng := NewRNG(c.Seed) // rseed(seed): drand
	return cplxest(ctx, psp, cnts, probs, zmin, n, smax, c.Equiv, c.Samples, rng)
}

// lg is ln(Gamma(n)) of an integer argument.
func lg(n int) float64 { return ruleval.LogGamma(float64(n)) }

// toCount is the conversion (size_t)(v) of a non-negative value; values
// that do not fit (and NaN) give math.MaxUint64 instead of undefined
// behavior.
func toCount(v float64) uint64 {
	if !(v < 0x1p64) {
		return math.MaxUint64
	}
	if v < 0 {
		return 0
	}
	return uint64(v)
}

// satMul and satAdd are uint64 operations that saturate at
// math.MaxUint64.
func satMul(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	if hi != 0 {
		return math.MaxUint64
	}
	return lo
}

func satAdd(a, b uint64) uint64 {
	s, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		return math.MaxUint64
	}
	return s
}

// choose is a port of patspec.c's choose: the binomial coefficient
// C(n, k) as size_t, math.MaxUint64 if a step would overflow.
func choose(n, k int) uint64 {
	if n-k < k {
		k = n - k
	}
	r := uint64(1)
	for i := 1; i <= k; i++ {
		if math.MaxUint64/uint64(n) < r {
			return math.MaxUint64
		}
		r = r * uint64(n) / uint64(i)
		n--
	}
	return r
}

// dchoose is a port of dchoose: C(n, k) in double arithmetic.
func dchoose(n, k int) float64 {
	if n-k < k {
		k = n - k
	}
	r := 1.0
	for i := 1; i <= k; i++ {
		r = r * float64(n) / float64(i)
		n--
	}
	return r
}

// taslots is a port of taslots: the number of item set "slots" per size,
// cnts[k] = sum over the transactions t of C(|t|, k) (for k < |t|; a
// transaction of size k counts once for k).
//
// Deviation from upstream (U11): upstream computes in size_t, where
// choose saturates at SIZE_MAX but the product and the sum wrap around
// (transactions of about 60 or more items); here they saturate at
// math.MaxUint64. Without overflow the values are identical.
func taslots(d *Data) []uint64 {
	n := d.NItems
	cnts := make([]uint64, n+1)
	for _, t := range d.Tracts {
		cnts[len(t)]++
	}
	// Deviation from upstream (U17, patch P7): upstream's search
	// "while (--n > 0)" starts below n, so transactions with all n items
	// were not expanded into their subsets.
	for n > 0 && cnts[n] == 0 { // find the size of the largest transaction
		n--
	}
	for i := 0; i <= n; i++ {
		for k := 1; k < i; k++ {
			cnts[k] = satAdd(cnts[k], satMul(cnts[i], choose(i, k)))
		}
	}
	return cnts
}

// simpest is a port of simpest (seq = 0): a Poisson approximation with
// equal item probabilities.
func simpest(ctx context.Context, psp *report.Spectrum, cnts []uint64, zmin, n int, smax tract.Supp, equiv uint64) error {
	for z := zmin; z < n; z++ {
		if cnts[z] == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		y := lg(n+1) - lg(n-z+1) - lg(z+1)
		l := math.Log(float64(cnts[z])) - y
		x := y - math.Exp(l) // start of the Poisson distribution
		for c := tract.Supp(1); c <= smax; c++ {
			y = l - math.Log(float64(c))
			x += y
			frq := toCount(mulAdd(math.Exp(x), float64(equiv), 0.5))
			if frq == 0 {
				if y > 0 {
					continue
				}
				break
			}
			psp.Add(z, c, frq)
		}
	}
	return nil
}

// cplxest is a port of cplxest (seq = 0): item probabilities from the
// item frequencies, sampled item sets per size.
func cplxest(ctx context.Context, psp *report.Spectrum, cnts []uint64, probs []float64, zmin, n int,
	smax tract.Supp, equiv, smpls uint64, rng *RNG) error {
	dist := make([]float64, smax+1)
	s := smax + 1 // clear fully on the first size
	minSupp := psp.MinSupp()
	for z := zmin; z < n; z++ {
		if cnts[z] == 0 {
			continue
		}
		clear(dist[:s])
		s = 0
		const q = 0.0 // seq = 0
		y := lg(n+1) - q - lg(n-z+1)
		t := -math.Log(float64(equiv)) - y - 9 // threshold for the distribution
		for i := range smpls {
			if i%64 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			l := math.Log(float64(cnts[z])) + samplelp(probs, n, z, rng) - q
			x := -math.Exp(l)
			dist[0] += math.Exp(x)
			c := tract.Supp(1)
			for ; c <= smax; c++ {
				y = l - math.Log(float64(c))
				x += y
				if x >= t {
					dist[c] += math.Exp(x)
				} else if y <= 0 {
					break
				}
			}
			s = max(s, c)
		}
		x := dchoose(n, z) / float64(smpls)
		y = 0.0
		for c := tract.Supp(0); c < s; c++ { // scale, sum the covered slots
			dist[c] *= x
			y = mulAdd(float64(c), dist[c], y)
		}
		x = 1.0
		if y > 0 {
			x = float64(cnts[z]) / y
		}
		for c := minSupp; c < s; c++ {
			dist[c] *= x
			frq := toCount(mulAdd(dist[c], float64(equiv), 0.5))
			if frq == 0 {
				continue
			}
			psp.Add(z, c, frq)
		}
	}
	return nil
}

// samplelp is a port of samplelp: the logarithm of the probability of a
// random k-subset of the items (a partial shuffle of probs, which keeps
// its new order for the next call).
func samplelp(probs []float64, n, k int, rng *RNG) float64 {
	if k <= 0 {
		return 0.0
	}
	k = min(k, n)
	dblSelect(probs[:n], k, rng)
	switch k {
	case 1:
		return math.Log(probs[0])
	case 2:
		p1, p2 := probs[0], probs[1]
		p := p1 * p2
		p = p/(1.0-p1) + p/(1.0-p2)
		return math.Log(p)
	case 3:
		p1, p2, p3 := probs[0], probs[1], probs[2]
		p := p1 * p2 * p3
		p = (p/(1.0-p2)+p/(1.0-p3))/(1.0-p2-p3) +
			(p/(1.0-p3)+p/(1.0-p1))/(1.0-p1-p3) +
			(p/(1.0-p1)+p/(1.0-p2))/(1.0-p1-p2)
		return math.Log(p)
	case 4:
		p1, p2, p3, p4 := probs[0], probs[1], probs[2], probs[3]
		p := p1 * p2 * p3 * p4
		p = ((p/(1.0-p3)+p/(1.0-p4))/(1.0-p3-p4)+
			(p/(1.0-p4)+p/(1.0-p2))/(1.0-p2-p4)+
			(p/(1.0-p2)+p/(1.0-p3))/(1.0-p2-p3))/(1.0-p2-p3-p4) +
			((p/(1.0-p3)+p/(1.0-p4))/(1.0-p3-p4)+
				(p/(1.0-p4)+p/(1.0-p1))/(1.0-p1-p4)+
				(p/(1.0-p1)+p/(1.0-p3))/(1.0-p1-p3))/(1.0-p1-p3-p4) +
			((p/(1.0-p2)+p/(1.0-p4))/(1.0-p2-p4)+
				(p/(1.0-p4)+p/(1.0-p1))/(1.0-p1-p4)+
				(p/(1.0-p1)+p/(1.0-p2))/(1.0-p1-p2))/(1.0-p1-p2-p4) +
			((p/(1.0-p2)+p/(1.0-p3))/(1.0-p2-p3)+
				(p/(1.0-p3)+p/(1.0-p1))/(1.0-p1-p3)+
				(p/(1.0-p1)+p/(1.0-p2))/(1.0-p1-p2))/(1.0-p1-p2-p3)
		return math.Log(p)
	}
	p := 0.0
	nf := float64(n)
	s := probs[:k]
	for i := range 8 { // 8 permutations and their reversals
		if i == 0 {
			slices.Sort(s) // dbl_qsort(probs, k, +1)
		} else {
			dblSelect(s, k-1, rng) // dbl_shuffle
		}
		p += permProb(s, nf)
		slices.Reverse(s)
		p += permProb(s, nf)
	}
	// The factor n of every step, removed again by -k·log(n), prevents
	// underflow.
	// log(p/16) - k*log(n) + logGamma(k+1); negating k is exact
	return mulAdd(-float64(k), math.Log(nf), math.Log(p/16)) + lg(k+1)
}

// permProb is the probability (times n^k) of drawing the items of s in
// this order without replacement.
func permProb(s []float64, n float64) float64 {
	t, r := 1.0, 1.0
	for _, v := range s {
		t *= n * v / r
		r -= v
	}
	return t
}

// dblSelect is a port of dbl_select(a, len(a), k, drand): it moves k
// randomly selected elements to the front of a (a partial Fisher-Yates
// shuffle from the front). dbl_shuffle(a, n) is dblSelect(a[:n], n-1).
func dblSelect(a []float64, k int, rng *RNG) {
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
