# Design — issue #34 (maximal check: defer long light cover transactions)

- PR: https://github.com/dimchansky/fpgrowth-go/pull/43
- Written by the implementer and challenged before implementation; the challenge follows the design. Sections 12-13 and the round 4 note record the changes made in review.


Phase 1 (design and proof). Branch `perf/34-maximal-cover` (local, not
pushed), prototype commit `31e40d2` on top of `origin/main` `e6d02b0`.
Written 2026-10-26, daytime: only light checks were run (unit tests of
`internal/fpg`, a few seconds each); every heavy measurement is listed in
§8 as deferred to the night window.

## 1. Problem and root cause

A size-limited maximal search (`-tm` with `-n` below the longest
transaction, `mine.go:94-118`) decides every set that the repository
accepts with the complete one-item extension check (`extChecker`, M5):
X is maximal iff supp(X ∪ {j}) < smin for every item j ∉ X.

`decide` (`extcheck.go:487-500` on main) picks the horizontal scan when
`costs` (`:506-513`) estimates it cheaper: h = n·(average transaction
length + 1), with the average taken over the whole bag. On the NEW-3
shape (lpN: one N-item line, then N one-item lines; `-tm -s-2 -n1`) every
item has n = 2 cover transactions and the global average length is about
2, so h ≈ 6 against v = N·|nz|: the scan is chosen for every item.

`scanMaximal` (`extcheck.go:803-836` on main) then counts every item of
every cover transaction in cover order and stops only when some count
reaches smin. The long line comes first in each cover (bag order) and has
weight 1 < smin = 2, so all its N items are counted to 1 and none stops
the scan: N visits per check, N checks, Θ(N²) in all. The estimate is
not wrong on average; the scan has no protection against one long
transaction that cannot decide anything by itself. The closed check had
the same defect and #20 fixed it by taking its candidates from the
shortest cover transaction (stage a/b, `scanClosed`); #19 changed only
how the maximal supports are computed when sibling rows exist
(`extsib.go`), not this scan.

Measured in the prototype (unit test, item visits counted by `c.work`,
n = 20,000, all one-item checks): 400,020,000 visits with the old scan,
20,000 with the fix (lp shape); 680,000 for two-item short lines (lp2,
2 lookups per check). The test fails on the old scan
(`TestExtCheckMaximalMixedScaling`, run with deferral disabled).

Upstream C: there is no counterpart. `isr_report`
(`reference/upstream/fpgrowth-6.21/tract/src/report.c:1454-1485`)
consults only the closed/maximal repository (`cm_supp`, `cm_update`) and
searches to zmax+1 (`report.c:1118`), which is the incomplete behavior
that M5 fixes. C's own quadratic time on lp50000 `-tm` (3.32 s, issue
table) comes from its search and repository, not from a transaction
check; the C number is context, not a parity target.

## 2. The bound

Notation. B is the bag without weight-0 transactions (the checker drops
them, `extcheck.go:156`; negative weights are rejected by the item base,
`tract/itembase.go:183-190`), so every w(t) > 0. C = cover(X) = the
transactions of B that contain X, W = w(C) = supp(X), and for j ∉ X,
supp(X ∪ {j}) = Σ_{t ∈ C, j ∈ t} w(t).

**Lemma 1 (issue hypothesis).** If t ∈ C and w(t) > W − smin, every
frequent extension j (supp(X ∪ {j}) ≥ smin) occurs in t.
Proof: if j ∉ t, supp(X ∪ {j}) ≤ W − w(t) < smin. ∎

So only the items of t need to be tested, and the shortest such t gives
the fewest candidates. Taken literally this needs W (the reporter's
`supp`, or a weighted popcount of the cover over all weight planes) and a
pass over the cover to find the shortest qualifying t; and for unweighted
data it applies only when W = smin (w(t) = 1 > W − smin), so it does not
cover lp50000s2 (W = 3, smin = 2). The design uses the following
generalization, which needs neither W nor `supp`.

