# PR #28 review: numerical correctness

- **Issue:** #16 (ALG-1, plus ALG-6 from the #15 comment)
- **PR:** https://github.com/dimchansky/fpgrowth-go/pull/28 (branch `fix/16-numerical-stability`)
- **Head SHA reviewed:** `803626e43ce7c77b7fbd161b003bab8f2c274e51`
- **Reviewer specialization:** numerical analysis, floating-point and exact arithmetic, statistics (chi², G-test, Fisher exact tests, certainty factor). Correctness critic. Performance and architecture are covered by a separate critic.
- **Decision: REJECT.** Two HIGH findings are open: CI is red on every non-arm64 platform, and the certainty factor still collapses (it returns 1 for an independent table).

## Scope

- The issue body and its comment.
- The design notes design16.md and design16-challenge.md, read for context.
- The full diff `origin/main...803626e`. I read every new line of `internal/ruleval/{exact,measures,fisher,fisherlarge}.go` and `internal/istree/report.go`, and the diffs of `internal/oracle/*`, `reference/difftest/deviations.go`, `docs/differences.md`, `CHANGELOG.md`, `measure.go` and `README.md`.
- The documented bounds, checked against my own independent high-precision reference (below), mutation testing, a cross-architecture run, and the CLI.

## Evidence examined and method

- **Independent reference** (`scratchpad/review/pr28own/ref.py`, `cmp.py`):
  - Built on Python `fractions.Fraction` and `decimal`, at 120–300 digits. It does not use `internal/oracle` or mpmath (mpmath is not installed).
  - Chi² and Yates are exact rationals. The certainty factor is an exact rational.
  - The information is Σ c·ln(c·n/(r·k)) at 300 digits.
  - Q(x,1) is erfc(√(x/2)): a series below 9, a continued fraction above.
  - Fisher:
    - ln P comes from exact factorials (m < 3000) or the Stirling–Bernoulli series (15 terms).
    - Tables are summed with exact rational ratios from the mode, with a tail cut at 1e-50.
    - Regions are taken from the exact definitions: f by bisection on high-precision ln P, h by integer mirror bounds, m by Σ c ln c, s as x ≥ supp.
- **Driver:** a throw-away `cmd/zzdump` built from the PR head. It prints `ruleval.Eval` for each measure letter and `AboveExpected`. It was later removed from the clone.
- **Bounds checked:**
  - `n y`: relative 2^-49.
  - `i`: relative 2^-47.
  - `p t g`: relative 2^-36·(1+x) + 2^-1000.
  - `z`: absolute 2^-40.
  - `f h m s`: relative 2^-36 + 2^-1000 (absolute 2^-36 for h and s at n ≤ 170).
  - `-i`: exact.

## Commands run (results)

