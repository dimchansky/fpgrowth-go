# How FP-growth works in this implementation

This document explains the FP-growth algorithm and the implementation
strategies that this Go port takes over from Christian Borgelt's C program
FP-Growth 6.21. The strategies are his; the port reproduces them operation
by operation so that it reports the same item sets, in the same order, as
the original. Where the port deviates on purpose, this document says so and
points to [differences.md](differences.md).

For the exact upstream semantics, line by line, see the audit notes in
[upstream/](upstream/) (mainly [fpgrowth-core.md](upstream/fpgrowth-core.md),
[tract.md](upstream/tract.md), [report.md](upstream/report.md) and
[rules-eval.md](upstream/rules-eval.md)). The package layout of the port is
described in [architecture.md](architecture.md).

## Contents

1. [Terms](#1-terms)
2. [The FP-growth idea](#2-the-fp-growth-idea)
3. [Data preparation: two passes, recoding, the transaction bag](#3-data-preparation-two-passes-recoding-the-transaction-bag)
4. [The mining recursion and the reporter](#4-the-mining-recursion-and-the-reporter)
5. [Perfect extension pruning](#5-perfect-extension-pruning)
6. [Tree variants](#6-tree-variants)
7. [The 16-items machine](#7-the-16-items-machine)
8. [Conditional item reordering](#8-conditional-item-reordering)
9. [Closed and maximal item sets](#9-closed-and-maximal-item-sets)
10. [Generators](#10-generators)
11. [Association rules and rule-based evaluation](#11-association-rules-and-rule-based-evaluation)
12. [Evaluation measures and filters](#12-evaluation-measures-and-filters)
13. [Order of the results](#13-order-of-the-results)
14. [Memory layout, limits and cancellation](#14-memory-layout-limits-and-cancellation)
15. [References](#15-references)

## 1. Terms

- A **transaction** is a set of items with a non-negative integer weight.
  A transaction database is a sequence of transactions; its **total
  weight** `W` is the sum of the weights.
- The **support** of an item set `X` is the total weight of the
  transactions that contain `X`. `X` is **frequent** if its support is at
  least the minimum support `smin`.
- `X` is **closed** if no proper superset has the same support,
  **maximal** if no proper superset is frequent, and a **generator** (free
  set) if no proper subset has the same support.
- An item `i` is a **perfect extension** of `X` if `supp(X ∪ {i}) =
  supp(X)`, i.e. every transaction that contains `X` also contains `i`.
- An **association rule** `B -> h` has a body `B` and a single head item
  `h`. Its confidence is `supp(B ∪ {h}) / supp(B)`; its lift is the
  confidence divided by `supp({h}) / W`.

## 2. The FP-growth idea

FP-growth (Han, Pei and Yin, 2000) finds all frequent item sets without
generating candidate sets. It is a depth-first divide-and-conquer search:

1. Sort the frequent items in a fixed order and drop infrequent items from
   every transaction.
2. Store the transactions in a prefix tree, the **FP-tree**: transactions
   that share a prefix (in item order) share a path. Every node stores the
   total weight of the transactions that pass through it. For every item a
   linked list connects all nodes of that item (the header table).
3. For each item `i`, following its node list yields the paths from the
   root to the nodes of `i`; with the node supports they form the
   **conditional database** of `i`: the transactions that contain `i`,
   restricted to the items that precede `i`. Building an FP-tree from it
   (a **projection**) and recursing finds all frequent sets that contain
   `i` and otherwise only items before `i`.
4. Each item set is found exactly once: in the recursion for prefix `P`,
   the extension `P ∪ {i}` is reported with the support from the header
   table, and its supersets are found in the projection.

The rest of this document describes how the implementation makes these
steps fast and how it supports closed, maximal and generator sets, rules
and evaluation measures.

## 3. Data preparation: two passes, recoding, the transaction bag

The miner needs two passes over the data (`internal/tract`).

**Pass 1 (count).** The item base collects the distinct items in order of
first occurrence, their frequencies (weighted supports), the sum of the
sizes of the transactions that contain each item (the "transaction size
sum"), the number of records and the total weight. Repeated items in a
transaction count once. Empty transactions count toward the number of
records and the total weight. Negative weights and an overflowing total
weight are errors (deviations M6, M7).

**Thresholds.** The minimum support is converted to an absolute support
with upstream's arithmetic: a percentage `s` becomes
`ceil(s/100 · W · (1 − 2⁻⁵²))`, a maximum `floor(S/100 · W · (1 + 2⁻⁵²))`.
The tolerance absorbs the representation error of decimal fractions. The
library's `Fraction(f)` applies the same formula to `f` directly; the
library's `Count(n)` is passed to the miner as an exact integer, so
absolute thresholds are exact beyond 2^53, where the CLI's float notation
`-s-n` is not. The effective minimum support is at least 1 (deviation
M2). For association
rules, where the minimum support applies to the rule body, the item set
threshold is lowered to `ceil(smin · conf)`, because a rule `B -> h` with a
frequent body and enough confidence can have a body ∪ head support down to
that value; with `OriginalSupport` (`-o`) it is not lowered.

**Recoding.** Infrequent items (and items excluded by a selection or an
appearance of "none") are removed, and the remaining items get the codes
`0, 1, ...` in an order chosen by `ItemOrder` (`-q`). By default (`-q2`)
code 0 goes to the item with the largest transaction size sum, or, where
conditional reordering is active (section 8), to the most frequent item;
other modes sort by frequency in either direction or keep the
first-occurrence order. Ties are
broken by a literal port of upstream's unstable quicksort, because the
tie order determines the item codes and with them the order of the output.

**Pass 2 (transaction bag).** Every transaction is read again, recoded,
sorted by item code, and inserted into a prefix tree whose nodes count the
weight of the transactions ending there. This prefix tree replaces
upstream's array of all transactions, which upstream sorts and then reduces
by merging identical transactions; a depth-first traversal of the prefix
tree yields the same distinct transactions in the same order, so every
variant builds exactly the FP-tree that upstream builds. Raw transactions
are never stored: memory grows with the number of distinct transaction
prefixes, not with the number of records. Mining fails with
`ErrNotReplayable` if the second pass does not match the first (number of
records, total weight, or an item the first pass did not see).

For item set targets other than generators, transactions with fewer items
than the minimum size are emptied before insertion, because they cannot
contain a reported set (they still count toward the total weight).
Generators are mined without this filter, because a generator is defined by
the supports of its subsets, which the filter would deflate (deviation M3).

## 4. The mining recursion and the reporter

All variants share one recursion skeleton. For each item `i` of the current
tree, in loop order:

1. add `i` to the current item set together with its support; if the
   reporter rejects it (the closed/maximal repository proves that neither
   the extended set nor any of its extensions can be reported, or the
   extended set is not a generator), skip the item and its subtree;
2. collect the perfect extensions of the extended set (section 5);
3. if further extensions are possible, build the projection for `i` and
   recurse into it;
4. report the current set, then remove `i` again.

Item sets are therefore reported after their supersets in the same branch
(post-order). After the top-level loop the empty set is reported (if the
minimum size is 0) together with the top-level perfect extensions.

The **reporter** (`internal/report`) keeps the current item set and its
perfect extensions and decides what is reported: it applies the support
range, the size range, the support border, the closed/maximal/generator
filters and the `LogSupportRatio` evaluation; it counts the results per
size and fills the pattern spectrum; and it passes accepted sets to a sink
(the library's callback adapter or the CLI's text writer). Because the
reporter is a separate component with a narrow protocol (add item, add
perfect extension, report, remove), all variants share the same filtering
logic.

## 5. Perfect extension pruning

If `i` is a perfect extension of `P`, then for every set `Y` found below
`P`, `Y ∪ {i}` has the same support as `Y`. The miner therefore removes
perfect extensions from the projection instead of searching them, and the
reporter expands them at report time: a set `P` with `k` perfect extensions
stands for `2^k` sets with the same support (the "hypercube"). In
count-only mode the reporter adds binomial coefficients instead of
enumerating the sets.

Perfect extensions are detected when a projection is built (a conditional
item whose support equals the support of the projected set), at the top
level (an item contained in every transaction), and through a **chain
shortcut**: if an item has exactly one node in the tree, all items on the
path above that node are perfect extensions of the extended set, and no
projection is needed.

`Tuning.NoPerfectExtensions` (`-x`) turns the projection test off; the
chain shortcut and the 16-items machine still find perfect extensions, as
upstream. For generators, perfect extension pruning is always on: a set
with a perfect extension is never a generator, so removing the item from
the projection is exactly right, and the reporter ignores such items.

## 6. Tree variants

`Algorithm` (`-A`) selects how FP-trees are represented and processed.
Without an evaluation threshold, all variants report the same sets with
the same supports; they differ in speed and in result order. With the
default aggregation `AggregateFirst` (`-ax`) the evaluation values of
item sets can differ, and with an evaluation threshold so can the
reported sets, because the rule that evaluates a set depends on the
internal item order (section 12, "Rule measures"; deviation list P6).

**Simple** (`-As`). Nodes have only a parent link, a successor link (the
next node of the same item) and a support. A new path shares an existing
node only if the most recently created node of that item has the current
node as parent; since the transactions are inserted in sorted order, this
yields a proper prefix tree without child lists. A projection collects,
for every node of item `i`, the ancestors on its path and inserts them into
a new tree with the node's support.

**Complex** (`-Ac`, the default; `AlgorithmAuto` selects it). Nodes also have child
and sibling links, children sorted by item, so insertion finds shared
prefixes in any order. This variant supports conditional item reordering
(section 8) and uses the 16-items machine for every conditional database
with at most 16 items (section 7).

**SingleTree** (`-Ad`), the top-down approach of Wang et al. (2002). Only
one tree is built; there are no projections. Items are processed in
ascending order; for item `i`, the header entries of the items before `i`
are cleared and relinked to the ancestors of `i`'s nodes, whose supports
are overwritten with the conditional supports. The recursion for `i` then
works on these relinked lists in place. This relies on the order of the
node lists that sorted insertion guarantees.

**TopDown** (`-At`). The tree is stored reversed (each transaction is
inserted from its last item) with child and sibling lists and no parent
links. The recursion consumes the tree from the highest item: it reports
the root item, projects its children by copying and remapping them, and
then merges the root's children into the remaining siblings, so that all
occurrences of lower items move up. This variant does not use the 16-items
machine.

Variant substitutions follow upstream: closed and maximal targets with
`SingleTree` run `Simple`; generators with `TopDown` run `SingleTree`;
reordering is used only by `Complex` and not for closed or maximal targets.
Association rules and item sets with a rule-based evaluation measure use a
separate path (section 11) on which the variant only influences the item
coding.

## 7. The 16-items machine

When a conditional database has few items, tree projection is wasteful.
The 16-items machine (`internal/fpg/fim16.go`, a port of upstream's
`fim16.c`; its basic scheme follows LCM ver. 3 by Uno, Kiyomi and Arimura)
represents each transaction over at most 16 items as a 16-bit mask and
keeps one weight per distinct mask. It mines such a database with bit
operations: the masks are grouped by their highest bit, the conditional
database of an item is obtained by restricting the masks of its group to
the lower bits, and perfect extensions are detected inside the machine.

- **Simple** and **SingleTree** pack the `k` items with the lowest codes
  (`k = Tuning.ItemMachine`, `-l`, default 16) into one bit-mask entry per
  transaction before mining; the packed part of every conditional
  database goes to the machine.
- **Complex** mines every conditional database with at most 16 items
  entirely with the machine; here only whether the machine is enabled
  matters, not `k`.
- **TopDown**, association rules and rule-based evaluation do not use it.

## 8. Conditional item reordering

In the `Complex` variant, each projection sorts the items of the
conditional database by descending conditional support and recodes them in
that order (`proj_reord` upstream). Frequent items then come first in the
conditional tree, which keeps it small. Ties are broken by the port of
upstream's unstable sort so that the output order matches.

Reordering changes the order of the items within a prefix. Closed and
maximal mining needs a fixed item order (section 9), so reordering is
disabled for these targets; for generators the reporter sorts the items of
a set before looking up its subsets. `Tuning.NoReorder` (`-j`) disables
reordering.

## 9. Closed and maximal item sets

For closed and maximal targets the items are processed in descending code
order. Together with the post-order reporting of section 4, every superset
of a set is then reported before the set itself, which is what the
repository test below relies on.

**Repository.** The reporter keeps the closed or maximal sets reported so
far in a repository (`internal/report/clomax.go`, a port of `clomax.c`): a
stack of prefix trees that mirrors the current item set. Level `k` holds the
stored sets that contain the first `k` items of the current set, projected
on the remaining items, and every node carries the largest support of the
stored sets below it. When the current set is reported:

- if a stored superset has the same (or a larger) support, the set is not
  closed;
- for the maximal target, if any stored superset exists, the set is not
  maximal;
- otherwise the set together with its perfect extensions is added to the
  repository.

The repository is updated before the support, size, border and evaluation
filters, so closedness and maximality are decided on all frequent item
sets, and the filters only select which of them are reported.

**Head-union-tail pruning** (maximal only). Before the recursion for a
conditional database, the miner checks whether the current set ("head")
together with all items of the conditional database ("tail") is contained
in an already stored maximal set. If so, every set in this subtree is a
subset of a known maximal set and the subtree is skipped. This test goes
back to MAFIA (Burdick, Calimlim and Gehrke, 2001). `Tuning.NoHeadUnionTail`
(`-u`) disables it.

**Size limits (deviation M5).** Upstream limits the search depth to one
item more than the maximum size. The repository then lacks the supersets
beyond the limit, and non-closed or non-maximal sets can be reported. The
port also limits the search: it stops where no set of at most the maximum
size can follow (`xable2` and `deeper` in `internal/fpg/mine.go`). Every
set that passes the repository and the support and size gates is then
checked against **all** items of the database, with supports computed from
the cover of the set in the transaction bag (`internal/fpg/extcheck.go`):
a closed set must have no one-item extension of the same support, a
maximal set no frequent one-item extension. One item suffices, so this
complete one-item extension check makes the results equal to the
definition. Without a maximum size below the longest transaction the
search is unlimited and no check is installed (the repository alone is
exact there). The proof, the minimized counterexample of an earlier
attempt and the test evidence are in `docs/m5-investigation.md`; the
measured cost is in `docs/benchmarks.md` ("M5: size-limited
closed/maximal"): faster than C on dense data with a small maximum size,
2.2–3.2× C on large sparse data, and slowest for dense maximal sets of
exactly the maximum size, whose check must count every cover transaction.

## 10. Generators

A set is a generator if each of its subsets with one item less has a
different (larger) support. Because sets are extended one item at a time
and the prefix is already known to be a generator, the reporter only has to
check the subsets that drop one of the other items. It keeps all
generators found so far in a hash table keyed by the sorted item list;
a candidate is rejected if one of these subsets is missing from the table
(so it is not a generator, or not frequent) or has the same support. Single
items are generators unless they occur in every transaction. The table is
never pruned, so it grows with the number of generators.

## 11. Association rules and rule-based evaluation

Association rules, and item sets evaluated with a rule measure, need the
supports of all subsets of a set at once (the body supports). FP-growth's
recursion does not provide them, so this path (`fpg_tree` upstream,
`internal/fpg/tree.go`) works in two phases:

1. **Fill an item set tree.** A simple FP-tree is built from the bag and
   processed in place like the `SingleTree` variant (relinking the node
   lists of the items before `i`), but without perfect extension pruning
   and without the 16-items machine. In parallel it walks an item set tree
   (`internal/istree`, a port of the parts of `istree.c` that fpgrowth
   uses) and stores the support of every frequent item set up to the size
   limit (one more level for closed and maximal targets).
2. **Evaluate, filter, report.** The item set tree computes the evaluation
   measure, applies evaluation-based pruning (`PruneSize`, `-p`), applies
   the closed/maximal/generator filter for item set targets, and reports.
   For rules it generates, for every frequent set `S` and every item `h`
   of `S` that may appear in a head, the rule `S \ {h} -> h`, and checks
   the body support (or the support of body ∪ head with
   `OriginalSupport`), the maximum support, the confidence, the
   appearance constraints and the evaluation measure.

Rules have a single item in the head; the body may be empty (a rule of size
1, `-> h`, whose confidence is the relative support of `h`).

The item set tree holds every frequent set up to the size limit in memory;
the size limit (`MaxSize`, `-n`) and a higher minimum support keep it
small.

## 12. Evaluation measures and filters

- **Log support ratio** (`LogSupportRatio`, `-eb` for item sets): the
  binary logarithm of `supp(S)` divided by the support expected if the
  items of `S` were independent. It is computed in the reporter on the
  normal FP-growth path, without the item set tree.
- **Rule measures** (25 measures, `internal/ruleval`, a port of
  `ruleval.c` with its chi² and gamma functions): computed from the
  supports of body ∪ head, body, head and the total weight. For rules the
  measure of the rule itself is used. For an item set `S` the measure is
  aggregated over the rules `S \ {h} -> h` (first rule, minimum, maximum or
  average, `-a`). "First" (`AggregateFirst`, `-ax`, the default) is the
  rule whose head is the first item of `S` in the miner's internal item
  order. That order comes from the item codes (`-q`), the variant and its
  reordering, and the target, so the value of an item set, and with a
  threshold whether it is reported, can depend on them (deviation list
  P6); minimum, maximum and average do not. With "invalidate below expected support" (`-i`), a rule
  whose support is not above the expected support gets the measure's
  worst value.
- **Values without filtering** (library only, `Evaluation` without
  `Filter`): the measure is computed for every result and nothing is
  filtered by it; the CLI always filters (`-d`).
- **Pruning** (`PruneSize`, `-p`, item sets with a rule measure only)
  marks the sets that fail the evaluation. With a negative value `p`,
  from size `max(-p, 2) + 1` on a set is also marked if all its subsets
  with one item less are marked (weak forward pruning); with a positive
  value, from size `max(p, 2) + 1` on, if any such subset is marked
  (strong forward pruning); with 0 only the failing sets are marked
  (backward pruning). Marked sets are not reported. Upstream 6.21 applied the filter before setting the measure,
  so `-p` had no effect; the port fixes this (deviation R2).

Several measures are corrected for edge cases where upstream returns 0,
NaN or slightly wrong values (deviations R4-R7, R9).

All measures are computed in float64. The statistical ones avoid
upstream's cancellation (deviation N1): the deviation from independence
supp·base − head·body is computed exactly in 128-bit integers, the
information as a sum of non-negative terms, and the Fisher tests above a
total weight of 170 from Stirling's formula, summing only the tables
that contribute (about 9 standard deviations of the hypergeometric
distribution on each side of its mode); they are accurate up to a total
weight of 2^63 (numeric contract in `docs/differences.md`). A value
exactly on a threshold may fall on either side for any measure (F-1).

## 13. Order of the results

The order of the reported sets is a property of the algorithm: the item
codes (from the item order and its tie breaking), the variant, the loop
direction, reordering, where perfect extensions are found and the order in
which the reporter expands them. The port reproduces all of these, so its
output order equals upstream's for every variant. The order is
deterministic for given data and options; it is not sorted, and it is not
part of the library's compatibility promise. Different variants or tuning
settings report the same sets in a different order (with an evaluation
threshold under `AggregateFirst`, possibly also different sets, section
6).

## 14. Memory layout, limits and cancellation

- Trees are slices of value structs addressed by `int32` indices, not
  pointer graphs. Each variant has one node arena; a projection allocates
  its nodes after a mark and releases them when its recursion returns.
  Scratch buffers are reused across recursion levels.
- An arena, the item set tree or the closed/maximal repository that would
  need more than 2^31-1 nodes makes mining fail with `ErrTooLarge`
  instead of crashing.
- Supports are 64-bit integers. Since all weights are non-negative, every
  support is bounded by the total weight, whose overflow is checked in
  pass 1.
- Floating-point computations follow upstream's evaluation order and avoid
  fused multiply-add, so values match the C reference built without FMA;
  log-based values can differ in the last bit from the platform's C library
  (deviation F-1).
- Cancellation is checked before each pass and every 1024 transactions
  while reading; every 32768 prefix-tree nodes while `BuildBag`
  completes the bag (`ItemBase.SetAbort`: sorting the child lists visits
  every node, about 0.6 s for 69 million nodes); before `OnStart` and at
  the start of `Miner.Mine`; every 4096 transactions while any variant
  (and the item set tree path) builds its top-level FP-tree
  (`buildCheckEvery`); at every recursion entry; before every node of a
  top-down level (`recTopDown`: each one costs a merge of its subtrees,
  proportional to the tree at the top level, without a recursion entry
  when nothing is projected, e.g. with `-n1`); every 1024 reporter steps
  (`Add` and `Report` calls and sets of an expanded perfect-extension
  hypercube, `report.AbortEvery`); every 256 nodes or counters while the
  item set tree is filtered (`Filter`, `CloMax`) and reported; and every
  4096 tables of a Fisher test (about 9σ per side of the mode, σ <= √n/4). The
  recursion entries alone are not enough: a hypercube of 2^k sets or a
  burst of the 16-items machine reports without returning to one. The
  reporter latches the abort error like a repository overflow, so `Add`
  fails and `Report` returns it until every recursion has unwound (the
  top-down loop returns it at once instead of merging the rest of the
  level). A canceled context ends mining with `ctx.Err()`, also if the
  miner finished after its last check. The checks are non-blocking
  receives on the context's done channel and do not allocate. Up to about
  1024 sets (rules: those of 256 item sets) found between two checks may
  still be reported after the cancellation. The latency is the work
  between two checks: a single projection, merge or transaction
  insertion, each proportional to the tree it processes, so it grows with
  the input and is not a hard bound (measured once, with a program that
  is not committed, on 4 million transactions of 20 items out of 1000:
  usually 10 ms or less, up to 0.35 s when a node arena of several GB is
  grown, i.e. copied, by one insertion; the independent audit's own probe
  of that shape measured 54 µs to 104 ms). The tests assert only 500 ms
  (50 ms while the top-level tree is built).

## 15. References

- J. Han, J. Pei, Y. Yin. Mining Frequent Patterns without Candidate
  Generation. Proc. ACM SIGMOD Int. Conf. on Management of Data
  (SIGMOD 2000, Dallas, TX), 1-12. ACM Press, 2000.
- C. Borgelt. An Implementation of the FP-growth Algorithm. Proc. Workshop
  Open Source Data Mining Software (OSDM 2005, Chicago, IL), 1-5. ACM
  Press, 2005.
- C. Borgelt. Frequent Item Set Mining. Wiley Interdisciplinary Reviews:
  Data Mining and Knowledge Discovery 2(6):437-456, 2012.
- K. Wang, L. Tang, J. Han, J. Liu. Top Down FP-Growth for Association Rule
  Mining. Proc. 6th Pacific-Asia Conf. on Knowledge Discovery and Data
  Mining (PAKDD 2002, Taipei, Taiwan), 334-340. Springer, 2002.
- T. Uno, M. Kiyomi, H. Arimura. LCM ver. 3: Collaboration of Array,
  Bitmap and Prefix Tree for Frequent Itemset Mining. Proc. 1st Int.
  Workshop on Open Source Data Mining (OSDM 2005). ACM Press, 2005.
- D. Burdick, M. Calimlim, J. Gehrke. MAFIA: A Maximal Frequent Itemset
  Algorithm for Transactional Databases. Proc. 17th Int. Conf. on Data
  Engineering (ICDE 2001). IEEE, 2001.
- Christian Borgelt's FP-growth page, with the original program and
  documentation: <https://borgelt.net/fpgrowth.html>.
