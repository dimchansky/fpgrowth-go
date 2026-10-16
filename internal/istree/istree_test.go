package istree

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// dataset is a transaction database recoded like fpg_data does on the tree
// path, together with the brute-force supports of the oracle.
type dataset struct {
	txs    []oracle.Transaction
	db     *oracle.DB
	names  []string // item names by code
	freqs  []tract.Supp
	appear []tract.Appearance
	total  tract.Supp
	maxLen int // largest transaction size after recoding (tbg_max)
}

// appEntry is one record of an appearance file (-R with -tr).
type appEntry struct {
	item string
	app  tract.Appearance
}

// newDataset recodes txs with minimum support smin and sort order (the -q
// value after fpg_data's REORDER normalization). app, if not nil, is an
// appearance file whose default is AppearBoth; its items are registered in
// file order before the transactions are read, like ib_readapp.
func newDataset(t testing.TB, txs []oracle.Transaction, smin tract.Supp, order int, app []appEntry) *dataset {
	t.Helper()
	ib := tract.NewItemBase[string]()
	if app != nil {
		ib.SetDefaultAppearance(tract.AppearBoth)
		for _, e := range app {
			if err := ib.AddAppearance(e.item, e.app); err != nil {
				t.Fatal(err)
			}
		}
	}
	scan := func(yield func(items []string, weight tract.Supp) bool) error {
		for _, tx := range txs {
			if !yield(tx.Items, tx.Weight) {
				return nil
			}
		}
		return nil
	}
	if err := ib.Count(scan); err != nil {
		t.Fatal(err)
	}
	n := ib.Recode(max(smin, 1), order)
	db, err := oracle.New(txs, oracle.Options{})
	if err != nil {
		t.Fatal(err)
	}
	d := &dataset{txs: txs, db: db, total: ib.TotalWeight()}
	code := map[string]bool{}
	for i := range n {
		it := tract.Item(i)
		d.names = append(d.names, ib.ID(it))
		d.freqs = append(d.freqs, ib.Frequency(it))
		d.appear = append(d.appear, ib.Appearance(it))
		code[ib.ID(it)] = true
	}
	for _, tx := range txs {
		seen := map[string]bool{}
		for _, it := range tx.Items {
			if code[it] {
				seen[it] = true
			}
		}
		d.maxLen = max(d.maxLen, len(seen))
	}
	return d
}

// supp returns the support of the item set with the given codes.
func (d *dataset) supp(codes []tract.Item) tract.Supp {
	names := make([]string, len(codes))
	for i, c := range codes {
		names[i] = d.names[c]
	}
	return d.db.Support(oracle.NewItemSet(names...))
}

// addChildren is AddChildren for test trees, which cannot outgrow their
// arenas; an error is a test bug.
func addChildren(tr *Tree) {
	if _, err := tr.AddChildren(); err != nil {
		panic(err)
	}
}

// fillRecTree fills the tree exactly like fpgrowth's rec_tree: depth first,
// items in increasing code order, every frequent extension k < i of a
// frequent set S ∪ {i} set in the node of S ∪ {i}.
func fillRecTree(tr *Tree, supp func([]tract.Item) tract.Supp, smin tract.Supp) {
	smin = max(smin, 1)
	set := make([]tract.Item, 0, tr.NumItems()+1)
	var rec func(n int)
	rec = func(n int) {
		if !tr.Expandable(1) {
			return
		}
		addChildren(tr)
		for i := 1; i < n; i++ {
			s := append(set, tract.Item(i))
			if supp(s) < smin {
				continue
			}
			if !tr.Down(tract.Item(i)) {
				continue
			}
			m := 0
			for k := range i {
				sk := supp(append(s, tract.Item(k)))
				if sk < smin {
					continue
				}
				tr.SetSupp(tract.Item(k), sk)
				m++
			}
			if m > 0 {
				set = s
				rec(i)
				set = set[:len(set)-1]
			}
			tr.Up()
		}
	}
	rec(tr.NumItems())
}

