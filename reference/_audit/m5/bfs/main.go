// Command bfs: differential test of closed/maximal with -n on SPARSE data
// with many items (so the extension checker uses sparse bit-set rows and
// the merge/gallop paths). Reference: level-wise exact counting of all
// sets up to zmax+1 items occurring in transactions; closedness via the
// closure (intersection of all positive-weight transactions containing X),
// maximality via frequent one-item extensions (anti-monotonicity).
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type tx struct {
	items []int
	w     int64
}

func main() {
	n := flag.Int("n", 50, "cases")
	seed := flag.Int64("seed", 1, "seed")
	gobin := flag.String("go", "./fpg", "Go CLI")
	dir := flag.String("dir", "works", "dir")
	flag.Parse()
	os.MkdirAll(*dir, 0o755)
	r := rand.New(rand.NewSource(*seed))
	runs, fails := 0, 0
	variants := [][]string{{"-As", "-l0"}, {"-As"}, {"-Ac"}, {"-Ac", "-l0"}, {"-At"}}
	for c := 0; c < *n; c++ {
		k := 200 + r.Intn(1800)
		T := 500 + r.Intn(15000)
		var pats [][]int
		for p := 0; p < 5+r.Intn(30); p++ {
			var s []int
			for j := 0; j < 2+r.Intn(6); j++ {
				s = append(s, r.Intn(k))
			}
			pats = append(pats, s)
		}
		wmode := r.Intn(3)
		txs := make([]tx, T)
		for t := range txs {
			set := map[int]bool{}
			if r.Intn(3) == 0 {
				for _, j := range pats[r.Intn(len(pats))] {
					if r.Intn(10) != 0 {
						set[j] = true
					}
				}
			}
			L := 1 + r.Intn(3)
			for j := 0; j < L; j++ {
				// Zipf-ish popularity
				set[int(float64(k)*r.Float64()*r.Float64()*r.Float64())] = true
			}
			for j := range set {
				txs[t].items = append(txs[t].items, j)
			}
			sort.Ints(txs[t].items)
			txs[t].w = 1
			switch wmode {
			case 1:
				txs[t].w = int64(r.Intn(3))
			case 2:
				txs[t].w = int64(r.Intn(100000))
			}
		}
		var total int64
		for _, t := range txs {
			total += t.w
		}
		tg := []string{"c", "m"}[r.Intn(2)]
		zmax := 1 + r.Intn(3)
		zmin := 1
		if r.Intn(3) == 0 {
			zmin = 2
		}
		smin := int64(2 + r.Intn(8))
		if wmode == 2 {
			smin = int64(50000 + r.Intn(500000))
		}
		want := ref(txs, tg, smin, zmin, zmax)
		data := filepath.Join(*dir, "d.tab")
		var b strings.Builder
		for _, t := range txs {
			for _, it := range t.items {
				fmt.Fprintf(&b, "x%d ", it)
			}
			fmt.Fprintf(&b, "%d\n", t.w)
		}
		os.WriteFile(data, []byte(b.String()), 0o644)
		opts := []string{"-t" + tg, fmt.Sprintf("-s-%d", smin), fmt.Sprintf("-m%d", zmin), fmt.Sprintf("-n%d", zmax),
			[]string{"-q2", "-q-1", "-q0"}[r.Intn(3)]}
		if r.Intn(3) == 0 {
			opts = append(opts, "-x")
		}
		for _, v := range variants {
			a := append(append(append([]string{}, v...), opts...), "-w", "-v", " (%a)", data, "-")
			got, err := run(*gobin, a)
			runs++
			if err != nil || got != want {
				fails++
				fmt.Printf("FAIL case %d: k=%d T=%d %v err=%v\n", c, k, T, a, err)
				diff(want, got)
			}
		}
		fmt.Printf("case %d k=%d T=%d tg=%s zmax=%d sets=%d\n", c, k, T, tg, zmax, strings.Count(want, "\n")+1)
	}
	fmt.Printf("seed=%d runs=%d fails=%d\n", *seed, runs, fails)
}

