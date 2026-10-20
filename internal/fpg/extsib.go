package fpg

import (
	"fmt"
	"math/bits"
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Sibling rows of the maximal extension check (issue #19,
// docs/m5-investigation.md §4.1).
//
// The sets checked by checkPath under one parent P (the prefix without
// its last item, at path level L) are the children X = P ∪ {h} ∪ pex,
// and cover(X) = cover(P ∪ {h}) ⊆ cover(P). For a maximal check each of
// them computes supp(X ∪ {j}) over its cover for every item j; on dense
// data with many children per parent these scans dominate the run. The
// sibling rows re-index cover(P): its transactions t_0 < t_1 < ... get
// the positions 0, 1, ..., and every item j ∉ P with supp(P ∪ {j}) ≥ smin
// gets the bit set R_j of the positions of the cover transactions that
// contain it (plus the positions' weight bit planes if some weight is
// above 1). A child's cover is R_h, and supp(X ∪ {j}) is the (weighted)
// population count of R_h & R_j, over ⌈|cover(P)|/64⌉ words. The
// position map is a bijection onto cover(P), so these are the same sums
// over the same transactions as suppWith; an item without a row has
// supp(X ∪ {j}) ≤ supp(P ∪ {j}) < smin (weights are not negative). The
// verdict is therefore the complete check's, item by item.
//
// The rows of level L are a function of path[L] only and are dropped
// whenever checkPath recomputes a level ≤ L. Whether they are built is a
// cost decision (sibWorth), in units of item visits and word tests (a
// build visit counts like a scan visit; on the M5A-1 target they cost
// about the same time, 1.9 and 2.1 ns): the children's ordinary checks
// at that level count their actual work (scanMaximal, vertMaximal), and
// the rows are built only if
//
//   - that work reached half the build cost B (so a build never costs
//     more than twice the work already spent at the level), and
//   - the children that can still follow, including the current one,
//     would save at least B: their number times (the average actual work
//     of a child so far − the rows' cost per child) ≥ B.
//
// The number of children after h is bounded by the miner (c.left: the
// entries left in its loop over the parent's conditional items, or the
// items of the 16-items machine that reports the set) and by the
// frequent items with a code below h (freqBelow[h]; the children of P
// are checked in descending code order in all variants,
// docs/m5-investigation.md §3.3). Both are upper bounds; a wrong bound
// could only make the cost decision worse, never a verdict. After the build a child uses the rows only if they are
// estimated cheaper than its ordinary check. The rows of all levels
// together stay within sibCap words (sibBudget per item occurrence).

// sibMode selects when sibling rows are built.
type sibMode uint8

const (
	sibAuto  sibMode = iota // by actual work and cost (production)
	sibOff                  // never (tests)
	sibForce                // at the first child, without cost gates (tests)
	sibAt                   // at child number sibHook.at of a parent, without cost gates (tests)
)

// sibHook are the test settings of the sibling rows.
type sibHook struct {
	mode sibMode
	at   int // sibAt: the child (0-based) at which the rows are built
	// verify also runs the ordinary check for every check answered from
	// sibling rows and panics if the verdicts differ.
	verify bool
}

// sibBudget is the memory budget of the sibling rows of all levels, in
// 64-bit words per item occurrence of the checker's horizontal form
// (16 bytes per occurrence, 4 times the horizontal items).
const sibBudget = 2

// sibLevel holds the sibling rows of one path level.
type sibLevel struct {
	built   bool
	refused bool  // over the memory budget until the level is dropped
	work    int64 // actual work of the ordinary checks of children so far
	need    int64 // build cost (-1: not yet computed)
	kids    int   // children checked so far
	words   int   // words per row: ⌈|cover|/64⌉
	ents    []sibEnt
	rows    []uint64 // row r at rows[r*words:], r in touch order
	planes  []uint64 // weight bit b at planes[b*words:]; empty if unweighted
}

// sibEnt is an item with a row: its offset in rows and supp(P ∪ {item}).
type sibEnt struct {
	item tract.Item
	off  int
	supp tract.Supp
}

// sibStats counts the use of the sibling rows (tests, docs).
type sibStats struct {
	builds, multiWord, weighted, checks int
	refused                             int // builds refused by the memory budget
	released                            int // budget releases of other levels' buffers
	peakWords                           int // peak words of all levels
}

// sibReset drops the sibling rows of levels from and up (their path
// levels are recomputed or no longer on the path).
func (c *extChecker) sibReset(from int) {
	for L := from; L < len(c.sibs); L++ {
		s := &c.sibs[L]
		s.built, s.refused, s.work, s.need, s.kids = false, false, 0, -1, 0
		s.ents = s.ents[:0]
	}
}

// sibWords returns the words held by the rows of all levels.
func (c *extChecker) sibWords() int {
	n := 0
	for L := range c.sibs {
		n += cap(c.sibs[L].rows) + cap(c.sibs[L].planes)
	}
	return n
}

// sibPlanes returns the number of weight bit planes (0: unweighted).
func (c *extChecker) sibPlanes() int {
	if c.planes == nil {
		return 0
	}
	return len(c.planes) / c.words
}

// sibWant returns the words that rows of level L take at most (every
// frequent item and the planes, over the level's cover).
func (c *extChecker) sibWant(L int) int {
	return (c.nFreq + c.sibPlanes()) * ((c.path[L].n + 63) / 64)
}

// sibCheck decides the marked set, whose prefix is prefix (at least two
// items) and whose cover is path[len(prefix)-1] with n transactions,
// from the sibling rows of its parent level if they exist or are worth
// building now. ok is the verdict if done; otherwise the caller runs the
// ordinary check and adds its work (c.work) to the level.
func (c *extChecker) sibCheck(prefix []tract.Item, n int) (ok, done bool) {
	L := len(prefix) - 2
	for len(c.sibs) <= L {
		c.sibs = append(c.sibs, sibLevel{need: -1})
	}
	s := &c.sibs[L]
	pl := int64(max(c.sibPlanes(), 1))
	hc, vc := c.costs(n) // the child's ordinary check, estimated
	est := int64(min(hc, vc))
	h := prefix[L+1]
	if !s.built && !s.refused {
		var build bool
		switch c.sib.mode {
		case sibForce:
			build = true
		case sibAt:
			build = s.kids >= c.sib.at
		case sibAuto:
			build = c.sibWorth(L, s, est, pl, h)
		}
		if build && c.sibFits(L) {
			c.sibBuild(L, prefix)
		}
	}
	s.kids++
	if !s.built {
		return false, false
	}
	if c.sib.mode == sibAuto && int64(len(s.ents))*int64(s.words)*pl > est {
		return false, false // the ordinary check is estimated cheaper
	}
	got, found := c.sibMaximal(s, h)
	if !found {
		return false, false // cannot happen for a frequent child
	}
	c.sibStat.checks++
	if c.sib.verify {
		lv := &c.path[L+1]
		if want := c.decide(lv.n, 0); want != got {
			panic(fmt.Sprintf("sibling rows: prefix %v: maximal %v, ordinary check %v", prefix, got, want))
		}
	}
	return got, true
}

// sibWorth reports whether the rows of level L are worth building now
// (sibAuto) for the child h, whose ordinary check is estimated at est:
// see the cost rule at the top of this file.
func (c *extChecker) sibWorth(L int, s *sibLevel, est, pl int64, h tract.Item) bool {
	if s.kids == 0 {
		return false // no child's actual work known yet
	}
	lv := &c.path[L]
	words := int64((lv.n + 63) / 64)
	row := int64(c.nFreq) * words * pl // the rows' cost per child, at most
	if row >= est {
		return false // no gain per child
	}
	avg := s.work / int64(s.kids)
	rem := int(c.freqBelow[h]) // the children after h
	if c.left >= 0 {
		rem = min(rem, c.left)
	}
	gain := int64(rem+1) * (avg - row) // what the children left, h included, save
	// Every cover transaction contains the L+1 items of P, so B is at
	// least (L+1)·n. The exact B needs one pass over the lengths of the
	// cover transactions (n steps); it is computed only once the work
	// reached 4·(L+1)·n, so that it costs at most a quarter of the work.
	// That delays a build until the work reaches max(B/2, 4·(L+1)·n),
	// i.e. by up to a factor of 8 on transactions of about L+1 items.
	lo := int64(L+1) * int64(lv.n)
	if gain < lo || s.work < 4*lo {
		return false
	}
	if s.need < 0 {
		var visits int64
		for i, w := range lv.nz {
			for x := lv.cover[i]; x != 0; x &= x - 1 {
				t := w*64 + int32(bits.TrailingZeros64(x))
				visits += int64(c.txOff[t+1] - c.txOff[t])
			}
		}
		s.need = visits + (int64(c.nFreq)+pl)*words // item visits and cleared words
	}
	return 2*s.work >= s.need && gain >= s.need
}

// sibFits reports whether rows of level L fit the memory budget (every
// mode). If not, it releases the buffers of the other levels that hold
// no rows and retries; if they still do not fit, the level is refused
// until it is dropped.
func (c *extChecker) sibFits(L int) bool {
	s := &c.sibs[L]
	want := c.sibWant(L)
	if c.sibWords()-cap(s.rows)-cap(s.planes)+want <= c.sibCap {
		return true
	}
	for i := range c.sibs {
		if t := &c.sibs[i]; !t.built && i != L && cap(t.rows)+cap(t.planes) > 0 {
			t.rows, t.planes = nil, nil
			c.sibStat.released++
		}
	}
	if c.sibWords()-cap(s.rows)-cap(s.planes)+want <= c.sibCap {
		return true
	}
	s.refused = true
	c.sibStat.refused++
	return false
}

// sibBuild builds the sibling rows of level L (the parent prefix[:L+1])
// in one pass over the horizontal form of its cover.
func (c *extChecker) sibBuild(L int, prefix []tract.Item) {
	s, lv := &c.sibs[L], &c.path[L]
	words := (lv.n + 63) / 64
	np := 0
	if c.planes != nil {
		np = len(c.planes) / c.words
	}
	if need := c.nFreq * words; cap(s.rows) < need {
		s.rows = make([]uint64, need)
	} else {
		s.rows = s.rows[:need]
	}
	if cap(s.planes) < np*words {
		s.planes = make([]uint64, np*words)
	} else {
		s.planes = s.planes[:np*words]
		clear(s.planes)
	}
	slot, supp := c.sibSlot, c.sibSupp
	touched := c.sibTouch[:0]
	r := 0
	for i, w := range lv.nz {
		for x := lv.cover[i]; x != 0; x &= x - 1 {
			t := w*64 + int32(bits.TrailingZeros64(x))
			wt := c.txW[t]
			wi, bit := r>>6, uint64(1)<<(r&63)
			for _, it := range c.txItems[c.txOff[t]:c.txOff[t+1]] {
				if c.freq[it] < c.smin {
					continue // supp(P ∪ {it}) ≤ freq(it) < smin: never a row
				}
				k := slot[it]
				if k == 0 {
					touched = append(touched, it)
					k = int32(len(touched))
					slot[it] = k
					clear(s.rows[int(k-1)*words : int(k)*words])
				}
				s.rows[int(k-1)*words+wi] |= bit
				supp[it] += wt
			}
			for b := range np {
				if wt&(1<<b) != 0 {
					s.planes[b*words+wi] |= bit
				}
			}
			r++
		}
	}
	s.ents = s.ents[:0]
	for k, it := range touched {
		if supp[it] >= c.smin && !slices.Contains(prefix[:L+1], it) {
			s.ents = append(s.ents, sibEnt{item: it, off: k * words, supp: supp[it]})
		}
		slot[it], supp[it] = 0, 0 // reset every touched item, kept or not
	}
	c.sibTouch = touched
	// most frequent extensions first: a non-maximal child stops early
	slices.SortFunc(s.ents, func(a, b sibEnt) int {
		switch {
		case a.supp > b.supp:
			return -1
		case a.supp < b.supp:
			return 1
		}
		return int(a.item) - int(b.item)
	})
	s.words, s.built = words, true
	st := &c.sibStat
	st.builds++
	if words > 1 {
		st.multiWord++
	}
	if np > 0 {
		st.weighted++
	}
	st.peakWords = max(st.peakWords, c.sibWords())
}

// sibMaximal decides the marked set, a child P ∪ {h} ∪ pex of the
// level's parent P, from the rows: maximal iff supp(X ∪ {j}) < smin for
// every item j with a row that is not in the set. found is false if h
// has no row.
func (c *extChecker) sibMaximal(s *sibLevel, h tract.Item) (ok, found bool) {
	w := s.words
	var rh []uint64
	for _, e := range s.ents {
		if e.item == h {
			rh = s.rows[e.off : e.off+w]
			break
		}
	}
	if rh == nil {
		return false, false
	}
	for _, e := range s.ents {
		if c.inSet[e.item] == c.setStamp {
			continue // h, perfect extensions (pex of P have full rows)
		}
		sp := s.supp(rh, s.rows[e.off:e.off+w])
		if sp >= c.smin {
			return false, true // a frequent superset with one more item
		}
	}
	return true, true
}

// supp returns the weight of the positions in both rows a and b (the
// population count of a & b, by weight bit plane if weighted).
func (s *sibLevel) supp(a, b []uint64) tract.Supp {
	b = b[:len(a)]
	if len(s.planes) == 0 {
		n := 0
		for i, x := range a {
			n += bits.OnesCount64(x & b[i])
		}
		return tract.Supp(n)
	}
	var sp tract.Supp
	for p := range len(s.planes) / s.words {
		pb := s.planes[p*s.words : (p+1)*s.words]
		pb = pb[:len(a)]
		n := 0
		for i, x := range a {
			n += bits.OnesCount64(x & b[i] & pb[i])
		}
		sp += tract.Supp(n) << p
	}
	return sp
}
