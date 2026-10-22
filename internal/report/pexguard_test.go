package report

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Tests of the perfect-extension guard (issue #21): PexUseful and the
// skips in Report and expand.

// TestReporter_PexUseful is the truth table of the guard: pexMax after
// New and every configuration call, in both orders, and PexUseful at
// the levels around it.
func TestReporter_PexUseful(t *testing.T) {
	type setup func(r *Reporter)
	size := func(zmin, zmax int) setup { return func(r *Reporter) { r.SetSize(zmin, zmax) } }
	target := func(tg Target, m Mode) setup { return func(r *Reporter) { r.SetTarget(tg, m, -1) } }
	guard := func(on bool) setup { return func(r *Reporter) { r.SetPexGuard(on) } }
	for _, c := range []struct {
		name   string
		calls  []setup
		pexMax int
	}{
		{"New", nil, MaxSize},
		{"Setup", []setup{func(r *Reporter) { r.Setup() }}, MaxSize},
		{"all n2", []setup{size(1, 2)}, 2},
		{"all n0", []setup{size(0, 0)}, 0},
		{"all m3 n1", []setup{size(3, 1)}, 1},
		{"all n2 then target", []setup{size(1, 2), target(TargetAll, 0)}, 2},
		{"target then all n2", []setup{target(TargetAll, 0), size(1, 2)}, 2},
		{"rules n2", []setup{target(TargetRules, ModeNoFilter), size(1, 2)}, 2},
		{"all nofilter n2", []setup{size(1, 2), target(TargetAll, ModeNoFilter)}, 2},
		{"generators", []setup{target(TargetGenerators, 0)}, -1},
		{"generators n2", []setup{size(1, 2), target(TargetGenerators, ModeSort)}, -1},
		{"generators nofilter", []setup{target(TargetGenerators, ModeNoFilter), size(1, 2)}, -1},
		{"closed n2", []setup{target(TargetClosed, 0), size(1, 2)}, math.MaxInt},
		{"n2 closed", []setup{size(1, 2), target(TargetClosed, 0)}, math.MaxInt},
		{"maximal n1", []setup{size(1, 1), target(TargetMaximal, 0)}, math.MaxInt},
		{"closed nofilter n1", []setup{size(1, 1), target(TargetClosed, ModeNoFilter)}, math.MaxInt},
		{"all noexpand n1", []setup{size(1, 1), target(TargetAll, ModeNoExpand)}, math.MaxInt},
		{"all n1 guard off", []setup{size(1, 1), guard(false)}, math.MaxInt},
		{"guard off then n1", []setup{guard(false), size(1, 1)}, math.MaxInt},
		{"generators guard off", []setup{target(TargetGenerators, 0), guard(false)}, math.MaxInt},
		{"guard off and on n1", []setup{size(1, 1), guard(false), guard(true)}, 1},
		{"all n1 then n3", []setup{size(1, 1), size(1, 3)}, 3},
		{"closed then all n1", []setup{target(TargetClosed, 0), size(1, 1), target(TargetAll, 0)}, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := New(8, 10, nil)
			for _, f := range c.calls {
				f(r)
			}
			r.Setup()
			if r.pexMax != c.pexMax {
				t.Fatalf("pexMax = %d, want %d", r.pexMax, c.pexMax)
			}
			for k := range 5 {
				if got, want := r.PexUseful(), k < c.pexMax; got != want {
					t.Errorf("PexUseful() with %d items = %v, want %v", k, got, want)
				}
				r.Add(tract.Item(k), 5)
			}
		})
	}
}

// TestReporter_PexUsefulNew checks that a reporter used right after New
// keeps its perfect extensions (pexMax must not be its zero value).
func TestReporter_PexUsefulNew(t *testing.T) {
	r := New(4, 10, nil)
	s := &sliceSink{}
	r.SetSink(s)
	if !r.PexUseful() {
		t.Fatal("PexUseful() = false after New")
	}
	r.Add(0, 5)
	if !r.PexUseful() {
		t.Fatal("PexUseful() = false after New and Add")
	}
	r.AddPex(1)
	if err := r.Report(); err != nil {
		t.Fatal(err)
	}
	want := [][]tract.Item{{0, 1}, {0}}
	if !slices.EqualFunc(s.sets, want, slices.Equal) {
		t.Errorf("sets %v, want %v", s.sets, want)
	}
}

