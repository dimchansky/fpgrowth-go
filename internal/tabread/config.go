package tabread

// Config describes the input format. The four byte sets are the upstream
// character classes (tabread.c trd_allchs); a byte may belong to several
// classes, with the precedence rules of the upstream tokenizer (see the
// package documentation). The strings hold literal bytes; option values
// given in upstream's escape notation are decoded with DecodeEscapes
// first.
//
// The zero Config has no special bytes at all: the whole input is one
// field of one record. Use DefaultConfig and replace the classes that an
// option overrides; like upstream's -r/-f/-b/-C, setting a class replaces
// it completely, and an empty string empties it (-C "" disables
// comments).
type Config struct {
	// RecordSeps are the record separators (upstream -r, default "\n").
	RecordSeps string
	// FieldSeps are the field separators (upstream -f, default " \t,").
	FieldSeps string
	// Blanks are the blank characters (upstream -b, default " \t\r").
	Blanks string
	// Comments are the comment characters (upstream -C, default "#"). A
	// record whose first byte is one of them is skipped.
	Comments string
	// Weighted makes the last field of every transaction record an integer
	// weight (upstream -w, TA_WEIGHT). It applies to Reader, Scan and
	// ScanFile only; selection and appearance files have no weights.
	Weighted bool
}

// DefaultConfig returns upstream's default format: records end at "\n",
// fields are separated by space, tab or comma, blanks are space, tab and
// carriage return (so CRLF line ends work), "#" starts a comment record,
// and there are no weights.
//
// Upstream also marks '?' and '*' as null-value characters by default,
// but fpgrowth clears that class (trd_allchs(..., "", ...)), so it is not
// modeled here.
func DefaultConfig() Config {
	return Config{
		RecordSeps: "\n",
		FieldSeps:  " \t,",
		Blanks:     " \t\r",
		Comments:   "#",
	}
}

// byteClass is a set of upstream character class flags (tabread.h).
type byteClass uint8

const (
	classRecSep  byteClass = 1 << iota // TRD_RECSEP
	classFldSep                        // TRD_FLDSEP
	classBlank                         // TRD_BLANK
	classComment                       // TRD_COMMENT

	classSep = classRecSep | classFldSep
)

// classTable maps every byte value to its classes (upstream flags[256]).
type classTable [256]byteClass

// newClassTable builds the class table of cfg.
//
// Deviation from upstream: bytes >= 0x80 become class members. Upstream
// decodes the class strings into a signed char and indexes its table with
// the (negative) value, writing outside the table, so such bytes never
// become separators (cli-io.md B4).
func newClassTable(cfg Config) classTable {
	var t classTable
	for _, set := range []struct {
		bytes string
		class byteClass
	}{
		{bytes: cfg.RecordSeps, class: classRecSep},
		{bytes: cfg.FieldSeps, class: classFldSep},
		{bytes: cfg.Blanks, class: classBlank},
		{bytes: cfg.Comments, class: classComment},
	} {
		for i := range len(set.bytes) {
			t[set.bytes[i]] |= set.class
		}
	}
	return t
}
