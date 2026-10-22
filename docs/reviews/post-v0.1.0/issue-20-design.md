# Design and design challenge for #20

Written before the implementation. The challenge found that probing from the new start transaction alone created a new quadratic case (H1); the merged code keeps main's first probe. See the review records for the two rounds that followed.

## Design proposal

## Design #20 (NEW-3): quadratic closed checks on mixed-length covers

Phase 1; nothing optimised yet. Branch `perf/20-closed-check`, local
commit `923da41` (workloads only, on `ba63d14`). M1 Max, Go 1.27.2,
load up to ~175 (instructions retired, deterministic, are given too).
Raw data, binaries and tools: `~/.cache/fpgrowth-cycle/20/`.

### 1. Reproduction (main `ba63d14`, v0.1.0, C `reference/build/native`)

`reference/bench/gen` has the `lp*` specs (`benchSpec.Short`, `Step`;
SHA-256 pinned; `lp12500`…`lp100000` are the issue's awk file byte for
byte, checked) and `configs.txt` the `lp-*` rows. Medians of 5
interleaved runs after 1 warm-up (3 for the slow rows), `/dev/null`;
main, v0.1.0 and C report the same set counts on every row.

| row (`-s-2`) | main s / G instr | v0.1.0 | C |
| --- | --- | --- | --- |
| lp12500 `-As -tc -n1` | 0.43 / 5.15 | 0.43 / 5.15 | 0.22 / 2.04 |
| lp25000 `-As -tc -n1` | 1.68 / 20.2 | 1.67 / 20.1 | 0.85 / 8.00 |
| lp50000 `-As -tc -n1` | 6.73 / 79.7 | 6.74 / 79.7 | 3.38 / 31.8 |
| lp100000 `-As -tc -n1` (n = 3) | 26.56 / 317.0 | 26.54 / 317.0 | 13.50 / 126.7 |
| lp50000 `-Ac` / `-Ad` | 6.74 / 6.79 | 6.75 / 6.78 | 2.16 / 3.40 |
| lp50000 `-At` | 9.91 / 97.2 | 9.92 | 11.14 / 43.0 |
| lp50000 `-As -l0` | 6.65 | 6.67 | 1.94 |
| lp50000 `-As -tc -n3` | 11.69 / 134.2 | 11.66 | 3.31 / 31.8 |
| lp50000 `-As -tm -n1` | 5.50 / 125.9 | 5.31 / 120.9 | 3.32 / 31.8 |
| lp50000s2 `-tc -n1` / `-n2` | 6.67 / 18.34 (213 G) | 6.65 / 18.39 | 3.34 / 3.37 |
| lp50000w `-w -tc -n1` | 6.71 | 6.66 | 3.34 |
| lpc20000 `-tc -n1` (default `-q2`) | 1.48 / 22.1 | 1.49 | 5.86 / 58.7 (8.0 GB RSS) |
| lpc20000 `-tc -n1 -q0` (n = 3) | 57.66 / 986.7 | 57.89 / 986.7 | killed after 541 s, 169 GB footprint |

- Scaling ×2 items → ×3.9–4.0 time and instructions in Go and C:
  quadratic in both. Go ≈ 2× C on `-As`/`-Ad`/`-w`, 3.1–3.4× on
  `-Ac`/`-l0`, 5.4× on lp50000s2 `-n2`; Go is faster than C on `-At`
  and on lpc20000 `-q2`.
