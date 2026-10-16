package fpg

import (
	"context"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// BenchmarkMine runs the whole pipeline (count, recode, bag, mining) per
// variant and target on a generated dataset of 3000 transactions over 40
// items, counting the item sets without a sink.
func BenchmarkMine(b *testing.B) {
	txs := midDataset(b, 3000)
	scan := scanOf(txs)
	benchVariants := []variant{
		{name: "s-l0", algo: AlgoSimple, pack: 0},
		{name: "s-l16", algo: AlgoSimple, pack: 16},
		{name: "c", algo: AlgoComplex, pack: 16},
		{name: "c-j-l0", algo: AlgoComplex, pack: 0, clear: ModeReorder},
		{name: "d-l16", algo: AlgoSingle, pack: 16},
		{name: "t", algo: AlgoTopDown, pack: 0},
	}
	for _, tg := range []struct {
		name   string
		target report.Target
	}{{"s", report.TargetAll}, {"c", report.TargetClosed}, {"m", report.TargetMaximal}} {
		for _, v := range benchVariants {
			cfg := propCase{smin: -40, smax: 100, zmin: 1, zmax: MaxSize, sort: 2}.config(v, tg.target)
			b.Run(tg.name+"/"+v.name, func(b *testing.B) {
				b.ReportAllocs()
				var sets uint64
				for b.Loop() {
					rep, err := Mine(context.Background(), cfg, tract.NewItemBase[string](), scan, nil)
					if err != nil {
						b.Fatal(err)
					}
					sets = rep.Count()
				}
				b.ReportMetric(float64(sets), "sets")
			})
		}
	}
}
