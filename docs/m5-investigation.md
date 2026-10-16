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
and the supports are computed is described in §4.

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

- `internal/fpg/extcheck.go`: `extChecker`. It keeps the items of every
  transaction of positive weight and either one bit set of transactions
  per item (if that costs at most 4× the horizontal form or < 64 MiB) or
  per-item transaction lists. The cover of a set is the AND of its bit
  sets (visiting only the non-zero words of its sparsest item), or the
  rarest item's list filtered by containment. Per check it picks the
  cheaper of scanning the cover transactions (closed: intersect until no
  candidate is left; maximal: count until an item reaches smin) and
  testing every item against the cover (closed: subset test, items in
  descending frequency, stop at the first item rarer than the set;
  maximal: weighted popcount via weight bit planes).
- `internal/fpg/mine.go`: the checker is installed when the target is
  closed/maximal and zmax < `Bag.MaxSize()` (otherwise the search is
  unlimited as before); `xable2`/`deeper` are the size gates.
- `internal/fpg/{simple,complex,topdown,fim16}.go`: projections gated by
  `deeper` (the machine `clear`s an item instead of projecting it).
- `internal/report/report.go`: `Reporter.SetCloMaxCheck`, called in
  `Report` after the repository and the support/size gates, before the
  border, evaluation and sink.
- The full-exploration path with a limit and the temporary switch used
  for the comparison below were removed. Test hooks (a replacement check,
  forcing transaction lists) live in an unexported `Miner` field.

## 5. Memory and cost of the check

The checker stores every item instance once (4 bytes) plus either the
bit sets (bounded as above) or a second copy as transaction lists. A
check costs at most min(Σ |t| over the cover, #items × #non-zero cover
words) plus the cover computation; closed checks usually stop after a
few transactions or items. It runs once per set that passes the
repository and the size/support gates, i.e. at most once per output
candidate. Full exploration's extra cost, by contrast, is the whole
closed (maximal) lattice above zmax, which is exponential on dense data.

## 6. Test evidence

All on the final code (`go test ./internal/fpg -run
'TestExtCheck|TestOracle$|TestDeviations' -v` with
`FPG_M5_CASES=10000 FPG_ORACLE_CASES=5000 FPG_M5_UNLIMITED=300`):

| test | what | result |
| --- | --- | --- |
| `TestExtCheckOracle` | 10,000 random datasets (dense: each transaction half to all of 1–12 items; sparse: 0–7 items; 1/10 with 17–20 items for the >16-items paths), unit/small/zero/large weights, `-s` absolute and relative, zmin 0–3, zmax zmin..zmin+3, in 1/6 of the cases 0–7, `-S`, `-x`, `-u`, `-q2 1 0 -1 -2`, borders, item selection in 1/4; closed and maximal × 11 variants (`-As -l0/3/16`, `-Ac` ± `-j` ± `-l0`, `-Ad -l0/3/16`, `-At`); even seeds bit sets, odd seeds transaction lists | **220,000 comparisons, 0 differences** |
| `TestOracle` (existing) | 5,000 datasets, all four targets × 11 variants | 220,000 comparisons, 0 differences |
| `TestExtCheckUnlimited` | 300 datasets beyond the oracle (20–40 items, 40–200 transactions): limited run (zmax 1–4) = unlimited run filtered by size, closed/maximal × 11 variants | 26,400 runs equal |
| `TestExtCheckBrute` | 300 datasets: every call of the check (both forms; with bit sets both the scan and the vertical path) against a brute-force single-item support scan, plus the reporter's support | 6,600 runs, 0 mismatches |
| `TestExtCheckRegressions` | minimized seed 678 (with/without `-x`, closed/maximal), original seed 678, the B5 reproducers, two-item and depth-2 "above" cases, all variants, both checker forms | complete check: 0 differences; local check fails on every case marked so (e.g. `above closed`: all 11 variants) |
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
index made the latter 15 s. The final hybrid fixed both.

## 8. Decision

The size-limited search with the complete extension check is exactly
correct (§3, §6) and replaces full exploration for closed/maximal targets
with a maximum size below the longest transaction. It removes the
exponential cost of searching beyond zmax: 1,400–2,800× faster and
50–200× less memory on dense data with small `-n`, and also faster than
C there (C searches one level deeper, to zmax+1, and its 16-items machine
has no limit at all). On sparse data, where full exploration is cheap,
it costs up to 1.7× (≤ 60 ms on 20,000 transactions) for the per-set
check; that bounded overhead is accepted for the removal of the
exponential worst case. Without a relevant limit nothing changes.

Not done (possible follow-ups): reusing covers along the search path (the
cover of a set equals the cover of its prefix, so it could be computed
incrementally per recursion level) would cut the check cost on sparse
data further. The pre-change M5 rows in `docs/benchmarks.md` describe the
old full-exploration path.
