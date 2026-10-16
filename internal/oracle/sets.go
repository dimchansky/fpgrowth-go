package oracle

import (
	"fmt"
	"math/bits"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Target selects which item sets Mine reports (upstream -t).
type Target uint8

// Item set targets. The zero value is Frequent, upstream's default (-ts),
// deliberately not an "unknown" sentinel.
const (
	Frequent   Target = iota // -ts: all frequent item sets
	Closed                   // -tc: closed item sets
	Maximal                  // -tm: maximal item sets
	Generators               // -tg: generators (free / key item sets)
)

// String returns the target's name.
func (t Target) String() string {
	switch t {
	case Frequent:
		return "frequent"
	case Closed:
		return "closed"
	case Maximal:
		return "maximal"
	case Generators:
		return "generators"
	default:
		return fmt.Sprintf("Target(%d)", uint8(t))
	}
}

// SetParams configures Mine. Start from NewSetParams for upstream's
// defaults; the zero value has MaxSupp = MaxSize = 0 and reports nothing.
type SetParams struct {
	Target Target
	// MinSupp is the absolute minimum support (smin); values below 1 act
	// as 1 (EffectiveMinSupp). Frequent means supp >= max(MinSupp, 1).
	MinSupp tract.Supp
	// MaxSupp is the absolute maximum support (smax) of reported sets;
	// NoMaxSupp for no limit. It filters reported sets only.
	MaxSupp tract.Supp
	// MinSize and MaxSize bound the size of reported sets (zmin, zmax);
	// MinSize 0 admits the empty set, NoMaxSize means no limit.
	MinSize, MaxSize int
	// Border is the per-size support border (upstream -F, see AbsBorder):
	// a set of size k < len(Border) is not reported if its support is below
	// Border[k]; sizes >= len(Border) are not restricted.
	Border []tract.Supp
	// Eval, if non-nil, evaluates sets by a rule measure and reports only
	// sets that pass its threshold (see SetEval).
	Eval *SetEval
}

// NewSetParams returns upstream's defaults for target with the absolute
// minimum support minSupp: sizes from 1 without upper limit, no maximum
// support, no border, no evaluation.
func NewSetParams(target Target, minSupp tract.Supp) SetParams {
	return SetParams{
		Target:  target,
		MinSupp: minSupp,
		MaxSupp: NoMaxSupp,
		MinSize: 1,
		MaxSize: NoMaxSize,
	}
}

// validate checks p.
func (p *SetParams) validate() error {
	if p.Target > Generators {
		return fmt.Errorf("oracle: invalid target %d", p.Target)
	}
	if p.MinSize < 0 || p.MaxSize < 0 {
		return fmt.Errorf("oracle: invalid size range [%d, %d]", p.MinSize, p.MaxSize)
	}
	if p.Eval != nil {
		return p.Eval.validate()
	}
	return nil
}

// Set is a reported item set.
type Set struct {
	Items ItemSet
	Supp  tract.Supp
	// Eval is the SetEval value, 0 without evaluation.
	Eval float64
}

// String formats the set as {"a" "b"} supp=3 (plus eval=... if non-zero).
func (s Set) String() string {
	if s.Eval != 0 {
		return fmt.Sprintf("%v supp=%d eval=%v", s.Items, s.Supp, s.Eval)
	}
	return fmt.Sprintf("%v supp=%d", s.Items, s.Supp)
}

// Mine returns the item sets selected by p, in canonical order (SortSets).
//
// A set S is reported if and only if
//
//  1. S is frequent: supp(S) >= max(MinSupp, 1);
//  2. S has the target property, decided against the complete lattice of
//     frequent sets (IsClosed, IsMaximal with MinSupp, IsGenerator);
//  3. supp(S) <= MaxSupp;
//  4. MinSize <= |S| <= MaxSize;
//  5. |S| >= len(Border) or supp(S) >= Border[|S|];
//  6. Eval is nil or the set's value v satisfies v*dir >= dir*threshold.
//
// Filters 3-6 never change which sets are closed, maximal or generators.
func (db *DB) Mine(p SetParams) ([]Set, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	smin := EffectiveMinSupp(p.MinSupp)
	var evaluator *setEvaluator
	if p.Eval != nil {
		evaluator = newSetEvaluator(db, p.Eval)
	}
	sets := []Set{}
	for m := range uint64(len(db.supp)) {
		s := db.supp[m]
		if s < smin || s > p.MaxSupp {
			continue
		}
		size := bits.OnesCount64(m)
		if size < p.MinSize || size > p.MaxSize {
			continue
		}
		if size < len(p.Border) && s < p.Border[size] {
			continue
		}
		if !db.hasTarget(m, p.Target, smin) {
			continue
		}
		var v float64
		if evaluator != nil {
			var err error
			if v, err = evaluator.value(m); err != nil {
				return nil, err
			}
			if !p.Eval.Measure.acceptsSet(v) {
				continue
			}
		}
		sets = append(sets, Set{Items: db.set(m), Supp: s, Eval: v})
	}
	SortSets(sets)
	return sets, nil
}

// hasTarget reports whether the frequent set m has the target property.
func (db *DB) hasTarget(m uint64, t Target, smin tract.Supp) bool {
	switch t {
	case Closed:
		return db.isClosed(m)
	case Maximal:
		return db.isMaximal(m, smin)
	case Generators:
		return db.isGenerator(m)
	default:
		return true
	}
}

// IsFrequent reports whether supp(s) >= max(smin, 1).
func (db *DB) IsFrequent(s ItemSet, smin tract.Supp) bool {
	return db.Support(s) >= EffectiveMinSupp(smin)
}

// IsClosed reports whether no proper superset of s (within the universe)
// has the same support as s. Because supports are anti-monotone, it
// suffices to check the supersets with one more item. The empty set is
// closed if and only if no item occurs with the total weight. A set with
// an item outside the universe is not closed.
func (db *DB) IsClosed(s ItemSet) bool {
	m, ok := db.mask(s)
	return ok && db.isClosed(m)
}

// IsMaximal reports whether s is maximal for the minimum support smin:
// s is not empty, supp(s) >= max(smin, 1), and no proper superset has
// support >= max(smin, 1). The empty set is never maximal (upstream never
// reports it; it could only be maximal if no item were frequent).
func (db *DB) IsMaximal(s ItemSet, smin tract.Supp) bool {
	m, ok := db.mask(s)
	return ok && db.supp[m] >= EffectiveMinSupp(smin) && db.isMaximal(m, EffectiveMinSupp(smin))
}

// IsGenerator reports whether no proper subset of s has the same support as
// s, where the empty set has the total weight as its support. It suffices
// to check the subsets with one item less. The empty set is a generator.
// A set with an item outside the universe is not a generator.
func (db *DB) IsGenerator(s ItemSet) bool {
	m, ok := db.mask(s)
	return ok && db.isGenerator(m)
}

func (db *DB) isClosed(m uint64) bool {
	s := db.supp[m]
	for r := db.full() &^ m; r != 0; r &= r - 1 {
		if db.supp[m|r&-r] == s {
			return false
		}
	}
	return true
}

func (db *DB) isMaximal(m uint64, smin tract.Supp) bool {
	if m == 0 {
		return false
	}
	for r := db.full() &^ m; r != 0; r &= r - 1 {
		if db.supp[m|r&-r] >= smin {
			return false
		}
	}
	return true
}

func (db *DB) isGenerator(m uint64) bool {
	s := db.supp[m]
	for r := m; r != 0; r &= r - 1 {
		if db.supp[m&^(r&-r)] == s {
			return false
		}
	}
	return true
}
