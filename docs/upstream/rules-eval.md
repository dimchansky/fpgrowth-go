# Upstream audit: association rules and evaluation measures (fpgrowth 6.21)

Scope: how the `fpgrowth` program generates association rules (target
`-tr`) and how it evaluates rules and item sets with the additional
measures (`-e`, `-a`, `-d`, `-i`, `-p`, `-o`, `-c`, and `-eb`). Covered
code:

- `fpgrowth/src/fpgrowth.c`: `rec_tree()`/`fpg_tree()` (the rule path),
  `fpg_create()`, `fpg_data()`, `fpg_report()`, `fpg_mine()`, and the
  option parsing in `main()`.
- `apriori/src/istree.c`: only the functions that fpgrowth reaches (list
  in section 2.3).
- `math/src/ruleval.[ch]`, `math/src/gamma.c`, `math/src/chi2.c`.
- The rule-related parts of `tract/src/report.c` (`isr_reprule`,
  `isr_rinfo`, `isr_logrto`, the evaluation check in `output`).

All paths are relative to `reference/upstream/fpgrowth-6.21/`. `$FPG` is
the reference executable built with the upstream makefile flags
(`-std=c99 -O3 -funroll-loops -DNDEBUG`) using Apple clang 21 on macOS
arm64. `$FPGDBG` is a `-g -O0` build with assertions enabled. A finding
marked **confirmed** was reproduced with these binaries or with the
harness in section 8.1, and the reproducer is given. A finding marked
**hypothesis** comes from reading the code only.

Notation used throughout: for a rule `B -> h` (body `B`, single head
item `h`):

| Symbol | C name | Meaning |
| --- | --- | --- |
| `s` | `supp` | support of `B ∪ {h}` (absolute, a transaction weight sum) |
| `b` | `body` | support of `B` (for an empty body this is `n`) |
| `h` | `head` | support of `{h}` (the item frequency) |
| `n` | `base` | total transaction weight (`ib_getwgt`, number of transactions when there are no weights) |

`SUPP` is C `int` in the default build (`tract/src/tract.h:103-109`).

---

## 1. When the rule/evaluation path is used

### 1.1 Path selection

`fpg_mine()` (`fpgrowth.c:2012-2081`) picks one of two independent code
paths:

| Target | `-e` letter | Path |
| --- | --- | --- |
| `s f c m g` | `x` (none) or `b` | plain fpgrowth variants (`fpgvars[algo]`, `-A` honored). `b` is evaluated by the reporter (`isr_logrto`). |
| `s f c m g` | any rule measure (`o c d l a q v e r k w u j z n p y t i g f h m s`) | item set tree: `fpg_tree()` + `ist_*` |
| `r` | anything | item set tree: `fpg_tree()` + `ist_*` |

On the item set tree path the following are switched off or ignored:

- `-A`: `fpg_tree()`/`rec_tree()` (`fpgrowth.c:1687-1795`) is called
  directly. The help text says "enforces algorithm variant 'd'", but the
  code is a separate routine, not `fpg_single`. **Confirmed:** `-tr` and
  `-ts -el` give identical output for `-As -Ac -Ad -At`.
- The 16-items machine (`-l`) is cleared (`fpgrowth.c:1840-1842`).
- Filtering transactions by size (`tbg_filter`) is skipped
  (`fpgrowth.c:1947-1949`).
- `-x` (perfect extension pruning) has no effect. The tree is created
  with mode `IST_REVERSE` only (`fpgrowth.c:2041`), so `IST_PERFECT` is
  never set. **Confirmed:** `-x`, `-l0`, `-l4`, `-j` and `-u` leave the
  `-tr` and `-ts -el` outputs on `r1.tab` byte-identical.
- `-P` (pattern spectrum) is disabled for rules (`fpgrowth.c:2455-2456`).
- The reporter gets `ISR_NOFILTER` (`fpgrowth.c:1986-1988`), so it does
  no closed/maximal filtering of its own.

### 1.2 Parameter derivation

| Quantity | Derivation | Lines |
| --- | --- | --- |
| `conf` | `-c` percent / 100. Forced to 100% for non-rule targets. | `fpgrowth.c:1824-1825, 1854` |
| body minimum `fpg->body` | `ceil(smin% · w · (1-DBL_EPSILON))`, or `-smin` if negative | `fpgrowth.c:1914-1917` |
| set minimum `fpg->supp` | rules without `-o`: `ceil(body_min · conf · (1-DBL_EPSILON))`. Otherwise: `body_min`. | `fpgrowth.c:1918-1920` |
| tree `smin` / `body` | `ist->smin = max(fpg->supp, 1)`, `ist->body = max(fpg->body, ist->smin)` | `istree.c:635-636` |
| tree `conf` | `conf · (1-DBL_EPSILON)` (a second, independent epsilon) | `istree.c:637` |
| reporter range | `[fpg->supp, floor(smax% · w · (1+DBL_EPSILON))]` | `fpgrowth.c:1996-1998` |
| `thresh` | `-d` value / 100, **for every measure**, including `o` (an absolute count), `b`, `j` (bits) and the p-values | `fpgrowth.c:1859` |
| item recoding threshold | `fpg->supp` (the set minimum, not the body minimum), so items that can only be heads survive | `fpgrowth.c:1936` |

### 1.3 Call sequence on the tree path (`fpgrowth.c:2038-2078`)

```
ist_create(base, IST_REVERSE, fpg->supp, fpg->body, fpg->conf)
ist_setsize(zmin, x)        // x = zmax+1 for c/m targets, capped at tbg_max
fpg_tree() -> rec_tree()    // fills the tree: ist_addchn/ist_down/ist_setsupp/ist_up
if (ITEM_MIN < prune <= 0) ist_filter(prune)       // runs BEFORE ist_seteval, see 5
if (c/m/g target) ist_clomax(target | (prune > ITEM_MIN ? IST_SAFE : 0))
if (e != FPG_LDRATIO) ist_seteval(eval, agg, thresh, prune)
ist_init(order = 0); ist_report(target)
```

