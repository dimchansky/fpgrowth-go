# Design and design challenge for #21

Written before the implementation. The challenge (F1) extended the fix to two more quadratic shapes (guards in Report and expand) and moved the miner guard out of AddPex to keep the reporter's C-parity goldens (F2).

## Design proposal

## Issue #21 design: skip perfect extensions that cannot affect any output

Base: origin/main e4cf7d0; local commit 5f7d90f adds the T4 workload only.

### 1. Reproduction (instructions retired, one run each, daytime short check)

Data: `t4-N` = benchSpec.Chain (21/gen, SHA pinned), `-s-17 -n1`, default
target. C ran with `ulimit -s 65520`. Raw: 21/baseline/t4-small.tsv (+ .load,
load average 11.9 during the run; instructions are load independent).

| n      | main          | v0.1.0        | C             | sets   | md5 equal |
|--------|---------------|---------------|---------------|--------|-----------|
| 6,250  | 2.114 G       | 2.100 G       | 1.290 G       | 6,251  | yes (all 3) |
| 12,500 | 7.942 G       | 7.936 G       | 5.007 G       | 12,501 | yes (all 3) |
| ratio  | 3.76x         | 3.78x         | 3.88x         |        |           |

Doubling n multiplies the work by about 4: quadratic in all three programs.
Deferred to night: t4-25000 and the 12.5k variant rows (-As, -Ad, -As -l0,
-x, -tg), 2 runs each, main / v0.1.0 / C (saved once to 21/baseline/).

### 2. Profile (12.5k, CLI `run` in-process, 4 runs; 21/prof/s.*, g.*)

Default target: 92 % cum in `recCmplx`. `Remove` 29 % (the pex unmark loop,
report.go:419-420), `AddPex` 24 % (called from the chain walk,
complex.go:381-382; the walk itself another 4 %), `Report` 28 % cum, of it
the supps/wgts fill (report.go:610-612) 9 % and `expand` 19 % (the loop that
rejects every pex against zmax, report.go:686-689). Each chain item at
depth d adds d pexs, so the sum is n^2/2 = 78 M pex operations at 12.5k
(about 100 instructions per pex across add, fill, expand and remove).

`-tg` (also quadratic, 0.93 s at 25k in the audit): 63 % in `AddPex`, which
returns false at once for generators, plus 15 % for the walk. The walk is
pure waste there.

### 3. Where perfect extensions go (grep: AddPex*, PexCount, Uses, pexs, inUse, wgts)

Producers: chain walks in recCmplx, recSimple, recSmp16, recSingle (the only
O(depth) sites); projection loops projCmplx, projReord, projSimple,
projectSingle, recTopDown (O(1) extra per item, the loop exists anyway);
topLevel (mine.go); the 16-items machine (filter, addPexBits, recPos, pair,
single, mine; at most 16 per call); AddPexPacked (at most 31 bits).

Consumers in the reporter: `Report` (n = len(pexs): size gate `cnt+n <
zmin`, closed/maximal set building, NoExpand output, count-only `countOnly`,
fill of supps/wgts[cnt+1..cnt+n], `expand`), `Remove` (pexCnt, inUse),
`AddPex` itself (inUse dedup), `Uses` (no caller in the miners),
`PexCount` (miners: `xable2`, `deeper`, `machine.deeper` -- all only when
`m.limited`, i.e. a size-limited closed/maximal search). The closed/maximal
repository, `SetCloMaxCheck`/extChecker (`checkPath(rep.Items(), set)`) and
`Tail` exist only with `cm != nil`.

### 4. The exact condition

Let L be the reporter level (`cnt`) at which a pex is added. A pex added at L
belongs to every set reported at levels >= L until `Remove` pops level L.

Claim. If target is not closed/maximal (no NoExpand mode, no repository)
and L >= zmax, no pex added at L can change any output. If target is
generators, no pex can ever change anything (AddPex is already a no-op).

