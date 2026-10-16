package tabread

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/dimchansky/fpgrowth-go/internal/tract"
)

// record is a transaction as returned by Reader.Next, with its record
// number.
type record struct {
	items  []string
	weight tract.Supp
	rec    int
}

func (r record) String() string {
	return fmt.Sprintf("%d:%q/%d", r.rec, r.items, r.weight)
}

// readAll reads all records of src.
func readAll(src io.Reader, cfg Config) ([]record, error) {
	r := NewReader(src, "in", cfg)
	recs := []record{}
	for {
		items, weight, err := r.Next()
		if errors.Is(err, io.EOF) {
			return recs, nil
		}
		if err != nil {
			return recs, err
		}
		names := make([]string, 0, len(items))
		for _, it := range items {
			names = append(names, string(it))
		}
		recs = append(recs, record{items: names, weight: weight, rec: r.Record()})
	}
}

// rec1 builds an expected record.
func rec1(rec int, weight tract.Supp, items ...string) record {
	if items == nil {
		items = []string{}
	}
	return record{items: items, weight: weight, rec: rec}
}

func TestReaderNext(t *testing.T) {
	t.Parallel()
	def := DefaultConfig()
	weighted := DefaultConfig()
	weighted.Weighted = true
	tests := []struct {
		name     string
		cfg      Config
		input    string
		expected []record
		err      error  // expected error kind, if any
		errMsg   string // expected error message, if any
	}{
		{name: "empty input", cfg: def, input: "", expected: []record{}},
		{
			name:     "items and records",
			cfg:      def,
			input:    "a b c\nd\n",
			expected: []record{rec1(1, 1, "a", "b", "c"), rec1(2, 1, "d")},
		},
		{
			name:     "empty and blank lines are empty transactions",
			cfg:      def,
			input:    "a\n\n \t\nb",
			expected: []record{rec1(1, 1, "a"), rec1(2, 1), rec1(3, 1), rec1(4, 1, "b")},
		},
		{
			name:     "trailing separator is ignored",
			cfg:      def,
			input:    "a,b,\n",
			expected: []record{rec1(1, 1, "a", "b")},
		},
		{
			name:     "duplicates are kept",
			cfg:      def,
			input:    "a b a\n",
			expected: []record{rec1(1, 1, "a", "b", "a")},
		},
		{
			name:     "comments are skipped and counted",
			cfg:      def,
			input:    "#h\na\n#m\nb\n",
			expected: []record{rec1(2, 1, "a"), rec1(4, 1, "b")},
		},
		{
			name:     "nul cuts an item",
			cfg:      def,
			input:    "a\x00b c\n",
			expected: []record{rec1(1, 1, "a", "c")},
		},
		{
			name:     "nul at the start of the last field makes it empty",
			cfg:      def,
			input:    "a \x00x\n",
			expected: []record{rec1(1, 1, "a")},
		},
		{
			name:     "high bytes are kept",
			cfg:      def,
			input:    "\xc3\xa9 \xff\n",
			expected: []record{rec1(1, 1, "\xc3\xa9", "\xff")},
		},
		{
			// Deviation from upstream (T3): the last record is kept.
			name:     "trailing separator at end of input",
			cfg:      def,
			input:    "a b\nc d,",
			expected: []record{rec1(1, 1, "a", "b"), rec1(2, 1, "c", "d")},
		},
		{
			name:     "empty field before another field",
			cfg:      def,
			input:    "a\nb,,c\n",
			expected: []record{rec1(1, 1, "a")},
			err:      ErrItemExpected,
			errMsg:   "in:2(2): item expected",
		},
		{
			name:   "nul at the start of a middle field",
			cfg:    def,
			input:  "\x00x a\n",
			err:    ErrItemExpected,
			errMsg: "in:1(1): item expected",
		},
		{
			name:     "weights",
			cfg:      weighted,
			input:    "a b 2\n7\nc (3)\nd [0x10]\ne {010}\nf (+3)\ng -0\nh 1 \n",
			expected: []record{rec1(1, 2, "a", "b"), rec1(2, 7), rec1(3, 3, "c"), rec1(4, 16, "d"), rec1(5, 8, "e"), rec1(6, 3, "f"), rec1(7, 0, "g"), rec1(8, 1, "h")},
		},
		{
			// Deviation from upstream (T6): no truncation to int.
			name:     "weights beyond int32",
			cfg:      weighted,
			input:    "a 4294967297\nb 9223372036854775807\n",
			expected: []record{rec1(1, 4294967297, "a"), rec1(2, math.MaxInt64, "b")},
		},
		{
			name:     "weight beyond int64",
			cfg:      weighted,
			input:    "a 1\nb 9223372036854775808\n",
			expected: []record{rec1(1, 1, "a")},
			err:      ErrInvalidWeight,
			errMsg:   "in:2(2): invalid transaction weight 9223372036854775808",
		},
		{
			name:   "negative weight",
			cfg:    weighted,
			input:  "a -1\n",
			err:    ErrNegativeWeight,
			errMsg: "in:1(2): invalid transaction weight -1 (must not be negative)",
		},
		{
			name:   "item as weight",
			cfg:    weighted,
			input:  "a\n",
			err:    ErrInvalidWeight,
			errMsg: "in:1(1): invalid transaction weight a",
		},
		{
			name:   "empty line in weighted input",
			cfg:    weighted,
			input:  "\n",
			err:    ErrWeightExpected,
			errMsg: "in:1(1): transaction weight expected",
		},
		{
			name:   "unclosed bracket",
			cfg:    weighted,
			input:  "a (3\n",
			err:    ErrInvalidWeight,
			errMsg: "in:1(2): invalid transaction weight (3",
		},
		{
			name:   "weight with nul keeps the text before it",
			cfg:    weighted,
			input:  "a (3\x00)\n",
			err:    ErrInvalidWeight,
			errMsg: "in:1(2): invalid transaction weight (3",
		},
		{
			// Deviation from upstream (B10): upstream reports record 2.
			name:     "blank only last record in weighted input",
			cfg:      weighted,
			input:    "a 1\nb 2\n   ",
			expected: []record{rec1(1, 1, "a"), rec1(2, 2, "b")},
			err:      ErrWeightExpected,
			errMsg:   "in:3(1): transaction weight expected",
		},
		{
			name:     "comment records count for error positions",
			cfg:      weighted,
			input:    "#x\na 1\n#y\nb z\n",
			expected: []record{rec1(2, 1, "a")},
			err:      ErrInvalidWeight,
			errMsg:   "in:4(2): invalid transaction weight z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, src := range []io.Reader{
				strings.NewReader(tt.input),
				iotest.OneByteReader(strings.NewReader(tt.input)),
			} {
				got, err := readAll(src, tt.cfg)
				if fmt.Sprint(got) != fmt.Sprint(tt.expected) && !(len(got) == 0 && len(tt.expected) == 0) {
					t.Errorf("records = %v, want %v", got, tt.expected)
				}
				switch {
				case tt.err == nil && err != nil:
					t.Errorf("unexpected error %v", err)
				case tt.err != nil && !errors.Is(err, tt.err):
					t.Errorf("error = %v, want %v", err, tt.err)
				case tt.err != nil && err.Error() != tt.errMsg:
					t.Errorf("error message = %q, want %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestReaderSyntaxErrorFields(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.Weighted = true
	_, err := readAll(strings.NewReader("a 1\n\nb (2]\n"), cfg)
	se, ok := errors.AsType[*SyntaxError](err)
	if !ok {
		t.Fatalf("error %v is not a *SyntaxError", err)
	}
	want := SyntaxError{File: "in", Record: 2, Field: 1, Text: "", Err: ErrWeightExpected}
	if *se != want {
		t.Errorf("SyntaxError = %+v, want %+v", *se, want)
	}
}

func TestReaderStickyErrors(t *testing.T) {
	t.Parallel()
	r := NewReader(strings.NewReader("a,,b\nc\n"), "in", DefaultConfig())
	_, _, err1 := r.Next()
	_, _, err2 := r.Next()
	if err1 == nil || err1 != err2 {
		t.Errorf("Next after an error = %v, want the same error %v", err2, err1)
	}

	r = NewReader(strings.NewReader("a\n"), "in", DefaultConfig())
	if _, _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, _, err := r.Next(); !errors.Is(err, io.EOF) {
			t.Errorf("Next at the end = %v, want io.EOF", err)
		}
	}
}

func TestReaderReadError(t *testing.T) {
	t.Parallel()
	errBoom := errors.New("boom")
	r := NewReader(&errAfterReader{data: "a b\nc", err: errBoom}, "data.tab", DefaultConfig())
	if _, _, err := r.Next(); err != nil {
		t.Fatalf("first record: %v", err)
	}
	_, _, err := r.Next()
	if !errors.Is(err, ErrRead) || !errors.Is(err, errBoom) {
		t.Fatalf("error = %v, want ErrRead wrapping boom", err)
	}
	if want := "read error on file data.tab: boom"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestReaderItemsDoNotAlias(t *testing.T) {
	t.Parallel()
	r := NewReader(strings.NewReader("ab cd ef\n"), "in", DefaultConfig())
	items, _, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	_ = append(items[0], 'X', 'Y') // must not overwrite items[1]
	if got := string(items[1]); got != "cd" {
		t.Errorf("appending to an item changed its neighbor: %q", got)
	}
}

// simpleConfig is the configuration of the fuzz model: records end at
// '\n', fields are separated by runs of spaces, '#' starts a comment.
func simpleConfig(weighted bool) Config {
	return Config{RecordSeps: "\n", FieldSeps: " ", Blanks: " ", Comments: "#", Weighted: weighted}
}

// modelRead is an independent reference reader for simpleConfig without
// weights. It returns the records and whether the input is valid.
func modelRead(input []byte) ([]record, bool) {
	recs := []record{}
	lines := bytes.Split(input, []byte("\n"))
	if len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1] // final newline or empty input
	}
	for i, line := range lines {
		if len(line) > 0 && line[0] == '#' {
			continue
		}
		fields := bytes.FieldsFunc(line, func(r rune) bool { return r == ' ' }) // only ' ' separates
		items := []string{}
		for j, f := range fields {
			if k := bytes.IndexByte(f, 0); k >= 0 {
				f = f[:k]
			}
			if len(f) == 0 {
				if j == len(fields)-1 {
					break // empty last field: end of the transaction
				}
				return recs, false
			}
			items = append(items, string(f))
		}
		recs = append(recs, record{items: items, weight: 1, rec: i + 1})
	}
	return recs, true
}

// serialize writes records in simpleConfig syntax. A leading blank keeps
// items starting with '#' from becoming comments.
func serialize(recs []record, weighted bool) []byte {
	var b bytes.Buffer
	for _, r := range recs {
		b.WriteByte(' ')
		b.WriteString(strings.Join(r.items, " "))
		if weighted {
			fmt.Fprintf(&b, " %d", r.weight)
		}
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func sameRecords(a, b []record, withRec bool) bool {
	return slices.EqualFunc(a, b, func(x, y record) bool {
		return slices.Equal(x.items, y.items) && x.weight == y.weight && (!withRec || x.rec == y.rec)
	})
}

func FuzzReader(f *testing.F) {
	f.Add([]byte("a b c\nd\n"), "\n", " \t,", " \t\r", "#", false)
	f.Add([]byte("#c\n a,,b\n\n"), "\n", ",", " ", "#", false)
	f.Add([]byte("a 2\nb (3)\n7\n"), "\n", " ", " ", "", true)
	f.Add([]byte("a\x00b c,\r\nd\xff\te,"), ";\n", "\t,", "\r", "%#", false)
	f.Add([]byte("x;y;;z"), ";", ";", ";", ";", true)
	f.Fuzz(func(t *testing.T, input []byte, recSeps, fldSeps, blanks, comments string, weighted bool) {
		cfg := Config{RecordSeps: recSeps, FieldSeps: fldSeps, Blanks: blanks, Comments: comments, Weighted: weighted}
		got, err := readAll(bytes.NewReader(input), cfg)

		// The result does not depend on how the source splits its data.
		got1, err1 := readAll(iotest.OneByteReader(bytes.NewReader(input)), cfg)
		if !sameRecords(got, got1, true) || fmt.Sprint(err) != fmt.Sprint(err1) {
			t.Fatalf("one-byte reads differ: %v %v vs %v %v", got, err, got1, err1)
		}
		if err != nil && !errors.As(err, new(*SyntaxError)) {
			t.Fatalf("unexpected error type %T: %v", err, err)
		}

		// Structural invariants of every transaction.
		table := newClassTable(cfg)
		prev := 0
		for _, r := range got {
			if r.rec <= prev {
				t.Fatalf("record numbers not increasing: %v", got)
			}
			prev = r.rec
			if r.weight < 0 || (!weighted && r.weight != 1) {
				t.Fatalf("bad weight in %v", r)
			}
			for _, it := range r.items {
				if it == "" || strings.IndexByte(it, 0) >= 0 || table[it[0]]&classBlank != 0 {
					t.Fatalf("bad item %q in %v", it, r)
				}
				for i := range len(it) {
					if table[it[i]]&classSep != 0 {
						t.Fatalf("item %q contains a separator", it)
					}
				}
			}
		}

		// Model check and round trip under the simple configuration.
		simple, serr := readAll(bytes.NewReader(input), simpleConfig(false))
		want, valid := modelRead(input)
		if valid != (serr == nil) || !sameRecords(simple, want, true) {
			t.Fatalf("simple config: got %v (%v), model %v (valid %t)", simple, serr, want, valid)
		}
		if serr == nil {
			again, err := readAll(bytes.NewReader(serialize(simple, false)), simpleConfig(false))
			if err != nil || !sameRecords(again, simple, false) {
				t.Fatalf("round trip: %v (%v), want %v", again, err, simple)
			}
		}
		if weighted && err == nil {
			again, err := readAll(bytes.NewReader(serialize(got, true)), simpleConfig(true))
			ok := true
			for _, r := range got {
				for _, it := range r.items {
					ok = ok && !strings.ContainsAny(it, " \n")
				}
			}
			if ok && (err != nil || !sameRecords(again, got, false)) {
				t.Fatalf("weighted round trip: %v (%v), want %v", again, err, got)
			}
		}
	})
}
