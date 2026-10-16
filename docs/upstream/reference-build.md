# Reference build: sanitizer, assertion and cross-variant findings

Scope: upstream FP-Growth 6.21 built by `reference/build.sh` (see
`reference/README.md`) on macOS 26 / arm64, Apple clang 21.0.0, GNU make
3.81. Variants: `release` (`-O3 -DNDEBUG -funroll-loops`), `debug`
(`-g -O0`, assertions on), `asan` (`-g -O1 -fsanitize=address,undefined`,
assertions on), all three with `-ffp-contract=off`; plus `native` (exact
upstream flags, fused multiply-add allowed), compared separately.

All repro commands below assume

```bash
cd reference
R=build/release/fpgrowth/src/fpgrowth   # likewise D=debug, A=asan, N=native
export UBSAN_OPTIONS=print_stacktrace=1
```

and write tiny inputs with `printf`. Every command was run and its result
is quoted as observed.

## Summary

| # | Finding | Trigger | release | debug/asan | Verdict |
| --- | --- | --- | --- | --- | --- |
| F1 | `fpg_topdn` copies the item **map** instead of the item list | `-At`, perfect-extension pruning on (default), an item in every transaction | SIGSEGV/SIGBUS, or **silently wrong/empty output** | assertion `isr_add`/`isr_addpex`; ASan heap-buffer-overflow | **Real bug** |
| F2 | Empty item set printed despite `-m1` | fast output path (absolute support or `-v' (%a)'`), an item in every transaction | extra line ` (N)` | same | **Real bug** (output inconsistency) |
| F3 | Maximal set lost with `-x` on the 16-items machine | `-tm -x -As`/`-Ad` (`-l`>0), items in every transaction | set missing | same | **Real bug** |
| F4 | Minimum support 0 accepted | `-s0` / `-s-0` | SIGSEGV for `-As`/`-Ad` (and `-tg -At`); `-Ac` reports some 0-support sets, `-At` does not | NULL deref, heap overflow, fim16 assertion | **Real bug** (missing validation) |
| F5 | Pre-format table size overflow | maximum item support ≳ 2.2·10⁸ (large `-w` weights) | E_NOMEM or SIGSEGV; 1.5 GB RSS at 10⁸ | UBSan int overflow `report.c:944–949`, ASan SEGV/heap overflow `report.c:963` | **Real bug** |
| F6 | Weight sums overflow `int` | total/item weight > INT_MAX | wrong counts | UBSan `tract.c:806–811`, `tract.c:1818` | **Real bug** (no overflow check) |
| F7 | Negative transaction weights accepted | `-w` with negative weight | runs on nonsense counts; `-At` crashes | assertions `supp >= 0` (`add_*`), `isr_setsupp` | Missing validation |
| F8 | `ist_init` reads `lvls[zmin-1]` out of bounds | item set tree (`-tr`, rule measures) with `-m` > items+1 | output unaffected | ASan heap-buffer-overflow / use-after-free `istree.c:1700`; assertion `evaluate` `istree.c:554` | Real OOB read, benign in practice |
| F9 | `GammaQ` assertion on −3·10⁻¹⁶ information gain | `-eg` at exact independence | p-value 1 (correct limit) | assertion `gamma.c:252` | Over-strict assertion, benign |
| F10 | Unvalidated size/support ranges and NaN/Inf | `-n0`, `-S0`, `-Snan`, `-snan`, `-s-1e12`, `-Fnan` | no crash, empty or arbitrary output | assertions `report.c:815/825`; UBSan float-cast-overflow `fpgrowth.c:1917/1920/1998/2188` | Missing validation, benign |
| F11 | Misaligned `ISTNODE*` accesses | any item set tree use | correct | UBSan alignment, `istree.c` (16 sites) | Benign on arm64/x86-64 (upstream `ALIGN8` off) |
| F12 | Memory leaks | repeated `-F`; `-P` with `-n` | n/a (release never frees) | `leaks`: `getbdr` `fpgrowth.c:2164`, `resize` `patspec.c:260` | Real, harmless at exit |

Cross-variant result: over 97,351 option/input combinations (389,404
runs), **release, debug and asan produced byte-identical output, exit code,
pattern spectrum, stdout and (normalized) stderr in every run that did not
hit a crash, an assertion or an ASan error.** All 7,269 flagged cases are
explained by F1–F12; 0 unexplained mismatches, 0 timeouts, 0 hangs.
`native` (FMA allowed) differs from `release` in 9 runs by output and in
243 runs only by the signal number of an F5 crash (SIGBUS vs SIGSEGV).

## Method

