package oracle

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestSortSets(t *testing.T) {
	t.Parallel()
	sets := parseSets(t, "b c:1", "a:2", ":3", "a b:4", "b:5")
	SortSets(sets)
	var keys []string
	for _, s := range sets {
		keys = append(keys, s.Items.String())
	}
	if got, expected := strings.Join(keys, " "), `{} {"a"} {"b"} {"a" "b"} {"b" "c"}`; got != expected {
		t.Errorf("SortSets order %s, want %s", got, expected)
	}
}

func TestSortRules(t *testing.T) {
	t.Parallel()
	rules := parseRules(t, "c <- b:1/1/1", "b <- c:1/1/1", "a <- :1/1/1", "b <- a c:1/1/1", "c <- a:1/1/1")
	SortRules(rules)
	var keys []string
	for _, r := range rules {
		keys = append(keys, RuleKey(r.Head, r.Body))
	}
	expected := []string{`"a" <- `, `"c" <- "a"`, `"b" <- "c"`, `"c" <- "b"`, `"b" <- "a" "c"`}
	if got := strings.Join(keys, "|"); got != strings.Join(expected, "|") {
		t.Errorf("SortRules order %s, want %s", got, strings.Join(expected, "|"))
	}
}

func TestDiffSets(t *testing.T) {
	t.Parallel()
	want := parseSets(t, "a:3", "b:2", "a b:1", "c:4")
	tests := []struct {
		name     string
		got      []Set
		opts     DiffOptions
		expected string
	}{
		{name: "equal in another order", got: parseSets(t, "c:4", "a b:1", "b:2", "a:3"), expected: ""},
		{
			name: "all kinds of differences",
			got:  parseSets(t, "a:3", "a:3", "b:5", "d:1", "c:4"),
			expected: `item sets differ (want 4, got 5): 1 missing, 1 unexpected, 1 changed, 1 duplicates
  duplicate in got:  {"a"} supp=3
  changed:    {"b"}: supp want 2, got 5
  unexpected: {"d"} supp=1
  missing:    {"a" "b"} supp=1`,
		},
		{
			name: "truncated",
			got:  []Set{},
			opts: DiffOptions{MaxDiffs: 2},
			expected: `item sets differ (want 4, got 0): 4 missing, 0 unexpected, 0 changed, 0 duplicates
  missing:    {"a"} supp=3
  missing:    {"b"} supp=2
  ... and 2 more`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DiffSets(want, tt.got, tt.opts); got != tt.expected {
				t.Errorf("DiffSets() =\n%s\nwant\n%s", got, tt.expected)
			}
		})
	}

	dupWant := DiffSets(parseSets(t, "a:1", "a:1"), parseSets(t, "a:1"), DiffOptions{})
	if !strings.Contains(dupWant, "duplicate in want") {
		t.Errorf("duplicate in want not reported: %s", dupWant)
	}
}

func TestDiffSets_eval(t *testing.T) {
	t.Parallel()
	set := func(eval float64) []Set { return []Set{{Items: NewItemSet("a"), Supp: 1, Eval: eval}} }
	nan, inf := math.NaN(), math.Inf(1)
	tests := []struct {
		name      string
		want, got float64
		opts      DiffOptions
		equal     bool
	}{
		{name: "exact", want: 1.5, got: 1.5, equal: true},
		{name: "one ulp", want: 1.5, got: math.Nextafter(1.5, 2), equal: false},
		{name: "one ulp within tolerance", want: 1.5, got: math.Nextafter(1.5, 2), opts: DiffOptions{EvalTol: 1e-12}, equal: true},
		{name: "absolute tolerance near zero", want: 0, got: 1e-13, opts: DiffOptions{EvalTol: 1e-12}, equal: true},
		{name: "relative tolerance", want: 1e6, got: 1e6 + 1e-7, opts: DiffOptions{EvalTol: 1e-12}, equal: true},
		{name: "relative tolerance exceeded", want: 1e6, got: 1e6 + 1e-5, opts: DiffOptions{EvalTol: 1e-12}, equal: false},
		{name: "nan equals nan", want: nan, got: nan, equal: true},
		{name: "nan differs from number", want: nan, got: 0, opts: DiffOptions{EvalTol: 1}, equal: false},
		{name: "infinities", want: inf, got: inf, equal: true},
		{name: "infinity differs", want: inf, got: -inf, opts: DiffOptions{EvalTol: 1}, equal: false},
		{name: "ignored", want: 1, got: 2, opts: DiffOptions{IgnoreEval: true}, equal: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if d := DiffSets(set(tt.want), set(tt.got), tt.opts); (d == "") != tt.equal {
				t.Errorf("DiffSets(eval %v, eval %v) = %q, equal want %v", tt.want, tt.got, d, tt.equal)
			}
		})
	}
}

func TestDiffRules(t *testing.T) {
	t.Parallel()
	want := parseRules(t, "b <- a:2/3/4", "a <- :3/5/3")
	if d := DiffRules(want, parseRules(t, "a <- :3/5/3", "b <- a:2/3/4"), DiffOptions{}); d != "" {
		t.Errorf("equal rules: %s", d)
	}
	got := parseRules(t, "b <- a:2/4/5", "a <- b:2/4/3")
	got[0].Eval = 1
	expected := `rules differ (want 2, got 2): 1 missing, 1 unexpected, 1 changed, 0 duplicates
  missing:    "a" <- {} supp=3 body=5 head=3
  unexpected: "a" <- {"b"} supp=2 body=4 head=3
  changed:    "b" <- {"a"}: body want 3, got 4; head want 4, got 5; eval want 0, got 1`
	if d := DiffRules(want, got, DiffOptions{}); d != expected {
		t.Errorf("DiffRules() =\n%s\nwant\n%s", d, expected)
	}
}

// ExampleDiffSets shows a failure report.
func ExampleDiffSets() {
	want := []Set{{Items: NewItemSet("a"), Supp: 3}, {Items: NewItemSet("a", "b"), Supp: 2}}
	got := []Set{{Items: NewItemSet("a"), Supp: 4}}
	fmt.Println(DiffSets(want, got, DiffOptions{}))
	// Output:
	// item sets differ (want 2, got 1): 1 missing, 0 unexpected, 1 changed, 0 duplicates
	//   changed:    {"a"}: supp want 3, got 4
	//   missing:    {"a" "b"} supp=2
}
