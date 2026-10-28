# Final integration audit — correctness, API/compatibility, packaging

- Main reviewed: e6d02b0 vs v0.1.0 (599 files, 63 non-test Go files, PRs #26 #27 #28 #30 #31 #32 #33 #35 #36 #37 #38)
- Date: 2026-10-26 (light work; no night sweep needed)
- Decision: READY WITH CONDITIONS
- Recorded by the orchestrator from the auditor's final message.

## Commands and results
- gofmt -l empty; go vet, staticcheck ok; govulncheck no vulnerabilities; CGO_ENABLED=0 go build ok; go mod tidy -diff ok; no cgo, no C files, no non-stdlib deps.
- go test -count=1 ./... ok (14 packages); go test -race ./... ok.
- linux/386 and linux/arm vet+build ok; linux/386 test -c internal/ruleval ok; CI on e6d02b0 green.
- reference: build.sh all zlib fpgpsp fpgpsp-fixed ok; vet, staticcheck ok; go test ./... ok (10,000 default difftest cases, 0 skips; TestZlib, TestFpgpsp pass).
- goreleaser check valid; snapshot --skip=publish ok: 6 archives, each with fpgrowth, fpgpsp, LICENSE, README, CHANGELOG, PROVENANCE; checksums 6 lines; binaries run.
- Integration campaigns (scratch, untracked):
  - internal/fpg: weights rescaled to totals 2^48..2^62 (straddling 2^52), % and absolute thresholds, max support, -m/-n, -F, -x/-u, 4 targets, 11 variants, sibling rows auto/force/at: 155,760 runs vs internal/oracle, 0 failures; also with -tags fpgforce.
  - root: TestRulesOracle with weights 2^40..2^62, 21 non-Fisher measures, filters, InvalidateBelowExpected, appearances, MinConfidence: 1,500 cases, 17,618 rules equal to oracle and exact values.
  - internal/thresh: 3M random (f, W): bit-equal to v0.1.0 below 2^52, exact floor/ceil above, monotone; 0 violations.
  - CLI -z vs C USE_ZLIB on combinations TestZlib does not cover: equal outputs and exit codes except documented O1/O2.
  - fpgpsp -D for s/c/m/g equals fpgpsp-fixed; negated border into fpgrowth -F -z works.
- API: root package exported declarations 185 = 185, none added/removed/changed; go.mod unchanged. CLI: only new -z usage line; no existing golden changed.
- CHANGELOG [0.1.0] byte-identical to the tag; historical records only gained notes; upstream C unchanged; every differences row naming a test points at an existing test (U9 latent, U10 preserved have none, acceptable).

## Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| A-1 | LOW | confirmed | README Limitations still says maximal mining "up to 2.6× slower" for dense maximal sets of maximum size; after #19 M5A-1 is 19.8 s vs C 39 s. |
| A-2 | LOW | confirmed | README says "There is no tagged release yet" (stale since v0.1.0). |
| A-3 | LOW | confirmed | docs/releasing.md names only fpgrowth as deliverable/CLI target and in the version note (l.24, 55, 256). |
| A-4 | LOW | confirmed | docs/PROGRESS.md status heading still v0.1.0; roadmap #24 completion and #39 missing. |
| A-5 | LOW | pre-existing | #29 difftest harness gaps; M2 row lacks the order-dependent evaluation note. |
| A-6 | LOW | pre-existing | #34 maximal check quadratic on NEW-3; P-9 FisherInfo allocations at n ≳ 1e15 (perf only). |
| A-7 | LOW | confirmed | #39 fpgpsp 1.8-3.3× C instructions; new feature; documented in D7. |
| A-8 | INFO | confirmed | fpgpsp -s-<n> stays double (no -w, supports < 2^53), harmless. |

No CRITICAL/HIGH/MEDIUM. Version constraint (docs/releasing.md, v0): new features (fpgpsp, -z) → next version must be a minor (v0.X.0); choice left to the maintainer.
Conditions: D7 sign-off; performance auditor's verdict; LOW doc fixes A-1..A-4 recommended.

## Resolution

A-1 to A-4 were fixed in PR #51; A-5 to A-8 by #29, #34, #40, #39 and #47. The audit was repeated on main 37d33b3 (`final-audit-2-correctness.md`).
