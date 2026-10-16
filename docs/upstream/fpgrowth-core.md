# FP-growth mining core (fpgrowth.c 1–2081): audit and porting spec

Status: audit of upstream FP-Growth 6.21, reviewed against the source and
tested against the reference binary. This is a porting specification, not
a design document. Go names in §7 are suggestions only.

Scope: `reference/upstream/fpgrowth-6.21/fpgrowth/src/fpgrowth.c` lines
1–2081, which cover the data structures, every algorithm variant,
`fpg_create`, `fpg_delete`, `fpg_data`, `fpg_report` and `fpg_mine`.
`main`, `help`, `getbdr` and `setbdr` are out of scope. They are cited only
where they decide which core paths are reachable. Functions in `tract.c`,
`report.c`, `fim16.c`, `clomax.c` and `istree.c` are described only by the
contract the core relies on. Those modules have their own audits.

All paths below are relative to `reference/upstream/fpgrowth-6.21/`, and
`fpgrowth.c:N` means `fpgrowth/src/fpgrowth.c` line N.

## 0. Method and evidence conventions

- **Reference build.** The upstream makefiles were used unchanged: `-O3
  -DNDEBUG`, which disables every `assert`. `fpgmain.o` is built with
  `-DFPG_MAIN` and **without** `FPG_ABORT`, `VISITED`, `BENCH` or
  `USE_ZLIB` (`fpgrowth/src/makefile`). The report module is built as
  `repcm.o` with `-DISR_PATSPEC -DISR_CLOMAX` (`tract/src/makefile:363`).
  Experiments ran on darwin/arm64.
- **Experiments.** All experiments ran outside the repository, in the
  session scratchpad:
  - **CLI.** The reference CLI (`$FPG` below).
  - **Patched build.** A copy with the one-line top-down fix from §6 B1.
    Its only purpose was to separate that bug from everything else.
  - **API driver.** A small C driver linked against the reference objects,
    used to reach `fpg_data` modes the CLI never passes.
  - **Python harnesses.** These compared sets across variants and against a
    brute-force oracle for frequent, closed, maximal and generator sets.
- **Run counts.** About 9,600 cross-variant CLI runs, about 24,000 + 69,000
  + 11,500 oracle comparisons, 5,400 zmax-specific oracle runs and about
  2,900 API runs. Section 6 states the results. Every claim marked
  **confirmed** has a reproducer that was actually run; **hypothesis**
  means it was derived from the code only.
- **Output format in experiments.** Exact-support comparisons used `-v'
  [%a]'`, not `' (%a)'`, because the default absolute format triggers the
  report bug in §6 B6.

---

## 1. Pipeline overview

### 1.1 Life cycle (as driven by `main`)

```
fpg_create(target, smin, smax, conf, zmin, zmax, eval, agg, thresh, algo, mode)   fpgrowth.c:1810
fpg_data(fpg, tabag, 0 /*data mode*/, sort)                                       fpgrowth.c:1898  (main: 2510)
report = isr_create(ibase)
fpg_report(fpg, report)                                                           fpgrowth.c:1970  (main: 2514)
setbdr(...), isr_addpsp(...), isr_setfmt(...), isr_open(...), isr_setup(report)   main: 2516-2525
fpg_mine(fpg, prune, 0 /*order*/)                                                 fpgrowth.c:2012  (main: 2526)
fpg_delete(fpg, 0)                                                                fpgrowth.c:1886
```

The caller must call `isr_setup` after `fpg_report` and before `fpg_mine`.
`isr_setup` is what computes the reporter's exploration limit `xmax`
(`tract/src/report.c:1117`). Without it, `xmax` stays at `ITEM_MAX` and
`zmax` does not limit the search.

### 1.2 Miner object (`struct _fpgrowth`, fpgrowth.c:220-245)

| Field | Meaning | Set by |
|---|---|---|
| `target` | normalised target: exactly one of `ISR_FREQUENT(0)`, `ISR_CLOSED(1)`, `ISR_MAXIMAL(2)`, `ISR_GENERAS(4)`, `ISR_RULES(8)` | create |
| `smin`, `smax` | user thresholds as given (percent if ≥ 0, absolute if < 0) | create |
| `supp` | absolute minimum support used by the miners (init 1) | data (overwritten, never clamped) |
| `body` | absolute minimum body support for rules (init 1) | data |
| `conf` | confidence as a fraction (`conf/100`) | create |
| `zmin`, `zmax` | size limits | create |
| `eval`, `agg`, `thresh` | evaluation measure (with flags), aggregation mode, threshold as a fraction (`thresh/100`) | create |
| `algo` | variant index into `fpgvars[]` | create (normalised) |
| `mode` | operation-mode bit set (§1.3) | create (normalised) |
| `tabag`, `report` | borrowed transaction bag and reporter | data / report |
| `dir` | item-loop direction (+1 or −1) | set inside each `fpg_*` |
| `set`, `map`, `cis` | scratch buffers (§2.4) | allocated and freed inside each `fpg_*` |
| `fim16` | 16-items machine (complex variant only; the others keep it in the tree) | `fpg_cmplx` |
| `istree` | item set tree (rule or evaluation path) | `fpg_mine` |

### 1.3 Constants (`fpgrowth/src/fpgrowth.h`)

| Group | Name | Value | Notes |
|---|---|---|---|
| target | `FPG_FREQ`/`FPG_FREQUENT` | `ISR_FREQUENT` = 0 | `main` maps `-ts` to `ISR_ALL` (also 0), so `-ts` and `-tf` are the same |
| | `FPG_CLOSED` | 1 | |
| | `FPG_MAXIMAL` | 2 | |
| | `FPG_GENERAS` | 4 | |
| | `FPG_RULES` | 8 | |
| data mode (argument of `fpg_data`) | `FPG_NORECODE` | 0x01 | skip `tbg_recode` |
| | `FPG_NOFILTER` | 0x02 | skip `tbg_filter` by size |
| | `FPG_NOSORT` | 0x04 | skip the item sort, transaction sort **and** reduce |
| | `FPG_NOREDUCE` | 0x08 | skip `tbg_reduce` |
| | `FPG_NOPACK` | 0x10 | do not pack items |
| | `FPG_SURR` | `NORECODE\|NOFILTER\|NOREDUCE` | used only by `fpgpsp.c` |
| eval | `FPG_LDRATIO` | `RE_FNCNT` (= 26, `math/src/ruleval.h:51`) | `-eb`; handled by the reporter, **not** rule-based |
| | `FPG_INVBXS` | `IST_INVBXS` = `INT_MIN` | flag OR-ed into `eval` (`-i`) |
| agg | `FPG_NONE/FIRST/MIN/MAX/AVG` | 0/0/1/2/3 | passed through to the istree |
| algo | `FPG_SIMPLE` | 0 | `-As` |
| | `FPG_COMPLEX` | 1 | `-Ac` (default) |
| | `FPG_SINGLE` | 2 | `-Ad` |
| | `FPG_TOPDOWN` | 3 | `-At` |
| | `FPG_AUTO` | 4 | becomes `FPG_COMPLEX`; API only |
| mode | `FPG_FIM16` | 0x001f | **a count field** (0..31): number of items to pack. `main` stores `min(max(-l,0),16)` |
| | `FPG_PERFECT` | 0x0020 | perfect-extension pruning (`-x` clears it) |
| | `FPG_REORDER` | 0x0040 | reorder items in conditional trees (`-j` clears it) |
| | `FPG_ORIGSUPP` | 0x0080 | rule support = support of body ∪ head (`-o`) |
| | `FPG_TAIL` | 0x0100 | head-union-tail pruning (`-u` clears it) |
| | `FPG_PREFMT` | 0x1000 | pre-format integers in the reporter (`-N` clears it) |
| | `FPG_ZLIB` | 0x4000 | only if `USE_ZLIB` is defined (not in the reference build) |
| | `FPG_NOCLEAN` | 0x8000 (`NDEBUG`) or 0 | do not free the istree in `cleanup()` |
| | `FPG_VERBOSE` | `INT_MIN` | log messages (`XMSG`); the sign bit of `mode` |
| | `FPG_DEFAULT` | `PERFECT\|REORDER\|TAIL\|FIM16` (= 0x017f) | |

The CLI default mode is `FPG_DEFAULT|FPG_PREFMT`. The FIM16 field is then
replaced by `pack` (default 16), and `main` always adds
`FPG_VERBOSE|FPG_NOCLEAN` (`fpgrowth.c:2243, 2453-2454, 2462`).

### 1.4 `fpg_create`: normalisation (fpgrowth.c:1810-1871)

`fpg_create` does no validation; `main` validates (§1.8). The rules below
are applied in this order. Later rules see the results of earlier ones.

1. **Target collapsed to a single type** (1819-1823). Precedence is
   `RULES > GENERAS > MAXIMAL > CLOSED > FREQUENT`.
2. **Neutral confidence for non-rule targets** (1824-1825). `conf` becomes
   100.
3. **`FPG_AUTO`** (1826-1827) becomes `FPG_COMPLEX`.
4. **Generators** (1828-1830): set `FPG_PERFECT` (forced, even after `-x`).
   If `algo == FPG_TOPDOWN`, it becomes `FPG_SINGLE`.
5. **Closed or maximal** (1831-1834): clear `FPG_REORDER`. If
   `algo == FPG_SINGLE`, it becomes `FPG_SIMPLE`.
6. **Non-complex variants** (1835-1836): `algo != FPG_COMPLEX` clears
   `FPG_REORDER`.
7. **Topdown** (1837-1839): `algo ∉ {SIMPLE, COMPLEX, SINGLE}`, which in
   practice means TOPDOWN, clears `FPG_FIM16`.
