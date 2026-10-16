package oracle

import (
	"fmt"
	"math"
	"math/bits"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// liftMeasure is upstream's re_lift.
func liftMeasure(supp, body, head, base tract.Supp) float64 {
	if body <= 0 || head <= 0 {
		return 0
	}
	return (float64(supp) * float64(base)) / (float64(body) * float64(head))
}

// mineOrFail runs Mine or fails the test.
func mineOrFail(t testing.TB, db *DB, p SetParams) []Set {
	t.Helper()
	sets, err := db.Mine(p)
	if err != nil {
		t.Fatalf("Mine(%+v): %v", p, err)
	}
	return sets
}

// test1 (smin 2) by hand; the C reference (fpgrowth -t? -s20) agrees.
var (
	test1Frequent2 = []string{
		"a:6", "b:7", "c:7", "d:7", "e:3",
		"a b:5", "a c:4", "a d:4", "b c:6", "b d:4", "c d:4", "d e:3",
		"a b c:4", "a b d:3", "a c d:2", "b c d:3", "a b c d:2",
	}
	test1Closed2 = []string{
		"a:6", "b:7", "c:7", "d:7",
		"a b:5", "a d:4", "b c:6", "b d:4", "c d:4", "d e:3",
		"a b c:4", "a b d:3", "b c d:3", "a b c d:2",
	}
	test1Generators2 = []string{
		"a:6", "b:7", "c:7", "d:7", "e:3",
		"a b:5", "a c:4", "a d:4", "b c:6", "b d:4", "c d:4",
		"a b d:3", "a c d:2", "b c d:3",
	}
)

func TestDB_Mine_test1(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	without := func(specs []string, drop ...string) []string {
		out := []string{}
	next:
		for _, s := range specs {
			for _, d := range drop {
				if s == d {
					continue next
				}
			}
			out = append(out, s)
		}
		return out
	}
	tests := []struct {
		name     string
		target   Target
		smin     tract.Supp
		zmin     int
		expected []string
	}{
		{name: "frequent smin 2", target: Frequent, smin: 2, zmin: 1, expected: test1Frequent2},
		{name: "frequent smin 3", target: Frequent, smin: 3, zmin: 1, expected: without(test1Frequent2, "a c d:2", "a b c d:2")},
		{name: "closed smin 2", target: Closed, smin: 2, zmin: 1, expected: test1Closed2},
		{name: "closed smin 3", target: Closed, smin: 3, zmin: 1, expected: without(test1Closed2, "a b c d:2")},
		{name: "maximal smin 2", target: Maximal, smin: 2, zmin: 1, expected: []string{"d e:3", "a b c d:2"}},
		{name: "maximal smin 3", target: Maximal, smin: 3, zmin: 1, expected: []string{"d e:3", "a b c:4", "a b d:3", "b c d:3"}},
		{name: "generators smin 2", target: Generators, smin: 2, zmin: 1, expected: test1Generators2},
		{name: "generators smin 3", target: Generators, smin: 3, zmin: 1, expected: without(test1Generators2, "a c d:2")},
		// -m0: the empty set (support 10 = total weight).
		{name: "frequent zmin 0", target: Frequent, smin: 2, zmin: 0, expected: append([]string{":10"}, test1Frequent2...)},
		{name: "closed zmin 0", target: Closed, smin: 2, zmin: 0, expected: append([]string{":10"}, test1Closed2...)},
		{name: "maximal zmin 0", target: Maximal, smin: 2, zmin: 0, expected: []string{"d e:3", "a b c d:2"}},
		{name: "generators zmin 0", target: Generators, smin: 2, zmin: 0, expected: append([]string{":10"}, test1Generators2...)},
		{name: "smin 0 acts as 1", target: Maximal, smin: 0, zmin: 1, expected: []string{"a d e:1", "c d e:1", "a b c d:2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewSetParams(tt.target, tt.smin)
			p.MinSize = tt.zmin
			got := mineOrFail(t, db, p)
			if d := DiffSets(parseSets(t, tt.expected...), got, DiffOptions{}); d != "" {
				t.Error(d)
			}
		})
	}
}

func TestDB_Mine_han(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(hanData), Options{})
	tests := []struct {
		name     string
		target   Target
		expected []string
	}{
		{
			name:   "frequent",
			target: Frequent,
			expected: []string{
				"I1:6", "I2:7", "I3:6", "I4:2", "I5:2",
				"I1 I2:4", "I1 I3:4", "I1 I5:2", "I2 I3:4", "I2 I4:2", "I2 I5:2",
				"I1 I2 I3:2", "I1 I2 I5:2",
			},
		},
		{
			name:   "closed",
			target: Closed,
			expected: []string{
				"I1:6", "I2:7", "I3:6", "I1 I2:4", "I1 I3:4", "I2 I3:4", "I2 I4:2",
				"I1 I2 I3:2", "I1 I2 I5:2",
			},
		},
		{
			name:     "maximal",
			target:   Maximal,
			expected: []string{"I2 I4:2", "I1 I2 I3:2", "I1 I2 I5:2"},
		},
		{
			name:   "generators",
			target: Generators,
			expected: []string{
				"I1:6", "I2:7", "I3:6", "I4:2", "I5:2", "I1 I2:4", "I1 I3:4", "I2 I3:4", "I1 I2 I3:2",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mineOrFail(t, db, NewSetParams(tt.target, 2))
			if d := DiffSets(parseSets(t, tt.expected...), got, DiffOptions{}); d != "" {
				t.Error(d)
			}
		})
	}
}

