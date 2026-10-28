package tract

import (
	"errors"
	"fmt"
	"math"
)

// Errors returned by the item base and the transaction bag. They are
// wrapped with the 1-based number of the offending record where one
// applies; test for them with errors.Is.
var (
	// ErrNegativeWeight reports a record with a negative weight.
	ErrNegativeWeight = errors.New("tract: negative transaction weight")
	// ErrWeightOverflow reports a total transaction weight beyond int64.
	ErrWeightOverflow = errors.New("tract: total transaction weight overflows int64")
	// ErrDuplicateItem reports an item listed twice in an appearance list
	// (upstream E_DUPITEM).
	ErrDuplicateItem = errors.New("tract: duplicate item")
	// ErrNotReplayable reports a source whose second pass differs from
	// the first one.
	ErrNotReplayable = errors.New("tract: source is not replayable (passes differ)")
	// ErrTooLarge reports a database that exceeds an implementation
	// limit (2^31-1 items or prefix tree nodes, math.MaxInt records).
	ErrTooLarge = errors.New("tract: database too large")
)

// maxItems is the largest number of items: codes must fit into Item.
const maxItems = math.MaxInt32

// itemData is the per-item record of the item base (upstream ITEMDATA).
type itemData[T comparable] struct {
	id   T          // identifier (upstream: the item name)
	frq  Supp       // sum of the weights of the records containing it
	xfq  Supp       // sum of size·weight of those records (saturating)
	last int        // number of the last record containing it (dedup)
	app  Appearance // appearance indicator
}

// ItemBase manages the items of a transaction database: identifiers in
// first-seen order, read-time frequencies, appearance indicators and the
// total transaction weight. Port of upstream's ITEMBASE (tract.c,
// compiled with TA_READ, created by ib_create(0, 0)).
//
// Use it in this order: optionally Retain, Select or
// SetDefaultAppearance and AddAppearance (from an -R file), then Count
// or CountBytes (pass 1), Recode, and BuildBag (pass 2). Calling a
// method out of order panics. T must be
// strictly comparable: an interface type holding an incomparable
// dynamic value panics when used as a map key.
type ItemBase[T comparable] struct {
	codes     map[T]Item    // identifier -> item; after Recode -1 = removed
	items     []itemData[T] // indexed by item identifier, after Recode by code
	defApp    Appearance    // appearance of new items (upstream base->app)
	wgt       Supp          // total weight of all records
	max       Supp          // running maximum of the item frequencies
	records   int           // number of records counted
	isRecoded bool          // Recode has been called
	buf       []Item        // scratch: items of the current record
	check     func(T) error // validation of unknown identifiers (SetCheck)
	abort     func() error  // cancellation check of BuildBag (SetAbort)
	ret       retained      // the counted records, if retained (Retain)
}

// NewItemBase returns an empty item base in which every new item may
// appear anywhere (upstream: ib_create(0, 0), default appearance
// APP_BOTH).
func NewItemBase[T comparable]() *ItemBase[T] {
	return &ItemBase[T]{
		codes:  map[T]Item{},
		items:  []itemData[T]{},
		defApp: AppearBoth,
	}
}

// Select restricts the item base to the given items. Port of ib_readsel
// for an already parsed item list: items not yet known are registered in
// the given order with appearance AppearBoth and zero counters, known
// ones are skipped, and the default appearance becomes AppearNone, so
// Count ignores every other item. Calling Select with no items selects
// nothing (an empty selection file).
func (b *ItemBase[T]) Select(ids ...T) error {
	b.mustNotBeRecoded("Select")
	b.defApp = AppearNone
	for _, id := range ids {
		if _, ok := b.codes[id]; ok {
			continue
		}
		if _, err := b.add(id, AppearBoth); err != nil {
			return err
		}
	}
	return nil
}

// SetDefaultAppearance sets the appearance of items that are not
// registered yet (the first record of an appearance file, ib_readapp).
// With AppearNone, Count ignores such items like unselected ones.
func (b *ItemBase[T]) SetDefaultAppearance(app Appearance) {
	b.mustNotBeRecoded("SetDefaultAppearance")
	mustBeValid(app)
	b.defApp = app
}

// AddAppearance registers a new item with the given appearance (one
// item/indicator record of an appearance file, ib_readapp). Items with
// AppearNone are still counted (and contribute to record sizes) but are
// removed by Recode. It returns ErrDuplicateItem if the item is known.
func (b *ItemBase[T]) AddAppearance(id T, app Appearance) error {
	b.mustNotBeRecoded("AddAppearance")
	mustBeValid(app)
	if _, ok := b.codes[id]; ok {
		return fmt.Errorf("item %v: %w", id, ErrDuplicateItem)
	}
	_, err := b.add(id, app)
	return err
}