8. **Rules and rule-based measures** (1840-1842): with
   `e = eval & ~FPG_INVBXS`, if the target is rules or
   `RE_NONE < e < RE_FNCNT`, clear `FPG_FIM16`.
9. **Head-union-tail pruning** (1843-1844): unless the target is
   `MAXIMAL`, clear `FPG_TAIL`.

The miner is initialised with `supp = body = 1`, `dir = 1` and all
pointers `NULL` (1849-1869). The thresholds are not converted yet.

Resulting forced and rejected combinations:

| Request | Effect |
|---|---|
| `-tr` (rules) or a rule-based `-e` measure (anything except `x`, `b`), any target | `fpg_mine` uses **`fpg_tree` + istree**, whatever `-A` says (§3.7). FIM16 is off. The help text claims "enforces algorithm variant 'd'" (fpgrowth.c:2126-2127), but the code calls `fpg_tree`, not `fpg_single`. |
| `-tc` or `-tm` with `-Ad` | runs `-As` (simple) |
| `-tg` with `-At` | runs `-Ad` (single) |
| `-tg` with `-x` | `-x` is ignored (PERFECT forced) |
| `-tc` or `-tm` with `-Ac` | no reordering (as if `-j`) |
| `-As`, `-Ad`, `-At` | no reordering |
| `-At` | no 16-items machine |
| `-u` with a target other than `-tm` | already irrelevant (TAIL cleared) |
| `-l#` with `-Ac` | only zero vs non-zero matters. `fpg_data` sets `pack = 0` for complex (fpgrowth.c:1925-1926), and `fpg_cmplx` creates a machine whenever the FIM16 field is non-zero. **Confirmed:** `-Ac -l1`, `-Ac -l3` and `-Ac` give byte-identical output; `-l0` differs. The help text says `-l` applies "only for variants s and d". |
| `FPG_REORDER` still set, with rules or rule-based evaluation and `-Ac` | Reordering is meaningless there, but the flag still changes item **coding** in `fpg_data` (sort normalisation, §1.5 step 3). **Confirmed:** rule output of `-Ac` equals `-As -q1`, and `-Ac -j` equals `-As`. |

### 1.5 `fpg_data`: thresholds and data preparation (fpgrowth.c:1898-1966)

`mode` here is the **data mode argument**, not `fpg->mode`. `main` always
passes 0. The steps run in this order:

1. **Absolute minimum support** (1914-1920). The exact operation order
   matters for bit-identical results:
   ```c
   w    = tbg_wgt(tabag);                                   /* total weight, before any filtering */
   smin = ceilsupp((fpg->smin < 0) ? -fpg->smin
                 : (fpg->smin/100.0) *(double)w *(1-DBL_EPSILON));
   fpg->body = (SUPP)smin;
   if ((fpg->target & ISR_RULES) && !(fpg->mode & FPG_ORIGSUPP))
     smin *= fpg->conf *(1-DBL_EPSILON);                    /* = smin * (conf*(1-eps)) */
   fpg->supp = (SUPP)ceilsupp(smin);
   ```
   `ceilsupp` is `ceil` for integer `SUPP` and the identity for
   `SUPP == double` (`tract/src/tract.h:272-274, 378-379`). There is no
   clamp to ≥ 1: `-s0`, `-s-0` and rules with `-c0` give `supp == 0` (bug
   §6 B2). Values above `INT_MAX` hit an undefined double→int conversion
   (§6 B9).
2. **Pack count** (1923-1928). `pack = min(fpg->mode & FPG_FIM16, 16)`. It
   is set to 0 when `algo == FPG_COMPLEX` or when the data mode contains
   `FPG_NOPACK`.
3. **Recode** (1931-1941), unless `FPG_NORECODE`. If `FPG_REORDER` is
   still set, `sort` is normalised to its sign (−1, 0 or +1). The call is
   `m = tbg_recode(tabag, fpg->supp, -1, -1, -sort)`: drop items with
   frequency `< supp`, re-number the rest, and drop them from the
   transactions. `m < 0` returns `E_NOMEM`; `m < 1` returns `E_NOITEMS`
   (−15; the CLI prints "no (frequent) items found" and exits with 15).

   The recode direction is the **negated** `sort` (`tract/src/tract.c`
   `ib_recode`, 864-916; comparators at 299-365):

   | CLI `-q` | `sort` | effective `sort` with REORDER | recode `dir` | comparator | item code 0 is |
   |---|---|---|---|---|---|
   | `2` (default) | 2 | 1 | −2 (no reorder) or −1 (reorder) | `descmpx` or `descmp` | highest transaction-size sum (xfq) or highest frequency |
   | `1` | 1 | 1 | −1 | `descmp` | most frequent |
   | `0` | 0 | 0 | 0 | `nocmp` | first-seen order (infrequent items still removed) |
   | `-1` | −1 | −1 | +1 | `asccmp` | least frequent |
   | `-2` | −2 | −1 | +2 or +1 | `asccmpx` or `asccmp` | lowest xfq or lowest frequency |

   With the default `-q2`, `-Ac` (reorder kept) codes items by plain
   frequency, while `-As`, `-Ad`, `-At`, `-Ac -j` and closed or maximal
   targets code by xfq. Tie order inside `idm_sort` is part of the tract
   audit and directly determines output order.
4. **Filter by size** (1946-1949), unless `FPG_NOFILTER`, rules, or a
   rule-based measure. `tbg_filter(tabag, zmin, NULL, 0)` empties (size
   0, weight kept) every transaction with fewer than `zmin` items. It is a
   no-op if `zmin ≤ 1` (`tract/src/tract.c:2003-2049`, early return). This
   changes the supports of sets smaller than `zmin`, which breaks
   generators (bug §6 B3).
5. **Sort and reduce** (1950-1955), unless `FPG_NOSORT`.
   `tbg_itsort(+1)` sorts items in each transaction ascending by code.
   `tbg_sort(+1)` sorts transactions lexicographically ascending (end of
   transaction < any item). Unless `FPG_NOREDUCE`, `tbg_reduce(tabag, 0)`
   merges identical transactions by summing weights and **drops
   transactions whose resulting weight is 0**
   (`tract/src/tract.c:2326-2381`).
6. **Pack** (1956-1957), if `pack > 0`. `tbg_pack(tabag, pack)` folds all
   items with code `< pack` into one entry
   `TA_END | bitmask` (negative). The entry sits where the first such item
   was, which is position 0 when items are sorted
   (`tract/src/tract.c:1066-1086`).

Preconditions this establishes for the miners. They are relied on, not
re-checked.

- **P1.** Items in every transaction are **strictly ascending by code**,
  apart from the packed entry, which comes first. Every `proj_*`,
  `rec_topdn` and `rec_single`/`rec_tree` assumes ancestors have smaller
  codes. `FPG_NOSORT` breaks this (bug §6 B8): wrong results, crashes, and
  infinite loops in `rec_single`.
- **P2.** All items in the bag have frequency `≥ supp`, unless
  `FPG_NORECODE` is set or `tbg_filter` changed frequencies. The miners
  still re-check `f[i] < supp`.
- **P3.** Transaction **order** is lexicographic. This affects only tree
  shape, so efficiency and the item order inside output lines for the
  simple and single variants (§5.3). It does not affect correctness:
  **confirmed** with shuffled transaction order for all four variants
  (2,880 API runs, set-equal).
- **P4.** Transaction weights are positive. After `tbg_reduce` no weight is
  zero, but negative weights from `-w` input are not rejected (§6 B7).
- **P5.** If `pack > 0`, `tbg_ifrqs()` must be computed **after** packing,
  so that `f[0]` is the weight of transactions containing any packed item
  and `f[1..pack-1] == 0`. This holds because recode, filter and reduce
  free the cached counters and `tbg_ifrqs` recounts lazily
  (`tract/src/tract.c:1745-1792`). `tbg_pack` itself does **not**
  invalidate the cache, so it would be stale if computed before packing
  without an intervening free (API-only hazard).

### 1.6 `fpg_report`: reporter configuration (fpgrowth.c:1970-2008)

- **`mrep` reporter mode.** `ISR_SORT` if generators and `FPG_REORDER`
  (the generator repository then needs canonical item order).
  `ISR_NOFILTER` for rules or a rule-based measure, because the istree
  does the closed, maximal and generator filtering. `ISR_ZLIB` is only
  compiled in with `USE_ZLIB`.
- **Support range.**
  `smax = (fpg->smax < 0) ? -fpg->smax : (fpg->smax/100.0)*(double)w*(1+DBL_EPSILON)`
  with `w = tbg_wgt`, then
  `isr_setsupp(report, (RSUPP)fpg->supp, (RSUPP)floorsupp(smax))`. An
  absolute `smax` is also floored (`-S-2.5` gives 2). The reporter
  rejects `s < smin || s > smax` (`tract/src/report.c:1487`).
- **Size range.** `isr_setsize(report, zmin, zmax)`.
- **Evaluation.** If `e == FPG_LDRATIO`:
  `isr_seteval(report, isr_logrto, NULL, +1, fpg->thresh)`.
- **Pre-format.** `isr_prefmt(report, supp, PREFMT ? ib_maxfrq(base) : -1)`.
- **Target.** `isr_settarg(report, target, mrep, -1)`. The direction
  passed to the closed/maximal filter (`clomax`) is **always −1**
  (descending item codes, `tract/src/report.c:777-808`). It is used for
  `clomax` order and for `ISR_SORT`. Every miner that feeds a closed or
  maximal target must therefore add prefix items in descending code order
  (§2.2).

`isr_settarg` also sets `ISR_NOEXPAND` for closed and maximal targets
(`tract/src/report.c:786`). A set and its perfect extensions are then
reported as one set, not as the hypercube.

