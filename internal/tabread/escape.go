package tabread

import "strings"

// DecodeEscapes decodes the escape sequences of an option value, as
// upstream does for the -r, -f, -b and -C values (escape.c esc_decode,
// applied byte by byte by tabread.c trd_chars; esc_decstr for whole
// strings). Upstream does not decode -k, -I, -h, -v or file names; the
// caller decides where to apply it.
//
// Sequences:
//
//	\a \b \f \n \r \t \v    the C control characters
//	\o \oo \ooo             octal byte value, 1 to 3 digits, modulo 256
//	\xh \xhh                hexadecimal byte value, 1 or 2 digits
//	\x                      'x' if no hexadecimal digit follows
//	\c                      c, for any other byte c (e.g. "\\" is '\')
//	\                       '\' at the end of the string
//
// All other bytes, including bytes >= 0x80, stand for themselves; "\0"
// yields a NUL byte, which is a valid class member.
//
// Deviation from upstream: an unknown escape \c decodes to c and a lone
// trailing backslash to a backslash. Upstream's default branch returns
// the byte after c (so "\,;" means ";" and "\," at the end means "\"),
// and for a trailing backslash it reads past the end of the string
// (cli-io.md B3). Upstream also passes bytes >= 0x80 as negative values
// (B4); here they are ordinary bytes, also after a backslash.
func DecodeEscapes(s string) string {
	if strings.IndexByte(s, '\\') < 0 {
		return s
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c, n := decodeEscape(s[i:])
		b = append(b, c)
		i += n
	}
	return string(b)
}

// decodeEscape decodes the (possibly escaped) byte at the start of the
// non-empty string s and returns it with the number of bytes consumed.
// Port of esc_decode.
func decodeEscape(s string) (c byte, n int) {
	if s[0] != '\\' {
		return s[0], 1
	}
	if len(s) == 1 {
		// Deviation from upstream: a trailing backslash stands for itself
		// instead of reading past the end of the string.
		return '\\', 1
	}
	switch c := s[1]; c {
	case 'a':
		return '\a', 2
	case 'b':
		return '\b', 2
	case 'f':
		return '\f', 2
	case 'n':
		return '\n', 2
	case 'r':
		return '\r', 2
	case 't':
		return '\t', 2
	case 'v':
		return '\v', 2
	case '0', '1', '2', '3', '4', '5', '6', '7':
		x := int(c - '0')
		n = 2
		for n < 4 && n < len(s) && '0' <= s[n] && s[n] <= '7' {
			x = x<<3 | int(s[n]-'0')
			n++
		}
		return byte(x), n // three octal digits may exceed 0xff; upstream masks with 0xff
	case 'x':
		hi, ok := hexDigit(s, 2)
		if !ok {
			return 'x', 2
		}
		if lo, ok := hexDigit(s, 3); ok {
			return hi<<4 | lo, 4
		}
		return hi, 3
	default:
		// Deviation from upstream: an unknown escape stands for the
		// escaped byte (upstream returns the byte after it).
		return c, 2
	}
}

// hexDigit returns the value of the hexadecimal digit s[i], if there is
// one.
func hexDigit(s string, i int) (byte, bool) {
	if i >= len(s) {
		return 0, false
	}
	switch c := s[i]; {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