// SetCheck sets a function that validates item identifiers read by Count
// and BuildBag that are not registered in the item base: it is called
// for every occurrence of an unknown identifier before it is registered
// (Count) or reported as not replayable (BuildBag), also when it is then
// ignored because the default appearance is AppearNone. A registered
// identifier is not checked again, so the cost is one call per distinct
// item plus one per occurrence of an ignored item. A non-nil result stops
// the pass with that error, wrapped with the record number. Identifiers
// registered by Select and AddAppearance are not checked; the caller
// validates them. nil (the default) disables the check.
//
// The check cannot catch interface values whose dynamic type is not
// comparable: the map lookup that precedes it panics.
func (b *ItemBase[T]) SetCheck(fn func(T) error) { b.check = fn }

// SetAbort sets a cancellation check for the work of BuildBag after its
// scan: completing the bag visits every node of its prefix tree (tens of
// millions for large databases) and calls fn every few tens of thousands
// of nodes. A non-nil result stops BuildBag, which returns it unchanged.
// The scan itself is not checked; the caller's scan function does that.
// nil (the default) disables the check.
func (b *ItemBase[T]) SetAbort(fn func() error) { b.abort = fn }

// Retain makes Count keep the records it counts in memory, up to limit
// bytes (4 bytes per distinct item of a record and per record, 8 more
// per record once a weight other than 1 occurs), so that the next BuildBag
// builds the bag from them instead of calling its scan function: the
// database is read once, like upstream's tbg_read. If the records
// exceed the budget, Count releases them and BuildBag scans as usual.
// The first BuildBag releases the retained records; a later one scans.
//
// With retained records BuildBag does not read the database again, so it
// cannot detect a source whose second pass would differ
// (ErrNotReplayable); the bag is that of the counted records. Retain must
// be called before the first Count; limit <= 0 disables retention.
func (b *ItemBase[T]) Retain(limit int) {
	b.mustNotBeRecoded("Retain")
	if b.records > 0 {
		panic("tract: ItemBase.Retain called after Count")
	}
	b.ret = retained{on: limit > 0, limit: limit}
}

// add appends a new item with zero counters.
func (b *ItemBase[T]) add(id T, app Appearance) (Item, error) {
	if len(b.items) >= maxItems {
		return 0, fmt.Errorf("more than %d items: %w", maxItems, ErrTooLarge)
	}
	i := Item(len(b.items))
	b.codes[id] = i
	b.items = append(b.items, itemData[T]{id: id, app: app})
	return i, nil
}

// Count reads one pass of a transaction database and accumulates the
// item counters (pass 1; port of the counting part of ib_read as called
// by tbg_read). For every record: items that are not registered get the
// next identifier, unless the default appearance is AppearNone, in which
// case they are skipped; repeated items count once (first occurrence);
// the total weight grows by the record weight; and each item's frequency
// grows by the weight and its size-weighted frequency by size·weight,
// where size is the number of distinct, not skipped items of the record.
// Every record counts, also empty ones and those whose items were all
// skipped.
//
// Count may be called more than once; the passes accumulate like repeated
// tbg_read calls. After an error the item base must not be used.
func (b *ItemBase[T]) Count(scan ScanFunc[T]) error {
	b.mustNotBeRecoded("Count")
	var err error
	scanErr := scan(func(ids []T, w Supp) bool {
		err = b.countRecord(ids, w)
		return err == nil
	})
	if err != nil {
		return err
	}
	return scanErr
}

// CountBytes is Count over a scan of byte-string identifiers for an item
// base of string identifiers: the identifier of an item is string(id).
// It looks every identifier up once, without converting it to a string,
// and allocates a string only for a new item; Count over a scan that
// converts its identifiers would look them up twice (see
// tabread.ScanBytes). The result is that of Count over the same records
// with string identifiers.
func CountBytes(b *ItemBase[string], scan ByteScanFunc) error {
	b.mustNotBeRecoded("Count")
	var err error
	scanErr := scan(func(ids [][]byte, w Supp) bool {
		err = countBytesRecord(b, ids, w)
		return err == nil
	})
	if err != nil {
		return err
	}
	return scanErr
}

// countRecord adds one record to the counters.
func (b *ItemBase[T]) countRecord(ids []T, w Supp) error {
	rec, total, err := b.beginRecord(w)
	if err != nil {
		return err
	}
	buf := b.buf[:0]
	for _, id := range ids {
		i, known := b.codes[id]
		if !known {
			if i, err = b.register(id, rec); err != nil {
				return err
			}
			if i < 0 {
				continue
			}
		}
		buf = b.keep(buf, i, rec)
	}
	b.endRecord(buf, rec, total, w)
	return nil
}

