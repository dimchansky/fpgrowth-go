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

// Itemset is a reported item set.
type Itemset[T comparable] struct {
	// Items are the items of the set in report order (upstream's order,
	// not sorted). The slice is owned by the library and only valid
	// during the callback or iteration step; use Clone to retain it.
	Items []T
	// Support is the weighted support: the total weight of the
	// transactions that contain all items.
	Support int64
	// Value is the evaluation value (Options.Evaluation), NaN without an
	// evaluation.
	Value float64
}

// Clone returns a copy of s whose Items slice is not shared.
func (s Itemset[T]) Clone() Itemset[T] {
	s.Items = slices.Clone(s.Items)
	return s
}

// Summary describes a mining run.
type Summary struct {
	// Transactions is the number of transactions of one pass, including
	// empty ones and those with weight 0.
	Transactions int
	// TotalWeight is the total transaction weight, the support of the
	// empty set.
	TotalWeight int64
	// Items is the number of frequent items (after the item restriction
	// of Options.Items or the appearances of RuleOptions). If it is 0,
	// nothing was mined and nothing was reported.
	Items int
	// Reported is the number of reported item sets or rules. After an
	// early stop it includes the one at which mining stopped.
	Reported uint64
	// BySize[k] is the number of reported item sets (rules: body and
	// head) with k items; the slice ends at the largest size reported.
	BySize []uint64
	// Spectrum is the pattern spectrum if Options.PatternSpectrum was set:
	// the number of reported item sets per (size, support) pair, ordered
	// by size and support.
	Spectrum []SpectrumCell
}

// SpectrumCell is one entry of a pattern spectrum.
type SpectrumCell struct {
	Size    int    // number of items
	Support int64  // support
	Count   uint64 // number of reported item sets with this size and support
}

// errStop stops mining when an iterator's consumer breaks out of its
// loop.
var errStop = errors.New("fpgrowth: iteration stopped")

// MineItemsets mines the item sets of src described by opt and calls fn
// for each of them, in upstream's order. It runs synchronously on the
// calling goroutine and reads src twice.
//
// If fn returns an error, mining stops and MineItemsets returns that error
// unchanged. A nil fn only counts the item sets (Summary.Reported and
// BySize), which is faster. If ctx is canceled, mining stops and
// MineItemsets returns ctx.Err(). Errors of src are returned wrapped with
// the pass in which they occurred. A database without frequent items is
// not an error: nothing is reported and Summary.Items is 0.
//
// MineItemsets is safe for concurrent use if src supports concurrent
// passes. The Summary is valid also when an error is returned (as far as
// mining got).
func MineItemsets[T comparable](ctx context.Context, src Source[T], opt Options[T], fn func(Itemset[T]) error) (Summary, error) {
	p, err := opt.plan()
	if err != nil {
		return Summary{}, err
	}
	if src == nil {
		return Summary{}, invalid("src", "nil source")
	}
	ib := tract.NewItemBase[T]()
	if len(opt.Items) > 0 {
		if err := ib.Select(opt.Items...); err != nil {
			return Summary{}, mapInternal(err)
		}
	}
	var sink resultSink[T] // nil: count only
	if fn != nil {
		sink = &setSink[T]{fn: fn, hasEval: p.hasEval}
	}
	return mine(ctx, src, p, ib, nil, sink)
}

// Itemsets returns an iterator over the item sets that MineItemsets would
// report. Mining starts when the iteration starts and runs on the
// iterating goroutine; breaking out of the loop stops it. An error ends
// the iteration with a final (zero Itemset, err) pair. The Items slice is
// only valid during the iteration step.
func Itemsets[T comparable](ctx context.Context, src Source[T], opt Options[T]) iter.Seq2[Itemset[T], error] {
	return func(yield func(Itemset[T], error) bool) {
		stopped := false
		_, err := MineItemsets(ctx, src, opt, func(s Itemset[T]) error {
			if !yield(s, nil) {
				stopped = true
				return errStop
			}
			return nil
		})
		if err != nil && !stopped {
			yield(Itemset[T]{}, err)
		}
	}
}

// CollectItemsets returns all item sets that MineItemsets would report,
// each with its own Items slice. It holds the whole result in memory;
// prefer MineItemsets or Itemsets for large results.
func CollectItemsets[T comparable](ctx context.Context, src Source[T], opt Options[T]) ([]Itemset[T], Summary, error) {
	var sets []Itemset[T]
	sum, err := MineItemsets(ctx, src, opt, func(s Itemset[T]) error {
		sets = append(sets, s.Clone())
		return nil
	})
	if err != nil {
		return nil, sum, err
	}
	return sets, sum, nil
}

// setSink converts the item sets of the reporter to Itemset values. Its
// buffer is reused, so steady-state reporting does not allocate.
type setSink[T comparable] struct {
	names   []T // item code -> item
	buf     []T
	fn      func(Itemset[T]) error
	hasEval bool
	err     error // the last error of fn
}

var errUnexpectedRule = errors.New("fpgrowth: internal error: rule reported while mining item sets")

func (s *setSink[T]) ItemSet(is report.ItemSet) error {
	buf := s.buf[:0]
	for _, c := range is.Items {
		buf = append(buf, s.names[c])
	}
	s.buf = buf
	v := math.NaN()
	if s.hasEval {
		v = is.Eval
	}
	s.err = s.fn(Itemset[T]{Items: buf[:len(buf):len(buf)], Support: int64(is.Supp), Value: v})
	return s.err
}

func (s *setSink[T]) Rule(report.Rule) error { return errUnexpectedRule }

