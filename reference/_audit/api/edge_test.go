package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	fp "github.com/dimchansky/fpgrowth-go"
)

var bg = context.Background()

func collect[T comparable](t *testing.T, src fp.Source[T], opt fp.Options[T]) ([]fp.Itemset[T], fp.Summary, error) {
	t.Helper()
	return fp.CollectItemsets(bg, src, opt)
}

// --- empty databases ---

func TestEmptyDatabase(t *testing.T) {
	for name, src := range map[string]fp.Source[string]{
		"nil":        fp.FromItems[string](nil),
		"emptyTxs":   fp.FromItems([][]string{{}, {}, {}}),
		"zeroWeight": fp.FromSlice([]fp.Transaction[string]{{Items: []string{"a"}}, {Items: []string{"a", "b"}}}),
	} {
		for _, inc := range []bool{false, true} {
			var started *fp.Stats
			opt := fp.Options[string]{MinSupport: fp.Count(0), IncludeEmpty: inc,
				OnStart: func(s fp.Stats) error { started = &s; return nil }}
			sets, sum, err := collect(t, src, opt)
			t.Logf("%s includeEmpty=%v: sets=%v sum=%+v err=%v onStart=%+v", name, inc, sets, sum, err, started)
		}
		// Fraction on zero total weight
		sets, sum, err := collect(t, src, fp.Options[string]{MinSupport: fp.Fraction(1), IncludeEmpty: true})
		t.Logf("%s Fraction(1)+empty: sets=%v sum=%+v err=%v", name, sets, sum, err)
	}
	rules, sum, err := fp.CollectRules(bg, fp.FromItems([][]string{{}, {}}), fp.RuleOptions[string]{MinSupport: fp.Count(1)})
	t.Logf("rules empty: %v %+v %v", rules, sum, err)
}

// --- NaN, -0, interface items ---

func TestNaNItems(t *testing.T) {
	nan := math.NaN()
	_, _, err := collect(t, fp.FromItems([][]float64{{1, nan}, {1, nan}}), fp.Options[float64]{MinSupport: fp.Count(1)})
	t.Logf("NaN in tx: err=%v is=%v", err, errors.Is(err, fp.ErrInvalidItem))
	_, _, err = collect(t, fp.FromItems([][]float64{{1}}), fp.Options[float64]{MinSupport: fp.Count(1), Items: []float64{nan}})
	t.Logf("NaN in Options.Items: err=%v isInvalidOptions=%v isInvalidItem=%v", err, errors.Is(err, fp.ErrInvalidOptions), errors.Is(err, fp.ErrInvalidItem))
	// NaN ignored by Options.Items: still an error?
	_, _, err = collect(t, fp.FromItems([][]float64{{1, nan}}), fp.Options[float64]{MinSupport: fp.Count(1), Items: []float64{1}})
	t.Logf("NaN outside Options.Items: err=%v", err)
	// -0 and +0
	negz := math.Copysign(0, -1)
	sets, _, err := collect(t, fp.FromItems([][]float64{{negz}, {0}}), fp.Options[float64]{MinSupport: fp.Count(1)})
	for _, s := range sets {
		t.Logf("-0/+0: items=%v signbit=%v support=%d err=%v", s.Items, math.Signbit(s.Items[0]), s.Support, err)
	}
	// NaN as key in RuleOptions.Appearance: silently ignored?
	rules, _, err := fp.CollectRules(bg, fp.FromItems([][]float64{{1, 2}, {1, 2}}),
		fp.RuleOptions[float64]{MinSupport: fp.Count(1), Appearance: map[float64]fp.Appearance{nan: fp.AppearBody}})
	t.Logf("NaN appearance key: rules=%d err=%v", len(rules), err)
	// Complex NaN inside struct
	type pt struct {
		X float32
		N string
	}
	_, _, err = collect(t, fp.FromItems([][]pt{{{float32(nan), "a"}}}), fp.Options[pt]{MinSupport: fp.Count(1)})
	t.Logf("NaN in struct: err=%v", err)
}

type sliceErr struct{ msgs []string }

func (e sliceErr) Error() string { return strings.Join(e.msgs, ";") }

