# Upstream audit: transaction and support modules (`tract.c`, `fim16.c`, `clomax.c`, `patspec.c`) as used by `fpgrowth`

Scope: the item base, transaction bag, sorting/recoding/filtering/reduction
and packing of transactions, the 16-items machine, the closed/maximal
repository and the pattern spectrum, **in the configuration the `fpgrowth`
program is built with**. The low-level tokenizer (`tabread.c`) is covered in
`docs/upstream/cli-io.md` §4.3; the reporter that drives `clomax`/`patspec`
is covered in `docs/upstream/report.md`. Paths are relative to
`reference/upstream/fpgrowth-6.21/`.

Status labels: **confirmed** = reproduced with the reference binary or a
sanitizer build (commands in Appendix A); **inspection** = follows directly
from the code, no run needed; **hypothesis** = plausible, not reproduced.

---

## 0. Build configuration actually used

`fpgrowth` links (`fpgrowth/src/makefile:46-55`):

| Object | Source and flags | Rule |
| --- | --- | --- |
| `taread.o` | `tract.c -DTA_READ` | `tract/src/makefile:165-167` |
| `fim16.o` | `fim16.c` (no extra flags; the file itself defines `ISR_CLOMAX`, `fim16.c:41-43`) | `:256-258` |
| `clomax.o` | `clomax.c` | `:303-305` |
| `patspec.o` | `patspec.c -DPSP_REPORT` | `:266-268` |
| `repcm.o` | `report.c -DISR_PATSPEC -DISR_CLOMAX` (the consumer of clomax/patspec) | `:363-366` |

All objects use `-DNDEBUG -O3` (`tract/src/makefile:30`), so **every
`assert()` precondition is unchecked** at run time (several of them are
violated by inputs the reader accepts, see §11).

Not defined: `TA_WRITE`, `TA_SURR`, `TATREEFN`, `TATCOMPACT`, `PSP_ESTIM`,
`PSP_TRAIN`, `ALIGN8`, the `*_MAIN` symbols. Consequently the
transaction-tree (`tat_*`/`tan_*`), surrogate, writer and estimation code is
dead; `istree.o` is also built without `TATREEFN`
(`apriori/src/makefile:96-98`).

Types (defaults, nothing overrides them):

| Name | Type | Where |
| --- | --- | --- |
| `ITEM` | `IDENT` = `int` | `tract.h:99-101`, `symtab.h:34-35` |
| `TID` | `int` | `tract.h:103-105` |
| `SUPP` | `TID` = `int`; `SUPP_MAX = INT_MAX`, `SUPP_EPS = 1` | `tract.h:107-109,256-289` |
| `RSUPP` | `SUPP` | `report.h:66-71`, `clomax.h:30-34`, `patspec.h:25-30` |
| `strtosupp(s,p)` | `(int)strtol(s,p,0)` | `tract.h:270` |
| `ceilsupp` / `floorsupp` | `ceil` / `floor` | `tract.h:272-276` |
| `TA_END` | `ITEM_MIN` = `INT_MIN` (sentinel) | `tract.h:431` |
| `BITTA` | `unsigned short` | `fim16.h:21` |

The item base is created with `ib_create(0, 0)` (`fpgrowth.c:2466`): string
item names, **no** `IB_WEIGHTS`. Therefore every `WTRACT`/`WITEM`
(item-weight) code path in `tract.c` is dead for fpgrowth and is not
described here.

### Functions reached by the fpgrowth program

- Item base: `ib_create`, `ib_delete`, `ib_readsel`, `ib_readapp` (+ static
  `appcode`, `appcmp`), `ib_read` (via `tbg_read`), `ib_errmsg`,
  `ib_recode` (via `tbg_recode`), macros `ib_cnt`, `ib_name`, `ib_maxfrq`,
  `ib_getwgt`, `ib_getfrq`; `ib_getapp` and `ib_xname` from
  `istree.c`/`report.c`.
- Bag: `tbg_create`, `tbg_delete`, `tbg_read`, `tbg_add`, `tbg_recode`
  (+ static `recode`), `tbg_filter`, `tbg_itsort`, `tbg_sort` (+ static
  `sort`, `pksort`), `tbg_reduce`, `tbg_pack` (+ `ta_pack`), `tbg_ifrqs`
  (+ static `tbg_count`), macros `tbg_cnt`, `tbg_wgt`, `tbg_max`,
  `tbg_itemcnt`, `tbg_base`, `tbg_tract`, `tbg_errmsg`, `ta_items`,
  `ta_wgt`, `ta_size`; comparators `ta_cmp`, `ta_cmpoff`, `ta_cmpsfx`,
  `ta_cmpsep` (inside `tbg_sort`).
- Util: `idm_sort` → `ptr_qsort`; `ptr_mrgsort`, `int_qsort`,
  `ptr_bisect`, `ptr_reverse` (`util/src/arrays.c`, `symtab.c`).
- `fim16.c`: `m16_create`, `m16_delete`, `m16_setmap`, `m16_add`,
  `m16_mine` (+ static `count`, `project`, `clear`, `filter`, `rec_pos`,
  `rec_neg`). `m16_addx/adds/addta/addtbg/clear` are unused.
- `clomax.c` (only through `report.c`): `cm_create`, `cm_delete`,
  `cm_add`, `cm_supp`, `cm_update`, `cm_remove`, `cm_tail` and the
  `cmt_*` helpers they call. `cm_addnc`/`isr_addnc` are not used by
  fpgrowth.
- `patspec.c` (through `report.c` and `main`): `psp_create`, `psp_incfrq`
  (+ static `resize`), `psp_error`, `psp_report`, `psp_sigcnt`,
  `psp_delete`.

Not reached: `ib_add2ta`, `ib_finta`, `ib_readpen`, `ib_frqcnt`,
`ib_trunc`, `tbg_trim`, `tbg_mirror`, `tbg_sortsz`, `tbg_istab`,
`tbg_occur`, `tbg_ipwgt`, `tbg_unpack`, `tbg_clone/copy`, all `taa_*`,
`wta_*`, `wi_*`, `tat_*`.

---

## 1. The data pipeline, in exact order

1. `ib_create(0,0)` (`fpgrowth.c:2466`). Optional `-R file`:
   `ib_readapp` for target `r`, otherwise `ib_readsel`
   (`fpgrowth.c:2476-2478`).
2. `tbg_create(ibase)`; `tbg_read(tabag, tread, mtar)` with
   `mtar = TA_WEIGHT` iff `-w` (`fpgrowth.c:2400,2486-2492`). Every record
   → `ib_read` (dedup, item-base counters) → `tbg_add` (copy into the
   bag). **All transactions are retained in memory, including items that
   will later turn out infrequent.**
3. `m = ib_cnt`, `n = tbg_cnt`, `w = tbg_wgt`; `m <= 0 || n <= 0` →
   `E_NOITEMS` (`fpgrowth.c:2496-2502`). `m` counts every item in the base
   (including `-R` items that never occur); `n` counts empty transactions.
4. `fpg_create` adjusts modes (`fpgrowth.c:1819-1844`): `FPG_REORDER` is
   kept only for algorithm `c` and targets other than `c`/`m` (and no
   `-j`); `FPG_FIM16` (packing) is removed for `-At`, rules, and
   rule-measure evaluations.
5. `fpg_data` (`fpgrowth.c:1898-1966`):
   1. `smin = ceil(-s)` if `-s` < 0, else `ceil(s/100 · w · (1-DBL_EPSILON))`;
      `fpg->supp = (SUPP)smin` (rules: times confidence) (`:1914-1920`).
   2. `pack = mode & 0x1f` capped to 16; 0 for `-Ac` (packing happens in
      the recursion) (`:1923-1928`).
   3. If `FPG_REORDER`: `sort = sign(sort)` (`:1934-1935`), then
      `tbg_recode(tabag, supp, -1, -1, -sort)` (`:1936`); 0 items →
      `E_NOITEMS`.
   4. If not rules and evaluation is none or `b`: `tbg_filter(tabag, zmin,
      NULL, 0)` (`:1946-1949`).
   5. `tbg_itsort(+1)`, `tbg_sort(+1, 0)`, `tbg_reduce(0)` (`:1950-1955`).
   6. If `pack > 0`: `tbg_pack(tabag, pack)` (`:1956-1957`) — only for
      `-As`/`-Ad`.
