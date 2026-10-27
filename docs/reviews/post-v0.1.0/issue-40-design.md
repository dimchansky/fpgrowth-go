# Design record for #40 (P-9): FisherInfo underflow shortcut

Base: origin/main c66d052. Finding P-9 of the #16 performance review
(`issue-16-review-performance.md`, re-review of 59bcede, §3): for strongly
associated tables at n ≳ 10^15, `fetInfoLarge` decides the near-ties of
its bisection with `cmpXLogX` (big.Float self-powers), at 31 µs to 0.61 ms
and 542 to 11,369 allocations per evaluation, although every such p-value
is 0.

Decision: an underflow shortcut, as `probUnderflows` for FisherProb (#16,
P-6), with its own bound. The big-float path is unchanged (option 2 of the
issue, reusing big.Float values, was not needed: after the shortcut, no
evaluation of the issue's scan reaches `cmpXLogX`).

```go
// in fetInfoLarge, before the bisection
ss := infoNats(supp, body, head, base) // normalized table
if ss > (6·ln(n+1) + 800)·(1 + 2^-40) { return 0, nil }
```

## Notation

Normalized table (`fisherTable`): margins b <= h, rest r >= 0, n = b + h + r
(n = base); table x in [0, b] has cells x, b−x, h−x, r+x. u = 2^-53.
λ = ln(n+1) <= 63·ln 2 < 43.67. E = b·h/n is the expected support.

- F(x) = Σ c·ln c over the four cells of table x (0·ln 0 = 0).
- S(x) = n·I(x) = Σ c·ln(c·n/(row·col)), the information in nats times n
  (`infoNats`). With fixed margins, S(x) − S(y) = F(x) − F(y).
- P(x) is the hypergeometric probability of table x.
- s is the observed table, ss the computed S(s). R is the set of tables
  that `infoSum` (the region sum, the code without the shortcut) sums.

On the real interval, F'(x) = ln(x·(r+x)/((b−x)·(h−x))) is negative below
E, zero at E and positive above, and F''(x) = 1/x + 1/(b−x) + 1/(h−x) +
1/(r+x) >= 4/n (every cell is at most n). So S is strictly convex with
its minimum 0 at E. It decreases on [0, E] and increases on [E, b].

## Assumptions (established in #16 and reviewed there)

- A1: `infoNats` has a relative error below 59u.
- A2: `lnP` has an absolute error below lnPErr(S) = 2^-46·(S+4).
- A3: `cmpXLogX` returns the exact sign, except that it returns 0 (tie)
  when the self-power products differ by a relative amount below 2^-65000.
  Then |F(x) − F(s)| < 2^-64999.
- A4: Go's `math.Exp(x)` is +0 for x < −750. The pure-Go and arm64 code
  return 0 below −745.1332. The amd64 code scales a value fr·2^k with
  k = −1075 or less, which is below 2^-1075 and rounds to 0. `math.Log`
  and `math.Exp` are accurate to 1 ulp.

A1 is used with a wide margin: the cutoff tolerates any relative error of
ss up to 2^-42.

## Lemma 1 (entropy bound): P(x) <= (n+1)·e^-S(x) and P(x) >= e^-S(x)/(n+1)^2

P(x) = C(b, x)·C(n−b, h−x)/C(n, h). For 0 <= k <= m, the term
C(m, k)·p^k·(1−p)^(m−k) of the binomial expansion of 1 with p = k/m gives
C(m, k) <= e^(m·H(k/m)), with H the entropy in nats. That term is also
the largest of the m+1 terms (k is a mode of Bin(m, k/m)), so
C(m, k) >= e^(m·H(k/m))/(m+1). Then

    b·H(x/b) + (n−b)·H((h−x)/(n−b)) − n·H(h/n) = −n·(H(Y) − H(Y|X)) = −S(x),

so P(x) <= (n+1)·e^-S(x). Also P(x) >= e^-S(x)/((b+1)(n−b+1)) >=
e^-S(x)/(n+1)^2. ∎

Corollary: the mode m0 has P(m0) >= 1/(b+1), so S(m0) <= ln((n+1)(b+1)) <= 2λ.

## Lemma 2 (the region): every x in R has S(x) >= S(s) − δ, δ = 2^-64999

`infoSum` (code unchanged; formerly the body of `fetInfoLarge`):

- If s = E exactly (D = 0), every table is summed. Then S(s) = ss = 0 and
  the shortcut never fires.
- If s < E, R = [0, s] ∪ [c, b], with c = firstTrue(s+1, b, pred).
  If s > E, R = [0, a] ∪ [s, b], with a = lastTrue(0, s−1, pred).
- pred(x) = infoAtLeast(x, ss).

On s's own side, x <= s < E (or x >= s > E) gives S(x) >= S(s), because S
is monotone on each side of E.

On the other side (case s < E; the case s > E is symmetric):

- (i) If the range [c, b] is not empty, pred(c) was evaluated and was true.
  Bisection sets hi = mid − 1 only after pred(mid) is true, and at the
  end c = lo = hi + 1.
- (ii) pred(x) true implies S(x) >= S(s) − δ:
  - The float branch fires when sx > fl(ss + fl(2^-45·fl(sx + ss))). With
    A1, S(x)·(1+59u) >= sx and ss >= S(s)·(1−59u). The tolerance is at
    least 2^-45·(1−3u)·(sx+ss), and 2^-45 = 256u > 2·59u + 3u. So
    S(x) > S(s).
  - Otherwise `cmpXLogX` >= 0, which means F(x) >= F(s), or a tie with
    |F(x) − F(s)| < δ (A3).
- (iii) c > E. Assume s+1 <= c <= E. F is decreasing on [s, E], so
  F(c) <= F(s+1). The Taylor expansion at s+1, with F'(s+1) <= 0 and
  F'' >= 4/n, gives F(s) >= F(s+1) − F'(s+1) + 2/n >= F(s+1) + 2/n.
  (F is C² on the open interval where all cells are positive and
  continuous at its ends.) So S(s) − S(c) >= 2/n >= 2^-62 > δ, which
  contradicts (ii).
- (iv) For x >= c > E, S(x) >= S(c) >= S(s) − δ. ∎

The same holds for the mathematical region {x : S(x) >= S(s)}.

## Theorem 1: under the cutoff, the exact p-value of R is below e^-750 and rounds to 0

The computed right-hand side
fl(fl(6·fl(log(fl(fl(n)+1)))) + 800)·(1+2^-40) has an absolute error
below 3e-13 before the last factor and a relative error of u in it. So it
is at least (6λ + 800)·(1+2^-41). By A1,
S(s) >= ss/(1+59u) > (6λ+800)·(1+2^-41)/(1+59u) > 6λ + 800.

By Lemmas 1 and 2,

    Σ_{x∈R} P(x) <= (b+1)·(n+1)·e^(δ − S(s)) < e^(2λ + δ − 6λ − 800) < e^-800 < 2^-1076.

So the correctly rounded value of the exact p-value, of R and of the
mathematical region alike, is +0. That is the value the shortcut returns. ∎

## Theorem 2: under the cutoff and S(s) <= 1.88·10^16, `infoSum` returns +0

So the shortcut is bit-identical to the code it skips. Fix one summed
range [lo, hi] and its walk (`sumRange`), with x0 = clamp(mode, lo, hi).

1. x0 is not the mode, and S(x) >= S(x0) >= S(s) − δ for every x in the
   range.
   - x0 is in R, so S(x0) >= S(s) − δ > 6λ + 799 > 2λ >= S(mode)
     (Corollary). So x0 ≠ mode.
   - If mode < lo, then x0 = lo >= mode+1 > (b+1)(h+1)/(n+2) > E. The
     last inequality holds because n·(b+h+1) − 2bh >= n > 0. S increases
     on the range.
   - If mode > hi, then x0 = hi <= mode − 1 <= (b+1)(h+1)/(n+2) − 1 <= E,
     and S decreases towards x0.
2. Only the walk away from the mode runs. For x0 = lo the downward loop
   does not start (x > lo is false), and every upward ratio
   P(x+1)/P(x) <= 1 for x >= mode. That holds iff
   x >= (b+1)(h+1)/(n+2) − 1. For x0 = hi it is symmetric.
3. lnA = lp(x0) <= λ − (1−2^-46)·S(x0) + 2^-44 < −5λ − 798 (Lemma 1 and
   A2). So exp(lnA) < 2^-1022, and `fisherSum` takes its log-domain branch
   exp(lnA + ln q).
4. Every term p of the walk is at most V = e^Λ·(1+2^-43), with
   Λ = 3λ + 2^-45·S(x0) + 2^-42:
   - An anchor term is exp(fl(lp(x) − lnA)). By Lemma 1,
     lnP(x) − lnP(x0) <= 3λ − (S(x) − S(x0)). With A2,
     lp(x) − lnA <= 3λ − (1−2^-46)(S(x) − S(x0)) + 2^-45·S(x0) + 2^-43,
     and S(x) >= S(x0) (step 1).
   - Between anchors, p is the product of at most 63 computed ratios.
     Each has a relative error of at most 8u (7 roundings), the true
     ratios are <= 1, and each product rounds once. The factor
     (1+10u)^63 <= 1 + 2^-43.
5. q <= 2^60·V, whatever the number of terms. The Neumaier sum keeps a
   running sum t and a compensation c. If t >= 2^55·V, a term v <= V is
   below half an ulp of t, so t does not change. Hence t <= 2^56·V. Each
   compensation increment is an exact rounding error <= u·t, and the same
   argument bounds |c| <= 2^56·u·2^56·V < 2^59·V. Then
   q = fl(t + c) < 2^60·V.
6. No overflow: ln q <= 60·ln 2 + Λ < 41.6 + 131.1 + 2^-45·S(x0) < 709.7
   when S(x0) <= 1.889·10^16. S(x0) <= S(s) + 4λ + 4. For x0 = s this is
   trivial. For x0 = c, pred(c−1) was false (or c−1 = s), so
   S(c−1) <= S(s), and one step changes F by at most 4·(ln n + 1). So
   S(s) <= 1.88·10^16 suffices.
7. fl(lnA + fl(ln q)) <= λ − (1−2^-46)·S(x0) + 41.6 + 3λ + 2^-45·S(x0) + 1e-11.
   The right side decreases in S(x0). At S(x0) = 6λ + 799 it is
   −2λ − 757.4 < −750. So the range contributes exp(·) = +0 (A4).
8. `fisherSum` adds +0 for every range and returns clampUnit(+0) = +0,
   the bits the shortcut returns. ∎

Beyond S(s) = 1.88·10^16, which needs n >= 2.7·10^16 because S <= n·ln 2,
step 6 cannot exclude an anchor overflow from A2 alone. A2 allows an
error of 2^-46·S, up to 9·10^4 nats at n = 2^63. That would turn an
anchor into +Inf, and `infoSum` would return 1. The identity there rests
on the actual lnP error, which is much smaller than A2's bound. The scans
below show that infoSum returns 0 on every such table tried. Any other
value would violate the numeric contract (relative 2^-36 plus 2^-1000
against an exact value below 2^-1076), so the shortcut cannot make a
result worse than the contract.

The cutoff is about 6λ + 800 (979 nats at n = 10^13, 1062 at 2^63). The
p-value underflows already from about S(s) ≈ 745 + O(λ). Tables in
between take the unchanged region sum. They are cheap, because their
tolerance 2^-45·2S is below 10^-10 and almost never needs cmpXLogX.

## Cancellation

The shortcut returns before any abort check, like `probUnderflows`. An
evaluation that the region sum would have aborted (after 4096 steps) now
completes with 0. This is the same documented behavior as FisherProb.

## Evidence (2026-10-27, Apple M1 Max, go1.27.3, load 1.6 to 4.4)

**Issue scan.** FisherInfo with b = n/2, h = n/2+1, s = f·n, for
f in {0.05, 0.1, 0.2, 0.24}. Measured with `testing.Benchmark`, per
evaluation:

| n | origin/main | this change |
| --- | --- | --- |
| 10^13, 10^14 | 2.5–6.4 µs, 0 allocs | 0.13 µs, 0 allocs |
| 10^15 | 6.6–90 µs, 0–1,639 allocs | 0.14 µs, 0 allocs |
| 10^16 | 70–230 µs, 1,195–4,341 allocs | 0.14 µs, 0 allocs |
| 10^17 | 213–310 µs, 3,923–5,863 allocs | 0.14 µs, 0 allocs |
| 10^18 | 337–469 µs, 6,206–8,773 allocs | 0.14 µs, 0 allocs |
| 9·10^18 | 458–608 µs, 8,661–11,369 allocs | 0.13 µs, 0 allocs |

All values are 0 on both.

**Boundary scan** (scratch test, not committed). Four margin families:
b = n/2 with h = n/2+1; b = n/10 with h = n/3; b = 10^6 with h = n/2; and
b = n/2 with h = n − n/1000. Eight totals from 10^13 to 9·10^18. Both
sides of E. S(supp) at 0.5, 0.9, 0.999, 0.999999, 1.000001, 1.001, 1.1,
2 and 100 times the cutoff. That gives 576 tables in all:

- 336 tables fired the shortcut. Each one equals `infoSum` bit for bit,
  and none was skipped by the step budget.
- 240 did not fire. Of these, 64 have positive values.

**Random scan.** 20,000 random tables with log-uniform n in
[10^13, 9·10^18], random margins and random, near-cutoff or moderately
deviating supports:

- 11,368 fired tables equal `infoSum` bit for bit. 1,152 of them lie
  beyond S = 1.88·10^16, outside the range of Theorem 2.
- 6,304 fired tables exceeded the 2^20-step budget of the comparison
  and were not compared.
- There were 0 mismatches.

**Committed tests:**

- `TestFisherInfoUnderflow`: the issue scan plus a positive association.
  It checks for 0 allocations and the value of the region sum. With the
  shortcut disabled it fails with 542 to 11,369 allocations.
- `TestFisherInfoUnderflowCutoff`: 40 tables compared against the exact
  oracle in three bands of S(supp)/cutoff. 17 fired and 14 have positive
  values.

**Ordinary path.** `BenchmarkEval`-style cases at n = 1000 (FisherInfo,
FisherProb, FisherChi2, FisherSupport), 150 and 10^4. 8 interleaved
rounds of main, PR and v0.1.0:

- FisherInfo n=1000: 358 → 370 ns (+3.3 %, ±7 % spread; median
  358 → 362). Instructions retired over 3·10^6 evaluations of each of
  the three FisherInfo cases: 192.15 G → 192.36 G (+0.11 %). That is
  about +0.8 % wall time for the large path, from the extra comparison
  and the split into `infoSum`. Note (2026-10-27, performance review):
  that instruction figure is diluted by the n = 150 and n = 10^4 cases.
  Measured per case at c56eca9, FisherInfo n=1000 is +0.9 % wall
  (359 → 362 ns) and +0.5 % instructions (6511 → 6544 per evaluation);
  the CLI (`-tr -s5 -c30 -em -d100`, d5000x60d40) is +0.13 %
  instructions (51.57e9 → 51.64e9). The follow-up commit moves the
  `ss > 800` test to the call site, which saves part of that.
- FisherInfo n=10^4: 465 → 468 ns (~, p = 0.07).
- Every other case: ~. Every case: 0 allocations.
- Against v0.1.0: FisherInfo n=1000 is 3.80 µs → 0.37 µs. That speedup
  comes from #16.

Raw files are in `~/.cache/fpgrowth-cycle/40/` on the implementer's
machine.
