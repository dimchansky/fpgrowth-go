package tract

import (
	"fmt"
	"runtime"
)

// retainBlock is the number of entries per block of retained records
// (128 KiB). Blocks are never reallocated, so retaining the records costs
// no copying and leaves no garbage behind.
const retainBlock = 1 << 15

// retainCheckEvery is the number of retained records between two calls
// of the abort function in buildRetained (the CLIs' scans check every
// 1024 records).
const retainCheckEvery = 1024

// retained holds the records counted by Count while they fit into the
// budget of Retain, so that BuildBag can build the bag without reading
// the database again (upstream keeps every record in its TABAG).
type retained struct {
	on     bool     // Retain was called, and the records fit so far
	limit  int      // budget in bytes
	used   int      // bytes allocated so far
	blocks [][]Item // per record its number of identifiers, then the identifiers; no record spans two blocks
	n      int      // number of records
	wgts   []Supp   // per record: the weight; nil while every weight is 1
	recode []Item   // after Recode: identifier -> code, -1 if removed
}

// add appends a record with the distinct, not skipped identifiers ids
// and the weight w, or releases all records if they exceed the budget.
func (r *retained) add(ids []Item, w Supp) {
	if r.wgts == nil && w != 1 { // the first weight other than 1
		r.wgts = make([]Supp, r.n, r.n+r.n/4+1)
		for i := range r.wgts {
			r.wgts[i] = 1
		}
		r.used += 8 * cap(r.wgts)
	}
	last := len(r.blocks) - 1
	if last < 0 || len(r.blocks[last])+1+len(ids) > cap(r.blocks[last]) {
		size := max(retainBlock, 1+len(ids))
		if r.used += 4 * size; r.used > r.limit {
			r.drop() // over budget: BuildBag scans
			return
		}
		r.blocks = append(r.blocks, make([]Item, 0, size))
		last++
	}
	r.blocks[last] = append(r.blocks[last], Item(len(ids))) // len(ids) <= maxItems
	r.blocks[last] = append(r.blocks[last], ids...)
	r.n++
	if r.wgts != nil {
		// Growing copies the weights; the old array is garbage until
		// the next collection, so every allocation counts, not only the
		// difference.
		c := cap(r.wgts)
		r.wgts = append(r.wgts, w)
		if cap(r.wgts) != c {
			r.used += 8 * cap(r.wgts)
		}
	}
	if r.used > r.limit {
		r.drop()
	}
}

// drop releases the records and collects them at once. The heap goal was
// set while they were live, so whatever is allocated next (the second
// pass, the bag, the miner's structures) would otherwise take fresh pages,
// and the peak resident memory would grow by up to the whole budget
// (fpgpsp -ge on B×90: 135 MB without the collection, 92 MB with it, as
// without retention).
func (r *retained) drop() {
	*r = retained{}
	runtime.GC()
}

// buildRetained is BuildBag over the records retained by Count: the same
// recoding, sorting and zmin filter as the scan, without reading the
// database again. The retained identifiers of a record are distinct and
// registered, so none is unknown and no code repeats within a record.
func (b *ItemBase[T]) buildRetained(r *retained, zmin int) (*Bag, error) {
	bld := newBagBuilder(len(b.items))
	var codes []Item
	blk, off := 0, 0
	for k := range r.n {
		if b.abort != nil && k%retainCheckEvery == retainCheckEvery-1 {
			if err := b.abort(); err != nil {
				return nil, err
			}
		}
		if off == len(r.blocks[blk]) { // the record starts a block
			blk, off = blk+1, 0
		}
		n := int(r.blocks[blk][off])
		off++
		codes = codes[:0]
		for _, i := range r.blocks[blk][off : off+n] { // recode
			if c := r.recode[i]; c >= 0 {
				codes = append(codes, c)
			}
		}
		off += n
		SortItems(codes)                   // tbg_itsort
		if zmin > 1 && len(codes) < zmin { // tbg_filter
			codes = codes[:0]
		}
		w := Supp(1)
		if r.wgts != nil {
			w = r.wgts[k]
		}
		if err := bld.add(codes, w); err != nil {
			return nil, fmt.Errorf("record %d: %w", k+1, err)
		}
	}
	// Release the records before finish allocates the bag's final arrays.
	// The compiler already treats r as dead here (live heap measured in
	// finish is the same with and without retention); this does not
	// depend on that.
	r.drop()
	return bld.finish(b.records, b.wgt, b.abort)
}
