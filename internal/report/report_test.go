package report

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

func TestNew_Panics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		n     int
		freqs []tract.Supp
	}{
		{"negative items", -1, nil},
		{"frequency count mismatch", 3, []tract.Supp{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Fatal("New did not panic")
				}
			}()
			New(tt.n, 10, tt.freqs)
		})
	}
}

func TestReporter_SetTargetNormalization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		in       Target
		mode     Mode
		want     Target
		wantMode Mode
		cm, gen  bool
	}{
		{"all", TargetAll, 0, TargetAll, 0, false, false},
		{"closed forces noexpand", TargetClosed, 0, TargetClosed, ModeNoExpand, true, false},
		{"maximal beats closed", TargetClosed | TargetMaximal, 0, TargetMaximal, ModeNoExpand, true, false},
		{"generators beat maximal", TargetMaximal | TargetGenerators, ModeSort, TargetGenerators, ModeSort, false, true},
		{"rules beat all", TargetRules | TargetGenerators, 0, TargetRules, 0, false, false},
		{"nofilter", TargetClosed, ModeNoFilter, TargetClosed, ModeNoFilter | ModeNoExpand, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := New(3, 5, nil)
			r.SetTarget(tt.in, tt.mode, -1)
			if r.target != tt.want || r.mode != tt.wantMode || (r.cm != nil) != tt.cm || (r.gen != nil) != tt.gen {
				t.Fatalf("target %v mode %v cm %v gen %v", r.target, r.mode, r.cm != nil, r.gen != nil)
			}
		})
	}
}

func TestReporter_Xable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target Target
		zmax   int
		want   int // xmax
	}{
		{"all", TargetAll, 3, 3},
		{"closed", TargetClosed, 3, 4},
		{"maximal unlimited", TargetMaximal, MaxSize, MaxSize},
		{"generators", TargetGenerators, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := New(4, 5, nil)
			r.SetTarget(tt.target, 0, -1)
			r.SetSize(1, tt.zmax)
			r.Setup()
			if !r.Xable(tt.want) || (tt.want < MaxSize && r.Xable(tt.want+1)) {
				t.Fatalf("xmax is not %d", tt.want)
			}
		})
	}
}

// sliceSink collects reported sets and rules; it can fail after a number
// of calls.
type sliceSink struct {
	sets    [][]tract.Item
	supps   []tract.Supp
	rules   []Rule
	failAt  int // fail on this call (1-based); 0 = never
	calls   int
	failErr error
}

func (s *sliceSink) ItemSet(set ItemSet) error {
	s.calls++
	if s.calls == s.failAt {
		return s.failErr
	}
	s.sets = append(s.sets, slices.Clone(set.Items))
	s.supps = append(s.supps, set.Supp)
	return nil
}

func (s *sliceSink) Rule(r Rule) error {
	s.calls++
	if s.calls == s.failAt {
		return s.failErr
	}
	r.Body = slices.Clone(r.Body)
	s.rules = append(s.rules, r)
	return nil
}

func TestReporter_SinkErrorPropagation(t *testing.T) {
	t.Parallel()
	errStop := errors.New("stop")
	tests := []struct {
		name string
		run  func(r *Reporter) error
	}{
		{"report expansion", func(r *Reporter) error {
			r.Add(0, 5)
			r.AddPex(1)
			r.AddPex(2)
			return r.Report()
		}},
		{"report eval", func(r *Reporter) error {
			r.Add(0, 5)
			r.AddPex(1)
			return r.ReportEval(0.5)
		}},
		{"report noexpand", func(r *Reporter) error {
			r.SetTarget(TargetAll, ModeNoExpand, -1)
			r.Add(0, 5)
			r.AddPex(1)
			if err := r.Report(); err != nil {
				return err
			}
			r.Remove(1)
			r.Add(2, 4)
			return r.Report()
		}},
		{"rule", func(r *Reporter) error {
			r.Add(0, 5)
			r.Add(1, 4)
			if err := r.ReportRule(1, 5, 6, 0); err != nil {
				return err
			}
			return r.ReportRule(0, 6, 5, 0)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := New(3, 10, nil)
			sink := &sliceSink{failAt: 2, failErr: errStop}
			r.SetSink(sink)
			r.Setup()
			lenBefore := 0
			err := tt.run(r)
			if err != errStop { //nolint:errorlint // must be returned unchanged
				t.Fatalf("error %v, want the sink's error unchanged", err)
			}
			// an aborted expansion restores the current set
			if r.Len() == lenBefore || r.Len() > 2 {
				t.Fatalf("state after error: items %v", r.Items())
			}
		})
	}
}

