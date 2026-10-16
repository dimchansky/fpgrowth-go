package cconv

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestStrtol(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		base     int
		value    int64
		n        int
		overflow bool
	}{
		{name: "empty", input: "", base: 0},
		{name: "only blanks", input: " \t", base: 0},
		{name: "only sign", input: "-", base: 10},
		{name: "c white space", input: " \t\n\v\f\r42", base: 0, value: 42, n: 8},
		{name: "nbsp is not space", input: "\xa042", base: 0},
		{name: "plus", input: "+7x", base: 0, value: 7, n: 2},
		{name: "minus", input: "-7", base: 10, value: -7, n: 2},
		{name: "double sign", input: "+-1", base: 0},
		{name: "blank after sign", input: "- 1", base: 0},
		{name: "partial", input: "12ab", base: 10, value: 12, n: 2},
		{name: "octal", input: "010", base: 0, value: 8, n: 3},
		{name: "octal stops at 8", input: "08", base: 0, value: 0, n: 1},
		{name: "decimal keeps leading zero", input: "010", base: 10, value: 10, n: 3},
		{name: "hex", input: "0x1f", base: 0, value: 31, n: 4},
		{name: "hex upper prefix", input: "0X1F", base: 16, value: 31, n: 4},
		{name: "hex without prefix", input: "ff", base: 16, value: 255, n: 2},
		{name: "prefix without digit", input: "0xg", base: 0, value: 0, n: 1},
		{name: "prefix at end", input: "0x", base: 16, value: 0, n: 1},
		{name: "negative hex", input: " -0x10", base: 0, value: -16, n: 6},
		{name: "prefix not in base 10", input: "0x10", base: 10, value: 0, n: 1},
		{name: "no binary prefix", input: "0b101", base: 0, value: 0, n: 1},
		{name: "binary", input: "1012", base: 2, value: 5, n: 3},
		{name: "base 36", input: "Zz9!", base: 36, value: 35*36*36 + 35*36 + 9, n: 3},
		{name: "nul stops", input: "1\x002", base: 0, value: 1, n: 1},
		{name: "max", input: "9223372036854775807", base: 0, value: math.MaxInt64, n: 19},
		{name: "max plus one", input: "9223372036854775808", base: 0, value: math.MaxInt64, n: 19, overflow: true},
		{name: "min", input: "-9223372036854775808", base: 0, value: math.MinInt64, n: 20},
		{name: "min minus one", input: "-9223372036854775809", base: 0, value: math.MinInt64, n: 20, overflow: true},
		{name: "overflow consumes all digits", input: "99999999999999999999x", base: 10, value: math.MaxInt64, n: 20, overflow: true},
		{name: "hex min", input: "-0x8000000000000000", base: 0, value: math.MinInt64, n: 19},
		{name: "hex overflow", input: "0xffffffffffffffff", base: 0, value: math.MaxInt64, n: 18, overflow: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			value, n, overflow := Strtol(tt.input, tt.base)
			if value != tt.value || n != tt.n || overflow != tt.overflow {
				t.Errorf("Strtol(%q, %d) = (%d, %d, %t), want (%d, %d, %t)",
					tt.input, tt.base, value, n, overflow, tt.value, tt.n, tt.overflow)
			}
			bv, bn, bo := Strtol([]byte(tt.input), tt.base)
			if bv != value || bn != n || bo != overflow {
				t.Errorf("Strtol([]byte(%q), %d) = (%d, %d, %t), differs from the string result",
					tt.input, tt.base, bv, bn, bo)
			}
		})
	}
}

func TestStrtolInvalidBasePanics(t *testing.T) {
	t.Parallel()
	for _, base := range []int{-1, 1, 37} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Strtol(\"1\", %d) did not panic", base)
				}
			}()
			Strtol("1", base)
		}()
	}
}

