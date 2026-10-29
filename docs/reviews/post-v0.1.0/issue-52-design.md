# Design — issue #52 (second pass over byte slices above the budget)

- Written by the implementer on 2026-10-29, on Apple M1 Max (arm64),
  Go 1.27, main at acd8d4a. Raw data, scripts and the activity log:
  `~/.cache/fpgrowth-cycle/52/` (`perf/bin/big.sh`, `perf/res/big.txt`,
  `perf/res/micro-{main,pr}.txt`, `difftest*.txt`, `ACTIVITY.md`). Data:
  input B of the #39 performance review
  (`~/.cache/fpgrowth-cycle/48/data/B.tab`, 20,000 sparse records over
  1,000 items), concatenated N times (B×N).
- Starting point: finding P3 of `issue-48-review-performance.md`. Above
  the 128 MiB budget of D8 the CLIs read the input a second time, and
  that pass (`ItemBase.BuildBag` over `tabread.Scan`) converted every
  field to a string before the map lookup. Pass 1 already used
  `tract.CountBytes` over `tabread.ScanBytes`.

## Changes

1. `internal/tract`: `BuildBagBytes(b *ItemBase[string], scan
   ByteScanFunc, zmin int)`, the counterpart of `CountBytes`. It looks
   every field up with `b.codes[string(id)]`, which does not allocate.
   It allocates a string only for an unregistered identifier that goes to
   the check function or into a "not seen in pass 1" error. An ignored
   identifier (default appearance AppearNone and no check) is skipped
   without one, and so are unselected items.
2. `BuildBag` and `BuildBagBytes` share all steps except the lookup
   loop. `buildFromRetained` does the Recode panic and the retained
   records. The per-record steps are in `pass2[T]`: `begin` checks the
   record count, a negative weight and weight overflow, `unknown`
   handles the check function, the selection skip and
   `ErrNotReplayable`, `end` sorts, drops duplicates, applies the zmin
   filter and calls `bagBuilder.add`, and `finish` checks the totals.
   The error messages are unchanged.
3. `internal/fpg`: `DataBytes(m *Miner[string], ib, scan ByteScanFunc)`.
   `internal/psp`: `PrepareBytes`, which shares `prepare` with
   `Prepare`.
4. `cmd/fpgrowth` now uses `fpg.DataBytes`. Its `openInput` returns only
   the byte scan, because the string scan is no longer used. `cmd/fpgpsp`
   uses `psp.PrepareBytes` for `-ge` and the surrogate methods. `-gs`
   still reads its records with `psp.Records` over the string scan,
   because it never retains them (D8). That path is outside this issue.
5. The library is unchanged. It calls `Count` and `BuildBag` over
   string scans and never retains. `BuildBag` stays for it and for the
   generic tests.

## Correctness evidence

- `TestBuildBagBytes_matchesBuildBag`: 2,000 random databases with
  weights 0-3, duplicates, empty records, selections, the check
  function, smin 1-3, all five item orders and zmin 0-4. In about a
  third of them pass 2 differs from pass 1 (an unknown item, an extra
  or a missing record, another weight, a negative or overflowing weight)
  or the scan fails. The bags (`checkBagsEqual`) and the error messages
  and kinds match `BuildBag`.
- `TestBuildBagBytes_errors`, `_abort` (with and without retained
  records, and a scan canceled like `ctxScan`), `_retained` (no scan
  while records are retained, a scan in the second call, the same bag).
- `TestBuildBagBytes_noAllocation`: the allocations of a build do not
  grow with the number of records of known items. This includes one
  unselected item per record. It fails before the change: with
  `BuildBagBytes` written as a string conversion around `BuildBag` it
  reports 50,018 allocations for 10,000 records against 68 for 10.
- `TestGoldenRetention` (both CLIs) runs every golden case, which is
  verified against C by `TestGolden`, with a budget of 1 byte and
  without retention. Both now run the new path.
- `reference/difftest` ran with the default binaries, and again with
  both programs built with `retainLimit = 1`. That edit was local and
  not committed. With it, every case of `TestDifferential`, `TestLarge` and
  `TestFpgpsp` (3,000 cases) reads the input a second time through
  `BuildBagBytes`. Both runs passed.

## Performance

`/usr/bin/time -l`, GOMAXPROCS=2, one process at a time under the RSS
watchdog, two interleaved rounds (instructions within 0.1 %), load
average 3.5-4.2 from other sessions. The output md5 is the same for C,
main and #52 on every row.

| Row | C | main | #52 | #52 / main | #52 / C | max RSS MB (C / main / #52) |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| `fpgrowth -s50` B×360 (195 MB, over) | 16.97G | 53.43G | 44.42G | -16.9 % | 2.62 | 464 / 135 / 135 |
| `fpgpsp -ge -z1 -s-20 -S1` B×360 | 19.92G | 61.88G | 52.85G | -14.6 % | 2.65 | 519 / 421 / 421 |
| `fpgrowth -s50` B×200 (108 MB, within) | 9.42G | 14.40G | 14.40G | -0.0 % | 1.53 | 259 / 127 / 127 |
| `fpgpsp -ge -z1 -s-20 -S1` B×200 | 11.08G | 18.76G | 18.75G | -0.1 % | 1.69 | 259 / 254 / 254 |

User time over the budget fell from 4.14 s to 3.44 s for `fpgrowth`
(C 1.72 s) and from 5.27 s to 4.52 s for `fpgpsp` (C 2.87 s). Within
the budget the second pass does not run.

The library path is the string `BuildBag` now built on `pass2`.
`BenchmarkItemBase_BuildBag` and `BenchmarkBuildBag` were run on main
and #52 interleaved, 6 runs each, with benchstat: -1.4 % (p=0.004),
-1.6 % (p=0.008) and ~ (p=0.15) in time, and 2 fewer allocations per
build. There is no regression.

## What remains

Above the budget the input is still read and tokenized twice, while C
reads it once. That is most of the remaining factor of 2.6. Removing it
would need a larger budget or a spill format, and D8 rejected both for
now. `fpgpsp -gs` and the library still use the string scan in their
second pass.
