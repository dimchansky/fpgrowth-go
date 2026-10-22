package psp

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tabread"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// surrCase is a line of testdata/surr/cases.txt.
type surrCase struct {
	name, data   string
	method       Method
	seed         uint32
	cnt, zmin    int
	supp         float64
	target, algo byte
	flags        string
}

func readSurrCases(t *testing.T) []surrCase {
	t.Helper()
	b, err := os.ReadFile("testdata/surr/cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	var cs []surrCase
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if len(f) != 10 {
			t.Fatalf("bad case line %q", line)
		}
		c := surrCase{name: f[0], data: f[9], target: f[6][0], algo: f[7][0], flags: f[8]}
		c.method = Method(strings.IndexByte("irps", f[1][0]))
		seed, _ := strconv.ParseUint(f[2], 10, 32)
		c.seed = uint32(seed)
		c.cnt, _ = strconv.Atoi(f[3])
		c.zmin, _ = strconv.Atoi(f[4])
		c.supp, _ = strconv.ParseFloat(f[5], 64)
		cs = append(cs, c)
	}
	return cs
}

// config is the miner configuration of fpgpsp's options for the case.
func (c surrCase) config() fpg.Config {
	cfg := fpg.DefaultConfig()
	cfg.Target = map[byte]report.Target{'s': report.TargetAll, 'c': report.TargetClosed,
		'm': report.TargetMaximal, 'g': report.TargetGenerators}[c.target]
	cfg.Algo = map[byte]fpg.Algorithm{'s': fpg.AlgoSimple, 'c': fpg.AlgoComplex,
		'd': fpg.AlgoSingle, 't': fpg.AlgoTopDown}[c.algo]
	cfg.SMin = c.supp
	cfg.ZMin = c.zmin
	if strings.Contains(c.flags, "x") {
		cfg.Mode &^= fpg.ModePerfect
	}
	if strings.Contains(c.flags, "i") {
		cfg.Mode &^= fpg.ModeReorder
	}
	if strings.Contains(c.flags, "u") {
		cfg.Mode &^= fpg.ModeTail
	}
	if strings.Contains(c.flags, "0") {
		cfg.Pack = 0
	}
	return cfg
}

// dumpChain produces the output of reference/harness/fpgpsp/surrdump for
// the case.
func dumpChain(t *testing.T, c surrCase) string {
	t.Helper()
	var sb strings.Builder
	scan := tabread.ScanFile(filepath.Join("testdata", "surr", c.data), tabread.DefaultConfig())
	ib := tract.NewItemBase[string]()
	if err := ib.Count(scan); err != nil {
		t.Fatal(err)
	}
	var d *Data
	if c.method == Shuffle {
		recs, err := Records(ib, scan)
		if err != nil {
			t.Fatal(err)
		}
		if !IsTable(recs, ib.Len()) {
			return "notable\n"
		}
		d = TableData(recs, ib.Len(), c.zmin)
	} else {
		var err error
		if _, d, err = Prepare(ib, scan, c.config()); err != nil {
			return fmt.Sprintf("error %v\n", err)
		}
	}
	dump := func(ts [][]tract.Item) {
		for _, tr := range ts {
			sb.WriteString("t")
			for _, i := range tr {
				fmt.Fprintf(&sb, " %d", i)
			}
			sb.WriteString("\n")
		}
	}
	fmt.Fprintf(&sb, "prep %d\n", d.Len())
	dump(d.Tracts)
	for i := range ib.Len() {
		fmt.Fprintf(&sb, "item %d %s\n", i, ib.ID(tract.Item(i)))
	}
	before := slices.Clone(d.Tracts)
	ch := NewChain(d, c.method, c.seed)
	for k := 1; k <= c.cnt; k++ {
		fmt.Fprintf(&sb, "set %d\n", k)
		dump(ch.Next())
	}
	if !slices.EqualFunc(before, d.Tracts, slices.Equal[[]tract.Item]) {
		t.Errorf("%s: the chain modified its source", c.name)
	}
	return sb.String()
}

// TestChain_matchesC compares the surrogate chains with the dumps of the
// C harness (reference/harness/fpgpsp/surrdump, the upstream generators
// with the oracle patches): the prepared array, the item codes and every
// surrogate, transaction by transaction.
func TestChain_matchesC(t *testing.T) {
	for _, c := range readSurrCases(t) {
		t.Run(c.name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "surr", c.name+".c.out"))
			if err != nil {
				t.Fatal(err)
			}
			got := dumpChain(t, c)
			if got != string(want) {
				gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
				for i := range min(len(gl), len(wl)) {
					if gl[i] != wl[i] {
						t.Fatalf("line %d: Go %q, C %q", i+1, gl[i], wl[i])
					}
				}
				t.Fatalf("Go has %d lines, C %d", len(gl), len(wl))
			}
		})
	}
}
