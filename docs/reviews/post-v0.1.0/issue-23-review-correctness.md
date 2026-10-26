# Review: #23 / PR #38 — correctness, statistics, RNG reproducibility

- Head reviewed: 904e54944e44b47fc1f6c4cf5b1e4ee1437a52e8
- Decision: REJECT (two High)
- Note: the critic could not write this file (harness restriction); the orchestrator recorded its final report.

## Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| F1 | HIGH | confirmed | -ge checks cannot catch systematic estimator errors: replacing rounding with truncation (+0.5 → +0.0 in simpest/cplxest, internal/psp/estim.go:195,257) or changing the cplxest threshold -9 → -7 still passes go test and TestFpgpsp (3,000 cases: 708/893 estimations "tolerated", 0 failed). No golden pins -ge. Tolerance allows 1 count in every cell; real disagreement on darwin/arm64 is ~1 per 130,000 estimations (0/92,964 at seed 404). docs/upstream/fpgpsp.md §7 only justifies rare .5-boundary differences. |
| F2 | HIGH | confirmed | Generators with -m>=2 keep upstream B3/M3 which fpgrowth fixes: psp.PrepConfig (internal/psp/data.go:69-84) keeps the -m filter for -tg. `fpgpsp -gi -tg -s-1 -m3 -n3` gives 0 signatures; brute force and Go `fpgrowth -tg -m3` give 2 generators of size 3. Null model mismatches what `fpgrowth -tg -F` filters; docs/differences.md "M1-M11 apply to the mined surrogates too" is false for M3. |
| F3 | LOW | confirmed | Apple libc %g keeps a trailing zero on a tie (1679605.0 → 1.67960e+06; Go 1.6796e+06): 2 of ~4,000 large-shape cases differ (-gi, counts >= 1e6). Oracle-platform quirk, undocumented; also affects fpgrowth -P. |
| F4 | LOW | confirmed | One goroutine per part, unbounded: -Z20000 -c20000 uses 4.31 GB RSS vs 62 MB at -Z8 (C 2.07 GB), same output. GOMAXPROCS-bounded pool would be identical. |
| F5 | LOW | confirmed | Difftest shapes small (<=12 items, ~100 rows); M1/M4/M5/M9 exclusions leave configs without Go-side checks. Suggest a committed Go-only -gi consistency test (brute force or fpgrowth -P). |

## Fixes requested
1. F1: bound tolerance by frequency (e.g. <=1 differing cell per case, <=~0.1% tolerated cases), log every tolerated cell, add C-derived -ge goldens to go test ./..., show the rounding mutation fails.
2. F2: drop the zmin filter for -tg in generation (oracle patch so -gi -tg -m>=2 equals fpgrowth -P), or maintainer decision + U row + test; correct the "M1-M11 apply" sentence.
3. F3-F5 as above.

## Verified without findings
- RNG port exact; rng.c.out, 27 surrogate chains and 66 CLI goldens regenerate byte-identically from C.
- SeedOf, Partition, per-thread cloning and merge order match C.
- >400,000 TestFpgpsp cases on new seeds (101/202/303: 30k; 404: 300k; larger shapes 66k) byte-equal apart from F3 and one tolerated estimation.
- 189 -race runs across -Z1/3/8 and GOMAXPROCS=1/2/4 equal C, no races.
- Go matches pristine upstream + P4 in 600 cases where no other patch applies (patches hide no Go behavior); P4 changes 27% of single -gr surrogates (U12).
- Reverting U2/U3, U5, U11, U12, U13, U17 fixes or breaking partition/seeding makes tests fail.
- Every test named in U/P rows exists; both fuzz targets 60 s each ok; 44 CLI edge cases match C except documented U6, U14, U15, M2; SIGINT → 130.
- #25: living docs corrected, audit records only got dated notes, CHANGELOG [0.1.0] untouched.

## Commands / data
gofmt, vet, staticcheck clean; go test ./... ok; -race on cmd/fpgpsp, internal/psp ok; build.sh fpgpsp fpgpsp-fixed release builds. Logs: review-correctness/sweep-{101,202,303,404,med-606,med-707,big-505}.log; scripts review-correctness/scripts/{pristine.py,brute.py,brute2.py,det.sh}; mutations in mut/, regeneration in regen/. Seed-505 over-large sweep stopped at load 180 (both sides timed out); its two real differences are F3. Load 2.7 → 27 (300k sweep) → 180 briefly; no timing conclusions; F4 rests on RSS.

---

# Re-review (round 2): #23 / PR #38 — correctness, statistics, RNG

- Head reviewed: a11e72ed7f42a492ae0c677ffff9fffa263cc901
- Decision: ACCEPT WITH CONDITIONS (both High fixed; one optional Low doc note)
- Date: 2026-10-26, light work only; no night sweep needed for the decision.
- Note: recorded by the orchestrator from the critic's final message (the critic cannot write files).

## Commands
- gofmt, go vet, staticcheck (root and reference/) clean; go test ./... ok; go test -race on cmd/fpgpsp and internal/psp ok.
- fpgpsp-fixed rebuilt with p01-p11; default TestFpgpsp: 3,000 cases (893 estimations) byte-equal, 0 tolerated, 0 failed.
- gen.sh and gen-cli.sh regenerate rng.c.out, 28 surrogate chains, 15 estimation traces, the CLI and golden-estim goldens byte-identically from C; estim-trace.patch lives only in the harness, not in the oracle.
- p11 scope: oracle with vs without p11 on 400 random cases (-gi/-gr/-gp/-ge, all targets, -m0..3, -Z1..3): 372 identical; all 28 differences are generators with -m>=2 (the documented trigger); 0 unexpected.
- Empty maximal set: -gs -tm -m0 table probe with no frequent item gives the same in C and Go (nothing for -tm; `0 3 1` for s/c/g); matches internal/oracle ("S is not empty"); TestMine_identity covers all four targets; onlyUniversal no longer hides the case. Matches upstream, no differences row needed.

## Findings
| ID | Sev | Status | Summary |
|---|---|---|---|
| F1 | (High) | FIXED | +0.5→+0.0 fails TestEstimate_matchesC, TestGoldenEstimate and TestFpgpsp (697 failed + tolerated-count limit). Bound (<=1 cell/case, <=1+0.1% tolerated) and logging in place. Threshold -9→-5 fails TestFpgpsp via the limit. |
| F1a | Info | acceptable | -9→-7 fails only TestEstimate_matchesC (dropped terms below printed resolution); pinning unrounded values to 1e-10 rel is the right guard; also kills 8→7 permutations. |
| F2 | (High) | FIXED | Reverting PrepConfig fails TestIdentity_bruteForce, TestIdentity_generatorsMinSize, TestChain_matchesC; U25 documented; "M1-M11 apply" sentence corrected. |
| I3 | Info | confirmed | Reverting the -gs zmin=0 change survives all tests; by reasoning it changes no output (equal-size table records). Could be noted in a comment. |
| F3 | (Low) | fixed | normGTies + TestNormGTies; row F-2. |
| F4 | (Low) | fixed | GOMAXPROCS-bounded workers; 1 worker changes no output; chainError prefers non-cancellation errors. |
| F5 | (Low) | fixed | TestIdentity_bruteForce covers M1/M4/M5/M9-excluded configs and generators with -m>=2. |
| I5 | Low | open | The empty-maximal change is only in a commit message and code comment; a one-line doc note is enough. |
| I6 | Low | open | Difftest random shapes still small; earlier 66,000 medium-shape cases byte-equal; no night sweep needed. |
