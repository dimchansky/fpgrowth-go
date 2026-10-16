package fpg

import "github.com/dimchansky/fpgrowth-go/internal/tract"

// csNode is a node of a children/sibling tree (upstream CSNODE). Children
// are sorted ascending by id.
type csNode struct {
	id       tract.Item // head index, tract.End for the root
	children int32
	sibling  int32
	parent   int32
	succ     int32 // next node with the same item
	supp     tract.Supp
}

// csHead is a header table entry (upstream CSHEAD).
type csHead struct {
	item tract.Item
	supp tract.Supp
	list int32
}

// csTree is a children/sibling tree (upstream CSTREE).
type csTree struct {
	cnt   int
	root  int32
	heads []csHead
}

// csArena holds the nodes of all children/sibling trees of one run.
type csArena struct{ nodes []csNode }

func (a *csArena) alloc(n csNode) int32 {
	checkArena(len(a.nodes))
	a.nodes = append(a.nodes, n)
	return int32(len(a.nodes) - 1)
}

// csLink addresses a node link: the children field of node (sibling
// false) or its sibling field (upstream CSNODE **).
type csLink struct {
	node    int32
	sibling bool
}

func (a *csArena) get(l csLink) int32 {
	if l.sibling {
		return a.nodes[l.node].sibling
	}
	return a.nodes[l.node].children
}

func (a *csArena) set(l csLink, v int32) {
	if l.sibling {
		a.nodes[l.node].sibling = v
	} else {
		a.nodes[l.node].children = v
	}
}

// csHeadBuf returns the reusable header buffer of recursion depth depth.
func (m *miner) csHeadBuf(depth, n int) []csHead {
	for len(m.csHeads) <= depth {
		m.csHeads = append(m.csHeads, nil)
	}
	if cap(m.csHeads[depth]) < n {
		m.csHeads[depth] = make([]csHead, n)
	}
	return m.csHeads[depth][:n]
}

// addCmplx is a port of add_cmplx: it adds the transaction ids (ascending
// head indices) with weight supp to the trie.
func (m *miner) addCmplx(t *csTree, ids []int32, supp tract.Supp) {
	a := &m.cs
	node := t.root
	k := 0
	var i int32
	var p csLink
	for {
		a.nodes[node].supp += supp
		if k >= len(ids) {
			return
		}
		i = ids[k]
		k++
		p = csLink{node: node}
		for c := a.get(p); c >= 0 && int32(a.nodes[c].id) < i; c = a.get(p) {
			p = csLink{node: c, sibling: true}
		}
		c := a.get(p)
		if c < 0 || int32(a.nodes[c].id) != i {
			break
		}
		node = c
	}
	c := a.alloc(csNode{
		id: tract.Item(i), supp: supp, parent: node, children: nilIdx,
		sibling: a.get(p), succ: t.heads[i].list,
	})
	t.heads[i].list = c
	a.set(p, c)
	node = c
	for k < len(ids) {
		i = ids[k]
		k++
		c := a.alloc(csNode{
			id: tract.Item(i), supp: supp, parent: node, children: nilIdx,
			sibling: nilIdx, succ: t.heads[i].list,
		})
		a.nodes[node].children = c
		t.heads[i].list = c
		node = c
	}
}

// condSupp computes the conditional supports of the items below head id
// (the first step of every proj_* function) into m.cis[:id].
func (m *miner) csCondSupp(src *csTree, id int) []tract.Supp {
	nodes := m.cs.nodes
	s := m.cis[:id]
	clear(s)
	for node := src.heads[id].list; node >= 0; node = nodes[node].succ {
		for anc := nodes[node].parent; nodes[anc].id >= 0; anc = nodes[anc].parent {
			s[nodes[anc].id] += nodes[node].supp
		}
	}
	return s
}

