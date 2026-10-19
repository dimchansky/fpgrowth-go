# PR #28 review: performance and architecture

- **Issue:** #16 (ALG-1, numerical instability of the statistical measures at large weights)
- **PR:** https://github.com/dimchansky/fpgrowth-go/pull/28, branch `fix/16-numerical-stability`
- **Head SHA reviewed:** `803626e`
- **Base:** `origin/main` `0fd4a0a`; also tag `v0.1.0` (`e99628c`)
- **Reviewer specialization:** Go performance engineering (runtime, memory, benchmarking method), code architecture and maintainability. Numerical correctness is covered by a separate critic. I did not re-derive any error bound.
- **Machine:** Apple M1 Max (10 cores), darwin/arm64, go1.27.2. The machine had other load during the runs: `uptime` load averages 5.1 to 9.8 (recorded before and after each run below). All comparisons interleave base, head and v0.1.0 run by run, so this load affects every side the same way.

## Scope and evidence examined

- Issue #16 body and its acceptance criteria. The performance criterion reads: "Benchmarks of the evaluation paths show no significant slowdown (benchstat, count ≥ 6, same machine), or any slowdown is quantified and accepted by the maintainer."
- `git diff origin/main...803626e`: `internal/ruleval/{exact.go, fisherlarge.go, measures.go, fisher.go, ruleval.go}`, `internal/istree/report.go`, the tests and the docs (`measure.go` GoDoc, README, CHANGELOG cost statements).
- Inlining and escape analysis (`go build -gcflags=-m=2`) on base and head, and `go tool objdump` of `reChi2` on both.
- CI workflow (`.github/workflows/ci.yml`): test matrix including arm64, `-race`, coverage, and the fuzz job (`FUZZTIME: 20s` per target).

## Commands run (results summarized)

All scratch work is in `scratchpad/review/` (`pr28p` = head, `pr28base` = origin/main, `pr28v010` = v0.1.0, `pr28exp` = head plus experiments, `pr28bench/` = binaries and raw results).

