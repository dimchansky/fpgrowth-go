package fpg

import (
	"errors"
	"fmt"
	"maps"
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

// m5Case is a random closed/maximal case with a maximum size (deviation
// M5, docs/m5-investigation.md).
type m5Case struct {
	propCase
	sel []string // item selection, nil for none
}

// genM5Case draws a dense or sparse dataset and options with a maximum
// size, weighted towards small limits.
func genM5Case(seed uint64) (m5Case, error) {
	r := rand.New(rand.NewPCG(seed, 0x5eed0f35))
	dense := r.IntN(2) == 0
	gc := oracle.GenConfig{
		Seed:            seed ^ 0xa5a5,
		NumTransactions: 1 + r.IntN(40),
		NumItems:        1 + r.IntN(12),
		Skew:            r.IntN(3),
		Patterns:        r.IntN(3),
		PatternProb:     r.Float64() * 0.7,
		DupItemProb:     0.05,
		DupTxProb:       r.Float64() * 0.3,
		EmptyProb:       r.Float64() * 0.1,
		Weights:         oracle.WeightDist(r.IntN(4)),
		Names:           oracle.NamesLetters,
	}
	if seed%10 == 9 {
		gc.NumItems = 17 + r.IntN(4) // more than 16 items
	}
	if dense {
		gc.MinLen = gc.NumItems / 2
		gc.MaxLen = gc.NumItems
	} else {
		gc.MinLen = r.IntN(3)
		gc.MaxLen = gc.MinLen + 1 + r.IntN(5)
	}
	gc.PatternLen = 1 + r.IntN(min(5, gc.NumItems))
	txs, err := oracle.Generate(gc)
	if err != nil {
		return m5Case{}, err
	}
	c := m5Case{propCase: propCase{seed: seed, txs: txs, smax: 100}}
	switch r.IntN(4) {
	case 0:
		c.smin = float64(r.IntN(70)) // percentage
	default:
		c.smin = float64(-(1 + r.IntN(6)))
	}
	c.zmin = 1
	if r.IntN(4) == 0 {
		c.zmin = r.IntN(4)
	}
	c.zmax = c.zmin + r.IntN(4)
	if r.IntN(6) == 0 {
		c.zmax = r.IntN(8) // also below zmin and above most sizes
	}
	if r.IntN(6) == 0 {
		c.smax = float64(-(2 + r.IntN(10)))
	}
	c.noPerf = r.IntN(3) == 0
	c.noTail = r.IntN(3) == 0
	c.sort = []int{2, 2, 1, 0, -1, -2}[r.IntN(6)]
	if r.IntN(8) == 0 {
		c.border = []float64{float64(-(1 + r.IntN(3))), float64(-(1 + r.IntN(4)))}
	}
	if r.IntN(4) == 0 { // select a random subset of the items
		c.sel = []string{}
		seen := map[string]bool{}
		for _, tx := range txs {
			for _, it := range tx.Items {
				if !seen[it] {
					seen[it] = true
					if r.IntN(3) != 0 {
						c.sel = append(c.sel, it)
					}
				}
			}
		}
	}
	return c, nil
}

// cloMaxTargets are the targets with the closed/maximal repository.
var cloMaxTargets = targets[1:3]

// m5Source computes supports from a case's source transactions (item
// names, with the item selection applied), independently of the
// checker's storage of the bag.
type m5Source struct {
	txs   []oracle.Transaction
	sel   []string
	sink  *collectSink // its item base maps the codes (set by run)
	ready bool
	sets  []map[string]bool // the items of each transaction
	items []string          // every item (sorted)
}

// newSource returns the source of txs and sel for the run with sink.
func newSource(txs []oracle.Transaction, sel []string, sink *collectSink) *m5Source {
	return &m5Source{txs: txs, sel: sel, sink: sink}
}

func (s *m5Source) init() {
	if s.ready {
		return
	}
	s.ready = true
	var keep map[string]bool
	if s.sel != nil {
		keep = map[string]bool{}
		for _, it := range s.sel {
			keep[it] = true
		}
	}
	all := map[string]bool{}
	for _, tx := range s.txs {
		set := map[string]bool{}
		for _, it := range tx.Items {
			if keep == nil || keep[it] {
				set[it], all[it] = true, true
			}
		}
		s.sets = append(s.sets, set)
	}
	s.items = slices.Sorted(maps.Keys(all))
}

// names returns the item names of the codes of set.
func (s *m5Source) names(set []tract.Item) []string {
	out := make([]string, len(set))
	for k, it := range set {
		out[k] = s.sink.ib.ID(it)
	}
	return out
}

// supp returns the weight of the source transactions that contain all
// of names.
func (s *m5Source) supp(names []string) tract.Supp {
	s.init()
	var sum tract.Supp
	for t, set := range s.sets {
		ok := true
		for _, it := range names {
			if !set[it] {
				ok = false
				break
			}
		}
		if ok {
			sum += s.txs[t].Weight
		}
	}
	return sum
}

// m5Check makes a replacement extension check for one run from the
// run's checker, reporter and source transactions.
type m5Check func(*extChecker, *report.Reporter, *m5Source) report.CloMaxCheck

// withSource returns the hooks and the sink of a run of txs and sel whose
// extension check is check (nil: the miner's own).
func withSource(check m5Check, txs []oracle.Transaction, sel []string, rows rowMode) (testHooks, *collectSink) {
	sink := &collectSink{}
	hooks := testHooks{rows: rows}
	if check != nil {
		src := newSource(txs, sel, sink)
		hooks.cloMaxCheck = func(chk *extChecker, rep *report.Reporter) report.CloMaxCheck {
			return check(chk, rep, src)
		}
	}
	return hooks, sink
}

// checkM5Case compares the closed and maximal sets of every variant with
// the oracle; check, if not nil, replaces the extension check. It returns
// the number of comparisons and the names of the differing runs.
func checkM5Case(t *testing.T, c m5Case, check m5Check) (int, []string) {
	t.Helper()
	db, err := oracle.New(c.txs, oracle.Options{Selection: c.sel})
	if err != nil {
		t.Fatalf("seed %d: oracle: %v", c.seed, err)
	}
	total := db.Total()
	var border []tract.Supp
	if c.border != nil {
		border = oracle.AbsBorder(c.border, c.zmin, total)
	}
	n := 0
	var bad []string
	for _, tg := range cloMaxTargets {
		p := oracle.NewSetParams(tg.oracle, oracle.AbsMinSupp(c.smin, total))
		p.MaxSupp = oracle.AbsMaxSupp(c.smax, total)
		p.MinSize = c.zmin
		if c.zmax < MaxSize {
			p.MaxSize = c.zmax
		}
		p.Border = border
		want, err := db.Mine(p)
		if err != nil {
			t.Fatalf("seed %d: oracle mine: %v", c.seed, err)
		}
		for _, v := range variants {
			cfg := c.config(v, tg.target)
			// every form of the bit sets
			hooks, sink := withSource(check, c.txs, c.sel, rowMode(c.seed%3))
			res, err := run(scanOf(c.txs), cfg, runOpts{border: border, sel: c.sel, hooks: hooks, sink: sink})
			n++
			name := fmt.Sprintf("%s/%s %s sel=%v border=%v", tg.name, v.name, describe(cfg), c.sel, c.border)
			if errors.Is(err, ErrNoItems) {
				if len(want) > 0 && (len(want) > 1 || len(want[0].Items) > 0) {
					bad = append(bad, name+": ErrNoItems")
				}
				continue
			}
			if err != nil {
				t.Fatalf("seed %d %s: %v", c.seed, name, err)
			}
			if d := oracle.DiffSets(want, res.sink.sets, oracle.DiffOptions{IgnoreEval: true}); d != "" {
				bad = append(bad, name+":\n"+d)
			}
		}
	}
	return n, bad
}

// TestExtCheckOracle compares closed and maximal sets with a maximum size
// of every variant with the oracle (deviation M5). FPG_M5_CASES sets the
// number of datasets (default 1500, 200 with -short).
func TestExtCheckOracle(t *testing.T) {
	t.Parallel()
	cases := 1500
	if testing.Short() {
		cases = 200
	}
	if s := os.Getenv("FPG_M5_CASES"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("FPG_M5_CASES: %v", err)
		}
		cases = n
	}
	runs := 0
	for seed := range uint64(cases) {
		c, err := genM5Case(seed)
		if err != nil {
			t.Fatal(err)
		}
		n, bad := checkM5Case(t, c, nil)
		runs += n
		for _, b := range bad {
			t.Errorf("seed %d %s", seed, b)
		}
		if t.Failed() {
			break
		}
	}
	t.Logf("%d datasets, %d closed/maximal oracle comparisons", cases, runs)
}

