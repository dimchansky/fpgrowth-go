// Command bf is an independent, definition-based differential tester for
// closed/maximal/generator item sets with size limits (audit M5).
//
// Brute force: enumerate all subsets of the item universe, compute weighted
// support directly from the transactions, decide closedness/maximality
// against ALL proper supersets (not just one-item extensions), then apply
// the size/support filters. Compare against the Go CLI (and optionally C).
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var evalRate int
var focus, wide bool

type tx struct {
	items []string
	w     int64
}

type caseT struct {
	txs   []tx
	names []string
	sel   []string // nil: no selection
	opts  []string
	tgt   byte
	smin  int64 // absolute, effective (>=1)
	smax  int64
	zmin  int
	zmax  int
	desc  string
}

func main() {
	n := flag.Int("n", 1000, "cases")
	seed := flag.Int64("seed", 1, "seed")
	gobin := flag.String("go", "./fpg", "Go CLI")
	cbin := flag.String("c", "", "C CLI (optional, compared only where C is believed exact)")
	dir := flag.String("dir", "work", "work dir")
	big := flag.Bool("big", false, "more items (up to 16)")
	stop := flag.Int("stop", 5, "stop after this many failures")
	flag.IntVar(&evalRate, "eval", 0, "1/N cases use a rule-based evaluation (istree path)")
	flag.BoolVar(&wide, "wide", false, "zmax anywhere up to k")
	flag.BoolVar(&focus, "focus", false, "closed/maximal, small zmax, dense")
	flag.Parse()
	os.MkdirAll(*dir, 0o755)
	rng := rand.New(rand.NewSource(*seed))
	runs, fails, cdiff := 0, 0, 0
	for i := 0; i < *n; i++ {
		c := genCase(rng, *big)
		want := brute(c)
		data := filepath.Join(*dir, "d.tab")
		writeData(data, c)
		args := append([]string{}, c.opts...)
		if c.sel != nil {
			sf := filepath.Join(*dir, "sel.txt")
			os.WriteFile(sf, []byte(strings.Join(c.sel, "\n")+"\n"), 0o644)
			args = append(args, "-R", sf)
		}
		args = append(args, "-w", "-v", " (%a)", data, "-")
		for _, v := range variants {
			a := append(append([]string{}, v...), args...)
			got, err := run(*gobin, a)
			runs++
			if err != nil || got != want {
				fails++
				fmt.Printf("FAIL case %d seed %d: %s %v\nerr=%v\nwant:\n%s\ngot:\n%s\n", i, *seed, c.desc, a, err, want, got)
				d, _ := os.ReadFile(data)
				fmt.Printf("data:\n%s\n", d)
				if fails >= *stop {
					fmt.Printf("runs=%d fails=%d\n", runs, fails)
					os.Exit(1)
				}
			}
			if *cbin != "" {
				cg, _ := run(*cbin, a)
				if cg != want {
					cdiff++
				}
			}
		}
	}
	fmt.Printf("seed=%d cases=%d runs=%d fails=%d cdiff=%d\n", *seed, *n, runs, fails, cdiff)
	if fails > 0 {
		os.Exit(1)
	}
}

var variants = [][]string{
	{"-As", "-l0"}, {"-As", "-l3"}, {"-As"}, {"-Ac"}, {"-Ac", "-l0"}, {"-Ac", "-j"},
	{"-Ac", "-j", "-l0"}, {"-Ad", "-l0"}, {"-Ad"}, {"-At"}, {"-Ac", "-l5"},
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
		if p < 0 {
			return "", fmt.Errorf("bad line %q", l)
		}
		its := strings.Fields(l[:p])
		sort.Strings(its)
		lines = append(lines, strings.Join(its, " ")+":"+strings.TrimSuffix(l[p+2:], ")"))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n"), nil
}

