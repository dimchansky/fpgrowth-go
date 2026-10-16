package report

import (
	"slices"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// treeSets lists the paths of t as (items, supp) pairs in depth-first
// order, with the root first.
func treeSets(t *cmTree) []string {
	var out []string
	var walk func(n int32, prefix []tract.Item)
	walk = func(n int32, prefix []tract.Item) {
		for ; n >= 0; n = t.nodes[n].sibling {
			p := append(slices.Clone(prefix), t.nodes[n].item)
			out = append(out, fmtSet(p, t.nodes[n].supp))
			walk(t.nodes[n].children, p)
		}
	}
	out = append(out, fmtSet(nil, t.nodes[0].supp))
	walk(t.nodes[0].children, nil)
	return out
}

func fmtSet(items []tract.Item, s tract.Supp) string {
	b := []byte("{")
	for i, it := range items {
		if i > 0 {
			b = append(b, ' ')
		}
		b = append(b, byte('0'+it))
	}
	b = append(b, '}', ':')
	return string(append(b, []byte(fmtFloat(float64(s)))...))
}

func TestCMTree_AddGet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		desc bool
		get  []tract.Item
		want tract.Supp
	}{
		{"desc top", true, []tract.Item{4}, 5},
		{"desc pair", true, []tract.Item{4, 2}, 4},
		{"desc triple", true, []tract.Item{4, 2, 1}, 3},
		{"desc other branch", true, []tract.Item{4, 3}, 5},
		{"desc single", true, []tract.Item{2}, 6},
		{"desc missing", true, []tract.Item{4, 1}, -1},
		{"desc root", true, nil, 6},
		{"asc triple", false, []tract.Item{1, 2, 4}, 3},
		{"asc shared node", false, []tract.Item{2}, 6},
		{"asc pair", false, []tract.Item{3, 4}, 5},
		{"asc missing", false, []tract.Item{4}, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := newCMTree(tt.desc)
			dir := 1
			if tt.desc {
				dir = -1
			}
			for _, s := range []struct {
				items []tract.Item
				supp  tract.Supp
			}{{[]tract.Item{4, 2, 1}, 3}, {[]tract.Item{4, 3}, 5}, {[]tract.Item{4, 2}, 4}, {[]tract.Item{2}, 6}} {
				items := slices.Clone(s.items)
				sortItems(items, dir)
				tr.add(items, s.supp)
			}
			if tr.max != 6 || tr.nodes[0].supp != 6 {
				t.Fatalf("max %d root %d, want 6", tr.max, tr.nodes[0].supp)
			}
			if got := tr.get(tt.get); got != tt.want {
				t.Errorf("get(%v) = %d, want %d", tt.get, got, tt.want)
			}
		})
	}
}

func TestCMTree_PruneMerges(t *testing.T) {
	t.Parallel()
	tr := newCMTree(true)
	tr.add([]tract.Item{5, 3, 1}, 2)
	tr.add([]tract.Item{5, 2}, 4)
	tr.add([]tract.Item{4, 3}, 3)
	tr.add([]tract.Item{3, 1}, 1)
	tr.pruneTo(3)
	// items 5 and 4 are merged away; their subtrees join the list
	want := []string{"{}:4", "{3}:3", "{3 1}:2", "{2}:4"}
	if got := treeSets(tr); !slices.Equal(got, want) {
		t.Fatalf("after pruneTo(3): %v, want %v", got, want)
	}
	if tr.max != 3 || tr.item != 3 {
		t.Fatalf("max %d item %d, want 3 3", tr.max, tr.item)
	}
	tr.pruneTo(2)
	if tr.max != 4 {
		t.Fatalf("max after pruneTo(2) = %d, want 4", tr.max)
	}
	tr.pruneTo(0)
	if tr.max != cmNone {
		t.Fatalf("max after pruneTo(0) = %d, want %d", tr.max, cmNone)
	}
	// freed nodes are reused
	n := len(tr.nodes)
	tr.add([]tract.Item{0}, 1)
	if len(tr.nodes) != n {
		t.Fatalf("add did not reuse a freed node: %d -> %d nodes", n, len(tr.nodes))
	}
}

