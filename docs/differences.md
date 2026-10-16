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
| M1 | `-At` copies the item map from the wrong array: crash or wrong item names when an item is dropped at the top level (core B1) | Correct map; same sets as the other variants | decided |
| M2 | `-s0` crashes on zero-frequency items or reports support-0 sets (core B2, tract) | Effective absolute minimum support is at least 1 | decided |
| M3 | Generators with `-m ≥ 2` are missed because the `zmin` transaction filter deflates subset supports (core B3) | No `zmin` filter for the generator target | decided |
| M4 | Maximal target reports nothing when every frequent item is a top-level perfect extension (core B4) | The maximal set is reported | decided |
| M5 | Closed/maximal targets with `-n` can report non-closed/non-maximal sets (core B5) | Closure is always computed; results equal the brute-force definition | decided |
| M6 | Negative weights corrupt supports and crash the 16-items machine (core B7, tract) | Negative weights are rejected with an error | decided |
| M7 | Supports and weight sums are 32-bit `int`; overflow is undefined behavior (tract, cli) | 64-bit supports; total-weight overflow is an error | decided |
| M8 | Out-of-range floating-point thresholds are converted to `int` with undefined behavior (core B9) | Explicit clamping | decided |

## Rules and evaluation

| # | Upstream behavior (evidence) | Go behavior | Status / test |
| --- | --- | --- | --- |
| R1 | `-eb` always selects the log support quotient; the rule measure "imbalance ratio" is unreachable, and `-tr -eb` evaluates to 0 (rules B1) | decision pending | decided later |
| R2 | `-p` has no effect: `ist_filter` runs before `ist_seteval` (rules B2) | decision pending | decided later |
| R3 | Head-only items (`-R`) appear in rule bodies (rules B3) | Appearance constraints honoured | decided |
| R4 | Conviction is 0 when confidence is 1 (rules B4) | decided in `internal/ruleval` | decided |
| R5 | Certainty factor is NaN when the head occurs in every transaction (rules B5) | Finite value; no measure returns NaN | decided |
| R6 | Fisher table-probability/information variants wrong for tied tables (rules B6) | Correct computation | decided |
| R7 | Information measure can be slightly negative; G-test asserts in debug builds (rules B7) | Clamped at 0 | decided |
| R8 | `-S` applies to body ∪ head support even without `-o` (rules B8) | `-S` applies to the same support as `-s` | decided |
| R9 | Two Fisher variants can return negative p-values (rules B9); Yates correction not clamped (B10); `logGamma(170.5)` returns 0 (B11) | Values clamped/corrected | decided |

## Output (CLI)

| # | Upstream behavior (evidence) | Go behavior | Status / test |
| --- | --- | --- | --- |
| O1 | `-h` (record header) has no effect (report B1, cli) | Header is written as documented | decided |
| O2 | `-g` (scanable item names) has no effect (report B2, cli) | Names are quoted as documented | decided |
| O3 | Fast output path prints a spurious empty-set line and a wrong pattern spectrum (report B3, core B6, cli) | Single output path; no spurious line | decided |
| O4 | Count-only mode (no output file) ignores `-n` for closed/maximal counts (report B4) | Counts honour `-n` | decided |
| O5 | Binomial set count overflows with 63 perfect extensions (report B5) | Saturating 64-bit count | decided |
| O6 | `-Z` statistics are spliced into stdout output at 64 KiB boundaries (report B9, cli) | Statistics are written after the item sets | decided |
| O7 | `-P` write errors are ignored (cli) | Reported as errors | decided |
| O8 | Number formatting rounds half-to-odd and differs from `printf` (report B7/B8) | **Preserved** for byte-compatible output (not a defect) | decided |

## Input (CLI)

| # | Upstream behavior (evidence) | Go behavior | Status / test |
| --- | --- | --- | --- |
| I1 | Escape decoding of `-r/-f/-b/-C` is broken for unknown escapes and a trailing backslash (cli) | Correct decoding | decided |
| I2 | Non-ASCII separator bytes write out of bounds (cli) | Bytes ≥ 0x80 handled like any other byte | decided |
| I3 | A final record ending in a field separator without newline is lost (tract) | The record is kept | decided |
| I4 | Fields are silently truncated at 1024 bytes (cli) | No length limit | decided |
| I5 | "read error" message prints the current field instead of the file name; record numbers off by one in one message (cli) | Correct file name and record number | decided |
| I6 | Weight-sum overflow segfaults (cli) | Error (see M7) | decided |
