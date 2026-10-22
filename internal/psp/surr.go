package psp

import (
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

	// Random: the persistent item frequency buffer (dst->buf), the sum of
	// the frequencies, and the per-call work arrays (by item code)
	ifrq  []itemFrq
	total int64
	dif   []int64
	mark  []bool

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
		sortTracts(c.dst) // fpg_data(tasur, FPG_SURR, 0) of the last set
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
	for _, t := range ts {
		slices.Sort(t)
	}
	slices.SortFunc(ts, slices.Compare[[]tract.Item])
}

// random is a port of tbg_random with oracle patch P4.
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
	}
	ifrq := c.ifrq
	for k := len(c.dst) - 1; k >= 0; k-- {
		t := c.dst[k]
		z := uint64(c.total) // number of item instances
		m := n
		for i := range t { // draw the items without replacement
			x := uint64(c.rng.Float64() * float64(z))
			j := 0
			for ; j < m; j++ {
				if x < uint64(ifrq[j].frq) {
					break
				}
				x -= uint64(ifrq[j].frq)
			}
			if j >= m {
				j = m - 1
			}
			t[i] = ifrq[j].item
			z -= uint64(ifrq[j].frq)
			m--
			ifrq[j], ifrq[m] = ifrq[m], ifrq[j]
		}
	}
	// repair the item frequencies: replace over-represented items by
	// under-represented ones, one per transaction
	dif, mark := c.dif, c.mark
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
	for _, d := range dif {
		if d > 0 {
			z += d
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
			i := int(tract.Item(c.rng.Float64()*float64(n))) % n
			j := (i + n - 1) % n // search cyclically from a random start
			for i != j && (mark[i] || dif[i] >= 0) {
				i = (i + 1) % n
			}
			// P4: the item the search stopped at is examined too
			// (upstream: if (i != j)).
			if !mark[i] && dif[i] < 0 {
				old := t[d]
				mark[old] = false
				dif[old]--
				t[d] = tract.Item(i)
				dif[i]++
				z--
			}
		}
		for _, s := range t {
			mark[s] = false
		}
	}
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
