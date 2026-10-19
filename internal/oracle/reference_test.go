package oracle

import (
	"errors"
	"go/parser"
	"go/token"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// TestReferenceImportGraph checks the independence of the reference
// measures (issue #15, audit TST-3): the non-test code of this package
// and everything it imports from this module must not include the
// implementation's formula packages.
func TestReferenceImportGraph(t *testing.T) {
	t.Parallel()
	const module = "github.com/dimchansky/fpgrowth-go/"
	allowed := map[string]bool{
		module + "internal/oracle": true,
		module + "internal/tract":  true, // the Supp type
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var visit func(pkg string)
	visit = func(pkg string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		if !allowed[pkg] {
			t.Errorf("the oracle depends on %s (allowed: internal/oracle, internal/tract)", pkg)
			return
		}
		dir := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(pkg, module)))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		files := 0
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			files++
			f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if strings.HasPrefix(path, module) || path == strings.TrimSuffix(module, "/") {
					visit(path)
				}
			}
		}
		if files == 0 {
			t.Fatalf("no Go files in %s", dir)
		}
	}
	visit(module + "internal/oracle")
	if !seen[module+"internal/tract"] {
		t.Error("import walk did not reach internal/tract; is the walk broken?")
	}
}

// refEval evaluates the reference measure with letter c or fails.
func refEval(t *testing.T, c byte, s, b, h, n tract.Supp) Value {
	t.Helper()
	m, ok := RefByLetter(c)
	if !ok {
		t.Fatalf("no reference measure %c", c)
	}
	v, err := m.Eval(s, b, h, n)
	if err != nil {
		t.Fatalf("%c(%d, %d, %d, %d): %v", c, s, b, h, n, err)
	}
	return v
}

