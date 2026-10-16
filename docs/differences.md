# Differences from upstream FP-Growth 6.21

Every intentional behavioral difference between this port and Christian
Borgelt's C implementation (version 6.21) is listed here, with the evidence
for the upstream behavior and the test that pins the Go behavior. Code
sites are marked with `// Deviation from upstream:`.

Bug identifiers refer to the audit notes: `core` = `docs/upstream/fpgrowth-core.md`,
`report` = `docs/upstream/report.md`, `rules` = `docs/upstream/rules-eval.md`,
`tract` = `docs/upstream/tract.md`, `cli` = `docs/upstream/cli-io.md`.

Status: **decided** (policy fixed, implementation pending) or
**implemented** (code and regression test exist; test named).

## Mining results

| # | Upstream behavior (evidence) | Go behavior | Status / test |
| --- | --- | --- | --- |
| M1 | `-At` copies the item map from the wrong array: crash or wrong item names when an item is dropped at the top level (core B1) | Correct map; same sets as the other variants | implemented in `internal/fpg` (`TestDeviations`, golden `deviations.txt`) |
| M2 | `-s0` crashes on zero-frequency items or reports support-0 sets (core B2, tract) | Effective absolute minimum support is at least 1 | implemented in `internal/fpg` (`TestDeviations`, golden `deviations.txt`) |
| M3 | Generators with `-m ≥ 2` are missed because the `zmin` transaction filter deflates subset supports (core B3) | No `zmin` filter for the generator target | implemented in `internal/fpg` (`TestDeviations`, golden `deviations.txt`) |
| M4 | Maximal target reports nothing when every frequent item is a top-level perfect extension (core B4) | The maximal set is reported | implemented in `internal/report` (scripts `mp`, `ab2`, property test) |
| M5 | Closed/maximal targets with `-n` can report non-closed/non-maximal sets (core B5) | Closed/maximal search is not depth-limited by `zmax`; the reporter's size gate decides what is reported, so results equal the brute-force definition. Cost: can be slower than upstream with `-n` on large data | implemented in `internal/fpg` (`TestDeviations`, oracle seed 678) |
| M6 | Negative weights corrupt supports and crash the 16-items machine (core B7, tract) | Negative weights are rejected with an error | decided |
| M7 | Supports and weight sums are 32-bit `int`; overflow is undefined behavior (tract, cli) | 64-bit supports; total-weight overflow is an error | decided |
| M9 | Maximal target with `-x` and the 16-items machine (`-As`/`-Ad`) loses maximal sets, e.g. a single transaction `a` gives no output (reference-build bug 3) | Maximal sets reported in all variants | implemented in `internal/fpg` (`TestDeviations`, golden `deviations.txt`) |
| M8 | Out-of-range floating-point thresholds are converted to `int` with undefined behavior (core B9) | Explicit clamping | implemented in `internal/fpg` (`TestDeviations`, golden `deviations.txt`) |

## Rules and evaluation

| # | Upstream behavior (evidence) | Go behavior | Status / test |
| --- | --- | --- | --- |
| R1 | `-eb` always selects the log support quotient; the rule measure "imbalance ratio" is unreachable, and `-tr -eb` evaluates to 0 (rules B1) | For `-tr`, `-eb` selects the imbalance ratio as documented by `-!`; for item sets it stays the log support quotient | implemented in `internal/istree` (`TestReport_ImbalanceB1`); CLI mapping pending |
| R2 | `-p` has no effect: `ist_filter` runs before `ist_seteval` (rules B2) | Evaluation set before filtering, so `-p` prunes item sets as documented (rules unaffected, matching patched C `reference/patches/istree/fpgrowth-p-order.patch`) | implemented in `internal/istree` (`TestFilter_CallOrderB2`) |
| R3 | Head-only items (`-R`) appear in rule bodies (rules B3) | Appearance constraints honoured | implemented in `internal/istree` (`TestReport_HeadOnlyB3`) |
| R4 | Conviction is 0 when confidence is 1 (rules B4) | +Inf (passes every (+) threshold) | implemented in `internal/ruleval` |
| R5 | Certainty factor is NaN when the head occurs in every transaction (rules B5) | Finite value; no measure returns NaN | implemented in `internal/ruleval` (regression tests, exact big.Rat oracle for Fisher) |
| R6 | Fisher table-probability/information variants wrong for tied tables (rules B6) | Correct computation | implemented in `internal/ruleval` (regression tests, exact big.Rat oracle for Fisher) |
| R7 | Information measure can be slightly negative; G-test asserts in debug builds (rules B7) | Clamped at 0 | implemented in `internal/ruleval` (regression tests, exact big.Rat oracle for Fisher) |
| R8 | `-S` applies to body ∪ head support even without `-o` (rules B8) | `-S` applies to the same support as `-s` | implemented in `internal/istree` (`TestReport_MaxSuppB8`) |
| R10 | After fixing R2, the generator filter compares against the total weight with the skip flag set, so `-tg -p` reports nothing (patched C) | Flag masked | implemented in `internal/istree` (`TestCloMax_GeneratorsTotalWeight`) |
| R11 | `ist_init` reads past its array when `-m` exceeds the number of items; recursive report results ignored | Guarded; sink errors abort and are returned | implemented in `internal/istree` |
| R9 | Two Fisher variants can return negative p-values (rules B9); Yates correction not clamped (B10); `logGamma(170.5)` returns 0 (B11) | Values clamped/corrected | implemented in `internal/ruleval` (regression tests, exact big.Rat oracle for Fisher) |

