# Design and design challenge for #19

Written before the implementation. The build rule was refined twice after the review of PR #33 (actual-work trigger with a remaining-children bound, then history scaling below the maximum-size level); see the review records.

## Design proposal

## Issue #19 (M5A-1): design proposal for the size-limited maximal check

Branch `perf/19-maximal-size-limit` (phase 1 commit `b5098d4`: pinned
dataset `d20000x200d30`, `m5-d20000-*` rows in `configs.txt`, baseline in
`docs/benchmarks.md`). Machine: Apple M1 Max, Go 1.27.2, load 3.3–12.8.

### 1. Baseline (5 interleaved runs, median [range], wall s / max RSS MB)

| `d20000x200d30` | sets | Go main `3c0b7e8` | Go v0.1.0 | C 6.21 native |
| --- | ---: | --- | --- | --- |
| `-tm -s2 -n3` (target) | 1,313,400 | 97.06 [97.03–97.20] / 111.6 | 97.47 [97.20–97.76] / 111.0 | 39.21 [39.07–39.65] / 1259 |
| `-tc -s2 -n3` | 1,333,500 | 10.31 [10.22–10.34] / 111.3 | 10.24 / 111.0 | 39.26 / 1259 |
| `-tm -s2 -n2` (n=3) | 0 | 0.63 / 107.6 | 0.62 / 107.7 | 8.40 / 1259 |
| `-tm -s1 -n4` (n=2) | 1,211,590 | 131.4 / 111.0 | 130.9 / 110.9 | — |
| `-tm -s2 -n3 -As/-Ac/-Ad/-At` (main, n=2) | 1,313,400 | 96.0 / 97.1 / 95.5 / 119.2 | — | — |

The counts are the audit's: every 3-set is frequent at `-s2` (smin 400),
no 4-set is. main = v0.1.0 within noise everywhere (`extcheck.go`
unchanged since v0.1.0); the representative set (dense/sparse/
sparse-large/dNxKdP/weighted, closed/maximal, n2–n4, 5 runs each) is in
`scratchpad/m5a1/baseB.tsv`; Go benchmarks (`CloMaxLimited`, `Mine`,
`SuiteMaxSize`, `SuiteVariants`, 6 interleaved rounds; `benchstat-*.txt`):
v0.1.0 → main all `~` except 7 of 60 time rows at −2.2 … +2.4 % (p < 0.05).

### 2. Profile of the target (`-tm -s2 -n3`, pprof, in-process)

- 91 % in the check (`checkPath` cum 83.4 of 91.6 s), 86 % in
  `scanMaximal` alone; mining (`projCmplx`, `csCondSupp`, `addCmplx`)
  ~9 %; `and` (cover update) 1.3 %. Allocations: the checker 4.6 MB of
  129 MB allocated in all; RSS 111 MB. Not an allocation problem.
- `scanMaximal` lines: the item loop over the cover transactions
  (`txItems` 21 s), the `inSet` test (16 s), `cnt==0` touched test
  (13 s), `cnt += w; >= smin` (23 s): ~2.1 ns per item visit, random
  access into `cnt`/`inSet`.
- Instrumented counts (temporary code, not committed): 1,313,400 checks,
  all with prefix length 3 and no perfect extensions; 1,166,380 take the
  scan path, 147,020 the vertical path. Per check: cover n ≈ 539
  transactions in nz ≈ 239 of 313 words; h ≈ 32,400 item visits, v ≈
  47,700 word tests. **Every check is maximal**, so none can stop early:
  each counts its whole cover.
- Repeated work: 19,701 parents P (2-sets) with 66.7 checked children
  P∪{h} each (histogram: 8.5k parents < 50 children, 1.2k ≥ 150). The
  parent cover has ≈ 1,798 transactions (297 words). Every child check
  rescans 539 transactions of 61 items that all lie inside cover(P), and
  recomputes supp(P∪{h}∪{j}) for the same 197 items j.
