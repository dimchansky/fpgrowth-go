package fpg

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Tests of the perfect-extension guard (issue #21): the miners skip the
// chain walks when report.Reporter.PexUseful is false, and the reporter
// skips the extensions of sets with zmax items. The results must equal
// those with the guard off (report.Reporter.SetPexGuard).

// t4Txs is the T4 shape of the performance audit: 17 lines of a chain of
// n items, then n+10 lines of one more item.
func t4Txs(n int) []oracle.Transaction {
	chain := make([]string, n)
	for i := range chain {
		chain[i] = "c" + strconv.Itoa(i)
	}
	txs := make([]oracle.Transaction, 0, 17+n+10)
	for range 17 {
		txs = append(txs, oracle.Transaction{Items: chain, Weight: 1})
	}
	for range n + 10 {
		txs = append(txs, oracle.Transaction{Items: []string{"x"}, Weight: 1})
	}
	return txs
}

// pexSink records the reported sets and the reporter's number of perfect
// extensions at each report.
type pexSink struct {
	rep    *report.Reporter
	sets   int
	maxPex int
	sumPex int
}

func (s *pexSink) ItemSet(report.ItemSet) error {
	s.sets++
	n := s.rep.PexCount()
	s.maxPex = max(s.maxPex, n)
	s.sumPex += n
	return nil
}

func (s *pexSink) Rule(report.Rule) error { return nil }

// mineT4 mines the T4 shape with -s-17 -n1 and returns the sink.
func mineT4(t *testing.T, n int, cfg Config, guard bool) *pexSink {
	t.Helper()
	cfg.SMin, cfg.ZMin, cfg.ZMax = -17, 1, 1
	m, err := New[string](cfg)
	if err != nil {
		t.Fatal(err)
	}
	ib := tract.NewItemBase[string]()
	scan := scanOf(t4Txs(n))
	if err := ib.Count(scan); err != nil {
		t.Fatal(err)
	}
	if err := m.Data(ib, scan); err != nil {
		t.Fatal(err)
	}
	rep, err := m.Reporter()
	if err != nil {
		t.Fatal(err)
	}
	rep.SetPexGuard(guard)
	sk := &pexSink{rep: rep}
	rep.SetSink(sk)
	if err := m.Mine(context.Background()); err != nil {
		t.Fatal(err)
	}
	return sk
}

// TestMine_T4NoPerfectExtensions is the scaling regression test of issue
// #21 (bounded work, no timing): on the T4 shape with a maximum size of 1
// no chain item may collect its predecessors as perfect extensions, in
// every variant with a chain walk (the 16-items machine may add up to 16
// of its own). Without the guard each item at depth d has d of them,
// n²/2 in total.
func TestMine_T4NoPerfectExtensions(t *testing.T) {
	const n = 600
	for _, v := range []struct {
		name string
		algo Algorithm
		pack int
	}{
		{"Ac", AlgoComplex, 16},   // recCmplx
		{"Ac-l0", AlgoComplex, 0}, // recCmplx without the machine
		{"As-l0", AlgoSimple, 0},  // recSimple
		{"As", AlgoSimple, 16},    // recSmp16
		{"Ad-l0", AlgoSingle, 0},  // recSingle
		{"Ad", AlgoSingle, 16},    // recSingle with packed items
		{"At", AlgoTopDown, 0},    // no chain walk
	} {
		t.Run(v.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Algo, cfg.Pack = v.algo, v.pack
			got := mineT4(t, n, cfg, true)
			// The 16-items machine (packed items of -As and -Ad) adds
			// the perfect extensions among its at most 16 items itself
			// (AddPex stays an exact port): a bound independent of n.
			maxPex, sumPex := 0, 0
			if v.pack > 0 && v.algo != AlgoComplex {
				maxPex, sumPex = 16, 16*16
			}
			if got.sets != n+1 || got.maxPex > maxPex || got.sumPex > sumPex {
				t.Errorf("%d sets, perfect extensions at a report: max %d, sum %d; want %d sets, <= %d, <= %d",
					got.sets, got.maxPex, got.sumPex, n+1, maxPex, sumPex)
			}
			off := mineT4(t, n, cfg, false)
			if off.sets != n+1 {
				t.Errorf("guard off: %d sets, want %d", off.sets, n+1)
			}
			if v.algo != AlgoTopDown && off.sumPex < n*(n-1)/4 {
				// sensitivity of the test: the walks do collect them
				t.Errorf("guard off: sum of perfect extensions %d, want >= %d", off.sumPex, n*(n-1)/4)
			}
		})
	}
}

// recSink records everything a sink receives, with bit-exact floats.
type recSink struct{ b strings.Builder }

func (s *recSink) ItemSet(is report.ItemSet) error {
	fmt.Fprintf(&s.b, "%v %d %x %x\n", is.Items, is.Supp, math.Float64bits(is.Weight), math.Float64bits(is.Eval))
	return nil
}

