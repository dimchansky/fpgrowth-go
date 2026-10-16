package fpgrowth

import (
	"fmt"
	"math"
	"strconv"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Target selects the kind of item sets to report (upstream -t).
//
// Closedness, maximality and the generator property are decided on all
// frequent item sets; the size limits, the maximum support, the border and
// the evaluation only filter which sets of the target are reported (as
// upstream does).
type Target int

// Item set targets.
const (
	// TargetFrequent reports all frequent item sets (upstream -ts,
	// default).
	TargetFrequent Target = iota
	// TargetClosed reports closed item sets: frequent sets without a
	// superset of the same support (-tc).
	TargetClosed
	// TargetMaximal reports maximal item sets: frequent sets without a
	// frequent superset (-tm).
	TargetMaximal
	// TargetGenerators reports generators (free or key item sets):
	// frequent sets without a subset of the same support (-tg).
	TargetGenerators
)

// String returns the name of the constant.
func (t Target) String() string {
	switch t {
	case TargetFrequent:
		return "TargetFrequent"
	case TargetClosed:
		return "TargetClosed"
	case TargetMaximal:
		return "TargetMaximal"
	case TargetGenerators:
		return "TargetGenerators"
	}
	return fmt.Sprintf("Target(%d)", int(t))
}

func (t Target) internal() report.Target {
	switch t {
	case TargetClosed:
		return report.TargetClosed
	case TargetMaximal:
		return report.TargetMaximal
	case TargetGenerators:
		return report.TargetGenerators
	}
	return report.TargetAll
}

// Algorithm selects the FP-growth variant (upstream -A). All variants
// report the same item sets and rules with the same supports; the order of
// the results and the speed differ.
type Algorithm int

// FP-growth variants.
const (
	// AlgorithmAuto lets the library choose; currently AlgorithmComplex
	// (upstream default).
	AlgorithmAuto Algorithm = iota
	// AlgorithmSimple uses simple tree nodes (-As).
	AlgorithmSimple
	// AlgorithmComplex uses complex tree nodes with item reordering (-Ac).
	AlgorithmComplex
	// AlgorithmSingleTree processes a single prefix tree top-down (-Ad).
	AlgorithmSingleTree
	// AlgorithmTopDown processes the prefix trees top-down (-At). It does
	// not use the k-items machine (Tuning.ItemMachine).
	AlgorithmTopDown
)

// String returns the name of the constant.
func (a Algorithm) String() string {
	switch a {
	case AlgorithmAuto:
		return "AlgorithmAuto"
	case AlgorithmSimple:
		return "AlgorithmSimple"
	case AlgorithmComplex:
		return "AlgorithmComplex"
	case AlgorithmSingleTree:
		return "AlgorithmSingleTree"
	case AlgorithmTopDown:
		return "AlgorithmTopDown"
	}
	return fmt.Sprintf("Algorithm(%d)", int(a))
}

func (a Algorithm) internal() fpg.Algorithm {
	switch a {
	case AlgorithmSimple:
		return fpg.AlgoSimple
	case AlgorithmComplex:
		return fpg.AlgoComplex
	case AlgorithmSingleTree:
		return fpg.AlgoSingle
	case AlgorithmTopDown:
		return fpg.AlgoTopDown
	}
	return fpg.AlgoAuto
}

// ItemOrder selects how items are ordered before mining (upstream -q).
// It affects speed and the order of the results, not the results
// themselves.
type ItemOrder int

// Item orders, named like upstream's help text.
const (
	// ItemOrderDefault is upstream's default, ItemOrderSizeSumAscending.
	ItemOrderDefault ItemOrder = iota
	// ItemOrderSizeSumAscending sorts ascending w.r.t. the transaction
	// size sum (-q2).
	ItemOrderSizeSumAscending
	// ItemOrderFrequencyAscending sorts ascending w.r.t. frequency (-q1).
	ItemOrderFrequencyAscending
	// ItemOrderNone keeps the order of first occurrence (-q0).
	ItemOrderNone
	// ItemOrderFrequencyDescending sorts descending w.r.t. frequency
	// (-q-1).
	ItemOrderFrequencyDescending
	// ItemOrderSizeSumDescending sorts descending w.r.t. the transaction
	// size sum (-q-2).
	ItemOrderSizeSumDescending
)

func (o ItemOrder) internal() int {
	switch o {
	case ItemOrderFrequencyAscending:
		return 1
	case ItemOrderNone:
		return 0
	case ItemOrderFrequencyDescending:
		return -1
	case ItemOrderSizeSumDescending:
		return -2
	}
	return 2
}

// Tuning holds performance options. The zero value is upstream's
// defaults; none of them changes which item sets or rules are found, only
// speed and the order of the results.
type Tuning struct {
	// NoPerfectExtensions disables perfect extension pruning (-x).
	// Generators always use it.
	NoPerfectExtensions bool
	// NoReorder disables item reordering in conditional trees of the
	// Complex variant (-j).
	NoReorder bool
	// NoHeadUnionTail disables head union tail pruning for maximal item
	// sets (-u).
	NoHeadUnionTail bool
	// ItemMachine is the number of items processed with the k-items
	// machine (-l): 0 means upstream's default 16, a negative value
	// disables it, at most 16.
	ItemMachine int
	// ItemOrder is the item order (-q).
	ItemOrder ItemOrder
}

func (t *Tuning) validate() error {
	switch {
	case t.ItemMachine > 16:
		return invalid("Tuning.ItemMachine", "%d exceeds 16", t.ItemMachine)
	case t.ItemOrder < ItemOrderDefault || t.ItemOrder > ItemOrderSizeSumDescending:
		return invalid("Tuning.ItemOrder", "invalid value %d", int(t.ItemOrder))
	}
	return nil
}

func (t *Tuning) apply(c *fpg.Config) {
	if t.NoPerfectExtensions {
		c.Mode &^= fpg.ModePerfect
	}
	if t.NoReorder {
		c.Mode &^= fpg.ModeReorder
	}
	if t.NoHeadUnionTail {
		c.Mode &^= fpg.ModeTail
	}
	switch {
	case t.ItemMachine == 0:
		c.Pack = 16
	case t.ItemMachine < 0:
		c.Pack = 0
	default:
		c.Pack = t.ItemMachine
	}
	c.Sort = t.ItemOrder.internal()
}

// supportKind tells how a Support is given.
type supportKind uint8

const (
	supportUnset supportKind = iota
	supportCount
	supportFraction
)

// Support is a support threshold: an absolute weighted count (Count) or a
// fraction of the total transaction weight (Fraction). The zero value is
// "not set".
type Support struct {
	kind  supportKind
	count int64
	frac  float64
}

// Count returns the absolute support n: the sum of the weights of the
// transactions that contain an item set. n must not be negative. A
// minimum support below 1 acts as 1. Counts are exact over the whole
// int64 range; both bounds are inclusive.
func Count(n int64) Support { return Support{kind: supportCount, count: n} }

// Fraction returns a support of f times the total transaction weight W,
// 0 < f <= 1. As a minimum it is the absolute support ceil(f·W·(1-ε)), as
// a maximum floor(f·W·(1+ε)), with ε = 2^-52: like upstream's percentage
// thresholds, this tolerates the representation error of decimal
// fractions, so Fraction(0.3) of W = 10 is exactly 3. Fraction(k/100)
// gives exactly the thresholds of the command line options -sk and -Sk.
// The tolerance is relative: for a total weight beyond 2^53 it is more
// than one (Fraction(1) of W = 2^60+1 is a minimum of W-256); use Count
// for exact bounds.
func Fraction(f float64) Support { return Support{kind: supportFraction, frac: f} }

// IsZero reports whether s is the zero value (not set).
func (s Support) IsZero() bool { return s.kind == supportUnset }

// String describes s, e.g. "Count(5)" or "Fraction(0.3)".
func (s Support) String() string {
	switch s.kind {
	case supportCount:
		return "Count(" + strconv.FormatInt(s.count, 10) + ")"
	case supportFraction:
		return "Fraction(" + strconv.FormatFloat(s.frac, 'g', -1, 64) + ")"
	}
	return "unset"
}

// validate checks a support threshold that is set.
func (s Support) validate(field string) error {
	switch s.kind {
	case supportCount:
		if s.count < 0 {
			return invalid(field, "negative count %d", s.count)
		}
	case supportFraction:
		if !(s.frac > 0 && s.frac <= 1) {
			return invalid(field, "fraction %g outside (0, 1]", s.frac)
		}
	}
	return nil
}

// dblEpsilon is C's DBL_EPSILON, the tolerance of upstream's threshold
// conversions.
const dblEpsilon = 0x1p-52

// minAbs converts a minimum support to an absolute support for the total
// weight w (upstream fpg_data and setbdr arithmetic for fractions).
func (s Support) minAbs(w int64) int64 {
	switch s.kind {
	case supportCount:
		return s.count
	case supportFraction:
		return suppFromFloat(math.Ceil(s.frac * float64(w) * (1 - dblEpsilon)))
	}
	return 0
}

// maxAbs converts a maximum support to an absolute support for the total
// weight w (fpg_report arithmetic for fractions); the zero value gives
// math.MaxInt64 (no limit).
func (s Support) maxAbs(w int64) int64 {
	switch s.kind {
	case supportCount:
		return s.count
	case supportFraction:
		return suppFromFloat(math.Floor(s.frac * float64(w) * (1 + dblEpsilon)))
	}
	return math.MaxInt64
}

// suppFromFloat converts an integral float64 to int64, saturating.
func suppFromFloat(x float64) int64 {
	switch {
	case !(x > 0):
		return 0
	case x >= 0x1p63:
		return math.MaxInt64
	}
	return int64(x)
}

// percentOf converts a fraction to the percentage p that the internal
// code divides by 100 again. If no float64 p has p/100 == f exactly
// (about 15% of all values), it takes the neighbor on the permissive side
// of f: p/100 < f for dir > 0 (f is a minimum), p/100 > f otherwise.
func percentOf(f float64, dir int) float64 {
	p := f * 100
	if p/100 == f || math.IsInf(p, 0) || math.IsNaN(p) {
		return p
	}
	up, down := math.Inf(1), math.Inf(-1)
	if dir > 0 {
		for p/100 > f {
			p = math.Nextafter(p, down)
		}
		for q := math.Nextafter(p, up); q/100 <= f; q = math.Nextafter(q, up) {
			p = q
		}
	} else {
		for p/100 < f {
			p = math.Nextafter(p, up)
		}
		for q := math.Nextafter(p, down); q/100 >= f; q = math.Nextafter(q, down) {
			p = q
		}
	}
	return p
}

// invalid returns an ErrInvalidOptions error for field.
func invalid(field, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalidOptions, field, fmt.Sprintf(format, args...))
}

