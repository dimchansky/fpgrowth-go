# Design and design challenge for #17

Written before the implementation. The first contract (exact with a tolerance of 2^-52) was later replaced by the hybrid contract after the review of PR #30 (finding C1); see the review records.

## Design proposal

## Design for #17 (ALG-2): exact threshold contract

Status: phase 1 (design only). Base: origin/main ee75321. Experiments: `scratchpad/exp17/{,v2,v3,v4,v5}`
(big.Rat references; `v5/inttrick_test.go`).

### 1. What exists today (verified in the code)

| Threshold | Library | CLI | Comparison |
| --- | --- | --- | --- |
| min support | `Support.minAbs`: `ceil(f·float64(W)·(1−ε))` | `minSuppValue`: `ceil(p/100·W·(1−ε))` | integer `s >= min` |
| max support | `maxAbs`: `floor(f·W·(1+ε))` | `maxSuppValue` | integer |
| border | `minAbs` per entry | `borderSupp` (same formula) | integer |
| rule set min (body def.) | `countThresholds`: `ceil(nextafter-safe body·conf·(1−ε))`, capped by body | `ceil(ceil(smin)·conf·(1−ε))` | integer |
| confidence | `percentOf(MinConfidence)` → `Conf/100` | `Conf/100` | per rule `float64(supp) < float64(body)·conf·(1−ε)` (`istree` `rule`, `confEps`) |
| range check | `checkRange`: reject if `g(1+4ε) < f(1−4ε)` | none | once |
| eval (-d) | `percentOf(Threshold)` | `Thresh/100` | float measure value — **out of scope** (#16, F-1) |

ε = 2^-52. The tolerance is relative, so it exceeds one support unit once f·W > 2^52 (N2): `-s50` admits
2^61−200 of W = 2^62 (min 2^61−512), `-c100` admits supp = body − 1000 at body ≈ 2^62 (C admits deficits up
to 2559 at MaxInt64), `Fraction(1)` of W = 2^60+1 is W−257 (the GoDoc says W−256: `float64(W)` rounds to
2^60 first; confirmed). Beyond 2^53, `float64(W)` adds another rounding.

### 2. Candidate contracts that were rejected (measured)

1. **Exact binary value** (`s ≥ f·W`): Fraction(0.1) of W = 10 would be 2. Breaks ordinary use. Rejected.
2. **Shortest decimal of f** (`strconv 'g' -1`, exact rational): 0.1 → 1/10 and 1 → exact, but computed
fractions break. `p = smin·100/total` (difftest `large.go`) gives smin+1 in 50 % of the cases at W < 2^31,
and `TestFractionFormula` (k/d) fails. Rejected.
3. **Round-to-nearest** ("RN(s/W) ≥ f", tolerance half an ulp of f): it changes ordinary-weight results. The
CLI's double rounding `RN(RN(p)/100)` gives 161 diffs in 6 M (2-dp percents, W ≤ 600).
`Fraction(nextafter(0.3))` of 10 becomes 4. MinConfidence 1 stays inexact for body > 2^54. Rejected.

### 3. Proposed contract (one rule for every threshold kind)

Let a threshold be a float64 fraction t ≥ 0 taken at its **exact binary value**, and x = t·N the exact
rational product with the reference support N (W for supports, the body support for confidence). Then:

- **Minimum** (`-s`, `MinSupport: Fraction`, `-F`/`Border` entries, rule body minimum, `-c`/`MinConfidence`
  with N = body): s qualifies ⇔ **s ≥ x·(1−2^-52) and s > x − 1**, i.e. `min = max(⌈x(1−ε)⌉, ⌊x⌋)`, then
  clamped to ≥ 1 for supports (B2).
- **Maximum** (`-S`, `MaxSupport: Fraction`): s qualifies ⇔ **s ≤ x·(1+2^-52) and s < x + 1**, i.e. `max =
  min(⌊x(1+ε)⌋, ⌈x⌉)`, saturating at MaxInt64.
- **Rule set minimum** (body definition): `setMin = minConf(c, bodyMin)` with the confidence minimum above
  (it is nondecreasing in the body, so it is the exact pruning bound), clamped to [1, bodyMin]. With `-o` it
  is the support minimum, as today.
- **CLI percentages** become fractions by upstream's own first step, `t = p/100` as a float64 division
  (`fpg->conf = conf/100.0`; `smin/100.0`). So `-sk` and `Fraction(k/100)` are identical **at every W**, by
  construction. Negative (absolute) CLI values keep `ceil(-v)`/`floor(-v)` (N3 is #18's).
- `Count(n)` is unchanged (exact).

Plain words: this is upstream's tolerance, computed exactly, and never as large as one unit of support. The
second condition (the cap) can matter only when x·ε ≥ 1, i.e. x ≥ 2^52: for x < 2^52 the contract *is*
`⌈x(1−ε)⌉`/`⌊x(1+ε)⌋` in exact arithmetic (proof: ⌊x⌋ ≥ ⌈x(1−ε)⌉+1 implies x ≥ x(1−ε)+1). When x is an
integer the threshold is exactly x. So `Fraction(1)` = W, `Fraction(0.5)` of an even W = W/2, and
`MinConfidence: 1` / `-c100` admit only supp = body, for every weight. Decimal and computed fractions keep
hitting their intended counts while the float error stays within ε·x. If t·N > 2^53, t is only as precise as
its float64 value: `Fraction(0.1)` of 10·2^59 is 2^59+32 (today 2^59−128). Use `Count` for exact bounds
there.

Results on the cases (big.Rat reference, v4/v5):
- N2 repro 1 (`-s50`, W = 2^62): min = 2^61. `a` (2^61−200) is no longer reported.
- N2 repro 2 (`-c100`): `y <- x` is rejected. Confidence 1 at body MaxInt64: min = body (today body−2559).
- `Fraction(1)`, W = 2^60+1: W (today W−257). MaxInt64: min = max = W.
- `Fraction(nextafter(0.3,1))` of 10 = 3 (unchanged; test fpgrowth_test.go:347).
- `-s49.99999999999999`, W = 2^62: min = ⌊x⌋ (x = t·W with t = RN(p/100)).

### 4. Where results change (vs. C and vs. v0.1.0)

(a) **x ≥ 2^52** (the cap): the intended fix. (b) **Any x**: wherever C's float evaluation error (three
roundings, ≤ ~1.5·2^-52 relative, plus `float64(W)` beyond 2^53) moves `p/100·W·(1−ε)` across an integer.
Measured old-vs-new diffs (min/max, 1e5 samples each unless noted):

| inputs | W < 2^31 | 2^36 | 2^40 | 2^44 | 2^48 | 2^52 |
| --- | --- | --- | --- | --- | --- | --- |
| CLI 2-dp percents, all, W ≤ 600 (6 M) | 0 | | | | | |
| CLI 3-dp percents | 0 | 0 | 1/2 | 33/27 | 504/486 | 8078/8130 |
| random percents / random fractions | 0 | 0 | 1 / 2 | | | |
| computed `smin·100/total`, `smin/total` | 0 (= smin) | | 0 | | 0 (2^46) | |
| confidence: difftest percents × all supp, body ≤ 800 (4.2 M) | 0 | | | | | |
| confidence 3-dp near boundary (1.8e5) | 0 | 0 | 1 | | 371 | |
| rule set min, difftest percents, bodyMin ≤ 20000 | 0 | | | | | |

So: no observed change below 2^36 (difftest totals are < 2^31, so difftest and the goldens are expected to
pass unchanged — this has to be verified by running them). This is empirical. The provable statement is:
results differ only where an integer lies within ~2.5·2^-52·x of x. The implementer must pin this with a
deterministic sweep (§8).

### 5. Exact evaluation strategy and cost

Write t = m·2^-k (m odd, from `math.Frexp`, trailing zeros stripped) and X = m·N as a 128-bit product
(`bits.Mul64`; m < 2^53, N < 2^63 ⇒ X < 2^116). With q = X >> k and r = X mod 2^k:
- min = q if r = 0; else q+1 if r > ⌊X/2^52⌋, else q (⌊x⌋ ≥ x(1−ε) ⇔ r·2^52 ≤ X).
- max = q if r = 0; else q+1 if 2^k − r ≤ ⌊X/2^52⌋, else q.
- k ≥ 128 ⇒ q = 0, r = X. t ≥ 1 (k ≤ 0, only t = 1 or CLI `-S` > 100) shifts left with saturation. t = 0 ⇒
  0. ±Inf/huge `-S` ⇒ MaxInt64. NaN is already rejected.

The v5 prototype (`minInt`) matches big.Rat on 4e5 random cases (subnormal t, 2^-70, nextafter(1), W up to
MaxInt64). **Supports and borders**: converted once per run (as today). **Confidence**: one `minConf(c,
body)` per candidate rule (1 Mul64, 2 shifts, 2 compares, no division, no float) replaces one float multiply
and compare. A float fast path is not needed; it is the fallback only if benchmarks regress.

### 6. Code structure (prepares #18)

- New leaf package **`internal/thresh`** (stdlib only): `type Frac` (m, k, precomputed from a float64),
  `New(t float64) Frac`, `(Frac) Min(n int64) int64`, `(Frac) Max(n int64) int64`. Doc comment = the
  contract and the proof of §5. It is a leaf because `istree` (imported by `fpg`) needs it.
- `internal/fpg/thresh.go`: `computeThresholds` uses `thresh.New(SMin/100)` etc. for non-negative SMin/SMax.
  Absolute branches stay. `countThresholds` derives `supp = clamp(conf.Min(body))`; the nextafter dance
  goes. `minSuppValue`/`maxSuppValue`/`dblEpsilon` are removed or kept only for docs.
- **Confidence units**: `fpg.Config.Conf` becomes a fraction in [0,1]. The CLI sets `o.conf/100` in
  `minerConfig`, the library sets `MinConfidence` directly, and `percentOf` is no longer used for it (still
  used for `Evaluation.Threshold`). The neutral confidence is 1. `istree.Params.MinConf` builds a
  `thresh.Frac` in `New`; `rule` tests `supp < t.conf.Min(body)`. `confEps` is removed.
- Root `options.go`: `minAbs`/`maxAbs` use `thresh`. `cmd/fpgrowth` `borderSupp` uses `thresh` for v ≥ 0.
  Only one implementation, no copies.
- For #18: absolute literals can then become exact `Count`s per threshold. Recommendation for #18 (not done
  here): replace `SMin/SMax/HasCounts` with a per-threshold spec `{count int64 | frac thresh.Frac}`, so
  mixed `-s`/`-S` kinds need no float path.

