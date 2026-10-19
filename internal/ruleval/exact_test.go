package ruleval

import (
	"math"
	"math/big"
	"math/bits"
	"math/rand/v2"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Tests of the exact integer arithmetic of the statistical measures and
// of their bit-identity with upstream's formulas (issue #16).

// extremeTables returns valid tables near the int64 limits and with large
// products, plus random ones of every size.
func extremeTables(r *rand.Rand, count int) []table {
	const m = math.MaxInt64
	tables := []table{
		{m, m, m, m}, {m - 1, m - 1, m - 1, m}, {0, m / 2, m / 2, m}, {1, 1, 1, m},
		{m / 3, m / 2, m / 2, m}, {9223372036854775800, 9223372036854775800, 9223372036854775800, 9223372036854775803},
		{1e18 + 1, 3e18, 3e18, 9e18}, {1 << 26, 1 << 27, 1 << 27, 1 << 28}, {1<<31 - 1, 1<<31 - 1, 1<<31 - 1, 1 << 31},
	}
	for range count {
		tables = append(tables, randomTable(r, 1+r.IntN(62)))
	}
	return tables
}

func bigCrossDiff(tb table) *big.Int {
	d := new(big.Int).Mul(big.NewInt(tb.s), big.NewInt(tb.n))
	return d.Sub(d, new(big.Int).Mul(big.NewInt(tb.h), big.NewInt(tb.b)))
}

// TestCrossDiff checks crossDiff, crossDiffFloat and AboveExpected
// against big integers, including the correct rounding of D to float64.
func TestCrossDiff(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(16, 10))
	for _, tb := range extremeTables(r, 20000) {
		s, b, h, n := tract.Supp(tb.s), tract.Supp(tb.b), tract.Supp(tb.h), tract.Supp(tb.n)
		want := bigCrossDiff(tb)
		neg, hi, lo := crossDiff(s, b, h, n)
		got := new(big.Int).Lsh(new(big.Int).SetUint64(hi), 64)
		got.Or(got, new(big.Int).SetUint64(lo))
		if neg {
			got.Neg(got)
		}
		if got.Cmp(want) != 0 || neg && want.Sign() == 0 {
			t.Fatalf("crossDiff%v = %v, want %v", tb, got, want)
		}
		wf, _ := new(big.Float).SetInt(want).Float64() // nearest even
		if f := crossDiffFloat(s, b, h, n); f != wf {
			t.Errorf("crossDiffFloat%v = %v, want %v", tb, f, wf)
		}
		if AboveExpected(s, b, h, n) != (want.Sign() > 0) {
			t.Errorf("AboveExpected%v = %v, D = %v", tb, AboveExpected(s, b, h, n), want)
		}
	}
}

// TestU128Float checks the correct rounding of u128Float, including ties
// and values just around them.
func TestU128Float(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(16, 11))
	check := func(hi, lo uint64) {
		x := new(big.Int).Lsh(new(big.Int).SetUint64(hi), 64)
		x.Or(x, new(big.Int).SetUint64(lo))
		want, _ := new(big.Float).SetInt(x).Float64()
		if got := u128Float(hi, lo); got != want {
			t.Errorf("u128Float(%#x, %#x) = %v, want %v", hi, lo, got, want)
		}
	}
	for range 20000 {
		hi := r.Uint64() >> r.IntN(64)
		lo := r.Uint64()
		check(hi, lo)
		// ties: the bits below the 53 significant ones are 100...0
		l := bits.Len64(hi) + 64
		if l > 54 {
			x := new(big.Int).Lsh(new(big.Int).SetUint64(hi), 64)
			x.SetBit(x, l-54, 1)
			for i := range l - 54 {
				x.SetBit(x, i, 0)
			}
			for _, d := range []int64{-1, 0, 1} {
				y := new(big.Int).Add(x, big.NewInt(d))
				check(new(big.Int).Rsh(y, 64).Uint64(), new(big.Int).And(y, new(big.Int).SetUint64(math.MaxUint64)).Uint64())
			}
		}
	}
}

// Upstream's formulas, evaluated in double precision as ruleval.c does
// (and as this package did before issue #16).
func upstreamChi2(s, b, h, n tract.Supp) float64 {
	if marginsDegenerate(b, h, n) {
		return 0
	}
	t := float64(float64(h)*float64(b)) - float64(float64(s)*float64(n))
	return (t * t) / (float64(h) * float64(n-h) * float64(b) * float64(n-b))
}

