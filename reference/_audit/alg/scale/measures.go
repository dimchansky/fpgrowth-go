package main

// Independent, definition-based rule evaluation measures. Written from the
// textbook definitions (and the one-line descriptions printed by -!), not
// from the port's or upstream's source. Degenerate-table conventions are
// listed explicitly so mismatches can be classified.

import (
	"math"
	"math/big"
)

// measureDir: +1 = larger is better, -1 = smaller is better (p-values).
var measureDir = map[byte]int{
	'o': 1, 'c': 1, 'd': 1, 'l': 1, 'a': 1, 'q': 1, 'v': 1, 'e': 1, 'r': 1,
	'b': 1, 'k': 1, 'w': 1, 'u': 1, 'j': 1, 'z': 1, 'n': 1, 'p': -1, 'y': 1,
	't': -1, 'i': 1, 'g': -1, 'f': -1, 'h': -1, 'm': -1, 's': -1,
}

const ruleMeasures = "ocdlaqverbkwujznpytigfhms"

func f(x int64) float64 { return float64(x) }

// chi2Q1 is the upper tail of the chi^2 distribution with 1 d.o.f.
func chi2Q1(x float64) float64 {
	if x <= 0 {
		return 1
	}
	return math.Erfc(math.Sqrt(x / 2))
}

func degenerate(b, h, n int64) bool { return h <= 0 || h >= n || b <= 0 || b >= n }

// evalRule returns the measure value for a rule with set support s, body
// support b, head support h and total weight n. defn selects the strict
// definition for the documented quirks (kulczynski diff, conviction quot);
// with defn=false the -! conventions of upstream are used.
func evalRule(m byte, s, b, h, n int64, defn bool) float64 {
	R := func(x, y *big.Int) *big.Rat { return new(big.Rat).SetFrac(x, y) }
	F := func(r *big.Rat) float64 { v, _ := r.Float64(); return v }
	one := big.NewRat(1, 1)
	conf := R(bi(s), bi(b))
	prior := R(bi(h), bi(n))
	ab := func(r *big.Rat) *big.Rat { return new(big.Rat).Abs(r) }
	subR := func(x, y *big.Rat) *big.Rat { return new(big.Rat).Sub(x, y) }
	quoR := func(x, y *big.Rat) *big.Rat { return new(big.Rat).Quo(x, y) }
	minInv := func(x *big.Rat) *big.Rat { // 1 - min(x, 1/x)
		if x.Cmp(one) > 0 {
			x = new(big.Rat).Inv(x)
		}
		return subR(one, x)
	}
	switch m {
	case 'o':
		if defn {
			return f(s) / f(n) // relative rule support
		}
		return f(s)
	case 'c':
		return F(conf)
	case 'd':
		return F(ab(subR(conf, prior)))
	case 'l', 'a', 'q':
		l := R(mul(s, n), mul(b, h))
		switch m {
		case 'l':
			return F(l)
		case 'a':
			return F(ab(subR(l, one)))
		}
		return F(minInv(l))
	case 'v', 'e', 'r':
		if b == s {
			if m == 'r' && defn {
				return 1 // 1 - min(inf, 0)
			}
			return math.Inf(1)
		}
		v := R(mul(b, n-h), mul(b-s, n))
		switch m {
		case 'v':
			return F(v)
		case 'e':
			return F(ab(subR(v, one)))
		}
		return F(minInv(v))
	case 'b':
		return F(R(abs(sub(bi(h), bi(b))), sub(new(big.Int).Add(bi(b), bi(h)), bi(s))))
	case 'k', 'w':
		k := R(new(big.Int).Mul(bi(s), new(big.Int).Add(bi(b), bi(h))), new(big.Int).Mul(mul(b, h), bi(2)))
		if m == 'k' {
			return F(k)
		}
		d := ab(subR(k, big.NewRat(1, 2)))
		if defn {
			return F(d)
		}
		return 2 * F(d)
	case 'u', 'j':
		if b >= n {
			return 0
		}
		if h == s {
			return math.Inf(1)
		}
		u := R(mul(s, n-b), mul(b, h-s))
		if m == 'u' {
			return F(u)
		}
		return math.Log1p(F(subR(u, one))) / math.Ln2
	case 'z':
		d := subR(conf, prior)
		if d.Sign() < 0 {
			return F(quoR(d, prior))
		}
		if h == n {
			return 0
		}
		return F(quoR(d, subR(one, prior)))
	case 'n', 'p':
		var c float64
		if !degenerate(b, h, n) {
			c = ratioBig(sq(sub(mul(n, s), mul(b, h))), mul4(b, h, n-b, n-h))
		}
		if m == 'n' {
			return c
		}
		if degenerate(b, h, n) {
			return 1
		}
		x := new(big.Rat).SetFrac(new(big.Int).Mul(bi(n), sq(sub(mul(n, s), mul(b, h)))), mul4(b, h, n-b, n-h))
		return chi2Q1(F(x))
	case 'y', 't':
		var c *big.Rat = new(big.Rat)
		if !degenerate(b, h, n) {
			d := new(big.Rat).SetInt(abs(sub(mul(n, s), mul(b, h))))
			d.Sub(d, new(big.Rat).SetFrac64(n, 2))
			if d.Sign() < 0 {
				d.SetInt64(0)
			}
			d.Mul(d, d)
			d.Quo(d, new(big.Rat).SetInt(mul4(b, h, n-b, n-h)))
			c = d
		}
		if m == 'y' {
			return F(c)
		}
		return chi2Q1(F(new(big.Rat).Mul(c, new(big.Rat).SetInt64(n))))
	case 'i', 'g':
		var mi float64
		if !degenerate(b, h, n) {
			mi = mutualInfoBits(s, b, h, n)
		}
		if m == 'i' {
			return mi
		}
		return chi2Q1(2 * f(n) * math.Ln2 * mi)
	case 'f', 'h', 'm', 's':
		return fisher(m, s, b, h, n)
	}
	panic("unknown measure")
}

