// Package isrout is the text output layer of the fpgrowth command: it
// writes item sets and association rules, the -Z statistics and the -P
// pattern spectrum byte for byte like Christian Borgelt's FP-Growth 6.21.
//
// It ports the output side of upstream's report.c (compiled as repcm.o),
// scn_format of scanner.c and psp_report of patspec.c. The mining side
// (which sets are reported, counting, the pattern spectrum itself) is
// internal/report; Writer implements its Sink. Upstream behavior is
// specified in docs/upstream/report.md §5 and docs/upstream/cli-io.md §7.
//
// # Line layout
//
// An item set is written as Header, the item names separated by Sep, the
// expansion of Info and a newline; a rule as Header, the head's name, Imp,
// the body's names separated by Sep, Info and a newline.
//
// # Information format
//
// Info is copied except for '%' codes: '%', up to two decimal digits (the
// number of significant digits of a floating-point value, default 6, at
// most 32; ignored for integers) and a code character. An unknown code is
// copied literally with its '%' and digits; a '%' (with digits) at the end
// is copied as well. With supp the support, total the total transaction
// weight (1 if not positive), body and head the rule body and head
// supports:
//
//	code   item sets              rules
//	%%     %                      %
//	%i     set size               rule size (body + head)
//	%a     supp                   supp
//	%d     supp (see below)       (literal)
//	%q     total                  (literal)
//	%Q     total                  total
//	%s %S  supp/total, ×100       supp/total, ×100
//	%x %X  supp/total, ×100       body/total, ×100
//	%b %h  (literal)              body, head
//	%y %Y  (literal)              head/total, ×100
//	%c %C  (literal)              supp/body (0 if body is 0), ×100
//	%l %L  (literal)              supp·total/(body·head) (or 0), ×100
//	%e %E  evaluation, ×100       evaluation, ×100
//	%p %P  evaluation, ×100       (literal)
//	%w %W  weight, ×100           (literal)
//	%r %R  weight/total, ×100     (literal)
//	%z     weight·total           (literal)
//
// The ×100 forms are computed as 100*(x/y), division first, like upstream.
// Integers are written in decimal. Floating-point values use upstream's
// own formatter (isr_numout), which differs from printf's %g: values below
// 0.001 or at least 10^digits are written in exponential form, and exact
// ties round half to odd without decimals (docs/differences.md O8, kept
// for byte-compatible output). The -N option of fpgrowth (no
// pre-formatted integers) only affects upstream's speed; it has no
// counterpart here.
//
// # Upstream functions
//
//	isr_setfmtx, hdr/sep/imp/info   Format, Config
//	inames (isr_createx, -g)        New, scanable
//	isr_open, isr_close             New, (*Writer).Close
//	isr_flush, isr_putc/puts/putsn  bufio.Writer, (*Writer).Flush
//	output (text part)              (*Writer).ItemSet
//	isr_reprule (text part)         (*Writer).Rule
//	isr_sinfo                       (*Writer).appendSetInfo
//	isr_rinfo                       (*Writer).appendRuleInfo
//	getsd                           parseDigits
//	isr_intout, isr_prefmt          strconv.AppendInt
//	isr_numout                      appendNum
//	mantout                         appendMant
//	pows, dbl_bisect                pows, bisect
//	scftab, scn_format              scanClass, appendScanable
//	isr_prstats                     AppendStats, WriteStats, (*Writer).WriteStats
//	psp_report                      WriteSpectrum
//	fpgrowth.c default -v formats   DefaultInfo
//
// # Deviations from upstream
//
// Each is marked "Deviation from upstream:" in the code and covered by a
// test:
//
//   - The record header (-h) is written; upstream never assigns it
//     (report.md B1, differences O1).
//   - Scanable item names (-g) are produced; upstream formats the names
//     before the flag is set (report.md B2, differences O2).
//   - %d is the absolute support of an item set everywhere; upstream
//     understands it only on its fast output path and prints "%d"
//     literally otherwise (report.md B6). For rules %d stays literal, as
//     upstream's rule output never knew it.
//   - The -Z statistics are written after the item sets, never spliced
//     into them (report.md B9, differences O6).
//   - Pattern spectrum write errors are reported (differences O7), like
//     all other write errors.
//
// Upstream's fast output path (report.md §5.7) does not exist; its other
// observable effect, the spurious empty set (B3), is internal/report's
// business (differences O3).
package isrout
