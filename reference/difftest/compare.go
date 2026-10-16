package difftest

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
)

// Float tolerances of the semantic comparison.
//
// Supports, sizes and the total weight are integers and compared exactly.
// Confidence, lift and relative supports are single divisions and
// multiplications of the same integers in the same order on both sides
// (the reference is built with -ffp-contract=off and the port avoids
// fused multiply-adds), so they agree to the bit. Evaluation measures call
// log, exp, lgamma-based and incomplete-gamma functions, where Apple's
// libm and Go's math package may differ by an ulp per call; the measures
// chain at most a few dozen such operations, some with cancellation
// (differences of logarithms, 1-p complements). A relative error of 1e-12
// leaves four orders of magnitude of headroom over 100 ulps (2.2e-14);
// the absolute floor 1e-15 covers values that cancel to about 0, which are
// O(1) quantities computed to about 1e-16 absolute accuracy.
const (
	relTol      = 1e-12
	absTol      = 1e-15
	boundaryTol = 1e-9 // relative to max(1, |threshold|), see onBoundary
)

// filterLogRE matches the log line of the evaluation filter (-p).
var filterLogRE = regexp.MustCompile(`(?m)^filtering with evaluation \.\.\. done \[T\]\.\n`)

// runResult is the observable result of one program run.
type runResult struct {
	exit      int  // exit status (-1 if killed by a signal)
	signaled  bool // crashed (SIGSEGV, SIGBUS, SIGABRT, ...)
	stdout    []byte
	stderr    []byte // timings normalized
	out       []byte
	psp       []byte
	hasOut    bool
	hasPsp    bool
	runErrMsg string // failure to start the program
}

// verdict classifies a case.
type verdict int

const (
	verdictFail       verdict = iota
	verdictByteEqual          // all outputs identical
	verdictSemantic           // same sets/rules and values, not byte-identical
	verdictTolerated          // equal within the float/boundary tolerances
	verdictErrorEqual         // both failed with the same message
	verdictDeviation          // documented deviation, Go verified by brute force
)

var verdictNames = [...]string{"FAIL", "byte-equal", "semantic-equal", "tolerated", "error-equal", "deviation-verified"}

func (v verdict) String() string { return verdictNames[v] }

// outcome is the result of comparing one case.
type outcome struct {
	verdict    verdict
	byteEqual  bool
	deviations []string // documented deviations that apply (case level)
	recordDevs int      // records excused by a value-level deviation (R4-R9)
	tolerated  int      // float values equal only within the tolerances
	boundary   int      // sets/rules on one side only, at the threshold
	cAgrees    bool     // deviation case: C agreed with Go anyway
	bfChecked  bool     // Go was checked against the brute force
	bfSubset   bool     // ... only as a subset (evaluation measure)
	maxRel     float64  // largest relative difference of equal 17-digit values (relTol)
	maxRelLog  float64  // ... of the logarithmic measures (measureTol)
	maxAbsLog  float64  // largest absolute difference of the logarithmic measures
	firstDiff  string   // first differing output line (not byte-equal cases)
	r2Log      bool     // the R2 log line was removed before comparing
	failures   []string
}

func (o *outcome) fail(format string, args ...any) {
	o.failures = append(o.failures, fmt.Sprintf(format, args...))
}

