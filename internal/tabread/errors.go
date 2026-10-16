package tabread

import (
	"errors"
	"fmt"
)

// Error kinds. The texts are upstream's message texts (tract.c errmsgs),
// without a package prefix, because callers compose them into
// upstream-compatible messages. Positional errors are reported as
// *SyntaxError wrapping one of these; open and read failures wrap ErrOpen
// or ErrRead together with the underlying I/O error.
var (
	// ErrOpen: the input file could not be opened (upstream E_FOPEN).
	ErrOpen = errors.New("cannot open file")
	// ErrRead: reading the input failed (upstream E_FREAD).
	ErrRead = errors.New("read error on file")

	// ErrItemExpected: an empty field where an item is required
	// (E_ITEMEXP), e.g. "a,,b" or an empty line in a selection file.
	ErrItemExpected = errors.New("item expected")
	// ErrWeightExpected: the weight field of a weighted record is empty
	// (E_WGTEXP), e.g. an empty line or a trailing field separator.
	ErrWeightExpected = errors.New("transaction weight expected")
	// ErrInvalidWeight: the weight field is not an integer in upstream's
	// syntax or does not fit in 64 bits (E_TAWGT).
	ErrInvalidWeight = errors.New("invalid transaction weight")
	// ErrNegativeWeight: the weight is negative. It wraps
	// ErrInvalidWeight, so errors.Is(err, ErrInvalidWeight) also holds.
	ErrNegativeWeight = fmt.Errorf("%w: negative", ErrInvalidWeight)

	// ErrDuplicateItem: an item is listed twice in an appearance file
	// (E_DUPITEM).
	ErrDuplicateItem = errors.New("duplicate item")
	// ErrTooManyFields: a record of an appearance file has more fields
	// than allowed (E_FLDCNT); also reported for an empty appearance file,
	// whose missing first record upstream counts as malformed.
	ErrTooManyFields = errors.New("too many fields/columns")
	// ErrAppearanceExpected: an appearance file record lists an item
	// without an indicator (E_APPEXP).
	ErrAppearanceExpected = errors.New("appearance indicator expected")
	// ErrUnknownAppearance: an appearance indicator is not recognized
	// (E_UNKAPP).
	ErrUnknownAppearance = errors.New("unknown appearance indicator")
)

// maxQuoted is the maximum number of bytes of a field quoted in an error
// message: upstream's field buffer holds TRD_MAXLEN = 1024 bytes, so its
// messages never quote more.
const maxQuoted = 1024

// SyntaxError reports malformed input at a position. Its message matches
// upstream's ib_errmsg: "<file>:<record>(<field>): <message>".
type SyntaxError struct {
	File   string // input name as given to the reader, e.g. "<stdin>"
	Record int    // 1-based record number; comment records are counted
	Field  int    // 1-based field number within the record
	Text   string // offending field as a C string (up to its first NUL)
	Err    error  // kind of error: one of the Err* sentinels
}

// Error returns "<file>:<record>(<field>): <message>".
func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s:%d(%d): %s", e.File, e.Record, e.Field, e.Message())
}

// Message returns the message without the position, worded like
// upstream's. At most the first 1024 bytes of the field are quoted.
func (e *SyntaxError) Message() string {
	text := e.Text
	if len(text) > maxQuoted {
		text = text[:maxQuoted]
	}
	switch {
	case errors.Is(e.Err, ErrNegativeWeight):
		return "invalid transaction weight " + text + " (must not be negative)"
	case errors.Is(e.Err, ErrInvalidWeight):
		return "invalid transaction weight " + text
	case errors.Is(e.Err, ErrDuplicateItem):
		return "duplicate item '" + text + "'"
	case errors.Is(e.Err, ErrUnknownAppearance):
		return "unknown appearance indicator '" + text + "'"
	case e.Err == nil:
		return "syntax error"
	}
	return e.Err.Error()
}

// Unwrap returns the error kind.
func (e *SyntaxError) Unwrap() error { return e.Err }

// readError reports a failed read of the named input.
//
// Deviation from upstream: the message names the file and carries the
// I/O error; upstream formats the current field instead of the file name
// (cli-io.md B8).
func readError(name string, err error) error {
	return fmt.Errorf("%w %s: %w", ErrRead, name, err)
}
