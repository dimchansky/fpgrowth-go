package fpg

import (
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// fpNode is a node of a simple frequent pattern tree (upstream FPNODE).
// id is a head index, a packed entry (tract.End | bits) or tract.End for
// the root; parent and succ are arena indices (nilIdx for none).
type fpNode struct {
	id     tract.Item
	parent int32
	succ   int32 // next node with the same item
	supp   tract.Supp
}

// fpHead is a header table entry (upstream FPHEAD).
type fpHead struct {
	item tract.Item // reporter item code (or packed head marker)
	supp tract.Supp
	list int32 // most recently created node of the item
}

// fpTree is a simple frequent pattern tree (upstream FPTREE). Its nodes
// live in the miner's fpArena, shared by all projections.
type fpTree struct {
	cnt   int
	dir   int
	fim   *machine
	root  int32
	heads []fpHead
}

// fpArena holds the nodes of all simple trees of one run (upstream MEMSYS
// pool). Projections allocate after a mark and release to it when they are
// done.
type fpArena struct{ nodes []fpNode }

func (a *fpArena) alloc(n fpNode) int32 {
	checkArena(len(a.nodes))
	a.nodes = append(a.nodes, n)
	return int32(len(a.nodes) - 1)
}

// fpHeadBuf returns the reusable header buffer of recursion depth depth
// with length n.
func (m *miner) fpHeadBuf(depth, n int) []fpHead {
	for len(m.fpHeads) <= depth {
		m.fpHeads = append(m.fpHeads, nil)
	}
	if cap(m.fpHeads[depth]) < n {
		m.fpHeads[depth] = make([]fpHead, n)
	}
	return m.fpHeads[depth][:n]
}

// addSimple is a port of add_simple: it adds the transaction ids (head
// indices in ascending order) with weight supp. A node is shared only if
// the most recently created node of its item has the current node as
// parent.
func (m *miner) addSimple(t *fpTree, ids []int32, supp tract.Supp) {
	nodes := m.fp.nodes
	node := t.root
	k := 0
	var i int32
	for {
		nodes[node].supp += supp // update the item set support
		if k >= len(ids) {
			return
		}
		i = ids[k]
		k++
		c := t.heads[i].list
		if c < 0 || nodes[c].parent != node {
			break
		}
		node = c
	}
	for {
		c := m.fp.alloc(fpNode{id: tract.Item(i), supp: supp, parent: node, succ: t.heads[i].list})
		t.heads[i].list = c
		node = c
		if k >= len(ids) {
			return
		}
		i = ids[k]
		k++
	}
}

// projSimple is a port of proj_simple: it builds in dst the conditional
// tree of head id of src and returns false if it is empty.
func (m *miner) projSimple(dst, src *fpTree, id int) bool {
	nodes := m.fp.nodes
	s := m.cis[:id]
	clear(s)
	for node := src.heads[id].list; node >= 0; node = nodes[node].succ {
		for anc := nodes[node].parent; nodes[anc].id >= 0; anc = nodes[anc].parent {
			s[nodes[anc].id] += nodes[node].supp // conditional support
		}
	}
	pex := m.perfectThreshold(src.heads[id].supp)
	imap := m.imap
	n := 0
	for i := range id {
		if s[i] < m.supp { // eliminate infrequent items
			imap[i] = -1
			continue
		}
		if s[i] >= pex { // collect perfect extension items
			imap[i] = -1
			m.rep.AddPex(src.heads[i].item)
			continue
		}
		imap[i] = int32(n)
		dst.heads[n] = fpHead{item: src.heads[i].item, supp: s[i], list: nilIdx}
		n++
	}
	if n <= 0 {
		return false
	}
	dst.cnt = n
	m.fp.nodes[dst.root].supp = 0
	for node := src.heads[id].list; node >= 0; node = m.fp.nodes[node].succ {
		nodes := m.fp.nodes
		d := len(m.path)
		for anc := nodes[node].parent; nodes[anc].id > tract.End; anc = nodes[anc].parent {
			if i := imap[nodes[anc].id]; i >= 0 {
				d--
				m.path[d] = i
			}
		}
		m.addSimple(dst, m.path[d:], nodes[node].supp)
	}
	return true
}

// recSimple is a port of rec_simple.
func (m *miner) recSimple(t *fpTree, depth int) error {
	if err := m.aborted(); err != nil {
		return err
	}
	if m.mode&ModeTail != 0 { // head union tail pruning
		tb := m.tailBuf()
		for i := range t.cnt {
			tb.push(t.heads[i].item)
		}
		if m.rep.Tail(tb.items()) {
			return nil
		}
	}
	var proj *fpTree
	var mark int
	if t.cnt > 1 && m.xable2() {
		proj = &fpTree{dir: t.dir, heads: m.fpHeadBuf(depth+1, t.cnt-1)}
		proj.root = m.fp.alloc(fpNode{id: tract.End, parent: nilIdx, succ: nilIdx})
		mark = len(m.fp.nodes)
		defer func() { m.fp.nodes = m.fp.nodes[:mark-1] }()
	}
	i, z := 0, t.cnt
	if t.dir < 0 {
		i, z = t.cnt-1, -1
	}
	for ; i != z; i += t.dir {
		h := &t.heads[i]
		if !m.rep.Add(h.item, h.supp) {
			continue
		}
		nodes := m.fp.nodes
		node := h.list
		switch {
		case nodes[node].succ < 0: // projection would be a chain
			for anc := nodes[node].parent; nodes[anc].id > tract.End; anc = nodes[anc].parent {
				m.rep.AddPex(t.heads[nodes[anc].id].item)
			}
		case proj != nil:
			if m.projSimple(proj, t, i) {
				if err := m.recSimple(proj, depth+1); err != nil {
					return err
				}
			}
			m.fp.nodes = m.fp.nodes[:mark] // release the projection
		}
		if err := m.rep.Report(); err != nil {
			return err
		}
		m.rep.Remove(1)
	}
	return nil
}

// addSmp16 is a port of add_smp16: add_simple for transactions that may
// start with a packed entry, which goes to the packed head 0 (and, for
// dir > 0, directly to the 16-items machine).
func (m *miner) addSmp16(t *fpTree, ids []int32, supp tract.Supp) {
	node := t.root
	m.fp.nodes[node].supp += supp
	if len(ids) == 0 {
		return
	}
	k := 0
	if ids[0] < 0 { // packed items
		i := tract.Item(ids[0])
		k++
		if t.dir > 0 {
			t.fim.add(uint16(i.PackedBits()), supp)
		}
		t.heads[0].item |= i // union of packed items
		node = t.heads[0].list
		if node >= 0 && m.fp.nodes[node].id == i {
			m.fp.nodes[node].supp += supp
		} else {
			c := m.fp.alloc(fpNode{id: i, supp: supp, parent: t.root, succ: node})
			t.heads[0].list = c
			node = c
		}
		if k >= len(ids) {
			return
		}
	}
	var i int32
	for {
		i = ids[k]
		k++
		c := t.heads[i].list
		if c < 0 || m.fp.nodes[c].parent != node {
			break
		}
		node = c
		m.fp.nodes[node].supp += supp
		if k >= len(ids) {
			return
		}
	}
	for {
		c := m.fp.alloc(fpNode{id: tract.Item(i), supp: supp, parent: node, succ: t.heads[i].list})
		t.heads[i].list = c
		node = c
		if k >= len(ids) {
			return
		}
		i = ids[k]
		k++
	}
}

// projSmp16 is a port of proj_smp16. Head 0 of dst is always the packed
// head; packed ancestors are restricted to mask.
func (m *miner) projSmp16(dst, src *fpTree, id int, mask int32) {
	nodes := m.fp.nodes
	s := m.cis[:id]
	clear(s)
	for node := src.heads[id].list; node >= 0; node = nodes[node].succ {
		for anc := nodes[node].parent; nodes[anc].id >= 0; anc = nodes[anc].parent {
			s[nodes[anc].id] += nodes[node].supp
		}
	}
	pex := m.perfectThreshold(src.heads[id].supp)
	imap := m.imap
	dst.heads[0] = fpHead{item: tract.End, supp: 0, list: nilIdx}
	n := 1
	for i := 1; i < id; i++ {
		if s[i] < m.supp {
			imap[i] = -1
			continue
		}
		if s[i] >= pex {
			imap[i] = -1
			m.rep.AddPex(src.heads[i].item)
			continue
		}
		imap[i] = int32(n)
		dst.heads[n] = fpHead{item: src.heads[i].item, supp: s[i], list: nilIdx}
		n++
	}
	// n >= 1, so upstream's "projection is empty" test is dead code.
	dst.cnt = n
	m.fp.nodes[dst.root].supp = 0
	for node := src.heads[id].list; node >= 0; node = m.fp.nodes[node].succ {
		nodes := m.fp.nodes
		d := len(m.path)
		for anc := nodes[node].parent; nodes[anc].id > tract.End; anc = nodes[anc].parent {
			i := int32(nodes[anc].id)
			if i < 0 {
				if i &= mask; i != 0 {
					d--
					m.path[d] = i | int32(tract.End)
				}
			} else if i = imap[i]; i >= 0 {
				d--
				m.path[d] = i
			}
		}
		m.addSmp16(dst, m.path[d:], nodes[node].supp)
	}
}

// recSmp16 is a port of rec_smp16: rec_simple with the packed head 0 mined
// by the 16-items machine (first for dir > 0, last for dir < 0).
func (m *miner) recSmp16(t *fpTree, depth int) error {
	if err := m.aborted(); err != nil {
		return err
	}
	if m.mode&ModeTail != 0 { // head union tail pruning
		tb := m.tailBuf()
		i := 0
		if t.heads[0].item < 0 { // first the packed tail items
			i++
			for z := range 16 {
				if t.heads[0].item&(1<<z) != 0 {
					tb.push(tract.Item(z))
				}
			}
		}
		for ; i < t.cnt; i++ {
			tb.push(t.heads[i].item)
		}
		if m.rep.Tail(tb.items()) {
			return nil
		}
	}
	var proj *fpTree
	var mark int
	if t.cnt > 1 && m.xable2() {
		proj = &fpTree{dir: t.dir, fim: t.fim, heads: m.fpHeadBuf(depth+1, t.cnt-1)}
		proj.root = m.fp.alloc(fpNode{id: tract.End, parent: nilIdx, succ: nilIdx})
		mark = len(m.fp.nodes)
		defer func() { m.fp.nodes = m.fp.nodes[:mark-1] }()
	}
	mask := int32(math.MaxInt32) // packed items to keep
	i, z := 0, t.cnt
	if t.dir < 0 {
		i, z = t.cnt-1, -1
	}
	for ; i != z; i += t.dir {
		h := &t.heads[i]
		if h.item < 0 { // packed head: use the 16-items machine
			if t.dir < 0 {
				nodes := m.fp.nodes
				for node := h.list; node >= 0; node = nodes[node].succ {
					t.fim.add(uint16(nodes[node].id.PackedBits()), nodes[node].supp)
				}
			}
			r, err := t.fim.mine()
			if err != nil {
				return err
			}
			mask = int32(r)
			continue
		}
		if !m.rep.Add(h.item, h.supp) {
			continue
		}
		nodes := m.fp.nodes
		node := h.list
		switch {
		case nodes[node].succ < 0: // projection would be a chain
			for anc := nodes[node].parent; nodes[anc].id > tract.End; anc = nodes[anc].parent {
				if k := nodes[anc].id; k >= 0 {
					m.rep.AddPex(t.heads[k].item)
				} else {
					m.rep.AddPexPacked(k)
				}
			}
		case proj != nil:
			m.projSmp16(proj, t, i, mask)
			if err := m.recSmp16(proj, depth+1); err != nil {
				return err
			}
			m.fp.nodes = m.fp.nodes[:mark] // release the projection
		}
		if err := m.rep.Report(); err != nil {
			return err
		}
		m.rep.Remove(1)
	}
	return nil
}

// mineSimple is a port of fpg_simple (with and without the 16-items
// machine).
func (m *miner) mineSimple() error {
	usePack := m.pack != 0
	d, s, f, cont, err := m.topLevel(usePack)
	if !cont {
		return err
	}
	k := len(s)
	t := &fpTree{cnt: k, dir: m.dir, heads: make([]fpHead, k)}
	m.fp.nodes = make([]fpNode, 0, presize(2*m.bag.Extent()+16))
	t.root = m.fp.alloc(fpNode{id: tract.End, parent: nilIdx, succ: nilIdx})
	for i := range t.heads {
		t.heads[i] = fpHead{item: tract.Item(s[i]), supp: f[s[i]], list: nilIdx}
	}
	buf := m.path
	if usePack {
		t.fim = newMachine(m.dir, m.supp, m.rep)
		t.heads[0].item = tract.End
		nt := 0
		for items, w := range m.bag.Backward() {
			if nt++; nt%buildCheckEvery == 0 {
				if err := m.aborted(); err != nil {
					return err
				}
			}
			n := 0
			for _, p := range items {
				if p < 0 { // packed items are always copied
					buf[n] = int32(p)
					n++
				} else if x := d[p]; x >= 0 {
					buf[n] = x
					n++
				}
			}
			m.addSmp16(t, buf[:n], w)
		}
		if err := m.recSmp16(t, 0); err != nil {
			return err
		}
		return m.rep.Report() // finally report the empty item set
	}
	nt := 0
	for items, w := range m.bag.Backward() {
		if nt++; nt%buildCheckEvery == 0 {
			if err := m.aborted(); err != nil {
				return err
			}
		}
		n := 0
		for _, p := range items {
			if x := d[p]; x >= 0 {
				buf[n] = x
				n++
			}
		}
		m.addSimple(t, buf[:n], w)
	}
	if err := m.recSimple(t, 0); err != nil {
		return err
	}
	return m.rep.Report() // report the empty item set
}
