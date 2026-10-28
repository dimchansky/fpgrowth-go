package tract

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"
)

// countingScan is intScan that counts its passes.
func countingScan(recs []intRecord, passes *int) ScanFunc[int] {
	scan := intScan(recs)
	return func(yield func([]int, Supp) bool) error {
		*passes++
		return scan(yield)
	}
}

// setupBase prepares an item base like an -R file would: none, a
// selection, or items with explicit appearances (some AppearNone).
func setupBase(t *testing.T, kind, n int) *ItemBase[int] {
	t.Helper()
	b := NewItemBase[int]()
	switch kind {
	case 1:
		sel := []int{}
		for i := 0; i < n; i += 2 {
			sel = append(sel, i)
		}
		if err := b.Select(sel...); err != nil {
			t.Fatal(err)
		}
	case 2:
		for i := 0; i < n; i += 3 {
			if err := b.AddAppearance(i, AppearNone); err != nil {
				t.Fatal(err)
			}
		}
	}
	return b
}

// TestItemBase_Retain_matchesScan checks that the bag built from the
// retained records is the bag of a second scan, for random databases,
// weights, selections, appearances, minimum supports, orders and zmin
// filters, budgets that hold, that are exceeded on the way, and none,
// and records counted in one or two passes; and that BuildBag scans
// exactly when the records were not retained, and again on a second call.
func TestItemBase_Retain_matchesScan(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(48, 1))
	for run := range 400 {
		n := 1 + r.IntN(30)
		recs := randomRecords(r, r.IntN(80), n, 1+r.IntN(12), Supp(r.IntN(4)))
		if r.IntN(2) == 0 { // unit weights, sometimes one other weight late
			for k := range recs {
				recs[k].w = 1
			}
			if len(recs) > 0 && r.IntN(2) == 0 {
				recs[r.IntN(len(recs))].w = Supp(r.IntN(5))
			}
		}
		kind, zmin, smin, order := r.IntN(3), r.IntN(4), Supp(1+r.IntN(3)), r.IntN(5)-2
		split := r.IntN(len(recs) + 1) // Count passes: recs[:split], recs[split:]
		var limit int
		switch r.IntN(4) {
		case 0:
			limit = 0 // disabled
		case 1:
			limit = 1 + r.IntN(4*retainBlock+16) // may be exceeded on the way
		default:
			limit = 1 << 30
		}
		name := fmt.Sprintf("run %d (kind %d, limit %d)", run, kind, limit)

		want := setupBase(t, kind, n)
		if err := want.Count(intScan(recs)); err != nil {
			t.Fatal(err)
		}
		want.Recode(smin, order)
		wantBag, err := want.BuildBag(intScan(recs), zmin)
		if err != nil {
			t.Fatal(err)
		}

		b := setupBase(t, kind, n)
		b.Retain(limit)
		if err := b.Count(intScan(recs[:split])); err != nil {
			t.Fatal(err)
		}
		if err := b.Count(intScan(recs[split:])); err != nil {
			t.Fatal(err)
		}
		retained := b.ret.on
		if limit <= 0 && retained {
			t.Fatalf("%s: retained without a budget", name)
		}
		if limit == 1<<30 && !retained {
			t.Fatalf("%s: not retained within the budget", name)
		}
		b.Recode(smin, order)
		passes := 0
		bag, err := b.BuildBag(countingScan(recs, &passes), zmin)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if wantPasses := map[bool]int{true: 0, false: 1}[retained]; passes != wantPasses {
			t.Fatalf("%s: %d scans, want %d", name, passes, wantPasses)
		}
		checkBagsEqual(t, bag, wantBag)
		if b.ret.on {
			t.Fatalf("%s: records not released by BuildBag", name)
		}

		// A second BuildBag scans and gives the same bag.
		bag2, err := b.BuildBag(countingScan(recs, &passes), zmin)
		if err != nil {
			t.Fatalf("%s: second BuildBag: %v", name, err)
		}
		if passes != map[bool]int{true: 1, false: 2}[retained] {
			t.Fatalf("%s: second BuildBag did not scan", name)
		}
		checkBagsEqual(t, bag2, wantBag)
	}
}