### 1.7 `fpg_mine`: dispatch (fpgrowth.c:2012-2081)

- `prune` is forced to `ITEM_MIN` unless an evaluation measure is set
  (2024-2026).
- **Item-set path** (2029-2037): not rules and `e ∉ (RE_NONE, RE_FNCNT)`.
  It calls `fpgvars[fpg->algo](fpg)` (`fpg_simple`, `fpg_cmplx`,
  `fpg_single`, `fpg_topdn`, 1801-1806). Any negative result becomes
  `E_NOMEM`. A positive return value (possible, §2.2) is treated as
  success.
- **Rule and evaluation path** (2038-2079):
  1. `ist_create(base, IST_REVERSE, supp, body, conf)`. The istree
     clamps `smin ≥ 1` and `body ≥ smin`
     (`apriori/src/istree.c:635-636`).
  2. Size limit `x = zmax`, plus 1 for closed or maximal targets if
     `zmax < ITEM_MAX`, capped at `tbg_max(tabag)`; then
     `ist_setsize(zmin, x)`.
  3. `fpg_tree(fpg)` fills the istree.
  4. Optional `ist_filter(istree, prune)` if
     `ITEM_MIN < prune ≤ 0`.
  5. `ist_clomax(istree, target | (prune > ITEM_MIN ? IST_SAFE : 0))` for
     closed, maximal and generator targets.
  6. `ist_seteval(...)` unless `LDRATIO`; then `ist_init(istree, order)`
     and `ist_report(istree, report, target)`.
  7. `cleanup()`. With `FPG_NOCLEAN` (always set by the CLI) the istree is
     **not** freed; it is a deliberate leak (1875-1882).

### 1.8 CLI defaults and validation (only what affects the core)

| Option | Default / mapping | Validation in `main` |
|---|---|---|
| `-t` | `s` (maps to `ISR_ALL` = 0); `f`, `c`, `m`, `g`, `r` | unknown letter: error |
| `-m` / `-n` | 1 / `ITEM_MAX` | `< 0` is an error; `zmin > zmax` is **not** checked (then nothing is reported) |
| `-s` | 10 (%) | `> 100` is an error; negative means absolute; 0 is accepted (bug §6 B2) |
| `-S` | 100 (%) | none |
| `-c` | 80 (%) | outside [0,100] is an error |
| `-e` / `-a` / `-d` / `-i` / `-p` | `x` / `x` / 10 / off / `ITEM_MIN` | unknown letter: error |
| `-q` | 2 | none (any int) |
| `-A` | `c` | `s`, `c`, `d`, `t` only |
| `-x`, `-j`, `-u`, `-o`, `-N` | flag toggles | |
| `-l` | 16; stored as `min(max(l,0),16)` | |

`fpg_data` is always called with data mode 0 (fpgrowth.c:2510) and
`fpg_mine` with `order` 0 (2526). The data modes `NORECODE`, `NOFILTER`,
`NOSORT`, `NOREDUCE` and `NOPACK` are therefore **unreachable from the
fpgrowth CLI**. `fpgpsp.c` uses them (`FPG_NOPACK` at
`fpgrowth/src/fpgpsp.c:265`, `FPG_SURR` at 216 and 354,
`FPG_NORECODE|FPG_NOSORT` at 264, `FPG_NORECODE|FPG_NOREDUCE` at 299).

---

## 2. Shared machinery

### 2.1 Types (tract.h, report.h)

- `ITEM = IDENT = int` (`util/src/symtab.h:35`). `TID = int`.
  `SUPP = TID = int`. `RSUPP = SUPP`. These are compile-time choices (int,
  long, ptrdiff_t; `SUPP` may also be double), but the CLI reads integer
  weights with `strtol` truncated to int (`tract/src/tract.c:711`,
  `strtosupp`).
- `TA_END = ITEM_MIN = INT_MIN` is the item-array sentinel and the root
  node's id.
- A **packed entry** is `TA_END | bits` with `bits` in the low 16 bits:
  negative and `> TA_END`. The test `ispacked(i)` is
  `((i) ^ TA_END) > 0` (`tract/src/tract.h:471`).
- `SUPP_MAX = INT_MAX` is the "no perfect extensions" sentinel
  (`pex = SUPP_MAX`).

### 2.2 Reporter contract used by the core (`tract/src/report.c`, `report.h`)

| Call | Contract as used here |
|---|---|
| `isr_add(rep, item, supp)` | Push `item` with the support of the extended set. Returns **1** if the subtree must be processed, **0** if it can be skipped, **−1** on error. It returns 0 when (a) the closed/maximal filter finds a known superset with support ≥ `supp` (`cm_add`, `tract/src/clomax.c:459-475`), or (b) the generator filter finds a one-item-smaller subset with equal support or one missing from the repository (`is_isgen`, `report.c:579-627`). Otherwise it marks the item used and pushes it. It does **not** check size or support (`report.c:1125-1145`). |
| `isr_addpex(rep, item)` | Record a perfect extension at the current prefix level. It is **idempotent**: an item already in use gives −1 and nothing changes. It is **ignored for generators** (returns −1). The return value is never checked by the core (`report.c:1223-1235`). |
| `isr_addpexpk(rep, bits)` | The same for each bit of a packed entry, where bit index = item code (`report.c:1238-1253`). |
| `isr_report(rep)` | Report the current set (and its pex hypercube, or with `NOEXPAND` the single expanded set). Returns < 0 on error. The closed/maximal logic runs **before** the support and size checks (`report.c:1454-1568`), so a set needs **all** its supersets to have been reported first; every miner reports in post-order. |
| `isr_remove(rep, n)` | Pop `n` items and every pex recorded at those levels. |
| `isr_xable(rep, n)` | `rep->cnt + n <= rep->xmax` (`report.h:478`). `xmax = zmax+1` for closed or maximal if `zmax < ITEM_MAX`, else `zmax` (`report.c:1116-1117`). `cnt` counts prefix items only, **not** pex. |
| `isr_tail(rep, items, n)` | `cm_tail(clomax, items, n) > 0 ? 1 : 0` (`report.h:507`). It returns 1 if prefix ∪ tail is contained in a known maximal set, so the subtree can be skipped; `n == 0` also gives 1. A memory error (−1) **becomes 0** (§6 B10). The items must be in descending code order. |
| `isr_supp(rep)` | Support of the current prefix (`report.h:484`). Used by fim16. |

The core's ordering obligations, on which correctness of closed, maximal
and generator targets depends:

- **O1.** Within one recursion path, items are added in **descending code
  order**: every conditional database holds only items with smaller codes
  (smaller head index) than the item it is conditioned on. Reordering
  breaks this (hence rule 5 in §1.4 and `ISR_SORT` for generators).
- **O2.** For closed and maximal targets, sibling items are processed in
  **descending** code order (`dir = −1`; top-down is inherently
  descending), and every set is reported **after** its subtree.
- **O3.** For generators, a set's one-item-smaller subsets must already be
  in the repository. Any order works with subtree-before-parent
  post-order and O1; no special `dir` is needed.

### 2.3 16-items machine contract (`tract/src/fim16.c`, `fim16.h`)

| Call | Contract |
|---|---|
| `m16_create(dir, smin, report)` | `dir > 0`: one machine, `rec_pos` (ascending bit order). `dir ≤ 0`: 16 sub-machines, `rec_neg` (descending bit order). The item map starts as the identity (`m16_clrmap`, `fim16.c:176-212`). |
| `m16_setmap(fim, i, item)` | Bit `i` stands for reporter item `item` (macro). |
| `m16_add(fim, bits, w)` | Add a bit transaction. `bits == 0` only adds to the total. It relies on `w > 0`: the "first insertion" test is `(wgts[t] += w) <= w` (`fim16.c:246-255`). With `w ≤ 0` duplicates appear (hypothesis; affects only zero or negative weights). |
| `m16_mine(fim)` | Mine everything reachable from the current reporter prefix using the bit items: `isr_add`, `isr_addpex`, `isr_report`, `isr_remove`. The pex test uses `isr_supp(report)` as the prefix support. It returns < 0 on error, otherwise the **mask** of bits that are frequent and not perfect extensions (0 if the machine was empty or everything was pex), and **clears** the machine (`fim16.c:613-638`). It never calls `isr_xable` and ignores `zmax` (it explores deeper; the reporter filters sizes). |

Pex discovery inside the machine runs whether or not `FPG_PERFECT` is set,
so `-x` does not stop it.

### 2.4 Scratch buffers (allocated in each `fpg_*`, freed at its end)

| Variant | Allocation | Layout and use |
|---|---|---|
| simple, complex, topdown | `set = malloc(2k·ITEM + k·SUPP)` | `set[0..k)`: new→old item map at build time, then the transaction buffer. `map = set + k`: old→new map (`d`). `cis = (SUPP*)(map + k)`: conditional supports. |
| single, tree | `set = malloc(2k·ITEM)` | no `cis` |

Here `k = tbg_itemcnt` is the number of items after recoding.

Reuse during recursion:

- `proj_simple`, `proj_smp16` and `proj_cmplx` use `fpg->map[0..id)` as
  the old→new map of the projection. They build each projected path
  **backwards from** `fpg->map` (`*--d`), into the tail of `set`.
- `proj_reord` uses `set[0..id)` as the map and `set[id..2id)` as a 0/1
  flag array. `2id` can exceed `k`, so the flags may overlap the start of
  `map`. That is harmless because `map` is not used by the reorder path.
- `rec_simple`, `rec_smp16`, `rec_cmplx` and `rec_topdn` write the HUT
  tail items **backwards from** `fpg->map` into `set`.
- `rec_topdn` uses `cis[0..node->id)` and `map[0..node->id)`.

