package report

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// This file contains a small FP-growth-like miner that drives a Reporter
// with valid call sequences (the protocol of fpgrowth.c rec_simple and of
// the istree reporting functions). It is used by the property tests, which
// compare the reported sets with a brute-force enumeration, and to
// generate the random scripts under testdata/random, whose expected output
// comes from the C harness.

// simDB is a transaction database over items 0..nItems-1. Every
// transaction holds distinct items in ascending order.
type simDB struct {
	nItems int
	trans  [][]tract.Item
	wgts   []tract.Supp
}

func (db *simDB) total() tract.Supp {
	var s tract.Supp
	for _, w := range db.wgts {
		s += w
	}
	return s
}

// support returns the support of the item set given as a bit mask.
func (db *simDB) support(mask uint64) tract.Supp {
	var s tract.Supp
	for i, t := range db.trans {
		var m uint64
		for _, it := range t {
			m |= 1 << it
		}
		if m&mask == mask {
			s += db.wgts[i]
		}
	}
	return s
}

func (db *simDB) itemFreqs() []tract.Supp {
	f := make([]tract.Supp, db.nItems)
	for i := range f {
		f[i] = db.support(1 << i)
	}
	return f
}

func randomDB(rng *rand.Rand, nItems, nTrans, maxWgt int, density float64) *simDB {
	db := &simDB{nItems: nItems}
	for range nTrans {
		var t []tract.Item
		for i := range nItems {
			if rng.Float64() < density {
				t = append(t, tract.Item(i))
			}
		}
		db.trans = append(db.trans, t)
		db.wgts = append(db.wgts, tract.Supp(1+rng.IntN(maxWgt)))
	}
	return db
}

// simConfig configures the reporter and the simulated miner.
type simConfig struct {
	target     Target
	mode       Mode // extra mode flags (ModeNoFilter, ModeSort)
	dir        int  // filter item order passed to SetTarget
	smin, smax tract.Supp
	zmin, zmax int
	border     []tract.Supp // border[size], applied for size >= zmin
	eval       bool         // LogRatio evaluation
	evalDir    int
	evalThresh float64
	psp        bool
	output     string // "emit" (sink), "count" (no sink) or "file"

	descending bool // process sibling items in descending code order
	perfect    bool // perfect extension pruning in projections
	tail       bool // head union tail pruning (maximal only)
	packed     bool // push perfect extensions < 16 with AddPexPacked
	istree     bool // istree-style reporting (ReportEval, pre-order)
	rules      bool // istree-style rule reporting (ReportRule)
	queries    bool // sprinkle query calls (only affects the script)
	// noChain disables the chain shortcut; boundaryPex collects perfect
	// extensions also where no projection is built (Xable(2) false),
	// the fix of fpgrowth-core.md B5.
	noChain, boundaryPex bool
}

// tracer forwards calls to a Reporter and records them as a script.
type tracer struct {
	r   *Reporter
	w   *strings.Builder // nil: do not record
	rng *rand.Rand       // for query sprinkling, may be nil
}

func (t *tracer) printf(format string, args ...any) {
	if t.w != nil {
		fmt.Fprintf(t.w, format+"\n", args...)
	}
}

func (t *tracer) Add(it tract.Item, s tract.Supp) bool {
	t.printf("add %d %d", it, s)
	return t.r.Add(it, s)
}

func (t *tracer) AddPex(it tract.Item) {
	t.printf("addpex %d", it)
	t.r.AddPex(it)
}

func (t *tracer) AddPexPacked(bits tract.Item) {
	t.printf("addpexpk %d", bits)
	t.r.AddPexPacked(bits | PackedFlag)
}

func (t *tracer) Remove(n int) {
	t.printf("remove %d", n)
	t.r.Remove(n)
}

func (t *tracer) Report() error {
	t.printf("report")
	return t.r.Report()
}

func (t *tracer) ReportEval(v float64) error {
	t.printf("reportv %s", fmtFloat(v))
	return t.r.ReportEval(v)
}