6. `isr_create(ibase)` reads `ib_getwgt` (empty-set support) and
   `ib_getfrq` (for the `-eb` log-probabilities) (`report.c:702-708`);
   `fpg_report` → `isr_settarg(..., -1)` creates `cm_create(-1, ib_cnt)` for
   closed/maximal (`fpgrowth.c:2005`, `report.c:789-802`); `-P` →
   `isr_addpsp` → `psp_create(zmin, zmax, smin, smax)`
   (`report.c:1894-1906`).
7. Mining (`fpg_simple/cmplx/single/topdown`, e.g. `fpgrowth.c:742-801`):
   `pex = tbg_wgt` (perfect-extension threshold), `f = tbg_ifrqs(bag,0)`
   (frequencies **after** filter/reduce/pack), item map dropping
   `f[i] < supp` and turning `f[i] >= pex` into perfect extensions, then
   insertion of **every** bag transaction in reverse order
   (`for (j = n; --j >= 0; )`, `:782,797,1242,1410,1661,1781`).
8. `-P`: `psp_report(psp, twr, 1.0)`, `psp_sigcnt` (`fpgrowth.c:2533-2547`).

---

## 2. Item base (`ITEMBASE`, `tract.h:476-501`, `tract.c:371-942`)

### 2.1 Data structures

- `ITEMBASE`: `IDMAP *idmap` (hash table + id array, `symtab.c`), `wgt`
  (total transaction weight), `max` (running maximum item frequency),
  `mode` (0), `app` (default appearance for *new* items, initially
  `APP_BOTH`), `pen` (unused), `idx` (current transaction index, starts at
  1), `size`/`tract` (one reusable transaction buffer, initially 1024
  items), `err`, `trd`.
- `ITEMDATA` (per item, stored in the idmap entry): `id` (must be first:
  the idmap writes the identifier there, `symtab.c:357-358`), `app`,
  `pen`, `frq` (Σ weights), `xfq` (Σ size·weight), `idx` (index of the last
  transaction that contained the item — used for duplicate detection). 32
  bytes + symbol-table entry + the name.

### 2.2 Item identity and identifiers

- An item is the field string returned by the tokenizer, compared as a C
  string (`st_strcmp`/`st_strhash`). **Bytes after an embedded NUL are
  ignored**: `a\0b` and `a\0d` are the same item `a` (**confirmed**, R13).
  Fields are truncated at 1024 bytes by the tokenizer (cli-io B14).
- Identifiers are dense `0..n-1` **in order of first insertion**
  (`symtab.c:355-359`): `-R` file order first, then first appearance in the
  data (records in order, fields left to right). Hashing never influences
  ids or output. Items are never removed until `ib_recode`.

### 2.3 Item selection file — `ib_readsel` (`tract.c:568-592`)

- Sets `base->app = APP_NONE` (default for *later* new items) **before**
  reading; every field of every record is an item name (record structure
  is irrelevant); each new item gets `app = APP_BOTH`, counters 0.
- Duplicate names are skipped silently (`EXISTS → continue`).
- Empty field (e.g. `a,,b`) → `E_ITEMEXP`; read error → `E_FREAD`; EOF →
  success. An empty file is accepted (0 items) — then every data item is
  ignored and the run ends with `no (frequent) items found` (**confirmed**).
- Effect on `ib_read`: unknown names are skipped entirely (not added, not
  part of the transaction, do not contribute to the transaction size used
  for `xfq`). A record consisting only of unselected items becomes an
  **empty transaction that still counts in `n` and the total weight**
  (**confirmed**: `a`/`x`/`x y` with selection `a` → `a (1 33.3333)`).
- Selected items that never occur keep `frq = 0` (see §11 T2 for `-s0`).

### 2.4 Item appearance file — `ib_readapp` (`tract.c:596-632`, target `r` only)

- First record must be exactly one field: the default appearance for
  items not listed (`d != TRD_REC` → `E_FLDCNT`, so an **empty file is an
  error** "too many fields/columns", **confirmed**).
- Each further record: `item indicator`. One field only → `E_APPEXP`; three
  or more → `E_FLDCNT`; duplicate item → `E_DUPITEM`; unknown indicator →
  `E_UNKAPP`.
- `appcode` (`tract.c:277-294`) sorts `appmap` once (`ptr_qsort` with
  `appcmp`, static flag), `ptr_bisect`s for the first name `>=` the field
  and accepts the field if it is a **prefix** of that name. So
  abbreviations resolve to the lexicographically first match: `bo` →
  `body` (1), `he` → `head` (2), `an` → `ante` (1) (**confirmed**). The
  empty string maps to `-` (0). Codes: 0 none, 1 body, 2 head, 3 both
  (`tract.h:420-423`, table `tract.c:182-191`, `x` added in 2022).
- Items listed with indicator 0 (`-`, `n`, `none`, `neither`, `ignore`)
  **are** read into transactions and counted (and contribute to transaction
  sizes / `xfq`), but are dropped by `ib_recode`. New items under a default
  of 0 are skipped at read time like unselected items. The flags are later
  read by `istree.c` via `ib_getapp`.

### 2.5 Reading one transaction — `ib_read` (`tract.c:683-816`)

Per call: `++base->idx`; buffer `size = 0`, weight 1. Field loop
(`do … while (d == TRD_FLD)`), with `d` from `trd_read`
(`TRD_ERR=-2, TRD_EOF=-1, TRD_FLD=0, TRD_REC=1`, `tabread.h:46-49`):

| Situation | Behaviour |
| --- | --- |
| `d <= TRD_ERR` | `E_FREAD` |
| `d == TRD_EOF` | return 1 = end of input. `trd_read` returns EOF only if not a single character could be read for this field (`tabread.c:224-227`). **If this happens after a field separator of a started record (input ends with `…,` and no newline), the partial record is discarded** but its new items stay in the item base with `frq = 0` (**confirmed**, R3). |
| `d == TRD_REC` and `-w` | this last field is the weight: optional `(`/`[`/`{`, `(int)strtol(…, 0)` (base 0: `0x10`=16, `010`=8, sign allowed), matching closer if opened, nothing else (`tract.c:706-720`). Empty → `E_WGTEXP`; garbage → `E_TAWGT`. A record with only a weight is an empty transaction with that weight. Out-of-range values are truncated `long → int` (`4294967297` → 1, `99999999999999999999` → `LONG_MAX` → -1) (**confirmed**). |
| empty field | end of transaction if `TRD_REC` (trailing separator or empty line → empty transaction), else `E_ITEMEXP` (`:721-724`). |
| unknown name, `base->app == APP_NONE` | skipped (`:726-734`). |
| unknown name otherwise | appended to the item base: next id, `app = base->app`, counters 0 (`:735-741`). |
| name already in this transaction (`itd->idx >= base->idx`) | `TA_DUPERR` not used, `TA_DUPLICS` not used → **duplicate silently dropped, first occurrence kept** (`:743-756`; **confirmed** `a a b` → `{a,b}`). |
| otherwise | `itd->idx = base->idx`; append id (buffer grows by 1024, then ×1.5). |

After the loop (`:801-814`): sentinel `TA_END`; `base->wgt += wgt`;
`w = size · wgt`; for every item in the transaction: `xfq += w`,
`frq += wgt`, `max = max(max, frq)`. All in `int`.

Hence, at the tract level:

- An item's `frq` is the sum of weights of the records containing it
  (duplicates counted once). Zero and negative weights are accepted
  ("2011.08.23 negative transaction weights no longer prohibited",
  `tract.c:61`).
