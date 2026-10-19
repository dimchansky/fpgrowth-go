package ruleval

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// exactFisherResult holds the four Fisher p-values as exact rationals.
type exactFisherResult struct {
	prob, chi2, info, supp *big.Rat
}

// exactFisher is an independent reference implementation of the four
// Fisher tests on the original (not normalized) table, in exact rational
// arithmetic. X, the support of body and head, ranges over
// [max(0, b+h-n), min(b, h)] with the hypergeometric probabilities
// P(x) = C(h, x)·C(n-h, b-x)/C(n, b). The tests sum P(x) over
//
//	prob: P(x) <= P(s)
//	chi2: |x - E| >= |s - E|, E = h·b/n
//	info: I(x) >= I(s), mutual information, i.e. prod t^t >= over the cells
//	supp: x >= s
//
// and are 1 for tables with a vanishing margin.
func exactFisher(s, b, h, n int64) exactFisherResult {
	if h <= 0 || h >= n || b <= 0 || b >= n {
		one := big.NewRat(1, 1)
		return exactFisherResult{one, one, one, one}
	}
	lo, hi := max(0, b+h-n), min(b, h)
	denom := new(big.Int).Binomial(n, b)
	prob := func(x int64) *big.Rat {
		num := new(big.Int).Binomial(h, x)
		num.Mul(num, new(big.Int).Binomial(n-h, b-x))
		return new(big.Rat).SetFrac(num, denom)
	}
	dist := func(x int64) int64 { // |x·n - h·b| = n·|x - E|
		d := x*n - h*b
		if d < 0 {
			return -d
		}
		return d
	}
	ps, ds, fs := prob(s), dist(s), selfPowers(s, b, h, n)
	r := exactFisherResult{new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)}
	for x := lo; x <= hi; x++ {
		p := prob(x)
		if p.Cmp(ps) <= 0 {
			r.prob.Add(r.prob, p)
		}
		if dist(x) >= ds {
			r.chi2.Add(r.chi2, p)
		}
		if selfPowers(x, b, h, n).Cmp(fs) >= 0 {
			r.info.Add(r.info, p)
		}
		if x >= s {
			r.supp.Add(r.supp, p)
		}
	}
	return r
}

// selfPowers returns prod t^t over the four cells of table x.
func selfPowers(x, b, h, n int64) *big.Int {
	p := big.NewInt(1)
	for _, c := range []int64{x, b - x, h - x, n - b - h + x} {
		p.Mul(p, new(big.Int).Exp(big.NewInt(c), big.NewInt(c), nil))
	}
	return p
}

func ratFloat(r *big.Rat) float64 {
	f, _ := r.Float64()
	return f
}

// TestFisherExact compares the four Fisher tests with exact rational
// arithmetic on every valid table with base <= 24, and on random tables
// with larger bases. The tolerance (1e-12) is far below the smallest
// probability of a single table (> 3.7e-7 for base <= 24), so a table
// that is summed wrongly cannot go unnoticed.
func TestFisherExact(t *testing.T) {
	t.Parallel()
	maxBase := tract.Supp(24)
	if testing.Short() {
		maxBase = 14
	}
	check := func(s, b, h, n tract.Supp) {
		want := exactFisher(int64(s), int64(b), int64(h), int64(n))
		for _, c := range []struct {
			m    Measure
			want *big.Rat
		}{
			{FisherProb, want.prob},
			{FisherChi2, want.chi2},
			{FisherInfo, want.info},
			{FisherSupport, want.supp},
		} {
			got, exp := Eval(c.m, s, b, h, n), ratFloat(c.want)
			if math.Abs(got-exp) > 1e-12 {
				t.Errorf("Eval(%v, %d, %d, %d, %d) = %.17g, exact %.17g", c.m, s, b, h, n, got, exp)
			}
		}
	}
	forEachValidTable(maxBase, check)
	rng := splitmix(7)
	for range 300 {
		n := tract.Supp(25 + rng.intn(176))
		b := tract.Supp(rng.intn(int64(n) + 1))
		h := tract.Supp(rng.intn(int64(n) + 1))
		if rng.next()&1 == 0 { // exercise tied tables: a margin of n/2
			n &^= 1
			h = n / 2
			b = min(b, n)
		}
		lo, hi := max(0, b+h-n), min(b, h)
		check(lo+tract.Supp(rng.intn(int64(hi-lo)+1)), b, h, n)
	}
}

