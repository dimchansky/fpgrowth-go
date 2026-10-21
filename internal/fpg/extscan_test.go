package fpg

// Tests of the closed scan's start transaction and exponential search
// (issue #20, docs/m5-investigation.md §4): covers that mix long and
// short transactions.

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// TestSeekItems checks the exponential search on item codes (keepSeek)
// against a linear search, including long lists and positions at and
// beyond the end.
func TestSeekItems(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for range 3000 {
		idx := make([]tract.Item, r.IntN(600))
		v := tract.Item(0)
		for k := range idx {
			v += 1 + tract.Item(r.IntN(3))
			idx[k] = v
		}
		i := r.IntN(len(idx) + 1)
		w := tract.Item(r.IntN(int(v) + 3))
		want := i
		for want < len(idx) && idx[want] < w {
			want++
		}
		if got := seek(idx, i, w); got != want {
			t.Fatalf("seek(%d items, %d, %d) = %d, want %d", len(idx), i, w, got, want)
		}
	}
}

// mixedTxs draws transactions over the items i0..i(k-1): long ones (128
// to 600 items, so that keepSeek runs at the production thresholds),
// short ones (1–3 items), suffix chains, and weights 0, 1 and 2.
func mixedTxs(r *rand.Rand, k int) []oracle.Transaction {
	name := func(i int) string { return "i" + strconv.Itoa(i) }
	var txs []oracle.Transaction
	w := func() tract.Supp {
		switch x := r.IntN(10); {
		case x == 0:
			return 0
		case x < 3:
			return 2
		}
		return 1
	}
	for range 1 + r.IntN(4) { // long lines: most items, with gaps
		var items []string
		for i := range k {
			if r.IntN(20) != 0 {
				items = append(items, name(i))
			}
		}
		txs = append(txs, oracle.Transaction{Items: items, Weight: w()})
	}
	if r.IntN(2) == 0 { // suffix chains
		step := 1 + r.IntN(k/4)
		for j := 0; j < k; j += step {
			var items []string
			for i := j; i < k; i++ {
				items = append(items, name(i))
			}
			txs = append(txs, oracle.Transaction{Items: items, Weight: w()})
		}
	}
	for range k + r.IntN(k) { // short lines
		items := []string{name(r.IntN(k))}
		for range r.IntN(3) {
			if it := name(r.IntN(k)); !slices.Contains(items, it) {
				items = append(items, it)
			}
		}
		txs = append(txs, oracle.Transaction{Items: items, Weight: w()})
	}
	if r.IntN(3) == 0 {
		r.Shuffle(len(txs), func(a, b int) { txs[a], txs[b] = txs[b], txs[a] })
	}
	return txs
}

// TestExtCheckMixedCover cross-checks closed checks on covers that mix
// long and short transactions with a brute force over the source
// transactions (positive weights), for 1–3-item sets, in every row form
// and with the production thresholds, every stage b forced, and every
// intersection by exponential search; the scan (scanClosed) and the
// vertical test (vertClosed) separately, and check. It also requires
// that each new path of the scan ran at the production thresholds.
func TestExtCheckMixedCover(t *testing.T) {
	params := []scanParams{
		scanDefaults,
		{gallopMin: 128, gallopRatio: 16, guard: 0},
		{gallopMin: 2, gallopRatio: 1, guard: 4},
		{gallopMin: 1, gallopRatio: 0, guard: 0},
	}
	var used [4]scanStats
	for seed := range uint64(24) {
		r := rand.New(rand.NewPCG(seed, 20))
		k := 150 + r.IntN(250)
		txs := mixedTxs(r, k)
		in := func(tx oracle.Transaction, names []string) bool {
			for _, it := range names {
				if !slices.Contains(tx.Items, it) {
					return false
				}
			}
			return true
		}
		for _, rows := range []rowMode{rowsAuto, rowsSparse, rowsMixed} {
			c, ib := newTestChecker(t, txs, report.TargetClosed, 1, rows)
			nItems := tract.Item(len(c.freq))
			for pi, p := range params {
				c.scanParams = p
				c.scanStat = scanStats{}
				for q := range 300 {
					set := []tract.Item{tract.Item(r.IntN(int(nItems)))}
					for range q % 3 {
						if it := tract.Item(r.IntN(int(nItems))); !slices.Contains(set, it) {
							set = append(set, it)
						}
					}
					names := make([]string, len(set))
					for i, it := range set {
						names[i] = ib.ID(it)
					}
					var supp tract.Supp
					var cover []oracle.Transaction
					for _, tx := range txs {
						if tx.Weight > 0 && in(tx, names) {
							supp += tx.Weight
							cover = append(cover, tx)
						}
					}
					if supp == 0 {
						continue // never checked
					}
					want := true
					for j := range nItems {
						if !slices.Contains(set, j) && !slices.ContainsFunc(cover, func(tx oracle.Transaction) bool {
							return !slices.Contains(tx.Items, ib.ID(j))
						}) {
							want = false
							break
						}
					}
					got := c.check(set, supp)
					c.markSet(set)
					n := c.bitCover(set)
					h, v := c.scanClosed(n), c.vertClosed(supp)
					if got != want || h != want || v != want {
						t.Fatalf("seed %d rows %d params %+v set %v (supp %d): check %v, scan %v, vertical %v, want %v",
							seed, rows, p, names, supp, got, h, v, want)
					}
				}
				st := &used[pi]
				st.moved += c.scanStat.moved
				st.stageB += c.scanStat.stageB
				st.gallops += c.scanStat.gallops
			}
		}
	}
	if u := used[0]; u.moved == 0 || u.stageB == 0 || u.gallops == 0 {
		t.Fatalf("production thresholds: not every path ran: %+v", u)
	}
	t.Logf("paths per setting: %+v", used)
}

