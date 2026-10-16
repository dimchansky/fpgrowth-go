#!/usr/bin/env bash
#
# Regenerate the fixtures of internal/tract from the upstream C sources.
#
# Usage: ./run.sh            (from any directory)
#
# Environment:
#   CC        C compiler driver (default: cc)
#   BUILDDIR  directory for objects and binaries (default: a temporary
#             directory that is removed on exit)
#
# Writes internal/tract/testdata/inputs (input files) and
# internal/tract/testdata/expected (one file per case set). See README.md.
#
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
up=$repo/reference/upstream/fpgrowth-6.21
td=$repo/internal/tract/testdata
in=$td/inputs
exp=$td/expected

die() { printf 'run.sh: error: %s\n' "$*" >&2; exit 1; }

[ -d "$up" ] || die "upstream sources not found at $up"
build=${BUILDDIR:-}
if [ -z "$build" ]; then
	build=$(mktemp -d "${TMPDIR:-/tmp}/tractharness.XXXXXX")
	trap 'rm -rf "$build"' EXIT
fi
mkdir -p "$build"

# --- build ------------------------------------------------------------
# The objects fpgrowth links for reading and preparing transactions
# (fpgrowth/src/makefile, util/src/makefile, tract/src/makefile), with the
# release flags of the reference build (reference/build.sh).
cc=${CC:-cc}
cflags=(-std=c99 -O3 -DNDEBUG -funroll-loops -ffp-contract=off)
incs=(-I"$up/util/src" -I"$up/tract/src")
"$cc" "${cflags[@]}" -w -c "$up/util/src/arrays.c" -o "$build/arrays.o"
"$cc" "${cflags[@]}" -w -DIDMAPFN -c "$up/util/src/symtab.c" -o "$build/idmap.o"
"$cc" "${cflags[@]}" -w -c "$up/util/src/escape.c" -o "$build/escape.o"
"$cc" "${cflags[@]}" -w -c "$up/util/src/tabread.c" -o "$build/tabread.o"
"$cc" "${cflags[@]}" -w "${incs[@]}" -DTA_READ -c "$up/tract/src/tract.c" -o "$build/taread.o"
"$cc" "${cflags[@]}" -Wall -Wextra "${incs[@]}" -DTA_READ -c "$here/tractdump.c" -o "$build/tractdump.o"
"$cc" -o "$build/tractdump" "$build/tractdump.o" "$build/taread.o" \
	"$build/tabread.o" "$build/escape.o" "$build/idmap.o" "$build/arrays.o" -lm
"$cc" -std=c99 -O2 -Wall -Wextra -o "$build/gen" "$here/gen.c"
"$cc" "${cflags[@]}" -Wall -Wextra -I"$up/util/src" -o "$build/qsortdump" \
	"$here/qsortdump.c" "$build/arrays.o"

# --- inputs -----------------------------------------------------------
rm -rf "$in" "$exp"
mkdir -p "$in" "$exp"
cp "$up"/fpgrowth/ex/test{1,2,3,4,5,6}.tab "$up/fpgrowth/ex/test1.csv" "$in/"

# gen <name> seed items records maxlen skew dup empty wmin wmax pool noise
gen() {
	local name=$1
	shift
	"$build/gen" "$@" >"$in/$name.tab"
}
gen r01 1 6 30 4 0 20 10 0 -1 0 0     # few items, many ties, duplicates
gen r02 2 8 40 5 0 10 5 0 4 0 0       # weights 0..4
gen r03 3 24 60 8 1 5 5 1 3 0 0       # more than 16 items, weighted
gen r04 4 10 50 4 0 30 0 0 -1 6 0     # 6 record patterns: heavy merging
gen r05 5 5 20 3 0 0 10 0 1 0 0       # weights 0..1, many zero weights
gen r10 10 40 80 10 1 10 2 0 -1 0 1   # skewed, separator noise, comments
gen r12 12 18 100 6 0 5 3 1 9 20 1    # weighted patterns with noise
printf 'a b 0\n' >"$in/h01.tab"                      # one record, weight 0
printf 'a b c\n' >"$in/h02.tab"                      # one record
: >"$in/h03.tab"                                     # no records
printf '\n\n\n' >"$in/h04.tab"                       # only empty records
printf 'a\na b\na b c\na b c d\nb\nb c\n\na b c d\nc\na c\nb d\na b d\n' \
	>"$in/h05.tab"                                   # prefix chains
