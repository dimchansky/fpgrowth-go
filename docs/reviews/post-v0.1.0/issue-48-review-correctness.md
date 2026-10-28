# Critic review (correctness) — PR #50, issue #48

- PR: https://github.com/dimchansky/fpgrowth-go/pull/50 (branch `perf/48-prep-pipeline`)
- Head reviewed: a7e03c2
- Decision: ACCEPT (three Low findings, one Info)
- Recorded by the orchestrator from the critic's final message.

## Resolution of findings

- F1: fixed in 76b27de. `TestItemBase_Retain_budget` has rows with the
  limit equal to the bytes used and one byte less; the `>=` mutant
  fails.
- F2: fixed in 16a1f56. A test asserts that `fpgpsp -gs` does not
  retain; forcing retention on fails it.
- F3: hypothesis not confirmed (the live heap in `finish` was the same
  with and without retention: the compiler already treats the records
  as dead). The release was made explicit anyway in ba14973. The peak
  resident memory the critic suspected was real, but its cause was the
  heap goal, not reachability; the performance review found it (P2)
  and ede1046 fixed it.
- F4: fixed in 76b27de. The random budgets of
  `TestItemBase_Retain_matchesScan` now also fall inside the count.

## Critic's report

**Decision: ACCEPT.** I found no Critical, High or Medium issues. There are three Low findings and one Info. None of them blocks the merge.


### What I verified by reading the code

**Tokenizer: uncopied fields and buffer refills**
- Before `fill` overwrites the buffer, it copies any field that still points into it. This also covers the case where the step that skips blanks after a field separator triggers a refill.
- `read` resets the field to empty before the general path, so every comment, blank, end-of-input and error return gives an empty field, as before.
- No caller keeps a field across reads. `Reader.next` copies into its arena. `ReadSelection`, `ReadAppearances` and the syntax error convert to strings at once.
- The test oracle `tokenizer_ref_test.go` is the pre-change `tokenizer.go`; a diff shows only the shared constants differ.

**Fast path (`plainField`)**
- I checked it case by case against the general tokenizer: a record separator, a blank field separator followed by a plain byte, a non-blank field separator that is also a record separator, and a plain field separator.
- It shares `next()` with the general path, so record and field numbers, and therefore every error position, are unchanged.

**Retention (keeping the counted records)**
- Every item gets a recode entry; removed and none-appearance items map to -1.
- The block walk handles empty records, records that fill a block exactly, and records longer than a block.
- The memory accounting covers the blocks, the growth of the record-length array, and the late weight array.
- Over the budget, the records are dropped and the second pass reads the file as before.
- `BuildBag` releases the records; a second call reads the file.
- Cancellation is checked at the same record counts as the file path, and both paths report "interrupted".

**Other paths**
- stdin is still spooled to a temporary file, so reading it twice over the budget works as before.
- `fpgpsp -gs` correctly skips retention.
- Within the budget, the only things lost are the documented "input changed while reading" check and the chance of a second-pass read error. No other message or exit code depends on the second pass.

### Commands and results
- `gofmt -l`, `go vet ./...`, `staticcheck ./...`, `go test ./...`: all pass.
- `go test -race` on tabread, tract and cmd/...: pass.
- Three ad-hoc tests, written in the clone and deleted with it, all pass:
  - **Error mid-data:** 3000 random inputs, with buffer sizes 1-64 KiB and a source that returns data, then a non-EOF error. Compared with the reference tokenizer.
  - **Small buffers:** 3000 inputs, half weighted. Reader output at buffer sizes 1-7 compared with 64 KiB.
  - **Budget edge (both CLIs):** all 224 golden cases with a budget met exactly by the first record and exceeded in the middle of the count. stdout, stderr and exit status are identical to retention off.
- `FuzzTokenizerReference`, 60 s at GOMAXPROCS=2: 65,568 executions, pass.
- Mutation runs: 18 mutants, 15 killed by the PR's own tests (including removing the copy in `fill`, the blank-separator check, the block walk, sorting, the zero-weight backfill, the zmin filter, release, recode -1 and the abort check). Of the 3 survivors, one is equivalent (the next read skips the blank separator anyway); the other two are F1 and F2.
- Differential runs, one process at a time under the RSS watchdog with GOMAXPROCS=2:
  - **fpgrowth:** 300 random inputs covering separators, CRLF, weights, stdin, no final newline and 70-140 KB fields that span buffer refills. PR and main were identical in all 300. Against C, 4 of 282 differ; all are documented deviations (T7 negative weights ×3, M4 maximal sets ×1), and main differs the same way.
  - **fpgpsp:** 200 inputs with `-ge`, `-gi`, `-gs` and `-gr`. PR and main were identical in all 200 (90 successful runs, 110 identical errors).

### Findings

| ID | Severity | Status | Summary | Evidence |
|---|---|---|---|---|
| F1 | LOW | confirmed (test gap) | No unit test pins the exact-budget boundary: changing `used > limit` to `>=` survives. It only changes which path runs, and both give the same output. | mutations.txt R2; my CLI check exactly at the budget passes |
| F2 | LOW | confirmed (test gap) | The `-gs` opt-out is not tested; forcing retention on for `-gs` survives. The cost would be up to 128 MiB of unused memory, not wrong output. | mutations.txt C1; `cmd/fpgpsp/run.go:124` |
| F3 | LOW | hypothesis, not measured | The retained records probably stay reachable until the bag is finished, because `BuildBag` passes `&ret` (an address-taken local). Peak memory would then be the records plus the bag; dropping the slices before `finish` could lower it. The PR reports fpgpsp `-ge` B×90 at 180 MB, against 120 MB on main and 124 MB in C. | `internal/tract/retain.go` buildRetained; `bag.go:94-97` |
| F4 | INFO | confirmed | The random budgets in `TestItemBase_Retain_matchesScan` almost never drop the records after the first block. Mid-count release is covered only by the "+weights" row of `TestItemBase_Retain_budget`, plus my CLI check. | `retain_test.go:66-72` |

**Optional fixes:** add a budget test row with the limit equal to the bytes used (F1); assert that `-gs` does not retain (F2); drop the retained slices before `finish` (F3).

**Deferred to night:** nothing required. Optional: a longer `FuzzTokenizerReference` run of 10 minutes or more.

**Cleanup:** the clone, the main worktree, all binaries and the generated inputs are deleted; about 40 KB of logs and scripts remain. None of my processes is still running, and 106 GiB of disk is free.
