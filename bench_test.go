package fpgrowth

import (
	"context"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// benchDataset is the dataset of internal/fpg's BenchmarkMine: 3000
// weighted transactions over 40 items with planted patterns.
func benchDataset(tb testing.TB) []oracle.Transaction {
	tb.Helper()
	txs, err := oracle.Generate(oracle.GenConfig{
		Seed: 42, NumTransactions: 3000, NumItems: 40, MinLen: 3, MaxLen: 12,
		Skew: 1, Patterns: 8, PatternLen: 6, PatternProb: 0.6,
		DupTxProb: 0.05, Weights: oracle.WeightsSmall, Names: oracle.NamesLetters,
	})
	if err != nil {
		tb.Fatal(err)
	}
	return txs
}

// typedData is a dataset in both representations for item type T.
type typedData[T comparable] struct {
	items   [][]T
	weights []int64
}

func newTypedData[T comparable](txs []oracle.Transaction, conv func(string) T) typedData[T] {
	d := typedData[T]{items: make([][]T, len(txs)), weights: make([]int64, len(txs))}
	for i, tx := range txs {
		for _, it := range tx.Items {
			d.items[i] = append(d.items[i], conv(it))
		}
		d.weights[i] = int64(tx.Weight)
	}
	return d
}

func (d typedData[T]) scan(yield func([]T, tract.Supp) bool) error {
	for i, items := range d.items {
		if !yield(items, tract.Supp(d.weights[i])) {
			return nil
		}
	}
	return nil
}

func (d typedData[T]) source() Source[T] {
	txs := make([]Transaction[T], len(d.items))
	for i := range txs {
		txs[i] = Transaction[T]{Items: d.items[i], Weight: d.weights[i]}
	}
	return FromSlice(txs)
}

// countSink counts the reported sets (the internal baseline for a
// callback that does almost nothing).
type countSink struct{ n, items int }

func (s *countSink) ItemSet(is report.ItemSet) error { s.n++; s.items += len(is.Items); return nil }
func (s *countSink) Rule(report.Rule) error          { s.n++; return nil }

// runInternalBench runs the internal pipeline like the public API does.
func runInternalBench[T comparable](d typedData[T], cfg fpg.Config) (int, error) {
	sink := &countSink{}
	_, err := fpg.Mine(context.Background(), cfg, tract.NewItemBase[T](), d.scan, sink)
	return sink.n, err
}

// runPublicBench runs MineItemsets with a callback that does almost
// nothing.
func runPublicBench[T comparable](d typedData[T], src Source[T], opt Options[T]) (int, error) {
	n, items := 0, 0
	_, err := MineItemsets(context.Background(), src, opt, func(s Itemset[T]) error {
		n++
		items += len(s.Items)
		return nil
	})
	return n, err
}

var benchTargets = []struct {
	name   string
	target Target
}{{"frequent", TargetFrequent}, {"closed", TargetClosed}, {"maximal", TargetMaximal}}

func benchConfig(target Target) fpg.Config {
	c := fpg.DefaultConfig()
	c.Target = target.internal()
	c.SMin = -40
	return c
}

func benchTyped[T comparable](b *testing.B, name string, d typedData[T]) {
	src := d.source()
	for _, tg := range benchTargets {
		cfg := benchConfig(tg.target)
		opt := Options[T]{Target: tg.target, MinSupport: Count(40)}
		b.Run(name+"/"+tg.name+"/internal", func(b *testing.B) {
			b.ReportAllocs()
			var n int
			for b.Loop() {
				var err error
				if n, err = runInternalBench(d, cfg); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(n), "sets")
		})
		b.Run(name+"/"+tg.name+"/public", func(b *testing.B) {
			b.ReportAllocs()
			var n int
			for b.Loop() {
				var err error
				if n, err = runPublicBench(d, src, opt); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(n), "sets")
		})
	}
}

// BenchmarkMineItemsets compares the public API with the internal
// pipeline it wraps, for string and int items.
func BenchmarkMineItemsets(b *testing.B) {
	txs := benchDataset(b)
	benchTyped(b, "string", newTypedData(txs, func(s string) string { return s }))
	benchTyped(b, "int", newTypedData(txs, intOf))
}

// BenchmarkMineRules compares rule mining through the public API with the
// internal pipeline.
func BenchmarkMineRules(b *testing.B) {
	d := newTypedData(benchDataset(b), func(s string) string { return s })
	src := d.source()
	cfg := fpg.DefaultConfig()
	cfg.Target = report.TargetRules
	cfg.SMin, cfg.Conf = -100, 50
	opt := RuleOptions[string]{MinSupport: Count(100), MinConfidence: 0.5}
	b.Run("internal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := runInternalBench(d, cfg); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("public", func(b *testing.B) {
		b.ReportAllocs()
		var n int
		for b.Loop() {
			n = 0
			if _, err := MineRules(context.Background(), src, opt, func(Rule[string]) error { n++; return nil }); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(n), "rules")
	})
}

// intOf maps the generator's letter names to distinct ints.
func intOf(s string) int {
	n := 0
	for _, c := range s {
		n = n*27 + int(c-'a'+1)
	}
	return n
}

// TestAllocsPerResult checks that reporting results through the public
// API does not allocate per result: the allocation difference to the
// internal pipeline is a small constant while thousands of sets are
// reported.
func TestAllocsPerResult(t *testing.T) {
	txs := benchDataset(t)
	checkAllocs(t, "string", newTypedData(txs, func(s string) string { return s }))
	checkAllocs(t, "int", newTypedData(txs, intOf))

	d := newTypedData(txs, func(s string) string { return s })
	src := d.source()
	cfg := fpg.DefaultConfig()
	cfg.Target = report.TargetRules
	cfg.SMin, cfg.Conf = -100, 50
	var n int
	internal := testing.AllocsPerRun(3, func() {
		var err error
		if n, err = runInternalBench(d, cfg); err != nil {
			t.Fatal(err)
		}
	})
	public := testing.AllocsPerRun(3, func() {
		if _, err := MineRules(context.Background(), src, RuleOptions[string]{MinSupport: Count(100), MinConfidence: 0.5},
			func(Rule[string]) error { return nil }); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("rules: %d rules, allocations internal %.0f, public %.0f", n, internal, public)
	if n < 1000 || public-internal > 64 {
		t.Errorf("rules: %d rules, public API allocates %.0f more than the internal pipeline", n, public-internal)
	}
}

func checkAllocs[T comparable](t *testing.T, name string, d typedData[T]) {
	t.Helper()
	src := d.source()
	for _, tg := range benchTargets {
		var n int
		internal := testing.AllocsPerRun(3, func() {
			var err error
			if n, err = runInternalBench(d, benchConfig(tg.target)); err != nil {
				t.Fatal(err)
			}
		})
		public := testing.AllocsPerRun(3, func() {
			if _, err := runPublicBench(d, src, Options[T]{Target: tg.target, MinSupport: Count(40)}); err != nil {
				t.Fatal(err)
			}
		})
		t.Logf("%s/%s: %d sets, allocations internal %.0f, public %.0f", name, tg.name, n, internal, public)
		if n < 1000 || public-internal > 64 {
			t.Errorf("%s/%s: %d sets, public API allocates %.0f more than the internal pipeline",
				name, tg.name, n, public-internal)
		}
	}
}
