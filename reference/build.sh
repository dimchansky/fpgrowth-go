#!/usr/bin/env bash
#
# Build the pristine upstream FP-Growth 6.21 C sources into reference
# executables that serve as the differential-testing oracle.
#
# Usage: ./build.sh [-h] [release|debug|asan|native|zlib|fpgpsp|all]...
#        (default: all = release debug asan native)
#
# Variants:
#   release   upstream flags (-O3 -DNDEBUG -funroll-loops) + -ffp-contract=off
#   debug     -g -O0, assertions on                   + -ffp-contract=off
#   asan      -g -O1 -fsanitize=address,undefined     + -ffp-contract=off
#   native    exact upstream makefile flags, nothing added (performance
#             comparison only; NOT the differential-testing oracle)
#   zlib      release + -DUSE_ZLIB, linked with -lz: the build with
#             option -z (zlib-compressed output); needs the zlib headers
#             and library (oracle of difftest's TestZlib)
#   fpgpsp    release flags, but the make target fpgpsp: upstream's
#             pattern-spectrum program (surrogate data or estimation),
#             linked with -lpthread; binary build/fpgpsp/fpgrowth/src/fpgpsp
#
# Environment:
#   CC        C compiler driver (default: cc). "-std=c99" is appended
#             unless CC already selects a language standard (-std=...).
#   LD        linker driver (default: the value of CC)
#   MAKE      GNU make to use (default: "make", or "gmake" if "make"
#             is not GNU make)
#   ADDFLAGS  extra compiler and linker flags; this is the upstream
#             makefiles' own hook (e.g. -D_FILE_OFFSET_BITS=64).
#             Default: empty.
#
# Each variant is built from a fresh copy of the upstream tree in
# build/<variant>/; the upstream directory itself is never written to.
# The resulting binary paths are printed on stdout, one
# "<variant> <path>" pair per line; progress goes to stderr.
#
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
upstream=$here/upstream
srcname=fpgrowth-6.21
srcdir=$upstream/$srcname
manifest=$upstream/SHA256SUMS
patchdir=$here/patches
builddir=$here/build

die() { printf 'build.sh: error: %s\n' "$*" >&2; exit 1; }
log() { printf 'build.sh: %s\n' "$*" >&2; }

usage() {
	sed -n '3,35p' "$0" | sed 's/^# \{0,1\}//'
}

# --- arguments -------------------------------------------------------

variants=()
for arg in "$@"; do
	case $arg in
	-h | --help) usage; exit 0 ;;
	all) variants+=(release debug asan native) ;;
	release | debug | asan | native | zlib | fpgpsp) variants+=("$arg") ;;
	*) die "unknown variant '$arg' (expected release, debug, asan, native, zlib, fpgpsp or all)" ;;
	esac
done
[ ${#variants[@]} -gt 0 ] || variants=(release debug asan native)

# --- tools -----------------------------------------------------------

find_gnu_make() {
	local m
	for m in ${MAKE:-} make gmake; do
		if "$m" --version 2>/dev/null | grep -q 'GNU Make'; then
			printf '%s\n' "$m"
			return 0
		fi
	done
	return 1
}
make_cmd=$(find_gnu_make) || die "GNU make not found (set MAKE=/path/to/gnu-make)"

cc=${CC:-cc}
case " $cc " in
*" -std="*) ;;
*) cc="$cc -std=c99" ;;
esac
ld=${LD:-${CC:-cc}}
command -v "${cc%% *}" >/dev/null 2>&1 || die "C compiler '${cc%% *}' not found (set CC)"
command -v "${ld%% *}" >/dev/null 2>&1 || die "linker driver '${ld%% *}' not found (set LD)"
[ -x /bin/bash ] || die "/bin/bash is required (the upstream makefiles set SHELL=/bin/bash)"

if command -v sha256sum >/dev/null 2>&1; then
	sha256=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
	sha256=(shasum -a 256)
else
	die "neither sha256sum nor shasum found"
fi

# Make the build independent of the caller's make state and of stray
# environment variables that the upstream makefiles would pick up
# (ADDLIBS and ADDOBJS are referenced but never assigned there).
unset MAKEFLAGS MFLAGS MAKELEVEL MAKEOVERRIDES GNUMAKEFLAGS || true

