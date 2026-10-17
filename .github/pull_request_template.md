## What and why

<!-- What does this change, and why? Link issues, deviation ids
(docs/differences.md) and decisions (docs/decisions.md) it touches. -->

## How it was tested

<!-- Commands run and their results, e.g. go test ./..., go test -race ./...,
(cd reference && go test ./...) for changes to mining, output, input or
the CLI. -->

## Checklist

- [ ] `gofmt`, `go vet` and `staticcheck` are clean; `go test ./...` passes.
- [ ] Behavior changes relative to upstream are intentional, listed in
      `docs/differences.md` and covered by a test.
- [ ] The CLI stays compatible with upstream `fpgrowth` (or the deviation
      is documented).
- [ ] Docs updated (README, `docs/cli.md`, `docs/feature-matrix.md`,
      doc comments) where needed.
- [ ] `CHANGELOG.md` (Unreleased) updated for user-visible changes.
- [ ] No new dependencies outside the standard library in the root module.
