package tract

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// itemScan returns a ScanFunc over records of item identifiers, weight 1.
func itemScan(recs [][]Item) ScanFunc[Item] {
	return func(yield func([]Item, Supp) bool) error {
		for _, t := range recs {
			if !yield(t, 1) {
				return nil
			}
		}
		return nil
	}
}

// denseRecords returns cnt records over the identifiers 0..n-1 with
// skewed frequencies, repeated identifiers and empty records.
func denseRecords(r *rand.Rand, cnt, n, maxlen int) [][]Item {
	recs := make([][]Item, cnt)
	for k := range recs {
		t := make([]Item, r.IntN(maxlen+1))
		for i := range t {
			t[i] = Item(min(int(r.ExpFloat64()*float64(n)/3), n-1))
		}
		recs[k] = t
	}
	return recs
}

// checkBagsEqual compares every observable property of two bags.
func checkBagsEqual(t *testing.T, got, want *Bag) {
	t.Helper()
	if got.NumItems() != want.NumItems() || got.Len() != want.Len() || got.Records() != want.Records() ||
		got.TotalWeight() != want.TotalWeight() || got.MaxSize() != want.MaxSize() ||
		got.Extent() != want.Extent() || got.NodeCount() != want.NodeCount() || got.PackCount() != want.PackCount() {
		t.Fatalf("bag properties differ: got %d %d %d %d %d %d %d, want %d %d %d %d %d %d %d",
			got.NumItems(), got.Len(), got.Records(), got.TotalWeight(), got.MaxSize(), got.Extent(), got.NodeCount(),
			want.NumItems(), want.Len(), want.Records(), want.TotalWeight(), want.MaxSize(), want.Extent(), want.NodeCount())
	}
	if !slices.Equal(got.Frequencies(), want.Frequencies()) {
		t.Fatalf("frequencies %v, want %v", got.Frequencies(), want.Frequencies())
	}
	if g, w := collect(got.All()), collect(want.All()); !equalWeighted(g, w) {
		t.Fatalf("transactions %v, want %v", g, w)
	}
	if g, w := collect(got.Backward()), collect(want.Backward()); !equalWeighted(g, w) {
		t.Fatalf("backward transactions %v, want %v", g, w)
	}
}

// TestDenseBase_matchesItemBase checks that a DenseBase, reused for many
// databases, gives exactly what a new ItemBase[Item] gives for each: the
// number of kept items, the identifier and frequency of every code (so
// the ties of every item order), and the bag, with and without the zmin
// filter and packing, including databases where no item is frequent.
func TestDenseBase_matchesItemBase(t *testing.T) {
	r := rand.New(rand.NewPCG(39, 3))
	var db DenseBase
	for it := range 600 {
		n := 1 + r.IntN(60)
		recs := denseRecords(r, r.IntN(150), n, 1+r.IntN(12))
		smin := Supp(r.IntN(6))
		order := r.IntN(5) - 2
		if it%50 == 0 {
			order = []int{math.MinInt, math.MaxInt}[it/50%2]
		}
		zmin := r.IntN(4)
		ib := NewItemBase[Item]()
		if err := ib.Count(itemScan(recs)); err != nil {
			t.Fatal(err)
		}
		if err := db.Count(recs, n); err != nil {
			t.Fatal(err)
		}
		if db.Len() != ib.Len() || db.TotalWeight() != ib.TotalWeight() || db.Records() != ib.Records() {
			t.Fatalf("case %d: counted %d items, %d records, want %d, %d", it, db.Len(), db.Records(), ib.Len(), ib.Records())
		}
		k, kd := ib.Recode(smin, order), db.Recode(smin, order)
		if kd != k || db.Len() != k {
			t.Fatalf("case %d: %d items kept, want %d", it, kd, k)
		}
		for c := range Item(k) {
			if id := db.order[db.ids[c]]; id != ib.ID(c) || db.Frequency(c) != ib.Frequency(c) ||
				db.Appearance(c) != ib.Appearance(c) {
				t.Fatalf("case %d: code %d is item %d (frequency %d), want %d (%d)",
					it, c, id, db.Frequency(c), ib.ID(c), ib.Frequency(c))
			}
		}
		if k == 0 {
			continue
		}
		want, err := ib.BuildBag(itemScan(recs), zmin)
		if err != nil {
			t.Fatal(err)
		}
		got, err := db.BuildBag(zmin)
		if err != nil {
			t.Fatal(err)
		}
		if p := r.IntN(20); p < 8 {
			want.Pack(p)
			got.Pack(p)
		}
		checkBagsEqual(t, got, want)
	}
}

// TestDenseBase_largeFanout checks the reuse of the child index of nodes
// with many children: a database with a big fanout, then one without.
func TestDenseBase_largeFanout(t *testing.T) {
	var db DenseBase
	for _, nchild := range []int{40, 3, 60} {
		var recs [][]Item
		for i := range nchild {
			recs = append(recs, []Item{0, Item(1 + i)}, []Item{0})
		}
		ib := NewItemBase[Item]()
		if err := ib.Count(itemScan(recs)); err != nil {
			t.Fatal(err)
		}
		ib.Recode(1, 2)
		want, err := ib.BuildBag(itemScan(recs), 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Count(recs, nchild+1); err != nil {
			t.Fatal(err)
		}
		db.Recode(1, 2)
		got, err := db.BuildBag(0)
		if err != nil {
			t.Fatal(err)
		}
		checkBagsEqual(t, got, want)
	}
}

// TestDenseBase_order checks that methods called out of order panic.
func TestDenseBase_order(t *testing.T) {
	for name, fn := range map[string]func(*DenseBase){
		"Recode":   func(d *DenseBase) { d.Recode(1, 2) },
		"BuildBag": func(d *DenseBase) { _, _ = d.BuildBag(0) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			var d DenseBase
			fn(&d)
		})
	}
}

// TestSortItems checks the insertion sort against slices.Sort.
func TestSortItems(t *testing.T) {
	r := rand.New(rand.NewPCG(39, 4))
	for range 2000 {
		x := make([]Item, r.IntN(40))
		for i := range x {
			x[i] = Item(r.IntN(30)) - 5
		}
		want := slices.Sorted(slices.Values(x))
		SortItems(x)
		if !slices.Equal(x, want) {
			t.Fatalf("%v, want %v", x, want)
		}
	}
}
