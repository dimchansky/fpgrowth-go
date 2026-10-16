# Stage 1 review — iteration 1

- **Stage / iteration:** Stage 1 (faithful port), iteration 1
- **Critic:** independent reviewer agent (FP-Growth algorithms specialist,
  C systems engineer, differential-testing expert); not involved in writing
  the code. Repository files were not modified by the critic.
- **Date / state:** 2026-10-16, HEAD `2dd7193`, clean tree.
- **Criteria:** Stage 1 objective and acceptance criteria (functionality
  accounted for, builds, meaningful tests, differential equivalence,
  documented and tested divergences, working CLI, pure Go, attribution, no
  known correctness-critical problem).

## Evidence gathered by the critic

| Check | Result |
| --- | --- |
| `gofmt -l`, `go vet ./...` (root + reference), `staticcheck ./...` | clean |
| `go test -race -count=1 ./...` | 10 packages ok; coverage 96.4 %–99.7 % |
| `CGO_ENABLED=0 go build ./...`, `go list -m all` | builds; no dependencies |
| `reference/build.sh` | 4 variants built |
| difftest seed 1 (10 000) and new seed 777 (20 000) | 0 failures (seed 777: 8 783 byte-equal, 103 semantic, 228 tolerated, 2 820 error-equal, 8 066 deviation-verified) |
| Existing fuzz targets (reader, bag, strtod, strtol) | pass |
| Critic's own Python brute force: sets (3 400 adversarial cases) and rules (1 000) | Go equals brute force in every case; all C differences explained by M3/M4/M5/M9, R3, R8 |
| Byte comparisons C vs Go: `-At` without top-level elimination (572), other variants (389 each), closed/maximal with `-n` (1 146), wide random (1 500), large data 40–1000 items / 500–8000 transactions (270) | identical except documented deviations and **one unexplained case (F1)** |
| Option-parser differential fuzz (3 000), CLI process fuzz with binary input (3 000) | only `-h`/`-g` (O1/O2) differ; 0 panics/timeouts |
| CLI edge cases, stdin spooling, SIGINT | as documented; spool file removed |

## Findings

No Critical or High findings.

| ID | Severity | Summary | Resolution |
| --- | --- | --- | --- |
| F1 | Medium | Go `math.Log` differs from Apple libm by 1 ulp (e.g. `log 3`, `log 14`); for `-eb` and log-based measures, sets at exactly the threshold (true value 0 with `-d0`) can flip membership. Repro: `printf 'a b\na\nb\nb\nx\nx\n'`, `-ts -s-1 -eb -d0 -v' %a %20e'` (C omits `a b`, Go reports it with exact 0). Undocumented; difftest count-only comparison is not boundary-aware. | Documented as deviation F-1 with test; difftest made boundary-aware and given structured independence/tie data (see iteration notes) |
| F2 | Medium | Difftest deviation predicates too broad (M1 = every `-At`, M5 = every closed/maximal with `-n`, O3 = every fast format); 40 % of cases skip C comparison of order, stderr, `-Z`, `-P`. | Predicates narrowed; full C comparison enforced whenever C's records pass the brute-force check |
| F3 | Medium | Differential suite only covers ≤ 18 items; no large-scale C comparison. | C-only large-data mode added (zipf, equal-frequency, block generators) |
| F4 | Medium | Status documents stale/contradictory (PROGRESS, feature matrix all "planned", `differences.md` statuses, reference README on `patches/`). | Fixed |
| F5 | Medium (documented) | M5: `-tc -s2 -n2` on dense data: C 0.22 s / 22 MB vs Go 31.6 s / 422 MB, same 820 sets. | Scheduled Stage 3 investigation (user requirements in PROGRESS); CLI docs warning added |
| F6 | Low | M3 side effect: logged transaction count for `-tg -m≥2` differs. | Documented in M3 |
| F7 | Low | O3 description incomplete: C's fast-path spectrum is wrong even without top-level perfect extensions. | O3 wording corrected |
| F8 | Low | C1 wording: upstream accepts `-snan`/`-cnan`, Go rejects them. | C1 made explicit |
| F9 | Low | No fuzz targets for option parsing or end-to-end mining vs oracle. | Fuzz targets added |
| F10 | Info | Pass-2 time is attributed to the recoding log step. | Documented under C7 |

## Assessment

| Criterion | Assessment |
| --- | --- |
| Upstream functionality accounted for | Met in code (34 options, all targets/variants, fim16, pex, hut, reorder, 25 measures, `-Z`, `-P`, count-only); traceability docs stale (F4) |
| Module builds | Met |
| Meaningful tests pass | Met |
| Differential equivalence | Substantially met (0 failures in 30 000 cases; critic's independent sweeps found no hidden regressions); evidence strengthened by F2/F3 fixes |
| Divergences documented and tested | Met for listed deviations; F1, F6–F8 were gaps |
| CLI works | Met |
| Pure Go | Met |
| License attribution | Met |
| No known correctness-critical problem | Met (F1 is last-bit float behavior; C itself is libm-dependent) |

## Decision

**ACCEPT Stage 1**, conditional on fixing F4, resolving F1 and addressing
F2/F3 before Stage 2 builds on it. Because only Medium/Low findings were
raised, no further full review cycle is required; the fixes are verified by
the checks listed in the iteration notes below.

## Iteration notes (fixes after the review)

Recorded by the orchestrator after the fixes landed; see the commits
following this report.

- F1: `TestLogRatioIndependenceBoundary` (internal/fpg) pins Go's value on
  the reproducer; difftest count-only comparison reruns both programs with
  an output file to classify threshold-boundary differences; structured
  independence/tie datasets added (15 % of cases).
- F2: deviation predicates narrowed (M1 only with top-level elimination,
  M5/O4 only with frequent sets above `-n`, O3 only when the fast path
  takes effect); C output that passes the brute-force check is compared
  in full (order, stderr, `-Z`, `-P`). Seed 777, 20 000 cases:
  deviation-verified 8 066 → 1 338, byte-equal 8 783 → 14 957,
  0 failures.
- F3: `TestLarge` (40–1000 items, 500–8000 transactions; zipf,
  equal-frequency, block): 10 000 cases, 0 failures (9 828 byte-equal).
- F9: `FuzzParseArgs` (9.9 M execs) and `FuzzMine` vs oracle (368 k
  execs): no failures.
- F4, F6, F7, F8, F10: documentation fixed (commit "Record Stage 1
  review ..."); M2/O3 log side effects added to `docs/differences.md`.
- Result: all review findings addressed; no new Critical/High issue.
