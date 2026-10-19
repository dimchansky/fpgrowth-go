# Command-line reference: `fpgrowth`

`cmd/fpgrowth` is a Go port of Christian Borgelt's `fpgrowth` program,
version 6.21. It accepts the same options, reads and writes the same
formats, and prints the same messages and exit codes, except for the
deviations listed at the end of this document and in
[differences.md](differences.md).

```bash
go install github.com/dimchansky/fpgrowth-go/cmd/fpgrowth@latest
```

```text
fpgrowth [options] infile [outfile]
```

- Without any argument the program prints the usage text to standard
  output and exits with status 0. `-!` prints additional information (all
  evaluation measures, aggregation modes, variants and info format codes)
  to standard output and exits with status 0.
- With arguments, a two-line banner and progress messages go to standard
  error. The banner shows the upstream version (`version 6.21
  (2022.11.22)`) because the program reproduces that version's behavior.
- `infile` is required. `-` (or an empty argument) means standard input.
- Without `outfile` the results are only counted (the log says
  `writing <null>`). `-` (or an empty argument) writes them to standard
  output.

Examples with real output are in the [README](../README.md#command-line-program).

## Option syntax

The grammar is upstream's (details in
[upstream/cli-io.md](upstream/cli-io.md) §1.1):

- Options may appear before, between or after the file names and can be
  combined in one argument: `-xjZ`, `-s10m2n5`.
- Numeric options take their value from the rest of the same argument and
  never from the next argument: `-s20`, not `-s 20` (that sets the
  minimum support to 0 and makes `20` the input file). Integer values are
  parsed like C `strtol` with base 0 (so `-m010` is octal 8), real values
  like C `strtod`.
- Character options (`-t -e -a -A`) take the next character of the same
  argument.
- String options (`-R -P -h -k -I -v -r -f -b -C`) take the rest of the
  argument or, if it is empty, the whole next argument verbatim (`-k -`
  sets the separator to `-`). For `-R` and `-P`, `-` is a file name; only
  an empty value (`-R ""`, `-P ""`) means standard input or output.
- A repeated option overrides the earlier value; flags cannot be undone.

## Options

