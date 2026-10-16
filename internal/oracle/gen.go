package oracle

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// NameStyle selects how Generate names items. Item i is the i-th most
// popular item (see GenConfig.Skew).
type NameStyle uint8

// Name styles. The first three always round-trip through WriteTable and
// upstream's default reader; NamesHash round-trips thanks to WriteTable's
// comment protection; NamesSeparators never round-trips and is meant only
// for differential tests of input readers (port versus C).
const (
	// NamesLetters: a, b, ..., z, aa, ab, ... (bijective base 26).
	NamesLetters NameStyle = iota
	// NamesNumbers: 0, 1, 2, ...; byte order differs from numeric order.
	NamesNumbers
	// NamesUnicode: multi-byte UTF-8 names (Greek, Cyrillic, CJK, emoji),
	// every second one with an ASCII prefix, e.g. "α", "xа", "一".
	NamesUnicode
	// NamesHash (risky, opt-in): letters, every third name prefixed with
	// '#', the default comment character: "#a", "b", "c", "#d", ...
	NamesHash
	// NamesSeparators (risky, opt-in): letters with an embedded field
	// separator or blank ("a,x", "b x", "c\tx") or a trailing carriage
	// return ("d\r"). Upstream's reader splits or trims such names.
	NamesSeparators
)

// RoundTrips reports whether every name of the style survives WriteTable
// followed by upstream's default reader unchanged (see SafeName).
func (s NameStyle) RoundTrips() bool {
	return s <= NamesHash
}

// itemName returns the name of item i in style s.
func (s NameStyle) itemName(i int) string {
	switch s {
	case NamesNumbers:
		return strconv.Itoa(i)
	case NamesUnicode:
		blocks := [...]rune{0x03B1, 0x0430, 0x4E00, 0x1F600} // α, а, 一, 😀
		name := string(blocks[i%len(blocks)] + rune(i/len(blocks)%24))
		if i/(len(blocks)*24) > 0 {
			name += strconv.Itoa(i / (len(blocks) * 24))
		}
		if i%2 == 1 {
			name = "x" + name
		}
		return name
	case NamesHash:
		if i%3 == 0 {
			return "#" + letters(i)
		}
		return letters(i)
	case NamesSeparators:
		suffixes := [...]string{",x", " x", "\tx", "\r"}
		return letters(i) + suffixes[i%len(suffixes)]
	default:
		return letters(i)
	}
}

// letters returns the bijective base-26 name of i: a..z, aa..zz, aaa, ...
func letters(i int) string {
	var buf [16]byte
	pos := len(buf)
	for n := i + 1; n > 0; n = (n - 1) / 26 {
		pos--
		buf[pos] = byte('a' + (n-1)%26)
	}
	return string(buf[pos:])
}

// WeightDist selects the transaction weights of Generate.
type WeightDist uint8

// Weight distributions. MaxWeight 0 selects the default given per value.
const (
	// WeightsUnit: every weight is 1 (writable without -w).
	WeightsUnit WeightDist = iota
	// WeightsSmall: uniform in [1, MaxWeight], default MaxWeight 5.
	WeightsSmall
	// WeightsWithZero: uniform in [0, MaxWeight], default MaxWeight 3.
	// Zero weights exercise upstream corner cases (zero-weight records
	// count toward the record count, merged zero-weight groups are deleted
	// by tbg_reduce, and the 16-items machine assumes positive weights,
	// tract.md T1), so differential tests against C must expect them.
	WeightsWithZero
	// WeightsLarge: uniform in [MaxWeight/2, MaxWeight], default
	// MaxWeight 1<<20.
	WeightsLarge
)

