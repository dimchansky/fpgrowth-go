package fpgrowth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
)

// SpoolTable copies a non-replayable input such as standard input or a
// network stream to a temporary file in dir (os.TempDir() if dir is
// empty) and returns a TableFile source over it. The input is copied in
// fixed-size chunks and never held in memory as a whole.
//
// ctx is checked before every chunk; if it is canceled, SpoolTable
// removes the partial file and returns ctx.Err(). A Read that blocks is
// not interrupted (close the reader to unblock it). An invalid format
// (see TableFormat) and a nil r are errors wrapping ErrInvalidOptions,
// returned before anything is copied; other errors are "fpgrowth: spool: ..."
// wrapping the cause.
//
// The caller must call cleanup when the source is no longer used; it
// removes the temporary file and may be called more than once. On error
// nothing is left behind and cleanup is nil.
func SpoolTable(ctx context.Context, r io.Reader, dir string, f TableFormat) (src Source[string], cleanup func() error, err error) {
	if err := f.validate(); err != nil {
		return nil, nil, err
	}
	path, cleanup, err := spool(ctx, r, dir)
	if err != nil {
		return nil, nil, err
	}
	return TableFile(path, f), cleanup, nil
}

// SpoolCSV is like SpoolTable for CSV input: it copies r to a temporary
// file in dir and returns a CSVFile source over it, together with the
// function that removes the file. An invalid Comma or Comment is an error
// wrapping ErrInvalidOptions, returned before anything is copied.
func SpoolCSV(ctx context.Context, r io.Reader, dir string, f CSVFormat) (src Source[string], cleanup func() error, err error) {
	if err := f.validate(); err != nil {
		return nil, nil, err
	}
	path, cleanup, err := spool(ctx, r, dir)
	if err != nil {
		return nil, nil, err
	}
	return CSVFile(path, f), cleanup, nil
}

// spoolChunk is the size of the copy buffer of spool.
const spoolChunk = 64 << 10

// spool copies r to a new temporary file and returns its path and a
// function that removes it.
func spool(ctx context.Context, r io.Reader, dir string) (path string, cleanup func() error, err error) {
	if r == nil {
		return "", nil, invalid("r", "nil reader")
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	file, err := os.CreateTemp(dir, "fpgrowth-spool-*")
	if err != nil {
		return "", nil, fmt.Errorf("fpgrowth: spool: %w", err)
	}
	path = file.Name()
	err = copyChunks(ctx, file, r)
	if cerr := file.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("fpgrowth: spool: %w", cerr)
	}
	if err != nil {
		_ = os.Remove(path) // the copy error is the one to report
		return "", nil, err
	}
	var once sync.Once
	var rerr error
	cleanup = func() error {
		once.Do(func() {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				rerr = err
			}
		})
		return rerr
	}
	return path, cleanup, nil
}

// copyChunks copies r to w in chunks of spoolChunk bytes, checking ctx
// before every chunk. It returns ctx.Err() unchanged and other errors
// wrapped.
func copyChunks(ctx context.Context, w io.Writer, r io.Reader) error {
	buf := make([]byte, spoolChunk)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return fmt.Errorf("fpgrowth: spool: %w", werr)
			}
		}
		switch {
		case errors.Is(rerr, io.EOF):
			return nil
		case rerr != nil:
			return fmt.Errorf("fpgrowth: spool: %w", rerr)
		}
	}
}
