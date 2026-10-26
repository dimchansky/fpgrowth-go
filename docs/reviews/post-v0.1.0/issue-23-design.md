# Design and design challenge for #23

Written before the implementation. The challenge rejected the first design (D1: patch P2 alone leaves the oracle unusable, P2b needed; D2: new defect U13, -ge normalisation with -m > 1); the binding decisions that followed (patched oracle fpgpsp-fixed, upstream defaults with printed seed and -Z, CLI only, optional outfile, both binaries in one archive, -ge compared by a derived tolerance) are recorded in docs/decisions.md D7.

## Design proposal

# Issue #23 (FEAT-1) — fpgpsp: phase 1 findings and design

Branch `feat/23-fpgpsp` from `origin/main` d51eb19; local commit a615e9b
(reference build variant `fpgpsp`, not pushed). Raw notes, scripts and
experiment data: `~/.cache/fpgrowth-cycle/23/{bin,data,scratch}`.
Every "confirmed" below is backed by a script in `bin/` (exp2..exp12).

## 1. Sources, provenance, build (acceptance 1, 2; issue #25)

- All sources are in the SHA-256-pinned archive (`build.sh` pristine
  check: 71 files OK). `fpgpsp` = `fpgpsp.c` (`-DFPGPSP_MAIN`, v1.3
  2024.12.12) + `fpgrowth.c` (no `FPG_MAIN`) + `tars.o` = `tract.c
  -DTA_READ -DTA_SURR` (`tbg_ident/random/swap/shuffle`, `tract.c:2602-2801`)
  + `pspest.o` = `patspec.c -DPSP_REPORT -DPSP_ESTIM` (`psp_tbgest`,
  `simpest`, `cplxest`, `samplelp`, `taslots`, `patspec.c:371-756`) +
  `util/src/random.c` + `math/src/gamma.c` (`logGamma`) + `arrays.c`
  (`dbl_select/shuffle/qsort/reverse`) + `-lpthread`. No new download.
- License: no per-file headers; MIT/Expat via `{util,math,tract,apriori,
  fpgrowth}/doc/mit-license.txt` and `fpgrowth.html`, same author. Nothing
  third-party. `random.c` cites Marsaglia's xorshift ("xorwow") post (algorithm
  reference only, no copied code).
- `reference/build.sh fpgpsp` (committed): release flags + `-ffp-contract=off`,
  make target `fpgpsp`; checks no assertions (nm), usage lists `-g#`,
  smoke runs `-ge -S1`, `-gr -c4 -S1 -Z1`, `-gp -c4 -S1 -Z2`. 3.4 s.
  84 warnings = 77 keyword-macro + 5 unknown-option + 2 GNU variadic (same
  harmless classes as fpgrowth). `reference/README.md` updated (variant,
  binary path, corrected "sources missing" sentence).
- #25 (to do in phase 2, living docs only): `README.md:1231-1235`,
  `docs/feature-matrix.md:76`, `reference/upstream/PROVENANCE.md:40-42`
  (done: `reference/README.md`). Dated notes only (no rewrite) in
  `docs/audit/differences.md` FEAT-1 (l.249-262, 297, 315, 332),
  `docs/audit/independent-audit.md:161-163`, `docs/audit/reviews/pr4-review.md`
  (l.85, 181). `CHANGELOG.md:291` is in the immutable `[0.1.0]` section:
  leave it; the `[Unreleased]` entry states the correction instead.

## 2. Algorithms (for `docs/upstream/fpgpsp.md`)

Options (`fpgpsp.c:577-606`): `-t -m -n -s -A -x -l -i -u` as fpgrowth;
`-g#` e (default)/i/r/p/s; `-c#` count (default 1000; for `-ge` it is the
"equivalent number of surrogates"; `-gi` forces 1); `-e#` dispersion
(0.5); `-z#` samples per size (1000); `-S#` seed (default `time(NULL)`;
parsed with `strtol` into `long`, used as `(unsigned)(seed+n)`); `-Z#`
threads (0 = `sysconf(_SC_NPROCESSORS_ONLN)`); `-D`; `-R -r -f -b -C`.
No `-w`, `-q`, `-v`, no output-format options. Output: psp_report with
scale `1/cnt`: `size supp %g(count/cnt)`; `-D` prints to stdout.

- **Data prep** (`fpg_genpsp:265`): `fpg_data(tabag, FPG_NOPACK [|NORECODE|
  NOSORT for -gs], +2)`: recode by frequency, zmin filter, itsort, sort and
  **reduce** (duplicates merged into weighted transactions) except for -gs.
