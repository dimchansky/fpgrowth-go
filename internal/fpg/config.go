package fpg

import (
	"errors"
	"fmt"
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
)

// Algorithm selects the FP-growth variant (upstream FPG_SIMPLE ...
// FPG_AUTO, option -A). The values are upstream's.
type Algorithm int

// FP-growth variants.
const (
	AlgoSimple  Algorithm = 0 // -As: simple tree nodes (parent/successor)
	AlgoComplex Algorithm = 1 // -Ac: complex nodes (children/sibling), default
	AlgoSingle  Algorithm = 2 // -Ad: single tree, top-down processing
	AlgoTopDown Algorithm = 3 // -At: top-down trees
	AlgoAuto    Algorithm = 4 // automatic choice (becomes AlgoComplex)
)

// String returns the -A letter of the variant.
func (a Algorithm) String() string {
	switch a {
	case AlgoSimple:
		return "s"
	case AlgoComplex:
		return "c"
	case AlgoSingle:
		return "d"
	case AlgoTopDown:
		return "t"
	case AlgoAuto:
		return "auto"
	default:
		return fmt.Sprintf("Algorithm(%d)", int(a))
	}
}

// Mode holds operation mode flags (upstream FPG_* mode bits). The values
// are upstream's; the 16-items machine count (FPG_FIM16) is Config.Pack and
// the output-only flags (FPG_PREFMT, FPG_VERBOSE, FPG_NOCLEAN) belong to the
// caller.
type Mode int

// Mode flags.
const (
	// ModePerfect enables perfect extension pruning (-x clears it).
	ModePerfect Mode = 0x0020
	// ModeReorder reorders items in conditional trees of the complex
	// variant (-j clears it).
	ModeReorder Mode = 0x0040
	// ModeOrigSupp makes the rule support the support of body and head
	// together (-o).
	ModeOrigSupp Mode = 0x0080
	// ModeTail enables head union tail pruning for maximal item sets
	// (-u clears it).
	ModeTail Mode = 0x0100
	// ModeDefault is upstream's FPG_DEFAULT without the FIM16 field.
	ModeDefault = ModePerfect | ModeReorder | ModeTail

	modeAll = ModePerfect | ModeReorder | ModeOrigSupp | ModeTail
)

// Evaluation measure codes (upstream -e). Codes between EvalNone and
// EvalLogRatio (exclusive) are the rule evaluation measures of ruleval
// (RE_* codes); they are evaluated on the item set tree (rules path).
const (
	EvalNone     = 0  // RE_NONE: no evaluation measure (-ex)
	EvalLogRatio = 26 // FPG_LDRATIO = RE_FNCNT: binary log support ratio (-eb)
)

// Aggregation modes of rule-based item set evaluation (upstream FPG_NONE,
// FPG_FIRST, FPG_MIN, FPG_MAX, FPG_AVG, option -a). Only used by the rules
// path.
const (
	AggNone  = 0
	AggFirst = 0
	AggMin   = 1
	AggMax   = 2
	AggAvg   = 3
)

// NoPrune is upstream's ITEM_MIN, the default of Config.Prune (no
// evaluation-based pruning).
const NoPrune = math.MinInt32

// MaxSize is the "no limit" item set size (upstream ITEM_MAX).
const MaxSize = report.MaxSize

// Config holds the parameters of fpg_create (and the -q sort argument of
// fpg_data, the prune argument of fpg_mine). Start from DefaultConfig.
type Config struct {
	// Target is a bit set of report targets; it is normalized to one
	// target with precedence rules > generators > maximal > closed >
	// frequent (report.TargetAll), like fpg_create.
	Target report.Target
	// SMin is the minimum support: a percentage of the total transaction
	// weight if >= 0, else an absolute support (its negation). At most 100.
	SMin float64
	// SMax is the maximum support, in the same notation as SMin.
	SMax float64
	// Conf is the minimum confidence of rules in percent, in [0, 100].
	Conf float64
	// ZMin and ZMax are the minimum and maximum item set (rule) size;
	// ZMax >= MaxSize means no limit. Both must be >= 0.
	ZMin, ZMax int
	// Eval is the evaluation measure code (EvalNone, EvalLogRatio or a
	// rule measure code below EvalLogRatio, i.e. a ruleval.Measure). For
	// association rules EvalLogRatio selects ruleval.Imbalance, the rule
	// measure of the same -e letter 'b' (deviation for rules-eval.md B1).
	Eval int
	// InvalidateBelowExpected is upstream's FPG_INVBXS flag (-i), passed
	// to rule-based evaluation.
	InvalidateBelowExpected bool
	// Agg is the aggregation mode of rule-based evaluation (Agg*).
	Agg int
	// Thresh is the evaluation threshold in percent (-d).
	Thresh float64
	// Algo is the FP-growth variant.
	Algo Algorithm
	// Mode holds the operation mode flags.
	Mode Mode
	// Pack is the number of items to process with the 16-items machine
	// (upstream FPG_FIM16 field, -l), 0 to disable it. Values above 16
	// act as 16. For AlgoComplex only zero versus non-zero matters.
	Pack int
	// Sort is the item sort direction for recoding (-q): see
	// tract.ItemBase.Recode.
	Sort int
	// Prune is the minimum size for evaluation filtering (-p) of the
	// rules path; NoPrune disables it.
	Prune int
}

