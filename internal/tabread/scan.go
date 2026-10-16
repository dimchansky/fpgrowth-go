package tabread

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// ScanFile returns a tract.ScanFunc over the transactions of the file at
// path. Every call of the ScanFunc opens the file anew, reads it with a
// Reader (see Reader.Next for the semantics) and closes it before
// returning, also when yield stops the scan early or an error occurs.
// Error messages name the file by path.
//
// An open failure wraps ErrOpen, a read failure ErrRead (both together
// with the underlying error); malformed input yields a *SyntaxError.
func ScanFile(path string, cfg Config) tract.ScanFunc[string] {
	open := func() (io.ReadCloser, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err // not a typed nil *os.File in the interface
		}
		return f, nil
	}
	return Scan(open, path, cfg)
}

// Scan is like ScanFile for an arbitrary replayable input: every pass
// calls open, reads the returned stream to the end (or until yield stops
// the scan or an error occurs) and closes it. name identifies the input
// in error messages, so a caller that spools standard input to a
// temporary file can still report "<stdin>".
//
// The returned function may be called concurrently; every call uses its
// own reader. Item names are Go strings holding the raw bytes of the
// fields; within a pass equal names share one string.
func Scan(open func() (io.ReadCloser, error), name string, cfg Config) tract.ScanFunc[string] {
	return func(yield func(items []string, weight tract.Supp) bool) (err error) {
		rc, err := open()
		if err != nil {
			return fmt.Errorf("%w %s: %w", ErrOpen, name, err)
		}
		stopped := false
		defer func() {
			cerr := rc.Close()
			if err == nil && !stopped && cerr != nil {
				err = readError(name, cerr)
			}
		}()

		rd := NewReader(rc, name, cfg)
		names := []string{}
		intern := map[string]string{}
		for {
			items, weight, nerr := rd.Next()
			if errors.Is(nerr, io.EOF) {
				return nil
			}
			if nerr != nil {
				return nerr
			}
			names = names[:0]
			for _, item := range items {
				s, ok := intern[string(item)]
				if !ok {
					s = string(item)
					intern[s] = s
				}
				names = append(names, s)
			}
			if !yield(names, weight) {
				stopped = true
				return nil
			}
		}
	}
}
