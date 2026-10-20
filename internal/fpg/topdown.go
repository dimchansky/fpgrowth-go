package fpg

import "github.com/dimchansky/fpgrowth-go/internal/tract"

// tdNode is a node of a top-down tree (upstream TDNODE). The tree is
// stored reversed (highest ids at the root level); sibling lists are
// sorted descending by id.
type tdNode struct {
	id       int32
	children int32
	sibling  int32
	supp     tract.Supp
}

// tdTree is a top-down tree (upstream TDTREE): root is the first node of
// the root level, items maps node ids to reporter items.
type tdTree struct {
	cnt   int
	root  int32
	items []tract.Item
}

// tdArena holds the nodes of all top-down trees of one run.
type tdArena struct{ nodes []tdNode }

func (a *tdArena) alloc(n tdNode) int32 {
	checkArena(len(a.nodes))
	a.nodes = append(a.nodes, n)
	return int32(len(a.nodes) - 1)
}

// tdItemBuf returns the reusable item map buffer of recursion depth depth.
func (m *miner) tdItemBuf(depth, n int) []tract.Item {
	for len(m.tdItems) <= depth {
		m.tdItems = append(m.tdItems, nil)
	}
	if cap(m.tdItems[depth]) < n {
		m.tdItems[depth] = make([]tract.Item, n)
	}
	return m.tdItems[depth][:n]
}

// addTopDown is a port of add_topdn: it inserts the transaction ids
// (ascending) from its last item.
func (m *miner) addTopDown(t *tdTree, ids []int32, supp tract.Supp) {
	a := &m.td
	n := len(ids)
	link := nilIdx          // nilIdx: t.root, else the children field of link
	var prev int32 = nilIdx // node whose sibling field is the position
	var i int32
	var cur int32
	for {
		if n--; n < 0 {
			return
		}
		i = ids[n]
		prev = nilIdx
		cur = t.root
		if link >= 0 {
			cur = a.nodes[link].children
		}
		for cur >= 0 && a.nodes[cur].id > i {
			prev = cur
			cur = a.nodes[cur].sibling
		}
		if cur < 0 || a.nodes[cur].id != i {
			break
		}
		a.nodes[cur].supp += supp
		link = cur
	}
	node := a.alloc(tdNode{id: i, supp: supp, sibling: cur, children: nilIdx})
	switch {
	case prev >= 0:
		a.nodes[prev].sibling = node
	case link >= 0:
		a.nodes[link].children = node
	default:
		t.root = node
	}
	for n--; n >= 0; n-- {
		c := a.alloc(tdNode{id: ids[n], supp: supp, sibling: nilIdx, children: nilIdx})
		a.nodes[node].children = c
		node = c
	}
}

// getSupp is a port of getsupp: it sums the supports per item over the
// node list and its subtrees.
func (m *miner) getSupp(node int32, supp []tract.Supp) {
	nodes := m.td.nodes
	for ; node >= 0; node = nodes[node].sibling {
		supp[nodes[node].id] += nodes[node].supp
		m.getSupp(nodes[node].children, supp)
	}
}

// tdMerge is a port of merge: it merges two node lists (destructively),
// summing the supports of equal items and merging their children.
func (m *miner) tdMerge(s1, s2 int32) int32 {
	if s1 < 0 {
		return s2
	}
	if s2 < 0 {
		return s1
	}
	nodes := m.td.nodes
	out, last := nilIdx, nilIdx
	link := func(x int32) {
		if last < 0 {
			out = x
		} else {
			nodes[last].sibling = x
		}
		last = x
	}
	for {
		switch {
		case nodes[s1].id > nodes[s2].id:
			link(s1)
			s1 = nodes[s1].sibling
		case nodes[s2].id > nodes[s1].id:
			link(s2)
			s2 = nodes[s2].sibling
		default: // item occurs in both lists
			nodes[s1].children = m.tdMerge(nodes[s1].children, nodes[s2].children)
			nodes[s1].supp += nodes[s2].supp
			link(s1)
			s1 = nodes[s1].sibling
			s2 = nodes[s2].sibling
		}
		if s1 < 0 || s2 < 0 {
			break
		}
	}
	if s1 >= 0 {
		link(s1)
	} else {
		link(s2)
	}
	return out
}

