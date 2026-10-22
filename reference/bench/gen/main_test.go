package main

import "testing"

// TestListStatsTrap checks the -list accounting on the probe trap, whose
// filler items are numbered beyond Items (it used to panic): N items a_i,
// 7 items m and r, and 7 filler items.
func TestListStatsTrap(t *testing.T) {
	s := benchSpec{Name: "trap", Transactions: 25, Items: 10, Trap: 20}
	st, err := listStats(s)
	if err != nil {
		t.Fatal(err)
	}
	want := datasetStats{used: s.Items + 14, sum: 7 + (3 + 10) + (4 + 8) + 1 + 1 + 20, minLen: 1, maxLen: 13}
	if st != want {
		t.Errorf("listStats = %+v, want %+v", st, want)
	}
}
