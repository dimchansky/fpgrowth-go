package main

import (
	"math"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// TestSuppValue checks the support literal grammar of issue #18
// (differences.md N3): the strtod value and end position are upstream's
// for every literal, and exactly the negative decimal integer literals
// carry an exact count (or, beyond int64, an error literal).
func TestSuppValue(t *testing.T) {
	t.Parallel()
	negZero := math.Copysign(0, -1)
	tests := []struct {
		in    string
		v     float64
		count int64
		bad   string
		rest  string
	}{
		{in: "-17", v: -17, count: 17},
		{in: "-17m2", v: -17, count: 17, rest: "m2"},
		{in: "-17e", v: -17, count: 17, rest: "e"}, // strtod leaves an exponent without digits
		{in: "-17e+", v: -17, count: 17, rest: "e+"},
		{in: " \t-17", v: -17, count: 17},
		{in: "-0017", v: -17, count: 17},
		{in: "-1", v: -1, count: 1},
		{in: "-17.0", v: -17},
		{in: "-17.", v: -17},
		{in: "-1e3", v: -1000},
		{in: "-17E0Z", v: -17, rest: "Z"},
		{in: "-0x11", v: -17},
		{in: "-0X11p0", v: -17},
		{in: "-2.5", v: -2.5},
		{in: "-inf", v: math.Inf(-1)},
		{in: "-infinity", v: math.Inf(-1)},
		{in: "-0", v: negZero},
		{in: "-000", v: negZero},
		{in: "-0.0", v: negZero},
		{in: "17", v: 17},
		{in: "+17", v: 17},
		{in: "0", v: 0},
		{in: "", v: 0},
		{in: "-", v: 0, rest: "-"},
		{in: "--17", v: 0, rest: "--17"},
		{in: "-+17", v: 0, rest: "-+17"},
		{in: "- 17", v: 0, rest: "- 17"},
		{in: "-9007199254740991", v: -(1<<53 - 1), count: 1<<53 - 1},
		{in: "-9007199254740992", v: -(1 << 53), count: 1 << 53},
		{in: "-9007199254740993", v: -(1 << 53), count: 1<<53 + 1},     // strtod rounds to even
		{in: "-9007199254740993.0", v: -(1 << 53)},                     // not an integer literal
		{in: "-4611686018427387840", v: -(1 << 62), count: 1<<62 - 64}, // strtod rounds up
		{in: "-4611686018427387904", v: -(1 << 62), count: 1 << 62},    // 2^62
		{in: "-9223372036854775807", v: -0x1p63, count: math.MaxInt64}, // MaxInt64
		{in: "-9223372036854775806m2", v: -0x1p63, count: math.MaxInt64 - 1, rest: "m2"},
		{in: "-9223372036854775808", v: -0x1p63, bad: "-9223372036854775808"}, // MaxInt64+1
		{in: " -99999999999999999999Z", v: -1e20, bad: "-99999999999999999999", rest: "Z"},
		{in: "-1e19", v: -1e19},
		{in: "9223372036854775808", v: 0x1p63}, // a percentage, rejected by validate as > 100
	}
	for _, tt := range tests {
		a, rest := suppValue(tt.in)
		fv, frest := floatValue(tt.in)
		if math.Float64bits(a.v) != math.Float64bits(fv) || rest != frest {
			t.Errorf("suppValue(%q) = %v, %q; strtod %v, %q", tt.in, a.v, rest, fv, frest)
		}
		if math.Float64bits(a.v) != math.Float64bits(tt.v) || a.count != tt.count || a.bad != tt.bad || rest != tt.rest {
			t.Errorf("suppValue(%q) = %+v, %q; want v %v count %d bad %q rest %q",
				tt.in, a, rest, tt.v, tt.count, tt.bad, tt.rest)
		}
		if a.exact() != (tt.count > 0) {
			t.Errorf("suppValue(%q).exact() = %v", tt.in, a.exact())
		}
	}
}

// TestAbsoluteOptions checks parsing, validation and the miner
// configuration of exact absolute thresholds: errors for integer
// literals beyond int64 (in upstream's validation order, with exit
// status 11), clustering, mixed kinds, -F entries and the sign rule of
// the default output format.
func TestAbsoluteOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"-s-9223372036854775808", "x"}, exitSupport, "invalid minimum support -9223372036854775808"},
		{[]string{"-s-99999999999999999999", "x"}, exitSupport, "invalid minimum support -99999999999999999999"},
		{[]string{"-s -9223372036854775808", "x"}, exitSupport, "invalid minimum support -9223372036854775808"},
		{[]string{"-S-9223372036854775808", "x"}, exitSupport, "invalid maximum support -9223372036854775808"},
		{[]string{"-F-5:-9223372036854775808", "x"}, exitSupport, "invalid support border -9223372036854775808"},
		{[]string{"-m-1", "-s-9223372036854775808", "x"}, exitSize, "invalid item set size -1"},
		{[]string{"-s-9223372036854775808", "-Snan", "x"}, exitSupport, "invalid minimum support -9223372036854775808"},
		{[]string{"-s200", "-S-9223372036854775808", "x"}, exitSupport, "invalid minimum support 200"},
		{[]string{"-S-9223372036854775808", "-c200", "x"}, exitSupport, "invalid maximum support -9223372036854775808"},
		// the last value wins, as upstream
		{[]string{"-s-9223372036854775808", "-s-5", "x"}, 0, ""},
		// non-integer syntax keeps upstream's double (saturating)
		{[]string{"-s-1e19", "-S-1e400", "-F-9223372036854775808.0", "x"}, 0, ""},
	} {
		o, _, err := parseArgs(tc.args)
		if err == nil {
			err = o.validate()
		}
		switch {
		case tc.code == 0 && err != nil:
			t.Errorf("%q: error %d %q", tc.args, err.code, err.msg)
		case tc.code != 0 && (err == nil || err.code != tc.code || err.msg != tc.msg):
			t.Errorf("%q: error %v, want %d %q", tc.args, err, tc.code, tc.msg)
		}
	}

	type want struct {
		hasMin, hasMax bool
		minC, maxC     tract.Supp
		smin, smax     float64
	}
	for _, tc := range []struct {
		args []string
		want want
	}{
		{[]string{"-s-9007199254740993m2", "x"}, want{true, false, 1<<53 + 1, 0, -(1 << 53), 100}},
		{[]string{"-s-4611686018427387840", "-S50", "x"}, want{true, false, 1<<62 - 64, 0, -(1 << 62), 50}},
		{[]string{"-s30", "-S-9223372036854775807", "x"}, want{false, true, 0, math.MaxInt64, 30, -0x1p63}},
		{[]string{"-s-5", "-S-7", "x"}, want{true, true, 5, 7, -5, -7}},
		{[]string{"-s-2.5", "-S-7.5", "x"}, want{false, false, 0, 0, -2.5, -7.5}},
		{[]string{"-s-0", "-S-0", "x"}, want{false, false, 0, 0, math.Copysign(0, -1), math.Copysign(0, -1)}},
		{[]string{"-s-5", "-s20", "x"}, want{false, false, 0, 0, 20, 100}},
	} {
		o, _, err := parseArgs(tc.args)
		if err == nil {
			err = o.validate()
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.args, err)
		}
		c := o.minerConfig()
		got := want{c.HasMinCount, c.HasMaxCount, c.MinCount, c.MaxCount, c.SMin, c.SMax}
		if got != tc.want && !(got.smin == 0 && tc.want.smin == 0) {
			t.Errorf("%q: %+v, want %+v", tc.args, got, tc.want)
		}
		if _, err := fpg.New[string](c); err != nil {
			t.Errorf("%q: %v", tc.args, err)
		}
	}

	o, _, err := parseArgs([]string{"-F-9007199254740993:50:-3.5:-7Z", "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.border) != 4 || o.border[0].count != 1<<53+1 || o.border[1] != (suppArg{v: 50}) ||
		o.border[2] != (suppArg{v: -3.5}) || o.border[3].count != 7 || !o.stats {
		t.Errorf("border %+v -Z %v", o.border, o.stats)
	}
}

// TestALG3CLI runs the reproductions of audit ALG-3 (issue #18,
// differences.md N3) and absolute thresholds around 2^53, 2^62 and
// math.MaxInt64 through the program. Before the fix every case marked
// "rounded" failed: strtod rounded the threshold to a double, so sets
// below the minimum were reported, sets at the minimum (or maximum) were
// lost, or no item was found.
func TestALG3CLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// audit ALG-3: supports 2^61-200 and 2^61+200
	s50 := writeFile(t, dir, "s50.txt", "a 2305843009213693752\nb 2305843009213694152\n")
	// a has support 2^62-64, which strtod rounds up to 2^62
	s62 := writeFile(t, dir, "s62.txt", "a 4611686018427387840\nb 4611686018427387000\n")
	// 2^53+1, 2^53 and 2^53-1 (sum below 2^63)
	s53 := writeFile(t, dir, "s53.txt", "a 9007199254740993\nb 9007199254740992\nc 9007199254740991\n")
	// 2^53+2 and 2^53+1
	m53 := writeFile(t, dir, "m53.txt", "a 9007199254740994\nb 9007199254740993\n")
	// math.MaxInt64-1 (and 1): the total weight is math.MaxInt64
	smax := writeFile(t, dir, "smax.txt", "a 9223372036854775806\nb 1\n")
	// math.MaxInt64 itself
	sall := writeFile(t, dir, "sall.txt", "a 9223372036854775807\n")
	tests := []struct {
		name string
		args []string
		want string
		code int
	}{
		{"ALG-3 -s (rounded)", []string{"-w", "-s-2305843009213693753", s50, "-"}, "b (2305843009213694152)\n", 0},
		{"ALG-3 -s at the support", []string{"-w", "-s-2305843009213693752", s50, "-"},
			"b (2305843009213694152)\na (2305843009213693752)\n", 0},
		{"ALG-3 2^62-64 (rounded)", []string{"-w", "-s-4611686018427387840", s62, "-"}, "a (4611686018427387840)\n", 0},
		{"2^62-63", []string{"-w", "-s-4611686018427387841", s62, "-"}, "", exitNoItems},
		{"2^53+1 (rounded)", []string{"-w", "-s-9007199254740993", s53, "-"}, "a (9007199254740993)\n", 0},
		{"2^53", []string{"-w", "-s-9007199254740992", s53, "-"}, "a (9007199254740993)\nb (9007199254740992)\n", 0},
		{"2^53-1", []string{"-w", "-s-9007199254740991", s53, "-"},
			"a (9007199254740993)\nb (9007199254740992)\nc (9007199254740991)\n", 0},
		{"2^53 clustered", []string{"-ws-9007199254740993n1v %a", s53, "-"}, "a 9007199254740993\n", 0},
		{"MaxInt64-1 (rounded)", []string{"-w", "-s-9223372036854775806", smax, "-"}, "a (9223372036854775806)\n", 0},
		{"MaxInt64", []string{"-w", "-s-9223372036854775807", smax, "-"}, "", exitNoItems},
		{"MaxInt64 support", []string{"-w", "-s-9223372036854775807", sall, "-"}, "a (9223372036854775807)\n", 0},
		{"MaxInt64+1", []string{"-w", "-s-9223372036854775808", sall, "-"}, "", exitSupport},
		{"-S 2^53+1 (rounded)", []string{"-w", "-s-1", "-S-9007199254740993", m53, "-"}, "b (9007199254740993)\n", 0},
		{"-S MaxInt64", []string{"-w", "-s-1", "-S-9223372036854775807", smax, "-"},
			"a (9223372036854775806)\nb (1)\n", 0},
		{"-S MaxInt64-2 (rounded)", []string{"-w", "-s-1", "-S-9223372036854775805", smax, "-"}, "b (1)\n", 0},
		{"-S mixed with -s percentage", []string{"-w", "-s10", "-S-9007199254740993", "-v %a", m53, "-"},
			"b 9007199254740993\n", 0},
		{"-s mixed with -S percentage", []string{"-w", "-s-9007199254740994", "-S100", "-v %a", m53, "-"},
			"a 9007199254740994\n", 0},
		{"-F (rounded)", []string{"-w", "-s0", "-F-2305843009213693753", "-v %a", s50, "-"}, "b 2305843009213694152\n", 0},
		{"-F mixed list", []string{"-w", "-s-1", "-F-9223372036854775806:50", "-v %a", smax, "-"},
			"a 9223372036854775806\n", 0},
		// non-integer absolute values keep upstream's ceil/floor of the double
		{"-s-2.5", []string{"-w", "-s-2.5", "-v %a", writeFile(t, dir, "small.txt", "a 3\nb 2\n"), "-"}, "a 3\n", 0},
		{"-S-2.5", []string{"-w", "-s-1", "-S-2.5", "-v %a", writeFile(t, dir, "small2.txt", "a 3\nb 2\n"), "-"}, "b 2\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, stderr, code := invocation{args: tt.args}.run(t)
			if code != tt.code {
				t.Fatalf("exit %d, want %d: %s", code, tt.code, stderr)
			}
			if out != tt.want {
				t.Errorf("%s: output\n%s\nexpected\n%s", strings.Join(tt.args[:len(tt.args)-2], " "), out, tt.want)
			}
		})
	}
}