A Go port can use separately allocated slices per call, as long as it
keeps the same values.

### 2.5 Node memory (`util/src/memsys.c`)

- **Pools.** Each tree has a fixed-size pool:
  `ms_create(sizeof(node), 65535)`. Projections allocate their nodes in the
  **parent's** pool.
- **Mark and release per level.** `rec_*` calls `ms_push` once **before
  its item loop** and `ms_pop` once **after** it (rec_simple 500/531,
  rec_smp16 676/718, rec_cmplx 1140/1179, rec_topdn 1569/1609). Nodes of
  every projection built at one level stay alive until the level ends,
  so memory grows with the sum of the sibling projections, not with one.
  The projection header (`proj`) is allocated once per level for the
  maximum size `cnt−1` and reused for every item.
- **No zeroing.** `ms_alloc` does not zero memory. Every `add_*` and
  `copy()` sets all fields it later reads.
- **Errors.** Any allocation failure returns −1 up the recursion.
  `rec_*` frees `proj` and pops the pool. `fpg_*` frees the machine, the
  pool, the tree and `set`, and returns −1. `fpg_mine` maps this to
  `E_NOMEM`. A partially built 16-items machine is not cleared on error,
  but mining is aborted anyway.

### 2.6 Threshold comparisons (identical in all variants)

- **Frequent.** `supp(X) >= fpg->supp`. All tests are written as
  "infrequent if `< fpg->supp`": `f[i] < supp`, `s[i] < supp`,
  `h->supp < supp`, and inside fim16 `supps[n] < smin`.
- **Perfect extension of prefix P∪{i}.** Conditional support
  `>= pex`, where `pex` is the support of P∪{i}. With non-negative weights
  the conditional support can never exceed `pex`, so `>=` means `==`.
  At the top level `pex = tbg_wgt` (an item contained in **every**
  transaction, counting the transactions emptied by `tbg_filter`).
  `pex = SUPP_MAX` disables the test (`!FPG_PERFECT`).
- **Early exit at top level.** `if (fpg->supp > tbg_wgt) return 0;`
  (fpgrowth.c:742-743, 1203-1204, 1371-1372, 1632-1633). Nothing is
  reported, not even the empty set. `fpg_tree` has no such check.
- **Reporter.** Sets are reported iff `smin ≤ s ≤ smax` and
  `zmin ≤ |set| ≤ zmax`. With perfect extensions the size check is
  `cnt ≤ zmax && cnt + n ≥ zmin` before expansion.

### 2.7 Perfect extensions (pex): who adds them

| Where | Condition | Respects `FPG_PERFECT`? |
|---|---|---|
| Top level, every `fpg_*` except `fpg_tree` | `f[i] >= tbg_wgt` | yes |
| `proj_simple`, `proj_smp16`, `proj_cmplx`, `proj_reord`, the `rec_single` pex loop, `rec_topdn` | conditional support `>=` support of the projection item | yes |
| **Chain shortcut** in `rec_simple`, `rec_smp16`, `rec_cmplx`, `rec_single`: the item's node list has exactly one node | every ancestor (and every packed bit on the path) | **no**, always applied |
| Inside fim16 (`filter`, single-transaction case) | `supps >= isr_supp` | **no**, always applied |

Generators ignore every `isr_addpex`. For them, "pex" pruning simply
deletes the item from the projection. This is exactly right for
generators, which is why `FPG_PERFECT` is forced.

The pex expansion order (the order of items in output lines and the order
of the hypercube lines) is decided by the reporter (`report()` and
`fastout()` take the **earliest-added** pex first). The order in which
the core calls `isr_addpex` therefore affects output.

---

## 3. Algorithm variants

The variants share one skeleton. For each item `i` of the current tree, in
loop order:

1. `isr_add(item_i, supp_i)`; on `r < 0` stop, on `r == 0` skip.
2. If the item is a chain, add its ancestors as pex. Otherwise, if a
   projection is allowed, project and recurse.
3. `isr_report`, then `isr_remove(1)`.

After the top-level recursion, `isr_report` is called once more for the
empty set (prefix ∅ plus top-level pex). `fpg_tree` is the exception.

### 3.1 Simple tree (`-As -l0`; `fpg_simple`, `add_simple`, `proj_simple`, `rec_simple`)

**Data structures** (162-182):

- `FPNODE {id, supp, parent, succ}`, where `succ` is the next node with
  the same item.
- `FPHEAD {item, supp, list}`.
- `FPTREE {cnt, dir, fim16, mem, root, heads[]}`. The root has
  `id = TA_END`.
- `id` is the head index; `heads[id].item` is the reporter item code.
- Head order equals ascending item code. Maps preserve order, so this
  holds in every projection.

**Initial build** (`fpg_simple`, 724-818):

1. Check `supp > W` (the early exit in §2.6). If `k == 0` (API only),
   report the empty set and return.
2. Item map (757-762): `f[i] < supp` gives −1. `f[i] >= pex` gives
   `isr_addpex(i)` and −1. Otherwise the item gets the next new index.
3. If no item is left (`m == 0`), call `isr_report` (empty set plus pex)
   and return.
4. Direction (765): `dir = −1` for closed or maximal, else +1.
5. Build the tree: transactions in **reverse order**
   (`j = n−1 … 0`), each one mapped and passed to
   `add_simple(tree, s, k, ta_wgt(t))`.

`add_simple` (395-422) walks from the root. At each step it shares an
existing node **only if the most recently created node for that item
(the list head) has the current node as parent** (408). Otherwise it
creates the rest of the path. Supports are added along the matched
prefix, including the root.

- With lexicographically sorted transactions processed in reverse, this
  yields a proper prefix tree.
- With another order it yields duplicate nodes. That is still correct
  (lists are sums) but changes chain detection, and with it how pex are
  found. The exact output (line order and item order) then changes while
  the sets stay equal (confirmed, §5.3).
- **List invariant.** A node can be extended only while it is the head of
  its list. This guarantees that, between two nodes of item `i` below the
  same node `X` in list order, no other node of `X`'s item is linked.
  `rec_single` and `rec_tree` depend on this invariant. It holds for any
  transaction order (confirmed by the shuffled-order API runs).

**Projection** (`proj_simple`, 426-468):

1. Conditional supports (438-441): clear `cis[0..id)`; then for every node
   of item `id` and every ancestor `anc`, add the node's support to
   `cis[anc->id]` (P1 guarantees `anc->id < id`).
2. Head selection (444-456), ascending `i < id`: `< supp` drops the item;
   `>= pex` (= `src->heads[id].supp`) gives `isr_addpex(heads[i].item)`
   and drops it; otherwise the item gets the next new index (order
   preserved).
3. Return 0 if nothing is left.
4. Rebuild (460-466): for each node of `id`, collect the mapped ancestors
   backwards into `map−len..map`, which gives ascending order, then
   `add_simple(dst, ..., node->supp)`.

The list of `id` is traversed head-first, i.e. in reverse creation order.
`dst->root.supp` ends up as the support of P∪{id}.

**Recursion** (`rec_simple`, 472-533):

1. Optional abort check (482-484, `FPG_ABORT` only).
2. HUT (485-490): collect all head items backwards from `map`; that is
   descending code. If `isr_tail` returns non-zero, return that value (1).
3. Projection header (491-501), only if `cnt > 1 && isr_xable(2)`; then
   `ms_push`.
4. Loop (502-504) from `i = 0` up (`dir > 0`) or from `cnt−1` down
   (`dir < 0`):
   - `isr_add(h->item, h->supp)` (513-515).
   - Chain (517-520): `!h->list->succ` → `isr_addpex` for every ancestor,
     bottom-up, i.e. descending code. This is done even when the
     projection is not allowed.
   - Otherwise, if `proj`: `proj_simple`; if it returns > 0,
     `rec_simple(proj)` (521-525).
   - `isr_report`, `isr_remove(1)` (526-528).
5. Free `proj` and `ms_pop` (530-531).
6. **Return value.** It is the last `r`, which may be 1 from `isr_tail`,
   or 0 from `isr_report`.

Finally `fpg_simple` calls `isr_report` once more for the empty set (806).

**Closed and maximal.** `dir = −1` satisfies O1 and O2. HUT is active only
for maximal (TAIL is cleared otherwise).

### 3.2 Simple tree + 16-items machine (`-As`, `-Ad` builds; `add_smp16`, `proj_smp16`, `rec_smp16`)

Active when `fpg->mode & FPG_FIM16` (default `-l16`). Items with code
`< pack` arrive as one packed entry, the first element of each
transaction.

**Initial build** (fpg_simple 755-795):

- **Packed head.** `d[0] = s[0] = 0` and `m = 1`: head 0 is **forced**
  as the packed head and gets `item = TA_END` (781). The map loop then
  starts at `i = 1`. Items `1..pack−1` have `f == 0` after packing (P5),
  so they get no head.
- **Machine.** `m16_create(dir, supp, report)` with the identity map
  (packed bit = item code).
- **Transactions.** Packed entries are copied as they are; other items
  are mapped.

`add_smp16` (537-585):

- If the first id is negative (packed), and `dir > 0`, it calls
  `m16_add(fim16, bits, supp)` **immediately**. It ORs the bits into
  `heads[0].item`.
- It shares the packed node only if `heads[0].list` (the most recent
  packed node) has exactly the same bits; otherwise it creates a new
  packed node under the root.
- It then continues like `add_simple`, but adds support **after**
  matching (the root is updated first).
- Duplicate packed nodes can arise even with sorted data. An example is
  transactions `[0]`, `[0,5]`, `[0,20]`: the `{0}` node is duplicated but
  only for packed-only transactions, so harmless.

**Projection** (`proj_smp16`, 589-636):