// TestReferenceTextbook checks the reference against values derived by
// hand from the definitions (or taken from standard tables), each written
// as an exact fraction or with its source.
func TestReferenceTextbook(t *testing.T) {
	t.Parallel()
	type tc struct {
		name       string
		c          byte
		s, b, h, n tract.Supp
		want       float64
		tol        float64 // 0: the reference value must be the nearest float64
	}
	r := func(p, q float64) float64 { return p / q } // p/q correctly rounded for small integers
	tests := []tc{
		// Fisher's "lady tasting tea" (Fisher 1935): 8 cups, 4 with milk
		// first, 3 of 4 identified. P(x) = C(4,x)·C(4,4-x)/70 =
		// (1, 16, 36, 16, 1)/70.
		{"tea one-sided", 's', 3, 4, 4, 8, r(17, 70), 0},
		{"tea two-sided prob", 'f', 3, 4, 4, 8, r(34, 70), 0},
		{"tea two-sided chi2", 'h', 3, 4, 4, 8, r(34, 70), 0},
		{"tea two-sided info", 'm', 3, 4, 4, 8, r(34, 70), 0},
		{"tea all identified", 's', 4, 4, 4, 8, r(1, 70), 0},
		{"tea center", 'f', 2, 4, 4, 8, 1, 0},
		// Table a=10 b=20 c=30 d=40 (n=100, rows 30/70, columns 40/60):
		// ad-bc = -200, X² = 100·200²/(30·70·40·60) = 50/63,
		// Yates X² = 100·150²/5040000 = 25/56.
		{"chi2 normalized", 'n', 10, 30, 40, 100, r(50, 6300), 0},
		{"yates normalized", 'y', 10, 30, 40, 100, r(25, 5600), 0},
		{"confidence", 'c', 10, 30, 40, 100, r(1, 3), 0},
		{"confidence diff", 'd', 10, 30, 40, 100, r(1, 15), 0},
		{"lift", 'l', 10, 30, 40, 100, r(5, 6), 0},
		{"lift diff", 'a', 10, 30, 40, 100, r(1, 6), 0},
		{"lift quotient", 'q', 10, 30, 40, 100, r(1, 6), 0},
		// conviction (1 - 0.4)/(1 - 1/3) = 9/10
		{"conviction", 'v', 10, 30, 40, 100, r(9, 10), 0},
		{"conviction diff", 'e', 10, 30, 40, 100, r(1, 10), 0},
		{"conviction quotient", 'r', 10, 30, 40, 100, r(1, 10), 0},
		{"imbalance", 'b', 10, 30, 40, 100, r(10, 60), 0},
		// Kulczynski (10/30 + 10/40)/2 = 7/24; upstream w = 2·|7/24 - 1/2| = 5/12
		{"kulczynski", 'k', 10, 30, 40, 100, r(7, 24), 0},
		{"kulczynski diff", 'w', 10, 30, 40, 100, r(5, 12), 0},
		// P(h|B) = 1/3, P(h|¬B) = 30/70: ratio 7/9
		{"cond prob ratio", 'u', 10, 30, 40, 100, r(7, 9), 0},
		{"importance", 'j', 10, 30, 40, 100, math.Log2(7.0 / 9), 1e-15},
		// certainty: conf 1/3 < prior 2/5: (1/3 - 2/5)/(2/5) = -1/6
		{"certainty below", 'z', 10, 30, 40, 100, r(-1, 6), 0},
		// conf 3/4 > prior 2/5: (3/4 - 2/5)/(3/5) = 7/12
		{"certainty above", 'z', 15, 20, 40, 100, r(7, 12), 0},
		{"support", 'o', 10, 30, 40, 100, 10, 0},
		// mutual information of s=1 b=1 h=2 n=4: H(B) + H(h) - H(B,h) =
		// h2(1/4) + 1 - 1.5 = 0.8112781244591328 - 0.5
		{"information", 'i', 1, 1, 2, 4, 0.3112781244591328, 1e-15},
		// perfect association with p = 1/2: I = 1 bit, X²/n = 1
		{"information perfect", 'i', 50, 50, 50, 100, 1, 0},
		{"chi2 perfect", 'n', 50, 50, 50, 100, 1, 0},
		// independence
		{"information independent", 'i', 6, 12, 20, 40, 0, 0},
		{"chi2 independent", 'n', 6, 12, 20, 40, 0, 0},
		{"chi2 p independent", 'p', 6, 12, 20, 40, 1, 0},
		{"G p independent", 'g', 6, 12, 20, 40, 1, 0},
		{"fisher independent prob", 'f', 6, 12, 20, 40, 1, 0},
		// G for the 50/50 perfect association: 2·100·ln 2 = 138.63,
		// p = erfc(√(100·ln 2)); chi² 100: erfc(√50)
		{"G p perfect", 'g', 50, 50, 50, 100, math.Erfc(math.Sqrt(100 * math.Ln2)), 1e-43},
		{"chi2 p perfect", 'p', 50, 50, 50, 100, math.Erfc(math.Sqrt(50)), 1e-36},
	}
	for _, tt := range tests {
		v := refEval(t, tt.c, tt.s, tt.b, tt.h, tt.n)
		ok := v.V == tt.want
		if tt.tol > 0 {
			ok = math.Abs(v.V-tt.want) <= tt.tol
		}
		if !ok {
			t.Errorf("%s: %c(%d, %d, %d, %d) = %v, want %.17g", tt.name, tt.c, tt.s, tt.b, tt.h, tt.n, v, tt.want)
		}
	}
}

// TestQ1 checks the chi²(1) tail against standard quantiles and erfc
// values (Abramowitz and Stegun, Table 26.8; NIST DLMF 7.2).
func TestQ1(t *testing.T) {
	t.Parallel()
	tests := []struct{ x, p, tol float64 }{
		{0, 1, 0},
		{3.841458820694124, 0.05, 1e-15},
		{6.634896601021214, 0.01, 1e-16},
		{10.827566170662733, 0.001, 1e-17},
		{2, 0.15729920705028513, 1e-17},    // erfc(1)
		{8, 0.004677734981047266, 1e-18},   // erfc(2)
		{18, 2.209049699858544e-05, 1e-20}, // erfc(3)
	}
	for _, tt := range tests {
		if got := q1(tt.x); math.Abs(got-tt.p) > tt.tol {
			t.Errorf("q1(%v) = %.17g, want %.17g", tt.x, got, tt.p)
		}
	}
}