func TestInterfaceItems(t *testing.T) {
	// non-comparable dynamic values
	_, _, err := collect(t, fp.FromItems([][]any{{1, []int{1}}}), fp.Options[any]{MinSupport: fp.Count(1)})
	t.Logf("slice in any: err=%v is=%v", err, errors.Is(err, fp.ErrInvalidItem))
	_, _, err = collect(t, fp.FromItems([][]any{{map[string]int{}}}), fp.Options[any]{MinSupport: fp.Count(1)})
	t.Logf("map in any: err=%v", err)
	type S struct{ V any }
	_, _, err = collect(t, fp.FromItems([][]S{{{1}}, {{[]byte("x")}}}), fp.Options[S]{MinSupport: fp.Count(1)})
	t.Logf("slice in struct{any}: err=%v", err)
	_, _, err = collect(t, fp.FromItems([][]any{{1}}), fp.Options[any]{MinSupport: fp.Count(1), Items: []any{[]int{}}})
	t.Logf("slice in Options.Items: err=%v", err)
	// rules with appearance map and slice in transaction (hook path)
	_, _, err = fp.CollectRules(bg, fp.FromItems([][]any{{1, 2}, {[]int{1}, 2}}),
		fp.RuleOptions[any]{MinSupport: fp.Count(1), Appearance: map[any]fp.Appearance{1: fp.AppearBody}})
	t.Logf("rules appearance + slice item: err=%v", err)
	// rules with AppearNone default and slice items (ignored items)
	_, _, err = fp.CollectRules(bg, fp.FromItems([][]any{{1, 2}, {[]int{1}, 2}}),
		fp.RuleOptions[any]{MinSupport: fp.Count(1), DefaultAppearance: fp.AppearNone, Appearance: map[any]fp.Appearance{1: fp.AppearBoth, 2: fp.AppearBoth}})
	t.Logf("rules AppearNone + slice item: err=%v", err)
	// distinct dynamic types are distinct items
	sets, _, _ := collect(t, fp.FromItems([][]any{{1, int64(1), "1"}}), fp.Options[any]{MinSupport: fp.Count(1), MaxSize: 1})
	t.Logf("1,int64(1),\"1\": %d singletons", len(sets))
	// nil interface item
	sets, _, err = collect(t, fp.FromItems([][]any{{nil, 1}, {nil}}), fp.Options[any]{MinSupport: fp.Count(1)})
	t.Logf("nil item: sets=%v err=%v", sets, err)
	// error interface: items of type error
	_, _, err = collect(t, fp.FromItems([][]error{{sliceErr{[]string{"x"}}}}), fp.Options[error]{MinSupport: fp.Count(1)})
	t.Logf("non-comparable error value as item: err=%v", err)
}

// --- callback errors ---

func TestCallbackErrorNonComparable(t *testing.T) {
	src := fp.FromItems([][]string{{"a", "b"}, {"a"}})
	defer func() {
		if r := recover(); r != nil {
			t.Logf("PANIC with non-comparable callback error: %v", r)
		}
	}()
	_, err := fp.MineItemsets(bg, src, fp.Options[string]{MinSupport: fp.Count(1)}, func(fp.Itemset[string]) error {
		return sliceErr{[]string{"boom"}}
	})
	t.Logf("non-comparable callback error returned: %v (%T)", err, err)
}

func TestCallbackErrorNonComparableRules(t *testing.T) {
	src := fp.FromItems([][]string{{"a", "b"}, {"a"}})
	defer func() {
		if r := recover(); r != nil {
			t.Logf("PANIC (rules) with non-comparable callback error: %v", r)
		}
	}()
	_, err := fp.MineRules(bg, src, fp.RuleOptions[string]{MinSupport: fp.Count(1)}, func(fp.Rule[string]) error {
		return sliceErr{[]string{"boom"}}
	})
	t.Logf("rules non-comparable callback error returned: %v", err)
}

func TestCallbackErrorNonComparableOnStart(t *testing.T) {
	src := fp.FromItems([][]string{{"a", "b"}, {"a"}})
	_, err := fp.MineItemsets(bg, src, fp.Options[string]{MinSupport: fp.Count(1), OnStart: func(fp.Stats) error {
		return sliceErr{[]string{"boom"}}
	}}, nil)
	t.Logf("onstart non-comparable: %v", err)
}

