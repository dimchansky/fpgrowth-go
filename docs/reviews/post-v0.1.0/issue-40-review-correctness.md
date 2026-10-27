# Critic review (proof and correctness) — PR #45, issue #40

- PR: https://github.com/dimchansky/fpgrowth-go/pull/45 (branch `perf/40-fisherinfo`)
- Head reviewed: c56eca9 in full, delta to ed0ead7 verified; later commits verified by the orchestrator up to 05aadbf
- Decision: ACCEPT WITH CONDITIONS (Low and Info items only)
- Recorded by the orchestrator from the critic's final message.

## Resolution of findings

- P1: A4 now names every Go 1.27 `math.Exp` implementation (eeb98ce).
- P2: the infoNats derivation is corrected to below 65u (eeb98ce). Every dependent bound was rechecked. The corrected bound for the information (about 65.5u) exceeded the documented 2^-47 (64u), so the contract and the reference tolerance were widened to 2^-46 in 05aadbf. v0.1.0 did not have this contract, so the change is not breaking. Largest measured error: 12.85u.
- P3: `TestFisherInfoUnderflowSmallTotals` covers n = 1216..2000, bit for bit, with non-vacuity checks (f1c9c2d).
- P4, P5: accepted as reported.

## Critic's report

**Critic (proof and numerics) — PR #45 (#40). Verdict is based on head ed0ead7; the full review was done on c56eca9.**

**Decision: ACCEPT WITH CONDITIONS.** All findings are Low or Info and concern the docs or tests. I found no correctness defect.

**Proof check (step by step):**
- **Lemma 1** holds. The count of summed tables is |R| ≤ b+1 ≤ n+1, which is correct. The corollary S(mode) ≤ 2λ comes from the upper bound with P(mode) ≥ 1/(b+1).
- **Lemma 2** holds for both tails.
  - Bisection: if c ≤ b, then pred(c) was evaluated and was true. Only that is needed, so a predicate that is not monotone is harmless.
  - The float branch gives S(x) > S(s), because 256u is more than 2·59u + 3u.
  - The tie case of cmpXLogX gives F(x) ≥ F(s) − δ.
  - The 2/n gap from F'' ≥ 4/n is valid: s+1 ≤ E < min(b,h), so the expansion point is interior.
  - The normalization (complement and swap) leaves S unchanged. D = 0 gives ss = 0 exactly, so the shortcut never fires there.
- **Theorem 1** holds with a wide margin. It only needs S > 2λ + 745.2, and the cutoff gives 4λ + 55 to spare. The cutoff's own rounding error is below 3e-13 absolute plus u relative, and the factor 1+2^-40 covers it.
- **Theorem 2** holds.
  - Every step re-derived holds: x0 is never the mode, the inequalities between the mode and E go the stated way, only the walk away from the mode runs, the log-domain branch is always taken, and the Neumaier sum stays below 2^60·V (the compensation term is bounded the same way).
  - Step 6, "S(c−1) ≤ S(s)", is stated tersely but holds strictly: both ways pred can be false imply S(c−1) < S(s).
- **A4 (math.Exp underflow)** is true on every architecture.
  - Pure Go, arm64, loong64 and riscv64 return 0 for x < −745.1332 (explicit Underflow check).
  - amd64 (`exp_amd64.s`, denormal path: `CMPL BX,$-52; JL underflow`): k ≤ −1076 gives 0. For k = −1075, fr < 1 rounds to 0. For x around −6e18, CVTSD2SL saturates to −2^31 and also gives 0.
  - s390x: |x| ≥ 800 returns 0 (L16/L13). For 704 ≤ |x| < 800 a scaled path runs, and its result rounds to 0 below −750.

**Delta c56eca9 → ed0ead7:**
- The `infoCutoff` expression is identical character for character.
- Moving `ss > 800` to the caller does not change the decision, because the cutoff is at least 800·(1+2^-40) > 800.
- Mutation: with the shortcut disabled, `TestFisherInfoUnderflow` fails 15 allocation checks.