- `size` in `xfq` is the number of **distinct, non-skipped** items of the
  record — it includes items that will later be infrequent and
  appearance-0 items, excludes unselected items.
- Item order inside the stored transaction = first-occurrence order (later
  re-sorted by `tbg_itsort`).
- Comment records (first byte a comment char) are skipped by the tokenizer
  and do not count.

### 2.6 Recoding — `ib_recode` (`tract.c:864-916`)

Called as `tbg_recode(bag, min = fpg->supp, max = -1, cnt = -1, dir = -sort)`:

1. `max < 0 → SUPP_MAX`, `cnt < 0 → ITEM_MAX`.
2. Every item with `frq < min || frq > max` gets `app = APP_NONE`
   (`:879-883`). `frq` is the **read-time** frequency (before the `zmin`
   filter). Items already `APP_NONE` (appearance file) stay so regardless
   of frequency.
3. Comparator by `dir` (`:884-888`): `dir > 1` `asccmpx`, `dir == 1`
   `asccmp`, `dir == 0` `nocmp`, `dir == -1` `descmp`, `dir < -1`
   `descmpx`. All comparators (`tract.c:299-365`) first put `APP_NONE`
   items **after** all others (two `APP_NONE` items compare equal), then
   compare `frq`/`xfq`/`id`; **no secondary key** — ties return 0.
4. `idm_sort(idmap, cmp, NULL, map, 1)` (`symtab.c:501-518`):
   `ptr_qsort` over the pointer array in current-id order, then position =
   new id; `map[old] = new`.
5. `n` = number of non-`APP_NONE` items (scan from the end), capped by
   `cnt`; `idm_trunc` deletes the rest; `map[k] = -1` for removed items.
   Returns `n` (0 → fpgrowth reports `E_NOITEMS`).

`ITEMDATA` records move with their item, so after recoding `ib_name`,
`ib_getfrq` etc. are indexed by the new code. The item base counters are
**never updated again** (they stay the read-time values).

### 2.7 Sorting modes `-q` and tie-breaking

`FPG_REORDER` is on iff algorithm `c` (default), target not `c`/`m`, and no
`-j` (`fpgrowth.h:68`, `fpgrowth.c:1831-1836`); it clamps `sort` to its sign
(`fpgrowth.c:1934-1935`).

| `-q` | REORDER | effective `sort` | `dir` | comparator | item code 0 is |
| --- | --- | --- | --- | --- | --- |
| `>= 2` (default 2) | off | `s` | `-s` | `descmpx` | largest `xfq` |
| `>= 2` | on | 1 | -1 | `descmp` | largest `frq` |
| 1 | – | 1 | -1 | `descmp` | largest `frq` |
| 0 | – | 0 | 0 | `nocmp` | first-inserted item |
| -1 | – | -1 | 1 | `asccmp` | smallest `frq` |
| `<= -2` | off | `s` | `-s` | `asccmpx` | smallest `xfq` |
| `<= -2` | on | -1 | 1 | `asccmp` | smallest `frq` |

**Confirmed** (R6): with `-Ac -ts`, `-q2`≡`-q1` and `-q-2`≡`-q-1`
byte for byte; with `-As` or `-tc` they differ. (The help text calls
`-q1` "ascending"; in code terms `-q1` gives code 0 to the most frequent
item.)

Tie-breaking is decided entirely by `ptr_qsort` (`arrays.c:516-585`,
`TH_INSERT = 2` at `arrays.c:47`): a median-of-three quicksort with a
Hoare-style partition that skips pivot-equal elements at the crossing point
(`ptr_qrec`), followed by a (here no-op) insertion pass. It
is **deterministic but unstable**, and the relative order of tied items
depends on the **whole** array, including the items that are about to be
removed (**confirmed** with a harness linking `arrays.c`, R7):

| Keys in id order (`x` = `APP_NONE`) | Result (ids) |
| --- | --- |
| `5 5 5 5 5 5` | `0 4 3 2 1 5` |
| `5 5 5 5` | `0 2 1 3` |
| `5 5 5 5 x` | `0 3 2 1 [4]` |
| `5 x 5 5 5` | `0 3 2 4 [1]` |

A Go port must therefore: build the item array in first-insertion order,
include every item (also infrequent/`APP_NONE` ones), apply the identical
comparator, and run a literal port of `ptr_qrec` + the final pass. Using
`sort.Slice`/`sort.Stable`, or filtering infrequent items first, changes
item codes and therefore output order.

---

## 3. Transactions and the transaction bag (`TRACT`, `TABAG`)

### 3.1 Representation

- `TRACT` (`tract.h:503-508`): `wgt` (SUPP), `size` (ITEM), `mark` (unused
  by fpgrowth), `items[size+1]` terminated by `TA_END = INT_MIN`. One
  `malloc` per transaction: 16 + 4·size bytes (64-bit).
- Iteration convention everywhere: `for (p = items; *p > TA_END; p++)`.
- **Packed entry** (after `tbg_pack`): one negative element
  `TA_END | bits` (bits = items `0..pack-1`), which is `> TA_END` as long
  as `bits != 0`; consumers test `*p < 0`. Packing leaves `size`
  unchanged and fills the freed tail with extra `TA_END`s
  (`tract.c:1083-1084`), so `ta_size()` is no longer the element count.
- `TABAG` (`tract.h:522-534`): `base`, `mode` (gets `TA_PACKED` bits),
  `max` (largest size), `wgt` (total weight), `extent` (Σ sizes, `size_t`),
  `size`/`cnt`/`tracts` (pointer array), `icnts`/`ifrqs` (one cached block),
  `buf` (unused).

### 3.2 `tbg_read` / `tbg_add` (`tract.c:1865-1880`, `1799-1823`)

`tbg_read` frees cached counts, then loops `ib_read`; `< 0` error, `> 0`
EOF → return 0; otherwise `tbg_add(bag, NULL)` clones the item-base buffer
(`ta_clone`), appends it (array grows by 1024, then ×1.5), `wgt += t->wgt`,
updates `max` and `extent`. Every record becomes one bag entry, also empty
ones, zero/negative weight ones and those that will lose all items.

### 3.3 `tbg_recode` / `recode` (`tract.c:1988-1999`, `1944-1984`)

Allocates `map[ib_cnt]`, calls `ib_recode`, then rewrites every
transaction in place: `items[i] = map[items[i]]`, items mapped to -1
dropped, **relative order preserved**, new sentinel; recomputes `max` and
`extent`; frees cached counts. Returns the new item count (-1 on OOM).

### 3.4 `tbg_filter(bag, zmin, NULL, 0)` (`tract.c:2003-2049`)

With `marks == NULL`: `zmin <= 1` → returns immediately (no-op). Otherwise
every transaction with `size < zmin` gets `size = 0` (becomes empty, **keeps
its weight**, stays in the bag); recomputes `max`, `extent`; frees cached
counts. Runs after recoding, so sizes count only frequent items. Total
weight unchanged (**confirmed**: `a b`/`a`/`a` with `-m2` → `b a (1 33.3333)`).

### 3.5 `tbg_itsort(bag, +1, 0)` (`tract.c:2118-2143`)

`int_qsort` ascending on every transaction with ≥ 2 items. Items are
distinct (dedup in `ib_read`, injective recoding), so the result is unique.

### 3.6 `tbg_sort(bag, +1, 0)` (`tract.c:2266-2303`)

Target order = `ta_cmp` (`tract.c:1127-1139`): lexicographic on item codes,
the sentinel `INT_MIN` being smallest, so the empty transaction is first and
a proper prefix precedes its extensions.