// TestLnBig checks the big.Float logarithm against known constants and
// against math.Log.
func TestLnBig(t *testing.T) {
	t.Parallel()
	// ln 2 to 60 digits (OEIS A002162)
	const ln2Digits = "0.693147180559945309417232121458176568075500134360255254120680"
	want, _, _ := big.ParseFloat(ln2Digits, 10, refPrec, big.ToNearestEven)
	d := newFloat().Sub(ln2Big(), want)
	if d.Sign() != 0 && d.MantExp(nil) > -(refPrec-4) {
		t.Errorf("ln 2 = %v, want %s", ln2Big().Text('g', 60), ln2Digits)
	}
	// ln 10 (OEIS A002392)
	const ln10Digits = "2.30258509299404568401799145468436420760110148862877297603333"
	want, _, _ = big.ParseFloat(ln10Digits, 10, refPrec, big.ToNearestEven)
	d = newFloat().Sub(lnBig(newFloat().SetInt64(10)), want)
	if d.Sign() != 0 && d.MantExp(nil) > -(refPrec-6) {
		t.Errorf("ln 10 = %v, want %s", lnBig(newFloat().SetInt64(10)).Text('g', 60), ln10Digits)
	}
	for _, x := range []float64{1, 0.5, 0.7, 1.4, 3, 1e-300, 1e300, 123456789} {
		if got := float64Of(lnBig(newFloat().SetFloat64(x))); math.Abs(got-math.Log(x)) > 4.5e-16*max(1, math.Abs(math.Log(x))) {
			t.Errorf("lnBig(%v) = %v, math.Log %v", x, got, math.Log(x))
		}
	}
	// ln(1+eps) - eps ≈ -eps²/2 + eps³/3 for tiny eps
	for _, e := range []int64{1 << 40, 1 << 62, -(1 << 50)} {
		eps := big.NewRat(1, e)
		got := float64Of(log1pMinus(eps))
		x := 1 / float64(e)
		want := -x*x/2 + x*x*x/3
		if math.Abs(got-want) > 1e-15*math.Abs(want) {
			t.Errorf("log1pMinus(1/%d) = %v, want %v", e, got, want)
		}
	}
	for _, e := range []*big.Rat{big.NewRat(1, 2), big.NewRat(-1, 2), big.NewRat(3, 1), big.NewRat(-9, 10), big.NewRat(1, 3)} {
		f, _ := e.Float64()
		got := float64Of(log1pMinus(e))
		if want := math.Log1p(f) - f; math.Abs(got-want) > 1e-15 {
			t.Errorf("log1pMinus(%v) = %v, want %v", e, got, want)
		}
	}
}

