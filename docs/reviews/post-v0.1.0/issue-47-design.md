# Design — issue #47 (fpgpsp `-ge`: sampling speed)

- Written by the implementer on 2026-10-28, on Apple M1 Max (arm64),
  Go 1.27.3, main at fde9e41. Raw data, scripts and the activity log:
  `~/.cache/fpgrowth-cycle/47/` (`ge.sh`, `ge.txt`, `ge-summary.txt`,
  `dt-400-s*.log`, `ACTIVITY.md`). Data: the shapes of the #23
  performance review (`~/.cache/fpgrowth-cycle/23/review-perf/data`:
  A 4,000 x 40 dense, B 20,000 sparse transactions over 1,000 items,
  C a 5,000 x 10 table).
- Starting point: the performance review of #46
  (`issue-39-review-performance.md`, section 2), which profiled `-ge`
  and prototyped items 1-3 below
  (`~/.cache/fpgrowth-cycle/39/critic-perf/r2/ge-proto.diff`).

## Where the time went (from that review; B, `-z20000`)

`samplelp` 87 %: `dblSelect` 49 % of it (the swap line 40 % of that),
`RNG.Uint32` not inlined with its state stored back on every draw
(10 %), `slices.Sort` of the k selected probabilities (26 %),
`permProb` (8 %); `math.Log(float64(c))` of the support loop about 4 %.

## Changes (all in `internal/psp/estim.go`)

1. **`dblSelect`** copies the five words of the generator state into
   local variables, runs `RNG.Uint32`'s xorshift recurrence on them and
   writes the state back once at the end. The index is computed as
   before, `float64(u) * rnorm * float64(len(a))` (drand()·n, left to
   right, the product (u·2^-32) exact), truncated and clamped to
   `len(a)-1`; the swap reslices the array instead of an offset. The
   number of draws is unchanged.
2. **`sortAsc`**, an insertion sort, replaces `slices.Sort` on the k
   selected probabilities (k is at most the largest transaction size,
   and the call happens only for k >= 5). It uses `cmp.Less`'s order
   for floats (NaN first), so the sorted sequence equals
   `slices.Sort`'s: a strict weak order has one sorted sequence up to
   the order of equivalent elements, and equivalent elements here are
   equal doubles (interchangeable bit for bit, except +0/-0, which
   probabilities never are: `itemProbs` computes x·f >= 0 or
   round(a·b) + x, whose zero is +0) or NaNs. The prototype's plain
   `v < s[j-1]` would have differed from `slices.Sort` only for NaN
   (an infinite `-e`); the NaN-aware comparison removes that case
   instead of arguing it away.
3. **`log(cnts[z])`** is computed once per size instead of once per
   sample, and `log(c)` of the support loop comes from `logTable`,
   grown on demand up to the largest support reached
   (`math.Log(float64(c))`, so the same arguments and values).
