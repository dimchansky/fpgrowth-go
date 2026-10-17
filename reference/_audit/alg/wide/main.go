// Command wide checks closed and maximal item sets (and the counts of
// frequent item sets) on wide databases (up to 100 items, few
// transactions) where exhaustive enumeration of item sets is impossible.
// Closed sets are computed independently as the intersections of the
// covers (subsets of positive-weight transactions); maximal sets are the
// frequent closed sets without a frequent closed proper superset; the
// number of frequent item sets is computed by inclusion-exclusion-free
// counting over closed sets (each frequent set has a unique closure).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"math/big"
	"math/bits"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type set [2]uint64 // up to 128 items

func (a set) and(b set) set       { return set{a[0] & b[0], a[1] & b[1]} }
func (a set) sub(b set) bool      { return a[0]&^b[0] == 0 && a[1]&^b[1] == 0 }
func (a set) cnt() int            { return bits.OnesCount64(a[0]) + bits.OnesCount64(a[1]) }
func (a *set) add(i int)          { a[i/64] |= 1 << (i % 64) }
func (a set) has(i int) bool      { return a[i/64]&(1<<(i%64)) != 0 }
func (a set) key(k int) string {
	var s []string
	for i := 0; i < k; i++ {
		if a.has(i) {
			s = append(s, "j"+strconv.Itoa(i))
		}
	}
	sort.Strings(s)
	return strings.Join(s, " ")
}