- **Single path** (`cpus<=1 || cnt<=1`): `rng_create(seed)`, one surrogate
  buffer, loop `surrfn(tabag, rng, tasur)`, `fpg_data(tasur, FPG_SURR)`
  (itsort + sort, no reduce/filter/recode), `fpg_mine`, spectrum accumulates
  in one reporter. **Threaded path**: `c = ceil(cnt/cpus)`; thread n gets
  `min(c, cnt-n*c)` sets while > 0, `rng_create(seed+n)`, its own
  `tbg_clone(tabag)` buffer, miner and reporter; spectra merged by
  `psp_addpsp` into thread 0's in thread order. Confirmed: result is a
  function of (seed, partition) only — `-Z2 -c2 -S7` = `-c1 -S7` + `-c1 -S8`;
  `-c5 -Z3` = `-c5 -Z4` (both 2,2,1); `-c3 -Z3` = `-Z8`; reruns identical;
  `-Z1/-Z2/-Z4` differ (issue's observation reproduced).
- **Surrogates are chains**: the buffer persists and is re-sorted by
  `fpg_data(FPG_SURR)` after each set, so set k depends on set k-1 (swap:
  `2*extent` swaps first, then `extent/2`; random: the `ifrq` permutation
  persists in `dst->buf`). `tbg_ident`: no change. `tbg_random`: per slot
  draw `size` distinct items with prob ∝ item frequency (without replacement),
  then repair over/under-represented items by one cyclic search per
  transaction. `tbg_swap`: random pairs (i≠j), `iswap` exchanges one item
  present only in the larger with one present only in the smaller.
  `tbg_shuffle`: copy source, per-column Fisher-Yates interleaved over rows
  (k descending, columns inside). RNG: `rng_dbl = uint32 * 2^-32`.
- **Estimation** (`fpg_estpsp`): global `rseed(seed)` (seed<=0 → time),
  miner FPG_AUTO/FPG_DEFAULT (ignores -A/-x/-t), `taslots` (slots per size
  via C(i,k)), `alpha<=0` → `simpest` (Poisson, no randomness), else
  `cplxest` with `samplelp` (random k-subsets via `dbl_select(drand)`,
  exact formulas k<=4, 16 permutations otherwise), counts
  `(size_t)(dist*equiv+0.5)`. Confirmed: depends on `-S`, not on `-Z`;
  `-e0` independent of `-S`.
- **Border `-D`** (`fpgpsp.c:725-740`): zmax = largest size with
  `max4sz(zmax) >= min4sz(zmin)`; `border[m] = max(cur_m + 1, border[m+1])`
  for m = zmax..zmin, min 1; prints `b_zmin:...:b_zmax` only if zmax > zmin.

## 3. Upstream defects found (each needs a differences.md row)

| ID | Defect | Evidence |
|---|---|---|
| U1 | Item-frequency cache (`bag->icnts/ifrqs`) is never invalidated between surrogates (`FPG_SURR` skips recode/filter/reduce, the only invalidators): every set is mined with the first set's item frequencies (header supports, elimination, pex). Wrong singleton supports, e.g. constant `1 28`,`1 43` | instrumented build: 297-334 stale events/30 sets (basket, table); patched recount build differs in 105/504 cases (exp6), all -gr/-gp with weighted data; none for -gs or unique data except -gr repair failures |
| U2 | Single-thread `-gr` builds its buffer with `clone()` (weights 1) while threads use `tbg_clone()` (weights kept): `-Z1` mines only the 28 unique of 80 transactions | exp8: `-Z1` singletons 4..12 vs true 9..36 |
| U3 | Surrogates are generated on the reduced bag: swap/random act on unique transactions whose weight sticks to the slot, so weighted item frequencies are not preserved (the point of swap randomization) | code + exp5 (frequencies drift) |
| U4 | `-ge` uses the reduced bag: `smax` = unique count, slots over unique transactions, probabilities `frq/extent_unique` (sum > 1) | exp12: supports capped at 133 (unique) while real pair supports are 176+; only size 1 estimated |
| U5 | `-D` compares with `min4sz(zmin)`, the row-array minimum of the smallest size (depends on `resize` history/BLKSIZE); dense data → empty border. And zmax == zmin prints nothing | exp11: border empty for all 5 runs; `-m2 -n2` and `-tm` print nothing |
| U6 | Usage says outfile optional; `k < 2` → "wrong number of arguments" | exp9 |
| U7 | `-ge` treats `-S<0` as "time"; generation uses negative seeds | code `fpgpsp.c:395` vs `258` |
| U8 | Progress counter `done` incremented by threads without atomics (stderr only) | code `fpgpsp.c:226` |
| U9 | A failing thread setup (`break` with `w[n].err=-1`) is neither joined nor merged into `r`, its resources leak | code `fpgpsp.c:296-322` |
| U10 | `-D` values are absolute counts, but `fpgrowth -F` reads positive values as percent (`setbdr`): must be negated to use | code `fpgrowth.c:2186-2187` |
| U11 | `taslots`: `choose` saturates at SIZE_MAX and the product wraps (transactions > ~60 items) | code `patspec.c:373-387, 480` |
| — | Known F1/M1 (`-At` crash) is reachable via surrogates (bus error seen in exp4) | reference-build.md F1 |
| — | `tbg_swap`: `dst->mode | TA_PACKED` always true (harmless) | `tract.c:2750` |

## 4. Determinism and exactness

Upstream output = f(input, options, seed, partition(cnt, Z)); defaults
(time seed, all CPUs) make it run- and machine-dependent. Go reproduces f
exactly:
- Port `random.c` (RNG struct, `rng_seed/uint/dbl` and the global
  `rseed/urand/drand` as a separate value, no globals) with uint32
  arithmetic; unit tests against sequences printed by a tiny C harness
  (`reference/harness/random/`, seeds 0, 1, 7, 2^31, 2^32-1, -1 truncation).
- Surrogate state = C-like array of transactions (`[]Item` + weight) in the
  exact C order: Bag.All() of the prepared data is upstream's sorted bag
  order (documented in internal/tract); after each surrogate re-apply
  itsort + sort (a correct sort suffices when equal transactions are
  indistinguishable — true with weights 1, see D-fix below).
- Same partition (logical chains n with seed+n), each chain sequential;
  goroutines execute chains, merge in chain order. Results are identical
  for any goroutine count; they depend on `-Z` only through the partition,
  exactly as upstream (an upstream-compatible "streams" parameter).
- Mining per surrogate: build a `tract.Bag` from the array (new constructor
  `tract.NewBag(tracts, nitems, wgt)`), mine with `internal/fpg`; the
  spectrum is order-independent, so only the multiset matters. `-gs` needs
  raw records in file order and no recoding (identity code map in order of
  first appearance; a new ItemBase method).
- Estimation floats: `exp/log` (Go math vs libm) may differ by 1 ulp;
  counts can flip where `dist*equiv+0.5` is within ~1e-12 of an integer.
  Byte-exact expected nearly always; tolerance only per cell |Δcount| <= 1
  when Go's own value lies within 1e-9 (relative) of the rounding boundary
  (Go reports it through a test hook). logGamma: reuse/export the existing
  port (`internal/ruleval/gamma.go`).
- `%g` output: reuse `internal/isrout` appendG (P4 precedent).
- Seed: int64 `strtol` semantics (LP64), `uint32(seed+n)`; on Windows C
  `long` is 32-bit (strtol clamps) — Go uses LP64 behavior everywhere.

**Recommended semantics (maintainer decision, Q1):** fix U1-U5 rather than
replicate them, since they change results materially and U1/U2 make `-Z1`
and `-Z>1` mean different things. All fixes are expressible as a small,
documented patch set to the C sources, so a byte-exact oracle still exists:
- P1 `fpg_genpsp`/`fpg_estpsp`: add `FPG_NOREDUCE` to the initial
  `fpg_data` (surrogates and estimation on all records, weights 1) — fixes
  U2-U4 and makes U1 inert for -gp/-gs. Tried (exp13, instrumented build):
  0 stale events in all 18 runs that had up to 334; `-ge` on dense data
  then estimates sizes 1-7 up to support 324 instead of size 1 capped at 133.
- P2 `fpgrowth.c`: `tbg_ifrqs(fpg->tabag, 1)` (5 sites) — fixes U1 (needed
  for -gr repair failures).
- P3 `-D`: `min4sz(zmin)` → `min4sz(zmax)` (= "row non-empty"), print when
  zmax >= zmin.
These live in `reference/patches/fpgpsp/*.patch` (never applied to the
pristine variants) and build a second variant `fpgpsp-fixed`. Go is
byte-exact against `fpgpsp-fixed` for all methods, and against pristine
`fpgpsp` wherever P1-P3 are provably inert (no equal records after recode
and zmin filter; Go flags U1/U5 triggers through a test hook, those cases
are reported as deviation-verified as in difftest). Alternative (Q1b):
replicate U1-U5 by default (needs C-exact row-min bookkeeping in
`report.Spectrum`, stale-frequency override in the miner, weighted
reduced arrays and C's exact tie order of `tbg_sort`/`ptr_mrgsort`) and
offer fixes behind a flag — more code, less correct; not recommended.

Defaults (Q2): keep upstream's (time seed, all CPUs = `runtime.NumCPU()`,
which may differ from `sysconf` under Linux affinity/cgroups — documented).
Proposal: when `-S` is not given, print the seed used in the log line
("[seed N]") so any run can be reproduced (stderr differs from C only then).

## 5. Architecture (draft docs/decisions.md D7)

- New command `cmd/fpgpsp` (own option parser — `-c -e -z -S -Z -D` mean
  different things than in fpgrowth), reusing `internal/tabread`,
  `internal/tract`, `internal/fpg`, `internal/report`, `internal/isrout`,
  `internal/cconv`.
- New `internal/psp` (name open): `rng.go` (random.c), `surr.go`
  (ident/random/swap/shuffle on an array bag), `estim.go` (taslots, simpest,
  cplxest, samplelp, dbl_select/shuffle), `chain.go` (one sequential chain:
  ctx, seed, count → *report.Spectrum), `border.go` (-D), spectrum merge
  (`Spectrum.Merge`, psp_addpsp). No goroutines in internal packages.
- Goroutines only in `cmd/fpgpsp` (one per chain, at most Z); library
  guarantee D6 unchanged. No public API in #23 (Q3): the chain design makes
  a later sequential `fpgrowth.PatternSpectrum(ctx, src, opts, SurrOpts)`
  easy and its result would not depend on concurrency.
- SIGINT: like `cmd/fpgrowth` (`signal.NotifyContext`, chains check ctx
  between sets and inside mining, exit 130 "interrupted"); upstream fpgpsp
  is built without `FPG_ABORT` and dies by the signal (difference row).
- Progress (`%10ld` + 10 backspaces every 20 sets): byte-identical for -Z1;
  for -Z>1 printed by the coordinator in increasing order (upstream racy).
- Errors/exit codes from fpgpsp's own table (E_SURRCNT 13, E_SURR 14,
  E_NOTABLE 27); keep the "outfile required" check (U6) unless Q4 says fix.