// TestDB_Mine_gates checks that the size, support and border filters are
// applied after the closed/maximal/generator decision. Expected results
// are by hand; the C reference gives the same (commands in the names).
func TestDB_Mine_gates(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	border := AbsBorder([]float64{60, 50}, 1, db.Total())
	tests := []struct {
		name     string
		target   Target
		smin     tract.Supp
		smax     tract.Supp
		zmin     int
		zmax     int
		border   []tract.Supp
		expected []string
	}{
		{
			name: "-tc -s20 -n2", target: Closed, smin: 2, smax: NoMaxSupp, zmin: 1, zmax: 2,
			expected: []string{"a:6", "b:7", "c:7", "d:7", "a b:5", "a d:4", "b c:6", "b d:4", "c d:4", "d e:3"},
		},
		{
			// e d (3) is maximal although a b c d (size 4) is not reported.
			name: "-tm -s20 -n2", target: Maximal, smin: 2, smax: NoMaxSupp, zmin: 1, zmax: 2,
			expected: []string{"d e:3"},
		},
		{
			name: "-tm -s20 -m3", target: Maximal, smin: 2, smax: NoMaxSupp, zmin: 3, zmax: NoMaxSize,
			expected: []string{"a b c d:2"},
		},
		{
			name: "-tc -s20 -S50", target: Closed, smin: 2, smax: 5, zmin: 1, zmax: NoMaxSize,
			expected: []string{
				"a b:5", "a d:4", "b d:4", "c d:4", "d e:3", "a b c:4", "a b d:3", "b c d:3", "a b c d:2",
			},
		},
		{
			// Supersets above smax still make their subsets non-maximal.
			name: "-tm -s30 -S30", target: Maximal, smin: 3, smax: 3, zmin: 1, zmax: NoMaxSize,
			expected: []string{"d e:3", "a b d:3", "b c d:3"},
		},
		{
			name: "-tm -s20 -S30", target: Maximal, smin: 2, smax: 3, zmin: 1, zmax: NoMaxSize,
			expected: []string{"d e:3", "a b c d:2"},
		},
		{
			name: "-ts -s20 -F60:50", target: Frequent, smin: 2, smax: NoMaxSupp, zmin: 1, zmax: NoMaxSize,
			border: border,
			expected: []string{
				"a:6", "b:7", "c:7", "d:7", "a b:5", "b c:6",
				"a b c:4", "a b d:3", "a c d:2", "b c d:3", "a b c d:2",
			},
		},
		{
			name: "-tc -s20 -F60:50", target: Closed, smin: 2, smax: NoMaxSupp, zmin: 1, zmax: NoMaxSize,
			border: border,
			expected: []string{
				"a:6", "b:7", "c:7", "d:7", "a b:5", "b c:6", "a b c:4", "a b d:3", "b c d:3", "a b c d:2",
			},
		},
		{
			name: "-tg -s20 -m2 -n2", target: Generators, smin: 2, smax: NoMaxSupp, zmin: 2, zmax: 2,
			expected: []string{"a b:5", "a c:4", "a d:4", "b c:6", "b d:4", "c d:4"},
		},
		{
			name: "-ts -s20 -m0 -n0", target: Frequent, smin: 2, smax: NoMaxSupp, zmin: 0, zmax: 0,
			expected: []string{":10"},
		},
		{
			name: "border at size 0", target: Frequent, smin: 2, smax: NoMaxSupp, zmin: 0, zmax: 1,
			border:   []tract.Supp{11, 7},
			expected: []string{"b:7", "c:7", "d:7"},
		},
		{
			name: "smax below smin", target: Frequent, smin: 3, smax: 2, zmin: 0, zmax: NoMaxSize,
			expected: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := SetParams{
				Target: tt.target, MinSupp: tt.smin, MaxSupp: tt.smax,
				MinSize: tt.zmin, MaxSize: tt.zmax, Border: tt.border,
			}
			got := mineOrFail(t, db, p)
			if d := DiffSets(parseSets(t, tt.expected...), got, DiffOptions{}); d != "" {
				t.Error(d)
			}
		})
	}
}

