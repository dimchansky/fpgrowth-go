# PR #30 correctness review (issue #17, ALG-2)

- Issue: #17 (read with its acceptance criteria). PR: https://github.com/dimchansky/fpgrowth-go/pull/30
- Head reviewed: d59ed0fb67af39fd50f739f811be125252252ba3 (branch fix/17-threshold-semantics)
- Reviewer: independent correctness critic (numerical semantics, exact/integer arithmetic, floating point). API/performance is covered by a second critic.
- Scope: internal/thresh vs the contract; every call site (fpg -s/-S, -F border, rule set min, -c/MinConfidence, HasCounts/derive, -o); Frac.Quick fast-path proof; ordinary-weight behavior change of library fractions; oracle independence; difftest/goldens; docs.
- Work dir: scratchpad/review/pr30c (clone `repo`; my test files are copied to `root_zcritic_test.go` (root package), `zcritic2_test.go` (root), `thresh_zcritic_test.go` (internal/thresh)).

## Commands run (results)

| Command | Result |
| --- | --- |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` | clean (no output) |
| `go test ./...` | all ok |
| `go test -race ./...` | all ok |
| `cd reference && ./build.sh && go test ./...` | ok (difftest 63.0 s, modcheck ok). `difftest.sh 50000` not rerun by me. |
| Own big.Rat reference vs `Frac.Min/Max` (internal/thresh `TestCriticBig`): 600k adversarial (subnormal t, 2^-k down to 2^-1099, nextafter(1,±), t in (1, 1e6], 1e300·rand, random bit patterns, k/d, ulp-off decimals) × N in {MaxInt64−[0,4096), 2^(52..62)±2, 2^k, random, negative}; 200k targeted near-integer x at 2^50..2^62; specials (0, −0, −1, −Inf, +Inf, MaxFloat64, smallest subnormal) × boundary N | 0 mismatches |
| `TestCriticQuick`: 19.5M (t, n, s = Min±3) checks of `Quick` sure-decisions and `Meets` | 0 wrong |
| `TestCriticQuickBand`: s placed at ⌊x(1±2^-50)⌋±4 (where the float decision flips), n in [2^50, 2^63), 32.2M sure decisions | 0 wrong |
| Mutation runs (worktree), see table below | all but one detected |

## Contract and implementation (focus 1)

The integer derivation is correct: Min returns q+1 iff r > ⌊X/2^52⌋ (⇔ ⌊x⌋ < x(1−ε)); Max returns q+1 iff 2^k−r ≤ ⌊X/2^52⌋; k ≥ 128 handled; k ≤ 0 shifts with saturation; q.hi≠0 saturates; inc saturates. Special values: t ≤ 0 / −0 → 0, +Inf → MaxInt64, NaN panics (callers validate: rules.go, fpg validate, CLI parse). Every call site uses thresh: `minSupp`/`maxSupp` (fpg), `derive` (both float and HasCounts paths; clamp [1, body]; -o keeps supp = body), `borderSupp` (v ≥ 0 incl. −0), `minAbs`/`maxAbs` (library), `istree.rule` (Quick + Min), CLI `ConfFrac = conf/100.0`, library `ConfFrac = MinConfidence`. The set minimum Min(c, bodyMin) is a valid pruning bound (Min nondecreasing in N). Both N2 reproductions, Fraction(1) of 2^60+1 = W, Fraction(0.1) of 10·2^59 = 2^59+32, Fraction(0.30000000000000004) of 10 = 3 and the F4 constant-expression caveat (33.3/100 constant = 0.333, run-time = 0.33299999999999996) verified.

## Quick fast path (focus 2)

Proof checked: a = RN(s), b = RN(n), hi = RN(t(1+2^-50)), product RN(b·hi): four relative roundings ≤ 2^-53 each, so a ≥ RN(b·hi) ⇒ s ≥ x(1+2^-50)(1−2^-53)^3/(1+2^-53) > x(1+2^-51) > x ⇒ s ≥ ⌈x⌉ ≥ Min; a < RN(b·lo) ⇒ s < x(1−2^-51) < x(1−ε) ⇒ s < Min. The restriction 2^-60 ≤ t ≤ 1 keeps lo/hi and b·lo normal and x ≤ n (no saturation); n = 0 gives b·hi = 0 (true, Min(0) = 0); t = 0 gives lo = hi = 0 (true); other t have ±Inf and always fall back (0·Inf = NaN compares false). No FMA can apply (single multiply then compare). No counterexample in 51M targeted checks; mutating the margin to 0 or 2^-53, or widening the range to all t > 0, is detected by TestAgainstBig/FuzzFrac/TestALG2HugeWeights.

## Ordinary-weight change of library fractions (focus 3)

Exact characterization. For x = t·W below 2^52, the contract result differs from v0.1.0's double formula only when an integer n lies within about 2.5·2^-52·x of x. At ordinary W this means the *intended* x is an integer and t's relative error δ against the intended rational exceeds the tolerance: new min = n+1 when δ > ε (old double rounding often still gave n), new max = n−1 when δ < −ε. CLI decimals (error ≤ ~ε from p/100) and correctly rounded k/d (≤ u) never qualify; values with δ in (ε, ~2ε] do. That class is not only "one ulp off a 2-dp decimal" but ordinary run-time arithmetic:

- `1 - a` for a 2-dp `a` (Sterbenz-exact, so the full error of RN(a) lands on a small result): 6 of 99 values change (`1-0.8` = 0.19999999999999996, `1-0.9` = 0.09999999999999998, `1-0.81`, `1-0.82`, `1-0.83`, `1-0.91`).
- `MaxSupport: Fraction(1-0.8)` (run time): for every W ≤ 20000 with 0.2·W integer, the new maximum is 0.2·W − 1 (4000 of 4000 W off the intended 20 %; 3989 of 4000 changed vs v0.1.0, old matched intent in all of them). W = 10: max 2 → 1; W = 5: max 1 → 0 (empty result). Same for `1-0.9` (1990/2000 changed).
- `MinConfidence: 1-0.83` (0.17000000000000004): a rule with support 17 of body 100 (confidence exactly 17 %) was accepted, is now rejected (min 18).
- In 200k samples of `1-a` and of nextafter(k/100, ±) (half of W multiples of 100), every one of the 4,884 resp. 10,814 changes moved the result away from the intended decimal while the old result equaled it (new==intent 0, old==intent 100 %).
- `a+b`, `k*0.01`, `a*b`, 3-dp `p/100`, `r.Float64()`: 0 (a+b, k*0.01, p/100, random) or 6/40k (a*b) changes.

Is it inherent? Yes for τ = ε exactly: the contract's tolerance absorbs a relative error of ε, v0.1.0's three roundings effectively absorbed about ε ± 1.5u (luck-dependent), so any value with error in (ε, ~1.75ε] flips. No exact fixed-τ contract reproduces v0.1.0 (it is not a function of exact x), but alternatives reduce the change sharply while keeping the ALG-2 fix (the cap s > x−1 / s < x+1 alone makes integer x exact and bounds the tolerance below one unit for any τ):

| exact contract, τ (capped) | `1-a`, W ≤ 3000 (297k): changed vs v0.1.0 | result ≠ intended decimal | ulp-off 2-dp (40k): changed |
| --- | --- | --- | --- |
| τ = ε (this PR) | 978 | 1650 | 2178 |
| τ = 1.5ε | 46 | 630 | 539 |
| τ = 2ε | 72 | 600 | 539 |
| v0.1.0 (reference) | 0 | 672 | 0 |

τ ≤ ~3.5ε keeps the existing 4ε checkRange proof sound; CLI 2-dp/3-dp and k*0.01 showed 0 changes vs C at W < 2^31 for τ up to 2.5ε (40k samples each); the F1 bound shifts down by at most a factor ~2. The only zero-change option is the issue's first suggested direction (v0.1.0/C double formula where x < 2^52, exact capped contract above), at the cost of a non-closed-form contract and of losing the F1 decimal corrections.

Severity: HIGH. The issue requires that results at normal weights do not change by accident and that any change is documented. The change is documented, but the documentation (N2 row, CHANGELOG, PR body) presents it as "rarely", "fractions with many significant digits / one ulp off a decimal", "to the exact result of its shortest decimal" (a correction). For the `1 - 0.8` class it is systematic (every W with integer intent) and moves results away from the user's intent. The "shortest decimal" assertion in `TestLibraryThresholdsUnchanged` is near-tautological for 17-digit decimals (it shows the tolerance did nothing, not that the result is right), and the test skips the confidence comparison for exactly these fractions. This is an unverified key assumption about ordinary-weight behavior on a public API, so it needs either a contract change or an explicit, accurately informed maintainer decision.

## Oracle independence (focus 4)

`internal/oracle/threshold.go` and `rules.go` import only math, math/big and tract; MeetsMin/MeetsMax/FracMinSupp/FracMaxSupp are big.Rat transcriptions of the definition (no 128-bit trick, no thresh/fpg/istree). C's formulas appear only in test helpers (`internal/fpg/thresh_test.go`, root tests). OK.

## Difftest and goldens (focus 5)

`reference` build + `go test ./...` ok; goldens unchanged and passing (no regeneration in the diff). I did not rerun `difftest.sh 50000`; the PR's 1-FAIL case 10442 (R2 classification, claimed pre-existing) was not verified by me.

## Mutation results

| Mutation | Detected by |
| --- | --- |
| Quick margin 0 / 2^-53 | TestAgainstBig, FuzzFrac seeds, TestALG2HugeWeights |
| Quick range widened to all t > 0 | TestAgainstBig, FuzzFrac |
| rule(): fallback replaced by false | TestRulesOracle, TestALG2API, TestGolden, ExampleRules |
| Min/Max: old double formula for n > 2^52 | TestALG2API/HugeWeights/CLI, TestFractionMatchesCLI, TestThresholdsHugeWeights |
| saturation removed | TestAgainstBig, TestPinned |
| derive: set min = body | TestRulesOracle, TestGolden |
| CLI conf ×(1−ε) | TestMinerConfig, TestALG2CLI |
| borderSupp back to C double formula | TestALG2CLI |
| library minAbs/maxAbs back to old | TestFractionMatchesCLI, TestALG2API, TestExactCountThresholds |
| **borderSupp Min → Max** | **not detected** by `go test ./...` (only integer-x -F cases) |

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| C1 | HIGH | confirmed | Systematic ordinary-weight change for natural run-time fractions with relative error in (ε, ~1.75ε], e.g. `Fraction(1-0.8)` as MaxSupport is 0.2W−1 for every W with integer 0.2W (W=5: 1 → 0, empty), `MinConfidence: 1-0.83` rejects exact-17 % rules; all changes move away from the intended decimal; docs/CHANGELOG/PR call it rare and a correction | `root_zcritic_test.go` TestCriticSystematic/TestCriticIntent/TestCriticTau; options.go Fraction, docs/differences.md N2, CHANGELOG |
| C2 | LOW | confirmed | -F border Min↔Max swap not caught by `go test ./...` | mutation M6; cmd/fpgrowth/thresh_large_test.go only -F50 at even W |
| C3 | LOW | confirmed | `TestLibraryThresholdsUnchanged` "exact for shortest decimal" check is near-tautological, and the MinConfidence comparison skips non-"guaranteed" fractions (where C1 occurs) | options_test.go |

No issue found in internal/thresh arithmetic, the Quick proof, saturation/special values, call-site wiring, oracle independence, checkRange soundness, goldens or difftest.

## Fixes requested

1. C1: maintainer decision with accurate data. Either (a) widen the tolerance (τ = 1.5ε or 2ε, still capped below one unit; re-run the sweeps and checkRange proof), or (b) adopt the issue's hybrid (old double formula for x < 2^52), or (c) keep τ = ε but rewrite N2/CHANGELOG/GoDoc to state the class precisely (run-time values with relative error above 2^-52, e.g. `1-0.8`, `1-0.9`; systematic, not rare; off-by-one from the intended decimal, not a correction) and add pinned tests for `Fraction(1-0.8)` and `MinConfidence: 1-0.83`.
2. C2: add a -F case with non-integer x (e.g. `-F33` of W = 10 at ordinary weight, or at W ≈ 2^62 with odd x).
3. C3: compare MinConfidence for all generated fractions; replace the shortest-decimal assertion by an intent-based statement or document what it shows.

## Performance impact

Not assessed here (second critic). Quick is O(1) floats with integer fallback only near the boundary.

## Decision

REJECT (one HIGH: C1). The implementation matches its contract exactly; the rejection concerns the contract's ordinary-weight effect on public `Fraction`/`MinConfidence` inputs and its inaccurate documentation, which needs a maintainer decision or a contract refinement.

---

# Re-review of d1b9070

- Head reviewed: d1b90705ae2cc221d801d851f212ce5462a91c73 (one commit on top of d59ed0f: "Keep upstream's threshold arithmetic below 2^52"). Full correctness re-review against the original issue #17.
- Contract now: hybrid, branch decided by exact x = t·N. Below 2^52 (maxima: x <= 2^52) upstream/v0.1.0 double formulas; library MinConfidence via percentOf(f,+1)/100 as in v0.1.0; rule set min via v0.1.0 countThresholds arithmetic (RuleSetMin, N rounded toward zero). At/above the switch: ⌊x⌋ (minima, confidence, set min), ⌈x⌉ (maxima, strictly above 2^52).
- New probe files: scratchpad/review/pr30c/probe_test.go.txt (run identically in the v0.1.0 tag worktree and at head), thresh_zcritic2_test.go.txt.

## Commands run (results)

| Command | Result |
| --- | --- |
| gofmt -l, go vet, staticcheck | clean |
| go test -count=1 ./... ; go test -race -count=1 ./... | all ok |
| reference ./build.sh + go test ./... | ok (difftest 49.3 s) |
| Probe vs **v0.1.0 tag** (same test file in both trees): 907 fractions (`1-a/100` for a=1..99 at run time, running sums of 0.01 and 0.1, nextafter(k/100,±), k*0.01, a/100+b/100, random) × (all W ≤ 20000 + 20,000 random W with x < 2^52), minAbs/maxAbs digests | **identical** for all 907 |
| Probe vs v0.1.0, end to end through CollectRules: 15 MinConfidence values (1-0.83, 1-0.8, 1-0.9, 1-0.81, nextafter(0.17,1), nextafter(0.26,1), nextafter(0.3,0), 0.3, 0.1+0.2, 0.7, 0.1*7, 1, 0.5, 0.33, nextafter(1,0)) × every (body ≤ 110, supp ≤ body) | **identical** decisions (e.g. 1-0.83: 5122 accepted in both) |
| CLI new vs v0.1.0 binary: `-S3.091`/`-s3.091` at W = 833815673989 (the former F1 change) | identical; ALG-2 reproductions fixed (`-c100`: only `x <- y`; `-s50`: only `b`) |
| Above switch vs my big.Rat reference (1.5M adversarial t × N, 566,758 with x ≥ 2^52): Min, ConfMin, RuleSetMin = max(⌈x(1−ε)⌉,⌊x⌋); Max (x > 2^52) = min(⌊x(1+ε)⌋,⌈x⌉); below-switch Min/Max = literal C formula | 0 mismatches |
| Switch walks: 317 thresholds (1, nextafter(1,±), 1/2^k, 0.3, 0.1, 1/3, 0.7, 0.99, 1.5, 3.7, 100, 300 random) × N covering x ∈ [2^52−2^12, 2^52+2^12] (stepped + dense ±64 at the switch); 2000 N × 128 consecutive t across the switch | Min, Max, ConfMin, RuleSetMin nondecreasing in N and t; Min ≤ Max+1; RuleSetMin ≤ ConfMin; Meets ⇔ s ≥ ConfMin; Quick sure ⇒ = Meets: 0 violations |
| Specials t ∈ {0, −0, −1, −Inf, +Inf, MaxFloat64, subnormal} × boundary N | 0/−0/negative → 0 for all; +Inf → MaxInt64 (0 at N = 0); saturation correct; NaN panics in New (callers validate) |
| Mutations (9): border Min→Max; max switch at x = 2^52; min switch at ≤; drop percentOf; Quick without n < 2^52; Meets always exact; RuleSetMin rounding to nearest; switch at 2^51; switch at 2^53 | all 9 detected (TestBorderUnchanged, TestAgainstBig/TestPinned, TestLibraryThresholdsUnchanged, TestALG2*, TestThresholdsIdenticalBelowSwitch, TestFractionMatchesCLI, ...) |

## Verification of the monotonicity proof

Checked the package-doc argument: for x < 2^52, t·RN(N) ≤ x(1+2^-53) < 2^52 + 1/2, so RN(t·RN(N)) ≤ 2^52 (tie to even) and the double minimum ≤ ⌈RN(2^52(1−ε))⌉ = 2^52−1 < ⌊x⌋ above. The double maximum at x ≤ 2^52 is ≤ 2^52+1 ≤ ⌈x⌉ for x > 2^52. The t = 1/8, N = 2^55−1 example is correct (RN(N) = 2^55 gives double max 2^52+1). Confidence: RN(N)·RN(t(1−ε)) < 2^52 for x < 2^52, ⌈·⌉ ≤ 2^52 ≤ ⌊x⌋ above. At x = 2^52 exactly N is a power of two (t = 2^-j), so min (exact 2^52) ≤ double max (2^52+1). Sound; confirmed by the walks.

## Oracle independence

internal/oracle imports only math, math/big and tract. The branch and the exact part use big.Rat; the below-switch part restates C's formulas (upstream.go) since they are now the definition there. No import of thresh/fpg/istree. Acceptable.

## Status of earlier findings

| ID | Status | Evidence |
| --- | --- | --- |
| C1 (HIGH) | resolved | probes above: identical to v0.1.0 for every probed fraction, computed confidence and CLI case below 2^52 |
| C2 (LOW) | resolved | border Min→Max mutation now fails TestBorderUnchanged and TestALG2CLI |
| C3 (LOW) | resolved | sweep compares with copies of main's formulas, including MinConfidence through percentOf; dropping percentOf is detected |

## New findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| R1 | LOW | confirmed | The docs say that upstream's tolerance would exceed one unit only above the switch, and that the exact branch keeps it below one unit. Just below the switch the kept double formula already admits a support one full unit outside an integer x. This happens from about x = 2^51.58: min = x−1 and max = x+1. It is upstream/v0.1.0 behavior and intended by the hybrid, but the docs imply a tolerance below one unit everywhere below 2^52. | `t=0.7677, N=4399772623513087`: x = 3377705443070997 (integer), Min = x−1. `t=0.3214, N=10509402814600758`: x = 3377722064612684, Max = x+1. Over x in (2^50, 2^53) the bound is up to 1.5 units from x. Wording: differences.md "Threshold contract" ("above it upstream's relative tolerance would exceed one unit") and N2 ("Once x = t·N exceeds about 2^52"). |

Fix requested (R1): state that below the switch the double formula may admit a support up to one unit outside the exact bound from about 2^51.6. Below that it stays under one unit. The switch is placed at 2^52 to keep results bit-identical. Docs only; no code change.

Performance: not assessed (second critic). Quick is now upstream's float test for t ≤ 1 and N < 2^52, which is v0.1.0's per-rule cost plus one compare.

## Decision

ACCEPT WITH CONDITIONS (only a LOW finding: R1, docs wording). No Critical, High or Medium findings. ALG-2 is fixed from 2^52 on (exact, verified with big.Rat). Ordinary-weight results are identical to v0.1.0 in all probes. Monotonicity across the switch holds by proof and by adversarial walks.
