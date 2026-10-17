# Command-line harness (`cmd/fpgrowth` golden files)

`gen.sh` runs the reference `fpgrowth` executable (release variant, built
by `reference/build.sh release`) for every case of
`cmd/fpgrowth/testdata/cases.txt` and stores its observable behavior:

```bash
reference/build.sh release                 # once
reference/harness/cli/gen.sh [FPGROWTH]    # data/ and golden/
go test ./cmd/fpgrowth -run TestGolden     # replay (no C needed)
go test ./cmd/fpgrowth -run TestGolden -update   # rewrite golden-go/
```

- `cases.txt`: one case per line, `<name> <shell words>`. The words are
  evaluated by bash, so quoting works as on a command line; `<file`
  redirects stdin (otherwise stdin is `/dev/null`) and `>&-` closes
  stdout. Case names must be unique ignoring case (macOS file systems).
- `data/`: the datasets, written by `gen.sh`: the upstream examples
  `test1.tab`, `test2.tab`, `test3.tab`, `test1.csv`, `test6pct.tab`
  (`test6.tab` with the blank lines replaced by `%`), a few datasets of
  `internal/fpg/testdata/golden/data`, and small files that provoke
  errors and upstream bugs (selection/appearance files, weights, escapes,
  long names, ...).
- `golden/<name>.stdout`, `.stderr`, `.exit`: the C program's output,
  run in `data/` with `argv[0]` set to `fpgrowth` (`exec -a`). Times in
  log messages (`[0.00s]`) are replaced by `[T]`; the Go test normalizes
  its own output the same way.

`TestGolden` runs every case through the program's `run` function (same
arguments, stdin and working directory) and requires stdout, stderr and
the exit status to be identical. Cases listed in
`cmd/fpgrowth/testdata/deviations.txt` (with the id of
`docs/differences.md`, or `NaN` for option values the port rejects) must
differ from the C output and equal the reviewed Go output in
`cmd/fpgrowth/testdata/golden-go/`, which `-update` rewrites; review its
diff against `golden/` before committing.

Golden files in the repository were produced on macOS arm64 with Apple
clang 21 (`cc`). Three cases crash the C program (exit 138/139: `dev-bigw`,
`dev-prefmt`, `dev-At`); their exit status is platform dependent.
