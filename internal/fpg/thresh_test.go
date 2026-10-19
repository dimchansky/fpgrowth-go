package fpg

import (
	"math"
	"math/big"
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/thresh"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Regression tests of the threshold contract (issue #17, N2) against
// upstream's double-arithmetic formulas, which the port used before. The
// formulas below are copies of fpg_data, fpg_report and the istree
// confidence test, used only here.

const upEps = 0x1p-52

// upMin is fpg_data's ceil((smin/100)*w*(1-DBL_EPSILON)) for smin >= 0.
func upMin(smin float64, w tract.Supp) tract.Supp {
	return suppOf(math.Ceil(smin / 100.0 * float64(w) * (1 - upEps)))
}

// upMax is fpg_report's floor((smax/100)*w*(1+DBL_EPSILON)).
func upMax(smax float64, w tract.Supp) tract.Supp {
	return suppOf(math.Floor(smax / 100.0 * float64(w) * (1 + upEps)))
}

// upSetMin is fpg_data's rule set minimum: ceil(smin*(conf*(1-eps))) with
// conf = -c/100 and the double smin (here an absolute body minimum below
// 2^53, so exact).
func upSetMin(body tract.Supp, conf float64) tract.Supp {
	return min(max(suppOf(math.Ceil(float64(body)*(conf*(1-upEps)))), 1), body)
}

// upConf is istree's confidence test: !(supp < body*(conf*(1-eps))).
func upConf(supp, body tract.Supp, conf float64) bool {
	return !(float64(supp) < float64(body)*(conf*(1-upEps)))
}

// newConf is the contract's confidence test.
func newConf(supp, body tract.Supp, conf float64) bool {
	f := thresh.New(conf)
	return f.Meets(int64(supp), int64(body))
}

// pct parses a decimal percentage like the command line (strtod).
func pct(p, d int) float64 {
	v, err := strconv.ParseFloat(strconv.FormatFloat(float64(p)/math.Pow10(d), 'f', d, 64), 64)
	if err != nil {
		panic(err)
	}
	return v
}

// belowSwitch reports whether x = (p/100)·w, with p/100 a float64
// division, is below 2^52 (orEqual: at most 2^52), computed with math/big.
func belowSwitch(p float64, w tract.Supp, orEqual bool) bool {
	x := new(big.Rat).Mul(new(big.Rat).SetFloat64(p/100), new(big.Rat).SetInt64(int64(w)))
	c := x.Cmp(new(big.Rat).SetInt64(1 << 52))
	return c < 0 || orEqual && c == 0
}

// adversarialPercent draws a CLI percentage from the classes where an
// exact evaluation would differ from upstream's double rounding
// (review of PR #30, C1): decimals with 1-3 places, values one or two
// ulps off a decimal, 100-a, running sums of 0.01, 0.1 and 1, products,
// computed percentages and random values.
func adversarialPercent(r *rand.Rand) float64 {
	switch r.IntN(9) {
	case 0:
		return pct(r.IntN(10001), 1+r.IntN(3)) / math.Pow10(r.IntN(2))
	case 1:
		p := pct(1+r.IntN(10000), 2)
		for range 1 + r.IntN(2) {
			p = math.Nextafter(p, float64(r.IntN(2)*200))
		}
		return p
	case 2:
		return 100 - pct(r.IntN(10001), 2)
	case 3:
		step := []float64{0.01, 0.1, 1, 0.001}[r.IntN(4)]
		p := 0.0
		for range 1 + r.IntN(int(min(100/step, 3000))) {
			p += step
		}
		return min(p, 100)
	case 4:
		return pct(1+r.IntN(100), 0) * pct(1+r.IntN(100), 2) / 100
	case 5:
		return float64(1+r.IntN(1000)) * 0.01 * float64(1+r.IntN(9)) / 9
	case 6:
		w := 1 + r.Int64N(1<<31)
		return float64(1+r.Int64N(w)) * 100 / float64(w)
	case 7:
		return r.Float64() * 100
	}
	return float64(1+r.IntN(1000)) / float64(1+r.IntN(1000)) * 100
}

// adversarialWeight draws a total weight for p: small, a multiple of 100,
// 1000 or 10000 (an integer intended threshold) plus 0..2, or large up to
// x = 2^52.
func adversarialWeight(r *rand.Rand, p float64) tract.Supp {
	switch r.IntN(4) {
	case 0:
		return tract.Supp(1 + r.Int64N(5000))
	case 1:
		m := []int64{100, 1000, 10000, 100000}[r.IntN(4)]
		return tract.Supp(m*(1+r.Int64N(1<<20)) + r.Int64N(3))
	case 2:
		return tract.Supp(1 + r.Int64N(1<<31))
	}
	limit := 0x1p52 / max(p/100, 0x1p-20)
	return tract.Supp(1 + r.Int64N(int64(min(limit, 0x1p62))))
}

// TestThresholdsIdenticalBelowSwitch checks that every threshold decision
// is upstream's (and v0.1.0's) while x = t·N is below the switch of the
// threshold contract (differences.md N2): the minimum and maximum support
// (-s, -S), the confidence test and the rule set minimum (-c, CLI and
// HasCounts paths), over adversarial percentages and weights. Above the
// switch the exact branch is checked by TestThresholdsHugeWeights and
// package thresh.
func TestThresholdsIdenticalBelowSwitch(t *testing.T) {
	t.Parallel()
	for k := 1; k <= 10000; k++ { // every 2-dp percentage, small weights
		p := pct(k, 2)
		for w := tract.Supp(1); w <= 300; w++ {
			if a, b := upMin(p, w), minSupp(p, w); a != b {
				t.Fatalf("-s%v of %d: upstream %d, contract %d", p, w, a, b)
			}
			if a, b := upMax(p, w), maxSupp(p, w); a != b {
				t.Fatalf("-S%v of %d: upstream %d, contract %d", p, w, a, b)
			}
		}
	}
	r := rand.New(rand.NewPCG(17, 3))
	checked, confChecked := 0, 0
	for range 300000 {
		p := adversarialPercent(r)
		w := adversarialWeight(r, p)
		if belowSwitch(p, w, false) {
			checked++
			if a, b := upMin(p, w), minSupp(p, w); a != b {
				t.Fatalf("-s%v of %d: upstream %d, contract %d", p, w, a, b)
			}
		}
		if belowSwitch(p, w, true) {
			if a, b := upMax(p, w), maxSupp(p, w); a != b {
				t.Fatalf("-S%v of %d: upstream %d, contract %d", p, w, a, b)
			}
		}
		// confidence c = p/100 <= 1 with the body support w
		if p > 100 {
			continue
		}
		c, body := p/100, w
		if !belowSwitch(p, body, false) {
			continue
		}
		confChecked++
		s := tract.Supp(math.Ceil(float64(body) * (c * (1 - upEps))))
		for _, supp := range []tract.Supp{s - 2, s - 1, s, s + 1, s + 2} {
			if supp >= 0 && supp <= body && upConf(supp, body, c) != newConf(supp, body, c) {
				t.Fatalf("-c%v supp %d body %d: upstream %v", p, supp, body, upConf(supp, body, c))
			}
		}
		// rule set minimum, CLI notation (absolute -s) and HasCounts
		cfg := DefaultConfig()
		cfg.Target, cfg.ConfFrac = report.TargetRules, c
		if body < 1<<53 {
			cfg.SMin = -float64(body)
			if th := computeThresholds(&cfg, body); th.supp != upSetMin(body, c) || th.body != body {
				t.Fatalf("-s-%d -c%v: %+v, upstream set minimum %d", body, p, th, upSetMin(body, c))
			}
		}
		cfg.HasCounts, cfg.MinCount, cfg.MaxCount = true, body, math.MaxInt64
		if th := computeThresholds(&cfg, 0); th.supp != upCountSetMin(body, c) || th.body != body {
			t.Fatalf("counts body %d conf %v: %+v, v0.1.0 set minimum %d", body, c, th, upCountSetMin(body, c))
		}
	}
	if checked < 200000 || confChecked < 100000 {
		t.Errorf("only %d / %d cases below the switch", checked, confChecked)
	}
	// confidence over all supports for the difftest's percentages
	confs := []float64{0, 10, 12.5, 25, 33.3, 50, 60, 66.6, 70, 80, 90, 99.9, 100}
	for _, cp := range confs {
		c := cp / 100
		for body := tract.Supp(1); body <= 300; body++ {
			for supp := tract.Supp(0); supp <= body; supp++ {
				if upConf(supp, body, c) != newConf(supp, body, c) {
					t.Fatalf("-c%v supp %d body %d: upstream %v", cp, supp, body, upConf(supp, body, c))
				}
			}
		}
	}
}

// upCountSetMin is v0.1.0's countThresholds set minimum for the exact
// body minimum (a float64 rounded down, as there).
func upCountSetMin(body tract.Supp, conf float64) tract.Supp {
	x := float64(body)
	if x >= 0x1p63 || tract.Supp(x) > body {
		x = math.Nextafter(x, 0)
	}
	x *= conf * (1 - upEps)
	return min(max(suppOf(math.Ceil(x)), 1), body)
}

// TestThresholdsPinnedUnchanged pins two cases where an exact evaluation
// would differ from upstream's double rounding below 2^52 (found by the
// design review of issue #17): the contract keeps upstream's results.
func TestThresholdsPinnedUnchanged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		p        float64
		w        tract.Supp
		min, max tract.Supp
	}{
		// 3.091 % of 833815673989 is 25773242482.99999 (decimal)
		{3.091, 833815673989, 25773242483, 25773242483},
		// 16.01 % of 1290957281599 is 206682260783.9999 (decimal)
		{16.01, 1290957281599, 206682260784, 206682260784},
	}
	for _, tt := range tests {
		if upMin(tt.p, tt.w) != tt.min || upMax(tt.p, tt.w) != tt.max {
			t.Errorf("-s%v of %d: upstream %d/%d, expected %d/%d", tt.p, tt.w,
				upMin(tt.p, tt.w), upMax(tt.p, tt.w), tt.min, tt.max)
		}
		if got := minSupp(tt.p, tt.w); got != tt.min {
			t.Errorf("-s%v of %d: minimum %d, expected %d", tt.p, tt.w, got, tt.min)
		}
		if got := maxSupp(tt.p, tt.w); got != tt.max {
			t.Errorf("-S%v of %d: maximum %d, expected %d", tt.p, tt.w, got, tt.max)
		}
	}
}

