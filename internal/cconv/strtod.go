package cconv

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Strtod emulates C strtod(s, &end) in the "C" locale with the default
// rounding mode (round to nearest, ties to even) and returns the
// converted value and the number of bytes consumed (end - s). n == 0
// means that no conversion could be performed; the value is then 0.
//
// Accepted syntax (ISO C99 7.20.1.3): leading white space (space, \t,
// \n, \v, \f, \r), an optional sign, then one of
//
//   - a decimal significand (digits with an optional '.', at least one
//     digit) with an optional exponent [eE][+-]digits;
//   - "0x"/"0X" followed by a hexadecimal significand (hex digits with an
//     optional '.', at least one hex digit) with an optional binary
//     exponent [pP][+-]digits; without a hex digit after the prefix only
//     the "0" is consumed;
//   - "inf" or "infinity", case-insensitive (the longer match wins);
//   - "nan", case-insensitive, optionally followed by "(" n-char-sequence
//     ")" where the sequence consists of ASCII letters, digits and '_'.
//
// An exponent is consumed only if at least one digit follows the
// exponent character and its optional sign. The decimal point is always
// '.', there are no digit separators.
//
// Results: overflow yields ±Inf and underflow a correctly rounded
// subnormal or ±0, as in C (errno is not reported; upstream never reads
// it). The sign of zero is kept. NaN results are quiet NaNs carrying the
// sign of the input; NaN payloads given as "nan(...)" are not reproduced
// (C libraries derive them differently, and NaN is only ever compared).
//
// Platform note: Apple's libc accepts any bytes up to the next ')' in
// "nan(...)", glibc and ISO C only letters, digits and '_'. Strtod
// follows ISO C and glibc; the corpus test lists the affected inputs.
//
// Correctness argument: the scanner above determines the consumed prefix
// independently of strconv. The value is then computed by
// strconv.ParseFloat on a canonical rewrite of that prefix,
// "[-]0.<significant digits>e<E>" or "[-]0x0.<significant hex
// digits>p<E>", whose exponent E is small because values whose leading
// digit lies far outside the double range are resolved to ±Inf or ±0
// directly (see decimalValue and hexValue). For such inputs both
// ParseFloat and the C libraries return the correctly rounded
// (round-half-even) double of the exact decimal or hexadecimal value, so
// the results are identical. The rewrite avoids ParseFloat's internal
// exponent saturation, which would otherwise mis-round inputs that pair
// thousands of leading zeros with a matching exponent (which both glibc
// and Apple libc handle exactly).
func Strtod(s string) (v float64, n int) {
	i := 0
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	if v, k, ok := special(s[i:], neg); ok {
		return v, i + k
	}
	if i+1 < len(s) && s[i] == '0' && s[i+1]|0x20 == 'x' {
		if v, k, ok := hexFloat(s[i+2:], neg); ok {
			return v, i + 2 + k
		}
		// No hex digit after the prefix: the subject sequence is "0".
	}
	if v, k, ok := decimalFloat(s[i:], neg); ok {
		return v, i + k
	}
	return 0, 0
}

// Value ranges outside of which the result is ±Inf or ±0 regardless of
// the remaining digits. A decimal value 0.D×10^E with a nonzero leading
// digit lies in [10^(E-1), 10^E): it overflows for E > 309 (10^309 >
// MaxFloat64 + ½ulp) and rounds to zero for E < -323 (10^-324 < 2^-1075,
// half the smallest subnormal). A hexadecimal value 0.H×2^E lies in
// [2^(E-4), 2^E): it overflows for E > 1028 and rounds to zero for
// E < -1075. The bounds below are looser; values in between are left to
// ParseFloat.
const (
	maxDecExp = 400
	minDecExp = -400
	maxBinExp = 1100
	minBinExp = -1200

	// expLimit caps the accumulated exponent long before int64 overflow;
	// any exponent this large is far outside the bounds above even after
	// adding the significand's digit offset (bounded by len(s)).
	expLimit = 1 << 40
)

// special parses "inf", "infinity" and "nan" with an optional
// n-char-sequence at the start of s.
func special(s string, neg bool) (v float64, n int, ok bool) {
	sign := 1.0
	if neg {
		sign = -1
	}
	switch {
	case hasPrefixFold(s, "infinity"):
		return math.Inf(int(sign)), len("infinity"), true
	case hasPrefixFold(s, "inf"):
		return math.Inf(int(sign)), len("inf"), true
	case hasPrefixFold(s, "nan"):
		n = len("nan")
		if n < len(s) && s[n] == '(' {
			j := n + 1
			for j < len(s) && isNChar(s[j]) {
				j++
			}
			if j < len(s) && s[j] == ')' {
				n = j + 1
			}
		}
		// The default quiet NaN of glibc and Apple libc (no payload).
		return math.Copysign(math.Float64frombits(0x7ff8000000000000), sign), n, true
	}
	return 0, 0, false
}