Proof sketch. Every Report while the pex is present has cnt >= L >= zmax.
(a) cnt > zmax: Report returns before using n (`r.cnt > r.zmax` gate) --
the closed/maximal branch before it is absent (cm == nil). (b) cnt == zmax:
the gate `cnt+n < zmin` passes with and without the pex whenever cnt >=
zmin; if zmin > zmax nothing is output in either case (expand outputs only
sizes in [zmin, zmax]). Count-only: z = cnt is counted iff cnt >= zmin,
then the loop breaks at k = 1 because cnt+1 > zmax -- identical for every
n. Expand path: the loop over pexs never recurses (`r.cnt <= r.zmax` fails
after the increment), so the only output is the base set items[:cnt] with
supps[cnt], wgts[cnt]; the fill writes indices > cnt only. Order and count
of outputs are therefore unchanged. Pexs of lower levels stay present and
are handled as before.
Scratch arrays: skipped work writes only items/supps/wgts at indices >
zmax and pexs/pexCnt/inUse entries of the skipped items. Indices > zmax
are never read by an output (sizes <= zmax) and supps[k] is set by Add
before every read; wgts[k] is never written by Add, so a later fill at a
level cnt' < zmax copies wgts[cnt'] with cnt' <= zmax, an index the skipped
fills never wrote. (ItemSet.Weight, the %w %W %r %R %z codes, is thus
unchanged.) inUse of a skipped item stays false; it is read only by AddPex
(dedup, deeper levels are also >= zmax and skip) and Uses (no miner
caller). Add does not consult inUse. pexCnt[L] stays 0, which Add set.

Per consumer:
- Reported sets, supports, order, Summary.Reported/BySize (`countSets` from
  `output`/`countOnly`) and saturation: equal by (a)/(b).
- `-Z` count-only: (b), countOnly. `-F` border: applied in `output` to the
  base set only, size cnt, support supps[cnt]: unchanged.
- `-P` spectrum: `countSets` -> `psp.Add(z, s, c)`, same calls.
- `-eb` LogRatio: reads items[:cnt], supps[cnt], supps[0]: unchanged. Other
  `-e` measures, `-a`, `-i`, `-p`, rules (`-tr`) and item-set-tree output
  go through `mineTree` (fpg.go:200), which never calls AddPex; unaffected.
- `-x` (ModePerfect off): chain walks still add pexs; same condition.
- `-q`, `-j`, `-R` selection/appearance, `-u`: change the tree/items or the
  maximal path only, not what a pex at L >= zmax can do.
- Closed/maximal (`-tc -tm`, NoExpand, repository, cmCheck/extcheck, Tail,
  PexCount in xable2/deeper): excluded by the guard. Counterexample:
  two lines "a b", `-tc -s-2 -n1`: main prints nothing (checked). b is a
  pex of {a} at level 1 = zmax; NoExpand drops set+pexs as cnt+n > zmax
  (report.go:597). Skipping b would print the non-closed "a (2)". The
  repository and `deeper`/`xable2` also use the pexs.
- Generators: AddPex returns false for every item already; skipping the
  walk changes no state at all (only the inUse loads disappear).
- Cancellation ticks: AddPex does not tick; Report ticks equally except
  when zmin > zmax and cnt+n >= zmin (one expand tick less). That only
  shifts where a cancellation is noticed, which is documented as
  imprecise (up to AbortEvery results); final results are unaffected.
- Variants: the projection sites (projCmplx/projReord/projSimple/
  projectSingle/recTopDown) only run when Xable(2) held at the parent, so
  cnt <= zmax-1 there and the guard never fires: unchanged, nothing to gain.
  The 16-items machine (pack/-l16, also inside -Ac) adds pexs at any level
  (recPos has no Xable gate); guarding there is exact by the same claim and
  costs nothing. Single tree (-Ad) and -As with and without -l0: the chain
  walks of recSingle/recSmp16/recSimple are the quadratic sites like
  recCmplx. -At: no chain walk; already linear (0.07 s at 25k).
- zmax = 0 (`-n0`): the guard fires already at the top level (topLevel's
  pexs); the empty set's expansion outputs only size 0 anyway. Exact.

The guard cannot be widened to cnt >= zmax-1 or similar: a pex at L <
zmax yields reported supersets of size L+1 <= zmax.

### 5. Proposed change (minimal)

