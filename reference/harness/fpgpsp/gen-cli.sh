#!/usr/bin/env bash
#
# Produce the end-to-end golden outputs of cmd/fpgpsp from the C oracle
# (upstream fpgpsp with the patches of reference/patches/fpgpsp).
#
# Usage: reference/harness/fpgpsp/gen-cli.sh [FPGPSP]
#
# FPGPSP defaults to reference/build/fpgpsp-fixed/fpgrowth/src/fpgpsp
# (build it with "reference/build.sh fpgpsp-fixed").
#
# Input:  cmd/fpgpsp/testdata/cases.txt and estim-cases.txt, one
#         "<name> <words>" case per line (# comments); the words are
#         separated by blanks, and a word "<file" redirects stdin
#         (/dev/null otherwise).
# Output: cmd/fpgpsp/testdata/data/   the datasets (written here)
#         cmd/fpgpsp/testdata/golden/ and golden-estim/ (the estimation
#         cases) <name>.stdout, <name>.stderr,
#         <name>.exit and, if the case wrote out.txt, <name>.file. The
#         program runs in testdata/data with argv[0] "fpgpsp"; times in
#         stderr ("[0.00s]") are replaced by "[T]".
#
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
bin=${1:-$root/reference/build/fpgpsp-fixed/fpgrowth/src/fpgpsp}
td=$root/cmd/fpgpsp/testdata
ex=$root/reference/upstream/fpgrowth-6.21/fpgrowth/ex
surr=$root/internal/psp/testdata/surr

[ -x "$bin" ] || { echo "gen-cli.sh: no executable $bin (run reference/build.sh fpgpsp-fixed)" >&2; exit 1; }
bin=$(cd "$(dirname "$bin")" && pwd)/$(basename "$bin")

# --- datasets ---
data=$td/data
rm -rf "$data"
mkdir -p "$data"
cp "$ex/test1.tab" "$ex/test2.tab" "$data/"
cp "$surr/basket.tab" "$surr/dense.tab" "$surr/table.tab" "$data/"
cp "$root/internal/psp/testdata/estim/est1.tab" "$root/internal/psp/testdata/estim/est2.tab" "$data/" # larger sets for the estimation
printf '' >"$data/empty.tab"
printf '\n\n' >"$data/blank.tab"
printf 'a b c\n' >"$data/onerow.tab"
printf 'a c\nd\n' >"$data/sel.txt"

# --- cases ---
# gen <cases file> <golden directory>
gen() {
	local cases=$1 golden=$2 line name w in code
	rm -rf "$golden"
	mkdir -p "$golden"
	n=0
	while IFS= read -r line || [ -n "$line" ]; do
		case $line in '' | '#'*) continue ;; esac
		read -r -a words <<<"$line"
		name=${words[0]}
		args=()
		in=/dev/null
		for w in "${words[@]:1}"; do
			case $w in
			'<'*) in=$data/${w#<} ;;
			*) args+=("$w") ;;
			esac
		done
		rm -f "$data/out.txt"
		set +e
		(cd "$data" && exec -a fpgpsp "$bin" ${args[@]+"${args[@]}"} <"$in") \
			>"$golden/$name.stdout" 2>"$golden/$name.stderr.raw"
		code=$?
		set -e
		sed -E 's/\[[0-9]+\.[0-9]{2}s\]/[T]/g' "$golden/$name.stderr.raw" >"$golden/$name.stderr"
		rm "$golden/$name.stderr.raw"
		echo "$code" >"$golden/$name.exit"
		if [ -e "$data/out.txt" ]; then
			mv "$data/out.txt" "$golden/$name.file"
		fi
		n=$((n + 1))
	done <"$cases"
	echo "gen-cli.sh: $n cases written to $golden" >&2
}
gen "$td/cases.txt" "$td/golden"
gen "$td/estim-cases.txt" "$td/golden-estim"
