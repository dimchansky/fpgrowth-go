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
// public API (docs/audit/algorithms.md): ALG-1 (statistical measures and
// -i for large total weights, differences.md N1, issue #16) and ALG-2
// (relative (1-ε) threshold tolerances beyond 2^52, N2, issue #17). Each
// check returns "" if the result is the exact (definition-based) one and
// a description of the deviation otherwise. TestKnownAPIFailures requires
// every entry to deviate: an entry that passes must be removed by the fix.
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

var knownAPIFailures = []knownAPIFailure{
	{"ALG-1", "chi_big: s=3k-2 b=3k h=9k-2 n=9k, k=1e15 (audit CLI reproduction)", func(t *testing.T) string {
		txs := []oracle.Transaction{tx(2999999999999998, "x", "y"), tx(2, "x"), tx(6000000000000000, "y")}
		return measureDeviations(t, txs, "y", "x", 2999999999999998, 3e15, 8999999999999998, 9e15, false)
	}},
	{"ALG-1", "perfect association x y M / q 3 (audit CLI reproduction)", func(t *testing.T) string {
		const m = 9223372036854775800
		txs := []oracle.Transaction{tx(m, "x", "y"), tx(3, "q")}
		return measureDeviations(t, txs, "y", "x", m, m, m, m+3, false)
	}},
	{"ALG-1", "certainty with confidence 1 and prior 1-2/M", func(t *testing.T) string {
		const m = 9223372036854775800
		txs := []oracle.Transaction{tx(2, "x", "y"), tx(m-4, "y"), tx(2)}
		return measureDeviations(t, txs, "y", "x", 2, 2, m-2, m, false)
	}},
	{"ALG-1", "-i: s·n = h·b + 9e18 exceeds 2^53 and float64 rounds it to equality", func(t *testing.T) string {
		// s = 1e18+1, b = h = 3e18, n = 9e18: s·n > h·b exactly, but
		// float64(s)·float64(n) == float64(h)·float64(b), so the rule is
		// invalidated (value 0 instead of its lift ≈ 1).
		txs := []oracle.Transaction{tx(1e18+1, "x", "y"), tx(2e18-1, "x"), tx(2e18-1, "y"), tx(4e18 + 1)}
		return measureDeviations(t, txs, "y", "x", 1e18+1, 3e18, 3e18, 9e18, true)
	}},
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

// wantKnownAPIDeviations pins the measures that deviate in the ALG-1
// entries (the letters before ':'), so that a partial fix is noticed.
var wantKnownAPIDeviations = map[int]string{
	0: "ptgfhm",
	1: "znpytgfhms",
	2: "zpgs",
	3: "oclvkwuptg", // the measures whose value differs from their worst value
}

// TestKnownAPIFailures checks that every known defect of knownAPIFailures
// still deviates from the exact result (and, for ALG-1, in exactly the
// listed measures). The suite reports them and keeps running.
func TestKnownAPIFailures(t *testing.T) {
	t.Parallel()
	for i, kf := range knownAPIFailures {
		dev := kf.check(t)
		if dev == "" {
			t.Errorf("known failure %s %q no longer deviates: remove it from knownAPIFailures", kf.id, kf.name)
			continue
		}
		t.Logf("known failure %s %q: %s", kf.id, kf.name, dev)
		if want, ok := wantKnownAPIDeviations[i]; ok {
			var letters string
			for _, part := range strings.Split(dev, "; ") {
				letters += part[:1]
			}
			if letters != want {
				t.Errorf("known failure %s %q: deviating measures %q, listed %q", kf.id, kf.name, letters, want)
			}
		}
	}
}
