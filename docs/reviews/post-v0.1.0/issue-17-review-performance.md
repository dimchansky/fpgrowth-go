# Critic review: PR #30 (issue #17, ALG-2): API, architecture, performance

- **Issue:** #17, "Correct support and confidence threshold rounding at extreme weights" (ALG-2)
- **PR:** https://github.com/dimchansky/fpgrowth-go/pull/30, branch `fix/17-threshold-semantics`
- **Head SHA reviewed:** `d59ed0fb67af39fd50f739f811be125252252ba3`
- **Base:** origin/main `ee75321`; also compared with v0.1.0 (`e99628c`)
- **Reviewer specialization:** Go API design and compatibility, performance engineering, architecture. A separate critic covers numerical correctness, so the arithmetic proofs in `internal/thresh` were not re-derived here. They were only exercised through mutation and probes.
- **Machine:** Apple M1 Max, darwin/arm64, go1.27.2. The machine was shared with other agents. `uptime` load averages during the benchmark rounds were 3.6 to 30 (logged per round, see Performance).

## Scope

1. Public API and behavior compatibility:
   - exported names and GoDoc (`Fraction`, `Count`, `MinConfidence`, `MaxSupport`, `Border`);
   - changes that library users observe at ordinary weights;
   - CLI goldens and error messages;
   - the CHANGELOG and semver note.
2. Architecture:
   - the new leaf package `internal/thresh`;
   - the rename of `fpg.Config.Conf` to `ConfFrac`;
   - the removal of `knownAPIFailures`;
   - duplication between the library and CLI paths, and readability.
3. Performance:
   - interleaved benchmarks against origin/main and v0.1.0;
   - how well the `Frac.Quick` fast path works;
   - inlining and allocations.
4. Test runtime in CI (default and `-race`) and the fuzz targets.
5. Docs consistency: README, `docs/cli.md`, `docs/api-design.md`, and `docs/differences.md` N2, L-5 and L-7.

## Evidence examined

- `gh issue view 17` (all acceptance criteria) and the PR body.
- `git diff origin/main...HEAD` (38 files):
  - `internal/thresh/thresh.go`
  - `internal/fpg/{thresh,config,tree}.go`
  - `internal/istree/{istree,report}.go`
  - `options.go`, `rules.go`
  - `cmd/fpgrowth/{options,run}.go`
  - `internal/oracle/{threshold,rules}.go`
  - every changed test
  - the CHANGELOG, README and docs
- `reference/difftest/{brute,cases}.go`, for the threshold formulas the difftest still uses.

## Commands run (with results)

| Command | Result |
| --- | --- |
| `gofmt -l .` | empty |
| `go vet ./...` | ok |
| `staticcheck ./...` | ok |
| `go test -count=1 ./...`, PR head vs main | all ok. Wall time 11.48 s vs 10.99 s; the new `internal/thresh` takes 2.9 s, but packages run in parallel |
| `go test -race -count=1 ./...`, PR head vs main | all ok. Wall time 57.75 s vs 57.52 s (root 54.8 s vs 54.6 s; `internal/thresh` 6.2 s) |
| `go test -fuzz '^FuzzFrac$' -fuzztime 30s ./internal/thresh` | PASS |
| `go test -fuzz '^FuzzMineRules$' -fuzztime 30s .` | PASS |
| `(cd reference && ./build.sh && go test -count=1 ./...)` | ok (difftest 46.1 s) |
| `go doc -all .` diffed between main and the PR | exported identifiers identical; only the `Fraction` doc text changes |
| `git diff --stat origin/main...HEAD -- testdata cmd/fpgrowth/testdata` | empty: no golden file was regenerated |
| `go build -gcflags=-m ./internal/thresh ./internal/istree` | `(*Frac).Quick` can be inlined and is inlined at `report.go:362`. `Meets` and `Min` are not inlined (expected) |
| CLI `-snan -Snan -Fnan -s-nan -S-nan -F-nan -sinf -s-inf -Finf -Sinf`, main vs PR binary | identical exit codes and messages (11 / 15 / 0), so `thresh.New` never sees a NaN from the CLI |
| GoDoc example probes (scratch test) | `Fraction(0.1)` of 10·2^59 = 2^59+32 ✓; `Fraction(0.30000000000000004)` of 10 = 3 ✓; `Fraction(1)` of 2^60+1 = W ✓; constant `33.3/100` = 0.333 vs run-time 0.33299999999999996 ✓ (supports the GoDoc caveat) |