- `cnt < 2` → nothing. `k = max(ib_cnt, 2)`.
- Buffer `n` pointers + `(k+1)` `TID` counters.
- `k < n` (fewer items than transactions): recursive bucket sort
  `sort()` (`:2205-2262`): count transactions per item at offset `o`
  (`-1` = ended, `0` = packed), scatter, skip ended ones, recurse on each
  run of equal items with `o+1`; runs of ≤ 16 use `ptr_mrgsort` with
  `ta_cmpsfx`. When all transactions share the item at `o`, it recurses
  without scattering — **recursion depth = length of the longest prefix
  shared by more than 16 transactions** (stack overflow, §11 T4).
- `k >= n`: stable `ptr_mrgsort` with `ta_cmp`.
- OOM fallback: `ptr_qsort` with `ta_cmp`.
- `dir < 0` reverses (fpgrowth uses `+1`).

All paths produce the same order up to identical transactions, which
`tbg_reduce` merges, so the post-reduce sequence is unique. Packed-item
handling in `sort`/`pksort` is unreachable for fpgrowth (packing happens
after sorting).

### 3.7 `tbg_reduce(bag, 0)` (`tract.c:2326-2360`)

Precondition: sorted (otherwise non-adjacent duplicates simply stay).
`cnt <= 1` → returns 1 without changes (also for an empty bag). Otherwise
adjacent transactions with equal size and `ta_cmp == 0` are merged into the
first (`wgt += `, others freed); a merged group with **total weight 0 is
deleted** (`keep0 == 0`); `extent` recomputed, `cnt` updated. `wgt` is not
recomputed (unchanged anyway) and **`max` is not recomputed** (may be stale,
only used to cap the istree size for rules). Empty transactions (blank
lines, emptied by `zmin`) collapse into one empty transaction carrying
their summed weight (log line `[4/5 transaction(s)]`, **confirmed** R1).

### 3.8 `tbg_pack(bag, pack)` / `ta_pack` (`tract.c:2384-2394`, `1066-1086`)

Only `-As`/`-Ad` with `-l > 0`. For each transaction: find the first item
`< pack` (index 0 because items are ascending), OR `1 << item` of all items
`< pack`, store `bits | TA_END` at that position, compact the remaining
items behind it, fill the tail with `TA_END`; `size` unchanged.
Transactions without such items are untouched. `bag->mode |= pack & 0x1f`.
`pack` is clamped to 31 inside `ta_pack`, to 16 by fpgrowth (BITTA is 16
bits). The bag is no longer guaranteed to be `ta_cmp`-sorted afterwards
(nothing relies on it).

### 3.9 `tbg_ifrqs(bag, 0)` / `tbg_count` (`tract.c:1791-1795`, `1745-1779`)

If no cached block: allocate `ib_cnt · (sizeof(TID)+sizeof(SUPP))`,
`icnts[i] += 1`, `ifrqs[i] += t->wgt` over all bag transactions; a packed
entry is counted under **item 0**, so after packing `ifrqs[0]` = weight of
transactions containing any packed item and `ifrqs[1..pack-1] = 0`. The
cache is invalidated by `tbg_add/read`, `recode`, an acting `tbg_filter`
and `tbg_reduce` (cnt > 1), **not** by `tbg_pack`; in fpgrowth the first
computation happens after packing, so it is consistent. With `zmin <= 1`
and no packing, `ifrqs[i]` equals the item base `frq` of the (recoded)
item; with `zmin >= 2` it is the post-filter frequency.

### 3.10 The total weight

`tbg_wgt(bag) == ib_getwgt(base)` = Σ of the weights of **all records
read**: including empty records, records whose items were all unselected,
records later emptied by the `zmin` filter, zero and negative weights;
excluding comment records and a final record lost to "separator + EOF"
(§2.5). Computed in `int`, never recomputed. It is the base of relative
supports (`-s`, `-S`, `%S`), the empty-set support, and the
perfect-extension threshold `pex` at the top level of every variant.

---

## 4. Streaming vs. all-transactions-in-memory

Upstream keeps the whole bag (raw, then recoded) for the whole run; peak
memory ≈ item base + all raw transactions (incl. infrequent items) + sort
buffer, then bag + FP-tree during mining (the bag is not freed).