- **The auditor's bound prunes nothing here**: supp(P∪{j}) ≥ smin for
  198.0 of 198 items per parent (expected 20,000·0.3³ = 540 > 400). The
  pairwise bound supp({h,j}) ≈ 1,800 is useless too. On sparse-large
  (`-tm -n3`) the bound would leave 4.4 of 2,592 items, but there checks
  are already cheap scans (h ≈ 1,500) and 2.4 children per parent.
- Cost-model experiment (temporary build forcing the vertical path for
  every check, one run each): target 97.1 → 56.8 s; `dense -tm -s2 -n3`
  0.85 → 0.62 s; `d5000x60d40 -tm -s5 -n3` 0.24 → 0.24 s; but
  `sparse-large -tm -s0.05 -n3` 0.81 → 28.4 s. Measured unit costs on the
  target: scan ≈ 2.1 ns per item visit, vertical (dense rows) ≈ 0.8 ns
  per word test, i.e. `decide`'s h ≤ v treats a scan visit as ~2.7×
  too cheap for dense rows.

### 3. Candidates

All candidates keep what is computed (supp(X∪{j}) for every j ∈ E \ X,
or a proof that it is < smin) and only change how. The proof of
`docs/m5-investigation.md` §3.2 depends "only on what is computed, not
how"; each candidate must show that its verdict equals the complete
one-item test of §3.1 for every item (maximal: no frequent superset of
any size iff no frequent one-item extension).

**A. Parent-condensed bit sets (proposed core).** For the parent level
L = |prefix| − 2 ≥ 0 of `checkPath` (P = prefix[:L+1], cover C = path[L],
n_P = |C|, positions r = 0..n_P−1 for the cover transactions t_r in
ascending order), build once per parent, from the horizontal form: for
every item j ∉ P with supp(P∪{j}) ≥ smin a row R_j of ⌈n_P/64⌉ words
(bit r set iff j ∈ t_r), the condensed weight planes (bit r of plane b
iff bit b of w(t_r)), and supp(P∪{j}) itself. A child X = prefix ∪ pex,
prefix = P∪{h}: cover(X) = cover(P∪{h}) ↔ R_h (§3.2: pex do not change
the cover), supp(X∪{j}) = Σ_b 2^b·popcount(R_h & R_j & plane_b).
- Exactness: r ↦ t_r is a bijection onto C and cover(X) ⊆ C, so the
  popcounts are the same sums over the same positive-weight
  transactions as `suppWith`. Items j with supp(P∪{j}) < smin are
  decided by anti-monotonicity (positive weights, M6): supp(X∪{j}) ≤
  supp(P∪{j}) < smin. So every j ∈ E \ X is decided (§3.2 completeness
  bullet). Closed: j violates closedness iff supp(X∪{j}) = supp(X) ≥ smin
  (reported sets are frequent, §3.4), hence supp(P∪{j}) ≥ smin: the
  same filter is exact; the test is R_h &^ R_j == 0. If h itself is not
  a condensed row (impossible for a frequent child), fall back to the
  existing path.
- Cache rule: a function of path[L] only, so invalidated exactly when
  `checkPath` recomputes a level ≤ L (k0 ≤ L) or the prefix length
  changes; `check` (empty prefix) and |prefix| = 1 (parent = whole bag,
  no gain) never use it. One condensed level, buffers reused.
- When to build (no workload special case; ski rental): accumulate the
  estimated cost min(h, v) of the checks under the current parent; build
  when it reaches the build cost B = Σ_{t∈C} |t| + k and the condensed
  per-child cost |E_P|·⌈n_P/64⌉·planes is below min(h, v). Worst case:
  at most B extra per parent on top of work already spent (≤ 2× the
  check cost of that parent), and never for parents with one or two
  cheap children (sparse data).
- Memory cap: build only if |E_P|·⌈n_P/64⌉ words ≤ 2·Σ_{t∈C}|t|
  (≤ 16 bytes per item occurrence of C, ≤ 16·O in all; target:
  198·29·8 = 46 KB). Keeps Go's ~11× memory advantage over C.
