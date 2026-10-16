package difftest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Fixed file names of a case directory; the binaries run inside it, so
// that both see (and print) the same relative names.
const (
	dataFile = "data.tab"
	outFile  = "out.txt"
	selFile  = "sel.txt"
	pspFile  = "psp.txt"
)

// rule-based evaluation measures (-e letters other than x and b for item
// sets; b is the imbalance ratio for rules, see R1).
const ruleMeasures = "ocdlaqverbkwujznpytigfhms"

// minimizing measures, marked (-) in the -! help.
const minMeasures = "ptgfhms"

// Fisher's exact tests, and the largest total weight they are used with.
const (
	fisherMeasures = "fhms"
	fisherMaxTotal = 20000
)

// logMeasures are the measures computed from sums of logarithms or
// logGamma values of the counts, or from incomplete gamma functions of a
// statistic that grows with the total weight (see measureTol).
const logMeasures = "ptgifhms"

// tcase is one differential test case: a dataset and an option set. All
// fields are derived from the seed.
type tcase struct {
	index int
	seed  uint64

	gen      genConfig
	weighted bool
	txs      []transaction

	target    byte // s c m g r
	algo      byte // s c d t, 0 = default (c)
	noPerfect bool // -x
	noReorder bool // -j
	noHUT     bool // -u
	pack      int  // -l, -1 = default (16)
	sortMode  int  // -q, 2 = default
	hasSort   bool

	sArg      float64 // -s
	hasSMax   bool
	sMaxArg   float64 // -S
	zmin      int     // -m (default 1)
	hasZMin   bool
	zmax      int // -n, -1 = none
	border    []float64
	conf      float64 // -c (default 80)
	hasConf   bool
	orig      bool // -o
	eval      byte // -e, 'x' = none
	agg       byte // -a, 'x' = default
	thresh    float64
	hasThresh bool
	invBxs    bool // -i
	prune     int  // -p
	hasPrune  bool

	selection []string              // -R for item sets
	defApp    string                // -R for rules: default indicator
	appear    map[string]string     // -R for rules: item -> indicator
	appearLst []string              // item order in the appearance file
	appVals   map[string]appearance // parsed appearances (BF)
	defAppVal appearance

	info      infoFormat
	sep, imp  string
	countOnly bool // no output file
	stats     bool // -Z
	spectrum  bool // -P
}

// isRules reports whether the target is association rules.
func (c *tcase) isRules() bool { return c.target == 'r' }

// ruleEval reports whether a rule-based evaluation measure is selected.
func (c *tcase) ruleEval() bool {
	return c.eval != 'x' && (c.isRules() || c.eval != 'b')
}

// treePath reports whether mining goes through the item set tree.
func (c *tcase) treePath() bool { return c.isRules() || c.ruleEval() }

// total returns the total transaction weight.
func (c *tcase) total() int64 {
	var w int64
	for _, tx := range c.txs {
		w += tx.weight
	}
	return w
}

// pick returns one of the choices.
func pick[T any](r *rng, choices ...T) T { return choices[r.intN(len(choices))] }

// chance returns true with probability p.
func chance(r *rng, p float64) bool { return r.float() < p }

