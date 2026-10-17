# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While the major version is 0, minor versions may contain incompatible API
changes (see [docs/releasing.md](docs/releasing.md)).

The release workflow publishes the section whose heading matches the tag
(`## [0.1.0] - <date>` for `v0.1.0`) as the release notes.

## [Unreleased]

First public version, planned as v0.1.0. A pure Go port and further
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
  - Option validation with `ErrInvalidOptions` naming the field; size
    limits reported as `ErrTooLarge`.
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
- Golden fixtures produced by the C reference, property tests against a
  brute-force oracle, and fuzz targets for the table reader, the C number
  parsers, the transaction bag, the miner, the CSV source and the CLI
  option parser.
- Benchmarks of the library and the CLI against the C reference
  ([docs/benchmarks.md](docs/benchmarks.md)).
- CI (tests on Linux, macOS and Windows with Go 1.26 and 1.27, race
  detector, static analysis, vulnerability scan, fuzz smoke tests,
  differential tests) and GoReleaser-based release automation.

### Differences from upstream

Every difference is intentional, tested and listed with its evidence in
[docs/differences.md](docs/differences.md). In summary:

- Fixed mining results: `-At` crashes or wrong item names (M1), `-s0` and
  zero-frequency items (M2), generators missed with `-m ≥ 2` (M3), maximal
  sets missed (M4, M9), non-closed or non-maximal sets with `-n` (M5).
  Negative weights are rejected (M6), supports are 64-bit with an
  overflow check (M7), out-of-range thresholds are clamped (M8).
- Rules and evaluation: `-eb` selects the imbalance ratio for rules (R1),
  `-p` prunes as documented (R2), head-only items stay out of rule bodies
  (R3), `-S` applies to the same support as `-s` (R8), and measure edge
  cases are corrected (R4-R7, R9-R11).
- Floating point: values are bit-identical to the C reference built
  without fused multiply-add, except log-based values, which can differ in
  the last bit from the platform's C library (F-1).
- CLI output, input and messages: `-h` and `-g` work as documented, no
  spurious empty-set line, count-only mode honors `-n`, write errors are
  reported, escapes, non-ASCII separators, long fields and a last record
  without a newline are handled, NaN thresholds are rejected, log times
  are wall-clock times (O1-O10, I1-I8, C1-C9).

### Not included

- The `fpgpsp` program (its sources are missing from the upstream
  archive) and zlib-compressed output (`-z`).

[Unreleased]: https://github.com/dimchansky/fpgrowth-go/commits/main