- Expected gain: per child 198 × 29 = 5.7k sequential word ANDs+popcounts
  instead of 32k random item visits (or 47.7k gathered word tests);
  build 1,798 × 61 ≈ 110k visits per parent ≈ 19.7k × 0.2 ms ≈ 4 s.
  Estimate for the target: check 83 s → ~8–12 s, total ~17–22 s
  (< C's 39 s). To be confirmed by a prototype in phase 2.
- Risks: invalidation bugs (stale rows after a prefix change), the
  position mapping, weight planes, `-x`/pex and packed items (checker
  unpacks them when built), sparse-row forms (rows built from the
  horizontal form, so the row form does not matter). Extra code ~150
  lines in `extcheck.go`.

**B. Recalibrate `decide`.** Weight the unit costs (scan visit vs word
test, separately for dense rows, sparse rows and planes) from
measurements. Exact by construction (both paths exact, forced by
`TestExtCheckBrute`). Gain on the target 97 → ~57 s alone, `dense -tm`
−27 %; with A it matters only for checks that A does not cover (prefix
length 1, first children before the build). Risk: sparse-large shows
that the vertical path can be 35× slower; the factor must be per row
form, and measured on every M5 row. Proposed as a small, separately
benchmarked follow-up inside this issue, kept only if it is a
non-regression everywhere.

**C. `scanMaximal` micro-optimizations** (e.g. one combined stamp/count
array, set items pre-marked so the `inSet` test disappears). Exact
(same sums). Gain ≤ ~30 % of the scan, which A mostly replaces; helps
sparse maximal scans. Optional, only with benchstat evidence.

**D. Auditor's sibling bound alone** (one pass over cover(P), then only
items with supp(P∪{j}) ≥ smin are tested per child). Exact (anti-
monotonicity). Prunes 0 of 198 items on the target, so no gain there; it
is subsumed by A as A's row filter (and candidate order: test j in
descending supp(P∪{j}) so non-maximal children stop early).

**E. Pairwise / symmetric caching** of supp(P∪{h,j}) for sibling pairs
(both h and j children of P): up to 2× less popcount work after A,
memory |E_P|²·8 bytes. Deferred unless A's remaining check time is
still dominant.

**F. Popcount improvements** (unrolled 4-word loops, `bits.OnesCount64`
already maps to VCNT on arm64). Small, part of A's inner loop tuning.

### 4. Proposed choice

A (with D as its filter and order), then B if it still pays, C/F only
with evidence. Order of work: prototype A, measure the target and the
representative set, then harden (cap, ski-rental rule, tests), then B.
Interaction with #20: A does not touch `scanClosed`'s candidate start
(the NEW-3 shape has |prefix| = 1, so A never applies there); keep the
two PRs separate and re-run both issues' benchmarks after either lands.

### 5. Correctness test plan

- Extend `docs/m5-investigation.md` §3.2/§4 with the condensed path
  (bijection argument, anti-monotone filter, cache invalidation rule);
  independent review of that extension.
- `TestExtCheckBrute`: a hook forcing the condensed path on (build at the
  first child, no cap) and off; every call against the source-
  transaction brute force, all three row forms, weights incl. 0 and
  > 1 (planes), `-x`, all 11 variants; and paths A/scan/vertical must
  return identical verdicts.
- New `TestExtCheckCondensed`: random parents/children, R_h & R_j
  popcounts (with planes) against `suppWith`; random `checkPath` prefix
  sequences (DFS-like and arbitrary jumps at every depth) against a
  fresh checker per call (invalidation).
- Mutants that must be caught: filter `>` instead of `≥`, missing
  invalidation at level L, off-by-one position, ignored planes, filter
  applied with the child's instead of the parent's support.
- Documented counts: `FPG_M5_CASES=10000 FPG_ORACLE_CASES=5000
  FPG_M5_UNLIMITED=300 go test ./internal/fpg -run
  'TestExtCheck|TestOracle$|TestDeviations|TestSeek' -v`.
