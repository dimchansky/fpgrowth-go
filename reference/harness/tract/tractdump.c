/*----------------------------------------------------------------------
  File    : tractdump.c
  Contents: reproduce fpg_data()'s data preparation with the upstream
            tract module and dump the item base and the transaction bag
            (fixture generator for internal/tract)
  Usage   : tractdump [options] input
              -r#  record separators          (default: "\n")
              -f#  field separators           (default: " \t,")
              -b#  blank characters           (default: " \t\r")
              -C#  comment characters         (default: "#")
              -w   last field of a record is the transaction weight
              -R#  item selection file        (ib_readsel)
              -A#  item appearance file       (ib_readapp)
              -s#  absolute minimum support   (default: 1)
              -q#  item order mode, as -q of fpgrowth (default: 2)
              -o   FPG_REORDER is set: reduce -q to its sign
              -m#  zmin for tbg_filter        (default: 0)
              -l#  number of items to pack    (default: 0)
            Option arguments must follow the option letter directly
            (e.g. -b, or -f,.;:); they are escape-decoded like in
            fpgrowth (trd_allchs).
  Output  : see README.md
----------------------------------------------------------------------*/
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <limits.h>
#include "tract.h"

static void die (const char *msg, const char *arg)
{                               /* --- print an error and abort */
  fprintf(stderr, "tractdump: %s%s\n", msg, arg ? arg : "");
  exit(2);
}  /* die() */

static void dump_bag (TABAG *bag, const char *tag)
{                               /* --- print all transactions */
  TID        j;                 /* loop variable for transactions */
  TRACT      *t;                /* to traverse the transactions */
  const ITEM *p;                /* to traverse the items */

  for (j = 0; j < tbg_cnt(bag); j++) {
    t = tbg_tract(bag, j);      /* print weight, size and items */
    printf("%s %d %d", tag, (int)ta_wgt(t), (int)ta_size(t));
    for (p = ta_items(t); *p > TA_END; p++)
      printf(" %d", (int)*p);   /* (packed entries are negative) */
    printf("\n");
  }
}  /* dump_bag() */

