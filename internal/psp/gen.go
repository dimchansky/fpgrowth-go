package psp

import (
	"context"
	"errors"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Part is the work of one thread of fpg_genpsp: a chain of Count
// surrogate data sets generated with the seed Seed.
type Part struct {
	Seed  uint32
	Count int64
}

// Partition is fpg_genpsp's division of cnt surrogates among cpus threads
// for the base seed of option -S (after replacing 0 by a time-based seed):
// one chain with the base seed if cpus <= 1 or cnt <= 1, else
// c = ceil(cnt/cpus) data sets per thread, thread n getting
// min(c, cnt - n·c) of them (seed + n) while that is positive. The
// spectrum of fpgpsp depends on the seed and this partition only, never
// on how many goroutines compute the chains. cnt must be positive.
func Partition(seed, cnt int64, cpus int) []Part {
	if cpus <= 1 || cnt <= 1 {
		return []Part{{Seed: SeedOf(seed, 0), Count: cnt}}
	}
	c := cnt/int64(cpus) + min(cnt%int64(cpus), 1) // no overflow near MaxInt64
	var ps []Part
	for n := 0; n < cpus; n++ {
		x := cnt - int64(n)*c
		if x <= 0 {
			break
		}
		ps = append(ps, Part{Seed: SeedOf(seed, n), Count: min(x, c)})
	}
	return ps
}

// Spectrum returns an empty pattern spectrum with the limits that the
// item set reporter of fpg_genpsp uses (isr_addpsp after fpg_report):
// sizes [cfg.ZMin, cfg.ZMax] and supports [smin, smax] for the minimum
// and maximum support of the prepared data (fpg.Miner.MinSupport and
// MaxSupport).
func Spectrum(cfg fpg.Config, smin, smax tract.Supp) *report.Spectrum {
	return report.NewSpectrum(cfg.ZMin, cfg.ZMax, smin, smax)
}

// Workspace holds what the mining of one surrogate data set leaves for
// the next one of the same chain or worker: the item base and the
// prefix-tree bag (tract.DenseBase) and the miner's buffers
// (fpg.Workspace). Every data set is still counted, recoded and mined on
// its own (P2, P2b, P6), with the item codes, bag and miner that a new
// item base gives it, so the spectrum does not depend on the workspace;
// only the allocations of earlier data sets are reused. A Workspace is
// used by one goroutine at a time; its zero value is ready to use.
type Workspace struct {
	db tract.DenseBase
	ws fpg.Workspace
}

// Mine mines the item sets of one surrogate data set (fpg_data with
// FPG_SURR and fpg_mine of fpg_genpsp, with oracle patches P2 and P2b:
// every data set is mined on its own) and adds them to psp. The item
// codes are recoded for mining (D5: the spectrum does not depend on the
// codes); cfg is the configuration of the options and smin the minimum
// support of the prepared data, which has the same total weight. Mine
// uses a new Workspace; see Workspace.Mine.
func Mine(ctx context.Context, ts [][]tract.Item, cfg fpg.Config, smin tract.Supp, psp *report.Spectrum) error {
	return new(Workspace).Mine(ctx, ts, cfg, smin, psp)
}

// Mine is the package function Mine with the buffers of w.
func (w *Workspace) Mine(ctx context.Context, ts [][]tract.Item, cfg fpg.Config, smin tract.Supp, psp *report.Spectrum) error {
	n := 0 // the items are below n
	for _, t := range ts {
		for _, i := range t {
			n = max(n, int(i)+1)
		}
	}
	return w.mine(ctx, ts, n, cfg, smin, psp)
}

// mine is Mine for transactions over the items 0..n-1: the pipeline of a
// new item base over ts (ib.Count, fpg.New, m.Data, which rank the items
// in the order of their first occurrence in ts and recode them by their
// frequencies in ts), on the dense item base of the workspace.
func (w *Workspace) mine(ctx context.Context, ts [][]tract.Item, n int, cfg fpg.Config, smin tract.Supp, psp *report.Spectrum) error {
	if err := w.db.Count(ts, n); err != nil {
		return err
	}
	m, err := fpg.New[tract.Item](cfg)
	if err != nil {
		return err
	}
	if err := m.DataDense(&w.db); err != nil {
		if errors.Is(err, fpg.ErrNoItems) {
			// No item is frequent in this surrogate. Upstream mines the
			// unrecoded data and reports only the empty set, if it is
			// within the size range and frequent, and not for the maximal
			// target (a maximal set is not empty, internal/oracle).
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
	m.SetWorkspace(&w.ws)
	if err := m.Mine(ctx); err != nil {
		return err
	}
	psp.Merge(rep.Spectrum())
	return nil
}

// RunChain generates and mines the data sets of one part (one thread of
// fpg_genpsp) and returns their pattern spectrum. done, if not nil, is
// called after every data set. Cancellation of ctx is checked between
// data sets and inside mining. RunChain uses a new Workspace; see
// Workspace.RunChain.
func RunChain(ctx context.Context, src *Data, method Method, p Part, cfg fpg.Config,
	smin, smax tract.Supp, done func()) (*report.Spectrum, error) {
	return new(Workspace).RunChain(ctx, src, method, p, cfg, smin, smax, done)
}

// RunChain is the package function RunChain with the buffers of w, which
// a worker passes from one chain to the next.
func (w *Workspace) RunChain(ctx context.Context, src *Data, method Method, p Part, cfg fpg.Config,
	smin, smax tract.Supp, done func()) (*report.Spectrum, error) {
	psp := Spectrum(cfg, smin, smax)
	ch := NewChain(src, method, p.Seed)
	for range p.Count {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := w.mine(ctx, ch.Next(), src.NItems, cfg, smin, psp); err != nil {
			return nil, err
		}
		if done != nil {
			done()
		}
	}
	return psp, nil
}
