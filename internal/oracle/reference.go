package oracle

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Reference evaluation measures.
//
// This file is an independent reference for the 25 rule evaluation
// measures of upstream's ruleval.c (and, in logratio.go, for the item set
// measure -eb). It is written from the mathematical definitions of the
// measures. It does not import or call the implementation
// (internal/ruleval, internal/istree, internal/report, internal/fpg; a test
// checks the import graph), and it uses none of its formulas: products and
// quotients are exact big.Rat/big.Int arithmetic, logarithms are big.Float
// series, and the p-values use math.Erfc instead of an incomplete gamma
// function. The origin is the auditor's checker
// reference/_audit/alg/bf/measures.go (audit ALG, "Tools"), reviewed and
// rewritten: that checker computed the information measure and the
// chi²-type p-values in float64 and limited Fisher to n <= 600.
//
// Upstream defines constant values where a definition is undefined (0/0 or
// x/0) and uses a few definitions that differ from the textbook (audit
// ALG-5) or were changed by the port (docs/differences.md R4, R5, R9b).
// Those values are conventions, not mathematics. The reference returns
// them with Kind Convention and the identifier of the convention; every
// other value has Kind Exact. See the Conv* constants.

// A 2x2 contingency table is given by upstream's four supports: s =
// supp(B ∪ {h}), b = supp(B), h = supp({h}) and n, the total weight. Its
// cells are
//
//	        h       not h
//	B       s       b-s
//	not B   h-s     n-b-h+s
//
// and it is valid if n >= 1, 0 <= s <= min(b, h), max(b, h) <= n and
// b + h - s <= n.

// ValueKind tells whether a reference value is the mathematical value of
// the measure's definition or a convention.
type ValueKind uint8

// Value kinds.
const (
	// Exact: the value follows from the definition. V is the float64
	// nearest to it (rational measures), within a few ulps of it
	// (logarithms), or within a relative (1 + x)·2^-51 of it for a
	// chi²(1) tail probability of a statistic x (p-values; see q1).
	Exact ValueKind = iota
	// Convention: the definition does not apply or is not textbook; V is
	// the value the convention Conv prescribes. A constant convention
	// (0, 1, +Inf) has Tol 0 and must be returned exactly; KulczynskiDiff,
	// whose convention ALG-5 is a different formula (2·|K - 1/2|), carries
	// the usual tolerance.
	Convention
)

// String returns "exact" or "convention".
func (k ValueKind) String() string {
	if k == Convention {
		return "convention"
	}
	return "exact"
}

// Conventions. The identifiers are those of docs/differences.md and of the
// audit reports (docs/audit/algorithms.md); ConvUndefined refers to the
// degenerate cases of docs/upstream/rules-eval.md §6.2.
const (
	// ConvUndefined: the definition is undefined for this table (a zero
	// denominator: an empty body or head, a body or head that occurs in
	// every transaction, ...). The value is upstream's constant for the
	// case (rules-eval.md §6.2, "Degenerate cases"): 0 for most measures
	// and their p-values 1, +Inf for Kulczynski, KulczynskiDiff and
	// Imbalance, +Inf for CondProbRatio and Importance when the head never
	// occurs without the body, and +Inf for ConvictionDiff and
	// ConvictionQuotient with an empty body (upstream tests b <= s only).
	ConvUndefined = "U"
	// ConvALG5: RuleSupport is the absolute support s, compared with
	// -d/100 (not s/n); KulczynskiDiff is 2·|K - 1/2| (not |K - 1/2|);
	// ConvictionQuotient is +Inf for confidence 1 (its limit is 1). Audit
	// ALG-5, rules-eval.md §6.2 (note N3).
	ConvALG5 = "ALG-5"
	// ConvR4: Conviction and ConvictionDiff are +Inf for confidence 1
	// (differences.md R4; upstream: Conviction 0).
	ConvR4 = "R4"
	// ConvR5: Certainty is 0 when the head occurs in every transaction
	// (differences.md R5; upstream: NaN).
	ConvR5 = "R5"
	// ConvR9b: Yates' continuity correction is clamped at 0, i.e. the
	// statistic is n·max(0, |ad-bc| - n/2)²/(margins) (differences.md
	// R9b, as R's chisq.test; upstream squares a negative difference).
	ConvR9b = "R9b"
	// ConvWorst: the constant an evaluation assigns where no rule can be
	// evaluated (the empty set and singletons) or a rule is invalidated by
	// -i: 0 for HigherIsBetter, 1 for LowerIsBetter (istree.c evaluate,
	// rules-eval.md §4.1). The oracle applies it in Measure and SetEval;
	// these values carry no tolerance.
	ConvWorst = "worst"
)

// The other port fixes of docs/differences.md that concern measure values
// are not conventions: they make the implementation agree with the exact
// definition, which the reference computes directly, so their values
// have Kind Exact. R6: the Fisher tests count tied tables (equal
// probability or information) as in the region; the reference decides
// ties exactly. R7: the information is never negative; the reference's
// mutual information is >= 0 by construction. R9a: the Fisher p-values
// lie in [0, 1]; the reference's exact sums do.

