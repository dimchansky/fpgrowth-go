# Final integration audit — performance (main e6d02b0 vs v0.1.0 and C)

- Decision: REGRESSIONS FOUND (one, A1, from #19)
- Environment: Apple M1 Max, go1.27.1; C native/zlib/fpgpsp-fixed; night run 21:02-21:11, load 2.0-3.7; instructions retired primary; 2-run spread <= 0.7 % (C large-m-n3 1.2 %). Raw data: final/audit-perf/res/, p9-probe.txt.
- Recorded by the orchestrator from the auditor's final message.

## Results (G instructions, min of 2; md5 identical across binaries on every row)
| row | v0.1.0 | main | C | main/v0.1.0 |
|---|---|---|---|---|
| sparse -s0.1 / -n3 / -tc / -tm / -tg | 0.504/0.492/0.550/0.510/0.530 | 0.506/0.492/0.550/0.511/0.530 | 0.16-0.18 | +0.32/+0.01/0.00/+0.16/+0.05 % |
| sparse -tr -c60 / -el / -ep / -ei / -ez | 0.836/1.771/1.849/1.685/1.837 | 0.841/1.780/1.858/1.684/1.846 | 0.34-0.91 | +0.56/+0.52/+0.46/-0.06/+0.49 % |
| sparse -tr -ef -d1 | 5.892 | 3.318 | 3.877 | -43.7 % |
| thresholds (3 rows) | 0.702/0.569/3.640 | 0.703/0.572/3.638 | | +0.10/+0.49/-0.04 % |
| weighted -w / large -s / large -tc -n3 / large -tm -n3 | 0.458/4.752/5.630/6.208 | 0.458/4.753/5.644/6.272 | 0.14/1.22/1.23/1.20 | -0.08/+0.02/+0.25/+1.04 % |
| dense -s5 / -n3 / -eb / -el -aa | 5.844/3.399/3.630/1.960 | 5.848/3.402/3.632/1.959 | 2.81/1.56/1.57/0.74 | +0.06/+0.09/+0.07/-0.05 % |
| dense -tc / -tc -n3 / -tm / -tg / -tg -n3 | 5.956/4.035/5.023/2.511/2.269 | 5.961/4.037/5.026/2.515/2.271 | 0.95-2.89 | +0.05..+0.17 % |
| **dense -tm -s5 -n3** | 4.465 | 4.783 | 2.373 | **+7.14 %** |
| **dense -tm -s5 -n4** | 9.471 | 10.390 | - | **+9.70 %** |
| dense -tm -s5 -n2 | 1.594 | 1.591 | - | -0.20 % |
| dense -tr -n4 -c90/-el/-en/-ey | 2.07-2.16 | 2.08-2.17 | 0.82-0.88 | +0.14..+0.22 % |
| long -tc / -tm | 4.218/4.216 | 4.218/4.225 | 1.75 | 0.00/+0.21 % |
| d2000 -tc -n3 / -tm -n3 | 0.490/0.450 | 0.490/0.452 | 8.45/6.08 | +0.08/+0.42 % |
| d20000 -tm -s-2 -n2 | 5.761 | 5.758 | - | -0.06 % |
| tier -tm -n3 / -tc -n3 | 25.979/14.287 | 27.197/14.347 | 5.32/5.36 | **+4.69**/+0.42 % |
| split s50000x414 -tm -n3 | 16.706 | 17.264 | - | +3.34 % |
| -z dense -s5 / dense -tr / sparse -tc | n/a | 8.532/2.192/0.599 (plain 5.848/2.075/0.550) | 8.590/1.073/0.298 | -z overhead Go +46/+6/+9 %, C +202/+30/+63 % |

- R3-1 CloMaxLimited/dense2000x40d60/tm/n3: ~ (37.3 vs 37.3 ms, p=0.63, n=10); allocs +1.8 %.
- Reporter/frequent +2.22 % (p=0.008); pex-hypercube -0.9 %.
- fpgpsp main vs C (20k sparse): -gr 1.93x, -gp 2.86x (matches earlier).
- P-9: FisherInfo 0 allocs below 1e15; 1.6k-8.8k allocs/eval in 11 of 24 strong tables at 1e15-1e18, <= 0.77 ms, all values 0; unchanged.

## Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| A1 | HIGH | confirmed | dense -tm -n3/-n4 +7.1 %/+9.7 % instructions vs v0.1.0 (wall +8.6 %, user +9.2 % in a 5-run repeat at load 2.6). Bisect: 3c0b7e8 = v0.1.0 (+0.10 %); the whole increase arrives with #19 (ba63d14), unchanged through #20, #21, main. Same cause: tier -tm -n3 +4.6 %, split +3.4 %. -n2 unaffected. Contradicts PROGRESS "no measured regression" and m5-investigation "+3.9 % not significant". Within the 3.25x cost bound but neither measured nor stated. Fix the sibling-row build rule for this shape, or maintainer accepts explicitly with figures documented; add dense -tm -s5 -n3/-n4 to configs.txt. |
| A2 | LOW | confirmed | large-m-n3 +1.0 % (same source). |
| A3 | LOW | accepted earlier | Reporter/frequent micro +2.2 % (#21); nothing end to end. |
| A4 | LOW | not new | P-9 as above (now #40). |
| A5 | INFO | confirmed | R3-1 resolved; fpgpsp ratios confirmed; -ef 44 % faster; -z overhead below C's; all other rows within ±0.6 %. |

## Resolution

A1 and A2 became issue #42 (PR #44); the repeated audit on main 37d33b3 measures these rows 12-42 % below v0.1.0 (`final-audit-2-performance.md`).
