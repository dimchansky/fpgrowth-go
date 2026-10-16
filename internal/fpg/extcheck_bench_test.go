package fpg

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// denseTxs returns n random transactions over k items, each item present
// with probability d (independently, fixed seed).
func denseTxs(n, k int, d float64, seed uint64) [][]string {
	r := rand.New(rand.NewPCG(seed, 1))
	txs := make([][]string, n)
	for i := range txs {
		for j := range k {
			if r.Float64() < d {
				txs[i] = append(txs[i], fmt.Sprintf("i%d", j))
			}
		}
	}
	return txs
}

// benchMine mines txs with cfg in count-only mode.
func benchMine(b *testing.B, txs [][]string, cfg Config) {
	b.Helper()
	scan := func(yield func([]string, tract.Supp) bool) error {
		for _, t := range txs {
			if !yield(t, 1) {
				return nil
			}
		}
		return nil
	}
	b.ReportAllocs()
	for b.Loop() {
		ib := tract.NewItemBase[string]()
		if _, err := Mine(context.Background(), cfg, ib, scan, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCloMaxLimited measures closed and maximal item set mining with
// a small maximum size on dense data (deviation M5).
func BenchmarkCloMaxLimited(b *testing.B) {
	data := []struct {
		name string
		txs  [][]string
		smin float64
	}{
		{"dense2000x40d60", denseTxs(2000, 40, 0.6, 1), 2},
		{"dense1000x30d80", denseTxs(1000, 30, 0.8, 2), 10},
	}
	for _, d := range data {
		for _, tg := range []report.Target{report.TargetClosed, report.TargetMaximal} {
			for _, zmax := range []int{1, 2, 3} {
				name := fmt.Sprintf("%s/t%v/n%d", d.name, map[report.Target]string{
					report.TargetClosed: "c", report.TargetMaximal: "m"}[tg], zmax)
				b.Run(name, func(b *testing.B) {
					cfg := DefaultConfig()
					cfg.Target, cfg.SMin, cfg.ZMax = tg, d.smin, zmax
					benchMine(b, d.txs, cfg)
				})
			}
		}
	}
}
