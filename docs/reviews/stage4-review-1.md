# Stage 4 review — iteration 1 (with final production-readiness audit)

- **Stage / iteration:** Stage 4 (CI/CD, releases, OSS files), iteration 1
- **Critic:** independent reviewer agent (DevOps, GitHub Actions security,
  open-source release engineering; final audit); not involved in the
  files; worked only in fresh clones; nothing pushed or published.
- **Revision:** `acda7d9`.

## Evidence gathered by the critic

| Check | Result |
| --- | --- |
| actionlint (with shellcheck) | 0 errors |
| Pinned action SHAs vs tags (`gh api`) | all 5 match and are the latest releases |
| gofmt, vet, staticcheck, `go mod tidy -diff` (both modules), build | ok |
| **`go test ./...` in a fresh clone (Go 1.27.2 and 1.26.9, with and without `-race`)** | **FAIL** in `internal/isrout`, `internal/istree`, `internal/report`: golden files missing (C1) |
| **Module zip (`golang.org/x/mod/zip.CheckDir`/`CreateFromDir`)** | **FAIL**: `testdata/input/nul.txt` is a Windows-reserved name (C2); `reference/` and `.git` correctly omitted |
| Tests with fixtures restored (both Go versions, `-race`); coverage commands | all pass; 97.0 % |
| Windows test compilation and vet; 12-target cross-build; darwin/amd64 tests | ok |
| govulncheck (both modules) | no vulnerabilities |
| Fuzz-smoke loop (9 targets) | pass |
| C reference job + difftest | ok |
| goreleaser check; snapshot in two clones | ok; identical `checksums.txt` |
| Tagged dry run v0.1.0 | 6 archives with binary, LICENSE, README, CHANGELOG, PROVENANCE; `-trimpath`, `CGO_ENABLED=0` |
| Release-notes extraction, tag regex | works; regex too permissive (L3) |
| README quick start, `go install ./cmd/fpgrowth`, `go doc .` | match / work / correct |
| Workflow threat model (triggers, permissions, injection, credentials, caching, supply chain) | no findings beyond the table below |

## Findings

| ID | Severity | Title |
| --- | --- | --- |
| C1 | **Critical** | 245 golden fixtures never committed (unanchored `*.out` ignore rule); clean checkouts fail their tests, so CI and the release gate would be red |
| C2 | **Critical** | `internal/tabread/testdata/input/nul.txt` is a Windows-reserved name: no module zip can be created (library not fetchable via the proxy); Windows checkout likely fails |
| M1 | Medium | No clean-checkout / module-zip gate |
| L1 | Low | Release published before provenance attestation |
| L2 | Low | Tag validated only after full CI and environment approval |
| L3 | Low | Tag regex accepts invalid SemVer pre-releases |
| L4 | Low | Pre-release CHANGELOG sections undocumented |
| L5 | Low | No scheduled govulncheck; SECURITY.md "rebuild" wording |
| L6 | Low | README/PROGRESS status ahead of the review |
| L7 | Low | `GORELEASER_VERSION` duplicated in two workflows |
| I1–I4 | Info | Windows race not run; cache key; dev files in module zip; link definitions in release notes |

## Decision

**REJECT** (C1, C2). Iteration 2 must re-run the tests from a fresh clone
on both Go versions with `-race` and the module-zip check.

## Owner actions that cannot be done locally

1. Create the public repository `dimchansky/fpgrowth-go`, push `main`, and
   confirm all CI jobs pass (especially Windows checkout and tests).
2. Create the `release` environment: required reviewer, deployment refs
   limited to `v*.*.*` tags.
3. Rulesets: protect `v*` tags; protect `main` (PRs, required status
   checks, no force push).
4. Actions settings: read-only default `GITHUB_TOKEN`; require SHA-pinned
   actions if available; disallow Actions creating/approving PRs.
5. Enable private vulnerability reporting, Dependabot alerts and security
   updates.
6. Before the first tag: rename `## [Unreleased]` in CHANGELOG, tag with an
   annotated/signed tag, approve the deployment, verify with
   `gh attestation verify`, and confirm
   `GOPROXY=https://proxy.golang.org go list -m github.com/dimchansky/fpgrowth-go@v0.1.0`
   and pkg.go.dev rendering/license detection.

## Fix record

- C1, C2: fixed in `e70d1ce` (anchored ignore rules, 245 fixtures
  committed, `nul.txt` → `nul-byte.txt`); fresh-clone `go test ./...`
  passes on Go 1.27.2 and 1.26.9.
- Remaining items: see iteration 2.