## 6. Release and docs (not changed yet)

- `.goreleaser.yaml`: second build id `fpgpsp` (`./cmd/fpgpsp`, same
  flags), `archives.ids: [fpgrowth, fpgpsp]` (both binaries in one archive,
  name unchanged); comments; `docs/releasing.md` (binaries list, `go version
  -m` check), `release.yml` if it inspects archive contents (it does not
  appear to); CI: `./build.sh fpgpsp` in the reference job.
- Docs: `docs/upstream/fpgpsp.md` (new), `docs/decisions.md` D7,
  `docs/differences.md` (U1-U11 rows + SIGINT, NumCPU, LP64 seed, progress
  order, seed log line), `docs/feature-matrix.md`, README (new section +
  "Not ported" fix), `docs/cli.md` (fpgpsp options, exit codes, `-D` → `-F`
  needs negative values, U10), `CHANGELOG.md [Unreleased]` (Added, and the
  #25 correction), `reference/README.md` (fixed variant, harness).

## 7. Test plan

1. RNG: Go vs C sequences (golden file from the C harness; `rng_uint`,
   `rng_dbl`, `drand` after `rseed`), incl. seed truncation.
2. Surrogate functions: golden surrogates from a debug-only C dump patch
   (prints each surrogate array; never in oracle builds), byte-exact for
   chains of 5 sets per method, several seeds/datasets; invariant tests
   independent of C: swap/shuffle preserve item frequencies and sizes
   exactly, shuffle keeps columns, random preserves sizes and (after
   repair, when possible) frequencies; property/fuzz targets for
   surrogate functions (no panics, invariants).
3. Estimation: golden spectra from C for -e0 and -e>0 with fixed -S over
   synthetic inputs; tolerance rule of §4 only where flagged.
4. difftest `TestFpgpsp` (reference module): random datasets (incl.
   duplicates, tables, empty transactions) × methods × `-Z1..4`, `-c`
   1..50, `-S`, `-t -A -x -l -i -u -m -n -s`, `-R`, `-D`; byte-exact stdout,
   out file and stderr (without timings; -Z1 progress) vs `fpgpsp-fixed`;
   pristine comparisons as in §4; avoid M1/F1 (-At with pex) like
   `avoidDeviations`. Go `-race` run with -Z4.
5. CLI golden tests in `cmd/fpgpsp/testdata` (no C needed), option-parser
   fuzz (`FuzzParseArgs`), error/exit-code table tests, SIGINT test.
6. Performance (night window, budget §IMPLEMENTER): C vs Go, `-gp/-gr -c200
   -Z1` and `-Z4`, `-ge`, instructions retired, 2 runs each.

## 8. Risks

- Per-surrogate cost in Go adds a prefix-tree build from the array (C mines
  the array directly); may cost 10-30 % — measure; optimize later if needed.
- `internal/fpg` with non-recoded codes (-gs) is untested territory; Go's
  extra optimizations may assume frequency order (check, add tests).
- Estimation float ulp differences (bounded, see §4).
- Scope is large (RNG, 4 generators, estimation, CLI, release); recommend
  splitting the PR into reviewed commits: docs/#25 + oracle; RNG +
  surrogates; estimation; cmd + release.

## 9. Open questions for the maintainer

- Q1 Fix U1-U5 with a patched oracle `fpgpsp-fixed` (recommended), or
  replicate upstream exactly (Q1b)? Fix U11 (saturate)?
- Q2 Keep time/all-CPU defaults (recommended, with the seed shown in the log)
  or make defaults reproducible (e.g. `-S1 -Z1`)?
- Q3 CLI only now (recommended) or also a public library API?
- Q4 Keep upstream's mandatory outfile (U6) or accept one argument (stdout)?
- Q5 One release archive with both binaries (recommended) or separate ones?

## Design challenge

# Design challenge — issue #23 (FEAT-1, port fpgpsp)

- Issue: #23 (and #25 doc corrections). No PR yet.
- Reviewed: `~/.cache/fpgrowth-cycle/23/design23.md`, experiments `23/bin/exp2-13`,
  implementer commit `a615e9b` (worktree `.claude/worktrees/agent-a5fa3580b3774d83d`,
  `reference/build.sh fpgpsp`), base `origin/main` d51eb19 (fresh clone in
  `23/challenge/repo`).
- Reviewer: independent critic (FP-growth, surrogate statistics / pattern
  spectra, RNG reproducibility, Go concurrency, CLI). Did not write the design.
- Scope: Q1-Q5 and the five challenge topics. Read-only towards the repo and
  GitHub. All experiments ran in the night window (23:5x-00:3x), each a few
  seconds of CPU except one 86 s campaign (logged in ACTIVITY.md).

## Evidence examined

Upstream: `fpgpsp.c` (whole), `tract.c` TA_SURR (2602-2801), `clone`,
`tbg_clone/copy/count/filter/reduce/sort/pack`, `patspec.c` (resize,
incfrq, addpsp, choose, samplelp, taslots, simpest, cplxest, psp_tbgest,
psp_report), `report.c` (isr_addpex, isr_remove, isr_reset, isr_settarg),
`fpgrowth.c` (fpg_data, fpg_report, setbdr, the 5 `tbg_ifrqs` sites),
`random.c`, `gamma.c`. Go: `internal/tract` (Bag, All order, empties),
`internal/fpg/doc.go` (not-ported data modes), `internal/ruleval/gamma.go`,
`docs/upstream/fpgrowth-core.md` (P1-P5, B8), `docs/differences.md` F-1,
CI matrix, Go 1.27 `math` asm stubs.

## C builds made for this review (`23/challenge/`)

| Dir | Content |
|---|---|
| `pristine/` | copy of the implementer's `reference/build/fpgpsp` |
| `fixed/` | pristine + design P1, P2 (5 sites `tbg_ifrqs(...,1)`), P3 |
| `p1only/` | pristine + P1 (genpsp line only) |
| `fixed-p2b/` | fixed + **P2b**: clear root-level perfect extensions in the reporter after every `fpg_mine` (single path and worker) |
| `fixed-asan/` | fixed-p2b with `-g -O1 -fsanitize=address,undefined` |
| `fixed-p4/` | fixed-p2b + P4 (repair search also examines item j) |

Scripts: `challenge/bin/c1..c9*.sh` (each states its goal), outputs `challenge/c*.out`,
random small datasets `challenge/rand/r1..r150.tab`.

## Commands run (results)

| Command | Result |
|---|---|
| `bin/c1-variant.sh`, `bin/c2-options.sh` | Same seed, -Z1: spectra fall into two classes: `-Ac` (reorder) vs `-Ac -i`/`-As`/`-Ad`/`-At` (no reorder); `-tc`/`-tm` also change the class. Holds for pristine and fixed. |
| `bin/c3-pack.sh` | pristine: packing (`-As` vs `-As -l0`) changes spectra in 81/180 cases, only on data with duplicates (basket, dense, table), never on uniq/uniq2; fixed: 0/180. |
| singleton spectra `-m1 -n1 -c4 -S9` basket | pristine `-gp` singletons differ from `-gi` (swaps do not preserve item frequencies), `-gr -Z1` sums to 94 = unique extent (U2); fixed: identical to `-gi` for -gp and -gr, -Z1 and -Z2. |
| `-D` on dense/basket | pristine dense -gp: `311:247` while the spectrum reaches size 8 (U5); -ge basket pristine: `29:29:29:29:29:29` with singleton supports capped at 28 = unique count (U4); fixed: full monotone borders; `-m2 -n2` prints `21` (pristine prints nothing). |
| `bin/c4-p2.sh` (instr-P1 vs fixed, 240 cases) | 0 stale, 0 diffs on the 23/data sets. |
| `bin/c5-repair.sh` (150 small random datasets × 3 seeds) | instrumented build reports stale frequencies in 403/450 chains; **fixed output differs from instr-P1 in 34 and is non-deterministic** (r1: 3 different md5 in 6 identical runs). |
| ASan on fixed, r1 `-gr -s-1 -c5 -S1 -Z1` | `heap-buffer-overflow ... isr_report report.c:1519` from `rec_cmplx`; a size-4 "pattern" is printed for 3 items. |
| `bin/c6-asan.sh` (1800 runs) | fixed (P1-P3): non-deterministic in 59/1800 double runs; differs from P2b in 371/1800 (all -gr); -gp fixed == P2b 150/150. |
| `bin/c7-asan-only.sh` | P2b ASan build, 900 -gr runs: **0** AddressSanitizer errors (only the benign misaligned `size_t` UB at tract.c:2680/2685). |
| chain decomposition with P2b on r1 | `-Z2 -c4 -S7` == `(-c2 -S7) + (-c2 -S8)`: partition model holds. |
| `-ge -m1` vs `-m2` (fixed) r1, basket, basket2 (`-z20000`, 2 seeds) | r1 `-m2`: pair supports estimated up to 14 though only 5 transactions have ≥2 items; basket: size-2 mean support 6.46 (-m1) vs 6.79 (-m2), 64.5 vs 61.4 patterns; basket2 (no 1-item transactions): unchanged (6.47 vs 6.48). |
| `-gp -m1` vs `-m2`, basket, `-c300`, 2 seeds | size-3 patterns per surrogate 109.0/109.3 (-m1) vs 106.2/106.1 (-m2). |
| `bin/c9-repair-rate.sh` | -gr chains with item-frequency drift on small random data: 671/750 (upstream repair) vs 373/750 (P4); on basket/dense/table/uniq/uniq2 `-c20` × 5 seeds: 0 for both. |
| `-gs -A{c,s,d,t}` table, pristine and fixed-p2b | all 8 identical (P1 inert on -gs, mining variant irrelevant without recode). |
| Go `math` stubs (go1.27.3) | `haveArchLog` true only on amd64/s390x, `haveArchExp` true on amd64/arm64/s390x; CI tests run on amd64, arm64, Windows, 386; difftest on ubuntu (gcc/glibc) and macOS (clang/Apple libm). |

## Findings

| ID | Sev | Status | Summary | Evidence |
|---|---|---|---|---|
| D1 | HIGH | confirmed | **P2 as specified gives an invalid oracle.** Root-level perfect extensions are added to the reporter (`isr_addpex` at `rep->cnt == 0`, fpgrowth.c 759/1218/1386/1647) and never removed (`isr_remove` pops only levels ≥ 1); the reporter is reused for every surrogate. With stale frequencies (pristine) the root-pex set never changes, so it is silently carried over from the first surrogate (part of U1: wrong for -gr whenever an item stops being in all transactions). With P2's fresh counts the set changes, a former pex item is also a tree item, and sets with duplicate items overflow the reporter: heap-buffer-overflow, non-deterministic output, impossible sizes. Fix P2b: clear the root-level pex list after every `fpg_mine` in both loops (5-line patch in `fixed-asan/fpgrowth/src/fpgpsp.c`, marked `P2b`). Go's fresh per-surrogate mining equals P2+P2b. Difftests with small random data (the usual difftest shape) would hit this constantly. | c5, c6, c7; ASan trace; r1 |
| D2 | HIGH | confirmed | **Missing defect U13: `-ge` with `-m` > 1.** `psp_tbgest` uses `ib_getfrq` (item-base counts from reading, including transactions that `tbg_filter` empties for `zmin`) divided by `tbg_extent` after the filter, so probabilities sum to more than 1 and supports are overestimated. P1 does not fix it. `-m2` is the usual setting for pattern spectra. Same class as U4: fix (e.g. use `tbg_ifrqs` of the filtered bag, oracle patch P5) or document as replicated; a maintainer decision either way, plus a differences row and test. | r1/basket/basket2 -ge runs above; patspec.c:744-746 |
| D3 | MEDIUM | confirmed | **The chain depends on the recode order, not only on (seed, partition).** `fpg_data` uses `sort=±1` instead of `±2` when FPG_REORDER is set, which is on for `-Ac -ts` and off for `-i`, `-As/-Ad/-At`, `-tc/-tm` (fpgrowth.c 1832-1836, 1934); item codes differ, so surrogates differ. Go must reproduce both code maps (Recode `order`). The estimation uses FPG_AUTO/FPG_DEFAULT whatever `-A/-i/-t` say. Document ("same seed, other `-A`/`-i`/`-t` → other result") and include these options in the byte-exact difftest. | c1, c2 |
| D4 | MEDIUM | confirmed / hypothesis | **Float exactness plan is incomplete.** (a) FMA: Go fuses on arm64; the new code has fusable sites `probs[z] = (probs[z]-x)*alpha + x` and `y += c*dist[c]` (cplxest, patspec.c:705, 750) — each product needs an explicit `float64()`, pinned by a TestPinnedBits-style test. (b) Go's `math.Log` is assembly on amd64 and pure Go on arm64/386, so Go's own `-ge` output is architecture-dependent: Go-only goldens in `cmd/fpgpsp/testdata` need the same boundary rule or inputs with a verified margin. (c) The fixed 1e-9 relative window is not derived: in the Poisson recurrence `x` starts at `-λ` (λ up to `smax`) and each `x += y` rounds at `ulp(|x|)` (≈1.5e-11 at 1e5, ≈2e-9 at 1e7), so the Go/C gap scales with `smax`; derive the window from that bound or measure it from a C debug dump of unrounded values (`%a`) with a margin. (d) A flip also changes stderr "[N signature(s)]" and, at the tail, the `-D` line under `-ge`; the rule must cover them. (e) The C oracle itself differs between glibc (CI ubuntu) and Apple libm: goldens from one platform are not valid for the other. | Go math stubs; CI matrix; patspec.c:683-713 |
| D5 | MEDIUM | confirmed (simplification) | **`-gs` does not need non-recoded mining.** The spectrum is invariant under item recoding, and only the surrogate *array* (column layout, row order) must be C-exact. Go can mine every surrogate through its normal recoded pipeline; no identity code map / new ItemBase method, and no entry into the FPG_NORECODE territory that `internal/fpg/doc.go` and core B8 mark "not ported / partly broken upstream". The same separation (C-exact array state, any mining representation) holds for all methods. | `-gs -A{c,s,d,t}` identical; core B8 |
| D6 | MEDIUM | design gap | **Logging the seed alone does not make a default run reproducible.** With default `-Z` = NumCPU the partition is machine-dependent; the log line should carry the seed and the effective chain count / `-Z` (e.g. "[seed N, Z k]"). Tests must always pass `-S` and `-Z`. | fpgpsp.c 269-283 |
| D7 | LOW | confirmed | Upstream semantics to document (keep): recoding drops infrequent items and `tbg_filter` empties transactions shorter than `zmin` *before* surrogates, so the null model depends on `-s` and `-m` (-gp size-3 mass 109 vs 106 per surrogate for -m1/-m2). Empty and emptied transactions stay in the array (first in sort order) and consume RNG draws (iswap draws before it finds no partner): the Go array must keep them. | tract.c 2003-2050, 2615-2640 |
| D8 | LOW | confirmed | `tbg_random` repair: the cyclic search never examines index `j = start-1` (`while (i != j ...)`, then `if (i != j)`), so repairs fail needlessly (671→373 of 750 small chains with drift when fixed; 0 on the realistic sets). No RNG impact. Add as U12 (document; optional oracle patch P4, maintainer decision). | c9 |
| D9 | LOW | confirmed | P3 reasoning should be written down: in a `RSUPP=double` build `rows[z].min` is the *observed* minimum, which suggests Borgelt meant "max support at z ≥ smallest observed support at zmin". "Row non-empty" (P3) is the conservative choice: `fpgrowth -F` applies no border beyond `bdrcnt`, so any truncation is anti-conservative. Also state that the border starts at fpgpsp's `-m` and must be used with the same `-m` in fpgrowth (besides U10's sign). | patspec.c 312-315, report.c 833-852, fpgrowth.c 2178-2192 |
| D10 | LOW | confirmed | Further `-ge` details to port/document: sizes are estimated only for `z < n` (a size equal to the item count is never estimated); output values are `%g` of `(1.0/cnt)*frq` (scale multiply, not `frq/cnt`); `(size_t)(v+0.5)` must be `uint64` in Go (386 CI), and `choose` saturates at 2^64-1 on LP64. | patspec.c 649, 683, 710, 823, 373-385 |
| D11 | LOW | confirmed | #25 list is almost complete. Add: `docs/PROGRESS.md:49,103` (status), `internal/tract/doc.go:66-67` ("Not ported: … surrogates") and `internal/fpg/doc.go` "Not ported: FPG_NORECODE…" if the port touches them; `docs/audit/release-v0.1.0.md:57` is historical (leave or dated note). `CHANGELOG.md:291-292` correctly left as is with an `[Unreleased]` correction. | grep over the repo |
| D12 | LOW | hypothesis | Per-surrogate cost: Go would rebuild a hash-indexed trie per surrogate, where C bin-sorts the array. Since the trie's `All()` order is already upstream's sort order, one build can serve both (mine it, then read back the sorted array for the next surrogate), or the array can be sorted directly and fed to the FP-tree builders through a small iterator interface. Measure as planned (night window). Memory is Z × (array + trie + FP-tree), the same order as C's per-thread clones. | bag.go 160-330 |