func TestProjectTree(t *testing.T) {
	t.Parallel()
	src := newCMTree(true)
	src.item = cmValid
	src.add([]tract.Item{4, 3, 1}, 2)
	src.add([]tract.Item{4, 2}, 5)
	src.add([]tract.Item{3, 2}, 3)
	dst := projectTree(nil, src, 4, nil)
	if got, want := treeSets(dst), []string{"{}:5", "{3}:2", "{3 1}:2", "{2}:5"}; !slices.Equal(got, want) {
		t.Fatalf("projection: %v, want %v", got, want)
	}
	if !dst.valid() || dst.max != cmNone || src.max != 5 {
		t.Fatalf("dst valid %v max %d, src max %d", dst.valid(), dst.max, src.max)
	}
	// item 4 is removed from the source, its subtree merged in
	if got, want := treeSets(src), []string{"{}:5", "{3}:3", "{3 2}:3", "{3 1}:2", "{2}:5"}; !slices.Equal(got, want) {
		t.Fatalf("source after projection: %v, want %v", got, want)
	}
	// projection with keep flags merges dropped nodes into their parents
	keep := make([]bool, 5)
	keep[1] = true
	x := projectTree(nil, src, 3, keep)
	if got, want := treeSets(x), []string{"{}:3", "{1}:2"}; !slices.Equal(got, want) {
		t.Fatalf("keep projection: %v, want %v", got, want)
	}
	// a missing projection item gives an empty valid tree
	e := projectTree(nil, src, 0, nil)
	if got := treeSets(e); !slices.Equal(got, []string{"{}:0"}) || !e.valid() {
		t.Fatalf("empty projection: %v", got)
	}
}

func TestClosedMaximal_Protocol(t *testing.T) {
	t.Parallel()
	cm := newClosedMaximal(-1, 5)
	if cm.supp() != 0 || cm.stored {
		t.Fatalf("new repository: supp %d stored %v", cm.supp(), cm.stored)
	}
	// prefix {4, 2}, store {4, 2, 1}:3
	if !cm.add(4, 6) || !cm.add(2, 4) {
		t.Fatal("add on empty repository failed")
	}
	if cm.supp() != cmNone {
		t.Fatalf("supp with no superset = %d", cm.supp())
	}
	cm.update([]tract.Item{4, 2, 1}, 3)
	if !cm.stored || cm.supp() != 3 {
		t.Fatalf("after update: stored %v supp %d", cm.stored, cm.supp())
	}
	cm.remove(1)
	if cm.cnt != 1 || cm.supp() != 3 {
		t.Fatalf("after remove: cnt %d supp %d", cm.cnt, cm.supp())
	}
	// {4, 1} with support 3 has the stored superset {4, 2, 1}:3
	if cm.add(1, 3) {
		t.Fatal("add of a dominated item succeeded")
	}
	if !cm.add(1, 4) {
		t.Fatal("add with larger support failed")
	}
	cm.remove(5) // more than cnt: back to the empty prefix
	if cm.cnt != 0 {
		t.Fatalf("cnt after remove(5) = %d", cm.cnt)
	}
	if got := cm.tail(nil); got != 1 {
		t.Fatalf("empty tail = %d, want 1", got)
	}
	if got := cm.tail([]tract.Item{3}); got != 0 {
		t.Fatalf("tail with empty prefix = %d, want 0", got)
	}
}

func TestClosedMaximal_Tail(t *testing.T) {
	t.Parallel()
	cm := newClosedMaximal(-1, 6)
	if !cm.add(5, 7) {
		t.Fatal("add 5 failed")
	}
	cm.update([]tract.Item{5, 4, 2, 1}, 3)
	cm.remove(1)
	if !cm.add(4, 6) {
		t.Fatal("add 4 failed")
	}
	// {4} ∪ {2, 1} is covered by the stored {5, 4, 2, 1}
	if got := cm.tail([]tract.Item{2, 1}); got != 3 {
		t.Fatalf("tail {2 1} under {4} = %d, want 3", got)
	}
	if got := cm.tail([]tract.Item{3}); got != 0 {
		t.Fatalf("tail {3} under {4} = %d, want 0", got)
	}
}

func TestClosedMaximal_Ascending(t *testing.T) {
	t.Parallel()
	cm := newClosedMaximal(+1, 4)
	if !cm.add(0, 5) || !cm.add(1, 3) {
		t.Fatal("add failed")
	}
	cm.update([]tract.Item{0, 1, 3}, 3)
	cm.remove(1)
	if cm.add(3, 3) {
		t.Fatal("{0 3} is dominated by {0 1 3}")
	}
	if got := cm.tail([]tract.Item{2, 3}); got != 0 {
		t.Fatalf("tail {2 3} = %d, want 0", got)
	}
}
