package fpg

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

func TestIdxQsort(t *testing.T) {
	t.Parallel()
	// Hand-traced tie orders of i2i_qsort (arrays.c): equal keys keep
	// their order in the ascending pass, so descending reverses them.
	tests := []struct {
		name  string
		vals  []tract.Supp
		dir   int
		index []int32
	}{
		{"all equal descending", []tract.Supp{5, 5, 5}, -1, []int32{2, 1, 0}},
		{"all equal ascending", []tract.Supp{5, 5, 5}, +1, []int32{0, 1, 2}},
		{"single", []tract.Supp{7}, -1, []int32{0}},
		{"two", []tract.Supp{1, 2}, -1, []int32{1, 0}},
		{"distinct", []tract.Supp{3, 1, 2, 5, 4}, -1, []int32{3, 4, 0, 2, 1}},
	}
	for _, tt := range tests {
		index := make([]int32, len(tt.vals))
		for i := range index {
			index[i] = int32(i)
		}
		idxQsort(index, tt.dir, tt.vals)
		if !slices.Equal(index, tt.index) {
			t.Errorf("%s: %v, want %v", tt.name, index, tt.index)
		}
	}
	r := rand.New(rand.NewPCG(3, 4))
	for range 5000 {
		n := r.IntN(40)
		vals := make([]tract.Supp, n)
		for i := range vals {
			vals[i] = tract.Supp(r.IntN(1 + r.IntN(10)))
		}
		index := make([]int32, n)
		for i := range index {
			index[i] = int32(n - 1 - i)
		}
		dir := 1 - 2*r.IntN(2)
		idxQsort(index, dir, vals)
		seen := make([]bool, n)
		for k, x := range index {
			seen[x] = true
			if k > 0 && tract.Supp(dir)*(vals[x]-vals[index[k-1]]) < 0 {
				t.Fatalf("not sorted (dir %d): %v by %v", dir, index, vals)
			}
		}
		if slices.Contains(seen, false) {
			t.Fatalf("not a permutation: %v", index)
		}
	}
}
