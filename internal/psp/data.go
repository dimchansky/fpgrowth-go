package psp

import (
	"errors"
	"fmt"
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Data is the transaction array from which surrogates are generated or a
// pattern spectrum is estimated: upstream's TABAG after fpg_genpsp's (or
// fpg_estpsp's) fpg_data call, with oracle patch P1 (no reduction), as a
// list of transactions in upstream's slot order. Every transaction has
// weight 1; empty transactions (also those emptied by the -m filter) are
// kept, they take part in the surrogate generation (D7 in
// docs/upstream/fpgpsp.md).
type Data struct {
	// Tracts holds the item codes of every transaction.
	Tracts [][]tract.Item
	// NItems is the number of items of the item base (ib_cnt): item
	// codes are below it.
	NItems int
}

// Len returns the number of transactions (tbg_cnt).
func (d *Data) Len() int { return len(d.Tracts) }

// Extent returns the number of item occurrences (tbg_extent).
func (d *Data) Extent() int64 {
	var n int64
	for _, t := range d.Tracts {
		n += int64(len(t))
	}
	return n
}

// Frequencies returns the number of transactions that contain each item
// (tbg_ifrqs of the array, all weights 1).
func (d *Data) Frequencies() []int64 {
	f := make([]int64, d.NItems)
	for _, t := range d.Tracts {
		for _, i := range t {
			f[i]++
		}
	}
	return f
}

// clone returns a deep copy of the transactions (tbg_clone).
func (d *Data) clone() [][]tract.Item {
	n := d.Extent()
	buf := make([]tract.Item, 0, n)
	ts := make([][]tract.Item, len(d.Tracts))
	for k, t := range d.Tracts {
		start := len(buf)
		buf = append(buf, t...)
		ts[k] = buf[start:len(buf):len(buf)]
	}
	return ts
}

// ErrTooLarge reports a database whose array of transactions would not fit
// into memory addressable by an int.
var ErrTooLarge = errors.New("psp: database too large")

// PrepConfig returns the miner configuration of fpg_genpsp's and
// fpg_estpsp's data preparation for the mining configuration cfg: the same
// thresholds, sizes and item order (which depends on the variant, -i and
// the target through FPG_REORDER, D3), no packing (FPG_NOPACK), and the
// transaction filter of fpg_data, which empties transactions with fewer
// than -m items (part of the null model, D7).
//
// Deviation from upstream (U25, oracle patch P11): for generators the
// filter is not applied, as internal/fpg does for fpgrowth (M3): emptying
// short transactions deflates the supports of the subsets that a
// generator is compared with, so upstream misses generators with -m >= 2
// (fpgpsp -gi -tg -m3 found none where fpgrowth finds two). fpg.Miner.Data
// skips the filter for the generator target.
func PrepConfig(cfg fpg.Config) fpg.Config {
	c := cfg
	c.Pack = 0
	return c
}

// Prepare is the data preparation of fpg_genpsp (all methods except
// Shuffle) and fpg_estpsp: ib, configured (selection) and counted with
// scan but not recoded, is recoded with the minimum support and item
// order of cfg (see PrepConfig), the transactions with fewer than cfg.ZMin
// items are emptied, the items of every transaction and the transactions
// are sorted, and equal transactions stay separate (patch P1). It returns
// the miner of the preparation (for its thresholds) and the array; the
// error is fpg.ErrNoItems if no item is frequent.
func Prepare[T comparable](ib *tract.ItemBase[T], scan tract.ScanFunc[T], cfg fpg.Config) (*fpg.Miner[T], *Data, error) {
	m, err := fpg.New[T](PrepConfig(cfg))
	if err != nil {
		return nil, nil, err
	}
	if err := m.Data(ib, scan); err != nil {
		return nil, nil, err
	}
	d, err := FromBag(m.Bag(), ib.Len())
	if err != nil {
		return nil, nil, err
	}
	return m, d, nil
}

// FromBag returns the array of a transaction bag: its distinct
// transactions in bag order (upstream's sort order), each repeated as
// many times as its weight. Bags of fpgpsp's input have record weights 1.
func FromBag(bag *tract.Bag, nitems int) (*Data, error) {
	var n, ext int64
	for t, w := range bag.All() {
		n += int64(w)
		ext += int64(w) * int64(len(t))
		if n > math.MaxInt32 || ext > math.MaxInt-1 {
			return nil, fmt.Errorf("%d transactions: %w", n, ErrTooLarge)
		}
	}
	buf := make([]tract.Item, 0, ext)
	ts := make([][]tract.Item, 0, n)
	for t, w := range bag.All() {
		for range w {
			start := len(buf)
			buf = append(buf, t...)
			ts = append(ts, buf[start:len(buf):len(buf)])
		}
	}
	return &Data{Tracts: ts, NItems: nitems}, nil
}

// Records returns the records of scan as item codes of ib (counted, not
// recoded): in file order, with the items in the order of their first
// occurrence in the record, without repeated and unknown (unselected)
// items. This is the transaction array of upstream's tbg_read before
// fpg_data, which fpg_genpsp keeps for Shuffle (FPG_NORECODE|FPG_NOSORT).
func Records[T comparable](ib *tract.ItemBase[T], scan tract.ScanFunc[T]) ([][]tract.Item, error) {
	var ts [][]tract.Item
	var buf []tract.Item
	seen := make([]int, ib.Len()) // record number + 1 of the last occurrence
	err := scan(func(ids []T, _ tract.Supp) bool {
		rec := len(ts) + 1
		start := len(buf)
		for _, id := range ids {
			i, ok := ib.Code(id)
			if !ok || seen[i] == rec {
				continue
			}
			seen[i] = rec
			buf = append(buf, i)
		}
		ts = append(ts, buf[start:len(buf):len(buf)])
		return len(ts) < math.MaxInt32
	})
	if err != nil {
		return nil, err
	}
	if len(ts) >= math.MaxInt32 {
		return nil, ErrTooLarge
	}
	return ts, nil
}

// IsTable is a port of tbg_istab: whether the records are table-derived,
// i.e. all have the same size and every item always occurs at the same
// position. Upstream returns false for at most one record.
func IsTable(recs [][]tract.Item, nitems int) bool {
	if len(recs) <= 1 {
		return false
	}
	col := make([]int, nitems)
	for i := range col {
		col[i] = -1
	}
	z := len(recs[0])
	for k := len(recs) - 1; k >= 0; k-- {
		t := recs[k]
		if len(t) != z {
			return false
		}
		for i := len(t) - 1; i >= 0; i-- {
			switch c := col[t[i]]; {
			case c < 0:
				col[t[i]] = i
			case c != i:
				return false
			}
		}
	}
	return true
}

// TableData is fpg_genpsp's preparation for Shuffle: the records stay in
// file order and unrecoded, and records with fewer than zmin items are
// emptied (tbg_filter); for table-derived data either all or none.
func TableData(recs [][]tract.Item, nitems, zmin int) *Data {
	ts := make([][]tract.Item, len(recs))
	for k, t := range recs {
		if len(t) < zmin {
			t = t[:0:0]
		}
		ts[k] = t
	}
	return &Data{Tracts: ts, NItems: nitems}
}
