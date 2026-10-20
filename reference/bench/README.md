# Benchmark datasets and the C comparison

Development-only tooling (nested `reference` module) for comparing the Go
port with Christian Borgelt's C program on identical input. The Go
benchmark suite itself lives in the root module (`benchmarks_test.go`,
`internal/*/*_bench_test.go`, `cmd/fpgrowth/cli_bench_test.go`); results
and methodology are in [`docs/benchmarks.md`](../../docs/benchmarks.md).

| Path | Purpose |
| --- | --- |
| `gen/` | Deterministic dataset generator (`go run ./bench/gen`) |
| `gen/core.go` | Dataset definitions (`benchSpecs`) and generator; copied verbatim into the root's `benchgen_test.go` |
| `compare/` | Runner that measures both programs (`go run ./bench/compare`, driven by `compare.sh`) |
| `configs.txt` | The compared configurations: id, input, fpgrowth options |
| `compare.sh` | Builds both programs, generates the data, runs the comparison |

## Datasets

```bash
cd reference
go run ./bench/gen -list                         # characteristics and SHA-256 prefixes
go run ./bench/gen -dataset dense -o dense.tab   # one dataset, upstream table format
go run ./bench/gen -dataset dense -csv -o d.csv  # the same as CSV
go run ./bench/gen -all -dir /tmp/fpgbench       # every dataset as <name>.tab
```

Every dataset is defined by a `benchSpec` (seed, number of transactions,
item universe, transaction length range, Zipf skew, planted patterns,
weights). Randomness comes from a seeded PCG source with this package's own
bounded-integer and shuffle derivations and integer-only popularity
weights, so the bytes are identical on every platform and Go version.

The `dNxKdP` datasets (`d2000x40d60`, `d1000x30d80`, `d5000x60d40`) are
the independent dense items of the size-limited closed/maximal
investigation ([M5](../../docs/m5-investigation.md) §7): N transactions
over K items, each item present with probability P % independently
(`benchSpec.Density`). They are generated exactly like `denseTxs` of
`internal/fpg/extcheck_bench_test.go` (PCG seeded `(Seed, 1)`, one
`math/rand/v2` `Float64` per item and transaction; both are exact IEEE
operations), with seeds 1, 2 and 3; the first two are also the data of
the in-process `BenchmarkCloMaxLimited`. The files name item `j` `j`,
`denseTxs` names it `i<j>`.