4. **No physical reversal** (the issue's untested item 4). Upstream
   computes the probability of a permutation, reverses the array
   (`dbl_reverse`), computes the probability of the reversal and
   shuffles the reversed array for the next permutation. Invariant of
   the port: before iteration i the Go array equals upstream's array
   for even i and its reversal for odd i. So for odd i the shuffle runs
   mirrored (`dblSelectBack`: the same random numbers and indices,
   counted from the back, `a[last-i]` swapped with `a[last]`), and the
   two probabilities are added in the opposite order (upstream's
   "forward" is the Go array's backward). `permProbs` computes both
   products in one loop; they are independent chains, each with
   upstream's operations in upstream's order (`t *= n*v/r; r -= v`).
   After 8 reversals (an even number) the arrays agree again, so the
   order `samplelp` leaves for the next sample is upstream's.

The closed forms for k <= 4 moved unchanged into `smallSampleLP` and
the permutation average into `largeSampleLP`, so the tests can call the
closed forms from the reference implementation.

Not changed: `RNG.Uint32` itself (still not inlined; the other methods
call it a few times per surrogate, not in a tight loop), `math.Exp` in
the support loop, the 8 permutations.

## Why the output cannot change

Every value that reaches the output is computed from the same inputs by
the same floating-point operations in the same order: the generator
produces the same sequence (same recurrence, state written back), the
swap indices are the same (same expression, same clamp), the sorted
array is the same (point 2), the array order after every call is the
same (point 4), the sums `p += ...` add the same terms in the same
order, and the logarithms get the same arguments. None of the changed
lines contains a product that feeds an addition, so no FMA contraction
can appear (`mulAdd` is unchanged where it was used).

## Tests (`internal/psp/sample_test.go`)

The pre-#47 functions are kept in the test file as references
(`dblSelectRef`, `permProbRef`, `samplelpRef`: one `RNG.Float64` per
draw, `slices.Sort`, `slices.Reverse`).

- `TestDblSelect_matchesReference`: every n <= 70 and k in [0, n+2];
  the array bits and the generator state after the call; also
  `dblSelectBack(a) == reverse(dblSelectRef(reverse(a)))`.
- `TestDblSelect_maxDraw`: a constructed generator state whose next
  output is 2^32-1, for n up to 2^20: the index truncates to n-1 in all
  three functions (the clamp is upstream's guard against drand() == 1).
- `TestSortAsc_matchesSlicesSort`: 20,000 random slices with
  duplicates, 0, ±Inf, NaN, -1 and the smallest subnormal, bit for bit.
- `TestLogTable`: increasing and random supports up to 3,000, bit for
  bit against `math.Log`, and no allocation for a covered support.
- `TestSamplelp_matchesReference`: 400 chains of 30 samples each on a
  shared array and generator (n <= 60, k in [0, n+1], both the closed
  forms and the permutation average): result bits, array order and
  generator state after every call.

Mutation check (by hand, not committed): adding the odd-iteration
probabilities in the forward order, or shuffling from the front in odd
iterations, makes `TestSamplelp_matchesReference` fail.

The existing `TestEstimate_matchesC` (values before rounding, against
the oracle's trace) and the CLI goldens pass unchanged.

## Differential check (light, daytime)

`reference/difftest` `TestFpgpsp`, GOMAXPROCS=2, `-parallel 1`, under an
RSS watchdog, 400 cases each:

| Seed | Cases | Estimations | Byte-equal | Tolerated | Failed |
|---|---|---|---|---|---|
| 1 | 400 | 119 | 400 | 0 | 0 |
| 2 | 400 | 127 | 400 | 0 | 0 |
| 3 | 400 | 125 | 400 | 0 | 0 |

## Performance (instructions retired, `/usr/bin/time -l`, mean of 3 interleaved runs)

C is the upstream oracle (`~/.cache/fpgrowth-cycle/39/baseline/fpgpsp-c`);
main is fde9e41. All -ge outputs md5-equal across C, main and the PR.

| Row | C | main | PR | PR vs main | main / C | PR / C |
|---|---|---|---|---|---|---|
| B `-ge -s-20 -S1` | 0.797G | 1.407G | 1.096G | -22.1 % | 1.77 | 1.38 |
| B `-ge -s-20 -S1 -z1` | 0.071G | 0.298G | 0.298G | 0.0 % | 4.20 | 4.20 |
| A `-ge -s-60 -S1` | 0.208G | 0.376G | 0.235G | -37.5 % | 1.80 | 1.13 |
| C `-ge -s-100 -S1` | 0.190G | 0.386G | 0.231G | -40.2 % | 2.04 | 1.22 |
| B `-ge -s-20 -S7 -e0.9` | 0.799G | 1.401G | 1.092G | -22.1 % | 1.75 | 1.37 |
| A `-gp -s-60 -S1 -n20` | 16.40G | 40.11G | 40.17G | +0.2 % | 2.45 | 2.45 |
| B `-gr -s-20 -S1 -n20` | (CPU limit) | 288.2G | 288.1G | 0.0 % | - | - |

The prototype of the review gave -18.8 %, -35 % and -39 % on B, A and
C; the implicit reversal (point 4) adds about 3 points on B and 2-3 on
A. `-z1` samples once per size, so its instructions are the fixed cost
outside the sampling (reading, `taslots`, start-up), which this issue
does not change. The `-gp` and `-gr` rows do not run this code; they
are within noise (the C `-gr` runs hit the 60 s CPU limit of the script
and are not a comparison). Load: `uptime` 5.2 / 8.3 / 22.0 at the
start of the round (spindump and mds_stores the top consumers);
instruction counts do not depend on it.

## Deferred to the night window

- The full `TestFpgpsp` sweep (seeds 1 and 2 x 3,000, seed 3 x 20,000,
  as the review ran on the prototype) and the default
  `(cd reference && go test ./...)`.
- A profile of B `-ge -z20000` to see what remains of the 1.38x.
