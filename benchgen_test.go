package fpgrowth

// Deterministic benchmark datasets. The code between the BEGIN and END
// markers is a verbatim copy of reference/bench/gen/core.go, which writes
// the same datasets as files for the comparison with the C reference
// (reference/bench/compare.sh); reference/bench/gen's TestCoreInSync
// fails if the copies differ. Edit both together.

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"strconv"
)

// BEGIN BENCHGEN CORE

// benchSpec describes a deterministic benchmark dataset. All randomness
// comes from a PCG generator seeded with Seed; bounded integers, shuffles
// and the item popularity (integer arithmetic only) are derived by this
// code, not by math/rand/v2's Rand methods, so a spec gives the same bytes
// on every platform and Go version.
type benchSpec struct {
	Name string
	Seed uint64
	// Transactions is the number of transactions.
	Transactions int
	// Items is the size of the item universe; items are named by their
	// decimal index, item 0 being the most popular one.
	Items int
	// MinLen and MaxLen bound the number of distinct random items per
	// transaction (uniform); planted pattern items come on top.
	MinLen, MaxLen int
	// Skew is the Zipf exponent of item popularity: item i is drawn with
	// weight floor(2^40/(i+1)^Skew); 0 means uniform.
	Skew int
	// Patterns item sets of PatternLen distinct items, drawn uniformly
	// once, are planted: a transaction gets one of them (chosen uniformly)
	// with probability PatternPermille/1000, each item of the pattern
	// being kept with probability KeepPermille/1000. They create
	// correlated items, long frequent item sets and closed sets.
	Patterns, PatternLen int
	PatternPermille      int
	KeepPermille         int
	// MaxWeight > 0 gives every transaction an integer weight drawn
	// uniformly from [1, MaxWeight] (written as the last field, fpgrowth
	// -w); 0 means unweighted.
	MaxWeight int
}

// benchSpecs are the datasets of the benchmark suite and of the C
// comparison (reference/bench/compare.sh).
var benchSpecs = []benchSpec{
	// Sparse retail-like data: medium universe, short transactions.
	{Name: "sparse", Seed: 1, Transactions: 20000, Items: 1000, MinLen: 4, MaxLen: 12, Skew: 1,
		Patterns: 30, PatternLen: 5, PatternPermille: 300, KeepPermille: 900},
	// Large sparse data: large universe, many transactions.
	{Name: "sparse-large", Seed: 2, Transactions: 200000, Items: 20000, MinLen: 5, MaxLen: 15, Skew: 1,
		Patterns: 100, PatternLen: 6, PatternPermille: 200, KeepPermille: 900},
	// Dense data: small universe, transactions cover a third of it,
	// strongly correlated planted patterns (long frequent item sets).
	{Name: "dense", Seed: 3, Transactions: 20000, Items: 80, MinLen: 10, MaxLen: 20, Skew: 0,
		Patterns: 4, PatternLen: 18, PatternPermille: 900, KeepPermille: 1000},
	// Long transactions over a medium universe.
	{Name: "long", Seed: 4, Transactions: 10000, Items: 400, MinLen: 40, MaxLen: 80, Skew: 0,
		Patterns: 20, PatternLen: 8, PatternPermille: 400, KeepPermille: 900},
	// Weighted variant of "sparse" (weights 1..20).
	{Name: "weighted", Seed: 5, Transactions: 20000, Items: 1000, MinLen: 4, MaxLen: 12, Skew: 1,
		Patterns: 30, PatternLen: 5, PatternPermille: 300, KeepPermille: 900, MaxWeight: 20},
}

// benchSpecByName returns the spec with the given name.
func benchSpecByName(name string) (benchSpec, bool) {
	for _, s := range benchSpecs {
		if s.Name == name {
			return s, true
		}
	}
	return benchSpec{}, false
}

// benchTx is a generated transaction: item indices (distinct, in random
// order) and a weight (1 if unweighted).
type benchTx struct {
	Items  []int32
	Weight int64
}

// validate checks the spec.
func (s benchSpec) validate() error {
	switch {
	case s.Transactions < 0 || s.Items < 1:
		return fmt.Errorf("benchgen %s: invalid size", s.Name)
	case s.MinLen < 0 || s.MaxLen < s.MinLen || s.MaxLen > s.Items/2+1:
		return fmt.Errorf("benchgen %s: invalid length range [%d, %d]", s.Name, s.MinLen, s.MaxLen)
	case s.Skew < 0 || s.Skew > 4:
		return fmt.Errorf("benchgen %s: invalid skew %d", s.Name, s.Skew)
	case s.Patterns < 0 || s.PatternLen < 0 || s.PatternLen > s.Items:
		return fmt.Errorf("benchgen %s: invalid patterns", s.Name)
	case s.PatternPermille < 0 || s.PatternPermille > 1000 || s.KeepPermille < 0 || s.KeepPermille > 1000:
		return fmt.Errorf("benchgen %s: invalid probabilities", s.Name)
	case s.MaxWeight < 0 || int64(s.MaxWeight)*int64(s.Transactions) > math.MaxInt32:
		return fmt.Errorf("benchgen %s: total weight may exceed the C reference's 32-bit supports", s.Name)
	}
	return nil
}

