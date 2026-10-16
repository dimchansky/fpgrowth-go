# Upstream audit: command-line program and file I/O (fpgrowth 6.21)

Scope: `fpgrowth/src/fpgrowth.c` `main()`, `help()`, `getbdr()`, `setbdr()`
(lines 2084-2556) and the I/O helpers it relies on: `util/src/tabread.[ch]`,
`escape.c`, `scanner.c` (scanable names), `symtab.c` (item identifier map),
`tabwrite.c`, plus the reading/reporting entry points in `tract/src/tract.c`,
`tract/src/report.c` and `tract/src/patspec.c` as far as they define
observable CLI behavior. All paths below are relative to
`reference/upstream/fpgrowth-6.21/`.

Method: source reading plus black-box runs of the reference executable,
built with the upstream makefile flags (`-std=c99 -O3 -funroll-loops
-DNDEBUG`, no `USE_ZLIB`, no `QUIET`) with clang on macOS arm64 (plain
`char` is signed on this target). Every behavior marked **confirmed** was
reproduced with that binary; the reproducer is given inline. `$FPG` below
stands for the reference executable. Timing values in logs are shown as
`[T]`.

Related upstream documentation: `fpgrowth/doc/fpgrowth.html`. Where it
disagrees with the code, the code is described here and the discrepancy is
listed in section 11.

---

## 1. Invocation model

```
fpgrowth [options] infile [outfile]
```

- With **no arguments at all** (`argc == 1`) the usage text is printed to
  **stdout** and the program exits 0 (`fpgrowth.c:2262-2357`). The first line
  is `usage: <argv[0]> [options] infile [outfile]`.
- With **any argument**, a two-line banner is printed to **stderr** first
  (`fpgrowth.c:2259-2261`): `"<argv[0]> - find frequent item sets with the
  fpgrowth algorithm\n"` followed by `"version 6.21 (2022.11.22)        (c)
  2004-2022   Christian Borgelt"` (8 and 3 spaces) **without** a newline. The
  newline is written only after all arguments were parsed and validated
  (`fpgrowth.c:2463`).
- `argv[0]` is used verbatim as the program name in the banner, usage line
  and every error message (`prgname`, `fpgrowth.c:2256`).

### 1.1 Argument grammar (`fpgrowth.c:2362-2416`)

Arguments are processed strictly left to right; options may appear before,
between or after the file names.

- An argument is an **option cluster** iff it starts with `-` and has at
  least one more character (`(*s == '-') && *++s`, line 2365). Every
  following character is an option letter, evaluated in a loop.
- **Flag options** (`o i x j u Z N g w !`) consume only their letter, so
  they combine freely: `-xjZ`.
- **Char-valued options** (`t e a A`) take the *next character of the same
  argument* as value (any byte, including another option letter); if the
  cluster ends there the value is `'s'` for `-t` and `'\0'` for `-e`, `-a`,
  `-A` (which then fails validation, printing a literal NUL byte in the
  message, **confirmed**: `$FPG -e x.tab` prints `invalid evaluation measure
  '\0'`).
- **Numeric options** parse a prefix of the rest of the cluster:
  `-m -n -p -q -l` with `strtol(s, &s, 0)` (C base-0: optional leading
  whitespace, sign, `0x`/`0X` hex, leading `0` octal) cast to `int`;
  `-s -S -c -d` with `strtod` (leading whitespace, sign, decimal or hex
  float, `inf`/`infinity`/`nan`, case-insensitive). Parsing continues with
  the first unconsumed character as the next option letter, so
  `-s10m2n5` == `-s10 -m2 -n5`. A numeric option **never** takes the next
  argv: `-s 20 in out` sets `smin = 0` and makes `20` the input file
  (**confirmed**: `cannot open file 20`). An empty value yields 0.
- **String options** (`R P h k I v r f b C`): if characters follow the
  letter, the rest of the argument is the value (`-k,`); otherwise the
  **next argv is taken verbatim**, even if it is empty or starts with `-`
  (`-k -` sets the separator to `-`; `-v ''` sets an empty info format).
  A string option at the very end of the command line is the error
  `missing option argument` (exit 7).
- **`-F`** (support border) parses a colon-separated list of `strtod`
  values from the rest of the cluster (`getbdr`, section 2.3).
- Any other letter: `unknown option -<c>` (exit 6), raised immediately
  during the scan. `--` therefore fails with `unknown option --`;
  `-m2.5` fails with `unknown option -.` (**confirmed**).
- **Non-option arguments**: the first is `infile`, the second `outfile`, a
  third raises `wrong number of arguments` (exit 8) immediately.
- **`-` as a file name**: because of the pre-increment in line 2365 the
  stored name for a bare `-` is the empty string, and an empty name means
  stdin (input) / stdout (output). So `-` and `""` are equivalent for
  `infile` and `outfile`. For option values the argument is stored
  verbatim: `-R-`, `-R -`, `-P-`, `-P -` name a file literally called
  `-` (**confirmed**: `-R- x.tab` gives `cannot open file -`); only an
  empty value (`-R ""`, `-P ""`) means stdin/stdout.
- Repeated options: last value wins (doc: "the last statement counts").
  Exceptions: flags cannot be undone; a repeated `-F` leaks the earlier
  border array (harmless).

Strtol/strtod edge cases (all **confirmed**):

| Argument | Effect |
| --- | --- |
| `-m010` | octal, `zmin = 8` |
| `-m0X3`, `-m0x2` | hex, 3 / 2 |
| `-m08` | `zmin = 0`, then `unknown option -8` |
| `-m0xZ` | `"0"` parsed (no hex digit after `x`), then flags `-x`, `-Z` |
| `-m4294967298` | `long` 4294967298 truncated to `int` 2 |
| `-m99999999999999999999` | `LONG_MAX` truncated to `int` -1: `invalid item set size -1` |
| `-m-99999999999999999999` | `LONG_MIN` truncated to 0 |
| `"-m 2"` | leading blank skipped, 2 |
| `-s0x1E`, `-s0x1p4`, `-s1e1` | 30, 16, 10 (percent) |
| `-s1e` | 1, then option `-e` with value `'\0'`: error 13 |
| `-sinf`, `-s1e400` | `invalid minimum support inf` |
| `-snan` | accepted (`nan > 100` is false); behaves like `-s0` on arm64 (UB, section 10) |

### 1.2 Processing order after parsing (`fpgrowth.c:2417-2463`)

Checks run in this order; the first failure exits:

1. pending string option without value: `missing option argument` (7)
2. no `infile`: `wrong number of arguments` (8)
3. `zmin < 0`, then `zmax < 0`: `invalid item set size %d` (10)
4. `smin > 100`: `invalid minimum support %g` (11). No lower bound.
5. `bdrcnt < 0` (border allocation failed): `not enough memory` (1)
6. `conf < 0 || conf > 100`: `invalid minimum confidence %g` (12) - checked
   even when not mining rules
7. stdin used twice (`infile` empty/`-` **and** `-R ""`): `double
   assignment of standard input` (5)
8. target letter: `invalid target type '%c'` (9)
9. measure letter: `invalid evaluation measure '%c'` (13)
10. aggregation letter: `invalid aggregation mode '%c'` (14)
11. algorithm letter: `invalid fpgrowth variant '%c'` (16)

