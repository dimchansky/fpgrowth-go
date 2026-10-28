# Critic review (performance) — PR #49, issue #47

- PR: https://github.com/dimchansky/fpgrowth-go/pull/49 (branch `perf/47-ge-sampling`)
- Head reviewed: 8f441be
- Decision: ACCEPT WITH FINDINGS (one Medium, one Low, one Info)
- Recorded by the orchestrator from the critic's final message.

## Resolution of findings

- P49-1 (Medium): fixed in 08eacc6. `sortAsc` calls `slices.Sort` above
  256 elements (`sortAscMax`), as the critic proposed;
  `TestSortAsc_matchesSlicesSort` covers both branches (a mutant that
  breaks the long branch fails it). Focused check by the orchestrator
  on freshly generated sets of the critic's shapes, main fde9e41 vs
  08eacc6, md5-equal: XL (k <= 600) 6.78G -> 5.94G instructions
  (-12 %), XXL (k <= 1,500) 3.91G -> 3.12G (-20 %).
- P49-2 (Low): fixed in 08eacc6. The design record has a
  "Long transactions" section with the measurements and the cutoff.
- P49-3 (Info): the watchdog's 2 s poll only lengthens short runs; no
  change.

## Critic's report

**Verdict: ACCEPT WITH FINDINGS (1 Medium, 2 Low).** The claimed `-ge`
speed-up is real: the PR's own numbers reproduce to within 0.5 %. Every
output is md5-equal to main, and to C wherever main equals C. The other
methods do not regress. The one real problem is the new insertion sort
`sortAsc`: it is O(k²) and regresses `-ge` on data with long
transactions.

**Setup.** Head 8f441be vs main fde9e41 vs the C oracle
`39/baseline/fpgpsp-c`. v0.1.0 has no `cmd/fpgpsp` (`git ls-tree v0.1.0
cmd/` lists only `cmd/fpgrowth`), so it has no column. M1 Max,
GOMAXPROCS=2, one process at a time, every run under the RSS watchdog;
no watchdog kills. `uptime` load was 2.5–3.6 throughout. Conclusions
rest on instructions retired (`/usr/bin/time -l`), which repeat to
within 0.2 %; user time is the secondary signal.

### 1. `-ge`: C / main / PR (median of 3 interleaved runs, all md5-equal across C, main and PR)

| Row | C | main | PR | PR vs main | main/C | PR/C | user C/main/PR (s) |
|---|---|---|---|---|---|---|---|
| B -s-20 -S1 | 0.796G | 1.402G | 1.090G | -22.3 % | 1.76 | 1.37 | 0.10/0.12/0.09 |
| B -z1 | 0.071G | 0.296G | 0.296G | 0.0 % | 4.17 | 4.17 | 0.00/0.02/0.02 |
| B -z5000 | 3.665G | 5.820G | 4.252G | -26.9 % | 1.59 | 1.16 | 0.46/0.52/0.35 |
| B -S7 -e0.9 | 0.799G | 1.397G | 1.089G | -22.0 % | 1.75 | 1.36 | 0.10/0.12/0.09 |
| A -s-60 | 0.209G | 0.375G | 0.234G | -37.5 % | 1.80 | 1.12 | 0.02/0.03/0.02 |
| A -z5000 | 0.925G | 1.554G | 0.851G | -45.3 % | 1.68 | 0.92 | 0.08/0.13/0.07 |
| C -s-100 | 0.189G | 0.385G | 0.229G | -40.5 % | 2.03 | 1.21 | 0.01/0.03/0.02 |
| C -z5000 | 0.804G | 1.442G | 0.665G | -53.9 % | 1.79 | 0.83 | 0.05/0.11/0.05 |
| B -z20000 | 14.413G | 22.389G | 16.104G | -28.1 % | 1.55 | 1.12 | 1.81/2.00/1.30 |

- The PR's claims hold: it states -22.1 % / -37.5 % / -40.2 % and `-z1`
  unchanged; measured -22.3 % / -37.5 % / -40.5 % / 0.0 %.
