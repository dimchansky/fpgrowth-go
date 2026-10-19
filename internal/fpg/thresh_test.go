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

// upSetMin is fpg_data's rule set minimum: ceil(body*(conf*(1-eps))) with
// conf = -c/100 (and the port's former countThresholds for the library,
// whose conf was an exact fraction f).
func upSetMin(body tract.Supp, conf float64) tract.Supp {
	return min(max(suppOf(math.Ceil(float64(body)*(conf*(1-upEps)))), 1), body)
}

// upConf is istree's confidence test: !(supp < body*(conf*(1-eps))).
func upConf(supp, body tract.Supp, conf float64) bool {
	return !(float64(supp) < float64(body)*(conf*(1-upEps)))
}

// newConf is the contract's confidence test.
func newConf(supp, body tract.Supp, conf float64) bool {
	return int64(supp) >= thresh.New(conf).Min(int64(body))
}

// pct parses a decimal percentage like the command line (strtod).
func pct(p, d int) float64 {
	v, err := strconv.ParseFloat(strconv.FormatFloat(float64(p)/math.Pow10(d), 'f', d, 64), 64)
	if err != nil {
		panic(err)
	}
	return v
}

// TestThresholdsUnchanged checks that the exact contract gives upstream's
// results at ordinary weights (W < 2^31): every 2-dp percentage for
// W <= 300, seeded samples of 3-dp, random and computed (smin·100/total,
// like difftest's large cases) percentages, the rule set minimum and the
// confidence test (CLI percentages and library fractions), and the
// HasCounts path (the library's former countThresholds). For short
// decimals and fractions k/d this is guaranteed (TestThresholdsDecimalBound);
// for values with many digits it is a seeded regression sample (see
// TestLibraryThresholdsUnchanged in the root package for the rare changes
// of fractions one ulp off a decimal).
func TestThresholdsUnchanged(t *testing.T) {
	t.Parallel()
	for k := 1; k <= 10000; k++ { // every 2-dp percentage
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
	for range 200000 {
		w := tract.Supp(1 + r.Int64N(1<<31-1))
		var p float64
		switch r.IntN(3) {
		case 0: // computed percentage, like difftest's large cases
			p = float64(1+r.Int64N(int64(w))) * 100 / float64(w)
		case 1:
			p = r.Float64() * 100
		default:
			p = pct(r.IntN(100001), 3)
		}
		if a, b := upMin(p, w), minSupp(p, w); a != b {
			t.Fatalf("-s%v of %d: upstream %d, contract %d", p, w, a, b)
		}
		if a, b := upMax(p, w), maxSupp(p, w); a != b {
			t.Fatalf("-S%v of %d: upstream %d, contract %d", p, w, a, b)
		}
	}
	// confidence: CLI percentages (conf = c/100) and library fractions
	confs := []float64{0, 0.1, 0.125, 0.25, 33.3 / 100, 0.5, 0.6, 66.6 / 100, 0.7, 0.8, 0.9, 99.9 / 100, 1}
	for k := 0; k <= 1000; k += 7 {
		confs = append(confs, float64(k)/1000, pct(k, 1)/100)
	}
	for _, c := range confs {
		for body := tract.Supp(1); body <= 300; body++ {
			if a, b := upSetMin(body, c), max(tract.Supp(thresh.New(c).Min(int64(body))), 1); a != b {
				t.Fatalf("set minimum conf %v body %d: upstream %d, contract %d", c, body, a, b)
			}
			for supp := tract.Supp(0); supp <= body; supp++ {
				if upConf(supp, body, c) != newConf(supp, body, c) {
					t.Fatalf("conf %v supp %d body %d: upstream %v", c, supp, body, upConf(supp, body, c))
				}
			}
		}
	}
	for range 200000 {
		c := []float64{r.Float64(), float64(r.IntN(1001)) / 1000, pct(r.IntN(100001), 3) / 100,
			float64(1+r.IntN(1000)) / float64(1001+r.IntN(1000))}[r.IntN(4)]
		body := tract.Supp(1 + r.Int64N(1<<31-1))
		s := tract.Supp(math.Round(c * float64(body)))
		for _, supp := range []tract.Supp{s - 1, s, s + 1} {
			if supp >= 0 && supp <= body && upConf(supp, body, c) != newConf(supp, body, c) {
				t.Fatalf("conf %v supp %d body %d: upstream %v", c, supp, body, upConf(supp, body, c))
			}
		}
		// HasCounts path (the library's countThresholds)
		cfg := DefaultConfig()
		cfg.Target, cfg.ConfFrac = report.TargetRules, c
		cfg.HasCounts, cfg.MinCount, cfg.MaxCount = true, body, math.MaxInt64
		if th := computeThresholds(&cfg, 0); th.supp != upSetMin(body, c) || th.body != body {
			t.Fatalf("counts body %d conf %v: %+v, upstream set minimum %d", body, c, th, upSetMin(body, c))
		}
	}
}

// TestThresholdsDecimalBound checks the bound of the changes against
// upstream's double formula (differences.md N2): for a CLI percentage with
// d decimals p = P/10^d, upstream's rounding can only cross an integer if
// x = p·W/100 is at least about 2^53/(4.5·10^(d+2)) and P·W lies close to
// a multiple of 10^(d+2). The generator targets exactly those totals (P·W
// within ±3 of a multiple). Below the bound results equal upstream; above
// it every change is the exact decimal answer ⌈P·W/10^(d+2)⌉ (minimum) or
// ⌊P·W/10^(d+2)⌋ (maximum), which upstream misses.
func TestThresholdsDecimalBound(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(17, 4))
	changes := 0
	for d := 0; d <= 3; d++ {
		m := int64(math.Pow10(d + 2)) // p·W/100 = P·W/m
		bound := 0x1p53 / (4.5 * float64(m))
		for range 40000 {
			P := 1 + r.Int64N(100*int64(math.Pow10(d)))
			if P%10 == 0 && d > 0 {
				continue // fewer decimals
			}
			p := pct(int(P), d)
			// W with P·W ≡ off (mod m) and x near 2^e
			e := 20 + r.IntN(30) // x up to 2^50
			w0 := int64(math.Ldexp(1, e)) * m / P
			inv := new(big.Int).ModInverse(big.NewInt(P), big.NewInt(m))
			if inv == nil {
				continue // gcd(P, m) > 1: P·W mod m cannot be ±1 for all offsets
			}
			off := int64(r.IntN(7) - 3)
			wr := (off%m + m) % m * inv.Int64() % m
			w := tract.Supp(w0 - w0%m + wr)
			if w <= 0 {
				continue
			}
			x := float64(P) * float64(w) / float64(m)
			pw := new(big.Int).Mul(big.NewInt(P), big.NewInt(int64(w)))
			q, rem := new(big.Int).QuoRem(pw, big.NewInt(m), new(big.Int))
			decMin, decMax := tract.Supp(q.Int64()), tract.Supp(q.Int64())
			if rem.Sign() > 0 {
				decMin++
			}
			gotMin, gotMax := minSupp(p, w), maxSupp(p, w)
			upLo, upHi := upMin(p, w), upMax(p, w)
			if gotMin != upLo || gotMax != upHi {
				changes++
				if x < bound {
					t.Fatalf("-s%v of %d (x = 2^%.2f below the bound 2^%.2f): upstream %d/%d, contract %d/%d",
						p, w, math.Log2(x), math.Log2(bound), upLo, upHi, gotMin, gotMax)
				}
				if gotMin != decMin || gotMax != decMax {
					t.Fatalf("-s%v of %d: contract %d/%d is not the decimal answer %d/%d (upstream %d/%d)",
						p, w, gotMin, gotMax, decMin, decMax, upLo, upHi)
				}
			}
		}
	}
	if changes == 0 {
		t.Error("the generator found no changes; it no longer targets the boundary")
	}
	t.Logf("%d changes, all decimal-exact", changes)
}

