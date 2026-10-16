package fpgrowth_test

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	fpgrowth "github.com/dimchansky/fpgrowth-go"
)

var baskets = [][]string{
	{"bread", "milk"},
	{"bread", "diapers", "beer", "eggs"},
	{"milk", "diapers", "beer", "cola"},
	{"bread", "milk", "diapers", "beer"},
	{"bread", "milk", "diapers", "cola"},
}

// Mine the frequent item sets of a few market baskets: every set of items
// that occurs in at least 60% of them.
func Example() {
	src := fpgrowth.FromItems(baskets)
	opt := fpgrowth.Options[string]{MinSupport: fpgrowth.Fraction(0.6)}
	for set, err := range fpgrowth.Itemsets(context.Background(), src, opt) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(set.Items, set.Support)
	}
	// Output:
	// [bread] 4
	// [milk bread] 3
	// [milk] 4
	// [diapers milk] 3
	// [diapers bread] 3
	// [diapers] 4
	// [beer diapers] 3
	// [beer] 3
}

// Items can be any comparable type, here product numbers. MineItemsets
// calls a function for every set and returns statistics.
func ExampleMineItemsets() {
	src := fpgrowth.FromItems([][]int{{1, 2, 3}, {1, 2}, {2, 3}, {1, 2, 3}, {4}})
	sum, err := fpgrowth.MineItemsets(context.Background(), src,
		fpgrowth.Options[int]{MinSupport: fpgrowth.Count(2), MinSize: 2},
		func(s fpgrowth.Itemset[int]) error {
			fmt.Println(s.Items, s.Support)
			return nil
		})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d transactions, %d frequent items, %d sets by size %v\n",
		sum.Transactions, sum.FrequentItems, sum.Reported, sum.BySize)
	// Output:
	// [1 2] 3
	// [3 1 2] 2
	// [3 1] 2
	// [3 2] 3
	// 5 transactions, 3 frequent items, 4 sets by size [0 0 3 1]
}

// Weighted transactions stand for several identical ones; supports are
// sums of weights.
func ExampleFromSlice() {
	src := fpgrowth.FromSlice([]fpgrowth.Transaction[string]{
		{Items: []string{"tea", "lemon"}, Weight: 30},
		{Items: []string{"tea", "milk"}, Weight: 50},
		{Items: []string{"coffee", "milk"}, Weight: 20},
	})
	sets, sum, err := fpgrowth.CollectItemsets(context.Background(), src,
		fpgrowth.Options[string]{MinSupport: fpgrowth.Fraction(0.25)})
	if err != nil {
		log.Fatal(err)
	}
	for _, s := range sets {
		fmt.Printf("%v %d (%.0f%%)\n", s.Items, s.Support, 100*float64(s.Support)/float64(sum.TotalWeight))
	}
	// Output:
	// [tea] 80 (80%)
	// [milk tea] 50 (50%)
	// [milk] 70 (70%)
	// [lemon tea] 30 (30%)
	// [lemon] 30 (30%)
}

// Closed item sets have no superset with the same support, maximal ones
// no frequent superset; both summarize all frequent item sets.
func ExampleTarget() {
	src := fpgrowth.FromItems(baskets)
	for _, target := range []fpgrowth.Target{fpgrowth.TargetClosed, fpgrowth.TargetMaximal} {
		sets, _, err := fpgrowth.CollectItemsets(context.Background(), src,
			fpgrowth.Options[string]{Target: target, MinSupport: fpgrowth.Count(3)})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(target, sets)
	}
	// Output:
	// TargetClosed [{[beer diapers] 3 NaN} {[bread milk] 3 NaN} {[bread diapers] 3 NaN} {[bread] 4 NaN} {[milk diapers] 3 NaN} {[milk] 4 NaN} {[diapers] 4 NaN}]
	// TargetMaximal [{[beer diapers] 3 NaN} {[bread milk] 3 NaN} {[bread diapers] 3 NaN} {[milk diapers] 3 NaN}]
}

