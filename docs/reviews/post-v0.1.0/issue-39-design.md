# Design — issue #39 (fpgpsp: per-surrogate rebuild)

- Written by the implementer on 2026-10-27 (night window), on Apple M1
  Max (arm64), Go 1.27.3. Raw data, scripts and the activity log:
  `~/.cache/fpgrowth-cycle/39/` (`baseline/`, `bin/`, `bench/`,
  `ACTIVITY.md`). Shapes: the data of the #23 performance review
  (`~/.cache/fpgrowth-cycle/23/review-perf/data`: A 4,000 x 40 dense,
  B 20,000 sparse transactions over 1,000 items, C a 5,000 x 10 table).

## Where the time and memory went (measured on main, c66d052)

CPU profiles of `cmd/fpgpsp` runs and memory profiles of
`internal/psp` `BenchmarkRunChain_Swap`:

- **Allocation: 3.3 MB per surrogate** (33.5 MB per 10 surrogates of the
  benchmark data): half the node storage of the prefix-tree bag, which
  grew from one node for every surrogate; a quarter the complex
  variant's node arena; a fifth the 16-items machine (640 KB, allocated
  per mining run); plus the map-based `ItemBase[Item]`. The garbage
  collector's work showed in every profile.
- **`-gr` on sparse data: 66 % in `tbg_random`.** The port searched the
  item buffer linearly for every drawn item (1,000 items, 138,000 draws
  per surrogate) and searched linearly for a repair item.
- **`-gp`: 23 % sorting the transactions** before the next pair swaps
  (comparison sort of 20,000 slices), 13 % `iswap`, 16 % item base and
  bag, the rest the miner.
- **`-gs` and dense `-gp`: 74-78 % and 40 % the miner itself**
  (`projReord`, `csCondSupp`, `addReord`), which this issue does not
  change.

## Can the item base be shared across surrogates?

The issue proposes to reuse the item base and the recoding. Per method:

| Method | Item frequencies | Item codes of a fresh pipeline |
|---|---|---|
| `-gi` | the data's | the same (but `-gi` mines one data set) |
| `-gp` | kept (an exchange moves one item each way) | can change: the default item order `-q 2` sorts by the size-weighted frequency, and a swap between transactions of different sizes changes it |
| `-gs` | kept (each item stays in its column); size-weighted frequencies too (all rows have one size) | can change: ties are broken by the first-seen order of the surrogate |
| `-gr` | not always kept: the repair can fail (U12), so items can drift and the frequent item set can change | can change |
| `-ge` | no surrogates; prepared once | n/a |

Sharing one recoding would give other item codes than main for `-gp`,
`-gs` and `-gr`. The spectrum does not depend on the codes in theory
(D5), but a different code order builds other trees and reports in
another order, so byte-equality with main would rest on every miner
variant being order-independent. Counting and recoding a surrogate
costs about 3 % of its time once done without a map. **Decision: every
surrogate is still counted, recoded and mined on its own, with exactly
the item codes, bag and miner of the fresh pipeline; only the storage
is reused.** Byte-equality then holds by construction and is checked
against the old pipeline.

## Changes

1. **`tract.DenseBase`** — an item base for records of identifiers
   0..n-1 with weight 1. `Count` ranks identifiers in first-seen order
   and counts frequencies and size-weighted frequencies in arrays;
   `Recode` uses the same comparator and port of `ptr_qsort` as
   `ItemBase.Recode` (shared helper `sortItems`); `BuildBag` inserts
   into the same `bagBuilder`, whose new `reset` keeps the node storage,
   the child index map and the root arrays of the previous bag.
   `TestDenseBase_matchesItemBase`: 600 random databases through one
   reused `DenseBase` against a new `ItemBase[Item]` each (codes,
   identifiers, frequencies, every bag property, `All`, `Backward`,
   packing, every item order including the extreme `-q` values).
