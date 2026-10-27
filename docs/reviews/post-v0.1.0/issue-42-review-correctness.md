# Critic review (correctness) — PR #44, issue #42

- PR: https://github.com/dimchansky/fpgrowth-go/pull/44 (branch `perf/42-sibling-rows`)

- Head reviewed: c75bb225d30bc9c1dd3b36beec0a368b4529ba1c
- Decision: ACCEPT WITH CONDITIONS (Low, docs only)
- Recorded by the orchestrator from the critic's final message. Reused clone ~/.cache/fpgrowth-cycle/34/review-correctness/repo detached at c75bb22; raw outputs ~/.cache/fpgrowth-cycle/42/critic-correctness/; load 2.3-8.

### Code
Every maximal verdict unchanged; cost model performance-only:
- f = 1 - 0.6*vwork/work in [0.4, 1] (vwork is part of work: added together in vertMaximal, reset in checkPath and sibReset, summed only while the level is not built); gain only decreases; negative gain stays below lo, so no build where main did not build.
- Touched buffers sized for every item (len(c.cnt), len(slot)); each item at most once (weights > 0, cnt == 0 / slot == 0 guards), also for empty X.
- scanDefer gets touched[:nt] with full capacity; nt resynced after; touched cut before clearing and before c.cand = touched.
- sibBuild rows fresh or cleared up front; indexed by k-1 < nFreq.
- vertMaximal word-test count unchanged.

### Findings
| ID | Sev | Status | Summary | Evidence |
|---|---|---|---|---|
| C1 | Low | confirmed | issue-42-design.md: "a level that still builds builds at the same child" is false; should be "the same or a later child". | TestExtCheckSiblingBound build child 16 -> 31 |
| C2 | Low | confirmed | Constant not documented as machine-specific (sibWordCost comment, CHANGELOG give ns timings without machine); add M1 Max + Go version and the instruction ratio (~15/39 ≈ 0.38). | only m5-investigation.md:380 mentions M1 |
| C3 | Low | confirmed, equivalent | Surviving mutants equivalent: scan buffer one short; continue vs break after an infrequent item in vertMaximal; build touched not cut to nt (support 0). | mut.txt, mut2.txt |
| C4 | Info | verified | New tests meaningful: sibWordCost 1.0/0.6/0.25 and inverted factor killed by TestExtCheckSiblingWordCost and TestExtCheckSiblingBound; 16 -> 31 is a re-pin after an intended rule change. | mut.txt |
| C5 | Info | verified | advb-101 and advc-100-250 equal the #34 review files after renumbering by first appearance; config rows match (-s-2; -w -s-1000). | md5 a09d9531… / 449fb6d1… (canon.py) |

### Commands
- gofmt, go vet, staticcheck clean; go test -count=1 ./... default and fpgforce (incl. TestLargeFileStreaming).
- FPG_M5_CASES=20000 both builds (46 s / 74 s); FPG_DEFER_ROUNDS=30000 both builds.
- reference build.sh + go test ./... (difftest 82 s, gen, modcheck) pass.
- Mutants: 15; 13 killed by committed tests, 2 equivalent.
- CLI sweep vs main bd39eec: dense random (-tm -n1..4, -A s/c/d/t, -q, -m, -x, -l0, -w) 2,385 runs PR + 1,193 fpgforce, 0 differences; sweep34.py 540 runs, 0 main/PR differences (5 C differences are known M5 / C crash -11, also on main); ~30 runs skipped at 20 s timeout.

## Focused verification of 58404cd

C1 and C2/P4 fixed in documentation and one code comment only (no code change; the orchestrator checked the diff). gofmt, `go vet ./internal/fpg`, `go test ./internal/fpg` pass; CI 15/15.
