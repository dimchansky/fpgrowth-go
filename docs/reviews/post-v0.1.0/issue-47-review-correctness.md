# Critic review (correctness) — PR #49, issue #47

- PR: https://github.com/dimchansky/fpgrowth-go/pull/49 (branch `perf/47-ge-sampling`)
- Head reviewed: 59d9c12
- Decision: ACCEPT (two Low documentation and test points)
- Recorded by the orchestrator from the critic's final message.

## Resolution of findings

- L1: fixed in 8f441be. The design record and the `sortAsc` comment no
  longer claim bit equality with `slices.Sort` for all inputs; they name
  the domain (`itemProbs` yields no -0 and NaNs of one payload) and the
  test keeps to it.
- L2: fixed in 8f441be. The design record says the clamp never fires
  with this generator and is kept as a defensive copy of upstream's
  guard; `TestDblSelect_maxDraw` is described as pinning the largest
  draw only.

## Critic's report

**Verdict: ACCEPT.** Correctness critic, PR #49 (issue #47), head 59d9c12. I found no defect. The only findings are two Low documentation and test points, and neither changes the output.

**Byte-identity: verified.** I checked it by reading the code against upstream `samplelp` in `reference/upstream` and the pre-PR code, then by tests and differential runs.

- **RNG and swap indices:** the inlined xorshift in `dblSelect` and `dblSelectBack` is exactly `RNG.Uint32`. The index is `float64(u)*rnorm*float64(len(a))`, which matches the old `Float64()*float64(n)` in value and in association order. The remaining length `len(a)` equals the old decremented `n`. The clamp is the same (old `i>n-1 → n-1`, new `i>=len → len-1`), the iteration count is the same (k-1), and the state is written back.
- **`dblSelectBack` and the swapped addition order (proof):**
  - At even i the Go array equals upstream's. At odd i it is upstream's array reversed. Mirroring the index j → L-1-j turns upstream's swap `a[off+i]↔a[off]` into `a[last-i]↔a[last]` on the shrinking prefix, with the same draws.
  - At odd i, `fwd` = permProb(rev U) is upstream's second term and `bwd` = permProb(U) its first, so `p+=bwd; p+=fwd` keeps upstream's summation order.
  - Each loop step flips the parity once (one reversal upstream, none in Go). After i=7 both arrays are rev(U7') = upstream's final array, which matters because `probs` carries into the next sample.
  - `permProbs`' backward product does the same operations, in the same order, as `permProb` on the reversed slice.
- **`math.Log` arguments and operation order:** the hoisted `log(cnts[z])` has the same argument. `logTable` returns `math.Log(float64(c))`. No new `x*y+z` pattern is introduced, so arm64 FMA fusion cannot change any result.
- **`logTable`:** it is a local variable in each `cplxest` call (not in `Workspace`), so there is no sharing or concurrency. It grows only up to the largest c reached, which is at most `smax` (`dist` already has that size). Lookups return the stored value directly, so there is no aliasing issue.

| ID | Sev | Status | Summary | Evidence |
|---|---|---|---|---|
| L1 | LOW | Confirmed, unreachable | `sortAsc` is not bit-equal to `slices.Sort` "for all inputs". It differs for mixed +0/-0, or for NaNs with different payloads. The design doc covers ±0 but says equivalent NaNs are interchangeable bit for bit, which holds only for equal payloads. Neither case can happen: `itemProbs` never yields -0, and its NaNs all come from one operation (fma(0,±inf,x)), so they share one payload. Main's `slices.Sort` and C's `dbl_qsort` already differed on equal keys. | Probe over 200k random slices: first mismatch at n=46 (±0) and n=33 (NaN 0x7ff8 vs 0xfff8). No mismatch for positive values with duplicates. |
| L2 | LOW | Confirmed | The clamp is dead code: (1-2^-32)·n is always below n by many ULPs, so truncation never reaches n. `TestDblSelect_maxDraw` therefore pins index n-1 but cannot catch a broken clamp, and mutants M3/M4 survive as equivalent. The C code has the same defensive clamp. No change needed beyond perhaps a comment. | `mut.txt` |

**Mutation run:** 11 of 13 mutants are killed. The survivors are M3/M4 (`>=`↔`>` in the clamps), equivalent per L2. Killed mutants:
- the even and odd summation orders (by `TestSamplelp_matchesReference`);
- the NaN rule in `less` (by `TestSortAsc_matchesSlicesSort`);
- an off-by-one in the log-table growth, the backward-product index, the missing state write-back in `dblSelectBack`, `dblSelect`/`dblSelectBack` swapped by parity, the `lcnt` argument, a `logc` shift, the index using n-1, and a descending sort (the run log records `TestEstimate_matchesC` as a failing test for most of these; the backward-index mutant failed with no test named).

**Commands run** (light, `GOMAXPROCS=2`):
- `gofmt -l .`, `go vet ./...`, `staticcheck ./...`: clean.
- `go test ./...` at the root: pass. `go test -race ./internal/psp/`: pass.
- `critic-correctness/mut.sh`: unit-test mutations, results above.
- `watchdog.sh go test ./difftest -run 'TestFpgpsp$' -parallel 1` with seeds 4 and 5 × 300 cases, the C oracle rebuilt with `DIFFTEST_REBUILD=1`:
  - seed 4: 300/300 byte-equal (97 estimations);
  - seed 5: 300/300 byte-equal (111 estimations);
  - no watchdog trips.
- `critic-correctness/ge-md5.sh`: C (39/baseline/fpgpsp-c) vs PR on 9 new `-ge` rows of A/B/C. They vary the seed and use `-e` 0, 0.5, 1.7, -0.5 and inf, plus `-n7`, `-m5`/`-m6` and `-z200..2000`. All 9 are md5-equal; 6 of them have non-empty output.

**Housekeeping:**
- Disk had 109 GB free before and after.
- The PR binary is deleted, the clone is clean and detached at 59d9c12, and no processes are left running.
- Steps are logged in `~/.cache/fpgrowth-cycle/47/ACTIVITY.md` under `[critic-correctness]`. Logs and scripts are in `~/.cache/fpgrowth-cycle/47/critic-correctness/`.

**Deferred to night (not my job, already scheduled):**
- the full `TestFpgpsp` sweep (seeds 1 and 2 × 3,000, seed 3 × 20,000);
- the default `(cd reference && go test ./...)`.

I did not measure performance; that is the performance critic's job.
