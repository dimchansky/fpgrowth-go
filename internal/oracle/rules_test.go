package oracle

import (
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// parseRules parses expected rules written as "c <- a b:4/5/7" (support,
// body support, head support); "d <- :7/10/7" has an empty body.
func parseRules(t testing.TB, specs ...string) []Rule {
	t.Helper()
	rules := make([]Rule, 0, len(specs))
	for _, spec := range specs {
		lhs, nums, ok1 := strings.Cut(spec, ":")
		head, body, ok2 := strings.Cut(lhs, " <-")
		parts := strings.Split(nums, "/")
		if !ok1 || !ok2 || len(parts) != 3 {
			t.Fatalf("bad rule spec %q", spec)
		}
		var supps [3]tract.Supp
		for i, p := range parts {
			n, err := strconv.ParseInt(p, 10, 64)
			if err != nil {
				t.Fatalf("bad rule spec %q", spec)
			}
			supps[i] = tract.Supp(n)
		}
		rules = append(rules, Rule{
			Head:     strings.TrimSpace(head),
			Body:     NewItemSet(strings.Fields(body)...),
			Supp:     supps[0],
			BodySupp: supps[1],
			HeadSupp: supps[2],
		})
	}
	return rules
}

// rulesOrFail runs Rules or fails the test.
func rulesOrFail(t testing.TB, db *DB, p RuleParams) []Rule {
	t.Helper()
	rules, err := db.Rules(p)
	if err != nil {
		t.Fatalf("Rules(%+v): %v", p, err)
	}
	return rules
}

// test1Rules40x70 are the rules of test1 for -s40 -c70 (body minimum 4,
// confidence 0.7), by hand; fpgrowth -tr -s40 -c70 -v' (%a %b %h)' agrees.
var test1Rules40x70 = []string{
	"d <- :7/10/7", "c <- :7/10/7", "b <- :7/10/7",
	"c <- b:6/7/7", "b <- c:6/7/7", "b <- a:5/6/7", "a <- b:5/7/6",
	"c <- b d:3/4/7", "b <- c d:3/4/7", "b <- a d:3/4/7", "a <- b d:3/4/6",
	"c <- a b:4/5/7", "b <- a c:4/4/7",
}

func TestDB_Rules_test1(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	bodyMin := AbsMinSupp(40, db.Total())
	tests := []struct {
		name     string
		modify   func(p *RuleParams)
		expected []string
	}{
		{name: "-s40 -c70", modify: func(*RuleParams) {}, expected: test1Rules40x70},
		{
			// -o: the minimum support 4 applies to body ∪ head (upstream: 9 rules).
			name:   "-s40 -c70 -o",
			modify: func(p *RuleParams) { p.OrigSupp = true },
			expected: []string{
				"d <- :7/10/7", "c <- :7/10/7", "b <- :7/10/7",
				"c <- b:6/7/7", "b <- c:6/7/7", "b <- a:5/6/7", "a <- b:5/7/6",
				"c <- a b:4/5/7", "b <- a c:4/4/7",
			},
		},
		{
			// Fixed B8: the maximum 6 applies to the body support. Upstream
			// applies it to body ∪ head and reports 10 rules.
			name:   "-s40 -S60 -c70",
			modify: func(p *RuleParams) { p.MaxSupp = AbsMaxSupp(60, db.Total()) },
			expected: []string{
				"b <- a:5/6/7", "c <- b d:3/4/7", "b <- c d:3/4/7", "b <- a d:3/4/7", "a <- b d:3/4/6",
				"c <- a b:4/5/7", "b <- a c:4/4/7",
			},
		},
		{
			name: "-s40 -S60 -c70 -o",
			modify: func(p *RuleParams) {
				p.OrigSupp = true
				p.MaxSupp = AbsMaxSupp(60, db.Total())
			},
			expected: []string{
				"c <- b:6/7/7", "b <- c:6/7/7", "b <- a:5/6/7", "a <- b:5/7/6", "c <- a b:4/5/7", "b <- a c:4/4/7",
			},
		},
		{
			name:     "-s40 -c70 -m2 -n2",
			modify:   func(p *RuleParams) { p.MinSize, p.MaxSize = 2, 2 },
			expected: []string{"c <- b:6/7/7", "b <- c:6/7/7", "b <- a:5/6/7", "a <- b:5/7/6"},
		},
		{
			name:   "-s40 -c70 -m3",
			modify: func(p *RuleParams) { p.MinSize = 3 },
			expected: []string{
				"c <- b d:3/4/7", "b <- c d:3/4/7", "b <- a d:3/4/7", "a <- b d:3/4/6", "c <- a b:4/5/7", "b <- a c:4/4/7",
			},
		},
		{
			// Head-only a (fixed B3): a never in a body. Upstream also
			// prints b <- a d and b <- a c.
			name:   "-s40 -c70 head-only a",
			modify: func(p *RuleParams) { p.Appear = map[string]Appearance{"a": AppearHead} },
			expected: []string{
				"d <- :7/10/7", "c <- :7/10/7", "b <- :7/10/7", "c <- b:6/7/7", "b <- c:6/7/7", "a <- b:5/7/6",
				"c <- b d:3/4/7", "b <- c d:3/4/7", "a <- b d:3/4/6",
			},
		},
		{
			name:   "-s40 -c70 body-only b",
			modify: func(p *RuleParams) { p.Appear = map[string]Appearance{"b": AppearBody} },
			expected: []string{
				"d <- :7/10/7", "c <- :7/10/7", "c <- b:6/7/7", "a <- b:5/7/6",
				"c <- b d:3/4/7", "a <- b d:3/4/6", "c <- a b:4/5/7",
			},
		},
		{
			name:   "-s40 -c70 ignored d",
			modify: func(p *RuleParams) { p.Appear = map[string]Appearance{"d": AppearNone} },
			expected: []string{
				"c <- :7/10/7", "b <- :7/10/7", "c <- b:6/7/7", "b <- c:6/7/7", "b <- a:5/6/7", "a <- b:5/7/6",
				"c <- a b:4/5/7", "b <- a c:4/4/7",
			},
		},
		{
			name: "-s40 -c70 default none",
			modify: func(p *RuleParams) {
				p.Appear = map[string]Appearance{"b": AppearBoth, "c": AppearBoth}
				p.DefaultAppear = AppearNone
			},
			expected: []string{"c <- :7/10/7", "b <- :7/10/7", "c <- b:6/7/7", "b <- c:6/7/7"},
		},
		{
			// Two head-only items never form a rule together.
			name:     "-s40 -c70 head-only b and c",
			modify:   func(p *RuleParams) { p.Appear = map[string]Appearance{"b": AppearHead, "c": AppearHead} },
			expected: []string{"d <- :7/10/7", "c <- :7/10/7", "b <- :7/10/7", "b <- a:5/6/7", "b <- a d:3/4/7"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewRuleParams(bodyMin, 0.7)
			tt.modify(&p)
			got := rulesOrFail(t, db, p)
			if d := DiffRules(parseRules(t, tt.expected...), got, DiffOptions{}); d != "" {
				t.Error(d)
			}
		})
	}
}

func TestDB_Rules_derivedValues(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	rules := rulesOrFail(t, db, NewRuleParams(4, 0.7))
	for _, r := range rules {
		if r.Conf != float64(r.Supp)/float64(r.BodySupp) {
			t.Errorf("%v: conf %v", r, r.Conf)
		}
		if expected := liftMeasure(r.Supp, r.BodySupp, r.HeadSupp, db.Total()); r.Lift != expected {
			t.Errorf("%v: lift %v, want %v", r, r.Lift, expected)
		}
		if !r.Items().Contains(r.Head) || len(r.Items()) != len(r.Body)+1 {
			t.Errorf("%v: Items() = %v", r, r.Items())
		}
	}
}

// TestDB_Rules_confidenceTolerance: 25*0.28 = 7.000000000000001 in float64,
// so the rule y <- x with confidence exactly 7/25 passes only thanks to
// upstream's factor (1-DBL_EPSILON).
func TestDB_Rules_confidenceTolerance(t *testing.T) {
	t.Parallel()
	txs := []Transaction{{Items: []string{"x", "y"}, Weight: 7}, {Items: []string{"x"}, Weight: 18}}
	db := mustNew(t, txs, Options{})
	if !(float64(7) < float64(25)*0.28) {
		t.Fatal("test premise: 25*0.28 should exceed 7 in float64")
	}
	tests := []struct {
		conf     float64
		expected []string
	}{
		{conf: 0.28, expected: []string{"y <- x:7/25/7"}},
		{conf: 0.2801, expected: []string{}},
	}
	for _, tt := range tests {
		p := NewRuleParams(1, tt.conf)
		p.MinSize = 2
		got := rulesOrFail(t, db, p)
		// x <- y always passes (confidence 1).
		want := parseRules(t, append(tt.expected, "x <- y:7/7/25")...)
		if d := DiffRules(want, got, DiffOptions{}); d != "" {
			t.Errorf("conf %v: %s", tt.conf, d)
		}
	}
}

func TestDB_Rules_eval(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	tests := []struct {
		name     string
		measure  Measure
		expected []string
		evals    []float64
	}{
		{
			// fpgrowth -tr -s40 -c70 -el -d110: 7 rules.
			name:     "lift >= 1.1",
			measure:  Measure{Func: liftMeasure, Dir: HigherIsBetter, Threshold: 1.1},
			expected: []string{"c <- b:6/7/7", "b <- c:6/7/7", "a <- b d:3/4/6", "c <- a b:4/5/7", "b <- a c:4/4/7", "b <- a:5/6/7", "a <- b:5/7/6"},
			evals:    []float64{60.0 / 49, 60.0 / 49, 30.0 / 24, 40.0 / 35, 40.0 / 28, 50.0 / 42, 50.0 / 42},
		},
		{
			// -i: rules at exactly the expected support (lift 1, the
			// empty-body rules) get the worst value 0 and fail.
			name:    "lift >= 1 with -i",
			measure: Measure{Func: liftMeasure, Dir: HigherIsBetter, Threshold: 1, InvalidateBelowExpected: true},
			expected: []string{
				"c <- b:6/7/7", "b <- c:6/7/7", "b <- a:5/6/7", "a <- b:5/7/6",
				"c <- b d:3/4/7", "b <- c d:3/4/7", "b <- a d:3/4/7", "a <- b d:3/4/6", "c <- a b:4/5/7", "b <- a c:4/4/7",
			},
		},
		{
			// Rules use reject-if-less, so NaN values pass any threshold.
			name:     "nan passes",
			measure:  Measure{Func: func(_, _, _, _ tract.Supp) float64 { return math.NaN() }, Dir: LowerIsBetter, Threshold: -1e300},
			expected: test1Rules40x70,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewRuleParams(4, 0.7)
			p.Eval = &tt.measure
			got := rulesOrFail(t, db, p)
			expected := parseRules(t, tt.expected...)
			for i := range expected {
				switch {
				case tt.evals != nil:
					expected[i].Eval = tt.evals[i]
				case tt.measure.InvalidateBelowExpected:
					expected[i].Eval = liftMeasure(expected[i].Supp, expected[i].BodySupp, expected[i].HeadSupp, 10)
				default:
					expected[i].Eval = math.NaN()
				}
			}
			if d := DiffRules(expected, got, DiffOptions{}); d != "" {
				t.Error(d)
			}
		})
	}
}

