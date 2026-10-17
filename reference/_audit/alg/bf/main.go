// Command bf is an independent, definition-based brute-force checker for the
// fpgrowth CLI (Go port, optionally also the C reference). It generates
// adversarial transaction databases and option combinations, runs the
// binary, parses its output and compares it with item sets / rules derived
// by exhaustive enumeration from the textbook definitions.
//
// It deliberately does not import anything from the repository.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"math"
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

var forceHuge, noHuge bool

type DB struct {
	k        int
	trans    [][]int
	w        []int64
	weighted bool
	dups     bool
	shape    string
}

type Opts struct {
	target   byte
	zmin     int
	zmax     int // -1: none
	smin     string
	smax     string // "" none
	conf     string
	orig     bool
	alg      byte
	noPex    bool
	noHut    bool
	noReord  bool
	q        int
	l        int // -1 none
	measure  byte
	agg      byte
	thr      string
	inval    bool
	weighted bool
	rfile    string
	app      []int // per item: 0 none, 1 body, 2 head, 3 both (nil: all 3)
}

func (o Opts) args() []string {
	a := []string{"-t" + string(o.target), "-m" + strconv.Itoa(o.zmin), "-s" + o.smin}
	if o.weighted {
		a = append(a, "-w")
	}
	if o.rfile != "" {
		a = append(a, "-R"+o.rfile)
	}
	if o.zmax >= 0 {
		a = append(a, "-n"+strconv.Itoa(o.zmax))
	}
	if o.smax != "" {
		a = append(a, "-S"+o.smax)
	}
	if o.target == 'r' {
		a = append(a, "-c"+o.conf)
		if o.orig {
			a = append(a, "-o")
		}
	}
	a = append(a, "-A"+string(o.alg), "-q"+strconv.Itoa(o.q))
	if o.noPex {
		a = append(a, "-x")
	}
	if o.noHut {
		a = append(a, "-u")
	}
	if o.noReord {
		a = append(a, "-j")
	}
	if o.l >= 0 {
		a = append(a, "-l"+strconv.Itoa(o.l))
	}
	if o.measure != 0 {
		a = append(a, "-e"+string(o.measure), "-d"+o.thr)
		if o.agg != 0 {
			a = append(a, "-a"+string(o.agg))
		}
		if o.inval {
			a = append(a, "-i")
		}
	}
	if o.target == 'r' {
		a = append(a, "-v | %a %b %h %32e")
	} else {
		a = append(a, "-v | %a %32e")
	}
	return a
}

// ---------------------------------------------------------------- generation