// benchGenerate calls fn for every transaction of the dataset, in order.
// The items slice is reused between calls.
func benchGenerate(s benchSpec, fn func(tx benchTx) error) error {
	if err := s.validate(); err != nil {
		return err
	}
	r := &benchRNG{src: rand.NewPCG(s.Seed, 0x9e3779b97f4a7c15)}
	// cumulative popularity for inverse-CDF sampling
	cum := make([]uint64, s.Items)
	var total uint64
	for i := range cum {
		w := uint64(1) << 40
		for range s.Skew {
			w /= uint64(i + 1)
		}
		total += max(w, 1)
		cum[i] = total
	}
	patterns := make([][]int32, s.Patterns)
	for k := range patterns {
		perm := make([]int32, s.Items)
		for i := range perm {
			perm[i] = int32(i)
		}
		r.shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
		patterns[k] = perm[:s.PatternLen]
	}
	mark := make([]uint32, s.Items) // mark[i] == stamp: item i is in the transaction
	items := make([]int32, 0, s.MaxLen+s.PatternLen)
	for t := range s.Transactions {
		stamp := uint32(t + 1)
		items = items[:0]
		n := s.MinLen + r.intN(s.MaxLen-s.MinLen+1)
		for len(items) < n {
			u := r.uint64N(total)
			lo, hi := 0, len(cum)-1 // smallest i with cum[i] > u
			for lo < hi {
				mid := int(uint(lo+hi) >> 1)
				if cum[mid] > u {
					hi = mid
				} else {
					lo = mid + 1
				}
			}
			if mark[lo] != stamp {
				mark[lo] = stamp
				items = append(items, int32(lo))
			}
		}
		if s.Patterns > 0 && r.intN(1000) < s.PatternPermille {
			for _, i := range patterns[r.intN(s.Patterns)] {
				if r.intN(1000) < s.KeepPermille && mark[i] != stamp {
					mark[i] = stamp
					items = append(items, i)
				}
			}
		}
		r.shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
		w := int64(1)
		if s.MaxWeight > 0 {
			w = 1 + int64(r.intN(s.MaxWeight))
		}
		if err := fn(benchTx{Items: items, Weight: w}); err != nil {
			return err
		}
	}
	return nil
}

// benchWrite writes the dataset in upstream's default table format (items
// separated by single spaces, one transaction per line, the weight as the
// last field if s.MaxWeight > 0) or, with csv, as comma-separated values
// (the weight as the last column).
func benchWrite(w io.Writer, s benchSpec, csv bool) error {
	bw := bufio.NewWriterSize(w, 1<<16)
	sep := byte(' ')
	if csv {
		sep = ','
	}
	var line []byte
	err := benchGenerate(s, func(tx benchTx) error {
		line = line[:0]
		for k, i := range tx.Items {
			if k > 0 {
				line = append(line, sep)
			}
			line = strconv.AppendInt(line, int64(i), 10)
		}
		if s.MaxWeight > 0 {
			line = append(line, sep)
			line = strconv.AppendInt(line, tx.Weight, 10)
		}
		line = append(line, '\n')
		_, err := bw.Write(line)
		return err
	})
	if err != nil {
		return err
	}
	return bw.Flush()
}

// benchRNG derives bounded integers and shuffles from a PCG source with
// fixed algorithms.
type benchRNG struct {
	src *rand.PCG
}

// uint64N returns a uniform value in [0, n) by rejection sampling; n > 0.
func (r *benchRNG) uint64N(n uint64) uint64 {
	limit := math.MaxUint64 - (-n)%n
	for {
		if v := r.src.Uint64(); v <= limit {
			return v % n
		}
	}
}

// intN returns a uniform value in [0, n); n > 0.
func (r *benchRNG) intN(n int) int { return int(r.uint64N(uint64(n))) }

// shuffle is a Fisher-Yates shuffle of n elements.
func (r *benchRNG) shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		swap(i, r.intN(i+1))
	}
}

// END BENCHGEN CORE