// fillLevelwise fills the tree level by level (breadth first): all
// supports of a level are set before the next level's nodes are created.
// It stores the same supports as fillRecTree in a different order.
func fillLevelwise(tr *Tree, supp func([]tract.Item) tract.Supp, smin tract.Supp) {
	smin = max(smin, 1)
	// paths of the nodes of the current level, deepest item last
	level := [][]tract.Item{{}}
	for len(level) > 0 {
		var next [][]tract.Item
		for _, path := range level {
			goTo(tr, path)
			if !tr.Expandable(1) {
				continue
			}
			addChildren(tr)
			n := tr.NumItems()
			if len(path) > 0 {
				n = int(path[len(path)-1])
			}
			for i := 1; i < n; i++ {
				goTo(tr, path)
				if !tr.Down(tract.Item(i)) {
					continue
				}
				s := append(slices.Clip(path), tract.Item(i))
				m := 0
				for k := range i {
					sk := supp(append(slices.Clip(s), tract.Item(k)))
					if sk >= smin && tr.SetSupp(tract.Item(k), sk) {
						m++
					}
				}
				if m > 0 && supp(s) >= smin {
					next = append(next, s)
				}
			}
		}
		level = next
	}
	goTo(tr, nil)
}

// goTo moves the insertion cursor to the node of path.
func goTo(tr *Tree, path []tract.Item) {
	for tr.Depth() > 1 {
		tr.Up()
	}
	for _, it := range path {
		if !tr.Down(it) {
			panic(fmt.Sprintf("goTo: no node for %v", path))
		}
	}
}

// collect is a report.Sink that records item sets and rules with names.
type collect struct {
	names []string
	sets  []oracle.Set
	rules []oracle.Rule
	lines []string // report order: items in report order and values
	// ordered holds copies of the reported report.ItemSet and report.Rule
	// values in report order.
	ordered []any
}

func (c *collect) name(it tract.Item) string { return c.names[it] }

func (c *collect) ItemSet(s report.ItemSet) error {
	s.Items = slices.Clone(s.Items)
	c.ordered = append(c.ordered, s)
	names := make([]string, len(s.Items))
	for i, it := range s.Items {
		names[i] = c.name(it)
	}
	c.sets = append(c.sets, oracle.Set{Items: oracle.NewItemSet(names...), Supp: s.Supp, Eval: s.Eval})
	c.lines = append(c.lines, fmt.Sprintf("%s %d %v", strings.Join(names, " "), s.Supp, s.Eval))
	return nil
}

func (c *collect) Rule(r report.Rule) error {
	r.Body = slices.Clone(r.Body)
	c.ordered = append(c.ordered, r)
	body := make([]string, len(r.Body))
	for i, it := range r.Body {
		body[i] = c.name(it)
	}
	c.rules = append(c.rules, oracle.Rule{
		Head: c.name(r.Head), Body: oracle.NewItemSet(body...),
		Supp: r.Supp, BodySupp: r.BodySupp, HeadSupp: r.HeadSupp, Eval: r.Eval,
	})
	c.lines = append(c.lines, fmt.Sprintf("%s <- %s %d %d %d %v",
		c.name(r.Head), strings.Join(body, " "), r.Supp, r.BodySupp, r.HeadSupp, r.Eval))
	return nil
}

// ruleConfig holds the fpgrowth options of a rule run.
type ruleConfig struct {
	bodyMin    tract.Supp // fpg->body
	conf       float64
	orig       bool
	smax       tract.Supp // report.MaxSupp: none
	zmin, zmax int
	eval       Eval
	order      int
	app        []appEntry
}

// setMin is fpg->supp for rules.
func (c ruleConfig) setMin() tract.Supp {
	if c.orig {
		return c.bodyMin
	}
	return oracle.RuleSetMinSupp(c.bodyMin, c.conf)
}

