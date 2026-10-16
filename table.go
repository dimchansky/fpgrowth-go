package fpgrowth

import (
	"errors"
	"iter"

	"github.com/dimchansky/fpgrowth-go/internal/tabread"
	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// TableFormat describes upstream fpgrowth's flexible text format, the
// format of the command line program's input file. Each of the four
// string fields is a class of bytes; a byte may belong to several classes.
// The strings hold literal bytes (upstream's escape notation such as "\t" written as a backslash
// and a letter is not decoded). Bytes >= 0x80 are ordinary bytes; UTF-8 is
// not interpreted.
//
// A record ends at a record separator and is one transaction; a field ends
// at a field separator and is one item. Blanks around a field are dropped,
// a blank that is also a field separator merges with neighboring blanks.
// A record whose first byte is a comment character is skipped. Empty lines
// are empty transactions. An item ends at its first NUL byte.
//
// The zero TableFormat has no special bytes at all, so the whole input is
// one item of one transaction; start from DefaultTableFormat and replace
// what differs.
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

func (f TableFormat) config() tabread.Config {
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
// never loaded into memory as a whole. The file must not change between
// passes (mining detects most changes and fails with ErrNotReplayable).
//
// Errors: a file that cannot be opened or read yields an error wrapping
// the *os.PathError (or other I/O error); malformed input yields an error
// wrapping ErrSyntax (ErrNegativeWeight for a negative weight) whose
// message has upstream's form "<path>:<record>(<field>): <message>".
func TableFile(path string, f TableFormat) Source[string] {
	return tableSource{scan: tabread.ScanFile(path, f.config())}
}

// tableSource adapts a tabread scan to a Source.
type tableSource struct {
	scan tract.ScanFunc[string]
}

func (s tableSource) Transactions() iter.Seq2[Transaction[string], error] {
	return func(yield func(Transaction[string], error) bool) {
		stopped := false
		err := s.scan(func(items []string, w tract.Supp) bool {
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
	// "cannot open file <path>: ..." and "read error on file <path>: ...",
	// wrapping the *os.PathError
	return err
}
