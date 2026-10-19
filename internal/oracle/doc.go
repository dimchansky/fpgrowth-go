// Package oracle is an independent brute-force reference for testing the
// FP-growth port and, through differential tests, the upstream C program.
//
// Nothing here reuses the port's mining or measure code: the package
// enumerates every subset of a small item universe (at most MaxItems
// items) as a uint64 bit mask, computes exact weighted supports with a
// superset-sum transform, applies the definitions below literally, and
// evaluates rules and item sets with its own reference measures (see
// "Reference measures"; callers may also pass the implementation's
// measure functions, for bit-identical comparisons). It is meant for
// tests only and is fast enough for thousands of small random cases.
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
// A Measure has a direction, a threshold and either a rule evaluation
// function with the signature of upstream's ruleval functions (Func) or
// an independent reference measure (Ref). Rules are filtered with
// upstream's reject-if-less test, item sets with the
// accept-if-greater-or-equal test of the item set tree, and item sets are
// evaluated by aggregating the measure over the rules S\{h} -> h
// (see SetEval). LogRatioEval evaluates item sets by upstream's item set
// measure -eb (LogSupportRatio).
//
// # Reference measures
//
// RefMeasures holds an independent reference for each of the 25 rule
// measures, and LogRatioValue one for -eb. They are written from the
// mathematical definitions; this package does not import the
// implementation's formula packages (internal/ruleval, internal/istree,
// internal/report, internal/fpg; TestReferenceImportGraph checks the
// import graph) and does not reuse their formulas. Products and quotients
// are exact (math/big), logarithms are computed with 128-bit big.Float
// series, the mutual information is computed with a precision that
// covers its one bounded cancellation, the chi²-type p-values use
// math.Erfc, and the Fisher tests are exact
// hypergeometric sums over big integer weights (up to FisherMaxTables
// tables; ErrOutOfScope beyond). Each Value is labelled Exact or
// Convention; conventions cover the cases where a definition is undefined
// (ConvUndefined) and the upstream or port conventions ALG-5, R4, R5 and
// R9b (see the Conv* constants), and the worst value given to sets that
// cannot be evaluated and to rules invalidated by -i (ConvWorst). The
// port fixes R6, R7 and R9a coincide with the exact definition and give
// exact values. The first rule of AggFirst follows the implementation's
// item order (Order; docs/differences.md P6).
//
// Reference values carry a tolerance (Value.Tol): the error bound the
// implementation documents for the measure (see ratTau and the constants
// after it): 2^-44 relative for products and quotients, 2^-44·max(1, |v|)
// for the differences and Importance, 2^-49 relative for chi² and Yates,
// 2^-47 relative for the information, 2^-40 absolute for the certainty
// factor, 2^-36 scaled by the conditioning of the chi² tail for p-values
// and 2^-36 relative for the Fisher tests, for every valid table up to a
// total weight of 2^63 - 1. With Measure.Ref, rules and item sets whose
// value lies within the tolerance of the threshold are reported with
// Optional set, and DiffRules/DiffSets accept them missing; Eval values
// must lie within EvalTol. The -i test is exact in integers in both
// modes.
//
// # Helpers
//
// AbsMinSupp, AbsMaxSupp, RuleSetMinSupp and AbsBorder convert upstream's
// threshold arguments under the threshold contract of docs/differences.md
// (N2): upstream's 1±DBL_EPSILON tolerance evaluated exactly (math/big)
// and capped below one unit of support; FracMinSupp, FracMaxSupp,
// MeetsMin and MeetsMax state it for fractions. Generate produces deterministic random
// datasets and WriteTable serializes them in upstream's default input
// format. DiffSets and DiffRules compare results order-independently and
// describe the first differences.
package oracle