// mine16 mines the conditional database of head id of src with the
// 16-items machine, bits given by imap (the 16-items paths of proj_cmplx
// and proj_reord). The heads of dst define the machine's item map.
func (m *miner) mine16(dst, src *csTree, id, n int, imap []int32) error {
	for i := range n {
		m.fim.setMap(i, dst.heads[i].item)
	}
	nodes := m.cs.nodes
	for node := src.heads[id].list; node >= 0; node = nodes[node].succ {
		var b uint16
		for anc := nodes[node].parent; nodes[anc].id >= 0; anc = nodes[anc].parent {
			if i := imap[nodes[anc].id]; i >= 0 {
				b |= 1 << i
			}
		}
		m.fim.add(b, nodes[node].supp)
	}
	_, err := m.fim.mine()
	return err
}

// projCmplx is a port of proj_cmplx. It returns whether a projection was
// created (false also when the 16-items machine processed it).
func (m *miner) projCmplx(dst, src *csTree, id int) (bool, error) {
	s := m.csCondSupp(src, id)
	pex := m.perfectThreshold(src.heads[id].supp)
	imap := m.imap
	n := 0
	for i := range id {
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
		dst.heads[n] = csHead{item: src.heads[i].item, supp: s[i], list: nilIdx}
		n++
	}
	if n <= 0 {
		return false, nil
	}
	dst.cnt = n
	if n <= 16 && m.fim != nil { // at most 16 items left
		return false, m.mine16(dst, src, id, n, imap)
	}
	a := &m.cs
	a.nodes[dst.root].supp = 0
	a.nodes[dst.root].children = nilIdx
	for node := src.heads[id].list; node >= 0; node = a.nodes[node].succ {
		nodes := a.nodes
		d := len(m.path)
		for anc := nodes[node].parent; nodes[anc].id >= 0; anc = nodes[anc].parent {
			if i := imap[nodes[anc].id]; i >= 0 {
				d--
				m.path[d] = i
			}
		}
		m.addCmplx(dst, m.path[d:], nodes[node].supp)
	}
	return true, nil
}

// addReord is a port of add_reord: it adds the items whose flags are set
// (in ascending index order), clearing the flags, with weight supp; n is
// the number of set flags.
func (m *miner) addReord(t *csTree, flags []int32, n int, supp tract.Supp) {
	a := &m.cs
	node := t.root
	a.nodes[node].supp += supp
	if n <= 0 {
		return
	}
	var p csLink
	i := 0
	for ; ; i++ {
		if flags[i] == 0 {
			continue
		}
		flags[i] = 0
		p = csLink{node: node}
		for c := a.get(p); c >= 0 && int(a.nodes[c].id) < i; c = a.get(p) {
			p = csLink{node: c, sibling: true}
		}
		c := a.get(p)
		if c < 0 || int(a.nodes[c].id) != i {
			break
		}
		node = c
		a.nodes[node].supp += supp
		if n--; n <= 0 {
			return
		}
	}
	c := a.alloc(csNode{
		id: tract.Item(i), supp: supp, parent: node, children: nilIdx,
		sibling: a.get(p), succ: t.heads[i].list,
	})
	t.heads[i].list = c
	a.set(p, c)
	node = c
	if n--; n <= 0 {
		return
	}
	for {
		i++
		if flags[i] == 0 {
			continue
		}
		flags[i] = 0
		c := a.alloc(csNode{
			id: tract.Item(i), supp: supp, parent: node, children: nilIdx,
			sibling: nilIdx, succ: t.heads[i].list,
		})
		a.nodes[node].children = c
		t.heads[i].list = c
		node = c
		if n--; n <= 0 {
			return
		}
	}
}

