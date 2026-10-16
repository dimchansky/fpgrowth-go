package fpgrowth

import (
	"errors"
	"strings"

	"github.com/dimchansky/fpgrowth-go/internal/fpg"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Errors returned by this package. They are wrapped with details; test
// for them with errors.Is.
var (
	// ErrInvalidOptions reports an invalid Options or RuleOptions value.
	// The wrapping error names the offending field.
	ErrInvalidOptions = errors.New("fpgrowth: invalid options")
	// ErrNotReplayable reports a Source whose second pass differs from the
	// first one (number of transactions, total weight, or an item that the
	// first pass did not contain).
	ErrNotReplayable = errors.New("fpgrowth: source is not replayable")
	// ErrNegativeWeight reports a transaction with a negative weight.
	ErrNegativeWeight = errors.New("fpgrowth: negative transaction weight")
	// ErrWeightOverflow reports a total transaction weight beyond
	// math.MaxInt64.
	ErrWeightOverflow = errors.New("fpgrowth: total transaction weight overflows int64")
	// ErrTooLarge reports a database or result structure that exceeds an
	// implementation limit (2^31-1 tree nodes or items).
	ErrTooLarge = errors.New("fpgrowth: database too large")
	// ErrSyntax reports malformed input of a table or CSV source.
	ErrSyntax = errors.New("fpgrowth: syntax error")
	// ErrInvalidItem reports an item that cannot be mined: a value whose
	// dynamic type is not comparable (for example a slice in an item of
	// interface type), or a value that is not equal to itself (a NaN, also
	// inside a struct, array or interface). Only item types that can hold
	// such values (interfaces, floats, and arrays and structs containing
	// them) are checked, once per distinct item when it is first read
	// rather than per occurrence; ignored items (outside Options.Items, or
	// not in RuleOptions.Appearance with DefaultAppearance AppearNone) are
	// never registered and are checked at every occurrence.
	ErrInvalidItem = errors.New("fpgrowth: invalid item")
)

// detailError is an error with its own message that matches several
// targets with errors.Is and errors.As: a sentinel of this package and,
// optionally, the underlying cause.
type detailError struct {
	msg  string
	errs []error
}

func (e *detailError) Error() string   { return e.msg }
func (e *detailError) Unwrap() []error { return e.errs }

// newDetailError returns an error with message msg that wraps errs.
func newDetailError(msg string, errs ...error) error {
	return &detailError{msg: msg, errs: errs}
}

// internalDetail strips the package prefixes and the trailing internal
// sentinel text from an internal error message, leaving the description
// (for example "record 7: item x not seen in pass 1").
func internalDetail(err error, sentinel error) string {
	msg := err.Error()
	for trimmed := true; trimmed; {
		trimmed = false
		for _, p := range [...]string{"fpg: ", "tract: ", "report: ", "istree: "} {
			if s, ok := strings.CutPrefix(msg, p); ok {
				msg, trimmed = s, true
			}
		}
	}
	msg = strings.TrimSuffix(msg, ": "+sentinel.Error())
	if msg == sentinel.Error() {
		return ""
	}
	return msg
}

// mapInternal translates errors of the internal packages to this
// package's sentinels. Other errors are returned unchanged.
func mapInternal(err error) error {
	var sentinel, public error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, tract.ErrNotReplayable):
		sentinel, public = tract.ErrNotReplayable, ErrNotReplayable
	case errors.Is(err, tract.ErrNegativeWeight):
		sentinel, public = tract.ErrNegativeWeight, ErrNegativeWeight
	case errors.Is(err, tract.ErrWeightOverflow):
		sentinel, public = tract.ErrWeightOverflow, ErrWeightOverflow
	case errors.Is(err, tract.ErrTooLarge):
		sentinel, public = tract.ErrTooLarge, ErrTooLarge
	case errors.Is(err, fpg.ErrConfig):
		// excluded by validation; reported as an invalid option
		sentinel, public = fpg.ErrConfig, ErrInvalidOptions
	default:
		return err
	}
	if err == sentinel {
		return public
	}
	if d := internalDetail(err, sentinel); d != "" {
		return newDetailError(public.Error()+": "+d, public)
	}
	return public
}
