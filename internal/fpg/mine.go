package fpg

import (
	"context"
	"math"
	"math/bits"

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
	// left is, while a node's set is reported, an upper bound of the
	// sibling nodes that follow it in its loop (-1: unknown). The
	// maximal check uses it to decide whether sibling rows pay off
	// (extsib.go); it never changes a result.
	left int
	// limited: closed/maximal search limited by the maximum size zmax,
	// with the complete extension check (see xable2).
	limited bool
	zmax    int

	// Scratch buffers (upstream fpg->set, fpg->map, fpg->cis).
	set   []int32      // reorder map and flags (2k)
	imap  []int32      // old -> new item map of a projection (k)
	cis   []tract.Supp // conditional item supports (k)
	path  []int32      // projected transaction, filled backwards (k)
	bit16 []uint16     // 16-items machine bit of each item of a projection (k)
	tail  []tract.Item // head union tail items, filled backwards (k+16)

	fim   *machine  // 16-items machine (complex variant)
	paths projPaths // paths of the current projection

	fp fpArena // nodes of the simple and single variants
	cs csArena // nodes of the complex variant
	td tdArena // nodes of the top-down variant

	fpHeads [][]fpHead     // per recursion depth (projection headers)
	csHeads [][]csHead     // per recursion depth
	tdItems [][]tract.Item // per recursion depth
}

// testHooks are settings of a Miner for tests only.
type testHooks struct {
	// cloMaxCheck, if set, replaces the complete extension check of a
	// size-limited closed/maximal search.
	cloMaxCheck func(*extChecker, *report.Reporter) report.CloMaxCheck
	// rows selects the form of the extension checker's bit sets
	// (rowsAuto by default).
	rows rowMode
	// sib selects when the maximal check builds sibling rows (sibAuto by
	// default) and whether it verifies them.
	sib sibHook
	// checker, if set, is called with the run's extension checker.
	checker func(*extChecker)
}

// mineItemSets runs the item set variant selected by c (fpgvars[algo]).
func mineItemSets(ctx context.Context, c *Config, bag *tract.Bag,
	rep *report.Reporter, supp tract.Supp, h testHooks,
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
		left:   -1,
	}
	if m.clomax && c.ZMax < bag.MaxSize() {
		// A maximum size below the longest transaction: limited search
		// with the complete extension check (see xable2).
		chk, err := newExtChecker(bag, c.Target, supp, h.rows, m.aborted)
		if err != nil {
			return err
		}
		chk.sib, chk.zmax = h.sib, c.ZMax
		if h.checker != nil {
			h.checker(chk)
		}
		m.limited, m.zmax = true, c.ZMax
		// chk.left: the siblings that can follow the set (cost only)
		fn := func(set []tract.Item, supp tract.Supp) bool {
			chk.left = m.left
			return chk.checkPath(rep.Items(), set, supp)
		}
		if h.cloMaxCheck != nil {
			hook := h.cloMaxCheck(chk, rep)
			fn = func(set []tract.Item, supp tract.Supp) bool {
				chk.left = m.left
				return hook(set, supp)
			}
		}
		rep.SetCloMaxCheck(fn)
	}
	if m.done != nil {
		// Recursion entries alone do not bound the work after a
		// cancellation: a perfect-extension hypercube or a burst of the
		// 16-items machine reports up to 2^k sets without returning to
		// one. The reporter checks every report.AbortEvery steps and
		// latches the error, so every recursion unwinds.
		rep.SetAbort(m.aborted)
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

// later returns the number of loop entries after index i of a loop over
// cnt entries in direction dir.
func later(i, cnt, dir int) int {
	if dir < 0 {
		return i
	}
	return cnt - 1 - i
}

// packedCount returns the number of items of a packed head item (0 for
// an ordinary item).
func packedCount(it tract.Item) int {
	if it >= 0 {
		return 0
	}
	return bits.OnesCount32(uint32(it) & 0xffff)
}

// buildCheckEvery is the number of transactions between two cancellation
// checks while a top-level FP-tree is built from the bag (all item set
// variants and fillTree of the item set tree path).
const buildCheckEvery = 4096

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
	m.bit16 = make([]uint16, k)
	m.tail = make([]tract.Item, k+16)
}

// xable2 is the projection condition isr_xable(rep, 2) of the rec_*
// functions: whether the items of a projection can be added to the
// current set.
//
// Deviation from upstream (fpgrowth-core.md B5, docs/m5-investigation.md):
// for closed and maximal item sets with a maximum size, upstream projects
// up to zmax+1 items and lets the closed/maximal repository decide. The
// repository then misses the supersets beyond the limit that prove a set
// non-closed or non-maximal, so such sets are reported. Here the search
// stops where no reportable set can follow (the current set, its perfect
// extensions and one more item exceed zmax, see deeper), and every set
// the repository accepts is checked against all items of the database
// (extChecker). Without a maximum size below the longest transaction the
// search is unlimited and the repository alone is exact.
func (m *miner) xable2() bool {
	if m.limited {
		return m.rep.Len()+m.rep.PexCount()+2 <= m.zmax
	}
	return m.clomax || m.rep.Xable(2)
}

// deeper reports whether the set just added needs its projection (its
// perfect extensions and subtree): for a size-limited closed/maximal
// search only if the set with its perfect extensions so far is smaller
// than zmax. Otherwise every set of the subtree exceeds zmax, and the set
// itself is reportable only if it has no further perfect extension, which
// the extension check verifies.
func (m *miner) deeper() bool {
	return !m.limited || m.rep.Len()+m.rep.PexCount() < m.zmax
}

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
