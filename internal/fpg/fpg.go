package fpg

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/istree"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Miner drives one FP-growth run, mirroring upstream's life cycle:
//
//	m, err := fpg.New[string](cfg)     // fpg_create
//	err = ib.Count(scan)               // tbg_read (pass 1, by the caller)
//	err = m.Data(ib, scan)             // fpg_data
//	rep, err := m.Reporter()           // isr_create + fpg_report
//	rep.SetBorder(...); rep.AddSpectrum(); rep.SetSink(sink) // optional
//	err = m.Mine(ctx)                  // isr_setup + fpg_mine
//
// A Miner is used for one run and is not safe for concurrent use.
type Miner[T comparable] struct {
	cfg   Config
	ib    *tract.ItemBase[T]
	bag   *tract.Bag
	th    thresholds
	rep   *report.Reporter
	state int
	// progress, if set, is called at the start and end of the phases of
	// the rules path (SetProgress).
	progress func(p Phase, done bool)
}

// Miner states.
const (
	stateNew = iota
	stateData
	stateReporter
	stateMined
)

// New validates cfg and returns a miner with the parameters normalized
// like fpg_create does (target collapsed to one type, forced and cleared
// mode flags, variant substitutions).
func New[T comparable](cfg Config) (*Miner[T], error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.normalize()
	return &Miner[T]{cfg: cfg}, nil
}

// Config returns the normalized configuration.
func (m *Miner[T]) Config() Config { return m.cfg }

// Data is a port of fpg_data (data mode 0, the only one the fpgrowth
// program uses): it converts the support thresholds with the total weight
// of the counted item base ib, recodes the items (Recode, with the sort
// direction reduced to its sign under ModeReorder), builds the transaction
// bag from a second pass over scan with the zmin transaction filter, and
// packs the items of the 16-items machine. ib must have been counted
// (ItemBase.Count) with the same scan. It returns ErrNoItems if no item is
// frequent.
func (m *Miner[T]) Data(ib *tract.ItemBase[T], scan tract.ScanFunc[T]) error {
	if m.state != stateNew {
		return ErrState
	}
	c := &m.cfg
	m.th = computeThresholds(c, ib.TotalWeight())
	pack := min(c.Pack, 16) // pack at most 16 items
	if c.Algo == AlgoComplex {
		pack = 0 // complex trees pack in the recursion
	}
	sort := c.Sort
	if c.Mode&ModeReorder != 0 { // simplified sorting if reordering
		sort = max(-1, min(sort, 1))
	}
	if ib.Recode(m.th.supp, sort) < 1 {
		return ErrNoItems
	}
	zmin := 0 // no filter for rules and rule-based evaluation
	if c.Target != report.TargetRules && !isRuleEval(c.Eval) {
		zmin = c.ZMin
	}
	if c.Target == report.TargetGenerators {
		// Deviation from upstream (fpgrowth-core.md B3): emptying the
		// transactions with fewer than zmin items deflates the supports
		// of the subsets a generator is compared with, so upstream misses
		// generators with -m >= 2. Generators are mined unfiltered.
		zmin = 0
	}
	bag, err := ib.BuildBag(scan, zmin)
	if err != nil {
		return fmt.Errorf("fpg: %w", err)
	}
	if pack > 0 {
		bag.Pack(pack)
	}
	m.ib, m.bag = ib, bag
	m.state = stateData
	return nil
}

// Bag returns the transaction bag built by Data (nil before).
func (m *Miner[T]) Bag() *tract.Bag { return m.bag }

// MinSupport returns the absolute minimum support of an item set derived
// by Data (at least 1).
func (m *Miner[T]) MinSupport() tract.Supp { return m.th.supp }

// BodySupport returns the absolute minimum support of a rule body
// derived by Data (at least 1).
func (m *Miner[T]) BodySupport() tract.Supp { return m.th.body }

// MaxSupport returns the absolute maximum support passed to the reporter.
func (m *Miner[T]) MaxSupport() tract.Supp { return m.th.smax }

