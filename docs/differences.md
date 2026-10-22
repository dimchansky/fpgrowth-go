# Differences from upstream FP-Growth 6.21

Every intentional behavioral difference between this port and Christian
Borgelt's C implementation (version 6.21) is listed here, with the evidence
for the upstream behavior and the test that pins the Go behavior. Code
sites are marked with `// Deviation from upstream:`. The identifiers (M1,
R2, ...) are stable; other documents and tests cite them.

Bug identifiers in parentheses refer to the audit notes: `core` =
`docs/upstream/fpgrowth-core.md`, `report` = `docs/upstream/report.md`,
`rules` = `docs/upstream/rules-eval.md`, `tract` = `docs/upstream/tract.md`,
`cli` = `docs/upstream/cli-io.md`, "reference-build" =
`docs/upstream/reference-build.md`. Upstream locations are files of
`reference/upstream/` (`fpgrowth/src`, `tract/src`, `apriori/src`,
`math/src`, `util/src`); most were verified by the independent audit
([audit/differences.md](audit/differences.md)).

## Kinds

Every row has a **Kind**, so that correctness fixes can be told apart from
intentional behavior changes:

| Kind | Meaning |
| --- | --- |
| defect fix | Upstream has a confirmed correctness or safety defect (crash, wrong result, ignored option, undefined behavior); the port behaves as documented or as the definition requires. |
| defect fix (policy) | As above, but the fix needs a choice that upstream does not make (which documentation to follow, a value convention such as +Inf, rejecting input). |
| latent | A defect that is unreachable from the upstream program or has no visible effect (found by code reading or a harness); fixed anyway. |
| platform | A necessary difference between C and Go or the operating system (libm, address space, file descriptors, time source). |
| improvement | Upstream behavior is not wrong, but the port's is better for the same output (memory, early stop, rejecting meaningless options). |
| new | Behavior without an upstream counterpart (library API, signal handling). |
| preserved | Upstream behavior kept on purpose, although it may look like a defect; listed so that it is not "fixed" by accident. |
| limit | A numeric limit of the port that upstream does not have or shares; documented, not changed. |

Status is **implemented** (code exists; the regression test is named) or
**documented** (limits and preserved behavior without a dedicated test).

## Summary

The independent audit before v0.1.0 classified the 55 deviation rows of
this file as it stood then (36 upstream defects, 4 improvements, 6
platform differences, 6 new functions, 3 incompletely verified). After
merging different manifestations of one root cause and splitting rows
that combine unrelated defects, the port fixes **33 distinct, confirmed,
user-visible upstream defects**:

- merged: M9 into M4 (same reporter check, `report.c:1473`); I6, C4 and
  I7 into M7 (32-bit supports and weights); C1 into M8 (float-to-int
  threshold conversion); C3 into O7 (unchecked write errors);
- split: R9 into R9a and R9b (R9c is latent), I5 into I5a and I5b, O3
  into O3a and O3b.

In addition, **2 upstream crashes** that the port avoids were found
undocumented by the audit and are now listed (M10, bucket-sort recursion,
tract T4; O11, pre-format table overflow, reference-build F5), and **5
latent defects** are fixed: R9c, R10 (a defect of the project's patched C
used to check R2, not of upstream), R11a, R11b and C2.