// DefaultConfig returns the fpgrowth program's defaults: all frequent item
// sets with minimum support 10%, maximum support 100%, confidence 80%,
// sizes from 1 without limit, no evaluation, complex variant, default
// mode, 16-items machine for 16 items, -q2.
func DefaultConfig() Config {
	return Config{
		Target: report.TargetAll,
		SMin:   10,
		SMax:   100,
		Conf:   80,
		ZMin:   1,
		ZMax:   MaxSize,
		Eval:   EvalNone,
		Agg:    AggNone,
		Thresh: 10,
		Algo:   AlgoComplex,
		Mode:   ModeDefault,
		Pack:   16,
		Sort:   2,
		Prune:  NoPrune,
	}
}

// Errors returned for invalid parameters and data.
var (
	// ErrConfig reports an invalid parameter; it wraps a description.
	ErrConfig = errors.New("fpg: invalid configuration")
	// ErrNoItems reports that no item is frequent (upstream E_NOITEMS,
	// "no (frequent) items found").
	ErrNoItems = errors.New("fpg: no (frequent) items found")
	// ErrState reports a call out of the New, Data, Reporter, Mine order.
	ErrState = errors.New("fpg: method called out of order")
)

const targetAll = report.TargetClosed | report.TargetMaximal |
	report.TargetGenerators | report.TargetRules

// validate checks the parameters like the fpgrowth program's main does,
// and additionally rejects values main leaves undefined (NaN thresholds,
// unknown flags).
func (c *Config) validate() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrConfig, fmt.Sprintf(format, args...))
	}
	switch {
	case c.Target < 0 || c.Target&^targetAll != 0:
		return bad("invalid target %#x", int(c.Target))
	case math.IsNaN(c.SMin) || c.SMin > 100:
		return bad("invalid minimum support %g", c.SMin)
	case math.IsNaN(c.SMax):
		return bad("invalid maximum support %g", c.SMax)
	case !(c.Conf >= 0 && c.Conf <= 100):
		return bad("invalid minimum confidence %g", c.Conf)
	case c.ZMin < 0:
		return bad("invalid minimum item set size %d", c.ZMin)
	case c.ZMax < 0:
		return bad("invalid maximum item set size %d", c.ZMax)
	case c.Eval < EvalNone || c.Eval > EvalLogRatio:
		return bad("invalid evaluation measure %d", c.Eval)
	case c.Agg < AggNone || c.Agg > AggAvg:
		return bad("invalid aggregation mode %d", c.Agg)
	case math.IsNaN(c.Thresh):
		return bad("invalid evaluation threshold %g", c.Thresh)
	case c.Algo < AlgoSimple || c.Algo > AlgoAuto:
		return bad("invalid algorithm variant %d", int(c.Algo))
	case c.Mode&^modeAll != 0:
		return bad("invalid mode %#x", int(c.Mode))
	case c.Pack < 0:
		return bad("invalid number of items to pack %d", c.Pack)
	}
	return nil
}

// isRuleEval reports whether the evaluation measure needs the item set
// tree (RE_NONE < e < RE_FNCNT).
func isRuleEval(eval int) bool { return eval > EvalNone && eval < EvalLogRatio }

// normalize is the parameter normalization of fpg_create, applied in
// upstream's order. pack is the FIM16 field (Config.Pack, clamped to
// 0..31 like the 5-bit field it replaces).
func (c *Config) normalize() {
	switch t := c.Target; {
	case t&report.TargetRules != 0:
		c.Target = report.TargetRules
	case t&report.TargetGenerators != 0:
		c.Target = report.TargetGenerators
	case t&report.TargetMaximal != 0:
		c.Target = report.TargetMaximal
	case t&report.TargetClosed != 0:
		c.Target = report.TargetClosed
	default:
		c.Target = report.TargetAll
	}
	if c.Target != report.TargetRules {
		c.Conf = 100 // neutral confidence
	} else if c.Eval == EvalLogRatio {
		// Deviation from upstream (rules-eval.md B1): fpgrowth maps -eb to
		// its item set measure for every target, so rules are not
		// evaluated at all and the documented rule measure "imbalance
		// ratio" (b) is unreachable. For rules 'b' selects it.
		c.Eval = int(ruleval.Imbalance)
	}
	if c.Algo == AlgoAuto {
		c.Algo = AlgoComplex
	}
	if c.Target == report.TargetGenerators {
		c.Mode |= ModePerfect // generators need perfect extension pruning
		if c.Algo == AlgoTopDown {
			c.Algo = AlgoSingle
		}
	} else if c.Target&(report.TargetClosed|report.TargetMaximal) != 0 {
		c.Mode &^= ModeReorder // reordering breaks the item order
		if c.Algo == AlgoSingle {
			c.Algo = AlgoSimple
		}
	}
	if c.Algo != AlgoComplex {
		c.Mode &^= ModeReorder
	}
	c.Pack = min(c.Pack, 31) // the FPG_FIM16 field has 5 bits
	if c.Algo != AlgoSimple && c.Algo != AlgoComplex && c.Algo != AlgoSingle {
		c.Pack = 0
	}
	if c.Target == report.TargetRules || isRuleEval(c.Eval) {
		c.Pack = 0
	}
	if c.Target != report.TargetMaximal {
		c.Mode &^= ModeTail
	}
}