- Independent brute forces `reference/_audit/m5/bf` (dense, 11 variants)
  and `bfs` (sparse), copied back per `reference/_audit/README.md`, over
  size limits 1–4, weights and zero weights: 0 failures required; plus
  a randomized campaign with denser data (≥ 30 % density, many children
  per parent) so that the condensed path is actually exercised (count
  the condensed builds in the test).
- `go test ./...`, `-race`, vet, staticcheck, `(cd reference &&
  ./build.sh && go test ./...)`.

### 6. Benchmark plan

- Target and neighbors (`m5-d20000-*`: c/m n2, n3, m n4, -As/-At),
  n ≥ 5 interleaved main / v0.1.0 / branch / C, wall, CPU, RSS, uptime.
- All M5 rows `bench/compare.sh -n 5 -m devnull
  '^(m5-|large-[cm]-n)'`; sparse-large `-tm -n2/-n3`, the closed target.
- benchstat (count 6, interleaved): `BenchmarkSuite*`, `internal/fpg`
  `BenchmarkCloMaxLimited`, `BenchmarkMine`; no repeatable regression
  > ~5 % (p < 0.05) without explanation.
- Memory: RSS of the target before/after; condensed-buffer peak size.

### 7. Open questions

1. A also for closed checks (closed target 10.3 s, check ≈ 30 % of it)?
   Proposed yes if measured beneficial, same code path.
2. Should a `vs1M`-like large sparse generator spec (audit's 1M
   transactions, `-n1`) be added for the "no regression" check, or is
   sparse-large enough? (A never applies at `-n1`; B could.)
3. Is a dataset with the same shape but different bytes than the
   audit's acceptable as "the" M5A-1 workload (counts and times agree)?

## Independent design challenge

## Challenge of the #19 (M5A-1) design (design19.md)

Reviewer: independent FP-growth / performance specialist; did not write the
design. Read-only with respect to the repository. Evidence comes from a
throwaway prototype of design A in a scratch clone of `main` (`3c0b7e8`),
`scratchpad/review/design19` on branch `proto19` (never pushed). Files:
`internal/fpg/zz_cond.go`, hooks in `extcheck.go`, harness
`zz_prof_test.go`. Results are in `scratchpad/review/runs.out`. Every
timing is **one in-process run** (`BenchmarkZZProf19`, `-benchtime 1x`) on
an Apple M1 Max at load average 4–6. The timings show direction only;
they are not acceptance numbers.

Prototype switches: `FPG_COND=force|est|act` (build at the first child /
ski rental on estimated cost / ski rental on actual work),
`FPG_COND_MULTI=0|1` (one cached level as in the design / one cache per
path level), `FPG_VERIFY=1` (also run `decide` and panic if the verdicts
differ). Rows are built in two passes over the horizontal form of
cover(P). Items are kept iff supp(P∪{j}) ≥ smin, sorted by descending
supp(P∪{j}). The condensed weight planes are built as in the design.

### Verdict

**Design A is correct, and it is the right core. Proceed, with the
changes below.** The prototype brings the target from 97.5 s to
23.6–24.2 s, close to the design's 17–22 s. A forced-on prototype passes
`TestExtCheck*|TestOracle$|TestDeviations` with `FPG_M5_CASES=3000`, all
variants, and both cache modes, and the design's mutants are caught. Two
parts of the cost model are wrong as written:

- The ski-rental trigger on *estimated* cost (F1, High).
- The single cached level, which thrashes at `-n4` (F2, Medium).

The test plan also has a coverage gap: multi-word rows (F4, Medium).

### Findings

#### F1 (High): ski rental on estimated cost breaks the "≤ 2×" bound

The design accumulates the estimate min(h, v) of the checks under a
parent. A scan check stops at the first frequent extension, so on
non-maximal checks the estimate can exceed the work actually done by
orders of magnitude. The design's worst-case claim ("at most B extra per
parent on top of work already spent") holds only if the accumulated
quantity is the *actual* work.

Counterexample (`d20000x200d30 -tm -s-2 -n2`, smin 2, 19,869 checks, all
non-maximal):

