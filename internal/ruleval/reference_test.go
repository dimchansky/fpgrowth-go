package ruleval

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
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

// isALG6 reports whether a mismatch of measure c on tb is explained by
// the known G-test defect ALG-6 (docs/audit/algorithms.md): the
// implementation computes the information as (ln n + Σ t·ln(t/(r·k))/n)/ln 2,
// whose absolute error is a few ulps of ln n, and the G statistic
// G = 2·n·I (nats) inherits an absolute error of about δG = 2·n·10·ε·ln n.
// The chi²(1) tail has an infinite slope at G = 0, so near independence
// this becomes a p-value error of up to √(2·δG/π): 1 - O(1e-7) instead of
// 1 at exact independence (the audit's case), and relative errors of
// 1e-11 to 1e-7 for nearly independent tables with n from 10^3 to 10^5.
// The mismatch is accepted only if it is within that bound (with a factor
// of about 2 to spare); any larger deviation is an error.
func isALG6(c byte, tb table, r compareResult) bool {
	if c != 'g' || math.IsNaN(r.got) {
		return false
	}
	infoRef, _ := oracle.RefByLetter('i')
	info, err := infoRef.Eval(tract.Supp(tb.s), tract.Supp(tb.b), tract.Supp(tb.h), tract.Supp(tb.n))
	if err != nil {
		return false
	}
	n := float64(tb.n)
	g := 2 * math.Ln2 * n * info.V
	dg := 32 * 0x1p-52 * n * math.Log(n)
	var bound float64
	if g <= 2*dg {
		bound = math.Sqrt(2 * 3 * dg / math.Pi)
	} else {
		x := g - dg // the tail's slope is largest at the smallest argument
		bound = dg * math.Exp(-x/2) / math.Sqrt(2*math.Pi*x)
	}
	return math.Abs(r.got-r.want.V) <= bound+r.want.Tol
}

