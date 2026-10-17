# M5 investigation: closed/maximal item sets with a maximum size

Status: resolved (Stage 3). Deviation M5 (`docs/differences.md`), upstream
bug B5 (`docs/upstream/fpgrowth-core.md` §6).

## 1. Summary

Upstream limits the closed/maximal search to `zmax+1` items and lets the
closed/maximal repository decide closedness. That reports non-closed and
non-maximal sets (B5). Commit 9397bab fixed this by removing the depth
limit for closed/maximal targets ("full exploration"): exact, but on dense
data with a small `-n` it explores the whole closed lattice only to report
its bottom layers (critic: `-tc -s2 -n2` on 2000×40, density 0.6: C 0.22 s
/ 22 MB, Go 31.6 s / 422 MB).

A first attempt to keep the depth limit and only collect perfect
extensions at the boundary failed on oracle seed 678. This report
minimizes that case, identifies the first violated invariant, and replaces
full exploration by a **size-limited search plus a mathematically complete
one-item extension check** against all items of the database. It is
exact (verified against the brute-force oracle, §6) and on the critic's
case runs in 21 ms / 14 MiB instead of 34 s / 673 MiB (C: 236 ms /
22 MiB; §7). It is now the only path for closed/maximal targets with `-n`
below the longest transaction.

## 2. The boundary-pex attempt and the minimized case

### 2.1 Recreated attempt

The attempt was recreated on a scratch branch of commit c16c2f5 (not
kept): `miner.xable2` is upstream's `isr_xable(rep, 2)` again
(`xmax = zmax+1`), and where that suppresses the projection of a
closed/maximal search the head's conditional supports are still computed
and the items with full support are added as perfect extensions
(`rec_simple`, `rec_smp16` including packed items, `rec_cmplx`,
`rec_topdn`). Nothing else changed.

Over the first 1000 `genCase` seeds of `TestOracle` it fails for seeds 25,
138, 678, 755 and 822 (closed and maximal). 678 and 822 fail with
`-As -l0`, `-Ac -l0`, `-Ac -j -l0`, `-Ad -l0` and `-At`; the others only
with `-At` (which has no chain shortcut). Seed 678 is
`-tc -s-3 -n2 -q-1 -x` on 16 weighted transactions (the
`B5 closed zmax -x complex` row of `TestDeviations`).

### 2.2 Delta debugging

Greedy 1-minimal delta debugging (drop transactions, drop single items,
decrement weights, lower `-s`, repeat until no step keeps the failure)
for `-tc -As -l0` gives

```
c d a   1
d b a c 1
c       1
a d     2         -tc -s-1 -n2 -q-1 -x   (also -tm)
```

and, forcing perfect-extension pruning on (no `-x`; one transaction `d 1`
more is needed to keep `d` from being a top-level perfect extension):

```
c d a   1
d b a c 1
c       1
d       1
a d     2         -tc -s-1 -n2 -q-1      (also -tm)
```

Item codes (`-q-1`, ascending frequency): b=0 (1), c=1 (3), a=2 (4),
d=3 (4, resp. 5). Closed/maximal search processes codes descending.

| | closed (`-tc`) | maximal (`-tm`) |
| --- | --- | --- |
| expected (oracle) | `c (3)`, `a d (4)` (+ `d (5)` without `-x`) | nothing (the only maximal set, `{a,b,c,d}`, has 4 items) |
| boundary-pex attempt | additionally `c b (1)` | `c b (1)` |

`{b,c}` is neither closed nor maximal: `{a,b,c,d}` has support 1.

### 2.3 Trace and repository state

Reporter calls of the attempt (`-tc`, with `-x`; indentation = depth,
`cm.max` = largest stored support of a superset of the prefix):

```
add d 4                                      prefix {d}
  add a 4                                    prefix {d,a}
    add c 2 ; report {d,a,c}:2  STORE        size 3 = xmax: no projection
    add b 1 ; report {d,a,b}:1  STORE        size 3 = xmax: no projection
  report {d,a}:4  STORE
  add c 2  -> REJECTED (stored {d,a,c}:2 ⊇ {d,c})
  add b 1  -> REJECTED (stored {d,a,b}:1 ⊇ {d,b})
  report {d}:4  REJECTED (stored {d,a}:4)
add a 4  -> REJECTED (stored {d,a}:4 ⊇ {a})
add c 3                                      prefix {c}
  add b 1   (cm.max = -1: no stored superset of {c,b})
  report {c,b}:1  STORE + REPORTED           <- wrong
  report {c}:3  STORE + REPORTED
add b 1  -> REJECTED (stored {d,a,b}:1)
```

Repository contents when `{c,b}` is decided: `{d,a,c}:2`, `{d,a,b}:1`,
`{d,a}:4`. The tree of prefix `{c}` holds the stored sets containing `c`
(`{d,a,c}`), pruned to items ≤ b: none contains `b`, so `cm_add` and
`cm_supp` find no superset (`-1`) and the set is stored and reported.