func TestReporter_ExpansionAndEmptySet(t *testing.T) {
	t.Parallel()
	// Deviation from upstream (report.md B3): no fast path, so with
	// zmin 1 the empty set is not reported even with top-level perfect
	// extensions; with zmin 0 it is, with the total weight as weight.
	for _, zmin := range []int{0, 1} {
		r := New(3, 4, nil)
		r.SetSize(zmin, MaxSize)
		var weights []float64
		sink := sinkFunc(func(s ItemSet) error {
			if len(s.Items) == 0 {
				weights = append(weights, s.Weight)
			}
			return nil
		})
		r.SetSink(sink)
		r.Setup()
		r.AddPex(2)
		if err := r.Report(); err != nil {
			t.Fatal(err)
		}
		wantEmpty := zmin == 0
		if (len(weights) == 1) != wantEmpty || (wantEmpty && weights[0] != 4) {
			t.Fatalf("zmin %d: empty set weights %v", zmin, weights)
		}
		if r.Count() != uint64(2-zmin) {
			t.Fatalf("zmin %d: Count %d", zmin, r.Count())
		}
	}
}

type sinkFunc func(ItemSet) error

func (f sinkFunc) ItemSet(s ItemSet) error { return f(s) }
func (f sinkFunc) Rule(Rule) error         { return nil }

func TestReporter_CountOnlyBinomials(t *testing.T) {
	t.Parallel()
	// Deviation from upstream (report.md B5): exact binomials.
	tests := []struct {
		name      string
		n         int
		zmin      int
		zmax      int
		wantCount uint64
		wantSat   bool
	}{
		{"62 pexs", 62, 1, MaxSize, 1<<62 - 1, false},
		{"63 pexs", 63, 1, MaxSize, 1<<63 - 1, false},
		{"64 pexs", 64, 1, MaxSize, math.MaxUint64, false},
		{"64 pexs with empty set", 64, 0, MaxSize, math.MaxUint64, true},
		{"100 pexs, sizes 1..2", 100, 1, 2, 100 + 4950, false},
		{"100 pexs, sizes 98..100", 100, 98, 100, 4950 + 100 + 1, false},
		{"100 pexs", 100, 1, MaxSize, math.MaxUint64, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := New(tt.n, 3, nil)
			r.SetSize(tt.zmin, tt.zmax)
			r.Setup()
			for i := range tt.n {
				r.AddPex(tract.Item(i))
			}
			if err := r.Report(); err != nil {
				t.Fatal(err)
			}
			if r.Count() != tt.wantCount || r.Saturated() != tt.wantSat {
				t.Fatalf("Count %d saturated %v, want %d %v", r.Count(), r.Saturated(), tt.wantCount, tt.wantSat)
			}
		})
	}
}

func TestReporter_CountOnlyClosedZmax(t *testing.T) {
	t.Parallel()
	// Deviation from upstream (report.md B4): an unexpanded closed set
	// larger than zmax is not counted without a sink either.
	for _, sink := range []Sink{nil, sinkFunc(func(ItemSet) error { return nil })} {
		r := New(3, 5, nil)
		r.SetTarget(TargetClosed, 0, -1)
		r.SetSize(1, 1)
		r.SetSink(sink)
		r.Setup()
		r.Add(2, 4)
		r.AddPex(1)
		if err := r.Report(); err != nil {
			t.Fatal(err)
		}
		if r.Count() != 0 {
			t.Fatalf("sink %v: Count %d, want 0", sink != nil, r.Count())
		}
	}
}

func TestReporter_MaximalTopLevelPex(t *testing.T) {
	t.Parallel()
	// Deviation from upstream (fpgrowth-core.md B4).
	r := New(2, 4, nil)
	r.SetTarget(TargetMaximal, 0, -1)
	sink := &sliceSink{}
	r.SetSink(sink)
	r.Setup()
	r.AddPex(0)
	if err := r.Report(); err != nil {
		t.Fatal(err)
	}
	if len(sink.sets) != 1 || !slices.Equal(sink.sets[0], []tract.Item{0}) || sink.supps[0] != 4 {
		t.Fatalf("reported %v %v, want [[0]] [4]", sink.sets, sink.supps)
	}
	// the empty set alone is still never maximal
	r = New(2, 4, nil)
	r.SetTarget(TargetMaximal, 0, -1)
	r.SetSize(0, MaxSize)
	r.Setup()
	if err := r.Report(); err != nil || r.Count() != 0 {
		t.Fatalf("empty set reported as maximal (count %d, err %v)", r.Count(), err)
	}
}