### Mutation checks (scratch worktree, reverted afterwards)

- **Mutation 1:** `thresh.Frac.Min` replaced by the uncapped tolerance ⌈x·(1−ε)⌉.
  - Fails: `TestALG2API` (both subtests), `TestALG2HugeWeights`, `TestFractionMatchesCLI`, `TestSupportSaturation`, `TestExactCountThresholds`, and `TestALG2CLI` (`-s50`, `-F50`, `-c100`).
- **Mutation 2:** fast-path margin narrowed from 2^-50 to 2^-56.
  - Fails: `TestAgainstBig`, `FuzzFrac` seed corpus and `TestALG2HugeWeights`.
  - So an unsound `Quick` is caught.

## Performance

**Setup:**

- Test binaries were built once per version (PR, main, v0.1.0).
- Ten interleaved rounds ran with `-test.count 1 -test.benchmem`, in the order PR → main → v0.1.0 in each round.
- Benchmarks: root `MineRules|SuiteRules|MineItemsets`, istree `BenchmarkReport`, `cmd/fpgrowth` `BenchmarkCLI`.
- One or two outliers per benchmark were dropped by benchstat (n = 8 to 10).

**Load per round:**

| Round | Load averages |
| --- | --- |
| 1 | 5.2 |
| 2 | 16.8 |
| 3 | 7.6 |
| 4 | 30.0 |
| 5 | 7.9 |
| 6 | 4.6 |
| 7 | 3.9 |
| 8 | 3.6 |
| 9 | 3.8 |
| 10 | 4.2 |

Rounds 2 and 4 ran under heavy outside load, which explains the wide ± on some rows. Interleaving spreads that noise over all three versions.

### istree `BenchmarkReport` (the rule test in isolation)

| Case | main → PR | v0.1.0 → PR |
| --- | --- | --- |
| rules | 325 → 339 µs, **+4.54 %** (p=0.002) | +5.12 % (p=0.000) |
| rules_lift | 364 → 375 µs, **+3.06 %** (p=0.021) | +4.30 % (p=0.000) |
| sets_lift_avg | ~ | −0.12 % |
| closed_chi2_min | ~ | ~ |

This reproduces the PR's claim (+4.8 % / +3.4 %) within noise.

### Root package

- **Against main:** all 21 rows are `~`:
  - MineItemsets: 12 rows
  - MineRules internal and public: p=0.16 / 0.07
  - SuiteRules: 7 rows
- **Against v0.1.0:** significant rows are:
  - MineRules/internal +3.4 % (p=0.001)
  - SuiteRules sparse lift +2.9 %
  - MineItemsets int/frequent/internal +2.3 % and string/maximal/public +2.3 %

  The MineItemsets rows do not touch the rule test at all, and main → PR shows no difference on them. So the v0.1.0 drift belongs to earlier PRs and the noise of this run, not to this PR.

### CLI `BenchmarkCLI`

- Against main: all 12 rows are `~`.
- Against v0.1.0: only `closed/file` +1.3 % (unrelated to rules).

### Allocations

- allocs/op unchanged.
- alloc/op +0.00 %: `istree.Tree` grows by the 40-byte `thresh.Frac` in place of a float64.

### Fast-path effectiveness

Measured with a scratch counter in `Tree.rule`, mining the suite data through the public API:

| Data / thresholds | Rule checks | Exact fallback (`Frac.Min`) |
| --- | --- | --- |
| sparse s=0.1 % c=0.6 | 172,038 | 803 (0.47 %) |
| sparse s=0.05 % c=0.5 | 671,708 | 15,407 (2.29 %) |
| dense s=10 % c=0.9 | 76,912 | 0 |
| dense s=10 % c=0.8 | 93,163 | 0 |
| dense s=10 % c=1 | 64,012 | 0 |
| dense s=10 % c=0.75 | 93,223 | 3 (0.003 %) |