### 2.4 What went wrong (classification)

The three candidate explanations, checked against the trace:

- **Rejection by a non-genuine superset: no.** Every stored set is a real
  item set with its true support (`{d,a,c}`: 2, `{d,a,b}`: 1, `{d,a}`: 4),
  and every rejection is correct: `{d,c}`, `{d,b}`, `{a}` and `{d}` are not
  closed, and their subtrees contain no closed set.
- **Stored superset with a wrong (partial-closure) support: no.** Stored
  sets can lack perfect extensions beyond the boundary (`{d,a,b}` lacks
  `c`), but their supports are the supports of exactly the stored items.
- **A set wrongly accepted because its proving superset was never
  stored: yes.** `{c,b}` needs a stored superset with support ≥ 1. Its
  candidates are `{a,b,c}` (node `{a,c,b}`), `{b,c,d}` (node `{d,c,b}`)
  and `{a,b,c,d}` (node `{d,a,c,b}`):
  - `{d,a,c,b}` has 4 items, beyond `xmax = 3`: node `{d,a,c}` is not
    projected. `b` has conditional support 1 < 2 there, so it is not a
    perfect extension and boundary pex cannot help.
  - `{d,c,b}` lies in the subtree of `{d,c}`, which is pruned by the
    genuine stored superset `{d,a,c}:2`.
  - `{a,c,b}` lies in the subtree of `{a}`, pruned by `{d,a}:4`.

**First violated invariant.** FP-growth's closed/maximal repository is
correct because of the subtree-completeness invariant

> (I1) When a node is reported, every frequent superset in its subtree has
> been visited, and every closed (maximal) one has been stored.

