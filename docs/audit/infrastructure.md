# Infrastructure / supply-chain / release audit: fpgrowth-go @ f7bb606

Scope: `.github/workflows/{ci,release,vulncheck}.yml`, `.github/dependabot.yml`,
`.github/goreleaser-version`, `.goreleaser.yaml`, `docs/releasing.md`,
`CHANGELOG.md`, `LICENSE`, `reference/upstream/PROVENANCE.md`,
`CONTRIBUTING.md`, `SECURITY.md`, `reference/modcheck`, hosted CI run
39004922968, and the live repository settings (read-only `gh api GET`).

Clone: `scratchpad/audit/infra` at `f7bb606643b2fdc10ffcc907271753b97892a4f7`.
Throwaway material: `scratchpad/audit/infra/_audit/infra/` (run.json, run.log,
snapshot clones `snap/`, `snap2/`, their logs, `ci-checksums.txt`).
Nothing was changed on GitHub; nothing committed in the clone.

## Summary

The workflow code is in good shape: every action is SHA-pinned and each pin
was verified to equal its tag, there is no untrusted-input interpolation in
`run:` steps, `pull_request` (not `_target`) is used, credentials are not
persisted, permissions are least-privilege per job, the release is drafted
and only undrafted after the provenance attestation step, and the build is
bit-reproducible (CI Linux checksums == local macOS checksums, twice).

The release-safety controls the docs rely on, however, **do not exist on the
repository**: no `release` environment (so no approval gate), no rulesets
(no main branch protection, no tag protection, no required checks), private
vulnerability reporting disabled although SECURITY.md points reporters to it,
Dependabot alerts and security updates off, immutable releases off. Because
a tag-triggered workflow runs the `release.yml` *of the tagged commit*, the
in-workflow checks (`validate-tag`, ancestry on main) are not a control
against whoever can push a tag; only the environment and rulesets are.

| ID | Sev | Status | Title | Blocks v0.1.0 |
| --- | --- | --- | --- | --- |
| INF-1 | Critical | Confirmed | `release` environment does not exist: no approval gate | yes |
| INF-2 | High | Confirmed | No rulesets: main unprotected, `v*` tags unprotected, no required checks | yes |
| INF-3 | High | Confirmed | Private vulnerability reporting disabled; SECURITY.md channel broken | yes |
| INF-4 | Medium | Confirmed | Immutable releases disabled | no (strongly recommended) |
| INF-5 | Medium | Confirmed | Dependabot alerts and security updates disabled | no (recommended) |
| INF-6 | Medium | Confirmed (design) | Library version goes public on tag push, before/independent of gates | no, if INF-2 done |
| INF-7 | Low | Confirmed | Actions policy: all actions allowed, SHA pinning not enforced | no |
| INF-8 | Low | Confirmed | GoReleaser binary installed with checksum only, signature skipped | no |
| INF-9 | Low | Confirmed | `1.26.x` matrix resolves to 1.26.8 on Windows, 1.26.9 elsewhere | no |
| INF-10 | Low | Hypothesis | Undraft without `--latest` may mark a backport patch as Latest | no |
| INF-11 | Low | Confirmed | OIDC/`contents: write` tokens reachable from all steps of `publish` | no |
| INF-12 | Low | Confirmed | Attestation verify instructions do not pin signer workflow/ref | no |
| INF-13 | Info | Confirmed | zizmor: cache-poisoning (snapshot job), self-repository, concurrency | no |
| INF-14 | Info | Confirmed | Test job may report `(cached)` results in later runs | no |
| INF-15 | Info | Confirmed | Fuzz loop masks `go test -list` failures | no |
| INF-16 | Info | Confirmed | Tool pins in CONTRIBUTING.md not cross-checked; Dependabot blind to them | no |
| INF-17 | Info | Confirmed | CHANGELOG has no `## [0.1.0]` section yet (validate-tag would refuse) | release step, expected |
| INF-18 | Info | Hypothesis | RC and final tag on one commit: RC binaries may embed the final version | no |
| INF-19 | Info | Confirmed | Fork PR approval policy is `first_time_contributors` | no |

---

