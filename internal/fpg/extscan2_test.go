package fpg

// Regression tests of the PR #35 review (issue #20): the exponential
// search's decision and the transactions the full scan must intersect.

import (
	"slices"
	"strconv"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// TestExtCheckClusteredCandidates (review P1): candidates at the start
// of a long transaction are intersected by the merge, which stops at the
// last candidate, not by the exponential search; candidates spread over
// the long transaction are searched.
func TestExtCheckClusteredCandidates(t *testing.T) {
	one := func(items ...string) oracle.Transaction { return oracle.Transaction{Items: items, Weight: 1} }
	var low, high []string
	for i := range 20 {
		low = append(low, "c"+strconv.Itoa(i))
	}
	for i := range 1000 {
		high = append(high, "o"+strconv.Itoa(i))
	}
	with := func(head []string, rest ...[]string) []string {
		s := slices.Clone(head)
		for _, r := range rest {
			s = append(s, r...)
		}
		return s
	}
	spread, spread2 := []string{"h"}, []string{"h"}
	for i := 0; i < 1000; i += 50 {
		spread, spread2 = append(spread, high[i]), append(spread2, high[i+25])
	}
	fill := make([]oracle.Transaction, 3000) // the scan is cheaper than the vertical test (decide)
	for i := range fill {
		fill[i] = one("z" + strconv.Itoa(i)) // distinct: not merged into one weighted transaction
	}
	// first appearance gives h, the c's, then the o's ascending codes
	clustered := append([]oracle.Transaction{
		one(with([]string{"h"}, low)...),       // t0 = t*: the candidates are the c's
		one(with([]string{"h"}, low, high)...), // the c's are its first items
		one(with([]string{"h"}, high)...),      // empties the candidates: {h} is closed
	}, fill...)
	spreadTxs := append([]oracle.Transaction{
		one(with([]string{"h"}, high)...), // t0, searched
		one(spread...),                    // t*: the candidates are spread over the o's
		one(spread2...),                   // empties the candidates
	}, fill...)
	for _, tc := range []struct {
		name    string
		txs     []oracle.Transaction
		gallops bool
	}{{"clustered", clustered, false}, {"spread", spreadTxs, true}} {
		for _, rm := range []rowMode{rowsAuto, rowsSparse, rowsMixed} {
			c, ib := newTestChecker(t, tc.txs, report.TargetClosed, 1, rm)
			c.scanParams = scanParams{gallopMin: 128, gallopRatio: 16, guard: 4} // production
			h, _ := ib.Code("h")
			if !c.check([]tract.Item{h}, c.freq[h]) {
				t.Fatalf("%s rows %d: {h} reported not closed", tc.name, rm)
			}
			if c.scanStat.stageB == 0 {
				t.Fatalf("%s rows %d: setup: the check did not scan (no stage b)", tc.name, rm)
			}
			if got := c.scanStat.gallops > 0; got != tc.gallops {
				t.Fatalf("%s rows %d: %d intersections by exponential search, want some: %v",
					tc.name, rm, c.scanStat.gallops, tc.gallops)
			}
		}
	}
}

// TestExtCheckFirstOfWord (review C20-1): a closed check whose cover
// spans more than 16 non-zero words and whose start moved away from t0.
// Every cover transaction lacks one "almost" item a_i, so each of them,
// in particular the first one of every cover word that is not a spread
// word, must be intersected: skipping any leaves its a_i a candidate
// and reports the closed {h} as not closed.
func TestExtCheckFirstOfWord(t *testing.T) {
	const m = 1100 // cover transactions: 18 words
	name := func(p string, i int) string { return p + strconv.Itoa(i) }
	var txs []oracle.Transaction
	for i := range m {
		items := []string{"h"}
		for j := range m {
			if j != i {
				items = append(items, name("a", j))
			}
		}
		if i == 0 { // t0 (lexicographically first: it lacks a0, the highest code): longer, the start moves
			for k := range 40 {
				items = append(items, name("p", i*40+k))
			}
		}
		txs = append(txs, oracle.Transaction{Items: items, Weight: 1})
	}
	for range 20000 {
		txs = append(txs, oracle.Transaction{Items: []string{"z"}, Weight: 1})
	}
	rows := []rowMode{rowsAuto, rowsSparse, rowsMixed}
	if testing.Short() || raceEnabled {
		rows = rows[:1]
	}
	for _, rm := range rows {
		c, ib := newTestChecker(t, txs, report.TargetClosed, 1, rm)
		h, _ := ib.Code("h")
		for _, p := range []scanParams{scanDefaults(), {gallopMin: 2, gallopRatio: 1, guard: 0}} {
			c.scanParams, c.scanStat = p, scanStats{}
			c.markSet([]tract.Item{h})
			n := c.bitCover([]tract.Item{h})
			if len(c.nz) < 16 {
				t.Fatalf("setup: the cover spans %d words, want at least 16", len(c.nz))
			}
			if !c.scanClosed(n) {
				t.Fatalf("rows %d params %+v: {h} reported not closed", rm, p)
			}
			if c.scanStat.moved == 0 {
				t.Fatalf("setup: the start did not move from t0")
			}
			if !c.check([]tract.Item{h}, c.freq[h]) {
				t.Fatalf("rows %d params %+v: check reports {h} not closed", rm, p)
			}
		}
	}
}
