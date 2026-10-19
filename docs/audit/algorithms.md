# Algorithmic correctness audit (Phase 1.1): fpgrowth-go @ f7bb606

Auditor: independent algorithms review (did not write the code).
Clone: `scratchpad/audit/alg` (baseline `f7bb606`, clean except the new `_audit/` directory).
Checker sources: `_audit/alg/` inside the clone (see "Tools" below).
Nothing in the implementation was modified. Nothing was committed.

## Summary

No correctness defect was found in mining. Across ~118,000 randomized brute-force cases (CLI and
library), wide-database closed/maximal checks and an exhaustive sweep of the rule measures, every
result for total transaction weight below about 1e13 matched the textbook definitions. The only
exceptions are documented upstream semantics and floating-point ties exactly on a threshold. The
reported supports were exact in every case, including supports and weights near the int64 limit.

The findings are numerical. The most important one (ALG-1) is that the contingency-table measures
(chi², Yates, information/G, Fisher, certainty, `-i`) lose all precision once the total weight
exceeds about 1e12–1e14. They then silently report clearly significant associations as
independent (for example a perfect association gives chi² = 0, p = 1). Upstream never reaches
this range because its supports are 32-bit; the port's 64-bit weights (M7/I7) open it up.

| ID | Severity | Status | Title | Blocks v0.1.0 |
| --- | --- | --- | --- | --- |
| ALG-1 | Medium | confirmed | Catastrophic precision loss in chi²/Yates/info/G/Fisher/certainty and `-i` for large total weights | no (document or fix before release recommended) |
| ALG-2 | Low | confirmed | Relative (1-ε) tolerances admit sets/rules beyond the requested bound when weights exceed 2^52 (`-s/-S` %, `-c`, `MinConfidence`, `-c100` accepts conf < 1) | no |
| ALG-3 | Low | confirmed | CLI absolute thresholds `-s-N`/`-S-N` are parsed as float64: values above 2^53 are rounded (wrong items in/out) | no |
| ALG-4 | Info | confirmed | Values exactly on an evaluation threshold can be excluded by rounding for all measures, not only the log-based ones (F-1 understates this) | no |
| ALG-5 | Info | confirmed | Upstream measure quirks kept: `-eo` absolute vs `-d`/100, `-ew` = 2·\|K−½\|, `-er` = +Inf for confidence 1 | no |
| ALG-6 | Info | confirmed | G-test p-value at exact independence is 0.9999997 rather than 1 (rel. 3e-7) | no |
| ALG-7 | Info | confirmed (documented L-2) | Library: `IncludeEmpty` reports nothing when no item is frequent, although supp(∅) = W ≥ smin | no |

## Tools (independent checkers)

All under `_audit/alg/` in the clone (a leading underscore keeps them out of `./...`):

- `bf/main.go`, `bf/measures.go`: definition-based brute-force checker for the CLI. It imports
  nothing from the repository. It generates adversarial databases (random, sparse, dense, chains,
  identical transactions, planted large sets, transactions drawn from a few patterns, empty
  transactions, duplicate items in a transaction, single item, up to 18 items so that the
  16-items machine boundary is crossed) and weights (none; 0–4; 0/1; huge, near
  `MaxInt64/#huge` mixed with tiny ones). It also generates option sets: `-t s/c/m/g/r`,
  `-m`/`-n` (including 0), `-s`/`-S` as percent or absolute (including values derived exactly from
  actual supports so thresholds land on boundaries), `-c` (including exact support ratios), `-o`,
  `-A s/c/d/t`, `-x`, `-u`, `-j`, `-q 0/±1/±2`, `-l 0..16`, all 25 rule measures plus `-eb`
  with `-am/-an/-aa`, `-i`, `-d`, and `-R` (item-selection files for sets; appearance files with
  all keyword synonyms for rules). The expected results come from exhaustive enumeration
  (superset-sum supports over all 2^k item masks) and these definitions:
  - closed: no superset with equal support;
  - maximal: no frequent superset (over all items);
  - generator: no subset with equal support;
  - rules: rule support (body, or body ∪ head with `-o`) ≥ smin and ≤ smax, and
    s·100 ≥ c·b exactly.

  Thresholds use exact rationals (big.Rat). Mismatches are then re-classified against the
  upstream float formulas (`(1±ε)` tolerances, float confidence, float `-i`) so that
  float-boundary effects are reported separately from real errors. Measures are computed exactly
  with big.Rat / big.Int (Fisher tests by exact hypergeometric sums, with ties decided exactly).
  The checker optionally runs the C reference too.
  Run: `bash _audit/alg/runall.sh <N> [-nohuge]` (4 parallel modes: all, sets, rules, eval).
