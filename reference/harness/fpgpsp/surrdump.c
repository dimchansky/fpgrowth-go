/*----------------------------------------------------------------------
  File    : surrdump.c
  Contents: dump the surrogate data sets of one fpgpsp chain (the
            single-threaded loop of fpg_genpsp in fpgpsp.c, with the
            oracle patches of reference/patches/fpgpsp) for the Go port
            (internal/psp golden files)
  Usage   : surrdump method seed cnt zmin supp target algo flags file
            method  i, r, p or s (fpgpsp -g#)
            seed    unsigned seed of rng_create (fpgpsp: seed+n)
            cnt     number of surrogate data sets
            zmin    minimum item set size (-m#)
            supp    minimum support (-s#, negative: absolute)
            target  s, c, m or g (-t#)
            algo    s, c, d or t (-A#)
            flags   any of x (-x), i (-i), u (-u), 0 (-l0), or -
  Output  : "prep <n>" and n lines "t <item codes>" for the prepared
            transactions, "item <code> <name>" for every item, then for
            every surrogate "set <k>" and its transactions in slot
            order, as returned by the generator (before fpg_data sorts
            them for mining).
----------------------------------------------------------------------*/
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "tabread.h"
#include "tract.h"
#include "fpgrowth.h"

static void dump (TABAG *bag)
{                               /* --- print the transactions */
  TID   k;
  const ITEM *s;
  for (k = 0; k < tbg_cnt(bag); k++) {
    printf("t");
    for (s = ta_items(tbg_tract(bag, k)); *s > TA_END; s++)
      printf(" %d", (int)*s);
    printf("\n");
  }
}

int main (int argc, char *argv[])
{
  static TBGSURRFN *tab[] = { tbg_ident, tbg_random, tbg_swap, tbg_shuffle };
  int      surr, target, algo, mode = FPG_DEFAULT, pack = 16, r;
  unsigned seed;
  long     cnt, i;
  ITEM     zmin, k;
  double   supp;
  const char *f;
  ITEMBASE *ibase;
  TABREAD  *tread;
  TABAG    *tabag, *tasur = NULL;
  FPGROWTH *fpg;
  RNG      *rng;

  if (argc != 10) { fprintf(stderr, "usage: see surrdump.c\n"); return 2; }
  switch (argv[1][0]) {
    case 'i': surr = 0; break;  case 'r': surr = 1; break;
    case 'p': surr = 2; break;  case 's': surr = 3; break;
    default: return 2; }
  seed = (unsigned)strtoul(argv[2], NULL, 0);
  cnt  = strtol(argv[3], NULL, 0);
  zmin = (ITEM)strtol(argv[4], NULL, 0);
  supp = strtod(argv[5], NULL);
  switch (argv[6][0]) {
    case 's': target = ISR_ALL;     break; case 'c': target = ISR_CLOSED;  break;
    case 'm': target = ISR_MAXIMAL; break; case 'g': target = ISR_GENERAS; break;
    default: return 2; }
  switch (argv[7][0]) {
    case 's': algo = FPG_SIMPLE; break; case 'c': algo = FPG_COMPLEX; break;
    case 'd': algo = FPG_SINGLE; break; case 't': algo = FPG_TOPDOWN; break;
    default: return 2; }
  for (f = argv[8]; *f; f++) {  /* the option flags of fpgpsp's main */
    if (*f == 'x') mode &= ~FPG_PERFECT;
    if (*f == 'i') mode &= ~FPG_REORDER;
    if (*f == 'u') mode &= ~FPG_TAIL;
    if (*f == '0') pack = 0;
  }
  mode = (mode & ~FPG_FIM16) | ((pack <= 0) ? 0 : (pack < 16) ? pack : 16);

  ibase = ib_create(0, 0);      /* read the transactions like main() */
  tread = trd_create();
  if (!ibase || !tread) return 1;
  trd_allchs(tread, NULL, NULL, NULL, "", NULL);
  if (trd_open(tread, NULL, argv[9]) != 0) return 1;
  tabag = tbg_create(ibase);
  if (!tabag || tbg_read(tabag, tread, 0) < 0) return 1;
  trd_delete(tread, 1);
  if ((surr == 3) && !tbg_istab(tabag)) { printf("notable\n"); return 0; }

  /* --- prepare the data like fpg_genpsp (with patches P1, P11) --- */
  fpg = fpg_create(target, supp, 100.0, 100.0, zmin, ITEM_MAX,
                   RE_NONE, FPG_NONE, 0.0, algo, mode);
  if (!fpg) return 1;
  r = (surr == 3) ? FPG_NORECODE|FPG_NOSORT : 0;
  if (target & ISR_GENERAS) r |= FPG_NOFILTER; /* P11 */
  r = fpg_data(fpg, tabag, r|FPG_NOPACK|FPG_NOREDUCE, +2);
  if (r) { printf("error %d\n", r); return 0; }
  printf("prep %d\n", (int)tbg_cnt(tabag));
  dump(tabag);
  for (k = 0; k < ib_cnt(ibase); k++)
    printf("item %d %s\n", (int)k, (const char*)ib_name(ibase, k));

  /* --- one chain: the single-threaded loop of fpg_genpsp --- */
  rng = rng_create(seed);
  if (!rng) return 1;
  for (i = 1; i <= cnt; i++) {
    tasur = tab[surr](tabag, rng, tasur);
    if (!tasur) return 1;
    printf("set %ld\n", i);
    dump(tasur);
    if (fpg_data(fpg, tasur, FPG_SURR, 0) < 0) return 1;
  }
  return 0;
}
