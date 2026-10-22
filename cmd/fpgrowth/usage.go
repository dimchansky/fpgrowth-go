package main

import (
	"fmt"
	"io"

	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
)

// Program identification (fpgrowth.c PRGNAME, DESCRIPTION, VERSION).
const (
	description = "find frequent item sets with the fpgrowth algorithm"
	version     = "version 6.21 (2022.11.22)        (c) 2004-2022   Christian Borgelt"
)

// writeBanner writes the startup message to stderr; its second line is
// terminated only after the options were validated.
func writeBanner(w io.Writer, prog string) {
	fmt.Fprintf(w, "%s - %s\n%s", prog, description, version)
}

// writeUsage writes upstream's usage message (main without arguments),
// as printed by a build with USE_ZLIB, which has option -z.
func writeUsage(w io.Writer, prog string) {
	o := defaultOptions()
	p := func(format string, args ...any) { fmt.Fprintf(w, format, args...) }
	p("usage: %s [options] infile [outfile]\n", prog)
	p("%s\n", description)
	p("%s\n", version)
	p("-t#      target type                              (default: %c)\n", o.target)
	p("         (s: frequent, c: closed, m: maximal item sets,\n")
	p("          g: generators, r: association rules)\n")
	p("-m#      minimum number of items per set/rule     (default: %d)\n", o.zmin)
	p("-n#      maximum number of items per set/rule     (default: no limit)\n")
	p("-s#      minimum support of an item set/rule      (default: %s%%)\n", formatG(o.smin.v))
	p("-S#      maximum support of an item set/rule      (default: %s%%)\n", formatG(o.smax.v))
	p("         (positive: percentage, negative: absolute number)\n")
	p("-o       use original rule support definition     (body & head)\n")
	p("-c#      minimum confidence of an assoc. rule     (default: %s%%)\n", formatG(o.conf))
	p("-e#      additional evaluation measure            (default: none)\n")
	p("-a#      aggregation mode for evaluation measure  (default: none)\n")
	p("-d#      threshold for add. evaluation measure    (default: %s%%)\n", formatG(o.thresh))
	p("-i       invalidate eval. below expected support  (default: evaluate all)\n")
	p("-p#      (min. size for) pruning with evaluation  (default: no pruning)\n")
	p("         (< 0: weak forward, > 0 strong forward, = 0: backward pruning)\n")
	p("-q#      sort items w.r.t. their frequency        (default: %d)\n", o.sort)
	p("         (1: ascending, -1: descending, 0: do not sort,\n")
	p("          2: ascending, -2: descending w.r.t. transaction size sum)\n")
	p("-A#      variant of the fpgrowth algorithm to use (default: %c)\n", o.algo)
	p("-x       do not prune with perfect extensions     (default: prune)\n")
	p("-l#      number of items for k-items machine      (default: %d)\n", o.pack)
	p("         (only for variants s and d, options -As or -Ad)\n")
	p("-j       do not sort items w.r.t. cond. support   (default: sort)\n")
	p("         (only for algorithm variant c, option -Ac)\n")
	p("-u       do not use head union tail (hut) pruning (default: use hut)\n")
	p("         (only for maximal item sets, option -tm)\n")
	p("-F#:#..  support border for filtering item sets   (default: none)\n")
	p("         (list of minimum support values, one per item set size,\n")
	p("         starting at the minimum size, as given with option -m#)\n")
	p("-R#      read item selection/appearance indicators\n")
	p("-P#      write a pattern spectrum to a file\n")
	p("-Z       print item set statistics (number of item sets per size)\n")
	p("-N       do not pre-format some integer numbers   (default: do)\n")
	p("-g       write item names in scanable form (quote certain characters)\n")
	// The line of upstream's build with USE_ZLIB (the default build has no
	// -z and does not print it; docs/differences.md O12).
	p("-z       compress output with zlib (deflate)      (default: plain text)\n")
	p("-h#      record header  for output                (default: \"%s\")\n", o.hdr)
	p("-k#      item separator for output                (default: \"%s\")\n", o.sep)
	p("-I#      implication sign for association rules   (default: \"%s\")\n", o.imp)
	p("-v#      output format for set/rule information   (default: \"%s\")\n", o.info)
	p("-w       integer transaction weight in last field (default: only items)\n")
	p("-r#      record/transaction separators            (default: \"\\n\")\n")
	p("-f#      field /item        separators            (default: \" \\t,\")\n")
	p("-b#      blank   characters                       (default: \" \\t\\r\")\n")
	p("-C#      comment characters                       (default: \"#\")\n")
	p("-!       print additional option information\n")
	p("infile   file to read transactions from           [required]\n")
	p("outfile  file to write item sets/assoc. rules to  [optional]\n")
}

