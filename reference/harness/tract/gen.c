/*----------------------------------------------------------------------
  File    : gen.c
  Contents: deterministic random transaction databases in fpgrowth's
            default input format (fixture inputs for internal/tract)
  Usage   : gen seed items records maxlen skew dup empty wmin wmax
                pool noise
              seed     seed of the 64-bit LCG
              items    number of distinct items (named i0, i1, ...)
              records  number of records
              maxlen   maximum number of item slots per record
              skew     0: uniform item choice, 1: biased to small indices
              dup      percentage of item slots that repeat an item
              empty    percentage of empty records
              wmin     minimum weight; wmax < 0: no weight field
              wmax     maximum weight
              pool     > 0: records are drawn from this many patterns
              noise    1: vary separators/blanks, add comments and CRLF
  The output only depends on the arguments.
----------------------------------------------------------------------*/
#include <stdio.h>
#include <stdlib.h>

static unsigned long long state;

static unsigned rnd (unsigned n)
{                               /* --- random number in [0, n) */
  state = state *6364136223846793005ULL +1442695040888963407ULL;
  return (unsigned)((state >> 33) % n);
}  /* rnd() */

typedef struct {                /* --- a record pattern --- */
  int len;                      /* number of item slots */
  int items[64];                /* item indices */
} PATTERN;

static int  nitems, maxlen, skew, dup, empty;

static void make (PATTERN *p)
{                               /* --- draw a record pattern */
  int i, a, b;                  /* loop variable, item candidates */

  p->len = ((int)rnd(100) < empty) ? 0 : 1 +(int)rnd((unsigned)maxlen);
  for (i = 0; i < p->len; i++) {
    if (i > 0 && (int)rnd(100) < dup) {
      p->items[i] = p->items[rnd((unsigned)i)]; continue; }
    a = (int)rnd((unsigned)nitems);
    if (skew) {                 /* minimum of two draws */
      b = (int)rnd((unsigned)nitems); if (b < a) a = b; }
    p->items[i] = a;            /* (a repeated item is a duplicate) */
  }
}  /* make() */

int main (int argc, char *argv[])
{                               /* --- main function */
  int     r, i, nrec, wmin, wmax, pool, noise;
  PATTERN pats[256], cur, *p;   /* record patterns */
  static const char *seps[] = { " ", "\t", ",", " , ", "  ", "\t, " };

  if (argc != 12) {
    fprintf(stderr, "usage: gen seed items records maxlen skew dup "
                    "empty wmin wmax pool noise\n");
    return 2;
  }
  state  = strtoull(argv[1], NULL, 10);
  nitems = atoi(argv[2]);  nrec  = atoi(argv[3]);
  maxlen = atoi(argv[4]);  skew  = atoi(argv[5]);
  dup    = atoi(argv[6]);  empty = atoi(argv[7]);
  wmin   = atoi(argv[8]);  wmax  = atoi(argv[9]);
  pool   = atoi(argv[10]); noise = atoi(argv[11]);
  if (nitems < 1 || maxlen < 1 || maxlen > 64 || pool > 256) {
    fprintf(stderr, "gen: invalid arguments\n"); return 2; }
  for (i = 0; i < pool; i++) make(pats+i);
  for (r = 0; r < nrec; r++) {
    if (noise && rnd(10) == 0)  /* comment record */
      printf("# comment %d\n", r);
    if (pool > 0) p = pats +rnd((unsigned)pool);
    else        { make(&cur); p = &cur; }
    if (noise && rnd(5) == 0) printf(" ");
    for (i = 0; i < p->len; i++) {
      if (i > 0) printf("%s", noise ? seps[rnd(6)] : " ");
      printf("i%d", p->items[i]);
    }
    if (wmax >= 0)              /* transaction weight */
      printf("%s%d", (p->len > 0) ? " " : "",
             wmin +(int)rnd((unsigned)(wmax -wmin +1)));
    if (noise && rnd(5) == 0) printf(" ");
    printf((noise && rnd(4) == 0) ? "\r\n" : "\n");
  }
  return 0;
}  /* main() */
