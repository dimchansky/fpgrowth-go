package psp

import (
	"context"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// TestMulAdd_noFusion pins mulAdd to C's unfused a*b + c: with these
// operands a fused multiply-add keeps the 2^-60 that the rounded product
// loses.
func TestMulAdd_noFusion(t *testing.T) {
	a := 1 + 0x1p-30
	if got, fused := mulAdd(a, a, -1), math.FMA(a, a, -1); got != 0x1p-29 || fused == got {
		t.Errorf("mulAdd = %x, want %x (FMA %x)", got, 0x1p-29, fused)
	}
}

// TestChoose checks choose, including upstream's conservative overflow
// test (it gives up when r·n overflows, even if the result would fit).
func TestChoose(t *testing.T) {
	for _, c := range []struct {
		n, k int
		want uint64
	}{
		{5, 0, 1}, {5, 2, 10}, {5, 5, 1}, {10, 3, 120}, {60, 30, 118264581564861424},
		{62, 31, 465428353255261088}, {66, 33, math.MaxUint64}, {200, 100, math.MaxUint64},
	} {
		if got := choose(c.n, c.k); got != c.want {
			t.Errorf("choose(%d, %d) = %d, want %d", c.n, c.k, got, c.want)
		}
	}
	if got := dchoose(10, 3); got != 120 {
		t.Errorf("dchoose(10, 3) = %g", got)
	}
}

// tr builds a transaction.
func tr(items ...tract.Item) []tract.Item { return items }

func TestTaslots(t *testing.T) {
	d := &Data{NItems: 5, Tracts: [][]tract.Item{tr(), tr(0), tr(0, 1, 2), tr(1, 2), tr(0, 1, 2, 3)}}
	// sizes 0,1,2,3,4 once each: cnts[k] = #size-k + sum_{i>k} C(i,k)
	want := []uint64{1, 1 + 2 + 3 + 4, 1 + 3 + 6, 1 + 4, 1, 0}
	if got := taslots(d); !slices.Equal(got, want) {
		t.Errorf("taslots = %v, want %v", got, want)
	}
}

// TestTaslots_saturates checks deviation U11: a transaction of 70 items
// has C(70, 35) > 2^64 subsets of size 35; upstream's size_t arithmetic
// wraps around, here the count saturates.
func TestTaslots_saturates(t *testing.T) {
	big := make([]tract.Item, 70)
	for i := range big {
		big[i] = tract.Item(i)
	}
	cnts := taslots(&Data{NItems: 71, Tracts: [][]tract.Item{big, big}})
	if cnts[1] != 140 || cnts[2] != 2*2415 || cnts[70] != 2 {
		t.Errorf("cnts[1], cnts[2], cnts[70] = %d, %d, %d", cnts[1], cnts[2], cnts[70])
	}
	for _, k := range []int{30, 35, 40} {
		if cnts[k] != math.MaxUint64 {
			t.Errorf("cnts[%d] = %d, want saturation", k, cnts[k])
		}
	}
}

func TestPartition(t *testing.T) {
	for _, c := range []struct {
		seed, cnt int64
		cpus      int
		want      []Part
	}{
		{7, 40, 1, []Part{{7, 40}}},
		{7, 40, 0, []Part{{7, 40}}},
		{7, 1, 8, []Part{{7, 1}}},
		{7, 40, 4, []Part{{7, 10}, {8, 10}, {9, 10}, {10, 10}}},
		{7, 5, 3, []Part{{7, 2}, {8, 2}, {9, 1}}},
		{7, 5, 4, []Part{{7, 2}, {8, 2}, {9, 1}}}, // upstream: same as -Z3
		{7, 3, 8, []Part{{7, 1}, {8, 1}, {9, 1}}},
		{-1, 4, 2, []Part{{4294967295, 2}, {0, 2}}},
		{0, math.MaxInt64, 2, []Part{{0, math.MaxInt64/2 + 1}, {1, math.MaxInt64 / 2}}},
	} {
		if got := Partition(c.seed, c.cnt, c.cpus); !slices.Equal(got, c.want) {
			t.Errorf("Partition(%d, %d, %d) = %v, want %v", c.seed, c.cnt, c.cpus, got, c.want)
		}
	}
}

// TestBorder checks the decision border with patch P3: up to the largest
// size with a signature, monotone, at least 1, also for a single size.
func TestBorder(t *testing.T) {
	spec := func(cells ...[3]int) *report.Spectrum {
		p := report.NewSpectrum(1, report.MaxSize, 1, 1000)
		for _, c := range cells {
			p.Add(c[0], tract.Supp(c[1]), uint64(c[2]))
		}
		return p
	}
	for _, c := range []struct {
		name string
		p    *report.Spectrum
		zmin int
		want string
	}{
		{"empty", spec(), 1, ""},
		{"single size", spec([3]int{2, 7, 1}), 2, "8\n"},
		{"below zmin only", spec([3]int{1, 7, 1}), 2, ""},
		{"monotone", spec([3]int{1, 300, 1}, [3]int{1, 290, 5}, [3]int{2, 40, 1}, [3]int{3, 41, 1}), 1, "301:42:42\n"},
		{"gap", spec([3]int{1, 9, 1}, [3]int{3, 2, 1}), 1, "10:3:3\n"},
		// upstream (U5) compares with the counter array of size 1, which
		// starts 32 below the first support; this border would be empty
		{"dense", spec([3]int{1, 311, 1}, [3]int{2, 247, 1}, [3]int{3, 120, 2}, [3]int{8, 2, 1}), 1, "312:248:121:3:3:3:3:3\n"},
	} {
		if got := FormatBorder(Border(c.p, c.zmin)); got != c.want {
			t.Errorf("%s: border %q, want %q", c.name, got, c.want)
		}
	}
}

// randData returns random transactions over n items (some empty, some
// repeated).
func randData(r *rand.Rand, n, cnt int) *Data {
	d := &Data{NItems: n}
	for range cnt {
		var t []tract.Item
		if len(d.Tracts) > 0 && r.IntN(5) == 0 {
			t = slices.Clone(d.Tracts[r.IntN(len(d.Tracts))])
		} else {
			for i := range n {
				if r.IntN(3) == 0 {
					t = append(t, tract.Item(i))
				}
			}
		}
		d.Tracts = append(d.Tracts, t)
	}
	sortTracts(d.Tracts)
	return d
}

// counts returns the item frequencies and the sorted sizes of ts.
func counts(ts [][]tract.Item, n int) ([]int64, []int) {
	f := make([]int64, n)
	var sizes []int
	for _, t := range ts {
		sizes = append(sizes, len(t))
		for _, i := range t {
			f[i]++
		}
	}
	slices.Sort(sizes)
	return f, sizes
}

// checkInvariants checks what every surrogate method promises,
// independently of upstream: the transaction sizes are kept and no
// transaction has a repeated item; Swap and Shuffle keep every item
// frequency; Shuffle keeps every column's multiset; Random keeps the item
// frequencies when its repair could fix every difference (not always
// possible).
func checkInvariants(t *testing.T, d *Data, m Method, ts [][]tract.Item) {
	t.Helper()
	f0, s0 := counts(d.Tracts, d.NItems)
	f1, s1 := counts(ts, d.NItems)
	if !slices.Equal(s0, s1) {
		t.Fatalf("method %d: sizes %v, source %v", m, s1, s0)
	}
	for _, tx := range ts {
		u := slices.Clone(tx)
		slices.Sort(u)
		if len(slices.Compact(u)) != len(tx) {
			t.Fatalf("method %d: repeated item in %v", m, tx)
		}
	}
	if (m == Swap || m == Shuffle || m == Identity) && !slices.Equal(f0, f1) {
		t.Fatalf("method %d: frequencies %v, source %v", m, f1, f0)
	}
	if m == Shuffle {
		for i := range len(d.Tracts[0]) {
			var a, b []tract.Item
			for k := range d.Tracts {
				a = append(a, d.Tracts[k][i])
				b = append(b, ts[k][i])
			}
			slices.Sort(a)
			slices.Sort(b)
			if !slices.Equal(a, b) {
				t.Fatalf("shuffle changed column %d", i)
			}
		}
	}
}

func TestChain_invariants(t *testing.T) {
	r := rand.New(rand.NewPCG(23, 1))
	for range 50 {
		d := randData(r, 2+r.IntN(8), 1+r.IntN(30))
		for _, m := range []Method{Identity, Random, Swap} {
			ch := NewChain(d, m, r.Uint32())
			for range 4 {
				checkInvariants(t, d, m, ch.Next())
			}
		}
		// a table: column c holds the items 3c..3c+2
		cols := 1 + r.IntN(4)
		td := &Data{NItems: 3 * cols}
		for range 2 + r.IntN(20) {
			var row []tract.Item
			for c := range cols {
				row = append(row, tract.Item(3*c+r.IntN(3)))
			}
			td.Tracts = append(td.Tracts, row)
		}
		ch := NewChain(td, Shuffle, r.Uint32())
		for range 3 {
			checkInvariants(t, td, Shuffle, ch.Next())
		}
	}
}

// FuzzChain runs every surrogate method on arbitrary small data and
// checks the invariants and that the source stays unchanged.
func FuzzChain(f *testing.F) {
	f.Add([]byte{3, 1, 2, 0, 7, 5}, uint32(1), uint8(1))
	f.Add([]byte{0, 0, 0}, uint32(7), uint8(2))
	f.Add([]byte{255, 1, 255, 2, 255, 4}, uint32(0), uint8(0))
	f.Fuzz(func(t *testing.T, raw []byte, seed uint32, m uint8) {
		const n = 8
		d := &Data{NItems: n}
		for _, b := range raw { // one transaction per byte: its bits
			var tx []tract.Item
			for i := range n {
				if b&(1<<i) != 0 {
					tx = append(tx, tract.Item(i))
				}
			}
			d.Tracts = append(d.Tracts, tx)
		}
		sortTracts(d.Tracts)
		method := Method(m % 3) // Identity, Random, Swap
		before := slices.Clone(d.Tracts)
		ch := NewChain(d, method, seed)
		for range 3 {
			checkInvariants(t, d, method, ch.Next())
		}
		if !slices.EqualFunc(before, d.Tracts, slices.Equal[[]tract.Item]) {
			t.Fatal("the chain modified its source")
		}
	})
}

// TestRandom_repairsFrequencies checks that the repair of tbg_random
// (with patch P4) restores the item frequencies of realistic data.
func TestRandom_repairsFrequencies(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 5))
	d := randData(r, 12, 200)
	f0, _ := counts(d.Tracts, d.NItems)
	ch := NewChain(d, Random, 9)
	for range 10 {
		if f1, _ := counts(ch.Next(), d.NItems); !slices.Equal(f0, f1) {
			t.Fatalf("frequencies %v, source %v", f1, f0)
		}
	}
}

