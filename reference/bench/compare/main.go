// Command compare runs the upstream C fpgrowth and the Go port on the same
// datasets and options and records wall time, user and system CPU time,
// maximum resident set size and the number of reported patterns of every
// run. It is driven by reference/bench/compare.sh; see
// reference/bench/README.md.
//
// Every run is wrapped in /usr/bin/time (-l on macOS, -v on Linux) for the
// resource usage; the wall time is measured by this program with the
// monotonic clock (nanosecond resolution, including the negligible
// /usr/bin/time fork, equally for both programs). Runs of the two programs
// are interleaved (C, Go, C, Go, ...) after the warmup runs, so that slow
// drift of the machine's load affects both alike.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// config is one line of the configuration file: an identifier, the input
// file (relative to the data directory) and the fpgrowth options.
type config struct {
	id    string
	input string
	args  []string
}

// sample is the measurement of one run.
type sample struct {
	wall, user, sys float64 // seconds
	rss             int64   // maximum resident set size, bytes
	footprint       int64   // peak memory footprint (macOS only), bytes
	instr           int64   // instructions retired (macOS only)
	mine            float64 // the program's own mining phase time (stderr), seconds
	count           int64   // reported patterns (stderr)
	lines           int64   // lines in the output file (file mode)
}

// mode is an output mode.
type mode struct {
	name string
	out  func(dir string) string // output argument; "" for none
}

var modes = map[string]mode{
	"count":   {"count", func(string) string { return "" }},                           // no output file: count only
	"devnull": {"devnull", func(string) string { return "/dev/null" }},                // format, discard
	"file":    {"file", func(d string) string { return filepath.Join(d, "out.txt") }}, // format, write a file
}

func main() {
	cbin := flag.String("c", "", "C fpgrowth binary")
	gobin := flag.String("go", "", "Go fpgrowth binary")
	data := flag.String("data", "", "data directory (inputs)")
	cfgFile := flag.String("configs", "", "configuration file")
	only := flag.String("only", "", "regexp: run only matching configuration ids")
	modeList := flag.String("modes", "count,devnull,file", "output modes")
	warmup := flag.Int("warmup", 1, "warmup runs per program, configuration and mode")
	reps := flag.Int("n", 5, "measured runs per program, configuration and mode")
	raw := flag.String("raw", "", "file for the raw samples (TSV)")
	work := flag.String("work", "", "scratch directory for output files")
	flag.Parse()
	if *cbin == "" || *gobin == "" || *data == "" || *cfgFile == "" || *work == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*cbin, *gobin, *data, *cfgFile, *only, *modeList, *warmup, *reps, *raw, *work); err != nil {
		fmt.Fprintln(os.Stderr, "compare:", err)
		os.Exit(1)
	}
}

func run(cbin, gobin, data, cfgFile, only, modeList string, warmup, reps int, rawPath, work string) error {
	cfgs, err := readConfigs(cfgFile)
	if err != nil {
		return err
	}
	var filter *regexp.Regexp
	if only != "" {
		if filter, err = regexp.Compile(only); err != nil {
			return err
		}
	}
	var ms []mode
	for _, name := range strings.Split(modeList, ",") {
		m, ok := modes[name]
		if !ok {
			return fmt.Errorf("unknown mode %q", name)
		}
		ms = append(ms, m)
	}
	var rawW io.Writer = io.Discard
	if rawPath != "" {
		f, err := os.Create(rawPath)
		if err != nil {
			return err
		}
		defer f.Close()
		rawW = f
	}
	fmt.Fprintln(rawW, "config\tmode\tprog\trep\twall_s\tuser_s\tsys_s\tmaxrss_b\tfootprint_b\tinstr\tmine_s\tcount\tlines")

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "| config | mode | patterns | C wall ms (min-max) | Go wall ms (min-max) | Go/C wall | C user+sys ms | Go user+sys ms | C maxRSS MB | Go maxRSS MB | C mine s | Go mine s | Go/C instr |")
	fmt.Fprintln(tw, "| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	failed := false
	for _, c := range cfgs {
		if filter != nil && !filter.MatchString(c.id) {
			continue
		}
		for _, m := range ms {
			res := map[string][]sample{}
			progs := []struct{ name, bin string }{{"C", cbin}, {"Go", gobin}}
			for i := range warmup + reps {
				for _, p := range progs {
					s, err := runOnce(p.bin, data, work, c, m)
					if err != nil {
						return fmt.Errorf("%s %s %s: %w", c.id, m.name, p.name, err)
					}
					if i < warmup {
						continue
					}
					res[p.name] = append(res[p.name], s)
					fmt.Fprintf(rawW, "%s\t%s\t%s\t%d\t%.6f\t%.3f\t%.3f\t%d\t%d\t%d\t%.2f\t%d\t%d\n",
						c.id, m.name, p.name, i-warmup, s.wall, s.user, s.sys, s.rss, s.footprint, s.instr, s.mine, s.count, s.lines)
				}
			}
			cs, gs := res["C"], res["Go"]
			ok := consistent(cs, gs, m.name == "file")
			if !ok {
				failed = true
			}
			mark := ""
			if !ok {
				mark = " MISMATCH"
			}
			fmt.Fprintf(tw, "| %s | %s | %d%s | %s | %s | %.2f | %.0f | %.0f | %.1f | %.1f | %.2f | %.2f | %s |\n",
				c.id, m.name, cs[0].count, mark,
				spread(cs, func(s sample) float64 { return s.wall * 1e3 }),
				spread(gs, func(s sample) float64 { return s.wall * 1e3 }),
				median(gs, wallOf)/median(cs, wallOf),
				median(cs, cpuOf)*1e3, median(gs, cpuOf)*1e3,
				median(cs, rssOf)/1e6, median(gs, rssOf)/1e6,
				median(cs, mineOf), median(gs, mineOf),
				instrRatio(cs, gs))
			tw.Flush()
		}
	}
	tw.Flush()
	if failed {
		return errors.New("pattern counts differ (see MISMATCH rows)")
	}
	return nil
}

