#!/bin/bash
C=../../reference/build/native/fpgrowth/src/fpgrowth; D=data
while read -r cfg; do [ -z "$cfg" ] && continue; set -- $cfg; f=$1; shift
 ./meas.sh "Go $f $*" ./fpg "$@" $D/$f -; ./meas.sh "C  $f $*" $C "$@" $D/$f -; done