// TestThresholdsPinnedChanges pins two changes against upstream below
// 2^52 (found by the review of the design, issue #17): upstream's double
// rounding gives a maximum one above the exact decimal answer.
func TestThresholdsPinnedChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		p                float64
		w                tract.Supp
		upMin, upMax     tract.Supp
		wantMin, wantMax tract.Supp
	}{
		// 3.091 % of 833815673989 is 25773242482.999989 (decimal)
		{3.091, 833815673989, 25773242483, 25773242483, 25773242483, 25773242482},
		// 16.01 % of 1290957281599 is 206682260783.99990 (decimal)
		{16.01, 1290957281599, 206682260784, 206682260784, 206682260784, 206682260783},
	}
	for _, tt := range tests {
		if upMin(tt.p, tt.w) != tt.upMin || upMax(tt.p, tt.w) != tt.upMax {
			t.Errorf("-s%v of %d: upstream %d/%d, expected %d/%d", tt.p, tt.w,
				upMin(tt.p, tt.w), upMax(tt.p, tt.w), tt.upMin, tt.upMax)
		}
		if got, want := minSupp(tt.p, tt.w), tt.wantMin; got != want {
			t.Errorf("-s%v of %d: minimum %d, expected %d", tt.p, tt.w, got, want)
		}
		if got, want := maxSupp(tt.p, tt.w), tt.wantMax; got != want {
			t.Errorf("-S%v of %d: maximum %d, expected %d", tt.p, tt.w, got, want)
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
		wantSupp := max(tract.Supp(thresh.New(tt.conf).Min(int64(tt.body))), 1)
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
