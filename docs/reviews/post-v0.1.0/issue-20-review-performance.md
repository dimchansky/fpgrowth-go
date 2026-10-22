# PR #35 performance review (issue #20)

- **Issue:** #20, "Reduce quadratic closed-itemset checking on mixed-length transaction covers"
- **PR:** https://github.com/dimchansky/fpgrowth-go/pull/35 (`perf/20-closed-check`)
- **Head SHA reviewed:** `bec16810b3120f7d25c37ab0d0941bf9b4c1249b` (base `origin/main` = `ba63d14`, baseline also `v0.1.0`)
- **Reviewer specialization:** Go performance engineering, benchmarking methodology, architecture. This is the performance critic. Correctness is reviewed separately.
- **Machine:** Apple M1 Max (10 cores), go1.27.1 darwin/arm64, C reference built with `reference/build.sh` (native)
- **Working directory (raw data, scripts):** `~/.cache/fpgrowth-cycle/20/review-performance/`
  (`res/*.tsv` raw runs, `res/*.md` summaries, `scripts/run.py` interleaved runner, `scripts/summ.py`, `scripts/gen.py` adversary generator, `scripts/spanfix.diff` prototype fix, `mb/internal/fpg/zz_gallop_bench_test.go` microbenchmark)

## Decision: REJECT (one High finding)

The headline claim holds. The lp/lpc shapes go from quadratic to linear, and every existing `-tc`, `-tm` and Go benchmark row is unchanged. One part of the PR is not justified, though: the new exponential-search path (`gallop` → `keepSeek`). Its decision rule compares `|t|` with `16·|cand|`. The merge it replaces stops at the last candidate, not at the end of `t`. So when the candidates sit near the start of a long transaction, `keepSeek` runs 1.6–2.1× slower than the merge, and runs on such data take about 10 % more instructions in total. Disabling the search alone brings the regression back to `main` exactly. A one-line O(1) change to the rule removes the regression and keeps every lp gain (measured below). The PR's own comment ("16 is where the exponential search and the merge cost the same") holds only for candidates spread across the whole transaction.

## Measurement methodology and load

- **CLI runs:** `scripts/run.py` runs each config with every binary in a rotating order, 5–7 times. It records `/usr/bin/time -l` wall, user CPU, max RSS and **instructions retired**, plus the md5 of the output file of each binary. Load is recorded at the start and end of each group (`# start/end load` lines in the TSVs). Later rounds also record `ps -Aceo pcpu,comm -r` (`res/*-load.txt`).
- **Load:** other sessions ran CPU-heavy work while I measured. The correctness critic's `bfbin` brute forces pushed the load to 150–175 during the lp group and early in the tc group, and `edcat` used about 400 % CPU later (load 9–18).
- **Which metric supports what:** wall times from the high-load window are inflated by up to 3×, so I do not use them for any comparison. **Every regression and improvement conclusion below rests on instruction counts** (load-independent; run-to-run noise ≈ ±1–3 % on sub-0.1 s runs, < 0.5 % on longer ones) and on user CPU. The lp speedups and the adversary regression also hold in wall time from low-load rounds (load ≈ 9–10: `res/fix.tsv`, `res/adv2.tsv`, `res/slow.tsv`).

## Commands run (results summarized)

| Command / step | Result |
| --- | --- |
| `go build` main / PR / v0.1.0 / `-tags fpgforce` / C `reference/build.sh` | ok |
| `run.py cfg/lp.txt 7 … main,pr,v010,c` (all 15 lp-*/adv5 rows of configs.txt) | `res/lp.md`; outputs identical to main and C for all rows |
| `run.py cfg/slow.txt 3 … main,pr` (lp-100000, lp-c20000 -q0), load ≈ 5–8 | `res/slow.md` |
| `run.py cfg/tc.txt 7 … main,pr,v010` (all 23 `-tc` rows of configs.txt outside lp/adv5, plus m5-split-c-n3) | `res/tc.md`; output identical |
| `run.py cfg/tm.txt 5 … main,pr` (6 `-tm` rows) | `res/tm.md`; output identical |
| `run.py cfg/extra.txt 7` (ordinary data with `-tc -n` that hits the new paths) | `res/extra.md` |
| `run.py cfg/adv.txt 7`, `cfg/adv2.txt 5` (ablations), `cfg/fix.txt 5` (prototype fix) | `res/adv.md`, `res/adv2.md`, `res/fix.md` |
| Instrumented PR copy (`bin/fpg-instr`, global path counters) | path usage per dataset (below) |
| `go test -bench BenchmarkReviewKeep` (keepCand vs keepSeek microbenchmark) | `res/mb.txt`, `res/mb-prefix.txt` |
| Go benchmarks: `BenchmarkCloMaxLimited`, `BenchmarkMine`, `BenchmarkSuite{MaxSize,Targets,Variants}`, `-benchmem`, 6 rounds alternating main/PR, `benchstat` | `res/gobench/bs-*.txt`: all time rows `~`, allocs/bytes identical |
| `go build -gcflags=-m` / `-m=2` on `internal/fpg` | keepCand inlined at all 3 call sites; see F4 |
| `go test ./...` (clean cache) main vs PR; `go test -race -count=1 ./internal/fpg` main vs PR | see F3 |
| `gofmt -l .`, `go vet ./...`, `go vet -tags fpgforce ./...`, `staticcheck ./...` | clean |