| mode | builds | build visits (pass 1) | check work before build | wall |
| --- | ---: | ---: | ---: | ---: |
| main | 0 | 0 | 5.87 M visits in all | 0.583 s |
| A, estimated cost | 187 | 68.0 M (×2 passes) | 0.36 M | 0.740 s (+27 %) |
| A, actual work | 0 | 0 | 5.87 M | 0.558 s |

The check work becomes ~23× larger; the run gets +27 % slower in total.
In general the extra cost per level is bounded by Σ over the parents of
Σ_{t∈cover(P)} |t|, i.e. one horizontal pass over every parent cover.
With a very cheap miner (heavily overlapping transactions, low smin, all
checks non-maximal) this can dominate the run.

**Fix:** accumulate the actual work (item visits of `scanMaximal` up to
its stop, word tests of `vertMaximal`/`suppWith` up to its stop). The
prototype does this with a counter; it changes nothing on the target:
23.6 s with actual work vs 24.2 s with estimates, and 18,335 builds
either way. Also gate the per-child choice "condensed vs today" on an
*actual-work* history if needed. Its estimate |E_P|·W·planes is a full
cost against a full cost, so it is fine as is. Add this row as a
no-regression benchmark.

#### F2 (Medium): one cached level thrashes when zmax ≥ |P| + 3

All miners report in post-order (Add → subtree → Report → Remove). This
holds for `simple.go`, `complex.go`, `single.go`, `topdown.go`, and the
16-items machine in `fim16.go` (`recPos`, `recNeg`, `pair`, `single`). So
the checks of P's children are contiguous **except** that each child's
own subtree checks come between them. At `-n4`, the 3-set checks under
the 2-set parent P are each followed by the 4-set checks under P∪{h}.
The design invalidates on "the prefix length changes", which resets the
level-L cache and its accumulator after every child:

| `d20000x200d30 -tm -s1 -n4` (1,211,590 checks) | builds | condensed checks | wall |
| --- | ---: | ---: | ---: |
| main | 0 | 0 | 131.3 s |
| A, one level (design), actual work | 56,487 | 795,907 | 95.5 s |
| A, one cache per path level, actual work | 20,569 | 1,022,415 | 70.8 s |

**Fix:** keep one cache and one accumulator per path level, and
invalidate level L iff `checkPath` recomputes a level ≤ L. In the
prototype this is `condReset(k)` for every level ≥ k, the first
recomputed level. It needs no "length changed" rule and is exact: the
cache is a function of path[L] only, and path[L] is valid iff k > L.
This also covers prefix shrink-then-regrow. The memory is the sum over
nested levels. Bound the total, e.g. Σ_L |E_L|·W_L ≤ 2·Σ_{t∈bag}|t|
words, and evict the deepest level first. Covers shrink with depth, so
this is rarely binding.

#### F3 (Low): the 17–22 s estimate is plausible but optimistic; the remaining time is ski-rental waste and build

Target, prototype (actual work): 23.6 s. Breakdown from the counters:

- Pre-build checks: 79k of 1,313,400 checks, 2.50 G item visits ≈ 5 s.
  That is about 4 children per parent, because B ≈ 110k visits ≈ 3.4
  child scans.
- Build: 18,335 builds × 110k visits × 2 passes ≈ 4 G visits ≈ 4–6 s.
- Condensed popcounts: 6.89 G words ≈ 2–3 s.
- Mining: ~9 s.

Cheap improvements that keep exactness:

1. A one-pass build: count and set bits for every item, then compact or
   drop the rows with supp < smin. This needs k·W temporary words, which
   are within the cap on dense data.
2. A lower threshold, e.g. B/2. Any constant keeps the bound; the ratio
   becomes 1 + 1/c.
3. Optionally, start at the first child if the previous parent at the
   same level built and used ≥ m children. Its worst case is bounded by
   alternation, so it needs the same actual-work cap.

With 1 and 2 the estimate of ~18–20 s looks reachable. None of this needs
B.

Other rows (single runs, so ±10 % noise):