The battery (scripts kept outside the repository) builds 97,351 cases,
runs each with `release`, `debug`, `asan` and `native`, and compares exit
code, output file, `-P` file, stdout, and stderr with timings
(`[0.00s]`), the work directory and UBSan report lines removed. Runtime
options: `ASAN_OPTIONS=detect_stack_use_after_return=1:strict_string_checks=1:check_initialization_order=1:halt_on_error=1`,
`UBSAN_OPTIONS=print_stacktrace=1:report_error_type=1:suppressions=<file
with alignment:istree.c>` (F11 was recorded unsuppressed separately),
timeout 60 s, file-size limit 256 MiB.

Inputs (115 datasets):

- Upstream examples, read as documented: `test1.tab`; `test1.csv` with
  `-b,` and `-b" ,"` (raw: "item expected" error, as documented);
  `test2.tab` with `-f",.;:"` and raw; `test3.tab` with `-w` and raw;
  `test4.tab` through `tid2set`, `test5.tab` through `flg2set`, `hdr2set`
  and `tab2set`, `test6.tab` through `row2set` (the gawk scripts were
  re-implemented with the same logic), and all of 4–6 raw.
- Edge cases: empty file, only empty records, single item, single
  transaction, duplicate items, no final newline, CRLF, comments, blanks
  and tabs, empty fields, numeric/quoted/backslash/UTF-8/high-byte/control
  item names, `#` inside a record, item names longer than 1024 bytes
  (`TRD_MAXLEN`: truncated and merged).
- Weights: plain and parenthesised, zero (all/some), negative, near
  INT_MAX, 10⁹, non-numeric, missing.
- 48 random small datasets (1–20 items, 1–40 transactions, up to 10
  items/transaction, empty transactions, duplicates) and 12 random
  weighted ones; dense 15/16/17/24-item sets (16-items machine boundary);
  3000 transactions × 60 items; one 1008-item transaction; a repeated
  40-item transaction.
- 15 `-R` files: item selections (incl. empty, unknown items, duplicates)
  and appearance files (all indicator aliases, unknown indicator,
  duplicate item, missing indicator, two fields in the header, empty).

Option groups (cases): core grid `-t{s,f,c,m,g,r}` × `-A{s,c,d,t}` ×
{none, `-x`, `-j`, `-u`, `-l0`, `-l16`, `-l1`, `-x -j -u`, `-q0`, `-q1`,
`-q-1`, `-q-2`} × 2–3 supports (51,672); evaluation `-e` (all 26
measures) × `-a{x,m,n,a}` × `-p{none,-1,0,1,2}`, `-i`, `-o`, `-d`
(21,472); `-m`/`-n` limits (5,120); 21 `-s` values from `-s100` to
`-s1e-300`, `-s-0.5`, `-s-1e12`, `-S`, `-c`, `-o` (3,312); 14 `-F`
borders × `-m` (5,600); `-Z`/`-P` (1,539); `-R` (908); `-v`/`-k`/`-h`/
`-I`/`-g`/`-N` incl. 2000-char formats and `%9999999999S` (748); `-r`/
`-f`/`-b`/`-C`/`-w`/stdin (723); option errors (109); long transactions
(148); 6,000 random combinations of all options.

Additional checks: macOS `leaks --atExit` on 300 sampled debug runs;
a cross-algorithm comparison (same options, `-As`/`-Ac`/`-Ad`/`-At`,
item sets compared as sets) over 15,858 option groups; a scratch copy with
the one-line F1 fix.

Clean areas (no sanitizer report, assertion or mismatch): all input
format options, `-R` selection/appearance files, all output format
options, `-g` with odd item names, long and dense data, `-F` borders,
rules and all 26 evaluation measures (apart from F8/F9).

## Findings

### F1 — `-At` uses the item map as item list (real bug)

```bash
printf 'a b\na c\n' > t.tab
$R -At t.tab -;  echo $?      # 139 (SIGSEGV), no output
$R -At -x t.tab -; echo $?    # 0, correct
$D -At t.tab -               # Assertion failed: (rep && (item >= 0) && (item < ib_cnt(rep->base))),
                             #   function isr_add, file report.c, line 1128.
printf 'b a c\nc\n' > t2.tab
$R -At -s-1 -eb -d10 t2.tab -; echo $?   # 0 and NO output (silently wrong)
$R -Ac -s-1 -eb -d10 t2.tab -            # b c a (1) / b a (1)
```

With asserts disabled under ASan (`-DNDEBUG -fsanitize=address`, scratch
build): `heap-buffer-overflow ... READ of size 4 in isr_addpex
report.c:1227`, called from `rec_topdn fpgrowth.c:1592`, the buffer
allocated by `isr_createx report.c:691`.