1. **Eval microbenchmarks.** I added the same file `zz_review_eval_bench_test.go` (BenchmarkEvalR, the PR's case table plus `Chi2/2^30`) to all three trees and built test binaries. Command: `-test.bench BenchmarkEvalR -test.benchtime 300ms -test.cpu 1`, 12 interleaved rounds (base → head → v0.1.0), then `benchstat`. Load 6.85 → 5.98.
2. **Rule mining in the library.** `BenchmarkRulesR` (dense, s=10 %, c=80 %, n=4, one sub-benchmark per measure), 10 interleaved rounds, base vs head, `-benchtime 8x`. Load 6.58 → 7.69. FisherProb was measured separately (see 4), because on base one iteration takes 20 s.
3. **CLI.** `reference/bench/gen` generated `dense.tab` and `sparse.tab`. I built `cmd/fpgrowth` from each tree and timed 11 configurations with a Python driver (`pr28bench/clibench.py`): `dense-r-lift` and `sparse-r-lift` from `configs.txt`, the same dense options with `-en -ep -ey -et -ei -eg -ez`, and sparse with `-ep -eg`. 10 interleaved rounds over all three builds. Load 9.77 → 7.60. A separate run timed `-i` (10 rounds).
4. **Fisher in the CLI.** `fpgrowth -tr -s10 -c80 -n4 -ef -d1 dense.tab`, 3 runs each with `/usr/bin/time -l`. A second run with `-d100 -v' %e'` compared the outputs line by line.
5. **Gate experiment.** In `pr28exp`, `reChi2` branches first on `(supp|base)>>26` and `reYates` gates on `(supp|base)>>31`. Valid because `0 < body, head < base` holds after `marginsDegenerate`. 12 interleaved rounds, base vs head vs experiment.
6. **Fisher cost and cancellation** (a test file in `pr28exp`): abort latency at n = 10^17, full evaluation time for n = 10^12 … 10^17, and `cmpWeight` cost at near-tie distances 16, 256 and 1024 at n = 10^18.
7. **Tests:** `go test -count=1 ./...` and `go test -race -count=1 ./...` on base and head; `go test -v ./internal/ruleval` for per-test times; `go test -fuzz '^FuzzMeasuresMatchReference$' -fuzztime 20s`.
8. `gofmt -l`, `go vet ./...` and `staticcheck ./...` on head: all clean.

## Results

### 1. Per-call Eval cost (ns/op, n = 12, base → head)

| Benchmark | base | head | Δ | v0.1.0 → head |
| --- | --- | --- | --- | --- |
| Lift | 3.58 | 3.59 | ~ | ~ |
| Chi2 | 3.80 | 4.03 | **+5.9 %** (+0.23 ns, p=0.000) | +4.8 % |
| Chi2PValue | 23.3 | 23.3 | ~ | ~ |
| Yates | 4.08 | 4.34 | **+6.3 %** (+0.26 ns, p=0.000) | +6.0 % |
| YatesPValue | 23.9 | 24.7 | +3.4 % (p=0.000) | +2.9 % |
| Info | 34.6 | 35.3 | **+2.0 %** (p=0.011) | ~ |
| InfoPValue | 110 | 97 | **−12.3 %** | −12.9 % |
| Certainty | 3.59 | 3.66 | +2.0 % | +1.7 % |
| Info/nearIndep (n=10^8) | 35.0 | 36.4 | +4.1 % | +3.0 % |
| InfoPValue/nearIndep | 78.4 | 79.0 | +0.8 % | ~ |
| Chi2/2^30 (128-bit path) | 3.81 | 4.97 | +30.5 % (+1.16 ns) | +29.7 % |
| Chi2PValue/2^42 | 4.91 | 6.61 | +34.6 % | +33.7 % |
| Info/2^42 | 35.3 | 7.0 | −80 % | −80 % |
| FisherProb (n=1000) | 2460 | 510 | −79 % | −80 % |
| FisherProb/150 (upstream path) | 1190 | 1180 | ~ | ~ |
| FisherChi2 / FisherInfo / FisherSupport (n=1000) | 1310 / 3980 / 1300 | 200 / 380 / 200 | −85 % / −91 % / −85 % | same |
| FisherProb/1e6 | 4.20 ms | ~1 µs | −99.97 % | same |

- Allocations: 0 B/op and 0 allocs/op for every case on all three trees. `TestEvalDoesNotAllocate` passes and covers the walk (n=20000), the 128-bit paths and the certainty exact form. `-gcflags=-m` reports nothing escaping or moved to the heap in `fisherlarge.go`.
- **Orchestrator gate:** Info +2.0 % and InfoPValue −12.3 % are both within 5 % of origin/main. Info/nearIndep (+4.1 %) is also within 5 %. **The gate passes.**
- These numbers reproduce the implementer's: Chi2 +5.8 %, Yates +6.3 %, Info +2.8 %, InfoPValue −12 %, FisherProb n=1000 4.8×.

### 2. Is the Chi2/Yates regression real, and can it be removed cheaply?

It is real. `objdump` of `reChi2` shows the common path gained exactly five instructions: three `ORR`, an `ASR $26` and a `CBZ`. The upstream expression is still computed first, and its FP latency covers the integer gate. That is about 0.7 cycles, which matches the +0.23 ns.

The experiment (branch first on a narrower gate):

| | base | head | experiment |
| --- | --- | --- | --- |
| Chi2 | 3.83 | 4.05 | 4.17 (**worse** than head) |
| Yates | 4.11 | 4.39 | 4.28 (−0.11 ns vs head, still +4 % vs base) |
| Chi2/2^30 | 3.83 | 5.00 | 4.60 |

- The implementer's layout for Chi2 (compute upstream's expression unconditionally, then override) is already the fastest I found.
- The narrower Yates gate saves 0.11 ns, which is not worth changing code that has been proven bit-identical.
- Inlining does not matter at mining level. The miner calls measures through a function value (`istree.Tree.fn`, `m.Func()`), so `reChi2`, `reYates` and `reCert` losing inlinability (cost 61/76/50 → 221/296/230) has no effect there. Only `reChi2PVal` used the inlined `reChi2`, and the PR compensates with a manual copy of the common path.
- **No cheap change removes the 0.2 ns.** It is the minimum cost of any range gate.

### 3. Visible at mining level? No.

Library, `BenchmarkRulesR` dense, base vs head (n = 9–10):

| Measure | lift | chi2 | chi2p | yates | yatesp | info | infop | cert |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| base → head | 223→203 ~ | 202→200 ~ | 205→205 ~ | 202→204 +1.2 % (p=0.028) | 204→205 ~ | 205→206 ~ | 211→207 ~ | 205→205 ~ |

