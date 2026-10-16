package tract

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// weighted is a transaction as yielded by Bag.All, copied.
type weighted struct {
	items []Item
	w     Supp
}

// collect copies the transactions of an iterator.
func collect(seq iter.Seq2[[]Item, Supp]) []weighted {
	out := []weighted{}
	for t, w := range seq {
		out = append(out, weighted{items: slices.Clone(t), w: w})
	}
	return out
}

// intRecord is a test record over int identifiers.
type intRecord struct {
	items []int
	w     Supp
}

// intScan returns a ScanFunc over recs.
func intScan(recs []intRecord) ScanFunc[int] {
	return func(yield func([]int, Supp) bool) error {
		for _, r := range recs {
			if !yield(r.items, r.w) {
				return nil
			}
		}
		return nil
	}
}

// buildIntBag counts, recodes with -q0 (codes = first-seen order) and
// builds the bag of recs.
func buildIntBag(t *testing.T, recs []intRecord, smin Supp, zmin int) (*ItemBase[int], *Bag) {
	t.Helper()
	b := NewItemBase[int]()
	if err := b.Count(intScan(recs)); err != nil {
		t.Fatalf("Count: %v", err)
	}
	b.Recode(smin, 0)
	bag, err := b.BuildBag(intScan(recs), zmin)
	if err != nil {
		t.Fatalf("BuildBag: %v", err)
	}
	return b, bag
}

// rec is shorthand for an intRecord.
func rec(w Supp, items ...int) intRecord { return intRecord{items: items, w: w} }

// tx is shorthand for an expected transaction.
func tx(w Supp, items ...Item) weighted {
	if items == nil {
		items = []Item{}
	}
	return weighted{items: items, w: w}
}

func equalWeighted(a, b []weighted) bool {
	return slices.EqualFunc(a, b, func(x, y weighted) bool {
		return x.w == y.w && slices.Equal(x.items, y.items)
	})
}

func TestItem_Packed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		item     Item
		isPacked bool
		bits     uint32
	}{
		{name: "item zero", item: 0, isPacked: false, bits: 0},
		{name: "item", item: 17, isPacked: false, bits: 17},
		{name: "end", item: End, isPacked: false, bits: 0},
		{name: "packed 0 and 2", item: End | 5, isPacked: true, bits: 5},
		{name: "packed 30", item: End | 1<<30, isPacked: true, bits: 1 << 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.item.IsPacked(); got != tt.isPacked {
				t.Errorf("IsPacked(%d) = %v, want %v", tt.item, got, tt.isPacked)
			}
			if got := tt.item.PackedBits(); got != tt.bits {
				t.Errorf("PackedBits(%d) = %#x, want %#x", tt.item, got, tt.bits)
			}
		})
	}
}

func TestItemBase_BuildBag_order(t *testing.T) {
	t.Parallel()
	// Items are first seen in the order 0, 1, 2, 3, so with -q0 the codes
	// equal the identifiers.
	recs := []intRecord{
		rec(1, 0, 1, 2, 3),
		rec(1, 2, 0),
		rec(1),
		rec(2, 0),
		rec(1, 1, 3),
		rec(1, 0, 2, 0), // duplicate 0
		rec(1, 3),
		rec(1, 0, 1),
		rec(1, 1),
		rec(1, 0, 1, 2),
		rec(3),
	}
	_, bag := buildIntBag(t, recs, 1, 0)
	want := []weighted{
		tx(4), // empty transactions first, merged
		tx(2, 0),
		tx(1, 0, 1), // a prefix precedes its extensions
		tx(1, 0, 1, 2),
		tx(1, 0, 1, 2, 3),
		tx(2, 0, 2),
		tx(1, 1),
		tx(1, 1, 3),
		tx(1, 3),
	}
	if got := collect(bag.All()); !equalWeighted(got, want) {
		t.Errorf("All() = %v, want %v", got, want)
	}
	slices.Reverse(want)
	if got := collect(bag.Backward()); !equalWeighted(got, want) {
		t.Errorf("Backward() = %v, want %v", got, want)
	}
	if bag.Len() != 9 || bag.Records() != 11 || bag.TotalWeight() != 14 {
		t.Errorf("Len, Records, TotalWeight = %d, %d, %d, want 9, 11, 14",
			bag.Len(), bag.Records(), bag.TotalWeight())
	}
	if bag.MaxSize() != 4 || bag.Extent() != 16 || bag.NumItems() != 4 {
		t.Errorf("MaxSize, Extent, NumItems = %d, %d, %d, want 4, 16, 4",
			bag.MaxSize(), bag.Extent(), bag.NumItems())
	}
	if got, want := bag.Frequencies(), []Supp{7, 5, 4, 3}; !slices.Equal(got, want) {
		t.Errorf("Frequencies() = %v, want %v", got, want)
	}
}

