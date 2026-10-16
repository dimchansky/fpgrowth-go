package isrout_test

// Output-only benchmarks: formatting of item sets and rules by the CLI's
// Writer, without mining. The records are deterministic synthetic item
// sets and rules over 1,000 numerically named items.

import (
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/isrout"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

const benchItems = 1000

// countWriter counts the bytes written.
type countWriter struct{ n int64 }

func (w *countWriter) Write(p []byte) (int, error) { w.n += int64(len(p)); return len(p), nil }

func benchNames() []string {
	names := make([]string, benchItems)
	for i := range names {
		names[i] = strconv.Itoa(i)
	}
	return names
}

// benchSets returns 10,000 item sets of 1 to 8 items.
func benchSets() []report.ItemSet {
	r := rand.New(rand.NewPCG(1, 2))
	sets := make([]report.ItemSet, 10000)
	for k := range sets {
		items := make([]tract.Item, 1+r.IntN(8))
		for i := range items {
			items[i] = tract.Item(r.IntN(benchItems))
		}
		sets[k] = report.ItemSet{Items: items, Supp: tract.Supp(1 + r.IntN(5000)), Eval: r.Float64()}
	}
	return sets
}

// benchRules returns 10,000 rules with bodies of 1 to 5 items.
func benchRules() []report.Rule {
	r := rand.New(rand.NewPCG(3, 4))
	rules := make([]report.Rule, 10000)
	for k := range rules {
		body := make([]tract.Item, 1+r.IntN(5))
		for i := range body {
			body[i] = tract.Item(r.IntN(benchItems))
		}
		supp := tract.Supp(1 + r.IntN(2000))
		rules[k] = report.Rule{
			Head: tract.Item(r.IntN(benchItems)), Body: body, Supp: supp,
			BodySupp: supp + tract.Supp(r.IntN(2000)), HeadSupp: supp + tract.Supp(r.IntN(5000)), Eval: r.Float64(),
		}
	}
	return rules
}

// BenchmarkWriterItemSet writes 10,000 item sets per op with several
// information formats (" (%S)" is the CLI default for item sets: a
// relative support, formatted like C's printf %g).
func BenchmarkWriterItemSet(b *testing.B) {
	sets := benchSets()
	names := benchNames()
	for _, info := range []string{"", " (%a)", " (%S)", " (%a,%S,%e)"} {
		b.Run("info="+strconv.Quote(info), func(b *testing.B) {
			b.ReportAllocs()
			cw := &countWriter{}
			for b.Loop() {
				w := isrout.New(cw, isrout.Config{Names: names, Total: 20000,
					Format: isrout.Format{Sep: " ", Info: info}})
				for _, s := range sets {
					if err := w.ItemSet(s); err != nil {
						b.Fatal(err)
					}
				}
				if err := w.Close(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(sets)), "ns/set")
			b.ReportMetric(float64(cw.n)/float64(b.N), "outbytes/op")
		})
	}
}

// BenchmarkWriterRule writes 10,000 rules per op with the CLI's default
// rule format and with lift and evaluation.
func BenchmarkWriterRule(b *testing.B) {
	rules := benchRules()
	names := benchNames()
	for _, info := range []string{isrout.DefaultInfo(true, false), " (%a,%b,%c,%l,%e)"} {
		b.Run("info="+strconv.Quote(info), func(b *testing.B) {
			b.ReportAllocs()
			cw := &countWriter{}
			for b.Loop() {
				w := isrout.New(cw, isrout.Config{Names: names, Total: 20000,
					Format: isrout.Format{Sep: " ", Imp: " <- ", Info: info}})
				for _, r := range rules {
					if err := w.Rule(r); err != nil {
						b.Fatal(err)
					}
				}
				if err := w.Close(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(rules)), "ns/rule")
		})
	}
}
