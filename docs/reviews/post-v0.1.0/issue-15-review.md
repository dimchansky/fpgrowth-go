# Critical review: PR #27 (issue #15, TST-3: independent measure oracle)

- Issue: #15 "Add an independent oracle for association-rule evaluation measures"
- PR: https://github.com/dimchansky/fpgrowth-go/pull/27, branch `test/15-independent-measure-oracle`
- Head SHA reviewed: `05e4773`
- Reviewer specialization: testing methodology and independent mathematical oracles; numerical analysis (exact/big arithmetic, special functions, floating-point error bounds); statistics of contingency-table measures.
- Scope: `internal/oracle` (`reference.go`, `bigmath.go`, `logratio.go`, `measure.go`, `rules.go`, `sets.go`, `diff.go`, `doc.go`, tests), wiring in root, `internal/istree`, `internal/fpg`, `internal/ruleval`, docs. Read-only review in a fresh clone (`scratchpad/review/pr27`); scratch copies for mutants and independent checks.

## Evidence examined

- Original issue #15 (scope 1–7, all acceptance criteria), PR body, full diff `origin/main...05e4773`.
- Production formulas `internal/ruleval/measures.go`, `fisher.go`, `gamma.go`, `internal/istree/report.go` (`-i`), `internal/report/report.go` (`LogRatio`), for the independence comparison and mutants.
- CI workflow `.github/workflows/ci.yml` (test, race, coverage, fuzz smoke with `FUZZTIME: 20s`) and `gh pr checks 27` (all green).

## Commands run (with results)