printf 'a b 0\na b 0\nc 1\n0\nd e 2\nc 0\n' >"$in/h06.tab" # zero weights
printf 'a 0\nb 0\n' >"$in/h07.tab"                   # all weights zero
printf 'i3 i1 zz i5\ni0\n' >"$in/r02.sel"            # selection (zz absent)
printf 'a d x\n' >"$in/t1.sel"
printf 'in\ni1 head\ni2 -\ni3 bo\ni7 x\nzz out\n' >"$in/r10.app"
printf -- '-\na h\nb both\nc none\n' >"$in/t1.app"   # default: ignore

# --- cases ------------------------------------------------------------
# cases <set> <input> <format> <weights> <selection> <smin values>
# runs every item order mode (-q 0, +-1, +-2, +-3; with and without
# FPG_REORDER where it matters) for each minimum support and cycles zmin
# through 0..3 and the pack count through 0, 4, 16.
packs=(0 4 16)
cases() {
	local set=$1 input=$2 fmt=$3 w=$4 sel=$5 smins=$6
	local out=$exp/$set.txt k=0 q o s z p ros
	local args=()
	case $fmt in
	default) ;;
	csv) args+=(-b,) ;;
	seps) args+=('-f,.;:') ;;
	para) args+=('-r%' '-f\n' '-b\n') ;;
	*) die "unknown format $fmt" ;;
	esac
	[ "$w" = 1 ] && args+=(-w)
	case $sel in
	-) ;;
	*.sel) args+=("-R$in/$sel") ;;
	*.app) args+=("-A$in/$sel") ;;
	*) die "unknown selection $sel" ;;
	esac
	: >"$out"
	for q in 0 1 -1 2 -2 3 -3; do
		if [ "${q#-}" -ge 2 ]; then ros="0 1"; else ros="0"; fi
		for o in $ros; do
			for s in $smins; do
				z=$((k % 4))
				p=${packs[$(((k / 4) % 3))]}
				k=$((k + 1))
				printf 'case %s fmt=%s w=%s sel=%s smin=%s q=%s reorder=%s zmin=%s pack=%s\n' \
					"$input" "$fmt" "$w" "$sel" "$s" "$q" "$o" "$z" "$p" >>"$out"
				local oarg=()
				[ "$o" = 1 ] && oarg=(-o)
				"$build/tractdump" ${args[@]+"${args[@]}"} ${oarg[@]+"${oarg[@]}"} \
					"-s$s" "-q$q" "-m$z" "-l$p" "$in/$input" >>"$out"
			done
		done
	done
}

cases test1 test1.tab default 0 - "1 3"
cases test1-sel test1.tab default 0 t1.sel "1 2"
cases test1-app test1.tab default 0 t1.app "1 2"
cases test2 test2.tab seps 0 - "1 4"
cases test3 test3.tab default 1 - "1 3"
cases test4 test4.tab default 0 - "1 2"
cases test5 test5.tab default 0 - "1 4"
cases test6 test6.tab default 0 - "1 3"
cases test6-para test6.tab para 0 - "1"
cases test1-csv test1.csv csv 0 - "1 3"
cases r01 r01.tab default 0 - "1 5"
cases r02 r02.tab default 1 - "1 8"
cases r02-sel r02.tab default 1 r02.sel "1 8"
cases r03 r03.tab default 1 - "1 6"
cases r04 r04.tab default 0 - "1 10"
cases r05 r05.tab default 1 - "1 2"
cases r10 r10.tab default 0 - "1 4"
cases r10-app r10.tab default 0 r10.app "1 4"
cases r12 r12.tab default 1 - "1 30"
cases h01 h01.tab default 1 - "1"
cases h02 h02.tab default 0 - "1"
cases h03 h03.tab default 0 - "1"
cases h04 h04.tab default 0 - "1"
cases h05 h05.tab default 0 - "1 3"
cases h06 h06.tab default 1 - "1 2"
cases h07 h07.tab default 1 - "1"

# --- tie order of ptr_qsort ------------------------------------------
"$build/qsortdump" 1 300 >"$td/qsort.txt"

printf 'run.sh: %s case sets, %s cases, %s bytes in %s\n' \
	"$(ls "$exp" | wc -l | tr -d ' ')" \
	"$(cat "$exp"/*.txt | grep -c '^case ')" \
	"$(du -sk "$td" | cut -f1)k" "$td" >&2