// localCheck is the negative control of the extension check: it tests
// only the items below the smallest item of the current prefix, i.e. the
// items a conditional FP-tree of the set can contain, and relies on the
// closed/maximal repository for every other item. This is what a miner
// that collects perfect extensions (conditional supports) at the size
// limit decides by itself. It is not complete.
func localCheck(c *extChecker, rep *report.Reporter, src *m5Source) report.CloMaxCheck {
	return func(set []tract.Item, supp tract.Supp) bool {
		lo := tract.Item(len(c.freq))
		if p := rep.Items(); len(p) > 0 {
			lo = slices.Min(p)
		}
		names := src.names(set)
		for j := range lo {
			if slices.Contains(set, j) {
				continue
			}
			s := src.supp(append(slices.Clone(names), src.sink.ib.ID(j)))
			if (!c.maximal && s >= supp) || (c.maximal && s >= c.smin) {
				return false
			}
		}
		return true
	}
}

// bruteCheck decides closedness/maximality by brute force from the
// source transactions, over every item of the source (after the
// selection), and compares it with extChecker.check, checkPath and both
// paths of a check (the scan and the vertical tests).
func bruteCheck(c *extChecker, rep *report.Reporter, src *m5Source) report.CloMaxCheck {
	return func(set []tract.Item, supp tract.Supp) bool {
		got := c.check(set, supp)
		if p := c.checkPath(rep.Items(), set, supp); p != got {
			panic(fmt.Sprintf("checkPath(%v, %v, %d) = %v, check %v", rep.Items(), set, supp, p, got))
		}
		if len(set) > 0 { // both decisions
			c.markSet(set)
			c.bitCover(set)
			h, v := c.scanClosed(), c.vertClosed(supp)
			if c.maximal {
				h, v = c.scanMaximal(), c.vertMaximal()
			}
			if h != got || v != got {
				panic(fmt.Sprintf("check(%v, %d): %v, scan %v, vertical %v", set, supp, got, h, v))
			}
		}
		names := src.names(set)
		if s := src.supp(names); s != supp {
			panic(fmt.Sprintf("support of %v: reporter %d, brute force %d", names, supp, s))
		}
		want := true
		for _, j := range src.items {
			if slices.Contains(names, j) {
				continue
			}
			s := src.supp(append(slices.Clone(names), j))
			if (!c.maximal && s >= supp) || (c.maximal && s >= c.smin) {
				want = false
				break
			}
		}
		if got != want {
			panic(fmt.Sprintf("extChecker.check(%v, %d) = %v, brute force %v", names, supp, got, want))
		}
		return got
	}
}