func mutualInfoBits(s, b, h, n int64) float64 {
	cells := [4][3]int64{{s, b, h}, {b - s, b, n - h}, {h - s, n - b, h}, {n - b - h + s, n - b, n - h}}
	sum := 0.0
	for _, c := range cells {
		if c[0] > 0 {
			// ratio = c*n/(r*col); log computed as log1p(ratio-1) exactly-ish
			num := mul(c[0], n)
			den := mul(c[1], c[2])
			d, _ := new(big.Rat).SetFrac(sub(num, den), den).Float64()
			var lg float64
			if math.Abs(d) < 0.5 {
				lg = math.Log1p(d)
			} else {
				r, _ := new(big.Rat).SetFrac(num, den).Float64()
				lg = math.Log(r)
			}
			sum += f(c[0]) / f(n) * lg / math.Ln2
		}
	}
	if sum < 0 {
		sum = 0
	}
	return sum
}

func bi(x int64) *big.Int            { return big.NewInt(x) }
func mul(a, b int64) *big.Int        { return new(big.Int).Mul(bi(a), bi(b)) }
func sub(a, b *big.Int) *big.Int     { return new(big.Int).Sub(a, b) }
func sq(a *big.Int) *big.Int         { return new(big.Int).Mul(a, a) }
func abs(a *big.Int) *big.Int        { return new(big.Int).Abs(a) }
func mul4(a, b, c, d int64) *big.Int { return new(big.Int).Mul(mul(a, b), mul(c, d)) }
func ratioBig(a, b *big.Int) float64 {
	r, _ := new(big.Rat).SetFrac(a, b).Float64()
	return r
}

// fisherMaxN bounds exact Fisher computation.
const fisherMaxN = 600

// fisher computes Fisher's exact test variants exactly (big.Rat) over all
// 2x2 tables with the observed margins. Returns NaN if n is too large.
func fisher(m byte, s, b, h, n int64) float64 {
	if n > fisherMaxN {
		return math.NaN()
	}
	lo := max(0, b+h-n)
	hi := min(b, h)
	// P(x) = C(h,x) C(n-h, b-x) / C(n,b)
	den := new(big.Int).Binomial(n, b)
	prob := func(x int64) *big.Rat {
		num := new(big.Int).Mul(new(big.Int).Binomial(h, x), new(big.Int).Binomial(n-h, b-x))
		return new(big.Rat).SetFrac(num, den)
	}
	// exact chi^2 numerator |n x - b h| (monotone in chi^2 for fixed margins)
	chiKey := func(x int64) *big.Int { return abs(sub(mul(n, x), mul(b, h))) }
	ps := prob(s)
	sum := new(big.Rat)
	for x := lo; x <= hi; x++ {
		px := prob(x)
		in := false
		switch m {
		case 'f':
			in = px.Cmp(ps) <= 0
		case 'h':
			in = chiKey(x).Cmp(chiKey(s)) >= 0
		case 'm':
			in = cmpInfo(x, s, b, h, n) >= 0
		case 's':
			in = x >= s
		}
		if in {
			sum.Add(sum, px)
		}
	}
	r, _ := sum.Float64()
	return r
}

// cmpInfo compares the mutual information of tables x and s exactly:
// MI = (const + sum c ln c)/n, so compare prod c^c (big ints).
func cmpInfo(x, s, b, h, n int64) int {
	pw := func(t int64) *big.Int {
		cells := []int64{t, b - t, h - t, n - b - h + t}
		r := big.NewInt(1)
		for _, c := range cells {
			if c > 0 {
				r.Mul(r, new(big.Int).Exp(bi(c), bi(c), nil))
			}
		}
		return r
	}
	return pw(x).Cmp(pw(s))
}
