package difftest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// defaultCases is the number of cases of a plain `go test` run; it takes
// about 35 s on a 10-core Apple M-series machine (the cases run in
// parallel, GOMAXPROCS workers). DIFFTEST_CASES overrides it.
const defaultCases = 10000

// caseTimeout bounds one program run.
const caseTimeout = 60 * time.Second

// binaries are the two programs under comparison.
type binaries struct {
	c, goBin string
}

var (
	binOnce sync.Once
	bins    binaries
	binSkip string // reason to skip (no C compiler)
	binErr  error
	tmpRoot string
)

func TestMain(m *testing.M) {
	var err error
	tmpRoot, err = os.MkdirTemp("", "difftest-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(tmpRoot)
	os.Exit(code)
}

// getBinaries locates (or builds) the C reference and builds the Go CLI.
func getBinaries(t *testing.T) binaries {
	t.Helper()
	binOnce.Do(func() {
		refDir, err := filepath.Abs("..")
		if err != nil {
			binErr = err
			return
		}
		bins.c, binSkip, binErr = cReference(refDir)
		if binErr != nil || binSkip != "" {
			return
		}
		if p := os.Getenv("DIFFTEST_GO"); p != "" {
			bins.goBin = p // a prebuilt (or instrumented) program under test
			return
		}
		bins.goBin, binErr = buildGo(filepath.Dir(refDir))
	})
	if binSkip != "" {
		t.Skip(binSkip)
	}
	if binErr != nil {
		t.Fatal(binErr)
	}
	return bins
}

// cReference returns the release build of the upstream program: the path
// in DIFFTEST_C, else reference/build/release (built with ../build.sh
// release if it is missing or DIFFTEST_REBUILD is set). Without a C
// compiler it returns a reason to skip.
func cReference(refDir string) (path, skip string, err error) {
	if p := os.Getenv("DIFFTEST_C"); p != "" {
		return p, "", nil
	}
	path = filepath.Join(refDir, "build", "release", "fpgrowth", "src", "fpgrowth")
	if _, err := os.Stat(path); err == nil && os.Getenv("DIFFTEST_REBUILD") == "" {
		return path, "", nil
	}
	cc := strings.Fields(os.Getenv("CC") + " cc")[0]
	if _, err := exec.LookPath(cc); err != nil {
		return "", fmt.Sprintf("no C compiler %q to build the reference (set CC, or DIFFTEST_C to a built fpgrowth)", cc), nil
	}
	cmd := exec.Command("./build.sh", "release")
	cmd.Dir = refDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("build.sh release: %v\n%s", err, stderr.Bytes())
	}
	for _, l := range strings.Split(string(out), "\n") {
		if p, ok := strings.CutPrefix(l, "release "); ok {
			return p, "", nil
		}
	}
	return "", "", fmt.Errorf("build.sh release printed no binary path: %q", out)
}

// buildGo builds cmd/fpgrowth of the root module into a temporary
// directory. The binary is named fpgrowth like upstream's, and both run
// with argv[0] "fpgrowth", so messages carry the same program name.
func buildGo(rootDir string) (string, error) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("go tool not found: %w", err)
	}
	out := filepath.Join(tmpRoot, "go", "fpgrowth")
	cmd := exec.Command(goTool, "build", "-o", out, "./cmd/fpgrowth")
	cmd.Dir = rootDir
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build ./cmd/fpgrowth: %v\n%s", err, b)
	}
	return out, nil
}

// run executes one binary on a case directory and collects its outputs;
// the output files are removed afterwards.
func run(bin, dir string, args []string) *runResult {
	ctx, cancel := context.WithTimeout(context.Background(), caseTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Args[0] = "fpgrowth"
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	r := &runResult{stdout: stdout.Bytes(), stderr: normStderr(stderr.Bytes())}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		r.exit = ee.ExitCode()
		r.signaled = r.exit < 0 // killed by a signal (crash or timeout)
	default:
		r.runErrMsg = err.Error()
	}
	if ctx.Err() != nil {
		r.runErrMsg = "timeout"
	}
	if b, err := os.ReadFile(filepath.Join(dir, outFile)); err == nil {
		r.out, r.hasOut = b, true
	}
	if b, err := os.ReadFile(filepath.Join(dir, pspFile)); err == nil {
		r.psp, r.hasPsp = b, true
	}
	os.Remove(filepath.Join(dir, outFile))
	os.Remove(filepath.Join(dir, pspFile))
	return r
}