- v0.1.0 = main on every closed row (the closed scan is unchanged
  since PR #6); `-tm` +3.6 % in main (#19's work counter).
- `-n3` adds FP-growth projection (`fpCondSupp` + `projSmp16`), which
  is not the check; C's time is the same with `-n1` and `-n3`.
- `-tm` is quadratic too, in the maximal check (`scanMaximal`): every
  maximal check counts the long line. Outside the closed scope (open
  question 4).

### 2. Profile and counters

CPU profiles (in-process CLI, `prof-*.cpu`, `profiles.txt`): lp25000
`-As -tc -n1`: `initCand` 81 % cum, `keepCand` 17 %, nothing else
above 1 %. lp25000 `-n3`: `initCand` 42 %, `projSmp16` 44 % cum (of
which `fpCondSupp` 28 %), `keepCand` 11 %. lp25000 `-tm`:
`scanMaximal` 90 %. Counters in `scanClosed` (instrumented copy
`src/inst`, `counters.txt`; identical before and after the reboot):

| run | checks | cover tx | initCand items | keepCand steps | Σ shortest len: t0+spread / true |
| --- | ---: | ---: | ---: | ---: | --- |
| lp12500 `-As` | 12,500 | 25,000 | 156.2 M | 78.1 M | 787 k / 12.5 k |
| lp25000 (every variant, `-l0`) | 25,000 | 50,000 | 625.0 M | 312.5 M | 1.57 M / 25 k |
| lp50000s2 `-n1` / `-n2` | 50,000 / 100,000 | 150 k / 250 k | 2.50 G / 5.00 G | 1.25 G / 2.50 G | 3.1 M / 100 k; 6.4 M / 200 k |
| lp50000w `-w` | 50,000 | 100,000 | 2.50 G | 1.25 G | 3.1 M / 50 k |
| lpc20000 (`-q2`) | 20,000 | 3.16 M | 200.6 M | 0.70 G | 20 k / 20 k |
| lpc20000 `-q0` | 20,000 | 3.16 M | 400.0 M | **71.3 G** | 224 M / 20 k |

In every lp run all checks are scans, all probes run, none hits, and
every set is closed. Why quadratic: item i's cover is {long line,
short line(s)}; in bag order (lexicographic, prefix first) the long
line precedes every short one except `{0}`, so t0 is the long line.
`decide` picks the scan (h = n·(avg|t|+1) = 6 ≪ v = k·nz), the 4
probes of the long line all miss the short line, `initCand` copies
N−1 items and `keepCand` walks the candidates up to item i before it
stops (≈ N² + N²/2 steps). h is right on average and wrong here: a
cover samples transactions by length (size bias).
`lpc20000` with descending or no item sorting (`-q0`, `-q-1`, `-q-2`:
151–160 s instrumented, 58 s plain) copies a 20,000-item chain per check and intersects
~160 long chains with ~10,000 candidates before the one-item line
empties them; the spread probes see only chains (the first
transaction of each sampled word). With ascending sorting (default
`-q2`) the short line leads its word and the probes reach it.

Ordinary workloads (`-tc -n3` of d20000x200d30, sparse-large,
sparse, dense, t100000x204c4): |t0| > 4·avg|t| never occurs, no probe
runs; d5000/d2000 use only `vertClosed`. d20000 `-tc -s2 -n3`: 1.17 M scans, mean cover 538
transactions, 63 candidates from t0, 4.4 keepCands and ≈ 313 steps per
check. Profile of that run (11 s): `csCondSupp` 35 %, `addCmplx`
21 %, `checkPath` 27 % cum (`and` 11 %, `scanClosed` 14 % with
`keepCand` 10 %, `vertClosed` 2 %).

### 3. Exactness basis (M5 §3)

