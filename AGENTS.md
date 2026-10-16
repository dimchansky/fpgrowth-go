# Agent and contributor guide

This repository is a pure Go port and further development of Christian
Borgelt's FP-Growth C implementation (version 6.21). Read this file before
changing anything.

## Ground rules

- All code, comments, docs, commit messages and CLI text are in English.
- The published module (`github.com/dimchansky/fpgrowth-go`) is **pure Go**:
  no cgo, no C files, no native libraries, no non-stdlib dependencies unless
  explicitly approved in `docs/decisions.md`.
- `reference/` is a **separate nested Go module** with development-only
  tooling: pristine upstream C sources (`reference/upstream/`, never edit
  them), reference build scripts, and differential tests. `go build ./...`
  and `go test ./...` at the repository root must work without a C compiler.
- Correctness first. Every behavioral difference from upstream must be
  intentional, documented in `docs/differences.md`, and covered by a test.
- Do not invent results. Never claim parity, coverage or performance numbers
  without a command whose output shows it.
- Commit messages: imperative subject, wrapped body explaining why. Do not
  add AI attribution trailers.

## Layout

| Path | Purpose |
| --- | --- |
| `*.go` (root, package `fpgrowth`) | Public library API |
| `internal/` | Implementation packages (port of upstream modules) |
| `cmd/fpgrowth/` | Command-line program compatible with upstream `fpgrowth` |
| `testdata/` | Golden fixtures produced by the C reference (committed) |
| `docs/` | Design, upstream audit notes, feature matrix, reviews, progress |
| `reference/` | Nested module: upstream C, reference build, differential tests |

## Commands

```bash
go build ./...                       # build library and CLI
go test ./...                        # unit, golden, property tests (no C needed)
go test -race ./...                  # race detector
go vet ./... && staticcheck ./...    # static analysis
(cd reference && ./build.sh)         # build the C reference executable(s)
(cd reference && go test ./...)      # differential tests against the C build
```

## Where things are documented

- `docs/PROGRESS.md` — current stage, status, next steps (keep it current).
- `docs/feature-matrix.md` — upstream feature → Go implementation → tests.
- `docs/upstream/*.md` — audit notes on the upstream C modules.
- `docs/decisions.md` — architecture decisions and their rationale.
- `docs/differences.md` — every intentional deviation from upstream.
- `docs/reviews/` — critical-review reports for each stage.

## Code intelligence

A code-review-graph index covers both the upstream C sources and the Go
code (`code-review-graph build`, then `code-review-graph query
callees_of <function>` / `file_summary <path>` / `impact`, etc.). Use it to
narrow scope, then read the source; the source always wins.
