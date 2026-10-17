package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// identicalItems writes n transactions that all consist of the same k
// items. Every item is a perfect extension of the empty set, so the
// program reports the whole hypercube of 2^k sets from a single report
// call, without returning to a recursion entry of the miner.
func identicalItems(t *testing.T, n, k int) string {
	t.Helper()
	items := make([]string, k)
	for i := range items {
		items[i] = "i" + string(rune('a'+i/26)) + string(rune('a'+i%26))
	}
	line := strings.Join(items, " ") + "\n"
	path := filepath.Join(t.TempDir(), "identical.tab")
	if err := os.WriteFile(path, []byte(strings.Repeat(line, n)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRun_cancelDuringReport checks that a context canceled while the
// reporter expands a large perfect-extension hypercube stops the run
// promptly with status 130 and "interrupted".
func TestRun_cancelDuringReport(t *testing.T) {
	t.Parallel()
	// 2^27 sets: about 17 s of output without the reporter's abort check
	in := identicalItems(t, 3, 27)
	ctx, cancel := context.WithCancel(context.Background()) // what SIGINT does
	defer cancel()
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, stderr, code := invocation{args: []string{"-s-1", in, "-"}, stdout: io.Discard, ctx: ctx}.run(t)
	elapsed := time.Since(start)
	if code != exitInterrupt || !strings.Contains(stderr, "fpgrowth: interrupted") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if elapsed > 2*time.Second {
		t.Errorf("run took %v with a cancel after 100ms", elapsed)
	}
	t.Logf("stopped after %v (cancel after 100ms)", elapsed)
}

// TestMain_interrupt runs the real program and sends SIGINT while it
// writes the 2^27 sets of 27 identical items: it must exit promptly
// with status 130 and "<prog>: interrupted".
func TestMain_interrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGINT cannot be sent to a process on Windows")
	}
	if testing.Short() {
		t.Skip("builds the program")
	}
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fpgrowth")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	in := identicalItems(t, 3, 27)
	cmd := exec.Command(bin, "-s-1", in, os.DevNull)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Wait until mining writes the output, then give it a moment.
	var log bytes.Buffer
	rd := bufio.NewReader(stderr)
	for !strings.Contains(log.String(), "writing ") {
		b, err := rd.ReadByte()
		if err != nil {
			_ = cmd.Process.Kill()
			t.Fatalf("program ended before mining: %v\n%s", err, log.String())
		}
		log.WriteByte(b)
	}
	time.Sleep(200 * time.Millisecond)
	sent := time.Now()
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	rest, _ := io.ReadAll(rd)
	log.Write(rest)
	err = cmd.Wait()
	latency := time.Since(sent)
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != exitInterrupt {
		t.Fatalf("exit: %v\n%s", err, log.String())
	}
	if !strings.HasSuffix(log.String(), bin+": interrupted\n") {
		t.Errorf("stderr does not end with %q:\n%s", bin+": interrupted", log.String())
	}
	if latency > 2*time.Second {
		t.Errorf("exit %v after SIGINT", latency)
	}
	t.Logf("exited %v after SIGINT", latency)
}

// runHelperEnv makes the test binary act as the program in
// TestMain_closedStdout: its value is the input file of the run.
const runHelperEnv = "FPGROWTH_RUN_HELPER"

// TestRun_helper is the child process of TestMain_closedStdout: it runs
// the program on the process's standard streams, like main, after
// noting on stderr whether standard output is the null device.
func TestRun_helper(t *testing.T) {
	in := os.Getenv(runHelperEnv)
	if in == "" {
		t.Skip("helper process")
	}
	if fi, err := os.Stdout.Stat(); err == nil {
		if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(fi, null) {
			fmt.Fprintln(os.Stderr, "stdout is "+os.DevNull)
		}
	}
	os.Exit(run(context.Background(), env{
		args:   []string{"fpgrowth", "-Z", in, "-"},
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}))
}

// TestMain_closedStdout documents docs/differences.md C10 (audit
// DIF-5): with standard output closed by the caller (">&-"), upstream
// fails on the first write with "write error on file <stdout>" and exit
// status 4. The Go runtime opens the null device for a closed file
// descriptor 0-2 at startup, so the program's writes succeed and it
// exits 0 as with ">/dev/null". Unix only: it closes the descriptor with
// the shell.
func TestMain_closedStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("closes standard output with /bin/sh")
	}
	t.Parallel()
	in := filepath.Join(t.TempDir(), "t1.tab")
	if err := os.WriteFile(in, []byte(test1), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", `exec "$0" "$@" >&-`, os.Args[0], "-test.run=^TestRun_helper$")
	cmd.Env = append(os.Environ(), runHelperEnv+"="+in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		t.Fatalf("exit: %v (upstream: exit 4); stderr:\n%s", err, stderr.String())
	}
	log := stderr.String()
	if !strings.HasPrefix(log, "stdout is "+os.DevNull+"\n") {
		t.Errorf("closed stdout was not reopened on %s; stderr:\n%s", os.DevNull, log)
	}
	if strings.Contains(log, "write error") ||
		!strings.Contains(log, "writing <stdout> ... [21 set(s)] done") {
		t.Errorf("stderr:\n%s", log)
	}
}

// helperEnv makes the test binary act as the process of
// TestInterruptContext_second instead of running tests.
const helperEnv = "FPGROWTH_INTERRUPT_HELPER"

// TestInterruptContext_helper is the child process of
// TestInterruptContext_second: it sets up interruptContext, reports the
// first SIGINT and then keeps running as if stopping took long.
func TestInterruptContext_helper(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("helper process")
	}
	ctx, stop := interruptContext()
	defer stop()
	fmt.Println("ready")
	<-ctx.Done()
	fmt.Println("canceled")
	time.Sleep(30 * time.Second)
	os.Exit(0) // not reached if the second SIGINT kills the process
}

// TestInterruptContext_second checks that the first SIGINT cancels the
// context and a second one kills the process (it is not swallowed).
func TestInterruptContext_second(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGINT cannot be sent to a process on Windows")
	}
	t.Parallel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInterruptContext_helper$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	rd := bufio.NewReader(stdout)
	expect := func(want string) {
		t.Helper()
		line, err := rd.ReadString('\n')
		if err != nil || line != want+"\n" {
			t.Fatalf("got %q, %v; want %q", line, err, want)
		}
	}
	expect("ready")
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	expect("canceled")
	go func() { _, _ = io.Copy(io.Discard, rd) }()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	// The default behavior is restored asynchronously right after the
	// cancellation; repeat the second SIGINT until it takes effect.
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case err := <-waited:
			var ws syscall.WaitStatus
			if ee, ok := err.(*exec.ExitError); ok {
				ws, _ = ee.Sys().(syscall.WaitStatus)
			}
			if !ws.Signaled() || ws.Signal() != syscall.SIGINT {
				t.Fatalf("process ended with %v, want death by SIGINT", err)
			}
			return
		case <-tick.C:
			_ = cmd.Process.Signal(os.Interrupt)
		case <-deadline:
			t.Fatal("a second SIGINT did not kill the process")
		}
	}
}
