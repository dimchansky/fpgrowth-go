package ruleval

import (
	"math/big"
	"sync"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// High-precision comparison of the weights W(x) = Π c! of two Fisher
// tables with the same margins, for the near-ties of FisherProb whose
// tables are too far apart for cmpWeight's exact big-integer products.
//
// ln W is evaluated at bigLogPrec bits: ln m! exactly for m < 256 (the
// logarithm of the big-integer factorial) and by Stirling's series with
// 15 Bernoulli terms beyond (truncation error below 2^-220 for m >= 256).
// Every value has magnitude below 2^70 (m·ln m for m < 2^63), so the
// rounding errors of the few hundred operations of a comparison stay below
// 2^(70+10-bigLogPrec) = 2^-240; differences beyond bigTieBound are
// therefore decided correctly. The cost is bounded: eight logarithms of
// about 64 series terms at 320 bits (well below a millisecond).

const (
	bigLogPrec  = 320
	bigTieBound = 0x1p-200
)

// bigConsts holds ln 2 and ½·ln(2π) at bigLogPrec bits.
var bigConsts = sync.OnceValue(func() [2]*big.Float {
	ln2 := atanhSeries(newBig().Quo(newBig().SetInt64(1), newBig().SetInt64(3)))
	ln2.Mul(ln2, newBig().SetInt64(2)) // ln 2 = 2·atanh(1/3)
	// π = 16·atan(1/5) - 4·atan(1/239) (Machin)
	pi := atanInv(5)
	pi.Mul(pi, newBig().SetInt64(16))
	pi.Sub(pi, newBig().Mul(atanInv(239), newBig().SetInt64(4)))
	half := bigLog(newBig().Mul(pi, newBig().SetInt64(2)), ln2)
	half.Quo(half, newBig().SetInt64(2))
	return [2]*big.Float{ln2, half}
})

func newBig() *big.Float { return new(big.Float).SetPrec(bigLogPrec) }

// atanhSeries returns atanh(z) = Σ z^(2k+1)/(2k+1) for |z| <= 1/3. The
// loop keeps its operands apart from the results (ping-pong buffers), so
// that math/big does not allocate temporaries for aliased arguments.
func atanhSeries(z *big.Float) *big.Float {
	z2 := newBig().Mul(z, z)
	p, p2 := newBig().Set(z), newBig()
	sum, sum2 := newBig().Set(z), newBig()
	q, kf := newBig(), newBig()
	for k := int64(3); ; k += 2 {
		p2.Mul(p, z2)
		p, p2 = p2, p
		q.Quo(p, kf.SetInt64(k))
		if q.Sign() == 0 || q.MantExp(nil) < sum.MantExp(nil)-bigLogPrec-4 {
			return sum
		}
		sum2.Add(sum, q)
		sum, sum2 = sum2, sum
	}
}

// atanInv returns atan(1/m) = Σ (-1)^k/((2k+1)·m^(2k+1)) for m >= 5.
func atanInv(m int64) *big.Float {
	inv := newBig().Quo(newBig().SetInt64(1), newBig().SetInt64(m))
	inv2 := newBig().Mul(inv, inv)
	p, sum := newBig().Set(inv), newBig().Set(inv)
	q := newBig()
	for k := int64(3); ; k += 2 {
		p.Mul(p, inv2)
		q.Quo(p, q.SetInt64(k))
		if q.MantExp(nil) < sum.MantExp(nil)-bigLogPrec-4 {
			return sum
		}
		if k%4 == 3 {
			sum.Sub(sum, q)
		} else {
			sum.Add(sum, q)
		}
	}
}

// bigLog returns ln x for x > 0: with x = m·2^e, m in [1/√2, √2),
// ln x = e·ln 2 + 2·atanh((m-1)/(m+1)), |(m-1)/(m+1)| < 0.18.
func bigLog(x, ln2 *big.Float) *big.Float {
	m := newBig()
	e := x.MantExp(m) // x = m·2^e, m in [0.5, 1)
	if m.Cmp(big.NewFloat(0.7071067811865476)) < 0 {
		m.SetMantExp(m, 1)
		e--
	}
	one := newBig().SetInt64(1)
	z := newBig().Quo(newBig().Sub(m, one), newBig().Add(m, one))
	r := atanhSeries(z)
	r.Mul(r, newBig().SetInt64(2))
	return r.Add(r, newBig().Mul(ln2, newBig().SetInt64(int64(e))))
}

// stirBern are B_2k/(2k·(2k-1)) for k = 1..15 (numerator, denominator).
var stirBern = [15][2]int64{
	{1, 12}, {-1, 360}, {1, 1260}, {-1, 1680}, {1, 1188},
	{-691, 360360}, {1, 156}, {-3617, 122400}, {43867, 244188}, {-174611, 125400},
	{77683, 5796}, {-236364091, 1506960}, {657931, 300}, {-3392780147, 93960}, {1723168255201, 2492028},
}

// stirCoef holds the stirBern coefficients as bigLogPrec-bit values.
var stirCoef = sync.OnceValue(func() [len(stirBern)]*big.Float {
	var c [len(stirBern)]*big.Float
	for i, cb := range stirBern {
		c[i] = newBig().Quo(newBig().SetInt64(cb[0]), newBig().SetInt64(cb[1]))
	}
	return c
})

// bigLnFact returns ln m! at bigLogPrec bits for m >= 0.
func bigLnFact(m tract.Supp) *big.Float {
	c := bigConsts()
	ln2, halfLn2Pi := c[0], c[1]
	if m < 2 {
		return newBig()
	}
	if m < 256 {
		f := new(big.Int).MulRange(2, int64(m))
		return bigLog(newBig().SetInt(f), ln2)
	}
	x := newBig().SetInt64(int64(m))
	lx := bigLog(x, ln2)
	// (m + 1/2)·ln m - m + ½·ln(2π) + Σ_k c_k / m^(2k-1)
	r := newBig().Mul(newBig().Add(x, big.NewFloat(0.5)), lx)
	r.Sub(r, x)
	r.Add(r, halfLn2Pi)
	inv := newBig().Quo(newBig().SetInt64(1), x)
	inv2 := newBig().Mul(inv, inv)
	pw, pw2 := newBig().Set(inv), newBig()
	t, r2 := newBig(), newBig()
	for _, ck := range stirCoef() {
		t.Mul(pw, ck)
		r2.Add(r, t)
		r, r2 = r2, r
		pw2.Mul(pw, inv2)
		pw, pw2 = pw2, pw
	}
	return r
}

// cmpWeightBig returns the sign of W(x) - W(supp) (see cmpWeight) from
// ln W at bigLogPrec bits, and false if |ln W(x) - ln W(supp)| is below
// bigTieBound, where the rounding error could decide it (the tables are
// then tied to within a relative 2^-200).
func (t *fisherTable) cmpWeightBig(x tract.Supp) (int, bool) {
	cx, cs := t.cells(x), t.cells(t.supp)
	d := newBig()
	for i := range cx {
		if cx[i] == cs[i] { // sorted cells: equal ones cancel
			continue
		}
		d.Add(d, bigLnFact(tract.Supp(cx[i])))
		d.Sub(d, bigLnFact(tract.Supp(cs[i])))
	}
	if newBig().Abs(d).Cmp(big.NewFloat(bigTieBound)) <= 0 {
		return 0, false
	}
	return d.Sign(), true
}
