package tabread

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// The optimized tokenizer (issue #48: fields as views of the read buffer,
// the plainField fast path) must behave exactly like the tokenizer before
// it, refTokenizer, for every input, configuration, buffer size and way
// the source splits its data.

// tokenDump is the result of one read: delimiter, record and field
// number and the field's bytes.
type tokenDump struct {
	d        delim
	rec, fld int
	field    string
}

// dumpTokens reads src to the end (or the first error) with read and
// returns every result, plus the error.
func dumpTokens(read func() (delim, int, int, []byte), errOf func() error) ([]tokenDump, string) {
	var out []tokenDump
	for range 1 << 16 { // bounded: inputs are small
		d, rec, fld, f := read()
		out = append(out, tokenDump{d, rec, fld, string(f)})
		if d == delimErr {
			return out, fmt.Sprint(errOf())
		}
		if d == delimEOF {
			return out, ""
		}
	}
	return out, "no end"
}

func optTokens(src io.Reader, cfg Config, size int) ([]tokenDump, string) {
	t := newTokenizerSize(src, cfg, size)
	return dumpTokens(func() (delim, int, int, []byte) {
		d := t.read()
		return d, t.rec, t.fld, t.field
	}, t.err)
}

func refTokens(src io.Reader, cfg Config) ([]tokenDump, string) {
	t := newRefTokenizer(src, cfg)
	return dumpTokens(func() (delim, int, int, []byte) {
		d := t.read()
		return d, t.rec, t.fld, t.field
	}, t.err)
}

// readers returns the sources of data for one comparison: whole blocks,
// one byte per read, half blocks, and data together with io.EOF.
func readers(data []byte) map[string]io.Reader {
	return map[string]io.Reader{
		"bytes":   bytes.NewReader(data),
		"onebyte": iotest.OneByteReader(bytes.NewReader(data)),
		"half":    iotest.HalfReader(bytes.NewReader(data)),
		"dataEOF": iotest.DataErrReader(bytes.NewReader(data)),
	}
}

// checkAgainstReference compares the optimized tokenizer with refTokenizer
// on data for the buffer sizes 1 to 9 and the default size.
func checkAgainstReference(t *testing.T, data []byte, cfg Config) {
	t.Helper()
	want, wantErr := refTokens(bytes.NewReader(data), cfg)
	for _, size := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, bufSize} {
		for name, src := range readers(data) {
			got, gotErr := optTokens(src, cfg, size)
			if gotErr != wantErr || fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("size %d, %s reader, input %q, cfg %+v:\ngot  %v %s\nwant %v %s",
					size, name, data, cfg, got, gotErr, want, wantErr)
			}
		}
	}
}

func TestTokenizerMatchesReference(t *testing.T) {
	t.Parallel()
	def := DefaultConfig()
	sepBlank := Config{RecordSeps: "\n", FieldSeps: " ,", Blanks: " ", Comments: "#"}
	recFld := Config{RecordSeps: ";\n", FieldSeps: ";,", Blanks: "\t", Comments: "%"}
	inputs := []string{
		"", "\n", "a", "a\n", "a b c\n", "a  b\t,c \r\n", "a,b,\n", "a,,b\n",
		"#c\na b\n", " #x\n", "a#b c#\n", "a\x00b c\n", "\xff\xfe x\n",
		"a b\nc d\n\ne\n", "  \n \t\n", "a ,\n b", "ab cd ef gh ij kl mn",
		"a;b;c\n", "x;y,;z\n", "p0 p17 p171 p23 p80\np0 p1\n",
	}
	for _, cfg := range []Config{def, sepBlank, recFld} {
		for _, in := range inputs {
			checkAgainstReference(t, []byte(in), cfg)
		}
	}
}

func FuzzTokenizerReference(f *testing.F) {
	f.Add([]byte("a b c\nd\n"), "\n", " \t,", " \t\r", "#")
	f.Add([]byte("#c\n a,,b\n\n"), "\n", ",", " ", "#")
	f.Add([]byte("a\x00b c,\r\nd\xff\te,"), ";\n", "\t,", "\r", "%#")
	f.Add([]byte("x;y;;z"), ";", ";", ";", ";")
	f.Add([]byte("p0 p17 p171\np0 p1 p16\n"), "\n", " \t,", " \t\r", "#")
	f.Fuzz(func(t *testing.T, input []byte, recSeps, fldSeps, blanks, comments string) {
		if len(input) > 4096 {
			return
		}
		cfg := Config{RecordSeps: recSeps, FieldSeps: fldSeps, Blanks: blanks, Comments: comments}
		checkAgainstReference(t, input, cfg)
	})
}

// TestTokenizerFieldSurvivesRefill checks that a field held as a view of
// the read buffer is copied before a refill overwrites the buffer: the
// field ends at a separator that is the last byte of a block, and step 6
// (blanks after a field separator) must read the next block.
func TestTokenizerFieldSurvivesRefill(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	for size := 2; size <= 8; size++ {
		field := strings.Repeat("x", size-1) // field + separator fill a block
		input := field + " " + strings.Repeat("y", size) + "\n"
		tok := newTokenizerSize(bytes.NewReader([]byte(input)), cfg, size)
		if d := tok.read(); d != delimFld || string(tok.field) != field {
			t.Fatalf("size %d: first field %v %q, want FLD %q", size, d, tok.field, field)
		}
	}
}
