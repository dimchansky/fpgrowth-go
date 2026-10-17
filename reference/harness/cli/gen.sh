#!/usr/bin/env bash
#
# Produce the end-to-end golden outputs of cmd/fpgrowth from the C
# reference fpgrowth executable.
#
# Usage: reference/harness/cli/gen.sh [FPGROWTH]
#
# FPGROWTH defaults to reference/build/release/fpgrowth/src/fpgrowth
# (build it with "reference/build.sh release").
#
# Input:  cmd/fpgrowth/testdata/cases.txt, one "<name> <shell words>" case
#         per line (# comments); the words may redirect stdin ("<file") or
#         close stdout (">&-"); stdin is /dev/null otherwise.
# Output: cmd/fpgrowth/testdata/data/   the datasets (written here)
#         cmd/fpgrowth/testdata/golden/ <name>.stdout, <name>.stderr and
#         <name>.exit for every case. The program runs in testdata/data
#         with argv[0] "fpgrowth"; times in stderr ("[0.00s]") are
#         replaced by "[T]".
#
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
bin=${1:-$root/reference/build/release/fpgrowth/src/fpgrowth}
td=$root/cmd/fpgrowth/testdata
ex=$root/reference/upstream/fpgrowth-6.21/fpgrowth/ex
fpgdata=$root/internal/fpg/testdata/golden/data

[ -x "$bin" ] || { echo "gen.sh: no executable $bin (run reference/build.sh release)" >&2; exit 1; }
bin=$(cd "$(dirname "$bin")" && pwd)/$(basename "$bin")

# --- datasets ---
data=$td/data
rm -rf "$data"
mkdir -p "$data"
cp "$ex/test1.tab" "$ex/test2.tab" "$ex/test3.tab" "$ex/test1.csv" "$data/"
# test6 with the blank lines between transactions replaced by '%'
awk 'BEGIN { ORS = "" } /^$/ { print "%"; next } { print $0 "\n" }' "$ex/test6.tab" >"$data/test6pct.tab"
for f in r00.tab r09.tab b1a.tab b3.tab b4.tab m9.tab app1.app; do cp "$fpgdata/$f" "$data/"; done
w() { printf "$2" >"$data/$1"; }
w sel.txt 'a c\nd a\n'
w app.txt 'in\na out\n'
w empty.tab ''
w blank.tab '\n\n'
w badw.tab 'a b 2.5\n'
w wempty.tab 'a 1\n\nb 2\n'
w negw.tab 'a b -2\na 3\n'
w bigw.tab 'a 2000000000\na b 2000000000\n'
w prefmt.tab 'a 1000000000\n'
w selblank.txt 'a\n\nb\n'
w appdup.txt 'both\na in\na out\n'
w appfld.txt 'both\na in x\n'
w appfld1.txt 'both x\na in\n'
w appexp.txt 'both\na\n'
w appunk.txt 'both\na Both\n'
w gnames.tab 'a"b c\\d e\na"b c\\d\n'
w ab.tab 'a b\na c\n'
w escsep.tab 'a,b;c\nb;c\\d\n'
w nonascii.tab 'x\303\251y z\nx\303\251y\n'
w lastsep.tab 'a b\nc,'
w recnum.tab 'a 1\nb 2\n   '
w crlf.tab 'a b\r\nb c\r\n\r\na c\r\n'
w comment.tab '# comment\na b\n  # x\na # b\n'
w dupitems.tab 'a b a a\nb a\n'
x1030=$(printf 'x%.0s' $(seq 1030))
x1024=$(printf 'x%.0s' $(seq 1024))
printf '%sEND\n%sZZZ\n' "$x1030" "$x1024" >"$data/longnames.tab"

# --- cases ---
golden=$td/golden
rm -rf "$golden"
mkdir -p "$golden"
n=0
while IFS= read -r line || [ -n "$line" ]; do
	case $line in '' | '#'*) continue ;; esac
	name=${line%% *}
	words=
	[ "$name" != "$line" ] && words=${line#* }
	set +e
	# shellcheck disable=SC2086 # the words are evaluated on purpose
	(cd "$data" && eval "exec -a fpgrowth \"\$bin\" $words" </dev/null) \
		>"$golden/$name.stdout" 2>"$golden/$name.stderr.raw"
	code=$?
	set -e
	sed -E 's/\[[0-9]+\.[0-9]{2}s\]/[T]/g' "$golden/$name.stderr.raw" >"$golden/$name.stderr"
	rm "$golden/$name.stderr.raw"
	echo "$code" >"$golden/$name.exit"
	n=$((n + 1))
done <"$td/cases.txt"
echo "gen.sh: $n cases written to $golden" >&2
