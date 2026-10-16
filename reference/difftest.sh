#!/usr/bin/env bash
# Runs the differential test of the Go port against the C reference
# (package ./difftest) and writes the summary with all failures to a file.
#
#   ./difftest.sh [cases] [seed]     # default: 10000 cases, seed 1
#
# The summary goes to difftest-summary.txt in the current directory (or
# $DIFFTEST_SUMMARY); the inputs of failing cases are kept under
# difftest-artifacts/. Other variables: see reference/difftest/doc.go.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
cases=${1:-10000}
seed=${2:-1}
summary=${DIFFTEST_SUMMARY:-$PWD/difftest-summary.txt}
artifacts=$PWD/difftest-artifacts

log=$(mktemp)
trap 'rm -f "$log"' EXIT
rm -f "$summary"
mkdir -p "$artifacts"
cd "$here"
status=0
DIFFTEST_CASES=$cases DIFFTEST_SEED=$seed DIFFTEST_SUMMARY=$summary \
	go test ./difftest/ -run TestDifferential -count=1 -timeout 0 -v \
	-artifacts -outputdir "$artifacts" >"$log" 2>&1 || status=$?
if [ -f "$summary" ]; then
	cat "$summary"
else
	cat "$log" # skipped (no C compiler) or failed before the sweep
fi
exit "$status"
