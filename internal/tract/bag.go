package tract

import (
	"cmp"
	"fmt"
	"iter"
	"math"
	"slices"
)

// End is upstream's TA_END (ITEM_MIN), the sentinel that terminates
// upstream's item arrays. Go transactions are slices and carry no
// sentinel; End remains the base of a packed entry (see Bag.Pack).
const End Item = math.MinInt32

// maxPack is the largest number of items ta_pack packs.
const maxPack = 31

// IsPacked reports whether i is a packed entry (End | bits with bits
// != 0) rather than an item code. Upstream consumers test *p < 0.
func (i Item) IsPacked() bool { return i < 0 && i != End }

// PackedBits returns the bit set of a packed entry: bit k is set if
// item k is part of the transaction.
func (i Item) PackedBits() uint32 { return uint32(i) &^ uint32(1<<31) }

// node is a node of the staging prefix tree (24 bytes). The path from
// the root to a node spells a transaction prefix in ascending item order.
type node struct {
	item    Item  // item on the edge from the parent
	child   int32 // first child, 0 if none (index 0 is the root)
	sibling int32 // next sibling, 0 if none; descending items once built
	nchild  int32 // number of children (used while building)
	wgt     Supp  // summed weight of the records ending here, -1 if none
}

// bigFanout is the number of children above which a node's children are
// found through bagBuilder.index instead of a linear list scan.
const bigFanout = 8

// Bag is the transaction bag of a recoded item base: the distinct
// transactions of the database with their summed weights. It replaces
// upstream's TABAG after tbg_filter, tbg_itsort(+1), tbg_sort(+1, 0),
// tbg_reduce(0) and optionally tbg_pack, as performed by fpg_data. The
// transactions are kept in a prefix tree, never as a list.
//
// A Bag is created by ItemBase.BuildBag. Its methods may be called
// concurrently, except Pack.
type Bag struct {
	nodes     []node  // prefix tree; nodes[0] is the root (empty set)
	rootChild []int32 // child of the root per item, 0 if none
	frq       []Supp  // per item: weight of the records containing it
	headFrq   []Supp  // per item: weight of the records starting with it
	records   int     // number of records (tbg_cnt before reduction)
	cnt       int     // number of transactions (tbg_cnt after reduction)
	wgt       Supp    // total weight (tbg_wgt)
	maxSize   int     // tbg_max
	extent    int64   // tbg_extent
	pack      int     // number of packed items, 0 if not packed
	dropZero  bool    // transactions of weight 0 were deleted by reduction
}

// bagBuilder is the state of pass 2.
type bagBuilder struct {
	bag       *Bag
	index     map[uint64]int32 // (parent<<32 | item) -> child, big fanouts
	terms     int              // number of nodes where records end
	zeroTerms int              // ... of which with summed weight 0
	ext       int64            // sum of their depths (< 2^62: int32 nodes)
	zeroExt   int64            // ... of which with summed weight 0
	sortBuf   []int32          // scratch of finish: the children of a node
}

