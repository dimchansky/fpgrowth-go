package difftest

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// The harness gaps of issue #29: in count-only mode the brute force
// checked only the total count (M1, difftest seed 1 case 31717), equal
// counts with different -Z statistics were not record differences (R2,
// seed 11 case 10442), and M2's zero-frequency items change upstream's
// order-dependent evaluation (seed 1 case 67615), so M2 cases are now
// compared with upstream run with -s-1.

// gapCase returns a case with the defaults of newCase's options.
func gapCase(data string, f func(*tcase)) *tcase {
	c := &tcase{
		txs: txsOf(data), target: 's', pack: -1, sortMode: 2,
		sArg: -1, zmin: 1, zmax: -1, conf: 80, eval: 'x', agg: 'x', thresh: 10,
		info: infoFormat{open: " (", close: ")", fields: []infoField{{code: 'a'}}},
		sep:  " ", imp: " <- ",
	}
	f(c)
	return c
}

// m1Case is the minimized case 31717: -tm -At -Z in count-only mode.
// Upstream counts the two maximal sets {x,a,b} and {x,c} as two sets of
// size 2 (M1); it crashes when it writes them.
func m1Case() *tcase {
	return gapCase("x a b|x c", func(c *tcase) {
		c.target, c.algo, c.countOnly, c.stats = 'm', 't', true, true
	})
}

// r2Case is the minimized case 10442: -tm -s60 -eu -am -p1 -Z in
// count-only mode. Upstream does not prune with -p > 0 and counts the
// maximal set {a,b,x}; the port prunes it and counts {a,b} (R2).
func r2Case() *tcase {
	return gapCase("a b x|a b x|x", func(c *tcase) {
		c.target, c.sArg, c.eval, c.agg = 'm', 60, 'u', 'm'
		c.hasPrune, c.prune = true, 1
		c.countOnly, c.stats = true, true
	})
}

// m2Case is the minimized case 67615: -ts -s0 -m2 -q-1 -el -d0 with the
// selection "a b c z", where z is absent from the data. Upstream keeps z
// (M2), which changes the tie order of the items of equal frequency and
// with it the lift of {a,b,c} under the default aggregation -ax: 1.5 in
// upstream, 0.75 in the port, which is upstream's value with -s-1.
func m2Case() *tcase {
	return gapCase("a|b c|a b c", func(c *tcase) {
		c.sArg, c.zmin, c.hasZMin = 0, 2, true
		c.sortMode, c.hasSort = -1, true
		c.eval, c.thresh, c.hasThresh = 'l', 0, true
		c.selection = []string{"a", "b", "c", "z"}
		c.info.fields = append(c.info.fields, infoField{code: 'e', digits: 6}) // " (%a,%6e)"
	})
}

func newTestBF(t *testing.T, c *tcase) *bfDB {
	t.Helper()
	var sel map[string]bool
	if !c.isRules() {
		sel = c.selectionSet()
	}
	db, err := newBF(c.txs, sel)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// stats formats -Z statistics with the given counts per size.
func stats(per ...int) []byte {
	var b strings.Builder
	all := 0
	for _, n := range per {
		all += n
	}
	b.WriteString("all: " + itoa(all) + "\n")
	for k, n := range per {
		b.WriteString(strings.Repeat(" ", 3-len(itoa(k))) + itoa(k) + ": " + itoa(n) + "\n")
	}
	return []byte(b.String())
}

func itoa(n int) string { return fmtNum(float64(n)) }

func TestCheckBF_countOnlyStats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		c      *tcase
		count  string
		stdout []byte
		want   string // substring of the first difference, "" for none
	}{
		{"exact per-size counts pass", m1Case(), "2", stats(0, 0, 1, 1), ""},
		{"upstream's M1 count fails", m1Case(), "2", stats(0, 0, 2), "-Z size 2: 2, brute force 1"},
		{"a missing size fails", m1Case(), "2", stats(0, 0, 1, 0, 1), "-Z size 3: 0, brute force 1"},
		{"total must match the log", m1Case(), "3", stats(0, 0, 1, 1), "-Z all 2, log 3"},
		{"malformed statistics fail", m1Case(), "2", []byte("al: 2\n"), "bad -Z statistics"},
		{"subset: fewer sets per size pass", r2Case(), "1", stats(0, 0, 1), ""},
		{"subset: upstream's unpruned set passes", r2Case(), "1", stats(0, 0, 0, 1), ""},
		{"subset: more sets of a size than frequent fail", r2Case(), "1", stats(0, 0, 0, 0, 1), "-Z size 4: 1, brute force 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := newTestBF(t, tt.c)
			r := &runResult{stdout: tt.stdout, stderr: countLog(tt.count)}
			bad, _ := checkBF(tt.c, db, caseDeviations(tt.c, db), r)
			switch {
			case tt.want == "" && len(bad) > 0:
				t.Errorf("checkBF = %q, want no differences", bad)
			case tt.want != "" && !slices.ContainsFunc(bad, func(s string) bool { return strings.Contains(s, tt.want) }):
				t.Errorf("checkBF = %q, want %q", bad, tt.want)
			}
		})
	}
}

func TestCompareC_countOnlyStatsKind(t *testing.T) {
	t.Parallel()
	for _, eval := range []byte{'u', 'x'} {
		c := r2Case()
		c.eval = eval
		db := newTestBF(t, c)
		o := &outcome{}
		cr := &runResult{stdout: stats(0, 0, 0, 1), stderr: countLog("1")}
		gr := &runResult{stdout: stats(0, 0, 1), stderr: countLog("1")}
		diffs := o.compareC(c, db, cr, gr)
		want := diffRecords // other sets passed the measure
		if eval == 'x' {
			want = diffCount
		}
		if len(diffs) != 1 || diffs[0].kind != want {
			t.Errorf("-e%c: differences %+v, want one of kind %d", eval, diffs, want)
		}
	}
}