- The fallbacks are exact ties (supp = c·body, for example c = 0.5 with an even body). `Quick` can never decide a tie by construction.
- At about 4.5 ns per `Min` call, 2.3 % of checks is negligible.
- Threshold setup runs once per run.

**Verdict:** no end-to-end regression is measurable. A +3 to 5 % regression in the isolated reporting microbenchmark is acceptable for an exact contract, and it stays below the 5 % bar.

## Behavior-compatibility probes (library, W < 2^31)

These scratch tests in the root package compare the old library arithmetic with the new contract.

- **Old library arithmetic:**
  - `Fraction`: ⌈f·W·(1−ε)⌉ / ⌊f·W·(1+ε)⌋
  - `MinConfidence`: `percentOf(f,+1)/100·(1−ε)`
- **New:** `Fraction(f).minAbs`, the same contract the PR uses.

### `MinConfidence` decisions

The probe checked supp ∈ {⌈f·body⌉−1, ⌈f·body⌉, ⌈f·body⌉+1} for random body < 2^31:

| Fraction class | Changed / total | Direction |
| --- | --- | --- |
| uniform random | 0 / 600,000 | |
| k/b, b ≤ 10^5 | 0 / 599,976 | |
| 1−ulp | 0 / 400,000 | |
| one ulp off a 2-dp decimal | **220 / 597,966** | all tighter (old admitted, new rejects) |

### `Fraction` min/max

| Fraction class | Changed |
| --- | --- |
| k/b | 0 / 300,000 |
| count/W | 0 / 300,000 |
| uniform random | 0 / 300,000 |

### Realistic computed values

Probe: body = multiple of 100, supp = the exact decimal share.

| Computed value | Change |
| --- | --- |
| `k*0.01`, `1-k*0.01`, `k*0.1` | none |
| accumulated sums (`f += 0.01`, k times) | changes |

For example, f = 0.21000000000000005 (21 × 0.01):

- `Fraction(f)` of W = 100 was a minimum of 21 and is now 22.
- A rule with 21/100 now fails `MinConfidence: f`.

In the scan over k = 1..99 and W = 1..200,000:

- 21,999 minimum supports changed. All were tighter, and all equal ⌈shortest decimal · W⌉.
- In the exact-boundary probe, 15,139 confidence decisions and 10,562 minimum supports changed.

