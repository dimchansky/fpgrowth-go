/*----------------------------------------------------------------------
  File    : qsortdump.c
  Contents: sort pseudo-random item arrays with upstream's ptr_qsort and
            the comparators of ib_recode (fixture generator for the tie
            order of internal/tract)
  Usage   : qsortdump seed count
  Output  : one line per array: "q=<order> <keys> | <ids>", where keys
            are the frequencies in identifier order (x = APP_NONE) and
            ids the identifiers in sorted order; order is the -q value
            (1: descmp, -1: asccmp, 0: nocmp)
----------------------------------------------------------------------*/
#include <stdio.h>
#include <stdlib.h>
#include "arrays.h"

typedef struct {                /* --- the fields ib_recode compares */
  int id;                       /* item identifier */
  int none;                     /* whether app == APP_NONE */
  int frq;                      /* item frequency */
} ITEM;

/* The comparators below are copies of nocmp, asccmp and descmp
   (tract/src/tract.c) for this record type. */

static int nocmp (const void *p1, const void *p2, void *data)
{
  const ITEM *a = (const ITEM*)p1, *b = (const ITEM*)p2;
  (void)data;
  if (a->none) return (b->none) ? 0 : 1;
  if (b->none) return -1;
  if (a->id  >  b->id)  return +1;
  if (a->id  <  b->id)  return -1;
  return 0;
}

static int asccmp (const void *p1, const void *p2, void *data)
{
  const ITEM *a = (const ITEM*)p1, *b = (const ITEM*)p2;
  (void)data;
  if (a->none) return (b->none) ? 0 : 1;
  if (b->none) return -1;
  if (a->frq >  b->frq) return +1;
  if (a->frq <  b->frq) return -1;
  return 0;
}

static int descmp (const void *p1, const void *p2, void *data)
{
  const ITEM *a = (const ITEM*)p1, *b = (const ITEM*)p2;
  (void)data;
  if (a->none) return (b->none) ? 0 : 1;
  if (b->none) return -1;
  if (a->frq <  b->frq) return +1;
  if (a->frq >  b->frq) return -1;
  return 0;
}

static unsigned long long state;

static int rnd (int n)
{                               /* --- random number in [0, n) */
  state = state *6364136223846793005ULL +1442695040888963407ULL;
  return (int)((state >> 33) % (unsigned)n);
}  /* rnd() */

int main (int argc, char *argv[])
{                               /* --- main function */
  static ITEM items[1024];      /* item records */
  static void *ptrs[1024];      /* array to sort */
  int  k, i, n, keys, nonepct, order, count;
  CMPFN *cmp;

  if (argc != 3) { fprintf(stderr, "usage: qsortdump seed count\n"); return 2; }
  state = strtoull(argv[1], NULL, 10);
  count = atoi(argv[2]);
  for (k = 0; k < count; k++) {
    switch (k % 4) {            /* size: mostly small, some large */
      case 0:  n = 1 +rnd(8);    break;
      case 1:  n = 1 +rnd(40);   break;
      case 2:  n = 1 +rnd(200);  break;
      default: n = 1 +rnd(600);  break;
    }
    keys    = 1 +rnd((k % 3 == 0) ? 2 : (k % 3 == 1) ? 5 : 50);
    nonepct = (k % 5 == 0) ? 0 : rnd(60);
    order   = (k % 7 == 0) ? 0 : (k % 2) ? 1 : -1;
    cmp     = (order == 0) ? nocmp : (order > 0) ? descmp : asccmp;
    for (i = 0; i < n; i++) {
      items[i].id   = i;
      items[i].none = rnd(100) < nonepct;
      items[i].frq  = rnd(keys);
      ptrs[i] = items +i;
    }
    printf("q=%d", order);
    for (i = 0; i < n; i++) {
      if (items[i].none) printf(" x");
      else               printf(" %d", items[i].frq);
    }
    ptr_qsort(ptrs, (size_t)n, +1, cmp, NULL);
    printf(" |");
    for (i = 0; i < n; i++) printf(" %d", ((ITEM*)ptrs[i])->id);
    printf("\n");
  }
  return 0;
}  /* main() */