// TestItemBase_Retain_blocks checks records over several blocks of
// retained identifiers, a record longer than a block, and empty records.
func TestItemBase_Retain_blocks(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(48, 2))
	recs := randomRecords(r, 12000, 500, 20, 1)
	long := make([]int, retainBlock+7) // one record longer than a block
	for i := range long {
		long[i] = 1000 + i
	}
	recs = slices.Insert(recs, 5000, intRecord{items: long, w: 2}, intRecord{w: 1})
	recs = append(recs, intRecord{w: 3}) // an empty last record
	for _, zmin := range []int{0, 3} {
		want, wantBag := buildIntBag(t, recs, 2, zmin)
		b := NewItemBase[int]()
		b.Retain(1 << 30)
		if err := b.Count(intScan(recs)); err != nil {
			t.Fatal(err)
		}
		if len(b.ret.blocks) < 4 {
			t.Fatalf("%d blocks, want at least 4", len(b.ret.blocks))
		}
		b.Recode(2, 0) // as buildIntBag
		passes := 0
		bag, err := b.BuildBag(countingScan(recs, &passes), zmin)
		if err != nil || passes != 0 {
			t.Fatalf("BuildBag: %v after %d scans", err, passes)
		}
		if b.Len() != want.Len() {
			t.Fatalf("%d items, want %d", b.Len(), want.Len())
		}
		checkBagsEqual(t, bag, wantBag)
	}
}

// TestItemBase_Retain_budget checks the accounting of the budget: the
// records are kept while their storage fits, and released (BuildBag
// scans) as soon as it does not.
func TestItemBase_Retain_budget(t *testing.T) {
	t.Parallel()
	// The first record allocates a block (4·retainBlock bytes) and the
	// array of record lengths (a few int32: 8 bytes for two records).
	block := 4 * retainBlock
	limit := block + 12 // two record lengths, but not two weights (16 bytes)
	for _, tt := range []struct {
		limit int
		recs  []intRecord
		want  bool
	}{
		{block - 1, []intRecord{rec(1, 0, 1, 2)}, false},
		{limit, []intRecord{rec(1, 0, 1, 2), rec(1, 1, 2)}, true},
		{limit, []intRecord{rec(1, 0, 1, 2), rec(2, 1, 2)}, false}, // + weights
		{0, []intRecord{rec(1, 0)}, false},
		{-1, []intRecord{rec(1, 0)}, false},
	} {
		b := NewItemBase[int]()
		b.Retain(tt.limit)
		if err := b.Count(intScan(tt.recs)); err != nil {
			t.Fatal(err)
		}
		if b.ret.on != tt.want {
			t.Errorf("limit %d, %v: retained = %v, want %v (used %d)", tt.limit, tt.recs, b.ret.on, tt.want, b.ret.used)
		}
		if !b.ret.on && (b.ret.blocks != nil || b.ret.lens != nil || b.ret.wgts != nil) {
			t.Errorf("limit %d: records not released", tt.limit)
		}
	}
}

// TestItemBase_Retain_abort checks that building the bag from retained
// records calls the abort function every retainCheckEvery records and
// stops with its error.
func TestItemBase_Retain_abort(t *testing.T) {
	t.Parallel()
	recs := make([]intRecord, 3*retainCheckEvery)
	for i := range recs {
		recs[i] = rec(1, i%7)
	}
	errAbort := errors.New("aborted")
	for _, stopAt := range []int{0, 1, 2} {
		b := NewItemBase[int]()
		b.Retain(1 << 30)
		if err := b.Count(intScan(recs)); err != nil {
			t.Fatal(err)
		}
		b.Recode(1, 0)
		calls := 0
		b.SetAbort(func() error {
			if calls++; calls > stopAt {
				return errAbort
			}
			return nil
		})
		passes := 0
		bag, err := b.BuildBag(countingScan(recs, &passes), 0)
		if bag != nil || err != errAbort || calls != stopAt+1 || passes != 0 {
			t.Errorf("stop after %d: got %v, %v after %d calls, %d scans", stopAt, bag, err, calls, passes)
		}
	}
}

