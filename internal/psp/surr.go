package psp

import (
	mathbits "math/bits"
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Method is a surrogate data generation method (fpgpsp's -g#, sur_tab).
type Method int

// Surrogate generation methods (upstream FPG_IDENTITY ... FPG_SHUFFLE).
const (
	Identity Method = iota // tbg_ident: the data itself (-gi)
	Random                 // tbg_random: transaction randomization (-gr)
	Swap                   // tbg_swap: permutation by pair swaps (-gp)
	Shuffle                // tbg_shuffle: column shuffles of a table (-gs)
)

// itemFrq is an entry of tbg_random's item frequency buffer (ITEMFRQ
// without the dif field, which tbg_random indexes by item code instead
// of by entry).
type itemFrq struct {
	item tract.Item
	frq  int64
}

// Chain generates a sequence of surrogate data sets from one source with
// one random number generator: the state of one thread of fpg_genpsp (or
// of its single-threaded loop). Every data set is derived from the
// previous one where upstream does so: Random keeps its permuted item
// buffer, Swap continues from the previous surrogate, and before each
// new data set the previous one is sorted the way fpg_data(FPG_SURR)
// sorts it for mining. A Chain is not safe for concurrent use.
type Chain struct {
	src    *Data
	method Method
	rng    RNG
	dst    [][]tract.Item // the surrogate buffer (upstream tasur)
	done   bool           // dst holds a surrogate (sort before the next)
	sorter tractSorter

	// Random: the persistent item frequency buffer (dst->buf), its
	// prefix-sum tree, the sum of the frequencies, and the per-call work
	// arrays (by item code; under is the bit set of dif < 0)
	ifrq  []itemFrq
	fen   fenwick
	total int64
	dif   []int64
	mark  []bool
	under []uint64

	// Swap: whether the first run (2·extent swaps) is done, the extent,
	// and the item flag and offset arrays of iswap
	swapped bool
	extent  int64
	flags   []bool
	offs    []int32
}

// NewChain returns a chain over src (which it does not modify) seeded
// like rng_create(seed).
func NewChain(src *Data, method Method, seed uint32) *Chain {
	c := &Chain{src: src, method: method}
	c.rng.Seed(seed)
	return c
}

// Next returns the next surrogate data set. The transactions belong to
// the chain and are overwritten by the next call; the caller may read
// them (in any order) until then, but must not modify them.
func (c *Chain) Next() [][]tract.Item {
	if c.done && (c.method == Random || c.method == Swap) {
		c.sorter.sort(c.dst) // fpg_data(tasur, FPG_SURR, 0) of the last set
	}
	c.done = true
	switch c.method {
	case Identity:
		if c.dst == nil {
			c.dst = c.src.clone()
		}
	case Random:
		c.random()
	case Swap:
		c.swap()
	case Shuffle:
		c.shuffle()
	default:
		panic("psp: unknown surrogate method")
	}
	return c.dst
}

// sortTracts is fpg_data's tbg_itsort(bag, +1, 0) and tbg_sort(bag, +1, 0)
// for unweighted transactions: items ascending within a transaction, and
// transactions in ascending lexicographic order where a proper prefix
// comes first (ta_cmp). Equal transactions are indistinguishable, so any
// correct sort gives upstream's order.
func sortTracts(ts [][]tract.Item) {
	var s tractSorter
	s.sort(ts)
}

// tractSorter is sortTracts with a reusable buffer. It sorts the
// transactions by a key of their first two items, which decides most
// comparisons with one integer comparison (the keys order like ta_cmp
// on the first two items, a missing item before every item), and
// compares the remaining items only for equal keys.
type tractSorter struct{ buf []keyedTract }

// keyedTract is a transaction and its sort key: (t[0]+1)<<32 | (t[1]+1),
// with 0 for a missing item (item codes are non-negative int32 values).
type keyedTract struct {
	key uint64
	t   []tract.Item
}

func (s *tractSorter) sort(ts [][]tract.Item) {
	buf := s.buf[:0]
	if cap(buf) < len(ts) {
		buf = make([]keyedTract, 0, len(ts))
	}
	for _, t := range ts {
		tract.SortItems(t)
		var key uint64
		if len(t) > 0 {
			key = (uint64(t[0]) + 1) << 32
			if len(t) > 1 {
				key |= uint64(t[1]) + 1
			}
		}
		buf = append(buf, keyedTract{key: key, t: t})
	}
	slices.SortFunc(buf, func(a, b keyedTract) int {
		switch {
		case a.key < b.key:
			return -1
		case a.key > b.key:
			return 1
		case a.key&(1<<32-1) == 0: // at most one item, equal
			return 0
		}
		return slices.Compare(a.t[2:], b.t[2:])
	})
	for k := range buf {
		ts[k] = buf[k].t
	}
	s.buf = buf[:0]
}

// random is a port of tbg_random with oracle patch P4.
//
// Upstream finds a drawn item by a linear search of the item buffer and
// a replacement item of the repair by a linear cyclic search; both are
// linear in the number of items for every item instance, which made the
// generation the larger part of fpgpsp -gr on sparse data. Here the
// draw searches a Fenwick tree of the prefix sums of the buffer (exact
// integer sums, so it finds the same entry), and the repair searches a
// bit set of the under-represented items (it finds the same item: the
// first one from the random start that is under-represented and not in
// the transaction). The surrogates and the random numbers drawn are
// upstream's (TestRandom_matchesLinear, TestChain_matchesC).
func (c *Chain) random() {
	n := c.src.NItems
	if c.dst == nil { // clone(src): the slot sizes of the source
		c.dst = c.src.clone()
	}
	if c.ifrq == nil { // initialize the item frequencies (dst->buf)
		c.ifrq = make([]itemFrq, n)
		for i := range c.ifrq {
			c.ifrq[i].item = tract.Item(i)
		}
		for _, t := range c.src.Tracts {
			for _, s := range t {
				c.ifrq[s].frq++
			}
		}
		for _, f := range c.ifrq {
			c.total += f.frq
		}
		c.dif = make([]int64, n)
		c.mark = make([]bool, n)
		c.under = make([]uint64, (n+63)/64)
		c.fen = newFenwick(c.ifrq)
	}
	ifrq, fen := c.ifrq, c.fen
	for k := len(c.dst) - 1; k >= 0; k-- {
		t := c.dst[k]
		z := uint64(c.total) // number of item instances
		m := n
		for i := range t { // draw the items without replacement
			x := uint64(c.rng.Float64() * float64(z))
			// the first entry j < m whose prefix sum exceeds x; the
			// entries from m on hold the items drawn for t, so a j >= m
			// means x >= z (upstream: the loop ends with j = m)
			j := min(fen.search(x), m-1)
			t[i] = ifrq[j].item
			z -= uint64(ifrq[j].frq)
			m--
			if j != m {
				if d := ifrq[m].frq - ifrq[j].frq; d != 0 {
					fen.add(j, d)
					fen.add(m, -d)
				}
				ifrq[j], ifrq[m] = ifrq[m], ifrq[j]
			}
		}
	}
	// repair the item frequencies: replace over-represented items by
	// under-represented ones, one per transaction
	dif, mark, under := c.dif, c.mark, c.under
	clear(dif)
	for _, t := range c.dst {
		for _, s := range t {
			dif[s]++
		}
	}
	for _, t := range c.src.Tracts {
		for _, s := range t {
			dif[s]--
		}
	}
	var z int64 // amount of over-representation
	clear(under)
	for i, d := range dif {
		if d > 0 {
			z += d
		} else if d < 0 {
			under[i>>6] |= 1 << (i & 63)
		}
	}
	for k := len(c.dst) - 1; k >= 0 && z > 0; k-- {
		t := c.dst[k]
		d := -1 // position of the last over-represented item
		for p, s := range t {
			mark[s] = true
			if dif[s] > 0 {
				d = p
			}
		}
		if d >= 0 {
			// Upstream searches cyclically from a random start i up to
			// i-1 for an item that is not marked and under-represented;
			// with P4 the item where the search stops is examined too,
			// so every item is examined (upstream: if (i != j)).
			start := int(tract.Item(c.rng.Float64()*float64(n))) % n
			if i := firstUnder(under, mark, start, n); i >= 0 {
				old := t[d]
				mark[old] = false
				dif[old]-- // was over-represented: stays >= 0
				t[d] = tract.Item(i)
				if dif[i]++; dif[i] == 0 {
					under[i>>6] &^= 1 << (i & 63)
				}
				z--
			}
		}
		for _, s := range t {
			mark[s] = false
		}
	}
}

// firstUnder returns the first item in the cyclic order start, start+1,
// ..., n-1, 0, ..., start-1 that is in the bit set under and not marked,
// or -1 if there is none.
func firstUnder(under []uint64, mark []bool, start, n int) int {
	if i := nextUnder(under, mark, start, n); i >= 0 {
		return i
	}
	return nextUnder(under, mark, 0, start)
}

// nextUnder returns the first item i in [from, to) that is in under and
// not marked, or -1.
func nextUnder(under []uint64, mark []bool, from, to int) int {
	for w := from >> 6; w<<6 < to; w++ {
		bits := under[w]
		if w == from>>6 {
			bits &^= 1<<(from&63) - 1 // the items below from
		}
		for bits != 0 {
			i := w<<6 | mathbits.TrailingZeros64(bits)
			if i >= to {
				return -1
			}
			if !mark[i] {
				return i
			}
			bits &= bits - 1
		}
	}
	return -1
}

// fenwick is a Fenwick (binary indexed) tree over the frequencies of the
// entries of tbg_random's item buffer, by position.
type fenwick struct {
	tree []int64 // 1-based: tree[i] sums the entries (i - lowbit(i), i]
	high int     // the largest power of 2 not above len(tree)-1
}

// newFenwick returns the tree of the frequencies of buf.
func newFenwick(buf []itemFrq) fenwick {
	f := fenwick{tree: make([]int64, len(buf)+1)}
	for i, e := range buf {
		f.tree[i+1] += e.frq
		if p := i + 1 + (i+1)&-(i+1); p < len(f.tree) {
			f.tree[p] += f.tree[i+1]
		}
	}
	for f.high = 1; f.high*2 < len(f.tree); f.high *= 2 {
	}
	return f
}

// add adds d to the frequency of the entry at position j.
func (f fenwick) add(j int, d int64) {
	for i := j + 1; i < len(f.tree); i += i & -i {
		f.tree[i] += d
	}
}

// search returns the smallest position j whose prefix sum (of the
// entries 0..j) exceeds x, or the number of entries if there is none.
func (f fenwick) search(x uint64) int {
	pos := 0
	for step := f.high; step > 0; step >>= 1 {
		if p := pos + step; p < len(f.tree) && uint64(f.tree[p]) <= x {
			pos = p
			x -= uint64(f.tree[p])
		}
	}
	return pos
}

// swap is a port of tbg_swap.
func (c *Chain) swap() {
	if c.dst == nil {
		c.dst = c.src.clone()
		c.extent = c.src.Extent()
	}
	cnt := len(c.dst)
	if cnt < 2 {
		return
	}
	var z int64
	if !c.swapped { // first run: 2·extent swaps, later extent/2
		c.swapped = true
		c.flags = make([]bool, c.src.NItems)
		c.offs = make([]int32, c.src.NItems)
		z = c.extent << 1
	} else {
		z = c.extent >> 1
	}
	for ; z > 0; z-- {
		i := min(max(int(c.rng.Float64()*float64(cnt)), 0), cnt-1)
		j := min(max(int(c.rng.Float64()*float64(cnt)), 0), cnt-1)
		if i == j {
			continue
		}
		c.iswap(c.dst[i], c.dst[j])
	}
}

// iswap is a port of iswap: it exchanges a random item of t1 that is not
// in t2 with a random item of t2 that is not in t1, where t1 is the
// larger transaction. Random numbers are drawn as upstream does, also
// when no exchange is possible.
func (c *Chain) iswap(t1, t2 []tract.Item) {
	flags, offs := c.flags, c.offs
	if len(t1) < len(t2) {
		t1, t2 = t2, t1
	}
	for _, s := range t2 {
		flags[s] = true
	}
	k := int32(0)
	for p, s := range t1 { // offsets of the items of t1 not in t2
		if !flags[s] {
			offs[k] = int32(p)
			k++
		}
		flags[s] = false
	}
	if k <= 0 { // equal transactions: all flags are cleared
		return
	}
	i := int32(c.rng.Float64() * float64(k))
	i = offs[max(i, 0)%k]
	k = 0
	for p, s := range t2 { // offsets of the items of t2 not in t1
		if flags[s] {
			offs[k] = int32(p)
			k++
		}
		flags[s] = false
	}
	if k <= 0 {
		return
	}
	j := int32(c.rng.Float64() * float64(k))
	j = offs[max(j, 0)%k]
	t1[i], t2[j] = t2[j], t1[i]
}

// shuffle is a port of tbg_shuffle: a copy of the source whose columns
// are permuted independently (Fisher-Yates over the rows from the last
// one down, with all columns of a row in turn).
func (c *Chain) shuffle() {
	if c.dst == nil {
		c.dst = c.src.clone()
	} else {
		for k, t := range c.src.Tracts {
			copy(c.dst[k], t)
		}
	}
	t := c.dst
	if len(t) < 2 {
		return
	}
	n := len(t[0])
	for k := len(t) - 1; k > 0; k-- {
		for i := range n {
			j := min(max(int(c.rng.Float64()*float64(k+1)), 0), k)
			t[j][i], t[k][i] = t[k][i], t[j][i]
		}
	}
}
