# Critic review (performance) — PR #45, issue #40

- PR: https://github.com/dimchansky/fpgrowth-go/pull/45 (branch `perf/40-fisherinfo`)
- Head reviewed: c56eca9; follow-up commits verified by the orchestrator up to 05aadbf
- Decision: ACCEPT WITH CONDITIONS (Low items only)
- Recorded by the orchestrator from the critic's final message.

## Resolution of findings

- P-1: done in 87219a2. The `ss > 800` pre-test is now at the call site, so ordinary tables skip the non-inlined call (not re-measured; the critic's estimate is 12 of about 32 instructions).
- P-2: the PR body now gives the critic's figures (+0.9 % wall, +0.5 % instructions at n=1000; +0.13 % instructions on the CLI row).
- D-1: the CHANGELOG is reworded in ed0ead7 (fires for n > 170; the 1e15 row is included).
- Q-1: `infoCutoff` is now defined once and shared with the tests (87219a2).

## Critic's report

[critic-perf] PR #45 (Fixes #40), head c56eca9 reviewed against main c66d052 and v0.1.0. Measured 06:37–06:50, inside the night window. Load averages ran 3–10, and other sessions were running ruleval.test and fpgrowth-main at the same time, so the conclusions rest on instructions retired and allocations. Wall time is only a secondary signal.

**Decision: ACCEPT WITH CONDITIONS.** No CRITICAL, HIGH or MEDIUM findings; the conditions are the LOW items below.

**Measurements**
- **BenchmarkEval, every measure** (10 interleaved rounds, 300 ms each, -cpu 1, main / PR / v0.1.0):
  - Every row is unchanged except FisherInfo at n=1000: 359 → 362 ns, +0.91 %, p=0.000.
  - Yates (+0.71 %), Certainty (−6.15 %) and Info/nearIndep (−0.65 %) also moved. The PR does not touch their code, so this is code-layout noise and sets the noise floor.
  - No row allocates. The new FisherInfo/1e16 row takes 135 ns with 0 allocations.
- **Instructions per evaluation** (fixed N under `/usr/bin/time -l`, 2 runs each, startup subtracted):

| Row | main | PR | Change |
| --- | --- | --- | --- |
| FisherInfo, n=1000 | 6511 | 6544 | +0.5 % |
| FisherProb, n=1000 (control) | — | — | identical to 4 digits |
| Independent table, n=1e4 | — | — | +0.57 % |
| S(supp) ≈ 805 at n=2000 (logarithm computed, shortcut not taken) | — | — | +1.05 %, +133 instr |
| Strong association, n=2000 | 12408 | 2422 | 807 → 137 ns |
| Scan n=1e13 | 2.9 µs | 136 ns | — |
| Scan n=1e16 | 240 µs, 4341 allocs | 142–148 ns, 0 allocs | — |
| Scan n=9e18 | 632 µs, 11,369 allocs | 146–149 ns, 0 allocs | — |

- **CLI, `-tr -s5 -c30 -em -d100` on 20/gen/d5000x60d40.tab** (2,056,591 rules, 2 runs each):
  - main 51.57e9 instructions vs PR 51.64e9 (+0.13 %). User time is the same (4.36 vs 4.33/4.38 s).
  - v0.1.0 takes 1095.6e9 instructions and 87 s, about 21 times main.
  - The output hash is identical for all three (2583b3c86571).
- **Tests and mutations:**
  - A small-n check I ran: 45,758 tables at n = 171..4000 where the shortcut fires all equal the region sum, bit for bit (0 mismatches).
  - Making the shortcut return 0.5 fails 6 tests, including the existing oracle tests at small n. An unsoundly low cutoff (ln(n+1)+600) fails 3 tests. So the shortcut is covered at ordinary totals too.

**Answers to your questions**
- **Is the +3.3 % at n=1000 real?** No. The real cost is +0.5 % instructions and +0.9 % wall time, about 32 instructions per evaluation.
- **Where those instructions go:** the cheap `ss > 800` test is already first, so the logarithm is skipped on ordinary tables. The cost is two calls that are not inlined: `infoUnderflows` (inline cost 84 against a budget of 80) and the new `infoSum` split.
- **Cheaper ordering:** moving `ss > 800 &&` to the call site saved 12 of the 32 instructions (instr-variant.txt). That is optional; the overhead is justified.
- **Is there a shape where the check costs more than it saves?** Only the band 800 < S(supp) ≤ cutoff (about 845–1062). There the logarithm costs about +1 %, and only for strongly associated tables. No shape costs noticeably more than it saves.

**Findings**

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| P-1 | LOW | open (optional) | The ordinary path pays about 32 instructions (+0.5 %) for two calls that are not inlined. Putting `ss > 800 &&` at the call site removes 12 of them. | instr.txt, instr-variant.txt, `-gcflags=-m=2` |
| P-2 | LOW | confirmed | The "+3.3 %, +0.11 % instructions" claim is inaccurate. The +0.11 % was diluted by the /150 row (the upstream path for n ≤ 170, untouched) and the /1e4 row. The independent figures are +0.9 % wall and +0.5 % instructions. Please correct the PR body. | benchstat-main-pr.txt, instr.txt |
| D-1 | LOW | open | The CHANGELOG under-states and slightly mis-states the effect. The shortcut fires from n > 170: at n=2000 a strong rule goes 807 → 137 ns, and at n=1e13 2.9 µs → 0.14 µs. The "31 µs to 0.61 ms" range leaves out the 1e15 f=0.24 row, which was 6.55 µs with 0 allocations. | ZZCritic40 strong-n2000 and scan1e13 rows; baseline/scan-main.txt |
| Q-1 | LOW | open | The cutoff formula is duplicated in `infoCutoff` in the test, which can drift from `infoUnderflows`. Suggest one helper or constant used by both. | fisherlarge_test.go:567 |
| Q-2 | info | ok | The code follows the FisherProb shortcut pattern from #16, with explicit `float64()` rounding against FMA. The tests are bounded by counts (`AllocsPerRun(10)`, 40 or 15 oracle cases), not wall time. The GoDoc and the `docs/differences.md` contract are accurate, including the 1.88·10^16 proof range. `TestEvalDoesNotAllocate` was not run separately; the benchmarks show 0 allocations on every row. | diff review, mutation runs |

**Cleanup is not finished, because the disk filled up.** At 06:50 the disk hit ENOSPC, apparently from another session; my measurements only hashed their output. After that no shell command could start, including `rm`. Left behind:
- About 45 MB of my binaries in `~/.cache/fpgrowth-cycle/40/critic-perf/bin/`: main.test, pr.test, v010.test, main-zz.test, pr-zz.test, prv.test and fpg-{main,pr,v010}. Delete them once there is space.
- The `wt-main` worktree under `~/.cache/fpgrowth-cycle/34/review-perf` is checked out at c66d052. Restore it with `git -C ~/.cache/fpgrowth-cycle/34/review-perf/wt-main checkout --detach 8edb1d3`.
- The repo clone is at c56eca9 and clean. All temporary test files are removed. No processes of mine are running; the 43 s calibration run was killed.
- The last ACTIVITY.md entry (CLI, 06:50) is missing its "Done" line; please add it.

Raw data is in `~/.cache/fpgrowth-cycle/40/critic-perf/`: benchstat-eval.txt, benchstat-main-pr.txt, be-*.txt, bench-load.txt, instr.txt, instr-variant.txt, cli.txt, bench.sh, instr.sh, cli.sh and zz_critic40_test.go. I wrote no separate report file and did not push.
