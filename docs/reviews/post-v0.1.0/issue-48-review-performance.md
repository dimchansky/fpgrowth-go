# Critic review (performance) — PR #50, issue #48

- PR: https://github.com/dimchansky/fpgrowth-go/pull/50 (branch `perf/48-prep-pipeline`)
- Head reviewed: 8e118af (against main fde9e41 and v0.1.0)
- Decision: ACCEPT WITH FINDINGS (three Low)
- Recorded by the orchestrator from the critic's final message.

## Resolution of findings

- P1 (peak RSS up to about 2.2–2.4 times the budget): fixed in ede1046
  and 9fb84ff instead of only documenting it. The record sizes are
  stored inline in the identifier blocks, which are never copied (a
  test checks every block's capacity; leaving the inline size out of
  the fit test fails it); the weights, which still grow by append,
  count every array they allocate.
- P2 (`fpgpsp` peak RSS grows by the retained size): fixed in ede1046,
  as the critic proposed: both release paths (within and over the
  budget) collect at once (`runtime.GC`).
- P3 (the over-budget reread is still 3.1 times C): not changed in this
  PR; the second pass still converts names to strings. It is 29 % fewer
  instructions than main and is offered to the maintainer as a possible
  follow-up (`BuildBag` over byte slices).

Focused check by the orchestrator (ede1046 vs 2b844cc, the PR head
before the fixes, and main 95f2794 for `fpgpsp`; GOMAXPROCS=2, one
process at a time under the RSS watchdog; 3 rounds, all md5-equal;
instructions within 1 %, max RSS in MB):

| Row | before | after | main |
|---|---:|---:|---:|
| `fpgrowth -s50`, 15 million one-item records (73 MB) | 242 | 121 | — |
| `fpgrowth -s50` B×30 | 30.6 | 24.4 | — |
| `fpgrowth -s50` B×300 (over budget) | 165 | 135 | — |
| `fpgpsp -ge -z1 -S1` B×90 | 135 | 92 | 92 |
| `fpgpsp -ge -z1 -S1` B×200 | 273 | 201 | — |
| `fpgpsp -ge -z1 -S1` B×300 (over budget) | 290 | 290 | 290 |

The before column of the over-budget `fpgpsp` row is 290–294 MB; a
first version of the fix that collected only after building the bag
peaked at 332 MB there, which is why the over-budget release collects
too. D8, `docs/benchmarks.md`, the CHANGELOG and the README give the new
figures.

## Critic's report

**Verdict: ACCEPT WITH FINDINGS.** All findings are Low. Nothing
regressed.

- Reviewed: PR head 8e118af against main fde9e41 (origin/main at review
  time) and v0.1.0. C reference built in the critic's clone with
  `reference/build.sh release fpgpsp-fixed`.
- Apple M1 Max, Go 1.27.3, 21:28–21:48, GOMAXPROCS=2, one process at a
  time, each run under the RSS watchdog with `ulimit -t 120`; no run was
  killed or timed out. Load average 2.1–4.3.
- Each cell is the median of 3 interleaved rounds (C / v0.1.0 / main /
  PR). Conclusions rest on instructions retired; user time and max RSS
  from `/usr/bin/time -l`.
- md5: PR output equals main and C on every row (the d2000 row and the
  one-item rows produce empty output for all builds).

### Rows dominated by reading and preparation

Instructions in G (PR/C in brackets), RSS in MB.

| Row | C | v0.1.0 | main | PR (×C) | user s C/main/PR | RSS C / main / PR |
|---|---:|---:|---:|---:|---|---|
| fpgrowth -s50 B | 0.063 | 0.236 | 0.237 | 0.097 (1.55) | 0.00/0.01/0.00 | 3.4 / 5.7 / 6.1 |
| fpgrowth -s50 B×30 (16 MB) | 1.430 | 6.309 | 6.309 | 2.192 (1.53) | 0.14/0.44/0.16 | 41.8 / 5.7 / 30.4 |
| fpgrowth -s50 B×90 (49 MB) | 4.254 | 18.876 | 18.886 | 6.517 (1.53) | 0.42/1.36/0.49 | 118.5 / 5.8 / 75.7 |
| fpgrowth -s50 B×30 from stdin | 1.431 | 6.358 | 6.360 | 2.239 (1.56) | 0.14/0.44/0.16 | 41.8 / 5.7 / 30.5 |
| fpgrowth -s50 sparse-large | 0.701 | 3.396 | 3.396 | 1.178 (1.68) | 0.08/0.27/0.10 | 20.3 / 12.8 / 19.9 |
| fpgpsp -ge -z1 B | 0.072 | – | 0.298 | 0.156 (2.17) | | 3.3 / 11.5 / 11.7 |
| fpgpsp -ge -z1 B×30 | 1.713 | – | 7.088 | 2.875 (1.68) | 0.21/0.54/0.25 | 41.8 / 45.1 / 49.3 |
| fpgpsp -ge -z1 B×90 | 5.024 | – | 21.123 | 8.789 (1.75) | 0.67/1.64/0.76 | 118.7 / 113.4 / **164.5** |
| fpgpsp -ge -z1 B×30 from stdin | 1.712 | – | 7.133 | 2.926 (1.71) | 0.21/0.54/0.25 | 41.8 / 45.0 / 49.5 |

