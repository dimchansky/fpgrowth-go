package difftest

import (
	"math"
	"math/bits"
	"slices"
	"strings"
)

// caseDeviations returns the documented deviations (docs/differences.md)
// that a case can trigger. The predicates are necessary conditions of the
// upstream behavior, derived from the data where possible; they may still
// over-approximate (a listed deviation need not change the output), but
// must not miss a case where upstream behaves differently by design.
//
// A flagged deviation does not by itself skip the comparison with C:
// evaluate checks C's result against the brute force as well, and if C
// passes, C and Go are compared in full (records, order, stdout, stderr,
// spectrum); only the output channels in devExempt are excused. The case
// is deviation-verified only when C fails the brute-force check (or
// crashes) or differs in an excused channel only.
func caseDeviations(c *tcase, db *bfDB) []string {
	var devs []string
	add := func(id string, cond bool) {
		if cond {
			devs = append(devs, id)
		}
	}
	total := db.total
	smin := absMinSupp(c.sArg, total)
	setTarget := !c.isRules()
	closedMax := c.target == 'c' || c.target == 'm'
	direct := !c.treePath()

	// M1: -At copies the old->new item map instead of new->old, which
	// matters only if fpg_topdn eliminates an item at the top level
	// (infrequent after the zmin filter, or a perfect extension) while
	// another item is kept. Whether the eliminated item's code lies below
	// the last kept code depends on the (unstable) frequency sort, so that
	// part is not modeled. -tg uses -Ad instead.
	add("M1", direct && c.algo == 't' && c.target != 'g' && topLevelElimination(c, db, smin))
	// M2: -s0 (absolute minimum support 0). For rules the item filter
	// uses the support of body and head, ceil(smin*conf), which is 0 for
	// -c0: upstream then keeps items of frequency 0 (absent items of the
	// appearance file, zero-weight records), which shows in the log's
	// item count, or reports nothing instead of "no (frequent) items".
	add("M2", smin <= 0 || (c.isRules() && !c.orig && ruleSetMinSupp(smin, c.conf/100) <= 0))
	// M3: generators with -m >= 2.
	add("M3", c.target == 'g' && c.zmin >= 2)
	// M4: maximal sets when every frequent item is a top-level perfect
	// extension (of the transactions kept by the zmin filter).
	add("M4", c.target == 'm' && allItemsPerfect(c, db, smin))
	// M5: closed/maximal sets with -n: the search stops at depth zmax+1,
	// which matters only if a frequent set of that size exists.
	add("M5", closedMax && c.zmax >= 0 && db.hasFrequentOfSize(smin, c.zmax+1))
	// M9: maximal sets with -x and the 16-items machine (-As/-Ad).
	add("M9", direct && c.target == 'm' && c.noPerfect && (c.algo == 's' || c.algo == 'd') && c.pack != 0)
	// R1: -tr -eb is the imbalance ratio.
	add("R1", c.isRules() && c.eval == 'b')
	// R2/R10: -p prunes item sets with a rule-based measure.
	add("R2", setTarget && c.ruleEval() && c.hasPrune)
	// R3: head-only items (appearance file) for rules.
	add("R3", c.isRules() && c.appear != nil && hasHeadOnly(c))
	// R8: -S for rules without -o.
	add("R8", c.isRules() && c.hasSMax && !c.orig)
	// R11: -m beyond the number of items on the item set tree path.
	add("R11", c.treePath() && c.zmin > db.numFrequent(smin))
	// O3: upstream's fast output path is active (fastchk, report.c:135-151:
	// an output file, format " (%a)", zmin <= 1, no zmax, no border, no
	// -eb, default -k; isr_setrepo is never called by fpgrowth) and it
	// makes a difference: it prints a spurious empty set when there are
	// top-level perfect extensions (an item in every transaction; -x does
	// not prevent them where the 16-items machine runs at the top level),
	// and it writes a wrong pattern spectrum.
	fast := setTarget && !c.countOnly && c.info.String() == " (%a)" && c.sep == " " &&
		c.zmin <= 1 && c.zmax < 0 && len(c.border) == 0 && c.eval != 'b' &&
		(!closedMax || c.treePath())
	add("O3", fast && (c.spectrum || db.hasUniversalItem()))
	// O4: count-only mode ignores -n for closed/maximal counts, which
	// matters only if a frequent set larger than zmax exists.
	add("O4", c.countOnly && closedMax && c.zmax >= 0 && db.hasFrequentOfSize(smin, c.zmax+1))
	// Value-level measure deviations (R4-R9) are excused per record, also
	// in count-only mode, where evaluate reruns both programs with an
	// output file to see the records (countByRerun).
	return devs
}

