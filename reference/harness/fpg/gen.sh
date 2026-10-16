#!/usr/bin/env bash
#
# Produce the exact-order golden outputs of internal/fpg from the C
# reference fpgrowth executable.
#
# Usage: reference/harness/fpg/gen.sh [FPGROWTH]
#
# FPGROWTH defaults to reference/build/release/fpgrowth/src/fpgrowth
# (build it with "reference/build.sh release"; that variant adds
# -ffp-contract=off to the upstream flags).
#
# Inputs (under internal/fpg/testdata/golden):
#   data/*.tab   datasets; *.w.tab are weighted (-w). test1.tab and
#                test3.w.tab are copied from the upstream examples here,
#                the r*.tab files are written by
#                FPG_GOLDEN_GEN=1 go test ./internal/fpg -run TestGoldenGenerate
#   cases.txt    one "<dataset> <options>" case per line (# comments)
#   tree_cases.txt  the same for the rules path (association rules and
#                item sets with a rule measure); data/*.app are -R
#                appearance files (TestTreeGoldenGenerate)
# Output:
#   expected.txt for every case a header "### <dataset> | <options>",
#                a line "exit <status>" and the item set lines written by
#                fpgrowth <options> [-w] -v' [%a]' <dataset> -
#   tree_expected.txt  likewise for tree_cases.txt, with -v' [%a %b %h %e]'
#                for rules (-tr) and -v' [%a %e]' for item sets
#
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
bin=${1:-$root/reference/build/release/fpgrowth/src/fpgrowth}
golden=$root/internal/fpg/testdata/golden
ex=$root/reference/upstream/fpgrowth-6.21/fpgrowth/ex

[ -x "$bin" ] || { echo "gen.sh: no executable $bin (run reference/build.sh release)" >&2; exit 1; }

mkdir -p "$golden/data"
cp "$ex/test1.tab" "$golden/data/test1.tab"
cp "$ex/test3.tab" "$golden/data/test3.w.tab"

# run_cases CASES OUT: run every case of CASES and write OUT.
run_cases() {
	local cases=$1 out=$2 tmp line data opts w info res code ncases=0
	tmp=$(mktemp "$out.XXXXXX")
	trap 'rm -f "$tmp"' EXIT
	while IFS= read -r line || [ -n "$line" ]; do
		case $line in '' | '#'*) continue ;; esac
		data=${line%% *}
		opts=${line#* }
		w=
		case $data in *.w.tab) w=-w ;; esac
		info=" [%a]"
		if [ "$cases" = "$golden/tree_cases.txt" ]; then
			# the rules path: rules with all supports and the measure,
			# item sets with the measure
			case " $opts " in
			*" -tr "*) info=" [%a %b %h %e]" ;;
			*) info=" [%a %e]" ;;
			esac
		fi
		set +e
		# shellcheck disable=SC2086 # options are split on purpose
		res=$(cd "$golden/data" && "$bin" $w $opts "-v$info" "$data" - 2>/dev/null)
		code=$?
		set -e
		printf '### %s | %s\nexit %d\n' "$data" "$opts" "$code"
		if [ -n "$res" ]; then printf '%s\n' "$res"; fi
		ncases=$((ncases + 1))
	done <"$cases" >"$tmp"
	chmod 644 "$tmp"
	mv "$tmp" "$out"
	trap - EXIT
	echo "gen.sh: $ncases cases written to $out" >&2
}

run_cases "$golden/cases.txt" "$golden/expected.txt"
run_cases "$golden/tree_cases.txt" "$golden/tree_expected.txt"
