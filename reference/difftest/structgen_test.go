package difftest

import (
	"testing"
)

// itemSupports returns the weighted support of every item and pair
// (key "a" or "a\x00b" with a < b) and the total weight.
func itemSupports(txs []transaction) (map[string]int64, int64) {
	s := map[string]int64{}
	var total int64
	for _, tx := range txs {
		total += tx.weight
		seen := map[string]bool{}
		for _, it := range tx.items {
			seen[it] = true
		}
		for a := range seen {
			s[a] += tx.weight
			for b := range seen {
				if a < b {
					s[a+"\x00"+b] += tx.weight
				}
			}
		}
	}
	return s, total
}

func TestGenIndependent(t *testing.T) {
	t.Parallel()
	for seed := range uint64(200) {
		r := newRNG(seed)
		n := 2 + int(seed%12)
		names := make([]string, n)
		for i := range names {
			names[i] = letters(i)
		}
		weighted := seed%2 == 0
		txs := genIndependent(r, names, weighted)
		s, total := itemSupports(txs)
		independent := 0
		for i, a := range names {
			for _, b := range names[i+1:] {
				if s[a+"\x00"+b]*total == s[a]*s[b] {
					independent++
				}
			}
		}
		// items of different groups (two or three round-robin groups)
		if want := (n / 2) * ((n + 1) / 2); independent < want {
			t.Errorf("seed %d, %d items: %d exactly independent pairs, want at least %d", seed, n, independent, want)
		}
		for _, tx := range txs {
			if !weighted && tx.weight != 1 {
				t.Fatalf("seed %d: unweighted dataset has weight %d", seed, tx.weight)
			}
		}
		if total > 1000 {
			t.Errorf("seed %d: total weight %d", seed, total)
		}
	}
}

func TestGenTies(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b", "c", "d", "e", "f", "g"}
	for seed := range uint64(100) {
		r := newRNG(seed)
		check := func(name string, txs []transaction) {
			t.Helper()
			s, _ := itemSupports(txs)
			for _, it := range names[1:] {
				if s[it] != s[names[0]] {
					t.Errorf("seed %d %s: support of %s %d, of a %d", seed, name, it, s[it], s[names[0]])
				}
			}
		}
		k := 1 + r.intN(20)
		txs := genEqualFreq(r, names, 20, k, seed%2 == 0)
		if len(txs) != 20 {
			t.Errorf("seed %d: %d transactions", seed, len(txs))
		}
		check("equal frequency", txs)
		check("cyclic", genCyclic(r, names, seed%3 == 0))
	}
	txs := genBlocks(newRNG(1), names, 50, 3, 1, 0)
	for _, tx := range txs {
		seen := map[string]bool{}
		for _, it := range tx.items {
			if seen[it] {
				t.Fatalf("duplicate item %s in %v", it, tx.items)
			}
			seen[it] = true
		}
	}
}
