# Critic review: PR #31 (issue #18, ALG-3 / N3)

- Issue: #18 "Preserve exact large integer support thresholds in the CLI" (no comments on the issue)
- PR: https://github.com/dimchansky/fpgrowth-go/pull/31, branch `fix/18-exact-cli-thresholds`
- Head SHA reviewed: `b64a522` (commits 3e3c7fe, b4823ea, b64a522 on top of main d4a2022)
- Reviewer specialization: CLI design and compatibility with upstream C, number parsing (strtod/strtol), integer/float numerics; performance and architecture spot check
- Scope: `cmd/fpgrowth/options.go` (`suppArg`, `suppValue`, `borderValue`, `validate`, `minerConfig`), `cmd/fpgrowth/run.go` (border), `internal/fpg/{config,thresh}.go` (HasMinCount/HasMaxCount split), root `options.go`, the new and changed tests, and docs (differences.md N3/L-5, cli.md, README, CHANGELOG, feature-matrix, api-design).
- Work tree: fresh clone in `scratchpad/review/pr31/repo`, `git checkout FETCH_HEAD`. Comparison binaries: PR (`fpg-new`), `origin/main` (`fpg-main`), tag `v0.1.0` (`fpg-v010`), C reference `reference/build/release/fpgrowth/src/fpgrowth` (built with `./build.sh release debug`, arm64 macOS).

## Commands run and results

| Command | Result |
| --- | --- |
| `gofmt -l .` | no output |
| `go vet ./...` | OK |
| `staticcheck ./...` | OK |
| `go test ./...` | all 12 packages ok |
| `go test -race ./...` | all ok |
| `(cd reference && ./build.sh release debug && go test ./...)` | difftest ok (45 s, 71 PASS lines with -v, no skips), modcheck ok, bench/gen ok |
| `go test -run '^$' -fuzz FuzzParseArgs -fuzztime 60s ./cmd/fpgrowth` | PASS, 803,844 execs, 18 new interesting |
| Edge-spelling sweep, 3000-transaction generated dataset, C vs PR vs main vs v0.1.0 (`cmp.sh`: byte compare of output file and exit code) | 74 command lines; see below |
| Mutation tests (7 mutants) | all killed; see below |
| N3 reproductions and 2^53/2^62/2^63 cases with PR vs main | PR exact, main rounded; see below |
| CLI timing, 200,000 transactions, `-s-200` and `-s0.1`, 3 runs each | 0.16-0.17 s for both binaries; outputs identical (12,338 lines). `uptime` load 108 (other jobs), so this is only a spot check |

### Edge spellings vs C, v0.1.0 and main (outputs and exit codes byte-identical unless noted)

Identical across all four programs: `-s-5`, `-s-005`, `-s -5` (blank and tab), `-s+5`, `-s5`, `-s--5` (exit 6), `-s-5.0`, `-s-5.`, `-s-4.5`, `-s-5e0`, `-s-5e` (exit 13, `e` is the next option), `-s-5E+` (exit 6), `-s-0x5`, `-s-0x5p0`, `-s-inf` (exit 15), `-s-1e19` (15), `-s-2147483647`, `-s-2147483648`, `-s-4294967296` (15), `-s-17m2`, `-s-17e`, `-s-17x`, `-s-5S-100`, `-s-5n2`, `-s-5o`, `-s-30n2x`, `-s-0000000000000000000000000030`, `-s  -30m2`, `-s-5F-3:-5:-7`, `-F-300:-200`, `-F-300.5:x`, `-F-0x10`, `-s-5 -F-300:-200:-100`, `-s-5 -F-300.5:-199.5`, `-s-5 -F-0x10:-50`, `-s-5 -F5:-50:3`, `-S-200`, `-S-200.7`, `-S-0`, `-S0`, `-S-1e19`, `-s-5 -S-200|-200.7|-0200|10`, `-s1 -S-400`, `-s1 -S-1e19`, `-s-20 -S-0x100`, `-s-5 -S-9223372036854775807`, `-s-5 -S-2147483648`, `-s-9223372036854775808.0`, `-s-9.3e18`, `-tc/-tm/-tg` with absolute `-s`/`-S`, `-Z`, `-P` (pattern spectrum file also identical), `-ea -d50`, `-v "%a %S"`, `-tr -s-20 -c5`, `-tr -s-20 -c5 -o`, `-tr -s-21 -c5 -S-300 -o`.

Differ from C, but PR == v0.1.0 == main (pre-existing, documented): `-s-0`, `-s-00` (C segfaults, M2), `-s-nan` (C segfaults, Go exit 11, C1), `-tr ... -S...` without `-o` (C keeps empty-body rules whose body exceeds `-S`, R8).

Differ from C and from v0.1.0 (the intentional new error): `-s-5 -S-99999999999999999999` and `-s-5 -S-9223372036854775808` (C and v0.1.0 exit 0 with the full 3972-line result, PR exit 11), `-s-99999999999999999999` (C/v0.1.0 exit 15, PR exit 11), `-s-5 -F-99999999999999999999` (C/v0.1.0 exit 0, PR exit 11).

