package tract

import (
	"errors"
	"math"
	"slices"
	"testing"
)

// strRecord is a test record over string identifiers.
type strRecord struct {
	items []string
	w     Supp
}

func strScan(recs ...strRecord) ScanFunc[string] {
	return func(yield func([]string, Supp) bool) error {
		for _, r := range recs {
			if !yield(r.items, r.w) {
				return nil
			}
		}
		return nil
	}
}

func srec(w Supp, items ...string) strRecord { return strRecord{items: items, w: w} }

// itemRow is the observable state of one item.
type itemRow struct {
	id       string
	app      Appearance
	frq, xfq Supp
}

func itemRows(b *ItemBase[string]) []itemRow {
	rows := []itemRow{}
	for i := range Item(b.Len()) {
		rows = append(rows, itemRow{id: b.ID(i), app: b.Appearance(i), frq: b.Frequency(i), xfq: b.ExtFrequency(i)})
	}
	return rows
}

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic", name)
		}
	}()
	f()
}

func TestNewItemBase(t *testing.T) {
	t.Parallel()
	b := NewItemBase[string]()
	if b.Len() != 0 || b.Records() != 0 || b.TotalWeight() != 0 || b.MaxFrequency() != 0 {
		t.Errorf("new item base: Len %d, Records %d, TotalWeight %d, MaxFrequency %d, want 0",
			b.Len(), b.Records(), b.TotalWeight(), b.MaxFrequency())
	}
	if b.DefaultAppearance() != AppearBoth {
		t.Errorf("DefaultAppearance() = %d, want %d", b.DefaultAppearance(), AppearBoth)
	}
	if _, ok := b.Code("a"); ok {
		t.Error("Code of an unknown item reports ok")
	}
}

