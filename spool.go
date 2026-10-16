package fpgrowth

import (
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
// The caller must call cleanup when the source is no longer used; it
// removes the temporary file and may be called more than once. On error
// nothing is left behind and cleanup is nil.
func SpoolTable(r io.Reader, dir string, f TableFormat) (src Source[string], cleanup func() error, err error) {
	path, cleanup, err := spool(r, dir)
	if err != nil {
		return nil, nil, err
	}
	return TableFile(path, f), cleanup, nil
}

// SpoolCSV is like SpoolTable for CSV input: it copies r to a temporary
// file in dir and returns a CSVFile source over it, together with the
// function that removes the file.
func SpoolCSV(r io.Reader, dir string, f CSVFormat) (src Source[string], cleanup func() error, err error) {
	path, cleanup, err := spool(r, dir)
	if err != nil {
		return nil, nil, err
	}
	return CSVFile(path, f), cleanup, nil
}

// spool copies r to a new temporary file and returns its path and a
// function that removes it.
func spool(r io.Reader, dir string) (path string, cleanup func() error, err error) {
	if r == nil {
		return "", nil, errors.New("fpgrowth: spool: nil reader")
	}
	file, err := os.CreateTemp(dir, "fpgrowth-spool-*")
	if err != nil {
		return "", nil, fmt.Errorf("fpgrowth: spool: %w", err)
	}
	path = file.Name()
	_, err = io.Copy(file, r)
	if cerr := file.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path) // the copy error is the one to report
		return "", nil, fmt.Errorf("fpgrowth: spool: %w", err)
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
