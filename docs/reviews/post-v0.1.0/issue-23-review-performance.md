# Review: #23 / PR #38 — architecture, CLI, packaging, performance

- Head reviewed: 904e54944e44b47fc1f6c4cf5b1e4ee1437a52e8
- Decision: ACCEPT WITH CONDITIONS (Low only)
- Note: the critic could not write this file (harness restriction); the orchestrator recorded its final report verbatim in substance.

## Evidence
- gofmt/vet/staticcheck clean; go test ./... ok; -race ok on cmd/fpgpsp, psp, report, isrout; cross-vet windows/amd64, linux/arm64, linux/386 ok.
- goreleaser check + snapshot (v2.18.3): 6 archives, each with fpgrowth, fpgpsp, LICENSE, README, CHANGELOG, PROVENANCE. Nothing published, no tags.
- Go vs C spectra byte-equal for -gr/-gp/-ge/-gs on the critic's own sparse and table shapes.
- Root API, go.mod/go.sum untouched; stdlib + internal only; no cgo. internal/psp reuses fpg.Miner, tract, report, isrout; no goroutines in it (D6 holds). Changes to existing packages are additive; isrout.WriteSpectrum prints 1.0*float64(c) (bit-identical, -P path only). fpgrowth mining code untouched, so main-vs-PR measurement skipped per budget.

## Performance (Go vs C fpgpsp-fixed -O3, 2 interleaved runs, <1.5% instruction spread; load 4.5 → 23.7)
| Shape / args | instr Go/C | user Go/C |
|---|---|---|
| 20k sparse -gr | 1.94x | 1.6-1.8x |
| 20k sparse -gp | 2.89x | 1.9x |
| 20k sparse -ge | 1.78x | 1.4x |
| 5k×10 table -gs | 2.89x | 2.1x |
| 5k×10 table -gp | 2.85x | 2.0x |
| 4000×40 bench -gp -c64 -Z1 | 3.30x | 2.5x |

maxrss at -Z64: Go 80-92 MB, C 41 MB. New feature, not a regression; no easy fix (iswap already tight; remainder is per-surrogate item base/tree rebuild + GC).

## Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| F1 | LOW | confirmed in code, likely unreachable | run.go generate: a failing chain cancels; an earlier chain may return context.Canceled, first-non-nil selection reports "interrupted"/130, hiding the real error (ErrTooLarge, fpg.New on -gs). Fix: prefer first non-Canceled error. |
| F2 | LOW | measured, same as upstream | one goroutine per chain; -Z k keeps k miners/surrogates alive (15 MB → 80-92 MB). Optional NumCPU semaphore. |
| F3 | LOW | confirmed | perf range is 1.8-3.3x instr, 1.4-2.5x user; D7/PR text quote only the best case. Follow-up: reuse item base/recoding across surrogates. |
| F4 | LOW | confirmed, pre-existing | CI snapshot job does not assert both binaries per archive. |
| F5 | LOW | documented in D7 | cmd/fpgpsp/input.go duplicates ~200 lines of fpgrowth input helpers. |

Conditions: fix or defer F1; correct F3 wording; D7 needs maintainer sign-off. F2/F4 optional.

---

# Re-review (round 2): #23 / PR #38 — architecture, CLI, packaging, performance

- Head reviewed: a11e72ed7f42a492ae0c677ffff9fffa263cc901
- Decision: ACCEPT WITH CONDITIONS (one Low wording nit; D7 maintainer sign-off)
- Date: 2026-10-26 ~11:15, light work only; no heavy step needed for the decision.

## Checks
- gofmt/vet/staticcheck clean; go test ./... ok (14 packages); -race on cmd/fpgpsp run/cancel/chainError tests ok; CI 15/15; snapshot log "6 archives with fpgrowth and fpgpsp".
- New commits touch only internal/psp, cmd/fpgpsp, reference/, docs, ci.yml. U25 and the empty-maximal-set change live in psp PrepConfig, TableData(zmin=0) and Mine; fpg.Miner.Data is unchanged. Library/fpgrowth CLI paths untouched → main-vs-PR measurement skipped.

## Worker-pool probe (904e549 vs a11e72e, -gp -s-60 -c128 -S1, 4000×40 bench, 10 CPUs, load ~5; raw: review-perf/out/pool.txt)
| Run | old maxrss | new maxrss | notes |
|---|---|---|---|
| -Z128 ×3 | 126-173 MB | 62-83 MB | instr 9.155e9 vs 9.166e9 (+0.1%); user 1.30-1.48 s vs 1.35-1.37 s; output byte-equal |
| -Z8 | 62 MB | 59 MB | unchanged |

## Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| P-F1 | was LOW | FIXED, verified | chainError prefers first non-Canceled error; mutating the branch fails TestChainError. |
| P-F2 | was LOW | FIXED, verified | min(parts, GOMAXPROCS) workers over a buffered closed channel; cancel drains, wg.Wait joins, no leak; spectra merged in chain order (TestRun_workersSameResult + byte-equal probe). |
| P-F3 | was LOW | FIXED | D7/PR state 1.8-3.3x instr, 1.4-2.5x user, ~2x maxrss; follow-up #39. |
| P-F4 | was LOW | FIXED, verified | CI reads each listing fully before grep (no SIGPIPE under pipefail); checks 4 tar.gz + 2 zip, requires n=6. |
| P-F5 | LOW | open, documented | cmd/fpgpsp/input.go duplicates fpgrowth input helpers (D7: later refactoring). |
| P-N1 | LOW | new | D7/PR quote pre-pool "80-92 MB vs 41 MB at -Z64"; mark as pre-pool or re-measure. |

---

## Orchestrator follow-up (2026-10-26)

Applied in the record commit: D7 memory figure marked as measured before the GOMAXPROCS bound (P-N1); D7 status corrected to "awaiting the maintainer's sign-off" (it had said "decided by the maintainer"); D7 floating-point bullet updated to the bounded tolerance and the unrounded C trace; the `generate` comment no longer says one goroutine per part; the -gs zmin=0 line explains why it changes no output (I3); docs/upstream/fpgpsp.md §7 notes the empty maximal set (I5).
