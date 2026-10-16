package tract

import (
	"bufio"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// keysFromSpec parses frequencies in identifier order; "x" marks an item
// to be removed (APP_NONE).
func keysFromSpec(t *testing.T, fields []string) []itemKey {
	t.Helper()
	keys := make([]itemKey, len(fields))
	for i, f := range fields {
		if f == "x" {
			keys[i] = itemKey{isNone: true}
			continue
		}
		v, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			t.Fatalf("bad key %q", f)
		}
		keys[i] = itemKey{frq: Supp(v), xfq: Supp(v)}
	}
	return keys
}

// sortedIDs sorts the identifiers 0..len(keys)-1 like ib_recode.
func sortedIDs(keys []itemKey, order int) []int32 {
	ids := make([]int32, len(keys))
	for i := range ids {
		ids[i] = int32(i)
	}
	ptrQsort(ids, itemComparator(keys, order))
	return ids
}

func TestPtrQsort_ties(t *testing.T) {
	t.Parallel()
	// docs/upstream/tract.md §2.7 (R7): descmp on equal frequencies.
	tests := []struct {
		name string
		keys string
		want []int32
	}{
		{name: "six ties", keys: "5 5 5 5 5 5", want: []int32{0, 4, 3, 2, 1, 5}},
		{name: "four ties", keys: "5 5 5 5", want: []int32{0, 2, 1, 3}},
		{name: "ties and removed last", keys: "5 5 5 5 x", want: []int32{0, 3, 2, 1, 4}},
		{name: "removed in between", keys: "5 x 5 5 5", want: []int32{0, 3, 2, 4, 1}},
		{name: "single", keys: "7", want: []int32{0}},
		{name: "empty", keys: "", want: []int32{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			keys := keysFromSpec(t, strings.Fields(tt.keys))
			if got := sortedIDs(keys, 1); !slices.Equal(got, tt.want) {
				t.Errorf("sorted ids = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPtrQsort_upstream replays testdata/qsort.txt: pseudo-random arrays
// with many ties sorted by upstream's ptr_qsort (qsortdump.c).
func TestPtrQsort_upstream(t *testing.T) {
	t.Parallel()
	f, err := os.Open(filepath.Join("testdata", "qsort.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	n := 0
	for sc.Scan() {
		n++
		lhs, rhs, ok := strings.Cut(sc.Text(), " |")
		fields := strings.Fields(lhs)
		if !ok || len(fields) == 0 || !strings.HasPrefix(fields[0], "q=") {
			t.Fatalf("line %d: malformed", n)
		}
		order, err := strconv.Atoi(strings.TrimPrefix(fields[0], "q="))
		if err != nil {
			t.Fatalf("line %d: %v", n, err)
		}
		want := []int32{}
		for _, s := range strings.Fields(rhs) {
			v, err := strconv.Atoi(s)
			if err != nil {
				t.Fatalf("line %d: %v", n, err)
			}
			want = append(want, int32(v))
		}
		keys := keysFromSpec(t, fields[1:])
		if got := sortedIDs(keys, order); !slices.Equal(got, want) {
			t.Errorf("line %d (q=%d, %d items): sorted ids = %v, want %v", n, order, len(keys), got, want)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n < 100 {
		t.Fatalf("only %d arrays in testdata/qsort.txt", n)
	}
}

func TestPtrQsort_sorts(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(3, 4))
	for range 500 {
		a := make([]int, r.IntN(300))
		for i := range a {
			a[i] = r.IntN(1 + r.IntN(20))
		}
		want := slices.Clone(a)
		slices.Sort(want)
		ptrQsort(a, func(x, y int) int { return x - y })
		if !slices.Equal(a, want) {
			t.Fatalf("ptrQsort result %v is not sorted", a)
		}
	}
}

func TestItemComparator(t *testing.T) {
	t.Parallel()
	// frq and xfq order the items differently, so every mode is visible.
	keys := []itemKey{
		{frq: 3, xfq: 9},
		{frq: 5, xfq: 6},
		{isNone: true, frq: 9, xfq: 99},
		{frq: 1, xfq: 12},
	}
	tests := []struct {
		name  string
		order int
		want  []int32
	}{
		{name: "q -3 ascending xfq", order: -3, want: []int32{1, 0, 3, 2}},
		{name: "q -2 ascending xfq", order: -2, want: []int32{1, 0, 3, 2}},
		{name: "q -1 ascending frq", order: -1, want: []int32{3, 0, 1, 2}},
		{name: "q 0 identifiers", order: 0, want: []int32{0, 1, 3, 2}},
		{name: "q 1 descending frq", order: 1, want: []int32{1, 0, 3, 2}},
		{name: "q 2 descending xfq", order: 2, want: []int32{3, 0, 1, 2}},
		{name: "q min int", order: -1 << 63, want: []int32{1, 0, 3, 2}},
		{name: "q max int", order: 1<<63 - 1, want: []int32{3, 0, 1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sortedIDs(keys, tt.order); !slices.Equal(got, tt.want) {
				t.Errorf("sorted ids = %v, want %v", got, tt.want)
			}
		})
	}
}
