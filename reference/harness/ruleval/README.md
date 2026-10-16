# ruleval fixture harness

`harness.c` links the pristine upstream `math/src/ruleval.c`, `gamma.c`
and `chi2.c` (FP-Growth 6.21) and prints the fixtures that
`internal/ruleval` is tested against:

| Fixture | Contents |
| --- | --- |
| `internal/ruleval/testdata/measures_c.txt` | one line per table `supp body head base`, then the values of all 26 measures (ids 0..25, called through `re_function(id)` like `istree.c` does); `-` where a Fisher test was skipped |
| `internal/ruleval/testdata/specfun_c.txt` | `lg x logGamma(x)` and `q x chi2cdfQ(x, 1)` |

All values are printed with `%.17g`, which round-trips IEEE 754 doubles.

## Regenerating

```bash
reference/harness/ruleval/gen.sh /path/to/scratch/build
```

`gen.sh` checks the three upstream sources (and `tract.h`) against
`reference/upstream/SHA256SUMS`, compiles them and the harness into the
given build directory (outside the repository), checks on arm64 that the
objects contain no fused multiply-add instruction, and rewrites both
fixtures. Each fixture starts with `#` lines recording the platform, the
compiler and the flags.

The committed fixtures were generated with

- Apple clang version 21.0.0 (clang-2100.3.34.2), Darwin 25.6.0 arm64
  (Apple libm),
- flags `-std=c99 -O3 -funroll-loops -DNDEBUG -ffp-contract=off`: the
  upstream release flags (`math/src/makefile`) plus `-ffp-contract=off`,
  like the oracle variants of `reference/build.sh`. Without it clang fuses
  `a*b+c` into FMA instructions and the last bits of the chi^2, Kulczynski,
  information, gamma and Fisher values change; the Go port prevents fusion
  with explicit `float64(...)` conversions.

`-DNDEBUG` matters: upstream's debug build aborts in `GammaQ` on the
slightly negative information values of bug B7.

## Tuple grid (`measures`)

Deterministic (splitmix64, fixed seed), 2461 tables, all valid
(`0 <= supp <= min(body, head)`, `body + head - supp <= base`, base < 2^31
because upstream's `SUPP` is `int`):

1. every table with base <= 9 (all degenerate cases: empty margins,
   full margins, empty body support, ...);
2. boundary tables (empty/full margins, supp = 0/1/n-1, halves, ...) for
   bases 2, 3, 170, 171, 172 (the logGamma table/Lanczos boundary), 1000,
   65536, 1000003, 2^24, 123456789, 2^30, 2^31-1;
3. 500 uniformly random tables, base log-uniform up to 2^31-1;
4. 500 random tables with one margin <= 2000 and base up to 2^21, so that
   the Fisher tests are evaluated (half with supp near the expectation);
5. 300 tables near independence (supp = floor(body·head/base) ± 1) for
   the chi^2, Yates and information cancellation cases (B7, B10);
6. 200 tables with a margin of exactly base/2 (tied Fisher tables, B6);
7. the reproducers of docs/upstream/rules-eval.md (B4, B5, B6, B7, B9,
   B10).

A Fisher test is evaluated only if it sums at most 20000 tables
(`FCAP`); otherwise its column is `-`.

## Special functions (`specfun`)

`logGamma(k)` and `logGamma(k + 1/2)` for k <= 300 (tables, the 170/171
boundary and the Lanczos formula; `logGamma(170.5)` shows bug B11), a few
non-half arguments, and `chi2cdfQ(x, 1)` for x = 0, 0.025, ..., 25 plus
tiny and large x (series, continued fraction and underflow).
