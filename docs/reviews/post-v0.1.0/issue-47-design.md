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
   number of draws is unchanged. The clamp never fires with this
   generator: u <= 2^32-1, and (1-2^-32)·n rounds to a value below n,
   so it truncates to at most n-1. It is kept as a defensive copy of
   upstream's guard (`if (i > --n) i = n`) against a drand() that
   returns 1.
2. **`sortAsc`**, an insertion sort, replaces `slices.Sort` on the k
   selected probabilities (k is at most the largest transaction size,
   and the call happens only for k >= 5). It uses `cmp.Less`'s order
   for floats (NaN first). A strict weak order has one sorted sequence
   up to the order of equivalent elements, and the two sorts may order
   equivalent elements differently (pdqsort is not stable). So the
   results are bit-identical exactly when equivalent elements are
   bit-identical. That is not true for every input: +0 and -0 are
   equivalent, and so are NaNs with different payloads. A probe by the
   correctness critic found a difference at n = 46 for a mix of ±0, and
   at n = 33 for NaNs 0x7ff8... and 0xfff8.... Neither can occur here.
   The sorted values come from `itemProbs`. A zero there is x·f with
   x > 0 and f >= 0, or round(a·b) + x with x > 0, and both give +0,
   never -0. A NaN there only comes from one operation,
   mulAdd(0, ±Inf, x) with an infinite `-e`, so all NaNs of a run carry
   the platform's one default payload. So `sortAsc` and `slices.Sort`
   give the same bits for every input `itemProbs` can produce.
   Insertion sort is quadratic, so above 256 elements (`sortAscMax`)
   `sortAsc` calls `slices.Sort` itself (see "Long transactions"
   below). The
   prototype's plain `v < s[j-1]` would also have differed on NaNs in
   any position; the NaN-aware comparison removes that case.
   For comparison, main's `slices.Sort` and upstream's `dbl_qsort`
   already ordered equal keys differently (different unstable
   algorithms). That never showed in the output, for the same reason:
   equal keys from `itemProbs` are bit-identical.
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
  three functions. The test pins the largest draw only. It cannot detect
  a broken clamp, because no draw reaches the clamp (point 1).
- `TestSortAsc_matchesSlicesSort`: 20,000 random slices (every 50th
  longer than `sortAscMax`, so both branches run) with
  duplicates, +0, ±Inf, NaN (one payload), -1 and the smallest
  subnormal, compared bit for bit. This is the input domain of the claim
  in point 2, so the test passes by the argument and not by chance.
  -0 and NaNs with different payloads are deliberately left out.
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

### Long transactions (performance review, P49-1)

The data sets above have transactions of at most 18 / 55 / 10 items,
and the sorted k is bounded by that. The performance critic generated
synthetic sets with longer transactions (L: k <= 200, `-z1000`; XL:
k <= 600, `-z100`; XXL: k <= 1,500, `-z10`) and measured the plain
insertion sort ("PR", head 8f441be) against main and against the
cutoff at 256 that the branch now uses ("cutoff"). Instructions
retired, median of 3; all Go outputs md5-equal to main:

| Row | C | main | PR | cutoff | cutoff vs main |
|---|---|---|---|---|---|
| L, k <= 200 | 9.72G | 15.40G | 14.59G | 14.60G | -5 % |
| XL, k <= 600 | 8.76G | 14.14G | 22.69G | 11.57G | -18 % |
| XXL, k <= 1,500 | 5.52G | 8.83G | 27.37G | 6.87G | -22 % |
| B `-ge -z5000` | 3.69G | 5.85G | 4.28G | 4.28G | -27 % |
| A `-ge -z5000` | 0.95G | 1.58G | 0.88G | 0.88G | -44 % |

Without the cutoff, `-ge` regressed against main on XL (+60 %) and
XXL (+210 %). C differs from Go on XXL only; that is the documented
U11 deviation (slot counts saturate in Go, wrap in C), not this
change.

The "cutoff" column is the critic's probe build. Check of the committed
code (main fde9e41 vs this branch, freshly generated sets of the same
shapes: XL 300 transactions of 400-600 items, `-ge -s-60 -S1 -z100`;
XXL 100 transactions of 1,200-1,500 items, `-ge -s-40 -S1 -z10`; 2 runs
each, md5-equal): XL 6.78G -> 5.94G (-12 %), XXL 3.91G -> 3.12G (-20 %).

## Deferred to the night window

- The full `TestFpgpsp` sweep (seeds 1 and 2 x 3,000, seed 3 x 20,000,
  as the review ran on the prototype) and the default
  `(cd reference && go test ./...)`.
- A profile of B `-ge -z20000` to see what remains of the 1.38x.