| row | main | A |
| --- | ---: | ---: |
| `dense -tm -s2 -n3` | 0.79 s | 0.78–0.79 s (1,283 builds; no gain, check share small) |
| `d5000x60d40 -tm -s5 -n3` | 0.227 s | 0.247–0.252 s (**possible +9 %**) |
| `sparse-large -tm -n2`, `-n3` | — | 0 builds (ski rental never fires); equal within noise |
| d2000, d1000 | — | 0 builds; equal |

The d5000 row needs benchstat; it is the case where B ≈ the work before
the build and the children are few (~20 per parent).

#### F4 (Medium): the repository's per-call brute force never sees multi-word condensed rows

`genM5Case` (`TestExtCheckOracle`, `TestExtCheckBrute`) uses at most 40
transactions, so every condensed row is one word. The bit-position
mapping (r>>6, r&63, the partial last word) is then exercised only by
`TestExtCheckUnlimited` (40–200 transactions), and only when the
condensed path is forced. With the production rule it rarely triggers
on such small data.

Mutant `wi := 0` instead of `r >> 6`: caught **only** by
`TestExtCheckUnlimited` under force. The per-call comparison
(`bruteCheck`) never fails on it.

**Fix:** add forced-path brute cases with 65–400 transactions, so that
covers cross word boundaries and have ragged last words. Add a direct
unit test of condensed popcounts against `suppWith` on random covers of
1–10 words with planes. Assert counters in the test: the number of
builds with W ≥ 2, builds with planes, and builds under `-x`.

#### F5 (Low): correctness of A, item by item (no defect found)

- **Completeness over E \ X:**
  - An item j that is absent from cover(P) has supp(P∪{j}) = 0 < smin,
    so it gets no row and is decided by anti-monotonicity:
    supp(X∪{j}) ≤ supp(P∪{j}) < smin.
  - Items of P are excluded (they are in X).
  - All other items with supp(P∪{j}) ≥ smin get a row.
  - The child item h always has a row: supp(P∪{h}) = supp(prefix) =
    supp(X) ≥ smin. `Report` filters s < smin before `cmCheck`.
  - Correct only with weights ≥ 0 (M6); that is the existing assumption.
- **Weights:** zero-weight transactions are already dropped from
  `txW`, so C has only positive weights. Condensed planes are needed iff
  `c.planes != nil`. Σ_b 2^b·popcount(R_h & R_j & plane_b) is the same
  sum over the same transactions as `suppWith`. Mutant "ignore planes"
  is caught.
- **Perfect extensions:**
  - The pex of P (rows that are all ones in C) and the pex of X must be
    skipped via `inSet`, not dropped at build time.
  - Mutant "drop rows with supp(P∪{j}) = supp(P), assuming pex" is
    caught only by `TestExtCheckRegressions/seed678_maximal_-x`. With
    `-x` such items are not pex and do make X non-maximal. Keep -x in
    the forced campaigns.
- **Size limit:** with `!deeper` the miner may not detect all pex of X.
  The check then sees them as ordinary items with supp = supp(X) ≥ smin,
  i.e. non-maximal, the same as today.
- **Repository / clomax prune:** A changes only how the check's verdict
  is computed. The repository decision and `cm.update` happen before the
  check, so the set of checked sets is unchanged.
- **Packed items / 16-items machine:** prefix items are bag codes
  (`imap`). The checker's unpacked codes match, as today. Rows come from
  the unpacked horizontal form.
- **Interleaved `check()` (empty prefix) and `|prefix| = 1` calls**, as
  in `bruteCheck`: they touch neither `path` nor `depth`, so no
  invalidation is needed. The condensed path must not read
  `c.nz`/`c.cover` (the prototype does not).
- **A prototype bug found by verify mode**, which is a class to test for:
  the per-item scratch accumulator (supp(P∪{j}) counts) was not fully
  reset between builds. An in-place filter aliased the touched list.
  Stale counts made later builds miss items, giving a wrong "maximal".
  `TestExtCheckOracle` caught it immediately under force. Keep the
  verify-against-`decide` hook in tests, and add the mutant "do not reset
  the count scratch".

