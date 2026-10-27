# Critic reviews (correctness) — PR #43, issue #34

- PR: https://github.com/dimchansky/fpgrowth-go/pull/43 (branch `perf/34-maximal-cover`)
- Working dir: `~/.cache/fpgrowth-cycle/34/review-correctness`
- Rounds: 1 on 2fecaf1, 2 on bc00619, 3 on 59cddde. The last Low (H1/R3-F1) was fixed in a47d4ae and checked by focused verification.

## Critic review (correctness) — PR #43, issue #34, round 1

- Heads reviewed: 2fecaf1 (REJECT, F1 High) and fd7163d (ACCEPT WITH CONDITIONS; precheck pushed during the review)
- Recorded by the orchestrator from the critic's final message. Load 2.5-8.

### Correctness (both heads): verdict sound, output byte-identical to main
- Weights > 0 (extcheck.go:159 skips w <= 0): counts <= true supports; items dropped only when cnt + rest < smin; untouched items cannot become candidates (needs rest < smin).
- Precheck hit means cnt + w(t) >= smin, a valid lower bound.
- Edge cases: smin <= 0 never defers; empty X exercised (zmin 0); length ties irrelevant (cover order); n is lv.n; deferLen saturation exercised by plainGuard = MaxInt (mutant M11 killed), defensive on 32-bit.
- c.work feeds only the sibling-build heuristic, never a verdict.

### Evidence (~/.cache/fpgrowth-cycle/34/review-correctness/: repo at 2fecaf1, head2 at fd7163d)
- gofmt clean; vet, staticcheck, linux/386 vet ok; go test ./..., fpgforce, race ok on both heads.
- reference build.sh + go test ./... ok on both heads (difftest ~60 s).
- Oracle FPG_M5_CASES=20000: 440,000 comparisons, both builds, both heads.
- Property FPG_DEFER_ROUNDS=30000: 6,199,167 checks (2fecaf1, matches implementer), 6,890,832 (fd7163d); both builds pass.
- Own adversarial tests (critic34_test.go, local): equal/random-length long light lines, weights 0/heavy/up to 2^24, covers over many words, smin up to 2^22; mining vs brute force and oracle 3,000 seeds; scan 656,816 checks (164k deferred, 16k searched, 13.5k capped); all pass both builds.
- CLI sweep main vs PR (default and fpgforce), -tm -n1..4 x -A variants, -q/-x/-u/-m2/-w: 26,400 runs (2fecaf1), 14,400 (fd7163d), 0 differences. C differences seen also occur on main (C segfault -11, known M5).
- md5 lp12500/25000/50000/50000s2 (-n1, -n2)/50000w/lpc20000 match main; instructions 0.24/0.44/0.91/1.01/0.96/15.96 G.
- Mutants (mut*.txt): every verdict-changing mutant killed (M1, M4, M15, M16, M18, M19, P2, P4).

### Findings
| ID | Sev | Status | Summary | Evidence |
|---|---|---|---|---|
| F1 | High (2fecaf1) | Fixed in fd7163d | V + |t1| <= 2V counts t1 in full; early exit inside t1 lost (quadratic). | TestCritic34EarlyExitInT1: plain 250 ns vs deferred 662 us (~2650x); fd7163d 167 ns |
| F2 | Medium (fd7163d) | Open | precheck passes over all touched items for every long light transaction before t1 is chosen, even when the search is too expensive: up to n passes per check, outside "2V + S"; work counter unchanged so the property test cannot see it. Fix: count t in cover order without prechecking when |touched| >= |t|. | TestCritic34PrecheckPasses n=200/400/800: plain 0.35/1.4/6.0 ms vs 6.0/47/374 ms (17x/34x/62x), work 640,600 both |
| F3 | Low | Open | Tests miss a cap-weakening fault (budget reset by a later deferral, M5). Add TestCritic34CapSecondLong. | M5: work 370 vs plain 90 passes all PR tests |
| F4 | Low | Open | Stale shortest-first wording: benchmarks.md:799, m5-investigation.md:516; code counts deferred lines in cover order (lpc: first, longest chain, +0.6 %). | scanDeferred; gen/core.go:74 |
| F5 | Low | Open | CHANGELOG.md:128 is 102 characters. | awk |
| F6 | Info | — | Cost-only/equivalent mutants survive: M6/M14, M7/M10/M13/M17, M9/M12/M20, M2/M3. | mut2.txt, mut3.txt |

## Critic review (correctness) — PR #43, issue #34, round 2

- Head reviewed: bc0061984471b70d9531613a4ab1bddd25a4814e
- Decision: ACCEPT WITH CONDITIONS (G1 Medium open)
- Recorded by the orchestrator from the critic's final message.

