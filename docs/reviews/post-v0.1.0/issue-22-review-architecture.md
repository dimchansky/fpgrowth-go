# PR #37 review — architecture, CI/build, docs, I/O performance

- Issue: #22 (FEAT-2, zlib output `-z`). PR: https://github.com/dimchansky/fpgrowth-go/pull/37
- Head reviewed: 45e8e15 (branch feat/22-zlib-output), clone in `review-arch/fpgrowth-go`
- Reviewer: critic-arch (Go architecture/maintainability, CI/build, docs accuracy, I/O-path performance). CLI/zlib semantics and error handling in depth: other critic.

## Evidence examined
Issue #22 body and acceptance criteria; `git diff origin/main...HEAD` (24 files); `cmd/fpgrowth/run.go` (`mine`, `output.close`, `openOutput`), `options.go`, `usage.go`, `main.go`, `zlib_test.go`; `reference/build.sh`, `reference/difftest/zlib_test.go`, `doc.go`; `.github/workflows/ci.yml`; `.goreleaser.yaml`; all doc changes; upstream `fpgrowth.c:2326-2334`; repository ruleset 25292730; CI job logs of run 39845961847.

## Commands and results
- `gofmt -l .` empty; `go vet ./...` ok; `staticcheck ./...` ok.
- `CGO_ENABLED=0 go build ./...` ok; `CGO_ENABLED=0 go test ./...` all 12 packages ok (no C, no zlib needed at root).
- `go test -race ./...` rc=0 (`review-arch/race.txt`).
- `git diff origin/main...HEAD -- go.mod go.sum`: unchanged. Only new import is stdlib `compress/zlib`.
- `goreleaser check`: 1 configuration validated; config has `CGO_ENABLED=0`, nothing needed for `-z`.
- `gh pr checks 37`: 15/15 pass. Ruleset "main" requires 15 contexts; names identical to the PR's checks (the reference job name is unchanged; only a step was added).
- CI logs: `build.sh zlib` ran on ubuntu-latest and macos-latest (warning counts equal to release: 1 and 77); difftest ok in 114 s / 208 s. Because the step builds the binary first, `zlibReference` takes the prebuilt path and cannot skip.
- Mutation: removing `o.zw.Close()` from `output.close` makes `TestZlibRoundTrip`, `TestZlibOtherOutputs`, `TestZlibWriteErrors` fail; restored.
- Perf (`review-arch/perf.sh`, `perf.tsv`, `perf.load`; load avg 40-43, VM + other go builds; judged on instructions retired, 2 runs, main vs PR built from my clone, 30k-transaction synthetic data):

| row | main (G instr) | PR (G instr) | output |
| --- | --- | --- | --- |
| `-s0.01` to file | 0.765 / 0.761 | 0.760 / 0.760 | identical, 4.15 MB |
| `-tr -s0.03 -c30` to file | 1.290 / 1.285 | 1.287 / 1.286 | identical, 6.71 MB |
| `-tc -s0.005` to file | 1.006 / 1.002 | 1.004 / 1.006 | identical, 3.21 MB |
| `-s0.01` to stdout | 0.750 / 0.750 | 0.751 / 0.751 | — |

  Differences -0.7 %..+0.3 % in both directions: noise. `-z` cost: sets 0.760 -> 1.097 G (+44 %), 4.15 MB -> 0.49 MB; rules 1.288 -> 1.850 G (+44 %), 6.71 MB -> 0.99 MB; wall 0.06 -> 0.08 s. Python `zlib.decompress` of both `-z` outputs equals the plain output. Default level (6) matches upstream `Z_DEFAULT_COMPRESSION`; reasonable.

## Architecture assessment
- Writer stack `isrout.Writer (64 KiB bufio) -> zlib.Writer -> file|stdout` is minimal; zlib receives large chunks. Close order isrout -> zlib -> file, first error wins, file always closed, idempotent (`o.w, o.zw, o.f = nil`). The early close for `-Z` on stdout reuses the idempotency cleanly.
- `env.create` sits next to the existing injected fields (stdin/stdout/tempDir); nil falls back to `os.Create`, `main.go` does not set it. `output.f` becomes `io.WriteCloser`, no other production effect. Acceptable and minimal.
- `build.sh zlib` is not part of `all` (local `./build.sh` unchanged); `-lz` via `ADDLIBS`; smoke check verifies usage line and 0x78 header. CI step is placed so a missing zlib fails the job. Pinned action SHAs unchanged, no permission changes, upload paths extended only.

## Findings
| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| A1 | LOW | confirmed | README "Differences from upstream" still says output deviations "(O1-O11)"; O12 was added (docs/cli.md was updated to O1-O12). | `README.md:1184` |
| A2 | LOW | confirmed | `docs/PROGRESS.md` still lists `-z` (#22) as open; AGENTS.md asks to keep it current. | `docs/PROGRESS.md:48-49` |
| A3 | LOW | confirmed | The new `-z` bullet sits under README "## Limitations" though it describes an added feature; reads oddly next to the "Not ported" bullet. Move it (e.g. to "Differences from upstream") or drop it (the CLI section already documents `-z`). | `README.md:1235-1237` |
| A4 | LOW | hypothesis | `docs/feature-matrix.md:74` "34 options" counts upstream's default build; the Go CLI now has one more. Verify and add "(+ `-z` with USE_ZLIB)" if so. | `docs/feature-matrix.md:74` |

No Critical, High or Medium findings in this specialization.

## Fixes requested
A1-A3 (doc edits); A4 check. No retest needed beyond `go test ./cmd/fpgrowth` if goldens are untouched.

## Decision
ACCEPT WITH CONDITIONS (Low only: A1-A4).
