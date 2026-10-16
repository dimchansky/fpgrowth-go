# Stage 2 review — iteration 2 (full stage review)

- **Stage / iteration:** Stage 2, iteration 2 (full review + verification of iteration 1)
- **Critic:** fresh independent reviewer agent (senior Go library architect,
  API design, maintainability/testing); not involved in the code; no
  repository files modified.
- **Revision:** `53c71de` on `59970a4`, Go 1.27.2 darwin/arm64.

## Evidence gathered by the critic

| Check | Result |
| --- | --- |
| gofmt, vet, staticcheck | clean (3 pre-existing ST1023 hints with `-checks all`) |
| `go test -race -count=1 ./...` | 11 packages ok; coverage total 97.4 %, root 94.0 % |
| difftest default + new seed 917331 (20 000 + 400 large) | 0 failures (14 887 byte-equal) |
| Fuzz `FuzzMineItemsets` 465 k, `FuzzMineRules` 157 k, `FuzzCSVFile` 761 k, `FuzzMine` | pass |
| Own brute force via public API (72 000 cases incl. weights up to 2^59, exact counts, OnStart, Filter on/off) | 0 failures |
| Public API vs real CLI binary incl. order (1 500 runs) | 0 differences (36 documented L-2 cases) |
| README snippets (15) | outputs match |
| Exact counts near 2^63, OnStart semantics, TableReader/CSVReader over gzip and `fs.FS` | correct |
| Leaks (iterator break, cancel, timeouts, spool cancel), 8 concurrent miners under `-race` | none |

## Iteration-1 findings

H1, M1, M2, M3, L1, L6, L7, L8, L10 fixed; M4, L3, L4 fixed in code with
stale docs (see N5); L2, L5 partly; L9 accepted (D5).

## New findings

| ID | Severity | Title |
| --- | --- | --- |
| N1 | **High** | Cancellation ignored while the reporter enumerates perfect extensions (2^k subsets) and during k-items-machine bursts; `MineItemsets` returns nil after cancel (e.g. 26 identical items, 50 ms deadline: 268 M sets in 5.4 s, error nil); CLI SIGINT ignored, exit 0 after 16 s |
| N2 | Medium | Fisher measures cost O(min(body, head)) per evaluation with int64 weights (w = 1e8: 27–40 s) and cannot be cancelled; undocumented |
| N3 | Low | `ErrInvalidItem` reflection checks allocate per item per pass for `any`/structs with interface fields (5–8× slower) |
| N4 | Low | Replay check: failing `Stat` is fatal, volatile ModTime gives false `ErrNotReplayable`, wrapped file sources unchecked, touch-only change rejected (undocumented) |
| N5 | Low | Documentation drift (README zero `TableFormat`, weighted empty lines; `Evaluation` GoDoc; `api-design.md` field name; PROGRESS; missing library differences; cancellation bounds) |
| N6 | Low (observation) | Memory documentation qualitative; measured peak heap ≈ 6× file size on 4 M distinct transactions with 1 067 frequent items |

## Decision

**REJECT pending fixes**: N1 with tests (API and CLI); N2 fixed or
accepted and documented; N3–N6 as polish.

## Orchestrator decisions for the fixes

- N1: abort checks every N outputs in `Reporter.expand`/`output` and in
  the k-items machine reporting, latched into the reporter so every miner
  path unwinds; `mine` returns `ctx.Err()` whenever the context was
  canceled before mining finished; CLI SIGINT exits 130 promptly. Tests on
  data with many perfect extensions and fim16 bursts (API + CLI).
- N2: fix — abort checks inside the Fisher summation loops (internal
  abortable evaluation path used by `istree`), plus documentation of the
  O(min(body, head)) per-evaluation cost.
- N3: validate each distinct item once (when first entered into the item
  base); document the remaining cost.
- N4: skip the metadata check when `Stat` fails; document that only
  unwrapped file sources are checked and only by size and mtime.
- N5: fix all listed drift. N6: publish the measured memory figures and a
  warning that peak memory can exceed the input size.