// sweep compares all measures on the tables and returns the number of
// comparisons and of ALG-6 mismatches. Every other mismatch is an error.
func sweep(t *testing.T, tables []table) (checked, alg6 int) {
	t.Helper()
	bad := 0
	for _, tb := range tables {
		for _, rm := range oracle.RefMeasures() {
			r := compare(t, rm, tb)
			if r.skipped {
				continue
			}
			checked++
			switch {
			case r.ok:
			case isALG6(rm.Letter, tb, r):
				alg6++
			default:
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
	return checked, alg6
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
// weighted tables up to n = 2^24. The only accepted mismatches are ALG-6
// (exactly independent tables, G-test p-value); the test fails if there
// are none, so the exemption must be removed when ALG-6 is fixed.
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
	checked, alg6 := sweep(t, tables)
	t.Logf("%d tables (%d exhaustive with n <= %d), %d comparisons, %d ALG-6 mismatches",
		len(tables), exhaustive, nmax, checked, alg6)
	if alg6 == 0 {
		t.Error("no ALG-6 mismatch left: ALG-6 appears fixed; remove the exemption (isALG6) and its known-failure entries")
	}
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
	// ALG-6: exactly independent tables (s·n = b·h), the audit's worst
	// case (numcheck) and two small ones.
	tables = append(tables,
		largeTable{"alg6 numcheck worst", table{12, 13, 48, 52}},
		largeTable{"alg6 small", table{2, 4, 3, 6}},
		largeTable{"alg6 small 2", table{1, 2, 5, 10}},
	)
	return tables
}

// knownFailures lists the measures that currently differ from the
// reference on the tables of largeTables (by table name): the known
// numerical defects ALG-1 (precision loss for large total weights;
// docs/differences.md N1; issue #16) and ALG-6 (G-test p-value at exact
// independence; issue #16/#17). The suite reports them and keeps running.
// The list is strict in both directions: a listed measure that matches
// the reference, or an unlisted one that does not, fails
// TestKnownMeasureFailures, so a fix must remove its entries.
//
// ALG-1 entries (by measure): p, t (chi² and Yates p-values: s·n - b·h
// cancels in float64 beyond 2^53), n, y (chi² and Yates collapse to 0 for
// the perfect association), g (information: ln n + Σ/n cancels), z
// (certainty: h/n rounds to 1), f h m s (Fisher: log-gamma differences).
// ALG-6 entries are the g failures of exactly independent tables (the
// "-0" shapes with s·n = b·h and the alg6 tables).
var knownFailures = map[string]struct{ id, letters string }{
	"alg1 k=1e+06":                          {"ALG-1", "g"},
	"alg1 k=1e+09":                          {"ALG-1", "ptgfhm"},
	"alg1 k=1e+12":                          {"ALG-1", "ptgfhm"},
	"alg1 k=1e+13":                          {"ALG-1", "ptgfhm"},
	"alg1 k=1e+14":                          {"ALG-1", "ptgfhm"},
	"alg1 k=1e+15":                          {"ALG-1", "ptgfhm"},
	"alg1 k=1e+16":                          {"ALG-1", "ptgfhm"},
	"alg1 k=1e+17":                          {"ALG-1", "ptgfhm"},
	"alg1 k=1e+18":                          {"ALG-1", "ptgfhm"},
	"shape [1 2 2 4]·1000000-1":             {"ALG-1", "g"},
	"shape [1 2 2 4]·1000000-7":             {"ALG-1", "g"},
	"shape [1 3 3 9]·1000000-1":             {"ALG-1", "g"},
	"shape [1 3 3 9]·1000000-7":             {"ALG-1", "g"},
	"shape [1 2 2 4]·1000000000-0":          {"ALG-6", "g"},
	"shape [1 2 2 4]·1000000000-1":          {"ALG-1", "g"},
	"shape [1 2 2 4]·1000000000-7":          {"ALG-1", "g"},
	"shape [1 3 3 9]·1000000000-1":          {"ALG-1", "g"},
	"shape [1 3 3 9]·1000000000-7":          {"ALG-1", "g"},
	"shape [1 2 2 4]·1000000000000-1":       {"ALG-1", "ptg"},
	"shape [1 2 2 4]·1000000000000-7":       {"ALG-1", "g"},
	"shape [1 3 3 9]·1000000000000-1":       {"ALG-1", "g"},
	"shape [1 3 3 9]·1000000000000-7":       {"ALG-1", "ptg"},
	"shape [1 2 2 4]·1000000000000000-1":    {"ALG-1", "ptg"},
	"shape [1 2 2 4]·1000000000000000-7":    {"ALG-1", "ptg"},
	"shape [1 3 3 9]·1000000000000000-0":    {"ALG-6", "g"},
	"shape [1 3 3 9]·1000000000000000-1":    {"ALG-1", "ptg"},
	"shape [1 3 3 9]·1000000000000000-7":    {"ALG-1", "ptg"},
	"shape [1 2 2 4]·1152921504606846976-1": {"ALG-1", "ptg"},
	"shape [1 2 2 4]·1152921504606846976-7": {"ALG-1", "ptg"},
	"perfect association":                   {"ALG-1", "znpytgfhms"},
	"certainty near 1":                      {"ALG-1", "zpgs"},
	"alg6 numcheck worst":                   {"ALG-6", "g"},
	"alg6 small":                            {"ALG-6", "g"},
	"alg6 small 2":                          {"ALG-6", "g"},
}

// TestKnownMeasureFailures compares all measures with the reference on
// the large-weight grid (up to n = 2^63 - 8) and the ALG-6 tables and
// checks that exactly the measures of knownFailures differ.
func TestKnownMeasureFailures(t *testing.T) {
	t.Parallel()
	names := map[string]bool{}
	reported := map[string]int{}
	for _, lt := range largeTables() {
		if names[lt.name] {
			t.Fatalf("duplicate table name %q", lt.name)
		}
		names[lt.name] = true
		known := knownFailures[lt.name]
		for _, rm := range oracle.RefMeasures() {
			r := compare(t, rm, lt.table)
			if r.skipped {
				if strings.IndexByte(known.letters, rm.Letter) >= 0 {
					t.Errorf("%s %v: known failure %c (%s) is not checked", lt.name, lt.table, rm.Letter, known.id)
				}
				continue
			}
			listed := strings.IndexByte(known.letters, rm.Letter) >= 0
			switch {
			case !r.ok && listed:
				reported[known.id]++
				t.Logf("known failure %s: %s %s(%c) got %.17g, reference %v", known.id, lt.name, rm.Name, rm.Letter, r.got, r.want)
			case !r.ok:
				t.Errorf("%s %v: %s(%c) got %.17g, reference %v (not a known failure)", lt.name, lt.table, rm.Name, rm.Letter, r.got, r.want)
			case listed:
				t.Errorf("%s %v: %s(%c) now matches the reference (%v): remove it from knownFailures (%s)", lt.name, lt.table, rm.Name, rm.Letter, r.want, known.id)
			}
		}
	}
	for name := range knownFailures {
		if !names[name] {
			t.Errorf("knownFailures entry %q names no table", name)
		}
	}
	t.Logf("known failures reported: %v", reported)
}

// alg1Measures are the measures affected by ALG-1 (differences.md N1).
const alg1Measures = "nptyigzfhms"

// TestMeasuresExtendedLarge runs only with FPG_MEASURE_CASES: random
// weighted tables with n log-uniform up to 2^62. Mismatches of the ALG-1
// measures are counted per measure and order of magnitude of n and
// logged (they are the known limitation N1); a mismatch of any other
// measure fails.
func TestMeasuresExtendedLarge(t *testing.T) {
	k := measureCases(t)
	if k == 0 {
		t.Skip("set FPG_MEASURE_CASES to run the extended large-weight sweep")
	}
	t.Parallel()
	r := rand.New(rand.NewPCG(15, 4))
	counts := map[string]int{}
	checked := 0
	for range k {
		tb := randomTable(r, 62)
		for _, rm := range oracle.RefMeasures() {
			res := compare(t, rm, tb)
			if res.skipped {
				continue
			}
			checked++
			if res.ok {
				continue
			}
			if strings.IndexByte(alg1Measures, rm.Letter) < 0 {
				t.Errorf("%s(%c) %v: got %.17g, reference %v", rm.Name, rm.Letter, tb, res.got, res.want)
				continue
			}
			counts[fmt.Sprintf("%c n<2^%02d", rm.Letter, bits.Len64(uint64(tb.n)))]++
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		t.Logf("ALG-1 mismatches %s: %d", key, counts[key])
	}
	t.Logf("%d random tables, %d comparisons", k, checked)
}