### 7. checkRange, oracle, compatibility, docs

**checkRange**: keep the code. Re-justify the comment: if `RN(g(1+4ε)) < RN(f(1−4ε))` then g(1+ε) < f(1−ε)
exactly (each product rounds by ≤ 2^-53), and under §3 every qualifying s satisfies f·W(1−ε) ≤ s ≤ g·W(1+ε),
so the range is empty for every W. It never rejects a satisfiable range. The 0.30000000000000004/0.3 example
stays valid (both 3 of 10). Tightening to the exact `g(1+ε) < f(1−ε)` is optional (open question).

**Oracle** (`internal/oracle/threshold.go`, `rules.go`): an independent re-implementation of the definition
with `math/big` (big.Rat of `SetFloat64(t)`, the predicate `s ≥ x(1−ε) ∧ s > x−1` per set/rule). It does not
use `thresh` or the 128-bit trick. `AbsMinSupp`/`AbsMaxSupp`/ `AbsBorder` keep their upstream-argument
signatures (positive = percent, t = arg/100 as float64). `RuleSetMinSupp` and `Rules` (`confAdj`) follow the
contract. Docs drop "copies C's formulas". The C formulas move into a test helper (`upstreamMinSupp` …) used
only by the regression sweep.

**C compatibility**: a deliberate deviation for x ≥ 2^52 (upstream: 32-bit supports, unreachable) and for
C's float-rounding crossings (rare, none observed below 2^36). The difftest needs no new classification if
it stays green. If a difftest case differs, classify it as a documented N2 deviation (`deviations.go`), not
as tolerance.