## Findings

### INF-1 (Critical, Confirmed) `release` environment does not exist

- Files: `.github/workflows/release.yml:72-74` (`environment: name: release`),
  `docs/releasing.md:84-86,131-133,165-167,233-235`.
- Evidence: `gh api repos/dimchansky/fpgrowth-go/environments` →
  `{"total_count":0,"environments":[]}`.
- Impact: GitHub auto-creates a referenced environment on first use **with no
  protection rules**. The first tag push would run `publish` (contents:write,
  id-token:write, attestations:write) right after CI with no human approval,
  contrary to docs/releasing.md step 5 ("Approve the release deployment") and
  the workflow header comment. Because a tag push runs the `release.yml`
  from the tagged commit, a tag on a commit with an edited `release.yml`
  (e.g. `validate-tag` and the ancestry check removed) would also publish
  and produce a valid-looking attestation; server-side environment rules
  are the only thing that can stop that.
- Resolution (settings, no code): Settings → Environments → New `release`:
  required reviewer `dimchansky`; "Prevent self-review" off (single
  maintainer); "Allow administrators to bypass" **off**; Deployment branches
  and tags → "Selected branches and tags" → tag rule `v*.*.*` only (no branch
  rules). Verify afterwards with
  `gh api repos/dimchansky/fpgrowth-go/environments/release` (expect
  `protection_rules` with `required_reviewers` and `deployment_branch_policy`
  `custom_branch_policies: true`, and
  `.../environments/release/deployment-branch-policies` listing `v*.*.*`, type `tag`).
- Blocks v0.1.0: **yes**.

### INF-2 (High, Confirmed) No rulesets: main and tags unprotected, no required checks

- Evidence: `/rulesets` → `[]`; `/branches/main/protection` → 404 "Branch not
  protected"; `/rules/branches/main` → `[]`. Only collaborator: `dimchansky`
  (admin). `docs/releasing.md:236-239` requires both.
- Impact: (a) `validate-tag`'s "Tagged commit is on main"
  (`release.yml:50-57`) only means "someone pushed it to main", which needs
  no PR and no green CI; (b) `v*` tags can be created, moved or deleted by
  any push credential (stolen PAT, compromised machine); a moved tag after
  the Go checksum DB recorded it breaks every user (docs/releasing.md:142);
  (c) the library version is published by the tag alone (INF-6), so the
  only effective quality gate for the library is "main is protected by
  required checks", which is absent.
- Resolution:
  1. Branch ruleset "main" (target `refs/heads/main`, enforcement active,
     no bypass or admin-only bypass "for pull requests only"): restrict
     deletions; block force pushes; require linear history (optional);
     require a pull request (0 approvals is acceptable for a single
     maintainer); **require status checks** (strict / up to date) with the
     15 names listed in "Required status checks" below, source GitHub
     Actions (integration id 15368).
  2. Tag ruleset "release tags" (target `refs/tags/v*`): restrict creations
     (bypass: repository admin role only), restrict updates, restrict
     deletions, block force pushes.
- Blocks v0.1.0: **yes**.

### INF-3 (High, Confirmed) Private vulnerability reporting disabled

- Files: `SECURITY.md:26-28` instructs reporters to use
  `/security/advisories/new`.
- Evidence: `/private-vulnerability-reporting` → `{"enabled":false}`.
- Impact: the only documented reporting channel does not work; reporters
  either give up or open a public issue (which SECURITY.md forbids).
- Resolution: Settings → Code security → Private vulnerability reporting →
  Enable. Verify: endpoint returns `{"enabled":true}`.
- Blocks v0.1.0: **yes** (one click, and SECURITY.md is shipped publicly).

### INF-4 (Medium, Confirmed) Immutable releases disabled

- Evidence: `/immutable-releases` → `{"enabled":false,"enforced_by_owner":false}`.
- Impact: after publication, release assets and the release's tag can be
  replaced by any write credential; the "Do not re-run a successful
  release" rule (docs/releasing.md:247-254) is advisory only; GoReleaser
  `mode: replace` on a rerun would rewrite a published release.
