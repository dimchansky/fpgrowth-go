package difftest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// defaultFpgpspCases is the number of random cases of TestFpgpsp
// (DIFFTEST_FPGPSP_CASES overrides it; -short: at most 300).
const defaultFpgpspCases = 3000

var (
	pspOnce         sync.Once
	pspC, pspGo     string
	pspSkip         string
	pspErr          error
	pspSeedRE       = regexp.MustCompile(`(?m)^using -S[0-9-]+( -Z[0-9]+ \([0-9]+ chain\(s\)\))?\n`)
	pspProgressRE   = regexp.MustCompile(`[ 0-9]{10}\x08{10}`)
	pspSignaturesRE = regexp.MustCompile(`\[([0-9]+) signature\(s\)\]`)
	pspBorderLineRE = regexp.MustCompile(`^[0-9]+(:[0-9]+)*$`)
)

// fpgpspBinaries returns the C oracle (upstream fpgpsp with the patches
// of ../patches/fpgpsp: DIFFTEST_C_FPGPSP, else build/fpgpsp-fixed, built
// with ../build.sh fpgpsp-fixed if missing or DIFFTEST_REBUILD is set)
// and the Go program (DIFFTEST_GO_FPGPSP, else built from ../cmd/fpgpsp).
func fpgpspBinaries(t *testing.T) (c, g string) {
	t.Helper()
	pspOnce.Do(func() {
		refDir, err := filepath.Abs("..")
		if err != nil {
			pspErr = err
			return
		}
		pspC = os.Getenv("DIFFTEST_C_FPGPSP")
		if pspC == "" {
			pspC = filepath.Join(refDir, "build", "fpgpsp-fixed", "fpgrowth", "src", "fpgpsp")
			if _, err := os.Stat(pspC); err != nil || os.Getenv("DIFFTEST_REBUILD") != "" {
				cc := strings.Fields(os.Getenv("CC") + " cc")[0]
				if _, err := exec.LookPath(cc); err != nil {
					pspSkip = fmt.Sprintf("no C compiler %q to build the fpgpsp oracle (set CC, or DIFFTEST_C_FPGPSP)", cc)
					return
				}
				cmd := exec.Command("./build.sh", "fpgpsp-fixed")
				cmd.Dir = refDir
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				if _, err := cmd.Output(); err != nil {
					pspErr = fmt.Errorf("build.sh fpgpsp-fixed: %v\n%s", err, stderr.Bytes())
					return
				}
			}
		}
		pspGo = os.Getenv("DIFFTEST_GO_FPGPSP")
		if pspGo == "" {
			pspGo = filepath.Join(tmpRoot, "go", "fpgpsp")
			cmd := exec.Command("go", "build", "-o", pspGo, "./cmd/fpgpsp")
			cmd.Dir = filepath.Dir(refDir)
			if b, err := cmd.CombinedOutput(); err != nil {
				pspErr = fmt.Errorf("go build ./cmd/fpgpsp: %v\n%s", err, b)
			}
		}
	})
	if pspSkip != "" {
		t.Skip(pspSkip)
	}
	if pspErr != nil {
		t.Fatal(pspErr)
	}
	return pspC, pspGo
}

// pspCase is a random fpgpsp case: a data set and the arguments.
type pspCase struct {
	index  int
	data   string
	sel    string // item selection file, "" for none
	args   []string
	estim  bool  // -ge
	cnt    int64 // -c (the scale of the output is 1/cnt)
	multi  bool  // more than one chain
	stdout bool  // the spectrum goes to standard output
}

