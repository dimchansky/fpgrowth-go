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
/ 22 MB, Go 31.6 s / 422 MB, as measured by the Stage 1 review, finding
F5, on its own copy of such data; the measurement of this report is in
§7).

A first attempt to keep the depth limit and only collect perfect
extensions at the boundary failed on oracle seed 678. This report
minimizes that case, identifies the first violated invariant, and replaces
full exploration by a **size-limited search plus a mathematically complete
one-item extension check** against all items of the database. It is
exact (verified against the brute-force oracle, §6) and on the critic's
case runs in 21 ms / 14 MiB instead of about 30 s and 0.4–0.7 GB (C:
236 ms / 22 MiB; §7 has the single measurement this figure comes from,
34.09 s / 673 MiB at c16c2f5, and the audit's re-measurement). It is now the only path for closed/maximal targets with `-n`
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

The minimized case is also a minimal reproducer of upstream bug B5 itself
(independent audit, M5A-3): C 6.21 reports `c b (1)` for both data sets,
closed and maximal, with `-As -l0`, `-Ac -l0`, `-Ad -l0` and `-At`, and
`c b (4)` for the original seed 678 data with the same variants. With the
16-items machine (`-As`, `-Ac`) C happens to be right, because upstream's
machine ignores the depth limit (§3.6). The Go program reports the
oracle's sets in every variant.

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

Items outside `E` cannot matter: an item removed by recoding is
infrequent, so supp(X ∪ {j}) ≤ freq(j) < smin ≤ supp(X) for every
reported X, which therefore stays closed and maximal with respect to it.
(Items removed by the item selection `-R` are outside the universe by
upstream's semantics, and the oracle agrees.)

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
- **Sibling rows (maximal checks, issue #19, §4.1).** A maximal check
  whose prefix is N = P ∪ {h} (|N| ≥ 2) may compute the same supports
  from bit sets re-indexed to cover(P). The positions 0, 1, … are the
  transactions of cover(P) in ascending order, a bijection onto
  cover(P), and cover(N̂) = cover(N) ⊆ cover(P) (first bullet). So the
  population count of R_h & R_j (weighted by the weight bit planes of
  the positions) is the weight of the cover transactions of N̂ that
  contain j, i.e. supp(N̂ ∪ {j}), for every item j with a row. Every
  item j ∉ P with supp(P ∪ {j}) ≥ smin has a row. For every other item
  j ∉ P, anti-monotonicity gives supp(N̂ ∪ {j}) ≤ supp(P ∪ {j}) < smin
  (weights are not negative, M6), so j cannot make N̂ non-maximal. The
  items of P are in N̂. Perfect extensions of P or of N keep their rows
  (all ones over the cover, resp. over R_h) and are skipped because
  they are in N̂; with `-x` an equal-support item is not in N̂ and is
  counted like any other item. The child item h has a row, because
  supp(P ∪ {h}) = supp(N̂) ≥ smin for every reported set (a reported
  set is frequent, §3.4). So every j ∈ E \ N̂ is still decided, by the
  same support as the ordinary check. The rows of level L are a
  function of path level L (the cover of P) and the bag only, and
  `checkPath` drops them whenever it recomputes a level ≤ L. Whether
  and when the rows are built is a cost decision only; it changes no
  verdict.

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
full support). Every stored set is also frequent, because the miners add
only frequent items; the maximal report rejection below relies on that (a
stored superset is a frequent superset). Hence:

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
    all n items (n = 100,000: 22.0 s, now 0.30 s, where the C program
    crashes from about n = 60,000; n = 30,000: 2.17 s, now 0.08 s,
    C 0.05 s; independent review of the change). One quadratic shape
    remains, in C too: a long first cover transaction whose probes fail,
    followed by short ones (one 50,000-item line plus 50,000 one-item
    lines, `-As -tc -s-2 -n1`: Go 6.8 s, C 3.4 s); starting the
    candidates from the shortest cover transaction would remove it. For a single-transaction cover the first item of t
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
| Stage 3 fix S3-1 | incremental for every form; closed scan with spread probes | compact rows (sparse items: non-zero words only), no absolute floor; transaction lists removed |
| issue #19 (this version) | as S3-1 | as S3-1, plus sibling rows for maximal checks (§4.1) |

The 64 MiB floor of the first two versions was a memory-for-speed
trade-off: on `sparse-large` (2,592 frequent items at `-s0.05`, 3,115
words) it admitted full rows of 64.6 MB, 9.1× the 7.1 MB of horizontal
items, because the transaction lists were slower there — measured with
incremental covers for the lists as well (in-process CLI benchmark,
n = 5–6, load 5–40): +21 to +31 % time for `-tc`/`-tm` `-s0.05 -n2/-n3`
at −80 MB allocated. Compact rows remove the trade-off: they bound the
vertical form relative to the input (§5) and are faster than both
(§7).

### 4.1 Sibling rows of the maximal check (issue #19)

The independent audit's finding M5A-1 (§8): on dense data with many
maximal sets of exactly the maximum size, every check of a maximal set
counts its whole cover, and the checks of the children of one parent
repeat that work over subsets of the same transactions. On
`d20000x200d30 -tm -s2 -n3` all 1,313,400 checked sets are maximal.
Their parents (19,701 pairs P) have 67 checked children each on
average. A parent cover has about 1,800 transactions in 297 of 313
words, a child cover about 540 transactions in 239 words. The check took
91 % of the run (`scanMaximal` 86 %, about 2.1 ns per item visit).
The auditor's suggested bound, supp(P ∪ {j}) ≥ smin, holds for 198 of
the 198 candidate items there, so it prunes nothing by itself.

`internal/fpg/extsib.go` re-indexes the parent's cover instead (the
proof is in §3.2, last bullet):

- **Rows.** For the parent level L = |prefix| − 2 ≥ 0 of `checkPath`,
  one pass over the horizontal form of cover(P) assigns the positions
  0, 1, … to the cover transactions. It sets, for every frequent item
  that occurs there, the bit of each position in a row of
  ⌈|cover(P)|/64⌉ words. It also sums supp(P ∪ {j}), and it fills the
  positions' weight bit planes when some distinct transaction has a
  weight above 1. Items with supp(P ∪ {j}) < smin and the items of P
  get no entry. The entries are sorted by descending supp(P ∪ {j}), so
  a non-maximal child usually stops at the first entries.
- **Child check** (`sibMaximal`). The row R_h of the child item is the
  child's cover; for every entry j not in the checked set,
  supp(X ∪ {j}) = Σ_b 2^b · popcount(R_h & R_j & plane_b) (without
  planes, the popcount of R_h & R_j). The check stops at the first j
  that reaches smin. On the target this replaces about 32,000 random
  item visits per child by 198 × 29 sequential word operations.
- **One cache per path level.** Level L's rows depend only on path level
  L. `checkPath` drops the rows of every level ≥ k, k being the first
  level it recomputes, so a level's rows survive the checks of the
  children's own subtrees between two children (all miners report in
  post-order). A single cache for the deepest parent was reset after
  every child at `-n4` (design review F2).