- `numcheck/main.go`: exhaustive comparison of `internal/ruleval` against `measures.go` over every
  2×2 table with n ≤ 60 (Fisher: n ≤ 40), plus scaled tables up to 2^60.
- `scale/main.go`: shows how the accuracy of each measure changes as n grows from 9e1 to 9e18.
- `apicheck/main.go`: brute force for the public API (`MineItemsets`, `MineRules`). It covers
  exact `Count` thresholds over the int64 range, `MinSize`/`MaxSize`/`IncludeEmpty`,
  `MaxSupport`, `Items`, `Border`, `Appearance`/`DefaultAppearance`, `OriginalSupport`,
  `MinConfidence`, all algorithms and tunings, and zero and huge weights.
- `wide/main.go`: closed and maximal sets on wide databases (20–100 items, up to 12 transactions).
  The expected sets are computed independently as the intersections of the covers. It also
  checks frequent-set counts in count-only mode via Möbius inversion over the closed-set
  lattice, including counts above 2^63 and saturation beyond 2^64.
- `edge.sh` (CLI edge inputs, Go against C), `ftime.sh` (Fisher timing).

## Findings

### ALG-1: Precision loss in the contingency-table measures for large total weights (Medium)

- **Status:** confirmed.
- **Affected:** `internal/ruleval/measures.go`:
  - `reChi2`, `reChi2PVal`, `reYates`, `reYatesPVal`: `head*body - supp*base` is computed in
    float64, so the difference of two ~n² products cancels.
  - `infoRaw`, `reInfo`, `reInfoPVal`: `log(base) + sum/base` cancels.
  - `reCert`: `p = head/base` rounds to 1.
- **Also affected:** `internal/ruleval/fisher.go` (`logProb`: log-gamma differences at huge n),
  and `internal/istree/report.go` `Tree.value` (`-i`: `float64(supp)*float64(base) >
  float64(head)*float64(body)` is inexact once the products exceed 2^53). The same formulas are
  reached from the root package `Evaluation`.
- **Evidence (`go run ./_audit/alg/scale`).** The table is s = 3k−2, b = 3k, h = 9k−2, n = 9k.
  The exact values are chi² p = 0.0455003 and G p = 0.0360561.

  | n | `-ep` Go | `-eg` Go | `-ei` Go (exact) |
  | --- | --- | --- | --- |
  | 9e9 | 0.0455003007 | 0.0360563665 | 3.52212900e-10 (3.52213889e-10) |
  | 9e12 | 0.0454977691 | 0.0356771391 | 3.5366e-13 (3.5221e-13) |
  | 9e13 | 0.0457767416 | **0.0501352168** (crosses 5 %) | 3.075e-14 |
  | 9e14 | 0.0461759835 | **1** | **0** |
  | 9e15 | **0.0243349** | **1** | **0** |
  | ≥9e16 | **1** | **1** | **0** |