func TestStrtolCorpus(t *testing.T) {
	t.Parallel()
	for _, c := range loadCorpus(t) {
		for base, want := range c.long {
			value, n, overflow := Strtol(c.input, base)
			if value != want.value || n != want.n || overflow != want.overflow {
				t.Errorf("case %s: Strtol(base %d) = (%d, %d, %t), C gives (%d, %d, %t)",
					c.line, base, value, n, overflow, want.value, want.n, want.overflow)
			}
		}
		value, _, _ := Strtol(c.input, 0)
		if got, _ := LongToInt(value); got != c.intValue {
			t.Errorf("case %s: LongToInt = %d, C (int)strtol gives %d", c.line, got, c.intValue)
		}
	}
}

func TestLongToInt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input int64
		value int32
		exact bool
	}{
		{name: "zero", input: 0, value: 0, exact: true},
		{name: "int32 max", input: math.MaxInt32, value: math.MaxInt32, exact: true},
		{name: "int32 min", input: math.MinInt32, value: math.MinInt32, exact: true},
		{name: "int32 max plus one wraps", input: math.MaxInt32 + 1, value: math.MinInt32},
		{name: "two to the 32 plus two", input: 1<<32 + 2, value: 2},
		{name: "long max", input: math.MaxInt64, value: -1},
		{name: "long min", input: math.MinInt64, value: 0},
		{name: "minus two to the 32 minus one", input: -(1 << 32) - 1, value: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			value, exact := LongToInt(tt.input)
			if value != tt.value || exact != tt.exact {
				t.Errorf("LongToInt(%d) = (%d, %t), want (%d, %t)", tt.input, value, exact, tt.value, tt.exact)
			}
		})
	}
}

func FuzzStrtol(f *testing.F) {
	for _, s := range []string{"", "0", "-0x1f", "010", "08", "  +42z", "9223372036854775808", "0xg", "zz"} {
		f.Add(s, 0)
		f.Add(s, 10)
		f.Add(s, 36)
	}
	f.Fuzz(func(t *testing.T, s string, base int) {
		if base < 0 {
			base = -base
		}
		base %= 37
		if base == 1 {
			base = 0
		}
		value, n, overflow := Strtol(s, base)
		if n < 0 || n > len(s) {
			t.Fatalf("Strtol(%q, %d) consumed %d bytes", s, base, n)
		}
		if n == 0 {
			if value != 0 || overflow {
				t.Fatalf("Strtol(%q, %d) = (%d, 0, %t) without conversion", s, base, value, overflow)
			}
			return
		}
		// Re-parsing the consumed prefix gives the same result.
		v2, n2, o2 := Strtol(s[:n], base)
		if v2 != value || n2 != n || o2 != overflow {
			t.Fatalf("Strtol(%q, %d) = (%d, %d, %t) but its prefix gives (%d, %d, %t)",
				s, base, value, n, overflow, v2, n2, o2)
		}
		// The subject sequence agrees with strconv.ParseInt.
		subject := strings.TrimLeft(s[:n], " \t\n\v\f\r")
		effBase := base
		sign, digits := "", subject
		if digits[0] == '+' || digits[0] == '-' {
			sign, digits = digits[:1], digits[1:]
		}
		if (base == 0 || base == 16) && len(digits) > 2 && digits[0] == '0' && digits[1]|0x20 == 'x' {
			digits, effBase = digits[2:], 16
		} else if base == 0 {
			effBase = 10
			if digits[0] == '0' {
				effBase = 8
			}
		}
		want, err := strconv.ParseInt(sign+digits, effBase, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			t.Fatalf("Strtol(%q, %d) consumed %q, which ParseInt rejects: %v", s, base, s[:n], err)
		}
		if want != value || errors.Is(err, strconv.ErrRange) != overflow {
			t.Fatalf("Strtol(%q, %d) = (%d, %t), ParseInt gives (%d, %v)", s, base, value, overflow, want, err)
		}
	})
}
