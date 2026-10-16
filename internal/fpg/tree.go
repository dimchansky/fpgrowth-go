package fpg

import (
	"context"
	"fmt"

	"github.com/dimchansky/fpgrowth-go/internal/istree"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Phase identifies a step of the rules path of Miner.Mine (association
// rules and item sets with a rule-based evaluation), for progress
// messages. The fpgrowth program logs each phase like upstream's fpg_mine.
type Phase int

// Phases of the rules path, in execution order.
const (
	// PhaseTree fills the item set tree ("finding frequent item set(s)").
	PhaseTree Phase = iota + 1
	// PhaseFilter prunes with the evaluation measure (-p, "filtering with
	// evaluation"); only with a measure and a prune setting.
	PhaseFilter
	// PhaseCloMax filters closed, maximal or generator item sets
	// ("filtering for ... item sets"); only for these targets.
	PhaseCloMax
	// PhaseReport reports the item sets or rules to the reporter
	// ("writing ...").
	PhaseReport
)

// String returns the name of the phase.
func (p Phase) String() string {
	switch p {
	case PhaseTree:
		return "tree"
	case PhaseFilter:
		return "filter"
	case PhaseCloMax:
		return "clomax"
	case PhaseReport:
		return "report"
	default:
		return fmt.Sprintf("Phase(%d)", int(p))
	}
}

// UsesTree reports whether Mine takes the rules path (fpg_tree and the
// item set tree) for the normalized configuration: association rules, or
// item sets with a rule-based evaluation measure.
func (m *Miner[T]) UsesTree() bool {
	return m.cfg.Target == report.TargetRules || isRuleEval(m.cfg.Eval)
}

// SetProgress sets a function that Mine calls at the start (done false)
// and at the end (done true) of every phase of the rules path. The item
// set path has a single phase (the whole of Mine) and makes no calls.
func (m *Miner[T]) SetProgress(fn func(p Phase, done bool)) { m.progress = fn }

func (m *Miner[T]) step(p Phase, done bool) {
	if m.progress != nil {
		m.progress(p, done)
	}
}

// mineTree is the rules path of fpg_mine: it fills an item set tree with
// fpg_tree/rec_tree, then evaluates, filters and reports it.
//
// Deviation from upstream (rules-eval.md B2): upstream filters with the
// evaluation (ist_filter) before setting the evaluation measure
// (ist_seteval), so -p only removes infrequent sets, and filters only for
// prune <= 0. Here the measure is set first and every prune value above
// NoPrune filters (positive values strong forward), as istree documents;
// the generator filter then needs istree's fix for the total weight
// (R10). Rules are not affected by the filter.
func (m *Miner[T]) mineTree(ctx context.Context) error {
	c := &m.cfg
	n := m.ib.Len()
	freqs := make([]tract.Supp, n)
	appear := make([]tract.Appearance, n)
	for i := range n {
		freqs[i] = m.ib.Frequency(tract.Item(i))
		appear[i] = m.ib.Appearance(tract.Item(i))
	}
	isRules := c.Target == report.TargetRules
	var maxSupp tract.Supp // ignored for item sets (the reporter applies -S)
	if isRules {
		maxSupp = m.th.smax
	}
	m.step(PhaseTree, false)
	ist := istree.New(istree.Params{
		Freqs:    freqs,
		Total:    m.bag.TotalWeight(),
		Appear:   appear,
		MinSupp:  m.th.supp,
		MinBody:  m.th.body,
		MinConf:  c.Conf / 100.0,
		MaxSupp:  maxSupp,
		OrigSupp: c.Mode&ModeOrigSupp != 0,
	})
	x := c.ZMax // closed/maximal sets need a superset level
	if c.Target&(report.TargetClosed|report.TargetMaximal) != 0 && x < MaxSize {
		x++
	}
	ist.SetSize(c.ZMin, min(x, m.bag.MaxSize()))
	tm := &miner{
		ctx:    ctx,
		done:   ctx.Done(),
		rep:    m.rep,
		bag:    m.bag,
		supp:   m.th.supp,
		target: c.Target,
		mode:   c.Mode,
		dir:    1,
	}
	if err := tm.fillTree(ist); err != nil {
		return err
	}
	m.step(PhaseTree, true)

	measure := ruleval.None
	if isRuleEval(c.Eval) {
		measure = ruleval.Measure(c.Eval)
	}
	ist.SetEval(istree.Eval{
		Measure:    measure,
		Agg:        istree.Aggregation(c.Agg),
		Invalidate: c.InvalidateBelowExpected,
		Threshold:  c.Thresh / 100.0,
	})
	prune := measure != ruleval.None && c.Prune > NoPrune
	if prune {
		m.step(PhaseFilter, false)
		ist.Filter(c.Prune)
		m.step(PhaseFilter, true)
	}
	if c.Target&(report.TargetClosed|report.TargetMaximal|report.TargetGenerators) != 0 {
		m.step(PhaseCloMax, false)
		ist.CloMax(c.Target, prune)
		m.step(PhaseCloMax, true)
	}
	if err := tm.aborted(); err != nil {
		return err
	}
	m.step(PhaseReport, false)
	ist.Init()
	// istree treats a maximum support of 0 as "no limit"; a rule support
	// is at least 1, so with an absolute maximum below 1 nothing passes.
	if !isRules || maxSupp > 0 {
		if err := ist.Report(m.rep, c.Target); err != nil {
			return err
		}
	}
	m.step(PhaseReport, true)
	return nil
}

// fillTree is a port of fpg_tree: it builds a simple FP-tree of the
// frequent items (no perfect extension pruning, no 16-items machine) and
// fills the item set tree ist with rec_tree.
func (m *miner) fillTree(ist *istree.Tree) error {
	k := m.bag.NumItems()
	if k <= 0 {
		return nil
	}
	f := m.bag.Frequencies()
	d := make([]int32, k) // old -> new item map
	s := make([]int32, 0, k)
	for i := range k {
		if f[i] < m.supp {
			d[i] = -1
			continue
		}
		d[i] = int32(len(s))
		s = append(s, int32(i))
	}
	if len(s) == 0 {
		return nil
	}
	t := &fpTree{cnt: len(s), dir: m.dir, heads: make([]fpHead, len(s))}
	m.fp.nodes = make([]fpNode, 0, presize(m.bag.Extent()+1))
	t.root = m.fp.alloc(fpNode{id: tract.End, parent: nilIdx, succ: nilIdx})
	for i, item := range s {
		t.heads[i] = fpHead{item: tract.Item(item), supp: f[item], list: nilIdx}
	}
	buf := make([]int32, k)
	for items, w := range m.bag.Backward() {
		n := 0
		for _, p := range items {
			// packed entries cannot occur: the rules path never packs
			if x := d[p]; x >= 0 {
				buf[n] = x
				n++
			}
		}
		m.addSimple(t, buf[:n], w)
	}
	return m.recTree(t, ist, t.cnt)
}

// recTree is a port of rec_tree: it processes heads 1..n-1 of the single
// tree t in place like rec_single and stores the support of every
// frequent extension in the item set tree, walking it in parallel. Head 0
// is never the current item: the support of a single item comes from the
// root of the item set tree.
func (m *miner) recTree(t *fpTree, ist *istree.Tree, n int) error {
	if err := m.aborted(); err != nil {
		return err
	}
	if !ist.Expandable(1) {
		return nil // no item can be added
	}
	if _, err := ist.AddChildren(); err != nil {
		return err // istree.ErrTooLarge, converted by Miner.Mine
	}
	for i := 1; i < n; i++ {
		if t.heads[i].supp < m.supp {
			continue // skip infrequent items
		}
		if !ist.Down(t.heads[i].item) {
			continue
		}
		m.relink(t, i)
		cnt := 0
		for k := range i {
			h := &t.heads[k]
			if h.supp < m.supp {
				continue
			}
			ist.SetSupp(h.item, h.supp)
			cnt++
		}
		if cnt > 0 {
			if err := m.recTree(t, ist, i); err != nil {
				return err
			}
		}
		ist.Up()
	}
	return nil
}

// relink links heads 0..i-1 of t to the nodes of the conditional database
// of head i and sums their supports (the projection step shared by
// rec_single and rec_tree).
func (m *miner) relink(t *fpTree, i int) {
	nodes := m.fp.nodes
	for k := range i { // clear item heads and support
		t.heads[k].supp = 0
		t.heads[k].list = nilIdx
	}
	for node := t.heads[i].list; node >= 0; node = nodes[node].succ {
		w := nodes[node].supp
		anc := nodes[node].parent
		for ; nodes[anc].id > tract.End; anc = nodes[anc].parent {
			h := &t.heads[nodes[anc].id]
			if h.list == anc {
				break // rest of the path already linked
			}
			nodes[anc].supp = w
			h.supp += w
			nodes[anc].succ = h.list
			h.list = anc
		}
		for ; nodes[anc].id > tract.End; anc = nodes[anc].parent {
			t.heads[nodes[anc].id].supp += w
			nodes[anc].supp += w
		}
	}
}
