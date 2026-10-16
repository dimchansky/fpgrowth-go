package oracle

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// MaxItems is the largest item universe New accepts. The support table has
// 2^MaxItems entries (8 MiB of supports at the limit).
const MaxItems = 20

// Errors returned by New.
var (
	ErrTooManyItems   = errors.New("oracle: too many distinct items")
	ErrNegativeWeight = errors.New("oracle: negative transaction weight")
	ErrWeightOverflow = errors.New("oracle: total transaction weight overflows")
)

// Transaction is one input record: item names and a weight.
type Transaction struct {
	// Items holds the item names. Duplicates are allowed and ignored (the
	// first occurrence counts), like upstream ignores repeated items within
	// a record. The order is irrelevant for supports.
	Items []string
	// Weight is the multiplicity of the transaction; it must be >= 0.
	Weight tract.Supp
}

// Options configures New.
type Options struct {
	// Selection, if non-nil, restricts the item universe to the listed
	// items (upstream's item selection file, -R for item set targets).
	// Other items are removed from every transaction, but the transactions
	// still count toward the total weight, also when they become empty.
	// Selected items that never occur are not part of the universe (their
	// support is 0, so they can never be frequent).
	Selection []string
}

// DB is the brute-force support table of a transaction database: the
// exact weighted support of every subset of the item universe.
type DB struct {
	items []string       // the universe in canonical order; bit i <-> items[i]
	index map[string]int // item name -> bit index
	supp  []tract.Supp   // supp[mask]: support of the set with bit mask mask
	total tract.Supp     // total weight = support of the empty set
}

// New computes the support table of txs. The universe consists of all
// distinct item names that occur in at least one transaction (after the
// optional selection); it must not have more than MaxItems items.
// Weights must be non-negative and their sum must fit in tract.Supp.
func New(txs []Transaction, opts Options) (*DB, error) {
	var selected map[string]bool
	if opts.Selection != nil {
		selected = make(map[string]bool, len(opts.Selection))
		for _, item := range opts.Selection {
			selected[item] = true
		}
	}
	isUsed := func(item string) bool { return selected == nil || selected[item] }

	nameSet := map[string]bool{}
	var total tract.Supp
	for i, tx := range txs {
		if tx.Weight < 0 {
			return nil, fmt.Errorf("transaction %d: weight %d: %w", i, tx.Weight, ErrNegativeWeight)
		}
		if tx.Weight > math.MaxInt64-total {
			return nil, fmt.Errorf("transaction %d: %w", i, ErrWeightOverflow)
		}
		total += tx.Weight
		for _, item := range tx.Items {
			if isUsed(item) {
				nameSet[item] = true
			}
		}
	}
	if len(nameSet) > MaxItems {
		return nil, fmt.Errorf("%d items, limit %d: %w", len(nameSet), MaxItems, ErrTooManyItems)
	}

	db := &DB{
		items: make([]string, 0, len(nameSet)),
		index: make(map[string]int, len(nameSet)),
		supp:  make([]tract.Supp, 1<<len(nameSet)),
		total: total,
	}
	for item := range nameSet {
		db.items = append(db.items, item)
	}
	slices.Sort(db.items)
	for i, item := range db.items {
		db.index[item] = i
	}

	// Weight of every distinct transaction (as a mask), then the
	// superset-sum transform: supp[m] = sum of w[t] over all t ⊇ m.
	for _, tx := range txs {
		var m uint64
		for _, item := range tx.Items {
			if i, ok := db.index[item]; ok {
				m |= 1 << i
			}
		}
		db.supp[m] += tx.Weight
	}
	for i := range db.items {
		bit := uint64(1) << i
		for m := range uint64(len(db.supp)) {
			if m&bit == 0 {
				db.supp[m] += db.supp[m|bit]
			}
		}
	}
	return db, nil
}

// Items returns the item universe in canonical (byte) order.
func (db *DB) Items() []string {
	return slices.Clone(db.items)
}

// Total returns the total transaction weight, i.e. the support of the
// empty set (upstream: ib_getwgt / tbg_wgt).
func (db *DB) Total() tract.Supp {
	return db.total
}

// Support returns the weighted support of s: the sum of the weights of all
// transactions containing every item of s. It is the total weight for the
// empty set and 0 if s contains an item outside the universe.
func (db *DB) Support(s ItemSet) tract.Supp {
	m, ok := db.mask(s)
	if !ok {
		return 0
	}
	return db.supp[m]
}

// mask returns the bit mask of s; ok is false if s contains an item
// outside the universe.
func (db *DB) mask(s ItemSet) (m uint64, ok bool) {
	for _, item := range s {
		i, found := db.index[item]
		if !found {
			return 0, false
		}
		m |= 1 << i
	}
	return m, true
}

// set returns the canonical item set of mask m.
func (db *DB) set(m uint64) ItemSet {
	s := make(ItemSet, 0, bits.OnesCount64(m))
	for ; m != 0; m &= m - 1 {
		s = append(s, db.items[bits.TrailingZeros64(m)])
	}
	return s
}

// full returns the mask of the whole universe.
func (db *DB) full() uint64 {
	return uint64(len(db.supp) - 1)
}