**Lemma 2 (partial counts).** Split C into the visited transactions P
and the rest R, with cnt_P(j) = Σ_{t ∈ P, j ∈ t} w(t) and rest = w(R).
Then cnt_P(j) ≤ supp(X ∪ {j}) ≤ cnt_P(j) + rest.
Proof: supp(X ∪ {j}) = cnt_P(j) + cnt_R(j) and 0 ≤ cnt_R(j) ≤ w(R)
(weights non-negative). ∎

Consequences: (a) cnt(j) ≥ smin for any partial sum over distinct cover
transactions containing j proves X not maximal; (b) if cnt_P(j) + rest
< smin, j is not a frequent extension, whatever R holds; in particular
once rest < smin no item that does not occur in P can be one. Lemma 1 is
(b) with P = {t}: rest = W − w(t) < smin.

**Lemma 3 (maximal = closed at W = smin).** If W = smin then X is
maximal iff X is closed. Proof: supp(X ∪ {j}) ≤ W = smin, so
supp(X ∪ {j}) ≥ smin ⟺ supp(X ∪ {j}) = W. ∎ (Used only in §5.)

## 3. Proposed exact fix (as prototyped)

`scanMaximal(n)` (`extcheck.go:823`) scans the cover in order as before,
but **defers** a transaction t if

    |t| > |X| + mguard·n   and   w(t) < smin

