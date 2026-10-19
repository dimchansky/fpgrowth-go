# Design and design challenge for #16

The design proposal (implementer) and its independent challenge, written before implementation. The binding decisions taken from them are listed in the PR description.

## Design proposal

## Design: #16 (ALG-1 + ALG-6) numerically stable statistical measures

Base: origin/main 0fd4a0a. Notation: n = base, s = supp, b = body, h = head,
D = s·n − b·h (exact, |D| < 2^126), ε = 2^-52, u = ε/2.
Evidence below comes from a throw-away prototype test (internal/ruleval,
deleted again) run against `internal/oracle` and `testdata/measures_c.txt`
(2,461 rows, n up to 2^31−1). Every number must be re-measured on the real
implementation.

### 1. Root causes (error analysis)

| measure | current formula | failure mechanism |
| --- | --- | --- |
| n Chi2, y Yates | `float(h)·float(b) − float(s)·float(n)` | each product is rounded once it exceeds 2^53; abs. error ≤ u·(hb+sn) ≈ u·n², so the rel. error of D is u·n²/\|D\|. Cancels to 0 for the perfect association (both products round to the same float). Already happens in the C fixtures: (123456787,123456788,123456788,123456789) upstream chi² = 0, exact 6.56e-17; (1073741823,1073741823,2147483646,2147483647) upstream Yates 1e-28, exact 0 (clamp). |
| p, t | `chi2cdfQ(n·chi2)` | inherit D's error; Q's own condition number is ~x/2 (rel. error ≈ ε·x for x carrying ε rel. error), which is inherent and accepted by the oracle (pTau·(1+x)). |
| i Info | `(ln n + Σ t·ln(t/(r·k))/n)/ln2` | absolute error ≈ few ε·ln n regardless of n (terms of size ln n cancel to I). Relative error unbounded as I → 0; this is ALG-6 (wrong already at n = 9000 under the oracle's relative 2^-44), and at n ≥ 1e14 I collapses to 0. |
| g InfoPValue | `chi2cdfQ(2·ln2·n·I)` | δG = 2n·δI ≈ 30ε·n·ln n absolute; near G = 0 the tail has slope ∝ G^-1/2, so p = 0.99999996 at exact independence and 1 for strong associations at n ≥ 9e14. |
| z Certainty | `(s/b − h/n)/(1 − h/n)` | abs. error ≈ 4u·n/(n−h): 1 − p cancels; h/n rounds to 1 for n−h ≪ n·u ⇒ 0 instead of 1. The negative branch `/p` is always ≤ ~6u absolute (all n). |
| f h m s Fisher | `exp(com − Σ4 lnΓ)`, com = Σ5 lnΓ | abs. error of ln P ≈ 43u·lnΓ(n+1) (9 terms of size up to lnΓ(n+1), Lanczos + sums) ⇒ rel. error of every P ≈ 2.4ε·lnΓ(n+1) (fixtures: 0.27 of ε·(9lnΓ+1)). 1e-8 at n = 1e6, 1e-5 at 1e9, meaningless at 1e15; prototype sweep found a rel. error of 1e56 at large n. h additionally uses `exs = float(h)·float(b)/float(n)` to find the mirror table (wrong boundary ⇒ a whole table probability lost/added at large n). |
| -i (istree `Tree.value`, oracle Func mode) | `float(s)·float(n) > float(h)·float(b)` | products rounded beyond 2^53 ⇒ wrong invalidation (root `TestKnownAPIFailures` case 3). |

Not affected: the rational measures (c d l a q v e r b k w u j) and
LogSupportRatio (`report.LogRatio`: a sum of log2 quotients of int64 values;
absolute error ≤ few ε·Σ\|log2(f/n)\| at every n, no product cancellation;
this is the oracle's tolerance already). isrout `%l` lift is a rational
product/quotient (relative error only). The CLI only calls the library.

### 2. Candidates

- Plain float64 (current): fails as above.
- Stable rearrangements: (a) D exactly via `bits.Mul64`/`Sub64` (2 Mul64 + 2 Sub64,
  ~1 ns); (b) information as a sum of non-negative terms (below); (c) certainty
  as D/(b·(n−h)) or D/(b·h); (d) Fisher ln P via Stirling with exact deviations.
- Exact 128-bit integers (`math/bits`): D, 2\|D\|−n, −i, Fisher h boundary. Enough:
  every quantity that cancels is a degree-2 polynomial in int64 values.
- `math/big`: never needed on the hot path (only the existing Fisher tie fallbacks).
- Hybrid: keep the current float code where it is provably bit-identical or
  accurate to the documented bound, use the stable form elsewhere.

### 3. Proposed choice per measure

#### 3.1 Chi2, Yates, their p-values, -i (exact D)
- `crossDiff(s,b,h,n) (neg bool, hi, lo uint64)`; convert |D| to float64:
  `float64(lo)` when hi = 0 (correctly rounded by Go), else
  `float64(hi)·2^64 + float64(lo)` (rel. error < ε). Chi2 = t²/(h·(n−h)·b·(n−b))
  with the same denominator expression as now. Yates: compute 2\|D\| − n in 128
  bits; ≤ 0 ⇒ 0 (B10 clamp decided exactly), else t = 0.5·float(2\|D\|−n).
  p-values keep `chi2cdfQ(float64(base)*Chi2(...), 1)` unchanged.
- Bound: relative ≤ 8ε (measured 3.3 ulp chi², 2.8 ulp Yates over 63,484
  tables up to 2^62, 0 oracle mismatches). p-values: rel. ≤ 8ε·(2+x) + GammaQ's own.
- Bit-identical to current (= C -ffp-contract=off) **whenever s·n ≤ 2^53 and b·h ≤ 2^53**
  (then both float products are exact and the float subtraction is the correctly
  rounded exact D; n/2 is exact for n < 2^53). Measured: 0 bit differences in such
  rows. Otherwise the value is the correctly computed one.
- -i: `Tree.value` and `oracle.Measure` Func mode use the exact 128-bit
  `aboveExpected` (already in the oracle; move it to a shared spot, e.g. ruleval
  `AboveExpected`). Identical decisions when both products ≤ 2^53.
- Cost: measured 3.61 → 3.61 ns (Chi2). No allocation.

#### 3.2 Information and G-test (fixes ALG-6 completely)
- With E_c = r_c·k_c/n per cell and δ_c = ±D/(r_c·k_c) (signs + − − + for
  the cells s, b−s, h−s, n−b−h+s; only the one D is needed):
  n·I = S = Σ_c E_c·g(δ_c), g(δ) = (1+δ)·ln(1+δ) − δ ≥ 0, valid since Σ(c − E) = 0.
  All terms are ≥ 0: no cancellation in the sum; S = 0 exactly iff D = 0.
- g: \|δ\| < 1/8: series δ²·Σ_{k≥2}(−δ)^(k−2)/(k(k−1)) (19 terms, Horner);
  δ < −1/2: with x = c/E (c = 0 ⇒ term E), x·ln x + (1−x) (no cancellation);
  else (1+δ)·log1p(δ) − δ (cancellation factor ≤ 2/\|δ\| ≤ 16). Note: computing
  x and δ independently in the direct form costs ~400 ulp; use δ there.
- Info = S/n/ln2 (keep the clamp, now a no-op); InfoPValue = chi2cdfQ(2·S, 1).
- Bound: relative ≤ 32ε (measured worst 9.9 ulp over 63,484 tables incl.
  exhaustive n ≤ 14, boundaries, random up to 2^62; G p-value 0 mismatches,
  worst 1.4e-4 of the oracle tolerance). G at exact independence is exactly 0 ⇒ p = 1.
  ALG-6 near independence: δG/G ≤ 32ε ⇒ |δp| ≤ f(G)·δG ≤ 32ε·√G, i.e. fixed.
- Not bit-identical to C on ordinary data (last bits of essentially every
  non-trivial row). Unavoidable: the old formula's absolute error can never meet a
  relative bound. Fixtures: new-vs-upstream max 2.2 units of ε·(1+ln n), class
  tolerance 8 ⇒ `TestEvalMatchesUpstream` passes unchanged; InfoPValue passes
  unchanged too (prototype). This is the F-1 class (log-based values) already.
- Cost: 32 → ~50 ns (+40–60 %, under load 7). Mitigations to try: precompute
  1/n, shorter series with 1/16 threshold, share D with nothing else. Report the
  BenchmarkEval/Info and -ei/-eg rule numbers; likely > 5 % ⇒ maintainer decision.

#### 3.3 Certainty (gated hybrid)
- Exact form: c ≥ p ⇔ D ≥ 0; cert = D/(b·(n−h)) (D > 0) or D/(b·h) (D < 0).
- Gate: use the exact form only when D > 0 and (n−h) < n>>10; otherwise the
  current formula (its abs. error there ≤ 4u·2^10 + 6u ≈ 2^-41; the negative
  branch ≤ 6u for all n). B5 (h = n ⇒ 0) unchanged.
- Bound: absolute ≤ 2^-40 (cert ∈ [−1, 1]); measured worst 1.2e-13 ≈ 2^-43
  over 200k tables up to 2^62 with h near n.
- Bit-identical unless D > 0 and n/(n−h) > 1024. Fixtures: 2 rows change
  (gate 2^12: 0 rows, bound ~2^-38; gate 2^6: 7 rows). Always-exact would change
  1,285 rows: gratuitous, rejected.
- Cost: unchanged on the float path (+1 compare); exact path ~same.

#### 3.4 Fisher tests (accurate method, no enforced limit)
- Old path unchanged for n ≤ 170 (= maxFact: every lnΓ argument ≤ 171 is a
  table lookup; fast and bit-identical; error ≤ 43u·lnΓ(171) ≈ 2^-38).
- New path for n > 170:
  1. Accurate anchor ln P(x) by Stirling with exact deviations (derivation:
     ln m! = m ln m − m + ½ln(2πm) + σ(m); the m ln m terms sum to −S(x) of 3.2,
     the linear terms cancel):
     ln P(x) = −S(x) + ½(3−k)·ln 2π + ½·ln(Π margins/(n·Π_{c>0} c))
               + Σσ(margins) − σ(n) − Σ_{c>0} σ(c),
     k = number of non-zero cells, σ = stirlerr (table for m < 16, 5-term
     series beyond). Margin terms are per evaluation, not per table.
     Measured (per-term prototype, f and s, 6,761 tables ≤ 300 tables each, n to
     2^62): 0 oracle mismatches, worst rel. 598 ulp (≈ 2^-42.8); current code:
     9 (f) and 2 (s) mismatches, worst rel. 1e56.
  2. Speed: walk from the mode m0 = ⌊(b+1)(h+1)/(n+2)⌋ outwards with the exact
     ratio P(x+1)/P(x) = (b−x)(h−x)/((x+1)(rest+x+1)) in float64 (~6u per step),
     re-anchoring every 32 steps and whenever P < 2^-1000 (avoids underflow
     loss). P ≤ 1 so no overflow. Stop a side when P underflows to 0: P is
     unimodal, so all further terms are 0 in float64 anyway (the old code adds
     exp(...) = 0 there too) — the value does not change, the cost drops to
     O(min(min margin, ~40σ)) where σ is the hypergeometric s.d.
  3. Neumaier compensated summation (bound independent of the table count).
  4. f: P(x) ≤ P(s) decided with a per-table relative bound, else the existing
     exact `cmpWeight`; m: I(x) ≥ I(s) from the same S(x) with a relative bound,
     else the existing exact `cmpXLogX`; h: the mirror boundary
     ⌈(2hb − s·n)/n⌉ / ⌊…⌋ in 128-bit (`bits.Div64`, quotient < 2^64) instead of
     `exs`; s: unchanged ranges. Complement forms (h, s) stay (absolute accuracy).
- Bound: f, m relative ≤ 2^-36; h, s absolute ≤ 2^-36 — for every n, plus an
  absolute 2^-1000 for p-values below that. (Old path ≤ 2^-38; new path
  measured 2^-42.8 per-term; recurrence adds ≤ 192u.) Prototype against the
  fixtures: new path within 0.23 of 1 unit even for all rows ⇒
  `TestEvalMatchesUpstream` passes unchanged; old rows n ≤ 170 identical.
- Cost: per-term prototype 4.2 → 4.5 ms at (35000,60000,70000,1e6) and
  2.5 → 5.7 µs at BenchmarkEval's (35,60,70,1000) — hence the recurrence, which
  should make every n > 170 faster than today (~5–8 ns/table vs 25–140 ns).
  Fallback if the recurrence is judged too complex: per-term anchors only
  (accurate, but ~2× slower than main at n ≈ 1e3 ⇒ needs maintainer acceptance).
- Limit: none enforced. Values are accurate at every n; only time grows, and it
  is already cancellable (AbortFunc, ctx in the root API, signal in the CLI).
  Balanced margins: σ ≈ √n/4, so n = 1e12 takes ~1e7 tables (~0.1 s), n = 1e18
  ~1e10 (minutes). GoDoc of `Measure`/`AbortFunc` states the cost model.
  `cmpRangeProducts` can build huge big.Ints for near-ties of distant tables at
  very large n (pre-existing); the much smaller new tolerance makes it rarer.
  Note it in the report; do not fix here unless a test hits it.

### 4. Bit-identity summary and fixture impact

| artefact | expectation |
| --- | --- |
| `measures_c.txt` classExact Chi2/Yates | rows with s·n or b·h > 2^53 change: 61 Chi2, 52 Yates (+26 Chi2PValue, 18 YatesPValue beyond class tol), all n ≥ ~1e8 where C is wrong. `fixedUpstreamValue` gets a deviation "N1 fixed" that compares them with an exact big.Rat (Chi2/Yates, ≤ 8ε rel.) and with chi2cdfQ(exact x) (p-values); upstream's value must be shown to differ (like B4–B11). |
| Certainty | 2 rows (n/(n−h) > 1024), same mechanism. |
| Info, InfoPValue, Fisher | values change in the last bits, tolerances unchanged (measured margins 3.6× and 4×). |
| CLI goldens | expected byte-identical (test1.tab etc. are tiny; `ts-ez-i`, `tg-ey` stay on bit-identical paths). Verify; regenerate nothing silently. |
| difftest | expected to pass; risk: weightsLarge totals up to ~2^33 give products > 2^53 where C's chi²/p near independence or -i decisions are wrong; if that exceeds relTol, add a documented deviation class, never a blanket tolerance. Report DIFFTEST_SUMMARY before/after. |

### 5. Supported range contract (document in `Measure` GoDoc, README, N1)

Every valid table with 0 ≤ supports ≤ math.MaxInt64 (all intermediate
products < 2^127, floats < 2^253): chi², Yates rel. ≤ 8ε; p-values
rel. ≤ 8ε·(2+x) (x the statistic; underflow to 0 beyond x ≈ 1490);
information rel. ≤ 32ε; certainty abs. ≤ 2^-40; Fisher f, m rel. ≤ 2^-36 and
h, s abs. ≤ 2^-36 (time O(min(min margin, 40σ))); -i exact. No new error
conditions, no NaN for valid tables. Coordinate with #17 (thresholds at large
weights) in one "Numeric contract" section of `docs/differences.md`.

### 6. Docs

`docs/differences.md`: replace N1 by (a) a row "statistical measures exact
beyond 2^53 / differ from C where C cancels" (with the fixture counts above and
tests), (b) info/G and Fisher (n > 170) last-bit differences (F-1 class),
(c) certainty gate, (d) -i exact, (e) Fisher h mirror boundary exact. Dated note
in `docs/audit/algorithms.md` ALG-1/ALG-6 (no rewrite). CHANGELOG `[Unreleased]`
Fixed + Changed (values, possible threshold-membership changes), Known
limitations entry removed/narrowed; `Measure`/`Evaluation`/`AbortFunc` GoDoc;
README; ruleval package doc (the "bit-identical" paragraph becomes conditional).