Then: `-l` is clamped into the mode bits (`pack <= 0 -> 0`, `>= 16 -> 16`,
line 2453); `-P` is silently dropped for `-tr` (2455); the default info
format is substituted (2457-2461, section 2.2); verbose mode is forced on
(2462, there is no CLI switch to silence logs); the banner newline is
printed (2463).

Not validated at all: `-S` (any value, including `nan` and values below
`-s`), `-d`, `-p`, `-q`, `-l`, `zmax < zmin` (empty result, no error),
`-s0`/negative zero.

`%g` in messages is C `printf("%g")` of the parsed double, so
`-s100.0000001` reports `invalid minimum support 100` and `-s1234567`
reports `1.23457e+06` (**confirmed**).

### 1.3 Execution phases and file opening order

| Phase | Code | Observable effect |
| --- | --- | --- |
| read `-R` file (if any) | 2466-2483 | `reading <name> ... [N item(s)] done [T].` |
| read transactions | 2486-2504 | `reading <name> ... [m item(s), n[/w] transaction(s)] done [T].` |
| check `m > 0 && n > 0` | 2502 | else `no (frequent) items found` (15) |
| `fpg_data` (recode, filter, sort, reduce) | 1898-1966 | two log lines; `no (frequent) items found` if nothing frequent |
| open item set output | 2522 | output file created/truncated **only now** |
| mine and write | 2526 | `writing <name> ... [N set(s)/rule(s)] done [T].` |
| `-Z` statistics to stdout | 2528-2529 | before the output file is closed |
| close item set output | 2530 | `write error on file <name>` (4) on failure |
| write pattern spectrum | 2534-2548 | file opened only now; `writing <name> ... [N signature(s)] done [T].` |

Consequences that a port must keep: the output file is never created or
truncated when reading or preprocessing fails (also `infile == outfile`
works: input is fully read before the output is opened); an unwritable
output path is reported only after reading (**confirmed**); an unwritable
`-P` path is reported after the item set file was completely written,
with exit 2 (**confirmed**).

---

## 2. Option reference

Default column = value of the C variable in `main()` (`fpgrowth.c:2216-2248`).
"Mode bits" refers to `fpgrowth.h:59-74`.

| Opt | Value syntax | Default | Semantics | Constraints / notes | Code |
| --- | --- | --- | --- | --- | --- |
| `-t#` | one char | `s` | target: `s` frequent (also undocumented `f`), `c` closed, `m` maximal, `g` generators, `r` association rules | other char: error 9; `-t` alone = `s` | 2369, 2427-2435 |
| `-m#` | strtol base 0 | 1 | minimum set/rule size | `< 0`: error 10; `-m0` reports the empty set | 2370, 2419 |
| `-n#` | strtol base 0 | `INT_MAX` ("no limit") | maximum set/rule size | `< 0`: error 10; `< zmin` gives empty output, no error; any value below `INT_MAX` disables fast output (section 7.3; `-n2147483647` keeps it) | 2371, 2420 |
| `-s#` | strtod | 10 | min support; `>= 0` percent of total weight, `< 0` absolute | `> 100`: error 11; absolute = `ceil(s/100*w*(1-DBL_EPSILON))` (`fpgrowth.c:1915-1916`); for rules multiplied by `conf` unless `-o` (1918-1920); `-s0` reports zero-support sets (section 10) | 2372, 2421 |
| `-S#` | strtod | 100 | max support, same sign convention | not validated; absolute = `floor(S/100*w*(1+DBL_EPSILON))` (1996-1998) | 2373 |
| `-o` | flag | off | original rule support definition (body & head) | mode `FPG_ORIGSUPP` | 2374 |
| `-c#` | strtod | 80 | min confidence (percent) | outside `[0,100]`: error 12 (checked for every target); forced to 100 internally if target is not `r` (`fpg_create`, 1824-1825) | 2375, 2423 |
| `-e#` | one char | `x` | evaluation measure: `x` none, `b` = binary log of support quotient (**always**, even for rules), other letters via `re_getid` (`math/src/ruleval.c:491-526`) | invalid/absent letter: error 13. Rule measure `b` (imbalance ratio) is unreachable from the CLI (**confirmed**: `-tr -eb -v' %e'` prints 0) | 2376, 2436-2438 |
| `-a#` | one char | `x` | aggregation `x` none, `m` min, `n` max, `a` average | other: error 14 (doc lists `s`, not implemented) | 2377, 2439-2445 |
| `-d#` | strtod | 10 | threshold for `-e` (percent; divided by 100) | not validated | 2378 |
| `-i` | flag | off | invalidate evaluation below expected support (`FPG_INVBXS`) | | 2379 |
| `-p#` | strtol base 0 | `INT_MIN` (none) | evaluation pruning: `<0` weak fwd, `>0` strong fwd, `0` backward | ignored unless a measure is set (`fpg_mine` 2025-2026) | 2380 |
| `-q#` | strtol base 0 | 2 | item sort: 0 none, ±1 by frequency, ±2 by transaction-size sum | passed **negated** to `tbg_recode` (1936); with item reordering active (default `-Ac`, non closed/maximal) magnitude collapses to ±1 (1934-1935), so `-q1`≡`-q2` there (**confirmed**); `|q| > 2` acts as ±2 | 2381 |
| `-A#` | one char | `c` | variant `s` simple, `c` complex, `d` single tree top-down, `t` top-down | other: error 16; adjusted by target (section 2.1) | 2382, 2446-2452 |
| `-x` | flag | prune | disable perfect extension pruning | re-enabled for `-tg` | 2383 |
| `-l#` | strtol base 0 | 16 | items in k-items machine; `<=0` off, `>=16` → 16 | doc example `-l` without number = 0 | 2384, 2453 |
| `-j` | flag | reorder | disable item reordering w.r.t. conditional support | | 2385 |
| `-u` | flag | hut | disable head-union-tail pruning | only meaningful for `-tm` | 2386 |
| `-F#:#..` | `strtod` list, `:`-separated | none | per-size minimum support border starting at size `zmin` (same sign convention as `-s`) | parse quirks/UB: section 2.3; disables fast output | 2387, 2156-2192 |
| `-R#` | string | none | item selection file (sets) or item appearance file (`-tr`) | `""` = stdin (error 5 if input is stdin too); `-` is a file name | 2388, 2471-2483 |
| `-P#` | string | none | write pattern spectrum | `""` = stdout; silently ignored for `-tr` | 2389, 2455, 2534 |
| `-Z` | flag | off | print item set counts per size to **stdout** | section 7.4 | 2390, 2528 |
| `-N` | flag | preformat | do not pre-format integers | output identical (**confirmed**), speed only | 2391 |
| `-g` | flag | off | "scanable" item names | **no effect in 6.21** (bug, section 10) | 2392 |
| `-z` | flag | — | zlib output | only with `USE_ZLIB`; reference build: `unknown option -z` (6) | 2393-2395 |
| `-h#` | string | `""` | record header | **no effect in 6.21** (bug, section 10) | 2396 |
| `-k#` | string | `" "` | item separator (output) | used verbatim, no escapes | 2397 |
| `-I#` | string | `" <- "` | implication sign for rules | verbatim (doc claims `" "`) | 2398 |
| `-v#` | string | `" (%S)"` | info format (section 7.2) | default substituted, section 2.2 | 2399 |
| `-w` | flag | off | integer transaction weight in last field | `TA_WEIGHT` | 2400 |
| `-r#` | string | `"\n"` | record separators (input) | escape-decoded (section 5.2); replaces the class | 2401 |
| `-f#` | string | `" \t,"` | field separators (input) | idem | 2402 |
| `-b#` | string | `" \t\r"` | blank characters (input) | idem | 2403 |
| `-C#` | string | `"#"` | comment characters (input) | idem; `-C ''` disables comments | 2404 |
| `-!` | flag | — | print extra help to stdout, exit 0 | processed immediately when reached | 2368, 2088-2152 |

