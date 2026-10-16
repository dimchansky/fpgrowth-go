# Upstream audit: item set reporter (`tract/src/report.[ch]`) as used by `fpgrowth`

Source: `reference/upstream/fpgrowth-6.21/` (pristine). All paths below are
relative to that directory; `tract/src/` is the default directory for bare
`report.c`, `report.h`, `clomax.c`, `patspec.c`, `tract.c`, `fim16.c`.
Line numbers refer to the pristine 6.21 sources.

Reference binary used for experiments: the C build in the session scratchpad
(`cbuild/fpgrowth/src/fpgrowth`, built with the upstream makefiles, Apple
clang on arm64). Every claim marked **confirmed** was reproduced with that
binary or with a small C harness linked against the same `repcm.o`
(see Appendix A).

---

## 0. Build configuration actually used by `fpgrowth`

| Item | Value | Evidence |
| --- | --- | --- |
| Object linked | `tract/src/repcm.o` (not `report.o`) | `fpgrowth/src/makefile` `OBJS`, rule `$(TRACTDIR)/repcm.o` |
| Defines for `repcm.o` | `-DISR_PATSPEC -DISR_CLOMAX` + `CFLAGS` = `-DNDEBUG -O3 -funroll-loops` | `tract/src/makefile:363-366`, `:30` |
| Also forced in `fpgrowth.c` | `ISR_PATSPEC`, `ISR_CLOMAX` | `fpgrowth/src/fpgrowth.c:95-99` |
| Not defined | `USE_ZLIB` (no `-z`, no `ISR_ZLIB`), `ISR_NONAMES` (names are used), `ISR_NOSTDFILE`, `STORAGE`, `RSUPP` | makefiles |
| `RSUPP` | `SUPP` = `TID` = `int` (32-bit) → `RSUPP_MAX = INT_MAX`, `RSUPP_FMT = "d"` | `report.h:66-71`, `tract.h:104-108,258-267` |
| `ITEM` | `IDENT` = `int`; `ITEM_MIN/MAX = INT_MIN/INT_MAX` | `tract.h:100,125,128`, `util/src/symtab.h:35` |
| `ISCNT` (set counters) | `size_t` (64-bit on LP64), printed with `"zu"` | `report.h:157-166`, `tract.h:413` |
| Asserts | disabled (`NDEBUG`) — every `assert()` in report.c is a no-op | |
| Debug flushes | disabled: `isr_flush()` after every set only `#ifndef NDEBUG` | `report.c:197-199,1545-1547,1563-1565` |

Consequence for the port: integer supports are 32-bit signed, set counters are
64-bit unsigned (wrap-around on overflow), all `RSUPP==double` branches in
`isr_sinfo`/`isr_rinfo`/`isr_xinfo` are dead (the `#else` integer branches are
live: `report.c:1953-1955,2009-2012`).

### Which functions the fpgrowth program reaches

| Reached (directly or via `istree.c` / `fim16.c`) | Not reached by the fpgrowth CLI |
| --- | --- |
| `isr_createx` (via macro `isr_create`), `isr_delete`, `isr_settarg`, `isr_setsupp`, `isr_setsize`, `isr_setbdr`, `isr_seteval`, `isr_prefmt`, `isr_setfmtx` (via `isr_setfmt`), `isr_addpsp`, `isr_getpsp` (macro), `isr_open`, `isr_close`, `isr_setup`, `isr_add`, `isr_addpex`, `isr_addpexpk`, `isr_remove`, `isr_xable`/`isr_uses`/`isr_tail`/`isr_repcnt`/`isr_name` (macros), `isr_report`, `isr_reportv`, `isr_reprule`, `isr_logrto`, `isr_prstats`, `isr_sinfo`, `isr_rinfo`, `isr_intout`, `isr_numout`, `mantout`, `fastchk`, `fastout`, `output`, `report`, `is_isgen`/`is_hash`/`is_cmp` | `isr_addnc`, `isr_addwgt`, `isr_addncw`, `isr_reportx`, `isr_reporto`, all `isr_tid*` (transaction-id output), `isr_iset`, `isr_isetx`, `isr_rule`, `isr_seqrule`, `isr_sxrule`, `isr_extrule`, `isr_xinfo`, `isr_wgtout`, `isr_getinfo`, `isr_reset` (only `fim16.c` test main), `isr_clrbdr`, `isr_setrepo`, `isr_setrule`, `isr_rempsp`, `isr_lrsize`, `isr_sizewgt`, `isr_wgtsize`, `isr_wgtsupp`, `uint128tostr` |

`isr_iset`/`isr_rule` are only called by `ist_report()` when a size order is
requested (`apriori/src/istree.c:2114-2146`); `fpg_mine()` always passes
`order = 0` (`fpgrowth/src/fpgrowth.c:2526,2071`), so they are unreachable.
Verified with `code-review-graph query callers_of <fn>`.

---

## 1. Data model (`ISREPORT`, `report.h:213-298`)

Fields that matter for fpgrowth (others are zlib/tid/sequence support):

| Field | Meaning |
| --- | --- |
| `base` | item base; names and item frequencies are read **once** in `isr_createx` |
| `target` | one of `ISR_ALL(0)`, `ISR_CLOSED(1)`, `ISR_MAXIMAL(2)`, `ISR_GENERAS(4)`, `ISR_RULES(8)` (`report.h:186-193`) |
| `mode` | `ISR_NOFILTER(0x10)`, `ISR_NOEXPAND(0x20)`, `ISR_SORT(0x40)` (`report.h:196-198`) |
| `zmin, zmax` | reported size range; defaults `1, ITEM_MAX` (`report.c:639-640`) |
| `xmax` | size limit for `isr_xable()`; `zmax+1` for closed/maximal with finite `zmax`, else `zmax` (`report.c:1117-1118`) |
| `smin, smax` | reported support range; defaults `1, RSUPP_MAX` (`report.c:643-644`) |
| `border[0..bdrcnt-1]` | per-size minimum support (`-F`); `NULL` if none |
| `cnt` | number of items in the current set |
| `items[0..cnt-1]` | current set, in insertion order |
| `pexs[0..pexcnt-1]` | stack of perfect extensions; grows **downward** and sits immediately below `items` in one allocation, so `pexs[0..n-1]` followed by `items[0..cnt-1]` is contiguous (`report.c:691,699-700`); `pexs[0]` is the newest |
| `pxpp[]` | packed dual-use array (`report.c:698`): bit `ITEM_MIN` of `pxpp[i]` = "item i is in use" (as set item or perfect extension); the low bits of `pxpp[k]` = number of perfect extensions pushed while the set had `k` items |
| `supps[k]` | support of the prefix of length `k`; `supps[0] = ib_getwgt(base)` = total transaction weight captured at creation (`report.c:702`) |
| `wgts[k]` | weights; calloc'ed so all 0, except `wgts[0] = total weight` (`report.c:694,702`); fpgrowth never sets item set weights |
| `ldps[i]` | `log(frq_i)/LN_2 - log(total)/LN_2` (0 if a frequency is 0) (`report.c:703-708`) |
| `clomax` | closed/maximal repository (`clomax.c`), created by `isr_settarg` |
| `gentab` | generator hash table (`symtab`), created by `isr_settarg` |
| `sto` | `0` for maximal, `RSUPP_MAX` otherwise (`report.c:802`) |
| `dir` | item order of the repository / generator sort, `-1` from fpgrowth (`report.c:803`) |
| `iset` | scratch buffer (`cnt+2` ITEMs) for generator keys and sorted closed sets |
| `evalfn, evaldir, evalthh, eval` | optional evaluation (`-eb`) |
| `hdr, sep, imp, info` | output strings |
| `inames[i]` | output name of item `i`, fixed at creation |
| `out, pos[], pfx` | output line cache: `out` holds `hdr` + formatted items, `pos[k]` = end of the text of the first `k` items, `pfx` = number of items whose text is valid |
| `stats[k], repcnt` | number of reported sets per size and in total |
| `psp` | optional pattern spectrum |
| `ints, imin, imax` | pre-formatted decimal strings (pure speed optimisation, §5.5) |
| `file, name, buf/next/end` | output `FILE*` and the 64 KiB write buffer |
| `fast` | output mode chosen by `fastchk()`: `-1` count only, `0` normal, `+1` fast (§5.7) |
| `fosize, foinfo` | pre-formatted `" (<supp>)\n"` for fast mode |

