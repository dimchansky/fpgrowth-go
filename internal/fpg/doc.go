// Package fpg is the FP-growth mining core: a port of the mining part of
// Christian Borgelt's fpgrowth.c (FP-Growth 6.21) and of the 16-items
// machine fim16.c, with the data preparation and reporter configuration of
// fpg_data and fpg_report as a driver. Upstream behavior is specified in
// docs/upstream/fpgrowth-core.md and docs/upstream/tract.md §5.
//
// Every variant reproduces upstream's traversal, perfect extension
// handling, 16-items machine behavior and sort order exactly, so the item
// sets reach the reporter (internal/report) in upstream's order and with
// upstream's item order within each set.
//
// # API
//
// Miner follows upstream's life cycle: New (fpg_create: validation and
// normalization of Config), Data (fpg_data on a counted tract.ItemBase),
// Reporter (isr_create and fpg_report; the caller may add a border, a
// pattern spectrum and the sink), Mine (isr_setup and fpg_mine). Mine (the
// function) runs the whole pipeline.
//
// # Upstream to Go
//
//	struct _fpgrowth                  Miner (parameters), miner (mining state)
//	fpg_create                        New, Config.validate, Config.normalize
//	fpg_data                          Miner.Data, computeThresholds
//	fpg_report                        Miner.Reporter
//	fpg_mine, fpgvars[]               Miner.Mine, mineItemSets
//	sig_aborted (FPG_ABORT)           miner.aborted (ctx.Done)
//	FPNODE, FPHEAD, FPTREE            fpNode, fpHead, fpTree (fpArena)
//	add_simple, proj_simple           miner.addSimple, miner.projSimple
//	rec_simple                        miner.recSimple
//	add_smp16, proj_smp16, rec_smp16  miner.addSmp16, miner.projSmp16, miner.recSmp16
//	fpg_simple                        miner.mineSimple
//	CSNODE, CSHEAD, CSTREE            csNode, csHead, csTree (csArena)
//	add_cmplx, proj_cmplx             miner.addCmplx, miner.projCmplx
//	add_reord, proj_reord             miner.addReord, miner.projReord
//	rec_cmplx, fpg_cmplx              miner.recCmplx, miner.mineComplex
//	rec_single, fpg_single            miner.recSingle (+ projectSingle), miner.mineSingle
//	TDNODE, TDTREE                    tdNode, tdTree (tdArena)
//	add_topdn, getsupp, merge, copy   miner.addTopDown, getSupp, tdMerge, tdCopy
//	rec_topdn, fpg_topdn              miner.recTopDown, miner.mineTopDown
//	i2s_sort = i2i_qsort (arrays.c)   idxQsort, idxQrec
//	FIM16, m16_create                 machine, m16, newMachine
//	m16_setmap, m16_add               machine.setMap, machine.add, m16.add
//	project, count, clear, filter     m16.project, count, clear, filter
//	rec_pos, rec_neg, m16_mine        m16.recPos, machine.recNeg, machine.mine
//	hibit, prjms                      hibit, prjms
//	fpg_tree, rec_tree                miner.fillTree, miner.recTree (+ relink)
//	fpg_mine (rules/evaluation path)  Miner.mineTree (internal/istree)
//	FPG_ABORT -1 return               ctx.Err()
//	memsys allocation failure         ErrTooLarge (int32 node arena overflow)
//
// The rules path (association rules, item sets with a rule-based
// evaluation measure) fills an item set tree (internal/istree) with
// rec_tree and lets it evaluate, filter and report; Miner.SetProgress
// observes its phases.
//
// Not ported here: the data modes FPG_NORECODE,
// FPG_NOFILTER, FPG_NOSORT, FPG_NOREDUCE, FPG_NOPACK (unreachable from the
// fpgrowth program and partly broken upstream, core B8), the debug
// printers, VISITED counters and FPG_ZLIB. Pre-formatting of supports
// (isr_prefmt) belongs to text output.
//
// # Memory
//
// Trees are slices of value structs addressed by int32 indices. Each
// variant has one node arena, presized from Bag.Extent; a projection
// allocates its nodes after a mark and releases them when its recursion
// returns (upstream keeps them until the end of the level, which is not
// observable). Scratch buffers and projection headers are reused across
// recursion levels.
//
// # Deviations from upstream
//
// Each is marked "Deviation from upstream:" in the code and has a
// regression test (and golden cases listed in
// testdata/golden/deviations.txt):
//
//   - B1: fpg_topdn copies the old -> new item map instead of the
//     new -> old one; -At crashes or prints wrong items. Fixed.
//   - B2: the minimum support (and rule body support) is at least 1.
//   - B3: generators are mined without the zmin transaction filter.
//   - B5: closed and maximal item sets with a maximum size are searched
//     only as deep as a reportable set can reach, and every set the
//     closed/maximal repository accepts is checked against all items of
//     the database (extChecker), so only closed/maximal sets are reported
//     (docs/m5-investigation.md).
//   - B9: threshold conversions saturate instead of being undefined.
//   - rules-eval.md B1: for association rules the measure code
//     EvalLogRatio (-eb) selects ruleval.Imbalance.
//   - rules-eval.md B2: on the rules path the evaluation measure is set
//     before the evaluation filter, which runs for every prune value.
//   - A node arena that would exceed 2^31-1 nodes makes Mine return
//     ErrTooLarge instead of crashing; so do the item set tree and the
//     closed/maximal repository of the reporter (their own ErrTooLarge
//     errors are mapped to this package's).
//
// Upstream bug B4 (and M9, its 16-items machine variant: -tm -x with -As
// or -Ad loses the maximal set made of items that occur in every
// transaction, because the machine registers them as perfect extensions of
// the empty set even with -x) is fixed in internal/report; this package
// only has regression tests for it. Negative weights are rejected by
// internal/tract (B7).
package fpg
