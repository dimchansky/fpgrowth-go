# Benchmarks (Stage 3 baseline)

**Status: baseline, before any optimization.** Every number below was
measured on commit `c16c2f5` (end of Stage 2) plus the new benchmark files
only; no production code was changed. The numbers describe one machine
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

Density is the average length divided by the number of items used.
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
- **Size-limited closed/maximal sets are the largest gap.** Outside the
  comparison script (3 runs each, `dense.tab -s2 -n3`, identical counts):
  frequent C 0.45 s / Go 0.67 s (1.5×), closed C 1.95 s / Go 4.54 s
  (2.3×), maximal C 1.10 s / Go 2.73 s (2.5×). In-process,
  `MaxSize/dense/s=2/closed/n=3` takes 4.39 s vs 0.60 s for frequent
  sets. This is the closed/maximal size-limit cost listed for Stage 3 in
  the README (deviation M5); it is being worked on separately and is not
  ranked below.

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
| dense closed `s=2 -n3` (M5) | `report.cmTree.add` 26.3 / 31.2; `csCondSupp` 16.7; `mine16` 11.1 / 62.6; `closedMaximal.update` cum 32.6 |

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
| — | Size-limited closed/maximal (M5) | 4.39 s vs 0.60 s frequent; Go/C 2.3–2.5× | handled separately (not ranked) | `SuiteMaxSize` |

Not candidates on this evidence: GC tuning (GC ≤ 1 % of CPU), allocation
counts on the result path (constant, 200–400 per run), the public API
layer (callback/iterator within noise of the internal pipeline), and
read syscalls (see the caveat above).

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

# Go vs C (about 6 minutes; needs a C compiler and GNU make)
(cd reference && bench/compare.sh -n 5 -w 1 -o /tmp/fpgcmp)   # /tmp/fpgcmp/summary.md, raw.tsv

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
input file size per op for file sources.

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
| MaxSize/dense/s=2/frequent/n=3 | 601ms ± 3% | 72.5MB ± 0% | 246 ± 0% | 54.8k ± 0% |  |
| MaxSize/dense/s=2/frequent/n=5 | 1.24s ± 1% | 72.6MB ± 0% | 288 ± 0% | 1.09M ± 0% |  |
| MaxSize/dense/s=2/closed/n=3 | 4.39s ± 1% | 157MB ± 0% | 420 ± 0% | 54.8k ± 0% |  |
| MaxSize/dense/s=2/closed/n=5 | 4.43s ± 1% | 157MB ± 0% | 421 ± 0% | 1.01M ± 0% |  |
| MaxSize/dense/s=2/maximal/n=3 | 2.65s ± 1% | 73.7MB ± 0% | 365 ± 0% | 11.2k ± 0% |  |
| MaxSize/dense/s=2/maximal/n=5 | 2.66s ± 3% | 73.7MB ± 0% | 366 ± 0% | 45.5k ± 0% |  |
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