// TestReferenceConventions checks the kinds and the convention values.
func TestReferenceConventions(t *testing.T) {
	t.Parallel()
	inf := math.Inf(1)
	tests := []struct {
		c          byte
		s, b, h, n tract.Supp
		v          float64
		conv       string // "" for exact
	}{
		{'o', 3, 5, 6, 10, 3, ConvALG5},
		{'w', 3, 5, 6, 10, math.Abs(2 * (0.5*(3.0/5+3.0/6) - 0.5)), ConvALG5},
		{'r', 5, 5, 6, 10, inf, ConvALG5},
		{'v', 5, 5, 6, 10, inf, ConvR4},
		{'e', 5, 5, 6, 10, inf, ConvR4},
		{'z', 5, 5, 10, 10, 0, ConvR5},
		{'z', 0, 0, 10, 10, 0, ConvUndefined},
		{'z', 0, 5, 0, 10, 0, ""},              // prior 0, confidence 0
		{'y', 2, 4, 5, 10, 0, ConvR9b},         // |ad-bc|/n = 0 < 1/2
		{'t', 2, 4, 5, 10, 1, ConvR9b},         // clamped
		{'y', 3, 4, 5, 10, 1.0 / 24, ""},       // |O-E| = 1: (1/2)²·(1/2+1/2+1/3+1/3)/n
		{'c', 0, 0, 3, 10, 0, ConvUndefined},   // empty body support
		{'l', 0, 4, 0, 10, 0, ConvUndefined},   // empty head support
		{'q', 0, 4, 3, 10, 1, ""},              // lift 0: 1 - min(0, ∞)
		{'k', 0, 0, 3, 10, inf, ConvUndefined}, // P(h|B) undefined
		{'b', 0, 0, 0, 10, inf, ConvUndefined}, // P(B or h) = 0
		{'u', 2, 4, 2, 10, inf, ConvUndefined}, // P(h|¬B) = 0
		{'u', 0, 4, 3, 10, 0, ""},              // P(h|B) = 0
		{'u', 0, 4, 0, 10, 0, ConvUndefined},   // 0/0
		{'u', 6, 10, 6, 10, 0, ConvUndefined},  // body in every transaction
		{'j', 0, 4, 3, 10, 0, ConvUndefined},   // log2(0)
		{'n', 3, 10, 3, 10, 0, ConvUndefined},  // degenerate margins
		{'p', 3, 10, 3, 10, 1, ConvUndefined},
		{'i', 3, 10, 3, 10, 0, ""}, // independent: exactly 0
		{'g', 3, 10, 3, 10, 1, ""},
		{'f', 3, 10, 3, 10, 1, ""}, // a single table
		{'s', 0, 0, 3, 10, 1, ""},
		{'e', 0, 0, 3, 10, inf, ConvUndefined},
		{'v', 0, 0, 3, 10, 0, ConvUndefined},
	}
	for _, tt := range tests {
		v := refEval(t, tt.c, tt.s, tt.b, tt.h, tt.n)
		wantKind := Exact
		if tt.conv != "" {
			wantKind = Convention
		}
		if v.Kind != wantKind || v.Conv != tt.conv || math.Abs(v.V-tt.v) > 1e-15 {
			t.Errorf("%c(%d, %d, %d, %d) = %v, want %v (%v %s)", tt.c, tt.s, tt.b, tt.h, tt.n, v, tt.v, wantKind, tt.conv)
		}
		if v.Kind == Convention && v.Tol != 0 && tt.c != 'w' {
			t.Errorf("%c(%d, %d, %d, %d): convention with tolerance %v", tt.c, tt.s, tt.b, tt.h, tt.n, v.Tol)
		}
	}
}

