package isrout

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

var testNames = []string{"a", "b", "c", "d"}

func writeSets(t *testing.T, cfg Config, sets ...report.ItemSet) string {
	t.Helper()
	var out bytes.Buffer
	w := New(&out, cfg)
	for _, s := range sets {
		if err := w.ItemSet(s); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func writeRules(t *testing.T, cfg Config, rules ...report.Rule) string {
	t.Helper()
	var out bytes.Buffer
	w := New(&out, cfg)
	for _, r := range rules {
		if err := w.Rule(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestDefaultInfo(t *testing.T) {
	tests := []struct {
		rules, absolute bool
		expected        string
	}{
		{false, false, " (%S)"},
		{false, true, " (%a)"},
		{true, false, " (%X, %C)"},
		{true, true, " (%b, %C)"},
	}
	for _, tt := range tests {
		if got := DefaultInfo(tt.rules, tt.absolute); got != tt.expected {
			t.Errorf("DefaultInfo(%v, %v) = %q, want %q", tt.rules, tt.absolute, got, tt.expected)
		}
	}
}

func TestWriter_ItemSet(t *testing.T) {
	set := report.ItemSet{Items: []tract.Item{2, 0}, Supp: 3, Weight: 0.5, Eval: 0.25}
	tests := []struct {
		name     string
		format   Format
		total    tract.Supp
		set      report.ItemSet
		expected string
	}{
		{"default", Format{Sep: " ", Info: " (%S)"}, 8, set, "c a (37.5)\n"},
		{"empty set", Format{Sep: " ", Info: " (%a)"}, 8, report.ItemSet{Supp: 8}, " (8)\n"},
		{"empty info", Format{Sep: ","}, 8, set, "c,a\n"},
		{"integer codes", Format{Sep: " ", Info: " %i %a %q %Q %%"}, 8, set, "c a 2 3 8 8 %\n"},
		{"fraction codes", Format{Sep: " ", Info: " %s %x %S %X %2S"}, 8, set, "c a 0.375 0.375 37.5 37.5 37\n"},
		{"weight codes", Format{Sep: " ", Info: " %w %W %r %R %z"}, 8, set, "c a 0.5 50 0.0625 6.25 4\n"},
		{"evaluation codes", Format{Sep: " ", Info: " %e %E %p %P"}, 8, set, "c a 0.25 25 0.25 25\n"},
		{"rule codes are literal", Format{Sep: " ", Info: " %b %h %y %Y %c %C %l %L"}, 8, set,
			"c a %b %h %y %Y %c %C %l %L\n"},
		{"unknown code with digits", Format{Sep: " ", Info: " %12j %123S %.2S"}, 8, set, "c a %12j %123S %.2S\n"},
		{"trailing percent", Format{Sep: " ", Info: " %"}, 8, set, "c a %\n"},
		{"trailing percent digits", Format{Sep: " ", Info: " %5"}, 8, set, "c a %5\n"},
		{"zero total counts as one", Format{Sep: " ", Info: " %S %Q"}, 0, set, "c a 300 1\n"},
		{"negative total counts as one", Format{Sep: " ", Info: " %s %q"}, -5, set, "c a 3 1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := writeSets(t, Config{Names: testNames, Total: tt.total, Format: tt.format}, tt.set)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestWriter_ItemSetDCode pins the %d deviation (report.md B6): the
// absolute support for item sets, also where upstream's normal output
// path prints "%d".
func TestWriter_ItemSetDCode(t *testing.T) {
	cfg := Config{Names: testNames, Total: 10, Format: Format{Sep: " ", Info: " (%d) %3d"}}
	got := writeSets(t, cfg, report.ItemSet{Items: []tract.Item{1, 2}, Supp: 4})
	if expected := "b c (4) 4\n"; got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

// TestWriter_Header pins the header deviation (report.md B1) for sets,
// the empty set and rules.
func TestWriter_Header(t *testing.T) {
	cfg := Config{Names: testNames, Total: 10, Format: Format{Header: "H:", Sep: " ", Imp: " <- ", Info: " (%a)"}}
	got := writeSets(t, cfg,
		report.ItemSet{Items: []tract.Item{0, 1}, Supp: 4},
		report.ItemSet{Supp: 10},
	)
	if expected := "H:a b (4)\nH: (10)\n"; got != expected {
		t.Errorf("sets: got %q, want %q", got, expected)
	}
	got = writeRules(t, cfg, report.Rule{Head: 3, Supp: 2, BodySupp: 10, HeadSupp: 2})
	if expected := "H:d <-  (2)\n"; got != expected {
		t.Errorf("rules: got %q, want %q", got, expected)
	}
}

// TestWriter_Scan pins the -g deviation (report.md B2).
func TestWriter_Scan(t *testing.T) {
	names := []string{"plain", "with space", `q"uote`}
	cfg := Config{Names: names, Total: 10, Format: Format{Sep: ",", Info: "", Scan: true}}
	got := writeSets(t, cfg, report.ItemSet{Items: []tract.Item{0, 1, 2}, Supp: 1})
	if expected := `plain,"with space","q\"uote"` + "\n"; got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
	if names[1] != "with space" {
		t.Error("New modified the caller's names")
	}
	cfg.Scan = false
	got = writeSets(t, cfg, report.ItemSet{Items: []tract.Item{1, 2}, Supp: 1})
	if expected := `with space,q"uote` + "\n"; got != expected {
		t.Errorf("without Scan: got %q, want %q", got, expected)
	}
}

func TestWriter_Rule(t *testing.T) {
	rule := report.Rule{Head: 1, Body: []tract.Item{0, 2}, Supp: 2, BodySupp: 4, HeadSupp: 5, Eval: 0.125}
	tests := []struct {
		name     string
		format   Format
		rule     report.Rule
		expected string
	}{
		{"default", Format{Sep: " ", Imp: " <- ", Info: " (%X, %C)"}, rule, "b <- a c (40, 50)\n"},
		{"separators", Format{Sep: ",", Imp: "=>", Info: ""}, rule, "b=>a,c\n"},
		{"empty body", Format{Sep: " ", Imp: " <- ", Info: " (%b, %C)"},
			report.Rule{Head: 3, Supp: 7, BodySupp: 10, HeadSupp: 7}, "d <-  (10, 70)\n"},
		{"integer codes", Format{Sep: " ", Imp: " <- ", Info: " %i %a %b %h %Q %%"}, rule, "b <- a c 3 2 4 5 10 %\n"},
		{"fraction codes", Format{Sep: " ", Imp: " <- ", Info: " %s %S %x %X %y %Y"}, rule,
			"b <- a c 0.2 20 0.4 40 0.5 50\n"},
		{"confidence and lift", Format{Sep: " ", Imp: " <- ", Info: " %c %C %l %L"}, rule,
			"b <- a c 0.5 50 1 100\n"},
		{"zero body support", Format{Sep: " ", Imp: " <- ", Info: " %c %l"},
			report.Rule{Head: 0, Supp: 1, HeadSupp: 1}, "a <-  0 0\n"},
		{"zero head support", Format{Sep: " ", Imp: " <- ", Info: " %l %L"},
			report.Rule{Head: 0, Supp: 1, BodySupp: 1}, "a <-  0 0\n"},
		{"evaluation", Format{Sep: " ", Imp: " <- ", Info: " %e %E %2E"}, rule, "b <- a c 0.125 12.5 13\n"},
		{"set-only codes are literal", Format{Sep: " ", Imp: " <- ", Info: " %q %w %W %r %R %z %p %P %d"}, rule,
			"b <- a c %q %w %W %r %R %z %p %P %d\n"},
		{"trailing percent", Format{Sep: " ", Imp: " <- ", Info: " %07"}, rule, "b <- a c %07\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := writeRules(t, Config{Names: testNames, Total: 10, Format: tt.format}, tt.rule)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestWriter_WriteStats(t *testing.T) {
	var out bytes.Buffer
	w := New(&out, Config{Names: testNames, Total: 4, Format: Format{Sep: " ", Info: " (%a)"}})
	if err := w.ItemSet(report.ItemSet{Items: []tract.Item{0}, Supp: 2}); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteStats(1, []uint64{0, 1}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if expected := "a (2)\nall: 1\n  0: 0\n  1: 1\n"; out.String() != expected {
		t.Errorf("got %q, want %q", out.String(), expected)
	}
}

// failWriter accepts limit bytes, then fails every write.
type failWriter struct {
	limit int
	n     int
	err   error
}

func (f *failWriter) Write(p []byte) (int, error) {
	if f.n+len(p) > f.limit {
		k := max(f.limit-f.n, 0)
		f.n += k
		return k, f.err
	}
	f.n += len(p)
	return len(p), nil
}

func TestWriter_WriteErrors(t *testing.T) {
	errDisk := errors.New("disk full")
	set := report.ItemSet{Items: []tract.Item{0, 1, 2, 3}, Supp: 1}
	rule := report.Rule{Head: 0, Body: []tract.Item{1}, Supp: 1, BodySupp: 1, HeadSupp: 1}

	t.Run("error while writing sets", func(t *testing.T) {
		fw := &failWriter{limit: 100000, err: errDisk}
		w := New(fw, Config{Names: testNames, Total: 1, Format: Format{Sep: " ", Info: " (%S)"}})
		var err error
		n := 0
		for ; err == nil && n < 1000000; n++ {
			err = w.ItemSet(set)
		}
		if !errors.Is(err, errDisk) {
			t.Fatalf("ItemSet error %v after %d sets, want %v", err, n, errDisk)
		}
		if bufferSize > fw.limit || n*len("a b c d (100)\n") < fw.limit {
			t.Errorf("error after %d sets, before the limit was reached", n)
		}
		for name, f := range map[string]func() error{
			"ItemSet":    func() error { return w.ItemSet(set) },
			"Rule":       func() error { return w.Rule(rule) },
			"WriteStats": func() error { return w.WriteStats(0, nil) },
			"Flush":      w.Flush,
			"Close":      w.Close,
		} {
			if err := f(); !errors.Is(err, errDisk) {
				t.Errorf("%s after a write error: %v, want %v", name, err, errDisk)
			}
		}
	})

	t.Run("error on close", func(t *testing.T) {
		fw := &failWriter{limit: 0, err: errDisk}
		w := New(fw, Config{Names: testNames, Total: 1, Format: Format{Sep: " ", Imp: " <- "}})
		if err := w.Rule(rule); err != nil {
			t.Fatalf("buffered Rule failed: %v", err)
		}
		if err := w.Close(); !errors.Is(err, errDisk) {
			t.Errorf("Close: %v, want %v", err, errDisk)
		}
		if err := w.Close(); !errors.Is(err, errDisk) {
			t.Errorf("second Close: %v, want %v", err, errDisk)
		}
	})

	t.Run("error on flush", func(t *testing.T) {
		fw := &failWriter{limit: 3, err: errDisk}
		w := New(fw, Config{Names: testNames, Total: 1, Format: Format{Sep: " "}})
		if err := w.ItemSet(set); err != nil {
			t.Fatalf("buffered ItemSet failed: %v", err)
		}
		if err := w.Flush(); !errors.Is(err, errDisk) {
			t.Errorf("Flush: %v, want %v", err, errDisk)
		}
		if err := w.ItemSet(set); !errors.Is(err, errDisk) {
			t.Errorf("ItemSet after failed Flush: %v, want %v", err, errDisk)
		}
	})

	t.Run("closed writer", func(t *testing.T) {
		var out bytes.Buffer
		w := New(&out, Config{Names: testNames, Total: 1})
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := w.ItemSet(set); !errors.Is(err, ErrClosed) {
			t.Errorf("ItemSet after Close: %v, want ErrClosed", err)
		}
		if err := w.Rule(rule); !errors.Is(err, ErrClosed) {
			t.Errorf("Rule after Close: %v, want ErrClosed", err)
		}
		if err := w.Close(); !errors.Is(err, ErrClosed) {
			t.Errorf("second Close: %v, want ErrClosed", err)
		}
	})
}

// TestWriter_ReporterSink drives the Writer from an internal/report
// Reporter (test1.tab's rule c <- b and some sets) to check the Sink
// contract end to end.
func TestWriter_ReporterSink(t *testing.T) {
	var out bytes.Buffer
	w := New(&out, Config{Names: testNames, Total: 10, Format: Format{Sep: " ", Imp: " <- ", Info: " (%a)"}})
	r := report.New(len(testNames), 10, nil)
	r.SetSink(w)
	r.Setup()
	if !r.Add(1, 7) {
		t.Fatal("Add(b) failed")
	}
	if !r.Add(2, 6) {
		t.Fatal("Add(c) failed")
	}
	if err := r.Report(); err != nil {
		t.Fatal(err)
	}
	r.Remove(1)
	if err := r.Report(); err != nil {
		t.Fatal(err)
	}
	r.Remove(1)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if expected := "b c (6)\nb (7)\n"; out.String() != expected {
		t.Errorf("got %q, want %q", out.String(), expected)
	}
}

// TestWriter_Allocs requires zero allocations per written set and rule
// once the line buffer has grown.
func TestWriter_Allocs(t *testing.T) {
	names := make([]string, 64)
	items := make([]tract.Item, 64)
	for i := range names {
		names[i] = strings.Repeat("item", 3) + string(rune('A'+i%26))
		items[i] = tract.Item(i)
	}
	var sink bytes.Buffer
	sink.Grow(1 << 22)
	w := New(&sink, Config{Names: names, Total: 1741, Format: Format{Sep: " ", Imp: " <- ", Info: " (%S) %e %a"}})
	set := report.ItemSet{Items: items, Supp: 160, Eval: 0.123456789}
	rule := report.Rule{Head: 0, Body: items[1:], Supp: 160, BodySupp: 170, HeadSupp: 900, Eval: 1.5}
	_ = w.ItemSet(set) // grow the line buffer
	if a := testing.AllocsPerRun(1000, func() { _ = w.ItemSet(set) }); a != 0 {
		t.Errorf("ItemSet: %v allocations per call", a)
	}
	w.info = " (%X, %C) %l %e"
	if a := testing.AllocsPerRun(1000, func() { _ = w.Rule(rule) }); a != 0 {
		t.Errorf("Rule: %v allocations per call", a)
	}
}

func BenchmarkWriter_ItemSet(b *testing.B) {
	names := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	w := New(io.Discard, Config{Names: names, Total: 3000, Format: Format{Sep: " ", Info: " (%S)"}})
	set := report.ItemSet{Items: []tract.Item{4, 2, 0}, Supp: 1741}
	b.ReportAllocs()
	for b.Loop() {
		_ = w.ItemSet(set)
	}
}
