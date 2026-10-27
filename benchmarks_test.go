package fpgrowth

// Benchmark suite of the public API (Stage 3 baseline). The datasets are
// the deterministic ones of benchgen_test.go (shared with the C comparison
// in reference/bench); see docs/benchmarks.md for their characteristics,
// the commands and the recorded results.
//
// Every benchmark reports the number of results as a custom metric
// ("sets" or "rules") so that a change of the result count (which would
// invalidate a comparison) is visible next to the timing. Thresholds are
// chosen so that no benchmark reports more than about 1.3 million results.
//
// Nothing here runs without -bench; datasets are generated in memory on
// first use and files are written to b.TempDir().

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

var (
	suiteMu    sync.Mutex
	suiteCache = map[string][]Transaction[string]{}
)

// suiteData returns the transactions of the named dataset (item names are
// decimal indices, as in the files), generated once per process.
func suiteData(tb testing.TB, name string) []Transaction[string] {
	tb.Helper()
	suiteMu.Lock()
	defer suiteMu.Unlock()
	if txs, ok := suiteCache[name]; ok {
		return txs
	}
	spec, ok := benchSpecByName(name)
	if !ok {
		tb.Fatalf("unknown dataset %q", name)
	}
	names := make([]string, spec.Items)
	for i := range names {
		names[i] = strconv.Itoa(i)
	}
	txs := make([]Transaction[string], 0, spec.Transactions)
	err := benchGenerate(spec, func(tx benchTx) error {
		items := make([]string, len(tx.Items))
		for k, i := range tx.Items {
			items[k] = names[i]
		}
		txs = append(txs, Transaction[string]{Items: items, Weight: tx.Weight})
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	suiteCache[name] = txs
	return txs
}

// suiteInts converts a dataset to int items.
func suiteInts(txs []Transaction[string]) []Transaction[int] {
	out := make([]Transaction[int], len(txs))
	for t, tx := range txs {
		items := make([]int, len(tx.Items))
		for k, s := range tx.Items {
			items[k], _ = strconv.Atoi(s)
		}
		out[t] = Transaction[int]{Items: items, Weight: tx.Weight}
	}
	return out
}

// suiteFile writes the named dataset to dir in the table format (csv
// false) or as CSV and returns the path and the file size.
func suiteFile(tb testing.TB, dir, name string, csv bool) (string, int64) {
	tb.Helper()
	spec, ok := benchSpecByName(name)
	if !ok {
		tb.Fatalf("unknown dataset %q", name)
	}
	ext := ".tab"
	if csv {
		ext = ".csv"
	}
	path := filepath.Join(dir, name+ext)
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	if err := benchWrite(f, spec, csv); err != nil {
		tb.Fatal(err)
	}
	if err := f.Close(); err != nil {
		tb.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		tb.Fatal(err)
	}
	return path, fi.Size()
}

// suiteCount mines item sets with a callback that only counts.
func suiteCount[T comparable](b *testing.B, src Source[T], opt Options[T]) uint64 {
	b.Helper()
	var n uint64
	if _, err := MineItemsets(context.Background(), src, opt, func(Itemset[T]) error { n++; return nil }); err != nil {
		b.Fatal(err)
	}
	return n
}

// suiteLoop runs fn in a b.Loop and reports its last result as metric.
func suiteLoop(b *testing.B, metric string, fn func() uint64) {
	b.ReportAllocs()
	var n uint64
	for b.Loop() {
		n = fn()
	}
	b.ReportMetric(float64(n), metric)
}

func pct(p float64) Support { return Fraction(p / 100) }

var suiteTargets = []struct {
	name   string
	target Target
}{
	{"frequent", TargetFrequent},
	{"closed", TargetClosed},
	{"maximal", TargetMaximal},
	{"generators", TargetGenerators},
}

// BenchmarkSuiteTargets mines every target on every dataset at two
// thresholds (public API, in-memory source, counting callback).
func BenchmarkSuiteTargets(b *testing.B) {
	cases := []struct {
		data       string
		thresholds []float64 // percent
	}{
		{"sparse", []float64{0.5, 0.1}},
		{"dense", []float64{10, 5}},
		{"long", []float64{3, 2.5}},
		{"weighted", []float64{0.2}},
	}
	for _, c := range cases {
		src := FromSlice(suiteData(b, c.data))
		for _, s := range c.thresholds {
			for _, tg := range suiteTargets {
				opt := Options[string]{Target: tg.target, MinSupport: pct(s)}
				b.Run(fmt.Sprintf("%s/s=%g/%s", c.data, s, tg.name), func(b *testing.B) {
					suiteLoop(b, "sets", func() uint64 { return suiteCount(b, src, opt) })
				})
			}
		}
	}
}

// BenchmarkSuiteMaxSize caps an otherwise exploding output with MaxSize
// (dense data at 2 % support), and mines maximal sets of a small maximum
// size at 5 % (issue #42: vertical checks, sibling rows of few children).
func BenchmarkSuiteMaxSize(b *testing.B) {
	src := FromSlice(suiteData(b, "dense"))
	for _, tg := range suiteTargets[:3] {
		for _, z := range []int{3, 5} {
			opt := Options[string]{Target: tg.target, MinSupport: pct(2), MaxSize: z}
			b.Run(fmt.Sprintf("dense/s=2/%s/n=%d", tg.name, z), func(b *testing.B) {
				suiteLoop(b, "sets", func() uint64 { return suiteCount(b, src, opt) })
			})
		}
	}
	for _, z := range []int{3, 4} {
		opt := Options[string]{Target: TargetMaximal, MinSupport: pct(5), MaxSize: z}
		b.Run(fmt.Sprintf("dense/s=5/maximal/n=%d", z), func(b *testing.B) {
			suiteLoop(b, "sets", func() uint64 { return suiteCount(b, src, opt) })
		})
	}
}

// suiteVariants are the FP-growth variants (upstream -A, -l, -j).
var suiteVariants = []struct {
	name   string
	algo   Algorithm
	tuning Tuning
}{
	{"simple", AlgorithmSimple, Tuning{ItemMachine: -1}},   // -As -l0
	{"simple+fim16", AlgorithmSimple, Tuning{}},            // -As
	{"complex", AlgorithmComplex, Tuning{NoReorder: true}}, // -Ac -j
	{"complex+reorder", AlgorithmComplex, Tuning{}},        // -Ac (default)
	{"single", AlgorithmSingleTree, Tuning{}},              // -Ad
	{"topdown", AlgorithmTopDown, Tuning{}},                // -At
}

// BenchmarkSuiteVariants compares the variants on dense data (frequent,
// closed and maximal sets) and on sparse data (frequent sets).
func BenchmarkSuiteVariants(b *testing.B) {
	cases := []struct {
		data   string
		s      float64
		target []int // indices into suiteTargets
	}{
		{"dense", 10, []int{0, 1, 2}},
		{"sparse", 0.1, []int{0}},
	}
	for _, c := range cases {
		src := FromSlice(suiteData(b, c.data))
		for _, ti := range c.target {
			tg := suiteTargets[ti]
			for _, v := range suiteVariants {
				opt := Options[string]{Target: tg.target, MinSupport: pct(c.s), Algorithm: v.algo, Tuning: v.tuning}
				b.Run(fmt.Sprintf("%s/s=%g/%s/%s", c.data, c.s, tg.name, v.name), func(b *testing.B) {
					suiteLoop(b, "sets", func() uint64 { return suiteCount(b, src, opt) })
				})
			}
		}
	}
}

// BenchmarkSuiteRules mines association rules, without and with an
// evaluation measure (lift, filtered at 1.5; on dense data also the
// information, its G-test p-value and Fisher's test, filtered at 0.01).
func BenchmarkSuiteRules(b *testing.B) {
	lift := &Evaluation{Measure: Lift, Filter: true, Threshold: 1.5}
	info := &Evaluation{Measure: Information, Filter: true, Threshold: 0.01}
	infop := &Evaluation{Measure: InformationPValue, Filter: true, Threshold: 0.01}
	fisher := &Evaluation{Measure: FisherProb, Filter: true, Threshold: 0.01}
	cases := []struct {
		name string
		data string
		opt  RuleOptions[string]
	}{
		{"sparse/s=0.1/c=60", "sparse", RuleOptions[string]{MinSupport: pct(0.1), MinConfidence: 0.6}},
		{"sparse/s=0.05/c=50/lift", "sparse", RuleOptions[string]{MinSupport: pct(0.05), MinConfidence: 0.5, Evaluation: lift}},
		{"dense/s=10/c=90/n=4", "dense", RuleOptions[string]{MinSupport: pct(10), MinConfidence: 0.9, MaxSize: 4}},
		{"dense/s=10/c=80/n=4/lift", "dense", RuleOptions[string]{MinSupport: pct(10), MinConfidence: 0.8, MaxSize: 4, Evaluation: lift}},
		{"dense/s=10/c=80/n=4/info", "dense", RuleOptions[string]{MinSupport: pct(10), MinConfidence: 0.8, MaxSize: 4, Evaluation: info}},
		{"dense/s=10/c=80/n=4/infop", "dense", RuleOptions[string]{MinSupport: pct(10), MinConfidence: 0.8, MaxSize: 4, Evaluation: infop}},
		{"dense/s=10/c=80/n=4/fisher", "dense", RuleOptions[string]{MinSupport: pct(10), MinConfidence: 0.8, MaxSize: 4, Evaluation: fisher}},
	}
	for _, c := range cases {
		src := FromSlice(suiteData(b, c.data))
		b.Run(c.name, func(b *testing.B) {
			suiteLoop(b, "rules", func() uint64 {
				var n uint64
				if _, err := MineRules(context.Background(), src, c.opt, func(Rule[string]) error { n++; return nil }); err != nil {
					b.Fatal(err)
				}
				return n
			})
		})
	}
}

// BenchmarkSuiteSources mines the same data from an in-memory slice
// (string and int items), a table file (TableFile) and a CSV file
// (CSVFile). File sources report the file size as bytes/op (MB/s).
func BenchmarkSuiteSources(b *testing.B) {
	cases := []struct {
		data string
		s    float64
	}{
		{"sparse", 0.1},
		{"sparse-large", 0.05},
		{"weighted", 0.2},
	}
	for _, c := range cases {
		txs := suiteData(b, c.data)
		weighted := c.data == "weighted"
		dir := b.TempDir()
		tab, tabSize := suiteFile(b, dir, c.data, false)
		csv, csvSize := suiteFile(b, dir, c.data, true)
		tf := DefaultTableFormat()
		tf.Weighted = weighted
		cf := CSVFormat{}
		if weighted {
			cf.WeightColumn = -1
		}
		opt := Options[string]{MinSupport: pct(c.s)}
		prefix := fmt.Sprintf("%s/s=%g/", c.data, c.s)
		b.Run(prefix+"memory-string", func(b *testing.B) {
			src := FromSlice(txs)
			suiteLoop(b, "sets", func() uint64 { return suiteCount(b, src, opt) })
		})
		b.Run(prefix+"memory-int", func(b *testing.B) {
			src := FromSlice(suiteInts(txs))
			iopt := Options[int]{MinSupport: pct(c.s)}
			suiteLoop(b, "sets", func() uint64 { return suiteCount(b, src, iopt) })
		})
		b.Run(prefix+"table-file", func(b *testing.B) {
			b.SetBytes(tabSize)
			src := TableFile(tab, tf)
			suiteLoop(b, "sets", func() uint64 { return suiteCount(b, src, opt) })
		})
		b.Run(prefix+"csv-file", func(b *testing.B) {
			b.SetBytes(csvSize)
			src := CSVFile(csv, cf)
			suiteLoop(b, "sets", func() uint64 { return suiteCount(b, src, opt) })
		})
	}
}

// BenchmarkSuiteReadOnly measures the two passes over a source without
// mining: the minimum support is above every item's frequency, so nothing
// is frequent (parsing, item dictionary, transaction bag of empty
// transactions). File sources report MB/s.
func BenchmarkSuiteReadOnly(b *testing.B) {
	for _, data := range []string{"sparse-large", "long"} {
		txs := suiteData(b, data)
		dir := b.TempDir()
		tab, tabSize := suiteFile(b, dir, data, false)
		csv, csvSize := suiteFile(b, dir, data, true)
		opt := Options[string]{MinSupport: pct(100)}
		run := func(b *testing.B, src Source[string], size int64) {
			if size > 0 {
				b.SetBytes(size)
			}
			b.ReportAllocs()
			for b.Loop() {
				sum, err := MineItemsets(context.Background(), src, opt, nil)
				if err != nil {
					b.Fatal(err)
				}
				if sum.Reported != 0 {
					b.Fatalf("%d sets reported", sum.Reported)
				}
			}
		}
		b.Run(data+"/memory-string", func(b *testing.B) { run(b, FromSlice(txs), 0) })
		b.Run(data+"/table-file", func(b *testing.B) { run(b, TableFile(tab, DefaultTableFormat()), tabSize) })
		b.Run(data+"/csv-file", func(b *testing.B) { run(b, CSVFile(csv, CSVFormat{}), csvSize) })
	}
}

// BenchmarkSuiteConsume compares the ways of consuming results: callback
// (MineItemsets), iterator (Itemsets), collecting (CollectItemsets), and
// the internal pipeline without a sink (algorithm only: no conversion to
// the public types, no callback).
func BenchmarkSuiteConsume(b *testing.B) {
	cases := []struct {
		data string
		s    float64
		tg   Target
	}{
		{"dense", 5, TargetFrequent},
		{"dense", 5, TargetClosed},
		{"sparse", 0.1, TargetFrequent},
	}
	for _, c := range cases {
		txs := suiteData(b, c.data)
		src := FromSlice(txs)
		opt := Options[string]{Target: c.tg, MinSupport: pct(c.s)}
		prefix := fmt.Sprintf("%s/s=%g/%s/", c.data, c.s, suiteTargetName(c.tg))
		b.Run(prefix+"callback", func(b *testing.B) {
			suiteLoop(b, "sets", func() uint64 { return suiteCount(b, src, opt) })
		})
		b.Run(prefix+"iterator", func(b *testing.B) {
			suiteLoop(b, "sets", func() uint64 {
				var n uint64
				for _, err := range Itemsets(context.Background(), src, opt) {
					if err != nil {
						b.Fatal(err)
					}
					n++
				}
				return n
			})
		})
		b.Run(prefix+"collect", func(b *testing.B) {
			suiteLoop(b, "sets", func() uint64 {
				sets, _, err := CollectItemsets(context.Background(), src, opt)
				if err != nil {
					b.Fatal(err)
				}
				return uint64(len(sets))
			})
		})
		b.Run(prefix+"internal-nosink", func(b *testing.B) {
			cfg := fpg.DefaultConfig()
			cfg.Target = c.tg.internal()
			cfg.SMin = c.s
			scan := suiteScan(txs)
			suiteLoop(b, "sets", func() uint64 {
				rep, err := fpg.Mine(context.Background(), cfg, tract.NewItemBase[string](), scan, nil)
				if err != nil {
					b.Fatal(err)
				}
				return rep.Count()
			})
		})
	}
	// rules
	txs := suiteData(b, "dense")
	src := FromSlice(txs)
	ropt := RuleOptions[string]{MinSupport: pct(10), MinConfidence: 0.9, MaxSize: 4}
	b.Run("dense/s=10/c=90/n=4/rules/callback", func(b *testing.B) {
		suiteLoop(b, "rules", func() uint64 {
			var n uint64
			if _, err := MineRules(context.Background(), src, ropt, func(Rule[string]) error { n++; return nil }); err != nil {
				b.Fatal(err)
			}
			return n
		})
	})
	b.Run("dense/s=10/c=90/n=4/rules/iterator", func(b *testing.B) {
		suiteLoop(b, "rules", func() uint64 {
			var n uint64
			for _, err := range Rules(context.Background(), src, ropt) {
				if err != nil {
					b.Fatal(err)
				}
				n++
			}
			return n
		})
	})
	b.Run("dense/s=10/c=90/n=4/rules/collect", func(b *testing.B) {
		suiteLoop(b, "rules", func() uint64 {
			rules, _, err := CollectRules(context.Background(), src, ropt)
			if err != nil {
				b.Fatal(err)
			}
			return uint64(len(rules))
		})
	})
	b.Run("dense/s=10/c=90/n=4/rules/internal-nosink", func(b *testing.B) {
		cfg := fpg.DefaultConfig()
		cfg.Target = report.TargetRules
		cfg.SMin, cfg.ConfFrac, cfg.ZMax = 10, 0.9, 4
		scan := suiteScan(txs)
		suiteLoop(b, "rules", func() uint64 {
			rep, err := fpg.Mine(context.Background(), cfg, tract.NewItemBase[string](), scan, nil)
			if err != nil {
				b.Fatal(err)
			}
			return rep.Count()
		})
	})
}

// suiteScan adapts a dataset to the internal scan function.
func suiteScan(txs []Transaction[string]) tract.ScanFunc[string] {
	return func(yield func([]string, tract.Supp) bool) error {
		for _, tx := range txs {
			if !yield(tx.Items, tract.Supp(tx.Weight)) {
				return nil
			}
		}
		return nil
	}
}

// suiteTargetName is a short target name for benchmark names.
func suiteTargetName(t Target) string {
	for _, tg := range suiteTargets {
		if tg.target == t {
			return tg.name
		}
	}
	return t.String()
}
