package tract

// Item is an item code: the index of an item in the (recoded) item base.
// Upstream: ITEM (int).
type Item int32

// Supp is a support value or a transaction weight. Upstream: SUPP (32-bit
// int); widened to 64 bits so that realistic weight sums cannot overflow.
// All weights are non-negative, so every support is bounded by the total
// transaction weight, which is checked for overflow when it is computed.
type Supp int64

// ScanFunc performs one complete pass over a transaction database.
//
// It calls yield once for every transaction, in a deterministic order that
// is the same on every call. items holds the transaction's item identifiers
// (possibly with duplicates, which do not affect supports) and weight its
// multiplicity. The items slice is only valid during the yield call; yield
// must not retain it. If yield returns false, the scan stops and returns
// nil. Otherwise ScanFunc returns the first error that prevented the pass
// from completing, or nil.
type ScanFunc[T comparable] func(yield func(items []T, weight Supp) bool) error

// ByteScanFunc is a ScanFunc over byte-string identifiers (see
// CountBytes): the items slice and the byte slices it holds are only
// valid during the yield call.
type ByteScanFunc func(yield func(items [][]byte, weight Supp) bool) error
