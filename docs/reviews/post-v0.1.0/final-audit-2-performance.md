# Repeated final integration audit — performance (main 37d33b3 vs v0.1.0 and C)

- Decision: NO REGRESSIONS. Every row comparable with v0.1.0 takes 9-49 % fewer instructions on main; all outputs md5-equal across v0.1.0, main and C on every row. The first audit's A1 rows are below v0.1.0.
- Environment: Apple M1 Max, 32 GB, macOS 26.6.1, go1.27.3 (first audit: 1.27.1; its v0.1.0 figures reproduce within 0.3 %). C built with `reference/build.sh` (native as the timing reference, zlib, fpgpsp-fixed). Night window 22:34-22:46, load 2.6-5.7. One process at a time, GOMAXPROCS=2, RLIMIT_CPU 150 s, RSS watchdog; no kills, no timeouts. The first audit's `run.py`/`summ.py` and data (SHA-256 prefixes as in `docs/benchmarks.md`). Instructions retired, minimum of 3 interleaved runs (spread ≤ 1 % Go, ≤ 1.4 % C).
- Recorded by the orchestrator from the auditor's final message.

## CLI table (G instructions)

| row | v0.1.0 | main | C | main/v0.1.0 | main/C |
|---|---|---|---|---|---|
| sparse -s0.1 / -n3 / -tc / -tm / -tg | 0.503/0.491/0.549/0.509/0.528 | 0.317/0.304/0.364/0.321/0.341 | 0.159/0.152/0.182/0.160/0.185 | -37.0/-38.0/-33.7/-37.1/-35.5 % | 1.85-2.01x |
| sparse -tr -c60 / -el / -ep / -ef / -ei / -ez | 0.836/1.770/1.847/5.892/1.681/1.836 | 0.648/1.589/1.666/3.127/1.495/1.657 | 0.344/0.853/0.909/3.879/0.757/0.905 | -22.5/-10.2/-9.8/-46.9/-11.1/-9.8 % | 0.81-1.97x |
| thr-abs-r / thr-frac-m / thr-frac-s | 0.700/0.568/3.637 | 0.519/0.383/3.086 | 0.246/0.194/1.597 | -26.0/-32.7/-15.1 % | 1.93-2.10x |
| weighted -w | 0.458 | 0.261 | 0.138 | -42.9 % | 1.89x |
| large -s / -tc -n3 / -tm -n3 | 4.762/5.640/6.228 | 2.453/3.339/3.623 | 1.225/1.229/1.205 | -48.5/-40.8/-41.8 % | 2.00/2.72/3.01x |
| dense -s5 / -n3 / -eb / -el -aa | 5.841/3.402/3.627/1.956 | 5.289/2.846/3.075/1.398 | 2.810/1.565/1.571/0.740 | -9.5/-16.4/-15.2/-28.5 % | 1.82-1.96x |
| dense -tc / -tc -n3 / -tm / -tg / -tg -n3 | 5.951/4.031/5.018/2.509/2.269 | 5.397/3.473/4.462/1.955/1.714 | 2.892/2.668/2.415/1.090/0.952 | -9.3/-13.8/-11.1/-22.1/-24.5 % | 1.30-1.87x |
| **dense -tm -s5 -n2 / -n3 / -n4** | 1.589/4.464/9.469 | 1.030/3.869/8.362 | 1.591/2.377/2.396 | **-35.2/-13.3/-11.7 %** | 0.65/1.63/3.49x |
| dense -tr -n4 -c90 / -el / -en / -ey | 2.075/2.155/2.163/2.164 | 1.516/1.599/1.605/1.606 | 0.820-0.877 | -25.8..-27.0 % | 1.83-1.85x |
| long -tc / -tm | 4.214/4.216 | 3.574/3.566 | 1.742/1.752 | -15.2/-15.4 % | 2.05/2.04x |
| d2000 -tc -n3 / -tm -n3 | 0.489/0.448 | 0.443/0.404 | 8.454/6.077 | -9.4/-9.9 % | 0.05/0.07x |
| d20000 -tm -s-2 -n2 | 5.760 | 4.533 | - | -21.3 % | - |
| **tier -tm -n3** / tier -tc -n3 | 25.988/14.277 | 19.072/12.250 | 5.318/5.373 | **-26.6**/-14.2 % | 3.59/2.28x |
| **split s50000x414 -tm -n3** | 16.699 | 10.739 | - | **-35.7 %** | - |
| -z dense -s5 / dense -tr / sparse -tc | n/a | 7.985/1.636/0.411 | 8.596/1.074/0.299 (zlib build) | - | 0.93/1.52/1.37x |

User time moves the same way (e.g. dense -tm -n4 0.74 → 0.68 s, tier 3.31 → 2.99 s, large -s 0.53 → 0.35 s).

