package difftest

import (
	"math"
	"math/bits"
	"slices"
	"strings"
)

// caseDeviations returns the documented deviations (docs/differences.md)
// that a case can trigger. For these cases the Go output is checked
// against the brute force instead of the C output. The predicates may
// over-approximate (a listed deviation need not change the output) but
// must not miss a case where upstream behaves differently by design.
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

	// M1: -At item map; triggered by any item dropped at the top level
	// (perfect extension, zmin filter). -tg uses -Ad instead.
	add("M1", direct && c.algo == 't' && c.target != 'g')
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
	// M5: closed/maximal sets with -n.
	add("M5", closedMax && c.zmax >= 0)
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
	// O3: upstream's fast output path (format " (%a)", zmin <= 1, no zmax,
	// no border, no -eb, no repository) prints a spurious empty set and a
	// wrong spectrum when there are top-level perfect extensions.
	add("O3", setTarget && !c.countOnly && c.info.String() == " (%a)" && c.sep == " " &&
		c.zmin <= 1 && c.zmax < 0 && len(c.border) == 0 && c.eval != 'b' &&
		(!closedMax || c.treePath()))
	// O4: count-only mode ignores -n for closed/maximal counts.
	add("O4", c.countOnly && closedMax && c.zmax >= 0)
	// Value-level measure deviations are excused per record; without an
	// output file only the counts can be compared.
	if d := measureDeviation(c); d != nil && c.countOnly {
		add(d.id, true)
	}
	return devs
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
// (R4-R7, R9): the port's value differs from upstream's in a known
// direction for the rules that rulePred selects (nil: any rule).
type valueDev struct {
	id       string
	goGE     bool // Go's value >= C's
	goLE     bool // Go's value <= C's
	rulePred func(supp, body, head, base int64) bool
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
	case 'i': // R7: information clamped at 0 (negative for independent tables)
		return &valueDev{id: "R7", goGE: true, rulePred: func(s, b, h, n int64) bool { return s*n == h*b }}
	case 'y': // R9 (B10): Yates' correction clamped at 0
		return &valueDev{id: "R9", goLE: true, rulePred: yatesClamped}
	case 't':
		return &valueDev{id: "R9", goGE: true, rulePred: yatesClamped}
	}
	return nil
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
	if percent {
		slack *= 100
	}
	return (!d.goGE || gv >= cv-slack) && (!d.goLE || gv <= cv+slack)
}

// allowsMissing reports whether a record that only one side reported (inGo
// tells which) is explained by the deviation: the side whose value moves
// toward acceptance may report more.
func (d *valueDev) allowsMissing(c *tcase, db *bfDB, r record, inGo bool) bool {
	if !d.affects(c, db, r) {
		return false
	}
	if d.goGE && d.goLE {
		return true
	}
	// a larger value is accepted more easily by a (+) measure
	goAcceptsMore := d.goGE != minimizing(c)
	return inGo == goAcceptsMore
}
