# Critic review (robustness and portability) — PR #54, issue #53

- PR: https://github.com/dimchansky/fpgrowth-go/pull/54 (branch `test/53-cancel-tree-build-work-bound`)
- Head reviewed: 9f7fb70
- Decision: ACCEPT (Low and informational findings only)
- Recorded by the orchestrator from the critic's final message.

## Resolution of findings

- F1 (Low, pre-existing): not changed in this PR. No test in `.` or
  `internal/fpg` covers the build-loop cancellation checks
  (`buildCheckEvery = 1 << 62` passes everything); offered to the
  maintainer as a follow-up.
- F2 (Low, by design): documented in 7da8ac9 together with design
  finding D1.
- F3 (Low, report only): the remaining 500 ms `cancelLatencyLimit` in
  parallel tests is not changed; no CI failure of it is known.
- F4 (Info): the linux/386 value (62,784 bytes) is now in the comment
  (7da8ac9).

## Critic's report

Scope: the `runtime.MemStats.TotalAlloc` bound (4 MiB) across CI
targets, ordering, `-race`, `-shuffle` and GOMAXPROCS; maintainability;
evidence for the issue's "Done when" items. Load 7-12 from other work;
conclusions rest on byte counts only.

### Evidence examined

- `fpgrowth.go:260-330` and `internal/fpg/fpg.go:208-215`: `ctx.Err()` is
  checked before `rep.Setup()` and before any tree build.
- Go 1.27.3 `runtime/mstats.go`: `readmemstats_m` flushes all mcaches
  with the world stopped, so `TotalAlloc` counts each object exactly,
  independent of GOMAXPROCS.
- The root package's library code starts no goroutines. Its four
  sequential top-level tests run while parallel tests are paused, under
  `-shuffle` and `-count` too.
- CI matrix: `-shuffle=on` on ubuntu amd64/arm64, macOS arm64, Windows;
  `-race` everywhere except Windows; linux/386 with `CGO_ENABLED=0`;
  coverage with `-covermode=atomic` (static counters).

### Commands run

| # | Command | Result |
|---|---|---|
| 1 | `GOMAXPROCS=1 go test -race -shuffle=on -count=1 -v .` | ok; 5 subtests at 71,232 B / 12 objects |
| 2 | `GOMAXPROCS=2 go test -shuffle=on -count=2 -v .` | ok; 10/10 at 71,232 B |
| 3 | darwin/amd64 binary under Rosetta, `-count 2` | PASS 10/10 at 71,232 B |
| 4 | linux/arm64 static binary in a container | PASS 5/5 at 71,232 B |
| 5 | linux/386 static binary in an emulated container | PASS 5/5 at 62,784 B |
| 6 | Mutation B (no `ctx.Err()` at the start of `Miner.Mine`), darwin/arm64 | FAIL 5/5: 128-285 MB; latencies 2-53 ms (30 ms would have missed 3 of 5) |
| 7 | Mutation B on linux/386 | FAIL 5/5: 107-238 MB |
| 8 | `GOGC=10`, the four sequential tests, `-shuffle=on -count=2` | ok; 10/10 at 71,232 B |
| 9 | `buildCheckEvery = 1 << 62` alone, `go test ./internal/fpg/ .` | ok: no test fails (F1) |
| 10 | gofmt; vet for darwin, windows/386, wasip1/wasm; staticcheck | clean |
| 11 | `gh pr checks 54` | 15/15 pass |

Mutations were reverted and checked with `git status`.

### Findings

| ID | Severity | Status | Summary |
|---|---|---|---|
| F1 | Low (pre-existing) | Confirmed | Build-loop cancellation checks are not covered by any test. |
| F2 | Low | By design | The time between cancellation and return is unbounded; a non-allocating O(bag) pass before the check would pass. |
| F3 | Low (report) | Assessed | `cancelLatencyLimit` 500 ms in parallel tests: at most ~19 ms locally, but a past 241 ms on a hosted macOS runner means moderate `-race` flake risk. |
| F4 | Info | Confirmed | linux/386 intact value 62,784 B; mutation still 107-238 MB. |

### "Done when"

1. Every subtest fails without the check: met, reproduced on
   darwin/arm64 and linux/386 (smallest value about 25 times the limit).
2. Reliable under `-race`: met through the "local `-count` run under
   load" branch (implementer 100/100; this review GOMAXPROCS 1 and 2,
   shuffle); CI passed once on every row.

### Decision

ACCEPT. The bound is exact, identical on every target tried, isolated
by Go's sequential and parallel test phases, about 59 times above the
intact value and at least 10 times below the smallest mutated value.
