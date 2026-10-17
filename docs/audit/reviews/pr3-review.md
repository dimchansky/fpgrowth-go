# Review of PR #3 (fix/api-audit, fd256e0) against main f7bb606

Reviewer: independent critic, read-only. Clean clone at `scratchpad/review/pr3`,
base worktree at `scratchpad/review/pr3-base` (origin/main). Reproductions in
untracked `_review/` (not part of the PR).

## Verdict: ACCEPT

Every claimed finding is fixed, each fix is minimal, every fix's test fails
when the fix is reverted, and all gates pass. Remaining points are Low/Info
and do not block.

## 1. Findings fixed (re-run reproductions)

| Audit ID | Reproduction | main | PR |
| --- | --- | --- | --- |
| API-1 | slice-typed / struct-with-slice error from MineItemsets, MineRules (all 4 algorithms x 4 targets), OnStart (callback and iterator forms), Itemsets/Rules early break | `panic: comparing uncomparable type review.sliceErr` | returned unchanged, `errors.AsType` ok, exactly 1 callback call; `err == w` identity holds for a wrapped sentinel; `multi{sentinel}` passes `errors.Is` |
| API-1 (race) | callback cancels ctx and returns an error | n/a | callback error returned (same precedence as before); cancel + nil return -> `context.Canceled` |
| API-2 | `TableReader` with `{FieldSeparators:";"}`, `{Weighted:true}`, `{Blanks:" "}` | 1 garbage transaction / weight error | `ErrInvalidOptions` naming `TableFormat.RecordSeparators`; zero format and `{RecordSeparators:"\n"}` still work |
| API-4 | `SpoolCSV(.., CSVFormat{Comma:'"'})`, `SpoolTable(.., {Comments:"#"})` | input spooled, error at mining | error up front, reader called 0 times, no file left, nil src/cleanup |
| API-5 | `[]int` item in `Options[any].Items` | only ErrInvalidOptions | matches ErrInvalidOptions and ErrInvalidItem |
| API-7 | `ItemOrderFrequencyDescending`, `ItemOrder(42)` | int | names / `ItemOrder(42)` |
| API-8 | typed-nil `SourceFunc` via MineItemsets, CollectItemsets, Rules | nil-pointer panic | `ErrInvalidOptions: src: nil source` |
| API-9 | Count(5)/Count(2), Fraction(.5)/Fraction(.4), Count(1)/Count(0), items and rules | accepted, empty | rejected; equal and mixed kinds accepted |
| API-3/10/13/16 | GoDoc + docs/api-design.md | - | accurate; order wording matches docs/releasing.md:47 |

Additional: a non-comparable error yielded by a SourceFunc in pass 1 and pass 2
is wrapped and still matched by `errors.AsType` (no panic in `mapInternal`;
`errors.go:100 err == sentinel` compares different dynamic types, safe).
No other `err ==` on user-carried errors in non-test code.

## 2. Soundness / regressions

- `mine()` now returns `sink.userErr()` whenever it is non-nil. Sound: the
  reporter returns the sink's error unchanged (`report.output`, `ReportRule`),
  mining stops at the first error (verified n == 1 for all algorithms and
  targets), and the switch order (`err == nil && ctx.Err()` first) is
  unchanged, so precedence vs cancellation is identical to before.
- Random generators: `orderedMax` is applied after all draws and consumes no
  randomness, so seeds reproduce the same datasets; only inverted same-kind
  ranges (trivially empty results) are remapped to max = min, which adds
  equal-bound coverage. The CLI path still accepts inverted ranges and
  `reference/difftest` (which does not use the public API) still covers them.

## 3. Mutation checks (fix reverted -> test result)

| Mutation | Test | Result |
| --- | --- | --- |
| `sink.userErr() != nil` -> `err == sink.userErr()` | TestCallbackErrorNotComparable | FAIL (panic) |
| `isNilSource` -> `src == nil` (items + rules) | TestNilSource | FAIL (panic) |
| drop checkRange in Options.plan | TestOptionsValidation | FAIL |
| drop checkRange in RuleOptions.plan | TestRuleOptionsValidation | FAIL |
| `max(minSupp.count,1)` -> `minSupp.count` | TestOptionsValidation | FAIL |
| drop `err: f.validate()` in TableReader | TestTableFormatValidation | FAIL |
| drop validate in SpoolTable | TestTableFormatValidation | FAIL |
| drop validate in SpoolCSV | TestSpool | FAIL |
| checkItems back to `invalid(...)` | TestInvalidItems | FAIL |
| remove Mine-entry ctx check + all `buildCheckEvery` checks (DIF-9 guard) | TestCancelTreeBuild (100 ms) | FAIL x3 runs, latencies 214-396 ms |

## 4. Findings of this review

| ID | Sev | Finding | Evidence / suggestion |
| --- | --- | --- | --- |
| R-1 | Low | Fraction/Fraction range is compared on the raw fractions, not after upstream's DBL_EPSILON-tolerant conversion, so a nominally inverted range that previously reported results is now an error. | W=10, Min `Fraction(0.30000000000000004)`, Max `Fraction(0.3)`: main reports `{a}:3 {c}:3`; PR returns `MaxSupport: Fraction(0.3) below MinSupport ...`. Consistent with the new doc ("must not be below"), contrived; accept as documented or compare with the same tolerance. |
| R-2 | Low | `Count(0)` as MaxSupport changes from "accepted, reports nothing" to ErrInvalidOptions. | Intentional, documented in `Options.MaxSupport` ("a Count minimum below 1 counts as 1"), consistent with Count(0) min semantics; acceptable pre-v0.1.0. Only users who mistook Count(0) for "no limit" are affected, and they now get an error instead of an empty result. |
| R-3 | Info | PR body says a full tree build takes "115-121 ms"; the code comment says 115-490 ms. The 100 ms bound leaves only ~15% below the lowest measured build time; on a faster machine the guard could stop discriminating. | Mutation still fails today (214-396 ms). Fix the PR text; consider deriving the bound from a measured build time. |
| R-4 | Info | `{RecordSeparators:"\n"}` with empty FieldSeparators is accepted (one item per line). | Reasonable (legit use); audit left it open; no change needed. |
| R-5 | Info | `ErrInvalidItem` GoDoc (errors.go:31) does not mention that invalid `Options.Items` entries now match it too (documented on `Options.Items` and in api-design.md). docs/PROGRESS.md not updated. | Optional doc touch. |
| R-6 | Info | Typed-nil check covers only `SourceFunc`; a nil pointer of a user Source type still panics in the user's method. | Standard Go behavior; fine. |

## 5. Commands run

- `gofmt -l .` -> empty
- `go vet ./...` -> ok
- `staticcheck ./...` -> no findings
- `go test -race -count=1 ./...` -> all 11 packages ok (root 44 s)
- `cd reference && ./build.sh && go test -count=1 ./...` -> native build ok (77 C warnings, upstream), bench/gen ok, difftest ok (41 s), modcheck ok
- `go test -count=1 -v ./_review/` on PR -> PASS; same file on main -> panics (API-1, API-8)
- `go test -run TestCancelTreeBuild .` x3 on PR -> ok
- Mutation runs as in section 3.
