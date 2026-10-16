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
func CSVFile(path string, f CSVFormat) Source[string]          // encoding/csv, reopened per pass
func Spool(r io.Reader, dir string, read func(io.Reader) Source) ... // see below
```

- **Errors.** A pass yields `(zero, err)` and stops at the first error;
  mining returns that error wrapped with context.
- **Replay check.** Mining verifies that the second pass is consistent
  with the first (record count, total weight) and fails with
  `ErrNotReplayable` otherwise.
- **Non-replayable input** (`io.Reader`, stdin, network). Rejected by the
  typed constructors. `SpoolTable`/`SpoolCSV` copy such input to a
  temporary file once and return a `Source` plus a cleanup function; the
  caller decides that disk use is acceptable. Nothing is ever silently
  buffered in memory.
- **Upstream table format.** `TableFormat` mirrors `-r -f -b -C -w`
  (record/field separators, blanks, comment characters, weight in the
  last field). `DefaultTableFormat()` gives upstream's defaults.
- **CSV.** RFC 4180 via `encoding/csv`: one record = one transaction;
  options `Comma`, `Comment`, `HasHeader` (skip first record),
  `WeightColumn` (index or -1), `TrimSpace`, `SkipEmptyFields` (default
  true: empty fields are not items). Distinct from the flexible table
  format on purpose.

## Mining item sets

```go
type Target int // Frequent (default), Closed, Maximal, Generators

type Support struct{ /* unexported */ }
func Count(n int64) Support        // absolute weighted support
func Fraction(f float64) Support   // of the total transaction weight, (0, 1]

type Options[T comparable] struct {
    Target       Target
    MinSupport   Support   // required (zero value is an error)
    MaxSupport   Support   // zero value: no limit
    MinSize      int       // default 1; use IncludeEmpty for the empty set
    MaxSize      int       // 0: no limit
    IncludeEmpty bool
    Border       []Support // per-size minimum support from MinSize (upstream -F)
    Items        []T       // restrict mining to these items (upstream -R selection)
    Algorithm    Algorithm // Auto (=Complex, upstream default), Simple, Complex, SingleTree, TopDown
    Tuning       Tuning    // optimizations, all on by default (see below)
    Evaluation   *Evaluation // optional measure filter (upstream -e/-a/-d/-i/-p)
}

type Itemset[T comparable] struct {
    Items   []T     // valid only during the callback / iteration step
    Support int64
    Value   float64 // evaluation value (NaN if no evaluation)
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
- `Summary` has `Transactions`, `TotalWeight`, `Items` (frequent items),
  `Reported`, `BySize []uint64`, and an optional pattern spectrum.
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
    Threshold   float64     // fraction, e.g. 0.1 (upstream -d percent / 100)
    InvalidateBelowExpected bool
    PruneSize   *int        // upstream -p; nil: no pruning
}
```

## Concurrency, ownership, determinism

- All functions are safe for concurrent use; each call has its own state.
  A `Source` used by concurrent calls must support concurrent passes
  (the built-in ones do).
- Result slices are owned by the library and reused; use `Clone`.
- Output order is deterministic for a given source, options and variant
  (it is upstream's order). Different algorithms may order differently;
  sets and supports are identical.
- Memory: item dictionary + prefix-tree bag + FP-trees (+ repository for
  closed/maximal, item set tree for rules and rule-based evaluation).
  Proportional to the data's distinct structure, not constant.

## Errors

Sentinels: `ErrInvalidOptions` (wrapped with the field name),
`ErrNotReplayable`, `ErrNegativeWeight`, `ErrWeightOverflow`,
`ErrTooLarge`. Context cancellation returns `ctx.Err()`. A database without frequent
items is not an error: nothing is reported and `Summary.Items` is 0 (the
CLI keeps upstream's "no (frequent) items found" error).

## Go version

Language level stays at 1.26 (D3): generic functions suffice; Go 1.27
generic methods are not needed for this surface.

## Implementation notes (deviations from this design, accepted)

1. `Fraction(f)` is converted after pass 1 with upstream's formula applied
   to `f` directly (`ceil(f·W·(1−ε))`, `floor(f·W·(1+ε))`) and passed as an
   absolute count, because `100·f/100 ≠ f` for ~15 % of fractions.
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
5. Settings that would silently have no effect are rejected
   (`ErrInvalidOptions`).
6. Additions: `ErrSyntax`, `Options.PatternSpectrum`, nil callback =
   count only, `Rule.Value` is NaN without evaluation, `Rule.Support` is
   always the support of body ∪ head (thresholds follow
   `OriginalSupport`).
