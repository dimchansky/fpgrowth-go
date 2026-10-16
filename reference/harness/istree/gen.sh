#!/usr/bin/env bash
#
# Regenerate the golden outputs of internal/istree (testdata/golden/*.out)
# from the C reference: for every case in internal/istree/testdata/cases.txt
# run the fpgrowth program on the item set tree path and record its output.
# See README.md in this directory.
#
# Usage: ./gen.sh BUILD_DIR [CASE_NAME ...]
#
# BUILD_DIR receives the patched out-of-tree build and the run logs (any
# scratch directory outside the repository). Cases of build "c" use the
# pristine release reference ($FPG, default reference/build/release/
# fpgrowth/src/fpgrowth, built by "reference/build.sh release"); cases of
# build "p" use a copy of the upstream tree with
# reference/patches/istree/fpgrowth-p-order.patch applied, built with the
# same release flags (-O3 -DNDEBUG -funroll-loops -ffp-contract=off).
#
# Environment: FPG (pristine binary), CC (default: cc).
set -euo pipefail

[ $# -ge 1 ] || { echo "usage: $0 BUILD_DIR [CASE_NAME ...]" >&2; exit 2; }
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
ref=$root/reference
build=$1
shift
mkdir -p "$build"
build=$(cd "$build" && pwd)

pristine=${FPG:-$ref/build/release/fpgrowth/src/fpgrowth}
[ -x "$pristine" ] || { echo "gen.sh: $pristine not found; run reference/build.sh release or set FPG" >&2; exit 1; }

# --- patched build (out of tree) -------------------------------------
cc=${CC:-cc}
case " $cc " in
*" -std="*) ;;
*) cc="$cc -std=c99" ;;
esac
make_cmd="make"
if ! make --version 2>/dev/null | grep -q 'GNU Make'; then make_cmd=gmake; fi
pdir=$build/patched
rm -rf -- "${pdir:?}"
mkdir -p "$pdir"
cp -R "$ref/upstream/fpgrowth-6.21/." "$pdir/"
patch -s -p1 -d "$pdir" <"$ref/patches/istree/fpgrowth-p-order.patch"
unset MAKEFLAGS MFLAGS MAKELEVEL MAKEOVERRIDES GNUMAKEFLAGS || true
if ! "$make_cmd" -C "$pdir/fpgrowth/src" fpgrowth CC="$cc" LD="${CC:-cc}" \
	ADDFLAGS=-ffp-contract=off ADDLIBS= ADDOBJS= >"$build/patched-build.log" 2>&1; then
	tail -n 40 "$build/patched-build.log" >&2
	exit 1
fi
patched=$pdir/fpgrowth/src/fpgrowth
echo "gen.sh: pristine $pristine" >&2
echo "gen.sh: patched  $patched" >&2

# --- cases -----------------------------------------------------------
data=$root/internal/istree/testdata/data
out=$root/internal/istree/testdata/golden
mkdir -p "$out"
n=0
while read -r name bld file opts; do
	case $name in '' | '#'*) continue ;; esac
	if [ $# -gt 0 ]; then
		case " $* " in *" $name "*) ;; *) continue ;; esac
	fi
	case $bld in
	c) bin=$pristine ;;
	p) bin=$patched ;;
	*) echo "gen.sh: $name: unknown build '$bld'" >&2; exit 1 ;;
	esac
	fmt=' %17a %17e'
	case " $opts " in *" -tr "*) fmt=' %17a %17b %17h %17e' ;; esac
	# shellcheck disable=SC2086 # options are split on purpose
	(cd "$data" && "$bin" $opts "-v$fmt" "$file" -) >"$out/$name.out.tmp" 2>"$build/$name.log" ||
		{ cat "$build/$name.log" >&2; exit 1; }
	mv "$out/$name.out.tmp" "$out/$name.out"
	n=$((n + 1))
done <"$root/internal/istree/testdata/cases.txt"
echo "gen.sh: wrote $n golden file(s)" >&2