The repository's pruning (`isr_add` rejects `X` because a stored `S ⊇ X`
has support ≥ supp(X)) relies on I1 for `S`'s node: the witnesses that `X`'s
subtree would have stored (here `{d,c,b}`) are supposed to be covered by
the closed sets stored from `S`'s subtree (here `clo({d,c,b}) =
{a,b,c,d}`, in the subtree of `{d,a,c}`). The first violation of I1 is at
`report {d,a,c}:2` (the 5th call above): its subtree contains the frequent
superset `{d,a,c,b}`, which the size limit never visits. From then on the
pruning of `{d,c}` (and later `{a}`) removes witnesses that nobody stores,
which violates the decision invariant

> (I2) When a set X is decided, the repository contains a superset S ⊋ X
> with supp(S) = supp(X) whenever X is not closed (for maximal: a
> frequent superset whenever X is not maximal),

first at `report {c,b}:1`. Perfect extensions at the boundary only add
items with *full* support to the stored set; the missing set has smaller
support than its parent, so no boundary-local computation can restore I1.

## 3. Correctness argument

Notation: items are codes after recoding (and item selection) of the
transaction bag; `E` is the set of all of them. supp is the weighted
support; smin ≥ 1. For a search node `N` (the prefix items added by
`isr_add`), `pex(N)` are the perfect extensions in force (inherited and
local), `N̂ = N ∪ pex(N)` is the set reported for `N`, and supp(N̂) =
supp(N).

### 3.1 One item suffices

X is closed iff supp(X ∪ {j}) < supp(X) for all j ∈ E \ X; X is maximal iff
supp(X ∪ {j}) < smin for all j ∈ E \ X. (If Y ⊋ X has supp(Y) = supp(X),
any j ∈ Y \ X has supp(X) ≥ supp(X ∪ {j}) ≥ supp(Y) = supp(X); likewise
for frequency.) With weights: transactions of weight 0 change no support;
removing them, supp(X ∪ {j}) = supp(X) iff j is in every transaction of
the cover of X.

### 3.2 Complete versus local checks

- A **complete** check evaluates the condition of 3.1 for every
  j ∈ E \ N̂, with supports computed from the database itself. Its result
  depends only on N̂ and the database, not on the traversal.
- A **local** check evaluates it only for the items it can see at the node:
  the items of the node's conditional database (the conditional FP-tree,
  i.e. the items below the node in the processing order, or the head
  union tail items), and leaves all other items to the repository
  ("a stored superset exists"). Its result depends on the traversal order,
  on which subtrees were pruned and on how deep the search went. Upstream
  with `xmax = zmax+1`, the boundary-pex attempt and "conditional supports
  at the boundary" are all local checks. §2 shows that the repository part
  is not a substitute for the missing items under a size limit.

Where the supports come from: for items *below* the last item of `N`,
the conditional supports computed for `N`'s projection are exact
(supp(N ∪ {j}) for every j kept, pruned as infrequent or found as a
perfect extension) — but only where the projection is built. Items
*above* it are never in `N`'s conditional tree: they were processed
earlier (descending loops) or eliminated at an ancestor. Ancestor
eliminations are harmless (infrequent there ⇒ infrequent with `N`; perfect
extension there ⇒ already in N̂). For the remaining above-items the only
traversal-side source is the repository, and §2 shows it is incomplete
under a size limit. A dedicated lookup is sound; we use one uniformly for
all items: `extChecker` computes the supports from the cover of N̂ (the
transactions containing it) in the bag after recoding, item selection
and the zmin filter (the filter only drops transactions with fewer than
zmin items, which contain no set of ≥ zmin items, so the supports of all
checked sets and their one-item extensions are unchanged). How the cover
and the supports are computed is described in §4; the argument depends
only on what is computed, not on how:

- The cover used is exactly the set of positive-weight transactions that
  contain N̂. It is computed as the cover of the prefix `N` (the items
  the reporter added): every perfect extension p of `N` has
  supp(N ∪ {p}) = supp(N), so the transactions of the cover of `N`
  without p have total weight 0 and are not stored; with only positive
  weights, cover(N) = cover(N̂). Zero-weight transactions change no
  support (3.1).
- Every item j ∈ E \ N̂ is decided: the vertical test visits all items
  (it stops early only at the first item rarer than supp(N̂), resp.
  smin, which cannot violate the condition, or at a violation); the
  closed scan keeps every item of the first cover transaction as a
  candidate (an item in every cover transaction is in the first one) and
  intersects the candidates with cover transactions until none is left,
  so a remaining candidate after all of them is in every one; the
  maximal scan sums the weight of every cover transaction per item and
  stops only at an item reaching smin. Intersection and sums do not
  depend on the order in which the cover transactions are visited, and
  intersecting with a transaction twice changes nothing, so the probe
  order of the closed scan (§4) does not affect the result.

### 3.3 Order facts used (all closed/maximal variants)

For closed/maximal targets upstream normalizes `-Ad` to `-As` and clears
`-j` (reordering), so every variant projects a head onto **lower codes
only** and processes the items of a level in **descending** code order:

- `-As` (`rec_simple`), `-Ac` (`rec_cmplx`): `dir = -1`.
- `-As -l` (`rec_smp16`): the packed head (codes below 16, lower than all
  other heads) is processed last, by the 16-items machine.
- 16-items machine (`rec_neg`, dir ≤ 0): bits `n-1 … 2`, then 1, then 0
  (bits map to ascending codes; `-Ac`'s `mine16` maps the projection's
  heads in ascending code order).
- `-At` (`rec_topdn`): always the root, the highest remaining id.

Reporting is post-order: a node is reported after its subtree. Perfect
extensions found at a node are removed from its projection (also by the
chain shortcut and the machine's `filter`/`single`); with `-x` there are
none except top-level and chain ones.

**Divergence lemma.** Let `M` be a node reported before node `N` is added
(or reported), not a descendant of `N`. Then `M` and `N` share a prefix `Q`
and continue with items `m > c` (`m` in `M`, `c` in `N`). `m ∉ N̂'` for
every node `N'` of `N`'s subtree: `m` is not in `Q`; it is in `Q`'s
conditional tree, so it is no perfect extension of `Q` or its ancestors;
all other items of `N'` and its perfect extensions are ≤ c < m.

### 3.4 Soundness of every repository decision (genuine stored sets)

Every stored set is `M̂` for some node `M`, with the true support
supp(M) (supports are exact conditional supports; perfect extensions have
full support). Hence:

- **Report rejection** (`cm_supp` ≥ supp(N), resp. ≥ 0 for maximal): a
  stored `S = M̂ ⊇ N` with supp(S) ≥ supp(N). If `M` is a descendant of
  `N`, its extra prefix item is in `N`'s conditional tree, hence not in N̂;
  otherwise the divergence lemma gives `m ∈ S \ N̂`. Either way S ⊄ N̂, and
  supp(N̂ ∪ S) = supp(N) (zero-weight argument), so N̂ is not closed (not
  maximal). Never a false rejection.
- **Add rejection** of `X` (stored `S ⊇ X`, supp(S) ≥ supp(X)): any closed
  (maximal) `Y` in `X`'s subtree has supp(Y ∪ S) = supp(Y), hence `S ⊆ Y`;
  but by the divergence lemma `S` contains an item no set of `X`'s subtree
  contains. So the pruned subtree holds no closed (maximal) set.
- **Head union tail** (maximal): a stored `S ⊇ prefix ∪ tail`; every set
  of the subtree is ⊆ prefix ∪ tail ∪ inherited pex, and `S` contains the
  divergence item `m`, which is not among them. No maximal set is lost.

So the repository never loses or wrongly rejects a set; its only defect
under a size limit is false *acceptance* (§2). The complete check removes
exactly those.

### 3.5 Reachability under the size limit

