# Releasing

How versions are numbered, how a release is cut, and what the release
workflow does. The workflow files are `.github/workflows/release.yml`
(release) and `.github/workflows/ci.yml` (quality gates, also run for
every release); the build configuration is `.goreleaser.yaml`.

## Versioning

Releases follow [Semantic Versioning 2.0.0](https://semver.org/) with the
`v` prefix that Go modules require: `vMAJOR.MINOR.PATCH`, optionally with
a pre-release suffix (`v0.2.0-rc.1`). The tag must be canonical: no
leading zeros, no empty or numeric-with-leading-zero pre-release
identifiers (`v1.2.3-rc.01` is refused), and no build metadata
(`+build`). The workflow checks this with `golang.org/x/mod/semver`
(`reference/modcheck`); to check a tag before pushing it:

```bash
(cd reference && go run ./modcheck tag vX.Y.Z)
```

One version covers both deliverables of the module
`github.com/dimchansky/fpgrowth-go`: the library API (package `fpgrowth`)
and the `fpgrowth` command. The nested module `reference/` is
development-only and never tagged.

### v0: no stability guarantee yet

While the major version is 0, the API may change:

- **Minor** (`v0.X.0`): new features, and incompatible API changes when
  needed (always listed under "Changed" or "Removed" in the changelog).
- **Patch** (`v0.x.Y`): bug fixes, documentation, and fixes that bring
  results closer to the documented behavior, without API changes.

### v1 and later

From `v1.0.0` on, the [Go 1 compatibility](https://go.dev/doc/go1compat)
style promise applies to the exported API of package `fpgrowth`:

- **Patch**: bug fixes only.
- **Minor**: backward-compatible additions (new functions, types, options
  fields, measures).
- **Major**: any incompatible change, which also changes the import path
  (see below).

Not covered by the API promise, in any version: the order of results
(upstream's order, which depends on data, options and algorithm variant),
the last bits of log-based evaluation values (F-1 in
`docs/differences.md`), and performance. A fix that makes results match
the documented behavior (a new entry in `docs/differences.md`) is a bug
fix, not a breaking change, but it is always called out in the
changelog.

The CLI's compatibility target is upstream `fpgrowth`: options, formats,
messages and exit codes change only to follow upstream or to fix a
documented deviation, and each such change is listed in the changelog.

### Major versions 2 and later: new import path

Go's [semantic import versioning](https://go.dev/ref/mod#major-version-suffixes)
requires the module path of `v2` and later to end in the major version:

```
module github.com/dimchansky/fpgrowth-go/v2
```

Every import in the repository (root package, `cmd/fpgrowth`,
`internal/...`, and the `require`/`replace` lines of `reference/go.mod`)
changes to the `/v2` path in the same commit, before the `v2.0.0` tag.
The release workflow refuses a `vN` tag (N ≥ 2) whose `go.mod` lacks the
`/vN` suffix.

### Go version support

The `go` directive (currently `1.26.0`) is the minimum supported Go
version; it follows Go's support policy (the two latest major releases).
Raising it is allowed in a minor release and is noted in the changelog.
Release binaries are built with the toolchain pinned by the `toolchain`
directive (currently `go1.27.2`, `docs/decisions.md` D3).

## Cutting a release

Prerequisites (once per repository, see
[Repository settings](#repository-settings)): the `release` environment,
the rulesets for `main` and `v*` tags, and immutable releases are in
place.

**The tag push publishes the library.** The Go module version
`vX.Y.Z` becomes public as soon as the tag reaches GitHub: anyone (or
pkg.go.dev, or a bot) can fetch it through `proxy.golang.org`, and
`sum.golang.org` records its content forever, whether or not the release
workflow then passes or the deployment is approved. The workflow gates
only the GitHub release and its binaries. The real gate for the library
is the `main` ruleset (a pull request and the 15 CI checks are required
for every change), so tag only a commit of `main` whose own CI run on
`main` succeeded (step 4).

1. Make sure `main` is green in CI and up to date locally:

   ```bash
   git switch main && git pull --ff-only
   ```

2. Update [CHANGELOG.md](../CHANGELOG.md): rename `## [Unreleased]` to
   `## [X.Y.Z] - YYYY-MM-DD`, add a new empty `## [Unreleased]` above it,
   and update the comparison links at the bottom. The release notes are
   taken from this section; the workflow fails if it is missing or empty.
   A pre-release tag (`vX.Y.Z-rc.N`) uses its own `## [X.Y.Z-rc.N]`
   section if there is one, and otherwise falls back to the `## [X.Y.Z]`
   section, so a release candidate can be cut from the prepared final
   section. Check what will be used:
   `(cd reference && go run ./modcheck notes vX.Y.Z)`.
   Commit ("Prepare vX.Y.Z") and merge it to `main` like any change.

3. Pre-tag checklist, on a **fresh clone** of the commit to tag (only
   committed files count; a test that passes only because of an
   uncommitted fixture fails here, as it would for users and CI):

   ```bash
   tmp=$(mktemp -d) && git clone --quiet . "$tmp/fpgrowth-go" && cd "$tmp/fpgrowth-go"
   go test ./...
   (cd reference && go run ./modcheck zip ..)   # module zip as the Go proxy builds it
   (cd reference && go run ./modcheck tag vX.Y.Z)
   goreleaser check
   goreleaser release --snapshot --clean --skip=publish
   ```

   `modcheck zip` fails on any file the Go command refuses in a module
   zip (Windows-reserved names such as `nul.txt`, case collisions, size
   limits) and lists the omitted files (`.git`, the nested `reference/`
   module) and the zip size. CI runs it in the lint job.

4. Check that CI passed for exactly this commit on `main` (the ruleset
   checks pull requests, but does not require them to be up to date
   with `main`, so the merge commit's own run is what counts), then tag
   it with an annotated (ideally signed) tag and push the tag:

   ```bash
   git fetch origin && test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"
   gh run list --workflow ci.yml --branch main --event push \
     --commit "$(git rev-parse HEAD)" --json conclusion --jq '.[0].conclusion'
   # must print: success
   git tag -a vX.Y.Z -m "vX.Y.Z"        # or: git tag -s vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

   When an RC and the final version are on the same commit, push the
   final tag only after the RC's release run has finished: the Go
   toolchain stamps the binaries' module version from the tags on the
   commit and may otherwise embed the final version in the RC binaries
   (`go version -m`).

5. Approve the `release` deployment when GitHub asks. GitHub asks only
   after the tag passed `validate-tag` and all quality gates. With a
   single maintainer, the maintainer is the required reviewer and
   self-review is allowed: open the waiting run (Actions tab, or the
   e-mail notification), choose "Review deployments", tick `release`,
   and choose "Approve and deploy" (or "Reject", which leaves no release;
   the module version stays public, see above). Before approving, check
   that the run's commit SHA is the `main` commit you tagged: the
   environment gates only workflows that declare it, and a tag runs the
   workflow file of the tagged commit, so this check is what ties the
   approval to reviewed code. Administrators cannot bypass this step.

6. Check the published release page, then make the module version known
   to the Go proxy and pkg.go.dev:

   ```bash
   GOPROXY=https://proxy.golang.org go list -m github.com/dimchansky/fpgrowth-go@vX.Y.Z
   ```

Never move or delete a pushed tag: the Go checksum database records the
content of every version it has seen, so a re-tagged version fails
verification for everyone (the `release tags` ruleset refuses both).
Fix a bad release with a new patch version, and mark the bad one with a
`retract` directive in `go.mod` (`retract vX.Y.Z // reason`) in that
next release.

## What the release workflow does

`release.yml` runs on a pushed tag matching `v*.*.*`. It gates the
GitHub release and its binaries; the Go module version is already
public at this point (see [Cutting a release](#cutting-a-release)).

1. **Tag validation** (`validate-tag` job, read-only, first): the tag is
   canonical SemVer without build metadata, a `v2+` tag matches the
   module path, `CHANGELOG.md` has a non-empty section for it (with the
   pre-release fallback above), and the tagged commit is on `main`. The
   other jobs depend on it, so an invalid tag never reaches the approval
   step.
2. **Quality gates** (`ci` job): calls `ci.yml` with read-only
   permissions: lint (gofmt, vet, staticcheck, `go mod tidy`, the
   module zip check),
   govulncheck, tests on Linux/macOS/Windows with Go 1.26 and 1.27 and the
   race detector, coverage, cross-builds (including 32-bit), fuzz smoke
   tests, the C reference build with the differential tests on Linux and
   macOS, and a GoReleaser snapshot.
3. **Publish** (`publish` job, only after both jobs above pass, only in
   the repository `dimchansky/fpgrowth-go`): runs in the protected
   `release` environment and is the only job with `contents: write` (plus
   `id-token: write` and `attestations: write` for the provenance
   attestation). It
   - extracts the tag's section of `CHANGELOG.md` as the release notes;
   - runs GoReleaser with the pinned Go toolchain and no restored build
     cache: `fpgrowth` binaries for linux, darwin and windows on amd64 and
     arm64, built with `CGO_ENABLED=0 -trimpath -ldflags="-s -w"` and file
     times from the commit (reproducible: rebuilding the same commit with
     the same toolchain gives the same `checksums.txt`);
   - packs each binary with `LICENSE`, `README.md`, `CHANGELOG.md` and
     the upstream `PROVENANCE.md` into `fpgrowth_<version>_<os>_<arch>`
     (`.tar.gz`, `.zip` for Windows) with fixed owner, mode and times;
   - writes `checksums.txt` (SHA-256) and creates the GitHub release as
     a **draft** (pre-release for versions with a suffix);
   - creates a signed build provenance attestation for every archive
     listed in `checksums.txt` (GitHub artifact attestations, Sigstore);
   - only then publishes the draft (`gh release edit --draft=false`), so
     a release is never visible without its attestation. If a step fails,
     the draft stays unpublished; rerunning the workflow replaces it.
     Publishing sets both flags explicitly: a pre-release is never
     marked "Latest", and a final release is "Latest" only if no higher
     final `vX.Y.Z` tag exists (a backport patch of an older line is
     not). If a higher tag exists whose release was never published,
     mark the right release by hand: `gh release edit vX.Y.Z --latest`.
     With immutable releases, the published release's assets and tag
     cannot be changed afterwards.

The `publish` job's write and OIDC permissions apply to all of its
steps, including `go run ./modcheck` and the GoReleaser build. This is
accepted: every action is pinned by SHA, Go dependencies are verified
against `go.sum`, GoReleaser runs no hooks, and the job runs only after
approval. Splitting build, attestation and upload into separate jobs
would narrow it further.

`goreleaser-action` verifies the downloaded GoReleaser archive against
the checksum file of the same GoReleaser release, which protects only
against corruption in transit (its cosign signature check is skipped
because cosign is not installed). The pinned GoReleaser release is an
immutable GitHub release, which is the remaining protection.

The GoReleaser version is pinned once, in `.github/goreleaser-version`,
and read by both `ci.yml` (snapshot) and `release.yml`. The govulncheck
pin appears in `ci.yml` and `vulncheck.yml` (weekly scan of `main`), the
staticcheck pin in `ci.yml`, and both in `CONTRIBUTING.md`; the lint job
fails if they differ.

The workflow uses only the automatic `GITHUB_TOKEN` and a short-lived
OIDC token; there are no stored secrets. All actions are pinned to full
commit SHAs.

The binaries do not carry a release version string: `cmd/fpgrowth`
prints upstream's banner (`version 6.21 (2022.11.22)`) for
compatibility and has no version variable to inject. The module version
and commit are embedded by the Go toolchain and can be read with
`go version -m fpgrowth`.

## Verifying a download

```bash
# checksums (Linux; on macOS use: shasum -a 256 --ignore-missing -c checksums.txt)
sha256sum --ignore-missing -c checksums.txt

# build provenance (GitHub CLI): built by release.yml of this repository
# from the tag vX.Y.Z on a GitHub-hosted runner
gh attestation verify fpgrowth_X.Y.Z_linux_amd64.tar.gz \
  --repo dimchansky/fpgrowth-go \
  --signer-workflow dimchansky/fpgrowth-go/.github/workflows/release.yml \
  --source-ref refs/tags/vX.Y.Z \
  --deny-self-hosted-runners

# embedded build information of the binary
go version -m ./fpgrowth
```

Without `--signer-workflow` and `--source-ref`, an attestation from any
workflow of the repository, or from any ref, would be accepted. The
attestations cover the archives listed in `checksums.txt`, not
`checksums.txt` itself.

## Go module proxy notes

- Library users need no release artifacts: `go get
  github.com/dimchansky/fpgrowth-go@vX.Y.Z` fetches the tagged source
  through `proxy.golang.org`, verified against `sum.golang.org`.
- The module zip excludes the nested module `reference/` (its own
  `go.mod`), so the upstream C sources and the differential tests are
  not downloaded by users.
- `go install github.com/dimchansky/fpgrowth-go/cmd/fpgrowth@vX.Y.Z`
  builds the CLI from the proxy; the result reports the module version in
  `go version -m`.
- The proxy caches a version forever once fetched; pkg.go.dev picks it up
  from the proxy (step 6 above triggers it).

## Repository settings

Configured once by a repository administrator (in place since
2026-10-17); keep them when changing the workflows:

- **Environment `release`** (Settings, Environments): required reviewer
  `dimchansky`, "Prevent self-review" off (single maintainer),
  administrators cannot bypass, deployments only from tags matching
  `v*.*.*`.
- **Ruleset `main`** (Settings, Rules, rulesets; the default branch, no
  bypass actors): pull request required (0 approvals, conversations
  resolved), 15 required status checks from GitHub Actions (the job
  names of `ci.yml`: lint, govulncheck, the seven test legs, coverage,
  cross-build, fuzz smoke, the two C reference legs and the GoReleaser
  snapshot; renaming a job requires updating the ruleset), no force
  pushes, no deletion. Branches need not be up to date before merging,
  hence the check in step 4 above.
- **Ruleset `release tags`** (`refs/tags/v*`, no bypass actors): tags
  cannot be updated (moved), deleted or force-pushed; creating them
  needs write access.
- **Releases**: immutable releases enabled, so a published release's
  assets and tag cannot be changed.
- **Actions** (Settings, Actions, General): default `GITHUB_TOKEN`
  permissions read-only; Actions cannot create or approve pull requests;
  only GitHub-owned actions and `goreleaser/goreleaser-action` are
  allowed, pinned to a full commit SHA; workflows from fork pull
  requests need approval for all external contributors.
- **Security** (Settings, Code security): private vulnerability
  reporting (the channel in [SECURITY.md](../SECURITY.md)), Dependabot
  alerts and security updates, secret scanning and push protection.

## Do not re-run a successful release

Re-running a successful release workflow run makes GoReleaser try to
update the already published release (`mode: replace`); with immutable
releases GitHub refuses to change its assets, so the rerun is expected
to fail (not yet exercised). If a
release needs a fix, publish a new patch version instead. When an RC and
the final version share a commit, the workflow passes the triggering tag
to GoReleaser (`GORELEASER_CURRENT_TAG`) and fails if `dist/metadata.json`
names a different tag.