- Sampling is now close to C: 0.8–1.16x C's instructions, and less user
  time than C at `-z20000`.
- The gap at the defaults is fixed cost: the `-z1` row (4.2x C) is
  reading and preparation, which this PR does not touch (#48).
- Memory is unchanged: max RSS 11.7 MB on B for main and PR.

### 2. Other methods: main vs PR (2 runs, `-Z1`, md5-equal)

| Row | main | PR | Δ |
|---|---|---|---|
| B `-gi -c10` | 0.527G | 0.528G | +0.2 % |
| B `-gr -c10` | 3.156G | 3.156G | 0.0 % |
| C `-gs -c50` | 3.393G | 3.393G | 0.0 % |
| A `-gp -c64` | 2.616G | 2.616G | 0.0 % |

No regression. The changed functions have no callers outside
`internal/psp/estim.go`.

### 3. Profile, B `-ge -z20000` at the PR head

A temporary test calling `run()` with `-cpuprofile` (removed
afterwards). `samplelp` is 92 % of total time. Inside it:
`dblSelectBack` 26 %, `dblSelect` 22 %, `permProbs` 19 %, `sortAsc` +
`less` 20 %, `math.log` 3 %, `exp` 2.5 % (shares of total). What remains
is the shuffle and the products, both inherent to the algorithm.

### 4. Long transactions

A/B/C have transactions of at most 18 / 55 / 10 items, and the sort's k
is bounded by that size. Synthetic sets with longer transactions (L:
500 transactions of 100–200 items; XL: 300 of 400–600; XXL: 100 of
1,200–1,500; deleted afterwards). Probe variants: "+sort" (`sortAsc`
replaced by `slices.Sort`) and "hybrid" (insertion sort up to 256
elements, `slices.Sort` above).

| Row | C | main | PR | +sort | hybrid | PR vs main | user C/main/PR/+sort/hybrid (s) |
|---|---|---|---|---|---|---|---|
| L, k≤200, `-z1000` | 9.72G | 15.40G | 14.59G | 11.72G | 14.60G | -5 % | 1.46/1.53/1.00/1.17/1.02 |
| XL, k≤600, `-z100` | 8.76G | 14.14G | 22.69G | 10.88G | 11.57G | **+60 %** | 1.40/1.51/1.28/1.09/1.07 |
| XXL, k≤1,500, `-z10` | 5.52G | 8.83G | 27.37G | 6.80G | 6.87G | **+210 %** | 0.85/0.88/1.36/0.65/0.64 |
| B `-z5000` | 3.69G | 5.85G | 4.28G | 4.40G | 4.28G | -27 % | 0.46/0.52/0.35/0.40/0.35 |
| A `-z5000` | 0.95G | 1.58G | 0.88G | 0.90G | 0.88G | -45 % | 0.08/0.13/0.07/0.08/0.07 |

All Go variants are md5-equal to main on every row. C matches on L, XL,
B and A; on XXL it differs from every Go build alike, which is the
documented U11 deviation (C's slot counts wrap, Go's saturate), not
this PR. The hybrid gets the better sort on every row.

### Findings

| ID | Severity | Summary |
|---|---|---|
| P49-1 | Medium | `sortAsc` makes the once-per-sample sort O(k²). On `-ge` with long transactions it regresses against main: +60 % instructions at k≤600, +210 % and user time 0.88 s → 1.36 s at k≤1,500. The design's "at most the largest transaction size, mostly few" holds only for short-transaction data. Fix: `if len(s) > 256 { slices.Sort(s); return }`, plus a test case longer than the threshold. |
| P49-2 | Low | The design record and the PR table do not say where the insertion sort stops paying off; every measured shape has transactions of at most 55 items. Fix: add a long-transaction row and a note on the cutoff. |
| P49-3 | Info | The watchdog's 2 s poll stretches short runs (the `-ge` table took 2:49 instead of ~1.5 min); time budgeting only. |