### 7. Test plan

Remove (strict tables fail until removed):
- `internal/ruleval` `knownFailures` and `knownFragile` entirely (all entries are
  ALG-1/ALG-6); `TestKnownMeasureFailures` becomes `TestLargeTablesMatchReference`
  (every comparison matches, keeps the "too close to tolerance" guard).
- `TestMeasuresMatchReference`: drop the ALG-6 acceptance and the
  "no ALG-6 mismatch left" error. `TestMeasuresExtendedLarge`: strict.
- Root `knownAPIFailures` entries 0–3 (ALG-1) and `wantKnownAPIDeviations` 0–3;
  the ALG-2 entries stay for #17. `TestALG6Exemption` → strict regression
  (independent table ⇒ InformationPValue exactly 1).
- `internal/oracle`: `DefectALG6`, `alg6InfoErr`, the defect fields of
  refInfo/refInfoPValue; Func-mode -i becomes exact. Open: keep or delete the
  generic KnownDefect/DefectTol/DiffOptions.Defects machinery (now unused).
New regression tests (shown failing on main first):
- ALG-1 table k = 1e3…1e18 for n p y t i g z f h m s against the oracle, plus the
  audit's exact values (chi² p 0.0455003, G p 0.0360561).
- CLI: `chi_big.txt` -ep/-eg/-ef, perfect association -en/-ey -d0, certainty
  -ez -d0 (cmd/fpgrowth test running the program, values within the bounds).