// pspData generates a random data set of one of several shapes.
func pspData(r *rand.Rand) (string, [][]string, bool) {
	var rows [][]string
	item := func(i int) string { return "i" + strconv.Itoa(i) }
	table := false
	n := 3 + r.IntN(10)
	switch kind := r.IntN(7); kind {
	case 0, 1: // independent items, some empty transactions
		p := 0.15 + 0.6*r.Float64()
		for range 5 + r.IntN(60) {
			var t []string
			for i := range n {
				if r.Float64() < p {
					t = append(t, item(i))
				}
			}
			rows = append(rows, t)
		}
	case 2: // few distinct transactions, many duplicates
		var pool [][]string
		for range 2 + r.IntN(6) {
			var t []string
			for i := range n {
				if r.IntN(2) == 0 {
					t = append(t, item(i))
				}
			}
			pool = append(pool, t)
		}
		for range 10 + r.IntN(60) {
			rows = append(rows, pool[r.IntN(len(pool))])
		}
	case 3: // a table: column c holds one of v values
		table = true
		cols, v := 1+r.IntN(5), 2+r.IntN(3)
		for range 2 + r.IntN(40) {
			var t []string
			for c := range cols {
				t = append(t, fmt.Sprintf("c%dv%d", c, r.IntN(v)))
			}
			rows = append(rows, t)
		}
	case 4: // dense, with transactions that contain every item (U17)
		for range 5 + r.IntN(40) {
			var t []string
			all := r.IntN(4) == 0
			for i := range n {
				if all || r.Float64() < 0.75 {
					t = append(t, item(i))
				}
			}
			rows = append(rows, t)
		}
	case 5: // an item in every transaction (perfect extension of the root)
		for range 4 + r.IntN(30) {
			t := []string{"u"}
			for i := range n {
				if r.Float64() < 0.4 {
					t = append(t, item(i))
				}
			}
			rows = append(rows, t)
		}
	default: // sparse spike-train-like bins
		for range 20 + r.IntN(80) {
			var t []string
			for i := range n {
				if r.Float64() < 0.12 {
					t = append(t, item(i))
				}
			}
			rows = append(rows, t)
		}
	}
	var b strings.Builder
	for _, t := range rows {
		b.WriteString(strings.Join(t, " "))
		b.WriteByte('\n')
	}
	return b.String(), rows, table
}

// onlyUniversal reports whether every frequent item is in every
// transaction (and there is a frequent item), after the -R selection and
// the -m filter, which mining the surrogates sees: then the maximal
// target hits fpgrowth's deviation M4 (upstream reports nothing). The
// surrogate methods keep the item frequencies (-gr up to rare failed
// repairs), so the original data decides.
func onlyUniversal(rows [][]string, sel map[string]bool, smin int64, zmin int) bool {
	frq := map[string]int64{}
	for _, t := range rows {
		var kept []string
		for _, it := range t {
			if sel == nil || sel[it] {
				kept = append(kept, it)
			}
		}
		if len(kept) < zmin {
			continue
		}
		for _, it := range kept {
			frq[it]++
		}
	}
	n, found := int64(len(rows)), false
	for _, f := range frq {
		if f >= smin {
			if f != n {
				return false
			}
			found = true
		}
	}
	return found
}

// newPspCase builds case i of the base seed: every option of fpgpsp with
// fixed -S and -Z.
func newPspCase(base uint64, i int) *pspCase {
	r := rand.New(rand.NewPCG(base, uint64(i)))
	data, rows, table := pspData(r)
	c := &pspCase{index: i, data: data}
	method := []string{"i", "r", "p", "s", "e", "e"}[r.IntN(6)]
	if table && r.IntN(2) == 0 {
		method = "s"
	}
	c.estim = method == "e"
	a := []string{"-g" + method}
	a = append(a, "-t"+[]string{"s", "s", "c", "m", "g"}[r.IntN(5)])
	switch algo := []string{"c", "c", "s", "d", "t"}[r.IntN(5)]; algo {
	case "t":
		// -At only with -x and where no item is eliminated at the top
		// level (avoidFpgDeviations), else deviation M1 (fpgrowth)
		a = append(a, "-At", "-x")
	default:
		a = append(a, "-A"+algo)
	}
	for _, f := range []string{"-x", "-i", "-u"} {
		if r.IntN(4) == 0 {
			a = append(a, f)
		}
	}
	if r.IntN(4) == 0 {
		a = append(a, "-l"+strconv.Itoa(r.IntN(5)))
	}
	if r.IntN(3) == 0 {
		a = append(a, "-m"+strconv.Itoa(r.IntN(4)))
	}
	if r.IntN(4) == 0 {
		a = append(a, "-n"+strconv.Itoa(1+r.IntN(4)))
	}
	if r.IntN(3) == 0 {
		a = append(a, "-s"+strconv.Itoa(5+r.IntN(60))) // percent
	} else {
		a = append(a, "-s-"+strconv.Itoa(1+r.IntN(6)))
	}
	c.cnt = int64(1 + r.IntN(45))
	if c.estim {
		c.cnt = []int64{1, 10, 100, 1000, 12345}[r.IntN(5)]
		a = append(a, "-e"+[]string{"0", "0.5", "0.5", "1", "1.7", "-1"}[r.IntN(6)])
		a = append(a, "-z"+strconv.Itoa(1+r.IntN(300)))
		a = append(a, "-S"+strconv.FormatUint(uint64(r.Uint32()), 10))
	} else {
		seed := int64(r.Uint32())
		if r.IntN(5) == 0 {
			seed = -int64(r.IntN(1000)) // fpg_genpsp uses negative seeds
		}
		a = append(a, "-S"+strconv.FormatInt(seed, 10))
	}
	a = append(a, "-c"+strconv.FormatInt(c.cnt, 10))
	z := 1 + r.IntN(5)
	a = append(a, "-Z"+strconv.Itoa(z))
	c.multi = z > 1 && c.cnt > 1 && method != "i" && !c.estim
	if r.IntN(3) == 0 {
		a = append(a, "-D")
	}
	if r.IntN(8) == 0 {
		c.sel = "i0\ni2\ni3\nc0v0\nc1v1\nu\n"
		a = append(a, "-R", "sel.txt")
	}
	c.args = a
	avoidFpgDeviations(c, rows)
	c.stdout = r.IntN(2) == 0
	if c.stdout {
		c.args = append(c.args, "data.tab", "-")
	} else {
		c.args = append(c.args, "data.tab", "out.txt")
	}
	return c
}

