package tract

// This file ports the sorting routines of util/src/arrays.c that decide
// the order of items in ib_recode (via idm_sort). Upstream's quicksort is
// not stable: the relative order of items with equal sort keys depends on
// the exact sequence of comparisons and exchanges, so the routines below
// follow the C code operation by operation. Do not replace them with
// package sort or slices.

// thInsert is TH_INSERT (arrays.c): sections with fewer elements are left
// to the final insertion sort pass of ptrQsort.
const thInsert = 2

// ptrQsort sorts a ascending with respect to cmp. Port of ptr_qsort
// (arrays.c) for dir = +1, the only direction idm_sort uses; the elements
// play the role of upstream's pointers. cmp must return a negative value,
// zero or a positive value like upstream's CMPFN.
func ptrQsort[E any](a []E, cmp func(x, y E) int) {
	n := len(a)
	if n < 2 { // do not sort less than two elements
		return
	}
	var k int // size of the first section to search for a minimum
	if n < thInsert {
		k = n
	} else {
		ptrQrec(a, cmp)
		k = thInsert - 1
	}
	// Find the smallest of the first k elements and swap it to the
	// front, where it serves as the sentinel of the insertion sort.
	l := 0
	for r := 0; ; {
		k--
		if k <= 0 {
			break
		}
		r++
		if cmp(a[r], a[l]) < 0 {
			l = r
		}
	}
	a[0], a[l] = a[l], a[0]
	for r := 1; r < n; r++ { // standard insertion sort
		t := a[r]
		l := r
		for {
			l--
			if cmp(a[l], t) <= 0 {
				break
			}
			a[l+1] = a[l]
		}
		a[l+1] = t
	}
}

// ptrQrec is the recursive part of ptrQsort. Port of ptr_qrec (arrays.c):
// median-of-three pivot, Hoare-style partitioning that skips elements
// equal to the pivot at the crossing point, recursion into the smaller
// section and iteration on the larger one. len(a) must be at least 2.
func ptrQrec[E any](a []E, cmp func(x, y E) int) {
	n := len(a)
	for {
		l, r := 0, n-1 // start at the left and right boundary
		e := r
		if cmp(a[l], a[r]) > 0 { // bring first and last element
			a[l], a[r] = a[r], a[l] // into proper order
		}
		p := a[n/2] // middle element as pivot
		if cmp(p, a[l]) < 0 {
			p = a[l]
		} else if cmp(p, a[r]) > 0 {
			p = a[r]
		}
		for { // split and exchange loop
			for { // skip left elements smaller than the pivot
				l++
				if cmp(a[l], p) >= 0 {
					break
				}
			}
			for { // skip right elements greater than the pivot
				r--
				if cmp(a[r], p) <= 0 {
					break
				}
			}
			if l >= r { // if at most one element left
				if l > r {
					break
				}
				for l < e { // skip all elements equal to the pivot
					l++
					if cmp(a[l], p) != 0 {
						break
					}
				}
				for r > 0 {
					r--
					if cmp(a[r], p) != 0 {
						break
					}
				}
				break
			}
			a[l], a[r] = a[r], a[l]
		}
		m := n - l // number of elements right of the split
		n = 1 + r  // number of elements left of the split
		if n > m { // right section is smaller
			if m >= thInsert {
				ptrQrec(a[l:l+m], cmp)
			}
		} else { // left section is smaller
			if n >= thInsert {
				ptrQrec(a[:n], cmp)
			}
			a = a[l : l+m] // continue with the right section
			n = m
		}
		if n < thInsert {
			return
		}
	}
}

// itemKey holds the sort keys ib_recode's comparators read from ITEMDATA.
type itemKey struct {
	frq    Supp // frequency
	xfq    Supp // size-weighted frequency
	isNone bool // appearance is AppearNone (item is to be removed)
}

// itemComparator returns the comparator ib_recode selects for the item
// order mode order (the -q value; upstream calls ib_recode with
// dir = -order): nocmp, asccmp, descmp, asccmpx or descmpx (tract.c).
// Elements are item identifiers (indices into keys). Every comparator
// puts items to be removed after all others and has no secondary key:
// ties compare equal. The mode is tested directly instead of negating
// it, which would overflow for math.MinInt.
func itemComparator(keys []itemKey, order int) func(x, y int32) int {
	none := func(a, b *itemKey) (int, bool) {
		if a.isNone {
			if b.isNone {
				return 0, true
			}
			return 1, true
		}
		if b.isNone {
			return -1, true
		}
		return 0, false
	}
	switch {
	case order < -1: // dir > 1: asccmpx
		return func(x, y int32) int {
			a, b := &keys[x], &keys[y]
			if c, ok := none(a, b); ok {
				return c
			}
			return compareSupp(a.xfq, b.xfq)
		}
	case order == -1: // dir == 1: asccmp
		return func(x, y int32) int {
			a, b := &keys[x], &keys[y]
			if c, ok := none(a, b); ok {
				return c
			}
			return compareSupp(a.frq, b.frq)
		}
	case order == 0: // dir == 0: nocmp
		return func(x, y int32) int {
			a, b := &keys[x], &keys[y]
			if c, ok := none(a, b); ok {
				return c
			}
			return compareSupp(Supp(x), Supp(y))
		}
	case order == 1: // dir == -1: descmp
		return func(x, y int32) int {
			a, b := &keys[x], &keys[y]
			if c, ok := none(a, b); ok {
				return c
			}
			return compareSupp(b.frq, a.frq)
		}
	default: // dir < -1: descmpx
		return func(x, y int32) int {
			a, b := &keys[x], &keys[y]
			if c, ok := none(a, b); ok {
				return c
			}
			return compareSupp(b.xfq, a.xfq)
		}
	}
}

// compareSupp returns the sign of a-b without computing the difference.
func compareSupp(a, b Supp) int {
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	default:
		return 0
	}
}