- Resolution: Settings → General → Releases → "Enable release immutability".
  The draft-then-publish flow in `release.yml` is exactly the flow this
  feature expects (assets uploaded to the draft, then undrafted → locked;
  tag locked too). Update docs/releasing.md "Repository settings" to list it.
- Blocks v0.1.0: no, but strongly recommended before the first release
  (it cannot be retroactively applied to an already published release).

### INF-5 (Medium, Confirmed) Dependabot alerts and security updates disabled

- Evidence: `/vulnerability-alerts` → HTTP 404 (disabled);
  `security_and_analysis.dependabot_security_updates.status = disabled`;
  `/automated-security-fixes` → `{"enabled":false}`. `.github/dependabot.yml`
  (version updates) is present and correct, docs/releasing.md:244-245
  requires alerts and security updates.
- Impact: low today (the root module has no deps; `reference` has only
  `golang.org/x/mod`, `x/tools` indirect), but GitHub-advisory alerts for
  actions and gomod are not raised; govulncheck covers Go code only.
- Resolution: enable Dependabot alerts and Dependabot security updates
  (Settings → Code security). Secret scanning and push protection are
  already enabled.

### INF-6 (Medium, Confirmed, design) Library is released by the tag push itself

- Files: `release.yml` (gates run after the tag exists), `docs/releasing.md:150-185`.
- Impact: as soon as `vX.Y.Z` is pushed, anyone (or pkg.go.dev, a bot) can
  fetch it via proxy.golang.org and it is recorded in sum.golang.org forever,
  even if `validate-tag` or `ci` fail or the approval is denied. The release
  workflow therefore gates only the binary artifacts, not the library
  version. The docs present the gates as protecting "a release".
- Resolution: rely on INF-2 (required checks on main, so every commit on
  main is green before it can be tagged); add one sentence to
  docs/releasing.md "What the release workflow does": "The Go module version
  is public as soon as the tag is pushed; the workflow gates only the GitHub
  release and its binaries. Tag only a commit whose CI run on main is green."
  Optionally add to step 3 a check `gh run list --commit "$(git rev-parse HEAD)" --workflow CI --json conclusion`.

### INF-7 (Low, Confirmed) Actions policy permissive

- Evidence: `/actions/permissions` → `allowed_actions: all`,
  `sha_pinning_required: false`; workflow perms default `read`,
  `can_approve_pull_request_reviews: false` (both good).
- Resolution: enable "Require actions to be pinned to a full-length commit
  SHA"; set allowed actions to "selected": GitHub-owned actions plus
  `goreleaser/goreleaser-action@*`. Already reflected as optional in
  docs/releasing.md:240-243.

### INF-8 (Low, Confirmed) GoReleaser download verified by checksum only

- Evidence (CI log, job "GoReleaser snapshot"): "Checksum verified for
  goreleaser_Linux_x86_64.tar.gz" then "cosign not found in PATH, skipping
  signature verification". The checksum comes from the same release, so it
  only protects against transport corruption. Mitigation: the upstream
  release v2.18.3 is an immutable release (`immutable=true` via API).
- Resolution (optional): add `sigstore/cosign-installer@<sha> # vX` before
  the goreleaser-action step in `publish` (and the CI snapshot), so the
  action verifies the signature, or accept and note it.

### INF-9 (Low, Confirmed) Inconsistent Go patch version in the `1.26.x` leg

- Evidence: run 39004922968: `Test (windows-latest, Go 1.26.x)` → `go1.26.8`
  ("Found in cache @ C:\hostedtoolcache\windows\go\1.26.8"); ubuntu/macos
  1.26.x → `go1.26.9`. Cache keys confirm (`...Windows-x64-go-1.26.8...`).
- Impact: Windows tests a stale patch release; results vary with runner
  image updates.
- Resolution: in `ci.yml` test job `setup-go` add `check-latest: true`, or
  pin exact versions (`go: ['1.26.9', '1.27.2']`).

### INF-10 (Low, Hypothesis) Undraft may mark a backport as "Latest"

- File: `release.yml:120-124` (`gh release edit "$TAG" --draft=false`), no
  `make_latest` in `.goreleaser.yaml`.