| Step | Needs | Streamable? |
| --- | --- | --- |
| Tokenize, dedup, item ids, `frq`, `xfq`, total weight, `n` | one record at a time | **yes** (pass 1); item dictionary is O(#items) |
| `smin` from total weight | end of pass 1 | after pass 1 |
| Item sort / recode map (`ib_recode`) | item table only | after pass 1 |
| Recode + drop + `itsort` + `zmin` filter + pack of one transaction | the map | **yes** (per record) |
| Header frequencies `tbg_ifrqs` (post-filter, packed slot 0) and top-level perfect extensions (`f[i] >= pex`) | full pass over filtered transactions | yes, but must be **complete before tree insertion** (the item map `d[]` and header table depend on it). If `zmin <= 1` and no packing they equal pass-1 `frq` of kept items. |
| `tbg_sort` + `tbg_reduce` | all transactions | **not needed for identical output** (see below) |
| FP-tree insertion, top-level `m16_add` | one transaction | yes, in any order |

**Empirical result (confirmed, R8):** a patched build that replaces the
`tbg_sort`/`tbg_reduce` block with (1) a seeded random shuffle without
reduction, (2) sorting without reduction, (3) sort + reduce + reverse,
produced **byte-identical output** to the unmodified program in 5232 runs
(`-A{s,c,d,t}` × `-t{s,c,m,g}` × `-q{0,±1,±2}`, `-m2`, `-n3`, `-l4`,
`-l16`, unit and positive `-w` weights, 53 random data sets). So the
insertion order of (recoded) transactions and the merging of duplicates do
not affect output. Caveats for the Go design:

- This is about the order of insertion *after* recoding. The **input
  record order still matters** through first-insertion ids, which break
  frequency ties (§2.7).
- `add_simple`/`add_smp16` (`fpgrowth.c:395-422`, `537-585`) only share a
  prefix with the most recently created node of each item; unsorted
  insertion still yields correct results but duplicates paths (more
  memory). A Go tree builder should look children up properly.
- Recommended Go pipeline: **pass 1** count (dictionary in first-seen
  order, `frq`, `xfq`, total weight, `n`); compute recode map; **pass 2**
  (only if `zmin >= 2` or packing) recompute header frequencies on
  recoded/filtered transactions without storing them; **pass 3** insert
  into the tree. Two passes suffice when `zmin <= 1` and no packing (the
  packed-slot frequency is only used as the head support and can be
  summed during insertion). Standard input cannot be re-read: spool it to a
  temporary file (or fall back to buffering).
- Not streamable: the closed/maximal repository (O(output), §6) and the
  FP-tree itself.

---

## 5. 16-items machine (`fim16.c`)

### 5.1 Purpose

Mines all frequent item sets over at most 16 items using bit-represented
transactions (scheme of LCM3, `fim16.c:22-31`). fpgrowth uses it (a) at the
top level of `-As`/`-Ad` for the packed items (`pack` = `-l`, default 16)
and in their projections, and (b) in `-Ac`, for every conditional database
with ≤ 16 items (`proj_cmplx`, `fpgrowth.c:899-911`; `proj_reord`, `:1043-1055`). Not used by `-At`,
rules, or rule-measure evaluations (`fpgrowth.c:1837-1842`).

### 5.2 Data layout (`fim16.h:23-34`, `fim16.c:176-211`)

- A transaction is a `BITTA` (16-bit mask over local items `0..15`);
  `map[i]` (shared by all sub-machines, `m16_setmap`) translates a local
  bit to the reporter's item code.
- `hibit[65536]` (static, lazily initialised by the first `m16_create`,
  `hibit[0] == 1` is the "not initialised" flag, `fim16.c:129,182-184`):
  index of the highest set bit. `prjms[i] = (1<<i)-1` (`:132-136`).
- Per machine with `s` items: `wgts[2^s]` (SUPP weight per pattern),
  lists `btas[i]..ends[i]` of **distinct** patterns whose highest bit is
  `i` (capacity 2 for `i<2`, `2^i` otherwise; lists `0..9` share one
  1024-entry block, lists `>= 10` are separate allocations), `supps[16]`,
  `ttw` (total added weight), `tor` (OR of added patterns), `smin`, `dir`,
  `report`.
- `dir > 0`: one machine (16 items). `dir < 0`: an array of 16 machines
  with 16, 15, …, 1 items; `rec_neg` projects level `k` into `k+1`.
- Memory: ≈ 384 KiB for `dir > 0`, ≈ 0.8 MiB for `dir < 0`, allocated once
  per `m16_create`.

### 5.3 Functions

- `m16_create(dir, smin, report)`: as above; map = identity.
- `m16_setmap(fim, i, item)`: macro (`fim16.h:62`).
- `m16_add(fim, t, w)` (`:246-254`): `ttw += w`; `t == 0` → return (empty
  pattern only counts towards `ttw`); `tor |= t`; `wgts[t] += w`; **if the
  new value `<= w`** (i.e. the previous value was `<= 0`, "first time" for
  positive weights) append `t` to list `hibit[t]`.
- `count(fim, n)` (`:331-351`): for `j = n-1 … 2`: `supps[j]` = Σ weights
  in list `j`; each pattern's remainder `t & prjms[j]` is accumulated
  (`wgts[p] += w`, appended on first time) into the list of its own highest
  bit. Finally `supps[1] = wgts[2]+wgts[3]`, `supps[0] = wgts[1]+wgts[3]`.
  After this, list `j` holds `T ∩ {0..j}` for every added `T ∋ j`, which is
  the conditional database of `j`.
- `filter(fim, n, pex)` (`:368-382`): for `j = n-1 … 0`: `supps < smin` →
  `clear`; `supps >= pex` → `clear` + `isr_addpex(map[j])`; else set mask
  bit. Returns the mask.
- `project(fim, i, mask, dst)` (`:300-321`): moves list `i` into `dst`,
  removing bit `i` and masked-out bits, zeroing the source weights.
- `rec_pos(fim, n, mask)` (`:386-519`, used when `dir > 0`): items 0, 1, 2,
  3 have hand-written code reading `wgts[1..15]` directly; then items
  `4 … n-1` ascending: `isr_add`; `r <= 0` → `clear` and skip; a list with
  at most one pattern → all its bits are perfect extensions; else
  `project` in place, `count`, `filter` (pex threshold = item support),
  recurse; then `isr_report`, `isr_remove(1)`. Sets are reported after
  their extensions (post-order). Clears weights/lists of items 0–3 itself.
- `rec_neg(fim, n, mask)` (`:523-593`, `dir < 0`): items `n-1 … 2`
  descending with projection into `fim+1`, then items 1 and 0 by hand.
- `m16_mine(fim)` (`:613-638`): `ttw <= 0` → 0. `s = isr_supp(report)` (the
  current prefix's support). If `wgts[tor] >= s` (all weight on one
  pattern) → every bit of `tor` is a perfect extension, machine cleared,
  return 0. Else `count(hibit[tor]+1)`, `filter(…, s)`, `rec_pos`/`rec_neg`,
  reset `ttw`/`tor`, return the error status or the **mask of frequent,
  non-perfect items**, which fpgrowth uses to strip packed bits in later
  projections (`fpgrowth.c:696,629`).

### 5.4 Interaction with perfect extensions, closed/maximal, generators

- Perfect-extension pruning is **always** applied inside the machine
  (threshold = prefix support), independent of `-x`; the reporter expands
  perfect extensions, so the set of output item sets is the same, but the
  emission order is the machine's.
- Closed/maximal: fpgrowth uses `dir = -1` (`fpgrowth.c:765,1224`) →
  `rec_neg`, i.e. items in **descending code order**, which is what the
  repository (created with `dir = -1`) requires. This needs `map[]`
  increasing in the local index; true because `FPG_REORDER` is off for
  `c`/`m`. `isr_add` returns 0 when the repository proves a superset with
  equal support (`cm_add`), and the machine then clears the item's list.
- Generators (`dir = +1`): `isr_addpex` returns -1 (ignored) but `filter`
  still drops perfect-extension items, which is correct for generators.
- The machine never checks `zmax`; size limits are enforced by
  `isr_report`.

### 5.5 Invariants and edge cases

- Requires **positive weights**: the "first time" test in `m16_add`,
  `project` and `count` (`:252,316,344`) misfires for non-positive
  accumulated weights → duplicate list entries → double-counted supports
  and list overflow (**confirmed**, §11 T1).
- Requires `smin >= 1`: with `smin = 0` zero-support items pass `filter`
  and the one-pattern shortcut reads `*btas[i]` of an empty list
  (`:502-506,538-542`) (hypothesis; related cli-io B15 "support 0" sets).
- Weights, supports and `ttw` are `int`.

---

## 6. Closed/maximal repository (`clomax.c`)

### 6.1 Contract

`CLOMAX` (`clomax.h:120-125`) is a stack of prefix trees `trees[0..cnt]`
mirroring the reporter's current prefix `p1 … pcnt` (fpgrowth adds items in
strictly descending code order along a path, and processes sibling items
in descending order). `trees[k]` stores the already reported closed/maximal
sets that contain `p1 … pk`, with those items removed, each node carrying
the maximum support of the stored sets through it. Children lists are
sorted in direction `dir` (fpgrowth: always `-1`, descending;
`fpgrowth.c:2005` → `report.c:799`). Before testing a new item `i` at
level `k`, all top-level branches for items already processed at that level
(items `> i`) are merged away (`prune`), because a stored set containing
such an item is still a superset witness once that item is projected out.

Semantics used by `report.c` (details in `report.md` §4.1):

- `cm_add(cm, item, supp)` (`:459-475`, from `isr_add`): lazily create
  `trees[cnt]` by projecting `trees[cnt-1]` on its last item
  (`cmt_project`); `cmt_prune(trees[cnt], item)` (sets `item`, and `max` =
  support of the top-level node `item` or -1); if `max >= supp` return 0
  (a stored superset of prefix∪{item} has the same support → neither it
  nor any extension by smaller items can be closed/maximal; the caller
  skips the whole branch); else `cnt++`, return 1.
- `cm_supp(cm)` (`:450-455`): `cnt > 0` → `trees[cnt-1]->max`, else root
  support of `trees[0]` (max support of all stored sets).
- `cm_update(cm, items, k, supp)` (`:530-544`): `items` sorted descending,
  containing the prefix items (plus perfect extensions); for every level
  `i < cnt` skip to `trees[i]->item` and `cmt_add` the remainder. Called
  **before** the support/size/border filters of `isr_report`.
- `cm_remove(cm, n)` (`:501-506`): pops `n` levels and clears the trees
  above the new top (they become invalid, `item = -2`).
- `cm_tail(cm, items, n)` (`:510-526`, via `isr_tail` for maximal with
  head-union-tail pruning): project `trees[cnt-1]` keeping only the tail
  items (`cmt_xproj`/`xcopy`, merging other nodes into their parents), store
  it as `trees[cnt]`, return the support of the whole tail set in it
  (`> 0` → prune).

### 6.2 Data structures and helpers

- `CMNODE {item, supp, sibling, children}`; `CMTREE {mem, size, dir, item,
  max, root, keep[size]}`; one `MEMSYS` per tree (`ms_create(…, 4095)`).
- Sentinels: `item = -2`/`max = -2` invalid/empty, `item = -1` valid root
  tree, `max = -1` "no superset" (`clomax.c:61-62,226,274`).