// runRules mines rules like fpg_mine on the tree path and returns the
// sink and the dataset.
func runRules(t testing.TB, txs []oracle.Transaction, c ruleConfig, levelwise bool) (*collect, *dataset) {
	t.Helper()
	smin := max(c.setMin(), 1)
	d := newDataset(t, txs, smin, c.order, c.app)
	sink := &collect{names: d.names}
	if len(d.names) == 0 {
		return sink, d
	}
	tr := New(Params{
		Freqs: d.freqs, Total: d.total, Appear: d.appear,
		MinSupp: c.setMin(), MinBody: c.bodyMin, MinConf: c.conf,
		MaxSupp: c.smax, OrigSupp: c.orig,
	})
	tr.SetSize(c.zmin, min(c.zmax, d.maxLen))
	if levelwise {
		fillLevelwise(tr, d.supp, smin)
	} else {
		fillRecTree(tr, d.supp, smin)
	}
	tr.SetEval(c.eval)
	tr.Init()
	rep := report.New(len(d.names), d.total, d.freqs)
	rep.SetTarget(report.TargetRules, report.ModeNoFilter, -1)
	rep.SetSupport(smin, report.MaxSupp)
	rep.SetSize(c.zmin, c.zmax)
	rep.Setup()
	rep.SetSink(sink)
	if err := tr.Report(rep, report.TargetRules); err != nil {
		t.Fatal(err)
	}
	return sink, d
}

// setConfig holds the fpgrowth options of an item set run with a measure.
type setConfig struct {
	target     report.Target
	smin       tract.Supp
	smax       tract.Supp
	zmin, zmax int
	eval       Eval
	prune      int // math.MinInt32: none
	order      int
}

const noPrune = math.MinInt32

// treeZmax is the tree size limit of fpg_mine.
func (c setConfig) treeZmax(maxLen int) int {
	x := c.zmax
	if c.target&(report.TargetClosed|report.TargetMaximal) != 0 && c.zmax < report.MaxSize {
		x++
	}
	return min(x, maxLen)
}

// runSets mines item sets with a rule measure like fpg_mine with the
// corrected call order (SetEval before Filter, Filter for any prune).
func runSets(t testing.TB, txs []oracle.Transaction, c setConfig, levelwise bool) (*collect, *dataset) {
	t.Helper()
	smin := max(c.smin, 1)
	d := newDataset(t, txs, smin, c.order, nil)
	sink := &collect{names: d.names}
	if len(d.names) == 0 {
		return sink, d
	}
	tr := New(Params{Freqs: d.freqs, Total: d.total, MinSupp: smin, MinBody: smin, MinConf: 1})
	tr.SetSize(c.zmin, c.treeZmax(d.maxLen))
	if levelwise {
		fillLevelwise(tr, d.supp, smin)
	} else {
		fillRecTree(tr, d.supp, smin)
	}
	prune := c.prune
	if c.eval.Measure == ruleval.None {
		prune = noPrune
	}
	tr.SetEval(c.eval)
	if prune != noPrune {
		tr.Filter(prune)
	}
	if c.target&(report.TargetClosed|report.TargetMaximal|report.TargetGenerators) != 0 {
		tr.CloMax(c.target, prune != noPrune)
	}
	tr.Init()
	rep := report.New(len(d.names), d.total, d.freqs)
	rep.SetTarget(c.target, report.ModeNoFilter, -1)
	smax := c.smax
	if smax <= 0 {
		smax = report.MaxSupp
	}
	rep.SetSupport(smin, smax)
	rep.SetSize(c.zmin, c.zmax)
	rep.Setup()
	rep.SetSink(sink)
	if err := tr.Report(rep, c.target); err != nil {
		t.Fatal(err)
	}
	return sink, d
}

// t1 is the reproducer data of rules-eval.md §10.
var t1 = parseTxs("a b c\na d e\nb c d\na b c d\nb c\na b d\nd e\na b c d\nc d e\na b c")

