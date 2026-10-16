# Progress log

Durable project state so work can resume after a context reset. Keep this
short: current status, next steps, open problems. Details belong in the
linked documents.

## Current status (2026-10-16)

- **Stage 0 (preparation): done.** Toolchain Go 1.27.2 (`toolchain`
  directive; language level 1.26), clang, staticcheck 2026.2.2,
  `samber/cc-skills-golang` skills, code-review-graph 2.3.9. Upstream
  FP-Growth 6.21 vendored and audited (`docs/upstream/*.md`).
- **Stage 1 (faithful port): accepted** by an independent critic
  (`docs/reviews/stage1-review-1.md`, no Critical/High findings).
  Packages: `internal/{tract,tabread,cconv,report,ruleval,istree,fpg,isrout,oracle}`,
  `cmd/fpgrowth`; C reference build and harnesses in `reference/`;
  randomized differential suite `reference/difftest` (0 failures in
  30 000+ cases). Medium/Low review findings: docs fixed (F4, F6–F8, F10);
  F1/F2/F3/F9 test-suite work in progress.
- **Stage 2 (idiomatic API, sources, streaming): accepted with conditions**
  after three review iterations (`docs/reviews/stage2-review-{1,2,3}.md`):
  iteration 1 rejected (H1 cancellation in item-set tree), iteration 2
  rejected (N1 cancellation during perfect-extension enumeration),
  iteration 3 accepted with Medium/Low conditions N7–N12 (cancellation
  during top-level tree build and top-down unwinding, second SIGINT, early
  returns, difftest classification gaps, this log). Public API: root
  package (`docs/api-design.md`); docs: README, `docs/algorithm.md`,
  `docs/cli.md`.
- **Stage 3 (benchmarks, profiling, optimization):** next, including the
  M5 investigation below.

## Next steps

1. Land the Stage 2 iteration-3 fixes (N7–N11), record them in
   `docs/reviews/stage2-review-3.md`.
2. Stage 3: benchmark suite (sparse/dense, item universes, lengths,
   weights, thresholds, targets, rules, variants, file vs memory sources,
   streaming), C comparison (time, CPU, RSS), profiling, optimizations with
   benchstat evidence, M5 investigation, benchmark report; then the Stage 3
   critic review.
3. Stage 4: CI/CD, release automation, OSS files; Stage 4 review; final
   report.

## Open problems

- **M5 performance investigation — scheduled for Stage 3 (user request,
  2026-10-16).** Closed/maximal with `-n` currently searches beyond the size
  limit to guarantee exact closure (commit 9397bab). Investigate whether full
  exploration can be replaced by a correct, efficient one-item extension
  check at the `zmax` boundary:
  1. Re-analyze the original failing randomized case (oracle seed 678 in
     `internal/fpg` tests): why boundary perfect extensions were not
     enough (repository rejected a set via a stored same-support superset
     whose subtree was never fully searched).
  2. Account for FP-growth traversal order (ascending/descending item loops,
     post-order reporting), perfect-extension pruning, fim16 and head-union-
     tail pruning, and each variant's projection rules.
  3. Keep correctness proven by differential tests against
     `internal/oracle` (large randomized sweeps, all variants, closed and
     maximal, many `-n` values incl. small ones).
  4. Benchmark both approaches (full exploration vs. boundary check), with
     emphasis on dense datasets and small `-n`; keep the faster one only if
     it is exactly correct. Record results in the Stage 3 report.

  Additional requirements (user, 2026-10-16):
  - First **minimize** the seed-678 case (delta-debug transactions/items/
    weights) and establish the exact root cause: expected vs. actual output
    and the **first violated invariant** in the traversal/repository state.
  - The earlier explanation ("repository rejects a set because a stored
    same-support superset exists whose subtree was not fully searched") is
    insufficient: if that superset is genuine, the rejected set really is
    not closed. Determine precisely what went wrong — e.g. a set wrongly
    *accepted* because its proof superset was never stored, a stored
    superset with a wrong (partial-closure) support, or a set rejected by a
    non-genuine superset — and state it with evidence.
  - The one-item extension check must consider **all eligible items of the
    original database** (after selection/recoding), not only items in the
    current conditional FP-tree or traversal tail.
  - Distinguish a **mathematically complete** extension check (closedness/
    maximality w.r.t. every eligible item) from an **implementation-
    dependent local** check (relying on traversal order, tail, repository
    contents). The correctness argument and regression tests must cover
    both: tests that would fail for a merely local check (items outside
    the conditional tree/tail), plus the minimized seed-678 case.
