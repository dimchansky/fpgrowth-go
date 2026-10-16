package tabread

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestDecodeEscapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty", input: "", expected: ""},
		{name: "plain", input: "abc", expected: "abc"},
		{name: "control characters", input: `\a\b\f\n\r\t\v`, expected: "\a\b\f\n\r\t\v"},
		{name: "octal one digit", input: `\0`, expected: "\x00"},
		{name: "octal three digits", input: `\123`, expected: "S"},
		{name: "octal stops after three digits", input: `\1234`, expected: "S4"},
		{name: "octal stops at non octal digit", input: `\18`, expected: "\x018"},
		{name: "octal modulo 256", input: `\400`, expected: "\x00"},
		{name: "octal max", input: `\777`, expected: "\xff"},
		{name: "hex one digit", input: `\x4`, expected: "\x04"},
		{name: "hex two digits", input: `\x41`, expected: "A"},
		{name: "hex stops after two digits", input: `\x414`, expected: "A4"},
		{name: "hex mixed case", input: `\xaF`, expected: "\xaf"},
		{name: "hex without digit is x", input: `\xg`, expected: "xg"},
		{name: "hex at end is x", input: `\x`, expected: "x"},
		{name: "upper X is no hex escape", input: `\X41`, expected: "X41"},
		{name: "escaped backslash", input: `\\`, expected: `\`},
		{name: "escaped backslash then n", input: `\\n`, expected: `\n`},
		// Deviations from upstream (B3): unknown escapes stand for the byte.
		{name: "escaped comma", input: `\,`, expected: ","},
		{name: "escaped comma then semicolon", input: `\,;`, expected: ",;"},
		{name: "escaped comma inside", input: `a\,b`, expected: "a,b"},
		{name: "escaped eight", input: `\8`, expected: "8"},
		{name: "escaped space", input: `\ `, expected: " "},
		{name: "lone trailing backslash", input: `\`, expected: `\`},
		{name: "trailing backslash after text", input: `a\`, expected: `a\`},
		{name: "escaped backslash then lone backslash", input: `\\\`, expected: `\\`},
		// Deviations from upstream (B4): bytes >= 0x80 are plain bytes.
		{name: "utf-8 bytes", input: "\xc3\xa9", expected: "\xc3\xa9"},
		{name: "escaped utf-8 bytes", input: "\\\xc3\xa9", expected: "\xc3\xa9"},
		{name: "hex escaped utf-8 bytes", input: `\xc3\xa9`, expected: "\xc3\xa9"},
		{name: "default field separators", input: ` \t,`, expected: " \t,"},
		{name: "octal and hex separators", input: `\054\x2e\073:`, expected: ",.;:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DecodeEscapes(tt.input); got != tt.expected {
				t.Errorf("DecodeEscapes(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// escapeDeviations lists the escapes.golden inputs whose decoding
// deliberately differs from upstream's esc_decstr (B3, B4), with the Go
// result.
var escapeDeviations = map[string]string{
	`\8`:         "8",
	`\9x`:        "9x",
	`\X41`:       "X41",
	`\\n`:        `\n`,
	`\,`:         ",",
	`\,;`:        ",;",
	`a\,b`:       "a,b",
	`\q`:         "q",
	`\N`:         "N",
	`\%`:         "%",
	`\ `:         " ",
	`\"`:         `"`,
	`\'`:         "'",
	`\?`:         "?",
	`\\\`:        `\\`,
	"\\\xc3\xa9": "\xc3\xa9",
}

// readsPastEnd lists inputs ending in a lone backslash: upstream reads the
// byte after the string terminator (column Z of escapes.golden shows it);
// Go decodes the backslash as itself.
var readsPastEnd = map[string]string{
	`\`:   `\`,
	`a\`:  `a\`,
	`\t\`: "\t\\",
}

func TestDecodeEscapesGolden(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/escapes.golden")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	used := map[string]bool{}
	sc := bufio.NewScanner(f)
	count := 0
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 4 {
			t.Fatalf("malformed golden line %q", line)
		}
		count++
		input := unescapeDump(t, cols[0])
		upstream := unescapeDump(t, strings.TrimPrefix(cols[1], "S="))
		pastEnd := unescapeDump(t, strings.TrimPrefix(cols[3], "Z="))
		got := DecodeEscapes(input)

		want := upstream
		if dev, ok := escapeDeviations[input]; ok {
			used[input] = true
			if dev == upstream {
				t.Errorf("%q: listed as a deviation, but upstream decodes it the same", input)
			}
			want = dev
		}
		if dev, ok := readsPastEnd[input]; ok {
			used[input] = true
			if !strings.HasSuffix(pastEnd, "Z") {
				t.Errorf("%q: upstream was expected to read past the end, got %q", input, pastEnd)
			}
			want = dev
		}
		if got != want {
			t.Errorf("DecodeEscapes(%q) = %q, want %q (upstream %q)", input, got, want, upstream)
		}

		// trd_chars uses the esc_decode codes as indices; negative codes
		// (bytes >= 0x80 with a signed char) never become class members
		// upstream (B4). Go adds every decoded byte to the class.
		if _, dev := escapeDeviations[input]; dev {
			continue
		}
		if _, dev := readsPastEnd[input]; dev {
			continue
		}
		codes := strings.TrimPrefix(cols[2], "C=")
		var bytesFromCodes []byte
		for c := range strings.SplitSeq(codes, ",") {
			if c == "" {
				continue
			}
			v, err := strconv.Atoi(c)
			if err != nil {
				t.Fatalf("bad code %q in %q", c, line)
			}
			bytesFromCodes = append(bytesFromCodes, byte(v))
		}
		if string(bytesFromCodes) != got {
			t.Errorf("%q: upstream class codes %s are not the bytes %q", input, codes, got)
		}
		table := newClassTable(Config{FieldSeps: got})
		for i := range len(got) {
			if table[got[i]]&classFldSep == 0 {
				t.Errorf("%q: byte %#x is not a field separator", input, got[i])
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("no golden lines")
	}
	for input := range escapeDeviations {
		if !used[input] {
			t.Errorf("deviation %q is not in the golden file", input)
		}
	}
	for input := range readsPastEnd {
		if !used[input] {
			t.Errorf("read-past-end case %q is not in the golden file", input)
		}
	}
}

// unescapeDump reverses dumpEscape (\xHH sequences).
func unescapeDump(t *testing.T, s string) string {
	t.Helper()
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+4 > len(s) || s[i+1] != 'x' {
			t.Fatalf("bad escape in %q", s)
		}
		v, err := strconv.ParseUint(s[i+2:i+4], 16, 8)
		if err != nil {
			t.Fatalf("bad escape in %q: %v", s, err)
		}
		b.WriteByte(byte(v))
		i += 3
	}
	return b.String()
}