**docs/differences.md**: N2 becomes a *defect fix (deviation)* row with the contract, both reproductions now
correct, and the §4 change table in short. L-5 changes (Fraction: "upstream's formula in exact arithmetic,
capped below one unit; `Fraction(k/100)` ≡ `-sk` for every W"). L-7 changes (MinConfidence is no longer
mapped through `percentOf`). New short table "Threshold contract" next to "Numeric contract".

**GoDoc**: `Fraction` (contract; fix W−257 → now exactly W; the 2^59+32 precision example), `MinConfidence`
(replace the `·(1−2^-52)` formula with the two conditions; 1 means Support == BodySupport),
`RuleOptions.MinSupport`/ `OriginalSupport` (body vs set), `Options.MaxSupport` (tolerance wording). Update
the README threshold paragraph (l. 171), docs/cli.md (rows `-s -S -c -F` l. 187–192; one contract for both
interfaces), and CHANGELOG `[Unreleased]` (Fixed: ALG-2; Changed: fewer results at huge weights).

### 8. Test plan

- Remove both ALG-2 entries from `knownAPIFailures`. The list becomes empty: delete the mechanism or keep it
  empty (open question). Add `TestALG2API` (both reproductions; apicheck class: random rules with weights in
  [2^52, 2^62], `MinConfidence: 1` ⇒ every rule has Support == BodySupport, and a full comparison with the
  oracle). Each test must fail on main before the fix.
