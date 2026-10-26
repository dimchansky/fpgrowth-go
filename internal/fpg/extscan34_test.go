package fpg

import (
	"math"
	"math/bits"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Tests of the deferred long transactions of scanMaximal (issue #34).

// plainGuard makes scanMaximal defer nothing: the plain scan.
const plainGuard = math.MaxInt

// TestExtCheckMaximalMixedScaling guards the work of maximal checks on
// covers of one long and short transactions (the lp shapes of
// reference/bench) by item visits, not by time: one line of all n items
// (weight 1), then one short line per item; every one-item set is
// checked with smin 2.
//   - lp: one-item short lines, all maximal; the long line is deferred and
//     never visited (rest 1 < smin, no candidate);
//   - lp2: two-item short lines {i, i+1}: {i, i+1} has support 2, so no
//     one-item set is maximal; two candidates are searched in the long
//     line;
//   - lpw: one-item short lines of weights 1..20, the long line weight 1:
//     the bound is the same (rest 1 < smin).
//
// With the plain scan (deferral off) lp costs n·(n+1) visits.
func TestExtCheckMaximalMixedScaling(t *testing.T) {
	item := func(i int) string { return "i" + strconv.Itoa(i) }
	const n = 20000
	r := rand.New(rand.NewPCG(34, 2))
	for _, tc := range []struct {
		name     string
		short    int
		weighted bool
		maximal  bool
	}{
		{"lp", 1, false, true},
		{"lp2", 2, false, false},
		{"lpw", 1, true, true},
	} {
		all := make([]string, n)
		for i := range n {
			all[i] = item(i)
		}
		txs := []oracle.Transaction{{Items: all, Weight: 1}}
		for i := range n {
			var s []string
			for k := range tc.short {
				s = append(s, item((i+k)%n))
			}
			w := tract.Supp(1)
			if tc.weighted {
				w = 1 + tract.Supp(r.IntN(20))
			}
			txs = append(txs, oracle.Transaction{Items: s, Weight: w})
		}
		c, _ := newTestChecker(t, txs, report.TargetMaximal, 2, rowsAuto)
		c.mguard = 4 // the production guard, also with fpgforce
		for i := range tract.Item(len(c.freq)) {
			if got := c.checkPath([]tract.Item{i}, []tract.Item{i}, c.freq[i]); got != tc.maximal {
				t.Fatalf("%s: {%d}: maximal %v, want %v", tc.name, i, got, tc.maximal)
			}
		}
		t.Logf("%s: %d item visits, %+v", tc.name, c.work, c.scanStat)
		if c.work > 64*n {
			t.Fatalf("%s: %d item visits for %d checks", tc.name, c.work, n)
		}
		if c.scanStat.deferred < n {
			t.Fatalf("%s: %d checks with deferral, want %d", tc.name, c.scanStat.deferred, n)
		}
	}
}

// TestExtCheckMaximalHeavyFirst checks that a long transaction of weight
// at least smin is not deferred: it decides the check within |X| + 1
// items, and deferring it would visit the short ones first.
func TestExtCheckMaximalHeavyFirst(t *testing.T) {
	const m = 2000
	var txs []oracle.Transaction
	var all []string
	for i := range m {
		all = append(all, "i"+strconv.Itoa(i))
	}
	txs = append(txs, oracle.Transaction{Items: all, Weight: 3})
	for i := range m {
		for k := range 3 { // three short lines per item, 10 other items each
			s := []string{"i" + strconv.Itoa(i)}
			for j := range 10 {
				s = append(s, "s"+strconv.Itoa((i*30+k*10+j)%997))
			}
			txs = append(txs, oracle.Transaction{Items: s, Weight: 1})
		}
	}
	c, _ := newTestChecker(t, txs, report.TargetMaximal, 3, rowsAuto)
	c.mguard = 4
	for i := range tract.Item(len(c.freq)) {
		c.checkPath([]tract.Item{i}, []tract.Item{i}, c.freq[i])
	}
	if c.scanStat.deferred != 0 {
		t.Fatalf("%d checks deferred a transaction of weight >= smin", c.scanStat.deferred)
	}
}

// TestExtCheckMaximalCap checks the deferral cap: a light long line L
// that the plain scan visits first, then short lines whose total length
// exceeds |L|; the item y of L and of the first two short lines reaches
// smin 3 with L. The plain scan stops at the second short line; the
// deferral counts short lines up to |L| items, then counts L (cap) and
// stops. Its visits stay within twice the plain scan's.
func TestExtCheckMaximalCap(t *testing.T) {
	const ln = 200
	l := []string{"a", "y"}
	for i := range ln - 2 {
		l = append(l, "l"+strconv.Itoa(i))
	}
	txs := []oracle.Transaction{{Items: l, Weight: 1}}
	for k := range 10 {
		s := []string{"a"}
		if k < 2 {
			s = append(s, "y")
		}
		for len(s) < 40 {
			s = append(s, "s"+strconv.Itoa(k)+"_"+strconv.Itoa(len(s)))
		}
		txs = append(txs, oracle.Transaction{Items: s, Weight: 1})
	}
	work := func(guard int) (bool, int64, scanStats) {
		c, ib := newTestChecker(t, txs, report.TargetMaximal, 3, rowsAuto)
		c.mguard = guard
		a := tract.Item(-1)
		for i := range tract.Item(len(c.freq)) {
			if ib.ID(i) == "a" {
				a = i
			}
		}
		c.markSet([]tract.Item{a})
		n := c.bitCover([]tract.Item{a})
		if n != 11 {
			t.Fatalf("cover of {a}: %d transactions, want 11", n)
		}
		if t0 := c.nz[0]*64 + int32(bits.TrailingZeros64(c.cover[0])); c.txLen(t0) != ln {
			t.Fatalf("first cover transaction has %d items, want the long line", c.txLen(t0))
		}
		ok := c.scanMaximal(n)
		return ok, c.work, c.scanStat
	}
	okP, wP, _ := work(plainGuard)
	ok, w, st := work(4)
	if ok || okP {
		t.Fatalf("{a} maximal: plain %v, deferred %v, want false", okP, ok)
	}
	if st.capped != 1 || st.deferred != 0 {
		t.Fatalf("stats %+v, want one cap and no end phase", st)
	}
	if w > 2*wP {
		t.Fatalf("%d item visits, plain scan %d", w, wP)
	}
	t.Logf("item visits: plain %d, deferred %d", wP, w)
}

// TestExtCheckMaximalDeferProperty compares scanMaximal with a brute
// force on random bags (half of them lp-like: sparse short lines and
// every 7th line nearly full), random weights (0 and up to 2^20) and
// smin, for every deferral guard and lookup mode, so that the deferral,
// the cap, the candidates, the searches and the full counts all run. In
// the production lookup mode its item visits must stay within twice the
// plain scan's (the cap's bound). FPG_DEFER_ROUNDS sets the number of
// random bags. (Adapted from the design challenge of issue #34.)
func TestExtCheckMaximalDeferProperty(t *testing.T) {
	rounds := 3000
	if testing.Short() {
		rounds = 150
	}
	if s := os.Getenv("FPG_DEFER_ROUNDS"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("FPG_DEFER_ROUNDS: %v", err)
		}
		rounds = n
	}
	r := rand.New(rand.NewPCG(34, 1))
	var checks, defs, looks, caps int
	for round := range rounds {
		k := 3 + r.IntN(60)
		nt := 1 + r.IntN(25)
		lpLike := round%2 == 1
		var txs []oracle.Transaction
		var sets []map[string]bool
		for ti := range nt {
			p := r.Float64()
			if lpLike {
				p = 0.03
				if ti%7 == 0 {
					p = 0.95
				}
			}
			var items []string
			for i := range k {
				if r.Float64() < p {
					items = append(items, strconv.Itoa(i))
				}
			}
			if len(items) == 0 {
				items = []string{"0"}
			}
			w := tract.Supp(r.IntN(4))
			if r.IntN(10) == 0 {
				w = tract.Supp(r.IntN(1 << 20))
			}
			txs = append(txs, oracle.Transaction{Items: items, Weight: w})
			in := map[string]bool{}
			for _, it := range items {
				in[it] = true
			}
			sets = append(sets, in)
		}
		smin := tract.Supp(1 + r.IntN(6))
		if r.IntN(10) == 0 {
			smin = tract.Supp(1 + r.IntN(1<<20))
		}
		c, ib := newTestChecker(t, txs, report.TargetMaximal, smin, rowsAuto)
		ni := tract.Item(len(c.freq))
		if ni == 0 {
			continue
		}
		for range 20 {
			var set []tract.Item
			for i := range ni {
				if r.IntN(3) == 0 || (lpLike && r.IntN(int(ni)) == 0) {
					set = append(set, i)
				}
			}
			if lpLike && len(set) > 2 {
				set = set[:1+r.IntN(2)]
			}
			if len(set) == 0 {
				continue
			}
			want := true
			for j := range ni {
				if j2 := ib.ID(j); !slices.Contains(set, j) {
					var s tract.Supp
					for ti, in := range sets {
						if !in[j2] {
							continue
						}
						all := true
						for _, x := range set {
							all = all && in[ib.ID(x)]
						}
						if all {
							s += txs[ti].Weight
						}
					}
					if s >= smin {
						want = false
						break
					}
				}
			}
			run := func(guard int, mode lookupMode) (bool, int64) {
				c.mguard, c.lookup = guard, mode
				c.markSet(set)
				n := c.bitCover(set)
				if n == 0 {
					return true, 0
				}
				w0 := c.work
				d0, l0, k0 := c.scanStat.deferred, c.scanStat.lookups, c.scanStat.capped
				got := c.scanMaximal(n)
				checks++
				if c.scanStat.deferred > d0 {
					defs++
				}
				if c.scanStat.lookups > l0 {
					looks++
				}
				if c.scanStat.capped > k0 {
					caps++
				}
				if got != want {
					t.Fatalf("round %d set %v guard %d mode %d smin %d: maximal %v, want %v; txs %v",
						round, set, guard, mode, smin, got, want, txs)
				}
				for i, x := range c.cnt {
					if x != 0 {
						t.Fatalf("round %d: cnt[%d] = %d after the check", round, i, x)
					}
				}
				return got, c.work - w0
			}
			_, plain := run(plainGuard, lookupAuto)
			for _, guard := range []int{-1 << 30, 0, 1, 4} {
				for _, mode := range []lookupMode{lookupAuto, lookupAlways, lookupNever} {
					if _, w := run(guard, mode); mode != lookupAlways && w > 2*plain {
						t.Fatalf("round %d set %v guard %d mode %d: %d item visits, plain scan %d",
							round, set, guard, mode, w, plain)
					}
				}
			}
		}
	}
	t.Logf("%d checks: %d deferred, %d with searches, %d capped", checks, defs, looks, caps)
	if defs == 0 || looks == 0 || caps == 0 {
		t.Fatal("a path of the deferral never ran")
	}
}