// parseTxs parses one transaction per line, items separated by blanks;
// a trailing ":w" field gives the weight.
func parseTxs(s string) []oracle.Transaction {
	var txs []oracle.Transaction
	for line := range strings.Lines(s) {
		f := strings.Fields(line)
		w := tract.Supp(1)
		if len(f) > 0 && strings.HasPrefix(f[len(f)-1], ":") {
			fmt.Sscan(f[len(f)-1][1:], &w)
			f = f[:len(f)-1]
		}
		txs = append(txs, oracle.Transaction{Items: f, Weight: w})
	}
	return txs
}

func TestNew(t *testing.T) {
	tests := []struct {
		name               string
		smin, body         tract.Supp
		expSmin, expBody   tract.Supp
		smax, expSmax      tract.Supp
		conf, expConfLower float64
	}{
		{name: "zero minimum", smin: 0, body: 0, expSmin: 1, expBody: 1, expSmax: report.MaxSupp, conf: 0.8},
		{name: "body below set minimum", smin: 3, body: 2, expSmin: 3, expBody: 3, smax: 7, expSmax: 7, conf: 1},
		{name: "body above", smin: 2, body: 5, expSmin: 2, expBody: 5, smax: -1, expSmax: report.MaxSupp, conf: 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := New(Params{Freqs: []tract.Supp{3, 2}, Total: 4, MinSupp: tt.smin, MinBody: tt.body, MinConf: tt.conf, MaxSupp: tt.smax})
			if tr.smin != tt.expSmin || tr.body != tt.expBody || tr.smax != tt.expSmax {
				t.Errorf("smin, body, smax = %d, %d, %d; want %d, %d, %d", tr.smin, tr.body, tr.smax, tt.expSmin, tt.expBody, tt.expSmax)
			}
			if tr.conf != tt.conf*(1-0x1p-52) || (tt.conf > 0 && tr.conf >= tt.conf) {
				t.Errorf("conf = %v for %v", tr.conf, tt.conf)
			}
			if tr.NumItems() != 2 || tr.Height() != 1 || tr.Depth() != 1 {
				t.Errorf("items, height, depth = %d, %d, %d", tr.NumItems(), tr.Height(), tr.Depth())
			}
		})
	}
	defer func() {
		if recover() == nil {
			t.Error("New with mismatched appearances did not panic")
		}
	}()
	New(Params{Freqs: []tract.Supp{1}, Appear: []tract.Appearance{1, 2}})
}

func TestTree_Insertion(t *testing.T) {
	// items 0..3 with frequencies; the pairs {3,0} and {3,2} are frequent
	tr := New(Params{Freqs: []tract.Supp{5, 4, 3, 6}, Total: 8, MinSupp: 2, MinBody: 2, MinConf: 1})
	if !tr.Expandable(1) || tr.Down(1) {
		t.Fatal("cursor at the root must be expandable and have no children yet")
	}
	first, err1 := tr.AddChildren()
	second, err2 := tr.AddChildren()
	if !first || second || err1 != nil || err2 != nil {
		t.Fatalf("AddChildren: %v, %v then %v, %v; want true once, then false", first, err1, second, err2)
	}
	if tr.Height() != 2 {
		t.Fatalf("height = %d, want 2", tr.Height())
	}
	if tr.Down(0) {
		t.Error("item 0 has no smaller item, so it has no child")
	}
	if !tr.Down(3) || tr.Depth() != 2 {
		t.Fatal("Down(3) failed")
	}
	if !tr.SetSupp(0, 3) || !tr.SetSupp(2, 2) || tr.SetSupp(3, 1) {
		t.Error("SetSupp: counters for 0, 1, 2 expected, none for 3")
	}
	if tr.Supp(0) != 3 || tr.Supp(1) != 0 || tr.Supp(2) != 2 || tr.Supp(7) != 0 {
		t.Errorf("Supp = %d %d %d %d", tr.Supp(0), tr.Supp(1), tr.Supp(2), tr.Supp(7))
	}
	tr.Up()
	tr.Up() // stays at the root
	if tr.Depth() != 1 {
		t.Errorf("depth = %d after Up at the root", tr.Depth())
	}
	tr.SetSize(1, 1)
	if tr.Expandable(1) {
		t.Error("zmax 1 at depth 1 must not be expandable")
	}
}

