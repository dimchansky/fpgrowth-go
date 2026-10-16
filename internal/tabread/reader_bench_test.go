package tabread_test

// Parsing-only benchmarks of the table reader (no mining). The input is a
// deterministic oracle dataset written in upstream's table format and held
// in memory, so no file system cost is measured.

import (
	"bytes"
	"io"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
	"github.com/dimchansky/fpgrowth-go/internal/tabread"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// benchTable returns a table of 50,000 transactions (4 to 16 items over
// 1,000 numerically named items, Zipf skew 1), weighted or not.
func benchTable(tb testing.TB, weighted bool) []byte {
	tb.Helper()
	cfg := oracle.GenConfig{
		Seed: 7, NumTransactions: 50000, NumItems: 1000, MinLen: 4, MaxLen: 16,
		Skew: 1, Names: oracle.NamesNumbers,
	}
	if weighted {
		cfg.Weights = oracle.WeightsSmall
	}
	txs, err := oracle.Generate(cfg)
	if err != nil {
		tb.Fatal(err)
	}
	var buf bytes.Buffer
	if err := oracle.WriteTable(&buf, txs, weighted); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// BenchmarkReaderNext tokenizes every record with Reader.Next (byte
// fields, no string conversion).
func BenchmarkReaderNext(b *testing.B) {
	for _, weighted := range []bool{false, true} {
		name := "unweighted"
		if weighted {
			name = "weighted"
		}
		data := benchTable(b, weighted)
		cfg := tabread.DefaultConfig()
		cfg.Weighted = weighted
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			var records int
			for b.Loop() {
				r := tabread.NewReader(bytes.NewReader(data), "bench", cfg)
				records = 0
				for {
					_, _, err := r.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
					records++
				}
			}
			b.ReportMetric(float64(records), "records")
		})
	}
}

// BenchmarkScan runs one pass of the scan function that the CLI and
// TableFile use (records as string slices).
func BenchmarkScan(b *testing.B) {
	data := benchTable(b, false)
	open := func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	scan := tabread.Scan(open, "bench", tabread.DefaultConfig())
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		n := 0
		err := scan(func(items []string, _ tract.Supp) bool { n += len(items); return true })
		if err != nil {
			b.Fatal(err)
		}
	}
}