Option count: 34 option letters in the reference build (35 with
`USE_ZLIB`), plus 2 positional arguments.

### 2.1 Options forced or forbidden by others

From `fpg_create` (`fpgrowth.c:1819-1844`), `fpg_data`, `fpg_report`,
`fpg_mine` and `main`:

- Target `r`: confidence used; `-P` dropped; `-R` file read as appearance
  file; mining always goes through the item set tree (`fpg_tree`),
  whatever `-A` says; transactions not filtered by size; k-items machine off.
- Rule-based measure (`-e` letter other than `x`/`b`) with a set target:
  same item-set-tree path, `ISR_NOFILTER` reporter mode, no k-items machine,
  closed/maximal/generator filtering done by `ist_clomax`.
- Target `g`: perfect extension pruning forced on (overrides `-x`); `-At`
  becomes `-Ad`; reporter needs set sorting when reordering.
- Targets `c`/`m`: reordering off (`-j` implied); `-Ad` becomes `-As`.
- Any variant other than `c`: reordering off. Variant `t`: no k-items
  machine. Variant `c`: items packed in the recursion, not up front.
- HUT pruning only for `m`.
- Negative `-s` (strictly `< 0`; `-s-0` does not count) changes the default
  info format (section 2.2).

### 2.2 Default info format substitution (`fpgrowth.c:2457-2461`)

Only if `-v` was **not** given (pointer comparison with the default
literal, so an explicit `-v ' (%S)'` is kept as is):

| Target | `smin >= 0` | `smin < 0` |
| --- | --- | --- |
| sets (`s f c m g`) | `" (%S)"` | `" (%a)"` |
| rules (`r`) | `" (%X, %C)"` | `" (%b, %C)"` |

### 2.3 Support border `-F` (`getbdr`, `setbdr`)

`getbdr` (`fpgrowth.c:2156-2174`) counts all `:` in the rest of the
argument, allocates that many plus one doubles, then reads values with
`strtod`, requiring `:` between them. The rest of the cluster after the
last parsed number continues as option letters (`-F60:50Z` also sets
`-Z`; `-Fabc` parses nothing, then `-a` with value `b` fails with error
14, **confirmed**).

```c
for (i = 0; i < k; i++) {
  b[i] = strtod(s, end);
  if (*end == s) break;            /* empty parameter: b[i] unset */
  s = *end; if (*s++ != ':') break;
}
if (++i < k) *border = (double*)realloc(b, (size_t)i *sizeof(double));
return i;
```

When the loop stops at an *empty* parameter, `++i` still counts the unset
slot, so `-F` (no value), `-F60:` and `-F:5` return a count that includes
one uninitialized double (hypothesis/UB; on macOS arm64 the fresh heap
memory made it harmless in all runs, also with `MallocPreScribble=1`).
`setbdr` (2178-2192) converts value `n` to an absolute threshold for size
`zmin + n`: `ceil(v/100*w*(1-DBL_EPSILON))` for `v >= 0`, `ceil(-v)` for
`v < 0`, where `w` is the total transaction weight. The border is applied
in `report.c:1378-1381`: a set of size `z < bdrcnt` is suppressed when its
support is below `border[z]`. Example (**confirmed**): `$FPG -F60:50 -s20
test1.tab -` keeps 11 of the 17 sets; `-F-6:-5` gives the same result.

---

## 3. Messages, streams and exit codes

### 3.1 Streams

| Stream | Content |
| --- | --- |
| stdout | usage text (no args); `-!` help; item sets/rules when `outfile` is `-` or `""`; `-Z` statistics (always stdout); `-P ""` pattern spectrum |
| stderr | banner, all progress logs, all error messages |

### 3.2 Progress log lines

Logs are printed by `MSG`/`XMSG` (`fpgrowth.c:147-157`); `XMSG` is gated by
`FPG_VERBOSE`, which `main` always sets. Each line is `"<text> ... "`
followed by bracketed counts and `"done [%.2fs].\n"`, where the time is
`clock()` CPU time since the phase started (`SEC_SINCE`). Exact formats:

```
reading <name> ... [<k> item(s)] done [T].                          (-R file)
reading <name> ... [<m> item(s), <n>[/<w>] transaction(s)] done [T].
filtering, sorting and recoding items ... [<m'> item(s)] done [T].
sorting and reducing transactions ... [<n'>[/<w>] transaction(s)] done [T].
writing <out> ... [<N> set(s)] done [T].                             (direct path)
finding frequent item set(s) ... done [T].                           (tree path)
filtering with evaluation ... done [T].                              (tree path, -p <= 0 with measure)
filtering for <generator|maximal|closed> item sets ... done [T].     (tree path)
writing <out> ... [<N> rule(s)|set(s)] done [T].                     (tree path)
writing <psp> ... [<S> signature(s)] done [T].                       (-P)
```

- `<name>` is the given file name or `<stdin>`; `<out>` is the name,
  `<stdout>` (`""`/`-`) or `<null>` (no outfile, sets are only counted).
- `/<w>` appears only when total weight differs from the transaction count
  (`-w`, or after reduction, e.g. `[8/10 transaction(s)]`).
- `<m>` in the reading line is the size of the item base (with `-R`: the
  number of selected items, even if absent from the data); `<n>` counts
  every record including empty ones.
- The "reading transactions" line is not newline-terminated until the
  `m > 0 && n > 0` check passes (2499-2504); every "... " prefix printed
  before an error stays unterminated and the error's leading `\n` ends it.

Source lines: 2475, 2481-2482, 2491, 2499-2501, 1933, 1939-1940, 1945,
1961-1963, 2032-2037, 2040, 2050, 2054-2056, 2060-2065, 2068, 2075-2078,
2541, 2546-2547.

### 3.3 Error message format

`GENERROR` (`util/src/error.h:24-43, 69-78`): for a negative code the
format from `errmsgs[]` (`fpgrowth.c:254-273`) is printed as
`"\n<prgname>: <formatted>\n"`; for a positive code (used for reader
errors) the supplied string is printed as `"\n<prgname>: <string>\n"`. The
process exits with `abs(code)`; no cleanup in `NDEBUG` builds.

Because the banner's own newline is printed only after validation,
errors detected during parsing/validation produce
`banner2 "\n" prgname ": " msg "\n"`, whereas later errors produce an
**empty line** in between (**confirmed**):

```
./fpgrowth - find frequent item sets with the fpgrowth algorithm
version 6.21 (2022.11.22)        (c) 2004-2022   Christian Borgelt

./fpgrowth: cannot open file 20
```

Reader errors (`ib_errmsg`, `tract/src/tract.c:820-840`) prefix messages
whose format starts with `#` with `"<file>:<record>(<field>): "` and format
`%s` with the current field text (`trd_field`).

### 3.4 Exit codes

