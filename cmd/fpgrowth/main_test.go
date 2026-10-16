package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
