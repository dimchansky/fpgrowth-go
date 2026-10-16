# fpgrowth-go

Frequent item set mining and association rule induction with the FP-growth
algorithm, in pure Go: a library with a generic, streaming API and a
command-line program that is compatible with the original `fpgrowth`.

This project is a port and further development of Christian Borgelt's C
implementation **FP-Growth 6.21** (2022-11-22,
<https://borgelt.net/fpgrowth.html>, MIT License, © 2004-2022 Christian
Borgelt). The mining algorithms, their optimizations, the item and result
order and the command-line behavior are his; this repository ports them to
Go, fixes confirmed upstream defects (listed in
[docs/differences.md](docs/differences.md)), and adds a typed library API.

- Library: `github.com/dimchansky/fpgrowth-go` (package `fpgrowth`), no cgo,
  no dependencies outside the standard library.
- Command-line program: `github.com/dimchansky/fpgrowth-go/cmd/fpgrowth`.

## Contents

- [Status](#status)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Features](#features)
- [Library usage](#library-usage)
- [Command-line program](#command-line-program)
- [Input formats](#input-formats)
- [Memory and performance](#memory-and-performance)
- [Differences from upstream](#differences-from-upstream)
- [Limitations](#limitations)
- [Testing](#testing)
- [License](#license)
- [Further documentation](#further-documentation)

## Status

| Stage | Scope | State |
| --- | --- | --- |
| 1 | Faithful port of the mining core, reporter, rule evaluation, item set tree, table reader, text output and CLI; C reference build and differential tests | done |
| 2 | Public library API: generic items, replayable sources (memory, generated, table files, CSV, spooled readers), streaming results, cancellation | done |
| 3 | Performance work (profiling, benchmarks against the C reference, the closed/maximal size-limit cost described under [Memory and performance](#memory-and-performance)) | pending |

The results (item sets, rules, supports and evaluation values) are checked
against Christian Borgelt's C program, built from the unmodified sources,
and against a brute-force oracle (see [Testing](#testing)). Performance has
not been measured or tuned yet, so this README makes no performance
claims. There is no tagged release yet; the API may still change.

## Installation

The module needs Go 1.26 or later.

Library:

```bash
go get github.com/dimchansky/fpgrowth-go
```

Command-line program:

```bash
go install github.com/dimchansky/fpgrowth-go/cmd/fpgrowth@latest
```

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"

	fpgrowth "github.com/dimchansky/fpgrowth-go"
)

func main() {
	baskets := [][]string{
		{"bread", "milk"},
		{"bread", "diapers", "beer", "eggs"},
		{"milk", "diapers", "beer", "cola"},
		{"bread", "milk", "diapers", "beer"},
		{"bread", "milk", "diapers", "cola"},
	}
	src := fpgrowth.FromItems(baskets)
	// Every set of items that occurs in at least 60% of the baskets.
	opt := fpgrowth.Options[string]{MinSupport: fpgrowth.Fraction(0.6)}
	for set, err := range fpgrowth.Itemsets(context.Background(), src, opt) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(set.Items, set.Support)
	}
}
```

Output:

```text
[bread] 4
[milk bread] 3
[milk] 4
[diapers milk] 3
[diapers bread] 3
[diapers] 4
[beer diapers] 3
[beer] 3
```

Results come in upstream's order, not sorted, and the items of a set are in
the miner's report order. The order is deterministic for given data,
options and algorithm variant.

## Features

**Targets** (`Options.Target`, CLI `-t`): all frequent item sets, closed
item sets, maximal item sets, generators (free item sets), and association
rules with a single item as head (`MineRules`, CLI `-tr`; the body may be
empty).

**Algorithm variants** (`Options.Algorithm`, CLI `-A`): `AlgorithmSimple`,
`AlgorithmComplex` (the default, also chosen by `AlgorithmAuto`),
`AlgorithmSingleTree` (top-down processing of one prefix tree) and
`AlgorithmTopDown` (top-down processing of the prefix trees). All variants find the
same sets with the same supports; order and speed differ.

**Optimizations** (`Tuning`, all on by default, as upstream): perfect
extension pruning (`-x` disables), the k-items machine for up to 16 items
(`-l`), item reordering by conditional support in the `Complex` variant
(`-j` disables), head-union-tail pruning for maximal item sets (`-u`
disables), and the initial item order (`-q`). None of them changes the
results, only speed and result order. See [docs/algorithm.md](docs/algorithm.md).

**Thresholds**: minimum and maximum support, absolute (`Count`, exact over
the whole `int64` range, bounds inclusive) or relative to the total
transaction weight (`Fraction`, converted with upstream's arithmetic, so
`Fraction(k/100)` equals `-sk`); minimum and maximum size;
the empty set (`IncludeEmpty`, `-m0`); a per-size support border
(`Border`, `-F`); minimum rule confidence (`MinConfidence`, `-c`).

**Weights**: every transaction has an integer weight (`int64`, at least 0);
supports are sums of weights. The total weight is checked for overflow;
negative weights are rejected.

**Item selection and appearances**: restrict item set mining to given items
(`Options.Items`, `-R` with a selection file); restrict where items may
appear in rules (`RuleOptions.Appearance` and `DefaultAppearance`, `-R`
with an appearance file for `-tr`).

**Evaluation measures** (`Evaluation`, `-e -a -d -i -p`): 25 rule measures
(confidence, lift, conviction, Kulczynski, imbalance ratio, certainty
factor, chi² and G-test values and p-values, Fisher's exact test, ...) for
rules and, aggregated over the rules `S \ {h} -> h`, for item sets; the
binary logarithm of the support ratio for item sets; the values alone or,
with `Evaluation.Filter`, thresholds and evaluation-based pruning;
"invalidate below expected support".

**Rules** carry the support of body ∪ head, body support, head support,
confidence, lift and the evaluation value. `OriginalSupport` (`-o`) makes
the support thresholds apply to body ∪ head instead of the body.

**Statistics**: `Stats` (the number of transactions, the total weight, the
number of frequent items and the resolved absolute thresholds) is passed
to `OnStart` before the first result and is part of the `Summary`, which
also counts the results and the results per size (`-Z`);
`Options.PatternSpectrum` adds the pattern spectrum, the number of item
sets per (size, support) (`-P`).

**API**: any `comparable` item type; transaction databases are replayable
`Source`s (in-memory, generated on demand, streamed from files or any
reopenable reader such as a gzip stream, or spooled from a one-shot
reader); results are delivered one at a time through an
iterator or a callback; mining stops early when the loop is left, the
callback returns an error or the context is canceled. Mining runs on the
calling goroutine and starts no goroutines. All functions are safe for
concurrent use.

## Library usage

The full API reference is the package documentation
(`go doc -all github.com/dimchansky/fpgrowth-go`). The snippets below are
bodies of a `main` function that imports `compress/gzip`, `context`,
`errors`, `fmt`, `io`, `log`, `os`, `time` and
`fpgrowth "github.com/dimchansky/fpgrowth-go"`; the shown
output is what they print.

Three entry points exist for each kind of result:

| Item sets | Rules | Delivery |
| --- | --- | --- |
| `Itemsets` | `Rules` | `iter.Seq2` iterator; breaking out of the loop stops mining |
| `MineItemsets` | `MineRules` | callback, returns a `Summary`; a nil callback only counts |
| `CollectItemsets` | `CollectRules` | slice of cloned results plus `Summary` (holds the whole result in memory) |

The `Items` slice of an `Itemset` and the `Body` slice of a `Rule` are
owned by the library and reused: they are valid only during the callback or
iteration step. Use `Clone` to keep a result.

### Items of any comparable type

```go
ctx := context.Background()
src := fpgrowth.FromItems([][]int{{1, 2, 3}, {1, 2}, {2, 3}, {1, 2, 3}, {4}})
opt := fpgrowth.Options[int]{MinSupport: fpgrowth.Count(2), MinSize: 2}
sum, err := fpgrowth.MineItemsets(ctx, src, opt, func(s fpgrowth.Itemset[int]) error {
	fmt.Println(s.Items, s.Support)
	return nil
})
if err != nil {
	log.Fatal(err)
}
fmt.Printf("%d transactions, %d frequent items, %d sets, by size %v\n",
	sum.Transactions, sum.FrequentItems, sum.Reported, sum.BySize)
```

```text
[1 2] 3
[3 1 2] 2
[3 1] 2
[3 2] 3
5 transactions, 3 frequent items, 4 sets, by size [0 0 3 1]
```

### Weighted transactions

A weight stands for that many identical transactions. Note that the zero
value of `Transaction` has weight 0 (it counts as a transaction but adds
nothing to any support); `FromItems` gives every transaction weight 1.

```go
ctx := context.Background()
src := fpgrowth.FromSlice([]fpgrowth.Transaction[string]{
	{Items: []string{"tea", "lemon"}, Weight: 30},
	{Items: []string{"tea", "milk"}, Weight: 50},
	{Items: []string{"coffee", "milk"}, Weight: 20},
})
sets, sum, err := fpgrowth.CollectItemsets(ctx, src,
	fpgrowth.Options[string]{MinSupport: fpgrowth.Fraction(0.25)})
if err != nil {
	log.Fatal(err)
}
for _, s := range sets {
	fmt.Printf("%v %d of %d\n", s.Items, s.Support, sum.TotalWeight)
}
```

```text
[tea] 80 of 100
[milk tea] 50 of 100
[milk] 70 of 100
[lemon tea] 30 of 100
[lemon] 30 of 100
```

### Closed and maximal item sets

Closed sets have no superset with the same support; maximal sets have no
frequent superset. Both describe all frequent item sets with fewer
results.

```go
ctx := context.Background()
src := fpgrowth.FromItems([][]string{
	{"bread", "milk"},
	{"bread", "diapers", "beer", "eggs"},
	{"milk", "diapers", "beer", "cola"},
	{"bread", "milk", "diapers", "beer"},
	{"bread", "milk", "diapers", "cola"},
})
for _, target := range []fpgrowth.Target{fpgrowth.TargetClosed, fpgrowth.TargetMaximal} {
	opt := fpgrowth.Options[string]{Target: target, MinSupport: fpgrowth.Count(3)}
	for s, err := range fpgrowth.Itemsets(ctx, src, opt) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(target, s.Items, s.Support)
	}
}
```

```text
TargetClosed [beer diapers] 3
TargetClosed [bread milk] 3
TargetClosed [bread diapers] 3
TargetClosed [bread] 4
TargetClosed [milk diapers] 3
TargetClosed [milk] 4
TargetClosed [diapers] 4
TargetMaximal [beer diapers] 3
TargetMaximal [bread milk] 3
TargetMaximal [bread diapers] 3
TargetMaximal [milk diapers] 3
```

Closedness, maximality and the generator property are decided on all
frequent item sets; `MinSize`, `MaxSize`, `MaxSupport`, `Border` and the
evaluation only select which sets of the target are reported.

### Association rules with confidence and lift

`MinSupport` applies to the support of the rule body unless
`OriginalSupport` is set. `MinConfidence` is a fraction; its zero value
means no confidence filter (the CLI default is 80 %).

```go
ctx := context.Background()
src := fpgrowth.FromItems([][]string{
	{"bread", "milk"},
	{"bread", "diapers", "beer", "eggs"},
	{"milk", "diapers", "beer", "cola"},
	{"bread", "milk", "diapers", "beer"},
	{"bread", "milk", "diapers", "cola"},
})
opt := fpgrowth.RuleOptions[string]{
	MinSupport:    fpgrowth.Fraction(0.6), // support of the body
	MinConfidence: 0.75,
	MinSize:       2, // no rules with an empty body
}
for r, err := range fpgrowth.Rules(ctx, src, opt) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%v -> %s  support %d  confidence %.2f  lift %.2f\n",
		r.Body, r.Head, r.Support, r.Confidence, r.Lift)
}
```

```text
[milk] -> bread  support 3  confidence 0.75  lift 0.94
[bread] -> milk  support 3  confidence 0.75  lift 0.94
[diapers] -> bread  support 3  confidence 0.75  lift 0.94
[bread] -> diapers  support 3  confidence 0.75  lift 0.94
[diapers] -> milk  support 3  confidence 0.75  lift 0.94
[milk] -> diapers  support 3  confidence 0.75  lift 0.94
[beer] -> diapers  support 3  confidence 1.00  lift 1.25
[diapers] -> beer  support 3  confidence 0.75  lift 1.25
```

### Evaluation measures

An `Evaluation` fills `Value` with a measure for every result; with
`Filter` it also filters by `Threshold` (without `Filter` the results are
those without an evaluation). `Threshold` is in the units of the measure
(a fraction such as 0.1, not upstream's percentage 10). For most measures
larger is better and the threshold is a minimum; for the p-value measures
it is a maximum (`Measure.HigherIsBetter`), so a zero threshold filters
everything there.

```go
ctx := context.Background()
src := fpgrowth.FromItems([][]string{
	{"bread", "milk"},
	{"bread", "diapers", "beer", "eggs"},
	{"milk", "diapers", "beer", "cola"},
	{"bread", "milk", "diapers", "beer"},
	{"bread", "milk", "diapers", "cola"},
})
// Item sets whose support is above the support expected under
// independence of their items (binary log of the ratio > 0.2).
sets, _, err := fpgrowth.CollectItemsets(ctx, src, fpgrowth.Options[string]{
	MinSupport: fpgrowth.Count(2),
	MinSize:    2,
	Evaluation: &fpgrowth.Evaluation{Measure: fpgrowth.LogSupportRatio, Filter: true, Threshold: 0.2},
})
if err != nil {
	log.Fatal(err)
}
for _, s := range sets {
	fmt.Printf("%v support %d  log2 ratio %.3f\n", s.Items, s.Support, s.Value)
}
// Rules with a lift of at least 1.5.
rules, _, err := fpgrowth.CollectRules(ctx, src, fpgrowth.RuleOptions[string]{
	MinSupport:      fpgrowth.Count(2),
	OriginalSupport: true, // MinSupport applies to body and head together
	MinSize:         2,
	Evaluation:      &fpgrowth.Evaluation{Measure: fpgrowth.Lift, Filter: true, Threshold: 1.5},
})
if err != nil {
	log.Fatal(err)
}
for _, r := range rules {
	fmt.Printf("%v -> %s  lift %.3f\n", r.Body, r.Head, r.Value)
}
```

```text
[beer diapers] support 3  log2 ratio 0.322
[cola milk diapers] support 2  log2 ratio 0.644
[cola milk] support 2  log2 ratio 0.322
[cola diapers] support 2  log2 ratio 0.322
[diapers milk] -> cola  lift 1.667
```

Item sets can also be evaluated with any rule measure, aggregated over the
rules `S \ {h} -> h` (`Evaluation.Aggregate`: first, minimum, maximum,
average); `PruneSize` (with `Filter`) enables upstream's evaluation-based
pruning for that case.

### Streaming from files: TableFile and CSVFile

File sources open the file on every pass, stream it and close it; the file
is never loaded into memory as a whole. The file must not change between
the two passes: mining compares its size and modification time and
reports a change as `ErrNotReplayable`.

`TableFile` reads upstream's flexible text format. The file `baskets.txt`
of this example is:

```text
# the last field is the transaction weight (upstream -w)
bread milk 3
bread diapers beer eggs 1
milk diapers beer cola 2
bread milk diapers beer 1
bread milk diapers cola (2)
```

```go
ctx := context.Background()
format := fpgrowth.DefaultTableFormat() // one transaction per line, items separated by " \t,"
format.Weighted = true                  // the last field is the transaction weight
src := fpgrowth.TableFile("baskets.txt", format)
sets, sum, err := fpgrowth.CollectItemsets(ctx, src, fpgrowth.Options[string]{
	Target:     fpgrowth.TargetMaximal,
	MinSupport: fpgrowth.Fraction(0.4),
})
if err != nil {
	log.Fatal(err)
}
fmt.Println(sum.Transactions, "transactions, total weight", sum.TotalWeight)
for _, s := range sets {
	fmt.Println(s.Items, s.Support)
}
```

```text
5 transactions, total weight 9
[cola milk diapers] 4
[beer diapers] 4
[bread diapers] 4
[bread milk] 6
```

`CSVFile` reads RFC 4180 CSV with `encoding/csv`; quoted fields may contain
separators. The file `baskets.csv`:

```text
count,item 1,item 2,item 3,item 4
3,bread,milk,,
1,bread,diapers,beer,eggs
2,milk,diapers,beer,"cola, diet"
1,bread,milk,diapers,beer
2,bread,milk,diapers,"cola, diet"
```

```go
ctx := context.Background()
src := fpgrowth.CSVFile("baskets.csv", fpgrowth.CSVFormat{
	HasHeader:    true, // skip the first record
	WeightColumn: 1,    // the first column is the weight, not an item
})
sets, sum, err := fpgrowth.CollectItemsets(ctx, src, fpgrowth.Options[string]{
	MinSupport: fpgrowth.Fraction(0.5),
	MinSize:    2,
})
if err != nil {
	log.Fatal(err)
}
fmt.Println("total weight", sum.TotalWeight)
for _, s := range sets {
	fmt.Printf("%q %d\n", s.Items, s.Support)
}
```

```text
total weight 9
["bread" "milk"] 6
["diapers" "milk"] 5
```

The zero `TableFormat` is `DefaultTableFormat()`; any other value is taken
literally (clear `Comments` to read items that start with `#`).

### Other inputs: TableReader and CSVReader

`TableReader` and `CSVReader` read from any input that can be opened again
for every pass: a compressed file, embedded data, an `fs.FS`, object
storage. The function returns a fresh `io.ReadCloser` each time; the name
appears in error messages.

```go
ctx := context.Background()
open := func() (io.ReadCloser, error) {
	f, err := os.Open("baskets.txt.gz")
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{zr, f}, nil // closing closes the file
}
format := fpgrowth.DefaultTableFormat()
format.Weighted = true
src := fpgrowth.TableReader(open, "baskets.txt.gz", format)
sum, err := fpgrowth.MineItemsets(ctx, src, fpgrowth.Options[string]{MinSupport: fpgrowth.Fraction(0.4)}, nil)
if err != nil {
	log.Fatal(err)
}
fmt.Println(sum.Reported, "item sets")
```

With `baskets.txt.gz` the gzip-compressed `baskets.txt` of the previous
section this prints `12 item sets`.

### Relative supports while streaming

`OnStart` is called once before the first result with the `Stats` of the
database, among them the total weight, so a streaming consumer can print
relative supports.

```go
ctx := context.Background()
src := fpgrowth.FromItems([][]string{{"a", "b"}, {"a"}, {"a", "b"}, {"c"}})
var total float64
_, err := fpgrowth.MineItemsets(ctx, src, fpgrowth.Options[string]{
	MinSupport: fpgrowth.Fraction(0.5),
	OnStart: func(st fpgrowth.Stats) error {
		total = float64(st.TotalWeight)
		fmt.Println("minimum support", st.MinSupport, "of", st.TotalWeight)
		return nil
	},
}, func(s fpgrowth.Itemset[string]) error {
	fmt.Printf("%v %.0f%%\n", s.Items, 100*float64(s.Support)/total)
	return nil
})
if err != nil {
	log.Fatal(err)
}
```

```text
minimum support 2 of 4
[a] 75%
[b a] 50%
[b] 50%
```

### Generated transactions

`Generate` produces transaction `i` on demand on every pass, so the
transactions themselves are never stored. The function must return the
same transaction for the same `i`; it may reuse `buf` to avoid
allocations.

```go
ctx := context.Background()
// Transaction i holds the divisors 2..6 of i. Nothing is stored: every
// pass calls the function again.
src := fpgrowth.Generate(1000, func(i int, buf []int) fpgrowth.Transaction[int] {
	for d := 2; d <= 6; d++ {
		if i%d == 0 {
			buf = append(buf, d)
		}
	}
	return fpgrowth.Transaction[int]{Items: buf, Weight: 1}
})
sets, _, err := fpgrowth.CollectItemsets(ctx, src, fpgrowth.Options[int]{
	Target:     fpgrowth.TargetClosed,
	MinSupport: fpgrowth.Fraction(0.1),
})
if err != nil {
	log.Fatal(err)
}
for _, s := range sets {
	fmt.Println(s.Items, s.Support)
}
```

```text
[5 2] 100
[5] 200
[6 2 3] 167
[4 2] 250
[3] 334
[2] 500
```

### Standard input and other one-shot readers

Mining reads the source twice, so an `io.Reader` such as standard input
cannot be used directly. `SpoolCSV` and `SpoolTable` copy it to a temporary
file in chunks (never holding it in memory) and return a file source and a
cleanup function that removes the file. The disk space used equals the
input size.

```go
ctx := context.Background()
// Standard input can be read only once; mining needs two passes. SpoolCSV
// copies it to a temporary file ("" = os.TempDir()) and streams that file.
src, cleanup, err := fpgrowth.SpoolCSV(ctx, os.Stdin, "", fpgrowth.CSVFormat{})
if err != nil {
	log.Fatal(err)
}
defer cleanup()
n := 0
sum, err := fpgrowth.MineItemsets(ctx, src,
	fpgrowth.Options[string]{MinSupport: fpgrowth.Fraction(0.01)},
	func(s fpgrowth.Itemset[string]) error {
		n++
		return nil
	})
if err != nil {
	log.Fatal(err)
}
fmt.Println(n, "item sets in", sum.Transactions, "transactions")
```

With the input `a,b`, `a,c`, `b` (three lines) this prints
`5 item sets in 3 transactions`.

### Early stop

```go
ctx := context.Background()
src := fpgrowth.FromItems([][]string{
	{"bread", "milk"},
	{"bread", "diapers", "beer", "eggs"},
	{"milk", "diapers", "beer", "cola"},
	{"bread", "milk", "diapers", "beer"},
	{"bread", "milk", "diapers", "cola"},
})
opt := fpgrowth.Options[string]{MinSupport: fpgrowth.Count(1)}

// Breaking out of the loop stops mining.
var first fpgrowth.Itemset[string]
for s, err := range fpgrowth.Itemsets(ctx, src, opt) {
	if err != nil {
		log.Fatal(err)
	}
	if len(s.Items) == 3 {
		first = s.Clone() // s.Items is reused after this iteration step
		break
	}
}
fmt.Println("first set of three items:", first.Items, first.Support)

// With a callback, return an error; MineItemsets returns it unchanged.
errEnough := errors.New("enough")
n := 0
_, err := fpgrowth.MineItemsets(ctx, src, opt, func(fpgrowth.Itemset[string]) error {
	if n++; n == 5 {
		return errEnough
	}
	return nil
})
fmt.Println(errors.Is(err, errEnough), n)
```

```text
first set of three items: [diapers bread milk] 2
true 5
```

### Cancellation

Mining checks the context every 1024 transactions of a pass, at every
recursion step, and every few hundred item sets while the item set tree
of rules and rule-based evaluations is filtered and reported; it returns
`ctx.Err()` when it is canceled. Results found since the last check may
still be delivered after the cancellation (for rules, at most those of a
few hundred item sets). `SpoolTable` and `SpoolCSV` check the context
between chunks.

```go
ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
defer cancel()
src := fpgrowth.Generate(100_000, func(i int, buf []int) fpgrowth.Transaction[int] {
	for k := range 60 {
		if (i>>(k%17))&1 == 1 || k%(i%7+2) == 0 {
			buf = append(buf, k)
		}
	}
	return fpgrowth.Transaction[int]{Items: buf, Weight: 1}
})
_, err := fpgrowth.MineItemsets(ctx, src,
	fpgrowth.Options[int]{MinSupport: fpgrowth.Count(1)}, nil) // nil callback: count only
if errors.Is(err, context.DeadlineExceeded) {
	fmt.Println("mining canceled:", err)
} else if err != nil {
	log.Fatal(err)
}
```

### Errors

Invalid options, source errors and limits are reported as errors, never as
panics. Test them with `errors.Is`: `ErrInvalidOptions` (the message names
the field), `ErrNotReplayable`, `ErrNegativeWeight`, `ErrWeightOverflow`,
`ErrTooLarge`, `ErrSyntax` (malformed table or CSV input) and
`ErrInvalidItem` (an item that is not comparable at run time, such as a
slice in an `any` item, or not equal to itself, such as NaN). A database
without frequent items is not an error: nothing is reported and
`Summary.FrequentItems` is 0.

```go
ctx := context.Background()
src := fpgrowth.FromItems([][]string{{"a", "b"}, {"a"}})
_, _, err := fpgrowth.CollectItemsets(ctx, src, fpgrowth.Options[string]{}) // MinSupport missing
fmt.Println(errors.Is(err, fpgrowth.ErrInvalidOptions), err)
```

```text
true fpgrowth: invalid options: MinSupport: required
```

## Command-line program

`cmd/fpgrowth` accepts upstream's options, reads and writes the same
formats, and prints the same messages and exit codes, apart from the
documented deviations. Run it without arguments for the usage text and
with `-!` for the list of evaluation measures, aggregation modes, variants
and info format codes. The complete option reference, exit codes and the
mapping of options to library fields are in [docs/cli.md](docs/cli.md).

```text
usage: fpgrowth [options] infile [outfile]
```

`infile` is required; `-` means standard input. Without `outfile` the
program only counts the results; `-` writes them to standard output.
Progress messages go to standard error.

The examples use `test1.tab`, an example file of the upstream distribution
(`reference/upstream/fpgrowth-6.21/fpgrowth/ex/test1.tab`):

```text
a b c
a d e
b c d
a b c d
b c
a b d
d e
a b c d
c d e
a b c
```

Frequent item sets with a minimum support of 50 % (standard error and
standard output shown together; the times vary):

```text
$ fpgrowth -s50 test1.tab -
fpgrowth - find frequent item sets with the fpgrowth algorithm
version 6.21 (2022.11.22)        (c) 2004-2022   Christian Borgelt
reading test1.tab ... [5 item(s), 10 transaction(s)] done [0.00s].
filtering, sorting and recoding items ... [4 item(s)] done [0.00s].
sorting and reducing transactions ... [8/10 transaction(s)] done [0.00s].
writing <stdout> ... [6 set(s)] done [0.00s].
d (70)
c (70)
b c (60)
b (70)
a b (50)
a (60)
```

The following examples show standard output only.

Maximal item sets with a minimum support of 30 %:

```text
$ fpgrowth -tm -s30 test1.tab -
e d (30)
a b c (40)
a b d (30)
b c d (30)
```

Absolute minimum support (negative `-s`), at least two items, comma as the
item separator (the default info format becomes ` (%a)`, the absolute
support):

```text
$ fpgrowth -s-3 -m2 -k, test1.tab -
c,d (4)
b,c (6)
b,d,c (3)
b,d (4)
a,b (5)
a,c,b (4)
a,c (4)
a,d,b (3)
a,d (4)
e,d (3)
```

Association rules (`head <- body`) with a minimum body support of 30 % and
a minimum confidence of 80 %; the default info is body support and
confidence in percent:

```text
$ fpgrowth -tr -s30 -c80 test1.tab -
c <- b (70, 85.7143)
b <- c (70, 85.7143)
c <- a b (50, 80)
b <- a c (40, 100)
b <- a (60, 83.3333)
d <- e (30, 100)
```

The same rules with an explicit info format: support of body ∪ head, body
support, confidence and lift:

```text
$ fpgrowth -tr -s30 -c80 -v ' (%a, %b, %c, %l)' test1.tab -
c <- b (6, 7, 0.857143, 1.22449)
b <- c (6, 7, 0.857143, 1.22449)
c <- a b (4, 5, 0.8, 1.14286)
b <- a c (4, 4, 1, 1.42857)
b <- a (5, 6, 0.833333, 1.19048)
d <- e (3, 3, 1, 1.42857)
```

Standard input, item set statistics:

```text
$ cat test1.tab | fpgrowth -s30 -Z - out.txt
all: 15
  0: 0
  1: 5
  2: 7
  3: 3
```

## Input formats

**Upstream flexible table format** (`TableFile`, `SpoolTable`, the CLI):
one record per transaction, one field per item. Four byte classes
configure it: record separators (default newline, CLI `-r`), field
separators (default space, tab and comma, `-f`), blanks dropped around
fields (default space, tab and carriage return, so CRLF files work, `-b`)
and comment characters that start a comment record when they are the first
byte of a record (default `#`, `-C`). Empty lines are empty transactions
(they count toward the number of transactions and the total weight).
Repeated items in a transaction count once. Bytes are not interpreted as
UTF-8; an item ends at its first NUL byte. With weights (`Weighted`, CLI
`-w`) the last field of every record is an integer weight in C syntax
(decimal, `0x` hex or `0` octal, optional sign), optionally enclosed in
`()`, `[]` or `{}`. The CLI decodes escape sequences such as `\t` in `-r -f
-b -C`; `TableFormat` holds literal bytes. The zero `TableFormat` has no
special bytes at all; start from `DefaultTableFormat()`.

**CSV** (`CSVFile`, `SpoolCSV`, library only): RFC 4180 through
`encoding/csv`. Every record is a transaction and every field an item;
quoted fields may contain the separator, quotes and newlines. The zero
`CSVFormat` is valid: comma-separated, no comments, no header, no weights,
empty fields ignored (so ragged rows such as `a,b,,` are `{a, b}`). Options:
`Comma`, `Comment`, `HasHeader`, `WeightColumn` (1-based; negative counts
from the end; the weight is a decimal `int64`), `TrimSpace`,
`KeepEmptyFields`, `LazyQuotes`. A UTF-8 byte order mark is skipped; blank
lines are not transactions. CSV is a separate format on purpose and is not
the same as the table format with `-f,`.

**Item selection file** (CLI `-R` for item set targets; library
`Options.Items`): a list of item names in the table format; only these
items are mined, other items are removed from the transactions (the
transactions still count toward the total weight).

**Item appearance file** (CLI `-R` with `-tr`; library
`RuleOptions.Appearance` and `DefaultAppearance`): the first record holds
the default appearance; each following record holds an item and its
appearance: body only (`i`, `in`, `body`, `antecedent`, ...), head only
(`o`, `out`, `head`, `consequent`, ...), both (`io`, `both`, `x`, ...) or
none (`-`, `none`, `ignore`, ...). Indicators are matched by prefix like
upstream; the full list is in
[docs/upstream/cli-io.md](docs/upstream/cli-io.md) §5.2. Example:

```text
both
a body
d head
e none
```

```text
$ fpgrowth -tr -R app.txt -s20 -c70 test1.tab -
c <-  (100, 70)
d <-  (100, 70)
c <- b (70, 85.7143)
b <- c (70, 85.7143)
b <-  (100, 70)
c <- a b (50, 80)
b <- a c (40, 100)
b <- a (60, 83.3333)
```

## Memory and performance

FP-growth needs two passes over the data: pass 1 counts the items, pass 2
builds the transaction bag. Sources are read twice and never retained;
what mining keeps in memory is:

- the item dictionary (one entry per distinct item, with its frequency);
- the transaction bag: a prefix tree of the recoded, sorted and filtered
  transactions, one node per distinct transaction prefix, plus the summed
  weights of identical transactions;
- the FP-tree of the selected variant and its conditional (projected)
  trees, held in node arenas; projections are released when their
  recursion returns.

Memory therefore grows with the distinct structure of the data (number of
distinct items and distinct transaction prefixes), not with the number of
transaction records. A test (`TestLargeFileStreaming`) mines a 50 MB
generated table file and a CSV file of the same data with a heap peak
below 8 MB; that bound holds for that data, not in general. Nothing in this
library is constant-memory.

Structures that grow with the output:

- closed and maximal targets keep a repository of the closed or maximal
  sets found so far;
- the generator target keeps a hash table of the generators found;
- association rules and item sets with a rule-based evaluation measure
  first store every frequent item set up to the size limit in an item set
  tree, then derive and filter the results from it;
- the pattern spectrum has one cell per (size, support) pair that occurs;
- `CollectItemsets` and `CollectRules` hold all results; use the
  iterators or callbacks for large results.

Results are delivered one at a time from reused buffers; a test checks that
the public API adds only a constant number of allocations to the internal
pipeline, independent of the number of results.

Closed and maximal item sets with a maximum size (`MaxSize`, CLI `-n`) are
searched without the depth limit that upstream uses, because upstream's
limit lets non-closed and non-maximal sets through (deviation M5 in
[docs/differences.md](docs/differences.md)). On large data this can be
slower than upstream with `-n`. A cheaper exact check is scheduled for the
performance stage.

The `Tuning` options and `Algorithm` change speed and result order only.
Which settings are fastest depends on the data; no measurements are
published yet.

## Differences from upstream

The port reproduces upstream's results, order and text output, and fixes
confirmed upstream defects instead of copying them. Every difference is
intentional, tested and listed with its evidence in
[docs/differences.md](docs/differences.md). In summary:

- **Results.** Fixed: crashes or wrong item names with `-At` (M1); `-s0`
  and zero-frequency items (the effective minimum support is at least 1,
  M2); generators missed with `-m ≥ 2` (M3); maximal sets missed when all
  frequent items are perfect extensions or with `-x` and the k-items
  machine (M4, M9); non-closed/non-maximal sets reported with `-n` (M5).
  Negative weights are rejected (M6); supports are 64-bit and total-weight
  overflow is an error (M7); out-of-range thresholds are clamped (M8).
- **Rules and evaluation.** `-eb` selects the imbalance ratio for rules
  (R1); `-p` prunes as documented (R2); head-only items stay out of rule
  bodies (R3); `-S` applies to the same support as `-s` (R8); several
  measure edge cases are corrected (R4-R7, R9-R11).
- **Floating point.** Values are bit-identical to the C reference built
  without fused multiply-add, except log-based values, which can differ in
  the last bit from the platform's C library (F-1).
- **CLI output.** `-h` and `-g` work as documented; no spurious empty-set
  line; count-only mode honors `-n`; `-Z` statistics follow the item sets;
  write errors are reported (O1-O10). Number formatting is kept
  byte-compatible.
- **CLI input and messages.** Escape decoding, non-ASCII separators, long
  fields and the last record without a newline are handled correctly
  (I1-I8); NaN thresholds are rejected; standard input is spooled to a
  temporary file; SIGINT exits with status 130; log times are wall-clock
  times (C1-C9).

## Limitations

- Sources must be replayable: mining reads them twice. One-shot readers
  must be spooled to disk (`SpoolTable`, `SpoolCSV`; the CLI does this for
  standard input).
- Memory is not bounded by a constant (see
  [Memory and performance](#memory-and-performance)). Trees, the item set
  tree and the closed/maximal repository are limited to 2^31-1 nodes or
  items (`ErrTooLarge`).
- Performance has not been measured against the C program or optimized
  yet; closed and maximal mining with a maximum size can be slower than
  upstream (M5).
- Association rules have exactly one item in the head.
- Results come in upstream's order; the library does not sort them. The
  order depends on the data, the options and the algorithm variant.
- The library has no text output; formatting like the CLI's `-v` info codes
  exists only in the command-line program.
- Not ported: the `fpgpsp` program (its sources are missing from the
  upstream archive) and zlib-compressed output (`-z`, not compiled into
  the upstream reference build; the Go CLI rejects `-z` as an unknown
  option).

## Testing

```bash
go test ./...          # unit, golden, property and example tests; no C compiler needed
go test -race ./...
go vet ./...
```

The root module's tests compare against golden files produced by the C
reference (committed under `testdata/` directories), check results against
a brute-force oracle (`internal/oracle`) on random data, and include fuzz
targets for the table reader, the C number parsers, the transaction bag,
the miner, the CSV source and the CLI option parser.

`reference/` is a separate Go module with development tooling: the
unmodified upstream C sources, a reproducible reference build, and a
randomized differential test suite that runs the C program and the Go CLI
side by side. It needs a C99 compiler and GNU make:

```bash
cd reference
./build.sh            # build the C reference executables
go test ./...         # differential tests against the C build
```

See [reference/README.md](reference/README.md) for the build variants,
the differential test options and how deviations are verified.

## License

MIT, like upstream. See [LICENSE](LICENSE): © 2004-2022 Christian Borgelt
(original C implementation), © 2026 Dmitrij Koniajev (Go port and further
development).

## Further documentation

- [docs/algorithm.md](docs/algorithm.md): how FP-growth and this
  implementation work.
- [docs/cli.md](docs/cli.md): command-line reference.
- [docs/differences.md](docs/differences.md): all deviations from upstream.
- [docs/api-design.md](docs/api-design.md),
  [docs/architecture.md](docs/architecture.md): design of the library and
  the internal packages.
- [docs/feature-matrix.md](docs/feature-matrix.md): upstream features and
  their tests.
- [docs/upstream/](docs/upstream/): audit notes on the upstream C sources.
