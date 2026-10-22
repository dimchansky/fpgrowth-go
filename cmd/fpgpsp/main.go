// Command fpgpsp generates or estimates a pattern spectrum for FP-growth:
// the average number of item sets per (size, support) signature in
// surrogate data sets (randomized versions of the input) or an analytical
// estimate of it, and optionally the decision border it induces. It is a
// Go port of Christian Borgelt's fpgpsp program (version 1.3), which is
// part of the FP-Growth 6.21 sources, with upstream's defects fixed
// (docs/differences.md, rows U1-U16) and otherwise the same options,
// formats, messages and exit codes; run it without arguments for the
// usage message and with -! for more information.
//
// The surrogate data sets are generated in chains, one per thread of
// upstream's program (-Z): the result depends on the seed (-S) and the
// number of chains, never on the machine or the scheduling. When -S or
// -Z is left at its default (the time, all CPUs), the values used are
// printed, so that the run can be repeated.
//
// An interrupt (SIGINT) stops the program with status 130. A second
// interrupt kills the program at once (the default signal behavior).
package main

import (
	"context"
	"os"
	"os/signal"
)

func main() {
	ctx, stop := interruptContext()
	code := run(ctx, env{
		args:   os.Args,
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	})
	stop()
	os.Exit(code)
}

// interruptContext returns a context that the first SIGINT cancels. It
// then restores the default behavior of SIGINT, so that a second one
// kills the program if stopping takes too long.
func interruptContext() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	context.AfterFunc(ctx, stop)
	return ctx, stop
}
