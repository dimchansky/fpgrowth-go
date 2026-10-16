package tabread

import (
	"bytes"
	"io"

	"github.com/dimchansky/fpgrowth-go/internal/cconv"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// Reader reads transactions from an input in the flexible table format:
// one record per transaction, one item per field. Port of the record loop
// of upstream's ib_read (tract.c) on top of trd_read.
//
// A Reader reuses its buffers: the item slices returned by Next are only
// valid until the next call.
type Reader struct {
	tok      *tokenizer
	name     string
	weighted bool

	arena []byte   // item bytes of the current record
	ends  []int    // end offset of each item in arena
	items [][]byte // views of arena returned by Next
	rec   int      // record number of the last record returned
	err   error    // sticky error (including io.EOF)
}

// NewReader returns a Reader for src. name identifies the input in error
// messages (upstream uses the file name, or "<stdin>").
func NewReader(src io.Reader, name string, cfg Config) *Reader {
	return &Reader{
		tok:      newTokenizer(src, cfg),
		name:     name,
		weighted: cfg.Weighted,
		arena:    []byte{},
		ends:     []int{},
		items:    [][]byte{},
	}
}

// Next reads the next record and returns its items and its weight. At the
// end of the input it returns io.EOF.
//
// The items are the record's fields in order, each cut at its first NUL
// byte (upstream uses C strings). Duplicates are kept; they do not affect
// supports. A record without fields (an empty or blank-only line) is an
// empty transaction. An empty last field (trailing field separator) is
// ignored; an empty field before another field is an error
// (ErrItemExpected). The weight is 1 unless the Config is Weighted, in
// which case the last field of the record is the weight: an optional
// '(', '[' or '{', an integer in C strtol base-0 syntax (leading white
// space, sign, 0x hex, 0 octal), the matching closing bracket if one was
// opened, and nothing else. A record that consists of the weight only is
// an empty transaction with that weight.
//
// Errors are *SyntaxError values for malformed input and errors wrapping
// ErrRead for read failures. Errors are sticky: once Next has returned an
// error, it returns the same error again.
func (r *Reader) Next() (items [][]byte, weight tract.Supp, err error) {
	if r.err != nil {
		return nil, 0, r.err
	}
	items, weight, err = r.next()
	if err != nil {
		r.err = err
		return nil, 0, err
	}
	return items, weight, nil
}

// Record returns the 1-based number of the record last returned by Next.
// Comment records are counted, so for "\n"-separated input it is the line
// number.
func (r *Reader) Record() int {
	return r.rec
}

func (r *Reader) next() ([][]byte, tract.Supp, error) {
	r.arena = r.arena[:0]
	r.ends = r.ends[:0]
	weight := tract.Supp(1)
fields:
	for first := true; ; first = false {
		d := r.tok.read()
		switch d {
		case delimErr:
			return nil, 0, readError(r.name, r.tok.err())
		case delimEOF:
			if !first {
				// The tokenizer completes a started record with delimRec.
				panic("tabread: internal error: end of input inside a record")
			}
			return nil, 0, io.EOF
		}
		field := cString(r.tok.field)
		if d == delimRec && r.weighted {
			w, err := r.parseWeight(field)
			if err != nil {
				return nil, 0, err
			}
			weight = w
			break fields
		}
		if len(field) == 0 {
			if d == delimRec {
				break fields // trailing separator or empty record
			}
			return nil, 0, r.syntaxError(ErrItemExpected, field)
		}
		r.arena = append(r.arena, field...)
		r.ends = append(r.ends, len(r.arena))
		if d == delimRec {
			break fields
		}
	}
	r.rec = r.tok.rec
	r.items = r.items[:0]
	start := 0
	for _, end := range r.ends {
		r.items = append(r.items, r.arena[start:end:end])
		start = end
	}
	return r.items, weight, nil
}

// parseWeight parses the weight field of a weighted record (tract.c
// ib_read, TA_WEIGHT branch).
func (r *Reader) parseWeight(field []byte) (tract.Supp, error) {
	if len(field) == 0 {
		return 0, r.syntaxError(ErrWeightExpected, field)
	}
	b := field
	var closer byte
	switch b[0] {
	case '(':
		closer = ')'
	case '[':
		closer = ']'
	case '{':
		closer = '}'
	}
	if closer != 0 {
		b = b[1:]
	}
	v, n, overflow := cconv.Strtol(b, 0)
	if n == 0 || (closer != 0 && n == len(b)) {
		return 0, r.syntaxError(ErrInvalidWeight, field)
	}
	rest := b[n:]
	if closer != 0 && rest[0] == closer {
		rest = rest[1:]
	}
	if len(rest) != 0 {
		return 0, r.syntaxError(ErrInvalidWeight, field)
	}
	if overflow {
		// Deviation from upstream: a weight outside the 64-bit range is
		// invalid. Upstream truncates LONG_MAX/LONG_MIN to int (-1/0).
		return 0, r.syntaxError(ErrInvalidWeight, field)
	}
	if v < 0 {
		// Deviation from upstream: negative weights are rejected (project
		// integer policy, docs/architecture.md); upstream accepts them
		// and produces meaningless supports (tract.md T7).
		return 0, r.syntaxError(ErrNegativeWeight, field)
	}
	// Deviation from upstream: the weight is kept as a 64-bit value;
	// upstream truncates it to a 32-bit int (tract.md T6).
	return tract.Supp(v), nil
}

func (r *Reader) syntaxError(kind error, field []byte) error {
	return newSyntaxError(r.tok, r.name, kind, field)
}

// newSyntaxError reports an error at the tokenizer's current field.
func newSyntaxError(t *tokenizer, name string, kind error, field []byte) error {
	return &SyntaxError{
		File:   name,
		Record: t.rec,
		Field:  t.fld,
		Text:   string(field),
		Err:    kind,
	}
}

// cString returns b up to its first NUL byte: upstream handles fields as
// C strings, so an embedded NUL ends the item name (tract.md T21).
func cString(b []byte) []byte {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return b[:i]
	}
	return b
}