// TestDB_Mine_closureOfEmptySet covers items that occur in every
// transaction (upstream: top-level perfect extensions).
func TestDB_Mine_closureOfEmptySet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		data     string
		smin     tract.Supp
		target   Target
		expected []string
	}{
		{name: "allA closed", data: "a b\na c\na b c", smin: 1, target: Closed, expected: []string{"a:3", "a b:2", "a c:2", "a b c:1"}},
		{name: "allA maximal", data: "a b\na c\na b c", smin: 1, target: Maximal, expected: []string{"a b c:1"}},
		{name: "allA generators", data: "a b\na c\na b c", smin: 1, target: Generators, expected: []string{":3", "b:2", "c:2", "b c:1"}},
		{name: "aa closed", data: "a\na", smin: 1, target: Closed, expected: []string{"a:2"}},
		// By definition {a} is maximal. Upstream's plain fpgrowth path
		// reports nothing here (fpgrowth -tm -s-1), its item set tree path
		// (-tm -el -d0) reports a (2).
		{name: "aa maximal", data: "a\na", smin: 1, target: Maximal, expected: []string{"a:2"}},
		{name: "ab maximal", data: "a b\na", smin: 2, target: Maximal, expected: []string{"a:2"}},
		{name: "aa generators", data: "a\na", smin: 1, target: Generators, expected: []string{":2"}},
		{name: "aa frequent", data: "a\na", smin: 1, target: Frequent, expected: []string{":2", "a:2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := mustNew(t, parseTxs(tt.data), Options{})
			p := NewSetParams(tt.target, tt.smin)
			p.MinSize = 0
			got := mineOrFail(t, db, p)
			if d := DiffSets(parseSets(t, tt.expected...), got, DiffOptions{}); d != "" {
				t.Error(d)
			}
		})
	}
}

