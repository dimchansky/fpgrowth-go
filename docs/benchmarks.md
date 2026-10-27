# Benchmarks (Stage 3)

**Status: baseline and optimization pass.** The sections from
[Environment](#environment) to [Optimization candidates](#optimization-candidates)
are the baseline: measured on commit `c16c2f5` (end of Stage 2) plus the
new benchmark files only, before any optimization (the M5 rows were
re-measured on `3e5d372`, see there). [After optimization](#after-optimization)
records every optimization attempt and the state after them. The numbers describe one machine
under the load stated below; they are not general performance claims.
Re-run the commands in [Reproducing](#reproducing) before comparing a
later state against this one, on the same machine and toolchain.

Contents:

- [Environment](#environment)
- [Datasets](#datasets)
- [Benchmark suite](#benchmark-suite)
- [Methodology](#methodology)
- [Go vs C](#go-vs-c)
- [Go benchmark results](#go-benchmark-results)
- [Profiles](#profiles)
- [Optimization candidates](#optimization-candidates)
- [After optimization](#after-optimization)
- [Reproducing](#reproducing)
- [Appendix: full results](#appendix-full-results)

## Environment

| | |
| --- | --- |
| Machine | MacBook Pro, Apple M1 Max (8 performance + 2 efficiency cores, `hw.ncpu` 10), 32 GiB |
| OS | macOS 26.6.1 (25G76), Darwin 25.6.0 arm64 |
| Go | go1.27.2 darwin/arm64 (module `go 1.26.0`, `toolchain go1.27.2`), default `GOGC`, no `GOMEMLIMIT`, no PGO, `GOMAXPROCS` 10 |
| Go CLI build | `go build ./cmd/fpgrowth` (default flags) |
| C compiler | Apple clang 21.0.0 (clang-2100.3.34.2), target arm64-apple-darwin25.6.0 |
| C build | `reference/build.sh native`: upstream makefiles unchanged, `-std=c99 ... -DNDEBUG -O3 -funroll-loops` (no `-ffp-contract=off`; see `reference/README.md`) |
| benchstat | golang.org/x/perf benchstat (legacy output format: value ± maximum relative deviation from the mean of the samples kept after outlier removal) |
| Load | the machine was not idle: load averages 4.3 to 7.2 (1 min) during the runs (`uptime` before and after each phase, below). No CPU pinning is possible on macOS. |

Run log (all phases ran sequentially, nothing else of ours in parallel):

```
start    22:06  load averages: 5.48 6.42 8.24
suite    22:19  load averages: 4.62 4.88 6.18   (go test -bench BenchmarkSuite, count 6)
internal 22:21  load averages: 6.38 5.62 6.25   (internal and CLI benchmarks, count 6)
compare  22:27  load averages: 4.42 4.80 5.67   (C vs Go, 1 warmup + 5 runs)
profiles 22:28  load averages: 4.27 4.72 5.60
```

## Datasets

All datasets are generated deterministically by
[`reference/bench/gen`](../reference/bench/README.md) (seeded PCG source,
integer-only Zipf popularity, own bounded-integer and shuffle
derivations; pinned by SHA-256 in `TestDatasetsPinned`). The root
module's benchmarks generate the same transactions in memory
(`benchgen_test.go`, a verbatim copy of `reference/bench/gen/core.go`,
checked by `TestCoreInSync`). Item names are decimal indices. Nothing is
committed; files are written to temporary directories.

| Dataset | Transactions | Items used | Avg / min / max length | Density | Skew | Planted patterns | Weights | Table size | SHA-256 prefix |
| --- | ---: | ---: | --- | ---: | --- | --- | --- | ---: | --- |
| `sparse` | 20,000 | 1,000 | 9.3 / 4 / 17 | 0.009 | Zipf 1 | 30 × 5 items, 30 %, keep 90 % | no | 586,310 | `486a2624710d` |
| `sparse-large` | 200,000 | 20,000 | 11.1 / 5 / 21 | 0.001 | Zipf 1 | 100 × 6 items, 20 %, keep 90 % | no | 8,548,809 | `f9d614829794` |
| `dense` | 20,000 | 80 | 28.1 / 10 / 38 | 0.352 | uniform | 4 × 18 items, 90 %, keep 100 % | no | 1,628,269 | `b29d20acbcb1` |
| `long` | 10,000 | 400 | 62.2 / 40 / 88 | 0.155 | uniform | 20 × 8 items, 40 %, keep 90 % | no | 2,316,733 | `cddca8f4d640` |
| `weighted` | 20,000 | 1,000 | 9.4 / 4 / 17 | 0.009 | Zipf 1 | as `sparse` | 1..20 | 639,352 | `12e2b434b872` |
| `d2000x40d60` (M5) | 2,000 | 40 | 24.0 / 12 / 32 | 0.599 | independent, 60 % | — | no | 131,953 | `4b7365057352` |
| `d1000x30d80` (M5) | 1,000 | 30 | 24.1 / 17 / 29 | 0.805 | independent, 80 % | — | no | 64,348 | `868be1637571` |
| `d5000x60d40` (M5) | 5,000 | 60 | 24.0 / 11 / 37 | 0.400 | independent, 40 % | — | no | 339,938 | `4017ec71e7fa` |
| `d20000x200d30` (M5A-1) | 20,000 | 200 | 60.0 / 36 / 86 | 0.300 | independent, 30 % | — | no | 4,137,790 | `a91a38aab4da` |
| `t100000x204c4` (#19) | 100,000 | 204 | 20.8 / 5 / 43 | 0.102 | independent: 4 items 20 %, 200 items 10 % | — | no | 7,144,752 | `768bd8ccd3c2` |
| `s50000x414` (#19) | 50,000 | 414 | 67.2 / 39 / 98 | 0.162 | 4 exclusive items (80 %), 10 at 65 %, 400 at 15 % | — | no | 12,205,031 | `0f109186d108` |

Density is the average length divided by the number of items used. The
three `dNxKdP` datasets (added by review fix S3-2, seeds 1–3) are the
independent dense items of the [M5 investigation](m5-investigation.md)
§7, generated exactly like its in-process `denseTxs`; they are used only
by the M5 rows of `reference/bench/configs.txt`. `d20000x200d30` (seed 5,
issue #19) is the dense maximal workload of the independent audit
(M5A-1, below), which the audit made with Python `random.Random(5)` and
did not commit; the spec has the same shape (and the same set counts),
not the same bytes. `t100000x204c4` (seed 6) is two-tier data from the
PR review of #19 (performance finding F1): every pair of rare items is a
parent with only the 4 common items as maximal children, a guard
against building sibling rows that cannot pay off (`m5-tier-*` rows).
"Keep" is the probability that each item of a chosen planted pattern is
included; `dense` keeps whole patterns, so many item sets have perfect
extensions and the closed sets are far fewer than the frequent ones.

The internal micro-benchmarks (`internal/tabread`, `internal/tract`,
`cmd/fpgrowth`) use `internal/oracle.Generate` datasets described in the
respective files (50,000 transactions over 100 to 2,000 items; the CLI
benchmark uses a 20,000 × 1,000 dataset similar to `sparse`).

## Benchmark suite

Benchmarks do not run in `go test ./...` (only with `-bench`); default
test time is unchanged.

| File | Benchmarks | What is measured |
| --- | --- | --- |
| `benchmarks_test.go` | `BenchmarkSuiteTargets` | public API (`MineItemsets`, in-memory source, counting callback): 4 datasets × 1–2 thresholds × frequent/closed/maximal/generators |
| | `BenchmarkSuiteMaxSize` | `MaxSize` 3 and 5 on `dense` at 2 % (output capped by size) |
| | `BenchmarkSuiteVariants` | `Algorithm`/`Tuning`: simple (`-As -l0`), simple+fim16 (`-As`), complex (`-Ac -j`), complex+reorder (`-Ac`), single (`-Ad`), top-down (`-At`); dense frequent/closed/maximal, sparse frequent |
| | `BenchmarkSuiteRules` | `MineRules`, sparse and dense, without and with `Evaluation{Measure: Lift, Filter, Threshold 1.5}` |
| | `BenchmarkSuiteSources` | the same mining from `FromSlice` (string and int items), `TableFile` and `CSVFile` (MB/s = file size per op) |
| | `BenchmarkSuiteReadOnly` | both input passes without mining (minimum support 100 %): parsing, item dictionary, bag |
| | `BenchmarkSuiteConsume` | callback vs iterator (`Itemsets`, `Rules`) vs collect (`CollectItemsets`, `CollectRules`) vs the internal pipeline without sink (`fpg.Mine(..., nil)`: algorithm only, no conversion to public types) |
| `benchgen_test.go` | — | dataset generator (copy of `reference/bench/gen/core.go`) |
| `internal/tabread/reader_bench_test.go` | `BenchmarkReaderNext`, `BenchmarkScan` | parsing only: tokenizer (byte fields) and the scan function (string fields) over an in-memory table |
| `internal/isrout/writer_bench_test.go` | `BenchmarkWriterItemSet`, `BenchmarkWriterRule` | output only: formatting 10,000 item sets/rules with several `-v` formats |
| `internal/tract/bag_bench_test.go` | `BenchmarkCountRecode`, `BenchmarkBuildBag` | pass 1 (count + recode) and pass 1 + pass 2 (transaction bag) |
| `cmd/fpgrowth/cli_bench_test.go` | `BenchmarkCLI` | end-to-end CLI in process (`run`): frequent/closed/maximal/rules × no output / `/dev/null` / file |
| `reference/bench/compare.sh` | — | out-of-process Go vs C ([below](#go-vs-c)) |

Every benchmark reports its result count (`sets`, `rules`, `records`,
`bag-nodes`) with `b.ReportMetric`, so a changed count is visible next to
the time. The existing benchmarks (`bench_test.go`,
`internal/fpg/fpg_bench_test.go`, `internal/isrout`, `internal/tract`)
are unchanged.

## Methodology

- **Go benchmarks:** test binaries were compiled once (`go test -c`) and
  run with `-test.benchmem -test.count 6` (default `-benchtime 1s`;
  benchmarks slower than 1 s run one iteration per count). `b.Loop()`
  everywhere; dataset generation and file writing happen before the loop.
  Summaries by `benchstat`; the ± column is benchstat's maximum relative
  deviation, i.e. a spread, not a confidence interval.
- **Go vs C:** `reference/bench/compare.sh -n 5 -w 1`: one warmup run, then
  five measured runs per program, configuration and output mode, C and Go
  interleaved. Wall time from the runner's monotonic clock; user/sys CPU,
  maximum RSS and instructions retired from `/usr/bin/time -l`. Tables show
  medians; the wall-time column also shows min–max. Pattern counts are
  checked: **all 96 rows matched** (count = other program's count = output
  file lines).
- **Summary statistics:** the geometric means and ranges quoted below were
  computed by hand from the per-configuration medians; no committed tool computes them (`bench/compare` prints
  medians and min–max only). No significance test or confidence interval
  was computed for the Go/C ratios (n = 5 per side), and `compare.sh`
  records the load (`uptime`) once per run, not per configuration. The
  32-configuration headline excludes the 15 size-limited closed/maximal
  rows ([M5](#m5-size-limited-closedmaximal)), where Go is both much
  faster (dense data) and slower (large sparse data) than C. The
  independent audit reproduced the headline under load 13–17: geometric
  mean of the medians 1.596, of the per-configuration minima 1.601, range
  1.11–2.07; on `large-[cm]-n[23]` it measured Go/C 1.71–2.99.
- **Noise:** the machine had other load (see [Environment](#environment)).
  Most time spreads are 1–5 %; a few rows reach 8–14 % (marked in the
  appendix) and should be re-measured before being used to judge a
  change smaller than that. Instruction counts (`Go/C instr`) are far less
  sensitive to load than times.
- **Profiles:** `go test -bench <one workload> -benchtime 3s -cpuprofile
  -memprofile` on the same test binary; analysed with `go tool pprof
  -top/-list/-peek/-traces`. GC share from `GODEBUG=gctrace=1` (the
  cumulative GC CPU percentage of the last GC line). Bounds checks from
  `go build -gcflags=-d=ssa/check_bce/debug=1`, escapes from
  `go build -gcflags=-m`.

## Go vs C

Headline (median wall time of 5 runs; full table with every mode in the
[appendix](#go-vs-c-all-rows)). "count" runs without an output file (both
programs only count); "file" formats and writes every pattern. CPU is
user+sys from `/usr/bin/time` (10 ms resolution). RSS is the maximum
resident set size.

| Config | Options | Patterns | C wall ms (count) | Go wall ms (count) | Go/C wall (count / file) | C / Go CPU ms (file) | C / Go max RSS MB (count) | Go/C instructions (count) |
| --- | --- | ---: | ---: | ---: | --- | --- | --- | ---: |
| sparse-s | `-s0.1` | 27,703 | 28.2 | 54.9 | 1.94 / 1.89 | 20 / 50 | 12.3 / 23.9 | 3.39 |
| sparse-c | `-tc -s0.1` | 26,775 | 31.6 | 61.5 | 1.95 / 1.91 | 20 / 50 | 13.4 / 24.1 | 3.20 |
| sparse-r-lift | `-tr -s0.05 -c50 -el -d150` | 130,194 | 79.6 | 143.7 | 1.81 / 1.67 | 80 / 160 | 21.5 / 69.6 | 2.36 |
| large-s | `-s0.05` (sparse-large) | 37,505 | 330.6 | 714.9 | 2.16 / 1.95 | 300 / 620 | 78.1 / 145.0 | 3.95 |
| large-m | `-tm -s0.05` | 11,838 | 283.2 | 603.6 | 2.13 / 2.00 | 280 / 620 | 77.2 / 147.9 | 3.96 |
| dense-s | `-s5` | 1,294,276 | 371.0 | 588.4 | 1.59 / 1.62 | 440 / 740 | 99.8 / 44.8 | 2.18 |
| dense-c | `-tc -s5` | 342,016 | 501.1 | 804.2 | 1.60 / 1.62 | 500 / 830 | 107.7 / 44.1 | 2.13 |
| dense-m | `-tm -s5` | 48,051 | 437.6 | 710.9 | 1.62 / 1.63 | 430 / 700 | 106.8 / 44.2 | 2.14 |
| dense-g | `-tg -s10` | 194,385 | 156.5 | 271.2 | 1.73 / 1.72 | 160 / 290 | 76.6 / 69.2 | 2.34 |
| dense-r | `-tr -s10 -c90 -n4` | 41,425 | 127.2 | 234.9 | 1.85 / 1.86 | 120 / 230 | 12.9 / 38.1 | 2.79 |
| long-s | `-s2.5` | 34,285 | 686.6 | 792.3 | 1.15 / 1.18 | 660 / 790 | 341.8 / 86.8 | 1.99 |
| long-c | `-tc -s2.5` | 34,285 | 626.7 | 756.2 | 1.21 / 1.32 | 620 / 810 | 344.1 / 87.5 | 2.10 |
| weighted-s | `-w -s0.2` | 10,343 | 24.6 | 49.9 | 2.02 / 2.00 | 20 / 40 | 11.4 / 23.8 | 3.45 |

Across all 32 configurations: Go/C wall time geometric mean **1.72**
(count mode; range 1.15–2.16), **1.75** (`/dev/null`; 1.15–2.27),
**1.73** (file; 1.18–2.11). The Go binary executes **2.0–4.0×** the
instructions of the C binary (lowest on `long`, highest on `sparse-large`).

Variants (dense, `-s10`, frequent / closed, count mode, median wall ms):

| Variant | C frequent | Go frequent | Go/C | C closed | Go closed | Go/C |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `-As -l0` (simple) | 142.5 | 255.5 | 1.79 | 172.7 | 299.8 | 1.74 |
| `-As` (simple + fim16) | 114.9 | 203.5 | 1.77 | 169.1 | 277.6 | 1.64 |
| `-Ac -j` (complex) | 127.6 | 227.3 | 1.78 | 161.8 | 280.0 | 1.73 |
| `-Ac` (complex + reorder, default) | 121.1 | 204.5 | 1.69 | 172.6 | 289.3 | 1.68 |
| `-Ad` (single) | 170.9 | 286.0 | 1.67 | 169.4 | 278.9 | 1.65 |
| `-At` (top-down) | 210.2 | 304.5 | 1.45 | 243.0 | 351.3 | 1.45 |

Observations (from these measurements only):

- **Mining is ~1.5–2.5× slower, not just I/O.** The programs' own "mine"
  phase timings (count mode, 10 ms resolution) give C/Go 0.34/0.56 s
  (dense-s), 0.47/0.77 s (dense-c), 0.21/0.52 s (large-s), 0.65/0.76 s
  (long-s). Small datasets (`sparse`, `weighted`: 25–35 ms in C) are
  dominated by start-up and reading, where Go is ~2× slower.
- **Output formatting costs Go about twice as much per pattern.**
  (`/dev/null` − count) / patterns: C 58 ns, Go 125 ns (dense-s,
  1.29 M sets); C 55 ns, Go 129 ns (dense-s-As). Rules with a measure:
  C 125 ns, Go 158 ns (sparse-r-lift).
- **Memory is mixed.** Go uses less RSS on dense (45 vs 100–108 MB) and
  long data (87–97 vs 342–344 MB), more on sparse data (24 vs 12 MB,
  where the Go runtime's own footprint dominates), on `sparse-large`
  (139–154 vs 77–78 MB) and on rules (38–70 vs 12–22 MB).
- **Size-limited closed/maximal sets (M5).** At `c16c2f5` the search
  explored the whole closed lattice beyond the size limit, which made
  this the largest gap (`dense.tab -s2 -n3`, 3 runs: closed C 1.95 s /
  Go 4.54 s, maximal C 1.10 s / Go 2.73 s; `MaxSize/dense/s=2/closed/n=3`
  4.39 s). Since `3e5d372` the search is size-limited with a complete
  extension check ([M5 investigation](m5-investigation.md)); re-measured
  there (CLI, `/dev/null`, medians of 5 interleaved runs): frequent
  C 444 / Go 641 ms, closed C 1917 / Go 752 ms, maximal C 1055 /
  Go 954 ms, i.e. Go is faster than C for closed/maximal on dense data;
  in process `MaxSize/dense/s=2/closed/n=3` 731 ms vs 609 ms for
  frequent sets (n = 6). On sparse data the check costs Go up to 2.6× C
  (`sparse.tab -tc -s0.05 -n3`: C 43 ms, Go 113 ms). Numbers after the
  optimization pass: [M5](#m5-size-limited-closedmaximal).

### Algorithm-only comparison

The C program has no library API, so an in-process algorithm-only C
number does not exist. The closest pairs measured here:

| Workload | C "mine" phase (count mode) | Go `internal-nosink` (in process) | Go public callback |
| --- | ---: | ---: | ---: |
| dense `-s5` frequent | 0.34 s | 566 ms ± 6 % | 577 ms ± 4 % |
| dense `-s5` closed | 0.47 s | 765 ms ± 3 % | 793 ms ± 6 % |
| dense `-s10 -c90 -n4` rules | 0.10 s | 195 ms ± 1 % | 195 ms ± 4 % |
| sparse `-s0.1` frequent | 0.01 s | 31.6 ms ± 3 % | 31.5 ms ± 1 % |

The Go in-process numbers include both input passes over an in-memory
slice; the C phase excludes reading. They are an upper bound for the Go
side and only indicative.

## Go benchmark results

Selected rows (medians of 6, benchstat spread). All rows:
[appendix](#go-suite-all-rows).

| Benchmark | time/op | B/op | allocs/op | results |
| --- | ---: | ---: | ---: | ---: |
| Targets/sparse/s=0.1/frequent | 31.3 ms ± 2 % | 27.8 MB | 247 | 27.7k sets |
| Targets/dense/s=5/frequent | 544 ms ± 0 % | 72.5 MB | 238 | 1.29M sets |
| Targets/dense/s=5/closed | 750 ms ± 4 % | 77.9 MB | 343 | 342k sets |
| Targets/dense/s=5/generators | 667 ms ± 9 % | 175 MB | 289 | 412k sets |
| Targets/long/s=2.5/frequent | 704 ms ± 1 % | 129 MB | 278 | 34.3k sets |
| Targets/long/s=3/frequent | 226 ms ± 4 % | 129 MB | 197 | 2.94k sets |
| Rules/sparse/s=0.05/c=50/lift | 120 ms ± 1 % | 126 MB | — | 130k rules |
| Rules/dense/s=10/c=90/n=4 | 194 ms ± 3 % | 53.3 MB | — | 41.4k rules |
| Sources/sparse-large/s=0.05/memory-string | 379 ms ± 14 % | 269 MB | 1.37k | 37.5k sets |
| Sources/sparse-large/s=0.05/table-file | 570 ms ± 3 % | 275 MB | 41.7k | 37.5k sets |
| Sources/sparse-large/s=0.05/csv-file | 587 ms ± 3 % | 289 MB | 442k | 37.5k sets |
| ReadOnly/sparse-large/memory-string | 48.0 ms ± 1 % | 7.47 MB | 187 | — |
| ReadOnly/sparse-large/table-file | 146 ms ± 3 % (58.7 MB/s) | 10.3 MB | 20.4k | — |
| ReadOnly/sparse-large/csv-file | 142 ms ± 1 % (60.4 MB/s) | 17.7 MB | 220k | — |
| Consume/dense/s=5/frequent/collect | 686 ms ± 2 % | 512 MB | 1.29M | 1.29M sets |

- **Allocation counts are tiny and constant** (200–400 per mining run,
  independent of the number of results) for callbacks and iterators;
  the callback, iterator and internal no-sink runs are within noise of
  each other (e.g. dense frequent 577 / 569 / 566 ms). Collecting
  allocates once per result (`Consume/.../collect`: 1.29 M allocs,
  512 MB, +19 % time on dense frequent).
- **Bytes per op are large** relative to the data: 129 MB for `long`
  (2.3 MB table), 269 MB for `sparse-large` (8.5 MB table), 72 MB for
  `dense` (1.6 MB). See [allocation analysis](#allocation-analysis).
- **File sources** add 150–210 ms on `sparse-large` (memory 379 ms,
  table 570 ms, CSV 587 ms; the read-only benchmarks put two parsing
  passes at ~100 ms). Table and CSV read at about the same speed; CSV
  allocates per record (`encoding/csv`: 442k allocs/op vs 41.7k).
- **Variants** (in-process, dense `s=10`): frequent fastest with
  simple+fim16 (186 ms) and complex+reorder (188 ms); single 287 ms,
  top-down 320 ms. On sparse frequent, top-down is 2.6× slower than the
  others (81.9 ms vs 27.8–34.5 ms).

Internal micro-benchmarks (medians of 6):

| Benchmark | time/op | Notes |
| --- | ---: | --- |
| tabread ReaderNext/unweighted | 12.9 ms ± 1 % | 118 MB/s, 50k records, 19 allocs |
| tabread ReaderNext/weighted | 14.5 ms ± 1 % | 112 MB/s |
| tabread Scan (strings) | 20.1 ms ± 1 % | 75.3 MB/s, 1.03k allocs |
| isrout WriterItemSet info `""` | 41 ns/set | items only |
| isrout WriterItemSet info `" (%a)"` | 65.5 ns/set | absolute support |
| isrout WriterItemSet info `" (%S)"` (CLI default) | 106 ns/set | relative support, `%g`-style float |
| isrout WriterItemSet info `" (%a,%S,%e)"` | 165 ns/set | |
| isrout WriterRule default `" (%X, %C)"` | 159 ns/rule | |
| tract CountRecode items=100 / 2000 | 7.65 / 8.90 ms | pass 1 |
| tract BuildBag items=100 / 2000 | 35.9 / 40.0 ms | pass 1 + 2; 29.4 / 47.6 MB/op for 226k / 313k bag nodes |
| CLI frequent count / devnull / file | 46.8 / 50.3 / 51.1 ms | 20k × 1k dataset, `-s0.1` |
| CLI rules count / devnull / file | 63.4 / 71.2 / 72.5 ms | `-tr -s0.1 -c60` |

## Profiles

CPU profiles (3 s per workload; flat = self time, cum = including
callees, percentages of all samples of the run, which include benchmark
setup). Function names are in `internal/fpg` unless stated.

| Workload | Top functions (flat % / cum %) |
| --- | --- |
| dense frequent `s=5` | `csCondSupp` 31.5 / 32.4; `mine16` 17.7 / 27.5; `projReord` 9.8 / 87.5; `addReord` 8.3 / 17.4; `csArena.alloc` 5.2; `csArena.get` 4.6; `setSink.ItemSet` (public API) 2.8 |
| sparse frequent `s=0.1` | `csCondSupp` 13.3; `maps.memHashAES` 10.3 + `mapaccess2_faststr` 7.0 / 20.7 (item dictionary); `tract.bagBuilder.finish` 10.0 / 14.7 (child-list sort); `tract.ItemBase.countRecord` 4.7 / 25.3; `projReord` 4.0 / 34.0 |
| dense closed `s=5` | `csCondSupp` 24.6; `mine16` 20.0 / 42.8; `report.cmTree.add` 12.6 / 14.4; `projCmplx` 8.8 / 87.7; `addCmplx` 5.3 / 10.9 |
| long frequent `s=2.5` | `csCondSupp` **59.0** / 60.4; `projReord` 11.8 / 87.9; `addReord` 11.5 / 14.9 |
| dense rules `-n4` | `relink` (tree.go, rules path) **73.4** / 73.7 |
| sparse rules + lift | `relink` 9.8; `istree.Tree.newChild` 9.2 / 22.9; `istree.Tree.getsupp` 6.4 / 14.4; `istree.Tree.child` 6.1; `recTree` cum 38.2 |
| sparse-large, memory source | `csCondSupp` 20.9; `maps.memHashAES` 8.0 + `mapaccess2_faststr` cum 10.9; `mine16` 6.2; `tract.bagBuilder.child` cum 11.8; `tract.ItemBase.countRecord` cum 8.3 |
| dense closed `s=2 -n3` (M5) | limited search + extension check (`3e5d372`): `projCmplx` 20.1 / 66.9; `csCondSupp` 19.5; `extChecker.scanCover` 13.6; `addCmplx` 11.0 / 17.8; `extChecker.bitCover` 8.9; `csArena.get` 4.4. (Full exploration at `c16c2f5`: `report.cmTree.add` 26.3 / 31.2; `csCondSupp` 16.7; `mine16` 11.1 / 62.6; `closedMaximal.update` cum 32.6) |

Line-level evidence (`pprof -list`, dense frequent):

- `csCondSupp` (complex.go:123–125): 470 ms on the `succ` walk of the
  header list, 260 ms on the parent walk, 330 ms on
  `s[nodes[anc].id] += nodes[node].supp`. It is a pointer chase through
  the node arena (data-dependent loads).
- `mine16` (complex.go:139–148): 360 ms on `b |= 1 << i`, i.e. waiting on
  the `imap[nodes[anc].id]` load of the line before.
- `relink` (tree.go:268–283, dense rules): 770 ms on `if h.list == anc`
  and 600 ms on the second parent walk.

**Bounds checks** (`-d=ssa/check_bce`): every node access in these loops
is checked: 5 checks in `csCondSupp` (complex.go:121–125), 7 in
`mine16`'s loop (complex.go:13x–14x), 8 in `relink`, 33 in
`addReord`/`projReord` (complex.go:2xx), 12 in `cmTree.add`, 11 in
`bagBuilder.add/child/link`. 865 bounds checks in `internal/fpg`,
`internal/tract` and `internal/report` overall. Together with the 2–4×
instruction count of the C binary, this is the most direct evidence for
the per-node overhead.

**Escape analysis** (`-gcflags=-m`): no variable is moved to the heap in
`internal/fpg`, `internal/report`, `internal/tract`, `internal/istree`
or `internal/isrout` (only `slices` iterator state from the standard
library, and 6 in `internal/tabread` setup code). The escapes reported
("escapes to heap": fpg 172, report 62, tract 29, istree 22, isrout 49)
are construction-time allocations (`New`, arenas, `make` of per-item
arrays), `append` growth of arenas, and `fmt`/error arguments; none is
per result or per node. This matches the constant allocation counts of
the benchmarks.

**GC share** (`GODEBUG=gctrace=1`, cumulative GC CPU of the run): 0 %
(rounded) for dense frequent, sparse frequent, long frequent,
sparse-large, and even collect (512 MB/op); 1 % for sparse rules with
lift. The heap holds few, large, pointer-free arrays, so marking is
cheap. GC is not a bottleneck.

### Allocation analysis

`alloc_space` profiles (bytes over the whole profiled run):

| Workload | Top allocation sites |
| --- | --- |
| sparse-large, memory | `tract.bagBuilder.link` 1,050 MB (49 %); `fpg.mineComplex` 863 MB (41 %) |
| long frequent | `bagBuilder.link` 420 MB (65 %); `mineComplex` 190 MB (29 %) |
| dense frequent | `mineComplex` 206 MB (46 %); `bagBuilder.link` 201 MB (45 %) |
| sparse rules + lift | `istree.Tree.newChild` 2.51 GB (78 %); `bagBuilder.link` 0.36 GB (11 %) |
| dense closed | `mineComplex` 137 MB (41 %); `bagBuilder.link` 133 MB (40 %); `report.cmTree.alloc` 15 MB |

- `bagBuilder.link` appends one 24-byte node per new prefix to
  `bag.nodes`. With `-memprofilerate=1` on `BenchmarkBuildBag/items=100`
  (226,115 nodes, i.e. 5.4 MB of final node array), `link` allocates
  550 MB over 21 runs, **26 MB per run: about 5× the final array**,
  consistent with `append`'s 1.25× growth for large slices (each growth
  copies the array). `istree.newChild` shows the same pattern for the
  item set tree arrays.
- `mineComplex` (and `mineSimple`, `topdown`, `single`) presize the
  FP-tree node arena to `2 × bag extent + 16` nodes (capped at 2^24);
  `long` and `sparse-large` have large extents, which explains
  129–269 MB per op. Whether the presized capacity is used was not
  measured; RSS shows that untouched capacity is not resident (long:
  Go RSS 87 MB vs 129 MB allocated per op).
- `runtime.madvise` appears with 1.5–13 % in the CPU profiles (13 % for
  sparse rules + lift); it is the runtime returning freed spans of these
  large arrays to the OS.

**Caveat on syscall samples:** the `TableFile` profile attributes 1.70 s
(44 %) to `syscall.rawsyscalln`, of which 1.39 s is under
`tabread.tokenizer.fill` (64 KiB reads). This is not credible: reading
the same 8.5 MB file with 64 KiB blocks takes 0.6–2.9 ms (`dd`), and the
read-only benchmarks bound two full parsing passes at ~100 ms per op.
macOS delivers profiling signals preferentially at syscall return, so
syscall-heavy samples are over-attributed there; the same may apply to
`runtime.madvise`. Confirm I/O findings on Linux or with a wall-clock
measurement before acting on them.

## Optimization candidates

Ranked by expected impact on the measured workloads. Expected impacts are
estimates derived from the profile shares above, not measurements; each
must be validated with the benchmarks of the first column (count ≥ 6,
`benchstat`) and with the differential tests, since none may change
results or their order.

| # | Candidate | Evidence | Expected impact | Validate with |
| --- | --- | --- | --- | --- |
| 1 | **Tighten the hot tree loops** in `csCondSupp`, `mine16`, `projReord`/`addReord`, `projCmplx`/`addCmplx`, `relink`: eliminate bounds checks (re-slice once, prove indices, hoist `nodes[node].supp` out of the parent walk), keep loads in locals | 25–60 % flat in `csCondSupp` alone on every complex-variant workload; `relink` 73 % on dense rules; 5–33 bounds checks per loop; Go executes 2.0–4.0× C's instructions | largest: 10–30 % of mining time on dense/long/rules workloads if the per-node instruction count drops toward C's | `SuiteTargets/dense`, `SuiteTargets/long`, `SuiteRules/dense`, `SuiteVariants`; compare.sh `-m count` instructions column |
| 2 | **Presize or chunk `bag.nodes`** (pass 1 knows an upper bound: the number of item occurrences after filtering) instead of `append` growth | `bagBuilder.link` 40–65 % of all bytes; 5× the final array per build; `bagBuilder.child` 11.8 % cum CPU on sparse-large | B/op down by up to half on long/sparse-large; fewer copies and page faults; a few % of time | `tract BuildBag`, `SuiteReadOnly`, `SuiteSources/sparse-large` |
| 3 | **Right-size the FP-tree arena** (presize from the bag's node count or grow in chunks rather than `2 × extent`) | `mineComplex` 29–46 % of bytes; 129 MB/op on `long`, 269 MB/op on `sparse-large`; Go RSS 2× C on sparse-large | lower B/op and RSS on large/high-cardinality data; little CPU | `SuiteSources/sparse-large`, `SuiteTargets/long`, compare.sh RSS column |
| 4 | **Faster output formatting** (`%S`/`%g`-style support printing and line assembly in `isrout`) | Go 125–129 ns per pattern vs C 55–58 ns (`/dev/null` − count, 1 M+ sets); `isrout` 106 ns/set with `" (%S)"` vs 41 ns without info | 5–20 % of CLI wall time when output is large (dense-s: file 776 ms vs count 588 ms) | `isrout BenchmarkWriterItemSet`, compare.sh `devnull` vs `count` |
| 5 | **Cheaper item dictionary lookups** in both passes (`countRecord`, `BuildBag` scan: `map[string]` hashing) | `mapaccess2_faststr` (incl. `memHashAES`) 20.7 % cum of sparse frequent and 10.9 % of sparse-large samples; Go ~2× C on small datasets | 10–20 % on sparse/small inputs where reading dominates | `SuiteTargets/sparse`, `SuiteReadOnly/memory-string`, `tract CountRecode` |
| 6 | **Bag finish sort**: `slices.SortFunc` with a closure per node to sort child lists | `bagBuilder.finish` 10 % flat / 14.7 % cum on sparse frequent | a few % on sparse data | `tract BuildBag`, `SuiteTargets/sparse` |
| 7 | **Item set tree growth** for rules (`istree.newChild` / `children` appends) | 2.51 GB alloc_space (78 %) on sparse rules + lift; Go RSS 3× C on rules (70 vs 21 MB) | lower B/op and RSS for rules; some CPU via `madvise` | `SuiteRules`, compare.sh `sparse-r*` |
| 8 | **CSV source allocations** (`encoding/csv` record per line; `ReuseRecord`) | 442k allocs/op vs 41.7k for the table reader on the same data; same speed | allocations only; small time effect | `SuiteSources/*/csv-file`, `SuiteReadOnly/*/csv-file` |
| 9 | **Top-down variant on sparse data** | `SuiteVariants/sparse/.../topdown` 81.9 ms vs 27.8–34.5 ms for the others; C `-At` was not measured on sparse data | variant-specific; check against C first (may be inherent to the algorithm) | add an `-At` sparse row to `configs.txt` |
| — | Size-limited closed/maximal (M5) | full exploration at `c16c2f5`: 4.39 s vs 0.60 s frequent, Go/C 2.3–2.5×; limited search with extension check since `3e5d372`: 731 ms vs 609 ms, faster than C on dense data, up to 2.6× C on sparse data (per-set check) | sparse data: the per-set check cost (incremental covers) | `SuiteMaxSize`, `fpg BenchmarkCloMaxLimited`, CLI `sparse.tab -tc -s0.05 -n3` |

Not candidates on this evidence: GC tuning (GC ≤ 1 % of CPU), allocation
counts on the result path (constant, 200–400 per run), the public API
layer (callback/iterator within noise of the internal pipeline), and
read syscalls (see the caveat above).

## After optimization

Measured on the working tree after the optimization pass ("final")
against commit `3e5d372` ("base": the baseline code of this report plus
the size-limited closed/maximal search with the complete extension check
of [M5](m5-investigation.md)). Same machine, toolchain and build flags as
in [Environment](#environment). Outputs are unchanged: CLI golden tests,
`reference/difftest` (byte-identical to the C reference) and the fuzzers
pass, and every benchmark below reports the same result counts before
and after.

Method: for every candidate the test binaries of the state before and
after the change were built once (`go test -c`) and run alternately,
one `-test.count 1 -test.benchmem` run each, 6 to 10 times
(`old; new; old; new; ...`), so that drifting load affects both alike;
`benchstat` (legacy format, Mann-Whitney U test) decides. A change was
kept only with a significant gain (p < 0.05) on its target benchmarks
and no significant loss elsewhere. Load averages during the final
base-vs-final suite run: 108 (1 min) at its start (a short foreign load
spike), 3–5 afterwards; the C comparison ran at load 2.6–5.5.

### Attempts

Deltas are time/op unless stated (before → after the change); n is the
number of runs per side; "~" = not significant. Each step was measured
against the state before it, so the rows do not add up exactly.

| # | Change | Files | Result | Key numbers |
| --- | --- | --- | --- | --- |
| 1 | Complex-variant hot loops: one pointer per node in the walks (`csCondSupp`, `mine16`, `projCmplx`, `projReord`), `*int32` link instead of the `csLink` get/set pair in `addCmplx`/`addReord` (`csInsert`), branch-free 16-items bit table in `mine16` | `internal/fpg/complex.go`, `mine.go` | **kept** | dense `s=5` frequent −6.8 %, closed −5.4 %, long `s=2.5` frequent −4.8 %, closed −6.2 % (all p = 0.000, n = 10) |
| 1b | Software-pipelined load of the next list node in `csCondSupp` | `complex.go` | rejected | dense −2.6 % (p = 0.24), long −2.3 % (p = 0.052), n = 6 |
| 1c | `relink` (rules path) with one pointer per node | `tree.go` | rejected | dense rules 177 → 178 ms ~ (p = 0.065), lift +0.9 % (p = 0.036), n = 10 |
| 1d | Header entries 24 → 16 bytes (`fpHead`, `csHead`: `supp` first) | `simple.go`, `complex.go` (struct order only) | **kept** | dense rules −6.7 % / lift −5.1 % (p = 0.000), sparse closed −3.1 % (p = 0.021), dense closed −1.0 % (p = 0.004), n = 10 |
| 2 | Bag prefix-tree nodes grow by doubling (`growNodes`) instead of `append`'s 1.25× | `internal/tract/bag.go` | **kept** | `BuildBag` items=100 −9.0 %, items=2000 −7.8 % (p = 0.000); B/op 29.4 → 11.0 MB, 47.6 → 27.0 MB (n = 10) |
| 2' | (first version) presize the bag from the item occurrences of pass 1 | `tract/itembase.go`, `bag.go` | rejected | as fast (−8 / −14 %) but reserves memory per occurrence: `TestLargeFileStreaming` (heap < 8 MB for a 20 MB file of few distinct transactions) fails |
| 3 | FP-tree arenas presized from the bag's node count (×1.5 with projections) instead of 2 × extent (`treePresize`); top-down keeps the extent estimate | `internal/fpg/arena.go`, `complex.go`, `simple.go`, `single.go`, `tree.go` | **kept** | B/op −20 % (long) to −44 % (dense); time ~ except sparse variants −2.4 to −4.7 % (p ≤ 0.002), n = 8. With top-down included its B/op rose 28–30 % (its reversed tree outgrows the estimate) |
| 4 | Bag child-list sort on packed `uint64` keys with `slices.Sort` | `tract/bag.go` | rejected | `BuildBag` ~ (p = 0.12 / 0.36), sparse targets ~ (n = 10) |
| 5 | Path recording: `csCondSupp` stores each list node's ancestor ids and weight (`projPaths`); the second pass of the projection (`mine16`, `projCmplx`, `projReord`) reads that buffer instead of walking the tree again | `internal/fpg/complex.go`, `mine.go` | **kept** | dense `s=5` frequent −16.6 %, closed −13.8 % (p = 0.000, n = 8); long `s=3` −22 to −24 % (p = 0.000), long `s=2.5` ~ (n = 10); B/op +11–13 % on dense (buffers) |
| 6a | Number formatting: integral digits with `uint64` division instead of the `math.Mod`/`Floor` loop (exact below 2^53; at most 11 digits are formatted there) | `internal/isrout/numout.go` | **kept** | `AppendNum` −14.7 %, `WriterItemSet " (%S)"` −7.2 %, `WriterRule " (%X, %C)"` −18.5 % (p = 0.000, n = 10); 20 M random values byte-identical to the old code |
| 6b | Number formatting: magnitude by a linear scan (`powIndex`) instead of `bisect` | `numout.go` | **kept** | `AppendNum` −7.5 %, item sets −3.0 %, rules −3.3 / −3.8 % (p ≤ 0.001, n = 10); differential check identical |
| 7 | Item set tree arenas grow by doubling (`grow`) | `internal/istree/istree.go` | **kept** | sparse rules −8.4 %, sparse rules + lift −7.9 % (p = 0.000), dense rules ~; B/op −48 % / −57 % (n = 10) |
| 8 | CSV per-record allocations | — | not applicable | `csv.go` already sets `ReuseRecord`; the remaining allocation is `encoding/csv`'s one string per record. Removing it needs an own CSV parser, not justified (CSV reads as fast as the table format) |
| 9 | Top-down: link pointers in `addTopDown`/`tdMerge` (no closure) | `fpg/topdown.go` | rejected | dense ~ (p = 0.083 / 0.63), sparse ~ (p = 0.19), n = 10. C's `-At` is also about twice its `-Ac` on sparse data (mine 0.04 vs 0.02 s): the gap of the variant is algorithmic |
| 10 | M5 extension check with incremental covers: the cover of a reported set is the cover of the reporter's prefix (perfect extensions are in every cover transaction), cached per depth, one AND per new prefix item (`checkPath`); compact cover form | `internal/fpg/extcheck.go`, `mine.go`, `extcheck_test.go` (brute-force hook also checks `checkPath`) | **kept** | CLI in process on `sparse.tab`: `-tc -s0.02 -n3` −16.3 %, `-tm -s0.02 -n3` −8.3 %, `-tc -s0.05 -n2` −5.4 % (p = 0.000, n = 10); dense `CloMaxLimited` ~ to −1.9 % |
| 11 | `cmTree.add` sibling scan split by direction | `internal/report/clomax.go` | rejected | `-tc -s0.02 -n3` ~ (p = 0.075), unlimited `-tc -s0.05` +1.0 % (p = 0.007), n = 10 |
| 12 | Path recording for the simple variant (`fpCondSupp` shared by `projSimple` and `projSmp16`, packed entries recorded) | `internal/fpg/simple.go` (and the `projPaths` doc in `complex.go`) | **kept** | dense `s=10` simple −12.4 %, simple+fim16 −14.6 %, closed −12.4 / −10.2 %, maximal −15.1 / −12.4 %; sparse −8.6 / −5.1 % (p = 0.000, n = 10) |
| 13 | `projReord`: bit set of the path's new indices and `addCmplx` instead of `add_reord`'s flag scan | `complex.go`, `mine.go` | rejected | dense `s=5` +6.4 % (p = 0.000), long `s=2.5` −4.0 %, n = 8 |
| 14 | Review fix S3-1 (after `671474e`): M5 checker with compact bit sets (sparse items store only their non-zero words; full rows for every item if they need ≤ 4× the horizontal items, else for items with non-zero words in ≥ half of the words; no 64 MiB floor; transaction lists removed), closed scan that probes 8 spread cover transactions first, lazy cover enumeration, incremental covers for every form | `internal/fpg/extcheck.go`, `mine.go`, `extcheck_test.go` | **kept** | CLI, interleaved, n = 10: `sparse-large -tc -s0.05 -n2/-n3` −14.3 / −14.1 % wall, RSS 181 → 126 MB; `-tm -n2/-n3` ~ (p = 0.68 / 0.85), RSS 179 → 126 MB; `sparse.tab -tc -s0.05 -n3` −9.1 %, `dense.tab -tc -s2 -n3` −12.6 %; `dense.tab -tm -s2 -n3` +2.5 % (p = 0.010), `CloMaxLimited` ~ to +2.8 % ([M5 §7.1](m5-investigation.md#71-cost-on-large-sparse-data-stage-3-fix-s3-1)) |
| 14a | (variant of 14) transaction lists with incremental covers instead of full rows on `sparse-large` (a size rule relative to the horizontal form only) | `extcheck.go` | rejected | +21 to +31 % time, −80 MB allocated (in process, n = 5–6, load 5–40) |
| 14b | (variant of 14) early exit of the maximal scan when the remaining cover weight cannot make an extension frequent; linear steps before the exponential search | `extcheck.go` | rejected | instructions unchanged (±0.1 %) on `sparse-large -tm -n2/-n3` |
| 14 | Table tokenizer: one append per field, locals in the scan loop | `internal/tabread/tokenizer.go` | rejected | `ReaderNext`, `Scan` ~ (p = 0.08–0.6), n = 10 |
| 15 | Cheaper item dictionary lookups (candidate 5) | — | not done | see [Remaining gaps](#remaining-gaps) |

File sets of the kept changes: 1 + 5 + 12 (`internal/fpg/complex.go`,
`internal/fpg/mine.go`, `internal/fpg/simple.go`; 12 builds on 5's
`projPaths`), 1d (struct field order in `simple.go` and `complex.go`),
2 (`internal/tract/bag.go`), 3 (`internal/fpg/arena.go` and the
`treePresize` call sites in `complex.go`, `simple.go`, `single.go`,
`tree.go`), 6a + 6b (`internal/isrout/numout.go`), 7
(`internal/istree/istree.go`), 10 (`internal/fpg/extcheck.go`,
`internal/fpg/extcheck_test.go`, the `checkPath` hook in
`internal/fpg/mine.go`).

### Go benchmarks: base vs final

`BenchmarkSuite*`, alternating runs, n = 6 per side. Over all 96 suite
benchmarks the geometric mean of time/op is **−11 %** (benchstat's
non-significant deltas counted as 0). Selected rows:

| Benchmark | base | final | delta | B/op base → final |
| --- | ---: | ---: | ---: | --- |
| Targets/sparse/s=0.1/frequent | 31.7 ms | 28.4 ms | −10.3 % | 27.8 → 17.3 MB |
| Targets/dense/s=5/frequent | 549 ms | 421 ms | −23.3 % | 72.5 → 42.3 MB |
| Targets/dense/s=5/closed | 736 ms | 587 ms | −20.2 % | 77.9 → 47.7 MB |
| Targets/dense/s=5/maximal | 649 ms | 495 ms | −23.8 % | 74.3 → 44.1 MB |
| Targets/long/s=3/frequent | 219 ms | 165 ms | −24.5 % | 129 → 90 MB |
| Targets/long/s=2.5/frequent | 703 ms | 678 ms | −3.5 % | 129 → 90 MB |
| MaxSize/dense/s=2/closed/n=3 | 731 ms | 575 ms | −21.4 % | 76.6 → 46.4 MB |
| MaxSize/dense/s=2/closed/n=5 | 3.65 s | 2.53 s | −30.8 % | 85.9 → 72.9 MB |
| MaxSize/dense/s=2/maximal/n=5 | 2.72 s | 2.10 s | −22.7 % | 84.0 → 71.0 MB |
| Variants/dense/s=10/frequent/simple+fim16 | 180 ms | 150 ms | −16.7 % | 63.5 → 37.5 MB |
| Variants/dense/s=10/frequent/single | 255 ms | 252 ms | ~ | 49.9 → 31.7 MB |
| Variants/sparse/s=0.1/frequent/topdown | 78.3 ms | 77.8 ms | ~ | 24.1 → 19.8 MB |
| Rules/sparse/s=0.05/c=50/lift | 117 ms | 109 ms | −6.7 % | 126 → 54 MB |
| Rules/dense/s=10/c=90/n=4 | 186 ms | 173 ms | −7.0 % | 53.3 → 33.4 MB |
| Sources/sparse-large/s=0.05/memory-string | 371 ms | 349 ms | −6.0 % | 269 → 129 MB |
| Sources/sparse-large/s=0.05/table-file | 575 ms | 546 ms | −5.2 % | 275 → 134 MB |
| ReadOnly/sparse-large/table-file | 144 ms | 147 ms | ~ | 10.3 → 10.3 MB |
| Consume/dense/s=5/frequent/internal-nosink | 525 ms | 409 ms | −22.1 % | 72.5 → 42.2 MB |

All time deltas shown are significant (p ≤ 0.041) unless marked "~".
Not significant over all 96: the single variant on frequent sets, the
top-down variant (except maximal, −1.5 %), two simple-variant rows with
a large spread, and `ReadOnly/*` (except long/table-file, −1.3 %); no
benchmark got significantly slower. B/op drops by 15–57 % on the
mining benchmarks (6–30 % when collecting results into slices);
allocations per run grow by 20–140 (the path buffers and doubling growth
steps), still independent of the number of results.

Internal and CLI benchmarks (n = 6): `tract BuildBag` −10.9 % / −7.0 %
(`CountRecode` ~); `isrout AppendNum` −22.0 %, `WriterItemSet " (%S)"`
−10.7 %, `" (%a,%S,%e)"` −9.4 %, `WriterRule " (%X, %C)"` −21.4 %,
five-code rule −7.8 % (formats without numbers ~); `tabread` ~;
`BenchmarkCLI` −3.3 to −8.9 % (11 of 12 rows significant);
`fpg CloMaxLimited` −13 to −22 % for n2/n3 (n1 −7 % or ~).

### Go vs C after optimization

`reference/bench/compare.sh -n 5 -w 1` (same 32 configurations, 1 warmup
+ 5 interleaved runs per program, configuration and mode); all 96 rows
matched. Headline (medians); "before" is the Go/C ratio of the
[baseline table](#go-vs-c) (both ratios come from interleaved runs; the
absolute "before → after" Go times compare two runs of the script on
different days and are only indicative):

| Config | Options | Patterns | C wall ms (count) | Go wall ms (count) | Go/C wall (count / file) | Go/C count before | Go wall ms count before → after | C / Go CPU ms (file) | C / Go max RSS MB (count) | Go/C instructions (count) |
| --- | --- | ---: | ---: | ---: | --- | ---: | --- | --- | --- | ---: |
| sparse-s | `-s0.1` | 27,703 | 26.0 | 48.9 | 1.88 / 1.85 | 1.94 | 54.9 → 48.9 | 20 / 40 | 12.3 / 22.2 | 3.41 |
| sparse-c | `-tc -s0.1` | 26,775 | 29.5 | 54.7 | 1.86 / 1.79 | 1.95 | 61.5 → 54.7 | 20 / 50 | 13.4 / 22.3 | 3.20 |
| sparse-r-lift | `-tr -s0.05 -c50 -el -d150` | 130,194 | 77.2 | 131.5 | 1.70 / 1.54 | 1.81 | 143.7 → 131.5 | 90 / 160 | 21.5 / 52.5 | 2.29 |
| large-s | `-s0.05` (sparse-large) | 37,505 | 308.7 | 573.3 | 1.86 / 1.93 | 2.16 | 714.9 → 573.3 | 270 / 550 | 77.0 / 106.6 | 3.93 |
| large-m | `-tm -s0.05` | 11,838 | 272.3 | 550.1 | 2.02 / 1.92 | 2.13 | 603.6 → 550.1 | 270 / 540 | 77.2 / 103.6 | 3.94 |
| dense-s | `-s5` | 1,294,276 | 357.5 | 439.9 | 1.23 / 1.32 | 1.59 | 588.4 → 439.9 | 440 / 590 | 99.8 / 42.9 | 2.16 |
| dense-c | `-tc -s5` | 342,016 | 467.5 | 619.5 | 1.33 / 1.32 | 1.60 | 804.2 → 619.5 | 480 / 650 | 107.7 / 46.1 | 2.09 |
| dense-m | `-tm -s5` | 48,051 | 418.5 | 547.6 | 1.31 / 1.20 | 1.62 | 710.9 → 547.6 | 420 / 530 | 106.8 / 45.7 | 2.09 |
| dense-g | `-tg -s10` | 194,385 | 152.9 | 241.9 | 1.58 / 1.61 | 1.73 | 271.2 → 241.9 | 140 / 250 | 76.6 / 76.3 | 2.41 |
| dense-r | `-tr -s10 -c90 -n4` | 41,425 | 120.0 | 214.3 | 1.79 / 1.76 | 1.85 | 234.9 → 214.3 | 120 / 220 | 12.8 / 38.4 | 2.63 |
| long-s | `-s2.5` | 34,285 | 644.2 | 723.4 | 1.12 / 1.14 | 1.15 | 792.3 → 723.4 | 630 / 730 | 341.8 / 86.5 | 2.35 |
| long-c | `-tc -s2.5` | 34,285 | 584.0 | 690.0 | 1.18 / 1.22 | 1.21 | 756.2 → 690.0 | 560 / 690 | 344.1 / 86.6 | 2.43 |
| weighted-s | `-w -s0.2` | 10,343 | 25.4 | 50.5 | 1.99 / 1.93 | 2.02 | 49.9 → 50.5 | 20 / 40 | 11.4 / 22.1 | 3.44 |

Across all 32 configurations (computed by hand, without the M5 rows; see
[Methodology](#methodology)): Go/C wall time geometric mean **1.60**
(count mode; range 1.12–2.08; baseline 1.72, 1.15–2.16), **1.61**
(`/dev/null`; 1.14–1.96; baseline 1.75), **1.60** (file; 1.14–2.03;
baseline 1.73). The Go binary executes 2.1–3.9× the instructions of the
C binary (baseline 2.0–4.0×); on `long` the ratio rose from 1.99 to 2.35
while the time fell: path recording adds a store per ancestor, but the
second pass of a projection then reads a sequential buffer instead of
chasing parent pointers again.

Variants (dense, `-s10`, count mode, median wall ms; baseline Go/C in
brackets):

| Variant | C frequent | Go frequent | Go/C (before) | C closed | Go closed | Go/C (before) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `-As -l0` (simple) | 134.2 | 212.6 | 1.58 (1.79) | 172.1 | 280.9 | 1.63 (1.74) |
| `-As` (simple + fim16) | 109.8 | 170.2 | 1.55 (1.77) | 163.6 | 244.3 | 1.49 (1.64) |
| `-Ac -j` (complex) | 122.8 | 187.8 | 1.53 (1.78) | 152.7 | 235.3 | 1.54 (1.73) |
| `-Ac` (complex + reorder, default) | 122.0 | 191.4 | 1.57 (1.69) | 168.1 | 258.4 | 1.54 (1.68) |
| `-Ad` (single) | 164.9 | 268.5 | 1.63 (1.67) | 163.0 | 244.4 | 1.50 (1.65) |
| `-At` (top-down) | 201.4 | 298.0 | 1.48 (1.45) | 240.3 | 334.8 | 1.39 (1.45) |

Output formatting, (`/dev/null` − count) / patterns: Go 110 ns
(baseline 125) vs C 55 ns on dense-s; Go 127 ns vs C 60 ns on
dense-s-As. RSS: Go below C on dense (43–46 vs 100–108 MB) and long
(87 vs 342–344 MB), above on sparse (22 vs 12 MB), sparse-large (104–107
vs 77 MB; baseline 139–154) and rules (30–53 vs 12–22 MB; baseline
38–70).

The runner's "mine s" column (appendix) sums every phase line of the
programs' logs that matches "finding", "writing" or "filtering ...". For
Go that includes "filtering, sorting and recoding items", which is pass 2
(the input is read again and the bag built), while C only recodes there;
the column therefore overstates Go's mining time on input-heavy data
(sparse-large: 0.42 s, of which about 0.2 s is pass 2).

### M5: size-limited closed/maximal

CLI, output to `/dev/null`, median wall ms of 5 runs after 1 warmup, the
three programs interleaved (C native, Go base `3e5d372`, Go final). C's
counts differ where its size limit reports non-closed/non-maximal sets
(deviation M5, bug B5); Go's counts are the oracle-verified ones.

| Config | C | Go base | Go final | sets C / base / final |
| --- | ---: | ---: | ---: | --- |
| `dense.tab -s2 -n3` (frequent) | 444.0 | 640.6 | 521.4 | 54755 / 54755 / 54755 |
| `dense.tab -tc -s2 -n3` | 1917.4 | 752.1 | 626.5 | 54755 / 54755 / 54755 |
| `dense.tab -tm -s2 -n3` | 1054.9 | 953.8 | 804.8 | 11189 / 11189 / 11189 |
| `dense.tab -tc -s2 -n5` | 2759.1 | 3764.2 | 2603.5 | 1010561 / 1010561 / 1010561 |
| `sparse.tab -tc -s0.05 -n2` | 36.4 | 72.5 | 69.0 | 13957 / 13957 / 13957 |
| `sparse.tab -tc -s0.05 -n3` | 42.7 | 113.0 | 96.0 | 37087 / 37072 / 37072 |
| `sparse.tab -tm -s0.05 -n3` | 37.9 | 94.1 | 85.6 | 11487 / 11412 / 11412 |
| `sparse.tab -tc -s0.02 -n3` | 93.4 | 215.8 | 177.6 | 100116 / 99227 / 99227 |
| `sparse.tab -tc -s0.05` (no limit) | 48.4 | 87.5 | 82.5 | 65811 / 65811 / 65811 |

On dense data the limited search is faster than C (C searches one level
deeper and its 16-items machine is unlimited). On sparse data the
complete extension check still costs Go 1.9–2.3× C (C performs no
check and reports extra sets; without a limit the ratio is 1.7); incremental covers cut the check cost (16 %
on `-tc -s0.02 -n3`), the rest is the cover scans (`scanCover`) and the
repository (`cmTree.add`).

**Large sparse data (review finding S3-1).** The table above has no
large sparse dataset. On `sparse-large` (200,000 transactions, 2,592
frequent items at `-s0.05`) the check cost more, and the bit sets were
admitted only by an absolute 64 MiB floor (full rows of 64.6 MB, 9.1×
the 7.1 MB of horizontal items; a memory-for-speed trade-off: the
transaction lists were 21–31 % slower, attempt 14a). Medians of 5
(`bench/compare.sh -n 5 -m devnull '^large-'`; wall ms / max RSS MB; the
counts of C and Go are equal):

| Config | sets | C | Go `671474e` | Go after fix S3-1 |
| --- | ---: | ---: | ---: | ---: |
| `-tc -s0.05` (no limit) | 37,504 | 280 / 77 | 575 / 107 | 550 / 107 |
| `-tm -s0.05` (no limit) | 11,838 | 279 / 77 | 595 / 106 | 557 / 104 |
| `-tc -s0.05 -n2` | 12,769 | 277 / 77 | 777 / 181 | 622 / 126 |
| `-tc -s0.05 -n3` | 25,337 | 288 / 77 | 814 / 181 | 673 / 126 |
| `-tm -s0.05 -n2` | 4,091 | 272 / 77 | 789 / 179 | 715 / 126 |
| `-tm -s0.05 -n3` | 8,512 | 280 / 77 | 876 / 180 | 903 / 126 |

At `671474e` the limited runs took 1.3–1.5× the unlimited ones and
+73 MB; after the fix (attempt 14: compact bit sets bounded by 5× the
horizontal items, probed closed scans) closed runs take 1.1–1.2× and
+20 MB, maximal runs 1.3–1.6× (a maximal set's check counts its whole
cover). The interleaved before/after comparison (n = 10) is in attempt
14 and [M5 §7.1](m5-investigation.md#71-cost-on-large-sparse-data-stage-3-fix-s3-1)
(the `-tm -n3` medians above, 876 vs 903 ms, are two separate runs; the
interleaved comparison shows no significant difference, p = 0.85).
These rows (Go/C 2.2–3.2) are not part of the 32-configuration
geometric mean.

**Dense maximal sets near the maximum size (independent audit, M5A-1).**
The worst observed ratio to C is dense data with many maximal sets of
exactly the maximum size: on 20,000 transactions of 200 items with
density 0.3, `-tm -s2 -n3` (1,313,400 sets) took Go 99.7 s / 110.8 MB
and C 39.0 s / 1,258.5 MB (C's output is not exact in general, B5),
while `-tc` on the same data took Go 12.1 s and C 46.2 s. A maximal set's
check must show every one-item extension infrequent, so it cannot stop
early and counts the whole cover per set. The dataset is not part of
`configs.txt`; the measurement is the audit's
([audit/m5.md](audit/m5.md)). Sharing the work among sibling
sets is a possible optimization, deferred.

*Note (2026-10-20, issue #19):* the workload is now reproducible as the
pinned dataset `d20000x200d30` and the `m5-d20000-*` rows of
`configs.txt`. Re-measured on `main` (`3c0b7e8`), v0.1.0 and C native
(Apple M1 Max, load average 3.3–12.8; `/usr/bin/time -l`, output to
`/dev/null`, 5 interleaved runs, median [min–max]; wall s / max RSS MB):

| options | sets | Go `main` | Go v0.1.0 | C 6.21 |
| --- | ---: | --- | --- | --- |
| `-tm -s2 -n3` | 1,313,400 | 97.06 [97.03–97.20] / 111.6 | 97.47 [97.20–97.76] / 111.0 | 39.21 [39.07–39.65] / 1259.1 |
| `-tc -s2 -n3` | 1,333,500 | 10.31 [10.22–10.34] / 111.3 | 10.24 [10.15–10.24] / 111.0 | 39.26 [39.06–39.49] / 1259.1 |
| `-tm -s2 -n2` (3 runs) | 0 | 0.63 [0.62–0.65] / 107.6 | 0.62 [0.61–0.63] / 107.7 | 8.40 [8.39–8.67] / 1259.0 |

The set counts equal the audit's (every 3-set is frequent, no 4-set is),
and the times match its 99.7 s / 39.0 s within noise. A CPU profile of
the `-tm -s2 -n3` run attributes 91 % of the time to the extension check
(`checkPath` cum), 86 % to `scanMaximal` alone.

*Update (2026-10-20, issue #19):* the maximal check now decides the
children of a parent from sibling rows, i.e. bit sets re-indexed to the
parent's cover ([M5 §4.1](m5-investigation.md#41-sibling-rows-of-the-maximal-check-issue-19)).
5 interleaved runs, load 4.6–4.8, median [min–max] wall s / max RSS MB:

| `d20000x200d30` | Go `main` | Go v0.1.0 | Go branch | C 6.21 |
| --- | --- | --- | --- | --- |
| `-tm -s2 -n3` | 97.12 [96.81–97.50] / 110.5 | 97.25 / 110.5 | **19.82 [19.78–20.19] / 110.9** | 39.12 / 1259.1 |
| `-tm -s1 -n4` (3 runs) | 131.39 / 111.3 | 131.06 / 111.3 | **67.74 / 111.8** | — |
| `-tm -s2 -n3 -At` (3 runs) | 119.93 | 119.47 | **42.31** | — |
| `-tc -s2 -n3` (3 runs) | 10.42 | 10.28 | 10.41 | — |
| `-tm -s-2 -n2` | 0.62 | 0.61 | 0.62 | — |

On 28 other configurations (all fast `m5-*` rows, `sparse-large`,
`dense`, `sparse`, `weighted`, `long`; 7 runs each) only `dense -tm -s2
-n3` (−15.0 %) and `d5000x60d40 -tm -s5 -n3` (−8.2 %) changed
significantly; the rest is within noise. The in-process benchmarks are
in M5 §7.2.

The PR review found two-tier data, where every parent has only 4
maximal children, that this first build rule made 23–34 % slower
(`-tm -n3`). The final rule also requires the remaining children to pay
for the build ([M5 §4.1](m5-investigation.md#41-sibling-rows-of-the-maximal-check-issue-19)).
On the pinned `t100000x204c4` (`m5-tier-*` rows) and the reviewer's
shape it now builds nothing and matches `main` (3.42 vs 3.42 s for
`-tm -s-180 -n3`, n = 7). The target is unchanged at 19.8 s
([M5 §7.3](m5-investigation.md#73-review-round-1-few-children-per-parent-issue-19)).

The second review round found slices-and-spreaders data (pinned as
`s50000x414`, `m5-split-*` rows), where most later siblings are never
checked. It was +7 to +13 % slower than `main` until the later-sibling
bound was scaled below the maximum size. It now measures 2.47 vs
2.52 s. The worst row measured since then is +3.9 % in one round and
`~` in a repeat. The bound is 3.25× of the ordinary checks per level
([M5 §7.4](m5-investigation.md#74-review-round-2-later-siblings-that-are-never-checked-issue-19)).

*Update (2026-10-21, issue #20):* closed checks on a long transaction
plus many short ones (audit NEW-3) were quadratic in Go and C; the
closed scan now starts from a short cover transaction
([M5 §4, §7.5](m5-investigation.md#75-closed-checks-on-long-and-short-transactions-issue-20)).
`lp-*` rows of `configs.txt`, 6 interleaved runs, median wall s:

| `-As -tc -s-2 -n1` | Go `main` | Go branch | C 6.21 |
| --- | ---: | ---: | ---: |
| `lp12500` | 0.45 | 0.02 | 0.23 |
| `lp25000` | 1.74 | 0.03 | 0.88 |
| `lp50000` | 6.98 | 0.06 | 3.70 |
| `lp100000` | 27.91 | 0.12 | 13.50 (3 runs) |
| `lpc20000 -q0` | 60.15 | 0.51 | killed after 541 s |

The claim is for `-n1`: with `-n2`/`-n3` the projection, with `-At` the
top-down tree build stay quadratic on this data (lp50000 `-n3`: 12.07 →
5.26 s, C 3.43 s; `-At`: 10.22 → 3.42 s, C 11.61 s), and `-tm` is
unchanged (5.68 s, C 3.42 s). Every other `-tc` row of `configs.txt`
is unchanged within noise (instructions at most +0.65 %). Generated data
with long transactions (PR #35 review): the corrected exponential-search
rule is −2.2 % to +0.2 % against `main`; the worst case of the pass over
the cover (it runs on most scans and never finds a shorter transaction)
is +1.1 % (M5 §7.5).

*Update (2026-10-22, issue #21):* frequent item sets and generators with
a small maximum size were quadratic on long single paths in Go and C
(audit NEW-1, T4): the perfect extensions of sets with zmax items were
collected, expanded and rejected against the size limit. The miners now
skip them (`Reporter.PexUseful`), and `Report`/`expand` ignore the
extensions of a set of zmax items. Instructions retired (`/usr/bin/time
-l`, minimum of 2 runs, load average 3.3–4.8; the outputs of all four
programs are md5-identical):

| row | Go `main` | Go v0.1.0 | C 6.21 | Go branch |
| --- | ---: | ---: | ---: | ---: |
| `t4-6250 -s-17 -n1` | 2.111 G | 2.100 G | 1.290 G | 0.245 G |
| `t4-12500 -s-17 -n1` | 7.930 G | 7.926 G | 5.005 G | 0.496 G |
| `t4-25000 -s-17 -n1` | 30.703 G | 30.689 G | 19.771 G | 1.000 G |
| `t4-12500 … -As` | 8.401 G | 8.395 G | 5.240 G | 0.508 G |
| `t4-12500 … -Ad` | 8.393 G | 8.395 G | 5.162 G | 0.503 G |
| `t4-12500 … -As -l0` | 8.244 G | 8.247 G | 5.086 G | 0.505 G |
| `t4-12500 … -x` | 7.929 G | 7.929 G | 5.005 G | 0.510 G |
| `t4-12500 -tg -s-17 -n1` | 2.150 G | 2.151 G | 1.260 G | 0.512 G |
| `one-6250 -s-1 -n1` | 0.560 G | 0.558 G | 0.491 G | 0.071 G |
| `one-12500 -s-1 -n1` | 2.106 G | 2.101 G | 1.907 G | 0.154 G |
| `t4b-3000 -s-1 -n1` | 0.764 G | 0.764 G | 0.559 G | 0.078 G |
| `t4b-6000 -s-1 -n1` | 2.901 G | 2.899 G | 2.177 G | 0.156 G |

Per doubling of the size `main` and C grow 3.6–3.9×, the branch 2.0×
(linear). On 20 ordinary rows whose chain walks the change touches
(`sparse`, `dense`, `long`, `weighted`, `d2000x40d60`, `d5000x60d40`,
`t100000x204c4`; default target and `-tg`, with and without `-n`,
variants `-As -Ac -Ad -l0 -j`) the instructions differ from `main` by
−0.35 % to +0.33 %. `internal/fpg` `BenchmarkMine` (benchstat, 6
interleaved runs): geomean −0.19 %, one row +2.4 % (`m/c-j-l0`, a
maximal-set row where the guard is constant), one −1.5 %.
`internal/report` `BenchmarkReporter` was +4.6 % on `frequent` and
+4.0 % on `pex-hypercube-8` in the first version (+2.5 % and +2.6 %
instructions: a guard compare per `Report` and per `expand` call, PR #36
review P1). The final code tests the `Report` guard only when there are
perfect extensions and stops the expansion loop at zmax items in place
of upstream's per-extension size test, at no extra cost per call:
benchstat (6 interleaved runs) `frequent` +1.4 %, `count-only` +1.0 %,
`pex-hypercube-8` −1.5 %, `closed` ~, geomean +0.14 %; instructions
+0.33 % and +0.65 %. The T4 and wide-line rows are unchanged by this
(e.g. `t4-25000` 1.04 G, `t4b-6000` 0.156 G, same output).

*Update (2026-10-27, issue #34):* maximal checks on a long transaction
of weight below the minimum support plus many short ones (the lp shapes
with `-tm`) were quadratic in Go and C; the maximal scan now visits such
a transaction last, after checking that the plain scan would not have
stopped inside it, and searches it only for the items that can still be
frequent extensions
([M5 §4, §7.6](m5-investigation.md#76-maximal-checks-on-long-and-short-transactions-issue-34)).
Instructions retired (`/usr/bin/time -l`, minimum of 2 interleaved runs;
output byte-identical to `main`):

| `-As -tm -s-2 -n1` | Go `main` | Go v0.1.0 | C 6.21 (1 run) | Go branch |
| --- | ---: | ---: | ---: | ---: |
| `lp12500` | 8.03 G | 7.72 G | 2.04 G | 0.23 G |
| `lp25000` | 31.67 G | 30.45 G | 8.01 G | 0.45 G |
| `lp50000` | 125.9 G | 120.9 G | 31.78 G | 0.91 G |
| `lp50000s2` | 126.0 G | — | — | 1.04 G |
| `lp50000w` (`-w`) | 126.0 G | — | — | 0.98 G |
| `lpc20000` | 16.31 G | — | 39.37 G | 16.41 G |
| `advB-151-152 -q0` (review shape) | 7.89 G | — | — | 7.92 G |

Per doubling `main` grows 3.9–4.0×, the branch 2.0× (linear; lp50000
5.46 s → 0.05 s). `lpc20000 -tm` stays quadratic: its covers hold
several long chains that together reach the minimum support, so the
first of them in cover order is still counted in full (+0.6 %). The advB shape of
the PR review, where the plain scan stops early inside the long line,
is at `main`'s cost (−0.5 % to +0.5 %; 4.3× before the precheck), and
so is the advC shape of the second review, where many candidates meet
many deferred transactions (−0.3 % to +0.3 %; up to +94 % before the
candidate filters were limited). On
the ordinary `-tm -n` rows of `configs.txt` no check defers anything
(instrumented runs), and the instructions differ from `main` by
−0.45 % to +0.44 %.

### Profiles after optimization

CPU profiles of the final code (3 s each; flat % / cum %, package
`internal/fpg` unless stated):

| Workload | Top functions |
| --- | --- |
| dense frequent `s=5` | `csCondSupp` 40.3 / 41.6 (now also records the paths); `addReord` 17.8 / 21.9; `mine16` 5.9 / 19.4; `projReord` 5.3 / 88.4; public `setSink.ItemSet` 4.1 |
| sparse frequent `s=0.1` | `csCondSupp` 17.7; `slices.insertionSortOrdered` 5.0 (record sort in pass 2); `mapaccess2_faststr` 9.3 cum; `addReord` 4.0 / 5.0; `tract.bagBuilder.child` 3.7 / 13.7 |
| dense closed `s=5` | `csCondSupp` 39.4 / 40.3; `report.cmTree.add` 10.9 / 12.1; `addCmplx` 10.3 / 11.5; `mine16` 5.5 / 29.4 |
| long frequent `s=2.5` | `csCondSupp` **61.9** / 62.8; `addReord` 12.3 / 19.1; `csArena.alloc` 6.5 |
| dense rules `-n4` | `relink` **74.5** / 76.0 |
| sparse rules + lift | `relink` 15.2; `istree.Tree.newChild` 10.6 / 29.8; `istree.Tree.child` 8.9; `istree.Tree.getsupp` 5.0 / 17.9; `runtime.madvise` 5.3 (baseline 13) |
| sparse-large, memory source | `csCondSupp` 22.6 / 24.1; `maps.memHashAES` 5.7; `tract.bagBuilder.link` 5.2, `child` 12.6 cum |
| dense closed `s=2 -n3` (M5) | `csCondSupp` 23.8 / 24.5; `addCmplx` 20.7 / 25.7; `extChecker.scanCover` 16.0; `projCmplx` 12.9 / 66.8; `extChecker.extend` 4.1 (base `3e5d372`: `projCmplx` 20.1 / 66.9, `csCondSupp` 19.5, `scanCover` 13.6, `addCmplx` 11.0, `bitCover` 8.9) |

GC stays negligible (`GODEBUG=gctrace=1`: 0 % on dense frequent and on
sparse rules with lift).

### Remaining gaps

- **Reading the input twice** is now the largest Go-specific cost on
  sparse and large inputs. On sparse-large the CLI spends 0.14 s in
  pass 1 and 0.23 s in pass 2 (re-parse plus bag) against C's 0.08 s
  read and 0.01 s recode, while mining itself is on par (Go 0.16 s, C
  0.17 s). Every item occurrence is hashed twice per pass (the reader's
  intern map and `ItemBase.codes`: `mapaccess2_faststr` 26 % cum and
  `memHashAES` 14 % flat in the read-only table profile). Fixing it
  needs the reader to hand dense integer ids to a slice-indexed item
  base (an internal re-architecture across `tabread`, `tract`,
  `fpg.Mine`, the root sources and the CLI; candidate 5, not done).
  Keeping raw transactions in memory instead of a second pass would
  contradict the documented streaming design (`docs/architecture.md`,
  `TestLargeFileStreaming`).
- **Small inputs** (sparse, weighted: Go/C 1.7–2.0) are dominated by the
  same two passes plus fixed start-up (Go 21 M vs C 13 M instructions on
  a two-line file).
- **Rules** (dense-r 1.79): `relink` does exactly upstream's work
  (7,274 relinks, 2.75 M list nodes, 15.2 M ancestor steps in both
  programs, counted with instrumented copies) and is bound by the
  dependent loads and the mispredicted `h.list == anc` exit; bounds
  checks and 24-byte index arithmetic remain (1c showed no gain).
- **Output** costs Go about twice as much per pattern (110–127 vs
  55–60 ns): the line is rebuilt per set and the info format is parsed
  per call; C appends only the changed suffix of the item list.
- **Top-down** (Go/C 1.39–1.48) is algorithmic (C's top-down is slow on
  sparse data, too).
- **Pointer chasing**: `csCondSupp` (40–62 % of the complex variant) is
  latency-bound on scattered header lists; C's nodes are larger (48
  bytes), so the per-node memory traffic is not the difference.
  Structure-of-arrays nodes would make the ancestor walk denser but the
  list walk sparser and were not attempted.

## Reproducing

```bash
# Go benchmark suite (about 13 minutes) and internal/CLI benchmarks (about 3 minutes)
go test -run '^$' -bench 'BenchmarkSuite' -benchmem -count 6 -timeout 0 . | tee suite.txt
go test -run '^$' -bench 'ReaderNext|^BenchmarkScan$' -benchmem -count 6 ./internal/tabread | tee internal.txt
go test -run '^$' -bench 'WriterItemSet|WriterRule' -benchmem -count 6 ./internal/isrout | tee -a internal.txt
go test -run '^$' -bench 'CountRecode|^BenchmarkBuildBag$' -benchmem -count 6 ./internal/tract | tee -a internal.txt
go test -run '^$' -bench 'BenchmarkCLI' -benchmem -count 6 ./cmd/fpgrowth | tee -a internal.txt
benchstat suite.txt; benchstat internal.txt
benchstat old.txt new.txt          # after a change: same machine, same toolchain

# Go vs C (about 6 minutes without the slow m5-d20000-* rows, which an
# unfiltered run includes; needs a C compiler and GNU make)
(cd reference && bench/compare.sh -n 5 -w 1 -o /tmp/fpgcmp)   # /tmp/fpgcmp/summary.md, raw.tsv
# (configs.txt has 30 M5 rows more than the 32 measured above: m5-*,
# large-[cm]-n* (review fix S3-2) and the slow m5-d20000-* (issue #19); '^(sparse|large-[scm]$|dense|long|weighted)'
# selects the original 32)
# only the size-limited closed/maximal (M5) configurations: the pinned
# dNxKdP datasets and sparse-large with -n (reference/bench/README.md)
(cd reference && bench/compare.sh -n 5 -m devnull -o /tmp/fpgm5 '^(m5-d[125]000-|m5-tier-|m5-split-|large-)')
# the dense maximal workload of finding M5A-1 (issue #19; slow: Go about 100 s and C about 40 s per -tm -n3 run, over an hour in all)
(cd reference && bench/compare.sh -n 5 -m devnull -o /tmp/fpgm5a1 '^m5-d20000-')

# profiles (one workload at a time)
go test -c -o /tmp/root.test .
/tmp/root.test -test.run '^$' -test.bench 'BenchmarkSuiteTargets/dense/s=5/frequent$' \
  -test.benchtime 3s -test.cpuprofile cpu.prof -test.memprofile mem.prof
go tool pprof -top /tmp/root.test cpu.prof
go tool pprof -sample_index=alloc_space -top /tmp/root.test mem.prof
GODEBUG=gctrace=1 /tmp/root.test -test.run '^$' -test.bench '<benchmark>' -test.benchtime 3s 2>&1 | grep '^gc ' | tail -1
go build -gcflags=-m ./internal/fpg 2>&1 | grep -c 'escapes to heap'
go build -gcflags='-d=ssa/check_bce/debug=1' ./internal/fpg 2>&1 | grep complex.go
```

Profiled workloads (`-test.bench` patterns): `SuiteTargets/dense/s=5/frequent$`,
`SuiteTargets/sparse/s=0.1/frequent$`, `SuiteTargets/dense/s=5/closed$`,
`SuiteRules/sparse/s=0.05/c=50/lift$`, `SuiteRules/dense/s=10/c=90/n=4$`,
`SuiteTargets/long/s=2.5/frequent$`, `SuiteSources/sparse-large/s=0.05/memory-string$`,
`SuiteSources/sparse-large/s=0.05/table-file$`, `SuiteMaxSize/dense/s=2/closed/n=3$`.

## Appendix: full results

### Go vs C, all rows

Medians of 5 runs after 1 warmup; wall ms with (min–max). "mine s" is the
program's own mining phase time from its log (10 ms resolution).

| config | mode | patterns | C wall ms (min-max) | Go wall ms (min-max) | Go/C wall | C user+sys ms | Go user+sys ms | C maxRSS MB | Go maxRSS MB | C mine s | Go mine s | Go/C instr |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| sparse-s | count | 27703 | 28.2 (27.1-29.2) | 54.9 (54.2-55.7) | 1.94 | 20 | 40 | 12.3 | 23.9 | 0.01 | 0.04 | 3.39 |
| sparse-s | devnull | 27703 | 30.5 (30.3-32.0) | 58.9 (57.6-61.4) | 1.93 | 20 | 50 | 12.3 | 24.4 | 0.02 | 0.04 | 3.18 |
| sparse-s | file | 27703 | 30.9 (30.2-32.8) | 58.5 (57.5-60.0) | 1.89 | 20 | 50 | 12.3 | 23.9 | 0.02 | 0.04 | 3.16 |
| sparse-c | count | 26775 | 31.6 (30.9-31.9) | 61.5 (60.4-61.7) | 1.95 | 20 | 50 | 13.4 | 24.1 | 0.02 | 0.04 | 3.20 |
| sparse-c | devnull | 26775 | 33.1 (32.3-33.5) | 63.2 (62.4-64.3) | 1.91 | 20 | 50 | 13.5 | 24.6 | 0.02 | 0.05 | 3.05 |
| sparse-c | file | 26775 | 34.5 (33.4-35.1) | 65.8 (65.6-67.2) | 1.91 | 20 | 50 | 13.4 | 24.2 | 0.02 | 0.05 | 3.03 |
| sparse-m | count | 8468 | 30.1 (28.5-32.6) | 57.8 (57.1-60.1) | 1.92 | 20 | 50 | 13.4 | 24.5 | 0.02 | 0.04 | 3.26 |
| sparse-m | devnull | 8468 | 29.8 (29.5-30.8) | 59.0 (57.9-63.5) | 1.98 | 20 | 50 | 13.4 | 24.2 | 0.02 | 0.04 | 3.20 |
| sparse-m | file | 8468 | 30.6 (29.8-31.7) | 59.2 (57.8-59.5) | 1.94 | 20 | 50 | 13.4 | 24.5 | 0.02 | 0.04 | 3.18 |
| sparse-g | count | 26853 | 30.7 (29.6-32.0) | 58.4 (56.8-63.5) | 1.90 | 20 | 50 | 22.3 | 24.3 | 0.02 | 0.04 | 3.00 |
| sparse-g | devnull | 26853 | 33.9 (32.3-35.1) | 61.6 (60.3-70.3) | 1.82 | 20 | 50 | 22.3 | 24.2 | 0.02 | 0.04 | 2.87 |
| sparse-g | file | 26853 | 33.6 (33.0-37.0) | 61.6 (60.3-67.9) | 1.83 | 20 | 50 | 22.3 | 24.6 | 0.02 | 0.04 | 2.85 |
| sparse-r | count | 52149 | 36.7 (36.5-37.4) | 75.3 (73.6-75.9) | 2.05 | 30 | 60 | 12.0 | 38.7 | 0.02 | 0.06 | 2.81 |
| sparse-r | devnull | 52149 | 43.7 (42.8-45.4) | 83.3 (81.8-90.1) | 1.91 | 30 | 70 | 12.1 | 39.0 | 0.03 | 0.06 | 2.58 |
| sparse-r | file | 52149 | 44.1 (43.7-54.2) | 84.5 (84.3-88.0) | 1.92 | 30 | 70 | 12.1 | 36.4 | 0.03 | 0.06 | 2.57 |
| sparse-r-lift | count | 130194 | 79.6 (79.0-84.2) | 143.7 (141.5-145.8) | 1.81 | 70 | 140 | 21.5 | 69.6 | 0.06 | 0.12 | 2.36 |
| sparse-r-lift | devnull | 130194 | 95.2 (94.6-104.0) | 164.4 (162.8-166.5) | 1.73 | 80 | 150 | 21.4 | 69.6 | 0.08 | 0.14 | 2.21 |
| sparse-r-lift | file | 130194 | 99.8 (98.9-103.3) | 166.8 (166.4-167.8) | 1.67 | 80 | 160 | 21.5 | 70.0 | 0.08 | 0.15 | 2.20 |
| large-s | count | 37505 | 330.6 (303.6-370.9) | 714.9 (593.4-729.7) | 2.16 | 310 | 710 | 78.1 | 145.0 | 0.21 | 0.52 | 3.95 |
| large-s | devnull | 37505 | 342.2 (299.9-364.8) | 699.2 (661.3-731.9) | 2.04 | 320 | 690 | 78.3 | 153.5 | 0.21 | 0.53 | 3.92 |
| large-s | file | 37505 | 310.6 (298.5-333.5) | 605.9 (605.3-665.2) | 1.95 | 300 | 620 | 76.6 | 138.8 | 0.19 | 0.45 | 3.89 |
| large-c | count | 37504 | 302.1 (288.9-307.9) | 618.8 (605.2-640.3) | 2.05 | 290 | 630 | 77.2 | 141.1 | 0.19 | 0.46 | 3.93 |
| large-c | devnull | 37504 | 307.7 (296.3-339.5) | 612.5 (601.3-622.9) | 1.99 | 290 | 620 | 77.3 | 152.6 | 0.19 | 0.46 | 3.89 |
| large-c | file | 37504 | 295.5 (292.8-313.3) | 624.9 (598.9-633.1) | 2.11 | 280 | 630 | 77.2 | 146.4 | 0.18 | 0.47 | 3.87 |
| large-m | count | 11838 | 283.2 (282.4-288.1) | 603.6 (587.5-613.4) | 2.13 | 260 | 610 | 77.2 | 147.9 | 0.17 | 0.44 | 3.96 |
| large-m | devnull | 11838 | 299.2 (287.9-345.8) | 679.0 (599.3-732.5) | 2.27 | 280 | 680 | 77.2 | 139.0 | 0.18 | 0.51 | 3.95 |
| large-m | file | 11838 | 304.8 (289.8-339.6) | 609.5 (590.1-617.6) | 2.00 | 280 | 620 | 77.2 | 140.3 | 0.18 | 0.45 | 3.93 |
| dense-s | count | 1294276 | 371.0 (366.4-375.9) | 588.4 (581.9-592.2) | 1.59 | 350 | 570 | 99.8 | 44.8 | 0.34 | 0.56 | 2.18 |
| dense-s | devnull | 1294276 | 447.5 (442.0-453.5) | 745.8 (738.7-773.9) | 1.67 | 430 | 730 | 99.8 | 46.7 | 0.42 | 0.72 | 2.19 |
| dense-s | file | 1294276 | 478.4 (448.1-485.1) | 776.2 (751.5-786.8) | 1.62 | 440 | 740 | 99.8 | 46.8 | 0.43 | 0.74 | 2.16 |
| dense-c | count | 342016 | 501.1 (484.0-502.2) | 804.2 (794.7-838.7) | 1.60 | 480 | 790 | 107.7 | 44.1 | 0.47 | 0.77 | 2.13 |
| dense-c | devnull | 342016 | 517.3 (511.0-526.0) | 838.5 (832.8-852.9) | 1.62 | 500 | 830 | 107.8 | 43.9 | 0.49 | 0.81 | 2.11 |
| dense-c | file | 342016 | 516.3 (512.5-522.6) | 839.0 (834.1-848.8) | 1.62 | 500 | 830 | 107.8 | 44.6 | 0.48 | 0.81 | 2.11 |
| dense-m | count | 48051 | 437.6 (431.0-448.6) | 710.9 (702.7-720.1) | 1.62 | 420 | 700 | 106.8 | 44.2 | 0.40 | 0.68 | 2.14 |
| dense-m | devnull | 48051 | 436.4 (432.1-455.5) | 717.8 (710.3-743.5) | 1.64 | 420 | 700 | 106.8 | 49.5 | 0.41 | 0.69 | 2.13 |
| dense-m | file | 48051 | 442.6 (434.4-470.3) | 721.8 (712.5-753.9) | 1.63 | 430 | 700 | 106.8 | 48.9 | 0.41 | 0.68 | 2.13 |
| dense-g | count | 194385 | 156.5 (154.1-166.8) | 271.2 (262.1-301.3) | 1.73 | 140 | 250 | 76.6 | 69.2 | 0.13 | 0.24 | 2.34 |
| dense-g | devnull | 194385 | 170.9 (168.3-181.8) | 292.0 (287.7-294.5) | 1.71 | 150 | 280 | 76.6 | 69.0 | 0.14 | 0.26 | 2.29 |
| dense-g | file | 194385 | 174.3 (167.3-189.0) | 299.8 (290.9-305.1) | 1.72 | 160 | 290 | 76.6 | 69.1 | 0.14 | 0.27 | 2.28 |
| dense-r | count | 41425 | 127.2 (124.6-136.0) | 234.9 (232.3-237.5) | 1.85 | 120 | 220 | 12.9 | 38.1 | 0.10 | 0.20 | 2.79 |
| dense-r | devnull | 41425 | 130.6 (129.5-132.1) | 243.6 (237.0-251.0) | 1.86 | 120 | 230 | 12.8 | 42.2 | 0.11 | 0.21 | 2.71 |
| dense-r | file | 41425 | 130.4 (129.8-137.2) | 242.3 (238.3-252.1) | 1.86 | 120 | 230 | 12.9 | 42.5 | 0.11 | 0.22 | 2.71 |
| dense-r-lift | count | 55985 | 130.3 (129.3-143.3) | 247.4 (239.6-262.7) | 1.90 | 120 | 230 | 12.9 | 40.2 | 0.10 | 0.21 | 2.76 |
| dense-r-lift | devnull | 55985 | 144.0 (136.1-151.8) | 254.3 (252.1-273.6) | 1.77 | 130 | 240 | 13.0 | 42.5 | 0.12 | 0.22 | 2.66 |
| dense-r-lift | file | 55985 | 138.8 (137.4-142.0) | 254.8 (253.8-259.5) | 1.84 | 130 | 240 | 13.0 | 42.4 | 0.11 | 0.22 | 2.66 |
| dense-s-As-l0 | count | 1050282 | 142.5 (140.1-144.9) | 255.5 (254.6-258.7) | 1.79 | 130 | 240 | 39.1 | 44.0 | 0.11 | 0.22 | 2.64 |
| dense-s-As-l0 | devnull | 1050282 | 200.3 (198.5-213.7) | 400.7 (392.5-408.4) | 2.00 | 180 | 380 | 39.1 | 44.1 | 0.17 | 0.36 | 2.48 |
| dense-s-As-l0 | file | 1050282 | 209.9 (208.0-235.1) | 405.9 (401.1-435.3) | 1.93 | 200 | 390 | 39.1 | 44.1 | 0.18 | 0.37 | 2.43 |
| dense-s-As | count | 1050282 | 114.9 (114.1-118.3) | 203.5 (202.9-208.9) | 1.77 | 100 | 190 | 36.9 | 44.3 | 0.09 | 0.17 | 2.70 |
| dense-s-As | devnull | 1050282 | 172.8 (171.9-173.4) | 339.3 (337.4-344.8) | 1.96 | 160 | 320 | 37.0 | 44.8 | 0.14 | 0.31 | 2.49 |
| dense-s-As | file | 1050282 | 207.2 (178.8-229.6) | 363.2 (346.9-377.1) | 1.75 | 170 | 340 | 37.1 | 39.2 | 0.16 | 0.34 | 2.43 |
| dense-s-Ac-j | count | 1050282 | 127.6 (126.1-131.3) | 227.3 (224.7-228.1) | 1.78 | 110 | 210 | 60.7 | 47.0 | 0.10 | 0.19 | 2.74 |
| dense-s-Ac-j | devnull | 1050282 | 186.9 (184.8-194.7) | 363.4 (361.5-373.1) | 1.94 | 170 | 350 | 60.7 | 43.8 | 0.16 | 0.34 | 2.50 |
| dense-s-Ac-j | file | 1050282 | 209.2 (203.7-222.9) | 395.1 (378.4-456.0) | 1.89 | 190 | 380 | 60.7 | 45.7 | 0.18 | 0.37 | 2.45 |
| dense-s-Ac | count | 1050282 | 121.1 (120.4-125.8) | 204.5 (203.9-207.9) | 1.69 | 110 | 190 | 54.5 | 43.7 | 0.09 | 0.18 | 2.80 |
| dense-s-Ac | devnull | 1050282 | 179.8 (178.5-187.0) | 342.4 (339.2-348.5) | 1.90 | 160 | 330 | 54.5 | 41.3 | 0.15 | 0.31 | 2.52 |
| dense-s-Ac | file | 1050282 | 188.2 (187.5-193.2) | 351.7 (349.0-359.5) | 1.87 | 180 | 340 | 54.5 | 41.3 | 0.16 | 0.32 | 2.47 |
| dense-s-Ad | count | 1050282 | 170.9 (169.0-173.9) | 286.0 (280.9-290.9) | 1.67 | 160 | 270 | 12.3 | 37.4 | 0.14 | 0.26 | 2.67 |
| dense-s-Ad | devnull | 1050282 | 229.8 (228.6-236.2) | 423.2 (420.6-447.3) | 1.84 | 220 | 410 | 12.4 | 39.7 | 0.20 | 0.40 | 2.50 |
| dense-s-Ad | file | 1050282 | 254.0 (247.3-263.5) | 457.3 (444.5-461.9) | 1.80 | 230 | 420 | 12.3 | 41.7 | 0.21 | 0.43 | 2.46 |
| dense-s-At | count | 1050282 | 210.2 (206.9-214.1) | 304.5 (299.6-311.5) | 1.45 | 190 | 290 | 84.1 | 51.3 | 0.18 | 0.27 | 2.19 |
| dense-s-At | devnull | 1050282 | 272.8 (269.9-280.5) | 446.5 (441.3-477.9) | 1.64 | 250 | 430 | 84.2 | 51.1 | 0.24 | 0.42 | 2.22 |
| dense-s-At | file | 1050282 | 291.1 (279.4-307.2) | 453.0 (446.6-480.7) | 1.56 | 270 | 440 | 84.2 | 51.4 | 0.26 | 0.42 | 2.19 |
| dense-c-As-l0 | count | 132841 | 172.7 (170.7-174.5) | 299.8 (298.6-304.4) | 1.74 | 160 | 290 | 40.2 | 39.2 | 0.14 | 0.26 | 2.52 |
| dense-c-As-l0 | devnull | 132841 | 183.7 (181.3-185.0) | 322.9 (316.2-326.9) | 1.76 | 170 | 310 | 40.2 | 40.2 | 0.15 | 0.29 | 2.47 |
| dense-c-As-l0 | file | 132841 | 186.0 (183.3-192.9) | 319.9 (318.2-333.6) | 1.72 | 170 | 310 | 40.2 | 44.1 | 0.16 | 0.29 | 2.46 |
| dense-c-As | count | 132841 | 169.1 (167.8-170.7) | 277.6 (275.8-281.5) | 1.64 | 150 | 260 | 38.5 | 44.5 | 0.14 | 0.24 | 2.39 |
| dense-c-As | devnull | 132841 | 180.1 (177.6-182.7) | 294.8 (293.9-299.0) | 1.64 | 170 | 280 | 38.5 | 42.4 | 0.15 | 0.26 | 2.36 |
| dense-c-As | file | 132841 | 181.1 (178.2-186.0) | 295.4 (292.6-296.3) | 1.63 | 170 | 280 | 38.5 | 45.2 | 0.15 | 0.26 | 2.36 |
| dense-c-Ac-j | count | 132841 | 161.8 (159.2-165.1) | 280.0 (274.7-282.6) | 1.73 | 150 | 260 | 62.2 | 47.2 | 0.13 | 0.25 | 2.54 |
| dense-c-Ac-j | devnull | 132841 | 169.5 (167.6-174.1) | 287.8 (285.7-295.3) | 1.70 | 150 | 280 | 62.3 | 47.2 | 0.14 | 0.25 | 2.49 |
| dense-c-Ac-j | file | 132841 | 175.1 (171.1-189.3) | 297.9 (291.5-310.6) | 1.70 | 160 | 280 | 62.2 | 47.6 | 0.14 | 0.27 | 2.48 |
| dense-c-Ac | count | 132841 | 172.6 (166.3-174.6) | 289.3 (276.3-292.3) | 1.68 | 160 | 270 | 62.2 | 47.4 | 0.14 | 0.26 | 2.54 |
| dense-c-Ac | devnull | 132841 | 171.6 (167.6-174.5) | 289.2 (287.2-295.1) | 1.69 | 160 | 280 | 62.2 | 45.6 | 0.14 | 0.25 | 2.49 |
| dense-c-Ac | file | 132841 | 169.9 (169.6-174.5) | 294.5 (290.9-308.0) | 1.73 | 150 | 280 | 62.2 | 42.2 | 0.14 | 0.27 | 2.48 |
| dense-c-Ad | count | 132841 | 169.4 (167.9-171.8) | 278.9 (276.6-282.3) | 1.65 | 160 | 260 | 38.5 | 39.9 | 0.14 | 0.25 | 2.39 |
| dense-c-Ad | devnull | 132841 | 177.7 (176.9-181.9) | 296.3 (293.9-297.1) | 1.67 | 160 | 280 | 38.5 | 44.8 | 0.15 | 0.26 | 2.36 |
| dense-c-Ad | file | 132841 | 181.3 (180.8-182.5) | 296.6 (293.8-297.8) | 1.64 | 170 | 280 | 38.5 | 40.5 | 0.15 | 0.27 | 2.36 |
| dense-c-At | count | 132841 | 243.0 (241.1-245.8) | 351.3 (350.4-376.6) | 1.45 | 220 | 340 | 85.2 | 51.7 | 0.21 | 0.32 | 2.15 |
| dense-c-At | devnull | 132841 | 263.4 (252.6-264.7) | 371.0 (366.5-386.5) | 1.41 | 240 | 360 | 85.3 | 46.9 | 0.23 | 0.34 | 2.14 |
| dense-c-At | file | 132841 | 253.3 (250.0-262.4) | 369.3 (365.0-372.3) | 1.46 | 230 | 360 | 85.3 | 49.0 | 0.22 | 0.33 | 2.13 |
| long-s | count | 34285 | 686.6 (666.8-705.0) | 792.3 (785.8-836.0) | 1.15 | 670 | 790 | 341.8 | 86.8 | 0.65 | 0.76 | 1.99 |
| long-s | devnull | 34285 | 691.2 (664.5-735.9) | 794.3 (781.8-830.2) | 1.15 | 670 | 790 | 341.8 | 87.1 | 0.65 | 0.76 | 1.99 |
| long-s | file | 34285 | 677.7 (668.7-689.7) | 801.3 (798.7-802.7) | 1.18 | 660 | 790 | 341.8 | 87.0 | 0.64 | 0.77 | 1.99 |
| long-c | count | 34285 | 626.7 (596.0-668.0) | 756.2 (752.6-818.1) | 1.21 | 610 | 750 | 344.1 | 87.5 | 0.59 | 0.72 | 2.10 |
| long-c | devnull | 34285 | 624.5 (596.3-644.9) | 755.8 (754.6-776.8) | 1.21 | 610 | 750 | 344.1 | 96.8 | 0.59 | 0.72 | 2.09 |
| long-c | file | 34285 | 644.2 (630.0-724.4) | 849.6 (766.9-860.7) | 1.32 | 620 | 810 | 344.2 | 96.7 | 0.60 | 0.81 | 2.09 |
| long-m | count | 33885 | 619.1 (603.4-692.1) | 769.2 (753.9-811.2) | 1.24 | 600 | 760 | 344.1 | 87.4 | 0.58 | 0.73 | 2.09 |
| long-m | devnull | 33885 | 629.4 (598.9-671.5) | 766.3 (764.8-786.0) | 1.22 | 620 | 760 | 344.1 | 87.4 | 0.59 | 0.73 | 2.09 |
| long-m | file | 33885 | 609.6 (595.7-645.7) | 753.2 (744.8-773.8) | 1.24 | 600 | 750 | 344.2 | 97.2 | 0.57 | 0.72 | 2.08 |
| weighted-s | count | 10343 | 24.6 (23.5-26.2) | 49.9 (49.4-51.3) | 2.02 | 10 | 40 | 11.4 | 23.8 | 0.01 | 0.03 | 3.45 |
| weighted-s | devnull | 10343 | 27.0 (24.0-48.7) | 57.4 (50.5-77.0) | 2.13 | 20 | 40 | 11.4 | 23.8 | 0.01 | 0.03 | 3.37 |
| weighted-s | file | 10343 | 27.7 (25.9-40.1) | 55.4 (54.4-139.1) | 2.00 | 20 | 40 | 11.5 | 24.2 | 0.01 | 0.03 | 3.34 |
| weighted-c | count | 10323 | 28.6 (24.6-40.4) | 54.6 (51.6-69.5) | 1.91 | 20 | 40 | 11.9 | 24.1 | 0.01 | 0.04 | 3.37 |
| weighted-c | devnull | 10323 | 27.9 (25.7-30.0) | 58.5 (56.3-66.8) | 2.10 | 20 | 50 | 12.0 | 24.2 | 0.01 | 0.04 | 3.29 |
| weighted-c | file | 10323 | 26.3 (25.3-28.1) | 52.6 (52.4-54.5) | 2.00 | 10 | 40 | 11.9 | 24.0 | 0.01 | 0.03 | 3.27 |

### Go suite, all rows

`benchstat` over 6 runs (± = maximum relative deviation). MB/s is the
input file size per op for file sources. The `MaxSize` rows were
re-measured on `3e5d372` (size-limited closed/maximal search with the
extension check; base side of the final base-vs-final run, n = 6); at
`c16c2f5` (full exploration) closed took 4.39 s (n=3) / 4.43 s (n=5)
and 157 MB, maximal 2.65 s / 2.66 s.

| benchmark | time/op | B/op | allocs/op | results | MB/s |
| --- | ---: | ---: | ---: | ---: | ---: |
| Targets/sparse/s=0.5/frequent | 20.5ms ± 1% | 22.7MB ± 0% | 226 ± 0% | 2.97k ± 0% |  |
| Targets/sparse/s=0.5/closed | 20.9ms ± 1% | 23.4MB ± 0% | 290 ± 0% | 2.97k ± 0% |  |
| Targets/sparse/s=0.5/maximal | 21.7ms ± 6% | 23.4MB ± 0% | 289 ± 0% | 1.03k ± 0% |  |
| Targets/sparse/s=0.5/generators | 20.8ms ± 2% | 23.2MB ± 0% | 248 ± 0% | 2.97k ± 0% |  |
| Targets/sparse/s=0.1/frequent | 31.3ms ± 2% | 27.8MB ± 0% | 247 ± 0% | 27.7k ± 0% |  |
| Targets/sparse/s=0.1/closed | 37.3ms ± 1% | 28.8MB ± 0% | 331 ± 0% | 26.8k ± 0% |  |
| Targets/sparse/s=0.1/maximal | 34.8ms ± 3% | 28.5MB ± 0% | 325 ± 0% | 8.47k ± 0% |  |
| Targets/sparse/s=0.1/generators | 38.9ms ± 7% | 32.4MB ± 0% | 280 ± 0% | 26.9k ± 0% |  |
| Targets/dense/s=10/frequent | 180ms ± 3% | 72.5MB ± 0% | 212 ± 0% | 1.05M ± 0% |  |
| Targets/dense/s=10/closed | 225ms ± 1% | 76.2MB ± 0% | 339 ± 0% | 133k ± 0% |  |
| Targets/dense/s=10/maximal | 184ms ± 5% | 73.1MB ± 0% | 294 ± 0% | 1.54k ± 0% |  |
| Targets/dense/s=10/generators | 224ms ± 9% | 124MB ± 0% | 259 ± 0% | 194k ± 0% |  |
| Targets/dense/s=5/frequent | 544ms ± 0% | 72.5MB ± 0% | 238 ± 0% | 1.29M ± 0% |  |
| Targets/dense/s=5/closed | 750ms ± 4% | 77.9MB ± 0% | 343 ± 0% | 342k ± 0% |  |
| Targets/dense/s=5/maximal | 674ms ± 6% | 74.3MB ± 0% | 321 ± 0% | 48.1k ± 0% |  |
| Targets/dense/s=5/generators | 667ms ± 9% | 175MB ± 0% | 289 ± 0% | 412k ± 0% |  |
| Targets/long/s=3/frequent | 226ms ± 4% | 129MB ± 0% | 197 ± 1% | 2.94k ± 0% |  |
| Targets/long/s=3/closed | 218ms ± 2% | 129MB ± 0% | 244 ± 1% | 2.94k ± 0% |  |
| Targets/long/s=3/maximal | 218ms ± 2% | 129MB ± 0% | 244 ± 0% | 2.56k ± 0% |  |
| Targets/long/s=3/generators | 234ms ±12% | 129MB ± 0% | 218 ± 0% | 2.94k ± 0% |  |
| Targets/long/s=2.5/frequent | 704ms ± 1% | 129MB ± 0% | 278 ± 0% | 34.3k ± 0% |  |
| Targets/long/s=2.5/closed | 681ms ± 4% | 130MB ± 0% | 271 ± 0% | 34.3k ± 0% |  |
| Targets/long/s=2.5/maximal | 666ms ± 2% | 130MB ± 0% | 271 ± 0% | 33.9k ± 0% |  |
| Targets/long/s=2.5/generators | 726ms ± 5% | 136MB ± 0% | 311 ± 0% | 34.3k ± 0% |  |
| Targets/weighted/s=0.2/frequent | 24.3ms ± 1% | 27.1MB ± 0% | 237 ± 0% | 10.3k ± 0% |  |
| Targets/weighted/s=0.2/closed | 25.9ms ± 0% | 27.9MB ± 0% | 315 ± 0% | 10.3k ± 0% |  |
| Targets/weighted/s=0.2/maximal | 25.1ms ± 1% | 27.8MB ± 0% | 311 ± 0% | 3.29k ± 0% |  |
| Targets/weighted/s=0.2/generators | 26.2ms ± 8% | 29.2MB ± 0% | 265 ± 0% | 10.3k ± 0% |  |
| MaxSize/dense/s=2/frequent/n=3 (`3e5d372`) | 609ms ± 7% | 72.5MB ± 0% | 246 ± 0% | 54.8k ± 0% |  |
| MaxSize/dense/s=2/frequent/n=5 (`3e5d372`) | 1.25s ± 1% | 72.6MB ± 0% | 288 ± 0% | 1.09M ± 0% |  |
| MaxSize/dense/s=2/closed/n=3 (`3e5d372`) | 731ms ± 7% | 76.6MB ± 0% | 312 ± 0% | 54.8k ± 0% |  |
| MaxSize/dense/s=2/closed/n=5 (`3e5d372`) | 3.65s ± 2% | 85.9MB ± 0% | 375 ± 0% | 1.01M ± 0% |  |
| MaxSize/dense/s=2/maximal/n=3 (`3e5d372`) | 922ms ± 3% | 76.6MB ± 0% | 312 ± 0% | 11.2k ± 0% |  |
| MaxSize/dense/s=2/maximal/n=5 (`3e5d372`) | 2.72s ± 1% | 84.0MB ± 0% | 374 ± 0% | 45.5k ± 0% |  |
| Variants/dense/s=10/frequent/simple | 239ms ± 5% | 62.8MB ± 0% | 301 ± 0% | 1.05M ± 0% |  |
| Variants/dense/s=10/frequent/simple+fim16 | 186ms ± 1% | 63.5MB ± 0% | 256 ± 0% | 1.05M ± 0% |  |
| Variants/dense/s=10/frequent/complex | 211ms ± 5% | 72.5MB ± 0% | 212 ± 0% | 1.05M ± 0% |  |
| Variants/dense/s=10/frequent/complex+reorder | 188ms ± 9% | 72.5MB ± 0% | 212 ± 0% | 1.05M ± 0% |  |
| Variants/dense/s=10/frequent/single | 287ms ± 1% | 49.9MB ± 0% | 181 ± 0% | 1.05M ± 0% |  |
| Variants/dense/s=10/frequent/topdown | 320ms ± 3% | 62.8MB ± 0% | 211 ± 0% | 1.05M ± 0% |  |
| Variants/dense/s=10/closed/simple | 290ms ± 6% | 65.9MB ± 0% | 328 ± 0% | 133k ± 0% |  |
| Variants/dense/s=10/closed/simple+fim16 | 267ms ± 7% | 67.2MB ± 0% | 355 ± 0% | 133k ± 0% |  |
| Variants/dense/s=10/closed/complex | 255ms ± 3% | 76.2MB ± 0% | 339 ± 0% | 133k ± 0% |  |
| Variants/dense/s=10/closed/complex+reorder | 252ms ± 2% | 76.2MB ± 0% | 339 ± 0% | 133k ± 0% |  |
| Variants/dense/s=10/closed/single | 243ms ± 2% | 67.2MB ± 0% | 355 ± 0% | 133k ± 0% |  |
| Variants/dense/s=10/closed/topdown | 313ms ± 9% | 65.9MB ± 0% | 328 ± 0% | 133k ± 0% |  |
| Variants/dense/s=10/maximal/simple | 179ms ± 4% | 62.8MB ± 0% | 248 ± 0% | 1.54k ± 0% |  |
| Variants/dense/s=10/maximal/simple+fim16 | 193ms ± 0% | 64.1MB ± 0% | 310 ± 0% | 1.54k ± 0% |  |
| Variants/dense/s=10/maximal/complex | 187ms ± 5% | 73.1MB ± 0% | 294 ± 0% | 1.54k ± 0% |  |
| Variants/dense/s=10/maximal/complex+reorder | 193ms ± 4% | 73.1MB ± 0% | 294 ± 0% | 1.54k ± 0% |  |
| Variants/dense/s=10/maximal/single | 199ms ± 2% | 64.1MB ± 0% | 310 ± 0% | 1.54k ± 0% |  |
| Variants/dense/s=10/maximal/topdown | 243ms ± 2% | 62.8MB ± 0% | 248 ± 0% | 1.54k ± 0% |  |
| Variants/sparse/s=0.1/frequent/simple | 34.5ms ± 0% | 24.1MB ± 0% | 286 ± 0% | 27.7k ± 0% |  |
| Variants/sparse/s=0.1/frequent/simple+fim16 | 30.3ms ± 1% | 24.8MB ± 0% | 254 ± 0% | 27.7k ± 0% |  |
| Variants/sparse/s=0.1/frequent/complex | 33.6ms ± 4% | 27.8MB ± 0% | 247 ± 0% | 27.7k ± 0% |  |
| Variants/sparse/s=0.1/frequent/complex+reorder | 31.7ms ± 1% | 27.8MB ± 0% | 247 ± 0% | 27.7k ± 0% |  |
| Variants/sparse/s=0.1/frequent/single | 27.8ms ± 1% | 20.3MB ± 0% | 224 ± 0% | 27.7k ± 0% |  |
| Variants/sparse/s=0.1/frequent/topdown | 81.9ms ± 1% | 24.1MB ± 0% | 272 ± 0% | 27.7k ± 0% |  |
| Rules/sparse/s=0.1/c=60 | 51.9ms ± 2% | 56.5MB ± 0% | 391 ± 0% | 52.1k ± 0% |  |
| Rules/sparse/s=0.05/c=50/lift | 120ms ± 1% | 126MB ± 0% | 451 ± 0% | 130k ± 0% |  |
| Rules/dense/s=10/c=90/n=4 | 194ms ± 3% | 53.3MB ± 0% | 276 ± 0% | 41.4k ± 0% |  |
| Rules/dense/s=10/c=80/n=4/lift | 202ms ± 0% | 53.8MB ± 0% | 278 ± 0% | 56.0k ± 0% |  |
| Sources/sparse/s=0.1/memory-string | 31.7ms ± 2% | 27.8MB ± 0% | 247 ± 0% | 27.7k ± 0% |  |
| Sources/sparse/s=0.1/memory-int | 31.2ms ± 9% | 27.7MB ± 0% | 247 ± 0% | 27.7k ± 0% |  |
| Sources/sparse/s=0.1/table-file | 48.3ms ± 1% | 28.2MB ± 0% | 2.34k ± 0% | 27.7k ± 0% | 12.1MB/s ± 1% |
| Sources/sparse/s=0.1/csv-file | 47.4ms ± 1% | 29.1MB ± 0% | 42.4k ± 0% | 27.7k ± 0% | 12.4MB/s ± 1% |
| Sources/sparse-large/s=0.05/memory-string | 379ms ±14% | 269MB ± 0% | 1.37k ± 0% | 37.5k ± 0% |  |
| Sources/sparse-large/s=0.05/memory-int | 351ms ± 8% | 267MB ± 0% | 1.37k ± 0% | 37.5k ± 0% |  |
| Sources/sparse-large/s=0.05/table-file | 570ms ± 3% | 275MB ± 0% | 41.7k ± 0% | 37.5k ± 0% | 15.0MB/s ± 3% |
| Sources/sparse-large/s=0.05/csv-file | 587ms ± 3% | 289MB ± 0% | 442k ± 0% | 37.5k ± 0% | 14.6MB/s ± 3% |
| Sources/weighted/s=0.2/memory-string | 24.8ms ± 2% | 27.1MB ± 0% | 237 ± 0% | 10.3k ± 0% |  |
| Sources/weighted/s=0.2/memory-int | 23.4ms ± 6% | 27.0MB ± 0% | 237 ± 0% | 10.3k ± 0% |  |
| Sources/weighted/s=0.2/table-file | 42.7ms ± 1% | 27.6MB ± 0% | 2.33k ± 0% | 10.3k ± 0% | 15.0MB/s ± 1% |
| Sources/weighted/s=0.2/csv-file | 42.0ms ± 2% | 28.5MB ± 0% | 42.3k ± 0% | 10.3k ± 0% | 15.1MB/s ± 4% |
| ReadOnly/sparse-large/memory-string | 48.0ms ± 1% | 7.47MB ± 0% | 187 ± 0% |  |  |
| ReadOnly/sparse-large/table-file | 146ms ± 3% | 10.3MB ± 0% | 20.4k ± 0% |  | 58.7MB/s ± 3% |
| ReadOnly/sparse-large/csv-file | 142ms ± 1% | 17.7MB ± 0% | 220k ± 0% |  | 60.4MB/s ± 1% |
| ReadOnly/long/memory-string | 8.44ms ± 1% | 105kB ± 0% | 46.0 ± 0% |  |  |
| ReadOnly/long/table-file | 29.2ms ± 1% | 225kB ± 0% | 493 ± 0% |  | 79.4MB/s ± 1% |
| ReadOnly/long/csv-file | 26.9ms ± 1% | 1.94MB ± 0% | 10.5k ± 0% |  | 86.1MB/s ± 1% |
| Consume/dense/s=5/frequent/callback | 577ms ± 4% | 72.5MB ± 0% | 238 ± 0% | 1.29M ± 0% |  |
| Consume/dense/s=5/frequent/iterator | 569ms ± 1% | 72.5MB ± 0% | 239 ± 0% | 1.29M ± 0% |  |
| Consume/dense/s=5/frequent/collect | 686ms ± 2% | 512MB ± 0% | 1.29M ± 0% | 1.29M ± 0% |  |
| Consume/dense/s=5/frequent/internal-nosink | 566ms ± 6% | 72.5MB ± 0% | 218 ± 0% | 1.29M ± 0% |  |
| Consume/dense/s=5/closed/callback | 793ms ± 6% | 77.9MB ± 0% | 343 ± 0% | 342k ± 0% |  |
| Consume/dense/s=5/closed/iterator | 767ms ± 1% | 77.9MB ± 0% | 344 ± 0% | 342k ± 0% |  |
| Consume/dense/s=5/closed/collect | 792ms ± 2% | 176MB ± 0% | 342k ± 0% | 342k ± 0% |  |
| Consume/dense/s=5/closed/internal-nosink | 765ms ± 3% | 77.9MB ± 0% | 319 ± 0% | 342k ± 0% |  |
| Consume/sparse/s=0.1/frequent/callback | 31.5ms ± 1% | 27.8MB ± 0% | 247 ± 0% | 27.7k ± 0% |  |
| Consume/sparse/s=0.1/frequent/iterator | 31.6ms ± 1% | 27.8MB ± 0% | 249 ± 0% | 27.7k ± 0% |  |
| Consume/sparse/s=0.1/frequent/collect | 33.8ms ± 4% | 34.5MB ± 0% | 28.0k ± 0% | 27.7k ± 0% |  |
| Consume/sparse/s=0.1/frequent/internal-nosink | 31.6ms ± 3% | 27.8MB ± 0% | 228 ± 0% | 27.7k ± 0% |  |
| Consume/dense/s=10/c=90/n=4/rules/callback | 195ms ± 4% | 53.3MB ± 0% | 276 ± 0% | 41.4k ± 0% |  |
| Consume/dense/s=10/c=90/n=4/rules/iterator | 192ms ± 1% | 53.3MB ± 0% | 278 ± 0% | 41.4k ± 0% |  |
| Consume/dense/s=10/c=90/n=4/rules/collect | 202ms ± 8% | 72.1MB ± 0% | 41.7k ± 0% | 41.4k ± 0% |  |
| Consume/dense/s=10/c=90/n=4/rules/internal-nosink | 195ms ± 1% | 53.3MB ± 0% | 254 ± 0% | 41.4k ± 0% |  |

### Go vs C after optimization, all rows

`reference/bench/compare.sh -n 5 -w 1` on the final code (load averages
5.5 at the start, 2.6 at the end). Medians of 5 runs after 1 warmup;
wall ms with (min–max). See [the note on the "mine s"
column](#go-vs-c-after-optimization).

| config | mode | patterns | C wall ms (min-max) | Go wall ms (min-max) | Go/C wall | C user+sys ms | Go user+sys ms | C maxRSS MB | Go maxRSS MB | C mine s | Go mine s | Go/C instr |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| sparse-s | count | 27703 | 26.0 (25.8-26.4) | 48.9 (48.8-49.1) | 1.88 | 20 | 40 | 12.3 | 22.2 | 0.01 | 0.03 | 3.41 |
| sparse-s | devnull | 27703 | 29.0 (27.9-31.7) | 52.6 (51.4-57.6) | 1.81 | 20 | 40 | 12.3 | 22.0 | 0.01 | 0.04 | 3.18 |
| sparse-s | file | 27703 | 28.0 (27.7-30.5) | 51.7 (51.5-52.0) | 1.85 | 20 | 40 | 12.3 | 22.0 | 0.01 | 0.04 | 3.17 |
| sparse-c | count | 26775 | 29.5 (29.2-34.1) | 54.7 (54.6-56.4) | 1.86 | 20 | 40 | 13.4 | 22.3 | 0.02 | 0.04 | 3.20 |
| sparse-c | devnull | 26775 | 31.0 (30.8-31.6) | 57.5 (56.6-57.7) | 1.86 | 20 | 50 | 13.5 | 22.4 | 0.02 | 0.04 | 3.03 |
| sparse-c | file | 26775 | 32.5 (31.2-35.9) | 58.1 (57.2-58.3) | 1.79 | 20 | 50 | 13.4 | 22.4 | 0.02 | 0.04 | 3.02 |
| sparse-m | count | 8468 | 27.8 (27.6-28.1) | 52.0 (51.6-52.5) | 1.87 | 20 | 40 | 13.4 | 22.4 | 0.01 | 0.04 | 3.27 |
| sparse-m | devnull | 8468 | 28.6 (28.5-29.7) | 53.6 (52.8-55.2) | 1.88 | 20 | 40 | 13.4 | 22.6 | 0.02 | 0.04 | 3.20 |
| sparse-m | file | 8468 | 28.8 (28.6-29.5) | 53.2 (53.1-53.6) | 1.85 | 20 | 40 | 13.4 | 22.3 | 0.01 | 0.04 | 3.19 |
| sparse-g | count | 26853 | 32.4 (29.1-34.9) | 55.7 (52.4-58.0) | 1.72 | 20 | 40 | 22.3 | 23.2 | 0.02 | 0.04 | 3.02 |
| sparse-g | devnull | 26853 | 32.1 (31.3-34.9) | 55.0 (54.9-62.9) | 1.71 | 20 | 40 | 22.3 | 23.4 | 0.02 | 0.04 | 2.88 |
| sparse-g | file | 26853 | 33.6 (31.3-34.0) | 58.2 (54.9-59.4) | 1.73 | 20 | 50 | 22.3 | 24.1 | 0.02 | 0.04 | 2.86 |
| sparse-r | count | 52149 | 36.2 (35.0-37.8) | 69.4 (67.5-71.4) | 1.92 | 30 | 60 | 12.1 | 30.4 | 0.02 | 0.06 | 2.72 |
| sparse-r | devnull | 52149 | 41.5 (41.2-44.3) | 74.8 (74.4-76.5) | 1.80 | 30 | 60 | 12.1 | 32.1 | 0.03 | 0.06 | 2.44 |
| sparse-r | file | 52149 | 42.7 (42.1-45.9) | 75.8 (75.1-77.7) | 1.78 | 30 | 60 | 12.1 | 30.1 | 0.03 | 0.06 | 2.43 |
| sparse-r-lift | count | 130194 | 77.2 (76.4-84.5) | 131.5 (129.5-140.4) | 1.70 | 60 | 120 | 21.5 | 52.5 | 0.06 | 0.11 | 2.29 |
| sparse-r-lift | devnull | 130194 | 92.1 (91.2-102.7) | 147.6 (145.4-162.8) | 1.60 | 80 | 130 | 21.5 | 52.6 | 0.08 | 0.13 | 2.08 |
| sparse-r-lift | file | 130194 | 106.5 (105.0-107.8) | 164.3 (163.5-169.1) | 1.54 | 90 | 160 | 21.5 | 52.7 | 0.09 | 0.14 | 2.07 |
| large-s | count | 37505 | 308.7 (285.5-375.9) | 573.3 (543.3-856.6) | 1.86 | 300 | 570 | 77.0 | 106.6 | 0.18 | 0.42 | 3.93 |
| large-s | devnull | 37505 | 287.5 (277.4-296.5) | 556.0 (534.0-618.6) | 1.93 | 280 | 550 | 76.6 | 106.5 | 0.17 | 0.41 | 3.89 |
| large-s | file | 37505 | 286.4 (280.9-296.5) | 552.3 (546.1-553.2) | 1.93 | 270 | 550 | 76.6 | 106.6 | 0.17 | 0.40 | 3.88 |
| large-c | count | 37504 | 276.7 (273.5-306.5) | 576.0 (547.9-598.1) | 2.08 | 260 | 570 | 77.2 | 106.2 | 0.16 | 0.43 | 3.91 |
| large-c | devnull | 37504 | 285.6 (279.0-299.5) | 553.3 (548.0-588.7) | 1.94 | 270 | 550 | 77.3 | 106.4 | 0.17 | 0.41 | 3.86 |
| large-c | file | 37504 | 293.7 (277.6-321.7) | 589.7 (542.7-594.1) | 2.01 | 270 | 590 | 77.2 | 106.8 | 0.18 | 0.42 | 3.86 |
| large-m | count | 11838 | 272.3 (268.5-297.0) | 550.1 (527.9-565.5) | 2.02 | 250 | 550 | 77.2 | 103.6 | 0.16 | 0.41 | 3.94 |
| large-m | devnull | 11838 | 281.3 (275.0-322.5) | 551.2 (536.7-602.2) | 1.96 | 270 | 550 | 77.2 | 105.0 | 0.17 | 0.40 | 3.92 |
| large-m | file | 11838 | 284.0 (272.6-294.2) | 544.9 (534.9-559.2) | 1.92 | 270 | 540 | 77.2 | 106.5 | 0.17 | 0.40 | 3.91 |
| dense-s | count | 1294276 | 357.5 (351.5-363.7) | 439.9 (437.6-447.1) | 1.23 | 340 | 430 | 99.8 | 42.9 | 0.33 | 0.41 | 2.16 |
| dense-s | devnull | 1294276 | 428.1 (424.7-439.4) | 582.8 (579.2-586.6) | 1.36 | 420 | 570 | 99.8 | 42.7 | 0.40 | 0.55 | 2.08 |
| dense-s | file | 1294276 | 467.8 (439.4-494.7) | 618.3 (602.3-673.8) | 1.32 | 440 | 590 | 99.8 | 43.3 | 0.42 | 0.58 | 2.06 |
| dense-c | count | 342016 | 467.5 (462.0-472.7) | 619.5 (616.9-629.4) | 1.33 | 460 | 600 | 107.7 | 46.1 | 0.44 | 0.59 | 2.09 |
| dense-c | devnull | 342016 | 492.6 (489.5-498.2) | 652.9 (649.6-670.7) | 1.33 | 480 | 640 | 107.8 | 45.9 | 0.46 | 0.62 | 2.06 |
| dense-c | file | 342016 | 496.2 (493.1-537.0) | 655.5 (653.9-670.6) | 1.32 | 480 | 650 | 107.7 | 46.1 | 0.47 | 0.62 | 2.05 |
| dense-m | count | 48051 | 418.5 (414.3-443.6) | 547.6 (534.1-579.7) | 1.31 | 400 | 530 | 106.8 | 45.7 | 0.39 | 0.52 | 2.09 |
| dense-m | devnull | 48051 | 419.7 (415.4-432.6) | 536.9 (535.3-546.0) | 1.28 | 410 | 520 | 106.8 | 46.0 | 0.39 | 0.51 | 2.08 |
| dense-m | file | 48051 | 451.1 (421.0-464.2) | 542.3 (536.2-677.7) | 1.20 | 420 | 530 | 106.8 | 45.9 | 0.41 | 0.51 | 2.08 |
| dense-g | count | 194385 | 152.9 (146.5-161.5) | 241.9 (236.1-247.9) | 1.58 | 140 | 220 | 76.6 | 76.3 | 0.12 | 0.21 | 2.41 |
| dense-g | devnull | 194385 | 160.9 (158.6-168.6) | 256.3 (254.2-275.3) | 1.59 | 140 | 250 | 76.6 | 76.8 | 0.13 | 0.22 | 2.31 |
| dense-g | file | 194385 | 161.3 (160.6-170.3) | 260.5 (257.4-265.8) | 1.61 | 140 | 250 | 76.6 | 77.0 | 0.13 | 0.23 | 2.30 |
| dense-r | count | 41425 | 120.0 (119.7-121.9) | 214.3 (212.1-218.3) | 1.79 | 110 | 200 | 12.8 | 38.4 | 0.09 | 0.18 | 2.63 |
| dense-r | devnull | 41425 | 127.9 (124.5-142.5) | 222.2 (218.7-234.4) | 1.74 | 120 | 210 | 12.9 | 38.2 | 0.11 | 0.19 | 2.53 |
| dense-r | file | 41425 | 134.4 (124.6-138.1) | 236.7 (221.5-255.2) | 1.76 | 120 | 220 | 12.9 | 38.6 | 0.11 | 0.21 | 2.53 |
| dense-r-lift | count | 55985 | 131.3 (124.4-155.2) | 229.1 (220.0-235.2) | 1.74 | 120 | 220 | 13.0 | 38.6 | 0.10 | 0.19 | 2.60 |
| dense-r-lift | devnull | 55985 | 148.5 (140.0-155.1) | 255.1 (247.5-255.3) | 1.72 | 140 | 240 | 13.0 | 38.5 | 0.12 | 0.23 | 2.48 |
| dense-r-lift | file | 55985 | 133.8 (131.8-135.0) | 229.7 (226.5-235.4) | 1.72 | 120 | 220 | 13.0 | 38.4 | 0.11 | 0.20 | 2.47 |
| dense-s-As-l0 | count | 1050282 | 134.2 (133.7-150.8) | 212.6 (211.2-215.4) | 1.58 | 120 | 200 | 39.1 | 40.2 | 0.11 | 0.18 | 2.70 |
| dense-s-As-l0 | devnull | 1050282 | 192.0 (190.3-194.0) | 331.3 (330.3-345.2) | 1.73 | 180 | 320 | 39.1 | 40.1 | 0.16 | 0.30 | 2.36 |
| dense-s-As-l0 | file | 1050282 | 205.9 (200.0-217.3) | 349.3 (340.9-391.5) | 1.70 | 190 | 330 | 39.1 | 40.1 | 0.17 | 0.32 | 2.31 |
| dense-s-As | count | 1050282 | 109.8 (109.0-113.4) | 170.2 (169.3-171.7) | 1.55 | 100 | 160 | 37.0 | 40.1 | 0.08 | 0.14 | 2.71 |
| dense-s-As | devnull | 1050282 | 172.4 (167.2-182.1) | 303.4 (299.5-304.3) | 1.76 | 160 | 290 | 37.0 | 40.4 | 0.14 | 0.27 | 2.33 |
| dense-s-As | file | 1050282 | 196.7 (185.8-205.7) | 315.9 (310.8-342.5) | 1.61 | 170 | 300 | 37.0 | 40.2 | 0.16 | 0.28 | 2.29 |
| dense-s-Ac-j | count | 1050282 | 122.8 (121.6-124.9) | 187.8 (187.2-192.0) | 1.53 | 110 | 180 | 60.7 | 43.0 | 0.10 | 0.16 | 2.76 |
| dense-s-Ac-j | devnull | 1050282 | 178.9 (177.4-183.1) | 305.8 (305.3-307.0) | 1.71 | 160 | 290 | 60.8 | 43.0 | 0.15 | 0.27 | 2.34 |
| dense-s-Ac-j | file | 1050282 | 193.9 (192.9-218.6) | 341.7 (327.1-363.3) | 1.76 | 180 | 320 | 60.7 | 43.1 | 0.16 | 0.31 | 2.30 |
| dense-s-Ac | count | 1050282 | 122.0 (115.9-133.4) | 191.4 (181.3-204.2) | 1.57 | 110 | 180 | 54.5 | 42.5 | 0.09 | 0.16 | 2.89 |
| dense-s-Ac | devnull | 1050282 | 172.6 (170.9-179.5) | 300.7 (298.8-307.4) | 1.74 | 160 | 290 | 54.5 | 42.7 | 0.14 | 0.27 | 2.39 |
| dense-s-Ac | file | 1050282 | 183.5 (179.8-233.0) | 312.4 (308.4-316.4) | 1.70 | 170 | 300 | 54.5 | 42.8 | 0.15 | 0.28 | 2.34 |
| dense-s-Ad | count | 1050282 | 164.9 (162.7-172.7) | 268.5 (266.6-271.4) | 1.63 | 150 | 260 | 12.3 | 38.2 | 0.14 | 0.24 | 2.58 |
| dense-s-Ad | devnull | 1050282 | 224.1 (219.7-245.1) | 392.9 (384.6-412.3) | 1.75 | 210 | 380 | 12.4 | 38.3 | 0.20 | 0.36 | 2.32 |
| dense-s-Ad | file | 1050282 | 229.7 (228.1-233.4) | 394.3 (391.8-409.0) | 1.72 | 210 | 380 | 12.3 | 38.2 | 0.20 | 0.36 | 2.28 |
| dense-s-At | count | 1050282 | 201.4 (198.4-204.7) | 298.0 (282.6-304.3) | 1.48 | 180 | 280 | 84.1 | 47.1 | 0.17 | 0.27 | 2.19 |
| dense-s-At | devnull | 1050282 | 257.1 (254.9-259.6) | 407.2 (403.9-414.6) | 1.58 | 240 | 390 | 84.2 | 47.1 | 0.23 | 0.37 | 2.11 |
| dense-s-At | file | 1050282 | 267.7 (266.9-284.2) | 420.2 (413.8-432.1) | 1.57 | 250 | 400 | 84.1 | 47.0 | 0.24 | 0.39 | 2.08 |
| dense-c-As-l0 | count | 132841 | 172.1 (165.2-183.0) | 280.9 (257.3-291.5) | 1.63 | 160 | 260 | 40.2 | 42.4 | 0.14 | 0.25 | 2.56 |
| dense-c-As-l0 | devnull | 132841 | 194.1 (176.1-212.4) | 305.2 (270.5-382.7) | 1.57 | 180 | 280 | 40.3 | 42.6 | 0.16 | 0.28 | 2.48 |
| dense-c-As-l0 | file | 132841 | 177.6 (173.9-215.6) | 274.5 (269.0-277.2) | 1.55 | 160 | 260 | 40.2 | 42.4 | 0.15 | 0.24 | 2.48 |
| dense-c-As | count | 132841 | 163.6 (161.0-165.1) | 244.3 (242.3-263.3) | 1.49 | 150 | 230 | 38.5 | 42.9 | 0.13 | 0.21 | 2.42 |
| dense-c-As | devnull | 132841 | 174.1 (173.4-181.1) | 260.1 (259.6-271.9) | 1.49 | 160 | 250 | 38.5 | 43.0 | 0.15 | 0.23 | 2.36 |
| dense-c-As | file | 132841 | 173.1 (172.1-179.8) | 258.6 (255.7-261.3) | 1.49 | 160 | 240 | 38.5 | 43.0 | 0.14 | 0.22 | 2.35 |
| dense-c-Ac-j | count | 132841 | 152.7 (152.3-155.2) | 235.3 (233.5-236.0) | 1.54 | 140 | 220 | 62.2 | 44.2 | 0.12 | 0.20 | 2.56 |
| dense-c-Ac-j | devnull | 132841 | 163.9 (161.3-174.4) | 251.2 (248.6-259.1) | 1.53 | 150 | 240 | 62.2 | 44.0 | 0.14 | 0.22 | 2.47 |
| dense-c-Ac-j | file | 132841 | 173.9 (163.7-185.5) | 273.8 (252.8-292.0) | 1.57 | 160 | 260 | 62.2 | 44.2 | 0.14 | 0.25 | 2.47 |
| dense-c-Ac | count | 132841 | 168.1 (163.8-192.9) | 258.4 (254.9-293.2) | 1.54 | 150 | 240 | 62.3 | 44.1 | 0.14 | 0.23 | 2.56 |
| dense-c-Ac | devnull | 132841 | 170.8 (163.9-200.4) | 259.2 (256.4-274.5) | 1.52 | 150 | 250 | 62.3 | 44.1 | 0.14 | 0.23 | 2.47 |
| dense-c-Ac | file | 132841 | 168.2 (163.1-178.4) | 252.2 (249.2-270.7) | 1.50 | 150 | 240 | 62.2 | 44.0 | 0.14 | 0.22 | 2.46 |
| dense-c-Ad | count | 132841 | 163.0 (162.6-180.5) | 244.4 (242.1-264.1) | 1.50 | 150 | 230 | 38.5 | 42.8 | 0.13 | 0.21 | 2.42 |
| dense-c-Ad | devnull | 132841 | 171.3 (170.7-173.6) | 257.6 (255.8-262.7) | 1.50 | 160 | 240 | 38.5 | 43.1 | 0.14 | 0.23 | 2.36 |
| dense-c-Ad | file | 132841 | 174.9 (171.2-183.4) | 259.3 (257.0-264.8) | 1.48 | 160 | 240 | 38.5 | 42.9 | 0.15 | 0.23 | 2.35 |
| dense-c-At | count | 132841 | 240.3 (231.7-257.5) | 334.8 (334.3-351.4) | 1.39 | 220 | 320 | 85.2 | 47.6 | 0.21 | 0.30 | 2.15 |
| dense-c-At | devnull | 132841 | 242.4 (239.1-284.9) | 344.9 (342.6-385.7) | 1.42 | 220 | 330 | 85.2 | 47.3 | 0.21 | 0.31 | 2.12 |
| dense-c-At | file | 132841 | 243.1 (239.8-252.0) | 347.5 (344.0-361.1) | 1.43 | 230 | 330 | 85.2 | 47.6 | 0.21 | 0.32 | 2.12 |
| long-s | count | 34285 | 644.2 (641.1-664.5) | 723.4 (720.4-738.4) | 1.12 | 630 | 720 | 341.8 | 86.5 | 0.61 | 0.68 | 2.35 |
| long-s | devnull | 34285 | 671.8 (633.1-733.0) | 763.0 (737.3-896.0) | 1.14 | 650 | 750 | 341.8 | 86.6 | 0.63 | 0.72 | 2.34 |
| long-s | file | 34285 | 645.1 (637.5-656.4) | 738.3 (729.1-745.9) | 1.14 | 630 | 730 | 341.8 | 86.6 | 0.61 | 0.70 | 2.34 |
| long-c | count | 34285 | 584.0 (566.4-624.8) | 690.0 (688.1-720.6) | 1.18 | 570 | 680 | 344.1 | 86.6 | 0.55 | 0.65 | 2.43 |
| long-c | devnull | 34285 | 585.4 (566.0-608.5) | 690.0 (686.3-713.0) | 1.18 | 570 | 680 | 344.1 | 86.5 | 0.55 | 0.65 | 2.40 |
| long-c | file | 34285 | 572.1 (564.6-642.8) | 696.9 (696.3-705.6) | 1.22 | 560 | 690 | 344.2 | 86.6 | 0.54 | 0.66 | 2.40 |
| long-m | count | 33885 | 585.2 (575.8-609.9) | 692.4 (682.9-742.1) | 1.18 | 570 | 690 | 344.1 | 86.8 | 0.55 | 0.65 | 2.42 |
| long-m | devnull | 33885 | 593.9 (567.8-739.3) | 694.4 (688.9-697.2) | 1.17 | 570 | 690 | 344.1 | 86.4 | 0.56 | 0.66 | 2.40 |
| long-m | file | 33885 | 580.7 (574.5-591.7) | 690.4 (685.8-728.1) | 1.19 | 570 | 680 | 344.1 | 86.4 | 0.55 | 0.65 | 2.41 |
| weighted-s | count | 10343 | 25.4 (23.8-25.9) | 50.5 (49.5-50.7) | 1.99 | 10 | 40 | 11.4 | 22.1 | 0.01 | 0.03 | 3.44 |
| weighted-s | devnull | 10343 | 26.7 (25.0-29.5) | 51.6 (50.4-52.3) | 1.93 | 20 | 40 | 11.4 | 21.7 | 0.01 | 0.03 | 3.33 |
| weighted-s | file | 10343 | 27.2 (26.4-29.9) | 52.6 (51.4-104.1) | 1.93 | 20 | 40 | 11.4 | 22.1 | 0.01 | 0.03 | 3.29 |
| weighted-c | count | 10323 | 26.9 (26.5-30.8) | 52.4 (50.8-54.1) | 1.95 | 20 | 40 | 12.1 | 22.1 | 0.01 | 0.03 | 3.36 |
| weighted-c | devnull | 10323 | 24.7 (24.5-25.0) | 48.4 (48.1-52.3) | 1.96 | 10 | 40 | 11.9 | 22.2 | 0.01 | 0.03 | 3.28 |
| weighted-c | file | 10323 | 26.2 (24.2-28.8) | 53.2 (48.8-53.6) | 2.03 | 10 | 40 | 11.9 | 22.2 | 0.01 | 0.03 | 3.25 |
