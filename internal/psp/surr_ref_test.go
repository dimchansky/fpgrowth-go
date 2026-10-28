package psp

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// refRandom is the straightforward port of tbg_random with patch P4 (the
// implementation before issue #39): a linear search of the item buffer
// for every drawn item and a linear cyclic search for the repair. Chain
// must generate the same surrogates.
type refRandom struct {
	src   *Data
	rng   RNG
	dst   [][]tract.Item
	ifrq  []itemFrq
	total int64
	dif   []int64
	mark  []bool
}

func (c *refRandom) next() [][]tract.Item {
	if c.dst != nil {
		sortTractsRef(c.dst)
	}
	n := c.src.NItems
	if c.dst == nil {
		c.dst = c.src.clone()
	}
	if c.ifrq == nil {
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
		z := uint64(c.total)
		m := n
		for i := range t {
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
	var z int64
	for _, d := range dif {
		if d > 0 {
			z += d
		}
	}
	for k := len(c.dst) - 1; k >= 0 && z > 0; k-- {
		t := c.dst[k]
		d := -1
		for p, s := range t {
			mark[s] = true
			if dif[s] > 0 {
				d = p
			}
		}
		if d >= 0 {
			i := int(tract.Item(c.rng.Float64()*float64(n))) % n
			j := (i + n - 1) % n
			for i != j && (mark[i] || dif[i] >= 0) {
				i = (i + 1) % n
			}
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
	return c.dst
}

// skewedData returns cnt transactions over n items with sizes up to
// maxSize and strongly skewed item frequencies (some items never occur),
// so that draws reach deep into the item buffer and repairs happen.
func skewedData(r *rand.Rand, n, cnt, maxSize int) *Data {
	d := &Data{NItems: n}
	for range cnt {
		size := r.IntN(min(maxSize, n) + 1)
		seen := map[tract.Item]bool{}
		var t []tract.Item
		for len(t) < size {
			i := tract.Item(min(int(r.ExpFloat64()*float64(n)/4), n-1))
			if r.IntN(8) == 0 {
				i = tract.Item(r.IntN(n))
			}
			if !seen[i] {
				seen[i] = true
				t = append(t, i)
			}
		}
		d.Tracts = append(d.Tracts, t)
	}
	sortTracts(d.Tracts)
	return d
}

// TestRandom_matchesLinear checks that the transaction randomization
// (prefix-sum tree for the draws, bit set for the repair, issue #39)
// generates exactly the surrogates of the linear searches of upstream's
// tbg_random, over many shapes: one item, items that never occur, empty
// transactions, transactions of all items, and long chains.
func TestRandom_matchesLinear(t *testing.T) {
	r := rand.New(rand.NewPCG(39, 1))
	runs := 0
	for it := range 400 {
		n := 1 + r.IntN(150)
		if it%10 == 0 {
			n = 1 + r.IntN(4)
		}
		d := skewedData(r, n, r.IntN(120), 1+r.IntN(12))
		seed := r.Uint32()
		ch := NewChain(d, Random, seed)
		ref := &refRandom{src: d}
		ref.rng.Seed(seed)
		for k := range 1 + r.IntN(6) {
			got, want := ch.Next(), ref.next()
			if !slices.EqualFunc(got, want, slices.Equal[[]tract.Item]) {
				t.Fatalf("case %d (n %d, %d transactions, seed %d), surrogate %d differs", it, n, d.Len(), seed, k)
			}
			runs++
		}
		if ch.rng != ref.rng {
			t.Fatalf("case %d: random number generators differ", it)
		}
	}
	t.Logf("%d surrogates compared", runs)
}

// sortTractsRef is the plain form of sortTracts: items ascending, then
// transactions in lexicographic order with a proper prefix first.
func sortTractsRef(ts [][]tract.Item) {
	for _, t := range ts {
		slices.Sort(t)
	}
	slices.SortFunc(ts, slices.Compare[[]tract.Item])
}

// TestSortTracts_matchesRef checks the keyed sort of the transactions
// against the plain comparison sort: empty transactions, one-item
// transactions, equal first items, equal prefixes and item codes near
// 2^31.
func TestSortTracts_matchesRef(t *testing.T) {
	r := rand.New(rand.NewPCG(39, 2))
	for it := range 300 {
		n := 1 + r.IntN(6)
		var ts [][]tract.Item
		for range r.IntN(200) {
			size := r.IntN(6)
			x := make([]tract.Item, size)
			for k := range x {
				x[k] = tract.Item(r.IntN(n))
				if it%7 == 0 {
					x[k] = math.MaxInt32 - x[k]
				}
			}
			ts = append(ts, slices.Compact(slices.Sorted(slices.Values(x))))
			r.Shuffle(len(ts[len(ts)-1]), func(i, j int) { ts[len(ts)-1][i], ts[len(ts)-1][j] = ts[len(ts)-1][j], ts[len(ts)-1][i] })
		}
		want := make([][]tract.Item, len(ts))
		for k, x := range ts {
			want[k] = slices.Clone(x)
		}
		sortTractsRef(want)
		var s tractSorter
		for range 2 { // also with the buffer of the first call
			got := slices.Clone(ts)
			s.sort(got)
			if !slices.EqualFunc(got, want, slices.Equal[[]tract.Item]) {
				t.Fatalf("case %d: %v, want %v", it, got, want)
			}
		}
	}
}