- `cmt_add` (`:94-134`): walk/insert along the sorted child lists, raising
  `supp` to the max on the way (also the root's). `cmt_get` (`:138-154`).
- `merge_pos/neg` (`:158-188`): merge two sorted lists, recursively merging
  children of equal items and keeping the larger support. `prune_*`
  (`:192-212`) removes leading nodes up to (not including) `item`, merging
  their subtrees into the rest.
- `cmt_project` (`:264-294`): prune the source to `item`, copy the
  children of `item` into the destination (root support = node support),
  then prune `item` itself from the source.
- `cm_create(dir, n)` (`:418-434`): `trees` array has `n+1` slots
  (`calloc`), `trees[0]` gets a root with `max = 0`. `cm_delete` relies on
  a NULL terminator; with `n` items at most `trees[0..n-1]` are used.

### 6.3 Invariants / edge cases

- Item order must be descending (`dir = -1`); `keep[]` in a level-`k` tree
  has `n-k` entries and is indexed by item codes, which is in range only
  because with a descending prefix every item below level `k` is `< n-k`
  (inspection, `clomax.c:55-63,356-357`).
- Supports must be `>= 0` (`assert`s at `:101,536`, disabled); `-1`/`-2`
  are sentinels, so negative supports would be misread (hypothesis).
- Memory grows with the number of stored closed/maximal sets (all levels
  share the content through projections; `prune` merges reclaim nodes).
  This state cannot be streamed or bounded by the input size.
- Brute-force cross-check (**confirmed**, R12): `-tc`/`-tm`/`-ts` of all
  four variants with `-l0` and `-l16` equal a brute-force enumeration in
  984 runs on random data (unit weights).

---

## 7. Pattern spectrum (`patspec.c`, `-P`)

### 7.1 Creation and counting

- `isr_addpsp(report, NULL)` → `psp_create(zmin, zmax, smin, smax)` with
  the reporter's limits (`report.c:1894-1906`); `maxsize >= ITEM_MAX` →
  `ITEM_MAX-1`, `maxsupp >= RSUPP_MAX` → `RSUPP_MAX-1` (`patspec.c:141-160`).
  `smax` defaults to the total weight.
- `psp_incfrq(psp, size, supp, frq)` (`:313-338`): values outside
  `[minsize,maxsize] × [minsupp,maxsupp]` are ignored silently; otherwise
  `resize`, `cur` updates, `sigcnt++` when a counter becomes non-zero,
  `frqs[supp-min] += frq`, `row->sum += frq`, `total += frq`.
- What is counted: every item set emitted by `output()`/`fastout()` after
  border and evaluation filters, and, in count-only mode, binomial counts
  (`report.c:1346,1391,1500-1522`); see `report.md` and cli-io B6 for a
  reporter-side bug.

### 7.2 Storage (`resize`, `:220-280`)

- `rows[]` indexed directly by size (`0..max`; rows below `minsize` are
  allocated but never initialised or read); grows by 32 or ×1.5, capped at
  `maxsize`.
- Per row a **dense** `size_t` counter array over `[min, max]` support,
  grown in blocks of 32 around the observed supports (clamped to
  `[minsupp, maxsupp]`), size-0 row has a single counter. Memory is
  proportional to the **support range per size**: two size-1 sets with
  supports 1 and 2·10⁷ cost 162 MB RSS vs 1.9 MB without `-P`
  (**confirmed**, R10).

### 7.3 Output (`psp_report`, `:802-833`)

Rows by increasing size, columns by increasing support, only non-zero
counters: `size<fldsep>support<fldsep>count<recsep>` with the table
writer defaults `' '` and `'\n'` (`tabwrite.c:35-36`); `count` is printed
with `"%g"` of `scale·(double)frq`, i.e. **6 significant digits**:
1234567 → `1.23457e+06` (**confirmed**, R11). `psp_sigcnt` = number of
non-zero `(size, support)` cells.

Go: `strconv.FormatFloat(v, 'g', 6, 64)` reproduces C's `%g` (checked:
`1e+06`, `1.23457e+06`, `999999`, `2.5e+06`); `fmt.Sprintf("%g")` does
**not** (`1.234567e+06`).

### 7.4 Edge cases

- `psp_delete`/`psp_clear` loop `size < psp->max` and skip the last row
  (`:171,187`): leak / stale row; no effect in fpgrowth (inspection).
- `resize` computes `supp + BLKSIZE` in `int` (`:253`): UB for supports
  within 32 of `INT_MAX` (hypothesis).

---

## 8. Integer types and overflow

| Quantity | C type | Location | Behaviour |
| --- | --- | --- | --- |
| record weight | `(int)strtol` | `tract.c:711`, `tract.h:270` | silent `long→int` truncation (**confirmed**) |
| total weight `base->wgt`, `bag->wgt` | `int` | `tract.c:806,1818` | signed overflow = UB (**confirmed** UBSan), negative total, later SIGSEGV |
| `frq`, `max` | `int` | `tract.c:811-812` | UB (**confirmed**) |
| `xfq` increment `size·wgt` and sum | `int` | `tract.c:807,810` | UB (**confirmed** at 807); realistic for large/dense weighted data; changes `-q±2` order |
| `icnts`/`ifrqs`, merged weights | `int` | `tract.c:1764-1774,2347` | UB |
| `base->idx` (dedup marker) | `TID` | `tract.c:697` | UB after 2³¹ records; dedup breaks (hypothesis) |
| bag array growth | `TID` | `tract.c:1806-1811` | `n + n/2` overflows near 1.4·10⁹ transactions (hypothesis) |
| `extent` | `size_t` | | fine |
| fim16 `wgts`, `supps`, `ttw` | `int` | `fim16.c:249-252,341` | UB |
| clomax supports | `int` | | follow input supports |
| psp counters | `size_t`, printed as `double %g` | `patspec.c:333,822` | 6 significant digits on output |

A Go port using `int64` will agree with C wherever C stays in range; every
case where it does not is C UB and should be documented in
`docs/differences.md` (or rejected with an error).

---

## 9. Ordering and determinism guarantees

- No dependence on hash order, pointers, time or randomness.
- Item ids: first insertion (selection file, then data order).
- Item codes: function of (ids, `frq`/`xfq`, `-q`, REORDER, the removed
  set) through the exact `ptr_qsort` algorithm (§2.7).
- Items inside transactions: ascending codes. Transactions: `ta_cmp`
  order, duplicates merged (unique sequence). Insertion into the FP-tree:
  reverse of that sequence. Output does **not** depend on the insertion
  order or on merging (§4, empirical).
- fim16: deterministic, bit order = local index order defined by
  `m16_setmap`.
- clomax: deterministic given the add/report order.
- patspec: output sorted by (size, support).

---

## 10. What fpgrowth reads from these modules (consumer contract)

- `tbg_wgt`: perfect-extension threshold at the top level and base for
  `-s`/`-S`; `ib_getwgt`: empty-set support in the reporter.
- `tbg_ifrqs(…,0)`: header supports, after `zmin` filter, reduce and
  packing (slot 0 = packed items).
- `tbg_cnt`, `tbg_tract(j)`, `ta_items`, `ta_wgt`: the transaction stream
  (`*p < 0` = packed bits).
- `tbg_itemcnt`/`ib_cnt`: number of (recoded) items; `ib_name`: names for
  the reporter; `ib_getfrq`: read-time item frequency (only for `-eb`);
  `ib_maxfrq`: running maximum read-time frequency, used only as the
  pre-format range (`fpgrowth.c:2002-2003`); `tbg_max`: istree size cap
  for rules; `ib_getapp`: istree rule appearance.

---

## 11. Suspected bugs and undefined behaviour

