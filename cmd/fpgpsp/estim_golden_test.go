package main

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// spectrumCells parses a pattern spectrum ("size support value" lines).
func spectrumCells(s string) (map[[2]int64]float64, error) {
	m := map[[2]int64]float64{}
	for line := range strings.Lines(s) {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) != 3 {
			return nil, fmt.Errorf("bad line %q", line)
		}
		z, err1 := strconv.ParseInt(f[0], 10, 64)
		c, err2 := strconv.ParseInt(f[1], 10, 64)
		v, err3 := strconv.ParseFloat(f[2], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("bad line %q", line)
		}
		m[[2]int64{z, c}] = v
	}
	return m, nil
}

var signaturesRE = regexp.MustCompile(`\[[0-9]+ signature\(s\)\]`)

// estimDiff compares an estimation with the C oracle's: "" if byte-equal;
// otherwise the one cell that differs by at most one count (1/cnt in the
// output, plus the rounding of %g) and an error if the difference goes
// beyond that: more than one cell, a larger difference, other log lines
// than the signature counts, or another exit status or border line.
func estimDiff(want, got result, cnt int64) (cell string, err error) {
	if want == got {
		return "", nil
	}
	if want.exit != got.exit {
		return "", fmt.Errorf("exit %d, C %d", got.exit, want.exit)
	}
	if signaturesRE.ReplaceAllString(want.stderr, "") != signaturesRE.ReplaceAllString(got.stderr, "") {
		return "", fmt.Errorf("logs differ beyond the signature counts:\nC:  %q\nGo: %q", want.stderr, got.stderr)
	}
	cm, err1 := spectrumCells(want.file)
	gm, err2 := spectrumCells(got.file)
	if err1 != nil || err2 != nil {
		return "", fmt.Errorf("parse: %v %v", err1, err2)
	}
	unit := 1 / float64(cnt)
	var diffs []string
	keys := map[[2]int64]bool{}
	for k := range cm {
		keys[k] = true
	}
	for k := range gm {
		keys[k] = true
	}
	for k := range keys {
		cv, gv := cm[k], gm[k]
		if cv == gv {
			continue
		}
		diffs = append(diffs, fmt.Sprintf("cell %v: C %g, Go %g", k, cv, gv))
		if math.Abs(cv-gv) > unit*(1+1e-9)+5e-6*(math.Abs(cv)+math.Abs(gv)) {
			return "", fmt.Errorf("%s: more than one count (%g)", diffs[len(diffs)-1], unit)
		}
	}
	if len(diffs) != 1 {
		return "", fmt.Errorf("%d cells differ: %v", len(diffs), diffs)
	}
	if want.stdout != got.stdout { // the -D line may change only with a cell at the border
		return diffs[0] + fmt.Sprintf(" (border C %q, Go %q)", want.stdout, got.stdout), nil
	}
	return diffs[0], nil
}

// TestGoldenEstimate compares the estimation (-ge) with golden outputs of
// the C oracle (testdata/estim-cases.txt, golden-estim/; both the simple
// estimation -e0 and the sampled one, several data sets, size ranges,
// dispersion factors and sample counts). The estimate goes through log
// and exp, whose last bit may differ between Go's math package and the
// libm the goldens were made with, which can move a count that lies at a
// rounding boundary by one. That is rare (about once in 10^5 estimations
// on random data), so at most one case may differ, in at most one cell, by
// one count; every other case must be byte-identical. A systematic error
// (another rounding, another cut-off) changes many cells and fails.
func TestGoldenEstimate(t *testing.T) {
	cases := readCasesFile(t, "estim-cases.txt")
	gold, _ := filepath.Abs(filepath.Join("testdata", "golden-estim"))
	t.Chdir(filepath.Join("testdata", "data"))
	tolerated := 0
	for _, c := range cases {
		got := runCase(t, c)
		want := readResult(t, gold, c.name)
		cnt := int64(1000)
		for _, a := range c.args {
			if v, ok := strings.CutPrefix(a, "-c"); ok {
				cnt, _ = strconv.ParseInt(v, 10, 64)
			}
		}
		cell, err := estimDiff(want, got, cnt)
		switch {
		case err != nil:
			t.Errorf("%s: fpgpsp %q: %v", c.name, c.args, err)
		case cell != "":
			tolerated++
			t.Logf("%s: tolerated: %s", c.name, cell)
		}
	}
	if tolerated > 1 {
		t.Errorf("%d of %d cases differ from C by one count; at most 1 is expected", tolerated, len(cases))
	}
	t.Logf("%d estimation cases: %d byte-identical to C, %d within one count", len(cases), len(cases)-tolerated, tolerated)
}