// TestItemBase_Retain_misuse checks that Retain panics after Count and
// after Recode.
func TestItemBase_Retain_misuse(t *testing.T) {
	t.Parallel()
	b := NewItemBase[int]()
	if err := b.Count(intScan([]intRecord{rec(1, 0)})); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "Retain after Count", func() { b.Retain(1 << 20) })
	b.Recode(1, 0)
	mustPanic(t, "Retain after Recode", func() { b.Retain(1 << 20) })
}

// bytesScan is strScan over byte-string identifiers; the slices are
// overwritten after every record, as tabread.ScanBytes's are.
func bytesScan(recs ...strRecord) ByteScanFunc {
	return func(yield func([][]byte, Supp) bool) error {
		var ids [][]byte
		for _, r := range recs {
			ids = ids[:0]
			for _, s := range r.items {
				ids = append(ids, []byte(s))
			}
			ok := yield(ids, r.w)
			for _, id := range ids {
				clear(id) // the consumer must not keep them
			}
			if !ok {
				return nil
			}
		}
		return nil
	}
}

// TestCountBytes_matchesCount checks that CountBytes gives what Count
// gives over the same records with string identifiers: the items in
// first-seen order with their counters, the totals, the errors (with
// record numbers), selections and the identifier check.
func TestCountBytes_matchesCount(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(48, 3))
	for run := range 300 {
		recs := make([]strRecord, r.IntN(40))
		for k := range recs {
			items := make([]string, r.IntN(8))
			for j := range items {
				items[j] = "i" + strconv.Itoa(r.IntN(20))
			}
			recs[k] = srec(Supp(r.IntN(4)), items...)
		}
		if len(recs) > 0 && r.IntN(10) == 0 {
			recs[r.IntN(len(recs))].w = -1
		}
		bad := "i" + strconv.Itoa(r.IntN(40)) // rejected by the check
		setup := func() *ItemBase[string] {
			b := NewItemBase[string]()
			if run%3 == 1 {
				if err := b.Select("i1", "i3", "i5", "i7"); err != nil {
					t.Fatal(err)
				}
			}
			if run%4 == 2 {
				b.SetCheck(func(id string) error {
					if id == bad {
						return errors.New("bad item " + id)
					}
					return nil
				})
			}
			return b
		}
		want, b := setup(), setup()
		werr := want.Count(strScan(recs...))
		err := CountBytes(b, bytesScan(recs...))
		if fmt.Sprint(err) != fmt.Sprint(werr) {
			t.Fatalf("run %d: error %v, want %v", run, err, werr)
		}
		if werr != nil {
			continue
		}
		if g, w := itemRows(b), itemRows(want); !slices.Equal(g, w) {
			t.Fatalf("run %d: items %v, want %v", run, g, w)
		}
		if b.Records() != want.Records() || b.TotalWeight() != want.TotalWeight() || b.MaxFrequency() != want.MaxFrequency() {
			t.Fatalf("run %d: totals differ", run)
		}
		for i := range Item(b.Len()) {
			if c, ok := b.Code(want.ID(i)); !ok || c != i {
				t.Fatalf("run %d: code of %q is %d, %v", run, want.ID(i), c, ok)
			}
		}
	}
}

// TestCountBytes_noAllocation checks that CountBytes converts no known
// identifier to a string: counting records of known items allocates
// nothing.
func TestCountBytes_noAllocation(t *testing.T) {
	b := NewItemBase[string]()
	ids := [][]byte{[]byte("alpha"), []byte("beta"), []byte("gamma"), []byte("alpha")}
	if err := countBytesRecord(b, ids, 1); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if err := countBytesRecord(b, ids, 1); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Errorf("%v allocations per record of known items, want 0", allocs)
	}
}
