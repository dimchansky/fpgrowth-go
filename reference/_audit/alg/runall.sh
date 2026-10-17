#!/bin/bash
# Runs the brute-force campaign: bash runall.sh <ncases> <extra flags...>
cd "$(dirname "$0")"
REF=../../reference/build/release/fpgrowth/src/fpgrowth
N=${1:-5000}; shift
TAG=${TAG:-std}
mkdir -p runs/$TAG
for spec in "all 10" "sets 11" "rules 12" "eval 13"; do
  set -- $spec "$@"
  m=$1; sd=$2; shift 2
  ./bfcheck -bin ./fpgrowth -ref $REF -n $N -seed $sd -mode $m -fails runs/$TAG/fails-$m "$@" > runs/$TAG/$m.txt 2>&1 &
done
wait