- -i: s = 1e18+1, b = h = 3e18, n = 9e18 (root API and istree).
- Bit-identity properties vs a test copy of the old formulas: Chi2/Yates/-i when
  products ≤ 2^53; certainty outside the gate; Fisher n ≤ 170 (random + exhaustive).
- Fisher: new path vs oracle at large n with ≤ 1024 tables; old vs new path for
  171 ≤ n ≤ 1e5 within the old bound; early-stop value == full sum; B6 tie tests
  on the new path; AbortFunc still stops and matches Func.
- Information: relative error vs oracle ≤ 32ε over the int64 grid; exactly 0 at
  independence; g/series/direct branch boundaries (δ = ±1/8, −1/2, c = 0).
- `TestEvalDoesNotAllocate`: add Chi2, Yates, YatesPValue, InfoPValue,
  Certainty and large-n inputs (new Fisher path, n = 2^62).
- Oracle sweeps: default, `FPG_MEASURE_CASES=200000` extended (report counts).
Benchmarks (count ≥ 6, benchstat, `uptime`): `BenchmarkEval` (add Yates,
Certainty, InfoPValue, Fisher at n = 1e6 and n = 2^40), `BenchmarkSuiteRules`,
reference/bench rows such as dense-r-lift, vs origin/main and v0.1.0.

