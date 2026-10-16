# Progress log

Durable project state so work can resume after a context reset. Keep this
short: current status, next steps, open problems. Details belong in the
linked documents.

## Current status

- **Stage 0 (preparation)** — in progress.
  - Environment: macOS arm64, Go 1.27.2 toolchain (1.27.1 installed
    system-wide; `toolchain go1.27.2` auto-downloads), Apple clang 21,
    Python 3.14, Node 23, gh, staticcheck, golangci-lint, benchstat.
    Missing: govulncheck, goreleaser, actionlint (install when needed).
  - Go skills: `samber/cc-skills-golang` 2.0.1 already installed as a
    Claude Code plugin (`cc-skills-golang:*` skills).
  - code-review-graph 2.3.9 installed via pipx; repo graph built over the
    upstream C sources (`code-review-graph build`). Local config files it
    created (`.mcp.json`, `.claude/`) are gitignored.
  - Upstream FP-Growth 6.21 vendored (commit "Import pristine upstream ...").
  - Upstream audits running: fpgrowth core, tract/fim16/clomax/patspec,
    report, rules/evaluation, CLI/IO, reference build + sanitizers.

## PAUSED (2026-10-16, at the user's request)

Stage 0 audits are complete (`docs/upstream/*.md`); `docs/architecture.md`,
`docs/feature-matrix.md` and `docs/differences.md` are drafted.

Stage 1 wave 1 was stopped mid-work. Partial, **uncommitted and unreviewed**
files are in the working tree:
- `internal/tract/` (item base, recode sort ports, bag) — C harness not done.
- `internal/tabread/`, `internal/cconv/` — tokenizer tests passed; reader
  tests/fuzz pending.
- `internal/ruleval/` — C harness started; Go code just begun.
- `internal/report/` — in progress (simulator/property tests pending).
- `internal/oracle/` — unit tests passed; C cross-check pending.
- `reference/build.sh`, `reference/README.md` — variant build being
  rewritten (add `-ffp-contract=off` to all variants, plus a `native`
  variant); `docs/upstream/reference-build.md` not written yet.
- `reference/harness/*` — partial C harnesses.

## Next steps (on resume)

1. Resume or restart the six wave-1 tasks above from their partial files;
   review each deliverable, then commit package by package.
2. Wave 2: `internal/fpg` (variants + fim16, fixes core B1/B3/B5),
   `internal/istree` (rules path; decide rules B1/B2), `internal/isrout`.
3. Wave 3: CLI, golden fixtures, differential tests, Stage 1 review.

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