// TestFisherTiesRegression covers the reproducers of bugs B6, B7 and B9
// (docs/upstream/rules-eval.md).
func TestFisherTiesRegression(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		m                Measure
		s, b, h, n       tract.Supp
		upstream, wanted float64
	}{
		// B6: the tables x = 1 and x = 2 both have probability 10/21.
		{"prob tie at the mode", FisherProb, 1, 2, 5, 7, 0.5238095238095235, 1},
		// B6/B7: the observed table is independent (I = 0) and excluded
		// itself upstream; the other two tables tie with each other.
		{"info observed table", FisherInfo, 1, 2, 3, 6, 0.40000000000000013, 1},
		// B9 clamp: the sum of all three tables exceeds 1 upstream.
		{"prob sum above one", FisherProb, 1, 2, 3, 6, 1.0000000000000004, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Eval(tt.m, tt.s, tt.b, tt.h, tt.n)
			if math.Abs(got-tt.wanted) > 1e-12 || got > 1 {
				t.Errorf("Eval(%v, %d, %d, %d, %d) = %.17g, want %.17g (upstream %.17g)",
					tt.m, tt.s, tt.b, tt.h, tt.n, got, tt.wanted, tt.upstream)
			}
			if up := upstreamFisher(tt.m, tt.s, tt.b, tt.h, tt.n); up != tt.upstream {
				t.Errorf("upstream replica gives %.17g, want %.17g (the regression case does not reproduce)", up, tt.upstream)
			}
		})
	}
}

func TestFisherComplementClamp(t *testing.T) {
	t.Parallel()
	// B9: upstream returns 9.8e-9 (no FMA) or -2.0e-8 (FMA) for s; the
	// exact upper tail is below 1e-300.
	for _, m := range []Measure{FisherProb, FisherChi2, FisherInfo, FisherSupport} {
		got := Eval(m, 7969403, 8875339, 7982035, 8893601)
		if got < 0 || got > 1e-7 {
			t.Errorf("Eval(%v, B9 table) = %v, want in [0, 1e-7]", m, got)
		}
	}
	if got := clampUnit(-2e-8); got != 0 {
		t.Errorf("clampUnit(-2e-8) = %v", got)
	}
	if got := clampUnit(1.0000000000000004); got != 1 {
		t.Errorf("clampUnit(1+4ulp) = %v", got)
	}
}

// upstreamFisher is a literal port of upstream's re_fetprob and
// re_fetinfo (without the B6/B9 fixes); the other two tests delegate to
// the package (which differs from upstream only by the clamp).
func upstreamFisher(m Measure, supp, body, head, base tract.Supp) float64 {
	if marginsDegenerate(body, head, base) {
		return 1
	}
	t := newFisherTable(supp, body, head, base)
	var sum float64
	switch m {
	case FisherProb:
		cut := t.logProb(t.supp)
		cut *= 1.0 - epsilon
		for x := tract.Supp(0); x <= t.body; x++ {
			if p := t.logProb(x); p <= cut {
				sum += math.Exp(p)
			}
		}
	case FisherInfo:
		cut := infoRaw(t.supp, t.body, t.head, t.base) * (1.0 - epsilon)
		for x := tract.Supp(0); x <= t.body; x++ {
			if infoRaw(x, t.body, t.head, t.base) >= cut {
				sum += math.Exp(t.logProb(x))
			}
		}
	default:
		return Eval(m, supp, body, head, base)
	}
	return sum
}