- **When to build (ski rental on actual work).** The ordinary maximal
  check counts its actual work: item visits in `scanMaximal`, word tests
  (times the weight planes) in `vertMaximal`, up to its stop. The work
  of the children's ordinary checks is accumulated per level. The build
  cost B is Σ_{t ∈ cover(P)} |t| plus the words to clear. It is
  computed by one pass over the cover's transaction lengths, but only
  once the work has reached 4·(L+1)·|cover(P)|; every cover transaction
  has at least L+1 items, so that pass costs at most a quarter of the
  work. The rows are built once the work reaches B/2.
  - Further gates: the rows must be estimated cheaper per child than
    the child's ordinary check (frequent items · words · planes < `decide`'s
    estimate), and they must fit the memory budget below. After the
    build, a child uses the rows only if |entries| · words · planes ≤
    its ordinary estimate.
  - Bound: the build and the cost pass take at most 2.25 times the
    ordinary-check work already spent at that level. A level whose rows
    never pay off costs at most 3.25 times its ordinary checks.
  - An estimated rather than actual cost was rejected (design review
    F1). A scan of a non-maximal set stops at its first frequent
    extension, so the estimate can exceed the work by orders of
    magnitude: on `d20000x200d30 -tm -s-2 -n2` the review's prototype
    built rows 187 times and the run became 27 % slower. With actual
    work it builds none.
  - Threshold B/2 instead of B: target 19.8 s instead of 22.3 s (2
    runs each).
- **Memory budget.** The rows and planes of all levels together hold at
  most 2 words per item occurrence of the horizontal form (16·O bytes).
  A build that does not fit first releases the buffers of levels without
  rows, then refuses the level until it is dropped. Peak use measured
  (test statistics, §7.2): 6,200 words (50 KB) on the target, 8,400
  words at `-n4`, against a budget of 2.4 M words.
