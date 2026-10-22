# PR #36 performance review (issue #21, NEW-1)

- Issue: #21 Avoid unnecessary perfect-extension work under restrictive size limits
- PR: https://github.com/dimchansky/fpgrowth-go/pull/36, branch perf/21-perfect-extensions
- Head reviewed: b55ad8b (base origin/main e4cf7d0)
- Reviewer: [critic-performance] Go performance engineering, benchmarking methodology, architecture (correctness covered by another critic)
- Scope: hot-path cost of the new guards (PexUseful before the four chain walks; the guards in Report and expand), inlining, test runtime, architecture; independent night measurement main vs PR.
- Working dir: 21/review-performance (fpgrowth-go = PR head, main = worktree of e4cf7d0; own binaries bin/fpg-{main,pr}, test binaries bin/{report,fpg,root}-{main,pr}.test)

## Evidence and commands

| Command | Result |
| --- | --- |
| `git diff origin/main...HEAD` | 17 files; code: report.go (+pexMax, PexUseful, SetPexGuard, guards in Report and expand), complex.go/simple.go/single.go (one `if m.rep.PexUseful()` around each chain walk) |
| `go build -gcflags=-m ./internal/report ./internal/fpg`, diff main vs PR | only additions: PexUseful inlinable and inlined at all 4 call sites; SetPexGuard inlinable; updateXmax still inlined (5 sites). No inlining lost (AddPex, AddPexPacked unchanged) |
| `go test -count=1 ./...` (PR) | all pass; new tests: report pex tests 0.02 s, TestMine_T4NoPerfectExtensions 0.01 s, TestMine_PexGuardSame 0.16 s; total impact about 0.2 s. The scaling test counts collected extensions (deterministic, not time based) |
| Daytime sanity (one run): inh-2000x500, inh2-2000x500 | outputs identical; numbers below |
| Night: `night-critic.sh` (started 21:43 after night.done, load avg 3.7-4.3) | cli.tsv (26 rows x main/pr x 2 runs, interleaved), benchstat.txt (count 6, interleaved) |
| Implementer's night data reviewed | baseline/t4-night.tsv, ordinary.tsv, benchstat.txt, sweep.txt (2880 runs, 0 differences) |

## Instructions retired, min of 2 runs (critic, cli.tsv); md5 identical main vs PR on every row

| Row | main | PR | PR/main |
| --- | --- | --- | --- |
| t4-12500 -s-17 -n1 | 7935 M | 505 M | 0.064 |
| t4-25000 -s-17 -n1 | 30685 M | 995 M | 0.032 |
| t4-12500 -tg | 2147 M | 512 M | 0.238 |
| t4-12500 -As | 8400 M | 506 M | 0.060 |
| t4-12500 -Ad | 8394 M | 505 M | 0.060 |
| one-12500 -s-1 -n1 (one long transaction) | 2099 M | 156 M | 0.074 |
| t4b-6000 -s-1 -n1 | 2908 M | 164 M | 0.057 |
| inh1-2000x500 -s-1 -n1 (500 top-level pexs inherited by 2000 sets at cnt == zmax) | 1668 M | 1617 M | 0.970 |
| inh2-2000x500 -s-1 -n2 (level-1 pexs inherited at cnt == zmax = 2) | 9431 M | 2619 M | 0.278 |

PR scaling 12.5k -> 25k: 1.97x (linear); main 3.87x (quadratic). Matches the implementer's t4-night.tsv (PR 251/496/1039 M at 6.25k/12.5k/25k; C 1291/5005/19771 M; v0.1.0 = main) and the challenger's 7.94 G -> 0.50 G.

