# PR #33 correctness review (issue #19, M5A-1)

- Issue: #19, "Optimize maximal itemset mining with small MaxSize on dense datasets"
- PR: https://github.com/dimchansky/fpgrowth-go/pull/33, branch `perf/19-maximal-size-limit`
- Head reviewed: `98c59854bb7d3340842e9b374c3550ad606acf2c`, in a fresh clone under `scratchpad/review/pr33c/repo`
- Reviewer specialization: FP-growth correctness, closed/maximal set theory, the M5 extension check. This is the correctness critic; another critic covers performance.
- Scope: `internal/fpg/extsib.go` and its hooks in `extcheck.go` (`checkPath`, `scanMaximal`/`vertMaximal` work counters, `newExtChecker`) and `mine.go` (test hooks); the proof text in `docs/m5-investigation.md` §3.2 (last bullet) and §4.1/§6. Performance claims were not re-measured.

## Analysis of the change

Notation: N = prefix (|N| ≥ 2), P = N without its last item h, L = |N| − 2, N̂ = the checked set (N plus the perfect extensions the reporter added), E = all items.

1. **Covers.** `path[L]` = cover(P) and `path[L+1]` = cover(N) = cover(N̂) ⊆ cover(P). Both covers leave out weight-0 transactions. `sibBuild` gives the positions r = 0, 1, … to the bits of `path[L]` in ascending order, so the positions are a bijection onto cover(P). R_j collects the positions of the cover transactions that contain j. R_h is therefore cover(N) in position space. `planes` holds bit b of each position's weight, with np = `bits.Len64(maxW)` from the global planes, so every weight bit is represented. With no planes, every weight is 1 (`maxW ≤ 1`). So Σ_b 2^b·popcount(R_h & R_j & plane_b) = supp(N̂ ∪ {j}). This is exactly what `suppWith` computes over the child cover.
2. **Completeness of the entries.** Take any j ∉ N̂ with supp(N̂ ∪ {j}) ≥ smin. Then supp(P ∪ {j}) ≥ smin (weights ≥ 0) and freq(j) ≥ smin. So j occurs in cover(P), is touched, has a row, and passes `supp ≥ smin`. It is also not in P, because P ⊆ N̂. So it is an entry and `sibMaximal` tests it. Items that are not entries have supp(N̂ ∪ {j}) < smin, or j ∈ P ⊆ N̂. The `inSet` skip only removes items of N̂, which the definition (j ∉ X) excludes anyway. A perfect extension that is *not* in N̂ (for example under `-x`) is counted, which is the correct answer. So the verdict equals the complete one-item check, and by anti-monotonicity that is maximality (m5-investigation §3.1). `found == false` falls back to the ordinary check.
3. **Cache validity.** `sibs[L]` depends only on prefix[0..L] (through `path[L]`), smin and the bag. `checkPath` keeps levels < k unchanged and calls `sibReset(k)`, which drops every level ≥ k before any level is recomputed. Pure ascent (k = |prefix|) keeps `sibs[|prefix|−1]`, which is still the cover of the unchanged prefix. That is correct. `check()` (empty prefix) does not touch `path`.
4. **Buffer reuse.** A row is cleared when its item is first touched. Rows of items that are never touched are never read. `planes` is cleared in full. `slot` and `supp` are reset for every touched item. At most nFreq rows are touched, so `need = nFreq·words` is enough.
5. **Budget.** A refusal sets `refused`, and the child then takes the ordinary check. The release loop only frees levels that are not built. The verdict does not depend on gating or cost decisions.
6. **Closed target** is unaffected (`c.maximal &&`). Packed items and fim16 use the same unpacked `txItems` as the ordinary check.

Conclusion: the sibling rows preserve the complete extension check exactly. The §3.2 proof text is correct. Its remark that perfect extensions of P "are in N̂" is not needed for the conclusion, and the text itself covers the `-x` case.

## Commands run (results)