| Exit | Source | Message (after `"<prgname>: "`) |
| --- | --- | --- |
| 0 | success, usage, `-!` | — |
| 1 | E_NOMEM | `not enough memory` |
| 2 | E_FOPEN | `cannot open file <name>` (input, `-R`, output, `-P`) |
| 3 | E_FREAD | `read error on file ` + **current field** (bug: not the file name; usually empty, **confirmed** with a directory as input) |
| 4 | E_FWRITE | `write error on file <name>` (item set output only, at close) |
| 5 | E_STDIN | `double assignment of standard input` |
| 6 | E_OPTION | `unknown option -<c>` |
| 7 | E_OPTARG | `missing option argument` |
| 8 | E_ARGCNT | `wrong number of arguments` |
| 9 | E_TARGET | `invalid target type '<c>'` |
| 10 | E_SIZE | `invalid item set size <int>` |
| 11 | E_SUPPORT | `invalid minimum support <%g>` |
| 12 | E_CONF | `invalid minimum confidence <%g>` |
| 13 | E_MEASURE | `invalid evaluation measure '<c>'` |
| 14 | E_AGGMODE | `invalid aggregation mode '<c>'` |
| 15 | E_NOITEMS | `no (frequent) items found` (also for empty input, `-s90`, ...) |
| 16 | E_VARIANT / E_ITEMEXP | `invalid fpgrowth variant '<c>'` / `<f>:<r>(<c>): item expected` |
| 18 | E_DUPITEM | `<f>:<r>(<c>): duplicate item '<s>'` (appearance file) |
| 20 | E_WGTEXP | `<f>:<r>(<c>): transaction weight expected` |
| 21 | E_TAWGT | `<f>:<r>(<c>): invalid transaction weight <s>` |
| 22 | E_FLDCNT | `<f>:<r>(<c>): too many fields/columns` (appearance file) |
| 23 | E_APPEXP | `<f>:<r>(<c>): appearance indicator expected` |
| 24 | E_UNKAPP | `<f>:<r>(<c>): unknown appearance indicator '<s>'` |
| 17, 19, 25, 26 | item weights / penalties | unreachable from this program |
| 139 | SIGSEGV | e.g. total weight overflow (section 10) |

Codes from `tract.c:213-238` and `tract.h:451-468`; 16 is shared by two
unrelated errors.

### 3.5 Usage and help text

The usage text (50 lines) and `-!` help (79 lines) are static apart from
`argv[0]` and the printed defaults; `-!` prints `"\n"` to stderr (ending
the banner line) and the help to stdout, then `exit(0)`. The help includes
`re_help()` output (`math/src/ruleval.c:546-555`, `"  %c   %-48s"` plus
`(+)`/`(-)`), so the line for `x` ends in trailing spaces. Byte-exact copies
should be generated from the reference binary as golden files (`$FPG >
usage.txt`, `$FPG -! > help.txt`), since both depend only on argv[0].

---

## 4. Input file handling

- Opened in binary mode (`fopen(name, "rb")`, `tabread.c:114`); empty name
  means stdin, reported as `<stdin>`. Read in 64 KiB blocks
  (`TRD_BUFSIZE`, `tabread.h:52`).
- The same reader object and character classes are used for the `-R` file
  and the transaction file (`fpgrowth.c:2468-2470`), so `-r -f -b -C` apply
  to both.
- A read error (`ferror`) is reported as E_FREAD (exit 3).

### 4.1 Character classes

Every byte value has a flag set (`int flags[256]`), classes can overlap.
Defaults (`tabread.c:80-86`), as finally configured by `main`'s
`trd_allchs(tread, recseps, fldseps, blanks, "", comment)` (2470):

| Class | Default bytes | Option |
| --- | --- | --- |
| record separator | `\n` | `-r` |
| field separator | space, `\t`, `,` | `-f` |
| blank | space, `\t`, `\r` | `-b` |
| null value | none (`?` and `*` from `trd_create` are cleared by the `""` argument) | — |
| comment | `#` | `-C` |

Giving an option **replaces** that class completely (`trd_chars`,
`tabread.c:142-155`); bytes keep memberships in other classes. Example:
`-f" "` makes tab a non-separator blank, so `a\tb` becomes the single item
`a\tb`.

### 4.2 Escape decoding of option strings (`escape.c:122-173`)

Each class string is decoded one character at a time with `esc_decode`:
`\a \b \f \n \r \t \v`, octal `\o`, `\oo`, `\ooo` (`& 0xff`), hex `\xh`,
`\xhh` (`\x` without a hex digit yields `x`). Every other byte stands for
itself. Escape-decoding is **not** applied to `-h -k -I -v` or file names.

The default branch for any other escaped character is broken (see section
10, **confirmed**): `\c` followed by a byte `d` decodes to `d` (both
consumed); `\c` at the end decodes to a backslash; a lone trailing `\`
reads past the string terminator. Correct spellings for arbitrary bytes:
octal or hex escapes (`-f'\054'` and `-f'\x2c'` both mean comma).

### 4.3 Tokenizer (`trd_read`, `tabread.c:195-267`)

Per call one field is returned with a delimiter: `FLD` (more fields
follow), `REC` (last field of the record), `EOF`, `ERR`.

1. **Comments** (207-214): only if the previous delimiter was not `FLD`
   (start of a record) and the **first byte** of the record is a comment
   byte. The record is skipped up to and including the next record
   separator (record counter incremented); repeated. Leading blanks are
   not skipped first, so `  # x` is data (items `#`, `x`), and `a # b`
   contains the item `#` (**confirmed**). EOF inside a comment returns EOF.
2. **Leading blanks** are skipped (217-218). EOF after at least one blank
   returns an empty field with `REC` (the record counter is *not*
   incremented, see 4.6).
3. If the current byte is a separator, an **empty field** is returned:
   `FLD` if it is a field separator (field wins when a byte is both), else
   `REC` (219-223).
4. Otherwise the field is read up to the next separator or EOF (230-236).
   Only the first **1024 bytes** (`TRD_MAXLEN`) are stored; the rest is
   silently dropped. EOF ends the field with `REC`.
5. **Trailing blanks** are removed (240-242). Interior blanks are kept.
6. If the field ended with a field separator, following blanks are skipped
   (257-260). Then: a record separator ends the record (`REC`); another
   field separator is consumed and merged into the same separator; any
   other byte is pushed back (261-266). Because step 6 starts from the
   separator byte itself, merging only happens when that separator is also
   a blank: `a ,b` gives 2 fields, `a, ,b` and `a,,b` give an empty middle
   field.

Byte semantics: all bytes are opaque (invalid UTF-8 passes through
unchanged, **confirmed**). Item names are C strings, so an embedded NUL
truncates the name at the NUL (`a\0b` becomes item `a`, **confirmed**).
Line endings: CRLF works by default because `\r` is a blank; with a `-b`
that omits `\r`, the `\r` stays part of the last item (**confirmed**:
`b^M`). Old-Mac CR-only files are one record whose items contain `\r`
(`b\rc`, **confirmed**). A missing newline at EOF is fine; a final newline
does not create an extra empty record.

### 4.4 Transactions (`ib_read`, `tract.c:683-816`)

- One record = one transaction, default weight 1. Empty records (empty
  lines, blank-only lines, CRLF-only lines) are **empty transactions**:
  they count for `n` and the total weight (**confirmed**: `a b\n\n\na c\n`
  has 4 transactions).
