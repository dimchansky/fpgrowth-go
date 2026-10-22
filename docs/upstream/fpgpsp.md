# Upstream audit: fpgpsp (pattern spectra from surrogate data)

Audit notes on upstream's second program, `fpgpsp` 1.3 (2024.12.12),
"generate or estimate a pattern spectrum (FP-growth)", and the modules it
links. Written for issue #23 (port to `cmd/fpgpsp` and `internal/psp`).
Locations are files of `reference/upstream/fpgrowth-6.21/`. Defects are
numbered U1-U20; the port fixes them (rows of the same numbers in
[../differences.md](../differences.md)) and the differential oracle is
upstream with the patches of `reference/patches/fpgpsp/`
(`reference/build.sh fpgpsp-fixed`, `reference/README.md`).

## 1. Sources and build

All sources are in the vendored archive (pristine check: 71 files). The
makefile target `fpgpsp` (`fpgrowth/src/makefile:57-72`) links:

| Object | Source and defines | Contents |
| --- | --- | --- |
| `pspmain.o` | `fpgrowth/src/fpgpsp.c` with `-DFPGPSP_MAIN` | `main`, `fpg_genpsp`, `fpg_estpsp` |
| `fpgrowth.o` | `fpgrowth/src/fpgrowth.c` (no `FPG_MAIN`) | the miner (`fpg_create`, `fpg_data`, `fpg_report`, `fpg_mine`) |
| `tars.o` | `tract/src/tract.c` with `-DTA_READ -DTA_SURR` | reading, and the surrogate generators `tbg_ident`, `tbg_random`, `tbg_swap`, `tbg_shuffle` (`tract.c:2602-2801`) |
| `pspest.o` | `tract/src/patspec.c` with `-DPSP_REPORT -DPSP_ESTIM` | pattern spectrum, `psp_report`, the estimation `psp_tbgest` (`patspec.c:371-756`) |
| `random.o` | `util/src/random.c` | the random number generator |
| others | as for `fpgrowth` | `gamma.c` (`logGamma`), `arrays.c` (`dbl_select`, `dbl_qsort`), reporter, `clomax.c`, `fim16.c`, `istree.c`, ... |