So the change always moves to the exact value of `f`'s shortest decimal. That matches the contract (the old double rounding absorbed about 2 ulps of accumulated error, the new tolerance absorbs exactly one relative ε).

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| P-1 | MEDIUM | confirmed | Changes to `MinConfidence` decisions at ordinary weights are neither documented nor tested. The PR documents and tests only `Fraction` min/max changes (N2: "309 changes of the minimum or maximum in 75,000 samples"). `TestLibraryThresholdsUnchanged` checks confidence only for "guaranteed" classes (`if !guaranteed { continue }`). The CHANGELOG says only that "`MinConfidence` is used as the fraction itself". For non-short-decimal confidences, rule decisions change, always toward fewer rules: 220 of 597,966 boundary decisions for one-ulp-off 2-dp values. For accumulated-sum values such as `0.21000000000000005` (21 × 0.01, typical for slider or grid-search code), the change is the rule of the exact decimal share (21/100 now rejected) and appears in 15,139 boundary probes. The library user is never told that confidence decisions can change, in which direction, or how to avoid it. | Scratch probes above. `options_test.go` `TestLibraryThresholdsUnchanged` (confidence loop limited to the `guaranteed` classes). `CHANGELOG.md` [Unreleased] Changed. `docs/differences.md` N2 and L-7 |
| P-2 | LOW | confirmed | The CHANGELOG "Changed" entry for library fractions is accurate but understates how visible the change is for computed fractions, and gives no direction and no workaround. Accumulated-error fractions (several ulps off a decimal) change about 0.11 % of all (k, W) minimum supports, always tighter. No semver or behavior-change note: the project is pre-1.0, so a minor bump is enough, but the release notes should say "behavior change" explicitly. | Probe above. `CHANGELOG.md` lines added under Changed |
| P-3 | LOW | confirmed | `Frac.Meets` is not used in production code. `Tree.rule` hand-expands it (`Quick`, then `Min`) because `Meets` is not inlinable. The soundness proof of the fast path lives on the doc of `Meets`, while production calls `Quick`. `BenchmarkMeets` therefore measures a function no production path calls. It is readable, but the code and the proof drift apart easily. | `grep` shows that `.Meets(` occurs only in `thresh.go:221` and tests. `report.go:357-368`. `-gcflags=-m` output |
| P-4 | LOW | confirmed | `reference/difftest/brute.go` (`absMinSupp`, `absMaxSupp`, `ruleSetMinSupp`, the `confAdj` rule test) still uses C's double formulas, while the port now follows the exact contract. This is harmless today: the difftest uses percentages with at most one decimal and weights ≤ 2^20 per transaction, so crossings would need x ≥ about 2^41. But it is undocumented, and a future large-weight difftest mode or arbitrary percentages would produce false R2 or deviation verdicts. | `brute.go:20-21, 266, 323-343`. `cases.go:196, 226` (percentage pools) |
| P-5 | LOW | hypothesis | The PR reports one difftest FAIL (case 10442, `-tm ... -q3 -s60`, R2) as pre-existing and "reported separately", but no open issue mentions it. #29 covers other R2 harness gaps. If it is not tracked, it can get lost. | `gh issue list` (open); the PR body "Tests executed" |

No CRITICAL or HIGH findings in this specialization.

- **Exported API:** unchanged (`go doc` diff).
- **CLI goldens:** unchanged.
- **Error messages:** unchanged. The changed internal message in `fpg.Config.validate` cannot be reached from the CLI or the library, because both validate first.
- **Rename:** `ConfFrac` makes stale callers fail to compile, as intended.
- **Leaf package:** `internal/thresh` imports only `math` and `math/bits`.
- **Library and CLI paths:** both funnel through `thresh.New(...).Min/Max`, so there is no duplicated arithmetic: `options.go` `minAbs`/`maxAbs`, `fpg` `minSupp`/`maxSupp`/`derive`, and CLI `borderSupp`.
- **`knownAPIFailures`:** removed cleanly. `TestALG2API` and `TestALG2HugeWeights` replace it, and both fail under mutation.
- **Docs:** consistent:
  - README thresholds paragraph and Limitations
  - `docs/cli.md` mapping table and N2 bullet
  - the dated note in `docs/api-design.md`
  - `docs/differences.md` N2, the "Threshold contract" section, L-5 and L-7
  - the `Fraction`, `MinConfidence` and `checkRange` GoDoc

  L-7 now correctly says that only `Evaluation.Threshold` still goes through `percentOf` (`measure.go:266`).

## Fixes requested

1. **P-1:**
   - In `CHANGELOG.md` (Changed), `docs/differences.md` N2 and L-7, state that `MinConfidence` values that are not short decimals can change rule decisions at any weight, always to the result of the value's shortest decimal, and in the probes always toward fewer rules.
   - Give an example, such as `0.1+0.11` / `0.21000000000000005` with 21/100, and a hint: pass the intended decimal, for example `math.Round(c*1e4)/1e4`.
   - Extend `TestLibraryThresholdsUnchanged`, or add a sibling test, so that confidence decisions for the non-guaranteed classes (random, one ulp off, accumulated sums) are allowed to change only to the shortest-decimal answer, as it already asserts for `Fraction`.
2. **P-2:**
   - Add "fewer results, never more" (if the numerics critic confirms the direction for library fractions below 2^52) and the workaround to the CHANGELOG entry.
   - Mark the entry as a behavior change for the next minor release.
