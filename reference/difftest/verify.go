package difftest

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// maxListed bounds the number of differences listed per check.
const maxListed = 5

// setParams derives the brute-force item set parameters of a case.
func (c *tcase) setParams(total int64) bfSetParams {
	p := bfSetParams{
		target: c.target,
		smin:   absMinSupp(c.sArg, total),
		smax:   absMaxSupp(100, total),
		zmin:   c.zmin,
		zmax:   math.MaxInt,
		border: absBorder(c.border, c.zmin, total),
	}
	if c.hasSMax {
		p.smax = absMaxSupp(c.sMaxArg, total)
	}
	if c.zmax >= 0 {
		p.zmax = c.zmax
	}
	return p
}

// ruleParams derives the brute-force rule parameters of a case.
func (c *tcase) ruleParams(total int64) bfRuleParams {
	sp := c.setParams(total)
	return bfRuleParams{
		smin: sp.smin, smax: sp.smax, orig: c.orig, conf: c.conf / 100,
		zmin: sp.zmin, zmax: sp.zmax, appear: c.appVals, defApp: c.defAppVal,
	}
}

// verifyGo checks the Go result against the brute force (checkBF).
func (o *outcome) verifyGo(c *tcase, db *bfDB, gr *runResult) {
	bad, subset := checkBF(c, db, o.deviations, gr)
	o.bfChecked, o.bfSubset = true, subset
	if len(bad) > 0 {
		o.fail("brute force (%d differences): %s", len(bad), strings.Join(bad[:min(len(bad), maxListed)], "; "))
	}
}

// checkBF checks the result of a successful run (of either program)
// against the brute force: exactly without an evaluation measure, as a
// subset (with exact supports) with one. In count-only mode the logged
// count and the -Z statistics (sets or rules per size) are checked. It
// returns the differences (nil: the result passes) and whether the check
// was a subset check.
func checkBF(c *tcase, db *bfDB, devs []string, r *runResult) (bad []string, subset bool) {
	subset = c.eval != 'x'
	want := map[string][3]int64{} // key -> supp, body supp, head supp
	var perSize []int64           // number of sets or rules per size
	count := func(size int) {
		for len(perSize) <= size {
			perSize = append(perSize, 0)
		}
		perSize[size]++
	}
	if c.isRules() {
		for _, r := range db.rules(c.ruleParams(db.total)) {
			want[ruleKey(r.head, r.body)] = [3]int64{r.supp, r.bsup, r.hs}
			count(len(r.body) + 1)
		}
	} else {
		p := c.setParams(db.total)
		if slices.Contains(devs, "R2") {
			// -p prunes sets with the measure before the closed, maximal
			// or generator filter runs on the remaining sets, so these
			// properties hold only relative to the pruned tree: check
			// that the sets are frequent with the right supports.
			p.target = 's'
		}
		for _, s := range db.sets(p) {
			want[setKey(s.items)] = [3]int64{s.supp}
			count(len(s.items))
		}
	}
	if c.countOnly {
		n := reportedCount(r.stderr)
		if n < 0 || (!subset && n != int64(len(want))) || n > int64(len(want)) {
			bad = append(bad, fmt.Sprintf("counted %d, brute force %d (subset %v)", n, len(want), subset))
		}
		if c.stats {
			bad = append(bad, checkStats(r.stdout, n, perSize, subset)...)
		}
		return bad, subset
	}
	recs, err := parseOutput(r.out, c.info, c.isRules(), c.sep, c.imp)
	if err != nil {
		return []string{fmt.Sprintf("output: %v", err)}, subset
	}
	fields := "a"
	if c.isRules() {
		fields = "abh"
	}
	seen := map[string]bool{}
	for _, rec := range recs {
		w, ok := want[rec.key]
		if !ok || seen[rec.key] {
			bad = append(bad, "not in brute force (or repeated): "+rec.line)
			continue
		}
		seen[rec.key] = true
		for k := range fields {
			if i := c.info.fieldIndex(fields[k : k+1]); i >= 0 && int64(rec.values[i]) != w[k] {
				bad = append(bad, fmt.Sprintf("%%%c: %s, brute force %d", fields[k], rec.line, w[k]))
			}
		}
	}
	if !subset {
		for _, k := range sortedKeys(want) {
			if !seen[k] {
				bad = append(bad, fmt.Sprintf("missing: %q %v", strings.ReplaceAll(k, "\x00", " "), want[k]))
			}
		}
	}
	return bad, subset
}

// checkStats checks the -Z statistics of a count-only run against the
// brute force's number of sets or rules per size (want): exactly, or,
// with an evaluation measure (subset), at most as many per size. Their
// total must equal the logged count n. Without records, the statistics
// are the only evidence of which sets were counted, and equal totals can
// hide sets of the wrong sizes (deviation M1, difftest seed 1 case 31717).
func checkStats(stdout []byte, n int64, want []int64, subset bool) []string {
	all, per, ok := parseStats(stdout)
	switch {
	case !ok:
		return []string{fmt.Sprintf("bad -Z statistics %q", stdout)}
	case all != n:
		return []string{fmt.Sprintf("-Z all %d, log %d", all, n)}
	}
	var bad []string
	for k := range max(len(per), len(want)) {
		var got, w int64
		if k < len(per) {
			got = per[k]
		}
		if k < len(want) {
			w = want[k]
		}
		if got > w || (!subset && got != w) {
			bad = append(bad, fmt.Sprintf("-Z size %d: %d, brute force %d (subset %v)", k, got, w, subset))
		}
	}
	return bad
}

// selfCheck checks the consistency of the Go outputs: the logged count,
// the -Z statistics and the pattern spectrum must describe the sets or
// rules written.
func (o *outcome) selfCheck(c *tcase, gr *runResult) {
	n := reportedCount(gr.stderr)
	var recs []record
	if !c.countOnly {
		var err error
		if recs, err = parseOutput(gr.out, c.info, c.isRules(), c.sep, c.imp); err != nil {
			return // reported by compareC
		}
		if n != int64(len(recs)) {
			o.fail("self-check: log says %d, output has %d", n, len(recs))
		}
	}
	if !c.stats {
		if len(gr.stdout) > 0 {
			o.fail("self-check: unexpected stdout %q", gr.stdout)
		}
	} else {
		all, per, ok := parseStats(gr.stdout)
		switch {
		case !ok:
			o.fail("self-check: bad -Z statistics %q", gr.stdout)
		case all != n:
			o.fail("self-check: -Z all %d, log %d", all, n)
		case !c.countOnly:
			var want []int64
			for _, r := range recs {
				for len(want) <= r.size() {
					want = append(want, 0)
				}
				want[r.size()]++
			}
			for len(per) > len(want) && per[len(per)-1] == 0 {
				per = per[:len(per)-1]
			}
			for len(want) > len(per) && want[len(want)-1] == 0 {
				want = want[:len(want)-1]
			}
			if !slices.Equal(per, want) {
				o.fail("self-check: -Z per size %v, output %v", per, want)
			}
		}
	}
	if c.spectrum && !c.countOnly {
		psp, err := parseSpectrum(gr.psp)
		ai := c.info.fieldIndex("a")
		want := map[[2]int64]int64{}
		for _, r := range recs {
			want[[2]int64{int64(r.size()), int64(r.values[ai])}]++
		}
		switch {
		case !gr.hasPsp:
			o.fail("self-check: no pattern spectrum written")
		case err != nil:
			o.fail("self-check: %v", err)
		case !mapsEqual(psp, want):
			o.fail("self-check: pattern spectrum %v, output %v", psp, want)
		}
	}
}

func mapsEqual(a, b map[[2]int64]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
