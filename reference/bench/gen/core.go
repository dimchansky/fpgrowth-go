package main

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"strconv"
)

// The code between the BEGIN and END markers is shared verbatim with the
// root module's benchmark suite (benchgen_test.go), which cannot import
// this nested module. TestCoreInSync (core_test.go) fails if the copies
// differ; edit both together.

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
	// Density > 0 selects independent items instead of the model above
	// (MinLen, MaxLen, Skew, Patterns and MaxWeight must be 0): every
	// item 0..Items-1 is in a transaction with probability Density/100,
	// independently, in ascending order. This is denseTxs of the M5
	// benchmark (internal/fpg/extcheck_bench_test.go) with the same seed:
	// a PCG source seeded (Seed, 1) and one math/rand/v2 Rand.Float64
	// per item and transaction, compared with Density/100 (both exact
	// IEEE operations). denseTxs names item j "i<j>", the files "<j>".
	Density int
	// Common > 0 (with Density > 0) makes the first Common items common:
	// item j < Common is present with probability CommonDensity/100
	// instead (two-tier data: a few common items, many rare ones).
	Common, CommonDensity int
	// Slices > 0 (with Density > 0) puts Slices mutually exclusive items
	// first: with probability SliceDensity/100 a transaction holds one
	// of them, chosen by the next PCG value modulo Slices (a categorical
	// attribute). The Common items follow them, then the others.
	Slices, SliceDensity int
	// Short > 0 selects long and short transactions instead (MinLen,
	// MaxLen, Skew, Patterns and Density must be 0; MaxWeight is
	// allowed): first the long transactions, items in ascending order,
	// then for every item i one short transaction of the Short items
	// i, i+1, …, i+Short−1 (modulo Items). With Step = 0 there is one
	// long transaction of all items; with Step > 0 there are
	// ⌈Items/Step⌉ suffix chains, chain j holding the items j·Step to
	// Items−1. Transactions must equal the resulting count. The closed
	// check of a single item then has a long first cover transaction and
	// short others (issue #20).
	Short, Step int
	// Trap > 0 selects the probe trap of issue #20 instead (every other
	// field but Name, Items and Transactions must be 0): with N = Items,
	// the lines {m1 m2 m3 r1 r2 r3 r4}, {m1 m2 m3 a0 … a(N−1)},
	// {r1 … r4 a0 … a(N−3)}, {a(N−1)}, {a(N−2)}, then Trap filler lines
	// {x(k mod 7)} (so that the closed check scans). Items are numbered
	// in this order of first appearance (m1 = 0, r1 = 3, a0 = 7,
	// x0 = N+7). With -q0 -tc -s-2 -n1 the first cover transaction of
	// {a_i} has a covering item among its first 4 probes, the shortest
	// one has not. Transactions must be Trap+5.
	Trap int
	// Chain > 0 selects the perfect-extension chain of issue #21 instead
	// (every other field but Name, Items and Transactions must be 0):
	// Chain copies of the line of the items 0, 1, …, Items−2, then
	// Transactions−Chain lines of the item Items−1. With N = Items−1,
	// Chain 17 and Transactions N+27 this is the T4 input of
	// docs/audit/t4-performance.md (17 lines of 0 … N−1, then N+10
	// lines of x) with x named N; with -s-17 -n1 every item of the
	// long line has all its predecessors as perfect extensions.
	Chain int
	// Wide > 0 selects wide lines of issue #21 instead (every other
	// field but Name, Items and Transactions must be 0): every one of
	// the Transactions lines holds the items 0, 1, …, Wide−1, followed
	// by its share of the items Wide … Items−1 (item Wide+k goes to line
	// k mod Transactions). With -s-1 -n1 (Transactions 1 or 2) the
	// common items are perfect extensions of the empty set.
	Wide int
}

