package difftest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// defaultLarge is the number of large cases of a plain `go test` run
// (about 5 s on a 10-core Apple M-series machine); DIFFTEST_LARGE
// overrides it, and -short runs at most 6.
const defaultLarge = 300

// largeIndices returns the large cases to run: DIFFTEST_LARGE_ONLY
// (comma-separated indices) or 0..DIFFTEST_LARGE-1.
func largeIndices() []int {
	if only := os.Getenv("DIFFTEST_LARGE_ONLY"); only != "" {
		var idx []int
		for _, f := range strings.Split(only, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil {
				idx = append(idx, n)
			}
		}
		return idx
	}
	n := envInt("DIFFTEST_LARGE", defaultLarge)
	if testing.Short() {
		n = min(n, 6)
	}
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	return idx
}

// TestLarge compares the C reference and the Go CLI on large random
// datasets (40-1000 items, 500-8000 transactions; Zipf-like, equal
// frequency and block generators) for all targets and variants, without
// the brute force: see large.go. DIFFTEST_LARGE sets the number of cases,
// DIFFTEST_SEED the base seed, DIFFTEST_LARGE_SUMMARY a summary file.
func TestLarge(t *testing.T) {
	indices := largeIndices()
	if len(indices) == 0 {
		t.Skip("DIFFTEST_LARGE=0")
	}
	b := getBinaries(t)
	sum := newSummary(fmt.Sprintf("difftest large (base seed %d)", baseSeed()),
		fmt.Sprintf("DIFFTEST_SEED=%d DIFFTEST_LARGE_ONLY=%%d go test ./difftest/ -run TestLarge -v -artifacts", baseSeed()))
	gens := map[string]int{}
	var outBytes int64
	work := make(chan int)
	var wg sync.WaitGroup
	root := t.TempDir()
	var artifactOnce sync.Once
	var artifacts string
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range work {
				lc, err := newLargeCase(baseSeed(), i)
				if err != nil {
					t.Errorf("large case %d: %v", i, err)
					continue
				}
				c := lc.tcase
				dir := filepath.Join(root, "large-"+strconv.Itoa(i))
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Error(err)
					continue
				}
				if err := c.writeFiles(dir); err != nil {
					t.Errorf("large case %d: %v", i, err)
					continue
				}
				args := c.args()
				cr := run(b.c, dir, args)
				gr := run(b.goBin, dir, args)
				o := evaluateLarge(c, cr, gr, b.rerunIn(dir))
				where := ""
				if o.verdict == verdictFail {
					artifactOnce.Do(func() { artifacts = t.ArtifactDir() })
					where = keepCase(dir, filepath.Join(artifacts, "large-"+strconv.Itoa(i)), c, cr, gr)
					o.failures = append([]string{lc.describe()}, o.failures...)
				}
				sum.add(c, o, gr, where)
				sum.mu.Lock()
				gens[lc.kind.String()]++
				outBytes += int64(len(gr.out))
				sum.mu.Unlock()
				os.RemoveAll(dir)
			}
		})
	}
	for _, i := range indices {
		work <- i
	}
	close(work)
	wg.Wait()

	report := sum.String() + fmt.Sprintf("  generators: %v; %d bytes of Go output\n", gens, outBytes)
	t.Log("\n" + report)
	if p := os.Getenv("DIFFTEST_LARGE_SUMMARY"); p != "" {
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
