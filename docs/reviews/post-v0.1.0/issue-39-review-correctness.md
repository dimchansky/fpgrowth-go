# Critic review (correctness) — PR #46, issue #39

- PR: https://github.com/dimchansky/fpgrowth-go/pull/46 (branch `perf/39-fpgpsp-reuse`)
- Head reviewed: 21b7522; the night sweep ran on 2aee120 (21b7522 with main merged)
- Decision: ACCEPT (Info items only)
- Recorded by the orchestrator from the critic's final message and its activity log.

## Night sweep (2026-10-27, head 2aee120)

- TestFpgpsp, large shapes (10-39 items, up to 500 rows), seed 4647, 4,000 cases, 10 min per case: 3,955 byte-equal, 0 tolerated, 0 mismatches, 45 timeouts (40 on both sides, 3 Go only, 2 C only).
- The 5 one-sided timeouts, rerun alone: 5 byte-equal.
- A rerun of 10 of the 40 two-sided timeouts was cut short by a reboot of the machine and was not repeated. Even the C oracle cannot finish those cases in 10 minutes, so they add little evidence.
- `go test -race ./internal/fpg`, unfiltered: ok.

## Critic's report (day review)

PR #46 (#39) correctness review, head 21b7522e44d5388dd088722a5ca72c0f97b1485a

**Verdict: ACCEPT.** I found nothing above Info. The output is byte-identical to the C oracle on every sweep case that ran to completion.

**Checks run** (clone: ~/.cache/fpgrowth-cycle/23/review-correctness/repo)
- `gofmt -l .` printed nothing.
- `go vet` passed on the touched packages, natively and with linux/386. `staticcheck` passed on internal/fpg, internal/psp, internal/tract and cmd/fpgpsp.
- `go test -count=1 ./...` passed in every package.
- `-race` passed on internal/psp, internal/tract and cmd/fpgpsp, and on internal/fpg with `-run Workspace`.
- Differential `TestFpgpsp` against fpgpsp-fixed, which already contains p01-p11 (the PR does not touch reference/):
  - (a) Seed 4646, 6,000 cases, default generator: ok, 54 s.
  - (c) Seed 4648, 30,000 cases, default generator: ok, 275 s.
  - (b) Seed 4647, 4,000 cases, larger shapes (10-39 items, up to 500 rows; generator changed in the copy scratch/pr46): hit the 17-minute timeout with no mismatch reported. Inconclusive.
  - (b2) Seed 4649, 150 cases, same larger shapes, `-v`: 148 byte-equal, 0 mismatches. The other 2 hit the per-case run timeout: in both the C oracle timed out (and Go too in one), on `-gr -tm -i -u` runs with large shapes. This is a limit of my enlarged generator, not a difference.

**Code reading**
- Workspace reuse is sound:
  - The 16-items machine `reset` brings back every field `newMachine` sets, and `dir` is only ever +1 or -1.
  - The node arenas and `bagBuilder.reset` rebuild every node from a full literal and clear the index map.
  - The per-depth header buffers were already reused within a run before this PR, so their callers already overwrite them.
  - Only one machine is alive per run.
  - `takeBack` runs in a defer, so it also runs after an error.
- `DenseBase` matches `ItemBase` in Count, Recode and BuildBag: first-seen ranking, duplicates counted once, record size used for the size-weighted frequency, smin at least 1, sort then dedupe, and the zmin filter. `finish` with a nil abort matches the old psp path, whose ItemBase had no abort either.
- The Fenwick search returns the same entry as the linear search, including zero frequencies, x >= z, and the clamp to m-1. Its updates are correct.
- `firstUnder` examines items in the same order as the cyclic search patched by P4. The bit set of under-represented items is kept consistent.
- The sort key of the first two items orders like `slices.Compare`, with no overflow for int32 codes. Ties among equal transactions are harmless: they have the same content and length, and no code reads `cap`.
- `cmd/fpgpsp` creates one workspace per worker goroutine.

**Mutations** (unit tests only, helper script ~/.cache/fpgrowth-cycle/23/review-correctness/scripts/mut46.sh)
- Killed (10): the reset of the machine's `wgts`, `imap` and `lists`; `clear(bb.index)`; `clear(headFrq)`; the wrap-around half of `firstUnder`; the `under` bit clear; the duplicate marker in `DenseBase.Count`; the insertion-sort bound; the Fenwick update sign; the sort-key shift.
- Survived (4), all equivalent mutants:
  - Not resetting `tor` and `supps` in the machine reset. `machine.mine` always zeroes `ttw` and `tor`, even on error, and `count` rewrites `supps`.
  - Not truncating the complex-variant node arena in `takeBack`. `arena()` truncates it again at the start of the next run.
  - My first `SortItems` mutation was invalid (it reversed the slice, which still ends up sorted).

**Findings**
- I1 (Info): no test covers the reset of `tor` and `supps` in `machine.reset`. It is redundant on every current path, including aborts. Optional: a comment saying so.
- I2 (Info): large-shape differential coverage is still thin: 148 cases ran to completion, all equal.

**Deferred** (heavy CPU, not run after 10:00)
- A night sweep of `TestFpgpsp` with the large-shape generator in scratch/pr46, with a longer per-case timeout or a cap on transactions for `-gr -tm -i -u`, e.g. 4,000 cases.
- Full `go test -race ./internal/fpg` without the `-run` filter.
- No benchmarks were rerun; the PR's performance numbers are not independently verified.

**Housekeeping**
- ACTIVITY.md is updated.
- The clone is clean at 21b7522. I moved a pre-existing untracked `reference/build.log` from 2026-10-23 to scratch/repo-reference-build.log.2026-10-23.
- I removed my mutation copy and kept scratch/pr46 for the deferred sweep.
- Logs are sweep-pr46-{a,b,b2,c}.log in ~/.cache/fpgrowth-cycle/23/review-correctness/.
