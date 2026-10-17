# Public API design (Stage 2)

Status: design approved by the orchestrator; implementation in the root
package `fpgrowth`. The internal packages stay as they are; the root
package is a thin, typed layer over `internal/fpg`.

## Goals

- Mine any `comparable` item type (strings, ints, custom IDs) without the
  caller recoding items.
- Never require the whole database in memory: transactions come from a
  *replayable source*, read once per pass (FP-growth needs two).
- Stream results: item sets and rules are delivered one at a time, the
  caller can stop early, cancellation works, nothing is accumulated.
- Small surface, explicit configuration, zero-value-safe defaults, errors
  instead of panics.

## Transactions and sources

```go
type Transaction[T comparable] struct {
    Items  []T   // duplicates ignored; order irrelevant
    Weight int64 // multiplicity, >= 0; zero contributes nothing
}

// A Source provides a transaction database that can be read several times.
// Every call to Transactions starts an independent pass that must yield
// the same transactions in the same order. Items slices yielded by a pass
// are only valid until the next iteration step.
type Source[T comparable] interface {
    Transactions() iter.Seq2[Transaction[T], error]
}

type SourceFunc[T comparable] func() iter.Seq2[Transaction[T], error]

func FromSlice[T comparable](txs []Transaction[T]) Source[T]  // in memory
func FromItems[T comparable](txs [][]T) Source[T]             // weight 1 each
func Generate[T comparable](n int, gen func(i int, buf []T) Transaction[T]) Source[T]
func TableFile(path string, f TableFormat) Source[string]      // upstream format, reopened per pass
func TableReader(open func() (io.ReadCloser, error), name string, f TableFormat) Source[string]
func CSVFile(path string, f CSVFormat) Source[string]          // encoding/csv, reopened per pass
func CSVReader(open func() (io.ReadCloser, error), name string, f CSVFormat) Source[string]
func SpoolTable(ctx context.Context, r io.Reader, dir string, f TableFormat) (Source[string], func() error, error)
func SpoolCSV(ctx context.Context, r io.Reader, dir string, f CSVFormat) (Source[string], func() error, error)
```

- **Errors.** A pass yields `(zero, err)` and stops at the first error;
  mining returns that error wrapped with context.
- **Replay check.** Mining verifies that the second pass is consistent
  with the first (record count, total weight, no unknown items) and fails
  with `ErrNotReplayable` otherwise. File sources (and reader sources whose
  reader has a `Stat` method) also compare size and modification time at
  the start of every pass with the first pass of the same mining run.
- **Reopenable inputs.** `TableReader`/`CSVReader` take an `open`
  function called once per pass (gzip, `fs.FS`, object storage, `[]byte`);
  `TableFile`/`CSVFile` are these with `os.Open`. Open failures are
  `cannot open file <name>: ...` wrapping the cause (`*fs.PathError`),
  read failures `read error on file <name>: ...`.
- **Non-replayable input** (`io.Reader`, stdin, network). Rejected by the
  typed constructors. `SpoolTable`/`SpoolCSV` copy such input to a
  temporary file once and return a `Source` plus a cleanup function; the
  caller decides that disk use is acceptable. Nothing is ever silently
  buffered in memory. The copy checks `ctx` between 64 KiB chunks
  (returning `ctx.Err()`); a nil reader is `ErrInvalidOptions`.
- **Upstream table format.** `TableFormat` mirrors `-r -f -b -C -w`
  (record/field separators, blanks, comment characters, weight in the
  last field). `DefaultTableFormat()` gives upstream's defaults; the zero
  `TableFormat` means the same, any other value is literal. A non-zero
  format with empty `RecordSeparators` is rejected (`ErrInvalidOptions`
  naming `TableFormat.RecordSeparators`) instead of reading the whole
  input as one transaction; `SpoolTable` and `SpoolCSV` validate their
  format before copying anything.
- **CSV.** RFC 4180 via `encoding/csv`: one record = one transaction;
  options `Comma`, `Comment`, `HasHeader` (skip first record),
  `WeightColumn` (1-based; 0 = none, negative counts from the end),
  `TrimSpace`, `KeepEmptyFields` (default false: empty fields are not
  items), `LazyQuotes`. Distinct from the flexible table format on
  purpose.

## Mining item sets

