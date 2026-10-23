package main

import (
	"fmt"
	"io"
)

// Program identification (fpgpsp.c PRGNAME, DESCRIPTION, VERSION).
const (
	description = "generate or estimate a pattern spectrum (FP-growth)"
	version     = "version 1.3 (2024.12.12)         (c) 2015-2024   Christian Borgelt"
)

// writeBanner writes the startup message to stderr; its second line is
// terminated only after the options were validated.
func writeBanner(w io.Writer, prog string) {
	fmt.Fprintf(w, "%s - %s\n%s", prog, description, version)
}

// writeUsage writes upstream's usage message (main without arguments).
func writeUsage(w io.Writer, prog string) {
	o := defaultOptions()
	p := func(format string, args ...any) { fmt.Fprintf(w, format, args...) }
	p("usage: %s [options] infile [outfile]\n", prog)
	p("%s\n", description)
	p("%s\n", version)
	p("-t#      target type                              (default: %c)\n", o.target)
	p("         (s: frequent, c: closed, m: maximal item sets,\n")
	p("          g: generators)\n")
	p("-m#      minimum number of items per item set     (default: %d)\n", o.zmin)
	p("-n#      maximum number of items per item set     (default: no limit)\n")
	p("-s#      minimum support of an item set           (default: %s)\n", formatG(o.supp))
	p("         (positive: percentage, negative: absolute number)\n")
	p("-A#      variant of the fpgrowth algorithm to use (default: %c)\n", o.algo)
	p("-x       do not prune with perfect extensions     (default: prune)\n")
	p("-l#      number of items for k-items machine      (default: %d)\n", o.pack)
	p("         (only for variants s and d, options -As or -Ad)\n")
	p("-i       do not sort items w.r.t. cond. support   (default: sort)\n")
	p("         (only for algorithm variant c, option -Ac)\n")
	p("-u       do not use head union tail (hut) pruning (default: use hut)\n")
	p("         (only for maximal item sets, option -tm)\n")
	p("-g#      surrogate generation method              (default: %c)\n", o.surr)
	p("-c#      number of surrogate data sets            (default: %d)\n", o.cnt)
	p("-e#      probability dispersion factor            (default: %s)\n", formatG(o.alpha))
	p("-z#      number of item set samples per size      (default: %d)\n", o.smpls)
	p("-S#      seed for random numbers                  (default: time)\n")
	p("-Z#      number of cpus/processor cores to use    (default: %d)\n", o.cpus)
	p("         (a value <= 0 means all cpus reported as available)\n")
	p("-D       print induced decision border\n")
	p("-R#      read an item selection from a file       (default: use all items)\n")
	p("-r#      record/transaction separators            (default: \"\\n\")\n")
	p("-f#      field /item        separators            (default: \" \\t,\")\n")
	p("-b#      blank   characters                       (default: \" \\t\\r\")\n")
	p("-C#      comment characters                       (default: \"#\")\n")
	p("-!       print additional option information\n")
	p("infile   file to read trains from                 [required]\n")
	p("outfile  file to write pattern spectrum to        [optional]\n")
}

// writeHelp writes the additional option information of -! (help).
func writeHelp(w io.Writer) {
	p := func(s string) { fmt.Fprint(w, s) }
	p("fpgrowth algorithm variants (option -A#)\n")
	p("  s   simple  tree nodes with only successor and parent\n")
	p("  c   complex tree nodes with children and siblings (default)\n")
	p("  d   top-down processing on a single prefix tree\n")
	p("  t   top-down processing of the prefix trees\n")
	p("Variant 'd' does not support mining closed/maximal item ")
	p("sets,\nvariant 't' does not support the use of a k-items ")
	p("machine, and\nonly variant 'c' supports item reordering ")
	p("w.r.t. conditional support,\nbut closed/maximal item sets ")
	p("can only be mined without reordering.\nThese restrictions ")
	p("may be removed in future versions of this program.\n")
	p("surrogate data generation methods (option -g#, default: -ge)\n")
	p("  e    estimate a pattern spectrum (no surrogates)\n")
	p("  i    identity (keep original data)\n")
	p("  r    random transaction generation\n")
	p("  p    permutation by pair swaps\n")
	p("  s    shuffle table-derived data\n")
}