// hexFloat parses a hexadecimal significand and optional binary exponent
// at the start of s (after the "0x" prefix).
func hexFloat(s string, neg bool) (v float64, n int, ok bool) {
	intPart, fracPart, n := significand(s, isHexDigit)
	if n == 0 {
		return 0, 0, false
	}
	exp, k := exponent(s[n:], 'p')
	n += k

	digits, offset, nonzero := leadingDigits(intPart, fracPart)
	if !nonzero {
		return signedZero(neg), n, true
	}
	// value = 0.digits (hex) × 16^offset × 2^exp
	e := 4*int64(offset) + exp
	switch {
	case e > maxBinExp:
		return math.Inf(signOf(neg)), n, true
	case e < minBinExp:
		return signedZero(neg), n, true
	}
	return parseCanonical(neg, "0x0.", digits, "p", e), n, true
}

// decimalFloat parses a decimal significand and optional exponent at the
// start of s.
func decimalFloat(s string, neg bool) (v float64, n int, ok bool) {
	intPart, fracPart, n := significand(s, isDecDigit)
	if n == 0 {
		return 0, 0, false
	}
	exp, k := exponent(s[n:], 'e')
	n += k

	digits, offset, nonzero := leadingDigits(intPart, fracPart)
	if !nonzero {
		return signedZero(neg), n, true
	}
	// value = 0.digits × 10^offset × 10^exp
	e := int64(offset) + exp
	switch {
	case e > maxDecExp:
		return math.Inf(signOf(neg)), n, true
	case e < minDecExp:
		return signedZero(neg), n, true
	}
	return parseCanonical(neg, "0.", digits, "e", e), n, true
}

// significand scans digits with an optional '.' at the start of s. It
// returns the digits before and after the point and the number of bytes
// consumed, which is 0 if there is no digit at all.
func significand(s string, isDigit func(byte) bool) (intPart, fracPart string, n int) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	intPart = s[:i]
	if i < len(s) && s[i] == '.' {
		j := i + 1
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		fracPart = s[i+1 : j]
		i = j
	}
	if intPart == "" && fracPart == "" {
		return "", "", 0
	}
	return intPart, fracPart, i
}

// exponent scans an exponent introduced by the letter mark (either case)
// at the start of s. It returns the exponent value, saturated at
// ±expLimit, and the number of bytes consumed, which is 0 unless at least
// one digit follows the mark and its optional sign.
func exponent(s string, mark byte) (exp int64, n int) {
	if len(s) == 0 || s[0]|0x20 != mark {
		return 0, 0
	}
	i := 1
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	for i < len(s) && isDecDigit(s[i]) {
		if exp < expLimit {
			exp = exp*10 + int64(s[i]-'0')
		}
		i++
	}
	if i == start {
		return 0, 0
	}
	if neg {
		exp = -exp
	}
	return exp, i
}

// leadingDigits returns the digits of intPart+fracPart starting at the
// first nonzero one and the position of the radix point relative to that
// digit: the value is 0.digits × base^offset. nonzero is false if all
// digits are zero.
func leadingDigits(intPart, fracPart string) (digits string, offset int, nonzero bool) {
	if k := strings.IndexFunc(intPart, isNonzeroDigit); k >= 0 {
		return intPart[k:] + fracPart, len(intPart) - k, true
	}
	if k := strings.IndexFunc(fracPart, isNonzeroDigit); k >= 0 {
		return fracPart[k:], -k, true
	}
	return "", 0, false
}

// parseCanonical converts "[-]<prefix><digits><mark><exp>" with
// strconv.ParseFloat. The syntax is valid by construction and the
// exponent is small; ParseFloat reports ErrRange only for overflow, in
// which case it returns ±Inf like C.
func parseCanonical(neg bool, prefix, digits string, mark string, exp int64) float64 {
	var b strings.Builder
	b.Grow(len(prefix) + len(digits) + 24)
	if neg {
		b.WriteByte('-')
	}
	b.WriteString(prefix)
	b.WriteString(digits)
	b.WriteString(mark)
	b.WriteString(strconv.FormatInt(exp, 10))
	v, err := strconv.ParseFloat(b.String(), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		panic("cconv: Strtod: internal error: " + err.Error())
	}
	return v
}

func signOf(neg bool) int {
	if neg {
		return -1
	}
	return 1
}

func signedZero(neg bool) float64 {
	if neg {
		return math.Copysign(0, -1)
	}
	return 0
}

// hasPrefixFold reports whether s starts with the lower-case ASCII word
// w, ignoring ASCII case.
func hasPrefixFold(s, w string) bool {
	if len(s) < len(w) {
		return false
	}
	for i := range len(w) {
		if s[i]|0x20 != w[i] {
			return false
		}
	}
	return true
}

func isDecDigit(c byte) bool { return '0' <= c && c <= '9' }

func isHexDigit(c byte) bool { return digitValue(c) < 16 }

// isNonzeroDigit reports whether r is a nonzero decimal or hexadecimal
// digit; it is only applied to strings that consist of such digits.
func isNonzeroDigit(r rune) bool { return r != '0' }

// isNChar reports whether c may appear in the n-char-sequence of
// "nan(...)": an ASCII letter, digit or underscore.
func isNChar(c byte) bool { return c == '_' || digitValue(c) < 36 }