// keepPspCase writes the files and the command of a failing case into
// dir/fpgpsp-case-<index>/ (the test's artifact directory).
func keepPspCase(t *testing.T, dir string, c *pspCase) {
	d := filepath.Join(dir, fmt.Sprintf("fpgpsp-case-%d", c.index))
	files := map[string]string{"data.tab": c.data, "command": "fpgpsp " + strings.Join(c.args, " ") + "\n"}
	if c.sel != "" {
		files["sel.txt"] = c.sel
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Error(err)
		return
	}
	for name, s := range files {
		if err := os.WriteFile(filepath.Join(d, name), []byte(s), 0o644); err != nil {
			t.Error(err)
		}
	}
}

// avoidFpgDeviations changes options that could trigger a documented
// deviation of the Go miner from upstream's fpgrowth (docs/differences.md)
// when the surrogates are mined: M5 (closed/maximal with -n: -n is
// dropped), M9 (maximal with -x and the 16-items machine: -x is dropped),
// M4 (maximal when every frequent item is a perfect extension of the
// empty set: the target becomes frequent item sets) and M1 (-At).
func avoidFpgDeviations(c *pspCase, rows [][]string) {
	has := func(p string) (string, bool) {
		for _, a := range c.args {
			if v, ok := strings.CutPrefix(a, p); ok {
				return v, true
			}
		}
		return "", false
	}
	drop := func(p string) {
		c.args = slices.DeleteFunc(c.args, func(a string) bool { return strings.HasPrefix(a, p) })
	}
	// M1 (-At copies the item map from the wrong array when an item is
	// eliminated at the top level): -At is used with -x only, and only
	// where no item can be infrequent in a surrogate: identity and pair
	// swaps keep the frequencies of the recoded items exactly, unless
	// the -m filter lowers them (-gr can fail to repair, -gs is unrecoded)
	if a, _ := has("-A"); a == "t" {
		g, _ := has("-g")
		if (g != "i" && g != "p") || c.zmin() > 1 {
			for k, x := range c.args {
				if x == "-At" {
					c.args[k] = "-Ad"
				}
			}
		}
	}
	t, _ := has("-t")
	if t == "c" || t == "m" {
		drop("-n")
	}
	if t != "m" {
		return
	}
	algo, _ := has("-A")
	l, hasL := has("-l")
	if (algo == "s" || algo == "d") && !(hasL && l == "0") {
		drop("-x")
	}
	var sel map[string]bool
	if c.sel != "" {
		sel = map[string]bool{}
		for _, it := range strings.Fields(c.sel) {
			sel[it] = true
		}
	}
	zmin := int(c.zmin())
	sv, _ := has("-s")
	v, _ := strconv.ParseFloat(sv, 64)
	smin := int64(-v)
	if v >= 0 {
		smin = int64(math.Ceil(v / 100 * float64(len(rows)) * (1 - 0x1p-52)))
	}
	if onlyUniversal(rows, sel, max(smin, 1), zmin) {
		for k, a := range c.args {
			if a == "-tm" {
				c.args[k] = "-ts"
			}
		}
	}
}

