# Critic review (night sweep) — PR #41, issue #29

- PR: https://github.com/dimchansky/fpgrowth-go/pull/41
- Working dir: `~/.cache/fpgrowth-cycle/29/review-sweep`

- Head swept: 084c91d440b1fc54c34dac7a783149d8d7911006 (rebuilt with `./build.sh release`)
- Decision: ACCEPT (no Critical, High or Medium findings)
- Recorded by the orchestrator from the critic's final message.
- Time: light checks by day; night sweeps 2026-10-26 23:31-23:48 (load 2.2 at start, 16-35 later).

## Light checks
- 26e99f0: reference `go test ./...` exit 0; default difftest 0 FAIL, 95 M2 cases compared with C at -s-1.
- 084c91d: `go test ./...` exit 0; 0 FAIL; 594 M2 cases compared with C at -s-1; `TestHarnessGaps` passes.
- The issue cases (seed 1: 31717, 67615; seed 11: 10442) pass on the PR harness.
- gofmt, go vet, staticcheck clean in reference/.

## Night sweeps
| Run | Seed | Cases | FAIL | M2 cases strict vs C -s-1 |
| --- | --- | --- | --- | --- |
| PR | 1 | 50,000 | 0 | 2,894 |
| PR | 11 | 50,000 | 0 | 2,924 |
| PR | 2 | 30,000 | 0 | 1,810 |
| PR | 3 | 30,000 | 0 | 1,831 |
| PR | 4242 | 30,000 | 0 | 1,791 |
| PR + log lines | 90210 | 30,000 | 0 | 1,793 |
| origin/main | 1 | 50,000 | 1 (case 31717, M1 count-only -Z gap) | — |

220,000 cases on the PR harness, 0 FAIL: no false failures.

## Seed 1, origin/main → PR (same 50,000 cases)
deviation-verified 3470 → 2550 (−920): byte-equal +857, semantic-equal +14,
tolerated +15, error-equal +35, FAIL −1. Nothing became newly excused.
Exact brute-force checks 30245 → 30211 (−34), matching the cases that moved
to error-equal against C -s-1.

## Findings
| ID | Sev | Status | Summary |
| --- | --- | --- | --- |
| S1 | LOW | confirmed, documented | Rules with -c0 keep M2's old exemptions (`withMinSupportOne` returns nil): 344 of 2,137 M2 cases at seed 90210 (~16%). The C rerun never failed. |
| S2 | INFO | confirmed | "sets/rules at the threshold" tolerated 370 → 761 at seed 1, because newly strict M2 cases go through the normal threshold tolerance; tolerated +15, none failed. |
| S3 | INFO | confirmed | origin/main fails case 31717 at seed 1; the PR fixes it. 67615 (beyond 50,000) passes via DIFFTEST_ONLY. |