- `cmd/fpgrowth` `TestALG2CLI`: both printf reproductions, `-S`, `-F` and `-c100` at W ≈ 2^62.
- Boundary tables at W ∈ {2^52−1, 2^52, 2^52+1, 2^53±1, 2^61±1, 2^62, MaxInt64} for min, max, border,
  confidence and set min (body and `-o`), library and CLI, against the oracle and pinned values (W, 2^61,
  2^59+32).
- `internal/thresh`: a property test against big.Rat (random t including subnormal, 2^-k, nextafter(1), k/d,
  decimals; random N in [0, MaxInt64]); `FuzzFrac`.
- Regression sweep (deterministic) vs the copied C formula, which must be equal: 2-dp percents × W ≤ 600;
  seeded 3-dp percents and random floats with W < 2^36; computed `smin·100/total`; confidence grid × all
  supp, body ≤ 800; rule set min. Any exception must be listed and documented.
- Unchanged and must pass without regeneration: `TestFractionFormula` (x ≤ 2^48), `TestThresholds` (vs the
  new oracle; table cases B2/B9 unchanged), the goldens, and the difftest (`cd reference && ./build.sh && go
  test ./...`). Update `TestCountThresholds`, `TestPlanMapping` (conf units), and the oracle
  `threshold_test`.
- `checkRange` property: a rejected range has minAbs > maxAbs for exhaustive W ≤ 1000 and random large W.
  Plus a test that a satisfiable near-equal pair is accepted.
- Fuzz `FuzzMineItemsets`/`FuzzMineRules` for the CI smoke time; race; vet; staticcheck.

### 9. Performance plan

Thresholds stay once per run. The per-rule change is integer-only. Compare `BenchmarkMineRules`,
`BenchmarkSuiteRules`, `internal/istree` `BenchmarkReport`, `BenchmarkCLI` (rule cases), and
`BenchmarkMineItemsets` (control) against origin/main: `-count 8`, benchstat, `uptime` recorded. A
regression above 5 % triggers the float fast path (decide by the float product when it is outside a proven
2^-50 band, exact otherwise).

### 10. Open questions for the maintainer

1. Approve contract §3 ("upstream's tolerance in exact arithmetic, never a whole unit") over exact-decimal /
  round-to-nearest (§2 data)?
