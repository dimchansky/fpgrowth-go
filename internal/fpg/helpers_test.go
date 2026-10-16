package fpg

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// scanOf returns a replayable scan over txs.
func scanOf(txs []oracle.Transaction) tract.ScanFunc[string] {
	return func(yield func([]string, tract.Supp) bool) error {
		for _, t := range txs {
			if !yield(t.Items, t.Weight) {
				return nil
			}
		}
		return nil
	}
}

// collectSink records the reported item sets with item names.
type collectSink struct {
	ib    *tract.ItemBase[string]
	sets  []oracle.Set
	lines []string // "name name [supp]" in report order
	// failAt, if > 0, makes the failAt-th item set fail with errSink.
	failAt int
	cnt    int
	// onSet, if set, is called for every item set.
	onSet func()
}

var errSink = errors.New("test sink failure")

func (s *collectSink) ItemSet(is report.ItemSet) error {
	s.cnt++
	if s.failAt > 0 && s.cnt == s.failAt {
		return errSink
	}
	if s.onSet != nil {
		s.onSet()
	}
	names := make([]string, len(is.Items))
	for i, it := range is.Items {
		names[i] = s.ib.ID(it)
	}
	s.sets = append(s.sets, oracle.Set{Items: oracle.NewItemSet(names...), Supp: is.Supp})
	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(n)
	}
	b.WriteString(" [")
	b.WriteString(strconv.FormatInt(int64(is.Supp), 10))
	b.WriteByte(']')
	s.lines = append(s.lines, b.String())
	return nil
}

func (s *collectSink) Rule(report.Rule) error { return errors.New("unexpected rule") }

// runResult is the outcome of one mining run.
type runResult struct {
	sink  *collectSink
	miner *Miner[string]
	rep   *report.Reporter
}

// runOpts are test-only reporter settings (CLI options outside the core).
type runOpts struct {
	border []tract.Supp // absolute border by size (index = size)
	sink   *collectSink
	ctx    context.Context
}

// run mines scan with cfg through the step API.
func run(scan tract.ScanFunc[string], cfg Config, o runOpts) (runResult, error) {
	ib := tract.NewItemBase[string]()
	m, err := New[string](cfg)
	if err != nil {
		return runResult{}, err
	}
	if err := ib.Count(scan); err != nil {
		return runResult{}, err
	}
	sink := o.sink
	if sink == nil {
		sink = &collectSink{}
	}
	sink.ib = ib
	res := runResult{sink: sink, miner: m}
	if err := m.Data(ib, scan); err != nil {
		return res, err
	}
	rep, err := m.Reporter()
	if err != nil {
		return res, err
	}
	res.rep = rep
	for size, supp := range o.border {
		rep.SetBorder(size, supp)
	}
	rep.SetSink(sink)
	ctx := o.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return res, m.Mine(ctx)
}

// variant is one algorithm configuration under test.
type variant struct {
	name  string
	algo  Algorithm
	pack  int
	clear Mode // mode flags to clear
}

var variants = []variant{
	{name: "s-l0", algo: AlgoSimple, pack: 0},
	{name: "s-l3", algo: AlgoSimple, pack: 3},
	{name: "s-l16", algo: AlgoSimple, pack: 16},
	{name: "c", algo: AlgoComplex, pack: 16},
	{name: "c-j", algo: AlgoComplex, pack: 16, clear: ModeReorder},
	{name: "c-l0", algo: AlgoComplex, pack: 0},
	{name: "c-j-l0", algo: AlgoComplex, pack: 0, clear: ModeReorder},
	{name: "d-l0", algo: AlgoSingle, pack: 0},
	{name: "d-l3", algo: AlgoSingle, pack: 3},
	{name: "d-l16", algo: AlgoSingle, pack: 16},
	{name: "t", algo: AlgoTopDown, pack: 0},
}

var targets = []struct {
	name   string
	target report.Target
	oracle oracle.Target
}{
	{"s", report.TargetAll, oracle.Frequent},
	{"c", report.TargetClosed, oracle.Closed},
	{"m", report.TargetMaximal, oracle.Maximal},
	{"g", report.TargetGenerators, oracle.Generators},
}

func describe(cfg Config) string {
	return fmt.Sprintf("-t%#x -A%v -l%d -s%g -S%g -m%d -n%d mode=%#x -q%d",
		int(cfg.Target), cfg.Algo, cfg.Pack, cfg.SMin, cfg.SMax, cfg.ZMin, cfg.ZMax,
		int(cfg.Mode), cfg.Sort)
}
