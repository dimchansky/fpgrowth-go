package fpgrowth

import (
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"time"
)

// Transaction is one record of a transaction database.
type Transaction[T comparable] struct {
	// Items are the items of the transaction. Repeated items count once;
	// the order is irrelevant.
	Items []T
	// Weight is the multiplicity of the transaction. It must not be
	// negative. A transaction with weight 0 still counts as a transaction
	// (Summary.Transactions) but contributes nothing to any support.
	// Note that the zero value of Transaction has weight 0; FromItems
	// gives every transaction weight 1.
	Weight int64
}

// Source provides a transaction database that can be read several times.
// Mining reads it twice: once to count the items and once to build the
// prefix tree.
//
// Every call of Transactions starts an independent pass that must yield
// the same transactions in the same order. A pass yields (zero value, err)
// and stops at the first error. The Items slice of a yielded transaction
// is only valid until the next iteration step; consumers must copy it to
// retain it and must not modify it.
//
// A Source used by concurrent mining calls must support concurrent passes.
// The sources of this package do.
type Source[T comparable] interface {
	Transactions() iter.Seq2[Transaction[T], error]
}

// SourceFunc adapts a function that starts a pass to a Source.
type SourceFunc[T comparable] func() iter.Seq2[Transaction[T], error]

// Transactions calls f.
func (f SourceFunc[T]) Transactions() iter.Seq2[Transaction[T], error] { return f() }

// FromSlice returns a Source over transactions held in memory. The slice
// and the item slices are read on every pass and must not be modified
// while mining.
func FromSlice[T comparable](txs []Transaction[T]) Source[T] {
	return SourceFunc[T](func() iter.Seq2[Transaction[T], error] {
		return func(yield func(Transaction[T], error) bool) {
			for _, tx := range txs {
				if !yield(tx, nil) {
					return
				}
			}
		}
	})
}

// FromItems returns a Source over item lists held in memory, each a
// transaction with weight 1. The slices must not be modified while
// mining.
func FromItems[T comparable](txs [][]T) Source[T] {
	return SourceFunc[T](func() iter.Seq2[Transaction[T], error] {
		return func(yield func(Transaction[T], error) bool) {
			for _, items := range txs {
				if !yield(Transaction[T]{Items: items, Weight: 1}, nil) {
					return
				}
			}
		}
	})
}

// Generate returns a Source of n transactions produced on demand: on every
// pass gen is called for i = 0, ..., n-1 and must return the same
// transaction for the same i. buf is an empty slice whose capacity gen may
// reuse for the items (it is the Items slice gen returned for the previous
// i of the same pass), so a generator can avoid allocations. Concurrent
// passes call gen concurrently with different buffers.
//
// A negative n is treated as 0.
func Generate[T comparable](n int, gen func(i int, buf []T) Transaction[T]) Source[T] {
	return SourceFunc[T](func() iter.Seq2[Transaction[T], error] {
		return func(yield func(Transaction[T], error) bool) {
			var buf []T
			for i := range max(n, 0) {
				tx := gen(i, buf[:0])
				if !yield(tx, nil) {
					return
				}
				buf = tx.Items
			}
		}
	})
}

// runSource is implemented by sources that keep state per mining run:
// mining calls forRun once and reads the returned source for all passes
// of the run. The file-backed sources use it to compare the input's size
// and modification time between passes.
type runSource[T comparable] interface {
	Source[T]
	forRun() Source[T]
}

// opener opens the input of a reader-backed source once per pass.
type opener struct {
	open func() (io.ReadCloser, error)
	name string
	// check is the replay check of the current mining run; nil outside
	// a run (a pass started by calling Transactions directly).
	check *replayCheck
}

// openFile returns an open function for the file at path.
func openFile(path string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err // not a typed nil *os.File in the interface
		}
		return f, nil
	}
}

// forRun returns a copy of o with a fresh replay check.
func (o opener) forRun() opener {
	o.check = &replayCheck{}
	return o
}

// openPass opens the input for one pass and runs the replay check.
func (o opener) openPass() (io.ReadCloser, error) {
	if o.open == nil {
		return nil, invalid("open", "nil function for %s", o.name)
	}
	rc, err := o.open()
	if err != nil {
		return nil, fmt.Errorf("cannot open file %s: %w", o.name, err)
	}
	if rc == nil {
		return nil, fmt.Errorf("cannot open file %s: open returned a nil reader", o.name)
	}
	if err := o.check.verify(rc, o.name); err != nil {
		_ = rc.Close() // the check's error is the one to report
		return nil, err
	}
	return rc, nil
}

// replayCheck records the size and modification time of the input at the
// first pass of a mining run and compares them at later passes. Inputs
// without a Stat method (such as decompressing readers) are not checked.
type replayCheck struct {
	isSet bool
	size  int64
	mtime time.Time
}

// statter is implemented by *os.File.
type statter interface {
	Stat() (fs.FileInfo, error)
}

func (c *replayCheck) verify(rc io.ReadCloser, name string) error {
	if c == nil {
		return nil
	}
	st, ok := rc.(statter)
	if !ok {
		return nil
	}
	fi, err := st.Stat()
	if err != nil {
		return fmt.Errorf("read error on file %s: %w", name, err)
	}
	if !c.isSet {
		c.isSet, c.size, c.mtime = true, fi.Size(), fi.ModTime()
		return nil
	}
	if fi.Size() != c.size || !fi.ModTime().Equal(c.mtime) {
		return newDetailError(fmt.Sprintf(
			"%s: %s changed between passes (size %d, modified %s; first pass: size %d, modified %s)",
			ErrNotReplayable, name, fi.Size(), fi.ModTime().Format(time.RFC3339Nano),
			c.size, c.mtime.Format(time.RFC3339Nano)), ErrNotReplayable)
	}
	return nil
}
