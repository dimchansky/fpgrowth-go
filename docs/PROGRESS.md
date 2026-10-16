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

## Next steps

1. Collect audits → `docs/feature-matrix.md`, architecture plan.
2. Stage 1: faithful port, component by component, with tests.

## Open problems

- None yet.
