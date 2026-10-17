# Review: PR #9 (fix/release-notes, head 1b3cde0)

**Verdict: approve.** The root cause is confirmed in GoReleaser's source and
reproduced locally, the fix works, and the new check fails closed. CI: 15/15
checks pass, and the PR is mergeable (CLEAN).

## 1. Root cause (GoReleaser v2.18.3 source in the Go module cache)

- **`internal/pipe/changelog/changelog.go` `Skip`:** returns true when
  `ctx.Snapshot` is set, and otherwise when `changelog.disable` is set.
- **`Run`:** the only place that loads `--release-notes`
  (`ctx.ReleaseNotes = loadContent(ReleaseNotesFile)`).
  - With a notes file or template it returns immediately. So there is no
    `buildChangelog` (no git log) and no `dist/CHANGELOG.md`.
  - The changelog header and footer files are also not used, but this
    project uses `release.footer`, not those.
- **So with `disable: true`, `ctx.ReleaseNotes` stays empty.**
- **`internal/pipe/release/body.go`:** the body is `release.header` +
  `ctx.ReleaseNotes` + `release.footer`, and the footer comes from the release
  config. It is therefore still appended, which matches the rc.1 body
  (`"\n---\nVerify a download: …"` only, from `gh release view v0.1.0-rc.1`).
- **`use: git` adds no requirement for a previous tag.**
  - The warning "couldn't find any tags before" comes from `internal/pipe/git/git.go` and is only a warning.
  - It is printed whatever the changelog setting is: it already appeared in the rc.1 run log, which used `disable: true`.
  - `changelog.use` matters only in `buildChangelog`, which the notes file bypasses.
- **No other pipe reads `Changelog.Disable` or `Changelog.Use`** (grep over the source).

**Local reproduction.** I ran a scratch clone of the PR branch with a fake
annotated tag v0.1.0-rc.9 and
`goreleaser release --clean --skip=publish --verbose --release-notes=<modcheck notes>`.
GoReleaser binary v2.18.3, no token.
- **PR config:** "generating changelog", then "loading file …notes9.md" and
  "read 5999 bytes". No git log command runs, and the release succeeds.
- **Same clone with `disable: true`:** "skipped generating changelog".

The release body itself cannot be rendered without publishing. That step is
covered by the source reading of body.go above.

## 2. `gh release view` on a draft

In cli/cli, `pkg/cmd/release/view/view.go` and `edit/edit.go` both call
`shared.FetchRelease`. That function tries REST `releases/tags/{tag}` and
then falls back to `fetchDraftRelease`: a GraphQL `release(tagName:)` lookup
for a draft, followed by REST `releases/{id}`. The rc.1 run's
`gh release edit "$TAG"` on the draft succeeded in the same job with the same
token (`contents: write`). So `view` resolves the draft the same way.

## 3. Bash, injection and step order

I ran the step under `bash --noprofile --norc -e -o pipefail` (the runner's
shell line), with `gh` stubbed:

| Case | Result |
| --- | --- |
| Good body | pass |
| Footer-only body (the rc.1 case) | `::error::`, rc=1 |
| Notes file with only blank lines | rc=1, **silently** |
| Missing notes file | rc=2 |

The silent case happens because `grep -m 1` returns 1 inside the assignment and
`-e` exits before the error line. The `[ -z "$want" ]` branch is therefore
unreachable. The step still fails closed, and `modcheck notes` already refuses
an empty section. Nit: `want=$(grep … || true)` would make the message appear.

- **Here-string:** `grep -qF -- "$want" <<< "$body"` is safe. It has no pipe,
  `--` guards a leading `-`, and `-F` treats the line as a literal string.
- **Injection:** `TAG` and `GH_TOKEN` come through `env`, with no `${{ }}` in
  `run`.
- **Order:** tag check, then **notes check**, then attestation, then publish.
  A failure leaves a draft and no attestation, and a rerun replaces the
  draft. "Never public without attestation" holds.
- **Weak spot (Info):** the check looks only at the first non-blank line, so
  truncated notes would pass. It would also pass falsely if that line were
  `---` (the footer's first line). That is acceptable for its purpose.

## 4. Tools

- `actionlint` v1.7.12 (with shellcheck): no findings.
- `goreleaser check` v2.18.3: validated.

## 5. docs/releasing.md

Accurate. The two claims about GoReleaser match `Skip()` and `Run()`: the
changelog step is skipped in snapshot mode, and a notes file means no commit
log. The new comment in `.goreleaser.yaml` is accurate too.

## 6. CI

`gh pr checks 9`: 15 pass. `mergeStateStatus`: CLEAN (after waiting for the
pending jobs).

## Reviewer incident (disclosure)

My first simulation command used `cd <scratch clone> && …; goreleaser …`.
The clone failed (I had cloned from a local repo without that branch), so
`goreleaser release --clean --skip=publish` ran in the user's main checkout,
`<main checkout>`, on branch
docs/rc1-report.
- It printed "cleaning distribution directory", which means it deleted `dist/`
  (gitignored) if one existed. `dist/` is now absent.
- It then aborted at git validation ("git tag v0.1.0-rc.9 was not made
  against commit …"), so it built and published nothing.
- No tag was created there, `git status` is clean, and nothing was sent to
  GitHub.
- Only a local GoReleaser build output can have been lost; rerunning the
  build recreates it.
