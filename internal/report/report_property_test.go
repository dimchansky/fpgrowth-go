package report

import (
	"fmt"
	"math"
	"math/bits"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// collectSink records the reported item sets in canonical form.
type collectSink struct {
	sets []reportedSet
}

type reportedSet struct {
	mask   uint64
	order  string // items in report order
	supp   tract.Supp
	eval   float64
	weight float64
}

func (c *collectSink) ItemSet(s ItemSet) error {
	var m uint64
	var b strings.Builder
	for i, it := range s.Items {
		m |= 1 << it
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprint(&b, it)
	}
	c.sets = append(c.sets, reportedSet{mask: m, order: b.String(), supp: s.Supp, eval: s.Eval, weight: s.Weight})
	return nil
}

func (c *collectSink) Rule(Rule) error { return nil }

// expectedSet is an item set the brute force says must be reported.
type expectedSet struct {
	mask uint64
	supp tract.Supp
	eval float64
}

// bruteForce enumerates all item sets of db and applies the semantics of
// the reporter: target filter on the frequent sets, then support, size,
// border and evaluation gates.
func bruteForce(db *simDB, cfg *simConfig) []expectedSet {
	n := db.nItems
	supp := make([]tract.Supp, 1<<n)
	for m := range supp {
		supp[m] = db.support(uint64(m))
	}
	total := db.total()
	ldps := make([]float64, n)
	b := float64(total)
	if b > 0 {
		b = math.Log(b) / ln2
	}
	for i, f := range db.itemFreqs() {
		if f > 0 {
			ldps[i] = math.Log(float64(f))/ln2 - b
		}
	}
	var out []expectedSet
	for m := range supp {
		s := supp[m]
		if s < cfg.smin || !targetHolds(cfg, supp, m, n) {
			continue
		}
		size := bits.OnesCount(uint(m))
		if s > cfg.smax || size < cfg.zmin || size > cfg.zmax {
			continue
		}
		if size < len(cfg.border) && size >= cfg.zmin && s < cfg.border[size] {
			continue
		}
		var v float64
		if cfg.eval {
			v = bruteLogRatio(uint64(m), s, total, ldps)
			dir := 1.0
			if cfg.evalDir < 0 {
				dir = -1
			}
			if dir*v < dir*cfg.evalThresh {
				continue
			}
		}
		out = append(out, expectedSet{mask: uint64(m), supp: s, eval: v})
	}
	return out
}

func targetHolds(cfg *simConfig, supp []tract.Supp, m, n int) bool {
	s := supp[m]
	switch cfg.target {
	case TargetClosed:
		for i := range n {
			if m&(1<<i) == 0 && supp[m|1<<i] == s {
				return false
			}
		}
	case TargetMaximal:
		if m == 0 {
			return false // the empty set is never maximal
		}
		for i := range n {
			if m&(1<<i) == 0 && supp[m|1<<i] >= cfg.smin {
				return false
			}
		}
	case TargetGenerators:
		for i := range n {
			if m&(1<<i) != 0 && supp[m&^(1<<i)] == s {
				return false
			}
		}
	}
	return true
}

func bruteLogRatio(m uint64, s, total tract.Supp, ldps []float64) float64 {
	if bits.OnesCount64(m) <= 1 {
		return 0
	}
	sum := 0.0
	if s > 0 {
		sum = math.Log(float64(s)/float64(total)) / ln2
	}
	for i := range ldps {
		if m&(1<<i) != 0 {
			sum -= ldps[i]
		}
	}
	return sum
}

// randomSimConfig draws a configuration for target.
func randomSimConfig(rng *rand.Rand, db *simDB, target Target) *simConfig {
	total := db.total()
	cfg := &simConfig{
		target:  target,
		dir:     -1,
		smin:    1 + tract.Supp(rng.Int64N(int64(max(1, total/2)))),
		smax:    MaxSupp,
		zmin:    rng.IntN(3),
		zmax:    MaxSize,
		output:  "emit",
		perfect: rng.IntN(5) != 0,
		packed:  rng.IntN(3) == 0,
	}
	switch target {
	case TargetClosed, TargetMaximal:
		cfg.descending = true
		cfg.tail = target == TargetMaximal && rng.IntN(2) == 0
	case TargetGenerators:
		cfg.perfect = true
		if rng.IntN(2) == 0 {
			cfg.mode |= ModeSort
		}
	default:
		cfg.descending = rng.IntN(2) == 0
	}
	if rng.IntN(3) == 0 {
		cfg.smax = cfg.smin + tract.Supp(rng.Int64N(int64(total)))
	}
	if rng.IntN(2) == 0 {
		cfg.zmax = cfg.zmin + rng.IntN(4)
	}
	if rng.IntN(4) == 0 {
		cfg.border = make([]tract.Supp, 1+rng.IntN(4))
		for i := range cfg.border {
			cfg.border[i] = cfg.smin + tract.Supp(rng.IntN(4))
		}
	}
	if rng.IntN(5) == 0 {
		cfg.eval = true
		cfg.evalDir = 1
		if rng.IntN(4) == 0 {
			cfg.evalDir = -1
		}
		cfg.evalThresh = float64(rng.IntN(1500)-500)/1000 + 1.0/1024
	}
	cfg.psp = rng.IntN(2) == 0
	return cfg
}

func TestReporter_PropertyBruteForce(t *testing.T) {
	t.Parallel()
	targets := []Target{TargetAll, TargetClosed, TargetMaximal, TargetGenerators}
	for _, target := range targets {
		t.Run(targetName(target), func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewPCG(uint64(target)+1, 42))
			for iter := range 600 {
				db := randomDB(rng, 2+rng.IntN(7), 1+rng.IntN(12), 1+rng.IntN(3), 0.25+rng.Float64()/2)
				cfg := randomSimConfig(rng, db, target)
				checkAgainstBruteForce(t, iter, db, cfg, rng)
				if t.Failed() {
					return
				}
			}
		})
	}
}