// TestExtCheckBrute cross-checks extChecker.check (every form of the bit
// sets, both the scan and the vertical path) on every set it is asked
// about with a brute-force computation from the source transactions.
func TestExtCheckBrute(t *testing.T) {
	t.Parallel()
	for seed := range uint64(300) {
		c, err := genM5Case(seed)
		if err != nil {
			t.Fatal(err)
		}
		_, bad := checkM5Case(t, c, bruteCheck)
		for _, b := range bad {
			t.Errorf("seed %d %s", seed, b)
		}
	}
}

// m5Regressions are cases where the extension that disproves a reported
// set lies outside the set's conditional FP-tree (an item above the set
// in the search order), so that a local check fails.
var m5Regressions = []struct {
	name     string
	data     string // weighted transactions
	opts     string
	localBad bool // whether localCheck must fail for some variant
}{
	// Minimized oracle seed 678 (genCase): {b, c} (supp 1) is reported,
	// but {a, b, c, d} has support 1. Its witnesses {a,b,c} and {b,c,d}
	// are pruned by genuine repository rejections, {a,b,c,d} is beyond
	// zmax+1 (docs/m5-investigation.md).
	{"seed678 closed -x", "c d a 1\nd b a c 1\nc 1\na d 2\n", "-tc -s-1 -n2 -q-1 -x", true},
	{"seed678 maximal -x", "c d a 1\nd b a c 1\nc 1\na d 2\n", "-tm -s-1 -n2 -q-1 -x", true},
	{"seed678 closed", "c d a 1\nd b a c 1\nc 1\nd 1\na d 2\n", "-tc -s-1 -n2 -q-1", true},
	{"seed678 maximal", "c d a 1\nd b a c 1\nc 1\nd 1\na d 2\n", "-tm -s-1 -n2 -q-1", true},
	// Original seed 678 data (also in TestDeviations).
	{"seed678 full", "a d 1\nd a 4\ne 4\nd a 5\na 4\na d b 2\ne a d 3\nd 4\n" +
		"e 1\ne 3\na e a 2\nc e d a 3\nc a d b a c 4\nc 1\ne d 3\na d e 2\n",
		"-tc -s-3 -n2 -q-1 -x", true},
	// Two items, zmax 1, a in every transaction of b. With -q-1, a is
	// processed first (descending codes) and is not in b's conditional
	// tree, so {b} needs a complete check (closed and maximal; no
	// projection at zmax 1, and the chain shortcut collects only items
	// below b).
	{"above closed", "a b 2\na 1\nc 1\n", "-tc -s-1 -n1 -q-1", true},
	{"above maximal", "a b 2\na 1\nc 1\n", "-tm -s-1 -n1 -q-1", true},
	// With -q1 the extension a of {b} is below b and the local check
	// suffices for closed sets. For maximal sets {a} (closed) has the
	// frequent extension b above it; the chain shortcut stores {a, b}
	// first in every variant except top-down, which has no shortcut.
	{"below closed", "a b 2\na 1\nc 1\n", "-tc -s-1 -n1 -q1", false},
	{"below maximal", "a b 2\na 1\nc 1\n", "-tm -s-1 -n1 -q1", true},
	// Deeper: x (most frequent, highest code with -q1) extends {p, q}
	// only together with r, which makes the closure 4 items (beyond
	// zmax+1 = 3); {x, p, q} is pruned by the stored {x, p, q, ...}
	// subtree of a sibling.
	{"above depth 2", "x p q r 2\nx p 3\nx q 3\np q r 1\nx r 1\n", "-tc -s-1 -n2 -q1", true},
	// B5 reproducers of fpgrowth-core.md.
	{"B5 closed", "i0 i3 i2 i1 x 2\ni0 i3 i2 i1 y 2\ni0 i3 5\ni0 3\ni2 z 1\n", "-tc -s-1 -n2 -q-1", true},
	{"B5 maximal", "i5 i2 i0 i7 i3 1\ni2 1\ni1 1\ni1 i3 i7 i8 1\ni0 i5 i6 1\n", "-tm -s-1 -n2 -x", true},
}

