package tract

import (
	"fmt"
)

// retainBlock is the number of identifiers per block of retained records
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
	blocks [][]Item // identifiers of the records; no record spans two blocks
	lens   []int32  // per record: the number of its identifiers
	wgts   []Supp   // per record: the weight; nil while every weight is 1
	recode []Item   // after Recode: identifier -> code, -1 if removed
}

// add appends a record with the distinct, not skipped identifiers ids
// and the weight w, or releases all records if they exceed the budget.
func (r *retained) add(ids []Item, w Supp) {
	if r.wgts == nil && w != 1 { // the first weight other than 1
		r.wgts = make([]Supp, len(r.lens), cap(r.lens))
		for i := range r.wgts {
			r.wgts[i] = 1
		}
		r.used += 8 * cap(r.wgts)
	}
	last := len(r.blocks) - 1
	if last < 0 || len(r.blocks[last])+len(ids) > cap(r.blocks[last]) {
		size := max(retainBlock, len(ids))
		if r.used += 4 * size; r.used > r.limit {
			*r = retained{} // over budget: BuildBag scans
			return
		}
		r.blocks = append(r.blocks, make([]Item, 0, size))
		last++
	}
	r.blocks[last] = append(r.blocks[last], ids...)
	c := cap(r.lens)
	r.lens = append(r.lens, int32(len(ids))) // len(ids) <= maxItems
	r.used += 4 * (cap(r.lens) - c)
	if r.wgts != nil {
		c = cap(r.wgts)
		r.wgts = append(r.wgts, w)
		r.used += 8 * (cap(r.wgts) - c)
	}
	if r.used > r.limit {
		*r = retained{}
	}
}

// buildRetained is BuildBag over the records retained by Count: the same
// recoding, sorting and zmin filter as the scan, without reading the
// database again. The retained identifiers of a record are distinct and
// registered, so none is unknown and no code repeats within a record.
func (b *ItemBase[T]) buildRetained(r *retained, zmin int) (*Bag, error) {
	bld := newBagBuilder(len(b.items))
	var codes []Item
	blk, off := 0, 0
	for k, n := range r.lens {
		if b.abort != nil && k%retainCheckEvery == retainCheckEvery-1 {
			if err := b.abort(); err != nil {
				return nil, err
			}
		}
		if off+int(n) > len(r.blocks[blk]) { // the record starts a block
			blk, off = blk+1, 0
		}
		codes = codes[:0]
		for _, i := range r.blocks[blk][off : off+int(n)] { // recode
			if c := r.recode[i]; c >= 0 {
				codes = append(codes, c)
			}
		}
		off += int(n)
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
	return bld.finish(b.records, b.wgt, b.abort)
}
