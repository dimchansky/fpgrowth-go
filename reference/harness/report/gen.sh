#!/usr/bin/env bash
#
# Build isrreplay against the pristine upstream reporter sources and
# regenerate the golden outputs of internal/report/testdata: for every
# script <name>.isr a file <name>.c.out with the harness output.
# See README.md in this directory.
#
# Usage: ./gen.sh BUILD_DIR [SCRIPT.isr ...]
#
# BUILD_DIR receives the object files and the harness binary (any scratch
# directory outside the repository). Without script arguments all scripts
# under internal/report/testdata are replayed.
#
# Environment: CC (default: cc).
set -euo pipefail

[ $# -ge 1 ] || { echo "usage: $0 BUILD_DIR [SCRIPT.isr ...]" >&2; exit 2; }
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
src=$root/reference/upstream/fpgrowth-6.21
build=$1
shift
mkdir -p "$build"

cc=${CC:-cc}
case " $cc " in
*" -std="*) ;;
*) cc="$cc -std=c99" ;;
esac

# Upstream release flags (tract/src/makefile, util/src/makefile) plus
# -ffp-contract=off like every oracle build (reference/build.sh).
cflags=(-Wall -Wextra -Wno-unused-parameter -Wconversion -pedantic
	-DNDEBUG -O3 -funroll-loops -ffp-contract=off)
incs=(-I"$src/tract/src" -I"$src/util/src" -I"$src/math/src")

# Compiler diagnostics (the known upstream warnings, see reference/README.md)
# go to BUILD_DIR/build.log.
log=$build/build.log
: >"$log"
compile() { # compile <object> <source> [defines...]
	local obj=$1 file=$2
	shift 2
	$cc "${cflags[@]}" "${incs[@]}" "$@" -c "$file" -o "$build/$obj" 2>>"$log" ||
		{ cat "$log" >&2; exit 1; }
}

# The objects fpgrowth links for the reporter (fpgrowth/src/makefile OBJS
# and FPGOBJS), compiled with the defines of their makefile rules.
compile repcm.o    "$src/tract/src/report.c" -DISR_PATSPEC -DISR_CLOMAX
compile clomax.o   "$src/tract/src/clomax.c"
compile patspec.o  "$src/tract/src/patspec.c" -DPSP_REPORT
compile taread.o   "$src/tract/src/tract.c" -DTA_READ
compile arrays.o   "$src/util/src/arrays.c"
compile memsys.o   "$src/util/src/memsys.c"
compile idmap.o    "$src/util/src/symtab.c" -DIDMAPFN
compile escape.o   "$src/util/src/escape.c"
compile tabread.o  "$src/util/src/tabread.c"
compile tabwrite.o "$src/util/src/tabwrite.c"
compile scform.o   "$src/util/src/scanner.c"
compile isrreplay.o "$here/isrreplay.c" -DISR_PATSPEC -DISR_CLOMAX

objs=(isrreplay.o repcm.o clomax.o patspec.o taread.o arrays.o memsys.o
	idmap.o escape.o tabread.o tabwrite.o scform.o)
(cd "$build" && $cc -o isrreplay "${objs[@]}" -lm)
echo "built $build/isrreplay" >&2

if [ $# -eq 0 ]; then
	set -- $(find "$root/internal/report/testdata" -name '*.isr' | LC_ALL=C sort)
fi
for script in "$@"; do
	out=${script%.isr}.c.out
	"$build/isrreplay" "$script" >"$out.tmp"
	mv "$out.tmp" "$out"
done
echo "replayed $# script(s)" >&2