- Empty field followed by `FLD` (e.g. `a,,b`, test1.csv line 5): error
  `item expected` (exit 16). An empty *last* field after other fields
  (trailing `,`) is ignored (721-724).
- Item names are the field strings; a new name is appended to the item
  base with the next identifier (section 8). With a `-R` selection,
  unknown names are skipped entirely (726-734).
- **Duplicates** in a transaction are silently dropped (first occurrence
  kept; 743-756; **confirmed** `a b a a` is `{a, b}`).
- No limit on items per transaction or number of transactions other than
  memory and `int` ranges (`ITEM`, `TID`, `SUPP` are `int`,
  `tract.h:99-109`).

### 4.5 Transaction weights `-w` (`tract.c:706-720`)

The field that ends a record (`REC`) is the weight. Format:
optional `(`, `[` or `{`, then `strtosupp` = `(int)strtol(s, &e, 0)`
(`tract.h:270`), then the matching closing bracket if one was opened, then
nothing else.

| Field | Result (**confirmed**) |
| --- | --- |
| `2`, `+5`, `(3)`, `[3]`, `{3}` | 2, 5, 3, 3, 3 |
| `0x10`, `010` | 16, 8 (base-0 parse) |
| `4294967297` | 1 (`long` truncated to `int`) |
| `0`, `-2` | accepted; zero/negative weights and totals (no validation) |
| `2.5`, `x`, `(3`, `(3]`, `3)` | `invalid transaction weight <field>` (21) |
| empty last field (empty line, trailing `,`) | `transaction weight expected` (20) |
| record with a single field `3` | empty transaction of weight 3 |

Sum of weights is accumulated in `int` without overflow checks; two
weights of 2000000000 make the total negative and crash the program
(section 10).

### 4.6 Record/field positions in messages

`<record>` = `trd_rec()` = internal counter minus one if the last
delimiter was `REC` (`tabread.h:120`); comment records are counted, so
numbers match physical lines for `\n`-separated files. `<field>` is the
1-based field index in the record. Off by one (**confirmed**): when the
last record consists only of blanks and has no newline, the reader returns
`REC` without incrementing the counter, so `a 1\nb 2\n   ` with `-w`
reports `:2(1): transaction weight expected` instead of `:3(1)`.

---

## 5. Item selection / appearance file `-R`

Read with the same tokenizer before the transactions. `-R ""` reads stdin.

### 5.1 Item selection (targets `s f c m g`, `ib_readsel`, `tract.c:568-592`)

- A free list of item names in any record/field layout; comments allowed.
- Listed items are created first, in file order (so they get the first
  identifiers); duplicates are silently skipped.
- Items not listed are ignored in the transactions (not even added to the
  item base). Transactions that lose all items remain empty transactions.
- An empty field (blank line, `a,,b`, trailing `,`) is `item expected`
  (16) (**confirmed**: blank line in the selection file is an error).
- Log: `reading <sel> ... [<k> item(s)] done [T].`; later "reading" line
  reports the selected item count.

### 5.2 Item appearances (target `r`, `ib_readapp`, `tract.c:596-632`)

- Record 1: exactly one field, the default appearance for unlisted items
  (two fields: `too many fields/columns`, 22). An empty first record is
  accepted and means `none` (empty string prefix-matches `-`,
  **confirmed**).
- Following records: `item indicator`, exactly two fields. One field:
  `appearance indicator expected` (23, doc claims the item is ignored);
  three fields: 22; empty record: `item expected` (16, doc claims empty
  records are ignored); repeated item: `duplicate item '<name>'` (18).
- Indicators (`tract.c:182-191`): body only `i in inp input a ante
  antecedent b body`; head only `o out output c cons consequent h head`;
  both `io i&o inout in&out ac a&c c&a canda bh b&h h&b both x`; neither
  `- n none neither ignore`. Matching (`appcode`, 277-294) is a
  case-sensitive **prefix match** against the lexicographically sorted
  list: the first name `>=` the given string must start with it. So
  `ign` = ignore, but `bo` = **body** (not both), `Both` is unknown (24)
  (**confirmed**).

---

## 6. Output file handling

- Item set file: `fopen(name, "w")` (text mode) at `isr_open`
  (`report.c:974-1008`); `""`/`-` = stdout; absent = no output, sets are
  only counted (`<null>`). Written through a private 64 KiB buffer
  (`BS_WRITE`, `report.c:115`) that is flushed only when full and at close
  (`isr_flush`, 177-200); close checks `ferror` plus `fclose`/`fflush`
  (1012-1028) and is the only point where write errors are detected (exit
  4, after the `writing ... done` log line, **confirmed** with stdout
  closed).
- Pattern spectrum file: `twr_open` (`tabwrite.c:56-74`, mode `"wb"`),
  stdio-buffered. `psp_report` checks `ferror` before the final flush and
  the result of `twr_delete`/`fclose` is ignored (`fpgrowth.c:2544`), so
  late write errors are lost (**confirmed**: `-P ""` with stdout closed
  exits 0).

---

## 7. Output formats

### 7.1 Item sets and rules

Item set line: `[hdr]item1<sep>item2...<info>\n`, items in the reporter's
current order (recursion order, not sorted; differs between `-A`, `-q`,
`-x`, `-j` settings). Rule line: `[hdr]head<imp>body1<sep>body2...<info>\n`;
a rule with empty body prints `head <- ` followed by the info (for example
`d <-  (10, 70)`). `hdr` is always empty in 6.21 (bug). Item names are
written raw (`-g` has no effect, bug).

### 7.2 Info format `-v`

Characters are copied except `%`. After `%`, up to two decimal digits give
the number of significant digits `k` (default 6, capped at 32; `getsd`,
`report.c:155-167`), then a code character. Unknown codes are copied
literally including `%` and digits (`%b` for sets, `%123S` → `%123S`, `%-5S`
→ `%-5S`, trailing `%` → `%`; **confirmed**).

| Code | Sets (`isr_sinfo`, 1925-1977) | Rules (`isr_rinfo`, 1981-2039) |
| --- | --- | --- |
| `%%` | `%` | `%` |
| `%i` | set size (int) | rule size (int) |
| `%a` | absolute support (int) | rule support (int) |
| `%q`, `%Q` | total weight (int) | `%Q` only |
| `%s` / `%S` | support fraction / percent | same |
| `%x` / `%X` | same as `%s` / `%S` | body support fraction / percent |
| `%b` | (literal) | body support (int) |
| `%h`, `%y`, `%Y` | (literal) | head support int / fraction / percent |
| `%c`, `%C` | (literal) | confidence fraction / percent |
| `%l`, `%L` | (literal) | lift / lift percent |
| `%e`, `%E` | evaluation / percent | evaluation / percent |
| `%w %W %r %R %z %p %P` | item set weight variants (0 here) | (literal) |

Floats use the custom formatter `isr_numout`/`mantout`
(`report.c:281-375`), not printf: `nan`, `inf`, `-inf`, `0` for
`|x| < DBL_MIN`; for `k > 11` `sprintf("%.*g")`; otherwise exponential
form (`e+XX`/`e-XX`, at least two exponent digits) when `x >= 10^k` or
`x < 0.001`, else fixed form with at most `k` significant digits, its own
rounding and trailing-zero removal. Examples (**confirmed**): `%S` of 70 →
`70`, `%0S`/`%1S` → `7e+01`, `%10s` of 1/3 → `0.3333333333`, `%12S` of
33.33.. → `33.3333333333`. Integer codes ignore `k`; `-N` only changes how
integers are produced, not the bytes.

