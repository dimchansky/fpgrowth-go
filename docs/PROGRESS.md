# Progress log

Durable project state so work can resume after a context reset. Keep this
short: current status, next steps, open problems. Details belong in the
linked documents.

## Current status (2026-10-17)

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
- **Stage 3 (benchmarks, profiling, optimization): accepted and closed**
  (`docs/reviews/stage3-review-1.md`). Benchmark suite and Go-vs-C runner
  (`reference/bench`), report `docs/benchmarks.md` (Go/C geometric mean
  1.72 → 1.60), M5 resolved with a complete extension check
  (`docs/m5-investigation.md`).
- **Stage 4 (CI/CD, releases, OSS files): accepted and closed**
  (`docs/reviews/stage4-review-{1,2}.md`): iteration 1 rejected (C1
  uncommitted golden fixtures, C2 Windows-reserved file name breaking the
  module zip; fixed in `e70d1ce`), iteration 2 accepted with conditions
  (N1 release tag selection etc.; fixed). CI, release, vulncheck
  workflows; `reference/modcheck` module-zip and tag gate.
- **All four stages closed.** Remaining work needs the GitHub repository
  (owner actions below).

## Next steps (owner actions; cannot be done locally)

1. Create the public repository `dimchansky/fpgrowth-go`, add the remote,
   push `main`, confirm all CI jobs pass (especially Windows).
2. Create the `release` environment (required reviewer, tag rule
   `v*.*.*`); rulesets protecting `v*` tags and `main`; read-only default
   `GITHUB_TOKEN`; private vulnerability reporting and Dependabot alerts.
3. Before the first tag: rename `## [Unreleased]` in CHANGELOG to
   `## [0.1.0] - <date>`, run the pre-tag checklist in
   `docs/releasing.md`, push an annotated tag, approve the deployment,
   verify the attestation and `go list -m ...@v0.1.0` via the proxy.

## Open problems

- None blocking. (The M5 notes below are kept for history; resolved in
  Stage 3.)

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