- Head 0 is always the packed head (`item = TA_END`, `supp = 0`); regular
  heads start at index 1.
- Packed ancestors are masked with `mask` and kept only if non-zero:
  `if ((i &= mask)) *--d = i | TA_END`.
- `n` starts at 1, so the `n <= 0` test is dead code and the function
  always returns 1.

**Recursion** (`rec_smp16`, 640-720):

- **HUT** (654-665): the bits of `heads[0].item` (as item codes 0..15),
  then the other head items, collected backwards (overall descending).
- **Packed head** (690-697):
  - If `dir < 0`, first `m16_add` every node of list 0, using
    `node->id & ~TA_END` and `node->supp`. If `dir > 0`, the data was
    already added during construction of this tree.
  - Then `r = m16_mine(fim16)` and `mask = r` (continue; no
    `isr_add`/`isr_report` for this head).
  - With `dir > 0` the packed head is processed **first**: the mask
    then prunes packed bits from every later projection at this level,
    and pex found by the machine stay attached to this prefix level.
  - With `dir < 0` it is processed **last**: the mask stays `ITEM_MAX`,
    so nothing is pruned (correct, only slower).
- **Chain** (702-707): `isr_addpex` for regular ancestors and
  `isr_addpexpk` for the packed ancestor. The bits are not masked, but
  in projections they are already masked.
- **Machine sharing.** A single machine is shared by all levels
  (`proj->fim16 = tree->fim16`).
  - `dir > 0`: `proj_smp16` → `add_smp16` fills the machine for the child
    level, and the child mines it as its first loop step. HUT never
    returns early here, because TAIL implies maximal implies `dir < 0`.
  - `dir < 0`: the machine is filled only right before mining.
- **Return value.** It can be a positive mask value if the packed head
  was the last processed (`dir < 0`). Callers treat it as success.

### 3.3 Complex tree (`-Ac -j`, and closed or maximal with `-Ac`; `add_cmplx`, `proj_cmplx`, `rec_cmplx`)

**Data structures** (184-204):

- `CSNODE {id, supp, children, sibling, parent, succ}`. Children are
  sorted **ascending by id**.
- `CSHEAD {item, supp, list}`.
- `CSTREE {cnt, mem, root, heads[]}`.

**Build** (`fpg_cmplx`, 1185-1264):

- The same map, pex, `dir` and empty-set rules as simple.
- No packing (`pack = 0` in `fpg_data`).
- `fpg->fim16 = m16_create(dir, ...)` if the FIM16 field is non-zero.
- `add_cmplx` (824-862) searches the sorted child list for real (a proper
  trie for any transaction order). Ids must be ascending (P1).

**Projection** (`proj_cmplx`, 866-954):

- Conditional supports, pex and the map work as in `proj_simple`.
- **16-items path.** If `n ≤ 16 && fpg->fim16` (899-911):
  1. `m16_setmap(i, heads[i].item)`.
  2. For every node of `id`: OR `1 << map[anc->id]` over its ancestors,
     then `m16_add(bits, node->supp)`.
  3. `m16_mine`.
  4. Return 0 ("no projection"). The caller then reports P∪{id}.
  So whenever a conditional database has at most 16 items it is mined
  entirely by the machine. This never happens at top level.
- **Tree path.** Otherwise (945-951), rebuild with `add_cmplx` from
  ancestors collected backwards (ascending).
- The `#if 0` blocks (914-944) are dead code; do not port them.

**Recursion** (`rec_cmplx`, 1113-1181):

- Same structure as `rec_simple`. It loops over `fpg->dir`, not
  `tree->dir`, and calls `isr_tail(..., tree->cnt)`.
- It dispatches to `proj_reord` or `proj_cmplx` by `FPG_REORDER`
  (1168-1170).
- The `#if 0` at 1142-1145 is dead code.

### 3.4 Complex tree with reordering (`-Ac` default for frequent and generator targets; `add_reord`, `proj_reord`)

`proj_reord` (1007-1109):

1. Same conditional supports as `proj_cmplx`.
2. Kept items are written to `d = set + id`.
3. **`i2s_sort(d, n, −1, s)`** (1036) sorts them **descending by
   conditional support**. `i2s_sort` is `i2i_qsort` (`tract/src/tract.h`
   around line 280), an unstable quicksort plus insertion sort that sorts
   ascending and then **reverses** for `dir < 0`
   (`util/src/arrays.c:1141-1216`). Equal supports end up in an
   algorithm-specific order that a Go port must reproduce exactly
   (§8 R1).
4. New heads follow this order: head 0 has the highest conditional
   support; `map[k] = new index`.
5. **16-items path.** If `n ≤ 16 && fim16`, the machine is used with bits
   = new indices (1043-1055).
6. **Tree path.** Otherwise (1089-1097, the `#elif 1` branch):
   - clear the flags `d[0..id)`;
   - per node, set `d[map[anc]] = 1` for every kept ancestor and count
     `k`;
   - `add_reord(dst, d, k, supp)` scans the flags ascending, so the items
     go in new-index order, and clears each flag as it consumes it.
   - The `#if 0` and `#else` variants are dead code.

Consequences:

- Paths are ordered by descending conditional frequency and processed with
  `dir = +1`, so the most frequent conditional item comes first.
- Item order within a prefix is no longer descending by code, which breaks
  O1. That is why closed and maximal targets disable reordering and
  generators use `ISR_SORT`, with `rep->dir = −1` sorting keys descending
  in `is_isgen`.

### 3.5 Single tree, top-down (`-Ad` for frequent and generator targets; `fpg_single`, `rec_single`)

Reference: Wang et al., PAKDD 2002 (fpgrowth.c:78-83).

**Build** (1353-1436):

- Like `fpg_simple` with the FIM16 head forcing (1382-1383), but
  **`dir = +1` always** (1392).
- The packed head is initialised with `item = s[0] = 0`, not `TA_END`,
  and `add_smp16` then ORs packed entries into it. It is never read:
  `rec_single` starts at 1 when `tree->fim16`.
- The tree is always built with `add_smp16`, so packed entries go to the
  machine at insertion.
- With fim16, `m16_mine` runs **once at top level before** the recursion
  (1419-1420). It mines the packed items for prefix ∅.

**Recursion** `rec_single(tree, n)` (1270-1349) works **in place on one
tree**; no projections are allocated. For `i` from `(fim16 ? 1 : 0)` to
`n−1`:

1. Skip the item if `h->supp < supp` (1292). Heads below `n` hold the
   conditional supports of the current level.
2. `isr_add`.
3. **Chain** (1297-1302): pex for regular ancestors, `isr_addpexpk` for
   the packed ancestor.
4. **Otherwise, if `i > 0 && isr_xable(1)`** (1303-1304):
   - Clear `heads[0..i)` (supp and list).
   - For every node of `i`: walk the ancestors.
     - A packed ancestor gets `m16_add(bits, node->supp)`.
     - A regular ancestor with `heads[anc].list == anc` is already
       linked: **break**.
     - Otherwise set `anc->supp = node->supp` (overwrite), add it to
       `heads[anc].supp`, and push `anc` onto `heads[anc].list`.
     - After a break, add `node->supp` to `anc->supp` and the head
       support of every remaining ancestor (1307-1327).
   - Pex loop (1328-1336): for `k` from `(fim16 ? 1 : 0)` to `i−1`,
     skip `< supp`, count `< pex` as frequent (`m++`). Otherwise set
     `h->supp = 0` and call `isr_addpex`.
   - If there is a machine, `m16_mine`; an error breaks the loop
     (1337-1340).
   - If `m > 0`, `rec_single(tree, i)`.
5. `isr_report`, then `isr_remove`.

Notes:

- The early `break` at 1313 relies on the list invariant from §3.1, and
  on **P1**. With unsorted items, ancestors with an id ≥ `i` have lists
  that are never cleared, and the relinking creates cycles. **Confirmed:**
  `FPG_NOSORT` without fim16 hangs in 19/20 test datasets (§6 B8).
- The upward order (heads `< i` rebuilt, heads `≥ i` untouched) is why
  `dir` must be +1. Node supports of item `i` are never overwritten while
  items `< i` are processed.
- Pex items stay in the tree with `h->supp = 0`. At deeper levels their
  heads are rebuilt and they are found as pex **again**; `isr_addpex`
  ignores them because they are already in use. This only works because
  `supp ≥ 1`: with `supp == 0`, zero-support heads with `NULL` lists are
  processed and crash (§6 B2).
- Single is not used for closed or maximal targets (forced to simple).

### 3.6 Top-down trees (`-At`; `fpg_topdn`, `add_topdn`, `getsupp`, `merge`, `copy`, `rec_topdn`)

**Data structures** (206-218):

- `TDNODE {id, supp, children, sibling}`. There are no parent or list
  links.
- `TDTREE {cnt, mem, root, items[]}`. `items[]` maps a node id to the
  reporter item.
- The tree is stored **reversed**: each transaction is inserted from its
  last (highest code) item, so the roots are the highest codes and
  children have smaller ids. Sibling lists are sorted **descending** by
  id (`add_topdn`, 1442-1475).

**Build** (1615-1681):

- Same map, pex and empty-set handling as simple. No fim16 (cleared in
  `fpg_create`). `dir = +1` is set but unused.
- **Bug B1:** `memcpy(tree->items, d, k*sizeof(ITEM))` at 1660 copies the
  old→new map `d` instead of the new→old map `s` (§6 B1).

**Recursion** (`rec_topdn`, 1543-1611). It consumes the tree: always take
`tree->root`, the highest remaining id.

1. `isr_add(items[node->id], node->supp)`. If `r == 0`, set
   `root = merge(sibling, children)` and continue (1574-1575).
