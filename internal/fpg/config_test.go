package fpg

import (
	"errors"
	"math"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/report"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mod  func(c *Config)
	}{
		{"target bits", func(c *Config) { c.Target = 0x10 }},
		{"negative target", func(c *Config) { c.Target = -1 }},
		{"smin above 100", func(c *Config) { c.SMin = 100.5 }},
		{"smin NaN", func(c *Config) { c.SMin = math.NaN() }},
		{"smax NaN", func(c *Config) { c.SMax = math.NaN() }},
		{"conf below 0", func(c *Config) { c.Conf = -1 }},
		{"conf above 100", func(c *Config) { c.Conf = 101 }},
		{"conf NaN", func(c *Config) { c.Conf = math.NaN() }},
		{"zmin negative", func(c *Config) { c.ZMin = -1 }},
		{"zmax negative", func(c *Config) { c.ZMax = -1 }},
		{"eval negative", func(c *Config) { c.Eval = -1 }},
		{"eval too large", func(c *Config) { c.Eval = EvalLogRatio + 1 }},
		{"agg", func(c *Config) { c.Agg = 4 }},
		{"thresh NaN", func(c *Config) { c.Thresh = math.NaN() }},
		{"algo", func(c *Config) { c.Algo = 5 }},
		{"negative algo", func(c *Config) { c.Algo = -1 }},
		{"mode bits", func(c *Config) { c.Mode |= 0x1f }},
		{"pack negative", func(c *Config) { c.Pack = -1 }},
	}
	for _, tt := range tests {
		c := DefaultConfig()
		tt.mod(&c)
		if _, err := New[string](c); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: err %v, want ErrConfig", tt.name, err)
		}
	}
	valid := DefaultConfig()
	valid.SMin, valid.SMax, valid.ZMin, valid.ZMax = -1e300, math.Inf(1), 0, 0
	if _, err := New[string](valid); err != nil {
		t.Errorf("valid config: %v", err)
	}
}

// TestConfigNormalize checks the fpg_create adjustments (core §1.4).
func TestConfigNormalize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target report.Target
		algo   Algorithm
		mode   Mode
		pack   int
		eval   int
		// expected
		wantTarget report.Target
		wantAlgo   Algorithm
		wantMode   Mode
		wantPack   int
	}{
		{"default", report.TargetAll, AlgoComplex, ModeDefault, 16, EvalNone,
			report.TargetAll, AlgoComplex, ModePerfect | ModeReorder, 16},
		{"target precedence", report.TargetClosed | report.TargetMaximal | report.TargetGenerators,
			AlgoComplex, ModeDefault, 16, EvalNone,
			report.TargetGenerators, AlgoComplex, ModePerfect | ModeReorder, 16},
		{"rules first", report.TargetRules | report.TargetClosed, AlgoSimple, ModeDefault, 16, EvalNone,
			report.TargetRules, AlgoSimple, ModePerfect, 0},
		{"auto", report.TargetAll, AlgoAuto, ModeDefault, 3, EvalNone,
			report.TargetAll, AlgoComplex, ModePerfect | ModeReorder, 3},
		{"generators force perfect, -At -> -Ad", report.TargetGenerators, AlgoTopDown, 0, 16, EvalNone,
			report.TargetGenerators, AlgoSingle, ModePerfect, 16},
		{"closed -Ad -> -As, no reorder", report.TargetClosed, AlgoSingle, ModeDefault, 16, EvalNone,
			report.TargetClosed, AlgoSimple, ModePerfect, 16},
		{"maximal keeps tail", report.TargetMaximal, AlgoComplex, ModeDefault, 16, EvalNone,
			report.TargetMaximal, AlgoComplex, ModePerfect | ModeTail, 16},
		{"topdown no machine", report.TargetAll, AlgoTopDown, ModeDefault, 16, EvalNone,
			report.TargetAll, AlgoTopDown, ModePerfect, 0},
		{"rule measure no machine", report.TargetAll, AlgoComplex, ModeDefault, 16, 5,
			report.TargetAll, AlgoComplex, ModePerfect | ModeReorder, 0},
		{"log ratio keeps machine", report.TargetAll, AlgoSimple, ModeDefault, 99, EvalLogRatio,
			report.TargetAll, AlgoSimple, ModePerfect, 31},
	}
	for _, tt := range tests {
		c := DefaultConfig()
		c.Target, c.Algo, c.Mode, c.Pack, c.Eval = tt.target, tt.algo, tt.mode, tt.pack, tt.eval
		m, err := New[string](c)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		got := m.Config()
		if got.Target != tt.wantTarget || got.Algo != tt.wantAlgo || got.Mode != tt.wantMode || got.Pack != tt.wantPack {
			t.Errorf("%s: got target %#x algo %v mode %#x pack %d, want %#x %v %#x %d", tt.name,
				int(got.Target), got.Algo, int(got.Mode), got.Pack,
				int(tt.wantTarget), tt.wantAlgo, int(tt.wantMode), tt.wantPack)
		}
		if tt.wantTarget != report.TargetRules && got.Conf != 100 {
			t.Errorf("%s: confidence %g, want 100", tt.name, got.Conf)
		}
	}
	for a, s := range map[Algorithm]string{AlgoSimple: "s", AlgoComplex: "c", AlgoSingle: "d",
		AlgoTopDown: "t", AlgoAuto: "auto", 9: "Algorithm(9)"} {
		if a.String() != s {
			t.Errorf("Algorithm(%d).String() = %q", int(a), a.String())
		}
	}
}
