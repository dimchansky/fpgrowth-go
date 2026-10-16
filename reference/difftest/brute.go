package difftest

// An independent brute-force evaluator, adapted from internal/oracle (which
// the reference module cannot import): the exact weighted support of every
// subset of a small item universe, computed with a superset-sum transform
// over bit masks, and the item set and rule definitions applied literally.
// It shares no code with the port.

import (
	"fmt"
	"math"
	"math/bits"
	"slices"
	"strings"
)

// maxBruteItems bounds the universe of bfDB (2^maxBruteItems supports).
const maxBruteItems = 18

// dblEpsilon is C's DBL_EPSILON.
const dblEpsilon = 0x1p-52

// bfDB is the support table of a transaction database.
type bfDB struct {
	items []string       // universe in byte order; bit i <-> items[i]
	index map[string]int // item -> bit
	supp  []int64        // supp[mask]
	total int64          // total weight, the support of the empty set
}

// newBF builds the support table. With a non-nil selection only the
// selected items are kept (upstream -R for item sets); every transaction
// still counts toward the total weight.
func newBF(txs []transaction, selection map[string]bool) (*bfDB, error) {
	names := map[string]bool{}
	var total int64
	for _, tx := range txs {
		total += tx.weight
		for _, it := range tx.items {
			if selection == nil || selection[it] {
				names[it] = true
			}
		}
	}
	if len(names) > maxBruteItems {
		return nil, fmt.Errorf("difftest: %d items exceed the brute-force limit %d", len(names), maxBruteItems)
	}
	db := &bfDB{index: map[string]int{}, total: total}
	for it := range names {
		db.items = append(db.items, it)
	}
	slices.Sort(db.items)
	for i, it := range db.items {
		db.index[it] = i
	}
	db.supp = make([]int64, 1<<len(db.items))
	for _, tx := range txs {
		var m uint64
		for _, it := range tx.items {
			if i, ok := db.index[it]; ok {
				m |= 1 << i
			}
		}
		db.supp[m] += tx.weight
	}
	for i := range db.items {
		bit := uint64(1) << i
		for m := range uint64(len(db.supp)) {
			if m&bit == 0 {
				db.supp[m] += db.supp[m|bit]
			}
		}
	}
	return db, nil
}

func (db *bfDB) full() uint64 { return uint64(len(db.supp) - 1) }

// mask returns the bit mask of items; ok is false if an item is unknown.
func (db *bfDB) mask(items []string) (uint64, bool) {
	var m uint64
	for _, it := range items {
		i, found := db.index[it]
		if !found {
			return 0, false
		}
		m |= 1 << i
	}
	return m, true
}

// support returns the support of items (0 if an item is unknown).
func (db *bfDB) support(items []string) int64 {
	m, ok := db.mask(items)
	if !ok {
		return 0
	}
	return db.supp[m]
}

// set returns the sorted items of mask m.
func (db *bfDB) set(m uint64) []string {
	s := make([]string, 0, bits.OnesCount64(m))
	for ; m != 0; m &= m - 1 {
		s = append(s, db.items[bits.TrailingZeros64(m)])
	}
	return s
}

func (db *bfDB) isClosed(m uint64) bool {
	s := db.supp[m]
	for r := db.full() &^ m; r != 0; r &= r - 1 {
		if db.supp[m|r&-r] == s {
			return false
		}
	}
	return true
}

