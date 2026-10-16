package isrout

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// numCase is one line of a numdump output: a value (as IEEE-754 bits), a
// number of significant digits and upstream's isr_numout result.
type numCase struct {
	line     int
	bits     uint64
	digits   int
	expected string
}

func readNumCases(t *testing.T, path string) []numCase {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var cases []numCase
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("%s:%d: malformed line %q", path, n, line)
		}
		bits, err1 := strconv.ParseUint(fields[0], 16, 64)
		digits, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			t.Fatalf("%s:%d: malformed line %q", path, n, line)
		}
		cases = append(cases, numCase{line: n, bits: bits, digits: digits, expected: fields[2]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}

func checkNumCases(t *testing.T, path string, cases []numCase) {
	t.Helper()
	fails := 0
	var buf []byte
	for _, c := range cases {
		x := math.Float64frombits(c.bits)
		buf = appendNum(buf[:0], x, c.digits)
		if string(buf) != c.expected {
			fails++
			if fails <= 20 {
				t.Errorf("%s:%d: appendNum(%v [%016x], %d) = %q, want %q",
					path, c.line, x, c.bits, c.digits, buf, c.expected)
			}
		}
	}
	if fails > 0 {
		t.Errorf("%d of %d cases differ", fails, len(cases))
	}
}

// TestAppendNumCorpus requires bit-exact equality with upstream's
// isr_numout on the committed corpus (reference/harness/isrout).
func TestAppendNumCorpus(t *testing.T) {
	const path = "testdata/numout.txt"
	cases := readNumCases(t, path)
	if len(cases) < 10000 {
		t.Fatalf("corpus has only %d cases", len(cases))
	}
	checkNumCases(t, path, cases)
}

// TestAppendNumExtra checks an additional, uncommitted numdump output
// (numdump random SEED N > file), e.g. millions of cases:
//
//	ISROUT_NUMOUT_EXTRA=/path/to/file go test ./internal/isrout -run Extra
func TestAppendNumExtra(t *testing.T) {
	path := os.Getenv("ISROUT_NUMOUT_EXTRA")
	if path == "" {
		t.Skip("ISROUT_NUMOUT_EXTRA not set")
	}
	cases := readNumCases(t, path)
	checkNumCases(t, path, cases)
	t.Logf("%d cases", len(cases))
}

// TestAppendNumKnown pins the differences from printf measured in the
// upstream audit (report.md §5.6) and a few basic forms.
func TestAppendNumKnown(t *testing.T) {
	tests := []struct {
		name     string
		x        float64
		digits   int
		expected string
	}{
		{"threshold 0.001", 0.0005, 6, "5e-04"},
		{"integer tie 2.5 rounds up", 2.5, 1, "3"},
		{"integer tie 3.5 stays", 3.5, 1, "3"},
		{"integer tie 9.5 stays", 9.5, 1, "9"},
		{"integer tie 12.5", 12.5, 2, "13"},
		{"integer tie 37.5", 37.5, 2, "37"},
		{"integer tie 999999.5", 999999.5, 6, "999999"},
		{"integer tie 123456.5", 123456.5, 6, "123457"},
		{"digit generation 0.35", 0.35, 1, "0.4"},
		{"digit generation 0.1125", 0.1125, 3, "0.112"},
		{"digit generation 1.088125", 100 * (1741.0 / 160000), 6, "1.08812"},
		{"negative zero", math.Copysign(0, -1), 6, "0"},
		{"subnormal", 1e-320, 6, "0"},
		{"negative subnormal", -1e-320, 6, "-0"},
		{"zero digits 75", 75, 0, "7e+01"},
		{"zero digits 0.7", 0.7, 0, "1"},
		{"zero digits 0.3", 0.3, 0, "0"},
		{"nan", math.NaN(), 6, "nan"},
		{"negative nan", math.Copysign(math.NaN(), -1), 6, "nan"},
		{"inf", math.Inf(1), 6, "inf"},
		{"negative inf", math.Inf(-1), 6, "-inf"},
		{"percentage", 70, 6, "70"},
		{"one digit 70", 70, 1, "7e+01"},
		{"fraction 1/3", 1.0 / 3, 10, "0.3333333333"},
		{"printf branch", 100.0 / 3, 12, "33.3333333333"},
		{"capped digits", 0.7, 99, "0.69999999999999995559107901499374"},
		{"large exponent", 1.5e100, 6, "1.5e+100"},
		{"small exponent", 2.5e-7, 6, "2.5e-07"},
		{"carry to power", 9.9999995, 6, "10"},
		{"carry to exponent", 999999.7, 6, "1e+06"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(appendNum(nil, tt.x, tt.digits))
			if got != tt.expected {
				t.Errorf("appendNum(%v, %d) = %q, want %q", tt.x, tt.digits, got, tt.expected)
			}
		})
	}
}

func TestParseDigits(t *testing.T) {
	tests := []struct {
		input          string
		expectedDigits int
		expectedN      int
	}{
		{"", 6, 0},
		{"S", 6, 0},
		{"0S", 0, 1},
		{"5", 5, 1},
		{"12S", 12, 2},
		{"123S", 12, 2},
		{"99", 32, 2},
		{"33x", 32, 2},
		{".2S", 6, 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			digits, n := parseDigits(tt.input)
			if digits != tt.expectedDigits || n != tt.expectedN {
				t.Errorf("parseDigits(%q) = %d, %d, want %d, %d",
					tt.input, digits, n, tt.expectedDigits, tt.expectedN)
			}
		})
	}
}

func TestBisect(t *testing.T) {
	a := []float64{1, 2, 2, 2, 5}
	tests := []struct {
		key      float64
		expected int
	}{
		{0, 0}, {1, 0}, {1.5, 1}, {2, 3}, {3, 4}, {5, 4}, {6, 5},
	}
	for _, tt := range tests {
		if got := bisect(tt.key, a); got != tt.expected {
			t.Errorf("bisect(%v) = %d, want %d", tt.key, got, tt.expected)
		}
	}
}

// TestAppendGCorpus checks the %g formatting of pattern spectrum counts
// against C's printf (testdata/gfmt.txt, reference/harness/isrout).
func TestAppendGCorpus(t *testing.T) {
	const path = "testdata/gfmt.txt"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for i, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		count, expected, ok := strings.Cut(line, " ")
		v, err := strconv.ParseUint(count, 10, 64)
		if !ok || err != nil {
			t.Fatalf("%s:%d: malformed line %q", path, i+1, line)
		}
		if got := string(appendG(nil, float64(v))); got != expected {
			t.Errorf("%s:%d: appendG(%d) = %q, want %q", path, i+1, v, got, expected)
		}
		n++
	}
	if n < 300 {
		t.Fatalf("only %d cases", n)
	}
}

func BenchmarkAppendNum(b *testing.B) {
	buf := make([]byte, 0, 64)
	for b.Loop() {
		buf = appendNum(buf[:0], 100*(1741.0/160000), 6)
	}
}