- **Closed checks** are unchanged: a closed check of a non-closed set
  usually stops after a few candidates (§4), and the closed scan's
  remaining quadratic case is issue #20.
- **Test hooks** (`testHooks.sib`): never, at the first child, at child
  number m, or by the production rule. A verify mode also runs the
  ordinary check and panics on a different verdict. The statistics
  (`sibStats`: builds, builds with several words, with planes, checks
  answered from rows, refusals, peak words) are asserted by the tests
  (§6).

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
| per-depth path cache (`checkPath`) | ≤ 12·W per level (12 bytes per non-zero cover word; level k ⊆ level k−1 ⊆ the first item's words), at most zmax levels (the prefix of a reported set has at most zmax items): ≤ 12·W·zmax. Tighter (independent audit, M5A-2): a transaction contributes a cover word to at most \|t\| levels, so all levels together hold ≤ 12·O bytes, independent of zmax |
| scratch covers of `check` | ≤ 2 · 12·W |
| sibling rows of the maximal check (§4.1), all levels | ≤ 16·O (budget: 2 words per item occurrence); measured peak 50 KB on `d20000x200d30 -tm -s2 -n3` |
| per item: frequencies, order, stamps, counts, offsets | ≈ 44·k (maximal: + 8·k build scratch) |

Measured on `sparse-large` at `-s0.05` (k = 2,592, T = 199,337,
W = 3,115, O = 1,765,336, Z = 924,374; the 4× rule fails, 62 dense
items, 5 weight planes from merged duplicate transactions): vertical
form 11.9 MB = 1.7× the 7.1 MB of horizontal items, instead of 64.6 MB
of full rows plus a 3.7 MB index before. Peak RSS of the CLI (§7):
126 MB, before 179–181 MB, the unlimited search 106 MB. The audit measured
+50 MB (+20 %) over the unlimited search on 1 million sparse transactions
with `-n1` (297.7 vs 248.1 MB; C 146.5 MB). In all, the checker's extra
memory is linear in the input: at most about 36·O + 24·T bytes plus the
per-item arrays (audit's derivation).

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

*Sibling rows (issue #19, 2026-10-20).* The table's tests were re-run
with `FPG_M5_CASES=20000 FPG_ORACLE_CASES=5000 FPG_M5_UNLIMITED=300
FPG_SIB_CASES=2000`: `TestExtCheckOracle` 440,000 comparisons, 0
differences; `TestOracle` 220,000 comparisons, 0 differences;
`TestExtCheckUnlimited` 26,400 runs equal; `TestExtCheckBrute`,
`TestExtCheckRegressions`, `TestExtCheckLocalSweep` (5,072 of 33,000 runs
differ, as before) and `TestDeviations` pass. New tests
(`internal/fpg/extsib_test.go`):

| test | what | result |
| --- | --- | --- |
| `TestExtCheckSiblingRows` | 2,000 dense datasets of 65–400 transactions (rows of up to 7 words, ragged last word; a third with independent items and unit weights, the rest with merged or random weights, i.e. planes), 4–20 items, zmax 2–4, `-x` in a third; maximal × 11 variants; rows built at the first child, at child m (random 0–5) and by the production rule, every verdict from rows verified against the ordinary check; a quarter also through `bruteCheck` (every call against the source transactions); counters asserted | 63,459 runs, 729,482 builds (245,991 with several words, 507,263 with planes, 272,015 under `-x`), 2,900,118 checks from rows, 0 differences |
| `TestExtCheckSiblingRowsOracle` | `TestExtCheckOracle`'s first 600 generator cases, rows forced at the first child and at child seed mod 4 | 6,094 runs, 22,554 builds, 0 differences |
| `TestExtCheckSiblingSupp` | random parents and children over 1–1,400 transactions (up to 22 words), weights 1, 0–3, 0–9: every row's supp(P ∪ {j}) and every child's row popcount against `suppWith`; every item without a row is infrequent with P | pass |
| `TestExtCheckSiblingInvalidate` | 40 datasets × 400 `checkPath` steps: deeper, next sibling, shrink and regrow, the same last item under a different earlier item, jumps, interleaved `check` and one-item prefixes, random subsets of perfect extensions, wrapping set stamp; against a checker without rows | pass |
| `TestExtCheckSiblingWork` | production rule: children that stop at once build nothing; 39 maximal children build after child 7 and use the rows; a budget below one level's rows refuses the build with the same verdicts | pass |

Mutants of the sibling rows, each run against the default `go test
./internal/fpg` (no environment variables): row filter `>` instead of
`≥` smin, word index 0, weight planes ignored, invalidation from level
k+1, no invalidation, rows with supp(P ∪ {j}) = supp(P) dropped (assumed
perfect extensions), build scratch reset only for kept items, perfect
extensions of the child counted (only h skipped), support test `>`
instead of `≥` smin, last word of the rows dropped: **all caught** (by
`TestExtCheckSiblingInvalidate`, `TestExtCheckSiblingRowsOracle`,
`TestExtCheckSiblingSupp` or `TestExtCheckSiblingRows`). The mutant
"bit (r+1) mod 64 instead of r mod 64" survives; it is equivalent (a
permutation of the positions within each word, applied to every row
and plane alike).

The audit's brute forces (`reference/_audit/m5/bf`, `bfs`, built from
the archive) against the branch: with the production rule `bf -big -n
2500` seeds 11 and 12, `-focus -big -eval 6 -n 2500` seed 21, `-focus
-eval 6 -n 4000` seed 22, `-wide -focus -big -eval 8 -n 2500` seed 41,
`-wide -eval 8 -n 3000` seed 42 and `bfs -n 400` seed 51: 189,000 runs,
0 failures (16,191 builds in 859 runs, from a statistics build of the
same code). With rows forced at the first child (`-big` seed 13,
`-focus -big` seed 23, `-wide -focus -big` seed 43, plain `-n 4000` seed
14, `bfs -n 200` seed 52) and at the third child (`-big` seed 15,
`-focus -n 4000` seed 24), both verifying every verdict against the
ordinary check: 199,000 runs, 0 failures. `go test -race ./...`,
`FuzzMine` and `FuzzMineItemsets` (60 s each) and `cd reference &&
./build.sh && go test ./...` (difftest) pass.

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

The "before" column is the one measurement of the full-exploration path
that this report relies on (c16c2f5, data generated in process before the
datasets were pinned, see below). Other figures for the same case differ:
the Stage 1 review measured 31.6 s / 422 MB (§1), and the independent
audit rebuilt c16c2f5 and measured 29.1–29.8 s / 434–496 MiB on the
pinned `d2000x40d60.tab` (3 runs, `/dev/null`). Read the "before" peak
memory as 0.4–0.7 GB; the conclusion (roughly 1,500× faster and 30–50×
less memory) does not depend on which figure is used. The audit's
"after" figures were 19.8 ms / 13.9 MB, C 224.8 ms / 22.0 MB.

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

### 7.2 Sibling rows on dense maximal data (issue #19)

The CLI with output to `/dev/null`, `/usr/bin/time -l`. Runs are
interleaved per configuration: `main` (`3c0b7e8`), v0.1.0, the branch
and, for the target, C native. Apple M1 Max, Go 1.27.2, load average
4.6–4.8 (the first rounds of the target ran while the load decayed
from an earlier job). Median [min–max] of wall seconds and max RSS MB;
the set counts are equal for all Go binaries:

| `d20000x200d30` | sets | n | `main` | v0.1.0 | branch | C |
| --- | ---: | ---: | --- | --- | --- | --- |
| `-tm -s2 -n3` (target) | 1,313,400 | 5 | 97.12 [96.81–97.50] / 110.5 | 97.25 [97.14–97.67] / 110.5 | **19.82 [19.78–20.19] / 110.9** | 39.12 [39.02–39.51] / 1259.1 |
| `-tm -s1 -n4` | 1,211,590 | 3 | 131.39 [131.06–131.78] / 111.3 | 131.06 [131.02–131.75] / 111.3 | **67.74 [67.68–67.78] / 111.8** | — |
| `-tm -s2 -n3 -As` | 1,313,400 | 3 | 95.72 [95.72–96.20] | 95.79 [95.60–96.15] | **18.53 [18.44–18.54]** | — |
| `-tm -s2 -n3 -At` | 1,313,400 | 3 | 119.93 [119.34–120.02] | 119.47 [119.25–120.22] | **42.31 [42.00–42.44]** | — |
| `-tc -s2 -n3` | 1,333,500 | 3 | 10.42 [10.40–10.58] | 10.28 [10.20–10.45] | 10.41 [10.15–10.43] | — |
| `-tm -s-2 -n2` (F1 row) | 0 | 5 | 0.62 [0.61–0.65] | 0.61 [0.61–0.62] | 0.62 [0.61–0.62] | — |
| `-tm -s2 -n2` | 0 | 5 | 0.62 | 0.62 | 0.61 | — |

On the target the branch is 4.9× faster than `main` and 2.0× faster than
C, at a ninth of C's memory. The remaining profile (in-process, 18.5 s):
building the rows 20 %, the mining 38 % (`csCondSupp`, `addCmplx`,
`projCmplx`), the row popcounts 20 %, the ordinary checks before the
builds (`scanMaximal`) 14 %, the cover updates (`and`) 6 %. 18,528
parents build rows; 1,271,436 of the 1,313,400 checks are answered from
them. The peak row memory is 6,200 words (50 KB). At `-n4` there are
24,631 builds and the peak is 8,400 words. The F1 row (every check stops
at once) builds nothing and is unchanged.

The 28 other configurations ran 7 times each: every `m5-*` row of
`configs.txt` except the slow ones, `sparse-large` with and without
`-n`, `dense`, `sparse`, `weighted` and `long`, closed and maximal.
benchstat of the wall time, branch against `main`: significant only
`dense -tm -s2 -n3` −15.0 % (0.83 → 0.71 s, p = 0.002) and `d5000x60d40
-tm -s5 -n3` −8.2 % (243 → 223 ms, p = 0.001). The others are `~`, and
against v0.1.0 the result is the same. RSS differences are within
±2 % (≤ 0.8 MB). The design review's prototype hint of +9 % on
`d5000x60d40 -tm` (single runs) does not reproduce: that row has 1,379
builds and is faster.

In process, there were 6 interleaved rounds of `internal/fpg`
`BenchmarkCloMaxLimited` and `BenchmarkMine`, and of the root
`BenchmarkSuiteMaxSize`, `SuiteVariants` and `SuiteTargets` (88 rows),
at load 4.2–4.8. benchstat, branch against `main`:

- `SuiteMaxSize/dense/s=2/maximal/n=3`: −17.4 % (808 → 667 ms,
  p = 0.004).
- `CloMaxLimited/dense2000x40d60/tm/n3`: +0.5 % (p = 0.032). This data
  builds no rows; the extra cost is the work counter.
- `Mine/s/s-l0` and `Mine/m/s-l0`: −1.0 % and −0.7 %.
- Three rows without a maximum size, where the checker is not installed
  and no code changed: `SuiteTargets/dense/s=10/closed` +2.4 %,
  `SuiteVariants/dense/s=10/frequent/simple` +1.1 %, `maximal/complex`
  +1.9 %. Against v0.1.0 the same benchmarks show −2.7 to +2.4 %; these
  are noise.
- Everything else is `~`.

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
+20 MB; Go is then 2.2–3.2× C, whose limited runs perform no check (and
are not exact in general, B5). That bounded overhead is accepted for the
removal of the exponential worst case. Without a relevant limit nothing
changes.

"Bounded overhead" was measured on sparse data. The independent audit
found the worst ratio to C on dense data with many maximal sets of
exactly the maximum size (M5A-1): `d20000x200d30 -tm -s2 -n3`, 1,313,400
sets, Go 99.7 s / 110.8 MB against C 39.0 s / 1,258.5 MB (2.6× the time,
a ninth of the memory), while `-tc` on the same data is faster than C
(12.1 s vs 46.2 s). Every reported maximal set needs all of its one-item
extensions shown infrequent, so its check cannot stop early and counts
the whole cover; C pays a similar cost by exploring to zmax+1, on
compressed FP-trees. The result is exact; the time is accepted for now.
A possible optimization is to share the work among the children of a
boundary node (one pass over the parent's cover bounds every child's
extensions); it is deferred.

*Update (2026-10-20, issue #19):* done, as sibling rows (§4.1, proof in
§3.2). The bound alone prunes nothing on that workload; the gain comes
from re-indexing the parent's cover, so that a child's check counts
over the parent's transactions in sequential words. The run takes
19.8 s instead of 97.1 s (C 39.1 s), with the same peak memory. `-n4` is
2× faster, and `dense -tm -s2 -n3` 15 % faster. No measured
configuration got slower (§7.2). Closed checks are unchanged.

Reusing covers along the search path (`checkPath`, the cover of a set
is the cover of its prefix) was implemented after this report's first
version (`docs/benchmarks.md`, attempt 10) and extended to every form of
the bit sets by fix S3-1. Possible follow-up: maximal checks of maximal
sets on sparse data still count every cover transaction. The pre-change
M5 rows in `docs/benchmarks.md` describe the old full-exploration
path.
