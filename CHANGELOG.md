# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While the major version is 0, minor versions may contain incompatible API
changes (see [docs/releasing.md](https://github.com/dimchansky/fpgrowth-go/blob/main/docs/releasing.md)).

The release workflow publishes the section whose heading matches the tag
(`## [0.1.0] - <date>` for `v0.1.0`) as the release notes.

## [Unreleased]

### Added

- Tests: the test oracle (`internal/oracle`) has an independent reference
  for all 25 rule evaluation measures and `LogSupportRatio`, written from
  the definitions with exact arithmetic, and the oracle comparisons check
  every evaluation value against it, so a wrong measure formula now fails
  `go test ./...` without the C-derived fixtures (audit TST-3, #15). The
  known numerical defects for large total weights (ALG-1, ALG-2, ALG-6)
  are recorded in expected-failure tables (all three are fixed since, see
  below, and the tables are gone). No library or CLI behavior changes.
- Benchmarks: the dense maximal workload of audit finding M5A-1
  (`d20000x200d30`: 20,000 transactions, 200 independent items at 30 %)
  is a pinned dataset of the benchmark generator, and the Go-vs-C
  comparison has `m5-d20000-*` rows for it (`-tc`/`-tm` with `-n2` to
  `-n4`), so the size-limited maximal check can be measured from
  committed code (#19).
- Benchmarks: the long-and-short shapes of the closed check (`lp12500`
  to `lp100000`, `lp50000s2`, `lp50000w`, `lpc20000`) and the probe
  trap of its design review (`adv5-20000`, `adv5-40000`) are pinned
  datasets with `lp-*` and `adv5-*` rows in the Go-vs-C comparison
  (#20).

### Changed

- Closed item sets with a maximum size (`-tc -n`, `TargetClosed` with
  `MaxSize`) no longer take quadratic time when a long transaction and
  many short ones contain the same items. The complete extension check
  starts from a short cover transaction instead of the first one, and
  searches long transactions instead of merging them. One line of
  50,000 items plus 50,000 one-item lines (`-As -tc -s-2 -n1`) now
  takes 0.06 s instead of 7.0 s (C: 3.7 s), and the time grows
  linearly with the size. The output is unchanged. With `-n2` or more,
  or with `-At`, such data stays quadratic in the mining itself, not
  in the check (#20, audit NEW-3).

- Maximal item sets with a maximum size (`-tm -n`, `TargetMaximal` with
  `MaxSize`) are much faster on dense data with many maximal sets of the
  maximum size. The complete extension check re-indexes a parent's
  cover once and decides its children by population counts over the
  parent's transactions, instead of scanning each child's cover. On the
  audit's dense workload (`d20000x200d30 -tm -s2 -n3`, 1,313,400 sets)
  the run takes about 20 s instead of 97 s (C: 39 s), with the same peak
  memory. The output is unchanged. A parent's rows are built only when
  two conditions hold. First, its children's actual check work has
  reached half the build cost. Second, the children still to come
  would save at least that cost. That count comes from a bound given
  by the miner, scaled below the maximum size by how many such children
  were actually checked. Data with few checked children per parent
  therefore keeps its speed (#19, audit M5A-1).
- The values of the information (`i`) and the G-test p-value (`g`), and
  of the Fisher tests (`f h m s`) for total weights above 170, differ from
  upstream's (and v0.1.0's) in the last bits on ordinary data: they are
  computed with cancellation-free formulas (see Fixed). Chi², Yates,
  their p-values, the certainty factor and `-i` keep upstream's values
  wherever upstream's double arithmetic is exact (products up to 2^53;
  for the certainty factor, unless base/(base−head) > 2^10, where an
  exact form is used). A rule or item set whose value lies within
  those last bits of an evaluation threshold can change membership
  (`docs/differences.md` N1, F-1).
- The Fisher tests for total weights above 170 visit only the tables
  that contribute to the p-value, about 9 standard deviations of the
  hypergeometric distribution on each side of its mode, instead of
  min(body, head)+1 tables: a few hundred tables for ordinary data
  (`BenchmarkEval/FisherProb`, total 1000: 2.43 µs → 0.51 µs; dense rule
  mining with FisherProb: 20 s → 0.3 s), and at a total weight of 10^8
  with balanced margins about 0.4 ms instead of seconds. FisherProb
  returns 0 at once when an upper bound of its p-value is below the
  smallest float64 (strong associations at large totals), without
  resolving near-ties.
- Relative support and confidence thresholds (`Fraction`, `MinConfidence`,
  `-s`, `-S`, `-F`, `-c`) follow a threshold contract
  (`docs/differences.md` N2 and "Threshold contract", #17). Behavior
  change only for large weights: where x = threshold × total weight (or
  confidence × body support) is below 2^52, every decision is unchanged
  (upstream's and v0.1.0's double formula, bit for bit, for every
  fraction, percentage and `MinConfidence` value). From 2^52 on the bound
  is exact, ⌊x⌋ for a minimum or the confidence and ⌈x⌉ for a maximum,
  instead of upstream's value, which could be up to x·2^-52 more
  permissive (see Fixed); maxima switch above 2^52.

### Fixed

- The command-line program applies absolute support thresholds written
  as integers exactly (audit ALG-3, #18; `docs/differences.md` N3, a
  known limitation of v0.1.0): `-s-<n>`, `-S-<n>` and negative `-F`
  entries were parsed as doubles, as upstream does, and rounded above
  2^53 (by up to 1024 near 2^63). `-w -s-2305843009213693753` on
  `a 2305843009213693752`, `b 2305843009213694152` no longer reports `a`,
  and `-w -s-4611686018427387840` finds the item with exactly that
  support instead of "no (frequent) items found". Integer literals
  beyond 2^63−1 still saturate at 2^63−1, like every other spelling of
  such values. Other spellings (`-s-2.5`, `-s-1e3`, hexadecimal, `inf`),
  percentages, option clustering and all results for absolute
  thresholds up to 2^53 are unchanged.
- Support and confidence thresholds are exact at large total weights
  (audit ALG-2, #17; `docs/differences.md` N2, a known limitation of
  v0.1.0): above about 2^52 the relative tolerance admitted sets and rules
  up to W·2^-52 outside the bound (512 for W = 2^61), and
  `MinConfidence: 1` / `-c100` admitted rules with a confidence below 1.
  `-w -s50` on `a 2305843009213693752`, `b 2305843009213694152` no longer
  reports `a` (49.999999999999992895 %), `-tr -w -s0 -c100 -m2` on
  `x y 4611686018427387000`, `x 1000` no longer reports `y <- x`, and
  `Fraction(1)` of a total weight W is exactly W (it was W−257 for
  W = 2^60+1, not W−256 as the `Fraction` documentation said). Results
  for x below 2^52 do not change.

- The statistical evaluation measures are accurate for every total
  transaction weight up to 2^63−1 (audit ALG-1 and ALG-6, #16;
  `docs/differences.md` N1, which was a known limitation of v0.1.0):
  chi² and Yates (`n y`) within 8 ulps, the information (`i`) within 32
  ulps, the certainty factor (`z`) within 2^-40, the p-values (`p t g`)
  within 2^-36·(1+x) and the Fisher tests (`f h m s`) within 2^-36. They
  lost precision from about 10^12 and collapsed beyond (strong
  associations got p = 1, chi² = 0 or information 0), and already within
  upstream's 32-bit range chi² and Yates were off by more than 8 ulps
  (chi² 0 instead of 6.6e-17 for a total of 123456789). The G-test p-value
  of an exactly independent table is exactly 1 (it was 0.99999996), and
  it is accurate near independence (relative 2.4e-8 off at n = 65535
  before). `InvalidateBelowExpected` (`-i`) compares
  `supp·base > head·body` exactly; it could invalidate a rule whose
  products exceed 2^53. The audit's CLI reproductions now print 0.0455,
  0.0361 and 1/9 for `-ep`, `-eg`, `-ef` on `chi_big`, 1 and 25/36 for
  `-en`, `-ey` on the perfect association and 1 for `-ez` on the
  certainty case. FisherChi2 finds its mirror table exactly, and
  FisherChi2 and FisherSupport sum tiny tails directly (for totals above
  170), so they have a relative error bound. The certainty factor no
  longer gets the wrong sign near independence at large totals (it gave
  1 instead of −1.2e-19, and 0.1667 for an independent table at
  n = 2^62). FisherProb decides near-ties of far-apart tables at 320
  bits, so only probabilities equal within a relative 2^-200 count as
  ties.
- `MineItemsets`, `Itemsets`, `CollectItemsets`, `MineRules`, `Rules` and
  `CollectRules` return `ctx.Err()` when the context is canceled inside
  `OnStart` (which then returns nil) and nothing is mined: an empty
  database, only empty transactions, no frequent item, or an item
  selection that leaves no item. They returned nil before (RC-2, #14).
  An error returned by `OnStart` or by the callback still takes
  precedence over the context error and is returned unchanged; this is
  now documented.

## [0.1.0] - 2026-10-17

First public version. A pure Go port and further
development of Christian Borgelt's FP-Growth 6.21 (2022-11-22) C
implementation.

### Added

- Library package `fpgrowth` (`github.com/dimchansky/fpgrowth-go`): pure
  Go, no cgo, no dependencies outside the standard library; Go 1.26 or
  later.
  - Targets: frequent, closed and maximal item sets, generators, and
    association rules with a single-item head.
  - Generic items (any `comparable` type) and replayable sources:
    `FromItems`, `FromSlice`, `Generate`, `TableFile`, `TableReader`,
    `CSVFile`, `CSVReader`, and `SpoolTable`/`SpoolCSV` for one-shot
    readers.
  - Three delivery styles for each result kind: iterators (`Itemsets`,
    `Rules`), callbacks (`MineItemsets`, `MineRules`) and collectors
    (`CollectItemsets`, `CollectRules`); early stop and cancellation
    through `context.Context`.
  - Upstream's algorithm variants (`Algorithm`) and optimizations
    (`Tuning`), absolute and relative support thresholds (`Count`,
    `Fraction`), size limits, a per-size support border, transaction
    weights, item selection, rule item appearances, 25 rule evaluation
    measures with aggregation for item sets, `Stats`, `Summary` and the
    pattern spectrum.
  - Option validation with `ErrInvalidOptions` naming the field
    (including a non-zero `TableFormat` without record separators, a
    nil `SourceFunc`, and a maximum support below the minimum of the
    same kind; `SpoolTable`/`SpoolCSV` check the format before copying
    any input); size limits reported as `ErrTooLarge`. Callback errors
    of any type, comparable or not, are returned unchanged.
  - `Value` is NaN when no evaluation measure is set; results are
    deterministic for the same data, options and version, but their
    order is not part of the compatibility promise.
- Command-line program `cmd/fpgrowth`, compatible with upstream
  `fpgrowth` 6.21: same options, input and output formats, messages and
  exit codes, apart from the documented differences below. Standard input
  is spooled to a temporary file; SIGINT stops the run with exit status
  130.
- Development tooling in the nested module `reference/` (not part of the
  published module): the unmodified upstream sources with a SHA-256
  manifest, a scripted C reference build (release, debug, sanitizer and
  native variants), and a randomized differential test suite against the
  C program with a brute-force checker.
- Golden fixtures produced by the C reference (plus Go output snapshots
  for the documented differences), property tests against a brute-force
  oracle, and fuzz targets for the table reader, the C number
  parsers, the transaction bag, the miner, the CSV source and the CLI
  option parser.
- Benchmarks of the library and the CLI against the C reference
  ([docs/benchmarks.md](https://github.com/dimchansky/fpgrowth-go/blob/main/docs/benchmarks.md)).
- CI (tests on Linux, macOS and Windows with Go 1.26 and 1.27, race
  detector, static analysis, vulnerability scan, fuzz smoke tests,
  differential tests) and GoReleaser-based release automation.

### Differences from upstream

Every difference is intentional and listed with its kind (upstream
defect fix, platform difference, improvement, new or preserved behavior),
evidence and tests in
[docs/differences.md](https://github.com/dimchansky/fpgrowth-go/blob/main/docs/differences.md);
they fix 33 distinct, confirmed upstream defects and avoid 2 upstream
crashes. In summary:

- Fixed mining results: `-At` crashes or wrong item names (M1), `-s0` and
  zero-frequency items (M2), generators missed with `-m ≥ 2` (M3), maximal
  sets missed (M4, M9), non-closed or non-maximal sets with `-n` (M5).
  Negative weights are rejected (M6), supports are 64-bit with an
  overflow check (M7), out-of-range thresholds are clamped (M8), a sort
  recursion crash on long shared prefixes is avoided (M10), and item
  counts saturate instead of overflowing (M11).
- Rules and evaluation: `-eb` selects the imbalance ratio for rules (R1),
  `-p` prunes as documented (R2), head-only items stay out of rule bodies
  (R3), `-S` applies to the same support as `-s` (R8), and measure edge
  cases are corrected (R4-R7, R9, R11; R9c and R11 are latent; R10 fixes the project's
  patched C reference only).
- Floating point: values are bit-identical to the C reference built
  without fused multiply-add, except log-based values, which can differ in
  the last bit from the platform's C library (F-1).
- CLI output, input and messages: `-h` and `-g` work as documented, no
  spurious empty-set line, count-only mode honors `-n`, write errors are
  reported, escapes, non-ASCII separators, long fields and a last record
  without a newline are handled, NaN thresholds are rejected, log times
  are wall-clock times, a pre-formatting overflow crash is avoided
  (O1-O11, I1-I8, C1-C10).
- Numeric limits: the statistical rule measures lose precision at total
  transaction weights above about 1e12, and thresholds tolerate rounding
  above 2^52 (N1-N3).
- Upstream behavior kept on purpose is listed as P1-P6.

### Known limitations

- **Statistical rule measures at very large total weights** (audit
  finding ALG-1, `docs/differences.md` N1): chi², Yates, the G-test and
  information, the Fisher tests, the certainty factor and `-i` are
  computed in float64 as upstream does, and lose precision once the total
  transaction weight exceeds about 10^12 (about 1–40 % error from
  10^12–10^13, meaningless by 10^14–10^16). Item sets, supports,
  confidence and the other measures are not affected. Exact arithmetic
  would change the last bits of these values against the C reference on
  ordinary data, so it is deferred to a future release.

### Not included

- The `fpgpsp` program (`fpgpsp.c` is in the upstream archive, but the
  modules it needs, `tars.c` and `pspest.c`, are not) and
  zlib-compressed output (`-z`, off in upstream's default build).

[Unreleased]: https://github.com/dimchansky/fpgrowth-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/dimchansky/fpgrowth-go/releases/tag/v0.1.0