### 7.3 Fast output path

`fastchk` (`report.c:135-151`) enables a specialised writer when there is
an output file, no border, no evaluation function, `zmin <= 1`,
`zmax == INT_MAX`, `hdr == ""`, `sep == " "` and the info format is
exactly `" (%a)"` or `" (%d)"`. It then prints `" (%d)\n"` with the support
directly (`report.c:1532-1534`). Observable differences from the normal
path (all **confirmed**):

- `-v ' (%d)'` prints the absolute support only on this path; with any
  other setting (e.g. `-m2 -v ' (%d)'`) `%d` is printed literally.
- When an item occurs in every transaction (perfect extension of the empty
  set), the empty set is written as a line ` (N)` and counted (bug,
  section 10).
- The pattern spectrum is computed from stale supports (bug, section 10).

The path is selected implicitly by `-s<negative>` (default info becomes
`" (%a)"`) or by `-v ' (%a)'`.

### 7.4 Statistics `-Z` (`isr_prstats`, `report.c:1880-1891`)

Printed to stdout after mining, before the item set file is closed:

```
all: <total>
  0: <count of size 0>
  1: ...
```

`"all: %zu\n"` then `"%3d: %zu\n"` for every size from 0 up to the largest
size with a non-zero count (sizes below `-m` print 0). With no result:
only `all: 0`. For `-tr` it counts rules by size. Ordering relative to the
item sets when `outfile` is stdout: the item sets still in the 64 KiB
private buffer are written after the statistics, so for small outputs the
statistics come **first**, and for larger outputs they are spliced into
the item set stream at a 65536-byte boundary, possibly mid-line
(**confirmed**: `... i26 i3 i34 (all: 10905\n  0: 0\n...1.5)\n`).

### 7.5 Pattern spectrum `-P` (`psp_report`, `patspec.c:802-833`)

One line per `(size, support)` with non-zero count, sizes ascending, then
supports ascending: `"%d %d %g\n"` (size, absolute support, count). The
count uses C `%g` with 6 significant digits, so counts `>= 10^6` are
printed lossy, e.g. `5.2003e+06` for 5200300 (**confirmed** with one
transaction of 25 items). Not produced for `-tr`. Example for test1.tab
with defaults (matches the HTML doc): `1 3 1`, `1 6 1`, `1 7 3`, `2 1 2`,
... `4 2 1` (13 lines).

---

## 8. Item identity, order and determinism

- Items are strings (byte sequences up to the first NUL, at most 1024
  bytes). They are stored in an identifier map (`symtab.c`, compiled with
  `-DIDMAPFN`), hash `h = h*251 + byte` (`symtab.c:78-94`), initial table
  8191 bins (`tract.c:395`). Identifiers are assigned **in order of first
  insertion** (`symtab.c:355-359`): `-R` file order first, then first
  appearance in the transactions. Hashing does not influence any output.
- Item codes are then recoded by frequency with `idm_sort` →
  `ptr_qsort` (`symtab.c:501-518`, `arrays.c:516-590`) using comparators
  that ignore the identifier (`tract.c:313-365`); `ptr_qsort` is an
  unstable quicksort/insertion sort, so **ties in frequency are ordered by
  this exact algorithm**. Output order (lines and items within a line)
  depends on it. Deterministic for a given input, but a Go port must
  reproduce `ptr_qsort` (and `ptr_bisect`, used for appearance indicators)
  bit for bit, not use `sort.Slice`.
- `-q0` keeps identifier order (first appearance).

---

## 9. Example data sets (`fpgrowth/ex`)

All seven inputs encode the **same 10 transactions** (`a b c`, `a d e`,
`b c d`, `a b c d`, `b c`, `a b d`, `d e`, `a b c d`, `c d e`, `a b c`), in
different formats:

| File | Format | Direct command |
| --- | --- | --- |
| `test1.tab` | space separated, one transaction per line | `fpgrowth test1.tab out` |
| `test2.tab` | mixed separators `, . : ;` | `fpgrowth -f",.;:" -l test2.tab out` (doc; `-l` = no k-items machine) |
| `test3.tab` | last field = weight, 8 lines | `fpgrowth -w test3.tab out` |
| `test1.csv` | fixed 4 columns with empty cells | fails with `test1.csv:5(3): item expected`; works with `-b,` or `-b" ,"` |
| `test4.tab` | `tid item` pairs | `tid2set test4.tab x.tab` first |
| `test5.tab` | header of item names + `T`/`F` flags | `flg2set test5.tab x.tab` first |
| `test6.tab` | one item per line, blank line between transactions | `row2set test6.tab x.tab` first; or replace blank lines by `%` and use `-r"%" -f"\n" -b"\n"` |

Conversion scripts (bash + **gawk**; gawk is not installed on the audit
machine, they were run with `awk` substituted and produced files equal to
test1.tab):

- `tid2set in out`: `sort` the file, then group consecutive lines with the
  same first field (transaction id) and print their second fields.
- `flg2set in out`: line 1 = item names; for each later line print the
  names of columns whose value is `T`, `t` or `1`.
- `row2set in out`: one item per line, empty line ends a transaction;
  prints a final newline at END (by inspection: an input ending in a blank
  line would yield an extra empty transaction); uses `printf($1)`, so items
  containing `%` are misformatted.
- `tab2set in out`: every field becomes `X<column>=<value>`.
- `hdr2set in out`: line 1 = column names; every field becomes
  `<name>=<value>`.
- `run.R` (Rscript): shows calling the program from R. It writes the 10
  transactions TAB-separated **without a final newline** to `test.txt`,
  runs `fpgrowth -s10 -m2 -n4 -k"<TAB>" -f"<TAB>" -v"<TAB>%a" test.txt
  test.out` through `system()` (the `\t` in the R strings are real TAB
  bytes), then reads `test.out` and splits lines on TAB. Equivalent run
  output starts `c<TAB>d<TAB>4`, `b<TAB>c<TAB>6`, `b<TAB>d<TAB>c<TAB>3`.

### 9.1 Candidate golden commands

Run from a directory containing the example files; `-` as outfile so the
result is on stdout. Line counts and first lines are from the reference
binary (**confirmed**). Store stdout, stderr (with `\[\d+\.\d\ds\]`
normalized) and the exit code.