func (db *bfDB) isMaximal(m uint64, smin int64) bool {
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

func (db *bfDB) isGenerator(m uint64) bool {
	s := db.supp[m]
	for r := m; r != 0; r &= r - 1 {
		if db.supp[m&^(r&-r)] == s {
			return false
		}
	}
	return true
}

// numFrequent returns the number of items with support >= max(smin, 1).
func (db *bfDB) numFrequent(smin int64) int {
	smin = max(smin, 1)
	n := 0
	for i := range db.items {
		if db.supp[1<<i] >= smin {
			n++
		}
	}
	return n
}

// bfSetParams selects item sets (oracle.SetParams without evaluation).
type bfSetParams struct {
	target     byte  // 's', 'c', 'm' or 'g'
	smin, smax int64 // absolute support range (smin < 1 acts as 1)
	zmin, zmax int   // size range
	border     []int64
}

// bfSet is an item set with its support.
type bfSet struct {
	items []string
	supp  int64
}

// sets returns the item sets selected by p: frequent sets with the target
// property decided on the complete lattice, then filtered by the maximum
// support, the size range and the border (see oracle.DB.Mine).
func (db *bfDB) sets(p bfSetParams) []bfSet {
	smin := max(p.smin, 1)
	var out []bfSet
	for m := range uint64(len(db.supp)) {
		s := db.supp[m]
		if s < smin || s > p.smax {
			continue
		}
		size := bits.OnesCount64(m)
		if size < p.zmin || size > p.zmax {
			continue
		}
		if size < len(p.border) && s < p.border[size] {
			continue
		}
		ok := true
		switch p.target {
		case 'c':
			ok = db.isClosed(m)
		case 'm':
			ok = db.isMaximal(m, smin)
		case 'g':
			ok = db.isGenerator(m)
		}
		if ok {
			out = append(out, bfSet{items: db.set(m), supp: s})
		}
	}
	return out
}

// Item appearances in rules (upstream APP_*).
type appearance uint8

const (
	appBoth appearance = iota
	appBody
	appHead
	appNone
)

// bfRuleParams selects association rules (oracle.RuleParams without
// evaluation).
type bfRuleParams struct {
	smin, smax int64   // support range of the rule support definition
	orig       bool    // -o: the support of body ∪ head is the rule support
	conf       float64 // minimum confidence as a fraction
	zmin, zmax int     // size range, body and head counted
	appear     map[string]appearance
	defApp     appearance
}

// bfRule is an association rule body -> head with its supports.
type bfRule struct {
	head           string
	body           []string
	supp, bsup, hs int64
}

// rules returns the association rules selected by p (oracle.DB.Rules,
// with upstream bugs B3 and B8 fixed: head-only items never occur in a
// body and the maximum support applies to the rule support).
func (db *bfDB) rules(p bfRuleParams) []bfRule {
	smin := max(p.smin, 1)
	setMin := smin
	if !p.orig {
		setMin = max(ruleSetMinSupp(p.smin, p.conf), 1)
	}
	confAdj := p.conf * (1 - dblEpsilon)
	var noneMask, headOnly, bodyOnly uint64
	for i, it := range db.items {
		a, ok := p.appear[it]
		if !ok {
			a = p.defApp
		}
		switch a {
		case appNone:
			noneMask |= 1 << i
		case appHead:
			headOnly |= 1 << i
		case appBody:
			bodyOnly |= 1 << i
		}
	}
	var out []bfRule
	for m := uint64(1); m < uint64(len(db.supp)); m++ {
		s := db.supp[m]
		if s < setMin || m&noneMask != 0 {
			continue
		}
		size := bits.OnesCount64(m)
		if size < p.zmin || size > p.zmax {
			continue
		}
		heads := m &^ bodyOnly
		if hd := m & headOnly; hd != 0 {
			heads &= hd
			if bits.OnesCount64(hd) > 1 {
				continue
			}
		}
		for r := heads; r != 0; r &= r - 1 {
			bit := r & -r
			b := db.supp[m&^bit]
			if p.orig {
				if s > p.smax {
					continue
				}
			} else if b < smin || b > p.smax {
				continue
			}
			if float64(s) < float64(b)*confAdj {
				continue
			}
			out = append(out, bfRule{
				head: db.items[bits.TrailingZeros64(bit)],
				body: db.set(m &^ bit),
				supp: s, bsup: b, hs: db.supp[bit],
			})
		}
	}
	return out
}

// absMinSupp converts a -s (or -F) argument like fpg_data and setbdr:
// negative values are absolute, others percentages of the total weight.
func absMinSupp(arg float64, total int64) int64 {
	if arg < 0 {
		return suppFromFloat(math.Ceil(-arg))
	}
	return suppFromFloat(math.Ceil(arg / 100 * float64(total) * (1 - dblEpsilon)))
}

// absMaxSupp converts a -S argument like fpg_report.
func absMaxSupp(arg float64, total int64) int64 {
	if arg < 0 {
		return suppFromFloat(math.Floor(-arg))
	}
	return suppFromFloat(math.Floor(arg / 100 * float64(total) * (1 + dblEpsilon)))
}

// ruleSetMinSupp is the minimum support of body ∪ head that fpg_data
// derives under the default (body) rule support definition.
func ruleSetMinSupp(bodyMin int64, conf float64) int64 {
	return suppFromFloat(math.Ceil(float64(bodyMin) * (conf * (1 - dblEpsilon))))
}

// absBorder converts -F values to a border indexed by set size.
func absBorder(args []float64, zmin int, total int64) []int64 {
	if len(args) == 0 {
		return nil
	}
	border := make([]int64, zmin+len(args))
	for i, a := range args {
		border[zmin+i] = absMinSupp(a, total)
	}
	return border
}

func suppFromFloat(x float64) int64 {
	switch {
	case math.IsNaN(x):
		return 0
	case x >= 0x1p63:
		return math.MaxInt64
	case x < -0x1p63:
		return math.MinInt64
	default:
		return int64(x)
	}
}

// setKey is the canonical key of a sorted item list.
func setKey(items []string) string { return strings.Join(items, "\x00") }

// ruleKey is the canonical key of a rule with a sorted body.
func ruleKey(head string, body []string) string { return head + "\x01" + setKey(body) }
