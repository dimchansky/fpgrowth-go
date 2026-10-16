// Package fpgrowth finds frequent item sets and association rules with the
// FP-growth algorithm.
//
// It is a pure Go port and further development of Christian Borgelt's C
// implementation FP-Growth 6.21 (https://borgelt.net/fpgrowth.html,
// MIT License, (c) 2004-2022 Christian Borgelt). The upstream-compatible
// command line program is github.com/dimchansky/fpgrowth-go/cmd/fpgrowth.
//
// # Data
//
// Items can be of any comparable type; they are keys of a map. Items must
// be strictly comparable at run time: an item of interface type (or a
// struct or array containing one) whose dynamic value is not comparable,
// such as a slice, and an item that is not equal to itself, such as a
// floating-point NaN (also inside a struct, array or interface), are
// rejected with [ErrInvalidItem] instead of panicking or never matching.
// The checks cost nothing for item types without floats and interfaces.
//
// A transaction database is a [Source]: something that can be read
// several times, each pass yielding the same transactions in the same
// order. Mining reads it twice and never keeps the transactions
// themselves; it keeps the item dictionary, a prefix tree of the
// (filtered) transactions and the FP-trees, so memory grows with the
// distinct structure of the data rather than with its size. [FromSlice],
// [FromItems] and [Generate] provide in-memory and generated sources,
// [TableFile] and [TableReader] stream upstream's text format from a file
// or any reopenable input (compressed files, embedded data, object
// storage), [CSVFile] and [CSVReader] stream CSV, and [SpoolTable] and
// [SpoolCSV] turn a one-shot [io.Reader] into a file-backed source.
//
// # Mining
//
// [MineItemsets] calls a function for every item set (frequent, closed,
// maximal or generators, see [Target]) and returns a [Summary];
// [Itemsets] is the same as an iterator and [CollectItemsets] returns a
// slice. [MineRules], [Rules] and [CollectRules] do the same for
// association rules. Mining runs synchronously on the calling goroutine;
// results are delivered one at a time; returning an error from the
// callback, breaking out of the loop, or canceling the context stops it.
// Cancellation is checked frequently but not before every result, so up
// to about a thousand results may still be delivered after the context is
// canceled; a canceled run always returns ctx.Err(), also if mining
// finished after the last check.
//
// Options.OnStart and RuleOptions.OnStart receive the [Stats] of the
// database (total weight, frequent items, resolved thresholds) before the
// first result, so that, for example, relative supports can be streamed.
//
// The slices in reported values are reused by the library and only valid
// during the callback or iteration step; Clone them to keep them. In
// steady state, reporting a result does not allocate.
//
// Results come in upstream's deterministic order, which depends on the
// data, the options and the [Algorithm]; the sets and supports themselves
// do not depend on the algorithm or the [Tuning].
//
// # Thresholds
//
// Supports are weighted counts (the sum of the weights of the transactions
// containing a set). A [Support] threshold is either absolute ([Count]) or
// relative to the total weight ([Fraction]), converted with upstream's
// arithmetic. Counts are exact over the whole int64 range. Confidences and
// evaluation thresholds are fractions or values of the measure, not
// upstream's percentages. An [Evaluation] computes a measure for every
// result and filters by it only with Evaluation.Filter.
//
// # Concurrency
//
// All functions are safe for concurrent use; each call has its own state.
// A Source shared by concurrent calls must support concurrent passes; the
// sources of this package do.
package fpgrowth