func writeData(path string, c caseT) {
	var b strings.Builder
	for _, t := range c.txs {
		for _, it := range t.items {
			b.WriteString(it)
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%d\n", t.w)
	}
	os.WriteFile(path, []byte(b.String()), 0o644)
}

func genCase(r *rand.Rand, big bool) caseT {
	var c caseT
	k := 2 + r.Intn(9) // 2..10
	if big && r.Intn(2) == 0 {
		k = 11 + r.Intn(6) // 11..16
	}
	perm := r.Perm(26)
	for i := 0; i < k; i++ {
		c.names = append(c.names, string(rune('a'+perm[i])))
	}
	m := 1 + r.Intn(30)
	if r.Intn(4) == 0 {
		m += r.Intn(100)
	}
	if big && r.Intn(3) == 0 {
		m = 200 + r.Intn(3000)
	}
	mode := r.Intn(4)
	pat := [][]int{}
	for p := 0; p < 1+r.Intn(4); p++ {
		var s []int
		for j := 0; j < k; j++ {
			if r.Float64() < 0.6 {
				s = append(s, j)
			}
		}
		pat = append(pat, s)
	}
	wmode := r.Intn(5)
	dens := 0.3 + 0.6*r.Float64()
	if focus {
		dens = 0.6 + 0.35*r.Float64()
		if mode == 1 {
			mode = 0
		}
	}
	for t := 0; t < m; t++ {
		var its []string
		switch mode {
		case 0, 1: // independent, dense or sparse
			d := dens
			if mode == 1 {
				d = 0.1 + 0.25*r.Float64()
			}
			for j := 0; j < k; j++ {
				if r.Float64() < d {
					its = append(its, c.names[j])
				}
			}
		default: // correlated: pattern + noise, drop some
			p := pat[r.Intn(len(pat))]
			for _, j := range p {
				if r.Float64() < 0.85 {
					its = append(its, c.names[j])
				}
			}
			for j := 0; j < k; j++ {
				if r.Float64() < 0.08 {
					its = append(its, c.names[j])
				}
			}
		}
		if len(its) > 0 && r.Intn(10) == 0 { // duplicate item
			its = append(its, its[r.Intn(len(its))])
		}
		r.Shuffle(len(its), func(a, b int) { its[a], its[b] = its[b], its[a] })
		var w int64 = 1
		switch wmode {
		case 1:
			w = int64(r.Intn(4)) // 0..3
		case 2:
			w = int64(r.Intn(1000))
		case 3:
			if r.Intn(3) == 0 {
				w = 0
			} else {
				w = int64(1 + r.Intn(2))
			}
		case 4:
			w = int64(r.Int63n(1 << 40)) // large weights -> many planes
		}
		c.txs = append(c.txs, tx{its, w})
	}
	// selection
	if r.Intn(6) == 0 {
		c.sel = []string{}
		for _, nm := range c.names {
			if r.Intn(4) != 0 {
				c.sel = append(c.sel, nm)
			}
		}
		if len(c.sel) == 0 {
			c.sel = append(c.sel, c.names[0])
		}
	}
	tg := []byte{'c', 'c', 'm', 'm', 'g'}[r.Intn(5)]
	if focus {
		tg = []byte{'c', 'm'}[r.Intn(2)]
	}
	c.tgt = tg
	var total int64
	for _, t := range c.txs {
		total += t.w
	}
	c.zmin = []int{1, 1, 1, 0, 2, 3}[r.Intn(6)]
	c.zmax = c.zmin + r.Intn(3)
	if c.zmax == 0 {
		c.zmax = 1
	}
	if r.Intn(8) == 0 {
		c.zmax = 1
		if c.zmin > 1 {
			c.zmin = 1
		}
	}
	if focus {
		c.zmin = []int{0, 1, 1, 1, 2}[r.Intn(5)]
		c.zmax = max(1, c.zmin+r.Intn(2))
	}
	if wide {
		c.zmin = []int{0, 1, 1, 2, 3}[r.Intn(5)]
		c.zmax = max(1, c.zmin+r.Intn(k+1))
	}
	// absolute support
	maxs := total
	if maxs < 1 {
		maxs = 1
	}
	var smin int64
	opts := []string{"-t" + string(tg)}
	if r.Intn(5) == 0 && total > 0 {
		pct := float64(1+r.Intn(60)) + []float64{0, 0.5, 0.25}[r.Intn(3)]
		v := math.Ceil(pct / 100 * float64(total) * (1 - 0x1p-52))
		smin = int64(v)
		opts = append(opts, "-s"+strconv.FormatFloat(pct, 'f', -1, 64))
	} else {
		smin = 1 + r.Int63n(max64(1, maxs/2))
		if r.Intn(3) == 0 {
			smin = 1 + r.Int63n(3)
		}
		opts = append(opts, fmt.Sprintf("-s-%d", smin))
	}
	if smin < 1 {
		smin = 1
	}
	c.smin = smin
	c.smax = math.MaxInt64
	if r.Intn(8) == 0 {
		c.smax = smin + r.Int63n(max64(1, total))
		opts = append(opts, fmt.Sprintf("-S-%d", c.smax))
	}
	opts = append(opts, fmt.Sprintf("-m%d", c.zmin), fmt.Sprintf("-n%d", c.zmax))
	if r.Intn(3) == 0 {
		opts = append(opts, "-x")
	}
	if r.Intn(4) == 0 {
		opts = append(opts, "-u")
	}
	if evalRate > 0 && tg != 'g' && r.Intn(evalRate) == 0 {
		opts = append(opts, []string{"-eo", "-ec"}[r.Intn(2)], "-d0") // item set tree path
	}
	opts = append(opts, []string{"-q2", "-q1", "-q0", "-q-1", "-q-2"}[r.Intn(5)])
	c.opts = opts
	c.desc = fmt.Sprintf("k=%d m=%d mode=%d wmode=%d", k, m, mode, wmode)
	return c
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// brute computes the expected output purely from the definitions.
func brute(c caseT) string {
	sel := map[string]bool{}
	for _, s := range c.sel {
		sel[s] = true
	}
	idx := map[string]int{}
	var uni []string
	masks := make([]uint32, len(c.txs))
	for i, t := range c.txs {
		for _, it := range t.items {
			if c.sel != nil && !sel[it] {
				continue
			}
			j, ok := idx[it]
			if !ok {
				j = len(uni)
				idx[it] = j
				uni = append(uni, it)
			}
			masks[i] |= 1 << j
		}
	}
	k := len(uni)
	N := 1 << k
	supp := make([]int64, N)
	for i, t := range c.txs {
		supp[masks[i]] += t.w
	}
	for j := 0; j < k; j++ { // superset-sum (zeta) transform
		for X := 0; X < N; X++ {
			if X&(1<<j) == 0 {
				supp[X] += supp[X|1<<j]
			}
		}
	}
	// supMax[X]: max support over ALL proper supersets (exact: every proper
	// superset Y of X contains some X|{j} ⊆ Y). subMin likewise for subsets.
	supMax := make([]int64, N)
	subMin := make([]int64, N)
	for X := N - 1; X >= 0; X-- {
		mx := int64(-1)
		for j := 0; j < k; j++ {
			if X&(1<<j) == 0 {
				Y := X | 1<<j
				mx = max(mx, max(supp[Y], supMax[Y]))
			}
		}
		supMax[X] = mx
	}
	for X := 0; X < N; X++ {
		mn := int64(math.MaxInt64)
		for j := 0; j < k; j++ {
			if X&(1<<j) != 0 {
				Y := X &^ (1 << j)
				mn = min(mn, min(supp[Y], subMin[Y]))
			}
		}
		subMin[X] = mn
	}
	anyFreq := false
	for j := 0; j < k; j++ {
		if supp[1<<j] >= c.smin {
			anyFreq = true
		}
	}
	if !anyFreq {
		return "" // upstream: "no (frequent) items found"
	}
	var out []string
	for X := 0; X < N; X++ {
		sx := supp[X]
		if sx < c.smin {
			continue
		}
		ok := true
		switch {
		case k > 10 && c.tgt == 'c':
			ok = supMax[X] < sx
		case k > 10 && c.tgt == 'm':
			ok = supMax[X] < c.smin && X != 0
		case k > 10 && c.tgt == 'g':
			ok = subMin[X] > sx
		case c.tgt == 'c': // no proper superset (any size) with equal support
			for Y := 0; Y < N && ok; Y++ {
				if Y != X && Y&X == X && supp[Y] == sx {
					ok = false
				}
			}
		case c.tgt == 'm': // no proper superset (any size) that is frequent
			for Y := 0; Y < N && ok; Y++ {
				if Y != X && Y&X == X && supp[Y] >= c.smin {
					ok = false
				}
			}
			if X == 0 {
				ok = false // the empty set is never reported as maximal (B4 behavior)
			}
		case c.tgt == 'g': // every proper subset has larger support
			for Y := 0; Y < N && ok; Y++ {
				if Y != X && Y&X == Y && supp[Y] == sx {
					ok = false
				}
			}
		}
		if !ok {
			continue
		}
		z := popc(X)
		if z < c.zmin || z > c.zmax || sx > c.smax {
			continue
		}
		var its []string
		for j := 0; j < k; j++ {
			if X&(1<<j) != 0 {
				its = append(its, uni[j])
			}
		}
		sort.Strings(its)
		out = append(out, strings.Join(its, " ")+":"+strconv.FormatInt(sx, 10))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func popc(x int) int {
	n := 0
	for ; x != 0; x &= x - 1 {
		n++
	}
	return n
}
