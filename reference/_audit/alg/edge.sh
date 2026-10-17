#!/bin/bash
# Edge-case CLI probe: Go port vs C reference (rc, first output chars, last stderr line)
cd "$(dirname "$0")"
B=./fpgrowth; C=../../reference/build/release/fpgrowth/src/fpgrowth; T=tmp
printf 'a b c\na b\nb c\na\n' > $T/e1.txt; : > $T/empty.txt; printf '\n\n\n' > $T/blank.txt; printf '5\n3\n' > $T/wonly.txt
printf 'a 9223372036854775807\na 1\n' > $T/ovf.txt; printf 'a 9223372036854775808\n' > $T/big.txt; printf 'a -1\n' > $T/neg.txt
printf 'a 1e3\n' > $T/exp.txt; printf 'a 0\nb 0\n' > $T/zero.txt; printf 'a 4611686018427387904\nb 4611686018427387903\n' > $T/max.txt
while IFS= read -r args; do
  [ -z "$args" ] && continue
  gout=$($B $args - 2>$T/gerr | tr '\n' ';' | cut -c1-70); grc=${PIPESTATUS[0]}
  cout=$($C $args - 2>$T/cerr | tr '\n' ';' | cut -c1-70); crc=${PIPESTATUS[0]}
  echo "[$args] Go rc=$grc {$gout} msg=$(tail -1 $T/gerr | cut -c1-80) || C rc=$crc {$cout}"
done <<'LIST'
-s200 tmp/e1.txt
-S0 tmp/e1.txt
-tr -c101 tmp/e1.txt
-tr -c-5 tmp/e1.txt
-tr -el -d-5 tmp/e1.txt
-n99999999999 tmp/e1.txt
-m99999999999 tmp/e1.txt
-m4294967297 tmp/e1.txt
-m-1 tmp/e1.txt
-n-1 tmp/e1.txt
-As -l17 tmp/e1.txt
-As -l-1 tmp/e1.txt
-q3 tmp/e1.txt
-q-3 tmp/e1.txt
-s-0 tmp/e1.txt
-sinf tmp/e1.txt
-s-inf tmp/e1.txt
-s1e308 tmp/e1.txt
-s-1e30 tmp/e1.txt
-S-1e30 tmp/e1.txt
tmp/empty.txt
tmp/blank.txt
-m0 -s0 tmp/blank.txt
-w tmp/wonly.txt
-w tmp/ovf.txt
-w tmp/big.txt
-w tmp/neg.txt
-w tmp/exp.txt
-w tmp/zero.txt
-w -m0 -s0 tmp/zero.txt
-w -s100 -v%a tmp/max.txt
-w -tr -c100 -s0 -v%a/%b tmp/max.txt
-p1 -eb tmp/e1.txt
-Aq tmp/e1.txt
-tx tmp/e1.txt
-tr -ez tmp/e1.txt
-ax -eb tmp/e1.txt
-ts -el -ax -s0 tmp/e1.txt
-F tmp/e1.txt
-F1:2:x tmp/e1.txt
-s0 -F-2:-1 -m1 tmp/e1.txt
LIST
