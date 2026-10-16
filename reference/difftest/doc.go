// Package difftest is the randomized differential test of the Go port's
// fpgrowth program against the upstream C program (FP-Growth 6.21). It
// consists of tests and their helpers only.
//
// # Running
//
//	cd reference && go test ./difftest/                # 10,000 small + 300 large cases
//	DIFFTEST_CASES=50000 go test ./difftest/ -run TestDifferential -timeout 30m -v
//	DIFFTEST_LARGE=10000 go test ./difftest/ -run TestLarge -timeout 30m -v
//	./difftest.sh 50000 1 10000                         # both, with summary files
//
// The C reference is reference/build/release/fpgrowth/src/fpgrowth; if it
// is missing (or DIFFTEST_REBUILD is set) the test runs ../build.sh
// release, and it is skipped if there is no C compiler. DIFFTEST_C
// selects another C binary. The Go CLI is built from ../cmd/fpgrowth
// into a temporary directory; DIFFTEST_GO selects a prebuilt program
// under test instead (e.g. a deliberately broken one, to check that the
// suite catches the fault).
//
// Environment: DIFFTEST_CASES (number of cases, default 10000; -short:
// at most 300), DIFFTEST_SEED (base seed, default 1), DIFFTEST_ONLY
// (comma-separated case indices to rerun), DIFFTEST_SUMMARY (file for
// the summary and all failures); for the large-data mode DIFFTEST_LARGE
// (number of cases, default 300; -short: at most 6; 0 skips it),
// DIFFTEST_LARGE_ONLY and DIFFTEST_LARGE_SUMMARY. With -artifacts, the
// inputs, the command and both outputs of every failing case are kept
// under the test's artifact directory.
//
// # Cases
//
// Case i of base seed s is fully determined by (s, i): newCase draws a
// dataset (number of transactions, items, length range, Zipf skew,
// planted patterns, duplicate items and transactions, empty records,
// unit/small/zero/large weights, four item name styles) and an option
// set: targets s/c/m/g/r and variants -As/-Ac/-Ad/-At (cycled evenly),
// -x, -j, -u, -l, -q, relative, absolute and zero -s, -S, -m, -n, -F, -c,
// -o, -e with all measures, -a, -d, -i, -p, -R (item selection or
// appearance file), -Z, -P, -k, -I, output to a file or count-only mode.
// Both programs run in the same directory with argv[0] "fpgrowth".
//
// The dataset generator is a copy of internal/oracle's (gen.go), because
// a nested module cannot import the root module's internal packages;
// TestGenerate_matchesOracle pins the copy to the oracle's golden hashes.
// About 15% of the cases use a structured dataset instead (structgen.go,
// drawn from a separate random stream so that the options stay the
// same): products of independent item groups, where measures of items of
// different groups are exactly at their independence values (and, for
// 60% of those cases with a measure, exactly at the threshold, e.g. -eb
// -d0 or a lift of -d100), or ties (every item in the same number of
// transactions, cyclic windows, a few repeated transactions).
//
// # Comparison
//
// The -v format of every case prints the absolute supports (%a, and %b,
// %h for rules) and, with a measure, the measure with 17 digits (%17e),
// so outputs can be parsed into canonical records (sorted item set, or
// head and sorted body) with exact integers and floats. A case passes if
//
//   - byte-equal: all outputs (item set file, stdout, stderr without the
//     timings, pattern spectrum) are identical; or
//   - semantic-equal/tolerated: the records match with exact supports,
//     float values agree within the tolerances (see measureTol: 1e-12
//     relative for most values, a bound that grows with N*ln(N) for the
//     logarithmic measures, one unit in the last printed digit for short
//     formats), records present on one side only lie within the tolerance
//     of the evaluation threshold, the common records appear in the same
//     order, and stdout, stderr and the spectrum are identical unless
//     membership differs; or
//   - error-equal: both fail with the same message; or
//   - deviation-verified: the case triggers a documented deviation
//     (docs/differences.md; caseDeviations lists the predicates, derived
//     from the data where possible) and the C output is wrong: it fails
//     the same independent brute-force check as the Go output (brute.go:
//     frequent, closed, maximal and generator sets and single-head rules
//     with all support, size, border, selection and appearance
//     constraints; with an evaluation measure only a subset with exact
//     supports), or C crashes, or it differs from Go only in an output
//     channel the deviation explains (devExempt, e.g. the log for M2/M3,
//     the pattern spectrum for O3). The Go output must pass the brute
//     force. If C's output passes the brute force, the case is compared
//     with C in full (records, order, stdout, stderr, spectrum) like any
//     other case, so a flagged deviation does not hide differences.
//
// In count-only mode (no output file) only the counts can be compared.
// If they differ and the case has an evaluation measure, both programs
// are rerun with an output file and the measure in the format
// (countByRerun); the difference is tolerated if the records differ only
// at the threshold or by a value-level deviation, and each count matches
// that program's records.
//
// Value-level deviations of measures (R4 conviction, R5 certainty factor,
// R6/R9 Fisher, R7 information, R9 Yates) excuse individual records in
// the direction the fix moves the value (valueDev). Every successful Go
// run is also checked against the brute force and for internal
// consistency (logged count, -Z statistics and pattern spectrum versus
// the records written).
//
// # Large-data mode
//
// TestLarge (large.go) compares C and Go only, without the brute force,
// on datasets with 40-1000 items and 500-8000 transactions from three
// generators (Zipf-like item popularity with planted patterns, every item
// in the same number of transactions, co-occurring item blocks), cycling
// all targets and variants, with -x, -j, -u, -l, -q, -m, -n, -F, -S, -c,
// -o, measures, -Z, -P, both item separators and count-only mode. The
// minimum support follows the item frequencies to keep the output
// moderate. Options that would trigger a documented deviation on the
// data are changed (avoidDeviations), so every case must be byte-equal or
// semantically equal (compareC) and pass the Go self-check.
package difftest
