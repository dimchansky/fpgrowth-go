package psp

import (
	"context"
	"errors"
	"math/rand/v2"
	"runtime"
	"slices"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// mineRef is Mine as it was before issue #39: a new item base, counted
// and recoded through the generic pipeline (map-based ItemBase), a new
// bag and a new miner for every data set.
func mineRef(ctx context.Context, ts [][]tract.Item, cfg fpg.Config, smin tract.Supp, psp *report.Spectrum) error {
	scan := func(yield func([]tract.Item, tract.Supp) bool) error {
		for _, t := range ts {
			if !yield(t, 1) {
				break
			}
		}
		return nil
	}
	ib := tract.NewItemBase[tract.Item]()
	if err := ib.Count(scan); err != nil {
		return err
	}
	m, err := fpg.New[tract.Item](cfg)
	if err != nil {
		return err
	}
	if err := m.Data(ib, scan); err != nil {
		if errors.Is(err, fpg.ErrNoItems) {
			if total := tract.Supp(len(ts)); smin <= total && cfg.Target != report.TargetMaximal {
				psp.Add(0, total, 1)
			}
			return nil
		}
		return err
	}
	rep, err := m.Reporter()
	if err != nil {
		return err
	}
	rep.AddSpectrum()
	rep.SetSink(nil)
	if err := m.Mine(ctx); err != nil {
		return err
	}
	psp.Merge(rep.Spectrum())
	return nil
}

// cells returns the cells of a spectrum.
func cells(p *report.Spectrum) []report.Cell { return slices.Collect(p.All()) }

// TestWorkspace_matchesFreshPipeline checks that chains mined with one
// Workspace, reused across chains of every method, target, variant,
// size range and support, give the spectra of the pipeline before issue
// #39 (mineRef), cell by cell.
func TestWorkspace_matchesFreshPipeline(t *testing.T) {
	r := rand.New(rand.NewPCG(39, 5))
	var ws Workspace
	algos := []fpg.Algorithm{fpg.AlgoComplex, fpg.AlgoSimple, fpg.AlgoSingle, fpg.AlgoTopDown}
	targets := []report.Target{report.TargetAll, report.TargetClosed, report.TargetMaximal, report.TargetGenerators}
	compared := 0
	for it := range 160 {
		n := 2 + r.IntN(24)
		d := randData(r, n, 5+r.IntN(80))
		method := []Method{Identity, Random, Swap, Swap}[r.IntN(4)]
		cfg := fpg.DefaultConfig()
		cfg.Target = targets[r.IntN(len(targets))]
		cfg.Algo = algos[r.IntN(len(algos))]
		cfg.SMin = -float64(1 + r.IntN(8))
		cfg.ZMin = r.IntN(3)
		if r.IntN(3) == 0 {
			cfg.ZMax = cfg.ZMin + r.IntN(4)
		}
		if r.IntN(4) == 0 {
			cfg.Mode &^= fpg.ModePerfect
		}
		if r.IntN(4) == 0 {
			cfg.Pack = r.IntN(17)
		}
		cfg.Sort = []int{2, 1, 0, -1, -2}[r.IntN(5)]
		smin, smax := fpg.Thresholds(cfg, tract.Supp(d.Len()))
		seed := r.Uint32()
		count := 1 + r.IntN(5)
		got, err := ws.RunChain(context.Background(), d, method, Part{Seed: seed, Count: int64(count)}, cfg, smin, smax, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := Spectrum(cfg, smin, smax)
		ch := NewChain(d, method, seed)
		for range count {
			if err := mineRef(context.Background(), ch.Next(), cfg, smin, want); err != nil {
				t.Fatal(err)
			}
		}
		if g, w := cells(got), cells(want); !slices.Equal(g, w) {
			t.Fatalf("case %d (method %d, %+v): spectrum\n%v\nwant\n%v", it, method, cfg, g, w)
		}
		compared += count
	}
	t.Logf("%d surrogates compared", compared)
}

// TestWorkspace_allocations bounds the memory a chain allocates per
// surrogate once its workspace is warm: about 3.3 MB on this data
// before issue #39 (item base, prefix tree, FP-tree arena and 16-items
// machine allocated for every surrogate), about 0.1 MB with the
// workspace (the reporter and the spectrum of a surrogate, and the
// chain's own buffers).
func TestWorkspace_allocations(t *testing.T) {
	d := benchData()
	cfg := fpg.DefaultConfig()
	cfg.SMin = -60
	smin, smax := fpg.Thresholds(cfg, tract.Supp(d.Len()))
	var ws Workspace
	run := func() {
		if _, err := ws.RunChain(context.Background(), d, Swap, Part{Seed: 1, Count: 10}, cfg, smin, smax, nil); err != nil {
			t.Fatal(err)
		}
	}
	run() // warm the workspace
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	run()
	runtime.ReadMemStats(&after)
	perSurrogate := (after.TotalAlloc - before.TotalAlloc) / 10
	t.Logf("%d bytes per surrogate", perSurrogate)
	if perSurrogate > 400<<10 {
		t.Errorf("%d bytes allocated per surrogate, want at most %d", perSurrogate, 400<<10)
	}
}