// TestReferenceIdentities checks the reference against other
// formulations of the same definitions on every table with n <= 14.
func TestReferenceIdentities(t *testing.T) {
	t.Parallel()
	entropy := func(ps ...float64) float64 {
		var h float64
		for _, p := range ps {
			if p > 0 {
				h -= p * math.Log2(p)
			}
		}
		return h
	}
	binom := func(n, k int64) *big.Int { return new(big.Int).Binomial(n, k) }
	for n := int64(1); n <= 14; n++ {
		for b := int64(0); b <= n; b++ {
			for h := int64(0); h <= n; h++ {
				lo, hi := max(0, b+h-n), min(b, h)
				// hypergeometric probabilities from binomial coefficients
				prob := func(x int64) *big.Rat {
					return frac(new(big.Int).Mul(binom(h, x), binom(n-h, b-x)), binom(n, b))
				}
				for s := lo; s <= hi; s++ {
					S, B, H, N := tract.Supp(s), tract.Supp(b), tract.Supp(h), tract.Supp(n)
					// closed form of chi²: n·(ad-bc)²/(r1·r2·c1·c2)
					if !(b == 0 || b == n || h == 0 || h == n) {
						d := s*n - b*h
						want := ratFloat64(big.NewRat(d*d, b*h*(n-b)*(n-h)))
						if v := refEval(t, 'n', S, B, H, N); v.V != want {
							t.Errorf("chi2(%d,%d,%d,%d) = %v, closed form %v", s, b, h, n, v.V, want)
						}
					}
					// information as H(B) + H(h) - H(B, h)
					fn := float64(n)
					mi := entropy(float64(b)/fn, float64(n-b)/fn) + entropy(float64(h)/fn, float64(n-h)/fn) -
						entropy(float64(s)/fn, float64(b-s)/fn, float64(h-s)/fn, float64(n-b-h+s)/fn)
					if v := refEval(t, 'i', S, B, H, N); math.Abs(v.V-mi) > 1e-14 {
						t.Errorf("info(%d,%d,%d,%d) = %v, entropies %v", s, b, h, n, v.V, mi)
					}
					// Fisher tests by direct summation of binomial probabilities
					ps := prob(s)
					sums := map[byte]*big.Rat{'f': new(big.Rat), 'h': new(big.Rat), 's': new(big.Rat)}
					for x := lo; x <= hi; x++ {
						px := prob(x)
						if px.Cmp(ps) <= 0 {
							sums['f'].Add(sums['f'], px)
						}
						if abs64(n*x-b*h) >= abs64(n*s-b*h) {
							sums['h'].Add(sums['h'], px)
						}
						if x >= s {
							sums['s'].Add(sums['s'], px)
						}
					}
					for c, sum := range sums {
						if v := refEval(t, c, S, B, H, N); v.V != ratFloat64(sum) {
							t.Errorf("%c(%d,%d,%d,%d) = %v, binomial sum %v", c, s, b, h, n, v.V, ratFloat64(sum))
						}
					}
				}
			}
		}
	}
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestReferenceLargeTables checks exact values for tables up to the int64
// range: the ALG-1 family s = 3k-2, b = 3k, h = 9k-2, n = 9k converges to
// the chi² p-value 0.0455003 and the G-test p-value 0.0360561 of the
// audit (docs/audit/algorithms.md), and its Fisher tests have three tables
// with the exact probabilities of the k = 1 table scaled away.
func TestReferenceLargeTables(t *testing.T) {
	t.Parallel()
	for _, k := range []int64{1e9, 1e12, 1e15, 1e18} {
		s, b, h, n := tract.Supp(3*k-2), tract.Supp(3*k), tract.Supp(9*k-2), tract.Supp(9*k)
		if v := refEval(t, 'p', s, b, h, n); math.Abs(v.V-0.0455003) > 5e-8 {
			t.Errorf("k=%d: chi2 p = %v, want 0.0455003", k, v)
		}
		if v := refEval(t, 'g', s, b, h, n); math.Abs(v.V-0.0360561) > 5e-8 {
			t.Errorf("k=%d: G p = %v, want 0.0360561", k, v)
		}
		if v := refEval(t, 'i', s, b, h, n); !(v.V > 0) {
			t.Errorf("k=%d: info = %v, want > 0", k, v)
		}
		// As k -> ∞ only the two "not head" transactions are random: the
		// table is x in [0, 2] "body and not head" out of 2, with
		// P(x) -> C(2,x)·(1/3)^x·(2/3)^(2-x) = (4, 4, 1)/9; the observed
		// x = 2 has P = 1/9, the smallest.
		if v := refEval(t, 'f', s, b, h, n); math.Abs(v.V-1.0/9) > 1e-8 {
			t.Errorf("k=%d: Fisher prob = %v, want ≈ 1/9", k, v)
		}
	}
	// perfect association near the int64 limit (M = s = b = h, n = M+3):
	// X²/n = 1; Yates: |O-E| = 3M/(M+3) ≈ 3 in every cell and the cell
	// "neither" has E = 9/(M+3), so X²_Y/n ≈ 2.5²/9 = 25/36.
	const big = tract.Supp(9223372036854775800)
	if v := refEval(t, 'n', big, big, big, big+3); v.V != 1 {
		t.Errorf("perfect association chi2 = %v, want 1", v)
	}
	if v := refEval(t, 'y', big, big, big, big+3); math.Abs(v.V-25.0/36) > 1e-15 {
		t.Errorf("perfect association yates = %v, want ≈ 25/36", v)
	}
	// certainty with confidence 1 and a prior just below 1
	if v := refEval(t, 'z', 2, 2, big-2, big); v.V != 1 {
		t.Errorf("certainty = %v, want 1", v)
	}
}

// TestReferenceErrors checks invalid tables and the Fisher scope.
func TestReferenceErrors(t *testing.T) {
	t.Parallel()
	m, _ := RefByLetter('c')
	for _, tc := range [][4]tract.Supp{{1, 0, 1, 1}, {0, 2, 1, 1}, {0, 0, 0, 0}, {-1, 1, 1, 2}, {0, 2, 2, 3}, {3, 3, 3, 2}} {
		if _, err := m.Eval(tc[0], tc[1], tc[2], tc[3]); !errors.Is(err, ErrInvalidTable) {
			t.Errorf("%v: err %v, want ErrInvalidTable", tc, err)
		}
	}
	f, _ := RefByLetter('f')
	if _, err := f.Eval(2000, 4000, 4000, 8000); !errors.Is(err, ErrOutOfScope) {
		t.Errorf("large Fisher: err %v, want ErrOutOfScope", err)
	}
	if _, err := f.Eval(500, FisherMaxTables-1, FisherMaxTables-1, 4000); err != nil {
		t.Errorf("Fisher at the limit: %v", err)
	}
	if _, ok := RefByLetter('x'); ok {
		t.Error("RefByLetter('x') found a measure")
	}
	if len(RefMeasures()) != 25 {
		t.Errorf("%d reference measures, want 25", len(RefMeasures()))
	}
	for _, rm := range RefMeasures() {
		if got, ok := RefByName(rm.Name); !ok || got != rm {
			t.Errorf("RefByName(%q) = %v, %v", rm.Name, got, ok)
		}
	}
}

// TestLogRatioValue checks the -eb reference.
func TestLogRatioValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		supp, base tract.Supp
		items      []tract.Supp
		want       float64
		kind       ValueKind
	}{
		{3, 10, []tract.Supp{5}, 0, Convention},
		{3, 10, nil, 0, Convention},
		{2, 4, []tract.Supp{2, 2}, 1, Exact},    // log2(2·4/(2·2))
		{1, 8, []tract.Supp{2, 2, 2}, 3, Exact}, // log2(1·64/8)
		{3, 12, []tract.Supp{4, 9}, 0, Exact},   // independent
		{5, 20, []tract.Supp{10, 10}, 0, Exact}, // 5·20/100 = 1
		{1, 3, []tract.Supp{1, 3}, 0, Exact},    // item in every transaction
		{7, 30, []tract.Supp{10, 15}, math.Log2(7.0 * 30 / 150), Exact},
	}
	for _, tt := range tests {
		v, err := LogRatioValue(tt.supp, tt.items, tt.base)
		if err != nil {
			t.Fatal(err)
		}
		if v.Kind != tt.kind || math.Abs(v.V-tt.want) > 1e-15 {
			t.Errorf("LogRatioValue(%d, %v, %d) = %v, want %v", tt.supp, tt.items, tt.base, v, tt.want)
		}
	}
	if _, err := LogRatioValue(0, []tract.Supp{1, 1}, 2); !errors.Is(err, ErrInvalidTable) {
		t.Errorf("support 0: err %v", err)
	}
	if _, err := LogRatioValue(3, []tract.Supp{2, 5}, 6); !errors.Is(err, ErrInvalidTable) {
		t.Errorf("item below set support: err %v", err)
	}
}