// TestDB_Mine_generatorsWithMinSize documents a case where upstream's plain
// path differs from the definition: with -m2 upstream empties transactions
// with fewer than 2 items before mining (tbg_filter), which lowers the
// supports of single items, so fpgrowth -tg -s-1 -m2 reports nothing.
func TestDB_Mine_generatorsWithMinSize(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs("a b\na b\na\nb\nc"), Options{})
	p := NewSetParams(Generators, 1)
	p.MinSize = 2
	got := mineOrFail(t, db, p)
	if d := DiffSets(parseSets(t, "a b:2"), got, DiffOptions{}); d != "" {
		t.Error(d)
	}
}

func TestDB_Mine_selection(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{Selection: []string{"a", "e"}})
	got := mineOrFail(t, db, SetParams{Target: Closed, MinSupp: 1, MaxSupp: NoMaxSupp, MaxSize: NoMaxSize})
	expected := parseSets(t, ":10", "a:6", "e:3", "a e:1")
	if d := DiffSets(expected, got, DiffOptions{}); d != "" {
		t.Error(d)
	}
}

// test1Order is the item code order upstream uses for test1
// (rules-eval.md §3.2: d=0 c=1 b=2 a=3 e=4).
var test1Order = []string{"d", "c", "b", "a", "e"}

// TestDB_Mine_eval checks rule-based set evaluation against values printed
// by the C reference: fpgrowth -ts -s20 -el -a{x,m,n,a} -d0 -v' (%a %17e)'.
func TestDB_Mine_eval(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	tests := []struct {
		agg       Aggregation
		abc, abcd float64
	}{
		{agg: AggFirst, abc: 1.1428571428571428, abcd: 0.7142857142857143},
		{agg: AggMin, abc: 1.1111111111111112, abcd: 0.7142857142857143},
		{agg: AggMax, abc: 1.4285714285714286, abcd: 1.4285714285714286},
		{agg: AggAvg, abc: 1.2275132275132274, abcd: 1.0515873015873014},
	}
	for _, tt := range tests {
		p := NewSetParams(Frequent, 2)
		p.Eval = &SetEval{
			Measure: Measure{Func: liftMeasure, Dir: HigherIsBetter, Threshold: 0},
			Agg:     tt.agg,
			Order:   test1Order,
		}
		sets := mineOrFail(t, db, p)
		if len(sets) != 17 {
			t.Errorf("agg %d: %d sets, want 17", tt.agg, len(sets))
		}
		for _, s := range sets {
			var expected float64
			switch s.Items.Key() {
			case NewItemSet("a", "b", "c").Key():
				expected = tt.abc
			case NewItemSet("a", "b", "c", "d").Key():
				expected = tt.abcd
			case NewItemSet("a", "b").Key():
				expected = 50.0 / 42 // both rules have the same lift
			default:
				if len(s.Items) > 1 {
					continue
				}
				expected = 0 // singletons: constant for a (+) measure
			}
			if s.Eval != expected {
				t.Errorf("agg %d: %v eval = %v, want %v", tt.agg, s.Items, s.Eval, expected)
			}
		}
	}
}

// TestDB_Mine_evalTargets: with a measure, the result is the intersection
// of the target with the sets passing the threshold (C reference:
// fpgrowth -tc -s20 -el -d110 and -tm).
func TestDB_Mine_evalTargets(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	eval := &SetEval{Measure: Measure{Func: liftMeasure, Dir: HigherIsBetter, Threshold: 1.1}, Order: test1Order}
	tests := []struct {
		target   Target
		expected []string
	}{
		{target: Closed, expected: []string{"a b:5", "b c:6", "d e:3", "a b c:4"}},
		{target: Maximal, expected: []string{"d e:3"}},
	}
	for _, tt := range tests {
		p := NewSetParams(tt.target, 2)
		p.Eval = eval
		got := mineOrFail(t, db, p)
		if d := DiffSets(parseSets(t, tt.expected...), got, DiffOptions{IgnoreEval: true}); d != "" {
			t.Errorf("%v: %s", tt.target, d)
		}
	}
}

