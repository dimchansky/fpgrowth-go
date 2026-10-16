package fpgrowth

import (
	"bufio"
	"cmp"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CSVFormat describes a CSV file (RFC 4180, read with encoding/csv) in
// which every record is one transaction and every field one item. Quoted
// fields may contain commas, quotes ("") and newlines. The zero value is
// a valid format: comma-separated, no comments, no header, no weights,
// empty fields ignored.
type CSVFormat struct {
	// Comma is the field delimiter; 0 means ','. It must be a valid rune
	// other than '"', '\r', '\n' and the Unicode replacement character.
	Comma rune
	// Comment, if not 0, starts comment lines: a line beginning with it
	// (without leading white space) is skipped.
	Comment rune
	// HasHeader skips the first record (after comment lines).
	HasHeader bool
	// WeightColumn selects a column holding the transaction weight; that
	// field is not an item. 0 means no weight column (every transaction
	// has weight 1), k > 0 the k-th field (1-based), k < 0 the |k|-th field
	// from the end (-1 is the last field, upstream's convention). The
	// weight is a decimal int64 (optional sign; surrounding white space is
	// ignored); it must not be negative. A record without that field is an
	// error.
	WeightColumn int
	// TrimSpace removes leading and trailing Unicode white space from
	// every field, also from quoted ones, and allows white space before
	// an opening quote (csv.Reader.TrimLeadingSpace). White space after a
	// closing quote is still a syntax error.
	TrimSpace bool
	// KeepEmptyFields makes empty fields (after trimming) items named "".
	// By default they are ignored, so ragged rows such as "a,b,," are
	// transactions {a, b}.
	KeepEmptyFields bool
	// LazyQuotes is csv.Reader.LazyQuotes: a quote may appear in an
	// unquoted field and a non-doubled quote in a quoted field.
	LazyQuotes bool
}

// validate checks the delimiters like encoding/csv does, so that an
// invalid format is reported as such before the file is opened.
func (f CSVFormat) validate() error {
	valid := func(r rune) bool {
		return r != '"' && r != '\r' && r != '\n' && utf8.ValidRune(r) && r != utf8.RuneError
	}
	switch {
	case f.Comma != 0 && !valid(f.Comma):
		return invalid("CSVFormat.Comma", "invalid delimiter %q", f.Comma)
	case f.Comment != 0 && !valid(f.Comment):
		return invalid("CSVFormat.Comment", "invalid comment character %q", f.Comment)
	case f.Comment != 0 && f.Comment == cmp.Or(f.Comma, ','):
		return invalid("CSVFormat.Comment", "equals the delimiter %q", f.Comment)
	}
	return nil
}

// utf8BOM is the byte order mark that some programs write at the start of
// UTF-8 CSV files.
const utf8BOM = "\xef\xbb\xbf"

// CSVFile returns a Source over the CSV file at path. Every pass opens the
// file, streams it record by record, and closes it before the pass ends,
// also on errors and when the consumer stops early; the file is never
// loaded into memory as a whole. The file must not change between passes.
//
// Semantics beyond encoding/csv: a UTF-8 byte order mark at the start of
// the file is skipped; records may have different numbers of fields;
// blank lines are skipped by encoding/csv and are therefore not
// transactions, but a record whose fields are all empty is an empty
// transaction; repeated items in a record count once. Equal item names
// share one string within a pass.
//
// Errors: an invalid Comma or Comment yields an error wrapping
// ErrInvalidOptions; a file that cannot be opened or read yields an error wrapping
// the underlying I/O error (an *os.PathError for open failures); malformed
// CSV yields an error wrapping ErrSyntax and the *csv.ParseError; an
// invalid weight yields an error wrapping ErrSyntax, a negative one
// ErrNegativeWeight. Messages start with "<path>:<line>:".
func CSVFile(path string, f CSVFormat) Source[string] {
	return csvSource{path: path, format: f}
}

type csvSource struct {
	path   string
	format CSVFormat
}

func (s csvSource) Transactions() iter.Seq2[Transaction[string], error] {
	return func(yield func(Transaction[string], error) bool) {
		if err := s.pass(yield); err != nil {
			yield(Transaction[string]{}, err)
		}
	}
}

// pass reads the file once. It returns nil when the consumer stopped.
func (s csvSource) pass(yield func(Transaction[string], error) bool) (err error) {
	if err := s.format.validate(); err != nil {
		return err
	}
	file, err := os.Open(s.path)
	if err != nil {
		return err
	}
	stopped := false
	defer func() {
		cerr := file.Close()
		if err == nil && !stopped && cerr != nil {
			err = fmt.Errorf("read error on file %s: %w", s.path, cerr)
		}
	}()
	br := bufio.NewReader(file)
	if b, perr := br.Peek(len(utf8BOM)); perr == nil && string(b) == utf8BOM {
		_, _ = br.Discard(len(utf8BOM)) // cannot fail after a successful Peek
	}
	f := s.format
	r := csv.NewReader(br)
	if f.Comma != 0 {
		r.Comma = f.Comma
	}
	r.Comment = f.Comment
	r.FieldsPerRecord = -1
	r.LazyQuotes = f.LazyQuotes
	r.TrimLeadingSpace = f.TrimSpace
	r.ReuseRecord = true

	intern := map[string]string{}
	var items []string
	for first := true; ; first = false {
		rec, rerr := r.Read()
		if errors.Is(rerr, io.EOF) {
			return nil
		}
		if rerr != nil {
			if perr, ok := errors.AsType[*csv.ParseError](rerr); ok {
				return newDetailError(s.path+": "+perr.Error(), ErrSyntax, perr)
			}
			return fmt.Errorf("read error on file %s: %w", s.path, rerr)
		}
		if first && f.HasHeader {
			continue
		}
		wcol := -1
		weight := int64(1)
		if f.WeightColumn != 0 {
			wcol = f.WeightColumn - 1
			if f.WeightColumn < 0 {
				wcol = len(rec) + f.WeightColumn
			}
			if wcol < 0 || wcol >= len(rec) {
				line, _ := r.FieldPos(0)
				return newDetailError(fmt.Sprintf("%s:%d: transaction weight expected (column %d)",
					s.path, line, f.WeightColumn), ErrSyntax)
			}
			w, werr := s.parseWeight(r, rec[wcol], wcol)
			if werr != nil {
				return werr
			}
			weight = w
		}
		items = items[:0]
		for j, field := range rec {
			if j == wcol {
				continue
			}
			if f.TrimSpace {
				field = strings.TrimSpace(field)
			}
			if field == "" && !f.KeepEmptyFields {
				continue
			}
			name, ok := intern[field]
			if !ok {
				// Clone: field shares memory with the whole record; the
				// item base retains the name for the rest of the run.
				name = strings.Clone(field)
				intern[name] = name
			}
			items = append(items, name)
		}
		if !yield(Transaction[string]{Items: items, Weight: weight}, nil) {
			stopped = true
			return nil
		}
	}
}

// parseWeight parses the weight field in column col of the current record.
func (s csvSource) parseWeight(r *csv.Reader, field string, col int) (int64, error) {
	line, column := r.FieldPos(col)
	text := strings.TrimSpace(field)
	if text == "" {
		return 0, newDetailError(fmt.Sprintf("%s:%d:%d: transaction weight expected",
			s.path, line, column), ErrSyntax)
	}
	w, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, newDetailError(fmt.Sprintf("%s:%d:%d: invalid transaction weight %q",
			s.path, line, column, field), ErrSyntax, err)
	}
	if w < 0 {
		return 0, newDetailError(fmt.Sprintf("%s:%d:%d: invalid transaction weight %q (must not be negative)",
			s.path, line, column, field), ErrNegativeWeight)
	}
	return w, nil
}
