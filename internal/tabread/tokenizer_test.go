package tabread

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// The dumps use the format of dumpFields: "<delim> <record> <field> <text>"
// per call of read, with bytes outside '!'..'~' as \xHH.
func TestTokenizerRead(t *testing.T) {
	t.Parallel()
	def := DefaultConfig()
	tests := []struct {
		name     string
		cfg      Config
		input    string
		expected string
	}{
		{
			name:     "empty input",
			cfg:      def,
			input:    "",
			expected: "EOF 1 1 \n",
		},
		{
			name:     "fields and records",
			cfg:      def,
			input:    "a b\nc\n",
			expected: "FLD 1 1 a\nREC 1 2 b\nREC 2 1 c\nEOF 3 1 \n",
		},
		{
			name:     "missing final newline",
			cfg:      def,
			input:    "a b",
			expected: "FLD 1 1 a\nREC 1 2 b\nEOF 2 1 \n",
		},
		{
			name:     "blanks merge with blank separators",
			cfg:      def,
			input:    "  a \t b  \n",
			expected: "FLD 1 1 a\nREC 1 2 b\nEOF 2 1 \n",
		},
		{
			name:     "blank before comma merges",
			cfg:      def,
			input:    "a ,b\n",
			expected: "FLD 1 1 a\nREC 1 2 b\nEOF 2 1 \n",
		},
		{
			name:     "two commas give an empty field",
			cfg:      def,
			input:    "a,,b\n",
			expected: "FLD 1 1 a\nFLD 1 2 \nREC 1 3 b\nEOF 2 1 \n",
		},
		{
			name:     "comma blank comma gives an empty field",
			cfg:      def,
			input:    "a, ,b\n",
			expected: "FLD 1 1 a\nFLD 1 2 \nREC 1 3 b\nEOF 2 1 \n",
		},
		{
			name:     "trailing comma gives an empty last field",
			cfg:      def,
			input:    "a,\n",
			expected: "FLD 1 1 a\nREC 1 2 \nEOF 2 1 \n",
		},
		{
			name:     "empty and blank lines are empty records",
			cfg:      def,
			input:    "\n \t\n\r\n",
			expected: "REC 1 1 \nREC 2 1 \nREC 3 1 \nEOF 4 1 \n",
		},
		{
			name:     "interior blanks are kept",
			cfg:      Config{RecordSeps: "\n", FieldSeps: ",", Blanks: " "},
			input:    " a  b ,c\n",
			expected: "FLD 1 1 a\\x20\\x20b\nREC 1 2 c\nEOF 2 1 \n",
		},
		{
			name:     "comment only at the first byte of a record",
			cfg:      def,
			input:    "#x y\n #a\nb #c\n",
			expected: "REC 2 1 #a\nFLD 3 1 b\nREC 3 2 #c\nEOF 4 1 \n",
		},
		{
			name:     "consecutive comment records are counted",
			cfg:      def,
			input:    "#1\n#2\n#3\na\n",
			expected: "REC 4 1 a\nEOF 5 1 \n",
		},
		{
			name:     "comment at end of input without newline",
			cfg:      def,
			input:    "a\n#c",
			expected: "REC 1 1 a\nEOF 2 1 \n",
		},
		{
			name:     "comment character inside a record is data",
			cfg:      def,
			input:    "a,#\n",
			expected: "FLD 1 1 a\nREC 1 2 #\nEOF 2 1 \n",
		},
		{
			name:     "crlf",
			cfg:      def,
			input:    "a b\r\n",
			expected: "FLD 1 1 a\nREC 1 2 b\nEOF 2 1 \n",
		},
		{
			name:     "cr only is data",
			cfg:      def,
			input:    "a\rb\n",
			expected: "REC 1 1 a\\x0db\nEOF 2 1 \n",
		},
		{
			name:     "nul and high bytes are data",
			cfg:      def,
			input:    "a\x00b \xff\x80\n",
			expected: "FLD 1 1 a\\x00b\nREC 1 2 \\xff\\x80\nEOF 2 1 \n",
		},
		{
			name:     "field separator wins over record separator at field start",
			cfg:      Config{RecordSeps: ";", FieldSeps: ";"},
			input:    ";a;",
			expected: "FLD 1 1 \nREC 1 2 a\nEOF 2 1 \n",
		},
		{
			name:     "separator that is both ends the record after a field",
			cfg:      Config{RecordSeps: "\n;", FieldSeps: " ;"},
			input:    "a;b\n",
			expected: "REC 1 1 a\nREC 2 1 b\nEOF 3 1 \n",
		},
		{
			name:     "blank wins over separators at field start",
			cfg:      Config{RecordSeps: "\n", FieldSeps: ",", Blanks: ",\n"},
			input:    ",,a,,\n\nb",
			expected: "FLD 1 1 a\nREC 1 2 b\nEOF 2 1 \n",
		},
		{
			name:     "comment byte that is also blank starts a comment",
			cfg:      Config{RecordSeps: "\n", FieldSeps: " ", Blanks: " ", Comments: " "},
			input:    " x\ny\n",
			expected: "REC 2 1 y\nEOF 3 1 \n",
		},
		{
			name:     "no record separator",
			cfg:      Config{FieldSeps: " "},
			input:    "a b\nc",
			expected: "FLD 1 1 a\nREC 1 2 b\\x0ac\nEOF 2 1 \n",
		},
		{
			name:     "zero config",
			cfg:      Config{},
			input:    "a b\nc",
			expected: "REC 1 1 a\\x20b\\x0ac\nEOF 2 1 \n",
		},
		{
			// Deviation from upstream (T3).
			name:     "field separator at end of input",
			cfg:      def,
			input:    "a,",
			expected: "FLD 1 1 a\nREC 1 2 \nEOF 2 1 \n",
		},
		{
			// Deviation from upstream (B10): upstream reports record 1.
			name:     "blank only record at end of input",
			cfg:      def,
			input:    "a\n  ",
			expected: "REC 1 1 a\nREC 2 1 \nEOF 3 1 \n",
		},
		{
			// Deviation from upstream (B10): upstream reports record 1.
			name:     "trailing blank separator at end of input",
			cfg:      def,
			input:    "a\nb ",
			expected: "REC 1 1 a\nREC 2 1 b\nEOF 3 1 \n",
		},
		{
			// Deviation from upstream (B14): no truncation at 1024 bytes.
			name:     "long field",
			cfg:      def,
			input:    strings.Repeat("x", 3000) + "\n",
			expected: "REC 1 1 " + strings.Repeat("x", 3000) + "\nEOF 2 1 \n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := dumpFields(strings.NewReader(tt.input), tt.cfg); got != tt.expected {
				t.Errorf("fields of %q:\ngot:\n%s\nwant:\n%s", tt.input, got, tt.expected)
			}
			// The result must not depend on how the source splits its data.
			if got := dumpFields(iotest.OneByteReader(strings.NewReader(tt.input)), tt.cfg); got != tt.expected {
				t.Errorf("fields of %q with one-byte reads:\ngot:\n%s\nwant:\n%s", tt.input, got, tt.expected)
			}
			if got := dumpFields(iotest.DataErrReader(strings.NewReader(tt.input)), tt.cfg); got != tt.expected {
				t.Errorf("fields of %q with data and EOF together:\ngot:\n%s\nwant:\n%s", tt.input, got, tt.expected)
			}
		})
	}
}