# --- pristine check --------------------------------------------------

verify_pristine() {
	[ -f "$manifest" ] || die "checksum manifest $manifest is missing"
	[ -d "$srcdir" ] || die "upstream tree $srcdir is missing"
	local out
	if ! out=$(cd "$upstream" && "${sha256[@]}" -c SHA256SUMS 2>&1); then
		printf '%s\n' "$out" | grep -v ': OK$' >&2 || true
		die "upstream tree does not match $manifest (it must stay pristine)"
	fi
	local listed actual extra
	listed=$(sed -e 's/^[0-9a-f]\{64\} [ *]//' "$manifest" | LC_ALL=C sort)
	actual=$(cd "$upstream" && find "$srcname" -type f ! -name .DS_Store | LC_ALL=C sort)
	if [ "$listed" != "$actual" ]; then
		extra=$(LC_ALL=C comm -13 <(printf '%s\n' "$listed") <(printf '%s\n' "$actual"))
		printf 'unexpected file: %s\n' $extra >&2
		die "upstream tree contains files not listed in $manifest"
	fi
	log "upstream tree verified against $(basename "$manifest") ($(printf '%s\n' "$listed" | wc -l | tr -d ' ') files)"
}

# --- build -----------------------------------------------------------

# Floating-point contraction (fusing a*b+c into one fma instruction) is
# disabled for every oracle variant: compilers differ in whether they
# fuse by default (clang on arm64 does, gcc on x86-64 does not), which
# changes the last bits of evaluation measures and percentages. The Go
# port does not fuse, so the oracle must not fuse either.
fpflags=-ffp-contract=off

# make_args <variant>: fills the array "margs" with the make variable
# assignments for a variant. The extra flags go through the upstream
# ADDFLAGS hook, which every makefile puts into $(CFBASE) and $(LDFLAGS).
# CFLAGS keeps each makefile's own $(CFBASE) (warnings, -c, $(ADDFLAGS));
# make expands it per (sub-)makefile.
make_args() {
	local add=${ADDFLAGS:-} libs=
	[ "$1" = native ] || add="$fpflags${add:+ $add}"
	if [ "$1" = zlib ]; then
		add="$add -DUSE_ZLIB"
		libs=-lz # the makefile links $(LIBS) = -lm $(ADDLIBS)
	fi
	margs=(CC="$cc" LD="$ld" ADDFLAGS="$add" ADDLIBS="$libs" ADDOBJS=)
	case $1 in
	release | native | zlib | fpgpsp) ;; # upstream: CFLAGS = $(CFBASE) -DNDEBUG -O3 -funroll-loops
	debug)
		margs+=('CFLAGS=$(CFBASE) -g -O0')
		;;
	asan)
		margs+=('CFLAGS=$(CFBASE) -g -O1 -fsanitize=address,undefined -fno-omit-frame-pointer')
		margs+=('LDFLAGS=$(ADDFLAGS) -g -fsanitize=address,undefined')
		;;
	esac
}