// TestRefMeasureInOracle checks the oracle with reference measures:
// tolerances, Optional at the threshold, the exact -i test and -eb.
func TestRefMeasureInOracle(t *testing.T) {
	t.Parallel()
	// a in 4 of 10, b in 5, a b in 2: lift(b <- a) = 2·10/(4·5) = 1 exactly.
	var txs []Transaction
	txs = append(txs, Transaction{Items: []string{"a", "b"}, Weight: 2},
		Transaction{Items: []string{"a"}, Weight: 2},
		Transaction{Items: []string{"b"}, Weight: 3},
		Transaction{Items: nil, Weight: 3})
	db, err := New(txs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	lift, _ := RefByLetter('l')
	p := NewRuleParams(1, 0)
	p.MinSize = 2
	p.Eval = &Measure{Ref: lift, Dir: HigherIsBetter, Threshold: 1}
	rules, err := db.Rules(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules %v, want 2", rules)
	}
	for _, r := range rules {
		if !r.Optional || r.Eval != 1 || r.EvalTol == 0 {
			t.Errorf("%v: want an optional rule with value 1 and a tolerance", r)
		}
	}
	// An implementation that drops the boundary rules and one that keeps
	// them with a value off by less than the tolerance both match.
	if d := DiffRules(rules, nil, DiffOptions{}); d != "" {
		t.Errorf("boundary rules must be optional: %s", d)
	}
	got := []Rule{rules[0]}
	got[0].Eval = 1 + 1e-15
	got[0].EvalTol, got[0].Optional = 0, false
	if d := DiffRules(rules, got, DiffOptions{}); d != "" {
		t.Errorf("value within tolerance: %s", d)
	}
	got[0].Eval = 1 + 1e-9
	if d := DiffRules(rules, got, DiffOptions{}); d == "" {
		t.Error("value outside the tolerance matched")
	}
	// -i: supp·base = 20 = head·body: invalidated exactly (worst value 0),
	// so the rules are rejected for threshold 1 (and not optional).
	p.Eval.InvalidateBelowExpected = true
	if rules, err = db.Rules(p); err != nil || len(rules) != 0 {
		t.Errorf("-i at exactly the expected support: %v, %v", rules, err)
	}
	// sets: lift 1 aggregated; AggAvg tolerance
	sp := NewSetParams(Frequent, 1)
	sp.MinSize = 2
	sp.Eval = &SetEval{Measure: Measure{Ref: lift, Dir: HigherIsBetter, Threshold: 0.5}, Agg: AggAvg}
	sets, err := db.Mine(sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 1 || sets[0].Eval != 1 || sets[0].Optional || sets[0].EvalTol == 0 {
		t.Errorf("sets %v, want {a b} with value 1", sets)
	}
	// -eb: log2(2·10/(4·5)) = 0, on the threshold 0: optional
	sp.Eval = nil
	sp.LogRatio = &LogRatioEval{Threshold: 0}
	if sets, err = db.Mine(sp); err != nil || len(sets) != 1 || !sets[0].Optional || sets[0].Eval != 0 {
		t.Errorf("-eb sets %v, %v", sets, err)
	}
	sp.LogRatio.Threshold = -1
	if sets, err = db.Mine(sp); err != nil || len(sets) != 1 || sets[0].Optional {
		t.Errorf("-eb sets %v, %v", sets, err)
	}
	// singletons get 0 (convention), which passes -1
	sp.MinSize = 1
	if sets, err = db.Mine(sp); err != nil || len(sets) != 3 {
		t.Errorf("-eb singletons %v, %v", sets, err)
	}
	// validation
	bad := []Measure{
		{Ref: lift, Func: func(_, _, _, _ tract.Supp) float64 { return 0 }, Dir: HigherIsBetter},
		{Ref: lift, Dir: LowerIsBetter},
	}
	for _, m := range bad {
		pp := NewRuleParams(1, 0)
		pp.Eval = &m
		if _, err := db.Rules(pp); err == nil {
			t.Errorf("measure %+v accepted", m)
		}
	}
	sp.Eval = &SetEval{Measure: Measure{Ref: lift, Dir: HigherIsBetter}}
	if _, err := db.Mine(sp); err == nil {
		t.Error("Eval and LogRatio together accepted")
	}
	if aboveExpected(3, 4, 5, 10) == false || aboveExpected(2, 4, 5, 10) || !aboveExpected(math.MaxInt64, math.MaxInt64-1, math.MaxInt64, math.MaxInt64) {
		t.Error("aboveExpected")
	}
}