func TestCallbackErrors(t *testing.T) {
	src := fp.FromItems([][]string{{"a", "b"}, {"a"}, {"b", "c"}})
	sentinel := errors.New("mine")
	n := 0
	sum, err := fp.MineItemsets(bg, src, fp.Options[string]{MinSupport: fp.Count(1)}, func(fp.Itemset[string]) error {
		n++
		if n == 2 {
			return sentinel
		}
		return nil
	})
	t.Logf("callback err identical=%v reported=%d calls=%d", err == sentinel, sum.Reported, n)
	// callback returns context.Canceled on a live context
	_, err = fp.MineItemsets(bg, src, fp.Options[string]{MinSupport: fp.Count(1)}, func(fp.Itemset[string]) error { return context.Canceled })
	t.Logf("callback returns context.Canceled: %v", err)
	// source error wrapping
	boom := errors.New("src boom")
	bad := fp.SourceFunc[string](func() iter.Seq2[fp.Transaction[string], error] {
		return func(yield func(fp.Transaction[string], error) bool) {
			if !yield(fp.Transaction[string]{Items: []string{"a"}, Weight: 1}, nil) {
				return
			}
			yield(fp.Transaction[string]{}, boom)
		}
	})
	_, err = fp.MineItemsets(bg, bad, fp.Options[string]{MinSupport: fp.Count(1)}, nil)
	t.Logf("source error: %q is=%v", err, errors.Is(err, boom))
	// missing file
	_, err = fp.MineItemsets(bg, fp.TableFile("/nonexistent/x.dat", fp.TableFormat{}), fp.Options[string]{MinSupport: fp.Count(1)}, nil)
	t.Logf("missing file: %q isNotExist=%v", err, errors.Is(err, os.ErrNotExist))
	// syntax error message
	dir := t.TempDir()
	p := filepath.Join(dir, "w.dat")
	os.WriteFile(p, []byte("a b 1\nc d x\n"), 0o644)
	_, err = fp.MineItemsets(bg, fp.TableFile(p, fp.TableFormat{RecordSeparators: "\n", FieldSeparators: " ", Blanks: " ", Weighted: true}), fp.Options[string]{MinSupport: fp.Count(1)}, nil)
	t.Logf("syntax: %q isSyntax=%v", err, errors.Is(err, fp.ErrSyntax))
	pc := filepath.Join(dir, "w.csv")
	os.WriteFile(pc, []byte("a,b,1\nc,d,-3\n"), 0o644)
	_, err = fp.MineItemsets(bg, fp.CSVFile(pc, fp.CSVFormat{WeightColumn: -1}), fp.Options[string]{MinSupport: fp.Count(1)}, nil)
	t.Logf("csv negative: %q isNeg=%v", err, errors.Is(err, fp.ErrNegativeWeight))
	_, err = fp.MineItemsets(bg, fp.FromSlice([]fp.Transaction[string]{{Items: []string{"a"}, Weight: -1}}), fp.Options[string]{MinSupport: fp.Count(1)}, nil)
	t.Logf("negative weight: %q", err)
	// invalid options
	_, err = fp.MineItemsets(bg, src, fp.Options[string]{}, nil)
	t.Logf("no minsupport: %q", err)
	_, err = fp.MineItemsets(bg, nil, fp.Options[string]{MinSupport: fp.Count(1)}, nil)
	t.Logf("nil src: %q", err)
	// typed-nil Source
	var sf fp.SourceFunc[string]
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("typed nil SourceFunc PANIC: %v", r)
			}
		}()
		_, err = fp.MineItemsets(bg, sf, fp.Options[string]{MinSupport: fp.Count(1)}, nil)
		t.Logf("typed nil SourceFunc: %v", err)
	}()
	// nil ctx
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("nil ctx PANIC: %v", r)
			}
		}()
		//lint:ignore SA1012 testing
		_, err = fp.MineItemsets(nil, src, fp.Options[string]{MinSupport: fp.Count(1)}, nil) //nolint
		t.Logf("nil ctx: %v", err)
	}()
}

// --- weights ---

