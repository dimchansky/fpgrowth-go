// Package oracle is an independent brute-force reference for testing the
// FP-growth port and, through differential tests, the upstream C program.
//
// Nothing here reuses the port's mining code: the package enumerates every
// subset of a small item universe (at most MaxItems items) as a uint64
// bit mask, computes exact weighted supports with a superset-sum transform,
// and applies the definitions below literally. It is meant for tests only
// and is fast enough for thousands of small random cases.
//
// # Data model
//
// A Transaction is a list of item names (duplicates are ignored, as
// upstream ignores repeated items in a record) with a non-negative weight.
// Every transaction counts toward the total weight, also when it is empty
// or when all its items were removed by an item selection. The total weight
// is the support of the empty set. Item sets are canonical ItemSet values
// (sorted by byte order, no duplicates).
//
// # Item set definitions (Mine)
//
// Let smin = max(MinSupp, 1) (the effective minimum support is at least 1).
// A set S is frequent if supp(S) >= smin. Then, for a frequent S:
//
//   - closed: no proper superset has the same support;
//   - maximal: S is not empty and no proper superset is frequent;
//   - generator: no proper subset has the same support (the empty set,
//     whose support is the total weight, counts as a subset, so a single
//     item that occurs with the total weight is not a generator).
//
// Closedness, maximality and the generator property are decided on the
// complete lattice of frequent sets, independent of the size range, the
// maximum support, the support border and the evaluation filter; those
// filters are applied afterwards, to the sets of the chosen target. This
// is upstream's behavior: the closed/maximal repository is updated before
// the support/size/border gates (report.c isr_report), the miner explores
// up to zmax+1 items for closed/maximal sets, generators are stored
// independent of all filters, and with a rule-based evaluation the result
// is "closed ∩ passes evaluation" (docs/upstream/rules-eval.md §4.2).
//
// The empty set is reported only if MinSize is 0, and then as frequent
// (always), closed (if no item occurs with the total weight) or generator
// (always), subject to the support, border and evaluation filters. It is
// never maximal.
//
// # Upstream bugs deliberately not replicated
//
// The oracle implements the definitions above, so differential tests
// against the C reference must expect these confirmed upstream bugs
// (docs/upstream/fpgrowth-core.md §6, rules-eval.md §7):
//
//   - core B2: -s0 reports support-0 sets or crashes; here the effective
//     minimum support is 1.
//   - core B3: generators with zmin >= 2 are missed, because upstream
//     empties transactions shorter than zmin before mining, deflating the
//     supports of smaller subsets; here generator status uses the true
//     supports over all transactions.
//   - core B4: for maximal sets, when every frequent item occurs with the
//     total weight (top-level perfect extensions), upstream reports
//     nothing; here the full set is maximal (a / a b / a c / a with
//     smin 2 gives {a}:4).
//   - core B5: closed/maximal with zmax can report non-closed or
//     non-maximal sets (variant dependent); here closedness and
//     maximality are decided against all supersets regardless of zmax.
//   - core B6 / report.md B3: the fast output path prints a spurious empty
//     set; here the empty set needs MinSize 0.
//   - rules-eval.md B3 (head-only items in bodies) and B8 (-S on body ∪
//     head): see Rules.
//
// When no item is frequent, upstream fails with "no (frequent) items
// found" instead of reporting the empty set; callers must special-case it.
//
// # Association rules (Rules)
//
// Rules have a single head item h and a body B (possibly empty) with
// supp(B ∪ {h}) >= 1; the body support of an empty body is the total
// weight. See RuleParams for the support definitions (default: body
// support; OrigSupp: body ∪ head), the confidence tolerance, the size
// limits (counting body and head), MaxSupp (fixed upstream bug B8: it
// applies to the same support as MinSupp) and item appearances (fixed
// upstream bug B3: head-only items never occur in a body).
//
// # Evaluation measures
//
// A Measure wraps a rule evaluation function with the signature of
// upstream's ruleval functions, a direction and a threshold. Rules are
// filtered with upstream's reject-if-less test, item sets with the
// accept-if-greater-or-equal test of the item set tree, and item sets are
// evaluated by aggregating the measure over the rules S\{h} -> h
// (see SetEval).
//
// # Helpers
//
// AbsMinSupp, AbsMaxSupp, RuleSetMinSupp and AbsBorder replicate
// upstream's threshold conversions. Generate produces deterministic random
// datasets and WriteTable serializes them in upstream's default input
// format. DiffSets and DiffRules compare results order-independently and
// describe the first differences.
package oracle
