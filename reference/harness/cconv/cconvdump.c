/*
 * cconvdump - print what the C library's strtol() and strtod() make of
 * a corpus of strings. Used to produce internal/cconv/testdata/expected.txt,
 * the golden file for the Go emulation in internal/cconv.
 *
 * Usage: cconvdump corpus.txt > expected.txt
 *
 * Corpus format (one case per line):
 *   - lines starting with '#' are comments, empty lines are ignored;
 *   - every other line must start with '>'; the rest of the line is the
 *     test string, with three escapes: "\\" is a backslash, "\xHH" is the
 *     byte with hex value HH, and "\R<count>:<HH>" is the byte HH repeated
 *     <count> (decimal) times.
 *
 * Output: for every case one line
 *   <case line as read> TAB l0=V,N,R l10=... l16=... l8=... l2=... l36=...
 *   i=I d=BITS,N,R
 * where lB is strtol(s, &e, B) with value V, N = e - s consumed bytes and
 * R = 1 if errno == ERANGE; i is (int)strtol(s, &e, 0) (the long -> int
 * conversion upstream performs); d is strtod(s, &e) with BITS the IEEE 754
 * bit pattern as 16 hex digits.
 *
 * The program deliberately does not call setlocale(), so the "C" locale
 * is in effect, as in the upstream fpgrowth program.
 */
#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <stdint.h>

static int hexval (int c)
{
  if (c >= '0' && c <= '9') return c - '0';
  if (c >= 'a' && c <= 'f') return c - 'a' + 10;
  if (c >= 'A' && c <= 'F') return c - 'A' + 10;
  return -1;
}

/* decode a case line (without the leading '>') into buf; returns length */
static size_t decode (const char *s, size_t len, char **buf, size_t *cap,
                      long lineno)
{
  size_t n = 0, i = 0;
  if (*cap < 2) {
    *cap = 64;
    *buf = realloc(*buf, *cap);
    if (!*buf) { fprintf(stderr, "out of memory\n"); exit(2); }
  }
  while (i < len) {
    size_t need = 1, rep = 1;
    int c = (unsigned char)s[i];
    if (c == '\\') {
      if (i + 1 < len && s[i+1] == '\\') { c = '\\'; i += 2; }
      else if (i + 3 < len && s[i+1] == 'x'
           && hexval(s[i+2]) >= 0 && hexval(s[i+3]) >= 0) {
        c = hexval(s[i+2]) * 16 + hexval(s[i+3]); i += 4; }
      else if (i + 1 < len && s[i+1] == 'R') {
        char *e; rep = strtoul(s + i + 2, &e, 10);
        if (*e != ':' || hexval(e[1]) < 0 || hexval(e[2]) < 0) {
          fprintf(stderr, "line %ld: bad \\R escape\n", lineno); exit(2); }
        c = hexval(e[1]) * 16 + hexval(e[2]);
        i = (size_t)(e + 3 - s); }
      else { fprintf(stderr, "line %ld: bad escape\n", lineno); exit(2); }
    } else i++;
    need = rep;
    if (n + need + 2 > *cap) {
      *cap = (n + need + 2) * 2;
      *buf = realloc(*buf, *cap);
      if (!*buf) { fprintf(stderr, "out of memory\n"); exit(2); }
    }
    memset(*buf + n, c, rep); n += rep;
  }
  (*buf)[n] = 0;
  return n;
}

static void dumpl (const char *s, int base, const char *sep)
{
  char *e; long v;
  errno = 0; v = strtol(s, &e, base);
  printf("%sl%d=%ld,%td,%d", sep, base, v, e - s, errno == ERANGE);
}

int main (int argc, char *argv[])
{
  FILE *f; char *line = NULL, *buf = NULL; size_t lcap = 0, bcap = 0;
  ssize_t len; long lineno = 0;
  static const int bases[] = { 0, 10, 16, 8, 2, 36 };

  if (argc != 2) { fprintf(stderr, "usage: %s corpus\n", argv[0]); return 2; }
  f = fopen(argv[1], "rb");
  if (!f) { perror(argv[1]); return 2; }
  while ((len = getline(&line, &lcap, f)) >= 0) {
    size_t i; char *e; long lv; double d; uint64_t bits;
    lineno++;
    if (len > 0 && line[len-1] == '\n') line[--len] = 0;
    if (len == 0 || line[0] == '#') continue;
    if (line[0] != '>') {
      fprintf(stderr, "line %ld: expected '>'\n", lineno); return 2; }
    decode(line + 1, (size_t)len - 1, &buf, &bcap, lineno);
    printf("%s\t", line);
    for (i = 0; i < sizeof(bases)/sizeof(*bases); i++)
      dumpl(buf, bases[i], (i > 0) ? " " : "");
    lv = strtol(buf, &e, 0);
    printf(" i=%d", (int)lv);
    errno = 0; d = strtod(buf, &e);
    memcpy(&bits, &d, sizeof(bits));
    printf(" d=%016llx,%td,%d\n", (unsigned long long)bits, e - buf,
           errno == ERANGE);
  }
  free(line); free(buf); fclose(f);
  return 0;
}
