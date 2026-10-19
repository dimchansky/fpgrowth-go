package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// TestALG1CLI runs the CLI reproductions of audit ALG-1 (issue #16):
// before the fix the program printed 0.0243 (exact 0.0455) for -ep, 1
// (exact 0.0361 and 1/9) for -eg and -ef on chi_big, 0 (exact 1 and
// 25/36) for -en and -ey on the perfect association and 0 (exact 1) for
// -ez on the certainty case. Every value must match the exact reference
// within its documented bound.
func TestALG1CLI(t *testing.T) {
	t.Parallel()
	const m = 9223372036854775800
	cases := []struct {
		name, data, measures, thresh string
		s, b, h, n                   tract.Supp // rule y <- x
	}{
		{"chi_big", "x y 2999999999999998\nx 2\ny 6000000000000000\n", "pgf", "100",
			2999999999999998, 3000000000000000, 8999999999999998, 9000000000000000},
		{"perfect association", "x y 9223372036854775800\nq 3\n", "ny", "0", m, m, m, m + 3},
		{"certainty", "x y 2\ny 9223372036854775796\n2\n", "z", "0", 2, 2, m - 2, m},
	}
	dir := t.TempDir()
	for _, c := range cases {
		in := writeFile(t, dir, strings.ReplaceAll(c.name, " ", "_")+".txt", c.data)
		for _, e := range c.measures {
			out, stderr, code := invocation{args: []string{"-tr", "-w", "-s0", "-c0", "-m2",
				"-e" + string(e), "-d" + c.thresh, "-v %a %b %h %20e", in, "-"}}.run(t)
			if code != 0 {
				t.Fatalf("%s -e%c: exit %d: %s", c.name, e, code, stderr)
			}
			var got float64
			found := false
			for line := range strings.SplitSeq(out, "\n") {
				f := strings.Fields(line)
				if len(f) == 7 && f[0] == "y" && f[2] == "x" {
					v, err := strconv.ParseFloat(f[6], 64)
					if err != nil {
						t.Fatal(err)
					}
					got, found = v, true
				}
			}
			if !found {
				t.Fatalf("%s -e%c: rule y <- x not in output:\n%s", c.name, e, out)
			}
			ref, _ := oracle.RefByLetter(byte(e))
			want, err := ref.Eval(c.s, c.b, c.h, c.n)
			if err != nil {
				t.Fatal(err)
			}
			// %20e prints 20 decimals, so allow 1e-20 for the output too.
			if d := got - want.V; d > want.Tol+1e-20 || -d > want.Tol+1e-20 {
				t.Errorf("%s -e%c: printed %v, reference %v", c.name, e, got, want)
			}
		}
	}
}

// TestCertaintyNearIndependenceCLI is the CLI reproduction of review
// finding F1 of #16: the rule y <- x is slightly below independence
// (certainty factor -1.2e-19, chi² 1.9e-20), but upstream's formula gave
// it the certainty factor 1, so -ez -d99 reported it. It must not be
// reported, and with -d-100 (no filtering) its value must match the
// reference.
func TestCertaintyNearIndependenceCLI(t *testing.T) {
	t.Parallel()
	in := writeFile(t, t.TempDir(), "near.txt", "x y 7974968969870368360\nx 61\ny 791446729924245358\nq 5\n")
	out, stderr, code := invocation{args: []string{"-tr", "-w", "-s0", "-c0", "-m2", "-ez", "-d99",
		"-v %a %b %h %20e", in, "-"}}.run(t)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.Contains(out, "y <- x") {
		t.Errorf("-ez -d99 reports y <- x:\n%s", out)
	}
	out, stderr, code = invocation{args: []string{"-tr", "-w", "-s0", "-c0", "-m2", "-ez", "-d-100",
		"-v %a %b %h %20e", in, "-"}}.run(t)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	ref, _ := oracle.RefByLetter('z')
	want, _ := ref.Eval(7974968969870368360, 7974968969870368421, 8766415699794613718, 8766415699794613784)
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 7 && f[0] == "y" && f[2] == "x" {
			v, err := strconv.ParseFloat(f[6], 64)
			if err != nil {
				t.Fatal(err)
			}
			// %20e prints 20 decimals: the value rounds to 0 there
			if d := v - want.V; d > want.Tol+1e-20 || -d > want.Tol+1e-20 {
				t.Errorf("-ez -d-100: printed %v for y <- x, reference %v", v, want)
			}
			return
		}
	}
	t.Errorf("-ez -d-100: rule y <- x not reported:\n%s", out)
}