func TestItemBase_Count(t *testing.T) {
	t.Parallel()
	b := NewItemBase[string]()
	err := b.Count(strScan(
		srec(2, "a", "b", "a"), // the duplicate counts once
		srec(3),                // empty record
		srec(1, "c", "a", "c", "c"),
		srec(0, "b"), // zero weight
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []itemRow{
		{id: "a", app: AppearBoth, frq: 3, xfq: 2*2 + 2*1},
		{id: "b", app: AppearBoth, frq: 2, xfq: 2 * 2},
		{id: "c", app: AppearBoth, frq: 1, xfq: 2},
	}
	if got := itemRows(b); !slices.Equal(got, want) {
		t.Errorf("items = %v, want %v", got, want)
	}
	if b.Records() != 4 || b.TotalWeight() != 6 || b.MaxFrequency() != 3 {
		t.Errorf("Records, TotalWeight, MaxFrequency = %d, %d, %d, want 4, 6, 3",
			b.Records(), b.TotalWeight(), b.MaxFrequency())
	}
	if c, ok := b.Code("c"); !ok || c != 2 {
		t.Errorf("Code(c) = %d, %v, want 2, true", c, ok)
	}

	// A second pass accumulates like a second tbg_read.
	if err := b.Count(strScan(srec(1, "d", "a"))); err != nil {
		t.Fatal(err)
	}
	if b.Records() != 5 || b.TotalWeight() != 7 || b.Len() != 4 || b.Frequency(0) != 4 {
		t.Errorf("after second pass: Records %d, TotalWeight %d, Len %d, Frequency(a) %d, want 5, 7, 4, 4",
			b.Records(), b.TotalWeight(), b.Len(), b.Frequency(0))
	}
}

func TestItemBase_Count_errors(t *testing.T) {
	t.Parallel()
	errScan := errors.New("scan failed")
	tests := []struct {
		name    string
		scan    ScanFunc[string]
		want    error
		records int
	}{
		{
			name:    "negative weight",
			scan:    strScan(srec(1, "a"), srec(-1, "b"), srec(1, "c")),
			want:    ErrNegativeWeight,
			records: 1,
		},
		{
			name:    "total weight overflow",
			scan:    strScan(srec(math.MaxInt64-1, "a"), srec(1), srec(1, "b")),
			want:    ErrWeightOverflow,
			records: 2,
		},
		{
			name: "scan error",
			scan: func(yield func([]string, Supp) bool) error {
				yield([]string{"a"}, 1)
				return errScan
			},
			want:    errScan,
			records: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := NewItemBase[string]()
			err := b.Count(tt.scan)
			if !errors.Is(err, tt.want) {
				t.Errorf("Count() error = %v, want %v", err, tt.want)
			}
			if b.Records() != tt.records {
				t.Errorf("Records() = %d, want %d (the scan must stop at the error)", b.Records(), tt.records)
			}
		})
	}
}

func TestItemBase_Count_saturation(t *testing.T) {
	t.Parallel()
	// size·weight overflows int64; the size-weighted frequency saturates.
	const w = math.MaxInt64 / 2
	b := NewItemBase[string]()
	if err := b.Count(strScan(srec(w, "a", "b", "c"), srec(1, "a"))); err != nil {
		t.Fatal(err)
	}
	if got := b.ExtFrequency(0); got != math.MaxInt64 {
		t.Errorf("ExtFrequency(a) = %d, want %d", got, int64(math.MaxInt64))
	}
	if got := b.Frequency(0); got != w+1 {
		t.Errorf("Frequency(a) = %d, want %d", got, int64(w+1))
	}
	if got := b.TotalWeight(); got != w+1 {
		t.Errorf("TotalWeight() = %d, want %d", got, int64(w+1))
	}
	if got := mulSaturated(3, 4); got != 12 {
		t.Errorf("mulSaturated(3, 4) = %d, want 12", got)
	}
	if got := addSaturated(math.MaxInt64-1, 5); got != math.MaxInt64 {
		t.Errorf("addSaturated(MaxInt64-1, 5) = %d, want MaxInt64", got)
	}
}

func TestItemBase_Select(t *testing.T) {
	t.Parallel()
	b := NewItemBase[string]()
	if err := b.Select("b", "a", "b", "z"); err != nil { // the duplicate is skipped
		t.Fatal(err)
	}
	if b.DefaultAppearance() != AppearNone || b.Len() != 3 {
		t.Fatalf("after Select: DefaultAppearance %d, Len %d, want %d, 3", b.DefaultAppearance(), b.Len(), AppearNone)
	}
	recs := []strRecord{
		srec(1, "a", "c", "b"), // c is not selected and does not count in the size
		srec(2, "c", "d"),      // becomes an empty record
		srec(1),
	}
	if err := b.Count(strScan(recs...)); err != nil {
		t.Fatal(err)
	}
	want := []itemRow{
		{id: "b", app: AppearBoth, frq: 1, xfq: 2},
		{id: "a", app: AppearBoth, frq: 1, xfq: 2},
		{id: "z", app: AppearBoth, frq: 0, xfq: 0},
	}
	if got := itemRows(b); !slices.Equal(got, want) {
		t.Errorf("items = %v, want %v", got, want)
	}
	if b.Records() != 3 || b.TotalWeight() != 4 {
		t.Errorf("Records, TotalWeight = %d, %d, want 3, 4", b.Records(), b.TotalWeight())
	}
	if n := b.Recode(1, 0); n != 2 {
		t.Fatalf("Recode() = %d, want 2", n)
	}
	for _, id := range []string{"c", "d", "z"} {
		if _, ok := b.Code(id); ok {
			t.Errorf("Code(%s) reports ok after Recode", id)
		}
	}
	bag, err := b.BuildBag(strScan(recs...), 0)
	if err != nil {
		t.Fatal(err)
	}
	wantBag := []weighted{tx(3), tx(1, 0, 1)}
	if got := collect(bag.All()); !equalWeighted(got, wantBag) {
		t.Errorf("All() = %v, want %v", got, wantBag)
	}
}

func TestItemBase_Select_empty(t *testing.T) {
	t.Parallel()
	b := NewItemBase[string]()
	if err := b.Select(); err != nil {
		t.Fatal(err)
	}
	if err := b.Count(strScan(srec(1, "a"), srec(1, "b"))); err != nil {
		t.Fatal(err)
	}
	if b.Len() != 0 || b.Records() != 2 || b.TotalWeight() != 2 {
		t.Errorf("Len, Records, TotalWeight = %d, %d, %d, want 0, 2, 2", b.Len(), b.Records(), b.TotalWeight())
	}
	if n := b.Recode(1, 2); n != 0 {
		t.Errorf("Recode() = %d, want 0", n)
	}
}

func TestItemBase_appearance(t *testing.T) {
	t.Parallel()
	b := NewItemBase[string]()
	b.SetDefaultAppearance(AppearBody)
	if err := b.AddAppearance("x", AppearNone); err != nil {
		t.Fatal(err)
	}
	if err := b.AddAppearance("y", AppearHead); err != nil {
		t.Fatal(err)
	}
	if err := b.AddAppearance("y", AppearBoth); !errors.Is(err, ErrDuplicateItem) {
		t.Errorf("AddAppearance of a known item: error = %v, want %v", err, ErrDuplicateItem)
	}
	if err := b.Count(strScan(srec(1, "x", "y", "z"), srec(1, "x"))); err != nil {
		t.Fatal(err)
	}
	want := []itemRow{
		{id: "x", app: AppearNone, frq: 2, xfq: 4}, // counted, sizes include it
		{id: "y", app: AppearHead, frq: 1, xfq: 3},
		{id: "z", app: AppearBody, frq: 1, xfq: 3}, // new item, default appearance
	}
	if got := itemRows(b); !slices.Equal(got, want) {
		t.Errorf("items = %v, want %v", got, want)
	}
	if n := b.Recode(1, 1); n != 2 { // x is removed despite its frequency
		t.Fatalf("Recode() = %d, want 2", n)
	}
	if _, ok := b.Code("x"); ok {
		t.Error("item with AppearNone survived Recode")
	}
	for id, app := range map[string]Appearance{"y": AppearHead, "z": AppearBody} {
		c, ok := b.Code(id)
		if !ok || b.Appearance(c) != app {
			t.Errorf("Appearance(%s) = %d (ok %v), want %d", id, b.Appearance(c), ok, app)
		}
	}

	ignore := NewItemBase[string]()
	ignore.SetDefaultAppearance(AppearNone)
	if err := ignore.AddAppearance("a", AppearBoth); err != nil {
		t.Fatal(err)
	}
	if err := ignore.Count(strScan(srec(1, "a", "b"))); err != nil {
		t.Fatal(err)
	}
	if ignore.Len() != 1 || ignore.ExtFrequency(0) != 1 {
		t.Errorf("default AppearNone: Len %d, ExtFrequency(a) %d, want 1, 1", ignore.Len(), ignore.ExtFrequency(0))
	}
}

func TestItemBase_Recode(t *testing.T) {
	t.Parallel()
	// First seen: b, c, d, e, a. Frequencies a 7, b 5, c 3, d 2, e 1;
	// size-weighted frequencies b 9, c 8, a 7, d 6, e 4 (no ties).
	recs := []strRecord{
		srec(1, "b", "c", "d", "e"), srec(1, "c", "d"), srec(1, "b", "c"),
		srec(3, "b"), srec(7, "a"),
	}
	tests := []struct {
		name  string
		smin  Supp
		order int
		want  []string
	}{
		{name: "q0 keeps first-seen order", smin: 1, order: 0, want: []string{"b", "c", "d", "e", "a"}},
		{name: "q1 descending frequency", smin: 1, order: 1, want: []string{"a", "b", "c", "d", "e"}},
		{name: "q-1 ascending frequency", smin: 1, order: -1, want: []string{"e", "d", "c", "b", "a"}},
		{name: "q2 descending size-weighted", smin: 1, order: 2, want: []string{"b", "c", "a", "d", "e"}},
		{name: "q-2 ascending size-weighted", smin: 1, order: -2, want: []string{"e", "d", "a", "c", "b"}},
		{name: "minimum support 2", smin: 2, order: 1, want: []string{"a", "b", "c", "d"}},
		{name: "minimum support 3", smin: 3, order: 2, want: []string{"b", "c", "a"}},
		{name: "minimum support above all", smin: 8, order: 1, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := NewItemBase[string]()
			if err := b.Count(strScan(recs...)); err != nil {
				t.Fatal(err)
			}
			n := b.Recode(tt.smin, tt.order)
			got := []string{}
			for i := range Item(b.Len()) {
				got = append(got, b.ID(i))
				if c, ok := b.Code(b.ID(i)); !ok || c != i {
					t.Errorf("Code(%s) = %d, %v, want %d, true", b.ID(i), c, ok, i)
				}
			}
			if n != len(tt.want) || !slices.Equal(got, tt.want) {
				t.Errorf("Recode() = %d, items %v, want %d, %v", n, got, len(tt.want), tt.want)
			}
			if b.Records() != 5 || b.TotalWeight() != 13 || b.MaxFrequency() != 7 {
				t.Errorf("Records, TotalWeight, MaxFrequency after Recode = %d, %d, %d, want 5, 13, 7",
					b.Records(), b.TotalWeight(), b.MaxFrequency())
			}
		})
	}
}

