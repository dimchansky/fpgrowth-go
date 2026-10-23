# Audit: deviations from upstream and feature completeness (Phases 1.3 and 1.4)

Baseline: clean clone at `f7bb606`
(`scratchpad/audit/diff`). Nothing in the implementation was modified. Throwaway
material is under `_audit/diff/` (binaries `bin/`, repro data `w/`, library
probe `libcheck/`, fd probe `fdcheck/`, difftest summaries `sum.txt`, `suml.txt`).

Abbreviations: `C` = `reference/build/release/fpgrowth/src/fpgrowth` (pristine
6.21, `-O3 -DNDEBUG -funroll-loops -ffp-contract=off`), `DBG`/`ASAN` = debug /
asan builds, `G` = Go CLI built with `go build -o _audit/diff/bin/gofpg
./cmd/fpgrowth`. `t1.tab` = `fpgrowth/ex/test1.tab`.

## 0. Environment and baseline checks

| Check | Command | Result |
| --- | --- | --- |
| Upstream integrity | `cd reference/upstream && shasum -a 256 -c SHA256SUMS` | 71/71 OK; 71 files on disk, none unlisted |
| Provenance | `PROVENANCE.md` | version 6.21 (2022.11.22); archive repacked Aug 2026; only `fpgpsp.c/.h` carry later dates (2024.12.12, `fpgpsp` v1.3). Archive tarball itself not stored, so its SHA cannot be rechecked |
| C reference build | `cd reference && ./build.sh` | release, debug, asan, native built, exit 0 |
| Go build | `go build ./...`, `go vet ./...` | OK |
| Root tests | `go test ./...` | all packages pass **except** one timing test: `TestCancelTreeBuild/AlgorithmSingleTree` "returned 54.109ms after the cancellation" (limit 50 ms) at load average ~24; passes on rerun (`-count=5`) → DIF-9 |
| Differential test, fresh seed | `DIFFTEST_SEED=424242 DIFFTEST_CASES=3000 DIFFTEST_LARGE=30 go test ./difftest/` | 0 FAIL / 3000 (2246 byte-equal, 91 semantic-equal, 55 tolerated, 406 error-equal, 202 deviation-verified); large 30/30 byte-equal. Largest non-log value difference 0; log measures ≤ 9.35e-13 relative |
| CLI usage / `-!` text | `./fpgrowth` vs Go binary named `fpgrowth` | byte-identical (both) |
| Example datasets | `-s-1` on `ex/test1..6.tab` (`-w` for test3) | md5 identical C vs Go |
| Info codes | sets: `%i %a %s %S %w %W %r %z %Z %e %E %Q %q %%`; rules: all codes | identical except the documented R4 values |

## 1. The "~40 confirmed upstream bugs" claim

`grep -rniE "[0-9]+ (confirmed )?(upstream )?(bugs|defects)|~ ?[0-9]+"` over
README, CHANGELOG, docs, reference/README finds **no numeric bug count**.
README says only "fixes confirmed upstream defects (listed in
docs/differences.md)". The "~40" figure is therefore not a repository claim;
if it is used externally (release notes, announcement) it must be qualified.

Independent count after deduplication (section 3 has the mapping):

| Bucket | Count |
| --- | --- |
| Distinct, confirmed, user-visible upstream defects that the port fixes **and documents** | **33** |
| Additional confirmed upstream crashes the port avoids but does **not** document (T4 sort recursion, F5 pre-format overflow) | 2 |
| Latent / hypothesis-only / unreachable-from-CLI defects that are listed as deviations (R9 `logGamma(170.5)`, R10, R11 OOB read, R11 ignored results, C2) | 5 |
| Total if every row is counted | 40 |

So "~40" is defensible only when latent issues and undocumented fixes are
included; the strictly justified figure for "confirmed user-visible defects
fixed and documented" is **33** (root-cause groups; manifestations such as
M4/M9, M7/I6/C4/I7, M8/C1, O7/C3 counted once; O3 counted as two code
defects). The raw number of documented deviation rows is 55 (M 9, R 11, F 1,
O 10, I 8, C 9, L 7), of which only 38 rows are upstream defects at all.

## 2. Class counts (55 documented rows)

| Class | Rows | IDs |
| --- | --- | --- |
| A. Confirmed upstream correctness/safety defect | 36 | M1 M2 M3 M4 M5 M6 M7 M8 M9 R1 R2 R3 R4 R5 R6 R7 R8 R9 O1 O2 O3 O4 O5 O6 O7 O10 I1 I2 I3 I4 I5 I6 I7 C1 C3 C4 |
| B. Intentional compatibility/semantic improvement | 4 | O9 (memory), C6 (stop at first failed write), L-2, L-3 |
| C. Necessary Go/platform difference | 6 | F-1, I8, C5, C7, C9, (O8 is "preserved", listed here as not-a-deviation) |
| D. New functionality without upstream counterpart | 6 | C8, L-1, L-4, L-5, L-6, L-7 |
| E. Incompletely verified / potentially incorrect claim | 3 | R10 (defect only in patched C, not upstream), R11 (OOB read not reproducible at CLI, output unaffected; second half error-handling only), C2 (UB by code reading, no visible effect) |