Allocation sizes (`report.c:632-695`): with `n = ib_cnt(base)` and
`k = max(max, n)` (`k = n` for fpgrowth because `isr_create(b)` =
`isr_createx(b, 0)`, `report.h:449`): `pxpp` `3k+2`, `iset` `k+1`, `supps`
`k+1`, `wgts` `k+n+1` (shared with `ldps`), `stats` `k+1`. Any set ever reported
has at most `n` distinct items, so all indices stay in range.

---

## 2. Configuration API and the fpgrowth call sequence

`fpgrowth` main (`fpgrowth/src/fpgrowth.c`) calls, in this order:

1. `fpg_data()` — recodes items (`tbg_recode`, `:1930-1941`) **before** the
   reporter exists, so reporter item ids are the recoded ids.
2. `isr_create(ibase)` (`:2512`).
3. `fpg_report()` (`:1970-2008`):
   `isr_setsupp(rep, fpg->supp, floor(smax))`, `isr_setsize(rep, zmin, zmax)`,
   `isr_seteval(rep, isr_logrto, NULL, +1, thresh/100)` only for `-eb`,
   `isr_prefmt(rep, fpg->supp, maxfrq or -1)` (`-N` → `-1`),
   `isr_settarg(rep, target, mrep, -1)`.
   `mrep` = `ISR_SORT` for generators with item reordering, `ISR_NOFILTER` for
   rules or rule-based measures (`:1981-1992`).
4. `setbdr()` → `isr_setbdr(rep, i+zmin, s_i)` for every `-F` value (`:2178-2193`).
5. `isr_addpsp(rep, NULL)` if `-P` (`:2518`).
6. `isr_setfmt(rep, scan, hdr, sep, imp, info)` (`:2520`).
7. `isr_open(rep, NULL, fn_out)` (`:2522`).
8. `isr_setup(rep)` (`:2524`).
9. `fpg_mine()` → mining calls (§3) (`:2526`).
10. `isr_prstats(rep, stdout, 0)` if `-Z` (`:2528-2529`).
11. `isr_close(rep)`; nonzero → `E_FWRITE` (`:2530-2531`).

### Function semantics

- **`isr_createx(base, max)`** (`report.c:620-730`): defaults as in §1;
  `hdr=""`, `sep=" "`, `imp=" <- "`, `iwf=":%w"`, `info=" (%a)"`, `fast=-1`,
  `scan=0`. Item names: `inames[i] = ib_xname(base, i)`; the scanable
  formatting branch (`scn_fmtlen`/`scn_format`) runs only `if (rep->scan)`,
  and `scan` was just set to 0 (`report.c:661,713`) — dead code (bug B2).
  `nsum`/`nmax` = sum/max of name lengths (used to size `out`).
  `malloc` of `rep` itself is not checked (`report.c:634`).
- **`isr_settarg(rep, target, mode, dir)`** (`report.c:777-808`): normalises
  `target` to a single flag with priority RULES > GENERAS > MAXIMAL > CLOSED >
  ALL; for CLOSED/MAXIMAL forces `mode |= ISR_NOEXPAND`. Deletes old filters.
  If target is closed/maximal/generator **and** not `ISR_NOFILTER`: generators
  → `st_create(1048575, …, is_hash, is_cmp, …)`; closed/maximal →
  `cm_create(dir, ib_cnt(base))`; `sto = maximal ? 0 : RSUPP_MAX`;
  `rep->dir = dir<0 ? -1 : +1`. Calls `fastchk`. Returns `E_NOMEM` or 0.
- **`isr_setsupp(rep, smin, smax)`** (`report.c:812-818`): stores; no
  `fastchk`; the assert `0 <= smin <= smax` is disabled.
- **`isr_setsize(rep, zmin, zmax)`** (`report.c:822-829`): stores; `fastchk`.
- **`isr_setbdr(rep, size, supp)`** (`report.c:833-853`): grows `border` (block
  32, then x1.5), new entries 0, `bdrcnt = max(bdrcnt, size+1)`,
  `border[size] = supp`; `fastchk`. Returns `supp` or `-1` (OOM).
- **`isr_seteval(rep, fn, data, dir, thresh)`** (`report.c:900-909`):
  `evaldir = dir>=0 ? +1 : -1`, `evalthh = evaldir*thresh`; `fastchk`.
- **`isr_prefmt(rep, min, max)`** (`report.c:932-970`): pre-formats decimal
  strings for `[max(min,0), max]`; `max < 0` disables. Output-neutral (§5.5).
- **`isr_setfmtx(rep, scan, hdr, sep, imp, info, iwf)`** (`report.c:868-896`):
  copies the five strings (NULL → `""`) into one buffer `rep->str` and sets
  `rep->sep`, `imp`, `info`, `iwf` to the copies — **but never assigns
  `rep->hdr`** (the header copy is only reachable as `rep->str`,
  `report.c:884`), so `rep->hdr` stays `""` (bug B1). Sets `rep->scan`, which
  is never read again (bug B2). `fastchk`.
- **`isr_addpsp(rep, NULL)`** (`report.c:1896-1906`): creates
  `psp_create(zmin, zmax, smin, smax)` with the limits current at that moment
  (fpgrowth calls it after `fpg_report`, so the limits are final).
- **`isr_open(rep, NULL, name)`** (`report.c:974-1008`): allocates the 64 KiB
  write buffer; `name == NULL` → no file, name `"<null>"`; `name == ""` →
  `stdout`, name `"<stdout>"`; otherwise `fopen(name, "w")` (failure →
  `E_FOPEN`). `fastchk`. (The fpgrowth argument parser turns a lone `-` into
  `""` because it increments `s` while testing `*++s`, `fpgrowth.c:2365`; so
  `fpgrowth in -` writes to stdout — **confirmed**.)
- **`isr_setup(rep)`** (`report.c:1101-1121`): allocates `out` of size
  `nsum + strlen(hdr) + (size-1)*strlen(sep) + 1`, copies `hdr` into it,
  `pos[0] = out + strlen(hdr)`, `pfx = cnt = 0`, computes `xmax`, `fastchk`.
- **`isr_close(rep)`** (`report.c:1012-1028`): no file → 0. Otherwise flush,
  `r = ferror(file)`, then `fflush` for stdout/stderr or `fclose` otherwise,
  ORed into `r`; `file = NULL`; `fastchk`. Returns nonzero on any error.
