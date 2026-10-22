package report

import (
	"fmt"
	"math"
	"math/bits"
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Target selects which item sets are reported (upstream ISR_* targets;
// the values are upstream's).
type Target int

// Report targets. The zero value, TargetAll, is upstream's default.
const (
	TargetAll        Target = 0x00 // all frequent item sets (ISR_ALL)
	TargetClosed     Target = 0x01 // closed item sets (ISR_CLOSED)
	TargetMaximal    Target = 0x02 // maximal item sets (ISR_MAXIMAL)
	TargetGenerators Target = 0x04 // generators (ISR_GENERAS)
	TargetRules      Target = 0x08 // association rules (ISR_RULES)
)

// Mode holds reporting mode flags (upstream ISR_* modes; the values are
// upstream's).
type Mode int

// Reporting mode flags.
const (
	// ModeNoFilter disables the internal closed/maximal/generator
	// filtering; the caller has filtered already (ISR_NOFILTER).
	ModeNoFilter Mode = 0x10
	// ModeNoExpand reports a set together with all its perfect
	// extensions as one set instead of expanding the hypercube
	// (ISR_NOEXPAND). SetTarget forces it for closed and maximal sets.
	ModeNoExpand Mode = 0x20
	// ModeSort sorts generator keys, needed when the miner adds items in
	// varying orders (ISR_SORT).
	ModeSort Mode = 0x40
)

const (
	// MaxSize is the "no limit" item set size (upstream ITEM_MAX).
	MaxSize = math.MaxInt32
	// MaxSupp is the "no limit" support (upstream RSUPP_MAX, widened to
	// the 64-bit tract.Supp).
	MaxSupp = tract.Supp(math.MaxInt64)
)

// PackedFlag marks an item code as a bit set of packed items (upstream
// TA_END = ITEM_MIN): bit i of a packed value stands for item i.
const PackedFlag = tract.Item(math.MinInt32)

// ln2 is upstream's LN_2 literal (identical to math.Ln2 as a float64).
const ln2 = 0.69314718055994530942

// EvalFunc evaluates the current item set of a reporter (upstream
// ISEVALFN). It is called for every expanded item set that passes the
// border; the reporter's Items and Supp describe that set.
type EvalFunc func(r *Reporter) float64

// Reporter is the mining-side item set reporter, a port of the parts of
// upstream's ISREPORT that decide which item sets and rules are reported.
//
// A Reporter is configured with the Set* methods and Setup, then driven by
// a miner through Add, AddPex, AddPexPacked, Remove and Report (or
// ReportEval, ReportRule). Accepted sets are passed to the Sink set with
// SetSink. A Reporter is not safe for concurrent use.
type Reporter struct {
	numItems int
	target   Target
	mode     Mode
	dir      int // item order of the filters: -1 descending, +1 ascending

	zmin, zmax, xmax int
	smin, smax       tract.Supp
	border           []tract.Supp // border[size]: minimum support per size

	cnt    int          // number of items in the current set
	items  []tract.Item // items[:cnt]: current set; beyond: expansion
	supps  []tract.Supp // supps[k]: support of the prefix of length k
	wgts   []float64    // wgts[k]: weight of the prefix of length k
	inUse  []bool       // item is in the set or a perfect extension
	pexCnt []int        // pexCnt[k]: perfect extensions added at length k
	pexs   []tract.Item // perfect extensions of all levels, oldest first
	ldps   []float64    // binary logarithms of the item probabilities

	cm   *closedMaximal
	err  error // latched error (ErrTooLarge or the abort error), see Err
	gen  *generatorTable
	sto  tract.Supp   // maximum superset support for storing a set
	iset []tract.Item // scratch: repository set / generator key

	evalFn     EvalFunc
	evalDir    int
	evalThresh float64
	eval       float64

	sink      Sink
	stats     []uint64 // stats[size]: number of reported sets
	repCnt    uint64
	saturated bool
	psp       *Spectrum

	binom []uint64     // scratch: binomial coefficients (count-only mode)
	body  []tract.Item // scratch: rule body

	abort func() error // cancellation check (SetAbort)
	ticks uint32       // steps since the start (see tick)

	cmCheck CloMaxCheck // final closed/maximal check (SetCloMaxCheck)

	// pexMax is the set size from which perfect extensions cannot change
	// any result (see PexUseful); noPexGuard disables the guard
	// (SetPexGuard).
	pexMax     int
	noPexGuard bool
}

// CloMaxCheck decides whether an item set is closed (TargetClosed) or
// maximal (TargetMaximal) in the whole database: set holds the items of
// the current set and all its perfect extensions, in no particular
// order, and supp is its support. See SetCloMaxCheck.
type CloMaxCheck func(set []tract.Item, supp tract.Supp) bool

// AbortEvery is the number of reporter steps between two calls of the
// abort function (SetAbort). A step is a call of Add or Report or one set
// of a perfect-extension hypercube visited while it is expanded. It is a
// power of two.
const AbortEvery = 1024

// New is a port of isr_create: it returns a reporter for item codes
// 0..numItems-1 with upstream's defaults (all frequent item sets, sizes
// [1, MaxSize], supports [1, MaxSupp], no border, no evaluation, no
// spectrum, no sink). total is the total transaction weight, the support
// of the empty set. freqs, if not nil, holds the item frequencies (length
// numItems) used by LogRatio; nil means all zero.
func New(numItems int, total tract.Supp, freqs []tract.Supp) *Reporter {
	if numItems < 0 {
		panic(fmt.Sprintf("report: negative number of items %d", numItems))
	}
	if freqs != nil && len(freqs) != numItems {
		panic(fmt.Sprintf("report: %d item frequencies for %d items", len(freqs), numItems))
	}
	r := &Reporter{
		numItems: numItems,
		zmin:     1,
		zmax:     MaxSize,
		xmax:     MaxSize,
		smin:     1,
		smax:     MaxSupp,
		items:    make([]tract.Item, numItems),
		supps:    make([]tract.Supp, numItems+1),
		wgts:     make([]float64, numItems+1),
		inUse:    make([]bool, numItems),
		pexCnt:   make([]int, numItems+1),
		pexs:     make([]tract.Item, 0, numItems),
		ldps:     make([]float64, numItems),
		sto:      MaxSupp,
		iset:     make([]tract.Item, numItems+1),
		evalDir:  1,
		stats:    make([]uint64, numItems+1),
	}
	r.updateXmax()
	r.supps[0] = total
	r.wgts[0] = float64(total)
	b := float64(total)
	if b > 0 {
		b = math.Log(b) / ln2
	} else {
		b = 0
	}
	for i, f := range freqs {
		if x := float64(f); x > 0 {
			r.ldps[i] = math.Log(x)/ln2 - b
		}
	}
	return r
}

// SetTarget is a port of isr_settarg. target is normalized to one target
// with priority rules > generators > maximal > closed > all; closed and
// maximal force ModeNoExpand. Unless mode has ModeNoFilter, closed and
// maximal targets get a fresh closed/maximal repository and generators a
// fresh generator repository. dir is the item order used by these filters
// (repository order and ModeSort order): negative for descending, which is
// what fpgrowth uses, else ascending.
func (r *Reporter) SetTarget(target Target, mode Mode, dir int) {
	switch {
	case target&TargetRules != 0:
		target = TargetRules
	case target&TargetGenerators != 0:
		target = TargetGenerators
	case target&TargetMaximal != 0:
		target = TargetMaximal
	case target&TargetClosed != 0:
		target = TargetClosed
	default:
		target = TargetAll
	}
	if target&(TargetClosed|TargetMaximal) != 0 {
		mode |= ModeNoExpand
	}
	r.target, r.mode = target, mode
	r.cm, r.gen = nil, nil
	if target&(TargetClosed|TargetMaximal|TargetGenerators) != 0 && mode&ModeNoFilter == 0 {
		if target == TargetGenerators {
			r.gen = newGeneratorTable()
		} else {
			r.cm = newClosedMaximal(dir, r.numItems)
		}
		r.sto = MaxSupp
		if target == TargetMaximal {
			r.sto = 0
		}
		r.dir = 1
		if dir < 0 {
			r.dir = -1
		}
	}
	r.updateXmax()
}

// SetCloMaxCheck sets a final check for closed and maximal item sets (an
// extension, not part of upstream). Report calls fn for every set that
// the closed/maximal repository accepts and that passes the support and
// size gates, before the border, the evaluation and the sink; a false
// result suppresses the set (the repository has stored it already, which
// is harmless: it is a genuine item set with its true support). A miner
// that limits its search by the maximum size needs it, because the
// repository then lacks the supersets beyond the limit that would prove
// a set non-closed or non-maximal (fpgrowth-core.md B5). nil (the
// default) removes the check. It has no effect for other targets or
// with ModeNoFilter.
func (r *Reporter) SetCloMaxCheck(fn CloMaxCheck) { r.cmCheck = fn }

// SetSupport is a port of isr_setsupp: only item sets (and rules) with a
// support in [smin, smax] are reported.
func (r *Reporter) SetSupport(smin, smax tract.Supp) {
	r.smin, r.smax = smin, smax
}

// SetSize is a port of isr_setsize: only item sets (and rules) with
// [zmin, zmax] items are reported. zmax >= MaxSize means no limit.
func (r *Reporter) SetSize(zmin, zmax int) {
	r.zmin, r.zmax = zmin, zmax
	r.updateXmax()
}

// SetBorder is a port of isr_setbdr: item sets with size items need a
// support of at least supp (option -F). Sizes beyond the largest size set
// are not filtered, unset smaller sizes have border 0.
func (r *Reporter) SetBorder(size int, supp tract.Supp) {
	if size < 0 {
		panic(fmt.Sprintf("report: negative border size %d", size))
	}
	if size >= len(r.border) {
		r.border = append(r.border, make([]tract.Supp, size+1-len(r.border))...)
	}
	r.border[size] = supp
}

// ClearBorder is a port of isr_clrbdr.
func (r *Reporter) ClearBorder() { r.border = nil }

// SetEval is a port of isr_seteval: item sets are reported only if
// sign(dir)·fn(r) >= sign(dir)·thresh (dir >= 0 counts as +1). A nil fn
// removes the evaluation.
func (r *Reporter) SetEval(fn EvalFunc, dir int, thresh float64) {
	r.evalFn = fn
	r.evalDir = 1
	if dir < 0 {
		r.evalDir = -1
	}
	r.evalThresh = float64(r.evalDir) * thresh
}

// AddSpectrum is a port of isr_addpsp(rep, NULL): it creates a pattern
// spectrum with the size and support limits current at the time of the
// call. It does nothing if the reporter already has a spectrum.
func (r *Reporter) AddSpectrum() {
	if r.psp == nil {
		r.psp = NewSpectrum(r.zmin, r.zmax, r.smin, r.smax)
	}
}

// Spectrum is a port of isr_getpsp: the pattern spectrum, or nil.
func (r *Reporter) Spectrum() *Spectrum { return r.psp }

// SetSink sets the receiver of accepted item sets and rules (the
// equivalent of isr_open). With a nil sink, no border and no evaluation
// function the reporter only counts (upstream's count-only mode) and
// computes the counts of perfect-extension hypercubes arithmetically.
func (r *Reporter) SetSink(s Sink) { r.sink = s }

// SetAbort sets a cancellation check: every AbortEvery steps (calls of
// Add and Report, and sets visited while a perfect-extension hypercube is
// expanded) the reporter calls fn. A non-nil result is latched like a
// repository error: Report returns it, Add returns false from then on,
// so that every miner recursion unwinds, and Err reports it after mining.
// fn should be cheap (a non-blocking receive on a done channel); nil (the
// default) disables the checks. This bounds the work after a cancellation
// also where a miner does not return to a recursion entry for a long
// time: a hypercube of 2^k sets, or a burst of the 16-items machine.
// The checks do not allocate.
func (r *Reporter) SetAbort(fn func() error) { r.abort = fn }

// tick counts one step and calls the abort function every AbortEvery
// steps; it returns the latched error, if any. The fast path is small
// enough to be inlined.
func (r *Reporter) tick() error {
	r.ticks++
	if r.ticks%AbortEvery != 0 {
		return nil
	}
	return r.checkAbort()
}

// checkAbort calls the abort function and latches its error.
func (r *Reporter) checkAbort() error {
	if r.abort == nil || r.err != nil {
		return r.err
	}
	if err := r.abort(); err != nil {
		r.err = err
	}
	return r.err
}

// Setup is a port of isr_setup: it finalizes the configuration (the size
// limit for Xable) and empties the current item set. Call it after the
// configuration and before mining.
func (r *Reporter) Setup() {
	r.cnt = 0
	r.updateXmax()
}

// updateXmax computes the size limit of Xable: zmax, or zmax+1 for closed
// and maximal sets so that a superset witness can be explored. It also
// computes pexMax (see PexUseful).
func (r *Reporter) updateXmax() {
	r.xmax = r.zmax
	if r.target&(TargetClosed|TargetMaximal) != 0 && r.zmax < MaxSize {
		r.xmax = r.zmax + 1
	}
	switch {
	case r.noPexGuard || r.mode&ModeNoExpand != 0 || r.cm != nil:
		r.pexMax = math.MaxInt
	case r.target&TargetGenerators != 0:
		r.pexMax = -1
	default:
		r.pexMax = r.zmax
	}
}

// PexUseful reports whether perfect extensions added now can still change
// a result. It is an extension, not part of upstream: a miner may skip
// collecting perfect extensions (AddPex, AddPexPacked) when it returns
// false, with identical results. AddPex itself stays an exact port.
//
// It returns false for generators (AddPex ignores perfect extensions
// there) and, for all other targets except closed and maximal item sets,
// once the current set has zmax items. Then every set reported while the
// extensions would be present has at least zmax items: Report rejects
// the sets with more than zmax items before looking at the extensions,
// and of the hypercube of a set with exactly zmax items only the set
// itself fits the size limit. Its items, support, weight, evaluation,
// border check, counts and spectrum entry do not depend on the
// extensions, and neither do the outputs of smaller sets (the skipped
// work writes only scratch entries beyond index zmax). Closed and
// maximal item sets (ModeNoExpand, the closed/maximal repository) are
// excluded: there a set is reported or stored together with all its
// perfect extensions, and the size gates of a size-limited search
// (PexCount) count them.
func (r *Reporter) PexUseful() bool { return r.cnt < r.pexMax }

// SetPexGuard turns the perfect-extension guard on (the default) or off:
// with the guard off, PexUseful always returns true and Report and
// expand handle the perfect extensions of sets with zmax items like
// upstream. The results are the same either way; it is meant for
// differential tests.
func (r *Reporter) SetPexGuard(on bool) {
	r.noPexGuard = !on
	r.updateXmax()
}

// Add is a port of isr_add: it extends the current item set by item with
// support supp. It returns false, leaving the set unchanged, if the
// closed/maximal repository proves that neither the extended set nor any
// of its extensions can be reported (a stored superset has at least the
// same support) or if the extended set is not a generator. The caller must
// then skip the subtree, the report and the removal.
//
// If the closed/maximal repository cannot grow (ErrTooLarge) or the abort
// function fails (SetAbort), Add latches the error and returns false; the
// next Report returns it, and Err reports it after mining. Once an error
// is latched, Add always returns false.
func (r *Reporter) Add(item tract.Item, supp tract.Supp) bool {
	switch {
	case r.err != nil || r.tick() != nil:
		return false
	case r.cm != nil:
		ok, err := r.cm.add(item, supp)
		if err != nil {
			r.err = err
			return false
		}
		if !ok {
			return false
		}
	case r.gen != nil:
		if !r.isGenerator(item, supp) {
			return false
		}
	}
	r.inUse[item] = true
	r.items[r.cnt] = item
	r.cnt++
	r.supps[r.cnt] = supp
	r.pexCnt[r.cnt] = 0
	return true
}

// AddPex is a port of isr_addpex: it adds item as a perfect extension of
// the current item set. It returns false and does nothing if the item is
// already in use or the target is generators (perfect extensions are
// meaningless for generators).
func (r *Reporter) AddPex(item tract.Item) bool {
	if r.inUse[item] || r.target&TargetGenerators != 0 {
		return false
	}
	r.inUse[item] = true
	r.pexs = append(r.pexs, item)
	r.pexCnt[r.cnt]++
	return true
}

// AddPexPacked is a port of isr_addpexpk: bits is a packed item value
// (PackedFlag | bit set, as produced by transaction packing and the
// 16-items machine); every set bit i adds item i as a perfect extension,
// lowest bit first, with the conditions of AddPex.
//
// Deviation from upstream: the C loop computes 1 << 31 (signed overflow,
// undefined behavior, report.md B16) when bit 30 is set; here the shift is
// unsigned. On arm64 upstream happens to behave the same.
func (r *Reporter) AddPexPacked(bits tract.Item) {
	b := uint32(bits) &^ (1 << 31) // strip PackedFlag
	for i := 0; i < 31 && uint32(1)<<i <= b; i++ {
		if b&(1<<i) == 0 {
			continue
		}
		r.AddPex(tract.Item(i))
	}
}

// Remove is a port of isr_remove: it removes the last n items from the
// current set together with the perfect extensions added at their levels.
func (r *Reporter) Remove(n int) {
	if r.cm != nil {
		r.cm.remove(n)
	}
	for ; n > 0; n-- {
		for k := r.pexCnt[r.cnt]; k > 0; k-- {
			last := len(r.pexs) - 1
			r.inUse[r.pexs[last]] = false
			r.pexs = r.pexs[:last]
		}
		r.cnt--
		r.inUse[r.items[r.cnt]] = false
	}
}

// Xable is a port of isr_xable: whether n more items can be added without
// exceeding the size limit that is still useful for reporting.
func (r *Reporter) Xable(n int) bool { return r.cnt+n <= r.xmax }

// Uses is a port of isr_uses: whether item is in the current set or is a
// perfect extension of it.
func (r *Reporter) Uses(item tract.Item) bool { return r.inUse[item] }

// Tail is a port of isr_tail (head union tail pruning for maximal item
// sets): it returns true if the current set together with all tail items
// (sorted in repository order) is already covered by a stored maximal set,
// so that the subtree need not be processed. It returns false if the
// reporter has no closed/maximal repository. If the repository cannot grow
// (ErrTooLarge), Tail latches the error like Add and returns true, so that
// the miner skips the subtree.
func (r *Reporter) Tail(items []tract.Item) bool {
	if r.cm == nil {
		return false
	}
	s, err := r.cm.tail(items)
	if err != nil {
		r.err = err
		return true
	}
	return s > 0
}

// Err returns the error latched by Add, Tail or Report when the
// closed/maximal repository outgrew its int32 node indices (ErrTooLarge)
// or the abort function failed (SetAbort), or nil. Once set, the
// reporter's results are incomplete and every Report returns the error;
// a miner should stop and return it.
func (r *Reporter) Err() error { return r.err }

// Len is a port of isr_cnt: the number of items in the current set.
func (r *Reporter) Len() int { return r.cnt }

// Items is a port of isr_items: the current item set. The slice aliases an
// internal buffer and is only valid until the next call that changes the
// reporter; it must not be modified.
func (r *Reporter) Items() []tract.Item { return r.items[:r.cnt:r.cnt] }

// Supp is a port of isr_supp: the support of the current item set.
func (r *Reporter) Supp() tract.Supp { return r.supps[r.cnt] }

// Total is the total transaction weight, the support of the empty set
// (upstream supps[0]).
func (r *Reporter) Total() tract.Supp { return r.supps[0] }

// PexCount is a port of isr_pexcnt: the number of perfect extensions of
// all levels.
func (r *Reporter) PexCount() int { return len(r.pexs) }

// Count is a port of isr_repcnt: the number of reported item sets and
// rules. It saturates at math.MaxUint64 (see Saturated).
func (r *Reporter) Count() uint64 { return r.repCnt }

// Stats is a port of isr_stats: the number of reported item sets (or
// rules) per size, up to the largest size with a non-zero count (empty if
// nothing was reported), as printed by isr_prstats. It returns a copy.
func (r *Reporter) Stats() []uint64 {
	n := len(r.stats)
	for n > 0 && r.stats[n-1] == 0 {
		n--
	}
	return slices.Clone(r.stats[:n:n])
}

// Saturated reports whether a set count (Count or Stats) reached
// math.MaxUint64 and was clamped. This needs astronomically large perfect
// extension hypercubes counted in count-only mode.
func (r *Reporter) Saturated() bool { return r.saturated }

// LogRatio is a port of isr_logrto, the evaluation of option -eb: the
// binary logarithm of the quotient of the support of the current set and
// the support expected under full independence of its items (0 for sets
// with fewer than two items). It needs the item frequencies passed to New.
func LogRatio(r *Reporter) float64 {
	if r.cnt <= 1 {
		return 0
	}
	sum := float64(r.supps[r.cnt])
	if sum > 0 {
		sum = math.Log(sum/float64(r.supps[0])) / ln2
	} else {
		sum = 0
	}
	for _, it := range r.items[:r.cnt] {
		sum -= r.ldps[it]
	}
	return sum
}

// ReportEval is a port of isr_reportv: Report with an evaluation value that
// is passed to the sink (unless an evaluation function replaces it).
func (r *Reporter) ReportEval(eval float64) error {
	r.eval = eval
	return r.Report()
}

// Report is a port of isr_report: it reports the current item set together
// with its perfect extensions. For closed and maximal targets the
// repository is consulted and updated first, then the support and size
// gates are applied; the set is reported unexpanded with all perfect
// extensions (ModeNoExpand). Otherwise every set of the perfect-extension
// hypercube within the size limits is reported, depth first, supersets
// before subsets, the oldest remaining extension first. Each set passes the
// border and the evaluation before it is counted and sent to the sink.
//
// Report returns the first error returned by the sink, or the latched
// repository or abort error (see Err, SetAbort).
func (r *Reporter) Report() error {
	if r.err != nil {
		return r.err
	}
	if err := r.tick(); err != nil {
		return err
	}
	n := len(r.pexs)
	s := r.supps[r.cnt]
	var set []tract.Item // closed/maximal: the set with its perfect extensions
	if r.cm != nil {
		rs := r.cm.supp()
		if r.target == TargetMaximal && r.cnt == 0 && n > 0 && !r.cm.stored {
			// Deviation from upstream (fpgrowth-core.md B4): for the empty
			// prefix cm_supp is the root support of the repository, which
			// starts at 0, so upstream's test r >= sto (sto = 0 for
			// maximal) rejects the set of the top-level perfect extensions
			// even when nothing has been stored, i.e. when it is the only
			// maximal item set ("a\na b\na c\na" with -tm -s-2 prints
			// nothing instead of "a (4)"). Every stored set contains all
			// top-level perfect extensions, so the set is maximal exactly
			// if nothing has been stored. The empty set itself (n == 0)
			// is still never maximal.
			rs = cmNone
		}
		if rs >= s || rs >= r.sto {
			return nil
		}
		set = r.items[:r.cnt]
		if n > 0 {
			k := r.cnt + n
			set = r.iset[:k]
			copy(set, r.pexs)
			copy(set[n:], r.items[:r.cnt])
			sortItems(set, r.dir)
		}
		if err := r.cm.update(set, s); err != nil {
			r.err = err
			return err
		}
		if r.target == TargetMaximal && rs >= 0 {
			return nil
		}
	}
	if s < r.smin || s > r.smax {
		return nil
	}
	if r.cnt >= r.pexMax {
		// Extension, not in upstream: no perfect extension fits into a
		// set of zmax items (see PexUseful; the extensions inherited
		// from smaller sets are skipped here). Only the set itself can be
		// reported, as without the extensions: the gate below rejects it
		// for zmin > zmax either way, countOnly counts only size cnt, and
		// the fill and expand below would only reject.
		n = 0
	}
	if r.cnt > r.zmax || r.cnt+n < r.zmin {
		return nil
	}
	if r.cmCheck != nil && r.cm != nil && r.cnt+n <= r.zmax && !r.cmCheck(set, s) {
		return nil // not closed/maximal: an extension the repository lacks
	}
	if r.sink == nil && len(r.border) == 0 && r.evalFn == nil {
		r.countOnly(n, s)
		return nil
	}
	if r.mode&ModeNoExpand != 0 {
		k := r.cnt + n
		if k > r.zmax {
			return nil
		}
		r.supps[k] = r.supps[r.cnt]
		r.wgts[k] = r.wgts[r.cnt]
		base := r.cnt
		copy(r.items[base:k], r.pexs)
		r.cnt = k
		err := r.output()
		r.cnt = base
		return err
	}
	w := r.wgts[r.cnt]
	for k := 1; k <= n; k++ {
		r.supps[r.cnt+k] = s
		r.wgts[r.cnt+k] = w
	}
	// Deviation from upstream: upstream has a second, "fast" output path
	// (fastout) that emits every set of the hypercube without a size
	// check and therefore reports and counts the empty set when there
	// are top-level perfect extensions and zmin is 1 (report.md B3).
	// There is a single path here; the empty set is reported only if
	// zmin <= 0, as on upstream's normal path.
	return r.expand(0)
}

// countOnly is the count-only branch of isr_report (no sink, no border, no
// evaluation): it counts the n-item perfect-extension hypercube of the
// current set (support s) without enumerating it.
func (r *Reporter) countOnly(n int, s tract.Supp) {
	if r.mode&ModeNoExpand != 0 {
		z := r.cnt + n
		// Deviation from upstream: upstream counts the unexpanded
		// closed/maximal set here even if it has more than zmax items,
		// unlike its output path (report.md B4).
		if z > r.zmax {
			return
		}
		r.countSets(z, s, 1)
		return
	}
	z := r.cnt
	if z >= r.zmin {
		r.countSets(z, s, 1)
	}
	// Deviation from upstream: upstream computes C(n,k) as
	// c*(n-k+1)/k in size_t, which overflows for n >= 63 although the
	// result fits (report.md B5). Here C(n,k) is computed exactly with a
	// 128-bit intermediate, using C(n,k) = C(n,n-k) past the maximum,
	// and saturates at math.MaxUint64.
	half := n / 2
	r.binom = append(r.binom[:0], 1)
	c, sat := uint64(1), false
	for k := 1; k <= n; k++ {
		z++
		if z > r.zmax {
			break
		}
		if k <= half {
			if !sat {
				hi, lo := bits.Mul64(c, uint64(n-k+1))
				if hi >= uint64(k) {
					c, sat = math.MaxUint64, true
				} else {
					c, _ = bits.Div64(hi, lo, uint64(k))
				}
			}
			r.binom = append(r.binom, c)
		} else {
			c = r.binom[n-k]
		}
		if z >= r.zmin {
			r.countSets(z, s, c)
		}
	}
}

// expand is a port of report(): it reports the current set and, depth
// first, its extensions by the perfect extensions pexs[next:], supersets
// before the set itself and the oldest remaining extension first.
//
// Every visited set is a step of the abort check (SetAbort), so a
// cancellation stops the expansion of a large hypercube after at most
// AbortEvery sets.
func (r *Reporter) expand(next int) error {
	if err := r.tick(); err != nil {
		return err
	}
	n := len(r.pexs)
	if r.cnt >= r.pexMax {
		// Extension, not in upstream: with zmax items no extension fits
		// (r.cnt+1 > zmax below), so the loop would only write the
		// scratch entry items[cnt] and reject every extension; skip it.
		n = next
	}
	for j := next; j < n; j++ {
		r.items[r.cnt] = r.pexs[j]
		r.cnt++
		if r.cnt+(n-j-1) >= r.zmin && r.cnt <= r.zmax {
			if err := r.expand(j + 1); err != nil {
				r.cnt--
				return err
			}
		}
		r.cnt--
	}
	if r.cnt >= r.zmin {
		return r.output()
	}
	return nil
}

// output is a port of the filtering part of output(): border, evaluation,
// counting, then the sink.
func (r *Reporter) output() error {
	z := r.cnt
	s := r.supps[z]
	if z < len(r.border) && s < r.border[z] {
		return nil
	}
	if r.evalFn != nil {
		r.eval = r.evalFn(r)
		if float64(r.evalDir)*r.eval < r.evalThresh {
			return nil
		}
	}
	r.countSets(z, s, 1)
	if r.sink == nil {
		return nil
	}
	return r.sink.ItemSet(ItemSet{
		Items:  r.items[:z:z],
		Supp:   s,
		Weight: r.wgts[z],
		Eval:   r.eval,
	})
}

// ReportRule is a port of isr_reprule: it reports the association rule
// with head head (an item of the current set) and the other items of the
// current set as body. body and headSupp are the supports of the body and
// the head. Only the support and size gates are applied (size counts the
// head); confidence and evaluation thresholds are the caller's business.
// It returns the error of the sink, if any.
func (r *Reporter) ReportRule(head tract.Item, body, headSupp tract.Supp, eval float64) error {
	n := r.cnt
	supp := r.supps[n]
	if supp < r.smin || supp > r.smax {
		return nil
	}
	if n < r.zmin || n > r.zmax {
		return nil
	}
	r.countSets(n, supp, 1)
	if r.sink == nil {
		return nil
	}
	b := r.body[:0]
	for _, it := range r.items[:n] {
		if it != head {
			b = append(b, it)
		}
	}
	r.body = b
	return r.sink.Rule(Rule{
		Head:     head,
		Body:     b[:len(b):len(b)],
		Supp:     supp,
		BodySupp: body,
		HeadSupp: headSupp,
		Eval:     eval,
	})
}

// countSets adds c sets of size z and support s to the statistics and the
// pattern spectrum.
func (r *Reporter) countSets(z int, s tract.Supp, c uint64) {
	var sat1, sat2 bool
	r.stats[z], sat1 = satAdd(r.stats[z], c)
	r.repCnt, sat2 = satAdd(r.repCnt, c)
	r.saturated = r.saturated || sat1 || sat2
	if r.psp != nil {
		r.psp.Add(z, s, c)
	}
}

// isGenerator is a port of is_isgen: it decides whether the current set
// extended by item (support supp) is a generator, i.e. every subset with
// one item less, except the current set itself, is a stored generator with
// a different support. If so, the extended set is stored.
func (r *Reporter) isGenerator(item tract.Item, supp tract.Supp) bool {
	c := r.cnt
	key := r.iset[:c+1]
	copy(key, r.items[:c])
	key[c] = item
	if c > 0 {
		if r.mode&ModeSort != 0 {
			sortItems(key, r.dir)
		}
		// Rotate a hold-out item through the buffer so that every
		// subset key key[:c] keeps the item order of the full set.
		a := key[c]
		for i := c; i >= 0; i-- {
			key[i], a = a, key[i]
			if a == item {
				continue
			}
			if st, ok := r.gen.lookup(key[:c]); !ok || st == supp {
				return false
			}
		}
		copy(key[1:], key[:c])
		key[0] = a
	}
	r.gen.insert(key, supp)
	return true
}

// sortItems is the equivalent of ia_qsort(items, n, dir): descending for
// dir < 0, ascending otherwise.
func sortItems(items []tract.Item, dir int) {
	slices.Sort(items)
	if dir < 0 {
		slices.Reverse(items)
	}
}

// satAdd returns a+b, clamped to math.MaxUint64, and whether it clamped.
func satAdd(a, b uint64) (uint64, bool) {
	s, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		return math.MaxUint64, true
	}
	return s, false
}