2. **`fpg.Workspace`** — the miner's scratch arrays, node arenas,
   per-depth header buffers and both 16-items machines (dir > 0 and
   dir <= 0), lent to one run (`Miner.SetWorkspace`) and taken back
   afterwards. A reused machine is reset to the state of a new one (its
   weights are cleared, about 1 MB for dir <= 0, cheaper than
   allocating it). `Miner.DataDense` is `Miner.Data` on a `DenseBase`
   (one implementation, `data`, behind an `itemSource` interface).
   `TestWorkspace_sameResults`: 6,600 runs (150 random datasets, every
   target, variant and option set) with one shared workspace give the
   report lines of a fresh miner in the same order;
   `TestWorkspace_afterError`: after a run stopped by an error.
3. **`psp.Workspace`** — a `DenseBase` and an `fpg.Workspace`;
   `Workspace.Mine` and `Workspace.RunChain`; the package functions
   `Mine` and `RunChain` use a new one. `cmd/fpgpsp` keeps one per
   worker goroutine (workers stay bounded by GOMAXPROCS).
   `TestWorkspace_matchesFreshPipeline`: 160 chains (453 surrogates) of
   every method, target, variant, size range and item order through one
   workspace against the old pipeline (`mineRef`, the code of main),
   cell by cell.