2. Only if `proj && node->children`:
   - `getsupp(children, cis)`: a recursive sum per id over the subtree.
   - Map loop over `i < node->id`: drop `< supp`; `>= pex` (= node
     support) gives pex; otherwise keep in order.
   - `proj->root = copy(children, map, mem)`. `copy` (1514-1539) maps
     ids, drops eliminated nodes, and merges their copied children into
     the result, sorted descending. `COPYERR` signals a memory error.
   - Then `rec_topdn(proj)`.
3. `isr_report`, `isr_remove(1)`.
4. `root = merge(sibling, children)` (1606). `merge` (1489-1510) is
   destructive, sums supports and recurses on equal ids, so all
   occurrences of lower items become siblings at the root.

Notes:

- There is **no chain shortcut**, and no pex detection when `proj` is
  `NULL` (at the `xmax` boundary). The second point causes the closed
  and maximal size-limit error to show up most often here (§6 B5).
- Processing is **descending** by code for **every** target. For
  frequent sets the output order is therefore the reverse of `-As` or
  `-Ad` (no identical order in 40/40 test datasets).
- `copy`, `merge` and `getsupp` recurse to the tree depth (the maximal
  transaction length). Go goroutine stacks grow, so this is fine.

### 3.7 Tree for rules and evaluation (`fpg_tree`, `rec_tree`): reached through `-tr` or a rule-based `-e`

**Build** (1744-1795):

- No early exit and no pex: items `< supp` are dropped. Recoding with the
  same `supp` keeps everything else.
- `k == 0` returns 0 without reporting.
- Tree build with `add_simple`. It also copies packed entries, but
  packing is always off here (FIM16 cleared); a packed entry would index
  `heads[negative]`.
- `rec_tree(tree, cnt)`.

**Recursion** (1687-1740). It walks the istree in parallel:

1. Stop if `!ist_xable(istree, 1)`, i.e. `depth + 1 > ist zmax`.
2. Otherwise `ist_addchn` (children of the current istree node).
3. For `i = 1 .. n−1`; item 0 is never a "current" item, because its
   single-item support comes from the istree root (base frequencies):
   - skip `< supp`;
   - `ist_down(item)`; if it is `< 0`, skip;
   - relink heads `< i` exactly as `rec_single`, including the `h->list
     == anc` break;
   - for every `k < i` with support `≥ supp`, call
     `ist_setsupp(item_k, supp)` and count;
   - if the count is positive, recurse;
   - `ist_up`.

Contract: the istree ends up holding the exact support of every frequent
set up to its size limit, in reverse item order (`IST_REVERSE`). Rule
generation, the closed, maximal and generator filters (`ist_clomax`),
evaluation and reporting all belong to the istree audit.

---

## 4. Support type, overflow and threshold conversion

- **Type.** `SUPP` is `int`, and so is `RSUPP`. The reporter counts with
  `ISCNT` (see the report audit). Weights are integers read with
  `strtol`, then cast to int. The reader accepts **zero and negative**
  weights.
- **Overflow.** Every support in the core is a partial sum of transaction
  weights, bounded by `W = tbg_wgt`. If `W` fits in `int` nothing in
  `fpgrowth.c` overflows. `W` itself is summed in tract and can overflow
  there (undefined behaviour in C).
- **double→int conversions.** These are UB when out of range:
  - `fpg->supp = (SUPP)ceilsupp(smin)` (1920);
  - `fpg->body = (SUPP)smin` (1917);
  - `(RSUPP)floorsupp(smax)` (1998).

  On arm64 the conversion saturates. `-s-3000000000` became `INT_MAX`
  and the run ended with "no (frequent) items found" (**confirmed**). On
  x86-64 the same input would produce `INT_MIN` (**hypothesis**:
  `cvttsd2si` returns 0x80000000), making everything "frequent".
- **Conversion formulas** (exact operation order, `DBL_EPSILON = 2^-52`):

  | Quantity | Formula |
  |---|---|
  | `supp` | `ceil(smin<0 ? −smin : (smin/100.0)*W*(1−ε))`; for rules without `-o`: `ceil(that * (conf*(1−ε)))` with `conf = c/100` |
  | `body` (rules) | `(int)ceil(smin<0 ? −smin : (smin/100.0)*W*(1−ε))` |
  | reporter `smax` | `floor(smax<0 ? −smax : (smax/100.0)*W*(1+ε))` |
  | `-F` border (main, out of scope) | `ceil(s≥0 ? s/100.0*W*(1−ε) : −s)` |

  The `(1−ε)` and `(1+ε)` factors absorb decimal-to-binary error in
  percentages. Go must use the same expression shape:
  `math.Ceil(smin / 100.0 * float64(w) * (1 - eps))`, evaluated left to
  right, and `smin * (conf * (1 - eps))` for rules.
- **`supp == 0` is not guarded** in the core (see §6 B2). `ist_create`
  does clamp to 1 for its own use.

---

## 5. Reachability, result sets and output order

### 5.1 CLI reachability

| CLI | Function path |
|---|---|
| `-ts`/`-tf`, `-tc`, `-tm`, `-tg` with `-e x` (default) or `-e b` | `fpgvars[algo]` |
| `-As` (default `-l16`) | `fpg_simple`: `add_smp16`, `proj_smp16`, `rec_smp16` |
| `-As -l0` | `fpg_simple`: `add_simple`, `proj_simple`, `rec_simple` |
| `-Ac` (default), frequent or generator targets | `fpg_cmplx`: `add_cmplx`, `proj_reord`, `add_reord`, `rec_cmplx`, plus fim16 in projections of ≤ 16 items |
| `-Ac -j`, or any `-Ac` with `-tc`/`-tm` | `fpg_cmplx` with `proj_cmplx` (plus fim16 unless `-l0`) |
| `-Ad`, frequent or generator targets | `fpg_single`: `add_smp16`, `rec_single`, plus fim16 unless `-l0` |
| `-Ad -tc`/`-tm` | `fpg_simple` (§1.4 rule 5) |
| `-At`, frequent, closed or maximal | `fpg_topdn` |
| `-At -tg` | `fpg_single` |
| `-tr`, or any target with `-e` ∉ {`x`, `b`} | `fpg_tree` + istree; `-A` is ignored except through REORDER coding |

Reachable only from the API or the `fpgpsp` program: `FPG_AUTO`, all
`fpg_data` modes other than 0, `FPG_FIM16` values above 16 (clamped in
`fpg_data`), and `order ≠ 0`.

Compiled out of the reference CLI:

- `FPG_ABORT`: `sig_aborted()` at the start of every `rec_*`.
- `VISITED` counters.
- The `NDEBUG` debug printers `fpt_show`, `cst_show` and `tdt_show`.
- `FPG_ZLIB`.

### 5.2 Are the reported sets identical across variants?

Yes, for valid inputs, after the top-down fix B1 and apart from the known
bugs. Evidence, using the patched binary and the `[%a]` format:

- **Cross-variant.** 9,600 runs across 40 random datasets, targets
  s/c/m/g, variants s/c/d/t and options `-x -j -l0 -l3 -u -q0 -q±1 -q-2
  -m0 -m2 -n3 -S60 -m3 -n4`: **0 set mismatches**.
- **Clustered data.** 912 runs on denser clustered data (up to about
  8,800 sets per run): 0 mismatches.
- **Oracle.** 69,120 and 11,520 brute-force oracle comparisons. They match
  except for bugs B3 (generators with `-m ≥ 2`), B4 (maximal with only
  top-level pex) and B5 (closed or maximal with `-n`). Each of these
  affects all variants equally except B5, which depends on the variant.
- **Rules and rule-based evaluation.** Identical sets for all `-A`
  letters after normalising the item order within lines.

Known exceptions in the **unpatched** reference:

- `-At` crashes or prints wrong item names when any item is removed at
  the top level before the last code (B1).
- `-s0` crashes or reports support-0 sets (B2).
- Sets can depend on the variant for closed or maximal targets with `-n`
  (B5), and with negative weights (B7).
- `-As`/`-Ad -x` versus `-Ac`/`-At -x` with the fast output format: the
  spurious empty set (B6) appears only where a machine finds top-level
  pex.

### 5.3 Output order

The order differs between variants. It is decided by the following
factors.

1. **Item codes.** These come from `tbg_recode` (`-q`, sort normalisation
   under REORDER, `idm_sort` tie-breaking). The miners process items by
   code or head index.
2. **Loop direction.**
   - Ascending (`dir = +1`): simple, complex and single for frequent and
     generator targets. Most frequent first with the default coding.
   - Descending: simple and complex for closed and maximal targets, and
     top-down always.
3. **Post-order.** A set is reported after all sets in its subtree. The
   empty set (if reported) comes last.
4. **Pex.** Which items become pex, through which mechanism, and in which
   order (chain bottom-up, projection ascending, fim16 descending through
   `filter`). This changes both the item order inside lines and how a
   pex group is expanded into lines.
5. **fim16.** The machine has its own traversal (`rec_pos`: bits
   0,1,2,3,4..; `rec_neg`: bits n−1..2,1,0). With `dir > 0` the packed
   head is processed before the regular items, with `dir < 0` after them.
6. **Reordering** (`-Ac`). Heads are sorted by descending conditional
   support, ties by the `i2i_qsort` algorithm.
7. **Tree shape** (simple and single only). Chain detection depends on
   duplicate nodes, i.e. on transaction order. With the CLI the order is
   canonical (lexicographic and reduced). Under shuffled order, the API
   driver showed different line order for simple and single without
   fim16 in 10 of 40 runs, while the sets were equal.

Observed equal exact order, 40 random datasets per row (`-s-2`):