// TestExtCheckRegressions runs the regression cases with every variant:
// the complete check must agree with the oracle, and where localBad is
// set the local check must fail for at least one variant (it is not
// enough to look at the conditional tree).
func TestExtCheckRegressions(t *testing.T) {
	t.Parallel()
	for _, tt := range m5Regressions {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			txs := txsOf(t, tt.data, true)
			base, err := parseOpts(tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			want := oracleSets(t, txs, base)
			var localFails []string
			for _, v := range variants {
				for mode := range 4 { // rowsAuto, rowsSparse, rowsMixed, local
					local := mode == 3
					cfg := base
					cfg.Algo, cfg.Pack, cfg.Mode = v.algo, v.pack, base.Mode&^v.clear
					var check m5Check
					rows := rowMode(mode)
					if local {
						check, rows = localCheck, rowsAuto
					}
					hooks, sink := withSource(check, txs, nil, rows)
					res, err := run(scanOf(txs), cfg, runOpts{hooks: hooks, sink: sink})
					if err != nil {
						t.Fatalf("%s: %v", v.name, err)
					}
					d := oracle.DiffSets(want, res.sink.sets, oracle.DiffOptions{IgnoreEval: true})
					switch {
					case !local && d != "":
						t.Errorf("%s: complete check differs from the oracle:\n%s", v.name, d)
					case local && d != "":
						localFails = append(localFails, v.name)
					}
				}
			}
			if tt.localBad && len(localFails) == 0 {
				t.Errorf("local check passes every variant, want a failure")
			}
			if !tt.localBad && len(localFails) > 0 {
				t.Errorf("local check fails for %v", localFails)
			}
			t.Logf("local check fails for %v", localFails)
		})
	}
}

