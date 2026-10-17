// Command apicheck is an independent brute-force checker for the public
// library API (MineItemsets / MineRules): exact Count thresholds over the
// full int64 range, MinSize/MaxSize/IncludeEmpty, MaxSupport, item
// selection (Items), support border, rule appearances, original support,
// and all algorithm/tuning variants.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"math/rand"
	"os"
	"sort"
	"strings"

	fp "github.com/dimchansky/fpgrowth-go"
)

type db struct {
	k   int
	txs []fp.Transaction[int]
	w   int64
}

func gen(r *rand.Rand, huge bool) db {
	d := db{k: 1 + r.Intn(10)}
	nt := r.Intn(20)
	if huge {
		nt = r.Intn(6)
	}
	p := 0.2 + 0.7*r.Float64()
	pool := [][]int{}
	for i := 0; i < 3; i++ {
		var t []int
		for j := 0; j < d.k; j++ {
			if r.Float64() < p {
				t = append(t, j)
			}
		}
		pool = append(pool, t)
	}
	hc := 0
	for i := 0; i < nt; i++ {
		var t []int
		if r.Intn(2) == 0 {
			t = append(t, pool[r.Intn(3)]...)
		} else {
			for j := 0; j < d.k; j++ {
				if r.Float64() < p {
					t = append(t, j)
				}
			}
		}
		w := int64(1 + r.Intn(3))
		if r.Intn(5) == 0 {
			w = 0
		}
		if huge && r.Intn(2) == 0 {
			w = -1 // placeholder
			hc++
		}
		d.txs = append(d.txs, fp.Transaction[int]{Items: t, Weight: w})
	}
	if hc > 0 {
		base := int64(math.MaxInt64)/int64(hc) - 64
		for i := range d.txs {
			if d.txs[i].Weight == -1 {
				d.txs[i].Weight = base - int64(r.Intn(3))
			}
		}
	}
	for _, t := range d.txs {
		d.w += t.Weight
	}
	return d
}

func supports(d db, keep int) []int64 {
	n := 1 << d.k
	s := make([]int64, n)
	for _, t := range d.txs {
		m := 0
		for _, x := range t.Items {
			m |= 1 << x
		}
		s[m&keep] += t.Weight
	}
	for i := 0; i < d.k; i++ {
		for m := 0; m < n; m++ {
			if m&(1<<i) == 0 {
				s[m] += s[m|1<<i]
			}
		}
	}
	return s
}

func key(m, k int) string {
	var s []string
	for i := 0; i < k; i++ {
		if m&(1<<i) != 0 {
			s = append(s, fmt.Sprint(i))
		}
	}
	return strings.Join(s, ",")
}

func keyItems(it []int) string {
	c := append([]int(nil), it...)
	sort.Ints(c)
	var s []string
	for _, x := range c {
		s = append(s, fmt.Sprint(x))
	}
	return strings.Join(s, ",")
}

func randCount(r *rand.Rand, s []int64) int64 {
	switch r.Intn(5) {
	case 0:
		return 0
	case 1:
		return 1
	default:
		v := s[r.Intn(len(s))]
		return max(0, v+int64(r.Intn(3))-1)
	}
}

