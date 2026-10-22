# PR #35 correctness review (issue #20, NEW-3)

- **Issue:** #20, reduce quadratic closed-item-set checking on covers that mix long and short transactions
- **PR:** https://github.com/dimchansky/fpgrowth-go/pull/35, branch `perf/20-closed-check`
- **Head SHA reviewed:** `bec16810b3120f7d25c37ab0d0941bf9b4c1249b`, compared with `origin/main` `ba63d14`
- **Reviewer specialization:** correctness of FP-growth and closed item sets, and the M5 extension check. Performance is covered by the other critic.
- **Working directory:** `~/.cache/fpgrowth-cycle/20/review-correctness`. It holds a fresh clone (`repo/`), the mutant worktree (`mut/`), the `main` worktree (`main-wt/`), my brute force (`bf/`) and the raw outputs (`campaign-*.txt`, `mutants-result*.txt`, `md5-sample.txt`, `test-*.txt`, `coverage-default.txt`).

## Scope

I checked that every new path in `internal/fpg/extcheck.go` is exact:

- stage a (shortest of t0 and the spread transactions);
- stage b (guard `|t*|−|X| > 4n`, break at `|t| ≤ |X|`);
- re-probing t* when t* ≠ t0;
- intersection of t0 when t* ≠ t0;
- skipping t* in the spread loop;
- the generic `seek` and `keepSeek`, and the `gallop` decision at the call sites;
- `setLen`, and the `fpgforce` thresholds.

Cross-cutting conditions: weights including 0, perfect extensions (`-x` on and off), packed items (`-As`/`-Ad` with `-l0`/`-l4`/`-l16`), variants `-As -Ac -Ad -At`, item orders `-q 2 1 -1 -2 0`, `-n1..-n4`, `-m1/-m2`, plus some `-tm` as a control. I also checked the docs: `m5-investigation.md` §4 and §7.5, the CHANGELOG and PROGRESS.

## Exactness argument (from reading the code)

- **Every t is a positive-weight cover transaction.** t0, the t_p and every stage-b candidate come from bits of `c.cover`. Transactions with weight ≤ 0 are skipped when the checker is built (`newExtChecker`: `if w <= 0 { continue }`), so they have no transaction index and cannot be a cover bit. A zero-weight t* therefore cannot happen.
- **The verdict does not depend on the start.** The set is closed iff ⋂(cover) \ X = ∅. `initCand(ts)` gives ts \ X. Every other cover transaction is then intersected:
  - t0, explicitly, when ts ≠ t0;
  - the t_p (never t0, since `i != last` with `last=0` excludes word 0);
  - the full scan, which skips only the first bit of word 0, i.e. t0.
  Repeats are idempotent.
- **The stage-b break cannot change the verdict.** It only stops the search for a shorter start. Any cover transaction is a valid start, so even a wrong break condition (or a wrong `setLen`) affects only cost. Mutants K8 and K17 confirm this.
- **The probes stay exact.** `probeCand(ts)` uses the same exact `covers()` as `probeCand(t0)`, and it excludes X through `inSet`.
- **`keepSeek` is correct.** Candidates are ascending and `seek` returns the first index ≥ the candidate from the previous position. The bound `i == len(items)` breaks correctly. `seek[T ~int32]` is the old code generalised, and is tested by `TestSeek`, `TestSeekItems` and the K5/K6 mutants.

## Evidence: commands and results

### 1. Independent brute force

`bf/main.go` is my own code and imports nothing from the repository. It works from the definition: X with zmin ≤ |X| ≤ zmax and supp ≥ smin is reported iff supp(X∪{j}) < supp(X) for every j ∉ X, with weighted supports (maximal: < smin). It runs the CLI binary and compares sets and supports exactly. With `-bin2` it also requires the output to be byte-identical to `main`.

Shapes:

| Shape | Content |
| --- | --- |
| `lp` | one long line, or suffix chains every 16/32/64/100, plus 1–3-item short lines and duplicates |
| `adv5` | the trap, with N = 8..260 and extra short a-lines |
| `mix` | long and short transactions over a hot core |
| `chain` | nested prefixes |
| `neardup` | near-duplicate long lines with private items |
| `words` | long and short transactions spread over several words |
| `manyw` | (added during the review) a hot item in 600–1400 transactions, so covers span 10–22 non-zero words; "almost" items are missing from 1–3 random cover transactions |
| `dense` | small, dense |

Filler lines push `decide` onto the scan. Weights are all 1, 1..5, or include 0, with weight 0 also put on the long lines.