func TestWeights(t *testing.T) {
	big := int64(math.MaxInt64/2 + 1)
	_, _, err := collect(t, fp.FromSlice([]fp.Transaction[string]{{Items: []string{"a"}, Weight: big}, {Items: []string{"a"}, Weight: big}}), fp.Options[string]{MinSupport: fp.Count(1)})
	t.Logf("overflow: %v is=%v", err, errors.Is(err, fp.ErrWeightOverflow))
	sets, sum, err := collect(t, fp.FromSlice([]fp.Transaction[string]{{Items: []string{"a", "b"}, Weight: math.MaxInt64 - 1}, {Items: []string{"a"}, Weight: 1}}), fp.Options[string]{MinSupport: fp.Count(math.MaxInt64)})
	t.Logf("maxint total: sets=%v total=%d min=%d err=%v", sets, sum.TotalWeight, sum.MinSupport, err)
	sets, sum, err = collect(t, fp.FromSlice([]fp.Transaction[string]{{Items: []string{"a", "b"}, Weight: math.MaxInt64 - 1}, {Items: []string{"a"}, Weight: 1}}), fp.Options[string]{MinSupport: fp.Fraction(1)})
	t.Logf("maxint Fraction(1): sets=%v min=%d err=%v", sets, sum.MinSupport, err)
	rules, _, err := fp.CollectRules(bg, fp.FromSlice([]fp.Transaction[string]{{Items: []string{"a", "b"}, Weight: math.MaxInt64 - 1}, {Items: []string{"a"}, Weight: 1}}), fp.RuleOptions[string]{MinSupport: fp.Count(1), Evaluation: &fp.Evaluation{Measure: fp.Chi2}})
	for _, r := range rules {
		t.Logf("huge rule %v->%v supp=%d conf=%g lift=%g val=%g", r.Body, r.Head, r.Support, r.Confidence, r.Lift, r.Value)
	}
	t.Logf("huge rules err=%v", err)
}

// --- thresholds ---

func TestThresholdBoundaries(t *testing.T) {
	var txs [][]string
	for i := range 10 {
		if i < 3 {
			txs = append(txs, []string{"a"})
		} else {
			txs = append(txs, []string{"b"})
		}
	}
	src := fp.FromItems(txs)
	for _, s := range []fp.Support{fp.Count(3), fp.Count(4), fp.Fraction(0.3), fp.Fraction(0.30000000000000004), fp.Fraction(0.3000001), fp.Fraction(1e-300), fp.Count(0)} {
		sets, sum, err := collect(t, src, fp.Options[string]{MinSupport: s, MaxSize: 1})
		t.Logf("min %v -> abs %d sets=%d err=%v", s, sum.MinSupport, len(sets), err)
	}
	for _, s := range []fp.Support{fp.Count(3), fp.Count(2), fp.Fraction(0.3), fp.Fraction(0.29999), fp.Count(0)} {
		sets, sum, err := collect(t, src, fp.Options[string]{MinSupport: fp.Count(1), MaxSupport: s, MaxSize: 1})
		t.Logf("max %v -> abs %d sets=%v err=%v", s, sum.MaxSupport, sets, err)
	}
	// MaxSupport < MinSupport: accepted?
	_, _, err := collect(t, src, fp.Options[string]{MinSupport: fp.Count(5), MaxSupport: fp.Count(2)})
	t.Logf("max<min err=%v", err)
	// invalid fractions
	for _, f := range []float64{0, -0.1, 1.0000001, math.NaN(), math.Inf(1)} {
		_, _, err := collect(t, src, fp.Options[string]{MinSupport: fp.Fraction(f)})
		t.Logf("Fraction(%v): %v", f, err)
	}
	// Support comparability
	t.Logf("Fraction(0.5)==Fraction(0.5): %v; zero Support String: %q", fp.Fraction(0.5) == fp.Fraction(0.5), fp.Support{}.String())
	// confidence boundary: conf exactly 0.3
	rules, _, _ := fp.CollectRules(bg, src, fp.RuleOptions[string]{MinSupport: fp.Count(1), MinConfidence: 0.3})
	for _, r := range rules {
		t.Logf("conf0.3 rule %v->%s conf=%v", r.Body, r.Head, r.Confidence)
	}
}

// --- data ownership ---