| Command | Exit | Lines | First lines / note |
| --- | --- | --- | --- |
| `fpgrowth test1.tab -` | 0 | 21 | `d (70)`, `c d (40)`, `c (70)` |
| `fpgrowth -s-2 test1.tab -` | 0 | 17 | `d (7)`, `c d (4)` (fast path) |
| `fpgrowth -s0 test1.tab -` | 0 | 23 | includes `e c a (0)`, `e c a d (0)` |
| `fpgrowth -m2 -n3 -s20 test1.tab -` | 0 | 11 | `c d (40)`, `b c (60)` |
| `fpgrowth -m0 -v' (%a)\|' test1.tab -` | 0 | 22 | last line ` (10)\|` (empty set) |
| `fpgrowth -tc test1.tab -` | 0 | 16 | `e a d (10)`, `e c d (10)` |
| `fpgrowth -tm test1.tab -` | 0 | 3 | `e a d (10)`, `e c d (10)`, `a b c d (20)` |
| `fpgrowth -tg test1.tab -` | 0 | 16 | `d (70)`, `c d (40)` |
| `fpgrowth -tr test1.tab -` | 0 | 9 | `c <- b (70, 85.7143)`, `b <- c (70, 85.7143)` |
| `fpgrowth -tr -s-2 -c60 test1.tab -` | 0 | 22 | `d <-  (10, 70)`, `c <-  (10, 70)` |
| `fpgrowth -tr -k, -I' => ' test1.tab -` | 0 | 9 | `c => b (70, 85.7143)`; `b => a,c (40, 100)` |
| `fpgrowth -k, test1.tab -` | 0 | 21 | `d (70)`, `c,d (40)` |
| `fpgrowth -v' %a %s %S %Q %i' test1.tab -` | 0 | 21 | `d 7 0.7 70 10 1` |
| `fpgrowth -Z test1.tab` | 0 | 6 | `all: 21`, `  0: 0`, `  1: 5`, `  2: 9`, `  3: 6`, `  4: 1` |
| `fpgrowth -P '' test1.tab` | 0 | 13 | `1 3 1`, `1 6 1`, `1 7 3`, ... |
| `fpgrowth -eb -d0 -v' %e' test1.tab -` | 0 | 10 | `d 0`, `c 0`, `b c 0.292181` |
| `fpgrowth -tc -el -d0 -p0 test1.tab -` | 0 | 16 | tree path logs (3.2) |
| `fpgrowth -F60:50 -s20 test1.tab -` | 0 | 11 | `d (70)`, `c (70)` |
| `fpgrowth -As` / `-Ad` / `-At` `test1.tab -` | 0 | 21 | same sets, different order (`-At` starts `e a d (10)`) |
| `fpgrowth -q0` / `-q-2` `test1.tab -` | 0 | 21 | `a (60)`, `b a (50)` / `e (30)`, `a e (10)` |
| `fpgrowth -f',.;:' -l test2.tab -` | 0 | 21 | identical to test1 |
| `fpgrowth -w test3.tab -` | 0 | 21 | identical to test1; log `[5 item(s), 8/10 transaction(s)]` |
| `fpgrowth -w -s-3 test3.tab -` | 0 | 15 | `d (7)`, `c d (4)` |
| `fpgrowth test1.csv -` | 16 | 0 | `test1.csv:5(3): item expected` |
| `fpgrowth -b, test1.csv -` | 0 | 21 | identical to test1 |
| `fpgrowth -r% -f'\n' -b'\n' test6pct.tab -` | 0 | 21 | identical to test1 (`test6pct.tab` = test6 with blank lines replaced by `%`) |
| `fpgrowth -R sel.txt test1.tab -` (sel: `a c\nd a\n`) | 0 | 7 | `d (70)`, `c d (40)` |
| `fpgrowth -tr -R app.txt -c0 test1.tab -` (app: `in\na out\n`) | 0 | 10 | `a <- d (70, 57.1429)` |
| `fpgrowth` (no args) | 0 | 50 | usage on stdout |
| `fpgrowth -!` | 0 | 79 | help on stdout |

---

## 10. Suspected bugs and undefined behavior

| # | Status | Issue | Evidence | Reproducer |
| --- | --- | --- | --- | --- |
| B1 | confirmed | `-h` record header never printed: `isr_setfmtx` copies the header into `rep->str` but never sets `rep->hdr`, which stays `""` | `report.c:663, 884`; `rep->hdr` used at 148, 1107-1114, 1634... | `$FPG -hXX test1.tab -` prints `d (70)` |
| B2 | confirmed | `-g` has no effect: names are (not) formatted in `isr_createx` while `rep->scan` is still 0; `isr_setfmtx` sets `scan` later and nothing re-formats | `report.c:661, 711-727, 876` | items `a"b`, `c\d\x01e` printed raw with and without `-g` |
| B3 | confirmed | escape decoding default branch returns the byte *after* the escaped one; `\c` at end returns `\`; lone trailing `\` reads past the NUL into the next argv | `escape.c:134-135, 168-171` | `-f'\,'` makes `\` the separator; `-f'\,;'` only `;`; with file `zq.tab` = `1z2 3q4`, `$FPG -s-1 -f'\' zq.tab -` makes `z q . t a b` separators (items `1`, `2 3`, `4`) |
| B4 | confirmed (no crash seen) | literal non-ASCII bytes in `-r -f -b -C`: `esc_decode` returns a signed `char`, `trd_chars` indexes `flags[negative]` (OOB write before the array/struct); the byte never becomes a separator. Platform dependent (works where `char` is unsigned) | `escape.c:131-133`, `tabread.c:153-154` | `-f$'\xc3\xa9'` has no effect; `-f'\xc3\xa9'` works |
| B5 | confirmed | fast output reports the empty set ` (N)` (and counts it in logs/`-Z`) when some item is in every transaction; count-only mode does not | `report.c:1336-1364` lacks the `cnt >= zmin` check of `report()` 1448; size check 1489-1491 admits `cnt = 0` | `printf 'a b\na c\n' \| $FPG -s-1 - -` ends with ` (2)`; `-x` or `-v ' (%a) '` avoid it |
| B6 | confirmed | wrong pattern spectrum on the fast path with an output file: `fastout` uses `rep->supps[cnt]`, which is not filled for perfect-extension levels (the normal path fills it at 1553-1556) | `report.c:1346, 1550-1551` | `$FPG -s-1 -P p test1.tab out` gives `2 1 3`, `3 2 3`, `3 3 3`, no `4 2 1`; without `out`, or with `-s10`, correct |
| B7 | confirmed | `-Z` with stdout output: statistics printed before the private buffer is flushed (first, or spliced mid-line at 64 KiB) | `fpgrowth.c:2528-2531`, `report.c:177-200` | section 7.4 |
| B8 | confirmed | E_FREAD message formats the current field instead of the file name | `tract.c:217, 838` | directory as input: `read error on file ` (exit 3) |
| B9 | confirmed | pattern spectrum write/close errors ignored | `fpgrowth.c:2542-2545`, `patspec.c:832` | `$FPG -P '' test1.tab >&-` exits 0 |
| B10 | confirmed | record number off by one when a blank-only last record has no newline | `tabread.c:217-218`, `tabread.h:120` | `printf 'a 1\nb 2\n   ' \| $FPG -w - ` reports `:2(1)` |
| B11 | confirmed | transaction weight sum overflows `int` (UB), negative total, then SIGSEGV | `tract.c:806` (and 524), `tract.h:107-108` | `printf 'a 2000000000\na b 2000000000\n'` with `-w` → exit 139 |
| B12 | confirmed | negative/zero weights accepted; totals `<= 0` give meaningless results (`2/-1 transaction(s)`, 0 sets; all-zero weights report sets with support 0) | `tract.c:711-718` | `a b -2` / `a 0` lines with `-w` |
| B13 | confirmed | numeric options silently truncate `long` to `int` (`-m4294967298` = 2, `LONG_MAX` → -1); weights likewise | `fpgrowth.c:2370-2384`, `tract.h:270` | section 1.1 |
| B14 | confirmed | 1024-byte field limit truncates silently; distinct long names can merge | `tabread.c:230-232`, `tabread.h:53` | names `x`×1030+`END` and `x`×1024+`ZZZ` become one item (1024 × `x`) with support 2 |
| B15 | confirmed | `-s0` (and `-snan` on arm64) report item sets with support 0 that never occur | absolute support 0 accepted (`fpgrowth.c:1915-1920`) | `$FPG -s0 test1.tab -` has `e c a (0)` |
| B16 | hypothesis (UB) | `getbdr` counts an unparsed slot after an empty parameter (`-F`, `-F60:`, `-F:5`): uninitialized double used as a border; `realloc` failure would leak and leave `border` NULL | `fpgrowth.c:2166-2173` | no visible effect on macOS arm64 |
| B17 | hypothesis (UB) | NaN/inf converted to `int`: `-snan`, `-s-inf`, `-Snan`, `-cnan` (`ceilsupp`/`floorsupp` casts). arm64 saturates (`-s-inf`: no items, `-Snan`: 0 sets); x86-64 yields `INT_MIN` | `fpgrowth.c:1915-1920, 1996-1998`, `tract.h:272-276` | results differ by architecture |
| B18 | hypothesis | `appcode` passes `s-2` (pointer before `trd->field`) to the comparator | `tract.c:288` | formally UB, works in practice |
| B19 | confirmed (code) | `-eb` always means log support quotient; rule measure "imbalance ratio" (`b` in `-!` output) cannot be selected | `fpgrowth.c:2436` | `-tr -eb -v' %e'` prints `0` |

