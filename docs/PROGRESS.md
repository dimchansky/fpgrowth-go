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
- **Stage 2 (idiomatic API, sources, streaming): accepted and closed** (conditions fixed)
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

1. Stage 3: benchmark suite (sparse/dense, item universes, lengths,
   weights, thresholds, targets, rules, variants, file vs memory sources,
   streaming), C comparison (time, CPU, RSS), profiling, optimizations with
   benchstat evidence, M5 investigation, benchmark report; then the Stage 3
   critic review.
2. Stage 4: CI/CD, release automation, OSS files; Stage 4 review; final
   report.

## Open problems

- **M5 investigation — done (Stage 3, awaiting the Stage 3 review).**
  Report: `docs/m5-investigation.md`. The seed-678 case was minimized to
  4 transactions; root cause: a set wrongly *accepted* because its
  proving supersets were never stored (beyond zmax+1, or in subtrees
  pruned by genuine stored supersets whose own subtrees were cut);
  boundary perfect extensions cannot fix it. Closed/maximal with `-n` now
  use a size-limited search plus a complete one-item extension check
  against all items (`internal/fpg/extcheck.go`); full exploration with a
  limit is removed. Oracle sweeps: 220,000 closed/maximal comparisons,
  0 differences; critic case 34 s / 673 MiB → 21 ms / 14 MiB (C 236 ms).
  Sparse data with small `-n` pays up to 1.7× for the check (follow-up:
  incremental covers).