func genDB(r *rand.Rand) DB {
	shapes := []string{"random", "chain", "identical", "planted", "single", "dense", "empty", "dups", "pool", "sparse"}
	d := DB{shape: shapes[r.Intn(len(shapes))]}
	d.k = 1 + r.Intn(12)
	if r.Intn(8) == 0 && (d.shape == "sparse" || d.shape == "planted" || d.shape == "chain" || d.shape == "pool") {
		d.k = 13 + r.Intn(6) // beyond the 16-items machine
	}
	nt := r.Intn(26)
	rs := func(p float64) []int {
		var t []int
		for i := 0; i < d.k; i++ {
			if r.Float64() < p {
				t = append(t, i)
			}
		}
		return t
	}
	switch d.shape {
	case "random":
		p := 0.1 + 0.8*r.Float64()
		for i := 0; i < nt; i++ {
			d.trans = append(d.trans, rs(p))
		}
	case "sparse":
		for i := 0; i < nt; i++ {
			d.trans = append(d.trans, rs(0.15))
		}
	case "chain":
		for i := 0; i < nt; i++ {
			n := r.Intn(d.k + 1)
			t := make([]int, n)
			for j := range t {
				t[j] = j
			}
			d.trans = append(d.trans, t)
		}
	case "identical":
		t := rs(0.6)
		for i := 0; i < nt; i++ {
			d.trans = append(d.trans, t)
		}
		if nt > 0 && r.Intn(2) == 0 {
			d.trans[r.Intn(nt)] = rs(0.5)
		}
	case "planted":
		P := rs(0.7)
		for i := 0; i < nt; i++ {
			t := rs(0.15)
			if r.Intn(2) == 0 {
				t = union(t, P)
			}
			d.trans = append(d.trans, t)
		}
	case "single":
		d.k = 1
		for i := 0; i < nt; i++ {
			d.trans = append(d.trans, rs(0.8))
		}
	case "dense":
		for i := 0; i < nt; i++ {
			d.trans = append(d.trans, rs(0.93))
		}
	case "empty":
		for i := 0; i < nt; i++ {
			if r.Intn(3) == 0 {
				d.trans = append(d.trans, rs(0.5))
			} else {
				d.trans = append(d.trans, nil)
			}
		}
	case "dups":
		d.dups = true
		for i := 0; i < nt; i++ {
			d.trans = append(d.trans, rs(0.5))
		}
	case "pool":
		pool := [][]int{rs(0.5), rs(0.5), rs(0.3), rs(0.7)}
		for i := 0; i < nt; i++ {
			d.trans = append(d.trans, pool[r.Intn(len(pool))])
		}
	}
	// weights
	d.w = make([]int64, len(d.trans))
	wm := r.Intn(10)
	if forceHuge {
		wm = 9
	}
	if noHuge && wm >= 8 {
		wm = 0
	}
	switch {
	case wm < 4:
		for i := range d.w {
			d.w[i] = 1
		}
	case wm < 7:
		d.weighted = true
		for i := range d.w {
			d.w[i] = int64(r.Intn(5))
		}
	case wm < 8:
		d.weighted = true
		for i := range d.w {
			d.w[i] = 1
			if r.Intn(4) == 0 {
				d.w[i] = 0
			}
		}
	default:
		d.weighted = true
		if len(d.trans) > 6 {
			d.trans = d.trans[:6]
			d.w = d.w[:6]
		}
		hc := 0
		huge := make([]bool, len(d.w))
		for i := range d.w {
			if r.Intn(2) == 0 {
				huge[i] = true
				hc++
			}
		}
		if hc > 0 {
			base := int64(math.MaxInt64)/int64(hc) - 64
			for i := range d.w {
				if huge[i] {
					d.w[i] = base - int64(r.Intn(4))
				} else {
					d.w[i] = int64(r.Intn(4))
				}
			}
		} else {
			for i := range d.w {
				d.w[i] = int64(r.Intn(4))
			}
		}
	}
	return d
}

func union(a, b []int) []int {
	m := map[int]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		m[x] = true
	}
	var t []int
	for x := range m {
		t = append(t, x)
	}
	sort.Ints(t)
	return t
}

