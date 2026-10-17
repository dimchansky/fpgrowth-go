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
[Repository settings](#repository-settings)): the `release` environment
exists and is protected.

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

4. Tag the merged commit on `main` with an annotated (ideally signed)
   tag and push the tag:

   ```bash
   git tag -a vX.Y.Z -m "vX.Y.Z"        # or: git tag -s vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

5. Approve the `release` deployment when GitHub asks (Actions tab, or the
   notification to the environment's reviewers). GitHub asks only after
   the tag passed `validate-tag` and all quality gates.

6. Check the published release page, then make the module version known
   to the Go proxy and pkg.go.dev:

   ```bash
   GOPROXY=https://proxy.golang.org go list -m github.com/dimchansky/fpgrowth-go@vX.Y.Z
   ```

Never move or delete a pushed tag: the Go checksum database records the
content of every version it has seen, so a re-tagged version fails
verification for everyone. Fix a bad release with a new patch version,
and mark the bad one with a `retract` directive in `go.mod`
(`retract vX.Y.Z // reason`) in that next release.

## What the release workflow does

`release.yml` runs on a pushed tag matching `v*.*.*`:

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

The GoReleaser version is pinned once, in `.github/goreleaser-version`,
and read by both `ci.yml` (snapshot) and `release.yml`. The govulncheck
pin appears in `ci.yml` and `vulncheck.yml` (weekly scan of `main`); the
lint job fails if the two differ.

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

# build provenance (GitHub CLI): which workflow, commit and tag built it
gh attestation verify fpgrowth_X.Y.Z_linux_amd64.tar.gz --repo dimchansky/fpgrowth-go

# embedded build information of the binary
go version -m ./fpgrowth
```

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

Required once, by a repository administrator:

- **Environment `release`** (Settings, Environments): required reviewers
  (the maintainer), "Prevent self-review" off for a single maintainer,
  deployment branches and tags limited to tags matching `v*.*.*`.
- **Tag protection** (Settings, Rules, rulesets): restrict creation,
  update and deletion of `v*` tags to maintainers; block force pushes.
- **Branch protection for `main`**: require pull requests and the CI
  status checks; block force pushes.
- **Actions** (Settings, Actions, General): default `GITHUB_TOKEN`
  permissions read-only; allow only actions pinned to a full commit SHA
  (if available); do not allow Actions to create or approve pull
  requests.
- **Security** (Settings, Code security): enable private vulnerability
  reporting, Dependabot alerts and security updates.