The CLI always passes `order = 0` (`fpgrowth.c:2526`). So reporting is
done by the recursive `isets()`/`rules()`/`r4set()`. The iterators
`ist_iset()`/`ist_rule()` are reachable only through the library API
(`fpg_mine(..., order != 0)`).

---

## 2. The item set tree as fpgrowth uses it

### 2.1 Shape and order

- `IST_REVERSE`: the child node for item `X` holds counters only for
  items with a **smaller** code (`istree.c:942`). A path from the root
  therefore lists items in decreasing code order, and the deepest item of
  a set has the smallest code. Codes come from `tbg_recode` with
  `-sort` (`fpgrowth.c:1936`), so they depend on `-q`.
- Root counters hold the item frequencies (`ib_getfrq`,
  `istree.c:656-657`). Every head support `h` is read from the root
  (`ist->lvls[0]->cnts[item]`). `n` is `ist->wgt = ib_getwgt(base)`
  (`istree.c:632`).
- `rec_tree()` (`fpgrowth.c:1687-1740`) walks the conditional FP-tree.
  For every frequent conditional item it calls `ist_down`, sets the
  counters with `ist_setsupp` (only counters with support >=
  `fpg->supp`), recurses, and goes back up with `ist_up`. Before a
  recursion step, `ist_addchn` creates the children of the current node
  (`children()`/`child()`, `istree.c:912-1131`).
- `child()` creates a counter for the candidate `S ∪ {k}` only if all
  of the following hold (`istree.c:942-973`):
  - every `|S|`-subset is present with support >= `smin`;
  - at least one subset (or `S`, or `S∪{k}` itself) has support >=
    `ist->body`, so that some rule body is possible;
  - it would not combine two head-only items, and no item is `APP_NONE`.
  Items are processed in increasing code order at every level, so all
  subsets are already filled in when they are checked (code reading,
  consistent with every output observed). As a result the
  tree is downward closed: every subset of a stored set is stored too.
  `evaluate()` and `r4set()` depend on this. Body lookups never miss in
  practice. A miss would make `getsupp` return `SKIP`, and `COUNT(SKIP)`
  is 0.

### 2.2 Skip flags

`SKIP` is `SUPP_MIN` (the sign bit, `istree.c:146-152`), and `COUNT()`
strips it. On the fpgrowth path only these set it: `child()` for unused
slots of pure arrays, `ist_clomax` for non-closed/non-maximal/non-generator
sets, and `ist_filter` for infrequent counters (section 5).

### 2.3 Reachable `istree.c` functions

| Reachability | Functions |
| --- | --- |
| CLI | `ist_create`, `ist_delete`, `ist_xable` (macro), `ist_addchn` -> `children` -> `child`, `getsupp`, `search`, `ist_down`, `ist_up`, `ist_setsupp`, `ist_setsize`, `ist_filter` (effectively a no-op), `ist_clomax` + `clear`, `ist_seteval`, `ist_init` -> `makelvls`/`reclvls`, `ist_report` -> `isets`/`evaluate` (sets) or `rules`/`r4set` (rules) |
| library API only (`order != 0`) | `ist_iset`, `emptyset`, `ist_rule` |
| unreachable from fpgrowth (apriori only) | `ist_count*`, `count`, `ist_commit`, `ist_check`, `used`, `ist_prune`, `ist_addlvl`, `needed`, `cleanup`, `ist_next`, `ist_getsupp`, `ist_incsupp`, `ist_supp`, `ist_clear`, `ist_eval`, `ist_evalx` |

---

## 3. Association rule generation (`-tr`)

### 3.1 Which rules are generated

`rules()` (`istree.c:2061-2110`) visits every stored set `S` with
`COUNT(supp) >= ist->smin`. `r4set()` (`istree.c:1980-2057`) then forms
the rules `S\{x} -> x` for the items `x ∈ S`:

1. **Single-item consequents only.** No multi-item heads are ever
   formed.
2. **Empty bodies are allowed.** For `|S| = 1` the body support is
   `n` (`istree.c:2005-2006`). With default options, `-c` gates them
   alone, as `supp(x)/n >= conf`. **Confirmed:** `$FPG -tr -s20 -c50
   t1.tab` prints `d <-  (100, 70)`. The info shows body support 100%.
3. **Size limits count body + head.** `isr_reprule` checks
   `rep->cnt = |S|` against `[zmin, zmax]` (`report.c:1621`), and
   `ist_xable` also caps tree depth at `zmax`. `-m2` removes
   empty-body rules, and `-n2` keeps only `a <- b` rules.
   **Confirmed** (`-m2 -n2`, `-m0 -n1`).
4. **Acceptance tests**, in this order (`istree.c:2015-2030` for the
   first rule, `2040-2054` for the rest):
   - The head may be a head (appearance flags, 3.4).
   - `b >= ist->body` (the body minimum).
   - Not `(double)s < (double)b * conf·(1-ε)`, so confidence is accepted
     with `>=` and a 1-ulp tolerance.
   - If a measure is set: compute `v` (with `-i`: `v` is the worst value
     unless `s·n > h·b`, see 4.3). Reject if `dir·v < dir·thresh`. Note
     that this is a *reject-if-less* test, so `NaN` passes (bug B5).
   - In the reporter (`report.c:1619-1621`), `s ∈ [rep->smin,
     rep->smax]` and `|S| ∈ [zmin, zmax]`.
5. **`-o` (original support definition).** Without `-o`, the minimum
   support applies to the **body** (`b >= smin`), and the set minimum is
   only the derived bound `ceil(smin·conf)`. With `-o`, the minimum
   applies to `B ∪ {h}`: `s >= smin`, with the body minimum set to the
   same value and therefore redundant. **Confirmed:** `-s30 -c50` gives
   32 rules, and adding `-o` drops the 6 rules with `s = 2`.
6. **`-S` (maximum support) always applies to `s`** (body ∪ head), even
   without `-o`, because `isr_reprule` compares `rep->supps[cnt]`
   (`report.c:1619`). This is bug B8.