func TestInputNotMutated(t *testing.T) {
	txs := [][]string{{"c", "b", "a", "a"}, {"b", "a"}, {"c", "c", "z"}}
	orig := make([][]string, len(txs))
	for i := range txs {
		orig[i] = slices.Clone(txs[i])
	}
	items := []string{"c", "a", "b"}
	origItems := slices.Clone(items)
	for _, alg := range []fp.Algorithm{fp.AlgorithmAuto, fp.AlgorithmSimple, fp.AlgorithmSingleTree, fp.AlgorithmTopDown} {
		_, _, err := collect(t, fp.FromItems(txs), fp.Options[string]{MinSupport: fp.Count(1), Algorithm: alg, Items: items})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = fp.CollectRules(bg, fp.FromItems(txs), fp.RuleOptions[string]{MinSupport: fp.Count(1), Algorithm: alg})
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("tx mutated=%v items mutated=%v", !reflect.DeepEqual(txs, orig), !slices.Equal(items, origItems))
}

func TestYieldedSliceReuse(t *testing.T) {
	src := fp.FromItems([][]string{{"a", "b", "c"}, {"a", "b"}, {"a", "c"}})
	var kept [][]string
	var cloned [][]string
	fp.MineItemsets(bg, src, fp.Options[string]{MinSupport: fp.Count(1)}, func(s fp.Itemset[string]) error {
		kept = append(kept, s.Items)
		cloned = append(cloned, slices.Clone(s.Items))
		return nil
	})
	t.Logf("kept (no clone): %v", kept)
	t.Logf("cloned:          %v", cloned)
	// append to yielded slice: does it overwrite library buffer?
	var after []string
	fp.MineItemsets(bg, src, fp.Options[string]{MinSupport: fp.Count(1)}, func(s fp.Itemset[string]) error {
		x := append(s.Items, "ZZZ")
		_ = x
		after = append(after, fmt.Sprint(s.Items))
		return nil
	})
	t.Logf("after append: %v", after)
	// NaN Value in collected results: reflect.DeepEqual
	a, _, _ := collect(t, src, fp.Options[string]{MinSupport: fp.Count(2)})
	b, _, _ := collect(t, src, fp.Options[string]{MinSupport: fp.Count(2)})
	t.Logf("DeepEqual of two identical CollectItemsets runs: %v (Value=%v)", reflect.DeepEqual(a, b), a[0].Value)
}

// --- non-deterministic sources ---

func TestNonReplayable(t *testing.T) {
	// pass 2 swaps item membership but keeps counts and total weight
	pass := 0
	src := fp.SourceFunc[string](func() iter.Seq2[fp.Transaction[string], error] {
		pass++
		p := pass
		return func(yield func(fp.Transaction[string], error) bool) {
			var data [][]string
			if p == 1 {
				data = [][]string{{"a", "b"}, {"a", "b"}, {"a", "b"}, {"c"}}
			} else {
				data = [][]string{{"c"}, {"c"}, {"c"}, {"a", "b"}}
			}
			for _, d := range data {
				if !yield(fp.Transaction[string]{Items: d, Weight: 1}, nil) {
					return
				}
			}
		}
	})
	sets, sum, err := collect(t, src, fp.Options[string]{MinSupport: fp.Count(3)})
	t.Logf("swapped pass2: sets=%v minsupp=%d frequent=%d err=%v", sets, sum.MinSupport, sum.FrequentItems, err)
	// pass 2 with fewer transactions
	pass = 0
	src2 := fp.SourceFunc[string](func() iter.Seq2[fp.Transaction[string], error] {
		pass++
		n := 4
		if pass > 1 {
			n = 3
		}
		return func(yield func(fp.Transaction[string], error) bool) {
			for range n {
				if !yield(fp.Transaction[string]{Items: []string{"a"}, Weight: 1}, nil) {
					return
				}
			}
		}
	})
	_, _, err = collect(t, src2, fp.Options[string]{MinSupport: fp.Count(1)})
	t.Logf("fewer pass2: %v", err)
	// unknown item in pass 2
	pass = 0
	src3 := fp.SourceFunc[string](func() iter.Seq2[fp.Transaction[string], error] {
		pass++
		it := "a"
		if pass > 1 {
			it = "new"
		}
		return func(yield func(fp.Transaction[string], error) bool) {
			yield(fp.Transaction[string]{Items: []string{it}, Weight: 1}, nil)
		}
	})
	_, _, err = collect(t, src3, fp.Options[string]{MinSupport: fp.Count(1)})
	t.Logf("unknown pass2: %v", err)
}

// --- files modified between passes ---

func TestFileModifiedBetweenPasses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.dat")
	os.WriteFile(p, []byte("a b\na b\nc\n"), 0o644)
	opens := 0
	open := func() (io.ReadCloser, error) {
		opens++
		if opens == 2 {
			// same size, different content, restore mtime
			st, _ := os.Stat(p)
			os.WriteFile(p, []byte("c b\na b\na\n"), 0o644)
			os.Chtimes(p, st.ModTime(), st.ModTime())
		}
		return os.Open(p)
	}
	sets, _, err := collect(t, fp.TableReader(open, p, fp.TableFormat{}), fp.Options[string]{MinSupport: fp.Count(2)})
	t.Logf("same size+mtime rewrite: sets=%v err=%v", sets, err)
	os.WriteFile(p, []byte("a b\na b\nc\n"), 0o644)
	opens = 0
	open2 := func() (io.ReadCloser, error) {
		opens++
		if opens == 2 {
			os.WriteFile(p, []byte("a b\na b\nc d\n"), 0o644)
		}
		return os.Open(p)
	}
	_, _, err = collect(t, fp.TableReader(open2, p, fp.TableFormat{}), fp.Options[string]{MinSupport: fp.Count(2)})
	t.Logf("size change: err=%v is=%v", err, errors.Is(err, fp.ErrNotReplayable))
}