// Value is the reference value of a measure for one table.
type Value struct {
	// V is the value (see Kind).
	V float64
	// Tol is the absolute tolerance for comparing an implementation's
	// float64 value with V (Matches). It is 0 for conventions, infinities
	// and integral values, which must match exactly. Otherwise it bounds
	// the rounding error that a float64 implementation of the measure
	// accumulates on top of the error of V itself; see ratTau.
	Tol float64
	// Kind tells whether V is exact or a convention.
	Kind ValueKind
	// Conv is the identifier of the convention (one of the Conv*
	// constants) if Kind is Convention, else "".
	Conv string
}

// Matches reports whether an implementation's value x matches v: x == V,
// or |x - V| <= Tol for a finite V. NaN never matches.
func (v Value) Matches(x float64) bool {
	if x == v.V {
		return true
	}
	if v.Tol == 0 || math.IsInf(v.V, 0) || math.IsNaN(x) {
		return false
	}
	return math.Abs(x-v.V) <= v.Tol
}

// String formats v for test messages.
func (v Value) String() string {
	s := fmt.Sprintf("%.17g ±%.3g (%v", v.V, v.Tol, v.Kind)
	if v.Conv != "" {
		s += " " + v.Conv
	}
	return s + ")"
}

// RefMeasure is the reference implementation of one rule evaluation
// measure.
type RefMeasure struct {
	// Letter is the letter of upstream's -e option.
	Letter byte
	// Name is the name of the measure in the public API (Measure.String).
	Name string
	// Dir is the measure's direction.
	Dir  Direction
	eval func(t *table) (Value, error)
}

// ErrInvalidTable is returned for supports that do not form a valid 2x2
// table (see the table definition above).
var ErrInvalidTable = errors.New("oracle: invalid contingency table")

// ErrOutOfScope is returned when the reference cannot compute a value with
// a guaranteed error bound: a Fisher test with more than
// FisherMaxTables tables, or a FisherInfo tie that cannot be decided.
var ErrOutOfScope = errors.New("oracle: reference value out of scope")

// FisherMaxTables is the largest number of contingency tables, i.e.
// min(b, h, n-b, n-h) + 1, for which the reference computes the Fisher
// tests. Their cost grows quadratically with it (exact big integer
// hypergeometric weights).
const FisherMaxTables = 1024

// Eval returns the reference value of m for the rule with support supp of
// body ∪ {head}, body support body, head support head and total weight
// base. It returns ErrInvalidTable for an invalid table and ErrOutOfScope
// where the reference has no value (see there).
func (m *RefMeasure) Eval(supp, body, head, base tract.Supp) (Value, error) {
	t, err := newTable(supp, body, head, base)
	if err != nil {
		return Value{}, err
	}
	return m.eval(t)
}

// refMeasures lists the reference measures in upstream's id order
// (RE_SUPP ... RE_FETSUPP), which is also the order of the public
// constants RuleSupport ... FisherSupport.
var refMeasures = []*RefMeasure{
	{'o', "RuleSupport", HigherIsBetter, refSupport},
	{'c', "Confidence", HigherIsBetter, refConfidence},
	{'d', "ConfidenceDiff", HigherIsBetter, refConfidenceDiff},
	{'l', "Lift", HigherIsBetter, refLift},
	{'a', "LiftDiff", HigherIsBetter, refLiftDiff},
	{'q', "LiftQuotient", HigherIsBetter, refLiftQuotient},
	{'v', "Conviction", HigherIsBetter, refConviction},
	{'e', "ConvictionDiff", HigherIsBetter, refConvictionDiff},
	{'r', "ConvictionQuotient", HigherIsBetter, refConvictionQuotient},
	{'b', "Imbalance", HigherIsBetter, refImbalance},
	{'k', "Kulczynski", HigherIsBetter, refKulczynski},
	{'w', "KulczynskiDiff", HigherIsBetter, refKulczynskiDiff},
	{'u', "ConditionalProbabilityRatio", HigherIsBetter, refCondProbRatio},
	{'j', "Importance", HigherIsBetter, refImportance},
	{'z', "Certainty", HigherIsBetter, refCertainty},
	{'n', "Chi2", HigherIsBetter, refChi2},
	{'p', "Chi2PValue", LowerIsBetter, refChi2PValue},
	{'y', "Yates", HigherIsBetter, refYates},
	{'t', "YatesPValue", LowerIsBetter, refYatesPValue},
	{'i', "Information", HigherIsBetter, refInfo},
	{'g', "InformationPValue", LowerIsBetter, refInfoPValue},
	{'f', "FisherProb", LowerIsBetter, fisherRef('f')},
	{'h', "FisherChi2", LowerIsBetter, fisherRef('h')},
	{'m', "FisherInfo", LowerIsBetter, fisherRef('m')},
	{'s', "FisherSupport", LowerIsBetter, fisherRef('s')},
}

