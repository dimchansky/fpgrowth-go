package psp

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// bruteSpectrum is the pattern spectrum of the item sets of the brute-force
// oracle (internal/oracle, the definitions applied literally).
func bruteSpectrum(t *testing.T, txs []oracle.Transaction, target oracle.Target, smin tract.Supp, zmin, zmax int) []report.Cell {
	t.Helper()
	db, err := oracle.New(txs, oracle.Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := oracle.NewSetParams(target, smin)
	p.MinSize, p.MaxSize = zmin, zmax
	sets, err := db.Mine(p)
	if err != nil {
		t.Fatal(err)
	}
	cnt := map[[2]int64]uint64{}
	for _, s := range sets {
		cnt[[2]int64{int64(len(s.Items)), int64(s.Supp)}]++
	}
	var cells []report.Cell
	for k, v := range cnt {
		cells = append(cells, report.Cell{Size: int(k[0]), Supp: tract.Supp(k[1]), Count: v})
	}
	slices.SortFunc(cells, func(a, b report.Cell) int {
		if a.Size != b.Size {
			return a.Size - b.Size
		}
		return int(a.Supp - b.Supp)
	})
	return cells
}

// TestIdentity_bruteForce checks the identity method (-gi, one data set)
// against the brute-force definitions on small random data, for every
// target, variant, size range and optimization, also where the
// differential test avoids a configuration because upstream deviates
// (M1 -At, M4 and M9 maximal, M5 -n with closed/maximal, U25 generators
// with -m >= 2): the data preparation of fpgpsp (recoding, the -m filter,
// arrays of records) and the per-surrogate mining must give the spectrum
// of the definitions.
func TestIdentity_bruteForce(t *testing.T) {
	r := rand.New(rand.NewPCG(25, 25))
	targets := []struct {
		rep report.Target
		ora oracle.Target
	}{
		{report.TargetAll, oracle.Frequent}, {report.TargetClosed, oracle.Closed},
		{report.TargetMaximal, oracle.Maximal}, {report.TargetGenerators, oracle.Generators},
	}
	algos := []fpg.Algorithm{fpg.AlgoSimple, fpg.AlgoComplex, fpg.AlgoSingle, fpg.AlgoTopDown}
	checked := 0
	for i := range 600 {
		n := 2 + r.IntN(8)
		var txs []oracle.Transaction
		universal := r.IntN(4) == 0 // an item in every transaction (M1, M4)
		for range 1 + r.IntN(25) {
			var items []string
			if universal {
				items = append(items, "u")
			}
			for k := range n {
				if r.IntN(3) == 0 {
					items = append(items, fmt.Sprintf("i%d", k))
				}
			}
			tx := oracle.Transaction{Items: items, Weight: 1}
			for range 1 + r.IntN(2) { // some duplicates
				txs = append(txs, tx)
			}
		}
		tg := targets[r.IntN(len(targets))]
		cfg := fpg.DefaultConfig()
		cfg.Target = tg.rep
		cfg.Algo = algos[r.IntN(len(algos))]
		cfg.SMin = -float64(1 + r.IntN(4))
		cfg.ZMin = r.IntN(4)
		if r.IntN(2) == 0 {
			cfg.ZMax = cfg.ZMin + r.IntN(3)
		}
		if r.IntN(3) == 0 {
			cfg.Mode &^= fpg.ModePerfect
		}
		if r.IntN(3) == 0 {
			cfg.Mode &^= fpg.ModeReorder
		}
		cfg.Pack = []int{0, 3, 16}[r.IntN(3)]
		scan := func(yield func([]string, tract.Supp) bool) error {
			for _, tx := range txs {
				if !yield(tx.Items, 1) {
					break
				}
			}
			return nil
		}
		ib := tract.NewItemBase[string]()
		if err := ib.Count(scan); err != nil {
			t.Fatal(err)
		}
		m, d, err := Prepare(ib, scan, cfg)
		if errors.Is(err, fpg.ErrNoItems) {
			continue // fpgpsp: "no (frequent) items found"
		}
		if err != nil {
			t.Fatal(err)
		}
		spec, err := RunChain(context.Background(), d, Identity, Part{Seed: 1, Count: 1}, cfg,
			m.MinSupport(), m.MaxSupport(), nil)
		if err != nil {
			t.Fatal(err)
		}
		zmax := cfg.ZMax
		if zmax >= fpg.MaxSize {
			zmax = oracle.NoMaxSize
		}
		want := bruteSpectrum(t, txs, tg.ora, -tract.Supp(cfg.SMin), cfg.ZMin, zmax)
		if got := slices.Collect(spec.All()); !slices.Equal(got, want) {
			t.Fatalf("case %d (target %v, algo %v, -s%g -m%d -n%d, mode %#x, pack %d): spectrum\n%v\nbrute force\n%v",
				i, tg.ora, cfg.Algo, cfg.SMin, cfg.ZMin, cfg.ZMax, int(cfg.Mode), cfg.Pack, got, want)
		}
		checked++
	}
	if checked < 400 {
		t.Errorf("only %d cases checked", checked)
	}
}

// TestIdentity_generatorsMinSize is the regression test of U25: with -m3
// the generators of size 3 are found (upstream's -m filter empties the
// transactions of fewer items and finds none).
func TestIdentity_generatorsMinSize(t *testing.T) {
	recs := [][]string{{"a", "b", "c"}, {"a"}, {"b"}, {"c"}, {"a", "b"}, {"a", "c"}, {"b", "c"}, {"a", "b", "c", "d"}}
	scan := func(yield func([]string, tract.Supp) bool) error {
		for _, t := range recs {
			if !yield(t, 1) {
				break
			}
		}
		return nil
	}
	ib := tract.NewItemBase[string]()
	if err := ib.Count(scan); err != nil {
		t.Fatal(err)
	}
	cfg := fpg.DefaultConfig()
	cfg.Target, cfg.SMin, cfg.ZMin, cfg.ZMax = report.TargetGenerators, -1, 3, 3
	m, d, err := Prepare(ib, scan, cfg)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := RunChain(context.Background(), d, Identity, Part{Seed: 1, Count: 1}, cfg, m.MinSupport(), m.MaxSupport(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var txs []oracle.Transaction
	for _, t := range recs {
		txs = append(txs, oracle.Transaction{Items: t, Weight: 1})
	}
	want := bruteSpectrum(t, txs, oracle.Generators, 1, 3, 3)
	if got := slices.Collect(spec.All()); !slices.Equal(got, want) || len(want) == 0 {
		t.Errorf("generators of size 3: %v, brute force %v", got, want)
	}
}