// Options configures item set mining. The zero value is invalid only
// because MinSupport is required; all other zero values are upstream's
// defaults.
type Options[T comparable] struct {
	// Target is the kind of item sets to report.
	Target Target
	// MinSupport is the minimum support of an item set. Required.
	MinSupport Support
	// MaxSupport is the maximum support of a reported item set; the zero
	// value means no limit.
	MaxSupport Support
	// MinSize is the minimum number of items of a reported set; 0 means 1.
	// The empty set is reported only with IncludeEmpty.
	MinSize int
	// MaxSize is the maximum number of items of a reported set; 0 means no
	// limit.
	MaxSize int
	// IncludeEmpty also reports the empty set (upstream -m0) if it belongs
	// to the target; MinSize must then be at most 1.
	IncludeEmpty bool
	// Border is a support border (upstream -F): Border[i] is the minimum
	// support of reported sets with zmin+i items, where zmin is the
	// effective minimum size (0 with IncludeEmpty, else max(MinSize, 1)).
	// Zero entries do not restrict their size; sizes beyond the border are
	// not restricted.
	Border []Support
	// Items, if not empty, restricts mining to these items (upstream -R
	// item selection); other items are removed from every transaction.
	// The transactions still count toward the total weight.
	Items []T
	// Algorithm selects the FP-growth variant.
	Algorithm Algorithm
	// Tuning holds performance options.
	Tuning Tuning
	// Evaluation, if not nil, computes an evaluation measure for every
	// reported item set and puts it into Itemset.Value; with
	// Evaluation.Filter it also filters the item sets by
	// Evaluation.Threshold (and enables Evaluation.PruneSize). Without
	// Filter (the zero value) no item set is removed. nil leaves Value
	// NaN.
	Evaluation *Evaluation
	// PatternSpectrum fills Summary.Spectrum.
	PatternSpectrum bool
	// OnStart, if not nil, is called once before the first item set is
	// reported (also if none is), with the Stats of the database: the
	// total weight for relative supports, the resolved thresholds;
	// not if ctx is canceled by then. Its error stops mining and is
	// returned unchanged.
	OnStart func(Stats) error
}

