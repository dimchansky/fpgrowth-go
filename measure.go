package fpgrowth

import (
	"fmt"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
)

// Measure is an evaluation measure (upstream option -e). All measures
// except LogSupportRatio are rule measures: they evaluate a rule B -> h
// from the supports of B ∪ {h}, B, {h} and the total weight. Item sets are
// evaluated with a rule measure by aggregating it over the rules
// S \ {h} -> h (see Aggregation).
//
// Each measure has a direction: for most, larger values are better and
// Evaluation.Threshold is a minimum; for the p-value measures (Chi2PValue,
// YatesPValue, InformationPValue and the Fisher measures) smaller values
// are better and the threshold is a maximum. Measure.HigherIsBetter tells.
//
// The zero Measure is not a measure; an Evaluation must name one.
type Measure int

// Evaluation measures. The comment gives upstream's -e letter.
const (
	RuleSupport                 Measure = iota + 1 // o: rule support (body and head together, absolute)
	Confidence                                     // c: rule confidence
	ConfidenceDiff                                 // d: absolute confidence difference to the prior
	Lift                                           // l: lift (confidence divided by the prior)
	LiftDiff                                       // a: absolute difference of the lift to 1
	LiftQuotient                                   // q: 1 - min(lift, 1/lift)
	Conviction                                     // v: conviction (+Inf for confidence 1)
	ConvictionDiff                                 // e: absolute difference of the conviction to 1
	ConvictionQuotient                             // r: 1 - min(conviction, 1/conviction)
	Imbalance                                      // b: imbalance ratio (rules)
	Kulczynski                                     // k: Kulczynski measure
	KulczynskiDiff                                 // w: 2·|Kulczynski - 1/2|
	ConditionalProbabilityRatio                    // u: conditional probability ratio
	Importance                                     // j: binary logarithm of the conditional probability ratio
	Certainty                                      // z: certainty factor (relative confidence change)
	Chi2                                           // n: normalized chi² measure
	Chi2PValue                                     // p: p-value of the chi² measure (smaller is better)
	Yates                                          // y: normalized chi² measure with Yates' correction
	YatesPValue                                    // t: p-value of Yates' corrected chi² (smaller is better)
	Information                                    // i: information difference to the prior (bits)
	InformationPValue                              // g: p-value of the G statistic (smaller is better)
	FisherProb                                     // f: Fisher's exact test, table probability (smaller is better)
	FisherChi2                                     // h: Fisher's exact test, chi² measure (smaller is better)
	FisherInfo                                     // m: Fisher's exact test, information gain (smaller is better)
	FisherSupport                                  // s: Fisher's exact test, support (smaller is better)
	// LogSupportRatio is the binary logarithm of the quotient of an item
	// set's support and the support expected if its items were
	// independent (0 for sets with fewer than two items). It is upstream's
	// item set measure -eb and is not available for rules (use Imbalance,
	// the rule measure of the letter b).
	LogSupportRatio
)

// The public values equal the internal codes, so no table is needed.
var (
	_ [int(LogSupportRatio) - fpg.EvalLogRatio]struct{}
	_ [fpg.EvalLogRatio - int(LogSupportRatio)]struct{}
	_ [int(FisherSupport) - int(ruleval.FisherSupport)]struct{}
	_ [int(ruleval.FisherSupport) - int(FisherSupport)]struct{}
)

var measureNames = [...]string{
	RuleSupport:                 "RuleSupport",
	Confidence:                  "Confidence",
	ConfidenceDiff:              "ConfidenceDiff",
	Lift:                        "Lift",
	LiftDiff:                    "LiftDiff",
	LiftQuotient:                "LiftQuotient",
	Conviction:                  "Conviction",
	ConvictionDiff:              "ConvictionDiff",
	ConvictionQuotient:          "ConvictionQuotient",
	Imbalance:                   "Imbalance",
	Kulczynski:                  "Kulczynski",
	KulczynskiDiff:              "KulczynskiDiff",
	ConditionalProbabilityRatio: "ConditionalProbabilityRatio",
	Importance:                  "Importance",
	Certainty:                   "Certainty",
	Chi2:                        "Chi2",
	Chi2PValue:                  "Chi2PValue",
	Yates:                       "Yates",
	YatesPValue:                 "YatesPValue",
	Information:                 "Information",
	InformationPValue:           "InformationPValue",
	FisherProb:                  "FisherProb",
	FisherChi2:                  "FisherChi2",
	FisherInfo:                  "FisherInfo",
	FisherSupport:               "FisherSupport",
	LogSupportRatio:             "LogSupportRatio",
}

// isValid reports whether m names a measure.
func (m Measure) isValid() bool { return m >= RuleSupport && m <= LogSupportRatio }

// isRuleMeasure reports whether m is a rule measure.
func (m Measure) isRuleMeasure() bool { return m >= RuleSupport && m < LogSupportRatio }

// String returns the name of the constant, or "Measure(n)" for an invalid
// value.
func (m Measure) String() string {
	if m.isValid() {
		return measureNames[m]
	}
	return fmt.Sprintf("Measure(%d)", int(m))
}