Not bugs but surprising (all **confirmed**): comments only at the very
first byte of a record; `-R-` is a file named `-`; the `" (%d)"` fast-path
special case; `-s-0` keeps the relative info format; `--` is an error;
`zmax < zmin` is not an error.

---

## 11. Upstream documentation vs. code

| Topic | `fpgrowth.html` | Code (wins) |
| --- | --- | --- |
| `-I` default | `" "` | `" <- "` |
| `-a s` (split into subsets) | listed | not implemented (error 14) |
| `-l` applicability | "variants i,r,o" | variants `s`, `d` (usage text) |
| default info format | `"  (%S)"` (two spaces) | `" (%S)"` (one space) |
| appearance file: item without indicator | ignored | error 23 |
| appearance file: empty records | ignored | error 16 |
| `-h`, `-g` | functional | no effect (B1, B2) |
| test2 example `-l` | — | means `-l0` (no k-items machine) |

---

## 12. Porting notes for `cmd/fpgrowth`

### 12.1 Must be preserved byte for byte

1. **Argument grammar** exactly as in 1.1: clusters, char-valued options,
   attached-only numeric values, rest-of-arg-or-next-argv strings, the `-`
   → empty-name rule for positional arguments only, positional count,
   immediate errors during the scan. Go's `flag` package cannot express
   this; write a hand-rolled parser.
2. **C-compatible number prefixes**: implement `strtod` (whitespace,
   sign, decimal/hex floats, exponent only if digits follow, `inf`,
   `infinity`, `nan`, `nan(...)`, overflow to ±Inf) and `strtol(…, 0)`
   (whitespace, sign, `0x` only when a hex digit follows, octal,
   saturation to `int64` limits) returning the consumed length, followed by
   the C `int` conversion (low 32 bits, two's complement). Note that
   `long` is 32-bit on Windows (LLP64); the reference is LP64.
3. **Validation order, messages, exit codes** (1.2, 3.3, 3.4), including
   `%g` formatting of doubles (C `%g`: 6 significant digits, `inf`,
   `e+06`; Go needs `strconv.FormatFloat(x, 'g', 6, 64)` plus special
   cases for `±Inf`/`NaN`), the NUL byte for `-e`/`-a`/`-A` without a
   letter, the banner without newline, the extra empty line for late
   errors, unterminated `"... "` log prefixes before errors, and the
   messages that carry `file:record(field)`.
4. **Log line texts** (3.2) except the time value; keep `"%.2f"` formatting.
5. **Tokenizer** (4.1-4.4): byte-oriented (never iterate runes), class
   replacement semantics, escape decoding including the B3 quirks unless
   intentionally fixed, comment rule, blank absorption, empty-field rules,
   1024-byte truncation, NUL truncation, EOF rules, record counting
   including B10.
6. **Weights** (4.5): bracket syntax, base-0 parse, `int` truncation,
   error texts, single-field records.
7. **Item identifier order** and **`ptr_qsort`/`ptr_bisect` replicas**
   (section 8); appearance prefix matching (5.2).
8. **Output bytes**: line layouts (7.1), `-v` parsing and unknown-code
   copying (7.2), `isr_numout`/`mantout` float formatting (a dedicated
   port, not `strconv`), `-Z` format, `-P` format with C `%g`.
9. **File timing**: open output only after preprocessing, `-P` after the
   item set file is closed; no output file on early errors; `""`/`-`
   handling; `<stdin>`, `<stdout>`, `<null>` names.
10. **Defaults** including the info-format substitution keyed on "-v not
    given" (not on the string value).

### 12.2 May reasonably differ (record each in `docs/differences.md`)

- Time values in logs (C uses process CPU time from `clock()`; Go could use
  `syscall.Getrusage` or wall time); tests must normalize `[\d+\.\d\ds]`.
- `argv[0]`: Go's `os.Args[0]` matches C's `argv[0]` for the same
  invocation; differential tests should invoke both binaries under the
  same name or normalize it.
- Undefined behavior (B4, B11, B16, B17, B18): pick defined behavior,
  e.g. treat separator bytes as unsigned (literal non-ASCII separators
  work), reject or saturate overflowing weight sums with an error, treat
  a missing `-F` value as "no value", reject `nan` for `-s/-S/-c`.
- Deterministic upstream bugs (B1, B2, B3, B5, B6, B7, B8, B9): either
  replicate (needed for byte-exact differential tests) or fix behind an
  explicit decision in `docs/decisions.md`. Recommendation: replicate by
  default for the parity milestone, then fix B5, B6 and B8 (clearly wrong
  results/messages) and B9 as documented differences; keep B1/B2
  replicable or make `-h`/`-g` work only if the project decides so. For B7
  a Go writer with an identical 65536-byte private buffer flushed only
  when full reproduces the exact interleaving cheaply.
- `-z` (zlib) is absent in the reference build; if added, it is an
  extension.

### 12.3 Top porting risks

1. Float formatting in `-v` output (`isr_numout`), used by the default
   `" (%S)"` on every line.
2. Tie ordering of items (`ptr_qsort`) and recursion order determine line
   order and item order within lines; any deviation breaks byte-exact
   goldens even when the set of results is equal.
3. Output-format-dependent code paths (fast output, count-only mode when
   no outfile) change counts, the empty-set line and the pattern spectrum.
4. Hand-written `strtod`/`strtol` prefix parsing and `int` truncation.
5. Exact stdout/stderr byte streams: banner without newline, blank lines
   before late errors, `-Z` ordering versus buffered item set output.
6. Byte vs. string semantics in Go: embedded NUL must truncate names, item
   names are arbitrary bytes, no UTF-8 validation or normalization.
7. Platform-dependent upstream behavior (signed `char`, `long` width,
   NaN-to-int conversions): differential tests must pin the reference
   platform or avoid these inputs.