// TestThresholdsHugeWeights checks the contract at the weights where
// upstream's tolerance exceeds one unit (N2): CLI notation and HasCounts.
func TestThresholdsHugeWeights(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                 string
		smin, smax, conf     float64
		w                    tract.Supp
		supp, body, smaxWant tract.Supp
	}{
		{"-s50 of 2^62", 50, 100, 1, 1 << 62, 1 << 61, 1 << 61, 1 << 62},
		{"-s100 of 2^60+1", 100, 100, 1, 1<<60 + 1, 1<<60 + 1, 1<<60 + 1, 1<<60 + 1},
		{"-s100 -S100 of MaxInt64", 100, 100, 1, math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64},
		{"-s50 -S50 of 2^52+2", 50, 50, 1, 1<<52 + 2, 1<<51 + 1, 1<<51 + 1, 1<<51 + 1},
		{"-s50 -S50 of 2^53+2", 50, 50, 1, 1<<53 + 2, 1<<52 + 1, 1<<52 + 1, 1<<52 + 1},
		{"-s50 -S50 of 2^61+2", 50, 50, 1, 1<<61 + 2, 1<<60 + 1, 1<<60 + 1, 1<<60 + 1},
		{"-s10 -c80 of 10·2^59", 10, 100, 0.8, 10 << 59, 1<<59 + 32, 1<<59 + 32, 10 << 59},
		{"-s0 -S200 of MaxInt64 saturates", 0, 200, 1, math.MaxInt64, 1, 1, math.MaxInt64},
		{"-S inf", 0, math.Inf(1), 1, 5, 1, 1, math.MaxInt64},
		{"-S-0", 0, math.Copysign(0, -1), 1, 5, 1, 1, 0},
	}
	for _, tt := range tests {
		c := DefaultConfig()
		c.Target, c.ConfFrac = report.TargetRules, tt.conf
		c.SMin, c.SMax = tt.smin, tt.smax
		th := computeThresholds(&c, tt.w)
		wantSupp := max(tract.Supp(thresh.New(tt.conf).RuleSetMin(int64(tt.body))), 1)
		if th.body != tt.body || th.supp != wantSupp || th.smax != tt.smaxWant {
			t.Errorf("%s: %+v, expected body %d supp %d smax %d", tt.name, th, tt.body, wantSupp, tt.smaxWant)
		}
		if tt.conf == 1 && th.supp != th.body {
			t.Errorf("%s: confidence 1 set minimum %d below the body minimum %d", tt.name, th.supp, th.body)
		}
	}
	// 0.8 of 5·2^60 bodies: float64(0.8)·5·2^60 is exactly 4·2^60+256, so
	// that is the set minimum (upstream: 4·2^60-1024 after its tolerance)
	c := DefaultConfig()
	c.Target, c.ConfFrac = report.TargetRules, 0.8
	c.HasCounts, c.MinCount, c.MaxCount = true, 5<<60, math.MaxInt64
	if th := computeThresholds(&c, 0); th.supp != 4<<60+256 || upSetMin(5<<60, 0.8) != 4<<60-1024 {
		t.Errorf("0.8 of 5·2^60: set minimum %d (upstream %d)", th.supp, upSetMin(5<<60, 0.8))
	}
}
