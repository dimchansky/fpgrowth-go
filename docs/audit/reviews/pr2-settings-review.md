# Review: PR #2 (fix/release-workflow) and repository hardening

Reviewer: independent critic, read-only (GET-only gh api, no settings changed).
Date: 2026-10-17. PR head 8a39c93, base main f7bb606. Clone: scratchpad/review/pr2.

## Verdicts

- **PR #2: approve.** No blocking defects. One Low nit (silent failure path in
  the CONTRIBUTING pin check), the rest Info.
- **Settings: verified.** Everything listed in independent-audit.md section 5
  matches the live API. Required check names match exactly. One Low design
  limitation (the environment gate does not stop write-access actors) and one
  Info item about an undocumented ruleset parameter.

## Evidence (commands run)

- `actionlint` v1.7.12 (built into scratch GOBIN, shellcheck 0.x from Homebrew
  on PATH) on the PR tree: no findings, exit 0.
- `goreleaser check` v2.18.3 (scratch GOBIN): "1 configuration file(s)
  validated", exit 0.
- `gh pr checks 2`: all 15 checks pass (run 39011511410). `mergeStateStatus`:
  PR #2 CLEAN, PR #1 BLOCKED (1 fail, 10 pending), PR #3 BLOCKED (Fuzz smoke
  fail, 5 pending).
- The ruleset's required contexts, sorted, were diffed against the job names
  of PR #2's run 39011511410 (`/actions/runs/<id>/jobs`). Result: identical (15/15),
  integration_id 15368 (GitHub Actions).
- I simulated the publish decision under `bash --noprofile --norc -eo pipefail`
  (GitHub's `shell: bash`) in a scratch repo:
  - v0.1.0-rc.1 gives prerelease=true, latest=false.
  - v0.1.0 (same commit as the RC) gives latest=true.
  - With v0.9.9 and v0.10.0 present: v0.10.0 is latest=true, and v0.9.9 (or a
    v0.9.10 backport) is latest=false.
  - Higher RC tags (v1.0.0-rc.1) are ignored.
  - If the final tag is absent from the checkout and only RC tags exist, the
    step exits 1 without a message.
- actions/checkout@3d3c42e (v7.0.1) source: `fetchDepth <= 0` adds the refspecs
  `+refs/heads/*` and `+refs/tags/*` (`getRefSpecForAllHistory`). So the
  publish job has every tag, including tags pushed while it waited for approval
  (checkout runs after approval).
- actions/attest-build-provenance@4d10147 is a composite action that uses
  `actions/attest@508db95…` (SHA-pinned, GitHub-owned), which is a node24
  action with no further `uses`. Every action in the workflows is GitHub-owned
  or goreleaser/goreleaser-action, and all are SHA-pinned.

## Part 1: PR #2

| ID | Sev | Finding |
| --- | --- | --- |
| P-1 | Low | **CONTRIBUTING pin check fails silently in one case.** The line `s=$(doc staticcheck) g=$(doc govulncheck)` runs under `-eo pipefail`, and the exit status of the assignment comes from the *last* command substitution. If the govulncheck pin is missing from CONTRIBUTING.md, `grep` returns 1 and the step exits 1 before the `::error::` line, so there is no annotation (reproduced: rc=1, nothing printed). A missing staticcheck pin does print the error. The job still fails, so this is not a correctness hole. Fix: `s=$(doc staticcheck || true)` and `g=$(doc govulncheck || true)`. The check is strict by design: two different versions give a two-line `$s` and fail with a message, and `@latest` or `@2026.2.2` gives an empty value and fails. Today only CONTRIBUTING.md lines 27-28 carry pins (grep over all *.md and *.yml files). |
| P-2 | Info | **Latest/prerelease logic is correct for the requested edge cases:** rc tags, v0.10.0 vs v0.9.x (GNU `sort -V` is correct for numeric X.Y.Z), RC then final on the same or a later commit, and backports. Tags of other forms that match `v*.*.*` (`v1.2.3.4`, `+meta`, non-canonical) are refused earlier by validate-tag. The grep cannot return 1 on the final-tag path, because the triggering tag is always in the checkout (above). An RC-only repo never reaches the grep. The `-eo pipefail` exit in the simulation needs a tag absent from the checkout, which is impossible here because the tag ruleset forbids deletion. |
| P-3 | Info | **Latest is decided by tags, not by published releases.** If a higher final tag exists whose release was rejected or never published, a later lower release is not marked Latest, and Latest stays on an older release. This is acceptable, but could be one sentence in releasing.md. |
| P-4 | Info | **Fuzz listing change is correct.** `pkgs=$(go list ./...)` aborts under `-e` on failure, `if ! list=$(go test -list …)` reports the package, and `grep … \|\| true` handles packages with no fuzz targets. This closes INF-15. |
| P-5 | Info | **Vulncheck concurrency** (`group: vulncheck`, no cancel) is fine. The group name is unique in the repo; ci.yml uses `ci-…`. GitHub keeps one pending run per group, so a newer queued run supersedes an older queued one. That is harmless for a scan. |
| P-6 | Info | **docs/releasing.md matches the live settings line by line:** environment, both rulesets, immutable releases, Actions policy, fork approval, and the security settings (see Part 2). It omits two settings: `require_extra_approval_for_unattributed_changes` (S-2) and head-branch deletion. The goreleaser release body now carries `--signer-workflow`, `--source-ref` and `--deny-self-hosted-runners` (INF-12). |

## Part 2: repository settings (live API)

Confirmed as claimed:

- **Ruleset `main` (25292730):**
  - active, `~DEFAULT_BRANCH`, `bypass_actors: []`, `current_user_can_bypass: never`
  - rules: deletion, non_fast_forward, pull_request (0 approvals, thread resolution required), 15 required checks
  - `strict_required_status_checks_policy: false` (documented, step 4)
- **Ruleset `release tags` (25292731):** `refs/tags/v*` with deletion, non_fast_forward and update rules, no `creation` rule (so creation is allowed), no bypass actors. `v*` (fnmatch, `*` does not cross `/`) matches `v0.1.0-rc.1` and `v0.1.0`.
- **Environment `release`:**
  - `can_admins_bypass: false`
  - required_reviewers [dimchansky], `prevent_self_review: false`
  - deployment policy: custom, one tag policy `v*.*.*`. Under fnmatch semantics this matches v0.1.0-rc.1 (the third `*` = `0-rc.1`) and v0.1.0. Branches cannot deploy.
- **Actions:**
  - `allowed_actions: selected`, `sha_pinning_required: true`
  - `github_owned_allowed: true`, patterns `goreleaser/goreleaser-action@*`, `verified_allowed: false`
  - default token read, `can_approve_pull_request_reviews: false`
  - fork approval `all_external_contributors`
- **Security:**
  - private vulnerability reporting enabled
  - `/vulnerability-alerts` returns 204 (on); automated security fixes enabled, not paused
  - immutable releases enabled
  - secret scanning and push protection enabled
  - non-provider patterns and validity checks disabled (optional extras)
  - `delete_branch_on_merge: true`
- **Collaborators:** only `dimchansky` (admin).

Assessment:

- **Deadlock: none.** 0 approvals, and self-review is allowed on the
  environment. PR #2 (maintainer-authored) is CLEAN.
- **Dependabot:** its PRs come from same-repo branches, so fork approval does
  not apply. CI needs no secrets, the maintainer merges with 0 approvals, and
  action bumps stay on allowlisted, SHA-pinned actions.
- **release.yml permissions:** the job-level `contents`, `id-token` and
  `attestations: write` are explicit, so the read-only default does not limit
  them. Immutable releases allow the draft-then-publish flow: drafts stay
  mutable until published.
- **Merge state:** PRs #1 and #3 are BLOCKED as expected (failing or pending
  required checks). #2 is CLEAN because all 15 checks passed.