| Run | Binary | Runs | Failures | Byte-identical to `main` |
| --- | --- | ---: | ---: | ---: |
| smoke | PR | 2,000 | 0 | 2,000 |
| main campaign (seed 100000) | PR | 60,000 | 0 | 60,000 |
| forced campaign (seed 500000) | `-tags fpgforce` | 30,000 | 0 | 30,000 |
| `manyw` | PR | 3,000 | 0 | 3,000 |
| `manyw` | forced | 3,000 | 0 | 3,000 |
| mixed with `manyw` (seed 2000000) | PR | 8,000 | 0 | 8,000 |
| audit `reference/_audit/m5/bfs` (seed 21) | PR | 2,000 | 0 | – |
| audit `reference/_audit/m5/bfs` (seed 22) | forced | 2,000 | 0 | – |

The main campaign compared 68.1 M expected sets. **Total: 110,000 runs with 0 failures**, of which 106,000 are byte-identical to `main`.

**Path reach in the default build.** I built a coverage binary and ran 3,000 runs with `covermode=count` (`coverage-default.txt`). Every new block was executed:

| Block | Executions |
| --- | ---: |
| stage b | 10.7 M |
| stage-b break | 46 k |
| moved start | 8.66 M |
| `probeCand(ts)` returned true | 1.93 M |
| `keepSeek(t0)` | 103 k |
| `keepSeek` on t_p | 424 k |
| `keepSeek` in the full scan | 7.9 M |
| `t == ts` continue | 1.39 M |

The only block not executed is the empty-cover guard, which was already unreachable.

### 2. Mutants

For each mutant I ran the default `go test ./internal/fpg`, plus 1,500 runs of my brute force against the default build and 1,500 against the forced build.

| ID | Mutant | go test | bf | bf forced |
| --- | --- | --- | --- | --- |
| K1 | skip t0 intersection | CAUGHT (MixedCover, MixedScaling, Unlimited) | 445 fail | 507 |
| K2 | stage b picks a non-cover neighbour transaction | CAUGHT (MixedCover, ZeroWeightSet, Unlimited) | 568 | 596 |
| K3 | stage a picks a non-cover transaction | CAUGHT | 229 | 228 |
| K4 | `keepSeek` drops the last item | CAUGHT (MixedCover) | 144 | 334 |
| K5 | `seek` gallop `<=` | CAUGHT (TestSeek, SeekItems, Bits, MixedCover) | 268 | 481 |
| K6 | `seek` bisect bound `len−1` | CAUGHT (TestSeek, SeekItems) | equivalent at call sites | – |
| K7 | `keepSeek` skips after a match | CAUGHT | 168 | 256 |
| K11 | `probeCand(ts)` ignores `inSet` | CAUGHT | 238 | 251 |
| W1 / W2 / W4 | PR #6 probe mutants (both probe calls) | CAUGHT (SingleCover, MixedCover, …) | 1148 / 1148 / 945 | same |
| K13 | no probe of t0 (trap H1) | CAUGHT (ChainScaling, MixedScaling, TrapProbe) | perf only | – |
| K14 | stage b off | CAUGHT (MixedCover path counter, MixedScaling) | perf only | – |
| **K10** | the full scan skips the first transaction of **every** cover word when t* ≠ t0 | **missed** | missed in the 1,500-run set; **caught by `manyw`** (1/300 fail; forced 6/300) and by audit `bfs` (125/750 fail) | |
| K8, K9, K12, K15b, K16, K17, K18, K19 | break variants, skip t* instead of t0, `break` instead of `continue`, gallop always, `initCand(t0)`, `setLen+1`, last probe as start, `keepSeek` restarting at 0 | missed | 0 | 0 |

K8, K9, K12, K15b, K16, K17, K18 and K19 are equivalent for the verdict, as the argument above predicts.

K10 also survives the extended suite (`FPG_M5_CASES=10000 FPG_ORACLE_CASES=5000 FPG_M5_UNLIMITED=300 … -run 'TestExtCheck|TestOracle$|TestDeviations|TestSeek'`, rc=0, `K10-extended.txt`) and the `fpgforce` suite (rc=0).

### 3. md5 identity (own datasets, `md5-sample.txt`)

I regenerated the datasets with `go run ./bench/gen`. `lp25000.tab` is byte-identical to the issue's awk reproducer (same md5 `dafef5c5…`). I added my own T4-style chain: 17 lines of 30,000 items plus 30,000 one-item lines.

PR, `main` and forced output are EQUAL on 18 rows:

- `lp25000` with `-As`, `-Ac`, `-At`, `-Ad -l0` and `-n2`;
- `lp50000w -w`;
- `lpc20000` with `-q2` and `-q0`;
- `adv5-20000 -At -q0`, and `-As -n2`;
- the T4 chain with `-At -s-17 -n1` and with `-As -n2`;
- `m5-d2000-c-n3`, and `-At -n2`;
- `sparse-c`, and `-n2`;
- `weighted -n2`;
- `long -n3`.

The sparse, m5-d2000 and lp25000 md5 values match the implementer's `md5-main-vs-branch.txt` where the rows overlap. The output of `lp25000 -n1` is semantically right: 25,000 single items with support 2, which is the closure of each item over {long line, one-item line}.

### 4. Required checks

| Command | Result |
| --- | --- |
| `gofmt -l .` | empty |
| `go vet ./...` | ok |
| `staticcheck ./...` | ok |
| `go test ./...` | rc=0 |
| `go test -race ./...` | rc=0 |
| `go test -tags fpgforce ./internal/fpg` | rc=0 |
| extended suite (env above) | rc=0, `internal/fpg` 182 s, 24 top-level PASS |

Machine load was high and shared: load average 5 at the start, 150–170 during the campaign (`campaign-load.txt`). That does not matter for correctness.

### 5. Docs

- **m5-investigation §4.** The stage descriptions match the code. The exactness paragraph is correct: every cover transaction is intersected and the stages only choose among cover bits. I checked the claim "the first probe cannot regress" against the bag order (`tract.Bag.All`: ascending lexicographic, a proper prefix first). If t0 is the lexicographically smallest cover transaction and t ⊇ X, the first position where they differ holds an item of t0 \ X that is smaller than every item of t \ X. The claim holds, and it only concerns performance.
- **§7.5.** The mutant list is consistent with my results. The two survivors it names are verdict-equivalent, which agrees with K12 and K9 here. The `-tm` pruning remark is correct: if j ∉ t, then supp(X∪j) ≤ supp(X) − w(t) < smin. The claim is limited to `-n1`, as the challenge required.
- CHANGELOG and PROGRESS are accurate as far as correctness goes. I did not re-check the timing numbers; that belongs to the performance critic.

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| C20-1 | MEDIUM | confirmed | The in-repo tests do not catch a verdict-changing mutant of the new t0/t* coupling in the full scan. K10 (`if ts != t0 \|\| i == 0 { x &= x-1 }`, which skips the first transaction of every cover word once the start moved) survives the default `go test ./internal/fpg`, the extended M5 suite and the `fpgforce` suite. The root cause is that no in-repo test has a closed check whose cover spans more than 8 non-zero words with a first-of-word transaction (not one of the spread probes) as the only one that excludes a candidate. `TestExtCheckMixedCover` covers of 1–3-item sets rarely reach 9 words. The shipped code is correct, so this is a test gap, not a defect. | `mutants-result.txt`, `K10-extended.txt`, `K10-forced-tests.txt`; caught by my `manyw` shape (fail-K10*/) and by audit `bfs` (125/750) |
| C20-2 | LOW | confirmed (info) | `TestExtCheckZeroWeightSet` cannot fail through `scanClosed`'s choice of start, because the weight-0 transaction is removed when the checker is built. It guards the build filter, as K2 shows. Its comment ("must not end stage b or give the candidates") overstates what it exercises. | code reading of `newExtChecker` and the test |

No CRITICAL or HIGH findings. I found no counterexample in 110,000 definition-based runs, and the output is byte-identical to `main` in all 106,000 compared runs.

## Fixes requested

1. **C20-1:** add a regression test with a cover spanning at least 16 non-zero words (for example, a hot item in about 1,000 transactions, with fillers so that the scan is chosen). "Almost" items should each be missing from exactly one transaction, and that transaction should be the first cover transaction of a word that is not a spread-probe word. The start must move (t* ≠ t0). Run it at production and forced thresholds and in all row modes, and confirm that mutant K10 fails it. Optionally, also make `TestExtCheckMixedCover` draw some larger covers.
2. **C20-2 (optional):** reword the comment of `TestExtCheckZeroWeightSet` so it says the test guards the build-time weight-0 filter, the precondition of stages a and b.

## Performance impact

Not assessed here; the performance critic covers it.

## Decision

**ACCEPT WITH CONDITIONS.** Only the MEDIUM finding C20-1 and the LOW finding C20-2 remain.

---

## Re-review of c70e83a

- **Head reviewed:** `c70e83aa15d67bc164eff3d40547a62b5f93efcf`. It adds two commits since `bec1681`: `58774a2` (code and tests) and `c70e83a` (docs).
- **Reviewer:** correctness critic (round 2). Light checks ran in the daytime. The brute force and the md5 sample ran in the night window, 21:00–21:19, logged in `ACTIVITY.md`.

