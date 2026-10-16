package report

import (
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

func TestGeneratorTable(t *testing.T) {
	t.Parallel()
	g := newGeneratorTable()
	key := func(items ...tract.Item) []tract.Item { return items }
	if _, ok := g.lookup(key(1)); ok {
		t.Fatal("lookup in empty table succeeded")
	}
	// keys are order sensitive, like is_cmp
	g.insert(key(1, 2), 5)
	if s, ok := g.lookup(key(1, 2)); !ok || s != 5 {
		t.Fatalf("lookup {1 2} = %d %v", s, ok)
	}
	if _, ok := g.lookup(key(2, 1)); ok {
		t.Fatal("lookup {2 1} found the key {1 2}")
	}
	if _, ok := g.lookup(key(1)); ok {
		t.Fatal("prefix of a key found")
	}
	g.insert(key(1, 2), 7) // existing key: support replaced
	if s, _ := g.lookup(key(1, 2)); s != 7 || g.used != 1 {
		t.Fatalf("re-insert: supp %d used %d", s, g.used)
	}
	// growth keeps all keys
	const n = 5000
	for i := range n {
		g.insert(key(tract.Item(i), tract.Item(i%7), 3), tract.Supp(i))
	}
	for i := range n {
		if s, ok := g.lookup(key(tract.Item(i), tract.Item(i%7), 3)); !ok || s != tract.Supp(i) {
			t.Fatalf("after growth: key %d -> %d %v", i, s, ok)
		}
	}
	if len(g.slots) < 2*g.used {
		t.Fatalf("load factor too high: %d slots for %d keys", len(g.slots), g.used)
	}
}