Defaults are those of the program, which differ from the library's zero
values in some places (see [Mapping to the library API](#mapping-to-the-library-api)).

### Target, sizes, thresholds

| Option | Default | Meaning |
| --- | --- | --- |
| `-t#` | `s` | Target: `s` frequent item sets (`f` is accepted too), `c` closed, `m` maximal, `g` generators, `r` association rules. |
| `-m#` | 1 | Minimum number of items per set or rule. `-m0` reports the empty set (not for maximal sets). |
| `-n#` | no limit | Maximum number of items per set or rule. `-n0` is accepted and gives an empty result. |
| `-s#` | 10 | Minimum support. Positive: percentage of the total transaction weight; negative: absolute support. For rules it is the support of the body (of body ∪ head with `-o`). The effective absolute minimum is at least 1. |
| `-S#` | 100 | Maximum support, same sign convention as `-s`; for rules applied to the same support as `-s`. |
| `-o` | off | Original rule support definition: `-s`/`-S` apply to body ∪ head. |
| `-c#` | 80 | Minimum confidence of a rule, in percent, `0..100`. |
| `-F#:#..` | none | Support border: minimum supports per item set size, starting at the size given by `-m`, same sign convention as `-s`. |

### Evaluation measures

| Option | Default | Meaning |
| --- | --- | --- |
| `-e#` | `x` (none) | Additional evaluation measure. `b` for item sets: binary logarithm of the support quotient; for rules (`-tr`): imbalance ratio. Other letters: the 25 rule measures listed by `-!`; for item sets they are aggregated over the rules with a single-item head. |
| `-a#` | `x` | Aggregation mode for item sets with a rule measure: `x` first value, `m` minimum, `n` maximum, `a` average. |
| `-d#` | 10 | Threshold for the measure, in percent (divided by 100). Measures marked `(+)` by `-!` must reach it, measures marked `(-)` must not exceed it. |
| `-i` | off | Invalidate the evaluation of rules whose support is not above the support expected under independence. |
| `-p#` | no pruning | Minimum size for pruning with the evaluation (item sets with a rule measure): `< 0` weak forward, `> 0` strong forward, `0` backward pruning. Ignored without a measure. |

### Algorithm and optimizations

| Option | Default | Meaning |
| --- | --- | --- |
| `-q#` | 2 | Item order: `1` ascending, `-1` descending w.r.t. frequency; `2` ascending, `-2` descending w.r.t. the transaction size sum; `0` do not sort. Values beyond ±2 act as ±2. Affects speed and result order only. |
| `-A#` | `c` | Variant: `s` simple tree nodes, `c` complex tree nodes, `d` top-down processing of a single prefix tree, `t` top-down processing of the prefix trees. Closed and maximal targets run `d` as `s`; generators run `t` as `d`. Rules and rule-based measures always use the item set tree path; there `-A` only influences the item coding (and so the result order). |
| `-x` | prune | Do not prune with perfect extensions (ignored for generators). |
| `-l#` | 16 | Number of items for the k-items machine; `0` or negative disables it, values above 16 act as 16. Used by variants `s` and `d`; variant `c` only distinguishes on (non-zero) and off (`0`). Not used by `t`, rules or rule-based measures. |
| `-j` | sort | Do not reorder items w.r.t. their conditional support (variant `c`; closed and maximal targets never reorder). |
| `-u` | use | Do not use head-union-tail pruning (maximal item sets only). |

### Input

| Option | Default | Meaning |
| --- | --- | --- |
| `-R#` | none | Item selection file (item set targets: only the listed items are used) or item appearance file (`-tr`: default appearance in the first record, then `item indicator` records). See the [README](../README.md#input-formats). |
| `-w` | off | The last field of every record is an integer transaction weight (C syntax: decimal, `0x` hex, `0` octal; optionally in `()`, `[]` or `{}`). Negative weights are an error. |
| `-r#` | `"\n"` | Record (transaction) separators. |
| `-f#` | `" \t,"` | Field (item) separators. |
| `-b#` | `" \t\r"` | Blank characters, removed around fields. |
| `-C#` | `"#"` | Comment characters; a record starting with one is skipped. `-C ""` disables comments. |

`-r -f -b -C` replace the whole character class and understand the escape
sequences `\a \b \f \n \r \t \v`, octal `\ooo` and hex `\xhh`; any other
escaped character stands for itself, and a trailing backslash is a
backslash. They apply to the input file and the
`-R` file.

### Output

| Option | Default | Meaning |
| --- | --- | --- |
| `-P#` | none | Write the pattern spectrum (lines `size support count`) to a file; `-P ""` writes it to standard output after the results. Not produced for rules. |
| `-Z` | off | Print the number of item sets (rules) per size to standard output after the results. |
| `-N` | pre-format | Do not pre-format integers. Accepted for compatibility; it changes nothing in the Go program (upstream: speed only). |
| `-g` | off | Write item names in scanable form (quote special characters). |
| `-h#` | `""` | Record header written before every output line. |
| `-k#` | `" "` | Item separator. |
| `-I#` | `" <- "` | Implication sign for rules (`head <- body`). |
| `-v#` | see below | Info format appended to each line. |

Default info format: ` (%S)` for item sets and ` (%X, %C)` for rules; with
a negative `-s` (absolute support) ` (%a)` and ` (%b, %C)`. An explicitly
given `-v` is used as is. The format codes (`%a` absolute support, `%S`
support in percent, `%b` body support, `%h` head support, `%c`/`%C`
confidence, `%l`/`%L` lift, `%e`/`%E` evaluation, `%Q` total weight, ...)
are listed by `-!`; a number between `%` and the letter sets the number of
significant digits. The strings of `-h -k -I -v` are used verbatim, without
escape decoding.

### Miscellaneous

| Option | Meaning |
| --- | --- |
| `-!` | Print additional option information and exit. |

`-z` (zlib-compressed output) is not supported, as in upstream's default
build, which does not define `USE_ZLIB`: it is an unknown option (exit
status 6). Support is deferred past v0.1.0.

## Exit status

| Status | Message (after `fpgrowth: `) |
| --- | --- |
| 0 | success, usage text, `-!` |
| 1 | `database too large` (a tree or the item set tree exceeds 2^31-1 nodes) |
| 2 | `cannot open file <name>`; `cannot create temporary file for <stdin>` |
| 3 | `read error on file <name>`; `read error on file <name> (input changed while reading)` |
| 4 | `write error on file <name>`; `write error on temporary file for <stdin>` |
| 5 | `double assignment of standard input` (input and `-R` both standard input) |
| 6 | `unknown option -<c>` |
| 7 | `missing option argument` |
| 8 | `wrong number of arguments` |
| 9 | `invalid target type '<c>'` |
| 10 | `invalid item set size <n>` |
| 11 | `invalid minimum support <x>`, `invalid maximum support nan`, `invalid support border nan` |
| 12 | `invalid minimum confidence <x>` |
| 13 | `invalid evaluation measure '<c>'`, `invalid evaluation threshold nan` |
| 14 | `invalid aggregation mode '<c>'` |
| 15 | `no (frequent) items found` |
| 16 | `invalid fpgrowth variant '<c>'`; `<file>:<record>(<field>): item expected` |
| 18 | `<file>:<record>(<field>): duplicate item '<name>'` (appearance file) |
| 20 | `<file>:<record>(<field>): transaction weight expected` |
| 21 | `<file>:<record>(<field>): invalid transaction weight <field>`; `<name>: total transaction weight too large` |
| 22 | `<file>:<record>(<field>): too many fields/columns` (appearance file) |
| 23 | `<file>:<record>(<field>): appearance indicator expected` |
| 24 | `<file>:<record>(<field>): unknown appearance indicator '<s>'` |
| 130 | `interrupted` (SIGINT; the temporary file for standard input is removed) |

Error messages go to standard error, preceded by an empty line, as
upstream.

## Mapping to the library API

The library (`github.com/dimchansky/fpgrowth-go`) exposes the same mining
functionality with typed options. `Options` configures item set mining
(`MineItemsets`, `Itemsets`, `CollectItemsets`), `RuleOptions` association
rules (`MineRules`, `Rules`, `CollectRules`). Thresholds that the CLI takes
in percent are fractions in the library.

| CLI | Library |
| --- | --- |
| `infile` | `TableFile(path, format)` (other inputs: `TableReader(open, name, format)`); standard input: `SpoolTable(ctx, os.Stdin, dir, format)` |
| `outfile` | the callback or iterator; text formatting has no library equivalent |
| `-ts`, `-tc`, `-tm`, `-tg` | `Options.Target`: `TargetFrequent`, `TargetClosed`, `TargetMaximal`, `TargetGenerators` |
| `-tr` | `RuleOptions` with `MineRules` / `Rules` / `CollectRules` |
| `-m#` | `MinSize`; `-m0`: `Options.IncludeEmpty` |
| `-n#` | `MaxSize` (0 means no limit in the library) |
| `-s#` (positive) | `MinSupport: Fraction(s/100)`; both follow the threshold contract (`differences.md`), and `Fraction(float64(k)/100)` gives exactly the threshold of `-sk` at every weight (for a decimal `s`, when `s/100` is a run-time float64 division like the CLI's) |
| `-s-#` (negative) | `MinSupport: Count(n)` (exact for every `int64`; the CLI's float notation is exact up to 2^53) |
| `-S#` | `MaxSupport`, same conversion; the zero value means no limit |
| `-o` | `RuleOptions.OriginalSupport` |
| `-c#` | `RuleOptions.MinConfidence = c/100` (the same threshold contract; library default 0: no confidence filter; CLI default 80) |
| `-F#:#..` | `Options.Border` (`Fraction` for positive, `Count` for negative values) |
| `-e#` | `Evaluation.Measure` (the letter of each `Measure` is in its doc comment); `-eb`: `LogSupportRatio` for item sets, `Imbalance` for rules; `-ex`: `Evaluation` nil |
| `-a#` | `Evaluation.Aggregate`: `x` `AggregateFirst`, `m` `AggregateMin`, `n` `AggregateMax`, `a` `AggregateAverage` |
| `-d#` | `Evaluation.Filter = true` with `Evaluation.Threshold = d/100` (the CLI always filters, default 10; the library filters only with `Filter`, and without it computes the values only) |
| `-i` | `Evaluation.InvalidateBelowExpected` |
| `-p#` | `Evaluation.PruneSize` (pointer to the value; nil: no pruning; requires `Filter`) |
| `-q#` | `Tuning.ItemOrder`: `2` `ItemOrderSizeSumAscending` (= `ItemOrderDefault`), `1` `ItemOrderFrequencyAscending`, `0` `ItemOrderNone`, `-1` `ItemOrderFrequencyDescending`, `-2` `ItemOrderSizeSumDescending` |
| `-A#` | `Algorithm`: `s` `AlgorithmSimple`, `c` `AlgorithmComplex` (= `AlgorithmAuto`), `d` `AlgorithmSingleTree`, `t` `AlgorithmTopDown` |
| `-x` | `Tuning.NoPerfectExtensions` |
| `-l#` | `Tuning.ItemMachine`: `-lk` with `1 ≤ k ≤ 16` is `k`; `-l0` is a negative value; 0 in the library means the default 16; values above 16 are rejected |
| `-j` | `Tuning.NoReorder` |
| `-u` | `Tuning.NoHeadUnionTail` |
| `-R#` (item sets) | `Options.Items` |
| `-R#` (rules) | `RuleOptions.Appearance` and `RuleOptions.DefaultAppearance` (`AppearBoth`, `AppearBody`, `AppearHead`, `AppearNone`) |
| `-P#` | `Options.PatternSpectrum`, result in `Summary.Spectrum` |
| `-Z` | `Summary.BySize` (and `Summary.Reported`) |
| progress messages (`[n item(s), m transaction(s)]`) | `Stats` via `Options.OnStart` / `RuleOptions.OnStart` and in `Summary` |
| `-w` | `TableFormat.Weighted` |
| `-r# -f# -b# -C#` | `TableFormat.RecordSeparators`, `FieldSeparators`, `Blanks`, `Comments` (literal bytes, no escape decoding); defaults from `DefaultTableFormat()` |
| `-N -g -h -k -I -v` | output formatting only; no library equivalent |

The library also reads CSV (`CSVFile`, `CSVReader`, `SpoolCSV`), which the CLI does not. The zero `TableFormat` is `DefaultTableFormat()`.

Differences between the CLI and the library beyond the units:

- The library requires `MinSupport`; the CLI defaults to 10 %.
- `Fraction` must be in `(0, 1]`; for an absolute threshold of 0 use
  `Count(0)`, which acts as 1 (like `-s0`).
- The library rejects invalid values that the CLI clamps or accepts (for
  example `ItemMachine > 16`, negative sizes, NaN thresholds) with
  `ErrInvalidOptions`.
- With an appearance map, items are registered in order of first
  occurrence in the data, not in the order of an appearance file; results
  are the same, the order of tied items (and so the result order) can
  differ from the CLI.
- An empty database or one without frequent items is not an error in the
  library (`Summary.FrequentItems` is 0); the CLI exits with status 15.
- The library evaluates without filtering unless `Evaluation.Filter` is
  set; a threshold without `Filter` is rejected.

## Deviations from upstream

The full list with evidence and tests is in
[differences.md](differences.md). The ones visible on the command line:

- **Results**: all mining-result fixes (M1-M9) and rule/evaluation fixes
  (R1-R11) apply to the CLI. In particular `-eb` selects the imbalance
  ratio for rules, `-p` prunes item sets as documented, `-s0` behaves like
  an absolute minimum support of 1, and closed/maximal results with `-n`
  are exact.
- **Output** (O1-O10): `-h` and `-g` work; no spurious empty-set line on
  upstream's fast output path; count-only mode honors `-n`; the binomial
  set count saturates instead of overflowing; `%d` prints the absolute
  support for item sets; `-Z` statistics are written after the item sets;
  `-P` write errors are reported. Number formatting reproduces upstream's
  formatter byte for byte (O8).
- **Input** (I1-I8): correct escape decoding, non-ASCII separators, no
  1024-byte field limit, the last record is kept when it ends in a field
  separator without a newline, correct file names in read errors, 64-bit
  weights, ISO C `nan(...)` syntax.
- **Validation and messages** (C1-C9): NaN values of `-s -S -c -d -F` are
  rejected; `-F` with an empty value uses only parsed values; write errors
  on standard output for `-Z` are reported (exit 4); total weight overflow
  gives exit 21 instead of a crash; data beyond 2^31-1 tree nodes gives
  `database too large` (exit 1); after a write error mining stops at once
  and the log shows the number of sets written; log times are wall-clock
  times; SIGINT exits with status 130; standard input is spooled to a
  temporary file instead of being read into memory.
- **Floating point** (F-1): log-based measures use Go's `math` package and
  can differ from the C library in the last bit; a value exactly at the
  threshold can then be reported by one program and not the other. For
  any measure, a value that is mathematically equal to the `-d`
  threshold may fall on either side after rounding, in both programs.
- **Statistical measures** (N1): `-e n p y t i g f h m s z` and `-i` are
  accurate for total weights up to 2^63 (upstream's double formulas
  cancel beyond about 10^12). Chi², Yates and `-i` print upstream's
  values wherever upstream's arithmetic is exact, the certainty factor
  wherever base/(base−head) <= 2^10; `-ei`, `-eg`
  and the Fisher tests above a total weight of 170 can differ from
  upstream in the last digits. The Fisher tests take time proportional to
  the square root of the total weight in the worst case.
- **Relative thresholds** (N2): `-s`, `-S`, `-F` and `-c` keep
  upstream's double formulas, with identical results, while x =
  percentage/100 × weight (or confidence × body support) is below 2^52.
  From 2^52 on (maxima: above 2^52), where upstream's relative tolerance
  alone is at least one unit of support and admitted results up to
  W·2^-52 outside the bound, the bound is exact (just below 2^52, from
  about 2^51.6, upstream's formula can already be a full unit off; it is
  kept there): ⌊x⌋ for `-s`, `-F` and `-c`, ⌈x⌉ for
  `-S`. `-c100` then admits only rules whose support equals the body
  support, and `-s100` is the total weight (the threshold contract in
  `differences.md`).
- **Numeric limits** (N3): absolute thresholds (`-s-<n>`, `-S-<n>`)
  above 2^53 are rounded, as upstream parses them as doubles.
- **Closed standard output** (C10): when the caller closes standard
  output (`>&-`), the Go runtime opens `/dev/null` in its place, so the
  program exits 0 where upstream reports "write error on file <stdout>"
  (exit 4). Redirect to a file if the exit status must detect this.
- **Preserved** (P1-P6, O8): see `differences.md`; among them, with an
  evaluation threshold and the default `-ax` the reported item sets can
  depend on `-A`, `-q` and the target (as upstream).

Kept as upstream: `-n0`, `-S0` and infinite values are accepted (empty or
saturated results), and integer option values are truncated from C `long`
to `int` like upstream.