// RefMeasures returns the 25 reference rule measures in upstream's id
// order (RuleSupport ... FisherSupport). The caller must not modify them.
func RefMeasures() []*RefMeasure { return slices.Clone(refMeasures) }

// RefByLetter returns the reference measure of the -e letter c ('b' is
// the rule measure Imbalance; 'x', "no measure", has no reference).
func RefByLetter(c byte) (*RefMeasure, bool) {
	for _, m := range refMeasures {
		if m.Letter == c {
			return m, true
		}
	}
	return nil, false
}

// RefByName returns the reference measure with the public API name name
// (for example "Conviction").
func RefByName(name string) (*RefMeasure, bool) {
	for _, m := range refMeasures {
		if m.Name == name {
			return m, true
		}
	}
	return nil, false
}

// table is a valid 2x2 contingency table with big integer copies of its
// supports.
type table struct {
	s, b, h, n     int64
	bs, bb, bh, bn *big.Int
}

func newTable(supp, body, head, base tract.Supp) (*table, error) {
	s, b, h, n := int64(supp), int64(body), int64(head), int64(base)
	// b + h - s <= n, written without overflow: s >= 0 and b, h <= n.
	if n < 1 || s < 0 || s > b || s > h || b > n || h > n || b-s > n-h {
		return nil, fmt.Errorf("%w: supp %d, body %d, head %d, base %d", ErrInvalidTable, s, b, h, n)
	}
	return &table{s: s, b: b, h: h, n: n,
		bs: big.NewInt(s), bb: big.NewInt(b), bh: big.NewInt(h), bn: big.NewInt(n)}, nil
}

// degenerate reports whether a margin of t is 0 or n.
func (t *table) degenerate() bool { return t.b == 0 || t.b == t.n || t.h == 0 || t.h == t.n }

// cell is one cell count with its row (body) and column (head) margins.
type cell struct{ c, r, k *big.Int }

// cells returns the four cells of t with their margins.
func (t *table) cells() [4]cell {
	nb := new(big.Int).Sub(t.bn, t.bb)
	nh := new(big.Int).Sub(t.bn, t.bh)
	return [4]cell{
		{t.bs, t.bb, t.bh},                          // B, h
		{new(big.Int).Sub(t.bb, t.bs), t.bb, nh},    // B, not h
		{new(big.Int).Sub(t.bh, t.bs), nb, t.bh},    // not B, h
		{big.NewInt(t.n - t.b - t.h + t.s), nb, nh}, // not B, not h (no overflow: valid table)
	}
}

// frac returns x/y as a big.Rat (y != 0).
func frac(x, y *big.Int) *big.Rat { return new(big.Rat).SetFrac(x, y) }

// Tolerances.
//
// The tolerances are the error bounds that the implementation documents
// (issue #16; docs/differences.md "Numeric contract"), so a comparison
// with the reference checks the documented accuracy directly. They hold
// for every valid table up to a total weight of 2^63 - 1. The reference
// value itself is exact up to its final rounding to float64.
//
// ratTau, 2^-44 ≈ 5.7e-14 (256 ulps of float64), is the tolerance unit of
// the rational measures and the logarithms of rational values. An
// implementation computes them with a short float64 formula of the four
// supports (at most a few dozen roundings of relative size 2^-53). Two
// forms are used:
//
//   - relative, ratTau·|v|, for the products and quotients c, l, v, b, k
//     and u;
//   - absolute below 1, ratTau·max(1, |v|), for the differences d, a, q,
//     e, r, w and for j, where a float64 evaluation subtracts quantities
//     of size about 1 (|conf - prior|, |lift - 1|, log2 of a ratio near
//     1) and keeps an absolute error of a few ulps.
//
// The statistical measures have their own, tighter bounds: chi² and
// Yates (n, y) chiTau relative, the information (i) infoTau relative, the
// certainty factor (z) certTau absolute, the p-values (p, t, g) pTau
// scaled by the conditioning of the chi² tail (pValue) and the Fisher
// tests fisherTau relative (fisherTol). A wrong formula changes values
// by far more: the TST-3 mutant (Kulczynski factor 0.5000001) changes
// them by 2e-7.
const ratTau = 0x1p-44

// chiTau, 2^-49 (8 ulps), bounds the relative error of chi² and Yates:
// the deviation s·n - b·h is exact in 128-bit integers and rounded once,
// and the quotient adds about ten roundings.
const chiTau = 0x1p-49

// infoTau, 2^-47 (32 ulps), bounds the relative error of the information
// (a sum of non-negative terms; differences.md, numeric contract).
const infoTau = 0x1p-47

// certTau, 2^-40, bounds the absolute error of the certainty factor (a
// value in [-1, 1]): upstream's formula amplifies its rounding errors by
// n/(n-h), the implementation uses an exact form beyond n/(n-h) = 2^10.
const certTau = 0x1p-40