Diagnosis: `fpg_topdn` (`fpgrowth.c:1660`) does
`memcpy(tree->items, d, k*sizeof(ITEM))`, where `d` is the old→new item
map (`-1` for removed items) and `s` holds the kept item ids. The other
three variants use `s` (`h->item = s[i]`, lines 776/1235/1409/1779). When
no item is removed at the top level the map is the identity and the bug
is invisible; an item contained in every transaction is removed as a
perfect extension, so `tree->items[]` then holds `-1` or wrong ids.
Results: SIGSEGV/SIGBUS, heap overflow in `isr_add`/`isr_addpex`/
`fastout report.c:1354`/`output report.c:1402`, or silently missing sets
(seen with `-eb`). In the battery: 876 + 463 assertion cases plus the
`e_longname`/`e_ctrl` ASan reports. With `memcpy(tree->items, s, ...)`,
`-At` equalled `-Ac` (as sets of item sets with support) in all 3,210
comparisons tried, against 657 crashes without the fix. Workaround: `-x`
or another `-A` variant. The Go port must not reproduce this; record the
deviation in `docs/differences.md`.

### F2 — empty item set reported by the fast output path (real bug)

```bash
printf 'a b\nb c\n' > e.tab
$R -s-1 e.tab -              # ... b (2) and a last line " (2)"
$R -v' (%a)' e.tab -         # also prints " (2)"
$R -v' (%a) ' -s-1 e.tab -   # no empty set (non-fast path)
$R -s-1 -Z e.tab -           # statistics count "0: 1"
```

`fastchk()` (`report.c:145`) enables fast output for `zmin <= 1`,
default separators, and info `" (%a)"` (the default for absolute
support). `isr_report` only rejects a set when `cnt+n < zmin`
(`report.c:1488`), and `fastout()` prints the current set before its
perfect extensions without checking `cnt >= zmin`. So when the empty set
has perfect extensions (an item in every transaction), it is printed and
counted. The non-fast `report()` path filters it. It happens with every
`-A` variant when pruning is on, and with `-x` only for `-As`/`-Ad` with
the 16-items machine (it registers the extensions itself). `-At` crashes
instead (F1). Relative support (`-s10`, info `" (%S)"`) does not show it.
Same bytes in all three oracle builds. Decision needed for the port:
reproduce it (bug-compatible golden files) or treat it as a documented
difference.

### F3 — `-tm -x` loses maximal sets on the 16-items machine (real bug)

```bash
printf 'a\n' > m.tab
$R -tm -x -As m.tab -        # no output
$R -tm -x -As -l0 m.tab -    # a (100)
$R -tm -x -Ac m.tab -        # a (100)
```

The cross-algorithm check found 31+ groups (`-tm -x`, also with `-j -u`)
where `-As`/`-Ad` with `-l`>0 miss the maximal set made of items present
in every transaction (for example `r_08`: a single 6-item transaction,
`r_14`: `{x5y,x6y}` in all 3 transactions). The 16-items machine treats
such items as perfect extensions even with `-x`. No sanitizer report.

### F4 — `-s0` (minimum support 0) (real bug, missing validation)

```bash
printf 'a b\n' > z.tab
$R -s0 -As z.tab -; echo $?     # 139
$A -s0 -As z.tab -              # runtime error: member access within null pointer of type
                                #   'FPNODE' at fpgrowth.c:702 (rec_smp16); ASan SEGV fpgrowth.c:702
$R -s0 -As -l0 z.tab -          # works
$R -ts -s0 -Ac upstream/fpgrowth-6.21/fpgrowth/ex/test1.tab -  # 23 sets, incl. "e c a d (0)", "e c a (0)"; -At: 21
```

`main()` only rejects `smin > 100`; with `-s0` (or `-s-0`) the absolute
minimum is 0, so items with zero conditional support count as frequent
and their empty node lists are dereferenced: `rec_smp16 fpgrowth.c:702`,
`rec_single fpgrowth.c:1297`, `rec_simple fpgrowth.c:517`,
`rec_cmplx fpgrowth.c:1162`, `fim16.c:540 rec_neg` (SEGV),
`fim16.c:501` assertion `(mask & (1 << i))`, `is_isgen report.c:611`
(misaligned store + SEGV, `-tg`), `isr_report report.c:1555` heap
overflow. Variants that survive disagree: on `ex/test1.tab` `-Ac`
reports 23 sets (2 with support 0), `-At` 21, `-As`/`-Ad` crash.
`-s1e-9` and `-s-0.5` round up to 1 and are fine. The port should reject
or clamp a zero minimum support.