// projReord is a port of proj_reord: the kept items are sorted descending
// by conditional support (i2s_sort with upstream's tie order) and become
// the heads of dst in that order.
func (m *miner) projReord(dst, src *csTree, id int) (bool, error) {
	s := m.csCondSupp(src, id)
	pex := m.perfectThreshold(src.heads[id].supp)
	imap := m.set[:id]
	d := m.set[id : 2*id]
	n := 0
	for i := range id {
		if s[i] < m.supp {
			imap[i] = -1
			continue
		}
		if s[i] >= pex {
			imap[i] = -1
			m.rep.AddPex(src.heads[i].item)
			continue
		}
		d[n] = int32(i)
		n++
	}
	if n <= 0 {
		return false, nil
	}
	dst.cnt = n
	idxQsort(d[:n], -1, s) // sort descending by conditional support
	for i := range n {
		k := d[i]
		dst.heads[i] = csHead{item: src.heads[k].item, supp: s[k], list: nilIdx}
		imap[k] = int32(i)
	}
	if n <= 16 && m.fim != nil { // at most 16 items left
		return false, m.mine16(dst, src, id, n, imap)
	}
	a := &m.cs
	a.nodes[dst.root].supp = 0
	a.nodes[dst.root].children = nilIdx
	clear(d)
	for node := src.heads[id].list; node >= 0; node = a.nodes[node].succ {
		nodes := a.nodes
		k := 0
		for anc := nodes[node].parent; nodes[anc].id >= 0; anc = nodes[anc].parent {
			if i := imap[nodes[anc].id]; i >= 0 {
				d[i] = 1
				k++
			}
		}
		m.addReord(dst, d, k, nodes[node].supp)
	}
	return true, nil
}

// recCmplx is a port of rec_cmplx.
func (m *miner) recCmplx(t *csTree, depth int) error {
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
	var proj *csTree
	var mark int
	if t.cnt > 1 && m.xable2() {
		proj = &csTree{heads: m.csHeadBuf(depth+1, t.cnt-1)}
		proj.root = m.cs.alloc(csNode{
			id: tract.End, parent: nilIdx, succ: nilIdx,
			sibling: nilIdx, children: nilIdx,
		})
		mark = len(m.cs.nodes)
		defer func() { m.cs.nodes = m.cs.nodes[:mark-1] }()
	}
	i, z := 0, t.cnt
	if m.dir < 0 {
		i, z = t.cnt-1, -1
	}
	for ; i != z; i += m.dir {
		h := &t.heads[i]
		if !m.rep.Add(h.item, h.supp) {
			continue
		}
		nodes := m.cs.nodes
		switch {
		case nodes[h.list].succ < 0: // projection would be a chain
			for node := nodes[h.list].parent; nodes[node].id >= 0; node = nodes[node].parent {
				m.rep.AddPex(t.heads[nodes[node].id].item)
			}
		case proj != nil:
			var ok bool
			var err error
			if m.mode&ModeReorder != 0 {
				ok, err = m.projReord(proj, t, i)
			} else {
				ok, err = m.projCmplx(proj, t, i)
			}
			if err == nil && ok {
				err = m.recCmplx(proj, depth+1)
			}
			if err != nil {
				return err
			}
			m.cs.nodes = m.cs.nodes[:mark] // release the projection
		}
		if err := m.rep.Report(); err != nil {
			return err
		}
		m.rep.Remove(1)
	}
	return nil
}

// mineComplex is a port of fpg_cmplx.
func (m *miner) mineComplex() error {
	d, s, f, cont, err := m.topLevel(false)
	if !cont {
		return err
	}
	k := len(s)
	t := &csTree{cnt: k, heads: make([]csHead, k)}
	m.cs.nodes = make([]csNode, 0, presize(2*m.bag.Extent()+16))
	t.root = m.cs.alloc(csNode{
		id: tract.End, parent: nilIdx, succ: nilIdx,
		sibling: nilIdx, children: nilIdx,
	})
	for i := range t.heads {
		t.heads[i] = csHead{item: tract.Item(s[i]), supp: f[s[i]], list: nilIdx}
	}
	if m.pack != 0 {
		m.fim = newMachine(m.dir, m.supp, m.rep)
	}
	buf := m.path
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
		m.addCmplx(t, buf[:n], w)
	}
	if err := m.recCmplx(t, 0); err != nil {
		return err
	}
	return m.rep.Report() // report the empty item set
}