func (d DB) write(path string, r *rand.Rand) error {
	var b bytes.Buffer
	for i, t := range d.trans {
		var f []string
		for _, x := range t {
			f = append(f, fmt.Sprintf("i%d", x))
			if d.dups && r.Intn(2) == 0 {
				f = append(f, fmt.Sprintf("i%d", x))
			}
		}
		r.Shuffle(len(f), func(a, c int) { f[a], f[c] = f[c], f[a] })
		if d.weighted {
			f = append(f, strconv.FormatInt(d.w[i], 10))
		}
		b.WriteString(strings.Join(f, " "))
		b.WriteByte('\n')
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

// ---------------------------------------------------------------- brute force

type BF struct {
	d    DB
	W    int64
	supp []int64 // indexed by item mask
}

func newBF(d DB) *BF {
	b := &BF{d: d}
	n := 1 << d.k
	b.supp = make([]int64, n)
	for i, t := range d.trans {
		m := 0
		for _, x := range t {
			m |= 1 << x
		}
		b.supp[m] += d.w[i]
		b.W += d.w[i]
	}
	// superset sum
	for i := 0; i < d.k; i++ {
		for m := 0; m < n; m++ {
			if m&(1<<i) == 0 {
				b.supp[m] += b.supp[m|1<<i]
			}
		}
	}
	return b
}

func ceilRat(x *big.Rat) *big.Int {
	q, rem := new(big.Int).QuoRem(x.Num(), x.Denom(), new(big.Int))
	if rem.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}
func floorRat(x *big.Rat) *big.Int {
	q := new(big.Int).Div(x.Num(), x.Denom()) // Euclidean; denom > 0 => floor
	return q
}

func clampI(x *big.Int) int64 {
	if x.Sign() < 0 {
		return 0
	}
	if !x.IsInt64() {
		return math.MaxInt64
	}
	return x.Int64()
}

// exact thresholds from the option strings (definition: percent of the
// total weight, negative = absolute; minimum at least 1).
func exactMin(s string, W int64) int64 {
	v, ok := new(big.Rat).SetString(s)
	if !ok {
		panic(s)
	}
	var t *big.Int
	if v.Sign() < 0 {
		t = ceilRat(new(big.Rat).Neg(v))
	} else {
		t = ceilRat(new(big.Rat).Mul(v, new(big.Rat).SetFrac64(W, 100)))
	}
	return max(clampI(t), 1)
}
func exactMax(s string, W int64) int64 {
	if s == "" {
		return math.MaxInt64
	}
	v, _ := new(big.Rat).SetString(s)
	if v.Sign() < 0 {
		return clampI(floorRat(new(big.Rat).Neg(v)))
	}
	return clampI(floorRat(new(big.Rat).Mul(v, new(big.Rat).SetFrac64(W, 100))))
}

// upstream-style float thresholds (for classification only)
func fsupp(x float64) int64 {
	if !(x > 0) {
		return 0
	}
	if x >= 0x1p63 {
		return math.MaxInt64
	}
	return int64(x)
}
func floatMin(s string, W int64) int64 {
	v, _ := strconv.ParseFloat(s, 64)
	var x float64
	if v < 0 {
		x = -v
	} else {
		x = v / 100.0 * float64(W) * (1 - 0x1p-52)
	}
	return max(fsupp(math.Ceil(x)), 1)
}
func floatMax(s string, W int64) int64 {
	if s == "" {
		return math.MaxInt64
	}
	v, _ := strconv.ParseFloat(s, 64)
	var x float64
	if v < 0 {
		x = -v
	} else {
		x = v / 100.0 * float64(W) * (1 + 0x1p-52)
	}
	return fsupp(math.Floor(x))
}

type Rec struct {
	key  string
	a, b int64 // set support (a), body support (b, rules)
	h    int64
	val  float64
	val2 float64 // definition variant
	hasV bool
}

type Flavor struct {
	floatThr  bool // upstream float threshold formulas
	floatConf bool // float confidence comparison
	floatInv  bool
}

func names(m int, k int) []string {
	var s []string
	for i := 0; i < k; i++ {
		if m&(1<<i) != 0 {
			s = append(s, fmt.Sprintf("i%d", i))
		}
	}
	return s
}

func setKey(items []string) string {
	c := append([]string(nil), items...)
	sort.Strings(c)
	return strings.Join(c, " ")
}

func (b *BF) worst(m byte) float64 {
	if measureDir[m] < 0 {
		return 1
	}
	return 0
}

func (b *BF) ruleVal(o Opts, s, body, h int64, defn bool, fl Flavor) float64 {
	if o.inval {
		var ok bool
		if fl.floatInv {
			ok = float64(s)*float64(b.W) > float64(h)*float64(body)
		} else {
			ok = mul(s, b.W).Cmp(mul(h, body)) > 0
		}
		if !ok {
			return b.worst(o.measure)
		}
	}
	return evalRule(o.measure, s, body, h, b.W, defn)
}

func (b *BF) setVal(o Opts, m int, defn bool, fl Flavor) float64 {
	k := b.d.k
	cnt := bits.OnesCount(uint(m))
	if o.measure == 'b' {
		if cnt <= 1 {
			return 0
		}
		v := math.Log2(float64(b.supp[m]) / float64(b.W))
		for i := 0; i < k; i++ {
			if m&(1<<i) != 0 {
				v -= math.Log2(float64(b.supp[1<<i]) / float64(b.W))
			}
		}
		return v
	}
	if cnt <= 1 {
		return b.worst(o.measure)
	}
	var agg float64
	first := true
	n := 0
	for i := 0; i < k; i++ {
		if m&(1<<i) == 0 {
			continue
		}
		v := b.ruleVal(o, b.supp[m], b.supp[m&^(1<<i)], b.supp[1<<i], defn, fl)
		n++
		if first {
			agg = v
			first = false
			continue
		}
		switch o.agg {
		case 'm':
			if v < agg {
				agg = v
			}
		case 'n':
			if v > agg {
				agg = v
			}
		default:
			agg += v
		}
	}
	if o.agg == 'a' {
		agg /= float64(n)
	}
	return agg
}

func (o Opts) keep(k int) int {
	if o.app == nil {
		return 1<<k - 1
	}
	m := 0
	for i, a := range o.app {
		if a != 0 {
			m |= 1 << i
		}
	}
	return m
}

func (o Opts) ap(i int) int {
	if o.app == nil {
		return 3
	}
	return o.app[i]
}

func parseThr(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v / 100 }

func (b *BF) expected(o Opts, fl Flavor) map[string]Rec {
	W := b.W
	var smin, smax int64
	if fl.floatThr {
		smin, smax = floatMin(o.smin, W), floatMax(o.smax, W)
	} else {
		smin, smax = exactMin(o.smin, W), exactMax(o.smax, W)
	}
	zmax := o.zmax
	if zmax < 0 {
		zmax = 1 << 30
	}
	k := b.d.k
	out := map[string]Rec{}
	thr := parseThr(o.thr)
	dir := float64(measureDir[o.measure])
	if o.measure == 'b' && o.target != 'r' {
		dir = 1
	}
	if o.target == 'r' {
		confR, _ := new(big.Rat).SetString(o.conf)
		cf, _ := strconv.ParseFloat(o.conf, 64)
		cf = cf / 100 * (1 - 0x1p-52)
		keep := o.keep(k)
		for m := 1; m < 1<<k; m++ {
			cnt := bits.OnesCount(uint(m))
			if cnt < o.zmin || cnt > zmax || m&^keep != 0 {
				continue
			}
			s := b.supp[m]
			if s < 1 {
				continue
			}
			for i := 0; i < k; i++ {
				if m&(1<<i) == 0 || o.ap(i)&2 == 0 {
					continue
				}
				bodyOK := true
				for j := 0; j < k; j++ {
					if j != i && m&(1<<j) != 0 && o.ap(j)&1 == 0 {
						bodyOK = false
					}
				}
				if !bodyOK {
					continue
				}
				body := b.supp[m&^(1<<i)]
				rs := body
				if o.orig {
					rs = s
				}
				if rs < smin || rs > smax || body < smin {
					continue
				}
				var cok bool
				if fl.floatConf {
					cok = !(float64(s) < float64(body)*cf)
				} else {
					l := new(big.Rat).SetInt(mul(s, 100))
					r := new(big.Rat).Mul(confR, new(big.Rat).SetInt64(body))
					cok = l.Cmp(r) >= 0
				}
				if !cok {
					continue
				}
				h := b.supp[1<<i]
				rec := Rec{a: s, b: body, h: h}
				if o.measure != 0 {
					rec.val = b.ruleVal(o, s, body, h, false, fl)
					rec.val2 = b.ruleVal(o, s, body, h, true, fl)
					rec.hasV = true
					if dir*rec.val < dir*thr { // NaN passes
						continue
					}
				}
				rec.key = fmt.Sprintf("i%d <- %s", i, setKey(names(m&^(1<<i), k)))
				out[rec.key] = rec
			}
		}
		return out
	}
	keep := o.keep(k)
	for m := 0; m < 1<<k; m++ {
		cnt := bits.OnesCount(uint(m))
		s := b.supp[m]
		if s < smin || m&^keep != 0 {
			continue
		}
		ok := true
		switch o.target {
		case 'c':
			for i := 0; i < k && ok; i++ {
				if keep&(1<<i) != 0 && m&(1<<i) == 0 && b.supp[m|1<<i] == s {
					ok = false
				}
			}
		case 'm':
			for i := 0; i < k && ok; i++ {
				if keep&(1<<i) != 0 && m&(1<<i) == 0 && b.supp[m|1<<i] >= smin {
					ok = false
				}
			}
		case 'g':
			for i := 0; i < k && ok; i++ {
				if m&(1<<i) != 0 && b.supp[m&^(1<<i)] == s {
					ok = false
				}
			}
		}
		if !ok || cnt < o.zmin || cnt > zmax || s > smax {
			continue
		}
		rec := Rec{a: s}
		if o.measure != 0 {
			rec.val = b.setVal(o, m, false, fl)
			rec.val2 = b.setVal(o, m, true, fl)
			rec.hasV = true
			if !(rec.val*dir >= thr*dir) {
				continue
			}
		}
		rec.key = setKey(names(m, k))
		out[rec.key] = rec
	}
	return out
}

// ---------------------------------------------------------------- run/parse

func run(bin, in string, o Opts) (string, string, int, error) {
	cmd := exec.Command(bin, append(o.args(), in, "-")...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	rc := 0
	if ee, ok := err.(*exec.ExitError); ok {
		rc = ee.ExitCode()
		err = nil
	}
	return so.String(), se.String(), rc, err
}

func parse(out string, rules bool) (map[string]Rec, []string) {
	res := map[string]Rec{}
	var probs []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		i := strings.LastIndex(line, " | ")
		var lhs string
		var info string
		if i < 0 {
			if strings.HasPrefix(line, "| ") {
				lhs, info = "", line[2:]
			} else {
				probs = append(probs, "unparsable: "+line)
				continue
			}
		} else {
			lhs, info = line[:i], line[i+3:]
		}
		f := strings.Fields(info)
		var rec Rec
		var key string
		if rules {
			if len(f) < 3 {
				probs = append(probs, "short: "+line)
				continue
			}
			rec.a, _ = strconv.ParseInt(f[0], 10, 64)
			rec.b, _ = strconv.ParseInt(f[1], 10, 64)
			rec.h, _ = strconv.ParseInt(f[2], 10, 64)
			if len(f) > 3 {
				rec.val = pf(f[3])
				rec.hasV = true
			}
			j := strings.Index(lhs, " <-")
			head := lhs[:j]
			body := strings.Fields(lhs[j+3:])
			if len(body) != len(dedup(body)) {
				probs = append(probs, "dup items: "+line)
			}
			key = head + " <- " + setKey(body)
		} else {
			rec.a, _ = strconv.ParseInt(f[0], 10, 64)
			if len(f) > 1 {
				rec.val = pf(f[1])
				rec.hasV = true
			}
			it := strings.Fields(lhs)
			if len(it) != len(dedup(it)) {
				probs = append(probs, "dup items: "+line)
			}
			key = setKey(it)
		}
		if _, dup := res[key]; dup {
			probs = append(probs, "duplicate record: "+line)
		}
		rec.key = key
		res[key] = rec
	}
	return res, probs
}

func pf(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return v
}

func dedup(s []string) []string {
	m := map[string]bool{}
	var r []string
	for _, x := range s {
		if !m[x] {
			m[x] = true
			r = append(r, x)
		}
	}
	return r
}

func close2(a, b float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}
	if math.IsInf(a, 0) || math.IsInf(b, 0) {
		return a == b
	}
	d := math.Abs(a - b)
	return d <= 1e-9+1e-7*math.Max(math.Abs(a), math.Abs(b))
}

// diff compares got against exp; returns membership diffs and value diffs.
func diff(exp, got map[string]Rec, rules bool) (mem []string, num []string, quirk []string) {
	for k, e := range exp {
		g, ok := got[k]
		if !ok {
			mem = append(mem, fmt.Sprintf("missing %q exp=%+v", k, e))
			continue
		}
		if g.a != e.a || (rules && (g.b != e.b || g.h != e.h)) {
			num = append(num, fmt.Sprintf("supp %q exp=%d/%d/%d got=%d/%d/%d", k, e.a, e.b, e.h, g.a, g.b, g.h))
		}
		if e.hasV && g.hasV {
			if !close2(e.val, g.val) {
				if math.IsNaN(e.val) { // not computed (Fisher, large n)
					continue
				}
				num = append(num, fmt.Sprintf("val %q exp=%v got=%v (a=%d b=%d h=%d)", k, e.val, g.val, e.a, e.b, e.h))
			} else if !close2(e.val2, g.val) && !math.IsNaN(e.val2) {
				quirk = append(quirk, fmt.Sprintf("defn %q def=%v got=%v", k, e.val2, g.val))
			}
		}
	}
	for k, g := range got {
		if _, ok := exp[k]; !ok {
			mem = append(mem, fmt.Sprintf("extra %q got=%+v", k, g))
		}
	}
	sort.Strings(mem)
	return
}

func sameKeys(a, b map[string]Rec) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- options

func genOpts(r *rand.Rand, b *BF, mode string) Opts {
	o := Opts{zmax: -1, l: -1, weighted: b.d.weighted}
	targets := "scmgrr"
	switch mode {
	case "sets":
		targets = "scmg"
	case "rules":
		targets = "r"
	}
	o.target = targets[r.Intn(len(targets))]
	o.zmin = r.Intn(4)
	if r.Intn(2) == 0 {
		o.zmax = o.zmin + r.Intn(4)
	}
	// candidate supports from the data, to hit boundaries exactly
	var vals []int64
	for m := range b.supp {
		if b.supp[m] > 0 {
			vals = append(vals, b.supp[m])
		}
	}
	pick := func() int64 {
		if len(vals) == 0 {
			return 1
		}
		return vals[r.Intn(len(vals))]
	}
	pct := func(v int64) string { // 100*v/W as exact-ish decimal
		if b.W == 0 {
			return "10"
		}
		x := new(big.Rat).SetFrac(mul(v, 100), big.NewInt(b.W))
		return x.FloatString(r.Intn(3) * 6)
	}
	sminChoice := func() string {
		switch r.Intn(9) {
		case 0:
			return "0"
		case 1:
			return "100"
		case 2:
			return "-" + strconv.FormatInt(pick(), 10)
		case 3:
			return "-" + strconv.FormatInt(pick(), 10) + ".5"
		case 4, 5:
			return pct(pick())
		case 6:
			return strconv.FormatFloat(r.Float64()*100, 'f', 3, 64)
		case 7:
			return "-1"
		default:
			return "10"
		}
	}
	o.smin = sminChoice()
	if r.Intn(4) == 0 {
		o.smax = sminChoice()
		if o.smax == "0" {
			o.smax = "100"
		}
	}
	o.conf = []string{"0", "50", "80", "100", "33.333333333333336", "66.66666666666667",
		strconv.FormatFloat(r.Float64()*100, 'f', 2, 64)}[r.Intn(7)]
	if r.Intn(3) == 0 && len(vals) > 1 { // exact ratio of two supports
		a, c := pick(), pick()
		if a > c {
			a, c = c, a
		}
		o.conf = new(big.Rat).SetFrac(mul(a, 100), big.NewInt(c)).FloatString(r.Intn(3) * 8)
	}
	o.orig = r.Intn(3) == 0
	o.alg = "scdt"[r.Intn(4)]
	o.noPex = r.Intn(5) == 0
	o.noHut = r.Intn(4) == 0
	o.noReord = r.Intn(4) == 0
	o.q = []int{0, 1, -1, 2, -2}[r.Intn(5)]
	if r.Intn(3) == 0 {
		o.l = r.Intn(17)
	}
	// evaluation
	useEval := r.Intn(2) == 0
	if o.target != 'r' {
		useEval = r.Intn(3) == 0
	}
	if mode == "eval" {
		useEval = true
	}
	if useEval {
		ms := ruleMeasures
		if o.target != 'r' {
			ms += "bbbb"
		}
		for {
			o.measure = ms[r.Intn(len(ms))]
			if strings.IndexByte("fhms", o.measure) >= 0 && b.W > fisherMaxN {
				continue
			}
			break
		}
		if o.target != 'r' && o.measure != 'b' {
			o.agg = "mna"[r.Intn(3)]
		}
		o.inval = r.Intn(5) == 0 && o.measure != 'b'
		o.thr = []string{"0", "10", "50", "100", "1", "5", "200",
			strconv.FormatFloat(r.Float64()*100, 'f', 4, 64)}[r.Intn(8)]
	}
	return o
}

// ---------------------------------------------------------------- main

func main() {
	bin := flag.String("bin", "", "fpgrowth binary under test (Go port)")
	ref := flag.String("ref", "", "optional C reference binary")
	n := flag.Int("n", 200, "number of cases")
	seed := flag.Int64("seed", 1, "seed")
	mode := flag.String("mode", "all", "all|sets|rules|eval")
	failDir := flag.String("fails", "fails", "directory for failing cases")
	verbose := flag.Bool("v", false, "verbose")
	flag.BoolVar(&noHuge, "nohuge", false, "no huge weights")
	flag.BoolVar(&forceHuge, "huge", false, "force huge weights")
	flag.Parse()
	tmp, _ := os.MkdirTemp("", "bf")
	defer os.RemoveAll(tmp)
	os.MkdirAll(*failDir, 0o755)
	stats := map[string]int{}
	examples := map[string][]string{}
	note := func(cat, ex string) {
		stats[cat]++
		if len(examples[cat]) < 4 {
			examples[cat] = append(examples[cat], ex)
		}
	}
	for c := 0; c < *n; c++ {
		r := rand.New(rand.NewSource(*seed*1000003 + int64(c)))
		d := genDB(r)
		bf := newBF(d)
		o := genOpts(r, bf, *mode)
		in := filepath.Join(tmp, "db.txt")
		if err := d.write(in, r); err != nil {
			panic(err)
		}
		appText := ""
		if r.Intn(4) == 0 {
			o.app = make([]int, d.k)
			var lines []string
			if o.target == 'r' {
				words := [][]string{{"-", "n", "none", "neither", "ignore"}, {"i", "in", "a", "ante", "b", "body", "antecedent"},
					{"o", "out", "c", "cons", "h", "head", "consequent"}, {"io", "x", "both", "bh", "inout", "a&c"}}
				def := []int{3, 3, 1, 2, 0}[r.Intn(5)]
				lines = append(lines, words[def][r.Intn(len(words[def]))])
				for i := range o.app {
					o.app[i] = def
					if r.Intn(2) == 0 {
						o.app[i] = r.Intn(4)
						lines = append(lines, fmt.Sprintf("i%d %s", i, words[o.app[i]][r.Intn(len(words[o.app[i]]))]))
					}
				}
			} else {
				for i := range o.app {
					if r.Intn(3) > 0 {
						o.app[i] = 3
						lines = append(lines, fmt.Sprintf("i%d", i))
					}
				}
			}
			appText = strings.Join(lines, "\n") + "\n"
			o.rfile = filepath.Join(tmp, "app.txt")
			os.WriteFile(o.rfile, []byte(appText), 0o644)
		}
		cmdline := strings.ReplaceAll("fpgrowth "+quoteArgs(o.args())+" db.txt -", tmp+"/", "")
		if appText != "" {
			cmdline += "  [app.txt: " + strings.ReplaceAll(appText, "\n", "; ") + "]"
		}
		gout, gerr, grc, err := run(*bin, in, o)
		if err != nil {
			panic(err)
		}
		stats["cases"]++
		stats["target-"+string(o.target)]++
		save := func(cat string, detail []string) {
			p := filepath.Join(*failDir, fmt.Sprintf("s%d-c%d-%s", *seed, c, cat))
			os.MkdirAll(p, 0o755)
			data, _ := os.ReadFile(in)
			os.WriteFile(filepath.Join(p, "db.txt"), data, 0o644)
			if appText != "" {
				os.WriteFile(filepath.Join(p, "app.txt"), []byte(appText), 0o644)
			}
			os.WriteFile(filepath.Join(p, "cmd.txt"), []byte(cmdline+"\nshape="+d.shape+fmt.Sprintf(" W=%d\n", bf.W)+strings.Join(detail, "\n")+"\n--- go stdout\n"+gout+"\n--- go stderr\n"+gerr), 0o644)
		}
		if grc != 0 {
			// The only acceptable error for our generated inputs: no items.
			if strings.Contains(gerr, "no (frequent) items found") {
				exp := bf.expected(o, Flavor{})
				_ = exp
				stats["rc-noitems"]++
				// check: no frequent item at all
				smin := exactMin(o.smin, bf.W)
				if o.target == 'r' && !o.orig {
					smin = 1 // body-support rules: item threshold ceil(smin*conf)
				}
				anyFreq := false
				for i := 0; i < d.k; i++ {
					if o.keep(d.k)&(1<<i) != 0 && bf.supp[1<<i] >= smin {
						anyFreq = true
					}
				}
				if anyFreq && o.target != 'r' {
					note("FAIL-noitems-but-frequent", cmdline)
					save("noitems", nil)
				}
				continue
			}
			if strings.Contains(gerr, "item expected") && strings.TrimSpace(appText) == "" {
				stats["rc-empty-selection-file"]++
				continue
			}
			note("FAIL-rc", fmt.Sprintf("%s rc=%d %s", cmdline, grc, lastLine(gerr)))
			save("rc", []string{gerr})
			continue
		}
		got, probs := parse(gout, o.target == 'r')
		for _, p := range probs {
			note("FAIL-format", cmdline+" :: "+p)
		}
		exp := bf.expected(o, Flavor{})
		mem, num, quirk := diff(exp, got, o.target == 'r')
		if len(quirk) > 0 {
			note("quirk-measure-"+string(o.measure), cmdline+" :: "+quirk[0])
		}
		cat := ""
		if len(mem) > 0 || len(num) > 0 {
			// classify with upstream float semantics
			for _, fl := range []Flavor{{floatThr: true}, {floatConf: true}, {floatInv: true}, {true, true, true}} {
				e2 := bf.expected(o, fl)
				m2, n2, _ := diff(e2, got, o.target == 'r')
				if len(m2) == 0 && len(n2) == 0 {
					cat = fmt.Sprintf("fp-boundary%+v", fl)
					break
				}
			}
			if cat == "" {
				// evaluation boundary: all membership diffs within tol of thr
				if o.measure != 0 && len(num) == 0 && allNearThr(mem, exp, got, parseThr(o.thr)) {
					cat = "eval-boundary"
				}
			}
			if cat == "" {
				cat = "FAIL-mismatch"
				det := append(mem, num...)
				e2 := bf.expected(o, Flavor{true, true, true})
				m2, n2, _ := diff(e2, got, o.target == 'r')
				det = append(det, "--- vs float flavor:")
				det = append(det, append(m2, n2...)...)
				save("mismatch", det)
			}
			ex := cmdline
			if len(mem) > 0 {
				ex += " :: " + mem[0]
			} else {
				ex += " :: " + num[0]
			}
			note(cat, ex)
		} else {
			stats["pass"]++
		}
		if *ref != "" {
			cout, cerr, crc, _ := run(*ref, in, o)
			if crc != 0 {
				note("ref-rc", fmt.Sprintf("%s rc=%d %s", cmdline, crc, lastLine(cerr)))
			} else {
				cgot, _ := parse(cout, o.target == 'r')
				if !sameKeys(cgot, got) {
					cm, _, _ := diff(exp, cgot, o.target == 'r')
					if len(cm) == 0 {
						note("ref-differs:C=def", cmdline+fmt.Sprintf(" W=%d", bf.W))
						save("cdef", append(mem, "--- C stdout", cout))
					} else if len(mem) == 0 {
						note("ref-differs:Go=def", cmdline+" :: C "+cm[0])
					} else {
						note("ref-differs:neither", cmdline)
					}
				} else {
					stats["ref-same"]++
				}
			}
		}
		if *verbose {
			fmt.Println(c, cmdline, len(exp), len(got), cat)
		}
	}
	var ks []string
	for k := range stats {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%-40s %d\n", k, stats[k])
		for _, e := range examples[k] {
			fmt.Printf("    e.g. %s\n", trunc(e, 400))
		}
	}
}

func allNearThr(mem []string, exp, got map[string]Rec, thr float64) bool {
	for _, m := range mem {
		k := m[strings.Index(m, "\"")+1:]
		k = k[:strings.Index(k, "\"")]
		var v float64
		if e, ok := exp[k]; ok {
			v = e.val
		} else {
			v = got[k].val
		}
		if !(math.Abs(v-thr) <= 1e-9*math.Max(1, math.Abs(thr))) {
			return false
		}
	}
	return true
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func quoteArgs(a []string) string {
	var s []string
	for _, x := range a {
		if strings.ContainsAny(x, " |%") {
			s = append(s, "'"+x+"'")
		} else {
			s = append(s, x)
		}
	}
	return strings.Join(s, " ")
}
