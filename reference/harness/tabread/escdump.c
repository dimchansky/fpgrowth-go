/*
 * escdump - dump what upstream's escape decoding (util/src/escape.c) makes
 * of option strings such as the values of fpgrowth's -r -f -b -C. Produces
 * internal/tabread/testdata/golden/escapes.golden.
 *
 * Usage: escdump escapes.txt
 *
 * Every line of the input that starts with '>' is one test string: the
 * rest of the line, taken literally (no escapes at this level; the file
 * may contain any bytes except NUL and newline).
 *
 * Output per test string, TAB-separated (bytes outside '!'..'~' and the
 * backslash written as \xHH):
 *   1. the test string;
 *   2. "S=" esc_decstr() result, with the string followed by two NULs in
 *      memory (so a lone trailing backslash, which makes upstream read
 *      past the terminator, sees a second NUL and stops);
 *   3. "C=" the esc_decode() codes as trd_chars() uses them, as signed
 *      decimal ints separated by ',' (a negative code is an out-of-bounds
 *      index into trd_chars' flag table);
 *   4. "Z=" esc_decstr() result with "Z" after the terminator, which shows
 *      upstream reading past the end of the string.
 */
#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "escape.h"

static void put (const char *s, size_t n)
{
  size_t i;
  for (i = 0; i < n; i++) {
    unsigned char c = (unsigned char)s[i];
    if (c > ' ' && c <= '~' && c != '\\') putchar(c);
    else printf("\\x%02x", c);
  }
}

static void decstr (const char *src, size_t len, const char *pad)
{
  char *in = malloc(len + 4), *out = malloc(len + 4);
  size_t n;
  memcpy(in, src, len);
  memcpy(in + len, pad, 3);    /* terminator plus padding */
  n = esc_decstr(out, in);
  put(out, n);
  free(in); free(out);
}

int main (int argc, char *argv[])
{
  FILE *f; char *line = NULL; size_t cap = 0; ssize_t len;

  if (argc != 2) { fprintf(stderr, "usage: %s escapes.txt\n", argv[0]); return 2; }
  f = fopen(argv[1], "rb");
  if (!f) { perror(argv[1]); return 2; }
  while ((len = getline(&line, &cap, f)) >= 0) {
    char *s, *in; size_t n; int first = 1;
    if (len > 0 && line[len-1] == '\n') line[--len] = 0;
    if (len == 0 || line[0] != '>') continue;
    s = line + 1; n = (size_t)len - 1;
    put(s, n);
    printf("\tS=");
    decstr(s, n, "\0\0\0");
    printf("\tC=");
    in = malloc(n + 4);
    memcpy(in, s, n); memcpy(in + n, "\0\0\0", 3);
    for (s = in; *s; ) {        /* the loop of trd_chars() */
      printf(first ? "%d" : ",%d", esc_decode(s, &s));
      first = 0;
    }
    free(in);
    printf("\tZ=");
    decstr(line + 1, n, "\0Z\0");
    putchar('\n');
  }
  free(line); fclose(f);
  return 0;
}