- **`isr_delete(rep, delis)`** (`report.c:734-773`): frees everything, closes
  files; returns the close result. The `inames` loop relies on the NULL
  sentinel `inames[n]` (`report.c:709,750-755`).

---

## 3. Incremental item set interface used during mining

### 3.1 Caller protocol (fpgrowth)

Every fpgrowth variant uses the same pattern (e.g. `rec_simple`,
`fpgrowth/src/fpgrowth.c:513-528`; also `:698-715`, `:1159-1176`,
`:1293-1346`, `:1572-1605`, and `fim16.c:397-590`):

```c
r = isr_add(rep, item, supp);      /* -1 error, 0 skip, 1 added   */
if (r < 0) break; if (r <= 0) continue;
/* isr_addpex(rep, x) for every perfect extension of prefix+item */
/* ... recurse into the projection (if isr_xable(rep, 2)) ...    */
r = isr_report(rep);               /* report prefix+item (+pexs) */
isr_remove(rep, 1);
```

Perfect extensions found while projecting the *current* level are pushed
after `isr_add` and therefore belong to that level. Supersets are reported
before the set itself (post-order). After the whole recursion the driver calls
`isr_report()` once more with `cnt == 0` to report the empty set together with
the top-level perfect extensions (items whose support equals the total weight,
pushed with `isr_addpex` before any `isr_add`, e.g. `fpgrowth.c:756-764,793`).
Top-level perfect extensions are counted in `pxpp[0]` and are never popped.

### 3.2 `isr_add(rep, item, supp)` (`report.c:1125-1145`)

1. Closed/maximal filter present: `r = cm_add(clomax, item, supp)`; return `r`
   if `r <= 0`. `cm_add` returns 0 when an already stored superset of
   prefix+item has support `>= supp` (prefix+item is not closed and its
   subtree holds nothing new; `clomax.c:459-476`), `-1` on OOM.
2. Else generator table present: `r = is_isgen(rep, item, supp)` (§4.9);
   return `r` if `r <= 0`.
3. Mark item in use (`pxpp[item] |= ITEM_MIN`), `items[cnt] = item`,
   `supps[++cnt] = supp`, clear the pex counter of the new level
   (`pxpp[cnt] &= ITEM_MIN`). Return 1.

Return 0 means **the item was not added** (`cnt` unchanged); the caller must
skip both recursion and `isr_report`/`isr_remove`. No duplicate check (the
`isr_uses` assert is commented out, `report.c:1129`).

### 3.3 `isr_addpex(rep, item)` (`report.c:1223-1234`)

Returns `-1` and does nothing if `item` is already in use **or the target is
generators** (perfect extensions are meaningless for generators; fpgrowth
forces `FPG_PERFECT` for generators so such items are simply dropped from the
projection, `fpgrowth.c:1828-1830`). Otherwise marks the item, pushes it
(`*--pexs = item`) and increments `pxpp[cnt]`. Callers ignore the result.

`isr_addpexpk(rep, bits)` (`report.c:1238-1253`) does the same for every set
bit `i` of `bits & ~ITEM_MIN` (packed 16-items-machine items have ids 0..15),
lowest bit first.

### 3.4 `isr_remove(rep, n)` (`report.c:1257-1275`)

If a closed/maximal filter exists, `cm_remove(clomax, n)` first. Then `n`
times: pop and un-mark the `pxpp[cnt] & ~ITEM_MIN` perfect extensions of the
top level, then pop and un-mark `items[--cnt]`. Finally `pfx = min(pfx, cnt)`.
(No call into the generator table: generators are never removed.)

### 3.5 Queries

- `isr_xable(r, n)` = `cnt + n <= xmax` (`report.h:478`). fpgrowth asks
  `isr_xable(rep, 2)` before building a projection (one more item after the one
  being added) and `isr_xable(rep, 1)` in the single-tree variant.
- `isr_uses(r, i)` = `pxpp[i] < 0` (`report.h:477`).
- `isr_pexcnt(r)` = `items - pexs` = total number of pushed perfect extensions
  over **all** levels (`report.h:490`).
- `isr_tail(r, items, n)` = `cm_tail(clomax, items, n) > 0 ? 1 : 0`
  (`report.h:507`): head-union-tail pruning for maximal sets; returns 1 if
  prefix ∪ tail is already covered by a stored set. A `-1` (OOM) from
  `cm_tail` is mapped to 0 and silently ignored (bug B12).

---

## 4. `isr_report()` — what is reported, filtered and counted

`isr_report(rep)` (`report.c:1454-1567`), with `n = isr_pexcnt`, `s = supps[cnt]`:

### 4.1 Closed/maximal repository (`report.c:1469-1486`)

Only if `clomax` exists (closed/maximal target and not `ISR_NOFILTER`):

1. `r = cm_supp(clomax)` — largest support of an already stored superset of
   the current set (negative when none; for `cnt == 0` it is the root support
   of the repository, initially 0; `clomax.c:450-455`).
2. `r >= s` → return 0 (not closed).
3. `r >= sto` → return 0. For maximal (`sto = 0`) this rejects the set if
   *any* superset is stored, **before** the repository update; for closed
   (`sto = INT_MAX`) it never triggers. Consequence: with `-tm -m0` the empty
   set is never reported — **confirmed** (closed `-tc -m0` does report ` (10)`
   on `test1.tab`).
4. Build the full set `items ∪ pexs` (`k = cnt + n` items). If `n > 0` it is
   copied from the contiguous `pexs..items` region into `iset` and sorted with
   `ia_qsort(…, dir)` (descending, `dir = -1`); with `n == 0` `items` is used
   as is (fpgrowth adds items in descending code order for closed/maximal,
   `fpgrowth.c:765`).
5. `cm_update(clomax, items, k, s)`; `< 0` → return -1.
6. `(target & ISR_MAXIMAL) && r >= 0` → return 0 — dead code under fpgrowth,
   step 3 already returned.

Important ordering: the repository is updated **before** the support, size,
border and evaluation filters. Closed/maximal is decided on the frequent-set
lattice explored by the miner (which explores up to `zmax+1` items via
`xmax`); the remaining filters are applied afterwards to the closed/maximal
sets.

### 4.2 Support and size gates (`report.c:1487-1491`)

Return 0 if `s < smin || s > smax`, or `cnt > zmax`, or `cnt + n < zmin`
(no expansion can reach the minimum size).

### 4.3 Count-only path (`fast < 0`, `report.c:1492-1528`)

Taken when there is no output file and no border, no evaluation function, no
report callback, no tid file (§5.7). Nothing is enumerated:

- `ISR_NOEXPAND` (closed/maximal): count one set of size `z = cnt + n`:
  `stats[z]++`, `repcnt++`, `psp_incfrq(psp, z, s, 1)`. **No `z <= zmax`
  check** (bug B4).
- Otherwise: if `cnt >= zmin` count the set itself; then for `k = 1..n` while
  `++z <= zmax`: `c = c*(n-k+1)/k` (i.e. `C(n,k)`, computed incrementally in
  `size_t`), and if `z >= zmin` add `c` to `stats[z]`, `repcnt` and
  `psp_incfrq(psp, z, s, c)`. The intermediate product overflows for
  `n >= 63` (bug B5).

`psp_incfrq` errors are checked here (return -1).

### 4.4 No-expansion path (`ISR_NOEXPAND`, closed/maximal, `report.c:1535-1549`)

