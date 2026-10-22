#!/usr/bin/env bash
#
# Build rngdump and surrdump against the upstream sources with the
# oracle patches of reference/patches/fpgpsp (the fpgpsp-fixed variant)
# and regenerate the golden files of internal/psp/testdata:
#   rng.c.out          sequences of util/src/random.c
#   surr/<case>.c.out  the surrogate chains listed in surr/cases.txt
#   estim/<case>.c.out the unrounded values of the estimations listed in
#                      estim/cases.txt (estim-trace.patch)
# See README.md in this directory.
#
# Usage: ./gen.sh BUILD_DIR
#
# BUILD_DIR receives a patched copy of the upstream tree, its objects and
# the two harness binaries (any scratch directory outside the repository).
#
# Environment: CC (default: cc).
set -euo pipefail

[ $# -eq 1 ] || { echo "usage: $0 BUILD_DIR" >&2; exit 2; }
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
build=$(mkdir -p "$1" && cd "$1" && pwd)
testdata=$root/internal/psp/testdata

cc=${CC:-cc}
case " $cc " in
*" -std="*) ;;
*) cc="$cc -std=c99" ;;
esac

rm -rf "$build/src"
cp -R "$root/reference/upstream/fpgrowth-6.21" "$build/src"
for p in "$root"/reference/patches/fpgpsp/*.patch; do
	patch -s -p1 -d "$build/src" <"$p"
done
# the objects of the fpgpsp program, with the flags of build.sh fpgpsp
make -C "$build/src/fpgrowth/src" fpgpsp CC="$cc" ADDFLAGS=-ffp-contract=off \
	ADDLIBS= ADDOBJS= >"$build/build.log" 2>&1 || { tail -n 30 "$build/build.log" >&2; exit 1; }

s=$build/src
incs=(-I"$s/util/src" -I"$s/math/src" -I"$s/tract/src" -I"$s/apriori/src" -I"$s/fpgrowth/src")
cflags=(-Wall -Wextra -Wno-unused-parameter -pedantic -DNDEBUG -O3 -ffp-contract=off)
# compiler diagnostics (the known upstream header warnings) go to the log
$cc "${cflags[@]}" "${incs[@]}" "$here/rngdump.c" "$s/util/src/random.o" -o "$build/rngdump" \
	2>>"$build/build.log" || { tail -n 30 "$build/build.log" >&2; exit 1; }
objs=()
for o in util/src/memsys.o util/src/arrays.o util/src/idmap.o util/src/escape.o \
	util/src/tabread.o util/src/tabwrite.o util/src/scform.o util/src/random.o \
	math/src/gamma.o math/src/chi2.o math/src/ruleval.o tract/src/clomax.o \
	tract/src/repcm.o tract/src/fim16.o tract/src/tars.o tract/src/pspest.o \
	apriori/src/istree.o fpgrowth/src/fpgrowth.o; do
	objs+=("$s/$o")
done
$cc "${cflags[@]}" "${incs[@]}" -DTA_READ -DTA_SURR "$here/surrdump.c" "${objs[@]}" \
	-lm -o "$build/surrdump" 2>>"$build/build.log" || { tail -n 30 "$build/build.log" >&2; exit 1; }
echo "built $build/rngdump and $build/surrdump" >&2

"$build/rngdump" >"$testdata/rng.c.out"
n=0
while read -r name method seed cnt zmin supp target algo flags data; do
	case $name in '' | '#'*) continue ;; esac
	"$build/surrdump" "$method" "$seed" "$cnt" "$zmin" "$supp" "$target" "$algo" "$flags" \
		"$testdata/surr/$data" >"$testdata/surr/$name.c.out"
	n=$((n + 1))
done <"$testdata/surr/cases.txt"
echo "wrote rng.c.out and $n surrogate chain(s)" >&2

# --- estimation traces: fpgpsp with estim-trace.patch prints every
# value v*equiv before rounding ("D <size> <support> <%a>") ---
rm -rf "$build/trace"
cp -R "$s" "$build/trace"
rm -f "$build/trace/tract/src/pspest.o" "$build/trace/fpgrowth/src/fpgpsp"
patch -s -p1 -d "$build/trace" <"$here/estim-trace.patch"
make -C "$build/trace/fpgrowth/src" fpgpsp CC="$cc" ADDFLAGS=-ffp-contract=off \
	ADDLIBS= ADDOBJS= >>"$build/build.log" 2>&1 || { tail -n 30 "$build/build.log" >&2; exit 1; }
est=$testdata/estim
n=0
while read -r name target zmin zmax supp equiv alpha smpls seed data; do
	case $name in '' | '#'*) continue ;; esac
	args=(-ge "-t$target" "-m$zmin" "-s$supp" "-c$equiv" "-e$alpha" "-z$smpls" "-S$seed")
	[ "$zmax" = 0 ] || args+=("-n$zmax")
	(cd "$est" && "$build/trace/fpgrowth/src/fpgpsp" "${args[@]}" "$data" /dev/null 2>&1 >/dev/null |
		grep '^D ' >"$est/$name.c.out") || true
	n=$((n + 1))
done <"$est/cases.txt"
echo "wrote $n estimation trace(s)" >&2
