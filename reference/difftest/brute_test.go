package difftest

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
)

// txsOf parses "a b|a|b c" (weights 1) into transactions.
func txsOf(s string) []transaction {
	var txs []transaction
	for _, r := range strings.Split(s, "|") {
		txs = append(txs, transaction{items: strings.Fields(r), weight: 1})
	}
	return txs
}

func fmtSets(sets []bfSet) []string {
	out := make([]string, len(sets))
	for i, s := range sets {
		out[i] = fmt.Sprintf("%s:%d", strings.Join(s.items, ""), s.supp)
	}
	slices.Sort(out)
	return out
}

func TestBF_Sets(t *testing.T) {
	t.Parallel()
	all := bfSetParams{smax: math.MaxInt64, zmin: 1, zmax: math.MaxInt}
	with := func(p bfSetParams, f func(*bfSetParams)) bfSetParams { f(&p); return p }
	tests := []struct {
		name string
		data string
		sel  map[string]bool
		p    bfSetParams
		want []string
	}{
		// docs/upstream/fpgrowth-core.md B4: the maximal set of the
		// top-level perfect extension
		{"maximal pex", "a|a b|a c|a", nil, with(all, func(p *bfSetParams) { p.target, p.smin = 'm', 2 }), []string{"a:4"}},
		// B3: {a, b} is a generator of size 2 ({b, c} is not: supp(c) = 1)
		{"generators m2", "a|a b|a b|b c", nil, with(all, func(p *bfSetParams) { p.target, p.smin, p.zmin = 'g', 1, 2 }),
			[]string{"ab:2"}},
		{"closed", "a b|a b|a", nil, with(all, func(p *bfSetParams) { p.target, p.smin = 'c', 1 }), []string{"a:3", "ab:2"}},
		{"frequent zmax border", "a b|a b|a|b c", nil, with(all, func(p *bfSetParams) {
			p.target, p.smin, p.zmax, p.border = 's', 1, 2, []int64{0, 0, 2}
		}), []string{"a:3", "ab:2", "b:3", "c:1"}},
		{"empty set closed", "a|b", nil, with(all, func(p *bfSetParams) { p.target, p.smin, p.zmin = 'c', 1, 0 }),
			[]string{":2", "a:1", "b:1"}},
		{"selection keeps the total", "a b|b|c", map[string]bool{"a": true, "c": true, "x": true},
			with(all, func(p *bfSetParams) { p.target, p.smin, p.zmin = 's', 1, 0 }), []string{":3", "a:1", "c:1"}},
		{"maximum support", "a b|a|a", nil, with(all, func(p *bfSetParams) { p.target, p.smin, p.smax = 's', 1, 2 }),
			[]string{"ab:1", "b:1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, err := newBF(txsOf(tt.data), tt.sel)
			if err != nil {
				t.Fatal(err)
			}
			if got := fmtSets(db.sets(tt.p)); !slices.Equal(got, tt.want) {
				t.Errorf("sets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBF_Rules(t *testing.T) {
	t.Parallel()
	db, err := newBF(txsOf("a b|a b|a|b c"), nil)
	if err != nil {
		t.Fatal(err)
	}
	fmtRules := func(rs []bfRule) []string {
		out := make([]string, len(rs))
		for i, r := range rs {
			out[i] = fmt.Sprintf("%s<-%s:%d/%d/%d", r.head, strings.Join(r.body, ""), r.supp, r.bsup, r.hs)
		}
		slices.Sort(out)
		return out
	}
	base := bfRuleParams{smin: 2, smax: math.MaxInt64, conf: 0.6, zmin: 1, zmax: math.MaxInt}
	tests := []struct {
		name string
		p    bfRuleParams
		want []string
	}{
		{"default", base, []string{"a<-:3/4/3", "a<-b:2/3/3", "b<-:3/4/3", "b<-a:2/3/3"}},
		{"original support", func() bfRuleParams { p := base; p.orig, p.conf = true, 0; return p }(),
			[]string{"a<-:3/4/3", "a<-b:2/3/3", "b<-:3/4/3", "b<-a:2/3/3"}},
		{"max support on the body", func() bfRuleParams { p := base; p.smax = 3; return p }(),
			[]string{"a<-b:2/3/3", "b<-a:2/3/3"}},
		{"head-only a", func() bfRuleParams { p := base; p.appear = map[string]appearance{"a": appHead}; return p }(),
			[]string{"a<-:3/4/3", "a<-b:2/3/3", "b<-:3/4/3"}}, // a is never in a body
		{"size 2", func() bfRuleParams { p := base; p.zmin = 2; return p }(), []string{"a<-b:2/3/3", "b<-a:2/3/3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fmtRules(db.rules(tt.p)); !slices.Equal(got, tt.want) {
				t.Errorf("rules = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestThresholds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  int64
		want int64
	}{
		{"-s50 of 10", absMinSupp(50, 10), 5},
		{"-s33.3 of 3", absMinSupp(33.3, 3), 1},
		{"-s-3", absMinSupp(-3, 10), 3},
		{"-s-2.5", absMinSupp(-2.5, 10), 3},
		{"-S100 of 7", absMaxSupp(100, 7), 7},
		{"-S-2.5", absMaxSupp(-2.5, 10), 2},
		{"rule set minimum", ruleSetMinSupp(5, 0.8), 4},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
	if b := absBorder([]float64{50, -1}, 2, 10); !slices.Equal(b, []int64{0, 0, 5, 1}) {
		t.Errorf("absBorder = %v", b)
	}
}