- **CLI reproductions** (`_audit/alg/tmp/*`):
  - Database `x y 2999999999999998` / `x 2` / `y 6000000000000000`, command
    `fpgrowth -tr -w -s0 -c0 -m2 -e{p,g,f} -d100 -v' %a %b %h %20e' chi_big.txt -`. Results:
    `-ep` = 0.02433 (exact 0.0455), `-eg` = 1 (exact 0.0361), `-ef` = 1 (exact 1/9 = 0.1111, the
    same table structure as `chi_small.txt`, where Go correctly gives 0.1111). `-ei` = 0.
  - Perfect association: database `x y 9223372036854775800` / `q 3`, command
    `fpgrowth -tr -w -s0 -c0 -m2 -en -d0 ... y.txt -` gives `y <- x` with chi²/n = 0 and Yates = 0;
    exact value 1. With `-ep -d0` the rule is dropped because p = 1.
  - Certainty factor: database `x y 2` / `y 9223372036854775800`, `-ez -d0` gives 0; exact value 1
    (confidence 1, prior 1−2.2e-19).
- **Randomized evidence:** every one of the 27 `FAIL-mismatch` cases of the final campaign
  including huge weights (`_audit/alg/runs/final-std/`) is in this family (`-ep/-et/-eg/-ey/-ez/
  -en/-ee`), and all have W ≥ 1e13. With small weights there are 0 mismatches (32,000 cases).
  numcheck: `p` 8 and `g` 389 mismatches, all on large-n tables or at exact independence (see ALG-6).
- **Impact:** rules and item sets evaluated with these measures on databases whose total weight
  is above ~1e12 (weights used as amounts, durations or byte counts) get wrong values without any
  warning. Strong associations get p = 1, chi² = 0 or information 0 and are filtered out (or kept
  wrongly for (−) thresholds). Upstream cannot reach this regime (32-bit `SUPP`), so this is new
  exposure created by the 64-bit extension. It is not covered by `docs/differences.md`.
- **Proposed resolution:**
  - Compute `head*body − supp*base` exactly (`math/bits.Mul64`/128-bit or `math/big`) and only
    then convert, which makes chi²/Yates exact to rounding.
  - Compute the information from per-cell `log1p((c·n − r·k)/(r·k))` terms (exact integer
    numerators) instead of `log n + Σ/n`.
  - Compute `p` for the certainty factor as `(base−head)/base` in integers.
  - Compare `-i` exactly in 128-bit.
  - For Fisher at n beyond ~1e9, either document the limit or switch to a log-space
    hypergeometric based on `lgamma` differences of small arguments.
  - At minimum, document in `docs/differences.md` and the `Evaluation`/`Measure` godoc a total
    weight above which these measures are unreliable (≈1e12), and add a regression test with
    the tables above.
- **Blocks v0.1.0:** no. Recommended to document before release and fix soon after; the default
  path (no evaluation) and the non-statistical measures are unaffected. The rational measures
  (`c d l a q v e r b k w u j`) were exact to rounding in all large-n checks.