// tdCopy is a port of copy: it copies the node list src with the item map
// imap, dropping eliminated items and merging their copied children into
// the result.
func (m *miner) tdCopy(src int32, imap []int32) int32 {
	dst, last := nilIdx, nilIdx
	b := nilIdx // buffer for children of dropped nodes
	for {
		c := m.td.nodes[src].children
		if c >= 0 {
			c = m.tdCopy(c, imap)
		}
		if i := imap[m.td.nodes[src].id]; i >= 0 {
			node := m.td.alloc(tdNode{
				id: i, supp: m.td.nodes[src].supp, children: c, sibling: nilIdx,
			})
			if last < 0 {
				dst = node
			} else {
				m.td.nodes[last].sibling = node
			}
			last = node
		} else if c >= 0 {
			if b < 0 {
				b = c
			} else {
				b = m.tdMerge(b, c)
			}
		}
		src = m.td.nodes[src].sibling
		if src < 0 {
			break
		}
	}
	switch {
	case b < 0:
		return dst
	case dst < 0:
		return b
	default:
		return m.tdMerge(dst, b)
	}
}

// recTopDown is a port of rec_topdn: it consumes the tree, always taking
// the root node (the highest remaining id).
func (m *miner) recTopDown(t *tdTree, depth int) error {
	if err := m.aborted(); err != nil {
		return err
	}
	if m.mode&ModeTail != 0 { // head union tail pruning
		tb := m.tailBuf()
		for i := range t.cnt {
			tb.push(t.items[i])
		}
		if m.rep.Tail(tb.items()) {
			return nil
		}
	}
	var proj *tdTree
	mark := len(m.td.nodes)
	if t.cnt > 1 && m.xable2() {
		proj = &tdTree{items: m.tdItemBuf(depth+1, t.cnt-1)}
	}
	for node := t.root; node >= 0; node = t.root {
		// Every node of this loop costs a merge of its subtrees, which
		// at the top level is proportional to the size of the tree and
		// does not reach a recursion entry when nothing is projected
		// (e.g. with a maximum size of 1): check before each one.
		if err := m.aborted(); err != nil {
			return err
		}
		nd := m.td.nodes[node]
		if !m.rep.Add(t.items[nd.id], nd.supp) {
			if err := m.rep.Err(); err != nil {
				return err // latched abort or repository overflow
			}
			t.root = m.tdMerge(nd.sibling, nd.children)
			continue
		}
		if proj != nil && nd.children >= 0 && m.deeper() {
			s := m.cis[:nd.id]
			clear(s)
			m.getSupp(nd.children, s)
			pex := m.perfectThreshold(nd.supp)
			imap := m.imap
			k := 0
			for i := range int(nd.id) {
				if s[i] < m.supp {
					imap[i] = -1
					continue
				}
				if s[i] >= pex {
					imap[i] = -1
					m.rep.AddPex(t.items[i])
					continue
				}
				imap[i] = int32(k)
				proj.items[k] = t.items[i]
				k++
			}
			if k > 0 {
				proj.cnt = k
				proj.root = m.tdCopy(nd.children, imap)
				err := m.recTopDown(proj, depth+1)
				m.td.nodes = m.td.nodes[:mark] // release the projection
				if err != nil {
					return err
				}
			}
		}
		m.left = int(nd.id) // the items left in the tree have lower ids
		if err := m.rep.Report(); err != nil {
			return err
		}
		m.left = -1
		m.rep.Remove(1)
		nd = m.td.nodes[node]
		t.root = m.tdMerge(nd.sibling, nd.children)
	}
	return nil
}

// mineTopDown is a port of fpg_topdn.
func (m *miner) mineTopDown() error {
	d, s, _, cont, err := m.topLevel(false)
	if !cont {
		return err
	}
	m.dir = 1 // only upward item loops possible
	k := len(s)
	// Deviation from upstream (fpgrowth-core.md B1): fpg_topdn copies the
	// old -> new map d into the item map instead of the new -> old map s,
	// which crashes or prints wrong items whenever an item below the last
	// code is eliminated at the top level (e.g. a perfect extension).
	items := make([]tract.Item, k)
	for i, x := range s {
		items[i] = tract.Item(x)
	}
	t := &tdTree{cnt: k, root: nilIdx, items: items}
	m.td.nodes = make([]tdNode, 0, presize(2*m.bag.Extent()+16))
	buf := m.path
	nt := 0
	for tx, w := range m.bag.Backward() {
		if nt++; nt%buildCheckEvery == 0 {
			if err := m.aborted(); err != nil {
				return err
			}
		}
		n := 0
		for _, p := range tx {
			if x := d[p]; x >= 0 {
				buf[n] = x
				n++
			}
		}
		m.addTopDown(t, buf[:n], w)
	}
	if err := m.recTopDown(t, 0); err != nil {
		return err
	}
	return m.rep.Report() // report the empty item set
}
