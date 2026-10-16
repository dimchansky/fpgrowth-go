package tabread

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// goldenCase is one line of testdata/cases.txt.
type goldenCase struct {
	name string
	mode string // trans, sel or app
	file string // relative to testdata
	cfg  Config
}

// loadCases parses testdata/cases.txt. Option values are decoded with
// DecodeEscapes, as the C harness decodes them with esc_decode; the
// manifest avoids the escapes where the two differ.
func loadCases(t *testing.T) []goldenCase {
	t.Helper()
	data, err := os.ReadFile("testdata/cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	cases := []goldenCase{}
	for line := range strings.Lines(string(data)) {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if len(f) < 3 {
			t.Fatalf("cases.txt: malformed line %q", line)
		}
		c := goldenCase{name: f[0], mode: f[1], file: f[2], cfg: DefaultConfig()}
		opts := f[3:]
		for i := 0; i < len(opts); i++ {
			if opts[i] == "-w" {
				c.cfg.Weighted = true
				continue
			}
			if i+1 >= len(opts) {
				t.Fatalf("cases.txt: %s: option %s without value", c.name, opts[i])
			}
			value := opts[i+1]
			if value == "''" {
				value = ""
			}
			value = DecodeEscapes(value)
			switch opts[i] {
			case "-r":
				c.cfg.RecordSeps = value
			case "-f":
				c.cfg.FieldSeps = value
			case "-b":
				c.cfg.Blanks = value
			case "-C":
				c.cfg.Comments = value
			default:
				t.Fatalf("cases.txt: %s: unknown option %s", c.name, opts[i])
			}
			i++
		}
		cases = append(cases, c)
	}
	return cases
}

// loadGolden parses testdata/golden.txt into sections keyed by
// "<case> <mode>".
func loadGolden(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile("testdata/golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	sections := map[string]string{}
	var key string
	var body strings.Builder
	flush := func() {
		if key != "" {
			sections[key] = body.String()
		}
		body.Reset()
	}
	for line := range strings.Lines(string(data)) {
		switch {
		case strings.HasPrefix(line, "=== "):
			flush()
			key = strings.TrimSpace(strings.TrimPrefix(line, "=== "))
		case strings.HasPrefix(line, "#") && key == "":
			// header
		default:
			body.WriteString(line)
		}
	}
	flush()
	return sections
}

// dumpEscape writes b like the C harness: bytes outside '!'..'~' and the
// backslash as \xHH.
func dumpEscape(w *strings.Builder, b []byte) {
	for _, c := range b {
		if c > ' ' && c <= '~' && c != '\\' {
			w.WriteByte(c)
		} else {
			fmt.Fprintf(w, `\x%02x`, c)
		}
	}
}

// upstreamCode maps an error to upstream's error code, as printed by the
// C harness.
func upstreamCode(err error) int {
	switch {
	case errors.Is(err, ErrItemExpected):
		return -16
	case errors.Is(err, ErrDuplicateItem):
		return -18
	case errors.Is(err, ErrWeightExpected):
		return -20
	case errors.Is(err, ErrInvalidWeight):
		return -21
	case errors.Is(err, ErrTooManyFields):
		return -22
	case errors.Is(err, ErrAppearanceExpected):
		return -23
	case errors.Is(err, ErrUnknownAppearance):
		return -24
	case errors.Is(err, ErrRead):
		return -3
	}
	return 0
}

func dumpError(w *strings.Builder, err error) {
	fmt.Fprintf(w, "ERR %d ", upstreamCode(err))
	dumpEscape(w, []byte(err.Error()))
	w.WriteByte('\n')
}

// dumpFields dumps the tokenizer's fields like "tabdump fields".
func dumpFields(src io.Reader, cfg Config) string {
	var w strings.Builder
	names := map[delim]string{delimErr: "ERR", delimEOF: "EOF", delimFld: "FLD", delimRec: "REC"}
	t := newTokenizer(src, cfg)
	for {
		d := t.read()
		fmt.Fprintf(&w, "%s %d %d ", names[d], t.rec, t.fld)
		dumpEscape(&w, t.field)
		w.WriteByte('\n')
		if d < 0 {
			return w.String()
		}
	}
}

// dumpTrans dumps the transactions of a Reader like "tabdump trans".
// Duplicates are dropped (first occurrence kept), as upstream's ib_read
// does; the Reader leaves that to the item base.
func dumpTrans(src io.Reader, name string, cfg Config) string {
	var w strings.Builder
	r := NewReader(src, name, cfg)
	for {
		items, weight, err := r.Next()
		if errors.Is(err, io.EOF) {
			w.WriteString("EOF\n")
			return w.String()
		}
		if err != nil {
			dumpError(&w, err)
			return w.String()
		}
		fmt.Fprintf(&w, "T %d %d", r.Record(), weight)
		seen := [][]byte{}
		for _, item := range items {
			if slices.ContainsFunc(seen, func(s []byte) bool { return bytes.Equal(s, item) }) {
				continue
			}
			seen = append(seen, item)
			w.WriteByte(' ')
			dumpEscape(&w, item)
		}
		w.WriteByte('\n')
	}
}

// dumpSelection dumps ReadSelection's result like "tabdump sel".
func dumpSelection(src io.Reader, name string, cfg Config) string {
	var w strings.Builder
	names, err := ReadSelection(src, name, cfg)
	if err != nil {
		dumpError(&w, err)
		return w.String()
	}
	for _, n := range names {
		w.WriteString("I ")
		dumpEscape(&w, []byte(n))
		w.WriteByte('\n')
	}
	w.WriteString("EOF\n")
	return w.String()
}

// dumpAppearances dumps ReadAppearances' result like "tabdump app".
func dumpAppearances(src io.Reader, name string, cfg Config) string {
	var w strings.Builder
	def, items, err := ReadAppearances(src, name, cfg)
	if err != nil {
		dumpError(&w, err)
		return w.String()
	}
	fmt.Fprintf(&w, "D %d\n", def)
	for _, it := range items {
		w.WriteString("A ")
		dumpEscape(&w, []byte(it.Name))
		fmt.Fprintf(&w, " %d\n", it.App)
	}
	w.WriteString("EOF\n")
	return w.String()
}

// dumpCase runs a golden case through the Go implementation in the given
// mode ("fields" or the case's mode).
func dumpCase(t *testing.T, c goldenCase, mode string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(c.file)))
	if err != nil {
		t.Fatal(err)
	}
	src := bufio.NewReaderSize(bytes.NewReader(data), 16) // small reads exercise buffer refills
	switch mode {
	case "fields":
		return dumpFields(src, c.cfg)
	case "trans":
		return dumpTrans(src, c.file, c.cfg)
	case "sel":
		return dumpSelection(src, c.file, c.cfg)
	case "app":
		return dumpAppearances(src, c.file, c.cfg)
	}
	t.Fatalf("unknown mode %q", mode)
	return ""
}