// TestExtCheckLocalSweep counts the random oracle cases a local check
// gets wrong (it must get some wrong; the complete check gets none, see
// TestExtCheckOracle).
func TestExtCheckLocalSweep(t *testing.T) {
	t.Parallel()
	cases := 1500
	if testing.Short() {
		cases = 200
	}
	runs, wrong, datasets := 0, 0, 0
	for seed := range uint64(cases) {
		c, err := genM5Case(seed)
		if err != nil {
			t.Fatal(err)
		}
		n, bad := checkM5Case(t, c, localCheck)
		runs += n
		wrong += len(bad)
		if len(bad) > 0 {
			datasets++
		}
	}
	if wrong == 0 {
		t.Errorf("local check agrees with the oracle on all %d runs", runs)
	}
	t.Logf("local check: %d of %d runs (%d of %d datasets) differ from the oracle",
		wrong, runs, datasets, cases)
}

// filterSize keeps the sets with zmin..zmax items.
func filterSize(sets []oracle.Set, zmin, zmax int) []oracle.Set {
	var out []oracle.Set
	for _, s := range sets {
		if len(s.Items) >= zmin && len(s.Items) <= zmax {
			out = append(out, s)
		}
	}
	return out
}

// TestExtCheckUnlimited compares the size-limited search with the
// unlimited one, filtered by size, on datasets too large for the oracle
// (up to 40 items): closedness and maximality do not depend on the size
// limit.
func TestExtCheckUnlimited(t *testing.T) {
	t.Parallel()
	cases := 25
	if testing.Short() {
		cases = 5
	}
	if s := os.Getenv("FPG_M5_UNLIMITED"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("FPG_M5_UNLIMITED: %v", err)
		}
		cases = n
	}
	runs := 0
	for seed := range uint64(cases) {
		r := rand.New(rand.NewPCG(seed, 0x11d))
		ni := 20 + r.IntN(21)
		gc := oracle.GenConfig{
			Seed: seed, NumTransactions: 40 + r.IntN(160), NumItems: ni,
			MinLen: 1 + r.IntN(4), Skew: r.IntN(3), Patterns: 1 + r.IntN(4),
			PatternLen: 2 + r.IntN(6), PatternProb: r.Float64() * 0.8,
			DupTxProb: r.Float64() * 0.2, Weights: oracle.WeightDist(r.IntN(4)),
			Names: oracle.NamesLetters,
		}
		gc.MaxLen = gc.MinLen + 2 + r.IntN(10)
		txs, err := oracle.Generate(gc)
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range cloMaxTargets {
			for _, v := range variants {
				cfg := DefaultConfig()
				cfg.Target, cfg.Algo, cfg.Pack = tg.target, v.algo, v.pack
				cfg.Mode &^= v.clear
				if r.IntN(3) == 0 {
					cfg.Mode &^= ModePerfect
				}
				if r.IntN(3) == 0 {
					cfg.Mode &^= ModeTail
				}
				cfg.SMin = float64(-(2 + r.IntN(8)))
				cfg.Sort = []int{2, 1, 0, -1, -2}[r.IntN(5)]
				full, err := run(scanOf(txs), cfg, runOpts{})
				if errors.Is(err, ErrNoItems) {
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, zmax := range []int{1, 2, 3, 4} {
					cfg.ZMax = zmax
					got, err := run(scanOf(txs), cfg, runOpts{})
					if err != nil {
						t.Fatal(err)
					}
					runs++
					want := filterSize(full.sink.sets, cfg.ZMin, zmax)
					if d := oracle.DiffSets(want, got.sink.sets, oracle.DiffOptions{IgnoreEval: true}); d != "" {
						t.Fatalf("seed %d %s/%s %s:\n%s", seed, tg.name, v.name, describe(cfg), d)
					}
				}
			}
		}
	}
	t.Logf("%d size-limited runs equal to the unlimited ones", runs)
}