Several A rows have a B component (the *fix* is a policy choice): M6 (reject
vs. other semantics), R1 (which measure `b` means), R2 (which of two
conflicting upstream docs to follow), R4/R5 (value conventions +Inf / 0),
R9 Yates clamp, O10 (only for sets). This is the main readability problem
of `docs/differences.md` (section 5).

## 3. Deviation table

Root-cause groups (RC): RC1 top-down map; RC2 zero support; RC3 generator
filter; RC4 maximal empty-base `sto` check; RC5 zmax depth limit; RC6 negative
weights; RC7 32-bit widths; RC8 float→int UB / NaN; RC9 `-eb` letter;
RC10 `-p` call order; RC11 head-only; RC12 conviction; RC13 CF; RC14 Fisher
tie ε; RC15 info rounding; RC16 `-S` support; RC17 Fisher 1−Σ; RC18 Yates;
RC19 lnΓ table; RC20 header; RC21 scan names; RC22 fast output path;
RC23 count-only zmax; RC24 binomial; RC25 `%d`; RC26 `-Z` buffering;
RC27 unchecked I/O errors; RC28 escapes; RC29 signed char index; RC30 last
record; RC31 field limit; RC32 messages; RC33 `-F` parsing.

| ID | Title | Class | RC | Upstream location (verified) | Reproduced | Go fix | Regression test (verified to exist) | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| M1 | `-At` copies item map instead of item list | A | RC1 | `fpgrowth.c:1660` `memcpy(tree->items, d, ...)` | yes: `printf 'x a b\nx a\nx b c\nx a b c\nx c\n'`, `C -At -s20` exit 139; also `-At -m2 -q1 -s10` on t3 exit 139. Go set = C `-As` set (normalized diff empty) | `internal/fpg/topdown.go` | `TestDeviations` "B1 …", `internal/fpg/testdata/golden/deviations.txt` (B1 rows), difftest M1 (23 dev-verified in my run) | solid |
| M2 | `-s0` / zero-frequency items | A | RC2 (= core B2, cli B15, tract T2/T15, ref F4) | `fpgrowth.c:1915-1920` (no clamp), `tract.c:881` | yes: `-As -s0` exit 139; `-Ac -s0` prints `a c b (0)`; Go 7 correct sets (hand-checked) | `internal/fpg/thresh.go`, `internal/tract/itembase.go` (`smin=max(smin,1)`) | `TestDeviations` "B2 …", `TestThresholds`; CLI `-tr -c0` cases in `cmd/fpgrowth/testdata/cases.txt` | doc cites golden `deviations.txt`, which has **no** B2 row (DIF-6). `-tr -c0` side effect on rule order documented |
| M3 | generators miss sets with `-m≥2` | A | RC3 | `fpgrowth.c:1946-1949` `tbg_filter` for all targets | yes: `printf 'a\na b\na b\nb c\n'`, `-tg -s-1 -m2`: C nothing, Go `b a (2)`; hand-check: {a,b} supp 2 < supp a,b = 3 → generator | `internal/fpg/fpg.go` | `TestDeviations` B3, golden B3 rows | logged transaction count differs (documented) |
| M4 | maximal: all frequent items top-level pex → no output | A | RC4 | `report.c:1473` `if (r >= rep->sto) return 0` with `sto=0` | yes: `printf 'a\na b\na c\na\n'`, `-tm -s-2`: C empty, Go `a (4)` | `internal/report/report.go` | `TestDeviations` B4, golden B4 rows | |
| M9 | maximal with `-x` + 16-items machine | A | RC4 (dup of M4: fim16 registers items as pex of ∅, then report.c:1473) | as M4 | yes: single transaction `a`, `-tm -x -As` / `-Ad`: C empty, Go `a (100)` | as M4 | `TestDeviations` "M9 …", golden `m9.tab` rows | same root cause as M4; do not count twice |
| M5 | closed/maximal with `-n` report non-closed/non-maximal sets | A | RC5 | `fpgrowth.c` (`isr_xable(2)` at depth `xmax-1`, core §6 B5) | yes: `zm` data, `-w -tc -s-1 -n2 -q-1 -v' [%a]'` with `-As -l0` / `-Ac -j -l0` / `-At`: C reports `i2 i1 [4]`, Go not; brute force (Python) closed sets ≤2 = Go output exactly. Maximal `mz` repro: C `i3 i7 [2]`, Go empty | `internal/fpg/mine.go` (`xable2`, `deeper`), `internal/fpg/extcheck.go` | `TestExtCheck*` (5 tests), `TestDeviations` B5, `TestReporter_BoundaryPexNotEnough` | README and `docs/algorithm.md` describe the **old** approach (DIF-2) |
| M6 | negative weights | A (+B policy) | RC6 (core B7, tract T1/T7/T13/T17, cli B12) | `tract.c:711-718`, `fim16.c:252` | yes: `a b -1`/`a 2`: C prints `a (1)` and ` (1)` (nonsense); Go: "invalid transaction weight -1 (must not be negative)" exit 21 | `internal/tract/itembase.go`, `internal/tabread/reader.go` | golden `cmd/fpgrowth/testdata/golden-go/dev-negw.*` | zero weight still accepted (documented in API) |
| M7 | 32-bit supports overflow | A | RC7 (tract T5, cli B11, ref F6, rules B13) | `tract.c:806-811`, `tract.h:107-108` | yes: `a 2000000000`/`a b 2000000000`: C logs `2/-294967296 transaction(s)`, exit 139; Go `a (4000000000)` | `internal/tract` (int64, `addChecked`) | golden `dev-bigw.*` | |
| M8 | out-of-range float→int threshold conversion (UB) | A (UB) | RC8 (core B9, cli B17, report B18) | `fpgrowth.c:1915-1920, 1996-1998` | partially: on arm64 C saturates, Go matches (`-s-3000000000` both "no (frequent) items found", exit 15; `-S-1e12` 7 sets both). x86-64 difference not reproduced (hypothesis in docs) | `internal/fpg/thresh.go` | `TestThresholds` (20,000 random conversions; B9 saturation) | doc names `TestDeviations`/golden; actual test is `TestThresholds` (DIF-6) |
| R1 | `-eb` always log support quotient; imbalance ratio unreachable | A (+B interpretation) | RC9 | `fpgrowth.c:2436` `e = (eval=='b') ? FPG_LDRATIO : ...`; `ruleval.c:130-135, 456` | yes: `-tr -s20 -c50 -eb -d0 -v' %e'`: C all `0`; Go `d <- a 0.111111` = hand calc \|7−6\|/(6+7−4)=1/9 per `re_imbal` | `internal/fpg/config.go` | `TestReport_ImbalanceB1`, golden `dev-eb-rules` | |
| R2 | `-p` has no effect | A (no-op) + B (semantics) | RC10 | `fpgrowth.c:2051-2057` (`ist_filter` before `ist_seteval` at 2069-2070; only `prune<=0`) | yes: C md5 identical for `-p` none/0/-1/2 on t1 and r200; Go changes output. Independent check: `-tm -s2 -el -am -d105 -p0` on r200: Go 26 sets = brute-force maximal sets of the evaluation-passing family; C 24 | `internal/istree/filter.go`, `internal/fpg/tree.go` | `TestFilter_CallOrderB2`, patched-C harness `reference/patches/istree/fpgrowth-p-order.patch` | Go follows the **help text** (`<0` weak fwd, `>0` strong fwd, `0` backward); the HTML doc says the opposite. Positive `-p` on fpgrowth's path is new behavior (only apriori's `ist_commit` used it). Doc says "as documented" without saying which (DIF-7) |
| R10 | generator filter vs. skip flag after R2 fix | E (A-latent) | RC10 | `istree.c` `ist_clomax` (patch part 2) | only in patched C (by design) | `internal/istree/filter.go` | `TestCloMax_GeneratorsTotalWeight` | not an upstream-visible bug; consequence of the port's own fix; should not be counted as an upstream defect |
| R3 | head-only items in rule bodies | A | RC11 | `istree.c:2036-2041` | yes: `printf 'both\na head\n' > app.txt`, `-tr -s20 -c50 -R app.txt`: C 26 rules incl. `c <- a d`, `b <- a c d`; Go 21, none with `a` in a body | `internal/istree/report.go` | `TestReport_HeadOnlyB3`, golden `dev-R3` | |
| R4 | conviction 0 at confidence 1 | A (+B convention) | RC12 | `ruleval.c:99` `if (... body <= supp) return 0` | yes: `b <- a c d` C `0`, Go `inf` | `internal/ruleval/measures.go` | ruleval tests; difftest valueDev | conviction = (1−P(h))/(1−conf) → +∞ (Brin et al. 1997) |
| R5 | certainty factor NaN when head in every transaction | A (+B convention) | RC13 | `ruleval.c:183-184` (0/0) | yes: t1+` z`, `-tr -s20 -c50 -ez -v' %e'`: C `z <- d nan` etc. **reported despite `-d10`**; Go value 0, rules filtered | `measures.go` (`reCert`) | ruleval tests | 0 is a convention (P(h)=1 ⇒ conf=1, no change) |
| R6 | Fisher table-probability/info wrong for tied tables | A | RC14 | `ruleval.c:291, 300, 383-385` | yes: base 7, body 2, head 5, supp 1 (`A B`,`A`,`B`×4,`C`): `-ef` C 0.5238, Go 0.99999999999999944; exact two-sided (probability criterion, Python `math.comb`) = 1.0 | `internal/ruleval/fisher.go` | big.Rat oracle tests | Go is 5e-16 below exact; fine |
| R7 | information gain slightly negative; G-test assert | A | RC15 | `ruleval.c:251-259`, `gamma.c:252` | yes: `neg.tab`, `-ei`: C `-3.2034265038149176e-16`, Go `0`; DBG `-eg` aborts "Assertion failed … GammaQ" | `measures.go` | ruleval tests | |
| R8 | `-S` applied to body∪head support | A | RC16 | `report.c:1619` (`supp = rep->supps[rep->cnt]`) | yes: `-tr -s20 -S50 -c50`: C keeps `d <- c (70, 57.1)` (body support 70 > 50); Go 15 rules, none with body support > 50 | `internal/istree/report.go` | `TestReport_MaxSuppB8`, golden `dev-S-rules` | |
| R9 | (a) Fisher negative p; (b) Yates unclamped; (c) `logGamma(170.5)`=0 | A (a,b); A-latent (c) | RC17, RC18, RC19 | `ruleval.c:336-341, 417-422`; `ruleval.c:217-218`; `gamma.c:72-75` | (b) yes: `yat.tab -ey`: C 0.0417, Go 0 (χ² = 0 for an independent table; standard `max(0,|ad−bc|−n/2)`); (a) harness-only (large tables); (c) unreachable from CLI | `fisher.go`, `measures.go`, `gamma.go` | ruleval tests | three unrelated defects in one row — split |
| R11 | (a) `ist_init` reads past level array for `-m` > items+1; (b) recursive report results ignored | E | — | `istree.c` `ist_init` (ref F8: ASan heap-buffer-overflow `istree.c:1700`) | (a) not reproduced: ASAN `-tr -m100 -s-1 t1.tab` shows only the known misaligned-access reports (F11), no OOB; output unaffected; (b) error path only | `internal/istree/report.go` (`Init`) | `TestInit_LargeZmin` (not named in the doc) | no user-visible effect |
| F-1 | libm vs Go `math` for log-based values | C | — | platform `log()` | difftest: log measures ≤ 9.35e-13 relative; 193 records tolerated at the threshold | — | difftest tolerance; review reproducer test | "bit-identical" holds against the `-ffp-contract=off` build, not the stock upstream makefile build (arm64 clang contracts by default) |
| O1 | `-h` ignored | A | RC20 | `report.c:884` (copied to `rep->str`, `rep->hdr` never set) | yes: `-s30 -h'HDR:'` C no header, Go `HDR:d (70)` | `internal/isrout/writer.go` | golden `t1-hdr*` | |
| O2 | `-g` ignored | A | RC21 | `report.c:661, 711-727, 876` | yes: names `a"b`, `c\d`: C raw, Go `"a\"b" "c\\d"` | `writer.go` | golden `names-g` | |
| O3 | fast output path: spurious empty set; wrong pattern spectrum | A (2 defects) | RC22 (report B3, cli B5/B6, core B6, ref F2) | `report.c:1336-1366` (no `cnt>=zmin`), `report.c:1346, 1550-1551` (`supps[cnt]` unfilled) | yes: `a b`/`a c`, `-s-1`: C extra ` (2)` line; `-s-1 -P p t1.tab out`: C 9 signatures (wrong), C without outfile 13 = Go 13. Also `-w -s-1 -P` on `a b 2000000`/`a 1`: C `1 2000000 2` only; Go and C(no outfile / `-s50`) give the 3 correct rows | `internal/report/report.go` (single path) | sim script `allA_file_m1` | two code defects, one group |
| O4 | count-only ignores `-n` for closed/maximal | A | RC23 | `report.c:1496-1505` vs `1537` | yes: `-tc -s-2 -n1 -Z t1.tab` (count-only): C `all: 5`, Go `all: 4` (= C with output file) | `report.go` | test1 `-n1` scripts | |
| O5 | binomial set count overflow at 63 pex | A | RC24 | `report.c:1515-1517` | yes: 2 identical 63-item transactions, count-only: C 3236635425598607969, Go 9223372036854775807 = 2^63−1 (exact) | `report.go` | 63/64/70-pex scripts | ≥64 pex: saturates (still not the true count; documented) |
| O10 | `%d` only on fast path | A (+B partial) | RC25 | `report.c:147` vs `1943-1974` | yes: `-s-3 -m2 -v' (%d)'`: C literal `(%d)`, Go absolute supports | `writer.go` | golden `t1-dnormal` | for rules `%d` stays literal (as upstream) |
| O9 | pattern-spectrum memory ∝ support range | B | (tract T9) | `patspec.c:250-266` | not re-measured | `internal/report/patspec.go` | memory test | improvement, identical output |
| O6 | `-Z` spliced into stdout | A | RC26 | `fpgrowth.c:2528-2531`, `report.c:177-200` | yes: 3000×8-item random data, `-s-5 -Z rnd.tab ""`: C `all:` at line 22926 mid-line; Go at line 24017 after the sets | `writer.go` | `TestWriter_WriteStats` | |
| O7 | `-P` write errors ignored | A | RC27 | `fpgrowth.c:2542-2545`, `patspec.c:832` | C side yes (`-P '' … >&-` exit 0). Go side **not reproducible this way**: Go also exits 0 because the Go runtime reopens a closed fd 1 on `/dev/null` (DIF-5) | `internal/isrout/stats.go` | `TestWriteSpectrumErrors` (in-process failing writer) | |
| O8 | number formatting half-to-odd etc. | n/a (preserved) | — | `report.c:296-302, 349-350` | — | `internal/isrout` | 18,829-case numout corpus | not a deviation; belongs in a "preserved quirks" list |
| I1 | broken escape decoding | A | RC28 | `escape.c:134-135, 168-171` | yes: `-f'\'` on `1z2 3q4`: C reads past NUL (`z q . t a b` become separators, 8 sets); Go one item, 1 set | `internal/tabread/escape.go` | golden deviation list | |
| I2 | non-ASCII separator bytes, OOB write | A (UB) | RC29 | `escape.c:131-133`, `tabread.c:153-154` | yes: literal `-f$'\xc3\xa9'`: C ignores it (`aéb (1)`); Go treats both bytes as separators — identical to C's escaped `-f'\xc3\xa9'` ("item expected" on both) | `internal/tabread/config.go` | golden deviation list | separators are byte sets; a UTF-8 multibyte separator is two separators (worth one sentence in cli.md) |
| I3 | last record ending in a separator without newline lost | A | RC30 (tract T3) | `tract.c:704`, `tabread.c:224-227` | yes: `a b\nc d,`: C 4 sets (1 transaction), Go 6 | `internal/tabread/tokenizer.go` | golden deviation list | |
| I4 | fields truncated at 1024 bytes | A | RC31 | `tabread.c:230-232` | yes: two long distinct names: C one item (1024 × `x`, supp 2); Go 2 items | `tokenizer.go` | golden deviation list | |
| I5 | (a) "read error" names the field; (b) record number off by one | A (2 cosmetic defects) | RC32 | `tract.c:217, 838`; `tabread.c:217-218` | yes: directory as input: C `read error on file ` (empty), Go `… file dir`; `a 1\nb 2\n   ` with `-w`: C `:2(1)`, Go `:3(1)` | `internal/tabread/errors.go`, `tokenizer.go` | golden deviation list | |
| I6 | weight-sum overflow segfaults | A | RC7 (dup M7) | — | see M7 | — | see M7 | duplicate row |
| I7 | weights truncated long→int | A | RC7 (tract T6, cli B13) | `tract.h:270` | not run (unit tests) | `internal/tabread/reader.go` | `internal/tabread/reader_test.go` (4294967297) | |
| I8 | `nan(...)` payload syntax | C | — | Apple libc `strtod` | not run | `internal/cconv` | cconv tests | |
| C1 | NaN thresholds accepted | A (UB) | RC8 | `fpgrowth.c:1915-1920` (`ceilsupp`), no `isnan` checks | yes: `-snan -Snan -cnan -dnan -Fnan`: C exit 0 (mines); Go exits 11/11/12/13/11 with the documented messages | `cmd/fpgrowth/options.go` | golden `cmd/fpgrowth/testdata/deviations.txt` | same group as M8 |
| C2 | `-F` empty value uses uninitialized slot | E | RC33 | `fpgrowth.c:2166-2173` | no visible effect: `-F`, `-F60:` md5 equal C/Go; `-F:5` both "unknown option -:" | `options.go` | not named | UB by code reading only |
| C3 | `-Z` write errors to stdout ignored | A | RC27 (dup O7 group) | `fpgrowth.c:2528-2531` | see DIF-5 | `cmd/fpgrowth/run.go` | `cmd/fpgrowth/run_test.go:281` (closedWriter) | not named in the doc |
| C4 | weight-sum overflow message | A | RC7 (dup M7) | — | see M7 | `run.go` | `dev-bigw` | duplicate row |
| C5 | trees beyond address space | C | — | — | not run | `internal/fpg` (`ErrTooLarge`) | `internal/fpg/tree_test.go:565ff` | |
| C6 | stop mining at first failed write | B | — | — | not run (in-process tests) | `run.go` | `TestWriteError` | behavior change of logged count |
| C7 | wall-clock times; time attribution | C | — | `clock()` | n/a | `run.go` | — | |
| C8 | SIGINT handling | D | — | `FPG_ABORT` off (and `sigint.h` absent from archive) | not run | `run.go` | `cmd/fpgrowth/main_test.go:39-110` | |
| C9 | stdin spooled to a temp file | C | — | — | yes: `cat t1.tab \| G -s30 - -` md5 = C | `run.go` | `run_test.go:315` | |
| L-1 | appearance map registration order | D | — | — | not run | root | root tests | |
| L-2 | empty DB / no frequent items is not an error | B | — | exit 15 upstream | yes (libcheck: 0 sets, `FrequentItems` 0, nil error) | root | root tests | |
| L-3 | `MaxSize < MinSize` rejected | B | — | — | yes (libcheck: `ErrInvalidOptions`) | root | `TestOptionsValidation` | |
| L-4 | CSV sources | D | — | — | Examples pass | `csv.go` | `FuzzCSVFile` | |
| L-5 | `Fraction`/`Count` | D | — | — | yes: `Fraction(p/100)` vs `-s p` for p = 0.1…100.0 (1000 values) on t1 and a 997-transaction file: 0 mismatches in set counts | `options.go` | root tests | |
| L-6 | Evaluation without Filter | D | — | — | not run | `measure.go` | `TestEvaluationWithoutFilter` | |
| L-7 | fractional thresholds mapping | D | — | — | not run | `options.go` | `TestPercentOf`, `TestPlanMapping` | |