// sizes validates and resolves the size range (zmin, zmax).
func sizes(minSize, maxSize int, includeEmpty bool) (zmin, zmax int, err error) {
	switch {
	case minSize < 0:
		return 0, 0, invalid("MinSize", "negative value %d", minSize)
	case maxSize < 0:
		return 0, 0, invalid("MaxSize", "negative value %d", maxSize)
	case minSize > fpg.MaxSize:
		return 0, 0, invalid("MinSize", "%d too large", minSize)
	case includeEmpty && minSize > 1:
		return 0, 0, invalid("IncludeEmpty", "requires MinSize <= 1, have %d", minSize)
	}
	zmin = max(minSize, 1)
	if includeEmpty {
		zmin = 0
	}
	zmax = fpg.MaxSize
	if maxSize > 0 && maxSize < fpg.MaxSize {
		zmax = maxSize
	}
	if zmax < zmin {
		return 0, 0, invalid("MaxSize", "%d below the minimum size %d", maxSize, zmin)
	}
	return zmin, zmax, nil
}

// plan is a validated mining configuration. The support thresholds are
// converted after the first pass, when the total weight is known.
type plan struct {
	cfg        fpg.Config
	minSupport Support
	maxSupport Support
	border     []Support
	spectrum   bool
	hasEval    bool
	onStart    func(Stats) error
}

