# Reference build of upstream FP-Growth 6.21

This directory builds Christian Borgelt's unmodified C implementation
(`upstream/fpgrowth-6.21/`) into the `fpgrowth` executables that serve
as the oracle for differential tests of the Go port. Nothing here is
part of the published Go module.

```bash
cd reference
./build.sh                 # release, debug, asan and native
./build.sh release         # one or more of: release | debug | asan | native | zlib | all
./build.sh zlib            # release with USE_ZLIB (option -z); needs zlib
CC=gcc ./build.sh          # another compiler (the default is cc)
```

`release`, `debug` and `asan` are the oracle: they are compiled with
`-ffp-contract=off` (see [Floating-point contraction](#floating-point-contraction))
and must produce identical output. `native` is the upstream build with
no added flags, for performance comparisons only. `zlib` is `release`
compiled with `-DUSE_ZLIB` and linked with `-lz`, upstream's optional
build with option `-z` (zlib-compressed output); it is the oracle of
`difftest`'s `TestZlib` and is not part of `all`.

`build.sh` prints one `<variant> <absolute path>` line per binary on
stdout; progress goes to stderr. It exits non-zero if the pristine
check, a build or a post-build check fails.

## Requirements

| Tool | Notes |
| --- | --- |
| C99 compiler | `cc` by default; tested with Apple clang 21.0.0 (arm64). gcc works too (it is the upstream default). The asan variant needs a compiler with AddressSanitizer and UndefinedBehaviorSanitizer runtimes. |
| GNU make | Upstream makefiles need GNU make (recursive `cd dir; $(MAKE) x.o`). Tested with GNU Make 3.81 (macOS). `make` is used if it is GNU make, else `gmake`; override with `MAKE=`. |
| `/bin/bash` | The upstream makefiles set `SHELL = /bin/bash`. |
| `sha256sum` or `shasum` | For the pristine check (`sha256sum` on Linux, `shasum -a 256` on macOS). |
| `nm` | Optional; used to check that assertions and sanitizers took effect. |
| `patch` | Only if top-level `patches/*.patch` exist (currently there are none). |
| zlib | Only for the `zlib` variant: `zlib.h` and `libz` (part of the macOS SDK; `zlib1g-dev` on Debian/Ubuntu). `TestZlib` is skipped if they are missing. |

Environment variables: `CC` (default `cc`; `-std=c99` is appended
unless `CC` already contains `-std=`), `LD` (default: `CC`), `MAKE`,
and `ADDFLAGS` (the upstream makefiles' own hook for extra compiler and
linker flags, e.g. `-D_FILE_OFFSET_BITS=64`; empty by default).

## Variants

| Variant | `ADDFLAGS` | Compiler flags after `$(CFBASE)` | Linker flags | Assertions | Use |
| --- | --- | --- | --- | --- | --- |
| `release` | `-ffp-contract=off` | upstream default `-DNDEBUG -O3 -funroll-loops` (`CFLAGS` not overridden) | upstream default `$(ADDFLAGS)` | off | oracle |
| `debug` | `-ffp-contract=off` | `-g -O0` | upstream default `$(ADDFLAGS)` | on | oracle, assertions |
| `asan` | `-ffp-contract=off` | `-g -O1 -fsanitize=address,undefined -fno-omit-frame-pointer` | `$(ADDFLAGS) -g -fsanitize=address,undefined` | on | oracle, sanitizers |
| `native` | (empty) | upstream default `-DNDEBUG -O3 -funroll-loops` | upstream default | off | performance only |
| `zlib` | `-ffp-contract=off -DUSE_ZLIB` | as `release` | as `release`; `ADDLIBS=-lz` | off | oracle of `-z` (`TestZlib`) |

`$(CFBASE)` is the upstream warning set plus `-c` and `$(ADDFLAGS)`:
`-Wall -Wextra -Wno-unused-parameter -Wconversion -pedantic -c $(ADDFLAGS)`
(the `tract` makefile adds the GCC-only `-Wimplicit-fallthrough=0`).
A user-supplied `ADDFLAGS` is appended to `-ffp-contract=off` for the
oracle variants and used alone for `native`.

### Floating-point contraction

All oracle variants are compiled with `-ffp-contract=off`. C allows a
compiler to contract `a*b + c` into one fused multiply-add (FMA) with a
single rounding. Apple clang does so by default on arm64 (even at
`-O0`), while gcc on x86-64 without `-mfma` does not, so the same source
gives different last bits in evaluation measures (`-e`, `%e`), relative
supports and confidences (`%s`, `%S`, `%c`, `%C`, `%l`, ...) depending on
the platform. The Go compiler may fuse too (it does on arm64), but an
explicit conversion such as `float64(a*b) + c` forces the intermediate
rounding; the Go port is written to avoid fusion this way, so the oracle
must not fuse either.

`build.sh` checks that every compile command in `build.log` carries the
flag (and that `native` does not). To confirm the effect on arm64:

```bash
for v in native release debug asan; do
  printf '%s: ' $v
  otool -tv build/$v/fpgrowth/src/fpgrowth | grep -cE '\s(fmadd|fmsub|fnmadd|fnmsub)\s'
done
# Apple clang 21: native 46, release/debug/asan 0
```

How often fusion actually changes the program's output is measured in
`docs/upstream/reference-build.md` (release vs native over the battery).

Note that `debug` and `asan` are not just "release with symbols": without
`-DNDEBUG` upstream also enables extra code. Every item set flushes the
output buffer (`report.c`), and `main()` and `error()` free all memory
before exiting (`CLEANUP` in `fpgrowth.c`, `FPG_NOCLEAN` is 0). Output
bytes are identical across the variants for every run in the battery
except where a crash or an assertion stops a run (see
`docs/upstream/reference-build.md`).

## Where the binaries land

```
reference/build/release/fpgrowth/src/fpgrowth
reference/build/debug/fpgrowth/src/fpgrowth
reference/build/asan/fpgrowth/src/fpgrowth
reference/build/native/fpgrowth/src/fpgrowth
reference/build/zlib/fpgrowth/src/fpgrowth
```

Each `reference/build/<variant>/` directory is a full copy of
`upstream/fpgrowth-6.21/`, built in place. It also holds `build.log`
(the full compiler output) and `smoke.out`/`smoke.log` from the post-build
check. `reference/build/` is gitignored and recreated on every run; the
upstream directory is never written to.

## How the build works

1. **Pristine check.** `upstream/SHA256SUMS` lists every file under
   `upstream/fpgrowth-6.21/` (71 files, paths relative to `upstream/`).
   `build.sh` runs `sha256sum -c` (or `shasum -a 256 -c`) on it and also
   compares the file list, so a modified, missing or extra file stops the
   build (`.DS_Store` files are ignored). Regenerate the manifest only
   when the upstream import is deliberately replaced:

   ```bash
   cd reference/upstream
   find fpgrowth-6.21 -type f ! -name .DS_Store | LC_ALL=C sort |
     xargs shasum -a 256 > SHA256SUMS     # or: xargs sha256sum
   ```

2. **Out-of-tree copy.** The tree is copied to `build/<variant>/`.
   Any `patches/*.patch` would be applied to the copy with `patch -p1`.
   None are needed: all four variants compile from the pristine sources
   with clang, so there are no top-level patches. Patches in
   subdirectories (e.g. `patches/istree/fpgrowth-p-order.patch`, used to
   verify deviation R2) are never applied to the oracle builds.

3. **make.** `make -C build/<variant>/fpgrowth/src fpgrowth` with the
   variables `CC`, `LD`, `ADDFLAGS` (with `-ffp-contract=off` except for
   `native`), `ADDLIBS=` (`-lz` for `zlib`), `ADDOBJS=` and, for
   `debug`/`asan`, `CFLAGS='$(CFBASE) ...'` (single-quoted, so that each
   sub-makefile expands its own `CFBASE`) and `LDFLAGS`. Command-line
   variables propagate to the recursive makes through `MAKEFLAGS`.
   `ADDLIBS`/`ADDOBJS` are referenced but never assigned upstream, so they
   are set empty to keep the environment out of the build; `MAKEFLAGS`
   and friends are unset for the same reason. Only the `fpgrowth` target
   is built: the default `all` target also wants `fpgpsp`, whose sources
   (`tract/src/tars.c`, `pspest.c`) are not in the archive.

4. **Checks.** Every compile command in `build.log` must contain
   `-ffp-contract=off` (and none may for `native`). `nm` must show the
   assertion handler (`__assert_rtn` on macOS, `__assert_fail` on
   glibc/musl) in `debug` and `asan` but not in `release`/`native`, and
   `__asan_init`/`__ubsan_*` in `asan`. Each binary then mines
   `fpgrowth/ex/test1.tab` at 50% support; the `zlib` binary must also
   list `-z` in its usage message and write a zlib header with `-z`.

A clean build of all four variants takes about 18 s on an M-series Mac.

## Running the sanitizer build

Recommended runtime options:

```bash
export ASAN_OPTIONS=detect_stack_use_after_return=1:strict_string_checks=1
export UBSAN_OPTIONS=print_stacktrace=1
```

- UBSan reports and continues; ASan stops at the first error with exit
  code 1.
- LeakSanitizer is not available on macOS/arm64
  (`ASAN_OPTIONS=detect_leaks=1` aborts with "detect_leaks is not
  supported on this platform"). On macOS use
  `leaks --atExit -- build/debug/fpgrowth/src/fpgrowth ...` instead (the
  debug build frees everything at exit, so any reported leak is real).
- Every run that uses the item set tree (`-tr`, or a rule-based `-e`
  measure for item sets) produces UBSan "misaligned address ... for type
  'ISTNODE *'" reports from `apriori/src/istree.c`. This is a known
  layout property, not memory corruption: upstream only pads child
  pointer arrays to 8 bytes when compiled with `-DALIGN8` (off by
  default, documented in `istree.c` and the `apriori` makefile).
  To silence it, put `alignment:istree.c` in a file and add
  `suppressions=<file>` to `UBSAN_OPTIONS`.

The confirmed upstream bugs and the full sanitizer findings are in
[`docs/upstream/reference-build.md`](../docs/upstream/reference-build.md).

## Compiler warnings (Apple clang 21.0.0, arm64)

Counts are from `build/<variant>/build.log` (17 translation units;
header warnings are counted once per unit that includes the header).

| Warning | release / native | debug | asan | Meaningful? |
| --- | ---: | ---: | ---: | --- |
| `-Wkeyword-macro` | 69 | 69 | 69 | No |
| `-Wunknown-warning-option` | 5 | 5 | 5 | No |
| `-Wgnu-zero-variadic-macro-arguments` | 2 | 2 | 2 | No |
| `-Wstring-concatenation` | 1 | 1 | 1 | No |
| `-Wunused-function` | 0 | 4 | 4 | No |
| `-Wdeprecated-declarations` | 0 | 0 | 2 | No |
| **Total** | **77** | **81** | **83** | |

There are no `-Wconversion`, `-Wsign-compare`, uninitialized-variable
or format warnings.

- **`-Wkeyword-macro` (69).** `symtab.h`, `tract.h`, `report.h`,
  `clomax.h`, `report.c`, `patspec.c` and `istree.c` temporarily
  `#define int 1`, `#define long 2`, `#define double 4` (and
  `ptrdiff_t`) so that `#if ITEM==int`-style tests can pick printf
  formats and limits for the configurable `ITEM`/`SUPP`/`IDENT` types.
  Every such `#define` has a matching `#undef` in the same file before
  any code, so no declaration is affected. Deliberate preprocessor
  trick; harmless.
- **`-Wunknown-warning-option` (5).** The `tract` makefile passes the
  GCC-only `-Wimplicit-fallthrough=0`; clang ignores it (once per
  `tract/src` unit: clomax, repcm, fim16, taread, patspec).
- **`-Wgnu-zero-variadic-macro-arguments` (2).** `tabwrite.h:75`
  `twr_printf(t, s, ...)` uses `, ##__VA_ARGS__`; all call sites pass at
  least one argument and both gcc and clang support the extension.
- **`-Wstring-concatenation` (1), `fpgrowth.c:265`.**
  `/* E_SIZE -10 */ "invalid item set size %"ITEM_FMT,` in the
  `errmsgs[]` table. `ITEM_FMT` is a macro for the string literal `"d"`
  (`tract.h:131`), so the two literals are meant to concatenate into
  `"invalid item set size %d"`; the trailing comma is present. The
  warning is a heuristic for a missing comma between array elements and
  does not apply. **Not a bug:** the table keeps its 18 entries
  (`E_NONE` 0 to -16 plus "unknown error"), so the `errmsgs[-code]`
  lookup in `error.h` is not shifted. Checked at run time:
  `fpgrowth -m-1 ex/test1.tab` prints "invalid item set size -1" (exit
  10) and `-Az` prints "invalid fpgrowth variant 'z'" (exit 16, the last
  regular entry).
- **`-Wunused-function` (4, debug/asan only).** `fpt_show`, `cst_show`,
  `tdt_show` (`fpgrowth.c`) and `show` (`fim16.c`) are tree-dump helpers
  compiled under `#ifndef NDEBUG` and never called. Harmless.
- **`-Wdeprecated-declarations` (2, asan only).** `sprintf` at
  `report.c:344` and `report.c:1533`. Under `-fsanitize=address` clang
  defines `_FORTIFY_SOURCE=0` on Darwin, so the macOS SDK's
  `__deprecated_msg` on plain `sprintf` becomes visible (otherwise the
  fortified `__builtin___sprintf_chk` hides it). Both buffers are large
  enough: `%.*g` with at most 32 significant digits into `buf[BS_FLOAT]`
  (96 bytes), and `" (%d)\n"` into `foinfo[64]`. Harmless.

None of the warnings points at a defect. The defects that do exist were
found at run time; see `docs/upstream/reference-build.md`.

## Differential test suite (`difftest/`)

`difftest/` (package `difftest`, tests and helpers only) runs the C
reference and the Go CLI side by side on random cases and compares them.

```bash
cd reference
go test ./difftest/                       # 10,000 small + 300 large cases, about 45 s on 10 cores
DIFFTEST_CASES=50000 go test ./difftest/ -run TestDifferential -timeout 30m
DIFFTEST_LARGE=10000 go test ./difftest/ -run TestLarge -timeout 30m
./difftest.sh 50000 [seed] [large]        # writes difftest-summary.txt (and difftest-large-summary.txt)
DIFFTEST_SEED=1 DIFFTEST_ONLY=429 go test ./difftest/ -run TestDifferential -v -artifacts
DIFFTEST_SEED=1 DIFFTEST_LARGE_ONLY=17 go test ./difftest/ -run TestLarge -v -artifacts
```

- **Binaries.** The C oracle is `build/release/fpgrowth/src/fpgrowth`; the
  test runs `./build.sh release` if it is missing (or `DIFFTEST_REBUILD` is
  set) and is skipped without a C compiler. `DIFFTEST_C` points to another
  C binary, `DIFFTEST_GO` to a prebuilt Go program under test. The Go CLI
  is otherwise built from `../cmd/fpgrowth` into a temporary directory.
  Both run in the case directory with `argv[0]` `fpgrowth`.
- **Cases.** Case `i` of base seed `DIFFTEST_SEED` (default 1) is fully
  determined by the pair: a random dataset (generator copied from
  `internal/oracle`, because a nested module cannot import the root
  module's internal packages; `TestGenerate_matchesOracle` pins the copy
  to the oracle's golden hashes) and a random option set covering targets
  `s c m g r` and variants `-As -Ac -Ad -At` (cycled evenly), `-x -j -u
  -l -q`, relative, absolute and zero `-s`, `-S -m -n -F -c -o`, every
  `-e` measure with `-a -d -i -p`, `-R` selection and appearance files,
  `-Z -P -k -I`, file output and count-only mode. About 15 % of the cases
  use a structured dataset (`structgen.go`): products of independent item
  groups, where the measures of items of different groups are exactly at
  their independence values (and often exactly at the threshold, e.g.
  `-eb -d0`, lift `-el -d100`), or frequency ties (equal item
  frequencies, cyclic windows, few repeated transactions).
- **Comparison.** Every case prints absolute supports (`%a`, `%b`, `%h`)
  and the measure with 17 digits (`%17e`), so both outputs are parsed into
  canonical records (sorted set, or head and sorted body). Supports must
  be equal; floats agree within justified tolerances (1e-12 relative;
  for the logarithmic measures a bound proportional to `N ln N` of the
  total weight `N`, see `measureTol`); records on one side only must lie
  at the evaluation threshold; common records must come in the same
  order; stdout, stderr (without timings) and the pattern spectrum must
  be identical. Byte equality of all outputs is reported separately.
  In count-only mode, differing counts of a case with a measure are
  classified by rerunning both programs with an output file: they are
  tolerated only if the records differ at the threshold (or by a
  value-level deviation) and each count equals that program's records.
  This covers sets exactly at a log-measure threshold, where Go's
  `math.Log` and Apple's libm can round differently (F-1 in
  `docs/differences.md`; `TestF1Reproducer`).
- **Deviations.** Cases that can trigger a documented deviation
  (`docs/differences.md`: M1-M5, M9, R1-R3, R8, R11, O3, O4; predicates
  in `caseDeviations`, derived from the data: e.g. M1 only for `-At` with
  an item eliminated at the top level, M5/O4 only if a frequent set
  larger than `-n` exists, O3 only where upstream's fast output path is
  active and has an effect) are checked against an independent
  brute-force evaluation (`brute.go`: frequent, closed, maximal,
  generator sets and single-head rules with all support, size, border,
  selection and appearance constraints; a subset with exact supports
  when an evaluation measure filters). The C output is checked the same
  way: if it passes, C and Go are compared in full (records, order,
  stdout, stderr, `-Z`, `-P`), and only the channels the deviation
  explains are excused (`devExempt`: the log for M2/M3, the rule order
  for M2, the spectrum for O3, values for R1, membership for R1/R2 and,
  with a measure, for record-level deviations). Only cases in which C
  fails the brute force, crashes or differs in an excused channel are
  *deviation-verified*. Value-level measure deviations (R4-R7, R9)
  excuse single records in the direction of the fix. Every successful Go
  run is also checked against the brute force and for consistency of its
  log count, `-Z` statistics and pattern spectrum.
- **Large-data mode.** `TestLarge` (`large.go`) compares C and Go
  without the brute force on 40-1000 items and 500-8000 transactions from
  three generators (Zipf-like popularity with planted patterns, equal
  item frequencies, co-occurring item blocks), cycling all targets and
  variants with `-x -j -u -l -q -m -n -F -S -c -o`, measures, `-Z`, `-P`,
  `-k` and count-only mode. The minimum support follows the item
  frequencies to keep the output moderate, and options that would
  trigger a documented deviation on the data are changed
  (`avoidDeviations`), so every case must be byte-equal or semantically
  equal. `DIFFTEST_LARGE` sets the number of cases (default 300, about
  5 s; `-short` 6; 0 skips), e.g. `DIFFTEST_LARGE=10000` for a larger
  run (about 3 minutes on 10 cores).
- **Report.** The summary lists cases per verdict (byte-equal,
  semantic-equal, tolerated, error-equal, deviation-verified, FAIL),
  brute-force checks, tolerated values, count-only reruns, the cases with
  a flagged deviation that were compared with C in full, flagged and
  verified cases per deviation, and each failure with its seed, case
  index and a reproducing command; `-artifacts` keeps the inputs and both
  outputs of failing cases.
