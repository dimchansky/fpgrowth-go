#!/bin/bash
# usage: meas.sh label bin args...  -> prints wall s, max RSS MB, set count
label=$1; shift; bin=$1; shift
out=$( { /usr/bin/time -l "$bin" "$@" > /dev/null; } 2>&1 )
real=$(echo "$out" | awk '/real/{print $1}')
rss=$(echo "$out" | awk '/maximum resident set size/{printf "%.1f", $1/1048576}')
sets=$(echo "$out" | grep -o '\[[0-9]* set(s)\]' | tail -1)
echo "$label | $real s | $rss MB | $sets"
