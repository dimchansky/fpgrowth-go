# Critic review (performance) — PR #44, issue #42

- PR: https://github.com/dimchansky/fpgrowth-go/pull/44 (branch `perf/42-sibling-rows`)

- Head reviewed: c75bb225d30bc9c1dd3b36beec0a368b4529ba1c
- Decision: ACCEPT WITH CONDITIONS (one Low doc item)
- Recorded by the orchestrator from the critic's final message. Instructions retired (/usr/bin/time -l), min of 2 interleaved runs; load 15-44 (wall and ns/op secondary). Own binaries fpg-br42, fpg-main42 (bd39eec), variants w025/w06/w10; v0.1.0 fpg-v010; C fpg-cref.

### #42 rows vs v0.1.0 (claims reproduced within 0.2 pp)
| Row | v0.1.0 | main | branch | C |
|---|---|---|---|---|
| dense -tm -s5 -n3 | 4.464 G | +7.2 % | 4.421 G (-1.0 %) | 2.369 G |
| dense -tm -s5 -n4 | 9.469 G | +9.7 % | 8.914 G (-5.9 %) | 2.396 G |
| tier -tm -n3 | 25.98 G | +4.8 % | 21.17 G (-18.5 %) | 5.32 G |
| split -tm -n3 | 16.70 G | +3.3 % | 14.18 G (-15.1 %) | 3.26 G |

### Other rows, branch vs main
- d20000 -tm -s2 -n3 (1 run): 293.8 -> 270.0 G (-8.1 %); v0.1.0 not re-run.
- d5000 -n3 -7.6 %; large -n2/-n3 -4.6/-5.7 %; sparse -n3 -4.8 %; dense -n5 -10.8 %; tier -At -15.3 %; split -As -17.7 %; weighted -3.3 %; long -s2 -n3 -2.5 %; dense unlimited and d2000 -n4 -0.1 %. No row above main.
- lp50000 -As 0.876 vs 0.889 G (+1.5 %, main spread 11 %: noise); advB-151 0.0 %; advC-300/750 -0.2 % (3 runs).
- benchstat (6 rounds, wall, loaded): only SuiteMaxSize s=5 maximal n=4 significant (-14.3 %); allocs/op -1..-4 % on SuiteMaxSize maximal. Fixed-iteration instructions: SuiteMaxSize maximal s2n3 -12.7 %, s2n5 -7.9 %, s5n3 -7.2 %, s5n4 -14.8 %; s=2 targets -3.5 %; CloMaxLimited tm -0.2 %.

### Adversarial search (gendense.py: A 100kx60, B 20kx200, C 200kx40, D 5kx300, E 200kx150, F 50kx100, Dw/Bw weighted; -tm -n3/-n4/-n6; 30 rows)
Branch 2.5-39 % below main on every row. Factor 0.4 vs 1.0 (br42-w10) never costs more than ~1 % (within noise); helps dense -n3 -5.4 pp, -n4 -8.5 pp, A -s10 -n4 -6.0 pp, C -s10 -n4 -5.0 pp, d5000 -4.4 pp. Most gain on tier/split/D/E/F is from the loop rewrite.

### sibWordCost sensitivity (dense n3 / n4 vs v0.1.0)
| sibWordCost | -n3 | -n4 |
|---|---|---|
| 0.25 | -2.9 % | -6.7 % |
| 0.4 | -1.0 % | -5.9 % |
| 0.6 | +1.3 % | -4.5 % |
| 1.0 | +4.4 % | +2.6 % |
tier, split, large, d20000, weighted unaffected (±0.3 %). 0.25 at most ~1 % worse than 0.4 on any shape.

### Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| P1 | — | confirmed | All claimed figures reproduce; #42 acceptance met; #19 speedup kept |
| P2 | — | confirmed | No adversarial shape slower than main |
| P3 | — | confirmed | sibWordCost 0.25-0.6 robust; only ~1.0 regresses dense |
| P4 | Low | open | Calibration CPU (Apple M1 Max, arm64) and 0.25-0.6 tolerance not recorded at sibWordCost (extsib.go), design doc; CHANGELOG states 0.4 as general fact |
| P5 | Info | noted | Gain scaling conservative (row word tests not scaled): builds only less than main |

## Focused verification of 58404cd

C1 and C2/P4 fixed in documentation and one code comment only (no code change; the orchestrator checked the diff). gofmt, `go vet ./internal/fpg`, `go test ./internal/fpg` pass; CI 15/15.