// TestReporter_ExpandSkip checks with a sentinel that the expansion of a
// hypercube does not visit the extensions of its sets of zmax items: the
// rejecting loop would overwrite the scratch entry items[zmax]. The
// expansion stops there also with the guard off (SetPexGuard).
func TestReporter_ExpandSkip(t *testing.T) {
	const sentinel = tract.Item(-7)
	for _, guard := range []bool{true, false} {
		r := New(8, 10, nil)
		s := &sliceSink{}
		r.SetSink(s)
		r.SetSize(1, 1)
		r.SetPexGuard(guard)
		r.Setup()
		for i := range tract.Item(4) {
			r.AddPex(i)
		}
		r.items[1] = sentinel
		if err := r.Report(); err != nil {
			t.Fatal(err)
		}
		want := [][]tract.Item{{0}, {1}, {2}, {3}}
		if !slices.EqualFunc(s.sets, want, slices.Equal) {
			t.Errorf("guard %v: sets %v, want %v", guard, s.sets, want)
		}
		if r.items[1] != sentinel {
			t.Errorf("guard %v: items[zmax] = %d, want the sentinel", guard, r.items[1])
		}
	}
}

// TestReporter_ReportSkip checks with a sentinel that Report ignores the
// inherited perfect extensions of a set of zmax items: the fill of the
// hypercube's supports would overwrite supps[zmax+1].
func TestReporter_ReportSkip(t *testing.T) {
	const sentinel = tract.Supp(-7)
	for _, guard := range []bool{true, false} {
		for _, nilSink := range []bool{false, true} {
			r := New(8, 10, nil)
			s := &sliceSink{}
			if !nilSink {
				r.SetSink(s)
			}
			r.SetSize(1, 1)
			r.SetPexGuard(guard)
			r.Setup()
			r.AddPex(5)
			r.AddPex(6)
			r.Add(0, 4)
			r.supps[2] = sentinel
			if err := r.Report(); err != nil {
				t.Fatal(err)
			}
			if !nilSink {
				want := [][]tract.Item{{0}}
				if !slices.EqualFunc(s.sets, want, slices.Equal) {
					t.Errorf("guard %v: sets %v, want %v", guard, s.sets, want)
				}
			}
			if got := r.Stats(); !slices.Equal(got, []uint64{0, 1}) {
				t.Errorf("guard %v, nil sink %v: stats %v, want [0 1]", guard, nilSink, got)
			}
			if kept := r.supps[2] == sentinel; kept != (guard || nilSink) {
				t.Errorf("guard %v, nil sink %v: supps[zmax+1] kept = %v", guard, nilSink, kept)
			}
		}
	}
}

// TestReporter_PexGuardClosedMaximal checks that the guard leaves closed
// and maximal item sets alone: a set of zmax items with a perfect
// extension is reported only together with it, i.e. not at all.
func TestReporter_PexGuardClosedMaximal(t *testing.T) {
	for _, tg := range []Target{TargetClosed, TargetMaximal} {
		for _, mode := range []Mode{0, ModeNoFilter} {
			for _, nilSink := range []bool{false, true} {
				r := New(4, 10, nil)
				s := &sliceSink{}
				if !nilSink {
					r.SetSink(s)
				}
				r.SetTarget(tg, mode, -1)
				r.SetSize(1, 1)
				r.Setup()
				r.Add(0, 4)
				r.AddPex(1)
				if err := r.Report(); err != nil {
					t.Fatal(err)
				}
				if r.Count() != 0 || len(s.sets) != 0 {
					t.Errorf("target %v mode %#x nil sink %v: %d sets %v, want none",
						tg, int(mode), nilSink, r.Count(), s.sets)
				}
			}
		}
	}
}

// fullSink records everything a sink receives, with bit-exact floats.
type fullSink struct{ b strings.Builder }

func (s *fullSink) ItemSet(is ItemSet) error {
	fmt.Fprintf(&s.b, "%v %d %x %x\n", is.Items, is.Supp, math.Float64bits(is.Weight), math.Float64bits(is.Eval))
	return nil
}