The search builds a node's projection (its local perfect extensions and
children) only if |N| + |inherited pex| < zmax (`miner.deeper`, also inside
the 16-items machine; `xable2` is the same test one level up). For a
closed (maximal) set `C` with |C| ≤ zmax, the canonical path from the root
adds, at node `P`, the largest item of `C \ P̂`. It is in `P`'s conditional
tree (frequent, not a perfect extension, below `P`'s last item by
induction), and `P̂ ⊊ C` gives |P| + |pex(P)| < |C| ≤ zmax, so `P` is
projected. At the final node `P̂ = C` holds: if `P` has local perfect
extensions, |P| + |inherited| < |C| ≤ zmax and they are computed; if not,
none are needed. No repository decision removes the path (3.4). Nodes
that are not projected have no reportable descendant (all of them have
more than zmax items); the node itself is reportable only without further
perfect extensions, which the complete check verifies.

**Uniqueness.** Two different nodes never produce the same N̂ (divergence
lemma, or a child's extra item, which is in the parent's tree and not in
its perfect extensions), so no set is reported twice.

**Theorem.** With the size-limited search and the complete check, the
reported sets are exactly the closed (maximal) sets with zmin ≤ size ≤
zmax that pass the support, border and evaluation filters: every reported
N̂ passed the complete check (and the filters), and every such set is
reached, accepted by the repository (3.4) and passes the check.

### 3.6 Variant notes

- **Ordering, post-order:** used only through the divergence lemma; it
  holds for every variant (3.3). The complete check itself is
  order-independent.
- **Perfect-extension pruning (`-x`):** with `-x` the equal-support items
  below a node stay in its tree; the check catches them like any other
  item. Pex never change supports, so stored sets stay genuine.