| Command | Result |
| --- | --- |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` (arm64) | clean |
| `go test ./...` (darwin/arm64) | ok |
| `go test -race -count=1 ./...` (arm64; load 4.0) | ok |
| `GOARCH=amd64 go test ./internal/ruleval/ . ./cmd/fpgrowth/ ./internal/istree/ ./internal/oracle/` (Rosetta) | **FAIL**: `TestInfoBranches` (F2) |
| `gh pr checks 28` at 803626e | **FAIL**: Test ubuntu (Go 1.26/1.27), Test windows (Go 1.26/1.27), Coverage and Cross-build (linux/386) all fail on `TestInfoBranches` (F2). C reference/difftest on macOS and ubuntu, Fuzz smoke and GoReleaser pass |
| Independent grid, seed 1 (`grid.py`): 6,883 adversarial tables (n ∈ {171 … 2^63−1}, margins 1, 2, n/2, n−1, n, s ∈ {lo, hi, lo+1, hi−1, E−1, E, E+1}, ALG-1 k = 1e3…1e18, perfect association, certainty near 1, 3,000 log-uniform random) | `n y i p t g`: 0 failures. Worst fractions of the bound: n 0.345, y 0.29, i 0.202, p 1.4e-4, t 9.8e-5, g 1.0e-4. **`z`: 2 failures (F1)** |
| Same grid, Fisher on the 5,982 tables with σ² < 1e6 | 0 failures. Worst fractions of the bound: f 0.010, h 0.021, m 0.010, s 0.011, including p-values down to 1e-273 |
| `grid2.py` seeds 2 and 3 (12,000 tables near independence and at δ = ±1/4, −1/2 ± ε) | `n y i p t g`: 0 failures (worst: n 0.35, y 0.34, i 0.29 of the bound). `z` fails again (F1) |
| Certainty search near independence, n ∈ [2^61, 2^63), n−h ≤ 2^40 (11,793 tables) | **1,826 exceed 2^-40. The worst error is 1.0** (F1) |
| Certainty search inside 32 bits (n < 2^31, 399,974 tables) | 0 exceed (worst 1.1e-16) |
| FisherProb near-tie scan (two pointers over s, σ ≈ 5e5, n ≈ 4.5e12; `zz_review_test.go`) | **7 near-ties** at distance ≈ 696,680 with true P(x) > P(s), counted as ties (F3) |
| arm64 vs amd64 (Rosetta) values on 20,000 random tables | `n y z` bit-identical. `p t g i` differ in the last bits on 1–1.3 % of rows (math.Log/Log1p; documented) |
| CLI `-i` with products ≈ 2^123 (D = 2^61 > 0, but the float test says "not above") | PR: rule value 0.99999976, set value 0.99999976, `-ax -el` set value 1. origin/main: 0 / 0 (invalidated). `-i` is exact for rules, sets and aggregation |
| Mutation testing, 25 mutants (`mut.sh`, tests of ruleval, root, cmd/fpgrowth, istree, oracle) | 21 killed, 4 survived (see F5) |

### Mutation detail

**Killed**, with the tests that catch them:

| Mutant | Killed by |
| --- | --- |
| certgate off | TestALG1API, TestALG1CLI, TestLargeTables…, TestPinnedBits |
| chi² 128-bit path removed | Fuzz…, TestALG1*, TestEvalMatchesUpstream, TestLargeTables…, TestPinnedBits |
| info wide-D removed | killed |
| gSeries truncated | TestInfoBranches |
| gSeries branch range | many tests |
| u128 sticky bit | TestCrossDiff, TestU128Float |
| AboveExpected as float | TestALG1API, TestCrossDiff |
| int64 path widened | TestCrossDiff, TestInfoBranches |
| fisherTailRel 2^-20 or 2^-34 | killed |
| x0 = lo | killed |
| mode + 2 | killed |
| FisherChi2 missing ceil | killed |
| FisherChi2 floor − 1 | killed |
| fisherTieExact = 0 | TestFisherLargeNearTie |
| near-tie → false | TestFisherLargeNearTie |
| lnPErr 2^-60 | TestEvalMatchesUpstream, TestHyperLnP |
| Stirling series truncated | TestPinnedBits, TestStirlerr |
| certainty conf<p branch | killed |

**Survived:**

- **Yates int64 fast path widened to 2^40.** The values become wrong, for example Yates(3e9, 6e9, 7e9, 1e10) = 0 instead of 0.2857, but no default test catches it.
- **No re-anchoring** (`fisherAnchorEvery = 2^40`).
- **Neumaier sum replaced by a plain sum.**
- **`r < 1` guard removed from the tail stop.** This mutant is equivalent: with r ≥ 1 the condition cannot hold anyway.

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| F1 | **HIGH** | confirmed | The certainty factor still fails catastrophically at large weights. The bound "absolute 2^-40" (numeric contract, `Measure` GoDoc, CHANGELOG) is false, and the acceptance criterion "no silent catastrophic failure for any input the API accepts" is not met | See F1 below |
| F2 | **HIGH** | confirmed | `TestInfoBranches` fails on amd64, 386 and Windows, and CI at the head is red. The fault is in the test's reference, not in the implementation | See F2 below |
| F3 | MEDIUM | confirmed | A FisherProb near-tie counted as a tie breaks the documented "relative 2^-36" bound by up to 5 orders of magnitude, and the contract does not quantify it | See F3 below |
| F4 | LOW | confirmed | The difftest N1 slack for `-eg` is not narrow | See F4 below |
| F5 | LOW | confirmed | Test gaps found by surviving mutants | See F5 below |
| F6 | LOW | confirmed | The README overstates the certainty factor's bit-identity | See F6 below |

### F1: certainty factor (HIGH)

**Mechanism.** The exact form is used only for gate ∧ D > 0. With gate ∧ D ≤ 0 (independent or below independence), upstream's `(s/b − h/n)/(1 − h/n)` is used.

- Converting each support above 2^53 to float64, plus the two divisions, gives conf − p an absolute error of a few u.
- When the float difference comes out ≥ 0 although D ≤ 0, it is divided by 1 − p ≈ (n−h)/n and amplified by n/(n−h), up to 2^50.
- The code comment's argument ("the conf < p branch divides by p") assumes the float sign equals the exact sign.

**Reproductions** (exact values in parentheses):

- `Certainty(7974968969870368360, 7974968969870368421, 8766415699794613718, 8766415699794613784)` = **1** (exact −1.2e-19).
- CLI: `printf 'x y 7974968969870368360\nx 61\ny 791446729924245358\nq 5\n' | fpgrowth -tr -w -s0 -c0 -m2 -ez -d99 -v' %a %b %h %20e'` prints `y <- x … 1`. Chi² for the same rule is 1.9e-20.
- An exactly independent table (D = 0, chi² = 0): `(3458764513820538678, 3458764513820540928, 4611686018427384904, 2^62)` gives **0.1667** (exact 0).
- `(102278961546595576, …)` gives 6.7e-6 (exact −3.5e-18).

**Frequency.** 1,826 of 11,793 near-independence tables with n ≥ 2^61 and n−h ≤ 2^40 exceed 2^-40.

**Status on main.** The same values occur on origin/main, so this is not a regression. But the PR claims it is fixed.

**Why the tests miss it.** `TestEvalMatchesUpstream`'s `exactScope` for Certainty also requires `AboveExpected`.

### F2: TestInfoBranches fails off arm64 (HIGH)

The failing check is `gSeries(-0.01874999999999999) = 0.00017689029983461424, direct 0.00017689029983461793` (exact_test.go:220).

- A Decimal computation gives the exact g = 1.76890299834614232e-4. gSeries is off by 0.4 ulp; the test's "direct" (1+d)·log1p(d) − d is off by 188 ulps.
- That form cancels by a factor of about 2/|δ| and is not a valid reference at the 64-ulp tolerance. It passes on arm64 only because FMA fusion inside `math.Log1p` changes the rounding.
- The PR's "tests executed" section was run on arm64 only.

### F3: FisherProb near-ties (MEDIUM)

**Example:** s=814290028136, b=1744408929115, h=2100559551280, n=4499911755383 (σ ≈ 5.2e5, mode 814290376474). The table x = 814290724812 lies 696,676 tables away.

- Its true probability is above the observed one: ln P(x) − ln P(s) = +1.35e-14 (Decimal at 130 digits). That is within lnPErr (1.2e-13).
- So it is summed. Go `f` = 0.49931686620866794. `h` = `m` = 0.49931625036102467, and the difference is exactly P(x) = 6.158e-7.
- The true `f` excludes x, so the relative error is 1.2e-6, about 85,000× 2^-36.

The scan found 7 consecutive s values with such ties at one margin set: the near-ties cluster, so this is not an astronomically rare event at σ ~ 1e5–1e6.

The direction is conservative (p only grows, so a rule may be wrongly dropped), as documented. But the numeric-contract table, the `Measure` GoDoc and the CHANGELOG state "Fisher within 2^-36" with no exception, and the near-tie sentence does not bound the effect. The extra error is at most one table, P(x) ≤ about 1/(σ√(2π)), relative to p.

**Fix options:**

- Document the error bound, for example "plus at most one table probability P(x) ≤ 1/(σ√(2π)) when a near-tie farther than 1024 tables away counts as a tie".
- Or decide such ties exactly: compute ln W(x) − ln W(s) as a Σ of log1p of exact rational ratios, or with an interval/big.Float bound, instead of treating them as ties.

### F4: difftest N1 slack for `-eg` (LOW)

`alg6Slack` applies the near-zero worst case √(6δG/π) to every rule, whatever its G. That gives absolute 2.5e-6 at n = 100 (about 4e5× `measureTol`), 1e-5 at n = 1000 and 1.7e-2 at n = 1e9.

#15's own DefectALG6 used the two-branch bound δG·f(G−δG) for G > 2δG, which is orders of magnitude tighter away from independence. A Go `-eg` regression of that size would pass difftest. The oracle tests still check `g` strictly, hence LOW.

### F5: test gaps (LOW)

**Yates.** Default tests do not cover the Yates int64 path with |D| ≥ 2^63. Widening the int64 fast path to 2^40 (overflow) survives, and `TestUpstreamFormulaIdentity`/`TestLargeTables` use tables whose D is small, so the wrap-around is harmless there. One table such as (3e10, 6e10, 7e10, 1e11) would kill it.

**Fisher walk.** Removing the re-anchoring and the Neumaier summation also survives. That is acceptable statistically, but no test exercises a walk long enough (σ ≥ 1e4) to bound the drift.

### F6: README certainty claim (LOW)

The README says "chi², Yates, certainty and `-i` stay bit-identical wherever the C formula is exact (products up to 2^53)". The certainty factor changes for conf > prior with n/(n−h) > 2^10 even with small products: 2 fixture rows change, per differences.md. differences.md states this correctly.

### Verified correct

- **Cross product and rounding.** `crossDiff`/`u128Float` are correctly rounded. Ties and sticky bit are tested, and the sticky mutant is killed.
- **Chi² and Yates.** Exact D gives 8 ulps (my worst is 0.35 of the bound up to n = 2^63−1, including the D = 0 and clamp boundaries of Yates).
- **Information.** The 59u (32-ulp) bound holds: my worst is 0.29 of the bound, including δ at ±1/4 and −1/2 ± ε, D = ±1 at n ≈ 2^62, and n = 2^63−1. It is exactly 0 at independence, with G = 1.
- **p-values.** p, t and g are far inside 2^-36(1+x) (≤ 1.4e-4 of the bound).
- **Fisher, n > 170.**
  - Within ≤ 0.021 of the bound on 5,982 tables (σ ≤ 1000), with n up to 2^63−1 and skewed margins, and tails down to 1e-273.
  - The FisherChi2 mirror bounds (both signs, ceil/floor) are correct, and their mutants are killed.
  - The geometric tail stop is rigorous. On each side of the mode the ratios r(x) are non-increasing (log-concavity), and the code's r is the ratio of the step just taken, so the rest is ≤ p·r/(1−r). Rounding only matters as r → 1, where the `r < 1` guard prevents a false stop. I found no counterexample.
  - The mode clamp is right: x0 = clamp(mode) is the maximum on [lo, hi] for a unimodal P.
- **`-i`.** Exact through rules, item sets and aggregation in the CLI. There is a single evaluation site (`istree.Tree.value`).
- **Oracle independence.** `internal/oracle` imports no `ruleval` (`go list`). Its tolerances were only tightened: chiTau 2^-49 < ratTau, infoTau 2^-47 < ratTau, certTau 2^-40 < pTau, fisherTau 2^-36 with the old lgamma scaling removed. The oracle's Func-mode `-i` is exact by its own `aboveExpected`.
- **`TestEvalMatchesUpstream`.** Its `exactScope` rule is principled: it is defined by mathematical conditions (products > 2^53, the certainty gate, a Fisher table cap), not by row lists. It mirrors the code's branches, which is also why it misses F1.
- **FMA guarding.** Every new product that feeds a sum is wrapped. `n y z` are bit-identical between arm64 and amd64 on 20,000 tables, and `TestPinnedBits` passes on amd64.
- **Regression tests.** `TestALG1API`, `TestALG1CLI`, `TestALG6Regression` and `TestLargeTablesMatchReference` cover the issue's tables, and the corresponding mutants are killed.

## Fixes requested

1. **F1 (HIGH).** In `reCert`, inside the gate (`head < base && base-head < base/certGate`), handle all signs exactly:
   - D > 0: D/(b·(n−h)), as now.
   - D < 0: D/(b·h) (= (conf−p)/p exactly), rounded once.
   - D = 0: 0.

   Also:
   - Add regression tests for the three reproductions above (the CLI case included).
   - Extend `exactScope` and the oracle comparison to gate ∧ D ≤ 0.
   - Correct the code comment's error argument.
2. **F2 (HIGH).** Replace the "direct" reference in `TestInfoBranches` with an accurate one, for example `oracle` or big.Float, or compare only where the direct form is well-conditioned (|δ| large). Re-run CI on amd64, 386 and Windows to green.
3. **F3 (MEDIUM).** Either decide FisherProb near-ties exactly at any distance (bounded-cost high-precision ln W difference), or document the extra error: at most one table, P ≤ 1/(σ√(2π)), conservative direction. Put it in the numeric-contract table, the `Measure` GoDoc, the CHANGELOG and the `fisherTieExact` comment. Add the example above as a test of whichever behavior is chosen.
4. **F4 (LOW).** Use the two-branch ALG-6 bound in `alg6Slack`.
5. **F5 (LOW).** Add a Yates/Chi² table with supports in [2^31, 2^40) and |D| ≥ 2^63. Optionally add a long-walk Fisher test (σ ≥ 1e4) against the per-term sum.
6. **F6 (LOW).** Correct the README sentence.

## Performance impact

Not assessed here (separate critic). Incidentally: an amd64 build under Rosetta and the arm64 `-race` run complete normally. The Fisher cost is O(σ), so roughly 9σ ≈ 7e9 steps per side at n = 2^63 with balanced margins, as documented.

---

## Re-review of 418a499

- **Head SHA:** `418a4998ee5b31cbeb980a3df7907976da1433a3`
- **Scope:** a full correctness re-review against issue #16 (body and comment), with the same method as above.
- **Decision: ACCEPT.** No Critical, High, Medium or Low findings are open.

### Delta reviewed

`git diff 803626e..418a499` touches 15 files:

- `biglog.go` (new)
- `measures.go` (`reCert`)
- `fisherlarge.go` (`probAtMost` → `cmpWeightBig`)
- `exact.go` (`crossDiffFloat` removed)
- the new tests
- `reference/difftest/deviations.go` (`alg6Slack`)
- CHANGELOG, README, `docs/cli.md`, `docs/differences.md` and the `measure.go` GoDoc

### Commands and results

| Check | Result |
| --- | --- |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` | clean |
| `go test ./...` (arm64) | ok |
| `go test -race` (ruleval, istree, root) | ok |
| `GOARCH=amd64 go test ./...` (Rosetta) | **all ok**, including `TestInfoBranches` (7.5 ulps) and `TestCertaintyNearIndependence` |
| `gh pr checks 28` | 15/15 pass |
| Independent grid seed 1: 6,883 tables, n up to 2^63−1 | **0 failures in any measure.** Fractions of the bound: z 0.075, n 0.345, y 0.29, i 0.20, p/t/g ≤ 1.4e-4 |
| Grid seed 1, Fisher on 5,982 tables | 0 failures; f, h, m, s ≤ 0.021 of the bound |
| `grid2` seeds 2 and 3: 12k tables near independence and at branch points | 0 failures; z ≤ 0.116, n ≤ 0.35, y ≤ 0.34, i ≤ 0.29 |
| Certainty searches: 11,793 tables with n ≥ 2^61 and n−h ≤ 2^40; 399,974 tables with n < 2^31 | **0 above 2^-40** (worst 1.1e-16 and 3.3e-24). Before the fix: 1,826 tables, worst error 1.0 |
| Earlier F1 examples | z = −1.2019994443647789e-19 (exact −1.20200e-19); 0 for the independent n = 2^62 table; −3.5251696990980861e-18 (exact −3.5252e-18). CLI `-ez -d0` now drops the rule |
| Earlier F3 cluster (7 values of s, σ ≈ 5e5) | `f` = `h` = the true value for all 7 (it was off by P(x) = 6.2e-7); 0.95 s for the 7 rows × 2 measures |
| `bigLnFact` vs. an independent reference (Python Decimal at 200 digits: exact ln m! below 20,000, Stirling with 39 Bernoulli terms from Akiyama–Tanigawa above), at 21 points from m = 0 to 2^63−1 | Worst absolute error **2^-224.1, at m = 256**, the smallest Stirling argument. That is the truncation error, matching the claimed bound below 2^-220. Exact-factorial range ≤ 2^-309.8. At m = 2^62, 2^63−2 and 2^63−1: 2^-250.5, 2^-250.0 and 2^-249.1 |

