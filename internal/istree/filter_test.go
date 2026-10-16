package istree

import (
	"math/bits"
	"math/rand/v2"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// pruneRef is an independent set-based specification of evaluation
// pruning (-p) followed by the closed/maximal/generator filter, written on
// bit masks of item codes instead of the tree: the semantics of ist_filter
// and ist_clomax(IST_SAFE) on a tree holding every frequent set with up to
// lim items.
type pruneRef struct {
	d      *dataset
	c      setConfig
	lim    int
	dir    float64
	thresh float64 // dir * threshold
	supp   map[uint32]tract.Supp
	marked map[uint32]bool
	bySize [][]uint32
}

func newPruneRef(d *dataset, c setConfig, lim int) *pruneRef {
	p := &pruneRef{d: d, c: c, lim: lim, supp: map[uint32]tract.Supp{}, marked: map[uint32]bool{}}
	p.dir = float64(c.eval.Measure.Dir())
	p.thresh = p.dir * c.eval.Threshold
	smin := max(c.smin, 1)
	p.bySize = make([][]uint32, lim+1)
	n := len(d.names)
	for m := uint32(1); m < 1<<n; m++ {
		z := bits.OnesCount32(m)
		if z > lim {
			continue
		}
		if s := d.supp(codesOf(m)); s >= smin {
			p.supp[m] = s
			p.bySize[z] = append(p.bySize[z], m)
		}
	}
	return p
}

func codesOf(m uint32) []tract.Item {
	var c []tract.Item
	for ; m != 0; m &= m - 1 {
		c = append(c, tract.Item(bits.TrailingZeros32(m)))
	}
	return c
}

func (p *pruneRef) worst() float64 {
	if p.dir < 0 {
		return 1
	}
	return 0
}

// eval is the aggregated rule evaluation of a set: heads in increasing
// code order, the first rule's value first.
func (p *pruneRef) eval(m uint32) float64 {
	if p.c.eval.Measure == ruleval.None {
		return 0
	}
	if bits.OnesCount32(m) <= 1 {
		return p.worst()
	}
	fn := p.c.eval.Measure.Func()
	s, n := p.supp[m], p.d.total
	var agg float64
	k := 0
	for _, h := range codesOf(m) {
		b := p.d.supp(codesOf(m &^ (1 << h)))
		hs := p.d.freqs[h]
		v := p.worst()
		if !p.c.eval.Invalidate || float64(s)*float64(n) > float64(hs)*float64(b) {
			v = fn(s, b, hs, n)
		}
		switch {
		case k == 0:
			agg = v
		case p.c.eval.Agg == AggMin:
			if v < agg {
				agg = v
			}
		case p.c.eval.Agg == AggMax:
			if v > agg {
				agg = v
			}
		case p.c.eval.Agg == AggAvg:
			agg += v
		}
		k++
		if p.c.eval.Agg == AggFirst {
			return agg
		}
	}
	if p.c.eval.Agg == AggAvg {
		agg /= float64(k)
	}
	return agg
}

// ok reports whether a frequent set is present and unmarked.
func (p *pruneRef) ok(m uint32) bool {
	_, f := p.supp[m]
	return f && !p.marked[m]
}

// subsets calls fn for every subset with one item less.
func subsets(m uint32, fn func(uint32)) {
	for r := m; r != 0; r &= r - 1 {
		fn(m &^ (r & -r))
	}
}

// run returns the reported item sets.
func (p *pruneRef) run() []oracle.Set {
	emptyMarked := false
	// evaluation marks (sets with at least two items)
	for z := 2; z <= p.lim; z++ {
		for _, m := range p.bySize[z] {
			if p.dir*p.eval(m) < p.thresh {
				p.marked[m] = true
			}
		}
	}
	prune := p.c.prune
	if prune != 0 {
		start, isStrong := max(-prune, 2), false
		if prune > 0 {
			start, isStrong = max(prune, 2), true
		}
		for z := start + 1; z <= p.lim; z++ {
			for _, m := range p.bySize[z] {
				all, anyBad := true, false
				subsets(m, func(s uint32) {
					good := p.ok(s)
					all = all && !good
					anyBad = anyBad || !good
				})
				if (!isStrong && all) || (isStrong && anyBad) {
					p.marked[m] = true
				}
			}
		}
	}
	if p.dir*p.worst() < p.thresh { // (dir<0 ? -1 : 0) < thresh
		emptyMarked = true
		for _, m := range p.bySize[1] {
			p.marked[m] = true
		}
	}
	tg := p.c.target
	isMax := tg == report.TargetMaximal
	if tg != report.TargetAll {
		// safe filtering: restore the subsets of unmarked sets, top down
		for z := p.lim; z >= 2; z-- {
			for _, m := range p.bySize[z] {
				if p.ok(m) {
					subsets(m, func(s uint32) { delete(p.marked, s) })
				}
			}
		}
		switch tg {
		case report.TargetGenerators:
			for z := 1; z <= p.lim; z++ {
				for _, m := range p.bySize[z] {
					if !p.ok(m) {
						continue
					}
					bad := false
					if z == 1 {
						bad = p.supp[m] >= p.d.total
					}
					subsets(m, func(s uint32) {
						if s != 0 && (!p.ok(s) || p.supp[s] <= p.supp[m]) {
							bad = true
						}
					})
					if bad {
						p.marked[m] = true
					}
				}
			}
		default:
			for _, m := range p.bySize[1] {
				if p.ok(m) && (isMax || p.supp[m] >= p.d.total) {
					emptyMarked = true
				}
			}
			// sets are decided against the unmarked supersets, which are
			// not changed by this step before their own level is reached
			n := len(p.d.names)
			for z := 1; z < p.lim; z++ {
				var mark []uint32
				for _, m := range p.bySize[z] {
					if !p.ok(m) {
						continue
					}
					for i := range n {
						t := m | 1<<i
						if t != m && p.ok(t) && (isMax || p.supp[t] >= p.supp[m]) {
							mark = append(mark, m)
							break
						}
					}
				}
				for _, m := range mark {
					p.marked[m] = true
				}
			}
		}
	}
	var out []oracle.Set
	report := func(m uint32, s tract.Supp, v float64) {
		z := bits.OnesCount32(m)
		if z < p.c.zmin || z > p.c.zmax || (p.c.smax > 0 && s > p.c.smax) || !(v*p.dir >= p.thresh) {
			return
		}
		names := make([]string, 0, z)
		for _, c := range codesOf(m) {
			names = append(names, p.d.names[c])
		}
		out = append(out, oracle.Set{Items: oracle.NewItemSet(names...), Supp: s, Eval: v})
	}
	if !emptyMarked {
		report(0, p.d.total, p.worst())
	}
	for z := 1; z <= p.lim; z++ {
		for _, m := range p.bySize[z] {
			if p.ok(m) {
				report(m, p.supp[m], p.eval(m))
			}
		}
	}
	return out
}

// TestFilter_PruneReference checks -p (backward, weak and strong forward,
// with the corrected call order of B2) for all item set targets against the
// set-based specification pruneRef.
func TestFilter_PruneReference(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	targets := []report.Target{report.TargetAll, report.TargetClosed, report.TargetMaximal, report.TargetGenerators}
	n := 600
	if testing.Short() {
		n = 100
	}
	total, changed := 0, 0
	defer func() { t.Logf("%d item sets compared, %d runs changed by -p", total, changed) }()
	for seed := range uint64(n) {
		txs := genTxs(t, seed, 4+int(seed%7))
		c := setConfig{
			target: targets[r.IntN(len(targets))],
			smin:   tract.Supp(1 + r.IntN(4)),
			zmin:   r.IntN(3),
			zmax:   []int{report.MaxSize, 2, 3}[r.IntN(3)],
			prune:  []int{-4, -3, -2, -1, 0, 1, 2, 3, 4}[r.IntN(9)],
			order:  r.IntN(5) - 2,
			eval: Eval{
				Measure:    randMeasure(r, false),
				Agg:        Aggregation(r.IntN(4)),
				Invalidate: r.IntN(4) == 0,
				Threshold:  thresholds[r.IntN(len(thresholds))],
			},
		}
		got, d := runSets(t, txs, c, false)
		if len(d.names) == 0 {
			continue
		}
		want := newPruneRef(d, c, c.treeZmax(d.maxLen)).run()
		total += len(want)
		if diff := oracle.DiffSets(want, got.sets, oracle.DiffOptions{}); diff != "" {
			t.Fatalf("seed %d config %+v codes %v:\n%s", seed, c, d.names, diff)
		}
		c.prune = noPrune
		plain, _ := runSets(t, txs, c, false)
		if oracle.DiffSets(plain.sets, got.sets, oracle.DiffOptions{}) != "" {
			changed++
		}
	}
	if changed == 0 {
		t.Error("-p never changed the result")
	}
}

// TestFilter_CallOrderB2 is the regression test for B2: Filter before
// SetEval (upstream's order) only marks infrequent sets and leaves the
// output unchanged, while the corrected order prunes.
func TestFilter_CallOrderB2(t *testing.T) {
	d := newDataset(t, t1, 2, 2, nil)
	e := Eval{Measure: ruleval.Lift, Threshold: 1.1}
	run := func(filterFirst, filter bool) []string {
		tr := New(Params{Freqs: d.freqs, Total: d.total, MinSupp: 2, MinBody: 2, MinConf: 1})
		tr.SetSize(1, d.maxLen) // maximal: zmax+1 capped at the largest transaction
		fillRecTree(tr, d.supp, 2)
		if filter && filterFirst {
			tr.Filter(0)
		}
		tr.SetEval(e)
		if filter && !filterFirst {
			tr.Filter(0)
		}
		tr.CloMax(report.TargetMaximal, filter)
		tr.Init()
		sink := &collect{names: d.names}
		rep := report.New(len(d.names), d.total, d.freqs)
		rep.SetTarget(report.TargetMaximal, report.ModeNoFilter, -1)
		rep.SetSupport(2, report.MaxSupp)
		rep.Setup()
		rep.SetSink(sink)
		if err := tr.Report(rep, report.TargetMaximal); err != nil {
			t.Fatal(err)
		}
		return sink.lines
	}
	plain := run(false, false)
	upstream := run(true, true)
	fixed := run(false, true)
	if len(plain) == 0 || !equalLines(plain, upstream) {
		t.Errorf("upstream order must not change the output:\n%v\n%v", plain, upstream)
	}
	if equalLines(plain, fixed) {
		t.Errorf("corrected order must prune: %v", fixed)
	}
}

// TestCloMax_GeneratorsTotalWeight is the regression test for the
// generator filter after Filter marked the empty set: upstream compares the
// items with the flagged total weight and reports no generator at all.
func TestCloMax_GeneratorsTotalWeight(t *testing.T) {
	c := setConfig{target: report.TargetGenerators, smin: 2, zmin: 1, zmax: report.MaxSize, prune: 0, order: 1,
		eval: Eval{Measure: ruleval.Lift, Threshold: 1.1}}
	got, d := runSets(t, t1, c, false)
	if len(got.sets) == 0 {
		t.Fatal("no generators reported with -p0")
	}
	want := newPruneRef(d, c, c.treeZmax(d.maxLen)).run()
	if diff := oracle.DiffSets(want, got.sets, oracle.DiffOptions{}); diff != "" {
		t.Error(diff)
	}
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestFilter_RulesUnaffected documents that evaluation pruning does not
// change rules: rule reporting reads the supports without the marks, like
// upstream's rules() (checked against the patched C build as well).
func TestFilter_RulesUnaffected(t *testing.T) {
	d := newDataset(t, t1, 2, 1, nil)
	run := func(prune int) []string {
		tr := New(Params{Freqs: d.freqs, Total: d.total, MinSupp: 2, MinBody: 2, MinConf: 0.3})
		tr.SetSize(1, d.maxLen)
		fillRecTree(tr, d.supp, 2)
		tr.SetEval(Eval{Measure: ruleval.Lift, Threshold: 1.1})
		if prune != noPrune {
			tr.Filter(prune)
		}
		tr.Init()
		sink := &collect{names: d.names}
		rep := report.New(len(d.names), d.total, d.freqs)
		rep.SetTarget(report.TargetRules, report.ModeNoFilter, -1)
		rep.SetSupport(2, report.MaxSupp)
		rep.Setup()
		rep.SetSink(sink)
		if err := tr.Report(rep, report.TargetRules); err != nil {
			t.Fatal(err)
		}
		return sink.lines
	}
	plain := run(noPrune)
	for _, p := range []int{0, -1, 2} {
		if got := run(p); !equalLines(plain, got) {
			t.Errorf("-p%d changed the rules", p)
		}
	}
}
