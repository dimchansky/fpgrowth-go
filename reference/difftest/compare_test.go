package difftest

import (
	"slices"
	"strings"
	"testing"
)

// f1Case returns the stage 1 review's F1 reproducer (docs/differences.md
// F-1): items a and b are exactly independent, so the -eb value of {a, b}
// is exactly 0 = the threshold of -d0, and last-bit differences of log
// decide whether it is reported. countOnly selects count-only mode.
func f1Case(countOnly bool) *tcase {
	return &tcase{
		txs:    txsOf("a b|a|b|b|x|x"),
		target: 's', pack: -1, sortMode: 2,
		sArg: -1, zmin: 1, zmax: -1, conf: 80,
		eval: 'b', agg: 'x', thresh: 0, hasThresh: true,
		info: infoFormat{open: " (", close: ")", fields: []infoField{{code: 'a'}, {code: 'e', digits: 17}}},
		sep:  " ", imp: " <- ", countOnly: countOnly,
	}
}

// countLog returns a log ending in "[n set(s)]".
func countLog(n string) []byte {
	return []byte("fpgrowth - find frequent item sets\nwriting <null> ... [" + n + " set(s)] done [T].\n")
}

func TestOutcome_countByRerun(t *testing.T) {
	t.Parallel()
	const cOut = "b (3,0)\na (2,0)\nx (2,0)\n" // C (Apple libm): {a, b} at -2^-52
	tests := []struct {
		name       string
		cCount     string
		goCount    string
		goOut      string
		wantDiff   bool
		wantReruns int
	}{
		{name: "pair at the threshold explains the counts", cCount: "3", goCount: "4",
			goOut: "b (3,0)\na b (1,0)\na (2,0)\nx (2,0)\n", wantReruns: 1},
		{name: "pair away from the threshold is a failure", cCount: "3", goCount: "4",
			goOut: "b (3,0)\na b (1,0.5)\na (2,0)\nx (2,0)\n", wantDiff: true},
		{name: "count-only count must match the rerun", cCount: "3", goCount: "5",
			goOut: "b (3,0)\na b (1,0)\na (2,0)\nx (2,0)\n", wantDiff: true},
		{name: "same records cannot explain different counts", cCount: "3", goCount: "4",
			goOut: cOut, wantDiff: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := f1Case(true)
			db, err := newBF(c.txs, nil)
			if err != nil {
				t.Fatal(err)
			}
			var rerunArgs []string
			o := &outcome{rerun: func(rc *tcase) (cr, gr *runResult) {
				rerunArgs = rc.args()
				return &runResult{out: []byte(cOut), hasOut: true}, &runResult{out: []byte(tt.goOut), hasOut: true}
			}}
			cr := &runResult{stderr: countLog(tt.cCount)}
			gr := &runResult{stderr: countLog(tt.goCount)}
			diffs := o.compareC(c, db, cr, gr)
			if got := len(diffs) > 0; got != tt.wantDiff {
				t.Errorf("differences %v, want some: %v", diffMessages(diffs), tt.wantDiff)
			}
			if o.reruns != tt.wantReruns {
				t.Errorf("reruns = %d, want %d", o.reruns, tt.wantReruns)
			}
			if !slices.Contains(rerunArgs, outFile) || !slices.Contains(rerunArgs, "-v (%a,%17e)") {
				t.Errorf("rerun arguments %q lack the output file or the format", rerunArgs)
			}
		})
	}
}

func TestOutcome_countByRerunNeedsMeasure(t *testing.T) {
	t.Parallel()
	c := f1Case(true)
	c.eval = 'x'
	db, err := newBF(c.txs, nil)
	if err != nil {
		t.Fatal(err)
	}
	o := &outcome{rerun: func(*tcase) (cr, gr *runResult) {
		t.Fatal("rerun without an evaluation measure")
		return nil, nil
	}}
	diffs := o.compareC(c, db, &runResult{stderr: countLog("3")}, &runResult{stderr: countLog("4")})
	if len(diffs) != 1 || diffs[0].kind != diffCount {
		t.Errorf("differences %+v, want one diffCount", diffs)
	}
}

// TestF1Reproducer runs both programs on the F1 reproducer. Whether the C
// reference reports {a, b} depends on its libm (Apple's omits it, see
// internal/fpg TestLogRatioIndependenceBoundary); in either case the
// comparison must accept the result, with output and in count-only mode
// (where the rerun with output classifies the differing counts).
func TestF1Reproducer(t *testing.T) {
	b := getBinaries(t)
	for _, countOnly := range []bool{false, true} {
		c := f1Case(countOnly)
		dir := t.TempDir()
		if err := c.writeFiles(dir); err != nil {
			t.Fatal(err)
		}
		cr := run(b.c, dir, c.args())
		gr := run(b.goBin, dir, c.args())
		o := evaluate(c, cr, gr, b.rerunIn(dir))
		if o.verdict == verdictFail {
			t.Errorf("count-only %v: %s", countOnly, strings.Join(o.failures, "\n"))
		}
		t.Logf("count-only %v: %s (boundary records %d, reruns %d)", countOnly, o.verdict, o.boundary, o.reruns)
	}
}