`k = cnt + n`; `k > zmax` → return 0. Set `supps[k] = supps[cnt]`,
`wgts[k] = wgts[cnt]`; append the perfect extensions to `items` oldest first
(`items[cnt++] = pexs[k]` for `k = n-1..0`); call `fastout(rep, 0)` or
`output(rep)`; `cnt -= n`. **`pfx` is not reduced**, so the line cache is
stale until the next `isr_remove()` (harmless in fpgrowth, which always calls
`isr_remove` next — latent hazard B13). Pattern-spectrum errors are not checked
on this path (B11).

### 4.5 Fast output path (`fast > 0`, `report.c:1532-1534,1550-1551`)

`foinfo = sprintf(" (%d)\n", supps[cnt])`, then `fastout(rep, n)`
(`report.c:1336-1364`): count (`stats[cnt]++`, `repcnt++`, `psp`), extend the
cached line text up to `cnt` items, recursively expand perfect extensions
(same order as §4.7), then write `out[0..pos]` + `foinfo`. `fastout` performs
**no size check**: every node of the hypercube is emitted, including the set
with `cnt == 0` (bug B3).

### 4.6 Normal path (`fast == 0`, `report.c:1552-1558`)

Set `supps[cnt+1..cnt+n] = s` and `wgts[cnt+1..cnt+n] = wgts[cnt]` (all
expanded sets share the support), then `report(rep, n)`
(`report.c:1437-1450`):

```c
while (n > 0) {                       /* oldest remaining pex first */
  items[cnt++] = pexs[--n];
  if (cnt + n >= zmin && cnt <= zmax) report(rep, n);
  if (--cnt < pfx) pfx = cnt;
}
if (cnt >= zmin) output(rep);         /* post-order */
```

Finally `psp_error(psp)` → return -1 (`report.c:1559-1562`).

### 4.7 Expansion order (confirmed)

For a set `P` with perfect extensions pushed in the order `p1, p2, …, pn`
(`p1` oldest), the reported sets are, depth first, supersets before subsets,
the oldest remaining extension chosen first at every level. For `n = 2`:
`P p1 p2`, `P p1`, `P p2`, `P`. For `n = 3`: `P p1 p2 p3`, `P p1 p2`,
`P p1 p3`, `P p1`, `P p2 p3`, `P p2`, `P p3`, `P`. Items are printed in
`items[]` order: prefix in insertion order, then the extensions in the order
they were appended. Example (`pexo.tab`, `-s-2`): `b c x`, `b c`, `b x`, `b`;
`a b c x`, `a b c`, `a b x`, `a b`, `a c x`, `a c`, `a x`, `a`. With `-m2 -n3`
exactly the members of that sequence with 2..3 items remain, in the same
relative order. Fast and normal paths produce the identical sequence (apart
from B3).

### 4.8 `output(rep)` (`report.c:1368-1433`) — filters, statistics, text

In this order, for every expanded set (size `cnt`, support `supps[cnt]`):

1. **Border** (`-F`): if `border && cnt < bdrcnt && supps[cnt] < border[cnt]`
   → skip. Sizes `>= bdrcnt` are **not** filtered; unset sizes have border 0
   (**confirmed**: `-F40:50` keeps the 3-sets with support 2). (Contrast
   `isr_iset`, which rejects `n >= bdrcnt`, `report.c:1660-1662` —
   inconsistent but unreachable, B15.)
2. **Evaluation**: if `evalfn`: `eval = evalfn(rep, evaldat)`; skip if
   `evaldir*eval < evalthh`. Evaluated per expanded set. A NaN would pass.
3. Count: `stats[cnt]++`, `repcnt++`, `psp_incfrq(psp, cnt, supp, 1)`
   (return value ignored; error latched in `psp`).
4. `repofn` callback (unused by fpgrowth).
5. No file → return (border/eval count-only mode ends here).
6. Text: extend the cached line from `pos[pfx]` (separator before every item
   except the first, then the item name), write `out`, then
   `isr_sinfo(rep, supps[cnt], wgts[cnt], eval)`, then `'\n'`.
7. Tid output (unused).

Statistics (`-Z`) and the pattern spectrum (`-P`) therefore count exactly the
sets that passed every filter, by their final size and support; `psp_incfrq`
silently ignores sizes/supports outside the limits given at `isr_addpsp` time
(`patspec.c:318-320`).

### 4.9 Generators (`is_isgen`, `report.c:579-613`)

Called from `isr_add` for prefix+`item` (support `supp`):

- Key format in `iset`: `[size, item_1, …, item_size]` (ITEMs); hash
  `is_hash` (`h = h*251 + item`, Duff's device, order dependent,
  `report.c:528-556`); equality `is_cmp` (size and items in order).
- If `cnt > 0`: copy `items` + new item; with `ISR_SORT` (generators with item
  reordering, `fpgrowth.c:1983-1984`) sort with `ia_qsort(…, dir)`. Then for
  every proper subset with one item removed, **except the one without the new
  item** (the prefix is known to be a generator): look it up; if absent or its
  stored support equals `supp` → not a generator, return 0. The loop rotates a
  hold-out item through the buffer so every subset key stays in sorted order.
- Otherwise insert the full set with its support (`-1` on OOM) and return 1.
- Single items (`cnt == 0`) are always generators; items with support equal to
  the total are never added (top-level perfect extensions are dropped).
- Stored sets are never removed and are inserted independent of the size,
  support, border and evaluation filters.

### 4.10 Association rules (`isr_reprule`, `report.c:1609-1645`)

Used only via `istree.c` `r4set()` (`apriori/src/istree.c:1980-2057`) for
`-tr`. The rule is the current set (`items[0..cnt-1]`, `supp = supps[cnt]`)
with head `item` (must be in the set). Gates: `smin <= supp <= smax` and
`zmin <= cnt <= zmax` (size counts the head). Counts `stats[cnt]`, `repcnt`,
`psp` (but `-P` is disabled for rules, `fpgrowth.c:2455-2456`). Confidence,
body-support, and evaluation thresholds are applied by the caller. Text:
`hdr`, head name, `imp`, body items in `items[]` order skipping the head
(separated by `sep`), `isr_rinfo(rep, supp, body, head, eval)`, `'\n'`. With an
empty body the line reads `d <-  (100, 70)` (two spaces) — **confirmed**.

### 4.11 Evaluation functions

`isr_logrto(rep, NULL)` (`report.c:1279-1291`), the only one fpgrowth installs
(`-eb`, threshold `-d/100`, direction +1): 0 if `cnt <= 1`; else
`log(supp/supps[0])/LN_2 - Σ ldps[items[i]]` (0 instead of the first log if
`supp <= 0`). With the default threshold (`-d10` → 0.1) all single items are
therefore filtered out — **confirmed** (`-eb` on `test1.tab`; `b c (6) 0.292181`
= log2(0.6) − 2·log2(0.7)). Other measures (`-e` rule measures) go through the
`istree` path: `isets()` calls `isr_reportv(rep, v)` (stores `eval = v`, no
`evalfn`), so `%e` prints `v`. The istree path pushes no perfect extensions
(`ist_create(…, IST_REVERSE, …)` without `IST_PERFECT`, `fpgrowth.c:2041`,
`apriori/src/istree.c:1907`).

### 4.12 Statistics output (`isr_prstats`, `report.c:1880-1891`)