| Command | Result |
| --- | --- |
| `uptime` | load 8–13 (noisy machine) |
| `go test -count=1 ./...` | all ok (13.0 s wall) |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` | clean |
| `git diff origin/main...HEAD -- '*.go' ':!internal/oracle' ':!*_test.go'` | empty: no production code changed |
| `go test -race -count=1 . ./internal/{istree,ruleval,fpg,oracle}`, main vs PR, 2 rounds each | main 55.3 / 52.5 s, PR 56.2 / 54.6 s wall; istree +1.8 s, others within noise |
| `FPG_MEASURE_CASES=3000 go test -run 'TestMeasuresExtendedLarge\|TestMeasuresMatchReference' -v ./internal/ruleval` | pass; 638,734 tables, 15,961,378 comparisons, 906 ALG-6 mismatches; 55 s |
| Independent Python checker (`pr27own/tools/check.py`, `Fraction` + 130-digit `Decimal`, closed forms, entropies, binomial Fisher sums, own erfc) vs reference dump of 3,918 tables up to n = 2^63−8 (`TestZZDump`) | 87,322 comparisons, **0 mismatches**: all rational and log measures 0 ulps; p, t, g at most 908 ulps at p ≈ 1e-247 (within the stated (1+x)·2^-51); Fisher (n ≤ 4000) exact |
| Mutation harness `pr27mut/mut.sh` (oracle-based tests only: root `TestRulesOracle\|TestOracleEvaluatedItemsets\|TestKnownAPIFailures\|TestOracleLogSupportRatio\|Fuzz*` seeds, fpg `TestTree*Oracle\|TestLogRatioOracle`, istree `TestReport_*Oracle`, ruleval `TestMeasuresMatchReference\|TestKnownMeasureFailures`; no C goldens) | see the mutant table |
| Tolerance usage `TestZZRatio` (production vs reference, exhaustive n ≤ 20, boundary, 20,000 random n ≤ 2^24) | max \|impl−ref\|/Tol: rational ≤ 3e-5, z 0.03, p/t 1.3e-4, i 6.6e-4, Fisher 0.03, **g 0.998** (s=4133 b=56861 h=5615 n=79025, not exempted) |
| `TestZZALG6Oracle`, `TestZZALG6Random` (root, same comparison as `TestRulesOracle`) | an exactly independent rule fails the reference comparison; 27 of 3000 random generated datasets fail with InformationPValue |
| `FuzzMineRules` seeded with a 9-byte DB and 65,536 option words (`go test -run '^FuzzMineRules$' .`) | **FAIL** (seed#720, #1053, #1862, ...): `"b" <- {"a"}: eval want 1 ±1.46e-11, got 0.9999999588138879` |
| `go test -fuzz '^FuzzMineRules$' -fuzztime 60s .` | pass (320k execs; not hit by chance in that run) |

### Mutants (none detected by the C goldens because those were not run)

| Mutant | Change | Oracle-based tests failing |
| --- | --- | --- |
| TST-3 kulc | `reKulc` 0.5 → 0.5000001 | root, fpg, istree, ruleval (9 tests) |
| yates049 | Yates correction 0.5·n → 0.49·n | 8 tests in all four packages |
| certsign | certainty `n/p` → `-n/p` | 7 tests, all four packages |
| cvctden | conviction denominator `base` → `base+1` | 8 tests |
| liftbase | lift numerator `base` → `base-1` | 8 tests |
| yatesclamp | R9b clamp removed (upstream behaviour) | 8 tests |
| chi2p1e-9 | chi² p statistic ×(1+1e-9) | 7 tests |
| gp8e-11 | G-test `2·ln2` → `2·0.6931471805` (rel 8e-11) | 8 tests |
| ln2_6e-11 | `ln2 = 0.6931471806` (rel 6e-11) | 8 tests |
| kulc1e-9 | Kulczynski factor rel 1e-9 | 9 tests |
| fetchi2tie | FisherChi2 region bound `+1e-9` (tie excluded) | 4 tests (root, fpg, istree, ruleval) |
| invge2 | istree `-i` test `>` → `>=` | 7 tests (root, fpg, istree) |
| logratio | `-eb` with base+1 | root `TestOracleLogSupportRatio`, fpg `TestLogRatioOracle` |
| kulc1e-11 (sensitivity probe) | rel 1e-11 | none (below 2^-36 by design) |
| ln2_8e-13 (sensitivity probe) | rel 8e-13 | none (below 2^-36 by design) |

## Assessment by focus area

1. **Independence.** Confirmed. The reference is structurally different from production: per-cell Σ(O−E)²/E instead of the closed form; conviction as (1−h/n)/(1−s/b) in `big.Rat`; MI as φ² plus non-positive log1p remainders at 128 bits instead of `ln n + Σ/n`; Fisher as exact big-integer hypergeometric weights instead of log-gamma sums; p-values via `math.Erfc` instead of the incomplete gamma function. Constant conventions necessarily mirror upstream (from rules-eval §6.2). `TestReferenceImportGraph` is sound for in-module dependencies (it follows the transitive in-module imports of all non-test files, ignores build tags conservatively, and catches the root module path). It runs in default CI (`go test ./...`, no skip). No test can detect copied code; my independent closed-form check (0 ulps) covers that risk.
2. **Mathematical correctness.** All 25 measures and `-eb` agree with my definitions (above). The Fisher weight recursion P(x+1)/P(x) = (h−x)(b−x)/((x+1)(n−h−b+x+1)) and the `cmpInfo` fast-path bound (≤ 2^-50·Σ per F including float64(c) rounding; 2^-48 used) are correct. `aboveExpected` (128-bit) is exact. p-value bound: relative (1+x)·2^-51 holds (measured ≤ 908 ulps at x ≈ 1100). Near p ≈ 1 the reference is accurate to about 1e-16 absolute; the tolerance there is about 1.5e-11 absolute. Exact/convention labels are correct (U, ALG-5, R4, R5, R9b, P6). R6, R7 and R9a are not mentioned (see L1).
3. **Tolerances.** Every realistic mutant I tried with a relative effect of ≥ 6e-11 is caught without C fixtures. The detection floor is about 1.5e-11 relative. Production uses ≤ 3 % of the tolerance on all measures except g (99.8 %, ALG-6 class) and i, so the margins are honest and not flaky. The absolute floor `max(1, |v|)` hides total precision loss of small-valued measures (M1).
4. **Known-failure tables.** `TestKnownMeasureFailures` and `TestKnownAPIFailures` are strict both ways, and the entries are ALG-1, ALG-2 and ALG-6 only. The broader ALG-6 claim is real: production `InformationPValue` (s=16383, b=h=32767, n=65535) is 0.99688317625 against 0.99688320002, which my Decimal computation matches; for n=400,000 the relative error is 4.7e-8. It is not recorded in the repository docs (M2), and it leaks into the other oracle comparisons (H1).
5. **Wiring and CI.** The reference comparisons run in default `go test ./...` and `-race` in root, istree, fpg and ruleval, and in the fuzz targets' seed corpus and fuzz smoke. They have no ALG-6 handling outside `internal/ruleval` (H1). The race-job cost is about +1–2 s wall.
6. **No production change.** Confirmed. The docs are accurate except for M1 and M2.

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| H1 | HIGH | confirmed | The new reference comparisons in root, istree, fpg and the fuzz targets do not account for the known defect ALG-6. Any rule or set with an exactly independent table evaluated by `InformationPValue` fails: production gives 1 − O(1e-7), the reference 1 ± 1.46e-11. CI is green only because the fixed seeds miss such tables. Fuzzing and any seed or case-count change produce false failures, and the Fuzz smoke CI job is exposed. | A 9-byte `FuzzMineRules` input (`{a,b}×2 {a}×2 {b} {c}`) fails at seed#720/#1053/#1862 with `"b" <- {"a"}: eval want 1 ±1.46e-11, got 0.9999999588138879`. 27 of 3000 random `genTestCase` datasets fail the same `refMeasure` + `DiffRules` comparison. `isALG6` exists only in `internal/ruleval/reference_test.go`. |
| M1 | MEDIUM | confirmed | The tolerance `2^-36·max(1,|v|)` is absolute for values below 1, so ALG-1 precision loss of the small-valued measures Chi2 (n), Yates (y) and Information (i) counts as a match. The expected-failure table therefore omits n/y/i for the ALG-1 family, although N1 lists them. A #16 fix of these values cannot be verified through them. The doc claim (`doc.go`, `reference.go`) that "a float64 implementation that loses precision for large total weights … shows as a mismatch" is inaccurate for them. | At s=3k−2, b=3k, h=9k−2, n=9k with k=1e15: production n 5.63e-16 against 4.44e-16 (27 % off), y 44 % off, i 0 against 3.52e-16 (100 % off). All three `Matches` = true; only p/g flag the table. |
| M2 | MEDIUM | confirmed | The broader ALG-6 defect (the G-test p of nearly independent tables, n 1e3–1e5, relative 1e-10 to 5e-8) is exempted in the sweep by `isALG6`'s derived bound. It is documented only in the PR body and a test comment. `docs/audit/algorithms.md` ALG-6 still says "exact independence", there is no `differences.md`/PROGRESS note, and #16 is not informed. | Reproduced: n=65535 relative 2.4e-8, n=40000 3e-10, n=400000 4.7e-8; the reference agrees with an independent 130-digit computation. One non-exempt table already uses 99.8 % of the g tolerance. |
| L1 | LOW | confirmed | The issue asks to classify R6, R6/R9a and R7 explicitly. The reference mentions only R6, in a comment. The worst-value replacement (−i, empty sets and singletons) is a convention, yet values carry no label. | `grep R7\|R9a internal/oracle` finds nothing. `ruleValue` and `setEvaluator.value` return the worst value with tolerance 0 and no Kind. |
| L2 | LOW | confirmed | The sensitivity floor is about 1.5e-11 relative. Constant errors of 1e-11 or smaller (kulc ×(1+1e-11), ln2 to 11 digits) are undetected. Production uses ≤ 3 % of the tolerance on the rational measures, so a tighter rational tolerance (for example 2^-44) would add sensitivity without flakiness. Informational; the current choice is justified in the code. | kulc1e-11 and ln2_8e-13 mutants survive. `TestZZRatio` usage figures above. |
| L3 | LOW | confirmed | The `mutualInfo` comment says "without cancellation", but φ² and the remainder sum do cancel (MI can be ≪ φ², for example a perfect association with a tiny margin). This is harmless at 128 bits (relative error ≤ 1e-21 even at n = 2^63); the comment should state the bound instead. | `reference.go` mutualInfo doc. My independent check shows 0 ulps on all 3,918 tables. |

## Fixes requested

- **H1 (required):** make the reference-based comparisons outside `internal/ruleval` robust to the known ALG-6 defect, with the same strictness as the sweep. One option: the oracle (or a shared test helper) applies the `isALG6` bound to `InformationPValue` values, including set aggregates, and counts the exemptions. Another: compare `g` there only where the exemption cannot apply. In both cases, add the 9-byte input above as a regression seed (fuzz corpus or unit test) and check that it passes. Re-run the fuzz targets for several minutes.
- **M1:** use a relative tolerance, with a tiny absolute floor, for the measures whose float64 error is relative: Chi2 and Yates, and other rational measures where possible. Add the resulting n/y ALG-1 entries to `knownFailures`. For Information, either document that its tolerance is absolute (and that ALG-1 shows only through `g`), or add a relative check with ALG-1/ALG-6 entries. Correct the doc sentences.
- **M2:** record the broader ALG-6 finding in the repository: a dated status note under ALG-6 in `docs/audit/algorithms.md`, and N1 or PROGRESS. Ask the maintainer to note it on #16.
- **L1–L3:** a short doc addition classifying R6/R7/R9a as exact-value fixes and the worst-value replacement as a convention; optionally a tighter rational tolerance; fix the mutualInfo comment.

## Performance impact

Test-only. `go test ./...` wall time is unchanged within noise (13.0 s). Under `-race` the five affected packages take +1–2 s wall (istree +1.8 s). The extended sweep takes 55 s with `FPG_MEASURE_CASES=3000`. No library or CLI code paths are touched.

## Decision

**REJECT** (one High finding: H1). The reference itself is mathematically sound and independent: 0-ulp agreement with an independent exact computation, every realistic mutant caught, and a correct import guard. The wiring is not robust, though: the known ALG-6 defect makes the new comparisons fail on valid inputs that the fuzzers and future seeds can reach. After H1 is fixed and M1/M2 are addressed, a focused re-review of the wiring and the known-failure handling is sufficient.

---

# Re-review of 4f4a63c

- Head SHA reviewed: `4f4a63c115f771b3098e500706fb2860e3084568`. This is a full re-review against the original issue #15, using the same rules and specialization as the first review. Delta reviewed: `git diff 05e4773..4f4a63c`, 20 files.
- Load during the run: 6 to 100 (very noisy), so timing figures are indicative only.

## Commands run (with results)

| Command | Result |
| --- | --- |
| `git diff --name-only origin/main...HEAD \| grep -i 'zz_\|tmp\|scratch'` | empty. No temporary files in the PR (`zz_discover_test.go` and `zz_ratio_test.go` are not in it). |
| `git diff origin/main...HEAD -- '*.go' ':!internal/oracle' ':!*_test.go'` | empty. No production change. |
| `go test -count=1 ./...` | all ok, 13.2 s wall |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` | clean |
| `gh pr checks 27` | all 15 checks pass, including Fuzz smoke, race, arm64 and windows |
| My H1 reproducers (`TestZZALG6Oracle`, `TestZZALG6Random` with 20,000 datasets × {InformationPValue, Information}), using the PR's wiring (`DiffOptions{Defects: …}`) | pass. 0 of 40,000 runs differ. With strict `DiffOptions{}`, 643 of 6,000 differ, which is the expected strict behaviour and is used by the removal trigger. |
| Regression seed `testdata/fuzz/FuzzMineRules/5fc53b9aa57af1cd`, plus my 9-byte DB seeded with 65,536 option words | pass |
| `go test -fuzz` for 75 s each: root `FuzzMineRules` (1.86M execs), `FuzzMineItemsets` (618k), `internal/fpg` `FuzzMine` (100k) | all pass, no new corpus entries |
| `FPG_MEASURE_CASES=2000` and `=100000`, ruleval extended sweep | pass. 100000: 18,168,982 comparisons, ALG-6 within bound g 14,574 / i 134,862. The large-weight sweep has no non-ALG-1 mismatch. |
| `TestKnownMeasureFailures` | 106 ALG-1 and 90 ALG-6 entries reported; strict both ways |
| `FPG_REF_CASES=3000` root `TestRulesOracle`, `TestOracleEvaluatedItemsets` | pass. ALG-6 counted 56 and 13. |
| `go test -race` on root, istree, ruleval, fpg and oracle | 60.8 s and 58.0 s wall at load about 100. This is comparable to the earlier 52–56 s at load 8. The race job in CI passes (3 min). |
| Tolerance-usage probe `TestZZDefectUsage`: production deviation / max(Tol, DefectTol), over exhaustive n ≤ 30, boundary tables and 50,000 random tables up to 2^24 | rational ≤ 0.8 %; n, y ≤ 0.7 %; p, t ≤ 0.015 %; i 11 %; g 17.5 %; Fisher ≤ 5.9 %; **z 40 %** (s=13785586 b=13785752 h=13793039 n=13793230, n/(n−h) ≈ 72,000) |