| Command | Result |
| --- | --- |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` | clean |
| `go test ./...`, `go test -race ./...` | all ok |
| `cd reference && ./build.sh && go test ./...` | ok (difftest 47 s) |
| `FPG_M5_CASES=10000 FPG_ORACLE_CASES=5000 FPG_M5_UNLIMITED=300 FPG_SIB_CASES=3000 go test ./internal/fpg -run 'TestExtCheck\|TestOracle$\|TestDeviations\|TestSeek' -v` | ok. TestExtCheckOracle 220,000 comparisons; TestOracle 220,000; TestExtCheckUnlimited 26,400 equal; TestExtCheckSiblingRows 95,271 runs, 1,215,669 builds (372,718 multi-word, 795,851 weighted, 451,837 with `-x`), 4,863,460 checks from rows; SiblingRowsOracle 6,094 runs; LocalSweep negative control 5,072/33,000 differ, as documented |
| Independent brute force `scratchpad/review/pr33c/bf/main.go` against the PR CLI (see below) | **143,712 runs, 0 failures** |
| Mutation testing `scratchpad/review/pr33c/mutate.py` (default `go test ./internal/fpg`, no env vars) | 18 of 19 mutants caught; M15 survives (finding C-1) |

### Independent brute force

`scratchpad/review/pr33c/bf/main.go` imports nothing from the repository. It enumerates every frequent set by DFS over transaction bit sets, with weighted supports. A set is maximal if it is frequent and has no frequent proper superset. The superset test runs against the whole frequent family when the family has at most 3,000 sets, and otherwise uses the one-item extension. The result is then filtered to zmin ≤ |X| ≤ zmax, and supports are compared exactly.

Generated data:
- 1–400 transactions, or 400–1,600 with `-big`, so most covers span many words with a ragged last word.
- 4–24 items, densities from 0.2 to 0.8, correlated patterns and a near-universal item.
- Duplicates (merged weights) in half the datasets; `-w` weights of 1–3, 0–3 or 0–9 (zero weights and up to 4 planes).
- Options: `-n1..-n5`, random `-m`, `-As`/`-As -l0`/`-As -l16`/`-Ac`/`-Ac -j`/`-Ad`/`-Ad -l0`/`-Ad -l16`/`-At`, random `-x`, `-u`, `-q{2,1,0,-1,-2}`.

The forced modes ran on an instrumented copy (`scratchpad/review/pr33c/patched`). It adds one file and one line, which select `sibForce`, `sibAt`, `verify` and `sibCap` by environment variable and print `sibStats`. The production rows used the unmodified PR binary.

| Seed | Binary / mode | Runs | Fails | Builds | Checks from rows | Refused |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| 101 | PR, production | 35,880 | 0 | — | — | — |
| 102 | PR, production, big | 11,988 | 0 | — | — | — |
| 103 | force + verify | 35,916 | 0 | 3,571,541 (1,372,974 multi-word) | 13,058,162 | 0 |
| 104 | at random child 0–5 + verify | 23,946 | 0 | 1,130,682 | 4,148,554 | 0 |
| 105 | production rule, budget random 0–199 words, big, verify | 11,988 | 0 | 70,720 | 216,728 | 55,170 |
| 106 | force + verify, big | 12,000 | 0 | 1,298,596 (1,220,396 multi-word) | 4,958,254 | 0 |
| 107 | production rule (stats), big | 11,994 | 0 | 128,146 | 412,362 | 0 |

The brute force is sensitive to the bugs in question:
- Mutant "no invalidation": 666/1,794 runs fail with rows forced, and 591/1,794 under the production rule on big data.
- Mutant "`sp > smin`": 411 and 92 failures.
- Mutant M15 under a low budget: 20 panics in 9,000 runs.

### Mutants (default `go test ./internal/fpg`)

| Mutant | Result |
| --- | --- |
| M1 row filter `supp > smin` | caught |
| M2 build skip `freq <= smin` | caught (SiblingSupp) |
| M3a words `(n+62)/64` (drops last word if n mod 64 = 1) | caught |
| M3c check uses `words−1` | caught |
| M4 drop top plane in `supp` | caught |
| M4b drop top plane in fill | caught |
| M5 `sibReset(k+1)` | caught |
| M5b no `sibReset` | caught |
| M6 `sp > smin` | caught |
| M7 skip only h (perfect extensions counted) | caught |
| M8 no row clear on reuse | caught |
| M9 no plane clear on reuse | caught |
| M10 scratch reset only for kept items | caught |
| M11 drop rows with supp(P∪{j}) = supp(P) | caught |
| M13 build from child cover `path[L+1]` | caught (SiblingSupp only) |
| **M15 budget release also frees built levels** | **SURVIVED** |
| M19 silently disable rows (look up `prefix[L]`) | caught (counters in SiblingRows/SiblingWork) |
| M20 plane counts not shifted | caught |
| M21 planes ignored | caught |

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| C-1 | MEDIUM | confirmed (test gap; production code correct) | No default test exercises the memory-budget *release* branch of `sibWorth`, where buffers of unbuilt levels are freed and the build is retried while another level is built. Changing `!t.built && i != L` to `i != L` (which frees a built level's rows and then panics in `sibMaximal`) passes the whole default `go test ./internal/fpg`. `TestExtCheckSiblingWork` only covers a budget below a single level. The branch is reachable: the same mutant panics in 20 of 9,000 brute-force runs with a random small `sibCap`, and the unmutated code had 55,170 refusals with 0 failures. | `extsib.go` `sibWorth` release loop; `mutate.py` M15; brute-force seed 205 against `fpg-mut-M15` |
| C-2 | LOW | hypothesis (not reproduced) | `sibEnt.off` is `int32(k*words)`. It wraps when nFreq·words > 2³¹, which gives wrong rows and so silently wrong verdicts. Under `sibAuto` the budget allows this only if the bag has more than about 1.07·10⁹ item occurrences (a horizontal form above 4 GB). The `sibForce`/`sibAt` test modes skip the budget. | `extsib.go` `sibBuild` (`off: int32(k * words)`), `sibCap = 2·len(txItems)` |

## Fixes requested

- **C-1:** add a default test where one level holds rows and a later build at another level needs the release branch. For example, at `-n4` set `sibCap` between one level's and two levels' rows, so the deeper build first frees unbuilt buffers. Assert that the release happened, for instance with a `released` counter in `sibStats`, and that the verdicts are unchanged (verify mode). It must kill mutant M15.
- **C-2 (optional):** store `off` as `int`, or check `nFreq*words <= math.MaxInt32` in `sibWorth` (refuse otherwise) and in the forced modes.

## Performance impact

Not re-measured; the performance critic covers it. Observations from the test statistics: peak row memory in my runs was ≤ 1,479 words. The production rule builds rows on dense multi-word data (128,146 builds over 11,994 big runs).

## Decision

**ACCEPT WITH CONDITIONS** (C-1 Medium, C-2 Low). I found no incorrect result. The theory holds, 143,712 independent brute-force runs and the large repository runs show 0 differences, and 18 of 19 mutants are caught by default tests.

---

# Re-review of cf4c6d7

- Head reviewed: `cf4c6d778b4e5611e35b10cd206b2325d4e13105` (commits 1edb5be, 19ca709, b500e1b, cf4c6d7 on top of 98c5985). Full correctness re-review against issue #19, same rules and specialization.

## Analysis of the changes

- **The bound r (`miner.left`, `freqBelow`).** `grep` shows that `c.left` and `freqBelow` are read in exactly one place, `sibWorth` (`rem`, then `gain`). `sibWorth` only returns whether to build. The rows built are the same function of `path[L]` in every case, and section 2 of round 1 showed they give the ordinary verdict. Nothing about r changes which items are entries, the inSet skip, or `sibMaximal`. The miner's plumbing only assigns `m.left` around `rep.Report()` and `fim.mine()`, and `mine.go` copies it into `chk.left` before every check. `m.left` is not read anywhere else in the miner. On an error return `m.left` is not reset to -1, which affects cost only. The `cloMaxCheck` hook wrapper only adds the same copy. **r cannot change a verdict.** `freqBelow[h]` is indexed by a checker item code h < k, so it is in range.
- **Budget in every mode (`sibFits`).** Refusal still falls back to the ordinary check, and release still frees only unbuilt levels. It is now counted (`released`).
- **`sibEnt.off` is `int` (C-2).** Fixed.
- **`costs` helper.** Same formula as before. `decide`'s choice of scan vs vertical check and `sibCheck`'s estimate are unchanged (`max(planes,1)` equals the old `pl`).
- **Work counter.** Now a local in `scanMaximal`, added once per check; it feeds cost only.
- **32-bit.** `GOOS=linux GOARCH=386 go vet ./... && go test -c ./internal/fpg` succeeds.
- **Docs.** §4.1 states the rule and the bound correctly, including "steer only the cost decision, never a verdict". The §3.2 proof is unchanged and still correct.

## Commands run (results)

| Command | Result |
| --- | --- |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` | clean |
| `go test ./...`, `go test -race ./...` | all ok |
| `cd reference && ./build.sh && go test ./...` | ok |
| linux/386 vet and test compile | ok |
| `FPG_M5_CASES=10000 FPG_ORACLE_CASES=5000 FPG_M5_UNLIMITED=300 FPG_SIB_CASES=3000 go test ./internal/fpg -run 'TestExtCheck\|TestOracle$\|TestDeviations\|TestSeek' -v` | ok (566 s). ExtCheckOracle 220,000; TestOracle 220,000; Unlimited 26,400 equal; SiblingRows 95,271 runs, 1,170,586 builds, 4,690,940 checks from rows; SiblingLeft 34,687 bounded checks, 0 exceeded |
| Brute force `pr33c/bf` (now also counts `released`) against the new PR binary and the instrumented copy (`runall2.log`) | **155,616 runs, 0 failures** |
| Brute force against four r-plumbing mutant binaries, production rule + verify (`runR1.log`, `runR2.log`) | **35,928 runs, 0 failures** |
| Mutants, default `go test ./internal/fpg` (`mutate2.log`) | all 19 correctness mutants caught, **including M15** (by TestExtCheckSiblingRelease) |