`"all: %zu\n"` with `repcnt`, then `"%3d: %zu\n"` for every size from `min`
(fpgrowth passes 0) up to the largest size with a nonzero count. With no
reported set only `all: 0` is printed (**confirmed**). Written with `fprintf`
to `stdout` while the item sets are still in the reporter's 64 KiB buffer
(§5.8, B9).

---

## 5. Output formatting

### 5.1 Line layout

- Item set: `hdr` + `name(i1)` + (`sep` + `name(ik)`)… + info expansion + `"\n"`.
- Rule: `hdr` + `name(head)` + `imp` + body names joined by `sep` +
  rule-info expansion + `"\n"`.
- `hdr` is always empty in fpgrowth (B1). Empty `info` gives just the items.
- The empty set (only with `-m0`, or B3) is the line `info + "\n"`, e.g. ` (10)`.

### 5.2 Item names

`inames[i] = ib_xname(base, i)` taken at `isr_create` (after recoding), i.e.
the raw item string (string-named item base, `ib_create(0, 0)`,
`fpgrowth.c:2466`; `tract.c:454-467`). No quoting or escaping is ever applied:
`-g` is ignored (B2). Had it worked, `scn_format` (`util/src/scanner.c:253-284`)
would quote names containing spaces/control/special/non-ASCII characters and
escape them as `\c` or `\xhh`.

### 5.3 Defaults in the fpgrowth CLI (`fpgrowth.c:2225-2229,2457-2461`)

| Option | Default |
| --- | --- |
| `-h` header | `""` (ignored anyway) |
| `-k` separator | `" "` |
| `-I` implication | `" <- "` |
| `-v` sets | `" (%S)"`; `" (%a)"` if `-s` is negative (absolute) |
| `-v` rules | `" (%X, %C)"`; `" (%b, %C)"` if `-s` is negative |

The default is chosen only if `-v` was not given (pointer comparison with
`dflt`).

### 5.4 Info format grammar

`%` [ *d* [ *d* ] ] *c* — up to two decimal digits give the number of
significant digits `k` (default 6, capped at 32; `getsd`, `report.c:155-167`);
digits are ignored for integer quantities. Any other character is copied.
Unknown `c` → the whole `%`, digits and `c` are copied literally; `%` (plus
digits) at the end of the string is copied literally (`case 0: --s;` falls into
`default`, `report.c:1972-1973`; **confirmed** `%5` at end prints `%5`). A third
digit is taken as the code letter (`%123S` prints `%123` then `S`). `%.2S`
prints `%.` then `2S`.

**Item sets — `isr_sinfo` (`report.c:1925-1977`).** `supp` = set support,
`smax = supps[0]` (total weight; 1 if `<= 0`), `wgt = wgts[cnt]`,
`wmax = wgts[0]` (1 if `<= 0`), `eval = rep->eval`:

| Code | Value | Printer |
| --- | --- | --- |
| `%%` | `%` | |
| `%i` | `cnt` (set size) | int |
| `%a` | `supp` | int |
| `%q`, `%Q` | `smax` (total transaction weight) | int (of the clamped double) |
| `%s`, `%x` | `supp/smax` | num |
| `%S`, `%X` | `100*(supp/smax)` | num |
| `%w` / `%W` | `wgt` / `100*wgt` | num |
| `%r` / `%R` | `wgt/wmax` / `100*(wgt/wmax)` | num |
| `%z` | `wgt*smax` | num |
| `%e`, `%p` / `%E`, `%P` | `eval` / `100*eval` | num |
| `%d` | **not handled** — printed literally except in fast mode (B6) | |

In fpgrowth `wgt` is 0 for every non-empty set; for the empty set (and the
expansions of the top-level perfect extensions) it is the total weight, so
`%w %W %r %R %z` print `10 1000 1 100 100` for the empty set of `test1.tab`
(`-m0`) — **confirmed**. `eval` is 0 unless `-e` is used.

**Rules — `isr_rinfo` (`report.c:1981-2039`).** `smax` as above,
`supp` = rule (set) support, `body`, `head` supports:

| Code | Value | Printer |
| --- | --- | --- |
| `%%`, `%i` | `%`, `cnt` (items in rule incl. head) | int |
| `%a`, `%b`, `%h`, `%Q` | `supp`, `body`, `head`, `smax` | int |
| `%s`/`%S` | `supp/smax` / `100*(supp/smax)` | num |
| `%x`/`%X` | `body/smax` / x100 | num |
| `%y`/`%Y` | `head/smax` / x100 | num |
| `%c`/`%C` | `body>0 ? supp/body : 0` / x100 | num |
| `%l`/`%L` | `body<=0 or head<=0 ? 0 : (supp*smax)/(body*head)` / x100 | num |
| `%e`/`%E` | `eval` / x100 | num |

`%q`, `%w`, `%d`, … are literal for rules (**confirmed**). The x100 forms are
`100*(x/y)` (division first), which matters for bit-exactness.

### 5.5 Integers (`isr_intout`, `report.c:248-277`)

Plain decimal, `-` sign, `0` for zero, special string for `PTRDIFF_MIN`. The
`ints` table from `isr_prefmt` is used when the value is in `[imin, imax]`; the
strings are identical to the computed ones (**confirmed**: harness check of
`[1,5000]`, `[37,123456]`, `[0,9]`; `-N` vs default outputs are byte-identical
on a 3000-transaction file, fast and normal mode). Go: `strconv.AppendInt`.

### 5.6 Floating point (`isr_numout` + `mantout`, `report.c:281-375`)

This is **not** `printf("%.*g")`. Algorithm (with `k` significant digits):

1. `NaN` → `nan` (no sign). Negative → emit `-`, negate (so `-0.0` is not
   negative). `inf` → `inf`. `x < DBL_MIN` (zero and subnormals) → `0`
   (a negative subnormal prints `-0`).
2. `k > 32` → 32. `k > 11` → `sprintf("%.*g", k, x)` (correctly rounded libc).
3. Exponent form if `x >= 10^k` (`pows[k+2]`) **or `x < 0.001`**: scale by
   `1e32` until `1 <= x < 1e32`, find `e = floor(log10 x)` via `dbl_bisect` on
   the power table, divide by `10^e`.
4. `mantout(x, k, ints)` with `ints = k` (no exponent) or 1: number of
   decimals = `k` − (number of integer digits) (for `x < 1`: `k`, `k+1`, `k+2`
   for `x` in `[0.1,1)`, `[0.01,0.1)`, `[0.001,0.01)`); split `x` into
   `floor(x)` and fraction `y`; generate each decimal by `y *= 10;
   digit = (int)y; y -= floor(y)`; round on the remaining `y`:
   - `y > 0.5` → round up;
   - `y == 0.5` → if there are decimals, round up iff the last decimal is odd
     (half-even); **if there are no decimals, round up iff the integer part is
     even** (`floor(x/2) >= x/2`, `report.c:302`) — half-to-odd (bug B7);
   - carry propagates through `9`s; on carry into a new integer digit drop one
     decimal; strip trailing zeros and a bare point.
   If the integer part has more than `ints` digits, return `-n`; the caller
   rescales (`x /= 10^(n-1)`, `e += n-1`) and retries with `ints = 1`.
5. If `e != 0`: `e`, sign, at least two exponent digits (`e+06`, `e-04`,
   `e+100`). Values in `[1, 10)` that took the exponent branch end with
   `e == 0` and print without exponent.