Since v0.1.0: N1, the precision loss of the statistical measures (a
documented limit of v0.1.0), is fixed and is now a defect fix row
(issue #16, 2026-10-19).
N2, upstream's relative threshold tolerance beyond 2^52 (a documented
limit of v0.1.0), is replaced by a threshold contract that keeps
upstream's arithmetic below 2^52 and is exact above; it is now a defect
fix (policy) row (issue #17, 2026-10-19).

## Mining results

| # | Kind | Upstream behavior (evidence, location) | Go behavior | Status / test |
| --- | --- | --- | --- | --- |
| M1 | defect fix | `-At` copies the item map from the wrong array: crash or wrong item names when an item is dropped at the top level (core B1; `fpgrowth.c:1660`) | Correct map; same sets as the other variants | implemented in `internal/fpg` (`TestDeviations` "B1 …", golden `internal/fpg/testdata/golden/deviations.txt` rows `b1a.tab`, `b1b.tab`; CLI golden `dev-At`) |
| M2 | defect fix | `-s0` crashes on zero-frequency items or reports support-0 sets (core B2, tract; `fpgrowth.c:1915-1920` without a clamp, `tract.c:881`); likewise `-tr -c0` keeps zero-frequency items (difftest seed 1, cases 429, 7229) | Effective absolute minimum support, and the rule item threshold `ceil(smin·conf)` (e.g. `-tr -c0`), is at least 1. With `-tr -c0` this can also change rule order relative to upstream, since upstream keeps zero-frequency items in the item codes | implemented in `internal/fpg`, `internal/tract` (`TestDeviations` "B2 smin 0 …", `TestItemBase_Recode_minSupportAtLeastOne`; CLI golden `t1-s0`) |
| M3 | defect fix | Generators with `-m ≥ 2` are missed because the `zmin` transaction filter deflates subset supports (core B3; `fpgrowth.c:1946-1949`, `tbg_filter` for all targets) | No `zmin` filter for the generator target. Side effect: the transaction count logged on stderr for `-tg -m≥2` differs from upstream (review F6) | implemented in `internal/fpg` (`TestDeviations` "B3 …", golden `deviations.txt` rows `b3.tab`; CLI golden `dev-tg-m2`) |
| M4 | defect fix | Maximal target reports nothing when every frequent item is a top-level perfect extension (core B4; `report.c:1473`, `if (r >= rep->sto) return 0` with `sto = 0`) | The maximal set is reported | implemented in `internal/report` (simulator scripts `mp_maximal`, `ab2_maximal`; `TestDeviations` "B4 …", golden `deviations.txt` rows `b4.tab`; CLI golden `dev-tm-pex`) |
| M5 | defect fix | Closed/maximal targets with `-n` can report non-closed/non-maximal sets (core B5): the search goes to `zmax+1` items (`isr_setupx`, `report.c:1117`) and the repository lacks supersets beyond it | The search stops where no set of at most `zmax` items can follow, and every set the closed/maximal repository accepts is checked against all items of the database (complete one-item extension check), so results equal the brute-force definition. Without a limit below the longest transaction the search is unlimited (repository alone exact). Boundary perfect extensions alone are not enough (`docs/m5-investigation.md`) | implemented in `internal/fpg` (`extcheck.go`; `TestExtCheckOracle`, `TestExtCheckRegressions` incl. minimized oracle seed 678 and local-check counterexamples, `TestExtCheckUnlimited`, `TestExtCheckBrute`, `TestExtCheckBits`, `TestDeviations` "B5 …", golden `deviations.txt` rows `b5*.tab`) and `internal/report` (`SetCloMaxCheck`, `TestReporter_BoundaryPexNotEnough`) |
| M6 | defect fix (policy) | Negative weights corrupt supports and crash the 16-items machine (core B7, tract; `tract.c:711-718`, `fim16.c:252`) | Negative weights are rejected with an error (zero weights are accepted) | implemented in `internal/tract`, `internal/tabread` (CLI golden `dev-negw`) |
| M7 | defect fix | Supports and weight sums are 32-bit `int`; overflow is undefined behavior (tract T5, cli; `tract.c:806-811`, `tract.h:107-108`) | 64-bit supports; total-weight overflow is an error | implemented in `internal/tract` (CLI golden `dev-bigw`; CLI exit 21, C4) |
| M9 | defect fix (same root cause as M4) | Maximal target with `-x` and the 16-items machine (`-As`/`-Ad`) loses maximal sets, e.g. a single transaction `a` gives no output (reference-build bug 3; the machine registers its items as perfect extensions of ∅, then `report.c:1473`) | Maximal sets reported in all variants | implemented in `internal/fpg` (`TestDeviations` "M9 …", golden `deviations.txt` rows `m9.tab`; CLI golden `dev-tm-x`) |
| M8 | defect fix | Out-of-range floating-point thresholds are converted to `int` with undefined behavior (core B9; `fpgrowth.c:1915-1920, 1996-1998`). On arm64 C happens to saturate, so the outputs agree there | Explicit clamping | implemented in `internal/fpg` (`TestThresholds`: 20,000 random conversions incl. saturation) |
| M10 | defect fix | The bucket sort of the transactions recurses once per item of a prefix shared by more than 16 transactions and overflows the stack (tract T4; `tract.c:2225-2229, 2257, 2261, 2286`): 17 identical transactions of 100,000 items plus 100,010 one-item transactions, `-s-17 -n1`, exit 139 | No recursion over the transaction length: the bag is a prefix tree built incrementally, its child lists are sorted node by node in a flat loop, and it is traversed with an explicit stack (`internal/tract/bag.go`); the audit's reproducer completes (100,001 sets with the input above, as in the C program run with a raised stack limit). Not a deviation. Mining such a single long chain with `-n1` for the frequent (default) target was quadratic in the chain length, in upstream C and in Go alike, because the perfect extensions are enumerated once per item (upstream `rec_cmplx`, `isr_report`, `isr_remove`): at 100,000 items Go took 34.9 s and C 31.2 s CPU time (C with a 64 MB stack; with the default 8 MB stack it crashes), with identical output (follow-up investigation of the audit). Since issue #21 Go skips the perfect extensions that cannot change a result (the miners' chain walks ask `Reporter.PexUseful`; `Report` and the hypercube expansion ignore the perfect extensions of a set of zmax items), so this input is linear in Go and still quadratic in C: `t4-12500` (`-s-17 -n1`) 0.50 G instructions against 7.93 G before and 5.01 G in C (`t4-25000`: 1.0 G, 30.7 G, 19.8 G), with the same output. An improvement over upstream, not a deviation in results | implemented in `internal/tract` (`TestItemBase_BuildBag_longSharedPrefix`: the audit's reproducer, checks the bag's transactions and weights; since Go stacks grow, it would not catch a reintroduced recursion, which the iterative code rules out by construction); the #21 speed-up in `internal/report` and `internal/fpg` (`TestMine_T4NoPerfectExtensions`: no perfect extension is collected on the T4 shape in any variant; `TestMine_PexGuardSame`, `TestReporter_PexGuardSame`: results identical with the guard off; `TestReporter_PexUseful`, `TestReporter_ExpandSkip`, `TestReporter_ReportSkip`) |
| M11 | platform | The size·weight sums `xfq` used for the item order of `-q ±2` are `int` and overflow (undefined behavior, `tract.c:807, 810`; reference-build F6) | With 64-bit weights, size·weight and its sum saturate at `math.MaxInt64`; this can only affect the order of items whose sums both saturate | implemented in `internal/tract` (`itembase.go`; `TestItemBase_Count_saturation`) |

## Rules and evaluation