The claim "3-4× C to 1.5× C" holds for fpgrowth (4.4× to 1.53×); for
fpgpsp it becomes 1.7×. The figures match the PR's `docs/benchmarks.md`
table to within 1–2 %.

### Mining rows (regression check)

Instructions in G, main → PR; md5 equal on every row.

| Row | Main → PR | Change |
|---|---|---:|
| fpgrowth dense -s5 | 5.910 → 5.349 | −9 % |
| fpgrowth dense -tc -s5 | 5.976 → 5.418 | −9 % |
| fpgrowth dense -tm -s5 | 5.034 → 4.470 | −11 % |
| fpgrowth sparse -s0.1 | 0.506 → 0.316 | −38 % |
| fpgrowth sparse -tm -s0.1 | 0.512 → 0.323 | −37 % |
| fpgrowth d2000 -tm -s2 -n3 | 0.452 → 0.404 | −11 % |
| fpgrowth sparse-large -s0.05 | 4.767 → 2.454 | −49 % |
| fpgrowth B -s1 | 0.285 → 0.145 | −49 % |
| fpgpsp B -gr | 3.160 → 3.019 | −4 % |
| fpgpsp B -gp | 5.558 → 5.417 | −3 % |
| fpgpsp C -gs | 3.397 → 3.371 | −1 % |
| fpgpsp A -gp -c64 | 2.618 → 2.589 | −1 % |
| fpgpsp B -ge | 1.403 → 1.262 | −10 % |

No row got slower in instructions or user time. RSS on these rows rises
by 0–4 MB (dense -tc 43.5 → 47.6 MB). v0.1.0 equals main on every row.

### Large inputs (B×200 just below the 128 MiB budget, B×360 above it)

| Row | C | main | PR (×C) | user s C/main/PR | RSS C / main / PR |
|---|---:|---:|---:|---|---|
| fpgrowth -s50 B×200 (108 MB, below budget) | 9.473 | 41.912 | 14.419 (1.52) | 0.95/2.92/1.08 | 259 / 5.6 / 141 |
| fpgpsp -ge -z1 B×200 | 11.092 | 46.943 | 18.827 (1.70) | 1.56/3.62/1.65 | 259 / 239 / **342** |
| fpgrowth -s50 B×360 (195 MB, over budget, reread) | 16.977 | 75.403 | 53.450 (3.15) | 1.72/5.28/4.12 | 464 / 5.8 / 165 |
| fpgpsp -ge -z1 B×360 | 19.991 | 84.411 | 61.878 (3.10) | 2.91/6.48/5.24 | 519 / 421 / **524** |

The reread path does not regress (29 % fewer instructions than main:
tokenizer and the single lookup in pass 1). Peak RSS is bounded and at
or below C in every measured case.

### Findings

| ID | Severity | Summary |
|---|---|---|
| P1 | Low | Peak RSS can reach about 2.2–2.4 times the budget. 15M one-item records (58 MB file, ~120 MB retained): PR 279.5 MB (main 4.8 MB, C 577 MB); 20M records (over budget): 279 and 317 MB. Cause: `lens` grows by `append`, its old arrays are garbage the budget does not count, and the heap goal is about twice the live heap; D8's "blocks never copied" covers the identifiers only. Fix: document, or keep `lens`/`wgts` in blocks or the length inline. |
| P2 | Low | `fpgpsp` peak RSS grows by the full retained size although the records are released (B×90 113 → 164 MB, B×200 239 → 342 MB, B×360 421 → 524 MB); undocumented. Cause: the released records are not collected before the bag and the `-ge` structures are allocated. Releasing blocks as they are consumed made no difference; `runtime.GC()` after the release lowered B×90 to 137 MB at no measurable instruction cost. Fix: call `runtime.GC()` once after the release; document the `fpgpsp` RSS. |
| P3 | Low (opportunity) | Over the budget the reread is still 3.1 times C: pass 2 uses `Scan`, which converts every name to a string. Fix: a byte-slice `BuildBag` variant (follow-up). Not a regression. |

`FuzzTokenizerReference` ran 180 s with `-parallel 1`, GOMAXPROCS=2:
197,468 executions, 40 new interesting inputs, no difference.