func TestItemBase_BuildBag_zmin(t *testing.T) {
	t.Parallel()
	recs := []intRecord{
		rec(1, 0, 1, 2),
		rec(2, 0, 1),
		rec(3, 0),
		rec(4, 2),
		rec(5),
	}
	tests := []struct {
		name    string
		zmin    int
		want    []weighted
		maxSize int
		frq     []Supp
	}{
		{
			name:    "no filter",
			zmin:    1,
			want:    []weighted{tx(5), tx(3, 0), tx(2, 0, 1), tx(1, 0, 1, 2), tx(4, 2)},
			maxSize: 3,
			frq:     []Supp{6, 3, 5},
		},
		{
			name:    "zmin 2 empties short records",
			zmin:    2,
			want:    []weighted{tx(12), tx(2, 0, 1), tx(1, 0, 1, 2)},
			maxSize: 3,
			frq:     []Supp{3, 3, 1},
		},
		{
			name:    "zmin 4 empties every record",
			zmin:    4,
			want:    []weighted{tx(15)},
			maxSize: 0,
			frq:     []Supp{0, 0, 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, bag := buildIntBag(t, recs, 1, tt.zmin)
			if got := collect(bag.All()); !equalWeighted(got, tt.want) {
				t.Errorf("All() = %v, want %v", got, tt.want)
			}
			if bag.TotalWeight() != 15 || bag.Records() != 5 {
				t.Errorf("TotalWeight, Records = %d, %d, want 15, 5", bag.TotalWeight(), bag.Records())
			}
			if bag.MaxSize() != tt.maxSize {
				t.Errorf("MaxSize() = %d, want %d", bag.MaxSize(), tt.maxSize)
			}
			if got := bag.Frequencies(); !slices.Equal(got, tt.frq) {
				t.Errorf("Frequencies() = %v, want %v", got, tt.frq)
			}
		})
	}
}

func TestItemBase_BuildBag_zeroWeights(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		recs    []intRecord
		want    []weighted
		maxSize int
		extent  int64
	}{
		{
			name: "single record of weight 0 is kept",
			recs: []intRecord{rec(0, 0, 1)},
			want: []weighted{tx(0)}, // its items have frequency 0
		},
		{
			name: "zero groups are deleted",
			recs: []intRecord{
				rec(1, 0), rec(0, 0, 1, 2), rec(0, 0, 1, 2), rec(0), rec(1, 1),
			},
			want:    []weighted{tx(1, 0), tx(1, 1)},
			maxSize: 2, // upstream keeps the size of deleted transactions
			extent:  2,
		},
		{
			name:    "zero record merges with positive one",
			recs:    []intRecord{rec(0, 0, 1), rec(2, 0, 1), rec(0, 0, 1)},
			want:    []weighted{tx(2, 0, 1)},
			maxSize: 2,
			extent:  2,
		},
		{
			name: "all weights zero",
			recs: []intRecord{rec(0, 0), rec(0, 1)},
			want: []weighted{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, bag := buildIntBag(t, tt.recs, 1, 0)
			got := collect(bag.All())
			if !equalWeighted(got, tt.want) {
				t.Errorf("All() = %v, want %v", got, tt.want)
			}
			if bag.Len() != len(tt.want) || bag.Records() != len(tt.recs) {
				t.Errorf("Len, Records = %d, %d, want %d, %d",
					bag.Len(), bag.Records(), len(tt.want), len(tt.recs))
			}
			if bag.MaxSize() != tt.maxSize || bag.Extent() != tt.extent {
				t.Errorf("MaxSize, Extent = %d, %d, want %d, %d",
					bag.MaxSize(), bag.Extent(), tt.maxSize, tt.extent)
			}
		})
	}
}

