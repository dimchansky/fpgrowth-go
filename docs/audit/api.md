# Pre-v0.1.0 public API audit: github.com/dimchansky/fpgrowth-go

- Baseline: f7bb606 (clean clone at `scratchpad/audit/api`), no tracked file modified.
- Toolchains: go1.27.2 (pinned) and go1.26.9 (minimum language level check).
- Audit programs (throwaway, untracked): `_audit/api/edge_test.go`,
  `_audit/api/fisher_test.go`, `_audit/api/tablefmt_test.go`,
  `_audit/api/consumer/main.go`.
- Commands used:
  - `go test -count=1 -v ./_audit/api/` (all edge tests)
  - `go test -count=1 -race -v -run 'TestConcurrent|TestEarly|TestSpool' ./_audit/api/`
  - `go run ./_audit/api/consumer`
  - `GOTOOLCHAIN=go1.26.9 go build ./... && go vet . && go test .` -> `ok` (5.7 s)
  - `staticcheck .` -> no findings

## Overall verdict

The surface is small (one package, 46 lines of `go doc -short`), consistent and
unusually well documented; the hard runtime problems of a generic item type
(NaN, incomparable dynamic values, int64 overflow, negative weights,
non-replayable inputs) are handled with errors instead of panics, and the
streaming/cancellation contract holds under measurement. I found **one bug that
panics on legal input (API-1)** and **one silent-wrong-result footgun in a
zero-value design decision (API-2)**; both are cheap to fix and should be fixed
before tagging. Everything else is Low/Info or a documentation decision.

## Findings table

| ID | Severity | Status | Summary | Blocks v0.1.0 |
| --- | --- | --- | --- | --- |
| API-1 | High | Confirmed | Callback error of a non-comparable type panics (`err == sink.userErr()`) | Yes (1-line fix) |
| API-2 | Medium | Confirmed | Partially filled `TableFormat` silently reads the whole input as one transaction/item | Yes (decide semantics) |
| API-3 | Medium | Confirmed | `Value` is NaN without evaluation: `reflect.DeepEqual`/`==` on results is always false | No (doc), decision |
| API-4 | Low | Confirmed | `SpoolCSV` spools the whole input before rejecting an invalid `CSVFormat` | No |
| API-5 | Low | Confirmed | Invalid item in `Options.Items` matches `ErrInvalidOptions` but not `ErrInvalidItem` | No |
| API-6 | Low | Confirmed | Replay check misses passes with same count/weight/item universe: silent wrong result | No |
| API-7 | Low | Confirmed | `ItemOrder` has no `String()` (all other enums do) | No |
| API-8 | Low | Confirmed | Typed-nil `SourceFunc` passes the nil check and panics | No |
| API-9 | Low | Confirmed | `MaxSupport < MinSupport` (both `Count`) accepted, reports nothing | No |
| API-10 | Info | Confirmed | Empty non-nil `Options.Items` means "no restriction" | No (doc) |
| API-11 | Info | Confirmed | Iterator forms drop `Summary` (only `Stats` via `OnStart`) | No (additive later) |
| API-12 | Info | Confirmed | Friction: long-format (order_id,product) CSV and header lines in table format unsupported | No (additive later) |
| API-13 | Info | Confirmed | Item identity: -0.0/+0.0 merge (first spelling reported); `any(1)`/`any(int64(1))` distinct | No (doc) |
| API-14 | Info | Confirmed | Same-size, restored-mtime rewrite between passes undetected (documented limitation) | No |
| API-15 | Info | Hypothesis | `stepGuarded` re-panics with `panic(r)`, losing the original stack of internal panics | No |
| API-16 | Info | Decision | Result order is promised as "upstream's deterministic order" without saying whether it is covered by semver | No (doc) |

---

## API-1 (High, Confirmed): non-comparable callback error panics

Affected: `MineItemsets`, `MineRules` (via `mine`, fpgrowth.go:337
`case sink != nil && err == sink.userErr():`).