func (t *tracer) ReportRule(head tract.Item, body, hs tract.Supp, e float64) error {
	t.printf("reprule %d %d %d %s", head, body, hs, fmtFloat(e))
	return t.r.ReportRule(head, body, hs, e)
}

func (t *tracer) Xable(n int) bool {
	t.printf("xable %d", n)
	return t.r.Xable(n)
}

func (t *tracer) Tail(items []tract.Item) bool {
	if t.w != nil {
		t.w.WriteString("tail")
		for _, it := range items {
			fmt.Fprintf(t.w, " %d", it)
		}
		t.w.WriteByte('\n')
	}
	return t.r.Tail(items)
}

// query records a random query command (no effect on the reporter).
func (t *tracer) query(nItems int) {
	if t.w == nil || t.rng == nil || t.rng.IntN(8) != 0 {
		return
	}
	switch t.rng.IntN(5) {
	case 0:
		t.printf("cnt")
	case 1:
		t.printf("getsupp")
	case 2:
		t.printf("pexcnt")
	case 3:
		t.printf("uses %d", t.rng.IntN(nItems))
	default:
		t.printf("xable %d", 1+t.rng.IntN(3))
	}
}

// newSimReporter creates and configures a reporter for db and cfg,
// recording the configuration if w is not nil.
func newSimReporter(db *simDB, cfg *simConfig, sink Sink, w *strings.Builder) *Reporter {
	freqs := db.itemFreqs()
	r := New(db.nItems, db.total(), freqs)
	rec := func(format string, args ...any) {
		if w != nil {
			fmt.Fprintf(w, format+"\n", args...)
		}
	}
	rec("items %d", db.nItems)
	rec("total %d", db.total())
	if cfg.eval {
		var b strings.Builder
		for _, f := range freqs {
			fmt.Fprintf(&b, " %d", f)
		}
		rec("freqs%s", b.String())
	}
	smax := fmt.Sprint(cfg.smax)
	if cfg.smax >= MaxSupp {
		smax = "max"
	}
	zmax := fmt.Sprint(cfg.zmax)
	if cfg.zmax >= MaxSize {
		zmax = "max"
	}
	// Same order as fpgrowth.c: fpg_report, setbdr, addpsp, setfmt,
	// open, setup.
	rec("supp %d %s", cfg.smin, smax)
	r.SetSupport(cfg.smin, cfg.smax)
	rec("size %d %s", cfg.zmin, zmax)
	r.SetSize(cfg.zmin, cfg.zmax)
	if cfg.eval {
		rec("eval logrto %d %s", cfg.evalDir, fmtFloat(cfg.evalThresh))
		r.SetEval(LogRatio, cfg.evalDir, cfg.evalThresh)
	}
	rec("target %s%s dir %d", targetName(cfg.target), modeNames(cfg.mode), cfg.dir)
	r.SetTarget(cfg.target, cfg.mode, cfg.dir)
	for size, s := range cfg.border {
		if size >= cfg.zmin {
			rec("border %d %d", size, s)
			r.SetBorder(size, s)
		}
	}
	if cfg.psp {
		rec("psp")
		r.AddSpectrum()
	}
	rec("output %s", cfg.output)
	if cfg.output != "count" {
		r.SetSink(sink)
	}
	rec("setup")
	r.Setup()
	return r
}

func targetName(t Target) string {
	switch t {
	case TargetClosed:
		return "closed"
	case TargetMaximal:
		return "maximal"
	case TargetGenerators:
		return "generators"
	case TargetRules:
		return "rules"
	default:
		return "all"
	}
}

func modeNames(m Mode) string {
	var b strings.Builder
	if m&ModeNoFilter != 0 {
		b.WriteString(" nofilter")
	}
	if m&ModeNoExpand != 0 {
		b.WriteString(" noexpand")
	}
	if m&ModeSort != 0 {
		b.WriteString(" sort")
	}
	return b.String()
}

// simTrans is a transaction of a conditional database.
type simTrans struct {
	items []tract.Item // ascending
	w     tract.Supp
}

