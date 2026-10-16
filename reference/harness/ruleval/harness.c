/*----------------------------------------------------------------------
  File    : harness.c
  Contents: fixture generator for the Go port of ruleval.c/gamma.c/chi2.c
  Usage   : harness measures   (one line per (supp, body, head, base))
            harness specfun    (logGamma and chi2cdfQ samples)

  Links the pristine upstream math/src/ruleval.c, gamma.c and chi2.c
  (FP-Growth 6.21). Every measure is called through re_function(id),
  exactly as istree.c does. All values are printed with %.17g, which
  round-trips IEEE 754 doubles. See README.md for the build flags.
----------------------------------------------------------------------*/
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "ruleval.h"
#include "gamma.h"
#include "chi2.h"

/*----------------------------------------------------------------------
  Parameters of the tuple grid
----------------------------------------------------------------------*/
#define NSMALL   9      /* exhaustive grid: all tables with base <= NSMALL */
#define NRAND    500    /* uniformly random tables, base < 2^31 */
#define NFISHER  500    /* random tables with one small margin */
#define NINDEP   300    /* random tables close to independence */
#define NSYMM    200    /* tables with a margin of exactly base/2 */
#define FCAP     20000  /* max. tables per Fisher evaluation, else '-' */

/*----------------------------------------------------------------------
  Deterministic pseudo-random numbers (splitmix64)
----------------------------------------------------------------------*/
static unsigned long long state = 0x2545f4914f6cdd1dULL;

static unsigned long long next (void)
{
  unsigned long long z = (state += 0x9e3779b97f4a7c15ULL);
  z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9ULL;
  z = (z ^ (z >> 27)) * 0x94d049bb133111ebULL;
  return z ^ (z >> 31);
}

static long long uni (long long lo, long long hi)
{                               /* uniform integer in [lo, hi] */
  return lo +(long long)(next() % (unsigned long long)(hi -lo +1));
}

static long long logrand (int kmax)
{                               /* log-uniform integer in [1, 2^kmax-1] */
  int k = (int)uni(1, kmax);
  return uni(1LL << (k-1), (1LL << k) -1);
}

/*----------------------------------------------------------------------
  Tuple output
----------------------------------------------------------------------*/

static long long fishercost (long long b, long long h, long long n)
{                               /* number of tables a Fisher test sums */
  if ((h <= 0) || (h >= n) || (b <= 0) || (b >= n)) return 0;
  if (n -h -b < 0) { b = n -b; h = n -h; }
  return ((b < h) ? b : h) +1;
}

static long long emitted = 0;   /* number of tuples printed */

static void emit (long long s, long long b, long long h, long long n)
{                               /* evaluate all measures on one table */
  int    id;                    /* measure identifier */
  double v;                     /* measure value */

  if ((n < 1) || (n > INT_MAX)  /* print only valid 2x2 tables */
  ||  (b < 0) || (b > n) || (h < 0) || (h > n)
  ||  (s < 0) || (s > b) || (s > h) || (b +h -s > n))
    return;
  printf("%lld %lld %lld %lld", s, b, h, n);
  for (id = 0; id < RE_FNCNT; id++) {
    if ((id >= RE_FETPROB) && (id <= RE_FETSUPP)
    &&  (fishercost(b, h, n) > FCAP)) {
      printf(" -"); continue; }  /* skip too expensive Fisher tests */
    v = re_function(id)((SUPP)s, (SUPP)b, (SUPP)h, (SUPP)n);
    printf(" %.17g", v);
  }
  printf("\n");
  emitted++;
}

static void emitrand (long long b, long long h, long long n)
{                               /* emit with a random valid support */
  long long lo = b +h -n, hi = (b < h) ? b : h;
  if (lo < 0) lo = 0;
  emit(uni(lo, hi), b, h, n);
}

/*----------------------------------------------------------------------
  Measure grid
----------------------------------------------------------------------*/

