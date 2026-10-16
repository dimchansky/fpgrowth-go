package fpg

import "github.com/dimchansky/fpgrowth-go/internal/tract"

// recSingle is a port of rec_single: it processes heads [start, n) of the
// single tree t in place (no projections are allocated). Heads below the
// current item are relinked to the nodes of its conditional database.
func (m *miner) recSingle(t *fpTree, n int) error {
	if err := m.aborted(); err != nil {
		return err
	}
	start := 0
	if t.fim != nil {
		start = 1 // skip the packed items
	}
	for i := start; i < n; i++ {
		h := &t.heads[i]
		if h.supp < m.supp {
			continue // skip infrequent items
		}
		if !m.rep.Add(h.item, h.supp) {
			continue
		}
		nodes := m.fp.nodes
		node := h.list
		if nodes[node].succ < 0 { // projection would be a chain
			for anc := nodes[node].parent; nodes[anc].id > tract.End; anc = nodes[anc].parent {
				if k := nodes[anc].id; k >= 0 {
					m.rep.AddPex(t.heads[k].item)
				} else {
					m.rep.AddPexPacked(k)
				}
			}
		} else if i > 0 && m.rep.Xable(1) {
			if err := m.projectSingle(t, i); err != nil {
				return err
			}
		}
		if err := m.rep.Report(); err != nil {
			return err
		}
		m.rep.Remove(1)
	}
	return nil
}

// projectSingle is the projection step of rec_single for head i: relink
// heads 0..i-1 to the conditional database of i, collect the perfect
// extensions, mine the packed items and recurse.
func (m *miner) projectSingle(t *fpTree, i int) error {
	nodes := m.fp.nodes
	for k := range i { // clear item heads and support
		t.heads[k].supp = 0
		t.heads[k].list = nilIdx
	}
	for node := t.heads[i].list; node >= 0; node = nodes[node].succ {
		w := nodes[node].supp
		anc := nodes[node].parent
		for ; nodes[anc].id > tract.End; anc = nodes[anc].parent {
			id := nodes[anc].id
			if id < 0 { // packed items go to the 16-items machine
				t.fim.add(uint16(id.PackedBits()), w)
				continue
			}
			h := &t.heads[id]
			if h.list == anc {
				break // rest of the path already linked
			}
			nodes[anc].supp = w
			h.supp += w
			nodes[anc].succ = h.list
			h.list = anc
		}
		for ; nodes[anc].id > tract.End; anc = nodes[anc].parent {
			id := nodes[anc].id
			if id < 0 {
				t.fim.add(uint16(id.PackedBits()), w)
				continue
			}
			t.heads[id].supp += w
			nodes[anc].supp += w
		}
	}
	pex := m.perfectThreshold(t.heads[i].supp)
	k := 0
	if t.fim != nil {
		k = 1 // skip packed items
	}
	cnt := 0
	for ; k < i; k++ {
		h := &t.heads[k]
		if h.supp < m.supp {
			continue
		}
		if h.supp < pex {
			cnt++ // count the frequent items
			continue
		}
		h.supp = 0 // collect the perfect extensions
		m.rep.AddPex(h.item)
	}
	if t.fim != nil {
		if _, err := t.fim.mine(); err != nil {
			return err
		}
	}
	if cnt > 0 {
		return m.recSingle(t, i)
	}
	return nil
}

// mineSingle is a port of fpg_single.
func (m *miner) mineSingle() error {
	usePack := m.pack != 0
	d, s, f, cont, err := m.topLevel(usePack)
	if !cont {
		return err
	}
	m.dir = 1 // only upward item loops possible
	k := len(s)
	t := &fpTree{cnt: k, dir: m.dir, heads: make([]fpHead, k)}
	m.fp.nodes = make([]fpNode, 0, presize(m.bag.Extent()+1))
	t.root = m.fp.alloc(fpNode{id: tract.End, parent: nilIdx, succ: nilIdx})
	if usePack {
		t.fim = newMachine(m.dir, m.supp, m.rep)
	}
	for i := range t.heads {
		t.heads[i] = fpHead{item: tract.Item(s[i]), supp: f[s[i]], list: nilIdx}
	}
	buf := m.path
	for items, w := range m.bag.Backward() {
		n := 0
		for _, p := range items {
			if p < 0 { // packed items go to the 16-items machine
				buf[n] = int32(p)
				n++
			} else if x := d[p]; x >= 0 {
				buf[n] = x
				n++
			}
		}
		m.addSmp16(t, buf[:n], w)
	}
	if t.fim != nil {
		if _, err := t.fim.mine(); err != nil {
			return err
		}
	}
	if err := m.recSingle(t, t.cnt); err != nil {
		return err
	}
	return m.rep.Report()
}
