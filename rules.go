package fpgrowth

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math"
	"slices"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Appearance tells where an item may appear in an association rule
// (upstream's appearance indicators, -R for rules).
type Appearance int

// Item appearances.
const (
	// AppearBoth allows the item in rule bodies and heads (default).
	AppearBoth Appearance = iota
	// AppearBody allows the item in rule bodies only.
	AppearBody
	// AppearHead allows the item in rule heads only.
	AppearHead
	// AppearNone ignores the item: it is removed from every transaction.
	AppearNone
)

// String returns the name of the constant.
func (a Appearance) String() string {
	switch a {
	case AppearBoth:
		return "AppearBoth"
	case AppearBody:
		return "AppearBody"
	case AppearHead:
		return "AppearHead"
	case AppearNone:
		return "AppearNone"
	}
	return fmt.Sprintf("Appearance(%d)", int(a))
}

func (a Appearance) isValid() bool { return a >= AppearBoth && a <= AppearNone }

func (a Appearance) internal() tract.Appearance {
	switch a {
	case AppearBody:
		return tract.AppearBody
	case AppearHead:
		return tract.AppearHead
	case AppearNone:
		return tract.AppearNone
	}
	return tract.AppearBoth
}

// RuleOptions configures association rule mining. Rules have a single
// item as head. The zero value is invalid only because MinSupport is
// required.
type RuleOptions[T comparable] struct {
	// MinSupport is the minimum support of a rule: the support of its body
	// (upstream default), or of body and head together with
	// OriginalSupport. Required.
	MinSupport Support
	// MaxSupport is the maximum support of a rule, applied to the same
	// support as MinSupport; the zero value means no limit. It must not
	// be below MinSupport if both are of the same kind (see
	// Options.MaxSupport).
	MaxSupport Support
	// MinConfidence is the minimum confidence in [0, 1]; 0 means no
	// confidence filter. With c = BodySupport·MinConfidence (exact, with
	// the float64 value of MinConfidence), a rule passes iff
	// Support >= c·(1-2^-52) and Support > c-1: the minimum of Fraction
	// with the body support in place of the total weight. The tolerance
	// is below one unit of support, so MinConfidence 1 admits only rules
	// with Support == BodySupport, for every weight.
	MinConfidence float64
	// MinSize and MaxSize limit the number of items of a rule, body and
	// head together; 0 means 1 respectively no limit. Rules of size 1 have
	// an empty body.
	MinSize, MaxSize int
	// OriginalSupport makes MinSupport and MaxSupport apply to the support
	// of body and head together (upstream -o).
	OriginalSupport bool
	// Appearance restricts where items may appear; items not in the map
	// get DefaultAppearance. Entries for items that do not occur have no
	// effect.
	Appearance map[T]Appearance
	// DefaultAppearance is the appearance of items not in Appearance.
	// With AppearNone, only the items of Appearance are mined.
	DefaultAppearance Appearance
	// Evaluation, if not nil, computes an evaluation measure for every
	// reported rule and puts it into Rule.Value; with Evaluation.Filter it
	// also filters the rules by Evaluation.Threshold. Without Filter (the
	// zero value) no rule is removed. nil leaves Value NaN.
	// LogSupportRatio, aggregation and pruning are not available for
	// rules.
	Evaluation *Evaluation
	// Algorithm selects the FP-growth variant.
	Algorithm Algorithm
	// Tuning holds performance options (the k-items machine is not used
	// for rules).
	Tuning Tuning
	// OnStart, if not nil, is called once before the first rule is
	// reported (also if none is), with the Stats of the database, unless
	// ctx is canceled by then. Its error stops mining and is returned
	// unchanged, also if ctx is canceled as well; if it returns nil
	// after ctx was canceled (for example by OnStart itself), mining
	// returns ctx.Err().
	OnStart func(Stats) error
}

