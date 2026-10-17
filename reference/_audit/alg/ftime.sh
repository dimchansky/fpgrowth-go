#!/bin/bash
# Times the Fisher measures (-ef/-es) for growing total weights; kills after 20 s.
cd "$(dirname "$0")"
for f in f1 f2 chi_big; do for m in f s h; do
  start=$(date +%s)
  ./fpgrowth -tr -w -s0 -c0 -m2 -e$m -d100 -v' %a %b %h %20e' tmp/$f.txt - > tmp/out.txt 2>/dev/null &
  pid=$!; ( sleep 20; kill $pid 2>/dev/null ) & killer=$!
  wait $pid; rc=$?; kill $killer 2>/dev/null
  echo "$f -e$m rc=$rc secs=$(( $(date +%s) - start )) $(grep '^y <- x' tmp/out.txt)"
done; done
