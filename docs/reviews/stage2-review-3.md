# Stage 2 review — iteration 3 (full stage review)

- **Stage / iteration:** Stage 2, iteration 3 (full review + verification of iteration 2)
- **Critic:** fresh independent reviewer agent (Go library architect, API
  design and concurrency/cancellation, maintainability/testing); no
  repository files modified.
- **Revision:** `6659506` on `607fdea`, Go 1.27.2, Apple M1 Max.

## Evidence gathered by the critic

| Check | Result |
| --- | --- |
| gofmt, vet (root + reference), staticcheck | clean (3 pre-existing ST1023 hints with `-checks all`) |
| `go test -race ./...` + coverage | 11 packages ok; total 97.4 % |
| difftest default seed; new seed 663127 (20 000 + 400 large) | large 0 failures; small 2 FAIL — both harness classification gaps present already with `607fdea` (N11), no port defect |
| Fuzzers (MineItemsets 529 k, MineRules 583 k, CSVFile 775 k, Mine, BuildBag) | pass |
| Own brute force through the public API | 80 000 cases, 0 failures |
| Public API vs CLI binary, byte-exact incl. order | 4 000 runs, 0 differences |
| README snippets (15) | outputs match |
| Leaks (iterator break, cancel, spool) | none |
| Abort-check overhead (benchstat vs `607fdea`) | reporter micro-benchmark +7–9 %; whole mining runs within noise (−3 % … +5 %) |

## Iteration-2 findings

| ID | Status |
| --- | --- |
| N1 (High) | **Fixed** — 313 deadline runs over all targets/algorithms/evaluations: `ctx.Err()` always, ≤ 1 ms latency, ≤ 878 results after cancel; rules ≤ 1 ms; CLI exit 130 in 1.4–8.6 ms |
| N2 | Fixed — Fisher cancel 0.2–1.5 ms; cost documented |
| N3 | Fixed (any 1.5×, struct with interface 1.7× vs string) |
| N4 | Fixed and documented |
| N5 | Mostly fixed; `PROGRESS.md` stale (N12) |
| N6 | Fixed |

## New findings

| ID | Severity | Title |
| --- | --- | --- |
| N7 | Medium | No cancellation check while the bag is finished and the top-level FP-tree is built (item-set path): 2.6–3.6 s (34.6 s top-down) at 4 M transactions; the rules path already checks every 4 096 transactions |
| N8 | Medium | Top-down: after the abort is latched, `recTopDown` keeps merging the rest of the level instead of unwinding (17–54 s after cancel with `MaxSize: 1`) |
| N9 | Low | CLI swallows a second SIGINT |
| N10 | Low | `MineItemsets` returns nil when canceled just before an early "no frequent items" return; `OnStart` runs after cancel |
| N11 | Low (harness) | Two difftest classification gaps (Fisher value exactly at threshold without a value column; M3 order difference not exempt) |
| N12 | Low | `docs/PROGRESS.md` stale |

## Decision

**ACCEPT WITH CONDITIONS** — no Critical/High findings. Conditions: fix
N7 and N8 with tests; N9–N12 as polish. Being Medium/Low only, no further
full review cycle is required; fixes are verified by tests and recorded
below.

## Fix record

To be completed by the orchestrator after the fixes land.
