package istree

import (
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Filter is a port of ist_filter: evaluation-based pruning (option -p).
// It marks every set with at least two items that is infrequent or whose
// evaluation fails the threshold, then, by size:
//
//   - size < 0, weak forward pruning: from sets with max(-size, 2)+1 items
//     on (level by level, so marks propagate), a set is also marked if all
//     its subsets with one item less are marked;
//   - size > 0, strong forward pruning: from sets with max(size, 2)+1 items
//     on, a set is also marked if any subset with one item less is marked;
//   - size == 0, backward pruning: nothing else.
//
// Finally the empty set and all single items are marked if their constant
// evaluation fails the threshold. Marked sets are not reported as item
// sets; for closed, maximal and generator targets CloMax must then be
// called with safe set, which restores the subsets of the unmarked sets
// before deciding closedness on the remaining family. Rule reporting reads
// supports without the marks, as upstream's rules() does, so Filter does
// not change the reported rules.
//
// Deviation from upstream (bug B2, rules-eval.md §5): fpgrowth calls
// ist_filter before ist_seteval (so nothing but infrequent sets is marked)
// and only for prune <= 0. Callers of this package must call SetEval
// first, then Filter(prune) for any prune > ITEM_MIN when a measure is set,
// then CloMax(target, true) for closed/maximal/generator targets. A
// positive prune gets the strong filtering branch of ist_filter, which on a
// complete tree marks exactly the sets that apriori's level-wise
// ist_commit pruning (the only consumer of a positive prune upstream) would
// never generate; ist_commit itself has no equivalent on fpgrowth's path.
func (t *Tree) Filter(size int) {
	if !t.isLvl {
		t.makelvls()
	}
	for h := t.height - 1; h > 0; h-- {
		for _, ni := range t.lvls[h] {
			cnts := t.counters(&t.nodes[ni])
			for i := int32(len(cnts)) - 1; i >= 0; i-- {
				if cnts[i] < t.smin || t.dir*t.evaluate(ni, i) < t.thresh {
					cnts[i] |= skip
				}
			}
		}
	}
	switch {
	case size < 0:
		t.forward(max(-size, 2), false)
	case size > 0:
		t.forward(max(size, 2), true)
	}
	if ws := t.worstSigned(); ws < t.thresh {
		t.wgt |= skip
		for i := range t.counters(&t.nodes[0]) {
			t.cnts[i] |= skip
		}
	}
}

// worstSigned is upstream's ((ist->dir < 0) ? -1 : 0), the constant value of
// the empty set and the singletons multiplied by the direction.
func (t *Tree) worstSigned() float64 {
	if t.dir < 0 {
		return -1
	}
	return 0
}

// forward is the weak (isStrong false) and strong forward filtering of
// ist_filter, from level start on. A weak filter marks a set if no subset
// with one item less qualifies; a strong filter marks it if any does not.
func (t *Tree) forward(start int, isStrong bool) {
	end := len(t.buf)
	for h := start; h < t.height; h++ {
		for _, ni := range t.lvls[h] {
			nd := &t.nodes[ni]
			pn := &t.nodes[nd.parent]
			parentOK := t.cnts[pn.cnts+t.index(pn, nd.item)] >= t.smin
			cnts := t.counters(nd)
			if !isStrong && parentOK {
				continue // the subset without the deepest item qualifies
			}
			if isStrong && !parentOK {
				for i := range cnts {
					cnts[i] |= skip
				}
				continue
			}
			for i := int32(len(cnts)) - 1; i >= 0; i-- {
				pos := end - 2
				t.buf[pos], t.buf[pos+1] = nd.item, t.itemAt(nd, i)
				cur := nd.parent
				for ; cur >= 0; cur = t.nodes[cur].parent {
					ok := t.getsupp(cur, t.buf[pos+1:end]) >= t.smin
					if ok != isStrong {
						break // weak: a qualifying subset; strong: a failing one
					}
					pos--
					t.buf[pos] = t.nodes[cur].item
				}
				if (cur < 0) != isStrong {
					cnts[i] |= skip
				}
			}
		}
	}
}

// clear is a port of clear(): it clears the skip flag of the set formed by
// the path of node ni and items if its raw counter is at most supp (always
// for a marked counter, whose raw value is negative).
func (t *Tree) clear(ni int32, items []tract.Item, supp tract.Supp) {
	for ; len(items) > 1; items = items[1:] {
		ni = t.child(&t.nodes[ni], items[0])
		if ni < 0 {
			return // never on a downward closed tree (upstream: no check)
		}
	}
	nd := &t.nodes[ni]
	i := t.index(nd, items[0])
	if i < 0 {
		return
	}
	if c := &t.cnts[nd.cnts+i]; *c <= supp {
		*c &^= skip
	}
}

// CloMax is a port of ist_clomax for IST_REVERSE: it marks the sets that
// are not closed (target report.TargetClosed), not maximal
// (report.TargetMaximal) or not generators (report.TargetGenerators), as
// well as all infrequent sets. Sets marked before (by Filter) count as
// absent. With safe (IST_SAFE, upstream: any -p), the subsets of every
// unmarked set are unmarked first, which makes the family of unmarked sets
// downward closed again.
//
// Closedness and maximality are decided against the supersets stored in
// the tree, so the tree must hold one more level than the largest size to
// report (fpgrowth: SetSize(zmin, zmax+1)); sets on the deepest level count
// as closed and maximal.
func (t *Tree) CloMax(target report.Target, safe bool) {
	if !t.isLvl {
		t.makelvls()
	}
	isMax := target&report.TargetMaximal != 0
	if safe {
		t.restore(isMax)
	}
	if target&report.TargetGenerators != 0 {
		t.generators()
		return
	}
	// the empty set
	supp := t.wgt
	if isMax {
		supp = t.smin
	}
	for _, c := range t.counters(&t.nodes[0]) {
		if c >= supp {
			t.wgt |= skip
			break
		}
	}
	h := 0
	for ; h < t.height-1; h++ {
		for _, ni := range t.lvls[h] {
			t.cloMaxNode(ni, isMax)
		}
	}
	// the deepest level: every frequent set is closed and maximal
	for _, ni := range t.lvls[h] {
		cnts := t.counters(&t.nodes[ni])
		for i := range cnts {
			if cnts[i] < t.smin {
				cnts[i] |= skip
			}
		}
	}
}

// restore is the safe filtering of ist_clomax: top down, every unmarked
// frequent set unmarks its subsets with one item less (for closed sets only
// those with a raw counter at most its support, which includes all marked
// ones).
func (t *Tree) restore(isMax bool) {
	end := len(t.buf)
	supp := report.MaxSupp
	for k := t.height - 1; k > 0; k-- {
		for _, ni := range t.lvls[k] {
			nd := &t.nodes[ni]
			cnts := t.counters(nd)
			for i := int32(len(cnts)) - 1; i >= 0; i-- {
				if cnts[i] < t.smin {
					cnts[i] |= skip
					continue
				}
				if !isMax {
					supp = cnts[i]
				}
				cur := nd.parent
				pos := end - 1
				t.buf[pos] = t.itemAt(nd, i)
				t.clear(cur, t.buf[pos:pos+1], supp)
				pos--
				t.buf[pos] = nd.item
				t.clear(cur, t.buf[pos:pos+1], supp)
				n := 1
				for ; t.nodes[cur].parent >= 0; cur = t.nodes[cur].parent {
					n++
					t.clear(t.nodes[cur].parent, t.buf[pos:pos+n], supp)
					pos--
					t.buf[pos] = t.nodes[cur].item
				}
			}
		}
	}
}

// generators is the generator filter of ist_clomax: a set is marked unless
// it is frequent and every subset with one item less is an unmarked set
// with a larger support (a marked subset compares as negative, so the
// generator property propagates upwards level by level).
func (t *Tree) generators() {
	// Deviation from upstream: ist_clomax compares the single items with
	// the raw total weight, which carries the skip flag after Filter has
	// marked the empty set; then every item compares as ">= wgt" and no
	// generator is reported at all. This is reachable only with the
	// corrected -p call order (B2); the total weight without the flag is
	// used here.
	wgt := count(t.wgt)
	root := t.counters(&t.nodes[0])
	for i := len(root) - 1; i >= 0; i-- {
		if root[i] < t.smin || root[i] >= wgt {
			root[i] |= skip
		}
	}
	end := len(t.buf)
	for h := 1; h < t.height; h++ {
		for _, ni := range t.lvls[h] {
			nd := &t.nodes[ni]
			pn := &t.nodes[nd.parent]
			pc := t.cnts[pn.cnts+t.index(pn, nd.item)]
			cnts := t.counters(nd)
			for i := int32(len(cnts)) - 1; i >= 0; i-- {
				supp := cnts[i]
				if supp < t.smin || pc <= supp {
					cnts[i] |= skip
					continue
				}
				pos := end - 2
				t.buf[pos], t.buf[pos+1] = nd.item, t.itemAt(nd, i)
				cur := nd.parent
				for ; cur >= 0; cur = t.nodes[cur].parent {
					if t.getsupp(cur, t.buf[pos+1:end]) <= supp {
						break
					}
					pos--
					t.buf[pos] = t.nodes[cur].item
				}
				if cur >= 0 {
					cnts[i] |= skip
				}
			}
		}
	}
}

// cloMaxNode is the closed/maximal check of ist_clomax for the counters of
// one node on an intermediate level.
func (t *Tree) cloMaxNode(ni int32, isMax bool) {
	end := len(t.buf)
	nd := &t.nodes[ni]
	cnts := t.counters(nd)
	for i := int32(len(cnts)) - 1; i >= 0; i-- {
		if cnts[i] < t.smin {
			cnts[i] |= skip
			continue
		}
		item := t.itemAt(nd, i)
		supp := cnts[i]
		if isMax {
			supp = t.smin
		}
		// supersets in the child of the set
		if c := t.child(nd, item); c >= 0 {
			if hasAtLeast(t.counters(&t.nodes[c]), supp) {
				cnts[i] |= skip
				continue
			}
		}
		// other supersets: insert an item between two path items
		pos := end - 1
		t.buf[pos] = item
		n := 1
		found := false
		for cur := ni; cur >= 0; cur = t.nodes[cur].parent {
			cn := &t.nodes[cur]
			if pos < 1 {
				break // upstream: path <= ist->buf, never reached
			}
			var k, size int32 = 0, cn.size
			if cn.offset >= 0 {
				k = int32(t.buf[pos]) - cn.offset
			} else {
				k = bisect(t.ids[cn.ids:cn.ids+cn.size], t.buf[pos])
			}
			for k++; k < size; k++ {
				t.buf[pos-1] = t.itemAt(cn, k)
				if t.getsupp(cur, t.buf[pos-1:pos+n]) >= supp {
					break
				}
			}
			if k < size {
				found = true
				break
			}
			pos--
			t.buf[pos] = cn.item
			n++
		}
		if found {
			cnts[i] |= skip
		}
	}
}

// hasAtLeast reports whether some raw counter is at least supp.
func hasAtLeast(cnts []tract.Supp, supp tract.Supp) bool {
	for k := len(cnts) - 1; k >= 0; k-- {
		if cnts[k] >= supp {
			return true
		}
	}
	return false
}
