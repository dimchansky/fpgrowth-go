package difftest

// This file is a trimmed copy of the deterministic dataset generator of
// internal/oracle (gen.go, write.go). The reference module cannot import
// the root module's internal packages, and exec'ing a Go helper per case
// would cost a process start, so the generator is copied. It keeps the
// oracle's algorithms (PCG source with fixed bounded-integer, float and
// shuffle derivations), so a seed gives the same dataset on every platform
// and Go version. Only the name styles that round-trip through upstream's
// default reader are kept.

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"strconv"
)

// transaction is one input record: item names (duplicates allowed, the
// first occurrence counts) and a non-negative weight.
type transaction struct {
	items  []string
	weight int64
}

// nameStyle selects how genConfig names items.
type nameStyle uint8

// Name styles (oracle.NamesLetters ... oracle.NamesHash).
const (
	namesLetters nameStyle = iota // a, b, ..., z, aa, ...
	namesNumbers                  // 0, 1, 2, ...
	namesUnicode                  // multi-byte UTF-8 names, half with an ASCII prefix
	namesHash                     // every third name starts with '#'
	numNameStyles
)

// itemName returns the name of item i in style s.
func (s nameStyle) itemName(i int) string {
	switch s {
	case namesNumbers:
		return strconv.Itoa(i)
	case namesUnicode:
		blocks := [...]rune{0x03B1, 0x0430, 0x4E00, 0x1F600} // α, а, 一, 😀
		name := string(blocks[i%len(blocks)] + rune(i/len(blocks)%24))
		if i/(len(blocks)*24) > 0 {
			name += strconv.Itoa(i / (len(blocks) * 24))
		}
		if i%2 == 1 {
			name = "x" + name
		}
		return name
	case namesHash:
		if i%3 == 0 {
			return "#" + letters(i)
		}
		return letters(i)
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

// weightDist selects the transaction weights.
type weightDist uint8

// Weight distributions (oracle.WeightsUnit ... oracle.WeightsLarge).
const (
	weightsUnit     weightDist = iota // all 1
	weightsSmall                      // uniform in [1, maxWeight], default 5
	weightsWithZero                   // uniform in [0, maxWeight], default 3
	weightsLarge                      // uniform in [maxWeight/2, maxWeight], default 1<<20
)

// genConfig is oracle.GenConfig (see there for the meaning of the fields).
type genConfig struct {
	seed                 uint64
	numTransactions      int
	numItems             int
	minLen, maxLen       int
	skew                 int
	patterns, patternLen int
	patternProb          float64
	dupItemProb          float64
	dupTxProb            float64
	emptyProb            float64
	weights              weightDist
	maxWeight            int64
	names                nameStyle
}

// generate is oracle.Generate. cfg must be valid (the case generator only
// builds valid configurations); the total weight stays below 2^31, the
// limit of the C reference's 32-bit SUPP type.
func generate(cfg genConfig) ([]transaction, error) {
	maxWeight := cfg.maxWeight
	switch cfg.weights {
	case weightsUnit:
		maxWeight = 1
	case weightsSmall:
		if maxWeight == 0 {
			maxWeight = 5
		}
	case weightsWithZero:
		if maxWeight == 0 {
			maxWeight = 3
		}
	case weightsLarge:
		if maxWeight == 0 {
			maxWeight = 1 << 20
		}
	}
	switch {
	case cfg.numTransactions < 0 || cfg.numItems < 1 || cfg.minLen < 0 || cfg.maxLen < cfg.minLen ||
		cfg.skew < 0 || cfg.skew > 8 || cfg.patternLen > cfg.numItems || maxWeight < 1:
		return nil, fmt.Errorf("difftest: invalid generator configuration %+v", cfg)
	case cfg.numTransactions > 0 && maxWeight > math.MaxInt32/int64(cfg.numTransactions):
		return nil, fmt.Errorf("difftest: total weight of %+v may exceed 2^31-1", cfg)
	}
	g := &generator{
		cfg:       cfg,
		rnd:       newRNG(cfg.seed),
		maxWeight: maxWeight,
		names:     make([]string, cfg.numItems),
		pop:       make([]uint64, cfg.numItems),
		scratch:   make([]uint64, cfg.numItems),
	}
	for i := range g.names {
		g.names[i] = cfg.names.itemName(i)
		g.pop[i] = popularity(i, cfg.skew)
	}
	patterns := make([][]int, cfg.patterns)
	for k := range patterns {
		patterns[k] = g.sampleDistinct(cfg.patternLen)
	}

	txs := make([]transaction, 0, cfg.numTransactions)
	for t := range cfg.numTransactions {
		weight := g.weight()
		if t > 0 && g.rnd.float() < cfg.dupTxProb {
			items := append([]string{}, txs[g.rnd.intN(t)].items...)
			g.rnd.shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
			txs = append(txs, transaction{items: items, weight: weight})
			continue
		}
		if g.rnd.float() < cfg.emptyProb {
			txs = append(txs, transaction{items: []string{}, weight: weight})
			continue
		}
		idx := g.sampleDistinct(cfg.minLen + g.rnd.intN(cfg.maxLen-cfg.minLen+1))
		if cfg.patterns > 0 && g.rnd.float() < cfg.patternProb {
			for _, i := range patterns[g.rnd.intN(cfg.patterns)] {
				if !containsInt(idx, i) {
					idx = append(idx, i)
				}
			}
		}
		for _, i := range idx {
			if g.rnd.float() < cfg.dupItemProb {
				idx = append(idx, i)
			}
		}
		g.rnd.shuffle(len(idx), func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
		items := make([]string, len(idx))
		for k, i := range idx {
			items[k] = g.names[i]
		}
		txs = append(txs, transaction{items: items, weight: weight})
	}
	return txs, nil
}

// popularity returns floor(2^40/(i+1)^skew), at least 1.
func popularity(i, skew int) uint64 {
	w := uint64(1) << 40
	for range skew {
		w /= uint64(i + 1)
	}
	return max(w, 1)
}

// generator holds the state of one generate call.
type generator struct {
	cfg       genConfig
	rnd       *rng
	maxWeight int64
	names     []string
	pop       []uint64
	scratch   []uint64
}

// weight draws a transaction weight.
func (g *generator) weight() int64 {
	switch g.cfg.weights {
	case weightsSmall:
		return 1 + int64(g.rnd.intN(int(g.maxWeight)))
	case weightsWithZero:
		return int64(g.rnd.intN(int(g.maxWeight) + 1))
	case weightsLarge:
		lo := max(g.maxWeight/2, 1)
		return lo + int64(g.rnd.intN(int(g.maxWeight-lo+1)))
	default:
		return 1
	}
}

// sampleDistinct draws min(n, numItems) distinct items, each draw with
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

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// rng derives bounded integers, floats and shuffles from a PCG source with
// fixed algorithms (oracle's rng). The case generator uses it too, so a
// case seed reproduces the same options on every Go version.
type rng struct {
	src *rand.PCG
}

func newRNG(seed uint64) *rng {
	return &rng{src: rand.NewPCG(seed, 0x9e3779b97f4a7c15)}
}

// uint64N returns a uniform value in [0, n) by rejection sampling; n > 0.
func (r *rng) uint64N(n uint64) uint64 {
	limit := math.MaxUint64 - (-n)%n
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

// writeTable is oracle.WriteTable: one record per transaction, items
// separated by single spaces, the weight as the last field if weighted
// (every weight must be 1 otherwise), and a leading space before a record
// that would start with '#'.
func writeTable(w io.Writer, txs []transaction, weighted bool) error {
	bw := bufio.NewWriter(w)
	for t, tx := range txs {
		if !weighted && tx.weight != 1 {
			return fmt.Errorf("difftest: transaction %d has weight %d; write it weighted", t, tx.weight)
		}
		if len(tx.items) > 0 && tx.items[0][0] == '#' {
			bw.WriteByte(' ')
		}
		for i, item := range tx.items {
			if i > 0 {
				bw.WriteByte(' ')
			}
			bw.WriteString(item)
		}
		if weighted {
			if len(tx.items) > 0 {
				bw.WriteByte(' ')
			}
			bw.WriteString(strconv.FormatInt(tx.weight, 10))
		}
		bw.WriteByte('\n')
	}
	return bw.Flush()
}