7. Rules do **not** use the `-F` border, the reporter evaluation
   function (so `-eb` does not apply, B1), `-a`, or `-p`. **Confirmed**
   for `-F` and `-a`. For `-p`, see 5.

### 3.2 Output order (CLI, `order = 0`)

- `rules()` is **post-order**. In each node, items are visited in
  increasing code order. For each item it first recurses into the child
  (all rules of supersets), then calls `r4set` for the set itself.
- Inside `r4set`, the heads are tried from the deepest item (smallest
  code) up to the item directly below the root (largest code).
- The body is printed in path order (decreasing code), skipping the head
  (`report.c:1635-1640`). The format is `hdr head imp body-items info`.
- **Confirmed** on `t1.tab`, where the codes are `d=0 c=1 b=2 a=3 e=4`.
  The output starts `d <- `, `d <- c`, `c <- d`, `c <- `, `d <- b`,
  `b <- d`, `d <- b c`, `c <- b d`, `b <- c d`, `c <- b`, `b <- c`,
  `b <- `, ...

The order depends on the item codes, so `-q` changes it. The Go port must
reproduce `tbg_recode`'s order exactly.

### 3.3 Rule information output

The default `-v` for rules is `" (%X, %C)"`, or `" (%b, %C)"` when `-s` is
negative (`fpgrowth.c:2457-2460`). It does not depend on `-o`, so with
`-o` the default output still shows **body** support. The `isr_rinfo`
fields (`report.c:1981-2039`) are `%a/%s/%S` for `s`, `%b/%x/%X` for `b`,
`%h/%y/%Y` for `h`, `%Q` for `n`, `%c/%C` for `s/b`, `%l/%L` for
`s·n/(b·h)`, and `%e/%E` for the measure value. `%e` prints 0 when there
is no measure. The confidence and lift printed here are recomputed by the
reporter, but the expressions match `re_conf`/`re_lift` bitwise (no
add, so no contraction).

### 3.4 Item appearances (`-R` with `-tr`, `ib_readapp`)

The flags are `APP_NONE=0`, `APP_BODY=1`, `APP_HEAD=2`, `APP_BOTH=3`
(`tract.h:420-423`). `child()` never builds sets with two head-only items
or with `APP_NONE` items. `r4set` should only let the head-only item
be the head of a set that contains one. It enforces this for the
deepest item (`istree.c:2002, 2015-2016, 2032`), but **not** in the loop
over the other path items (`istree.c:2040`). This is bug B3. `ist_rule()`
handles the case correctly (`istree.c:1840, 1844-1847`).

---

## 4. Rule-based evaluation of item sets (`-ts/-tc/-tm/-tg` with a rule measure)

### 4.1 `evaluate()` (`istree.c:542-596`)

For a stored set `S` (tree node `node`, counter `index`):

- `|S| = 0` (`index < 0`) or `|S| = 1` (`node` is the root) gives the
  constant `dir < 0 ? 1 : 0` (`istree.c:557-561`). So **singletons and
  the empty set are never reported for a (+) measure with `-d > 0`, or
  for a (-) measure with `-d < 100`.** **Confirmed:** with `-d0`,
  singletons print value 0.