// pTau, 2^-36, is the tolerance unit of the p-values, whose evaluation
// involves an incomplete gamma function (see pValue).
const pTau = 0x1p-36

// fisherTau, 2^-36, bounds the relative error of the Fisher tests (see
// fisherTol).
const fisherTau = 0x1p-36

// relTau returns the exact value r with the relative tolerance tau·|v|.
func relTau(r *big.Rat, tau float64) Value {
	v := ratFloat64(r)
	return Value{V: v, Tol: tau * math.Abs(v)}
}

// exactRat returns the exact value r with the tolerance ratTau·max(1, |v|).
func exactRat(r *big.Rat) Value {
	v := ratFloat64(r)
	return Value{V: v, Tol: ratTau * max(1, math.Abs(v))}
}

// relRat returns the exact value r with the relative tolerance ratTau·|v|.
func relRat(r *big.Rat) Value {
	v := ratFloat64(r)
	return Value{V: v, Tol: ratTau * math.Abs(v)}
}

// conv returns a convention value.
func conv(v float64, id string) Value { return Value{V: v, Kind: Convention, Conv: id} }

// pValue returns the chi²(1) tail probability of the statistic x >= 0
// (as float64) with its tolerance: relative pTau·(1 + x), which covers
// the conditioning of the tail function (see q1) for the implementation
// and for the reference, plus an absolute 2^-1000 for results near the
// underflow range.
func pValue(x float64) Value {
	v := q1(x)
	return Value{V: v, Tol: pTau*(1+x)*v + 0x1p-1000}
}

func refSupport(t *table) (Value, error) {
	// Upstream's RE_SUPP is the absolute support s, not s/n (ALG-5).
	return conv(float64(t.s), ConvALG5), nil
}

// Confidence: P(h | B) = s/b.
func refConfidence(t *table) (Value, error) {
	if t.b == 0 {
		return conv(0, ConvUndefined), nil
	}
	return relRat(frac(t.bs, t.bb)), nil
}

// ConfidenceDiff: |P(h | B) - P(h)| = |s/b - h/n|.
func refConfidenceDiff(t *table) (Value, error) {
	if t.b == 0 {
		return conv(0, ConvUndefined), nil
	}
	r := new(big.Rat).Sub(frac(t.bs, t.bb), frac(t.bh, t.bn))
	return exactRat(r.Abs(r)), nil
}

// lift returns P(h | B)/P(h) = (s/b)/(h/n), or nil if b or h is 0.
func (t *table) lift() *big.Rat {
	if t.b == 0 || t.h == 0 {
		return nil
	}
	return new(big.Rat).Quo(frac(t.bs, t.bb), frac(t.bh, t.bn))
}

func refLift(t *table) (Value, error) {
	l := t.lift()
	if l == nil {
		return conv(0, ConvUndefined), nil
	}
	return relRat(l), nil
}

// LiftDiff: |lift - 1|.
func refLiftDiff(t *table) (Value, error) {
	l := t.lift()
	if l == nil {
		return conv(0, ConvUndefined), nil
	}
	l.Sub(l, big.NewRat(1, 1))
	return exactRat(l.Abs(l)), nil
}

// oneMinusMinInv returns 1 - min(x, 1/x) for x >= 0 (1 for x = 0, where
// 1/x is infinite).
func oneMinusMinInv(x *big.Rat) *big.Rat {
	one := big.NewRat(1, 1)
	if x.Sign() == 0 {
		return one
	}
	if x.Cmp(one) > 0 {
		x = new(big.Rat).Inv(x)
	}
	return new(big.Rat).Sub(one, x)
}

// LiftQuotient: 1 - min(lift, 1/lift).
func refLiftQuotient(t *table) (Value, error) {
	l := t.lift()
	if l == nil {
		return conv(0, ConvUndefined), nil
	}
	return exactRat(oneMinusMinInv(l)), nil
}

// conviction returns P(not h)/P(not h | B) = (1 - h/n)/(1 - s/b), or nil
// if b is 0 or b = s (confidence 1, conviction infinite).
func (t *table) conviction() *big.Rat {
	if t.b == 0 || t.b == t.s {
		return nil
	}
	one := big.NewRat(1, 1)
	num := new(big.Rat).Sub(one, frac(t.bh, t.bn))
	den := new(big.Rat).Sub(one, frac(t.bs, t.bb))
	return num.Quo(num, den)
}

func refConviction(t *table) (Value, error) {
	switch {
	case t.b == 0:
		return conv(0, ConvUndefined), nil
	case t.b == t.s:
		return conv(math.Inf(1), ConvR4), nil
	}
	return relRat(t.conviction()), nil
}

// ConvictionDiff: |conviction - 1|.
func refConvictionDiff(t *table) (Value, error) {
	switch {
	case t.b == 0:
		return conv(math.Inf(1), ConvUndefined), nil
	case t.b == t.s:
		return conv(math.Inf(1), ConvR4), nil
	}
	v := t.conviction()
	v.Sub(v, big.NewRat(1, 1))
	return exactRat(v.Abs(v)), nil
}

