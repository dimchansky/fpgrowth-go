package tract_test

// Benchmarks of the two input passes (item counting and recoding, then
// building the transaction bag), without mining. The input is a
// deterministic oracle dataset held in memory as string slices.

import (
	"strconv"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// benchScan returns a scan function over 50,000 transactions (4 to 16
// items over the given number of items, Zipf skew 1).
func benchScan(tb testing.TB, items int) tract.ScanFunc[string] {
	tb.Helper()
	txs, err := oracle.Generate(oracle.GenConfig{
		Seed: 11, NumTransactions: 50000, NumItems: items, MinLen: 4, MaxLen: 16,
		Skew: 1, Names: oracle.NamesNumbers,
	})
	if err != nil {
		tb.Fatal(err)
	}
	return func(yield func([]string, tract.Supp) bool) error {
		for _, tx := range txs {
			if !yield(tx.Items, tract.Supp(tx.Weight)) {
				return nil
			}
		}
		return nil
	}
}

var benchUniverses = []struct {
	name  string
	items int
}{{"items=100", 100}, {"items=2000", 2000}}

// BenchmarkCountRecode is pass 1: counting the items into a new item base
// and recoding them (minimum support 10, upstream's default order).
func BenchmarkCountRecode(b *testing.B) {
	for _, u := range benchUniverses {
		scan := benchScan(b, u.items)
		b.Run(u.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				ib := tract.NewItemBase[string]()
				if err := ib.Count(scan); err != nil {
					b.Fatal(err)
				}
				ib.Recode(10, 2)
			}
		})
	}
}

// BenchmarkBuildBag is pass 1 plus pass 2: counting, recoding and
// building the transaction bag (prefix tree of the recoded transactions).
// The difference to BenchmarkCountRecode is the bag construction.
func BenchmarkBuildBag(b *testing.B) {
	for _, u := range benchUniverses {
		scan := benchScan(b, u.items)
		b.Run(u.name, func(b *testing.B) {
			b.ReportAllocs()
			var nodes int
			for b.Loop() {
				ib := tract.NewItemBase[string]()
				if err := ib.Count(scan); err != nil {
					b.Fatal(err)
				}
				ib.Recode(10, 2)
				bag, err := ib.BuildBag(scan, 1)
				if err != nil {
					b.Fatal(err)
				}
				nodes = bag.NodeCount()
			}
			b.ReportMetric(float64(nodes), "bag-nodes")
		})
	}
}

// BenchmarkBuildBagSelect is BenchmarkBuildBag with only 20 of 2000
// items selected (default appearance AppearNone): most identifiers of
// both passes are unknown and ignored.
func BenchmarkBuildBagSelect(b *testing.B) {
	scan := benchScan(b, 2000)
	sel := make([]string, 20)
	for i := range sel {
		sel[i] = strconv.Itoa(i)
	}
	b.ReportAllocs()
	for b.Loop() {
		ib := tract.NewItemBase[string]()
		if err := ib.Select(sel...); err != nil {
			b.Fatal(err)
		}
		if err := ib.Count(scan); err != nil {
			b.Fatal(err)
		}
		ib.Recode(10, 2)
		if _, err := ib.BuildBag(scan, 1); err != nil {
			b.Fatal(err)
		}
	}
}
