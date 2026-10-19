package ruleval

import (
	"bufio"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// helpText is the measure list printed by upstream's re_help, captured
// from `fpgrowth -!` (FP-Growth 6.21). Lines end in spaces for 'x'.
const helpText = "" +
	"  x   no measure                                      \n" +
	"  o   rule support (original def.: body & head)       (+)\n" +
	"  c   rule confidence                                 (+)\n" +
	"  d   absolute confidence difference to prior         (+)\n" +
	"  l   lift value (confidence divided by prior)        (+)\n" +
	"  a   absolute difference of lift value to 1          (+)\n" +
	"  q   difference of lift quotient to 1                (+)\n" +
	"  v   conviction (inverse lift for negated head)      (+)\n" +
	"  e   absolute difference of conviction to 1          (+)\n" +
	"  r   difference of conviction quotient to 1          (+)\n" +
	"  b   imbalance ratio                                 (+)\n" +
	"  k   Kulczynski measure                              (+)\n" +
	"  w   difference of Kulczynski measure to 0.5         (+)\n" +
	"  u   conditional probability ratio                   (+)\n" +
	"  j   importance (binary log. of cond. prob. ratio)   (+)\n" +
	"  z   certainty factor (relative confidence change)   (+)\n" +
	"  n   normalized chi^2 measure                        (+)\n" +
	"  p   p-value from (unnormalized) chi^2 measure       (-)\n" +
	"  y   normalized chi^2 measure with Yates' correction (+)\n" +
	"  t   p-value from Yates-corrected chi^2 measure      (-)\n" +
	"  i   information difference to prior                 (+)\n" +
	"  g   p-value from G statistic/information difference (-)\n" +
	"  f   Fisher's exact test (table probability)         (-)\n" +
	"  h   Fisher's exact test (chi^2 measure)             (-)\n" +
	"  m   Fisher's exact test (information gain)          (-)\n" +
	"  s   Fisher's exact test (support)                   (-)\n"

func TestByLetter(t *testing.T) {
	t.Parallel()
	seen := map[Measure]byte{}
	for c := byte('a'); c <= 'z'; c++ {
		m, ok := ByLetter(c)
		if !ok {
			t.Fatalf("ByLetter(%q) not ok", c)
		}
		if prev, dup := seen[m]; dup {
			t.Errorf("ByLetter(%q) = %v, also selected by %q", c, m, prev)
		}
		seen[m] = c
		if m.Letter() != c {
			t.Errorf("ByLetter(%q) = %v with Letter() %q", c, m, m.Letter())
		}
	}
	if len(seen) != int(NumMeasures) {
		t.Errorf("letters select %d measures, want %d", len(seen), NumMeasures)
	}
	for _, c := range []byte{0, 'A', 'Z', '`', '{', '0', 0xff} {
		if m, ok := ByLetter(c); ok || m != None {
			t.Errorf("ByLetter(%q) = %v, %v; want None, false", c, m, ok)
		}
	}
	if m, _ := ByLetter('b'); m != Imbalance {
		t.Errorf("ByLetter('b') = %v, want Imbalance (re_getid)", m)
	}
}

func TestMeasureIDs(t *testing.T) {
	t.Parallel()
	// The values must equal upstream's RE_* identifiers (ruleval.h).
	ids := map[Measure]int{
		None: 0, Support: 1, Confidence: 2, ConfidenceDiff: 3, Lift: 4,
		LiftDiff: 5, LiftQuotient: 6, Conviction: 7, ConvictionDiff: 8,
		ConvictionQuotient: 9, Imbalance: 10, Kulczynski: 11,
		KulczynskiDiff: 12, CondProbRatio: 13, Importance: 14,
		Certainty: 15, Chi2: 16, Chi2PValue: 17, Yates: 18,
		YatesPValue: 19, Info: 20, InfoPValue: 21, FisherProb: 22,
		FisherChi2: 23, FisherInfo: 24, FisherSupport: 25,
		NumMeasures: 26,
	}
	for m, id := range ids {
		if int(m) != id {
			t.Errorf("%v = %d, want %d", m, int(m), id)
		}
	}
}

func TestMeasureAccessors(t *testing.T) {
	t.Parallel()
	for m := range NumMeasures {
		if !m.IsValid() {
			t.Errorf("%v.IsValid() = false", m)
		}
		if m.Func() == nil {
			t.Errorf("%v.Func() = nil", m)
		}
		want := 1
		switch m {
		case None:
			want = 0
		case Chi2PValue, YatesPValue, InfoPValue, FisherProb, FisherChi2, FisherInfo, FisherSupport:
			want = -1
		}
		if got := m.Dir(); got != want {
			t.Errorf("%v.Dir() = %d, want %d", m, got, want)
		}
		if m.Description() == "" || strings.HasPrefix(m.String(), "Measure(") {
			t.Errorf("%v: empty description or name", m)
		}
	}
	if NumMeasures.IsValid() {
		t.Error("NumMeasures.IsValid() = true")
	}
	if got := Measure(200).String(); got != "Measure(200)" {
		t.Errorf("Measure(200).String() = %q", got)
	}
	if got := FisherInfo.String(); got != "FisherInfo" {
		t.Errorf("FisherInfo.String() = %q", got)
	}
}

func TestInvalidMeasurePanics(t *testing.T) {
	t.Parallel()
	calls := map[string]func(){
		"Eval":        func() { Eval(NumMeasures, 1, 1, 1, 1) },
		"Func":        func() { NumMeasures.Func() },
		"AbortFunc":   func() { NumMeasures.AbortFunc() },
		"Dir":         func() { Measure(255).Dir() },
		"Letter":      func() { Measure(27).Letter() },
		"Description": func() { Measure(26).Description() },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Errorf("%s with an invalid measure did not panic", name)
				}
			}()
			call()
		})
	}
}