func upstreamYates(s, b, h, n tract.Supp) float64 { // with the B10 clamp
	if marginsDegenerate(b, h, n) {
		return 0
	}
	t := math.Abs(float64(float64(h)*float64(b))-float64(float64(s)*float64(n))) - float64(0.5*float64(n))
	if t < 0 {
		t = 0
	}
	return (t * t) / (float64(h) * float64(n-h) * float64(b) * float64(n-b))
}

func upstreamCert(s, b, h, n tract.Supp) float64 { // with the B5 fix
	if b <= 0 || n <= 0 {
		return 0
	}
	p := float64(h) / float64(n)
	d := float64(s)/float64(b) - p
	if d < 0 {
		return d / p
	}
	if p >= 1 {
		return 0
	}
	return d / (1 - p)
}

// TestUpstreamFormulaIdentity checks the claims of bit-identity with
// upstream's double formulas: Chi2, Yates, their p-values and -i whenever
// both products s·n and h·b are at most 2^53, and Certainty except for
// conf > prior with n/(n-h) > certGate.
func TestUpstreamFormulaIdentity(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(16, 12))
	var tables []table
	tables = append(tables, exhaustiveTables(16)...)
	for range 100000 {
		tables = append(tables, randomTable(r, 1+r.IntN(30)))
	}
	for range 20000 { // head close to base (certainty gate)
		n := 1 + r.Int64N(1<<40)
		h := n - r.Int64N(n/512+1)
		b := 1 + r.Int64N(n)
		lo, hi := max(0, b+h-n), min(b, h)
		tables = append(tables, table{lo + r.Int64N(hi-lo+1), b, h, n})
	}
	small, gated := 0, 0
	for _, tb := range tables {
		s, b, h, n := tract.Supp(tb.s), tract.Supp(tb.b), tract.Supp(tb.h), tract.Supp(tb.n)
		if productsSmall(s, b, h, n) {
			small++
			for _, c := range []struct {
				m    Measure
				want float64
			}{
				{Chi2, upstreamChi2(s, b, h, n)},
				{Yates, upstreamYates(s, b, h, n)},
				{Chi2PValue, chi2cdfQ(float64(n)*upstreamChi2(s, b, h, n), 1)},
				{YatesPValue, chi2cdfQ(float64(n)*upstreamYates(s, b, h, n), 1)},
			} {
				if got := Eval(c.m, s, b, h, n); math.Float64bits(got) != math.Float64bits(c.want) {
					t.Errorf("%v%v = %v, upstream formula %v", c.m, tb, got, c.want)
				}
			}
			if AboveExpected(s, b, h, n) != (float64(s)*float64(n) > float64(h)*float64(b)) {
				t.Errorf("AboveExpected%v differs from upstream's double test", tb)
			}
		}
		if b > 0 && h < n && n-h < n/certGate && AboveExpected(s, b, h, n) {
			gated++
			continue
		}
		if got, want := reCert(s, b, h, n), upstreamCert(s, b, h, n); math.Float64bits(got) != math.Float64bits(want) {
			t.Errorf("Certainty%v = %v, upstream formula %v", tb, got, want)
		}
	}
	t.Logf("%d tables, %d with products <= 2^53, %d in the certainty gate", len(tables), small, gated)
}