3. **P-3:** move the fast-path proof to `Quick`'s doc comment, or reference it from `Tree.rule`. Alternatively, note in `Meets` that `Tree.rule` mirrors it for inlining.
4. **P-4:** add a comment to `reference/difftest/brute.go` saying that it applies C's double formulas, and why they equal the Go contract for the difftest's percentage pools and weights. Alternatively, switch it to the exact contract.
5. **P-5:** make sure difftest case 10442 (seed 11) is tracked in an issue, or under #29.

## Decision

**ACCEPT WITH CONDITIONS** (only MEDIUM and LOW findings). The condition is P-1: document `MinConfidence` behavior changes and cover them with tests. Performance:

- no measurable end-to-end regression against main;
- istree `Report` rules +4.5 % and rules_lift +3.1 % in isolation, as the PR claims;
- the fast path decides at least 97.7 % of rule checks on suite data;
- allocations unchanged;
- CI test time unchanged within noise.

---

# Re-review of d1b9070

- **Head reviewed:** `d1b90705ae2cc221d801d851f212ce5462a91c73`, one commit on top of `d59ed0f`: "Keep upstream's threshold arithmetic below 2^52". The diff covers 26 files, +972/−463.
- **Specialization:** API and compatibility, architecture, performance (unchanged).
- **Scope:** a full re-review of the delta and the whole PR against origin/main and v0.1.0.

## What changed (as read in the diff)

`internal/thresh` is now a hybrid contract. The branch is chosen by the exact x = t·N, compared with 2^52 by `cmpSwitch` in 128-bit arithmetic.

- **Minimum (`Frac.Min`):** for x < 2^52 it is upstream's double formula, ceil(t·N·(1−ε)) evaluated left to right (`f.t*float64(n)*(1-eps)`). From 2^52 on it is the exact ⌊x⌋.
- **Maximum (`Frac.Max`):** for x ≤ 2^52 it is the double formula floor(t·N·(1+ε)). Above 2^52 it is the exact ⌈x⌉.
- **Confidence (`Frac.ConfMin` / `Frac.Meets`):** for x < 2^52 it is `float64(s) >= float64(n)*RN(t·(1−ε))`, istree's own test. From 2^52 on it is s ≥ ⌊x⌋.
- **Rule set minimum (`Frac.RuleSetMin`):** for x < 2^52 it is the double formula, with n rounded toward zero first (the v0.1.0 `countThresholds` rule). From 2^52 on it is ⌊x⌋.
- **`Frac.Quick`:** for t ≤ 1 and N < 2^52 it is upstream's double test itself and always sure. Otherwise `Meets` decides. `Tree.rule` now calls `Meets` as the fallback.
- **`rules.go`:** `ConfFrac = percentOf(MinConfidence, +1)/100`, which is v0.1.0's mapping again.
- **Docs and tests:**
  - GoDoc of `Fraction`, `MinConfidence` and `checkRange` (the margin is re-derived for both branches).
  - CHANGELOG, README, `docs/cli.md`, `docs/api-design.md`, and `docs/differences.md` N2, the "Threshold contract" table, L-5 and L-7.
  - `reference/difftest/brute.go` comment.
  - Oracle `upstream.go`.
  - Tests: `TestThresholdsIdenticalBelowSwitch`, `TestMonotoneAcrossSwitch`, `TestBorderUnchanged`, and others.

## Commands run (results)