**What the `bigLnFact` result implies.** A difference of ln W takes at most 8 terms, so its error is ≤ 2^-221. That is far below the 2^-200 tie bound, and at 320 bits the cancellation near MaxInt64 still leaves more than 2^-246 absolute accuracy.

**Hand check of the Stirling coefficients.** I checked all 15 `stirBern` entries by hand as B_2k/(2k(2k−1)) in lowest terms (k = 11, 13, 14 and 15 are reduced forms): correct.

**Cost of `cmpWeightBig`.** It is bounded: 8 logarithms of about 64 atanh terms at 320 bits, called only in the lnP error band beyond 1024 tables, at most a few times per bisection.

### Upstream definition of the certainty factor

`docs/upstream/rules-eval.md` (row RE_CERT, from `ruleval.c`) defines p = h/n, δ = s/b − p, and the value as δ/(1−p) for δ ≥ 0, else δ/p. The new branches match this exactly:

- **δ < 0:** δ/p = (s·n − h·b)/(b·h) = D/(b·h).
- **δ = 0:** 0.
- **δ > 0:** D/(b·(n−h)).

The δ < 0 branch is clamped at −1, which is exact because |D| ≤ b·h.

### Mutants

18 mutants: 16 killed, 2 survived, and both survivors are equivalent.