// pspResult is the outcome of one run.
type pspResult struct {
	exit           int
	stdout, stderr string
	out            string // the output file
	err            string // failure to run
}

func runPsp(bin, dir string, args []string) pspResult {
	ctx, cancel := context.WithTimeout(context.Background(), caseTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Args[0] = "fpgpsp"
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	r := pspResult{stdout: stdout.String(), stderr: string(normStderr(stderr.Bytes()))}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		r.exit = ee.ExitCode()
	default:
		r.err = err.Error()
	}
	if ctx.Err() != nil {
		r.err = "timeout"
	}
	if b, err := os.ReadFile(filepath.Join(dir, "out.txt")); err == nil {
		r.out = string(b)
		os.Remove(filepath.Join(dir, "out.txt"))
	}
	return r
}

// cells parses a spectrum ("size support value" lines) into a map.
func cells(s string) (map[[2]int64]float64, error) {
	m := map[[2]int64]float64{}
	for _, l := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		if l == "" {
			continue
		}
		f := strings.Fields(l)
		if len(f) != 3 {
			return nil, fmt.Errorf("bad line %q", l)
		}
		z, err1 := strconv.ParseInt(f[0], 10, 64)
		c, err2 := strconv.ParseInt(f[1], 10, 64)
		v, err3 := strconv.ParseFloat(f[2], 64)
		if err := errors.Join(err1, err2, err3); err != nil {
			return nil, fmt.Errorf("bad line %q: %v", l, err)
		}
		m[[2]int64{z, c}] = v
	}
	return m, nil
}

// borderOf is the decision border (patch P3) of a parsed spectrum.
func borderOf(m map[[2]int64]float64, zmin int64) string {
	maxs := map[int64]int64{}
	zmax := zmin - 1
	for k := range m {
		if k[0] >= zmin {
			zmax = max(zmax, k[0])
		}
		if s, ok := maxs[k[0]]; !ok || k[1] > s {
			maxs[k[0]] = k[1]
		}
	}
	if zmax < zmin {
		return ""
	}
	b := make([]string, zmax-zmin+1)
	bmin := int64(1)
	for z := zmax; z >= zmin; z-- {
		v := bmin
		if s, ok := maxs[z]; ok && s+1 > v {
			v = s + 1
		}
		b[z-zmin], bmin = strconv.FormatInt(v, 10), v
	}
	return strings.Join(b, ":") + "\n"
}

// splitOutput separates the spectrum and the -D line of a run.
func (c *pspCase) splitOutput(r pspResult) (spec, border string) {
	if !c.stdout {
		return r.out, r.stdout
	}
	s := r.stdout
	if slices.Contains(c.args, "-D") {
		if i := strings.LastIndexByte(strings.TrimSuffix(s, "\n"), '\n'); i >= 0 || s != "" {
			last := s[i+1:]
			if pspBorderLineRE.MatchString(strings.TrimSuffix(last, "\n")) {
				return s[:i+1], last
			}
		}
	}
	return s, ""
}

