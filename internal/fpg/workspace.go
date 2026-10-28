package fpg

import (
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Workspace keeps the buffers of an item set mining run (the node arenas
// of the FP-trees, the per-depth header buffers, the scratch arrays and
// the 16-items machines) for the runs of later miners, which then
// allocate only what the earlier ones did not need. fpgpsp mines
// thousands of surrogate data sets of the same shape one after the other
// (internal/psp); a fresh miner for each allocated about as much as it
// mined, and the garbage collector's work was a large part of the run.
//
// A Workspace is used by one Mine call at a time, never concurrently;
// the results of a miner do not depend on whether or which Workspace it
// uses. The zero value is ready to use. The item set tree path (rules
// and rule-based evaluation) does not use it.
type Workspace struct {
	buf     buffers
	fim     [2]*machine // 16-items machines for dir > 0 and dir <= 0
	running bool
}

// buffers are the reusable buffers of a miner.
type buffers struct {
	// Scratch buffers (upstream fpg->set, fpg->map, fpg->cis).
	set   []int32      // reorder map and flags (2k)
	imap  []int32      // old -> new item map of a projection (k)
	cis   []tract.Supp // conditional item supports (k)
	path  []int32      // projected transaction, filled backwards (k)
	bit16 []uint16     // 16-items machine bit of each item of a projection (k)
	tail  []tract.Item // head union tail items, filled backwards (k+16)

	paths projPaths // paths of the current projection

	fp fpArena // nodes of the simple and single variants
	cs csArena // nodes of the complex variant
	td tdArena // nodes of the top-down variant

	fpHeads [][]fpHead     // per recursion depth (projection headers)
	csHeads [][]csHead     // per recursion depth
	tdItems [][]tract.Item // per recursion depth
}

// SetWorkspace makes Mine take its buffers from ws and leave them there
// for the next miner; nil (the default) allocates them for this run.
func (m *Miner[T]) SetWorkspace(ws *Workspace) { m.ws = ws }

// lend gives the buffers of ws to m for one run.
func (ws *Workspace) lend(m *miner) {
	if ws.running {
		panic("fpg: Workspace used by two runs at once")
	}
	ws.running = true
	m.buffers = ws.buf
	m.ws = ws
}

// takeBack stores the buffers of m, which may have grown, in ws.
func (ws *Workspace) takeBack(m *miner) {
	b := m.buffers
	b.fp.nodes = b.fp.nodes[:0]
	b.cs.nodes = b.cs.nodes[:0]
	b.td.nodes = b.td.nodes[:0]
	ws.buf = b
	ws.running = false
}

// zeroed returns a zeroed slice of length n, reusing the array of s if
// it is large enough.
func zeroed[E any](s []E, n int) []E {
	if cap(s) < n {
		return make([]E, n)
	}
	s = s[:n]
	clear(s)
	return s
}

// arena returns an empty node arena with room for at least n nodes,
// reusing the array of s if it is large enough.
func arena[E any](s []E, n int) []E {
	if cap(s) < n {
		return make([]E, 0, n)
	}
	return s[:0]
}

// newMachine returns a 16-items machine for the direction dir, from the
// workspace if there is one (newMachine, cleared).
func (m *miner) newMachine(dir int, smin tract.Supp, rep *report.Reporter) *machine {
	if m.ws == nil {
		return newMachine(dir, smin, rep)
	}
	x := 0
	if dir <= 0 {
		x = 1
	}
	f := m.ws.fim[x]
	if f == nil {
		f = newMachine(dir, smin, rep)
		m.ws.fim[x] = f
		return f
	}
	f.reset(smin, rep)
	return f
}

// reset makes f equal to a new machine of its direction for the minimum
// support smin and the reporter rep.
func (f *machine) reset(smin tract.Supp, rep *report.Reporter) {
	f.rep, f.limited, f.zmax = rep, false, 0
	for i := range f.imap {
		f.imap[i] = tract.Item(i)
	}
	for k := range f.subs {
		cur := &f.subs[k]
		clear(cur.wgts)
		for i := range cur.lists {
			cur.lists[i] = cur.lists[i][:0]
		}
		cur.supps = [16]tract.Supp{}
		cur.ttw, cur.tor, cur.smin = 0, 0, smin
	}
}
