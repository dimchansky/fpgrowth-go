package fpg

import (
	"errors"
	"slices"
	"testing"
)

// TestWorkspace_sameResults checks that mining with a Workspace reused
// across runs gives exactly the results of a fresh miner, in the same
// report order: one workspace serves every variant, target and option
// set of many random datasets in turn, so buffers, arenas and both
// 16-items machines come back from runs of other shapes, sizes and item
// loop directions.
func TestWorkspace_sameResults(t *testing.T) {
	t.Parallel()
	var ws Workspace
	runs := 0
	for seed := range uint64(150) {
		c, err := genCase(1000+seed, 24)
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range targets {
			for _, v := range variants {
				cfg := c.config(v, tg.target)
				want, werr := run(scanOf(c.txs), cfg, runOpts{})
				got, gerr := run(scanOf(c.txs), cfg, runOpts{ws: &ws})
				if !errors.Is(gerr, werr) {
					t.Fatalf("seed %d %s/%s: error %v, want %v", c.seed, tg.name, v.name, gerr, werr)
				}
				if !slices.Equal(got.sink.lines, want.sink.lines) {
					t.Fatalf("seed %d %s/%s %s: reported\n%v\nwant\n%v", c.seed, tg.name, v.name,
						describe(cfg), got.sink.lines, want.sink.lines)
				}
				runs++
			}
		}
	}
	if ws.running {
		t.Fatal("workspace still marked as running")
	}
	t.Logf("%d runs compared", runs)
}

// TestWorkspace_afterError checks that a workspace whose run stopped
// with an error (here the sink's) still gives the results of a fresh
// miner afterwards: a 16-items machine or an arena left in the middle of
// a run is reset.
func TestWorkspace_afterError(t *testing.T) {
	var ws Workspace
	txs := midDataset(t, 120)
	for _, v := range variants {
		for _, tg := range targets {
			cfg := DefaultConfig()
			cfg.Target, cfg.Algo, cfg.Pack, cfg.SMin = tg.target, v.algo, v.pack, -12
			cfg.Mode &^= v.clear
			if _, err := run(scanOf(txs), cfg, runOpts{ws: &ws, sink: &collectSink{failAt: 50}}); !errors.Is(err, errSink) {
				t.Fatalf("%s/%s: error %v, want the sink's", tg.name, v.name, err)
			}
			want, err := run(scanOf(txs), cfg, runOpts{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := run(scanOf(txs), cfg, runOpts{ws: &ws})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.sink.lines, want.sink.lines) {
				t.Fatalf("%s/%s: %d sets after an error, want %d", tg.name, v.name, len(got.sink.lines), len(want.sink.lines))
			}
		}
	}
}