func TestFits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		n, k int
		want bool
	}{
		{"empty", 0, 0, true},
		{"to the limit", math.MaxInt32 - 3, 3, true},
		{"one beyond", math.MaxInt32 - 3, 4, false},
		{"full", math.MaxInt32, 1, false},
		{"full, nothing added", math.MaxInt32, 0, true},
		{"huge request", 0, math.MaxInt32 + 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fits(tt.n, tt.k); got != tt.want {
				t.Errorf("fits(%d, %d) = %v, want %v", tt.n, tt.k, got, tt.want)
			}
		})
	}
}

func TestTree_AddChildrenTooLarge(t *testing.T) { //nolint:paralleltest // changes maxArena
	// The root of TestTree_Insertion's tree gets children for items 1, 2
	// and 3 with 1, 2 and 3 counters (4 root counters, 1 root node).
	tests := []struct {
		name  string
		limit int
	}{
		{"nodes", 1},    // the first child node does not fit
		{"counters", 6}, // counters 4+1 fit, 5+2 do not
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := New(Params{Freqs: []tract.Supp{5, 4, 3, 6}, Total: 8, MinSupp: 2, MinBody: 2, MinConf: 1})
			old := maxArena
			maxArena = tt.limit
			t.Cleanup(func() { maxArena = old })
			ok, err := tr.AddChildren()
			if ok || !errors.Is(err, ErrTooLarge) || !errors.Is(err, tract.ErrTooLarge) {
				t.Fatalf("AddChildren with arena limit %d = %v, %v; want false, ErrTooLarge", tt.limit, ok, err)
			}
			if len(tr.nodes) > tt.limit || len(tr.chn) > 0 {
				t.Errorf("%d nodes and %d child slots after the failure, limit %d", len(tr.nodes), len(tr.chn), tt.limit)
			}
		})
	}
	// with the default limit the same tree grows normally
	tr := New(Params{Freqs: []tract.Supp{5, 4, 3, 6}, Total: 8, MinSupp: 2, MinBody: 2, MinConf: 1})
	if ok, err := tr.AddChildren(); !ok || err != nil {
		t.Fatalf("AddChildren = %v, %v; want true, nil", ok, err)
	}
}

func TestTree_NodeLayout(t *testing.T) {
	// Only the items 0, 2, 8 and 9 are frequent. The root's child for
	// item 9 gets the candidates 0, 2 and 8: 3 candidates over a range of
	// 9 items use an identifier map; the child for item 2 gets a pure
	// array.
	freqs := []tract.Supp{5, 1, 4, 1, 1, 1, 1, 1, 5, 6}
	tr := New(Params{Freqs: freqs, Total: 10, MinSupp: 2, MinBody: 2, MinConf: 1})
	addChildren(tr)
	if !tr.Down(9) {
		t.Fatal("no child for item 9")
	}
	nd := tr.nodes[tr.curr]
	if nd.offset >= 0 || nd.size != 3 {
		t.Errorf("child of 9: offset %d size %d, want an identifier map of 3 (0, 2, 8)", nd.offset, nd.size)
	}
	tr.Up()
	if !tr.Down(2) {
		t.Fatal("no child for item 2")
	}
	if nd := tr.nodes[tr.curr]; nd.offset != 0 || nd.size != 1 {
		t.Errorf("child of 2: offset %d size %d, want a pure array at 0 of size 1", nd.offset, nd.size)
	}
	tr.Up()
	if !tr.Down(8) {
		t.Fatal("no child for item 8")
	}
	// candidates 0 and 2 over the range 0..2: pure array with a skip slot
	nd = tr.nodes[tr.curr]
	if nd.offset != 0 || nd.size != 3 || !isSkip(tr.cnts[nd.cnts+1]) {
		t.Errorf("child of 8: offset %d size %d slot1 %d", nd.offset, nd.size, tr.cnts[nd.cnts+1])
	}
	// ist_setsupp overwrites the non-candidate slot like upstream
	if !tr.SetSupp(1, 7) || tr.Supp(1) != 7 {
		t.Error("SetSupp on a pure array slot must succeed")
	}
}