## Topic answers

1. **Defects.** U2, U3, U4 confirmed by reproduction: reduction makes weights stick to slots,
   so swap randomization no longer preserves item margins (pristine `-gp` singletons ≠ identity),
   which is the defining property of the method; the surrogate functions count `+1` per
   transaction, so they assume weight 1. Deduplication therefore changes the null model;
   P1 is correct and minimal (and makes packing inert, c3). U1 is real but narrower after P1
   (only -gr frequency drift) and broader than stated (stale root-level pex, D1). P2 is not
   correct alone (D1). U5 confirmed; P3 fine (D9). Missing: U13 (D2), U12 (D8).
2. **Bit-exactness.** RNG is integer-only and easy; the chain/partition model holds with P2b
   (decomposition check). The RNG stream does not depend on float values (draw counts are
   fixed by k/n), so float differences cannot cascade into different samples. The equal-
   record argument for sorting is right (weights 1). Remaining risks: D3, D4, D7.
3. **Architecture.** Reuse is sound; no miner change is needed if D5 is adopted. Fresh
   per-surrogate mining in Go is the P2+P2b semantics. Concurrency model (chains,
   merge in chain order, identical for any goroutine count) is correct; spectrum merge
   is order-independent for the output once P3 is used.
4. **Scope.** Deliverable in staged commits: (1) docs/#25 + oracle variants (P1, P2+P2b,
   P3, decisions on P4/P5) + C debug dump patch; (2) RNG + array state + -gi/-gr/-gp/-gs +
   difftest; (3) -ge estimation + tolerance machinery; (4) cmd, release, docs. Minimal
   complete feature = all five methods, -D, -Z partition semantics, SIGINT.
5. **#25 list.** See D11.

## Fixes requested before implementation

- Replace P2 by P2 + P2b in the oracle and in the U1 description (D1).
- Decide U13 (D2), U12 (D8); add rows/tests accordingly.
- Write the recode-order dependence, prefilter semantics and empty-transaction handling
  into `docs/upstream/fpgpsp.md` and the test plan (D3, D7).
- Rework the tolerance rule (derivation or measurement, FMA guards, cross-arch Go goldens,
  stderr and -D coverage, per-platform C goldens) (D4).
- Mine -gs with the normal recoded pipeline (D5); log seed and chain count (D6).

## Q1-Q5

- Q1: agree to fix and use a patched oracle, **with P2b added** and U13 decided; keep the
  `-s/-m` prefilter semantics (document).
- Q2: agree, but log seed **and** effective `-Z`/chain count.
- Q3: agree (CLI only, goroutines only in `cmd/fpgpsp`).
- Q4: agree to accept one argument (stdout) as a documented extension; `-` already works upstream.
- Q5: agree.

## Decision

REJECT as is (2 HIGH): revise the design for D1 and D2, then proceed; the rest are
conditions for the implementation and docs.