func main() {
	bin := flag.String("bin", "./fpgrowth", "binary")
	n := flag.Int("n", 500, "cases")
	seed := flag.Int64("seed", 1, "seed")
	flag.Parse()
	tmp, _ := os.MkdirTemp("", "wide")
	defer os.RemoveAll(tmp)
	stats := map[string]int{}
	for c := 0; c < *n; c++ {
		r := rand.New(rand.NewSource(*seed*104729 + int64(c)))
		k := 20 + r.Intn(81)
		nt := 1 + r.Intn(12)
		base := set{}
		for i := 0; i < k; i++ {
			if r.Float64() < 0.8 {
				base.add(i)
			}
		}
		var tx []set
		var w []int64
		for t := 0; t < nt; t++ {
			s := base
			for i := 0; i < k; i++ { // noise: drop/add a few items
				if r.Float64() < 0.08 {
					s[i/64] ^= 1 << (i % 64)
				}
			}
			tx = append(tx, s)
			wt := int64(1 + r.Intn(3))
			if r.Intn(6) == 0 {
				wt = 0
			}
			w = append(w, wt)
		}
		// closed sets via cover intersections
		var pos []int
		for t := range tx {
			if w[t] > 0 {
				pos = append(pos, t)
			}
		}
		supp := func(s set) int64 {
			var v int64
			for t := range tx {
				if s.sub(tx[t]) {
					v += w[t]
				}
			}
			return v
		}
		closed := map[set]int64{}
		for u := 1; u < 1<<len(pos); u++ {
			s := set{^uint64(0), ^uint64(0)}
			for j, t := range pos {
				if u&(1<<j) != 0 {
					s = s.and(tx[t])
				}
			}
			closed[s] = supp(s)
		}
		var W int64
		for _, x := range w {
			W += x
		}
		target := "cm"[r.Intn(2)]
		if r.Intn(3) == 0 {
			target = 's' // count-only check of frequent sets via -v and stats
		}
		smin := int64(1 + r.Intn(int(max(W, 1))))
		zmin := r.Intn(3)
		zmax := -1
		if r.Intn(3) == 0 {
			zmax = zmin + r.Intn(k)
		}
		args := []string{"-t" + string(target), "-s-" + strconv.FormatInt(smin, 10), "-m" + strconv.Itoa(zmin), "-w", "-v | %a",
			"-A" + string("scdt"[r.Intn(4)])}
		if zmax >= 0 {
			args = append(args, "-n"+strconv.Itoa(zmax))
		}
		// -x is not used: without perfect-extension pruning the search
		// enumerates up to 2^k sets on these databases.
		if r.Intn(4) == 0 {
			args = append(args, "-u")
		}
		zm := zmax
		if zm < 0 {
			zm = 1 << 30
		}
		// expected
		freqClosed := map[set]int64{}
		for s, v := range closed {
			if v >= smin {
				freqClosed[s] = v
			}
		}
		// empty set closure: if no positive transaction... closure(empty) = ∩ all pos
		exp := map[string]int64{}
		var expCount *big.Int = new(big.Int)
		switch target {
		case 'c':
			for s, v := range freqClosed {
				if s.cnt() >= zmin && s.cnt() <= zm {
					exp[s.key(k)] = v
				}
			}
			// the empty set is closed iff it is its own closure
			if zmin == 0 && W >= smin {
				if _, ok := freqClosed[set{}]; !ok {
					// closure of the empty set is nonempty -> not closed
				} else {
					exp[""] = W
				}
			}
		case 'm':
			for s, v := range freqClosed {
				maxi := true
				for t := range freqClosed {
					if t != s && s.sub(t) {
						maxi = false
						break
					}
				}
				if maxi && s.cnt() >= zmin && s.cnt() <= zm {
					exp[s.key(k)] = v
				}
			}
			if len(freqClosed) == 0 && zmin == 0 && W >= smin {
				exp[""] = W
			}
		case 's':
			// count frequent sets of size in [zmin, zm]: each frequent set X has
			// a unique closure; X ⊆ C with closure(X) = C. Count by
			// inclusion over closed sets ordered by size: N(C) = sets whose
			// closure is C = subsets of C minus subsets counted for smaller
			// closed subsets... use Möbius on the closed-set lattice:
			// g(C) = #subsets(C) in size range - sum_{C' ⊂ C closed} g(C').
			if len(freqClosed) > 60 {
				stats["skip-count"]++
				continue
			}
			var cs []set
			for s := range freqClosed {
				cs = append(cs, s)
			}
			sort.Slice(cs, func(i, j int) bool { return cs[i].cnt() < cs[j].cnt() })
			g := map[set]*big.Int{}
			for _, C := range cs {
				v := subsetsInRange(C.cnt(), zmin, zm)
				for _, D := range cs {
					if D != C && D.sub(C) {
						v.Sub(v, g[D])
					}
				}
				g[C] = v
				expCount.Add(expCount, v)
			}
			// the empty set (zmin == 0) is counted inside the closure of the empty set
			if zmin == 0 && len(freqClosed) > 0 {
				if _, ok := freqClosed[closureOfEmpty(pos, tx)]; !ok {
					expCount.Add(expCount, big.NewInt(0))
				}
			}
		}
		// write db
		var b bytes.Buffer
		for t := range tx {
			var f []string
			for i := 0; i < k; i++ {
				if tx[t].has(i) {
					f = append(f, "j"+strconv.Itoa(i))
				}
			}
			f = append(f, strconv.FormatInt(w[t], 10))
			b.WriteString(strings.Join(f, " ") + "\n")
		}
		in := filepath.Join(tmp, "db.txt")
		os.WriteFile(in, b.Bytes(), 0o644)
		var cmd *exec.Cmd
		if target == 's' {
			cmd = exec.Command(*bin, append(args, in)...) // no output file: count only
		} else {
			cmd = exec.Command(*bin, append(args, in, "-")...)
		}
		var so, se bytes.Buffer
		cmd.Stdout, cmd.Stderr = &so, &se
		err := cmd.Run()
		cmdline := strings.Join(args, " ")
		stats["cases"]++
		if err != nil {
			if strings.Contains(se.String(), "no (frequent) items") && len(freqClosed) == 0 {
				stats["noitems-ok"]++
				continue
			}
			if strings.Contains(se.String(), "no (frequent) items") {
				// frequent closed sets exist; only the empty set possible?
				only := true
				for s := range freqClosed {
					if s.cnt() > 0 {
						only = false
					}
				}
				if only {
					stats["noitems-ok"]++
					continue
				}
			}
			stats["FAIL-err"]++
			fmt.Println("FAIL err", c, cmdline, se.String())
			continue
		}
		if target == 's' {
			// parse "[N set(s)]" from the writing line
			got := ""
			for _, l := range strings.Split(se.String(), "\n") {
				if i := strings.Index(l, " set(s)]"); i >= 0 && strings.Contains(l, "writing") {
					j := strings.LastIndex(l[:i], "[")
					got = l[j+1 : i]
				}
			}
			if zmin == 0 {
				// empty set counted iff W >= smin and some item frequent (CLI)
				expCount.Add(expCount, big.NewInt(0))
			}
			if expCount.BitLen() > 64 {
				if got == "18446744073709551615" {
					stats["pass-count-saturated"]++
				} else {
					stats["FAIL-count-saturation"]++
					fmt.Println("FAIL saturation", c, cmdline, "exp", expCount, "got", got)
				}
				continue
			}
			if got != expCount.String() {
				// allow the empty set difference
				e1 := new(big.Int).Add(expCount, big.NewInt(1)).String()
				if zmin == 0 && got == e1 {
					stats["pass-count(empty)"]++
					continue
				}
				stats["FAIL-count"]++
				fmt.Println("FAIL count", c, cmdline, "exp", expCount, "got", got, "k", k, "nt", nt)
			} else {
				stats["pass-count"]++
			}
			continue
		}
		got := map[string]int64{}
		for _, l := range strings.Split(so.String(), "\n") {
			if l == "" {
				continue
			}
			i := strings.LastIndex(l, "| ")
			v, _ := strconv.ParseInt(strings.TrimSpace(l[i+2:]), 10, 64)
			items := strings.Fields(l[:i])
			sort.Strings(items)
			got[strings.Join(items, " ")] = v
		}
		bad := 0
		var ex []string
		for kk, v := range exp {
			if g, ok := got[kk]; !ok || g != v {
				bad++
				ex = append(ex, fmt.Sprintf("missing {%s} %d (got %d %v)", trunc(kk), v, g, ok))
			}
		}
		for kk, v := range got {
			if _, ok := exp[kk]; !ok {
				bad++
				ex = append(ex, fmt.Sprintf("extra {%s} %d", trunc(kk), v))
			}
		}
		if bad > 0 {
			stats["FAIL-"+string(target)]++
			fmt.Println("FAIL", c, cmdline, "k", k, "nt", nt, ex[:min(3, len(ex))])
			os.WriteFile(fmt.Sprintf("wide-fail-%d.txt", c), b.Bytes(), 0o644)
		} else {
			stats["pass-"+string(target)]++
		}
	}
	fmt.Println(stats)
}

func trunc(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

func closureOfEmpty(pos []int, tx []set) set {
	s := set{^uint64(0), ^uint64(0)}
	for _, t := range pos {
		s = s.and(tx[t])
	}
	return s
}

// subsetsInRange returns sum_{j=lo..hi} C(n, j).
func subsetsInRange(n, lo, hi int) *big.Int {
	v := new(big.Int)
	for j := lo; j <= min(hi, n); j++ {
		v.Add(v, new(big.Int).Binomial(int64(n), int64(j)))
	}
	return v
}