| ID | Sev | Finding |
| --- | --- | --- |
| S-1 | Low | **The environment gate binds only workflows that declare `environment: release`.** Anyone with write access can push a branch, or a `v*` tag on a non-main commit, carrying a workflow that requests `contents: write` without the environment. That workflow runs and can create a release with no approval. The same applies to a tag on a non-main commit with a modified release.yml from which validate-tag was removed: the tag push runs the tagged commit's workflow, and the tag policy `v*.*.*` does not check ancestry. No setting available to a personal repository closes this: tag rulesets cannot require "on main", and "required workflows" is an org feature. The effective control is that write access belongs to the solo admin only. A second, social control is the approver checking the commit. `gh attestation verify --signer-workflow … --source-ref refs/tags/vX` also accepts an attestation from such a modified release.yml. Recommendation: in releasing.md step 5, have the approver confirm that the run's commit equals the expected main commit before approving. Optionally tell verifiers to check that the tag's commit is on main (or to pin `--source-digest`). Keep the collaborator list at one. |
| S-2 | Info | **Undocumented ruleset parameter.** The pull_request rule has `require_extra_approval_for_unattributed_changes: true`. I found no GitHub documentation for this parameter. It does not block maintainer-authored PRs today (PR #2 is CLEAN), but its semantics are unverified. If it ever demands an approval on the maintainer's own PRs (for example, commits whose author email is not linked to a GitHub account), self-approval is impossible and those PRs would deadlock. The owner should check the field's meaning in the ruleset UI, or note it in releasing.md. |
| S-3 | Info | Required checks match the PR job names exactly. Renaming any ci.yml job or matrix value (for example `1.27.x`) makes every PR unmergeable until the ruleset is updated. releasing.md already says so. |
| S-4 | Info | PR #1 (Test macos 1.27.x) and PR #3 (Fuzz smoke) currently have failing checks; outside this review's scope. |

## Manual steps for the owner (optional, none blocking)

1. In the main ruleset UI, check what the "unattributed changes" option is
   (S-2). Keep it only if its meaning is understood.
2. Optionally add to releasing.md step 5: before approving, confirm the run's
   commit SHA equals `git rev-parse origin/main` at tagging (S-1).
3. Optional follow-up to PR #2: add `|| true` to the two `doc` substitutions (P-1).