// BuildBag reads the database a second time (pass 2) and returns its
// transaction bag. scan must yield the same records in the same order
// as all Count passes together; BuildBag returns ErrNotReplayable if the
// number of records, the total weight or the set of items differ.
//
// Every record is recoded (unknown, unselected and removed items are
// dropped, duplicates ignored), its items are sorted ascending, and the
// zmin filter of tbg_filter(bag, zmin, NULL, 0) is applied: if zmin > 1,
// a record with fewer than zmin items becomes the empty transaction. It
// keeps its weight and still counts in TotalWeight and Records, and,
// like every other empty transaction, merges into the one empty
// transaction of the bag. The caller passes zmin <= 1 where fpg_data
// does not filter (target rules, evaluation measures other than none
// and b). BuildBag does not modify the item base; it may be called again.
func (b *ItemBase[T]) BuildBag(scan ScanFunc[T], zmin int) (*Bag, error) {
	if !b.isRecoded {
		panic("tract: ItemBase.BuildBag called before Recode")
	}
	bld := newBagBuilder(len(b.items))
	skipUnknown := b.defApp == AppearNone
	var (
		records int
		total   Supp
		codes   []Item
		err     error
	)
	scanErr := scan(func(ids []T, w Supp) bool {
		records++
		if records > b.records {
			err = fmt.Errorf("record %d: more records than in pass 1: %w", records, ErrNotReplayable)
			return false
		}
		if w < 0 {
			err = fmt.Errorf("record %d: weight %d: %w", records, w, ErrNegativeWeight)
			return false
		}
		var ok bool
		if total, ok = addChecked(total, w); !ok {
			err = fmt.Errorf("record %d: %w", records, ErrWeightOverflow)
			return false
		}
		codes = codes[:0]
		for _, id := range ids { // recode (upstream: recode)
			c, known := b.codes[id]
			if !known {
				if b.check != nil {
					if err = b.check(id); err != nil {
						err = fmt.Errorf("record %d: %w", records, err)
						return false
					}
				}
				if skipUnknown {
					continue
				}
				err = fmt.Errorf("record %d: item %v not seen in pass 1: %w", records, id, ErrNotReplayable)
				return false
			}
			if c >= 0 {
				codes = append(codes, c)
			}
		}
		slices.Sort(codes) // tbg_itsort; then drop duplicates
		codes = slices.Compact(codes)
		if zmin > 1 && len(codes) < zmin { // tbg_filter
			codes = codes[:0]
		}
		if err = bld.add(codes, w); err != nil {
			err = fmt.Errorf("record %d: %w", records, err)
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if scanErr != nil {
		return nil, scanErr
	}
	if records != b.records || total != b.wgt {
		return nil, fmt.Errorf(
			"pass 2 read %d records of weight %d, pass 1 %d of weight %d: %w",
			records, total, b.records, b.wgt, ErrNotReplayable)
	}
	return bld.finish(records, total, b.abort)
}

// newBagBuilder returns a builder for a bag over n items.
func newBagBuilder(n int) *bagBuilder {
	bb := &bagBuilder{}
	bb.reset(n)
	return bb
}

// reset prepares the builder for a new bag over n items. The bag of the
// previous build, if any, is reused: its storage (and that of the index
// map) is kept, so the previous bag becomes invalid.
func (bb *bagBuilder) reset(n int) {
	bag := bb.bag
	if bag == nil {
		bag = &Bag{}
	}
	nodes := append(bag.nodes[:0], node{wgt: -1})
	rootChild := resize(bag.rootChild, n)
	frq := resize(bag.frq, n)
	headFrq := resize(bag.headFrq, n)
	clear(rootChild)
	clear(frq)
	clear(headFrq)
	*bag = Bag{nodes: nodes, rootChild: rootChild, frq: frq, headFrq: headFrq}
	clear(bb.index)
	*bb = bagBuilder{bag: bag, index: bb.index, sortBuf: bb.sortBuf[:0]}
}

// add inserts one recoded, sorted and filtered record into the prefix
// tree and updates the counters.
func (bb *bagBuilder) add(t []Item, w Supp) error {
	bag := bb.bag
	if len(t) > math.MaxInt32-len(bag.nodes) {
		return fmt.Errorf("more than %d prefix tree nodes: %w", math.MaxInt32, ErrTooLarge)
	}
	bag.maxSize = max(bag.maxSize, len(t))
	var v int32 // current node, starting at the root
	for _, c := range t {
		v = bb.child(v, c)
		bag.frq[c] += w // bounded by the total weight
	}
	if len(t) > 0 {
		bag.headFrq[t[0]] += w
	}
	nd := &bag.nodes[v]
	size := int64(len(t))
	switch {
	case nd.wgt < 0: // first record ending here
		nd.wgt = w
		bb.terms++
		bb.ext += size
		if w == 0 {
			bb.zeroTerms++
			bb.zeroExt += size
		}
	case nd.wgt == 0 && w > 0: // a zero-weight transaction gets weight
		nd.wgt = w
		bb.zeroTerms--
		bb.zeroExt -= size
	default:
		nd.wgt += w // bounded by the total weight
	}
	return nil
}

// child returns the child of node v for item c, creating it if needed.
// The root's children are indexed directly, nodes with few children are
// scanned, and nodes with many children use the index map, so a lookup
// never scans more than bigFanout siblings.
func (bb *bagBuilder) child(v int32, c Item) int32 {
	bag := bb.bag
	if v == 0 { // the root's children are not linked into a list
		ch := bag.rootChild[c]
		if ch == 0 {
			ch = int32(len(bag.nodes)) // add checked the capacity
			bag.nodes = append(growNodes(bag.nodes), node{item: c, wgt: -1})
			bag.rootChild[c] = ch
		}
		return ch
	}
	nodes := bag.nodes
	if nodes[v].nchild > bigFanout {
		key := childKey(v, c)
		if ch, ok := bb.index[key]; ok {
			return ch
		}
		ch := bb.link(v, c)
		bb.index[key] = ch
		return ch
	}
	for ch := nodes[v].child; ch != 0; ch = nodes[ch].sibling {
		if nodes[ch].item == c {
			return ch
		}
	}
	ch := bb.link(v, c)
	nodes = bag.nodes
	if nodes[v].nchild > bigFanout { // switch to the index map
		if bb.index == nil {
			bb.index = map[uint64]int32{}
		}
		for x := nodes[v].child; x != 0; x = nodes[x].sibling {
			bb.index[childKey(v, nodes[x].item)] = x
		}
	}
	return ch
}

// link creates a new child of node v for item c at the head of v's
// child list and returns it.
func (bb *bagBuilder) link(v int32, c Item) int32 {
	bag := bb.bag
	ch := int32(len(bag.nodes)) // add checked the capacity
	bag.nodes = append(growNodes(bag.nodes), node{item: c, sibling: bag.nodes[v].child, wgt: -1})
	bag.nodes[v].child = ch
	bag.nodes[v].nchild++
	return ch
}

// growNodes returns nodes with room for one more node, doubling the
// capacity when it must grow. append grows large slices by only a
// quarter, which copied every node about five times while building a
// prefix tree of hundreds of thousands of nodes. (The tree is not
// presized from the item occurrences of pass 1: a bag of few distinct
// transactions must stay small however long the input is.)
func growNodes(nodes []node) []node {
	if len(nodes) < cap(nodes) {
		return nodes
	}
	return slices.Grow(nodes, len(nodes))
}

// childKey is the index map key of the child of v for item c.
func childKey(v int32, c Item) uint64 {
	return uint64(v)<<32 | uint64(uint32(c))
}

// finishCheckEvery is the number of prefix tree nodes between two calls
// of the abort function in finish.
const finishCheckEvery = 1 << 15

// finish completes the bag: it applies tbg_reduce's rules to the counts
// and sorts every child list by descending item. abort, if not nil, is
// called every finishCheckEvery nodes; its error stops finish.
func (bb *bagBuilder) finish(records int, total Supp, abort func() error) (*Bag, error) {
	bag := bb.bag
	bag.records = records
	bag.wgt = total
	// tbg_reduce(bag, 0) deletes merged transactions of weight 0, but
	// only if the bag holds at least two transactions; it returns early
	// otherwise. Upstream does not recompute tbg_max (bag.maxSize keeps
	// the size of deleted transactions).
	bag.dropZero = records > 1
	bag.cnt = bb.terms
	bag.extent = bb.ext
	if bag.dropZero {
		bag.cnt -= bb.zeroTerms
		bag.extent -= bb.zeroExt
	}
	buf := bb.sortBuf
	defer func() { bb.sortBuf = buf[:0] }()
	for v := range bag.nodes {
		if abort != nil && v%finishCheckEvery == finishCheckEvery-1 {
			if err := abort(); err != nil {
				return nil, err
			}
		}
		nd := &bag.nodes[v]
		if nd.nchild < 2 {
			continue
		}
		buf = buf[:0]
		for x := nd.child; x != 0; x = bag.nodes[x].sibling {
			buf = append(buf, x)
		}
		slices.SortFunc(buf, func(x, y int32) int {
			return cmp.Compare(bag.nodes[y].item, bag.nodes[x].item)
		})
		nd.child = buf[0]
		for k, x := range buf {
			next := int32(0)
			if k+1 < len(buf) {
				next = buf[k+1]
			}
			bag.nodes[x].sibling = next
		}
	}
	return bag, nil
}

// NumItems returns the number of items, i.e. of codes 0..NumItems()-1
// that may occur in the transactions (tbg_itemcnt).
func (bag *Bag) NumItems() int { return len(bag.rootChild) }

// Records returns the number of records read, including empty ones and
// those of weight 0 (tbg_cnt before tbg_reduce).
func (bag *Bag) Records() int { return bag.records }

// Len returns the number of distinct transactions All and Backward yield
// (tbg_cnt after tbg_reduce). Transactions whose summed weight is 0 are
// not counted, unless Records() is at most 1.
func (bag *Bag) Len() int { return bag.cnt }

// TotalWeight returns the summed weight of all records (tbg_wgt).
func (bag *Bag) TotalWeight() Supp { return bag.wgt }

// MaxSize returns the largest number of items in a record after recoding
// and filtering (tbg_max). Like upstream it includes records of weight 0
// that the reduction deleted.
func (bag *Bag) MaxSize() int { return bag.maxSize }

// Extent returns the total number of item instances in the distinct
// transactions (tbg_extent after tbg_reduce). Packing does not change it.
func (bag *Bag) Extent() int64 { return bag.extent }

// NodeCount returns the number of prefix tree nodes besides the root.
// A prefix tree of the transactions in the same item order, with some
// items removed, has at most this many nodes; Extent bounds a prefix tree
// in any item order.
func (bag *Bag) NodeCount() int { return max(len(bag.nodes)-1, 0) }

// Pack makes All and Backward yield the items with codes 0..n-1 of every
// transaction as one packed entry (port of tbg_pack/ta_pack as called by
// fpg_data; fpgrowth itself limits n to 16). n is clamped to 31 like in
// ta_pack; n <= 0 does nothing. Packing again keeps the larger count,
// which is what re-packing packed transactions does upstream.
//
// A transaction containing at least one item below n starts with the
// entry End | bits, where bit k is set for every packed item k, followed
// by its remaining items in ascending order. Since items are sorted, the
// packed items are a prefix of the transaction and the entry takes the
// place of the first one. Other transactions are unchanged. The order of
// the transactions does not change, as upstream does not sort again.
func (bag *Bag) Pack(n int) {
	if n <= 0 {
		return
	}
	bag.pack = max(bag.pack, min(n, maxPack))
}

// PackCount returns the number of packed items, 0 if Pack was not called.
func (bag *Bag) PackCount() int { return bag.pack }

// Frequencies returns the item frequencies of the bag (tbg_ifrqs(bag,
// 0)): for every item the summed weight of the transactions containing
// it, after the zmin filter. If the bag is packed, element 0 holds the
// weight of the transactions that have a packed entry and elements
// 1..min(PackCount(), NumItems())-1 are 0. The result is a new slice.
// Unlike upstream's cached array it always reflects the current packing.
func (bag *Bag) Frequencies() []Supp {
	f := make([]Supp, len(bag.frq))
	copy(f, bag.frq)
	if bag.pack > 0 && len(f) > 0 {
		var s Supp // bounded by the total weight
		for i := range min(bag.pack, len(f)) {
			s += bag.headFrq[i]
			f[i] = 0
		}
		f[0] = s
	}
	return f
}

// All returns an iterator over the distinct transactions and their
// summed weights in upstream's bag order (tbg_tract(bag, j) for
// j = 0..Len()-1): ascending lexicographic order of the item sequences,
// where a proper prefix precedes its extensions, so the empty
// transaction comes first. The items slice is a buffer that is
// overwritten by the next step; it must not be modified or retained.
func (bag *Bag) All() iter.Seq2[[]Item, Supp] {
	return func(yield func([]Item, Supp) bool) {
		if len(bag.nodes) == 0 {
			return
		}
		path := make([]Item, bag.maxSize)
		if !bag.emit(0, path[:0], yield) {
			return
		}
		type frame struct{ node, depth int32 }
		var stack []frame
		nodes := bag.nodes
		for _, r := range bag.rootChild { // pre-order, ascending items
			if r == 0 {
				continue
			}
			stack = append(stack[:0], frame{node: r, depth: 1})
			for len(stack) > 0 {
				f := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				path[f.depth-1] = nodes[f.node].item
				if !bag.emit(f.node, path[:f.depth], yield) {
					return
				}
				// Children are linked in descending order, so the
				// smallest one ends up on top of the stack.
				for x := nodes[f.node].child; x != 0; x = nodes[x].sibling {
					stack = append(stack, frame{node: x, depth: f.depth + 1})
				}
			}
		}
	}
}

// Backward returns an iterator over the transactions of All in reverse
// order (j = Len()-1..0, the order in which fpgrowth inserts them into
// its FP-trees). The same buffer rules apply.
func (bag *Bag) Backward() iter.Seq2[[]Item, Supp] {
	return func(yield func([]Item, Supp) bool) {
		if len(bag.nodes) == 0 {
			return
		}
		nodes := bag.nodes
		path := make([]Item, bag.maxSize)
		stk := make([]int32, bag.maxSize) // node per depth
		for c := len(bag.rootChild) - 1; c >= 0; c-- {
			v := bag.rootChild[c] // post-order, descending items
			if v == 0 {
				continue
			}
			d := 1
			stk[0], path[0] = v, nodes[v].item
		subtree:
			for {
				for nodes[v].child != 0 { // descend to the largest leaf
					v = nodes[v].child
					stk[d], path[d] = v, nodes[v].item
					d++
				}
				for { // v's subtree is done: emit v, then move on
					if !bag.emit(v, path[:d], yield) {
						return
					}
					if d == 1 {
						break subtree
					}
					if s := nodes[v].sibling; s != 0 {
						v = s
						stk[d-1], path[d-1] = s, nodes[s].item
						continue subtree
					}
					d--
					v = stk[d-1]
				}
			}
		}
		bag.emit(0, path[:0], yield)
	}
}

// emit yields the transaction t ending at node v if it is one of the
// bag's transactions, packing it if needed. It returns false if the
// iteration is to stop.
func (bag *Bag) emit(v int32, t []Item, yield func([]Item, Supp) bool) bool {
	w := bag.nodes[v].wgt
	if w < 0 || (w == 0 && bag.dropZero) {
		return true
	}
	n := len(t)
	if bag.pack == 0 || n == 0 || int(t[0]) >= bag.pack {
		return yield(t[:n:n], w)
	}
	var bits uint32 // ta_pack: collect the items below pack
	j := 0
	for j < n && int(t[j]) < bag.pack {
		bits |= 1 << uint32(t[j])
		j++
	}
	saved := t[j-1]
	t[j-1] = End | Item(bits) // the entry replaces the last packed item
	ok := yield(t[j-1:n:n], w)
	t[j-1] = saved
	return ok
}