func TestReporter_AddPexPacked(t *testing.T) {
	t.Parallel()
	r := New(32, 10, nil)
	r.Add(31, 5)
	r.AddPex(1)
	r.AddPexPacked(PackedFlag | 1<<30 | 1<<3 | 1<<1 | 1)
	if got, want := r.pexs, []tract.Item{1, 0, 3, 30}; !slices.Equal(got, want) {
		t.Fatalf("pexs %v, want %v", got, want)
	}
	r.AddPexPacked(0)
	r.AddPexPacked(PackedFlag)
	if r.PexCount() != 4 {
		t.Fatalf("empty packed values added extensions")
	}
	r.Remove(1)
	if r.PexCount() != 0 || r.Uses(30) || r.Uses(31) {
		t.Fatal("Remove did not pop the extensions")
	}
}

func TestReporter_TailWithoutRepository(t *testing.T) {
	t.Parallel()
	r := New(3, 5, nil)
	if r.Tail(nil) || r.Tail([]tract.Item{1}) {
		t.Fatal("Tail without repository returned true")
	}
}

func TestReporter_RepositoryTooLarge(t *testing.T) { //nolint:paralleltest // changes maxRepoNodes
	t.Run("report", func(t *testing.T) {
		r := New(4, 10, nil)
		r.SetTarget(TargetClosed, 0, -1)
		sink := &sliceSink{}
		r.SetSink(sink)
		r.Setup()
		setMaxRepoNodes(t, 2) // storing {3 2 1} needs a root and two nodes
		for i, s := range []tract.Supp{5, 4, 3} {
			if !r.Add(tract.Item(3-i), s) {
				t.Fatalf("Add(%d) failed", 3-i)
			}
		}
		if err := r.Report(); !errors.Is(err, ErrTooLarge) || !errors.Is(err, tract.ErrTooLarge) {
			t.Fatalf("Report: %v, want ErrTooLarge", err)
		}
		if !errors.Is(r.Err(), ErrTooLarge) {
			t.Fatalf("Err after a failed Report: %v", r.Err())
		}
		if len(sink.sets) != 0 {
			t.Fatalf("reported %v despite the error", sink.sets)
		}
		r.Remove(1)
		if r.Add(0, 1) {
			t.Fatal("Add succeeded after the repository failed")
		}
		if err := r.Report(); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("second Report: %v, want the latched ErrTooLarge", err)
		}
	})
	t.Run("tail", func(t *testing.T) {
		r := New(6, 10, nil)
		r.SetTarget(TargetMaximal, 0, -1)
		r.SetSink(&sliceSink{})
		r.Setup()
		for _, it := range []tract.Item{5, 4, 2, 1} {
			if !r.Add(it, 3) {
				t.Fatalf("Add(%d) failed", it)
			}
		}
		if err := r.Report(); err != nil {
			t.Fatal(err)
		}
		r.Remove(4)
		if !r.Add(4, 6) {
			t.Fatal("Add(4) failed")
		}
		setMaxRepoNodes(t, 1) // projecting on {4} must copy {2 1}
		if !r.Tail([]tract.Item{2, 1}) {
			t.Fatal("Tail = false after a repository overflow, want true (skip the subtree)")
		}
		if !errors.Is(r.Err(), ErrTooLarge) {
			t.Fatalf("Err after a failed Tail: %v", r.Err())
		}
		if err := r.Report(); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("Report after a failed Tail: %v, want ErrTooLarge", err)
		}
	})
	t.Run("add", func(t *testing.T) {
		r := New(6, 10, nil)
		r.SetTarget(TargetClosed, 0, -1)
		r.SetSink(&sliceSink{})
		r.Setup()
		for _, it := range []tract.Item{5, 4, 2, 1} {
			r.Add(it, 3)
		}
		if err := r.Report(); err != nil {
			t.Fatal(err)
		}
		r.Remove(4)
		r.Add(4, 6)
		setMaxRepoNodes(t, 1) // the projection on {4} for the next item
		if r.Add(2, 5) {
			t.Fatal("Add = true after a repository overflow")
		}
		if !errors.Is(r.Err(), ErrTooLarge) {
			t.Fatalf("Err after a failed Add: %v", r.Err())
		}
	})
}

