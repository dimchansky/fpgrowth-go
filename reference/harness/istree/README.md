# Item set tree harness (`internal/istree` golden files)

`gen.sh` runs the C reference `fpgrowth` on the item set tree path
(association rules `-tr`, and item sets evaluated with a rule measure
`-e<letter>`) for every case in `internal/istree/testdata/cases.txt` and
writes the output, in output order, to
`internal/istree/testdata/golden/<case>.out`. Rules are printed with
`-v ' %17a %17b %17h %17e'` (rule support, body support, head support,
measure value) and item sets with `-v ' %17a %17e'`; 17 significant digits
make every double round-trip.

```bash
reference/build.sh release                       # pristine oracle (once)
reference/harness/istree/gen.sh BUILD_DIR          # all cases
reference/harness/istree/gen.sh BUILD_DIR t1-c-el  # selected cases
```

Each case names a build:

- `c`: the pristine release reference (`$FPG`, default
  `reference/build/release/fpgrowth/src/fpgrowth`).
- `p`: an out-of-tree copy of the upstream sources in `BUILD_DIR/patched`
  with `reference/patches/istree/fpgrowth-p-order.patch` applied, built
  with the same release flags (`-O3 -DNDEBUG -funroll-loops
  -ffp-contract=off`). The patch fixes rules-eval.md bug B2: `ist_seteval`
  is called before `ist_filter`, `ist_filter` runs for every `-p` value
  (positive values select its strong forward branch), and the generator
  filter compares items with the total weight without the skip flag that
  `ist_filter` sets on the empty set (without that hunk, `-tg` with `-p`
  reports nothing). The patch lives in a subdirectory of
  `reference/patches/` on purpose: `reference/build.sh` applies only
  `reference/patches/*.patch`, so the oracle builds stay pristine.

The datasets are in `internal/istree/testdata/data`: `t1.tab` and `t3.tab`
are upstream's `fpgrowth/ex/test1.tab` and `test3.tab` (weighted, `-w`),
`r1.tab`..`r4.tab` are random (`ISTREE_UPDATE_DATA=1 go test
./internal/istree -run TestGolden_WriteData`), and `*.app` are appearance
files for `-R`.

The Go test (`TestGolden` in `internal/istree/report_golden_test.go`)
recodes the items with `tract.ItemBase.Recode` exactly like `fpg_data` on
this path (threshold: the set minimum support; sort order: the sign of
`-q`, or `-q` itself for closed and maximal targets, which clear
`FPG_REORDER`), fills the tree from brute-force supports in `rec_tree`'s
order, reports through `internal/report`, and requires every line in the
same order with equal items and supports. Measure values must be equal,
except for the measures that use `log`/`exp` (`j p t i g f h m s`), which
may differ by a relative 1e-12 (libm versus Go `math`).

Cases deliberately avoid the behavior the port changes (the cases file
lists them): `-S` with rules (B8), head-only appearances (B3), `-eb` (B1),
`-p` on the pristine build (B2) and the measures with fixed `ruleval`
bugs. These are covered by the oracle tests in `internal/istree`.
Checked when the files were generated: for every `p` case the pristine
build prints the same output with and without `-p`, while the patched
build differs; with rules, `-p` changes nothing in either build.

Golden files in the repository were produced on macOS arm64 with Apple
clang 21 (`cc`).