- Hypothesis (not exercised; no releases exist): GitHub's default
  `make_latest` for a newly published non-prerelease is `true`, so a
  `v0.1.5` published after `v0.2.0` becomes "Latest". SECURITY.md says only
  the latest release is supported, so backports are unlikely.
- Resolution: `gh release edit "$TAG" --draft=false --latest=$(...)` computed
  by comparing with the highest existing tag, or document "no backports".

Related, also untested: `gh release edit <tag>` on a **draft** depends on gh's
draft-release fallback lookup. Failure mode is safe (the draft stays
unpublished); verify on the first RC.

### INF-11 (Low, Confirmed) Write/OIDC tokens visible to every `publish` step

- File: `release.yml:75-78`. `id-token: write` exposes
  `ACTIONS_ID_TOKEN_REQUEST_TOKEN/URL` to all steps, including `go run
  ./modcheck` (downloads `golang.org/x/mod` from the proxy, go.sum-verified)
  and GoReleaser (which runs `go build`, no hooks configured). Risk is small
  (all code pinned/verified).
- Resolution (optional hardening): split into `build` (contents:read; runs
  GoReleaser with `--skip=publish`, uploads `dist/`), `attest` (id-token,
  attestations), `release` (contents:write; `goreleaser release
  --skip=build`/`gh release create --draft` + upload + undraft). Not needed
  for v0.1.0.

### INF-12 (Low, Confirmed) Attestation verification not bound to workflow/ref

- Files: `.goreleaser.yaml:84-86`, `docs/releasing.md:208-209`:
  `gh attestation verify <file> --repo dimchansky/fpgrowth-go`.
- Impact: accepts an attestation from any workflow in the repo (e.g. a
  future workflow, or a modified `release.yml` on an unmerged tagged commit).
- Resolution: document
  `gh attestation verify <file> --repo dimchansky/fpgrowth-go --signer-workflow dimchansky/fpgrowth-go/.github/workflows/release.yml --source-ref refs/tags/vX.Y.Z --deny-self-hosted-runners`.
  `checksums.txt` itself is not an attestation subject (only the archives
  listed in it are); optionally add `subject-path: dist/checksums.txt`
  in a second attest step.

### INF-13 (Info, Confirmed) zizmor results

`zizmor 1.31.1 --persona=auditor` (online audits, gh token): 3 findings.
- `cache-poisoning` (low confidence) `ci.yml:322-331`: setup-go cache enabled
  in the GoReleaser snapshot job. Snapshot output is not published; the
  `publish` job uses `cache: false` (`release.yml:87`) and `validate-tag`
  `cache: false`. Caches are written only by main/tag-ref runs; PR caches
  are ref-scoped. Optional: `cache: false` in the snapshot job too.
- `self-repository` `release.yml:62`: style suggestion, ignore or adopt.
- `concurrency-limits` `vulncheck.yml`: add
  `concurrency: {group: vulncheck, cancel-in-progress: false}`.
`actionlint v1.7.12`: no findings (exit 0). No `${{ github.event.* }}` or
`github.head_ref` in any `run:`; `github.ref_name` is passed via `env:` only.

### INF-14 (Info, Confirmed) Tests may be served from the test cache

