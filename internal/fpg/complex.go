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
	supp tract.Supp
	item tract.Item
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
	nodes := m.cs.nodes
	node := t.root
	k := 0
	var i int32
	var p *int32 // the link to the insertion point (upstream CSNODE **)
	for {
		nodes[node].supp += supp
		if k >= len(ids) {
			return
		}
		i = ids[k]
		k++
		p = &nodes[node].children
		c := *p
		for c >= 0 && int32(nodes[c].id) < i {
			p = &nodes[c].sibling
			c = *p
		}
		if c < 0 || int32(nodes[c].id) != i {
			break
		}
		node = c
	}
	m.csInsert(t, p, node, tract.Item(i), supp)
	node = int32(len(m.cs.nodes) - 1)
	for k < len(ids) {
		i = ids[k]
		k++
		c := m.cs.alloc(csNode{
			id: tract.Item(i), supp: supp, parent: node, children: nilIdx,
			sibling: nilIdx, succ: t.heads[i].list,
		})
		m.cs.nodes[node].children = c
		t.heads[i].list = c
		node = c
	}
}

// csInsert allocates a node for item i below parent with weight supp and
// links it at *p, a children or sibling field of the arena, in front of
// the node *p pointed to. *p is written before the allocation: the
// append in alloc may move the arena, and p points into the old one.
func (m *miner) csInsert(t *csTree, p *int32, parent int32, i tract.Item, supp tract.Supp) {
	c := int32(len(m.cs.nodes))
	sib := *p
	*p = c
	m.cs.alloc(csNode{
		id: i, supp: supp, parent: parent, children: nilIdx,
		sibling: sib, succ: t.heads[i].list,
	})
	t.heads[i].list = c
}

// projPaths are the paths of the nodes of one header list, recorded by
// csCondSupp (fpCondSupp in the simple variant): node k has weight w[k]
// and the ancestor ids (from its parent upwards, so descending)
// ids[end[k-1]:end[k]] (from 0 for k = 0). The second pass of a
// projection (mine16, projCmplx, projReord; projSimple, projSmp16) reads
// them instead of walking the scattered tree nodes again.
type projPaths struct {
	ids []int32
	end []int32
	w   []tract.Supp
}

// condSupp computes the conditional supports of the items below head id
// (the first step of every proj_* function) into m.cis[:id] and records
// the paths of the head's nodes in m.paths.
//
// This walk is the hottest loop of the complex variant. It loads every
// node once through a pointer (one bounds check per node instead of one
// per field access) and keeps the transaction weight in a register.
func (m *miner) csCondSupp(src *csTree, id int) []tract.Supp {
	nodes := m.cs.nodes
	s := m.cis[:id]
	clear(s)
	p := &m.paths
	ids, end, ws := p.ids[:0], p.end[:0], p.w[:0]
	for node := src.heads[id].list; node >= 0; {
		n := &nodes[node]
		w := n.supp
		for anc := n.parent; ; {
			a := &nodes[anc]
			if a.id < 0 {
				break
			}
			s[a.id] += w
			ids = append(ids, int32(a.id))
			anc = a.parent
		}
		end = append(end, int32(len(ids)))
		ws = append(ws, w)
		node = n.succ
	}
	p.ids, p.end, p.w = ids, end, ws
	return s
}