### Answers
- §12 proof: sound for the verdict. Precheck pass P <= |touched| <= pre; one precheck per check (hit stops, decline sets capped, cheap sets t1). S <= |C1|*bitlen <= pre*log|t1| (log factor; last-probe hit covered). Gap: "Candidate filters ... are not counted" — the filter runs once per deferred transaction, D*|alive| total (G1).
- measureScan = work - skipped + prepass; omits the candidate-build pass (once per check, harmless) and the scanDeferred filter passes (G1), so it can hide cost.
- Old shapes: PrecheckPasses n=400 1.4 ms plain and deferred (was 34x); EarlyExitInT1 83-375 ns vs 167 ns-1.1 us (was ~2650x).
- Byte identity: sweep.py 14,400 runs (6 seeds, default+fpgforce), 0 differences; lp12500/25000/50000/50000s2 (-n1,-n2)/50000w/lpc20000/lp12500 -At -n3 md5 equal to main; 0.22/0.43/0.94/1.01/0.99/16.42 G.
- Commands: gofmt, vet, staticcheck, linux/386 vet; go test ./..., fpgforce, race -short; FPG_M5_CASES=20000 (440,000, both builds); FPG_DEFER_ROUNDS=30000 (6,890,832 checks) + TestExtCheckMaximal* both builds; critic tests 3,000 rounds (656,816 checks) both builds.

### G1 evidence (TestCritic34FilterPasses, local head3)
S short lines weight smin-2 with own items, then D=S-1 long light lines weight 1, smin=D+1; each too long to search, counted in full, then filtered.
| n | measureScan defer/plain | plain | deferred | slowdown |
|---|---|---|---|---|
| 99 | 1.5x | 0.14 ms | 1.4 ms | 10x |
| 199 | 1.5x | 0.37 ms | 7.2 ms | 19x |
| 399 | 1.5x | 1.4 ms | 55 ms | 39x |
Fix sketch: filter only when `cands && len(alive) <= len(items)` (stale candidate list is a superset; alive counts stay exact; stale dead items keep partial counts below support). Sketch gave 1.5x wall at n=199/399; go test ./internal/fpg passes both builds; reverted.

### Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| F1 | High (2fecaf1) | Resolved | Early exit inside t1 |
| F2 | Medium | Resolved | Repeated prechecks (TestExtCheckMaximalOnePrecheck) |
| F3 | Low | Resolved | TestExtCheckMaximalCapSecondLong |
| F4 | Low | Resolved | "shortest" wording |
| F5 | Low | Resolved | CHANGELOG wrap |
| G1 | Medium | Open | scanDeferred candidate filter runs once per deferred transaction (D*|alive|), outside 2V+S, invisible to measureScan |
| G2 | Info | — | S term is O(pre*log|t1|), documented |

Condition: fix G1 with a regression test that measures filter passes; drop "not counted" from §12.

## Critic review (correctness) — PR #43, issue #34, round 3

- Head reviewed: 59cdddeabf02819611f796090162adec55ee691d
- Decision: ACCEPT (no Critical/High/Medium; Low/Info optional)
- Recorded by the orchestrator from the critic's final message.

### Answers
- Superset argument holds: an item leaves alive only when cnt + rest < smin and rest only decreases; untouched items at build have support <= rest < smin; stale items keep exact partial counts below support; a deferred transaction is either searched below |t| or counted in full.
- 4V+2S sound: visits + P <= 2V+S (§12); build B <= |touched| <= visits before; each filter |alive| <= `since`.
- Delayed empty-alive exit is cost only (maximal X: plain visits every deferred transaction too).
- measureScan counts visits, probes, prepass, build and filters; not the precheck's second loop over touched (<= pre, so true total ~5V+2S, H1); final reset pass <= visits, same as plain (H3).

### Shapes on head4 (wall, plain vs deferral)
| Shape | Size | Plain | Deferral | Ratio |
|---|---|---|---|---|
| FilterPasses | n=199 | 0.38 ms | 0.50 ms | 1.3x (19x on bc00619) |
| FilterPasses | n=399 | 1.42 ms | 1.90 ms | 1.3x (39x) |
| FilterPasses | n=799 | 5.5 ms | 7.5 ms | 1.4x |
| PrecheckPasses | n=800 | 5.57 ms | 5.61 ms | 1.0x |
| DelayedEmpty (new) | n=300/600 | | | 1.46x / 1.49x (real work 2.3x) |
| EarlyExitInT1 | | 83 ns | 167 ns | 2.0x |

### Byte identity and tests
- sweep.py default+fpgforce 14,400 runs, 0 differences; md5 of 7 lp rows equal to main.
- FPG_M5_CASES=20000 (440,000) and FPG_DEFER_ROUNDS=30000 (6,890,832) + TestExtCheckMaximal* both builds; critic tests 3,000 rounds both builds; gofmt, vet, staticcheck, linux/386 vet, go test, fpgforce, race ok.
- Lazy-filter mutants killed: filter `>`, build threshold +1, since never reset, filter every transaction. Survived (cost/measure only): since >= 8|alive|, no filtered count.

### Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| G1 | Medium | Resolved | FilterPasses 1.3-1.4x (was 10-39x) |
| F1-F5 | — | Resolved | |
| H1 | Low | Open (optional) | precheck's search loop walks touched again, not in measureScan; true bound ~5V+2S. Count it or stop after n candidates. |
| H2 | Info | — | Cost/measure-only mutants survive |
| H3 | Info | — | Final reset pass <= visits, same as plain |

## Focused verification of a47d4ae (H1/R3-F1)
`precheck` now collects the candidates in its one counted pass and
searches only them, so the stated bound 4V + 2S counts every pass. The
orchestrator read the diff; the implementer ran vet, linux/386 vet,
staticcheck, `go test ./internal/fpg` (default, fpgforce, race), the
property test at 10,000 rounds in both builds (2.3 M checks) and the
oracle at 5,000 cases (110,000 comparisons); md5 equal to main on
lp50000, advB-151 and advC 300/750. CI 15/15.