The yates +1.2 % is at the noise floor. One round overlapped a `go test` I ran at the same time, and the CLI run below shows no such effect. 0.26 ns × 59 078 reported rules is about 15 µs per 200 ms run (< 0.01 %).

CLI, ms, n = 8–10, base → head (v0.1.0 → head in parentheses). Every row is "~" (p ≥ 0.08):

```
dense-r-lift  260→259   dense-r-chi2  262→259   dense-r-chi2p 260→262   dense-r-yates 259→262
dense-r-yatesp 262→261  dense-r-info  262→260   dense-r-infop 269→262   dense-r-cert  257→258
sparse-r-lift 160→160   sparse-r-chi2p 185→180  sparse-r-infop 195→194
dense-r-lift -i: 243→244 (~, p=0.63)  — AboveExpected (not inlinable, cost 99) is invisible
```

Fisher in the CLI (`dense.tab -tr -s10 -c80 -n4 -ef`): **base 20.10 / 20.29 / 20.02 s → head 0.08 s** in the writing phase, wall 20.4 s → 0.32 s, max RSS ~38 MB on both. The `-v' %e'` output is identical line by line (59 079 lines, 0 differ at the printed precision). This is far larger than the CHANGELOG's claim of "20 s → 0.3 s", and consistent with it.

### 4. Fisher at large n: worst case and cancellation

Full evaluation, balanced margins at independence (s = n/4):

| n | FisherProb | FisherSupport |
| --- | --- | --- |
| 10^12 | 36.5 ms | 18.3 ms |
| 10^14 | 0.40 s | 0.18 s |
| 10^16 | 3.52 s | 1.82 s |
| 10^17 | 11.15 s | 5.61 s |

- This matches the documented "11 s at 10^17" (README, `Measure` GoDoc) and the √n scaling. Extrapolating √n to n ≈ 2^63 gives about 100 s per evaluation; I did not measure this.
- **Abort latency at n = 10^17**, with the abort fired 200 ms in: FisherProb 1.6 µs, FisherChi2 30 µs, FisherInfo 44 µs, FisherSupport 18 µs. The step cost is about 8 ns, so `fisherAbortEvery = 4096` means about 33 µs between checks. Cancellation latency is fine.
- **Bisection near-ties.** `cmpWeight` at n = 10^18 takes 0.10 ms (distance 16), 0.19 ms (256) and 1.07 ms (1024 = `fisherTieExact`). At most 64 bisection steps gives about 70 ms without an abort check, because the stepper calls abort only every 4096 steps. That is acceptable. `TestFisherLargeNearTie` covers the far-distance shortcut.

### 5. Tests, CI and fuzz

| Package | `go test` base | `go test` head | `-race` base | `-race` head |
| --- | --- | --- | --- | --- |
| internal/ruleval | 5.4 s | **17.1 s** | 21.9 s | 23.8 s |
| internal/oracle | 1.1 s | 3.9 s | 6.9 s | 5.8 s |
| root | 12.7 s | 10.5 s | 59.6 s | 58.5 s |
| whole `./...` wall | ≈13 s (fpg) | ≈17 s (ruleval) | 62.8 s | 61.5 s |

- Under the default run, `TestFisherLargeMatchesReference` takes **12.25 s**, then `TestEvalMatchesUpstream` 4.2 s. The new test makes ruleval the slowest package in the default run (previously `internal/fpg`), and the coverage job will slow it further.
- `-race` is unaffected because the test reduces its counts under race. `-short` brings ruleval down to 4.3 s.
- The fuzz job gains one target, `FuzzMeasuresMatchReference`, which adds 20 s to the job. It ran 12–13 k execs/s for 20 s with no failures and no slow inputs; the Fisher cases beyond the oracle's table limit are skipped before evaluation.

### 6. Architecture and maintainability

