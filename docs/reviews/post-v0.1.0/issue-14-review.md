# Review of PR #26 (issue #14): ctx.Err() when OnStart cancels a run with nothing to mine

- Issue: dimchansky/fpgrowth-go#14
- PR: https://github.com/dimchansky/fpgrowth-go/pull/26 (branch `fix/14-onstart-cancellation`)
- Head SHA reviewed: `846558591fd17629f16125c9fcc9b62b65fddb74` (confirmed with `git rev-parse HEAD` and `gh pr view --json headRefOid`)
- Base: `origin/main` = `aff7f28`
- Reviewer specialization: Go API correctness, context cancellation, error propagation, iterator/callback semantics
- Scope: `fpgrowth.go` (`mine`, `MineItemsets`, `Itemsets`, `CollectItemsets`), `rules.go` (`MineRules`, `Rules`, `CollectRules`, `RuleOptions.OnStart`), `options.go` (`Options.OnStart`), new tests in `fpgrowth_test.go`, docs (README, `doc.go`, `docs/api-design.md`, `docs/algorithm.md`, CHANGELOG, PROGRESS).

## Evidence examined

- Issue #14 body (scope items 1-5, acceptance criteria, required verification), `docs/audit/rc1-verification.md` row RC-2.
- `git diff origin/main...HEAD` (8 files, +251/-12); every return path of `mine` read in full.
- PR body (exit-path table and design rationale), checked against the code.

## Code analysis of `mine` exits

| Exit | Before PR | After PR | Assessment |
| --- | --- | --- | --- |
| `ctx.Err()` at entry | ctx.Err() | unchanged | ok |
| pass-1 scan failure | `sc.failure` (ctx-aware) | unchanged | ok |
| `ib.Len()==0 \|\| ib.Records()==0` | `start()`: nil after cancel in OnStart | `startEmpty()`: OnStart error, else ctx.Err() | fixed |
| `m.Data` -> `ErrNoItems` | `start()` | `startEmpty()` | fixed |
| `m.Data` -> bag abort / scan error | `cerr` / `sc.failure` | unchanged; `start()` not reached | ok |
| `start()` before mining | ctx.Err() or OnStart error | unchanged | ok; a cancel inside OnStart is caught by `m.Mine`'s entry check (`internal/fpg/fpg.go:189`) or the final `switch` |
| final `switch` | callback error before ctx; `err==nil && ctx.Err()!=nil` -> ctx.Err() | unchanged | ok |

Precedence is now the same everywhere: an error from OnStart or the callback is returned unchanged; otherwise a canceled ctx gives `ctx.Err()`. That matches what `main` already did on the mining path, so the behavior there does not change. `startEmpty` returns nil only when OnStart returned nil and ctx is not canceled, so an uncanceled empty run still succeeds (`TestEmptyDatabase`, `TestNoFrequentItems` and the probe below pass). On an empty path, ctx.Err() is now returned only when ctx was really canceled before the function returned. This matches the "canceled run returns ctx.Err()" contract. It cannot turn a successful, uncanceled run into an error.

Iterator adapters: an early `break` sets `stopped` and returns `errStop`, which is the callback error and wins over a concurrent cancel. Nothing is yielded after the break, and the adapters are unchanged. On the empty paths `fn` is never called, so the final `(zero, ctx.Err())` pair is yielded. The tests and my probe both confirm this.

## Commands run (results)

Machine load: `uptime` 8.03 / 9.42 / 7.85 at the start. During the stress run it rose to 27-34 (other sessions and my own stress run).

