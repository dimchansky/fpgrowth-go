package fpg

import (
	"math/bits"
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// extChecker decides closedness and maximality of an item set against
// every item of the transaction bag (deviation M5, fpgrowth-core.md B5).
//
// A search that is limited by the maximum size zmax cannot rely on the
// closed/maximal repository alone: the repository proves a set
// non-closed (non-maximal) only with a stored superset, and the supersets
// beyond the limit are never stored (docs/m5-investigation.md). The check
// is complete because closedness and maximality are decided by single
// items: X is closed iff supp(X ∪ {j}) < supp(X) for every item j ∉ X,
// and maximal iff supp(X ∪ {j}) < smin for every j ∉ X (supports are
// anti-monotone). It computes these supports from the bag itself, not
// from a conditional FP-tree, so items "above" the set in the search
// order, which a conditional tree never contains, are covered as well.
//
// Transactions of weight 0 are left out: they do not change any support,
// so with them gone supp(X ∪ {j}) = supp(X) holds iff j is in every
// transaction of the cover of X (the transactions that contain X).
//
// The items of every transaction are stored (horizontal form). The cover
// of X comes from one bit set of transactions per item (the AND of the
// bit sets of X), if the bit sets need at most denseFactor times the
// memory of the horizontal form or less than denseMinBytes; otherwise
// from the transaction list of the rarest item of X, filtered by
// containment. A check then either scans the cover transactions
// (horizontal: a closed check intersects them and stops as soon as no
// candidate is left, a maximal check counts supports and stops at the
// first frequent extension) or, with bit sets, tests every item against
// the cover (vertical: a subset test, resp. a weighted population count,
// over the non-zero cover words), whichever is estimated to be cheaper.
type extChecker struct {
	maximal bool
	smin    tract.Supp
	freq    []tract.Supp // item frequencies (weight of the transactions)
	order   []tract.Item // items by descending frequency

	txOff   []int        // items of transaction t: txItems[txOff[t]:txOff[t+1]]
	txItems []tract.Item // ascending codes per transaction
	txW     []tract.Supp // positive transaction weights

	// bit sets (words > 0)
	words  int
	bits   []uint64 // item i: bits[i*words : (i+1)*words]
	nzOff  []int    // non-zero words of item i: nzIdx[nzOff[i]:nzOff[i+1]]
	nzIdx  []int32
	planes []uint64 // weight bit b: planes[b*words:]; nil: all weights 1
	// The cover of the checked set: its non-zero words cover[i], at word
	// index nz[i]. They alias a level of path or the scratch below.
	nz    []int32
	cover []uint64

	// path[k] is the cover of the first k+1 items of the reporter's
	// current prefix (checkPath).
	path  []coverLevel
	depth int // number of valid levels of path

	nzBuf    []int32  // scratch: the cover computed by bitCover
	coverBuf []uint64 // (nz and cover)

	// transaction lists (words == 0)
	tidOff []int // transactions of item i: tids[tidOff[i]:tidOff[i+1]]
	tids   []int32

	// Scratch state: stamps avoid clearing per call.
	inSet    []uint32     // inSet[i] == setStamp: i is in the checked set
	seen     []uint32     // seen[i] == txStamp: i is in the current transaction
	cnt      []tract.Supp // maximal: supp(X ∪ {i}) so far
	cand     []tract.Item // candidates / touched items
	coverTxs []int32      // the cover transactions

	setStamp, txStamp uint32
}

// coverLevel is a cover in compact form: the non-zero words cover[i] of
// the transaction bit set, at word index nz[i], and the number n of
// transactions in it.
type coverLevel struct {
	item  tract.Item // the item that this level adds to the prefix
	nz    []int32
	cover []uint64
	n     int
}

// Size rule for the bit sets (see extChecker).
const (
	denseFactor   = 4
	denseMinBytes = 64 << 20
)

// newExtChecker builds the checker for target (closed or maximal) from
// bag, whose items are codes 0..bag.NumItems()-1 (packed entries are
// unpacked). smin is the absolute minimum support. noBits forces the
// transaction lists instead of bit sets (tests). abort is the
// cancellation check, called every buildCheckEvery transactions.
func newExtChecker(bag *tract.Bag, target report.Target, smin tract.Supp,
	noBits bool, abort func() error,
) (*extChecker, error) {
	k := bag.NumItems()
	c := &extChecker{
		maximal: target == report.TargetMaximal,
		smin:    smin,
		freq:    make([]tract.Supp, k),
		inSet:   make([]uint32, k),
		seen:    make([]uint32, k),
		cnt:     make([]tract.Supp, k),
	}
	c.txItems = make([]tract.Item, 0, presize(bag.Extent()))
	c.txOff = append(make([]int, 0, bag.Len()+1), 0)
	c.txW = make([]tract.Supp, 0, bag.Len())
	occ := make([]int, k+1) // occurrences per item, then offsets
	nt := 0
	for items, w := range bag.All() {
		if nt++; nt%buildCheckEvery == 0 {
			if err := abort(); err != nil {
				return nil, err
			}
		}
		if w <= 0 {
			continue // weight 0: no effect on any support
		}
		for _, it := range items {
			if it.IsPacked() {
				for b := tract.Item(0); b < 31; b++ {
					if it.PackedBits()&(1<<b) != 0 {
						c.txItems = append(c.txItems, b)
					}
				}
				continue
			}
			c.txItems = append(c.txItems, it)
		}
		for _, it := range c.txItems[c.txOff[len(c.txOff)-1]:] {
			occ[it+1]++
			c.freq[it] += w
		}
		c.txOff = append(c.txOff, len(c.txItems))
		c.txW = append(c.txW, w)
	}
	c.order = make([]tract.Item, k)
	for i := range c.order {
		c.order[i] = tract.Item(i)
	}
	slices.SortStableFunc(c.order, func(a, b tract.Item) int {
		switch {
		case c.freq[a] > c.freq[b]:
			return -1
		case c.freq[a] < c.freq[b]:
			return 1
		}
		return 0
	})
	words := (len(c.txW) + 63) / 64
	if b := int64(k) * int64(words) * 8; !noBits &&
		(b <= denseMinBytes || b <= denseFactor*4*int64(len(c.txItems))) {
		c.makeBits(words)
		return c, nil
	}
	for i := range k {
		occ[i+1] += occ[i]
	}
	c.tidOff = occ
	c.tids = make([]int32, occ[k])
	pos := make([]int, k)
	copy(pos, occ[:k])
	for t := range c.txW {
		for _, it := range c.txItems[c.txOff[t]:c.txOff[t+1]] {
			c.tids[pos[it]] = int32(t)
			pos[it]++
		}
	}
	return c, nil
}

// makeBits builds the item bit sets and the weight planes.
func (c *extChecker) makeBits(words int) {
	k := len(c.freq)
	c.words = words
	c.bits = make([]uint64, k*words)
	for t := range c.txW {
		for _, it := range c.txItems[c.txOff[t]:c.txOff[t+1]] {
			c.bits[int(it)*words+t/64] |= 1 << (t % 64)
		}
	}
	c.nzOff = make([]int, k+1)
	for i := range k {
		for w, x := range c.bits[i*words : (i+1)*words] {
			if x != 0 {
				c.nzIdx = append(c.nzIdx, int32(w))
			}
		}
		c.nzOff[i+1] = len(c.nzIdx)
	}
	maxW := slices.Max(append([]tract.Supp{1}, c.txW...))
	if maxW > 1 {
		n := bits.Len64(uint64(maxW))
		c.planes = make([]uint64, n*words)
		for t, w := range c.txW {
			for b := range n {
				if w&(1<<b) != 0 {
					c.planes[b*words+t/64] |= 1 << (t % 64)
				}
			}
		}
	}
}

// check is the report.CloMaxCheck of the miner: whether set (support
// supp) is closed, respectively maximal, with respect to every item.
func (c *extChecker) check(set []tract.Item, supp tract.Supp) bool {
	if len(set) == 0 { // the cover is the whole bag
		lim := supp // closed: an item in every transaction
		if c.maximal {
			lim = c.smin // maximal: any frequent item
		}
		for _, f := range c.freq {
			if f >= lim {
				return false
			}
		}
		return true
	}
	c.markSet(set)
	if c.words == 0 {
		c.listCover(set)
		return c.scanCover()
	}
	return c.decide(c.bitCover(set), supp)
}

// checkPath is check for a set whose items are prefix, the items the
// reporter has added on the current search path, and their perfect
// extensions. A perfect extension is in every transaction of the
// prefix's cover (transactions of weight 0 are left out), so the cover
// of the set is the cover of prefix, which is computed incrementally:
// the covers of the prefix's leading items are kept from the previous
// call (a depth-first search changes only the end of the prefix), and
// each new item costs one AND over the non-zero words of the level
// before it.
func (c *extChecker) checkPath(prefix, set []tract.Item, supp tract.Supp) bool {
	if len(prefix) == 0 || c.words == 0 {
		return c.check(set, supp)
	}
	k := 0 // levels that remain valid
	for k < min(c.depth, len(prefix)) && c.path[k].item == prefix[k] {
		k++
	}
	for len(c.path) < len(prefix) {
		c.path = append(c.path, coverLevel{})
	}
	for ; k < len(prefix); k++ {
		c.extend(k, prefix[k])
	}
	c.depth = len(prefix)
	lv := &c.path[len(prefix)-1]
	c.nz, c.cover = lv.nz, lv.cover
	c.markSet(set)
	return c.decide(lv.n, supp)
}

// extend computes level k of the path cache: the cover of level k-1 (all
// transactions for k = 0) restricted to the transactions of item.
func (c *extChecker) extend(k int, item tract.Item) {
	lv := &c.path[k]
	lv.item = item
	nz, cover, n := lv.nz[:0], lv.cover[:0], 0
	b := c.itemBits(item)
	if k == 0 {
		for _, w := range c.nzIdx[c.nzOff[item]:c.nzOff[item+1]] {
			x := b[w]
			nz, cover = append(nz, w), append(cover, x)
			n += bits.OnesCount64(x)
		}
	} else {
		prev := &c.path[k-1]
		for i, w := range prev.nz {
			if x := prev.cover[i] & b[w]; x != 0 {
				nz, cover = append(nz, w), append(cover, x)
				n += bits.OnesCount64(x)
			}
		}
	}
	lv.nz, lv.cover, lv.n = nz, cover, n
}

// decide decides the marked set from its cover in c.nz and c.cover with
// n transactions.
func (c *extChecker) decide(n int, supp tract.Supp) bool {
	// Estimated costs: scanning the cover transactions versus testing
	// every item against the non-zero cover words.
	h := n * (len(c.txItems)/max(len(c.txW), 1) + 1)
	v := len(c.order) * len(c.nz)
	if c.maximal && c.planes != nil {
		v *= len(c.planes) / c.words
	}
	if h <= v {
		c.coverTxs = c.coverTxs[:0]
		for i, w := range c.nz {
			for x := c.cover[i]; x != 0; x &= x - 1 {
				c.coverTxs = append(c.coverTxs, w*64+int32(bits.TrailingZeros64(x)))
			}
		}
		return c.scanCover()
	}
	if c.maximal {
		return c.vertMaximal()
	}
	return c.vertClosed(supp)
}

// markSet marks the items of set (inSet).
func (c *extChecker) markSet(set []tract.Item) {
	c.setStamp++
	if c.setStamp == 0 { // wrapped: reset the stamps
		clear(c.inSet)
		c.setStamp = 1
	}
	for _, it := range set {
		c.inSet[it] = c.setStamp
	}
}

// itemBits returns the bit set of item i.
func (c *extChecker) itemBits(i tract.Item) []uint64 {
	return c.bits[int(i)*c.words : (int(i)+1)*c.words]
}

// bitCover computes the cover of set into c.nz and c.cover and returns
// the number of cover transactions. Only the non-zero words of the set's
// sparsest item are visited.
func (c *extChecker) bitCover(set []tract.Item) int {
	r := set[0]
	for _, it := range set[1:] {
		if c.nzOff[it+1]-c.nzOff[it] < c.nzOff[r+1]-c.nzOff[r] {
			r = it
		}
	}
	nz, cover := c.nzBuf[:0], c.coverBuf[:0]
	n := 0
	rb := c.itemBits(r)
	for _, w := range c.nzIdx[c.nzOff[r]:c.nzOff[r+1]] {
		x := rb[w]
		for _, it := range set {
			x &= c.bits[int(it)*c.words+int(w)]
		}
		if x != 0 {
			nz, cover = append(nz, w), append(cover, x)
			n += bits.OnesCount64(x)
		}
	}
	c.nzBuf, c.coverBuf = nz, cover
	c.nz, c.cover = nz, cover
	return n
}

// listCover collects the cover of the marked set into c.coverTxs from
// the transaction list of its rarest item.
func (c *extChecker) listCover(set []tract.Item) {
	r := set[0]
	for _, it := range set[1:] {
		if c.tidOff[it+1]-c.tidOff[it] < c.tidOff[r+1]-c.tidOff[r] {
			r = it
		}
	}
	c.coverTxs = c.coverTxs[:0]
	for _, t := range c.tids[c.tidOff[r]:c.tidOff[r+1]] {
		n := len(set)
		for _, it := range c.txItems[c.txOff[t]:c.txOff[t+1]] {
			if c.inSet[it] == c.setStamp {
				n--
			}
		}
		if n == 0 {
			c.coverTxs = append(c.coverTxs, t)
		}
	}
}

// scanCover decides the marked set from its cover transactions
// c.coverTxs. Closed: an item has the support of the set iff it is in
// every cover transaction.
func (c *extChecker) scanCover() bool {
	if c.maximal {
		return c.scanMaximal()
	}
	for k, t := range c.coverTxs {
		items := c.txItems[c.txOff[t]:c.txOff[t+1]]
		if k == 0 { // candidates: the items of the first cover transaction
			c.cand = c.cand[:0]
			for _, it := range items {
				if c.inSet[it] != c.setStamp {
					c.cand = append(c.cand, it)
				}
			}
		} else { // keep the candidates that occur in t
			c.txStamp++
			if c.txStamp == 0 {
				clear(c.seen)
				c.txStamp = 1
			}
			for _, it := range items {
				c.seen[it] = c.txStamp
			}
			n := 0
			for _, it := range c.cand {
				if c.seen[it] == c.txStamp {
					c.cand[n] = it
					n++
				}
			}
			c.cand = c.cand[:n]
		}
		if len(c.cand) == 0 {
			return true // no item is in every cover transaction
		}
	}
	// An empty cover (support 0) cannot occur for a reported set.
	return len(c.coverTxs) == 0
}

// scanMaximal accumulates supp(X ∪ {j}) over the cover transactions and
// stops at the first frequent extension.
func (c *extChecker) scanMaximal() bool {
	touched := c.cand[:0]
	ok := true
scan:
	for _, t := range c.coverTxs {
		w := c.txW[t]
		for _, it := range c.txItems[c.txOff[t]:c.txOff[t+1]] {
			if c.inSet[it] == c.setStamp {
				continue
			}
			if c.cnt[it] == 0 {
				touched = append(touched, it)
			}
			if c.cnt[it] += w; c.cnt[it] >= c.smin {
				ok = false // a frequent superset with one more item
				break scan
			}
		}
	}
	for _, it := range touched {
		c.cnt[it] = 0
	}
	c.cand = touched
	return ok
}

// vertClosed tests every item at least as frequent as the set against the
// cover (most frequent first).
func (c *extChecker) vertClosed(supp tract.Supp) bool {
	for _, j := range c.order {
		if c.freq[j] < supp {
			return true // all remaining items are rarer than the set
		}
		if c.inSet[j] == c.setStamp {
			continue
		}
		b := c.itemBits(j)
		sub := true
		for i, w := range c.nz {
			if c.cover[i]&^b[w] != 0 {
				sub = false
				break
			}
		}
		if sub {
			return false // j is in every transaction of the cover
		}
	}
	return true
}

// vertMaximal computes supp(X ∪ {j}) for every frequent item j (most
// frequent first) by weighted population counts over the cover.
func (c *extChecker) vertMaximal() bool {
	for _, j := range c.order {
		if c.freq[j] < c.smin {
			return true // the remaining items are infrequent
		}
		if c.inSet[j] == c.setStamp {
			continue
		}
		b := c.itemBits(j)
		var s tract.Supp
		if c.planes == nil {
			n := 0
			for i, w := range c.nz {
				n += bits.OnesCount64(c.cover[i] & b[w])
			}
			s = tract.Supp(n)
		} else {
			for p := range len(c.planes) / c.words {
				pl := c.planes[p*c.words : (p+1)*c.words]
				n := 0
				for i, w := range c.nz {
					n += bits.OnesCount64(c.cover[i] & b[w] & pl[w])
				}
				s += tract.Supp(n) << p
			}
		}
		if s >= c.smin {
			return false // a frequent superset with one more item
		}
	}
	return true
}
