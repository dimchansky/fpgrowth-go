# Critic review (harness logic) — PR #41, issue #29

- Issue: #29 — Close three difftest harness gaps (count-only -Z check; M2 with order-dependent evaluation; R2 in count-only mode)
- PR: https://github.com/dimchansky/fpgrowth-go/pull/41 (branch `test/29-difftest-gaps`)
- Working dir: `~/.cache/fpgrowth-cycle/29/review-logic`
- Round 1 reviewed `26e99f0`; round 2 re-checked the fixes at `084c91d`.

## Round 1

- Head reviewed: 26e99f0e7c92cdf73c1c2aba36ce88e7e65d2cb9
- Decision: ACCEPT WITH CONDITIONS (Low only)
- Recorded by the orchestrator from the critic's final message.

### Soundness of "C with -s-1 equals Go"
- Go `derive` (internal/fpg/thresh.go) clamps body support to 1; rules use min(max(RuleSetMin(1),1), body); SMin is used nowhere else. C -s-1 gives smin=1, body=1, supp=ceil(1·conf)=1 for conf>0: same as Go.
- -tr -c0 still triggers M2 under -s-1; withMinSupportOne returns nil and the old path is kept (unit-tested).
- The oracle cannot hide a Go bug: Go's -s0 output is compared in full with an independent C run; M2 exemptions dropped; deviations recomputed at smin=1; Go's rerun result discarded.
- Upstream picks the default -v from the sign of -s (fpgrowth.c:2459-2460); difftest always passes -v (cases.go:475), so unaffected (docs: L1).
- hasZeroFrequency covers absent -R items, absent appearance items, weight-0-only items; over-flagging harmless, under-flagging falls back to main's behaviour.
- Nothing weakened: per-size rule with a measure is stronger than main's total-only check; the diffRecords reclassification applies only with a measure, a flagged deviation and C passing brute force (main's existing policy).

### Mutations (applied and reverted)
- checkStats disabled → M1 and R2 cases of TestHarnessGaps and 4 subtests of TestCheckBF_countOnlyStats fail.
- main's compareC condition → R2 case and TestCompareC_countOnlyStatsKind fail.
- M2 rerun disabled → M2 case fails.
- exact per-size check without a measure dropped → "a missing size fails" subtest fails.
- Ad hoc: reversed records, dropped last record, altered stderr on 67615 and a weight-0 case → FAIL (before the PR M2 excused order and stderr).
- Docs examples verified (C -s0 ≠ Go; C -s-1 byte-equal to Go for -d0, -d100).

### Commands
New tests pass; reference go test ./... ok (difftest 62 s, load 9-14); gofmt, vet, staticcheck clean.

### Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| L1 | LOW | confirmed | "port's result is upstream's with -s-1" holds only with the same explicit -v (default -v depends on the sign of -s). |
| L2 | LOW | confirmed | "which R2 excuses": with a measure any flagged deviation except O3 excuses it (deviations.go:113-123). |
| L3 | LOW | confirmed | README/doc.go omit that -tr -c0 M2 cases keep the old path. |
| L4 | LOW | suggestion | M2 cases without zero-frequency items where C reports support-0 sets could also be compared with C -s-1. |
| L5 | LOW | confirmed | rerunIn also reruns Go on the -s-1 copy and discards it (~95 cases/sweep). |

## Round 2

- Head reviewed: 084c91d440b1fc54c34dac7a783149d8d7911006
- Decision: ACCEPT (L1-L5 resolved, no new findings)
- Recorded by the orchestrator from the critic's final message.

### Soundness of comparing every M2 case with C -s-1
- M2 flagged for -s0, percentage -s with total weight 0, and rules with item threshold 0 (-c0).
- Outside -c0 Go's thresholds equal C's with -s-1: derive (internal/fpg/thresh.go) clamps body support to >= 1; rules set support max(RuleSetMin(1), 1) = 1 for conf > 0; percentage with total weight 0 → minSupp 0 → clamp 1. C -s-1: body 1, set support ceil(1·conf) = 1.
- Only other sign-of-s dependence in upstream main is the default -v (fpgrowth.c:2459-2460); cases always pass -v.
- No M2 case found where Go legitimately differs from C -s-1. -tr -c0 keeps the old path (withMinSupportOne nil; TestWithMinSupportOne).
- C -s0 crash cases now compared in full with C -s-1 (stricter than brute force only).

### Nothing weakened
cOnly set only on the C rerun copy; countByRerun and later reruns still run both programs; zlib_test zpair follows cOnly.

### Mutations (applied and reverted), all caught
(a) checkStats disabled → M1, R2 cases of TestHarnessGaps and TestCheckBF_countOnlyStats fail. (b) main's compareC condition → R2 case and TestCompareC_countOnlyStatsKind fail. (c) M2 rerun disabled → both M2 cases incl. "M2 without zero-frequency items" fail. (e) Go allowed in the -s-1 rerun → both M2 cases fail via the guard.

### Docs
L1 (same explicit -v, fpgrowth.c:2459-2460), L2 (any flagged deviation except O3 with a measure when C passes brute force), L3 (-c0 fallback) accurate in differences.md M2 row, CHANGELOG, README, doc.go.

### Commands
New tests pass; reference go test ./... ok (difftest 107 s, load 7-10); gofmt, vet, staticcheck clean.
