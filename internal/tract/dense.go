package tract

import (
	"fmt"
	"math"
	"slices"
)

// DenseBase is an item base for a sequence of databases whose records are
// lists of identifiers 0..n-1 of a fixed universe, each with weight 1:
// the surrogate data sets of fpgpsp (package psp), which are mined one
// after the other. For every database it gives exactly what a new
// ItemBase[Item] would give after Count, Recode and BuildBag over the
// same records (the same identifiers in first-seen order, the same item
// codes and the same Bag), but without a map, and it reuses its arrays
// and the storage of the bag from one database to the next.
//
// Use it in this order for every database: Count, Recode, BuildBag. The
// Bag returned by BuildBag belongs to the DenseBase and is valid until
// the next Count. A DenseBase is not safe for concurrent use; its zero
// value is ready to use.
type DenseBase struct {
	recs    [][]Item  // the records of the current database (Count)
	last    []int     // per identifier: number of the last record + 1
	ident   []int32   // per identifier: first-seen rank, -1 if not seen
	order   []Item    // per rank: the identifier (first-seen order)
	keys    []itemKey // per rank: the counters (Recode's sort keys)
	ids     []int32   // ranks in code order (Recode)
	code    []Item    // per identifier: code after Recode, -1 if removed
	frq     []Supp    // per code: frequency
	codes   []Item    // scratch: the recoded record
	records int
	kept    int
	state   int // 0 new, 1 counted, 2 recoded
	bld     bagBuilder
}

// Count starts a new database with the records recs (pass 1, like
// ItemBase.Count with weight 1 for every record): identifiers are ranked
// in the order of their first occurrence, repeated identifiers of a
// record count once, and every item's frequency grows by 1 and its
// size-weighted frequency by the number of distinct identifiers of the
// record. Identifiers must be in [0, n). recs is kept until the next
// Count and must not be modified before BuildBag.
func (d *DenseBase) Count(recs [][]Item, n int) error {
	if len(recs) >= math.MaxInt {
		return fmt.Errorf("more than %d records: %w", math.MaxInt, ErrTooLarge)
	}
	d.recs, d.records, d.kept, d.state = recs, len(recs), 0, 1
	d.last = resize(d.last, n)
	d.ident = resize(d.ident, n)
	clear(d.last)
	for i := range d.ident {
		d.ident[i] = -1
	}
	d.order, d.keys = d.order[:0], d.keys[:0]
	for k, t := range recs {
		rec := k + 1
		size := 0
		for _, i := range t { // distinct identifiers, first-seen ranks
			if d.last[i] == rec {
				continue
			}
			d.last[i] = rec
			size++
			if d.ident[i] < 0 {
				d.ident[i] = int32(len(d.order))
				d.order = append(d.order, i)
				d.keys = append(d.keys, itemKey{})
			}
		}
		xw := Supp(size)
		for _, i := range t {
			if d.last[i] != rec {
				continue
			}
			d.last[i] = -rec // counted
			key := &d.keys[d.ident[i]]
			key.frq++
			key.xfq = addSaturated(key.xfq, xw)
		}
	}
	return nil
}

// Recode is ItemBase.Recode for the counted database: it removes the
// items with frequency below smin (at least 1), sorts the others with the
// comparator of order and returns their number.
func (d *DenseBase) Recode(smin Supp, order int) int {
	if d.state != 1 {
		panic("tract: DenseBase.Recode called before Count")
	}
	d.state = 2
	smin = max(smin, 1) // as ItemBase.Recode
	for r := range d.keys {
		d.keys[r].isNone = d.keys[r].frq < smin
	}
	d.ids = resize(d.ids, len(d.keys))
	d.kept = sortItems(d.keys, d.ids, order)
	d.code = resize(d.code, len(d.ident))
	for i := range d.code {
		d.code[i] = -1
	}
	d.frq = resize(d.frq, d.kept)
	for c, r := range d.ids[:d.kept] {
		d.code[d.order[r]] = Item(c)
		d.frq[c] = d.keys[r].frq
	}
	return d.kept
}

// Len returns the number of items: the distinct identifiers counted
// before Recode, the kept items after it.
func (d *DenseBase) Len() int {
	if d.state == 2 {
		return d.kept
	}
	return len(d.order)
}

// Frequency returns the frequency of the item with code i (after Recode).
func (d *DenseBase) Frequency(i Item) Supp { return d.frq[i] }

// Appearance returns the appearance of item i, always AppearBoth.
func (d *DenseBase) Appearance(Item) Appearance { return AppearBoth }

// TotalWeight returns the number of records (all weights are 1).
func (d *DenseBase) TotalWeight() Supp { return Supp(d.records) }

// Records returns the number of records counted.
func (d *DenseBase) Records() int { return d.records }

// BuildBag is ItemBase.BuildBag for the counted and recoded database
// (pass 2, with the zmin filter). The bag reuses the storage of the
// previous one, which becomes invalid.
func (d *DenseBase) BuildBag(zmin int) (*Bag, error) {
	if d.state != 2 {
		panic("tract: DenseBase.BuildBag called before Recode")
	}
	bld := &d.bld
	bld.reset(d.kept)
	codes := d.codes
	for k, t := range d.recs {
		codes = codes[:0]
		for _, i := range t {
			if c := d.code[i]; c >= 0 {
				codes = append(codes, c)
			}
		}
		SortItems(codes)
		codes = slices.Compact(codes)
		if zmin > 1 && len(codes) < zmin { // tbg_filter
			codes = codes[:0]
		}
		if err := bld.add(codes, 1); err != nil {
			d.codes = codes
			return nil, fmt.Errorf("record %d: %w", k+1, err)
		}
	}
	d.codes = codes
	return bld.finish(d.records, Supp(d.records), nil)
}

// resize returns s with length n, reusing its array if it is large
// enough. The contents are unspecified.
func resize[E any](s []E, n int) []E {
	if cap(s) < n {
		return make([]E, n)
	}
	return s[:n]
}

// SortItems sorts the items of a transaction ascending: by insertion for
// short transactions (the common case, and fast for the nearly sorted
// transactions of a pair swap), else with slices.Sort.
func SortItems(t []Item) {
	if len(t) > 16 {
		slices.Sort(t)
		return
	}
	for i := 1; i < len(t); i++ {
		x := t[i]
		j := i
		for ; j > 0 && t[j-1] > x; j-- {
			t[j] = t[j-1]
		}
		t[j] = x
	}
}
