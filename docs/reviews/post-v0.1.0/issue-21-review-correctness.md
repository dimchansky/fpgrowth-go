# PR #36 correctness review (issue #21, NEW-1)

- Issue: dimchansky/fpgrowth-go#21, "Avoid unnecessary perfect-extension work under restrictive size limits"
- PR: https://github.com/dimchansky/fpgrowth-go/pull/36, branch `perf/21-perfect-extensions`
- Head reviewed: `b55ad8b` (`gh pr view 36` headRefOid b55ad8befa9628228bd51c8cd5b4d367416d7367)
- Reviewer: independent correctness critic (FP-growth, perfect extensions, closed/maximal/generator semantics). Performance is covered by another critic.
- Workspace: ~/.cache/fpgrowth-cycle/21/review-correctness/ (clone `fpgrowth-go` at b55ad8b, worktree `main-wt` at origin/main e4cf7d0, binaries in `bin/`, scripts and raw data in `notes/`)

## Scope

Exactness of the three guards (miner walk guard `PexUseful`, the `Report` guard `n = 0`, the `expand` loop skip) against every consumer named in the issue's Scope 2; the pexMax configuration (New, SetSize, SetTarget, Setup, SetPexGuard); the closed/maximal exclusion; the tests and the mutation claim; an independent brute force from the definitions in the night window.

## Evidence examined

- Issue #21 body (no comments), design21.md, design21-challenge.md, ACTIVITY.md, mutants.txt, PR body.
- `git diff origin/main...b55ad8b`: internal/report/report.go, doc.go, internal/fpg/complex.go, simple.go, single.go, both pexguard_test.go, docs/differences.md, CHANGELOG.md, PROGRESS.md.
- Whole reporter: New, SetTarget, SetSize, Setup, updateXmax, Add, AddPex, AddPexPacked, Remove, Xable, Uses, Tail, Report, countOnly, expand, output, countSets, LogRatio, ReportRule.
- Every AddPex/AddPexPacked/PexCount/Uses caller (grep over internal/), `limited` (set only for closed/maximal with zmax < longest transaction, mine.go:94-105; fim16.go:58), the reporter set-up in fpg.go:138-163 (single reporter, SetSize then SetTarget, Setup in Mine), the projection sites (complex.go:199/305, simple.go:140/299, single.go:109, topdown.go:237), the 16-items machine sites.

## Exactness proof check (per guard, per consumer)

Notation: L = reporter level (`cnt`) at which an extension is added; pexMax = zmax for the frequent target (and rules), -1 for generators, MaxInt for `ModeNoExpand` (forced by SetTarget for closed/maximal, including ModeNoFilter), `cm != nil`, or guard off.