### Brute-force campaign (seeds 301–308)

| Seed | Binary / mode | Runs | Fails | Builds | Refused | Released |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| 301 | PR, production | 35,904 | 0 | — | — | — |
| 302 | PR, production, big | 11,958 | 0 | — | — | — |
| 303 | force + verify, unlimited budget | 35,904 | 0 | 3,220,084 | 0 | 0 |
| 304 | at random child + verify, unlimited budget | 23,910 | 0 | 1,262,695 | 0 | 0 |
| 305 | force + verify, budget random 0–599 words, big | 11,994 | 0 | 1,187,742 | 270,180 | 27,120 |
| 306 | at random child + verify, random budget, big | 11,994 | 0 | 636,205 | 158,337 | 10,395 |
| 307 | production rule + verify, big | 11,982 | 0 | 44,578 | 0 | 0 |
| 308 | production rule + verify, random budget, big | 11,970 | 0 | 38,347 | 7,764 | 647 |

### r-plumbing mutants

None of these may change a verdict; the brute force runs used the production rule with verify, seeds 401 (normal) and 402 (big, random budget).

| Mutant | Default tests | Brute force |
| --- | --- | --- |
| R1 `rem = 2^40` (builds early) | survives (cost only) | 8,982 runs, 0 fails, 33,509 builds |
| R2 `rem = 0` | caught (SiblingWork) | — |
| R3 miner bound ignored | survives (cost only) | — |
| R4 `chk.left = 2^30` | caught (SiblingLeft) | 8,982 runs, 0 fails |
| R5 build at every child after the first (all cost gates removed) | survives (cost only) | 8,982 runs, 0 fails, 693,882 builds |
| R6 forced modes ignore the budget | caught (SiblingRelease, SiblingWork) | — |
| R7 `freqBelow[h+1]` | survives (cost only) | — |
| R8 `m.left = 0` in every miner loop | caught (SiblingLeft) | 8,982 runs, 0 fails |