Mutants run against
`TestExtCheck|TestOracle$|TestDeviations` with `FPG_COND=force`, per
level:

| mutant | caught by |
| --- | --- |
| filter `>` instead of `≥` | caught |
| word index `0` | caught, by Unlimited only (F4) |
| planes ignored | caught |
| invalidation from k+1 | caught |
| no invalidation | caught |
| drop full-support rows | caught, by the -x regression only |

#### F6 (Low): (B), recalibrating `decide`, has lower risk than stated, and little value after A

The sparse-large 0.81 → 28.4 s figure forced the vertical path for
*every* check. It does not measure a reweighted `decide`. Instrumented
count of scan checks with h ≤ v < 2.7·h (the only checks that a uniform
2.7× reweight flips):

| row | flipped checks |
| --- | --- |
| `sparse-large -tm -n3` | 0 of 14,587 |
| `sparse-large -tm -n2` | 0 of 9,445 |
| `dense -tm -s2 -n3` | all 8,002 scan checks (of 49,335) |
| d5000 n3, d20000 n2 | none (already vertical) |

So B cannot hurt sparse-large unless the sparse-row constants are also
changed. Its remaining gain after A is limited to the ~79k pre-build
checks (≈ 2 s of 5 s on the target). The unit costs are
machine-dependent (random access vs popcount). Keep B last, apply it to
dense rows only, and accept it only with benchstat on arm64 (and ideally
amd64).

#### F7 (Info): simpler alternatives give less

- **D (sibling bound / cached supp(P∪{j}) with horizontal child scans):**
  prunes 0 of 198 items on the target, so no gain. The gain of A comes
  from *condensing*. cover(P) is ~9 % of the transactions, so a child's
  rows shrink from ~239 non-zero global words to 29 words, and random
  item visits become sequential AND+popcount.
- **Ordering by supp(P∪{j}) descending:** free, and helps only
  non-maximal children (early stop). No effect on the target, where
  every check is maximal.
- **A k×k pair matrix per parent from one pass:** Σ|t|²/2 ≈ 3.3 M
  increments per parent, ≈ 66 G on the target. Worse than A.
- **E (pair symmetry):** a child may skip the items j that are earlier
  children shown maximal. That is ~17 % of the popcounts on the target,
  not 2×. Low value.
- **Also small:** skip the `and` for path[L+1] when the condensed path
  answers (1.3 %).

#### F8 (Low): test and benchmark plan additions

- **Production-rule transition:** add a hook that builds at child #m,
  with m random. A parent then has children checked before and after the
  build, under both cache modes.
- **Invalidation sequences:** besides DFS-like and arbitrary jumps,
  cover these cases:
  - same item at level L with a different earlier level (k < L);
  - prefix shrink, then regrow with the same items;
  - interleaved `check()` and `|prefix| = 1` calls;
  - repeated identical prefixes with different pex sets;
  - `setStamp` wrap.
- **No-regression benchmark rows:** add the F1 adversarial row
  (`d20000x200d30 -tm -s-2 -n2`) and `-n4` (F2).
- **Memory:** report the peak condensed-buffer size per level and in
  all, not only RSS.
- **`bf -big`:** run with the CLI rule. It reaches up to 3,200
  transactions and so does exercise multi-word rows in production mode;
  record how many builds it triggers.

### Recommendations (ordered)

1. Implement A with ski rental on **actual** work (F1), with one cache
   per path level invalidated by `k ≤ L` (F2), and with a total memory
   cap.
2. Use a one-pass build and a threshold of about B/2, to aim at about
   18–20 s (F3).
3. Extend the tests as in F4 and F8: forced multi-word brute force, a
   verify hook, the listed mutants with the test that kills each, and
   counter assertions.
4. Run benchstat on d5000, dense, sparse-large and the F1 row before
   accepting.
5. Treat B as optional and dense-rows only (F6). Skip C, E and F unless
   the profile after A shows them.