1. **Walk guard** (`PexUseful`, evaluated after `Add`, so the skipped extensions would belong to level cnt >= zmax). They would be visible only to Reports at cnt' >= cnt >= zmax. Every such Report now treats them as absent (guard 2). Other readers: `PexCount` (only `xable2`/`deeper`/`fim16.deeper` under `limited`, which is set only for closed/maximal, mine.go:94-105), `Uses` (no caller in the miners), `inUse` (read only by AddPex; any item pushed and popped by the old code ends `false`, the value it had before, because AddPex pushes only when the flag was false; so state at levels < zmax is identical), `Tail` (cm only). No miner uses the return value of AddPex. Confirmed.
2. **Report guard** (`n = 0` when cnt >= pexMax, placed after the cm block and the support gate): cnt > zmax returns in both versions. cnt == zmax: gate `cnt+n < zmin` gives the same verdict whenever an output is possible (zmin <= zmax); for zmin > zmax neither version outputs anything (expand/countOnly emit only sizes >= zmin, <= zmax). countOnly: z = cnt counted iff cnt >= zmin, loop breaks at k = 1 in both. Expand path: fill writes indices > zmax only; `wgts`/`supps`/`items` at indices <= zmax are read by output, Report and LogRatio, and are written only by Add or by fills at levels < zmax, which are unchanged. Border (`-F`), `-eb` LogRatio, spectrum (`countSets`), Stats/BySize/`-Z`, saturation: all evaluate the base set only, same calls. Generators: pexs are always empty (AddPex/AddPexPacked are no-ops for the target), so n = 0 changes nothing. Confirmed.
3. **expand skip** (cnt >= pexMax): in the old loop `cnt+1 > zmax`, so it never recurses and only writes `items[cnt]`, index >= zmax, never read by an output and rewritten by Add. Ticks: one tick per expand call in both; Report may skip one expand call when zmin > zmax (cancellation timing only, documented as imprecise). Confirmed.
4. **Configuration**: pexMax recomputed by every mutator of zmax/target/mode/cm (SetSize, SetTarget, Setup, SetPexGuard) and by New (F4 of the challenge fixed). Single reporter per Mine (fpg.go:138). Rules and rule-based evaluation go through the item set tree (no AddPex); `-x`, `-u`, `-q`, `-j`, `-R` change only tree/items. Projection sites run under Xable(2) of the parent, so at levels < zmax. Variants -As/-Ac/-Ad/-At, -l0/-l16 (packed walks guarded in recSmp16/recSingle), single tree, top-down (no walk): covered by the same argument.
5. **Closed/maximal exclusion**: kept; justification in the PR body and GoDoc is now correct (repository, NoExpand reporting, limited-search gates). The implementer's mutant `pexmax-no-clomax` is caught by TestExtCheckUnlimited/TestOracle, so with the reporter guards the exclusion is in fact necessary. Fine.

## Commands run (results)