// newCase derives case index of a sweep with base seed base. Targets and
// variants are cycled so that every combination occurs evenly.
func newCase(base uint64, index int) (*tcase, error) {
	seed := base*0x9e3779b97f4a7c15 + uint64(index)*0xbf58476d1ce4e5b9 + 1
	r := newRNG(seed)
	c := &tcase{index: index, seed: seed}

	// dataset
	g := genConfig{
		seed:            r.src.Uint64(),
		numTransactions: pick(r, 5, 8, 12, 20, 30, 50, 80, 120, 200),
		numItems:        1 + r.intN(14),
		skew:            r.intN(4),
		dupItemProb:     pick[float64](r, 0, 0, 0.05, 0.2),
		dupTxProb:       pick[float64](r, 0, 0.1, 0.3, 0.6),
		emptyProb:       pick[float64](r, 0, 0, 0.05, 0.2),
		names:           nameStyle(r.intN(int(numNameStyles))),
	}
	if chance(r, 0.08) { // tiny and empty databases
		g.numTransactions = r.intN(4)
	}
	g.minLen = r.intN(3)
	g.maxLen = g.minLen + r.intN(8)
	if chance(r, 0.6) {
		g.patterns = 1 + r.intN(3)
		g.patternLen = 1 + r.intN(min(5, g.numItems))
		g.patternProb = pick[float64](r, 0.2, 0.5, 0.8, 1.0)
	}
	switch k := r.intN(10); {
	case k < 6:
		g.weights = weightsUnit
	case k < 8:
		g.weights = weightsSmall
	case k < 9:
		g.weights = weightsWithZero
	default:
		g.weights = weightsLarge
		g.maxWeight = pick[int64](r, 1000, 1<<20)
	}
	c.gen = g
	c.weighted = g.weights != weightsUnit || chance(r, 0.1)
	txs, err := generate(g)
	if err != nil {
		return nil, err
	}
	c.txs = txs
	total := c.total()

	// target and variant
	c.target = "scmgr"[index%5]
	c.algo = "scdt"[index/5%4]
	if chance(r, 0.1) {
		c.algo = 0
	}
	c.noPerfect = chance(r, 0.25)
	c.noReorder = chance(r, 0.25)
	c.noHUT = chance(r, 0.25)
	c.pack = pick(r, -1, -1, 0, 16, 1+r.intN(15))
	c.sortMode = 2
	if chance(r, 0.4) {
		c.hasSort, c.sortMode = true, pick(r, 0, 1, -1, 2, -2, 3)
	}

	// supports
	switch k := r.intN(20); {
	case k == 0:
		c.sArg = 0 // M2
	case k < 9:
		c.sArg = -float64(1 + r.intN(int(max(1, min(total, 12)))))
	default:
		c.sArg = pick[float64](r, 1, 5, 10, 12.5, 20, 25, 30, 33.3, 40, 50, 60, 75, 90, 100)
	}
	if chance(r, 0.2) {
		c.hasSMax = true
		if chance(r, 0.5) {
			c.sMaxArg = pick[float64](r, 30, 50, 66.6, 80, 95, 100, 0)
		} else {
			c.sMaxArg = -float64(r.intN(int(max(1, total)) + 1))
		}
	}
	c.zmin, c.zmax = 1, -1
	if chance(r, 0.35) {
		c.hasZMin, c.zmin = true, pick(r, 0, 0, 1, 2, 2, 3, 4)
	}
	if chance(r, 0.3) {
		c.zmax = pick(r, 0, 1, 2, 2, 3, 3, 4, 5)
	}
	if chance(r, 0.15) {
		n := 1 + r.intN(3)
		for range n {
			if chance(r, 0.5) {
				c.border = append(c.border, pick[float64](r, 5, 10, 20, 30, 50, 70))
			} else {
				c.border = append(c.border, -float64(1+r.intN(int(max(1, min(total, 10))))))
			}
		}
	}
	c.conf = 80
	if c.isRules() || chance(r, 0.05) {
		if chance(r, 0.8) {
			c.hasConf, c.conf = true, pick[float64](r, 0, 10, 25, 33.3, 50, 60, 70, 80, 90, 100)
		}
	}
	c.orig = c.isRules() && chance(r, 0.3)

	// evaluation
	c.eval, c.agg = 'x', 'x'
	if chance(r, 0.3) {
		if !c.isRules() && chance(r, 0.25) {
			c.eval = 'b'
		} else {
			c.eval = ruleMeasures[r.intN(len(ruleMeasures))]
			// Fisher's tests sum over all tables of a rule: O(total
			// weight) logGamma calls per rule, too slow for large weights
			for strings.IndexByte(fisherMeasures, c.eval) >= 0 && total > fisherMaxTotal {
				c.eval = ruleMeasures[r.intN(len(ruleMeasures))]
			}
		}
		if !c.isRules() && chance(r, 0.6) {
			c.agg = pick[byte](r, 'x', 'm', 'n', 'a')
		}
		if chance(r, 0.8) {
			c.hasThresh = true
			if strings.IndexByte(minMeasures, c.eval) >= 0 {
				c.thresh = pick[float64](r, 0.1, 1, 5, 10, 50, 100)
			} else {
				c.thresh = pick[float64](r, -10, 0, 1, 5, 10, 20, 50, 100, 150, 300)
			}
		} else {
			c.thresh = 10
		}
		c.invBxs = chance(r, 0.2)
		if chance(r, 0.3) {
			c.hasPrune, c.prune = true, pick(r, -1, 0, 1, 2, 3)
		}
	}

	// item selection or appearances
	if chance(r, 0.15) {
		universe := make([]string, 0, g.numItems+2)
		for i := range g.numItems + 2 { // two names absent from the data
			universe = append(universe, g.names.itemName(i))
		}
		if c.isRules() {
			c.defApp = pick(r, "both", "in", "out", "none", "x", "i", "o", "-")
			c.appear = map[string]string{}
			for _, it := range universe {
				if chance(r, 0.4) {
					c.appear[it] = pick(r, "both", "body", "head", "none", "in", "out", "ante", "cons", "ignore", "io")
					c.appearLst = append(c.appearLst, it)
				}
			}
			c.parseAppearances()
		} else {
			for _, it := range universe {
				if chance(r, 0.6) {
					c.selection = append(c.selection, it)
				}
			}
			if len(c.selection) == 0 {
				c.selection = append(c.selection, universe[0])
			}
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
	c.countOnly = chance(r, 0.05)
	c.stats = chance(r, 0.2)
	c.spectrum = !c.isRules() && chance(r, 0.15)
	return c, nil
}

// pickFormat chooses a parseable info format that contains the absolute
// supports and, with an evaluation measure, the measure with 17 digits.
func pickFormat(r *rng, c *tcase) infoFormat {
	f := func(open, close string, codes ...string) infoFormat {
		fm := infoFormat{open: open, close: close}
		for _, code := range codes {
			d, _ := strconv.Atoi(code[:len(code)-1])
			fm.fields = append(fm.fields, infoField{code: code[len(code)-1], digits: d})
		}
		return fm
	}
	if c.isRules() {
		choices := []infoFormat{
			f(" (", ")", "a", "b", "h"),
			f(" (", ")", "a", "b", "h", "17c", "17l"),
			f(" [", "]", "a", "b", "h", "X", "C", "L", "s", "y", "i", "Q"),
			f(" (", ")", "a", "b", "h", "4C", "9Y", "17x", "3l"),
		}
		fm := pick(r, choices...)
		if c.eval != 'x' {
			fm.fields = append(fm.fields, infoField{code: 'e', digits: 17})
			if chance(r, 0.3) {
				fm.fields = append(fm.fields, infoField{code: 'E', digits: 0})
			}
		}
		return fm
	}
	choices := []infoFormat{
		f(" (", ")", "a"), // upstream's fast output format
		f(" (", ")", "a"),
		f(" [", "]", "a"),
		f(" (", ")", "a", "S"),
		f(" (", ")", "a", "17s", "i", "Q"),
		f(" [", "]", "a", "3S", "11s", "i"),
	}
	fm := pick(r, choices...)
	if c.eval != 'x' {
		if fm.String() == " (%a)" && chance(r, 0.5) {
			return fm // no measure in the output
		}
		fm.fields = append(fm.fields, infoField{code: 'e', digits: 17})
		if chance(r, 0.3) {
			fm.fields = append(fm.fields, infoField{code: 'E', digits: 0})
		}
	}
	return fm
}

// parseAppearances maps the indicators of the appearance file like
// upstream's appcode (prefix match against the sorted indicator list).
func (c *tcase) parseAppearances() {
	c.defAppVal = appearanceOf(c.defApp)
	c.appVals = map[string]appearance{}
	for it, ind := range c.appear {
		c.appVals[it] = appearanceOf(ind)
	}
}

// appearanceOf resolves the indicators used by newCase.
func appearanceOf(ind string) appearance {
	switch ind {
	case "both", "x", "io":
		return appBoth
	case "in", "i", "body", "ante":
		return appBody
	case "out", "o", "head", "cons":
		return appHead
	default: // none, -, ignore
		return appNone
	}
}

// fmtNum formats an option value.
func fmtNum(x float64) string { return strconv.FormatFloat(x, 'g', -1, 64) }

// args returns the command line options and file arguments.
func (c *tcase) args() []string {
	a := []string{"-t" + string(c.target)}
	if c.algo != 0 {
		a = append(a, "-A"+string(c.algo))
	}
	if c.noPerfect {
		a = append(a, "-x")
	}
	if c.noReorder {
		a = append(a, "-j")
	}
	if c.noHUT {
		a = append(a, "-u")
	}
	if c.pack >= 0 {
		a = append(a, "-l"+strconv.Itoa(c.pack))
	}
	if c.hasSort {
		a = append(a, "-q"+strconv.Itoa(c.sortMode))
	}
	a = append(a, "-s"+fmtNum(c.sArg))
	if c.hasSMax {
		a = append(a, "-S"+fmtNum(c.sMaxArg))
	}
	if c.hasZMin {
		a = append(a, "-m"+strconv.Itoa(c.zmin))
	}
	if c.zmax >= 0 {
		a = append(a, "-n"+strconv.Itoa(c.zmax))
	}
	if len(c.border) > 0 {
		s := make([]string, len(c.border))
		for i, b := range c.border {
			s[i] = fmtNum(b)
		}
		a = append(a, "-F"+strings.Join(s, ":"))
	}
	if c.hasConf {
		a = append(a, "-c"+fmtNum(c.conf))
	}
	if c.orig {
		a = append(a, "-o")
	}
	if c.eval != 'x' {
		a = append(a, "-e"+string(c.eval))
		if c.agg != 'x' {
			a = append(a, "-a"+string(c.agg))
		}
		if c.hasThresh {
			a = append(a, "-d"+fmtNum(c.thresh))
		}
		if c.invBxs {
			a = append(a, "-i")
		}
		if c.hasPrune {
			a = append(a, "-p"+strconv.Itoa(c.prune))
		}
	}
	if c.selection != nil || c.appear != nil {
		a = append(a, "-R"+selFile)
	}
	if c.stats {
		a = append(a, "-Z")
	}
	if c.spectrum {
		a = append(a, "-P"+pspFile)
	}
	if c.sep != " " {
		a = append(a, "-k"+c.sep)
	}
	if c.imp != " <- " {
		a = append(a, "-I"+c.imp)
	}
	a = append(a, "-v"+c.info.String())
	if c.weighted {
		a = append(a, "-w")
	}
	a = append(a, dataFile)
	if !c.countOnly {
		a = append(a, outFile)
	}
	return a
}

// command returns a shell-quoted command line that reproduces the case
// inside its directory.
func (c *tcase) command() string {
	q := []string{"fpgrowth"}
	for _, a := range c.args() {
		if strings.ContainsAny(a, " ()[]%#<>|;&$'\"\\*?!,=") || a == "" {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		q = append(q, a)
	}
	return strings.Join(q, " ")
}

// writeFiles writes the dataset and the -R file into dir.
func (c *tcase) writeFiles(dir string) error {
	var buf bytes.Buffer
	if err := writeTable(&buf, c.txs, c.weighted); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, dataFile), buf.Bytes(), 0o644); err != nil {
		return err
	}
	switch {
	case c.selection != nil:
		// a leading blank keeps a name starting with '#' (the comment
		// character) from turning the record into a comment
		return os.WriteFile(filepath.Join(dir, selFile), []byte(" "+strings.Join(c.selection, " ")+"\n"), 0o644)
	case c.appear != nil:
		var b strings.Builder
		b.WriteString(c.defApp + "\n")
		for _, it := range c.appearLst {
			fmt.Fprintf(&b, " %s %s\n", it, c.appear[it])
		}
		return os.WriteFile(filepath.Join(dir, selFile), []byte(b.String()), 0o644)
	}
	return nil
}

// selectionSet returns the item selection as a set, or nil.
func (c *tcase) selectionSet() map[string]bool {
	if c.selection == nil {
		return nil
	}
	m := map[string]bool{}
	for _, it := range c.selection {
		m[it] = true
	}
	return m
}

// describe returns a one-line summary of the case for reports.
func (c *tcase) describe() string {
	return fmt.Sprintf("case %d (seed %#x, %d transactions, %d items, weights %d): %s",
		c.index, c.seed, len(c.txs), c.gen.numItems, c.gen.weights, c.command())
}

// sortedKeys returns the keys of m in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