// HigherIsBetter reports whether larger values of m are better, i.e.
// whether Evaluation.Threshold is a minimum (true) or a maximum (false).
// It is false for invalid values.
func (m Measure) HigherIsBetter() bool {
	switch {
	case m == LogSupportRatio:
		return true
	case m.isRuleMeasure():
		return ruleval.Measure(m).Dir() > 0
	}
	return false
}

// Aggregation selects how a rule measure is aggregated over the rules
// S \ {h} -> h of an item set S with at least two items (upstream -a).
// Sets with fewer items get the measure's worst value (0 or 1).
type Aggregation int

// Aggregation modes.
const (
	// AggregateFirst uses the rule whose head is the first item in the
	// miner's internal item order (upstream default, -ax).
	AggregateFirst Aggregation = iota
	// AggregateMin takes the minimum over the rules (-am).
	AggregateMin
	// AggregateMax takes the maximum over the rules (-an).
	AggregateMax
	// AggregateAverage takes the average over the rules (-aa).
	AggregateAverage
)

// String returns the name of the constant.
func (a Aggregation) String() string {
	switch a {
	case AggregateFirst:
		return "AggregateFirst"
	case AggregateMin:
		return "AggregateMin"
	case AggregateMax:
		return "AggregateMax"
	case AggregateAverage:
		return "AggregateAverage"
	}
	return fmt.Sprintf("Aggregation(%d)", int(a))
}

// Evaluation filters item sets or rules by an evaluation measure
// (upstream -e, -a, -d, -i, -p). The value of every reported item set or
// rule is in its Value field.
//
// A rule passes unless its value is worse than Threshold. An item set
// passes if its aggregated value is at least as good as Threshold; with
// a rule measure, closed, maximal and generator targets report the sets
// of that target that pass (upstream semantics). NaN values of rules pass.
type Evaluation struct {
	// Measure is the evaluation measure. Required.
	Measure Measure
	// Aggregate is the aggregation mode for item sets evaluated with a
	// rule measure. It must be AggregateFirst for rules and for
	// LogSupportRatio.
	Aggregate Aggregation
	// Threshold is the threshold in units of the measure: a fraction for
	// confidence-like measures (0.1, not upstream's percentage 10), a
	// p-value for the p-value measures, and so on. The zero value is a
	// threshold of 0. NaN is invalid.
	Threshold float64
	// InvalidateBelowExpected (upstream -i) gives a rule the measure's
	// worst value unless its support is above the support expected if
	// body and head were independent. Rule measures only.
	InvalidateBelowExpected bool
	// PruneSize (upstream -p) enables pruning of item sets with the
	// evaluation from this item set size on: values <= 0 prune backward
	// (only the evaluation filter), positive values also prune forward
	// (sets that fail are not extended). nil disables pruning. Item sets
	// with a rule measure only.
	PruneSize *int
}

// validate checks e for item sets (forRules false) or rules.
func (e *Evaluation) validate(forRules bool) error {
	switch {
	case !e.Measure.isValid():
		return invalid("Evaluation.Measure", "%v is not a measure", e.Measure)
	case forRules && e.Measure == LogSupportRatio:
		return invalid("Evaluation.Measure", "LogSupportRatio is an item set measure; use Imbalance for rules")
	case e.Threshold != e.Threshold:
		return invalid("Evaluation.Threshold", "NaN")
	case e.Aggregate < AggregateFirst || e.Aggregate > AggregateAverage:
		return invalid("Evaluation.Aggregate", "%v is not an aggregation mode", e.Aggregate)
	case e.Aggregate != AggregateFirst && (forRules || e.Measure == LogSupportRatio):
		return invalid("Evaluation.Aggregate", "aggregation applies to item sets with a rule measure only")
	case e.InvalidateBelowExpected && e.Measure == LogSupportRatio:
		return invalid("Evaluation.InvalidateBelowExpected", "applies to rule measures only")
	case e.PruneSize != nil && (forRules || e.Measure == LogSupportRatio):
		return invalid("Evaluation.PruneSize", "applies to item sets with a rule measure only")
	case e.PruneSize != nil && (*e.PruneSize <= fpg.NoPrune || *e.PruneSize > fpg.MaxSize):
		return invalid("Evaluation.PruneSize", "%d out of range", *e.PruneSize)
	}
	return nil
}

// apply sets the evaluation parameters of cfg.
func (e *Evaluation) apply(cfg *fpg.Config) {
	cfg.Eval = int(e.Measure)
	cfg.Agg = int(e.Aggregate)
	cfg.InvalidateBelowExpected = e.InvalidateBelowExpected
	dir := -1
	if e.Measure.HigherIsBetter() {
		dir = +1
	}
	cfg.Thresh = percentOf(e.Threshold, dir)
	cfg.Prune = fpg.NoPrune
	if e.PruneSize != nil {
		cfg.Prune = *e.PruneSize
	}
}