- **16-items machine:** upstream's machine ignores `xmax` and mines its
  items completely; here it is gated by `deeper` per item (`clear` instead
  of `project`), which is sound by 3.5. Pex from the machine
  (`filter`, `single`, `mine`'s one-transaction case) have full support.
- **Head union tail (`-u` off):** sound (3.4); it only prunes.
- **Projections:** all variants project onto lower codes; `-Ac -j`
  (reordering) is disabled for closed/maximal by `fpg_create`.
- **Without a relevant limit** (zmax ≥ the longest transaction) the search
  is unlimited, I1 holds, and the repository alone is exact; no check is
  installed.

## 4. Implementation

- `internal/fpg/extcheck.go`: `extChecker`, built once per run from the
  bag. It keeps
  - the items of every transaction of positive weight (horizontal form,
    ascending codes per transaction, in bag order), and
  - the bit set of transactions of every item (vertical form; word w
    holds transactions 64w..64w+63) in compact form: the indices of the
    item's non-zero words and, for a *sparse* item, their values; a
    *dense* item has a full row of words instead of the values, for
    direct access. Every item is dense if the full rows need at most 4×
    the memory of the horizontal items (all benchmark datasets except
    `sparse-large`); otherwise the items whose non-zero words fill at
    least half of the words are dense.
- Covers. The cover of a reported set is the cover of the reporter's
  prefix (§3.2). `checkPath` keeps the covers of the prefix's leading
  items per depth (a depth-first search changes only the end of the
  prefix) and computes each new level with one AND of the level before
  and the new item's bit set: a dense item costs one lookup per
  non-zero cover word; a sparse item merges the two word lists if their
  lengths are within a factor of 4, else visits the shorter one and
  searches the other exponentially. `check` (an empty prefix: the set
  consists of top-level perfect extensions) ANDs the set's bit sets,
  starting from the item with the fewest non-zero words.
- Decision. Per check the cheaper of two methods is estimated and used:
  - scanning the cover transactions (closed: the candidates are the
    items of the first cover transaction; they are intersected first
    with the first transactions of 8 cover words spread over the cover,
    because bag order is lexicographic and neighbouring transactions
    share most items, then with all cover transactions in order, until
    no candidate is left; maximal: count supp(X ∪ {j}) until an item
    reaches smin);
  - testing every item against the cover (closed: subset test, items in
    descending frequency, stop at the first item rarer than the set;
    maximal: weighted popcount via weight bit planes).
  - Candidate probes (closed scan, `probeCand`). Before the candidates
    are copied from the first cover transaction t, up to 4 of its items
    outside the set are tested against the cover (`covers`); one in
    every cover transaction proves the set not closed. This is exact
    because the cover holds exactly the positive-weight transactions
    that contain the set (weight 0 is dropped when the checker is
    built), so supp(X ∪ {j}) = supp(X) iff j is in all of them; a failed
    probe decides nothing and the full scan follows. The probes run
    only if t has more than 4 items per non-zero cover word, so they
    cost at most about as much as the copy. Without them a long chain
    with a small maximum size was quadratic: with `-At -tc -n1` on 17
    identical transactions of n items each of the n item checks copied
    all n items (22.0 s at n = 100,000, now 0.30 s; C 0.24 s, same
    machine). For a single-transaction cover the first item of t
    outside the set always succeeds, so a check reads at most |X| + 1
    items of t; the maximal scan already stopped at the first frequent
    extension and is unchanged.
- `internal/fpg/mine.go`: the checker is installed when the target is
  closed/maximal and zmax < `Bag.MaxSize()` (otherwise the search is
  unlimited as before); `xable2`/`deeper` are the size gates. The miner
  calls `checkPath(rep.Items(), set, supp)`.
- `internal/fpg/{simple,complex,topdown,fim16}.go`: projections gated by
  `deeper` (the machine `clear`s an item instead of projecting it).
- `internal/report/report.go`: `Reporter.SetCloMaxCheck`, called in
  `Report` after the repository and the support/size gates, before the
  border, evaluation and sink.
- The full-exploration path with a limit and the temporary switch used
  for the comparison below were removed. Test hooks (a replacement check,
  the form of the bit sets: automatic, all sparse, or mixed) live in an
  unexported `Miner` field.

History of the checker (all versions exact; only memory and speed
differ):

| version | cover | vertical form |
| --- | --- | --- |
| `3e5d372` | per check from scratch (AND of the set's bit sets) | full rows for every item if they need ≤ 4× the horizontal items **or ≤ 64 MiB**, else per-item transaction lists (the rarest item's list filtered by containment) |
| `671474e` | incremental per prefix depth (`checkPath`, bit sets only) | as above |
| Stage 3 fix S3-1 (this version) | incremental for every form; closed scan with spread probes | compact rows (sparse items: non-zero words only), no absolute floor; transaction lists removed |

The 64 MiB floor of the first two versions was a memory-for-speed
trade-off: on `sparse-large` (2,592 frequent items at `-s0.05`, 3,115
words) it admitted full rows of 64.6 MB, 9.1× the 7.1 MB of horizontal
items, because the transaction lists were slower there — measured with
incremental covers for the lists as well (in-process CLI benchmark,
n = 5–6, load 5–40): +21 to +31 % time for `-tc`/`-tm` `-s0.05 -n2/-n3`
at −80 MB allocated. Compact rows remove the trade-off: they bound the
vertical form relative to the input (§5) and are faster than both
(§7).

## 5. Memory and cost of the check

Memory, with T distinct transactions of positive weight, W = ⌈T/64⌉
words, k items, O item occurrences (Σ |t|) and Z ≤ O non-zero words
(Σ over items):

| part | bytes |
| --- | --- |
| horizontal items, offsets, weights | 4·O + 16·T |
| non-zero word index (every item) | 4·Z |
| values of sparse items | 8 per non-zero word of a sparse item |
| full rows of dense items | 8·W per dense item; ≤ 16 per non-zero word (2·nz ≥ W), or ≤ 16·O in all when every item is dense (the 4× rule) |
| vertical form in all (index + values + rows) | **≤ 20·O** (5× the horizontal items) |
| weight planes (only if some distinct transaction has weight > 1) | 8·W·⌈log₂(max weight + 1)⌉ |
| per-depth path cache (`checkPath`) | ≤ 12·W per level (12 bytes per non-zero cover word; level k ⊆ level k−1 ⊆ the first item's words), at most zmax levels (the prefix of a reported set has at most zmax items): ≤ 12·W·zmax |
| scratch covers of `check` | ≤ 2 · 12·W |
| per item: frequencies, order, stamps, counts, offsets | ≈ 44·k |

Measured on `sparse-large` at `-s0.05` (k = 2,592, T = 199,337,
W = 3,115, O = 1,765,336, Z = 924,374; the 4× rule fails, 62 dense
items, 5 weight planes from merged duplicate transactions): vertical
form 11.9 MB = 1.7× the 7.1 MB of horizontal items, instead of 64.6 MB
of full rows plus a 3.7 MB index before. Peak RSS of the CLI (§7):
126 MB, before 179–181 MB, the unlimited search 106 MB.

A check costs at most min(Σ |t| over the cover, #items × #non-zero cover
words) plus the cover update; closed checks usually stop after a few
transactions or items. It runs once per set that passes the repository
and the size/support gates, i.e. at most once per output candidate.
Full exploration's extra cost, by contrast, is the whole closed
(maximal) lattice above zmax, which is exponential on dense data.

## 6. Test evidence

All on the final code, re-run after fix S3-1 with the same counts
(`go test ./internal/fpg -run
'TestExtCheck|TestOracle$|TestDeviations|TestSeek' -v` with
`FPG_M5_CASES=10000 FPG_ORACLE_CASES=5000 FPG_M5_UNLIMITED=300`):

| test | what | result |
| --- | --- | --- |
| `TestExtCheckOracle` | 10,000 random datasets (dense: each transaction half to all of 1–12 items; sparse: 0–7 items; 1/10 with 17–20 items for the >16-items paths), unit/small/zero/large weights, `-s` absolute and relative, zmin 0–3, zmax zmin..zmin+3, in 1/6 of the cases 0–7, `-S`, `-x`, `-u`, `-q2 1 0 -1 -2`, borders, item selection in 1/4; closed and maximal × 11 variants (`-As -l0/3/16`, `-Ac` ± `-j` ± `-l0`, `-Ad -l0/3/16`, `-At`); seed mod 3 selects the bit set form: automatic, every item sparse, even items dense and odd ones sparse | **220,000 comparisons, 0 differences** |
| `TestOracle` (existing) | 5,000 datasets, all four targets × 11 variants | 220,000 comparisons, 0 differences |
| `TestExtCheckUnlimited` | 300 datasets beyond the oracle (20–40 items, 40–200 transactions): limited run (zmax 1–4) = unlimited run filtered by size, closed/maximal × 11 variants | 26,400 runs equal |
| `TestExtCheckBrute` | 300 datasets: every call of the check (all three bit set forms; `check`, `checkPath` and both the scan and the vertical path) against a brute-force single-item support scan computed from the **source transactions** (item names, selection applied; independent of the checker's storage and of the bag), plus the reporter's support | 6,600 runs, 0 mismatches |
| `TestExtCheckBits` | 12 datasets of 500–4,500 transactions over 40 items with skewed frequencies (word lists of very different lengths; weights incl. 0), all three forms: covers of random 1–3-item sets (`bitCover` and level by level in arbitrary order as `checkPath` builds them), `covers` and `suppWith` of every item, against a direct computation | pass |
| `TestSeek` | 2,000 random cases of the exponential search against a linear one | pass |
| `TestExtCheckRegressions` | minimized seed 678 (with/without `-x`, closed/maximal), original seed 678, the B5 reproducers, two-item and depth-2 "above" cases, all variants, all three bit set forms | complete check: 0 differences; local check fails on every case marked so (e.g. `above closed`: all 11 variants) |
| `TestExtCheckLocalSweep` | negative control: the same traversal with a **local** check (only items below the prefix, i.e. the conditional tree's items; repository for the rest) | 5,072 of 33,000 runs (508 of 1,500 datasets) differ from the oracle |
| `TestReporter_BoundaryPexNotEnough` (report) | minimized case on the reporter simulator: boundary pex alone reports `{b,c}`; with the complete check equal to brute force | pass |
| `TestReporter_PropertyClosedMaximalZmax` (report) | 1,600 random simulator runs without the chain shortcut, with the complete check | pass |

Also green: `gofmt`, `go vet ./...`, `staticcheck ./...`,
`go test -race ./...` (all packages, incl. CLI golden tests and the
fpg golden cases against the C reference), and
`cd reference && go test ./difftest/`.

The recreated boundary-pex attempt fails 5 of the first 1,000
`TestOracle` seeds (§2.1); the original full-exploration code passes them
all.

## 7. Benchmarks

Apple M1 Max, Go 1.27.2, macOS. C: `reference/build/native` (exact
upstream makefile flags). Data: dense data with independent items
(`dNxKdP`: N transactions, K items, each present with probability P),
sparse data `s20000x500` (20,000 transactions, 500 Zipf-like items,
exponential lengths, mean 8). CLI runs (6 each, output to a file, median
wall time / peak RSS from `/usr/bin/time -l`); "before" is commit c16c2f5
(full exploration), "after" the working tree. All three produce identical
sets (sorted comparison) in every configuration.

| config | before (full) | after (limited + check) | C native |
| --- | --- | --- | --- |
| `d2000x40d60 -tc -s2 -n2` (critic) | 34.09 s / 673 MiB | **21.2 ms / 14 MiB** | 236 ms / 22 MiB |
| `d2000x40d60 -tm -s2 -n2` | 27.81 s / 674 MiB | **19.5 ms / 13 MiB** | 163 ms / 19 MiB |
| `d1000x30d80 -tc -s10 -n2` | 33.05 s / 1757 MiB | **11.8 ms / 9 MiB** | 423 ms / 45 MiB |
| `d1000x30d80 -tm -s10 -n2` | 21.73 s / 1111 MiB | **11.4 ms / 10 MiB** | 199 ms / 26 MiB |
| `d2000x40d60 -tc -s2 -n1` | — | 12.9 ms / 14 MiB | 26.4 ms / 16 MiB |
| `d2000x40d60 -tc -s2 -n3` | ≈ 34 s (independent of n) | 59.0 ms / 14 MiB | 1.42 s / 41 MiB |
| `d2000x40d60 -tm -s2 -n3` | ≈ 28 s | 56.9 ms / 14 MiB | 1.01 s / 36 MiB |
| `d2000x40d60 -tc -s2 -n4` | ≈ 34 s | 299 ms / 14 MiB | 4.76 s / 135 MiB |
| `d5000x60d40 -tc -s5 -n3` | 378 ms / 21 MiB | 201 ms / 21 MiB | 264 ms / 44 MiB |
| `d5000x60d40 -tm -s5 -n3` | 380 ms / 21 MiB | 297 ms / 20 MiB | 266 ms / 44 MiB |
| `s20000x500 -tc -s0.05 -n2` | 111 ms / 21 MiB | 72.5 ms / 20 MiB | 60.3 ms / 21 MiB |
| `s20000x500 -tm -s0.05 -n2` | 81.0 ms / 20 MiB | 73.7 ms / 22 MiB | 48.9 ms / 21 MiB |
| `s20000x500 -tc -s0.05 -n3` | 108 ms / 19 MiB | 152 ms / 20 MiB | 63.8 ms / 21 MiB |
| `s20000x500 -tm -s0.05 -n3` | 83.2 ms / 19 MiB | 139 ms / 22 MiB | 51.8 ms / 21 MiB |
| `s20000x500 -tc -s0.05` (no `-n`, same code path) | 122 ms / 20 MiB | 138 ms / 20 MiB | 70.0 ms / 21 MiB |
| `d1000x30d80 -tc -s30` (no `-n`, same code path) | 273 ms / 13 MiB | 274 ms / 13 MiB | 171 ms / 7 MiB |

The two runs without `-n` execute identical code before and after; their
spread (122 vs 138 ms) is run-to-run noise of the CLI measurement.

In-process (`go test ./internal/fpg -bench CloMaxLimited -benchmem
-count 6`, count-only, Go-generated data of the same shape; full = the
removed switch), benchstat:

```
name                                    old time/op    new time/op    delta
CloMaxLimited/dense2000x40d60/tc/n2-10     27.9s ± 0%      0.0s ± 1%  -99.96%  (p=0.008 n=5+5)
CloMaxLimited/dense2000x40d60/tm/n2-10     23.0s ± 1%      0.0s ± 1%  -99.96%  (p=0.004 n=5+6)
CloMaxLimited/dense1000x30d80/tc/n2-10     38.5s ± 3%      0.0s ± 1%  -99.99%  (p=0.004 n=6+5)
CloMaxLimited/dense1000x30d80/tm/n2-10     24.2s ± 1%      0.0s ± 1%  -99.98%  (p=0.002 n=6+6)

name                                    old alloc/op   new alloc/op   delta
CloMaxLimited/dense2000x40d60/tc/n2-10     796MB ± 0%       9MB ± 0%  -98.88%  (p=0.002 n=6+6)
CloMaxLimited/dense2000x40d60/tm/n2-10     796MB ± 0%       9MB ± 0%  -98.88%  (p=0.008 n=5+5)
CloMaxLimited/dense1000x30d80/tc/n2-10    2.73GB ± 0%    0.00GB ± 0%  -99.83%  (p=0.002 n=6+6)
CloMaxLimited/dense1000x30d80/tm/n2-10    2.21GB ± 0%    0.00GB ± 0%  -99.80%  (p=0.002 n=6+6)
```

(new means: 10.4 / 10.3 / 4.4 / 4.3 ms; 8.9 / 8.9 / 4.5 / 4.5 MB/op.)
Limited path, all limits (mean time/op, alloc/op): `dense2000x40d60`
n1 4.1 ms, n2 10.4 ms, n3 48.9 ms (8.9 MB); `dense1000x30d80` n1 2.1 ms,
n2 4.4 ms, n3 15.0 ms (4.5 MB); maximal within 4 % of closed.

Earlier versions of the checker, for the record: scanning the rarest
item's transactions only (no bit sets) made `d5000x60d40 -tm -n3` 3.0 s
and `s20000x500 -tm -n3` 0.27 s; plain bit sets without the cover-word
index made the latter 15 s. The hybrid of `3e5d372` fixed both.

The `dNxKdP` data of the table above was generated in process before the
datasets were pinned; `s20000x500` is not reproducible. Since Stage 3
fix S3-2 the dense datasets are part of `reference/bench/gen` (pinned
SHA-256, seeds 1, 2, 3; `reference/bench/README.md`), the configurations
are in `reference/bench/configs.txt` (`m5-*`, `large-[cm]-n*`):

```bash
cd reference
bench/compare.sh -n 5 -m devnull '^(m5-|large-)'
```

### 7.1 Cost on large sparse data (Stage 3 fix S3-1)

`sparse-large` (200,000 transactions, 20,000 items, average length
11.1; at `-s0.05` 2,592 frequent items) is where the check costs most:
the sets are many and their covers large. CLI, output to `/dev/null`,
`bench/compare.sh -n 5 -m devnull` medians (wall ms / peak RSS MB); C
reports the same counts here. "671474e" is the checker before the fix,
"fix" the working tree:

| config | sets | C | Go `671474e` | Go fix |
| --- | ---: | ---: | ---: | ---: |
| `-tc -s0.05` (unlimited) | 37,504 | 280 / 77 | 575 / 107 | 550 / 107 |
| `-tm -s0.05` (unlimited) | 11,838 | 279 / 77 | 595 / 106 | 557 / 104 |
| `-tc -s0.05 -n2` | 12,769 | 277 / 77 | 777 / 181 | 622 / 126 |
| `-tc -s0.05 -n3` | 25,337 | 288 / 77 | 814 / 181 | 673 / 126 |
| `-tm -s0.05 -n2` | 4,091 | 272 / 77 | 789 / 179 | 715 / 126 |
| `-tm -s0.05 -n3` | 8,512 | 280 / 77 | 876 / 180 | 903 / 126 |

(Load 3–6; the C and "fix" columns are one run of `compare.sh`, the
`671474e` column another.) Before the fix the limited runs took
1.3–1.5× the unlimited ones' wall time and +73 MB RSS (the full rows of
§4); C, which performs no check, does not get slower with `-n`. The
interleaved before/after comparison (10 runs each, `/usr/bin/time -l`,
benchstat; instructions retired are deterministic):

| config | wall | user+sys | instructions | RSS |
| --- | --- | --- | --- | --- |
| `-tc -s0.05 -n2` | −14.3 % (p = 0.001) | −12.3 % | −9.8 % | 181 → 126 MB |
| `-tc -s0.05 -n3` | −14.1 % (p = 0.000) | −13.9 % | −12.7 % | 181 → 126 MB |
| `-tm -s0.05 -n2` | ~ (p = 0.68) | ~ | +0.8 % | 179 → 126 MB |
| `-tm -s0.05 -n3` | ~ (p = 0.85) | ~ | +2.9 % | 179 → 126 MB |
| `-tc -s0.05` (no limit, control) | ~ | ~ | ~ | ~ |

What remains: closed checks now visit about 3 % of the cover
transactions (the probes; before the fix the scan visited 57 % of
the cover on average, 4.9 M transactions for 25,337 checks at `-n3`,
because neighbouring transactions in bag order share most items). A
maximal check of a maximal set must count every cover transaction (here
1.7 M of 2.5 M cover transactions at `-n3`), so `-tm` stays 1.3–1.6×
the unlimited run. The remaining memory (+20 MB over the unlimited
run) is the horizontal copy of the bag (7.1 MB items, 3.2 MB offsets and
weights), the vertical form (11.9 MB, §5) and the per-item arrays.

The other datasets (same interleaved method, base `671474e`): closed
checks −3 to −13 % wall (`sparse.tab -tc -s0.05 -n2/-n3`, `-s0.02 -n3`,
`dense.tab -tc -s2 -n3` −12.6 %, `weighted.tab -w -tc -s0.2 -n3`
−11.9 %); maximal and the small `dNxKdP` runs within noise or slightly
slower (`dense.tab -tm -s2 -n3` +2.5 %, p = 0.010; `weighted.tab -w -tm`
+4.8 % wall, p = 0.002, user+sys equal; `d5000x60d40` ~ wall, +3–5 %
user+sys at 10 ms resolution; in-process `CloMaxLimited` ~ to +2.8 %).
RSS is equal or lower everywhere.

## 8. Decision

The size-limited search with the complete extension check is exactly
correct (§3, §6) and replaces full exploration for closed/maximal targets
with a maximum size below the longest transaction. It removes the
exponential cost of searching beyond zmax: 1,400–2,800× faster and
50–200× less memory on dense data with small `-n`, and also faster than
C there (C searches one level deeper, to zmax+1, and its 16-items machine
has no limit at all). On sparse data, where full exploration is cheap,
the per-set check is a real cost: at `3e5d372` up to 1.7× full
exploration on 20,000 transactions (`s20000x500`, ≤ 60 ms), and on
`sparse-large` (200,000 transactions) 1.3–1.5× the unlimited run's wall
time and +73 MB RSS (§7.1, `671474e`). After
fix S3-1 (compact bit sets, probed closed scans) the limited runs there
take 1.1–1.2× (closed) and 1.3–1.6× (maximal) the unlimited ones and
+20 MB; Go is then 2.3–3.2× C, whose limited runs perform no check (and
are not exact in general, B5). That bounded overhead is accepted for the
removal of the exponential worst case. Without a relevant limit nothing
changes.

Reusing covers along the search path (`checkPath`, the cover of a set
is the cover of its prefix) was implemented after this report's first
version (`docs/benchmarks.md`, attempt 10) and extended to every form of
the bit sets by fix S3-1. Possible follow-up: maximal checks of maximal
sets on sparse data still count every cover transaction. The pre-change
M5 rows in `docs/benchmarks.md` describe the old full-exploration
path.
