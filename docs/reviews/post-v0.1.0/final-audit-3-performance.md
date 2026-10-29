# Repeated final integration audit 3 — performance (main a5f548c vs 9f1aa62, v0.1.0 and C)

- **Decision: NO REGRESSIONS.**
  - All 52 CLI rows (46 `fpgrowth`, 6 `fpgpsp`) are within -0.64 % to
    +0.44 % of 9f1aa62 in instructions retired. The run-to-run spread is
    up to 1.6 %.
  - All 42 rows that can be compared with v0.1.0 are 9.3-48.5 % below
    it.
  - Every output has the same md5 on v0.1.0, 9f1aa62, a5f548c and C.
  - The B×360 rows that exceed the retention budget reproduce PR #55's
    numbers within 0.2 %.
- **Scope:** #53 (test only) and #52 (PR #55). `reference/` is
  byte-identical between 9f1aa62 and a5f548c.
- **Environment and procedure:**
  - Apple M1 Max, 32 GB, macOS 26.6.1, go1.27.3, in the night window
    (2026-10-29, 22:58-23:14).
  - One process at a time, GOMAXPROCS=2, RLIMIT_CPU 150 s, under the RSS
    watchdog. Nothing was killed and nothing timed out.
  - Load average 2.6-10.2 during the CLI round (a concurrent correctness
    audit was running) and 1.6-2.3 for the rest. Conclusions rest on
    instructions retired.
  - The first audit's `run.py` and row set, plus the A1 rows and the
    README row d20000 `-tm -s2 -n3`. Each value is the minimum of 3
    interleaved, rotated runs.
  - The data is byte-identical to `docs/benchmarks.md` (SHA-256
    prefixes).
  - C was not re-run: its sources, build scripts, data and toolchain are
    identical, so the C numbers of the previous audit are reused.
- Recorded by the orchestrator from the auditor's final message. Raw
  tables are in the auditor's work directory (`res/cli.md`, `psp.md`,
  `big.txt`, `*-benchstat.txt`).

## CLI rows (G instructions, minimum of 3)

| Rows | a5f548c vs 9f1aa62 | a5f548c vs v0.1.0 | md5 |
| --- | --- | --- | --- |
| sparse `-s0.1`, `-n3`, `-tc`, `-tm`, `-tg` | -0.21 to +0.44 % | -33.9 to -38.1 % | equal |
| sparse `-tr` (`-c60`, lift, `-ep`, `-ef`, `-ei`, `-ez`) | -0.11 to +0.16 % | -9.8 to -46.9 % | equal |
| thresholds (abs-r, frac-m, frac-s), weighted `-w` | -0.10 to +0.21 % | -15.2 to -43.0 % | equal |
| large `-s`, `-tc -n3`, `-tm -n3` (A1) | -0.16 to +0.11 % | -40.8 to -48.5 % | equal |
| dense `-s5`, `-n3`, `-eb`, `-el -aa` | -0.15 to +0.21 % | -9.5 to -28.7 % | equal |
| dense `-tc`, `-tm`, `-tg` (with and without `-n3`) | -0.19 to +0.29 % | -9.4 to -24.5 % | equal |
| dense `-tm -s5 -n2` / `-n3` / `-n4` (A1) | -0.09 / +0.04 / +0.01 % | -35.3 / -13.4 / -11.7 % | equal |
| dense `-tr -n4 -c90` (`-el`, `-en`, `-ey`) | -0.11 to +0.21 % | -25.7 to -26.9 % | equal |
| long `-tc` / `-tm` | -0.11 / -0.09 % | -15.1 / -15.2 % | equal |
| d2000 `-tc -n3` / `-tm -n3`; d20000 `-tm -s-2 -n2` | -0.11 to +0.07 % | -9.3 to -21.2 % | equal |
| tier `-tm -n3` (A1) / `-tc -n3`; split `-tm -n3` (A1) | +0.02 to +0.03 % | -14.3 to -35.7 % | equal |
| `-z`: dense `-s5`, dense `-tr`, sparse `-tc` | -0.21 to +0.12 % | n/a | equal |
| README d20000 `-tm -s2 -n3` | +0.00 % (268.0G; C 118.2G) | n/a | equal |

