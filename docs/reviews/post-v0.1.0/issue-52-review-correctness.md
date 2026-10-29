# Critic review (correctness) — PR #55, issue #52

- PR: https://github.com/dimchansky/fpgrowth-go/pull/55 (branch `perf/52-byte-reread`)
- Head reviewed: f452168
- Decision: ACCEPT WITH CONDITIONS (two Low findings)
- Recorded by the orchestrator from the critic's final message.

## Resolution of findings

- L1: fixed in 98e98dd. `TestBuildBagBytes_errors` checks, for both
  passes, that a record error wins over the error of a scan that fails
  after yield returned false; the order mutant (M7) now fails it.
- L2: fixed in 98e98dd. The changelog entry says that `fpgpsp -gs` still
  reads its records through the string scan.
- I1 (info): no change in this PR. The C reference reaches 5.2 GB RSS on
  some `-P` cases of `TestDifferential` (seed 2), so night sweeps under
  a 4 GB-per-process watchdog can be killed by the C side.

## Critic's report

Scope: `tract.BuildBagBytes` against the refactored `BuildBag` and the
shared helpers (`buildFromRetained`, `pass2[T]`: begin/unknown/end/finish);
`fpg.DataBytes`, `psp.PrepareBytes`/`prepare`, the `cmd/fpgrowth`
`openInput` change and the `cmd/fpgpsp` estimate/generate wiring
including `-gs`; `tabread.Scan` vs `ScanBytes`, `CountBytes`, the new
tests and the docs; the issue's acceptance criteria.

### Evidence examined (code reading)

- Equivalence step by step: per record, the record counter, the "more
  records than in pass 1" check, the negative and overflowing weight
  checks and the codes reset run in the same order. For an unknown id the
  check function is called first, then the id is skipped under
  `AppearNone` or the scan stops with ErrNotReplayable; codes below 0 are
  skipped. End of record: sort, compact, zmin filter, `bld.add`. After
  the scan: the per-record error takes precedence over the scan error,
  then the totals are checked, then `bld.finish` with the abort hook.
  Error messages are the same (`%v` of `string(id)`).
- The `ignore` fast path requires `defApp == AppearNone && check == nil`,
  exactly the case where `unknown` would only `continue`.
- Aliasing: `b.codes[string(id)]` does not keep the slice. A string is
  built (a heap copy) only when `unknown` passes the id to the check
  function or formats it into an error. The bag receives only `Item`
  codes. The test scan clears the ids after each yield, so retained
  aliases would show up.
- `tabread.Scan` is a wrapper over `ScanBytes`, so both passes read
  through the same tokenizer.
- CLI wiring: `fpgrowth` no longer uses the string scan. In `fpgpsp`,
  `-ge` and the surrogate methods use the byte scan; `-gs` keeps
  `psp.Records` over the string scan and never retains. Stdin spooling,
  `ctxScan` and the error paths are unchanged.

### Commands run

| Command | Result |
| --- | --- |
| `gofmt -l .` | empty |
| `go vet ./...`, `staticcheck ./...` | ok |
| `GOMAXPROCS=2 go test -p 2 ./...` | ok |
| `go test -race` on tract, fpg, psp, cmd/... | ok |
| 12 line mutants of `bag.go` | 11 killed, 1 survived (M7, L1) |
| `fpg.DataBytes` passing zmin 0 | killed by the CLI goldens |
| `reference/build.sh` | ok |
| Forced reread (`retainLimit = 1`, local edit, not committed), `TestDifferential` seed 2 | 20,000 cases ok |
| Forced reread, `TestLarge` seed 2 | 300 cases ok |
| Forced reread, `TestFpgpsp` seed 2 | 5,000 cases byte-equal (1,582 estimations) |
| FIFO check with the forced binary | pass 2 blocks reopening the FIFO: the reread really happens |

Cases ran one at a time, `GOMAXPROCS=2`, under the watchdog. The first
`TestDifferential` attempt was killed by the watchdog at 4.9 GB; a
per-run rusage wrapper showed that all 236 runs above 512 MB were the C
reference (up to 5,176 MB on `-P` cases) and the Go binary never went
above 512 MB. An unwrapped rerun with the same seed passed.

### Findings

| ID | Severity | Status | Summary |
| --- | --- | --- | --- |
| L1 | Low | Confirmed, pre-existing | No test pins that the per-record error wins over the scan error in `pass2.finish` (mutant M7 survives; it changes both paths the same way). Real scans return nil when stopped by yield, so the CLIs are not affected. |
| L2 | Low | Confirmed (docs) | The CHANGELOG entry does not say that `fpgpsp -gs` still reads through the string scan. |
| I1 | Info | Confirmed | The C reference reaches 5.2 GB RSS on seed-2 `-P` cases, so a 4 GB-per-process watchdog can kill a `TestDifferential` sweep. |

### Fixes requested (non-blocking)

1. L1: one case where both the per-record error and the scan error are
   set, asserting that the per-record error wins.
2. L2: mention `fpgpsp -gs` in the CHANGELOG entry.

### Decision

ACCEPT WITH CONDITIONS (Low only).
