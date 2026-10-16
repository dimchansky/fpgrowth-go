package difftest

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

// Large-data mode: cases with 40-1000 items and 500-8000 transactions,
// beyond the brute force (maxBruteItems), compared with the C reference
// only. Options that trigger a documented deviation are avoided (the C
// output would not be authoritative and there is no brute force to
// decide), so every case must be byte-equal or semantically equal within
// the tolerances of compareC.

// largeGen selects the dataset generator of a large case.
type largeGen uint8

const (
	largeZipf  largeGen = iota // gen.go with a Zipf-like item popularity and planted patterns
	largeEqual                 // every item in exactly k transactions (genEqualFreq)
	largeBlock                 // co-occurring item blocks (genBlocks)
	numLargeGens
)

func (g largeGen) String() string { return [...]string{"zipf", "equal", "block"}[g] }

// largeMeasures are the evaluation measures of large cases: those
// without a value-level deviation (R4-R9) and without Fisher's tests
// (too slow for large total weights). 'b' is added for item sets only.
const largeMeasures = "ocdlaqerkwujnpg"

// largeCase is a large case and the facts its options were checked
// against.
type largeCase struct {
	*tcase
	kind    largeGen
	items   int
	freqs   []int64 // item frequencies, descending
	nFreq   int     // items with frequency >= smin
	univers bool    // an item occurs in every transaction
}

