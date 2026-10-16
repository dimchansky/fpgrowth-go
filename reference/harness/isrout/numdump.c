/*----------------------------------------------------------------------
  numdump.c -- dump upstream isr_numout() results for internal/isrout

  Links against the pristine report.c compiled like repcm.o (fpgrowth's
  reporter object, -DISR_PATSPEC -DISR_CLOMAX) and calls isr_numout()
  directly on a hand-made ISREPORT whose output buffer is read back after
  every call (the 64 KiB buffer is never flushed for a single number).

  Usage:
    numdump corpus            deterministic corpus (committed fixture)
    numdump random SEED N     N random cases (large out-of-tree checks)
    numdump gfmt              C "%g" of pattern spectrum counts

  Output of corpus/random: one line per case
    <IEEE-754 bits, 16 hex digits> <digits> <isr_numout output>
  Output of gfmt: one line per count
    <count as unsigned decimal> <printf("%g", (double)count)>
----------------------------------------------------------------------*/
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <math.h>
#include <float.h>
#include "report.h"

#define BS (64*1024)

static ISREPORT *rep;           /* reporter with an output buffer only */

/* --- splitmix64 pseudo-random numbers (deterministic) --- */
static uint64_t state;
static uint64_t next64 (void)
{
  uint64_t z = (state += 0x9e3779b97f4a7c15ULL);
  z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9ULL;
  z = (z ^ (z >> 27)) * 0x94d049bb133111ebULL;
  return z ^ (z >> 31);
}
static uint64_t below (uint64_t n) { return next64() % n; }
static double unif (void) { return (double)(next64() >> 11) / 9007199254740992.0; }

static uint64_t bits (double x)
{ uint64_t u; memcpy(&u, &x, sizeof(u)); return u; }
static double frombits (uint64_t u)
{ double x; memcpy(&x, &u, sizeof(x)); return x; }

static void emit (double x, int digits)
{                               /* --- print one case */
  rep->next = rep->buf;         /* empty the reporter's buffer */
  isr_numout(rep, x, digits);   /* format into the buffer */
  printf("%016llx %d %.*s\n", (unsigned long long)bits(x), digits,
         (int)(rep->next -rep->buf), rep->buf);
}

static int sigdigits (double x)
{                               /* --- significant decimal digits */
  char buf[128], *s;            /* of the exact decimal expansion */
  int  n = 0, lead = 1, z = 0;
  snprintf(buf, sizeof(buf), "%.60f", fabs(x));
  for (s = buf; *s; s++) {      /* count digits from the first */
    if (*s == '.') continue;    /* non-zero digit to the last one */
    if (lead && *s == '0') continue;
    lead = 0; n++;
    if (*s == '0') z++; else z = 0;
  }
  return n - z;
}

static int rnddigits (void)
{                               /* --- random digits, biased to 0..11 */
  uint64_t r = below(10);
  if (r < 3) return 6;          /* the default precision */
  if (r < 9) return (int)below(12);
  return (int)below(34);        /* includes the printf branch */
}

/* --- case families --- */

static void specials (void)
{
  static const double v[] = { 0.0, -0.0, 1.0, -1.0, 0.5, -0.5, 2.5, 3.5,
    9.5, 12.5, 37.5, 0.35, 0.1125, 1.088125, 999999.5, 123456.5,
    0.0005, 0.001, 75, 0.7, 0.3, 1e-320, -1e-320, DBL_MIN, -DBL_MIN,
    DBL_MAX, -DBL_MAX, 5e-324, 1e32, 1e33, 1e34, 1e-32, 1e-33,
    100.0, 1e6, 1e11, 1e12, 99.99999, 9.9999995, 0.99999995,
    0.0009999995, 70, 30, 85.71428571428571, 33.333333333333336,
    0.6666666666666666, 1.0/3, 2.0/3, 100.0/3, 200.0/3 };
  int i, d;
  for (i = 0; i < (int)(sizeof(v)/sizeof(v[0])); i++)
    for (d = 0; d <= 33; d++) emit(v[i], d);
  for (d = 0; d <= 33; d++) {
    emit(NAN, d); emit(-NAN, d); emit(INFINITY, d); emit(-INFINITY, d);
    emit(nextafter(DBL_MIN, 0), d);
    emit(nextafter(0.001, 0), d); emit(nextafter(0.001, 1), d);
  }
  emit(1.5, 40); emit(1.0/3, 99);
}

static void powers (void)
{                               /* 10^p and its neighbours */
  int p, j; double x;
  for (p = -323; p <= 308; p++) {
    x = pow(10, p);             /* (libm; any nearby value will do) */
    emit(x, (int)below(14));
    emit(nextafter(x, 0), (int)below(14));
    emit(nextafter(x, INFINITY), (int)below(14));
  }
  for (p = -6; p <= 14; p++)    /* exact powers in all small digits */
    for (j = 0; j <= 12; j++) emit(pow(10, p), j);
}

