package ruleval

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Comparison of every rule measure with the independent reference
// measures of internal/oracle (issue #15, audit TST-3). Unlike the C
// fixtures (measures_c.txt) these tests catch a formula error that the C
// program shares, and they cover weights beyond upstream's 32-bit range.
//
// The default run checks every table with n <= 20, boundary margins up to
// n = 2^24 and 3,000 random tables with n <= 2^24 (with -short or -race:
// n <= 12 and 500 random tables); Fisher tests are compared where they
// sum over at most maxFisherTables tables.
// FPG_MEASURE_CASES=<k> runs the extended sweep instead: every table with
// n <= 60 (the range of the audit's numcheck), k random tables with
// n <= 2^24, and k random tables with n up to 2^62, where the mismatches
// of the ALG-1 measures are only counted (TestMeasuresExtendedLarge).

// table is a 2x2 contingency table in upstream's argument order.
type table struct{ s, b, h, n int64 }

func (tb table) String() string {
	return fmt.Sprintf("s=%d b=%d h=%d n=%d", tb.s, tb.b, tb.h, tb.n)
}

// fisherTables returns the number of tables a Fisher test of tb sums over.
func (tb table) fisherTables() int64 {
	lo := max(0, tb.b-(tb.n-tb.h))
	return min(tb.b, tb.h) - lo + 1
}

// compareResult is the outcome of comparing one measure on one table.
type compareResult struct {
	got     float64
	want    oracle.Value
	ok      bool
	skipped bool // out of the reference's (or the test's) Fisher scope
}

// maxFisherTables bounds the Fisher tests in the sweeps: the
// implementation's cost is linear and the reference's quadratic in it.
const maxFisherTables = 256

// compare evaluates measure rm on tb with the implementation and the
// reference.
func compare(t *testing.T, rm *oracle.RefMeasure, tb table) compareResult {
	t.Helper()
	m, ok := ByLetter(rm.Letter)
	if !ok {
		t.Fatalf("no measure %c", rm.Letter)
	}
	if strings.IndexByte("fhms", rm.Letter) >= 0 && tb.fisherTables() > maxFisherTables {
		return compareResult{skipped: true}
	}
	s, b, h, n := tract.Supp(tb.s), tract.Supp(tb.b), tract.Supp(tb.h), tract.Supp(tb.n)
	want, err := rm.Eval(s, b, h, n)
	if errors.Is(err, oracle.ErrOutOfScope) {
		return compareResult{skipped: true}
	}
	if err != nil {
		t.Fatalf("%c %v: %v", rm.Letter, tb, err)
	}
	got := Eval(m, s, b, h, n)
	return compareResult{got: got, want: want, ok: want.Matches(got)}
}

// sweep compares all measures on the tables and returns the number of
// comparisons. Every mismatch is an error.
func sweep(t *testing.T, tables []table) (checked int) {
	t.Helper()
	bad := 0
	for _, tb := range tables {
		for _, rm := range oracle.RefMeasures() {
			r := compare(t, rm, tb)
			if r.skipped {
				continue
			}
			checked++
			if !r.ok {
				bad++
				if bad <= 20 {
					t.Errorf("%s(%c) %v: got %.17g, reference %v", rm.Name, rm.Letter, tb, r.got, r.want)
				}
			}
		}
	}
	if bad > 20 {
		t.Errorf("... %d mismatches in total", bad)
	}
	return checked
}

// exhaustiveTables returns every valid table with 1 <= n <= nmax,
// including empty and full margins and s = 0.
func exhaustiveTables(nmax int64) []table {
	var tables []table
	for n := int64(1); n <= nmax; n++ {
		for b := int64(0); b <= n; b++ {
			for h := int64(0); h <= n; h++ {
				for s := max(0, b+h-n); s <= min(b, h); s++ {
					tables = append(tables, table{s, b, h, n})
				}
			}
		}
	}
	return tables
}

// boundaryTables returns tables with degenerate and extreme margins: body
// and head support 0, 1, 2, n/2, n-1 or n, and s at both ends of its
// range and next to them.
func boundaryTables() []table {
	var tables []table
	for _, n := range []int64{1, 2, 3, 5, 64, 1000, 1<<16 - 1, 1 << 20, 1 << 24} {
		margins := []int64{0, 1, 2, n / 2, n - 1, n}
		for _, b := range margins {
			for _, h := range margins {
				if b < 0 || h < 0 || b > n || h > n {
					continue
				}
				lo, hi := max(0, b+h-n), min(b, h)
				for _, s := range []int64{lo, lo + 1, (lo + hi) / 2, hi - 1, hi} {
					if s >= lo && s <= hi {
						tables = append(tables, table{s, b, h, n})
					}
				}
			}
		}
	}
	slices.SortFunc(tables, func(x, y table) int {
		return cmpTable(x, y)
	})
	return slices.Compact(tables)
}