func (s *recSink) Rule(r report.Rule) error {
	fmt.Fprintf(&s.b, "R %d %v %d %d %d %x\n", r.Head, r.Body, r.Supp, r.BodySupp, r.HeadSupp, math.Float64bits(r.Eval))
	return nil
}

// guardRun mines c with cfg and returns a transcript of every result:
// the error, Count, Stats, Saturated, the pattern spectrum and the sets
// or rules in order.
func guardRun(c propCase, cfg Config, border []tract.Supp, nilSink, spec, guard bool) string {
	ib := tract.NewItemBase[string]()
	m, err := New[string](cfg)
	if err != nil {
		return "new: " + err.Error()
	}
	scan := scanOf(c.txs)
	if err := ib.Count(scan); err != nil {
		return "count: " + err.Error()
	}
	if err := m.Data(ib, scan); err != nil {
		return "data: " + err.Error()
	}
	rep, err := m.Reporter()
	if err != nil {
		return "reporter: " + err.Error()
	}
	rep.SetPexGuard(guard)
	for size, s := range border {
		rep.SetBorder(size, s)
	}
	if spec {
		rep.AddSpectrum()
	}
	var sk *recSink
	if !nilSink {
		sk = &recSink{}
		rep.SetSink(sk)
	}
	err = m.Mine(context.Background())
	var b strings.Builder
	fmt.Fprintf(&b, "err=%v count=%d stats=%v sat=%v\n", err, rep.Count(), rep.Stats(), rep.Saturated())
	if p := rep.Spectrum(); p != nil {
		for cell := range p.All() {
			fmt.Fprintf(&b, "psp %v\n", cell)
		}
		fmt.Fprintf(&b, "psp total %d sig %d sat %v\n", p.Total(), p.SigCount(), p.Saturated())
	}
	if sk != nil {
		b.WriteString(sk.b.String())
	}
	return b.String()
}

// TestMine_PexGuardSame compares the results with and without the
// perfect-extension guard on random databases, for every variant and
// target, random size ranges (zmin > zmax and zmax 0 included), the
// LogRatio evaluation, rule-based evaluation, borders, the pattern
// spectrum and count-only mode. FPG_PEXGUARD_CASES sets the number of
// databases (default 40; each gives 11 variants x 5 targets).
func TestMine_PexGuardSame(t *testing.T) {
	cases := 40
	if testing.Short() {
		cases = 8
	}
	if s := os.Getenv("FPG_PEXGUARD_CASES"); s != "" {
		var err error
		if cases, err = strconv.Atoi(s); err != nil {
			t.Fatal(err)
		}
	}
	first := uint64(0)
	if s := os.Getenv("FPG_PEXGUARD_SEED"); s != "" {
		var err error
		if first, err = strconv.ParseUint(s, 10, 64); err != nil {
			t.Fatal(err)
		}
	}
	allTargets := []report.Target{report.TargetAll, report.TargetClosed, report.TargetMaximal,
		report.TargetGenerators, report.TargetRules}
	n, limited := 0, 0
	for seed := first; seed < first+uint64(cases); seed++ {
		c, err := genCase(seed+21000, 8)
		if err != nil {
			t.Fatal(err)
		}
		r := rand.New(rand.NewPCG(seed, 21))
		for _, v := range variants {
			for _, tg := range allTargets {
				cfg := c.config(v, tg)
				cfg.ZMin = r.IntN(4)
				cfg.ZMax = []int{0, 1, 1, 2, 2, 3, 4, MaxSize}[r.IntN(8)]
				switch r.IntN(6) {
				case 0:
					cfg.Eval = EvalLogRatio
					cfg.Thresh = float64(r.IntN(200) - 100)
				case 1:
					cfg.Eval = 1 + r.IntN(EvalLogRatio-1) // rule-based (-e, -a, -i)
					cfg.Agg = r.IntN(AggAvg + 1)
					cfg.InvalidateBelowExpected = r.IntN(2) == 0
					cfg.Thresh = float64(r.IntN(100))
				}
				var border []tract.Supp
				if r.IntN(4) == 0 {
					border = []tract.Supp{0, tract.Supp(1 + r.IntN(3)), tract.Supp(1 + r.IntN(4)), tract.Supp(r.IntN(5))}
				}
				nilSink := r.IntN(3) == 0
				spec := r.IntN(2) == 0
				want := guardRun(c, cfg, border, nilSink, spec, false)
				got := guardRun(c, cfg, border, nilSink, spec, true)
				n++
				if cfg.ZMax < MaxSize {
					limited++
				}
				if got != want {
					t.Fatalf("seed %d %s %s border=%v nilSink=%v spectrum=%v\nguard off:\n%s\nguard on:\n%s",
						seed, v.name, describe(cfg), border, nilSink, spec, want, got)
				}
			}
		}
	}
	t.Logf("%d comparisons, %d with a maximum size", n, limited)
}