func TestBag_Pack(t *testing.T) {
	t.Parallel()
	recs := []intRecord{
		rec(1, 0, 1, 2, 3, 4, 5),
		rec(2, 1, 4),
		rec(3, 4, 5),
		rec(4, 2),
		rec(5),
	}
	_, bag := buildIntBag(t, recs, 1, 0)
	unpacked := collect(bag.All())
	bag.Pack(3)
	if bag.PackCount() != 3 {
		t.Fatalf("PackCount() = %d, want 3", bag.PackCount())
	}
	want := []weighted{
		tx(5),
		tx(1, End|0b111, 3, 4, 5),
		tx(2, End|0b010, 4),
		tx(4, End|0b100),
		tx(3, 4, 5), // nothing to pack
	}
	got := collect(bag.All())
	if !equalWeighted(got, want) {
		t.Errorf("All() after Pack(3) = %v, want %v", got, want)
	}
	slices.Reverse(want)
	if got := collect(bag.Backward()); !equalWeighted(got, want) {
		t.Errorf("Backward() after Pack(3) = %v, want %v", got, want)
	}
	if got, want := bag.Frequencies(), []Supp{7, 0, 0, 1, 6, 4}; !slices.Equal(got, want) {
		t.Errorf("Frequencies() = %v, want %v", got, want)
	}
	if bag.Extent() != 11 || bag.MaxSize() != 6 {
		t.Errorf("Extent, MaxSize = %d, %d, want 11, 6", bag.Extent(), bag.MaxSize())
	}

	bag.Pack(2) // a smaller count keeps the larger one
	if bag.PackCount() != 3 {
		t.Errorf("PackCount() after Pack(2) = %d, want 3", bag.PackCount())
	}
	bag.Pack(0)
	bag.Pack(-1)
	if bag.PackCount() != 3 {
		t.Errorf("PackCount() after Pack(0), Pack(-1) = %d, want 3", bag.PackCount())
	}
	bag.Pack(100) // clamped like ta_pack, beyond the number of items
	if bag.PackCount() != maxPack {
		t.Errorf("PackCount() after Pack(100) = %d, want %d", bag.PackCount(), maxPack)
	}
	want = []weighted{
		tx(5),
		tx(1, End|0b111111),
		tx(2, End|0b010010),
		tx(4, End|0b000100),
		tx(3, End|0b110000),
	}
	if got := collect(bag.All()); !equalWeighted(got, want) {
		t.Errorf("All() after Pack(100) = %v, want %v", got, want)
	}
	if got, want := bag.Frequencies(), []Supp{10, 0, 0, 0, 0, 0}; !slices.Equal(got, want) {
		t.Errorf("Frequencies() after Pack(100) = %v, want %v", got, want)
	}
	if len(unpacked) != bag.Len() {
		t.Errorf("packing changed the number of transactions: %d, want %d", bag.Len(), len(unpacked))
	}
}

func TestBag_iterationStops(t *testing.T) {
	t.Parallel()
	recs := []intRecord{rec(1, 0, 1), rec(1, 0, 2), rec(1, 1), rec(1)}
	_, bag := buildIntBag(t, recs, 1, 0)
	bag.Pack(1)
	for _, seq := range []iter.Seq2[[]Item, Supp]{bag.All(), bag.Backward()} {
		n := 0
		for range seq {
			n++
			if n == 2 {
				break
			}
		}
		if n != 2 {
			t.Errorf("iteration ran %d steps after break, want 2", n)
		}
	}
	// The buffer is restored after a packed transaction was yielded.
	want := []weighted{tx(1), tx(1, End|1, 1), tx(1, End|1, 2), tx(1, 1)}
	if got := collect(bag.All()); !equalWeighted(got, want) {
		t.Errorf("All() = %v, want %v", got, want)
	}
}

func TestItemBase_BuildBag_emptyDatabase(t *testing.T) {
	t.Parallel()
	b, bag := buildIntBag(t, nil, 1, 0)
	if b.Len() != 0 || b.Records() != 0 || b.TotalWeight() != 0 {
		t.Errorf("item base Len, Records, TotalWeight = %d, %d, %d, want 0",
			b.Len(), b.Records(), b.TotalWeight())
	}
	if bag.Len() != 0 || bag.Records() != 0 || bag.NumItems() != 0 || bag.NodeCount() != 0 {
		t.Errorf("bag Len, Records, NumItems, NodeCount = %d, %d, %d, %d, want 0",
			bag.Len(), bag.Records(), bag.NumItems(), bag.NodeCount())
	}
	bag.Pack(16)
	if got := collect(bag.All()); len(got) != 0 {
		t.Errorf("All() = %v, want nothing", got)
	}
	if got := collect(bag.Backward()); len(got) != 0 {
		t.Errorf("Backward() = %v, want nothing", got)
	}
	if got := bag.Frequencies(); got == nil || len(got) != 0 {
		t.Errorf("Frequencies() = %#v, want empty slice", got)
	}
	var zero Bag
	if got := collect(zero.All()); len(got) != 0 {
		t.Errorf("zero Bag All() = %v, want nothing", got)
	}
	if got := collect(zero.Backward()); len(got) != 0 {
		t.Errorf("zero Bag Backward() = %v, want nothing", got)
	}
}