| ID | Issue | Evidence | Status | Impact |
| --- | --- | --- | --- | --- |
| T1 | fim16 "first occurrence" test `(wgts[t] += w) <= w` assumes positive accumulated weights. With negative weights a pattern is appended to its list twice → `count()` double-counts it, and a list can exceed its `2^i` capacity. | `fim16.c:252` (also `:316,344`), capacities `:198-206` | **confirmed**: R4 gives `p0 (2)`, `p2 (2)` with `-l3` vs `p0 (1)` with `-l0`; R5 ASan `heap-buffer-overflow` WRITE at `fim16.c:253` | wrong supports, memory corruption with `-w` and negative weights (`-As`/`-Ad`/`-Ac`) |
| T2 | Items with frequency 0 survive recoding when the minimum support is 0, because only `frq < min` is removed; the miners assume every header item has nodes → NULL dereference. Zero-frequency items come from `-R` items absent in the data, zero-weight records, T3, and packed items `1..pack-1` whose counts are folded into slot 0. | `tract.c:881`, `tract.c:1772`; crash sites `fpgrowth.c:1162` (`rec_cmplx`), `fpgrowth.c:702` (`rec_smp16`) | **confirmed** (R2): `-s0` with `-R` / zero weight / dropped record → exit 139 for `-As/-Ac/-Ad`; `-As -l16 -s0` crashes even on plain data | SIGSEGV for `-s0` |
| T3 | A record ending with a field separator directly followed by EOF (no newline) is discarded; its new items remain in the item base with `frq = 0`. | `tract.c:704`, `tabread.c:224-227` | **confirmed** (R3): `a b\nc d,` → 1 transaction, 4 items | silent data loss (and T2 with `-s0`) |
| T4 | Unbounded recursion in the bucket sort: depth = length of a prefix shared by > 16 transactions; only when #items < #transactions. | `tract.c:2225-2229,2257,2261,2286` | **confirmed** (R9): 17 identical transactions of 100 000 items + 100 010 one-item transactions → exit 139; ASan `stack-overflow` in `sort tract.c:2218/2229`; merge-sort path (`k >= n`) fine | crash on long, repetitive transactions |
| T5 | Signed overflow of weight/frequency sums. | `tract.c:806,807,811,1818` | **confirmed** (R14, UBSan) | UB, negative totals, crash (cli-io B11) |
| T6 | Weights: base-0 `strtol` and `long→int` truncation. | `tract.h:270`, `tract.c:711` | **confirmed** | silent wrong weights |
| T7 | Negative weights break `supp(X) <= total weight`, which the top-level perfect-extension test relies on (`f[i] >= pex`): an item is reported with the total weight as its support. Also violates the disabled `assert(supp >= 0)` of `add_simple/add_smp16/add_cmplx/add_topdn` and `cmt_add`. | `fpgrowth.c:742-760`, `:401,543,831,1449`, `clomax.c:101` | **confirmed** (R4b): `a -2`/`b 3` → `b (1 100)` in all variants | wrong supports |
| T8 | Unstable `ptr_qsort` for item recoding; tie order depends on removed items. | `arrays.c:516-585`, `tract.c:889` | **confirmed** (R7) | not a bug; porting hazard |
| T9 | Pattern-spectrum memory ∝ support range. | `patspec.c:250-266` | **confirmed** (R10) | memory blow-up with large weights |
| T10 | Spectrum counts printed with `%g`. | `patspec.c:822` | **confirmed** (R11) | lossy counts ≥ 10⁶ |
| T11 | `psp_delete`/`psp_clear` skip the last row. | `patspec.c:171,187` | inspection | leak only |
| T12 | `tbg_reduce` returns 1 for an empty bag and never recomputes `max`. | `tract.c:2335,2356-2359` | inspection | none for output |
| T13 | `ib_maxfrq` is a running maximum (can exceed every final frequency with negative weights). | `tract.c:812` | inspection | pre-format range only |
| T14 | `TID` overflow of `base->idx` / bag growth beyond 2³¹ records. | `tract.c:697,1808` | hypothesis | UB on huge inputs |
| T15 | fim16 with `smin <= 0`: zero-support items kept; one-pattern shortcut dereferences an empty list. | `fim16.c:374,502-506,538-542` | hypothesis (cf. cli-io B15) | spurious sets |
| T16 | clomax `keep[]` indexed by item code but sized by tree size; safe only for `dir = -1`. `cmt_cnt` macro names a non-existent field. | `clomax.c:55-63,356-357`, `clomax.h:178` | inspection; unreachable | none |
| T17 | clomax uses -1/-2 sentinels in support fields; negative supports collide. | `clomax.c:61-62,226,274,473` | hypothesis | with negative weights only |
| T18 | `psp` `resize`: `supp + BLKSIZE` int overflow. | `patspec.c:253` | hypothesis | supports near `INT_MAX` |
| T19 | `ptr_mrgsort` passes `buf` instead of the allocated `b`. | `util/src/arrays.c:700` | inspection; unreachable (tract always passes a buffer) | none |
| T20 | `ib_xname` with `IB_INTNAMES` lacks `else`; `%d` result overwritten by `%p`. | `tract.c:463-465` | inspection; unreachable (string names) | none |
| T21 | Item names compared as C strings: embedded NUL truncates the name. | `tract.c:725,735`, `symtab.c:68-94` | **confirmed** (R13) | distinct byte strings merge |

---

## 12. C function → proposed Go component

Suggested packages under `internal/` (names are suggestions):

| C | Go component | Notes |
| --- | --- | --- |
| `ITEMBASE`, `ib_create`, `ib_cnt`, `ib_name`, `ib_getfrq`, `ib_getwgt`, `ib_maxfrq`, `ib_getapp` | `itembase.Base` (`names []string`, `index map[string]int32`, `frq/xfq []int64`, `app []uint8`, `total int64`) | key = bytes up to the first NUL if byte compatibility is wanted (T21) |
| `ib_readsel`, `ib_readapp`, `appcode` | `itembase.ReadSelection`, `itembase.ReadAppearance` | port `appcode` prefix rule and `ptr_bisect` |
| `ib_read`, `tbg_read`, `tbg_add` | `txn.Reader.Next() (items []int32, w int64, err)` on top of the tokenizer + a pass-1 `txn.Counter` | dedup via per-item "last seen" stamp (`idx`) or a small set; keep T3 behaviour or document |
| `ib_errmsg` | typed errors with file, record, field | message texts in cli-io.md |
| `ib_recode`, `idm_sort` | `itembase.Recode(min, max, dir) (oldToNew []int32, n int)` | comparator + literal `ptr_qsort` port (`internal/csort.PtrQsort`) over **all** items |
| `ptr_qsort`, `ptr_qrec` | `internal/csort` | must be bit-exact; unit-test against the R7 harness outputs |
| `recode`, `tbg_filter`, `tbg_itsort` | `txn.Mapper.Apply(items) []int32` (map, drop, sort, zmin → empty) | per transaction, no bag |
| `tbg_sort`, `tbg_reduce`, `sort`, `pksort`, `ta_cmp` | optional `txn.Dedup` (hash of the item slice) | not needed for output (§4) |
| `tbg_pack`, `ta_pack` | `txn.Packed{Bits uint16; Rest []int32}` | explicit representation, no sentinel tricks |
| `tbg_ifrqs`, `tbg_count` | header-frequency pass (`[]int64`, slot 0 = packed) | before tree insertion |
| `tbg_wgt`, `tbg_cnt`, `tbg_max` | fields of the pass-1 summary | |
| `m16_*`, `count`, `project`, `filter`, `rec_pos`, `rec_neg`, `hibit`, `prjms` | `internal/fim16.Machine` | literal port incl. incremental clearing and the item 0–3 special cases; weights `int64` |
| `cm_*`, `cmt_*`, `merge_*`, `prune_*`, `copy`, `xcopy_*` | `internal/clomax.Filter`, `clomax.Tree` | arena/slab allocation per tree instead of `MEMSYS`; dir fixed to -1 |
| `psp_create`, `psp_incfrq`, `psp_report`, `psp_sigcnt` | `internal/patspec.Spectrum` | sparse rows (map or sorted slice per size); output sorted; `FormatFloat(…,'g',6,64)` |

---

## 13. Porting risks (highest first)

