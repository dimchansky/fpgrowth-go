package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	fpg "github.com/dimchansky/fpgrowth-go"
)

func main() {
	ctx := context.Background()
	t1 := os.Args[1]
	cli := os.Args[2]
	data, _ := os.ReadFile(t1)
	// 1. Fraction(k/100) == -sk
	mism := 0
	for k := 1; k <= 1000; k++ {
		pct := float64(k) / 10
		_, sum, err := fpg.CollectItemsets(ctx, fpg.TableFile(t1, fpg.DefaultTableFormat()), fpg.Options[string]{MinSupport: fpg.Fraction(pct / 100)})
		if err != nil {
			fmt.Println("err", err)
			return
		}
		out, _ := exec.Command(cli, fmt.Sprintf("-s%g", pct), t1).CombinedOutput()
		s := string(out)
		i := strings.Index(s, "writing <null> ... [")
		var n uint64
		if i >= 0 {
			fmt.Sscanf(s[i+len("writing <null> ... ["):], "%d", &n)
		}
		if n != sum.Reported {
			mism++
			if mism < 5 {
				fmt.Println("Fraction mismatch", pct, n, sum.Reported)
			}
		}
	}
	fmt.Println("1. Fraction(k/100) vs -sk, 1000 values, mismatches:", mism)
	// 2. gzip reader
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write(data)
	w.Close()
	src := fpg.TableReader(func() (io.ReadCloser, error) {
		r, err := gzip.NewReader(bytes.NewReader(gz.Bytes()))
		return r, err
	}, "t1.gz", fpg.DefaultTableFormat())
	sets, _, err := fpg.CollectItemsets(ctx, src, fpg.Options[string]{MinSupport: fpg.Count(3)})
	fmt.Println("2. gzip TableReader sets:", len(sets), err)
	// 3. negative weight
	_, _, err = fpg.CollectItemsets(ctx, fpg.FromSlice([]fpg.Transaction[string]{{Items: []string{"a"}, Weight: -1}}), fpg.Options[string]{MinSupport: fpg.Count(1)})
	fmt.Println("3. negative weight:", err)
	// 4. MaxSize < MinSize
	_, _, err = fpg.CollectItemsets(ctx, fpg.FromItems([][]string{{"a", "b"}}), fpg.Options[string]{MinSupport: fpg.Count(1), MinSize: 3, MaxSize: 2})
	fmt.Println("4. MaxSize<MinSize:", err, errors.Is(err, fpg.ErrInvalidOptions))
	// 5. empty db
	sets, sum, err := fpg.CollectItemsets(ctx, fpg.FromItems[string](nil), fpg.Options[string]{MinSupport: fpg.Count(1), IncludeEmpty: true})
	fmt.Println("5. empty DB:", len(sets), sum.FrequentItems, err)
	// 6. algorithms and targets equal sets
	for _, tg := range []fpg.Target{fpg.TargetFrequent, fpg.TargetClosed, fpg.TargetMaximal, fpg.TargetGenerators} {
		var ref map[string]int64
		for _, a := range []fpg.Algorithm{fpg.AlgorithmSimple, fpg.AlgorithmComplex, fpg.AlgorithmSingleTree, fpg.AlgorithmTopDown} {
			sets, _, err := fpg.CollectItemsets(ctx, fpg.TableFile(t1, fpg.DefaultTableFormat()), fpg.Options[string]{Target: tg, MinSupport: fpg.Count(1), Algorithm: a})
			if err != nil {
				fmt.Println(err)
			}
			m := map[string]int64{}
			for _, s := range sets {
				it := append([]string(nil), s.Items...)
				sortStrings(it)
				m[strings.Join(it, " ")] = s.Support
			}
			if ref == nil {
				ref = m
			} else if fmt.Sprint(ref) != fmt.Sprint(m) {
				fmt.Println("6. MISMATCH target", tg, "alg", a)
			}
		}
		fmt.Println("6. target", tg, "sets", len(ref), "all algorithms equal")
	}
}

func sortStrings(a []string) {
	for i := range a {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}