int main (int argc, char *argv[])
{                               /* --- main function */
  int        i, r;              /* loop variable, result */
  char       *s;                /* to traverse the options */
  const char *recseps = NULL;   /* record  separators */
  const char *fldseps = NULL;   /* field   separators */
  const char *blanks  = NULL;   /* blank   characters */
  const char *comment = NULL;   /* comment characters */
  const char *fn_inp  = NULL;   /* name of the input file */
  const char *fn_sel  = NULL;   /* name of the selection file */
  int        app      = 0;      /* whether fn_sel holds appearances */
  int        mtar     = 0;      /* mode for transaction reading */
  long       smin     = 1;      /* absolute minimum support */
  int        sort     = 2;      /* item order mode (-q) */
  int        reorder  = 0;      /* whether FPG_REORDER is set */
  long       zmin     = 0;      /* minimum transaction size */
  int        pack     = 0;      /* number of items to pack */
  ITEMBASE   *ibase;            /* item base */
  TABREAD    *tread;            /* table/transaction reader */
  TABAG      *tabag;            /* transaction bag */
  ITEM       m;                 /* number of items */
  TID        records;           /* number of records read */
  const SUPP *f;                /* item frequencies in the bag */

  for (i = 1; i < argc; i++) {  /* traverse the arguments */
    s = argv[i];
    if (*s != '-' || !s[1]) {   /* non-option: the input file */
      if (fn_inp) die("too many arguments", NULL);
      fn_inp = s; continue; }
    switch (s[1]) {             /* evaluate an option */
      case 'r': recseps = s+2;                         break;
      case 'f': fldseps = s+2;                         break;
      case 'b': blanks  = s+2;                         break;
      case 'C': comment = s+2;                         break;
      case 'w': mtar   |= TA_WEIGHT;                   break;
      case 'R': fn_sel  = s+2; app = 0;                break;
      case 'A': fn_sel  = s+2; app = 1;                break;
      case 's': smin    = strtol(s+2, NULL, 0);        break;
      case 'q': sort    = (int)strtol(s+2, NULL, 0);   break;
      case 'o': reorder = 1;                           break;
      case 'm': zmin    = strtol(s+2, NULL, 0);        break;
      case 'l': pack    = (int)strtol(s+2, NULL, 0);   break;
      default : die("unknown option ", s);             break;
    }
  }
  if (!fn_inp) die("no input file", NULL);

  /* --- read item selection/appearance indicators (fpgrowth main) --- */
  ibase = ib_create(0, 0);
  tread = trd_create();
  if (!ibase || !tread) die("not enough memory", NULL);
  trd_allchs(tread, recseps, fldseps, blanks, "", comment);
  if (fn_sel) {
    if (trd_open(tread, NULL, fn_sel) != 0) die("cannot open ", fn_sel);
    r = (app) ? ib_readapp(ibase, tread) : ib_readsel(ibase, tread);
    if (r < 0) die(ib_errmsg(ibase, NULL, 0), NULL);
    trd_close(tread);
  }

  /* --- read the transaction database (fpgrowth main) --- */
  tabag = tbg_create(ibase);
  if (!tabag) die("not enough memory", NULL);
  if (trd_open(tread, NULL, fn_inp) != 0) die("cannot open ", fn_inp);
  r = tbg_read(tabag, tread, mtar);
  if (r < 0) die(tbg_errmsg(tabag, NULL, 0), NULL);
  trd_delete(tread, 1);
  records = tbg_cnt(tabag);
  printf("read items %d records %d wgt %d maxfrq %d defapp %d\n",
         (int)ib_cnt(ibase), (int)records, (int)tbg_wgt(tabag),
         (int)ib_maxfrq(ibase), ib_getapp(ibase, -1));
  for (i = 0; i < ib_cnt(ibase); i++)
    printf("item %d %d %d %d %s\n", i, ib_getapp(ibase, i),
           (int)ib_getfrq(ibase, i), (int)ib_getxfq(ibase, i),
           ib_name(ibase, i));

  /* --- fpg_data(): sort and recode items --- */
  if (reorder)                  /* simplified sorting if reordering */
    sort = (sort < 0) ? -1 : (sort > 0) ? +1 : 0;
  m = tbg_recode(tabag, (SUPP)smin, -1, -1, -sort);
  if (m < 0) die("not enough memory", NULL);
  printf("recode %d\n", (int)m);
  for (i = 0; i < m; i++)
    printf("code %d %d %d %d %s\n", i, ib_getapp(ibase, i),
           (int)ib_getfrq(ibase, i), (int)ib_getxfq(ibase, i),
           ib_name(ibase, i));

  /* --- fpg_data(): filter, sort and reduce transactions --- */
  tbg_filter(tabag, (ITEM)zmin, NULL, 0);
  tbg_itsort(tabag, +1, 0);
  tbg_sort  (tabag, +1, 0);
  tbg_reduce(tabag, 0);
  printf("bag cnt %d records %d wgt %d max %d extent %zu\n",
         (int)tbg_cnt(tabag), (int)records, (int)tbg_wgt(tabag),
         (int)tbg_max(tabag), tbg_extent(tabag));
  dump_bag(tabag, "ta");
  if (pack > 0) {               /* fpg_data(): pack items */
    tbg_pack(tabag, pack);
    printf("pack %d\n", pack);
    dump_bag(tabag, "pta");
  }

  /* --- item frequencies as read by the miners --- */
  printf("ifrqs");
  if (m > 0) {                  /* (no array for an empty item base) */
    f = tbg_ifrqs(tabag, 0);
    if (!f) die("not enough memory", NULL);
    for (i = 0; i < m; i++) printf(" %d", (int)f[i]);
  }
  printf("\nend\n");
  tbg_delete(tabag, 1);         /* also deletes the item base */
  return 0;
}  /* main() */