### F5 — `isr_prefmt` size overflow with large supports (real bug)

```bash
printf 'a 1000000000\n' > w.tab
$R -w w.tab -; echo $?          # 139
$A -w w.tab -                   # report.c:944:20: runtime error: signed integer overflow ...
                                # ERROR: AddressSanitizer: SEGV ... isr_prefmt report.c:963
printf 'a 250000000\n' > w.tab; $R -w w.tab -; echo $?   # 1 (not enough memory)
printf 'a 100000000\n' > w.tab; $R -w w.tab -            # works, 1.5 GB max RSS
$R -w -N w.tab -                # -N (no pre-formatting) always works
```

`isr_prefmt(report, min, max)` (called from `fpg_report fpgrowth.c:2004`
unless `-N`) pre-formats every integer from the minimum support to the
largest item support. Its byte count `z` is a `TID` (`int`) and overflows
above about 2.2·10⁸, so `malloc` either fails or gets a short buffer that
the copy loop overruns (`report.c:963`, heap-buffer-overflow / SEGV /
BUS). Below that the table is still O(max support) memory.

### F6 — weight sums overflow `int` (real bug)

`ib_read` sums `base->wgt`, `itd->xfq` (`size*weight`) and `itd->frq` in
`int` without checks (`tract.c:806,807,810,811`); `tbg_add` sums
`bag->wgt` (`tract.c:1818`). Repro: `printf 'a b 2147483647\nb c
2147483647\na c 1\n' | $A -w - -` reports `tract.c:807` (`2 * 2147483647`),
`tract.c:806` and `tract.c:811` (`2147483647 + 2147483647`); release then
prints "no (frequent) items found" (exit 15) because the sums wrapped.
`printf 'a b 1000000000\nb c 1000000000\na c 1\n'` overflows
`xfq` (`tract.c:810`, 2·10⁹ + 2·10⁹) and then hits F5. The port should
use 64-bit sums or reject such inputs.

### F7 — negative weights accepted (missing validation)

```bash
printf 'a b -1\nb c 5\na c 3\na b c 2\n' > n.tab
$R -w -s-1 -As n.tab -; echo $?   # 0, sets computed from negative counts
$D -w -s-1 -As n.tab -            # Assertion failed: (tree && (ids || (n <= 0)) && (supp >= 0)),
                                  #   function add_smp16, file fpgrowth.c, line 543.
printf 'a b -1\nb c 2\na c -3\n' > n2.tab
$D -w -s-1 n2.tab -               # Assertion failed: (rep && (smin >= 0) && (smax >= smin)),
                                  #   function isr_setsupp, file report.c, line 815.
```

`strtosupp` accepts any sign (`tract.c:712`). The same assertion fires in
`add_simple` (401), `add_cmplx` (831), `add_topdn` (1449). Release runs
on, and `-At` crashes (F1). Garbage-in; the port should reject negative
weights. Zero weights are fine (all zero: "no (frequent) items found").

### F8 — `ist_init` out-of-bounds read with large `-m` (benign)

```bash
printf 'a b c d e\n' > o.tab
$R -tr -s-1 -c0 -m100 o.tab -; echo $?   # 0, no rules (correct)
$A -tr -s-1 -c0 -m100 o.tab -           # heap-buffer-overflow (or heap-use-after-free) in
                                        #   ist_init istree.c:1700
```

`ist_init` sets `ist->node = ist->lvls[zmin-1]` without checking zmin
against the `n+1` entries of `lvls` (`istree.c:616`); entries between
the tree height and `n` are uninitialized. The value is only used by
`evaluate()`, which returns before dereferencing because `index == -1`;
in debug builds the `assert(ist && node)` at `istree.c:554` fires first
(for example `-tg -ei -s50 -m2 -ax -p0 -d50 -At` on a 1-item input).
Fix: clamp `size` to the height.

### F9 — `GammaQ` assertion on rounding (benign)

```bash
printf 'a b\na\na\nb\n\n\n' > g.tab        # n=6, a in 3, b in 2, ab in 1: independence
$D -ts -s-1 -eg g.tab -   # Assertion failed: ((n > 0) && (x >= 0)), function GammaQ, file gamma.c, line 252.
$R -ts -s-1 -ei -d-100 -v' %a %20e' g.tab -  # b a 1 -3.2034265038149176408e-16
$R -ts -s-1 -eg -d100 -v' %a %e' g.tab -     # b a 1 1
```