// ConvictionQuotient: 1 - min(conviction, 1/conviction).
func refConvictionQuotient(t *table) (Value, error) {
	switch {
	case t.b == 0:
		return conv(math.Inf(1), ConvUndefined), nil
	case t.b == t.s:
		return conv(math.Inf(1), ConvALG5), nil
	}
	return exactRat(oneMinusMinInv(t.conviction())), nil
}

// Imbalance ratio (Wu, Chen and Han): |P(B) - P(h)| / P(B or h) =
// |b - h|/(b + h - s).
func refImbalance(t *table) (Value, error) {
	d := new(big.Int).Add(t.bb, t.bh)
	d.Sub(d, t.bs)
	if d.Sign() == 0 {
		return conv(math.Inf(1), ConvUndefined), nil
	}
	num := new(big.Int).Sub(t.bb, t.bh)
	return relRat(frac(num.Abs(num), d)), nil
}

// kulczynski returns (P(h | B) + P(B | h))/2 = (s/b + s/h)/2, or nil if
// b or h is 0.
func (t *table) kulczynski() *big.Rat {
	if t.b == 0 || t.h == 0 {
		return nil
	}
	k := new(big.Rat).Add(frac(t.bs, t.bb), frac(t.bs, t.bh))
	return k.Quo(k, big.NewRat(2, 1))
}

func refKulczynski(t *table) (Value, error) {
	k := t.kulczynski()
	if k == nil {
		return conv(math.Inf(1), ConvUndefined), nil
	}
	return relRat(k), nil
}

// KulczynskiDiff: upstream's 2·|K - 1/2| (ALG-5), for every table.
func refKulczynskiDiff(t *table) (Value, error) {
	k := t.kulczynski()
	if k == nil {
		return conv(math.Inf(1), ConvUndefined), nil
	}
	k.Sub(k, big.NewRat(1, 2))
	k.Abs(k)
	k.Mul(k, big.NewRat(2, 1))
	v := exactRat(k)
	v.Kind, v.Conv = Convention, ConvALG5
	return v, nil
}

// condProbRatio returns P(h | B)/P(h | not B) = (s/b)/((h-s)/(n-b)) for a
// table with s > 0, 0 < b < n and h > s.
func (t *table) condProbRatio() *big.Rat {
	num := frac(t.bs, t.bb)
	den := frac(new(big.Int).Sub(t.bh, t.bs), new(big.Int).Sub(t.bn, t.bb))
	return num.Quo(num, den)
}

// condProbSpecial handles the tables where the conditional probability
// ratio is 0, undefined or infinite; ok is false otherwise.
func (t *table) condProbSpecial() (v Value, ok bool) {
	switch {
	case t.b == 0 || t.b == t.n:
		// P(h | B) or P(h | not B) undefined
		return conv(0, ConvUndefined), true
	case t.s == 0:
		if t.h == 0 {
			return conv(0, ConvUndefined), true // 0/0
		}
		return Value{}, false // exactly 0 (CondProbRatio), log 0 (Importance)
	case t.h == t.s:
		return conv(math.Inf(1), ConvUndefined), true // x/0, x > 0
	}
	return Value{}, false
}

func refCondProbRatio(t *table) (Value, error) {
	if v, ok := t.condProbSpecial(); ok {
		return v, nil
	}
	if t.s == 0 {
		return Value{}, nil // exactly 0
	}
	return relRat(t.condProbRatio()), nil
}

// Importance: log2 of the conditional probability ratio.
func refImportance(t *table) (Value, error) {
	if v, ok := t.condProbSpecial(); ok {
		return v, nil
	}
	if t.s == 0 {
		// log2(0) = -Inf; upstream (re_import) returns 0 for s <= 0.
		return conv(0, ConvUndefined), nil
	}
	l := lnRat(t.condProbRatio(), refPrec)
	v := float64Of(l.Quo(l, ln2Big(refPrec)))
	return Value{V: v, Tol: ratTau * max(1, math.Abs(v))}, nil
}

// certValue returns the exact certainty factor r with the absolute
// tolerance certTau (|r| <= 1).
func certValue(r *big.Rat) Value {
	v := ratFloat64(r)
	return Value{V: v, Tol: certTau}
}

// Certainty factor (Shortliffe and Buchanan; Berzal et al.): with the
// prior p = h/n and the confidence c = s/b, (c - p)/(1 - p) if c >= p and
// (c - p)/p if c < p.
func refCertainty(t *table) (Value, error) {
	if t.b == 0 {
		return conv(0, ConvUndefined), nil
	}
	c := frac(t.bs, t.bb)
	p := frac(t.bh, t.bn)
	d := new(big.Rat).Sub(c, p)
	if d.Sign() < 0 {
		return certValue(d.Quo(d, p)), nil // p > c >= 0
	}
	if t.h == t.n {
		return conv(0, ConvR5), nil // (1 - 1)/(1 - 1)
	}
	return certValue(d.Quo(d, new(big.Rat).Sub(big.NewRat(1, 1), p))), nil
}