### 8. Open questions

1. Accept Info/InfoPValue being ~40–60 % slower per call (no stable formula is
   as cheap as the cancelling one), if optimization does not close the gap?
2. Fisher: recurrence with mode walk (faster, more code) vs per-term anchors
   (simpler, ~2× slower at n ≈ 1e3)? Proposal: recurrence.
3. Certainty gate 2^10 (2 fixture rows change, bound 2^-40) vs 2^12 (0 rows,
   bound ~2^-38)?
4. Oracle: delete the now-unused known-defect machinery or keep it for #17?
5. Should a hard Fisher table limit (error) still be added for callers of
   `Measure.Func` without abort? Proposal: no (accurate, documented cost).

## Independent design challenge

## Challenge: design16.md (#16, ALG-1 + ALG-6)

Reviewer: independent numerical-analysis challenge, read-only. Base: origin/main
0fd4a0a, cloned to `scratchpad/review/design16`. All experiments are in two
throw-away test files in that clone (`internal/ruleval/zz_proto_test.go` is the
design's prototype, unchanged; `internal/ruleval/zz_review_test.go` is mine). Every
number below comes from `go test -run 'TestZZ|TestRV' -v` or `-bench` there
(darwin/arm64, go1.27.2, load average about 4).

**Verdict: sound with changes.** The formulations for chi², Yates, -i, certainty and
information are correct, and their bounds hold in my own stress tests at the int64
limits. The Fisher part is the right idea (Stirling anchors with exact
deviations, plus a ratio walk). It has two High problems: an unbounded exact
tie-break that the faster walk now makes reachable, and a false "fixtures pass
unchanged" claim. It also has gaps in specification and testing. The 40–60 %
information slowdown is not inherent.

### Verified (no change needed)

- **Exact D (crossDiff)**: correct for every valid table up to MaxInt64. s·n and b·h
  are below 2^126 and 2|D| is below 2^127, so 128-bit unsigned arithmetic is enough
  and the Yates quantity 2|D| − n cannot overflow.
- **Chi² and Yates bit-identity when both products are at most 2^53**: proven. The
  float products are then exact, and the subtraction correctly rounds an exact
  integer. For Yates, `|D| − n/2` is a half-integer: when it needs 54 bits, the old
  float subtraction and the new `0.5·float64(2|D|−n)` correctly round the same exact
  value (scaled by an exact 2), so they agree. Clamp equivalence is exact.
- **Measured against the fixture rows** (`TestRVChangedRows`): the new value is never
  less accurate than C for Chi2 (61 changed rows), Yates (52), Chi2PValue (26) or
  YatesPValue (18). One Yates row is 1 ulp worse. The design's counts are
  reproduced.
- **Information**: the identity `n·I = Σ E_c·g(δ_c)` with δ = ±D/(r·k) is correct,
  since Σ(c − E) = 0. All terms are non-negative.
  - No subnormals for valid tables: E ≥ 1/n and every term is at least about 2^-190.
  - The `c = 0 ⇒ E` and `δ < −1/2 ⇒ x·ln x + 1 − x` branches are necessary. Forming
    `log1p(diff/E)` naively gives NaN at (3, 1.52e18, 1.13e18, 2.88e18) (my
    `bd0InfoNats` without that branch).
  - Stress test (`TestRVInfoStress`): 36,592 tables at n = MaxInt64, MaxInt64−1,
    2^62, 2^53±, empty cells, near independence, and δ ≈ −0.999, −1/2, ±1/8.
    Prototype worst error is 10.3 ε, with 0 G p-value mismatches. The current code
    is off by up to 1.6e43 ε.
- **Stirling anchor for ln P**: I re-derived it independently, including the
  (3 − k)/2·ln 2π count when cells are empty and the cancellation of the linear
  terms. Truncating stirlerr at m ≥ 16 after 5 terms leaves about 1e-16 absolute.
  - Note that the anchor's relative error is ≈ 32ε·S(x) + O(ε). At the underflow
    edge (S ≈ 745) that is about 2^-37.5, so the 2^-36 claim holds with little
    margin for the tables that matter.
- **Certainty**: the exact form D/(b(n−h)) or D/(b·h) is correct. The gate analysis
  is correct: the error is ≈ 3u·n/(n−h) on the positive branch, and the negative
  branch stays at a few u. Measured: 2 fixture rows change at a gate of 2^10, 0 at
  2^12; worst error at 2^10 is 1.23e-13.
- **G p-value** from `chi2cdfQ(2S, 1)`: an exactly independent table gives exactly 1,
  so ALG-6 is fixed. The worst relative error of the chi² p-value (2.3e-6) is at
  p = 1.95e-317 (subnormal), so it is covered by the absolute 2^-1000 term and is
  not a defect.

### Findings

#### H1 (High): exact Fisher tie-break has unbounded cost, now reachable

`cmpWeight` → `cmpRangeProducts` builds a `big.Int` product of up to |x − s|
consecutive 63-bit integers whenever P(x) is within the float tolerance of P(s).

Measured (`TestRVCmpRangeCost`): l = 1e4 takes 19 ms, l = 1e5 takes 0.69 s and
l = 1e6 takes **29 s**. The growth is superquadratic. The call is not abortable,
because AbortFunc is only checked between tables.

Today this cannot happen, because the linear old algorithm never finishes at such n.
With the mode walk, n = 1e12…1e18 becomes feasible:

- A mirror table on the other side of the mode sits at distance l ≈ 2d ≈ 2σ…10σ.
- The chance that some table lies within relative τ = 2^-36 of P(s) is about
  2τσ²/d ≈ 3e-11·σ. That is about 1e-5 per evaluation at n = 1e12 (σ ≈ 2.5e5,
  l ≈ 5e5, ≈ 10 s) and **about 1e-2 at n = 1e18** (l ≈ 5e8, effectively a hang or
  out-of-memory).
- Exact symmetric ties are fine, because equal cells short-cut.

**Required**: bound the cost of the tie-break. Options:

- (a) Evaluate ln(W(x)/W(s)) as a difference of the two Stirling anchors in
  double-double, or with big.Float at about 128 bits (cost O(1)), and decide when
  |Δ| exceeds its bound.
- (b) Call `cmpRangeProducts` only if l ≤ about 2^12.
- (c) Otherwise count the table as tied, i.e. include it. That is conservative (the
  p-value gets larger), the error is below 2^-100 relative, and it is documented.

The same pattern already exists for `m` (`cmpXLogX` is capped by
`maxSelfPowerPrec`). Add a regression test with a near-tie at n ≥ 1e15.

#### H2 (High): "TestEvalMatchesUpstream passes unchanged" is false for Fisher with n > 170

`fisherFragile` first checks `Eval(m) != clampUnit(upstreamFisher(m))`. If that
holds, the row goes to `fisherOracle`, which for n > 64 **replays the old
logGamma summation** and is compared with **tol = 0**. The prototype's fixture check
(`TestZZFisherFixtures`) missed this, because `fixedUpstreamValue` calls the
*current* `Eval` internally.

With the new value substituted (`TestRVFisherHarness`, FisherProb only): of 919
rows with n > 170, 222 are bit-equal, 701 are classified fragile, and **697 would
fail**.

**Required**:

- Rework `fisherFragile` so that it compares *decisions*: the replicated upstream
  inclusion set against the exact one, not the values.
- Make `fisherOracle` for n > 170 an exact or tolerance-based value, not a replay
  of the old code.
- Re-measure. Also check `TestFisherLargeTiedTable`, `TestSymmetry` (Fisher
  complement invariance, bitwise?) and `TestAbortFuncMatches` against the new path.

#### M1 (Medium): the early-stop rule must be specified as "moving away from the mode"

The design says "stop a side when P underflows to 0" and "s: unchanged ranges". If
each range is walked from its own end, this is wrong.

Counterexample (`TestRVFisherWalk`): FisherSupport (523678, 524215, 524621, 525540).
The complement range [0, s−1] contains the mode. Walking down from s−1, the first
term underflows, which gives 1 − 0 = **1 instead of 0**.

My mode-outward implementation, which starts at clamp(mode, a, c) and stops only when
moving away from the mode, matched the oracle on 3,149 tables at n ≤ 2^62 (worst
error 1.0e-14).

Specify for every range [a, c] of every variant: start at clamp(mode, a, c), and
stop only on the side moving away from the mode. Add tables to the "early stop ==
full sum" test where s is far beyond the mode and the summed range contains the mode.

#### M2 (Medium): the information slowdown is not inherent

On this machine the current code takes 31 ns for (350, 600, 700, 10000) and the
prototype takes 43–59 ns (+37–87 %).

A tuned variant (`fastInfoNats`) runs at the **same speed as main: 31.3 / 31.2 /
31.8 ns** against 31.5 / 32.5 / 31.4 ns. Its changes:

- coefficients 1/(k(k−1)) from a table, so Horner has no divisions;
- the series only for |δ| < 1/16, with 13 terms;
- a single 1/n multiplication;
- the four cells unrolled, with no struct array.

Its accuracy is 21.4 ε worst on random tables and 28 ε in a stress test dominated by
the middle branch (the prototype gives 15 ε there).

So keep the bound ≤ 32ε but use the 1/8 threshold with a coefficient table, or an
atanh/bd0-style series in v = δ/(2+δ), which converges as v², has no cancellation
up to |δ| = 1/2 and needs about 8 terms for |δ| < 1/8.

A hybrid (old formula when a Pinsker-type lower bound 8D²/n⁴ shows I is large) is
**not recommended**. It could only guarantee about 2^-30 relative, it keeps two
paths to test, and it does not buy speed once the stable path is tuned. Target: no
regression in BenchmarkEval/Info at the 5 % level.

#### M3 (Medium): drop the complement forms on the new Fisher path (relative accuracy for free)

Upstream picks `1 − Σ` by the *count* of tables. With the truncated walk, both tails
cost O(σ), so the count no longer matters. Today a tiny upper tail computed as a
complement is only accurate in absolute terms (`TestRVTinyTail`):

| table | complement | exact |
| --- | --- | --- |
| (400, 1000, 1e5, 1e6) | 1.1e-16 | 1.3e-137 |
| (300, 1000, 1e11, 1e12) | 1.1e-16 | 6.8e-69 (main gives 0.0084) |
| (60, 200, 1e4, 1e5) | 2.9e-15 | 3.4e-15 (15 % off) |

For n > 170, sum the included tables directly, walking from the mode outward. h and
s then get the same **relative** bound as f and m. It also removes B9 (negative
complements) on that path and simplifies the contract to "all four relative ≤ τ".
Values change only by about ε in absolute terms, which fits the existing fixture
tolerance.

#### M4 (Medium): truncation by underflow plus re-anchoring below 2^-1000 is costly; use a rigorous relative tail bound

Re-anchoring at every step while 2^-1074 < P < 2^-1000 costs about 1.35σ anchors
(around 80 ns each) per tail. At σ = 1e8 that is 10+ s.

The walk to underflow (≈ 38σ per side; measured 34σ in `TestRVFisherWalkCost`) is
also about 4× longer than needed. P is log-concave, so beyond x the tail is at most
P(x)·r/(1−r), where r is the current ratio (below 1 when moving away from the mode).

Stop when P(x)·r/(1−r) ≤ 2^-60·(running included sum). That is rigorous, keeps the
relative accuracy (M3), costs about 9σ per side, and needs no subnormal handling.
Keep the 2^-1000 absolute floor only for p-values that underflow as a whole.

#### M5 (Medium): frame the chi²/Yates fixture exception as a scope of classExact, not as "C is wrong"

Of the 61 Chi2 rows that change, **25 have a C value within 8ε of the exact value**
(27 of 52 for Yates differ by more than 8ε, so 25 do not). The C value is not wrong
there; it is just rounded differently. "Upstream's value must be shown to differ
(like B4–B11)" is true but frames rounding noise as a bug.

The principled rule follows from the proof above: classExact (bit-identical)
applies **exactly when s·n ≤ 2^53 and b·h ≤ 2^53**. Beyond that:

- Chi2/Yates are compared with the exact big.Rat at 8ε relative.
- Their p-values are compared with chi2cdfQ(exact x) within the class tolerance.

The new value is checked against the truth, not against C. Assert separately that
the rows where C exceeds the bound (36 Chi2, 27 Yates) are listed in differences.md
as the evidence for N1.

#### M6 (Medium): oracle tolerances are too loose to show the claimed bounds

The claims are information ≤ 32ε (2^-47), chi²/Yates ≤ 8ε (2^-49), Fisher ≤ 2^-36.
The oracle's checks are looser:

- `ratTau` = 2^-44 relative for i, n and y;
- Fisher: `fisherTol` = min(pTau·(1 + lnΓ(n+1)), 2^-20), which is **2^-20 relative
  for every n ≳ 1e5**. A Fisher implementation off by 1e-7 would pass.

AGENTS.md says "never claim … without a command whose output shows it". The new
regression tests must assert the documented bounds directly, as relative error
against `Value.V`. Tighten the oracle's `fisherTol` (and possibly `ratTau` for
i, n and y) to the new contract, so that a later regression cannot hide under the
old loose tolerance.

