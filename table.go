package fpgrowth

import (
	"errors"
	"io"
	"iter"

	"github.com/dimchansky/fpgrowth-go/internal/tabread"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// TableFormat describes upstream fpgrowth's flexible text format, the
// format of the command line program's input file. Each of the four
// string fields is a class of bytes; a byte may belong to several classes.
// The strings hold literal bytes (upstream's escape notation such as "\t"
// written as a backslash and a letter is not decoded). Bytes >= 0x80 are
// ordinary bytes; UTF-8 is not interpreted.
//
// A record ends at a record separator and is one transaction; a field ends
// at a field separator and is one item. Blanks around a field are dropped,
// a blank that is also a field separator merges with neighboring blanks.
// A record whose first byte is a comment character is skipped. An item
// ends at its first NUL byte. Without Weighted, an empty line is an empty
// transaction; with Weighted it is a syntax error ("transaction weight
// expected"), as upstream.
//
// The zero TableFormat means DefaultTableFormat(). Any other value is
// taken literally: a field left empty is an empty class, so start from
// DefaultTableFormat and replace what differs (Comments = "" disables
// comments, FieldSeparators = "\t" reads tab-separated items with spaces
// inside). A format without any special byte (the whole input one item)
// cannot be expressed and is not useful.
type TableFormat struct {
	// RecordSeparators separate records (upstream -r, default "\n").
	RecordSeparators string
	// FieldSeparators separate fields (upstream -f, default " \t,").
	FieldSeparators string
	// Blanks are dropped around fields (upstream -b, default " \t\r", so
	// CRLF line ends work).
	Blanks string
	// Comments start comment records (upstream -C, default "#").
	Comments string
	// Weighted makes the last field of every record the transaction
	// weight (upstream -w): an integer in C syntax (decimal, 0x hex or 0
	// octal, optional sign), optionally enclosed in (), [] or {}. A record
	// with only a weight is an empty transaction. Negative weights are an
	// error.
	Weighted bool
}

// DefaultTableFormat returns upstream's default format: one transaction
// per line, items separated by spaces, tabs or commas, "#" starts a
// comment line, no weights.
func DefaultTableFormat() TableFormat {
	c := tabread.DefaultConfig()
	return TableFormat{
		RecordSeparators: c.RecordSeps,
		FieldSeparators:  c.FieldSeps,
		Blanks:           c.Blanks,
		Comments:         c.Comments,
		Weighted:         c.Weighted,
	}
}

// config returns the reader configuration; the zero format is the
// default one.
func (f TableFormat) config() tabread.Config {
	if f == (TableFormat{}) {
		return tabread.DefaultConfig()
	}
	return tabread.Config{
		RecordSeps: f.RecordSeparators,
		FieldSeps:  f.FieldSeparators,
		Blanks:     f.Blanks,
		Comments:   f.Comments,
		Weighted:   f.Weighted,
	}
}

// TableFile returns a Source over the file at path in the given format.
// Every pass opens the file, streams it, and closes it before the pass
// ends, also on errors and when the consumer stops early; the file is
// never loaded into memory as a whole. It is TableReader with os.Open.
//
// The file must not change between passes: mining compares its size and
// modification time at the start of every pass with those of the first
// pass and fails with ErrNotReplayable if they differ (and detects most
// other changes by the transaction count and total weight).
//
// Errors: a file that cannot be opened yields an error "cannot open file
// <path>: ..." wrapping the *os.PathError, a failed read "read error on
// file <path>: ..." wrapping the I/O error; malformed input yields an
// error wrapping ErrSyntax (ErrNegativeWeight for a negative weight) whose
// message has upstream's form "<path>:<record>(<field>): <message>".
func TableFile(path string, f TableFormat) Source[string] {
	return TableReader(openFile(path), path, f)
}

// TableReader returns a Source over the input that open returns, in the
// given format, for inputs that are not plain files: compressed files,
// embedded data, object storage, an fs.FS. Every pass calls open, streams
// the returned reader to the end (or until the consumer stops or an error
// occurs) and closes it; open must return the same data every time. name
// identifies the input in error messages, which have the form described
// at TableFile.
//
// If the reader that open returns has a method Stat() (fs.FileInfo,
// error), like *os.File, mining checks its size and modification time
// between passes as TableFile does; other inputs are checked only by
// transaction count and total weight.
//
// Concurrent passes call open concurrently.
func TableReader(open func() (io.ReadCloser, error), name string, f TableFormat) Source[string] {
	return tableSource{in: opener{open: open, name: name}, cfg: f.config()}
}

// tableSource adapts tabread's reader to a Source.
type tableSource struct {
	in  opener
	cfg tabread.Config
}

func (s tableSource) forRun() Source[string] {
	s.in = s.in.forRun()
	return s
}

func (s tableSource) Transactions() iter.Seq2[Transaction[string], error] {
	return func(yield func(Transaction[string], error) bool) {
		rc, err := s.in.openPass()
		if err != nil {
			yield(Transaction[string]{}, err)
			return
		}
		scan := tabread.Scan(func() (io.ReadCloser, error) { return rc, nil }, s.in.name, s.cfg)
		stopped := false
		err = scan(func(items []string, w tract.Supp) bool {
			if !yield(Transaction[string]{Items: items, Weight: int64(w)}, nil) {
				stopped = true
				return false
			}
			return true
		})
		if err != nil && !stopped {
			yield(Transaction[string]{}, tableError(err))
		}
	}
}

// tableError translates an error of package tabread.
func tableError(err error) error {
	if se, ok := errors.AsType[*tabread.SyntaxError](err); ok {
		if errors.Is(se.Err, tabread.ErrNegativeWeight) {
			return newDetailError(se.Error(), ErrNegativeWeight)
		}
		return newDetailError(se.Error(), ErrSyntax)
	}
	// "read error on file <name>: ...", wrapping the I/O error
	return err
}
