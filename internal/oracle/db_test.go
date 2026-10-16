package oracle

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// test1 is upstream's fpgrowth/ex/test1.tab (also t1.tab of the rules
// audit) with its separators normalized to spaces.
const test1 = `a b c
a d e
b c d
a b c d
b c
a b d
d e
a b c d
c d e
a b c`

// hanData is the AllElectronics example of Han, Kamber and Pei, "Data
// Mining: Concepts and Techniques", Table 6.1 (min_sup = 2).
const hanData = `I1 I2 I5
I2 I4
I2 I3
I1 I2 I4
I1 I3
I2 I3
I1 I3
I1 I2 I3 I5
I1 I2 I3`

// parseTxs returns one unit-weight transaction per line; items are
// separated by spaces and an empty line is an empty transaction.
func parseTxs(data string) []Transaction {
	lines := strings.Split(data, "\n")
	txs := make([]Transaction, len(lines))
	for i, line := range lines {
		txs[i] = Transaction{Items: strings.Fields(line), Weight: 1}
	}
	return txs
}

// mustNew builds a DB or fails the test.
func mustNew(t testing.TB, txs []Transaction, opts Options) *DB {
	t.Helper()
	db, err := New(txs, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return db
}

// parseSets parses expected sets written as "a b:5"; ":10" is the empty
// set with support 10.
func parseSets(t testing.TB, specs ...string) []Set {
	t.Helper()
	sets := make([]Set, 0, len(specs))
	for _, spec := range specs {
		items, supp, ok := strings.Cut(spec, ":")
		n, err := strconv.ParseInt(supp, 10, 64)
		if !ok || err != nil {
			t.Fatalf("bad set spec %q", spec)
		}
		sets = append(sets, Set{Items: NewItemSet(strings.Fields(items)...), Supp: tract.Supp(n)})
	}
	return sets
}

// randomConfig returns a small random generator configuration for
// property tests.
func randomConfig(seed uint64) GenConfig {
	r := rand.New(rand.NewPCG(seed, 42))
	minLen := r.IntN(3)
	return GenConfig{
		Seed:            seed,
		NumTransactions: r.IntN(15),
		NumItems:        1 + r.IntN(8),
		MinLen:          minLen,
		MaxLen:          minLen + r.IntN(5),
		Skew:            r.IntN(3),
		Patterns:        r.IntN(3),
		PatternLen:      1,
		PatternProb:     0.6 * r.Float64(),
		DupItemProb:     0.1,
		DupTxProb:       0.2,
		EmptyProb:       0.1,
		Weights:         WeightDist(r.IntN(4)),
		MaxWeight:       tract.Supp(r.IntN(4)),
		Names:           NameStyle(r.IntN(3)),
	}
}

// forEachRandomDB runs check on n random small datasets.
func forEachRandomDB(t *testing.T, n int, check func(t *testing.T, txs []Transaction, db *DB)) {
	t.Helper()
	for seed := range uint64(n) {
		cfg := randomConfig(seed)
		txs, err := Generate(cfg)
		if err != nil {
			t.Fatalf("seed %d: Generate: %v", seed, err)
		}
		db := mustNew(t, txs, Options{})
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) { check(t, txs, db) })
		if t.Failed() {
			return
		}
	}
}

// naiveSupport sums the weights of the transactions containing all items
// of s, scanning the transactions directly.
func naiveSupport(txs []Transaction, s ItemSet) tract.Supp {
	var supp tract.Supp
	for _, tx := range txs {
		have := map[string]bool{}
		for _, item := range tx.Items {
			have[item] = true
		}
		contains := true
		for _, item := range s {
			contains = contains && have[item]
		}
		if contains {
			supp += tx.Weight
		}
	}
	return supp
}