// GenConfig configures Generate. All randomness comes from a PCG generator
// seeded with Seed (math/rand/v2 PCG, whose output sequence is a fixed
// algorithm); bounded integers and floats are derived from its Uint64
// output by this package's own code, and popularity weights use integer
// arithmetic only. Hence the same configuration always yields the same
// dataset and the same WriteTable bytes, on every platform and Go version.
type GenConfig struct {
	Seed uint64
	// NumTransactions is the number of transactions (>= 0).
	NumTransactions int
	// NumItems is the alphabet size (>= 1).
	NumItems int
	// MinLen and MaxLen bound the number of distinct random items per
	// non-empty transaction (uniform; capped at NumItems). Planted
	// patterns and duplicates come on top.
	MinLen, MaxLen int
	// Skew makes item popularity Zipf-like: item i is drawn with weight
	// floor(2^40/(i+1)^Skew) (at least 1). 0 means uniform; 0..8.
	Skew int
	// Patterns planted item sets of PatternLen distinct items are drawn
	// once; each non-empty transaction contains one of them (chosen
	// uniformly) with probability PatternProb. They create correlated
	// items, equal supports and perfect extensions.
	Patterns, PatternLen int
	PatternProb          float64
	// DupItemProb is the probability that an item of a transaction is
	// written a second time (duplicates must be ignored by readers).
	DupItemProb float64
	// DupTxProb is the probability that a transaction (other than the
	// first) repeats the items of an earlier one, in shuffled order, with
	// its own weight.
	DupTxProb float64
	// EmptyProb is the probability of an empty transaction.
	EmptyProb float64
	// Weights selects the weight distribution and MaxWeight its upper
	// bound (0: the distribution's default).
	Weights   WeightDist
	MaxWeight tract.Supp
	// MaxTotal bounds NumTransactions*MaxWeight, the largest possible
	// total weight; 0 means math.MaxInt32, the limit of the C reference's
	// 32-bit SUPP type.
	MaxTotal tract.Supp
	// Names selects the item names.
	Names NameStyle
}