func describe(db *simDB, cfg *simConfig) string {
	return fmt.Sprintf("db=%v w=%v cfg=%+v", db.trans, db.wgts, *cfg)
}

func checkAgainstBruteForce(t *testing.T, iter int, db *simDB, cfg *simConfig, rng *rand.Rand) {
	t.Helper()
	want := bruteForce(db, cfg)
	sink := &collectSink{}
	r, _, err := simulate(db, cfg, sink, rng, false)
	if err != nil {
		t.Fatalf("iter %d: %v", iter, err)
	}
	got := slices.Clone(sink.sets)
	slices.SortFunc(got, func(a, b reportedSet) int { return compareMasks(a.mask, b.mask) })
	for i := 1; i < len(got); i++ {
		if got[i].mask == got[i-1].mask {
			t.Fatalf("iter %d: set %q reported twice\n%s", iter, got[i].order, describe(db, cfg))
		}
	}
	if len(got) != len(want) {
		t.Fatalf("iter %d: %d sets reported, brute force %d\ngot  %v\nwant %v\n%s",
			iter, len(got), len(want), masksOf(got), wantMasks(want), describe(db, cfg))
	}
	wantStats := make([]uint64, db.nItems+1)
	for i, w := range want {
		g := got[i]
		if g.mask != w.mask || g.supp != w.supp {
			t.Fatalf("iter %d: got set %b (%d), want %b (%d)\n%s", iter, g.mask, g.supp, w.mask, w.supp, describe(db, cfg))
		}
		if cfg.eval && math.Abs(g.eval-w.eval) > 1e-9 {
			t.Fatalf("iter %d: set %b eval %v, want %v", iter, g.mask, g.eval, w.eval)
		}
		wantStats[bits.OnesCount64(w.mask)]++
	}
	checkCounts(t, iter, r, want, wantStats, cfg)

	// The same run without a sink must count the same sets (count-only
	// mode unless there is a border or an evaluation).
	countCfg := *cfg
	countCfg.output = "count"
	r2, _, err := simulate(db, &countCfg, nil, rng, false)
	if err != nil {
		t.Fatalf("iter %d: %v", iter, err)
	}
	checkCounts(t, iter, r2, want, wantStats, &countCfg)
}

func checkCounts(t *testing.T, iter int, r *Reporter, want []expectedSet, wantStats []uint64, cfg *simConfig) {
	t.Helper()
	if r.Count() != uint64(len(want)) {
		t.Fatalf("iter %d (%s): Count = %d, want %d", iter, cfg.output, r.Count(), len(want))
	}
	n := len(wantStats)
	for n > 0 && wantStats[n-1] == 0 {
		n--
	}
	if got := r.Stats(); !slices.Equal(got, wantStats[:n]) {
		t.Fatalf("iter %d (%s): Stats = %v, want %v", iter, cfg.output, got, wantStats[:n])
	}
	if !cfg.psp {
		return
	}
	wantPsp := map[[2]int64]uint64{}
	for _, w := range want {
		wantPsp[[2]int64{int64(bits.OnesCount64(w.mask)), int64(w.supp)}]++
	}
	psp := r.Spectrum()
	var cells int
	for c := range psp.All() {
		cells++
		if wantPsp[[2]int64{int64(c.Size), int64(c.Supp)}] != c.Count {
			t.Fatalf("iter %d (%s): spectrum cell %+v, want count %d", iter, cfg.output, c,
				wantPsp[[2]int64{int64(c.Size), int64(c.Supp)}])
		}
	}
	if cells != len(wantPsp) || psp.SigCount() != len(wantPsp) || psp.Total() != uint64(len(want)) {
		t.Fatalf("iter %d (%s): spectrum has %d cells (sigcnt %d, total %d), want %d cells, total %d",
			iter, cfg.output, cells, psp.SigCount(), psp.Total(), len(wantPsp), len(want))
	}
}

func compareMasks(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func masksOf(s []reportedSet) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = fmt.Sprintf("{%s}:%d", x.order, x.supp)
	}
	return out
}

func wantMasks(s []expectedSet) []string {
	out := make([]string, len(s))
	for i, x := range s {
		var items []string
		for j := range 64 {
			if x.mask&(1<<j) != 0 {
				items = append(items, fmt.Sprint(j))
			}
		}
		out[i] = fmt.Sprintf("{%s}:%d", strings.Join(items, " "), x.supp)
	}
	return out
}

// TestReporter_PropertyClosedMaximalZmax checks that a miner that collects
// perfect extensions also at the Xable boundary (the fix for
// fpgrowth-core.md B5) gets exactly the closed/maximal sets up to zmax,
// without the chain shortcut that otherwise masks the problem.
func TestReporter_PropertyClosedMaximalZmax(t *testing.T) {
	t.Parallel()
	for _, target := range []Target{TargetClosed, TargetMaximal} {
		rng := rand.New(rand.NewPCG(uint64(target)+7, 3))
		for iter := range 800 {
			db := randomDB(rng, 3+rng.IntN(6), 2+rng.IntN(12), 1+rng.IntN(3), 0.3+rng.Float64()/2)
			cfg := randomSimConfig(rng, db, target)
			cfg.zmax = cfg.zmin + rng.IntN(3)
			cfg.noChain, cfg.boundaryPex, cfg.perfect = true, true, true
			checkAgainstBruteForce(t, iter, db, cfg, rng)
			if t.Failed() {
				return
			}
		}
	}
}