4. **`tbg_random`** — the draw searches a Fenwick tree of the prefix
   sums of the persistent item buffer (O(log n) instead of O(n); exact
   integer sums, so it finds the entry of the linear search; an index
   at or beyond `m` means `x >= z`, which the linear search maps to
   `m - 1` too). The repair searches a bit set of the under-represented
   items from the random start, skipping marked items (the first item
   of upstream's cyclic search; P4 examines all n items). The random
   numbers drawn are unchanged. `TestRandom_matchesLinear`: 1,382
   surrogates of 400 shapes (1-150 items, items that never occur,
   empty transactions) against the linear implementation (kept in the
   test), and the generator states after each chain; two mutations
   (no wrap-around of the repair search, an off-by-one in the draw)
   fail it. `TestChain_matchesC` (C harness dumps) still passes.
5. **Sorting the surrogates** — transactions are sorted by a 64-bit key
   of their first two items (`(t[0]+1)<<32 | (t[1]+1)`, 0 for a missing
   item), comparing the remaining items only for equal keys; items
   within a transaction by insertion (they are nearly sorted after a
   pair swap). `TestSortTracts_matchesRef` against the plain sort,
   including codes near 2^31 (it caught an int32 overflow of the first
   key version).

Tried and dropped here (no change in instructions on `-ge` with the
RNG state kept in memory): an inlinable `RNG.Uint32`, and an insertion
sort of the probabilities in `samplelp`. The performance review showed
that keeping the RNG state in locals inside `dblSelect`, together with
an insertion sort and hoisted logarithms, does save 19-39 % on `-ge`;
that work is #47.

Not done:

- Building the miner's FP-tree directly from the surrogate arrays,
  without the prefix-tree bag. The bag fixes the insertion order of the
  FP-tree (`Bag.Backward`) and with it the node lists and the order in
  which projections are built; another order would again rest
  byte-equality on order independence. The bag costs 12-16 % of a
  surrogate after this change.
- `-ge`: it generates no surrogates. With `-z1` (preparation only) Go
  needs 0.30 G instructions against C's 0.07 G, the reading and
  preparation shared with `fpgrowth`; the 1,000 samples per size add
  1.11 G against 0.72 G (`samplelp`: random selections, sorting,
  probabilities). Both are outside this issue.
- The miner itself (`-gs`, dense `-gp`): unchanged.

## Results

Instructions retired, mean of 2 runs (`/usr/bin/time -l`); C is the
`fpgpsp-fixed` oracle (-O3). Main and C were measured once at 06:01
(load 2.6-4.3), the PR at 06:31 (load 3.0); every run's instruction
count was within 2 % of its repeat. User time is secondary.

| Shape | C | main | PR | PR vs main | main/C | PR/C | user main | user PR |
|---|---|---|---|---|---|---|---|---|
| B `-gr -s-20 -c10 -S1 -Z1` | 5.148G | 9.955G | 3.160G | -68.3 % | 1.93 | 0.61 | 0.84 s | 0.36 s |
| B `-gp -s-20 -c20 -S1 -Z1` | 2.322G | 6.642G | 5.563G | -16.2 % | 2.86 | 2.40 | 0.69 s | 0.62 s |
| B `-ge -s-20 -S1` | 0.794G | 1.403G | 1.403G | -0.0 % | 1.77 | 1.77 | 0.12 s | 0.12 s |
| C `-gs -s-100 -c50 -S1 -Z1` | 1.511G | 4.244G | 3.394G | -20.0 % | 2.81 | 2.25 | 0.46 s | 0.40 s |
| C `-gp -s-100 -c50 -S1 -Z1` | 1.949G | 5.413G | 4.457G | -17.7 % | 2.78 | 2.29 | 0.61 s | 0.55 s |
| A `-gp -s-60 -c64 -S1 -Z1` | 1.076G | 3.502G | 2.617G | -25.3 % | 3.25 | 2.43 | 0.40 s | 0.34 s |
| A `-gp -s-60 -c64 -S1 -Z64` | 2.184G | 4.629G | 3.841G | -17.0 % | 2.12 | 1.76 | 0.70 s | 0.61 s |

Maximum resident memory: `-Z1` runs 12-28 MB (main 13-31 MB), `-Z64`
55-58 MB (main 58-69 MB).

`internal/psp` benchmarks, main against the PR, interleaved, count 6
(benchstat; load 9-14 from other sessions, so wall time is the weaker
signal):

| Benchmark (10 surrogates/op) | time | bytes/op | allocs/op |
|---|---|---|---|
| `RunChain_Swap` | 64.6 -> 56.8 ms (-12.2 %) | 33.5 -> 4.2 MB (-87 %) | 2,100 -> 810 |
| `RunChain_Random` | 61.2 -> 55.3 ms (-9.7 %) | 33.5 -> 5.0 MB (-85 %) | 2,100 -> 820 |
| `Chain_Swap` (generation only) | 20.1 -> 18.7 ms (-6.7 %) | 221 -> 352 kB | 4 -> 5 |

`RunChain_*` start a new workspace per operation, so their bytes
include warming it. The steady state (`Workspace_Swap`,
`Workspace_Random`, one workspace for all operations): 1.07 and 1.11 MB
per 10 surrogates, **about 0.09-0.11 MB per surrogate instead of
3.3 MB**, mostly the reporter and spectrum of each surrogate.
`TestWorkspace_allocations` bounds it at 400 KiB per surrogate
(measured 90,796 bytes); without the miner's workspace it measures
1.66 MB and fails. `Chain_Swap` allocates one 128 KB sort buffer more
per chain (the keys).

## Byte-equality

- `bin/md5.sh`: 209 cases (shapes A, B, C; methods `-gi -gr -gp -ge`,
  and `-gs` on C; targets s, c, m, g; `-Z1` and `-Z3`; variants
  `-As -Ad -At`, `-x`, `-i`, `-u`, `-l8`, `-m`/`-n` limits, `-D`,
  generators with `-m2`, including the invalid `-Aa`/`-Ar` exits):
  spectrum and standard output identical to main in every case
  (`md5-pr-final.txt` against `baseline/md5-main.txt`).
- `bin/procs.sh`: `-Z8` chains with GOMAXPROCS 1, 3 and 10, methods
  `-gr -gp -gi`, all targets, shapes A and B: identical to each other
  and to main.
- `reference` difftest (`go test ./...` after `build.sh`): pass;
  `TestFpgpsp` 3,000 cases (893 estimations) byte-equal.
  `TestEstimate_matchesC`, `TestGoldenEstimate`, `TestGolden`,
  `TestChain_matchesC`, `TestRun_workersSameResult` pass.

## Risks

- `fpg.Workspace` and `tract.DenseBase` are new state carried between
  runs. A buffer that a later run relies on being zero must be cleared:
  the scratch arrays are cleared on reuse (`zeroed`), arenas are
  truncated, the 16-items machines are reset; header buffers per depth
  were already reused within a run without clearing. The equivalence
  tests share one workspace across all configurations to catch a
  missed reset; two mutations of the reset fail them.
- The fpgrowth library and CLI paths use `Miner.Data` unchanged (no
  workspace); `ItemBase.BuildBag` shares `bagBuilder.reset` and
  `finish` (which now keeps its sort buffer and no longer drops the
  index map of a builder that is garbage afterwards).
