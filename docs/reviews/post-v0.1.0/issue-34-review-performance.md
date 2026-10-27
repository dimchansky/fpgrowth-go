# Critic reviews (performance) — PR #43, issue #34

- PR: https://github.com/dimchansky/fpgrowth-go/pull/43 (branch `perf/34-maximal-cover`)
- Working dir: `~/.cache/fpgrowth-cycle/34/review-perf`
- Rounds: 1 on 2fecaf1, 2 on bc00619, 3 on 59cddde. The last Low (H1/R3-F1) was fixed in a47d4ae and checked by focused verification.

## Critic review (performance) — PR #43, issue #34, round 1

- Head reviewed: 2fecaf176bbdb106132a366c599745e69d21938f
- Decision: REJECT (one High: F1)
- Recorded by the orchestrator from the critic's final message. Night window 01:48-02:03 EET; load 2.5-96, instructions retired only.

### Verified
- lp (min of 2, instructions): main 8.03/31.68/125.9 G (lp12500/25000/50000); branch 0.224/0.440/0.901 G (1.96x, 2.05x per doubling: linear); v0.1.0 7.72/30.43/120.9 G; C 2.04 G (12.5k), 31.79 G (50k). lp50000s2 126.0 -> 0.996 G; lp50000w 126.0 -> 0.937 G; lpc20000 16.28 -> 15.99 G (quadratic).
- Ordinary rows branch vs main: large-n2 -0.54 %, large-n3 -0.10 %, d2000-n3 -0.05 %, d5000-n3 -0.16 %, d20000-n2 +0.04 %, d20000-n3-As +0.01 %, tier-n3-At +0.07 %, split -0.02 %; tier-n3 +0.10 % (low-load min of 4).
- #42 rows vs main: dense-n3 -0.02 %, dense-n4 -0.01 %; vs v0.1.0 +7.1/+9.7/+5.0/+3.2 % (existing #42).
- No ordinary row defers (instr34_test.go: deferred/capped/lookups 0 on large-n2, dense-n4, tier-n3, d2000-n3).
- benchstat BenchmarkCloMaxLimited (6 maximal sub-benchmarks, 6 runs): no change (p >= 0.25).

### Findings
| ID | Sev | Status | Summary | Evidence |
|---|---|---|---|---|
| F1 | HIGH | confirmed | New quadratic shape: when the plain scan would exit early inside the first deferred t1, deferral loses that exit; the "2x" bound counts t1 in full, so it does not bound real work. | genadv.py AG(2,q) shape, `-As -tm -s-2 -n1 -q0`: advB-101-102 main 2.44 G / branch 7.71 G; advB-151-152 7.87 G / 34.12 G (4.3x); main grows 3.2x, branch 4.4x. -q2: 2.57 -> 7.82 G; without -As 8.01 -> 34.17 G. Output identical. Unit probe: scan 1.09 ms plain vs 263 ms deferred (~3 vs ~10,200 visits per check), c.work reports 2x. |
| F2 | MEDIUM | confirmed | CHANGELOG, docs/m5-investigation.md, TestExtCheckMaximalCap comment and PR body ("0 lost early exits") claim a 2x visit bound measured in c.work units that charge whole transactions. | Same as F1; property test compares w > 2*plain in c.work units only. |
| F3 | LOW | confirmed | lpc20000 stays quadratic, as stated (-1.8 %). | full.tsv |

### Requested fix
Before deferring t1, binary-search in t1 the touched items with cnt[j] >= smin - w(t1); a hit means not maximal (as the plain scan finds it). If that costs >= |t1|, do not defer t1. Then V + |t1| <= 2V holds against actual visits. Add the advB shape as a real-visit regression test; make the property test count real visits; correct the wording.

Files: ~/.cache/fpgrowth-cycle/34/review-perf/ (repo, bin, genadv.py, out/).

## Critic review (performance) — PR #43, issue #34, round 2