// evaluate compares the C and Go runs of a case.
func evaluate(c *tcase, cr, gr *runResult) *outcome {
	o := &outcome{}
	if cr.runErrMsg != "" || gr.runErrMsg != "" {
		o.fail("cannot run: C %q, Go %q", cr.runErrMsg, gr.runErrMsg)
		return o
	}
	var sel map[string]bool
	if !c.isRules() {
		sel = c.selectionSet()
	}
	db, err := newBF(c.txs, sel)
	if err != nil {
		o.fail("%v", err)
		return o
	}
	o.deviations = caseDeviations(c, db)
	o.byteEqual = !cr.signaled && cr.exit == gr.exit && bytes.Equal(cr.stdout, gr.stdout) &&
		bytes.Equal(cr.stderr, gr.stderr) && bytes.Equal(cr.out, gr.out) && bytes.Equal(cr.psp, gr.psp) &&
		cr.hasOut == gr.hasOut && cr.hasPsp == gr.hasPsp

	switch {
	case gr.signaled:
		o.fail("Go crashed: %s", lastLine(gr.stderr))
		return o
	case cr.signaled || cr.exit != gr.exit:
		if len(o.deviations) > 0 && gr.exit == 0 {
			// e.g. M1/M2: upstream crashes, the port must be right
			o.verifyGo(c, db, gr)
			o.finishDeviation()
			return o
		}
		if slices.Contains(o.deviations, "M2") && noUsableItems(c, db) &&
			strings.HasSuffix(string(gr.stderr), ": no (frequent) items found\n") {
			// M2: with a minimum support of at least 1 no item with
			// support 0 is frequent; upstream (-s0) keeps them
			o.bfChecked = true
			o.finishDeviation()
			return o
		}
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

	// both succeeded
	if c.ruleEval() && c.hasPrune && c.prune > 0 {
		// R2: the port filters with the evaluation for every prune value
		// (upstream only for -p <= 0) and logs the step
		cr.stderr = filterLogRE.ReplaceAll(cr.stderr, nil)
		gr.stderr = filterLogRE.ReplaceAll(gr.stderr, nil)
		o.r2Log = true
	}
	o.selfCheck(c, gr)
	diffs := o.compareC(c, db, cr, gr)
	if len(o.deviations) > 0 {
		o.cAgrees = len(diffs) == 0
		o.verifyGo(c, db, gr)
		o.finishDeviation()
		return o
	}
	o.failures = append(o.failures, diffs...)
	if !o.byteEqual {
		o.firstDiff = firstDiff(cr, gr)
		if o.firstDiff == "" && o.r2Log {
			o.firstDiff = `stderr: "filtering with evaluation" logged for -p > 0 (R2)`
		}
	}
	o.verifyGo(c, db, gr) // the brute force must agree as well
	if len(o.failures) > 0 {
		return o
	}
	switch {
	case o.byteEqual:
		o.verdict = verdictByteEqual
	case o.tolerated > 0 || o.boundary > 0 || o.recordDevs > 0:
		o.verdict = verdictTolerated
	default:
		o.verdict = verdictSemantic
	}
	return o
}

// finishDeviation sets the verdict of a case with documented deviations.
// The brute-force check decides; C failures are informational.
func (o *outcome) finishDeviation() {
	if len(o.failures) > 0 {
		return
	}
	if !o.bfChecked {
		o.fail("deviation %v: no brute-force check possible", o.deviations)
		return
	}
	o.verdict = verdictDeviation
}

// compareC compares the C output with the Go output semantically and
// returns the differences that no tolerance or value-level deviation
// explains. Malformed Go output is recorded as a failure directly.
func (o *outcome) compareC(c *tcase, db *bfDB, cr, gr *runResult) []string {
	var diffs []string
	failf := func(format string, args ...any) { diffs = append(diffs, fmt.Sprintf(format, args...)) }

	if c.countOnly {
		if !bytes.Equal(cr.stdout, gr.stdout) || !bytes.Equal(cr.stderr, gr.stderr) {
			failf("count-only output differs:\nC:  %q %q\nGo: %q %q", cr.stdout, cr.stderr, gr.stdout, gr.stderr)
		}
		return diffs
	}
	crecs, err := parseOutput(cr.out, c.info, c.isRules(), c.sep, c.imp)
	if err != nil {
		failf("C output: %v", err)
		return diffs
	}
	grecs, err := parseOutput(gr.out, c.info, c.isRules(), c.sep, c.imp)
	if err != nil {
		o.fail("Go output: %v", err)
		return diffs
	}
	dropFastEmpty := slices.Contains(o.deviations, "O3") && c.zmin >= 1
	if dropFastEmpty {
		crecs = slices.DeleteFunc(crecs, func(r record) bool { return len(r.items) == 0 })
	}
	cm, err1 := indexRecords(crecs)
	gm, err2 := indexRecords(grecs)
	if err1 != nil {
		failf("C output: %v", err1)
		return diffs
	}
	if err2 != nil {
		o.fail("Go output: %v", err2)
		return diffs
	}
	dev := measureDeviation(c)
	evalIdx := c.info.fieldIndex("e")
	membership := false // a set/rule is on one side only
	for _, key := range sortedKeys(cm) {
		cr := cm[key]
		gr, ok := gm[key]
		if !ok {
			membership = true
			switch {
			case o.onBoundary(c, cr, evalIdx):
				o.boundary++
			case dev != nil && dev.allowsMissing(c, db, cr, false):
				o.recordDevs++
			default:
				failf("only in C: %s", cr.line)
			}
			continue
		}
		for k, f := range c.info.fields {
			cv, gv := cr.values[k], gr.values[k]
			if isInt(f.code) {
				if cv != gv {
					failf("%%%c differs:\nC:  %s\nGo: %s", f.code, cr.line, gr.line)
				}
				continue
			}
			rt, at := relTol, absTol
			if f.code == 'e' || f.code == 'E' {
				rt = measureTol(c, db.total)
				switch {
				case strings.IndexByte(logMeasures, c.eval) >= 0:
					at = rt // p-values and information: O(1) scale
				case c.eval == 'b' || c.eval == 'j':
					// differences of binary logarithms of supports: the
					// terms are up to log2(N) each, about |S|+1 of them
					at = 64 * dblEpsilon * float64(maxBruteItems+1) * math.Log2(float64(db.total)+2)
				}
				if f.code == 'E' {
					at *= 100
				}
			}
			eq, tol, rel := floatsMatch(cv, gv, f.digits, rt, at)
			switch {
			case eq:
				if tol {
					o.tolerated++
				}
				switch {
				case f.digits < 17 || math.IsNaN(cv) || math.IsInf(cv, 0): // short format, or identical
				case at == absTol:
					o.maxRel = max(o.maxRel, rel)
				default:
					o.maxRelLog = max(o.maxRelLog, rel)
					o.maxAbsLog = max(o.maxAbsLog, math.Abs(cv-gv))
				}
			case (f.code == 'e' || f.code == 'E') && dev != nil && dev.allowsValue(c, db, cr, cv, gv, f.code == 'E'):
				o.recordDevs++
			default:
				failf("%%%d%c differs (rel %.3g):\nC:  %s\nGo: %s", f.digits, f.code, rel, cr.line, gr.line)
			}
		}
	}
	for _, key := range sortedKeys(gm) {
		if _, ok := cm[key]; ok {
			continue
		}
		membership = true
		gr := gm[key]
		switch {
		case o.onBoundary(c, gr, evalIdx):
			o.boundary++
		case dev != nil && dev.allowsMissing(c, db, gr, true):
			o.recordDevs++
		default:
			failf("only in Go: %s", gr.line)
		}
	}
	// the order of the common records must be the same
	var co, gorder []string
	for _, r := range crecs {
		if _, ok := gm[r.key]; ok {
			co = append(co, r.key)
		}
	}
	for _, r := range grecs {
		if _, ok := cm[r.key]; ok {
			gorder = append(gorder, r.key)
		}
	}
	if !slices.Equal(co, gorder) {
		failf("same records in a different order")
	}
	if !membership && !dropFastEmpty {
		if !bytes.Equal(cr.stdout, gr.stdout) {
			failf("stdout differs:\nC:  %q\nGo: %q", cr.stdout, gr.stdout)
		}
		if !bytes.Equal(cr.stderr, gr.stderr) {
			failf("stderr differs:\nC:  %q\nGo: %q", cr.stderr, gr.stderr)
		}
		if !bytes.Equal(cr.psp, gr.psp) {
			failf("pattern spectrum differs:\nC:  %q\nGo: %q", cr.psp, gr.psp)
		}
	}
	return diffs
}

// indexRecords maps records by key; duplicates are an error.
func indexRecords(recs []record) (map[string]record, error) {
	m := make(map[string]record, len(recs))
	for _, r := range recs {
		if _, dup := m[r.key]; dup {
			return nil, fmt.Errorf("duplicate record %q", r.line)
		}
		m[r.key] = r
	}
	return m, nil
}

// floatsMatch compares two printed values. eq reports equality within
// the relative and absolute tolerances, or (tol) within one unit in the last of digits printed
// significant digits, which happens when the exact values straddle a
// rounding boundary of the output. rel is the relative difference.
func floatsMatch(a, b float64, digits int, relTol, absTol float64) (eq, tol bool, rel float64) {
	switch {
	case a == b || (math.IsNaN(a) && math.IsNaN(b)):
		return true, false, 0
	case math.IsNaN(a) || math.IsNaN(b) || math.IsInf(a, 0) || math.IsInf(b, 0):
		return false, false, math.Inf(1)
	}
	d := math.Abs(a - b)
	scale := max(math.Abs(a), math.Abs(b))
	rel = d / scale
	if d <= relTol*scale+absTol {
		return true, true, rel
	}
	if digits == 0 {
		digits = 6
	}
	if digits < 17 {
		unit := math.Pow(10, math.Floor(math.Log10(scale))-float64(digits)+1)
		if d <= unit*(1+1e-9) {
			return true, true, rel
		}
	}
	return false, false, rel
}

// onBoundary reports whether a record that only one side reported has an
// evaluation value within boundaryTol of the threshold, where the two
// implementations' last-bit differences can decide either way.
func (o *outcome) onBoundary(c *tcase, r record, evalIdx int) bool {
	if c.eval == 'x' || evalIdx < 0 {
		return false
	}
	v, thr := r.values[evalIdx], c.thresh/100
	if c.info.fields[evalIdx].code == 'E' {
		v /= 100
	}
	tol := max(boundaryTol, 10*measureTol(c, c.total()))
	return math.Abs(v-thr) <= tol*max(1, math.Abs(thr))
}

// measureTol returns the relative tolerance of the evaluation measure.
//
// The logarithmic measures (p-values of chi^2 and G statistics,
// information gain, Fisher's tests) evaluate logarithms or logGamma
// values of counts up to the total weight N, about N*ln(N) in magnitude,
// and exponentiate differences of them. One ulp of such a term is
// eps*N*ln(N) absolute in the log domain, i.e. relative in the result,
// and a sum of up to a few dozen terms (or the 1-p complement of a Fisher
// sum) accumulates it; libm and Go round such terms independently. The
// bound 64*eps*N*ln(N+2) is that error with headroom, never below relTol.
func measureTol(c *tcase, total int64) float64 {
	if strings.IndexByte(logMeasures, c.eval) < 0 {
		return relTol
	}
	n := float64(max(total, 1))
	return max(relTol, 64*dblEpsilon*n*math.Log(n+2))
}

// firstDiff describes the first difference between the outputs.
func firstDiff(cr, gr *runResult) string {
	for _, f := range []struct {
		name   string
		c, go_ []byte
	}{{"out", cr.out, gr.out}, {"stdout", cr.stdout, gr.stdout}, {"stderr", cr.stderr, gr.stderr}, {"psp", cr.psp, gr.psp}} {
		if bytes.Equal(f.c, f.go_) {
			continue
		}
		cl, gl := strings.Split(string(f.c), "\n"), strings.Split(string(f.go_), "\n")
		for i := range max(len(cl), len(gl)) {
			var a, b string
			if i < len(cl) {
				a = cl[i]
			}
			if i < len(gl) {
				b = gl[i]
			}
			if a != b {
				return fmt.Sprintf("%s line %d: C %q, Go %q", f.name, i+1, a, b)
			}
		}
	}
	return ""
}

// lastLine returns the last non-empty line of a log.
func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	return lines[len(lines)-1]
}