Ordinary rows (17 rows: dense, sparse, long, m5 d2000/d5000/d1000, tier; default target and -tg; -n2/-n3 and unlimited; -As/-Ad; one -tc -n3): geomean PR/main **+0.05 %**, range -0.38 % (dense -Ad -n3) to +0.67 % (m5-d1000 -n2, a 133 M row; the implementer's 20 rows give +0.02 % geomean, range -0.35 % .. +0.33 %). Noise level, no regression.

## Go benchmarks (benchstat, count 6, interleaved; critic run, implementer's run agrees)

- fpg BenchmarkMine (18 cases): sec/op geomean -0.07 % (all ~ except m/t +0.55 %, p=0.041); B/op +0.00 %; allocs equal.
- BenchmarkSuiteMaxSize (frequent/closed/maximal, n=3/5): sec/op geomean +0.39 %, all ~ (p >= 0.065); allocs equal.
- report BenchmarkReporter: frequent +3.78 % (p=0.002), pex-hypercube-8 +4.08 % (p=0.019), pex-hypercube-8-abort-hook +3.82 % (p=0.002), frequent-count-only ~, closed ~; closed B/op +0.85 % (7.359 -> 7.422 KiB: the Reporter struct grew by 16 bytes into the next size class). The implementer's run shows the same (+4.58 %, +3.98 %, +4.54 %). Reproduced twice, so real at micro level; about 12-80 ns per op.

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| P1 | LOW | confirmed | BenchmarkReporter micro-regression of about 4 % on frequent and pex-hypercube (two independent interleaved runs, p <= 0.02). Not visible end to end: CLI instruction geomean +0.05 % on 17 ordinary rows, BenchmarkMine -0.07 %, SuiteMaxSize +0.39 % n.s. Likely the extra load/compare of r.pexMax in Report and expand (or code layout). Optional: test the guard only when it can matter (e.g. `if n > 0 && r.cnt >= r.pexMax` in Report; in expand only when n > next) and recheck BenchmarkReporter. | review-performance/benchstat.txt, 21/benchstat.txt |
| P2 | LOW | confirmed | Reporter allocation +64 bytes per New (closed benchmark B/op +0.85 %), from the two new fields; negligible. Could pack noPexGuard into the existing mode/flags, not required. | benchstat B/op |
| P3 | LOW | note | inh1 shape (top-level pexs inherited by singletons at zmax = 1) gains only 3 %: input parsing dominates; the Report guard works (inh2: 3.6x fewer instructions). No action. | cli.tsv |

No CRITICAL/HIGH/MEDIUM findings. Architecture: the change is minimal and local (one inlined compare per chain head in the miners, one compare per Report and per expand call; AddPex stays an exact port, so the C-harness goldens stay valid); SetPexGuard gives a cheap differential switch. The documented numbers (CHANGELOG, differences.md M10, PROGRESS: 7.94 G -> 0.52 G, one-12500 2.11 G -> 0.16 G) agree with my measurements (0.505-0.518 G; 0.156 G).

## Fixes requested

None blocking. Optional: P1 (narrow the Report/expand guard and re-run BenchmarkReporter).

## Performance impact

Large, reproducible improvement on the targeted shapes (T4 12.5k 15.7x, 25k 30.9x fewer instructions, now linear; -tg 4.2x; one-N 13.5x; t4b 17.7x; inherited-at-zmax 3.6x), all with identical output. No end-to-end regression on ordinary rows (instructions, 2 runs each), Go benchmarks neutral apart from the 4 % reporter micro-benchmark.

## Decision

ACCEPT WITH CONDITIONS (LOW only: P1 optional follow-up; P2, P3 notes).

Processes: my night-critic.sh finished at 21:51 (critic.done); none of my processes remain (the running night-critic.sh at 21/review-correctness belongs to the correctness critic).

## Re-review of 5eba17d

Delta 841551f..5eba17d: the Report guard is narrowed to `n > 0 && cnt >= pexMax`, and expand's per-call guard is replaced by a loop break at `cnt >= zmax`, which takes the place of upstream's `cnt <= zmax` test. Run at 22:36, load 2.9-4.2, with no other heavy process (rr.load). Script: review-performance/rr.sh. I built my own binaries from 5eba17d (bin/fpg-pr2, bin/report-pr2.test).

- BenchmarkReporter main vs PR, benchstat count 6, interleaved (rr-benchstat.txt):
  - frequent +2.10 % (p=0.004); it was +3.8 % at b55ad8b.
  - pex-hypercube-8 -1.04 % (p=0.037); it was +4.1 %.
  - abort-hook not significant; it was +3.8 %.
  - count-only and closed not significant.
  - Time geomean +0.27 %; it was +2.4 %.
  - closed B/op +0.85 % is unchanged (P2).
- CLI instructions, 1 run each, md5 identical on every row (rr-cli.tsv):

| Row | main | PR | PR/main |
| --- | --- | --- | --- |
| t4-12500 `-s-17 -n1` | 7953 M | 519 M | 0.065 |
| one-12500 `-s-1 -n1` | 2105 M | 159 M | 0.076 |
| dense `-s5` | 5898 M | 5904 M | +0.11 % |
| dense `-s5 -n3` | 3415 M | 3417 M | +0.07 % |
| sparse `-tg -s0.1` | 534 M | 534 M | -0.04 % |
| m5 d2000 `-tc -s2 -n3` | 492 M | 492 M | +0.02 % |
| tier `-s-180 -n3` | 13019 M | 13017 M | -0.02 % |

- Result:
  - P1 is mostly resolved. A +2 % wall-time difference remains on the frequent micro-benchmark, about 7 ns per op. It does not show end to end, so it stays LOW and needs no further action.
  - The speedup on the targeted shapes is unchanged, and the ordinary rows are flat.
- Decision for 5eba17d: ACCEPT WITH CONDITIONS (LOW only).
- No process of mine is running.
