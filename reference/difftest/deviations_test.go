package difftest

import (
	"slices"
	"testing"
)

func TestCaseDeviations(t *testing.T) {
	t.Parallel()
	base := func(data string, target, algo byte, f func(*tcase)) *tcase {
		c := &tcase{
			txs: txsOf(data), target: target, algo: algo, pack: -1, sortMode: 2,
			sArg: -1, zmin: 1, zmax: -1, conf: 80, eval: 'x', agg: 'x',
			info: infoFormat{open: " (", close: ")", fields: []infoField{{code: 'a'}}},
			sep:  " ", imp: " <- ",
		}
		if f != nil {
			f(c)
		}
		return c
	}
	const pexData = "x a b|x a|x b c|x a b c|x c" // core B1: x is a top-level perfect extension
	const plain = "a b|a|b c|a b c|c"
	tests := []struct {
		name string
		c    *tcase
		id   string
		want bool
	}{
		{"M1 top-level perfect extension", base(pexData, 's', 't', nil), "M1", true},
		{"M1 not with -x", base(pexData, 's', 't', func(c *tcase) { c.noPerfect = true }), "M1", false},
		{"M1 not without eliminated items", base(plain, 's', 't', nil), "M1", false},
		{"M1 zmin filter makes an item infrequent", base("a b|a c|b c|d|d|d", 's', 't', func(c *tcase) {
			c.zmin, c.sArg = 2, 10 // core B1 reproducer: d is only in short transactions
		}), "M1", true},
		{"M1 not for other variants", base(pexData, 's', 'c', nil), "M1", false},
		{"M5 depth limit cuts frequent sets", base(plain, 'c', 'c', func(c *tcase) { c.zmax = 2 }), "M5", true},
		{"M5 not when no larger set is frequent", base(plain, 'c', 'c', func(c *tcase) { c.zmax = 3 }), "M5", false},
		{"O3 fast path with a spectrum", base(plain, 's', 'c', func(c *tcase) { c.spectrum = true }), "O3", true},
		{"O3 fast path with a perfect extension", base(pexData, 's', 'c', nil), "O3", true},
		{"O3 fast path without effect", base(plain, 's', 'c', nil), "O3", false},
		{"O3 not without the fast format", base(pexData, 's', 'c', func(c *tcase) { c.info.open = " [" }), "O3", false},
		{"O3 not with -n", base(pexData, 's', 'c', func(c *tcase) { c.zmax = 5 }), "O3", false},
		{"O4 count-only -n with larger frequent sets", base(plain, 'm', 'c', func(c *tcase) {
			c.countOnly, c.zmax = true, 1
		}), "O4", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, err := newBF(tt.c.txs, tt.c.selectionSet())
			if err != nil {
				t.Fatal(err)
			}
			devs := caseDeviations(tt.c, db)
			if got := slices.Contains(devs, tt.id); got != tt.want {
				t.Errorf("caseDeviations = %v, %s flagged: %v, want %v", devs, tt.id, got, tt.want)
			}
		})
	}
}

func TestExempted(t *testing.T) {
	t.Parallel()
	noEval := &tcase{eval: 'x'}
	withEval := &tcase{eval: 'l'}
	tests := []struct {
		name string
		c    *tcase
		devs []string
		kind diffKind
		want bool
	}{
		{"M3 excuses the log", noEval, []string{"M3"}, diffStderr, true},
		{"M3 does not excuse the order", noEval, []string{"M3"}, diffOrder, false},
		{"O3 excuses the spectrum only", noEval, []string{"O3"}, diffStdout, false},
		{"M1 excuses nothing without a measure", noEval, []string{"M1"}, diffRecords, false},
		{"M1 excuses membership under a measure", withEval, []string{"M1"}, diffRecords, true},
		{"O3 never excuses membership", withEval, []string{"O3"}, diffRecords, false},
		{"R1 excuses values", withEval, []string{"R1"}, diffValue, true},
		{"no deviation excuses nothing", withEval, nil, diffRecords, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := exempted(tt.c, tt.devs, tt.kind); got != tt.want {
				t.Errorf("exempted(%v, %d) = %v, want %v", tt.devs, tt.kind, got, tt.want)
			}
		})
	}
}