Measured differences from `printf("%.*g")` (harness, `repcm.o` on arm64):

| Input, digits | `isr_numout` | `printf %.*g` | Cause |
| --- | --- | --- | --- |
| `0.0005`, 6 | `5e-04` | `0.0005` | threshold `0.001` vs `1e-4` |
| `1/2000` via `-v' %s'` (CLI) | `5e-04` | — | same |
| `2.5`, 1 / `3.5`, 1 / `9.5`, 1 | `3` / `3` / `9` | `2` / `4` / `1e+01` | half-to-odd on integers |
| `12.5`, 2 (`-v' %2S'`, 1 of 8, CLI) | `13` | `12` | half-to-odd |
| `999999.5`, 6 / `123456.5`, 6 | `999999` / `123457` | `1e+06` / `123456` | half-to-odd |
| `0.35`, 1 | `0.4` | `0.3` | inexact digit generation hits an exact tie |
| `0.1125`, 3 | `0.112` | `0.113` | idem (true value is above the tie) |
| `100*1741/160000 = 1.088125`, 6 | `1.08812` | `1.08813` | idem, default `%S` precision |
| `-0.0` | `0` | `-0` | sign test `num < 0` |
| `1e-320` (subnormal) | `0` | `9.99989e-321` | `DBL_MIN` cut-off |
| `75`, 0 | `7e+01` | `8e+01` | `k = 0` is kept (printf treats it as 1); half-to-odd |
| `0.7`, 0 / `0.3`, 0 | `1` / `0` | `0.7` / `0.3` | `k = 0` |

Frequency: over all `k/n` and `100*k/n` with `n <= 2000`, mismatches per digit
count were 1000–4000 of 4 006 000 values (most of them the `[1e-4,1e-3)`
threshold for fractions); for the default `%S` (6 digits) and `n <= 3000`
there were **0** mismatches in 4 504 500 percentages, but large databases do
hit them (90 of 1 058 635 for dyadic/quinary denominators up to 390 625).
Outside `[1e-4, 1e-3)` random values agree with `printf` almost always.
Conclusion: the Go port must reimplement `mantout`/`isr_numout` operation by
operation in `float64`; `strconv`/`fmt` are only valid for the `k > 11` branch
(`strconv.FormatFloat(x, 'g', k, 64)` matches C `%.*g` there, including the
two-digit exponent and trailing-zero removal).

### 5.7 Fast mode (`fastchk`, `report.c:135-151`)

```c
if (border || repofn || evalfn || tidfile) fast = 0;
else if (!file)                            fast = -1;   /* count only */
else fast = (zmin <= 1 && zmax >= ITEM_MAX
          && (info == " (%a)" || info == " (%d)")       /* strcmp */
          && hdr == "" && sep == " ") ? +1 : 0;
```

Re-evaluated by `isr_settarg`, `isr_setsize`, `isr_setbdr`, `isr_clrbdr`,
`isr_setfmtx`, `isr_seteval`, `isr_setrepo`, `isr_open`, `isr_close`,
`isr_setup`; the value after `isr_setup` is the one used. In fpgrowth fast
mode is active for absolute support (`-s-N`, default info `" (%a)"`) or an
explicit `-v' (%a)'`/`-v' (%d)'`, with no `-m>1`, `-n`, `-F`, `-eb`, and
default `-k`. The default relative `" (%S)"` is never fast.

Observable differences of fast mode (everything else is byte-identical; a
differential run over all four targets and the four `-A` variants on a dense
random file found no other difference):

- the empty set is emitted (and counted in `stats[0]`/`repcnt`) when there are
  top-level perfect extensions and `zmin == 1` (B3);
- `%d` prints the support (B6).

Count-only mode (`fast = -1`) differs from output mode by B4 and B5.

### 5.8 Buffering, files and errors

- All text goes through a private 64 KiB buffer (`BS_WRITE`, `report.c:115`)
  flushed with `fwrite` when full (`isr_putc`/`isr_puts`/`isr_putsn`,
  `report.c:204-244`) and at `isr_close`. `fwrite` results are ignored; errors
  are only detected at close via `ferror()`/`fclose()` (`report.c:1022-1024`).
  Mining always runs to completion; then fpgrowth reports
  `write error on file <name>` and exits with status 4 (`E_FWRITE = -4`) —
  **confirmed** with stdout closed (`>&-`). `fopen` failure → `cannot open file
  <name>`, status 2 (**confirmed**).
- The line cache `out` is sized in `isr_setup` from the name lengths; there are
  no bounds checks when appending, relying on each item appearing at most once.
- `-Z` statistics are printed with `fprintf(stdout)` before `isr_close`, so
  with output to stdout they land **inside** the item-set stream at the last
  64 KiB boundary — mid-line (B9, **confirmed**: offset 262144 of a 297 532
  byte stream: `…i01 i24 i11all: 16880\n  0: 0\n…`). For short outputs they
  appear before all item sets.
- The progress message `writing <name> ... [N set(s)] done` (stderr) uses
  `repcnt`, so it includes B3/B4/B5 effects.

---

## 6. Suspected bugs and undefined behaviour

