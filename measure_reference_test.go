package fpgrowth

import (
	"context"
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Tests of the public evaluation against the independent reference
// measures of internal/oracle (issue #15, audit TST-3).

// TestMeasureReferenceNames checks that every public rule measure has a
// reference measure of the same name and direction.
func TestMeasureReferenceNames(t *testing.T) {
	t.Parallel()
	refs := oracle.RefMeasures()
	if len(refs) != int(LogSupportRatio)-1 {
		t.Fatalf("%d reference measures, %d public rule measures", len(refs), int(LogSupportRatio)-1)
	}
	for m := RuleSupport; m < LogSupportRatio; m++ {
		ref, ok := oracle.RefByName(m.String())
		if !ok {
			t.Errorf("%v: no reference measure", m)
			continue
		}
		if (ref.Dir > 0) != m.HigherIsBetter() {
			t.Errorf("%v: reference direction %d, HigherIsBetter %v", m, ref.Dir, m.HigherIsBetter())
		}
		if refs[m-RuleSupport] != ref {
			t.Errorf("%v: reference measure %s at its position", m, refs[m-RuleSupport].Name)
		}
	}
}

// TestOracleLogSupportRatio compares item sets evaluated with
// LogSupportRatio (values and filter) with the oracle's reference.
func TestOracleLogSupportRatio(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(15, 27))
	thresholds := []float64{-10, -0.5, 0, 0.1, 0.5, 1, 3}
	for seed := range uint64(numCases(300)) {
		c, err := genTestCase(seed+13000, 8)
		if err != nil {
			t.Fatal(err)
		}
		tg := targets[r.IntN(len(targets))]
		v := algorithms[r.IntN(len(algorithms))]
		ev := &Evaluation{Measure: LogSupportRatio}
		if r.IntN(4) != 0 {
			ev.Filter, ev.Threshold = true, thresholds[r.IntN(len(thresholds))]
		}
		opt := c.options(tg.target, v.algo, v.tun)
		opt.Evaluation = ev
		got, sum, err := CollectItemsets(context.Background(), sourceOf(c.txs), opt)
		if err != nil {
			t.Fatal(err)
		}
		if sum.FrequentItems == 0 {
			continue
		}
		db, err := oracle.New(c.txs, oracle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		total := db.Total()
		p := oracle.NewSetParams(tg.oracle, oracle.AbsMinSupp(c.smin, total))
		p.MaxSupp = oracle.AbsMaxSupp(c.smax, total)
		p.MinSize = c.zmin
		if c.zmax < fpg.MaxSize {
			p.MaxSize = c.zmax
		}
		p.Border = oracle.AbsBorder(c.border, c.zmin, total)
		p.LogRatio = &oracle.LogRatioEval{Threshold: math.Inf(-1)}
		if ev.Filter {
			p.LogRatio.Threshold = ev.Threshold
		}
		want, err := db.Mine(p)
		if err != nil {
			t.Fatal(err)
		}
		if d := oracle.DiffSets(want, toOracleSets(got), oracle.DiffOptions{}); d != "" {
			t.Fatalf("seed %d %s/%s eval %+v:\n%s", seed, tg.name, v.name, *ev, d)
		}
	}
}

// knownAPIFailure is a known numerical defect reproduced through the
// public API (docs/audit/algorithms.md): ALG-2 (relative (1-ε) threshold
// tolerances beyond 2^52, N2, issue #17). Each check returns "" if the
// result is the exact (definition-based) one and a description of the
// deviation otherwise. TestKnownAPIFailures requires every entry to
// deviate: an entry that passes must be removed by the fix. (The ALG-1
// entries of issue #15 became the regression tests of TestALG1API.)
type knownAPIFailure struct {
	id, name string
	check    func(t *testing.T) string
}

// ruleValues mines the rules of size 2 with confidence 0 and returns the
// value of rule head <- body for each of the given measures (Filter off,
// so nothing is filtered).
func ruleValues(t *testing.T, txs []oracle.Transaction, head, body string, measures []Measure, invalidate bool) map[Measure]float64 {
	t.Helper()
	vals := map[Measure]float64{}
	for _, m := range measures {
		opt := RuleOptions[string]{MinSupport: Count(1), MinSize: 2, MaxSize: 2,
			Evaluation: &Evaluation{Measure: m, InvalidateBelowExpected: invalidate}}
		rules, _, err := CollectRules(context.Background(), sourceOf(txs), opt)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rules {
			if r.Head == head && slices.Equal(r.Body, []string{body}) {
				vals[m] = r.Value
			}
		}
		if _, ok := vals[m]; !ok {
			t.Fatalf("%v: rule %s <- %s not reported", m, head, body)
		}
	}
	return vals
}

// measureDeviations returns the letters of the measures whose value for
// the rule head <- body (with supports s, b, h, n) differs from the
// reference, with details. Measures outside the reference's scope (Fisher
// tests over more than oracle.FisherMaxTables tables) are skipped.
func measureDeviations(t *testing.T, txs []oracle.Transaction, head, body string, s, b, h, n int64, invalidate bool) string {
	t.Helper()
	var dev []string
	for m := RuleSupport; m < LogSupportRatio; m++ {
		ref, _ := oracle.RefByName(m.String())
		want, err := ref.Eval(tract.Supp(s), tract.Supp(b), tract.Supp(h), tract.Supp(n))
		if errors.Is(err, oracle.ErrOutOfScope) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if invalidate && new(big.Int).Mul(big.NewInt(s), big.NewInt(n)).Cmp(new(big.Int).Mul(big.NewInt(h), big.NewInt(b))) <= 0 {
			want = oracle.Value{V: float64(max(0, -int(ref.Dir)))} // worst value: 0 or 1
		}
		got := ruleValues(t, txs, head, body, []Measure{m}, invalidate)[m]
		if !want.Matches(got) {
			dev = append(dev, string(ref.Letter)+": "+formatDev(got, want))
		}
	}
	return strings.Join(dev, "; ")
}

func formatDev(got float64, want oracle.Value) string {
	return "got " + strings.TrimSpace(big.NewFloat(got).Text('g', 10)) + ", reference " + want.String()
}

// tx builds a weighted transaction.
func tx(w int64, items ...string) oracle.Transaction {
	return oracle.Transaction{Items: items, Weight: tract.Supp(w)}
}

// TestALG6Regression checks audit ALG-6 (issue #16): the G-test p-value
// of an exactly independent table is exactly 1 (it was 0.99999996), and
// the strict comparison with the reference passes. The fuzz corpus entry
// testdata/fuzz/FuzzMineRules/5fc53b9aa57af1cd is the same case found by
// fuzzing.
func TestALG6Regression(t *testing.T) {
	t.Parallel()
	// a in 4 of 6, b in 3, a b in 2: s·n = 12 = b·h
	txs := []oracle.Transaction{tx(2, "a", "b"), tx(2, "a"), tx(1, "b"), tx(1)}
	ev := &Evaluation{Measure: InformationPValue}
	got, _, err := CollectRules(context.Background(), sourceOf(txs),
		RuleOptions[string]{MinSupport: Count(1), MinSize: 2, Evaluation: ev})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no rules")
	}
	for _, r := range got {
		if r.Value != 1 {
			t.Errorf("rule %s <- %v: InformationPValue %v, want exactly 1", r.Head, r.Body, r.Value)
		}
	}
	db, err := oracle.New(txs, oracle.Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := oracle.NewRuleParams(1, 0)
	p.MinSize = 2
	p.Eval = refMeasure(t, ev)
	want, err := db.Rules(p)
	if err != nil {
		t.Fatal(err)
	}
	if d := oracle.DiffRules(want, toOracleRules(got), oracle.DiffOptions{}); d != "" {
		t.Errorf("strict comparison: %s", d)
	}
}

// alg1APICase is a regression case of audit ALG-1 (issue #16) through the
// public API: the audit's CLI reproductions and the -i case of the #15
// known-failure table. Every measure (and -i) must match the reference
// within its documented bound.
type alg1APICase struct {
	name       string
	txs        []oracle.Transaction
	head, body string
	s, b, h, n int64
	invalidate bool
}

var alg1APICases = []alg1APICase{
	{"chi_big: s=3k-2 b=3k h=9k-2 n=9k, k=1e15 (audit CLI reproduction)",
		[]oracle.Transaction{tx(2999999999999998, "x", "y"), tx(2, "x"), tx(6000000000000000, "y")},
		"y", "x", 2999999999999998, 3e15, 8999999999999998, 9e15, false},
	{"perfect association x y M / q 3 (audit CLI reproduction)",
		[]oracle.Transaction{tx(9223372036854775800, "x", "y"), tx(3, "q")},
		"y", "x", 9223372036854775800, 9223372036854775800, 9223372036854775800, 9223372036854775803, false},
	{"certainty with confidence 1 and prior 1-2/M",
		[]oracle.Transaction{tx(2, "x", "y"), tx(9223372036854775800-4, "y"), tx(2)},
		"y", "x", 2, 2, 9223372036854775800 - 2, 9223372036854775800, false},
	// s = 1e18+1, b = h = 3e18, n = 9e18: s·n > h·b exactly, but
	// float64(s)·float64(n) == float64(h)·float64(b), so upstream's test
	// invalidated the rule (value 0 instead of its lift ≈ 1).
	{"-i: s·n = h·b + 9e18 exceeds 2^53 and float64 rounds it to equality",
		[]oracle.Transaction{tx(1e18+1, "x", "y"), tx(2e18-1, "x"), tx(2e18-1, "y"), tx(4e18 + 1)},
		"y", "x", 1e18 + 1, 3e18, 3e18, 9e18, true},
}

// TestALG1API checks the ALG-1 regression cases (they were known failures
// in all of n p y t i g z f h m s, and -i, before issue #16).
func TestALG1API(t *testing.T) {
	t.Parallel()
	for _, c := range alg1APICases {
		if d := measureDeviations(t, c.txs, c.head, c.body, c.s, c.b, c.h, c.n, c.invalidate); d != "" {
			t.Errorf("%s: %s", c.name, d)
		}
	}
}

var knownAPIFailures = []knownAPIFailure{
	{"ALG-2", "-s50 admits a set with relative support 49.999999999999992895 %", func(t *testing.T) string {
		// W = 2^62; exact minimum support ceil(W/2) = 2^61 > supp(a)
		txs := []oracle.Transaction{tx(2305843009213693752, "a"), tx(2305843009213694152, "b")}
		sets, _, err := CollectItemsets(context.Background(), sourceOf(txs), Options[string]{MinSupport: Fraction(0.5)})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range sets {
			if slices.Equal(s.Items, []string{"a"}) {
				return "{a} reported with support 2305843009213693752 < 2^61"
			}
		}
		return ""
	}},
	{"ALG-2", "MinConfidence 1 admits a rule with confidence < 1", func(t *testing.T) string {
		txs := []oracle.Transaction{tx(4611686018427387000, "x", "y"), tx(1000, "x")}
		rules, _, err := CollectRules(context.Background(), sourceOf(txs),
			RuleOptions[string]{MinSupport: Count(1), MinConfidence: 1, MinSize: 2})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rules {
			if r.Support < r.BodySupport { // confidence < 1 exactly
				return "rule " + r.Head + " <- " + strings.Join(r.Body, " ") + " reported with confidence < 1"
			}
		}
		return ""
	}},
}

// TestKnownAPIFailures checks that every known defect of knownAPIFailures
// still deviates from the exact result. The suite reports them and keeps
// running.
func TestKnownAPIFailures(t *testing.T) {
	t.Parallel()
	for _, kf := range knownAPIFailures {
		dev := kf.check(t)
		if dev == "" {
			t.Errorf("known failure %s %q no longer deviates: remove it from knownAPIFailures", kf.id, kf.name)
			continue
		}
		t.Logf("known failure %s %q: %s", kf.id, kf.name, dev)
	}
}