// Association rules with a minimum confidence; every rule has its
// confidence and lift.
func ExampleRules() {
	src := fpgrowth.FromItems(baskets)
	opt := fpgrowth.RuleOptions[string]{
		MinSupport:    fpgrowth.Fraction(0.6), // of the body
		MinConfidence: 0.75,
		MinSize:       2, // no rules with an empty body
	}
	for r, err := range fpgrowth.Rules(context.Background(), src, opt) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%v -> %s  support %d  confidence %.2f  lift %.2f\n",
			r.Body, r.Head, r.Support, r.Confidence, r.Lift)
	}
	// Output:
	// [milk] -> bread  support 3  confidence 0.75  lift 0.94
	// [bread] -> milk  support 3  confidence 0.75  lift 0.94
	// [diapers] -> bread  support 3  confidence 0.75  lift 0.94
	// [bread] -> diapers  support 3  confidence 0.75  lift 0.94
	// [diapers] -> milk  support 3  confidence 0.75  lift 0.94
	// [milk] -> diapers  support 3  confidence 0.75  lift 0.94
	// [beer] -> diapers  support 3  confidence 1.00  lift 1.25
	// [diapers] -> beer  support 3  confidence 0.75  lift 1.25
}

// An evaluation measure fills Value and, with Filter, filters the
// results: here only rules whose lift is at least 1.2.
func ExampleEvaluation() {
	src := fpgrowth.FromItems(baskets)
	rules, _, err := fpgrowth.CollectRules(context.Background(), src, fpgrowth.RuleOptions[string]{
		MinSupport:      fpgrowth.Count(2),
		OriginalSupport: true, // MinSupport applies to body and head together
		MinSize:         2,
		Evaluation:      &fpgrowth.Evaluation{Measure: fpgrowth.Lift, Filter: true, Threshold: 1.2},
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, r := range rules {
		fmt.Printf("%v -> %s  lift %.3f\n", r.Body, r.Head, r.Value)
	}
	// Output:
	// [beer bread] -> diapers  lift 1.250
	// [beer milk] -> diapers  lift 1.250
	// [beer] -> diapers  lift 1.250
	// [diapers] -> beer  lift 1.250
	// [cola] -> milk  lift 1.250
	// [milk] -> cola  lift 1.250
	// [cola diapers] -> milk  lift 1.250
	// [cola milk] -> diapers  lift 1.250
	// [diapers milk] -> cola  lift 1.667
	// [cola] -> diapers  lift 1.250
	// [diapers] -> cola  lift 1.250
}

// Without Filter, an evaluation only computes the values: the results are
// those without an evaluation.
func ExampleEvaluation_valuesOnly() {
	src := fpgrowth.FromItems(baskets)
	sets, _, err := fpgrowth.CollectItemsets(context.Background(), src, fpgrowth.Options[string]{
		MinSupport: fpgrowth.Count(3),
		MinSize:    2,
		Evaluation: &fpgrowth.Evaluation{Measure: fpgrowth.Chi2PValue},
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, s := range sets {
		fmt.Printf("%v p-value %.3f\n", s.Items, s.Value)
	}
	// Output:
	// [milk bread] p-value 0.576
	// [diapers bread] p-value 0.576
	// [diapers milk] p-value 0.576
	// [beer diapers] p-value 0.171
}

// OnStart receives the database statistics before the first result, so
// relative supports can be streamed.
func ExampleOptions_onStart() {
	src := fpgrowth.FromItems(baskets)
	var total float64
	_, err := fpgrowth.MineItemsets(context.Background(), src, fpgrowth.Options[string]{
		MinSupport: fpgrowth.Fraction(0.6),
		OnStart: func(st fpgrowth.Stats) error {
			total = float64(st.TotalWeight)
			fmt.Printf("%d transactions, minimum support %d\n", st.Transactions, st.MinSupport)
			return nil
		},
	}, func(s fpgrowth.Itemset[string]) error {
		fmt.Printf("%v %.0f%%\n", s.Items, 100*float64(s.Support)/total)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	// Output:
	// 5 transactions, minimum support 3
	// [bread] 80%
	// [milk bread] 60%
	// [milk] 80%
	// [diapers milk] 60%
	// [diapers bread] 60%
	// [diapers] 80%
	// [beer diapers] 60%
	// [beer] 60%
}

// TableReader reads from any replayable input, here a gzip-compressed
// file that is decompressed on every pass.
func ExampleTableReader() {
	open := func() (io.ReadCloser, error) {
		f, err := os.Open("testdata/basket.txt.gz")
		if err != nil {
			return nil, err
		}
		zr, err := gzip.NewReader(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		return gzipFile{zr, f}, nil
	}
	src := fpgrowth.TableReader(open, "basket.txt.gz", fpgrowth.DefaultTableFormat())
	sets, _, err := fpgrowth.CollectItemsets(context.Background(), src,
		fpgrowth.Options[string]{Target: fpgrowth.TargetMaximal, MinSupport: fpgrowth.Fraction(0.6)})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(sets)
	// Output:
	// [{[beer diapers] 3 NaN} {[bread milk] 3 NaN} {[bread diapers] 3 NaN} {[milk diapers] 3 NaN}]
}

// gzipFile closes a gzip reader and the file below it.
type gzipFile struct {
	*gzip.Reader
	file *os.File
}

func (g gzipFile) Close() error { return errors.Join(g.Reader.Close(), g.file.Close()) }

// TableFile streams upstream's text format from a file on every pass.
func ExampleTableFile() {
	src := fpgrowth.TableFile("testdata/basket.txt", fpgrowth.DefaultTableFormat())
	sets, sum, err := fpgrowth.CollectItemsets(context.Background(), src,
		fpgrowth.Options[string]{Target: fpgrowth.TargetMaximal, MinSupport: fpgrowth.Fraction(0.6)})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(sum.Transactions, "transactions")
	for _, s := range sets {
		fmt.Println(s.Items, s.Support)
	}
	// Output:
	// 5 transactions
	// [beer diapers] 3
	// [bread milk] 3
	// [bread diapers] 3
	// [milk diapers] 3
}

// CSVFile streams a CSV file; here the first column is a weight and the
// first line a header.
func ExampleCSVFile() {
	src := fpgrowth.CSVFile("testdata/basket.csv", fpgrowth.CSVFormat{HasHeader: true, WeightColumn: 1})
	sets, sum, err := fpgrowth.CollectItemsets(context.Background(), src,
		fpgrowth.Options[string]{MinSupport: fpgrowth.Fraction(0.5), MinSize: 2})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("total weight", sum.TotalWeight)
	for _, s := range sets {
		fmt.Printf("%q %d\n", s.Items, s.Support)
	}
	// Output:
	// total weight 9
	// ["bread" "milk"] 6
	// ["diapers" "milk"] 5
}

// Generate produces transactions on demand, so even huge synthetic
// databases need no memory for the transactions.
func ExampleGenerate() {
	// transaction i contains the divisors 2..6 of i
	src := fpgrowth.Generate(1000, func(i int, buf []int) fpgrowth.Transaction[int] {
		for d := 2; d <= 6; d++ {
			if i%d == 0 {
				buf = append(buf, d)
			}
		}
		return fpgrowth.Transaction[int]{Items: buf, Weight: 1}
	})
	sets, _, err := fpgrowth.CollectItemsets(context.Background(), src,
		fpgrowth.Options[int]{Target: fpgrowth.TargetClosed, MinSupport: fpgrowth.Fraction(0.1)})
	if err != nil {
		log.Fatal(err)
	}
	for _, s := range sets {
		fmt.Println(s.Items, s.Support)
	}
	// Output:
	// [5 2] 100
	// [5] 200
	// [6 2 3] 167
	// [4 2] 250
	// [3] 334
	// [2] 500
}

// Mining stops when the loop is left or the callback returns an error.
func ExampleItemsets_earlyStop() {
	src := fpgrowth.FromItems(baskets)
	opt := fpgrowth.Options[string]{MinSupport: fpgrowth.Count(1)}
	n := 0
	for set, err := range fpgrowth.Itemsets(context.Background(), src, opt) {
		if err != nil {
			log.Fatal(err)
		}
		if len(set.Items) == 3 {
			fmt.Println("first set of three items:", set.Items)
			break // stops mining
		}
		n++
	}
	fmt.Println(n, "smaller sets before it")

	errFound := errors.New("found")
	var found fpgrowth.Itemset[string]
	_, err := fpgrowth.MineItemsets(context.Background(), src, opt, func(s fpgrowth.Itemset[string]) error {
		if strings.Join(s.Items, " ") == "cola milk" {
			found = s.Clone() // Items is reused after the callback
			return errFound
		}
		return nil
	})
	fmt.Println(errors.Is(err, errFound), found.Items, found.Support)
	// Output:
	// first set of three items: [diapers bread milk]
	// 4 smaller sets before it
	// true [cola milk] 2
}
