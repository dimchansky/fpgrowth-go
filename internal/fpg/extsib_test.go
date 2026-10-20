package fpg

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Tests of the sibling rows of the maximal check (extsib.go, issue #19).

// genSibCase draws a dense dataset of 65 to 400 transactions, so that
// parent covers span several 64-bit words with a ragged last word, and
// options with a small maximum size.
func genSibCase(seed uint64) (m5Case, error) {
	r := rand.New(rand.NewPCG(seed, 0x51b5))
	gc := oracle.GenConfig{
		Seed:            seed ^ 0x5a5a,
		NumTransactions: 65 + r.IntN(336),
		NumItems:        4 + r.IntN(11),
		Skew:            r.IntN(3),
		Patterns:        r.IntN(3),
		PatternProb:     r.Float64() * 0.5,
		DupItemProb:     0.05,
		DupTxProb:       r.Float64() * 0.2,
		EmptyProb:       r.Float64() * 0.05,
		Weights:         oracle.WeightDist(r.IntN(3)), // unit, small, with zero
		Names:           oracle.NamesLetters,
	}
	switch {
	case seed%8 == 7:
		gc.NumItems = 17 + r.IntN(3) // more than 16 items
	}
	gc.MinLen = gc.NumItems / 4
	gc.MaxLen = gc.NumItems
	gc.PatternLen = 1 + r.IntN(min(5, gc.NumItems))
	txs, err := oracle.Generate(gc)
	if err != nil {
		return m5Case{}, err
	}
	if seed%3 == 0 {
		// independent items, unit weights: almost no identical
		// transactions, so no merged weights and no weight planes
		txs = sibData(r, gc.NumTransactions, 16+r.IntN(5), 0.3+0.3*r.Float64(), 1)
	}
	c := m5Case{propCase: propCase{seed: seed, txs: txs, smax: 100}}
	c.smin = float64(5 + r.IntN(40)) // percentage: many frequent pairs and triples
	c.zmin = 1
	c.zmax = 2 + r.IntN(3)
	c.noPerf = r.IntN(3) == 0
	c.noTail = r.IntN(3) == 0
	c.sort = []int{2, 2, 1, 0, -1, -2}[r.IntN(6)]
	return c, nil
}

// sibTotals sums the sibling statistics of many runs.
type sibTotals struct {
	sibStats
	runs, noPerfBuilds int
}

// checkSibCase compares the maximal sets of every variant with the
// oracle, with sibling rows built as sib says and verified against the
// ordinary check (and, with brute, every check against a brute force
// from the source transactions), and adds the statistics to tot.
func checkSibCase(t *testing.T, c m5Case, sib sibHook, brute bool, tot *sibTotals) {
	t.Helper()
	db, err := oracle.New(c.txs, oracle.Options{})
	if err != nil {
		t.Fatalf("seed %d: oracle: %v", c.seed, err)
	}
	p := oracle.NewSetParams(oracle.Maximal, oracle.AbsMinSupp(c.smin, db.Total()))
	p.MaxSupp = oracle.AbsMaxSupp(c.smax, db.Total())
	p.MinSize, p.MaxSize = c.zmin, c.zmax
	want, err := db.Mine(p)
	if err != nil {
		t.Fatalf("seed %d: oracle mine: %v", c.seed, err)
	}
	for _, v := range variants {
		cfg := c.config(v, report.TargetMaximal)
		var check m5Check
		if brute {
			check = bruteCheck
		}
		hooks, sink := withSource(check, c.txs, nil, rowMode(c.seed%3))
		hooks.sib = sib
		var chk *extChecker
		hooks.checker = func(x *extChecker) { chk = x }
		res, err := run(scanOf(c.txs), cfg, runOpts{hooks: hooks, sink: sink})
		name := fmt.Sprintf("seed %d %s %s sib %+v", c.seed, v.name, describe(cfg), sib)
		if errors.Is(err, ErrNoItems) {
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if d := oracle.DiffSets(want, res.sink.sets, oracle.DiffOptions{IgnoreEval: true}); d != "" {
			t.Errorf("%s:\n%s", name, d)
		}
		if chk != nil {
			st := chk.sibStat
			tot.runs++
			tot.builds += st.builds
			tot.multiWord += st.multiWord
			tot.weighted += st.weighted
			tot.checks += st.checks
			if c.noPerf {
				tot.noPerfBuilds += st.builds
			}
		}
	}
}

// TestExtCheckSiblingRows runs dense multi-word cases with the sibling
// rows built at the first child, at a random child (children checked
// before and after the build) and by the production rule, against the
// oracle and the ordinary check (verify), and against a brute force from
// the source transactions on a part of them. FPG_SIB_CASES sets the
// number of datasets (default 120, 30 with -short or the race detector).
func TestExtCheckSiblingRows(t *testing.T) {
	t.Parallel()
	cases := 120
	if testing.Short() || raceEnabled {
		cases = 30
	}
	if s := os.Getenv("FPG_SIB_CASES"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("FPG_SIB_CASES: %v", err)
		}
		cases = n
	}
	var tot sibTotals
	for seed := range uint64(cases) {
		c, err := genSibCase(seed)
		if err != nil {
			t.Fatal(err)
		}
		r := rand.New(rand.NewPCG(seed, 7))
		for _, sib := range []sibHook{
			{mode: sibForce, verify: true},
			{mode: sibAt, at: r.IntN(6), verify: true},
			{mode: sibAuto, verify: true},
		} {
			checkSibCase(t, c, sib, seed%4 == 0, &tot)
		}
		if t.Failed() {
			break
		}
	}
	t.Logf("%d datasets, %d runs: %d builds (%d of several words, %d weighted, %d with -x), %d checks from sibling rows",
		cases, tot.runs, tot.builds, tot.multiWord, tot.weighted, tot.noPerfBuilds, tot.checks)
	if tot.builds == 0 || tot.multiWord == 0 || tot.weighted == 0 || tot.weighted == tot.builds ||
		tot.noPerfBuilds == 0 || tot.checks == 0 {
		t.Errorf("sibling rows not exercised: %+v", tot)
	}
}

// TestExtCheckSiblingRowsOracle runs the ordinary random oracle cases
// (TestExtCheckOracle's generator) with the sibling rows forced at the
// first child and at a random child.
func TestExtCheckSiblingRowsOracle(t *testing.T) {
	t.Parallel()
	cases := 600
	if testing.Short() || raceEnabled {
		cases = 100
	}
	var tot sibTotals
	for seed := range uint64(cases) {
		c, err := genM5Case(seed)
		if err != nil {
			t.Fatal(err)
		}
		if c.sel != nil || c.border != nil {
			continue // checkSibCase mines without selection and border
		}
		for _, sib := range []sibHook{{mode: sibForce, verify: true}, {mode: sibAt, at: int(seed % 4), verify: true}} {
			checkSibCase(t, c, sib, false, &tot)
		}
		if t.Failed() {
			break
		}
	}
	t.Logf("%d runs: %d builds (%d weighted), %d checks from sibling rows", tot.runs, tot.builds, tot.weighted, tot.checks)
	if tot.builds == 0 || tot.weighted == 0 {
		t.Errorf("sibling rows not exercised: %+v", tot)
	}
}

// sibData returns n random transactions over k items (each present with
// probability d) with weights 0..maxW (none 0 if maxW is 1).
func sibData(r *rand.Rand, n, k int, d float64, maxW int) []oracle.Transaction {
	txs := make([]oracle.Transaction, n)
	for i := range txs {
		for j := range k {
			if r.Float64() < d {
				txs[i].Items = append(txs[i].Items, "i"+strconv.Itoa(j))
			}
		}
		if len(txs[i].Items) == 0 {
			txs[i].Items = []string{"i0"}
		}
		txs[i].Weight = 1
		if maxW > 1 {
			txs[i].Weight = tract.Supp(r.IntN(maxW + 1))
		}
	}
	return txs
}

// pexOf returns the items outside prefix that are in every transaction
// of the cover of prefix (path level len(prefix)-1 of c).
func pexOf(c *extChecker, prefix []tract.Item) []tract.Item {
	lv := &c.path[len(prefix)-1]
	c.nz, c.cover = lv.nz, lv.cover
	var pex []tract.Item
	for j := range tract.Item(len(c.freq)) {
		if !slices.Contains(prefix, j) && lv.n > 0 && c.covers(j) {
			pex = append(pex, j)
		}
	}
	return pex
}

// TestExtCheckSiblingSupp compares the supports from sibling rows with
// suppWith on the child's cover, for random parents and children of 1 to
// 10 words of positions, with and without weight planes, and checks that
// every item without a row is infrequent with the parent.
func TestExtCheckSiblingSupp(t *testing.T) {
	r := rand.New(rand.NewPCG(19, 1))
	for iter := range 60 {
		n := 1 + r.IntN(1400)
		maxW := []int{1, 1, 3, 9}[iter%4]
		txs := sibData(r, n, 6+r.IntN(20), 0.2+0.6*r.Float64(), maxW)
		smin := tract.Supp(1 + r.IntN(max(n/8, 1)))
		c, _ := newTestChecker(t, txs, report.TargetMaximal, smin, rowMode(iter%3))
		c.sib = sibHook{mode: sibForce}
		c.sibCap = math.MaxInt // small bags: rows may exceed 2 words per occurrence
		k := tract.Item(len(c.freq))
		if k < 3 {
			continue
		}
		for range 20 {
			plen := 1 + r.IntN(2)
			perm := r.Perm(int(k))
			prefix := make([]tract.Item, 0, plen+1)
			for _, i := range perm[:plen+1] {
				prefix = append(prefix, tract.Item(i))
			}
			c.checkPath(prefix, prefix, 1) // builds the rows of the parent level
			L := len(prefix) - 2
			s := &c.sibs[L]
			if !s.built {
				t.Fatalf("iter %d: rows of level %d not built", iter, L)
			}
			if want := (c.path[L].n + 63) / 64; s.words != want {
				t.Fatalf("iter %d: %d words per row, want %d", iter, s.words, want)
			}
			// supp(P ∪ {j}) per row; no row: infrequent with P
			lp := &c.path[L]
			c.nz, c.cover = lp.nz, lp.cover
			rowOf := map[tract.Item][]uint64{}
			for _, e := range s.ents {
				rowOf[e.item] = s.rows[e.off : e.off+s.words]
				if got := c.suppWith(e.item); got != e.supp {
					t.Fatalf("iter %d: supp(P ∪ {%d}) = %d, row %d", iter, e.item, got, e.supp)
				}
			}
			for j := range k {
				if _, ok := rowOf[j]; !ok && !slices.Contains(prefix[:L+1], j) && c.suppWith(j) >= smin {
					t.Fatalf("iter %d: item %d has supp %d >= smin %d but no row", iter, j, c.suppWith(j), smin)
				}
			}
			// supp(P ∪ {h} ∪ {j}) from the rows against suppWith
			rh, ok := rowOf[prefix[L+1]]
			lc := &c.path[L+1]
			c.nz, c.cover = lc.nz, lc.cover
			for _, e := range s.ents {
				want := c.suppWith(e.item)
				if !ok {
					continue // the child is infrequent (checked above)
				}
				if got := s.supp(rh, rowOf[e.item]); got != want {
					t.Fatalf("iter %d n %d words %d planes %d: supp(X ∪ {%d}) = %d from rows, suppWith %d",
						iter, n, s.words, len(s.planes)/s.words, e.item, got, want)
				}
			}
		}
	}
}

// TestExtCheckSiblingInvalidate drives checkPath with sequences of
// prefixes (depth-first steps, shrink and regrow with the same items,
// the same item at a level with a different earlier level, repeated
// prefixes with different perfect-extension sets, jumps) interleaved with
// check calls and one-item prefixes, and a wrapping set stamp, and
// compares every verdict with a checker without sibling rows.
func TestExtCheckSiblingInvalidate(t *testing.T) {
	r := rand.New(rand.NewPCG(19, 2))
	for iter := range 40 {
		n := 65 + r.IntN(500)
		maxW := []int{1, 4}[iter%2]
		txs := sibData(r, n, 5+r.IntN(10), 0.3+0.5*r.Float64(), maxW)
		smin := tract.Supp(1 + r.IntN(max(n/6, 1)))
		c, _ := newTestChecker(t, txs, report.TargetMaximal, smin, rowsAuto)
		ref, _ := newTestChecker(t, txs, report.TargetMaximal, smin, rowsAuto)
		c.sib = sibHook{mode: []sibMode{sibForce, sibAt}[iter%2], at: 1}
		ref.sib = sibHook{mode: sibOff}
		k := len(c.freq)
		if k < 4 {
			continue
		}
		var prefix []tract.Item
		for step := range 400 {
			switch op := r.IntN(10); {
			case op < 3 && len(prefix) < 4: // deeper
				prefix = append(prefix, tract.Item(r.IntN(k)))
			case op < 5 && len(prefix) > 0: // replace the last item (next sibling)
				prefix[len(prefix)-1] = tract.Item(r.IntN(k))
			case op == 5 && len(prefix) > 1: // shrink, later regrow with the same items
				prefix = prefix[:len(prefix)-1]
			case op == 6 && len(prefix) > 2: // same last item, different earlier one
				prefix[r.IntN(len(prefix)-1)] = tract.Item(r.IntN(k))
			case op == 7: // jump
				prefix = prefix[:0]
				for range 1 + r.IntN(3) {
					prefix = append(prefix, tract.Item(r.IntN(k)))
				}
			case op == 8: // interleaved calls that do not use the path
				set := []tract.Item{tract.Item(r.IntN(k))}
				c.check(set, 1)
				c.checkPath(set, set, 1)
				ref.checkPath(set, set, 1)
			}
			if len(prefix) == 0 {
				continue
			}
			if dup := slices.Compact(slices.Sorted(slices.Values(prefix))); len(dup) != len(prefix) {
				continue // a prefix has distinct items
			}
			if step%97 == 0 {
				c.setStamp = ^uint32(0) - 1 // wraps within the next calls
			}
			// prefix ∪ a random subset of its perfect extensions
			ref.checkPath(prefix, prefix, 1)
			set := slices.Clone(prefix)
			for _, p := range pexOf(ref, prefix) {
				if r.IntN(2) == 0 {
					set = append(set, p)
				}
			}
			got := c.checkPath(prefix, set, 1)
			want := ref.checkPath(prefix, set, 1)
			if got != want {
				t.Fatalf("iter %d step %d: checkPath(%v, %v) = %v, want %v", iter, step, prefix, set, got, want)
			}
		}
		if c.sibStat.builds == 0 {
			t.Fatalf("iter %d: no sibling rows built", iter)
		}
	}
}

// TestExtCheckSiblingWork checks the production rule: the rows are built
// only after the children's actual work reached half the build cost, so
// a parent whose children stop early builds no rows, and a parent with
// many maximal children does. The parent is the item with the highest
// code: the bag is sorted by codes, so its transactions are spread over
// the bag (a low code's are contiguous and its children's checks cheap).
func TestExtCheckSiblingWork(t *testing.T) {
	r := rand.New(rand.NewPCG(19, 3))
	txs := sibData(r, 3000, 40, 0.3, 1)
	children := func(c *extChecker) (built, maximal int) {
		built = -1
		p := tract.Item(len(c.freq) - 1)
		for h := range p {
			if c.checkPath([]tract.Item{p, h}, []tract.Item{p, h}, 1) {
				maximal++
			}
			if built < 0 && c.sibStat.builds > 0 {
				built = int(h)
			}
		}
		return built, maximal
	}
	// smin 1: every child has a frequent extension, found at once
	c, _ := newTestChecker(t, txs, report.TargetMaximal, 1, rowsAuto)
	if built, maximal := children(c); built >= 0 || maximal > 0 {
		t.Fatalf("smin 1: rows built at child %d, %d maximal children", built, maximal)
	}
	// smin above every triple's support (about 81) and below every
	// pair's (about 270): every child is maximal and counts its cover
	c, _ = newTestChecker(t, txs, report.TargetMaximal, 140, rowsAuto)
	built, maximal := children(c)
	if built < 2 || maximal != len(c.freq)-1 || c.sibStat.checks == 0 {
		t.Fatalf("rows built at child %d (want after a few children), %d maximal, %d checks from rows",
			built, maximal, c.sibStat.checks)
	}
	t.Logf("built at child %d of %d, %d checks from rows", built, len(c.freq)-1, c.sibStat.checks)
	// a memory budget below one level's rows: refused, same verdicts
	c, _ = newTestChecker(t, txs, report.TargetMaximal, 140, rowsAuto)
	c.sibCap = 100
	built, maximal = children(c)
	if built >= 0 || c.sibStat.refused == 0 || maximal != len(c.freq)-1 {
		t.Fatalf("budget 100 words: built at child %d, %d refusals, %d maximal", built, c.sibStat.refused, maximal)
	}
	if w := c.sibWords(); w > c.sibCap {
		t.Fatalf("%d words held, budget %d", w, c.sibCap)
	}
}

// TestExtCheckSiblingLeft checks the miner's bound of the siblings that
// follow a reported set (miner.left, used only for the cost decision of
// the sibling rows): for every check, the later checks of sets with the
// same parent prefix must not outnumber it, in every variant.
func TestExtCheckSiblingLeft(t *testing.T) {
	t.Parallel()
	type call struct {
		prefix []tract.Item
		left   int
	}
	known, unknown, tight := 0, 0, 0
	for seed := range uint64(40) {
		c, err := genSibCase(seed)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range variants {
			var calls []call
			hooks := testHooks{cloMaxCheck: func(chk *extChecker, rep *report.Reporter) report.CloMaxCheck {
				return func(set []tract.Item, supp tract.Supp) bool {
					calls = append(calls, call{slices.Clone(rep.Items()), chk.left})
					return chk.checkPath(rep.Items(), set, supp)
				}
			}}
			cfg := c.config(v, report.TargetMaximal)
			if _, err := run(scanOf(c.txs), cfg, runOpts{hooks: hooks, sink: &collectSink{}}); err != nil {
				if errors.Is(err, ErrNoItems) {
					continue
				}
				t.Fatal(err)
			}
			for i, ci := range calls {
				if len(ci.prefix) < 2 {
					continue
				}
				if ci.left < 0 {
					unknown++
					continue
				}
				known++
				parent := ci.prefix[:len(ci.prefix)-1]
				later := 0
				for _, cj := range calls[i+1:] {
					if len(cj.prefix) == len(ci.prefix) && slices.Equal(cj.prefix[:len(parent)], parent) {
						later++
					}
				}
				if later > ci.left {
					t.Fatalf("seed %d %s: prefix %v: %d later siblings checked, bound %d",
						seed, v.name, ci.prefix, later, ci.left)
				}
				if later == ci.left {
					tight++
				}
			}
		}
	}
	t.Logf("%d checks with a bound (%d exact), %d without", known, tight, unknown)
	if known == 0 || tight == 0 {
		t.Errorf("bound not exercised: %d known, %d exact", known, tight)
	}
}

// TestExtCheckSiblingRelease drives the memory budget's release branch:
// level 0 holds rows, level 2 an old buffer without rows, and the build
// at level 1 fits only after that buffer is released. The rows of level
// 0 must survive and keep giving the ordinary check's verdicts.
func TestExtCheckSiblingRelease(t *testing.T) {
	r := rand.New(rand.NewPCG(19, 4))
	txs := sibData(r, 2000, 12, 0.5, 1)
	const smin = 20
	c, _ := newTestChecker(t, txs, report.TargetMaximal, smin, rowsAuto)
	ref, _ := newTestChecker(t, txs, report.TargetMaximal, smin, rowsAuto)
	c.sib = sibHook{mode: sibForce, verify: true}
	ref.sib = sibHook{mode: sibOff}
	k := tract.Item(len(c.freq))
	a, b, d, e, f, g := k-1, k-2, k-3, k-4, k-5, k-6
	check := func(prefix ...tract.Item) {
		t.Helper()
		if got, want := c.checkPath(prefix, prefix, 1), ref.checkPath(prefix, prefix, 1); got != want {
			t.Fatalf("checkPath(%v) = %v, want %v", prefix, got, want)
		}
	}
	check(a, b)       // builds level 0 (parent {a})
	check(a, b, d)    // level 1 (parent {a, b})
	check(a, b, d, e) // level 2 (parent {a, b, d})
	check(a, f)       // drops levels 1 and 2; level 0 answers
	if !c.sibs[0].built || c.sibs[1].built || c.sibs[2].built || cap(c.sibs[2].rows) == 0 {
		t.Fatal("unexpected level state before the release")
	}
	// budget: level 0's and level 1's buffers and level 1's new rows
	// (for the parent {a, f}), but not level 2's old buffer
	held := c.sibWords() - cap(c.sibs[2].rows) - cap(c.sibs[2].planes) -
		cap(c.sibs[1].rows) - cap(c.sibs[1].planes)
	c.sibCap = held + c.sibWant(1)
	check(a, f, g) // builds level 1 after releasing level 2's buffer
	if c.sibStat.released == 0 || c.sibStat.refused != 0 || !c.sibs[1].built || cap(c.sibs[2].rows) != 0 {
		t.Fatalf("no release: %+v, level 1 built %v", c.sibStat, c.sibs[1].built)
	}
	if !c.sibs[0].built {
		t.Fatal("level 0 lost its rows")
	}
	for h := range k - 6 {
		check(a, f, h) // level 1's rows
	}
	for h := range k - 6 {
		check(a, h) // level 0's rows, still valid
	}
	if w := c.sibWords(); w > c.sibCap {
		t.Fatalf("%d words held, budget %d", w, c.sibCap)
	}
}