| ID | Description | Evidence | Status | fpgrowth impact |
| --- | --- | --- | --- | --- |
| B1 | `isr_setfmtx` never assigns `rep->hdr`; `-h` has no effect. | `report.c:884` (copy goes to `rep->str`); uses at `:1107,1114,1634` | **confirmed**: `-h'HDR:'` output identical to default | `-h` ignored |
| B2 | `scan` is read only in `isr_createx`, where it is always 0; `-g` has no effect. | `report.c:661,713,876` | **confirmed**: `-g` on names with `'`, `$`, UTF-8 → byte-identical | `-g` ignored |
| B3 | Fast output emits and counts the empty set when `zmin == 1` and top-level perfect extensions exist (`fastout` has no size check). | `report.c:1336-1364,1550-1551`; gate `:1489-1491` passes because `cnt+n >= zmin` | **confirmed**: `allA.tab -s-1` → 8 sets incl. ` (3)`; with `-v' (%a) '` 7 sets; count-only 7; `-Z` shows `0: 1` | spurious line ` (N)` and count +1 |
| B4 | Count-only `ISR_NOEXPAND` path counts closed/maximal sets with `cnt+n > zmax`. | `report.c:1496-1505` vs `:1537` | **confirmed**: `test1.tab -tc -s-2 -n1`: count-only 5 (`2: 1`), with output 4; `-tm -n1`: 1 vs 0 | wrong `[N set(s)]`/`-Z` without output file; `-P` unaffected (out-of-range sizes ignored) |
| B5 | Binomial counting `c = c*(n-k+1)/k` overflows `size_t` for `n >= 63` perfect extensions although `2^63-1` fits. | `report.c:1515-1517` | **confirmed**: 2 identical transactions of 63 items, no output file → `3236635425598607969` instead of `9223372036854775807` (62 items correct) | wrong counts for huge hypercubes (`n >= 64` cannot be represented at all) |
| B6 | `%d` (absolute support) is only understood by the fast path; `isr_sinfo` prints it literally. | `fastchk` `report.c:147` vs no `case 'd'` in `:1943-1974` | **confirmed**: `-v' (%d)'` → ` (7)`; with `-m2` → ` (%d)` | inconsistent output |
| B7 | Exact-tie rounding in `mantout` is half-to-odd when there are no decimals (`floor(x/2) >= x/2` is true for even `x`). | `report.c:301-302` | **confirmed** (harness; CLI `-v' %2S'`: 12.5 → `13`, 37.5 → `37`) | only with few significant digits |
| B8 | Digit generation by repeated `y*=10` is not exact; near-ties round differently from correctly rounded output; exponent threshold `0.001` instead of `1e-4`. | `report.c:296-302,349-350` | **confirmed** (§5.6 table) | rare differences from `printf`; must be ported exactly, not "fixed" silently |
| B9 | `-Z` statistics written to `stdout` while item sets are still buffered → spliced into the stream. | `fpgrowth.c:2528-2531`, `report.c:1880-1891` vs `:115,195` | **confirmed** | garbled stdout with `-Z` and output `""`/`-` |
| B10 | `isr_createx`: `malloc` of `rep` unchecked; on a later allocation failure `isr_delete` walks the not yet initialised `inames` array. | `report.c:634-697,709,750-755` | hypothesis (OOM only) | crash instead of "not enough memory" |
| B11 | Pattern-spectrum OOM is not reported on the no-expansion path (closed/maximal with output), only latched. | `report.c:1390-1391,1535-1549` vs `:1559-1562` | hypothesis (OOM only) | silently incomplete `-P` file |
| B12 | `isr_tail` maps `cm_tail() == -1` (OOM) to 0. | `report.h:507` | hypothesis (OOM only) | OOM in maximal HUT pruning ignored |
| B13 | No-expansion path leaves `pfx > cnt`; a following `output()` before `isr_remove()` would print a stale prefix. | `report.c:1538-1544`, `:1396-1404` | latent; unreachable in fpgrowth (always `isr_remove` next; the istree path pushes no perfect extensions) | none |
| B14 | `isr_occout` range test uses `occ <= imin` instead of `>=` → negative index into `ints`. | `report.c:512` | hypothesis, unreachable (tid output unused) | none |
| B15 | Border semantics differ: `output()` keeps sizes `>= bdrcnt`, `isr_iset` drops them; `isr_rule` does not update the pattern spectrum while `isr_reprule` does. | `report.c:1378-1381` vs `:1660-1662`; `:1744-1749` vs `:1625-1628` | latent, unreachable | none |
| B16 | `isr_addpexpk`: `1 << i` reaches `1 << 31` (signed overflow, UB) if bit 30 is set. | `report.c:1244-1245` | latent; only 16 packed bits are used | none |
| B17 | Header slips: macro `isr_info` uses non-existent field `format`; `isr_pex(r,t)` uses `i`; prototypes declare `isr_smin/isr_smax` as `ITEM`, `isr_pexs` as `const int*`; `mantout` has external linkage; `isr_reset` clears `ib_cnt+1` entries with `sizeof(size_t)` instead of `size+1` `ISCNT`s. | `report.h:469,491,314-315,384`; `report.c:281,1871-1872` | latent (unused / harmless for fpgrowth) | none |
| B18 | fpgrowth passes `(RSUPP)floorsupp(smax)`; out-of-range `double → int` conversion is UB in C (arm64 saturates; x86-64 `cvttsd2si` yields `INT_MIN`, which would reject every set). | `fpgrowth.c:1996-1998` | hypothesis for x86-64; on arm64 `-S-1e12` works (17 sets) | platform-dependent `-S` with huge absolute values |

---

## 7. C function → proposed Go component

Suggestion: split the reporter into (a) a mining-side package that decides
**which** item sets/rules exist and counts them, and (b) a text writer that
decides **how** they look. The miner talks only to (a); (a) hands each
accepted set to a sink interface implemented by (b) (or by library users).

### 7.1 (a) Library: e.g. `internal/report` (mining-side reporter)

| C | Proposed Go | Notes |
| --- | --- | --- |
| `ISREPORT` (mining fields) | `type Reporter struct` | separate `inUse []bool` and `pexPerLevel []int32` instead of the packed `pxpp`; `items`, `pexs` as slices (concatenate explicitly where C relies on contiguity, `report.c:1478`) |
| `isr_createx` | `New(nItems int, totalWeight Supp, itemFreq []Supp) *Reporter` | captures `supps[0]` and `ldps`; names stay in the CLI |
| `isr_settarg` | `SetTarget(t Target, m Mode, dir int) error` | builds `clomax.Filter` or generator set; forces `NoExpand` for closed/maximal |
| `isr_setsupp`, `isr_setsize`, `isr_setbdr`, `isr_clrbdr` | `SetSupport`, `SetSize`, `SetBorder(size, supp)`, `ClearBorder` | |
| `isr_seteval`, `isr_logrto` | `SetEval(fn func(*Reporter) float64, dir int, thresh float64)`, `LogRatio` | `LogRatio` must use `math.Log(x)/Ln2`, not `math.Log2` |
| `isr_addpsp`, `isr_getpsp` | `SetPatternSpectrum(*patspec.Spectrum)` | limits captured at call time |
| `isr_setup` | `Setup()` | computes `xmax`, resets counters |
| `isr_add` (+ `cm_add`, `is_isgen`) | `Add(item Item, supp Supp) (added bool, err error)` | 0/1/-1 contract |
| `isr_addpex`, `isr_addpexpk` | `AddPex(item) bool`, `AddPexPacked(bits uint32)` | |
| `isr_remove` | `Remove(n int)` | |
| `isr_xable`, `isr_uses`, `isr_cnt`, `isr_supp`, `isr_pexcnt`, … | methods | |
| `isr_tail` | `Tail(items []Item) bool` | |
| `isr_report`, `isr_reportv` | `Report() error`, `ReportEval(v float64) error` | clomax update, gates, count-only binomials, no-expand, expansion |
| `report()` / `fastout()` recursion | `expand(n int)` | one implementation; `fastout` differs only by B3 |
| `output()` filter part | `emit()` | border → eval → stats/psp → `sink.Set(...)` |
| `isr_reprule` | `ReportRule(head Item, body, headSupp Supp, eval float64) error` | |
| `is_isgen`, `is_hash`, `is_cmp` | `generatorFilter` (`map[string]Supp` keyed by the encoded item sequence) | keep order sensitivity and `ISR_SORT` |
| `stats`, `repcnt` | `Stats() []uint64`, `Count() uint64` | uint64 wrap semantics for B5 parity |
| (new) | `type Sink interface { Set(items []Item, supp Supp, wgt, eval float64) error; Rule(head Item, items []Item, supp, body, headSupp Supp, eval float64) error }` | `nil` sink ⇒ count-only mode (§4.3) |
| (compat knobs) | e.g. `Compat{EmitEmptyInFastMode, CountOnlyIgnoresZmaxForClosed bool}` | lets the CLI request B3/B4 for byte parity; defaults to be decided in `docs/decisions.md` |

### 7.2 (b) CLI / output layer: e.g. `internal/isrout` used by `cmd/fpgrowth`

