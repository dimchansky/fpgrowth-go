package tract

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"
)

// errScan2 is the error of a failing scan in pass 2.
var errScan2 = errors.New("scan failed")

// failingScans returns strScan and bytesScan over recs that fail with
// errScan2 after yielding the first n records.
func failingScans(recs []strRecord, n int) (ScanFunc[string], ByteScanFunc) {
	head := recs[:min(n, len(recs))]
	str, byt := strScan(head...), bytesScan(head...)
	return func(yield func([]string, Supp) bool) error {
			if err := str(yield); err != nil {
				return err
			}
			return errScan2
		}, func(yield func([][]byte, Supp) bool) error {
			if err := byt(yield); err != nil {
				return err
			}
			return errScan2
		}
}

// mutatePass2 returns the records of a second pass over recs: the same
// records, or (mode 1-6) records that differ from pass 1 in one way.
func mutatePass2(r *rand.Rand, recs []strRecord, mode int) []strRecord {
	out := make([]strRecord, len(recs))
	for k, rc := range recs {
		out[k] = srec(rc.w, slices.Clone(rc.items)...)
	}
	if len(out) == 0 {
		return append(out, srec(1, "i1")) // more records than in pass 1
	}
	k := r.IntN(len(out))
	switch mode {
	case 1: // an item never seen in pass 1 (skipped under a selection)
		out[k].items = append(out[k].items, "new"+strconv.Itoa(r.IntN(3)))
	case 2: // more records than in pass 1
		out = append(out, srec(Supp(r.IntN(3)), "i1", "i2"))
	case 3: // fewer records
		out = slices.Delete(out, k, k+1)
	case 4: // another total weight
		out[k].w++
	case 5: // a negative weight
		out[k].w = -1
	case 6: // an overflowing total weight
		out[k].w = 1<<63 - 1
	}
	return out
}

// TestBuildBagBytes_matchesBuildBag checks that BuildBagBytes gives what
// BuildBag gives over the same records with string identifiers: the same
// bag (weights, duplicates, unknown, unselected and infrequent items,
// empty records, the zmin filter) and the same errors with the same
// messages (not replayable, negative and overflowing weights, the
// identifier check, a failing scan).
func TestBuildBagBytes_matchesBuildBag(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(52, 1))
	var built, failed int
	for run := range 2000 {
		recs := make([]strRecord, r.IntN(40))
		for k := range recs {
			items := make([]string, r.IntN(8)) // duplicates and empty records
			for j := range items {
				items[j] = "i" + strconv.Itoa(r.IntN(20))
			}
			recs[k] = srec(Supp(r.IntN(4)), items...)
		}
		mode := 0
		if r.IntN(3) == 0 {
			mode = 1 + r.IntN(7) // 7: a failing scan
		}
		pass2 := recs
		if mode >= 1 && mode <= 6 {
			pass2 = mutatePass2(r, recs, mode)
		}
		smin, order, zmin := Supp(1+r.IntN(3)), r.IntN(5)-2, r.IntN(5)
		bad := "new" + strconv.Itoa(r.IntN(3)) // rejected by the check
		setup := func() *ItemBase[string] {
			b := NewItemBase[string]()
			if run%3 == 1 {
				if err := b.Select("i1", "i3", "i5", "i7", "i11"); err != nil {
					t.Fatal(err)
				}
			}
			if err := b.Count(strScan(recs...)); err != nil {
				t.Fatal(err)
			}
			if run%4 == 2 { // after Count: checks the unknown items of pass 2
				b.SetCheck(func(id string) error {
					if id == bad {
						return errors.New("bad item " + id)
					}
					return nil
				})
			}
			b.Recode(smin, order)
			return b
		}
		sscan, bscan := strScan(pass2...), bytesScan(pass2...)
		if mode == 7 {
			sscan, bscan = failingScans(pass2, r.IntN(len(pass2)+1))
		}
		want, b := setup(), setup()
		wbag, werr := want.BuildBag(sscan, zmin)
		bag, err := BuildBagBytes(b, bscan, zmin)
		if fmt.Sprint(err) != fmt.Sprint(werr) {
			t.Fatalf("run %d (mode %d): error %v, want %v", run, mode, err, werr)
		}
		if werr != nil {
			for _, kind := range []error{ErrNotReplayable, ErrNegativeWeight, ErrWeightOverflow, errScan2} {
				if errors.Is(err, kind) != errors.Is(werr, kind) {
					t.Fatalf("run %d: error %v, want %v (kind %v)", run, err, werr, kind)
				}
			}
			failed++
			continue
		}
		built++
		checkBagsEqual(t, bag, wbag)
	}
	if built < 500 || failed < 300 {
		t.Errorf("%d bags built, %d errors: too few of either", built, failed)
	}
}

