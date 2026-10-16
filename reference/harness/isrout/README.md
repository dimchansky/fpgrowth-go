# Output layer harness (`internal/isrout` golden files)

`gen.sh` regenerates every fixture under `internal/isrout/testdata`:

```bash
(cd reference && ./build.sh release)          # the reference executable
reference/harness/isrout/gen.sh BUILD_DIR     # BUILD_DIR: scratch, outside the repo
reference/harness/isrout/gen.sh BUILD_DIR /path/to/fpgrowth
```

## Number formatting (`numdump.c`)

`numdump` links the pristine `report.c` compiled exactly like fpgrowth's
`repcm.o` (`-DISR_PATSPEC -DISR_CLOMAX`) and its dependencies, built out
of tree with the upstream release flags plus `-ffp-contract=off`
(diagnostics go to `BUILD_DIR/build.log`). It calls `isr_numout` on a
hand-made `ISREPORT` and reads the result back from the reporter's
private buffer.

- `numdump corpus` → `testdata/numout.txt`: a deterministic corpus
  (splitmix64, fixed seed) of `<IEEE-754 bits> <digits> <output>` lines:
  special values (0, -0, nan, ±inf, subnormals, `DBL_MIN`, `DBL_MAX`) in
  all digit counts 0..33, the audit's examples (report.md §5.6), powers of
  ten 1e-323..1e308 and their neighbours, rounding boundaries `9…95`,
  exact decimal ties `m·10^q/2^j` at their tie precision, `a/b` and
  `100·(a/b)` as the CLI computes them, `n + 0.5`, log-uniform values and
  random bit patterns. `internal/isrout` requires bit-exact equality
  (`TestAppendNumCorpus`).
- `numdump random SEED N`: about N further random cases of the same
  families, for large uncommitted checks:
  `ISROUT_NUMOUT_EXTRA=file go test ./internal/isrout -run Extra`.
- `numdump gfmt` → `testdata/gfmt.txt`: C's `printf("%g")` of pattern
  spectrum counts (`psp_report`), checked by `TestAppendGCorpus`.

## CLI-shaped golden outputs

For every case of `internal/isrout/testdata/golden/cases.txt` (input file
and fpgrowth arguments, one per `arg` line) `gen.sh` runs the reference
executable twice:

- `NAME.c.out`: with the case's arguments;
- `NAME.m.out`: with the same mining arguments, but the format options
  (`-v -k -I -h -g`) replaced by a machine-readable format
  (`-k'|' -I' => '` and `-v' # %a %17w %17e %Q'` for sets,
  `-v' # %a %b %h %17e %Q'` for rules; 17 digits round-trip the doubles);
  its `-Z` output is `NAME.z` and, for sets, its `-P` output `NAME.psp`.

`TestGolden` feeds the values of `NAME.m.out` through a `Writer`
configured like the first run and requires `NAME.c.out` byte for byte, and
the `-Z`/`-P` writers for the per-size and per-(size, support) counts of
the same run. Cases marked with a deviation id expect `NAME.go.out`
(written by `ISROUT_UPDATE=1 go test ./internal/isrout -run Golden`,
reviewed against `NAME.c.out`), which must differ from `NAME.c.out`.

None of the cases uses a configuration in which upstream's fast output
path would add the spurious empty set (report.md B3): no item of an input
occurs in every transaction where fast output is active.

Golden files in the repository were produced on macOS arm64 with Apple
clang 21 (`cc`).
