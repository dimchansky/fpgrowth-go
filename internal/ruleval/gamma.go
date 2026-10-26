package ruleval

import "math"

// Constants of upstream gamma.c. The decimal literals are those of the C
// source; as untyped Go constants they round to the same float64 values as
// the C literals. epsilon is written as the exact power of two that
// upstream's 2.2204460492503131e-16 rounds to, so that the constant
// expressions derived from it (tiny, 4*epsilon, tableLimit) are the values
// C computes in double arithmetic.
const (
	lnBase    = 2.71828182845904523536028747135  // e (LN_BASE)
	sqrtPi    = 1.77245385090551602729816748334  // sqrt(pi) (SQRT_PI)
	lnPi      = 1.14472988584940017414342735135  // ln(pi) (LN_PI)
	lnSqrt2Pi = 0.918938533204672741780329736406 // ln(sqrt(2 pi)) (LN_SQRT_2PI)
	epsilon   = 0x1p-52                          // EPSILON = DBL_EPSILON
	maxFact   = 170                              // MAXFACT
	maxIter   = 1024                             // MAXITER
	tiny      = epsilon * epsilon * epsilon      // TINY

	// tableLimit is upstream's MAXFACT+1+4*EPSILON. In double arithmetic
	// 171 + 2^-50 rounds to 171, and Go rounds the exact constant to the
	// same float64 when it is compared with a float64 operand.
	tableLimit = maxFact + 1 + 4*epsilon
)

// logGammaTables holds upstream's lazily filled tables of gamma.c (only
// the logarithmic ones are needed). Go fills them once during package
// initialization, so they are safe for concurrent use; upstream's lazy
// init() is not thread-safe.
type logGammaTables struct {
	logfs [maxFact + 1]float64 // logfs[i] = ln(i!), from a running product
	loghs [maxFact + 1]float64 // loghs[i] = ln(Gamma(i + 1/2))
}

var lgTables = newLogGammaTables()

// newLogGammaTables ports init() of gamma.c. The factorials are built by
// repeated double multiplication exactly like upstream, so the table
// entries carry the same rounding as upstream's (up to the last-place
// differences between math.Log and the platform libm's log).
func newLogGammaTables() logGammaTables {
	var t logGammaTables
	x := 1.0 // facts[0] = facts[1] = 1, logfs[0] = logfs[1] = 0
	for i := 2; i <= maxFact; i++ {
		x *= float64(i)
		t.logfs[i] = math.Log(x)
	}
	x = sqrtPi
	t.loghs[0] = 0.5 * lnPi
	// Deviation from upstream: upstream's loop "for (i = 0; ++i < MAXFACT;)"
	// never fills loghs[170], so logGamma(170.5) returns 0 (bug B11). The
	// loop runs up to MAXFACT here; Gamma(170.5) ~ 5.6e305 is finite.
	for i := 1; i <= maxFact; i++ {
		x *= float64(i) - 0.5
		t.loghs[i] = math.Log(x)
	}
	return t
}

// logGamma ports logGamma() of gamma.c (the Lanczos variant with g = 7 and
// nine coefficients that upstream compiles): ln(Gamma(n)) for n > 0.
// Integers and half-integers below 171 are looked up in the tables.
//
// Upstream asserts n > 0. logGamma returns +Inf for n <= 0 (the value at
// the poles of Gamma) instead of reading outside the tables; valid
// contingency tables never get there.
func logGamma(n float64) float64 {
	if n <= 0 {
		return math.Inf(1)
	}
	if n < tableLimit {
		// k >= 1 only excludes 0 < n < 4*epsilon, where upstream reads
		// logfs[-1]; such arguments fall through to the other cases.
		if k := math.Floor(n); math.Abs(n-k) < 4*epsilon && k >= 1 {
			return lgTables.logfs[int(k)-1]
		}
		if math.Abs(float64(2*n)-math.Floor(2*n)) < 4*epsilon {
			return lgTables.loghs[int(math.Floor(n))]
		}
	}
	s := 0.99999999999980993227684700473478 +
		676.520368121885098567009190444019/(n+1) -
		1259.13921672240287047156078755283/(n+2) +
		771.3234287776530788486528258894/(n+3) -
		176.61502916214059906584551354/(n+4) +
		12.507343278686904814458936853/(n+5) -
		0.13857109526572011689554707/(n+6) +
		9.984369578019570859563e-6/(n+7) +
		1.50563273514931155834e-7/(n+8)
	return float64((n+0.5)*math.Log((n+7.5)/lnBase)) + (lnSqrt2Pi + math.Log(s/n) - 7.0)
}

// series ports series() of gamma.c: the series part of the regularized
// lower incomplete gamma function P(n, x).
func series(n, x float64) float64 {
	t := 1 / n
	sum := t
	for range maxIter {
		n++
		t = float64(t * (x / n))
		sum += t
		if math.Abs(t) < math.Abs(sum)*epsilon {
			break
		}
	}
	return sum
}

// cfrac ports cfrac() of gamma.c: the continued fraction for the
// regularized upper incomplete gamma function Q(n, x), evaluated with
// Lentz's algorithm.
func cfrac(n, x float64) float64 {
	b := x + 1 - n
	c := 1 / tiny
	d := 1 / b
	f := d
	for i := 1; i < maxIter; i++ {
		a := float64(float64(i) * (n - float64(i)))
		b += 2
		d = float64(a*d) + b
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = b + a/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		e := float64(d * c)
		f *= e
		if math.Abs(e-1) < epsilon {
			break
		}
	}
	return f
}

// gammaQ ports GammaQ() of gamma.c: the regularized upper incomplete gamma
// function Q(n, x) = 1 - P(n, x). Upstream asserts x >= 0; like
// upstream's release build, gammaQ returns 1 for every x <= 0.
func gammaQ(n, x float64) float64 {
	if x <= 0 {
		return 1
	}
	if x < n+1 {
		return 1 - float64(series(n, x)*math.Exp(float64(n*math.Log(x))-x-logGamma(n)))
	}
	return cfrac(n, x) * math.Exp(float64(n*math.Log(x))-x-logGamma(n))
}

// chi2cdfQ ports chi2cdfQ() of chi2.c: the upper tail probability of the
// chi^2 distribution with df degrees of freedom.
func chi2cdfQ(x, df float64) float64 {
	return gammaQ(0.5*df, 0.5*x)
}

// LogGamma is logGamma for other internal packages: ln(Gamma(n)) for
// n > 0, a port of gamma.c's logGamma (used by fpgpsp's estimation,
// internal/psp).
func LogGamma(n float64) float64 { return logGamma(n) }