// newLargeCase derives large case index of base seed base. Targets,
// variants and generators are cycled.
func newLargeCase(base uint64, index int) (*largeCase, error) {
	seed := base*0xd6e8feb86659fd93 + uint64(index)*0x9e3779b97f4a7c15 + 7
	r := newRNG(seed)
	c := &tcase{index: index, seed: seed}
	lc := &largeCase{tcase: c, kind: largeGen(index % int(numLargeGens))}
	numItems := 40 + r.intN(961)
	numTx := 500 + r.intN(7501)
	lc.items = numItems
	style := nameStyle(r.intN(int(numNameStyles)))
	names := make([]string, numItems)
	for i := range names {
		names[i] = style.itemName(i)
	}
	c.weighted = chance(r, 0.2)
	g := genConfig{numItems: numItems, numTransactions: numTx, names: style}
	c.gen = g
	switch lc.kind {
	case largeZipf:
		g.seed = r.src.Uint64()
		g.skew = 1 + r.intN(2)
		g.minLen = r.intN(4)
		g.maxLen = g.minLen + 3 + r.intN(20)
		g.patterns = r.intN(6)
		if g.patterns > 0 {
			g.patternLen = 2 + r.intN(min(8, numItems-1))
			g.patternProb = pick[float64](r, 0.05, 0.1, 0.3)
		}
		g.dupTxProb = pick[float64](r, 0, 0.05, 0.2)
		g.weights = weightsUnit
		if c.weighted {
			g.weights = weightsSmall
		}
		txs, err := generate(g)
		if err != nil {
			return nil, err
		}
		c.txs = txs
	case largeEqual:
		k := max(2, int(float64(numTx)*pick(r, 0.02, 0.05, 0.1)))
		c.txs = genEqualFreq(r, names, numTx, k, c.weighted)
	case largeBlock:
		c.txs = genBlocks(r, names, numTx, 3+r.intN(10), pick(r, 0.6, 0.8, 0.95, 1.0), r.intN(4))
		if c.weighted {
			for i := range c.txs {
				c.txs[i].weight = 1 + int64(r.intN(5))
			}
		}
	}
	c.gen = g
	total := c.total()

	freq := map[string]int64{}
	for _, tx := range c.txs {
		seen := map[string]bool{}
		for _, it := range tx.items {
			if !seen[it] {
				seen[it] = true
				freq[it] += tx.weight
			}
		}
	}
	for _, f := range freq {
		lc.freqs = append(lc.freqs, f)
		lc.univers = lc.univers || (f == total && total > 0)
	}
	slices.SortFunc(lc.freqs, func(a, b int64) int { return int(b - a) })
	if len(lc.freqs) == 0 {
		return nil, fmt.Errorf("large case %d: no items", index)
	}

	// target and variant
	c.target = "scmgr"[index/int(numLargeGens)%5]
	c.algo = "scdt"[index/int(numLargeGens)/5%4]
	if chance(r, 0.1) {
		c.algo = 0
	}
	c.noPerfect = chance(r, 0.25)
	c.noReorder = chance(r, 0.25)
	c.noHUT = chance(r, 0.25)
	c.pack = pick(r, -1, -1, 0, 16, 1+r.intN(15))
	c.sortMode = 2
	if chance(r, 0.5) {
		c.hasSort, c.sortMode = true, pick(r, 0, 1, -1, 2, -2, 3)
	}

	// The minimum support keeps the output moderate: at the frequency of
	// the k-th most frequent item, with fewer items for the targets that
	// report every frequent set or rule.
	maxK := 18
	switch c.target {
	case 'c', 'm':
		maxK = 40
	case 'r':
		maxK = 14
	}
	k := min(len(lc.freqs), 3+r.intN(maxK-2))
	smin := max(2, lc.freqs[k-1])
	if lc.kind == largeEqual {
		// all items tie, so the support decides how many pairs are
		// frequent (the expected support of a pair is k^2/numTx)
		frac := pick[float64](r, 0.1, 0.3, 0.6, 1)
		if c.target == 'r' {
			frac = max(frac, 0.3)
		}
		smin = max(2, int64(float64(lc.freqs[0])*frac))
	}
	if chance(r, 0.5) {
		c.sArg = -float64(smin)
	} else {
		c.sArg = float64(smin) * 100 / float64(total) // relative; rounds up to about smin
	}
	c.zmin, c.zmax = 1, -1
	if chance(r, 0.3) {
		c.hasZMin, c.zmin = true, pick(r, 0, 1, 2, 3)
	}
	if c.target != 'c' && c.target != 'm' && chance(r, 0.4) { // M5/O4: no -n for closed/maximal
		c.zmax = pick(r, 1, 2, 3, 4, 5)
	}
	if c.target == 'r' && c.zmax < 0 {
		c.zmax = pick(r, 2, 3, 4) // rules multiply the output
	}
	if !c.isRules() && chance(r, 0.15) {
		for range 1 + r.intN(3) {
			c.border = append(c.border, -float64(smin+int64(r.intN(int(smin)+1))))
		}
	}
	if chance(r, 0.15) && (!c.isRules() || chance(r, 0.5)) {
		c.hasSMax, c.sMaxArg = true, pick[float64](r, 30, 50, 80, 95)
	}
	c.conf = 80
	if c.isRules() {
		c.hasConf, c.conf = true, pick[float64](r, 10, 25, 50, 60, 80, 90, 100)
		c.orig = chance(r, 0.3)
	}

	// evaluation
	c.eval, c.agg = 'x', 'x'
	if chance(r, 0.25) {
		measures := largeMeasures
		if !c.isRules() {
			measures += "b"
		}
		c.eval = measures[r.intN(len(measures))]
		if !c.isRules() && chance(r, 0.5) {
			c.agg = pick[byte](r, 'x', 'm', 'n', 'a')
		}
		c.hasThresh = true
		if strings.IndexByte(minMeasures, c.eval) >= 0 {
			c.thresh = pick[float64](r, 1, 5, 50)
		} else {
			c.thresh = pick[float64](r, 0, 1, 5, 10, 20, 50, 100, 150)
		}
		c.invBxs = chance(r, 0.2)
		if c.isRules() && chance(r, 0.3) { // R2: no -p for item sets
			c.hasPrune, c.prune = true, pick(r, -1, 0, 1, 2)
		}
	}

	// output
	c.sep, c.imp = " ", " <- "
	if chance(r, 0.15) {
		c.sep = ","
	}
	if c.isRules() && chance(r, 0.15) {
		c.imp = " => "
	}
	c.info = pickFormat(r, c)
	c.countOnly = chance(r, 0.1)
	c.stats = chance(r, 0.3)
	c.spectrum = !c.isRules() && chance(r, 0.3)
	smin = max(absMinSupp(c.sArg, total), 1)
	for _, f := range lc.freqs {
		if f >= smin {
			lc.nFreq++
		}
	}
	lc.avoidDeviations()
	return lc, nil
}