func TestCmpRangeProducts(t *testing.T) {
	t.Parallel()
	brute := func(l, a1, a2, c1, c2 int64) int {
		var x, y, f big.Int
		x.MulRange(a1, a1+l-1)
		x.Mul(&x, f.MulRange(a2, a2+l-1))
		y.MulRange(c1, c1+l-1)
		y.Mul(&y, f.MulRange(c2, c2+l-1))
		return x.Cmp(&y)
	}
	for l := int64(1); l <= 6; l++ {
		for a1 := int64(1); a1 <= 9; a1++ {
			for a2 := int64(1); a2 <= 9; a2++ {
				for c1 := int64(1); c1 <= 9; c1++ {
					for c2 := int64(1); c2 <= 9; c2++ {
						if got, want := cmpRangeProducts(l, a1, a2, c1, c2), brute(l, a1, a2, c1, c2); got != want {
							t.Fatalf("cmpRangeProducts(%d, %d, %d, %d, %d) = %d, want %d", l, a1, a2, c1, c2, got, want)
						}
					}
				}
			}
		}
	}
	// Long ranges: the same pair in the other order ties.
	if got := cmpRangeProducts(100000, 2, 1000000, 1000000, 2); got != 0 {
		t.Errorf("swapped ranges: %d, want 0", got)
	}
	// Nested ranges (the case that needs big integers).
	if got, want := cmpRangeProducts(4, 1, 9, 3, 7), brute(4, 1, 9, 3, 7); got != want {
		t.Errorf("nested: %d, want %d", got, want)
	}
}

func TestCmpXLogX(t *testing.T) {
	t.Parallel()
	brute := func(u, v [4]int64) int {
		pu, pv := big.NewInt(1), big.NewInt(1)
		for i := range 4 {
			pu.Mul(pu, new(big.Int).Exp(big.NewInt(u[i]), big.NewInt(u[i]), nil))
			pv.Mul(pv, new(big.Int).Exp(big.NewInt(v[i]), big.NewInt(v[i]), nil))
		}
		return pu.Cmp(pv)
	}
	sorted := func(c [4]int64) [4]int64 {
		slices.Sort(c[:])
		return c
	}
	// All pairs of tables with equal margins and base <= 30.
	for n := int64(2); n <= 30; n++ {
		for b := int64(1); b < n; b++ {
			for h := b; h < n; h++ {
				lo, hi := max(0, b+h-n), min(b, h)
				for x := lo; x <= hi; x++ {
					for y := lo; y <= hi; y++ {
						u := sorted([4]int64{x, b - x, h - x, n - b - h + x})
						v := sorted([4]int64{y, b - y, h - y, n - b - h + y})
						if got, want := cmpXLogX(u, v), brute(u, v); got != want {
							t.Fatalf("cmpXLogX(%v, %v) = %d, want %d", u, v, got, want)
						}
					}
				}
			}
		}
	}
	// Large cells, checked against 320-bit logarithms (cmpXLogXBigLog).
	// Tables of one margin family differ by +-d per cell; the differences
	// of sum t·ln t are then about d²/t, far below the float64 resolution
	// of the sums themselves.
	for _, c := range [][2][4]int64{
		{{1, 0, 61728394, 61728394}, {0, 1, 61728393, 61728395}},
		{{1000000, 2000000, 3000000, 4000000}, {1000001, 1999999, 2999999, 4000001}},
		{{999, 1000, 4000000, 5000000}, {1000, 999, 4000001, 4999999}},
		{{12345, 54321, 99999999, 77777777}, {12346, 54320, 99999998, 77777778}},
	} {
		u, v := sorted(c[0]), sorted(c[1])
		want, ok := cmpXLogXBigLog(u, v)
		if !ok {
			t.Fatalf("cmpXLogXBigLog(%v, %v) undecided", u, v)
		}
		if got := cmpXLogX(u, v); got != want {
			t.Errorf("cmpXLogX(%v, %v) = %d, want %d", u, v, got, want)
		}
		if got := cmpXLogX(v, u); got != -want {
			t.Errorf("cmpXLogX(%v, %v) = %d, want %d", v, u, got, -want)
		}
	}
	// Equal multisets tie without any arithmetic.
	big1 := [4]int64{1 << 40, 1 << 41, 3 << 40, 5 << 40}
	if got := cmpXLogX(big1, big1); got != 0 {
		t.Errorf("cmpXLogX(equal) = %d", got)
	}
}