2. CLI percent → `p/100` as a float64 division (upstream's step; `-sk` ≡ `Fraction(k/100)` for all W), not
the exact rational p/100?
3. checkRange: keep the sound 4ε test (no new rejections) or tighten it to the exact `g(1+ε) < f(1−ε)`?
4. Empty `knownAPIFailures`: remove the mechanism, or keep it for future use?
5. `fpg.Config.Conf` switches to a fraction (internal only). Is that fine, or should a separate field be
added?

## Independent design challenge

## Challenge of design17.md (#17, ALG-2)

Reviewer: independent numerical-semantics critic. Base: origin/main ee75321 (clone in
scratchpad/review/design17). Experiments: scratchpad/review/exp17c (`go run .`; main() dispatches to
below/classify/libconf/intmax), big.Rat references.

Verdict: **approve the contract (§3) with changes.** The mathematics is sound. Two claims are wrong
(the "no change below 2^36" statement, and the integer path when t > 1), and those must be fixed before
implementation. No Critical or High findings.

### Verified (no finding)

- **Well defined and monotone.** For an integer s, `s > x−1` ⇔ `s ≥ ⌊x⌋`, so min = max(⌈x(1−ε)⌉, ⌊x⌋) ∈
  {⌊x⌋, ⌈x⌉}. Both terms are nondecreasing in x, so min is nondecreasing in t and in N. The same holds for
  max = min(⌊x(1+ε)⌋, ⌈x⌉). For an integer x, min = max = x.
- **Equivalence for x < 2^52.** For x < 2^52 the contract equals ⌈x(1−ε)⌉ (the proof in §3 is correct).
  The cap acts only when εx ≥ 1. Then min = ⌊x⌋.
- **checkRange stays sound.** min ≥ ⌈x(1−ε)⌉ ≥ fW(1−ε) and max ≤ gW(1+ε) still hold with the cap. So the
  argument in §7 is valid: RN(g(1+4ε)) < RN(f(1−4ε)) ⇒ g(1+ε) < f(1−ε) ⇒ min > max for every W ≥ 1
  (W = 0 gives 1 > 0). The test never rejects a satisfiable range.
- **The integer Min trick is exact.** min = q+1 ⇔ ⌊x⌋ < x(1−ε) ⇔ r·2^52 > X ⇔ r > ⌊X/2^52⌋. It matches
  big.Rat for t ∈ (0,1] (v5 test). Max for t ∈ (0,1): 0 mismatches in 2e5 cases with W up to MaxInt64
  (intmax.go).
- **Rule set minimum.** Because minConf(c,·) is monotone, setMin = minConf(c, bodyMin) is a valid
  pruning bound, and it is ≤ bodyMin for c ≤ 1.
- **Library MinConfidence** (today percentOf(f)/100·(1−ε), proposed exact f) changes nothing at ordinary
  weights. Exhaustive f = k/1000, body ≤ 300, all supp: 45,495,450 checks, 0 diffs. Random, 3-dp, a/b
  and nextafter(k/100) values of f with body < 2^31, 2.4 M checks: 0 diffs. 1 diff at body < 2^40
  (f=0.9120015405275446 supp=871414286650 body=955496507326).

### Findings

#### F1 (Medium): "no change below 2^36" is false; replace it with the provable bound
Targeted search (W chosen so that P·W ≡ ±1..3 mod 10^(d+2)) finds C-vs-contract differences below 2^36:
- `-s3.091`, W = 833815673989, x ≈ 2^34.59: C min/max 25773242483/25773242483, new 25773242483/**25773242482**.
- `-s16.01`, W = 1290957281599, x ≈ 2^37.59: C max 206682260784, new 206682260783.

All 36,389 differences found for x in [2^34, 2^42] (2-dp and 3-dp percents) agree with the **exact
decimal intent** (⌈P·W/M⌉, ⌊P·W/M⌋). C agrees with it in 0 of them. So every change is an improvement:
C's three roundings push the result beyond its own tolerance.

Necessary condition for a difference with a d-decimal CLI percent: |x_dec − n| ≥ 10^-(d+2), and this
must fit inside a band of about 4.5·2^-53·x. So x ≳ 2^53/(4.5·10^(d+2)): about 2^44.2 for integer
percents, 2^40.9 for 1 dp, 2^37.5 for 2 dp and 2^34.2 for 3 dp. The searches agree: 0 diffs for d = 0
and 1 up to 2^40 (1.6 M targeted samples); the smallest diffs are at 2^37.59 (2 dp) and 2^34.59 (3 dp).

Required changes:
- §4 and the docs must give this bound, not the empirical 2^36. Example wording: "results differ from C
  only where an integer lies within about 2.5·2^-52·x of x. For d-decimal percentages this needs
  x ≥ ~2^53/(4.5·10^(d+2)). In every observed case the new result is the exact decimal one."
- The issue's acceptance criterion says results are unchanged below 2^52 "unless a change is
  documented". These cases must therefore be documented in differences.md N2 and in CHANGELOG.
- Add the targeted counterexamples above as pinned tests.

#### F2 (Medium): the integer path overflows for t > 1 (CLI `-S` > 100, `-F` > 100)
§5 saturates only in the k ≤ 0 branch. A non-integer t > 1 (for example 1.5, nextafter(1,2) or
229388.4) takes the k > 0 path, where q = X >> k can reach 2^116. Transcribing §5 literally gives
108,678 mismatches in 400,000 cases against a saturating big.Rat reference. Example: t = 1.0000000000000002,
W ≈ MaxInt64 gives −9223372036854774228. The k ≤ 0 branch `lo << -k` also overflows.

Fix: saturate every result to MaxInt64 when q.hi ≠ 0 or q.lo ≥ 2^63 (including the q+1 step). Cover
t > 1 in both Min (border) and Max in the property test and the fuzz test.

#### F3 (Low): `thresh.New` must handle special values before Frexp
- `Frexp(+Inf)` gives m = 2^64−1, k = 53 (garbage).
- −0 and negative values give m = 0.

`-S inf` is valid on the CLI. `-S-0`/`-F-0` arrive as −0 through the percent branch (`v < 0` is false
for −0). Specify the cases explicitly: t ≤ 0 (including −0) gives 0; +Inf gives MaxInt64 (Max) and
saturating Min; NaN panics or is rejected earlier. Test each one.

#### F4 (Low): wording of the Fraction(k/100) ≡ `-sk` claim
The equivalence holds for integer k. For a decimal p it holds only when p/100 is a run-time float64
division: `Fraction(p/100)` with p a float64 variable. A Go constant expression such as
`Fraction(33.3/100)` rounds the exact rational once, which can differ from the CLI's RN(RN(33.3)/100).
The GoDoc should say this.

#### F5 (Low): sweep coverage
§8 has no old-vs-new sweep for the library `MinConfidence` path (percentOf) or for `countThresholds`
(the HasCounts path the library always uses). Add both, with the data above as the baseline. Also add
the targeted P·W ≡ ±r (mod 10^(d+2)) generator from F1. Random sampling under-samples the only region
where differences occur (about 1 per 1e5 samples at 2^40).

#### F6 (Low): hot-path cost
The per-rule Min costs about 1 Mul64 plus shifts. That is probably negligible next to getsupp, but §9
correctly requires a benchmark. Simplest option: precompute m and k in `Tree`. No float fast path unless
the benchmark asks for one.

### Intent (task point 2)
- **Exact `s ≥ x`** breaks ordinary use: RN(0.1)·10 = 1.0000000000000000555 gives a minimum of 2.
- **The cap `s > x−1` alone** also breaks ordinary use: Fraction(0.25) of W = 10 gives 2 instead of 3.
- **The (1−ε) tolerance** is still needed with exact evaluation. It absorbs the representation error of
  t: up to u for a decimal, about 2u for a computed p/100.
- **Decimal semantics** are rejected with data (§2).

The contract keeps C's purpose, never exceeds one unit and is exact at integer x. This is principled.
The remaining limitation is honest and documented: beyond 2^53, t is only as precise as its float64
(Fraction(0.1) of 10·2^59 gives 2^59+32), and `Count` is the exact alternative.

### Answers to the open questions
1. **Approve the contract,** with the F1 wording (bound plus "differences are decimal-exact
   corrections").
2. **Yes, use float64 `p/100`.** It is upstream's step and makes `-sk` ≡ `Fraction(k/100)` for all W.
   An exact rational p/100 would make CLI and library diverge, and it fixes nothing that the
   tolerance does not already cover.
3. **Keep the 4ε test.** It is proven sound (it never rejects a satisfiable range). Tightening it only
   rejects more ranges that are already empty and adds churn.
4. **Keep the `knownAPIFailures` mechanism** with an empty list (or a comment saying so). It costs
   nothing and future audits can reuse it. Removing it is also acceptable; this is not a correctness
   question.
5. **Switch `fpg.Config.Conf` to a fraction, and rename the field** (for example `ConfFrac` or a
   `thresh.Frac` value). The rename makes every stale percent-unit caller (CLI `minerConfig`, tests,
   difftest harness) fail to compile instead of silently giving results 100× off.

### Other points
- `-d`/Threshold is a measure threshold and is correctly out of scope (#16).
- `-m`/`-n` are unaffected.
- Negative CLI absolutes stay with #18. Agree on `thresh.Frac` versus an exact `Count` spec there.
- With `-o`, confidence still uses N = body, and the set minimum is the support minimum (as §3 says).
- The oracle must use only big.Rat (it does in the design). Keep the C formulas only in a test helper.