// goldenDeviations lists the golden sections where the Go implementation
// deliberately differs from upstream, with the expected Go output. Every
// entry must differ from the C output (checked), so the list cannot go
// stale. See the package documentation for the deviations.
func goldenDeviations() map[string]string {
	x1024 := strings.Repeat("x", 1024)
	return map[string]string{
		// T3: a final record ending in a field separator without newline is kept.
		"trailing-sep-eof trans":  "T 1 1 a b\nT 2 1 c d\nEOF\n",
		"trailing-sep-eof fields": "FLD 1 1 a\nREC 1 2 b\nFLD 2 1 c\nFLD 2 2 d\nREC 2 3 \nEOF 3 1 \n",
		// T3 for selection files: "a,b," at the end of input is like "a,b,\n".
		"sel-trailing-sep-eof sel":    "ERR -16 input/sel-trailing-sep-eof.txt:1(3):\\x20item\\x20expected\n",
		"sel-trailing-sep-eof fields": "FLD 1 1 a\nFLD 1 2 b\nREC 1 3 \nEOF 2 1 \n",
		// T3 for appearance files: same result, the tokenizer now reports REC.
		"app-trailing-sep-eof fields": "REC 1 1 in\nFLD 2 1 a\nREC 2 2 \nEOF 3 1 \n",

		// B10: correct record numbers when the input ends in trailing blanks.
		"trailing-sep-blank-eof trans":  "T 1 1 a b\nT 2 1 c d\nEOF\n",
		"trailing-sep-blank-eof fields": "FLD 1 1 a\nREC 1 2 b\nFLD 2 1 c\nFLD 2 2 d\nREC 2 3 \nEOF 3 1 \n",
		"trailing-blank-eof trans":      "T 1 1 a 1\nT 2 1 b 2\nT 3 1\nEOF\n",
		"trailing-blank-eof fields":     "FLD 1 1 a\nREC 1 2 1\nFLD 2 1 b\nREC 2 2 2\nREC 3 1 \nEOF 4 1 \n",
		"weights-trailing-blank-eof trans": "T 1 1 a\nT 2 2 b\n" +
			"ERR -20 input/trailing-blank-eof.txt:3(1):\\x20transaction\\x20weight\\x20expected\n",
		"weights-trailing-blank-eof fields": "FLD 1 1 a\nREC 1 2 1\nFLD 2 1 b\nREC 2 2 2\nREC 3 1 \nEOF 4 1 \n",
		"weights-trailing-blank-after-field-eof trans": "T 1 1 a\n" +
			"ERR -21 input/trailing-blank-after-field-eof.txt:2(2):\\x20invalid\\x20transaction\\x20weight\\x20x\n",
		"weights-trailing-blank-after-field-eof fields": "FLD 1 1 a\nREC 1 2 1\nFLD 2 1 b\nREC 2 2 x\nEOF 3 1 \n",
		"ex-test6pct trans": "T 1 1 a b c\nT 2 1 a d e\nT 3 1 b c d\nT 4 1 a b c d\nT 5 1 b c\n" +
			"T 6 1 a b d\nT 7 1 d e\nT 8 1 a b c d\nT 9 1 c d e\nT 10 1 a b c\nEOF\n",
		"ex-test6pct fields": "FLD 1 1 a\nFLD 1 2 b\nREC 1 3 c\nFLD 2 1 a\nFLD 2 2 d\nREC 2 3 e\n" +
			"FLD 3 1 b\nFLD 3 2 c\nREC 3 3 d\nFLD 4 1 a\nFLD 4 2 b\nFLD 4 3 c\nREC 4 4 d\n" +
			"FLD 5 1 b\nREC 5 2 c\nFLD 6 1 a\nFLD 6 2 b\nREC 6 3 d\nFLD 7 1 d\nREC 7 2 e\n" +
			"FLD 8 1 a\nFLD 8 2 b\nFLD 8 3 c\nREC 8 4 d\nFLD 9 1 c\nFLD 9 2 d\nREC 9 3 e\n" +
			"FLD 10 1 a\nFLD 10 2 b\nREC 10 3 c\nEOF 11 1 \n",

		// B14: no truncation of fields to 1024 bytes.
		"long-field trans":  "T 1 1 " + x1024 + "xxxxxxEND\nT 2 1 " + x1024 + "ZZZ\nEOF\n",
		"long-field fields": "REC 1 1 " + x1024 + "xxxxxxEND\nREC 2 1 " + x1024 + "ZZZ\nEOF 3 1 \n",

		// T6: 64-bit weights, no truncation to int.
		"weights-big trans": "T 1 4294967297 a\nT 2 2147483648 b\nEOF\n",
		// Negative weights are rejected (project policy).
		"weights-neg trans": "T 1 1 a\nERR -21 input/weights-neg.txt:2(2):\\x20invalid\\x20transaction\\x20weight" +
			"\\x20-2\\x20(must\\x20not\\x20be\\x20negative)\n",
		// Weights beyond int64 are invalid instead of LONG_MAX truncated to -1.
		"weights-overflow trans": "ERR -21 input/weights-overflow.txt:1(2):\\x20invalid\\x20transaction" +
			"\\x20weight\\x2099999999999999999999\n",
	}
}

