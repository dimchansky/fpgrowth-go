# Progress log

Durable project state so work can resume after a context reset. Keep this
short: current status, next steps, open problems. Details belong in the
linked documents.

## Current status (2026-10-29, post-v0.1.0 cycle complete, not released)

- **Roadmap #24: all 19 tasks merged** into `main` with green CI, each
  with independent review (records in `docs/reviews/post-v0.1.0/`). No
  Critical or High finding is open.
- **Not released.** No tag and no version are chosen; the maintainer
  decides (`docs/releasing.md`). The new commands and options (`fpgpsp`,
  `-z`) make the next version a minor (v0.X.0).
- **Final integration audit:** first run on `e6d02b0` (2026-10-26; its
  regression A1 became #42, its documentation findings were fixed in
  PR #51), repeated on `37d33b3` (2026-10-28): correctness, API and
  packaging READY WITH CONDITIONS (the D7 sign-off; two Low documentation
  findings, fixed), performance NO REGRESSIONS (every comparable row
  9-49 % fewer instructions than v0.1.0, outputs identical to v0.1.0
  and C), and repeated again on `a5f548c` (2026-10-29, after #53 and
  #52): READY WITH CONDITIONS (the D7 sign-off; two Low documentation
  findings, fixed) and NO REGRESSIONS (all 52 CLI rows within ±0.64 %
  of the previous audit, every comparable row 9-49 % below v0.1.0,
  outputs identical; over-budget rows -15 to -17 %). Records:
  `docs/reviews/post-v0.1.0/final-audit-*.md`.
- **Sign-offs:** D7 was approved on 2026-10-30, D8 on 2026-10-28. No
  condition of the audits is open.
- **Gap to C (2026-10-30, `docs/benchmarks.md`):** on the 32 main
  configurations Go takes 1.37× C's wall time with output to `/dev/null`
  (geometric mean; 1.61× at the end of Stage 3) and 1.8-2.1× its
  instructions.

| Task | Issue | PR | Merge |
| --- | --- | --- | --- |
| RC-2: cancellation in `OnStart` with nothing to mine | #14 | #26 | `458abf0` |
| TST-3: independent oracle for the rule measures | #15 | #27 | `0fd4a0a` |
| ALG-1: statistical measures at large weights | #16 | #28 | `ee75321` |
| ALG-2: threshold rounding at extreme weights | #17 | #30 | `d4a2022` |
| ALG-3: exact large absolute CLI thresholds | #18 | #31 | `c398bb6` |
| M5A-1: maximal mining, small size limit, dense data | #19 | #33 | `ba63d14` |
| NEW-3: quadratic closed check on mixed-length covers | #20 | #35 | `e4cf7d0` |
| NEW-1: perfect extensions that cannot affect results | #21 | #36 | `47a0f32` |
| FEAT-2: zlib-compressed output `-z` | #22 | #37 | `d51eb19` |
| FEAT-1: `fpgpsp` (pattern spectra), also closes #25 | #23 | #38 | `e6d02b0` |
| difftest harness gaps | #29 | #41 | `8edb1d3` |
| maximal check on long light cover transactions | #34 | #43 | `bd39eec` |
| maximal sets with a small size limit slower than v0.1.0 (from #19) | #42 | #44 | `c66d052` |
| FisherInfo allocations at n ≳ 1e15 (P-9) | #40 | #45 | `325ad4b` |
| `fpgpsp`: no per-surrogate item base and FP-tree rebuild | #39 | #46 | `fde9e41` |
| `fpgpsp -ge` sampling | #47 | #49 | `95f2794` |
| reading and preparation pipeline (read once, D8) | #48 | #50 | `37d33b3` |
| `TestCancelTreeBuild` bounded by allocations, not wall time | #53 | #54 | `acd8d4a` |
| over-budget reread with byte-slice names | #52 | #55 | `a5f548c` |

What each task changed, with measurements and CI runs, is in the
progress log of issue #24, `CHANGELOG.md` (`[Unreleased]`) and the
review records.

## v0.1.0 (2026-10-17, released)

- **Development stages 0–4: closed** (`docs/reviews/`). Stage 1 faithful
  port; Stage 2 idiomatic API, sources, streaming; Stage 3 benchmarks and
  M5 (`docs/m5-investigation.md`); Stage 4 CI/CD and release automation.
- **Published:** https://github.com/dimchansky/fpgrowth-go (public,
  default branch `main`). First hosted CI run
  [39004922968](https://github.com/dimchansky/fpgrowth-go/actions/runs/39004922968)
  at `f7bb606`: all 15 jobs green, including Windows with Go 1.26 and
  1.27. No tags, no releases.
- **Phase 1 — independent audit: done.** Six fresh auditors, one per area
  (algorithms, M5, upstream differences and features, public API, tests
  and performance, CI and release), each in its own clean clone of
  `f7bb606`. Report: `docs/audit/independent-audit.md` (detailed reports
  and checker sources alongside). No mining correctness defect; M5 sound.
- **Phase 2 — remediation: done; Gate A accepted.** PRs #2–#6, each
  with an independent reviewer (`docs/audit/independent-audit.md` §6,
  §7). No Critical or High finding open.
- **Phase 3 — repository hardening: done; Gate B accepted**
  (`docs/audit/independent-audit.md` §5, independently verified).

- **Phase 4 — release candidates.** `v0.1.0-rc.1` (`8478311`): published,
  Gate D not accepted (release notes missing, RC-1;
  `docs/audit/rc1-verification.md`). `v0.1.0-rc.2` (`534855a`, with the
  fix #9): published, **Gate D accepted** — notes, assets, checksums,
  attestations, immutable pre-release, external install through the
  public proxy with Go 1.26 and 1.27 (`docs/audit/rc2-verification.md`).

- **Phase 5 — v0.1.0: released.** Tag on `e99628c`, approved by the
  maintainer, published as Latest and immutable; notes, checksums,
  attestations, proxy, `go get`/`go install` and pkg.go.dev verified
  (`docs/audit/release-v0.1.0.md`). ALG-1 ships as a documented known
  limitation.

## Next steps

- Maintainer: choose the version, release following
  `docs/releasing.md`.

## Open problems

- None blocking known.
- Above the 128 MiB budget of D8 the CLIs still read and tokenize the
  input twice (since #52 without string conversions): about 2.6× C's
  instructions on B×360. Removing it needs a larger budget or a spill
  format, which D8 rejected for now.
- Accepted test limits from the #53 reviews: `TestCancelTreeBuild` does
  not detect work after the cancellation that does not allocate (D1),
  and no test covers the build-loop cancellation checks
  (`buildCheckEvery`, F1). Not filed.
- ALG-1 shipped in v0.1.0 as a documented known limitation and is
  fixed for the next release (#16).