// TestBuildBagBytes_errors checks the error kinds of BuildBagBytes.
func TestBuildBagBytes_errors(t *testing.T) {
	t.Parallel()
	recs := []strRecord{srec(1, "a", "b"), srec(2, "b", "c", "b"), srec(1)}
	tests := []struct {
		name  string
		pass2 []strRecord
		want  error
	}{
		{"unknown item", []strRecord{srec(1, "a", "x"), srec(2, "b", "c"), srec(1)}, ErrNotReplayable},
		{"extra record", append(slices.Clone(recs), srec(1, "a")), ErrNotReplayable},
		{"missing record", recs[:2], ErrNotReplayable},
		{"other weight", []strRecord{srec(1, "a", "b"), srec(3, "b", "c"), srec(1)}, ErrNotReplayable},
		{"negative weight", []strRecord{srec(-1, "a")}, ErrNegativeWeight},
		{"weight overflow", []strRecord{srec(1, "a"), srec(1<<63-1, "b")}, ErrWeightOverflow},
	}
	for _, tt := range tests {
		b := NewItemBase[string]()
		if err := b.Count(strScan(recs...)); err != nil {
			t.Fatal(err)
		}
		b.Recode(1, 0)
		if _, err := BuildBagBytes(b, bytesScan(tt.pass2...), 0); !errors.Is(err, tt.want) {
			t.Errorf("%s: error %v, want %v", tt.name, err, tt.want)
		}
	}
	b := NewItemBase[string]()
	mustPanic(t, "BuildBagBytes before Recode", func() { _, _ = BuildBagBytes(b, bytesScan(), 0) })
}

// TestBuildBagBytes_abort checks the abort hook of SetAbort with
// BuildBagBytes: its error stops the build unchanged after the same number
// of calls as with BuildBag, and a scan stopped by a cancellation error
// returns that error.
func TestBuildBagBytes_abort(t *testing.T) {
	t.Parallel()
	recs := make([]strRecord, 2*finishCheckEvery)
	for i := range recs {
		recs[i] = srec(1, strconv.Itoa(i)) // one node per record
	}
	errAbort := errors.New("aborted")
	for _, retain := range []bool{false, true} {
		b := NewItemBase[string]()
		if retain {
			b.Retain(1 << 30)
		}
		if err := b.Count(strScan(recs...)); err != nil {
			t.Fatal(err)
		}
		b.Recode(1, 0)
		calls := 0
		b.SetAbort(func() error { calls++; return errAbort })
		bag, err := BuildBagBytes(b, bytesScan(recs...), 0)
		if bag != nil || err != errAbort || calls != 1 {
			t.Errorf("retain %v: got %v, %v after %d calls", retain, bag, err, calls)
		}
	}
	// A canceled scan (the CLIs' ctxScan) stops pass 2 with its error.
	b := NewItemBase[string]()
	if err := b.Count(strScan(recs...)); err != nil {
		t.Fatal(err)
	}
	b.Recode(1, 0)
	inner := bytesScan(recs...)
	canceled := func(yield func([][]byte, Supp) bool) error {
		n := 0
		_ = inner(func(ids [][]byte, w Supp) bool {
			n++
			return n < 100 && yield(ids, w)
		})
		return errors.New("context canceled")
	}
	if _, err := BuildBagBytes(b, canceled, 0); err == nil || err.Error() != "context canceled" {
		t.Errorf("canceled scan: error %v", err)
	}
}

// TestBuildBagBytes_retained checks that BuildBagBytes builds the bag
// from retained records without scanning, releases them, and scans in a
// later call, giving the same bag every time.
func TestBuildBagBytes_retained(t *testing.T) {
	t.Parallel()
	recs := []strRecord{srec(1, "a", "b", "a"), srec(2, "c"), srec(0), srec(1, "b", "c")}
	want := NewItemBase[string]()
	if err := want.Count(strScan(recs...)); err != nil {
		t.Fatal(err)
	}
	want.Recode(1, 0)
	wbag, err := want.BuildBag(strScan(recs...), 2)
	if err != nil {
		t.Fatal(err)
	}
	b := NewItemBase[string]()
	b.Retain(1 << 20)
	if err := CountBytes(b, bytesScan(recs...)); err != nil {
		t.Fatal(err)
	}
	b.Recode(1, 0)
	passes := 0
	scan := func(yield func([][]byte, Supp) bool) error {
		passes++
		return bytesScan(recs...)(yield)
	}
	for k := range 2 {
		bag, err := BuildBagBytes(b, scan, 2)
		if err != nil {
			t.Fatal(err)
		}
		if passes != k || b.Retaining() {
			t.Fatalf("call %d: %d passes, retaining %v", k, passes, b.Retaining())
		}
		checkBagsEqual(t, bag, wbag)
	}
}

// TestBuildBagBytes_noAllocation checks that BuildBagBytes converts no
// known identifier to a string: the allocations of a build do not grow
// with the number of records of known items.
func TestBuildBagBytes_noAllocation(t *testing.T) {
	ids := [][]byte{[]byte("alpha"), []byte("beta"), []byte("gamma"), []byte("alpha"), []byte("delta")}
	allocs := func(n int) float64 {
		b := NewItemBase[string]()
		if err := b.Select("alpha", "beta", "gamma"); err != nil { // delta is skipped
			t.Fatal(err)
		}
		scan := func(yield func([][]byte, Supp) bool) error {
			for range n {
				if !yield(ids, 1) {
					return nil
				}
			}
			return nil
		}
		if err := CountBytes(b, scan); err != nil {
			t.Fatal(err)
		}
		b.Recode(1, 0)
		return testing.AllocsPerRun(20, func() {
			if _, err := BuildBagBytes(b, scan, 0); err != nil {
				t.Fatal(err)
			}
		})
	}
	if few, many := allocs(10), allocs(10000); many > few {
		t.Errorf("%v allocations for 10000 records, %v for 10", many, few)
	}
}