func TestNew(t *testing.T) {
	t.Parallel()
	txs := []Transaction{
		{Items: []string{"b", "a", "b", "a"}, Weight: 2}, // duplicates ignored
		{Items: []string{}, Weight: 3},                   // empty record counts
		{Items: []string{"c"}, Weight: 0},                // zero weight
		{Items: []string{"a"}, Weight: 1},
	}
	db := mustNew(t, txs, Options{})
	if got, expected := db.Items(), []string{"a", "b", "c"}; !ItemSet(got).Equal(expected) {
		t.Errorf("Items() = %q, want %q", got, expected)
	}
	tests := []struct {
		set      ItemSet
		expected tract.Supp
	}{
		{set: NewItemSet(), expected: 6},
		{set: NewItemSet("a"), expected: 3},
		{set: NewItemSet("b"), expected: 2},
		{set: NewItemSet("a", "b"), expected: 2},
		{set: NewItemSet("c"), expected: 0},
		{set: NewItemSet("a", "c"), expected: 0},
		{set: NewItemSet("x"), expected: 0},
	}
	if got := db.Total(); got != 6 {
		t.Errorf("Total() = %d, want 6", got)
	}
	for _, tt := range tests {
		if got := db.Support(tt.set); got != tt.expected {
			t.Errorf("Support(%v) = %d, want %d", tt.set, got, tt.expected)
		}
	}
}

func TestNew_selection(t *testing.T) {
	t.Parallel()
	// Upstream (tract.md §2.3): a / x / x y with selection a gives a (1 33.3333).
	db := mustNew(t, parseTxs("a\nx\nx y"), Options{Selection: []string{"a", "never"}})
	if got := db.Items(); !ItemSet(got).Equal(NewItemSet("a")) {
		t.Errorf("Items() = %q, want [a]", got)
	}
	if got := db.Total(); got != 3 {
		t.Errorf("Total() = %d, want 3", got)
	}
	if got := db.Support(NewItemSet("a")); got != 1 {
		t.Errorf("Support(a) = %d, want 1", got)
	}
	if got := db.Support(NewItemSet("x")); got != 0 {
		t.Errorf("Support(x) = %d, want 0 (not selected)", got)
	}

	empty := mustNew(t, parseTxs("a\nb"), Options{Selection: []string{}})
	if len(empty.Items()) != 0 || empty.Total() != 2 {
		t.Errorf("empty selection: items %q, total %d; want none, 2", empty.Items(), empty.Total())
	}
}

func TestNew_errors(t *testing.T) {
	t.Parallel()
	tooMany := make([]string, MaxItems+1)
	for i := range tooMany {
		tooMany[i] = letters(i)
	}
	tests := []struct {
		name     string
		txs      []Transaction
		opts     Options
		expected error
	}{
		{
			name:     "negative weight",
			txs:      []Transaction{{Items: []string{"a"}, Weight: -1}},
			expected: ErrNegativeWeight,
		},
		{
			name: "weight overflow",
			txs: []Transaction{
				{Items: []string{"a"}, Weight: math.MaxInt64},
				{Items: []string{"b"}, Weight: 1},
			},
			expected: ErrWeightOverflow,
		},
		{
			name:     "too many items",
			txs:      []Transaction{{Items: tooMany, Weight: 1}},
			expected: ErrTooManyItems,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(tt.txs, tt.opts); !errors.Is(err, tt.expected) {
				t.Errorf("New() error = %v, want %v", err, tt.expected)
			}
		})
	}

	// The limit counts selected items only.
	if _, err := New([]Transaction{{Items: tooMany, Weight: 1}}, Options{Selection: tooMany[:MaxItems]}); err != nil {
		t.Errorf("New() with %d selected items: %v", MaxItems, err)
	}
}

func TestDB_Support_matchesNaiveScan(t *testing.T) {
	t.Parallel()
	forEachRandomDB(t, 500, func(t *testing.T, txs []Transaction, db *DB) {
		var total tract.Supp
		for _, tx := range txs {
			total += tx.Weight
		}
		if db.Total() != total {
			t.Fatalf("Total() = %d, want %d", db.Total(), total)
		}
		for m := range uint64(len(db.supp)) {
			s := db.set(m)
			if got, expected := db.Support(s), naiveSupport(txs, s); got != expected {
				t.Fatalf("Support(%v) = %d, want %d", s, got, expected)
			}
		}
	})
}

func TestDB_Support_antiMonotone(t *testing.T) {
	t.Parallel()
	forEachRandomDB(t, 500, func(t *testing.T, _ []Transaction, db *DB) {
		for m := range uint64(len(db.supp)) {
			for i := range db.items {
				if sup := m | 1<<i; db.supp[sup] > db.supp[m] {
					t.Fatalf("supp(%v) = %d > supp(%v) = %d", db.set(sup), db.supp[sup], db.set(m), db.supp[m])
				}
			}
		}
	})
}
