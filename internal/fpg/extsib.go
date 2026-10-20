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
// whenever checkPath recomputes a level ≤ L. They are built when the
// actual work of the children's ordinary checks at that level (item
// visits of scanMaximal, word tests of vertMaximal) reaches half the
// build cost, so building (with the pass that computes its cost) costs
// at most 2.25 times the work already spent there (ski rental): a level
// whose rows never pay off costs at most 3.25 times its ordinary checks.
// After the build a child uses the rows only if they are estimated
// cheaper than its ordinary check. The rows of all levels together stay
// within sibBudget words per item occurrence of the bag.

// sibMode selects when sibling rows are built.
type sibMode uint8

const (
	sibAuto  sibMode = iota // by actual work and cost (production)
	sibOff                  // never (tests)
	sibForce                // at the first child, without gates (tests)
	sibAt                   // at child number sibHook.at of a parent, without gates (tests)
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
	kids    int   // children checked so far (sibAt)
	words   int   // words per row: ⌈|cover|/64⌉
	ents    []sibEnt
	rows    []uint64 // row r at rows[r*words:], r in touch order
	planes  []uint64 // weight bit b at planes[b*words:]; empty if unweighted
}

// sibEnt is an item with a row: its offset in rows and supp(P ∪ {item}).
type sibEnt struct {
	item tract.Item
	off  int32
	supp tract.Supp
}

// sibStats counts the use of the sibling rows (tests, docs).
type sibStats struct {
	builds, multiWord, weighted, checks, refused int
	peakWords                                    int // peak words of all levels
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
	pl := 1
	if c.planes != nil {
		pl = len(c.planes) / c.words
	}
	// today's estimated cost of the child's check (decide)
	est := int64(min(n*(len(c.txItems)/max(len(c.txW), 1)+1), len(c.order)*len(c.nz)*pl))
	if !s.built && !s.refused {
		var build bool
		switch c.sib.mode {
		case sibForce:
			build = true
		case sibAt:
			build = s.kids >= c.sib.at
		case sibAuto:
			build = c.sibWorth(L, s, est, pl)
		}
		if build {
			c.sibBuild(L, prefix)
		}
	}
	s.kids++
	if !s.built {
		return false, false
	}
	gated := c.sib.mode == sibAuto
	if gated && int64(len(s.ents))*int64(s.words)*int64(pl) > est {
		return false, false // the ordinary check is estimated cheaper
	}
	got, found := c.sibMaximal(s, prefix[L+1])
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

// sibWorth reports whether the rows of level L should be built now
// (sibAuto): the actual work spent on the level's children reached half
// the build cost, the rows are cheaper per child than the child's
// estimated ordinary check, and they fit the memory budget.
func (c *extChecker) sibWorth(L int, s *sibLevel, est int64, pl int) bool {
	lv := &c.path[L]
	words := int64((lv.n + 63) / 64)
	if int64(c.nFreq)*words*int64(pl) >= est {
		return false // no gain per child
	}
	// The exact build cost needs one pass over the lengths of the cover
	// transactions (n steps). It is computed only once the work reached
	// 4·(L+1)·n, so that it costs at most a quarter of the work; every
	// cover transaction contains the L+1 items of P, so the build cost
	// is at least (L+1)·n and this delays no build by more than a factor
	// of 2 in work.
	if s.work < 4*int64(L+1)*int64(lv.n) {
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
		s.need = visits + int64(c.nFreq+pl)*words // item visits and cleared words
	}
	if 2*s.work < s.need {
		return false
	}
	want := int(int64(c.nFreq+pl) * words)
	if c.sibWords()-cap(s.rows)-cap(s.planes)+want > c.sibCap {
		// release the rows of the levels that hold none, then retry
		for i := range c.sibs {
			if t := &c.sibs[i]; !t.built && i != L {
				t.rows, t.planes = nil, nil
			}
		}
		if c.sibWords()-cap(s.rows)-cap(s.planes)+want > c.sibCap {
			s.refused = true
			c.sibStat.refused++
			return false
		}
	}
	return true
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
			s.ents = append(s.ents, sibEnt{item: it, off: int32(k * words), supp: supp[it]})
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
			rh = s.rows[e.off : int(e.off)+w]
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
		sp := s.supp(rh, s.rows[e.off:int(e.off)+w])
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
