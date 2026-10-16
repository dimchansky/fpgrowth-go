package fpg

import (
	"math/bits"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// The 16-items machine is a port of fim16.c: it mines all frequent item
// sets over at most 16 items whose transactions are 16-bit masks (bit i
// stands for the reporter item imap[i]).

// prjms are the projection masks (upstream prjms): prjms[i] keeps the bits
// below i.
var prjms = [16]uint16{
	0x0000, 0x0001, 0x0003, 0x0007,
	0x000f, 0x001f, 0x003f, 0x007f,
	0x00ff, 0x01ff, 0x03ff, 0x07ff,
	0x0fff, 0x1fff, 0x3fff, 0x7fff,
}

// hibit is upstream's hibit table: the index of the highest set bit, and
// 0 for 0.
func hibit(t uint16) int {
	if t == 0 {
		return 0
	}
	return bits.Len16(t) - 1
}

// m16 is one (sub-)machine (upstream FIM16).
type m16 struct {
	wgts  []tract.Supp // weight per bit pattern (2^s entries)
	lists [16][]uint16 // distinct patterns per highest bit (btas..ends)
	supps [16]tract.Supp
	ttw   tract.Supp // total transaction weight
	tor   uint16     // bitwise or of the added patterns
	smin  tract.Supp
	base  *machine
}

// machine is a 16-items machine: one m16 for dir > 0 (rec_pos), sixteen
// with 16, 15, ..., 1 items for dir <= 0 (rec_neg, which projects machine
// k into k+1). The item map is shared by all sub-machines.
type machine struct {
	dir  int
	subs []m16
	imap [16]tract.Item
	rep  *report.Reporter
	// limited and zmax limit a closed/maximal search like miner.deeper.
	limited bool
	zmax    int
}

// deeper is miner.deeper for an item added by the machine.
func (f *machine) deeper() bool {
	return !f.limited || f.rep.Len()+f.rep.PexCount() < f.zmax
}

// newMachine is a port of m16_create; the item map is the identity
// (m16_clrmap).
func newMachine(dir int, smin tract.Supp, rep *report.Reporter) *machine {
	n := 1
	if dir <= 0 {
		n = 16
	}
	f := &machine{dir: dir, subs: make([]m16, n), rep: rep}
	for i := range f.imap {
		f.imap[i] = tract.Item(i)
	}
	for k := range f.subs {
		s := 16 - k // number of items of this sub-machine
		cur := &f.subs[k]
		cur.base = f
		cur.smin = smin
		cur.wgts = make([]tract.Supp, 1<<s)
		total := 0
		for i := range s {
			total += listCap(i)
		}
		block := make([]uint16, total)
		off := 0
		for i := range s {
			c := listCap(i)
			cur.lists[i] = block[off : off : off+c]
			off += c
		}
	}
	return f
}

// listCap is the capacity of the list of patterns with highest bit i.
func listCap(i int) int {
	if i > 1 {
		return 1 << i
	}
	return 2
}

// setMap is m16_setmap: bit i stands for the reporter item item.
func (f *machine) setMap(i int, item tract.Item) { f.imap[i] = item }

// add is a port of m16_add on the base machine.
func (f *machine) add(t uint16, w tract.Supp) { f.subs[0].add(t, w) }

func (f *m16) add(t uint16, w tract.Supp) {
	f.ttw += w // sum the transaction weight
	if t == 0 {
		return // empty transactions only count
	}
	f.tor |= t
	f.wgts[t] += w
	if f.wgts[t] <= w { // first occurrence of the pattern
		h := hibit(t)
		f.lists[h] = append(f.lists[h], t)
	}
}

// project is a port of project: it moves the patterns of item into dst,
// removing the item and the bits not in mask.
func (f *m16) project(item int, mask uint16, dst *m16) {
	mask &= prjms[item]
	f.supps[item] = 0
	list := f.lists[item]
	f.lists[item] = list[:0]
	for _, t := range list {
		w := f.wgts[t]
		if p := t & mask; p != 0 {
			dst.wgts[p] += w
			if dst.wgts[p] <= w {
				h := hibit(p)
				dst.lists[h] = append(dst.lists[h], p)
			}
		}
		f.wgts[t] = 0
	}
}

// count is a port of count: it computes the supports of items 0..n-1 and
// leaves in list j the conditional database of item j.
func (f *m16) count(n int) {
	for j := n - 1; j > 1; j-- {
		var s tract.Supp
		for _, t := range f.lists[j] { // appends go to lower lists only
			w := f.wgts[t]
			s += w
			p := t & prjms[j]
			if p == 0 {
				continue
			}
			f.wgts[p] += w
			if f.wgts[p] <= w {
				h := hibit(p)
				f.lists[h] = append(f.lists[h], p)
			}
		}
		f.supps[j] = s
	}
	f.supps[1] = f.wgts[2] + f.wgts[3]
	f.supps[0] = f.wgts[1] + f.wgts[3]
}

// clear is a port of clear: it empties the list of item.
func (f *m16) clear(item int) {
	f.supps[item] = 0
	for _, t := range f.lists[item] {
		f.wgts[t] = 0
	}
	f.lists[item] = f.lists[item][:0]
}

// filter is a port of filter: it removes infrequent items and perfect
// extensions (support >= pex, reported to the reporter) and returns the
// mask of the remaining items.
func (f *m16) filter(n int, pex tract.Supp) uint16 {
	var mask uint16
	for j := n - 1; j >= 0; j-- {
		switch {
		case f.supps[j] < f.smin:
			f.clear(j)
		case f.supps[j] >= pex:
			f.clear(j)
			f.base.rep.AddPex(f.base.imap[j])
		default:
			mask |= 1 << j
		}
	}
	return mask
}

// addPexBits adds every bit of t as a perfect extension, lowest first.
func (f *machine) addPexBits(t uint16) {
	for r := 0; 1<<r <= int(t); r++ {
		if t&(1<<r) != 0 {
			f.rep.AddPex(f.imap[r])
		}
	}
}

// reportItem reports the current set and removes its last item.
func (f *machine) reportItem() error {
	if err := f.rep.Report(); err != nil {
		return err
	}
	f.rep.Remove(1)
	return nil
}

// recPos is a port of rec_pos (dir > 0): items 0, 1, 2, 3 by hand, then
// items 4..n-1 ascending.
func (f *m16) recPos(n int, mask uint16) error {
	rep := f.base.rep
	imap := &f.base.imap
	// The mask must be checked, because items 0, 1, 2 may have been
	// identified as perfect extensions in higher recursion levels.
	if mask&1 != 0 && f.supps[0] >= f.smin {
		if rep.Add(imap[0], f.supps[0]) {
			if err := f.base.reportItem(); err != nil {
				return err
			}
		}
	}
	if mask&2 != 0 && f.supps[1] >= f.smin {
		if rep.Add(imap[1], f.supps[1]) {
			if err := f.pair(mask); err != nil {
				return err
			}
			if err := f.base.reportItem(); err != nil {
				return err
			}
		}
	}
	if n <= 2 { // clear counters and lists of items 0 and 1
		clear(f.wgts[1:4])
		clear(f.supps[0:2])
		f.lists[0], f.lists[1] = f.lists[0][:0], f.lists[1][:0]
		return nil
	}
	if mask&4 != 0 && f.supps[2] >= f.smin {
		if rep.Add(imap[2], f.supps[2]) {
			var m uint16 // project/count for items 0 and 1
			f.wgts[3] = f.wgts[7]
			f.wgts[2] = f.wgts[6]
			f.wgts[1] = f.wgts[5]
			s := f.wgts[3] + f.wgts[1]
			f.supps[0] = s
			if s >= f.supps[2] {
				rep.AddPex(imap[0])
			} else if s >= f.smin {
				m |= mask & 1
			}
			s = f.wgts[3] + f.wgts[2]
			f.supps[1] = s
			if s >= f.supps[2] {
				rep.AddPex(imap[1])
			} else if s >= f.smin {
				m |= mask & 2
			}
			if err := f.recPos(2, m); err != nil {
				return err
			}
			if err := f.base.reportItem(); err != nil {
				return err
			}
		}
	}
	if n <= 3 { // clear counters and lists of items 0, 1, 2
		clear(f.wgts[1:8])
		clear(f.supps[0:3])
		for i := range 3 {
			f.lists[i] = f.lists[i][:0]
		}
		return nil
	}
	if mask&8 != 0 && f.supps[3] >= f.smin {
		if rep.Add(imap[3], f.supps[3]) {
			var m uint16 // project/count for items 0, 1, 2
			f.wgts[4] = f.wgts[12]
			f.wgts[7] = f.wgts[15]
			f.wgts[3] = f.wgts[11] + f.wgts[7]
			f.wgts[6] = f.wgts[14]
			f.wgts[2] = f.wgts[10] + f.wgts[6]
			f.wgts[5] = f.wgts[13]
			f.wgts[1] = f.wgts[9] + f.wgts[5]
			s := f.wgts[3] + f.wgts[1]
			f.supps[0] = s
			if s >= f.supps[3] {
				rep.AddPex(imap[0])
			} else if s >= f.smin {
				m |= mask & 1
			}
			s = f.wgts[3] + f.wgts[2]
			f.supps[1] = s
			if s >= f.supps[3] {
				rep.AddPex(imap[1])
			} else if s >= f.smin {
				m |= mask & 2
			}
			s = f.wgts[7] + f.wgts[6] + f.wgts[5] + f.wgts[4]
			f.supps[2] = s
			if s >= f.supps[3] {
				rep.AddPex(imap[2])
			} else if s >= f.smin {
				m |= mask & 4
			}
			if err := f.recPos(3, m); err != nil {
				return err
			}
			if err := f.base.reportItem(); err != nil {
				return err
			}
		}
	}
	clear(f.wgts[1:16]) // clear weights, supports and lists of items 0..3
	clear(f.supps[0:4])
	for i := range 4 {
		f.lists[i] = f.lists[i][:0]
	}
	for i := 4; i < n; i++ {
		s := f.supps[i]
		if s < f.smin {
			continue // skip infrequent/eliminated items
		}
		if !rep.Add(imap[i], s) {
			f.clear(i)
			continue
		}
		if len(f.lists[i]) <= 1 { // one transaction: all perfect extensions
			if err := f.single(i); err != nil {
				return err
			}
		} else {
			f.project(i, mask, f)
			f.count(i)
			m := f.filter(i, s)
			if err := f.recPos(i, m); err != nil {
				return err
			}
		}
		if err := f.base.reportItem(); err != nil {
			return err
		}
	}
	return nil
}

// pair handles item 0 below item 1 (the block shared by rec_pos and
// rec_neg); item 1 has just been added.
func (f *m16) pair(mask uint16) error {
	rep := f.base.rep
	if mask&1 == 0 || f.wgts[3] < f.smin {
		return nil
	}
	if f.wgts[3] >= f.supps[1] { // perfect extension
		rep.AddPex(f.base.imap[0])
		return nil
	}
	if rep.Add(f.base.imap[0], f.wgts[3]) {
		return f.base.reportItem()
	}
	return nil
}

// single handles an item whose list holds at most one pattern: every bit
// of the pattern is a perfect extension.
func (f *m16) single(i int) error {
	list := f.lists[i]
	f.lists[i] = list[:0]
	if len(list) == 0 { // impossible with a minimum support >= 1
		return nil
	}
	t := list[0]
	f.wgts[t] = 0
	f.base.addPexBits(t)
	return nil
}

// recNeg is a port of rec_neg (dir < 0): items n-1..2 descending with
// projection into the next sub-machine, then items 1 and 0 by hand.
func (f *machine) recNeg(k, n int, mask uint16) error {
	cur := &f.subs[k]
	rep := f.rep
	for i := n - 1; i >= 2; i-- {
		s := cur.supps[i]
		if s < cur.smin {
			continue
		}
		if !rep.Add(f.imap[i], s) {
			cur.clear(i)
			continue
		}
		if len(cur.lists[i]) <= 1 {
			if err := cur.single(i); err != nil {
				return err
			}
		} else if !f.deeper() { // size limit: no projection needed
			cur.clear(i)
		} else {
			nxt := &f.subs[k+1]
			cur.project(i, mask, nxt)
			nxt.count(i)
			m := nxt.filter(i, s)
			if err := f.recNeg(k+1, i, m); err != nil {
				return err
			}
		}
		if err := f.reportItem(); err != nil {
			return err
		}
	}
	if mask&2 != 0 && cur.supps[1] >= cur.smin {
		if rep.Add(f.imap[1], cur.supps[1]) {
			if err := cur.pair(mask); err != nil {
				return err
			}
			if err := f.reportItem(); err != nil {
				return err
			}
		}
	}
	if mask&1 != 0 && cur.supps[0] >= cur.smin {
		if rep.Add(f.imap[0], cur.supps[0]) {
			if err := f.reportItem(); err != nil {
				return err
			}
		}
	}
	clear(cur.wgts[1:4])
	clear(cur.supps[0:2])
	cur.lists[0], cur.lists[1] = cur.lists[0][:0], cur.lists[1][:0]
	return nil
}

// mine is a port of m16_mine: it mines the machine below the current
// prefix of the reporter, clears it and returns the mask of the frequent
// items that are not perfect extensions (0 if the machine was empty or
// all items are perfect extensions).
func (f *machine) mine() (int, error) {
	m0 := &f.subs[0]
	if m0.ttw <= 0 {
		return 0, nil // the machine is empty
	}
	s := f.rep.Supp() // minimum support for perfect extensions
	t := m0.tor
	if m0.wgts[t] >= s { // only one transaction:
		m0.wgts[t] = 0 // all items are perfect extensions
		f.addPexBits(t)
		h := hibit(t)
		m0.lists[h] = m0.lists[h][:0]
		m0.ttw, m0.tor = 0, 0
		return 0, nil
	}
	n := hibit(t) + 1
	m0.count(n)
	mask := m0.filter(n, s)
	var err error
	if f.dir > 0 {
		err = m0.recPos(n, mask)
	} else {
		err = f.recNeg(0, n, mask)
	}
	m0.ttw, m0.tor = 0, 0
	if err != nil {
		return 0, err
	}
	return int(mask), nil
}