| C | Proposed Go | Notes |
| --- | --- | --- |
| `isr_setfmtx`, `hdr/sep/imp/info` | `type Format struct{ Header, Sep, Imp, Info string; Scan bool }` | B1/B2 decide whether `Header`/`Scan` are honoured |
| `inames` (`ib_xname`, `scn_format`) | `names []string` built from the recoded item base | |
| `isr_open`, `isr_close`, `isr_flush`, `isr_putc/puts/putsn` | `bufio.NewWriterSize(w, 65536)`; `Close()` returns first write/close error | B9 parity needs the same 64 KiB flush points |
| `fastchk` | `fastEligible(Format, reporter cfg) bool` | only to reproduce B3/B6 |
| `output()` text part, `fastout` text | `(*Writer).Set(...)` implementing `Sink` | |
| `isr_reprule` text | `(*Writer).Rule(...)` | |
| `isr_sinfo`, `isr_rinfo`, `getsd` | `appendSetInfo`, `appendRuleInfo`, `parseDigits` | table-driven per §5.4 |
| `isr_intout`, `isr_prefmt` | `strconv.AppendInt` | pre-formatting not needed |
| `isr_numout`, `mantout`, `pows`, `dbl_bisect` | `appendNum(dst []byte, x float64, k int) []byte` | operation-by-operation port; `strconv.FormatFloat(x,'g',k,64)` only for `k > 11` |
| `isr_prstats` | `printStats(w io.Writer, stats []uint64, total uint64)` | |
| tid output, `isr_iset/isetx/rule/seqrule/sxrule/extrule/xinfo/wgtout/getinfo` | not needed for fpgrowth | port later only if apriori/eclat are ported |

---

## 8. Porting risks (highest first)

1. **Float formatting bit-exactness.** `appendNum` must mirror `mantout`
   exactly: same power table, same `1e32` scaling loops, same
   `y*=10 / (int)y / y-=floor(y)` sequence, same tie rules (B7, B8). Go may
   fuse `x*y+z` into FMA on arm64/ppc64le/s390x (the spec allows fusion, also
   across statements); wrap every product in an explicit `float64(...)`
   conversion. Use `math.Floor`, `math.Mod` (exact). Port `dbl_bisect`
   semantics (index of the last equal element, else the insertion point).
2. **libm differences.** `-eb` values use `log()`; Go's `math.Log` and Apple or
   glibc `log` may differ in the last ulp, which can flip a 6-digit `%e`
   rounding or a threshold comparison. Plan differential tests with that in
   mind and document any residual.
3. **Fast/normal/count-only divergences** (B3, B4, B5, B6): the C program's
   output and counts depend on the output format and on whether an output file
   is given. A faithful port needs the library to know "count-only" (nil sink,
   no border, no eval) and the CLI to know "fast-eligible". Decide per quirk
   whether to replicate or fix, and record it in `docs/differences.md`.
4. **Output order.** Perfect-extension expansion order (§4.7), post-order
   reporting, prefix order of items, and the final empty-set report must match;
   golden tests should compare files byte for byte, not as sets.
5. **Closed/maximal semantics.** Repository update before all other filters;
   `NoExpand` reporting of `cnt+n` items, sorted descending for the repository
   but printed in `items` order; `xmax = zmax+1`; `sto` logic; the empty set is
   never maximal; size gates on `cnt+n`.
6. **Generators.** Order-sensitive keys, `ISR_SORT` with `dir = -1`, perfect
   extensions silently ignored, stored sets independent of filters.
7. **Integer widths.** `Supp` is `int32` in C; set counters are 64-bit and wrap.
   `float64 → int32` conversions of user limits must be clamped explicitly (Go
   leaves out-of-range conversions implementation-dependent; cf. B18).
8. **Ignored options** (`-h`, `-g`): replicating means accepting and ignoring
   them; fixing changes output. Either way document it.
9. **I/O behaviour.** Errors only surface at close (exit 4, mining completes);
   `-Z` splicing into stdout (B9) depends on the 64 KiB buffer; `-` means
   stdout because of the argument parser; `""` means stdout; no output file
   means count-only.
10. **Weights and `%w`-family codes** print 0 for non-empty sets and the total
    weight for the empty set; `%q/%Q` print the total weight of all input
    transactions (`ib_getwgt` at creation, unchanged by `-m` filtering:
    `filt.tab -s-1 -m2 -v' (%a, %Q, %S)'` → `b a (2, 6, 33.3333)`).

---

## Appendix A: Reproducers

All runs from a scratch directory; `FPG` is the reference binary; `test1.tab`
is `fpgrowth/ex/test1.tab`. Small inputs:

- `allA.tab`: `a b` / `a c` / `a b c` (item `a` in every transaction).
- `pexo.tab`: `x a b c` / `x a b c` / `y x` / `y`.
- `names.tab`: `it'em x$y café` / `it'em x$y` / `it'em`.
- `pex63.tab`: the same line of 63 distinct items, twice.
- `tiny.tab`: `a b` followed by 1999 lines `b`; `eighth.tab`: `x y` + 7 × `y`.
- `filt.tab`: `a b`, `a b`, `a`, `b`, `c`, `c`.
- `rnd.tab`: 3000 random transactions (1–12 items out of 41, seed 7).

| Bug / fact | Command | Observed |
| --- | --- | --- |
| B1 | `$FPG -s30 -h'HDR:' test1.tab o` | lines start with item names, no `HDR:` |
| B2 | `$FPG -s-1 -g names.tab g; $FPG -s-1 names.tab n; cmp g n` | identical |
| B3 | `$FPG -s-1 allA.tab ""` vs `-v' (%a) '` vs no outfile | 8 / 7 / 7 sets; last fast line ` (3)` |
| B4 | `$FPG -tc -s-2 -n1 -Z test1.tab` vs `... test1.tab ""` | `all: 5` (`2: 1`) vs `all: 4` |
| B5 | `$FPG -s-1 pex63.tab` | `[3236635425598607969 set(s)]` (62 items: `4611686018427387903`, correct) |
| B6 | `$FPG -s-3 -v' (%d)' test1.tab ""` vs with `-m2` | ` (7)` vs ` (%d)` |
| B7/B8 | `$FPG -s-1 -v' %S %2S %1S %3s' eighth.tab ""` | `x y 12.5 13 1e+01 0.125` |
| threshold | `$FPG -s-1 -v' %s %S %2S %12s' tiny.tab ""` | `a b 5e-04 0.05 0.05 0.0005` |
| B9 | `$FPG -s-5 -Z rnd.tab "" > z` | `all:` at byte 262144, mid-line |
| write error | `$FPG -s-2 test1.tab "" >&-` | `write error on file <stdout>`, exit 4 |
| `-eb` | `$FPG -s-2 -eb -v' (%a) %e' test1.tab ""` | only 2-/3-sets, e.g. `b c (6) 0.292181` |
| order | `$FPG -s-2 -k_ pexo.tab ""` | §4.7 sequence |
| border | `$FPG -s-2 -F40:50 test1.tab ""` | sizes 1/2 need ≥4/≥5; 3-sets with support 2 kept |

Number-format harness: a C file that `calloc`s an `ISREPORT`, sets `info`,
`hdr`, `sep`, opens it on an `fmemopen` stream with `isr_open`, and calls
`isr_numout`/`isr_intout`/`isr_prefmt` directly; compiled with
`cc -std=c99 -O3 -DNDEBUG -DISR_PATSPEC -DISR_CLOMAX -I{util,math,tract}/src`
and linked against the reference build's `repcm.o clomax.o patspec.o taread.o
arrays.o memsys.o idmap.o scform.o escape.o tabread.o tabwrite.o -lm`. It
compares against `snprintf("%.*g")`; the numbers in §5.6 come from it. A Go
port should turn the §5.6 table plus a corpus generated by this harness into
golden tests for `appendNum`.
=== END docs/upstream/report.md ===