// countBytesRecord is countRecord for byte-string identifiers.
func countBytesRecord(b *ItemBase[string], ids [][]byte, w Supp) error {
	rec, total, err := b.beginRecord(w)
	if err != nil {
		return err
	}
	buf := b.buf[:0]
	for _, id := range ids {
		i, known := b.codes[string(id)] // no allocation
		if !known {
			if i, err = b.register(string(id), rec); err != nil {
				return err
			}
			if i < 0 {
				continue
			}
		}
		buf = b.keep(buf, i, rec)
	}
	b.endRecord(buf, rec, total, w)
	return nil
}

// beginRecord checks the next record of weight w and returns its number
// (also the duplicate marker) and the new total weight.
func (b *ItemBase[T]) beginRecord(w Supp) (rec int, total Supp, err error) {
	if b.records >= math.MaxInt {
		return 0, 0, fmt.Errorf("more than %d records: %w", math.MaxInt, ErrTooLarge)
	}
	rec = b.records + 1
	// Deviation from upstream: negative weights are rejected. Upstream
	// accepts them and then miscounts (fim16 double counting, perfect
	// extensions above the true support) or corrupts memory
	// (docs/upstream/tract.md T1, T7).
	if w < 0 {
		return 0, 0, fmt.Errorf("record %d: weight %d: %w", rec, w, ErrNegativeWeight)
	}
	// Deviation from upstream: an overflowing total weight is an error.
	// Upstream sums in int (UB, T5). Every frequency and merged weight is
	// bounded by the total, so they cannot overflow afterwards.
	total, ok := addChecked(b.wgt, w)
	if !ok {
		return 0, 0, fmt.Errorf("record %d: %w", rec, ErrWeightOverflow)
	}
	return rec, total, nil
}

// register handles the unknown identifier id of record rec: it returns
// the new item, or -1 if new items are ignored (default appearance
// AppearNone).
func (b *ItemBase[T]) register(id T, rec int) (Item, error) {
	if b.check != nil {
		if err := b.check(id); err != nil {
			return 0, fmt.Errorf("record %d: %w", rec, err)
		}
	}
	if b.defApp == AppearNone { // ignore new items
		return -1, nil
	}
	i, err := b.add(id, b.defApp)
	if err != nil {
		return 0, fmt.Errorf("record %d: %w", rec, err)
	}
	return i, nil
}

// keep appends item i to the items buf of record rec unless the record
// already contains it (duplicates count once, the first one is kept).
func (b *ItemBase[T]) keep(buf []Item, i Item, rec int) []Item {
	d := &b.items[i]
	if d.last == rec {
		return buf
	}
	d.last = rec
	return append(buf, i)
}

// endRecord adds the record rec of weight w with the distinct items buf
// to the counters; total is the new total weight.
func (b *ItemBase[T]) endRecord(buf []Item, rec int, total, w Supp) {
	b.buf = buf
	b.records = rec
	b.wgt = total
	if b.ret.on {
		b.ret.add(buf, w)
	}
	// Deviation from upstream: size·weight and its sum saturate at
	// math.MaxInt64 instead of overflowing int (UB upstream).
	xw := mulSaturated(Supp(len(buf)), w)
	for _, i := range buf {
		d := &b.items[i]
		d.xfq = addSaturated(d.xfq, xw)
		d.frq += w
		if d.frq > b.max {
			b.max = d.frq
		}
	}
}

// Recode removes the items with frequency below smin and those with
// appearance AppearNone, sorts the remaining ones and assigns them the
// codes 0..n-1 in sorted order, and returns n. Port of ib_recode as
// called by fpg_data: tbg_recode(bag, smin, -1, -1, -order).
//
// order is the -q value and selects upstream's comparator; items to be
// removed always go last, and ties keep the order upstream's unstable
// quicksort (ptr_qsort over all items in identifier order) gives them:
//
//	order   comparator   item code 0 is the item with
//	> 1     descmpx      largest size-weighted frequency
//	1       descmp       largest frequency
//	0       nocmp        smallest identifier (first seen)
//	-1      asccmp       smallest frequency
//	< -1    asccmpx      smallest size-weighted frequency
//
// The caller applies fpg_data's FPG_REORDER adjustment (order is reduced
// to its sign when the FP-tree will be reordered) before calling Recode.
//
// After Recode, items are addressed by code: ID, Frequency, ExtFrequency
// and Appearance report the read-time values of the kept items, which are
// never updated again; TotalWeight, MaxFrequency and Records are
// unchanged.
func (b *ItemBase[T]) Recode(smin Supp, order int) int {
	b.mustNotBeRecoded("Recode")
	// Deviation from upstream: the minimum support is at least 1. With 0,
	// upstream keeps items of frequency 0, on which the miners crash
	// (docs/upstream/tract.md T2).
	smin = max(smin, 1)
	n := len(b.items)
	keys := make([]itemKey, n)
	ids := make([]int32, n)
	for i := range b.items {
		d := &b.items[i]
		if d.frq < smin { // (the maximum support is unlimited)
			d.app = AppearNone
		}
		keys[i] = itemKey{frq: d.frq, xfq: d.xfq, isNone: d.app == AppearNone}
	}
	k := sortItems(keys, ids, order)
	items := make([]itemData[T], k)
	if b.ret.on {
		b.ret.recode = make([]Item, n)
	}
	for c, i := range ids {
		d := &b.items[i]
		code := Item(-1) // removed (upstream: map[k] = -1)
		if c < k {
			items[c] = *d
			code = Item(c)
		}
		b.codes[d.id] = code
		if b.ret.on {
			b.ret.recode[i] = code
		}
	}
	b.items = items
	b.isRecoded = true
	return k
}