`d20000x200d30` (seed 5) is the dense maximal workload of the independent
audit (finding M5A-1, issue #19): 20,000 transactions over 200 items at
30 %, where every 3-set is frequent at `-s2` and no 4-set is, so
`-tm -s2 -n3` reports all 1,313,400 3-sets. The audit generated its
file with Python `random.Random(5)` and did not commit it; this spec has
the same shape and reproduces the audit's set counts and timings in
magnitude, not its bytes. Its `m5-d20000-*` rows in `configs.txt` are
slow (a `-tm -n3` run takes about 100 s in Go, 40 s in C).

`t100000x204c4` (seed 6, `benchSpec.Common`) is two-tier independent
data: items 0–3 at 20 %, items 4–203 at 10 %. With `-tm -s-180 -n3`
every pair of rare items is a parent whose only maximal children are
the common items. Its `m5-tier-*` rows guard against sibling rows
(issue #19) that are built for a few children and cannot pay off.

`s50000x414` (seed 7, `benchSpec.Slices`) puts 4 mutually exclusive
items first (80 % of the transactions hold one), then 10 items at 65 %,
then 400 at 15 %. With `-tm -s-1350 -n3` a rare item's slice children
are maximal and checked. Its other children have frequent supersets
and are never checked. The `m5-split-*` rows guard the later-sibling
bound of the same rule.

```bash
cd reference
go run ./bench/gen -dataset d2000x40d60 -o d2000x40d60.tab
go run ./bench/gen -dataset d1000x30d80 -o d1000x30d80.tab
go run ./bench/gen -dataset d5000x60d40 -o d5000x60d40.tab
go run ./bench/gen -dataset d20000x200d30 -o d20000x200d30.tab
bench/compare.sh -m devnull '^(m5-d[125]000-|m5-tier-|m5-split-|large-[cm]-n)'   # the fast M5 configurations
bench/compare.sh -n 5 -m devnull '^m5-d20000-'         # the M5A-1 workload (slow)
```
`go test ./bench/...` checks that

- the generator code between the `BEGIN`/`END BENCHGEN CORE` markers is
  identical in `gen/core.go` and the root's `benchgen_test.go`
  (`TestCoreInSync`; the nested module cannot be imported by the root
  module, so the code is copied), and
- the SHA-256 of every dataset is unchanged (`TestDatasetsPinned`).

Datasets are generated on demand into a temporary directory and never
committed (the largest is 8.5 MB).

## Comparison with the C program

```bash
cd reference
bench/compare.sh                       # all configurations, 1 warmup + 5 runs, three output modes
bench/compare.sh -n 10 'dense-'        # only configurations whose id matches, 10 runs
bench/compare.sh -m count -o /tmp/cmp  # only the count-only mode, results in /tmp/cmp
GO_FPGROWTH=/path/to/fpgrowth bench/compare.sh   # a prebuilt Go binary
```

- **C:** `./build.sh native` (upstream makefile flags
  `-DNDEBUG -O3 -funroll-loops`, nothing added; not the
  `-ffp-contract=off` oracle build).
- **Go:** `go build ./cmd/fpgrowth` with default flags (no PGO, no
  `-gcflags`), unless `GO_FPGROWTH` names a binary.
- **Output modes** (`-m`): `count` runs without an output file (both
  programs then only count the patterns, which is the closest the C
  program gets to an algorithm-only measurement); `devnull` formats every
  pattern and writes it to `/dev/null`; `file` writes a file, whose line
  count must equal the reported pattern count.
- **Measurement:** every run is wrapped in `/usr/bin/time -l` (macOS) or
  `/usr/bin/time -v` (Linux) for user and system CPU time and the maximum
  resident set size (on macOS also the peak memory footprint and the
  instructions retired). The wall time is measured by the runner with the
  monotonic clock, because `/usr/bin/time` prints it with 10 ms
  resolution. After the warmup runs, C and Go runs are interleaved
  (C, Go, C, Go, ...) so that drifting machine load affects both alike.
  The "mine" columns are the programs' own phase timings from their
  logs (the "finding ..."/"writing ..." lines, 10 ms resolution).
- **Correctness:** the pattern count printed by each program (`[N
  set(s)]`, `[N rule(s)]`) must be equal across programs, runs and modes,
  and equal to the output file's line count in `file` mode; otherwise the
  row is marked `MISMATCH` and the script exits non-zero. A configuration
  id marked with a trailing `*` in `configs.txt` (size-limited
  closed/maximal sets) may report fewer sets in Go than in C (deviation
  M5: C reports non-closed/non-maximal sets there): its counts must be
  equal across the runs and modes of each program and Go's must not
  exceed C's; the table then shows "C / Go".
- **Output:** `<outdir>/summary.md` (environment, dataset table and a
  Markdown table with medians and min-max spreads) and `<outdir>/raw.tsv`
  (every measured run).

### Limitations

- The C program has no library API, so there is no in-process
  algorithm-only C measurement. The `count` mode still includes process
  start, reading and recoding the input and building the transaction
  bag; the programs' own "mine" phase timings isolate the mining (with
  10 ms resolution). The Go side's algorithm-only numbers come from the
  in-process benchmarks (`BenchmarkSuiteConsume/*/internal-nosink`).
- Wall-clock comparisons of runs shorter than about 50 ms are dominated by
  process start and reading; prefer the CPU time and instruction counts
  there.
- The runner compares counts, not contents; byte equality of the outputs
  is the job of the differential tests (`reference/difftest`).
