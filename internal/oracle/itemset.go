package oracle

import (
	"slices"
	"strconv"
	"strings"
)

// ItemSet is an item set in canonical form: item names sorted in ascending
// byte order (Go string order) without duplicates. Build one with
// NewItemSet; all results of this package are canonical.
type ItemSet []string

// NewItemSet returns the canonical item set of the given names: a sorted,
// duplicate-free copy. It never returns nil.
func NewItemSet(items ...string) ItemSet {
	s := make(ItemSet, len(items))
	copy(s, items)
	slices.Sort(s)
	return slices.Compact(s)
}

// Key returns a canonical, injective string key of the set: every item
// quoted with strconv.Quote, separated by single spaces. The empty set has
// the empty key. Two canonical sets are equal if and only if their keys are.
func (s ItemSet) Key() string {
	var b strings.Builder
	for i, item := range s {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strconv.Quote(item))
	}
	return b.String()
}

// String formats the set as "{" + Key() + "}", e.g. {"a" "b"}.
func (s ItemSet) String() string {
	return "{" + s.Key() + "}"
}

// Equal reports whether two canonical sets contain the same items.
func (s ItemSet) Equal(t ItemSet) bool {
	return slices.Equal(s, t)
}

// Contains reports whether the canonical set s contains item.
func (s ItemSet) Contains(item string) bool {
	_, found := slices.BinarySearch(s, item)
	return found
}

// Compare orders canonical sets canonically: by size first, then
// lexicographically by item (byte order). It returns -1, 0 or +1.
func (s ItemSet) Compare(t ItemSet) int {
	if len(s) != len(t) {
		if len(s) < len(t) {
			return -1
		}
		return +1
	}
	return slices.Compare(s, t)
}