// --- resources on early stop ---

type countingRC struct {
	io.ReadCloser
	closed *int
	mu     *sync.Mutex
}

func (c countingRC) Close() error {
	c.mu.Lock()
	*c.closed++
	c.mu.Unlock()
	return c.ReadCloser.Close()
}

func TestEarlyBreakResources(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.csv")
	var sb strings.Builder
	for i := range 50000 {
		fmt.Fprintf(&sb, "i%d,i%d,i%d\n", i%7, i%11, i%13)
	}
	os.WriteFile(p, []byte(sb.String()), 0o644)
	var mu sync.Mutex
	opened, closed := 0, 0
	open := func() (io.ReadCloser, error) {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		opened++
		mu.Unlock()
		return countingRC{f, &closed, &mu}, nil
	}
	g0 := runtime.NumGoroutine()
	for _, mk := range []func() fp.Source[string]{
		func() fp.Source[string] { return fp.CSVReader(open, p, fp.CSVFormat{}) },
		func() fp.Source[string] { return fp.TableReader(open, p, fp.TableFormat{}) },
	} {
		n := 0
		for _, err := range fp.Itemsets(bg, mk(), fp.Options[string]{MinSupport: fp.Count(1)}) {
			if err != nil {
				t.Fatal(err)
			}
			n++
			if n == 3 {
				break
			}
		}
		// cancel during pass 1
		ctx, cancel := context.WithCancel(bg)
		cnt := 0
		src := fp.SourceFunc[string](func() iter.Seq2[fp.Transaction[string], error] {
			inner := mk().Transactions()
			return func(yield func(fp.Transaction[string], error) bool) {
				for tx, err := range inner {
					cnt++
					if cnt == 3000 {
						cancel()
					}
					if !yield(tx, err) {
						return
					}
				}
			}
		})
		_, err := fp.MineItemsets(ctx, src, fp.Options[string]{MinSupport: fp.Count(1)}, nil)
		t.Logf("cancel in pass 1: err=%v", err)
		// error in callback
		_, err = fp.MineItemsets(bg, mk(), fp.Options[string]{MinSupport: fp.Count(1)}, func(fp.Itemset[string]) error { return io.ErrUnexpectedEOF })
		t.Logf("callback error: %v", err)
	}
	runtime.Gosched()
	t.Logf("opened=%d closed=%d goroutines before=%d after=%d", opened, closed, g0, runtime.NumGoroutine())
}

// --- spool ---

