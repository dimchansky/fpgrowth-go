# Feature matrix

Traceability from upstream FP-Growth 6.21 features to the Go port.
Semantics are specified in the audit notes (`docs/upstream/*.md`, section
references in brackets). Status: **planned** → **ported** (code + unit
tests) → **verified** (golden and differential tests pass, or an
intentional difference is documented in `docs/differences.md`).
All upstream rows were verified at the end of Stage 1 (review
`docs/reviews/stage1-review-1.md`).

## Mining targets and algorithm

| Feature | Upstream symbols (files) | Semantics [audit] | Go component | Tests | Status |
| --- | --- | --- | --- | --- | --- |
| Frequent item sets (`-ts`) | `fpg_mine`, `fpgvars[]` (fpgrowth.c), `isr_report` (report.c) | [core §1.7, report §4] | `internal/fpg`, `internal/report` | oracle property, golden, differential | verified |
| Closed item sets (`-tc`) | `ISR_CLOSED`, `cm_*` (clomax.c) | repository before gates [report §4] | `internal/report` (clomax) | oracle, differential | verified |
| Maximal item sets (`-tm`) | `ISR_MAXIMAL`, `cm_*`, `isr_tail` | [report §4, core §6 B4/B5] | `internal/report`, `internal/fpg` | oracle, differential | verified |
| Generators (`-tg`) | `ISR_GENERAS`, `is_isgen` (report.c) | [report §4, core §6 B3] | `internal/report` | oracle, differential | verified |
| Association rules (`-tr`) | `fpg_tree`, `rec_tree`, `ist_*` (istree.c) | single-item heads, empty body allowed [rules §3] | `internal/istree`, `internal/fpg` | oracle, differential | verified |
| Variant simple (`-As`) | `add_simple`, `proj_simple`, `rec_simple`, `fpg_simple` | [core §3.1] | `internal/fpg` | cross-variant, differential | verified |
| Simple + 16-items machine | `add_smp16`, `proj_smp16`, `rec_smp16`, `m16_*` (fim16.c) | [core §3.2, tract §5] | `internal/fpg` (fim16) | cross-variant, differential | verified |
| Variant complex (`-Ac`, default) | `add_cmplx`, `proj_cmplx`, `rec_cmplx`, `fpg_cmplx` | [core §3.3] | `internal/fpg` | cross-variant, differential | verified |
| Complex + conditional reordering | `add_reord`, `proj_reord` | default for `-ts`/`-tg` with `-Ac` [core §3.4] | `internal/fpg` | cross-variant, differential | verified |
| Variant single tree (`-Ad`) | `fpg_single`, `rec_single` | [core §3.5] | `internal/fpg` | cross-variant, differential | verified |
| Variant top-down (`-At`) | `fpg_topdn`, `add_topdn`, `rec_topdn`, `merge`, `copy`, `getsupp` | [core §3.6, B1] | `internal/fpg` | cross-variant, differential | verified |
| Perfect-extension pruning (`-x` disables) | `FPG_PERFECT`, `isr_addpex`, chain shortcut | [core §2.7] | `internal/fpg`, `internal/report` | on/off equivalence, differential | verified |
| Head-union-tail pruning (`-u` disables) | `FPG_TAIL`, `isr_tail` | maximal only [core §3] | `internal/fpg`, `internal/report` | on/off equivalence | verified |
| k-items machine size (`-l`) | `FPG_FIM16`, `tbg_pack`, `m16_*` | [core §3.2, tract §3.8] | `internal/tract`, `internal/fpg` | `-l0`…`-l16` equivalence | verified |
| Item reordering off (`-j`) | `FPG_REORDER` | [core §1.4] | `internal/fpg` | differential | verified |
| Variant/target normalisation | `fpg_create` | [core §1.4] | `internal/fpg` | table tests | verified |

## Thresholds, weights, constraints

| Feature | Upstream symbols | Semantics [audit] | Go component | Tests | Status |
| --- | --- | --- | --- | --- | --- |
| Min/max support, relative and absolute (`-s`, `-S`) | `fpg_data`, `fpg_report`, `ceilsupp`, `floorsupp` | `ceil(s/100·w·(1−ε))`, `floor(S/100·w·(1+ε))` [core §1.5, §4] | `internal/fpg` | boundary tables, differential | verified |
| Min/max size (`-m`, `-n`) | `isr_setsize`, `ist_setsize` | `xmax = zmax+1` for closed/maximal [report §4]; Go: size-limited search plus complete one-item extension check (M5, `docs/m5-investigation.md`) | `internal/report`, `internal/fpg` (`extcheck.go`) | oracle, `TestExtCheck*` | verified |
| Support border (`-F`) | `getbdr`, `setbdr`, `isr_setbdr` | per-size minimum from `zmin` [report §4] | `internal/report`, CLI | oracle, differential | verified |
| Transaction weights (`-w`) | `TA_WEIGHT`, `ib_read` | integer last field [tract §2.5, cli §5] | `internal/tabread`, `internal/tract` | golden, differential | verified |
| Duplicate items in a transaction | `ib_read` | ignored, first occurrence [tract §2.5] | `internal/tract` | unit, oracle | verified |
| Item selection (`-R`, sets) | `ib_readsel` | only listed items used [tract §2.3] | `internal/tabread`, `internal/tract` | golden, differential | verified |
| Item appearances (`-R`, rules) | `ib_readapp`, `ist_*` | body/head/both/ignore [tract §2.4, rules §3.4, B3] | `internal/tabread`, `internal/tract`, `internal/istree` | oracle, differential | verified |
| Item sorting (`-q`) | `tbg_recode`, `ib_recode`, `ptr_qsort`/`i2i_qsort` (arrays.c) | negated, normalised under reorder; unstable tie order [tract §2.7, core §5.3] | `internal/tract` (sort ports) | C fixtures (item order) | verified |
| `zmin` transaction filter | `tbg_filter` | not for generators (B3 fix) [tract §3.4] | `internal/tract`, `internal/fpg` | unit, oracle | verified |

