# Independent technical audit before v0.1.0

- **Baseline:** `f7bb606643b2fdc10ffcc907271753b97892a4f7` (published `main`,
  2026-10-17), hosted CI run
  [39004922968](https://github.com/dimchansky/fpgrowth-go/actions/runs/39004922968).
- **Auditors:** six fresh agents that took no part in the implementation or
  in the earlier stage reviews. Each worked in its own clean clone of the
  public repository at the baseline, modified no implementation file, and
  treated the existing documentation and review reports as claims to verify.
  The orchestrator consolidated their reports; it did not audit.
- **Detailed reports** (evidence, commands, run counts):
  [algorithms](algorithms.md), [M5](m5.md),
  [upstream differences and features](differences.md), [public API](api.md),
  [testing and performance](testing.md),
  [infrastructure and release](infrastructure.md). The auditors' checker
  sources are archived in [`reference/_audit/`](../../reference/_audit/).

Severities: **Critical** — correctness, security, data-integrity or
release-safety failure that makes the project unsuitable for publication;
**High** — must be corrected before release; **Medium** — should be fixed
when practical; **Low** — minor; **Info** — observation.

## 1. Verdict

No correctness defect was found in mining. Item sets, closed and maximal
sets, generators and rules matched independent definition-based brute
forces in every tested case for total transaction weight below about 1e13.
M5 (closed/maximal sets under a size limit) is mathematically sound; its
proof was re-derived and its implementation tested independently.

The blocking findings were one library panic on legal input (API-1), one
silent-wrong-result zero-value trap (API-2), stale README statements
(DIF-2/TST-1), and missing repository protections (INF-1 to INF-3: the
`release` environment, rulesets, private vulnerability reporting). The
repository settings were fixed directly (section 5); the code and
documentation fixes go through pull requests (section 6).

## 2. Evidence summary

| Area | Independent evidence | Result |
| --- | --- | --- |
| Algorithms (1.1) | Own CLI brute force (imports nothing from the repo): 56 000 cases all targets/variants/options + 23 000 with weights near 2^63; 32 000 vs C; 595 733 contingency tables per measure; 6 000 public-API cases; 5 300 wide-database runs; fuzzing ≈ 4.5 min | 0 mining failures; numerical findings ALG-1..3 at extreme weights |
| M5 (1.2) | Seed-678 minimized case reproduced on C and Go; proof reviewed; own brute forces: 22 203 datasets × 11 variants plus sparse runs, 241 815 runs; 4 planted bugs | 0 differences; 3 of 4 planted bugs caught by the auditor's harness, the 4th by the repo's unit tests |
| Differences (1.3) | All 55 documented deviations classified; most class-A rows reproduced on the C binary and checked by hand or brute force; difftest seed 424242 (3 000 + 30 large) | 0 failures; 33 strict upstream defects after de-duplication |
| Features (1.4) | 6.21 archive inventory vs Go | All CLI options except `-z`; all targets, variants, 25 measures |
| API (1.5) | Edge-case programs, 24-goroutine `-race` run, consumer program, go1.26.9 build | API-1 (High), API-2/3 (Medium), rest Low/Info |
| Tests and performance (1.6) | Root and reference suites incl. `-race`; coverage; difftest seed 1 (10 000 + 300 large); 9 fuzz targets; full fixture regeneration; 2 planted bugs; benchmark reproduction | Claims reproduced (table below); methodology findings TST-1..7, PERF-1..5 |
| Infrastructure (1.7) | Every action SHA checked against its tag; actionlint 1.7.12, zizmor 1.31.1; two local GoReleaser snapshots; hosted run inspection; settings read via API | Workflows sound; settings missing (INF-1..5) |

Reproduced claims (testing report, machine shared with other auditors;
load recorded per run):

| Claim | Reproduced |
| --- | --- |
| Coverage ≈ 97 % | 97.0 % total (97.0 % without `internal/oracle`) |
| Go/C geometric mean 1.60 | 1.596 (median-based), range 1.11–2.07 |
| M5 critic case 21 ms, C 236 ms | 19.8 ms, C 224.8 ms |
| M5 before: 34 s / 673 MiB | 29–30 s / 434–496 MiB (order of magnitude only; the docs disagree internally, PERF-3) |
| Cancellation within milliseconds | tests ≤ 8.8 ms; 4 M-transaction probe 54 µs–104 ms; API probe 11–187 µs |
| Golden fixtures produced by the C reference | all C-derived fixtures regenerate byte-identically (exit code of one crash case differs, never compared) |

### CI job count discrepancy

The publication report claimed 16 CI jobs; the API returns 15. `ci.yml`
defines 6 single jobs + 7 test-matrix jobs (4 OS × 2 Go − 1 exclusion) + 2
reference jobs = **15**; "16" was a counting error in that report. Under
`release.yml` the total is 17 (plus `validate-tag` and `publish`). In run
39004922968 all skipped steps were skipped by design (CRLF step off
Windows, race detector on Windows, upload-on-failure steps); difftest ran
(65.5 s ubuntu, 172.8 s macOS), fuzz ran 9 targets × 20 s, Windows ran the
full test suite.

## 3. Upstream deviations: classification

Of 55 documented deviation rows in `docs/differences.md`: **A** confirmed
upstream defect 36, **B** intentional improvement 4, **C** platform
difference 6, **D** new functionality 6, **E** incompletely verified 3
(R10, R11, C2). The per-row table is in [differences.md](differences.md)
§3.

The "about 40 upstream bugs" figure (used in conversation, not in the
repository) is not justified as stated. Merging different manifestations
of one root cause (M9→M4; I6, C4, I7→M7; C1→M8; C3→O7) and splitting
combined rows (R9, I5, O3) gives **33** confirmed, user-visible upstream
defects that are fixed and documented. Two more upstream crashes are
avoided but undocumented (T4 sort recursion, F5 pre-format overflow), and
five items are latent (R9c, R10, R11a, R11b, C2; R10 is a defect of the
project's patched C, not upstream). The project states the strict figure.

## 4. Findings

"Status" is the auditor's: *confirmed* (reproduced with a command) or
*hypothesis*. "Disposition" is the orchestrator's decision; PR numbers are
filled in section 6.

### Blocking v0.1.0

| ID | Sev | Status | Summary | Disposition |
| --- | --- | --- | --- | --- |
| INF-1 | Critical | confirmed | `release` environment missing: GitHub would create it unprotected on first use, so `publish` (contents/id-token/attestations write) would run without approval | Fixed in settings (§5) |
| INF-2 | High | confirmed | No rulesets: `main` unprotected, no required checks, no tag protection | Fixed in settings (§5) |
| INF-3 | High | confirmed | Private vulnerability reporting disabled although SECURITY.md relies on it | Fixed in settings (§5) |
| API-1 | High | confirmed | A callback error of a non-comparable type (e.g. `type Errors []error`) panics: `fpgrowth.go` compares it with `==` | Fix |
| API-2 | Medium | confirmed | A partial `TableFormat` (e.g. `{FieldSeparators: ";"}`) silently reads the whole input as one transaction | Fix: reject a non-zero format with empty `RecordSeparators` (`ErrInvalidOptions`) |
| DIF-2 / TST-1 | Medium | confirmed | README (Limitations, M5 text) and `docs/algorithm.md` §9 describe the superseded M5 approach and say performance was not measured | Fix docs |

### Not blocking

| ID | Sev | Status | Summary | Disposition |
| --- | --- | --- | --- | --- |
| ALG-1 | Medium | confirmed | Chi², Yates, G/information, Fisher-based, certainty and `-i` lose precision in float64 at total weight ≳ 1e12–1e13 (collapse by 1e14–1e16); not documented | Document a reliability limit now; exact arithmetic deferred (it would change last bits against C on normal data) |
| API-3 | Medium | confirmed | `Value` is NaN without evaluation, so `reflect.DeepEqual` of identical results is false | Keep NaN (upstream semantics), document |
| TST-2 | Medium | confirmed | `TestLargeFileStreaming` mines 3 sets and samples only the Go heap; CLI RSS on the same file at `-s0.5` is 4.9× the file | Document what the test shows; publish the RSS figure |
| INF-4 | Medium | confirmed | Immutable releases off | Fixed in settings |
| INF-5 | Medium | confirmed | Dependabot alerts and security updates off | Fixed in settings |
| INF-6 | Medium | confirmed | The library version is public through the module proxy as soon as the tag is pushed; the workflow gates only binaries | Document in `docs/releasing.md` |
| ALG-2 | Low | confirmed | Above W ≈ 2^52 the (1−ε) tolerances admit sets/rules just outside the bound | Document |
| ALG-3 | Low | confirmed | CLI absolute thresholds above 2^53 are parsed as float64 (as upstream); the library's `Count` is exact | Document |
| API-4 | Low | confirmed | `SpoolCSV`/`SpoolTable` copy the input before rejecting an invalid format | Fix |
| API-5 | Low | confirmed | Invalid item in `Options.Items` matches `ErrInvalidOptions` but not `ErrInvalidItem` | Fix |
| API-6 | Low | confirmed | Pass 2 with identical counts/weights/items but different data succeeds with wrong results (documented contract violation) | Keep documented |
| API-7 | Low | confirmed | `ItemOrder` has no `String()` | Fix |
| API-8 | Low | confirmed | Typed-nil `SourceFunc` panics | Fix |
| API-9 | Low | confirmed | `MaxSupport < MinSupport` of the same kind accepted | Fix |
| M5A-1 | Low | confirmed | Dense maximal near zmax: check dominates (99.7 s vs C 39.0 s, Go memory 111 MB vs 1 258 MB) | Document; optimization deferred |
| DIF-1 | Low | confirmed | Bug count overstated (33 strict) | Stated in §3 |
| DIF-3 | Low | confirmed | "All variants find the same sets"/"tuning never changes results" false with an evaluation threshold under `AggregateFirst` (same in C) | Fix docs |
| DIF-4 | Low | confirmed | Undocumented: T4/F5 crash fixes, size·weight saturation, preserved quirks T10, T21 | Fix docs |
| DIF-5 | Low | confirmed | With stdout closed Go exits 0 (runtime reopens fd 1 on `/dev/null`), C exits 4 | Document |
| DIF-6 | Low | confirmed | Wrong or missing test references in `differences.md` | Fix docs |
| DIF-7 | Low | confirmed | R2 "as documented" is ambiguous upstream; R10 is not an upstream bug | Fix docs |
| DIF-9 | Low | confirmed | `TestCancelTreeBuild` 54 ms vs a 50 ms bound under heavy load | Relax bound |
| TST-3 | Low | confirmed | The brute-force oracle takes rule measures from `internal/ruleval` (only C fixtures and difftest catch a measure bug) | Document; independent measure formulas deferred |
| TST-4 | Low | confirmed | "0 failures in 30 000+ cases" overstates effective checks (14 % both fail, many subset-only) | Fix docs |
| TST-5 | Low | confirmed | No committed tool reproduces the README memory table and 4 M cancellation figures | Document the commands |
| TST-6 | Low | confirmed | Tests allow 500/50 ms while README says "within 2 ms" | Fix docs |
| TST-7 | Low | confirmed | `golden-go/` fixtures are Go snapshots, README calls goldens C-produced; goldens generated on macOS only | Fix docs |
| PERF-2 | Low | confirmed | Geomean computed by hand; no significance test; headline excludes `-n` sparse rows (1.7–3.0× slower) | Fix docs |
| PERF-3 | Low | confirmed | M5 "before" figures inconsistent across docs | Fix docs |
| INF-7 | Low | confirmed | Any action allowed, SHA pinning not enforced | Fixed in settings |
| INF-8 | Low | confirmed | goreleaser-action verifies only the checksum (no cosign) | Accept; documented |
| INF-9 | Low | confirmed | Windows `1.26.x` resolved go1.26.8, others go1.26.9 (runner cache) | Accept (patch-level skew) |
| INF-10 | Low | hypothesis | Undrafting without `--latest` may mark a backport release as Latest | Fix in release workflow |
| INF-11 | Low | confirmed | OIDC and write token visible to every step of `publish` | Accept; documented (all steps are pinned, no untrusted input) |
| INF-12 | Low | confirmed | `gh attestation verify` instructions do not pin signer workflow/ref | Fix docs |
| NEW-2 | Medium | confirmed | Found during remediation ([t4-performance.md](t4-performance.md) §B): `-At -tc -n` on a long single chain is quadratic in Go only (100k items: 21.4 s, C 0.46 s); the M5 extension check copies the whole covering transaction per check | Fix (exact single-cover shortcut) |
| NEW-1 | Low | confirmed | Found during remediation (§A of the same report): on the T4 reproducer (long chain, `-n1`) mining is quadratic in upstream C and Go alike (100k items: 34.9 s vs 31.2 s CPU, identical output) | Document; optimization deferred |

Info (no action needed before v0.1.0, recorded in the detailed reports):
ALG-4 (any measure can fall either side of an exact `-d` threshold),
ALG-5 (preserved upstream measure quirks), ALG-6 (G-test p = 0.9999997 at
exact independence), ALG-7 (= L-2), M5A-2 (memory bound tighter than
documented), M5A-3 (the minimized case is also a C 6.21 B5 reproducer),
M5A-4 (two one-line proof gaps), M5A-5 (sparse vertical rows practically
unreachable), DIF-8, FEAT-1..4, API-10..16, TST-8, PERF-1, PERF-4, PERF-5,
INF-13..19.

### Feature completeness (1.4)

- **fpgpsp** (pattern spectrum from surrogate data, used for significance
  filtering via `-F`): `fpgpsp.c` is in the archive but needs `tars.c`,
  `pspest.c` and pthreads, which the archive does not contain. Deferred
  past v0.1.0; README wording corrected (it said the sources are missing).
- **zlib (`-z`)**: zlib-format compression of the item-set output file,
  off in upstream's default build. Cheap in pure Go (`compress/zlib`) but
  of low importance. Deferred.
- Library-only C features without a counterpart (size-ordered output,
  data-mode flags, `fpg_genpsp`) and the `ex/` gawk/R scripts: deferred,
  not program features.

### Decisions that are expensive to change after v0.1.0

| Decision | Outcome |
| --- | --- |
| `TableFormat` zero-value semantics | Changed now (API-2): partial formats without record separators are rejected |
| NaN `Value` without evaluation | Kept, documented |
| Result order and semver | Order is not covered by the API promise (already in `docs/releasing.md`); stated in GoDoc |
| `Source`/`iter.Seq2` without ctx; generic `Options[T]`; `Weight` 0 default; rule `MinSupport` on body support; sentinel errors with multi-unwrap; append-only enums; go 1.26 minimum | Kept |

## 5. Repository hardening applied (Phase 3)

Applied with `gh api` on 2026-10-17 by the orchestrator; to be verified
independently.

| Setting | Value |
| --- | --- |
| Environment `release` | Required reviewer `dimchansky`, self-review allowed (solo maintainer), admins cannot bypass, deployments only from tags `v*.*.*` |
| Ruleset `main` (default branch) | Pull request required (0 approvals, conversations resolved), 15 required checks from GitHub Actions, no force push, no deletion, no bypass actors |
| Ruleset `release tags` (`refs/tags/v*`) | No deletion, no update (moving), no force push; creation allowed; no bypass actors |
| Default `GITHUB_TOKEN` | Read-only (unchanged); Actions cannot approve PRs |
| Actions | Only GitHub-owned actions and `goreleaser/goreleaser-action`; SHA pinning required; fork PR workflows need approval for all external contributors |
| Security | Private vulnerability reporting, Dependabot alerts and security updates, immutable releases on; secret scanning and push protection on (unchanged) |
| Merging | Head branches deleted after merge |

## 6. Remediation record

Every pull request had an independent reviewer that did not write it;
review reports are in [reviews/](reviews/).

| PR | Content | Findings | Review |
| --- | --- | --- | --- |
| [#1](https://github.com/dimchansky/fpgrowth-go/pull/1) | This report, detailed reports, archived checkers | — | (evidence only) |
| [#2](https://github.com/dimchansky/fpgrowth-go/pull/2) | Release workflow: explicit prerelease/Latest on undraft, fuzz listing failures fail, vulncheck concurrency, pin check; `docs/releasing.md` | INF-6, INF-8, INF-10..13, INF-15, INF-16, INF-18 | [pr2-settings-review.md](reviews/pr2-settings-review.md): accept; P-1 and S-1 applied before merge |
| [#3](https://github.com/dimchansky/fpgrowth-go/pull/3) | API fixes with regression tests | API-1 (High), API-2..5, API-7..9, docs API-3/10/13/16, DIF-9 | [pr3-review.md](reviews/pr3-review.md): accept; delta [pr3-delta-pr5-review.md](reviews/pr3-delta-pr5-review.md): accept with condition D-1 (latency bound), applied before merge |
| [#5](https://github.com/dimchansky/fpgrowth-go/pull/5) | Regression tests for avoided upstream crashes and CLI rows | M10, O11, C10, C2, C7 (DIF-4/6 follow-up) | [pr3-delta-pr5-review.md](reviews/pr3-delta-pr5-review.md): accept |
| [#4](https://github.com/dimchansky/fpgrowth-go/pull/4) | Documentation corrections | DIF-1..7, ALG-1..4, TST-1..7, PERF-2/3, M5A-1..4, FEAT-1/2, NEW-1 | [pr4-review.md](reviews/pr4-review.md): approve with changes; re-review of the fixes: approve (R-1..R-3 applied before merge) |
| this PR | Changelog, progress log, `AGENTS.md`, this record | D-3, D-8 of the PR #4 review | — |

Repository settings (§5) were verified independently in
[pr2-settings-review.md](reviews/pr2-settings-review.md): every claimed
setting is live, the 15 required check names equal the CI job names, the
tag and environment patterns match `v0.1.0-rc.1` and `v0.1.0`, and no
configuration deadlocks a solo maintainer. A direct push to `main` was
not attempted (the orchestrator's tooling refused it as a merge without
review); enforcement evidence is the active rules on `main`
(`gh api repos/dimchansky/fpgrowth-go/rules/branches/main`) and the
`BLOCKED` merge state of every PR until its checks passed.

Residual risk accepted by the settings review (S-1): the `release`
environment gates only workflows that declare it, and a tag runs the
workflow file of the tagged commit. With a single collaborator the
control is that the approver checks the run's commit is the tagged
`main` commit (`docs/releasing.md`, step 5).

## 7. Acceptance

Pending the independent review of the fixes.