### Mutants (oracle-based tests only, no C goldens)

| Mutant | Result |
| --- | --- |
| `reInfo` ×(1+1e-6) | caught in root (incl. `TestRulesOracle`, `TestOracleEvaluatedItemsets`), fpg, istree, ruleval (10 tests) |
| `reInfo` ×(1+1e-9) | caught, same 10 tests. Example on mined data: `{"b" "c"}: eval want 0.034510702883738045 ±1.96e-15, got 0.03451070291824866` |
| `reInfo` +1e-12 absolute | caught in 9 tests |
| G statistic ×(1+1e-6) | caught in 10 tests |
| G statistic ×(1+1e-9) | caught in 10 tests, e.g. `"a" <- {"c"}: want 0.05067190234699015 ±3.55e-12, got 0.05067190223148911` |
| `reChi2` ×(1+1e-12) | caught in 7 tests (the new relative 2^-44 tolerance) |
| TST-3 `reKulc` 0.5000001 | caught in 9 tests |

## Specific checks

- **(a) The ALG-6 bound is sound and not too wide.**
  - The bound is `alg6InfoErr(n) = 16·2^-52·ln n` (absolute, in nats) on the information. For the G-test p-value it is propagated as √(6δG/π) for G ≤ 2δG, and as δG·f(G−δG) otherwise, where f is the chi²(1) density. The density is decreasing, so evaluating it at G−δG bounds the slope over the whole interval. This is the correct first-order propagation.
  - Production uses at most 11 % (i) and 17.5 % (g) of the bound, so there is more than 5× headroom.
  - Information and G mutants of relative size 1e-6 and 1e-9 are caught on mined data, and so is an absolute +1e-12 on the information. The bound absorbs only deviations at the level of production's own rounding.