## Performance impact (instructions are the primary metric)

### Claimed improvement: reproduced

| row | main | PR | C | PR/main ins |
| --- | ---: | ---: | ---: | ---: |
| lp-12500 `-As -tc -s-2 -n1` | 0.43 s, 5.14 G | 0.01 s, 0.22 G | 0.22 s | −95.7 % |
| lp-25000 | 1.72 s, 20.2 G | 0.03 s, 0.46 G | 0.86 s | −97.7 % |
| lp-50000 | 6.93 s, 79.8 G | 0.05 s, 0.93 G | 3.61 s | −98.8 % |
| lp-100000 (load ≈ 5–8) | 27.35 s, 317 G | 0.10 s, 1.76 G | — | −99.4 % |
| lp-c20000 `-q0` (load ≈ 5–8) | 61.29 s, 987 G | 0.53 s, 7.09 G | (does not finish) | −99.3 % |
| lp-50000s2 / lp-50000w / -Ac / -Ad / -As-l0 | 79.8–79.9 G | 0.91–1.09 G | | −98.6…−98.9 % |
| lp-50000 `-At` / `-n3` / s2 `-n2` / lp-c20000 | | | | −81 / −59 / −74 / −71 % |
| lp-50000-m-As (`-tm`, control) | 126.00 G | 126.00 G | | ±0.00 % |
| adv5-20000 / adv5-40000 | 0.785 / 1.154 G | 0.788 / 1.137 G | | +0.4 / −1.4 % (noise) |

Scaling (instructions): 0.22 → 0.46 → 0.93 → 1.76 G for N = 12.5k → 100k, which is linear. Output md5 matched main (and C, where C finishes) on every row.

### Existing rows: unchanged

- **24 `-tc` rows** (`res/tc.md`): instruction geomean PR/main **+0.14 %**. The worst rows are sparse-c +1.9 % and d20000 -tc -n3 +0.60 %. I reran sparse-c 5× per binary at low load: main 549.7–553.0 M, PR 548.4–550.9 M, so its +1.9 % is GC/runtime noise (that row does not use `-n` and never calls the closed scan). d20000 -tc -n3 at +0.60 % (92.18 → 92.73 G) reproduces the implementer's figure and is real but negligible. m5-tier-c-n3 +0.41 %. v0.1.0 vs main: geomean −0.01 %.
- **6 `-tm` rows** (`res/tm.md`): geomean +0.03 %, worst +0.12 %. They are untouched, as expected.
- **Ordinary data with `-tc -n` that does hit the new paths** (`res/extra.md`; instrumented counts in parentheses): long `-n2` (31.8k scans, 30.4k "moved" starts, 0 stage b, 0 gallops) −0.37 %; sparse `-s-2 -n2` (81k scans, 2.6k stage b) +0.17 %; sparse-large `-s-2 -n1` −0.05 %. No transaction of the existing datasets has ≥ 128 items, so `keepSeek` never runs on them.
- **Go benchmarks** (`res/gobench`, 6 alternating rounds, load 8–17): every sec/op row of CloMaxLimited, Mine and the closed/MaxSize/Targets/Variants rows is `~` (p ≥ 0.06), except SuiteVariants closed/simple −9.7 % (p = 0.041, ±75 % spread, noise). B/op and allocs/op are identical (the ±0.00 % rows are significant only because the values are constant).
- **RSS:** unchanged on every row (±2 MB). The new code allocates nothing (`tp` is a stack array).

### Adversaries against the new stages

