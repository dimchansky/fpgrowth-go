# v0.1.0-rc.1 verification

- **Tag:** `v0.1.0-rc.1` (annotated, object `f6d9524`) on commit
  `847831142524f19d7739900588f1c2bd331d61f6` (merge of
  [#8](https://github.com/dimchansky/fpgrowth-go/pull/8) "Prepare v0.1.0").
- **Release:** https://github.com/dimchansky/fpgrowth-go/releases/tag/v0.1.0-rc.1
- **Release run:** [39029057065](https://github.com/dimchansky/fpgrowth-go/actions/runs/39029057065)
- **Authorization:** Gate C authorized by the maintainer for this tag
  only; the deployment was approved by the maintainer (`dimchansky`) in
  the `release` environment.

## Before the tag

| Check | Result |
| --- | --- |
| Tagged commit = `origin/main` = local `HEAD` | `8478311` for all three; clean working tree; no other tags locally or on GitHub; no open pull requests |
| CI on `main` for exactly this commit | run [39028734972](https://github.com/dimchansky/fpgrowth-go/actions/runs/39028734972): success, 15/15 jobs |
| Every change on `main` reviewed | PRs #1–#8 each passed the 15 required checks; #1–#7 had independent reviews (`docs/audit/independent-audit.md` §6); #8 changes only release metadata |
| Fresh clone from GitHub at `8478311` | `go test ./...` (11 packages ok), `go vet ./...` ok, `modcheck zip` (1 425 files, 2.3 MB, `reference/` omitted), `modcheck tag v0.1.0-rc.1` valid, `modcheck notes` finds the `[0.1.0]` section, `goreleaser check` ok, `goreleaser release --snapshot` built 6 archives and checksums |
| Push | only `refs/tags/v0.1.0-rc.1` |

## Release workflow

| Stage | Result |
| --- | --- |
| Validate the tag | success |
| Quality gates (15 jobs of `ci.yml`) | success |
| Publish release | waited for the `release` environment (reviewer `dimchansky`, no wait timer); started only after the maintainer's approval; all steps succeeded: GoReleaser, tag check, provenance attestation, undraft |

## Published release

| Check | Result |
| --- | --- |
| Pre-release flag | `isPrerelease: true` |
| Latest | not marked Latest (`/releases/latest` returns 404: no Latest release exists) |
| Immutable | `isImmutable: true` |
| Assets | `checksums.txt` and 6 archives: darwin, linux, windows × amd64, arm64 |
| Checksums | `shasum -a 256 -c checksums.txt`: all 6 OK |
| Attestations | `gh attestation verify <archive> --repo dimchansky/fpgrowth-go --signer-workflow dimchansky/fpgrowth-go/.github/workflows/release.yml --source-ref refs/tags/v0.1.0-rc.1 --deny-self-hosted-runners`: all 6 verified, source digest `8478311`, ref `refs/tags/v0.1.0-rc.1`. `checksums.txt` itself is not attested (by design: `subject-checksums` attests the files it lists) |
| Archive contents | each: the binary, `LICENSE`, `README.md`, `CHANGELOG.md`, `PROVENANCE.md`; no development files |
| Binary metadata (darwin/arm64) | `go1.27.2`, module `github.com/dimchansky/fpgrowth-go v0.1.0-rc.1`, `-trimpath=true`, `CGO_ENABLED=0`, `vcs.revision=8478311…`, `vcs.modified=false`; runs and mines correctly |
| **Release notes** | **missing (RC-1)**: the body holds only the verification footer |

## External consumer

A new module `example.com/fpgrowth-smoke` outside the repository, empty
module cache, `GOPROXY=https://proxy.golang.org`, `GOSUMDB=sum.golang.org`,
`GOWORK=off`, no `replace`:

- `go get github.com/dimchansky/fpgrowth-go@v0.1.0-rc.1` resolved through
  the proxy (`h1:iM9Qhs4bkHIQyypk8tf52ve9aB2oi3V7wPQ7/X9XRKA=`).
- A program checked against its own brute force: frequent, closed and
  maximal item sets on weighted transactions; association rules (support,
  body/head support, confidence, lift); `TableFile` with `Fraction`
  support; iterator streaming with early stop; a callback error returned
  unchanged; cancellation before and during mining; rejection of a
  partial `TableFormat`. All checks pass with Go 1.27.1 and with Go 1.26.9
  (the minimum supported version, `go 1.26.0` in the consumer's `go.mod`).
- `go install github.com/dimchansky/fpgrowth-go/cmd/fpgrowth@v0.1.0-rc.1`
  builds the CLI (`mod … v0.1.0-rc.1`); its output equals the released
  darwin/arm64 binary's byte for byte on 6 option sets (`-tc`, `-tm`,
  `-tg`, `-tr` with `-ez`, `-ts -n2`, `-tc -n2 -At`).

The consumer program first failed two checks because of mistakes in the
test itself, not the library: a `Transaction` without `Weight` has weight
0 (documented), and an `Options` without `MinSupport` is rejected for
that field first.

## Findings

| ID | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| RC-1 | Medium | The release has no notes. `.goreleaser.yaml` set `changelog.disable: true`, and in GoReleaser v2.18.3 the changelog pipe is the one that loads `--release-notes`; snapshots always skip that pipe, so the pre-tag snapshot could not show it | Fixed in [#9](https://github.com/dimchansky/fpgrowth-go/pull/9): changelog enabled, and the workflow checks the draft's notes before attesting and publishing. Needs a new release candidate to confirm on a real run. The rc.1 release itself is immutable and stays as published |
| RC-2 | Info | If the context is canceled inside `OnStart` and the database has no frequent item, `MineItemsets` returns nil (nothing remained to mine) rather than `ctx.Err()` | Recorded; no change before v0.1.0 |

No defect was found in the binaries, the library, the attestations or
the protections. ALG-1 remains a documented known limitation (release
notes "Known limitations", `docs/differences.md` N1); whether it needs an
implementation fix before v0.1.0 is the maintainer's decision.

## Gate D

Not accepted for rc.1: the release path works end to end except for the
release notes (RC-1). A release candidate `v0.1.0-rc.2` from a `main`
that contains #9 would confirm the fix; it needs the maintainer's
authorization.
