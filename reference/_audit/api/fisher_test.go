package api

import (
	"context"
	"testing"
	"time"

	fp "github.com/dimchansky/fpgrowth-go"
)

func TestFisherCancel(t *testing.T) {
	src := fp.FromSlice([]fp.Transaction[string]{
		{Items: []string{"a", "b"}, Weight: 30_000_000},
		{Items: []string{"a"}, Weight: 20_000_000},
		{Items: []string{"b"}, Weight: 20_000_000},
		{Items: []string{"c"}, Weight: 30_000_000},
	})
	ctx, cancel := context.WithCancel(context.Background())
	var at time.Time
	go func() { time.Sleep(100 * time.Millisecond); at = time.Now(); cancel() }()
	start := time.Now()
	_, err := fp.MineRules(ctx, src, fp.RuleOptions[string]{MinSupport: fp.Count(1), Evaluation: &fp.Evaluation{Measure: fp.FisherChi2}}, nil)
	t.Logf("fisher total=%v latency=%v err=%v", time.Since(start), time.Since(at), err)
}