#### M7 (Medium): FMA contraction and cross-platform determinism

The Go spec allows fusing `x*y + z`, and arm64 does it unless the code has explicit
`float64()` conversions. The current code relies on such conversions for
bit-identity.

- Paths that must stay bit-identical (Chi2/Yates denominator, certainty float
  branch, `|D|` conversion): keep the explicit conversions.
- New code (Horner series, Stirling sums, the walk): it will be contracted on arm64
  and not on amd64, so CLI outputs that print many digits can differ by platform.
  Either use `math.FMA` deliberately or wrap the products in `float64()`. Then add
  the CLI `-ei/-eg/-ef` cases to the goldens only with a tolerance or a coarse
  print format.

#### L1 (Low): the 32ε information bound is measured, not derived

The middle branch `(1+δ)log1p(δ) − δ` has a rounding cancellation factor of up to
about 17 at |δ| = 1/8, which gives roughly 24–33 ε in the worst case. Measured: 15 ε
for the prototype, 28 ε for a 1/16 threshold. Write the derivation down (or use the
atanh series of M2, which removes the cancellation) before putting "32ε" into
GoDoc.

#### L2 (Low): smaller items

- `u128f` is not correctly rounded (≤ ~1.5 ulp). That is harmless, but correct
  rounding costs about 3 ns (`bits.Len64`, shift, round-to-nearest-even) and would
  make "the value is the correctly rounded D" a true statement.