// sim is the simulated miner.
type sim struct {
	t      *tracer
	cfg    *simConfig
	nItems int
	rng    *rand.Rand
	fullDB *simDB
}

// run mines db like fpg_simple (or like ist_report for istree modes).
func (s *sim) run(db *simDB) error {
	cdb := make([]simTrans, len(db.trans))
	for i, t := range db.trans {
		cdb[i] = simTrans{items: t, w: db.wgts[i]}
	}
	if s.cfg.istree || s.cfg.rules {
		return s.runIstree(cdb)
	}
	total := db.total()
	if s.cfg.smin > total {
		return nil // fpg_simple returns without reporting anything
	}
	pex := MaxSupp
	if s.cfg.perfect {
		pex = total
	}
	var keep []tract.Item
	var pexs []tract.Item
	for i := range db.nItems {
		f := condSupport(cdb, tract.Item(i))
		switch {
		case f < s.cfg.smin:
		case f >= pex:
			pexs = append(pexs, tract.Item(i))
		default:
			keep = append(keep, tract.Item(i))
		}
	}
	s.pushPexs(pexs)
	if len(keep) > 0 {
		if err := s.rec(restrict(cdb, keep), keep); err != nil {
			return err
		}
	}
	return s.t.Report()
}

func condSupport(cdb []simTrans, it tract.Item) tract.Supp {
	var s tract.Supp
	for _, t := range cdb {
		if _, ok := slices.BinarySearch(t.items, it); ok {
			s += t.w
		}
	}
	return s
}

// restrict keeps only the given items (ascending) in every transaction.
func restrict(cdb []simTrans, items []tract.Item) []simTrans {
	out := make([]simTrans, 0, len(cdb))
	for _, t := range cdb {
		var r []tract.Item
		for _, it := range t.items {
			if _, ok := slices.BinarySearch(items, it); ok {
				r = append(r, it)
			}
		}
		out = append(out, simTrans{items: r, w: t.w})
	}
	return out
}

// project returns the conditional database of item x: the transactions
// containing x, restricted to the items below x.
func project(cdb []simTrans, x tract.Item) []simTrans {
	var out []simTrans
	for _, t := range cdb {
		i, ok := slices.BinarySearch(t.items, x)
		if ok {
			out = append(out, simTrans{items: t.items[:i], w: t.w})
		}
	}
	return out
}

// pushPexs adds perfect extensions, packing items < 16 if configured.
func (s *sim) pushPexs(pexs []tract.Item) {
	if s.cfg.packed && len(pexs) > 1 && s.rng.IntN(2) == 0 {
		var bits tract.Item
		var rest []tract.Item
		for _, it := range pexs {
			if it < 16 {
				bits |= 1 << it
			} else {
				rest = append(rest, it)
			}
		}
		if bits != 0 {
			s.t.AddPexPacked(bits)
		}
		pexs = rest
	}
	for _, it := range pexs {
		s.t.AddPex(it)
	}
}

// rec is the equivalent of rec_simple for the conditional database cdb
// whose frequent items are items (ascending).
func (s *sim) rec(cdb []simTrans, items []tract.Item) error {
	if s.cfg.tail {
		tail := slices.Clone(items)
		slices.Reverse(tail)
		if s.t.Tail(tail) {
			return nil
		}
	}
	canProj := len(items) > 1 && s.t.Xable(2)
	order := slices.Clone(items)
	if s.cfg.descending {
		slices.Reverse(order)
	}
	for _, x := range order {
		sx := condSupport(cdb, x)
		if !s.t.Add(x, sx) {
			continue
		}
		s.t.query(s.nItems)
		sub := project(cdb, x)
		if single, pattern := singlePattern(sub); single && !s.cfg.noChain {
			// one node in the FP-tree: all ancestors are perfect
			// extensions (pushed from the parent upwards)
			p := slices.Clone(pattern)
			slices.Reverse(p)
			s.pushPexs(p)
		} else if canProj || s.cfg.boundaryPex {
			pex := MaxSupp
			if s.cfg.perfect {
				pex = sx
			}
			var kept, pexs []tract.Item
			for _, y := range items {
				if y >= x {
					break
				}
				sy := condSupport(sub, y)
				switch {
				case sy < s.cfg.smin:
				case sy >= pex:
					pexs = append(pexs, y)
				default:
					kept = append(kept, y)
				}
			}
			s.pushPexs(pexs)
			if len(kept) > 0 && canProj {
				if err := s.rec(restrict(sub, kept), kept); err != nil {
					return err
				}
			}
		}
		if err := s.t.Report(); err != nil {
			return err
		}
		s.t.query(s.nItems)
		s.t.Remove(1)
	}
	return nil
}

