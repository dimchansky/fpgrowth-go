package isrout

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/report"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

func TestAppendStats(t *testing.T) {
	tests := []struct {
		name     string
		count    uint64
		stats    []uint64
		expected string
	}{
		{"nothing reported", 0, nil, "all: 0\n"},
		{"test1.tab", 21, []uint64{0, 5, 9, 6, 1}, "all: 21\n  0: 0\n  1: 5\n  2: 9\n  3: 6\n  4: 1\n"},
		{"large count", 1<<64 - 1, []uint64{1<<64 - 1}, "all: 18446744073709551615\n  0: 18446744073709551615\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(AppendStats(nil, tt.count, tt.stats)); got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestAppendStatsWidth checks the "%3d" size column beyond three digits.
func TestAppendStatsWidth(t *testing.T) {
	stats := make([]uint64, 1001)
	stats[1000] = 1
	got := string(AppendStats(nil, 1, stats))
	lines := strings.Split(got, "\n")
	for _, tt := range []struct {
		line     int
		expected string
	}{
		{1, "  0: 0"}, {10, "  9: 0"}, {11, " 10: 0"}, {100, " 99: 0"}, {101, "100: 0"}, {1001, "1000: 1"},
	} {
		if lines[tt.line] != tt.expected {
			t.Errorf("line %d = %q, want %q", tt.line, lines[tt.line], tt.expected)
		}
	}
}

func TestWriteStats(t *testing.T) {
	var out bytes.Buffer
	if err := WriteStats(&out, 2, []uint64{0, 2}); err != nil {
		t.Fatal(err)
	}
	if expected := "all: 2\n  0: 0\n  1: 2\n"; out.String() != expected {
		t.Errorf("got %q, want %q", out.String(), expected)
	}
	errDisk := errors.New("disk full")
	if err := WriteStats(&failWriter{limit: 3, err: errDisk}, 2, nil); !errors.Is(err, errDisk) {
		t.Errorf("error %v, want %v", err, errDisk)
	}
}

func TestWriteSpectrum(t *testing.T) {
	p := report.NewSpectrum(1, report.MaxSize, 1, report.MaxSupp)
	p.Add(2, 1, 2)
	p.Add(1, 7, 3)
	p.Add(1, 3, 1)
	p.Add(25, 1, 5200300) // the audit's lossy %g example
	p.Add(3, 1000000000, 1000000)
	var out bytes.Buffer
	if err := WriteSpectrum(&out, p); err != nil {
		t.Fatal(err)
	}
	expected := "1 3 1\n1 7 3\n2 1 2\n3 1000000000 1e+06\n25 1 5.2003e+06\n"
	if out.String() != expected {
		t.Errorf("got %q, want %q", out.String(), expected)
	}

	out.Reset()
	if err := WriteSpectrum(&out, nil); err != nil || out.Len() != 0 {
		t.Errorf("nil spectrum: %q, %v", out.String(), err)
	}
}

// TestWriteSpectrumErrors pins deviation O7: write errors, also those of
// the final flush, are returned.
func TestWriteSpectrumErrors(t *testing.T) {
	errDisk := errors.New("disk full")
	small := report.NewSpectrum(1, report.MaxSize, 1, report.MaxSupp)
	small.Add(1, 1, 1)
	if err := WriteSpectrum(&failWriter{limit: 2, err: errDisk}, small); !errors.Is(err, errDisk) {
		t.Errorf("flush error %v, want %v", err, errDisk)
	}
	large := report.NewSpectrum(1, report.MaxSize, 1, report.MaxSupp)
	for s := range 20000 {
		large.Add(1+s%7, tract.Supp(s), 1)
	}
	if err := WriteSpectrum(&failWriter{limit: 70000, err: errDisk}, large); !errors.Is(err, errDisk) {
		t.Errorf("write error %v, want %v", err, errDisk)
	}
}

// TestWriteSpectrumScaled checks psp_report with fpgpsp's scale
// 1/(number of surrogates); the expected lines are upstream fpgpsp's
// output for these counts.
func TestWriteSpectrumScaled(t *testing.T) {
	p := report.NewSpectrum(1, report.MaxSize, 1, report.MaxSupp)
	p.Add(2, 5, 1)
	p.Add(2, 4, 5)
	p.Add(3, 2, 7000000)
	var out bytes.Buffer
	if err := WriteSpectrumScaled(&out, p, 1.0/7); err != nil {
		t.Fatal(err)
	}
	if want := "2 4 0.714286\n2 5 0.142857\n3 2 1e+06\n"; out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}
