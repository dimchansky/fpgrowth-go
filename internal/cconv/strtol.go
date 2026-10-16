package cconv

import "math"

// Strtol emulates C strtol(s, &end, base) for a 64-bit long in the "C"
// locale and returns the converted value, the number of bytes consumed
// (end - s) and whether the value was out of range (errno == ERANGE).
//
// Accepted syntax, as in ISO C99 7.20.1.4 and the glibc/Apple libc
// implementations:
//
//   - leading white space (space, \t, \n, \v, \f, \r) is skipped;
//   - an optional '+' or '-' sign;
//   - for base 0 or 16 an optional "0x"/"0X" prefix, recognized only when
//     a hexadecimal digit follows it (otherwise only the "0" is parsed and
//     n points at the 'x');
//   - base 0 selects base 16 after the prefix, base 8 for a leading '0'
//     and base 10 otherwise; there is no "0b" prefix (C23 adds one, the
//     C99 reference build has none);
//   - one or more digits of the base, letters a-z/A-Z standing for 10-35.
//
// If no digits can be parsed, Strtol returns (0, 0, false), regardless of
// skipped white space or a sign. On overflow all digits of the subject
// sequence are still consumed and the value saturates at math.MaxInt64 or
// math.MinInt64 with overflow == true, exactly like C.
//
// base must be 0 or between 2 and 36; other values panic (C would set
// errno to EINVAL; upstream never passes them).
func Strtol[T ~string | ~[]byte](s T, base int) (v int64, n int, overflow bool) {
	if base != 0 && (base < 2 || base > 36) {
		panic("cconv: Strtol: invalid base")
	}
	i := 0
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	hasHexPrefix := i+2 < len(s) && s[i] == '0' && s[i+1]|0x20 == 'x' && digitValue(s[i+2]) < 16
	switch {
	case (base == 0 || base == 16) && hasHexPrefix:
		i += 2
		base = 16
	case base == 0 && i < len(s) && s[i] == '0':
		base = 8
	case base == 0:
		base = 10
	}

	// Accumulate the magnitude as unsigned, like the C implementations,
	// so that the magnitude of math.MinInt64 is representable.
	limit := uint64(math.MaxInt64)
	if neg {
		limit++
	}
	b := uint64(base)
	cutoff, cutlim := limit/b, limit%b
	var acc uint64
	start := i
	for ; i < len(s); i++ {
		d := uint64(digitValue(s[i]))
		if d >= b {
			break
		}
		if overflow {
			continue // keep consuming digits, like C
		}
		if acc > cutoff || (acc == cutoff && d > cutlim) {
			overflow = true
			continue
		}
		acc = acc*b + d
	}
	switch {
	case i == start:
		return 0, 0, false
	case overflow && neg:
		return math.MinInt64, i, true
	case overflow:
		return math.MaxInt64, i, true
	case neg:
		// Two's complement negation; acc == 1<<63 yields math.MinInt64.
		return int64(-acc), i, false // intended wrap-around
	default:
		return int64(acc), i, false // acc <= math.MaxInt64 here
	}
}

// LongToInt emulates the C conversion (int)v from a 64-bit long to a
// 32-bit int that upstream applies to every strtol result (ITEM and SUPP
// are int): the value is reduced modulo 2^32 into the int32 range (two's
// complement truncation, which is what gcc and clang do; ISO C leaves it
// implementation-defined). exact reports whether v was representable, i.e.
// whether the conversion changed the value.
//
// Upstream silently uses the truncated value, so -m4294967298 means
// -m2 and a saturated LONG_MAX becomes -1. A caller that wants to reject
// such input instead (the recommended policy for the Go CLI, documented
// as a deviation there) checks the overflow result of Strtol and exact.
func LongToInt(v int64) (i int32, exact bool) {
	i = int32(v) // truncation is the emulated behavior
	return i, int64(i) == v
}

// isSpace reports whether c is white space for isspace() in the "C"
// locale.
func isSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// digitValue returns the value of c as a digit in bases up to 36, or 36
// if c is not a digit or an ASCII letter.
func digitValue(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'z':
		return int(c-'a') + 10
	case 'A' <= c && c <= 'Z':
		return int(c-'A') + 10
	}
	return 36
}
