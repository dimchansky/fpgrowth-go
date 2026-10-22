# Critic review (CLI) — PR #37, issue #22 (FEAT-2, zlib output `-z`)

- Issue: #22 — Implement optional zlib-compressed output in the fpgrowth CLI
- PR: https://github.com/dimchansky/fpgrowth-go/pull/37 (branch `feat/22-zlib-output`)
- Head SHA reviewed: `45e8e15193e67b8b2021e9f081fe23b7391660e9`
- Reviewer specialization: CLI design and upstream compatibility, zlib streams (RFC 1950), I/O error handling, interoperability
- Working dir: `~/.cache/fpgrowth-cycle/22/review-cli` (clone, scripts, raw outputs)
- Time: 2026-10-22 23:0x-23:2x (night window; heavy steps logged in ACTIVITY.md as `[critic-cli]`)

## Scope

`cmd/fpgrowth/{options,run,usage}.go`, `cmd/fpgrowth/zlib_test.go`, option
tests, goldens (`usage`, `err-z`), `reference/build.sh zlib`,
`reference/difftest/zlib_test.go`, docs (`docs/cli.md`, `docs/differences.md`
O12, `docs/audit/differences.md`, `docs/feature-matrix.md`, README, CHANGELOG).

## Evidence examined (upstream)

- `fpgrowth.c:1989-1991` (`FPG_ZLIB` -> `ISR_ZLIB`), `2329-2332` (usage line
  only with `USE_ZLIB`), `2393-2394` (`case 'z'`, flag without value),
  `2526-2531` (`isr_prstats(report, stdout)` before `isr_close`).
- `report.c:171-200` (`isr_flushx`: deflate of the 64 KiB buffer, `fwrite`,
  return codes unchecked), `975-1007` (`isr_open`: `deflateInit` only when a
  file/stdout exists — count-only unaffected), `1011-1026` (`isr_close`:
  `Z_FINISH`, `deflateEnd`, `ferror`/`fclose`). `fastout` and all other
  writers go through `isr_putsn`/`isr_putc` -> the same buffer, so the fast
  path is compressed too. `isr_prstats` uses plain `fprintf` -> `-Z` never
  compressed; `-P` uses the table writer -> never compressed.

Conclusion: the PR compresses exactly what upstream compresses (item sets /
rules to a file or stdout; not `-Z`, `-P`, log, stderr; nothing in
count-only mode).

## Commands run and results