// sortItems sorts the item identifiers 0..len(keys)-1 into ids (which
// must have the length of keys) with the comparator of order (idm_sort)
// and returns the number of items to keep, which come first.
func sortItems(keys []itemKey, ids []int32, order int) int {
	for i := range ids {
		ids[i] = int32(i)
	}
	ptrQsort(ids, itemComparator(keys, order)) // idm_sort
	k := len(ids)                              // find the items to keep
	for k > 0 && keys[ids[k-1]].isNone {
		k--
	}
	return k
}

// Len returns the number of items: all registered items before Recode,
// the kept ones after it (ib_cnt).
func (b *ItemBase[T]) Len() int { return len(b.items) }

// ID returns the identifier of item i (ib_name).
func (b *ItemBase[T]) ID(i Item) T { return b.items[i].id }

// Code returns the item (identifier before Recode, code after it) of
// identifier id. ok is false for unknown and removed items.
func (b *ItemBase[T]) Code(id T) (i Item, ok bool) {
	i, ok = b.codes[id]
	if !ok || i < 0 {
		return -1, false
	}
	return i, true
}

// Frequency returns the read-time frequency of item i: the sum of the
// weights of the records containing it (ib_getfrq).
func (b *ItemBase[T]) Frequency(i Item) Supp { return b.items[i].frq }

// ExtFrequency returns the read-time size-weighted frequency of item i:
// the sum of size·weight over the records containing it, saturated at
// math.MaxInt64 (upstream xfq, the key of -q ±2).
func (b *ItemBase[T]) ExtFrequency(i Item) Supp { return b.items[i].xfq }

// Appearance returns the appearance indicator of item i (ib_getapp).
func (b *ItemBase[T]) Appearance(i Item) Appearance { return b.items[i].app }

// DefaultAppearance returns the appearance given to new items
// (ib_getapp with a negative item).
func (b *ItemBase[T]) DefaultAppearance() Appearance { return b.defApp }

// TotalWeight returns the summed weight of all records counted
// (ib_getwgt, equal to tbg_wgt): including empty records, records whose
// items were all skipped and records of zero weight.
func (b *ItemBase[T]) TotalWeight() Supp { return b.wgt }

// MaxFrequency returns the largest item frequency seen while counting,
// including items later removed by Recode (ib_maxfrq).
func (b *ItemBase[T]) MaxFrequency() Supp { return b.max }

// Records returns the number of records counted (tbg_cnt after
// tbg_read).
func (b *ItemBase[T]) Records() int { return b.records }

// mustNotBeRecoded panics if Recode has been called.
func (b *ItemBase[T]) mustNotBeRecoded(method string) {
	if b.isRecoded {
		panic("tract: ItemBase." + method + " called after Recode")
	}
}

// mustBeValid panics if app is not an appearance code.
func mustBeValid(app Appearance) {
	if !app.isValid() {
		panic(fmt.Sprintf("tract: invalid appearance %d", app))
	}
}

// addChecked returns a+b for non-negative a and b and whether the sum
// fits into Supp.
func addChecked(a, b Supp) (Supp, bool) {
	if a > math.MaxInt64-b {
		return 0, false
	}
	return a + b, true
}

// addSaturated returns a+b for non-negative a and b, saturated at
// math.MaxInt64.
func addSaturated(a, b Supp) Supp {
	if s, ok := addChecked(a, b); ok {
		return s
	}
	return math.MaxInt64
}

// mulSaturated returns a·b for non-negative a and b, saturated at
// math.MaxInt64.
func mulSaturated(a, b Supp) Supp {
	if a == 0 || b == 0 {
		return 0
	}
	if a > math.MaxInt64/b {
		return math.MaxInt64
	}
	return a * b
}
