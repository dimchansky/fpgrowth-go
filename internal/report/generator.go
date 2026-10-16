package report

import "github.com/dimchansky/fpgrowth-go/internal/tract"

// generatorTable replaces the symbol table (symtab.c with is_hash and
// is_cmp) that upstream uses as the generator repository. Keys are item
// sequences compared in order, exactly like is_cmp; the hash function is
// irrelevant for the results. Keys are stored back to back in one arena and
// addressed from an open-addressing table, so inserting a generator does not
// allocate per key (only amortized growth of the two slices).
type generatorTable struct {
	keys  []tract.Item // concatenated keys
	slots []genSlot    // length is a power of two
	used  int
}

type genSlot struct {
	off  int        // offset of the key in keys
	n    int32      // key length; 0 marks an empty slot
	hash uint32     // high bits of the key hash
	supp tract.Supp // stored support
}

const genInitialSlots = 1024

func newGeneratorTable() *generatorTable {
	return &generatorTable{slots: make([]genSlot, genInitialSlots)}
}

// hashItems hashes an item sequence (order dependent, like is_hash).
func hashItems(key []tract.Item) uint64 {
	h := uint64(len(key)) * 0x9e3779b97f4a7c15
	for _, it := range key {
		h ^= uint64(uint32(it))
		h *= 0x100000001b3
		h ^= h >> 29
	}
	h ^= h >> 32
	h *= 0xd6e8feb86659fd93
	h ^= h >> 32
	return h
}

// find returns the slot index holding key, or the empty slot where key
// would be inserted, and whether key was found.
func (g *generatorTable) find(key []tract.Item, h uint64) (int, bool) {
	mask := len(g.slots) - 1
	hi := uint32(h >> 32)
	for i := int(h) & mask; ; i = (i + 1) & mask {
		s := &g.slots[i]
		if s.n == 0 {
			return i, false
		}
		if s.hash == hi && int(s.n) == len(key) && equalItems(g.keys[s.off:s.off+len(key)], key) {
			return i, true
		}
	}
}

func equalItems(a, b []tract.Item) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// lookup is the equivalent of st_lookup: it returns the support stored for
// key and whether key is present.
func (g *generatorTable) lookup(key []tract.Item) (tract.Supp, bool) {
	i, ok := g.find(key, hashItems(key))
	if !ok {
		return 0, false
	}
	return g.slots[i].supp, true
}

// insert is the equivalent of st_insert followed by storing the support.
// key must not be empty. An existing key gets the new support (upstream
// would write through its EXISTS sentinel, which cannot happen in valid
// call sequences).
func (g *generatorTable) insert(key []tract.Item, supp tract.Supp) {
	if (g.used+1)*2 > len(g.slots) {
		g.grow()
	}
	h := hashItems(key)
	i, ok := g.find(key, h)
	if ok {
		g.slots[i].supp = supp
		return
	}
	off := len(g.keys)
	g.keys = append(g.keys, key...)
	g.slots[i] = genSlot{off: off, n: int32(len(key)), hash: uint32(h >> 32), supp: supp}
	g.used++
}

func (g *generatorTable) grow() {
	old := g.slots
	g.slots = make([]genSlot, 2*len(old))
	mask := len(g.slots) - 1
	for _, s := range old {
		if s.n == 0 {
			continue
		}
		h := hashItems(g.keys[s.off : s.off+int(s.n)])
		i := int(h) & mask
		for g.slots[i].n != 0 {
			i = (i + 1) & mask
		}
		g.slots[i] = s
	}
}