func wallOf(s sample) float64 { return s.wall }
func cpuOf(s sample) float64  { return s.user + s.sys }
func rssOf(s sample) float64  { return float64(s.rss) }
func mineOf(s sample) float64 { return s.mine }

func instrRatio(cs, gs []sample) string {
	c, g := median(cs, func(s sample) float64 { return float64(s.instr) }), median(gs, func(s sample) float64 { return float64(s.instr) })
	if c == 0 || g == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", g/c)
}

// consistent reports whether all runs reported the same pattern count
// (and, in file mode, wrote that many lines).
func consistent(cs, gs []sample, file bool) bool {
	want := cs[0].count
	for _, s := range slices.Concat(cs, gs) {
		if s.count != want || (file && s.lines != want) {
			return false
		}
	}
	return true
}

func median(ss []sample, f func(sample) float64) float64 {
	v := make([]float64, len(ss))
	for i, s := range ss {
		v[i] = f(s)
	}
	slices.Sort(v)
	n := len(v)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

func spread(ss []sample, f func(sample) float64) string {
	v := make([]float64, len(ss))
	for i, s := range ss {
		v[i] = f(s)
	}
	return fmt.Sprintf("%.1f (%.1f-%.1f)", median(ss, f), slices.Min(v), slices.Max(v))
}

func readConfigs(path string) ([]config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cfgs []config
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("%s: bad line %q", path, line)
		}
		cfgs = append(cfgs, config{id: fields[0], input: fields[1], args: fields[2:]})
	}
	return cfgs, sc.Err()
}

var (
	countRE = regexp.MustCompile(`\[(\d+) (?:set|rule)\(s\)\]`)
	mineRE  = regexp.MustCompile(`(?m)^(?:finding frequent item set\(s\)|filtering[^\n]*|writing [^\n]*?) \.\.\. (?:\[[^\]]*\] )?done \[([0-9.]+)s\]`)
)

// runOnce runs bin once in the data directory and measures it.
func runOnce(bin, data, work string, c config, m mode) (sample, error) {
	timeOut := filepath.Join(work, "time.txt")
	args := []string{"-o", timeOut}
	if runtime.GOOS == "linux" {
		args = append(args, "-v")
	} else {
		args = append(args, "-l")
	}
	args = append(args, bin)
	args = append(args, c.args...)
	args = append(args, c.input)
	out := m.out(work)
	if out != "" {
		args = append(args, out)
	}
	cmd := exec.Command("/usr/bin/time", args...)
	cmd.Dir = data
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	wall := time.Since(start).Seconds()
	if err != nil {
		return sample{}, fmt.Errorf("%v: %s", err, stderr.String())
	}
	s := sample{wall: wall}
	tb, err := os.ReadFile(timeOut)
	if err != nil {
		return sample{}, err
	}
	parseTime(string(tb), &s)
	log := stderr.String()
	if mm := countRE.FindAllStringSubmatch(log, -1); mm != nil {
		s.count, _ = strconv.ParseInt(mm[len(mm)-1][1], 10, 64)
	}
	for _, mm := range mineRE.FindAllStringSubmatch(log, -1) {
		v, _ := strconv.ParseFloat(mm[1], 64)
		s.mine += v
	}
	if m.name == "file" {
		n, err := countLines(out)
		if err != nil {
			return sample{}, err
		}
		s.lines = n
		os.Remove(out)
	}
	return s, nil
}

// parseTime extracts the resource usage from /usr/bin/time's output
// (BSD -l or GNU -v format).
func parseTime(t string, s *sample) {
	for _, line := range strings.Split(t, "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 6 && f[1] == "real" && f[3] == "user" && f[5] == "sys": // BSD
			s.user, _ = strconv.ParseFloat(f[2], 64)
			s.sys, _ = strconv.ParseFloat(f[4], 64)
		case strings.HasSuffix(line, "maximum resident set size"): // BSD, bytes
			s.rss, _ = strconv.ParseInt(f[0], 10, 64)
		case strings.HasSuffix(line, "peak memory footprint"):
			s.footprint, _ = strconv.ParseInt(f[0], 10, 64)
		case strings.HasSuffix(line, "instructions retired"):
			s.instr, _ = strconv.ParseInt(f[0], 10, 64)
		case strings.Contains(line, "User time (seconds):"): // GNU
			s.user, _ = strconv.ParseFloat(f[len(f)-1], 64)
		case strings.Contains(line, "System time (seconds):"):
			s.sys, _ = strconv.ParseFloat(f[len(f)-1], 64)
		case strings.Contains(line, "Maximum resident set size (kbytes):"):
			kb, _ := strconv.ParseInt(f[len(f)-1], 10, 64)
			s.rss = kb * 1024
		}
	}
}

func countLines(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var n int64
	buf := make([]byte, 1<<16)
	for {
		k, err := f.Read(buf)
		n += int64(bytes.Count(buf[:k], []byte{'\n'}))
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
	}
}