(mguard = `scanMaxGuard` = 4 in production, 0 with `-tags fpgforce`;
long relative to the cover, the same rule as #20's stage-b guard). It
appends t to a scratch list and adds w(t) to rest. All other
transactions are counted exactly as before, with the same early exit.
If nothing was deferred, the function is the old scan (same visits, same
order, same early exit, same `work`), plus one length comparison per
transaction.

If transactions were deferred and no frequent extension was found,
`scanDeferred` (`extcheck.go:870`) visits them **shortest first**:

1. While rest ≥ smin (rest includes the current t), t is counted in
   full, as in the old scan (touched items recorded, early exit).
2. At the first t with rest < smin, the candidates are built once:
   alive = {j ∈ touched : cnt[j] ≥ smin − rest}. If alive is empty, X
   is maximal (return true).
3. For this and every later t: if |alive|·bitlen(|t|) < |t|, each
   candidate is searched in t (`slices.BinarySearch`, items of a
   transaction are ascending) and cnt[j] += w(t) if found, exit false at
   cnt[j] ≥ smin (a "lookup"); otherwise t is counted in full as in 1.
   Then rest −= w(t), alive is filtered by cnt[j] ≥ smin − rest, and an
   empty alive returns true.
4. After the last t, return true.

Counts are reset through `touched` as before; scratch slices
(`deferred`, `alive`) are reused; `c.work` adds item visits plus
|alive|·bitlen(|t|) per lookup (the sibling-row cost rule sees the real,
lower work). Counters `scanStat.deferred` and `scanStat.lookups` record
the paths for tests.

On lp: X = {i}, the long line is deferred (rest = 1 < 2), the one-item
line {i} touches nothing, alive is empty: true after 1 visit. On
lp50000s2: the two short lines give cnt(i−1) = cnt(i+1) = 1, alive =
both, two binary searches in the long line find i+1 at count 2: false.

### Proof of exactness

Claim: scanMaximal(n) returns false iff some j ∉ X has
supp(X ∪ {j}) ≥ smin. It suffices that both returns are sound (it always
terminates: finitely many transactions, each visited once).

Invariants, with P the transactions visited so far (in full or by
lookup) and rest = w(C \ P):
- (I0) For every j, cnt[j] is a sum of weights of distinct transactions
  of P that contain j, so cnt[j] ≤ supp(X ∪ {j}). (Full counting adds
  w(t) to every item of t ∖ X; a lookup adds it only to found
  candidates.)
- (I1) Before step 2, cnt[j] = cnt_P(j) for every j (only full counts).
  After step 2, cnt[j] = cnt_P(j) for every j ∈ alive (alive items are
  updated by every later t, by lookup or full count).
- (I2) After step 2, every j ∉ X, j ∉ alive has supp(X ∪ {j}) < smin.
  At step 2: an untouched j has cnt_P(j) = 0 and rest < smin; a
  touched j left out has cnt_P(j) + rest < smin; Lemma 2(b) for both.
  Later filters drop j only when cnt_P(j) + rest < smin with exact
  cnt_P(j) (I1): Lemma 2(b) again. A dropped j stays dead: its bound
  was over all of C.

Returns: false only at cnt[j] ≥ smin: sound by (I0). true at an empty
alive: sound by (I2). true at the end (or without deferral): every
transaction is in P; for j ∈ alive (or every j, before step 2) cnt is
exact (I1) and below smin (else the scan had returned false); other j
by (I2). ∎

Neither the visiting order nor the choice of deferred set, mguard, the
lookup rule or the shortest-first sort enters the proof: they change
only cost. The heavy-transaction condition w(t) < smin is likewise cost
only (a heavy t decides within |X| + 1 visits).

### Case by case

- **Weights.** Exactness needs w(t) ≥ 0 (Lemma 2), guaranteed (zeros
  dropped, negatives rejected). Weighted rest/cnt use `tract.Supp`;
  rest ≤ W ≤ total weight, which the bag already bounds against
  overflow. Lemma 1 for weights is covered: a deferred t with
  w(t) > W − smin leaves rest < smin after the others. Prototype check
  (unit, 4,000 items, long weight 1, short weights 1..20): 16,004,000 →
  4,000 visits.
- **-m / -n.** The checker exists only if ZMax < bag.MaxSize()
  (`mine.go:94`); maximality is over every item regardless of size, so
  zmin/zmax do not enter the check. n (cover size) is the checker's
  popcount, not a size limit.
- **Perfect extensions (#21).** The set passed to the check always
  includes the perfect extensions (`report.go:612-619`, built before the
  `pexMax` guard at `:632-641` resets only the count). cover(prefix) =
  cover(X) over positive weights (`checkPath` comment), and the pex items
  are marked in `inSet` and skipped. Were they missing, they would be
  found as extensions of count W ≥ smin, exactly as before; the proof
  is for whatever X is marked, with its cover.
- **Sibling rows (#19).** `sibMaximal` is unchanged. A child's ordinary
  check still goes through `decide` → `scanMaximal(n)` and adds its
  (now actual, smaller) work to the level; fewer builds can follow,
  which is the intended cost rule. The verify mode calls
  `decide(lv.n, 0)` (`extsib.go:200`): harmless, because the new scan
  does not read `supp` — a literal Lemma-1 implementation that trusts
  `supp` would be wrong there (W − smin < 0 makes every t qualify).
- **Item order / recoding / -q / dir.** The verdict is a set property;
  codes only change cover order and transaction order. The lookup
  requires ascending item codes within a transaction, which
  `scanClosed`'s merge already requires (`tract/bag.go:79`: items sorted ascending; `txItems`: "ascending codes per
  transaction", packed bits unpacked in ascending order before the other
  codes).
- **Maximum support (-S).** The reporter rejects s > smax before the
  check (`report.go:629`); maximality is relative to smin (`c.smin` is
  the miner's absolute minimum support), smax does not enter.
- **Empty set.** Handled before the scan (`check`, `len(set) == 0`).
- **smin = 1.** Every transaction is heavy, nothing is deferred: old scan
  (which stops at the first item outside X anyway).
- **Vertical path.** `vertMaximal` unchanged; `decide`'s estimate
  unchanged.

## 4. Where the bound does not help, and what the code does

1. No cover transaction is longer than |X| + 4n, or every long one is
   heavy: the old scan, unchanged.
2. Deferred weight ≥ smin (two or more long light transactions in the
   cover, e.g. lpc20000's suffix chains with `-tm`): the shortest
   deferred transactions are counted in full until rest < smin. Still
   Θ(|shortest long transaction|) per check. Prototype unit check (lpc
   shape, 4,000 items, 63 chains): 31,492,161 → 16,015,105 visits
   (shortest-first halves it; it stays quadratic). Possible follow-up:
   merge the two shortest long transactions until a common item reaches
   smin; not proposed here.
3. Many candidates (|alive|·bitlen(|t|) ≥ |t|): the deferred t is
   counted in full: the old cost for that transaction.
4. Deferred weight < smin but the short transactions themselves are
   long in total: their visits are needed anyway (old cost).

Cost bound against the old scan: without deferral, equal (+1 compare per
transaction). With deferral: Σ_short |t| + Σ_deferred min(|t|,
|alive|·bitlen|t|) + |touched| (one filter) + |def|·log|def| (sort),
at most about the old scan's full cost Σ|t| plus lower-order terms.
**Not bounded** against the old scan's *early exit*: if the old scan
stopped inside or right after a deferred long transaction (a frequent
extension found via the long line plus a later short line), the new scan
first visits all short transactions. Example: n = 100, one light
5,000-item line first, 99 lines of 300 items, an extension found at the
second line: old ≈ 5,300 visits, new ≈ 29,700 + lookups. This is the main
cost risk (§9) and the reason for the regression rows in §8.

## 5. Interaction with the closed path (#20): share code?

By Lemma 3, for W = smin the maximal check *is* the closed check and
could call `scanClosed(n)` / `vertClosed(W)` directly. Not proposed as
the main fix because (i) it needs W: the passed `supp` is 0 in the
sibling verify path and would have to be trusted on the weighted path
(or recomputed by a popcount per weight plane, up to 31·|nz| word ops);
(ii) it covers only W = smin, not lp50000s2 (W = 3) or slack in general;
(iii) scanClosed's probes, stage a/b and the adv5 ordering are tuned and
reviewed for intersection, and its `work` is not counted (the sibling
cost rule would see 0). What is shared: the length-relative guard idea
and its fpgforce lowering, `txLen`, the `scanStats` counters, the
fpgforce test harness. `keepCand`/`keepSeek` are not reused: maximal
needs counts per candidate, and `keepCand` must stay small enough to
inline (PR #35). Open question for review: whether the W = smin case
should additionally route to scanClosed (it is never worse than the
counting scan on k = 0 covers, which must visit every transaction).

## 6. Prototype status

`git diff origin/main --stat`: extcheck.go +111/−10 (scanMaximal,
scanDeferred, fields `deferred`, `alive`, `mguard`, two counters),
extcheck_thresh.go / extcheck_force.go (+`scanMaxGuard` 4 / 0),
extcheck_test.go (call sites pass n), new extscan34_test.go. Checks run
(light): `go build ./...`, `go vet ./...`, `staticcheck ./internal/fpg/`
clean; `go test ./internal/fpg/` ok (15 s); `go test -tags fpgforce
./internal/fpg/` ok (27 s). Coverage of the fpgforce oracle/brute/bits/
regression tests: scanMaximal 100 %, scanDeferred 100 %.

Mutants (fpgforce, oracle + brute + bits + regressions + scaling, about
3 s each):

| Mutant | Result |
| --- | --- |
| defer heavy transactions too (drop `w(t) < smin`) | survives: cost-only (verdict-equivalent, by the proof); to be killed by a work test (§7) |
| `rest < smin` → `rest <= smin` | killed |
| alive filter `>=` → `>` | killed |
| `rest -= 2·w(t)` | killed |
| return true when one candidate is left | killed |
| lookup counts the not-found candidates | killed |

## 7. Test plan (phase 2)

- `TestExtCheckMaximalMixedScaling` (prototype): lp and lp2 shapes,
  n = 20,000, all one-item checks; verdicts against the known answer;
  bound `c.work ≤ 64·n` (fails before the fix with 400,020,000), and
  `scanStat.deferred`/`lookups` > 0. Extend with: the weighted lp (long
  weight 1, short weights 1..20), the lpc chain shape (documents case 2:
  verdicts plus a bound of the shortest-first work), and a heavy-first
  case (a long transaction of weight ≥ smin first in the cover: work ≤
  |X| + 2 per check), which kills the surviving mutant. Plus the
  early-exit example of §4 as a documented cost guard (a bound, not a
  time).
- Oracle/brute tests (`TestExtCheckOracle`, `TestExtCheckBrute`,
  `TestExtCheckBits`, `TestExtCheckRegressions`, sibling-rows oracle and
  verify mode) unchanged, in both builds; with fpgforce (mguard = 0)
  every transaction longer than X and lighter than smin is deferred, so
  the brute force covers the new code on small random data (100 %
  statement coverage already).
- A direct property test of scanMaximal(n) against vertMaximal and a
  brute force on random covers with random weights, random mguard and
  forced lookups/full counts (seeded, count-bounded).
- Mutation re-run of §6 on the final code.
- Night: `FPG_M5_CASES=20000 go test -tags fpgforce -run
  'ExtCheckOracle' ./internal/fpg/`, the fpgforce brute/difftest sweep
  used for #20, and `(cd reference && ./build.sh && go test ./...)`.
- Bench rows to add to `reference/bench/configs.txt`: `lp-12500-m-As`,
  `lp-25000-m-As` (scaling with `lp-50000-m-As`), `lp-50000s2-m-n1`,
  `lp-50000w-m` (-w), `lp-c20000-m` (case 2, documented as still
  quadratic).

## 8. Measurement plan (deferred to the night window)

Load-independent metric first: instructions retired and user CPU from
`/usr/bin/time -l`, 2 runs each, interleaved, `uptime` and top CPU
consumers recorded per round; wall time secondary.

1. Baselines once (origin/main `e6d02b0`, v0.1.0), saved under
   `~/.cache/fpgrowth-cycle/34/baseline/`: the lp maximal rows only at
   12.5k/25k/50k (and C once for context).
2. Scaling: `lp-12500-m-As`, `lp-25000-m-As`, `lp-50000-m-As` on the
   branch (expect linear: instructions about ×2 per doubling instead of
   ×4); `lp-50000s2-m-n1`, `lp-50000w-m`; `lp-c20000-m` to document the
   remaining case.
3. Which ordinary rows the change touches: a one-off instrumented run
   (scanStat.deferred) on the maximal rows that use the checker
   (`large-m-n2`, `large-m-n3`, `m5-d2000-m-n2/n3`, `m5-d1000-m-n2`,
   `m5-d5000-m-n3`, `m5-d20000-m-*`, `m5-tier-m-n3*`, `m5-split-m-n3*`).
   Rows with zero deferrals run the identical scan; they get one
   instructions check (2 runs, main vs branch) to bound the compare's
   overhead. Rows with deferrals get the full main/branch comparison and
   their deferral and early-exit counts. The rows without `-n`
   (`sparse-m`, `large-m`, `dense-m`, `long-m`) do not build the checker
   and are not touched.
4. The independent performance critic does the single full interleaved
   main / PR / v0.1.0 run before the merge decision.

## 9. Risks

- Reordering vs early exit (§4): an ordinary maximal row with deferrals
  could get slower. Mitigation if measured: defer only while the
  deferred weight is below smin (a deferral that cannot lead to
  rest < smin buys nothing), or cap Σ_short visits relative to the first
  deferred transaction's length and fall back to the old order.
- Decision depends on `scanMaxGuard` = 4 (borrowed from #20, not tuned
  for maximal); the night instrumentation (§8.3) should show whether
  ordinary rows ever defer.
- Case 2 (deferred weight ≥ smin) stays quadratic (lpc `-tm`); to be
  stated in docs/benchmarks.md, not hidden.
- 32-bit: `mguard·n` is int, as `c.guard*n` in scanClosed; fine for any
  bag that fits in memory, but the reviewer may prefer int64.
- The sort of the deferred list allocates nothing but costs
  |def|·log|def|; with mguard = 0 (fpgforce) |def| can reach n, which
  is only a test-build cost.
- Docs to update in phase 2: GoDoc of scanMaximal/extChecker overview,
  docs/m5-investigation.md §4, docs/benchmarks.md (lp maximal rows),
  reference/bench README/configs, CHANGELOG `[Unreleased]` (Fixed:
  performance). No behavior difference from upstream (verdicts
  unchanged), so no docs/differences.md row.

## 10. Open questions for review

1. Accept the W-free generalization (Lemma 2) instead of the literal
   shortest-t-with-w(t) > W − smin rule?
2. Also route W = smin to scanClosed (Lemma 3), or keep one counting
   path?
3. Is the early-exit reordering risk acceptable as measured, or should
   deferral be limited up front (§9 mitigation)?
4. Is case 2 (lpc `-tm`) in scope for #34 or a follow-up issue?

## 11. Phase 2 addendum (after the challenge, 2026-10-26)

Binding decisions applied: W-free Lemma 2, one counting path, the
deferral cap, the lookup hook (`scanParams.lookup`: auto / always /
never), the heavy-first and cap tests, no closures on the warm path
(`c.alive` set explicitly, no sort), scanMaxGuard = 4, plain int.

Cap as implemented: the budget is |t1| (the first deferred
transaction), not the total length of all deferred transactions, and
the deferred transactions are visited in cover order (the
shortest-first sort is gone). Reason: with a budget of the total
deferred length the bound does not hold — deferrals after the plain
scan's exit raise the budget, and the scan can visit short
transactions the plain scan never reaches (example: plain exits at the
first short line after t1; then 100 more long lines interleaved with
short ones of total length 100·|t1|). Bound proved (code comment of
scanMaximal, m5-investigation §4): item visits ≤ V + |t1| ≤ 2·V, V the
plain scan's visits up to its exit with t1 counted in full; checked on
every call of the randomized property test (lookup modes auto and
never). Cost of dropping the sort: lpc `-tm` loses the halving (it
stays quadratic, out of scope); with fpgforce (mguard 0) the lp2 test
sets mguard = 4.

Mutants (9, all killed; ~/.cache/fpgrowth-cycle/34/mutants.txt):
heavy deferral (HeavyFirst), no cap and budget 2·|t1| (Cap, property
bound), flush without counting, rest ≤ smin, filter >, rest −= 2w,
empty-candidate test at 1, inverted search result.

lpc follow-up assessment: the candidate bound cnt + rest − w(t_s) from
the shortest deferred transaction prunes only when rest − w(t_s) <
smin, i.e. one long transaction more than needed; on lpc the chains
of item i number ⌊i/64⌋ + 1, so it almost never applies (small effort,
~20 lines, little gain). What would fix lpc is an order that finds a
common item early: count the two (or all) deferred transactions
interleaved, item by item, so that an item in two light chains reaches
smin = 2 at its first common position (exact for any order; moderate
effort, ~60 lines plus a cost bound against the plain scan).

## 12. Round 2: review F1/F2 (2026-10-27)

F1 (perf critic): the bound of §11 charged t1 in full (c.work counts
whole transactions), but the plain scan can stop inside t1 after a few
items; deferral lost that stop. Shape advB (AG(2,q): {b,z,x} lines, a
long light T = all points + b, the q+1 lines of the plane; -tm -s-2 -n1
-q0): about 3 visits per check plain, about q² deferred; 34.1 G vs
7.87 G at q = 151.

Fix (precheck): an item j stops the plain scan inside a light t iff
j ∈ t and cnt[j] ≥ smin − w(t) before t (an untouched item has cnt 0,
and w(t) < smin). Before the first transaction t1 is deferred, the set
C1 of touched items with cnt ≥ smin − w(t1) is computed (one pass over
touched) and searched in t1 by binary search if |C1|·bitlen(|t1|) <
|t1|:
- C1 empty: defer t1 (the plain scan cannot stop in t1);
- a hit: X is not maximal (j ∈ t1 reaches smin with t1; the plain
  scan's verdict, found at about the same point);
- no hit: defer t1; the plain scan visits all of t1;
- search too expensive (≥ |t1|): t1 is not deferred, it is counted in
  cover order as by the plain scan, and the next long light
  transaction is the next candidate for t1.
Exactness is unchanged (a hit is a frequent extension; no verdict is
inferred from a miss).

Bound in actual item visits (R: items counted, plus one per binary-
search probe; scanStat.skipped subtracts the items after a stop). Let
V be the plain scan's actual visits. Then

    R ≤ 2·V + S,

S = probes of the precheck of t1 and of the lookup in the transaction
where the plain scan stops; each of the two is below that
transaction's length and at most bitlen(|t|) per candidate (an item
already visited). Proof: before t1 the scans are identical. If t1 is
deferred, the plain scan does not stop before the end of t1 (precheck),
so V ≥ pre + |t1|. After t1 the deferral counts at most |t1| items of
short transactions out of order (cap). All other work is in cover
order, the deferred transactions after the counted ones before them,
so at every transaction the counts are at least the plain scan's and
the scan stops at or before the plain scan's stop: in a full count at
or before the same item; in a lookup of the stopping transaction with
S_exit probes. A lookup of an earlier deferred transaction costs less
than the plain scan's full visit of it. So R ≤ V + |t1| + S ≤ 2V + S.
Candidate filters are passes over touched items (visited before) and
are not counted.

F2: the CHANGELOG, m5-investigation, the cap test comment and the PR
body now state the bound in these units; the tests measure actual
visits (measureScan: work − skipped), and the property test asserts
R ≤ 2·V + S on every call except lookupAlways (which ignores the cost
rule by design). The property test also draws bags of 65-264
transactions (several bit-set words; a word-reload mutant survived the
single-word bags).

Mutants (13, all killed): heavy deferral, no cap, budget 2|t1|, word
reload, flush without counting, rest ≤ smin, filter >, rest −= 2w,
empty-candidate test at 1, inverted search, precheck disabled
(EarlyExit), precheck threshold off by one both ways (EarlyExit).

Round 2b (correctness critic note, TestCritic34PrecheckPasses): when the
precheck declined (search ≥ |t|), the next long light transaction ran
its own precheck, each with a pass over all touched items: one pass per
long transaction, up to 62x slower than the plain scan on one check
(critic, n = 800). Now a declined precheck ends the deferral for the
check (the scan goes on in cover order, as the plain scan), and the
pass stops as soon as the candidates would cost |t|. One precheck per
check; its pass costs at most the touched items, i.e. the items visited
before. Test TestExtCheckMaximalOnePrecheck (4,000 touched items of
count 1, then 300 of count 2, then 50 long light transactions, smin 3):
pass 4,070 ≤ 39,401 actual visits; the mutant that keeps prechecking is
killed (14 mutants killed in all).

- Round 3 (critic F2-F5): bound includes the precheck pass P <= pre, so actual work <= 2V + S; tests count P; TestExtCheckMaximalCapSecondLong added (budget-reset mutant killed; 15 mutants killed in all); stale shortest-first wording fixed; CHANGELOG wrapped. Critic shape TestCritic34PrecheckPasses on bc00619: plain = deferral (1.4/5.5/22 ms at cover 400/800/1600).

## 13. Round 3: candidate filters (G1 / R2-F1, 2026-10-27)

scanDeferred filtered alive after every deferred transaction: D·|alive|
in all, outside the stated bound and not counted by measureScan. advC
(k short lines of weight smin−2 with 4n items, then D long light
lines; -w -As -tm -s-1000 -n1 -q0) took +94 % instructions at
k/D = 300/750 and grew like n³.

Fix: filter only once the visits and probes since the last filter
reach |alive| (`since`). Exactness: an unfiltered alive is a superset of
the live candidates; every item left out was dropped by cnt + rest <
smin (dead for good); the extra (dead) items get partial counts below
their support, so they never stop the scan, and alive counts stay
exact. The empty-candidate exit may come later (cost only).

Bound, now counting every pass (precheck pass P, candidate build B,
filters F): visits + P ≤ 2V + S (§12); B ≤ |touched| ≤ visits before
it; each filter costs at most the visits since the previous one, so
F ≤ visits after B; hence all work ≤ 2·(2V + S) = 4V + 2S. S ≤
bitlen(|t1|)·pre + bitlen(|t|)·|candidates|: a log factor, not a
constant times V (R2-F2).

measureScan: visits (as before) and real = visits + filtered (B and F);
`within` asserts visits ≤ 2V + S and real ≤ 4V + 2S, used by every
work test and the property test. New TestExtCheckMaximalFilterPasses
(advC, k = 40, D = 100): real 168,440 (filters 67,200) vs plain 78,840;
the old per-transaction filter mutant fails it. 17 mutants: 16 killed;
"never filter" survives (cost only, within both bounds; it only delays
the empty-candidate exit).

Round 4 (correctness H1 / perf R3-F1, Low): precheck's search loop went
over all touched items a second time, uncounted (true bound about
5V + 2S). Fixed by option (b): the one counted pass collects the
candidates into the (still unused) scratch of scanDeferred, and the
search iterates only these, one probe batch of bitlen(|t1|) ≥ 1 counted
probes per candidate. Every pass is now counted and the bound stays
4V + 2S; docs and tests unchanged.


# Design challenge


Verdict: ACCEPT WITH CHANGES. Proof holds; no counterexample. Changes concern cost and tests.

## Work done (light)
- Diff read against the three invariants.
- Randomized property test internal/fpg/challenge34_test.go (clone only): ~190k scanMaximal(n) calls vs brute-force maximality from source transactions; bags of 3-62 items, half lp-like (sparse short lines + every 7th line ~95% full); weights 0..3, occasionally up to 2^20; smin 1..6, occasionally up to 2^20; mguard forced to {-2^30, 0, 1, 4}; cnt reset checked after each call. PASS in default and fpgforce (13 s each); ~85k calls deferred, ~1k used lookups.
- go test -tags fpgforce ./internal/fpg/ ok (27.7 s); GOOS=linux GOARCH=386 go vet ./internal/fpg/ clean.

## Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| C1 | MEDIUM | confirmed (analysis) | Lost early exit: new work bounded by the old full scan, not by the old early exit; worst case ~n× per check (shorts of up to \|X\|+4n items visited before a long t the old scan would have finished on). |
| C2 | MEDIUM | hypothesis | Unknown whether ordinary -tm -n rows defer (guard \|X\|+4n can fire on webdocs-like data); needs night instrumentation. |
| C3 | LOW | confirmed | Lookup path thinly covered by random data (981 of 191k calls); force it in the property test. |
| C4 | LOW | hypothesis | Lookup work reported as \|alive\|·bitlen lowers the #19 sibling cost signal; sibling-heavy rows might slow. |
| C5 | LOW | confirmed | scanDeferred uses a deferred closure and a SortFunc closure on a warm path; set c.alive explicitly. |

## Verified correct
I0 (each transaction's weight added once per item), I1 (alive items counted in every later transaction), I2 (items first touched after the candidate step are already dead), rest includes current t; BinarySearch valid (sorted, deduplicated items; packed prefix unpacked ascending); weight-0 dropped, Supp int64, smin=1 never defers; mguard·n overflow on 32-bit only defers more (allowed), needs >5·10^8 transactions; sibling verify decide(lv.n, 0) safe (scanMaximal does not read supp); #21, -m/-n, -S unaffected.

## Requested changes
1. Cap deferral up front: defer only while visits of counted short transactions after the first deferral ≤ total length of deferred transactions; otherwise count the deferred ones in full and resume the old order. Gives new work ≤ ~2× the old scan up to the first deferred transaction.
2. Night instrumentation for C2/C4 incl. early exits lost to deferral.
3. Property test forcing the lookup path.
4. Heavy-first work test killing the surviving mutant.

## Answers
1. Accept the W-free Lemma 2 (literal Lemma 1 wrong on the verify path, which passes supp 0).
2. Keep one counting path (no routing to scanClosed).
3. Early-exit risk not acceptable unbounded: add the cap now.
4. lpc -tm (deferred weight ≥ smin) is a follow-up; state it stays quadratic in docs/benchmarks.md and CHANGELOG. Idea: candidates from the shortest deferred transaction with bound cnt + rest − w(t).
5. scanMaxGuard = 4 acceptable, tune from instrumentation; plain int enough.