// mineDirect mines d with the fpg pipeline and returns its spectrum.
func mineDirect(t *testing.T, d *Data, cfg fpg.Config) *report.Spectrum {
	t.Helper()
	smin, smax := fpg.Thresholds(cfg, tract.Supp(d.Len()))
	p := Spectrum(cfg, smin, smax)
	if err := Mine(context.Background(), d.Tracts, cfg, smin, p); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestMine_identity checks that the identity method gives the spectrum of
// the data itself, also for every target, and that the empty set is
// counted when no item is frequent and -m0 is given (upstream reports it
// from its unrecoded data).
func TestMine_identity(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	d := randData(r, 6, 40)
	for _, target := range []report.Target{report.TargetAll, report.TargetClosed, report.TargetMaximal, report.TargetGenerators} {
		cfg := fpg.DefaultConfig()
		cfg.Target, cfg.SMin = target, -3
		want := mineDirect(t, d, cfg)
		smin, smax := fpg.Thresholds(cfg, tract.Supp(d.Len()))
		got, err := RunChain(context.Background(), d, Identity, Part{Seed: 1, Count: 1}, cfg, smin, smax, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(slices.Collect(got.All()), slices.Collect(want.All())) || got.SigCount() == 0 {
			t.Errorf("target %d: identity spectrum differs", target)
		}
	}
	cfg := fpg.DefaultConfig()
	cfg.SMin, cfg.ZMin = -100, 0 // nothing frequent in 40 transactions
	p := mineDirect(t, d, cfg)
	if cells := slices.Collect(p.All()); len(cells) != 0 {
		t.Errorf("support above the total: %v", cells)
	}
	cfg.SMin = -40
	cfg.Target = report.TargetMaximal
	sparse := &Data{NItems: 3, Tracts: [][]tract.Item{tr(0), tr(1), tr(2), tr()}}
	p = mineDirect(t, sparse, fpg.Config(func() fpg.Config { c := cfg; c.SMin = -4; return c }()))
	if cells := slices.Collect(p.All()); !slices.Equal(cells, []report.Cell{{Size: 0, Supp: 4, Count: 1}}) {
		t.Errorf("no frequent item, -m0: %v", cells)
	}
}

// TestRunChain_cancel checks that a canceled context stops a chain.
func TestRunChain_cancel(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 3))
	d := randData(r, 8, 50)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := fpg.DefaultConfig()
	if _, err := RunChain(ctx, d, Swap, Part{Seed: 1, Count: 5}, cfg, 1, 50, nil); err != context.Canceled {
		t.Errorf("err = %v", err)
	}
}