// TestExtCheckZeroWeightSet: a transaction of weight 0 that equals the
// checked set X, first in bag order and the shortest of all, is not in
// the cover, so it must not end stage b or give the candidates: X is not
// closed (b is in every transaction of positive weight with a).
func TestExtCheckZeroWeightSet(t *testing.T) {
	long := []string{"a", "b"}
	for i := range 300 {
		long = append(long, "l"+strconv.Itoa(i))
	}
	txs := []oracle.Transaction{
		{Items: []string{"a"}, Weight: 0},
		{Items: long, Weight: 1},
		{Items: []string{"a", "b"}, Weight: 1},
		{Items: []string{"a", "b", "c"}, Weight: 1},
	}
	for range 300 {
		txs = append(txs, oracle.Transaction{Items: []string{"z"}, Weight: 1}) // the scan is cheaper
	}
	for _, rows := range []rowMode{rowsAuto, rowsSparse, rowsMixed} {
		for _, p := range []scanParams{scanDefaults, {gallopMin: 1, gallopRatio: 0, guard: 0}} {
			c, ib := newTestChecker(t, txs, report.TargetClosed, 1, rows)
			c.scanParams = p
			a, _ := ib.Code("a")
			c.markSet([]tract.Item{a})
			n := c.bitCover([]tract.Item{a})
			if n != 3 {
				t.Fatalf("cover of {a}: %d transactions, want 3", n)
			}
			if c.scanClosed(n) {
				t.Fatalf("rows %d params %+v: {a} reported closed, b is in its whole cover", rows, p)
			}
			if c.check([]tract.Item{a}, 3) {
				t.Fatalf("rows %d params %+v: check reports {a} closed", rows, p)
			}
		}
	}
}

// workChecker checks every item of a closed checker built from txs as a
// one-item set (support from the checker's frequencies) and fails if a
// check's verdict differs from want or if the candidate buffer grew
// beyond 64 items (a long transaction was copied).
func workChecker(t *testing.T, name string, txs []oracle.Transaction, want func(string) bool) *extChecker {
	t.Helper()
	c, ib := newTestChecker(t, txs, report.TargetClosed, 1, rowsAuto)
	for i := range tract.Item(len(c.freq)) {
		if got := c.checkPath([]tract.Item{i}, []tract.Item{i}, c.freq[i]); got != want(ib.ID(i)) {
			t.Fatalf("%s: {%s}: closed %v, want %v", name, ib.ID(i), got, !got)
		}
		if cap(c.cand) > 64 {
			t.Fatalf("%s: {%s}: candidate buffer grew to %d (a long transaction copied)", name, ib.ID(i), cap(c.cand))
		}
	}
	return c
}