func TestSpool(t *testing.T) {
	dir := t.TempDir()
	src, cleanup, err := fp.SpoolCSV(bg, strings.NewReader("a,b\na,c\n"), dir, fp.CSVFormat{})
	if err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	st, _ := os.Stat(filepath.Join(dir, ents[0].Name()))
	t.Logf("spool file %s mode %v", ents[0].Name(), st.Mode())
	sets, _, err := collect(t, src, fp.Options[string]{MinSupport: fp.Count(2)})
	t.Logf("spool sets=%v err=%v", sets, err)
	t.Logf("cleanup1=%v cleanup2=%v", cleanup(), cleanup())
	_, _, err = collect(t, src, fp.Options[string]{MinSupport: fp.Count(2)})
	t.Logf("after cleanup: %v", err)
	ents, _ = os.ReadDir(dir)
	t.Logf("left files: %d", len(ents))
	ctx, cancel := context.WithCancel(bg)
	cancel()
	_, c2, err := fp.SpoolTable(ctx, strings.NewReader("a"), dir, fp.TableFormat{})
	ents, _ = os.ReadDir(dir)
	t.Logf("spool canceled: err=%v cleanupNil=%v files=%d", err, c2 == nil, len(ents))
	_, _, err = fp.SpoolTable(bg, strings.NewReader("a"), filepath.Join(dir, "nope"), fp.TableFormat{})
	t.Logf("spool bad dir: %v", err)
	// invalid CSV format reported at spool time?
	_, c3, err := fp.SpoolCSV(bg, strings.NewReader("a"), dir, fp.CSVFormat{Comma: '"'})
	t.Logf("spool invalid CSV format: err=%v (cleanup nil=%v)", err, c3 == nil)
	if c3 != nil {
		c3()
	}
	// CSVFile invalid format detected when?
	_, _, err = collect(t, fp.CSVFile("/nonexistent", fp.CSVFormat{Comma: '\n'}), fp.Options[string]{MinSupport: fp.Count(1)})
	t.Logf("CSVFile invalid comma error: %v", err)
}

// --- concurrency ---

func TestConcurrentMining(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.dat")
	var sb strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&sb, "i%d i%d i%d i%d\n", i%5, i%7, i%11, i%3)
	}
	os.WriteFile(p, []byte(sb.String()), 0o644)
	var txs [][]string
	for i := range 3000 {
		txs = append(txs, []string{fmt.Sprint("i", i%5), fmt.Sprint("i", i%7), fmt.Sprint("i", i%11)})
	}
	srcs := []fp.Source[string]{fp.TableFile(p, fp.TableFormat{}), fp.FromItems(txs), fp.CSVFile(p, fp.CSVFormat{Comma: ' '})}
	opt := fp.Options[string]{MinSupport: fp.Count(10), Border: []fp.Support{fp.Count(1), fp.Count(20)}, Items: []string{"i1", "i2", "i3", "i4", "i0"},
		Evaluation: &fp.Evaluation{Measure: fp.Lift, Filter: true, Threshold: 0.5, PruneSize: new(int)}}
	ropt := fp.RuleOptions[string]{MinSupport: fp.Count(10), Appearance: map[string]fp.Appearance{"i1": fp.AppearHead}}
	var wg sync.WaitGroup
	results := make([]uint64, 24)
	for g := range 24 {
		wg.Go(func() {
			src := srcs[g%3]
			var s fp.Summary
			var err error
			if g%2 == 0 {
				s, err = fp.MineItemsets(bg, src, opt, func(fp.Itemset[string]) error { return nil })
			} else {
				s, err = fp.MineRules(bg, src, ropt, func(fp.Rule[string]) error { return nil })
			}
			if err != nil {
				t.Error(err)
			}
			results[g] = s.Reported
		})
	}
	wg.Wait()
	t.Logf("reported per goroutine: %v", results)
}

// --- cancellation latency ---

func genDense(n, width int) fp.Source[int] {
	return fp.Generate(n, func(i int, buf []int) fp.Transaction[int] {
		x := uint64(i)*2654435761 + 12345
		for j := range width {
			x ^= x << 13
			x ^= x >> 7
			x ^= x << 17
			if x%3 != 0 {
				buf = append(buf, j)
			}
		}
		return fp.Transaction[int]{Items: buf, Weight: 1}
	})
}