func cmpTable(x, y table) int {
	for _, d := range [4]int64{x.n - y.n, x.b - y.b, x.h - y.h, x.s - y.s} {
		if d != 0 {
			if d < 0 {
				return -1
			}
			return 1
		}
	}
	return 0
}

// randomTable returns a random valid table with n log-uniform in
// [1, 2^maxBits] (weighted data: the four cells are arbitrary weights).
func randomTable(r *rand.Rand, maxBits int) table {
	n := int64(1) << r.IntN(maxBits+1)
	n += r.Int64N(n) // [2^k, 2^(k+1))
	n = min(n, int64(1)<<maxBits)
	b := r.Int64N(n + 1)
	h := r.Int64N(n + 1)
	if r.IntN(4) == 0 { // a strong association
		h = b + r.Int64N(min(n-b, max(1, n/1000))+1)
	}
	lo, hi := max(0, b+h-n), min(b, h)
	s := lo + r.Int64N(hi-lo+1)
	if r.IntN(4) == 0 { // close to independence
		s = min(max(lo, b*h/n+r.Int64N(3)-1), hi)
	}
	return table{s, b, h, n}
}

// measureCases returns the FPG_MEASURE_CASES setting (0 if unset).
func measureCases(t *testing.T) int {
	t.Helper()
	s := os.Getenv("FPG_MEASURE_CASES")
	if s == "" {
		return 0
	}
	k, err := strconv.Atoi(s)
	if err != nil || k < 0 {
		t.Fatalf("FPG_MEASURE_CASES=%q: not a non-negative integer", s)
	}
	return k
}

// TestMeasuresMatchReference compares the 25 rule measures with the
// reference on exhaustive small tables, boundary margins and random
// weighted tables up to n = 2^24, within the documented error bounds (the
// reference tolerances). There are no accepted mismatches (ALG-6, the
// G-test p-value near independence, was fixed by issue #16).
func TestMeasuresMatchReference(t *testing.T) {
	t.Parallel()
	nmax, random := int64(20), 3000
	if testing.Short() || raceEnabled {
		nmax, random = 12, 500 // big-number arithmetic is slow under -race
	}
	if k := measureCases(t); k > 0 {
		nmax, random = 60, k
	}
	r := rand.New(rand.NewPCG(15, 3))
	var tables []table
	tables = append(tables, exhaustiveTables(nmax)...)
	exhaustive := len(tables)
	tables = append(tables, boundaryTables()...)
	for range random {
		tables = append(tables, randomTable(r, 24))
	}
	checked := sweep(t, tables)
	t.Logf("%d tables (%d exhaustive with n <= %d), %d comparisons", len(tables), exhaustive, nmax, checked)
}

// largeTable is a named table of the known-failure grid.
type largeTable struct {
	name string
	table
}

// largeTables is the grid of TestKnownMeasureFailures: the tables of the
// audit's numcheck and scale checkers (docs/audit/algorithms.md, "Tools")
// up to the int64 range.
func largeTables() []largeTable {
	var tables []largeTable
	// ALG-1 family: s = 3k-2, b = 3k, h = 9k-2, n = 9k (exact chi² p
	// 0.0455003, G p 0.0360561).
	for _, k := range []int64{1e3, 1e6, 1e9, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18} {
		tables = append(tables, largeTable{fmt.Sprintf("alg1 k=%.0e", float64(k)), table{3*k - 2, 3 * k, 9*k - 2, 9 * k}})
	}
	// numcheck's scaled shapes with small perturbations
	for _, scale := range []int64{1e6, 1e9, 1e12, 1e15, 1 << 60} {
		for _, sh := range [][4]int64{{1, 2, 2, 4}, {1, 3, 3, 9}, {3, 5, 7, 12}, {2, 4, 5, 9}} {
			if sh[3] > math.MaxInt64/scale { // 9·2^60 and 12·2^60 overflow
				continue
			}
			for _, d := range []int64{0, 1, 7} {
				s, b, h, n := sh[0]*scale-d, sh[1]*scale, sh[2]*scale, sh[3]*scale
				tables = append(tables, largeTable{fmt.Sprintf("shape %v·%d-%d", sh, scale, d), table{s, b, h, n}})
			}
		}
	}
	// The audit's CLI reproductions: perfect association (x y M / q 3)
	// and a certainty factor with confidence 1 and prior 1 - 2/M.
	const m = 9223372036854775800
	tables = append(tables,
		largeTable{"perfect association", table{m, m, m, m + 3}},
		largeTable{"certainty near 1", table{2, 2, m - 2, m}},
	)
	// supports between 2^31 and 2^40 with |s·n - b·h| >= 2^63 (int64
	// would overflow; review of #16, F5)
	tables = append(tables,
		largeTable{"int64 overflow 1e10", table{3e9, 6e9, 7e9, 1e10}},
		largeTable{"int64 overflow 1e11", table{3e10, 6e10, 7e10, 1e11}},
	)
	// ALG-6: exactly independent tables (s·n = b·h), the audit's worst
	// case (numcheck) and two small ones.
	tables = append(tables,
		largeTable{"alg6 numcheck worst", table{12, 13, 48, 52}},
		largeTable{"alg6 small", table{2, 4, 3, 6}},
		largeTable{"alg6 small 2", table{1, 2, 5, 10}},
	)
	return tables
}

