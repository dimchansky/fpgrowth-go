package cconv

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestStrtod(t *testing.T) {
	t.Parallel()
	negZero := math.Copysign(0, -1)
	tests := []struct {
		name  string
		input string
		value float64
		n     int
	}{
		{name: "empty", input: ""},
		{name: "only point", input: "."},
		{name: "only sign", input: "-"},
		{name: "exponent without significand", input: "e5"},
		{name: "integer", input: "42", value: 42, n: 2},
		{name: "white space and sign", input: " \t-1.5x", value: -1.5, n: 6},
		{name: "trailing point", input: "5.", value: 5, n: 2},
		{name: "leading point", input: ".25", value: 0.25, n: 3},
		{name: "exponent", input: "1e3", value: 1000, n: 3},
		{name: "signed exponent", input: "25E-1", value: 2.5, n: 5},
		{name: "exponent without digits", input: "1e", value: 1, n: 1},
		{name: "exponent sign without digits", input: "1e+", value: 1, n: 1},
		{name: "comma is no decimal point", input: "1,5", value: 1, n: 1},
		{name: "second point stops", input: "1.5.5", value: 1.5, n: 3},
		{name: "negative zero", input: "-0", value: negZero, n: 2},
		{name: "hex integer", input: "0x1E", value: 30, n: 4},
		{name: "hex with exponent", input: "0x1p4", value: 16, n: 5},
		{name: "hex fraction", input: "0x.8", value: 0.5, n: 4},
		{name: "hex exponent without digits", input: "0x1p", value: 1, n: 3},
		{name: "hex prefix without digits", input: "0x", value: 0, n: 1},
		{name: "hex prefix with point only", input: "0x.p1", value: 0, n: 1},
		{name: "decimal exponent letter in hex is a digit", input: "0x1e5", value: 0x1e5, n: 5},
		{name: "overflow", input: "1e400", value: math.Inf(1), n: 5},
		{name: "negative overflow", input: "-1e400", value: math.Inf(-1), n: 6},
		{name: "underflow", input: "1e-400", value: 0, n: 6},
		{name: "negative underflow", input: "-1e-400", value: negZero, n: 7},
		{name: "smallest subnormal", input: "5e-324", value: math.SmallestNonzeroFloat64, n: 6},
		{name: "max", input: "1.7976931348623157e308", value: math.MaxFloat64, n: 22},
		{name: "rounds to max", input: "1.7976931348623158e308", value: math.MaxFloat64, n: 22},
		{name: "rounds to inf", input: "1.7976931348623159e308", value: math.Inf(1), n: 22},
		{name: "inf", input: "inf", value: math.Inf(1), n: 3},
		{name: "infinity mixed case", input: "-InFiNiTy", value: math.Inf(-1), n: 9},
		{name: "incomplete infinity", input: "infinit", value: math.Inf(1), n: 3},
		{name: "huge exponent", input: "1e99999999999999999999", value: math.Inf(1), n: 22},
		{name: "zero with huge exponent", input: "0e99999999999999999999", value: 0, n: 22},
		{
			name:  "many leading zeros and matching exponent",
			input: "0." + strings.Repeat("0", 20000) + "1e20001",
			value: 1,
			n:     20009,
		},
		{
			name:  "many trailing zeros and matching exponent",
			input: "1" + strings.Repeat("0", 20000) + "e-20000",
			value: 1,
			n:     20008,
		},
		{
			name:  "hex many leading zeros and matching exponent",
			input: "0x0." + strings.Repeat("0", 20000) + "1p80004",
			value: 1,
			n:     20011,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			value, n := Strtod(tt.input)
			if math.Float64bits(value) != math.Float64bits(tt.value) || n != tt.n {
				t.Errorf("Strtod(%.40q) = (%v, %d), want (%v, %d)", tt.input, value, n, tt.value, tt.n)
			}
		})
	}
}