func TestItemBase_Recode_minSupportAtLeastOne(t *testing.T) {
	t.Parallel()
	// Deviation from upstream: a minimum support below 1 does not keep
	// items of frequency 0 (a selected item that never occurs, an item of
	// a zero-weight record); upstream's miners crash on them.
	for _, smin := range []Supp{0, -5} {
		b := NewItemBase[string]()
		if err := b.Select("a", "never"); err != nil {
			t.Fatal(err)
		}
		b.SetDefaultAppearance(AppearBoth)
		if err := b.Count(strScan(srec(1, "a"), srec(0, "zero"))); err != nil {
			t.Fatal(err)
		}
		if n := b.Recode(smin, 2); n != 1 || b.ID(0) != "a" {
			t.Errorf("Recode(%d, 2) kept %d items, want only a", smin, n)
		}
	}
}

func TestItemBase_misuse(t *testing.T) {
	t.Parallel()
	recoded := func() *ItemBase[string] {
		b := NewItemBase[string]()
		b.Recode(1, 0)
		return b
	}
	mustPanic(t, "Count after Recode", func() { _ = recoded().Count(strScan()) })
	mustPanic(t, "Recode after Recode", func() { recoded().Recode(1, 0) })
	mustPanic(t, "Select after Recode", func() { _ = recoded().Select("a") })
	mustPanic(t, "SetDefaultAppearance after Recode", func() { recoded().SetDefaultAppearance(AppearBody) })
	mustPanic(t, "AddAppearance after Recode", func() { _ = recoded().AddAppearance("a", AppearBody) })
	mustPanic(t, "invalid default appearance", func() { NewItemBase[string]().SetDefaultAppearance(4) })
	mustPanic(t, "invalid appearance", func() { _ = NewItemBase[string]().AddAppearance("a", 7) })
}