// rerunIn returns a rerunFunc that runs both programs in dir.
func (b binaries) rerunIn(dir string) rerunFunc {
	return func(c *tcase) (cr, gr *runResult) {
		args := c.args()
		return run(b.c, dir, args), run(b.goBin, dir, args)
	}
}

// summary aggregates the outcomes of a sweep.
type summary struct {
	mu           sync.Mutex
	name         string // sweep name and seed in the header
	reproduce    string // reproducing command, with %d for the case index
	cases        int
	verdicts     [len(verdictNames)]int
	byteEqual    int            // byte-identical among cases without deviations
	withOutput   int            // passed cases whose Go output has records
	devByID      map[string]int // deviation-verified cases per deviation
	devCAgrees   int            // ... in which C agreed with Go anyway
	devCAgreesBy map[string]int // ... by deviation set
	devFlagged   map[string]int // cases per flagged deviation (any verdict)
	devFull      int            // flagged cases compared with C in full (C passed the brute force)
	devExempt    int            // ... deviation-verified: C differed only in excused channels
	reruns       int            // cases classified by a rerun with the measure in the output
	bfExact      int            // cases whose Go result equals the brute force
	bfSubset     int            // ... is a subset of the brute force (measures)
	recordDevs   int
	tolerated    int
	boundary     int
	maxRel       float64
	maxRelLog    float64
	maxAbsLog    float64
	targets      map[string]int
	errors       map[string]int // messages of the error-equal cases
	examples     []string       // differences of semantic-equal and tolerated cases
	exampleCnt   [len(verdictNames)]int
	devCDiffers  map[string]int // deviation sets of the cases in which C differed
	failures     []string
}

// newSummary returns an empty summary for a sweep.
func newSummary(name, reproduce string) *summary {
	return &summary{name: name, reproduce: reproduce, devByID: map[string]int{}, devFlagged: map[string]int{}, devCAgreesBy: map[string]int{},
		targets: map[string]int{}, errors: map[string]int{}, devCDiffers: map[string]int{}}
}

func (s *summary) add(c *tcase, o *outcome, gr *runResult, artifact string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cases++
	s.verdicts[o.verdict]++
	if len(o.deviations) == 0 && o.byteEqual {
		s.byteEqual++
	}
	if o.verdict != verdictFail && len(bytes.TrimSpace(gr.out)) > 0 {
		s.withOutput++
	}
	for _, d := range o.deviations {
		s.devFlagged[d]++
	}
	if o.verdict == verdictDeviation {
		for _, d := range o.deviations {
			s.devByID[d]++
		}
		if o.cAgrees {
			s.devCAgrees++
			s.devCAgreesBy[strings.Join(o.deviations, "+")]++
		}
	}
	if o.cFull && o.verdict != verdictFail {
		s.devFull++
		if o.cExempt {
			s.devExempt++
		}
	}
	s.reruns += o.reruns
	if o.bfChecked && o.verdict != verdictFail {
		if o.bfSubset {
			s.bfSubset++
		} else {
			s.bfExact++
		}
	}
	s.recordDevs += o.recordDevs
	s.tolerated += o.tolerated
	s.boundary += o.boundary
	s.maxRel = max(s.maxRel, o.maxRel)
	s.maxRelLog = max(s.maxRelLog, o.maxRelLog)
	s.maxAbsLog = max(s.maxAbsLog, o.maxAbsLog)
	switch o.verdict {
	case verdictErrorEqual:
		s.errors[errorKind(gr.stderr)]++
	case verdictSemantic, verdictTolerated:
		if s.exampleCnt[o.verdict] < 4 && o.firstDiff != "" {
			s.exampleCnt[o.verdict]++
			s.examples = append(s.examples, fmt.Sprintf("%s case %d: %s", o.verdict, c.index, o.firstDiff))
		}
	case verdictDeviation:
		if !o.cAgrees {
			s.devCDiffers[strings.Join(o.deviations, "+")]++
		}
	}
	algo := c.algo
	if algo == 0 {
		algo = '-'
	}
	s.targets[fmt.Sprintf("-t%c -A%c", c.target, algo)]++
	if o.verdict == verdictFail {
		msg := fmt.Sprintf("%s\n  deviations %v\n  reproduce: %s\n  files: %s\n  %s",
			c.describe(), o.deviations, fmt.Sprintf(s.reproduce, c.index), artifact, strings.Join(o.failures, "\n  "))
		s.failures = append(s.failures, msg)
	}
}