| Command | Result |
| --- | --- |
| `gofmt -l .` | no output |
| `go vet ./...` | ok |
| `staticcheck ./...` | ok |
| `go build ./...` | ok |
| `go test ./...` (1st run) | **FAIL**: `TestCancelInOnStart/frequent_items/MineRules`: `deadline: got context deadline exceeded, OnStart called 0 times, 0 results` (fpgrowth_test.go:1790) |
| `go test -count=1 ./...` (2nd run) | all ok |
| `go test -run 'TestCancel\|TestOnStart\|TestNoFrequentItems\|TestErrorPrecedence' -count=50 .` x3 | 0, 0, 1 failure lines |
| `go test -count=10 .` | ok |
| `go test -race -count=3 .` | ok, no data race |
| `go test -race -run 'TestCancelInOnStart\|TestErrorPrecedence' -count=50 .` (also `-cpu=1`) | ok |
| Stress, plain build: 8 processes x `-test.run TestCancelInOnStart -test.count=200` | 0 failures |
| Stress, race build: 24 processes x `-test.run TestCancelInOnStart -test.count=100` | **159 failures**, all `fpgrowth_test.go:1790: deadline: ... OnStart called 0 times` |
| Mutation: `fpgrowth.go` reverted to `origin/main` | 54 failure lines in 27 subtests: every case x entry point that uses an empty exit (empty database, empty transactions, empty selection, unreachable support: all 6 APIs; no frequent item: the 3 item-set APIs). Rules "no frequent item" and "frequent items" pass on base too (mining path, as the issue table shows). |
| Mutation: `startEmpty` lets ctx.Err() win over the OnStart error | `TestErrorPrecedence`: 27 failing subtests |
| Mutation: main path lets ctx.Err() win over the OnStart error | `TestErrorPrecedence` fails (e.g. `OnStart/frequent_items/Rules`) |
| Independent probe (temporary file, removed): 5 DBs (empty, empty tx, no frequent item, frequent, random 3000x12) x 4 targets x 5 algorithms x nil/non-nil fn x supports {1,2,100}; MineItemsets + MineRules (conf 0.5) + `Itemsets` with immediate break; checked `err == ctx.Err()`, `errors.Is(Canceled)`, Stats/Transactions/FrequentItems equal to the uncanceled run; OnStart returning a wrapped DeadlineExceeded keeps its identity; cancel in callback after the last check gives `context.Canceled` | all passed, also with `-race`. The only failure was a bug in my probe: its last check left out `MinSupport`. |

Reference difftests were not run: the change does not affect results or the C-parity paths (uncanceled runs are unchanged, and only cancel or empty exits are affected).

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| F1 | HIGH | Confirmed | `TestCancelInOnStart` is flaky. Its "deadline" subtest gives the whole pre-OnStart pipeline a 1 ms budget (`context.WithTimeout(..., time.Millisecond)`, created before `ep.run`: 2 passes, bag, tree). On a loaded machine the deadline expires before OnStart. `start()` then correctly skips OnStart and the test reports `OnStart called 0 times`. This breaks the issue's "Required verification" (the new tests must not be flaky). It failed on a plain first `go test ./...` here. | fpgrowth_test.go:1786-1791; 1 failure in a plain `go test ./...`, 1 in 150 `-count=50` iterations, 159 failures in 86,400 subtest runs under the race-build stress |
| F2 | LOW | Confirmed | The package doc `doc.go:47-49` still says "a canceled run always returns ctx.Err(), also if mining finished after the last check" and does not mention the precedence exception (OnStart or callback error) that this PR now documents in the GoDoc, README and api-design. Scope item 4 asks for the precedence to be documented, so the docs now disagree. | doc.go:47-49 vs fpgrowth.go:131-135 |
| F3 | LOW | Confirmed | `docs/api-design.md:148` limits the new case to "when `OnStart` cancels it and no item is frequent". The fix also covers the empty database, empty transactions and an empty item selection, which CHANGELOG and README list. | docs/api-design.md:147-150 |
| F4 | Info | Confirmed | For the Collect* entry points, the test's `n != 0` check can never fail, because the adapters return nil results on error. The `sets != nil` guard covers this, so it is not a defect. | fpgrowth_test.go entryPoints |

There are no correctness defects in the library code. All acceptance criteria that concern behavior hold. The "fails on aff7f28" criterion holds for every path that was broken before; the rules no-frequent-item case and the frequent control rows were already correct on base, as the issue's table shows.

## Fixes requested

1. F1 (required): make the deadline subtest deterministic. Options:
   - Run that part inside `testing/synctest.Test` (stdlib, Go >= 1.25; the module is at 1.26). In the bubble the 1 ms timer fires only when every goroutine is durably blocked, which happens only once OnStart blocks on `<-ctx.Done()`. Keep `t.Parallel`/`t.Run` outside the bubble.
   - Or use a context whose `Done`/`Err` switch to `context.DeadlineExceeded` only when OnStart triggers it.

   Re-verify with a parallel stress run (for example 24 race-build processes x `-test.count=100`) and zero failures, as well as `-count=50`.
