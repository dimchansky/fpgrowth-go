package ruleval

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGammaConstants(t *testing.T) {
	t.Parallel()
	// The values C computes for EPSILON, TINY and MAXFACT+1+4*EPSILON.
	if epsilon != math.Nextafter(1, 2)-1 {
		t.Errorf("epsilon = %v, want DBL_EPSILON", epsilon)
	}
	if float64(2.2204460492503131e-16) != epsilon {
		t.Error("upstream's EPSILON literal does not round to 2^-52")
	}
	if tiny != math.Ldexp(1, -156) {
		t.Errorf("tiny = %v, want 2^-156", tiny)
	}
	var limit float64 = tableLimit
	if limit != 171 {
		t.Errorf("tableLimit = %v, want 171", limit)
	}
}

// specfunRow is one line of testdata/specfun_c.txt.
type specfunRow struct {
	kind string // "lg" (logGamma) or "q" (chi2cdfQ with df = 1)
	x, v float64
}

func loadSpecfunRows(t *testing.T) []specfunRow {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "specfun_c.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []specfunRow
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := sc.Text()
		if text == "" || text[0] == '#' {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 3 {
			t.Fatalf("specfun_c.txt:%d: %d fields", line, len(fields))
		}
		x, err1 := strconv.ParseFloat(fields[1], 64)
		v, err2 := strconv.ParseFloat(fields[2], 64)
		if err1 != nil || err2 != nil {
			t.Fatalf("specfun_c.txt:%d: bad number", line)
		}
		rows = append(rows, specfunRow{fields[0], x, v})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestSpecialFunctionsMatchUpstream compares logGamma and chi2cdfQ with
// upstream's C code. logGamma must agree within 4 ulps of its value (the
// table entries are logs of the same running products, and the Lanczos
// formula has one log per term; measured maximum 1 ulp), except for
// logGamma(170.5), where upstream returns 0 (bug B11). chi2cdfQ must
// agree within 8·ε·(2+x) relatively (see tolChi2PValue).
func TestSpecialFunctionsMatchUpstream(t *testing.T) {
	t.Parallel()
	var maxLG, maxQ float64
	var nLG, nQ int
	for _, r := range loadSpecfunRows(t) {
		switch r.kind {
		case "lg":
			nLG++
			got := logGamma(r.x)
			if r.x == 170.5 {
				if r.v != 0 {
					t.Errorf("upstream logGamma(170.5) = %v, expected 0 (B11)", r.v)
				}
				continue
			}
			e := math.Abs(got-r.v) / (epsilon * max(math.Abs(r.v), 1))
			maxLG = max(maxLG, e)
			if e > 4 {
				t.Errorf("logGamma(%v) = %.17g, upstream %.17g", r.x, got, r.v)
			}
		case "q":
			nQ++
			got := chi2cdfQ(r.x, 1)
			e := math.Abs(got-r.v) / (epsilon * max(r.v, 0x1p-1022) * (2 + r.x))
			if got == r.v {
				e = 0
			}
			maxQ = max(maxQ, e)
			if e > tolChi2PValue {
				t.Errorf("chi2cdfQ(%v, 1) = %.17g, upstream %.17g", r.x, got, r.v)
			}
		default:
			t.Fatalf("unknown row kind %q", r.kind)
		}
	}
	if nLG < 600 || nQ < 1000 {
		t.Fatalf("specfun_c.txt: %d logGamma and %d chi2cdfQ rows", nLG, nQ)
	}
	t.Logf("logGamma: %d values, max %.3g ulp; chi2cdfQ: %d values, max %.3g ε·(2+x)", nLG, maxLG, nQ, maxQ)
}

// TestLogGammaIndependent compares logGamma with math.Lgamma, an
// independent implementation, including the B11 regression at 170.5.
func TestLogGammaIndependent(t *testing.T) {
	t.Parallel()
	check := func(x float64) {
		want, _ := math.Lgamma(x)
		got := logGamma(x)
		if math.Abs(got-want) > 1e-13*max(math.Abs(want), 1) {
			t.Errorf("logGamma(%v) = %.17g, Lgamma %.17g", x, got, want)
		}
	}
	for k := 1; k <= 1000; k++ {
		check(float64(k))
		check(float64(k) - 0.5)
	}
	for _, x := range []float64{0.1, 0.25, 0.9, 1.3, 2.7, 170.25, 170.5, 170.75, 1e6 + 0.3, 1e9, 4e18} {
		check(x)
	}
	if got := logGamma(170.5); got < 700 {
		t.Errorf("logGamma(170.5) = %v (B11)", got)
	}
	for _, x := range []float64{0, -1, -0.5} {
		if got := logGamma(x); !math.IsInf(got, 1) {
			t.Errorf("logGamma(%v) = %v, want +Inf", x, got)
		}
	}
	if got := logGamma(1e-17); math.IsNaN(got) {
		t.Errorf("logGamma(1e-17) = NaN")
	}
}

// TestChi2cdfQIndependent compares chi2cdfQ(x, 1) = Q(1/2, x/2) with
// erfc(sqrt(x/2)), its closed form.
func TestChi2cdfQIndependent(t *testing.T) {
	t.Parallel()
	for i := 0; i <= 4000; i++ {
		x := float64(i) * 0.05
		want := math.Erfc(math.Sqrt(x / 2))
		got := chi2cdfQ(x, 1)
		if math.Abs(got-want) > 1e-12*want+1e-300 {
			t.Errorf("chi2cdfQ(%v, 1) = %.17g, erfc %.17g", x, got, want)
		}
	}
	if got := chi2cdfQ(-1, 1); got != 1 {
		t.Errorf("chi2cdfQ(-1, 1) = %v, want 1", got)
	}
	if got := chi2cdfQ(1e6, 1); got != 0 {
		t.Errorf("chi2cdfQ(1e6, 1) = %v, want 0", got)
	}
	// Other degrees of freedom: Q(1, x/2) = exp(-x/2) for df = 2.
	for _, x := range []float64{0.5, 1, 3, 10, 40} {
		if got, want := chi2cdfQ(x, 2), math.Exp(-x/2); math.Abs(got-want) > 1e-13*want {
			t.Errorf("chi2cdfQ(%v, 2) = %v, want %v", x, got, want)
		}
	}
}