func TestWithMinSupportOne(t *testing.T) {
	t.Parallel()
	c := m2Case()
	db := newTestBF(t, c)
	vc := c.withMinSupportOne(db)
	if vc == nil || !slices.Contains(vc.args(), "-s-1") || slices.Contains(caseDeviations(vc, db), "M2") {
		t.Fatalf("withMinSupportOne = %+v, want a -s-1 copy without M2", vc)
	}
	if c.sArg != 0 {
		t.Errorf("original case changed: -s%v", c.sArg)
	}
	// rules with -c0: the rule item threshold stays 0, M2 remains
	r := gapCase("a|b c", func(c *tcase) { c.target, c.sArg, c.conf, c.hasConf = 'r', 0, 0, true })
	if vr := r.withMinSupportOne(newTestBF(t, r)); vr != nil {
		t.Errorf("withMinSupportOne(-tr -c0) = %v, want nil", vr.args())
	}
}

// TestHarnessGaps runs both programs on the minimized cases of issue #29
// and checks that each passes for the documented reason, and that a Go
// output broken in the way the old checks could not see now fails.
func TestHarnessGaps(t *testing.T) {
	b := getBinaries(t)
	tests := []struct {
		name   string
		c      *tcase
		reason func(o *outcome, c *tcase, db *bfDB, cr *runResult) string // "" if the reason holds
		mutate func(cr, gr *runResult)                                    // makes Go's result wrong
	}{
		{
			name: "M1 count-only -Z", c: m1Case(),
			reason: func(o *outcome, c *tcase, db *bfDB, cr *runResult) string {
				bad, _ := checkBF(c, db, o.deviations, cr)
				if o.verdict != verdictDeviation || !slices.Contains(o.deviations, "M1") ||
					!slices.ContainsFunc(bad, func(s string) bool { return strings.HasPrefix(s, "-Z size") }) {
					return "want deviation-verified M1, C failing the -Z check"
				}
				return ""
			},
			// Go counts what upstream counts: equal totals, wrong sizes
			mutate: func(cr, gr *runResult) { gr.stdout = bytes.Clone(cr.stdout) },
		},
		{
			name: "R2 count-only -Z", c: r2Case(),
			reason: func(o *outcome, _ *tcase, _ *bfDB, _ *runResult) string {
				if o.verdict != verdictDeviation || !slices.Contains(o.deviations, "R2") || !o.cFull || !o.cExempt {
					return "want deviation-verified R2, C compared in full and differing in excused records"
				}
				return ""
			},
			// Go counts a set of a size that has no frequent set
			mutate: func(_, gr *runResult) { gr.stdout = stats(0, 0, 0, 0, 1) },
		},
		{
			name: "M2 order-dependent evaluation", c: m2Case(),
			reason: func(o *outcome, _ *tcase, _ *bfDB, _ *runResult) string {
				if o.verdict != verdictByteEqual || !o.m2Rerun {
					return "want byte-equal to C rerun with -s-1"
				}
				return ""
			},
			// Go gives upstream's -s0 result, which passes the subset
			// brute force and C's -s0 comparison
			mutate: func(cr, gr *runResult) { *gr = *cr },
		},
		{
			// M2 without items of frequency 0 (upstream crashes with
			// -s0 here): still compared with C rerun with -s-1
			name: "M2 without zero-frequency items", c: gapCase("a|b", func(c *tcase) { c.sArg, c.zmin, c.hasZMin = 0, 2, true }),
			reason: func(o *outcome, _ *tcase, _ *bfDB, _ *runResult) string {
				if o.verdict == verdictFail || !o.m2Rerun || slices.Contains(o.deviations, "M2") {
					return "want a pass against C rerun with -s-1, without M2"
				}
				return ""
			},
			// Go reports the support-0 set {a,b}
			mutate: func(_, gr *runResult) { gr.out = append(gr.out, "a b (0)\n"...) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := tt.c.writeFiles(dir); err != nil {
				t.Fatal(err)
			}
			args := tt.c.args()
			cr, gr := run(b.c, dir, args), run(b.goBin, dir, args)
			rerun := b.rerunIn(dir)
			o := evaluate(tt.c, cr, gr, func(rc *tcase) (*runResult, *runResult) {
				vcr, vgr := rerun(rc)
				if slices.Contains(rc.args(), "-s-1") && (!rc.cOnly || vgr != nil) {
					t.Errorf("the -s-1 rerun ran Go too (cOnly %v)", rc.cOnly)
				}
				return vcr, vgr
			})
			if msg := tt.reason(o, tt.c, newTestBF(t, tt.c), cr); msg != "" {
				t.Errorf("%s: verdict %s, deviations %v, m2Rerun %v, cFull %v, cExempt %v; %s\nC:  %q %q %q\nGo: %q %q %q\nfailures: %q",
					args, o.verdict, o.deviations, o.m2Rerun, o.cFull, o.cExempt, msg,
					cr.out, cr.stdout, cr.stderr, gr.out, gr.stdout, gr.stderr, o.failures)
			}
			// mutation: a broken Go result must fail
			cr, gr = run(b.c, dir, args), run(b.goBin, dir, args)
			tt.mutate(cr, gr)
			if o := evaluate(tt.c, cr, gr, b.rerunIn(dir)); o.verdict != verdictFail {
				t.Errorf("broken Go result accepted: verdict %s\nGo: %q %q", o.verdict, gr.out, gr.stdout)
			} else {
				t.Logf("broken Go result rejected: %s", strings.Join(o.failures, "; "))
			}
		})
	}
}
