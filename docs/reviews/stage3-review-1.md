# Stage 3 review — iteration 1

- **Stage / iteration:** Stage 3 (benchmarks, profiling, optimization, M5 investigation), iteration 1
- **Critic:** independent reviewer agent (Go runtime and memory,
  performance engineering, profiling and benchmarking); not involved in the
  code; no repository files modified.
- **Revision:** `671474e` (commits `2f6df9f..671474e`), Apple M1 Max,
  Go 1.27.2; machine moderately loaded (load 3–6), base/head runs
  interleaved.

## Evidence gathered by the critic

| Check | Result |
| --- | --- |
| gofmt, vet, staticcheck, `go test -race ./...` | clean / all pass |
| difftest default; new seed 73191 (20 000 + 1 500 large) | 0 failures (14 932 + 1 466 byte-equal) |
| Fuzz `FuzzMine`, `FuzzMineItemsets`, `FuzzMineRules` (45 s each) | pass |
| Critic's own definition-based brute-force CLI oracle, 12 variants, adversarial generators (chains, planted large sets, ties, zero/huge weights, selection, zmin 0–3, small zmax, `-S -x -u -q`) | 456 000 runs, 0 differences (oracle catches C's B5: 1 893 / 12 000 C runs differ) |
| Limited vs unlimited closed/maximal on bench datasets (47 configs incl. `-F -e -p -m -S`) | identical |
| M5 test suites with documented case counts | numbers reproduced exactly |
| Base `3e5d372` vs head, full suite (96 benchmarks, interleaved) | no significant regression; geomean −10.1 % |
| Go vs C (14 configs) and M5 tables | reproduced within a few percent |
| Byte identity C / base / head on 13 configs | identical (except documented M5) |
| Cancellation probe (dense, very dense, 164 MB file, all algorithms) | `context.Canceled`, 13 µs – 2.4 ms |
| Streaming memory (30 MB file, incl. extension-check path) | heap ≤ 3.7 MB, RSS ≤ 5.7 MB |
| Profiles, GC, escape analysis; `unsafe`/goroutine grep | consistent with docs; GC 0 %; no `unsafe`; no new goroutines |

M5 verification: the minimized case was re-derived by hand and reproduced on
the C reference (spurious `c b 1` in `-As -l0`, `-Ad -l0`, `-At`); the
proof (one-item sufficiency, repository soundness, reachability) withstood
targeted attacks; complete vs local check clearly separated in proof and
tests; benchmark claims confirmed in magnitude.

## Findings

| ID | Severity | Summary |
| --- | --- | --- |
| S3-1 | Medium | Undocumented cost of the complete extension check on large sparse data (`sparse-large -tc -s0.05 -n3`: 765 ms / 172 MiB vs 541 ms / 102 MiB unlimited, C 304 ms / 74 MiB; mining phase 2.1× Stage 2); the 64 MiB bit-set floor admits a vertical form 9× the horizontal one |
| S3-2 | Low | M5 CLI datasets (`dNxKdP`) have no documented generation command |
| S3-3 | Low | `m5-investigation.md` §4/§8 and `PROGRESS.md` stale after the optimization commits |
| S3-4 | Low | Extension checker's documented memory bound omits the non-zero-word index and the path cache |
| S3-5 | Low | `TestExtCheckBrute` reuses the checker's own transaction storage |
| S3-6 | Info | Two kept deltas not significant in the critic's re-run; one pre-M5 RSS figure not reproduced (434 vs 673 MiB) |

## Decision

**ACCEPT WITH CONDITIONS** — no Critical/High findings. Conditions: document
S3-1 (and correct the "up to 1.7×" statement), fix S3-2 to S3-5. Medium/Low
only, so no further full review cycle is required.

## Fix record

- S3-1: documented (M5 §7.1, §8; `docs/benchmarks.md`), "up to 1.7×"
  corrected, bit-set trade-off documented. Kept improvement in
  `internal/fpg/extcheck.go`: sparse rows store only non-zero words, the
  vertical form is bounded at 5× the horizontal items (absolute 64 MiB
  floor removed), closed scans probe spread-out cover transactions first.
  sparse-large `-tc -n3` 814 ms / 181 MB → 673 ms / 126 MB (C 288 ms /
  77 MB); closed checks −3 … −14 % everywhere; maximal flat on sparse-large,
  +2.5 % on `dense -tm -s2 -n3` (p = 0.010), accepted. Completeness
  argument updated (§3.2: independent of cover storage and scan order).
- S3-2: dense M5 datasets added to `reference/bench/gen` (seeds 1/2/3,
  pinned hashes) with 15 M5 configurations.
- S3-3: M5 §4/§8 rewritten for incremental covers; progress log updated.
- S3-4: full memory bound documented (M5 §5).
- S3-5: `TestExtCheckBrute` computes from source transactions; new
  `TestExtCheckBits`, `TestSeek`.
- Verification: gofmt, vet, staticcheck, `go test -race ./...`,
  `cd reference && go test -count=1 ./...`; `TestExtCheckOracle` and
  `TestOracle` 220 000 comparisons each, `TestExtCheckUnlimited` 26 400
  runs, 0 differences; `FuzzMine` 40 s; CLI output byte-identical.

**Stage 3 closed.**