## Peak RSS (MB, median; v0.1.0 / main / C)

| rows | v0.1.0 | main | C |
|---|---|---|---|
| sparse (non -tr), thr-frac-m | 20.1-21.1 | 20.7-21.2 | 11.8-21.4 |
| sparse -tr, thr-abs-r | 29.5-49.0 | 29.3-48.5 | 10.9-20.7 |
| weighted | 19.8 | 21.4 | 10.9 |
| large -s | 101.2 | 108.2 | 73.0 |
| large -tc -n3 / -tm -n3 | 119.1 / 119.0 | 108.3 / 108.3 | 73.7 |
| dense -s/-tc/-tm rows | 40.0-43.1 | 42.1-44.0 | 52-103 |
| dense -tg / -tg -n3 | 72.4 / 72.2 | 75.5 / 75.4 | 73.1 |
| dense -tr, -el -aa | 35.7 | 31.6-32.0 | 12.2-12.6 |
| long | 81.7 | 84.0 | 328.2 |
| d2000 | 15.0 | 15.1-15.2 | 33-35.5 |
| d20000 -n2 | 106.2 | 111.0 | - |
| tier -tm / -tc | 202.4 | 210.6 | 727.8 |
| split | 234.0 | 247.0 | - |

## Checks

- (a) A1 rows: at c66d052 (#42) dense -n3 -0.88 %, -n4 -5.88 %, tier -18.5 %, split -15.2 %, large -tm -n3 -4.6 % (as the #42 review); unchanged at 95f2794; #48 lowers them further (main: -13.3/-11.7/-26.6/-35.7/-41.8 %). The control row dense -s5 moves only with #48 (5.840 → 5.842 → 5.288 G).
- (b) No row above v0.1.0; no bisect needed. Smallest improvement dense -tc -9.3 %.
- (c) RSS increases match D8 (4 bytes per item occurrence and per record): estimate vs measured split ~13.6/+13.0, tier ~8.7/+8.2, d20000 ~4.9/+4.8, dense ~2.3/+0.9..+2.1, sparse ~0.8/+0.5..+0.7, weighted ~1.0/+1.7 MB; all far below the 128 MiB budget; -tr rows and large -n3 go down. Only dense -tg moves from below C to above it (72.4 → 75.5 vs 73.1 MB).
- (d) fpgpsp (G; e6d02b0 / main / C fixed; md5-equal): B -gr -c10 -Z1 9.960/3.028/5.148 (-69.6 %, 0.59x C); B -gp -c20 6.633/5.424/2.322 (-18.2 %, 2.34x); B -gi -c10 0.571/0.398/0.182 (-30.2 %, 2.18x); B -ge 1.402/0.951/0.795 (-32.1 %, 1.20x); C -gs -c50 4.232/3.371/1.511 (-20.3 %, 2.23x); C -gp -c50 5.396/4.401/1.950 (-18.4 %, 2.26x).
- (e) d20000x200d30 -tm -s2 -n3, 2 runs: Go main 18.40/17.89 s wall, 268.0 G, 114 MB; C native 38.37/38.24 s, 118.2 G, 1,082-1,259 MB; md5-equal (1,313,400 sets). README's Go 19.8 s / 111 MB, C 39.1 s / 1,259 MB holds and is now slightly conservative.
- (f) CloMaxLimited/dense2000x40d60/tm/n3, 10 rounds: time ~ (37.2 vs 37.2 ms, p=0.86), allocs/op +3.1 %. Reporter, 6 rounds: frequent +2.47 % (p=0.002, the first audit's A3), pex-hypercube -1.6 %, closed ~.

## Findings

| ID | Sev | Status | Summary |
|---|---|---|---|
| F1 | Info | confirmed | A1/A2 resolved: #42 brings the rows back to or below v0.1.0, #48 lowers them further. |
| F2 | Info | confirmed | No row regressed against v0.1.0; all 41 comparable rows 9-49 % lower; outputs md5-equal. |
| F3 | Low | confirmed, expected | RSS +0.5-13 MB where records are retained, matching D8; dense -tg now 75.5 MB vs C 73.1. No change required. |
| F4 | Low | confirmed | README d20000 figures (Go 19.8 s / 111 MB) slightly dated: now 17.9-18.4 s / 114 MB; the claim stays true. Optional refresh. |
| F5 | Low | carried over (A3) | Reporter/frequent micro +2.5 %, CloMaxLimited allocs/op +3.1 %; no effect on time or end to end. |
| F6 | Info | method | `-gs` needs table-derived data; future audits should include psp-C. |

Cleanup: clones, worktrees, binaries, C builds, data and outputs deleted; 192 KB of configs, scripts and result tables kept in `~/.cache/fpgrowth-cycle/final2/audit-perf/`.