func TestStrtodNaN(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		n        int
		negative bool
	}{
		{name: "plain", input: "nan", n: 3},
		{name: "upper", input: "NAN", n: 3},
		{name: "negative", input: "-nan", n: 4, negative: true},
		{name: "empty sequence", input: "nan()", n: 5},
		{name: "sequence", input: "nan(a_Z_09)z", n: 11},
		{name: "unterminated", input: "nan(abc", n: 3},
		{name: "invalid character", input: "nan(a-b)", n: 3},
		{name: "blank inside", input: "nan( )", n: 3},
		{name: "incomplete", input: "na", n: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			value, n := Strtod(tt.input)
			if n != tt.n {
				t.Errorf("Strtod(%q) consumed %d bytes, want %d", tt.input, n, tt.n)
			}
			if tt.n == 0 {
				return
			}
			if !math.IsNaN(value) || math.Signbit(value) != tt.negative {
				t.Errorf("Strtod(%q) = %v (sign %t), want NaN (sign %t)", tt.input, value, math.Signbit(value), tt.negative)
			}
		})
	}
}

// libcSpecificNaN lists corpus inputs for which Apple libc and glibc
// disagree: Apple accepts any bytes up to ')' in "nan(...)". Strtod
// follows ISO C and glibc; the value is the number of bytes consumed.
var libcSpecificNaN = map[string]int{
	"nan(a-b)":  3,
	"nan( )":    3,
	"nan(()":    3,
	"nan(\x80)": 3,
	"nan(a\nb)": 3,
	"nan(-)x":   3,
	"nan(a(b)":  3,
}

func TestStrtodCorpus(t *testing.T) {
	t.Parallel()
	for _, c := range loadCorpus(t) {
		value, n := Strtod(c.input)
		want := math.Float64frombits(c.doubleBits)
		wantN := c.doubleN
		if glibcN, ok := libcSpecificNaN[c.input]; ok {
			wantN = glibcN
		}
		if n != wantN {
			t.Errorf("case %s: Strtod consumed %d bytes, C consumed %d", c.line, n, wantN)
		}
		if math.IsNaN(want) {
			// Payloads are not reproduced; NaN-ness and sign are.
			if !math.IsNaN(value) || math.Signbit(value) != math.Signbit(want) {
				t.Errorf("case %s: Strtod = %v, C gives NaN with sign %t", c.line, value, math.Signbit(want))
			}
			continue
		}
		if math.Float64bits(value) != c.doubleBits {
			t.Errorf("case %s: Strtod = %v (%016x), C gives %v (%016x)",
				c.line, value, math.Float64bits(value), want, c.doubleBits)
		}
	}
}

func FuzzStrtod(f *testing.F) {
	for _, s := range []string{
		"", "1", "-1.5e-3", ".5", "5.", "0x1p-1074", "0x.8", "inf", "-nan(x)",
		"1e400", "2.4703282292062328e-324", "1.7976931348623158e308", " \t+0X1.8P+1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		value, n := Strtod(s)
		if n < 0 || n > len(s) {
			t.Fatalf("Strtod(%q) consumed %d bytes", s, n)
		}
		if n == 0 {
			if math.Float64bits(value) != 0 {
				t.Fatalf("Strtod(%q) = %v without conversion", s, value)
			}
			return
		}
		// Re-parsing the consumed prefix gives the same result.
		v2, n2 := Strtod(s[:n])
		same := math.Float64bits(v2) == math.Float64bits(value) || (math.IsNaN(v2) && math.IsNaN(value))
		if n2 != n || !same {
			t.Fatalf("Strtod(%q) = (%v, %d) but its prefix gives (%v, %d)", s, value, n, v2, n2)
		}
		// Where Go's float syntax and C's coincide (no digit separators,
		// no NaN, hex only with an exponent), the whole string is
		// consumed and the value equals strconv.ParseFloat's.
		if strings.ContainsAny(s, "_ \t\n\v\f\r") || strings.Contains(strings.ToLower(s), "nan") {
			return
		}
		want, err := strconv.ParseFloat(s, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return // not Go syntax
		}
		if n != len(s) || math.Float64bits(value) != math.Float64bits(want) {
			t.Fatalf("Strtod(%q) = (%v, %d), ParseFloat gives %v for the whole string", s, value, n, want)
		}
	})
}