## 4. Findings

| ID | Severity | Status | Title | Blocks v0.1.0 |
| --- | --- | --- | --- | --- |
| DIF-1 | Low | confirmed | No bug count in repo; "~40" only justified with latent and undocumented items (33 strict) | no |
| DIF-2 | Medium | confirmed | README / algorithm.md describe the obsolete M5 implementation and claim performance was never measured | yes (docs only) |
| DIF-3 | Low | confirmed | README: "all variants find the same sets" / "Tuning never changes results" is false with evaluation measures (`AggregateFirst`) | no (fix docs) |
| DIF-4 | Low | confirmed | Undocumented deviations / preserved quirks (T4, F5, xfq saturation; T10, T21) | no |
| DIF-5 | Low | confirmed | Closed stdout: Go exits 0 silently, upstream exits 4 | no |
| DIF-6 | Low | confirmed | `docs/differences.md` test references inaccurate or missing | no |
| DIF-7 | Low | confirmed | R2/R10 description: which `-p` documentation is followed; R10 is not an upstream bug | no |
| DIF-8 | Info | confirmed | Evaluation of item sets depends on internal item order (upstream semantics, preserved, under-documented) | no |
| DIF-9 | Low | confirmed | Timing test `TestCancelTreeBuild` flaky under load (54 ms > 50 ms) | no |
| FEAT-1 | Info | confirmed | `fpgpsp` exclusion: reason stated imprecisely; defer | no |
| FEAT-2 | Info | confirmed | zlib `-z` exclusion; cheap pure-Go option exists; defer | no |
| FEAT-3 | Info | confirmed | Upstream library-only facilities not exposed (size-ordered istree output, data-mode flags, `fpg_genpsp`) | no |
| FEAT-4 | Info | confirmed | `ex/` converter scripts and `run.R` not ported (not program features) | no |

