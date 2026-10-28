# Repeated final integration audit — correctness, API compatibility, packaging

- Main reviewed: 37d33b3 vs e6d02b0 (first audit) and v0.1.0; docs PR #51 at c48c4d5. 99 files changed since e6d02b0 (+8253/−336).
- Date: 2026-10-28, 22:14–22:31 (night window; heavy steps under the RSS watchdog, GOMAXPROCS=2, one at a time).
- Decision: READY WITH CONDITIONS (maintainer's D7 sign-off; performance auditor's verdict; Low doc fixes B-1, B-2).
- Recorded by the orchestrator from the auditor's final message.

## Commands and results

1. Root module, main 37d33b3: `gofmt -l` empty; `go vet`, `staticcheck`, `CGO_ENABLED=0 go build` pass; `govulncheck`: no vulnerabilities; `go mod tidy -diff` clean; no cgo imports or C files outside `reference/`; `go list -deps -test ./...` stdlib only; `go.mod`/`go.sum` unchanged since v0.1.0 (go 1.26.0, toolchain go1.27.2, no requirements). `go test -count=1 -p 2 ./...` rc=0 (14 packages, 29 s); `go test -race -p 2 ./...` rc=0 (112 s); linux/386 and linux/arm vet and build ok, `GOOS=linux GOARCH=386 go test -c ./internal/ruleval` ok. CI green on every merge #43–#50.
2. `reference/`: `./build.sh all zlib fpgpsp fpgpsp-fixed` rc=0; vet and staticcheck ok; `go test -count=1 -p 1 -parallel 1 ./...` rc=0 (106 s, 0 skips): TestDifferential 10,000 cases, TestLarge 300, TestFpgpsp 3,000/3,000 byte-equal (893 estimations), TestZlib 41 fixed + 1,000 random, TestHarnessGaps pass. Extra run with `DIFFTEST_SEED=20261028` rc=0: TestDifferential 3,000, TestFpgpsp 1,000/1,000 byte-equal (313 estimations), TestZlib 300.
3. API: exported declarations of the root package (go/ast dump: signatures, exported fields, consts, vars) identical between v0.1.0 and main (111 lines each). The only root-package source change since e6d02b0 is the `measure.go` doc comment (Information accuracy 32 → 64 ulps; v0.1.0 had no accuracy paragraph). CHANGELOG `[0.1.0]` has the same sha256 in v0.1.0, main and PR #51. No CLI golden or testdata file and no CLI message or usage string changed since e6d02b0. The one CLI behavior change is #48 (no "input changed while reading" within the 128 MiB budget), documented in CHANGELOG, D8, `docs/cli.md` and the C9 note, tested by `TestItemBase_Retain_*` and `TestGoldenRetention`. #40's value changes are in CHANGELOG and `docs/differences.md`. Every test the current docs name exists. `reference/upstream` unchanged.
4. Packaging: `goreleaser check` valid; `goreleaser release --snapshot --clean --skip=publish --parallelism 2` rc=0, 6 archives, each with fpgrowth and fpgpsp, LICENSE, README, CHANGELOG, PROVENANCE; checksums 6 lines; darwin/arm64 binaries run. No tag, nothing published.
5. Integration probes (scratch):
   - #48 retention, fpgrowth: 10,000 random inputs (weights or plain, custom `-r`/`-f`, comments, file or stdin, all targets and variants, `-n`, `-Z`), each with 5 budgets (default, off, 1, random, just above the size): stdout, normalized stderr and exit code identical (9,086 with output, 4,994 from stdin). fpgpsp: 3,000 cases (`-gi -gr -gp -ge`, `-Z1/2`, `-D`), same budgets: identical.
   - Real binaries vs the C release build: 600k records (within budget) and 9M records (149 MB, over budget) with `-s1 -n3`, `-s0.5 -tr -c50 -n2 -Z`, `-tm -s2`, file and stdin: byte-equal. Max RSS Go/C: 32/44 MB (small), 142/530 MB (big).
   - #34/#42 shapes (`-tm`/`-tc` with `-n`) vs the brute-force oracle: 5,000 datasets default build and 5,000 with `-tags fpgforce`, 275,000 runs each, all variants, `-tm` knob combinations (mguard 4/−1/1; lookup auto/always/never; sibling rows auto/force+verify/at+verify): PASS (1,324,788 deferrals, 702,945 lookups, 68,401 capped deferrals exercised).
6. Documentation and PR #51: PROGRESS table merge SHAs, roadmap ticks, review decisions, 19.8 s / 39.1 s (`docs/benchmarks.md`), 3.1× / 29 % (`issue-48-review-performance.md`) and the fpgpsp banner all verified; PR #51 checks green; A-1 to A-4 fixed by PR #51.
7. Next version: `[Unreleased]` adds features (`fpgpsp`, `-z`), so under the v0 rules of `docs/releasing.md` the next release is a minor (v0.X.0); the exported API has no incompatible change. No number chosen; nothing tagged.

## Findings

| ID | Sev | Status | Summary | Resolution |
|---|---|---|---|---|
| B-1 | Low | confirmed | CHANGELOG `[Unreleased]` ALG-1 entry still said the information is within 32 ulps; #40 widened it to 64 ulps (2^-46). | Fixed in PR #51 (1111a12, 7597d47). |
| B-2 | Low | confirmed (PR #51) | PR #51's `docs/releasing.md` said fpgpsp's defects U1-U25 are fixed; only U1-U20 and U25 are fixed defects (U6/U21/U24 new behavior, U22-U23 platform, U9 latent, U10 preserved). | Fixed in PR #51 (1111a12). |
| B-3 | Info | confirmed | README's 19.8 s for the M5A-1 case dates from #19; #42 later cut that row's instructions by 8.1 %. | Left as a lower-bound statement unless the performance audit re-times it. |
| B-4 | Info | expected | D7 awaits the maintainer's sign-off; D8 approved. | Maintainer action. |
| B-5 | Info | carried over | The first audit's A-5 to A-8 were addressed by #29, #34, #40, #39, #47 (reviews accepted). | — |

Cleanup: clone, v0.1.0 worktree, binaries, C builds, `dist/` and generated data deleted; 112 KB of logs kept in `~/.cache/fpgrowth-cycle/final2/audit-correctness/`.