// fisherOracle returns the value of FisherProb or FisherInfo for a table
// whose upstream result depends on rounding. For small bases it is the
// exact rational result. Otherwise every table whose computed value is
// within a generous width of the observed one (2^30 times the rounding
// error bound) is decided exactly in big integer arithmetic on the
// original table, and the probabilities of the selected tables are summed
// in the same order as the implementation does, so the result must match
// bit for bit.
func fisherOracle(t *testing.T, m Measure, s, b, h, n tract.Supp, got float64) float64 {
	t.Helper()
	if n <= 64 {
		want := exactFisher(int64(s), int64(b), int64(h), int64(n))
		exact := ratFloat(want.prob)
		if m == FisherInfo {
			exact = ratFloat(want.info)
		}
		if math.Abs(got-exact) <= 1e-12 {
			return got
		}
		return exact
	}
	ft := newFisherTable(s, b, h, n)
	shift := min(n-h-b, 0) // normalized x' = original x + shift
	var sum float64
	switch m {
	case FisherProb:
		ps := ft.logProb(ft.supp)
		width := 0x1p-30 * (9*logGamma(float64(n)+1) + 1)
		for x := tract.Supp(0); x <= ft.body; x++ {
			p := ft.logProb(x)
			in := p < ps || x == ft.supp
			if x != ft.supp && math.Abs(p-ps) <= width {
				in = cmpWeightBrute(int64(x-shift), int64(s), int64(b), int64(h), int64(n)) >= 0
			}
			if in {
				sum += math.Exp(p)
			}
		}
	case FisherInfo:
		is := infoRaw(ft.supp, ft.body, ft.head, ft.base)
		width := 0x1p-30 * (1 + math.Log(float64(n)))
		for x := tract.Supp(0); x <= ft.body; x++ {
			ix := infoRaw(x, ft.body, ft.head, ft.base)
			in := ix > is || x == ft.supp
			if x != ft.supp && math.Abs(ix-is) <= width {
				xo := int64(x - shift)
				if ft.cells(x) == ft.cells(ft.supp) { // same cells, permuted
					sum += math.Exp(ft.logProb(x))
					continue
				}
				if selfPowerBits(ft.cells(x)) <= 1<<20 {
					in = selfPowers(xo, int64(b), int64(h), int64(n)).Cmp(selfPowers(int64(s), int64(b), int64(h), int64(n))) >= 0
				} else {
					c, ok := cmpXLogXBigLog(ft.cells(x), ft.cells(ft.supp))
					if !ok {
						t.Fatalf("fisherOracle(%v, %d, %d, %d, %d): cannot decide table %d", m, s, b, h, n, xo)
					}
					in = c >= 0
				}
			}
			if in {
				sum += math.Exp(ft.logProb(x))
			}
		}
	}
	return clampUnit(sum)
}

// cmpWeightBrute returns the sign of W(x) - W(s) on the original table,
// W(x) = x!·(b-x)!·(h-x)!·(n-b-h+x)!, from the full products of the
// factorial quotients.
func cmpWeightBrute(x, s, b, h, n int64) int {
	r := n - b - h
	var num, den, f big.Int
	if x > s {
		num.MulRange(s+1, x)
		num.Mul(&num, f.MulRange(r+s+1, r+x))
		den.MulRange(b-x+1, b-s)
		den.Mul(&den, f.MulRange(h-x+1, h-s))
	} else {
		num.MulRange(b-s+1, b-x)
		num.Mul(&num, f.MulRange(h-s+1, h-x))
		den.MulRange(x+1, s)
		den.Mul(&den, f.MulRange(r+x+1, r+s))
	}
	return num.Cmp(&den)
}