In `internal/report`:
- field `pexMax int`, computed in `updateXmax` (called by SetTarget,
  SetSize, Setup): -1 for TargetGenerators; MaxInt if `mode&ModeNoExpand
  != 0 || cm != nil`; else zmax (>= MaxSize means effectively unlimited).
- `func (r *Reporter) PexUseful() bool { return r.cnt < r.pexMax }`
  (inlinable; doc comment carries the proof sketch of section 4).
- `AddPex`: replace `r.target&TargetGenerators != 0` by `r.cnt >=
  r.pexMax`, tested first (same number of compares; generators keep the
  no-op, and every site incl. the 16-items machine becomes exact-no-op).

In `internal/fpg`: wrap the four chain walks (recCmplx, recSimple,
recSmp16, recSingle) in `if m.rep.PexUseful() { ... }`. Other sites need
no change (AddPex handles them; they are O(1) per item).

Cost: one compare per chain item, AddPex unchanged in cost. Expected gain:
on T4 every item costs O(1) in the miner and the reporter (n = 0 at
Report, Remove pops nothing), so the run becomes linear: instructions
should drop to roughly the input parsing and output writing cost (to be
measured; prediction about 1-3 % of main's 7.9 G at 12.5k, ratio 12.5k to
6.25k about 2). -tg on T4 becomes linear too. Go then beats C on these rows
(an improvement, not a deviation: outputs stay byte-identical to C).

### 6. Correctness test plan

1. Reporter unit tests: pexMax/PexUseful truth table (targets x
   ModeNoExpand/NoFilter x zmax incl. 0 and MaxSize, after SetTarget and
   SetSize in both orders); AddPex at cnt >= zmax leaves PexCount, Uses,
   Report output and Stats unchanged versus a reference reporter that
   keeps the pexs (scripted sequences, also count-only, border, spectrum,
   eval, zmin > zmax).
2. Old/new comparison in internal/fpg: a test hook (testHooks, or a build
   tag like fpgforce) that disables the guard; randomized oracle databases
   (internal/oracle gen), every target (s c m g r), AlgoSimple/Complex/
   Single/TopDown, Pack 0/16, -j, -x, Sort -2..2, ZMin 0..3, ZMax 0..4 and
   unlimited (incl. zmin > zmax), border, spectrum, LogRatio with
   thresholds, count-only (nil sink), info formats with %w %r %z; equal
   sinks/Stats/Spectrum, case counts reported. Plus the oracle brute force,
   FuzzMine / FuzzMineItemsets for the CI smoke duration.
3. CLI: goldens unchanged; difftest `(cd reference && ./build.sh && go
   test ./...)`; md5 sweep main vs branch over configs.txt rows with -n1
   ..-n3 added (21/md5, reuse 20/md5cmp.py), C on the t4 rows.
4. Mutants that must fail: guard `<=` instead of `<` (cnt == zmax-1
   skipped); drop the NoExpand/cm condition; guard in the chain walk but
   not tied to cnt (always skip); pexMax not recomputed in SetSize.
5. Scaling regression test (no wall time): T4 shape at n = 4,000 with
   -n1, a sink that records `rep.PexCount()` at each set: max must be 0
   (main: n-1), and total pex work sum(PexCount) bounded by a constant;
   for the walk itself (generators, where PexCount is always 0) see
   open question 2.

### 7. Benchmark plan (budget: one round, night window)

- Baseline once (night): t4-6250/12500/25000 and the 12.5k variant rows,
  main / v0.1.0 / C, 2 runs, instructions + user time -> 21/baseline/.
- Branch: the same t4 rows, 2 runs (shows linear scaling at 3 sizes).
- Regression: BenchmarkSuite*, internal/fpg BenchmarkMine, internal/report
  BenchmarkReporter, main vs branch interleaved, count 6, benchstat, plus
  instructions for 2 CLI runs of the ordinary -s/-g/-n rows of configs.txt
  (dense-s, long, sparse-s, dense-g, and dense with -n2/-n3).
- The full interleaved main/PR/v0.1.0 round: performance critic.

### 8. Open questions

1. Put the guard into AddPex (proposed, covers all sites at zero cost) or
   only into the miners via PexUseful? The former changes AddPex's
   contract ("returns false at cnt >= zmax"); doc + test needed.