User time follows the instruction counts. For example, README d20000
took 18.19 s on 9f1aa62 and 18.05 s on a5f548c. Peak RSS is equal
within 0.4 MB on every row, except dense `-tg -n3`, which is 1.6 MB
lower on a5f548c.

## fpgpsp (G)

| Row | 9f1aa62 | a5f548c | Δ | C (fixed) | main/C | md5 |
| --- | --- | --- | --- | --- | --- | --- |
| B `-gr -c10 -Z1` | 3.027 | 3.028 | +0.04 % | 5.148 | 0.59x | equal |
| B `-gp -c20` | 5.423 | 5.425 | +0.03 % | 2.322 | 2.34x | equal |
| B `-gi -c10` | 0.399 | 0.396 | -0.64 % | 0.182 | 2.17x | equal |
| B `-ge` | 0.949 | 0.949 | -0.03 % | 0.794 | 1.19x | equal |
| C `-gs -c50` | 3.372 | 3.370 | -0.05 % | 1.511 | 2.23x | equal |
| C `-gp -c50` | 4.400 | 4.401 | +0.00 % | 1.950 | 2.26x | equal |

## B×360 (195 MB, above the retention budget, input read twice)

The file was generated, used once and deleted.

| Row | v0.1.0 | 9f1aa62 | a5f548c | Δ | PR #55 (main / PR) | RSS MB (9f1aa62 / a5f548c) |
| --- | --- | --- | --- | --- | --- | --- |
| `fpgrowth -s50` | 75.43 | 53.42 | 44.42 | -16.8 % | 53.41 / 44.44 | 134.7 / 134.5 |
| stdin `fpgrowth -s50` | - | 54.00 | 45.03 | -16.6 % | 54.04 / 45.03 | 134.8 / 134.7 |
| `fpgpsp -ge -z1` | - | 61.85 | 52.84 | -14.6 % | 61.87 / 52.84 | 421.0 / 420.6 |
| stdin `fpgpsp -ge -z1` | - | 62.53 | 53.49 | -14.5 % | 62.49 / 53.47 | 420.9 / 420.6 |
| `fpgpsp -gr -s50 -c1` | - | 60.42 | 51.46 | -14.8 % | 60.49 / 51.37 | 371.1 / 371.1 |
| `fpgpsp -gp -s50 -c1` | - | 61.26 | 51.91 | -15.3 % | 61.21 / 51.91 | 371.2 / 371.0 |

## Library benchmarks (benchstat, 9f1aa62 vs a5f548c, 6 interleaved runs)

| Benchmark | Δ time | p | Δ allocs/op |
| --- | --- | --- | --- |
| tract `ItemBase_BuildBag` | -1.39 % | 0.030 | -2 |
| tract `BuildBag/items=100`, `/items=2000` | ~ | 0.79 / 0.54 | -2 |
| tract `BuildBagSelect` (AppearNone) | -2.22 % | 0.026 | -2 |
| tract `CountRecode` (control) | ~ | ≥ 0.18 | = |
| root `ItemChecks` | ~ | ≥ 0.39 | = |
| root `SuiteSources` sparse memory-string / weighted memory-int | -0.67 % / -3.95 % | 0.032 / 0.004 | -2 |
| root `SuiteSources`, other 10 rows; `SuiteReadOnly` | ~ | ≥ 0.052 | -2 or = |

## Findings

| ID | Sev | Summary |
| --- | --- | --- |
| P3-1 | Info | No CLI row regressed against 9f1aa62; outputs have the same md5 on v0.1.0, 9f1aa62 and C. |
| P3-2 | Info | All 42 rows that can be compared with v0.1.0 stay below it, by 9.3-48.5 %. The A1 rows are unchanged from the previous audit. |
| P3-3 | Info | The merged state reproduces PR #55's numbers for the over-budget rows within 0.2 %; RSS is unchanged. |
| P3-4 | Info | The fix for PR #55's finding P1 holds after the merge: `BuildBagSelect` -2.2 %, `ItemBase_BuildBag` -1.4 %. |
| P3-5 | Low | PR #55's finding P2 (+1.5-1.7 % on 500k one-item records in the library) was not re-measured, because its ad hoc benchmark was not committed. The committed benchmarks show no regression. |
| P3-6 | Info | Method: C was not rebuilt (byte-identical inputs). PR #55's `-R sel300` row was not repeated. `fpgpsp -gs` on the B data was dropped as before, because both Go and C refuse it there. |
