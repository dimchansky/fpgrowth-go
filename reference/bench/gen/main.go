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

// datasetStats are the measured characteristics of a dataset.
type datasetStats struct {
	used           int // number of distinct items used
	sum            int // total transaction length
	minLen, maxLen int
}

// listStats measures s. The items used are counted without assuming that
// they are below s.Items: the probe trap (benchSpec.Trap) numbers its
// filler items beyond it.
func listStats(s benchSpec) (datasetStats, error) {
	st := datasetStats{minLen: 1 << 30}
	var used []bool
	err := benchGenerate(s, func(tx benchTx) error {
		for _, i := range tx.Items {
			if int(i) >= len(used) {
				used = append(used, make([]bool, int(i)+1-len(used))...)
			}
			if !used[i] {
				used[i] = true
				st.used++
			}
		}
		st.sum += len(tx.Items)
		st.minLen = min(st.minLen, len(tx.Items))
		st.maxLen = max(st.maxLen, len(tx.Items))
		return nil
	})
	return st, err
}

// printList prints each dataset's parameters and measured
// characteristics: average transaction length, density (average length
// divided by the number of distinct items used), table size and the
// SHA-256 prefix of the table bytes.
func printList(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "dataset\ttransactions\titems used\tavg len\tmin len\tmax len\tdensity\tweighted\ttable bytes\tsha256[:12]\t")
	for _, s := range benchSpecs {
		st, err := listStats(s)
		if err != nil {
			return err
		}
		nused, sum, minLen, maxLen := st.used, st.sum, st.minLen, st.maxLen
		h := sha256.New()
		cw := &countingWriter{h: h}
		if err := benchWrite(cw, s, false); err != nil {
			return err
		}
		avg := float64(sum) / float64(max(s.Transactions, 1))
		fmt.Fprintf(tw, "%s\t%d\t%d\t%.1f\t%d\t%d\t%.3f\t%v\t%d\t%x\t\n", s.Name, s.Transactions, nused, avg,
			minLen, maxLen, avg/float64(max(nused, 1)), s.weighted(), cw.n, h.Sum(nil)[:6])
	}
	return tw.Flush()
}