- Mode `⌊(b+1)(h+1)/(n+2)⌋`: in float it is off by up to about 2^-53·b ≤ 1024
  tables. That is harmless because σ² ≥ b²/(2n) ≥ 2^42 whenever b ≥ 2^53, but use
  `bits.Mul64`/`Div64` so that the outward-walk invariant (M1) is exact.
- h mirror boundary: when s ≥ E, `2hb − s·n` can be **negative** (s > 2E). Unsigned
  `Div64` needs explicit sign handling: a negative result means the lower range is
  empty (old `s = -1`). Test s = b with E small.
- Neumaier summation: fine. With M3/M4 the terms vary monotonically, so plain
  summation from small to large would also do. Neumaier is cheap; keep it.
- Certainty: either gate is fine (see Q3). The `h ≥ n` (B5) check must stay before
  the exact branch, as in `protoCert`.
- LogSupportRatio and isrout `%l`: the claim "not affected" was not independently
  re-verified in this review.

### Other callers, thresholds and related issues (item 6)

- **Call sites**: `internal/istree/report.go` `Tree.value` (-i and the rule value
  used for set aggregates and the rule filter `dir*val < thresh`), the root
  `Evaluation` and `measure.go`, the CLI options (`-i` → `InvalidateBelowExpected`),
  and the internal Fisher users of `infoRaw` (`fetInfo`) and `logProb`. The tests
  `fisherFragile` and `upstreamFisher` replicate `infoRaw`/`logProb` and must keep
  their own copies. Nothing else calls the cancelling formulas
  (grep for `infoRaw|chi2cdfQ|gammaQ|logGamma|float64(supp)*float64(base)`).