```go
type Target int // TargetFrequent (default), TargetClosed, TargetMaximal, TargetGenerators

type Support struct{ /* unexported */ }
func Count(n int64) Support        // absolute weighted support
func Fraction(f float64) Support   // of the total transaction weight, (0, 1]

type Options[T comparable] struct {
    Target       Target
    MinSupport   Support   // required (zero value is an error)
    MaxSupport   Support   // zero value: no limit; not below MinSupport if of the same kind
    MinSize      int       // default 1; use IncludeEmpty for the empty set
    MaxSize      int       // 0: no limit
    IncludeEmpty bool
    Border       []Support // per-size minimum support from MinSize (upstream -F)
    Items        []T       // restrict mining to these items (upstream -R selection);
                           // nil or empty: no restriction
    Algorithm    Algorithm // AlgorithmAuto (=AlgorithmComplex, upstream default), AlgorithmSimple,
                           // AlgorithmComplex, AlgorithmSingleTree, AlgorithmTopDown
    Tuning       Tuning    // optimizations, all on by default (see below)
    Evaluation   *Evaluation // optional measure, values and filter (upstream -e/-a/-d/-i/-p)
    PatternSpectrum bool
    OnStart      func(Stats) error // called once before the first result
}

type Stats struct {
    Transactions, TotalWeight, FrequentItems int64
    MinSupport, MaxSupport                   int64 // resolved absolute thresholds
}
type Summary struct {
    Stats
    Reported uint64
    BySize   []uint64
    Spectrum []SpectrumCell
}

type Itemset[T comparable] struct {
    Items   []T     // valid only during the callback / iteration step
    Support int64
    Value   float64 // evaluation value (NaN if no evaluation, see below)
}
func (s Itemset[T]) Clone() Itemset[T]

func Itemsets[T comparable](ctx context.Context, src Source[T], opt Options[T]) iter.Seq2[Itemset[T], error]
func MineItemsets[T comparable](ctx context.Context, src Source[T], opt Options[T],
    fn func(Itemset[T]) error) (Summary, error)
func CollectItemsets[T comparable](ctx context.Context, src Source[T], opt Options[T]) ([]Itemset[T], Summary, error)
```

- `MineItemsets` is the core (callback; returns statistics). A non-nil
  error from `fn` stops mining and is returned unchanged (also
  `ErrStop`-style early exit). `Itemsets` adapts it to an iterator:
  breaking out of the loop stops mining. Mining runs synchronously on the
  caller's goroutine — no goroutines, nothing to leak.
- `OnStart` receives the `Stats` once before the first result (also when
  nothing is reported): the total weight makes relative supports
  streamable. Its error stops mining and is returned unchanged.
- `Summary` embeds `Stats` and adds `Reported`, `BySize []uint64` and an
  optional pattern spectrum. Counts describing the input are `int64` (like
  weights and supports), counts of results `uint64` (like upstream's
  reporter counters).
- Cancellation is checked between transactions, at every recursion step,
  every 1024 reporter steps (bounding perfect-extension hypercubes and
  k-items machine bursts), every 256 nodes or counters of the item set
  tree and every 4096 tables of a Fisher test; up to about 1024 results
  may still be delivered after the context is canceled, and a canceled
  run always returns `ctx.Err()`.
- `Tuning`: `NoPerfectExtensions`, `NoReorder`, `NoHeadUnionTail`,
  `ItemMachine int` (k-items machine size, 0 = upstream default 16,
  negative = off), `ItemOrder` (upstream `-q`). Zero value = upstream
  defaults.

## Association rules

```go
type RuleOptions[T comparable] struct {
    MinSupport, MaxSupport Support
    MinConfidence   float64 // in [0, 1]; zero is valid and means no confidence filter
    MinSize, MaxSize int    // body + head
    OriginalSupport bool    // support = body ∪ head (upstream -o)
    Appearance      map[T]Appearance; DefaultAppearance Appearance
    Evaluation      *Evaluation
    Algorithm       Algorithm; Tuning Tuning
    OnStart         func(Stats) error
}
type Rule[T comparable] struct {
    Body []T; Head T
    Support, BodySupport, HeadSupport int64 // support of body ∪ head, body, head
    Confidence, Lift, Value float64
}
func Rules / MineRules / CollectRules  // same shapes as for item sets
```

`MinConfidence` zero means 0 (no confidence filter); documented, no
hidden 80 % default. Support of a rule follows upstream: body support by
default, body ∪ head with `OriginalSupport`.

## Evaluation

```go
type Measure int // public names for the 25 rule measures + LogSupportRatio
type Evaluation struct {
    Measure     Measure
    Aggregate   Aggregation // First, Min, Max, Average (item sets only)
    Filter      bool        // filter by Threshold; false: values only
    Threshold   float64     // fraction, e.g. 0.1 (upstream -d percent / 100); requires Filter
    InvalidateBelowExpected bool
    PruneSize   *int        // upstream -p; nil: no pruning; requires Filter
}
```

Without `Filter` the measure is computed for every result (`Value`) and
the results are those without an evaluation (internally: the item set
tree's and the reporter's threshold tests are disabled). A non-zero
`Threshold` or a `PruneSize` without `Filter` is rejected, as it would
have no effect.