### DIF-1 — bug count claim (Low, confirmed)
Evidence: grep in section 1; deduplicated mapping in section 3. Impact: an
external "~40 confirmed upstream bugs" claim would overstate by counting
duplicates (M4/M9, M7/I6/C4/I7, M8/C1, O7/C3), a defect of the port's own
patched C (R10), unreachable code (R9c, R11a) and UB-by-inspection (C2).
Resolution: if a number is published, use "33 confirmed user-visible upstream
defects (plus 2 avoided crashes and 5 latent issues)", and generate it from a
classification column in differences.md (section 5).

### DIF-2 — stale M5/performance statements (Medium, confirmed)
Evidence:
- `README.md:1018-1023`: "searched without the depth limit that upstream
  uses … A cheaper exact check is scheduled for the performance stage."
- `docs/algorithm.md:292-296`: "The port searches closed and maximal sets
  without the depth limit and applies the maximum size only when reporting."
- Code and differences.md M5: limited search (`internal/fpg/mine.go:148-163`,
  `xable2`/`deeper`) plus `extcheck.go`; README Status says Stage 3 delivered
  "exact closed/maximal size limits" and benchmarks.
- `README.md:1069-1071` (Limitations): "Performance has not been measured
  against the C program or optimized yet" vs README Status ("1.12–2.08× … ")
  and `docs/benchmarks.md`.