Generator: `scripts/gen.py out N U Lmin Lmax B seed [P]`. Each transaction holds B "block" items, each present with probability P, plus L random items from a universe of U. Run with `-tc -s-2 -n1`. I confirmed path usage with the instrumented binary.

| dataset | shape | instrumented (per 100k / 20k scans) | main ins | PR ins | PR/main |
| --- | --- | --- | ---: | ---: | ---: |
| g1 | N 22.5k, L = 200 fixed, U 100k | stage b 75 %, never shorter; 32.7k gallops | 12.27 G | 12.40–12.45 G | **+1.1…+1.5 %** |
| g2 | as g1, N 30k (stage b mostly off) | stage b 8 % | 15.58 G | 15.62 G | +0.2 % |
| g3 | L 150–250 (stage b finds shorter) | stage b 29 %, moved 94 % | 12.43 G | 12.30 G | −1.0 % |
| g6-60-0.9 | N 1k, L 1000, B 60, P 0.9 | stage b 95 %, 975k gallops | 4.36 G | 4.81 G | **+10.3 %** |
| g6-200-0.9 | B 200, P 0.9 | stage b 93 %, 876k gallops | 6.03 G | 6.63 G | **+10.1 %** |
| g6-200-0.95 | B 200, P 0.95 | 283k gallops | 4.90 G | 5.29 G | **+8.0 %** |
| g6-400-0.95 | B 400, P 0.95 | 212k gallops | 7.27 G | 7.57 G | **+4.2 %** |

In the low-load rounds (load ≈ 9–10) wall time matches: g6-60 0.56 → 0.59–0.60 s, g6-200-0.9 0.82–0.83 → 0.89 s. Wall time in the earlier round was +11–12 %.

Ablation (`res/adv2.md`, `res/fix.md`; same source with only `scanDefaults` changed):

| variant | g6-60-0.9 | g6-200-0.9 | g6-200-0.95 | g1 | lp-50000s2 |
| --- | ---: | ---: | ---: | ---: | ---: |
| PR | +10.2 % | +10.1 % | +7.9 % | +1.3 % | −98.7 % |
| no stage b (`guard` = ∞) | +11.1 % | +10.2 % | +9.5 % | +0.4 % | — |
| no gallop (`gallopMin` = ∞) | **+0.1 %** | **−0.0 %** | −2.5 % | +1.6 % | −87.7 % (s2 needs the search) |
| gallopRatio 32 | +9.3 % | +3.0 % | +0.5 % | +1.2 % | — |
| **span-aware rule (`scripts/spanfix.diff`)** | **+0.0 %** | **−1.2 %** | **−2.2 %** | +1.3 % | **−98.7 %** |

Stage b is cheap, as designed: on its worst case (g1: always triggered, never finds anything shorter, candidates die after one merge) it costs +0.4…+1.5 % in total instructions. The **exponential search** causes the g6 regression.

Microbenchmark (`BenchmarkReviewKeep`; one `keepCand` or `keepSeek` call; 6 counts; medians):

