#!/usr/bin/env bash
# Runs the differential test of the Go port against the C reference
# (package ./difftest) and writes the summary with all failures to a file.
#
#   ./difftest.sh [cases] [seed] [large]
#
# cases: small random cases checked against C and the brute force
# (default 10000); seed: base seed (default 1); large: cases of the
# C-only large-data mode (TestLarge, default 0 = skip). The summaries go
# to difftest-summary.txt and difftest-large-summary.txt in the current
# directory (or $DIFFTEST_SUMMARY, $DIFFTEST_LARGE_SUMMARY); the inputs of
# failing cases are kept under difftest-artifacts/. Other variables: see
# reference/difftest/doc.go.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
cases=${1:-10000}
seed=${2:-1}
large=${3:-0}
summary=${DIFFTEST_SUMMARY:-$PWD/difftest-summary.txt}
lsummary=${DIFFTEST_LARGE_SUMMARY:-$PWD/difftest-large-summary.txt}
artifacts=$PWD/difftest-artifacts

log=$(mktemp)
trap 'rm -f "$log"' EXIT
rm -f "$summary" "$lsummary"
mkdir -p "$artifacts"
cd "$here"
tests='^TestDifferential$'
if [ "$large" -gt 0 ]; then
	tests='^(TestDifferential|TestLarge)$'
fi
status=0
DIFFTEST_CASES=$cases DIFFTEST_SEED=$seed DIFFTEST_SUMMARY=$summary \
	DIFFTEST_LARGE=$large DIFFTEST_LARGE_SUMMARY=$lsummary \
	go test ./difftest/ -run "$tests" -count=1 -timeout 0 -v \
	-artifacts -outputdir "$artifacts" >"$log" 2>&1 || status=$?
if [ -f "$summary" ]; then
	cat "$summary"
	if [ -f "$lsummary" ]; then
		echo
		cat "$lsummary"
	fi
else
	cat "$log" # skipped (no C compiler) or failed before the sweep
fi
exit "$status"