func TestDB_Mine_evalConstants(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	tests := []struct {
		name     string
		dir      Direction
		thresh   float64
		expected []string // sets of size <= 1
	}{
		// (+) measure: empty set and singletons get 0.
		{name: "higher, threshold 0", dir: HigherIsBetter, thresh: 0, expected: []string{":10", "a:6", "b:7", "c:7", "d:7", "e:3"}},
		{name: "higher, threshold 0.1", dir: HigherIsBetter, thresh: 0.1, expected: []string{}},
		// (-) measure: empty set and singletons get 1.
		{name: "lower, threshold 1", dir: LowerIsBetter, thresh: 1, expected: []string{":10", "a:6", "b:7", "c:7", "d:7", "e:3"}},
		{name: "lower, threshold 0.99", dir: LowerIsBetter, thresh: 0.99, expected: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewSetParams(Frequent, 2)
			p.MinSize, p.MaxSize = 0, 1
			p.Eval = &SetEval{Measure: Measure{Func: liftMeasure, Dir: tt.dir, Threshold: tt.thresh}}
			got := mineOrFail(t, db, p)
			if d := DiffSets(parseSets(t, tt.expected...), got, DiffOptions{IgnoreEval: true}); d != "" {
				t.Error(d)
			}
			for _, s := range got {
				if s.Eval != tt.dir.worst() {
					t.Errorf("%v eval = %v, want %v", s.Items, s.Eval, tt.dir.worst())
				}
			}
		})
	}
}

// TestDB_Mine_evalNaN checks the C semantics of NaN rule values: a NaN
// first value sticks through min/max, later NaNs are ignored, the average
// propagates NaN, and sets with a NaN value are not reported.
func TestDB_Mine_evalNaN(t *testing.T) {
	t.Parallel()
	// supp(a) = 5, supp(b) = 4, supp(c) = 3; every rule of {a b c} has s = 3.
	db := mustNew(t, parseTxs("a b c\na b c\na b c\na b\na"), Options{})
	nanForA := func(_, _, head, _ tract.Supp) float64 {
		if head == 5 {
			return math.NaN()
		}
		return float64(head)
	}
	tests := []struct {
		name     string
		order    []string
		agg      Aggregation
		expected []string // reported sets of size 3 as "a b c:value"
		value    float64
	}{
		{name: "nan first, first", order: []string{"a", "b", "c"}, agg: AggFirst},
		{name: "nan first, min", order: []string{"a", "b", "c"}, agg: AggMin},
		{name: "nan first, max", order: []string{"a", "b", "c"}, agg: AggMax},
		{name: "nan first, avg", order: []string{"a", "b", "c"}, agg: AggAvg},
		{name: "nan last, first", order: []string{"c", "b", "a"}, agg: AggFirst, expected: []string{"a b c:3"}, value: 3},
		{name: "nan last, min", order: []string{"c", "b", "a"}, agg: AggMin, expected: []string{"a b c:3"}, value: 3},
		{name: "nan last, max", order: []string{"c", "b", "a"}, agg: AggMax, expected: []string{"a b c:3"}, value: 4},
		{name: "nan last, avg", order: []string{"c", "b", "a"}, agg: AggAvg},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewSetParams(Frequent, 1)
			p.MinSize = 3
			p.Eval = &SetEval{
				Measure: Measure{Func: nanForA, Dir: HigherIsBetter, Threshold: math.Inf(-1)},
				Agg:     tt.agg,
				Order:   tt.order,
			}
			got := mineOrFail(t, db, p)
			expected := parseSets(t, tt.expected...)
			for i := range expected {
				expected[i].Eval = tt.value
			}
			if d := DiffSets(expected, got, DiffOptions{}); d != "" {
				t.Error(d)
			}
		})
	}
}