- Candidates spread across the whole transaction (`even`/`rand`): seek/merge = 0.65–0.90 at ratio 12–20 and 0.25–0.5 at ratio ≥ 32. The ratio-16 threshold is safe for this case; the break-even is nearer 10–12.
- Candidates in a prefix of `t` (`prefix`: the first `c` items; `prefixhalf`: every other one of the first `2c`), at |t| = 256, 1024 or 4096 and ratio 16, 20 or 32: **seek/merge = 1.60–2.11** at every size and ratio. `keepCand` stops after the last candidate, after about `c` or `2c` steps. `keepSeek` still pays a gallop and a bisection for each candidate.

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| P1 | **HIGH** | Confirmed | The `gallop` rule (`internal/fpg/extcheck.go`, `gallop`: `l >= gallopMin && l >= gallopRatio*len(c.cand)`) assumes the merge costs O(|t|). `keepCand` actually ends at the last candidate. When the candidates sit near the start of a long transaction (≥ 128 items; they are the items with the lowest codes), `keepSeek` is 1.6–2.1× slower per intersection than the merge it replaces. End to end that is **+4…+10.3 % instructions** (low-load wall +5…+9 %) on `-tc -n1` data with long transactions and a block of common items. The regression disappears exactly when the search is disabled. It is an unjustified regression from a sub-feature (the PR comment's cost argument covers only spread candidates). It is confined to transactions of ≥ 128 items. | `res/adv.md`, `res/adv2.md`, `res/fix.md`, `res/mb-prefix.txt` |
| P2 | LOW | Confirmed | Stage b's worst case (always triggered, finds nothing shorter, candidates die quickly) costs +0.4…+1.5 % in total instructions (g1). That is within the documented bound ("at most a quarter of copying t*") and acceptable, but undocumented: docs/benchmarks.md says only "unchanged within noise" for existing rows. | g1 rows |
| P3 | LOW | Confirmed | CI time: `TestExtCheckMixedCover` takes 6.1 s (17.0 s under `-race`) and runs serially. `internal/fpg` went from 17.0 → 21.1 s (default) and 50.7 → 69.4 s (`-race`, +37 %); the whole `go test ./...` went from 18.4 → 22.4 s real. The package already has a `raceEnabled` convention for slow tests (`race_test.go`/`norace_test.go`) that the new test does not use. | `res/ci-*.txt`, `-race -v` output |
| P4 | LOW | Confirmed | Readability and architecture of the knobs: `scanParams` lives in every `extChecker` and the hot loop reads it from memory, `scanDefaults` is a mutable package `var` that an `init` in the `fpgforce`-tagged file overwrites, and the production code increments test-only `scanStat` counters (including in `keepSeek`). There is no measurable cost (ordinary rows ±0.2 % instructions). It is test scaffolding in production paths, though, and making `seek` generic produced a dictionary-passing shape function. `seek` was not inlinable on `main` either (cost 88), so this changes nothing measurable (−tm/−tc vertical rows unchanged). The `fpgforce` tag is documented in `reference/bench/README.md` and the file comment. Acceptable. | `-gcflags=-m=2` output |
| P5 | Info | Confirmed | `keepCand` stays inlined at all three call sites (lines 629, 642, 655) and `gallop`/`txLen`/`initCand` are inlined. `keepSeek` is not inlined (cost 132), which is fine for a rare path. | `-gcflags=-m` |

## Fixes requested

1. **(P1, required)** Make the gallop decision depend on how far the merge actually has to walk, not on |t|. A prototype I measured, O(1) per transaction, with `keepCand` still inlined at all sites:
   ```go
   k := c.gallopRatio * len(c.cand)
   return k == 0 || c.txItems[c.txOff[t]+k-1] < c.cand[len(c.cand)-1]
   ```
   (added after the existing `l` tests). It gives g6 −2.2…+0.0 % vs main (from +8…+10 %) and keeps lp-50000s2 −98.7 %, lp-50000 −98.9 % and lp-c20000 −71.3 %; `TestExtCheckMixed*`, `TestExtCheckTrap*` and `TestSeek*` pass. Add a clustered-candidate case to the tests or benchmarks. The current tests do not cover this regime. Rerun the brute forces with the `fpgforce` thresholds after the change.
2. (P2) In docs/benchmarks.md or m5-investigation.md §7.5, record the measured cost of stage b's worst case (about +1 % in total instructions) and of the corrected search's worst case.
3. (P3) Scale `TestExtCheckMixedCover` down under `raceEnabled` (or `testing.Short`), or run its seeds in parallel subtests.
4. (P4, optional) Consider making the counters and thresholds test-only (for example constants plus a test hook), or document why they stay in production.

## Accepted claims (verified)

- lp 50k `-n1`: 6.93 → 0.05 s (C 3.61 s); lp-c20000 `-q0`: 61.3 → 0.53 s; linear scaling; output identical to main (and to C).
- All 24 `-tc` rows unchanged (instructions geomean +0.14 %, d20000 -tc -n3 +0.60 %); `-tm` rows unchanged; CloMaxLimited and the Suite closed/MaxSize rows `~` with identical allocations.

---

# Re-review of c70e83a

- **Head reviewed:** `c70e83aa15d67bc164eff3d40547a62b5f93efcf` (code change in `58774a2`; `c70e83a` changes docs only). CI 15/15 green, as the coordinator verified.
- **Scope:** P1–P4 fixes. Per the measurement budget, the established lp/lpc speedups and the slow baselines were not re-measured. The baselines are round-1 data plus `~/.cache/fpgrowth-cycle/20/baseline/`.
- **Raw data:** `review-performance/r2/` (`tc.tsv`/`tc.md`, `adv.tsv`/`adv.md`, `lp.tsv`, `mb.txt`, `night-load.txt`, `inline.txt`, `newtests*.txt`, `race-pkg.txt`, `pkg.txt`).

## Daytime checks (19:29–19:35, light)

- **Diff:** `gallop` now returns `k == 0 || txItems[txOff[t]+k-1] < cand[last]` (with `k = 16·|cand|`) after the `l >= 128`, `l >= k` tests. This is the O(1) span test, identical to the round-1 prototype. The thresholds are now build constants (`extcheck_thresh.go` `!fpgforce` / `extcheck_force.go` `fpgforce`) copied into `c.scanParams` by `scanDefaults()`. There is no package-level mutable var and no `init` any more. The hot path still reads the thresholds from the checker struct, which costs nothing measurable (see below).
- **Inlining** (`-gcflags=-m`, `r2/inline.txt`): `gallop` is inlinable (cost 66); `gallop`, `txLen` and `keepCand` are inlined at all three call sites (lines 626/629, 639/642, 652/655), and also with `-tags fpgforce` (3 `keepCand` sites).
- **Static analysis:** `gofmt -l`, `go vet ./...`, `go vet -tags fpgforce ./...` and `staticcheck ./...` are clean.
- **Test runtime (P3).** Load at the start was 7.4.
  - The new and changed tests together take 7.96 s by default and **about 6.0 s under `-race`**: MixedCover 2.42 s (was 17.0 s), MixedScaling 2.50 s, FirstOfWord 0.96 s, Clustered 0.07 s.
  - `go test -race ./internal/fpg` takes 45.4 s, against 69.4 s on PR round 1 and 50.7 s on main in round 1. By default it takes 18.4 s (main 17.0 s in round 1).
  - The claim (about 6.4 s under `-race`) is confirmed, and **P3 is resolved**.

## Night check (21:00–21:05): interleaved instructions, main vs PR, 2 runs each

Load was 6.1 at the start and about 107 from 21:03, when the correctness critic's `bfbin2` started. Every conclusion here rests on **instructions retired**, which do not depend on load. Wall time is not used. Output md5 matched main on every row.

| set | PR/main instructions | worst row |
| --- | --- | --- |
| 24 `-tc` rows of configs.txt (+ m5-split-c-n3) | geomean **−0.29 %** | large-c-n3 +0.83 %, dense-c-Ac-j +0.83 %, d20000 -tc -n3 +0.58 % (noise level; round 1 showed ±2–3 % run-to-run on short rows) |
| g1–g6 adversaries (`-tc -s-2 -n1`) | geomean **−0.88 %** | g1 +0.96 % (stage b worst case, as in round 1); g6-60-0.9 **+0.30 %** (was +10.3 %), g6-200-0.9 **−1.26 %** (was +10.1 %), g6-200-0.95 **−3.03 %** (was +8.0 %), g6-400-0.95 **−2.34 %** (was +4.2 %) |
| PR-only lp sanity (not a re-measurement of the speedup) | lp-12500 0.23 G, lp-25000 0.45 G (linear), lp-50000s2 1.08 G, lp-c20000 6.32 G | these match round 1 (0.22 / 0.46 / 1.04–1.09 / 6.31–6.34 G), so the search still runs where the transaction is long and the candidates are spread |

The microbenchmark (`r2/mb.txt`; `keepCand` / `keepSeek` / production rule) ran while the load was about 100. Its medians are too noisy to rank paths within 2× (the merge alone varied 62–186 ns on one case), so **I draw no conclusion from it**. The rule's correctness of choice is shown by the instruction counts above and by `TestExtCheckClusteredCandidates`, which asserts the merge for clustered candidates and the search for spread ones.

## Findings after round 2

| ID | Severity | Status | Summary |
| --- | --- | --- | --- |
| P1 | HIGH | **Resolved** | The span-aware rule removes the g6 regression: +4…+10 % became −3.0…+0.3 % in instructions. The lp gains are unchanged, the regression test was added, and `keepCand` is still inlined. |
| P2 | LOW | Resolved (docs) | The stage-b worst case (g1) measures +0.96 % now (+1.1…+1.5 % in round 1). The coordinator reports the docs updated; I did not review the doc wording in detail. |
| P3 | LOW | **Resolved** | New tests take about 6 s under `-race`; `internal/fpg` `-race` takes 45 s. |
| P4 | LOW | Resolved | The thresholds are build constants with no `init` mutation. The `scanStat` counters remain in production code and are harmless (Info). |
| P5 | Info | Confirmed | Inlining holds at all call sites. |

## Decision (round 2): ACCEPT

There are no Critical, High or Medium performance findings left. The speedup established in round 1 stands, existing `-tc` rows and the adversaries are within ±1 % instructions of main, and the outputs are identical.