// writeHelp writes upstream's additional option information (-!, help()).
// The measure list comes from ruleval.WriteHelp (re_help).
func writeHelp(w io.Writer) {
	// Write errors are ignored, like upstream's printf calls.
	p := func(s string) { _, _ = io.WriteString(w, s) }
	p("additional evaluation measures (option -e#)\n")
	p("frequent item sets:\n")
	p("  x   no measure (default)\n")
	p("  b   binary logarithm of support quotient            (+)\n")
	p("association rules:\n")
	_ = ruleval.WriteHelp(w)
	p("All measures for association rules are also applicable\n")
	p("to item sets and are then aggregated over all possible\n")
	p("association rules with a single item in the consequent.\n")
	p("The aggregation mode can be set with the option -a#.\n")
	p("Measures marked with (+) must meet or exceed the threshold,\n")
	p("measures marked with (-) must not exceed the threshold\n")
	p("in order for the rule or item set to be reported.\n")
	p("\n")
	p("evaluation measure aggregation modes (option -a#)\n")
	p("  x   no aggregation (use first value)\n")
	p("  m   minimum of individual measure values\n")
	p("  n   maximum of individual measure values\n")
	p("  a   average of individual measure values\n")
	p("\n")
	p("fpgrowth algorithm variants (option -A#)\n")
	p("  s   simple  tree nodes with only successor and parent\n")
	p("  c   complex tree nodes with children and siblings (default)\n")
	p("  d   top-down processing on a single prefix tree\n")
	p("  t   top-down processing of the prefix trees\n")
	p("Variant 'd' does not support mining closed/maximal item sets,\n")
	p("variant 't' does not support the use of a k-items machine, and\n")
	p("only variant 'c' supports item reordering w.r.t. conditional support,\n")
	p("but closed/maximal item sets can only be mined without reordering.\n")
	p("These restrictions may be removed in future versions of this program.\n")
	p("Rules as the target type or using a rule-based evaluation measure\n")
	p("enforces algorithm variant 'd'.\n")
	p("information output format characters (option -v#)\n")
	p("  %%  a percent sign\n")
	p("  %i  number of items (item set size)\n")
	p("  %a  absolute item set  support\n")
	p("  %s  relative item set  support as a fraction\n")
	p("  %S  relative item set  support as a percentage\n")
	p("  %b  absolute body set  support\n")
	p("  %x  relative body set  support as a fraction\n")
	p("  %X  relative body set  support as a percentage\n")
	p("  %h  absolute head item support\n")
	p("  %y  relative head item support as a fraction\n")
	p("  %Y  relative head item support as a percentage\n")
	p("  %c  rule confidence as a fraction\n")
	p("  %C  rule confidence as a percentage\n")
	p("  %l  lift value of a rule (confidence/prior)\n")
	p("  %L  lift value of a rule as a percentage\n")
	p("  %e  additional evaluation measure\n")
	p("  %E  additional evaluation measure as a percentage\n")
	p("  %Q  total transaction weight (database size)\n")
	p("All format characters can be preceded by the number\n")
	p("of significant digits to be printed (at most 32 digits),\n")
	p("even though this value is ignored for integer numbers.\n")
}
