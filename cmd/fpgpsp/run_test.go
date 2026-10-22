package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
)

// invocation runs the program in-process.
type invocation struct {
	args   []string
	ctx    context.Context
	now    func() time.Time
	numCPU func() int
}

func (in invocation) run(t *testing.T) (stdout, stderr string, code int) {
	t.Helper()
	ctx := in.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var out, errb bytes.Buffer
	code = run(ctx, env{
		args:    append([]string{"fpgpsp"}, in.args...),
		stdin:   strings.NewReader(""),
		stdout:  &out,
		stderr:  &errb,
		tempDir: t.TempDir(),
		now:     in.now,
		numCPU:  in.numCPU,
	})
	return out.String(), errb.String(), code
}

// dataFile writes a data set into a temporary file.
func dataFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "in.tab")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const basket = "a b c\na d e\nb c d\na b c d\nb c\na b d\nd e\na b c d\nc d e\na b c\n"

// TestRun_defaultSeedAndThreads checks the log line of D6: with the
// default seed (time) or the default number of threads (all CPUs) the
// values used are printed, so that the run can be repeated, and the
// repetition with them gives the same spectrum.
func TestRun_defaultSeedAndThreads(t *testing.T) {
	in := dataFile(t, basket)
	now := func() time.Time { return time.Unix(1729593600, 0) }
	cpus := func() int { return 3 }
	out, log, code := invocation{args: []string{"-gr", "-s-2", "-c10", in, "-"}, now: now, numCPU: cpus}.run(t)
	if code != 0 || !strings.Contains(log, "\nusing -S1729593600 -Z3 (3 chain(s))\ngenerating pattern spectrum ... ") {
		t.Fatalf("code %d, log %q", code, log)
	}
	again, log2, _ := invocation{args: []string{"-gr", "-s-2", "-c10", "-S1729593600", "-Z3", in, "-"}}.run(t)
	if again != out || strings.Contains(log2, "using") {
		t.Errorf("repetition differs or logs the seed:\n%s\n%s\n%s", out, again, log2)
	}
	// -S0 also means "time"; -Z given, so the chain count is from -Z
	_, log, _ = invocation{args: []string{"-gp", "-s-2", "-c10", "-S0", "-Z2", in, "-"}, now: now}.run(t)
	if !strings.Contains(log, "using -S1729593600 -Z2 (2 chain(s))\n") {
		t.Errorf("-S0: log %q", log)
	}
	// only -Z defaulted
	_, log, _ = invocation{args: []string{"-gp", "-s-2", "-c10", "-S5", in, "-"}, numCPU: cpus}.run(t)
	if !strings.Contains(log, "using -S5 -Z3 (3 chain(s))\n") {
		t.Errorf("default -Z: log %q", log)
	}
	// nothing random: no line
	for _, args := range [][]string{{"-gi", "-s-2", in, "-"}, {"-ge", "-e0", "-s-2", in, "-"}} {
		if _, log, _ := (invocation{args: args, now: now}).run(t); strings.Contains(log, "using") {
			t.Errorf("%q: log %q", args, log)
		}
	}
	_, log, _ = invocation{args: []string{"-ge", "-s-2", "-c10", in, "-"}, now: now}.run(t)
	if !strings.Contains(log, "\nusing -S1729593600\nestimating pattern spectrum ... ") {
		t.Errorf("-ge: log %q", log)
	}
}

// TestRun_estimateNegativeSeed checks deviation U7: -ge uses a negative
// seed like the surrogate methods (upstream replaces it by the time).
func TestRun_estimateNegativeSeed(t *testing.T) {
	in := dataFile(t, basket)
	a, _, _ := invocation{args: []string{"-ge", "-s-2", "-c100", "-z50", "-S-5", in, "-"}}.run(t)
	b, log, _ := invocation{args: []string{"-ge", "-s-2", "-c100", "-z50", "-S-5", in, "-"}, now: func() time.Time { return time.Unix(99, 0) }}.run(t)
	c, _, _ := invocation{args: []string{"-ge", "-s-2", "-c100", "-z50", "-S4294967291", in, "-"}}.run(t)
	if a == "" || a != b || a != c || strings.Contains(log, "using") {
		t.Errorf("-S-5 not used as the seed 4294967291:\n%s\n%s\n%s", a, b, c)
	}
}

// TestRun_progressOrder checks deviation U8: with several chains the
// progress counter is printed for every 20th data set in order.
func TestRun_progressOrder(t *testing.T) {
	in := dataFile(t, basket)
	_, log, code := invocation{args: []string{"-gp", "-s-2", "-c100", "-S1", "-Z4", in, os.DevNull}}.run(t)
	want := ""
	for _, k := range []int{20, 40, 60, 80, 100} {
		want += strings.Repeat(" ", 10-len(strconv.Itoa(k))) + strconv.Itoa(k) + strings.Repeat("\b", 10)
	}
	if code != 0 || !strings.Contains(log, "generating pattern spectrum ... "+want+"[") {
		t.Errorf("code %d, log %q", code, log)
	}
}

