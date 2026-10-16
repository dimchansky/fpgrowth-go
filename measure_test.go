package fpgrowth

import (
	"context"
	"math"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
)

// TestEvaluationWithoutFilter checks that an Evaluation without Filter
// reports exactly the results of the run without an evaluation, with the
// values that a filtering run with a permissive threshold reports.
func TestEvaluationWithoutFilter(t *testing.T) {
	t.Parallel()
	for seed := range uint64(numCases(100)) {
		c, err := genTestCase(seed+9000, 8)
		if err != nil {
			t.Fatal(err)
		}
		tg := targets[seed%uint64(len(targets))]
		measures := []Measure{Lift, Chi2PValue, FisherProb, Certainty}
		if tg.target == TargetFrequent {
			measures = append(measures, LogSupportRatio)
		}
		plainOpt := c.options(tg.target, AlgorithmAuto, Tuning{})
		plain, _, err := CollectItemsets(context.Background(), sourceOf(c.txs), plainOpt)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range measures {
			opt := plainOpt
			opt.Evaluation = &Evaluation{Measure: m, InvalidateBelowExpected: m != LogSupportRatio && seed%2 == 0}
			got, _, err := CollectItemsets(context.Background(), sourceOf(c.txs), opt)
			if err != nil {
				t.Fatal(err)
			}
			if d := oracle.DiffSets(toOracleSets(plain), toOracleSets(got), oracle.DiffOptions{IgnoreEval: true}); d != "" {
				t.Fatalf("seed %d %s %v: %s", seed, tg.name, m, d)
			}
			// a filter that passes everything that is not NaN gives the same
			// values (frequent sets only: for the other targets the filter
			// changes which sets qualify, see Evaluation)
			if tg.target != TargetFrequent {
				continue
			}
			loose := *opt.Evaluation
			loose.Filter, loose.Threshold = true, math.Inf(-1)
			if !m.HigherIsBetter() {
				loose.Threshold = math.Inf(1)
			}
			opt.Evaluation = &loose
			filtered, _, err := CollectItemsets(context.Background(), sourceOf(c.txs), opt)
			if err != nil {
				t.Fatal(err)
			}
			byKey := map[string]float64{}
			for _, s := range toOracleSets(filtered) {
				byKey[s.Items.String()] = s.Eval
			}
			for _, s := range toOracleSets(got) {
				v, ok := byKey[s.Items.String()]
				if !ok && !math.IsNaN(s.Eval) || ok && math.Float64bits(v) != math.Float64bits(s.Eval) {
					t.Fatalf("seed %d %v %v: value %v, filtered run %v (%v)", seed, m, s.Items, s.Eval, v, ok)
				}
			}
		}
		rplain, _, err := CollectRules(context.Background(), sourceOf(c.txs), RuleOptions[string]{MinSupport: c.minSupp})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range []Measure{Lift, Chi2PValue, Imbalance} {
			got, _, err := CollectRules(context.Background(), sourceOf(c.txs),
				RuleOptions[string]{MinSupport: c.minSupp, Evaluation: &Evaluation{Measure: m}})
			if err != nil {
				t.Fatal(err)
			}
			if d := oracle.DiffRules(toOracleRules(rplain), toOracleRules(got), oracle.DiffOptions{IgnoreEval: true}); d != "" {
				t.Fatalf("seed %d rules %v: %s", seed, m, d)
			}
			for _, r := range got {
				if math.IsNaN(r.Value) && m == Lift && r.BodySupport > 0 && r.HeadSupport > 0 {
					t.Fatalf("seed %d: rule %v without a value", seed, r)
				}
			}
		}
	}
}