- **(b) The exemption is strict.**
  - `KnownDefect` is set only in `refInfo` and `refInfoPValue`.
  - Aggregates inherit it only from those values.
  - `Matches` ignores it, so the strict ruleval tables still see the deviation.
  - `DiffOptions.Defects` counts every exemption.
  - Comparisons without `Defects` stay strict.
  - `TestALG6Exemption` fails once ALG-6 is fixed.
  - No other measure or defect can pass through it: the chi², Kulczynski, Fisher and `-i` mutants are all still caught.
- **(c) The knownFragile band is principled and small.** It has 6 (table, letter) pairs. A comparison is fragile when its deviation lies between 0.25 and 4 times the tolerance, where a last-bit platform difference could flip the result. Each entry must stay inside that band, and every other comparison must be clearly decided (below 0.25× or above 4×).
- **(d) Fuzz and reproducers.** Both reproducers, the seeded variant and the regression seed pass, and each mining fuzz target ran for 75 s without failure.
- **(e) No production change.** Confirmed.
- **(f) CI runtime.** Acceptable. Default runtime is unchanged. The race job passes, and the local difference is within load noise.
- **(g) Docs.**
  - README, the ALG-6 note in `docs/audit/algorithms.md`, N1 and `doc.go` are accurate.
  - `ConvWorst` and the R6/R7/R9a classification are present.
  - One inaccuracy remains (L4 below).

