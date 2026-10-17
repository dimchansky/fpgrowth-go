# v0.1.0 release verification

- **Tag:** `v0.1.0` (annotated, object `c2c9e5e`) on commit
  `e99628c79d9879f0d9a7c9663b85858d5318fa25` (merge of
  [#12](https://github.com/dimchansky/fpgrowth-go/pull/12) "Prepare
  v0.1.0", documentation only; no change outside the docs and
  `CHANGELOG.md` since `v0.1.0-rc.2`, which passed Gate D,
  [rc2-verification.md](rc2-verification.md)).
- **Release:** https://github.com/dimchansky/fpgrowth-go/releases/tag/v0.1.0
- **Release run:** [39043151536](https://github.com/dimchansky/fpgrowth-go/actions/runs/39043151536)
- **Authorization:** Gate E authorized by the maintainer, who decided to
  ship ALG-1 as a documented known limitation and to defer it and RC-2 to
  a future release; the deployment was approved by the maintainer
  (`dimchansky`, `release` environment).

## Before the tag

| Check | Result |
| --- | --- |
| PR #12 | 15/15 checks; merged |
| CI on `main` for exactly the tagged commit | run [39042770961](https://github.com/dimchansky/fpgrowth-go/actions/runs/39042770961): success, 15/15 jobs |
| Tagged commit = `origin/main` = local `HEAD` | `e99628c`; clean tree; no open pull requests; earlier release runs finished; no `v0.1.0` tag before |
| Fresh clone from GitHub at `e99628c` | `go test ./...` ok, `go vet ./...` ok, `modcheck zip` (1 428 files), `modcheck tag v0.1.0` valid, `modcheck notes v0.1.0` (ALG-1 "deferred to a future release"), `goreleaser check` ok, snapshot built 6 archives |
| Push | only `refs/tags/v0.1.0` |

## Release workflow

Validate the tag and the 15 quality gates succeeded; "Publish release"
waited for the `release` environment and ran after the maintainer's
approval; all steps succeeded, including the release-notes check.

## Published release

| Check | Result |
| --- | --- |
| Latest / pre-release / immutable | `/releases/latest` is `v0.1.0`; `isPrerelease: false`; `isImmutable: true`; rc.1 and rc.2 remain pre-releases |
| Release notes | the `CHANGELOG.md` `[0.1.0]` section verbatim (ALG-1 under "Known limitations", deferred), followed by the verification footer; no "decision pending" wording |
| Assets | `checksums.txt` and 6 archives (darwin, linux, windows × amd64, arm64) |
| Checksums | all 6 OK |
| Attestations | all 6 verified (`--signer-workflow …/release.yml --source-ref refs/tags/v0.1.0 --deny-self-hosted-runners`): source digest `e99628c` |
| Archive contents | binary, `LICENSE`, `README.md`, `CHANGELOG.md`, `PROVENANCE.md` in each |
| Binaries | all 6: `mod github.com/dimchansky/fpgrowth-go v0.1.0`, `vcs.revision=e99628c…`, `vcs.modified=false`, `CGO_ENABLED=0`; darwin/arm64 runs correctly |

## Public distribution

| Check | Result |
| --- | --- |
| Go module proxy | `@v/list`: `v0.1.0-rc.1 v0.1.0-rc.2 v0.1.0`; `@latest`: `v0.1.0` (`e99628c`, `refs/tags/v0.1.0`); `go list -m …@v0.1.0` ok |
| Library install | fresh module, empty cache, no `replace`: `go get …@latest` → `v0.1.0` (`h1:315ZdPYbYP+ECewoUQJGwg8DFcwCyG3vsTfas9GOQ3U=`); the smoke program (frequent, closed, maximal, rules, weights, `TableFile`, streaming, callback errors, cancellation, validation; checked against its own brute force) passes with Go 1.27.1 and Go 1.26.9 |
| CLI install | `go install …/cmd/fpgrowth@v0.1.0` (`mod … v0.1.0`); output equals the released binary on 6 option sets |
| pkg.go.dev | https://pkg.go.dev/github.com/dimchansky/fpgrowth-go@v0.1.0 indexed: package documentation, version v0.1.0, MIT license |

## Remaining limitations and follow-ups

Documented known limitation: ALG-1 (`docs/differences.md` N1). Deferred
to future releases: ALG-1 exact arithmetic, RC-2, M5A-1, NEW-1, NEW-3,
TST-3, `fpgpsp`, `-z` (`docs/PROGRESS.md`).
