# Progress log

Durable project state so work can resume after a context reset. Keep this
short: current status, next steps, open problems. Details belong in the
linked documents.

## Current status (2026-10-17)

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

## Next steps

1. **Gate C — stop:** the maintainer must explicitly authorize the first
   public tag `v0.1.0-rc.1`. Then follow `docs/releasing.md`: changelog
   section, fresh-clone pre-tag checks, annotated tag, deployment
   approval, artifact and attestation checks, external consumer test.
2. **Gate E — stop:** separate explicit authorization for `v0.1.0`.

## Open problems

- None blocking known. Deferred past v0.1.0 (documented): exact
  arithmetic for the statistical rule measures at very large total
  weights (ALG-1, `docs/differences.md` N1), `fpgpsp` and `-z`, faster
  dense maximal checks near the size limit (M5A-1), skipping perfect
  extensions that cannot affect a result (NEW-1, quadratic in upstream
  too), the remaining quadratic closed-check shape (NEW-3), independent
  measure formulas in the oracle (TST-3).
