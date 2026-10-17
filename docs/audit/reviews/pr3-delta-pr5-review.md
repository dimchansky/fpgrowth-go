# Review: PR #3 delta (fd256e0..bcaa0dd) and PR #5 (test/crash-regressions, 00ecea9)

Read-only. Clones: `scratchpad/review/pr3` (origin/fix/api-audit = bcaa0dd),
`scratchpad/review/pr5` (FETCH_HEAD = 00ecea9, base f7bb606). All temporary
mutations and probes were reverted (`git status` clean afterwards).

## Verdicts

- PR #3 delta: **ACCEPT WITH CONDITIONS** (one condition, D-1: the DIF-9 guard barely detects the regression it exists to catch).
- PR #5: **ACCEPT** (Low/Info notes only).

---

## Part 1: PR #3 delta

### Fuzz ranges
- FuzzMineItemsets builds its own thresholds (no orderedMax). That is why CI hit
  Count(4)/Count(3) on fd256e0. It now expects ErrInvalidOptions naming
  MaxSupport when `invertedRange` holds.
- FuzzMineRules drops orderedMax and does the same, so both fuzzers now cover
  the rejection path.
- genTestCase still remaps inverted ranges, so equivalence and oracle tests
  run on valid ranges.
- `invertedRange` is described as an "independent" restatement, but it is the
  same formula as checkRange. The fuzzers therefore check that the rejection
  is wired in, not that the boundary is correct. The boundary itself is
  covered by the options_test cases and by my probe below (Info, D-3).
- 60 s fuzzing each:
  - FuzzMineItemsets: 731,701 execs, PASS.
  - FuzzMineRules: 554,131 execs, PASS.

### R-1 tolerance
The conversions are:
- minAbs = ceil(f·W·(1-ε)), raised to at least 1.
- maxAbs = floor(g·W·(1+ε)), saturating.

Both are used as exact counts for item sets and rules (setThresholds →
cfg.MinCount/MaxCount; percentOf is not involved).

Soundness argument:
- In real arithmetic, g(1+ε) < f(1-ε) gives, for every W,
  floor(g·W(1+ε)) ≤ g·W(1+ε) < f·W(1-ε) ≤ ceil(f·W(1-ε)). So the range is empty.
- The check uses 4ε on each side, while the conversion's float products err
  by at most about 1ε (two roundings of u = ε/2). The margin absorbs this.
- Saturation at 2^63 only raises max and clamping min to 1 only raises min.
  Neither can make a rejected range non-empty.

Empirical probe (temporary in-package test):
- 20M random (f, g) pairs, with g 0–13 ulps below f.
- W drawn from [1, 1000], [1, 2^53] and [MaxInt64 - 2^20, MaxInt64].
- 2,319,725 rejections, 0 false rejections (no rejected pair had min ≤ max).
- Near 0.3, rejection starts at 11 ulps below.
- My earlier counterexample (0.30000000000000004 / 0.3) is now accepted and
  reports {a}:3 (test added). R-1 is resolved.

### R-5
`ErrInvalidItem` GoDoc now says that an invalid Options.Items entry matches it
together with ErrInvalidOptions. Resolved.

### TestCancelTreeBuild made sequential
Instrumented latency, cancel → return:

| Setup | Latency |
| --- | --- |
| New (sequential), `GOMEMLIMIT=300MiB -cpu 1 -count=5`, test alone | ≤ 64 µs |
| New, same flags, whole package | 33–47 µs |
| Old (t.Parallel restored), test alone | ≤ 404 µs |
| Old, whole package, 2 runs | 0.09–2.1 ms |

The author's reasoning holds: between cancel and return there is only O(items)
work, and contention inflates it by about 30–50x locally. I could not
reproduce the 219–241 ms from CI here, so that size is plausible but
unverified.

### D-1 (Medium): the DIF-9 guard is now nearly toothless on a fast machine
Mutation: remove the ctx check at the start of Miner.Mine and every
`buildCheckEvery` check, so the whole tree is built before returning.

| Run | Latency per subtest |
| --- | --- |
| Sequential test (new), 3 runs | Simple 104–112 ms, Complex 110–114 ms, SingleTree 104–106 ms, TopDown 427–477 ms, rules 104–107 ms |
| `-race` | 320–617 ms |

All against the 100 ms limit. The guard still fails today, but four of the
five subtests are only 4–14% above the limit on an M1 Max. A faster CPU or a
faster bag/tree build would let the regression pass silently. On fd256e0
(parallel) the same mutation gave 214–396 ms, so the margin came from
contention.