- Head reviewed: bc0061984471b70d9531613a4ab1bddd25a4814e (binary review-perf/bin/fpg-br2)
- Decision: REJECT (R2-F1 High; same defect as the correctness critic's G1, found independently)
- Recorded by the orchestrator from the critic's final message. 03:03-03:10 EET, load 4-7.

### Verified (instructions, min of 2, main vs branch)
- advB -As -tm -s-2 -n1: q101 -q0 2.44 vs 2.45 G; q151 -q0 7.90 vs 7.88 G; q101 -q2 2.52 vs 2.52 G (round 1: 7.71 / 34.12 G).
- lp branch: 0.231/0.460/0.895 G (12.5k/25k/50k, linear); lp50000s2 0.992 G; lp50000w 0.987 G.
- lpc20000 16.33 vs 16.42 G (+0.51 %), quadratic as stated.
- Ordinary: large-n2 +0.18 % (min of 4), large-n3 +0.24 %, d5000-n3 -0.05 %, d20000-n2 -0.11 %, tier-n3 +0.22 %, tier-n3 -As +0.26 %, split-n3 +0.09 %.
- #42 rows vs main: dense-n3 -0.02 %, dense-n4 -0.04 %; vs v0.1.0 +7.1/+9.6/+4.9/+3.5 %.
- benchstat BenchmarkCloMaxLimited: no change (p >= 0.24).
- One-precheck rule: declined precheck costs <= its pass <= visits so far; no regression shape found.

### Findings
| ID | Sev | Status | Summary | Evidence |
|---|---|---|---|---|
| r1-F1 | HIGH | fixed | Early exit inside t1 (advB) | advB-151 7.90 -> 7.88 G |
| r1-F2 | MEDIUM | partly fixed | Bound documented in real work (2V+S) but not true; see R2-F1 | "filters ... (not counted)" |
| R2-F1 | HIGH | confirmed | scanDeferred filters alive after every deferred transaction: D*A, unpaid by visits, not in measureScan; branch ~n^3 vs main ~n^2, output identical. | genadvc.py: k short lines weight smin-2 (4n items each) then D long light lines weight 1; `-w -As -tm -s-1000 -n1 -q0`. k,D=100/250: 2.34 vs 3.03 G (+29 %); 200/500: 9.11 vs 14.68 G (+61 %); 300/750: 20.25 vs 39.17 G (+94 %, 42 MB). TestAdv34Filter: plain 4.6 ms vs 250 ms, measureScan 1.96 M vs 2.52 M (1.28x). Default -q2: branch faster (229 -> 121 G). |
| R2-F2 | LOW | confirmed | S bounded only by bitlen(|t1|)*pre; last-of-many hit costs up to ~(bitlen+2)x plain. | TestAdv34Precheck q=31, 90 candidates: 90 k vs 1.06 M (11.8x, S 0.88 M); 0.38 vs 1.84 ms |
| R2-F3 | LOW | confirmed | measureScan is built from the code's own counters; counts no filter or candidate-build pass. | code reading |

### Requested
1. Filters in scanDeferred must cost <= visits (filter once visits/probes since last filter reach |alive|, or only before a searched transaction and at the end).
2. Count every pass over touched/alive in measureScan; add advC as a real-work regression test.
3. Optional: document S <= log factor * pre.

## Critic review (performance) — PR #43, issue #34, round 3

- Head reviewed: 59cdddeabf02819611f796090162adec55ee691d (binary review-perf/bin/fpg-br3)
- Decision: ACCEPT (no Critical/High/Medium; two Low, optional)
- Recorded by the orchestrator from the critic's final message. 03:33-03:38 EET, load 3-8; instructions retired, min of 2 interleaved.

### Measured main -> branch
| Row | main | branch | change |
|---|---:|---:|---:|
| advC 100/250 `-w -As -tm -s-1000 -n1 -q0` | 2.277 G | 2.288 G | +0.50 % |
| advC 200/500 | 9.100 G | 9.167 G | +0.74 % |
| advC 300/750 | 20.214 G | 20.279 G | +0.32 % (r2 39.2 G) |
| advB-101 -q0 | 2.455 G | 2.440 G | -0.60 % |
| advB-151 -q0 | 7.897 G | 7.910 G | +0.16 % |
| lpc20000 | 16.308 G | 16.383 G | +0.45 % (quadratic, documented) |
| large-n2 / large-n3 | | | +0.23 % / +0.09 % |
| d20000-n2 | | | +0.05 % (min of 4) |
| tier-n3 / tier-n3 -As | | 28.323 -> 28.395 G (-As) | +0.22 % / +0.26 % |
| split-n3 | | | +0.15 % |
| dense-n3 / dense-n4 | | | +0.01 % / -0.02 % |
- lp branch: 0.216/0.430/0.902 G (linear); lp50000s2 0.991 G; lp50000w 0.954 G (main 125.9 G).
- #42 rows vs v0.1.0: +7.1/+9.7/+4.9/+3.4 % (existing #42).
- benchstat CloMaxLimited: no change (dense1000/n1 +5.3 % p=0.03 under load; 10+10 repeat ~ p=0.97).
- Lazy filter: filter runs after visits/probes since last reach |alive|; lookup adds |alive|*bitlen so filter follows any lookup; unfiltered items are dead (count < smin) and can only make a lookup look costlier; delayed empty-alive exit costs <= |alive| extra visits. advC unit probe plain 3.9 ms vs 5.9 ms (r2 250 ms); measureScan 1.96 M vs 4.20 M (within 4V). Filter-every-line mutant fails TestExtCheckMaximalFilterPasses. FPG_DEFER_ROUNDS=20000 both builds (4.6 M checks).

### Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| R2-F1 | HIGH | fixed | advC uncounted filters |
| r1-F2 / R2-F3 | MEDIUM / LOW | fixed | measureScan counts filter and build passes; 4V+2S asserted |
| R3-F1 | LOW | open (optional) | measureScan undercounts the precheck's second loop over touched (bounded by prepass, <= 2x) |
| R2-F2 | LOW | documented | S term is a log factor |

## Focused verification of a47d4ae (H1/R3-F1)
Instructions, min of 2 interleaved (main → branch): lp50000 125.92 →
0.915 G; advC 300/750 `-q0` 20.30 → 20.33 G (+0.16 %); advB-151 `-q0`
7.89 → 7.87 G; tier `-As` 28.31 → 28.38 G (+0.24 %).
