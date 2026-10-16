#!/usr/bin/env bash
#
# Compare the upstream C fpgrowth (native build: upstream flags, nothing
# added) with the Go port's CLI (go build, default flags) on the benchmark
# datasets of reference/bench/gen.
#
# Usage: bench/compare.sh [-n reps] [-w warmup] [-m modes] [-o outdir] [regexp]
#
#   -n reps    measured runs per program, configuration and mode (default 5)
#   -w warmup  warmup runs (default 1)
#   -m modes   comma-separated output modes (default count,devnull,file):
#              count    no output file (both programs only count; this is
#                       the closest to an algorithm-only comparison)
#              devnull  format every pattern, write to /dev/null
#              file     format every pattern, write to a file (the line
#                       count must equal the pattern count)
#   -o outdir  directory for the datasets, binaries and results
#              (default: a new temporary directory)
#   regexp     run only configurations whose id matches (bench/configs.txt)
#
# Environment: GO_FPGROWTH=<path> uses a prebuilt Go binary instead of
# building ./cmd/fpgrowth (e.g. to measure a fixed snapshot).
#
# Writes <outdir>/summary.md (Markdown table, also printed) and
# <outdir>/raw.tsv (every measured run). Exits non-zero if a pattern
# count differs between the programs, the modes or the output file.
#
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
ref=$(cd "$here/.." && pwd)
root=$(cd "$ref/.." && pwd)

reps=5 warmup=1 modes=count,devnull,file outdir= only=
while getopts n:w:m:o:h opt; do
	case $opt in
	n) reps=$OPTARG ;;
	w) warmup=$OPTARG ;;
	m) modes=$OPTARG ;;
	o) outdir=$OPTARG ;;
	*) sed -n '3,25p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
	esac
done
shift $((OPTIND - 1))
only=${1:-}
[ -n "$outdir" ] || outdir=$(mktemp -d "${TMPDIR:-/tmp}/fpgbench.XXXXXX")
mkdir -p "$outdir/data" "$outdir/bin" "$outdir/work"
outdir=$(cd "$outdir" && pwd)
log() { printf 'compare.sh: %s\n' "$*" >&2; }

# C: the native variant of the reference build (upstream makefile flags
# -DNDEBUG -O3 -funroll-loops, no -ffp-contract=off).
log "building the C native variant"
cbin=$("$ref/build.sh" native | awk '$1 == "native" { print $2 }')
cp "$cbin" "$outdir/bin/fpgrowth-c"

# Go: plain go build with default flags.
if [ -n "${GO_FPGROWTH:-}" ]; then
	log "using the Go CLI $GO_FPGROWTH"
	cp "$GO_FPGROWTH" "$outdir/bin/fpgrowth-go"
else
	log "building the Go CLI"
	(cd "$root" && go build -o "$outdir/bin/fpgrowth-go" ./cmd/fpgrowth)
fi

log "generating datasets in $outdir/data"
(cd "$ref" && go run ./bench/gen -all -dir "$outdir/data")
(cd "$ref" && go run ./bench/gen -list) >"$outdir/datasets.txt"

{
	echo "# fpgrowth C vs Go ($(date -u +%Y-%m-%dT%H:%M:%SZ))"
	echo
	echo '```'
	uname -a
	if [ "$(uname)" = Darwin ]; then
		sysctl -n machdep.cpu.brand_string
		echo "cores: $(sysctl -n hw.ncpu), memory: $(($(sysctl -n hw.memsize) / 1073741824)) GiB"
	else
		grep -m1 'model name' /proc/cpuinfo || true
		echo "cores: $(nproc)"; grep MemTotal /proc/meminfo || true
	fi
	go version
	echo "go source: $(git -C "$root" describe --always --dirty 2>/dev/null || echo unknown)"
	${CC:-cc} --version | head -1
	uptime
	echo "reps=$reps warmup=$warmup modes=$modes"
	echo '```'
	echo
	echo '```'
	cat "$outdir/datasets.txt"
	echo '```'
	echo
} >"$outdir/summary.md"

log "running (reps=$reps, warmup=$warmup, modes=$modes)"
status=0
(cd "$ref" && go run ./bench/compare \
	-c "$outdir/bin/fpgrowth-c" -go "$outdir/bin/fpgrowth-go" \
	-data "$outdir/data" -configs "$here/configs.txt" -work "$outdir/work" \
	-modes "$modes" -warmup "$warmup" -n "$reps" -only "$only" \
	-raw "$outdir/raw.tsv") | tee -a "$outdir/summary.md" || status=$?
log "results: $outdir/summary.md, $outdir/raw.tsv"
exit $status