## Findings

| ID | Severity | Status | Summary |
| --- | --- | --- | --- |
| C-1 | MEDIUM | **resolved** | `TestExtCheckSiblingRelease` covers the release branch and kills M15 |
| C-2 | LOW | **resolved** | `sibEnt.off` is now `int` |
| C-3 | LOW | confirmed, cost only | The cost mutants R1, R3, R5 and R7 (r too large, miner bound ignored, all cost gates removed, `freqBelow` off by one) pass the default tests. Their verdicts are correct (brute force, 0 failures), but no default test pins that the bound actually *prevents* a build. This guards performance, not correctness; it is passed to the performance critic. Optional: a `TestExtCheckSiblingWork` case where `c.left` is the binding bound and stops a build. |

## Decision

**ACCEPT.** There are no Critical, High or Medium findings, and the only open item (C-3) is Low and cost-only. The r plumbing provably steers only the build decision. All brute-force runs (155,616, plus 35,928 on r mutants) and all repository campaigns show 0 differences, and every correctness mutant is caught by the default tests.

---

# Re-review of 6f07d9b

- Head reviewed: `6f07d9b7be2fa0d76ed5fd0af7d7db4b39e5f613` (commits af5e58b and 6f07d9b on top of cf4c6d7).

## Analysis of the changes

- **New estimator state can only affect the build decision.** The new state is `sibLevel.bound`, `histLater`, `histBound` and `extChecker.zmax`. `grep` shows these fields are written only in `sibCheck` (`s.bound += r`), `sibReset` (history sums) and `mineItemSets` (`chk.zmax`). They are read only in `sibWorth`, to compute `rem` and then `gain`, and `sibWorth` decides only whether to build. `r` is now computed in `sibCheck` for every mode, but it is used only by `sibWorth` (auto mode) and by `s.bound`. `freqBelow[h]` is indexed by a checker code h < k. The rows, the entries, `sibMaximal` and the fallback are unchanged, so the round-1 proof still holds.
- **Reset and invalidation.** `sibReset(from)` runs at the same point as before, before any level ≥ k is recomputed. It still clears all build and cache state (`built`, `refused`, `work`, `need`, `kids`, `ents`) and now also `bound`.
  - The history sums are kept on purpose. They are statistics per level that only scale the cost estimate, so keeping them stale cannot touch a verdict.
  - Every level's history is updated at each reset, including resets of levels that were never used. Then `kids = 0` and `bound = 0`, so those resets add 0.