| Command | Result |
| --- | --- |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` (root and reference) | clean |
| `go test ./...` | ok (all 12 packages) |
| `go test -race ./...` | ok |
| `(cd reference && ./build.sh release zlib)` | built; zlib smoke checks passed |
| `(cd reference && go test ./...)` | ok, TestZlib ran (not skipped): 41 fixed + 1000 random, 0 FAIL |
| `DIFFTEST_SEED=4242 DIFFTEST_ZLIB_CASES=3000 go test ./difftest -run TestZlib -v` | 41 fixed + 3000 random, 0 FAIL (`zlib-seed4242.txt`) |
| `go test -fuzz FuzzParseArgs -fuzztime 30s ./cmd/fpgrowth` | ok |
| `scripts/cases.py`: all 154 CLI golden cases with main, PR, PR `-z`, C zlib `-z`, decoded with **Python zlib** (independent decoder, checks EOF and trailing data) | main vs PR plain: identical stdout/stderr/exit/files for all cases except `usage` and `err-z` (intended, O12). 91 `-z` streams (file and stdout) decoded: each one complete stream, decompression == plain output, stdout bytes after the stream == plain `-Z`/`-P` text. 69 non-deviation cases: C zlib `-z` == Go `-z` after decompression (stdout, stderr, files, exit). Only "failure" was my harness's `>&-` case (DEVNULL substitution), re-checked separately below. (`cases-out.txt`) |
| `scripts/opts.sh`: 39 option-syntax cases, C zlib vs Go (`-zZ`, `-Zz`, `-zs50`, `-s50z`, `-zz`, `-z -z`, `-z5`, `-tz`, `-zt`, `-ztr`, `-zw`, `-x`, `-q0`, `-q-2`, `-l0`, `-As -l0`, `-m10` empty output to file/stdout, `-v ""`, `-g`, `-N`, `-P`, `-Z -P ""`, `-z -!`, `-! -z`, `-k -z`, `-z` after the file names, extra args, missing input, unwritable output, `-R`) | identical exit, log, decoded stdout and decoded file in 38/39; the one difference is `-h` (documented deviation O1) |
| Empty output (`-m10`) | Go and C both emit `78 9c 03 00 00 00 00 01` (identical bytes) |
| README example `fpgrowth -z -s30 test1.tab out.z` + Python decode | prints `d (70)`, `c d (40)`, `c (70)` as documented |
| `scripts/pipes.sh`: closed stdout (`>&-`) and broken pipe (`| head -c 100`, 2.4 MB output) for main, PR, C zlib, with and without `-z` | broken pipe: exit 141 (SIGPIPE) for all six — `-z` behaves like plain and like C. Closed stdout: Go exit 0 with and without `-z` (C10, pre-existing, documented), C exit 4 |
| Mutations of `cmd/fpgrowth/run.go`, `go test ./cmd/fpgrowth` | M1 skip `zw.Close`: 3 tests FAIL. M2 ignore `zw.Close` error: TestZlibWriteErrors FAIL. M3 close file before zlib: 3 tests FAIL. M5 `-Z` written into the compressed stream: TestZlibOtherOutputs FAIL. M6 ignore file `Close` error: TestZlibWriteErrors FAIL. **M4 ignore the early-close error (`closeErr`) of `-z -Z` on stdout: all tests PASS** (see F1) |
| Write-call count of `zlib.Writer` (counting writer, 2.4 MB plain output) | 1448 writes for 355 KB, avg 245 bytes per write (see F2) |
| `/usr/bin/time -l` PR `-z` vs local variant with 64 KiB `bufio` under zlib, 4.4 MB plain output, 3 interleaved runs (load avg 3.7-14) | instructions retired 1.443-1.445 G vs 1.382-1.389 G (~4 %); user/sys/wall equal at this size. Decompressed outputs identical |

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| F1 | LOW | confirmed (test gap) | With `-z -Z` and the item sets on stdout, `mine` closes the stream early (`closeErr = out.close()`); no test fails when that error is ignored (mutation M4). Behavior is still correct today only because the following statistics write to the same failing stdout also fails and yields exit 4; a writer that fails only on the stream's last block/checksum and then recovers would give exit 0 with a truncated stream under M4. | `run.go` mine, `err != nil \|\| closeErr != nil`; mutation M4 passes `go test ./cmd/fpgrowth`. Fix: add a `TestZlibWriteErrors` case with `-Z` to stdout whose writer fails at the checksum but accepts later writes (a writer that fails exactly one write), asserting exit 4. |
| F2 | LOW | confirmed | `zlib.NewWriter` writes directly to the `*os.File` (file or stdout); `compress/flate` flushes every ~240 bytes, so a compressed output of N bytes costs ~N/245 `write` syscalls (C uses stdio buffering). Measured ~4 % more instructions on a 4.4 MB output; negligible in wall time at that size, grows linearly with output. | counting writer: 1448 writes / 355514 bytes; `/usr/bin/time -l` 1.44 G vs 1.38 G instructions with a 64 KiB `bufio.Writer` between zlib and the file (flush before the file close). Suggest: insert `bufio.NewWriterSize(dst, 64<<10)` under the zlib writer and flush it in `output.close` (or record the choice). Performance critic may weigh it. |
| F3 | LOW | confirmed (doc precision) | CHANGELOG says "`-Z`, `-P`, the log and count-only mode are unchanged" by `-z`; with `-Z` and stdout output the statistics move to after the zlib stream (O12). `docs/cli.md` states it precisely; CHANGELOG should say "not compressed" rather than "unchanged" or mention the O12 placement. | `CHANGELOG.md` [Unreleased] entry vs `docs/cli.md` "Compressed output" bullet 2 |
| F4 | LOW | confirmed (doc precision, partly pre-existing) | `docs/cli.md` says write errors of the stream give exit 4 "as without `-z`"; `TestZlibClosedPipe` uses an in-process pipe. For the real standard output a broken pipe kills the process with SIGPIPE (exit 141), as without `-z`, as on main, and as C — consistent behavior, but the docs (and pre-existing C10 text "broken pipe ... reported as in C3") imply exit 4. | `scripts/pipes.sh`: exit 141 for main/PR/C with and without `-z` |

No CRITICAL, HIGH or MEDIUM findings.

## Decisions under review

1. **Usage text lists `-z`.** Correct and consistent: the program implements
   the `USE_ZLIB` build, and its usage/help are byte-identical to that build
   (TestZlib fixed, `usage`/`-!` C-vs-Go equality). The golden `usage` moved
   to `golden-go` with deviation entry O12; documented in O12, `docs/cli.md`.
   Acceptable.
2. **`-Z` after the complete stream on stdout.** Correct choice: C's zlib
   build interleaves plain `fprintf` text with its own stdio-buffered
   compressed bytes (before the stream for small outputs, inside it for
   larger ones), which no decoder can separate reliably; Go's layout is
   decodable (stream, then plain text) and matches O6's "statistics after the
   item sets". O12 states the C behavior with citations
   (`fpgrowth.c:2528-2530`, `report.c:171-200, 1013-1020`) accurately;
   `docs/cli.md` explains how to read it. TestZlib verifies both layouts.

## Fixes requested

- F1: add the missing regression test (cheap).
- F2: optional buffer under the zlib writer, or a recorded decision.
- F3, F4: wording.

## Performance impact

Uncompressed path: byte-identical outputs on all 154 golden cases; code path
changes are a nil check and an `io.WriteCloser` indirection on file
creation (instruction comparison done by the implementer, not repeated per
the measurement budget). `-z` path: see F2 (~4 % instructions from small
writes).

## Decision

**ACCEPT WITH CONDITIONS** (only LOW findings: F1 test gap, F2 write
granularity, F3/F4 doc wording). All acceptance criteria of #22 verified:
valid RFC 1950 streams (independent Python decoder, 91 golden streams + C
parity on 69 cases + 4041 TestZlib cases), other outputs unchanged/not
compressed, write/close errors exit 4 (mutations M1-M3, M5, M6 caught),
plain output byte-identical to main, C zlib differential test present and
passing, no new dependencies.