# check_binary <variant> <binary>: confirms that the requested flags
# took effect (assertions on/off, sanitizer runtime linked) and that
# the program runs on the bundled example.
check_binary() {
	local variant=$1 bin=$2 dst=$3 syms
	[ -x "$bin" ] || die "$variant: $bin was not produced"
	if command -v nm >/dev/null 2>&1; then
		syms=$(nm "$bin" 2>/dev/null || true)
		case $variant in
		release | native | zlib | fpgpsp)
			if grep -Eq '__assert_(rtn|fail)' <<<"$syms"; then
				die "$variant: assertions are compiled in (expected -DNDEBUG)"
			fi
			;;
		debug | asan)
			grep -Eq '__assert_(rtn|fail)' <<<"$syms" ||
				die "$variant: assertions are not compiled in"
			;;
		esac
		if [ "$variant" = asan ]; then
			grep -q '__asan_init' <<<"$syms" || die "$variant: AddressSanitizer runtime not linked"
			grep -q '__ubsan_' <<<"$syms" || die "$variant: UndefinedBehaviorSanitizer runtime not linked"
		fi
	else
		log "nm not found; skipping flag checks for $variant"
	fi
	local smoke=$dst/smoke.out
	if [ "$variant" = fpgpsp ]; then
		# usage lists the surrogate options; estimation (-ge), a
		# single-threaded (-Z1) and a two-thread (-Z2) surrogate run
		# with a fixed seed must each produce a pattern spectrum
		"$bin" 2>/dev/null | grep -q '^-g# ' || die "$variant: -g missing from the usage message"
		local a
		for a in "-ge -S1" "-gr -c4 -S1 -Z1" "-gp -c4 -S1 -Z2"; do
			# shellcheck disable=SC2086 # $a holds several options
			if ! "$bin" $a -s-2 "$dst/fpgrowth/ex/test1.tab" "$smoke" >>"$dst/smoke.log" 2>&1; then
				cat "$dst/smoke.log" >&2
				die "$variant: smoke run '$a' failed"
			fi
			[ -s "$smoke" ] || die "$variant: smoke run '$a' produced no output"
		done
		return 0
	fi
	if ! "$bin" -s50 "$dst/fpgrowth/ex/test1.tab" "$smoke" >"$dst/smoke.log" 2>&1; then
		cat "$dst/smoke.log" >&2
		die "$variant: smoke run failed"
	fi
	[ -s "$smoke" ] || die "$variant: smoke run produced no output"
	if [ "$variant" = zlib ]; then
		# -z must be compiled in: listed in the usage message, and the
		# output starts with a zlib header (0x78, deflate, 32 KiB window)
		"$bin" 2>/dev/null | grep -q '^-z ' || die "$variant: -z missing from the usage message"
		if ! "$bin" -z -s50 "$dst/fpgrowth/ex/test1.tab" "$smoke.z" >>"$dst/smoke.log" 2>&1; then
			cat "$dst/smoke.log" >&2
			die "$variant: smoke run with -z failed"
		fi
		[ "$(head -c 1 "$smoke.z" | od -An -tx1 | tr -d ' \n')" = 78 ] ||
			die "$variant: -z output does not start with a zlib header"
	fi
}

build_variant() {
	local variant=$1
	local dst=$builddir/$variant
	local logf=$dst/build.log
	local target=fpgrowth
	[ "$variant" = fpgpsp ] && target=fpgpsp
	local bin=$dst/fpgrowth/src/$target
	local p nwarn

	rm -rf -- "${dst:?}"
	mkdir -p "$dst"
	cp -R "$srcdir/." "$dst/"
	find "$dst" -name .DS_Store -exec rm -f {} +

	if [ -d "$patchdir" ]; then
		for p in "$patchdir"/*.patch; do
			[ -e "$p" ] || continue
			log "$variant: applying $(basename "$p")"
			patch -s -p1 -d "$dst" <"$p" || die "$variant: patch $p failed"
		done
	fi

	make_args "$variant"
	log "$variant: building with $make_cmd, CC=\"$cc\", LD=\"$ld\""
	if ! "$make_cmd" -C "$dst/fpgrowth/src" "$target" "${margs[@]}" >"$logf" 2>&1; then
		tail -n 40 "$logf" >&2
		die "$variant: build failed (full log: $logf)"
	fi
	nwarn=$(grep -c 'warning:' "$logf" || true)
	log "$variant: built, $nwarn compiler warning(s) (log: $logf)"
	# every compile command must carry the floating-point flag (or, for
	# native, must not); make echoes the commands into the log
	if [ "$variant" = native ]; then
		! grep -F -- ' -c ' "$logf" | grep -qF -- "$fpflags" ||
			die "$variant: $fpflags found in a compile command"
	else
		! grep -F -- ' -c ' "$logf" | grep -vqF -- "$fpflags" ||
			die "$variant: a compile command lacks $fpflags (see $logf)"
	fi

	check_binary "$variant" "$bin" "$dst"
	printf '%s %s\n' "$variant" "$bin"
}

verify_pristine
mkdir -p "$builddir"
for v in "${variants[@]}"; do
	build_variant "$v"
done
