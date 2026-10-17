# Contributing to fpgrowth-go

Thank you for helping. This project is a pure Go port and further
development of Christian Borgelt's FP-Growth 6.21 C implementation; its
first priority is correctness relative to that reference. Please read
[AGENTS.md](AGENTS.md) (the contributor guide for people and coding agents)
before changing code.

By participating you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
Report security issues privately as described in [SECURITY.md](SECURITY.md),
not in public issues.

## Setup

Requirements:

- Go 1.27.2 for development (the `toolchain` directive in `go.mod` selects
  it automatically with the default `GOTOOLCHAIN=auto`). The module itself
  supports Go 1.26 and later (`docs/decisions.md`, D3).
- For the C reference and the differential tests only: a C99 compiler
  (`cc`, clang or gcc), GNU make, `/bin/bash`, `sha256sum` or `shasum`
  (details in [reference/README.md](reference/README.md)). The root
  module never needs a C compiler.
- Optional tools, at the versions CI uses (`.github/workflows/ci.yml`):

  ```bash
  go install honnef.co/go/tools/cmd/staticcheck@v0.8.2   # staticcheck 2026.2.2
  go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
  go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
  go install github.com/goreleaser/goreleaser/v2@v2.18.3
  ```

```bash
git clone https://github.com/dimchansky/fpgrowth-go.git
cd fpgrowth-go
go build ./... && go test ./...
```

## Commands

The checks CI runs, in the order to run them locally:

```bash
gofmt -l .                                   # must print nothing
go vet ./... && (cd reference && go vet ./...)
staticcheck ./... && (cd reference && staticcheck ./...)
go mod tidy -diff && (cd reference && go mod tidy -diff)
go build ./...
go test -shuffle=on ./...                    # unit, golden, property, example tests
go test -race ./...
govulncheck ./... && (cd reference && govulncheck ./...)

# fuzz smoke test: one target per run, for example
go test -run '^$' -fuzz '^FuzzReader$' -fuzztime 20s ./internal/tabread

# cross-compilation, including 32-bit targets (vet type-checks the tests)
GOOS=windows GOARCH=386 go vet ./...
GOARCH=386 go test ./...                     # on linux/amd64

# C reference and differential tests (needs a C compiler and GNU make)
(cd reference && ./build.sh release)         # or ./build.sh for all variants
(cd reference && go test ./...)              # 10,000 small + 300 large cases
(cd reference && ./difftest.sh 50000)        # larger sweep with a summary file

# release configuration
actionlint
goreleaser check
goreleaser release --snapshot --clean --skip=publish   # into dist/ (gitignored)
```

Golden files under `testdata/` directories are produced by the C
reference through the generators in `reference/harness/*/gen.sh` (each
directory has a README). Regenerate them only with the reference build
and review the diff; never edit golden output by hand.

## Coding rules

From [AGENTS.md](AGENTS.md):

- All code, comments, docs, commit messages and CLI text are in English.
- The published module is pure Go: no cgo, no C files, no non-stdlib
  dependencies unless approved in [docs/decisions.md](docs/decisions.md).
- `reference/` is a separate nested module for development tooling only.
  Never edit `reference/upstream/`. `go build ./...` and `go test ./...`
  at the root must work without a C compiler.
- Correctness first. Every behavioral difference from upstream must be
  intentional, documented in [docs/differences.md](docs/differences.md),
  and covered by a test.
- Do not invent results: claims about parity, coverage or performance
  need a command whose output shows them.
- The command-line program stays compatible with upstream `fpgrowth`
  (options, formats, messages, exit codes). Keep `docs/cli.md` and
  `docs/feature-matrix.md` current when behavior or coverage changes.
- New exported API needs doc comments and, where useful, an `Example`
  test. Record design decisions in `docs/decisions.md`.
- Tests must pass on Linux, macOS and Windows and on 32-bit platforms
  (`int` is 32 bits on 386 and arm): use `math.MaxInt` and friends
  instead of literals such as `1 << 63`, `filepath.Join` for paths, and
  skip signal tests on Windows.

## Commits and pull requests

- One logical change per commit. Subject in the imperative mood ("Fix
  maximal sets with -x"), at most about 72 characters; a wrapped body
  explains why. No AI attribution trailers.
- A pull request describes what changed, why, and how it was tested, and
  references the deviation or decision ids it touches. Fill in the pull
  request template.
- Add a line to the `Unreleased` section of [CHANGELOG.md](CHANGELOG.md)
  for every user-visible change.
- CI must pass; the differential-test job must pass for any change to
  mining, reporting, evaluation, input or the CLI.

## Upstream sync policy

The behavioral reference is the upstream version vendored in
`reference/upstream/` (currently 6.21, see
[reference/upstream/PROVENANCE.md](reference/upstream/PROVENANCE.md)).
Upstream publishes source archives at an unversioned URL, so adopting a
new upstream version is a deliberate, reviewed change:

1. **Check.** Compare the version on <https://borgelt.net/fpgrowth.html>
   and the `VERSION` string in the downloaded archive with
   `PROVENANCE.md`. Record the check date even if nothing changed.
2. **Re-vendor.** Extract the new archive verbatim into
   `reference/upstream/fpgrowth-<version>/` (no edits), regenerate
   `reference/upstream/SHA256SUMS` (command in
   [reference/README.md](reference/README.md)), and update
   `PROVENANCE.md`: version, download URL, date, HTTP `Last-Modified`,
   archive size and SHA-256, license statement. Update `srcname` in
   `reference/build.sh` and paths in the harnesses and docs.
3. **Re-audit.** Diff the old and new upstream trees and update the audit
   notes in `docs/upstream/*.md` for every changed module: new options,
   changed defaults, fixed or new bugs.
4. **Port.** Port the behavior changes; keep `docs/feature-matrix.md`
   current. An upstream fix of a bug the port already fixed removes the
   corresponding entry from `docs/differences.md` (or narrows it); a new
   intentional deviation gets a new entry with its evidence and test.
5. **Re-verify.** Rebuild the reference (`./build.sh`), regenerate the
   golden files with the harnesses and review their diffs, and run the
   full differential suite (`./difftest.sh 50000 1 10000`) with zero
   failures. Update the upstream version in the README, the CLI banner
   (only if upstream changed it) and `docs/decisions.md` (D1).
6. **Release.** Adopting a new upstream version is at least a minor
   release; note the upstream version and every changed deviation in the
   changelog.

## Releases

See [docs/releasing.md](docs/releasing.md).