**My own scans** (in `internal/ruleval`, compared bit for bit with infoSum):

| Scan | Shortcut fired | Mismatches | Not compared (over step budget) |
|---|---|---|---|
| Exhaustive, n = 1200..1340, every table with S ≥ 0.97·cutoff | 722,364 | 0 | 0 |
| Random small n (1500 to 2^20), near the cutoff on both sides | 192,064 | 0 | 0 |
| Boundary: n = 171 … 2^26±1, 2^31, 2^53±, 2^62, 9e18, 2^63−1; 15 margin families (r = 0, b = h, h = n−1, small b); s = 0, 1, b−1, b; ±3 around the crossing at 1, 1±2^-40, 1+2^-30, 1±0.001 × cutoff | 8,726 | 0 | 4,690 |
| S > 1.88e16, aimed at walks that reach an anchor | 8,328 | 0 (old path returned 0 every time) | 0 |

- In the exhaustive scan the first n where the shortcut fires is 1216.
- The shortcut never fired on a table where the old path returned a positive value.
- An earlier run reported 52 "mismatches". They were invalid tables caused by an int64 overflow in my own test filter (`b+h > n`). After fixing the filter there were none.
- Error measurement against 320-bit references: infoNats' largest relative error was 12.85u over 30k tables (the bound is 59u); lnP's largest error was 0.108 of lnPErr.

| ID | Severity | Status | Summary | Evidence |
|---|---|---|---|---|
| P1 | Low | Open (docs) | A4 in the design record names only pure Go, arm64 and amd64. Go 1.27 also has loong64, riscv64 and s390x assembly. A4 still holds on all of them, but the record should cite them. | `/usr/local/go/src/math/exp_{loong64,riscv64}.s:20,53`; `exp_s390x.s` (800.0 constant, L16/L13); `exp_amd64.s` denormal path |
| P2 | Low | Pre-existing (#16) | Two constants in the 59u derivation of infoNats are slightly off. The condition number \|δg'/g\| reaches 2.26 at δ = −0.5 (the doc says ≤ 2.1). For n > 2^53, converting the margins to float adds one rounding, so δ carries 6u, not 5u. The bound becomes about 64u. This does not matter for #40: Lemma 2 tolerates up to about 126u, and Theorem 1 tolerates 2^-42. | Analysis; measured maximum 12.85u |
| P3 | Low | Open (tests) | The committed tests start at n = 10^13. The shortcut fires from n = 1216, and no committed test covers small n. A small-n case (n ≈ 1216–2000) would close this. My exhaustive scan covers it. | `fisherlarge_test.go`; scan result |
| P4 | Info | Accepted | Above S = 1.88e16, bit identity with infoSum rests on scans only. My hunt aimed at the risky walks found no value other than 0, and the shortcut's 0 is the correctly rounded exact value in every case (Theorem 1). | Huge-S scan, 8,328 tables |
| P5 | Info | Accepted | The Theorem 2 step 6 wording is terse. The claim holds strictly. | Analysis |

**Commands run:**
- On ed0ead7, `go test ./...` passes, including my scans (ruleval took 317 s).
- `go vet` and `gofmt` are clean.

**Deferred:**
- I did not rerun the reference difftest. The c56eca9 → ed0ead7 delta does not change behavior, and the implementer reported it passing on c56eca9.
- No heavy work is pending.

**Files and cleanup:**
- My scan file is saved at `/Users/dimchansky/.cache/fpgrowth-cycle/40/zz_criticproof40_test.go` and removed from the clone.
- Raw output is in `/Users/dimchansky/.cache/fpgrowth-cycle/40/critic-proof-scan{,2,3}.txt`.
- ACTIVITY.md is updated.
- The clone `~/.cache/fpgrowth-cycle/34/review-correctness/repo` is detached at ed0ead7. It still holds the untracked `internal/fpg/critic34_test.go` and `internal/fpg/critic34f1_test.go`. They are not mine (they come from the #34 review), so I left them in place.
- None of my processes are still running. A `go test ./difftest/` (PID 87003) is running, and I did not start it.