| Mutant | Killed by |
| --- | --- |
| Yates int64 path widened to 2^40 (F5) | `TestLargeTablesMatchReference` |
| Certainty negative branch uses b·(n−h) | 6 tests |
| Certainty negative branch falls back to upstream | 6 tests |
| Certainty gate off | killed |
| `cmpWeightBig` replaced by `return true` | `TestFisherLargeNearTie` |
| Tie bound loosened to 2^-30 | `TestFisherLargeNearTie` |
| 3 Bernoulli terms | `TestCmpWeightBig` |
| 64-bit precision | `TestCmpWeightBig` |
| One Bernoulli coefficient changed | `TestCmpWeightBig` |
| Exact-factorial threshold lowered | `TestCmpWeightBig` |
| No re-anchoring (F5) | `TestFisherLongWalk` |
| Plain summation instead of Neumaier (F5) | `TestFisherLongWalk` |
| gSeries truncated | killed |
| chi² 128-bit path removed | killed |
| `AboveExpected` as float | killed |
| fisherTailRel 2^-34 | killed |

The two equivalent survivors:

- **D = 0 case removed from `reCert`:** the D > 0 branch then returns 0/… = 0, the same value.
- **`fisherTieExact = 0`:** every near-tie then goes through `cmpWeightBig`, which is also correct.

