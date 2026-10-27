# Design — issue #42 (maximal checks with a small maximum size)

- Written by the implementer on 2026-10-27 (night window). The fix is
  local (two loop rewrites and one factor in the build rule) and was
  implemented directly. Measurements: docs/m5-investigation.md §7.7.

## Root cause (measured)

Two independent costs, both added by #19 (ba63d14):

1. **Register spills in the scan loop** (tier, split; part of dense).
   #19 added the work counter `work += l` to `scanMaximal`'s
   transaction loop. With `touched = append(touched, it)` in the item
   loop (a call path: `growslice`), the Go register allocator then
   keeps `len(touched)` in a different register around the loop back
   edge. The item loop gets 2-3 extra moves and a jump per item visited
   (assembly of 3c0b7e8 against ba63d14 and main: in-set path 12 -> 15
   instructions). Tier visits 360 M items: +1.2 G instructions
   (+4.8 %). The tier row builds no rows, which matches the #34
   implementer's observation. Main (after #34) has the same loop shape.
2. **Sibling rows that do not pay on vertical checks** (dense). The
   build rule compares the children's actual work with the build cost
   B in one unit. In process (sampled checks of real runs): a build
   visit costs 1.4-1.9 ns (about 39 instructions), a scan visit
   1.4-1.6 ns (the #19 calibration, right for scans), but a vertical
   word test (`vertMaximal`) only 0.58-0.63 ns (about 15
   instructions). Every child of `dense` is checked vertically, so the
   rule over-valued the saving by about 2.5x: dense -n3 built 435
   levels for 7.7 children each, dense -n4 1,049 levels for 3.2. With
   the rows off, dense -n3 took 4.30 G instead of 4.70 G, -n4 8.76 G
   instead of 9.78 G. On d20000 (the #19 target) 99 % of the level work
   is scan visits and a level serves 70 children.
3. Smaller, not changed: without builds, the rule's pass over the
   lengths of a parent's cover (B) runs 15,417 times on tier and always
   fails the B/2 test: +0.40 G (+1.9 %) against rows off.

## Fix

1. `scanMaximal` and `sibBuild`: `touched` has room for every item
   (allocated lazily) and is written by index, without append; the hot
   fields are locals; `sibBuild` clears all rows up front (B already
   counts them) instead of a `clear` call per new row. The item loops
   have no call and keep their state in registers. Same verdicts and
   the same work counts.
2. `vertMaximal` counts its word tests apart (`vwork`, also per level),
   and `sibWorth` scales the saving by
   f = 1 - (1 - sibWordCost)·vwork/work with sibWordCost = 0.4 (the
   measured ratio, 0.36-0.42). f <= 1, so the rule never builds where
   main did not; on scan levels f is 1. The B/2 condition is unchanged,
   but the smaller gain can delay a build: a level that still builds
   does so at the same or a later child (TestExtCheckSiblingBound's
   pinned child moved from 16 to 31).

sibWordCost tried at 1.0 / 0.5 / 0.4 / 0.33 (instructions, minimum of
2 runs, against v0.1.0): dense -n3 +4.4 / +0.3 / -0.8 / -2.0 %, -n4
+2.6 / -5.3 / -5.9 / -6.7 %, d5000 -20.2 / -23.2 / -23.8 / -24.0 %.

The value 0.4 is a calibration of one machine (Apple M1 Max, arm64,
Go 1.27.3); the instruction ratio, about 15/39 ≈ 0.38, is the more
portable figure. The performance review measured the tolerance: with
0.25 / 0.4 / 0.6 / 1.0, dense -n3 is -2.9 / -1.0 / +1.3 / +4.4 % and
-n4 -6.7 / -5.9 / -4.5 / +2.6 % against v0.1.0; tier, split, large,
d20000 and weighted move by at most ±0.3 %. So any value from 0.25 to
0.6 keeps every row below main and within about 2 % of v0.1.0; only a
value near 1.0 brings dense back above v0.1.0 (still below main).

## Correctness

No verdict depends on the changes: (1) rewrites the same loops (touched
holds each item at most once, so room for every item suffices); (2)
changes only when rows are built. Evidence: the sibling-row oracle and
brute-force tests, the FPG_M5_CASES=20000 oracle and the deferral
property test (30,000 bags) in the default and fpgforce builds, the
reference difftest, and the md5 of 31 outputs against main. The new
test `TestExtCheckSiblingWordCost` fails on main (rows built with 16
later children) and passes; `TestExtCheckSiblingBound`'s pinned build
child (ascending, code bound) moves from 16 to 31. Ten mutants of the
new logic: nine are killed; one (the build's touched list not cut to
its length) is equivalent, because the stale entries have support 0.