func TestCancelLatency(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	type cs struct {
		name string
		run  func(ctx context.Context) error
	}
	cases := []cs{
		{"itemsets dense", func(ctx context.Context) error {
			_, err := fp.MineItemsets(ctx, genDense(2000, 40), fp.Options[int]{MinSupport: fp.Fraction(0.05)}, func(fp.Itemset[int]) error { return nil })
			return err
		}},
		{"itemsets count-only", func(ctx context.Context) error {
			_, err := fp.MineItemsets(ctx, genDense(2000, 40), fp.Options[int]{MinSupport: fp.Fraction(0.05)}, nil)
			return err
		}},
		{"closed", func(ctx context.Context) error {
			_, err := fp.MineItemsets(ctx, genDense(5000, 40), fp.Options[int]{MinSupport: fp.Fraction(0.02), Target: fp.TargetClosed}, nil)
			return err
		}},
		{"rules", func(ctx context.Context) error {
			_, err := fp.MineRules(ctx, genDense(2000, 30), fp.RuleOptions[int]{MinSupport: fp.Fraction(0.05)}, func(fp.Rule[int]) error { return nil })
			return err
		}},
		{"pass1 big", func(ctx context.Context) error {
			_, err := fp.MineItemsets(ctx, genDense(20_000_000, 10), fp.Options[int]{MinSupport: fp.Count(1)}, nil)
			return err
		}},
	}
	for _, c := range cases {
		ctx, cancel := context.WithCancel(bg)
		var canceledAt time.Time
		go func() {
			time.Sleep(200 * time.Millisecond)
			canceledAt = time.Now()
			cancel()
		}()
		start := time.Now()
		err := c.run(ctx)
		end := time.Now()
		lat := time.Duration(0)
		if !canceledAt.IsZero() {
			lat = end.Sub(canceledAt)
		}
		t.Logf("%-20s total=%v latency-after-cancel=%v err=%v", c.name, end.Sub(start), lat, err)
		cancel()
	}
}

// --- OnStart ---

func TestOnStart(t *testing.T) {
	src := fp.FromItems([][]string{{"a"}, {"b"}})
	calls := 0
	_, err := fp.MineItemsets(bg, src, fp.Options[string]{MinSupport: fp.Count(5), OnStart: func(s fp.Stats) error {
		calls++
		t.Logf("OnStart no frequent: %+v", s)
		return nil
	}}, nil)
	t.Logf("calls=%d err=%v", calls, err)
	stop := errors.New("stop")
	sum, err := fp.MineItemsets(bg, src, fp.Options[string]{MinSupport: fp.Count(1), OnStart: func(fp.Stats) error { return stop }}, func(fp.Itemset[string]) error {
		t.Error("callback after OnStart error")
		return nil
	})
	t.Logf("OnStart error: same=%v sum=%+v", err == stop, sum)
	// OnStart with iterator: stats not reachable from Itemsets except via OnStart
	var total int64
	for s, err := range fp.Itemsets(bg, src, fp.Options[string]{MinSupport: fp.Count(1), OnStart: func(st fp.Stats) error { total = st.TotalWeight; return nil }}) {
		t.Logf("iter %v rel=%.2f err=%v", s.Items, float64(s.Support)/float64(total), err)
	}
	// Re-iterating the same iterator mines again
	seq := fp.Itemsets(bg, src, fp.Options[string]{MinSupport: fp.Count(1)})
	n1, n2 := 0, 0
	for range seq {
		n1++
	}
	for range seq {
		n2++
	}
	t.Logf("re-iteration: %d then %d", n1, n2)
}

// --- determinism ---

func TestDeterminism(t *testing.T) {
	src := genDense(500, 20)
	app := map[int]fp.Appearance{}
	for i := range 20 {
		app[i] = fp.Appearance(i % 3)
	}
	var first []string
	for run := range 20 {
		rules, _, err := fp.CollectRules(bg, src, fp.RuleOptions[int]{MinSupport: fp.Fraction(0.2), Appearance: app, MaxSize: 3})
		if err != nil {
			t.Fatal(err)
		}
		var cur []string
		for _, r := range rules {
			cur = append(cur, fmt.Sprint(r.Body, r.Head, r.Support))
		}
		if run == 0 {
			first = cur
		} else if !slices.Equal(first, cur) {
			t.Logf("NON-DETERMINISTIC at run %d", run)
			return
		}
	}
	t.Logf("deterministic over 20 runs (%d rules)", len(first))
	// Options.Items order affects output order?
	a, _, _ := collect(t, src, fp.Options[int]{MinSupport: fp.Fraction(0.3), Items: []int{1, 2, 3}, Tuning: fp.Tuning{ItemOrder: fp.ItemOrderNone}})
	b, _, _ := collect(t, src, fp.Options[int]{MinSupport: fp.Fraction(0.3), Items: []int{3, 2, 1}, Tuning: fp.Tuning{ItemOrder: fp.ItemOrderNone}})
	var sa, sb []string
	for _, s := range a {
		sa = append(sa, fmt.Sprint(s.Items))
	}
	for _, s := range b {
		sb = append(sb, fmt.Sprint(s.Items))
	}
	t.Logf("Items order [1 2 3]: %v", sa)
	t.Logf("Items order [3 2 1]: %v", sb)
}