- `|S| >= 2`: the first rule's head is the deepest item (smallest code,
  printed last), and its body is the rest. Its value is the first value
  (`istree.c:563-575`).
  - `-ax` (`IST_FIRST`) returns it. This matches the HTML doc ("the item
    that comes last in the order of the items"). **Confirmed:** `a b c`
    gives the lift of `c <- a b` = 40/35 = 1.142857.
  - Otherwise the remaining `|S|-1` rules are evaluated by walking up
    the path (`istree.c:579-592`). Each uses `head = ITEMOF(node on
    path)` and `body = getsupp(parent, rest-of-path)`.
  - `-am`: `if (val < agg) agg = val`. `-an`: `if (val > agg) agg = val`.
    `-aa`: the sum divided by `n = |S|`.
  - A NaN aggregates in C fashion: a NaN first value sticks through
    min/max, a later NaN is ignored, and avg propagates it (B5).
- `-i` (`IST_INVBXS`): each rule value is replaced by the worst value
  (`dir < 0 ? 1 : 0`) unless `(double)s·n > (double)h·b`. The test is
  strict, so a rule at exactly the expected support is invalidated. The
  replaced values **still enter the aggregate**. **Confirmed:**
  `-ts -ep -i -d100` shows `1` for sets whose deepest-head rule is below
  expectation.
- Selection: `isets()` reports the set iff `v·dir >= thresh_internal`
  (`istree.c:1918, 1942, 1969`), where `thresh_internal = dir · thresh`
  (`istree.c:1685`). This is an *accept-if-greater-or-equal* test, so
  NaN fails.
- `isets()` is pre-order: a set is reported before its supersets, and
  each node is visited in increasing code order. Items print in
  decreasing code order. This **differs from the plain fpgrowth path**:
  switching a measure on changes set order and item order, for example
  `a b c` instead of `a c b`. **Confirmed.**
- The reporter still applies `-S`, `-F` (border) and size limits through
  `isr_report`/`output`. **Confirmed** for `-F`.
- The rule confidence (`-c`) plays no role for item sets (conf is forced
  to 100%, and only `r4set`/`ist_rule` check it).

### 4.2 Closed, maximal and generator targets with a measure

`ist_clomax` first marks non-closed (non-maximal, non-generator) sets
among all frequent sets. `isets` then applies the evaluation. The result
is the intersection *closed ∩ passes evaluation*. It is **not** "closed
among the sets that pass". **Confirmed** on `r1.tab`: `-tc -el -d110`
gives 78 sets, equal to `-tc` ∩ `-ts -el -d110`. The same holds for `-tm`
(32 sets). The HTML doc claims that `-p-1` reverses the order, but it
does not (section 5).

### 4.3 `-eb` (binary log of the support quotient)

`-eb` is not a ruleval measure. `main()` maps `'b'` to `FPG_LDRATIO =
RE_FNCNT` (`fpgrowth.c:2436`), and `fpg_report` installs `isr_logrto` as
the reporter evaluation (`fpgrowth.c:2000-2001`):

```
if (rep->cnt <= 1) return 0;
sum = log(supps[cnt]/supps[0])/LN_2  - Σ ldps[item]       // report.c:1285-1290
ldps[i] = log(frq_i)/LN_2 - log(n)/LN_2                   // report.c:701-708
```

It is evaluated in `output()` with a *reject-if-less* test
(`report.c:1383-1386`), so it runs on the plain fpgrowth path with any
`-A`. `-a`, `-i` and `-p` are ignored for it. For `-tr` it is silently
ignored (B1).

---

## 5. Evaluation-based pruning (`-p`)

Three sources disagree, and the code makes `-p` a no-op:

| Source | Semantics claimed |
| --- | --- |
| help text (`fpgrowth.c:2292-2295`) | `< 0`: weak forward, `> 0`: strong forward, `= 0`: backward |
| HTML doc (Pruning with Additional Measures) | `-p-1` (any negative): backward, filtering before closed/maximal; `-p0`: default; `-pN>0`: forward from size N |
| code | `-p` with `ITEM_MIN < p <= 0` calls `ist_filter(p)` (`fpgrowth.c:2051-2057`); `p > 0` is stored in `ist->prune` (`istree.c:1686`) |

What actually happens:

- `ist_filter` runs **before** `ist_seteval` (`fpgrowth.c:2055` vs
  `2070`). At that point the tree still has the `ist_create` defaults
  `eval = RE_NONE`, `dir = 0`, `thresh = 0` (`istree.c:651`).
  `evaluate()` returns 0, `0 < 0` is false, and the final singleton/empty
  check `0 < 0` is false too (`istree.c:1446`). Only infrequent counters
  get marked, and they are skipped anyway.
- `ist->prune` (positive `-p`) is read only in `ist_commit`
  (`istree.c:742-743`), which is apriori's level-wise hook. fpgrowth
  never calls it, and the strong-filter branch of `ist_filter`
  (`size > 0`) is unreachable from fpgrowth.
- `IST_SAFE` is passed to `ist_clomax` for any `-p`, but with no marks
  set it changes nothing.

**Confirmed (bug B2):** on `r1.tab`, the outputs for targets `s c m g r`,
measures `l n p f i`, and `-p` values `none, 0, -1, -3, 2, 4` are
byte-identical within each target and measure (md5). The outputs are
non-trivial (for example 80, 78, 32, 77 and 205 lines for `-el -d110`).

---

## 6. Evaluation measures

### 6.1 Listing

`$FPG -!` prints 28 lines. Two are under "frequent item sets" (`x`, `b`)
and 26 under "association rules", one per `reinfo[]` entry
(`ruleval.c:435-488`, printed by `re_help`, `ruleval.c:546-555`, as
`"  %c   %-48s"` + `(+)`/`(-)`/nothing). This gives 27 distinct measures
(26 ruleval ids 0-25 plus `FPG_LDRATIO`) and 26 letters, one for each of
`a`-`z` (`remap[]`, `ruleval.c:491-518`). From the CLI, 25 non-trivial
measures are reachable: `RE_IMBAL` is shadowed by `-eb` (B1).

The direction `dir` controls the test. (+) means the value must meet or
exceed `thresh`, and (-) means it must not exceed it. `thresh` is always
`-d`/100 (default `-d10`, so 0.1).

### 6.2 Measure table

`L = s·n/(b·h)` (lift), `V = b·(n-h)/((b-s)·n)` (conviction),
`K = ½·s·(1/b + 1/h)` (Kulczynski), `R = s·(n-b)/(b·(h-s))`. All
products and quotients are in `double`, with operands converted from
`SUPP` first. `Q(x)` = `chi2cdfQ(x, 1)` = `GammaQ(0.5, x/2)`.

| Letter | Id | Name (CLI text) | Formula as coded | Dir | Degenerate cases / notes |
| --- | --- | --- | --- | --- | --- |
| `x` | 0 `RE_NONE` | no measure | `0` | 0 | No filtering. `prune` is reset (`fpgrowth.c:2025-2026`). |
| `o` | 1 `RE_SUPP` | rule support (original def.: body & head) | `s` (absolute) | + | The threshold is an absolute count ×100 (`-d300` means `s >= 3`, **confirmed**). |
| `c` | 2 `RE_CONF` | rule confidence | `s/b` | + | `b <= 0` gives 0. Redundant for rules, useful for sets. |
| `d` | 3 `RE_CONFDIFF` | absolute confidence difference to prior | `\|s/b − h/n\|` | + | `b <= 0` or `n <= 0` gives 0. |
| `l` | 4 `RE_LIFT` | lift value | `(s·n)/(b·h)` | + | `b <= 0` or `h <= 0` gives 0. An empty body gives exactly 1. |
| `a` | 5 `RE_LIFTDIFF` | absolute difference of lift value to 1 | `\|(s·n)/(b·h) − 1\|` | + | Same guards, which give 0. |
| `q` | 6 `RE_LIFTQUOT` | difference of lift quotient to 1 | `1 − (L > 1 ? 1/L : L)` | + | Same guards, which give 0. |
| `v` | 7 `RE_CVCT` | conviction | `(b·(n−h))/((b−s)·n)` | + | `n <= 0` **or `b <= s` (conf = 1) gives 0** (B4). |
| `e` | 8 `RE_CVCTDIFF` | absolute difference of conviction to 1 | `\|V − 1\|` | + | `n <= 0` gives 0; `b <= s` gives +inf. |
| `r` | 9 `RE_CVCTQUOT` | difference of conviction quotient to 1 | `1 − (V > 1 ? 1/V : V)` | + | `n <= 0` gives 0; `b <= s` gives +inf, although the measure is otherwise in [0,1]. |
| `b` | 10 `RE_IMBAL` | imbalance ratio | `(double)abs(h−b) / (double)(b+h−s)` (int arithmetic for the sums) | + | Denominator `<= 0` gives +inf. **Unreachable from the CLI** (B1). |
| `k` | 11 `RE_KULC` | Kulczynski measure | `(0.5·s)·(1/b + 1/h)` | + | `b <= 0` or `h <= 0` gives +inf. |
| `w` | 12 `RE_KULCDIFF` | difference of Kulczynski measure to 0.5 | `\|s·(1/b + 1/h) − 1\|` = **2·\|K − ½\|** | + | Same guards, which give +inf. The value is twice the named difference (N3). |
| `u` | 13 `RE_CPROB` | conditional probability ratio | `R` = P(h\|B)/P(h\|¬B) | + | `s <= 0`, `b <= 0` or `n <= b` (incl. empty body) gives 0; `h <= s` gives +inf. |
| `j` | 14 `RE_IMPORT` | importance | `log(R)/LN_2` | + | Same guards (0 / +inf); `R <= 0` gives 0. Uses `log()/LN_2`, not `log2()`. |
| `z` | 15 `RE_CERT` | certainty factor | `p=h/n; δ=s/b−p; δ/(δ>=0 ? 1−p : p)` | + | `b <= 0` or `n <= 0` gives 0. **`h = n` gives 0/0 = NaN** (B5). |
| `n` | 16 `RE_CHI2` | normalized χ² measure | `(h·b − s·n)² / (h·(n−h)·b·(n−b))` = χ²/n | + | `h <= 0`, `h >= n`, `b <= 0` or `b >= n` (incl. empty body) gives 0. The denominator is evaluated left to right. |
| `p` | 17 `RE_CHI2PVAL` | p-value from (unnormalized) χ² | `Q(n · chi2)` | − | Degenerate cases give `Q(0)` = 1. |
| `y` | 18 `RE_YATES` | normalized χ² with Yates' correction | `(\|h·b − s·n\| − 0.5·n)² / (same denominator)` | + | Same guards. **No clamp at 0**: an independent table gives a positive value (B10). |
| `t` | 19 `RE_YATESPVAL` | p-value from Yates-corrected χ² | `Q(n · yates)` | − | Degenerate cases give 1. Independent tables give p < 1 (B10). |
| `i` | 20 `RE_INFO` | information difference to prior | `(ln n + Σ t·ln(t/(r·c))/n)/LN_2` over the 4 cells `(s;h,b) (b−s;n−h,b) (h−s;h,n−b) (n−h−b+s;n−h,n−b)` with `t > 0` | + | Mutual information in bits, with the same guards giving 0. Can be **slightly negative** (−3.2e−16) through cancellation (B7). |
| `g` | 21 `RE_INFOPVAL` | p-value from G statistic | `Q(2·LN_2·n · info)` | − | `GammaQ` returns 1 for `x <= 0` (release); the debug build asserts (B7). |
| `f` | 22 `RE_FETPROB` | Fisher's exact test (table probability) | `Σ P(X=x)` over `x` with `ln P(x) <= ln P(s)·(1−ε)` | − | Degenerate marginals give 1. The tie tolerance is too small (B6). |
| `h` | 23 `RE_FETCHI2` | Fisher's exact test (χ² measure) | two-sided over `\|x − E\| >= \|s − E\|`, `E = h·b/n`; uses `1 − Σ(less extreme)` if that range is smaller | − | Degenerate cases give 1. The complement form can give negative or garbage values for large tables (B9). |
| `m` | 24 `RE_FETINFO` | Fisher's exact test (information gain) | `Σ P(x)` over `x` with `info(x) >= info(s)·(1−ε)` | − | Degenerate cases give 1. Tie and negative-info problems (B6, B7). |
| `s` | 25 `RE_FETSUPP` | Fisher's exact test (support) | one-sided upper tail `P(X >= s)`; uses `1 − P(X < s)` when `s <= b'−s` | − | Degenerate cases give 1. The complement can go negative (B9). |
| `b` | `FPG_LDRATIO` (26) | binary logarithm of support quotient (item sets) | `log2(s/n) − Σ_i log2(f_i/n)` (`isr_logrto`) | + | `\|S\| <= 1` gives 0. Not applied to rules (B1). |

Fisher tests share a common setup (`ruleval.c:271-285`, repeated in each
function). If `rest = n−h−b < 0`, both margins are complemented
(`s -= |rest|`, `b = n−b`, `h = n−h`). If `h < b`, `b` and `h` are
swapped. Then `X ∈ [0, b]`, and

`ln P(X=x) = com − lnΓ(b−x+1) − lnΓ(h−x+1) − lnΓ(x+1) − lnΓ(rest+x+1)`,

where `com = lnΓ(h+1)+lnΓ(b+1)+lnΓ(n−h+1)+lnΓ(n−b+1)−lnΓ(n+1)`. The cost
is `O(min(b,h,n−b,n−h))` `logGamma` (or `re_info`, for `m`) calls per
rule. The port must keep this algorithm, including the transformed
arguments: `re_fetinfo` calls `re_info` with the transformed `s, b, h`,
which is mathematically equivalent but not bitwise.

### 6.3 Special functions (`gamma.c`, `chi2.c`)

- `logGamma(n)` (`gamma.c:105-127`) has three cases:
  - integer `n < 171` (the constant `171 + 4ε` rounds to 171.0) returns
    `logfs[n−1]`, where `logfs[i] = log(i!)` and `i!` is built by
    **repeated double multiplication** (`gamma.c:64-69`). It is not
    correctly rounded: `logGamma(171)` = 706.57306224578724 vs libm
    `lgamma` …747.
  - half-integer `n < 171` uses `loghs` (B11).
  - every other `n`, including all `n >= 171`, uses the Lanczos
    approximation with g=7 and 9 terms. Fisher tests reach it as soon as
    a count exceeds 169.

  The tables are filled lazily on first use (`gamma.c:110`), which is not
  thread-safe. **Do not use `math.Lgamma`**; replicate the tables and
  the Lanczos sum.
- `GammaQ(a, x)` (`gamma.c:250-256`): `x <= 0` gives 1. For `x < a+1` it
  is `1 − series·exp(a·ln x − x − lnΓ(a))` (a series of at most 1024
  terms, stopped at `|t| < |sum|·ε`). Otherwise it uses the Lentz
  continued fraction `cfrac` (TINY = ε³, at most 1024 iterations, stopped
  at `|e−1| < ε`). It asserts `x >= 0`.
- `chi2cdfQ(x, df) = GammaQ(df/2, x/2)` (`chi2.c:53-57`). Here `df = 1`
  always.

---

## 7. Suspected bugs and undefined behavior

| # | Status | Location | Finding |
| --- | --- | --- | --- |
| B1 | confirmed | `fpgrowth.c:2436`; `ruleval.c:456, 493` | Letter `b` is overloaded. `-eb` always selects `FPG_LDRATIO`, and the imbalance ratio (6.21 addition) is unreachable. With `-tr -eb`, no measure is installed in the tree (`fpgrowth.c:2069`), and `isr_reprule` ignores `rep->evalfn`, so `-eb` is silently ignored. `$FPG -tr -s20 -c50 -eb -d1000000 -v' %e' t1.tab` prints the same 33 rules as without `-eb`, each with `%e` = 0. |
| B2 | confirmed | `fpgrowth.c:2051-2070`; `istree.c:742-743, 1376-1451` | `-p` has no effect. `ist_filter` runs before `ist_seteval`, and positive values are consumed only by `ist_commit`, which fpgrowth never calls. The documented "backward pruning for closed/maximal" does not happen. Reproducer in section 5. |
| B3 | confirmed | `istree.c:2036-2041` | Head-only appearance violated. When a head-only item is not the deepest item of the set, the loop still forms rules whose head is another item, so the head-only item lands in the body. With `printf 'both\na head\n' > app.txt`, `$FPG -tr -s20 -c50 -R app.txt t1.tab` prints `c <- a d`, `b <- a d`, `c <- a b d`, `b <- a c d`, `b <- a c`. `ist_rule` (library path) checks `ist->hdonly` correctly (`istree.c:1844-1847`). |
| B4 | confirmed | `ruleval.c:99` | Conviction returns 0 for confidence 1 (it should be +inf, as `e`/`r` return). Rules with 100% confidence fail any positive `-ev` threshold. `b <- a c d` (2/2/7, n=10) gives `v=0`, `e=inf`. |
| B5 | confirmed | `ruleval.c:183-184`; `istree.c:2027, 2051` vs `1918`; `istree.c:587-589` | The certainty factor is 0/0 = NaN when `h = n`. Rule mode uses *reject-if-less*, so NaN rules are reported for every `-d`: with `t2.tab`, `$FPG -tr -s20 -c50 -ez -v' %e' t2.tab` prints `z <- d nan` (default `-d10`). Set mode uses *accept-if->=*, so NaN sets are dropped. Under `-am`/`-an`, NaN poisons only when it is the first value, which depends on `-q`: with `-q2` all sets containing `z` are dropped, and with `-q-2` they are kept under `-ax/-am/-an` but dropped under `-aa`. |
| B6 | confirmed | `ruleval.c:291, 300, 383-385` | The Fisher tie tolerance is one relative ε of the *result* (log-probability or info), while the rounding error scales with the summed `lnΓ` terms. Tables with exactly equal probability or info are often excluded, so the p-values are too small (anti-conservative). Against exact rational/50-digit evaluation over all 17,550 tables with `n <= 24`, `f` is wrong in 348 cases and `m` in 196 (C) / 191 (Go port). Example: `f(1,2,5,7)` = 0.5238 where exact is 1.0, and `m(1,2,3,6)` = 0.4 where exact is 1.0. `h` and `s` have 0 wrong cases. Which cases fail depends on libm and FMA. |
| B7 | confirmed | `ruleval.c:251, 258-259, 383`; `gamma.c:252` | `re_info` returns tiny negative values for independent tables. `printf 'A B\nA\nB\nB\nC\nC\n' > neg.tab; $FPG -tr -s1 -c0 -ei -d-100000 -v' %17e' neg.tab` gives `B <- A -3.2034265038149176e-16`. Consequences: (a) `re_infopval` passes `x < 0` to `GammaQ`, which **aborts the debug build** (`$FPGDBG -tr -s1 -c0 -eg -d100 neg.tab` gives `Assertion failed: ((n > 0) && (x >= 0)), function GammaQ`), while release returns 1; (b) in `re_fetinfo`, a negative observed info makes `cut > info`, so the observed table can exclude itself. |
| B8 | confirmed | `report.c:1619` | `-S` is checked against `s` (body ∪ head) even under the default body-support definition. `$FPG -tr -s20 -S50 -c50 t1.tab` still reports `d <- c` with body support 70%, and drops the empty-body rules (`s` = 70%). |
| B9 | confirmed | `ruleval.c:336-341, 417-422` | `re_fetchi2` and `re_fetsupp` compute `1 − Σ` for small p-values, which causes catastrophic cancellation for large tables. The harness produced negative p-values: `s(7969403, 8875339, 7982035, 8893601)` gives −2.0e−8 (clang FMA build) and +9.8e−9 (no FMA). A negative p passes any (−) threshold and prints as negative. |
| B10 | confirmed | `ruleval.c:217-218` | Yates' correction is not clamped: it computes `(\|ad−bc\| − n/2)²` rather than `max(0, …)²`. For the independent table `s=2, b=4, h=5, n=10` (`printf 'A B\nA B\nA\nA\nB\nB\nB\nC\nC\nC\n'`), `-ey` gives 0.0417 and `-et` gives p = 0.519, while χ² gives 0 and p = 1. |
| B11 | confirmed (latent) | `gamma.c:72-75, 114-115` | `loghs[170]` is never initialized (loop `++i < MAXFACT`), so `logGamma(170.5)` returns 0. fpgrowth calls `logGamma` only with integers and 0.5, so it is unreachable from the CLI. |
| B12 | hypothesis (latent) | `ruleval.c:532, 540` | The `re_function`/`re_dir` asserts allow `id == RE_FNCNT` (it should be `<`), which reads past `reinfo[]`. Callers sanitize (`istree.c:1680-1684`). |
| B13 | hypothesis | `ruleval.c:132`, `fpgrowth.c` (SUPP=int) | `b + h − s` and the total weight are summed in `int`. With weights above 2³¹ this is signed overflow (UB). |
| B14 | hypothesis (library path only) | `istree.c:1711-1712` vs `isets`; `istree.c:1873` vs `2027` | `emptyset()` accepts the empty set for (−) measures when `0 >= −thresh`, while `isets` requires `−1 >= −thresh`. `ist_rule` rejects NaN where `r4set` accepts it. Size-ordered output (`order != 0`) would therefore differ from the CLI order in content, not just in order. |
| N1 | doc mismatch | HTML doc | `-as` (split aggregation) is documented but not implemented (`$FPG -ts -el -as` prints `invalid aggregation mode 's'`). The G statistic is documented as using "the support of the antecedent", but the code uses `n`. The Fisher info and support variants are documented as "no larger than observed", but the code sums tables with **larger or equal** info and the **upper** tail `X >= s`. The `-p` semantics are contradictory (section 5). The imbalance ratio and Kulczynski measures are not in the HTML. |
| N2 | doc mismatch | `fpgrowth.c:2126-2127` | The help text says rules "enforce algorithm variant 'd'"; the rule path is the separate `fpg_tree`. |
| N3 | naming | `ruleval.c:150` | `w` is `2·\|K−½\|` (range [0,1]), not `\|K−½\|`. For example `d <- ` (7/10/7) has K = 0.85 and `w` = 0.7. |

---

## 8. Numerics: what the port can and cannot reproduce exactly

### 8.1 Experiments (session scratchpad `rules-eval/`, not committed)

Harness `harness.c` links pristine copies of `ruleval.c`, `gamma.c` and
`chi2.c`. It evaluates all 26 ruleval functions as hex floats on:

- all 17,550 valid `(s, b, h, n)` with `n <= 24`;
- 127,008 pseudo-random tuples (xorshift64, `n` up to 5·10⁷, Fisher cases
  capped at 20,000 tables);
- plus `logGamma(k)`, `logGamma(k+½)` for `k <= 400`, and `chi2cdfQ(x,1)`
  for `x = 0, 0.0125, …, 25`.

A Go port (`goport/`, products wrapped in `float64()`) generates the same
tuples. The results:

| Comparison | Result |
| --- | --- |
| clang `-O3` (contraction on, as `$FPG`) vs `-ffp-contract=off` | 136,477 of 146,959 lines differ. The differing measures are `w i g p t f h m s` and `lg`/`q`. Differences visible at 6 significant digits (the default `%e` precision): `h` 14,424, `s` 2,308, `i` 861, `m` 223, `w` 108, `g` 49, `f` 20. `objdump` shows 26 `fmadd/fmsub` in `ruleval.o` and 15 in `gamma.o` of the reference build. |
| Go port vs C `-ffp-contract=off` | `o c d l a q v e r b k w u z n y` (no transcendental functions) are **bit-identical on all 144,558 tuples**. `j p t i g f h m s` differ: 5,281 / 14,606 / 12,603 / 4,312 / 18,524 / 59,550 / 35,381 / 59,493 / 48,140 values. At 6 digits: `h` 12,602, `s` 1,823, `i` 115, `m` 85, `f` 36. |
| Apple libm vs Go `math` (300,000 inputs each) | `log(int)` differs in 2,453 inputs, `log` on [0.5,1.5) in 16,849, `exp(−750y)` in 27,762, `exp(−30y)` in 29,013. |
| Go `math.Exp` on arm64 (assembly) vs Go's portable `exp` | 166 of 300,000 differ, so Go's own results depend on GOARCH. `math.Log` is assembly on amd64/s390x and pure Go on arm64. |
| Go compiler contraction (`-gcflags=-S`) | arm64 fuses `x*y+z`, `x*y−z` and `a*b−c*d` into FMA. amd64 with `GOAMD64=v3` fuses `x*y+z`. amd64 v1 fuses none of them. `float64(x*y)` prevents fusion on all architectures (Go spec, "explicit conversion rounds"). |

### 8.2 Porting risks and recommendations

1. **The reference build itself is not canonical.** Upstream's makefile
   uses `gcc -std=c99`. According to the GCC manual, real GCC disables
   contraction in ISO mode (`-ffp-contract=off`); this was not tested
   here. On macOS, `gcc` is clang, which contracts by default. Build the C reference for golden files with
   `-ffp-contract=off` (in `reference/build.sh`). Record the libm
   identity (Apple libm or glibc version) with every golden file that
   contains `j p t i g f h m s`, and `b` for item sets.
2. **Guard every product that feeds a `+`/`−` with `float64(...)`** in
   the Go port: `re_chi2`/`re_yates`, `re_kulcdiff`, `re_info` sums,
   `logGamma`, `series`, `cfrac` and `GammaQ` exponents. Keep C's
   left-to-right association, for example
   `h*(n−h)*b*(n−b)` as `((h·(n−h))·b)·(n−b)`, and `0.5*s` before the
   multiplication in `re_kulc`. With this, all non-transcendental
   measures are bit-exact (8.1).
3. **Transcendental functions.** Bit-exact output for `j p t i g f h m s
   b` and `logGamma` needs identical `log`/`exp`. Options:
   - (a) A private pure-Go `log`/`exp` (for example Go's portable
     `log.go`/`exp.go`, guarded against FMA). This makes Go results
     identical across GOARCH. Then link the C reference against a C
     transcription of the same routines (`-Dlog=…`) for differential
     tests. This gives bit-exact parity and is the recommended option.
   - (b) Tolerance-based comparison. It must tolerate threshold-boundary
     flips and the large tie-induced jumps of `f`/`m`/`h`/`s` (B6, B9),
     so it is weaker.
   - Use `math.Log(x)/ln2`, never `math.Log2`. Never use `math.Lgamma`.
4. **Keep the exact comparison forms.** `r4set` rejects with
   `dir·v < thresh`, `isets` accepts with `v·dir >= thresh`,
   min/max aggregation uses `<` and `>`, and `-i` uses a strict `>`. Go's
   IEEE comparisons then reproduce the C NaN/±inf behavior.
5. **Constants.** Go untyped-constant arithmetic is exact, while C folds
   in double. The expressions used here (`1−DBL_EPSILON`, `2·LN_2`,
   `ε³`, `0.5·LN_PI`, `171+4ε`) round to the same float64 either way.
   Keep each literal as written (C literals are correctly rounded, as Go
   constants are).
6. **Integer semantics.** `SUPP` is 32-bit `int`. Use `int32` for exact
   fidelity, or `int64` and document that only overflow cases (UB in C)
   differ. `abs()` in `re_imbal` is integer `abs`. `(SUPP)ceil/floor`
   in `re_fetchi2` truncate in-range values identically.
7. **Products beyond 2⁵³.** `s·n`, `h·b` and the `-i` test are rounded
   when the counts exceed about 9.5·10⁷. Fused and unfused builds then
   disagree even for `n`/`y`. Guarding (point 2) keeps Go identical to a
   no-FMA C build.
8. **Output formatting.** `%e` and the like go through `isr_numout`
   (6 significant digits by default; `nan`, `inf`, `-inf`; values below
   `DBL_MIN` print as `0`; more than 11 digits falls back to `sprintf`).
   That hides most ulp noise, but not the cases counted above.
9. **Order dependence.** `-ax` and NaN aggregation depend on item codes,
   so `tbg_recode`'s sort and tie-breaking must be exact (another audit).
10. **Performance.** The Fisher measures are O(table range) per rule, and
    `m` calls `re_info` per table. Keep the algorithm, and consider a
    precomputed `lnΓ` table, which is legal only if it reproduces the
    same bits.

---

## 9. Proposed C → Go mapping

| C | Proposed Go component | Notes |
| --- | --- | --- |
| `ruleval.c` `re_*`, `reinfo[]`, `remap[]`, `re_getid`, `re_function`, `re_dir`, `re_help` | `internal/ruleval`: `type Measure uint8` (ids 0-25 + `LDRatio`), `type Func func(s, b, h, n int32) float64`, a descriptor table `{Func, Dir, Letter, Desc}`, `ByLetter(byte)`, `Help(w io.Writer)` | Keep the ids stable. `Help` must reproduce `"  %c   %-48s"` + `(+)`/`(-)` byte for byte, including the trailing spaces for `x`. |
| `gamma.c` `logGamma`, `init`, `series`, `cfrac`, `GammaQ` (and `GammaP`); `chi2.c` `chi2cdfQ` | `internal/specfun` (or `internal/mathx`) | `sync.Once` or a generated table for `facts/logfs/loghs`. Private `log`/`exp` (8.2 point 3). |
| `istree.c` `ISTREE`, `ISTNODE`, `ist_create`, `children`/`child`, `getsupp`, `ist_addchn/down/up/setsupp` | `internal/istree` | Node layout: pure array vs id-map (`offset < 0`), `SKIP` sign bit, `HDONLY` bit. Consider explicit fields instead of bit tricks, but keep the identical traversal order. |
| `istree.c` `evaluate` | `istree.(*Tree).evaluate(node, idx)` | Aggregation `First/Min/Max/Avg` with the exact C comparison forms. |
| `istree.c` `isets`, `rules`, `r4set`, `ist_report` | `istree.(*Tree).Report(rep, target)` | Pre-order sets, post-order rules, and head order deepest → top. B3 is decided here. |
| `istree.c` `ist_clomax`, `clear` | `istree.(*Tree).CloMax(target, safe)` | Shared with the closed/maximal audit. |
| `istree.c` `ist_filter`, `ist_seteval`, `ist_setsize`, `ist_init` | `istree` setters | `Filter` is behaviorally a no-op in fpgrowth (B2). Port it only if the call order is kept. |
| `istree.c` `ist_iset`, `ist_rule`, `emptyset` | `istree` iterators (optional) | Needed only for a size-ordered library API. Beware of B14. |
| `fpgrowth.c` `rec_tree`, `fpg_tree` | `internal/fpgrowth` rule/eval search | Fills the tree from the conditional FP-tree. |
| `fpgrowth.c` `fpg_create/data/report/mine` | `internal/fpgrowth` miner config | Parameter derivation from 1.2, including both epsilons. |
| `report.c` `isr_reprule`, `isr_rinfo`, `isr_logrto` | `internal/report` | `-S` on body ∪ head (B8). `rep->evalfn` is ignored for rules (B1). |
| `fpgrowth.c` `main` (`-e -a -d -i -p -o -c`), `help` | `cmd/fpgrowth` | The `'b'` intercept, the `/100` threshold, and the error texts `invalid evaluation measure '%c'` and `invalid aggregation mode '%c'`. |

Fidelity decisions for `docs/differences.md`: B1-B10 are observable
upstream behavior. A faithful port reproduces them by default. Any fix
(for example a correct head-only check, conviction = +inf, a Yates
clamp, a tie tolerance, executing `-p`) must be an explicit, documented,
tested deviation, ideally behind an option.

---

## 10. Reproducer data

`t1.tab` (10 transactions):

```
a b c
a d e
b c d
a b c d
b c
a b d
d e
a b c d
c d e
a b c
```

`t2.tab` is `t1.tab` with ` z` appended to every line. `r1.tab` is 300
random transactions over `a`-`l`, generated with Python
`random.seed(7)`. The item probabilities are `.6 .55 .5 .45 .4 .35 .3
.25 .2 .15 .1 .05`; `k` is added with p=.5 when `a` is present, `l` is
added with p=.6 when `b` and `c` are present, and an empty transaction
becomes `a`. Typical invocations:

```
$FPG -tr -s20 -c50 -v" (%a %b %h %s %x %y %c %l %e)" t1.tab -
$FPG -ts -s20 -el -a{x,m,n,a} -d0 -v" (%a %e)" t1.tab -
$FPG -tr -s20 -c50 -e<letter> -d-1000000000 -v" %a/%b/%h=%17e" t1.tab -
```

Note: in zsh, `-v""` collapses to `-v`, and the next argument is then
taken as the format. Use `-v " "` or a non-empty format.