// TestInfoBranches compares the information with the reference on tables
// whose cell deviations δ lie at the branch points of infoCell (±1/4,
// -1/2) and near 0, -1 and large values, at all sizes: relative error
// within the documented 32 ulps (the reference tolerance).
func TestInfoBranches(t *testing.T) {
	t.Parallel()
	ref, _ := oracle.RefByLetter('i')
	r := rand.New(rand.NewPCG(16, 13))
	targets := []float64{-0.999, -0.75, -0.5, -0.4999, -0.25, -0.2499, -0.1, -1e-6, 1e-6, 0.1, 0.2499, 0.25, 0.2501, 1, 30}
	worst := 0.0
	for range 4000 {
		n := int64(1) << (4 + r.IntN(59))
		n += r.Int64N(n)
		n = min(n, math.MaxInt64)
		b := 1 + r.Int64N(n-1)
		h := 1 + r.Int64N(n-1)
		e := float64(b) * (float64(h) / float64(n))
		s := int64(e * (1 + targets[r.IntN(len(targets))]))
		lo, hi := max(0, b+h-n), min(b, h)
		s = min(max(s, lo), hi)
		v, err := ref.Eval(tract.Supp(s), tract.Supp(b), tract.Supp(h), tract.Supp(n))
		if err != nil {
			t.Fatal(err)
		}
		got := Eval(Info, tract.Supp(s), tract.Supp(b), tract.Supp(h), tract.Supp(n))
		if !v.Matches(got) {
			t.Errorf("Info(%d, %d, %d, %d) = %.17g, reference %v", s, b, h, n, got, v)
		}
		if v.V > 0 {
			worst = max(worst, math.Abs(got-v.V)/v.V)
		}
	}
	for d := -0.3; d <= 0.3; d += 1.0 / 64 { // gSeries against the direct form
		want := float64(1+d)*math.Log1p(d) - d
		if got := gSeries(d); math.Abs(got-want) > 64*epsilon*want+1e-300 {
			t.Errorf("gSeries(%v) = %v, direct %v", d, got, want)
		}
	}
	t.Logf("largest relative error %.3g (%.3g ulps)", worst, worst/epsilon)
}

// TestPinnedBits pins the bit patterns of values whose computation uses
// only IEEE arithmetic (no math.Log/Exp, whose results the Go math package
// may compute differently per architecture): the rounding of every
// product is explicit, so FMA contraction (allowed by the Go spec, used
// on arm64 and others) must not change them on any platform.
func TestPinnedBits(t *testing.T) {
	t.Parallel()
	const m = 9223372036854775800
	for _, c := range []struct {
		name string
		got  float64
		want uint64
	}{
		{"Chi2 perfect association", Eval(Chi2, m, m, m, m+3), 0x3ff0000000000000},
		{"Chi2 ALG-1 k=1e15", Eval(Chi2, 2999999999999998, 3e15, 8999999999999998, 9e15), 0x3cc00346c622f730},
		{"Yates ALG-1 k=1e15", Eval(Yates, 2999999999999998, 3e15, 8999999999999998, 9e15), 0x3ca9051e9596a23c},
		{"Certainty near 1", Eval(Certainty, 2, 2, m-2, m), 0x3ff0000000000000},
		{"gSeries(0.2)", gSeries(0.2), 0x3f933c9a455f3e09},
		{"gSeries(-0.2)", gSeries(-0.2), 0x3f9600349c228bf8},
		{"stirSeries(17)", stirSeries(17), 0x3f74137c74da3631},
		{"ratioUp", (&hyperTable{ft: fisherTable{body: 1 << 40, head: 3 << 40, rest: 5 << 40}}).ratioUp(1 << 38), 0x3ff92492492428ed},
	} {
		if bits := math.Float64bits(c.got); bits != c.want {
			t.Errorf("%s = %v (%#x), pinned %v (%#x)", c.name, c.got, bits, math.Float64frombits(c.want), c.want)
		}
	}
}

// FuzzMeasuresMatchReference maps four arbitrary integers to a valid
// table (any total weight up to 2^63 - 1) and compares every measure with
// the reference within its documented bound (the Fisher tests only up to
// maxFisherTables tables).
func FuzzMeasuresMatchReference(f *testing.F) {
	for _, tb := range [][4]uint64{{2, 4, 3, 6}, {12, 13, 48, 52}, {2999999999999998, 3e15, 8999999999999998, 9e15},
		{1e18 + 1, 3e18, 3e18, 9e18}, {math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64}} {
		f.Add(tb[0], tb[1], tb[2], tb[3])
	}
	f.Fuzz(func(t *testing.T, s0, b0, h0, n0 uint64) {
		n := int64(n0%math.MaxInt64) + 1
		b, h := int64(b0%uint64(n+1)), int64(h0%uint64(n+1))
		lo, hi := max(0, b-(n-h)), min(b, h)
		s := lo + int64(s0%(uint64(hi-lo)+1))
		for _, rm := range oracle.RefMeasures() {
			r := compare(t, rm, table{s, b, h, n})
			if !r.skipped && !r.ok {
				t.Errorf("%s(%c) s=%d b=%d h=%d n=%d: got %.17g, reference %v", rm.Name, rm.Letter, s, b, h, n, r.got, r.want)
			}
		}
	})
}