func main() {
	n := flag.Int("n", 2000, "cases")
	seed := flag.Int64("seed", 1, "seed")
	flag.Parse()
	stats := map[string]int{}
	fails := 0
	for c := 0; c < *n; c++ {
		r := rand.New(rand.NewSource(*seed*7919 + int64(c)))
		huge := r.Intn(4) == 0
		d := gen(r, huge)
		algs := []fp.Algorithm{fp.AlgorithmAuto, fp.AlgorithmSimple, fp.AlgorithmComplex, fp.AlgorithmSingleTree, fp.AlgorithmTopDown}
		tun := fp.Tuning{
			NoPerfectExtensions: r.Intn(4) == 0, NoReorder: r.Intn(4) == 0, NoHeadUnionTail: r.Intn(4) == 0,
			ItemMachine: []int{0, -1, 1, 3, 16}[r.Intn(5)], ItemOrder: fp.ItemOrder(r.Intn(6)),
		}
		alg := algs[r.Intn(len(algs))]
		all := (1 << d.k) - 1
		var report []string
		if r.Intn(3) > 0 { // item sets
			keep := all
			var items []int
			if r.Intn(4) == 0 {
				keep = r.Intn(all + 1)
				for i := 0; i < d.k; i++ {
					if keep&(1<<i) != 0 {
						items = append(items, i)
					}
				}
				if len(items) == 0 {
					keep = all
				}
			}
			s := supports(d, keep)
			opt := fp.Options[int]{Target: fp.Target(r.Intn(4)), Algorithm: alg, Tuning: tun, Items: items}
			smin := randCount(r, s)
			opt.MinSupport = fp.Count(smin)
			smax := int64(math.MaxInt64)
			if r.Intn(4) == 0 {
				smax = randCount(r, s)
				opt.MaxSupport = fp.Count(smax)
			}
			opt.MinSize = r.Intn(3)
			opt.IncludeEmpty = r.Intn(4) == 0 && opt.MinSize <= 1
			if r.Intn(2) == 0 {
				opt.MaxSize = max(opt.MinSize, 1) + r.Intn(3)
			}
			var border []int64
			if r.Intn(4) == 0 {
				for i := 0; i < 1+r.Intn(3); i++ {
					b := randCount(r, s)
					border = append(border, b)
					opt.Border = append(opt.Border, fp.Count(b))
				}
			}
			zmin := max(opt.MinSize, 1)
			if opt.IncludeEmpty {
				zmin = 0
			}
			zmax := opt.MaxSize
			if zmax == 0 {
				zmax = 1 << 30
			}
			emin := max(smin, 1)
			exp := map[string]int64{}
			for m := 0; m <= all; m++ {
				if m&^keep != 0 {
					continue
				}
				sp := s[m]
				if sp < emin {
					continue
				}
				ok := true
				for i := 0; i < d.k && ok; i++ {
					if keep&(1<<i) == 0 {
						continue
					}
					switch opt.Target {
					case fp.TargetClosed:
						ok = !(m&(1<<i) == 0 && s[m|1<<i] == sp)
					case fp.TargetMaximal:
						ok = !(m&(1<<i) == 0 && s[m|1<<i] >= emin)
					case fp.TargetGenerators:
						ok = !(m&(1<<i) != 0 && s[m&^(1<<i)] == sp)
					}
				}
				cnt := bits.OnesCount(uint(m))
				if !ok || cnt < zmin || cnt > zmax || sp > smax {
					continue
				}
				if bi := cnt - zmin; bi < len(border) && border[bi] > 0 && sp < border[bi] {
					continue
				}
				exp[key(m, d.k)] = sp
			}
			// documented L-2: no frequent item -> nothing reported, not even the empty set
			anyItem := false
			for i := 0; i < d.k; i++ {
				if keep&(1<<i) != 0 && s[1<<i] >= emin {
					anyItem = true
				}
			}
			if !anyItem {
				if _, ok := exp[""]; ok {
					stats["L-2-empty-set-suppressed"]++
				}
				delete(exp, "")
			}
			got := map[string]int64{}
			_, err := fp.MineItemsets(context.Background(), fp.FromSlice(d.txs), opt, func(is fp.Itemset[int]) error {
				k := keyItems(is.Items)
				if _, dup := got[k]; dup {
					report = append(report, "duplicate "+k)
				}
				got[k] = is.Support
				return nil
			})
			if err != nil {
				report = append(report, "error: "+err.Error())
			}
			for k, v := range exp {
				if g, ok := got[k]; !ok || g != v {
					report = append(report, fmt.Sprintf("missing/wrong {%s} exp=%d got=%d(%v)", k, v, g, ok))
				}
			}
			for k, v := range got {
				if _, ok := exp[k]; !ok {
					report = append(report, fmt.Sprintf("extra {%s} %d", k, v))
				}
			}
			stats["itemsets"]++
			if len(report) > 0 {
				fails++
				fmt.Printf("FAIL case %d huge=%v W=%d opt=%+v smin=%d smax=%d border=%v\n  txs=%v\n  %s\n", c, huge, d.w, opt, smin, smax, border, d.txs, strings.Join(report[:min(5, len(report))], "\n  "))
			}
			continue
		}
		// rules
		s := supports(d, all)
		opt := fp.RuleOptions[int]{Algorithm: alg, Tuning: tun}
		smin := randCount(r, s)
		opt.MinSupport = fp.Count(smin)
		smax := int64(math.MaxInt64)
		if r.Intn(4) == 0 {
			smax = randCount(r, s)
			opt.MaxSupport = fp.Count(smax)
		}
		confs := []float64{0, 0.5, 0.8, 1, 1.0 / 3, 2.0 / 3}
		opt.MinConfidence = confs[r.Intn(len(confs))]
		opt.OriginalSupport = r.Intn(3) == 0
		opt.MinSize = r.Intn(3)
		if r.Intn(2) == 0 {
			opt.MaxSize = max(opt.MinSize, 1) + r.Intn(3)
		}
		app := make([]fp.Appearance, d.k)
		if r.Intn(2) == 0 {
			opt.Appearance = map[int]fp.Appearance{}
			opt.DefaultAppearance = fp.Appearance(r.Intn(3)) // both/body/head
			for i := range app {
				app[i] = opt.DefaultAppearance
				if r.Intn(2) == 0 {
					app[i] = fp.Appearance(r.Intn(4))
					opt.Appearance[i] = app[i]
				}
			}
		}
		keep := 0
		for i := range app {
			if app[i] != fp.AppearNone {
				keep |= 1 << i
			}
		}
		s = supports(d, keep)
		emin := max(smin, 1)
		zmin := max(opt.MinSize, 1)
		zmax := opt.MaxSize
		if zmax == 0 {
			zmax = 1 << 30
		}
		confR, _ := new(big.Rat).SetString(fmt.Sprintf("%.10g", opt.MinConfidence))
		exp := map[string][3]int64{}
		for m := 1; m <= all; m++ {
			if m&^keep != 0 || s[m] < 1 {
				continue
			}
			cnt := bits.OnesCount(uint(m))
			if cnt < zmin || cnt > zmax {
				continue
			}
			for h := 0; h < d.k; h++ {
				if m&(1<<h) == 0 || app[h] == fp.AppearBody {
					continue
				}
				bodyOK := true
				for i := 0; i < d.k; i++ {
					if i != h && m&(1<<i) != 0 && app[i] == fp.AppearHead {
						bodyOK = false
					}
				}
				if !bodyOK {
					continue
				}
				bm := m &^ (1 << h)
				sp, bs := s[m], s[bm]
				rs := bs
				if opt.OriginalSupport {
					rs = sp
				}
				if rs < emin || rs > smax || bs < emin {
					continue
				}
				// exact confidence: sp >= bs*conf (decimal intent, e.g. 0.8 = 4/5)
				l := new(big.Rat).SetInt64(sp)
				rr := new(big.Rat).Mul(confR, new(big.Rat).SetInt64(bs))
				if l.Cmp(rr) < 0 {
					// documented float rule: Support >= BodySupport*MinConfidence*(1-2^-52)
					if float64(sp) >= float64(bs)*opt.MinConfidence*(1-0x1p-52) {
						stats["conf-float-tolerance-accepts"]++
						if opt.MinConfidence == 1 {
							stats["conf=1-accepts-conf<1"]++
						}
					} else {
						continue
					}
				}
				exp[fmt.Sprintf("%d<-%s", h, key(bm, d.k))] = [3]int64{sp, bs, s[1<<h]}
			}
		}
		got := map[string][3]int64{}
		_, err := fp.MineRules(context.Background(), fp.FromSlice(d.txs), opt, func(ru fp.Rule[int]) error {
			k := fmt.Sprintf("%d<-%s", ru.Head, keyItems(ru.Body))
			if _, dup := got[k]; dup {
				report = append(report, "duplicate "+k)
			}
			got[k] = [3]int64{ru.Support, ru.BodySupport, ru.HeadSupport}
			return nil
		})
		if err != nil {
			report = append(report, "error: "+err.Error())
		}
		for k, v := range exp {
			if g, ok := got[k]; !ok || g != v {
				report = append(report, fmt.Sprintf("missing/wrong %s exp=%v got=%v(%v)", k, v, g, ok))
			}
		}
		for k, v := range got {
			if _, ok := exp[k]; !ok {
				report = append(report, fmt.Sprintf("extra %s %v", k, v))
			}
		}
		stats["rules"]++
		if len(report) > 0 {
			fails++
			fmt.Printf("FAIL rules case %d huge=%v W=%d opt=%+v app=%v\n  txs=%v\n  %s\n", c, huge, d.w, opt, app, d.txs, strings.Join(report[:min(5, len(report))], "\n  "))
		}
	}
	fmt.Println("stats", stats, "fails", fails)
	// overflow of the total weight must be an error
	_, err := fp.MineItemsets(context.Background(), fp.FromSlice([]fp.Transaction[int]{{Items: []int{1}, Weight: math.MaxInt64}, {Items: []int{1}, Weight: 1}}), fp.Options[int]{MinSupport: fp.Count(1)}, func(fp.Itemset[int]) error { return nil })
	fmt.Println("overflow:", err)
	_, err = fp.MineItemsets(context.Background(), fp.FromSlice([]fp.Transaction[int]{{Items: []int{1}, Weight: -1}}), fp.Options[int]{MinSupport: fp.Count(1)}, func(fp.Itemset[int]) error { return nil })
	fmt.Println("negative:", err, errors.Is(err, fp.ErrInvalidOptions))
	if fails > 0 {
		os.Exit(1)
	}
}