Also, CI's 219–241 ms is in the same range as a real missing abort check,
which is why measuring is better than raising the bound.

**Condition:** make the guard relative. For example:
- measure an uncanceled build time in the test and require latency < build/4, or
- raise n so the full build is well above the bound, or
- since sequential latency is now µs, lower the bound (for example 30 ms) and
  watch CI.

Also fix the PR body ("115–121 ms") against the code comment ("115–490 ms").

### D-2 (Info)
The PR body still says the full build takes 115–121 ms; the comment says
115–490 ms (carried over from R-3).

### D-3 (Info)
The `invertedRange` doc claims an independent restatement but duplicates the
checkRange formula.

### Commands (bcaa0dd)
- gofmt -l: empty.
- go vet, staticcheck: ok.
- `go test -race -count=1 ./...`: 11/11 ok.
- Both fuzz targets, 60 s each: PASS.
- `reference`: build.sh (from the earlier run) + `go test -count=1 ./...` →
  gen, difftest (39.8 s), modcheck ok.

---

## Part 2: PR #5 (test-only)

| Test | Exercises the deviation? | Evidence |
| --- | --- | --- |
| M10 TestItemBase_BuildBag_longSharedPrefix | Correctness on the audit's reproducer shape only | Go's bag is iterative (All: explicit stack; Backward: depth array). A recursive Go rewrite would not crash at depth 100k: goroutine stacks grow to 1 GB, roughly 20 MB here. So the test cannot detect a regression to recursion (P-1). Deterministic. Runtime: 0.10–0.13 s plain, 1.00–1.20 s with -race. Acceptable. |
| O11 goldens dev-prefmt / prefmt-N | Yes | `reference/build.sh release` + `reference/harness/cli/gen.sh` regenerated all 154 cases. dev-prefmt (C exit 139, no "writing" line) and prefmt-N (C exit 0, `a (100)`) are byte-identical to the committed files. The only diff was the pre-existing, documented platform-dependent dev-bigw.exit (138 vs 139 here). The pair isolates isr_prefmt (-N avoids the crash). The Go side equals golden-go via TestGolden. |
| C10 TestMain_closedStdout | Documents runtime behavior (nothing in this repo to mutate) | Real `>&-` via /bin/sh re-exec of the test binary. Asserts that stdout was reopened on /dev/null, the "writing ... [21 set(s)] done" line, and no "write error". Skipped on Windows; the helper skips without its env var. 10/10 under -race. |
| C2 TestBorderEmptyValue | Partly | Mutation "empty slot = 100" → FAIL. "= 0" or "-1" (no-op borders) → still PASS end to end. Those cases are caught by the existing TestParseArgs "border empty value B16" (FAIL under the 0 mutation), so the new test is a complementary end-to-end check. |
| C7 TestLogTimesWallClock | Yes | Mutation: log times from getrusage CPU time instead of wall time → FAIL "writing took [0.00s] ... want at least 0.30s". It cannot be flaky on slow runners because load only lengthens wall time. Assertion ≥ 0.29 s with %.2f rounding is correct. 10/10 under -race. Portable (no OS-specific code). |

### Findings

| ID | Sev | Finding |
| --- | --- | --- |
| P-1 | Low | The M10 test doc says upstream "overflows the stack", implying the test guards against that. In Go it only checks correctness. To make it a real guard, run it in a subprocess or a non-parallel test under `debug.SetMaxStack` (for example 8 MB), or reword the doc to "result check on the reproducer". |
| P-2 | Low | Cross-PR consistency. The tests cite differences.md M10/O11/C10, which exist only on PR #4 (docs/audit-corrections). PR #4's rows also say "no dedicated regression test" / "not testable in process". Whichever PR merges second must update the rows to name these tests (the C10 test does run in a subprocess). |
| P-3 | Info | cases.txt already supports `>&-`. C10 could also be a golden deviation case; the subprocess test is fine as is. |
| P-4 | Info | The C2 end-to-end test cannot see no-op garbage values. That is covered at parse level. |

### Commands (00ecea9)
- gofmt -l: empty.
- go vet, staticcheck: ok.
- `GOOS=windows go vet ./cmd/fpgrowth/`: ok.
- `go test -race -count=1 ./...`: 11/11 ok.
- `-race -count=10` of the C10, C2 and C7 tests: all pass.
- `-race -count=3` of the M10 test: all pass.
- `reference`: build.sh release, gen.sh (diff as above), `go test -count=1 ./...` ok.