// singlePattern reports whether all transactions of cdb have the same
// items, and returns them.
func singlePattern(cdb []simTrans) (bool, []tract.Item) {
	if len(cdb) == 0 {
		return false, nil
	}
	for _, t := range cdb[1:] {
		if !slices.Equal(t.items, cdb[0].items) {
			return false, nil
		}
	}
	return true, cdb[0].items
}

// runIstree reports like ist_report: item sets in pre-order with
// ReportEval (isets) or rules after the subtree (rules/r4set). The
// item set tree already filtered, so the reporter runs with ModeNoFilter.
func (s *sim) runIstree(cdb []simTrans) error {
	var items []tract.Item
	for i := range s.nItems {
		if condSupport(cdb, tract.Item(i)) >= s.cfg.smin {
			items = append(items, tract.Item(i))
		}
	}
	return s.istreeRec(cdb, items, nil)
}

func (s *sim) istreeRec(cdb []simTrans, items, set []tract.Item) error {
	if !s.cfg.rules && s.rng.IntN(5) != 0 { // the tree's own filtering
		if err := s.t.ReportEval(s.randEval()); err != nil {
			return err
		}
	}
	for i, x := range items {
		sx := condSupport(cdb, x)
		s.t.Add(x, sx)
		set = append(set, x)
		// extensions by larger items (apriori-style item set tree)
		sub := keepContaining(cdb, x)
		var ext []tract.Item
		for _, y := range items[i+1:] {
			if condSupport(sub, y) >= s.cfg.smin {
				ext = append(ext, y)
			}
		}
		if err := s.istreeRec(sub, ext, set); err != nil {
			return err
		}
		if s.cfg.rules {
			if err := s.reportRules(set); err != nil {
				return err
			}
		}
		set = set[:len(set)-1]
		s.t.Remove(1)
	}
	return nil
}

func keepContaining(cdb []simTrans, x tract.Item) []simTrans {
	var out []simTrans
	for _, t := range cdb {
		if _, ok := slices.BinarySearch(t.items, x); ok {
			out = append(out, t)
		}
	}
	return out
}

func (s *sim) randEval() float64 {
	return float64(s.rng.IntN(2001)-1000) / 997
}

// reportRules reports rules with random heads of the current set, with
// supports computed from the full database (as r4set does).
func (s *sim) reportRules(set []tract.Item) error {
	for _, head := range set {
		if s.rng.IntN(3) == 0 {
			continue
		}
		var body, hs uint64
		for _, it := range set {
			if it != head {
				body |= 1 << it
			}
		}
		hs = 1 << head
		bs, hss := s.fullDB.support(body), s.fullDB.support(hs)
		if err := s.t.ReportRule(head, bs, hss, s.randEval()); err != nil {
			return err
		}
	}
	return nil
}

// simulate runs the simulated miner on db with cfg, sending accepted sets
// to sink, and returns the reporter and the recorded script (if record).
func simulate(db *simDB, cfg *simConfig, sink Sink, rng *rand.Rand, record bool) (*Reporter, string, error) {
	var w *strings.Builder
	if record {
		w = &strings.Builder{}
	}
	r := newSimReporter(db, cfg, sink, w)
	t := &tracer{r: r, w: w}
	if cfg.queries {
		t.rng = rng
	}
	s := &sim{t: t, cfg: cfg, nItems: db.nItems, rng: rng, fullDB: db}
	err := s.run(db)
	if w == nil {
		return r, "", err
	}
	return r, w.String(), err
}