| Target | Always identical (40/40) | Never identical |
|---|---|---|
| frequent | `-As` ≡ `-Ad`; `-As -l0` ≡ `-Ac -j -l0` (39/40 vs `-Ad -l0`, which differed only in the order of two pex items inside one line) | `-At` vs everything (0/40) |
| closed, maximal | `-As` ≡ `-Ad` (forced); `-Ac` ≡ `-Ac -j`; `-As -l0` ≡ `-Ac -l0` ≡ `-Ad -l0` | — |
| generators | `-As`, `-Ad`, `-At`, `-Ac -j`, with or without fim16, all identical (pex are ignored) | `-Ac` (reorder) vs the others (5/40) |
| rules (checked on 3 files only) | `-As` ≡ `-Ad` ≡ `-At` ≡ `-Ac -j`; `-Ac` ≡ `-As -q1` | |

A port that wants byte-identical output must reproduce each variant's
traversal, fim16 behaviour, pex call order and the exact sorting
algorithms.

---

## 6. Suspected bugs, undefined behaviour and unsafe assumptions

Reproducers use `FPG=<reference fpgrowth binary>`. Data files are shown
inline; `\n` means a newline.

**B1. Top-down item map copied from the wrong array: crash or wrong item
names (confirmed).**

- **Evidence.** fpgrowth.c:1660:
  `memcpy(tree->items, d, (size_t)k *sizeof(ITEM));`. Here `d` is the
  old→new map (`d[i] = m` or −1, 1645-1650), but `tree->items[newid]` must
  be the old item (`s`, 1649). With any eliminated item below the last
  code, `items[]` holds −1 and shifted codes. `isr_add(-1, ...)` writes
  `pxpp[-1]` (heap corruption), and the output prints `inames[-1]`.
- **Triggers.** Any item contained in every transaction (a top-level pex,
  with default `FPG_PERFECT`). Under the default `-q2` that item gets
  code 0, so `-At` crashes on **every** such dataset unless `-x` is
  given. Also any item made infrequent by `tbg_filter` (`-m ≥ 2`), and
  `FPG_NORECODE` (API).
- **Reproducers.**
  - `printf 'x a b\nx a\nx b c\nx a b c\nx c\n' > t2; $FPG -At -s20 t2 -`
    exits with 139 (SIGSEGV). With `-x` it is correct.
  - `printf 'a b\na c\nb c\nd\nd\nd\n' > t3; $FPG -At -m2 -q1 -s10 t3 -`
    exits with 139.
  - In the random sweep, `-At` crashed on 10 of 40 datasets (exactly the
    ones with an item in every transaction) for every option set except
    `-x`, `-q-1` and `-q-2`. Some runs did not crash but printed wrong
    names, e.g. `i2 i1 ALL` for a set that does not exist.
- **Fix verified.** Copy `s` instead of `d`. With that one-line change,
  all 9,600 cross-variant runs agree.
- **Go.** Implement the intended semantics (new→old map) and record it in
  `docs/differences.md` (upstream crashes).

**B2. `supp == 0` is not prevented: crash or support-0 sets (confirmed).**

- **Evidence.** fpgrowth.c:1915-1920 never clamps.
  - In `proj_*` and `rec_single`, items with conditional support 0 pass
    the `< supp` test and become heads with `list == NULL`.
  - `rec_simple:517`, `rec_smp16:702`, `rec_cmplx:1162` and
    `rec_single:1297` then dereference `h->list->succ`.
  - With fim16, the machine reports sets with support 0.
- **Reproducers.** On `t3` above:
  - `$FPG -As -s0` → 139.
  - `-Ad -s0` → 139.
  - `-Ac -s0 -l0` → 139.
  - `-Ac -s0` prints `a c b (0)`.
  - `-At -s0` is correct (it only iterates existing nodes).
- **Triggers.** `-s0`, `-s-0` and `-s-0.0`. For rules, `-c0` gives
  `supp = 0`, but `fpg_tree` and the istree tolerate it (the istree
  clamps).
- **Go.** Clamp `supp` to at least 1 (likewise `body`), or reject 0, and
  document the difference.

**B3. Generators with `zmin ≥ 2` miss generators (confirmed).**

- **Evidence.** fpgrowth.c:1946-1949 runs `tbg_filter(tabag, zmin)` for
  generators too. Emptying transactions shorter than `zmin` lowers the
  supports of sets smaller than `zmin`. `is_isgen` then compares a set
  against deflated one-item-smaller subsets and wrongly rejects it.
  Closed, maximal and frequent targets are unaffected (they depend only
  on supersets; the oracle agrees).
- **Reproducer.** `printf 'a\na b\na b\nb c\n' > gen`:
  - `$FPG -tg -s-1 -m1 gen -` → `a (3)`, `b a (2)`, `b (3)`, `c (1)`.
  - `-tg -s-1 -m2` prints **nothing**, but `{a,b}` is a generator of size
    2.
  - The istree path gives the right answer: `-tg -s-1 -m2 -ec -d0` →
    `b a [2]`.
  - A patched build that skips the filter for generators matches the
    oracle (920 + 132 mismatches dropped to 0).
- **Go.** Skip the size filter for generators. This is a documented
  difference.

**B4. Maximal target: when every frequent item is a top-level pex,
nothing is reported (confirmed).**

- **Evidence.** The final `isr_report` with `cnt == 0` gets
  `r = cm_supp() = root.supp ≥ 0`. For maximal, `rep->sto = 0`, so
  `if (r >= rep->sto) return 0` always fires
  (`tract/src/report.c:1473`). The set "∅ + pex" is never output.
- **Reproducer.** `printf 'a\na b\na c\na\n' > mp; $FPG -tm -s-2 mp -`
  prints nothing. `-tm -x` and `-tc` print `a (4)`.
- **Where it lives.** The cause is in the reporter, but the trigger is
  the core's top-level pex handling. It affects every variant. Coordinate
  with the report audit.

**B5. Closed and maximal targets with `-n` (zmax) can report non-closed
or non-maximal sets (confirmed).**

- **Mechanism.** At depth `cnt == xmax−1`, no projection is created
  (`isr_xable(2)` is false), so the perfect extensions of the size-`xmax`
  sets are never collected. These sets enter the closed/maximal repository
  **without their closure**. A later branch then accepts a subset whose
  only same-support superset was never recorded in full.
- **Masking.** The chain shortcut and the 16-items machine ignore `xmax`,
  so they hide the problem in many cases. Top-down has neither, so it is
  hit most often.
- **Reproducers.**
  - Closed. `printf 'i0 i3 i2 i1 x 2\ni0 i3 i2 i1 y 2\ni0 i3 5\ni0 3\ni2 z 1\n' > zm`;
    `$FPG -w -tc -s-1 -n2 -q-1 -v' [%a]' zm -` with `-As -l0`,
    `-Ac -j -l0` or `-At` prints `i2 i1 [4]`. That set is not closed:
    `{i0,i1,i2,i3}` has support 4. `-As` and `-Ac` (with the machine) are
    correct.
  - Maximal. `printf '\ni5 i2 i0 i7 i3\ni2\ni1\ni1 i3 i7 i8\ni0 i5 i6\n' > mz`;
    `$FPG -tm -s-1 -n2 -At -x -v' [%a]' mz -` prints `i3 i7 [2]`, which
    is not maximal.
- **Go.** Reproducing the behaviour exactly requires porting this
  faithfully. Collecting pex (conditional supports) at the `xmax`
  boundary as well is **not** enough (Stage 3 investigation,
  `docs/m5-investigation.md`): the missing witnesses are supersets of
  smaller support, beyond the limit or inside subtrees pruned by genuine
  stored supersets, so the mechanism above is only part of the story. The
  port limits the search and checks every accepted set against all items
  of the database (deviation M5).

**B6. Spurious empty set and an off-by-one set count with the fast output
format (confirmed; report.c).**

- **Mechanism.** `fastout()` prints the base set without testing
  `cnt >= zmin` (`tract/src/report.c:1336-1366`). `isr_report` only
  checks `cnt + n >= zmin` (1489-1491). The core's final empty-set
  `isr_report` with top-level pex therefore prints `" (W)"`.
- **When fast output is on.** The info format must be exactly `" (%a)"`.
  That is the **CLI default whenever `-s` is negative**
  (fpgrowth.c:2459) and also requires `zmin ≤ 1`, no `zmax` and no
  border (`report.c:145`).
- **Reproducer.** `printf 'a\na b\n' > e; $FPG -s-1 e -` prints
  `b a (1)`, `b (1)`, `a (2)` and a final line ` (2)`, and reports 4
  sets. `-s50` (format `" (%S)"`) or `-v' [%a]'` do not print it.
- **Variants.** With `-x` this happens only where fim16 runs at top level
  (`-As` and `-Ad`; `-Ac` and `-At` do not).
- **Ownership.** The report audit owns the fix. The core port must keep
  the final empty-set call.

**B7. Negative transaction weights make results depend on the variant
(confirmed; input not validated).**

- **Evidence.** `strtosupp` accepts any integer
  (`tract/src/tract.c:711-718`). The miners assume monotone supports: the
  `>=` pex test, `m16_add`'s first-insertion test, and the tree supports.
- **Reproducer.** `printf 'a b -1\na 2\n' > wn; $FPG -w -s-1 wn -` runs.
  Random data with weights in {−2, −1, 1..5}: in 48 of 60 datasets the
  set of results for `-ts` differs between `-As`, `-Ac`, `-Ad` and `-At`.
- **Go.** Reject weights `≤ 0`, or define a clear behaviour. The tract
  audit should decide.

**B8. Data modes that break preconditions (API only; confirmed with the
C driver).**

- `FPG_NOPACK` with simple or single and a non-zero FIM16 field. The
  forced packed head 0 swallows real item 0. **Simple** exits with 139;
  **single** silently drops every set containing item code 0.