X is closed iff no item j ∉ X is in every cover transaction; the cover
is exactly the positive-weight transactions containing X (weight 0 is
dropped when the checker is built; with `checkPath` the prefix cover
is the set's cover). So X is closed iff C(X) = (∩_{t ∈ cover} t) \ X
= ∅. Intersection is commutative, associative and idempotent: any
cover transaction may start the candidates, the others may come in
any order, repeated or skipped once intersected, and the scan may stop
at ∅. A probe finding j ∉ X in all of the cover proves non-closed; a
failed probe decides nothing. No candidate changes the cover,
`decide` or the maximal path.

### 4. Candidates

**A. Start from a short cover transaction (proposed, two stages).**
(a) t* = shortest of t0 and the ≤ 7 spread-probe transactions (≤ 8
`txOff` lookups of transactions the scan visits anyway). (b) Only if
t* still has more than 4 candidates per cover transaction
(|t*| − |X| > 4·n; n is known in `decide`), enumerate the cover bits
once for the shortest transaction, stopping at one with |t| = |X|
(then t = X, because X ⊆ t and codes are distinct: closed, return).
(b) costs n ≤ (copy of t*)/4 — the probe guard's argument. lp2: (a)
suffices except for the 63 items whose short line shares word 0 with
the long one; lpc `-q0`: (b) is needed. Exact by §3. Ordinary rows:
(b) never triggers (d20000: n ≈ 538 vs |t0| ≈ 63; sparse-large
n ≈ 338 vs 14); (a) costs 7 compares per scan and may shorten the copy
(d20000: Σ spread-min / Σ|t0| = 62 M / 77 M). Risk: the start is no
longer the bag-order neighbour of the following transactions, so the
early stop may come later; benchmark, and drop (a) for ordinary covers
(apply it only behind the 4·n guard) if it does not pay.

**B. Adaptive intersection in `keepCand` (proposed).** With few
candidates and a long transaction, search each candidate exponentially
(`seek`-style) instead of merging linearly, e.g. when |t| ≥ 128 and
|t| ≥ 16·|cand|. Needed after A: `lp50000s2` starts from {i, i+1}, and
intersecting {i+1} with the long line walks i items. Cost
O(|cand|·log(|t|/|cand|)). Exact: the same intersection. Ordinary
rows: their transactions are ≤ ~80 items, so the gallop never runs,
but the test sits in the hottest loop of the check (`keepCand` ≈ 10 %
of d20000 `-tc`): benchmark. A candidate bit set / stamp array was
rejected: testing every item of a long transaction against it is the
walk to be avoided.

**C. Probes from t* (proposed with A).** `probeCand` probes up to 4
items of t* \ X, guard |t*| > 4·nz unchanged (probes ≤ copy). Every
item of C(X) is in t*, so t* is at least as good a source as t0. Helps
non-closed sets with a long t0. Exact as before (PR #6 review §1).

**D. More probes** (several cover transactions): subsumed by A + C;
each costs nz and never decides a closed set (all lp sets are closed).
**E. `decide` cost model**: h is length-biased-wrong (lpc), but with
A + B the scan costs ≤ n + |t*| + Σ|cand|·log|t|; a better h needs an
O(n) Σ|t| pass per check. Not proposed.

**F. Reuse #19's sibling rows for closed checks (rejected).** A child
X = P ∪ {h} ∪ pex is closed iff R_h ⊄ R_j for every row j ∉ X with
supp(P ∪ j) ≥ supp(X): exact by the same position bijection as M5
§3.2/§4.1 (the smin row filter suffices since supp(X) ≥ smin). But a
closed set must test every such entry (≈ 198 on d20000 `-tc -n3`, ≥ 1
word each, early exit per entry) and pay a share of the build (Σ|t|
over cover(P) ≈ 1,800 × 60 = 108 k visits per parent, ≈ 1.6 k per child
over ~67 children), against ≈ 376 steps of today's scan, which stops
after ~4 transactions: rows would be slower. The check incl. its
cover ANDs is 27 % of that run (scan 14 %). Revisit only if closed
scans visit long covers (non-closed sets).

### 5. Proposed choice

A (a + b) + B + C in `scanClosed`/`probeCand`/`keepCand`, plus a
`markSet` count of |X|; `decide`, covers, the maximal path and the
sibling rows unchanged; output identical by construction (§3).
Expected: lp2 ≈ N·log N, lpc `-q0` < 1 s, ordinary rows unchanged.
Open questions: (1) stage (a) for every scan or only behind the 4·n
guard — decide by benchmark. (2) Gallop thresholds. (3) In the full
scan, skip t* (one compare per transaction) or nothing. (4) `-tm` on
lp is quadratic too (Go 5.6 s, C 3.4 s), in `scanMaximal`. An exact
maximal analogue exists: if a cover transaction t has
w(t) > supp(X) − smin, every frequent extension j is in t (else
supp(X ∪ j) ≤ supp(X) − w(t) < smin), so only t's items need counting.
Maximal path (#19's code): separate issue or here? (5) `-n3`'s
projection work (≈ 44 %) stays; out of scope.

### 6. Correctness test plan

- New `TestExtCheckMixedCover`: lp-like bags (short lines of 1–3
  items, suffix chains, shared cover words, weights incl. 0, perfect
  extensions), every check against the brute force, three row forms.
- Work bound (no wall clock): lp shape, N = 20,000, next to
  `TestExtCheckChainScaling`: `cap(c.cand)` ≤ 64 plus a test-only
  `keepCand` step count O(short length) per check; fails on main,
  passes after; also the lpc `-q0` shape at N ≈ 4,000.
- Documented counts (`FPG_M5_CASES=10000 FPG_ORACLE_CASES=5000
  FPG_M5_UNLIMITED=300 go test ./internal/fpg -run
  'TestExtCheck|TestOracle$|TestDeviations|TestSeek' -v`): 11 variants
  incl. `-As -l0/3/16`, item selection; `TestExtCheckSibling*`.
- Mutants: PR #6 W1, W2, W4; new: start always from t0 (work test),
  |t| = |X| shortcut as |t| ≤ |X| + 1 (exactness tests), gallop
  off-by-one, t* skipped in the full scan when it was not intersected,
  stage (b) guard inverted, probes of t* including items of X.
- Brute forces `reference/_audit/m5/bfs` (sparse, reaches
  `scanClosed`) and `bf` (`-focus -big`, `-wide`): 0 failures. Difftest
  `cd reference && ./build.sh && go test ./...`.
- md5 of outputs main vs branch: all `lp-*` rows, the T4 chain
  (`-At -tc -n1`), every `m5-*` row.

### 7. Benchmark plan

- CLI, `bin/multitime` (interleaved, warm-up, n ≥ 5, wall, user+sys,
  instructions, RSS, load): all `lp-*` rows, branch/main/v0.1.0/C.
- Regression guard (n = 7, benchstat, > ~5 % reported): fast `m5-*`,
  `m5-tier-*`, `m5-split-*`, `m5-d20000-c-n2/-n3`, `large-c*`,
  sparse/dense/long/weighted `-tc` ± `-n`, `dense-c-*`.
- In process, 6 interleaved rounds: `BenchmarkCloMaxLimited`,
  `BenchmarkMine`, root `SuiteMaxSize`/`SuiteVariants`/`SuiteTargets`.

## Independent design challenge

## Challenge of design #20 (NEW-3): closed checks on mixed-length covers

Independent challenge of `design20.md` (A + B + C), before implementation.
Read-only on the repository; all experiments in
`~/.cache/fpgrowth-cycle/20/challenge/` (clone of `923da41` in `repo/`,
`repo2/`; binaries `fpg-proto`, `fpg-proto2`, `fpg-stat`; data generators
`gen.py`, `gen5.py`; raw tables `ord.md/.tsv`, `ord2.md/.tsv`; mutant log
`mutants-default.txt`; gallop micro-benchmark `gallop/`; brute-force runs
`bfrun/`). M1 Max, Go 1.27.2, desktop load 2–6 (instructions retired are
quoted where wall time is noisy).

### Verdict

**Sound in principle, not ready as written.** A and B are exact and give the
expected collapse on every `lp-*` `-n1` row (6.7 s → 0.05 s; Go becomes
40–60× faster than C). But C ("probes from t\*" instead of t0) **introduces a
new quadratic case** that main handles in linear time (H1), the keepCand
change as sketched **de-inlines the hottest loop** (M1), and the test plan
leaves the gallop path and stage (b) without coverage at the real thresholds
(M3). All three have cheap fixes, verified below. Q1: stage (a) always.
Q2: ratio 16–32, min 128. Q3: skip nothing new (keep skipping t0).

### Prototype

`repo/internal/fpg/extcheck.go` (`scanClosedN`, `keepCand`, `seekItem`,
`markSet`), env switches in `p20_env.go`:

- stage (a): t\* = shortest of t0 and the ≤ 7 spread transactions
  (`P20_A=1` always, `=2` only behind the guard);
- stage (b): if |t\*| − |X| > 4·n, enumerate the cover for the shortest
  transaction, stop at |t| = |X|;
- phase 1: `initCand(t*)`, then `keepCand(t0)` if t\* ≠ t0, then the
  spreads ≠ t\*; full scan as today (first bit, t0, skipped; t\*
  re-intersected unless `P20_SKIP=1`);
- B: per-candidate exponential search when |t| ≥ 128 and |t| ≥ 16·|cand|;
- `P20_PROBE=1`: the H1 fix (probe t0 as today, then t\* if different);
- |X| = distinct items marked by `markSet`.

`repo2` moves the gallop decision to the call sites so that `keepCand`
stays inlined (M1).

### Findings

#### H1 (High) — C (probes from t\*) creates a new quadratic case

The design argues "every item of C(X) is in t\*, so t\* is at least as good a
source as t0". True for the copy, false for the **probes**: probes test only
the first 4 items of t \ X, and which items those are depends on t.

Counterexample (`gen5.py N`, file `data/adv5.tab`): an order line
`m1 m2 m3 r1 r2 r3 r4`, `t0 = {m1,m2,m3} ∪ A` (A = a0…a(N−1)),
`t* = {r1..r4} ∪ A[:N−2]` (one item shorter), two one-item lines for the
last two a's, 200,000 filler lines (so that `decide` scans). `-At -q0 -tc
-s-2 -n1`: every check of {a_i} is non-closed; t0's 4th probe (a0) hits in
main; the proposal probes r1..r4 of t\*, all miss, copies N items and
intersects:

| N | main | A+B+C | A+B+C + fix | C 6.21 |
| --- | --- | --- | --- | --- |
| 20,000 | 0.05 s | 2.30 s (copied 400 M) | 0.05 s (copied 0) | 0.01 s |
| 40,000 | 0.06 s | 9.19 s (copied 1.6 G) | — | 0.02 s |

Output identical (md5) in all three Go runs. Why the first probe cannot
regress but probes 2–4 can: in ascending lexicographic bag order,
min(t \ X) ≥ min(t0 \ X) for every cover transaction t (at the first
difference t0 has the smaller item), so if min(t0 \ X) ∈ C(X) it is also the
first probe of t\*; a hit at rank 2–4 of t0 has no such guarantee.

**Fix (verified):** keep `probeCand(t0)` exactly as main, **before** stage
(a)/(b) (so a non-closed set decided by probes does not pay the stage (b)
enumeration), then probe t\* only if t\* ≠ t0 (guard |t\*| > 4·nz). The
probe budget stays ≤ 2 × the copy bound. With the fix: adv5 0.05 s, lp50000
0.05 s, lp50000s2 0.06 s, lpc20000 `-q0` 0.49 s, md5 unchanged. Add adv5
(N ≈ 4,000) as a work test (copied items per check ≤ some small constant);
it fails on the design as written and passes with the fix.

#### M1 (Medium) — the gallop test must not de-inline keepCand

Main's `keepCand` is inlined at cost 75 of the budget 80 (`go build
-gcflags=-m=2`). Any added test (even a call to a `//go:noinline` helper)
gives cost 150 → not inlined. Measured (`ord.md`, n = 5, same `sh` wrapper
for all prototype columns): with the test inside `keepCand` and stage (a)
off (pure overhead) d20000 `-tc -s2 -n3` +1.97 G instructions (+2.1 %);
time hidden by noise (10.7–15.4 s spread from desktop load).

With the decision at the three call sites in `scanClosed` (`repo2`,
`keepCand` byte-identical to main and inlined, `keepSeek` separate) and
stage (a) always (`ord2.md`, n = 3, load 3.6–4.1):

| row (`-tc`) | main G instr / s | proposal (a always) | (a) guarded |
| --- | --- | --- | --- |
| d20000 `-s2 -n3` | 92.17 / 10.93 | 92.76 / 11.01 (+0.6 %) | 93.29 / 11.02 |
| d20000 `-s2 -n3 -At` | 191.04 / 33.53 | 191.62 / 33.42 | 192.20 / 33.64 |
| tier `-s-180 -n3` | 14.33 / 2.40 | 14.38 / 2.33 | 14.39 / 2.33 |
| split `-s-1350 -n3` | 11.76 / 1.90 | 11.77 / 1.90 | 11.78 / 1.94 |
| large `-s0.05 -n3` | 5.65 / 0.81 | 5.66 / 0.80 | 5.67 / 0.80 |
| dense `-s2 -n3` | 5.46 / 0.57 | 5.47 / 0.56 | 5.47 / 0.56 |
| long `-s2.5 -n3` | 4.76 / 0.85 | 4.76 / 0.86 | 4.78 / 0.88 |

The prototype still carries counters and env loads; the residual +0.6 % on
d20000 is an upper bound. Requirement for the implementation: assert in
the review that `keepCand` stays inlined (`-gcflags=-m`), and run the
benchstat guard on instructions as well as time.

#### M2 (Medium) — `-n2`/`-n3` and `-At` lp rows stay quadratic; say so

With the fix, `-n1` rows collapse, but (prototype, single runs):
lp50000s2 `-n2` 5.2 s (main 18.3, C 3.37), lp50000 `-n3` 5.2 s (main 11.9,
C 3.31; lp12500/25000/50000 = 0.35/1.34/5.29 s, ×3.9 per doubling),
lp50000 `-At` 3.34 s (main 10.0, C 11.1). Profiles: `-n2`/`-n3` 96 %
`projSmp16`/`fpCondSupp` (projection, out of scope per orchestrator; the
design names only `-n3`, but `-n2` has the same cause), `-At` 92 %
`addTopDown` (tree build; C is quadratic there too). The acceptance claim
"scales sub-quadratically" must be stated for `-n1` and the check only;
§5's "Expected: lp2 ≈ N·log N" must exclude these rows explicitly.

#### M3 (Medium) — the test plan does not reach the new paths

Counters over the documented-count run (`FPG_M5_CASES=10000
FPG_ORACLE_CASES=5000 FPG_M5_UNLIMITED=300`, `-run
'TestExtCheck|TestOracle$|TestDeviations|TestSeek'`, prototype A always):
3,349,345 scans, stage (a) moved the start 1,361,445 times, stage (b)
450,059 times, |t| = |X| stop 425 times, **gallop 0 times** (no test
transaction reaches 128 items). bfs transactions are ≤ ~10 items: gallop
unreachable there too (counts in `bfrun/`, see below).

Mutants (existing suite, default counts, `mutants-default.txt`):

| mutant | caught by |
| --- | --- |
| 1 stop at \|t\| ≤ \|X\|+1 | TestExtCheckUnlimited |
| 2 t\* ≠ t0 but t0 not intersected (t0 still skipped in the full scan) | TestExtCheckUnlimited |
| 3 gallop off-by-one (bisect treats `== w` as `< w`) | **nothing** at real thresholds; caught (Unlimited, Brute) only with thresholds forced to 2/1 at documented counts |
| 4 stage (b) guard inverted | nothing (exact; performance only) |
| 5 probes include items of X | SingleCover, Unlimited, Brute |
| 7 stage (b) picks a non-cover transaction (t+1) | SingleCover, ChainScaling, Unlimited |
| 8 stage (b) misses the last cover word | nothing (exact; performance only) |

Performance mutants 4 and 8 are caught by the planned work test only if it
has the right shape: lp25000 copies 1.55 M items under both (62 word-0
items copy the long line), so `cap(c.cand) ≤ 64` on the lp shape catches
them; lpc `-q0` goes 0.48 s → 58.7 s (4) / 51.8 s (8). Required additions:

- a test hook for the gallop thresholds (and one forcing stage (b)), and a
  run of the extcheck suite with them forced; or make `seek` generic over
  `~int32` and reuse it, so `TestSeek` covers the gallop's search;
- `TestExtCheckMixedCover` must contain transactions ≥ 128 items with a
  surviving candidate found by the gallop, one not found, and one beyond
  the end of the long transaction (the `i == len(items)` break);
- the adv5 work test (H1); the lpc work test must assert that stage (b)
  fires (the short line in the last cover word), not only the time;
- a zero-weight transaction equal to X (shortest, first in bag order) with
  X non-closed: proves the shortcut sees only the checker's transactions.

#### L1 (Low) — make the |t| = |X| stop a `break`, not `return true`

Returning "closed" at |t| = |X| needs two extra facts: |X| counts distinct
items, and X ⊆ t for every cover transaction (true: the cover is the
prefix's cover and pex are in every positive-weight transaction of it,
§3; negative weights are rejected by `tract`). Both hold, but neither is
needed: stopping the enumeration with t\* = t and letting `initCand` find
no candidate costs |X| lookups and leaves exactness on §3 alone. Then |X|
is used only in the guard (performance), where `len(set)` is fine. If the
shortcut is kept, count |X| in `markSet` as newly stamped items (as the
prototype does), not `len(set)`.

#### L2 (Low) — remaining quadratic shape for non-closed sets (main and proposal alike)

`gen.py` `adv1f` (3 equal chains + T′ = 4 rare items ∪ chain minus 5, fillers),
`-At -q-2 -tc -s-2 -n1`: main 1.64 s, proposal 1.64–1.66 s; N = 40,000:
6.41 / 6.50 s; C 0.02 s. The shortest (and first) cover transaction's 4
probes are the rare items; every check copies N items. Not a regression,
and C's speed comes from its incomplete repository check (M5), not a
better complete check. Out of scope; record it in m5-investigation §4 as
the known remaining shape (a vertical probe of the most frequent items, or
a `decide` that charges |t\*|, are possible follow-ups).

#### I1 — exactness of A, B and stage (b)

Exact by §3 for every variant: any cover transaction may seed the
candidates; the others can be intersected in any order, repeated, or
skipped once intersected; zero-weight transactions are not in the
checker (built out), so neither stage can pick one; packed items are
unpacked into ascending codes before the bit sets are built; perfect
extensions are in every cover transaction; the 16-items machine and all
11 variants reach the same `checkPath`. Prototype evidence: documented-count
suite passes with A always and guarded (`repo2`), output md5 equal to main
on every `lp-*` `-n1/-n2/-n3/-At/-Ac/-Ad/-l0/-w` row, lpc20000 `-q2/-q0`,
adv1f/adv1fb (all orders), adv2f, adv3, adv5. Brute forces: see the last
section.

#### I2 — guard defeat goes vertical

Covers where stage (b) does not fire (n ≥ |t0|/4) with dense suffix chains
(`adv2f`: step-2 chains, M = 4,000) make `decide` choose the vertical test
(17–29 scans in total); main 0.62/0.80 s, proposal the same, C 0.52 s. The
4·n guard bounds stage (b) by the copy it replaces, so it cannot make a
check worse than main by more than a constant.

### Answers to the open questions

- **Q1: run stage (a) on every scan.** Phase 1 intersects the same set
  {t0, spreads} either way, so only the copy changes: copied items on
  d20000 `-n3` 73.3 M → 58.7 M (start moved in 1.12 M of 1.17 M scans),
  instructions lower than guarded on every row (table in M1; also `ord.md`:
  −0.65 G vs the same binary without (a)). Guarded is never better.
- **Q2: gallop when |t| ≥ 128 and |t| ≥ 16·|cand| (or 32).** Micro-benchmark
  (`gallop/`, candidates spread uniformly, half hits): the crossover is at a
  ratio of 16 for every |t| from 64 to 4,096 (equal at 16, gallop 1.5× faster
  at 32, 2× slower at 4). On lp50000s2 every setting from (16, 4) to
  (1024, 64) gives 1.06–1.09 G instructions vs 9.77 G without gallop, so
  the choice only matters for ordinary data, where 128 keeps it off
  (transactions ≤ 98 items).
- **Q3: skip nothing new.** Keep the full scan's "skip the first bit" (t0)
  and intersect t0 explicitly in phase 1 when t\* ≠ t0; re-intersecting t\*
  is idempotent and costs ≤ |t\*|. Measured: d20000 `-n3` 93.495 G vs
  93.507 G instructions (compare per transaction), tier 14.440 vs 14.429 G,
  identical keepCand counts — the scan empties before reaching t\* in
  practice. A per-transaction compare adds a branch for nothing; the
  mutant to test is "t0 skipped but not intersected" (mutant 2, caught).
- **Q4 (`-tm`)**: separate issue, as decided.

### Brute forces

Prototype with the H1 fix (`fpg-stat`, A always, probe t0 then t\*),
harnesses copied from `reference/_audit/m5/` (logs in `challenge/bfrun/`):

| run | result |
| --- | --- |
| `bfs -n 60` (seed 1), real thresholds | 300 runs, 0 failures |
| `bfs -n 60 -seed 2`, gallop forced (2/1), stage (b) guard inverted | 300 runs, 0 failures |
| `bf -n 3000 -focus -big`, real thresholds | 33,000 runs, 0 failures |
| `bf -n 3000 -wide`, forced as above | 33,000 runs, 0 failures |

Path counters (sum over the CLI runs): real thresholds 166,523 scans, start
moved 106,496, stage (b) 1,656, |t| = |X| stop **0**, gallop **0**; forced
144,766 scans, stage (b) 142,404, stop 25,835, gallop 478,682. So the
brute forces exercise the new paths only with forced thresholds (M3): the
implementation needs the test hook and a forced run of `bfs`/`bf` too.