// TestExtCheckMixedScaling guards the work of closed checks on covers of
// long and short transactions (issue #20, the lp shapes of
// reference/bench) by the candidate buffer and the path counters, not
// by time:
//   - lp: one line of all n items, then n one-item lines; the long line
//     is the first cover transaction of every item, and of 62 items it
//     shares the cover word with the short line (stage b);
//   - lp2: two-item short lines: a candidate survives the short line and
//     is searched in the long one (keepSeek);
//   - lpc: suffix chains every 64 items in first-appearance order (as
//     with -q0): a chain is the first transaction of every cover word,
//     the one-item line is found only by stage b;
//   - trap: the challenge's adv5 (H1): the first cover transaction holds
//     a covering item among its probes, the shortest one does not; the
//     first one must still be probed (else n items are copied per check).
func TestExtCheckMixedScaling(t *testing.T) {
	item := func(i int) string { return "i" + strconv.Itoa(i) }
	all := func(from, n int) []string {
		var s []string
		for i := from; i < n; i++ {
			s = append(s, item(i))
		}
		return s
	}
	one := func(items ...string) oracle.Transaction { return oracle.Transaction{Items: items, Weight: 1} }
	closed := func(string) bool { return true }

	const n = 20000
	lp := []oracle.Transaction{one(all(0, n)...)}
	for i := range n {
		lp = append(lp, one(item(i)))
	}
	if c := workChecker(t, "lp", lp, closed); c.scanStat.stageB == 0 {
		t.Fatalf("lp: stage b never ran: %+v", c.scanStat)
	}

	lp2 := []oracle.Transaction{one(all(0, n)...)}
	for i := range n {
		lp2 = append(lp2, one(item(i), item((i+1)%n)))
	}
	if c := workChecker(t, "lp2", lp2, closed); c.scanStat.gallops < n/2 {
		t.Fatalf("lp2: %d intersections by exponential search, want at least %d", c.scanStat.gallops, n/2)
	}

	const m = 4000
	var lpc []oracle.Transaction
	for j := 0; j < m; j += 64 {
		lpc = append(lpc, one(all(j, m)...))
	}
	for i := range m {
		lpc = append(lpc, one(item(i)))
	}
	if c := workChecker(t, "lpc", lpc, closed); c.scanStat.stageB < m/2 {
		t.Fatalf("lpc: stage b ran %d times, want at least %d", c.scanStat.stageB, m/2)
	}

	trap := []oracle.Transaction{
		one("m1", "m2", "m3", "r1", "r2", "r3", "r4"),
		one(append([]string{"m1", "m2", "m3"}, all(0, m)...)...),
		one(append([]string{"r1", "r2", "r3", "r4"}, all(0, m-2)...)...),
		one(item(m - 1)),
		one(item(m - 2)),
	}
	for k := range 40000 {
		trap = append(trap, one("x"+strconv.Itoa(k%7)))
	}
	// brute force: an item's closure is the intersection of its cover
	closure := map[string]map[string]bool{}
	for _, tx := range trap {
		in := map[string]bool{}
		for _, j := range tx.Items {
			in[j] = true
		}
		for _, it := range tx.Items {
			cl, ok := closure[it]
			if !ok {
				closure[it] = maps.Clone(in)
				continue
			}
			for j := range cl {
				if !in[j] {
					delete(cl, j)
				}
			}
		}
	}
	trapWant := func(id string) bool { return len(closure[id]) == 1 }
	workChecker(t, "trap", trap, trapWant)
}

// TestExtCheckTrapProbe is the trap of TestExtCheckMixedScaling at the
// scan level, for the ordering and fallbacks of the probes: a check of
// {a_i} (0 < i < n−2) is decided by the probes of the first cover
// transaction, before any candidate is copied.
func TestExtCheckTrapProbe(t *testing.T) {
	const m = 300
	one := func(items ...string) oracle.Transaction { return oracle.Transaction{Items: items, Weight: 1} }
	var as []string
	for i := range m {
		as = append(as, fmt.Sprintf("a%d", i))
	}
	txs := []oracle.Transaction{
		one("m1", "m2", "m3", "r1", "r2", "r3", "r4"),
		one(append([]string{"m1", "m2", "m3"}, as...)...),
		one(append([]string{"r1", "r2", "r3", "r4"}, as[:m-2]...)...),
		one(as[m-1]),
		one(as[m-2]),
	}
	c, ib := newTestChecker(t, txs, report.TargetClosed, 1, rowsAuto)
	for i := 1; i < m-2; i++ {
		a, _ := ib.Code(as[i])
		c.markSet([]tract.Item{a})
		c.cand = c.cand[:0]
		if c.scanClosed(c.bitCover([]tract.Item{a})) {
			t.Fatalf("{%s} reported closed, a0 is in its cover", as[i])
		}
		if len(c.cand) != 0 || c.scanStat.moved != 0 {
			t.Fatalf("{%s}: candidates copied (%d) or the start moved (%d) before the probes of t0 decided",
				as[i], len(c.cand), c.scanStat.moved)
		}
	}
}
