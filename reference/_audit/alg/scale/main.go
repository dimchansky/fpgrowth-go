// Command scale shows how the chi^2 / G / Yates / certainty measures of
// internal/ruleval lose accuracy as the total weight grows (table scaled
// by k, plus a fixed small perturbation).
package main

import (
	"fmt"

	"github.com/dimchansky/fpgrowth-go/internal/ruleval"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

func main() {
	for _, m := range []byte{'p', 'g', 't', 'n', 'y', 'i', 'z'} {
		meas, _ := ruleval.ByLetter(m)
		fmt.Printf("measure %c\n", m)
		for k := int64(10); k <= 1e18; k *= 10 {
			// body = 3k, head = 9k-2, n = 9k, s = 3k-2: strong association
			// of the two transactions outside the head (G = 4.39, p = 0.036)
			s, b, h, n := 3*k-2, 3*k, 9*k-2, 9*k
			if n < 0 {
				break
			}
			g := ruleval.Eval(meas, tract.Supp(s), tract.Supp(b), tract.Supp(h), tract.Supp(n))
			e := evalRule(m, s, b, h, n, false)
			fmt.Printf("  n=9e%-3d def=%-22.15g go=%-22.15g\n", len(fmt.Sprint(k))-1, e, g)
		}
	}
}
