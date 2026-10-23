# fpgpsp harness (`internal/psp` golden files)

Two small C programs pin the parts of the `fpgpsp` port that do not go
through mining, so that they can be compared bit for bit without the
differential test:

- `rngdump.c` prints sequences of upstream's random number generators
  (`util/src/random.c`): `rng_uint` and `rng_dbl` of `rng_create(seed)`,
  and `urand` and `drand` after `rseed(seed)`, for 13 seeds. Golden file:
  `internal/psp/testdata/rng.c.out` (`TestRNG_matchesC`).
- `surrdump.c` reads a transaction file like `fpgpsp`'s `main`, prepares
  it like `fpg_genpsp` (with the oracle patches), and prints the prepared
  transaction array, the item codes and every surrogate of one chain
  (the single-threaded loop of `fpg_genpsp`, including the sorting of
  each surrogate by `fpg_data(FPG_SURR)`). The cases are listed in
  `internal/psp/testdata/surr/cases.txt`; golden files
  `internal/psp/testdata/surr/<case>.c.out` (`TestChain_matchesC`).

```bash
reference/harness/fpgpsp/gen.sh BUILD_DIR
```

`gen.sh` copies the upstream tree into `BUILD_DIR/src`, applies
`reference/patches/fpgpsp/*.patch` (the `fpgpsp-fixed` oracle), builds the
objects of the `fpgpsp` program with its makefile and the flags of
`build.sh fpgpsp` (`-ffp-contract=off` added), links both harness
programs and rewrites the golden files.

Golden files in the repository were produced on macOS arm64 with Apple
clang 21 (`cc`). The generators use integer arithmetic and products with
2^-32, which are exact, so the files do not depend on the platform.