func TestDB_Rules_invalidParams(t *testing.T) {
	t.Parallel()
	db := mustNew(t, parseTxs(test1), Options{})
	tests := []struct {
		name   string
		modify func(p *RuleParams)
	}{
		{name: "negative size", modify: func(p *RuleParams) { p.MinSize = -1 }},
		{name: "confidence above 1", modify: func(p *RuleParams) { p.MinConf = 1.5 }},
		{name: "nan confidence", modify: func(p *RuleParams) { p.MinConf = math.NaN() }},
		{name: "appearance", modify: func(p *RuleParams) { p.Appear = map[string]Appearance{"a": AppearNone + 1} }},
		{name: "default appearance", modify: func(p *RuleParams) { p.DefaultAppear = AppearNone + 1 }},
		{name: "measure", modify: func(p *RuleParams) { p.Eval = &Measure{Dir: HigherIsBetter} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewRuleParams(2, 0.5)
			tt.modify(&p)
			if _, err := db.Rules(p); err == nil {
				t.Errorf("Rules(%+v) succeeded, want an error", p)
			}
		})
	}
}

// TestDB_Rules_properties checks rule invariants and counts on random
// datasets.
func TestDB_Rules_properties(t *testing.T) {
	t.Parallel()
	forEachRandomDB(t, 1000, func(t *testing.T, _ []Transaction, db *DB) {
		for _, smin := range []tract.Supp{1, 2} {
			for _, conf := range []float64{0, 0.5, 0.8} {
				for _, orig := range []bool{false, true} {
					p := NewRuleParams(smin, conf)
					p.OrigSupp = orig
					checkRules(t, db, p)
				}
			}
		}
	})
}