// cmpXLogXBigLog compares sum u_i·ln u_i with sum v_i·ln v_i using 320-bit
// logarithms (an independent check of cmpXLogX for large cells). ok is
// false if the difference is too small to decide.
func cmpXLogXBigLog(u, v [4]int64) (c int, ok bool) {
	const prec = 320
	sum := func(cells [4]int64) *big.Float {
		s := new(big.Float).SetPrec(prec)
		for _, t := range cells {
			if t > 1 {
				s.Add(s, new(big.Float).SetPrec(prec).Mul(new(big.Float).SetInt64(t), bigLn(t, prec)))
			}
		}
		return s
	}
	fu, fv := sum(u), sum(v)
	d := new(big.Float).SetPrec(prec).Sub(fu, fv)
	// Each term has a relative error of a few 2^-prec; 2^-(prec-40) covers them.
	bound := new(big.Float).SetPrec(prec).SetMantExp(new(big.Float).Add(fu, fv), 40-prec)
	if new(big.Float).Abs(d).Cmp(bound) <= 0 {
		return 0, false
	}
	return d.Sign(), true
}

// bigLn returns ln(x) for x >= 1 at the given precision: with
// x = m·2^k, 1 <= m < 2, ln x = k·ln 2 + ln m and ln y = 2·atanh((y-1)/(y+1)).
func bigLn(x int64, prec uint) *big.Float {
	wp := prec + 32
	atanh2 := func(y *big.Float) *big.Float { // 2·atanh((y-1)/(y+1))
		one := new(big.Float).SetPrec(wp).SetInt64(1)
		z := new(big.Float).SetPrec(wp).Quo(new(big.Float).SetPrec(wp).Sub(y, one), new(big.Float).SetPrec(wp).Add(y, one))
		z2 := new(big.Float).SetPrec(wp).Mul(z, z)
		term := new(big.Float).SetPrec(wp).Set(z)
		sum := new(big.Float).SetPrec(wp).Set(z)
		eps := new(big.Float).SetPrec(wp).SetMantExp(one, -int(wp))
		for k := int64(3); ; k += 2 {
			term.Mul(term, z2)
			q := new(big.Float).SetPrec(wp).Quo(term, new(big.Float).SetInt64(k))
			sum.Add(sum, q)
			if new(big.Float).Abs(q).Cmp(eps) < 0 {
				break
			}
		}
		return sum.Mul(sum, new(big.Float).SetInt64(2))
	}
	ln2 := atanh2(new(big.Float).SetPrec(wp).SetInt64(2))
	m := new(big.Float).SetPrec(wp).SetInt64(x)
	k := m.MantExp(m) - 1 // x = (2·mant)·2^(e-1)
	m.SetMantExp(m, 1)
	r := atanh2(m)
	return r.Add(r, new(big.Float).SetPrec(wp).Mul(ln2, new(big.Float).SetInt64(int64(k))))
}

// TestFisherLargeTiedTable checks that exact tie resolution stays cheap
// for a large table where every table has a tied partner (head = base/2):
// the ties are recognized from the cells, without big integers.
func TestFisherLargeTiedTable(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("sums 50001 tables per test")
	}
	const n, b, h = 200000, 50000, 100000
	for _, s := range []tract.Supp{25000, 25100, 24000} {
		f := Eval(FisherProb, s, b, h, n)
		m := Eval(FisherInfo, s, b, h, n)
		c := Eval(FisherChi2, s, b, h, n)
		// With a symmetric distribution, all three two-sided tests sum the
		// same two tails (table probability, information and |x - E| all
		// order the tables alike here), so they must agree closely.
		if math.Abs(f-c) > 1e-9 || math.Abs(m-c) > 1e-9 {
			t.Errorf("s=%d: FisherProb %v, FisherInfo %v, FisherChi2 %v", s, f, m, c)
		}
	}
}

