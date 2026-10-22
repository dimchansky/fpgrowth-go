package main

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/dimchansky/fpgrowth-go/internal/tabread"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// The input helpers below are those of cmd/fpgrowth (run.go): the same
// reading, spooling of standard input, cancellation and error messages.

// openInput opens the transaction file (upstream opens it before the
// "reading" message, so an open error has no message prefix) and returns
// a replayable scan over it. Standard input is spooled to a temporary
// file first, which is removed by cleanup.
func (r *runner) openInput(file, name string, cfg tabread.Config) (tract.ScanFunc[string], *cliError) {
	path := file
	var first *os.File
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return nil, errorf(exitOpen, "cannot open file "+name)
		}
		first = f
		r.logf("reading %s ... ", name)
	} else {
		r.logf("reading %s ... ", name)
		p, cerr := r.spool(name)
		if cerr != nil {
			return nil, cerr
		}
		path = p
	}
	open := func() (io.ReadCloser, error) {
		if first != nil { // the file opened above serves the first pass
			f := first
			first = nil
			return f, nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	r.cleanups = append(r.cleanups, func() {
		if first != nil {
			first.Close()
		}
	})
	return r.ctxScan(tabread.Scan(open, name, cfg)), nil
}

// spool copies standard input to a temporary file and returns its path.
func (r *runner) spool(name string) (string, *cliError) {
	f, err := os.CreateTemp(r.tempDir, "fpgpsp-stdin-*")
	if err != nil {
		// Deviation from upstream (which reads stdin into memory): the
		// spool file could not be created.
		return "", errorf(exitOpen, "cannot create temporary file for "+name)
	}
	path := f.Name()
	r.cleanups = append(r.cleanups, func() { _ = os.Remove(path) })
	err = r.copyCtx(f, r.stdin)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = &spoolWriteError{cerr}
	}
	if err != nil {
		return "", r.copyError(err, name)
	}
	return path, nil
}

// spoolWriteError marks a failure to write the spool file.
type spoolWriteError struct{ err error }

func (e *spoolWriteError) Error() string { return e.err.Error() }
func (e *spoolWriteError) Unwrap() error { return e.err }

// spoolWriter tags write errors of the destination of copyCtx.
type spoolWriter struct{ w io.Writer }

func (s spoolWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if err != nil {
		err = &spoolWriteError{err}
	}
	return n, err
}

// copyCtx copies src to dst; it returns ctx.Err() as soon as ctx is
// canceled, even while a read from src blocks (the copy then continues
// in the background until the process exits).
func (r *runner) copyCtx(dst io.Writer, src io.Reader) error {
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(spoolWriter{dst}, src)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-r.ctx.Done():
		return r.ctx.Err()
	}
}

// copyError maps an error of copyCtx.
func (r *runner) copyError(err error, name string) *cliError {
	var we *spoolWriteError
	switch {
	case errors.Is(err, context.Canceled):
		return errorf(exitInterrupt, "interrupted")
	case errors.As(err, &we):
		return errorf(exitWrite, "write error on temporary file for "+name)
	default:
		return errorf(exitRead, "read error on file "+name)
	}
}

// aborted is a non-blocking check of ctx (SIGINT).
func (r *runner) aborted() error {
	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	default:
		return nil
	}
}

// ctxScan stops a scan when ctx is canceled (checked every 1024 records).
func (r *runner) ctxScan(scan tract.ScanFunc[string]) tract.ScanFunc[string] {
	done := r.ctx.Done()
	return func(yield func([]string, tract.Supp) bool) error {
		var cerr error
		cnt := 0
		err := scan(func(items []string, w tract.Supp) bool {
			cnt++
			if cnt&1023 == 0 {
				select {
				case <-done:
					cerr = r.ctx.Err()
					return false
				default:
				}
			}
			return yield(items, w)
		})
		if cerr != nil {
			return cerr
		}
		return err
	}
}

// inputError maps an error of reading the named input to upstream's
// message and exit status.
func (r *runner) inputError(err error, name string) *cliError {
	var se *tabread.SyntaxError
	switch {
	case errors.Is(err, context.Canceled):
		return errorf(exitInterrupt, "interrupted")
	case errors.As(err, &se):
		code := exitItemExp
		switch {
		case errors.Is(se.Err, tabread.ErrWeightExpected):
			code = exitWgtExp
		case errors.Is(se.Err, tabread.ErrInvalidWeight):
			code = exitTaWgt
		case errors.Is(se.Err, tabread.ErrDuplicateItem):
			code = exitDupItem
		case errors.Is(se.Err, tabread.ErrTooManyFields):
			code = exitFldCnt
		case errors.Is(se.Err, tabread.ErrAppearanceExpected):
			code = exitAppExp
		case errors.Is(se.Err, tabread.ErrUnknownAppearance):
			code = exitUnkApp
		}
		return errorf(code, se.Error())
	case errors.Is(err, tabread.ErrOpen):
		return errorf(exitOpen, "cannot open file "+name)
	case errors.Is(err, tabread.ErrRead):
		// Deviation from upstream (cli-io.md B8): the message names the
		// file, not the current field.
		return errorf(exitRead, "read error on file "+name)
	case errors.Is(err, tract.ErrNotReplayable):
		return errorf(exitRead, "read error on file "+name+" (input changed while reading)")
	case errors.Is(err, tract.ErrWeightOverflow):
		// Deviation from upstream (cli-io.md B11): upstream's int sum
		// overflows and the program crashes.
		return errorf(exitTaWgt, name+": total transaction weight too large")
	case errors.Is(err, tract.ErrTooLarge):
		return errorf(exitNoMem, "database too large")
	}
	return errorf(exitNoMem, err.Error())
}