### Code changes checked

- **Gallop span rule.** The new rule is `l >= gallopMin && l >= k && (k == 0 || txItems[off+k-1] < cand[last])` with k = gallopRatio·|cand|.
  - It only chooses between `keepSeek` and `keepCand`. Both are exact intersections, so **no gallop rule can change a verdict**.
  - The index is in bounds: when k ≥ 1, `l ≥ k` gives `off+k-1 ≤ off+l-1`.
  - `cand[len-1]` is read only when k > 0, which requires |cand| ≥ 1 because the ratio is > 0. A ratio of 0 is used only in tests and gives k = 0, which short-circuits before the read.
- **Thresholds as build constants, no leak into production.**
  - `extcheck_thresh.go` (`!fpgforce`) sets 128/16/4. `extcheck_force.go` (`fpgforce`) sets 2/1/0.
  - `newExtChecker` copies them per checker via `scanDefaults()`.
  - A grep of the non-test code finds no other assignment to `scanParams`, and no package-level mutable state; the old `var` and `init()` are gone.
  - So test overrides apply only to a checker that a test built, and a release build cannot pick up forced values.
- **New tests.**
  - `TestExtCheckFirstOfWord`: 1,100 cover transactions over 18 words. Each transaction lacks one "almost" item. t0 is made longer, so the start moves. The test asserts the setup (at least 16 words, start moved) and runs at production and forced parameters.
  - `TestExtCheckClusteredCandidates` asserts that the scan actually ran (stage b) and asserts the search decision in both directions.
  - The C20-2 comment now says correctly that the test guards the build-time weight-0 filter.
  - Race and `-short` scaling only reduces the number of seeds and row modes.

### Commands and results

| Check | Result |
| --- | --- |
| `gofmt -l .` | empty |
| `go vet ./...`, `staticcheck ./...` | clean |
| `go test ./...` | rc=0 |
| `go test -tags fpgforce ./internal/fpg` | ok |

Mutants against the default `go test ./internal/fpg -run 'TestExtCheck|TestSeek'` (`round2-mutants.txt`):

| Mutant | Result |
| --- | --- |
| K10: full scan skips the first transaction of every word once the start moved | **CAUGHT** by `TestExtCheckFirstOfWord`. C20-1 resolved. |
| G1: old rule, no span test | CAUGHT (ClusteredCandidates) |
| G4: never gallop | CAUGHT (Clustered, MixedCover, MixedScaling) |
| G5: span compared with the first candidate | CAUGHT (Clustered) |
| G6: span test inverted | CAUGHT (Clustered, MixedScaling) |
| G2: `<=` instead of `<` | missed |
| G3: index `k` instead of `k-1` | missed |

G2 and G3 only move the boundary of a cost decision, so they are performance-only (see C20-3).

Night runs, using my own brute force from the definition, with byte identity against `main` (`ba63d14`):

| Run | Binary | Runs | Failures | Identical to `main` |
| --- | --- | ---: | ---: | ---: |
| mixed shapes, many-words included (seed 3000000) | default | 20,000 | 0 | 20,000 |
| mixed shapes (seed 3500000) | `fpgforce` | 10,000 | 0 | 10,000 |
| many-words only | default | 3,000 | 0 | 3,000 |
| many-words only | `fpgforce` | 2,000 | 0 | 2,000 |

**Total: 35,000 runs, 0 failures, 61.5 M expected sets compared.**

md5 sample, the same 18 rows as round 1 (`round2-md5.txt`): the default and forced outputs of `c70e83a` are EQUAL to the `main` hashes recorded in round 1 on all 18 rows. The baseline was not re-run, per the measurement budget.

### Docs

The m5-investigation §4 (span rule, constants, `fpgforce`) and §7.5 (round-1 table, K10 and `TestExtCheckFirstOfWord`) match the code and my results. In benchmarks.md I checked only the wording; the numbers are for the performance critic.

### Findings, round 2

| ID | Severity | Status | Summary |
| --- | --- | --- | --- |
| C20-1 | MEDIUM | **resolved** | K10 is caught by `TestExtCheckFirstOfWord`. |
| C20-2 | LOW | **resolved** | The comment is reworded. |
| C20-3 | LOW | new (info) | The exact boundary of the span test (`k-1` against `k`, `<` against `<=`) is not pinned by a test. It affects only cost: both intersections are exact, and the shipped index is in bounds. No action required. |

### Decision (round 2)

**ACCEPT.** No Critical, High or Medium findings remain.