### Status of the earlier findings

| ID | Status | Evidence |
| --- | --- | --- |
| F1 HIGH | **fixed, verified** | Exact sign of D inside the gate, matching upstream's definition. Independent searches find 0 violations. `exactScope` now covers the whole gate. The regression tests and the CLI test kill the mutants |
| F2 HIGH | **fixed, verified** | `TestInfoBranches` passes on amd64 under Rosetta; CI is 15/15 green |
| F3 MEDIUM | **fixed, verified** | Near-ties beyond 1024 tables are decided at 320 bits, and the 7-case cluster is now correct. `bigLnFact` is accurate to 2^-224 independently, and its docs match |
| F4 LOW | fixed | `alg6Slack` uses the two-branch bound |
| F5 LOW | fixed | The Yates overflow tables and `TestFisherLongWalk` kill the earlier surviving mutants |
| F6 LOW | fixed | README and `cli.md` now state the certainty factor's bit-identity scope correctly |

### Acceptance criteria of #16 (correctness side)

1. **Error bounded and documented up to MaxInt64 for every affected measure and `-i`:** verified independently for z, n, y, p, t, i and g over the full range, and for f, h, m and s up to σ ≈ 1e3 (plus the n near 2^63 skewed-margin tables).
2. **The issue's tables and the CLI reproductions:** regression tests exist and pass.
3. **No catastrophic failure:** none found.
4. **Ordinary-data changes explained:** the changes are explained in differences.md N1/F-1, and the `exactScope` rule is principled.
5. **Docs updated:** yes.