// checkRules verifies the invariants of Rules(p) and, for MinConf 0, the
// number of rules.
func checkRules(t *testing.T, db *DB, p RuleParams) {
	t.Helper()
	rules := rulesOrFail(t, db, p)
	seen := map[string]bool{}
	for _, r := range rules {
		key := RuleKey(r.Head, r.Body)
		if seen[key] {
			t.Fatalf("%+v: duplicate rule %v", p, r)
		}
		seen[key] = true
		s, b, h := db.Support(r.Items()), db.Support(r.Body), db.Support(NewItemSet(r.Head))
		switch {
		case r.Body.Contains(r.Head):
			t.Fatalf("%+v: head in body: %v", p, r)
		case r.Supp != s || r.BodySupp != b || r.HeadSupp != h:
			t.Fatalf("%+v: %v: supports want %d/%d/%d", p, r, s, b, h)
		case s < 1 || s > b || s > h:
			t.Fatalf("%+v: %v: inconsistent supports", p, r)
		case r.Conf < 0 || r.Conf > 1 || !MeetsMin(s, p.MinConf, b):
			t.Fatalf("%+v: %v: confidence %v", p, r, r.Conf)
		case !p.OrigSupp && b < p.MinSupp, p.OrigSupp && s < p.MinSupp:
			t.Fatalf("%+v: %v: below minimum support", p, r)
		}
	}
	if p.MinConf != 0 {
		return
	}
	// Count the rules independently: every (S, h) with h ∈ S, supp(S) >= 1
	// and the selected support >= smin.
	smin := EffectiveMinSupp(p.MinSupp)
	expected := 0
	for m := uint64(1); m < uint64(len(db.supp)); m++ {
		if db.supp[m] < 1 {
			continue
		}
		for r := m; r != 0; r &= r - 1 {
			selected := db.supp[m&^(r&-r)]
			if p.OrigSupp {
				selected = db.supp[m]
			}
			if selected >= smin {
				expected++
			}
		}
	}
	if len(rules) != expected {
		t.Fatalf("%+v: %d rules, want %d", p, len(rules), expected)
	}
}

