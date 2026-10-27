// Package ruleval implements the rule evaluation measures of FP-growth: a
// port of Christian Borgelt's math/src/ruleval.c (FP-Growth 6.21) together
// with the special functions of gamma.c and chi2.c that the measures need.
//
// A measure evaluates an association rule B -> h from four supports, in
// upstream's argument order:
//
//	supp  support of B ∪ {h} (the rule support)
//	body  support of B (the total transaction weight base for an empty body)
//	head  support of {h}
//	base  total transaction weight
//
// The arguments are valid when they describe a 2x2 contingency table:
// base > 0, 0 <= supp <= min(body, head), max(body, head) <= base and
// body + head - supp <= base. For valid arguments no measure returns NaN.
// Other arguments give unspecified (but non-panicking) results.
//
// # Upstream mapping
//
//	ruleval.c  RE_NONE ... RE_FETSUPP      None ... FisherSupport (same values)
//	           RE_FNCNT                    NumMeasures
//	           RULEVALFN                   Func
//	           re_none ... re_fetsupp      reNone ... reFetSupp (via Eval, Measure.Func)
//	           reinfo[] (fn, dir, chr, desc) measures; Measure.Func, Dir, Letter, Description
//	           remap[], re_getid           ByLetter
//	           re_function                 Measure.Func
//	           re_dir                      Measure.Dir
//	           re_help                     WriteHelp
//	gamma.c    init, facts/logfs/loghs     newLogGammaTables, lgTables
//	           logGamma                    logGamma
//	           series, cfrac, GammaQ       series, cfrac, gammaQ
//	chi2.c     chi2cdfQ                    chi2cdfQ
//
// The upstream evaluation order is kept operation by operation where
// upstream's arithmetic is exact. Products that feed an addition or
// subtraction are wrapped in float64(...) so that the Go compiler does not
// fuse them into FMA instructions (also in the new code); with that, every
// measure that uses no log/exp is bit-identical to upstream compiled with
// -ffp-contract=off, and the others differ from upstream only by the
// last-place differences between Go's math package and the platform libm
// (see testdata and reference/harness/ruleval), with the exceptions of
// ALG-1 below.
//
// # Numeric accuracy (audit ALG-1 and ALG-6, docs/differences.md N1)
//
// Upstream's statistical measures cancel in double arithmetic: chi² and
// Yates form supp·base - head·body from rounded products, the information
// subtracts terms of size ln n, the certainty factor divides by 1 - p and
// the Fisher tests add log-gamma values of size n·ln n. Here the
// cross-product difference is exact (exact.go), the information is a sum
// of non-negative terms (infoNats), the certainty factor uses an exact
// form near p = 1, and the Fisher tests for base > maxFact use Stirling's
// formula with an exact deviation and visit only the contributing tables
// (fisherlarge.go). The bounds hold for every valid table with supports
// below 2^63: Chi2 and Yates relative 8 ulps; Info relative 32 ulps;
// Certainty absolute 2^-40; the p-values of chi², Yates and G relative
// 2^-36·(1+x) for the statistic x; the Fisher tests relative 2^-36 (for
// base <= maxFact, FisherChi2 and FisherSupport keep upstream's
// complements, absolute 2^-36). Chi2, Yates, their p-values, the -i test
// (AboveExpected) and Certainty (outside its exact form) are
// bit-identical to upstream whenever both products are at most 2^53; the
// Fisher tests are unchanged for base <= maxFact; Info, InfoPValue and the
// Fisher tests for base > maxFact differ from upstream in the last bits.
//
// # Deviations from upstream
//
// Confirmed upstream bugs (docs/upstream/rules-eval.md, section 7) are
// fixed; each fix is marked "Deviation from upstream" in the code:
//
//   - B4: Conviction is +Inf (not 0) for confidence 1.
//   - B5: Certainty is 0 (not NaN) when the head occurs in every transaction.
//   - B6: FisherProb and FisherInfo decide ties exactly, so tables with the
//     same probability (information) as the observed one are always summed.
//   - B7: Info is clamped at 0 (it could be slightly negative).
//   - B9: The Fisher p-values are clamped to [0, 1].
//   - B10: Yates' correction is clamped at 0 (Yates, YatesPValue).
//   - B11: logGamma(170.5) is ln(Gamma(170.5)), not 0.
//   - ALG-1, ALG-6: see "Numeric accuracy" above.
package ruleval