// Rule is a reported association rule Body -> Head.
type Rule[T comparable] struct {
	// Body is the antecedent in report order (possibly empty). The slice
	// is owned by the library and only valid during the callback or
	// iteration step; use Clone to retain it.
	Body []T
	// Head is the consequent.
	Head T
	// Support is the support of body and head together.
	Support int64
	// BodySupport is the support of the body (the total weight for an
	// empty body).
	BodySupport int64
	// HeadSupport is the support of the head alone.
	HeadSupport int64
	// Confidence is Support / BodySupport.
	Confidence float64
	// Lift is Confidence divided by the relative head support
	// HeadSupport / Summary.TotalWeight.
	Lift float64
	// Value is the evaluation value (RuleOptions.Evaluation), NaN without
	// an evaluation (as for Itemset.Value, so reflect.DeepEqual and ==
	// report two such rules as different; see there).
	Value float64
}

// Clone returns a copy of r whose Body slice is not shared.
func (r Rule[T]) Clone() Rule[T] {
	r.Body = slices.Clone(r.Body)
	return r
}

// plan validates o and translates it to the miner configuration.
func (o *RuleOptions[T]) plan() (*plan, error) {
	if o.MinSupport.IsZero() {
		return nil, invalid("MinSupport", "required")
	}
	if err := o.MinSupport.validate("MinSupport"); err != nil {
		return nil, err
	}
	if err := o.MaxSupport.validate("MaxSupport"); err != nil {
		return nil, err
	}
	if err := checkRange(o.MinSupport, o.MaxSupport); err != nil {
		return nil, err
	}
	if !(o.MinConfidence >= 0 && o.MinConfidence <= 1) {
		return nil, invalid("MinConfidence", "%g outside [0, 1]", o.MinConfidence)
	}
	if o.Algorithm < AlgorithmAuto || o.Algorithm > AlgorithmTopDown {
		return nil, invalid("Algorithm", "invalid value %d", int(o.Algorithm))
	}
	if err := o.Tuning.validate(); err != nil {
		return nil, err
	}
	zmin, zmax, err := sizes(o.MinSize, o.MaxSize, false)
	if err != nil {
		return nil, err
	}
	if !o.DefaultAppearance.isValid() {
		return nil, invalid("DefaultAppearance", "invalid value %d", int(o.DefaultAppearance))
	}
	for it, a := range o.Appearance {
		if !a.isValid() {
			return nil, invalid("Appearance", "item %v: invalid value %d", it, int(a))
		}
	}
	if o.Evaluation != nil {
		if err := o.Evaluation.validate(true); err != nil {
			return nil, err
		}
	}
	cfg := fpg.DefaultConfig()
	cfg.Target = report.TargetRules
	cfg.ZMin, cfg.ZMax = zmin, zmax
	cfg.ConfFrac = o.MinConfidence
	cfg.Algo = o.Algorithm.internal()
	o.Tuning.apply(&cfg)
	if o.OriginalSupport {
		cfg.Mode |= fpg.ModeOrigSupp
	}
	if o.Evaluation != nil {
		o.Evaluation.apply(&cfg)
	}
	return &plan{
		cfg:        cfg,
		minSupport: o.MinSupport,
		maxSupport: o.MaxSupport,
		hasEval:    o.Evaluation != nil,
		onStart:    o.OnStart,
	}, nil
}

