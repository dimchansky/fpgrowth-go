# tabread harness: upstream table reader dumps

Produces the golden files of `internal/tabread`:

- `testdata/golden.txt`: for every case of `testdata/cases.txt`, the dump
  of upstream's reader in the case's mode (`trans` = the `ib_read` loop of
  `tbg_read`, `sel` = `ib_readsel`, `app` = `ib_readapp`) and in mode
  `fields` (raw `trd_read` calls);
- `testdata/escapes.golden`: `esc_decstr`/`esc_decode` results for
  `testdata/escapes.txt`.

```bash
reference/harness/tabread/gen.sh /path/to/scratch/build   # any directory outside the repo
```

`gen.sh` compiles the pristine upstream sources (`util/src/tabread.c`,
`escape.c`, `symtab.c` with `-DIDMAPFN`, `arrays.c`, `memsys.c`,
`tract/src/tract.c` with `-DTA_READ`) plus `tabdump.c` and `escdump.c`
into the build directory (`-std=c99 -O1 -g`, assertions on; override the
compiler with `CC`) and runs them from the testdata directory, so file
names in messages are the manifest's relative names. Nothing under
`reference/upstream` is modified. Option values in the manifest are
passed to `trd_allchs` exactly as fpgrowth's `main()` does (escape
decoding by upstream, empty null-value class).

Output formats are documented at the top of `tabdump.c` and `escdump.c`.
`escdump` pads strings with extra NULs (column `S`) or `"\0Z\0"` (column
`Z`) so that upstream's read past the end of a trailing backslash is
deterministic and visible. Inputs that would trigger upstream's
out-of-bounds class-table write (literal bytes >= 0x80 in `-r/-f/-b/-C`,
cli-io.md B4) are deliberately not passed to `tabdump`.

The Go test (`golden_test.go`) compares every section; sections where the
Go port deviates on purpose are listed with their expected Go output in
`goldenDeviations`, and the test fails if such an entry no longer differs
from upstream. Committed results: macOS 26 arm64, Apple clang 21.