// TestRun_estimateFormat checks the shape of an estimated spectrum:
// "size support value" lines, sizes and supports ascending, values > 0,
// and the -D line monotone.
func TestRun_estimateFormat(t *testing.T) {
	in := dataFile(t, strings.Repeat(basket, 3))
	out, _, code := invocation{args: []string{"-ge", "-s-2", "-c1000", "-S1", "-D", in, "-"}}.run(t)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	border := lines[len(lines)-1]
	prev := [2]int{0, 0}
	for _, l := range lines[:len(lines)-1] {
		f := strings.Fields(l)
		size, _ := strconv.Atoi(f[0])
		supp, _ := strconv.Atoi(f[1])
		v, err := strconv.ParseFloat(f[2], 64)
		if len(f) != 3 || err != nil || v <= 0 || size < prev[0] || size == prev[0] && supp <= prev[1] {
			t.Fatalf("bad line %q after %v", l, prev)
		}
		prev = [2]int{size, supp}
	}
	last := int64(1 << 62)
	for _, s := range strings.Split(border, ":") {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v > last || v < 1 {
			t.Fatalf("bad border %q", border)
		}
		last = v
	}
}

// TestRun_cancel checks that a canceled context stops generation with
// status 130 and "interrupted".
func TestRun_cancel(t *testing.T) {
	in := dataFile(t, basket)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, g := range []string{"-gp", "-ge"} {
		_, log, code := invocation{args: []string{g, "-s-2", "-c100000", "-S1", "-Z2", in, "-"}, ctx: ctx}.run(t)
		if code != exitInterrupt || !strings.HasSuffix(log, "fpgpsp: interrupted\n") {
			t.Errorf("%s: code %d, log %q", g, code, log)
		}
	}
}

// TestMain_interrupt runs the real program and sends SIGINT while it
// generates surrogates: it must exit with status 130 and
// "<prog>: interrupted".
func TestMain_interrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGINT cannot be sent to a process on Windows")
	}
	if testing.Short() {
		t.Skip("builds the program")
	}
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fpgpsp")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	in := dataFile(t, strings.Repeat(basket, 20))
	cmd := exec.Command(bin, "-gp", "-s-2", "-c1000000000", "-S1", "-Z2", in, os.DevNull)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	rd := bufio.NewReader(stderr)
	for !strings.Contains(log.String(), "        20") { // generation runs
		b, err := rd.ReadByte()
		if err != nil {
			_ = cmd.Process.Kill()
			t.Fatalf("program ended before generating: %v\n%s", err, log.String())
		}
		log.WriteByte(b)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	rest, _ := io.ReadAll(rd)
	log.Write(rest)
	err = cmd.Wait()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != exitInterrupt {
		t.Fatalf("exit: %v\n%s", err, log.String())
	}
	if !strings.HasSuffix(log.String(), bin+": interrupted\n") {
		t.Errorf("stderr does not end with %q:\n%q", bin+": interrupted", log.String())
	}
}

// TestRun_outputErrors checks the messages for an output file that
// cannot be created and a failing standard output.
func TestRun_outputErrors(t *testing.T) {
	in := dataFile(t, basket)
	_, log, code := invocation{args: []string{"-gi", "-s-2", in, filepath.Join(t.TempDir(), "no", "out")}}.run(t)
	if code != exitOpen || !strings.Contains(log, "cannot open file") {
		t.Errorf("code %d, log %q", code, log)
	}
	var errb bytes.Buffer
	code = run(context.Background(), env{
		args:   []string{"fpgpsp", "-gi", "-s-2", in, "-"},
		stdin:  strings.NewReader(""),
		stdout: failWriter{},
		stderr: &errb,
	})
	if code != exitWrite || !strings.HasSuffix(errb.String(), "fpgpsp: write error on file <stdout>\n") {
		t.Errorf("code %d, log %q", code, errb.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

// TestChainError checks that the error of a failing chain is reported, not
// the cancellation it caused in the other chains (status 1, not 130).
func TestChainError(t *testing.T) {
	tooLarge := errors.New("too large")
	canceled := fmt.Errorf("chain: %w", context.Canceled)
	for _, c := range []struct {
		errs []error
		want error
	}{
		{nil, nil},
		{[]error{nil, nil}, nil},
		{[]error{canceled, tooLarge, canceled}, tooLarge},
		{[]error{nil, canceled}, canceled},
		{[]error{tooLarge, nil}, tooLarge},
	} {
		if got := chainError(c.errs); got != c.want {
			t.Errorf("chainError(%v) = %v, want %v", c.errs, got, c.want)
		}
	}
	r := &runner{o: &options{inFile: new(string)}}
	if e := r.prepError(chainError([]error{canceled, fpg.ErrTooLarge})); e.code != exitNoMem {
		t.Errorf("status %d, want %d", e.code, exitNoMem)
	}
}

// TestRun_workersSameResult checks that the output does not depend on how
// many chains run at a time (GOMAXPROCS bounds the workers).
func TestRun_workersSameResult(t *testing.T) {
	in := dataFile(t, basket)
	args := []string{"-gr", "-s-2", "-c40", "-S9", "-Z12", in, "-"}
	want, _, _ := invocation{args: args}.run(t)
	prev := runtime.GOMAXPROCS(1)
	got, _, _ := invocation{args: args}.run(t)
	runtime.GOMAXPROCS(prev)
	if got != want || want == "" {
		t.Errorf("one worker:\n%s\nmany workers:\n%s", got, want)
	}
}
