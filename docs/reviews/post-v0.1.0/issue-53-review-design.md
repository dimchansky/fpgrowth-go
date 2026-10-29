# Critic review (test design) — PR #54, issue #53

- PR: https://github.com/dimchansky/fpgrowth-go/pull/54 (branch `test/53-cancel-tree-build-work-bound`)
- Head reviewed: 9f7fb70
- Decision: ACCEPT WITH CONDITIONS (one Medium, D1)
- Recorded by the orchestrator from the critic's final message.

## Resolution of findings

- D1 (Medium): fixed in 7da8ac9 by option (b). The test comment says that
  work which does not allocate before the cancellation check is no longer
  detected, and that the tree build the test is meant to catch always
  allocates. Option (a), a retried latency limit, was not taken: the
  cycle's rule is no wall-clock bounds in tests, and the robustness
  critic (F2) notes that a loose time limit cannot tell such a pass from
  a 0.1-0.7 s tree build anyway.
- D2 (Low): fixed in 7da8ac9. The `treeBuildAllocLimit` comment states
  the 9-14 MB of a tree built without a presized arena and stopped by
  the build loop's checks (2-3 times the limit), and that this margin
  depends on `buildCheckEvery`.
- D3, D4: informational, no change.

## Critic's report

Scope: `fpgrowth_test.go` (the only file changed): does the `TotalAlloc`
bound catch the defect class for all 5 subtests; measurement points;
comment accuracy; what is lost compared with the 30 ms latency check.
Daytime run on 2026-10-29 under load 10.9-23.6 from other work; no
conclusion rests on wall time except D1 (a difference of about 1000x).

### Evidence examined

- Issue #53 (no comments). Acceptance: every subtest still fails when the check before the tree build is removed; passes reliably under `-race`.
- `git diff origin/main...HEAD`, commit message.
- `fpgrowth.go` (`start`/OnStart, `m.Reporter`, `Mine`), `internal/fpg/fpg.go` `Miner.Mine`, `mine.go` (`mineItemSets`, `topLevel`, `buildCheckEvery`), `simple.go`/`complex.go`/`single.go`/`topdown.go`/`tree.go`, `arena.go` (`presize`, `treePresize`), `workspace.go`.
- No library code starts goroutines (only `cmd/`). `fpg.Workspace` arena reuse is used only by `internal/psp`.

### Commands run

Mutations reverted with `git checkout -- internal`; raw output in `review-design/mut-*.txt`.

| Run | Mutation | Bytes after cancel (Simple/Complex/Single/TopDown/rules) | Latency | Result |
| --- | --- | --- | --- | --- |
| base | none | 71,232 each, 12 objects | 31-81 µs | PASS 5/5 |
| base -race -count 3 | none | 71,232 each, 12 objects | 187-244 µs | PASS 15/15 |
| B | `ctx.Err()` check at the start of `Miner.Mine` removed | 192.9M / 257.0M / 128.9M / 285.4M / 128.3M | 2-44 ms | FAIL 5/5 |
| C | B + `presize` returns 16 | 9.57M / 13.76M / 9.57M / 8.90M / 8.99M | 1.9-2.8 ms | FAIL 5/5 |
| C -race | same as C | same bytes | 17-26 ms | FAIL 5/5 |
| D | C + `buildCheckEvery = 1<<62` | 662M / 962M / 662M / 661M / 661M | 130-590 ms | FAIL 5/5 |
| E | check intact; 3 allocation-free passes over the bag before it | 71,712 each | 294-528 ms | PASS 5/5 |
| F | B + E | 128-285M | 292-449 ms | FAIL 5/5 |

gofmt empty; vet, staticcheck ok; `go test ./...` ok; `go test -race .` ok (52.9 s); `go test -race -shuffle=on -run TestCancel .` ok.

### Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| D1 | MEDIUM | Confirmed | Allocation-free work after the cancellation (294-528 ms) is no longer detected; the old 30 ms limit caught it. Tree builds always allocate, so the core class is covered, but not "other substantial work". Not an acceptance failure. | Mutation E |
| D2 | LOW | Confirmed | Without a presized arena and with build-loop checks stopping after 4,096 transactions, the margin is only 2.1-3.3x (8.9-13.8 MB); depends on `buildCheckEvery`. Comment does not state it. | Mutations C |
| D3 | info | Confirmed | Measurement points correct; `ReadMemStats` outside the latency window; `TotalAlloc` cumulative; no other allocators in the window; values identical across plain, -race, -shuffle, repeats. | Base runs |
| D4 | info | Confirmed | Comment arithmetic correct (58.9x; 10.3x); `fpNode` is 16 B on 386 too. | `simple.go:12`, `arena.go` |

### Fixes requested

1. D1: (a) restore a loose latency check immune to scheduler noise (fail only if every one of 3 reruns exceeds ~30-50 ms), or (b) document that only allocations are bounded.
2. D2 (optional): state the no-presize margin or its dependence on `buildCheckEvery`.

### Performance impact

None (test only).

### Decision

**ACCEPT WITH CONDITIONS** (D1 Medium; rest Low/info). Both acceptance criteria of #53 met.