| # | Kind | Upstream behavior (evidence, location) | Go behavior | Status / test |
| --- | --- | --- | --- | --- |
| R1 | defect fix (policy) | `-eb` always selects the log support quotient; the rule measure "imbalance ratio" is unreachable, and `-tr -eb` evaluates to 0 (rules B1; `fpgrowth.c:2436`, `ruleval.c:130-135, 456`) | For `-tr`, `-eb` selects the imbalance ratio as documented by `-!`; for item sets it stays the log support quotient | implemented in `internal/istree` (`TestReport_ImbalanceB1`), `internal/fpg` (`TestTreeImbalanceR1`); CLI golden `dev-eb-rules` |
| R2 | defect fix (policy) | `-p` has no effect: `ist_filter` runs before `ist_seteval` (rules B2; `fpgrowth.c:2051-2057` before `2069-2070`). Upstream's two documents disagree on the meaning of the sign of `-p` | Evaluation set before filtering, so `-p` prunes item sets with the semantics of the program's help text: negative weak forward, positive strong forward, 0 backward pruning; the HTML documentation's sign convention ("negative = backward") is not followed. Positive values on fpgrowth's path are new behavior (upstream used them only in apriori's `ist_commit`). Rules are unaffected, matching the patched C `reference/patches/istree/fpgrowth-p-order.patch` | implemented in `internal/istree` (`TestFilter_CallOrderB2`), `internal/fpg` (`TestTreePruneR2`); CLI golden `dev-p` |
| R3 | defect fix | Head-only items (`-R`) appear in rule bodies (rules B3; `istree.c:2036-2041`) | Appearance constraints honoured | implemented in `internal/istree` (`TestReport_HeadOnlyB3`); CLI golden `dev-R3` |
| R4 | defect fix (policy) | Conviction is 0 when confidence is 1 (rules B4; `ruleval.c:99`) | +Inf (passes every (+) threshold) | implemented in `internal/ruleval` (`TestUpstreamBugRegressions`); CLI goldens `tm-ev`, `dev-ev-rules` |
| R5 | defect fix (policy) | Certainty factor is NaN (0/0) when the head occurs in every transaction, and such rules are reported despite `-d` (rules B5; `ruleval.c:183-184`) | Value 0 (convention: the confidence cannot change); no measure returns NaN | implemented in `internal/ruleval` (`TestUpstreamBugRegressions`, `TestNoNaNForValidTables`) |
| R6 | defect fix | Fisher table-probability/information variants wrong for tied tables (rules B6; `ruleval.c:291, 300, 383-385`) | Correct computation | implemented in `internal/ruleval` (`TestFisherTiesRegression`, `TestFisherExact`: exact big.Rat oracle) |
| R7 | defect fix | Information measure can be slightly negative; G-test asserts in debug builds (rules B7; `ruleval.c:251-259`, `gamma.c:252`) | Clamped at 0 | implemented in `internal/ruleval` (`TestUpstreamBugRegressions`) |
| R8 | defect fix | `-S` applies to body ∪ head support even without `-o` (rules B8; `report.c:1619`) | `-S` applies to the same support as `-s` | implemented in `internal/istree` (`TestReport_MaxSuppB8`); CLI golden `dev-S-rules` |
| R10 | latent (patched C only) | Not an upstream defect: after the R2 fix in the patched C, the generator filter compares against the total weight with the skip flag set, so `-tg -p` reports nothing (`istree.c` `ist_clomax`, patch part 2) | Flag masked | implemented in `internal/istree` (`TestCloMax_GeneratorsTotalWeight`) |
| R11 | latent | (a) `ist_init` reads past its level array when `-m` exceeds the number of items (reference-build F8, ASan `istree.c:1700`; not reproducible at the CLI, output unaffected); (b) results of the recursive report calls are ignored (error path only) | Guarded; sink errors abort and are returned | implemented in `internal/istree` (`TestInit_LargeZmin`, `TestReport_SinkError`) |
| R9 | R9a defect fix; R9b defect fix (policy); R9c latent | (a) Two Fisher variants can return negative p-values (rules B9; `ruleval.c:336-341, 417-422`); (b) the Yates correction is not clamped, so an independent table gets a positive value (B10; `ruleval.c:217-218`); (c) `logGamma(170.5)` returns 0 (B11; `gamma.c:72-75`; unreachable from the CLI) | (a) clamped; (b) `max(0, \|ad−bc\| − n/2)` as in the textbook correction (policy); (c) correct value | implemented in `internal/ruleval` (`TestFisherComplementClamp`, `TestUpstreamBugRegressions`, `TestSpecialFunctionsMatchUpstream`); CLI golden `tg-ey` |

## Floating point

| # | Kind | Upstream behavior | Go behavior | Status / test |
| --- | --- | --- | --- | --- |
| F-1 | platform | Log-based values (`-eb`, measures `j p t i g f h m s`) use the platform libm; results are libm-dependent | Go's `math` package, which can differ by 1 ulp from Apple libm (e.g. `log 3`, `log 14`). A set or rule whose true measure value lies exactly on the threshold (e.g. exactly independent items with `-eb -d0`) may be included by one implementation and not the other (review F1). This holds for **every** measure, not only the log-based ones: a value that is mathematically equal to the threshold is computed with rounding and may fall on either side, in C and in Go alike (e.g. `printf 'x y 5\nx 5\ny 4\nz 6\n'`, `-tr -w -s0 -c0 -m2 -ed -d5` omits `y <- x`, whose value is exactly 0.05 but computes to 0.049999999999999988898; C gives the same; audit ALG-4). All other values are bit-identical to the C reference built with `-ffp-contract=off` (not necessarily to a stock upstream build, since arm64 clang contracts by default), with the exceptions of N1 (issue #16): the information, the G-test p-value and the Fisher tests for total weights above 170 use cancellation-free formulas and differ from C in the last bits on ordinary data (within the same tolerances: at most 2.2 of 8 and 0.2 of 1 tolerance units on `testdata/measures_c.txt`), and chi², Yates, their p-values, the certainty factor and `-i` differ only where C loses precision. Go's own arithmetic is FMA-safe (every product that feeds a sum is rounded explicitly, `TestPinnedBits`); `math.Log`/`math.Exp` may differ per architecture as before | documented; regression test pins Go's value on the review reproducer; difftest tolerates boundary cases |

## Numeric limits

Limits of float64 arithmetic that matter only for very large total
transaction weights or thresholds. Upstream cannot reach most of this
range (32-bit supports). N3 (audit ALG-3), the rounding of absolute
CLI thresholds to doubles, was upstream semantics kept in v0.1.0 and is
fixed for integer literals (issue #18). N1 (audit ALG-1 and ALG-6),
the precision loss of the statistical measures, was a limit of v0.1.0
and is fixed (issue #16); its row now records the fix and the numeric
contract below. N2 (audit ALG-2), upstream's relative threshold
tolerance beyond 2^52, was a limit of v0.1.0 and is fixed by the
threshold contract below (issue #17).

| # | Kind | Behavior | Evidence | Status |
| --- | --- | --- | --- | --- |
| N1 | defect fix | The statistical measures cancel in float64. Chi², Yates and their p-values (`n y p t`) compute `head·body − supp·base` from products rounded to double; the information and the G-test (`i g`) compute `(ln n + Σ t·ln(t/(r·k))/n)/ln 2`, whose absolute error of a few ulps of ln n is unbounded relative to small values (ALG-6: G-test p = 0.99999996 at exact independence, relative 2.4e-8 at n = 65535); the certainty factor (`z`) divides by `1 − head/base`, which rounds to 0; the Fisher tests (`f h m s`) compute every probability from log-gamma values of size n·ln n (relative error about 2^-52·n·ln n) and FisherChi2's mirror table from a rounded expected support; `-i` compares rounded products (`ruleval.c` `re_chi2` … `re_fetsupp`, `istree.c` `FPG_INVBXS` test). With upstream's 32-bit supports this already shows in its own outputs: of the 103 rows of `internal/ruleval/testdata/measures_c.txt` (totals up to 2^31−1) with a product above 2^53, 36 chi² and 38 Yates values are off by more than 8 ulps (one chi² is 0 instead of 6.6e-17). With 64-bit weights (M7) the measures collapse beyond about 10^12: strong associations get p = 1, chi² = 0 or information 0 (audit ALG-1, v0.1.0 "Known limitations") | D = supp·base − head·body is exact and rounded once (upstream's double expression while all supports are below 2^26, where it is exact; int64 below 2^31; 128-bit `math/bits` beyond); the information is Σ E·g(δ) over the cells with g(δ) = (1+δ)·ln(1+δ) − δ >= 0 and δ = ±D/(r·k) (no cancellation; exactly 0, and the G-test p exactly 1, at independence); the certainty factor is computed from the exact D when base/(base−head) > 2^10 (D/(body·(base−head)) for D > 0, D/(body·head) for D < 0, 0 for D = 0; upstream's formula could get the sign of conf − prior wrong there and returned up to 1 near independence: 1 instead of −1.2e-19, 0.1667 for an independent table at n = 2^62); the Fisher tests for base > 170 compute ln P from Stirling's formula with the exact D, sum the region directly (no complement) from the mode outwards with exact probability ratios, stop when a geometric bound of the rest is below 2^-60 of the sum (about 9σ per side instead of min(body, head) tables), and find the mirror tables exactly (FisherChi2 in integers, FisherProb/FisherInfo by bisection; near-ties are decided exactly for distances up to 1024 tables and with 320-bit logarithms beyond, so only probabilities equal within a relative 2^-200 count as ties); `-i` is exact. Error bounds: see "Numeric contract" below. Bit-identity with upstream: chi², Yates, their p-values and `-i` whenever supp·base and head·body are at most 2^53 (`TestUpstreamFormulaIdentity`); the certainty factor except for base/(base−head) > 2^10 (32 fixture rows are in that scope, 8 change, upstream within 2^-40 on all); the Fisher tests for base <= 170 (unchanged code). The information, the G-test and the Fisher tests for base > 170 differ in the last bits on ordinary data (F-1). Membership at a threshold can change for values within those last bits; the Fisher tests for large totals are much faster | implemented (issue #16) in `internal/ruleval` (`TestLargeTablesMatchReference`, the former expected failures, all within the bounds; `TestEvalMatchesUpstream`, rows beyond the bit-identity scope compared with the exact reference; `TestUpstreamFormulaIdentity`, `TestFisherLargeMatchesReference`, `TestFisherLargeMatchesPerTerm`, `TestFisherLargeCost`, `TestFisherLargeNearTie`, `TestCmpWeightBig`, `TestFisherLongWalk`, `TestCertaintyNearIndependence`, `TestHyperLnP`, `TestInfoBranches`, `TestPinnedBits`), root `TestALG1API` (the audit's CLI reproductions and the `-i` case) and `TestALG6Regression`, `cmd/fpgrowth` `TestALG1CLI`, `TestCertaintyNearIndependenceCLI` |
| N2 | defect fix (policy) | Upstream applies the relative thresholds with a tolerance of 1 ∓ 2^-52 in double arithmetic: `-s` and `-F` as ⌈s/100·W·(1−ε)⌉, `-S` as ⌊S/100·W·(1+ε)⌋, `-c` as "reject if supp < body·(c/100·(1−ε))" and the rule set minimum as ⌈body·(c/100·(1−ε))⌉ (`fpgrowth.c:1915-1920`, `fpg_report`, the `istree.c` rule test; ε = DBL_EPSILON). Once x = t·N exceeds about 2^52 the tolerance is more than one unit of support (up to W·2^-52, 512 for W = 2^61), so sets and rules just outside the bound are reported and `-c100`/`MinConfidence: 1` admits a confidence below 1. v0.1.0 had the same arithmetic (`Fraction`, `MinConfidence` via a percentage) | The threshold contract (below) is a hybrid. While x = t·N < 2^52 (for maxima x <= 2^52) every decision is upstream's double formula, bit for bit as in C and v0.1.0, so **no result changes below 2^52**, for any fraction or percentage (also values such as `1-0.8` or `21*0.01`, whose double rounding happens to absorb more than the formula's tolerance). At and above the switch the same formula is evaluated exactly with the tolerance capped below one unit: the minimum (and the confidence minimum) is ⌊x⌋, the maximum ⌈x⌉, so an integer x is exact (`Fraction(1)` is W, `-c100` admits only rules whose support equals the body support). Every minimum, maximum and confidence minimum is nondecreasing in the threshold and in N across the switch (proof in `internal/thresh`); maxima switch only above x = 2^52 because the double maximum at x <= 2^52 can be 2^52+1. Upstream's formula is kept below the switch even where it is already a full unit off: its tolerance x·2^-52 plus up to about 1.5·2^-53·x of rounding admits a support more than one unit outside x from about x = 2^51.6 (for t = 0.7677 and N = 4399772623513087, x ≈ 3377705443070997.11 and the minimum is 3377705443070996; for t = 0.3214 and N = 10509402814600758, x ≈ 3377722064612683.82 and the maximum is 3377722064612685). From x = 2^52 on the tolerance alone is at least one unit. Results differ from C and v0.1.0 only from x = 2^52 on (for maxima above it), where the bound is ⌊x⌋ or ⌈x⌉ instead of upstream's value, which is up to x·2^-52 more permissive (and, through the rounding of W and of the products, can also be up to about x·2^-53 stricter). Beyond x = 2^53 a fraction is only as precise as its float64 (`Fraction(0.1)` of 10·2^59 is 2^59+32); `Count` is exact | Both audit reproductions now give the exact result: `printf 'a 2305843009213693752\nb 2305843009213694152\n'`, `-w -s50` reports only `b`; `printf 'x y 4611686018427387000\nx 1000\n'`, `-tr -w -s0 -c100 -m2` reports only `x <- y`. `Fraction(1)` of W = 2^60+1 was a minimum of W−257 and is W | implemented (issue #17) in `internal/thresh` (`TestAgainstBig`, `TestMonotoneAcrossSwitch`, `TestPinned`, `FuzzFrac`), `internal/fpg` (`TestThresholdsIdenticalBelowSwitch`, `TestThresholdsPinnedUnchanged`, `TestThresholdsHugeWeights`, `TestThresholds` against the oracle), root `TestALG2API`, `TestALG2HugeWeights` (the apicheck class), `TestLibraryThresholdsUnchanged`, `TestFractionMatchesCLI`, `TestCheckRangeSound`, `TestExactCountThresholds`, `cmd/fpgrowth` `TestALG2CLI`, `TestBorderUnchanged`; the oracle evaluates the contract independently (`math/big` for the branch and the exact part, upstream's formulas in `upstream.go`) |
| N3 | defect fix (deviation) | Upstream parses the absolute thresholds `-s-<n>`, `-S-<n>` and negative `-F` entries with `strtod` into a double and uses ⌈−v⌉ (minimum, border) or ⌊−v⌋ (maximum), so values above 2^53 are rounded (off by up to 1024 in either direction near 2^63); v0.1.0 did the same (audit ALG-3). Upstream's 32-bit supports never reach that range. **Integer literals are exact** (issue #18): when the bytes that `strtod` consumes (so the accepted syntax and the position where option clustering continues are upstream's) form a negative base-10 integer, that is optional leading white space, `-`, one or more decimal digits (leading zeros allowed) and nothing strtod would also take (no `.`, exponent, `0x` prefix, `inf` or `nan`), the absolute support is that integer, exactly, for every value in [1, 2^63−1]; `-s`, `-S` and each `-F` entry independently, so absolute and percentage bounds mix freely. Below 2^53 this is upstream's value, so **no result changes for absolute thresholds up to 2^53**. An integer literal beyond 2^63−1 (from `-9223372036854775808` on) saturates at 2^63−1, like every other spelling of such a value and as in v0.1.0 and the C reference on this platform (upstream's conversion to `int` is undefined behavior, fpgrowth-core.md B9); no support exceeds 2^63−1, so a saturated minimum finds nothing unless a support is exactly 2^63−1, and a saturated maximum is no limit. Every other syntax keeps upstream's double and its rounding: `-s-2.5` is ⌈2.5⌉ = 3, `-S-2.5` is ⌊2.5⌋ = 2, `-s-17.0`, `-s-1e3` and `-s-0x11` are 17, 1000 and 17 (exact below 2^53, rounded above), and values at or beyond 2^63 (`-s-1e19`, `-S-inf`) saturate at 2^63−1 (fpgrowth-core.md B9). `-s-0` (also `-0.0`, `-000`) is −0, a percentage of 0, as upstream. The sign of `-s` still selects the default output format (`%a` for absolute). The library's `Count(n)` was already exact (L-5) | Both audit reproductions now give the exact result: `printf 'a 2305843009213693752\nb 2305843009213694152\n'`, `-w -s-2305843009213693753` reports only `b` (was also `a`, support 2305843009213693752); `-w -s-4611686018427387840` reports the item of support 4611686018427387840 (was rounded to 2^62, "no (frequent) items found") | implemented (issue #18) in `cmd/fpgrowth` (`TestSuppValue`, `TestAbsoluteOptions`, `TestALG3CLI` with 2^53 ± 1, 2^62, 2^63−1, 2^63 and 10^20 (saturated, as v0.1.0), clustered options, `-S`, `-F` and mixed kinds; `FuzzParseArgs` checks strtod's value and end position and an independent `strconv.ParseInt` at every offset), `internal/fpg` `TestCountThresholdsMixed` |

### Numeric contract

For every valid table with supports up to 2^63 − 1 (issue #16; the
tolerances of the independent reference `internal/oracle` are these
bounds, so the reference comparisons check them):

| Measure | Bound |
| --- | --- |
| `n` Chi2, `y` Yates | relative 8 ulps (2^-49) |
| `i` Information | relative 32 ulps (2^-47) |
| `p t g` p-values | relative 2^-36·(1 + x) for the statistic x (plus 2^-1000) |
| `z` Certainty | absolute 2^-40 |
| `f h m s` Fisher | relative 2^-36 (plus 2^-1000); for base <= 170, `h` and `s` absolute 2^-36 (upstream's complement sums) |
| `-i` | exact |
| other measures | relative or absolute 2^-44 (short formulas of the supports) |

The Fisher tests take time proportional to the standard deviation σ <=
√base/4 of the hypergeometric distribution (about 9σ per side; about 100 s
per evaluation near 2^63, extrapolated) and are cancellable every 4096
tables. A FisherProb near-tie (two tables whose computed probabilities
agree within their error bound) more than 1024 tables apart
is decided with 320-bit logarithms (bounded cost); only probabilities equal
within a relative 2^-200 count as ties, and ties count as in the region (R6).

### Threshold contract

Every relative threshold (issue #17, N2) is a float64 fraction t; a CLI
percentage p is the fraction p/100 as a float64 division, upstream's own
first step, so `-sk` and `Fraction(float64(k)/100)` are the same
threshold at every weight. With the reference support N, x = t·N is the
exact product of t's binary value and N, ε = 2^-52, and RN rounds to
float64 in the order shown:

| Threshold | N | Below the switch (upstream, unchanged) | Switch | At and above the switch (exact, capped) |
| --- | --- | --- | --- | --- |
| minimum support (`-s`, `MinSupport: Fraction`), border (`-F`, `Border`), rule body minimum | total weight W | s >= ⌈RN(RN(t·RN(W))·(1−ε))⌉ | x < 2^52 | s >= ⌊x⌋ (= max(⌈x·(1−ε)⌉, ⌊x⌋)) |
| maximum support (`-S`, `MaxSupport: Fraction`) | W | s <= ⌊RN(RN(t·RN(W))·(1+ε))⌋ | x <= 2^52 | s <= ⌈x⌉ (= min(⌊x·(1+ε)⌋, ⌈x⌉)) |
| confidence (`-c`, `MinConfidence`) | the rule's body support | RN(s) >= RN(RN(N)·RN(t·(1−ε))) | x < 2^52 | s >= ⌊x⌋ |
| rule set minimum (body support definition) | the minimum body support | ⌈RN(N↓·RN(t·(1−ε)))⌉, N↓ = N rounded toward zero | x < 2^52 | ⌊x⌋ |

The branch is decided by the exact x. Below the switch the decisions are
those of C and v0.1.0, also where they are already a full unit off:
upstream's tolerance x·2^-52 plus up to about 1.5·2^-53·x of rounding
admits a support more than one unit outside x from about x = 2^51.6 (for
t = 0.7677 and N = 4399772623513087, x ≈ 3377705443070997.11 and the
minimum is 3377705443070996; for t = 0.3214 and N = 10509402814600758,
x ≈ 3377722064612683.82 and the maximum is 3377722064612685). From
x = 2^52 on the tolerance alone is at least one unit, and the exact
evaluation keeps it below one unit. All bounds
are nondecreasing in t and N across the switch, and every support meets
the minimum or the maximum of the same t. Results saturate at 2^63−1;
t = 0 (also −0) gives 0 and +Inf no limit. `MinConfidence` reaches the
contract as the fraction of its percentage (L-7), as in v0.1.0. Absolute
thresholds (`Count`, `-s-n`) are not part of it; integer literals at the
CLI are exact (N3). The evaluation
threshold (`-d`, `Evaluation.Threshold`) compares a computed measure
value and is not part of this contract (F-1).

## Output (CLI)

| # | Kind | Upstream behavior (evidence, location) | Go behavior | Status / test |
| --- | --- | --- | --- | --- |
| O1 | defect fix | `-h` (record header) has no effect (report B1, cli; `report.c:884`: copied, but `rep->hdr` is never set) | Header is written as documented | implemented in `internal/isrout` (golden `t1-hdr`, `t1-hdr-empty`); CLI golden `dev-h` |
| O2 | defect fix | `-g` (scanable item names) has no effect (report B2, cli; `report.c:661, 711-727, 876`) | Names are quoted as documented | implemented in `internal/isrout` (golden `names-g`); CLI golden `dev-g` |
| O3 | defect fix (O3a, O3b) | Fast output path (absolute-support formats): (a) prints a spurious empty-set line when there are top-level perfect extensions (`report.c:1336-1366`, no `cnt >= zmin` test); (b) writes a wrong pattern spectrum even without them (`report.c:1346, 1550-1551`, `supps[cnt]` not filled) (report B3, core B6, cli; review F7) | Single output path; no spurious line. The logged count of pattern-spectrum signatures for `-P` differs accordingly | implemented in `internal/report` (simulator script `allA_file_m1`); CLI goldens `dev-fast-empty`, `psp-out` |
| O4 | defect fix | Count-only mode (no output file) ignores `-n` for closed/maximal counts (report B4; `report.c:1496-1505` vs `1537`) | Counts honour `-n` | implemented in `internal/report` (`TestReporter_CountOnlyClosedZmax`, script `test1_closed_n1_count`) |
| O5 | defect fix | Binomial set count overflows with 63 perfect extensions (report B5; `report.c:1515-1517`) | Saturating unsigned 64-bit count: exact up to 64 perfect extensions (64 give exactly 2^64−1); from 65 on it saturates at 2^64−1 and is not the true count | implemented in `internal/report` (`TestReporter_CountOnlyBinomials`: 62, 63, 64 and 100 perfect extensions) |
| O10 | defect fix (policy) | `%d` works only on the fast path (sets with absolute support format) and stays literal otherwise (report B6; `report.c:147` vs `1943-1974`) | For item sets `%d` always prints the absolute support; for rules it stays literal as upstream | implemented in `internal/isrout` (golden `t1-dnormal`); CLI golden `dev-d` |
| O9 | improvement | Pattern spectrum memory grows with the support range (162 MB for two sets, tract; `patspec.c:250-266`) | Sparse rows; identical output | implemented in `internal/report` (`TestSpectrum_SparseMemory`) |
| O6 | defect fix | `-Z` statistics are spliced into stdout output at 64 KiB boundaries (report B9, cli; `fpgrowth.c:2528-2531`, `report.c:177-200`) | Statistics are written after the item sets | implemented in `internal/isrout` (`TestWriter_WriteStats`); CLI goldens `cluster-Z`, `dev-Z-stdout` |
| O7 | defect fix | `-P` write errors are ignored (cli; `fpgrowth.c:2542-2545`, `patspec.c:832`) | Reported as errors (exit 4). A closed standard output is not detected by Go; see C10 | implemented in `internal/isrout` (`TestWriteSpectrumErrors`), `cmd/fpgrowth` (`TestOutputFiles`, failing in-process writer) |
| O8 | preserved | Number formatting rounds half-to-odd and differs from `printf` (report B7/B8; `report.c:296-302, 349-350`) | **Preserved** for byte-compatible output (not a defect). Listed here as preserved behavior; the independent audit counted it among the platform differences, but it is upstream's own formatter, reproduced on every platform | implemented in `internal/isrout` (18,829-case numout corpus) |
| O11 | defect fix | `isr_prefmt` pre-formats every integer from the minimum support to the largest item support; its byte count is an `int` and overflows above a support of about 2.2·10^8, so the program crashes or runs out of memory, and below that the table needs O(max support) memory (reference-build F5; `report.c:944-949, 963`, called from `fpgrowth.c:2004` unless `-N`): `printf 'a 1000000000\n'`, `-w`, exit 139 | No pre-format table; `-N` is accepted and changes nothing. The reproducer prints `a (100)` | implemented in `internal/isrout` (no table exists; CLI goldens `dev-prefmt`, the reproducer, and `prefmt-N`, the same with `-N`) |

## Input (CLI)

| # | Kind | Upstream behavior (evidence, location) | Go behavior | Status / test |
| --- | --- | --- | --- | --- |
| I1 | defect fix | Escape decoding of `-r/-f/-b/-C` is broken for unknown escapes and a trailing backslash; it reads past the terminating NUL (cli; `escape.c:134-135, 168-171`) | Correct decoding | implemented in `internal/tabread` (`TestDecodeEscapes`); CLI goldens `dev-esc`, `dev-esc-end` |
| I2 | defect fix | Non-ASCII separator bytes index a table with a signed `char` and write out of bounds (cli; `escape.c:131-133`, `tabread.c:153-154`) | Bytes ≥ 0x80 handled like any other byte. Separators are byte sets, so a multibyte UTF-8 separator such as `é` is two separator bytes | implemented in `internal/tabread`; CLI golden `dev-nonascii` |
| I3 | defect fix | A final record ending in a field separator without newline is lost (tract T3; `tract.c:704`, `tabread.c:224-227`) | The record is kept | implemented in `internal/tabread`; CLI golden `dev-lastsep` |
| I4 | defect fix | Fields are silently truncated at 1024 bytes (cli; `tabread.c:230-232`) | No length limit | implemented in `internal/tabread`; CLI golden `dev-longfield` |
| I5 | defect fix (I5a, I5b) | (a) "read error" message prints the current field instead of the file name (`tract.c:217, 838`); (b) record numbers are off by one in one message (`tabread.c:217-218`) (cli) | Correct file name and record number | implemented in `internal/tabread`; CLI goldens `dev-dir` (a), `dev-recnum` (b) |
| I7 | defect fix (same root cause as M7) | Weights are truncated from `long` to `int` (tract T6; `tract.h:270`) | 64-bit weights; values beyond int64 are an invalid-weight error | implemented in `internal/tabread` (`TestReaderNext`, weight 4294967297) |
| I8 | platform | `nan(...)` payload syntax follows Apple libc (any bytes) | ISO C syntax (letters, digits, `_`) for option values | implemented in `internal/cconv` (`TestStrtodNaN`) |
| I6 | defect fix (same root cause as M7) | Weight-sum overflow segfaults (cli) | Error (see M7) | implemented (see M7, C4) |

## CLI validation, messages and limits

| # | Kind | Upstream behavior (evidence, location) | Go behavior | Status / test |
| --- | --- | --- | --- | --- |
| C1 | defect fix (same root cause as M8) | `-s`, `-S`, `-c`, `-d`, `-F` accept NaN and mine with undefined conversions (reference-build; review F8; `fpgrowth.c:1915-1920`, no `isnan` checks) | Rejected: "invalid maximum support nan" (exit 11), "invalid support border nan" (exit 11), "invalid evaluation threshold nan" (exit 13); `-snan`/`-cnan` are rejected with upstream's range-error messages "invalid minimum support nan" (exit 11) / "invalid minimum confidence nan" (exit 12) | implemented in `cmd/fpgrowth` (CLI goldens `dev-snan`, `dev-maxsupp-nan`, `dev-cnan`, `dev-dnan`, `dev-Fnan`, listed in `cmd/fpgrowth/testdata/deviations.txt`) |
| C2 | latent | `-F` with an empty value uses an uninitialized border entry (cli B16; `fpgrowth.c:2166-2173`; undefined behavior by code reading, no visible effect in the audit) | Only parsed values count | implemented in `cmd/fpgrowth` (`TestBorderEmptyValue`; `TestParseArgs` cases "border empty value B16" and "border without value B16"; CLI golden `F-trailing`) |
| C3 | defect fix (same root cause as O7) | Write errors on `-Z` output to stdout are ignored (`fpgrowth.c:2528-2531`) | Reported, exit 4. A closed standard output is not detected by Go; see C10 | implemented in `cmd/fpgrowth` (`TestOutputFiles`, failing in-process writer) |
| C4 | defect fix (same root cause as M7) | Weight-sum overflow segfaults | "<name>: total transaction weight too large" (exit 21) | implemented in `cmd/fpgrowth` (CLI golden `dev-bigw`) |
| C5 | platform | Trees beyond the C address space crash | "database too large" (exit 1) | implemented in `internal/fpg` (`fpg.ErrTooLarge`; `TestArenaOverflow`, `TestTooLarge`) |
| C6 | improvement | Write error: mining completes, error reported at close (exit 4) | Same message and exit 4, but mining stops at the first failed write, so the logged set count is the number written | implemented in `cmd/fpgrowth` (`TestWriteError`) |
| C7 | platform | Log times are CPU time (`clock()`) | Wall-clock time; same format. The second input pass runs inside the recoding step, so its time is logged there rather than under "sorting and reducing transactions" (review F10) | implemented in `cmd/fpgrowth` (`TestLogTimesWallClock`) |
| C8 | new | No interrupt handling in the reference build (`FPG_ABORT` off) | SIGINT cancels mining: "<prog>: interrupted", exit 130; stdin spool file removed | implemented in `cmd/fpgrowth` (`TestInterrupt`, `TestMain_interrupt`, `TestRun_cancelDuringReport`) |
| C9 | platform | stdin is read once into memory | stdin is spooled to a temporary file (two passes); failure: "cannot create temporary file for <stdin>" (exit 2) | implemented in `cmd/fpgrowth` (`TestStdinMatchesFile`, `TestSpoolRemoved`, `TestInputErrors`) |
| C10 | platform | Standard output closed by the caller (`fpgrowth t1.tab - >&-`): the first write fails, "write error on file <stdout>", exit 4 | Exit 0, nothing reported: at startup the Go runtime opens `/dev/null` for a closed file descriptor 0–2, so writes to standard output succeed and the case cannot be told apart from `>/dev/null`. The same holds for `-Z` and `-P ''` to standard output. Scripts that rely on exit 4 to detect a closed standard output lose the results silently; redirect to a file instead. Write errors on an open but failing output (full disk, broken pipe, failing file) are reported as in C3, C6, O7 | implemented in `cmd/fpgrowth` (`TestMain_closedStdout`, Unix only: it runs the program with standard output closed by the shell; audit DIF-5) |

## Preserved upstream behavior (not deviations)

Upstream behavior that may look like a defect but is kept for
compatibility. Changing any of it would be a deviation and needs a row
above.

| # | Kind | Behavior (location) | Status | Test |
| --- | --- | --- | --- | --- |
| P1 | preserved | `-n0`, `-S0` and infinite threshold values are accepted and give empty or saturated results | documented | — |
| P2 | preserved | Integer option values (`-m -n -p -q -l`) are truncated from C `long` to `int` | implemented | `internal/cconv` (`TestLongToInt`) |
| P3 | preserved | An embedded NUL byte ends an item name, so names that differ only after a NUL are one item (tract T21; `tract.c:725, 735`, `symtab.c:68-94`): `a\0x b` and `a\0y b` give one item `a` in both programs | implemented | `internal/tabread` (`TestReaderNext` "nul cuts an item", `TestReadSelection` "nul cuts names") |
| P4 | preserved | Pattern-spectrum counts (`-P`) are printed with `%g`, so counts of 10^6 and more are rounded (tract T10; `patspec.c:822`): a 25-item transaction prints `5.2003e+06` in both programs | implemented | `internal/isrout` (`TestWriteSpectrum`) |
| P5 | preserved | Items of equal frequency are ordered by an unstable sort, so their tie order depends on the removed items (tract T8; `arrays.c:516-585`, `tract.c:889`) | implemented | `internal/tract` (`TestPtrQsort_ties`, `TestPtrQsort_upstream`) |
| P6 | preserved | With an evaluation measure and the default aggregation `-ax` (`AggregateFirst`), an item set is evaluated by the rule whose head is its first item in the internal item order. That order depends on the variant, the tuning options (such as `-q`) and the target, so the `Value` (`%e`) of an item set can differ between them even when nothing is filtered (`fpgrowth -s20 -m3 -n3 -el -d0 -v' %e' test1.tab`: `{a,b,c}` has lift 1.14286 with `-q2` and 1.11111 with `-q-2`, `{a,b,d}` 0.857143 and 1.25), and with an evaluation threshold the reported sets can differ too (audit DIF-3: on 200 random transactions, `-ts -s2 -el -d105` gives 164 to 167 sets across `-q` and `-A`, in C and Go alike). Without an evaluation, or with `-am -an -aa`, values and sets do not depend on the order | documented | — (`measure.go`, `AggregateFirst`) |

## Library API (no upstream counterpart; behavior relative to the CLI)

| # | Kind | CLI / upstream behavior | Library behavior | Status / test |
| --- | --- | --- | --- | --- |
| L-1 | new | Appearance file items are registered in file order | `RuleOptions.Appearance` (a map) is registered in first-occurrence order in the data, so Go map order cannot leak into results; tie order among equal-frequency items may differ from the CLI with an appearance file (results identical) | implemented (`TestRulesDeterministicAppearance`, `TestRulesAppearanceEquivalence`) |
| L-2 | improvement | Empty database or no frequent items: error "no (frequent) items found" (exit 15) | Not an error: nothing is reported (even with `IncludeEmpty`), `Summary.FrequentItems` is 0 | implemented (`TestNoFrequentItems`) |
| L-3 | improvement | `-n` below `-m` gives empty output | `MaxSize < MinSize` is rejected with `ErrInvalidOptions` | implemented (`TestOptionsValidation`) |
| L-4 | new | Upstream table format only | Additional RFC 4180 CSV sources (`CSVFile`, `CSVReader`) with their own documented semantics | implemented (`TestCSVFile`, `TestCSVFileMining`, `FuzzCSVFile`) |
| L-5 | new | Thresholds given in percent (`-s`, `-S`) or negative absolute values | `Fraction(f)` applies the threshold contract (N2) to `f` directly: upstream's formula below 2^52, exact above; `Fraction(float64(k)/100)` is `-sk` at every weight; `Count(n)` is exact for all int64 values (as the CLI's absolute integer literals since issue #18, N3) | implemented (`TestFractionFormula`, `TestFractionMatchesCLI`, `TestExactCountThresholds`) |
| L-6 | new | `-e` always filters by the evaluation threshold (`-d`, default 10 %) | `Evaluation` without `Filter` (the zero value) computes `Value` for every result and removes nothing; `Filter` enables the threshold (and `PruneSize`), a non-zero `Threshold` without `Filter` is rejected. Without an `Evaluation`, `Value` is NaN | implemented (`TestEvaluationWithoutFilter`, `TestOptionsValidation`) |
| L-7 | new | Confidence and evaluation thresholds are percentages (`-c 80`, `-d 10`), divided by 100 internally | `MinConfidence` and `Evaluation.Threshold` are fractions in units of the measure (0.8, 0.1); they are passed as the percentage `p` with `p/100 == f`, or, where no float64 has that property (about 15 % of values), as the neighbor on the permissive side, so a threshold never rejects a result that the fraction itself admits. The confidence p/100 then follows the threshold contract (N2) | implemented (`TestPercentOf`, `TestPlanMapping`, `TestLibraryThresholdsUnchanged`) |
