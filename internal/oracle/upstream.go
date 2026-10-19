package oracle

import (
	"math"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Upstream's double formulas, which the threshold contract keeps below
// the switch at x = t·n = 2^52 (see threshold.go). They are restated from
// the C sources, in their evaluation order, and are the definition there:
//
//	fpg_data:   ceil((smin/100)*w*(1-DBL_EPSILON))
//	fpg_report: floor((smax/100)*w*(1+DBL_EPSILON))
//	istree:     reject a rule if supp < body*(conf*(1-DBL_EPSILON))
//
// with the fraction t = smin/100 (or conf) already divided. Their values
// are below 2^63 wherever the contract uses them.

// upEps is C's DBL_EPSILON.
const upEps = 0x1p-52

// upSat converts an integral double to a support, 0 for values <= 0 and
// NaN.
func upSat(v float64) tract.Supp {
	if !(v > 0) {
		return 0
	}
	return tract.Supp(v)
}

// upstreamMin is fpg_data's minimum support of the fraction t of n.
func upstreamMin(t float64, n tract.Supp) tract.Supp {
	return upSat(math.Ceil(t * float64(n) * (1 - upEps)))
}

// upstreamMax is fpg_report's maximum support of the fraction t of n.
func upstreamMax(t float64, n tract.Supp) tract.Supp {
	return upSat(math.Floor(t * float64(n) * (1 + upEps)))
}

// upstreamMeetsConf is istree's confidence test for the support s and the
// body support b.
func upstreamMeetsConf(s tract.Supp, conf float64, b tract.Supp) bool {
	return !(float64(s) < float64(b)*(conf*(1-upEps)))
}

// upstreamConfMin is the smallest support that upstreamMeetsConf accepts
// for a product below 2^52 (where every integer is a double).
func upstreamConfMin(conf float64, b tract.Supp) tract.Supp {
	return upSat(math.Ceil(float64(b) * (conf * (1 - upEps))))
}