func (s *summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d cases\n", s.name, s.cases)
	for v, n := range s.verdicts {
		fmt.Fprintf(&b, "  %-20s %6d\n", verdict(v).String()+":", n)
	}
	fmt.Fprintf(&b, "  byte-identical outputs among cases without deviations: %d\n", s.byteEqual)
	fmt.Fprintf(&b, "  passed cases with at least one set/rule written: %d\n", s.withOutput)
	fmt.Fprintf(&b, "  Go checked against the brute force: %d exactly, %d as a subset (evaluation measures)\n", s.bfExact, s.bfSubset)
	fmt.Fprintf(&b, "  tolerated: %d float values within tolerance, %d sets/rules at the threshold, %d records excused by value-level deviations (R4-R9)\n",
		s.tolerated, s.boundary, s.recordDevs)
	fmt.Fprintf(&b, "  largest difference of 17-digit values accepted as equal: %.3g relative;\n", s.maxRel)
	fmt.Fprintf(&b, "    logarithmic measures (%s, b, j): %.3g relative, %.3g absolute\n", logMeasures, s.maxRelLog, s.maxAbsLog)
	fmt.Fprintf(&b, "  cases whose differing counts or records a rerun with the measure explained: %d\n", s.reruns)
	fmt.Fprintf(&b, "  cases with a flagged deviation in which C passed the brute force and was compared in full: %d\n", s.devFull)
	fmt.Fprintf(&b, "    of which C differed only in channels the deviations excuse (deviation-verified): %d\n", s.devExempt)
	fmt.Fprintf(&b, "  deviation-verified cases whose C output the comparison accepted (C failed the brute force, e.g. O3's dropped empty set): %d\n", s.devCAgrees)
	for _, k := range sortedKeys(s.devCAgreesBy) {
		fmt.Fprintf(&b, "    %6d  %s\n", s.devCAgreesBy[k], k)
	}
	fmt.Fprintf(&b, "  deviations: flagged / deviation-verified\n")
	for _, id := range sortedKeys(s.devFlagged) {
		fmt.Fprintf(&b, "    %-6s %6d %6d\n", id, s.devFlagged[id], s.devByID[id])
	}
	fmt.Fprintf(&b, "  deviation-verified cases in which C differed, by deviation set:\n")
	for _, k := range sortedKeys(s.devCDiffers) {
		fmt.Fprintf(&b, "    %6d  %s\n", s.devCDiffers[k], k)
	}
	fmt.Fprintf(&b, "  error-equal cases by message:\n")
	for _, m := range sortedKeys(s.errors) {
		fmt.Fprintf(&b, "    %6d  %s\n", s.errors[m], m)
	}
	if len(s.examples) > 0 {
		fmt.Fprintf(&b, "  examples of accepted differences:\n")
		for _, e := range s.examples {
			fmt.Fprintf(&b, "    %s\n", e)
		}
	}
	fmt.Fprintf(&b, "  cases per target and variant:")
	for i, k := range sortedKeys(s.targets) {
		if i%5 == 0 {
			b.WriteString("\n   ")
		}
		fmt.Fprintf(&b, " %s: %d", k, s.targets[k])
	}
	b.WriteString("\n")
	return b.String()
}

