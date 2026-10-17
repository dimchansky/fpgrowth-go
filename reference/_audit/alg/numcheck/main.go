// Command numcheck compares every rule evaluation measure of
// internal/ruleval against the independent definition-based
// implementation in measures.go (copied from ../bf) over all 2x2 tables
// with total weight n <= -n (Fisher variants: n <= -nf), plus a set of
// large-weight tables.
package main

import (
	"flag"
	"fmt"
	"math"
	"sort"

	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

func tol(a, b float64, rel float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}
	if math.IsInf(a, 0) || math.IsInf(b, 0) {
		return a == b
	}
	return math.Abs(a-b) <= 1e-12+rel*math.Max(math.Abs(a), math.Abs(b))
}

func main() {
	nmax := flag.Int64("n", 60, "max total weight")
	nf := flag.Int64("nf", 40, "max total weight for Fisher")
	rel := flag.Float64("rel", 1e-9, "relative tolerance")
	large := flag.Bool("large", true, "also check large-weight tables")
	flag.Parse()
	cnt := map[byte]int{}
	bad := map[byte]int{}
	worst := map[byte]float64{}
	ex := map[byte]string{}
	check := func(m byte, s, b, h, n int64) {
		if m == 'f' || m == 'h' || m == 'm' || m == 's' {
			if n > *nf {
				return
			}
		}
		meas, _ := ruleval.ByLetter(m)
		g := ruleval.Eval(meas, tract.Supp(s), tract.Supp(b), tract.Supp(h), tract.Supp(n))
		e := evalRule(m, s, b, h, n, false)
		cnt[m]++
		if !tol(e, g, *rel) {
			bad[m]++
			d := math.Abs(e-g) / math.Max(math.Abs(e), 1e-300)
			if math.IsInf(e, 0) || math.IsInf(g, 0) || math.IsNaN(g) {
				d = math.Inf(1)
			}
			if d > worst[m] || ex[m] == "" {
				worst[m] = d
				ex[m] = fmt.Sprintf("s=%d b=%d h=%d n=%d def=%.17g go=%.17g", s, b, h, n, e, g)
			}
		}
	}
	for n := int64(1); n <= *nmax; n++ {
		for b := int64(1); b <= n; b++ {
			for h := int64(1); h <= n; h++ {
				for s := max(1, b+h-n); s <= min(b, h); s++ {
					for i := 0; i < len(ruleMeasures); i++ {
						check(ruleMeasures[i], s, b, h, n)
					}
				}
			}
		}
	}
	// large weights: tables scaled by big factors plus small perturbations
	for _, scale := range []int64{1e6, 1e9, 1e12, 1e15, 1 << 60} {
		if !*large {
			break
		}
		for _, t := range [][4]int64{{1, 2, 2, 4}, {1, 3, 3, 9}, {3, 5, 7, 12}, {2, 4, 5, 9}} {
			for _, d := range []int64{0, 1, 2, 7} {
				s, b, h, n := t[0]*scale-d, t[1]*scale, t[2]*scale, t[3]*scale
				if n < 0 || s < 1 {
					continue
				}
				for i := 0; i < len(ruleMeasures); i++ {
					check(ruleMeasures[i], s, b, h, n)
				}
			}
		}
		// near-degenerate: head in all but 2, body small
		n := 9 * scale
		check('g', 3*scale-2, 3*scale, n-2, n)
		check('i', 3*scale-2, 3*scale, n-2, n)
		check('p', 3*scale-2, 3*scale, n-2, n)
		check('n', 3*scale-2, 3*scale, n-2, n)
	}
	var ms []byte
	for m := range cnt {
		ms = append(ms, m)
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
	for _, m := range ms {
		fmt.Printf("%c checked=%d mismatches=%d", m, cnt[m], bad[m])
		if bad[m] > 0 {
			fmt.Printf(" worst-rel=%.3g e.g. %s", worst[m], ex[m])
		}
		fmt.Println()
	}
}