func TestItemBase_BuildBag_largeFanout(t *testing.T) {
	t.Parallel()
	// Item 0 gets many children (more than bigFanout), inserted in
	// descending order, then looked up again.
	const n = 50
	recs := []intRecord{}
	for i := range n {
		recs = append(recs, rec(1, i)) // fixes codes = identifiers
	}
	for i := n - 1; i >= 1; i-- {
		recs = append(recs, rec(1, 0, i), rec(1, i, 0))
	}
	_, bag := buildIntBag(t, recs, 1, 0)
	want := []weighted{tx(1, 0)}
	for i := 1; i < n; i++ {
		want = append(want, tx(2, 0, Item(i)))
	}
	for i := 1; i < n; i++ {
		want = append(want, tx(1, Item(i)))
	}
	if got := collect(bag.All()); !equalWeighted(got, want) {
		t.Errorf("All() = %v, want %v", got, want)
	}
	if bag.NodeCount() != 2*n-1 {
		t.Errorf("NodeCount() = %d, want %d", bag.NodeCount(), 2*n-1)
	}
}

func TestItemBase_BuildBag_notReplayable(t *testing.T) {
	t.Parallel()
	pass1 := []intRecord{rec(1, 0, 1), rec(2, 1)}
	tests := []struct {
		name  string
		pass2 []intRecord
		want  error
	}{
		{name: "more records", pass2: append(slices.Clone(pass1), rec(1, 0)), want: ErrNotReplayable},
		{name: "fewer records", pass2: pass1[:1], want: ErrNotReplayable},
		{name: "other weight", pass2: []intRecord{rec(1, 0, 1), rec(3, 1)}, want: ErrNotReplayable},
		{name: "unknown item", pass2: []intRecord{rec(1, 0, 7), rec(2, 1)}, want: ErrNotReplayable},
		{name: "negative weight", pass2: []intRecord{rec(1, 0, 1), rec(-2, 1)}, want: ErrNegativeWeight},
		{name: "overflow", pass2: []intRecord{rec(math.MaxInt64, 0, 1), rec(2, 1)}, want: ErrWeightOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := NewItemBase[int]()
			if err := b.Count(intScan(pass1)); err != nil {
				t.Fatal(err)
			}
			b.Recode(1, 0)
			_, err := b.BuildBag(intScan(tt.pass2), 0)
			if !errors.Is(err, tt.want) {
				t.Errorf("BuildBag() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestItemBase_BuildBag_scanError(t *testing.T) {
	t.Parallel()
	recs := []intRecord{rec(1, 0)}
	b := NewItemBase[int]()
	if err := b.Count(intScan(recs)); err != nil {
		t.Fatal(err)
	}
	b.Recode(1, 0)
	errScan := errors.New("scan failed")
	_, err := b.BuildBag(func(yield func([]int, Supp) bool) error {
		yield([]int{0}, 1)
		return errScan
	}, 0)
	if !errors.Is(err, errScan) {
		t.Errorf("BuildBag() error = %v, want %v", err, errScan)
	}
}

func TestItemBase_BuildBag_beforeRecode(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("BuildBag before Recode did not panic")
		}
	}()
	b := NewItemBase[int]()
	_, _ = b.BuildBag(intScan(nil), 0)
}

// oracleBag computes the expected bag with plain sorting and merging:
// recode, sort, deduplicate, filter, merge equal transactions, sort
// lexicographically, delete zero weights if there are two or more
// records.
func oracleBag(b *ItemBase[int], recs []intRecord, zmin int) []weighted {
	merged := map[string]*weighted{}
	for _, r := range recs {
		t := []Item{}
		for _, id := range r.items {
			if c, ok := b.Code(id); ok {
				t = append(t, c)
			}
		}
		slices.Sort(t)
		t = slices.Compact(t)
		if zmin > 1 && len(t) < zmin {
			t = t[:0]
		}
		key := fmt.Sprint(t)
		if m, ok := merged[key]; ok {
			m.w += r.w
		} else {
			merged[key] = &weighted{items: t, w: r.w}
		}
	}
	out := []weighted{}
	for _, m := range slices.Collect(maps.Values(merged)) {
		if len(recs) > 1 && m.w == 0 {
			continue
		}
		out = append(out, *m)
	}
	slices.SortFunc(out, func(a, b weighted) int { return slices.Compare(a.items, b.items) })
	return out
}

// randomRecords draws records over n items with weights in [0, wmax].
func randomRecords(r *rand.Rand, nrec, n, maxlen int, wmax Supp) []intRecord {
	recs := make([]intRecord, nrec)
	for k := range recs {
		items := make([]int, r.IntN(maxlen+1))
		for j := range items {
			items[j] = r.IntN(n)
		}
		recs[k] = intRecord{items: items, w: Supp(r.Int64N(int64(wmax) + 1))}
	}
	return recs
}

// checkBagInvariants compares a bag with the oracle and checks the
// counters derived from it.
func checkBagInvariants(t *testing.T, b *ItemBase[int], bag *Bag, recs []intRecord, zmin int) {
	t.Helper()
	want := oracleBag(b, recs, zmin)
	got := collect(bag.All())
	if !equalWeighted(got, want) {
		t.Fatalf("All() = %v, want %v", got, want)
	}
	bwd := collect(bag.Backward())
	slices.Reverse(bwd)
	if !equalWeighted(bwd, want) {
		t.Fatalf("reversed Backward() = %v, want %v", bwd, want)
	}
	var sum Supp
	var extent int64
	frq := make([]Supp, b.Len())
	for _, tr := range got {
		sum += tr.w
		extent += int64(len(tr.items))
		for _, i := range tr.items {
			frq[i] += tr.w
		}
	}
	if sum != bag.TotalWeight() || sum != b.TotalWeight() {
		t.Errorf("sum of weights %d, bag total %d, item base total %d", sum, bag.TotalWeight(), b.TotalWeight())
	}
	if extent != bag.Extent() || len(got) != bag.Len() || len(recs) != bag.Records() {
		t.Errorf("Extent, Len, Records = %d, %d, %d, want %d, %d, %d",
			bag.Extent(), bag.Len(), bag.Records(), extent, len(got), len(recs))
	}
	if f := bag.Frequencies(); !slices.Equal(f, frq) {
		t.Errorf("Frequencies() = %v, want %v", f, frq)
	}
}

func TestItemBase_BuildBag_random(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(1, 2))
	for i := range 300 {
		recs := randomRecords(r, r.IntN(60), 1+r.IntN(30), 1+r.IntN(12), Supp(r.IntN(4)))
		zmin := r.IntN(4)
		b := NewItemBase[int]()
		if err := b.Count(intScan(recs)); err != nil {
			t.Fatal(err)
		}
		b.Recode(Supp(1+r.IntN(3)), r.IntN(5)-2)
		bag, err := b.BuildBag(intScan(recs), zmin)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		checkBagInvariants(t, b, bag, recs, zmin)
	}
}

func FuzzItemBase_BuildBag(f *testing.F) {
	f.Add([]byte{0x01, 0x12, 0x80, 0x23, 0x01}, uint8(0))
	f.Add([]byte{0x81, 0x81, 0x00, 0x00, 0x90}, uint8(2))
	f.Add([]byte{}, uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, z uint8) {
		// Each byte adds item b&0x0f to the current record; a set bit 7
		// ends the record, bits 4..6 give its weight minus one or zero.
		recs := []intRecord{}
		cur := intRecord{items: []int{}}
		for _, c := range data {
			cur.items = append(cur.items, int(c&0x0f))
			if c&0x80 != 0 {
				cur.w = Supp(c>>4&0x07) - 1
				cur.w = max(cur.w, 0)
				recs = append(recs, cur)
				cur = intRecord{items: []int{}}
			}
		}
		zmin := int(z % 5)
		b := NewItemBase[int]()
		if err := b.Count(intScan(recs)); err != nil {
			t.Fatal(err)
		}
		b.Recode(1, int(z%5)-2)
		bag, err := b.BuildBag(intScan(recs), zmin)
		if err != nil {
			t.Fatal(err)
		}
		checkBagInvariants(t, b, bag, recs, zmin)
	})
}

func BenchmarkItemBase_BuildBag(b *testing.B) {
	r := rand.New(rand.NewPCG(5, 6))
	recs := make([]intRecord, 50000)
	for k := range recs { // skewed item choice over 2000 items
		items := make([]int, 1+r.IntN(15))
		for j := range items {
			items[j] = min(r.IntN(2000), r.IntN(2000))
		}
		recs[k] = intRecord{items: items, w: 1}
	}
	scan := intScan(recs)
	b.ReportAllocs()
	for b.Loop() {
		base := NewItemBase[int]()
		if err := base.Count(scan); err != nil {
			b.Fatal(err)
		}
		base.Recode(2, 2)
		bag, err := base.BuildBag(scan, 0)
		if err != nil {
			b.Fatal(err)
		}
		for range bag.Backward() {
		}
	}
}