// TestEstimate_seed checks that the estimation depends on the seed only
// through the sampling: the simple estimation (alpha <= 0) not at all,
// the complex one deterministically.
func TestEstimate_seed(t *testing.T) {
	r := rand.New(rand.NewPCG(4, 4))
	d := randData(r, 10, 120)
	est := func(alpha float64, seed uint32) []report.Cell {
		p := report.NewSpectrum(1, report.MaxSize, 2, tract.Supp(d.Len()))
		if err := Estimate(context.Background(), d, p, 1, EstimateConfig{Equiv: 1000, Alpha: alpha, Samples: 200, Seed: seed}); err != nil {
			t.Fatal(err)
		}
		return slices.Collect(p.All())
	}
	if a, b := est(0, 1), est(0, 2); !slices.Equal(a, b) || len(a) == 0 {
		t.Errorf("simple estimation depends on the seed or is empty")
	}
	if a, b := est(0.5, 1), est(0.5, 1); !slices.Equal(a, b) || len(a) == 0 {
		t.Errorf("complex estimation is not deterministic or is empty")
	}
	if a, b := est(0.5, 1), est(0.5, 2); slices.Equal(a, b) {
		t.Errorf("complex estimation does not depend on the seed")
	}
}

// TestEstimate_expectedSingletons checks the simple estimation against an
// independent computation: with equal item probabilities, the expected
// number of items with support c among the n items is
// n·Binomial(N, k/n)-like Poisson mass; here the total expected number of
// singletons over all supports must be close to n (every item has some
// support) when the minimum support is 1.
func TestEstimate_expectedSingletons(t *testing.T) {
	r := rand.New(rand.NewPCG(8, 8))
	d := randData(r, 10, 300)
	p := report.NewSpectrum(1, 1, 1, tract.Supp(d.Len()))
	if err := Estimate(context.Background(), d, p, 1, EstimateConfig{Equiv: 1000, Alpha: 0}); err != nil {
		t.Fatal(err)
	}
	var sum uint64
	for c := range p.All() {
		sum += c.Count
	}
	if got := float64(sum) / 1000; math.Abs(got-10) > 0.05 {
		t.Errorf("expected number of frequent single items %.3f, want 10", got)
	}
}

// TestTaslots_allItems checks deviation U17: transactions that contain
// every item count for the slots of the smaller sizes.
func TestTaslots_allItems(t *testing.T) {
	d := &Data{NItems: 3, Tracts: [][]tract.Item{tr(0, 1, 2), tr(0, 1, 2)}}
	if got, want := taslots(d), []uint64{0, 6, 6, 2}; !slices.Equal(got, want) {
		t.Errorf("taslots = %v, want %v (upstream: [0 0 0 2])", got, want)
	}
}

// TestEstimate_cancel checks that a canceled context stops the estimation.
func TestEstimate_cancel(t *testing.T) {
	d := randData(rand.New(rand.NewPCG(6, 6)), 10, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, a := range []float64{0, 0.5} {
		p := report.NewSpectrum(1, report.MaxSize, 1, tract.Supp(d.Len()))
		if err := Estimate(ctx, d, p, 1, EstimateConfig{Equiv: 10, Alpha: a, Samples: 10}); err != context.Canceled {
			t.Errorf("alpha %g: err %v", a, err)
		}
	}
}