// mine16 mines the conditional database of head id of src with the
// 16-items machine, bits given by imap (the 16-items paths of proj_cmplx
// and proj_reord), from the paths recorded by csCondSupp. The heads of
// dst define the machine's item map.
func (m *miner) mine16(dst *csTree, id, n int, imap []int32) error {
	for i := range n {
		m.fim.setMap(i, dst.heads[i].item)
	}
	// Translate imap into bit masks (0 for dropped items), so that the
	// path loop below ORs without a data-dependent branch.
	bits := m.bit16[:id]
	for i, k := range imap[:id] {
		bits[i] = 0
		if k >= 0 {
			bits[i] = 1 << (k & 15)
		}
	}
	p := &m.paths
	start := int32(0)
	for k, end := range p.end {
		var b uint16
		for _, i := range p.ids[start:end] {
			b |= bits[i]
		}
		m.fim.add(b, p.w[k])
		start = end
	}
	// every set the machine reports has its siblings among its n items
	m.left = n
	_, err := m.fim.mine()
	m.left = -1
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
		return false, m.mine16(dst, id, n, imap)
	}
	a := &m.cs
	a.nodes[dst.root].supp = 0
	a.nodes[dst.root].children = nilIdx
	path := m.path
	p := &m.paths
	start := int32(0)
	for k, end := range p.end {
		d := len(path)
		for _, x := range p.ids[start:end] { // ids descending
			if i := imap[x]; i >= 0 {
				d--
				path[d] = i
			}
		}
		m.addCmplx(dst, path[d:], p.w[k])
		start = end
	}
	return true, nil
}

// addReord is a port of add_reord: it adds the items whose flags are set
// (in ascending index order), clearing the flags, with weight supp; n is
// the number of set flags.
func (m *miner) addReord(t *csTree, flags []int32, n int, supp tract.Supp) {
	nodes := m.cs.nodes
	node := t.root
	nodes[node].supp += supp
	if n <= 0 {
		return
	}
	var p *int32 // the link to the insertion point (upstream CSNODE **)
	i := 0
	for ; ; i++ {
		if flags[i] == 0 {
			continue
		}
		flags[i] = 0
		p = &nodes[node].children
		c := *p
		for c >= 0 && int(nodes[c].id) < i {
			p = &nodes[c].sibling
			c = *p
		}
		if c < 0 || int(nodes[c].id) != i {
			break
		}
		node = c
		nodes[node].supp += supp
		if n--; n <= 0 {
			return
		}
	}
	m.csInsert(t, p, node, tract.Item(i), supp)
	node = int32(len(m.cs.nodes) - 1)
	if n--; n <= 0 {
		return
	}
	for {
		i++
		if flags[i] == 0 {
			continue
		}
		flags[i] = 0
		c := m.cs.alloc(csNode{
			id: tract.Item(i), supp: supp, parent: node, children: nilIdx,
			sibling: nilIdx, succ: t.heads[i].list,
		})
		m.cs.nodes[node].children = c
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
		return false, m.mine16(dst, id, n, imap)
	}
	a := &m.cs
	a.nodes[dst.root].supp = 0
	a.nodes[dst.root].children = nilIdx
	clear(d)
	p := &m.paths
	start := int32(0)
	for k, end := range p.end {
		n := 0
		for _, x := range p.ids[start:end] {
			if i := imap[x]; i >= 0 {
				d[i] = 1
				n++
			}
		}
		m.addReord(dst, d, n, p.w[k])
		start = end
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
			// Extension: skip the walk if no perfect extension can
			// change a result (rep.PexUseful; e.g. a set of zmax items).
			if m.rep.PexUseful() {
				for node := nodes[h.list].parent; nodes[node].id >= 0; node = nodes[node].parent {
					m.rep.AddPex(t.heads[nodes[node].id].item)
				}
			}
		case proj != nil && m.deeper():
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
		m.left = later(i, t.cnt, m.dir)
		if err := m.rep.Report(); err != nil {
			return err
		}
		m.left = -1
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
	m.cs.nodes = make([]csNode, 0, treePresize(m.bag, true))
	t.root = m.cs.alloc(csNode{
		id: tract.End, parent: nilIdx, succ: nilIdx,
		sibling: nilIdx, children: nilIdx,
	})
	for i := range t.heads {
		t.heads[i] = csHead{item: tract.Item(s[i]), supp: f[s[i]], list: nilIdx}
	}
	if m.pack != 0 {
		m.fim = newMachine(m.dir, m.supp, m.rep)
		m.fim.limited, m.fim.zmax = m.limited, m.zmax
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