`==` on two interface values with the same non-comparable dynamic type panics.
Value-typed multi-errors such as `type Errors []error` or a struct with a slice
field and a value receiver are legal and common.

Evidence (`TestCallbackErrorNonComparable`, `...Rules`):

```go
type sliceErr struct{ msgs []string }
func (e sliceErr) Error() string { ... }
fp.MineItemsets(bg, src, opt, func(fp.Itemset[string]) error { return sliceErr{[]string{"boom"}} })
```
```
PANIC with non-comparable callback error: runtime error: comparing uncomparable type api.sliceErr
PANIC (rules) with non-comparable callback error: runtime error: comparing uncomparable type api.sliceErr
onstart non-comparable: boom      # OnStart path does not compare, returns fine
```

Impact: a library panic (not even the user's) on a legal callback error; the
documented contract "fn's error is returned unchanged" is broken for these types.

Resolution (minimal, not API-visible): record that the callback failed instead
of comparing, e.g. a `failed bool` set in `setSink.ItemSet`/`ruleSink.Rule` when
`fn` returns non-nil, and `case sink != nil && sink.failed(): return sum, sink.userErr()`.
Add a regression test with a slice-typed error for both item sets and rules.
Also grep the internal packages for `==` on errors that may carry the user's
error (I found only this one on the user path).

Blocks v0.1.0: yes (trivial, panic on legal input).

## API-2 (Medium, Confirmed): partial `TableFormat` silently changes record semantics

Affected: `TableFormat`, `TableFile`, `TableReader`, `SpoolTable`.

Zero `TableFormat` means the default; any other value is literal, so leaving
`RecordSeparators` empty means "no record separator". This is documented, but
the natural literal `TableFormat{FieldSeparators: ";"}` or `{Weighted: true}`
mines garbage without an error.

Evidence (`TestTableFormatFootgun`, input `"a b 2\na c 3\n"`):
```
{FieldSeparators:";"}: sets=[{[a b 2\n a c 3\n] 1 NaN}] tx=1 w=1 err=<nil>
{Weighted:true}:       err=... mem:1(1): invalid transaction weight a b 2
```
The first case returns success with a single item consisting of the whole file.

Impact: silent wrong results for a very likely usage pattern; the semantics are
part of the zero-value contract and expensive to change after v0.1.0.

Resolution (pick one before tagging):
1. Minimal: validate like `CSVFormat.validate` and reject a non-zero format with
   empty `RecordSeparators` (and arguably empty `FieldSeparators`) with
   `ErrInvalidOptions` naming the field. Upstream cannot express "no record
   separator" usefully either.
2. Alternative: "empty class means default" plus explicit disable flags
   (`NoComments bool`, etc.). More ergonomic, but a larger design change.
I recommend (1) now; it can be relaxed later without breaking anyone.

Blocks v0.1.0: yes (decision on zero-value semantics).

## API-3 (Medium, Confirmed): NaN `Value` makes results non-equal to themselves

Affected: `Itemset.Value`, `Rule.Value`; consumers of `CollectItemsets`/`CollectRules`.

Evidence (`TestYieldedSliceReuse`):
```
DeepEqual of two identical CollectItemsets runs: false (Value=NaN)
```
Impact: downstream tests that compare collected results with
`reflect.DeepEqual`, `testify.Equal`, or `==` on `Rule`/`Itemset` fail
mysteriously. Changing the sentinel later (0 + `HasValue`, or a pointer) is a
semantic break.

Resolution: decide now. Either keep NaN (upstream-faithful, arguably correct)
and document it at `Itemset.Value`/`Rule.Value` and in README "Testing"
(compare with `cmpopts.EquateNaNs()` or compare fields), or switch to a
non-NaN representation before v0.1.0. I recommend keeping NaN plus the doc note.

Blocks v0.1.0: no (doc only), but the decision is expensive later.

## API-4 (Low, Confirmed): `SpoolCSV` validates the format only at mining time

Affected: `SpoolCSV`. Evidence (`TestSpool`):
```
spool invalid CSV format: err=<nil> (cleanup nil=false)
CSVFile invalid comma error: fpgrowth: reading transactions (pass 1): fpgrowth: invalid options: CSVFormat.Comma: invalid delimiter '\n'
```
Impact: a potentially large stdin stream is copied to disk before an error that
was knowable up front. Resolution: call `f.validate()` at the top of `SpoolCSV`
(and the API-2 validation in `SpoolTable`). Not blocking.

## API-5 (Low, Confirmed): sentinel inconsistency for invalid option items

Affected: `Options.Items` (`checkItems`), `ErrInvalidItem` doc.
```
NaN in tx:            ... is ErrInvalidItem=true
NaN in Options.Items: fpgrowth: invalid options: Items: item 0: ... isInvalidOptions=true isInvalidItem=false
```
Resolution: return a `detailError` wrapping both `ErrInvalidOptions` and
`ErrInvalidItem`. Cheap and only widens `errors.Is`. Not blocking (but easier
before users write `errors.Is` code against it).

## API-6 (Low, Confirmed): undetected non-replayable sources give silent wrong results

Affected: `Source` contract, `ErrNotReplayable`.
Pass 1 `{a,b}x3,{c}`, pass 2 `{c}x3,{a,b}` (same count, weight, items), MinSupport 3:
```
swapped pass2: sets=[] minsupp=3 frequent=2 err=<nil>
```
Pass 1 says `{a,b}` has support 3; the run reports nothing and succeeds.
Count change and unknown items are detected (`ErrNotReplayable` with good
messages). Documented as a contract violation. Optional hardening: compare
per-item weight sums of pass 2 with pass 1 (one add per occurrence, already
touched). Not blocking.

## API-7 (Low, Confirmed): `ItemOrder` lacks `String()`

`Target`, `Algorithm`, `Appearance`, `Aggregation`, `Measure` have `String()`;
`ItemOrder` does not (`grep "func (o ItemOrder) String"` empty). `%v` prints a
bare int. Additive; fix whenever.

## API-8 (Low, Confirmed): typed-nil `SourceFunc` panics

`var sf fp.SourceFunc[string]; fp.MineItemsets(bg, sf, ...)` ->
`runtime error: invalid memory address or nil pointer dereference`
(the `src == nil` check misses the typed nil). `nil` ctx also panics, which is
standard Go convention. Resolution: optional `if f, ok := src.(SourceFunc[T]); ok && f == nil`
check returning `ErrInvalidOptions`, or a doc sentence. Not blocking.

## API-9 (Low, Confirmed): inverted support range accepted

`Options{MinSupport: Count(5), MaxSupport: Count(2)}` -> `err=<nil>`, nothing
reported. With `Fraction` it can only be resolved after pass 1, so full
validation is impossible, but `Count/Count` (and `Fraction/Fraction`) pairs
can be rejected at plan time. Not blocking.

## API-10 (Info): empty non-nil `Options.Items` means "no restriction"

`len(opt.Items) > 0` decides; a caller who computes an empty allow-list mines
everything instead of nothing. Distinguishing nil from empty is un-idiomatic,
so I recommend keeping it and adding "nil or empty means all items" to the
field doc.

## API-11 (Info): iterators do not expose `Summary`

`Itemsets`/`Rules` discard the `Summary` (Reported, BySize, Spectrum); only
`Stats` is reachable through `OnStart`. Fine for v0.1.0; can be added later
additively (e.g. a callback option or a variant returning a summary func).

## API-12 (Info): downstream friction (consumer program)

`_audit/api/consumer/main.go` (market basket with rules, sorted by lift) works
and reads well: `Fraction`, `MinConfidence`, `Rule.Lift`, `Clone`, `OnStart` are
straightforward. Friction observed:
- The most common real export shape, long-format `order_id,product` CSV, is not
  supported; the consumer writes a grouping `SourceFunc` (20 lines) and must
  hold or pre-sort the data.
- `TableFormat` has no header skip: a `basket,weight` header line fails with
  `...:1(2): invalid transaction weight weight`.
- `RuleOptions[string]{...}` requires the type argument even when no field uses
  T (inherent to `Items`/`Appearance` living in the options; acceptable).
- Rule `MinSupport` applies to the body support by default (upstream); users
  coming from arules/mlxtend expect body+head. Documented in README and field
  doc; keep for fidelity.
- Bodies come in report order; display code sorts them.
All are additive later (e.g. a `GroupedCSV` source, `TableFormat.HasHeader`).

## API-13 (Info): item identity rules

- `-0.0` and `+0.0` are one item (map key equality); the first spelling seen is
  reported: `items=[-0] signbit=true support=2`.
- `any(1)`, `any(int64(1))`, `any("1")` are three items; `nil` is a valid `any` item.
Worth one sentence in the "Data" section of the package doc.

## API-14 (Info): metadata replay check limits

Rewriting a file with the same size and restoring its mtime between passes is
not detected (`same size+mtime rewrite: ... err=<nil>`); a size change is
(`ErrNotReplayable` with path, sizes, times). This is exactly what the docs say.

## API-15 (Info, Hypothesis): stack loss in `stepGuarded`

`stepGuarded` recovers every panic during a step and re-raises unexplained ones
with `panic(r)`, which replaces the original stack trace. Only library code runs
inside the guarded region (user Source code is outside it), so this affects
debuggability of internal bugs only. Optional: wrap with the captured
`debug.Stack()` or narrow the recover.

## API-16 (Info): output order and semver

Docs promise "upstream's deterministic order". Determinism is confirmed (20
runs, 1280 rules identical; `Appearance` map order does not leak). Option
order matters: `Items: [1 2 3]` vs `[3 2 1]` with `ItemOrderNone` give
mirrored output. Recommend stating explicitly whether result order is part of
the compatibility promise (I suggest: deterministic for a given version,
inputs and options; may change between minor versions; sets and supports never).

---

## Verified as correct (no finding)

| Area | Evidence |
| --- | --- |
| Empty DB, empty transactions, all-zero weights | no error, `OnStart` called once, `FrequentItems=0` (`TestEmptyDatabase`) |
| NaN items (float, struct field) | `ErrInvalidItem` naming transaction and pass |
| Incomparable dynamic values (`[]int`, `map`, struct{any}, error value) incl. rules hook and `AppearNone` path | `ErrInvalidItem`, no panic |
| int64 overflow of total weight | `ErrWeightOverflow: at transaction 2`; `MaxInt64` total and `Count(MaxInt64)` exact |
| Negative weights (slice, CSV) | `ErrNegativeWeight` with position |
| Threshold boundaries | `Fraction(0.3)` of 10 = 3, `Fraction(0.3000001)` = 4, `Fraction(1e-300)` = 1, `Count(0)` acts as 1; max bounds inclusive; invalid fractions (0, <0, >1, NaN, Inf) rejected |
| Input slices not mutated (all algorithms, items and rules) | `tx mutated=false items mutated=false` |
| Yielded slices | reused as documented (`kept (no clone)` corrupted, clones correct); capped, so `append` by the user does not corrupt the buffer |
| Callback error identity | `err == sentinel` true, `Reported` includes the stopping result |
| Error wrapping | `errors.Is(err, os.ErrNotExist)` through "cannot open file"; source errors wrapped with pass; `ErrSyntax` with upstream positions |
| Early break / errors / cancel in pass 1 | files opened=10 closed=10; goroutines 2 before, 2 after (no goroutines used) |
| Concurrency | 24 goroutines sharing `TableFile`, `CSVFile`, `FromItems` sources and one `Options` value (Border, Items, Evaluation with `PruneSize`), `-race` clean, consistent counts |
| Cancellation latency (cancel at 200 ms) | dense itemsets 111 us, count-only 39 us, closed 96 us, rules 11 us, pass 1 over 20M generated tx 109 us, Fisher (W=1e8) 187 us |
| OnStart | called with no frequent items; error returned unchanged with no callback after it; re-iterating an `Itemsets` sequence re-mines |
| Spool | temp file mode 0600, cleanup idempotent, canceled ctx leaves no file and nil cleanup |
| Go version policy | go 1.26.9: build, vet, tests pass; `toolchain` line does not affect importers |

## Decisions expensive to change after v0.1.0

| # | Decision | Recommendation |
| --- | --- | --- |
| 1 | `Source[T]` = one method `Transactions() iter.Seq2[Transaction[T], error]`, no ctx, no Close | Keep. It is minimal and composes. Document the pattern "capture ctx in the closure" for network sources; the miner already checks ctx every 1024 transactions. Adding a method later would break every implementer, so do not plan on it. |
| 2 | `TableReader/CSVReader(open func() (io.ReadCloser, error), name, f)` | Keep, consistent with (1). |
| 3 | Replay check via unexported `runSource` | Keep unexported; exporting a hook later is additive. |
| 4 | Generic `Options[T]`/`RuleOptions[T]` (forces type argument) | Keep; the alternatives (`[]any`, separate option types) are worse. |
| 5 | `TableFormat` zero = default, otherwise literal | Change now (API-2): validate at least `RecordSeparators`. |
| 6 | `CSVFormat` zero value usable, `WeightColumn` 1-based with negatives from the end | Keep. |
| 7 | `Transaction.Weight` zero value = weight 0 (contributes nothing) | Keep (upstream allows 0; documented in type and README). Optionally note in `FromSlice` doc. |
| 8 | `Support` opaque value from `Count`/`Fraction`, zero = unset | Keep; good design. Fraction tolerance documented (W-2047 at W = MaxInt64). |
| 9 | `Value` NaN when no evaluation | Keep with documentation (API-3), or change now; not later. |
| 10 | Result slices reused, valid only during the callback/step, `Clone` methods | Keep; already capped against append aliasing. |
| 11 | Callback error returned unchanged; nil fn = count only | Keep; fix API-1. |
| 12 | Three delivery styles (callback core, `iter.Seq2` with trailing `(zero, err)`, collect) | Keep; idiomatic. Iterators can gain a summary later additively (API-11). |
| 13 | Sentinel errors + multi-unwrap `detailError`; message formats | Keep. Treat message texts as non-contractual (say so in `errors.go`); sentinels are the contract. A positional error type for table input can be added later. |
| 14 | `int64` input counts/supports, `uint64` result counts | Keep. |
| 15 | Enum numbering (`Target`, `Algorithm`, `ItemOrder`, `Appearance`, `Aggregation`, `Measure` equal internal codes, zero `Measure` invalid) | Keep; declare constants append-only. Add `ItemOrder.String` (API-7). |
| 16 | `Evaluation` by pointer, `PruneSize *int` | Acceptable. `*int` is slightly awkward (`new(int)` / `&k`); if anything changes, do it now (e.g. `Prune bool` + `PruneSize int`). Low priority. |
| 17 | Rule `MinSupport` = body support unless `OriginalSupport` | Keep for upstream fidelity; it is documented on the field and in README. |
| 18 | Empty `Options.Items` = no restriction | Keep; document "nil or empty" (API-10). |
| 19 | Result order "upstream's deterministic order" | Clarify that order is not part of the semver promise (API-16). |
| 20 | `go 1.26.0` minimum, no generic methods | Keep (verified with go1.26.9). |
| 21 | Single root package; internals under `internal/`; CLI on `internal/fpg` (D5) | Keep. No internal types leak into the exported API. |

## Minimal pre-release change list

1. API-1: replace `err == sink.userErr()` with a failure flag; regression test.
2. API-2: reject non-zero `TableFormat` with empty `RecordSeparators` (and decide
   on empty `FieldSeparators`) with `ErrInvalidOptions`; validate in
   `SpoolTable`/`SpoolCSV` before copying (API-4).
3. Docs: NaN `Value` and equality (API-3), "nil or empty" `Items` (API-10),
   item identity notes (API-13), result-order compatibility (API-16).
4. Optional, cheap: API-5 dual sentinel, API-7 `ItemOrder.String`, API-8 typed-nil
   check, API-9 inverted-range check.