// Generate returns a random dataset as described by cfg.
func Generate(cfg GenConfig) ([]Transaction, error) {
	maxWeight, err := cfg.validate()
	if err != nil {
		return nil, err
	}
	g := &generator{
		cfg:       cfg,
		rnd:       &rng{src: rand.NewPCG(cfg.Seed, 0x9e3779b97f4a7c15)},
		maxWeight: maxWeight,
		names:     make([]string, cfg.NumItems),
		pop:       make([]uint64, cfg.NumItems),
		scratch:   make([]uint64, cfg.NumItems),
	}
	for i := range g.names {
		g.names[i] = cfg.Names.itemName(i)
		g.pop[i] = popularity(i, cfg.Skew)
	}
	patterns := make([][]int, cfg.Patterns)
	for k := range patterns {
		patterns[k] = g.sampleDistinct(cfg.PatternLen)
	}

	txs := make([]Transaction, 0, cfg.NumTransactions)
	for t := range cfg.NumTransactions {
		weight := g.weight()
		if t > 0 && g.rnd.float() < cfg.DupTxProb {
			items := append([]string{}, txs[g.rnd.intN(t)].Items...)
			g.rnd.shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
			txs = append(txs, Transaction{Items: items, Weight: weight})
			continue
		}
		if g.rnd.float() < cfg.EmptyProb {
			txs = append(txs, Transaction{Items: []string{}, Weight: weight})
			continue
		}
		idx := g.sampleDistinct(cfg.MinLen + g.rnd.intN(cfg.MaxLen-cfg.MinLen+1))
		if cfg.Patterns > 0 && g.rnd.float() < cfg.PatternProb {
			for _, i := range patterns[g.rnd.intN(cfg.Patterns)] {
				if !containsInt(idx, i) {
					idx = append(idx, i)
				}
			}
		}
		for _, i := range idx {
			if g.rnd.float() < cfg.DupItemProb {
				idx = append(idx, i)
			}
		}
		g.rnd.shuffle(len(idx), func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
		items := make([]string, len(idx))
		for k, i := range idx {
			items[k] = g.names[i]
		}
		txs = append(txs, Transaction{Items: items, Weight: weight})
	}
	return txs, nil
}

// validate checks cfg and returns the effective maximum weight.
func (cfg *GenConfig) validate() (tract.Supp, error) {
	isProb := func(p float64) bool { return p >= 0 && p <= 1 }
	switch {
	case cfg.NumTransactions < 0:
		return 0, fmt.Errorf("oracle: invalid number of transactions %d", cfg.NumTransactions)
	case cfg.NumItems < 1:
		return 0, fmt.Errorf("oracle: invalid number of items %d", cfg.NumItems)
	case cfg.MinLen < 0 || cfg.MaxLen < cfg.MinLen:
		return 0, fmt.Errorf("oracle: invalid transaction length range [%d, %d]", cfg.MinLen, cfg.MaxLen)
	case cfg.Skew < 0 || cfg.Skew > 8:
		return 0, fmt.Errorf("oracle: invalid skew %d", cfg.Skew)
	case cfg.Patterns < 0 || cfg.PatternLen < 0 || cfg.PatternLen > cfg.NumItems:
		return 0, fmt.Errorf("oracle: invalid patterns %d x %d", cfg.Patterns, cfg.PatternLen)
	case !isProb(cfg.PatternProb) || !isProb(cfg.DupItemProb) || !isProb(cfg.DupTxProb) || !isProb(cfg.EmptyProb):
		return 0, errors.New("oracle: probabilities must be in [0, 1]")
	case cfg.Names > NamesSeparators:
		return 0, fmt.Errorf("oracle: invalid name style %d", cfg.Names)
	}
	maxWeight := cfg.MaxWeight
	switch cfg.Weights {
	case WeightsUnit:
		maxWeight = 1
	case WeightsSmall:
		if maxWeight == 0 {
			maxWeight = 5
		}
	case WeightsWithZero:
		if maxWeight == 0 {
			maxWeight = 3
		}
	case WeightsLarge:
		if maxWeight == 0 {
			maxWeight = 1 << 20
		}
	default:
		return 0, fmt.Errorf("oracle: invalid weight distribution %d", cfg.Weights)
	}
	if maxWeight < 1 {
		return 0, fmt.Errorf("oracle: invalid maximum weight %d", cfg.MaxWeight)
	}
	maxTotal := cfg.MaxTotal
	if maxTotal == 0 {
		maxTotal = math.MaxInt32
	}
	if cfg.NumTransactions > 0 && maxWeight > maxTotal/tract.Supp(cfg.NumTransactions) {
		return 0, fmt.Errorf("oracle: %d transactions of weight up to %d may exceed the total %d",
			cfg.NumTransactions, maxWeight, maxTotal)
	}
	return maxWeight, nil
}

// popularity returns floor(2^40/(i+1)^skew), at least 1.
func popularity(i, skew int) uint64 {
	w := uint64(1) << 40
	for range skew {
		w /= uint64(i + 1)
	}
	return max(w, 1)
}

// generator holds the state of one Generate call.
type generator struct {
	cfg       GenConfig
	rnd       *rng
	maxWeight tract.Supp
	names     []string
	pop       []uint64 // popularity weight per item
	scratch   []uint64
}

// weight draws a transaction weight.
func (g *generator) weight() tract.Supp {
	switch g.cfg.Weights {
	case WeightsSmall:
		return 1 + tract.Supp(g.rnd.intN(int(g.maxWeight)))
	case WeightsWithZero:
		return tract.Supp(g.rnd.intN(int(g.maxWeight) + 1))
	case WeightsLarge:
		lo := max(g.maxWeight/2, 1)
		return lo + tract.Supp(g.rnd.intN(int(g.maxWeight-lo+1)))
	default:
		return 1
	}
}

// sampleDistinct draws min(n, NumItems) distinct items, each draw with
// probability proportional to the remaining items' popularity.
func (g *generator) sampleDistinct(n int) []int {
	n = min(n, len(g.pop))
	rem := g.scratch
	copy(rem, g.pop)
	var total uint64
	for _, w := range rem {
		total += w
	}
	idx := make([]int, 0, n)
	for range n {
		u := g.rnd.uint64N(total)
		i := 0
		for u >= rem[i] {
			u -= rem[i]
			i++
		}
		idx = append(idx, i)
		total -= rem[i]
		rem[i] = 0
	}
	return idx
}

// containsInt reports whether v is in s.
func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// rng derives bounded integers, floats and shuffles from a PCG source with
// fixed algorithms, independent of math/rand/v2's Rand methods.
type rng struct {
	src *rand.PCG
}

// uint64N returns a uniform value in [0, n) by rejection sampling; n > 0.
func (r *rng) uint64N(n uint64) uint64 {
	limit := math.MaxUint64 - (-n)%n // accept v <= limit: 2^64 - (2^64 mod n) values
	for {
		if v := r.src.Uint64(); v <= limit {
			return v % n
		}
	}
}

// intN returns a uniform value in [0, n); n > 0.
func (r *rng) intN(n int) int {
	return int(r.uint64N(uint64(n)))
}

// float returns a uniform value in [0, 1) with 53 random bits.
func (r *rng) float() float64 {
	return float64(r.src.Uint64()>>11) * 0x1p-53
}

// shuffle is a Fisher-Yates shuffle of n elements.
func (r *rng) shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		swap(i, r.intN(i+1))
	}
}

// SafeName reports whether name survives WriteTable followed by
// upstream's default reader (record separator "\n", field separators
// " \t,", blanks " \t\r", comment character '#') unchanged: it must be
// non-empty, at most 1024 bytes long (longer fields are truncated) and
// free of NUL (names are C strings), record and field separators and
// blanks. A leading '#' is safe because WriteTable protects records that
// would start with it. All other bytes, including invalid UTF-8, are
// opaque to the reader.
func SafeName(name string) bool {
	return name != "" && len(name) <= 1024 && !strings.ContainsAny(name, "\n \t,\r\x00")
}
