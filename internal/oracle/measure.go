package oracle

import (
	"errors"
	"fmt"
	"math/bits"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// MeasureFunc evaluates the rule body -> head. It has the signature of
// upstream's ruleval functions (RULEVALFN): supp is the support of
// body ∪ {head}, body the body support (the total weight for an empty
// body), head the support of the head item and base the total weight.
type MeasureFunc func(supp, body, head, base tract.Supp) float64

// Direction tells whether larger or smaller measure values are better
// (upstream's (+) and (-) measures, re_dir).
type Direction int8

// Directions of a Measure.
const (
	// HigherIsBetter: a value must reach or exceed the threshold.
	HigherIsBetter Direction = +1
	// LowerIsBetter: a value must not exceed the threshold.
	LowerIsBetter Direction = -1
)

// worst returns the value upstream assigns where no rule can be evaluated
// (empty sets and singletons) or a rule is invalidated by -i:
// 1 for LowerIsBetter, 0 for HigherIsBetter.
func (d Direction) worst() float64 {
	if d < 0 {
		return 1
	}
	return 0
}

// Measure is an additional rule evaluation measure (upstream -e, -d, -i).
type Measure struct {
	// Func computes the value of a rule. Required.
	Func MeasureFunc
	// Dir is the measure's direction. Required.
	Dir Direction
	// Threshold is the evaluation threshold as a value of the measure,
	// i.e. upstream's -d argument divided by 100.
	Threshold float64
	// InvalidateBelowExpected (upstream -i) replaces the value of a rule by
	// the worst value (1 for LowerIsBetter, 0 for HigherIsBetter) unless
	// float64(supp)*float64(base) > float64(head)*float64(body); the
	// replaced values still enter set aggregates.
	InvalidateBelowExpected bool
}

// validate checks that m is usable.
func (m *Measure) validate() error {
	if m.Func == nil {
		return errors.New("oracle: measure without function")
	}
	if m.Dir != HigherIsBetter && m.Dir != LowerIsBetter {
		return fmt.Errorf("oracle: invalid measure direction %d", m.Dir)
	}
	return nil
}

// ruleValue evaluates one rule like istree.c (r4set, evaluate), including
// the -i invalidation.
func (m *Measure) ruleValue(supp, body, head, base tract.Supp) float64 {
	if m.InvalidateBelowExpected &&
		!(float64(supp)*float64(base) > float64(head)*float64(body)) {
		return m.Dir.worst()
	}
	return m.Func(supp, body, head, base)
}

// rejectsRule is upstream's rule test (r4set): reject if
// dir*value < dir*threshold. A NaN value is therefore accepted
// (rules-eval.md B5).
func (m *Measure) rejectsRule(v float64) bool {
	dir := float64(m.Dir)
	return dir*v < dir*m.Threshold
}

// acceptsSet is upstream's item set test (isets): accept if
// value*dir >= dir*threshold. A NaN value is therefore rejected.
func (m *Measure) acceptsSet(v float64) bool {
	dir := float64(m.Dir)
	return v*dir >= dir*m.Threshold
}

// Aggregation selects how SetEval combines the values of the rules formed
// from an item set (upstream -a).
type Aggregation uint8

// Aggregation modes. The zero value is AggFirst, upstream's default (-ax),
// deliberately not an "unknown" sentinel.
const (
	// AggFirst (-ax, "none") uses only the first rule: the one whose head
	// is the item with the smallest code (SetEval.Order).
	AggFirst Aggregation = iota
	// AggMin (-am) takes the minimum: v replaces the aggregate if v < agg.
	AggMin
	// AggMax (-an) takes the maximum: v replaces the aggregate if v > agg.
	AggMax
	// AggAvg (-aa) sums the values in head-code order and divides by |S|.
	AggAvg
)

// SetEval evaluates item sets by a rule measure, like upstream's item set
// tree path (-ts/-tc/-tm/-tg with a rule measure; istree.c evaluate):
//
//   - the empty set and singletons get the constant 1 for LowerIsBetter
//     and 0 for HigherIsBetter;
//   - a set S with |S| >= 2 forms the |S| rules S\{h} -> h. The first
//     value is the rule whose head has the smallest code; AggFirst returns
//     it. Otherwise the remaining rules follow in increasing head code and
//     are combined with the C comparisons (so a NaN first value sticks
//     through min/max, later NaNs are ignored, and AggAvg propagates NaN).
//
// A set is accepted if value*dir >= dir*threshold.
type SetEval struct {
	Measure Measure
	Agg     Aggregation
	// Order lists items by increasing item code (code 0 first), i.e. the
	// miner's recoded item order. It decides which rule is first and the
	// summation order of AggAvg. Every item of an evaluated set with at
	// least two items must be listed. Nil means canonical byte order.
	Order []string
}

// validate checks that e is usable.
func (e *SetEval) validate() error {
	if err := e.Measure.validate(); err != nil {
		return err
	}
	if e.Agg > AggAvg {
		return fmt.Errorf("oracle: invalid aggregation mode %d", e.Agg)
	}
	return nil
}

// setEvaluator evaluates item sets of one DB.
type setEvaluator struct {
	db   *DB
	eval *SetEval
	rank []int // rank[bit]: position in the code order, -1 if unknown
}

// newSetEvaluator prepares the item ranks of e for db.
func newSetEvaluator(db *DB, e *SetEval) *setEvaluator {
	rank := make([]int, len(db.items))
	for i := range rank {
		rank[i] = i
	}
	if e.Order != nil {
		for i := range rank {
			rank[i] = -1
		}
		for pos, item := range e.Order {
			if i, ok := db.index[item]; ok && rank[i] < 0 {
				rank[i] = pos
			}
		}
	}
	return &setEvaluator{db: db, eval: e, rank: rank}
}

// value returns the aggregated measure value of the set with mask m.
func (se *setEvaluator) value(m uint64) (float64, error) {
	meas := &se.eval.Measure
	n := bits.OnesCount64(m)
	if n <= 1 {
		return meas.Dir.worst(), nil
	}
	heads := make([]int, 0, n)
	for r := m; r != 0; r &= r - 1 {
		i := bits.TrailingZeros64(r)
		if se.rank[i] < 0 {
			return 0, fmt.Errorf("oracle: item %q missing from SetEval.Order", se.db.items[i])
		}
		heads = append(heads, i)
	}
	sortByRank(heads, se.rank)

	db := se.db
	supp := db.supp[m]
	ruleValue := func(h int) float64 {
		bit := uint64(1) << h
		return meas.ruleValue(supp, db.supp[m&^bit], db.supp[bit], db.total)
	}
	agg := ruleValue(heads[0])
	if se.eval.Agg == AggFirst {
		return agg, nil
	}
	for _, h := range heads[1:] {
		v := ruleValue(h)
		switch se.eval.Agg {
		case AggMin:
			if v < agg {
				agg = v
			}
		case AggMax:
			if v > agg {
				agg = v
			}
		default:
			agg += v
		}
	}
	if se.eval.Agg == AggAvg {
		agg /= float64(n)
	}
	return agg, nil
}

// sortByRank sorts the bit indices by increasing rank (insertion sort; the
// slices hold at most MaxItems entries).
func sortByRank(idx []int, rank []int) {
	for i := 1; i < len(idx); i++ {
		for j := i; j > 0 && rank[idx[j]] < rank[idx[j-1]]; j-- {
			idx[j], idx[j-1] = idx[j-1], idx[j]
		}
	}
}
