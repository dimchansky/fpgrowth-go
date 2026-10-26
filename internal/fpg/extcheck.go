package fpg

import (
	"math"
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
// The items of every transaction are stored (horizontal form), and for
// every item the bit set of its transactions (vertical form), in compact
// form: the indices of its non-zero words and, for a sparse item, their
// values; a dense item has a full row of words instead of the values,
// for direct access. Every item is dense if the full rows need at most 4
// times the memory of the horizontal form's items, else the items with
// non-zero words in at least half of the words. The
// vertical form needs at most 20 bytes per item occurrence, i.e. 5 times
// the horizontal form's items (docs/m5-investigation.md §5). The cover
// of X is the AND of the bit sets of X, kept in the same compact form;
// an AND with a sparse item merges the two word lists if their lengths
// are within a factor of 4, else visits the shorter one and searches the
// other. A check then either scans the cover transactions
// (horizontal: a closed check intersects them and stops as soon as no
// candidate is left, a maximal check counts supports and stops at the
// first frequent extension) or tests every item against the cover
// (vertical: a subset test, resp. a weighted population count, over the
// non-zero cover words), whichever is estimated to be cheaper. A
// maximal check of a set whose prefix has a parent may instead use the
// parent's sibling rows (extsib.go), the same supports computed over the
// parent's cover.
type extChecker struct {
	maximal bool
	smin    tract.Supp
	freq    []tract.Supp // item frequencies (weight of the transactions)
	order   []tract.Item // items by descending frequency

	txOff   []int        // items of transaction t: txItems[txOff[t]:txOff[t+1]]
	txItems []tract.Item // ascending codes per transaction
	txW     []tract.Supp // positive transaction weights

	// Bit sets: word w holds transactions 64w..64w+63. Item i has its
	// non-zero words at the indices nzIdx[nzOff[i]:nzOff[i+1]]
	// (ascending); their values are rows[rowOf[i]*words+w] for a dense
	// item (rowOf[i] >= 0), else swW[swOff[i]:swOff[i+1]].
	words  int
	nzOff  []int
	nzIdx  []int32
	rowOf  []int32
	rows   []uint64
	swOff  []int
	swW    []uint64
	planes []uint64 // weight bit b: planes[b*words:]; nil: all weights 1
	// The cover of the checked set: its non-zero words cover[i], at word
	// index nz[i]. They alias a level of path or the scratch below.
	nz    []int32
	cover []uint64

	// path[k] is the cover of the first k+1 items of the reporter's
	// current prefix (checkPath).
	path  []coverLevel
	depth int // number of valid levels of path

	scratch [2]coverLevel // bitCover

	// Scratch state: stamps avoid clearing per call.
	inSet    []uint32     // inSet[i] == setStamp: i is in the checked set
	cnt      []tract.Supp // maximal: supp(X ∪ {i}) so far
	cand     []tract.Item // candidates / touched items
	deferred []int32      // scanMaximal: deferred long transactions
	alive    []tract.Item // scanMaximal: candidates of the deferred transactions
	setStamp uint32
	setLen   int // items of the checked set (len(set) of markSet)

	scanParams           // thresholds of scanClosed
	scanStat   scanStats // use of the paths of scanClosed

	// Sibling rows of the maximal check (extsib.go), one per path level.
	sibs      []sibLevel
	sib       sibHook
	sibStat   sibStats
	nFreq     int          // items with freq >= smin
	freqBelow []int32      // freqBelow[i]: items with code < i and freq >= smin
	sibCap    int          // words the sibling rows of all levels may hold
	sibSlot   []int32      // build scratch: row number + 1 per item
	sibSupp   []tract.Supp // build scratch: supp(P ∪ {i})
	sibTouch  []tract.Item // build scratch: items with a row, in touch order
	work      int64        // actual work of the last ordinary maximal check
	left      int          // miner's bound of the siblings after the set (-1: unknown)
	zmax      int          // maximum size of the search (sibling-row cost rule)
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

// rowMode selects the dense/sparse form of the item bit sets.
type rowMode uint8

const (
	rowsAuto   rowMode = iota // see makeBits
	rowsSparse                // every item sparse (tests)
	rowsMixed                 // even items dense, odd items sparse (tests)
)

// newExtChecker builds the checker for target (closed or maximal) from
// bag, whose items are codes 0..bag.NumItems()-1 (packed entries are
// unpacked). smin is the absolute minimum support. rows selects the
// form of the bit sets (rowsAuto except in tests). abort is the
// cancellation check, called every buildCheckEvery transactions.
func newExtChecker(bag *tract.Bag, target report.Target, smin tract.Supp,
	rows rowMode, abort func() error,
) (*extChecker, error) {
	k := bag.NumItems()
	c := &extChecker{
		maximal: target == report.TargetMaximal,
		smin:    smin,
		freq:    make([]tract.Supp, k),
		inSet:   make([]uint32, k),
		cnt:     make([]tract.Supp, k),

		scanParams: scanDefaults(),
	}
	c.txItems = make([]tract.Item, 0, presize(bag.Extent()))
	c.txOff = append(make([]int, 0, bag.Len()+1), 0)
	c.txW = make([]tract.Supp, 0, bag.Len())
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
	c.makeBits(rows)
	for _, f := range c.freq {
		if f >= smin {
			c.nFreq++
		}
	}
	c.sibCap = sibBudget * len(c.txItems)
	c.left = -1
	if c.maximal {
		c.sibSlot = make([]int32, k)
		c.sibSupp = make([]tract.Supp, k)
		c.freqBelow = make([]int32, k+1)
		for i, f := range c.freq {
			c.freqBelow[i+1] = c.freqBelow[i]
			if f >= smin {
				c.freqBelow[i+1]++
			}
		}
	}
	return c, nil
}

// makeBits builds the item bit sets (two passes over the transactions:
// count the non-zero words per item, then fill them) and the weight
// planes. Memory: 4 bytes per non-zero word for the index; if every item
// is dense, words*8 per item, at most 16 bytes per item occurrence in
// all; else 8 per non-zero word of a sparse item and words*8 per dense
// item (at most 16 per non-zero word). So at most 20 bytes per item
// occurrence.
func (c *extChecker) makeBits(mode rowMode) {
	k := len(c.freq)
	words := (len(c.txW) + 63) / 64
	c.words = words
	last := make([]int32, k) // word index + 1 of the item's last transaction
	c.nzOff = make([]int, k+1)
	for t := range c.txW {
		w := int32(t/64) + 1
		for _, it := range c.txItems[c.txOff[t]:c.txOff[t+1]] {
			if last[it] != w {
				last[it] = w
				c.nzOff[it+1]++
			}
		}
	}
	c.rowOf = make([]int32, k)
	c.swOff = make([]int, k+1)
	// Full rows for every item if they need at most 4 times the
	// horizontal form's items (4 bytes each): direct access throughout.
	all := int64(k)*int64(words)*8 <= 16*int64(len(c.txItems))
	dense := 0
	for i := range k {
		n := c.nzOff[i+1]
		var d bool
		switch mode {
		case rowsAuto:
			d = all || 2*n >= words
		case rowsMixed:
			d = i%2 == 0
		}
		c.swOff[i+1] = c.swOff[i]
		if d {
			c.rowOf[i] = int32(dense)
			dense++
		} else {
			c.rowOf[i] = -1
			c.swOff[i+1] += n
		}
		c.nzOff[i+1] += c.nzOff[i]
	}
	c.nzIdx = make([]int32, c.nzOff[k])
	c.swW = make([]uint64, c.swOff[k])
	c.rows = make([]uint64, dense*words)
	pos := make([]int, k) // non-zero words of item i so far
	clear(last)
	for t := range c.txW {
		w := int32(t/64) + 1
		bit := uint64(1) << (t % 64)
		for _, it := range c.txItems[c.txOff[t]:c.txOff[t+1]] {
			if last[it] != w {
				last[it] = w
				c.nzIdx[c.nzOff[it]+pos[it]] = w - 1
				pos[it]++
			}
			if r := c.rowOf[it]; r >= 0 {
				c.rows[int(r)*words+int(w-1)] |= bit
			} else {
				c.swW[c.swOff[it]+pos[it]-1] |= bit
			}
		}
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

// nzOf returns the indices of the non-zero words of item i.
func (c *extChecker) nzOf(i tract.Item) []int32 {
	return c.nzIdx[c.nzOff[i]:c.nzOff[i+1]]
}

// row returns the full row of a dense item i, nil for a sparse one.
func (c *extChecker) row(i tract.Item) []uint64 {
	r := int(c.rowOf[i])
	if r < 0 {
		return nil
	}
	return c.rows[r*c.words : (r+1)*c.words]
}

// sparseWords returns the non-zero words of a sparse item i (parallel to
// nzOf(i)).
func (c *extChecker) sparseWords(i tract.Item) []uint64 {
	return c.swW[c.swOff[i]:c.swOff[i+1]]
}

// seek returns the smallest index j >= i with idx[j] >= w (len(idx) if
// there is none), searching exponentially from i (idx ascending).
func seek[T ~int32](idx []T, i int, w T) int {
	if i >= len(idx) || idx[i] >= w {
		return i
	}
	// idx[i] < w: gallop to an upper end, then bisect (lo, hi]
	lo, step := i, 1
	hi := lo + step
	for hi < len(idx) && idx[hi] < w {
		lo = hi
		step <<= 1
		hi = lo + step
	}
	hi = min(hi, len(idx))
	for lo+1 < hi { // idx[lo] < w, hi == len(idx) or idx[hi] >= w
		m := int(uint(lo+hi) >> 1)
		if idx[m] < w {
			lo = m
		} else {
			hi = m
		}
	}
	return hi
}

// load sets lv to the bit set of item.
func (c *extChecker) load(lv *coverLevel, item tract.Item) {
	idx := c.nzOf(item)
	nz := append(lv.nz[:0], idx...)
	cover, n := lv.cover[:0], 0
	if b := c.row(item); b != nil {
		for _, w := range idx {
			x := b[w]
			cover = append(cover, x)
			n += bits.OnesCount64(x)
		}
	} else {
		for _, x := range c.sparseWords(item) {
			cover = append(cover, x)
			n += bits.OnesCount64(x)
		}
	}
	lv.nz, lv.cover, lv.n = nz, cover, n
}

// and sets dst to the cover a restricted to the transactions of item (dst
// and a must not share memory).
func (c *extChecker) and(dst, a *coverLevel, item tract.Item) {
	nz, cover, n := dst.nz[:0], dst.cover[:0], 0
	if b := c.row(item); b != nil {
		for i, w := range a.nz {
			if x := a.cover[i] & b[w]; x != 0 {
				nz, cover = append(nz, w), append(cover, x)
				n += bits.OnesCount64(x)
			}
		}
	} else {
		bnz, bw := c.nzOf(item), c.sparseWords(item)
		if la, lb := len(a.nz), len(bnz); 4*min(la, lb) >= max(la, lb) {
			i, j := 0, 0
			for i < la && j < lb {
				switch wa, wb := a.nz[i], bnz[j]; {
				case wa < wb:
					i++
				case wa > wb:
					j++
				default:
					if x := a.cover[i] & bw[j]; x != 0 {
						nz, cover = append(nz, wa), append(cover, x)
						n += bits.OnesCount64(x)
					}
					i++
					j++
				}
			}
		} else if len(a.nz) <= len(bnz) {
			j := 0
			for i, w := range a.nz {
				if j = seek(bnz, j, w); j == len(bnz) {
					break
				}
				if bnz[j] == w {
					if x := a.cover[i] & bw[j]; x != 0 {
						nz, cover = append(nz, w), append(cover, x)
						n += bits.OnesCount64(x)
					}
				}
			}
		} else {
			i := 0
			for j, w := range bnz {
				if i = seek(a.nz, i, w); i == len(a.nz) {
					break
				}
				if a.nz[i] == w {
					if x := a.cover[i] & bw[j]; x != 0 {
						nz, cover = append(nz, w), append(cover, x)
						n += bits.OnesCount64(x)
					}
				}
			}
		}
	}
	dst.nz, dst.cover, dst.n = nz, cover, n
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
	return c.decide(c.bitCover(set), supp)
}

// checkPath is check for a set whose items are prefix, the items the
// reporter has added on the current search path, and their perfect
// extensions. A perfect extension is in every transaction of the
// prefix's cover (transactions of weight 0 are left out), so the cover
// of the set is the cover of prefix, which is computed incrementally:
// the covers of the prefix's leading items are kept from the previous
// call (a depth-first search changes only the end of the prefix), and
// each new item costs one AND with the level before it.
func (c *extChecker) checkPath(prefix, set []tract.Item, supp tract.Supp) bool {
	if len(prefix) == 0 {
		return c.check(set, supp)
	}
	k := 0 // levels that remain valid
	for k < min(c.depth, len(prefix)) && c.path[k].item == prefix[k] {
		k++
	}
	for len(c.path) < len(prefix) {
		c.path = append(c.path, coverLevel{})
	}
	c.sibReset(k) // the sibling rows of the recomputed levels are stale
	for ; k < len(prefix); k++ {
		lv := &c.path[k]
		lv.item = prefix[k]
		if k == 0 {
			c.load(lv, prefix[k])
		} else {
			c.and(lv, &c.path[k-1], prefix[k])
		}
	}
	c.depth = len(prefix)
	lv := &c.path[len(prefix)-1]
	c.nz, c.cover = lv.nz, lv.cover
	c.markSet(set)
	if c.maximal && len(prefix) >= 2 && c.sib.mode != sibOff {
		ok, done := c.sibCheck(prefix, lv.n)
		if done {
			return ok
		}
		c.work = 0
		ok = c.decide(lv.n, supp)
		if s := &c.sibs[len(prefix)-2]; !s.built {
			s.work += c.work
		}
		return ok
	}
	return c.decide(lv.n, supp)
}

// decide decides the marked set from its cover in c.nz and c.cover with
// n transactions.
func (c *extChecker) decide(n int, supp tract.Supp) bool {
	if h, v := c.costs(n); h <= v {
		if c.maximal {
			return c.scanMaximal(n)
		}
		return c.scanClosed(n)
	}
	if c.maximal {
		return c.vertMaximal()
	}
	return c.vertClosed(supp)
}

// costs returns decide's estimated costs of the marked set's check from
// its cover in c.nz with n transactions: scanning the cover transactions
// (item visits) versus testing every item against the non-zero cover
// words (word tests, times the weight planes for a maximal check).
func (c *extChecker) costs(n int) (h, v int) {
	h = n * (len(c.txItems)/max(len(c.txW), 1) + 1)
	v = len(c.order) * len(c.nz)
	if c.maximal && c.planes != nil {
		v *= len(c.planes) / c.words
	}
	return h, v
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
	c.setLen = len(set)
}

// bitCover computes the cover of set into c.nz and c.cover and returns
// the number of cover transactions: the bit set of the set's sparsest
// item ANDed with the others.
func (c *extChecker) bitCover(set []tract.Item) int {
	r := set[0]
	for _, it := range set[1:] {
		if c.nzOff[it+1]-c.nzOff[it] < c.nzOff[r+1]-c.nzOff[r] {
			r = it
		}
	}
	a, b := &c.scratch[0], &c.scratch[1]
	c.load(a, r)
	for _, it := range set {
		if it != r {
			c.and(b, a, it)
			a, b = b, a
		}
	}
	c.nz, c.cover = a.nz, a.cover
	return a.n
}

// scanClosed decides the marked set from its n cover transactions (the
// bits of c.cover, enumerated lazily): an item has the support of the
// set iff it is in every cover transaction, so the candidates are the
// items of one of them, intersected with all the others, and the set is
// closed as soon as no candidate is left.
//
// The transactions are numbered in bag order (lexicographic), where
// neighbors share most of their items, so candidates survive a scan in
// order for long. The scan therefore intersects the first transactions
// t_p of scanProbes cover words spread over the cover first, then all of
// them in order.
//
// The candidates come from a short cover transaction t*, not
// necessarily the first one t0 (issue #20, docs/m5-investigation.md §4):
// a long t0 followed by short transactions made every check copy t0.
// t* is the shortest of t0 and the t_p (stage a); if t* still has more
// than guard (4) candidates per cover transaction, one pass over the
// cover's transaction lengths finds the shortest cover transaction
// (stage b), which costs n steps, at most a quarter of copying t*.
// The intersection is the same for any start and any order, repeats
// change nothing, and t0, the t_p and t* are each intersected (t0 and
// the t_p before the full scan, which skips t0 and repeats the others),
// so the verdict is the one of the plain scan. The probes (probeCand)
// of t0 run first and unchanged, then those of t* if it differs: the
// probes of t* alone can miss a covering item that t0's find (they
// test the first items of another transaction; the adv5 trap of
// reference/bench). A long transaction is intersected with few
// candidates by exponential search (keepSeek).
func (c *extChecker) scanClosed(n int) bool {
	nz, cover := c.nz, c.cover
	if len(nz) == 0 {
		return true // an empty cover (support 0) cannot occur for a reported set
	}
	t0 := nz[0]*64 + int32(bits.TrailingZeros64(cover[0]))
	if c.probeCand(t0) {
		return false
	}
	// stage a: the shortest of t0 and the spread transactions
	var tp [scanProbes - 1]int32
	np := 0
	ts, ls := t0, c.txLen(t0)
	for p, last := 1, 0; p < scanProbes; p++ {
		if i := p * len(nz) / scanProbes; i != last { // small covers: words repeat
			t := nz[i]*64 + int32(bits.TrailingZeros64(cover[i]))
			tp[np] = t
			np++
			if l := c.txLen(t); l < ls {
				ts, ls = t, l
			}
			last = i
		}
	}
	// stage b: the shortest cover transaction, if t* is long for the cover
	if ls-c.setLen > c.guard*n {
		c.scanStat.stageB++
	stageB:
		for i, w := range nz {
			for x := cover[i]; x != 0; x &= x - 1 {
				t := w*64 + int32(bits.TrailingZeros64(x))
				if l := c.txLen(t); l < ls {
					ts, ls = t, l
					if l <= c.setLen {
						break stageB // t = X: no candidate (initCand)
					}
				}
			}
		}
	}
	if ts != t0 {
		c.scanStat.moved++
		if c.probeCand(ts) {
			return false
		}
	}
	c.initCand(ts)
	if ts != t0 && len(c.cand) > 0 {
		if c.gallop(t0) {
			c.keepSeek(t0)
		} else {
			c.keepCand(t0)
		}
	}
	for _, t := range tp[:np] {
		if len(c.cand) == 0 {
			break
		}
		if t == ts {
			continue
		}
		if c.gallop(t) {
			c.keepSeek(t)
		} else {
			c.keepCand(t)
		}
	}
	for i, w := range nz {
		x := cover[i]
		if i == 0 {
			x &= x - 1 // t0: intersected above (or it gave the candidates)
		}
		for ; x != 0 && len(c.cand) > 0; x &= x - 1 {
			t := w*64 + int32(bits.TrailingZeros64(x))
			if c.gallop(t) {
				c.keepSeek(t)
			} else {
				c.keepCand(t)
			}
		}
	}
	return len(c.cand) == 0
}

// txLen returns the number of items of transaction t.
func (c *extChecker) txLen(t int32) int {
	return c.txOff[t+1] - c.txOff[t]
}

// gallop reports whether transaction t is to be intersected with the
// candidates by exponential search (keepSeek) instead of a merge
// (keepCand). The merge walks the items of t up to the last candidate,
// the search costs about log(|t|/|cand|) steps per candidate, so the
// search is chosen if t has at least gallopMin items and the merge
// would walk more than k = gallopRatio·|cand| of them: the k-th item of
// t is below the last candidate (O(1); candidates clustered at the
// start of a long transaction keep the merge, PR #35 review P1). The
// decision is made by the callers so that keepCand stays small enough
// to be inlined.
func (c *extChecker) gallop(t int32) bool {
	l, k := c.txLen(t), c.gallopRatio*len(c.cand)
	if l < c.gallopMin || l < k {
		return false
	}
	return k == 0 || c.txItems[c.txOff[t]+k-1] < c.cand[len(c.cand)-1]
}

// scanParams are the thresholds of scanClosed, set from the constants
// scanGallopMin, scanGallopRatio and scanGuard when the checker is
// built (tests may change them on a checker).
type scanParams struct {
	gallopMin, gallopRatio int        // keepSeek: |t| >= gallopMin, walk > gallopRatio·|candidates|
	guard                  int        // stage b: more than guard candidates per cover transaction
	mguard                 int        // scanMaximal: defer transactions of more than mguard items per cover transaction (< 0: all, tests)
	lookup                 lookupMode // scanDeferred: search or count a deferred transaction (tests)
}

// scanDefaults returns the thresholds of a build: the production
// constants (extcheck_thresh.go) or, with the build tag fpgforce, the
// lowered ones of extcheck_force.go.
func scanDefaults() scanParams {
	return scanParams{gallopMin: scanGallopMin, gallopRatio: scanGallopRatio, guard: scanGuard, mguard: scanMaxGuard}
}

// scanStats counts the use of the paths of scanClosed (tests, docs).
type scanStats struct {
	moved   int // candidates from a transaction other than t0
	stageB  int // passes over the cover for the shortest transaction
	gallops int // intersections by exponential search
	// scanMaximal (issue #34)
	deferred int // checks with deferred long transactions
	lookups  int // deferred transactions searched for the candidates only
	capped   int // deferrals ended by the cap
}

// scanProbes is the number of spread cover transactions that scanClosed
// intersects first.
const scanProbes = 8

// candProbes is the number of candidates that probeCand tests against
// the cover.
const candProbes = 4

// probeCand reports whether one of the first candProbes items of the
// first cover transaction t that are not in the marked set is in every
// cover transaction (covers), which proves the set not closed. It probes
// only if t is long compared to the cover (more than candProbes items
// per non-zero cover word), so that the probes cost at most about as
// much as copying the candidates (initCand); otherwise it reports false.
//
// Exactness: the cover holds the transactions of positive weight that
// contain the set (weight 0 is left out when the checker is built), and
// supp(X ∪ {j}) = supp(X) iff j is in every one of them. A true result
// names such a j ∉ X, so X is not closed; a false result decides
// nothing, and scanClosed goes on with the full scan. In particular, if
// the cover is a single transaction t, the first item of t that is not
// in X is in the whole cover: the check stops after at most |X| + 1
// items of t (codes in t are distinct) instead of copying all of t, and
// if no item is left, t = X and initCand finds no candidate, so X is
// closed. This keeps a long single chain with a small maximum size
// linear (docs/m5-investigation.md §4).
func (c *extChecker) probeCand(t int32) bool {
	items := c.txItems[c.txOff[t]:c.txOff[t+1]]
	if len(items) <= candProbes*len(c.nz) {
		return false
	}
	p := 0
	for _, it := range items {
		if c.inSet[it] == c.setStamp {
			continue
		}
		if c.covers(it) {
			return true
		}
		if p++; p == candProbes {
			break
		}
	}
	return false
}

// initCand sets the candidates to the items of transaction t that are
// not in the marked set (ascending).
func (c *extChecker) initCand(t int32) {
	c.cand = c.cand[:0]
	for _, it := range c.txItems[c.txOff[t]:c.txOff[t+1]] {
		if c.inSet[it] != c.setStamp {
			c.cand = append(c.cand, it)
		}
	}
}

// keepCand keeps the candidates that occur in transaction t (a merge of
// the two ascending lists).
func (c *extChecker) keepCand(t int32) {
	items := c.txItems[c.txOff[t]:c.txOff[t+1]]
	n, i := 0, 0
	for _, it := range c.cand {
		for i < len(items) && items[i] < it {
			i++
		}
		if i == len(items) {
			break
		}
		if items[i] == it {
			c.cand[n] = it
			n++
		}
	}
	c.cand = c.cand[:n]
}

// keepSeek is keepCand for a transaction t that is long compared to the
// candidates: each candidate is searched exponentially from the position
// of the previous one, O(|cand|·log(|t|/|cand|)) instead of O(|t|).
func (c *extChecker) keepSeek(t int32) {
	c.scanStat.gallops++
	items := c.txItems[c.txOff[t]:c.txOff[t+1]]
	n, i := 0, 0
	for _, it := range c.cand {
		if i = seek(items, i, it); i == len(items) {
			break
		}
		if items[i] == it {
			c.cand[n] = it
			n++
		}
	}
	c.cand = c.cand[:n]
}

// scanMaximal decides the marked set from its n cover transactions (the
// bits of c.cover, enumerated lazily): it accumulates cnt[j], the weight
// of the visited cover transactions that contain j, and stops at the
// first frequent extension (cnt[j] >= smin).
//
// Long transactions are deferred (issue #34): a cover transaction of
// more than mguard items per cover transaction besides the set, and of
// weight below smin (a heavier one decides within |X| + 1 items), is
// visited after the others. Let rest be the weight of the cover
// transactions not yet visited. Every j has
//
//	cnt[j] <= supp(X ∪ {j}) <= cnt[j] + rest
//
// (weights are positive), so once rest < smin only the touched items with
// cnt[j] >= smin − rest can be frequent extensions, and the deferred
// transactions are searched for these candidates only (scanDeferred).
// On a cover of one long line and short ones (the lp shapes of
// reference/bench) a check then costs the short lines instead of the
// long one.
//
// The visiting order and the choice of deferred transactions change only
// the cost, never the verdict: the counts are sums over distinct cover
// transactions, and an item is dropped only by the bound above. Without
// a deferred transaction the scan is the plain one, in cover order: the
// loop here is the plain scan plus one length test per transaction, and
// the scan continues in scanDefer at the first deferred transaction
// (with the cap's bookkeeping in this loop, ordinary maximal rows took
// up to 1.5 % more instructions; split, at most 0.15 %).
//
// Deferral cap: the plain scan may stop inside or soon after a long
// transaction t1 (the first deferred one), which the deferral skips. So
// the transactions counted after t1 while it is deferred may have at
// most |t1| items in all; the next one that would exceed that ends the
// deferral: the deferred transactions are counted in full, in cover
// order, and the scan goes on in cover order. The deferred ones are also
// visited in cover order at the end (scanDeferred). Let V be the item
// visits of the plain scan up to its exit, counting t1 in full (the
// plain scan visits all of t1 unless it stops inside it). The scan
// visits at most V + |t1| <= 2·V items: the cover transactions up to the
// plain scan's exit are all counted before the deferred ones after it
// (the deferred ones are taken in cover order, and the exit item reaches
// smin once all of these are counted, or earlier); beyond them it can
// only have counted the short transactions taken out of order, at most
// |t1| items. A search costs less than the full visit it replaces; the
// candidates add one pass over the touched items and their filters.
// (A cap on the total length of all deferred transactions instead of
// |t1| would not bound this: deferrals after the plain scan's exit
// would raise it.)
func (c *extChecker) scanMaximal(n int) bool {
	touched := c.cand[:0]
	long := c.deferLen(n)
	ok := true
	work := 0 // item visits (whole transactions), for the sibling rows
scan:
	for i, w := range c.nz {
		for x := c.cover[i]; x != 0; x &= x - 1 {
			t := w*64 + int32(bits.TrailingZeros64(x))
			wt := c.txW[t]
			l := c.txOff[t+1] - c.txOff[t]
			if l > long && wt < c.smin {
				// the first deferred transaction: the rest of the scan
				// (kept out of this loop, which stays the plain scan)
				ok, touched, work = c.scanDefer(i, x, long, touched, work)
				break scan
			}
			work += l
			for _, it := range c.txItems[c.txOff[t]:c.txOff[t+1]] {
				if c.inSet[it] == c.setStamp {
					continue
				}
				if c.cnt[it] == 0 {
					touched = append(touched, it)
				}
				if c.cnt[it] += wt; c.cnt[it] >= c.smin {
					ok = false // a frequent superset with one more item
					break scan
				}
			}
		}
	}
	for _, it := range touched {
		c.cnt[it] = 0
	}
	c.cand = touched
	c.work += int64(work)
	return ok
}

// scanDefer continues scanMaximal from its first deferred transaction t1,
// the lowest bit of x in cover word i: it defers t1 and the later long
// transactions of weight below smin, counts the others up to the cap
// (|t1| items in all), and either ends the deferral at the cap (the
// deferred ones are counted, then the scan goes on in cover order) or
// decides the deferred ones at the end (scanDeferred). It returns the
// verdict, touched and the work.
func (c *extChecker) scanDefer(i int, x uint64, long int, touched []tract.Item, work int) (bool, []tract.Item, int) {
	def := c.deferred[:0]
	var rest tract.Supp // weight of the deferred transactions
	budget := -1        // items the scan may count after t1 while it is deferred
	capped := false
	ok := true
scan:
	for ; i < len(c.nz); i++ {
		w := c.nz[i]
		if budget >= 0 || capped { // not t1's word: all its transactions
			x = c.cover[i]
		}
		for ; x != 0; x &= x - 1 {
			t := w*64 + int32(bits.TrailingZeros64(x))
			wt := c.txW[t]
			l := c.txOff[t+1] - c.txOff[t]
			if !capped {
				if l > long && wt < c.smin {
					if budget < 0 {
						budget = l // t1
					}
					def = append(def, t)
					rest += wt
					continue
				}
				if budget -= l; budget < 0 { // the cap: back to cover order
					c.scanStat.capped++
					capped = true
					for _, d := range def {
						work += c.txLen(d)
						var found bool
						if touched, found = c.countTx(d, touched); found {
							ok = false
							break scan
						}
					}
					def, rest = def[:0], 0
				}
			}
			work += l
			var found bool
			if touched, found = c.countTx(t, touched); found {
				ok = false // a frequent superset with one more item
				break scan
			}
		}
	}
	if ok && len(def) > 0 {
		ok, touched, work = c.scanDeferred(def, rest, touched, work)
	}
	c.deferred = def[:0]
	return ok, touched, work
}

// deferLen returns the length above which scanMaximal defers a light
// cover transaction of a cover of n transactions: |X| + mguard·n,
// saturated at the maximum int (no overflow with 32-bit int); a negative
// mguard (tests) defers every light transaction.
func (c *extChecker) deferLen(n int) int {
	switch {
	case c.mguard < 0:
		return -1
	case c.mguard > 0 && n > (math.MaxInt-c.setLen)/c.mguard:
		return math.MaxInt
	}
	return c.setLen + c.mguard*n
}

// countTx adds transaction t to the counts of scanMaximal (touched: the
// items with a count) and reports whether an item reached smin.
func (c *extChecker) countTx(t int32, touched []tract.Item) ([]tract.Item, bool) {
	wt := c.txW[t]
	for _, it := range c.txItems[c.txOff[t]:c.txOff[t+1]] {
		if c.inSet[it] == c.setStamp {
			continue
		}
		if c.cnt[it] == 0 {
			touched = append(touched, it)
		}
		if c.cnt[it] += wt; c.cnt[it] >= c.smin {
			return touched, true
		}
	}
	return touched, false
}

// lookupMode selects how scanDeferred visits a deferred transaction once
// it has candidates.
type lookupMode uint8

const (
	lookupAuto   lookupMode = iota // search if cheaper than a full count (production)
	lookupAlways                   // always search the candidates (tests)
	lookupNever                    // always count in full (tests)
)

// scanDeferred is the end of scanMaximal: the deferred transactions def
// (in cover order) of total weight rest, after all other cover
// transactions (counts in cnt, touched items in touched). It returns the
// verdict, touched and the work.
//
// While rest >= smin a deferred transaction is counted in full. At the
// first one with rest < smin (rest including it) the candidates are the
// touched items with cnt[j] >= smin − rest: every other item, touched or
// not, has supp(X ∪ {j}) <= cnt[j] + rest < smin. From then on each
// deferred transaction t either is searched for the candidates (binary
// search: the items of a transaction are ascending), if |candidates| ·
// bitlen(|t|) < |t|, or is counted in full (which adds to the candidates'
// counts as well); then rest drops by w(t) and the candidates are
// filtered by the same bound. No candidate left: X is maximal.
func (c *extChecker) scanDeferred(def []int32, rest tract.Supp, touched []tract.Item, work int) (bool, []tract.Item, int) {
	c.scanStat.deferred++
	alive := c.alive[:0]
	cands := false // alive holds the only possible frequent extensions
	ok := true
	for _, t := range def {
		wt := c.txW[t]
		items := c.txItems[c.txOff[t]:c.txOff[t+1]]
		if !cands && rest < c.smin {
			for _, it := range touched {
				if c.cnt[it] >= c.smin-rest {
					alive = append(alive, it)
				}
			}
			cands = true
		}
		if cands && len(alive) == 0 {
			break // ok: no frequent extension is possible
		}
		cost := len(alive) * bits.Len(uint(len(items)))
		if cands && (c.lookup == lookupAlways || c.lookup == lookupAuto && cost < len(items)) {
			c.scanStat.lookups++
			work += cost
			for _, it := range alive {
				if _, in := slices.BinarySearch(items, it); in {
					if c.cnt[it] += wt; c.cnt[it] >= c.smin {
						ok = false
						break
					}
				}
			}
		} else {
			work += len(items)
			var found bool
			if touched, found = c.countTx(t, touched); found {
				ok = false
			}
		}
		if !ok {
			break
		}
		rest -= wt
		if cands {
			k := 0
			for _, it := range alive {
				if c.cnt[it] >= c.smin-rest {
					alive[k] = it
					k++
				}
			}
			alive = alive[:k]
		}
	}
	c.alive = alive[:0]
	return ok, touched, work
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
		var sub bool
		if b := c.row(j); b != nil { // covers(j), inlined for dense rows
			sub = true
			for i, w := range c.nz {
				if c.cover[i]&^b[w] != 0 {
					sub = false
					break
				}
			}
		} else {
			sub = c.covers(j)
		}
		if sub {
			return false // j is in every transaction of the cover
		}
	}
	return true
}

// covers reports whether item j is in every transaction of the cover.
func (c *extChecker) covers(j tract.Item) bool {
	if b := c.row(j); b != nil {
		for i, w := range c.nz {
			if c.cover[i]&^b[w] != 0 {
				return false
			}
		}
		return true
	}
	bnz := c.nzOf(j)
	if len(bnz) < len(c.nz) {
		return false // a non-zero cover word that is zero for j
	}
	bw, k := c.sparseWords(j), 0
	for i, w := range c.nz {
		if k = seek(bnz, k, w); k == len(bnz) || bnz[k] != w || c.cover[i]&^bw[k] != 0 {
			return false
		}
	}
	return true
}

// vertMaximal computes supp(X ∪ {j}) for every frequent item j (most
// frequent first) by weighted population counts over the cover.
func (c *extChecker) vertMaximal() bool {
	pl := int64(1) // word tests per item and cover word
	if c.planes != nil {
		pl = int64(len(c.planes) / c.words)
	}
	for _, j := range c.order {
		if c.freq[j] < c.smin {
			return true // the remaining items are infrequent
		}
		if c.inSet[j] == c.setStamp {
			continue
		}
		c.work += int64(len(c.nz)) * pl
		var s tract.Supp
		if b := c.row(j); b != nil && c.planes == nil { // suppWith(j), inlined
			n := 0
			for i, w := range c.nz {
				n += bits.OnesCount64(c.cover[i] & b[w])
			}
			s = tract.Supp(n)
		} else {
			s = c.suppWith(j)
		}
		if s >= c.smin {
			return false // a frequent superset with one more item
		}
	}
	return true
}

// suppWith returns the weight of the cover transactions that contain j.
func (c *extChecker) suppWith(j tract.Item) tract.Supp {
	var s tract.Supp
	if b := c.row(j); b != nil {
		if c.planes == nil {
			n := 0
			for i, w := range c.nz {
				n += bits.OnesCount64(c.cover[i] & b[w])
			}
			return tract.Supp(n)
		}
		for p := range len(c.planes) / c.words {
			pl := c.planes[p*c.words : (p+1)*c.words]
			n := 0
			for i, w := range c.nz {
				n += bits.OnesCount64(c.cover[i] & b[w] & pl[w])
			}
			s += tract.Supp(n) << p
		}
		return s
	}
	add := func(w int32, x uint64) {
		if c.planes == nil {
			s += tract.Supp(bits.OnesCount64(x))
			return
		}
		for p := range len(c.planes) / c.words {
			s += tract.Supp(bits.OnesCount64(x&c.planes[p*c.words+int(w)])) << p
		}
	}
	bnz, bw := c.nzOf(j), c.sparseWords(j)
	if len(c.nz) <= len(bnz) {
		k := 0
		for i, w := range c.nz {
			if k = seek(bnz, k, w); k == len(bnz) {
				break
			}
			if bnz[k] == w {
				add(w, c.cover[i]&bw[k])
			}
		}
	} else {
		i := 0
		for k, w := range bnz {
			if i = seek(c.nz, i, w); i == len(c.nz) {
				break
			}
			if c.nz[i] == w {
				add(w, c.cover[i]&bw[k])
			}
		}
	}
	return s
}
