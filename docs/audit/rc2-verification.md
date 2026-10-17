# v0.1.0-rc.2 verification

- **Tag:** `v0.1.0-rc.2` (annotated, object `d934db5`) on commit
  `534855af50a9fb7351753a89ae66b37676e4a671` (merge of
  [#10](https://github.com/dimchansky/fpgrowth-go/pull/10); contains the
  release-notes fix [#9](https://github.com/dimchansky/fpgrowth-go/pull/9)).
- **Release:** https://github.com/dimchansky/fpgrowth-go/releases/tag/v0.1.0-rc.2
- **Release run:** [39038257393](https://github.com/dimchansky/fpgrowth-go/actions/runs/39038257393)
- **Authorization:** the maintainer authorized rc.2 after the
  [rc.1 verification](rc1-verification.md) and approved its deployment
  (`dimchansky`, `release` environment).

## Before the tag

| Check | Result |
| --- | --- |
| Tagged commit = `origin/main` = local `HEAD` | `534855a`; clean working tree; no open pull requests; only other tag `v0.1.0-rc.1` (its run finished) |
| CI on `main` for exactly this commit | run [39037215109](https://github.com/dimchansky/fpgrowth-go/actions/runs/39037215109): success |
| Fresh clone from GitHub at `534855a` | `go test ./...` ok, `go vet ./...` ok, `modcheck zip` (1 427 files, 2.3 MB), `modcheck tag v0.1.0-rc.2` valid, `modcheck notes` gives the `[0.1.0]` section with the ALG-1 known limitation, `changelog: use: git`, `goreleaser check` ok, snapshot built 6 archives |
| Push | only `refs/tags/v0.1.0-rc.2` |

## Release workflow

Validate the tag and all 15 quality gates succeeded; "Publish release"
waited for the `release` environment and ran after the maintainer's
approval. All its steps succeeded, including the new "Check the release
notes" before the attestation.

## Published release

| Check | Result |
| --- | --- |
| Release notes (RC-1) | **fixed**: the body contains the `CHANGELOG.md` `[0.1.0]` section verbatim (including "Known limitations" with ALG-1), followed by the verification footer |
| Pre-release / Latest / immutable | `isPrerelease: true`; no Latest release exists (`/releases/latest` 404); `isImmutable: true` |
| Assets | `checksums.txt` and 6 archives (darwin, linux, windows × amd64, arm64) |
| Checksums | all 6 OK |
| Attestations | all 6 verified with `--signer-workflow …/release.yml --source-ref refs/tags/v0.1.0-rc.2 --deny-self-hosted-runners`: source digest `534855a` |
| Archive contents | binary, `LICENSE`, `README.md`, `CHANGELOG.md`, `PROVENANCE.md` in each |
| Binary metadata (darwin/arm64) | `go1.27.2`, `github.com/dimchansky/fpgrowth-go v0.1.0-rc.2`, `-trimpath=true`, `CGO_ENABLED=0`, `vcs.revision=534855a…`, `vcs.modified=false`; runs correctly |

## External consumer

New module outside the repository, empty module cache, public proxy and
checksum database, no `replace`: `go get …@v0.1.0-rc.2`
(`h1:eEhU5AvslMxaFNH1JyX4IkVHD5dsQqShKBF/Ntln+3s=`). The smoke program
of the rc.1 verification (frequent, closed, maximal, rules, weights,
`TableFile`, streaming, callback errors, cancellation, option
validation; checked against its own brute force) passes all 10 checks
with Go 1.27.1 and Go 1.26.9. `go install …/cmd/fpgrowth@v0.1.0-rc.2`
gives `mod … v0.1.0-rc.2`; its output equals the released binary's on 6
option sets.

## Gate D

**Accepted for v0.1.0-rc.2**: the hosted release workflow passed,
artifacts, notes and attestations are verified, the external
installation works, and no release-blocking defect is open. Open item
for the maintainer: ALG-1 stays a documented known limitation unless
the maintainer decides it needs an implementation fix before v0.1.0.
RC-2 (Info) from rc.1 is unchanged. The final release `v0.1.0` needs a
separate authorization (Gate E).
