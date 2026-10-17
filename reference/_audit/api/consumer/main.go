// Realistic downstream consumer: market-basket analysis from an order-line
// CSV (order_id,product), the most common real-world export shape.
package main

import (
	"cmp"
	"context"
	"encoding/csv"
	"fmt"
	"iter"
	"log"
	"os"
	"slices"
	"strings"

	fp "github.com/dimchansky/fpgrowth-go"
)

const orderLines = `order_id,product
1,bread
1,milk
2,bread
2,diapers
2,beer
2,eggs
3,milk
3,diapers
3,beer
3,cola
4,bread
4,milk
4,diapers
4,beer
5,bread
5,milk
5,diapers
5,cola
`

// basketCSV is the "one row per basket" shape the library supports directly.
const basketCSV = "basket,weight\nbread;milk,1\n"

func main() {
	ctx := context.Background()

	// Friction 1: long-format (order_id,product) CSV is not supported by
	// CSVFile (one record = one transaction). The consumer must group rows
	// themselves and write a SourceFunc. Grouping requires either sorted
	// input or holding everything in memory.
	r := csv.NewReader(strings.NewReader(orderLines))
	rows, err := r.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	rows = rows[1:]
	src := fp.SourceFunc[string](func() iter.Seq2[fp.Transaction[string], error] {
		return func(yield func(fp.Transaction[string], error) bool) {
			var cur string
			var items []string
			for i, row := range rows {
				if i > 0 && row[0] != cur {
					if !yield(fp.Transaction[string]{Items: items, Weight: 1}, nil) {
						return
					}
					items = items[:0]
				}
				cur = row[0]
				items = append(items, row[1])
			}
			if len(items) > 0 {
				yield(fp.Transaction[string]{Items: items, Weight: 1}, nil)
			}
		}
	})

	// Friction 2: the type parameter must be spelled out for the options
	// literal even though no field uses T here.
	opt := fp.RuleOptions[string]{
		MinSupport:    fp.Fraction(0.4),
		MinConfidence: 0.6,
		MinSize:       2,
		Evaluation:    &fp.Evaluation{Measure: fp.Lift},
	}
	var total int64
	opt.OnStart = func(s fp.Stats) error { total = s.TotalWeight; return nil }
	rules, sum, err := fp.CollectRules(ctx, src, opt)
	if err != nil {
		log.Fatal(err)
	}
	// Friction 3: sorting results by lift is up to the consumer (fine), but
	// for Rule the rule support relative to total requires total from
	// OnStart or Summary.
	slices.SortFunc(rules, func(a, b fp.Rule[string]) int {
		return cmp.Or(cmp.Compare(b.Lift, a.Lift), cmp.Compare(b.Support, a.Support))
	})
	for _, r := range rules {
		body := slices.Clone(r.Body)
		slices.Sort(body) // report order is not sorted; consumers sort for display
		fmt.Printf("%-22s -> %-8s supp=%.2f conf=%.2f lift=%.2f\n",
			strings.Join(body, ","), r.Head, float64(r.Support)/float64(sum.TotalWeight), r.Confidence, r.Lift)
	}
	fmt.Printf("%d rules from %d baskets (total from OnStart=%d)\n", sum.Reported, sum.Transactions, total)

	// Also: the "one row per basket with ';'-joined items" shape: CSVFormat
	// cannot split a field into items, so the consumer needs the table format
	// with custom separators, and loses CSV quoting.
	f, _ := os.CreateTemp("", "b*.csv")
	f.WriteString(basketCSV)
	f.Close()
	defer os.Remove(f.Name())
	tf := fp.DefaultTableFormat()
	tf.FieldSeparators = ";,"
	tf.Weighted = true
	_, _, err = fp.CollectItemsets(ctx, fp.TableFile(f.Name(), tf), fp.Options[string]{MinSupport: fp.Count(1)})
	fmt.Println("table with header line:", err)
}