- `ci.yml:141-145` runs `go test` without `-count=1`; setup-go caches
  `GOCACHE` (which includes test results) keyed only on go.mod hashes, OS and
  Go version (7 cache entries exist on `refs/heads/main`, e.g.
  `setup-go-Linux-x64-ubuntu24-go-1.27.2-1e1d5e...`). Run 39004922968 had no
  cache ("Cache is not found" in all 15 jobs) and 0 `(cached)` lines, so
  every test executed; later runs, including the release gate on a tag
  (which restores main's cache), may print `ok ... (cached)`. Semantically
  valid per Go's cache rules, but "release gate ran the tests" is then not
  literally true. Optional: `-count=1` in the Test step, or `cache: false`
  in the release context (`if: github.event_name == 'push' && startsWith(github.ref,'refs/tags/')`).
  Difftest already uses `-count=1`.

### INF-15 (Info, Confirmed) Fuzz loop can skip a package silently

- `ci.yml:253`: `$(go test -list '^Fuzz' "$pkg" | grep '^Fuzz' || true)`
  masks a `go test -list` failure for one package; only `[ "$n" -gt 0 ]`
  is checked. The Test job would catch the compile error, so impact is nil
  in practice. Optional: assert the expected count (9 targets ran in run
  39004922968: FuzzMineItemsets, FuzzMineRules, FuzzCSVFile, FuzzParseArgs,
  FuzzStrtod, FuzzStrtol, FuzzMine, FuzzReader, FuzzItemBase_BuildBag).

### INF-16 (Info, Confirmed) Tool pin drift surfaces

- The lint job checks GOVULNCHECK_VERSION ci.yml == vulncheck.yml and the
  `.github/goreleaser-version` format, but CONTRIBUTING.md:27-28 hard-codes
  staticcheck v0.8.2 and govulncheck v1.9.0 (unchecked). GoReleaser is
  single-sourced (CONTRIBUTING reads the file). Dependabot cannot update
  env-var tool pins, `.github/goreleaser-version` or the toolchain line
  (documented in dependabot.yml). Consistency verified now: file `v2.18.3`,
  CI log "GoReleaser v2.18.3 installed", local `goreleaser --version`
  GitVersion v2.18.3; toolchain go1.27.2 in go.mod, reference/go.mod, CI.
- Optional: extend the pin check to grep CONTRIBUTING.md.

### INF-17 (Info, Confirmed) CHANGELOG not yet prepared for v0.1.0

- `CHANGELOG.md:13` only has `## [Unreleased]`; link refs at the bottom have
  only `[Unreleased]`. `modcheck notes v0.1.0` would fail, so a premature
  tag is refused by `validate-tag` before any write job (safe; note INF-6:
  the module version would still be public). Expected release step 2.

### INF-18 (Info, Hypothesis) RC and final on one commit

- `GORELEASER_CURRENT_TAG` (`release.yml:106`) plus the `metadata.json` tag
  check (`release.yml:107-115`) correctly pin GoReleaser's tag. The Go
  toolchain, however, stamps the binary's module version from VCS tags on
  HEAD; with both `v0.1.0-rc.1` and `v0.1.0` present it may stamp the
  higher one into the RC binaries (`go version -m`). Cosmetic; avoid by not
  tagging the final on the RC commit while the RC workflow is still to run.

### INF-19 (Info, Confirmed) Fork PR approval policy

- `/actions/permissions/fork-pr-contributor-approval` →
  `first_time_contributors`. Workflows are `pull_request` with read-only
  token and no secrets (0 repo secrets, 0 variables), so risk is runner
  abuse only. Optional: "all external contributors".

---

## Verified as correct (evidence)

- **Action SHA pins** (`gh api repos/<o>/<r>/git/ref/tags/<tag>`; all are
  lightweight tags pointing at commits): actions/checkout v7.0.1
  `3d3c42e5...` MATCH; actions/setup-go v7.0.0 `b7ad1dad...` MATCH;
  actions/upload-artifact v7.0.2 `cf430e03...` MATCH;
  goreleaser/goreleaser-action v7.3.0 `a4f02ab5...` MATCH;
  actions/attest-build-provenance v4.2.2 `4d101475...` MATCH. Each is the
  current latest release of its repo. (upload-artifact v7.0.2 and
  goreleaser-action v7.3.0 are 10 and 5 days old, i.e. newer than the
  7-day Dependabot cooldown; informational.)
- **Permissions**: top-level `contents: read` in all three workflows;
  `validate-tag`/`ci` read-only; only `publish` has contents/id-token/
  attestations write. Repo default workflow token is `read`.
- **Injection**: none; `github.ref_name` goes through `env: TAG`; the only
  `${{ }}` in `with.args` is `runner.temp`.
- **Triggers**: `pull_request`, `push: main`, `workflow_call` (CI);
  `push: tags v*.*.*` (release); `schedule`+`workflow_dispatch` (vulncheck).
  No `pull_request_target`, no `workflow_run`.
- **Concurrency**: CI group `ci-<workflow>-<pr|ref>` stays distinct from the
  caller's `release-<ref>` group under workflow_call (github.workflow is
  "Release"); release never cancels in progress.