## Concurrency, ownership, determinism

- All functions are safe for concurrent use; each call has its own state.
  A `Source` used by concurrent calls must support concurrent passes
  (the built-in ones do).
- Result slices are owned by the library and reused; use `Clone`.
- Output order is deterministic for a given source, options and variant
  (it is upstream's order). Different algorithms may order differently;
  sets and supports are identical. The order is stable for a given
  version, data and options, but it is not covered by the semver API
  promise (`docs/releasing.md`) and may change between versions; the
  sets, rules and supports are covered.
- `Value` stays NaN without an evaluation (upstream-faithful; no value
  exists). Since NaN != NaN, `reflect.DeepEqual` and `==` report equal
  results as different; compare items and supports, or treat NaNs as
  equal (for example `cmpopts.EquateNaNs()` of go-cmp).
- Item identity is map key equality: `-0.0` and `+0.0` are one item (the
  spelling read first is reported); `any(1)`, `any(int64(1))` and
  `any("1")` are three items; `nil` is a valid `any` item.
- Validation: an empty non-nil `Options.Items` means no restriction, like
  nil. An invalid item in `Options.Items` matches both
  `ErrInvalidOptions` and `ErrInvalidItem`. `MaxSupport` below
  `MinSupport` is rejected when both are `Count` (a minimum below 1
  counts as 1) or both `Fraction` and the range is empty for every total
  weight after the tolerant conversion (a few ulps apart is accepted);
  a `Count`/`Fraction` mix is resolved only after pass 1 and not
  checked. A nil `Source`, also a nil
  `SourceFunc`, is `ErrInvalidOptions`. A callback error is returned
  unchanged whatever its dynamic type (it is never compared, so
  non-comparable error types are fine).
- Memory: item dictionary + prefix-tree bag + FP-trees (+ repository for
  closed/maximal, item set tree for rules and rule-based evaluation).
  Proportional to the data's distinct structure, not constant.

## Errors

Sentinels: `ErrInvalidOptions` (wrapped with the field name),
`ErrNotReplayable`, `ErrNegativeWeight`, `ErrWeightOverflow`,
`ErrTooLarge`, `ErrSyntax`, `ErrInvalidItem` (an item whose dynamic type
is not comparable, or that is not equal to itself such as NaN; checked
only for item types that can hold interfaces or floats, once per
distinct item when it is first read; items ignored by `Options.Items` or
`RuleOptions.DefaultAppearance` are checked at every occurrence). Context
cancellation returns `ctx.Err()`. A database without frequent items is
not an error: nothing is reported and `Summary.FrequentItems` is 0 (the
CLI keeps upstream's "no (frequent) items found" error).

## Go version

Language level stays at 1.26 (D3): generic functions suffice; Go 1.27
generic methods are not needed for this surface.

## Implementation notes (deviations from this design, accepted)

1. `Fraction(f)` is converted after pass 1 with upstream's formula applied
   to `f` directly (`ceil(f·W·(1−ε))`, `floor(f·W·(1+ε))`) and passed as an
   absolute count, because `100·f/100 ≠ f` for ~15 % of fractions. All
   absolute thresholds reach the miner as exact integers
   (`fpg.Config.HasCounts`), never through the CLI's float notation.
   `Fraction(K/100)` equals CLI `-sK` exactly. Confidence and evaluation
   thresholds use a percentage `p` with `p/100 == f` when one exists,
   otherwise the neighbor on the permissive side.
2. `CSVFormat` zero value is usable: `WeightColumn` is 1-based (0 = none,
   negative counts from the end), `KeepEmptyFields` (default false),
   `LazyQuotes`, a UTF-8 BOM is skipped, blank lines are not transactions.
3. Appearance maps are registered in first-occurrence order (map iteration
   order cannot leak into output); tie order may differ from the CLI with
   an appearance file, results do not.
4. Names: `Aggregate*`, `RuleSupport`, `Information`,
   `ConditionalProbabilityRatio`; the zero `Measure` is invalid.
5. Evaluation settings that would silently have no effect are rejected
   (`ErrInvalidOptions`): `Aggregate`/`PruneSize` for rules,
   `LogSupportRatio` with `-a/-i/-p` or for rules, `ItemMachine > 16`,
   `IncludeEmpty` with `MinSize > 1`. Tuning flags are performance hints
   and, like upstream, are accepted where they do not apply (e.g. any
   `Tuning` for rules, `NoHeadUnionTail` for non-maximal targets,
   `ItemMachine` with `TopDown`).
6. Additions: `ErrSyntax`, `Options.PatternSpectrum`, nil callback =
   count only, `Rule.Value` is NaN without evaluation, `Rule.Support` is
   always the support of body ∪ head (thresholds follow
   `OriginalSupport`).