- **Edge values.** If the checker is built without `zmax` (zmax 0), the scaling is skipped, which only affects cost. The `float64` scaling is bounded by r times a ratio ≤ 1, because histLater ≤ histBound: each later child that is checked is among the bounded ones, as asserted by TestExtCheckSiblingLeft. A wrong ratio would again change cost only (E5 below).
- **Docs.** The new §4.1 text matches the code and says the rule cannot change a verdict.

## Commands run (results)

| Command | Result |
| --- | --- |
| `gofmt -l .`, `go vet ./...`, `staticcheck ./...` | clean |
| `go test ./...`, `go test -race ./...` | all ok |
| linux/386 test compile | ok |
| `cd reference && ./build.sh && go test ./...` | ok |
| `FPG_M5_CASES=10000 FPG_ORACLE_CASES=5000 FPG_M5_UNLIMITED=300 FPG_SIB_CASES=3000 go test ./internal/fpg -run 'TestExtCheck\|TestOracle$\|TestDeviations\|TestSeek' -v` | ok (377 s). ExtCheckOracle 220,000; TestOracle 220,000; Unlimited 26,400 equal; SiblingRows 95,271 runs, 1,169,685 builds, 4,685,313 checks from rows; SiblingLeft 34,687 bounded checks, 0 exceeded |
| Brute force `pr33c/bf` against the new PR binary and the instrumented copy (`runall3.log`, seeds 501–507) | **62,820 runs, 0 failures** |
| Brute force against four estimator mutant binaries, production rule + verify, normal data and big data with random budget (`runE.log`) | **26,376 runs, 0 failures** |
| Mutants, default `go test ./internal/fpg` (`mutate3.log`) | all 19 correctness mutants caught (M15 included); estimator mutants below |

Seeds 501–507 (forced modes ran on the instrumented copy, the rest on the unmodified PR binary):

| Seed | Mode | Runs | Builds | Refused | Released |
| --- | --- | ---: | ---: | ---: | ---: |
| 501 | PR binary, production | 17,922 | — | — | — |
| 502 | PR binary, production, big | 6,000 | — | — | — |
| 503 | force + verify | 11,922 | 1,185,844 | — | — |
| 504 | at random child, random budget, big | 5,994 | 312,623 | 70,948 | 5,500 |
| 505 | force, random budget, big | 5,988 | 617,204 | 138,449 | 13,658 |
| 506 | production + verify, random budget, big | 8,994 | 30,883 | 4,342 | 99 |
| 507 | production + verify, big | 6,000 | 22,478 | 0 | 0 |

### Cost and estimator mutants (verdicts must not change)

The r mutants R1, R3, R5 and R7 now **fail** the default tests, through the new TestExtCheckSiblingBound. This resolves C-3 for them. Estimator mutants:

| Mutant | Default tests | Brute force |
| --- | --- | --- |
| E1 no scaling at shallow levels | caught (SiblingBound) | 0 fails |
| E2 histBound never accumulates | caught (SiblingBound) | — |
| E3 history overwritten instead of summed | survives | — |
| E4 max-size level test off by one (`L+1 < zmax`) | survives | 0 fails |
| E5 histLater ×1000 (ratio > 1, aggressive builds) | survives | 0 fails |
| E6 `bound` not reset | survives | — |
| E7 zmax unset / set to 2^40 | survives | 0 fails |

The surviving estimator mutants change only when rows are built. The brute force on E4, E5 and E7 confirms that they keep exact results. Whether a test should pin that the scaling is accurate is for the performance critic.

## Findings

| ID | Severity | Status | Summary |
| --- | --- | --- | --- |
| C-1 | MEDIUM | resolved (round 1) | — |
| C-2 | LOW | resolved (round 1) | — |
| C-3 | LOW | partly resolved; cost only | R1/R3/R5/R7 are now caught. E3–E7 (history accumulation, max-size level test, reset of `bound`, zmax plumbing) pass the default tests. Verdicts are unaffected (brute force 0 fails); only the build decision's accuracy is unpinned. Passed to the performance critic. |

## Decision

**ACCEPT.** There are no Critical, High or Medium findings. The new estimator state is used only in the build decision. Its resets follow the cache resets, and the history it keeps cannot affect a verdict. All brute-force runs (62,820, plus 26,376 on estimator mutants) and all repository campaigns show 0 differences, and every correctness mutant is caught by the default tests.