- **Tag validation** (`reference/modcheck`, tests in `main_test.go`):
  canonical SemVer with `v`, pre-release allowed, build metadata refused,
  `vN≥2` requires `/vN` path; notes section required (RC falls back to base
  version); ancestry `git merge-base --is-ancestor $GITHUB_SHA origin/main`
  works with `fetch-depth: 0` (checkout fetches `refs/heads/*` into
  `refs/remotes/origin/*`; merge-base peels a tag object if needed).
  Effective only together with INF-1/INF-2 (the checks run from the tagged
  commit's own workflow file).
- **Draft → attest → publish ordering**: `.goreleaser.yaml:79-81`
  (`draft: true`, `replace_existing_draft: true`), attestation step before
  `gh release edit --draft=false`; any failure leaves an unpublished draft.
  A release is never public before its attestation exists.
- **Reproducibility**: `-trimpath`, `-ldflags=-s -w`, `CGO_ENABLED=0`,
  `mod_timestamp: {{.CommitTimestamp}}`, fixed archive owner/mode/mtime;
  `vcs.modified=false`, `vcs.revision=f7bb606...` embedded (buildvcs on,
  `/dist/` gitignored). `goreleaser check` passes (local v2.18.3).
  Two local snapshots (second with fresh GOCACHE, `umask 077`,
  `TZ=Pacific/Auckland`) produced identical `checksums.txt`, **and identical
  to the CI (linux/amd64 runner) snapshot checksums** of run 39004922968
  (`diff ci-checksums.txt snap/dist/checksums.txt` → empty).
- **Archives**: 6 (linux/darwin/windows × amd64/arm64; zip on Windows),
  each `fpgrowth_<ver>_<os>_<arch>/` with `CHANGELOG.md`, `LICENSE`,
  `PROVENANCE.md`, `README.md`, `fpgrowth[.exe]`, root:root, 0644/0755,
  mtime = commit date; no dev artifacts. LICENSE is MIT with both
  copyright lines (Borgelt 2004-2022; port 2026), consistent with
  PROVENANCE.md's upstream license section.
- **Module zip** (lint job log): omitted `.git` and `reference` (nested
  module), 1402 files, 2,203,204 bytes (limit 524,288,000), no INVALID.
- **Dependabot**: gomod for `/` and `/reference`, github-actions for `/`
  (covers `.github/workflows`), weekly, grouped, 7-day cooldown.

## Hosted CI evidence: run 39004922968

`gh run view 39004922968 --json jobs`: workflow CI, event `push`, branch
main, headSha `f7bb606643b2fdc10ffcc907271753b97892a4f7`, conclusion
success, **15 jobs**, all success.

Expected job count from ci.yml: lint 1 + vulncheck 1 + test (4 OS × 2 Go −
1 exclude [ubuntu-24.04-arm, 1.26.x]) 7 + coverage 1 + cross-build 1 +
fuzz 1 + reference (ubuntu, macos) 2 + goreleaser 1 = **15**. "16" is wrong
(it would be the test matrix without the exclude). Under release.yml the
total is 17 (validate-tag + 15 via workflow_call + publish).

Skipped steps (all by design): `Keep LF line endings on Windows` on 5
non-Windows test jobs; `Test with the race detector` on both Windows jobs
(`if: runner.os != 'Windows'`, so Windows has no race coverage); failure-only
upload steps in fuzz and both reference jobs. No other step skipped.

Paths actually exercised (from `gh run view --log`):
- Difftest ran, not skipped: `ok .../reference/difftest 65.516s` (ubuntu),
  `172.828s` (macos), after the `build.sh release` step; `modcheck` tests ok;
  `bench/compare` has no test files. (The silent-skip branch in
  `difftest_test.go:73-75` only triggers without a built reference and
  without `cc`, impossible after the build step.)
- Fuzz: "ran 9 fuzz targets for 20s each".
- Windows: both Windows jobs ran the full `go test -shuffle=on ./...`
  (all 11 packages `ok`, e.g. root 20.0s, internal/fpg 31.7s) and the
  clean-checkout check.
- Cross-build: 12 targets built and vetted; linux/386 `go test` ran.
- Toolchains: 1.27.x → go1.27.2 everywhere; 1.26.x → go1.26.9 (linux,
  macOS) but go1.26.8 (Windows), see INF-9. Pinned-toolchain jobs: go1.27.2,
  `GOTOOLCHAIN=local`.
- No caches restored (first run), no `(cached)` test results (INF-14).

## Repository settings (read-only, 2026-10-17)

| Setting | Current | Should be |
| --- | --- | --- |
| Environments | none | `release`: required reviewer, no admin bypass, tags `v*.*.*` only (INF-1) |
| Rulesets / branch protection | none / main not protected | main ruleset with PR + 15 required checks, no force push/deletion (INF-2) |
| Tag protection | none | tag ruleset `refs/tags/v*`: restrict create/update/delete (INF-2) |
| Immutable releases | disabled | enabled (INF-4) |
| Private vulnerability reporting | disabled | enabled (INF-3) |
| Dependabot alerts (`/vulnerability-alerts`) | 404 = disabled | enabled (INF-5) |
| Dependabot security updates | disabled | enabled (INF-5) |
| Secret scanning / push protection | enabled / enabled | keep |
| Actions allowed | all, SHA pinning not required | selected + require SHA pinning (INF-7) |
| Default GITHUB_TOKEN | read; cannot approve PRs | keep |
| Fork PR approval | first_time_contributors | optional: all external (INF-19) |
| Artifact/log retention | 90 days | ok |
| Collaborators | dimchansky (admin) only | ok |
| Tags / releases | none / none | — |
| Actions secrets / variables | 0 / 0 | ok (only GITHUB_TOKEN + OIDC) |

## Required status checks for the main ruleset

Exact check-run names as reported for `push`/`pull_request` runs of CI
(source: GitHub Actions, app id 15368):

```
Lint (gofmt, vet, staticcheck, tidy)
govulncheck
Test (ubuntu-latest, Go 1.26.x)
Test (ubuntu-latest, Go 1.27.x)
Test (ubuntu-24.04-arm, Go 1.27.x)
Test (macos-latest, Go 1.26.x)
Test (macos-latest, Go 1.27.x)
Test (windows-latest, Go 1.26.x)
Test (windows-latest, Go 1.27.x)
Coverage
Cross-build
Fuzz smoke
C reference + differential tests (ubuntu-latest)
C reference + differential tests (macos-latest)
GoReleaser snapshot
```

Do not require `govulncheck (main)` (scheduled workflow only). Under
`workflow_call` the names are prefixed `Quality gates / ...`, which is
irrelevant for the main ruleset. If the matrix changes, the ruleset must be
updated in the same PR, or a single aggregating job (`needs:` all jobs,
`if: always()`, fails unless all succeeded) can be added and required
instead, which makes the ruleset matrix-independent.

## Commands used (all read-only towards GitHub)

`gh api repos/dimchansky/fpgrowth-go{,/rulesets,/environments,/actions/permissions,/actions/permissions/workflow,/actions/permissions/selected-actions,/actions/permissions/fork-pr-contributor-approval,/actions/permissions/artifact-and-log-retention,/private-vulnerability-reporting,/immutable-releases,/branches/main/protection,/rules/branches/main,/automated-security-fixes,/collaborators,/tags,/releases,/actions/caches,/actions/secrets,/actions/variables}`;
`gh api -i .../vulnerability-alerts`; `gh api repos/<action>/git/ref/tags/<tag>` and `/releases/tags/<tag>`;
`gh run view 39004922968 --json jobs,...` and `--log`; `gh run list`;
`actionlint` (v1.7.12); `zizmor --persona=auditor` (1.31.1, scratch venv);
`goreleaser check`; `goreleaser release --snapshot --clean --skip=publish`
(v2.18.3, in throwaway clones `_audit/infra/snap{,2}`); `go version -m`;
`tar -tvzf`, `unzip -Z -l`.
