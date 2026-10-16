package fpg

import (
	"context"
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// suppMax is upstream's SUPP_MAX, the perfect extension threshold that
// disables perfect extension pruning (SUPP is widened to 64 bits).
const suppMax = tract.Supp(math.MaxInt64)

// nilIdx marks a missing node (upstream NULL pointer).
const nilIdx int32 = -1

// miner holds the state of one item set mining run (the mining part of
// upstream's struct _fpgrowth).
type miner struct {
	ctx    context.Context
	done   <-chan struct{}
	rep    *report.Reporter
	bag    *tract.Bag
	supp   tract.Supp    // absolute minimum support
	target report.Target // normalized target
	mode   Mode
	pack   int  // FIM16 field (number of items for the 16-items machine)
	dir    int  // item loop direction (fpg->dir)
	clomax bool // closed or maximal target

	// Scratch buffers (upstream fpg->set, fpg->map, fpg->cis).
	set  []int32      // reorder map and flags (2k)
	imap []int32      // old -> new item map of a projection (k)
	cis  []tract.Supp // conditional item supports (k)
	path []int32      // projected transaction, filled backwards (k)
	tail []tract.Item // head union tail items, filled backwards (k+16)

	fim *machine // 16-items machine (complex variant)

	fp fpArena // nodes of the simple and single variants
	cs csArena // nodes of the complex variant
	td tdArena // nodes of the top-down variant

	fpHeads [][]fpHead     // per recursion depth (projection headers)
	csHeads [][]csHead     // per recursion depth
	tdItems [][]tract.Item // per recursion depth
}

// mineItemSets runs the item set variant selected by c (fpgvars[algo]).
func mineItemSets(ctx context.Context, c *Config, bag *tract.Bag,
	rep *report.Reporter, supp tract.Supp,
) error {
	m := &miner{
		ctx:    ctx,
		done:   ctx.Done(),
		rep:    rep,
		bag:    bag,
		supp:   supp,
		target: c.Target,
		mode:   c.Mode,
		pack:   c.Pack,
		dir:    1,
		clomax: c.Target == report.TargetClosed || c.Target == report.TargetMaximal,
	}
	switch c.Algo {
	case AlgoSimple:
		return m.mineSimple()
	case AlgoSingle:
		return m.mineSingle()
	case AlgoTopDown:
		return m.mineTopDown()
	default:
		return m.mineComplex()
	}
}

// aborted is the cancellation check at recursion entry (upstream
// sig_aborted with FPG_ABORT): a non-blocking receive on ctx.Done().
func (m *miner) aborted() error {
	select {
	case <-m.done:
		return m.ctx.Err()
	default:
		return nil
	}
}

// alloc allocates the scratch buffers for k items.
func (m *miner) alloc(k int) {
	m.set = make([]int32, 2*k)
	m.imap = make([]int32, k)
	m.cis = make([]tract.Supp, k)
	m.path = make([]int32, k)
	m.tail = make([]tract.Item, k+16)
}

// xable2 is the projection condition isr_xable(rep, 2) of the rec_*
// functions.
//
// Deviation from upstream (fpgrowth-core.md B5): for closed and maximal
// item sets with a maximum size, upstream does not project beyond
// zmax+1 items. The closed/maximal repository then misses the supersets
// below the size limit, while isr_add still prunes subtrees with a stored
// superset of equal support on the assumption that the superset's subtree
// was searched completely, so non-closed and non-maximal sets are
// reported (and collecting the perfect extensions at the size limit is not
// enough: a closed set beyond the limit can be the only witness).
// Closed and maximal targets are therefore searched without a depth
// limit, like the 16-items machine does anyway; the reporter's size gate
// still applies to what is reported.
func (m *miner) xable2() bool { return m.clomax || m.rep.Xable(2) }

// perfectThreshold returns the support from which an item is a perfect
// extension: supp if perfect extension pruning is on, else SUPP_MAX.
func (m *miner) perfectThreshold(supp tract.Supp) tract.Supp {
	if m.mode&ModePerfect != 0 {
		return supp
	}
	return suppMax
}

// topLevel is the common start of every fpg_* variant: the early exit for
// a minimum support above the total weight, the empty database, and the
// item map of the initial tree. With keepPacked, item 0 is forced to be
// head 0 (the packed head of the 16-items machine). It returns the item
// frequencies, the old -> new map d and the new -> old map s (sliced to
// the number of kept items) and whether mining must continue; if not, err
// is the result of the variant.
func (m *miner) topLevel(keepPacked bool) (d, s []int32, f []tract.Supp, cont bool, err error) {
	pex := m.bag.TotalWeight()
	if m.supp > pex { // check against the minimum support
		return nil, nil, nil, false, nil
	}
	if m.mode&ModePerfect == 0 {
		pex = suppMax
	}
	k := m.bag.NumItems()
	if k <= 0 {
		return nil, nil, nil, false, m.rep.Report()
	}
	f = m.bag.Frequencies()
	m.alloc(k)
	d = make([]int32, k)
	s = make([]int32, k)
	n := 0
	if keepPacked { // always keep packed items
		d[0], s[0] = 0, 0
		n = 1
	}
	for i := n; i < k; i++ {
		if f[i] < m.supp {
			d[i] = -1
			continue
		}
		if f[i] >= pex {
			m.rep.AddPex(tract.Item(i))
			d[i] = -1
			continue
		}
		d[i] = int32(n)
		s[n] = int32(i)
		n++
	}
	if n <= 0 { // no items left
		return nil, nil, nil, false, m.rep.Report()
	}
	if m.target == report.TargetClosed || m.target == report.TargetMaximal {
		m.dir = -1
	}
	return d, s[:n], f, true, nil
}

// tailBuf collects the tail items of head union tail pruning backwards
// (upstream writes them downwards from fpg->map), so that items pushed in
// ascending code order end up in descending (repository) order.
type tailBuf struct {
	buf []tract.Item
	pos int
}

func (m *miner) tailBuf() tailBuf { return tailBuf{buf: m.tail, pos: len(m.tail)} }

func (t *tailBuf) push(i tract.Item) {
	t.pos--
	t.buf[t.pos] = i
}

func (t *tailBuf) items() []tract.Item { return t.buf[t.pos:] }