| Command | Result |
| --- | --- |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` | clean |
| exported declarations (`go doc -all .`, main vs PR) | identical; only doc text differs |
| `git diff --stat origin/main...HEAD -- testdata cmd/fpgrowth/testdata` | empty (no golden regenerated) |
| `go test -count=1 ./...`: main / PR (load about 5) | all ok. Wall 11.58 s / 12.43 s. Root 8.9 / 9.1 s, `fpg` 10.8 / 11.8 s, `thresh` 3.7 s |
| `go test -race -count=1 ./...`: main / PR | all ok. Wall 54.8 s / 59.9 s (+5 s). Root 54.2 / 57.0 s, `fpg` 38.0 / 41.6 s, `thresh` 12.4 s; the packages run in parallel |
| `FuzzFrac` 30 s, `FuzzMineRules` 30 s | PASS |
| `(cd reference && go test -count=1 ./difftest)` | ok (45.9 s) |
| `-gcflags=-m` | `(*Frac).Quick` can be inlined and is inlined in `Tree.rule` (`report.go:364`) |

### Previous P-1 probes, re-run at d1b9070

The confidence comparator now goes through the real path, `thresh.New(percentOf(f,+1)/100).Meets`. All changes are counted against v0.1.0 arithmetic.

| Probe | Changes |
| --- | --- |
| `MinConfidence`, uniform random | 0/600,000 |
| `MinConfidence`, one ulp off 2-dp | 0/597,966 |
| `MinConfidence`, k/b | 0/599,976 |
| `MinConfidence`, 1−ulp | 0/400,000 |
| `Fraction` min/max, k/b, count/W, random (W < 2^31) | 0 of 300,000 each |
| exact-boundary probe: `k*0.01`, `1-k*0.01`, `k*0.1`, running sums of 0.01 | 0 changes to confidence decisions and minimum supports |
| running-sum scan, k ≤ 99, W ≤ 200,000 | 0 changes (was 21,999) |
| new probe: W in [2^31, 2^31+2^56), fractions random / 3-dp / one ulp off 2-dp | 93,582 cases with x < 2^52: **0** differences from v0.1.0 (`Fraction` min/max and `MinConfidence` at s ∈ ⌈f·W⌉−2..+1). 306,418 cases with x > 2^52: minimum == ⌊x⌋ in every case |

### Mutation checks (scratch worktree, reverted)

| Mutation | Failing tests |
| --- | --- |
| M1: `Min` always takes the exact branch | `TestAgainstBig`, `TestPinned`, `TestThresholdsIdenticalBelowSwitch`, `TestBorderUnchanged`, `TestFractionMatchesCLI`, `TestLibraryThresholdsUnchanged` |
| M2: `Max` switches at x ≥ 2^52 instead of x > 2^52 | `TestAgainstBig`, `TestPinned` |
| M3: `Quick` uses the exact minimum instead of upstream's double test | `TestAgainstBig`, `TestLibraryThresholdsUnchanged` |

### Fast-path fallback on real data

The scratch counter in `Tree.rule` was re-applied. On the suite data (sparse with c = 0.6 and c = 0.5; dense with c = 0.9, 0.8, 1 and 0.75), there were **0 exact fallbacks** in 172,038 / 671,708 / 76,912 / 93,163 / 64,012 / 93,223 rule checks. This was 0–2.29 % at `d59ed0f`. A fallback now happens only for body supports of at least 2^52 or t > 1.

## Performance (10 interleaved rounds: PR → main → v0.1.0)

Binaries were rebuilt for the PR head. The main and v0.1.0 binaries are unchanged.

**Load per round:**

| Round | Load |
| --- | --- |
| 1 | 4.3 |
| 2 | 7.9 |
| 3 | 38.0 |
| 4 | 30.5 |
| 5 | 16.2 |
| 6 | 5.3 |
| 7 | 4.2 |
| 8 | 3.7 |
| 9 | 4.0 |
| 10 | 4.8 |

Rounds 3–5 ran under heavy outside load, hence the wide ± on some baseline rows.

### istree `BenchmarkReport`

| Case | main → PR | v0.1.0 → PR |
| --- | --- | --- |
| rules | 333 → 339 µs, ~ (p=0.11) | +3.87 % (p=0.003) |
| rules_lift | 364 → 373 µs, ~ (p=0.053, about +2.5 %) | +3.50 % (p=0.000) |
| sets_lift_avg | ~ | |
| closed_chi2_min | −12 % (baseline outlier, ±46 %) | +0.6 % |

This is consistent with the claimed +3.0 % / +2.2 % against main.

### Root package against main

- All rows are `~`, except MineRules/internal +1.05 % (p=0.036).
- SuiteRules: all `~`.
- MineItemsets: all `~`.

### Root package against v0.1.0

| Benchmark | Delta |
| --- | --- |
| MineRules/internal | +1.06 % |
| SuiteRules sparse c=60 | +3.1 % (p=0.034) |
| MineItemsets int/maximal/public | +2.3 % |
| string/frequent/public | −1.7 % |

These sit at the noise level of this machine. The item-set rows do not touch the changed code.

### CLI

- **Against main:** all `~`, except rules/file +2.6 % (p=0.015). That row writes the rules to a file, and its siblings rules/count (`~`, −0.7 %) and rules/devnull (`~`) did not move. I treat it as I/O noise.
- **Against v0.1.0:** frequent/file +2.0 % (no rules involved, noise).

### Allocations

allocs/op and B/op are unchanged (±0.05 %).

### Assessment

There is no regression above the noise floor. The remaining cost of the rule test (+2.5 to 3.9 % in the isolated reporting microbenchmark) comes from the extra `small && n < 2^52` guard and the `Frac` indirection. It is acceptable.

## Status of earlier findings

| ID | Status at d1b9070 | Evidence |
| --- | --- | --- |
| P-1 (MEDIUM) | **resolved** | No decision changes below 2^52 for any probed class, running sums and one-ulp-off values included. `MinConfidence` again goes through `percentOf` (v0.1.0). The probes above, plus `TestLibraryThresholdsUnchanged` and `TestThresholdsIdenticalBelowSwitch`, both of which fail under M1/M3. |
| P-2 (LOW) | **resolved** | The CHANGELOG "Changed" entry now says "Behavior change only for large weights", that every decision below 2^52 is unchanged bit for bit (fractions, percentages, `MinConfidence`), and gives the exact bounds above and the switch for maxima. "Fixed" ends with "Results for x below 2^52 do not change." Pre-1.0, so a minor bump is enough. |
| P-3 (LOW) | **resolved** | `Tree.rule` calls `Quick` and then `Meets`, so `Meets` is now a production path. `Quick`'s doc explains why it is exact (upstream's own test for t ≤ 1 and N < 2^52); no separate margin proof is needed any more. |
| P-4 (LOW) | **resolved** | `brute.go` comment: the C formulas equal the contract while x < 2^52, the difftest totals are below 2^31, and a large-total mode would need the exact branch. |
| P-5 (LOW) | **resolved** | Tracked in #29 (comment with the reproduction `DIFFTEST_SEED=11 DIFFTEST_ONLY=10442`). The PR body references it. |

## New findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| R-1 | LOW | confirmed | `internal/thresh` tests take 12.4 s under `-race` (3.7 s without), and the race job's wall time grows by about 5 s (54.8 → 59.9 s, load about 5). This is not a problem for CI, but the 200,000-case `TestAgainstBig` and similar loops could use a smaller count under `-race` or `-short`, if CI time ever matters. Optional. | Timed runs above |

No CRITICAL, HIGH or MEDIUM findings.

**Exported API:** unchanged (exported declarations identical).

**GoDoc:**

- `Fraction`, `MinConfidence` and `checkRange` describe the hybrid accurately.
- The `Fraction` examples hold, verified by probe:
  - 0.1 of 10·2^59 → 2^59+32;
  - 0.3 of 10 → 3;
  - `Fraction(1)` of 2^60+1 → W;
  - the constant 33.3/100 differs from the run-time division.
- "A larger fraction never gives a smaller bound" is covered by `TestMonotoneAcrossSwitch`. The proof is in the package doc; the numerics critic owns its verification.

**Docs consistency:** README, `docs/cli.md` (N2 bullet, switch for maxima stated), `docs/api-design.md` (dated note updated), and `docs/differences.md` (N2 row, "Threshold contract" table with the switch column, L-5, L-7) agree with the code and with each other.

**Architecture:**

- `internal/thresh` stays a leaf package (`math`, `math/bits`).
- The library (`minAbs`/`maxAbs`) and the CLI (`fpg.minSupp`/`maxSupp`/`derive`, `borderSupp`) share one implementation.
- The oracle carries an independent `upstream.go` plus a `math/big` exact branch.

## Decision (re-review)

**ACCEPT.** All earlier findings (P-1..P-5) are resolved with evidence. The only new finding, R-1, is optional and LOW.
