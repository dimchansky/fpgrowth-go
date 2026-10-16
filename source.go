package fpgrowth

import "iter"

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
