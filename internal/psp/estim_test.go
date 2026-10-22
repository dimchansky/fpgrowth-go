package psp

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tabread"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// traceValue is one value v·equiv of an estimation before rounding.
type traceValue struct {
	z int
	c tract.Supp
	v float64
}

// TestEstimate_matchesC compares every value of the estimation before it
// is rounded to a count (v·equiv) with the oracle's
// (reference/harness/fpgpsp/estim-trace.patch, testdata/estim): the same
// sequence of (size, support) cells, which pins the loop limits, the
// distribution threshold and the break conditions, and the same values
// within a relative 1e-10. Go's math.Log and math.Exp may differ from the
// platform's libm in the last bit, and the values come from a few hundred
// operations, so their relative difference stays near 1e-13; any change
// of the method (rounding, threshold, normalization, sampling) moves
// values by far more, mostly in the tail of the distributions.
func TestEstimate_matchesC(t *testing.T) {
	b, err := os.ReadFile("testdata/estim/cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for line := range strings.Lines(string(b)) {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if len(f) != 10 {
			t.Fatalf("bad case line %q", line)
		}
		name := f[0]
		t.Run(name, func(t *testing.T) {
			want := readTrace(t, filepath.Join("testdata", "estim", name+".c.out"))
			got := traceEstimate(t, f)
			for i := range min(len(got), len(want)) {
				g, w := got[i], want[i]
				if g.z != w.z || g.c != w.c {
					t.Fatalf("value %d: cell (%d, %d), C (%d, %d)", i, g.z, g.c, w.z, w.c)
				}
				if d := math.Abs(g.v - w.v); d > 1e-10*math.Abs(w.v) && d > 1e-300 {
					t.Fatalf("value %d, cell (%d, %d): Go %x, C %x (relative %.3g)", i, g.z, g.c, g.v, w.v, d/math.Abs(w.v))
				}
			}
			if len(got) != len(want) || len(want) == 0 {
				t.Fatalf("Go has %d values, C %d", len(got), len(want))
			}
		})
		n++
	}
	if n < 10 {
		t.Fatalf("only %d cases", n)
	}
}

func readTrace(t *testing.T, path string) []traceValue {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tr []traceValue
	for line := range strings.Lines(string(b)) {
		var v traceValue
		var hex string
		if _, err := fmt.Sscanf(line, "D %d %d %s", &v.z, &v.c, &hex); err != nil {
			t.Fatalf("%s: %q: %v", path, line, err)
		}
		if v.v, err = strconv.ParseFloat(hex, 64); err != nil {
			t.Fatal(err)
		}
		tr = append(tr, v)
	}
	return tr
}

// traceEstimate runs the estimation of a case line (fpg_estpsp with the
// options of the columns) and returns its trace.
func traceEstimate(t *testing.T, f []string) []traceValue {
	t.Helper()
	atoi := func(s string) int64 {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cfg := fpg.DefaultConfig() // FPG_AUTO, FPG_DEFAULT
	cfg.Target = map[string]report.Target{"s": report.TargetAll, "c": report.TargetClosed,
		"m": report.TargetMaximal, "g": report.TargetGenerators}[f[1]]
	cfg.ZMin = int(atoi(f[2]))
	if z := atoi(f[3]); z > 0 {
		cfg.ZMax = int(z)
	}
	supp, _ := strconv.ParseFloat(f[4], 64)
	cfg.SMin = supp
	alpha, _ := strconv.ParseFloat(f[6], 64)
	scan := tabread.ScanFile(filepath.Join("testdata", "estim", f[9]), tabread.DefaultConfig())
	ib := tract.NewItemBase[string]()
	if err := ib.Count(scan); err != nil {
		t.Fatal(err)
	}
	m, d, err := Prepare(ib, scan, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var tr []traceValue
	estimTrace = func(z int, c tract.Supp, v float64) { tr = append(tr, traceValue{z, c, v}) }
	defer func() { estimTrace = nil }()
	p := report.NewSpectrum(cfg.ZMin, cfg.ZMax, m.MinSupport(), tract.Supp(d.Len()))
	err = Estimate(context.Background(), d, p, cfg.ZMin, EstimateConfig{
		Equiv: uint64(atoi(f[5])), Alpha: alpha, Samples: uint64(atoi(f[7])), Seed: uint32(atoi(f[8])),
	})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}