func (s *fullSink) Rule(Rule) error { return nil }

// pexScript drives a reporter like a miner on a random search tree: each
// node adds an item, maybe perfect extensions (only if PexUseful, as the
// miners' chain walks do), recurses, reports and removes. Like a miner,
// it never adds an item that is in the set or a perfect extension of it
// (in its own bookkeeping, whether or not the reporter collected the
// extension), and its random choices do not depend on the reporter.
func pexScript(r *Reporter, rng *rand.Rand, numItems int) {
	used := make([]int, numItems) // depth+1 at which an item was taken
	var rec func(depth int, supp tract.Supp)
	take := func(depth int) {
		for np := rng.IntN(4); np > 0; np-- {
			it := tract.Item(rng.IntN(numItems))
			if used[it] == 0 {
				used[it] = depth + 1
			}
		}
	}
	release := func(depth int) {
		for i, d := range used {
			if d == depth+1 {
				used[i] = 0
			}
		}
	}
	addPexs := func(depth int) {
		// the extensions taken at this depth, collected only if useful
		if r.PexUseful() {
			for i, d := range used {
				if d == depth+1 && !r.Uses(tract.Item(i)) {
					r.AddPex(tract.Item(i))
				}
			}
		}
	}
	rec = func(depth int, supp tract.Supp) {
		for k := rng.IntN(3); k > 0 && depth < 6; k-- {
			it := tract.Item(rng.IntN(numItems))
			s := max(1, supp-tract.Supp(rng.IntN(3)))
			if used[it] != 0 || !r.Add(it, s) {
				continue
			}
			used[it] = depth + 2 // released with the extensions below
			take(depth + 1)
			addPexs(depth + 1)
			rec(depth+1, s)
			_ = r.Report()
			r.Remove(1)
			release(depth + 1)
		}
	}
	take(0)
	addPexs(0)
	rec(0, 20)
	_ = r.Report()
}

// TestReporter_PexGuardSame compares random miner-like scripts with and
// without the guard: frequent sets and generators, sizes (zmin > zmax and
// zmax 0 included), border, LogRatio evaluation, spectrum and count-only
// mode. (The scripts do not respect the item order that the closed/
// maximal repository needs; the guard is off for those targets, and the
// miners' comparison in internal/fpg covers them.)
func TestReporter_PexGuardSame(t *testing.T) {
	const numItems = 10
	freqs := make([]tract.Supp, numItems)
	for i := range freqs {
		freqs[i] = tract.Supp(5 + 3*i)
	}
	run := func(seed uint64, guard bool) string {
		cfg := rand.New(rand.NewPCG(seed, 1))
		r := New(numItems, 40, freqs)
		r.SetSize(cfg.IntN(4), []int{0, 1, 1, 2, 2, 3, MaxSize}[cfg.IntN(7)])
		r.SetTarget([]Target{TargetAll, TargetAll, TargetGenerators}[cfg.IntN(3)], 0, -1)
		if cfg.IntN(3) == 0 {
			r.SetBorder(1, tract.Supp(cfg.IntN(20)))
			r.SetBorder(2, tract.Supp(cfg.IntN(20)))
		}
		if cfg.IntN(3) == 0 {
			r.SetEval(LogRatio, 1, float64(cfg.IntN(4)-2))
		}
		if cfg.IntN(2) == 0 {
			r.AddSpectrum()
		}
		sk := &fullSink{}
		if cfg.IntN(3) != 0 {
			r.SetSink(sk)
		}
		r.SetPexGuard(guard)
		r.Setup()
		pexScript(r, rand.New(rand.NewPCG(seed, 2)), numItems)
		var b strings.Builder
		fmt.Fprintf(&b, "count=%d stats=%v err=%v\n", r.Count(), r.Stats(), r.Err())
		if p := r.Spectrum(); p != nil {
			for c := range p.All() {
				fmt.Fprintf(&b, "psp %v\n", c)
			}
		}
		b.WriteString(sk.b.String())
		return b.String()
	}
	for seed := range uint64(3000) {
		if want, got := run(seed, false), run(seed, true); got != want {
			t.Fatalf("seed %d:\nguard off:\n%s\nguard on:\n%s", seed, want, got)
		}
	}
}