func TestDB_Mine_invalidateBelowExpected(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	p := NewSetParams(Frequent, 2)
	p.MinSize = 2
	p.Eval = &SetEval{
		Measure: Measure{Func: liftMeasure, Dir: HigherIsBetter, InvalidateBelowExpected: true},
		Agg:     AggMax,
		Order:   test1Order,
	}
	got := mineOrFail(t, db, p)
	for _, s := range got {
		// With AggMax the value is positive iff some rule has s*n > h*b,
		// i.e. lift > 1; all other values are replaced by 0.
		m, _ := db.mask(s.Items)
		anyAbove := false
		for r := m; r != 0; r &= r - 1 {
			bit := r & -r
			anyAbove = anyAbove || liftMeasure(db.supp[m], db.supp[m&^bit], db.supp[bit], db.total) > 1
		}
		if (s.Eval > 0) != anyAbove {
			t.Errorf("%v eval = %v, some lift > 1: %v", s.Items, s.Eval, anyAbove)
		}
	}
}

func TestDB_Mine_invalidParams(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	valid := Measure{Func: liftMeasure, Dir: HigherIsBetter}
	tests := []struct {
		name   string
		modify func(p *SetParams)
	}{
		{name: "target", modify: func(p *SetParams) { p.Target = Generators + 1 }},
		{name: "negative min size", modify: func(p *SetParams) { p.MinSize = -1 }},
		{name: "negative max size", modify: func(p *SetParams) { p.MaxSize = -1 }},
		{name: "measure without function", modify: func(p *SetParams) { p.Eval = &SetEval{Measure: Measure{Dir: HigherIsBetter}} }},
		{name: "measure without direction", modify: func(p *SetParams) { p.Eval = &SetEval{Measure: Measure{Func: liftMeasure}} }},
		{name: "aggregation", modify: func(p *SetParams) { p.Eval = &SetEval{Measure: valid, Agg: AggAvg + 1} }},
		{name: "item missing from order", modify: func(p *SetParams) { p.Eval = &SetEval{Measure: valid, Order: []string{"a", "b"}} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewSetParams(Frequent, 2)
			tt.modify(&p)
			if _, err := db.Mine(p); err == nil {
				t.Errorf("Mine(%+v) succeeded, want an error", p)
			}
		})
	}
}

func TestDB_predicates(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	tests := []struct {
		set                       ItemSet
		frequent, closed, maximal bool
		generator                 bool
	}{
		{set: NewItemSet(), frequent: true, closed: true, maximal: false, generator: true},
		{set: NewItemSet("e"), frequent: true, closed: false, maximal: false, generator: true},
		{set: NewItemSet("d", "e"), frequent: true, closed: true, maximal: true, generator: false},
		{set: NewItemSet("a", "c", "d"), frequent: true, closed: false, maximal: false, generator: true},
		{set: NewItemSet("a", "b", "c", "d"), frequent: true, closed: true, maximal: true, generator: false},
		{set: NewItemSet("a", "e"), frequent: false, closed: false, maximal: false, generator: true},
		{set: NewItemSet("b", "e"), frequent: false, closed: false, maximal: false, generator: true},
		{set: NewItemSet("x"), frequent: false, closed: false, maximal: false, generator: false},
	}
	for _, tt := range tests {
		if got := db.IsFrequent(tt.set, 2); got != tt.frequent {
			t.Errorf("IsFrequent(%v, 2) = %v", tt.set, got)
		}
		if got := db.IsClosed(tt.set); got != tt.closed {
			t.Errorf("IsClosed(%v) = %v", tt.set, got)
		}
		if got := db.IsMaximal(tt.set, 2); got != tt.maximal {
			t.Errorf("IsMaximal(%v, 2) = %v", tt.set, got)
		}
		if got := db.IsGenerator(tt.set); got != tt.generator {
			t.Errorf("IsGenerator(%v) = %v", tt.set, got)
		}
	}
}

// TestDB_Mine_properties checks lattice properties on random datasets.
func TestDB_Mine_properties(t *testing.T) {
	t.Parallel()
	forEachRandomDB(t, 2000, func(t *testing.T, txs []Transaction, db *DB) {
		positiveWeights := true
		for _, tx := range txs {
			positiveWeights = positiveWeights && tx.Weight > 0
		}
		for smin := tract.Supp(1); smin <= 3; smin++ {
			checkLattice(t, txs, db, smin, positiveWeights)
		}
	})
}

