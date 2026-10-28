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
	scan := ScanBytes(open, name, cfg)
	return func(yield func(items []string, weight tract.Supp) bool) error {
		names := []string{}
		intern := map[string]string{}
		return scan(func(items [][]byte, weight tract.Supp) bool {
			names = names[:0]
			for _, item := range items {
				s, ok := intern[string(item)]
				if !ok {
					s = string(item)
					intern[s] = s
				}
				names = append(names, s)
			}
			return yield(names, weight)
		})
	}
}

// ScanBytes is Scan with the item names as byte slices, which are only
// valid during the yield call (see Reader.Next): for a consumer that
// looks the names up itself (tract.CountBytes), it saves Scan's string
// interning, a second lookup of every name.
func ScanBytes(open func() (io.ReadCloser, error), name string, cfg Config) tract.ByteScanFunc {
	return func(yield func(items [][]byte, weight tract.Supp) bool) (err error) {
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
		for {
			items, weight, nerr := rd.Next()
			if errors.Is(nerr, io.EOF) {
				return nil
			}
			if nerr != nil {
				return nerr
			}
			if !yield(items, weight) {
				stopped = true
				return nil
			}
		}
	}
}