## Output (CLI)

| # | Upstream behavior (evidence) | Go behavior | Status / test |
| --- | --- | --- | --- |
| O1 | `-h` (record header) has no effect (report B1, cli) | Header is written as documented | implemented in `internal/isrout` (golden `t1-hdr*`) |
| O2 | `-g` (scanable item names) has no effect (report B2, cli) | Names are quoted as documented | implemented in `internal/isrout` (golden `names-g`) |
| O3 | Fast output path prints a spurious empty-set line and a wrong pattern spectrum (report B3, core B6, cli) | Single output path; no spurious line | implemented in `internal/report` (script `allA_file_m1`) |
| O4 | Count-only mode (no output file) ignores `-n` for closed/maximal counts (report B4) | Counts honour `-n` | implemented in `internal/report` (test1 `-n1` scripts) |
| O5 | Binomial set count overflows with 63 perfect extensions (report B5) | Saturating 64-bit count | implemented in `internal/report` (63/64/70-pex scripts) |
| O10 | `%d` works only on the fast path (sets with absolute support format) and stays literal otherwise (report B6) | For item sets `%d` always prints the absolute support; for rules it stays literal as upstream | implemented in `internal/isrout` (golden `t1-dnormal`) |
| O9 | Pattern spectrum memory grows with the support range (162 MB for two sets, tract) | Sparse rows; identical output | implemented in `internal/report` (memory test) |
| O6 | `-Z` statistics are spliced into stdout output at 64 KiB boundaries (report B9, cli) | Statistics are written after the item sets | implemented in `internal/isrout` (`TestWriter_WriteStats`) |
| O7 | `-P` write errors are ignored (cli) | Reported as errors | implemented in `internal/isrout` (`TestWriteSpectrumErrors`) |
| O8 | Number formatting rounds half-to-odd and differs from `printf` (report B7/B8) | **Preserved** for byte-compatible output (not a defect) | implemented in `internal/isrout` (18,829-case numout corpus) |

## Input (CLI)

| # | Upstream behavior (evidence) | Go behavior | Status / test |
| --- | --- | --- | --- |
| I1 | Escape decoding of `-r/-f/-b/-C` is broken for unknown escapes and a trailing backslash (cli) | Correct decoding | implemented (`internal/tabread`, golden deviation list) |
| I2 | Non-ASCII separator bytes write out of bounds (cli) | Bytes ≥ 0x80 handled like any other byte | implemented (`internal/tabread`, golden deviation list) |
| I3 | A final record ending in a field separator without newline is lost (tract) | The record is kept | implemented (`internal/tabread`, golden deviation list) |
| I4 | Fields are silently truncated at 1024 bytes (cli) | No length limit | implemented (`internal/tabread`, golden deviation list) |
| I5 | "read error" message prints the current field instead of the file name; record numbers off by one in one message (cli) | Correct file name and record number | implemented (`internal/tabread`, golden deviation list) |
| I7 | Weights are truncated from `long` to `int` (tract T6) | 64-bit weights; values beyond int64 are an invalid-weight error | implemented (`internal/tabread`) |
| I8 | `nan(...)` payload syntax follows Apple libc (any bytes) | ISO C syntax (letters, digits, `_`) for option values | implemented (`internal/cconv`) |
| I6 | Weight-sum overflow segfaults (cli) | Error (see M7) | decided |
