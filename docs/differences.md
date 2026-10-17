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
(`docs/audit/differences.md` on the audit branch).

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
| M10 | defect fix | The bucket sort of the transactions recurses once per item of a prefix shared by more than 16 transactions and overflows the stack (tract T4; `tract.c:2225-2229, 2257, 2261, 2286`): 17 identical transactions of 100,000 items plus 100,010 one-item transactions, `-s-17 -n1`, exit 139 | No recursion over the transaction length: the bag is a prefix tree built incrementally, its child lists are sorted node by node in a flat loop, and it is traversed with an explicit stack (`internal/tract/bag.go`); the audit's reproducer completes (100,000 sets). Not a deviation, but slow on this input in both programs: mining such a single long chain with `-n1` for the frequent (default) target is quadratic in the chain length, in upstream C and in Go alike, because the perfect extensions are enumerated once per item (upstream `rec_cmplx`, `isr_report`, `isr_remove`; the same in Go): at 100,000 items Go took 34.9 s and C 31.2 s CPU time, with identical output (follow-up investigation of the audit) | implemented in `internal/tract` (`TestItemBase_BuildBag_longSharedPrefix`: the audit's reproducer, checks the bag's transactions and weights; since Go stacks grow, it would not catch a reintroduced recursion, which the iterative code rules out by construction) |
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
| F-1 | platform | Log-based values (`-eb`, measures `j p t i g f h m s`) use the platform libm; results are libm-dependent | Go's `math` package, which can differ by 1 ulp from Apple libm (e.g. `log 3`, `log 14`). A set or rule whose true measure value lies exactly on the threshold (e.g. exactly independent items with `-eb -d0`) may be included by one implementation and not the other (review F1). This holds for **every** measure, not only the log-based ones: a value that is mathematically equal to the threshold is computed with rounding and may fall on either side, in C and in Go alike (e.g. `printf 'x y 5\nx 5\ny 4\nz 6\n'`, `-tr -w -s0 -c0 -m2 -ed -d5` omits `y <- x`, whose value is exactly 0.05 but computes to 0.049999999999999988898; C gives the same; audit ALG-4). All other values are bit-identical to the C reference built with `-ffp-contract=off` (not necessarily to a stock upstream build, since arm64 clang contracts by default) | documented; regression test pins Go's value on the review reproducer; difftest tolerates boundary cases |

## Numeric limits

Limits of float64 arithmetic that matter only for very large total
transaction weights or thresholds. Upstream cannot reach most of this
range (32-bit supports), so N1 is new exposure created by the 64-bit
supports; N2 and N3 are upstream semantics. None of them affects data
whose total weight is below about 10^12. Found by the independent audit
(ALG-1 to ALG-3); not changed, because exact arithmetic would change the
last bits against C on normal data.

| # | Kind | Behavior | Evidence | Status |
| --- | --- | --- | --- | --- |
| N1 | limit | The statistical measures lose precision in float64 when the total transaction weight is large: chi² and its p-value (`n p`), Yates (`y t`), information and the G-test p-value (`i g`; `head·body − supp·base` and `log(base) + Σ/base` cancel), the certainty factor (`z`; `head/base` rounds to 1), the four Fisher tests (`f h m s`; log-gamma differences) and "invalidate below expected support" (`-i`; products beyond 2^53). They are **reliable only below a total transaction weight of about 10^12**; beyond that strong associations can get p = 1, chi² = 0 or information 0 and be filtered out (or kept wrongly by a (−) threshold), without a warning. The rational measures (`c d l a q v e r b k w u j`) were exact to rounding in all large checks | On the table supp = 3k−2, body = 3k, head = 9k−2, n = 9k (exact chi² p 0.0455003, G-test p 0.0360561): at n = 9·10^9 Go gives 0.0455003 and 0.0360564; at 9·10^12 0.0454978 and 0.0356771; at 9·10^13 the G-test p is 0.0501 (crosses 5 %); at 9·10^14 G-test p = 1 and information 0; from 9·10^16 the chi² p-value is 1. Randomized: 0 mismatches against a brute force in 32,000 cases with small weights; all 27 mismatches of the large-weight campaign had a total weight ≥ 10^13 | documented (audit ALG-1) |
| N2 | limit (upstream semantics) | The thresholds are applied with a relative tolerance (1 − 2^-52): `-s`/`-S` as `smin/100·W·(1−ε)`, `-c` (`MinConfidence`) multiplied by (1 − ε). Above a total weight of about 2^52 this admits sets and rules just outside the requested bound (by up to W·2^-52, e.g. 512 for W = 2^61), and `-c100`/`MinConfidence: 1` no longer means "exact rules only" | `printf 'a 2305843009213693752\nb 2305843009213694152\n'`, `-w -s50` reports `a` at 49.999999999999992895 %; `printf 'x y 4611686018427387000\nx 1000\n'`, `-tr -w -s0 -c100 -m2` reports `y <- x` at 99.999999999999971578 % confidence | documented (audit ALG-2) |
| N3 | limit (upstream semantics) | CLI absolute thresholds (`-s-<n>`, `-S-<n>`) are parsed as C doubles, as upstream, so values above 2^53 are rounded (off by up to 1024 in either direction near 2^63). The library's `Count(n)` is exact over the whole `int64` range | `-w -s-2305843009213693753` reports a set of support 2305843009213693752; `-s-4611686018427387840` rounds to 2^62 and finds no frequent item although one has that support | documented (audit ALG-3) |

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
| L-5 | new | Thresholds given in percent (`-s`, `-S`) or negative absolute values | `Fraction(f)` applies upstream's rounding formula to `f` directly; `Count(n)` is exact for all int64 values (unlike the CLI, N3) | implemented (`TestFractionFormula`, `TestExactCountThresholds`) |
| L-6 | new | `-e` always filters by the evaluation threshold (`-d`, default 10 %) | `Evaluation` without `Filter` (the zero value) computes `Value` for every result and removes nothing; `Filter` enables the threshold (and `PruneSize`), a non-zero `Threshold` without `Filter` is rejected. Without an `Evaluation`, `Value` is NaN | implemented (`TestEvaluationWithoutFilter`, `TestOptionsValidation`) |
| L-7 | new | Confidence and evaluation thresholds are percentages (`-c 80`, `-d 10`), divided by 100 internally | `MinConfidence` and `Evaluation.Threshold` are fractions in units of the measure (0.8, 0.1); they are passed as the percentage `p` with `p/100 == f`, or, where no float64 has that property (about 15 % of values), as the neighbor on the permissive side, so a threshold never rejects a result that the fraction itself admits | implemented (`TestPercentOf`, `TestPlanMapping`) |