`re_info` (`ruleval.c:232`) evaluates to −3.2·10⁻¹⁶ instead of 0, and
`re_infopval` passes `2 ln2 · n · info` to `chi2cdfQ`/`GammaQ`. Release
returns 1 for `x <= 0`, the correct limit; only the assertion is too
strict. The port must reproduce the tiny negative value for `-ei`/`%e`
(it is printed and compared against `-d`).

### F10 — unvalidated ranges and non-finite numbers (benign)

| Command (on `ex/test1.tab`) | release | debug/asan |
| --- | --- | --- |
| `-n0` (zmax < default zmin 1) | exit 0, nothing reported | `isr_setsize` assertion `report.c:825` |
| `-S0`, `-S-2` (smax < smin) | exit 0, nothing reported | `isr_setsupp` assertion `report.c:815` |
| `-Snan` | exit 0 | UBSan `fpgrowth.c:1998` nan → int, then `report.c:815` assertion |
| `-snan`, `-s nan` | exit 0 | UBSan float-cast-overflow `fpgrowth.c:1917`, `1920` |
| `-s-inf`, `-s-1e12`, `-s-2147483648` | exit 15 (no frequent items) | same + UBSan `fpgrowth.c:1917/1920` |
| `-Fnan`, `-Finf:-inf` | exit 0 | UBSan `fpgrowth.c:2188` |

`main()` checks only `zmin<0`, `zmax<0`, `smin>100` and the confidence
range. Converting NaN or out-of-range doubles to `int` is undefined
behaviour; on arm64 it saturates. Results are otherwise identical across
builds. The port should validate instead of copying this.

### F11 — misaligned `ISTNODE*` (benign)

Every item set tree run (`-tr`, or a rule measure for item sets) reports
`runtime error: load/store of misaligned address ... for type 'ISTNODE *',
which requires 8 byte alignment` at `istree.c` lines 226, 234, 266, 1086,
1108, 1116, 1119, 1225, 1226, 1233, 1925, 1938, 1939, 2074, 2083, 2084,
e.g.

```bash
$A -tr -s-1 upstream/fpgrowth-6.21/fpgrowth/ex/test1.tab /dev/null   # first report: children istree.c:1116
```

Child pointer arrays follow the `SUPP`/`ITEM` arrays of a node and are
only padded with `-DALIGN8` (documented, off by default, `istree.c:161`).
arm64 and x86-64 allow unaligned loads, so the results are correct; a
strict-alignment platform would need `ADDFLAGS=-DALIGN8`. Suppress with
`alignment:istree.c`. `report.c:611` (F4) is the only other alignment
report, and only under `-s0`.

### F12 — leaks (benign)

`leaks --atExit` on 300 sampled debug runs (debug builds free everything
at exit): 273 clean, 22 not checkable (process aborted by an assertion),
5 with leaks of two kinds:

- `-F2:2:1 -F-1`: the second `-F` overwrites `border` without freeing
  the first array (`getbdr`, `fpgrowth.c:2164`), 32 bytes.
- `-P file` with `-n` (for example `-tf -Ad -s-1 -Z -P x.psp -m2 -n3`
  on a random set): `psp_delete` (`patspec.c:171`) frees rows
  `minsize..max-1`, but rows go up to `max`, so the counters allocated by
  `resize` (`patspec.c:260`) for the largest size leak when `-n` caps the
  spectrum. Off-by-one, 80–256 bytes.

## Release vs native (floating-point contraction)

`native` has 46 fused instructions (`fmadd`/`fnmsub`), the oracle builds
none. Over the battery 9 runs differ in output (243 more differ only by
crash signal), all with measures whose exact value is 0 at independence:

- `-tr -s-1 -c50 -ei -d0 -o` on a 30-transaction random set: 21,573
  rules unfused vs 22,121 fused. Rule `i <- n o d b m` (support 4,
  body 8, head 15, n 30): information gain −6.4068530076298352816e-16
  unfused (fails `-d0`), 0 fused (passes).
- `-ts -ew -d0 -v' %e %E %a %s'`: `k i` has Kulczynski difference 0
  unfused and 1.11022e-16 fused.

So FMA changes threshold decisions, not just printed digits; this
confirms the `-ffp-contract=off` requirement for the oracle.

## Notes for the Go port

- Use the `release` build (or debug/asan, which agree) as the oracle.
  Avoid inputs and options that trigger F1, F4–F8 and F10 in golden
  tests, or record the intended deviation in `docs/differences.md`.
- F2, F3 and F9's negative zero-ish values are deterministic upstream
  behaviour that differential tests will hit with ordinary inputs; decide
  explicitly whether to match them.
- Item names are truncated to 1024 bytes (`TRD_MAXLEN`), which merges
  long names (`e_longname`); behaviour, not a bug, but must be ported.
