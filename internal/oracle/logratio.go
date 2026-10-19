package oracle

import (
	"fmt"
	"math"
	"math/big"
	"math/bits"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// LogRatioEval evaluates item sets by upstream's item set measure -eb
// (FPG_LDRATIO, the public LogSupportRatio): the binary logarithm of the
// quotient of a set's support and the support expected if its items were
// independent,
//
//	log2( (s/n) / Π_{i in S} (f_i/n) ) = log2( s·n^(|S|-1) / Π f_i ),
//
// where s is the support of S, f_i the support of item i and n the total
// weight. Sets with fewer than two items get 0 (upstream's constant,
// rules-eval.md §4.3; ConvUndefined). The value is computed exactly
// (an exact rational, then a refPrec-bit logarithm).
//
// Upstream applies it to every reported set with a reject-if-less test
// (report.c output): a set is rejected if v < Threshold. Aggregation and
// -i do not apply. Use -Inf as the threshold to compute values without
// filtering.
type LogRatioEval struct {
	Threshold float64
}

func (e *LogRatioEval) validate() error {
	if math.IsNaN(e.Threshold) {
		return fmt.Errorf("oracle: NaN log ratio threshold")
	}
	return nil
}

// decision classifies a value with tolerance tol.
func (e *LogRatioEval) decision(v, tol float64) decision {
	switch d := v - e.Threshold; {
	case tol > 0 && math.Abs(d) <= tol:
		return boundary
	case v < e.Threshold:
		return rejected
	}
	return accepted
}

// LogRatioValue returns the reference value of -eb for a set with support
// supp, item supports items and total weight base. The tolerance is
// ratTau·(1 + |log2(s/n)| + Σ |log2(f_i/n)|): an implementation computes
// the logarithms of the quotients separately (with an absolute error of a
// few ulps of each) and adds them. It returns ErrInvalidTable unless
// 0 < supp <= f_i <= base for every item.
func LogRatioValue(supp tract.Supp, items []tract.Supp, base tract.Supp) (Value, error) {
	if len(items) <= 1 {
		return conv(0, ConvUndefined), nil
	}
	if supp <= 0 || base < 1 {
		return Value{}, fmt.Errorf("%w: set support %d, base %d", ErrInvalidTable, supp, base)
	}
	n := big.NewInt(int64(base))
	num := big.NewInt(int64(supp))
	den := big.NewInt(1)
	scale := math.Abs(math.Log2(float64(supp) / float64(base)))
	for _, f := range items {
		if f < supp || f > base {
			return Value{}, fmt.Errorf("%w: item support %d, set support %d, base %d", ErrInvalidTable, f, supp, base)
		}
		den.Mul(den, big.NewInt(int64(f)))
		scale += math.Abs(math.Log2(float64(f) / float64(base)))
	}
	for range len(items) - 1 {
		num.Mul(num, n)
	}
	l := lnRat(frac(num, den), refPrec)
	v := float64Of(l.Quo(l, ln2Big(refPrec)))
	return Value{V: v, Tol: ratTau * (1 + scale)}, nil
}

// logRatio returns the -eb reference value of the set with mask m.
func (db *DB) logRatio(m uint64) Value {
	items := make([]tract.Supp, 0, bits.OnesCount64(m))
	for r := m; r != 0; r &= r - 1 {
		items = append(items, db.supp[r&-r])
	}
	v, err := LogRatioValue(db.supp[m], items, db.total)
	if err != nil {
		panic(err) // unreachable: Mine evaluates frequent sets only (supp >= 1)
	}
	return v
}
