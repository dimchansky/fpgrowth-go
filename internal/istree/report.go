package istree

import (
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Aggregation selects how the values of the rules formed from an item set
// are combined into the set's evaluation (upstream IST_FIRST ... IST_AVG,
// option -a). The values are upstream's.
type Aggregation uint8

// Aggregation modes.
const (
	// AggFirst uses the value of the first rule only (-ax): the rule whose
	// head is the item with the smallest code.
	AggFirst Aggregation = iota
	// AggMin takes the minimum (-am), with C's "if (val < agg)" update.
	AggMin
	// AggMax takes the maximum (-an), with C's "if (val > agg)" update.
	AggMax
	// AggAvg takes the average (-aa).
	AggAvg
)

// Eval configures the additional evaluation measure (ist_seteval).
type Eval struct {
	// Measure is the rule measure. None, or an invalid value, disables the
	// evaluation. For rules (-tr) the fpgrowth letter 'b' must select
	// ruleval.Imbalance (deviation for upstream bug B1, where 'b' always
	// selects the item set measure and rules are not evaluated); for item
	// sets 'b' is report.LogRatio and does not use the tree.
	Measure ruleval.Measure
	// Agg is the aggregation mode for item sets; an unknown value acts as
	// AggFirst.
	Agg Aggregation
	// Invalidate (-i) replaces the value of a rule by the worst value
	// (1 for (-) measures, 0 for (+) measures) unless
	// float64(supp)*float64(base) > float64(head)*float64(body).
	Invalidate bool
	// Threshold is the evaluation threshold (-d/100).
	Threshold float64
	// NoFilter computes the evaluation of every reported item set and
	// rule without filtering by Threshold (an extension: upstream always
	// filters). Filter must not be called then.
	NoFilter bool
}

// SetEval is a port of ist_seteval (without the pruning level, which only
// apriori's ist_commit reads; see Filter).
func (t *Tree) SetEval(e Eval) {
	m := e.Measure
	if !m.IsValid() || m == ruleval.None {
		m = ruleval.None
	}
	t.eval = m
	t.fn, t.afn = nil, nil
	if m != ruleval.None {
		t.fn, t.afn = m.Func(), m.AbortFunc()
	}
	t.agg = AggFirst
	if e.Agg > AggFirst && e.Agg <= AggAvg {
		t.agg = e.Agg
	}
	t.isInvBxs = e.Invalidate
	t.dir = float64(m.Dir())
	t.thresh = t.dir * e.Threshold
	t.isNoFilter = e.NoFilter
}

// passes reports whether the evaluation value v of an item set passes the
// threshold (accept if at least as good, so NaN fails).
func (t *Tree) passes(v float64) bool { return t.isNoFilter || v*t.dir >= t.thresh }

// worst is the constant value of the empty set and the singletons, and the
// value of invalidated rules: (dir < 0) ? 1 : 0.
func (t *Tree) worst() float64 {
	if t.dir < 0 {
		return 1
	}
	return 0
}

// value computes the measure of one rule with the -i invalidation. With
// an abort function (SetAbort), a measure of non-constant cost is
// evaluated by its abortable variant; an abort error is latched in t.err
// (returned by the next tick) and the value is NaN. Callers check t.err
// after evaluating.
func (t *Tree) value(supp, body, head, base tract.Supp) float64 {
	if !t.isInvBxs || float64(supp)*float64(base) > float64(head)*float64(body) {
		if t.afn == nil || t.abort == nil {
			return t.fn(supp, body, head, base)
		}
		if t.err != nil {
			return math.NaN()
		}
		v, err := t.afn(supp, body, head, base, t.abort)
		if err != nil {
			t.err = err
		}
		return v
	}
	return t.worst()
}

// evaluate is a port of evaluate(): the aggregated evaluation of the set
// represented by counter index of node ni (index -1: the empty set).
//
// The first rule has the deepest item (the smallest code) as its head; the
// others follow by walking up the path, i.e. in increasing head code.
func (t *Tree) evaluate(ni int32, index int32) float64 {
	if t.eval == ruleval.None {
		return 0
	}
	if index < 0 {
		return t.worst() // empty set
	}
	nd := &t.nodes[ni]
	cur := nd.parent
	if cur < 0 {
		return t.worst() // single item
	}
	item := t.itemAt(nd, index)
	supp := count(t.cnts[nd.cnts+index])
	base := count(t.wgt)
	head := count(t.cnts[item]) // root counters: item frequencies
	pn := &t.nodes[cur]
	body := count(t.cnts[pn.cnts+t.index(pn, nd.item)])
	agg := t.value(supp, body, head, base)
	if t.agg == AggFirst {
		return agg
	}
	end := len(t.buf)
	pos := end - 1
	t.buf[pos] = item
	n := 1
	item = nd.item
	for c := cur; c >= 0; c = t.nodes[c].parent {
		head = count(t.cnts[item])
		body = count(t.getsupp(c, t.buf[pos:end]))
		val := t.value(supp, body, head, base)
		switch t.agg {
		case AggMin:
			if val < agg {
				agg = val
			}
		case AggMax:
			if val > agg {
				agg = val
			}
		default:
			agg += val
		}
		pos--
		t.buf[pos] = item
		n++
		item = t.nodes[c].item
	}
	if t.agg == AggAvg {
		agg /= float64(n)
	}
	return agg
}

// Init is a port of ist_init(ist, 0): it prepares the extraction (the level
// lists that Filter and CloMax also build on demand).
//
// Deviation from upstream: ist_init stores lvls[zmin-1] as the node of the
// empty-set evaluation and reads past the level array when zmin exceeds
// the number of items + 1 (reference-build finding). The value is never
// dereferenced (the empty set evaluates to a constant), so here no level is
// read at all; Report evaluates the empty set with index -1 directly.
func (t *Tree) Init() {
	if !t.isLvl {
		t.makelvls()
	}
	t.evNode, t.evIndex = 0, -1
}

// Report is a port of ist_report with order 0 (the fpgrowth program's
// recursive reporting): for target report.TargetRules it reports the
// association rules, otherwise the item sets that pass the evaluation and
// are not marked by Filter or CloMax.
//
// The reporter must be configured like fpg_report does for this path
// (report.ModeNoFilter; the tree does closed/maximal/generator filtering).
// For rules, the reporter's maximum support must be report.MaxSupp: the
// tree applies Params.MaxSupp itself to the rule support (B8).
//
// Item sets are reported in pre-order (a set before its supersets), every
// node's items in increasing code order, with the items of a set added in
// decreasing code order. Rules are reported in post-order (all rules of the
// supersets of a set before the rules of the set); within a set the heads
// go from the smallest code up, and the body lists the other items in
// decreasing code order. The order depends only on the item codes and the
// stored sets, not on the order in which they were inserted.
//
// Report also stops with the error of the abort function (SetAbort).
//
// Deviation from upstream: an error of the sink aborts the whole report and
// is returned; upstream ignores the result of its recursive calls, so a
// write error below the top level was lost.
func (t *Tree) Report(rep *report.Reporter, target report.Target) error {
	t.evNode, t.evIndex = 0, -1
	if target&report.TargetRules != 0 {
		return t.rules(rep, 0)
	}
	return t.isets(rep, 0, t.wgt)
}

// isets is a port of isets() without perfect extensions (IST_PERFECT is
// never set on fpgrowth's path).
func (t *Tree) isets(rep *report.Reporter, ni int32, supp tract.Supp) error {
	if !isSkip(supp) {
		v := t.evaluate(t.evNode, t.evIndex)
		if t.err != nil {
			return t.err
		}
		if t.passes(v) {
			if err := rep.ReportEval(v); err != nil {
				return err
			}
		}
	}
	nd := &t.nodes[ni]
	cnts := t.counters(nd)
	for i := range nd.size {
		if err := t.tick(); err != nil {
			return err
		}
		s := count(cnts[i])
		if s < t.smin {
			continue
		}
		t.evNode, t.evIndex = ni, i
		k := t.itemAt(nd, i)
		if !rep.Add(k, s) {
			continue // only with a filtering reporter (not ModeNoFilter)
		}
		raw := cnts[i]
		if c := t.child(nd, k); c >= 0 {
			if err := t.isets(rep, c, raw); err != nil {
				return err
			}
		} else if !isSkip(raw) {
			v := t.evaluate(ni, i)
			if t.err != nil {
				return t.err
			}
			if t.passes(v) {
				if err := rep.ReportEval(v); err != nil {
					return err
				}
			}
		}
		rep.Remove(1)
	}
	return nil
}

// rules is a port of rules().
func (t *Tree) rules(rep *report.Reporter, ni int32) error {
	nd := &t.nodes[ni]
	cnts := t.counters(nd)
	for i := range nd.size {
		if err := t.tick(); err != nil {
			return err
		}
		s := count(cnts[i])
		if s < t.smin {
			continue
		}
		k := t.itemAt(nd, i)
		if !rep.Add(k, s) {
			continue // only with a filtering reporter (not ModeNoFilter)
		}
		if c := t.child(nd, k); c >= 0 {
			if err := t.rules(rep, c); err != nil {
				return err
			}
		}
		if err := t.r4set(rep, ni, i); err != nil {
			return err
		}
		rep.Remove(1)
	}
	return nil
}

// r4set is a port of r4set(): it reports the rules S\{h} -> h of the set S
// represented by counter index of node ni, heads from the deepest item (the
// smallest code) up to the item below the root.
func (t *Tree) r4set(rep *report.Reporter, ni int32, index int32) error {
	nd := &t.nodes[ni]
	base := count(t.wgt)
	supp := count(t.cnts[nd.cnts+index])
	item := t.itemAt(nd, index)
	app := t.appear(item)
	if app == tract.AppearNone || (app == tract.AppearHead && nd.hdonly) {
		return nil // sets with two head-only items yield no rules
	}
	var body tract.Supp
	if nd.parent < 0 {
		body = base
	} else {
		pn := &t.nodes[nd.parent]
		body = count(t.cnts[pn.cnts+t.index(pn, nd.item)])
	}
	// the first rule: the deepest item as head
	if !(app != tract.AppearHead && nd.hdonly) && app&tract.AppearHead != 0 {
		if err := t.rule(rep, item, supp, body, base); err != nil {
			return err
		}
	}
	if app == tract.AppearHead {
		return nil // a head-only item must be the head
	}
	// Deviation from upstream (bug B3, rules-eval.md §3.4): upstream lets
	// any other item of a set with a head-only item be the head, so the
	// head-only item lands in the body ("c <- a d" with "a" head-only).
	// A set whose path has a head-only item (nd.hdonly; the deepest item is
	// not head-only here) has exactly one admissible head: that item.
	hasHeadOnly := nd.hdonly
	end := len(t.buf)
	pos := end - 1
	t.buf[pos] = item
	for c, p := ni, nd.parent; p >= 0; c, p = p, t.nodes[p].parent {
		body = count(t.getsupp(p, t.buf[pos:end]))
		h := t.nodes[c].item
		pos--
		t.buf[pos] = h
		ha := t.appear(h)
		if ha&tract.AppearHead == 0 || (hasHeadOnly && ha != tract.AppearHead) {
			continue
		}
		if err := t.rule(rep, h, supp, body, base); err != nil {
			return err
		}
	}
	return nil
}

// rule checks the rule body -> head of the current set (support supp) and
// reports it if it qualifies: minimum body support, confidence with the
// 1-DBL_EPSILON tolerance, maximum support and the evaluation threshold
// (reject-if-less, so NaN passes like upstream).
func (t *Tree) rule(rep *report.Reporter, head tract.Item, supp, body, base tract.Supp) error {
	if body < t.body || float64(supp) < float64(body)*t.conf {
		return nil
	}
	// Deviation from upstream (bug B8, rules-eval.md §3.1): upstream's
	// reporter compares -S with the support of body and head even under
	// the default body support definition. Here -S applies to the same
	// support as -s: the body support, or with -o the set support.
	rs := body
	if t.isOrig {
		rs = supp
	}
	if rs > t.smax {
		return nil
	}
	hs := count(t.cnts[head])
	var val float64
	if t.fn != nil {
		val = t.value(supp, body, hs, base)
		if t.err != nil {
			return t.err
		}
		if !t.isNoFilter && t.dir*val < t.thresh {
			return nil
		}
	}
	return rep.ReportRule(head, body, hs, val)
}
