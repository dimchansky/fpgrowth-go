// Command gen writes the deterministic benchmark datasets of the
// fpgrowth-go benchmark suite in upstream's table format (or as CSV), so
// that the Go benchmarks and the C reference run on identical input.
//
// Usage:
//
//	go run ./bench/gen -list                 # dataset characteristics (computed)
//	go run ./bench/gen -dataset dense -o dense.tab
//	go run ./bench/gen -dataset dense -csv -o dense.csv
//	go run ./bench/gen -all -dir /tmp/fpgbench     # every dataset as <name>.tab
//
// The datasets are defined by benchSpecs in core.go; the root module's
// benchmarks (benchgen_test.go) generate the same transactions in memory.
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
)

func main() {
	list := flag.Bool("list", false, "print the datasets and their characteristics")
	name := flag.String("dataset", "", "dataset to write")
	out := flag.String("o", "", "output file (default: standard output)")
	csv := flag.Bool("csv", false, "write comma-separated values instead of the table format")
	all := flag.Bool("all", false, "write every dataset to -dir as <name>.tab")
	dir := flag.String("dir", "", "output directory for -all")
	flag.Parse()
	if err := run(*list, *name, *out, *csv, *all, *dir); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run(list bool, name, out string, csv, all bool, dir string) error {
	switch {
	case list:
		return printList(os.Stdout)
	case all:
		if dir == "" {
			return fmt.Errorf("-all needs -dir")
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, s := range benchSpecs {
			ext := ".tab"
			if csv {
				ext = ".csv"
			}
			if err := writeFile(filepath.Join(dir, s.Name+ext), s, csv); err != nil {
				return err
			}
		}
		return nil
	case name != "":
		s, ok := benchSpecByName(name)
		if !ok {
			return fmt.Errorf("unknown dataset %q (see -list)", name)
		}
		if out == "" {
			return benchWrite(os.Stdout, s, csv)
		}
		return writeFile(out, s, csv)
	}
	flag.Usage()
	return fmt.Errorf("nothing to do")
}

func writeFile(path string, s benchSpec, csv bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := benchWrite(f, s, csv); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// countingWriter counts and hashes the bytes written.
type countingWriter struct {
	n int64
	h io.Writer
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	w.h.Write(p)
	return len(p), nil
}

// printList prints each dataset's parameters and measured
// characteristics: average transaction length, density (average length
// divided by the number of distinct items used), table size and the
// SHA-256 prefix of the table bytes.
func printList(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "dataset\ttransactions\titems used\tavg len\tmin len\tmax len\tdensity\tweighted\ttable bytes\tsha256[:12]\t")
	for _, s := range benchSpecs {
		used := make([]bool, s.Items)
		var sum, minLen, maxLen int
		minLen = 1 << 30
		err := benchGenerate(s, func(tx benchTx) error {
			for _, i := range tx.Items {
				used[i] = true
			}
			sum += len(tx.Items)
			minLen = min(minLen, len(tx.Items))
			maxLen = max(maxLen, len(tx.Items))
			return nil
		})
		if err != nil {
			return err
		}
		nused := 0
		for _, u := range used {
			if u {
				nused++
			}
		}
		h := sha256.New()
		cw := &countingWriter{h: h}
		if err := benchWrite(cw, s, false); err != nil {
			return err
		}
		avg := float64(sum) / float64(max(s.Transactions, 1))
		fmt.Fprintf(tw, "%s\t%d\t%d\t%.1f\t%d\t%d\t%.3f\t%v\t%d\t%x\t\n", s.Name, s.Transactions, nused, avg,
			minLen, maxLen, avg/float64(max(nused, 1)), s.MaxWeight > 0, cw.n, h.Sum(nil)[:6])
	}
	return tw.Flush()
}