// avoidDeviations changes options that would trigger a documented
// deviation (caseDeviations) on this data.
func (lc *largeCase) avoidDeviations() {
	c := lc.tcase
	if c.isRules() && c.hasSMax && !c.orig { // R8
		c.hasSMax = false
	}
	if c.target == 'g' && c.zmin >= 2 { // M3
		c.zmin = 1
	}
	if c.treePath() && c.zmin > lc.nFreq { // R11
		c.zmin = 1
	}
	if c.target == 'm' && c.noPerfect && (c.algo == 's' || c.algo == 'd') && c.pack != 0 { // M9
		c.noPerfect = false
	}
	if !c.treePath() && c.algo == 't' && c.target != 'g' && lc.topLevelElimination() { // M1
		c.algo = 'c'
	}
	fast := !c.isRules() && !c.countOnly && c.info.String() == " (%a)" && c.sep == " " &&
		c.zmin <= 1 && c.zmax < 0 && len(c.border) == 0 && c.eval != 'b'
	if fast && (c.spectrum || lc.univers) { // O3
		c.info.open, c.info.close = " [", "]"
	}
	if c.target == 'm' && lc.univers { // M4 (a sufficient condition is cheap to avoid)
		c.target = 'c'
	}
}

// topLevelElimination is the M1 condition of deviations.go for a large
// case (without the brute-force table).
func (lc *largeCase) topLevelElimination() bool {
	c := lc.tcase
	smin := max(absMinSupp(c.sArg, c.total()), 1)
	all := map[string]int64{}
	for _, tx := range c.txs {
		seen := map[string]bool{}
		for _, it := range tx.items {
			if !seen[it] {
				seen[it] = true
				all[it] += tx.weight
			}
		}
	}
	coded := map[string]bool{}
	for it, f := range all {
		if f >= smin {
			coded[it] = true
		}
	}
	freq := map[string]int64{}
	total := c.total()
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
		case f < smin, !c.noPerfect && f >= total:
			elim++
		default:
			kept++
		}
	}
	return kept > 0 && elim > 0
}

// describe returns a one-line summary of the large case.
func (lc *largeCase) describe() string {
	return fmt.Sprintf("large case %d (seed %#x, %s, %d transactions, %d items): %s",
		lc.index, lc.seed, lc.kind, len(lc.txs), lc.items, lc.command())
}

// evaluateLarge compares the C and Go runs of a large case: no
// deviations, no brute force; the outputs must be byte-equal or
// semantically equal (compareC), and the Go output consistent
// (selfCheck).
func evaluateLarge(c *tcase, cr, gr *runResult, rerun rerunFunc) *outcome {
	o := &outcome{rerun: rerun}
	switch {
	case cr.runErrMsg != "" || gr.runErrMsg != "":
		o.fail("cannot run: C %q, Go %q", cr.runErrMsg, gr.runErrMsg)
		return o
	case gr.signaled:
		o.fail("Go crashed: %s", lastLine(gr.stderr))
		return o
	case cr.signaled || cr.exit != gr.exit:
		o.fail("exit status: C %d (signal %v), Go %d\nC stderr: %s\nGo stderr: %s",
			cr.exit, cr.signaled, gr.exit, lastLine(cr.stderr), lastLine(gr.stderr))
		return o
	case gr.exit != 0:
		if !bytes.Equal(cr.stderr, gr.stderr) {
			o.fail("both fail with exit %d, different messages:\nC:  %q\nGo: %q", gr.exit, cr.stderr, gr.stderr)
			return o
		}
		o.verdict = verdictErrorEqual
		return o
	}
	o.byteEqual = bytes.Equal(cr.stdout, gr.stdout) && bytes.Equal(cr.stderr, gr.stderr) &&
		bytes.Equal(cr.out, gr.out) && bytes.Equal(cr.psp, gr.psp) && cr.hasOut == gr.hasOut && cr.hasPsp == gr.hasPsp
	if c.ruleEval() && c.hasPrune && c.prune > 0 { // R2's log line (rules are not pruned)
		cr.stderr = filterLogRE.ReplaceAll(cr.stderr, nil)
		gr.stderr = filterLogRE.ReplaceAll(gr.stderr, nil)
	}
	o.selfCheck(c, gr)
	if !o.byteEqual {
		db := &bfDB{total: c.total()} // compareC needs only the total weight here
		o.failures = append(o.failures, diffMessages(o.compareC(c, db, cr, gr))...)
		o.firstDiff = firstDiff(cr, gr)
	}
	switch {
	case len(o.failures) > 0:
	case o.byteEqual:
		o.verdict = verdictByteEqual
	case o.tolerated > 0 || o.boundary > 0 || o.reruns > 0:
		o.verdict = verdictTolerated
	default:
		o.verdict = verdictSemantic
	}
	return o
}