// devExempt lists, per deviation, the kinds of differences between C and
// Go that the deviation explains even when C's records pass the
// brute-force check: M2 changes the log (item count) and, through the
// codes of the zero-frequency items it keeps, the order of the rules; M3
// changes the logged transaction count and the order of the sets (the
// port mines generators without upstream's zmin transaction filter, so
// the trees hold the short transactions too and the conditional supports
// by which the projections reorder their items differ, and with them the
// order in which the sets are found); O3 changes the pattern spectrum
// (and its logged signature count), R1 the rule values, R2 the pruned
// sets and O4 the count-only counts.
var devExempt = map[string][]diffKind{
	"M2": {diffStderr, diffOrder},
	"M3": {diffStderr, diffOrder},
	"O3": {diffSpectrum},
	"R1": {diffValue, diffRecords},
	"R2": {diffRecords},
	"O4": {diffCount, diffRecords},
}

// exempted reports whether a difference of kind k is explained by one of
// the deviations. With an evaluation measure the brute force checks only
// that the records are a subset with exact supports, which a C result
// missing records also passes; then a record-level deviation (all but
// O3, whose spurious empty set fails the check) excuses membership.
func exempted(c *tcase, devs []string, k diffKind) bool {
	for _, d := range devs {
		if slices.Contains(devExempt[d], k) {
			return true
		}
		if k == diffRecords && c.eval != 'x' && d != "O3" {
			return true
		}
	}
	return false
}

// topLevelElimination reports whether fpg_topdn's top-level item map
// eliminates an item that has a code (frequency at least smin before the
// zmin filter) while it keeps another one: the item is infrequent after
// the zmin filter (tbg_filter empties transactions with fewer than zmin
// coded items) or a perfect extension (frequency equal to the total
// weight, unless -x).
func topLevelElimination(c *tcase, db *bfDB, smin int64) bool {
	smin = max(smin, 1)
	coded := map[string]bool{}
	for i, it := range db.items { // db.items honour the -R selection
		if db.supp[1<<i] >= smin {
			coded[it] = true
		}
	}
	freq := map[string]int64{}
	for _, tx := range c.txs {
		seen := map[string]bool{}
		for _, it := range tx.items {
			if coded[it] {
				seen[it] = true
			}
		}
		if len(seen) < c.zmin {
			continue
		}
		for it := range seen {
			freq[it] += tx.weight
		}
	}
	kept, elim := 0, 0
	for it := range coded {
		switch f := freq[it]; {
		case f < smin, !c.noPerfect && f >= db.total:
			elim++
		default:
			kept++
		}
	}
	return kept > 0 && elim > 0
}

// noUsableItems reports whether no item has a positive support (and, for
// rules, may appear in a rule): with a minimum support of at least 1 the
// port must then fail with "no (frequent) items found".
func noUsableItems(c *tcase, db *bfDB) bool {
	for i, it := range db.items {
		if db.supp[1<<i] < 1 {
			continue
		}
		if c.isRules() {
			a, ok := c.appVals[it]
			if !ok {
				a = c.defAppVal
			}
			if c.appear != nil && a == appNone {
				continue
			}
		}
		return false
	}
	return true
}

// hasHeadOnly reports whether the appearance file makes any item head-only.
func hasHeadOnly(c *tcase) bool {
	if c.defAppVal == appHead {
		return true
	}
	for _, a := range c.appVals {
		if a == appHead {
			return true
		}
	}
	return false
}

// allItemsPerfect reports whether every frequent item occurs in every
// transaction that keeps at least max(zmin, 1) frequent items.
func allItemsPerfect(c *tcase, db *bfDB, smin int64) bool {
	smin = max(smin, 1)
	freq := map[string]bool{}
	for i, it := range db.items {
		if db.supp[1<<i] >= smin {
			freq[it] = true
		}
	}
	if len(freq) == 0 {
		return false
	}
	sel := c.selectionSet()
	zmin := max(c.zmin, 1)
	var wTotal int64
	cnt := map[string]int64{}
	for _, tx := range c.txs {
		seen := map[string]bool{}
		for _, it := range tx.items {
			if freq[it] && (sel == nil || sel[it]) {
				seen[it] = true
			}
		}
		if len(seen) < zmin {
			continue
		}
		wTotal += tx.weight
		for it := range seen {
			cnt[it] += tx.weight
		}
	}
	for it := range freq {
		if cnt[it] != wTotal {
			return false
		}
	}
	return true
}

// valueDev describes a value-level deviation of an evaluation measure
// (R4-R7, R9, N1): the port's value differs from upstream's in a known
// direction for the rules that rulePred selects (nil: any rule).
type valueDev struct {
	id       string
	goGE     bool // Go's value >= C's
	goLE     bool // Go's value <= C's
	rulePred func(supp, body, head, base int64) bool
	// slack, if not nil, replaces the direction test: the values may
	// differ by up to slack of the rule (the largest over the rules of an
	// item set), a bound on upstream's known error (N1).
	slack func(supp, body, head, base int64) float64
}