func (c *pspCase) zmin() int64 {
	z := int64(1)
	for _, a := range c.args {
		if v, ok := strings.CutPrefix(a, "-m"); ok {
			z, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	return z
}

// pspVerdict compares a case; it returns "" for byte-equal outputs,
// "tolerated" for an estimation within the tolerance, or a failure.
func (c *pspCase) compare(cr, gr pspResult) (string, error) {
	if cr.err != "" || gr.err != "" {
		return "", fmt.Errorf("run failed: C %q, Go %q", cr.err, gr.err)
	}
	if cr.exit != gr.exit {
		return "", fmt.Errorf("exit C %d, Go %d\nC: %s\nGo: %s", cr.exit, gr.exit, lastLine([]byte(cr.stderr)), lastLine([]byte(gr.stderr)))
	}
	if runtime.GOOS == "darwin" { // Apple libc's %g keeps zeros of a tie
		cr.stdout, cr.out = normGTies(cr.stdout), normGTies(cr.out)
	}
	cerr, gerr := cr.stderr, pspSeedRE.ReplaceAllString(gr.stderr, "")
	if c.multi { // upstream's progress of several threads is racy (U8)
		cerr, gerr = pspProgressRE.ReplaceAllString(cerr, ""), pspProgressRE.ReplaceAllString(gerr, "")
	}
	if cr.stdout == gr.stdout && cr.out == gr.out && cerr == gerr {
		return "", nil
	}
	if !c.estim {
		return "", fmt.Errorf("outputs differ\n%s", firstDifference(cr, gr, cerr, gerr))
	}
	// estimation: per cell at most one count (1/cnt in output units)
	cs, cb := c.splitOutput(cr)
	gs, gb := c.splitOutput(gr)
	cm, err1 := cells(cs)
	gm, err2 := cells(gs)
	if err := errors.Join(err1, err2); err != nil {
		return "", err
	}
	unit := 1 / float64(c.cnt)
	oneSided := 0
	var differing []string
	for k := range mergeKeys(cm, gm) {
		cv, cok := cm[k]
		gv, gok := gm[k]
		if cok != gok {
			oneSided++
		}
		if cv != gv {
			differing = append(differing, fmt.Sprintf("cell %v: C %g, Go %g", k, cv, gv))
		}
		if math.Abs(cv-gv) > unit*(1+1e-9)+5e-6*(math.Abs(cv)+math.Abs(gv)) {
			return "", fmt.Errorf("cell %v: C %g, Go %g (unit %g)", k, cv, gv, unit)
		}
	}
	// a boundary case moves one count of one cell; more is systematic
	if len(differing) > 1 {
		return "", fmt.Errorf("%d cells differ (at most 1 is tolerated): %v", len(differing), differing)
	}
	// every -D line is the border of its own spectrum
	if slices.Contains(c.args, "-D") {
		if cb != borderOf(cm, c.zmin()) || gb != borderOf(gm, c.zmin()) {
			return "", fmt.Errorf("border: C %q (own %q), Go %q (own %q)", cb, borderOf(cm, c.zmin()), gb, borderOf(gm, c.zmin()))
		}
	}
	// the logs may differ only in the signature counts, by the number of
	// one-sided cells
	csig := pspSignaturesRE.FindAllStringSubmatch(cerr, -1)
	gsig := pspSignaturesRE.FindAllStringSubmatch(gerr, -1)
	if len(csig) != len(gsig) || pspSignaturesRE.ReplaceAllString(cerr, "") != pspSignaturesRE.ReplaceAllString(gerr, "") {
		return "", fmt.Errorf("logs differ beyond the signature counts\nC:  %q\nGo: %q", cerr, gerr)
	}
	for i := range csig {
		a, _ := strconv.Atoi(csig[i][1])
		b, _ := strconv.Atoi(gsig[i][1])
		if d := a - b; d > oneSided || -d > oneSided {
			return "", fmt.Errorf("signature counts C %d, Go %d, %d one-sided cells", a, b, oneSided)
		}
	}
	if len(differing) == 0 {
		return "tolerated: only the border or the log differs", nil
	}
	return "tolerated: " + differing[0], nil
}

// gTieRE matches the exponential form of %g with trailing zeros in the
// mantissa ("1.67960e+06", "2.50000e+06").
var gTieRE = regexp.MustCompile(`\b([0-9]+)(\.[0-9]*?)0+(e[+-][0-9]+)\b`)

// normGTies removes the trailing zeros that Apple's libc prints for %g
// when the rounding to 6 digits is an exact tie (printf("%g", 1679605.0)
// gives "1.67960e+06" on macOS, "1.6796e+06" with glibc and Go; C99 asks
// for the zeros to be removed). Go never prints such zeros, so the
// normalization only changes C outputs. docs/differences.md F-2.
func normGTies(s string) string {
	return gTieRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := gTieRE.FindStringSubmatch(m)
		frac := sub[2]
		if frac == "." {
			frac = ""
		}
		return sub[1] + frac + sub[3]
	})
}

func mergeKeys(a, b map[[2]int64]float64) map[[2]int64]bool {
	k := map[[2]int64]bool{}
	for x := range a {
		k[x] = true
	}
	for x := range b {
		k[x] = true
	}
	return k
}

