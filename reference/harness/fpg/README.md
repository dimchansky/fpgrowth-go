# FP-growth core harness (`internal/fpg` golden files)

`gen.sh` runs the reference `fpgrowth` executable (release variant, built
by `reference/build.sh release` with `-ffp-contract=off`) over every case
of `internal/fpg/testdata/golden/cases.txt` and writes the exact output of
each case, in order, to `internal/fpg/testdata/golden/expected.txt`:

```bash
FPG_GOLDEN_GEN=1 go test ./internal/fpg -run TestGoldenGenerate  # data/r*.tab, data/b*.tab, cases.txt
reference/harness/fpg/gen.sh [FPGROWTH]                         # expected.txt
go test ./internal/fpg -run 'TestGolden$'                       # replay
```

Every case runs `fpgrowth <options> [-w] "-v [%a]" <dataset> -`. The info
format `" [%a]"` avoids upstream's fast output path (which prints a
spurious empty set, fpgrowth-core.md B6) while showing the absolute
support; `-w` is added for `*.w.tab` datasets. `expected.txt` holds, per
case, a header `### <dataset> | <options>`, the exit status (`exit 0`,
`exit 15` for "no (frequent) items found", `exit 139` for a crash) and the
item set lines.

Datasets (`internal/fpg/testdata/golden/data`):

- `test1.tab`, `test3.w.tab`: copies of the upstream examples
  `fpgrowth/ex/test1.tab` and `test3.tab` (made by `gen.sh`).
- `r00.tab` … `r15*.tab`: random datasets from `oracle.Generate`
  (`r15` has 19 items, so the 16-items machine and the tree paths of
  `-Ac` both occur).
- `b1a`, `b1b`, `b3`, `b4`, `m9`, `b5c`, `b5m`, `b5x`: reproducers of the
  upstream bugs fixed by the port (fpgrowth-core.md §6 B1, B3, B4, B5 and
  reference-build.md F3/M9).

The cases cover the variants `-As -l0`, `-As -l3`, `-As`, `-Ac`, `-Ac -j`,
`-Ac -l0`, `-Ac -j -l0`, `-Ad -l0`, `-Ad -l3`, `-Ad`, `-At` for the targets
`-ts -tc -tm -tg`, each with four option sets rotated from `-x`, `-q±1`,
`-q0`, `-q-2`, `-m0`, `-m2`, `-m3`, `-n2`, `-n3`, `-u`, `-S60` and
`-eb -d20`.

`TestGolden` requires the Go output to equal the C output line by line,
except for the cases listed in `testdata/golden/deviations.txt` with the
id of the fixed upstream bug. For those it requires the Go result to equal
the brute-force oracle and the difference to have the documented shape
(B1: C crashes or prints wrong items; B3, B4: the C lines are a proper
subsequence of the Go lines; B5: the Go lines are a proper subsequence of
the C lines). When regenerating, new mismatches must be classified there
by hand.

## Rules path (`tree_*` files)

`tree_cases.txt` (written by `FPG_GOLDEN_GEN=1 go test ./internal/fpg -run
TestTreeGoldenGenerate`, together with the appearance files
`data/app*.app`) lists cases of the item set tree path: association rules
(`-tr`) with all `-A` letters, `-q` orders, confidence, sizes, `-o`, every
rule measure letter with and without `-i`, item sets of every target with
rule measures and aggregation modes, and the documented deviations (`-eb`,
`-S`, `-p`, `-R`). `gen.sh` writes `tree_expected.txt` in the same format
as `expected.txt`, running rules with `-v' [%a %b %h %e]'` and item sets
with `-v' [%a %e]'`. `TestTreeGolden` replays them through `internal/isrout`
(the same text output as the CLI) and requires identical lines, except for
`testdata/golden/tree_deviations.txt` (R1, R2, R3, R4, R8, R9 of
`docs/differences.md`), where it requires a difference of the documented
shape and, where the oracle models it, equality with the oracle.

Golden files in the repository were produced on macOS arm64 with Apple
clang 21 (`cc`).