- **Note:** the linear cost of the Fisher tests is documented (README "Every measure costs
  constant time per evaluated rule, except…"); measured 7 s for n = 1e8 and 71 s for n = 1e9 with
  balanced margins (`ftime.sh`, `fl1.txt`/`fl2.txt`). With int64 weights this cost has no
  practical bound but can be cancelled.

### ALG-2: (1−ε) tolerances exceed the requested bound beyond 2^52 (Low)

- **Status:** confirmed.
- **Affected:**
  - `internal/fpg/thresh.go` (`minSuppValue`/`maxSuppValue`: `smin/100·W·(1−2^-52)`);
  - `internal/istree/istree.go` (`conf: p.MinConf * confEps`) and `report.go` `rule`
    (`float64(supp) < float64(body)*t.conf`);
  - root `RuleOptions.MinConfidence` and `Fraction`.
- **Evidence:**
  - `printf 'a 2305843009213693752\nb 2305843009213694152\n'`, then `fpgrowth -w -s50` reports
    `a` with relative support 49.999999999999992895 %.
  - `printf 'x y 4611686018427387000\nx 1000\n'`, then `fpgrowth -tr -w -s0 -c100 -m2` reports
    `y <- x` with confidence 99.999999999999971578 %.
  - apicheck: 203 of 1,994 rule cases with huge weights accepted rules with
    `MinConfidence: 1` and confidence < 1.
  - bf: these show as the `fp-boundary{floatThr}` and `fp-boundary{floatConf}` classes, which go
    away under the upstream float formulas. With small weights `floatConf` only occurs for
    decimal confidences that are not exactly representable.
- **Impact:** with total weights above 2^52 the minimum support can be undercut by up to
  W·2^-52 (512 for W = 2^61), the maximum support exceeded by the same amount, and `-c100`/
  `MinConfidence = 1` no longer means "exact rules only". This is upstream semantics, and
  `Fraction` documents it ("for a total weight beyond 2^53 it is more than one"). The CLI side and
  the confidence side are documented only implicitly (`MinConfidence` doc gives the formula).
- **Proposed resolution:** document it for `-s/-S/-c` in `docs/differences.md`/README and for
  `MinConfidence` explicitly. Optionally, for the library, compare confidence 1 (and rational
  confidences) exactly with 128-bit integers.
- **Blocks v0.1.0:** no.

### ALG-3: CLI absolute thresholds are float64 (Low)

- **Status:** confirmed.
- **Affected:** `cmd/fpgrowth` option parsing of `-s`/`-S` (strtod), `internal/fpg/thresh.go`
  (`-smin` as float64).
- **Evidence:**
  - `fpgrowth -w -s-2305843009213693753 s50.txt -` reports `a` with support
    2305843009213693752 (below the requested minimum).
  - `-s-4611686018427387840` rounds up to 2^62 and reports "no (frequent) items found" although an
    item has support 4611686018427387840 (bf `FAIL-noitems-but-frequent`: 127 cases in
    `runs/final-std`, all with |s| > 2^53).
- **Impact:** absolute thresholds above 2^53 are off by up to 1024 in either direction. This is
  upstream-identical (double), and the library's `Count` is exact, but the CLI help says
  "negative: absolute number".
- **Proposed resolution:** parse the value with `ParseInt` when it is an integer literal (keep
  strtod for the rest), or document it.
- **Blocks v0.1.0:** no.

### ALG-4: Exact-threshold values excluded by rounding (Info)

- **Status:** confirmed, upstream-identical.
- **Evidence:** `printf 'x y 5\nx 5\ny 4\nz 6\n'`, then `fpgrowth -tr -w -s0 -c0 -m2 -ed -d5`
  omits `y <- x`, whose |conf − prior| is exactly 0.05. The computed value is
  0.049999999999999988898. C gives the same result; with `-d4.9` the rule appears.
- bf `eval-boundary` class: 32 cases with small weights in 56,000 runs (`small` + `final-small`), all with the true value
  equal to the threshold.
- **Resolution:** broaden F-1 in `docs/differences.md`, which currently names only the log-based
  measures, to say that any measure value exactly on the threshold may fall on either side.
- **Blocks v0.1.0:** no.

### ALG-5: Measure definitions that differ from their names (Info)

- **Status:** confirmed. Documented in `docs/upstream/rules-eval.md` (N3, `o` row) and in Go
  comments, but the CLI `-!` text is unchanged.
- **The quirks:**
  - `-eo` returns the absolute support, compared with `-d`/100, so `-eo -d10` means s ≥ 0.1 and
    always passes.
  - `-ew` is 2·|K − ½|, not |K − ½|.
  - `-er` is +Inf for confidence-1 rules, although the measure lies in [0, 1] and its limit is 1.
- **Counts:** bf "quirk" classes (each checked as consistent with the upstream value):
  `quirk-measure-o/w/r` in every campaign, for example 104/83/78 in `final-small/eval.txt`.
- **Resolution:** mention these in the README measure table and godoc (RuleSupport already says
  "absolute"). `-er` = +Inf may deserve a note, since the measure is otherwise bounded.
- **Blocks v0.1.0:** no.

### ALG-6: G-test p-value at exact independence (Info)

- **Status:** confirmed, upstream-identical.
- **Evidence:** `numcheck -n 60 -nf 0 -large=false` gives `g` 359/595,665 mismatches above 1e-9,
  the worst being s=12 b=13 h=48 n=52 (exact independence): p = 0.999999703 instead of 1. The
  information is ~1e-16 instead of 0, and Q(½, x) ≈ 1 − 2√(x/π) amplifies that. With a relative
  tolerance of 1e-6 there are 0 mismatches.
- **Impact:** negligible.
- **Resolution:** optionally set the information to 0 when the table is exactly independent
  (`supp*base == head*body`, checked in integers).
- **Blocks v0.1.0:** no.
- **Note (2026-10-19, issue #15):** the defect is broader than exact independence. The
  independent reference of `internal/oracle` shows the same mechanism (an absolute error of a
  few ulps of ln n in the information, amplified by the chi² tail near G = 0) for *nearly*
  independent tables at moderate totals: the G-test p-value of s=16383, b=h=32767, n=65535 is
  0.996883176 instead of 0.996883200 (relative 2.4e-8), and relative errors of about 5e-8 occur
  at n ≈ 4e5. Compared relatively, the information itself is off by relative 1e-12 and more
  already at n = 9,000. The test suite accepts these deviations only within a derived bound
  (`oracle.DefectALG6`) and pins the defect in strict expected-failure tables
  (`internal/ruleval` `TestKnownMeasureFailures`, root `TestALG6Exemption`). Tracked by #16.

### ALG-7: Library `IncludeEmpty` with no frequent item (Info)

- **Status:** confirmed, documented as L-2.
- **Evidence:** apicheck counted 34 cases (for example transactions `{[] 1} {[] W−4} {[0] 3}`,
  `MinSupport = Count(W)`, `IncludeEmpty`) where the empty set (supp W ≥ smin) is not reported.
- **Note:** this is consistent with the CLI's "no (frequent) items found" error. It is mentioned
  only so that users of `IncludeEmpty` know about it.
- **Blocks v0.1.0:** no.

## What was checked and held

All counts come from the run logs under `_audit/alg/runs/`.

1. **Mining correctness against the brute force, small and zero weights, Go CLI.**
   `TAG=final-small bash runall.sh 8000 -nohuge` and `TAG=small bash runall.sh 6000 -nohuge`
   give 56,000 cases with **0 FAIL**. In `final-small`, the 32,000 cases split into:
   - 23,909 exact passes;
   - 7,409 cases that failed with "no (frequent) items found", each confirmed correct
     (no selected item frequent);
   - 338 `rc-empty-selection-file` (an empty `-R` file is rejected, as upstream);
   - the rest float-confidence boundary, exact-threshold ties (ALG-4) and quirk classes (ALG-5).

   This covers:
   - targets s, c, m, g, r with every `-A` variant, `-x`, `-u`, `-j`, `-q`, `-l`;
   - `-m0` and the empty set, `-n`, `-s`/`-S` percent and absolute (including boundary values
     derived from actual supports), `-c`, `-o`;
   - all 25 rule measures for rules and for sets with `-am/-an/-aa`, `-eb`, `-i`, `-d`;
   - `-R` selection and appearance files (all keyword synonyms) with the R3 head-only semantics;
   - zero-weight transactions, empty transactions, duplicate items and up to 18 items (beyond the
     16-items machine).

   Supports printed for every reported set and rule (`%a %b %h`) matched exactly. No duplicate
   records and no unparsable output occurred.
2. **Same with huge weights (W ≈ 2^63 − small).** `TAG=final-std bash runall.sh 5000` (20,000
   cases, ~10 % huge weights) and `bfcheck -huge -n 3000` gave **0 support mismatches**
   (`grep '^supp '` in every saved failure: 0). The only failures were ALG-1 (measures), ALG-2
   (tolerances) and ALG-3 (CLI parsing). All 188 saved failures in `final-std` have W ≥ 1e13.
3. **Go against the C reference** (release build, `-ffp-contract=off`), run in the same
   campaigns. In `final-small`:
   - 22,532 identical result sets;
   - 1,065 cases where C deviates and Go matches the definition. Sampled: `-s0` support-0 sets
     (M2), `-tg -m≥2` (M3), conviction 0 for confidence 1 (R4), head-only items in bodies (R3),
     `-tr -eb` (R1);
   - 620 C crashes or errors (`-s0`, `-At` item map, M1/M2);
   - 36 cases where both differ, all float-confidence boundaries combined with a documented C
     deviation;
   - **0 cases where only C matches the definition with W < 1e15.** The 20 C-only-correct cases
     seen earlier all had W ≈ 9.2e18, where C truncates weights and the result is coincidence.
4. **Rule measures against definitions** (`numcheck -n 60 -nf 40`). 595,733 tables per measure
   (Fisher f/h/m/s: 123,410 each, exact big.Rat, ties included) gave 0 mismatches for
   `o c d l a q v e r b k w u j z n y i f h m s`. `p` and `g` differed only at large n (ALG-1)
   or at exact independence (ALG-6). `t` differed by 4 at relative 1e-8 at n ≈ 4.6e18.
5. **Public API brute force** (`apicheckbin -n 6000 -seed 1`): 4,006 item-set and 1,994 rule cases
   with **0 failures**, after aligning the checker with the documented `MinConfidence` formula
   and L-2. This covers exact `Count` thresholds near `MaxInt64`, `Items`, `Border`,
   `Appearance`, `OriginalSupport`, `MaxSupport`, every algorithm and tuning, and zero weights.
   Total-weight overflow is an error ("total transaction weight overflows int64"), and a
   negative weight is an error.
6. **Wide databases** (`widebin -n 5000 -seed 2`, plus 300 with seed 1): 20–100 items, heavy
   perfect extensions. Closed: 1,686 passes. Maximal: 1,617 passes. Frequent-set counts:
   1,156 exact, plus 120 correctly saturated at 2^64 − 1. Counts between 2^63 and 2^64 were
   exact. 0 failures.
7. **CLI edge inputs, Go against C** (`bash _audit/alg/edge.sh`). Same results or documented
   deviations for:
   - empty and blank-only files, weight-only lines, zero-weight-only data (Go: no items, C:
     support-0 sets, M2);
   - weight overflow (Go exit message C4; C prints `a (0)`), weight 2^63 and negative weights
     rejected, `1e3` as a weight rejected;
   - `-s200`/`-sinf`/`-c101`/`-c-5` rejected, `-S0` gives empty output;
   - `-m/-n` int truncation (documented), `-l17`/`-l-1`, `-q±3`, `-F` with empty or invalid
     entries, `-p1 -eb`, invalid `-A`/`-t`.

   No panics.
8. **Fuzzing (existing targets, crash and oracle checks):** `FuzzMine` 90 s (40,793 execs),
   `FuzzMineItemsets` 60 s (59,643), `FuzzMineRules` 60 s (164,638), `FuzzParseArgs` 45 s
   (3.9 M). All PASS (`runs/fuzz.txt`).

**Not covered:**
- `-p` evaluation pruning: it is a heuristic with no definition-based ground truth, and only
  Go against C was looked at, incidentally.
- Output formatting and `-P` pattern spectra, which belong to another audit area.
- More than 18 items with exhaustive enumeration. Wide closed/maximal sets were covered by
  `wide/`.
- The race detector was not rerun.

**Total machine time:** about 30 minutes of campaigns, plus about 5 minutes of fuzzing.