func TestReporter_StatsCopyAndBorder(t *testing.T) {
	t.Parallel()
	r := New(4, 10, nil)
	r.SetBorder(2, 6)
	r.SetBorder(1, 3)
	r.Setup()
	r.Add(0, 7)
	r.Add(1, 5) // size 2, below border 6
	r.AddPex(2) // size 3: beyond the border, not filtered
	if err := r.Report(); err != nil {
		t.Fatal(err)
	}
	st := r.Stats()
	if want := []uint64{0, 0, 0, 1}; !slices.Equal(st, want) {
		t.Fatalf("Stats %v, want %v", st, want)
	}
	st[3] = 99
	if r.Stats()[3] != 1 {
		t.Fatal("Stats returned internal storage")
	}
	r.ClearBorder()
	if err := r.Report(); err != nil {
		t.Fatal(err)
	}
	if r.Count() != 3 {
		t.Fatalf("Count %d after ClearBorder, want 3", r.Count())
	}
}

func TestLogRatio(t *testing.T) {
	t.Parallel()
	// report.md 4.11: test1.tab, "b c (6) 0.292181" = log2(0.6) - 2*log2(0.7)
	r := New(2, 10, []tract.Supp{7, 7})
	r.Add(0, 7)
	if v := LogRatio(r); v != 0 {
		t.Fatalf("single item: %v", v)
	}
	r.Add(1, 6)
	want := math.Log2(0.6) - 2*math.Log2(0.7)
	if v := LogRatio(r); math.Abs(v-want) > 1e-12 || math.Abs(v-0.292181) > 5e-7 {
		t.Fatalf("LogRatio = %v, want %v", v, want)
	}
}

// discardSink accepts everything.
type discardSink struct{ n int }

func (d *discardSink) ItemSet(ItemSet) error { d.n++; return nil }
func (d *discardSink) Rule(Rule) error       { d.n++; return nil }

func TestReporter_ZeroAllocs(t *testing.T) {
	r := New(64, 1000, nil)
	r.SetSink(&discardSink{})
	r.Setup()
	allocs := testing.AllocsPerRun(1000, func() {
		addReportRemove(r)
	})
	if allocs != 0 {
		t.Fatalf("%v allocations per add/report/remove cycle", allocs)
	}
}

// addReportRemove reports a three-level path of frequent sets.
func addReportRemove(r *Reporter) {
	for i := range tract.Item(8) {
		r.Add(i, 900)
		r.Add(i+10, 800)
		r.Add(i+20, 700)
		_ = r.Report()
		r.Remove(1)
		_ = r.Report()
		r.Remove(1)
		_ = r.Report()
		r.Remove(1)
	}
}

func BenchmarkReporter(b *testing.B) {
	b.Run("frequent", func(b *testing.B) {
		r := New(64, 1000, nil)
		r.SetSink(&discardSink{})
		r.Setup()
		b.ReportAllocs()
		for b.Loop() {
			addReportRemove(r)
		}
	})
	b.Run("frequent-count-only", func(b *testing.B) {
		r := New(64, 1000, nil)
		r.Setup()
		b.ReportAllocs()
		for b.Loop() {
			addReportRemove(r)
		}
	})
	b.Run("pex-hypercube-8", func(b *testing.B) {
		r := New(64, 1000, nil)
		r.SetSink(&discardSink{})
		r.Setup()
		b.ReportAllocs()
		for b.Loop() {
			r.Add(40, 500)
			for i := range tract.Item(8) {
				r.AddPex(i)
			}
			_ = r.Report()
			r.Remove(1)
		}
	})
	b.Run("closed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			r := New(64, 1000, nil)
			r.SetTarget(TargetClosed, 0, -1)
			r.SetSink(&discardSink{})
			r.Setup()
			for i := tract.Item(63); i >= 32; i-- {
				if r.Add(i, tract.Supp(i)*10) {
					if r.Add(i-32, tract.Supp(i)*5) {
						_ = r.Report()
						r.Remove(1)
					}
					_ = r.Report()
					r.Remove(1)
				}
			}
		}
	})
}