static void measures (void)
{
  static const long long bases[] = { 2, 3, 170, 171, 172, 1000, 65536,
    1000003, 16777216, 123456789, 1073741824, 2147483647 };
  long long i, n, b, h, s, lo, hi, e, m;
  size_t    k;

  printf("# columns: supp body head base, then measures 0..%d"
         " (RE_NONE..RE_FETSUPP); '-' = not evaluated\n", RE_FNCNT-1);

  /* 1. all tables with a small base (includes every degenerate case) */
  for (n = 1; n <= NSMALL; n++)
    for (b = 0; b <= n; b++)
      for (h = 0; h <= n; h++) {
        lo = b +h -n; if (lo < 0) lo = 0;
        hi = (b < h) ? b : h;
        for (s = lo; s <= hi; s++) emit(s, b, h, n);
      }

  /* 2. degenerate and boundary tables with large bases */
  for (k = 0; k < sizeof(bases)/sizeof(*bases); k++) {
    n = bases[k];
    emit(0, 0, 0, n);   emit(0, 0, n, n);   emit(0, n, 0, n);
    emit(n, n, n, n);   emit(0, 0, 1, n);   emit(0, 1, 0, n);
    emit(1, 1, n, n);   emit(n-1, n-1, n, n); emit(1, n, 1, n);
    emit(n-1, n, n-1, n); emit(1, 1, 1, n); emit(0, 1, 1, n);
    emit(0, 1, n-1, n); emit(1, 1, n-1, n); emit(1, 2, 2, n);
    emit(n-2, n-1, n-1, n); emit(0, n/2, n-n/2, n);
    emit(n/2, n/2, n/2, n); emit(n/4, n/2, n/2, n);
    emit(n/3, n/2, n-1, n); emit(n/2, n/2, n-1, n);
  }

  /* 3. uniformly random tables */
  for (i = 0; i < NRAND; i++) {
    n = logrand(31); emitrand(uni(0, n), uni(0, n), n); }

  /* 4. random tables with one small margin (Fisher tests evaluated) */
  for (i = 0; i < NFISHER; i++) {
    n = logrand(21); if (n < 2) n = 2;
    m = uni(1, (n-1 < 2000) ? n-1 : 2000);
    b = (next() & 1) ? m : n -m;
    h = uni(1, n-1);
    if (next() & 1) { emitrand(b, h, n); continue; }
    e = (b *h) /n;              /* or a support near the expectation */
    lo = b +h -n; if (lo < 0) lo = 0; hi = (b < h) ? b : h;
    s = e +uni(-2, 2);
    emit((s < lo) ? lo : (s > hi) ? hi : s, b, h, n);
  }

  /* 5. random tables close to independence (chi^2, Yates, info ~ 0) */
  for (i = 0; i < NINDEP; i++) {
    n = logrand(31); if (n < 2) n = 2;
    b = uni(1, n-1); h = uni(1, n-1);
    e = (b *h) /n;              /* expected support (rounded down) */
    lo = b +h -n; if (lo < 0) lo = 0; hi = (b < h) ? b : h;
    s = e +uni(-1, 1);
    emit((s < lo) ? lo : (s > hi) ? hi : s, b, h, n);
  }

  /* 6. tables with a margin of exactly base/2 (tied Fisher tables) */
  for (i = 0; i < NSYMM; i++) {
    n = 2 *uni(2, 5000);
    m = uni(1, n-1);
    if (next() & 1) emitrand(m, n/2, n);
    else            emitrand(n/2, m, n);
  }

  /* 7. reproducers from docs/upstream/rules-eval.md */
  emit(2, 2, 7, 10);            /* B4: conviction with confidence 1 */
  emit(7, 7, 10, 10);           /* B5: certainty factor, head in all */
  emit(1, 2, 3, 6);             /* B7: negative information */
  emit(1, 2, 5, 7);             /* B6: tied table probabilities */
  emit(2, 4, 5, 10);            /* B10: Yates without clamp */
  emit(7969403, 8875339, 7982035, 8893601); /* B9: negative p-value */
  fprintf(stderr, "measures: %lld tuples\n", emitted);
}

/*----------------------------------------------------------------------
  Special functions
----------------------------------------------------------------------*/

static void specfun (void)
{
  static const double lgx[] = { 0.25, 0.75, 1.25, 2.2, 3.3, 7.7, 10.1,
    100.3, 170.25, 171.1, 1000.7, 12345.6, 1000000.3, 1000000000.5 };
  static const double qx[]  = { 1e-300, 1e-20, 1e-10, 1e-5, 30, 50, 100,
    200, 500, 700, 1000, 1400, 1450, 1500, 2000, 1e6 };
  int    i;                     /* loop variable */
  size_t k;                     /* loop variable */
  double x;                     /* function argument */

  printf("# lg x logGamma(x); q x chi2cdfQ(x, 1)\n");
  for (i = 1; i <= 300; i++)
    printf("lg %.17g %.17g\n", (double)i, logGamma((double)i));
  for (i = 0; i <= 300; i++) {
    x = i +0.5; printf("lg %.17g %.17g\n", x, logGamma(x)); }
  for (k = 0; k < sizeof(lgx)/sizeof(*lgx); k++)
    printf("lg %.17g %.17g\n", lgx[k], logGamma(lgx[k]));
  for (i = 0; i <= 1000; i++) {
    x = i *0.025; printf("q %.17g %.17g\n", x, chi2cdfQ(x, 1)); }
  for (k = 0; k < sizeof(qx)/sizeof(*qx); k++)
    printf("q %.17g %.17g\n", qx[k], chi2cdfQ(qx[k], 1));
}

/*--------------------------------------------------------------------*/

int main (int argc, char *argv[])
{
  if ((argc == 2) && (strcmp(argv[1], "measures") == 0)) measures();
  else if ((argc == 2) && (strcmp(argv[1], "specfun") == 0)) specfun();
  else { fprintf(stderr, "usage: %s measures|specfun\n", argv[0]); return 2; }
  return 0;
}
