package main

// End-to-end benchmarks of the command (in process: option parsing,
// reading the input file, mining, formatting and writing the output).
// The input is a deterministic oracle dataset similar to the "sparse"
// dataset of the root benchmark suite (20,000 transactions over 1,000
// items), written to a temporary file. The out-of-process comparison with
// the C program is reference/bench/compare.sh.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimchansky/fpgrowth-go/internal/oracle"
)

func benchInput(b *testing.B) string {
	b.Helper()
	txs, err := oracle.Generate(oracle.GenConfig{
		Seed: 1, NumTransactions: 20000, NumItems: 1000, MinLen: 4, MaxLen: 12, Skew: 1,
		Patterns: 30, PatternLen: 5, PatternProb: 0.3, Names: oracle.NamesNumbers,
	})
	if err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(b.TempDir(), "sparse.tab")
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	if err := oracle.WriteTable(f, txs, false); err != nil {
		b.Fatal(err)
	}
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}
	return path
}

// BenchmarkCLI runs the program with output to no file (count only), to
// /dev/null and to a file.
func BenchmarkCLI(b *testing.B) {
	in := benchInput(b)
	outFile := filepath.Join(b.TempDir(), "out.txt")
	cases := []struct {
		name string
		opts string
	}{
		{"frequent", "-s0.1"},
		{"closed", "-tc -s0.1"},
		{"maximal", "-tm -s0.1"},
		{"rules", "-tr -s0.1 -c60"},
	}
	for _, c := range cases {
		for _, out := range []struct{ name, path string }{
			{"count", ""}, {"devnull", os.DevNull}, {"file", outFile},
		} {
			args := append([]string{"fpgrowth"}, strings.Fields(c.opts)...)
			args = append(args, in)
			if out.path != "" {
				args = append(args, out.path)
			}
			b.Run(c.name+"/"+out.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					code := run(context.Background(), env{args: args, stdout: io.Discard, stderr: io.Discard})
					if code != exitOK {
						b.Fatalf("exit %d", code)
					}
				}
			})
		}
	}
}
