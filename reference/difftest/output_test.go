package difftest

import (
	"math"
	"slices"
	"testing"
)

func TestParseOutput(t *testing.T) {
	t.Parallel()
	setFmt := infoFormat{open: " (", close: ")", fields: []infoField{{code: 'a'}, {code: 'e', digits: 17}}}
	ruleFmt := infoFormat{open: " [", close: "]", fields: []infoField{{code: 'a'}, {code: 'b'}, {code: 'h'}, {code: 'C'}}}
	if got := setFmt.String(); got != " (%a,%17e)" {
		t.Fatalf("format %q", got)
	}
	recs, err := parseOutput([]byte("b a (3,0.5)\n (4,inf)\n#x (1,nan)\n"), setFmt, false, " ", " <- ")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || !slices.Equal(recs[0].items, []string{"a", "b"}) || recs[0].values[1] != 0.5 ||
		len(recs[1].items) != 0 || !math.IsInf(recs[1].values[1], 1) || !math.IsNaN(recs[2].values[1]) {
		t.Fatalf("parsed %+v", recs)
	}
	recs, err = parseOutput([]byte("c => b,a [2,3,4,66.6667]\nd =>  [5,9,5,55.5556]\n"), ruleFmt, true, ",", " => ")
	if err != nil {
		t.Fatal(err)
	}
	if recs[0].head != "c" || !slices.Equal(recs[0].items, []string{"a", "b"}) || recs[0].size() != 3 ||
		recs[1].head != "d" || len(recs[1].items) != 0 || recs[1].key != ruleKey("d", nil) {
		t.Fatalf("parsed %+v", recs)
	}
	for _, bad := range []string{"a b (1)\n", "a b (1,x)\n", "a a (1,2)\n", "a b 1,2\n"} {
		if _, err := parseOutput([]byte(bad), setFmt, false, " ", " <- "); err == nil {
			t.Errorf("parseOutput(%q): no error", bad)
		}
	}
	if _, err := parseOutput([]byte("a (1.5,2)\n"), setFmt, false, " ", " <- "); err == nil {
		t.Error("non-integer support accepted")
	}
}

func TestParseStatsAndSpectrum(t *testing.T) {
	t.Parallel()
	all, per, ok := parseStats([]byte("all: 5\n  0: 0\n  1: 3\n  2: 2\n"))
	if !ok || all != 5 || !slices.Equal(per, []int64{0, 3, 2}) {
		t.Errorf("parseStats = %d %v %v", all, per, ok)
	}
	if _, _, ok := parseStats([]byte("a (1)\nall: 1\n")); ok {
		t.Error("parseStats accepted item sets")
	}
	psp, err := parseSpectrum([]byte("1 3 2\n2 1 1\n"))
	if err != nil || psp[[2]int64{1, 3}] != 2 || psp[[2]int64{2, 1}] != 1 || len(psp) != 2 {
		t.Errorf("parseSpectrum = %v, %v", psp, err)
	}
	if got := reportedCount([]byte("writing out.txt ... [17 set(s)] done [T].\n")); got != 17 {
		t.Errorf("reportedCount = %d", got)
	}
	if got := string(normStderr([]byte("done [0.00s].\ndone [12.34s].\n"))); got != "done [T].\ndone [T].\n" {
		t.Errorf("normStderr = %q", got)
	}
}

func TestFloatsMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		a, b    float64
		digits  int
		eq, tol bool
	}{
		{"identical", 0.25, 0.25, 17, true, false},
		{"nan", math.NaN(), math.NaN(), 17, true, false},
		{"inf", math.Inf(1), math.Inf(1), 17, true, false},
		{"inf vs finite", math.Inf(1), 1, 17, false, false},
		{"last bits", 0.34860570030072402, 0.34860570030072391, 17, true, true},
		{"beyond relTol", 1, 1 + 1e-10, 17, false, false},
		{"one unit at 6 digits", 66.6667, 66.6666, 6, true, true},
		{"two units at 6 digits", 66.6668, 66.6666, 6, false, false},
		{"near zero", 1e-17, -2e-17, 17, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eq, tol, _ := floatsMatch(tt.a, tt.b, tt.digits, relTol, absTol)
			if eq != tt.eq || tol != tt.tol {
				t.Errorf("floatsMatch(%v, %v, %d) = %v, %v; want %v, %v", tt.a, tt.b, tt.digits, eq, tol, tt.eq, tt.tol)
			}
		})
	}
}