// TestDB_Rules_appearanceFilters: appearances only remove rules; the rules
// they keep are exactly the unrestricted rules that satisfy them.
func TestDB_Rules_appearanceFilters(t *testing.T) {
	t.Parallel()
	forEachRandomDB(t, 1000, func(t *testing.T, _ []Transaction, db *DB) {
		r := rand.New(rand.NewPCG(uint64(len(db.items)), uint64(db.total)))
		appear := map[string]Appearance{}
		for _, item := range db.items {
			if a := Appearance(r.IntN(5)); a <= AppearNone {
				appear[item] = a
			}
		}
		def := Appearance(r.IntN(4))
		appearanceOf := func(item string) Appearance {
			if a, ok := appear[item]; ok {
				return a
			}
			return def
		}
		p := NewRuleParams(1, 0.3)
		all := rulesOrFail(t, db, p)
		p.Appear, p.DefaultAppear = appear, def
		var expected []Rule
		for _, rule := range all {
			ok := appearanceOf(rule.Head).canHead()
			for _, item := range rule.Body {
				a := appearanceOf(item)
				ok = ok && a != AppearNone && a != AppearHead
			}
			if ok {
				expected = append(expected, rule)
			}
		}
		if d := DiffRules(expected, rulesOrFail(t, db, p), DiffOptions{}); d != "" {
			t.Fatalf("appear %v default %d: %s", appear, def, d)
		}
	})
}