// checkLattice verifies the relations between the targets for one smin.
func checkLattice(t *testing.T, txs []Transaction, db *DB, smin tract.Supp, positiveWeights bool) {
	t.Helper()
	mine := func(target Target) map[uint64]bool {
		p := NewSetParams(target, smin)
		p.MinSize = 0
		out := map[uint64]bool{}
		for _, s := range mineOrFail(t, db, p) {
			m, ok := db.mask(s.Items)
			if !ok || db.supp[m] != s.Supp {
				t.Fatalf("smin %d %v: bad set %v", smin, target, s)
			}
			out[m] = true
		}
		return out
	}
	frequent, closed, maximal, generators := mine(Frequent), mine(Closed), mine(Maximal), mine(Generators)

	for m := range uint64(len(db.supp)) {
		if isFreq := db.supp[m] >= smin; isFreq != frequent[m] {
			t.Fatalf("smin %d: frequent(%v) = %v, support %d", smin, db.set(m), frequent[m], db.supp[m])
		}
	}
	for m := range closed {
		if !frequent[m] {
			t.Fatalf("smin %d: closed %v is not frequent", smin, db.set(m))
		}
		if positiveWeights && m != db.intersection(txs, m) {
			t.Fatalf("smin %d: closed %v is not the intersection of its transactions", smin, db.set(m))
		}
	}
	for m := range maximal {
		if !closed[m] {
			t.Fatalf("smin %d: maximal %v is not closed", smin, db.set(m))
		}
	}
	for m := range frequent {
		// Some maximal superset exists (unless m is the empty set and no
		// item is frequent), and the support of m is the largest support
		// of its closed supersets.
		hasMaximal := m == 0 && len(maximal) == 0
		var best tract.Supp = -1
		for c := range maximal {
			hasMaximal = hasMaximal || c&m == m
		}
		for c := range closed {
			if c&m == m {
				best = max(best, db.supp[c])
			}
		}
		if !hasMaximal {
			t.Fatalf("smin %d: frequent %v has no maximal superset", smin, db.set(m))
		}
		if best != db.supp[m] {
			t.Fatalf("smin %d: supp(%v) = %d, max closed superset support %d", smin, db.set(m), db.supp[m], best)
		}
		// Some generator subset has the same support, and its closure is
		// closed with the same support.
		hasGenerator := false
		for g := range generators {
			hasGenerator = hasGenerator || (g&m == g && db.supp[g] == db.supp[m])
		}
		if !hasGenerator {
			t.Fatalf("smin %d: frequent %v has no generator subset with equal support", smin, db.set(m))
		}
	}
	for g := range generators {
		if !frequent[g] {
			t.Fatalf("smin %d: generator %v is not frequent", smin, db.set(g))
		}
		c := db.closure(g)
		if !closed[c] || db.supp[c] != db.supp[g] {
			t.Fatalf("smin %d: closure %v of generator %v is not closed with equal support", smin, db.set(c), db.set(g))
		}
		for r := g; r != 0; r &= r - 1 {
			if !generators[g&^(r&-r)] {
				t.Fatalf("smin %d: generator %v has a non-generator subset", smin, db.set(g))
			}
		}
	}

	// Gates filter the target results and nothing else.
	for _, target := range []Target{Frequent, Closed, Maximal, Generators} {
		all := mine(target)
		p := SetParams{Target: target, MinSupp: smin, MaxSupp: smin + 2, MinSize: 1, MaxSize: 2, Border: []tract.Supp{0, 0, smin + 1}}
		for _, s := range mineOrFail(t, db, p) {
			m, _ := db.mask(s.Items)
			if !all[m] {
				t.Fatalf("smin %d %v: gated result %v not in the ungated result", smin, target, s)
			}
			delete(all, m)
		}
		for m := range all {
			size := bits.OnesCount64(m)
			passes := db.supp[m] <= smin+2 && size >= 1 && size <= 2 && (size != 2 || db.supp[m] >= smin+1)
			if passes {
				t.Fatalf("smin %d %v: %v passes the gates but is missing", smin, target, db.set(m))
			}
		}
	}
}

