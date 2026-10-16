// Command fpgrowth finds frequent item sets and association rules with the
// FP-growth algorithm. It is a Go port of Christian Borgelt's fpgrowth
// program (version 6.21) and accepts the same options, reads and writes
// the same formats, and prints the same messages and exit codes; run it
// without arguments for the usage message and with -! for more
// information. Intentional differences from upstream are listed in
// docs/differences.md.
//
// An interrupt (SIGINT) stops reading and mining, removes the temporary
// spool file of standard input and exits with status 130. A second
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