1. **Item code assignment.** Exact `ptr_qsort` port, full item array
   (including items to be removed) in first-insertion order, comparator
   with "`APP_NONE` last" rule and no secondary key; `-q` mapping incl. the
   REORDER clamp; `xfq` with read-time sizes (incl. later-infrequent and
   appearance-0 items, excl. unselected items). Any deviation silently
   changes output order.
2. **Total weight and transaction count semantics** (§3.10): empty
   records, unselected-only records, `zmin`-emptied, zero/negative weights
   all count; comments do not; T3 records are lost.
3. **Streaming pipeline correctness**: header frequencies after `zmin`
   filtering and packing must be known before insertion (extra pass or
   buffering); stdin must be spooled; pass 1 must preserve first-seen order.
4. **fim16 fidelity**: its output order differs from plain recursion, it
   always uses perfect extensions, and its incremental clearing is easy to
   get subtly wrong. Port literally and differential-test with `-l0` vs
   `-l16`.
5. **Integer width**: C is 32-bit with UB on overflow; choose `int64` and
   document/reject overflow cases (T5, xfq).
6. **Weight parsing quirks** (base 0, brackets, truncation, weight-only
   records, `-w` + empty line = error) — see cli-io §4.5.
7. **Negative weights and `-s0`**: upstream miscounts (T1, T7), corrupts
   memory (T1) or crashes (T2). Decide a policy (reject negative weights
   and/or treat minimum support 0 as 1, or drop zero-frequency items) and
   record it in `docs/differences.md`; byte compatibility is impossible
   there.
8. **Pattern spectrum**: sparse storage (T9), sort order, `%g` formatting.
9. **Closed/maximal repository**: descending order invariant, update
   before filters, memory O(output).
10. **Packed transactions**: `size` is stale after packing in C; packed
    item frequencies live in slot 0 — replicate the observable effects, not
    the representation.
11. **Stack depth**: C recursion in `sort()` (T4) and in the miners;
    Go goroutine stacks grow, but avoid copying the C bucket sort at all.

---

## Appendix A: Reproducers

`$FPG` = reference binary (e.g. built by `reference/build.sh`);
`$ASAN` = sanitizer build:

```sh
cp -R reference/upstream/fpgrowth-6.21/{util,math,tract,apriori,fpgrowth} "$TMP/asan/"
cd "$TMP/asan/fpgrowth/src" && make fpgrowth CC=clang LD=clang \
  CFLAGS='-std=c99 -c -g -O1 -DNDEBUG -fsanitize=address,undefined -fno-omit-frame-pointer' \
  LDFLAGS='-fsanitize=address,undefined'   # command-line vars reach the sub-makes
```

All runs below were made on macOS arm64 (Apple clang 21) against the
6.21 sources.

- **R1** dedup / empty records: `printf 'a a b\n\nb c\n   \nc\n' > d; $FPG -s0 -m0 -v' (%a %S)' d -`
  → `5 transaction(s)`, `[4/5 transaction(s)]` after reduce, `a (1 20)`,
  ` (5 100)`.
- **R2** zero-frequency items with `-s0`:
  `printf 'a b z\n' > sel; printf 'a b\na\n' > d; $FPG -s0 -R sel d -` → exit
  139 (`-s-1` → exit 0); `printf 'a b 1\nc 0\n' > w; $FPG -s0 -w w -` → exit
  139 for `-A{s,c,d}`, 0 for `-At`; `printf 'a b\nc d\na c\n' > s; $FPG -As -l16 -s0 s -`
  → exit 139 (`-l0` fine). ASan: NULL member access at
  `fpgrowth.c:1162` (`rec_cmplx`) / `fpgrowth.c:702` (`rec_smp16`).
- **R3** dropped last record: `printf 'a b\nc d,' > e; $FPG -s-1 e -` →
  `[4 item(s), 1 transaction(s)]`; `printf 'a b\nc d'` → 2 transactions.
- **R4** fim16 double count: `printf 'p0 p1 p2 y -1\np0 p1 p2 2\ny 5\nz 10\n' > m;`
  `$FPG -As -q-1 -l0 -s-1 -w -v' (%a)' m -` → `p0 (1)`, `p2 (1)`;
  `-l3` → `p0 (2)`, `p1 (2)`, `p2 (2)`, `p0 p2 (2)`. A random differential
  test (`-l0` vs `-l16`, weights in [-3,4]) differs in 1053/1800 runs;
  with weights in [1,4]: 0/1800.
- **R4b** perfect extension with negative weights: `printf 'a -2\nb 3\n' > n; $FPG -s-1 -m0 -w -v' (%a %S)' n -`
  → `b (1 100)` (true support 3) for `-As/-Ac/-Ad/-At`.
- **R5** fim16 heap overflow: data = for every subset `S` of
  `{P0..P9}` a line `S P10 1` (1024 lines), plus `P10 u2 -1`, `P10 u1 2`,
  `u1 2000`, `u2 3000`; `$ASAN -As -q-1 -l11 -s-1 -w data /dev/null` →
  `heap-buffer-overflow … WRITE of size 2 … m16_add fim16.c:253`, region
  2048 bytes (list 10). The plain binary completes silently.
- **R6** `-q` modes: `printf 'a\na\na b\nb c d e f\nb c d e g\n' > x;`
  `for t in s c; for A in c s; for q in 1 2 -1 -2 0: $FPG -t$t -A$A -q$q -s-2 x -`
  → `-ts -Ac`: q2≡q1, q-2≡q-1; `-ts -As` and `-tc`: q2≠q1, q-2≠q-1.
- **R7** `ptr_qsort` ties: harness calling `ptr_qsort(p, n, +1, cmp, NULL)`
  on records `{id, app, frq}` with the `descmp` logic, linked with
  `util/src/arrays.c` → table in §2.7.
- **R8** insertion-order invariance: patch `fpgrowth.c:1952-1954` to switch
  on an environment variable between (0) original, (1) Fisher–Yates
  shuffle (LCG, fixed seed) without reduce, (2) sort without reduce, (3)
  sort + reduce + `ptr_reverse`; compare outputs with `cmp` → 0 differences
  in 5232 runs (random data: 30–40 items, 200–300 transactions; weighted
  variant: 20 items, 150 transactions, weights 1–5).
- **R9** stack overflow: 17 lines each containing items `i0 … i99999`, then
  100 010 lines `x`; `$FPG -tm -s-17 data /dev/null` → exit 139 (50 000
  items: exit 0; 17 lines of 300 000 items without the `x` lines, i.e.
  the merge-sort path: exit 0). ASan:
  `stack-overflow` in `sort tract.c:2218` ← `sort tract.c:2229` (repeated).
- **R10** spectrum memory: `printf 'a 20000000\nb 1\n' > p;`
  `/usr/bin/time -l $FPG -N -w -s-1 -Pspec p /dev/null` → 161 906 688 bytes
  max RSS; without `-P` 1 900 544. `spec` = `1 1 1` / `1 20000000 1`.
- **R11** `%g` counts: 1 234 567 lines `i<k>` plus 1 000 000 lines `x y`;
  `$FPG -s-1 -n2 -Pspec data /dev/null` → `1 1 1.23457e+06`,
  `1 1000000 2`, `2 1000000 1`.
- **R12** brute force: random data (9 items, 40 transactions), `-s-3`,
  targets `c`, `m`, `s`, variants `s c d t`, `-l0`/`-l16`, outputs
  normalised (items sorted per line, lines sorted) vs Python enumeration →
  0 mismatches in 984 runs.
- **R13** NUL in names: `printf 'a\0b c\na\0d\n' > z; $FPG -s-1 -v' (%a)' z -`
  → 2 items, `a (2)`.
- **R14** overflow: `printf 'a 2000000000\na b 2000000000\n' > o; $ASAN -w -s50 -m0 o /dev/null`
  → `tract.c:806:15: runtime error: signed integer overflow: 2000000000 + 2000000000`,
  likewise `tract.c:807`, `:811`, `:1818`; plain binary prints
  `2/-294967296 transaction(s)` and exits 139.