// errorKind returns an error message without file positions and names.
func errorKind(stderr []byte) string {
	m := lastLine(stderr)
	m = regexp.MustCompile(`[0-9]+`).ReplaceAllString(m, "N")
	return m
}

// envInt reads a non-negative integer environment variable.
func envInt(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

// baseSeed is DIFFTEST_SEED (default 1).
func baseSeed() uint64 {
	return uint64(envInt("DIFFTEST_SEED", 1))
}

// caseIndices returns the cases to run: DIFFTEST_ONLY (comma-separated
// indices) or 0..DIFFTEST_CASES-1.
func caseIndices() []int {
	if only := os.Getenv("DIFFTEST_ONLY"); only != "" {
		var idx []int
		for _, f := range strings.Split(only, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil {
				idx = append(idx, n)
			}
		}
		return idx
	}
	n := envInt("DIFFTEST_CASES", defaultCases)
	if testing.Short() {
		n = min(n, 300)
	}
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	return idx
}

// TestDifferential runs the C reference and the Go CLI on random cases
// and compares them (see the package documentation).
func TestDifferential(t *testing.T) {
	b := getBinaries(t)
	indices := caseIndices()
	sum := newSummary(fmt.Sprintf("difftest (base seed %d)", baseSeed()),
		fmt.Sprintf("DIFFTEST_SEED=%d DIFFTEST_ONLY=%%d go test ./difftest/ -run TestDifferential -v -artifacts", baseSeed()))
	work := make(chan int)
	var wg sync.WaitGroup
	root := t.TempDir()
	var artifactOnce sync.Once
	var artifacts string
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range work {
				c, err := newCase(baseSeed(), i)
				if err != nil {
					t.Errorf("case %d: %v", i, err)
					continue
				}
				dir := filepath.Join(root, "case-"+strconv.Itoa(i))
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Error(err)
					continue
				}
				if err := c.writeFiles(dir); err != nil {
					t.Errorf("case %d: %v", i, err)
					continue
				}
				args := c.args()
				cr := run(b.c, dir, args)
				gr := run(b.goBin, dir, args)
				o := evaluate(c, cr, gr, b.rerunIn(dir))
				where := ""
				if o.verdict == verdictFail {
					artifactOnce.Do(func() { artifacts = t.ArtifactDir() })
					where = keepCase(dir, filepath.Join(artifacts, "case-"+strconv.Itoa(i)), c, cr, gr)
				}
				sum.add(c, o, gr, where)
				os.RemoveAll(dir)
			}
		})
	}
	for _, i := range indices {
		work <- i
	}
	close(work)
	wg.Wait()

	report := sum.String()
	t.Log("\n" + report)
	if p := os.Getenv("DIFFTEST_SUMMARY"); p != "" {
		text := report
		for _, f := range sum.failures {
			text += "\n" + f + "\n"
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Error(err)
		}
	}
	slices.Sort(sum.failures)
	for k, f := range sum.failures {
		if k == 20 {
			t.Errorf("... and %d more failures", len(sum.failures)-k)
			break
		}
		t.Errorf("%s", f)
	}
}

// keepCase copies a failing case (inputs, command, both outputs) to dst
// and returns dst.
func keepCase(src, dst string, c *tcase, cr, gr *runResult) string {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err.Error()
	}
	for _, name := range []string{dataFile, selFile} {
		if b, err := os.ReadFile(filepath.Join(src, name)); err == nil {
			os.WriteFile(filepath.Join(dst, name), b, 0o644)
		}
	}
	os.WriteFile(filepath.Join(dst, "command.sh"), []byte(c.command()+"\n"), 0o644)
	for side, r := range map[string]*runResult{"c": cr, "go": gr} {
		os.WriteFile(filepath.Join(dst, side+".out"), r.out, 0o644)
		os.WriteFile(filepath.Join(dst, side+".stdout"), r.stdout, 0o644)
		os.WriteFile(filepath.Join(dst, side+".stderr"), r.stderr, 0o644)
		if r.hasPsp {
			os.WriteFile(filepath.Join(dst, side+".psp"), r.psp, 0o644)
		}
	}
	return dst
}