// TestTokenizerBufferBoundaries checks fields, blanks and separators that
// straddle the 64 KiB buffer boundary.
func TestTokenizerBufferBoundaries(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	for _, offset := range []int{-3, -2, -1, 0, 1, 2} {
		var b strings.Builder
		b.WriteString(strings.Repeat("y", bufSize+offset))
		b.WriteString(" ,\t z \r\n#c\n  w,\nv,")
		input := b.String()
		want := dumpFields(iotest.OneByteReader(strings.NewReader(input)), cfg)
		for name, src := range map[string]io.Reader{
			"bytes":   bytes.NewReader([]byte(input)),
			"half":    iotest.HalfReader(strings.NewReader(input)),
			"dataEOF": iotest.DataErrReader(strings.NewReader(input)),
		} {
			if got := dumpFields(src, cfg); got != want {
				t.Errorf("offset %d, %s reader: dumps differ", offset, name)
			}
		}
		if !strings.HasPrefix(want, "FLD 1 1 yyy") || !strings.Contains(want, "REC 1 2 z\nFLD 3 1 w\nREC 3 2 \nFLD 4 1 v\nREC 4 2 \n") {
			t.Errorf("offset %d: unexpected dump tail %q", offset, want[len(want)-80:])
		}
	}
}

// errAfterReader returns data, then err.
type errAfterReader struct {
	data string
	err  error
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if r.data == "" {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// zeroReader returns (0, nil) forever.
type zeroReader struct{}

func (zeroReader) Read([]byte) (int, error) { return 0, nil }

// badCountReader returns more bytes than the buffer holds.
type badCountReader struct{}

func (badCountReader) Read(p []byte) (int, error) { return len(p) + 1, nil }

func TestTokenizerReadErrors(t *testing.T) {
	t.Parallel()
	errBoom := errors.New("boom")
	tests := []struct {
		name     string
		src      io.Reader
		expected string // dump up to and including the ERR line
		err      error
	}{
		{
			name:     "error at start",
			src:      &errAfterReader{err: errBoom},
			expected: "ERR 1 1 \n",
			err:      errBoom,
		},
		{
			name:     "error inside a field",
			src:      &errAfterReader{data: "a b", err: errBoom},
			expected: "FLD 1 1 a\nERR 1 2 b\n",
			err:      errBoom,
		},
		{
			name:     "error after data returned with the error",
			src:      iotest.TimeoutReader(strings.NewReader("a\nb")),
			expected: "REC 1 1 a\nERR 2 1 b\n",
			err:      iotest.ErrTimeout,
		},
		{
			name:     "error in a comment",
			src:      &errAfterReader{data: "#c", err: errBoom},
			expected: "ERR 1 1 \n",
			err:      errBoom,
		},
		{
			name:     "error while skipping blanks",
			src:      &errAfterReader{data: "a, ", err: errBoom},
			expected: "FLD 1 1 a\nERR 1 2 \n",
			err:      errBoom,
		},
		{
			name:     "no progress",
			src:      zeroReader{},
			expected: "ERR 1 1 \n",
			err:      io.ErrNoProgress,
		},
		{
			name:     "invalid count",
			src:      badCountReader{},
			expected: "ERR 1 1 \n",
			err:      errInvalidRead,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tok := newTokenizer(tt.src, DefaultConfig())
			var got strings.Builder
			names := map[delim]string{delimErr: "ERR", delimEOF: "EOF", delimFld: "FLD", delimRec: "REC"}
			for range 10 {
				d := tok.read()
				fmt.Fprintf(&got, "%s %d %d ", names[d], tok.rec, tok.fld)
				dumpEscape(&got, tok.field)
				got.WriteByte('\n')
				if d < 0 {
					break
				}
			}
			if got.String() != tt.expected {
				t.Errorf("dump:\ngot:\n%s\nwant:\n%s", got.String(), tt.expected)
			}
			if !errors.Is(tok.err(), tt.err) {
				t.Errorf("err() = %v, want %v", tok.err(), tt.err)
			}
			// The error is sticky.
			if d := tok.read(); d != delimErr {
				t.Errorf("read after error = %d, want delimErr", d)
			}
		})
	}
}