// TestReport_InsertionOrder checks that the report does not depend on the
// insertion order: rec_tree's depth-first fill and a level-wise fill give
// identical output, line by line.
func TestReport_InsertionOrder(t *testing.T) {
	for seed := range uint64(40) {
		txs := genTxs(t, seed, 9)
		rc := ruleConfig{bodyMin: 2 + tract.Supp(seed%3), conf: 0.3, zmin: 1, zmax: report.MaxSize,
			eval: Eval{Measure: ruleval.Lift, Threshold: 0.5}, order: int(seed%5) - 2}
		a, _ := runRules(t, txs, rc, false)
		b, _ := runRules(t, txs, rc, true)
		if !slices.Equal(a.lines, b.lines) {
			t.Fatalf("seed %d: rules differ between fills:\n%v\n%v", seed, a.lines, b.lines)
		}
		for _, target := range []report.Target{report.TargetAll, report.TargetClosed, report.TargetMaximal, report.TargetGenerators} {
			sc := setConfig{target: target, smin: 2, zmin: 0, zmax: report.MaxSize, prune: noPrune,
				eval: Eval{Measure: ruleval.Lift, Agg: AggAvg, Threshold: 1}, order: int(seed%5) - 2}
			a, _ := runSets(t, txs, sc, false)
			b, _ := runSets(t, txs, sc, true)
			if !slices.Equal(a.lines, b.lines) {
				t.Fatalf("seed %d target %d: sets differ between fills:\n%v\n%v", seed, target, a.lines, b.lines)
			}
		}
	}
}

// genTxs generates a small random dataset.
func genTxs(t testing.TB, seed uint64, items int) []oracle.Transaction {
	t.Helper()
	txs, err := oracle.Generate(oracle.GenConfig{
		Seed: seed, NumTransactions: 12 + int(seed%20), NumItems: items,
		MinLen: 1, MaxLen: 5, Skew: int(seed % 3),
		Patterns: 2, PatternLen: 3, PatternProb: 0.4,
		DupTxProb: 0.1, EmptyProb: 0.05,
		Weights: oracle.WeightDist(seed % 2),
	})
	if err != nil {
		t.Fatal(err)
	}
	return txs
}

// countingSink counts the reported item sets and rules and cancels (sets
// *flag) at the cancelAt-th result.
type countingSink struct {
	n, cancelAt int
	flag        *bool
}

func (s *countingSink) count() error {
	s.n++
	if s.n == s.cancelAt {
		*s.flag = true
	}
	return nil
}

func (s *countingSink) ItemSet(report.ItemSet) error { return s.count() }
func (s *countingSink) Rule(report.Rule) error       { return s.count() }