- **File layout is good.** `exact.go` (93 lines) holds the 128-bit primitives. `fisherlarge.go` (424 lines) is a self-contained alternative algorithm behind one `base > maxFact` switch in each `fet*` function, plus a `validTable` guard. `infoNats`, `infoCell` and `gSeries` live in `measures.go` next to their single user. Each new file has a top-level comment that explains the method and the error budget.
- **Duplication** (minor, deliberate):
  - The `base > maxFact` / `validTable` / `fet*Large` prologue is repeated four times in `fisher.go` (6 lines each).
  - `reChi2PVal` repeats `reChi2`'s common path by hand. The motivation is real: without it, `reChi2PVal` would pay a non-inlined call of about 1–2 ns.
  - `infoNats` repeats `crossDiffFloat` "by hand".
  - All three copies are covered by `TestUpstreamFormulaIdentity` (bit-identity with upstream's formula for products ≤ 2^53, over 120 k tables) and by the reference comparisons.
- **The bit-identity rule** ("every product feeding an add/sub is wrapped in `float64(...)`") is a convention that depends on the reviewer. Its protection is `TestPinnedBits` (pinned bit patterns of the FMA-free paths) together with the arm64 CI legs (`ubuntu-24.04-arm`, `macos-latest`), where Go does contract FMAs. That detects a missed wrap only on the pinned inputs and on tables in the upstream identity test. Adequate for now; a vet-style check is not needed at this size.

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| P-1 | LOW | confirmed, acceptable | Chi2 is +5.9 % (+0.23 ns) and Yates +6.3 % (+0.26 ns) per call; YatesPValue +3.4 %, Certainty +2 %, Info/nearIndep +4.1 %. The 128-bit range (args ≥ 2^26) of Chi2 is +30 % (+1.2 ns). No effect at mining level: library and CLI differences are all within noise. No cheap fix exists: the five-instruction gate is the floor, and branch-first is slower for Chi2. | §1, §2, §3; `pr28bench/eval-*.txt`, `exp-*.txt`, `rules-*.txt`, `cli-*.txt` |
| P-2 | LOW | confirmed | `crossDiffFloat` is used only by `exact_test.go`, so it is dead in production. The comment on `crossDiffWide` says it is "kept separate so that crossDiffFloat can be inlined", but `-gcflags=-m=2` reports `cannot inline crossDiffFloat: cost 90 exceeds budget 80`. `infoNats` duplicates it by hand for exactly that reason. | `internal/ruleval/exact.go:57-72`, `measures.go:399-401` |
| P-3 | LOW | confirmed | The default `go test ./internal/ruleval` went from 5.4 s to 17.1 s, 12.25 s of it in `TestFisherLargeMatchesReference` (300 tables, up to `oracle.FisherMaxTables`). ruleval becomes the slowest package and lengthens the default and coverage CI jobs by a few seconds. Slower CI runners will scale this up (not measured). | §5 |
| P-4 | LOW | measured / extrapolated | The worst-case Fisher cost grows like √n: 11.15 s per FisherProb evaluation at 10^17 (measured), roughly 100 s near 2^63 (extrapolated, not measured). The docs stop at 10^17. Cancellation stays prompt (≤ 44 µs measured). | §4 |
| P-5 | INFO | confirmed (positive) | Large speedups. Fisher (n > 170): 4.8–10× per call at n=1000, 4000× at 10^6. Dense rule mining with `-ef`: 20.1 s → 0.08 s, printed output identical. Info at 2^42: −80 %. InfoPValue: −12 %. No new allocations; memory unchanged (RSS ~38 MB). | §1, §3 |

## Fixes requested

- **P-2 (should).** Either delete `crossDiffFloat` (test the int64 and 128-bit split through `crossDiffWide` and `infoNats`), or correct the comment to say why it exists and that it is not inlinable. Today the comment states something the compiler contradicts.
- **P-3 (optional).** Lower the default count of `TestFisherLargeMatchesReference` (for example 100 tables), or run the full 300 only without `-short` in a dedicated job. Either keeps ruleval near its previous runtime.
- **P-4 (optional).** Add one sentence to the `Measure` GoDoc and README on the worst case at the top of the range (about √(2^63)/4 ≈ 7.6·10^8 tables per side × ~8 ns), so users with weights near `MaxInt64` know a single evaluation can take minutes and should be bounded by a context deadline.
- **P-1: no change requested.** The regression is ≤ 0.26 ns per call, invisible in every mining and CLI benchmark (p ≥ 0.08 everywhere except a single p=0.028, +1.2 % yates sample at the noise floor), and no cheap change removes it. Per issue #16's criterion it is quantified here; it still needs the maintainer's explicit acceptance, which I recommend.

## Performance impact (summary)

- **Per call:** Chi2 +0.23 ns, Yates +0.26 ns, YatesPValue +0.8 ns, Certainty +0.07 ns, Info +0.7 ns. InfoPValue −13 ns. Chi2 for large arguments +1.2 ns. Fisher for n > 170 is 5–4000× faster.
- **Mining (library and CLI, 11 configurations plus `-i`):** no significant change for the closed-form measures. Fisher is about 250× faster on dense data.
- **Memory and allocations:** unchanged (0 allocs per Eval).
- **Tests:** +12 s for ruleval in the default run; `-race` unchanged; the fuzz job gains one 20 s target.

## Decision

**ACCEPT WITH CONDITIONS.** All findings are Low. The orchestrator's gate is met: Info is +2.0 % and InfoPValue −12.3 % vs origin/main, both within 5 %. The Chi2/Yates per-call regression is real but sub-nanosecond, shows up nowhere at mining or CLI level, and cannot be removed by a cheap change. I recommend the maintainer accept it. Condition: fix or remove the misleading `crossDiffFloat` inlining comment (P-2). P-3 and P-4 are optional polish.

---

# Re-review of 418a499

- **Head SHA reviewed:** `418a4998ee5b31cbeb980a3df7907976da1433a3`. Compared with origin/main `0fd4a0a` and with the previous head `803626e`.
- **Changes since 803626e:**
  - `9a49793`: certainty negative-D/zero-D exact branches inside the 2^10 gate, and the new `biglog.go`, which decides FisherProb near-ties beyond 1024 tables at 320 bits.
  - `5fe8f7b`: tests.
  - `418a499`: docs and difftest slack. `crossDiffFloat` is removed (P-2), the default of `TestFisherLargeMatchesReference` is reduced (P-3), and the worst-case docs are extended (P-4).
- **Load:** `uptime` during the runs ranged from 5.1 to 9.4, but was 42.7 at the start of the session. No timing below was taken during that spike; the benchmarks start at 18:41 with load 5.1. All comparisons are interleaved run by run.
- **Worktrees:**
  - `pr28p` and `pr28exp` = 418a499; `pr28exp` also carries the review test files.
  - `pr28old` = 803626e.
  - `pr28base` = main.

## Commands and results

**1. Eval microbenchmarks** (`BenchmarkEvalR2`: the earlier table plus `Certainty/gatePos` (2, 2, 2^40-2, 2^40) and `Certainty/gateNeg` (1, 1000, 2^40-100, 2^40)). 12 interleaved rounds of main → 803626e → 418a499, `-benchtime 200ms -test.cpu 1`. Load 5.12 → 9.42.

| ns/op | main | 803626e | 418a499 | 803626e → 418a499 |
| --- | --- | --- | --- | --- |
| Certainty | 3.56 | 3.61 | 3.62 | ~ (p=0.61) |
| Certainty/gatePos | 3.56 | 4.31 | 4.35 | +0.8 % (p=0.049) |
| Certainty/gateNeg | 3.55 | 4.20 | 4.19 | ~ |
| FisherProb (n=1000) | 2450 | 501 | 502 | ~ |
| FisherProb/150 | 1170 | 1180 | 1160 | −1.0 % |
| FisherChi2 / FisherSupport | 1290 / 1290 | 198 / 196 | 197 / 196 | ~ |
| FisherInfo | 3970 | 374 | 370 | −1.1 % |
| FisherProb/1e6 | 4.18 ms | 1.06 µs | 1.06 µs | ~ |
| Chi2 / Yates / Info / InfoPValue | 3.77 / 4.02 / 34.2 / 110 | 4.00 / 4.31 / 35.2 / 95.7 | 3.98 / 4.33 / 34.9 / 95.5 | all ~ |

- The new certainty branches add nothing on the ordinary path and only +0.04 ns inside the gate.
- The gated path costs about +0.7 ns compared with main's float formula, which was wrong there. That cost was already present at 803626e.
- 0 allocs/op for every case. `TestEvalDoesNotAllocate` passes; its tables never reach `cmpWeightBig`.

**2. Cost and allocations of `cmpWeightBig`** (a review test calling it directly, warmed up):

| Table | Time per call | Allocations per call |
| --- | --- | --- |
| n=10^18 | 152 µs | 2461 |
| n=20000 | 145 µs | 2371 |
| small cells, n=2^62 (factorial path) | 104 µs | 2529 |

Each `bigLnFact` does a 320-bit atanh series in which every operation allocates a fresh `big.Float` (`newBig()` per operand, and `big.NewFloat` per call in `bigLog` and in `cmpWeightBig`).

**3. Scan of FisherProb and FisherInfo per evaluation** (b = n/2, h = n/2+1, s = 0.05n / 0.2n / 0.24n, i.e. strong negative association; 803626e vs 418a499):

| n | FisherProb 803626e | FisherProb 418a499 | FisherInfo (both) |
| --- | --- | --- | --- |
| ≤ 10^14 | 2–8 µs, 0 allocs | 2–8 µs, 0 allocs | 2–7 µs, 0 allocs |
| 10^15 | 4.4 µs, 0 allocs | **345 µs, 5 576 allocs** (s=0.05n) | 34–114 µs, 542–1067 allocs |
| 10^16 | 4.3–8.4 µs, 0 allocs | **121–638 µs, 2–11 k allocs** | 67–268 µs, 1–4 k allocs |
| 10^18 | 4.7–9.2 µs, 0 allocs | **1.1–2.2 ms, 19–36 k allocs** | 0.3–0.5 ms, 6–9 k allocs |
| 9·10^18 | 5–9 µs, 0 allocs | **1.2–2.4 ms, 21–40 k allocs** | 0.5–0.67 ms, 9–11 k allocs |

- Every one of these p-values is **0** (underflow) on both heads.
- The FisherInfo cost (`cmpXLogX`) is unchanged since 803626e. It is not new.
- A second scan ran at n=10^15, h ∈ {0.5, 0.1, 0.01}·n, s = mode ± k·σ for k = 10…40 (p from about 1e-22 down to underflow). It found **no** allocating (big-log) evaluation with p > 0. Worst time was 431 ms, which is walk cost and not big-log.
- **Reading:** `lnPErr` grows with S, so the big-log path is reached essentially only for tables whose p-value underflows to 0. There the near-tie decision cannot change the result.

**4. Mining at large weights** (`dense.tab` with every transaction weighted 5·10^10, so n = 10^15; `fpgrowth -tr -w -s10 -c80 -n4 -ef -d1`):

| | Writing phase | Wall | Rules | Values |
| --- | --- | --- | --- | --- |
| 803626e | 0.23 s | 0.46 s | 59 078 | all 0 |
| 418a499 | **10.18 s** | **10.43 s** | 59 078 | all 0 |

- That is **44× slower** for an identical result: `-d100 -v' %20e'` outputs differ on 0 of 59 078 lines.
- Max RSS is 60 MB (38 MB on unweighted data), from GC churn of about 40 k allocations per evaluation.
- On main and v0.1.0 this run is infeasible (O(min(b,h)) ≈ 10^14 tables per rule), so this is **not** a regression vs main. It is a regression vs 803626e on a supported input class (large weights, strongly associated rules).

**5. Mining at ordinary weights** (CLI dense, `-tr -s10 -c80 -n4 -e{f,h,z} -d1`, 10 interleaved rounds, 803626e vs 418a499; load 8.37 → 7.42):

| | 803626e | 418a499 | Result |
| --- | --- | --- | --- |
| f | 317 ms | 322 ms | ~ (p=0.24) |
| h | 268 ms | 270 ms | ~ |
| z | 249 ms | 246 ms | ~ |

**6. Worst case and cancellation**

- **Big-log path:** at most 64 bisection steps × ~150 µs. The largest single evaluation measured was 2.4 ms. No adversarial table I tried exceeded 50 ms (strong association at n = 10^12…9·10^18, s/mode 0.3…0.499, b = n/2, h = n/2+1).
- **Abort checks:** the bisection calls abort only every 4096 steps, so it never checks. The uncancellable window per evaluation is therefore a few ms, which is acceptable.
- **Walk cancellation:** unchanged since 803626e (≤ 44 µs measured).

**7. Tests**

| Run | ruleval | Notes |
| --- | --- | --- |
| `go test ./...` | 8.6 s | in the parallel run; was 17.1 s at 803626e, 5.4 s on main. `internal/fpg` (11.4 s) is again the slowest package. Top ruleval tests: `TestEvalMatchesUpstream` 4.3 s, `TestFisherLongWalk` 2.5 s, `TestMeasuresMatchReference` 1.8 s, `TestCmpWeightBig` 0.84 s, `TestFisherLargeMatchesReference` 0.78 s. **P-3 resolved.** |
| `go test -race ./...` | 24.8 s | 61.2 s wall in total, unchanged from main (62.8 s). |

`gofmt -l`, `go vet` and `staticcheck` are clean.

**8. Architecture of `biglog.go`** (149 lines)

- It is self-contained: ln via 2·atanh range reduction, ln 2 via atanh(1/3), π via Machin, and Stirling with 15 Bernoulli terms. The shared constants are built once with `sync.OnceValue` and are only read afterwards, so concurrent use is safe.
- It hooks in through one line in `probAtMost`. The comments state the precision argument.
- It is clear and well isolated. Hand-rolling the logarithm is justified because `math/big` has no Log.
- Allocation discipline is poor: every operation calls `newBig()`, and there are about 2.4 k allocations per comparison. That matters only because of finding P-6 below.
- **P-2 resolved:** `crossDiffFloat` is gone; `infoNats` now uses `crossDiffWide` with a neutral comment.
- **P-4 resolved:** the README and `Measure` GoDoc now state the worst-case cost.

## Findings (re-review)

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| P-1 | LOW | accepted by the orchestrator | Chi2/Yates +0.23/+0.26 ns, unchanged. | §1 |
| P-2 | — | resolved | `crossDiffFloat` removed. | diff |
| P-3 | — | resolved | ruleval default 17.1 s → 8.6 s in the parallel run (4.7 s standalone per the implementer). | §7 |
| P-4 | — | resolved | Worst-case cost documented. | diff |
| P-6 | **MEDIUM** | confirmed | The new 320-bit near-tie path (`cmpWeightBig`) is reached for strongly associated tables at n ≥ ~10^15, whose FisherProb p-value underflows to 0. Each such evaluation goes from ~5 µs / 0 allocs (803626e) to 0.35–2.4 ms / up to 40 k allocs. Mining 59 078 rules at n = 10^15 goes from 0.23 s to 10.2 s (44×) with byte-identical output. The `Eval` GoDoc ("does not allocate, except in the rare tie resolution") is inaccurate for this input class. Not a regression vs main (infeasible there). | §3, §4 |
| P-7 | LOW | confirmed | `biglog.go` allocates a fresh `big.Float` for every intermediate (~2.4 k allocs, ~150 µs per comparison). Reusing scratch values would cut this several-fold, but it only matters if P-6 is not fixed by skipping the path. | §2 |
| P-8 | INFO | confirmed | The certainty branches cost nothing on the ordinary path (Certainty 3.61 → 3.62 ns, ~). Fisher ordinary paths are unchanged. TestEvalDoesNotAllocate holds. | §1 |

## Fix requested

**P-6 (should, cheap).** In `fetProbLarge`, return 0 before the bisection when the result must underflow. The p-value is Σ P(x) over tables with P(x) ≤ P(supp), so it is at most (min(b,h)+1)·P(supp). If `ls + lnPErr(ss) + log(min(b,h)+1)` is below about −745.2 (ln of half the smallest subnormal), no decision can change the float64 result. This would restore 803626e's ~5 µs per evaluation for exactly the class that now reaches `cmpWeightBig`. Scan 3 found no big-log hit with p > 0, but that is a hypothesis for n = 10^18, where I scanned only strong associations; it should be re-checked after the change with an allocation count over a table scan.

**Optional:**
- Correct the `Eval` GoDoc allocation note.
- Apply the same underflow short-circuit to FisherInfo, if the numerics critic agrees it is sound for the information ordering. It is not bounded by P(supp) directly, so this is not obvious.

## Decision (re-review)

**ACCEPT WITH CONDITIONS.** The only open finding above Low is P-6 (MEDIUM). It is a 44× mining slowdown vs the previous head on very large weights, with an identical result, and it is not a regression vs main. Ordinary-weight evaluation and mining are unchanged vs 803626e, and P-2, P-3 and P-4 are resolved. Condition: add the underflow short-circuit for FisherProb (or justify the cost), and correct the `Eval` allocation note.

---

# Re-review of 59bcede

- **Head SHA reviewed:** `59bcede9db03f6cad3393fb4689c1ff65b791dca`. Compared with 418a499 (`pr28exp`) and 803626e (`pr28old`).
- **Changes since 418a499:**
  - the `probUnderflows` shortcut in `fetProbLarge` (P-6);
  - the corrected `Eval` GoDoc allocation note;
  - buffer reuse in `biglog.go` (P-7);
  - one test.
- **Load:** `uptime` ranged from 12.07 down to 8.05 during the runs. All comparisons are interleaved run by run.

## Results

**1. The n = 10^15 mining reproducer** (dense data, each transaction weighted 5·10^10, `-tr -w -s10 -c80 -n4 -ef -d100 -v' %20e'`):

| Commit | Writing phase | Max RSS |
| --- | --- | --- |
| 803626e | 0.24 s | 38 MB |
| 418a499 | 10.26 s | 61 MB |
| 59bcede | **0.02 s** | 38 MB |

`cmp` shows the 59bcede output is byte-identical to 803626e's (59 078 rules). **P-6 resolved.**

**2. Eval benchmarks, 418a499 → 59bcede** (12 interleaved rounds, `-benchtime 200ms -cpu 1`):

| Benchmark | Change |
| --- | --- |
| Info, InfoPValue, Info/2^42, Certainty (all cases), FisherProb/150, FisherChi2, FisherInfo, FisherSupport | no significant change (p ≥ 0.08) |
| FisherProb (n=1000) | 512 → 517 ns, +0.9 % (p=0.005): one extra `math.Log` per evaluation |
| InfoPValue/nearIndep | +0.45 % (p=0.028), at the noise floor |
| FisherProb/1e6 | 1.08 → 0.18 µs, −84 %: that table's p-value underflows, so the shortcut applies |

- Every case is 0 B/op and 0 allocs/op.
- `go test ./internal/ruleval` passes (5.0 s); gofmt, vet and staticcheck are clean.

**3. Remaining allocating classes** (the same strong-association scan as before, n = 10^6 … 9·10^18):

- **FisherProb:** no longer allocates anywhere in the scan. Strong association at n = 10^18+1 takes 708 ns with 0 allocations (418a499: 2 ms, 35 k allocations).
- **`cmpWeightBig` itself (P-7):** 97–121 µs and 1.0–1.7 k allocations per call, down from 104–152 µs and 2.4–2.5 k. **P-7 improved.**
- **FisherInfo:** still allocates at n ≥ 10^15 for strong associations: 37 µs–0.77 ms and 542–11 369 allocations per evaluation. The numbers are identical to 803626e and 418a499, so this is not new in this PR's review rounds.
  - At mining level (same reproducer with `-em`): 3.37 s at 803626e vs 3.32 s now. RSS 60 → 54 MB.
  - All 59 078 values are 0, so an analogous underflow bound would make this fast too. That would need a bound on P(x) over the tables with I(x) ≥ I(supp) (lnP = −S + O(ln n)), which is a question for the numerics critic.
  - The cost per evaluation is bounded (≤ 0.8 ms measured).
- **FisherChi2 and FisherSupport** (`-eh`, `-es` on the reproducer): 0.03 s, unchanged.

**4. GoDoc.** The new `Eval` note matches what I measured:
- allocation only on exact near-tie resolution in FisherProb and FisherInfo;
- at most a few ms per evaluation (FisherProb ≤ 2.4 ms before the shortcut, FisherInfo ≤ 0.8 ms);
- the step is skipped for FisherProb when its p-value underflows.

The note does not say that for FisherInfo this happens for every strongly associated table at n ≳ 10^15, but "agree within their rounding error bound" is technically accurate, because that error bound grows with S. **Accurate.**

## Findings (re-review of 59bcede)

| ID | Severity | Status | Summary |
| --- | --- | --- | --- |
| P-6 | — | resolved | Reproducer 10.26 s → 0.02 s, output identical; FisherProb no longer allocates in the scan. |
| P-7 | — | improved | `cmpWeightBig` about 1.1 k allocations and 100–120 µs per call. |
| P-9 | LOW | confirmed, not new (present since 803626e) | FisherInfo at n ≳ 10^15 with strong association: ≤ 0.8 ms and ≤ 11 k allocations per evaluation; 3.3 s to mine 59 k rules whose values are all 0. Optional: an underflow shortcut like FisherProb's, if a sound bound is agreed. |
| P-1 | LOW | accepted earlier | Chi2/Yates +0.23/+0.26 ns. |

## Decision (re-review of 59bcede)

**ACCEPT.** No Medium or higher findings remain. P-9 is an optional Low follow-up.
