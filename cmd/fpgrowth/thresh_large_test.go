package main

import (
	"strings"
	"testing"
)

// TestALG2CLI runs the CLI reproductions of audit ALG-2 (issue #17,
// differences.md N2) and the other relative thresholds at a total weight
// of 2^62, where upstream's relative (1-ε) tolerance is 1024 units: before
// the threshold contract, -s50 reported a (49.999999999999992895 %), -c100
// reported y <- x (confidence 99.999999999999971578 %), -S50 reported b
// (2^61+200) and -F50 reported a (2^61-200).
func TestALG2CLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s50 := writeFile(t, dir, "s50.txt", "a 2305843009213693752\nb 2305843009213694152\n")
	c100 := writeFile(t, dir, "c100.txt", "x y 4611686018427387000\nx 1000\n")
	// below 2^52, upstream's double rounding gives a maximum one above the
	// exact decimal answer (3.091 % of 833815673989 is 25773242482.99999,
	// 16.01 % of 1290957281599 is 206682260783.9999)
	s3091 := writeFile(t, dir, "s3091.txt", "a 25773242483\nb 808042431506\n")
	s1601 := writeFile(t, dir, "s1601.txt", "a 206682260784\nb 1084275020815\n")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"-s50", []string{"-w", "-s50", s50, "-"}, "b (50)\n"},
		{"-c100", []string{"-tr", "-w", "-s0", "-c100", "-m2", c100, "-"}, "x <- y (100, 100)\n"},
		{"-S50", []string{"-w", "-s0", "-S50", "-v %a", s50, "-"}, "a 2305843009213693752\n"},
		{"-F50", []string{"-w", "-s0", "-F50", "-v %a", s50, "-"}, "b 2305843009213694152\n"},
		{"-S100", []string{"-w", "-s0", "-S100", "-v %a", s50, "-"}, "b 2305843009213694152\na 2305843009213693752\n"},
		{"-Sinf", []string{"-w", "-s0", "-Sinf", "-v %a", s50, "-"}, "b 2305843009213694152\na 2305843009213693752\n"},
		{"-S-0", []string{"-w", "-s0", "-S-0", "-v %a", s50, "-"}, ""},
		{"-F-0", []string{"-w", "-s0", "-F-0", "-v %a", s50, "-"}, "b 2305843009213694152\na 2305843009213693752\n"},
		{"-s3.091", []string{"-w", "-s3.091", "-v %a", s3091, "-"}, "b 808042431506\na 25773242483\n"},
		{"-S3.091", []string{"-w", "-s0", "-S3.091", "-v %a", s3091, "-"}, ""},
		{"-S16.01", []string{"-w", "-s0", "-S16.01", "-v %a", s1601, "-"}, ""},
		{"-S16.0101", []string{"-w", "-s0", "-S16.0101", "-v %a", s1601, "-"}, "a 206682260784\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, stderr, code := invocation{args: tt.args}.run(t)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if out != tt.want {
				t.Errorf("%s: output\n%s\nexpected\n%s", strings.Join(tt.args[:len(tt.args)-2], " "), out, tt.want)
			}
		})
	}
}