// longShortCount returns the number of transactions of a Short > 0 spec.
func (s benchSpec) longShortCount() int {
	if s.Step == 0 {
		return 1 + s.Items
	}
	return (s.Items+s.Step-1)/s.Step + s.Items
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
	// Independent dense items for the size-limited closed/maximal search
	// (deviation M5, docs/m5-investigation.md): dNxKdP has N transactions
	// over K items, each present with probability P %. The first two
	// equal the in-process BenchmarkCloMaxLimited data (seeds 1 and 2).
	{Name: "d2000x40d60", Seed: 1, Transactions: 2000, Items: 40, Density: 60},
	{Name: "d1000x30d80", Seed: 2, Transactions: 1000, Items: 30, Density: 80},
	{Name: "d5000x60d40", Seed: 3, Transactions: 5000, Items: 60, Density: 40},
	// The dense maximal workload of the independent audit (M5A-1, issue
	// #19): many maximal sets of exactly the maximum size. The audit made
	// it with Python random.Random(5); this is the same shape (seed 5),
	// not the same bytes.
	{Name: "d20000x200d30", Seed: 5, Transactions: 20000, Items: 200, Density: 30},
	// Two-tier independent data (issue #19, performance review F1): 4
	// common items at 20 % and 200 rare ones at 10 %. With -tm -n3 at
	// smin 0.9·N·0.1²·0.2 every pair of rare items is a parent whose few
	// children (the common items) are all maximal: sibling rows must not
	// be built for them.
	{Name: "t100000x204c4", Seed: 6, Transactions: 100000, Items: 204, Density: 10, Common: 4, CommonDensity: 20},
	// Slices and spreaders (issue #19, performance review R2-1): 400
	// rare items at 15 %, 4 exclusive slice items (80 % of the
	// transactions hold one) and 10 spreaders at 65 %. With -tm -n3 at
	// smin 1350 a rare item's children are its 4 slices (maximal) and
	// 10 spreaders, which are never checked (they have frequent
	// supersets below them): sibling rows must not be built for them.
	{Name: "s50000x414", Seed: 7, Transactions: 50000, Items: 414, Density: 15, Common: 10, CommonDensity: 65,
		Slices: 4, SliceDensity: 80},
	// Long and short transactions (issue #20, review PR6-2 "lp2"): one
	// line of all N items, then N one-item lines, so with -tc -s-2 -n1
	// every item's cover is the long line and one short line. lpN equals
	// the awk reproducer of the issue with n = N byte for byte; the four
	// sizes show the scaling. lp50000s2: short lines of 2 items (the
	// candidates of a short line must not be intersected with the long
	// one item by item). lp50000w: weights 1..20. lpc20000: suffix
	// chains every 64 items, so that a long transaction leads every
	// cover word (the shortest cover transaction is never the first of
	// a word).
	{Name: "lp12500", Transactions: 12501, Items: 12500, Short: 1},
	{Name: "lp25000", Transactions: 25001, Items: 25000, Short: 1},
	{Name: "lp50000", Transactions: 50001, Items: 50000, Short: 1},
	{Name: "lp100000", Transactions: 100001, Items: 100000, Short: 1},
	{Name: "lp50000s2", Transactions: 50001, Items: 50000, Short: 2},
	{Name: "lp50000w", Seed: 8, Transactions: 50001, Items: 50000, Short: 1, MaxWeight: 20},
	{Name: "lpc20000", Transactions: 20313, Items: 20000, Short: 1, Step: 64},
	// The probe trap (issue #20, challenge H1): the candidates of {a_i}
	// must not move to the shortest cover transaction before the first
	// one is probed. adv5-N is the challenge's gen5.py with renamed
	// items (the same order of first appearance).
	{Name: "adv5-20000", Transactions: 200005, Items: 20000, Trap: 200000},
	{Name: "adv5-40000", Transactions: 200005, Items: 40000, Trap: 200000},
	// The perfect-extension chain (issue #21, T4 of the performance
	// audit): t4-N has a chain of N items with 17 copies and N+10 lines
	// of one other item; the three sizes show the scaling.
	{Name: "t4-6250", Transactions: 6277, Items: 6251, Chain: 17},
	{Name: "t4-12500", Transactions: 12527, Items: 12501, Chain: 17},
	{Name: "t4-25000", Transactions: 25027, Items: 25001, Chain: 17},
	// Wide lines (issue #21, design challenge F1). one-N: one line of N
	// items, so that with -s-1 -n1 the empty set's hypercube of N
	// perfect extensions is expanded to size 1. t4b-N: two lines with N
	// common items (perfect extensions inherited from the empty set) and
	// N items split between them (sets of size 1 with N extensions).
	{Name: "one-6250", Transactions: 1, Items: 6250, Wide: 6250},
	{Name: "one-12500", Transactions: 1, Items: 12500, Wide: 12500},
	{Name: "t4b-3000", Transactions: 2, Items: 6000, Wide: 3000},
	{Name: "t4b-6000", Transactions: 2, Items: 12000, Wide: 6000},
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
	case s.Density < 0 || s.Density > 100 || s.Density > 0 &&
		(s.MinLen != 0 || s.MaxLen != 0 || s.Skew != 0 || s.Patterns != 0 || s.MaxWeight != 0):
		return fmt.Errorf("benchgen %s: invalid density", s.Name)
	case s.Common < 0 || s.Common > s.Items || s.Common > 0 && s.Density == 0 ||
		s.CommonDensity < 0 || s.CommonDensity > 100:
		return fmt.Errorf("benchgen %s: invalid common items", s.Name)
	case s.Slices < 0 || s.Slices+s.Common > s.Items || s.Slices > 0 && s.Density == 0 ||
		s.SliceDensity < 0 || s.SliceDensity > 100:
		return fmt.Errorf("benchgen %s: invalid slices", s.Name)
	case s.Short < 0 || s.Step < 0 || s.Step > 0 && s.Short == 0 || s.Short > 0 &&
		(s.Short > s.Items || s.MinLen != 0 || s.MaxLen != 0 || s.Skew != 0 || s.Patterns != 0 ||
			s.Density != 0 || s.Transactions != s.longShortCount()):
		return fmt.Errorf("benchgen %s: invalid long and short transactions", s.Name)
	case s.Trap < 0 || s.Trap > 0 && (s.Items < 3 || s.Transactions != s.Trap+5 || s.MinLen != 0 ||
		s.MaxLen != 0 || s.Skew != 0 || s.Patterns != 0 || s.Density != 0 || s.Short != 0 || s.MaxWeight != 0):
		return fmt.Errorf("benchgen %s: invalid probe trap", s.Name)
	case s.Chain < 0 || s.Chain > 0 && (s.Items < 2 || s.Transactions < s.Chain || s.MinLen != 0 ||
		s.MaxLen != 0 || s.Skew != 0 || s.Patterns != 0 || s.Density != 0 || s.Short != 0 ||
		s.Trap != 0 || s.MaxWeight != 0):
		return fmt.Errorf("benchgen %s: invalid perfect-extension chain", s.Name)
	case s.Wide < 0 || s.Wide > 0 && (s.Wide > s.Items || s.Transactions < 1 || s.MinLen != 0 ||
		s.MaxLen != 0 || s.Skew != 0 || s.Patterns != 0 || s.Density != 0 || s.Short != 0 ||
		s.Trap != 0 || s.Chain != 0 || s.MaxWeight != 0):
		return fmt.Errorf("benchgen %s: invalid wide lines", s.Name)
	}
	return nil
}