- **Threshold decisions**: the -d filter compares `dir*v` exactly. Last-bit changes
  in i, g and Fisher (n > 170) on ordinary data can flip rules whose value is within
  a few ulps of the threshold, both for rules and for set aggregates. The CHANGELOG
  should state this. The oracle's Ref mode already treats such cases as Optional, so
  it stays consistent if its tolerances are tightened (M6).
- **#17 / #18**: #17 is about support and confidence thresholds (`thresh.go`,
  `confEps`), not -d. The only shared contract is the "Numeric contract" section.
  Exact -i (128-bit) fits #17's direction ("library exact"). #18 (absolute CLI
  thresholds) does not interact with the measures. Do not reuse the ALG-6 Defect
  machinery for #17 (Q4).

### Recommended answers to the open questions

1. **Info slowdown**: do not accept it. A tuned stable implementation runs at the
   same speed as main (M2). Make "BenchmarkEval/Info within 5 %" an acceptance gate.
   Use no hybrid.
2. **Fisher**: use the recurrence with the mode walk, plus M1 (outward from the mode,
   for every range), M3 (direct tails, no complements), M4 (rigorous relative
   truncation) and H1 (bounded tie-break). Keep the per-term anchor version as a
   test oracle for the walk, not as the shipped fallback.
3. **Certainty gate**: 2^10. The bound 2^-40 is a cleaner contract and four times
   below the oracle's 2^-36 tolerance (relevant to the 0.25·tol fragility guard).
   2 rows change, documented. 2^12 is acceptable if fixture parity matters more,
   with bound 2^-38.
4. **Oracle known-defect machinery**: delete `DefectALG6`, `alg6InfoErr`,
   `KnownDefect`/`DefectTol` and `DiffOptions.Defects`. #17 does not need
   value-defect bands; its known failures live in `knownAPIFailures`. Tighten the
   tolerances instead (M6).
5. **Hard Fisher limit**: no error-returning limit. Do bound the work per table (H1),
   and with M4 the cost is about 9σ per tail, which states it plainly: σ ≈ √n/4 at
   balanced margins, about 1.5e9 tables at n = 1e18. Document this in `Measure`,
   `AbortFunc` and the CLI help. Without H1, a limit would be needed; with it, it is
   not.
