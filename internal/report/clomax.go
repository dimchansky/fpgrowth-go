package report

import (
	"fmt"
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// This file ports clomax.c, the repository of already reported closed or
// maximal item sets. See docs/upstream/tract.md §6 for the contract.
//
// The repository is a stack of prefix trees trees[0..cnt] that mirrors the
// reporter's current prefix p1 … pcnt (items added in the repository's item
// order, which is descending for fpgrowth). trees[k] holds the stored sets
// that contain p1 … pk, with those items removed; trees[k].item is p(k+1).
// Every node carries the maximum support of the stored sets through it.
//
// Upstream allocates nodes from one MEMSYS per tree; here every tree owns a
// node arena (a slice addressed by int32 indices, nodes[0] being the root)
// with a free list, so clearing a tree is O(1) and keeps its capacity.
//
// Node indices are int32, so a tree holds at most maxRepoNodes nodes. An
// allocation beyond that limit (upstream: memory exhaustion of the MEMSYS)
// fails with ErrTooLarge, which every allocating function returns; the
// repository is unusable afterwards and the reporter latches the error.

// ErrTooLarge is returned (by Reporter.Report and Reporter.Err) when a
// tree of the closed/maximal repository would need more nodes than its
// int32 indices can address. It wraps tract.ErrTooLarge.
var ErrTooLarge = fmt.Errorf("report: closed/maximal repository exceeds %d nodes: %w",
	math.MaxInt32, tract.ErrTooLarge)

// maxRepoNodes is the node limit of every repository tree. It is a
// variable so that tests can provoke an overflow with small data; tests
// that change it must not run in parallel.
var maxRepoNodes = math.MaxInt32

const (
	nilNode int32 = -1 // null node index

	cmInvalid tract.Item = -2 // cmTree.item: tree is empty/invalid
	cmValid   tract.Item = -1 // cmTree.item: valid tree, no prefix item yet

	cmNone  tract.Supp = -1 // cmTree.max: no stored superset of the prefix
	cmEmpty tract.Supp = -2 // cmTree.max: tree is empty/invalid
)

// cmNode is a port of CMNODE.
type cmNode struct {
	item     tract.Item
	sibling  int32
	children int32
	supp     tract.Supp
}

// cmTree is a port of CMTREE: a prefix tree of item sets whose sibling
// lists are sorted in the repository's item order.
type cmTree struct {
	nodes []cmNode // node arena; nodes[0] is the root (empty set)
	free  int32    // head of the free list, linked through sibling
	desc  bool     // item order: descending (upstream dir < 0)
	item  tract.Item
	max   tract.Supp
}

// newCMTree is a port of cmt_create.
func newCMTree(desc bool) *cmTree {
	t := &cmTree{desc: desc}
	t.clear()
	return t
}

// clear is a port of cmt_clear: it removes all nodes and marks the tree
// invalid.
func (t *cmTree) clear() {
	t.nodes = append(t.nodes[:0], cmNode{
		item:     cmValid,
		sibling:  nilNode,
		children: nilNode,
	})
	t.free = nilNode
	t.item = cmInvalid
	t.max = cmEmpty
}

// valid is a port of the cmt_valid macro.
func (t *cmTree) valid() bool { return t.item >= cmValid }

// before reports whether item a precedes item b in the tree's item order
// (upstream macros pos and neg).
func (t *cmTree) before(a, b tract.Item) bool {
	if t.desc {
		return a > b
	}
	return a < b
}

// alloc returns the index of an unused node (ms_alloc), or ErrTooLarge if
// the tree already has maxRepoNodes nodes and none is free. The node's
// fields are unspecified. alloc may reallocate t.nodes.
func (t *cmTree) alloc() (int32, error) {
	if n := t.free; n >= 0 {
		t.free = t.nodes[n].sibling
		return n, nil
	}
	if len(t.nodes) >= maxRepoNodes {
		return nilNode, ErrTooLarge
	}
	t.nodes = append(t.nodes, cmNode{})
	return int32(len(t.nodes) - 1), nil // len <= maxRepoNodes <= MaxInt32
}

// release returns node n to the free list (ms_free).
func (t *cmTree) release(n int32) {
	t.nodes[n].sibling = t.free
	t.free = n
}

// add is a port of cmt_add: it adds the item set items (in tree order) with
// support supp, raising the support of every node on its path and the
// tree's maximum to at least supp. It fails only with ErrTooLarge.
func (t *cmTree) add(items []tract.Item, supp tract.Supp) error {
	if supp > t.max {
		t.max = supp
	}
	node := int32(0)
	for k, it := range items {
		if supp > t.nodes[node].supp {
			t.nodes[node].supp = supp
		}
		prev, c := nilNode, t.nodes[node].children
		for c >= 0 && t.before(t.nodes[c].item, it) {
			prev, c = c, t.nodes[c].sibling
		}
		if c >= 0 && t.nodes[c].item == it {
			node = c
			continue
		}
		n, err := t.alloc()
		if err != nil {
			return err
		}
		t.nodes[n] = cmNode{item: it, supp: supp, sibling: c, children: nilNode}
		if prev >= 0 {
			t.nodes[prev].sibling = n
		} else {
			t.nodes[node].children = n
		}
		for _, rest := range items[k+1:] {
			m, err := t.alloc()
			if err != nil {
				return err
			}
			t.nodes[m] = cmNode{item: rest, supp: supp, sibling: nilNode, children: nilNode}
			t.nodes[n].children = m
			n = m
		}
		return nil
	}
	if supp > t.nodes[node].supp {
		t.nodes[node].supp = supp
	}
	return nil
}

// get is a port of cmt_get: it returns the support stored for the item set
// items (in tree order), or -1 if the tree has no such path.
func (t *cmTree) get(items []tract.Item) tract.Supp {
	p := int32(0)
	for _, it := range items {
		c := t.nodes[p].children
		for c >= 0 && t.before(t.nodes[c].item, it) {
			c = t.nodes[c].sibling
		}
		if c < 0 || t.nodes[c].item != it {
			return -1
		}
		p = c
	}
	return t.nodes[p].supp
}

// merge is a port of merge_pos/merge_neg: it merges two sorted sibling
// lists, recursively merging the children of nodes with equal items and
// keeping the larger support. The second node of an equal pair is freed.
func (t *cmTree) merge(s1, s2 int32) int32 {
	if s1 < 0 {
		return s2
	}
	if s2 < 0 {
		return s1
	}
	out, last := nilNode, nilNode
	for {
		a, b := t.nodes[s1].item, t.nodes[s2].item
		var next int32 // node to append to the output list
		switch {
		case t.before(a, b):
			next, s1 = s1, t.nodes[s1].sibling
		case t.before(b, a):
			next, s2 = s2, t.nodes[s2].sibling
		default:
			children := t.merge(t.nodes[s1].children, t.nodes[s2].children)
			t.nodes[s1].children = children
			if t.nodes[s1].supp < t.nodes[s2].supp {
				t.nodes[s1].supp = t.nodes[s2].supp
			}
			p := s2
			s2 = t.nodes[s2].sibling
			t.release(p)
			next, s1 = s1, t.nodes[s1].sibling
		}
		if last < 0 {
			out = next
		} else {
			t.nodes[last].sibling = next
		}
		last = next
		if s1 < 0 || s2 < 0 {
			break
		}
	}
	rest := s1
	if rest < 0 {
		rest = s2
	}
	t.nodes[last].sibling = rest
	return out
}

// prune is a port of prune_pos/prune_neg: it removes the leading nodes of
// the sorted list node whose items precede item (recursively also in their
// subtrees), merging their subtrees into the remaining list.
func (t *cmTree) prune(node int32, item tract.Item) int32 {
	b := nilNode
	for node >= 0 && t.before(t.nodes[node].item, item) {
		p := t.prune(t.nodes[node].children, item)
		t.nodes[node].children = p
		if p >= 0 {
			if b < 0 {
				b = p
			} else {
				b = t.merge(b, p)
			}
		}
		p = node
		node = t.nodes[node].sibling
		t.release(p)
	}
	switch {
	case node < 0:
		return b
	case b < 0:
		return node
	default:
		return t.merge(b, node)
	}
}

// pruneTo is a port of cmt_prune: it prunes the top level up to (not
// including) item and sets max to the support of the top-level node for
// item, or to cmNone if there is none.
func (t *cmTree) pruneTo(item tract.Item) {
	t.item = item
	c := t.prune(t.nodes[0].children, item)
	t.nodes[0].children = c
	if c >= 0 && t.nodes[c].item == item {
		t.max = t.nodes[c].supp
	} else {
		t.max = cmNone
	}
}

// pastItem returns the pruning bound that also removes item itself
// (upstream: item-1 for descending, item+1 for ascending order).
func (t *cmTree) pastItem(item tract.Item) tract.Item {
	if t.desc {
		return item - 1
	}
	return item + 1
}

// copyFrom is a port of copy: it copies the sibling list s of src (with
// all subtrees) into t and returns the new list. It fails only with
// ErrTooLarge.
func (t *cmTree) copyFrom(src *cmTree, s int32) (int32, error) {
	head, last := nilNode, nilNode
	for ; s >= 0; s = src.nodes[s].sibling {
		sn := src.nodes[s]
		c := nilNode
		if sn.children >= 0 {
			var err error
			if c, err = t.copyFrom(src, sn.children); err != nil {
				return nilNode, err
			}
		}
		n, err := t.alloc()
		if err != nil {
			return nilNode, err
		}
		t.nodes[n] = cmNode{item: sn.item, supp: sn.supp, sibling: nilNode, children: c}
		if last < 0 {
			head = n
		} else {
			t.nodes[last].sibling = n
		}
		last = n
	}
	return head, nil
}

// xcopyFrom is a port of xcopy_pos/xcopy_neg: it copies the sibling list s
// of src into t, keeping only nodes whose item is flagged in keep; the
// subtrees of dropped nodes are merged into the result. It fails only with
// ErrTooLarge.
func (t *cmTree) xcopyFrom(src *cmTree, s int32, keep []bool) (int32, error) {
	head, last, b := nilNode, nilNode, nilNode
	for ; s >= 0; s = src.nodes[s].sibling {
		sn := src.nodes[s]
		c := nilNode
		if sn.children >= 0 {
			var err error
			if c, err = t.xcopyFrom(src, sn.children, keep); err != nil {
				return nilNode, err
			}
		}
		switch {
		case keep[sn.item]:
			n, err := t.alloc()
			if err != nil {
				return nilNode, err
			}
			t.nodes[n] = cmNode{item: sn.item, supp: sn.supp, sibling: nilNode, children: c}
			if last < 0 {
				head = n
			} else {
				t.nodes[last].sibling = n
			}
			last = n
		case c >= 0:
			if b < 0 {
				b = c
			} else {
				b = t.merge(b, c)
			}
		}
	}
	switch {
	case b < 0:
		return head, nil
	case head < 0:
		return b, nil
	default:
		return t.merge(head, b), nil
	}
}

// projectTree is a port of cmt_project (keep == nil) and cmt_xproj (keep
// != nil): it projects src on its top-level item, storing the subtree of
// item in dst (created if nil) and removing item from src. With keep, only
// the flagged items are retained in dst. It fails only with ErrTooLarge
// (dst is returned anyway, so that the caller keeps its arena).
func projectTree(dst, src *cmTree, item tract.Item, keep []bool) (*cmTree, error) {
	if dst == nil {
		dst = newCMTree(src.desc)
	}
	src.item = item
	dst.item = cmValid
	dst.max, src.max = cmNone, cmNone
	dst.nodes[0].supp = 0
	if src.nodes[0].children < 0 {
		return dst, nil
	}
	p := src.prune(src.nodes[0].children, item)
	src.nodes[0].children = p
	if p < 0 || src.nodes[p].item != item {
		return dst, nil
	}
	src.max = src.nodes[p].supp
	dst.nodes[0].supp = src.max
	if c := src.nodes[p].children; c >= 0 {
		var err error
		if keep == nil {
			dst.nodes[0].children, err = dst.copyFrom(src, c)
		} else {
			dst.nodes[0].children, err = dst.xcopyFrom(src, c, keep)
		}
		if err != nil {
			return dst, err
		}
	}
	src.nodes[0].children = src.prune(src.nodes[0].children, src.pastItem(item))
	return dst, nil
}

// closedMaximal is a port of CLOMAX, the closed/maximal item set filter.
type closedMaximal struct {
	desc  bool
	cnt   int       // number of items in the current prefix
	trees []*cmTree // conditional prefix trees; nil = not created yet
	// keep holds the flags for tail projections. Upstream keeps one array
	// per tree, sized by the tree's item count and indexed by item code,
	// which is in range only for descending order (tract.md T16); one
	// shared array over all item codes is safe for both orders.
	keep []bool
	// stored records whether update ever stored a set.
	stored bool
}

// newClosedMaximal is a port of cm_create.
func newClosedMaximal(dir, numItems int) *closedMaximal {
	cm := &closedMaximal{desc: dir < 0, keep: make([]bool, numItems)}
	t := newCMTree(cm.desc)
	_ = t.add(nil, 0) // an empty set allocates no node
	t.item = cmValid
	cm.trees = []*cmTree{t}
	return cm
}

// supp is a port of cm_supp: the largest support of a stored superset of
// the current prefix (negative if there is none). For the empty prefix it
// is the root support of trees[0], which starts at 0.
func (cm *closedMaximal) supp() tract.Supp {
	if cm.cnt > 0 {
		return cm.trees[cm.cnt-1].max
	}
	return cm.trees[0].nodes[0].supp
}

// tree returns trees[p], extending the stack with nil entries if needed.
func (cm *closedMaximal) tree(p int) *cmTree {
	for len(cm.trees) <= p {
		cm.trees = append(cm.trees, nil)
	}
	return cm.trees[p]
}

// add is a port of cm_add: it extends the prefix by item unless a stored
// superset of prefix ∪ {item} has a support of at least supp, in which case
// it returns false and leaves the prefix unchanged. It fails only with
// ErrTooLarge.
func (cm *closedMaximal) add(item tract.Item, supp tract.Supp) (bool, error) {
	p := cm.cnt
	t := cm.tree(p)
	if t == nil || !t.valid() {
		src := cm.trees[p-1]
		var err error
		t, err = projectTree(t, src, src.item, nil)
		cm.trees[p] = t
		if err != nil {
			return false, err
		}
	}
	t.pruneTo(item)
	if t.max >= supp {
		return false, nil
	}
	cm.cnt++
	return true, nil
}

// remove is a port of cm_remove: it removes n items from the prefix and
// clears the trees above the new top.
func (cm *closedMaximal) remove(n int) {
	target := 0
	if n < cm.cnt {
		target = cm.cnt - n
	}
	for ; cm.cnt > target; cm.cnt-- {
		if cm.cnt < len(cm.trees) && cm.trees[cm.cnt] != nil {
			cm.trees[cm.cnt].clear()
		}
	}
}

// tail is a port of cm_tail (head union tail pruning): it projects the
// tree of the current prefix keeping only the tail items (in repository
// order), stores the projection as the next tree and returns the support of
// the whole tail in it if positive, else 0. An empty tail yields 1. It
// fails only with ErrTooLarge.
func (cm *closedMaximal) tail(items []tract.Item) (tract.Supp, error) {
	if len(items) == 0 {
		return 1, nil
	}
	if cm.cnt <= 0 {
		return 0, nil
	}
	p := cm.cnt
	dst := cm.tree(p)
	src := cm.trees[p-1]
	for _, it := range items {
		cm.keep[it] = true
	}
	t, err := projectTree(dst, src, src.item, cm.keep)
	for _, it := range items {
		cm.keep[it] = false
	}
	cm.trees[p] = t
	if err != nil {
		return 0, err
	}
	if s := t.get(items); s > 0 {
		return s, nil
	}
	return 0, nil
}

// update is a port of cm_update: it stores the item set items (sorted in
// repository order, containing every prefix item) with support supp in all
// trees of the current prefix. It fails only with ErrTooLarge.
func (cm *closedMaximal) update(items []tract.Item, supp tract.Supp) error {
	j := 0
	for i := range cm.cnt {
		t := cm.trees[i]
		for items[j] != t.item {
			j++
		}
		j++
		if err := t.add(items[j:], supp); err != nil {
			return err
		}
		cm.stored = true
	}
	return nil
}