## Status of the earlier findings

| ID | Status |
| --- | --- |
| H1 | fixed and verified. The ALG-6 exemption is shared, counted and bounded, and has a regression seed and a removal trigger. |
| M1 | fixed. n, y and i are now compared relatively (2^-44), and the ALG-1 entries for them are present (106 ALG-1 entries in total). |
| M2 | fixed. There is a dated note in `algorithms.md` and a pointer in N1. Commenting on #16 is left to the maintainer. |
| L1 | fixed: `ConvWorst`, and R6/R7/R9a classified as exact. |
| L2 | fixed. The tolerance is 2^-44, with 2^-36 for p-values, Fisher and certainty. |
| L3 | fixed. The precision is now 128+2·bitlen(n), and the comment states the bound. |

## New findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| L4 | LOW | confirmed | The `ratTau` comment says the certainty factor stays "below 5 %" of its tolerance. On a random table within the default generator's range (n < 2^24, n/(n−h) ≈ 72,000, beyond the 2^14 that `certValue` claims to cover), the measured usage is 40 %. That leaves only 2.5× headroom. It does not fail: `FPG_MEASURE_CASES=100000` passes. The comment should state the measured figure, and should state that tables with n/(n−h) above 2^14 at n < 2^24 are not excluded from the sweeps. | `TestZZDefectUsage`: z 0.404 at s=13785586 b=13785752 h=13793039 n=13793230 |

## Decision (re-review)

**ACCEPT.** All earlier Critical and High findings are fixed and verified. One new Low finding (L4, a documentation and margin note) is optional. No Critical, High or Medium findings remain.