2. F2: add the precedence sentence to the package doc (`doc.go`), or make it point to MineItemsets for the exception.
3. F3: widen the api-design wording to "when `OnStart` cancels it and nothing is left to mine".

## Performance impact

None measurable. The only added work is one `ctx.Err()` call on the two exits that skip mining. The mining path is byte-for-byte unchanged, so no benchmark was needed.

## Decision

**REJECT.** There is one HIGH finding (F1, a confirmed flaky regression test that breaks the issue's required verification). The library fix itself is correct and well covered. Once F1 is fixed (and preferably F2/F3), this should be ACCEPT.

## Re-review of f7d93ae

- Head SHA reviewed: `f7d93ae4b3d887a9ec6baa0a02d74505b79cf195`. It is one commit, "Make the OnStart deadline test deterministic", on top of `8465585`.
- I re-checked the original issue #14 requirements against the new head. Only this commit changed since the first review.
- `git diff --stat 8465585..f7d93ae`: `doc.go` (+3/-1), `docs/api-design.md` (+5/-3), `fpgrowth_test.go` (+18/-12). No library code changed, and `fpgrowth.go`, `rules.go` and `options.go` are the same as in the first review. My earlier analysis of the `mine` exits and of precedence therefore still holds.

### Delta examined

- **F1:** the deadline and expired subtests now run inside `synctest.Test(t, ...)`. Both `ctx` (`WithTimeout`, 1 ms) and `ep.run` are inside the bubble. OnStart blocks on `<-ctx.Done()`. The bubble's fake clock moves only while every goroutine in it is durably blocked, so the deadline can pass only while OnStart waits. `t.Run` and `t.Parallel` are called outside the bubble, as synctest requires. `testing/synctest` is imported in the regular import block, and there are no build tags.
- **F2:** `doc.go` now names the OnStart case and the precedence exception, in the same words as the GoDoc and the README.
- **F3:** `docs/api-design.md` now lists all four nothing-to-mine cases: empty database, only empty transactions, no frequent item, and an item selection that leaves no item.
- **F4:** left as it was (Info).

### Commands run (results)

Load at the start was 9.02 / 14.96 / 13.42, and 15.42 / 15.83 / 13.77 during the stress run. `go.mod`: `go 1.26.0`, `toolchain go1.27.2`.

| Command | Result |
| --- | --- |
| `go test -run 'TestCancelInOnStart\|TestErrorPrecedence' -count=1 .` (go1.27.2) | ok |
| `GOTOOLCHAIN=go1.26.9 go version` / same test / `go vet .` | go1.26.9 darwin/arm64; ok; vet ok |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` | clean |
| Stress: race build, 24 processes x `-test.run 'TestCancelInOnStart\|TestErrorPrecedence' -test.count=100` | 24 of 24 exit 0; 0 failure, race or panic lines |
| `go test -run 'TestCancel\|TestOnStart\|TestNoFrequentItems\|TestErrorPrecedence' -count=50 .` | ok |
| `go test -count=1 ./...` x4 | all ok (0 FAIL in each run) |
| `go test -race -count=1 ./...` | all ok |
| Mutation: `fpgrowth.go` reverted to `origin/main` | `TestCancelInOnStart` fails: 27 "canceled" and 27 "deadline" failure lines. The deterministic deadline subtest still detects the regression. File restored, worktree clean. |

The "undefined: synctest" report from the editor does not reproduce. The package builds and the tests pass with go1.26.9 and go1.27.2, the import is present, and there are no build tags. `synctest.Test` has been in the standard library since Go 1.25, so the editor message most likely came from a gopls or toolchain older than 1.25.

### Findings after re-review

| ID | Severity | Status | Summary |
| --- | --- | --- | --- |
| F1 | HIGH | Resolved | Deterministic with synctest: 0 failures in 24x100 race stress runs, and the reverted fix still fails the deadline subtest |
| F2 | LOW | Resolved | `doc.go` states the precedence |
| F3 | LOW | Resolved | `api-design.md` lists all cases |
| F4 | Info | Open (accepted) | For the Collect* entry points the test's `n != 0` check can never fail; harmless |

All acceptance criteria of issue #14 are met and verified by the commands above.

### Decision

**ACCEPT**. No Critical, High, Medium or Low findings remain; only F4 (Info) is open.
