package isrout

import (
	"math"
	"strconv"
)

// pows is upstream's table of powers of ten (report.c pows[]):
// pows[i] = 10^(i-2).
var pows = [36]float64{
	1e-02, 1e-01,
	1e+00, 1e+01, 1e+02, 1e+03, 1e+04, 1e+05, 1e+06, 1e+07,
	1e+08, 1e+09, 1e+10, 1e+11, 1e+12, 1e+13, 1e+14, 1e+15,
	1e+16, 1e+17, 1e+18, 1e+19, 1e+20, 1e+21, 1e+22, 1e+23,
	1e+24, 1e+25, 1e+26, 1e+27, 1e+28, 1e+29, 1e+30, 1e+31,
	1e+32, 1e+33,
}

// defaultDigits is the number of significant digits of a format code
// without explicit digits, maxDigits the cap applied by getsd.
const (
	defaultDigits = 6
	maxDigits     = 32
)

// bisect is a port of dbl_bisect (arrays.c BISECT): the index of the last
// element equal to key, or else the insertion position of key in the
// ascending array a.
func bisect(key float64, a []float64) int {
	l, r := 0, len(a)
	for l < r {
		m := (l + r) / 2
		t := a[m]
		switch {
		case key > t:
			l = m + 1
		case key < t:
			r = m
		default:
			for m++; m < r; m++ { // skip equal elements
				if key < a[m] {
					break
				}
			}
			return m - 1
		}
	}
	return l
}

// parseDigits is a port of getsd: up to two decimal digits at the start
// of s give the number of significant digits (default 6, at most 32). It
// returns the digits and the number of bytes consumed.
func parseDigits(s string) (digits, n int) {
	k := defaultDigits
	if n < len(s) && isDigit(s[n]) {
		k = int(s[n] - '0')
		n++
		if n < len(s) && isDigit(s[n]) {
			k = 10*k + int(s[n]-'0')
			n++
		}
	}
	return min(k, maxDigits), n
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

// appendMant is a port of mantout: it appends the non-negative number num
// with digits significant digits to dst, rounding with upstream's rule.
// If the integral part needs more than ints digits, it appends nothing
// and returns the negated number of integral digits.
//
// The digit generation, the tie rule and the carry handling are those of
// report.c operation by operation (report.md §5.6, B7, B8): decimals are
// produced by repeated multiplication with 10, an exact remaining 0.5
// rounds to an odd last decimal, and without decimals an even integral
// part is rounded up (half to odd). This is intentionally preserved
// (docs/differences.md O8).
func appendMant(dst []byte, num float64, digits, ints int) ([]byte, int) {
	i := bisect(num, pows[:])
	if i >= len(pows) || pows[i] == num {
		i++
	}
	n := digits - (i - 2) // number of decimals
	x := math.Floor(num)
	y := num - x
	var buf [96]byte
	const d = 40 // position of the decimal point
	e := d
	if n > 0 {
		buf[e] = '.'
		e++
		for ; n > 0; n-- {
			// The conversion prevents fusing the product into the
			// subtraction below (FMA): C computes them separately.
			y = float64(y * 10)
			buf[e] = byte(int(y) + '0')
			e++
			y -= math.Floor(y)
		}
	}
	roundUp := y > 0.5
	if y == 0.5 {
		if e > d {
			roundUp = buf[e-1]&1 != 0
		} else {
			roundUp = math.Floor(x/2) >= x/2
		}
	}
	if roundUp {
		s := e
		for {
			s--
			if s <= d {
				break
			}
			if buf[s] < '9' {
				buf[s]++
				break
			}
			buf[s] = '0'
		}
		if s <= d {
			x++
			if x >= pows[i] { // carry into a new integral digit:
				e-- // drop one decimal
				if e <= d+1 {
					e = d
				}
			}
		}
	}
	if e > d { // remove trailing zeros and a bare decimal point
		for {
			e--
			if buf[e] != '0' {
				break
			}
		}
		if e > d {
			e++
		}
	}
	s := d
	for {
		s--
		buf[s] = byte(int(math.Mod(x, 10)) + '0')
		x = math.Floor(x / 10)
		if !(x > 0) {
			break
		}
	}
	if k := d - s; k > ints {
		return dst, -k
	}
	return append(dst, buf[s:e]...), e - s
}

// appendNum is a port of isr_numout: it appends num with digits
// significant digits in upstream's format to dst. This is not printf's
// %g: values below 0.001 or at least 10^digits use the exponential form
// ("e+06", "e-04"), and the rounding is that of appendMant. More than 11
// digits are formatted like C's "%.*g" (strconv's 'g' format, which
// produces the same bytes); digits above 32 count as 32.
func appendNum(dst []byte, num float64, digits int) []byte {
	if math.IsNaN(num) {
		return append(dst, "nan"...)
	}
	if num < 0 {
		num = -num
		dst = append(dst, '-')
	}
	if math.IsInf(num, 0) {
		return append(dst, "inf"...)
	}
	if num < 0x1p-1022 { // DBL_MIN: zero and subnormal numbers
		return append(dst, '0')
	}
	digits = max(min(digits, maxDigits), 0)
	if digits > 11 {
		return strconv.AppendFloat(dst, num, 'g', digits, 64)
	}
	e := 0
	if num >= pows[digits+2] || num < 0.001 {
		for num < 1e00 {
			num = float64(num * 1e32)
			e -= 32
		}
		for num >= 1e32 {
			num /= 1e32
			e += 32
		}
		k := bisect(num, pows[2:])
		if k >= len(pows)-2 || pows[k+2] != num {
			k--
		}
		e += k
		num /= pows[k+2]
	}
	ints := 1
	if e == 0 {
		ints = digits
	}
	out, k := appendMant(dst, num, digits, ints)
	if k < 0 {
		num /= pows[1-k]
		e += -1 - k
		out, _ = appendMant(dst, num, digits, 1)
	}
	if e == 0 {
		return out
	}
	out = append(out, 'e')
	if e < 0 {
		out = append(out, '-')
		e = -e
	} else {
		out = append(out, '+')
	}
	if e < 10 {
		out = append(out, '0')
	}
	return strconv.AppendInt(out, int64(e), 10)
}