func (s *setSink[T]) setup(names []T, _ int64) { s.names = names }

func (s *setSink[T]) userErr() error { return s.err }

// resultSink is a reporter sink that converts results for a user
// callback.
type resultSink[T comparable] interface {
	report.Sink
	// setup passes the items in code order and the total weight.
	setup(names []T, total int64)
	// userErr returns the last error of the user callback.
	userErr() error
}

// mine runs the pipeline shared by item sets and rules: pass 1 (count),
// thresholds, pass 2 (bag), reporter set-up, mining. hook, if not nil, is
// called for every transaction of pass 1 before it is counted. A nil sink
// only counts.
func mine[T comparable](
	ctx context.Context,
	src Source[T],
	p *plan,
	ib *tract.ItemBase[T],
	hook func(items []T) error,
	sink resultSink[T],
) (Summary, error) {
	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}
	sc := &scanner[T]{ctx: ctx, src: src, hook: hook}
	if err := ib.Count(sc.scan); err != nil {
		return Summary{}, sc.failure(err)
	}
	sum := Summary{Transactions: ib.Records(), TotalWeight: int64(ib.TotalWeight())}
	if ib.Len() == 0 || ib.Records() == 0 {
		return sum, nil
	}
	border := p.setThresholds(sum.TotalWeight)
	m, err := fpg.New[T](p.cfg)
	if err != nil {
		return sum, mapInternal(err) // excluded by validation
	}
	if err := m.Data(ib, sc.scan); err != nil {
		if errors.Is(err, fpg.ErrNoItems) {
			return sum, nil
		}
		return sum, sc.failure(err)
	}
	sum.Items = ib.Len()
	rep, err := m.Reporter()
	if err != nil {
		return sum, mapInternal(err) // excluded by the call order
	}
	for i, b := range border {
		rep.SetBorder(p.cfg.ZMin+i, b)
	}
	if p.spectrum {
		rep.AddSpectrum()
	}
	if sink != nil {
		names := make([]T, ib.Len())
		for i := range names {
			names[i] = ib.ID(tract.Item(i))
		}
		sink.setup(names, sum.TotalWeight)
		rep.SetSink(sink)
	}
	err = m.Mine(ctx)
	sum.Reported = rep.Count()
	sum.BySize = rep.Stats()
	if p.spectrum {
		if psp := rep.Spectrum(); psp != nil {
			sum.Spectrum = make([]SpectrumCell, 0, psp.SigCount())
			for c := range psp.All() {
				sum.Spectrum = append(sum.Spectrum, SpectrumCell{Size: c.Size, Support: int64(c.Supp), Count: c.Count})
			}
		}
	}
	switch {
	case err == nil:
		return sum, nil
	case sink != nil && err == sink.userErr():
		return sum, err // the callback's error, unchanged
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		return sum, ctx.Err()
	}
	return sum, mapInternal(err)
}

// scanner adapts a Source to the internal scan function, counting the
// passes, checking weights, replay consistency and cancellation.
type scanner[T comparable] struct {
	ctx  context.Context
	src  Source[T]
	hook func(items []T) error
	pass int
	// transactions and total weight of pass 1
	n1  int
	w1  int64
	err error // the first error raised by the scanner itself
}

// checkEvery is the number of transactions between cancellation checks.
const checkEvery = 1024

func (s *scanner[T]) fail(err error) error {
	if s.err == nil {
		s.err = err
	}
	return err
}

// failure returns the error to report for err, an error of the item base
// or the miner after a pass: the scanner's own error if it raised one,
// else the internal error translated.
func (s *scanner[T]) failure(err error) error {
	if s.err != nil {
		return s.err
	}
	return mapInternal(err)
}

func (s *scanner[T]) scan(yield func(items []T, w tract.Supp) bool) error {
	s.pass++
	pass := s.pass
	if err := s.ctx.Err(); err != nil {
		return s.fail(err)
	}
	done := s.ctx.Done()
	n := 0
	var w int64
	for tx, err := range s.src.Transactions() {
		if err != nil {
			return s.fail(fmt.Errorf("fpgrowth: reading transactions (pass %d): %w", pass, err))
		}
		n++
		if tx.Weight < 0 {
			return s.fail(newDetailError(fmt.Sprintf("%s: transaction %d (pass %d): weight %d",
				ErrNegativeWeight, n, pass, tx.Weight), ErrNegativeWeight))
		}
		if w > math.MaxInt64-tx.Weight {
			return s.fail(newDetailError(fmt.Sprintf("%s: at transaction %d", ErrWeightOverflow, n),
				ErrWeightOverflow))
		}
		w += tx.Weight
		if pass > 1 && n > s.n1 {
			return s.fail(newDetailError(fmt.Sprintf("%s: pass %d has more than the %d transactions of pass 1",
				ErrNotReplayable, pass, s.n1), ErrNotReplayable))
		}
		if done != nil && n%checkEvery == 0 {
			select {
			case <-done:
				return s.fail(s.ctx.Err())
			default:
			}
		}
		if pass == 1 && s.hook != nil {
			if err := s.hook(tx.Items); err != nil {
				return s.fail(err)
			}
		}
		if !yield(tx.Items, tract.Supp(tx.Weight)) {
			return nil
		}
	}
	if pass == 1 {
		s.n1, s.w1 = n, w
	} else if n != s.n1 || w != s.w1 {
		return s.fail(newDetailError(fmt.Sprintf(
			"%s: pass %d has %d transactions of total weight %d, pass 1 had %d of total weight %d",
			ErrNotReplayable, pass, n, w, s.n1, s.w1), ErrNotReplayable))
	}
	return nil
}
