// Package report is the mining-side item set reporter of the FP-growth
// port: it decides which item sets and association rules are reported,
// counts them, and hands them to a Sink as typed values.
//
// It ports the library side of Christian Borgelt's report.c (compiled as
// repcm.o, i.e. with ISR_PATSPEC and ISR_CLOMAX, as linked by fpgrowth),
// together with clomax.c (closed/maximal repository) and patspec.c
// (pattern spectrum). Text output (item names, separators, info formats,
// number formatting, isr_prstats printing) is not part of this package; it
// lives in internal/isrout, which implements Sink. Upstream behavior is
// specified in docs/upstream/report.md and docs/upstream/tract.md §6–§7.
//
// # Protocol
//
// A miner adds an item with Add (skipping the subtree if it returns false),
// adds the perfect extensions found for the extended set with AddPex or
// AddPexPacked, recurses into the projection if Xable(2), then calls Report
// and Remove(1). Supersets are therefore reported before their subsets.
// Perfect extensions pushed while the set is empty are never removed; the
// final Report with an empty set reports the empty set and the hypercube
// of these top-level perfect extensions.
//
// Perfect extensions that cannot change any result are not processed
// (an extension of upstream, issue #21): a miner skips collecting them
// when PexUseful returns false (generators, or a set of zmax items for
// targets other than closed and maximal sets), and Report and the
// hypercube expansion ignore the perfect extensions of a set of zmax
// items, which do not fit into any reportable superset. Results are
// identical to upstream's; on a long single path with a small maximum
// size the work becomes linear instead of quadratic. SetPexGuard turns
// this off for differential tests.
//
// # Upstream functions
//
//	isr_create, isr_createx       New
//	isr_delete                    (garbage collected)
//	isr_settarg                   (*Reporter).SetTarget
//	isr_setsupp                   (*Reporter).SetSupport
//	isr_setsize                   (*Reporter).SetSize
//	isr_setbdr, isr_clrbdr        (*Reporter).SetBorder, ClearBorder
//	isr_seteval                   (*Reporter).SetEval
//	isr_logrto                    LogRatio
//	isr_addpsp, isr_getpsp        (*Reporter).AddSpectrum, Spectrum
//	isr_open, isr_close           (*Reporter).SetSink (no file handling)
//	isr_setup                     (*Reporter).Setup
//	isr_add                       (*Reporter).Add
//	isr_addpex                    (*Reporter).AddPex
//	isr_addpexpk                  (*Reporter).AddPexPacked
//	isr_remove                    (*Reporter).Remove
//	isr_xable, isr_uses           (*Reporter).Xable, Uses
//	isr_tail                      (*Reporter).Tail
//	isr_cnt, isr_items            (*Reporter).Len, Items
//	isr_supp, isr_pexcnt          (*Reporter).Supp, PexCount
//	supps[0] (ib_getwgt)          (*Reporter).Total
//	isr_report                    (*Reporter).Report
//	isr_reportv                   (*Reporter).ReportEval
//	isr_reprule                   (*Reporter).ReportRule
//	isr_repcnt, isr_stats         (*Reporter).Count, Stats
//	report, fastout               (*Reporter).expand (single path)
//	output (filter part)          (*Reporter).output
//	count-only branch (fast < 0)  (*Reporter).countOnly
//	is_isgen                      (*Reporter).isGenerator
//	is_hash, is_cmp, SYMTAB       generatorTable
//	ia_qsort                      sortItems
//	ISEVALFN                      EvalFunc
//	CLOMAX, cm_create             closedMaximal, newClosedMaximal
//	cm_supp, cm_add, cm_remove    closedMaximal.supp, add, remove
//	cm_tail, cm_update            closedMaximal.tail, update
//	CMTREE, cmt_create            cmTree, newCMTree
//	cmt_clear, cmt_add, cmt_get   cmTree.clear, add, get
//	cmt_prune                     cmTree.pruneTo
//	cmt_project, cmt_xproj        projectTree
//	merge_*, prune_*              cmTree.merge, prune
//	copy, xcopy_*                 cmTree.copyFrom, xcopyFrom
//	MEMSYS allocation failure     ErrTooLarge (int32 node indices), (*Reporter).Err
//	PATSPEC, psp_create           Spectrum, NewSpectrum
//	psp_incfrq, psp_getfrq        (*Spectrum).Add, Frequency
//	psp_sigcnt, psp_total         (*Spectrum).SigCount, Total
//	psp_report (order of cells)   (*Spectrum).All
//
// # Deviations from upstream
//
// Each is marked "Deviation from upstream:" in the code and covered by a
// test:
//
//   - There is no fast output path, so the empty set is reported only if
//     the minimum size is 0, as on upstream's normal path (report.md B3).
//   - Count-only mode honors the maximum size for closed and maximal sets
//     (B4).
//   - Hypercube counts in count-only mode are exact binomial coefficients
//     (B5); counts saturate at math.MaxUint64 instead of wrapping.
//   - For maximal sets, the set of the top-level perfect extensions is
//     reported when nothing else has been stored (fpgrowth-core.md B4;
//     upstream never reports it because the repository's initial root
//     support 0 counts as a stored superset).
//   - The pattern spectrum stores only non-zero (size, support) cells, so
//     its memory does not grow with the support values (tract.md T9, T18).
//   - AddPexPacked handles bit 30 without signed overflow (B16).
//
// # Closed and maximal sets with a size limit
//
// The repository stores each reported set together with all its perfect
// extensions, and decides closedness with stored supersets. That is exact
// only if the miner searched the subtree of every stored set completely.
// Upstream's miners stop at zmax+1 items (Xable), so supersets beyond the
// limit are never stored and non-closed or non-maximal sets are accepted
// (fpgrowth-core.md B5). Collecting perfect extensions at the boundary
// does not help: the missing witnesses have smaller support than their
// parents (docs/m5-investigation.md). A miner with a size limit installs
// a complete check with SetCloMaxCheck, which decides closedness and
// maximality of every accepted set against all items of the database.
//
// Supports are tract.Supp (int64) instead of upstream's 32-bit int; sizes
// are int. Like upstream, the reporter relies on non-negative supports
// (the repository uses -1 and -2 as sentinels).
package report