The benchmark criterion is for the performance critic.

### New findings

None.

Note: FisherChi2 and FisherSupport for n ≤ 170 keep the absolute 2^-36 bound, and the certainty factor below the gate can still have the wrong sign within its absolute 2^-40 bound (seen: 1.05e-13 against an exact −4.7e-18). Both are documented and inside their stated bounds.

---

## Re-review of 59bcede

- **Head SHA:** `59bcede9db03f6cad3393fb4689c1ff65b791dca`
- **Delta from 418a499:** `probUnderflows` in `fetProbLarge`, buffer reuse in `biglog.go` (P-7), two new tests, and doc text.
- **Decision: ACCEPT.** No Critical, High or Medium findings. One LOW is noted (L1); it predates this commit and is within the documented bound.

### Is the shortcut bound valid?

- **Which tables are summed.** FisherProb sums the tables x with P(x) ≤ P(supp). Since 418a499, `probAtMost` returns true only in three cases, so every summed table has P(x) ≤ P(supp)·(1+2^-200):
  - lx < ls − tol, which implies P(x) < P(supp) because `lnPErr` is a valid bound (verified earlier);
  - an exact `cmpWeight` result ≥ 0;
  - a `cmpWeightBig` result ≥ 0, i.e. truly smaller, or tied within 2^-200.
