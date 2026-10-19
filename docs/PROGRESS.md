# Progress log

Durable project state so work can resume after a context reset. Keep this
short: current status, next steps, open problems. Details belong in the
linked documents.

## Current status (2026-10-17, v0.1.0 released)

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

- Future releases: the deferred items below, through pull requests with
  independent review, following `docs/releasing.md`.

## Open problems

- None blocking known. Post-v0.1.0 roadmap: issue #24. Still open there:
  `fpgpsp` and `-z`, faster
  dense maximal checks near the size limit (M5A-1), skipping perfect
  extensions that cannot affect a result (NEW-1, quadratic in upstream
  too), the remaining quadratic closed-check shape (NEW-3).
- Resolved after v0.1.0: RC-2 (Info, `docs/audit/rc1-verification.md`),
  mining returned nil instead of `ctx.Err()` when the context was
  canceled inside `OnStart` and nothing was left to mine; fixed by
  issue #14 (PR #26). TST-3 (Low, `docs/audit/testing.md`), the oracle
  shared the measure formulas with the implementation; the oracle now
  has an independent reference for every measure, with the ALG-1, ALG-2
  and ALG-6 deviations in strict expected-failure tables; issue #15
  (PR #27). ALG-1 and ALG-6 (statistical measures at large weights and
  near independence), issue #16 (PR #28): exact cross products,
  cancellation-free information, exact certainty and `-i`, an accurate
  Fisher evaluation for n > 170; the numeric contract is in
  `docs/differences.md`. ALG-2 (threshold rounding at extreme weights),
  issue #17 (PR #30): hybrid threshold contract in `internal/thresh`,
  identical to v0.1.0 below x = 2^52 and exact above it ("Threshold
  contract" in `docs/differences.md`). ALG-3 (CLI absolute thresholds
  rounded to doubles above 2^53), issue #18 (PR #PRNUM): absolute integer
  literals of `-s`, `-S` and `-F` are exact up to 2^63−1, larger ones are
  rejected (exit 11), every other spelling and all results up to 2^53 are
  unchanged (N3 in `docs/differences.md`).
- Triage (outside the roadmap): #25 (fpgpsp documentation, handled by
  #23), #29 (three difftest harness gaps). Follow-up noted in PR #28:
  FisherInfo still allocates for strong associations at n ≳ 1e15 (P-9,
  Low).
