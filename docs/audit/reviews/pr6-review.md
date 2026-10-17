# Review: PR "Probe candidates before the closed scan copies them" (fix/extcheck-single-cover, 5fb8603)

Reviewer: independent (audited M5 earlier, did not write this change). Read-only: no pushes, comments, approvals.
Worktrees: `scratchpad/review/pr6` (PR head 5fb8603), `scratchpad/review/pr6-base` (origin/main 350df77).
Binaries/tools: `scratchpad/audit/m5/_audit/m5/` (`fpg6` = PR, `fpgmain` = main, `fpg6W1..W4` = mutants,
`bf*`, `bfs*`, `meas.sh`).

## Verdict: ACCEPT

The change is exact: proven below, no differential failures, and every probe mutant is caught. It removes the T4
quadratic (main 21.8 s → PR 0.26–0.62 s at n = 100,000; linear scaling shown for 10k/30k/60k) and has no
measurable regression on the M5 configs. One doc accuracy note (the C timing quoted is of a crashing run) and one
residual quadratic shape that this PR doesn't target. Neither blocks the merge.

| ID | Severity | Status | Finding |
| --- | --- | --- | --- |
| PR6-1 | Low (doc) | Confirmed | The "C 0.24 s" for T4 at n = 100,000 in `docs/m5-investigation.md` is the time of a C run that segfaults (exit 139, no output). C crashes for n ≥ ~60,000; Go prints the correct `x (100010)`. |
| PR6-2 | Info | Confirmed | Residual quadratic, not addressed (not a regression): a long first cover transaction plus short others, so the probes fail and `initCand` still copies the long one. `lp2` (one 50k-item line + 50k one-item lines) `-As -tc -s-2 -n1`: main 6.77 s, PR 6.76 s, C 3.39 s (C is quadratic here too). Suggestion: start the candidates from the shortest cover transaction. |
| PR6-3 | Info | Confirmed | Branch base 5c8b3d3 is behind origin/main 350df77. `git merge-tree` merges cleanly, and main has no extcheck changes since the base. |

## 1. Exactness argument (against my M5 proof)

- `covers(j)` returns true iff every bit of the cover (c.nz/c.cover, already set by checkPath/bitCover before
  `scanClosed`) is set in j's bit set, i.e. j is in every cover transaction.
- The cover is exactly the positive-weight transactions containing X. Zero-weight transactions are dropped in
  `newExtChecker` (`w <= 0: continue`), and with checkPath the prefix's cover equals cover⁺(N̂) (M5 §3.2).
- So `covers(j)` for j ∉ X ⇔ supp(X∪{j}) = supp(X) ⇔ X is not closed. A true probe is sound: j is drawn from
  `txItems` (bag codes, packed bits unpacked) and skipped if marked in X, so j is in the universe and not in X.
- A false probe returns to the unchanged full scan. So the result is the same as before, except for an early
  "not closed" that is itself correct. No needed check is skipped.
- Items outside the recoded bag are irrelevant (infrequent ⇒ supp(X∪j) < smin ≤ supp(X)).
- Sparse and dense rows are both handled by `covers` (the sparse branch was verified earlier by hook tests and is
  exercised again here through rowsSparse/rowsMixed).
- Maximal is untouched: `probeCand` is called only from `scanClosed`.
- Cost guard: probes run only if |t| > 4·nz; each `covers` costs ≤ nz word ops, so the probes cost ≤ |t|, the cost
  of `initCand`. Single-transaction cover: the first non-X item of t succeeds, so a check reads ≤ |X|+1 items.

## 2. Tests and mutants

`go vet ./internal/fpg`; `go test ./internal/fpg` ok. New tests `TestExtCheckSingleCover` and
`TestExtCheckChainScaling` pass.

Mutants of `probeCand` (source restored afterwards; worktree clean):

| mutant | change | new tests | other extcheck/oracle tests | my bfs (sparse) |
| --- | --- | --- | --- | --- |
| W1 | probe always reports "not closed" | SingleCover FAIL | Unlimited, Brute FAIL | 10/50 runs fail |
| W2 | first probed item counted as covering | SingleCover FAIL | Unlimited, Brute FAIL | 10/50 fail |
| W3 | probe disabled | ChainScaling FAIL | pass (performance only) | n/a |
| W4 | probe may pick items of X (inSet not checked) | SingleCover FAIL | Unlimited, Brute FAIL | 10/50; 10/40 with long patterns |

Note: my dense small-universe harness (`bf`) does not detect W1/W4. In such data `decide` almost always picks the
vertical test, so `scanClosed` rarely runs. The sparse harnesses are where the scan and the probe are exercised.

## 3. Differential runs on the PR binary (independent brute force, 0 failures)

| harness | command | runs |
| --- | --- | ---: |
| bf focus (c/m, dense, small `-n`), big, istree 1/6 | `./bfbin2 -focus -big -eval 6 -n 3000 -seed 61 -go ./fpg6` | 33,000 |
| bf wide zmax 1..k, focus | `./bfbin3 -wide -focus -n 3000 -seed 62 -go ./fpg6` | 33,000 |
| bfs sparse, 200–2,000 items | `./bfsbin -n 300 -seed 67 -go ./fpg6` | 1,500 |
| bfs2 sparse with long planted patterns (≤ 41 items) | `./bfs2bin -n 300 -seed 66 -go ./fpg6` | 1,500 |
| **total** | 11 variants (bf) / 5 variants (bfs) | **69,000, 0 failures** |

## 4. Timings (`/usr/bin/time -l`, shared machine)

T4 (`-At -tc -s-17 -n1`, 17 × chain of n items + n+10 × `x`):

| n | main | PR | C |
| ---: | ---: | ---: | ---: |
| 10,000 | 0.24 s | 0.03 s | 0.01 s |
| 30,000 | 2.17 s | 0.08 s | 0.05 s |
| 60,000 | 8.24 s | 0.18 s | 0.16 s, **segfault (139)** |
| 100,000 | 21.81 s | 0.26–0.62 s | 0.29 s, **segfault (139), no output** |

The outputs of PR and main are identical (md5). `-At -tm`: PR 0.27 s / main 0.26 s (unchanged).

Regression check (3 runs each; medians main → PR): d2000x40d60 `-tc -s2 -n4` 0.25 → 0.24 s; `-n2` 0.01 → 0.01 s;
d5000x60d40 `-tc -s5 -n3` 0.14 → 0.15 s; sparse-large `-tc -s0.05 -n3` 0.67 → 0.69 s; `-tm -s0.05 -n3`
0.92 → 0.83 s. RSS equal within 1 MB. Set counts equal.

## 5. Recommendations (non-blocking)

1. PR6-1: replace the C figure with one at a size where C completes (e.g. n = 30,000: C 0.05 s, PR 0.08 s,
   main 2.17 s), or note the C crash.
2. PR6-2: optionally initialize the candidates from the shortest cover transaction (or probe several cover
   transactions) to cover the long-plus-short shape; add `lp2` to the scaling test if done.