// TestFisherInvalidTables checks that tables that are not realizable
// (unspecified results) do not panic.
func TestFisherInvalidTables(t *testing.T) {
	t.Parallel()
	for _, tb := range [][4]tract.Supp{{0, 8, 8, 10}, {5, 3, 4, 10}, {-1, 3, 4, 10}, {3, 3, 4, 2}, {2, 3, 4, 0}} {
		for _, m := range []Measure{FisherProb, FisherChi2, FisherInfo, FisherSupport} {
			Eval(m, tb[0], tb[1], tb[2], tb[3])
		}
	}
}

// TestAbortFuncMatches checks that the abortable variants of the Fisher
// measures compute bit-identical values with a nil abort function and
// with one that never fails, over the upstream fixture tables and random
// tables (including ones with more than fisherAbortEvery tables), and
// that the other measures have no abortable variant.
func TestAbortFuncMatches(t *testing.T) {
	t.Parallel()
	never := func() error { return nil }
	type table struct{ supp, body, head, base tract.Supp }
	var tables []table
	for _, r := range loadUpstreamRows(t) {
		if r.has[FisherProb] && r.has[FisherChi2] && r.has[FisherInfo] && r.has[FisherSupport] { // cheap ones only
			tables = append(tables, table{r.supp, r.body, r.head, r.base})
		}
	}
	rng := rand.New(rand.NewPCG(7, 11))
	for range 300 {
		base := tract.Supp(rng.Int64N(40_000) + 1)
		body := rng.Int64N(int64(base) + 1)
		head := rng.Int64N(int64(base) + 1)
		lo := max(0, body+head-int64(base))
		supp := lo + rng.Int64N(min(body, head)-lo+1)
		tables = append(tables, table{tract.Supp(supp), tract.Supp(body), tract.Supp(head), base})
	}
	for m := range NumMeasures {
		afn := m.AbortFunc()
		isFisher := m >= FisherProb && m <= FisherSupport
		if (afn != nil) != isFisher {
			t.Fatalf("%v: AbortFunc nil = %v", m, afn == nil)
		}
		if afn == nil {
			continue
		}
		fn := m.Func()
		for _, tb := range tables {
			want := fn(tb.supp, tb.body, tb.head, tb.base)
			for _, abort := range []func() error{nil, never} {
				got, err := afn(tb.supp, tb.body, tb.head, tb.base, abort)
				if err != nil || math.Float64bits(got) != math.Float64bits(want) {
					t.Fatalf("%v%v: %v, %v; want %v", m, tb, got, err, want)
				}
			}
		}
	}
}

// TestAbortFuncStops checks that a failing abort function stops a long
// Fisher evaluation after fisherAbortEvery steps, returning its error and
// NaN. The table is at independence with balanced margins and n = 10^8,
// so every test sums about 9 standard deviations (σ = 2500) of tables on
// each side of the mode, more than 3·fisherAbortEvery.
func TestAbortFuncStops(t *testing.T) {
	t.Parallel()
	errStop := errors.New("stop")
	for _, m := range []Measure{FisherProb, FisherChi2, FisherInfo, FisherSupport} {
		calls := 0
		abort := func() error {
			calls++
			if calls == 3 {
				return errStop
			}
			return nil
		}
		start := time.Now()
		v, err := m.AbortFunc()(25_000_000, 50_000_000, 50_000_000, 100_000_000, abort)
		if !errors.Is(err, errStop) || !math.IsNaN(v) || calls != 3 {
			t.Errorf("%v: %v, %v after %d abort calls", m, v, err, calls)
		}
		if d := time.Since(start); d > time.Second {
			t.Errorf("%v: stopped after %v", m, d)
		}
	}
}
