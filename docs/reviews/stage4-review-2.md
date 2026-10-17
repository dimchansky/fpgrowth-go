# Stage 4 review — iteration 2 (full review, with final audit)

- **Stage / iteration:** Stage 4, iteration 2 (full re-review + verification of iteration 1)
- **Critic:** fresh independent reviewer agent (DevOps, Actions security,
  release engineering, final audit); worked only in fresh clones of
  `ba078e1`; nothing pushed or published.

## Evidence gathered by the critic (fresh clones)

| Check | Result |
| --- | --- |
| actionlint (3 workflows), pinned SHAs and tool versions via `gh api` | clean; all pins match tags and are latest releases |
| `go test -count=1 -shuffle=on ./...` on Go 1.27.2 and 1.26.9, with and without `-race` | all 11 packages pass |
| `git status` after tests, fuzz, reference build, difftest, benchmarks, coverage | clean |
| gofmt, vet, staticcheck, `go mod tidy -diff` (both modules) | clean |
| 12-target cross-build/vet; Windows test compilation and vet | ok |
| govulncheck (both modules) | no vulnerabilities |
| Fuzz smoke (9 targets) | 0 failures |
| C reference + difftest | 71 PASS, 0 FAIL |
| `modcheck zip` and an independent `x/mod/zip` check (`CreateFromVCS` vs `CreateFromDir` + `CheckZip`) | identical: 1 401 files, 2.2 MB, `reference/` and `.git` excluded |
| `modcheck tag` (13 tags) | correct accept/reject |
| GoReleaser check; two snapshots | ok; identical checksums |
| Tagged dry run v0.1.0 | 6 archives with LICENSE, README, CHANGELOG, PROVENANCE; trimpath, `CGO_ENABLED=0`, `vcs.modified=false` |
| Release-step simulation (valid, invalid, pre-release, off-main tags) | behaves as documented |
| README quick start (Go 1.26.9 and 1.27.2), 13 examples, `go install`, CLI vs C sample | exact / pass / byte-identical stdout and exit codes |
| Coverage | 97.0 % |

## Iteration-1 findings

C1, C2, M1, L1–L5, L7 fixed and verified; L6 fixed for README; I1–I3
accepted; I4 fixed.

## New findings

| ID | Severity | Title |
| --- | --- | --- |
| N1 | Medium | GoReleaser picks the release tag from git: with an RC and the final tag on one commit, the final run builds and publishes as the RC |
| N2 | Low | GoReleaser pin format check passes if any line matches |
| N3 | Low | `docs/PROGRESS.md` stale |
| N4 | Low | Relative links in CHANGELOG break in GitHub release bodies |
| N5–N7 | Info | `modcheck zip` reads the directory (CI uses fresh clones); OIDC token visible to all `publish` steps (trusted code only); re-running a successful release modifies it |

## Decision

**ACCEPT WITH CONDITIONS** — no Critical/High findings. Fix N1 before any
pre-release tag; N2–N4 opportunistically. Windows CI legs and the hosted
release path remain owner verification items.

## Fix record

- N1: `GORELEASER_CURRENT_TAG: ${{ github.ref_name }}` on the GoReleaser
  step plus a fail-closed "Check the released tag" step comparing
  `dist/metadata.json`; verified locally with `v0.1.0-rc.1` and `v0.1.0`
  on one commit → `v0.1.0`.
- N2: pin check requires exactly one line matching `vX.Y.Z`.
- N3: progress log updated.
- N4: CHANGELOG links made absolute.
- N7: documented in `docs/releasing.md` ("Do not re-run a successful
  release").
- N5, N6: accepted as Info.
- actionlint clean after the changes.

**Stage 4 closed.**