### N3 reproductions and large values (PR vs main)

- `-w -s-2305843009213693753` on `a 2305843009213693752 / b 2305843009213694152`: PR reports only `b`; main also reports `a`.
- `-w -s-4611686018427387840` on an item with that support: PR reports it; main "no (frequent) items found" (exit 15).
- `-s-9007199254740993` (2^53+1): PR reports only the 2^53+1 item, main also the 2^53 item.
- `-S-9007199254740993` in rules mode (`-tr -c40 -s-1`): PR reports `a <- b` (body 2^53+1); main nothing.
- `-tm`, `-tc`, `-o` at 2^53+1/2^53+2: correct by hand.
- `-s-9223372036854775807` with a MaxInt64 item: reported by both. `-s-00009223372036854775807`, `"-s -9223372036854775807"`, `-s-9223372036854775807m1`: exact.
- `-s-9223372036854775808`, `-S-9223372036854775808`, `-F-9223372036854775808`, `-S-99999999999999999999`: PR exit 11 with "invalid minimum/maximum support / support border <literal>"; main saturates. `-S-1e20` still saturates (no maximum) in the PR.

### Mutation tests (`go test ./cmd/fpgrowth ./internal/fpg`)

| Mutant | Result |
| --- | --- |
| `suppValue` never exact (`a.count = 0`) | killed (8 FAIL lines) |
| overflow saturates instead of `bad` | killed (7) |
| `run.go` border always uses the float path | killed (4) |
| `computeThresholds` ignores `HasMaxCount` | killed (7) |
| `computeThresholds` ignores `HasMinCount` | killed (10) |
| MinInt64 literal accepted (`i == math.MinInt64` dropped) | killed (7) |
| `minerConfig` never sets `HasMaxCount` | killed (6) |

## Analysis per focus area

1. **Exactness rule.** `suppValue` runs `cconv.Strtod`, and only if v < 0 runs `cconv.Strtol(s[:n], 10)` on the strtod subject. It accepts only if strtol consumes the same bytes. The end position is always strtod's (`s[n:]`), so clustering is unchanged by construction. The rule excludes `.`, exponents (`-17e` is still exact because strtod does not consume a bare `e`, which is correct), hex (`-0x10`: strtol stops after `-0`), `inf`, `nan` and `-0` (v = -0 is not < 0). Leading blanks and leading zeros are accepted. `+` cannot occur with v < 0. Neither the Go port nor upstream is locale-dependent: upstream never calls `setlocale`, so it runs in the C locale. Below 2^53 the integer equals the double, so ceil/floor give the same value. The sweep above confirms identical outputs vs C and v0.1.0 for every spelling upstream accepts, including clustered options and `-F` lists. Correct.
2. **Integer literals beyond int64.** These are now rejected (exit 11). The issue allowed either an error or a documented saturation, and the PR documents its choice. I recommend reverting to saturation (finding F1).
3. **HasMinCount/HasMaxCount split.** `computeThresholds` replaces each bound independently and then calls the single `derive`. No threshold logic is duplicated, and the rule body minimum, `-o` and the set minimum all come from `derive`. `-F` uses the exact count, or else `borderSupp` (unchanged). Count-only modes (`-Z`, `-P`) are unaffected (identical output). The library sets both flags, so it is unchanged. `fpg.Config` is internal, so there is no public API change. `Config.validate` checks each count only under its own flag. Correct.
4. **Tests.** The regression proof is solid: every mutant was killed. The `checkSuppValue` fuzz oracle is mostly independent of the implementation. It takes the consumed span from strtod (`floatValue`), which is the specification, not the code under test. It classifies the span with a string check and `strconv.ParseInt`, not with `cconv.Strtol`. It runs at every offset of every argument. One limitation: the span comes from the same `cconv.Strtod` that `suppValue` uses, so a strtod bug would not be caught here. That is acceptable, because `cconv.Strtod` has its own tests and the difftest.
5. **Docs.** N3, cli.md, README, CHANGELOG, feature-matrix and api-design are accurate against what I ran (both reproductions, the exit-11 messages, `-s-2.5`→3, `-S-2.5`→2, saturation of `-s-1e19`/`-S-inf`, `-s-0` as a percentage of 0). See F2 for one wording issue.
6. **Performance.** The change runs only at parse time, plus two branches in `computeThresholds`, once per run. CLI timings are indistinguishable, and the outputs are identical.

## Findings