func diff(want, got string) {
	w := map[string]bool{}
	for _, l := range strings.Split(want, "\n") {
		w[l] = true
	}
	g := map[string]bool{}
	for _, l := range strings.Split(got, "\n") {
		g[l] = true
		if !w[l] {
			fmt.Println("  extra:", l)
		}
	}
	for l := range w {
		if !g[l] {
			fmt.Println("  missing:", l)
		}
	}
}

func key(s []int) string {
	var b strings.Builder
	for _, x := range s {
		b.WriteString(strconv.Itoa(x))
		b.WriteByte(',')
	}
	return b.String()
}

func combos(items []int, z int, f func([]int)) {
	cur := make([]int, 0, z)
	var rec func(i int)
	rec = func(i int) {
		if len(cur) > 0 {
			f(cur)
		}
		if len(cur) == z {
			return
		}
		for j := i; j < len(items); j++ {
			cur = append(cur, items[j])
			rec(j + 1)
			cur = cur[:len(cur)-1]
		}
	}
	rec(0)
}

func ref(txs []tx, tg string, smin int64, zmin, zmax int) string {
	supp := map[string]int64{}
	sets := map[string][]int{}
	for _, t := range txs {
		combos(t.items, zmax+1, func(s []int) {
			k := key(s)
			supp[k] += t.w
			if _, ok := sets[k]; !ok {
				sets[k] = append([]int{}, s...)
			}
		})
	}
	lists := map[int][]int{}
	for ti, t := range txs {
		if t.w > 0 {
			for _, it := range t.items {
				lists[it] = append(lists[it], ti)
			}
		}
	}
	nonmax := map[string]bool{}
	for k2, s2 := range sets {
		if supp[k2] < smin {
			continue
		}
		for i := range s2 {
			sub := append(append([]int{}, s2[:i]...), s2[i+1:]...)
			nonmax[key(sub)] = true
		}
	}
	var out []string
	for k, s := range sets {
		sx := supp[k]
		if sx < smin || len(s) < zmin || len(s) > zmax {
			continue
		}
		ok := true
		if tg == "c" {
			// closure: intersection of positive-weight transactions containing s
			var clo map[int]bool
			r := s[0]
			for _, it := range s {
				if len(lists[it]) < len(lists[r]) {
					r = it
				}
			}
			for _, ti := range lists[r] {
				t := txs[ti]
				if !contains(t.items, s) {
					continue
				}
				if clo == nil {
					clo = map[int]bool{}
					for _, it := range t.items {
						clo[it] = true
					}
				} else {
					m := map[int]bool{}
					for _, it := range t.items {
						if clo[it] {
							m[it] = true
						}
					}
					clo = m
				}
			}
			ok = len(clo) == len(s)
		} else {
			// any frequent superset implies a frequent one-item extension
			ok = !nonmax[k]
		}
		if !ok {
			continue
		}
		names := make([]string, len(s))
		for i, x := range s {
			names[i] = "x" + strconv.Itoa(x)
		}
		sort.Strings(names)
		out = append(out, strings.Join(names, " ")+":"+strconv.FormatInt(sx, 10))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func contains(a, b []int) bool { // both sorted
	i := 0
	for _, x := range b {
		for i < len(a) && a[i] < x {
			i++
		}
		if i == len(a) || a[i] != x {
			return false
		}
	}
	return true
}

func run(bin string, args []string) (string, error) {
	cmd := exec.Command(bin, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if strings.Contains(errb.String(), "no (frequent) items found") {
			return "", nil
		}
		return "", fmt.Errorf("%v: %s", err, errb.String())
	}
	var lines []string
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		l := sc.Text()
		p := strings.LastIndex(l, " (")
		its := strings.Fields(l[:p])
		sort.Strings(its)
		lines = append(lines, strings.Join(its, " ")+":"+strings.TrimSuffix(l[p+2:], ")"))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n"), nil
}
