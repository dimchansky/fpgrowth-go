package main

import (
	"math"
	"math/big"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
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
	// below 2^52 the contract keeps upstream's double formula, also where
	// its rounding gives a maximum one above the exact decimal answer
	// (3.091 % of 833815673989 is 25773242482.99999, 16.01 % of
	// 1290957281599 is 206682260783.9999)
	s3091 := writeFile(t, dir, "s3091.txt", "a 25773242483\nb 808042431506\n")
	s1601 := writeFile(t, dir, "s1601.txt", "a 206682260784\nb 1084275020815\n")
	// -F with a non-integer x: W = 2^62+1, 50 % is 2^61+0.5; the border
	// (a minimum) is 2^61, so d (2^61) and b (2^61+1) qualify, a (2^61-300)
	// does not (upstream: 2^61-512); a maximum would be 2^61+1
	f50 := writeFile(t, dir, "f50.txt", "d b 2305843009213693952\nb 1\na 2305843009213693652\nc 300\n")
	// ordinary weight: 33 % of 10 is 3.3, the border is 4
	f33 := writeFile(t, dir, "f33.txt", "a 3\nb 4\nc 3\n")
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
		{"-S3.091", []string{"-w", "-s0", "-S3.091", "-v %a", s3091, "-"}, "a 25773242483\n"},
		{"-S16.01", []string{"-w", "-s0", "-S16.01", "-v %a", s1601, "-"}, "a 206682260784\n"},
		{"-F50 non-integer", []string{"-w", "-s0", "-n1", "-F50", "-v %a", f50, "-"}, "b 2305843009213693953\nd 2305843009213693952\n"},
		{"-F33", []string{"-w", "-s0", "-F33", "-v %a", f33, "-"}, "b 4\n"},
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

// TestBorderUnchanged checks that the -F conversion keeps setbdr's double
// formula ceil(v/100*w*(1-DBL_EPSILON)) wherever x = v/100·w is below
// 2^52 (threshold contract, differences.md N2), for adversarial values:
// 1-3 decimals, values one ulp off a decimal, 100-a and running sums.
func TestBorderUnchanged(t *testing.T) {
	t.Parallel()
	up := func(v float64, w tract.Supp) tract.Supp {
		s := math.Ceil(v / 100.0 * float64(w) * (1 - 0x1p-52))
		if !(s > 0) {
			return 0
		}
		return tract.Supp(s)
	}
	r := rand.New(rand.NewPCG(17, 9))
	for range 200000 {
		var v float64
		switch r.IntN(4) {
		case 0:
			v, _ = strconv.ParseFloat(strconv.FormatFloat(float64(r.IntN(100001))/1000, 'f', 1+r.IntN(3), 64), 64)
		case 1:
			v = math.Nextafter(float64(1+r.IntN(10000))/100, float64(r.IntN(2)*200))
		case 2:
			v = 100 - float64(r.IntN(10000))/100
		default:
			for range 1 + r.IntN(3000) {
				v += 0.01
			}
		}
		w := tract.Supp(1 + r.Int64N(1<<31))
		if r.IntN(2) == 0 {
			w = tract.Supp(100*(1+r.Int64N(1<<40)) + r.Int64N(2))
		}
		x := new(big.Rat).Mul(new(big.Rat).SetFloat64(v/100), new(big.Rat).SetInt64(int64(w)))
		if x.Cmp(new(big.Rat).SetInt64(1<<52)) >= 0 {
			continue
		}
		if a, b := up(v, w), borderSupp(v, w); a != b {
			t.Fatalf("-F%v of %d: upstream %d, contract %d", v, w, a, b)
		}
	}
}