It is linked with `-lpthread`. Earlier documentation of this project
said that `tars.c` and `pspest.c` were missing from the archive; they are
object names, not sources (issue #25). License: MIT/Expat like every
module of the archive (`*/doc/mit-license.txt`), same author; no
third-party code.

## 2. Options and main (`fpgpsp.c:470-748`)

`-t -m -n -s -A -x -l -i -u` as in fpgrowth (`-i` is fpgrowth's `-j`, no
reordering; `-t` without a letter gives target 0, an error), `-g#`
method (`e` estimation, default; `i` identity; `r` transaction
randomization; `p` pair swaps; `s` column shuffle of a table), `-c#`
number of surrogates (default 1000; for `-ge` the number of surrogates
the estimate is equivalent to; `-gi` forces 1), `-e#` probability
dispersion (0.5), `-z#` samples per size (1000), `-S#` seed (`strtol`
into `long`; default and 0: `time(NULL)`), `-Z#` threads (`int`; <= 0:
`sysconf(_SC_NPROCESSORS_ONLN)`), `-D` decision border, `-R# -r# -f# -b#
-C#`. No `-w`, `-q`, `-v` or output-format options; transactions are read
without weights (`tbg_read(..., 0)`).

Validation order: pending string option (7), fewer than two file names
(8, although the usage calls the output optional: U6), `-m` and `-n`
negative (10), `-s` above 100 (11, printed with `%d` for a double: U14),
`-c` <= 0 (13), target (9), variant (12, not 16 as in fpgrowth), method
(14). After reading: no items or no transactions (15), `-gs` on data that
are not table-derived (27). Messages and exit codes follow `GENERROR`
like fpgrowth's. The log is fpgrowth's plus `estimating`/`generating
pattern spectrum ... `, a progress counter (`%10ld` and ten backspaces
for every 20th data set, `repfn`) and `[N signature(s)]`. The spectrum
is written with `psp_report(psp, twr, 1.0/cnt)`: lines `size support
value`, value `%g` of `scale*(double)count`, i.e. the average count per
surrogate. `-D` prints its line on standard output afterwards.

## 3. Generation (`fpg_genpsp`, `fpgpsp.c:234-380`)

1. **Data preparation.** `fpg_create` with the options (maximum support
   100%), then `fpg_data(tabag, FPG_NOPACK [| FPG_NORECODE|FPG_NOSORT
   for -gs], +2)`: recode (drop infrequent items, sort by frequency with
   `sort` = +2 or its sign when `FPG_REORDER` survives `fpg_create`), the
   `-m` filter (`tbg_filter`: shorter transactions become empty), sort the
   items and the transactions, and **reduce** (merge equal transactions).
   The item order, and so the surrogates, depend on `-A`, `-i` and `-t`
   (D3: `FPG_REORDER` is on only for `-Ac` with frequent sets or
   generators). The prefilter (`-s`, `-m`) is part of the null model
   (D7): surrogates are made from the recoded, filtered transactions, and
   empty or emptied transactions stay and take part (for example in pair
   swaps, where they consume random numbers).
2. **Partition.** With `cpus > 1 && cnt > 1`, `c = ceil(cnt/cpus)` data
   sets per thread; thread `n` gets `min(c, cnt-n*c)` while positive, its
   own miner, a clone of the bag, `rng_create((unsigned)(seed+n))` and a
   reporter with a pattern spectrum; otherwise one loop with
   `rng_create(seed)`. The spectra of the threads are added to thread 0's
   in thread order (`psp_addpsp`). The result is a function of the seed
   and this partition: `-Z2 -c2 -S7` equals the sum of `-c1 -S7` and
   `-c1 -S8`, and `-c5 -Z3` equals `-c5 -Z4` (both 2+2+1).
3. **Chains.** Each thread keeps its surrogate buffer: `tasur =
   surrfn(tabag, rng, tasur)`, `fpg_data(tasur, FPG_SURR)` (sorts items
   and transactions, packs for the 16-items machine; no recoding,
   filtering or reduction), `fpg_mine`. Pair swaps continue from the
   previous surrogate; transaction randomization keeps its permuted item
   buffer (`dst->buf`); both see the transaction order of the previous
   surrogate after sorting.

### Surrogate generators (`tract.c`, `TA_SURR`)

- `tbg_ident` (2645): the data (a clone).
- `tbg_random` (2655): for every slot (from the last), draw its size of
  distinct items with probability proportional to the item frequencies
  (without replacement; the item buffer is permuted and the permutation
  persists), then repair: for every transaction (from the last) while
  items are over-represented, replace the last over-represented item by
  an under-represented item not in the transaction, found by a cyclic
  search from a random start. The search never examines the item where it
  stops (U12, patch P4).
- `tbg_swap` (2739): `2·extent` random pairs on the first call, `extent/2`
  later; `iswap` exchanges a random item of the larger transaction that
  is not in the smaller with a random item of the smaller that is not in
  the larger (random numbers are drawn even if the second step finds no
  item). `if (dst->mode | TA_PACKED)` is always true (harmless unpack).
- `tbg_shuffle` (2776): copy the source, then for rows `k` from the last
  down to 1 and every column, exchange with a random row `j <= k`.
  Requires table-derived data (`tbg_istab`: all transactions of one size,
  every item always in the same column; false for at most one
  transaction).

### Random numbers (`util/src/random.c`)

`rng_seed` fills five 32-bit registers with `69069*s + 362437` and
discards five outputs; `rng_uint` is Marsaglia's xorshift with an
odd-multiplier output step; `rng_dbl = rng_uint * 2^-32`. The global
generator (`rseed`, `urand`, `drand`) of the estimation is the same
algorithm. All of it is integer arithmetic and exact products, so it is
reproduced bit for bit (`internal/psp` `TestRNG_matchesC`).

## 4. Estimation (`fpg_estpsp`, `patspec.c:371-756`)

`fpg_estpsp` seeds the global generator (`seed <= 0`: time, U7), prepares
the data with `FPG_AUTO` (the complex variant) and `FPG_DEFAULT`, whatever
`-A -x -l -i -u` say (the target still selects the item order), and calls
`psp_tbgest` on a spectrum with sizes `[-m, -n]` and supports `[minimum
support, number of transactions]`.

- `taslots`: per size `k` the number of "slots" `sum_t C(|t|, k)` over
  the transactions (`choose` gives up with `SIZE_MAX` when `r·n` would
  overflow, after which products and sums wrap: U11; the search for the
  largest size starts below the number of items: U17).
- `alpha <= 0`: `simpest`, a Poisson approximation with equal item
  probabilities, no randomness. Otherwise `cplxest`: item probabilities
  `frq/extent`, contracted by `(p - 1/n)·alpha + 1/n`; for every size,
  `-z` random item subsets (`samplelp`: `dbl_select` with `drand`, exact
  formulas up to four items, 16 permutations otherwise) give the
  log-probability of a subset; the Poisson distribution of the
  coincidences is summed over the samples, cut at `exp(-9)/equiv`,
  scaled to `C(n, z)/smpls` and normalized to the number of slots.
- Counts are `(size_t)(v·equiv + 0.5)`; sizes are estimated only for
  `z < n` (a size equal to the number of items never; D10); the output
  value is `(1.0/cnt)·count`.

## 5. Decision border (`-D`, `fpgpsp.c:725-740`)

`zmax` is the largest size with `psp_max4sz(psp, zmax) >=
psp_min4sz(psp, zmin)`; `border[m] = max(max4sz(m) + 1, border[m+1])`
from `zmax` down to `zmin`, at least 1; printed as `b_zmin:...:b_zmax`
only if `zmax > zmin`. `psp_min4sz` is the lower end of the counter array
of a row, which `resize` places 32 below the first support it sees (or at
the minimum support), so on dense data the border is empty (U5).

**Why P3 is the right reading (D9).** In a build with `RSUPP=double`,
`rows[z].min` is the observed minimum of the row, which suggests that the
test was meant as "the largest support at size `z` reaches the smallest
support at `zmin`", i.e. the row is not empty; with integer supports the
same expression compares with an allocation artifact. Comparing the row
with its own lower end (`min4sz(zmax)`, P3) selects the largest size with
a signature. This is the conservative choice: fpgrowth applies no border
beyond the given values, so a shorter border lets more (larger) item sets
pass; the full border never lets an item set pass that the truncated one
would reject. A single-size border is printed too.

The values are absolute supports for the sizes from `-m` on. fpgrowth's
`-F` reads positive values as percentages (`setbdr`, `fpgrowth.c:2186`),
so the border must be passed with each value negated, and with the same
`-m` (U10; `docs/cli.md`).

## 6. Defects

| ID | Defect | Evidence | Fix (oracle patch) |
| --- | --- | --- | --- |
| U1 | Every surrogate after the first is mined with the first one's item frequencies: `tbg_ifrqs(bag, 0)` caches them, and only reading, recoding, filtering and reducing invalidate the cache. The root-level perfect extensions of a surrogate also stay in the reporter (`isr_addpex` at level 0, never removed) | instrumented build: up to 334 stale events in 30 surrogates; a recount build differs in 105/504 cases; with recounting alone, item sets with duplicate items overflow the reporter (AddressSanitizer, `isr_report`) | P2, P2b |
| U2 | Single-threaded `-gr` loses multiplicities: its buffer is `clone()` (weights 1) of the reduced bag, the threads' is `tbg_clone()` | 80 transactions, 28 distinct: `-Z1` singletons 4-12, `-Z2` weighted | P1 |
| U3 | Surrogates are generated from the reduced (weighted) transactions; the weight stays with the slot, so pair swaps do not keep the weighted item frequencies | pristine `-gp` singletons differ from `-gi` | P1 |
| U4 | `-ge` works on the reduced transactions: slots and the maximum support count distinct transactions only, the probabilities use all of them | dense 400 transactions, 133 distinct: supports capped at 133, only size 1 estimated; with P1 sizes 1-7 up to 324 | P1 |
| U5 | `-D` compares with the counter array of the smallest size; prints nothing for one size | dense data: empty border for spectra up to size 8 | P3 |
| U6 | The output file is required although the usage calls it optional | `wrong number of arguments` | Go: one argument writes to stdout (extension) |
| U7 | `-ge` replaces negative seeds by the time; generation uses them | `fpgpsp.c:395` vs `258` | Go: only 0 means time |
| U8 | The progress counter is incremented by all threads without synchronization | `fpgpsp.c:226`; stderr of threaded runs varies | Go: in order |
| U9 | A failing thread setup (`break` with `w[n].err = -1`) is neither merged into the result code nor freed | `fpgpsp.c:296-322` | Go: errors of every chain are reported |
| U10 | (preserved) `-D` prints absolute supports, `fpgrowth -F` reads positive values as percentages | `fpgrowth.c:2186` | documented |
| U11 | `taslots` wraps around in `size_t` (transactions of about 60 items or more) | `patspec.c:373-387, 480` | Go: saturating counts |
| U12 | The repair of `tbg_random` never examines the item where its search stops | small random data: 671/750 chains with frequency drift, 373 with P4 | P4 |
| U13 | `-ge` with `-m` > 1 divides read-time item frequencies by the filtered extent (probabilities sum to more than 1) | `patspec.c:743-746`; overestimated supports | P5 |
| U14 | The message of an invalid `-s` prints a double with `%d` (undefined output) | `fpgpsp.c:121` | Go: `%g` |
| U15 | A NaN `-e` and `-z <= 0` with `-ge` give undefined results (assertion, division by zero, `size_t` of a negative value) | `cplxest` | Go: rejected (exit 13) |
| U16 | The closed/maximal and generator filters of the reused reporter keep the sets of earlier surrogates | `-gp -tm`: sets lost from the third surrogate; `-gr -tg -s-4` on dense data: SIGSEGV in `is_isgen` from the second surrogate | P6 |
| U17 | `taslots` ignores transactions that contain every item | `while (--n > 0)` starts below `n` | P7 |
| U18 | A second support for size 0 (`-ge -m0`) overflows the counter array of the row (single counter for size 0) | AddressSanitizer heap-buffer-overflow in `resize` | P8 |
| U19 | `-gs` mines unrecoded data with the 16-items machine (`-As`/`-Ad`), which loses or miscounts item sets (fpgrowth-core.md B8) | non-integer average count of single items, which a column shuffle keeps exactly | P9 |
| U20 | Threads fill the static table `hibit` of the 16-items machine concurrently (`fim16.c:182-184`), so threaded spectra with the machine are nondeterministic | 14 of 400 parallel runs of one command gave 7 different spectra | P10 |

The mining deviations of the Go miner from fpgrowth (M1, M4, M5, M9 in
`docs/differences.md`) also show in fpgpsp's spectra; for example the
maximal empty set of `-tm -m0` on `-gs` surrogates without a frequent
item (the M4 check). The differential test avoids them.

## 7. Port (`internal/psp`, `cmd/fpgpsp`)

- The surrogate state is upstream's: an array of transactions in slot
  order (`Data`, `Chain`), taken from the prepared `tract.Bag` (whose
  order is upstream's sort order) and expanded by the weights (P1). After
  every surrogate the array is sorted like `fpg_data(FPG_SURR)` does; with
  weights 1, equal transactions are indistinguishable, so any correct sort
  gives upstream's order. `-gs` keeps the records in file order without
  recoding.
- Every surrogate is mined on its own through the normal pipeline
  (counting, recoding, the prefix-tree bag, the miner, a new reporter):
  that is P2, P2b and P6 by construction, and recoding does not change
  the spectrum (D5). The 16-items machine runs on recoded data only (P9).
- Chains run in one goroutine each (`cmd/fpgpsp`), the spectra are summed
  in part order; the library packages start no goroutines.
- Floating point: the oracle is compiled with `-ffp-contract=off`; every
  product that feeds a sum goes through `mulAdd` (an explicit rounding;
  `TestMulAdd_noFusion`). `math.Log`/`math.Exp` may differ from libm in
  the last bit (Go's `math.Log` is assembly on amd64, pure Go on arm64 and
  386), so estimated counts may differ by one where a value lies at a
  rounding boundary. Error bound: every value is a sum of at most a few
  hundred terms computed with a few hundred roundings of numbers below
  2^53, so its relative error is far below 10^-9, and `round(v·equiv)`
  can differ by at most one count. The differential test accepts at most
  one count per cell (`1/cnt` in the output, plus `%g` rounding),
  one-sided cells within one count of zero, signature counts in the log
  that differ by at most the number of one-sided cells, and a `-D` line
  that is the border of the program's own spectrum. On macOS arm64, 18,535
  estimations of 60,000 random cases were byte-identical.
