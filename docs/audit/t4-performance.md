# T4 performance investigation (fpgrowth-go, main @ 5c8b3d3)

Input: 17 x `0 1 ... n-1`, then n+10 x `x`. Command `fpgrowth -s-17 -n1 t4_n.tab out`.
After reduction the bag has 2 transactions: the long one (weight 17) and `{x}`.
The FP-tree is one chain of n items plus a separate `x` node.

## 1. Timings (Go CLI vs C reference, C run with `ulimit -s 65520`)

Default target (-ts), default algorithm, writing phase:

| n      | Go user | C user | sets   | outputs equal |
|--------|---------|--------|--------|---------------|
| 12,500 | 0.54 s  | 0.47 s | 12,501 | yes           |
| 25,000 | 2.08 s  | 1.89 s | 25,001 | yes           |
| 50,000 | 8.73 s  | 7.54 s | 50,001 | yes           |
| 100,000| 34.9 s  | 31.2 s | 100,001| yes           |

When n doubles, the time goes up about 4x, so the cost is Theta(n^2) in both
implementations. Go's constant is 1.1x C's. The reported ~72 s (and the 57 s
wall time I measured) is wall-clock time on a loaded machine. User CPU is 35 s.
With the default 8 MB stack the C binary still segfaults (exit 139) at
n=100k while sorting (M10). With a 64 MB stack it completes.

Variants at n=25k (Go user / C user; every output equals C's):
-As 2.31/1.96, -Ac 2.22/1.98, -Ad 2.30/1.96, -l0 2.22/1.99, -x 2.09/1.87,
-q0 2.24/1.94, -tg 0.93/0.51 (both quadratic: 0.23->3.36 / 0.13->1.89 for
12.5k->50k), -tc/-tm with s/c/d about 0.07/0.04, -At (frequent) 0.07/0.04,
**-At -tc 1.37/0.03**. The public API (`MineItemsets`, MaxSize 1,
Count(17)) gives the same numbers: Auto/Complex 2.1 s, TopDown 0.02 s,
TopDown+Closed 1.37 s, Complex+Closed 0.02 s at 25k.

## 2. Finding A: frequent target, s/c/d algorithms. Inherited from upstream (not port-specific)

pprof (25k, CLI harness) puts 96% of the time in `fpg.(*miner).recCmplx`:
- complex.go:378-379, chain branch: for every item, the loop walks all of its
  ancestors and calls `rep.AddPex` (26%, plus 9% for the walk itself).
- `report.(*Reporter).Remove` report.go:417-420 unmarks every pex (31%).
- `Report` report.go:610-612 fills supps/wgts for n pexs, and `expand`
  report.go:686-695 visits every pex, rejecting each one against zmax (26%).

Each item at depth d costs O(d), so the total is sum d = n^2/2. Upstream
`rec_cmplx` (fpgrowth.c:1155-1160, chain case calling isr_addpex), `isr_report`
(report.c, the loop that fills supps[cnt+k]), `report()` (while n>0 loop)
and `isr_remove` (the pex unmark loop) do exactly the same work. The port
is faithful. The only cause is the chain/perfect-extension handling: zmax=1
makes every pex useless, yet each one is still collected, expanded and
removed. No item-name formatting or slicing/copying cost appears in the
profile.

Optional improvement beyond upstream, with identical output: skip
collecting perfect extensions when they cannot affect any result, i.e.
`rep.cnt >= zmax` and there is no closed/maximal filter (clomax == nil),
no NOEXPAND mode (there, all pexs together decide whether the set is
reported), and the target is not generators (AddPex already ignores pexs
for generators). In recCmplx (and the matching chain/pex sites in
simple.go / single.go / topdown.go) guard the ancestor loop with a new
`rep.PexUseful()`. Report/expand/Remove then see n=0 pexs, so this input
runs in O(n) total (O(1) per item). It is not needed for parity.

## 3. Finding B: -At -tc with -n. Port-specific, caused by deliberate fix M5

Timings for `-At -tc -n1`: 12.5k 0.36 s / C 0.01 s; 50k 5.40 s / C 0.10 s;
100k 21.4 s / C 0.46 s. Go is quadratic here and C is not.
pprof: 94% in `fpg.(*extChecker).initCand` (extcheck.go:532-534), called
from `scanClosed` from `recTopDown -> rep.Report`. Temporary
instrumentation at n=12.5k: 12,501 scanClosed calls that copy 156,250,001
transaction items in total (n checks x the n-item transaction). The same
counters for -Ac -tc: 1 call, 1 item. -At -tm makes no checks.
Cause: the M5 complete extension check (docs/differences.md M5) runs for
each size-1 candidate that the repository accepts. Its cover is the single
long transaction, the cost model picks the horizontal scan (h ~ n/2 <
v = n), and initCand copies the whole transaction into the candidate list
before it can conclude "not closed". Upstream does no such check (it has
bug B5 instead), so it is fast.

Smallest sound fix (in `scanClosed`, or `decide`, using len(set) recorded
by markSet):
- If the cover has exactly one transaction t, then X is closed iff
  txLen(t) == |X|. Every item of X, including the pexs, is in t (weight-0
  transactions are excluded) and the items are distinct. This is O(1).
- More robust and still small: before initCand, probe up to P (e.g. 8)
  items of the first cover transaction that are not in X with the
  vertical subset test (cover ⊆ bits(j)). Any hit proves "not closed" in
  O(P·|nz|). For n=1 covers, use the exact length test above.
Expected: -At -tc -n1 on T4 becomes O(n) total (O(1) per check). Worst-case
complexity elsewhere is unchanged, and the result is identical because both
tests are exact. Needs a regression test, e.g. a T4-like bag in
extcheck_test.go that asserts the work is bounded or uses
TestExtCheckOracle parity.

## 4. Severity for v0.1.0

- Finding A (default -ts quadratic): **Low**. Upstream has the same
  complexity, Go is within 1.1x of C, the result is correct, and the input
  is pathological. Document it in the M10/T4 notes and, optionally, apply
  the PexUseful guard.
- Finding B (-At -tc/-tm with -n, quadratic, 46x slower than C at 100k):
  **Medium**. It is a port-specific asymptotic regression, introduced by
  the deliberate M5 correctness fix. The output is correct and the fix is
  small and exact. It only affects non-default -At combined with closed
  target and a size limit, so it does not block the release.
- -tg constant factor (1.8x C, same complexity): **Info**.

## Artifacts
- Clone: scratchpad/inv/t4 (clean apart from bin/; instrumentation reverted)
- Inputs/outputs: scratchpad/inv/data; profiles: scratchpad/inv/prof
- Harness tests (moved out of the clone): scratchpad/inv/harness/*_zz_inv_test.go