| Command | Result |
| --- | --- |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` | clean |
| `go test ./...` | all 12 packages ok |
| `go test -race -count=1 ./internal/report ./internal/fpg` | ok (4.3 s, 43.5 s) |
| guard tests `-v` | all pass; TestMine_PexGuardSame 2,200 comparisons default |
| own mutants, `notes/mut.py` (go test internal/report, internal/fpg, root) | 6/6 caught: Report guard off by one (cnt+1 >= pexMax), expand skip off by one, PexUseful off by one only, NoExpand dropped from pexMax, generators pexMax = zmax, Report guard `>` instead of `>=` (notes/mutants-critic.txt) |
| campaign sensitivity: Report-guard off-by-one mutant binary vs main, 200 runs | 9 PR!=main flagged (campaign is sensitive) |
| oracle sanity: hand example (weights incl. 0), targets s c m g | oracle == CLI |
| **night campaign** `notes/campaign.py night 40000 100000 6`, 21:52-21:53, after review-performance/critic.done, load 3.0 | **40,000 runs, 0 failures**. Every run: PR vs main byte-equal (exit code, stdout incl. `-Z`, output file, `-P` file, stderr without timings). 11,783 runs (s/c/m/g, rc 0, <= 13 items) also equal to the definition oracle. 37,463 runs completed with rc 0; the rest are equal error exits (no frequent items, empty selection file). |

Campaign coverage: shapes random, dense, chain (2-12) and chainbig (13-60 items, up to 20 copies plus partial chains), single long transaction (1-13 and 14-200 items), shared prefixes, T4 (2-60); weights 0,1,2,3,5,7 in 40 % of databases; targets s c m g r; -Ac/-As/-Ad/-At; -l0/1/2/3/16; -x, -j, -u, -q-2..2; -m0..4; -n0..5 or none (zmin > zmax included); -s, -S; -F border; -P spectrum; -Z; -eb and rule measures with -a/-i/-d; -v with %w %W %r %R %z %e %i %Q; -R selection (s c m g) and appearances (r); -c for rules. Script: notes/campaign.py, log notes/night-critic.log, failures file notes/night/fails-100000.json (empty).

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| C1 | LOW | confirmed | PR body still says "Night campaign: pending" and quotes 2,200 comparisons, while ACTIVITY.md records 2 x 55,000 (campaign1/2.txt). Update the evidence section with the final numbers (and this critic's 40,000-run result). | PR body, ACTIVITY 10:50 |
| C2 | LOW | open (not correctness) | Acceptance item "docs/benchmarks.md updated" is not in the diff yet (benchmark rows pending the night measurements; performance critic's area). | `git diff --stat origin/main...b55ad8b -- docs/benchmarks.md` empty |

No Critical, High or Medium correctness finding. No counterexample found by reading, by mutants or by 40,000 brute-force runs.

## Fixes requested

- C1: refresh the PR evidence section. C2: add the docs/benchmarks.md rows with the measured numbers before merge (tracked by the performance review).

## Performance impact

Not assessed here (performance critic). The guards are one compare per chain walk, per Report and per expand call.

## Decision

**ACCEPT WITH CONDITIONS** (correctness): the three guards are exact for every consumer of Scope 2; conditions are the two LOW documentation items.

## Re-review of 5eba17d

Delta 841551f..5eba17d (841551f docs only), reviewed at head 5eba17d.

**Report guard** `n > 0 && cnt >= pexMax`: the added `n > 0` only skips setting n = 0 when it already is 0. Equivalent to the reviewed version.

**expand loop break** (`if r.cnt >= r.zmax { break }` at the top of the loop; the recursion test drops `r.cnt <= r.zmax`). It is active with the guard on and off, for every target that reaches expand. Let c be cnt at loop entry. c is the same in every iteration because cnt++ is undone after each one. Old loop, for c >= zmax: after cnt++ the test `c+1 <= zmax` fails in every iteration, so the loop never recurses and only writes `items[c]`. New loop: it breaks before writing anything. For c < zmax, `c+1 <= zmax` always holds, so the dropped conjunct was redundant and both loops recurse on exactly the same `zmin` test. So the same recursive calls happen in the same order.

What differs: the write to `items[c]` at index c >= zmax. No output reads it (sizes <= zmax read `items[:z]`), and `Add` rewrites it before any other use. The NoExpand path writes its own `items[base:k]`. Ticks are unchanged: expand ticks once per call, and the set of calls is identical.

Per consumer:
- **Reported sets and order:** same expand calls, so the same output calls in the same order.
- **`-Z`/BySize, `-P`, `-F`, `-eb`, Weight:** all are evaluated in `output` on the same sets with the same `supps`/`wgts` entries at indices <= zmax.
- **Generators:** the extension list is always empty, so the loop never runs.
- **NoExpand, closed/maximal:** Report returns before expand on the NoExpand branch, so they are not affected.
- **zmin:** the recursion test is unchanged, so zmin > zmax still outputs nothing.
- **zmax unlimited:** cnt never reaches MaxSize.
- **expand calls:** expand is only called with cnt <= zmax (Report's gate, and the recursion runs only when c < zmax).

Exact.

| Command | Result |
| --- | --- |
| implementer's mutants2.py (14 mutants) run on my clone at 5eba17d, default go test of internal/report, internal/fpg | 14/14 CAUGHT (notes/mutants2-critic.txt): `expand-skip-off` (no break, so no size test at all) fails TestReporter_ExpandSkip, `expand-skip-early` and `expand-skip-late` fail oracle/T4 tests |
| campaign.py, PR = 5eba17d CLI vs main e4cf7d0, 12,000 runs, seeds 300000.., 6 processes, 22:35, load 2.8, nothing else heavy running | **0 failures**: all byte-equal to main; 3,444 also equal to the definition oracle; 11,199 with exit 0 (notes/night2.log) |

Earlier findings: C1 (stale PR evidence) has been addressed in the PR body according to the implementer's log. C2 (docs/benchmarks.md) is addressed: benchmarks.md is in the diff (+43 lines). There are no new findings.

**Decision for 5eba17d: ACCEPT** (correctness).