- **The count.** The normalized table has rest ≥ 0 and is indexed x ∈ [0, ft.body], so `ft.body + 1` is exactly the number of tables. It is the min-margin count after complement and swap; it cannot be undercounted.
- **The error direction.** `ls + lnPErr(ss)` is an upper bound on ln P(supp), so the direction is right.
- **The bound.** Together these give p ≤ (body+1)·P(supp)·(1+2^-200). The 2^-200 contributes ln(1+2^-200) ≈ 6e-61, well below the added 1e-15.
- **The cutoff.** ln 2^-1075 = −745.1332. With −745.2 the margin is 0.067, against a float rounding error of the sum of about 1e-13. Any exact value below 2^-1075 rounds to 0 (the tie at 2^-1075 goes to even, which is 0). So a value that would round to the smallest subnormal can never be shortcut to 0.

### Checks

| Check | Result |
| --- | --- |
| gofmt, vet, staticcheck | clean |
| `go test ./...` (arm64) | ok |
| `GOARCH=amd64 go test` ruleval and root (Rosetta) | ok |
| `-race` ruleval | ok |
| CI | 15/15 pass |
| `TestCmpWeightBig`, `TestFisherLargeNearTie`, `TestFisherProbUnderflow`, `TestFisherProbUnderflowCutoff` | pass (the cutoff test fired the shortcut 19 times, with 33 positive values) |
| `bigLnFact` after the buffer reuse, 21 points from m = 0 to 2^63−1 | identical to 418a499 to 95 digits, so still ≤ 2^-224 against my independent Decimal reference |
| The 7-case near-tie cluster | still correct (`f` equals `h`, which equals the true value) |
| Cutoff straddle sweep against my Python reference (`cut2.py`): 600 tables with (body+1)·P(supp) within ±0.4 of e^-745.2, n up to 2^63 | 293 tables in the shortcut region: Go 0 and reference rounds to 0 in every case (0 issues). 307 above the cutoff: 0 cases of Go 0 with a nonzero reference |
| Window sweep (`cutoff.py`): 1,500 tables, 857 with exact p in [1e-330, 1e-320] | 43 tables where Go gives 0 but the exact value rounds to a subnormal of 1–5 × 2^-1074. **None of them is the shortcut**: their bound is about −735, and 418a499 gives the same 0. See L1 |
| Mutant: shortcut removed | killed (`TestFisherProbUnderflow`) |
| Mutant: cutoff −740 | survived; it can misfire only in a sliver where p/((b+1)·P(supp)) > e^-5 |
| Mutant: ln(body+1) term dropped | survived; on such tables the unmutated code already returns 0 (L1) |