// chi2Stat returns Pearson's statistic X² = Σ (O - E)²/E over the four
// cells, E = r·k/n, or nil for a degenerate table (some E = 0).
func (t *table) chi2Stat() *big.Rat {
	if t.degenerate() {
		return nil
	}
	sum := new(big.Rat)
	for _, c := range t.cells() {
		e := frac(new(big.Int).Mul(c.r, c.k), t.bn)
		d := new(big.Rat).Sub(new(big.Rat).SetInt(c.c), e)
		d.Mul(d, d)
		sum.Add(sum, d.Quo(d, e))
	}
	return sum
}

// Chi2: upstream's normalized chi² measure X²/n.
func refChi2(t *table) (Value, error) {
	x := t.chi2Stat()
	if x == nil {
		return conv(0, ConvUndefined), nil
	}
	return relTau(x.Quo(x, frac(t.bn, big.NewInt(1))), chiTau), nil
}

// Chi2PValue: P(chi²(1) > X²).
func refChi2PValue(t *table) (Value, error) {
	x := t.chi2Stat()
	if x == nil {
		return conv(1, ConvUndefined), nil
	}
	return pValue(ratFloat64(x)), nil
}

// yatesStat returns Yates' corrected statistic Σ (max(0, |O - E| - 1/2))²/E
// and whether the correction was clamped (|O - E| < 1/2; for a 2x2 table
// |O - E| is the same in all cells), or nil for a degenerate table.
func (t *table) yatesStat() (*big.Rat, bool) {
	if t.degenerate() {
		return nil, false
	}
	sum := new(big.Rat)
	clamped := false
	half := big.NewRat(1, 2)
	for _, c := range t.cells() {
		e := frac(new(big.Int).Mul(c.r, c.k), t.bn)
		d := new(big.Rat).Sub(new(big.Rat).SetInt(c.c), e)
		d.Abs(d)
		d.Sub(d, half)
		if d.Sign() < 0 {
			d.SetInt64(0)
			clamped = true
		}
		d.Mul(d, d)
		sum.Add(sum, d.Quo(d, e))
	}
	return sum, clamped
}

// Yates: upstream's normalized chi² measure with Yates' correction, X²_Y/n.
func refYates(t *table) (Value, error) {
	x, clamped := t.yatesStat()
	if x == nil {
		return conv(0, ConvUndefined), nil
	}
	if clamped {
		return conv(0, ConvR9b), nil
	}
	return relTau(x.Quo(x, frac(t.bn, big.NewInt(1))), chiTau), nil
}

// YatesPValue: P(chi²(1) > X²_Y).
func refYatesPValue(t *table) (Value, error) {
	x, clamped := t.yatesStat()
	if x == nil {
		return conv(1, ConvUndefined), nil
	}
	if clamped {
		return conv(1, ConvR9b), nil
	}
	return pValue(ratFloat64(x)), nil
}

// infoPrec returns the working precision of the mutual information of a
// table with total weight n: 128 + 2·bitlen(n) bits (see mutualInfo).
func infoPrec(n int64) uint {
	return refPrec + 2*uint(bits.Len64(uint64(n)))
}

// mutualInfo returns the mutual information of body and head in nats,
//
//	I = Σ_{cells, c > 0} (c/n)·ln(c·n/(r·k)),
//
// computed as follows: with eps = (c·n - r·k)/(r·k) (exact), ln(1 + eps)
// = eps + (ln(1 + eps) - eps), and Σ (c/n)·eps = Σ c²/(r·k) - 1 is the
// exact rational φ² >= 0, while the remainders (c/n)·(ln(1 + eps) - eps)
// are all <= 0, computed with a small relative error (log1pMinus) and
// summed without cancellation. The one cancellation left is I = φ² -
// |remainders|. It is bounded: all four cells deviate from their
// expectation by the same d (in probabilities), so φ² = d²·Σ 1/q over the
// expected cell probabilities q, while Pinsker's inequality gives I >=
// (1/2)·(4d)² = 8d²; hence φ²/I <= Σ 1/q / 8 <= 1/(2·min q) <= n²/2. The
// working precision infoPrec(n) = 128 + 2·bitlen(n) bits therefore leaves
// at least 128 bits of I. I is exactly 0 for an independent table (s·n =
// b·h, all eps = 0) and for degenerate margins.
func (t *table) mutualInfo() *big.Float {
	prec := infoPrec(t.n)
	phi2 := big.NewRat(-1, 1)
	rest := newFloat(prec)
	for _, c := range t.cells() {
		if c.c.Sign() == 0 {
			continue // 0·ln 0 = 0; c > 0 implies r, k > 0
		}
		rk := new(big.Int).Mul(c.r, c.k)
		phi2.Add(phi2, frac(new(big.Int).Mul(c.c, c.c), rk))
		eps := new(big.Int).Mul(c.c, t.bn)
		eps.Sub(eps, rk)
		if eps.Sign() == 0 {
			continue
		}
		term := log1pMinus(frac(eps, rk), prec)
		term.Mul(term, floatFromRat(frac(c.c, t.bn), prec))
		rest.Add(rest, term)
	}
	mi := floatFromRat(phi2, prec)
	mi.Add(mi, rest)
	if mi.Sign() < 0 { // only by rounding, far below 2^-100 relative
		mi.SetInt64(0)
	}
	return mi
}

