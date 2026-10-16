# Stage 2 review — iteration 1

- **Stage / iteration:** Stage 2 (idiomatic API, sources, streaming), iteration 1
- **Critic:** independent reviewer agent (senior Go library architect, API
  design and maintainability/testing expert); not involved in writing the
  code; no repository files modified.
- **Revision:** `47a085a`, Go 1.27.2 darwin/arm64.

## Evidence gathered by the critic

| Check | Result |
| --- | --- |
| gofmt, vet, staticcheck | clean (2× ST1023 style hints in `cmd/fpgrowth/run.go` with `-checks all`) |
| `go test -race ./...` | 11 packages ok; total coverage 97.5 %, root 94.0 % |
| difftest default and new seed 20261016 (20 000 + 500 large) | 0 failures |
| Fuzz `FuzzCSVFile` (732 k execs), `FuzzMine` (64 k) | pass |
| Own brute force via public API (126 000 cases: all targets × 5 algorithms × random tuning, rules) | 0 failures |
| Public API vs Go CLI incl. result order (1 500 cases) | 1 457 identical; 43 = documented rejection of `MaxSize < MinSize` |
| README snippets and CLI examples | outputs match |
| Leak checks (iterator break, cancellation, spooling, 8 concurrent miners, `-race`) | no goroutine/fd/temp-file leaks |
| 88 MB inputs (table, CSV, spooled stdin) | 1.1–1.2 s, RSS 54–57 MB |

## Findings

| ID | Severity | Title |
| --- | --- | --- |
| H1 | **High** | Cancellation ignored in the item-set-tree phases (rules, item sets with a rule measure): after cancel at rule 1 000 all 46 M remaining rules were delivered and `MineRules` returned `nil` (`internal/fpg/tree.go` `mineTree`: `Filter`/`CloMax`/`Report` take no context) |
| M1 | Medium | Absolute thresholds above 2^53 inexact (round-trip through float `-s`/`-S`); inclusive bounds silently exclude equal supports |
| M2 | Medium | Streaming consumers cannot get total weight / statistics / resolved thresholds before results (relative support not streamable) |
| M3 | Medium | No table/CSV source over a replayable opener (gzip, object storage, `fs.FS`, `[]byte`) |
| M4 | Medium | Zero `Evaluation.Threshold` silently filters everything for p-value measures; no documented way to get values without filtering |
| L1 | Low | File sources don't detect a file changed between passes |
| L2 | Low | Memory notes qualitative only; large-file test samples heap only during passes |
| L3 | Low | Zero `TableFormat` treats the whole file as one item |
| L4 | Low | Docs say empty lines are empty transactions; with `Weighted` they are syntax errors (as upstream) |
| L5 | Low | Library-level differences missing from `docs/differences.md` |
| L6 | Low | Naming/type inconsistencies (unprefixed `Algorithm`/`Target` constants, `Summary.Items` count vs `Itemset.Items` slice, mixed int/uint64) |
| L7 | Low | Incomparable interface items panic; NaN float items give misleading `ErrNotReplayable` |
| L8 | Low | Spooling not cancelable; nil reader error not `ErrInvalidOptions`; inconsistent open-error style |
| L9 | Low (observation) | CLI does not use the public API (two front ends over `internal/fpg`) |
| L10 | Low (observation) | No public-API fuzz target; no rule-cancellation test; some results delivered shortly after cancel (undocumented) |

Verified sound: correctness vs brute force, CLI result order, early stop,
iterator reuse, no leaks, concurrency, error propagation (no skipped
transactions), non-replayable detection, overflow/negative-weight
sentinels, capacity-clipped result slices, CSV quirks, constant-memory
spooling, complete GoDoc.

## Decision

**REJECT pending fixes**: H1 must be fixed with tests; M1–M4 fixed or
explicitly accepted with documentation; Low findings as a polish batch.
A full Stage 2 re-review follows the fixes.

## Orchestrator decisions for the fixes

- H1: thread the non-blocking abort check through `istree` `Filter`,
  `CloMax`, `Report`; return `ctx.Err()`; tests in fill and report phases.
- M1: pass absolute integer thresholds directly (no float round-trip).
- M2: `Options.OnStart`/`RuleOptions.OnStart func(Stats) error` called
  once before the first result with transactions, total weight, frequent
  items and resolved absolute thresholds; also included in `Summary`.
- M3: `TableReader`/`CSVReader` over `func() (io.ReadCloser, error)`;
  `*File` constructors wrap them.
- M4: evaluation filtering becomes explicit (`Evaluation.Filter bool`);
  zero value computes values without filtering.
- L1: file sources compare size and modification time between passes.
- L3: zero `TableFormat` means upstream defaults.
- L6: prefixed constants, unambiguous `Summary` field names, consistent
  integer types.
- L7, L4, L2, L10: documentation and tests; L8: context-aware spooling,
  consistent errors.
- L9: accepted — the CLI deliberately drives `internal/fpg` in upstream
  notation for byte compatibility; equivalence is enforced by
  `TestEquivalenceCLI*`. Recorded in `docs/decisions.md`.
- L5: register library differences in `docs/differences.md`.
