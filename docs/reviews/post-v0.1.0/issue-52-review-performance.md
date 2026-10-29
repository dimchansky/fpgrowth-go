# Critic review (performance and memory) — PR #55, issue #52

- PR: https://github.com/dimchansky/fpgrowth-go/pull/55 (branch `perf/52-byte-reread`)
- Head reviewed: f452168 (binaries built from 98e98dd, which changes only a test and the changelog)
- Decision: ACCEPT WITH CONDITIONS (one Medium, three Low)
- Recorded by the orchestrator from the critic's final message.

## Resolution of findings

- P1 (Medium): fixed in 249b627. The string `BuildBag` handles unknown
  identifiers under `AppearNone` in its own loop (calling the check
  function there when one is set) instead of through the non-inlined
  `pass2.unknown`; `BenchmarkBuildBagSelect` added. Focused check by the
  orchestrator, main (acd8d4a) vs the fix, 6 interleaved runs,
  GOMAXPROCS=2, under the watchdog: the critic's `select20` -2.5 %
  (p=0.008), then ~ in a second run; `select20-check` ~ (p=0.056);
  `BuildBagSelect` ~; `ItemBase_BuildBag` -1.7 % (p=0.009); allocations
  2 fewer per build. The design record's "no regression" sentence is
  corrected.
- P2 (Low): accepted. The shared per-record helpers are not inlined:
  500,000 one-item records +1.7 % (p=0.008) in the library's string
  `BuildBag`; on the CLI the over-budget one-item row is -11.9 %.
- P3 (Low): documented in the design record (long records over the
  budget: peak RSS 141 to 125 MB).
- P4 (Low): noted; the measured code is identical at both heads.

## Critic's report

Method: fresh clone; C (`reference/build.sh release fpgpsp-fixed`),
v0.1.0 (`fpgrowth` only), main acd8d4a and the PR; `/usr/bin/time -l`
(instructions retired, user time, peak RSS, output md5); GOMAXPROCS=2,
one process at a time under the watchdog; versions interleaved; each
big file generated, used and deleted before the next. Load 1.6-4.3;
80 GB disk free; the watchdog never fired. Microbenchmarks: benchstat,
6 interleaved runs, main vs PR, plus ad hoc `BenchmarkReview` shapes
(not committed).

### CLI, instructions retired (G), medians of 2 rounds

| Row | C | v0.1.0 | main | PR | PR / main | max RSS MB (C / main / PR) | md5 |
| --- | ---: | ---: | ---: | ---: | ---: | --- | --- |
| `fpgrowth -s50` B×360 (195 MB, over budget) | 16.96 | 75.25 | 53.41 | 44.44 | -16.8 % | 464 / 135 / 135 | equal |
| `fpgpsp -ge -z1 -s-20 -S1` B×360 | 19.93 | – | 61.87 | 52.84 | -14.6 % | 519 / 421 / 421 | equal |
| `fpgpsp -gr -s50 -c1 -S1 -Z1` B×360 | 20.10 | – | 60.49 | 51.37 | -15.1 % | 740 / 371 / 371 | equal |
| `fpgpsp -gp -s50 -c1 -S1 -Z1` B×360 | 20.72 | – | 61.21 | 51.91 | -15.2 % | 740 / 371 / 371 | equal |
| stdin `fpgrowth -s50 -` B×360 | 16.99 | 75.87 | 54.04 | 45.03 | -16.7 % | 464 / 135 / 135 | equal |
| stdin `fpgpsp -ge -z1 -` B×360 | 19.93 | – | 62.49 | 53.47 | -14.4 % | 519 / 421 / 421 | equal |
| `fpgrowth -s50 -R sel300` B×360 (over, ignored names) | 16.41 | – | 54.19 | 45.07 | -16.8 % | 436 / 137 / 137 | equal |
| `fpgpsp -ge -z1 -R sel300` B×360 | 18.70 | – | 62.40 | 53.28 | -14.6 % | 436 / 465 / 463 | equal |
| `fpgrowth -s50` 4000 × 10,010 items (271 MB, over) | 12.75 | 61.60 | 43.40 | 35.91 | -17.3 % | 195 / 141 / 125 | equal |
| `fpgrowth -s0.5` 30M one-item records (over) | 28.20 | – | 49.19 | 43.35 | -11.9 % | 1380 / 135 / 135 | equal |
| `fpgrowth -s50` 30M one-item records (no pass 2) | 19.16 | – | 22.15 | 22.13 | ~ | 1151 / 134 / 134 | no output |
| `fpgrowth -s50` 20M one-item records (in budget) | 12.75 | 18.67 | 15.36 | 15.42 | +0.4 % (noise) | 768 / 134 / 134 | no output |
| `fpgrowth -s50` B×200 (in budget) | 9.44 | 41.90 | 14.40 | 14.40 | 0.0 % | 259 / 127 / 127 | equal |
| `fpgpsp -ge -z1` B×200 | 11.08 | – | 18.75 | 18.75 | 0.0 % | 259 / 254 / 254 | equal |
| `-R sel10` / `-tr -R app10` / `fpgpsp -R sel10` B×360 (in budget) | | | | | ~ (6 rounds, spread up to 5 %) | | equal |
| small inputs (B, dense, sparse, d2000 `-tm`, `fpgpsp -ge`/`-gr` on B) | | | | | 0 to -0.6 % | | equal |

User time over the budget: `fpgrowth` 4.17 to 3.44 s, `fpgpsp -ge`
5.24 to 4.48 s (C 1.72 and 2.88 s). The implementer's numbers were
reproduced within 0.1 %.

### Library `BuildBag`, benchstat main vs PR (f452168), n=6

| Benchmark | Δ time | p | Δ allocs/op |
| --- | --- | --- | --- |
| `ItemBase_BuildBag` | -2.7 % | 0.008 | -2 |
| `BuildBag/items=100` | -1.3 % | 0.004 | -2 |
| `BuildBag/items=2000` | -1.9 % | 0.002 | -1 |
| `Review/plain` | -1.4 % | 0.004 | -2 |
| `Review/long` | ~ | 0.056 | -2 |
| `Review/select20` (AppearNone) | +4.9 % (repeat +4.3 %) | 0.002 | -2 |
| `Review/select20-check` | +3.8 % (repeat +3.4 %) | 0.041 | -2 |
| `Review/tiny-1item` | +1.4 % (repeat +1.5 %) | 0.004 | -2 |
| root `ItemChecks`, `SuiteSources/sparse*` | ~ | ≥ 0.08 | = or -2 |

`go build -gcflags=-m=2`: `pass2` `begin`, `end` and `unknown` are not
inlined into either closure. An experiment with an ignore fast path in
`BuildBag` recovered `select20` (-6.9 % vs the PR).

### Findings

| ID | Severity | Status | Summary |
| --- | --- | --- | --- |
| P1 | Medium | Confirmed | Library string `BuildBag` +3.4-4.9 % on AppearNone-heavy input; the fast path exists only in `BuildBagBytes`; contradicts "no regression" in the design record. |
| P2 | Low | Confirmed | `pass2` helpers not inlined; +1.5 % on 500k one-item records (library). |
| P3 | Low | Confirmed | Docs say peak memory unchanged; on long records it drops (141 to 125 MB). |
| P4 | Low | Note | Head f452168 is behind the tip 98e98dd (test and changelog only). |

### Decision

ACCEPT WITH CONDITIONS. Over the budget -14.4 to -17.3 % instructions on
every CLI path, -11.9 % on tiny records; within the budget and on small
inputs unchanged; output md5 equal everywhere; RSS equal or lower.