// MineRules mines the association rules of src described by opt and
// calls fn for each of them, in upstream's order. It behaves like
// MineItemsets: synchronous, two passes over src, fn's and OnStart's
// errors returned unchanged, ctx.Err() on cancellation, a nil fn only
// counts. Cancellation is checked at the same points as in MineItemsets
// while reading, completing the bag and building the FP-tree (OnStart is
// not called once ctx is canceled), then at every recursion step that
// fills the item set tree, every 256 of its nodes or counters while it is
// filtered and reported, and every 4096 tables of a Fisher test, but not
// before every rule: fn may still be called for the rules of up to 256
// item sets after ctx is canceled, and a canceled run always returns
// ctx.Err(), also if OnStart canceled it and nothing is left to mine.
// The precedence of errors is that of MineItemsets: fn's or OnStart's
// error is returned unchanged even if ctx is canceled as well.
func MineRules[T comparable](ctx context.Context, src Source[T], opt RuleOptions[T], fn func(Rule[T]) error) (Summary, error) {
	p, err := opt.plan()
	if err != nil {
		return Summary{}, err
	}
	if isNilSource(src) {
		return Summary{}, invalid("src", "nil source")
	}
	ib := tract.NewItemBase[T]()
	ib.SetDefaultAppearance(opt.DefaultAppearance.internal())
	var hook func([]T) error
	if len(opt.Appearance) > 0 {
		// Items with an explicit appearance are registered when they first
		// occur, so the item order (and hence the output order) does not
		// depend on the map's iteration order.
		pending := make(map[T]tract.Appearance, len(opt.Appearance))
		for it, a := range opt.Appearance {
			pending[it] = a.internal()
		}
		hook = func(items []T) error {
			if len(pending) == 0 {
				return nil
			}
			for _, it := range items {
				if a, ok := pending[it]; ok {
					delete(pending, it)
					if err := ib.AddAppearance(it, a); err != nil {
						return mapInternal(err)
					}
				}
			}
			return nil
		}
	}
	var sink resultSink[T] // nil: count only
	if fn != nil {
		sink = &ruleSink[T]{fn: fn, hasEval: p.hasEval}
	}
	return mine(ctx, src, p, ib, hook, sink)
}

// Rules returns an iterator over the rules that MineRules would report.
// Breaking out of the loop stops mining; an error ends the iteration with
// a final (zero Rule, err) pair. The Body slice is only valid during the
// iteration step.
func Rules[T comparable](ctx context.Context, src Source[T], opt RuleOptions[T]) iter.Seq2[Rule[T], error] {
	return func(yield func(Rule[T], error) bool) {
		stopped := false
		_, err := MineRules(ctx, src, opt, func(r Rule[T]) error {
			if !yield(r, nil) {
				stopped = true
				return errStop
			}
			return nil
		})
		if err != nil && !stopped {
			yield(Rule[T]{}, err)
		}
	}
}

// CollectRules returns all rules that MineRules would report, each with
// its own Body slice.
func CollectRules[T comparable](ctx context.Context, src Source[T], opt RuleOptions[T]) ([]Rule[T], Summary, error) {
	var rules []Rule[T]
	sum, err := MineRules(ctx, src, opt, func(r Rule[T]) error {
		rules = append(rules, r.Clone())
		return nil
	})
	if err != nil {
		return nil, sum, err
	}
	return rules, sum, nil
}

// ruleSink converts the rules of the reporter to Rule values, reusing
// its body buffer.
type ruleSink[T comparable] struct {
	names   []T
	total   float64
	buf     []T
	fn      func(Rule[T]) error
	hasEval bool
	err     error // the error of fn that stopped mining, if any
}

var errUnexpectedSet = errors.New("fpgrowth: internal error: item set reported while mining rules")

func (s *ruleSink[T]) ItemSet(report.ItemSet) error { return errUnexpectedSet }

func (s *ruleSink[T]) Rule(r report.Rule) error {
	buf := s.buf[:0]
	for _, c := range r.Body {
		buf = append(buf, s.names[c])
	}
	s.buf = buf
	v := math.NaN()
	if s.hasEval {
		v = r.Eval
	}
	supp, body, head := float64(r.Supp), float64(r.BodySupp), float64(r.HeadSupp)
	var conf, lift float64 // upstream's %c and %l (0 for empty supports)
	if r.BodySupp > 0 {
		conf = supp / body
		if r.HeadSupp > 0 {
			lift = (supp * s.total) / (body * head)
		}
	}
	s.err = s.fn(Rule[T]{
		Body:        buf[:len(buf):len(buf)],
		Head:        s.names[r.Head],
		Support:     int64(r.Supp),
		BodySupport: int64(r.BodySupp),
		HeadSupport: int64(r.HeadSupp),
		Confidence:  conf,
		Lift:        lift,
		Value:       v,
	})
	return s.err
}

func (s *ruleSink[T]) setup(names []T, total int64) {
	s.names = names
	s.total = max(float64(total), 1)
}

func (s *ruleSink[T]) userErr() error { return s.err }