func firstDifference(cr, gr pspResult, cerr, gerr string) string {
	for _, f := range []struct{ name, c, g string }{
		{"stdout", cr.stdout, gr.stdout}, {"output file", cr.out, gr.out}, {"stderr", cerr, gerr},
	} {
		if f.c == f.g {
			continue
		}
		cl, gl := strings.Split(f.c, "\n"), strings.Split(f.g, "\n")
		for i := range max(len(cl), len(gl)) {
			var a, b string
			if i < len(cl) {
				a = cl[i]
			}
			if i < len(gl) {
				b = gl[i]
			}
			if a != b {
				return fmt.Sprintf("%s line %d: C %q, Go %q", f.name, i+1, a, b)
			}
		}
	}
	return ""
}

// TestFpgpsp compares cmd/fpgpsp with the C oracle (upstream fpgpsp with
// the patches of ../patches/fpgpsp) on random data sets and options. The
// surrogate methods (-gi, -gr, -gp, -gs) with fixed -S and -Z must give
// byte-identical stdout, output file, stderr (times normalized; with
// several chains without upstream's racy progress counter, U8) and exit
// status. The estimation (-ge) depends on log and exp, whose last bits
// differ between Go's math package and the platform's libm; its counts
// are round(v·cnt), and a last-bit difference of v moves a count only when
// v·cnt lies at a rounding boundary, which is rare: in at most one cell
// of a case, by one count (1/cnt in the printed unit, plus the rounding
// of %g; a cell on one side only counts as differing), with the signature
// counts of the log differing by at most the number of one-sided cells
// and each program's -D line the border of its own spectrum; and in at
// most 1 + 0.1% of the estimations. Every tolerated case is logged. DIFFTEST_FPGPSP_CASES sets the number of cases (default
// 3000, -short 300), DIFFTEST_SEED the base seed; DIFFTEST_FPGPSP_ONLY
// runs selected cases (comma-separated indices); with -artifacts the
// files and the command of every failing case are kept.
func TestFpgpsp(t *testing.T) {
	cBin, goBin := fpgpspBinaries(t)
	base := uint64(envInt("DIFFTEST_SEED", 1))
	n := envInt("DIFFTEST_FPGPSP_CASES", defaultFpgpspCases)
	if testing.Short() {
		n = min(n, 300)
	}
	var idx []int
	if only := os.Getenv("DIFFTEST_FPGPSP_ONLY"); only != "" {
		for _, s := range strings.Split(only, ",") {
			i, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				t.Fatal(err)
			}
			idx = append(idx, i)
		}
	} else {
		for i := range n {
			idx = append(idx, i)
		}
	}
	var equal, tolerated, estim, failed atomic.Int64
	var artOnce sync.Once
	var artDir string
	work := make(chan int)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range work {
				c := newPspCase(base, i)
				dir := t.TempDir()
				files := map[string]string{"data.tab": c.data}
				if c.sel != "" {
					files["sel.txt"] = c.sel
				}
				for name, s := range files {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
						t.Error(err)
						return
					}
				}
				cr := runPsp(cBin, dir, c.args)
				gr := runPsp(goBin, dir, c.args)
				v, err := c.compare(cr, gr)
				if c.estim {
					estim.Add(1)
				}
				switch {
				case err != nil:
					failed.Add(1)
					artOnce.Do(func() { artDir = t.ArtifactDir() })
					keepPspCase(t, artDir, c)
					t.Errorf("case %d (DIFFTEST_SEED=%d DIFFTEST_FPGPSP_ONLY=%d): fpgpsp %s\n%v",
						i, base, i, strings.Join(c.args, " "), err)
				case strings.HasPrefix(v, "tolerated"):
					tolerated.Add(1)
					t.Logf("case %d (DIFFTEST_SEED=%d): fpgpsp %s: %s", i, base, strings.Join(c.args, " "), v)
				default:
					equal.Add(1)
				}
			}
		})
	}
	for _, i := range idx {
		work <- i
	}
	close(work)
	wg.Wait()
	t.Logf("%d cases (%d estimations): %d byte-equal, %d within the estimation tolerance, %d failed",
		len(idx), estim.Load(), equal.Load(), tolerated.Load(), failed.Load())
	// A count at a rounding boundary of log/exp is rare (about one in
	// 10^5 estimations on darwin/arm64); more tolerated cases than
	// 1 + 0.1% of the estimations indicate a systematic difference.
	if limit := 1 + estim.Load()/1000; tolerated.Load() > limit {
		t.Errorf("%d estimations within the tolerance, more than the limit %d (1 + 0.1%% of %d)", tolerated.Load(), limit, estim.Load())
	}
}
