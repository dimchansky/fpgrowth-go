# Fixture harness for `internal/tract`

This directory regenerates the fixtures that `internal/tract` replays in
plain `go test` (no C compiler needed there). They are produced by the
pristine upstream sources in `reference/upstream/fpgrowth-6.21`, linked
the way the `fpgrowth` program links them.

```bash
reference/harness/tract/run.sh                  # temporary build directory
BUILDDIR=/some/dir reference/harness/tract/run.sh
CC=gcc reference/harness/tract/run.sh
```

Requirements: a C99 compiler (tested with Apple clang 21.0.0, arm64) and
bash. `run.sh` replaces `internal/tract/testdata/{inputs,expected}` and
`internal/tract/testdata/qsort.txt`; the output only depends on the
upstream sources and the files in this directory.

## Files

| File | Purpose |
| --- | --- |
| `run.sh` | builds the programs, writes the inputs, runs every case |
| `tractdump.c` | performs `fpg_data`'s data preparation and dumps the result |
| `qsortdump.c` | sorts pseudo-random item arrays with `ptr_qsort` (tie order) |
| `gen.c` | deterministic random transaction databases (64-bit LCG) |

## Build

The objects are those `fpgrowth` links for reading and preparing
transactions (`fpgrowth/src/makefile`, `util/src/makefile`,
`tract/src/makefile`): `arrays.o`, `idmap.o` (`symtab.c -DIDMAPFN`),
`escape.o`, `tabread.o` and `taread.o` (`tract.c -DTA_READ`). Every
object uses the release flags of the reference build
(`-std=c99 -O3 -DNDEBUG -funroll-loops -ffp-contract=off`), so assertions
are off as in the real program. Nothing is compiled in the upstream tree.

## What `tractdump` does

Options mirror `fpgrowth`: `-r -f -b -C` (characters, escape-decoded by
`trd_allchs`), `-w` (weights), `-R file` (`ib_readsel`), `-A file`
(`ib_readapp`, i.e. `-R` with target `r`), `-s#` (absolute minimum
support `fpg->supp`), `-q#`, `-o` (`FPG_REORDER` is set), `-m#` (`zmin`)
and `-l#` (pack count after `fpg_data`'s clamping). The steps are those of
`fpgrowth.c` `main` and `fpg_data`:

1. `ib_create(0, 0)`, `trd_allchs(tread, r, f, b, "", C)`, optional
   `ib_readsel`/`ib_readapp`, `tbg_read(bag, tread, -w ? TA_WEIGHT : 0)`;
2. `sort = sign(sort)` if `-o`; `tbg_recode(bag, smin, -1, -1, -sort)`;
3. `tbg_filter(bag, zmin, NULL, 0)`, `tbg_itsort(bag, +1, 0)`,
   `tbg_sort(bag, +1, 0)`, `tbg_reduce(bag, 0)`;
4. if `-l` > 0: `tbg_pack(bag, pack)`;
5. `tbg_ifrqs(bag, 0)`.

Output (all numbers `%d`, names last on the line):

```
read items <ib_cnt> records <tbg_cnt> wgt <tbg_wgt> maxfrq <ib_maxfrq> defapp <ib_getapp(-1)>
item <id> <app> <frq> <xfq> <name>          one per item, identifier order
recode <n>                                  result of tbg_recode
code <code> <app> <frq> <xfq> <name>        one per kept item, code order
bag cnt <tbg_cnt> records <records read> wgt <tbg_wgt> max <tbg_max> extent <tbg_extent>
ta <wgt> <size> <items...>                  bag after tbg_reduce, j = 0..cnt-1
pack <n>                                    only with -l > 0, then
pta <wgt> <size> <entries...>               bag after tbg_pack (packed entry = TA_END|bits)
ifrqs <f0> <f1> ...                         tbg_ifrqs(bag, 0)
end
```

`run.sh` prefixes each block with
`case <input> fmt=<f> w=<0|1> sel=<file|-> smin=<s> q=<q> reorder=<0|1> zmin=<z> pack=<p>`
and appends it to `testdata/expected/<set>.txt`. The Go test
(`internal/tract/bag_upstream_test.go`) reads the same input with a
test-only port of `trd_read`, runs `ItemBase`/`Bag` with the same
parameters and renders the same text.

## Inputs and cases

- The upstream examples `fpgrowth/ex/test1.tab` … `test6.tab` and
  `test1.csv` (copied unchanged), read with the separators the upstream
  documentation gives: `test2.tab` with `-f",.;:"`, `test3.tab` with
  `-w`, `test1.csv` with `-b,`, `test6.tab` both in the default format
  (empty lines = empty transactions) and with `-r% -f\n -b\n` (one
  record). `test1.tab` is also run with a selection and an appearance
  file.
- `r01`–`r12`: `gen` output designed for frequency ties (few items),
  zero weights, more than 16 items (packing), heavy record repetition
  (merging), duplicates within records, empty records, skewed frequencies,
  and separator/blank/comment/CRLF noise. `r02` also runs with a
  selection (`r02.sel`, includes an absent item), `r10` with an
  appearance file (`r10.app`, abbreviated indicators and an ignored item).
- `h01`–`h07`: hand-written edge cases: a single record of weight 0, a
  single record, no records, only empty records, prefix chains,
  zero-weight groups, all weights zero.

Each set runs every `-q` mode (0, ±1, ±2, ±3; `-o` on and off for |q| ≥ 2)
for two minimum supports, cycling `zmin` through 0–3 and the pack count
through 0, 4, 16 (`run.sh` `cases`). `qsort.txt` holds 300 arrays (1 to
600 items, many ties, 0–60 % removed items) sorted with `ptr_qsort` and the
`descmp`/`asccmp`/`nocmp` logic; `sort_test.go` replays them.

## Upstream behavior the fixtures pin down

- Records emptied by `zmin` (`tbg_filter`) stay in the bag with their
  weight; they count in the total weight and in `tbg_cnt` before
  `tbg_reduce`, and merge with all other empty transactions into one.
- `tbg_reduce` deletes merged transactions of total weight 0, but only
  if the bag holds at least two transactions, and does not recompute
  `tbg_max`.
- Packed entries sit at the position of the first (smallest) item, the
  bag is not sorted again, `size` is unchanged, and `tbg_ifrqs` counts a
  packed entry under item 0.