// Reporter creates the item set reporter for the recoded items (isr_create)
// and configures it like fpg_report: support range [MinSupport,
// MaxSupport], size range [ZMin, ZMax], the -eb evaluation (LogRatio with
// threshold Thresh/100), and the target with ModeSort for reordered
// generators, ModeNoFilter for the rules path, and repository order -1.
// The caller may then set a border, a pattern spectrum and the sink.
// Pre-formatting of supports (isr_prefmt) is part of text output.
func (m *Miner[T]) Reporter() (*report.Reporter, error) {
	if m.state != stateData {
		return nil, ErrState
	}
	c := &m.cfg
	n := m.ib.Len()
	freqs := make([]tract.Supp, n)
	for i := range freqs {
		freqs[i] = m.ib.Frequency(tract.Item(i))
	}
	rep := report.New(n, m.bag.TotalWeight(), freqs)
	var mrep report.Mode
	if c.Target == report.TargetGenerators && c.Mode&ModeReorder != 0 {
		mrep |= report.ModeSort // reordering requires set sorting
	}
	if c.Target == report.TargetRules || isRuleEval(c.Eval) {
		mrep |= report.ModeNoFilter // filtering done by the item set tree
	}
	smax := m.th.smax
	if c.Target == report.TargetRules {
		// The item set tree applies the maximum support to the rule
		// support itself (deviation for rules-eval.md B8), so the
		// reporter must not limit the support of body and head.
		smax = report.MaxSupp
	}
	rep.SetSupport(m.th.supp, smax)
	rep.SetSize(c.ZMin, c.ZMax)
	if c.Eval == EvalLogRatio {
		thresh := c.Thresh / 100.0
		if c.EvalNoFilter {
			thresh = math.Inf(-1) // every value passes (also NaN)
		}
		rep.SetEval(report.LogRatio, +1, thresh)
	}
	rep.SetTarget(c.Target, mrep, -1)
	m.rep = rep
	m.state = stateReporter
	return rep, nil
}

// Mine is a port of fpg_mine: it finalizes the reporter (Setup) and runs
// the selected variant, which reports the item sets to the reporter's
// sink. Association rules and item sets with a rule-based evaluation go
// through the item set tree (fpg_tree, see mineTree). Cancellation of ctx
// is checked at every recursion entry, every few thousand transactions
// while the item set tree's FP-tree is built, and every few hundred nodes
// or counters in the item set tree's filter and report phases; it returns
// ctx.Err(). Results already found between two checks may still be
// reported after the cancellation. An error of
// the sink is returned unchanged; a tree that outgrows its node arena, the
// item set tree or the closed/maximal repository of the reporter gives
// ErrTooLarge.
func (m *Miner[T]) Mine(ctx context.Context) (err error) {
	if m.state != stateReporter {
		return ErrState
	}
	m.state = stateMined
	defer recoverOverflow(&err)
	m.rep.Setup()
	c := &m.cfg
	if c.Target == report.TargetRules || isRuleEval(c.Eval) {
		err = m.mineTree(ctx)
	} else {
		err = mineItemSets(ctx, c, m.bag, m.rep, m.th.supp)
	}
	if err == nil {
		// a repository overflow in Add or Tail that no later Report
		// returned (e.g. in the last subtree)
		err = m.rep.Err()
	}
	return tooLarge(err)
}

// tooLarge maps the capacity errors of the reporter's closed/maximal
// repository and of the item set tree to ErrTooLarge, the error of the
// miner's own node arenas; other errors (sink, cancellation) are returned
// unchanged.
func tooLarge(err error) error {
	if errors.Is(err, report.ErrTooLarge) || errors.Is(err, istree.ErrTooLarge) {
		return ErrTooLarge
	}
	return err
}

// Mine runs the whole pipeline on an item base that is configured
// (selection, appearances) but not yet counted: ItemBase.Count, Data,
// Reporter, and Mine with sink as the reporter's sink (nil only counts).
// It returns the reporter for its statistics.
func Mine[T comparable](
	ctx context.Context,
	cfg Config,
	ib *tract.ItemBase[T],
	scan tract.ScanFunc[T],
	sink report.Sink,
) (*report.Reporter, error) {
	m, err := New[T](cfg)
	if err != nil {
		return nil, err
	}
	if err := ib.Count(scan); err != nil {
		return nil, fmt.Errorf("fpg: %w", err)
	}
	if err := m.Data(ib, scan); err != nil {
		return nil, err
	}
	rep, err := m.Reporter()
	if err != nil {
		return nil, err
	}
	rep.SetSink(sink)
	if err := m.Mine(ctx); err != nil {
		return rep, err
	}
	return rep, nil
}
