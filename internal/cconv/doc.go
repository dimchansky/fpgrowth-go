// Package cconv emulates the C library's numeric conversions as upstream
// fpgrowth uses them: strtol (option values -m -n -p -q -l and the -w
// transaction weight), the implicit long -> int conversion that follows,
// and strtod (option values -s -S -c -d -F).
//
// The emulated platform is LP64 (64-bit long, as on the macOS arm64 and
// Linux x86-64 reference builds) in the "C" locale, which upstream never
// changes. All functions are byte-oriented and never interpret UTF-8.
//
// C → Go mapping:
//
//	strtol(s, &end, base)       Strtol(s, base) → value, end-s, ERANGE
//	(int)strtol(...)            LongToInt(v)    → truncated value, exact
//	strtod(s, &end)             Strtod(s)       → value, end-s
//
// The functions report the number of bytes consumed instead of an end
// pointer, so a caller continues parsing at s[n:] exactly like upstream
// continues at *end. A result with n == 0 means "no conversion".
//
// Golden results produced by the platform C library for a corpus of
// tricky strings are in testdata/ (harness: reference/harness/cconv).
package cconv