static void boundaries (void)
{                               /* rounding boundaries 9...95 */
  int d, p; double x, m;
  for (d = 0; d <= 11; d++) {
    m = 1 - 5 * pow(10, -(d+1));
    for (p = -6; p <= 13; p++) {
      x = m * pow(10, p);
      emit(x, d); emit(nextafter(x, 0), d); emit(nextafter(x, 1e300), d);
    }
  }
}

static void dyadic_ties (int count)
{                               /* exact decimal ties m*10^q/2^j */
  int i, j, q, L, d; uint64_t m; double x;
  for (i = 0; i < count; ) {
    j = 1 + (int)below(12); q = (int)below(7);
    m = 2*below(1u << 20) + 1;
    x = ldexp((double)m * pow(10, q), -j);
    if ((double)m * pow(10, q) >= 9007199254740992.0) continue;
    if (below(4) == 0) x = ldexp((double)m, -j - (int)below(20));
    L = sigdigits(x);           /* exact tie at L-1 digits */
    d = (below(10) < 7) ? L-1 : (int)below(12);
    if (d < 0) d = 0;
    emit(x, d); i++;
  }
}

static void ratios (int count)
{                               /* the quantities the CLI prints */
  int i, d; uint64_t a, b; double s;
  for (i = 0; i < count; i++) {
    b = (below(4) == 0) ? 1 + below(2000000) : 1 + below(5000);
    a = below(b+1);
    s = (double)a / (double)b;
    d = (below(2) == 0) ? 6 : (int)below(12);
    emit(s, d);                 /* %s, %c */
    emit(100*s, d);             /* %S, %C */
  }
}

static void halves (int count)
{                               /* n + 0.5 at integer precision */
  int i, d, L; uint64_t n; double x;
  for (i = 0; i < count; i++) {
    n = below(4) ? below(2000000) : below(100000000000ULL);
    x = (double)n + 0.5;
    L = sigdigits((double)n);
    d = (below(4) != 0) ? L : (int)below(12);
    emit(x, d);
  }
}

static void loguniform (int count)
{
  int i; double x;
  for (i = 0; i < count; i++) {
    x = pow(10, -12 + 28 * unif());
    if (below(8) == 0) x = -x;
    emit(x, rnddigits());
  }
}

static void rawbits (int count)
{
  int i;
  for (i = 0; i < count; i++)
    emit(frombits(next64()), (int)below(34));
}

static void gfmt (void)
{                               /* C %g of (double)count, psp_report */
  static const uint64_t v[] = { 0, 1, 2, 9, 10, 99, 100, 12345, 99999,
    100000, 123456, 999999, 1000000, 1000001, 1234565, 1234575,
    5200300, 9999994, 9999995, 9999996, 10000000, 123456789,
    4294967295ULL, 4294967296ULL, 9007199254740993ULL,
    9223372036854775807ULL, 18446744073709551615ULL };
  int i;
  for (i = 0; i < (int)(sizeof(v)/sizeof(v[0])); i++)
    printf("%llu %g\n", (unsigned long long)v[i], (double)v[i]);
  state = 7;
  for (i = 0; i < 300; i++) {
    uint64_t n = next64() >> (next64() % 64);
    printf("%llu %g\n", (unsigned long long)n, (double)n);
  }
}

int main (int argc, char *argv[])
{
  rep = (ISREPORT*)calloc(1, sizeof(ISREPORT));
  if (!rep) return 1;
  rep->buf  = (char*)malloc(BS);
  if (!rep->buf) return 1;
  rep->next = rep->buf; rep->end = rep->buf +BS;
  rep->file = stdout;           /* never flushed: read back directly */
  if ((argc == 2) && (strcmp(argv[1], "corpus") == 0)) {
    state = 20221122;           /* FP-Growth 6.21 release date */
    specials(); powers(); boundaries();
    dyadic_ties(3000); ratios(3000); halves(1500);
    loguniform(2500); rawbits(1000);
  }
  else if ((argc == 4) && (strcmp(argv[1], "random") == 0)) {
    long n = atol(argv[3]), i;
    state = strtoull(argv[2], NULL, 0);
    for (i = 0; i < n; i += 4) {
      dyadic_ties(1); ratios(1); halves(1); loguniform(1);
    }
  }
  else if ((argc == 2) && (strcmp(argv[1], "gfmt") == 0))
    gfmt();
  else {
    fprintf(stderr, "usage: numdump corpus | random SEED N | gfmt\n");
    return 2;
  }
  return (fflush(stdout) != 0) ? 1 : 0;
}
