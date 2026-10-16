# Architecture

This document describes how the Go port is structured and the conventions
every package follows. Upstream behavior is specified in `docs/upstream/*.md`
(audits of the C sources); this document records how the Go code maps onto
it. Decisions with rationale are in `docs/decisions.md`; deviations from
upstream in `docs/differences.md`.

## Package layout (Stage 1)

| Package | Upstream origin | Responsibility |
| --- | --- | --- |
| `internal/tract` | `tract.c` (`TA_READ`), sorting routines of `arrays.c` | Base types (`Item`, `Supp`), generic item base (identifier ↔ code, frequencies, appearance flags), recoding with upstream-exact sort order, the transaction *bag* built from a replayable source (prefix-tree staging, iteration in upstream bag order), bag operations (`zmin` filter, item frequencies, 16-item packing) |
| `internal/tabread` | `tabread.c`, `escape.c`, number parsing in `tract.c`/`fpgrowth.c` | Upstream "flexible" table format reader (record/field/blank/comment characters), weight field, selection/appearance file parsing, C-compatible `strtol`/`strtod` emulation, escape decoding |
| `internal/report` | `report.c` (`ISR_PATSPEC`, `ISR_CLOMAX`), `clomax.c`, `patspec.c` | Mining-side item set reporter: current item set, perfect extensions, closed/maximal/generator filtering, size/support/border/evaluation gates, statistics, pattern spectrum; emits results to a `Sink` |
| `internal/ruleval` | `ruleval.c`, `chi2.c`, `gamma.c` | Rule evaluation measures and special functions |
| `internal/istree` | `istree.c` (subset used by fpgrowth) | Item set tree for association rules and rule-based evaluation |
| `internal/fpg` | `fpgrowth.c` (mining part), `fim16.c` | FP-growth variants (simple, complex, single, top-down, tree), 16-items machine, mining driver |
| `internal/isrout` | `report.c` text output, `scanner.c` (`scn_format`) | Text output for the CLI: item names, separators, info format codes, upstream-exact number formatting, statistics printing |
| `internal/oracle` | — (independent) | Brute-force reference miner and deterministic random data generator for tests |
| `cmd/fpgrowth` | `fpgrowth.c` `main()` | Upstream-compatible command line program |
| root `fpgrowth` | — | Public API (deliberately minimal in Stage 1; designed in Stage 2) |

Dependency direction: `tract` ← `report` ← `istree` ← `fpg` ← root/CLI;
`ruleval` ← `istree`, `report`; `tabread` ← CLI; `isrout` ← CLI.
`oracle` depends only on `tract` types and is imported by tests only.

## Base types (`internal/tract/types.go`)

- `Item int32` — item code after recoding (upstream `ITEM`, `int`).
- `Supp int64` — supports and transaction weights (upstream `SUPP`, 32-bit
  `int`). Widened deliberately; see "Integer policy".
- Counts of transactions/item sets use `int`/`uint64`.

## Data flow

1. **Pass 1 (count).** Read the source once: build the item base
   (identifiers in first-seen order, frequencies, size sums, total weight,
   record count). Duplicates within a record are ignored (first occurrence
   counts); every record, including empty ones, counts toward the record
   count and the total weight.
2. **Recode.** Compute the absolute minimum support from the total weight,
   drop infrequent items, sort the rest exactly as upstream (`-q` modes,
   upstream's unstable quicksort reproduced literally for tie order), and
   assign item codes.
3. **Pass 2 (build bag).** Read the source again, recode each record,
   apply the `zmin` filter, sort its items, and insert it into a prefix tree.
   The prefix tree is the Go replacement for upstream's sorted and reduced
   transaction bag (`tbg_sort` + `tbg_reduce`); its depth-first traversal
   yields the same transactions in the same order, so every variant's
   tree construction sees exactly what upstream sees. Raw transactions are
   never retained.
4. **Mine.** The selected variant builds its FP-tree from the bag and
   recurses, driving the reporter (`internal/report`) which applies all
   gates and emits item sets to a `Sink`. Rules and rule-based evaluation
   go through the item set tree (`internal/istree`), like upstream.
5. **Output.** The CLI's `Sink` (`internal/isrout`) formats records
   exactly like upstream. Library users get typed values.

Upstream needs every transaction in memory; the Go port needs the item
base, the bag prefix tree, the FP-tree(s) and any output-dependent
structures (closed/maximal repository, item set tree for rules). That is
inherent to FP-growth and documented as such; nothing here is
constant-memory. Sources that cannot be replayed (stdin) are spooled to a
temporary file by the CLI.

## Conventions for all packages

**Fidelity.** Port semantics, not syntax. Anything that influences *which*
patterns are produced or their *order* or *printed form* (sort routines,
tie-breaking, traversal order, threshold arithmetic, number formatting)
is ported operation by operation. Each package's doc comment lists the
upstream functions it ports (C name → Go name).

**Upstream bugs.** Confirmed upstream defects are fixed, not copied, when
the intended behavior is clear (documented help text, an independent
specification, or the obvious intent). Every fix is marked in code with a
`// Deviation from upstream:` comment, has a regression test, and is listed
in `docs/differences.md`. Pure presentation quirks that are deterministic
and harmless (e.g. the number formatter's rounding) are preserved for
byte-compatible CLI output.

**Integer policy.** Supports are `int64`. Negative transaction weights are
rejected with an error (upstream silently corrupts results). Because all
weights are non-negative, every support is bounded by the total weight, so
checking the total weight for overflow during pass 1 makes all support
arithmetic safe. Products and sums that can exceed the total (e.g. size ×
weight sums) use overflow-checked or saturating arithmetic. Conversions of
user-supplied floating-point limits to integers are clamped explicitly.
The effective absolute minimum support is at least 1.

**Floating point.** Follow upstream's evaluation order. Go may fuse
`x*y + z` into an FMA on arm64 and other platforms; wrap products that
feed additions in `float64(...)` so results match unfused C. The C
reference is built with `-ffp-contract=off` for the same reason. Use the
standard `math` package; where libm and Go can differ in the last unit in
the last place (`log`, `exp`, gamma functions), differential tests use
justified tolerances.

**Memory layout.** Trees are slices of value structs addressed by `int32`
indices, not pointer graphs. Scratch buffers are reused across recursion
levels where upstream does the same. No per-node heap allocations in hot
paths.

**Errors and cancellation.** Invalid input and I/O failures are returned as
errors; panics are reserved for violated internal invariants. Mining
checks for cancellation at the points where upstream checks
`sig_aborted()` (recursion entry) using a cheap non-blocking channel
receive. A `Sink` error aborts mining and is returned unchanged.

**Testing.** Each package has table-driven unit tests, edge-case tests and
property tests against `internal/oracle` where meaningful. Expected values
produced by running upstream C code are committed as fixtures together
with the harness that produced them (`reference/harness/...`), so plain
`go test ./...` never needs a C compiler. Floating-point fixtures record
the C build flags used.

**No new dependencies** in the root module. Development tools that need
C or extra modules live in the nested `reference/` module.