func TestWriteHelp(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	if err := WriteHelp(&sb); err != nil {
		t.Fatal(err)
	}
	if got := sb.String(); got != helpText {
		t.Errorf("WriteHelp output differs from upstream re_help:\ngot:\n%s\nwant:\n%s", got, helpText)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestWriteHelpError(t *testing.T) {
	t.Parallel()
	if err := WriteHelp(failWriter{}); err != os.ErrClosed {
		t.Errorf("WriteHelp error = %v, want %v", err, os.ErrClosed)
	}
}

func TestEvalMatchesFunc(t *testing.T) {
	t.Parallel()
	for m := range NumMeasures {
		got, want := Eval(m, 3, 5, 6, 12), m.Func()(3, 5, 6, 12)
		if math.Float64bits(got) != math.Float64bits(want) {
			t.Errorf("Eval(%v) = %v, Func() gives %v", m, got, want)
		}
	}
}

// forEachValidTable calls fn for every valid table with 1 <= base <= maxBase.
func forEachValidTable(maxBase tract.Supp, fn func(s, b, h, n tract.Supp)) {
	for n := tract.Supp(1); n <= maxBase; n++ {
		for b := tract.Supp(0); b <= n; b++ {
			for h := tract.Supp(0); h <= n; h++ {
				for s := max(0, b+h-n); s <= min(b, h); s++ {
					fn(s, b, h, n)
				}
			}
		}
	}
}

// randomTables returns count valid tables with bases up to 2^maxBits-1,
// from a fixed seed.
func randomTables(count, maxBits int, seed uint64) [][4]tract.Supp {
	rng := splitmix(seed)
	tables := make([][4]tract.Supp, 0, count)
	for range count {
		k := 1 + rng.intn(int64(maxBits))
		n := tract.Supp(int64(1)<<(k-1) + rng.intn(int64(1)<<(k-1)))
		b := tract.Supp(rng.intn(int64(n) + 1))
		h := tract.Supp(rng.intn(int64(n) + 1))
		lo, hi := max(0, b+h-n), min(b, h)
		s := lo + tract.Supp(rng.intn(int64(hi-lo)+1))
		tables = append(tables, [4]tract.Supp{s, b, h, n})
	}
	return tables
}

// splitmix is a deterministic pseudo-random generator for test data.
type splitmix uint64

func (r *splitmix) next() uint64 {
	*r += 0x9e3779b97f4a7c15
	z := uint64(*r)
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// intn returns a pseudo-random integer in [0, n).
func (r *splitmix) intn(n int64) int64 { return int64(r.next() % uint64(n)) }

func TestNoNaNForValidTables(t *testing.T) {
	t.Parallel()
	check := func(s, b, h, n tract.Supp) {
		for m := range NumMeasures {
			if isFisher(m) && fisherCost(b, h, n) > 5000 {
				continue
			}
			if v := Eval(m, s, b, h, n); math.IsNaN(v) {
				t.Errorf("Eval(%v, %d, %d, %d, %d) = NaN", m, s, b, h, n)
			}
		}
	}
	forEachValidTable(16, check)
	for _, tb := range randomTables(3000, 62, 1) {
		check(tb[0], tb[1], tb[2], tb[3])
	}
}

func isFisher(m Measure) bool { return m >= FisherProb && m <= FisherSupport }

// fisherCost returns the number of tables a Fisher test sums over.
func fisherCost(b, h, n tract.Supp) tract.Supp {
	if marginsDegenerate(b, h, n) {
		return 0
	}
	if n-h-b < 0 {
		b, h = n-b, n-h
	}
	return min(b, h) + 1
}

func TestPValuesInUnitInterval(t *testing.T) {
	t.Parallel()
	pvals := []Measure{Chi2PValue, YatesPValue, InfoPValue, FisherProb, FisherChi2, FisherInfo, FisherSupport}
	check := func(s, b, h, n tract.Supp) {
		for _, m := range pvals {
			if isFisher(m) && fisherCost(b, h, n) > 5000 {
				continue
			}
			if v := Eval(m, s, b, h, n); !(v >= 0 && v <= 1) {
				t.Errorf("Eval(%v, %d, %d, %d, %d) = %v, not in [0, 1]", m, s, b, h, n, v)
			}
		}
	}
	forEachValidTable(16, check)
	for _, tb := range randomTables(3000, 31, 2) {
		check(tb[0], tb[1], tb[2], tb[3])
	}
	// The B9 reproducer, where upstream's complement forms cancel.
	check(7969403, 8875339, 7982035, 8893601)
}

// upstreamRow is one line of testdata/measures_c.txt: a table and the
// values of all measures computed by upstream's C code (has[m] is false
// where the harness skipped an expensive Fisher test).
type upstreamRow struct {
	supp, body, head, base tract.Supp
	vals                   [NumMeasures]float64
	has                    [NumMeasures]bool
}

func loadUpstreamRows(t *testing.T) []upstreamRow {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "measures_c.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []upstreamRow
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := sc.Text()
		if text == "" || text[0] == '#' {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 4+int(NumMeasures) {
			t.Fatalf("measures_c.txt:%d: %d fields", line, len(fields))
		}
		var r upstreamRow
		var ints [4]tract.Supp
		for i := range ints {
			v, err := strconv.ParseInt(fields[i], 10, 64)
			if err != nil {
				t.Fatalf("measures_c.txt:%d: %v", line, err)
			}
			ints[i] = tract.Supp(v)
		}
		r.supp, r.body, r.head, r.base = ints[0], ints[1], ints[2], ints[3]
		for m := range NumMeasures {
			field := fields[4+int(m)]
			if field == "-" {
				continue
			}
			v, err := strconv.ParseFloat(field, 64)
			if err != nil {
				t.Fatalf("measures_c.txt:%d: %v", line, err)
			}
			r.vals[m], r.has[m] = v, true
		}
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2000 {
		t.Fatalf("measures_c.txt: only %d rows", len(rows))
	}
	return rows
}

// valueClass says how a measure's value is compared with upstream's.
type valueClass int

const (
	classExact      valueClass = iota // no log/exp/gamma: bit-identical
	classImportance                   // one log: relative, in ulps
	classChi2PValue                   // GammaQ(1/2, x/2): relative, in ε·(2+x)
	classInfo                         // sum of logs: absolute, in ε·(1+ln n)
	classInfoPValue                   // GammaQ of a classInfo value
	classFisher                       // sums of exp(lnΓ sums): absolute, in ε·T
)

// Tolerances for the comparison with upstream, in the units of each class
// (see normalizedDiff). The measured maxima over testdata/measures_c.txt
// (Apple libm vs Go's math package, both without FMA) are logged by
// TestEvalMatchesUpstream; they were 1.35 (importance), 2.12 (chi^2 and
// Yates p-values), 1.4 (information) and 0.19 (Fisher tests). The
// tolerances leave a margin of 3 to 5 times over these maxima:
//
//   - One log or exp has an error below 1 ulp in either library, so the
//     importance log(r)/LN_2 differs by a few ulps at most.
//   - GammaQ computes exp(a·ln x - x - lnΓ(a)); an error of a few ulps in
//     the exponent, whose magnitude is about x, becomes a relative error
//     of a few ε·x in the result.
//   - The information sums four terms t·log(t/(r·c)) of magnitude up to
//     2·n·ln n and divides by n; the result is near 0 for independent
//     tables, so its error is absolute, a few ε·(1 + ln n).
//   - A Fisher p-value sums exp(ln P(x)), where ln P(x) adds nine lnΓ
//     values of magnitude up to lnΓ(n+1); with T = 9·lnΓ(n+1) + 1 every
//     summed probability has a relative error of a few ε·T, so the sum
//     (at most 1) has an absolute error of a few ε·T. The complement forms
//     1 - sum have the same absolute error.
const (
	tolImportance = 4
	tolChi2PValue = 8
	tolInfo       = 8
	tolFisher     = 1
)

func measureClass(m Measure) valueClass {
	switch m {
	case Importance:
		return classImportance
	case Chi2PValue, YatesPValue:
		return classChi2PValue
	case Info:
		return classInfo
	case InfoPValue:
		return classInfoPValue
	case FisherProb, FisherChi2, FisherInfo, FisherSupport:
		return classFisher
	}
	return classExact
}

func classTolerance(c valueClass) float64 {
	switch c {
	case classImportance:
		return tolImportance
	case classChi2PValue, classInfoPValue:
		return tolChi2PValue
	case classInfo:
		return tolInfo
	case classFisher:
		return tolFisher
	}
	return 0
}

// diffStat collects the comparison results of one measure.
type diffStat struct {
	max      float64     // largest normalized difference outside deviations
	maxRel   float64     // largest relative difference outside deviations
	row      upstreamRow // where max occurred
	n        int         // rows compared
	skip     int         // rows without an upstream value
	dev      int         // rows where a deliberate deviation applies
	devDiff  int         // ... and the fixed value differs from upstream's
	exact    int         // rows compared with the exact reference (N1, issue #16)
	exactUp  int         // ... where upstream's value is outside the bound
	maxRef   float64     // largest deviation from the reference, in tolerances
	failures int
}

// productsSmall reports whether supp·base and head·body are at most 2^53,
// so that upstream's double products are exact.
func productsSmall(supp, body, head, base tract.Supp) bool {
	h1, l1 := bits.Mul64(uint64(supp), uint64(base))
	h2, l2 := bits.Mul64(uint64(head), uint64(body))
	return h1 == 0 && h2 == 0 && l1 <= 1<<53 && l2 <= 1<<53
}

// exactScope reports whether measure m on row r is compared with the
// exact reference (internal/oracle) within its documented bound instead
// of with upstream's value (docs/differences.md, numeric contract):
//
//   - Chi2, Yates and their p-values when a product exceeds 2^53
//     (upstream rounds them; below, the values are bit-identical
//     respectively within the class tolerance);
//   - Certainty when conf > prior and base/(base-head) > certGate (the
//     exact form; elsewhere bit-identical);
//   - the Fisher tests for base > maxFact where the reference has a value
//     (at most oracle.FisherMaxTables tables); with more tables they keep
//     the class tolerance against upstream's (fixed) value.
func exactScope(m Measure, r upstreamRow) bool {
	s, b, h, n := r.supp, r.body, r.head, r.base
	switch m {
	case Chi2, Yates, Chi2PValue, YatesPValue:
		return !productsSmall(s, b, h, n)
	case Certainty:
		return b > 0 && h < n && n-h < n/certGate && AboveExpected(s, b, h, n)
	case FisherProb, FisherChi2, FisherInfo, FisherSupport:
		lo, hi := max(0, b-(n-h)), min(b, h)
		return n > maxFact && hi-lo+1 <= oracle.FisherMaxTables
	}
	return false
}

// TestEvalMatchesUpstream compares every measure with the values computed
// by upstream's C code (testdata/measures_c.txt, generated by
// reference/harness/ruleval). Measures without log/exp/gamma must be
// bit-identical; the others must agree within the class tolerances above.
// Where a deliberate deviation applies (fixedUpstreamValue), the value is
// compared with the corrected value instead: a fixed constant, a clamped
// upstream value, or for FisherProb and FisherInfo an independent exact
// computation (fisherOracle). Where the port computes the value exactly
// although upstream does not (exactScope; issue #16), it is compared with
// the exact reference within the documented bound, and the rows where
// upstream's own value is outside that bound are counted.
func TestEvalMatchesUpstream(t *testing.T) {
	t.Parallel()
	rows := loadUpstreamRows(t)
	var stats [NumMeasures]diffStat
	for _, r := range rows {
		for m := range NumMeasures {
			st := &stats[m]
			if !r.has[m] {
				st.skip++
				continue
			}
			st.n++
			got := Eval(m, r.supp, r.body, r.head, r.base)
			if exactScope(m, r) {
				rm, _ := oracle.RefByName(m.String())
				ref, err := rm.Eval(r.supp, r.body, r.head, r.base)
				if err != nil {
					t.Fatalf("%v%v: %v", m, r.supp, err)
				}
				st.exact++
				if !ref.Matches(r.vals[m]) {
					st.exactUp++
				}
				if ref.Tol > 0 {
					st.maxRef = max(st.maxRef, math.Abs(got-ref.V)/ref.Tol)
				}
				if !ref.Matches(got) {
					st.failures++
					if st.failures <= 5 {
						t.Errorf("Eval(%v, %d, %d, %d, %d) = %.17g, reference %v (upstream %.17g)",
							m, r.supp, r.body, r.head, r.base, got, ref, r.vals[m])
					}
				}
				continue
			}
			want, dev, oracle := fixedUpstreamValue(t, m, r, got)
			tol := classTolerance(measureClass(m))
			e := normalizedDiff(m, r, got, want)
			if dev {
				st.dev++
				if normalizedDiff(m, r, got, r.vals[m]) > tol {
					st.devDiff++
				}
				if oracle && r.base <= maxFact {
					tol = 0 // the oracle reproduces the value exactly
				}
			} else {
				if e > st.max {
					st.max, st.row = e, r
				}
				if want != 0 && !math.IsInf(want, 0) {
					st.maxRel = max(st.maxRel, math.Abs(got-want)/math.Abs(want))
				}
			}
			if !(e <= tol) {
				st.failures++
				if st.failures <= 5 {
					t.Errorf("Eval(%v, %d, %d, %d, %d) = %.17g, want %.17g (upstream %.17g, deviation %v, diff %.3g > %v)",
						m, r.supp, r.body, r.head, r.base, got, want, r.vals[m], dev, e, tol)
				}
			}
		}
	}
	for m := range NumMeasures {
		st := &stats[m]
		t.Logf("%-18v rows=%4d skipped=%3d deviation=%3d (differs %3d) max=%.3g rel=%.2g at (%d, %d, %d, %d); exact reference %d rows (upstream outside the bound %d, max %.3g·tol)",
			m, st.n, st.skip, st.dev, st.devDiff, st.max, st.maxRel, st.row.supp, st.row.body, st.row.head, st.row.base,
			st.exact, st.exactUp, st.maxRef)
	}
}

// fixedUpstreamValue returns the value expected for measure m on row r,
// whether a deliberate deviation from upstream applies, and whether the
// expected value comes from fisherOracle (and must be matched exactly).
// It also checks that upstream's value shows the documented bug where a
// deviation applies.
func fixedUpstreamValue(t *testing.T, m Measure, r upstreamRow, got float64) (want float64, dev, oracle bool) {
	t.Helper()
	s, b, h, n := r.supp, r.body, r.head, r.base
	up := r.vals[m]
	bug := func(ok bool, what string) {
		t.Helper()
		if !ok {
			t.Errorf("%v(%d, %d, %d, %d): upstream gives %v, expected %s", m, s, b, h, n, up, what)
		}
	}
	switch m {
	case Conviction: // B4
		if b > 0 && b <= s {
			bug(up == 0, "0 for confidence 1")
			return math.Inf(1), true, false
		}
	case Certainty: // B5
		if b > 0 && h >= n {
			bug(math.IsNaN(up), "NaN for head = base")
			return 0, true, false
		}
	case Yates, YatesPValue: // B10
		if !marginsDegenerate(b, h, n) &&
			math.Abs(float64(float64(h)*float64(b))-float64(float64(s)*float64(n)))-float64(0.5*float64(n)) < 0 {
			if m == Yates {
				bug(up > 0, "a positive value")
				return 0, true, false
			}
			bug(up < 1, "a p-value below 1")
			return 1, true, false
		}
	case Info: // B7
		if up < 0 || infoRaw(s, b, h, n) < 0 {
			return max(up, 0), true, false
		}
	case FisherProb, FisherInfo: // B6, B9
		if fisherFragile(m, s, b, h, n) {
			return fisherOracle(t, m, s, b, h, n, got), true, true
		}
		if up > 1 {
			return 1, true, false
		}
	case FisherChi2, FisherSupport: // B9
		if up < 0 || up > 1 {
			return clampUnit(up), true, false
		}
	}
	return up, false, false
}

// normalizedDiff returns |got - want| in the units of the class of m (0
// for identical values and +Inf for any difference of a classExact
// measure).
func normalizedDiff(m Measure, r upstreamRow, got, want float64) float64 {
	if math.Float64bits(got) == math.Float64bits(want) || (math.IsNaN(got) && math.IsNaN(want)) {
		return 0
	}
	n := float64(r.base)
	d := math.Abs(got - want)
	switch measureClass(m) {
	case classImportance:
		return d / (epsilon * math.Abs(want))
	case classChi2PValue:
		x := n * Eval(Chi2, r.supp, r.body, r.head, r.base)
		if m == YatesPValue {
			x = n * Eval(Yates, r.supp, r.body, r.head, r.base)
		}
		return d / (epsilon * max(math.Abs(want), 0x1p-1022) * (2 + x))
	case classInfo:
		return d / (epsilon * (1 + math.Log(n)))
	case classInfoPValue:
		// The information may differ from upstream's by tolInfo·ε·(1+ln n)
		// (classInfo); accept the p-values in between, plus the error of
		// GammaQ itself.
		info := Eval(Info, r.supp, r.body, r.head, r.base)
		delta := tolInfo * epsilon * (1 + math.Log(n))
		lo := chi2cdfQ(2*ln2*n*(info+delta), 1)
		hi := chi2cdfQ(2*ln2*n*max(info-delta, 0), 1)
		if want >= lo && want <= hi {
			return 0
		}
		x := 2 * ln2 * n * info
		return min(math.Abs(want-lo), math.Abs(want-hi)) / (epsilon * max(math.Abs(want), 0x1p-1022) * (2 + x))
	case classFisher:
		return d / (epsilon * (9*logGamma(n+1) + 1))
	}
	return math.Inf(1)
}

// fisherFragile reports whether upstream's choice of the tables summed by
// FisherProb or FisherInfo can depend on rounding, i.e. whether the B6
// fix applies: the fixed and the replicated upstream algorithm differ, or
// some table (or, for the observed table, 0, which decides whether upstream
// keeps it) lies within the rounding error bound of the observed value.
// Upstream's decisions for all other tables are the exact ones.
func fisherFragile(m Measure, s, b, h, n tract.Supp) bool {
	if marginsDegenerate(b, h, n) {
		return false
	}
	if Eval(m, s, b, h, n) != clampUnit(upstreamFisher(m, s, b, h, n)) {
		return true
	}
	ft := newFisherTable(s, b, h, n)
	val := func(x tract.Supp) float64 { return ft.logProb(x) }
	tol := fisherLogProbTol(n)
	if m == FisherInfo {
		val = func(x tract.Supp) float64 { return infoRaw(x, ft.body, ft.head, ft.base) }
		tol = fisherInfoTol(n)
	}
	vs := val(ft.supp)
	if math.Abs(vs) <= tol {
		return true
	}
	for x := tract.Supp(0); x <= ft.body; x++ {
		if x != ft.supp && math.Abs(val(x)-vs) <= tol {
			return true
		}
	}
	return false
}