- `FPG_NOSORT` (P1 violated). Without fim16:
  - simple: wrong sets or SIGSEGV;
  - complex: wrong sets, SIGBUS or SIGSEGV;
  - top-down: wrong sets or SIGSEGV;
  - single: **infinite loop in 19/20** datasets (lists get cycles).

  With fim16, simple and single passed on the test datasets, presumably
  because every frequent item was packed.
- `FPG_NORECODE` alone. Correct for s, c and d (same sets, different
  codes); top-down crashes (B1).
- `FPG_NOREDUCE` and `FPG_NOFILTER`. Correct.
- Note that `fpgpsp.c` passes these modes (§1.8).
- **Go.** If these flags are exposed, either sort internally or reject
  combinations that cannot work.

**B9. Out-of-range threshold conversions are UB (confirmed on arm64;
x86-64 behaviour is a hypothesis).** See §4. On arm64,
`-s-3000000000` → "no (frequent) items found". `-S` with a huge absolute
value saturates harmlessly on arm64.

**B10. `isr_tail` swallows memory errors (hypothesis, code reading).**
`report.h:507` maps `cm_tail() == -1` to 0 ("not prunable"), and mining
continues after a failed `cmt_xproj`. It is benign apart from the lost
error, but a Go port should propagate the error.

**B11. Documentation mismatches (confirmed).**

- The help text says rules or rule-based measures "enforce variant 'd'"
  (fpgrowth.c:2126-2127). In fact `fpg_tree` and the istree are used.
- The help text says `-l` only applies to `s` and `d`; `-Ac` uses it as
  on/off.
- Neither the help nor the comments mention that `-Ac` (REORDER flag
  kept) changes item coding, and so the output order, even for rules.

**B12. Minor and benign (hypothesis, code reading).**

- `rec_*` return values can be positive (1 from HUT, or a fim16 mask).
  Callers treat them as success; preserve the `< 0` error checks
  exactly.
- `proj_smp16`'s `n <= 0` test is dead code.
- `tbg_pack` does not invalidate cached item counts (P5).
- `m16_add` misbehaves with weight ≤ 0 (needs `NOREDUCE` or negative
  weights).
- `E_VARIANT` (−16) in `main` has the same value as tract's `E_ITEMEXP`
  (−16). This is out of scope; the messages are resolved separately.

---

## 7. Proposed C → Go mapping

Suggested package: `internal/fpgrowth`. Its collaborators are
`internal/tract` (Bag, ItemBase), `internal/report` (Reporter),
`internal/fim16` (Machine), `internal/clomax` and `internal/istree`.

| C | Proposed Go | Notes |
|---|---|---|
| `struct _fpgrowth` | `type Miner struct` | Keep `supp`, `body`, `dir` and the scratch buffers as fields or locals. |
| `FPG_*` targets, modes, variants | `Target`, `Variant`, `Mode` (bit set), `DataMode`; `VerboseLogger` (or `io.Writer`) instead of the sign bit | Keep the FIM16 count separate from the flags (`PackItems int`). |
| `fpg_create` | `NewMiner(cfg Config) *Miner` plus `cfg.normalize()` | The normalisation must follow §1.4 exactly, in order. |
| `fpg_delete` | not needed (GC) | |
| `fpg_data` | `(*Miner).PrepareData(bag *tract.Bag, mode DataMode, sort int) error` | Thresholds as in §4. Returns `ErrNoItems`. |
| `fpg_report` | `(*Miner).ConfigureReporter(r *report.Reporter) error` | |
| `fpg_mine` | `(*Miner).Mine(ctx context.Context, prune int, order int) error` | `ctx` checked where `sig_aborted()` is. |
| `fpgvars[]` | `switch m.algo` | |
| `FPNODE`/`FPHEAD`/`FPTREE` | `fpNode{id int32; supp int64?; parent, succ int32}` in an index arena; `fpHead`; `fpTree` | Prefer index handles in a slab over pointers. |
| `add_simple` / `add_smp16` | `(*fpTree).add(ids []int32, w)` / `(*fpTree).addPacked(...)` | Keep head-only matching; it is part of the observable behaviour (§5.3 item 7). |
| `proj_simple` / `proj_smp16` | `projectSimple` / `projectPacked` | |
| `rec_simple` / `rec_smp16` | `recSimple` / `recPacked` | |
| `fpg_simple` | `mineSimple` | |
| `CSNODE`/`CSHEAD`/`CSTREE` | `csNode`, `csHead`, `csTree` | |
| `add_cmplx` / `add_reord` | `(*csTree).add` / `(*csTree).addFlags` | |
| `proj_cmplx` / `proj_reord` | `projectComplex` / `projectReorder` | `projectReorder` needs `arrays.IndexQsortInt` (an exact port of `i2i_qsort`). |
| `rec_cmplx`, `fpg_cmplx` | `recComplex`, `mineComplex` | |
| `rec_single`, `fpg_single` | `recSingle`, `mineSingle` | |
| `TDNODE`/`TDTREE` | `tdNode`, `tdTree` | |
| `add_topdn`, `getsupp`, `merge`, `copy` | `(*tdTree).add`, `sumCondSupp`, `mergeTD`, `copyTD` | Recursive is fine. |
| `rec_topdn`, `fpg_topdn` | `recTopDown`, `mineTopDown` | Use the **new→old** map (B1). |
| `rec_tree`, `fpg_tree` | `recISTree`, `mineISTree` | |
| `cleanup` | inline / defer | |
| `MEMSYS` with `ms_push`/`ms_pop` | `arena[T]` with `mark()`/`release(mark)` | The release point per level is not observable; it may be per item. |
| `fpt_show` / `cst_show` / `tdt_show` | optional `debugDump` | |

---

## 8. Porting risks and details that must not be missed

- **R1. Sorting must be exact.**
  - `i2i_qsort` (proj_reord): unstable quicksort, median-of-3,
    `TH_INSERT = 2`, then a full insertion pass, then reversed for
    descending.
  - `idm_sort` in recode (tract audit).
  - `tbg_sort`: lexicographic order with `TA_END` as the smallest item.

  These decide item codes, head order and tie handling, and therefore
  output order. `sort.SliceStable` will not reproduce them.
- **R2. `-q` is negated, and normalised under REORDER.** The REORDER flag
  is cleared only for non-complex variants and closed or maximal targets,
  so it changes coding even for rules (§1.4, §1.5).
- **R3. Head-only node matching** in `add_simple`/`add_smp16` changes
  tree shape, and so chain detection, pex order and output, whenever the
  transaction order is not canonical. Keep it and keep the reverse
  insertion order (`j = n−1 … 0`).
- **R4. The chain shortcut ignores `FPG_PERFECT` and `xmax`.** Top-down
  has none. Both choices shape output and B5.
- **R5. The packed head.**
  - It is always head 0. It is forced only when the FIM16 field is
    non-zero.
  - `heads[0].item` holds `TA_END | union bits` (simple) or 0 (single).
  - Packed bits equal item codes `< pack`.
  - The machine is mined first (`dir > 0`) or last (`dir < 0`), and its
    mask prunes later projections only when `dir > 0`.
  - With `dir > 0` the machine is filled during `add_smp16`.
  - One machine is shared across levels (simple) and per miner (complex).
- **R6. Pex bookkeeping is level-scoped in the reporter.** Pex found
  while processing the packed head or a projection stay until the parent
  item is removed. Pex may be re-added; that must be idempotent.
  Generators ignore pex.
- **R7. `dir`.**
  - +1 for frequent and generator targets in s/c; −1 for closed and
    maximal in s/c.
  - Single is always +1.
  - Top-down is effectively always −1 (descending ids), whatever
    `fpg->dir` says.
  - The closed/maximal filter is created with dir −1 regardless.
- **R8. `isr_xable` counts only prefix items.**
  - Projections need `xable(2)` (s/c/t) or `xable(1)` after adding the
    item (single).
  - `xmax = zmax+1` for closed and maximal; it is set in `isr_setup`.
  - fim16 ignores `xmax`.
- **R9. The empty set is reported** once at the end of each item-set
  miner, including the cases "no items left" and "k == 0". It is not
  reported by `fpg_tree`, nor when `supp > W`.
- **R10. Thresholds.** Copy the float expressions verbatim (§4). Decide
  how to handle `supp == 0` (B2) and out-of-range values (B9).
- **R11. Weights.** Supports are integer sums. Use `int64`, or `int32` if
  wrap-around must match C; that is a decision for `docs/decisions.md`.
  Validate the sign (B7).
- **R12. Generators and zmin (B3), maximal with top-level-only pex (B4),
  zmax closure (B5), top-down map (B1).** Each needs an explicit "match
  upstream" or "fix" decision, plus differential tests that know the
  difference.
- **R13. Errors.**
  - All negative returns propagate.
  - Positive returns from `rec_*` are not errors.
  - `fpg_mine` maps every failure to `E_NOMEM`, which loses the error
    type. A Go port should return typed errors but keep the CLI messages.
- **R14. Cancellation.** The upstream hook points are the entries of
  `rec_simple`, `rec_smp16`, `rec_cmplx`, `rec_single`, `rec_topdn` and
  `rec_tree`. A context check there gives the same granularity.
- **R15. Memory.** Per-level arenas grow with all sibling projections.
  Releasing per item in Go is fine and not observable.
- **R16. API-only modes** (`NOSORT`, `NOPACK` with FIM16, `NORECODE`)
  have undefined or buggy upstream behaviour (B8). Do not expose them
  unchanged.
- **R17. The default `(%a)` output with negative `-s`** uses fast output
  and is subject to B6. Golden fixtures produced with the reference CLI
  in that mode contain the spurious empty set.