func TestGolden(t *testing.T) {
	t.Parallel()
	golden := loadGolden(t)
	deviations := goldenDeviations()
	used := map[string]bool{}
	cases := loadCases(t)
	if len(cases) == 0 {
		t.Fatal("no golden cases")
	}
	for _, c := range cases {
		for _, mode := range []string{c.mode, "fields"} {
			key := c.name + " " + mode
			want, ok := golden[key]
			if !ok {
				t.Errorf("%s: no golden section; regenerate with reference/harness/tabread/gen.sh", key)
				continue
			}
			got := dumpCase(t, c, mode)
			if dev, ok := deviations[key]; ok {
				used[key] = true
				if dev == want {
					t.Errorf("%s: listed as a deviation, but upstream gives the same output", key)
				}
				want = dev
			}
			if got != want {
				t.Errorf("%s: Go output differs from the expected output\ngot:\n%s\nwant:\n%s", key, got, want)
			}
		}
	}
	for key := range deviations {
		if !used[key] {
			t.Errorf("deviation %q does not match any golden section", key)
		}
	}
}

// TestExampleCopies checks that the copies of upstream's example files in
// testdata/ex are identical to the vendored upstream files (skipped when
// the reference tree is not available, e.g. in a module download).
func TestExampleCopies(t *testing.T) {
	t.Parallel()
	upstream := filepath.Join("..", "..", "reference", "upstream", "fpgrowth-6.21", "fpgrowth", "ex")
	if _, err := os.Stat(upstream); err != nil {
		t.Skipf("upstream examples not available: %v", err)
	}
	copies, err := filepath.Glob(filepath.Join("testdata", "ex", "*"))
	if err != nil || len(copies) == 0 {
		t.Fatalf("no example copies found (%v)", err)
	}
	for _, c := range copies {
		got, err := os.ReadFile(c)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(upstream, filepath.Base(c)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the upstream file", c)
		}
	}
}
