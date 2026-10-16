# Reporter harness (`internal/report` golden files)

`isrreplay.c` replays scripted item set reporter call sequences against
the pristine upstream `report.c` (compiled exactly like `repcm.o` for
fpgrowth: `-DISR_PATSPEC -DISR_CLOMAX`), `clomax.c` and `patspec.c`, and
prints every decision of the reporter: reported item sets and rules (with
support, weight and evaluation as `%.17g`), call results, the per-size
counts (`isr_prstats`) and the raw pattern spectrum. The script language
is documented at the top of `isrreplay.c`; the Go test interpreter is
`internal/report/report_script_test.go`.

```bash
reference/harness/report/gen.sh BUILD_DIR            # all scripts
reference/harness/report/gen.sh BUILD_DIR a.isr ...  # selected scripts
```

`gen.sh` compiles the upstream objects out of tree into `BUILD_DIR` with
the release flags of the upstream makefiles plus `-ffp-contract=off`
(compiler diagnostics go to `BUILD_DIR/build.log`), links `isrreplay`, and
writes `<name>.c.out` next to every `<name>.isr` under
`internal/report/testdata`.

Scripts:

- `testdata/scenarios/*.isr`: hand-written edge cases.
- `testdata/sim/*.isr`, `testdata/random/*.isr`: produced by a simulated
  FP-growth/istree traversal (`internal/report/report_sim_test.go`);
  regenerate with
  `REPORT_UPDATE=1 go test ./internal/report -run TestUpdateScripts`,
  then run `gen.sh`.

A script that exercises a documented deviation contains a
`# deviation: <id>` line; its expected Go output is `<name>.go.out`
(written by `TestUpdateScripts`, reviewed against `<name>.c.out`). The Go
test requires Go = `.c.out` for all other scripts (evaluations within a
relative 1e-12 for libm differences) and Go = `.go.out` ≠ `.c.out` for
deviation scripts.

Golden files in the repository were produced on macOS arm64 with Apple
clang 21 (`cc`).