### Findings

- **L1 (LOW, confirmed, pre-existing, within the contract).** `fisherSum` computes each range as `exp(lnA)·q`. When `exp(lnA)` underflows (lnA < −745.13) while q is large, the whole range is lost.
  - Example: FisherProb(2748994426, 5500000000, 5500000007, 11000000000) has exact value 8.6e-322 (174 × 2^-1074); Go gives 0, and so does 418a499.
  - This is within the documented absolute 2^-1000 and affects only values below about 1e-305.
  - Optional fix: `exp(lnA + log(q))`, or scale q before exponentiating.
  - It also explains why the ln(body+1) mutant survives. A test with σ ≈ 2.6e4 tails (like the example) would kill that mutant once L1 is fixed.

---

## Re-review of 8f62ab2

- **Head SHA:** `8f62ab2513b9d6419bdaef0f981c28f2c06b15fe`
- **Delta from 59bcede:**
  - b9df540: `fisherSum` uses exp(lnA + ln q) when exp(lnA) < 2^-1022.
  - 8f62ab2: typed constants in the new test.
- **Decision: ACCEPT.** L1 is fixed as far as float64 allows, and there are no new findings.

### Results

All values below were checked against my own Python reference, not against the package's ln P.

| Check | Result |
| --- | --- |
| L1 example FisherProb(2748994426, 5500000000, 5500000007, 11000000000) | 8.5967e-322 (exact 8.6002e-322; previously 0) |
| Window sweep (`cutoff.py`): 1,500 tables, 857 with exact p in [1e-330, 1e-320] | Wrong zeros fell from 43 to 25. All 25 remaining are below 5.7e-324 (at most about one subnormal ulp, 4.9e-324). The shortcut still never misfires |
| Cutoff straddle sweep (`cut2.py`) | unchanged: 293 shortcut cases, all correct; 307 above the cutoff, none wrongly 0 |
| Ordinary data: 3,000 random tables (n from 171 to 1e6), all 11 measures `z n p y t i g f h m s` | **Bit-identical to 418a499 except on 9 rows, and only in Fisher values below 2.2e-308.** In every one of those, the new value matches the exact reference better than the old one (for example 0 → 1.98e-323, exact 1.98e-323; 1.19712e-320 → 1.19811e-320, exact 1.19811e-320). Normal-range values are unchanged |
| `TestEvalMatchesUpstream`, `TestPinnedBits`, `TestFisher*` | pass |
| `go test ./...` (arm64) | pass |
| `GOOS=linux GOARCH=386 go vet ./...` | clean |

The 25 remaining wrong zeros come from rounding each range of the sum separately in the subnormal range. That error is irreducible in float64 without summing in the log domain, and it is far within the documented absolute 2^-1000.