// deviationRatio returns |got - V|/Tol (0 for an exact match, +Inf for a
// mismatch without tolerance).
func deviationRatio(r compareResult) float64 {
	switch {
	case r.got == r.want.V:
		return 0
	case r.want.Tol == 0 || math.IsInf(r.want.V, 0) || math.IsNaN(r.got):
		return math.Inf(1)
	}
	return math.Abs(r.got-r.want.V) / r.want.Tol
}

// TestLargeTablesMatchReference compares all measures with the reference
// on the large-weight grid (up to n = 2^63 - 8) and the ALG-6 tables.
// They were the known failures of audit ALG-1 and ALG-6 (issue #15, in
// n p y t i g z f h m s); since issue #16 every comparison must match
// within the documented bound, and by a clear margin (below a quarter of
// the tolerance), so that a last-bit difference of the platform cannot
// decide the outcome.
func TestLargeTablesMatchReference(t *testing.T) {
	t.Parallel()
	names := map[string]bool{}
	worst := map[string]float64{}
	for _, lt := range largeTables() {
		if names[lt.name] {
			t.Fatalf("duplicate table name %q", lt.name)
		}
		names[lt.name] = true
		for _, rm := range oracle.RefMeasures() {
			r := compare(t, rm, lt.table)
			if r.skipped {
				continue
			}
			q := deviationRatio(r)
			worst[string(rm.Letter)] = max(worst[string(rm.Letter)], q)
			if !r.ok || q > 0.25 {
				t.Errorf("%s %v: %s(%c) got %.17g, reference %v (%.3g·tol)", lt.name, lt.table, rm.Name, rm.Letter, r.got, r.want, q)
			}
		}
	}
	t.Logf("largest deviation per measure, in units of its tolerance: %v", worst)
}

// TestMeasuresExtendedLarge runs only with FPG_MEASURE_CASES: random
// weighted tables with n log-uniform up to 2^62, compared with the
// reference within the documented bounds. The largest deviation per
// measure (in units of its tolerance) is logged.
func TestMeasuresExtendedLarge(t *testing.T) {
	k := measureCases(t)
	if k == 0 {
		t.Skip("set FPG_MEASURE_CASES to run the extended large-weight sweep")
	}
	t.Parallel()
	r := rand.New(rand.NewPCG(15, 4))
	worst := map[byte]float64{}
	checked, bad := 0, 0
	for range k {
		tb := randomTable(r, 62)
		for _, rm := range oracle.RefMeasures() {
			res := compare(t, rm, tb)
			if res.skipped {
				continue
			}
			checked++
			worst[rm.Letter] = max(worst[rm.Letter], deviationRatio(res))
			if !res.ok {
				bad++
				if bad <= 20 {
					t.Errorf("%s(%c) %v: got %.17g, reference %v", rm.Name, rm.Letter, tb, res.got, res.want)
				}
			}
		}
	}
	keys := make([]byte, 0, len(worst))
	for c := range worst {
		keys = append(keys, c)
	}
	slices.Sort(keys)
	for _, c := range keys {
		t.Logf("%c: largest deviation %.3g·tol", c, worst[c])
	}
	t.Logf("%d random tables, %d comparisons, %d mismatches", k, checked, bad)
}
