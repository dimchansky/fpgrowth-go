// Package difftest is the randomized differential test of the Go port's
// fpgrowth program against the upstream C program (FP-Growth 6.21). It
// consists of tests and their helpers only.
//
// # Running
//
//	cd reference && go test ./difftest/                # 10,000 cases
//	DIFFTEST_CASES=50000 go test ./difftest/ -timeout 30m -v
//	./difftest.sh 50000                                 # same, with a summary file
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
// the summary and all failures). With -artifacts, the inputs, the command
// and both outputs of every failing case are kept under the test's
// artifact directory.
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
//     (docs/differences.md; caseDeviations lists the predicates), so the
//     C output is not authoritative; instead the Go output must equal an
//     independent brute-force evaluation (brute.go: frequent, closed,
//     maximal and generator sets and single-head rules with all support,
//     size, border, selection and appearance constraints), or be a subset
//     of it with exact supports when an evaluation measure filters.
//
// Value-level deviations of measures (R4 conviction, R5 certainty factor,
// R6/R9 Fisher, R7 information, R9 Yates) excuse individual records in
// the direction the fix moves the value (valueDev). Every successful Go
// run is also checked against the brute force and for internal
// consistency (logged count, -Z statistics and pattern spectrum versus
// the records written).
package difftest