// setThresholds resolves the support thresholds for st.TotalWeight, stores
// them in st and, as exact absolute supports, in p.cfg, and returns the
// absolute border.
func (p *plan) setThresholds(st *Stats) []tract.Supp {
	w := st.TotalWeight
	st.MinSupport = max(p.minSupport.minAbs(w), 1)
	st.MaxSupport = p.maxSupport.maxAbs(w)
	p.cfg.HasCounts = true
	p.cfg.MinCount, p.cfg.MaxCount = tract.Supp(st.MinSupport), tract.Supp(st.MaxSupport)
	if len(p.border) == 0 {
		return nil
	}
	border := make([]tract.Supp, len(p.border))
	for i, s := range p.border {
		border[i] = tract.Supp(s.minAbs(w))
	}
	return border
}

// plan validates o and translates it to the miner configuration.
func (o *Options[T]) plan() (*plan, error) {
	if o.MinSupport.IsZero() {
		return nil, invalid("MinSupport", "required")
	}
	if err := o.MinSupport.validate("MinSupport"); err != nil {
		return nil, err
	}
	if err := o.MaxSupport.validate("MaxSupport"); err != nil {
		return nil, err
	}
	for i, s := range o.Border {
		if err := s.validate(fmt.Sprintf("Border[%d]", i)); err != nil {
			return nil, err
		}
	}
	if o.Target < TargetFrequent || o.Target > TargetGenerators {
		return nil, invalid("Target", "invalid value %d", int(o.Target))
	}
	if o.Algorithm < AlgorithmAuto || o.Algorithm > AlgorithmTopDown {
		return nil, invalid("Algorithm", "invalid value %d", int(o.Algorithm))
	}
	if err := o.Tuning.validate(); err != nil {
		return nil, err
	}
	zmin, zmax, err := sizes(o.MinSize, o.MaxSize, o.IncludeEmpty)
	if err != nil {
		return nil, err
	}
	if o.Evaluation != nil {
		if err := o.Evaluation.validate(false); err != nil {
			return nil, err
		}
	}
	cfg := fpg.DefaultConfig()
	cfg.Target = o.Target.internal()
	cfg.ZMin, cfg.ZMax = zmin, zmax
	cfg.Algo = o.Algorithm.internal()
	o.Tuning.apply(&cfg)
	if o.Evaluation != nil {
		o.Evaluation.apply(&cfg)
	}
	return &plan{
		cfg:        cfg,
		minSupport: o.MinSupport,
		maxSupport: o.MaxSupport,
		border:     o.Border,
		spectrum:   o.PatternSpectrum,
		hasEval:    o.Evaluation != nil,
		onStart:    o.OnStart,
	}, nil
}
