package oracle

import (
	"math"
	"slices"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

func TestAbsMinSupp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		arg      float64
		total    tract.Supp
		expected tract.Supp
	}{
		{name: "upstream default 10 percent", arg: 10, total: 10, expected: 1},
		{name: "20 percent of test1", arg: 20, total: 10, expected: 2},
		{name: "30 percent of test1", arg: 30, total: 10, expected: 3},
		{name: "rounding up", arg: 25, total: 10, expected: 3},
		// (28/100)*25 = 7.000000000000001; naive ceil gives 8.
		{name: "epsilon keeps exact 7", arg: 28, total: 25, expected: 7},
		{name: "epsilon keeps exact 28", arg: 56, total: 50, expected: 28},
		{name: "zero percent", arg: 0, total: 10, expected: 0},
		{name: "negative zero is a percentage", arg: math.Copysign(0, -1), total: 10, expected: 0},
		{name: "100 percent", arg: 100, total: 7, expected: 7},
		{name: "absolute", arg: -2, total: 10, expected: 2},
		{name: "absolute fraction rounds up", arg: -2.5, total: 10, expected: 3},
		{name: "absolute beyond total", arg: -50, total: 10, expected: 50},
		{name: "saturates", arg: -1e300, total: 10, expected: math.MaxInt64},
		{name: "nan", arg: math.NaN(), total: 10, expected: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AbsMinSupp(tt.arg, tt.total); got != tt.expected {
				t.Errorf("AbsMinSupp(%v, %d) = %d, want %d", tt.arg, tt.total, got, tt.expected)
			}
		})
	}
}

func TestAbsMaxSupp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		arg      float64
		total    tract.Supp
		expected tract.Supp
	}{
		{name: "upstream default 100 percent", arg: 100, total: 10, expected: 10},
		{name: "50 percent", arg: 50, total: 10, expected: 5},
		{name: "rounding down", arg: 55, total: 10, expected: 5},
		// (58/100)*50 = 28.999999999999996; naive floor gives 28.
		{name: "epsilon keeps exact 29", arg: 58, total: 50, expected: 29},
		{name: "epsilon keeps exact 63", arg: 70, total: 90, expected: 63},
		{name: "absolute", arg: -5, total: 10, expected: 5},
		{name: "absolute fraction rounds down", arg: -5.9, total: 10, expected: 5},
		{name: "saturates", arg: 1e300, total: 10, expected: math.MaxInt64},
		{name: "nan", arg: math.NaN(), total: 10, expected: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AbsMaxSupp(tt.arg, tt.total); got != tt.expected {
				t.Errorf("AbsMaxSupp(%v, %d) = %d, want %d", tt.arg, tt.total, got, tt.expected)
			}
		})
	}
}

func TestRuleSetMinSupp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		bodyMin  tract.Supp
		conf     float64
		expected tract.Supp
	}{
		{name: "test1 s40 c70", bodyMin: 4, conf: 0.7, expected: 3},
		{name: "test1 s20 c50", bodyMin: 2, conf: 0.5, expected: 1},
		{name: "full confidence", bodyMin: 4, conf: 1, expected: 4},
		{name: "zero confidence", bodyMin: 4, conf: 0, expected: 0},
		// 25*0.28 = 7.000000000000001; without the epsilon ceil gives 8.
		{name: "epsilon keeps exact 7", bodyMin: 25, conf: 0.28, expected: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := RuleSetMinSupp(tt.bodyMin, tt.conf); got != tt.expected {
				t.Errorf("RuleSetMinSupp(%d, %v) = %d, want %d", tt.bodyMin, tt.conf, got, tt.expected)
			}
		})
	}
}

func TestEffectiveMinSupp(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ input, expected tract.Supp }{{-3, 1}, {0, 1}, {1, 1}, {7, 7}} {
		if got := EffectiveMinSupp(tt.input); got != tt.expected {
			t.Errorf("EffectiveMinSupp(%d) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestAbsBorder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []float64
		zmin     int
		expected []tract.Supp
	}{
		// Upstream: -F60:50 -s20 on test1 needs >= 6 for size 1, >= 5 for size 2.
		{name: "percentages from zmin 1", args: []float64{60, 50}, zmin: 1, expected: []tract.Supp{0, 6, 5}},
		{name: "absolute values", args: []float64{-6, -5}, zmin: 1, expected: []tract.Supp{0, 6, 5}},
		{name: "from zmin 0", args: []float64{100}, zmin: 0, expected: []tract.Supp{10}},
		{name: "from zmin 3", args: []float64{30}, zmin: 3, expected: []tract.Supp{0, 0, 0, 3}},
		{name: "no border", args: nil, zmin: 1, expected: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AbsBorder(tt.args, tt.zmin, 10); !slices.Equal(got, tt.expected) {
				t.Errorf("AbsBorder(%v, %d, 10) = %v, want %v", tt.args, tt.zmin, got, tt.expected)
			}
		})
	}
}
