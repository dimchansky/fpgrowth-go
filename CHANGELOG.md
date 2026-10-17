# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While the major version is 0, minor versions may contain incompatible API
changes (see [docs/releasing.md](https://github.com/dimchansky/fpgrowth-go/blob/main/docs/releasing.md)).

The release workflow publishes the section whose heading matches the tag
(`## [0.1.0] - <date>` for `v0.1.0`) as the release notes.

## [Unreleased]

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
  is not used yet because it would change the last bits of these values
  against the C reference on ordinary data; whether to implement it is
  decided before the final release.

### Not included

- The `fpgpsp` program (`fpgpsp.c` is in the upstream archive, but the
  modules it needs, `tars.c` and `pspest.c`, are not) and
  zlib-compressed output (`-z`, off in upstream's default build).

[Unreleased]: https://github.com/dimchansky/fpgrowth-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/dimchansky/fpgrowth-go/releases/tag/v0.1.0