// Information: the mutual information in bits, compared relatively
// (infoTau).
func refInfo(t *table) (Value, error) {
	mi := t.mutualInfo()
	v := float64Of(mi.Quo(mi, ln2Big(infoPrec(t.n))))
	return Value{V: v, Tol: infoTau * v}, nil
}

// InformationPValue: P(chi²(1) > G) with the G statistic G = 2·n·I (I in
// nats). G = 0 (independence, degenerate margins) gives exactly 1.
func refInfoPValue(t *table) (Value, error) {
	prec := infoPrec(t.n)
	g := t.mutualInfo()
	g.Mul(g, newFloat(prec).SetInt64(t.n))
	g.Mul(g, newFloat(prec).SetInt64(2))
	return pValue(float64Of(g)), nil
}

// Fisher's exact tests.
//
// For fixed margins b, h and n, the support X of B ∪ {h} is
// hypergeometric: P(X = x) = C(h, x)·C(n-h, b-x)/C(n, b) for x in
// [lo, hi] = [max(0, b+h-n), min(b, h)]. The reference uses exact integer
// weights u(x) proportional to P(X = x): with num(k) = (h-k)·(b-k) and
// den(k) = (k+1)·(n-h-b+k+1), P(x+1)/P(x) = num(x)/den(x), so
//
//	u(x) = Π_{k=lo}^{x-1} num(k) · Π_{k=x}^{hi-1} den(k)
//
// and every p-value is the exact rational Σ_{x in A} u(x) / Σ_x u(x) for
// the acceptance region A of the variant:
//
//	f (FisherProb):    P(X = x) <= P(X = s), i.e. u(x) <= u(s);
//	h (FisherChi2):    X²(x) >= X²(s), i.e. |n·x - b·h| >= |n·s - b·h|;
//	m (FisherInfo):    I(x) >= I(s), where I(x) is the mutual information,
//	                   i.e. Σ c·ln c >= over the cells (the margins are
//	                   fixed), decided exactly as Π c^c where feasible;
//	s (FisherSupport): x >= s (one-sided upper tail).
//
// Ties count as in the region (differences.md R6; the exact definition).
// For degenerate margins there is one table and every p-value is 1.

// fisherRef returns the reference function of the Fisher variant v.
func fisherRef(v byte) func(t *table) (Value, error) {
	return func(t *table) (Value, error) { return fisher(v, t) }
}

func fisher(variant byte, t *table) (Value, error) {
	lo := max(0, t.b-(t.n-t.h)) // b + h - n without overflow
	hi := min(t.b, t.h)
	cnt := hi - lo + 1
	if cnt > FisherMaxTables {
		return Value{}, fmt.Errorf("%w: Fisher test over %d tables (limit %d)", ErrOutOfScope, cnt, FisherMaxTables)
	}
	rest := t.n - t.b - t.h // may be negative
	k := func(x int64) *big.Int { return big.NewInt(x) }
	num := func(x int64) *big.Int { return new(big.Int).Mul(k(t.h-x), k(t.b-x)) }
	den := func(x int64) *big.Int { return new(big.Int).Mul(k(x+1), k(rest+x+1)) }
	pre := make([]*big.Int, cnt)
	suf := make([]*big.Int, cnt)
	pre[0] = big.NewInt(1)
	for i := int64(1); i < cnt; i++ {
		pre[i] = new(big.Int).Mul(pre[i-1], num(lo+i-1))
	}
	suf[cnt-1] = big.NewInt(1)
	for i := cnt - 2; i >= 0; i-- {
		suf[i] = new(big.Int).Mul(suf[i+1], den(lo+i))
	}
	u := make([]*big.Int, cnt)
	total := new(big.Int)
	for i := range cnt {
		u[i] = new(big.Int).Mul(pre[i], suf[i])
		total.Add(total, u[i])
	}
	is := t.s - lo
	var inRegion func(i int64) (bool, error)
	switch variant {
	case 'f':
		inRegion = func(i int64) (bool, error) { return u[i].Cmp(u[is]) <= 0, nil }
	case 'h':
		dev := func(x int64) *big.Int {
			d := new(big.Int).Mul(t.bn, k(x))
			d.Sub(d, new(big.Int).Mul(t.bb, t.bh))
			return d.Abs(d)
		}
		ds := dev(t.s)
		inRegion = func(i int64) (bool, error) { return dev(lo+i).Cmp(ds) >= 0, nil }
	case 'm':
		inRegion = func(i int64) (bool, error) {
			c, err := t.cmpInfo(lo+i, t.s)
			return c >= 0, err
		}
	case 's':
		inRegion = func(i int64) (bool, error) { return lo+i >= t.s, nil }
	default:
		panic("oracle: unknown Fisher variant")
	}
	sum := new(big.Int)
	for i := range cnt {
		in, err := inRegion(i)
		if err != nil {
			return Value{}, err
		}
		if in {
			sum.Add(sum, u[i])
		}
	}
	v := ratFloat64(frac(sum, total))
	return Value{V: v, Tol: fisherTol(variant, t.n, v)}, nil
}

