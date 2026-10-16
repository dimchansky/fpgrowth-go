package fpg

import (
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// thInsert is arrays.c's TH_INSERT, the section size below which the
// quicksort leaves the sorting to the final insertion sort.
const thInsert = 2

// idxQrec is a port of the recursive part of arrays.c's IDX_QSORT
// (i2i_qrec): an unstable quicksort of the index array by the values
// array[index[i]], with a median-of-three pivot, skipping of elements equal
// to the pivot, and recursion into the smaller section only.
func idxQrec(index []int32, array []tract.Supp) {
	for {
		n := len(index)
		l, r := 0, n-1
		e := r
		if array[index[l]] > array[index[r]] { // first and last element
			index[l], index[r] = index[r], index[l]
		}
		p := array[index[n/2]] // median of three as pivot
		if p < array[index[l]] {
			p = array[index[l]]
		} else if p > array[index[r]] {
			p = array[index[r]]
		}
		for {
			for l++; array[index[l]] < p; l++ { // skip smaller elements
			}
			for r--; array[index[r]] > p; r-- { // skip greater elements
			}
			if l >= r {
				if l > r {
					break
				}
				// skip all elements equal to the pivot
				// (C: while ((l < e) && (array[*++l] == p)); and the same
				// for r towards the start)
				for l < e {
					l++
					if array[index[l]] != p {
						break
					}
				}
				for r > 0 {
					r--
					if array[index[r]] != p {
						break
					}
				}
				break
			}
			index[l], index[r] = index[r], index[l]
		}
		m := n - l // number of elements right of the split
		n = 1 + r  // and left of it
		if n > m { // right section is smaller
			if m >= thInsert {
				idxQrec(index[l:l+m], array)
			}
			index = index[:n]
		} else { // left section is smaller
			if n >= thInsert {
				idxQrec(index[:n], array)
			}
			index = index[l : l+m]
			n = m
		}
		if n < thInsert {
			return
		}
	}
}

// idxQsort is a port of i2i_qsort (i2s_sort for int supports): it sorts
// the index array ascending by array[index[i]] with upstream's exact tie
// order, then reverses it if dir < 0.
func idxQsort(index []int32, dir int, array []tract.Supp) {
	n := len(index)
	if n < 2 {
		return
	}
	var k int
	if n < thInsert {
		k = n
	} else {
		idxQrec(index, array)
		k = thInsert - 1
	}
	l := 0 // find the smallest of the first k elements
	for r := 0; k-1 > 0; k-- {
		r++
		if array[index[r]] < array[index[l]] {
			l = r
		}
	}
	index[0], index[l] = index[l], index[0] // as a sentinel
	for r := 1; r < n; r++ {                // insertion sort
		x := index[r]
		t := array[x]
		l := r
		for l > 0 && array[index[l-1]] > t {
			index[l] = index[l-1]
			l--
		}
		index[l] = x
	}
	if dir < 0 {
		slices.Reverse(index)
	}
}
