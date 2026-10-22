package psp

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// benchData is 4000 transactions over 43 items (the data of the
// performance comparison with the C program, issue #23 D12).
func benchData() *Data {
	r := rand.New(rand.NewPCG(23, 23))
	d := &Data{NItems: 43}
	for range 4000 {
		var t []tract.Item
		for i := range 40 {
			if r.Float64() < 0.08+0.004*float64(i) {
				t = append(t, tract.Item(i))
			}
		}
		if r.Float64() < 0.3 {
			t = append(t, 40, 41, 42)
		}
		d.Tracts = append(d.Tracts, t)
	}
	sortTracts(d.Tracts)
	return d
}

func benchChain(b *testing.B, m Method) {
	d := benchData()
	cfg := fpg.DefaultConfig()
	cfg.SMin = -60
	smin, smax := fpg.Thresholds(cfg, tract.Supp(d.Len()))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := RunChain(context.Background(), d, m, Part{Seed: 1, Count: 10}, cfg, smin, smax, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRunChain_Swap and BenchmarkRunChain_Random measure 10
// surrogates (generation and mining) per operation.
func BenchmarkRunChain_Swap(b *testing.B)   { benchChain(b, Swap) }
func BenchmarkRunChain_Random(b *testing.B) { benchChain(b, Random) }

// BenchmarkChain_Swap measures the generation alone (10 surrogates).
func BenchmarkChain_Swap(b *testing.B) {
	d := benchData()
	for b.Loop() {
		ch := NewChain(d, Swap, 1)
		for range 10 {
			ch.Next()
		}
	}
}
