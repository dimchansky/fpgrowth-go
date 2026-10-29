# Repeated final integration audit 3 — correctness, API, release readiness and docs (main a5f548c)

- Decision: READY WITH CONDITIONS. No Critical, High or Medium finding.
  Conditions: the maintainer's D7 sign-off (carried over) and the
  `docs/PROGRESS.md` update (C3-1).
- Scope: `9f1aa62..a5f548c`, that is #53 (PR #54, test only) and #52
  (PR #55), judged against v0.1.0 where they interact. 19 files,
  +1083/−102.
- Run on 2026-10-29, 22:56–23:10, in the night window, at the same time
  as the performance audit. Load average 2.6–6.2. GOMAXPROCS=2, heavy
  steps one at a time under the RSS watchdog, reference tests with
  `-p 1 -parallel 1`.
- Recorded by the orchestrator from the auditor's final message.

## Resolution of findings

- C3-1 (Low), fixed in this PR. `docs/PROGRESS.md` now:
  - has the 2026-10-29 status and says all 19 tasks are merged;
  - has rows for #53 and #52;
  - records this third audit run;
  - replaces the resolved "possible follow-up" with what is still open
    (the input over the budget is still read twice) and with the test
    limits the #53 reviews accepted.
- C3-2 (Low), fixed in this PR. The "What remains" note of the #48
  section in `docs/benchmarks.md` now says that #52 removed the string
  conversions of pass 2.
- C3-3 to C3-7 are informational; nothing was changed for them.

## Auditor's report

### Commands and results

1. **Resources.** 80 GiB of disk free; `vm_stat` was checked before
   starting.
2. **Root module at a5f548c, in a fresh clone:**
   - `gofmt -l` printed nothing.
   - `go vet`, `staticcheck` and `CGO_ENABLED=0 go build ./...` passed.
   - linux/386: `go vet`, `go build` and `go test -c ./internal/tract`
     passed.
   - `go mod tidy -diff` was clean, and `go.mod` and `go.sum` are
     unchanged since v0.1.0.
   - `go test ./...` passed (14 packages), and so did `go test -race
     ./...` (112 s).
3. **`reference/`:**
   - `./build.sh all zlib fpgpsp fpgpsp-fixed` succeeded, and vet and
     staticcheck passed.
   - `go test -p 1 -parallel 1 ./...` passed with 0 SKIP:

     | Test | Cases | Result |
     | --- | --- | --- |
     | TestDifferential | 10,000 | 0 FAIL |
     | TestLarge | 300 | 0 FAIL |
     | TestFpgpsp | 3,000 | all byte-equal |
     | TestZlib | 1,000 | 0 FAIL |
     | TestHarnessGaps and the modcheck tests | — | pass |
4. **API:**
   - apidiff from v0.1.0 to HEAD found no change in the root package. In
     module mode the only change is the compatible addition of
     `cmd/fpgpsp`.
   - A `go doc -all .` diff shows only doc-comment changes, all already
     recorded in the CHANGELOG. No signature changed.
   - Since 9f1aa62 no non-test file of the root package changed.
5. **Code review of #52:**
   - `BuildBag` and `BuildBagBytes` share the `pass2` helpers and
     `buildFromRetained`.
   - Error order and error text are preserved, and a record error still
     wins over a scan error.
   - The panic before `Recode` is kept, and retained records are still
     released.
   - The ignore path does exactly what `unknown` does.
   - The byte slices are used only as map keys inside the callback.
   - CLI wiring: `fpgrowth` uses only `ScanBytes`. `fpgpsp` uses the byte
     path for estimation and generation, and the string scan only for
     `-gs`.
   - The goldens cover the path over the budget in both CLIs.
6. **#53:** the allocation bound of `TestCancelTreeBuild` runs
   sequentially and its comment is accurate. Its limits (D1, F1) are
   documented.
7. **Integration probe.** The auditor ran 6,000 random `fpgrowth` runs
   from a scratch test, deleted afterwards:
   - options: targets `-ts`, `-tc`, `-tm`, `-tg`, `-tr`, together with
     `-w`, `-n`, `-m`, `-q`, `-R` selection and appearance files, and
     `-e` with `-c`;
   - input from a file and from stdin;
   - each run three ways: records retained (the default), not retained,
     and with a 1-byte budget.

   stdout, the normalized stderr and the exit code were identical in
   every run. 3,076 runs produced output and 1,807 exited non-zero.
8. **Release machinery:**
   - `goreleaser check` (v2.18.3) reported the configuration valid.
   - `goreleaser release --snapshot --clean --skip=publish` succeeded. It
     produced 6 archives (darwin, linux and windows on amd64 and arm64),
     each with fpgrowth, fpgpsp, LICENSE, README, CHANGELOG and
     PROVENANCE, plus the checksums.
   - The darwin/arm64 binaries ran correctly.
   - Nothing was tagged or published.
9. **CI:** main run 40952343740 at a5f548c passed 15/15.
10. **Docs:**
    - The CHANGELOG entry for #52 matches `docs/benchmarks.md`, and is
      consistent with the entry for #48.
    - The D8 note is accurate, and D7 is still pending.
    - `docs/differences.md` and `docs/feature-matrix.md` need no change.
    - README and `docs/releasing.md` are still correct.
    - The review records for #52 and #53 are present.

### Findings

| ID | Sev | Summary |
| --- | --- | --- |
| C3-1 | Low | `docs/PROGRESS.md` was out of date about #53 and #52. |
| C3-2 | Low | `docs/benchmarks.md` (#48, "What remains") still read as if pass 2 converted every name. |
| C3-3 | Info | #53 has no CHANGELOG entry, which is right for a test-only change. |
| C3-4 | Info | `BuildBagBytes` before `Recode` panics with BuildBag's message. It is internal only. |
| C3-5 | Info | The CHANGELOG entry for #48 has one unwrapped long line. Cosmetic. |
| C3-6 | Info | The goreleaser snapshot is labelled 0.1.1-snapshot (goreleaser's default). The real version is a minor and is the maintainer's choice. |
| C3-7 | Info | D7 is still awaiting the maintainer's sign-off. |