2. Testing that the walk itself is skipped (generators: PexCount is
   always 0) needs a testHooks step counter in the four walks; add it,
   or rely on the PexCount bound plus the benchmark?
3. Report at cnt == zmax still loops over pexs inherited from lower
   levels (fill + expand rejects). A second guard in Report (skip fill and
   expand loop when cnt >= zmax and not NoExpand) would make such reports
   O(1) too; not needed for T4 (no inherited pexs) -- out of scope unless
   the maintainer wants it.
4. Pre-existing tooling defect found: `go run ./bench/gen -list` panics on
   adv5-20000 (benchTrap emits item indices up to Items+13 >= Items;
   main.go:112 `used[i]`), so the list stops before the t4 rows. Not fixed
   here (issue #20 tooling); report to the orchestrator.

## Independent design challenge

## Design challenge: issue #21 (NEW-1), design21.md

- Issue: dimchansky/fpgrowth-go#21, "Avoid unnecessary perfect-extension work under restrictive size limits"
- Design reviewed: ~/.cache/fpgrowth-cycle/21/design21.md (base origin/main e4cf7d0)
- Reviewer: independent FP-growth algorithms critic (design stage, before implementation)
- Scope: exactness of the guard, closed/maximal exclusion, open questions 1-4, gain and regression risk
- Time: 2026-10-22 10:10-10:25 local, daytime. Only reading, `go test -short ./...` and single runs under 1 s each. No benchmarks, no campaigns over 10 s.
- Workspace: ~/.cache/fpgrowth-cycle/21/challenge/ (clone `fpgrowth-go`, branch `exp`, not pushed)
  - prototype diff: `challenge/proto-exp.diff`; experiment tests: `challenge/exp21_test.go`, `challenge/exp21prof_test.go`
  - probe inputs: `challenge/ce{1,2,3}.tab`, `challenge/one-{6250,12500}.tab`, `challenge/t4b-{3000,6000}.tab`

### Verdict

**The design is sound. Accept it with changes.** The guard is exact for the
reported sets and every consumer listed in the issue: I checked this by
reading the code and with a randomized old/new comparison (55,000
comparisons, 0 differences). Before implementation, two parts of the
design should change:

- Put the guard only in the miners. Leave `AddPex` as an exact port: the
  AddPex variant breaks the C-harness goldens of the reporter.
- Fix the closed/maximal counterexample in the written argument. The one
  given does not exercise the guard.

The design also leaves two quadratic shapes that the issue's own mechanism
covers. They need a decision (F1). Two small exact reporter changes fix
both of them.

### Evidence examined

- Issue #21 body. It has no comments.
- design21.md, ACTIVITY.md, baseline/t4-small.tsv, prof/ listing.
- internal/report/report.go (whole file): Add, AddPex, AddPexPacked, Remove, Report, countOnly, expand, output, countSets, updateXmax, SetTarget, New.
- internal/fpg: complex.go (recCmplx), simple.go (recSimple, recSmp16), single.go (recSingle, projectSingle), topdown.go (recTopDown), fim16.go (filter, addPexBits, recPos, pair), mine.go (topLevel, xable2, deeper, mineItemSets, extension-check wiring), fpg.go (Reporter, Mine, the mineTree split).
- Every caller of AddPex, PexCount, Uses, Xable, SetTarget and ReportEval (grep over the module), the CLI and root use of the reporter (only SetSink, SetBorder, AddSpectrum, Spectrum, Stats, Count), and the istree use (ReportEval only, no AddPex).
- internal/report tests: report_test.go, report_script_test.go and the C-harness fixtures in testdata/{random,scenarios}.
- reference/bench/gen/main.go printList and core.go benchTrap.

### Commands run (all short)

| Command | Result |
| --- | --- |
| Prototype A: AddPex guard + walk guards, then `go test ./internal/report` | **FAIL**: TestReporter_AddPexPacked (`pexs [], want [1 0 3 30]`, zero-value `pexMax`); TestGolden random/r000, random/r048, scenarios/smax (`addpex N -> -1`, C harness wants `0`) |
| Same, with `New` calling updateXmax and the script runner masking the addpex return code | PASS. All other harness output, including sets, `pexcnt` and `uses`, equals C |
| Prototype B: miners-only guard (`PexUseful` before the 4 chain walks, AddPex unchanged), `go test -short ./...` | all 12 packages ok (13 s) |
| `TestExp21`: main behaviour (guard off) vs guard on vs guard on with forced walks. 1000 random DBs (genCase, weights) x 11 variants x 5 targets (s c m g r); random ZMin 0..3, ZMax {0,1,2,3,4,unlimited} (zmin > zmax included); LogRatio with thresholds; border; spectrum; nil sink (count-only). Compared: sets, supports, Weight and Eval bits, order, Count, Stats, Saturated, spectrum cells | **55,000 comparisons, 0 differences**. The guards fired: 2,110 walk skips and 4,750 skipped AddPex calls (300-case run) |
| Mutant: pexMax = zmax-1 | detected at once (seed 0, `-n1`) |
| Mutant: no closed/maximal exclusion | **not detected** (16,500 comparisons; CLI probes ce1-ce3 also equal, see F3) |
| t4-6250, one run each (instructions retired) | main 2.123 G -> B 0.252 G (`-n1`); `-tg` 0.661 G -> 0.252 G; `-Ad` 2.219 G -> 0.257 G; `-As -l0` 2.182 G -> 0.246 G. md5 equal |
| t4-12500, B | 0.503 G (main 7.942 G, C 5.007 G); 12.5k/6.25k = 1.96 (linear). md5 80d311… equal to main and C |
| one-N (one transaction with N items, `-s-1 -n1`) | main 0.582 G / 2.106 G; C 0.493 G / 1.909 G; B 0.583 G / 2.111 G at N = 6,250 / 12,500. **Quadratic in all three, unchanged by the design** |
| t4b-N (2 transactions, N items in both, N items split, `-s-1 -n1`) | main 0.785 G / 2.902 G; B 0.565 G / 2.056 G at N = 3,000 / 6,000. **Still quadratic** |
| B + expand skip + Report guard (F1), the same probes | one-N 0.088 G / 0.161 G; t4b 0.081 G / 0.160 G (linear); md5 equal to main and C. TestExp21 with 600 cases and `go test ./internal/report` pass |
| B + expand skip only | t4b 0.225 G / 0.704 G: still quadratic. Both changes are needed |

Load average was 4.6-6.3 during the runs. All conclusions rest on instructions retired and md5. No wall-time claim is made.

### Findings

| ID | Severity | Status | Summary |
| --- | --- | --- | --- |
| F1 | HIGH | confirmed | Two natural shapes stay quadratic under the design: the empty-set hypercube at `zmax` and inherited extensions at `cnt == zmax`. Scope 3 asks for every valid site. |
| F2 | HIGH | confirmed | A guard inside `AddPex` (option 1) breaks the reporter's C-harness goldens and a unit test. Use the miners-only guard. |
| F3 | MEDIUM | confirmed | The closed/maximal counterexample in the design is wrong: it never exercises the guard. Its necessity is not shown. Keep the exclusion, with a correct justification. |
| F4 | MEDIUM | confirmed | `pexMax` is only computed in updateXmax, and `New` does not call it. Its zero value 0 disables extensions on a reporter that skips SetSize/SetTarget/Setup. |
| F5 | MEDIUM | design gap | The test plan cannot detect a dropped walk guard while an AddPex guard is in place. The miners-only guard fixes this. Add per-variant mutants. |
| F6 | LOW | confirmed | The bench/gen `-list` panic is in printList only (`used` has length `s.Items`, benchTrap emits up to `Items+13`). The fix is list-only. |
| F7 | LOW | confirmed | The predicted gain ("1-3 % of main") is optimistic. Measured: 6.3 % at 12.5k (0.503 G vs 7.94 G, 15.8x), 12 % at 6.25k. The run is linear. |
| F8 | LOW | note | The old/new comparison in internal/fpg needs a cross-package switch for any reporter-level guard. |

#### F1 (HIGH): the design leaves two quadratic shapes in the same mechanism

The issue's background names the cost: `expand` "rejecting every superset
against zmax". The design removes it only where the extensions were added
at `cnt >= zmax`. Two other paths do the same rejected work:

1. **`expand` at `cnt == zmax` below a report at `cnt < zmax`.** Take one
   transaction with N items, `-s-1 -n1`. Every item is a top-level perfect
   extension. The final `Report()` of the empty set calls `expand(0)` at
   cnt 0. That recurses into `expand(j+1)` at cnt 1 = zmax for every j,
   and each of those loops over the remaining N-j-1 extensions only to
   reject them: N^2/2 steps. Measured: main 0.58 G -> 2.11 G for N 6,250
   -> 12,500 (3.6x). C is the same; design B is unchanged. The profile on
   t4b shows `Report -> expand` at 79 % cum.
2. **Extensions inherited from levels below `zmax`, reported at
   `cnt == zmax`** (design open question 3). On t4b, design B still costs
   2.06 G at N = 6,000 (3.6x per doubling).

Fix (prototyped, exact):
- in `expand`, run the loop only if `r.cnt < r.zmax`;
- in `Report`, for non-NoExpand targets with `r.cnt >= r.zmax`, treat
  `n` as 0 before the size gate.

Why both are exact:
- In `expand` at `cnt >= zmax`, an iteration never recurses (`cnt+1 >
  zmax`). It only writes `items[cnt]` at an index >= zmax, which an
  output of size <= zmax never reads, and which `Add` rewrites before any
  later use. `expand` ticks once per call, so the abort steps do not
  change.
- In `Report` at `cnt >= zmax`, `n` matters only through the gate
  `cnt+n < zmin`, the fill of indices > zmax, the expand loop (now
  skipped) and countOnly, which breaks at k = 1. When `zmin > zmax` both
  versions output nothing. The only side effect: one expand tick less in
  that case. This is the same tick shift the design already accepts.

Result: both shapes become linear (0.16 G at the larger size), output
md5-identical to main and C, and TestExp21 and the reporter goldens pass.
These are two compare-and-branch lines in the reporter.

Requested: include both changes in #21, together with the probe shapes
(one-N, t4b) in the scaling test and the benchmarks, or put them to the
maintainer with these numbers. Without them, "-n1 on data with many items
in every transaction" stays quadratic, and the M10 row would overstate
the fix.

#### F2 (HIGH): open question 1, guard in AddPex vs only in the miners

Option 1 (`cnt >= pexMax` inside `AddPex`) changes the observable result
of `isr_addpex`. internal/report/testdata/random/r000 and r048 and
scenarios/smax call `addpex` at `cnt >= zmax`, and the C harness expects
return code 0 there. Go would answer -1. TestReporter_AddPexPacked also
fails. Taking option 1 would need a new documented reporter-level
deviation and golden exceptions.

AddPex's own cost is not the problem. The quadratic part is the walk,
which needs `PexUseful()` anyway. AddPex calls that are not in a walk are
bounded: the 16-items machine adds at most 16 per level, topLevel adds
O(items) once, and the projection loops run only under Xable(2).

**Recommendation:** keep `AddPex` an exact port and guard the four chain
walks with `PexUseful()`. With F1, the reporter side also needs the
`expand` and `Report` guards. Prototype B (miners-only) passes
`go test -short ./...` unchanged and gives the same T4 gain as option 1
(0.252 G vs 0.268 G, single runs).

#### F3 (MEDIUM): the closed/maximal exclusion, necessary and sufficient?

The design's counterexample: two lines "a b", `-tc -s-2 -n1`, described
as "b is a pex of {a} at level 1". This is wrong.
- Both a and b have support 2 = total weight, so topLevel adds both as
  extensions at **level 0**.
- 0 < zmax = 1, so the guard would not fire at all.
- main prints nothing because NoExpand drops {a,b} (2 > zmax), not
  because of a level-1 extension.

With the exclusion removed (mutant `fpg-mutA`), the output equals main on:
- ce1 ("a b" x2);
- ce2 ("a b" x2 + "c"), where b really is an extension of {a} at level 1
  = zmax;
- ce3;
- for each of them, `-tc`/`-tm` with `-s-1` and `-s-2`;
- 16,500 random comparisons.

Why the mutant survives:
- In an unlimited search (zmax >= longest transaction), a closed/maximal
  report at `cnt >= zmax` cannot have an extension.
- In a limited search, the complete extension check (cmCheck, #19/#20)
  rejects the set that has lost its extensions.

So the exclusion is **sufficient**, but it has **not been shown to be
necessary**. Still keep it. Without it:
- the repository stores different sets (fewer prunes, more work);
- `deeper`/`xable2` read `PexCount`;
- exactness would rest on the extension checker being complete and the
  repository being sound, which nobody has proved for this use.

On T4 itself, `-tc -n1` is already linear on main (0.251 G at 6.25k, the
repository prunes the walks), so the exclusion costs nothing there.

Requested: replace the counterexample with the correct reasoning. NoExpand
reports the set with all its extensions, and the repository update and
the limited-search size gates consume the extensions. Say that output
equality without the exclusion is plausible because of cmCheck, but is
not claimed. Optional: `cm != nil` implies closed/maximal, which forces
NoExpand, so `mode&ModeNoExpand != 0` alone is the condition.

#### F4 (MEDIUM): zero value of `pexMax`

The design computes `pexMax` in updateXmax, which is called from
SetTarget, SetSize and Setup, but not from `New`. A reporter used straight
after `New` gets `pexMax == 0`. In my option-1 prototype, every extension
was then dropped (TestReporter_AddPexPacked). With the miners-only guard
the same thing would happen to the walks.

The miners always configure the reporter, so this is latent. Still: call
updateXmax in `New`, or encode the field so that its zero value means "no
limit". Add a unit test for `New` -> PexUseful.

#### F5 (MEDIUM): open question 2, step counter vs PexCount bound

Under option 1, a mutant that drops the walk guard but keeps the AddPex
guard is invisible to any PexCount bound, for every target: AddPex
discards everything anyway. Only a counter or a timing would catch it.

With the miners-only guard (F2), the default target's T4 test sees
`PexCount` = d at each report if a walk guard is missing. Run it per walk
so that each site is covered:
- `-Ac` (recCmplx);
- `-As -l0` (recSimple);
- `-As` with packing (recSmp16, which needs Pack > 0);
- `-Ad` (recSingle).

Generators need no separate walk test: the walk guard is the same
`PexUseful()` call, and the pexMax truth table covers `-1`.

Recommendation: **no step counter in the hot loops.** Use:
- the per-variant PexCount bound (max 0 and sum 0 on T4 `-n1`);
- the pexMax truth table;
- the mutant list. Add to it: walk guard removed in one variant only;
  `New` without pexMax init; and, with F1, expand skip at `cnt <= zmax`
  instead of `<`, and the Report guard applied to NoExpand.

For the F1 `expand` skip, a counter-free white-box check exists. Put a
sentinel in `items[zmax]`, then Report at `cnt = zmax-1` with extensions.
On main the rejecting loop overwrites the sentinel; with the skip it
survives.

#### F6 (LOW): open question 4, bench/gen `-list` panic

Root cause, confirmed by reading the code:
- `printList` makes `used := make([]bool, s.Items)`.
- `benchTrap` emits `a(i) = 7+i` up to `Items+6` and trap items up to
  `Items+13`.

The data are fine; the listing is wrong. Size `used` dynamically, for
example by growing it to the largest item. This changes neither the
generated tables nor the pinned SHAs.

Recommendation: fix it in #21's PR as a separate commit. #21 adds the t4
rows, and `-list` must reach them to document them in
docs/benchmarks.md. Add a small test that runs the list accounting on a
trap spec. If the orchestrator prefers strict scope, file it as its own
issue. Either way, do not change benchTrap.

#### F7 (LOW): expected gain and regression risk

Measured (single runs, instructions retired) under design B:
- T4 default: 15.8x at 12.5k and linear (1.96x per doubling);
- `-tg`, `-Ad`, `-As -l0`: about 8.4-8.8x at 6.25k.

Go will then beat C on these rows (C 5.0 G at 12.5k). Record that as an
improvement.

Hot-path cost:
- One inlined `cnt < pexMax` per item that takes the chain branch. That
  is not per ancestor, and on ordinary data the walk it guards is longer.
- With F1: one compare per `expand` call and one per `Report`. `expand`
  already compares `cnt` with `zmax` for every extension.

I expect no measurable regression. Confirm with benchstat on
BenchmarkReporter (it covers expand) and BenchmarkMine, as planned. The
single `-tc` run (+4 % B vs main) is load noise. B does not touch that
path (pexMax = MaxInt and AddPex is unchanged), so I draw no conclusion
from it.

#### F8 (LOW): switch for the old/new comparison

For the reporter-level guards (F1), and for option 1 if it were kept, the
internal/fpg comparison must turn the guard off in another package.
- A walk guard can use a `testHooks` field.
- A reporter guard needs an exported method on the internal reporter, for
  example `SetPexGuard(false)`. internal/ is not public API.

A package-level variable (as in my prototype) is unsafe under
`t.Parallel` and should not be used.

### Consumer-by-consumer check (question 1)

| Consumer | Result | Basis |
| --- | --- | --- |
| Reported sets, supports, order; Count/Stats (`-Z`, Summary.BySize); saturation | equal | proof (a)/(b) of the design, rechecked; TestExp21 |
| Count-only (nil sink) | equal | countOnly breaks at k = 1 when cnt = zmax; TestExp21 nilSink |
| Border `-F` | equal | output on the base set only; TestExp21 border |
| Spectrum `-P` | equal | the same countSets calls; TestExp21 cells, Total, SigCount |
| `-eb` LogRatio, Eval value | equal | reads items[:cnt] and supps only; TestExp21 Eval bits |
| ItemSet.Weight (`%w %W %r %R %z`) | equal | wgts indices <= zmax are only written by fills at cnt < zmax, which are identical; TestExp21 Weight bits |
| Other `-e`, `-a`, `-i`, `-p`, rules, item-set-tree path | not reached | mineTree has no AddPex; istree only calls ReportEval. TargetRules is in TestExp21 |
| `-x`, `-u`, `-q`, `-j` | equal | TestExp21 (noPerf, noTail, sort, variants c-j) |
| `-R` appearances / selection | not affected | they change only the tree and the items; not in my random run (selection is covered by fpg tests) |
| zmin interplay, zmin > zmax, `-n0` | equal | TestExp21 ZMin 0..3, ZMax 0..; proof |
| Generators, also with zmin | equal | AddPex is already a no-op; walk skip only |
| `-As -Ac -Ad -At`, `-l0/-l3/-l16`, single tree | equal | TestExp21 (11 variants); the projection sites sit under Xable(2), so cnt < zmax there |
| Closed/maximal | excluded | see F3 |
| Custom EvalFunc or a sink reading PexCount/Uses | none exist | only LogRatio; the CLI and root use only SetSink, SetBorder, AddSpectrum, Spectrum, Stats, Count |
| Cancellation | shifted by at most one tick when zmin > zmax | documented as imprecise; results unaffected |

I found no counterexample for the default/all target, the generators or
the rules paths.

### Recommended answers to the open questions

1. Put the guard only in the miners (`PexUseful()` before the four chain
   walks) and keep `AddPex` an exact port (F2). Add the reporter guards
   of F1 in `expand` and `Report`.
2. No step counter. Use a per-variant PexCount bound on T4, the pexMax
   truth table, mutants, and the white-box sentinel for `expand` (F5).
3. In scope (F1). It is exact and tiny, and it removes measured quadratic
   shapes of the same mechanism. If the maximal-minimal scope is
   preferred, the maintainer must decide with the numbers above.
4. Fix it in the PR as a separate commit, list accounting only (F6).

### Decision

**Accept with changes.** Apply F1 to F5 to the design before
implementation. F1 and F2 are HIGH: F1 is a scope gap of the issue's own
mechanism, F2 would break the reporter's C-parity goldens. No heavy
measurement was done; the T4 25k rows and the variant baselines stay
deferred to the night window as the implementer planned. The prototype is
in challenge/ for reference only. It uses global switches and must not be
copied as is.