// benchGenerate calls fn for every transaction of the dataset, in order.
// The items slice is reused between calls.
func benchGenerate(s benchSpec, fn func(tx benchTx) error) error {
	if err := s.validate(); err != nil {
		return err
	}
	if s.Density > 0 {
		return benchIndependent(s, fn)
	}
	if s.Short > 0 {
		return benchLongShort(s, fn)
	}
	if s.Trap > 0 {
		return benchTrap(s, fn)
	}
	if s.Chain > 0 {
		return benchChain(s, fn)
	}
	if s.Wide > 0 {
		return benchWide(s, fn)
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

// benchIndependent generates a dataset of independent items
// (benchSpec.Density).
func benchIndependent(s benchSpec, fn func(tx benchTx) error) error {
	src := rand.NewPCG(s.Seed, 1)
	d := float64(s.Density) / 100
	dc := float64(s.CommonDensity) / 100
	ds := float64(s.SliceDensity) / 100
	uniform := func() float64 { return float64(src.Uint64()<<11>>11) / (1 << 53) }
	items := make([]int32, 0, s.Items)
	for range s.Transactions {
		items = items[:0]
		if s.Slices > 0 && uniform() < ds {
			items = append(items, int32(src.Uint64()%uint64(s.Slices)))
		}
		for j := int32(s.Slices); j < int32(s.Items); j++ {
			p := d
			if j < int32(s.Slices+s.Common) {
				p = dc
			}
			// math/rand/v2's Rand.Float64, spelled out
			if float64(src.Uint64()<<11>>11)/(1<<53) < p {
				items = append(items, j)
			}
		}
		if err := fn(benchTx{Items: items, Weight: 1}); err != nil {
			return err
		}
	}
	return nil
}

// benchLongShort generates a dataset of long and short transactions
// (benchSpec.Short).
func benchLongShort(s benchSpec, fn func(tx benchTx) error) error {
	r := &benchRNG{src: rand.NewPCG(s.Seed, 0x9e3779b97f4a7c15)}
	weight := func() int64 {
		if s.MaxWeight > 0 {
			return 1 + int64(r.intN(s.MaxWeight))
		}
		return 1
	}
	items := make([]int32, 0, s.Items)
	step := s.Step
	if step == 0 {
		step = s.Items // one chain
	}
	for first := 0; first < s.Items; first += step {
		items = items[:0]
		for i := first; i < s.Items; i++ {
			items = append(items, int32(i))
		}
		if err := fn(benchTx{Items: items, Weight: weight()}); err != nil {
			return err
		}
	}
	for i := range s.Items {
		items = items[:0]
		for k := range s.Short {
			items = append(items, int32((i+k)%s.Items))
		}
		if err := fn(benchTx{Items: items, Weight: weight()}); err != nil {
			return err
		}
	}
	return nil
}

// benchTrap generates the probe trap of issue #20 (benchSpec.Trap).
func benchTrap(s benchSpec, fn func(tx benchTx) error) error {
	n := int32(s.Items)
	a := func(i int32) int32 { return 7 + i }
	items := make([]int32, 0, s.Items+3)
	emit := func() error { return fn(benchTx{Items: items, Weight: 1}) }
	items = append(items, 0, 1, 2, 3, 4, 5, 6) // m1 m2 m3 r1 r2 r3 r4
	if err := emit(); err != nil {
		return err
	}
	items = append(items[:0], 0, 1, 2) // m1 m2 m3 and every a
	for i := range n {
		items = append(items, a(i))
	}
	if err := emit(); err != nil {
		return err
	}
	items = append(items[:0], 3, 4, 5, 6) // r1 … r4 and a0 … a(N−3)
	for i := range n - 2 {
		items = append(items, a(i))
	}
	if err := emit(); err != nil {
		return err
	}
	for _, i := range []int32{n - 1, n - 2} {
		items = append(items[:0], a(i))
		if err := emit(); err != nil {
			return err
		}
	}
	for k := range s.Trap {
		items = append(items[:0], n+7+int32(k%7))
		if err := emit(); err != nil {
			return err
		}
	}
	return nil
}

// benchChain generates the perfect-extension chain of issue #21
// (benchSpec.Chain).
func benchChain(s benchSpec, fn func(tx benchTx) error) error {
	items := make([]int32, 0, s.Items-1)
	for i := range int32(s.Items - 1) {
		items = append(items, i)
	}
	for range s.Chain {
		if err := fn(benchTx{Items: items, Weight: 1}); err != nil {
			return err
		}
	}
	items = append(items[:0], int32(s.Items-1))
	for range s.Transactions - s.Chain {
		if err := fn(benchTx{Items: items, Weight: 1}); err != nil {
			return err
		}
	}
	return nil
}

// benchWide generates the wide lines of issue #21 (benchSpec.Wide).
func benchWide(s benchSpec, fn func(tx benchTx) error) error {
	items := make([]int32, 0, s.Items)
	for t := range s.Transactions {
		items = items[:0]
		for i := range int32(s.Wide) {
			items = append(items, i)
		}
		for i := s.Wide + t; i < s.Items; i += s.Transactions {
			items = append(items, int32(i))
		}
		if err := fn(benchTx{Items: items, Weight: 1}); err != nil {
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