// TestTree_Abort checks that Filter, CloMax and Report stop with the
// error of the abort function, and that Report delivers only a bounded
// number of results after the abort function starts failing.
func TestTree_Abort(t *testing.T) {
	t.Parallel()
	txs, err := oracle.Generate(oracle.GenConfig{
		Seed: 77, NumTransactions: 300, NumItems: 13, MinLen: 4, MaxLen: 10,
		Patterns: 3, PatternLen: 5, PatternProb: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	errAbort := errors.New("aborted")
	d := newDataset(t, txs, 2, 0, nil)
	build := func() (*Tree, *bool, *int) {
		tr := New(Params{Freqs: d.freqs, Total: d.total, MinSupp: 2, MinBody: 2, MinConf: 0})
		tr.SetSize(1, d.maxLen)
		fillRecTree(tr, d.supp, 2)
		tr.SetEval(Eval{Measure: ruleval.Lift, Threshold: 0.5})
		isCanceled, calls := new(bool), new(int)
		tr.SetAbort(func() error {
			*calls++
			if *isCanceled {
				return errAbort
			}
			return nil
		})
		return tr, isCanceled, calls
	}
	if tr, _, _ := build(); len(tr.nodes) < 4*abortEvery {
		t.Fatalf("only %d nodes; the test needs more than %d", len(tr.nodes), 4*abortEvery)
	}

	phases := []struct {
		name string
		run  func(tr *Tree) error
	}{
		{"filter backward", func(tr *Tree) error { return tr.Filter(0) }},
		{"filter weak forward", func(tr *Tree) error { return tr.Filter(-2) }},
		{"filter strong forward", func(tr *Tree) error { return tr.Filter(2) }},
		{"clomax closed safe", func(tr *Tree) error { return tr.CloMax(report.TargetClosed, true) }},
		{"clomax maximal", func(tr *Tree) error { return tr.CloMax(report.TargetMaximal, false) }},
		{"clomax generators", func(tr *Tree) error { return tr.CloMax(report.TargetGenerators, false) }},
	}
	for _, ph := range phases {
		t.Run(ph.name, func(t *testing.T) {
			t.Parallel()
			tr, _, calls := build()
			if err := ph.run(tr); err != nil {
				t.Fatalf("not canceled: %v", err)
			}
			if *calls == 0 {
				t.Fatal("abort function never called")
			}
			tr, isCanceled, calls := build()
			*isCanceled = true
			if err := ph.run(tr); err != errAbort {
				t.Fatalf("canceled: got %v", err)
			}
			if *calls != 1 {
				t.Errorf("%d abort calls after the first failure", *calls-1)
			}
		})
	}
	for name, target := range map[string]report.Target{"report rules": report.TargetRules, "report sets": report.TargetAll} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tr, _, _ := build()
			tr.Init()
			rep := report.New(len(d.names), d.total, d.freqs)
			rep.SetTarget(target, report.ModeNoFilter, -1)
			rep.SetSupport(2, report.MaxSupp)
			rep.Setup()
			full := &countingSink{flag: new(bool)}
			rep.SetSink(full)
			if err := tr.Report(rep, target); err != nil {
				t.Fatal(err)
			}
			const cancelAt = 100
			if full.n < 20*cancelAt {
				t.Fatalf("only %d results; the test needs more", full.n)
			}
			tr, isCanceled, _ := build()
			tr.Init()
			rep = report.New(len(d.names), d.total, d.freqs)
			rep.SetTarget(target, report.ModeNoFilter, -1)
			rep.SetSupport(2, report.MaxSupp)
			rep.Setup()
			sink := &countingSink{cancelAt: cancelAt, flag: isCanceled}
			rep.SetSink(sink)
			if err := tr.Report(rep, target); err != errAbort {
				t.Fatalf("got %v", err)
			}
			// at most abortEvery counters after the cancellation, each with
			// at most maxLen rules (one item set)
			if after := sink.n - cancelAt; after > abortEvery*d.maxLen {
				t.Errorf("%d results after the cancellation (of %d), bound %d", after, full.n, abortEvery*d.maxLen)
			}
		})
	}
}

// TestTree_AbortAllocs checks that the cancellation check does not
// allocate. AllocsPerRun must not run in parallel.
func TestTree_AbortAllocs(t *testing.T) { //nolint:paralleltest // AllocsPerRun
	tr := New(Params{Freqs: []tract.Supp{3, 2}, Total: 4, MinSupp: 1})
	done := make(chan struct{})
	errAbort := errors.New("aborted")
	tr.SetAbort(func() error {
		select {
		case <-done:
			return errAbort
		default:
			return nil
		}
	})
	if a := testing.AllocsPerRun(10*abortEvery, func() { _ = tr.tick() }); a != 0 {
		t.Errorf("the cancellation check allocates %v times", a)
	}
}