## Evaluation

| Feature | Upstream symbols | Semantics [audit] | Go component | Tests | Status |
| --- | --- | --- | --- | --- | --- |
| Rule confidence (`-c`), lift | `ist_*`, `isr_rinfo` | `>=` with `1−ε` tolerance [rules §3] | `internal/istree`, `internal/isrout` | oracle | verified |
| Original rule support (`-o`) | `FPG_ORIGSUPP` | body ∪ head [rules §1.2] | `internal/istree` | oracle | verified |
| 25 rule measures (`-e`) | `re_*` (ruleval.c), chi2.c, gamma.c | [rules §6]; float64, the statistical measures reliable below a total weight of about 10^12 (N1) | `internal/ruleval` | C fixtures, exact big-number checks | verified |
| Log support quotient (`-eb`) | `isr_logrto` | [rules §4.3, report §4] | `internal/report` | differential (tolerance) | verified |
| Aggregation (`-a`) | `ist_seteval`, `evaluate` | first/min/max/avg over single-head rules; "first" depends on the internal item order (P6) [rules §4] | `internal/istree` | oracle | verified |
| Threshold (`-d`), invalidate below expected (`-i`) | `ist_seteval`, `FPG_INVBXS` | [rules §4] | `internal/istree` | oracle, differential | verified |
| Evaluation pruning (`-p`) | `ist_filter` | no effect upstream (rules B2); fixed for item sets (R2) | `internal/istree` | patched-C comparison, oracle | verified |

## Output and statistics

| Feature | Upstream symbols | Semantics [audit] | Go component | Tests | Status |
| --- | --- | --- | --- | --- | --- |
| Item set / rule text output | `isr_report`, `isr_reprule`, `output`, `fastout` | [report §5] | `internal/isrout` | golden (byte-exact) | verified |
| Info format codes (`-v`) | `isr_sinfo`, `isr_rinfo`, `isr_numout` | hand-written number formatter [report §5] | `internal/isrout` | C fixtures | verified |
| Separators, implication sign, header, scanable names (`-k -I -h -g`) | `isr_setfmt`, `scn_format` | `-h`/`-g` ignored upstream (bug) | `internal/isrout` | golden | verified |
| Item set statistics (`-Z`) | `isr_prstats` | [report §5] | `internal/report`, `internal/isrout` | golden | verified |
| Pattern spectrum (`-P`) | `psp_*` (patspec.c) | [tract §7] | `internal/report` | golden | verified |
| Count-only mode (no output file) | `isr_report` with no file | [report §4.3] | `internal/report` | unit | verified |

## Input and CLI

| Feature | Upstream symbols | Semantics [audit] | Go component | Tests | Status |
| --- | --- | --- | --- | --- | --- |
| Flexible table format (`-r -f -b -C`) | `trd_*` (tabread.c), `esc_*` (escape.c) | [cli §5] | `internal/tabread` | C fixtures, fuzz | verified |
| Option grammar, defaults, messages, exit codes | `main` (fpgrowth.c) | 34 options, exit codes 0–24 [cli §2–§4] | `cmd/fpgrowth`, `internal/cconv` | golden CLI tests | verified |
| stdin/stdout via `-` / empty name | `main`, `trd_open`, `isr_open` | [cli §3] | `cmd/fpgrowth` | CLI tests | verified |
| `fpgpsp` program (pattern spectrum from surrogate data, for significance filtering via `-F`) | `fpgpsp.c` | `fpgpsp.c` is in the archive, but it needs `tars.c` (surrogate data), `pspest.c` (spectrum estimation) and pthreads; `tars.c` and `pspest.c` are not in the archive | — | — | deferred past v0.1.0 |
| zlib output (`-z`) | `USE_ZLIB` | zlib (RFC 1950) compression of the item set/rule output; not compiled into upstream's default build, which rejects `-z` like the Go CLI | — | — | deferred past v0.1.0 |

## New in the Go port (Stage 2 and later)

| Feature | Go component | Status |
| --- | --- | --- |
| Generic item identifiers (`T comparable`) | root package, `internal/tract` | verified |
| Replayable transaction sources (in-memory, generated, files, CSV) | root package | verified |
| Streaming results (iterators/callbacks), cancellation | root package | verified |
