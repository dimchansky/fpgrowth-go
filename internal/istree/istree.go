package istree

import (
	"fmt"
	"math"
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/thresh"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// skip is upstream's SKIP flag for integer supports (SUPP_MIN, the sign
// bit). A counter with the flag set compares below every minimum support,
// which the filters rely on; count strips it.
const skip = tract.Supp(math.MinInt64)

// count is upstream's COUNT(): the support without the skip flag.
func count(s tract.Supp) tract.Supp { return s &^ skip }

// isSkip is upstream's IS2SKIP().
func isSkip(s tract.Supp) bool { return s < 0 }

// ErrTooLarge is returned by AddChildren when the tree would need more
// nodes, counters, identifier map entries or child slots than its int32
// indices can address. It wraps tract.ErrTooLarge.
var ErrTooLarge = fmt.Errorf("istree: item set tree exceeds %d entries: %w",
	math.MaxInt32, tract.ErrTooLarge)

// maxArena is the size limit of every arena of a Tree (nodes, cnts, ids,
// chn). It is a variable so that tests can provoke an overflow with small
// data; tests that change it must not run in parallel.
var maxArena = math.MaxInt32

// fits reports whether an arena of length n can grow by k entries without
// exceeding maxArena.
func fits(n, k int) bool { return k <= maxArena-n }

// node is an item set tree node (upstream ISTNODE). Its counters,
// identifier map and child slots live in the tree's shared arenas.
//
// A node represents the item set formed by the items on the path from the
// root (excluded) to the node; with IST_REVERSE the path lists items in
// decreasing code order. Counter i stands for that set extended by the
// item itemAt(i), which is smaller than the node's own item.
type node struct {
	parent int32      // parent node, -1 for the root
	item   tract.Item // item used in the parent node (ITEMOF)
	// hdonly is upstream's HDONLY flag: the path to the node contains a
	// head-only item.
	hdonly bool
	// offset is the item of counter 0 for a pure counter array (>= 0) or
	// -1 if the node uses an identifier map (ids).
	offset int32
	size   int32 // number of counters
	cnts   int32 // index of counter 0 in Tree.cnts
	ids    int32 // index of the identifier map in Tree.ids (offset < 0)
	// chcnt is the number of child slots (CHILDCNT); 0 if there are no
	// children. For a pure array the slots cover the items from chFirst to
	// the last child (absent children are -1); for an identifier map they
	// hold exactly the children in increasing item order.
	chcnt   int32
	chn     int32      // index of child slot 0 in Tree.chn
	chFirst tract.Item // item of the first child (ITEMOF(chn[0]))
}

// Params configures a tree (the arguments of ist_create plus the item
// base data it reads).
type Params struct {
	// Freqs holds the item frequencies by item code; its length is the
	// number of items. They become the counters of the root node and are
	// the head supports of all rules.
	Freqs []tract.Supp
	// Total is the total transaction weight (ib_getwgt), the support of
	// the empty set and the body support of rules with an empty body.
	Total tract.Supp
	// Appear holds the item appearances by item code (only meaningful for
	// rules, -R); nil means AppearBoth for every item.
	Appear []tract.Appearance
	// MinSupp is the minimum support of an item set (fpgrowth's
	// fpg->supp); values below 1 act as 1.
	MinSupp tract.Supp
	// MinBody is the minimum support of a rule body (fpg->body); it is
	// raised to the effective MinSupp.
	MinBody tract.Supp
	// MinConf is the minimum rule confidence as a fraction in [0, 1]. A
	// rule passes under the threshold contract of package thresh
	// (thresh.Frac.Meets): upstream's test supp >= body*(conf*(1-
	// DBL_EPSILON)) in double arithmetic while x = MinConf·body < 2^52,
	// above that supp >= ⌊x⌋ exactly (deviation for differences.md N2),
	// so MinConf 1 admits only rules whose support equals the body support.
	MinConf float64
	// MaxSupp is the maximum rule support (-S) for rules, applied to the
	// body support, or with OrigSupp to the support of body and head
	// (deviation for upstream bug B8). Values <= 0 or report.MaxSupp mean
	// no limit. It is ignored for item sets (the reporter applies -S).
	MaxSupp tract.Supp
	// OrigSupp selects the original rule support definition (-o) for
	// MaxSupp.
	OrigSupp bool
}

// Tree is the item set tree of fpgrowth's rule and evaluation path, a port
// of upstream's ISTREE in mode IST_REVERSE. It is filled by the miner with
// the insertion API (AddChildren, Down, SetSupp, Up), then optionally
// evaluated (SetEval), filtered (Filter, CloMax) and reported (Init,
// Report). A Tree is not safe for concurrent use.
type Tree struct {
	nodes []node
	cnts  []tract.Supp
	ids   []tract.Item
	chn   []int32

	app    []tract.Appearance
	wgt    tract.Supp // total weight, possibly with the skip flag
	smin   tract.Supp
	body   tract.Supp
	conf   thresh.Frac // minimum confidence (Params.MinConf)
	smax   tract.Supp
	isOrig bool

	height int // number of levels
	isLvl  bool
	lvls   [][]int32

	curr  int32 // current node of the insertion cursor
	depth int   // depth of the current node (1 for the root)
	zmax  int

	// evaluation (ist_seteval)
	eval       ruleval.Measure
	fn         ruleval.Func
	afn        ruleval.AbortFunc // abortable fn (non-constant measures), or nil
	agg        Aggregation
	isInvBxs   bool
	dir        float64
	thresh     float64 // dir * threshold
	evNode     int32   // ist->node: node of the set to evaluate
	evIndex    int32   // ist->index: counter of the set (-1: empty set)
	isNoFilter bool    // compute evaluations without filtering (Eval.NoFilter)
	buf        []tract.Item
	cand       []tract.Item
	childList  []int32

	// cancellation (SetAbort)
	abort func() error
	ticks uint32
	err   error // abort error latched by an evaluation (value)
}

// abortEvery is the number of steps (nodes in Filter and CloMax, counters
// in Report) between two calls of the abort function.
const abortEvery = 256

// SetAbort sets a function that the long-running methods Filter, CloMax
// and Report call every abortEvery steps. Evaluations with a measure whose
// cost is not constant (the Fisher tests, about 9 standard deviations of
// the hypergeometric distribution per side, up to √n/4 each)
// call it as well, every few thousand summed tables, so that a single
// evaluation with huge weights can be canceled too. A non-nil result stops the
// method, which returns it; the tree is then in an unspecified state and
// must not be used further. The function should be cheap (for example a
// non-blocking receive on a done channel); nil (the default) disables the
// checks. Between two calls the methods do a bounded amount of work, so at
// most abortEvery counters' worth of item sets or rules are reported after
// the function first returns an error.
func (t *Tree) SetAbort(fn func() error) { t.abort = fn }

// tick counts one step and calls the abort function every abortEvery
// steps.
func (t *Tree) tick() error {
	if t.abort == nil {
		return nil
	}
	if t.err != nil {
		return t.err
	}
	t.ticks++
	if t.ticks%abortEvery != 0 {
		return nil
	}
	return t.abort()
}

// New is a port of ist_create(base, IST_REVERSE, smin, body, conf).
// The minimum supports are normalized like upstream: smin >= 1 and
// body >= smin. It panics if Appear is not nil and has a different length
// than Freqs.
func New(p Params) *Tree {
	n := len(p.Freqs)
	if p.Appear != nil && len(p.Appear) != n {
		panic(fmt.Sprintf("istree: %d appearances for %d items", len(p.Appear), n))
	}
	t := &Tree{
		app:  p.Appear,
		wgt:  p.Total,
		smin: max(p.MinSupp, 1),
		conf: thresh.New(p.MinConf),
		smax: p.MaxSupp,
		// upstream allocates lvls, buf and map with n+1 entries
		lvls:   make([][]int32, 0, n+1),
		buf:    make([]tract.Item, n+2),
		cand:   make([]tract.Item, 0, n+1),
		isOrig: p.OrigSupp,
		height: 1,
		isLvl:  true,
		depth:  1,
		zmax:   report.MaxSize,
	}
	if t.smax <= 0 {
		t.smax = report.MaxSupp
	}
	t.body = max(p.MinBody, t.smin)
	t.cnts = make([]tract.Supp, n, 2*n+16)
	copy(t.cnts, p.Freqs)
	t.nodes = make([]node, 1, 1+n)
	t.nodes[0] = node{parent: -1, size: int32(n)}
	t.lvls = append(t.lvls, []int32{0})
	t.SetEval(Eval{})
	t.Init()
	return t
}

// NumItems is the number of items (ist_itemcnt).
func (t *Tree) NumItems() int { return int(t.nodes[0].size) }

// Height is the number of levels of the tree (ist_height).
func (t *Tree) Height() int { return t.height }

// Depth is the depth of the insertion cursor, 1 at the root (ist_depth).
func (t *Tree) Depth() int { return t.depth }

// SetSize is a port of ist_setsize: the range of item set (and rule) sizes.
// zmax bounds the depth of the tree (Expandable); fpgrowth passes zmax+1
// for closed and maximal item sets and caps it at the size of the largest
// transaction. zmin is accepted for symmetry with upstream, where only the
// size-ordered iterators (ist_iset/ist_rule, not reachable from the
// fpgrowth program) use it; the reporter's size gate applies it.
func (t *Tree) SetSize(zmin, zmax int) {
	_ = zmin
	t.zmax = zmax
}

// Expandable is a port of ist_xable: whether n more items can be added to
// the set of the insertion cursor without exceeding the maximum size.
func (t *Tree) Expandable(n int) bool {
	return int64(t.depth)+int64(n) <= int64(t.zmax)
}

// appear returns the appearance of item i (ib_getapp).
func (t *Tree) appear(i tract.Item) tract.Appearance {
	if t.app == nil {
		return tract.AppearBoth
	}
	return t.app[i]
}

// itemAt is upstream's ITEMAT(n, i).
func (t *Tree) itemAt(nd *node, i int32) tract.Item {
	if nd.offset >= 0 {
		return tract.Item(nd.offset + i)
	}
	return t.ids[nd.ids+i]
}

// counters returns the counter slice of a node.
func (t *Tree) counters(nd *node) []tract.Supp {
	return t.cnts[nd.cnts : nd.cnts+nd.size : nd.cnts+nd.size]
}

// index returns the counter index of item in node nd, or -1 if the node has
// no counter for it (the lookup of ist_setsupp/getsupp).
func (t *Tree) index(nd *node, item tract.Item) int32 {
	if nd.offset >= 0 {
		i := int32(item) - nd.offset
		if i < 0 || i >= nd.size {
			return -1
		}
		return i
	}
	return bsearch(t.ids[nd.ids:nd.ids+nd.size], item)
}

// bsearch is ia_bsearch: the index of id in the ascending array a, or -1.
func bsearch(a []tract.Item, id tract.Item) int32 {
	l, r := 0, len(a)
	for l < r {
		m := int(uint(l+r) >> 1)
		switch {
		case id > a[m]:
			l = m + 1
		case id < a[m]:
			r = m
		default:
			return int32(m)
		}
	}
	return -1
}

// bisect is ia_bisect: the index of id in the ascending array a, or the
// insertion position if it is not contained.
func bisect(a []tract.Item, id tract.Item) int32 {
	l, r := 0, len(a)
	for l < r {
		m := int(uint(l+r) >> 1)
		switch {
		case id > a[m]:
			l = m + 1
		case id < a[m]:
			r = m
		default:
			return int32(m)
		}
	}
	return int32(l)
}

// child returns the child of node nd for item, or -1 (the child lookups of
// ist_down, getsupp, clomax, isets and rules).
func (t *Tree) child(nd *node, item tract.Item) int32 {
	k := nd.chcnt
	if k <= 0 {
		return -1
	}
	if nd.offset >= 0 {
		i := int32(item - nd.chFirst)
		if i < 0 || i >= k {
			return -1
		}
		return t.chn[nd.chn+i]
	}
	// search(): binary search over the items of the children
	chn := t.chn[nd.chn : nd.chn+k]
	l, r := 0, len(chn)
	for l < r {
		m := int(uint(l+r) >> 1)
		x := t.nodes[chn[m]].item
		switch {
		case item > x:
			l = m + 1
		case item < x:
			r = m
		default:
			return chn[m]
		}
	}
	return -1
}

// getsupp is a port of getsupp(): the raw counter (skip flag included) of
// the set formed by the path of node ni and items (in path order), or skip
// if the tree has no counter for it.
func (t *Tree) getsupp(ni int32, items []tract.Item) tract.Supp {
	nd := &t.nodes[ni]
	for ; len(items) > 1; items = items[1:] {
		c := t.child(nd, items[0])
		if c < 0 {
			return skip
		}
		nd = &t.nodes[c]
	}
	i := t.index(nd, items[0])
	if i < 0 {
		return skip
	}
	return t.cnts[nd.cnts+i]
}

// Up is a port of ist_up: it moves the insertion cursor to the parent node
// (it stays at the root).
func (t *Tree) Up() {
	p := t.nodes[t.curr].parent
	if p < 0 {
		return
	}
	t.curr = p
	t.depth--
}

// Down is a port of ist_down: it moves the insertion cursor to the child
// for item and reports whether that child exists.
func (t *Tree) Down(item tract.Item) bool {
	c := t.child(&t.nodes[t.curr], item)
	if c < 0 {
		return false
	}
	t.curr = c
	t.depth++
	return true
}

// SetSupp is a port of ist_setsupp: it sets the support of the set of the
// insertion cursor extended by item and reports whether the current node
// has a counter for item. Like upstream it also overwrites a counter slot
// of a pure array that was not created as a candidate (skip flag).
func (t *Tree) SetSupp(item tract.Item, supp tract.Supp) bool {
	nd := &t.nodes[t.curr]
	i := t.index(nd, item)
	if i < 0 {
		return false
	}
	t.cnts[nd.cnts+i] = supp
	return true
}

// Supp returns the support of the set of the insertion cursor extended by
// item (ist_getsupp), 0 if there is no counter.
func (t *Tree) Supp(item tract.Item) tract.Supp {
	nd := &t.nodes[t.curr]
	i := t.index(nd, item)
	if i < 0 {
		return 0
	}
	return count(t.cnts[nd.cnts+i])
}

// AddChildren is a port of ist_addchn: it creates the children of the node
// of the insertion cursor for all counters whose sets can be extended (see
// child), unless the node has children already. It reports whether
// children were created. It fails only with ErrTooLarge (upstream: out of
// memory); the tree must not be used afterwards.
func (t *Tree) AddChildren() (bool, error) {
	if t.nodes[t.curr].chcnt > 0 {
		return false, nil
	}
	ok, err := t.children(t.curr)
	if err != nil {
		return false, err
	}
	if t.depth+1 > t.height {
		t.height = t.depth + 1
	}
	t.isLvl = false
	return ok, nil
}

// children is a port of children(): it creates the child nodes of node ni
// and its child slot array. It reports whether any child was created. It
// fails only with ErrTooLarge.
func (t *Tree) children(ni int32) (bool, error) {
	list := t.childList[:0]
	size := t.nodes[ni].size
	for i := range size {
		c, err := t.newChild(ni, i)
		if err != nil {
			t.childList = list
			return false, err
		}
		if c >= 0 {
			list = append(list, c)
		}
	}
	t.childList = list
	nd := &t.nodes[ni]
	if len(list) == 0 {
		nd.chcnt = 0 // upstream: ITEM_MIN (CHILDCNT 0)
		return false, nil
	}
	first := t.nodes[list[0]].item
	n := int32(len(list)) // at most size children
	if nd.offset >= 0 {   // pure child array from the first to the last child
		n = int32(t.nodes[list[len(list)-1]].item-first) + 1
	}
	if !fits(len(t.chn), int(n)) {
		return false, ErrTooLarge
	}
	nd.chn = int32(len(t.chn))
	nd.chcnt = n
	nd.chFirst = first
	base := len(t.chn)
	t.chn = append(grow(t.chn, int(n)), make([]int32, n)...)
	slots := t.chn[base:]
	if nd.offset >= 0 {
		for i := range slots {
			slots[i] = -1
		}
		for _, c := range list {
			slots[t.nodes[c].item-first] = c
		}
	} else {
		copy(slots, list)
	}
	for _, c := range list {
		t.nodes[c].parent = ni
	}
	return true, nil
}

// newChild is a port of child() for IST_REVERSE without perfect extension
// pruning: it creates the child node of node ni for counter index (the set
// S = path ∪ {item}) with a counter for every smaller candidate item k for
// which S ∪ {k} may be frequent and may yield a rule, and returns its
// index, or -1 if no candidate qualifies. It fails only with ErrTooLarge.
//
// A candidate k qualifies if neither k nor item is ignored, the set does
// not get two head-only items, S ∪ {k} has every subset with one item less
// stored with at least the minimum support, and at least one of these
// subsets has the minimum body support.
func (t *Tree) newChild(ni int32, index int32) (int32, error) {
	nd := &t.nodes[ni]
	cnts := t.counters(nd)
	supp := cnts[index]
	if supp < t.smin {
		return -1, nil
	}
	item := t.itemAt(nd, index)
	app := t.appear(item)
	if app == tract.AppearNone || (app == tract.AppearHead && nd.hdonly) {
		return -1, nil // do not combine two head-only items
	}
	hdonly := app == tract.AppearHead || nd.hdonly
	body := 0
	if supp >= t.body {
		body = 1
	}
	cand := t.cand[:0]
	end := len(t.buf)
	for i := range index { // IST_REVERSE: candidates have smaller codes
		k := t.itemAt(nd, i)
		ka := t.appear(k)
		if ka == tract.AppearNone || (hdonly && ka == tract.AppearHead) {
			continue // skip sets with two head-only items
		}
		s := cnts[i]
		if s < t.smin {
			continue
		}
		body &= 1
		if s >= t.body {
			body |= 2
		}
		// check the subsets S∪{k} \ {p} for the items p on the path
		pos := end - 2
		t.buf[pos], t.buf[pos+1] = item, k
		ci := ni
		for {
			p := t.nodes[ci].parent
			if p < 0 {
				break
			}
			s = t.getsupp(p, t.buf[pos:end])
			if s < t.smin {
				break
			}
			if s >= t.body {
				body |= 4
			}
			pos--
			t.buf[pos] = t.nodes[ci].item
			ci = p
		}
		if t.nodes[ci].parent < 0 && body != 0 {
			cand = append(cand, k)
		}
	}
	t.cand = cand
	m := int32(len(cand)) // candidates are distinct items: m <= MaxInt32
	if m <= 0 {
		return -1, nil
	}
	// decide on the node structure: identifier map if the candidates fill
	// less than half of their item range
	n := int32(cand[m-1]-cand[0]) + 1
	isMap := int64(m)+int64(m) < int64(n)
	size := n
	if isMap {
		size = m
	}
	if !fits(len(t.nodes), 1) || !fits(len(t.cnts), int(size)) ||
		(isMap && !fits(len(t.ids), int(m))) {
		return -1, ErrTooLarge
	}
	c := node{parent: ni, item: item, hdonly: hdonly, cnts: int32(len(t.cnts))}
	if isMap {
		c.offset = -1
		c.size = m
		c.ids = int32(len(t.ids))
		t.cnts = append(grow(t.cnts, int(m)), make([]tract.Supp, m)...)
		t.ids = append(grow(t.ids, int(m)), cand...)
	} else {
		c.offset = int32(cand[0])
		c.size = n
		base := len(t.cnts)
		t.cnts = append(grow(t.cnts, int(n)), make([]tract.Supp, n)...)
		cs := t.cnts[base:]
		for i := range cs {
			cs[i] = skip
		}
		for _, k := range cand {
			cs[int32(k)-c.offset] = 0
		}
	}
	t.nodes = append(grow(t.nodes, 1), c)
	return int32(len(t.nodes) - 1), nil
}

// grow returns s with room for n more elements, doubling the capacity
// when it must grow. The arenas of a Tree only grow, to millions of
// entries on large rule runs, and append's growth by a quarter for large
// slices copies every entry several times (the sparse rules benchmark
// with lift allocated 114 MB per run, 49 MB with doubling).
func grow[E any](s []E, n int) []E {
	if cap(s)-len(s) >= n {
		return s
	}
	return slices.Grow(s, max(n, len(s)))
}

// makelvls is a port of makelvls()/reclvls(): it rebuilds the per-level
// node lists. Upstream prepends each node to its level list during a
// depth-first traversal, so a level lists its nodes in reverse depth-first
// order; this order is kept (although nothing depends on it, see Filter).
func (t *Tree) makelvls() {
	for len(t.lvls) < t.height {
		t.lvls = append(t.lvls, nil)
	}
	for h := range t.lvls {
		t.lvls[h] = t.lvls[h][:0]
	}
	t.reclvls(0, 0)
	for _, l := range t.lvls {
		for i, j := 0, len(l)-1; i < j; i, j = i+1, j-1 {
			l[i], l[j] = l[j], l[i]
		}
	}
	t.isLvl = true
}

func (t *Tree) reclvls(ni int32, lvl int) {
	t.lvls[lvl] = append(t.lvls[lvl], ni)
	nd := &t.nodes[ni]
	for _, c := range t.chn[nd.chn : nd.chn+nd.chcnt] {
		if c >= 0 {
			t.reclvls(c, lvl+1)
		}
	}
}