// closure returns the support closure of m: m plus every item whose
// addition keeps the support.
func (db *DB) closure(m uint64) uint64 {
	c := m
	for r := db.full() &^ m; r != 0; r &= r - 1 {
		if db.supp[m|r&-r] == db.supp[m] {
			c |= r & -r
		}
	}
	return c
}

// intersection returns the items common to all transactions containing m
// (the full universe if there are none), computed from the raw data.
func (db *DB) intersection(txs []Transaction, m uint64) uint64 {
	common := db.full()
	for _, tx := range txs {
		var t uint64
		for _, item := range tx.Items {
			t |= 1 << db.index[item]
		}
		if t&m == m {
			common &= t
		}
	}
	return common
}

// ExampleDB_Mine mines the closed item sets with a minimum support of 50%.
func ExampleDB_Mine() {
	txs := []Transaction{
		{Items: []string{"a", "b", "c"}, Weight: 1},
		{Items: []string{"b", "a"}, Weight: 2},
		{Items: []string{"c"}, Weight: 1},
	}
	db, err := New(txs, Options{})
	if err != nil {
		panic(err)
	}
	sets, err := db.Mine(NewSetParams(Closed, AbsMinSupp(50, db.Total())))
	if err != nil {
		panic(err)
	}
	for _, s := range sets {
		fmt.Println(s)
	}
	// Output:
	// {"c"} supp=2
	// {"a" "b"} supp=3
}

// TestDB_Mine_upstreamBugReproducers runs the reproducers of
// fpgrowth-core.md B4 and B5; expected results follow the definitions.
func TestDB_Mine_upstreamBugReproducers(t *testing.T) {
	t.Parallel()
	weighted := func(rows ...Transaction) []Transaction { return rows }
	tests := []struct {
		name     string
		txs      []Transaction
		target   Target
		smin     tract.Supp
		zmax     int
		expected []string
	}{
		{
			// B4: upstream -tm -s-2 prints nothing.
			name: "B4 maximal with only top-level pex", txs: parseTxs("a\na b\na c\na"),
			target: Maximal, smin: 2, zmax: NoMaxSize, expected: []string{"a:4"},
		},
		{
			// B5: upstream -w -tc -s-1 -n2 (-As -l0, -At, ...) adds i1 i2 (4).
			name: "B5 closed with zmax",
			txs: weighted(
				Transaction{Items: []string{"i0", "i3", "i2", "i1", "x"}, Weight: 2},
				Transaction{Items: []string{"i0", "i3", "i2", "i1", "y"}, Weight: 2},
				Transaction{Items: []string{"i0", "i3"}, Weight: 5},
				Transaction{Items: []string{"i0"}, Weight: 3},
				Transaction{Items: []string{"i2", "z"}, Weight: 1},
			),
			target: Closed, smin: 1, zmax: 2, expected: []string{"i0:12", "i2:5", "i0 i3:9", "i2 z:1"},
		},
		{
			// B5: upstream -tm -s-1 -n2 -At -x prints i3 i7 (2).
			name: "B5 maximal with zmax", txs: parseTxs("\ni5 i2 i0 i7 i3\ni2\ni1\ni1 i3 i7 i8\ni0 i5 i6"),
			target: Maximal, smin: 1, zmax: 2, expected: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := mustNew(t, tt.txs, Options{})
			p := NewSetParams(tt.target, tt.smin)
			p.MaxSize = tt.zmax
			if d := DiffSets(parseSets(t, tt.expected...), mineOrFail(t, db, p), DiffOptions{}); d != "" {
				t.Error(d)
			}
		})
	}
}