import (
	"fmt"
	"io"
	"strconv"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Func is a rule evaluation function (upstream RULEVALFN). The arguments
// are the rule support, body support, head support and total weight.
type Func func(supp, body, head, base tract.Supp) float64

// AbortFunc is a rule evaluation function with a cancellation check:
// abort, if not nil, is called every few thousand steps of a long
// computation, and its non-nil result stops the evaluation and is
// returned (with a NaN value). With a nil abort, or when abort never
// fails, the value is bit-identical to the measure's Func.
type AbortFunc func(supp, body, head, base tract.Supp, abort func() error) (float64, error)

// abortable holds the AbortFunc of the measures whose cost is not
// constant: the Fisher tests sum over min(body, head)+1 contingency
// tables (after normalization) for base <= maxFact, and over about 9
// standard deviations of the hypergeometric distribution on each side of
// its mode beyond (fisherlarge.go); with balanced margins that is about
// 1.4 s at a total weight of 1.6·10^15.
var abortable = [NumMeasures]AbortFunc{
	FisherProb:    fetProb,
	FisherChi2:    fetChi2,
	FisherInfo:    fetInfo,
	FisherSupport: fetSupp,
}

// Measure identifies a rule evaluation measure. The values are upstream's
// RE_* identifiers, so they can be stored and compared like upstream's ids.
type Measure uint8

// Rule evaluation measures (upstream ruleval.h). The comments give the
// upstream identifier and the letter of the -e option.
const (
	None               Measure = iota // RE_NONE, x: no measure (constant 0)
	Support                           // RE_SUPP, o: rule support (absolute)
	Confidence                        // RE_CONF, c: rule confidence
	ConfidenceDiff                    // RE_CONFDIFF, d: |confidence - prior|
	Lift                              // RE_LIFT, l: lift value
	LiftDiff                          // RE_LIFTDIFF, a: |lift - 1|
	LiftQuotient                      // RE_LIFTQUOT, q: 1 - min(lift, 1/lift)
	Conviction                        // RE_CVCT, v: conviction
	ConvictionDiff                    // RE_CVCTDIFF, e: |conviction - 1|
	ConvictionQuotient                // RE_CVCTQUOT, r: 1 - min(conv., 1/conv.)
	Imbalance                         // RE_IMBAL, b: imbalance ratio
	Kulczynski                        // RE_KULC, k: Kulczynski measure
	KulczynskiDiff                    // RE_KULCDIFF, w: 2·|Kulczynski - 1/2|
	CondProbRatio                     // RE_CPROB, u: conditional probability ratio
	Importance                        // RE_IMPORT, j: log2 of CondProbRatio
	Certainty                         // RE_CERT, z: certainty factor
	Chi2                              // RE_CHI2, n: normalized chi^2 measure
	Chi2PValue                        // RE_CHI2PVAL, p: p-value of chi^2
	Yates                             // RE_YATES, y: normalized chi^2 with Yates' correction
	YatesPValue                       // RE_YATESPVAL, t: p-value of Yates-corrected chi^2
	Info                              // RE_INFO, i: information difference to prior (bits)
	InfoPValue                        // RE_INFOPVAL, g: p-value of the G statistic
	FisherProb                        // RE_FETPROB, f: Fisher's exact test (table probability)
	FisherChi2                        // RE_FETCHI2, h: Fisher's exact test (chi^2 measure)
	FisherInfo                        // RE_FETINFO, m: Fisher's exact test (information gain)
	FisherSupport                     // RE_FETSUPP, s: Fisher's exact test (support)

	// NumMeasures is the number of measures (upstream RE_FNCNT). The
	// fpgrowth program uses this value for its own item set measure 'b'
	// (FPG_LDRATIO), which is not a rule measure.
	NumMeasures
)

// measureInfo is one entry of upstream's reinfo[] table.
type measureInfo struct {
	fn     Func   // evaluation function
	dir    int    // +1: larger is better, -1: smaller is better, 0: none
	letter byte   // letter of the -e option
	name   string // Go constant name (for String)
	desc   string // upstream description (re_help)
}

// measures ports reinfo[] of ruleval.c. The entries are positional on
// purpose: one line per measure, in the order of reinfo[], keeps the table
// comparable with upstream (measureInfo is private and fully covered by
// tests).
var measures = [NumMeasures]measureInfo{
	None:               {reNone, 0, 'x', "None", "no measure"},
	Support:            {reSupp, +1, 'o', "Support", "rule support (original def.: body & head)"},
	Confidence:         {reConf, +1, 'c', "Confidence", "rule confidence"},
	ConfidenceDiff:     {reConfDiff, +1, 'd', "ConfidenceDiff", "absolute confidence difference to prior"},
	Lift:               {reLift, +1, 'l', "Lift", "lift value (confidence divided by prior)"},
	LiftDiff:           {reLiftDiff, +1, 'a', "LiftDiff", "absolute difference of lift value to 1"},
	LiftQuotient:       {reLiftQuot, +1, 'q', "LiftQuotient", "difference of lift quotient to 1"},
	Conviction:         {reCvct, +1, 'v', "Conviction", "conviction (inverse lift for negated head)"},
	ConvictionDiff:     {reCvctDiff, +1, 'e', "ConvictionDiff", "absolute difference of conviction to 1"},
	ConvictionQuotient: {reCvctQuot, +1, 'r', "ConvictionQuotient", "difference of conviction quotient to 1"},
	Imbalance:          {reImbal, +1, 'b', "Imbalance", "imbalance ratio"},
	Kulczynski:         {reKulc, +1, 'k', "Kulczynski", "Kulczynski measure"},
	KulczynskiDiff:     {reKulcDiff, +1, 'w', "KulczynskiDiff", "difference of Kulczynski measure to 0.5"},
	CondProbRatio:      {reCProb, +1, 'u', "CondProbRatio", "conditional probability ratio"},
	Importance:         {reImport, +1, 'j', "Importance", "importance (binary log. of cond. prob. ratio)"},
	Certainty:          {reCert, +1, 'z', "Certainty", "certainty factor (relative confidence change)"},
	Chi2:               {reChi2, +1, 'n', "Chi2", "normalized chi^2 measure"},
	Chi2PValue:         {reChi2PVal, -1, 'p', "Chi2PValue", "p-value from (unnormalized) chi^2 measure"},
	Yates:              {reYates, +1, 'y', "Yates", "normalized chi^2 measure with Yates' correction"},
	YatesPValue:        {reYatesPVal, -1, 't', "YatesPValue", "p-value from Yates-corrected chi^2 measure"},
	Info:               {reInfo, +1, 'i', "Info", "information difference to prior"},
	InfoPValue:         {reInfoPVal, -1, 'g', "InfoPValue", "p-value from G statistic/information difference"},
	FisherProb:         {reFetProb, -1, 'f', "FisherProb", "Fisher's exact test (table probability)"},
	FisherChi2:         {reFetChi2, -1, 'h', "FisherChi2", "Fisher's exact test (chi^2 measure)"},
	FisherInfo:         {reFetInfo, -1, 'm', "FisherInfo", "Fisher's exact test (information gain)"},
	FisherSupport:      {reFetSupp, -1, 's', "FisherSupport", "Fisher's exact test (support)"},
}

// remap ports remap[] of ruleval.c: the measure for each letter 'a'..'z'.
var remap = [26]Measure{
	LiftDiff,           // a
	Imbalance,          // b
	Confidence,         // c
	ConfidenceDiff,     // d
	ConvictionDiff,     // e
	FisherProb,         // f
	InfoPValue,         // g
	FisherChi2,         // h
	Info,               // i
	Importance,         // j
	Kulczynski,         // k
	Lift,               // l
	FisherInfo,         // m
	Chi2,               // n
	Support,            // o
	Chi2PValue,         // p
	LiftQuotient,       // q
	ConvictionQuotient, // r
	FisherSupport,      // s
	YatesPValue,        // t
	CondProbRatio,      // u
	Conviction,         // v
	KulczynskiDiff,     // w
	None,               // x
	Yates,              // y
	Certainty,          // z
}

// ByLetter returns the measure selected by letter c of the -e option
// (upstream re_getid). Every letter 'a'..'z' selects a measure; ok is false
// for any other byte. Note that 'b' selects Imbalance here, as in
// re_getid; the fpgrowth program intercepts 'b' for its item set measure
// before calling re_getid, which makes Imbalance unreachable from the
// upstream command line (bug B1).
func ByLetter(c byte) (m Measure, ok bool) {
	if c < 'a' || c > 'z' {
		return None, false
	}
	return remap[c-'a'], true
}

// IsValid reports whether m is one of the measures None..FisherSupport.
func (m Measure) IsValid() bool { return m < NumMeasures }

// Func returns the evaluation function of m (upstream re_function). It
// panics if m is not valid. Callers on hot paths should fetch the function
// once, like upstream's item set tree does.
func (m Measure) Func() Func { return m.info().fn }

// Dir returns the evaluation direction of m (upstream re_dir): +1 if the
// value must meet or exceed the threshold, -1 if it must not exceed it,
// and 0 for None. It panics if m is not valid.
func (m Measure) Dir() int { return m.info().dir }

// Letter returns the letter that selects m with the -e option. It panics
// if m is not valid.
func (m Measure) Letter() byte { return m.info().letter }

// Description returns upstream's description of m as printed by re_help.
// It panics if m is not valid.
func (m Measure) Description() string { return m.info().desc }

// String returns the Go name of m, or "Measure(n)" if m is not valid.
func (m Measure) String() string {
	if !m.IsValid() {
		return "Measure(" + strconv.Itoa(int(m)) + ")"
	}
	return measures[m].name
}

func (m Measure) info() *measureInfo {
	if !m.IsValid() {
		panic("ruleval: invalid measure " + strconv.Itoa(int(m)))
	}
	return &measures[m]
}

// AbortFunc returns the abortable evaluation function of m, or nil if m
// takes constant time per evaluation (every measure except the Fisher
// tests, see abortable). It panics if m is not valid.
func (m Measure) AbortFunc() AbortFunc {
	_ = m.info() // validity check
	return abortable[m]
}

// Eval evaluates measure m for a rule with the given rule support, body
// support, head support and total weight. It panics if m is not valid. It
// does not allocate, except when FisherProb or FisherInfo must resolve a
// near-tie of two tables exactly (big integers, or 320-bit logarithms for
// FisherProb tables more than 1024 apart). That happens only for tables
// whose computed probabilities (information) agree within their rounding
// error bound, costs at most a few milliseconds per evaluation, and is
// skipped for FisherProb and FisherInfo when their p-value certainly
// underflows to 0.
func Eval(m Measure, supp, body, head, base tract.Supp) float64 {
	return m.info().fn(supp, body, head, base)
}

// WriteHelp writes the list of measures exactly like upstream's re_help:
// one line per measure with its letter, its description padded to 48
// characters and "(+)" or "(-)" for its direction.
func WriteHelp(w io.Writer) error {
	for m := range NumMeasures {
		info := &measures[m]
		line := fmt.Sprintf("  %c   %-48s", info.letter, info.desc)
		switch {
		case info.dir > 0:
			line += "(+)"
		case info.dir < 0:
			line += "(-)"
		}
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	return nil
}
