// Package istree is the item set tree of FP-growth's rule and evaluation
// path: a port of the parts of Christian Borgelt's apriori/src/istree.c
// (FP-Growth 6.21) that the fpgrowth program reaches. fpgrowth uses it for
// association rules (-tr) and for item sets evaluated with a rule measure
// (-e other than x and b); upstream behavior is specified in
// docs/upstream/rules-eval.md and docs/upstream/fpgrowth-core.md §1.7, §3.7.
//
// # Shape
//
// The tree is always in upstream's IST_REVERSE mode: a node represents the
// items on its path from the root in decreasing code order and holds one
// counter per smaller item that may extend the set. The root counters are
// the item frequencies. Nodes are value structs in a slice addressed by
// int32 indices; their counters, identifier maps and child slots live in
// shared arenas. A node uses a pure counter array (with skip-flagged slots
// for items that are not candidates) or an identifier map exactly when
// upstream does, because ist_setsupp can overwrite a non-candidate slot of a
// pure array and that is observable in the output.
//
// # Protocol (fpg_mine on the tree path)
//
//	t := istree.New(params)                // ist_create(..., IST_REVERSE, ...)
//	t.SetSize(zmin, x)                     // x = zmax (+1 for closed/maximal), capped at the largest transaction
//	fill the tree like rec_tree:           // fpg_tree/rec_tree (internal/fpg)
//	    rec(n): if !t.Expandable(1) { return }
//	            if _, err := t.AddChildren(); err != nil { return err } // only ErrTooLarge
//	            for i := 1; i < n; i++ {
//	                if supp(i) < smin { continue }
//	                if !t.Down(i) { continue }
//	                m := 0
//	                for k := 0; k < i; k++ { if supp(i,k) >= smin { t.SetSupp(k, supp(i,k)); m++ } }
//	                if m > 0 { rec(i) }
//	                t.Up()
//	            }
//	t.SetEval(eval)                        // before Filter (B2)
//	if measure set && prune > ITEM_MIN { t.Filter(prune) }
//	if closed/maximal/generators { t.CloMax(target, measure set && prune > ITEM_MIN) }
//	t.Init()
//	t.Report(reporter, target)
//
// Here supp(i) is the support of the current set extended by item i in the
// conditional FP-tree, smin is fpgrowth's set minimum support (fpg->supp)
// and items are the recoded codes. The reporter must be configured with
// report.ModeNoFilter, the set minimum support and, for rules, an
// unlimited maximum support (Params.MaxSupp carries -S, see B8).
//
// The reported sets and rules, and their order, depend only on the item
// codes and the supports stored, not on the insertion order, provided every
// set's subsets with one item less are stored before AddChildren is called
// on its node (rec_tree's depth-first order and a level-wise order both
// satisfy this; the tests check both).
//
// # Upstream functions
//
//	ist_create                     New
//	ist_setsize, ist_xable         (*Tree).SetSize, Expandable
//	ist_addchn, children, child    (*Tree).AddChildren, children, newChild
//	ist_down, ist_up               (*Tree).Down, Up
//	ist_setsupp, ist_getsupp       (*Tree).SetSupp, Supp
//	ist_depth, ist_height          (*Tree).Depth, Height
//	getsupp, search, ia_bsearch    getsupp, child, bsearch
//	makelvls, reclvls              makelvls, reclvls
//	ist_filter                     (*Tree).Filter
//	ist_clomax, clear              (*Tree).CloMax, clear
//	ist_seteval                    (*Tree).SetEval
//	ist_init                       (*Tree).Init
//	ist_report (order 0)           (*Tree).Report
//	isets, evaluate                isets, evaluate
//	rules, r4set                   rules, r4set
//	SKIP, COUNT, IS2SKIP           skip, count, isSkip
//
// Not ported: the apriori counting functions, ist_commit, ist_prune,
// ist_addlvl, perfect extension pruning (IST_PERFECT is never set by
// fpgrowth) and the size-ordered iterators ist_iset/ist_rule (library API
// with order != 0 only).
//
// # Deviations from upstream
//
// Each is marked "Deviation from upstream:" in the code and has a
// regression test:
//
//   - B3: a head-only item never ends up in a rule body; body-only items
//     are never heads and ignored items appear nowhere.
//   - B8: the maximum support (Params.MaxSupp) applies to the body support,
//     or with Params.OrigSupp to the support of body and head; the
//     reporter's maximum support must be unlimited for rules.
//   - B2: Filter is meant to be called after SetEval and also for positive
//     prune values (strong forward filtering); with that order the
//     generator filter compares single items with the total weight without
//     the skip flag that Filter sets on the empty set.
//   - B1 is the caller's business: for rules the letter 'b' must select
//     ruleval.Imbalance (see Eval.Measure).
//   - ist_init's read past the level array (zmin > number of items) is not
//     replicated.
//   - Sink errors abort the report and are returned (upstream ignores the
//     results of its recursive calls).
//
// Supports are tract.Supp (int64) instead of upstream's 32-bit int; the
// skip flag is the int64 sign bit.
//
// Nodes, counters, identifier maps and child slots are addressed by int32
// indices. AddChildren returns ErrTooLarge (wrapping tract.ErrTooLarge)
// instead of growing an arena beyond math.MaxInt32 entries, where upstream
// would run out of memory; the tree is unusable afterwards.
package istree