// measureDeviation returns the value-level deviation of the case's
// evaluation measure, or nil.
func measureDeviation(c *tcase) *valueDev {
	if !c.ruleEval() {
		return nil
	}
	switch c.eval {
	case 'v': // R4: conviction +Inf instead of 0 for confidence 1
		return &valueDev{id: "R4", goGE: true, rulePred: func(s, b, _, _ int64) bool { return b > 0 && s >= b }}
	case 'z': // R5: certainty factor 0 instead of NaN if the head is in every transaction
		return &valueDev{id: "R5", goGE: true, goLE: true, rulePred: func(_, _, h, n int64) bool { return h == n }}
	case 'f', 'm', 'h', 's': // R6, R9: Fisher ties decided exactly, p-values clamped
		return &valueDev{id: "R6/R9", goGE: true}
	case 'g': // N1 (ALG-6): upstream's G statistic has an absolute error
		return &valueDev{id: "N1", slack: alg6Slack}
	case 'i': // R7: information clamped at 0 (negative for independent tables)
		return &valueDev{id: "R7", goGE: true, rulePred: func(s, b, h, n int64) bool { return s*n == h*b }}
	case 'y': // R9 (B10): Yates' correction clamped at 0
		return &valueDev{id: "R9", goLE: true, rulePred: yatesClamped}
	case 't':
		return &valueDev{id: "R9", goGE: true, rulePred: yatesClamped}
	}
	return nil
}

// alg6Slack bounds the error of upstream's G-test p-value (audit ALG-6,
// docs/differences.md N1; the bound of the #15 oracle's DefectALG6): its
// information (ln n + Σ t·ln(t/(r·k))/n) has an absolute error of at most
// δI = 16·2^-52·max(1, ln n) nats, so its statistic G = 2·n·I is off by
// δG = 2·n·δI, which the chi²(1) tail turns into a p-value error of at
// most √(6·δG/π) (the tail is 1 - √(2G/π) + O(G^1.5) near 0, where the
// error is largest). The port computes the exact value (within 2^-36
// relative), so the difference is upstream's error.
func alg6Slack(_, _, _, n int64) float64 {
	dg := 2 * float64(n) * 16 * dblEpsilon * max(1, math.Log(float64(n)))
	return math.Sqrt(6 * dg / math.Pi)
}

// yatesClamped reports whether |head*body - supp*base| < base/2, where
// upstream squares a negative corrected difference.
func yatesClamped(s, b, h, n int64) bool {
	d := h*b - s*n // |d| < 2^62 for 31-bit supports
	if d < 0 {
		d = -d
	}
	return 2*d < n
}

// minimizing reports whether the case's measure must not exceed the
// threshold.
func minimizing(c *tcase) bool { return strings.IndexByte(minMeasures, c.eval) >= 0 }

// affects reports whether the deviation applies to a record: a rule
// itself, or an item set any of whose rules S\{h} -> h (supports from
// the brute force).
func (d *valueDev) affects(c *tcase, db *bfDB, r record) bool {
	if d.rulePred == nil {
		return true
	}
	if c.isRules() {
		return d.rulePred(db.support(append(slices.Clone(r.items), r.head)), db.support(r.items),
			db.support([]string{r.head}), db.total)
	}
	m, ok := db.mask(r.items)
	if !ok || bits.OnesCount64(m) < 2 {
		return false
	}
	for x := m; x != 0; x &= x - 1 {
		bit := x & -x
		if d.rulePred(db.supp[m], db.supp[m&^bit], db.supp[bit], db.total) {
			return true
		}
	}
	return false
}

// allowsValue reports whether differing measure values cv (C) and gv (Go)
// of a record are explained by the deviation.
func (d *valueDev) allowsValue(c *tcase, db *bfDB, r record, cv, gv float64, percent bool) bool {
	if !d.affects(c, db, r) {
		return false
	}
	if math.IsNaN(cv) || math.IsNaN(gv) {
		return d.goGE && d.goLE // R5 only
	}
	slack := relTol*max(math.Abs(cv), math.Abs(gv)) + absTol
	if d.slack != nil {
		slack += d.maxSlack(c, db, r)
	}
	if percent {
		slack *= 100
	}
	if d.slack != nil {
		return math.Abs(cv-gv) <= slack
	}
	return (!d.goGE || gv >= cv-slack) && (!d.goLE || gv <= cv+slack)
}

// maxSlack returns the largest slack of the rules of a record (the rule
// itself, or the rules S\{h} -> h of an item set S).
func (d *valueDev) maxSlack(c *tcase, db *bfDB, r record) float64 {
	if c.isRules() {
		return d.slack(db.support(append(slices.Clone(r.items), r.head)), db.support(r.items),
			db.support([]string{r.head}), db.total)
	}
	m, ok := db.mask(r.items)
	if !ok {
		return 0
	}
	var s float64
	for x := m; x != 0; x &= x - 1 {
		bit := x & -x
		s = max(s, d.slack(db.supp[m], db.supp[m&^bit], db.supp[bit], db.total))
	}
	return s
}

// allowsMissing reports whether a record that only one side reported (inGo
// tells which) is explained by the deviation: the side whose value moves
// toward acceptance may report more.
func (d *valueDev) allowsMissing(c *tcase, db *bfDB, r record, inGo bool) bool {
	if !d.affects(c, db, r) {
		return false
	}
	if d.slack != nil { // a value within the slack of the threshold
		return false
	}
	if d.goGE && d.goLE {
		return true
	}
	// a larger value is accepted more easily by a (+) measure
	goAcceptsMore := d.goGE != minimizing(c)
	return inGo == goAcceptsMore
}