// TestSeek compares seek with a linear search.
func TestSeek(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		idx := make([]int32, r.IntN(40))
		v := int32(0)
		for k := range idx {
			v += 1 + int32(r.IntN(5))
			idx[k] = v
		}
		i := r.IntN(len(idx) + 1)
		w := int32(r.IntN(int(v) + 3))
		want := i
		for want < len(idx) && idx[want] < w {
			want++
		}
		if got := seek(idx, i, w); got != want {
			t.Fatalf("seek(%v, %d, %d) = %d, want %d", idx, i, w, got, want)
		}
	}
}

// TestExtCheckBits compares the cover computations of the bit sets
// (bitCover, i.e. load and and, covers, suppWith) with a direct
// computation from the stored transactions, on data large enough for
// word lists of very different lengths, in every row form.
func TestExtCheckBits(t *testing.T) {
	for seed := range uint64(12) {
		r := rand.New(rand.NewPCG(seed, 0xb175))
		var txs []oracle.Transaction
		for range 500 + r.IntN(4000) {
			var tx oracle.Transaction
			for j := range 40 {
				if r.IntN(2*(j+1)*(j+1)) < 3*(j/8+1) { // skewed item frequencies
					tx.Items = append(tx.Items, strconv.Itoa(j))
				}
			}
			tx.Weight = 1
			if seed%2 == 1 {
				tx.Weight = tract.Supp(r.IntN(9)) // also 0
			}
			txs = append(txs, tx)
		}
		ib := tract.NewItemBase[string]()
		if err := ib.Count(scanOf(txs)); err != nil {
			t.Fatal(err)
		}
		if ib.Recode(1, 0) < 1 {
			continue
		}
		bag, err := ib.BuildBag(scanOf(txs), 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, rows := range []rowMode{rowsAuto, rowsSparse, rowsMixed} {
			c, err := newExtChecker(bag, report.TargetMaximal, 1, rows, func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			k := tract.Item(len(c.freq))
			for range 300 {
				set := []tract.Item{tract.Item(r.IntN(int(k)))}
				for range r.IntN(3) {
					if it := tract.Item(r.IntN(int(k))); !slices.Contains(set, it) {
						set = append(set, it)
					}
				}
				var cover []int32 // the transactions that contain set
				for tx := range c.txW {
					items := c.txItems[c.txOff[tx]:c.txOff[tx+1]]
					if !slices.ContainsFunc(set, func(it tract.Item) bool { return !slices.Contains(items, it) }) {
						cover = append(cover, int32(tx))
					}
				}
				n := c.bitCover(set)
				var got []int32
				for i, w := range c.nz {
					if c.cover[i] == 0 {
						t.Fatalf("seed %d rows %d set %v: zero cover word %d", seed, rows, set, w)
					}
					for x := c.cover[i]; x != 0; x &= x - 1 {
						got = append(got, w*64+int32(bits.TrailingZeros64(x)))
					}
				}
				if n != len(cover) || !slices.Equal(got, cover) {
					t.Fatalf("seed %d rows %d set %v: cover %v (n %d), want %v", seed, rows, set, got, n, cover)
				}
				// the same cover level by level in the order of set (as
				// checkPath builds it)
				lv := []coverLevel{{}, {}}
				c.load(&lv[0], set[0])
				for _, it := range set[1:] {
					c.and(&lv[1], &lv[0], it)
					lv[0], lv[1] = lv[1], lv[0]
				}
				if lv[0].n != n || !slices.Equal(lv[0].nz, c.nz) || !slices.Equal(lv[0].cover, c.cover) {
					t.Fatalf("seed %d rows %d set %v: path cover differs", seed, rows, set)
				}
				for j := range k {
					var s tract.Supp
					all := true
					for _, tx := range cover {
						if slices.Contains(c.txItems[c.txOff[tx]:c.txOff[tx+1]], j) {
							s += c.txW[tx]
						} else {
							all = false
						}
					}
					if got := c.suppWith(j); got != s {
						t.Fatalf("seed %d rows %d set %v: suppWith(%d) = %d, want %d", seed, rows, set, j, got, s)
					}
					if got := c.covers(j); got != all {
						t.Fatalf("seed %d rows %d set %v: covers(%d) = %v, want %v", seed, rows, set, j, got, all)
					}
				}
			}
		}
	}
}