- `README.md:1025-1027`: "Which settings are fastest depends on the data; no
  measurements are published yet" vs `docs/benchmarks.md` `BenchmarkSuiteVariants`
  and the `-As -l0`/`-Ac -j` tables.
Impact: user-facing contradictions in the release README about a
correctness-relevant deviation. Resolution: rewrite those three README
paragraphs and algorithm.md §"Size limits" to the Stage 3 mechanism.

### DIF-3 — "same sets" claims vs evaluation measures (Low, confirmed)
Evidence (`r200.tab`: 200 random transactions over a–j, seed 7):
`-ts -s2 -el -d105` gives 166 / 166 / 164 / 165 / 165 distinct sets for
`-q2 / -q1 / -q0 / -q-1 / -q-2`, and 167 / 166 / 167 / 167 for
`-As / -Ac / -Ad / -At`, 167 with `-Ac -j`. C gives identical counts (upstream
semantics: `-a x`/`AggregateFirst` evaluates the rule whose head is the first
item in internal order; `measure.go:141` documents this). Same effect between
targets: `{b,c,d}` has lift < 1.05 under `-ts` but 1.073 under `-tc`.
Impact: README "All variants find the same sets with the same supports" and
"None of them changes the results, only speed and result order" are false
whenever an evaluation measure with the default aggregation filters results.
Resolution: qualify both sentences ("without an evaluation threshold, or with
`-am/-an/-aa`"); add a "preserved upstream semantics" note.

### DIF-4 — undocumented deviations and preserved quirks (Low, confirmed)
- T4 bucket-sort recursion: 17 identical 100,000-item transactions plus
  100,010 one-item transactions, `-s-17 -n1`: C exit 139; Go completes
  (100000 sets). Not in differences.md.
- F5 `isr_prefmt` overflow: `printf 'a 1000000000\n'`, `-w`: C exit 139; Go
  `a (100)`. Not in differences.md (cli.md only says `-N` "changes nothing").
- `internal/tract/itembase.go:224-226` saturates size·weight sums (`xfq`, used
  for `-q±2`) — a code-marked deviation absent from differences.md.
- Preserved, not listed under "Kept as upstream": embedded NUL truncates item
  names (T21; repro `a\0x b`/`a\0y b` → one item `a` in both); spectrum counts
  printed with `%g` (T10; `-P` on a 25-item transaction prints `5.2003e+06` in
  both); unstable tie order (T8); AggregateFirst order dependence (DIF-3).
Resolution: add rows (class A for T4/F5, C for xfq) and a "Preserved upstream
behavior" table.

### DIF-5 — closed stdout (Low, confirmed)
Evidence: `C t1.tab - >&-` → exit 4 ("write error on file <stdout>");
`G t1.tab - >&-` → exit 0. Probe `_audit/diff/fdcheck`: with `>&-`,
`os.Stdout.Stat()` returns a character device and `Write` returns nil — the
Go runtime (go1.27.2 darwin/arm64) opens `/dev/null` for a closed fd 0–2 at
startup. Same for `-Z` and `-P ''`. Impact: scripts that rely on exit 4 to
detect a closed stdout lose results silently; differences C3/O7/C6 read as if
such errors were always reported. Resolution: document as a C-class
platform difference (cannot be distinguished from an explicit `>/dev/null`);
tests exercise only in-process failing writers.

### DIF-6 — test references in differences.md (Low, confirmed)
- M2 and M8 cite "golden `deviations.txt`": `internal/fpg/testdata/golden/deviations.txt`
  only contains B1/B3/B4/B5 cases (72 lines). M8 is covered by `TestThresholds`.
- R11's `TestInit_LargeZmin`, C3's `run_test.go:281`, C8's `main_test.go`,
  C9's `run_test.go:315`, C5's `tree_test.go` are not named; C2, C7 have no test.
- "root tests", "regression tests", "golden deviation list" are not
  test names. Resolution: one exact test/golden name per row; a CI check that
  every name in the table exists (grep), as done in this audit.

### DIF-7 — R2 / R10 wording (Low, confirmed)
Upstream's help text and HTML documentation disagree on the sign of `-p`;
Go follows the help text and adds strong forward filtering for `-p>0` on
fpgrowth's path. R10 is a defect only of the patched C used to check R2.
Resolution: state "semantics of the `-p` help text; the HTML page's
'negative = backward' is not followed"; mark R10 "consequence of R2, not an
upstream defect".

### DIF-8 — order-dependent set evaluation (Info)
See DIF-3; documented at the API (`AggregateFirst`), not in README/cli.md.

### DIF-9 — flaky cancellation-latency test (Low, confirmed)
`go test ./...` at load ~24: `TestCancelTreeBuild/AlgorithmSingleTree
returned 54.109ms after the cancellation` (`fpgrowth_test.go:879`, 50 ms
limit). Passes in isolation. Impact: CI noise on shared runners.
Resolution: scale the limit, or measure work units instead of wall time.

### FEAT-1 — `fpgpsp` (Info, confirmed)

> **Note (2026-10-23, issues #23, #25).** The statement below that
> `tars.c` and `pspest.c` are missing from the archive is wrong: `tars.o`
> and `pspest.o` are objects built from `tract/src/tract.c` (`-DTA_READ
> -DTA_SURR`) and `tract/src/patspec.c` (`-DPSP_REPORT -DPSP_ESTIM`),
> which are in the archive. `fpgpsp` builds from it and has since been
> ported (`cmd/fpgpsp`, `docs/upstream/fpgpsp.md`). The record is kept
> as written.
Archive: `fpgrowth/src/fpgpsp.c` (748 lines, v1.3 2024.12.12) and `fpgpsp.h`
are present; the makefile's `PSPOBJS` needs `tract/src/tars.c` (surrogate
data: identity, randomization, pair swaps, shuffle — `TA_SURR`) and
`tract/src/pspest.c` (`psp_tbgest`, `PSP_ESTIM`), both absent; plus pthreads.
Purpose: generate a pattern spectrum (counts of patterns per (size, support))
over surrogate data sets, or estimate it, and derive a support border that is
fed back via `-F` — pattern-spectrum filtering for statistical significance
(used in spike-train analysis). Importance for a general FP-growth library:
low. Cost: the missing modules would have to be designed from scratch
(randomization semantics, RNG reproducibility vs. upstream impossible without
sources); no differential oracle. Recommendation: defer beyond v0.1.0; fix
README wording "its sources are missing from the upstream archive" →
"`fpgpsp.c` is included but depends on `tars.c` and `pspest.c`, which are not".

### FEAT-2 — zlib `-z` (Info, confirmed)
`-z` exists only with `USE_ZLIB` (`fpgrowth.c:2318-2321, 2393-2395`;
`report.c:171-200, 995-1008`): `deflateInit(Z_DEFAULT_COMPRESSION)` = zlib
container (RFC 1950, not gzip) of the item-set/rule output only (not `-P`,
not `-Z`). The default makefile does not define `USE_ZLIB`, so stock upstream
binaries reject `-z` exactly as Go does. Cost in Go: small and pure Go
(`compress/zlib` around the output writer, ~50 lines + tests); differential
testing possible by building C with `ADDFLAGS=-DUSE_ZLIB` and `-lz` (byte
equality of the compressed stream would depend on zlib's implementation, so
compare decompressed output). Importance: low (users can pipe through a
compressor; the library has no text output). Recommendation: defer; keep the
current rejection, which matches the default upstream build.

*Note (2026-10-22, issue #22):* implemented after v0.1.0. The CLI accepts
`-z` like the `USE_ZLIB` build (usage line included) and compresses only the
item set/rule output; `reference/build.sh zlib` builds the C oracle and
`reference/difftest` `TestZlib` compares decompressed outputs. The usage
line cited above is at `fpgrowth.c:2329-2332` in the vendored file. See
`docs/differences.md` O12.

### FEAT-3 — library-only upstream facilities (Info)
Not exposed: size-ordered item set tree output (`ist_init(order≠0)`,
`ist_iset/ist_rule`; beware rules B14), the data-mode flags `FPG_NOSORT /
NOPACK / NORECODE / NOFILTER / NOREDUCE` (several crash upstream, core B8),
`fpg_genpsp`. None is reachable from the upstream CLI. Recommendation: defer.

### FEAT-4 — example scripts (Info)
`ex/tab2set row2set hdr2set flg2set tid2set` (gawk converters) and `run.R`
(calls the CLI) are auxiliary; Go's `CSVFile`/`TableFormat` cover some of the
formats. Recommendation: none for v0.1.0; optionally mention in docs.

## 5. Part B — feature completeness (independent of feature-matrix.md)

Archive contents (71 files): programs `fpgrowth` (built) and `fpgpsp` (not
buildable); modules util (arrays, escape, memsys, random, scanner, symtab,
tabread, tabwrite), math (chi2, gamma, ruleval), tract (clomax, fim16,
patspec, report, tract), apriori (istree); docs (`fpgrowth.html`, licenses);
`ex/` data (test1–6.tab, test1.csv) and scripts.

CLI options from the usage text / `switch` in `fpgrowth.c:2362-2416`:
`t m n s S o c e a d i p q A x l j u F R P Z N g z h k I v w r f b C !` (35;
`z` only with `USE_ZLIB`). Go: every option except `-z` accepted; usage and
`-!` text byte-identical; invalid target/variant messages identical.
Targets s/f, c, m, g, r; variants s, c, d, t (+ auto); all 25 measures
(`RE_FNCNT`=26 incl. none) and `-eb`; aggregation x/m/n/a; info codes as in §0.

| Feature | Status in Go |
| --- | --- |
| All targets, variants, pruning options, thresholds, border, weights, selection/appearance, measures, aggregation, `-i`, `-p`, `-Z`, `-P`, output formatting, count-only mode, table format options | complete (verified by goldens, my spot checks, difftest 3000+30 cases seed 424242) |
| `-N` | accepted, no-op (no pre-format table) — correct; documents nothing about avoided F5 crash |
| `-z` | missing (FEAT-2) |
| `fpgpsp` | missing (FEAT-1) |
| Library-level C API features | partial (FEAT-3) |

README feature claims checked:

| Claim | Check | Result |
| --- | --- | --- |
| Targets, rules with single-item heads, empty body | `-tr` outputs `d <- ` rules | OK |
| "All variants find the same sets" | libcheck: 4 targets × 4 algorithms on t1, no measure: equal | OK without evaluation; false with `AggregateFirst` filtering (DIF-3) |
| Tuning never changes results | see DIF-3 | qualified |
| `Fraction(k/100)` equals `-sk` | 1000 values × 2 datasets | OK (0 mismatches) |
| Weights int64 ≥ 0; overflow checked; negative rejected | libcheck, M6/M7 repros | OK |
| 25 rule measures | `ruleval.h` `RE_FNCNT 26` | OK |
| Streaming from reopenable gzip reader | libcheck `TableReader` over `gzip.NewReader` | OK (15 sets on t1) |
| No goroutines in the library | grep `go func` outside tests: only `cmd/fpgrowth/run.go:388` | OK |
| Library has no text output | API listing | OK |
| "Performance has not been measured" (Limitations) | benchmarks.md | stale (DIF-2) |
| Not ported: `fpgpsp`, `-z` | verified | wording issue (FEAT-1) |

## 6. Proposed restructuring of `docs/differences.md` (not applied)

1. Add a **Kind** column with fixed values: `defect-fix` (A), `defect-fix
   (policy)` (A with a chosen semantics, e.g. M6, R1, R2, R4, R5), `latent`
   (unreachable/UB by inspection: R9c, R11, C2, R10), `platform` (C),
   `improvement` (B), `new` (D), `preserved` (O8 and the quirks of DIF-4).
2. Add **Upstream evidence** (file:line in `reference/upstream`, reproducer
   command, upstream output) and **Expected behaviour basis** (definition /
   upstream docs / brute force) columns, so each A row is self-contained
   instead of pointing to audit-note bug IDs.
3. Add **Root cause** and merge duplicates: M9→M4, I6/C4→M7, C1→M8, C3→O7;
   split R9 into R9a/b/c and R11 into R11a/b, I5 into I5a/b, O3 into O3a/b.
4. One **exact** regression test or golden file per row (DIF-6), checked
   by a script in CI.
5. Separate sections: "Upstream defects fixed", "Intentional behaviour
   changes", "Platform differences", "Library-only behaviour", "Preserved
   upstream behaviour (not deviations)" — the README summary can then be
   generated from the first section with a correct count.
6. Add T4, F5 and the xfq saturation rows; add the closed-stdout platform
   note (DIF-5); say which `-p` documentation is followed (DIF-7).

## 7. Reproduction notes
All commands above were run from `_audit/diff/w` with `C` and `G` as defined;
data files there (`t2`, `t3`, `gen`, `mp`, `one`, `zm`, `mz`, `wn`, `big`,
`t2.tab`, `neg.tab`, `yat.tab`, `f.tab`, `app.txt`, `nul.tab`, `i3.tab`,
`long.tab`, `zq.tab`, `na*.tab`, `i5.tab`, `e.tab`, `pex63.tab`, `rnd.tab`,
`r200.tab`, `r997.tab`, `t4.tab`, `w9.tab`, `psp.tab`, `p25.tab`). Brute-force
checks: `w/chk.py`, `w/chk2.py` (run with `python3 -I`). Library probe:
`go run ./_audit/diff/libcheck <tab> <C binary>`.
