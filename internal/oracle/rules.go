package oracle

import (
	"fmt"
	"math/bits"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Appearance restricts where an item may occur in a rule (upstream item
// appearance file, -R with -tr).
type Appearance uint8

// Item appearances. The zero value is AppearBoth, the unrestricted
// default without an appearance file, deliberately not an "unknown"
// sentinel.
const (
	AppearBoth Appearance = iota // body or head (upstream APP_BOTH)
	AppearBody                   // body only (APP_BODY)
	AppearHead                   // head only (APP_HEAD)
	AppearNone                   // ignored: never part of a rule (APP_NONE)
)

// canHead reports whether an item with appearance a may be a rule head.
func (a Appearance) canHead() bool { return a == AppearBoth || a == AppearHead }

// RuleParams configures Rules. Start from NewRuleParams for upstream's
// defaults.
type RuleParams struct {
	// MinSupp is the absolute minimum support (from -s, see AbsMinSupp) of
	// the selected rule support definition: the body support by default,
	// the support of body ∪ head with OrigSupp. Values below 1 act as 1.
	MinSupp tract.Supp
	// MaxSupp is the absolute maximum support (-S), applied to the same
	// support as MinSupp. NoMaxSupp for no limit.
	//
	// Deviation from upstream (bug B8, rules-eval.md §3.1): upstream
	// compares -S with the support of body ∪ head even under the default
	// body support definition.
	MaxSupp tract.Supp
	// OrigSupp selects the original rule support definition (-o):
	// MinSupp and MaxSupp apply to the support of body ∪ head.
	OrigSupp bool
	// MinConf is the minimum confidence as a fraction (-c/100). A rule is
	// rejected if float64(supp) < float64(body)*(MinConf*(1-DBL_EPSILON)),
	// i.e. it is accepted with >= and a tolerance of one relative epsilon.
	MinConf float64
	// MinSize and MaxSize bound the number of items of a rule, counting
	// body and head (-m, -n). NoMaxSize means no limit.
	MinSize, MaxSize int
	// Appear maps items to their appearance; items not listed get
	// DefaultAppear. A nil map with the zero DefaultAppear means no
	// restrictions.
	Appear        map[string]Appearance
	DefaultAppear Appearance
	// Eval, if non-nil, is an additional measure; rules whose value v
	// satisfies dir*v < dir*threshold are rejected (so NaN passes, like
	// upstream).
	Eval *Measure
}

// NewRuleParams returns upstream's defaults with the absolute minimum
// support minSupp and minimum confidence minConf (a fraction): rule sizes
// from 1 without upper limit, no maximum support, the default (body)
// support definition, no appearance restrictions, no measure.
func NewRuleParams(minSupp tract.Supp, minConf float64) RuleParams {
	return RuleParams{
		MinSupp: minSupp,
		MaxSupp: NoMaxSupp,
		MinConf: minConf,
		MinSize: 1,
		MaxSize: NoMaxSize,
	}
}

// validate checks p.
func (p *RuleParams) validate() error {
	if p.MinSize < 0 || p.MaxSize < 0 {
		return fmt.Errorf("oracle: invalid size range [%d, %d]", p.MinSize, p.MaxSize)
	}
	if !(p.MinConf >= 0 && p.MinConf <= 1) {
		return fmt.Errorf("oracle: invalid minimum confidence %v", p.MinConf)
	}
	for item, a := range p.Appear {
		if a > AppearNone {
			return fmt.Errorf("oracle: invalid appearance %d for item %q", a, item)
		}
	}
	if p.DefaultAppear > AppearNone {
		return fmt.Errorf("oracle: invalid default appearance %d", p.DefaultAppear)
	}
	if p.Eval != nil {
		return p.Eval.validate()
	}
	return nil
}

// Rule is an association rule Body -> Head.
type Rule struct {
	Head string
	Body ItemSet
	// Supp is the support of Body ∪ {Head}.
	Supp tract.Supp
	// BodySupp is the support of Body (the total weight for an empty body).
	BodySupp tract.Supp
	// HeadSupp is the support of {Head}.
	HeadSupp tract.Supp
	// Conf is Supp/BodySupp (0 if BodySupp <= 0), as upstream's %c.
	Conf float64
	// Lift is (Supp*total)/(BodySupp*HeadSupp) (0 if BodySupp or HeadSupp
	// is <= 0; total taken as 1 if <= 0), as upstream's %l.
	Lift float64
	// Eval is the measure value, 0 without a measure.
	Eval float64
}

// Items returns the canonical set Body ∪ {Head}.
func (r Rule) Items() ItemSet {
	items := make(ItemSet, 0, len(r.Body)+1)
	items = append(items, r.Body...)
	items = append(items, r.Head)
	return NewItemSet(items...)
}

// String formats the rule as "h" <- {"a" "b"} supp=... body=... head=...
func (r Rule) String() string {
	s := fmt.Sprintf("%q <- %v supp=%d body=%d head=%d", r.Head, r.Body, r.Supp, r.BodySupp, r.HeadSupp)
	if r.Eval != 0 {
		s += fmt.Sprintf(" eval=%v", r.Eval)
	}
	return s
}

// Rules returns the association rules selected by p, in canonical order
// (SortRules). A rule B -> h is formed for every item set S = B ∪ {h} with
// a single head item h ∈ S and the body B = S\{h} (B may be empty). Let
// s = supp(S), b = supp(B) (the total weight if B is empty), smin =
// max(MinSupp, 1) and conf = MinConf. The rule is reported if and only if
//
//  1. s >= 1;
//  2. support: by default b >= smin, b <= MaxSupp and
//     s >= max(RuleSetMinSupp(MinSupp, conf), 1) (implied by the
//     confidence test, applied for fidelity); with OrigSupp s >= smin and
//     s <= MaxSupp;
//  3. not float64(s) < float64(b)*(conf*(1-DBL_EPSILON));
//  4. MinSize <= |S| <= MaxSize;
//  5. appearances: no item of S is AppearNone, h is AppearHead or
//     AppearBoth, and no item of B is AppearHead (so a set with two
//     head-only items yields no rule). Deviation from upstream (bug B3,
//     rules-eval.md §3.4): upstream lets a head-only item that is not the
//     item with the smallest code end up in a body;
//  6. Eval is nil or not dir*v < dir*threshold for the rule's value v
//     (with the -i invalidation if requested).
func (db *DB) Rules(p RuleParams) ([]Rule, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	smin := EffectiveMinSupp(p.MinSupp)
	setMin := smin
	if !p.OrigSupp {
		setMin = EffectiveMinSupp(RuleSetMinSupp(p.MinSupp, p.MinConf))
	}
	confAdj := p.MinConf * (1 - dblEpsilon)

	// Appearance masks over the universe.
	var noneMask, headOnly, bodyOnly uint64
	for i, item := range db.items {
		a, ok := p.Appear[item]
		if !ok {
			a = p.DefaultAppear
		}
		switch a {
		case AppearNone:
			noneMask |= 1 << i
		case AppearHead:
			headOnly |= 1 << i
		case AppearBody:
			bodyOnly |= 1 << i
		}
	}

	rules := []Rule{}
	for m := uint64(1); m < uint64(len(db.supp)); m++ {
		s := db.supp[m]
		if s < setMin || m&noneMask != 0 {
			continue
		}
		size := bits.OnesCount64(m)
		if size < p.MinSize || size > p.MaxSize {
			continue
		}
		heads := m &^ bodyOnly
		if hd := m & headOnly; hd != 0 {
			heads &= hd // the only possible head is the head-only item
			if bits.OnesCount64(hd) > 1 {
				continue
			}
		}
		for r := heads; r != 0; r &= r - 1 {
			bit := r & -r
			b := db.supp[m&^bit]
			if p.OrigSupp {
				if s > p.MaxSupp { // s >= smin holds: setMin == smin
					continue
				}
			} else if b < smin || b > p.MaxSupp {
				continue
			}
			if float64(s) < float64(b)*confAdj {
				continue
			}
			h := db.supp[bit]
			var v float64
			if p.Eval != nil {
				v = p.Eval.ruleValue(s, b, h, db.total)
				if p.Eval.rejectsRule(v) {
					continue
				}
			}
			rules = append(rules, Rule{
				Head:     db.items[bits.TrailingZeros64(bit)],
				Body:     db.set(m &^ bit),
				Supp:     s,
				BodySupp: b,
				HeadSupp: h,
				Conf:     confidence(s, b),
				Lift:     lift(s, b, h, db.total),
				Eval:     v,
			})
		}
	}
	SortRules(rules)
	return rules, nil
}

// confidence is upstream's %c: supp/body, 0 if body <= 0.
func confidence(supp, body tract.Supp) float64 {
	if body <= 0 {
		return 0
	}
	return float64(supp) / float64(body)
}

// lift is upstream's %l (isr_rinfo): (supp*smax)/(body*head) with
// smax = total weight (1 if <= 0); 0 if body or head is <= 0.
func lift(supp, body, head, total tract.Supp) float64 {
	if body <= 0 || head <= 0 {
		return 0
	}
	smax := float64(total)
	if smax <= 0 {
		smax = 1
	}
	return (float64(supp) * smax) / (float64(body) * float64(head))
}
