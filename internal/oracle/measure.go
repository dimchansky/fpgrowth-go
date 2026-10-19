package oracle

import (
	"errors"
	"fmt"
	"math"
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
//
// A measure is given either by a function (Func), whose values are used
// as they are, or by a reference measure (Ref), whose values carry a
// tolerance. With Ref, rules and item sets whose value lies within its
// tolerance of the threshold are marked Optional (an implementation may
// report them or not), and the -i test is exact.
type Measure struct {
	// Func computes the value of a rule. Exactly one of Func and Ref is
	// required.
	Func MeasureFunc
	// Ref is an independent reference measure (RefMeasures) used instead
	// of Func.
	Ref *RefMeasure
	// Dir is the measure's direction. Required; with Ref it must equal
	// Ref.Dir.
	Dir Direction
	// Threshold is the evaluation threshold as a value of the measure,
	// i.e. upstream's -d argument divided by 100.
	Threshold float64
	// InvalidateBelowExpected (upstream -i) replaces the value of a rule by
	// the worst value (1 for LowerIsBetter, 0 for HigherIsBetter) unless
	// the rule's support is above the support expected under independence:
	// with Func, upstream's float64 test
	// float64(supp)*float64(base) > float64(head)*float64(body); with Ref,
	// the exact integer test supp·base > head·body. The replaced values
	// still enter set aggregates.
	InvalidateBelowExpected bool
}

// validate checks that m is usable.
func (m *Measure) validate() error {
	switch {
	case m.Func == nil && m.Ref == nil:
		return errors.New("oracle: measure without function")
	case m.Func != nil && m.Ref != nil:
		return errors.New("oracle: measure with both a function and a reference")
	case m.Dir != HigherIsBetter && m.Dir != LowerIsBetter:
		return fmt.Errorf("oracle: invalid measure direction %d", m.Dir)
	case m.Ref != nil && m.Ref.Dir != m.Dir:
		return fmt.Errorf("oracle: direction %d differs from the reference measure %s", m.Dir, m.Ref.Name)
	}
	return nil
}

// aboveExpected reports whether supp·base > head·body, exactly (the
// supports are non-negative, so the 128-bit products cannot overflow).
func aboveExpected(supp, body, head, base tract.Supp) bool {
	hi1, lo1 := bits.Mul64(uint64(supp), uint64(base))
	hi2, lo2 := bits.Mul64(uint64(head), uint64(body))
	return hi1 > hi2 || hi1 == hi2 && lo1 > lo2
}

// worstValue is the convention for sets that cannot be evaluated and for
// rules invalidated by -i (ConvWorst).
func (m *Measure) worstValue() Value { return conv(m.Dir.worst(), ConvWorst) }

// ruleValue evaluates one rule like istree.c (r4set, evaluate), including
// the -i invalidation. With Func the value has no tolerance; with Ref it
// is the reference Value.
func (m *Measure) ruleValue(supp, body, head, base tract.Supp) (Value, error) {
	if m.Ref == nil {
		if m.InvalidateBelowExpected &&
			!(float64(supp)*float64(base) > float64(head)*float64(body)) {
			return m.worstValue(), nil
		}
		return Value{V: m.Func(supp, body, head, base)}, nil
	}
	if m.InvalidateBelowExpected && !aboveExpected(supp, body, head, base) {
		return m.worstValue(), nil
	}
	rv, err := m.Ref.Eval(supp, body, head, base)
	if err != nil {
		return Value{}, fmt.Errorf("oracle: %s(%d, %d, %d, %d): %w", m.Ref.Name, supp, body, head, base, err)
	}
	return rv, nil
}

// margin returns the half-width of the band around a value in which an
// implementation's threshold decision may go either way: its tolerance,
// or the bound of its known defect if larger.
func (v Value) margin() float64 { return max(v.Tol, v.DefectTol) }

// decision is the outcome of a threshold test.
type decision uint8

const (
	rejected decision = iota
	accepted
	boundary // within the tolerance of the threshold: either is correct
)

// decideTol classifies d = dir·(v - threshold) with the tolerance tol > 0
// of a finite v.
func decideTol(v, tol float64, m *Measure) decision {
	d := float64(m.Dir) * (v - m.Threshold)
	switch {
	case d > tol:
		return accepted
	case d < -tol:
		return rejected
	}
	return boundary
}

// ruleDecision is upstream's rule test (r4set): reject if
// dir*value < dir*threshold. A NaN value is therefore accepted
// (rules-eval.md B5). With a tolerance, values within it of the
// threshold are boundary cases.
func (m *Measure) ruleDecision(val Value) decision {
	v, tol := val.V, val.margin()
	if tol > 0 && !math.IsInf(v, 0) {
		return decideTol(v, tol, m)
	}
	dir := float64(m.Dir)
	if dir*v < dir*m.Threshold {
		return rejected
	}
	return accepted
}

// setDecision is upstream's item set test (isets): accept if
// value*dir >= dir*threshold. A NaN value is therefore rejected. With a
// tolerance, values within it of the threshold are boundary cases.
func (m *Measure) setDecision(val Value) decision {
	v, tol := val.V, val.margin()
	if tol > 0 && !math.IsInf(v, 0) {
		return decideTol(v, tol, m)
	}
	dir := float64(m.Dir)
	if v*dir >= dir*m.Threshold {
		return accepted
	}
	return rejected
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
//
// With a reference measure (Measure.Ref) the aggregate carries a
// tolerance: that of the first rule for AggFirst, the largest tolerance of
// the set's rules for AggMin and AggMax (the minimum of values that are
// each within t_i of the reference is within max t_i of the reference
// minimum), and the average tolerance plus ratTau·|value| for AggAvg;
// known-defect bounds (Value.DefectTol) aggregate the same way.
//
// Which rule is first (AggFirst) depends on the item order of the
// implementation, not on a definition (docs/differences.md P6); callers
// pass the implementation's order in Order.
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

// value returns the aggregated measure value of the set with mask m with
// its tolerance and known defect (none with Measure.Func).
func (se *setEvaluator) value(m uint64) (Value, error) {
	meas := &se.eval.Measure
	n := bits.OnesCount64(m)
	if n <= 1 {
		return meas.worstValue(), nil
	}
	heads := make([]int, 0, n)
	for r := m; r != 0; r &= r - 1 {
		i := bits.TrailingZeros64(r)
		if se.rank[i] < 0 {
			return Value{}, fmt.Errorf("oracle: item %q missing from SetEval.Order", se.db.items[i])
		}
		heads = append(heads, i)
	}
	sortByRank(heads, se.rank)

	db := se.db
	supp := db.supp[m]
	ruleValue := func(h int) (Value, error) {
		bit := uint64(1) << h
		return meas.ruleValue(supp, db.supp[m&^bit], db.supp[bit], db.total)
	}
	agg, err := ruleValue(heads[0])
	if err != nil || se.eval.Agg == AggFirst {
		return agg, err
	}
	agg.Kind, agg.Conv = Exact, "" // an aggregate of possibly different kinds
	for _, h := range heads[1:] {
		v, err := ruleValue(h)
		if err != nil {
			return Value{}, err
		}
		if v.KnownDefect != "" {
			agg.KnownDefect = v.KnownDefect
		}
		switch se.eval.Agg {
		case AggMin:
			if v.V < agg.V {
				agg.V = v.V
			}
			agg.Tol, agg.DefectTol = max(agg.Tol, v.Tol), max(agg.DefectTol, v.DefectTol)
		case AggMax:
			if v.V > agg.V {
				agg.V = v.V
			}
			agg.Tol, agg.DefectTol = max(agg.Tol, v.Tol), max(agg.DefectTol, v.DefectTol)
		default:
			agg.V += v.V
			agg.Tol += v.Tol
			agg.DefectTol += max(v.Tol, v.DefectTol)
		}
	}
	if se.eval.Agg == AggAvg {
		fn := float64(n)
		agg.V /= fn
		if meas.Ref != nil {
			agg.Tol = agg.Tol/fn + ratTau*math.Abs(agg.V)
			agg.DefectTol = agg.DefectTol/fn + ratTau*math.Abs(agg.V)
		}
	}
	if agg.KnownDefect == "" {
		agg.DefectTol = 0
	}
	if math.IsInf(agg.V, 0) {
		agg.Tol, agg.DefectTol, agg.KnownDefect = 0, 0, ""
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