| ID | Severity | Status | Summary | Evidence |
| --- | --- | --- | --- | --- |
| F1 | MEDIUM | confirmed | Rejecting integer literals beyond int64 (exit 11) is a behavior change vs v0.1.0 and vs the C reference on this platform, and it is inconsistent with the rest of the contract. The same magnitudes spelled differently are still accepted and saturate: `-s-9223372036854775808.0`, `-s-9.3e18`, `-S-1e20`, `-S-inf` (P1: "infinite threshold values are accepted and give saturated results"). A script that passes a huge `-S-99999…` to mean "no maximum" worked with C (arm64, exit 0, full result) and v0.1.0, and now fails. The error protects against nothing: saturating at 2^63−1 is exact in meaning (no support can exceed MaxInt64, and the minimum then finds nothing, the same as C's exit 15). | `cmp.sh` rows `-s-5 -S-99999999999999999999` (C/v0.1.0 rc 0, 3972 lines; PR rc 11), `-s-99999999999999999999` (C 15, PR 11), `-s-5 -F-99999999999999999999` (C 0, PR 11). `options.go` `suppValue` `a.bad`, `validate`. |
| F2 | LOW | confirmed | N3 calls the new error "upstream's message naming the literal". Upstream formats `E_SUPPORT` with `%g` (it would print `-1e+20`), so the message only follows upstream's pattern. Moot if F1 is adopted. | `docs/differences.md` N3. Upstream `error(E_SUPPORT, smin)` (fpgrowth.c:2421) |

No CRITICAL or HIGH findings.

## Fixes requested

- F1: replace the error with explicit saturation. For an integer literal beyond int64, set `count = math.MaxInt64` (still exact in meaning). This matches `-s-1e19`/`-S-inf`, v0.1.0 and the C reference on arm64. Update N3, cli.md and CHANGELOG ("saturates at 2^63−1 like every other spelling"), and change the `TestAbsoluteOptions`/`TestALG3CLI` "MaxInt64+1" cases to expect saturation (`-s` → exit 15, `-S` → no maximum, `-F` → border MaxInt64). If the maintainer keeps the error instead, record the rationale and the inconsistency with float spellings explicitly in N3 and in CHANGELOG under a "Changed" entry rather than "Fixed".
- F2: reword to "an upstream-style message".

## Performance impact

None measurable on the mining path. The change is parse-time only. CLI runs over 200k transactions took 0.16-0.17 s for both main and the PR, with identical output. Load was high (uptime 108), so this is a spot check, not a benchstat result.

## Decision

**ACCEPT WITH CONDITIONS** (F1 Medium, F2 Low). All acceptance criteria of #18 are verified: exact for every integer literal in [1, MaxInt64] (2^53+1, 2^62, MaxInt64 tested and reproduced), overflow behavior documented and tested, identical results vs C and v0.1.0 for every upstream spelling below 2^53 including clustering, the `-F` decision documented and tested, fuzz, goldens and difftest pass, docs updated. The condition is a maintainer decision on F1, with saturation recommended.

## Re-review of cee3e95

Commit `cee3e95` ("Saturate integer literals beyond int64 instead of rejecting them") is the only commit after `b64a522`. Diff `b64a522..cee3e95`: 8 files.

- `options.go`: overflow and MinInt64 now set `count = math.MaxInt64`. The `bad` field, the `suppArg.String` method and the exit-11 branches in `validate` are removed, and `validate` is back to exactly main's checks (`formatG`). Nothing else changed.
- Tests: the error cases are replaced by saturation cases: `TestSuppValue`, `TestAbsoluteOptions` (including `-s -0009223372036854775808m2` and `-S-1e30Z`), and `TestALG3CLI` (MaxInt64+1 and 10^20 for `-s`, `-S`, `-F`). The fuzz oracle now expects saturation. Two validation-order cases now expect the next error (`-Snan`, `-c200`), which is correct.
- Docs (CHANGELOG, PROGRESS, cli.md, differences.md N3, feature-matrix): say "saturates". No "exit status 11", "rejected" or "upstream's message" wording remains for this case.
- Nothing unrelated is in the diff.

Commands at cee3e95:
- `gofmt -l` is clean; `go vet`, `staticcheck` and `go test ./...` pass.
- `FuzzParseArgs` 30 s: PASS. Reference difftest: ok.

C (release) vs PR vs main vs v0.1.0, output bytes and exit code (gen.txt, 3000 transactions). All identical:

| Command | Lines | Exit code |
| --- | --- | --- |
| `-s-5 -S-99999999999999999999` | 3972 | 0 |
| `-s-5 -S-9223372036854775808` | 3972 | 0 |
| `-s-99999999999999999999` | 0 | 15 |
| `-s-5 -F-99999999999999999999` | 3932 | 0 |
| `-s-5 -F-9223372036854775808:-50` | 3158 | 0 |
| `-s-5 -S-1e20` | 3972 | 0 |
| `-s-9.3e18` | 0 | 15 |
| `-s-9223372036854775808.0` | 0 | 15 |
| `-s-5 -S-inf` | 3972 | 0 |
| `-s-30 -S-0099999999999999999999999m2` | 622 | 0 |
| `-s-30`, `-s-30m2` (controls) | 662, 622 | 0 |

`-w -s-9223372036854775808` on an item of support MaxInt64: the PR and v0.1.0 both report `a (9223372036854775807)`, exit 0. C cannot represent that weight (32-bit supports), so C is not a meaningful comparison here.

Exactness re-checked against main:
- `-s-2305843009213693753`: PR reports only `b`.
- `-s-4611686018427387840`: PR finds `a`; main does not.
- `-s-9007199254740993`: PR reports only `a`.
- `-s-9223372036854775807`: exact in both.

F1 is resolved and F2 is moot. No new findings.

**Decision: ACCEPT.**