// fisherTol is the tolerance of a Fisher p-value v for total weight n:
// fisherTau·v + 2^-1000 (the bound documented by the implementation, which
// computes ln P(x) from Stirling's formula with an exact deviation for
// n > 170 and sums the tables directly). For n <= 170 the implementation
// keeps upstream's algorithm, which computes FisherChi2 and FisherSupport
// as complements 1 - Σ when that sums fewer tables, so their error is
// absolute there: fisherTau·(v + 1).
func fisherTol(variant byte, n int64, v float64) float64 {
	if n <= 170 && (variant == 'h' || variant == 's') {
		return fisherTau*(v+1) + 0x1p-1000
	}
	return fisherTau*v + 0x1p-1000
}

// cmpInfo compares the mutual information of the tables with supports x
// and y (same margins): the sign of I(x) - I(y). With fixed margins, n·I
// = Σ_cells c·ln c - Σ_margins m·ln m + n·ln n, so the sign is that of
// F(x) - F(y) with F = Σ_cells c·ln c, i.e. of Π c^c(x) - Π c^c(y). Equal
// sorted cells are a tie. Otherwise F is compared in float64 when the
// difference exceeds a bound on its rounding error; else the products are
// compared exactly if they have at most 2^18 bits, and beyond that F is
// computed with refPrec-bit logarithms and the difference must exceed a
// bound on their error; if it does not, ErrOutOfScope is returned.
func (t *table) cmpInfo(x, y int64) (int, error) {
	cellsOf := func(v int64) [4]int64 {
		c := [4]int64{v, t.b - v, t.h - v, t.n - t.b - t.h + v}
		slices.Sort(c[:])
		return c
	}
	cx, cy := cellsOf(x), cellsOf(y)
	if cx == cy {
		return 0, nil
	}
	// Fast path: F in float64. Each term c·ln c (math.Log has an error
	// below one ulp) and each of the three additions adds at most a
	// relative 2^-52 of the magnitudes involved, so the error of F is
	// below 2^-49·Σ c·|ln c|; the sign is certain if |F(x) - F(y)| exceeds
	// twice that.
	fx, ax := xlogxSum(cx)
	fy, ay := xlogxSum(cy)
	if d := fx - fy; math.Abs(d) > 0x1p-48*(ax+ay)+0x1p-1000 {
		if d > 0 {
			return 1, nil
		}
		return -1, nil
	}
	var bitsNeeded float64
	for _, c := range append(cx[:], cy[:]...) {
		if c > 1 {
			bitsNeeded += float64(c) * math.Log2(float64(c))
		}
	}
	if bitsNeeded <= 1<<18 {
		pow := func(c [4]int64) *big.Int {
			p := big.NewInt(1)
			for _, v := range c {
				if v > 1 {
					p.Mul(p, new(big.Int).Exp(big.NewInt(v), big.NewInt(v), nil))
				}
			}
			return p
		}
		return pow(cx).Cmp(pow(cy)), nil
	}
	f := func(c [4]int64) *big.Float {
		sum := newFloat(refPrec)
		for _, v := range c {
			if v > 1 {
				l := lnBig(newFloat(refPrec).SetInt64(v))
				sum.Add(sum, l.Mul(l, newFloat(refPrec).SetInt64(v)))
			}
		}
		return sum
	}
	d := f(cx)
	d.Sub(d, f(cy))
	// Each c·ln c (< 2^69) has an error below 2^(69-refPrec+8); eight of
	// them.
	if d.Sign() == 0 || d.MantExp(nil) < 69-refPrec+12 {
		return 0, fmt.Errorf("%w: FisherInfo tie between supports %d and %d undecidable", ErrOutOfScope, x, y)
	}
	return d.Sign(), nil
}

// xlogxSum returns Σ c·ln c over the cells in float64 and Σ c·|ln c|.
func xlogxSum(c [4]int64) (sum, abs float64) {
	for _, v := range c {
		if v > 1 {
			f := float64(v)
			t := f * math.Log(f)
			sum += t
			abs += math.Abs(t)
		}
	}
	return sum, abs
}
