/*
 * tabdump - dump what upstream's table reader (util/src/tabread.c) and
 * transaction/item-file readers (tract/src/tract.c, compiled with
 * TA_READ) make of an input file. Produces the golden files of
 * internal/tabread/testdata/golden/. Links the pristine upstream sources;
 * nothing upstream is modified.
 *
 * Usage: tabdump MODE [-r S] [-f S] [-b S] [-C S] [-w] FILE
 *
 *   -r -f -b -C  record separators, field separators, blanks, comment
 *                characters; escape-decoded by upstream (trd_allchs) and
 *                applied exactly like fpgrowth's main() does, with an
 *                empty null-value class. Without the option the upstream
 *                default of the class is kept.
 *   -w           transaction weights in the last field (TA_WEIGHT), for
 *                MODE trans.
 *
 * MODE and output (every byte outside '!'..'~' and the backslash are
 * written as \xHH; fields are written after a single space):
 *
 *   fields  one line per trd_read() call: "<delim> <rec> <pos> <field>"
 *           with delim one of ERR EOF FLD REC, rec = trd_rec(),
 *           pos = trd_pos(), until EOF or ERR.
 *   trans   ib_read() loop as in tbg_read(): per transaction
 *           "T <rec> <weight> <item>..." (items in read order, upstream
 *           drops duplicates), then "EOF" or "ERR <code> <ib_errmsg()>".
 *   sel     ib_readsel(): "I <name>" per item in identifier order, then
 *           "EOF", or "ERR <code> <message>".
 *   app     ib_readapp(): "D <default app>", "A <name> <app>" per item in
 *           identifier order, then "EOF", or "ERR <code> <message>".
 *           App codes: 0 none, 1 body, 2 head, 3 both.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "tabread.h"
#include "tract.h"

static void put (const char *s, size_t n)
{
  size_t i;
  for (i = 0; i < n; i++) {
    unsigned char c = (unsigned char)s[i];
    if (c > ' ' && c <= '~' && c != '\\') putchar(c);
    else printf("\\x%02x", c);
  }
}

static void puts0 (const char *s) { put(s, strlen(s)); }

static void dumperr (ITEMBASE *base, int code)
{
  printf("ERR %d ", code);
  puts0(ib_errmsg(base, NULL, 0));
  putchar('\n');
}

int main (int argc, char *argv[])
{
  const char *mode, *fname = NULL;
  const char *recseps = NULL, *fldseps = NULL, *blanks = NULL, *comment = NULL;
  int i, weighted = 0, r;
  TABREAD *trd;
  ITEMBASE *base;

  if (argc < 3) {
    fprintf(stderr, "usage: %s fields|trans|sel|app [-r S] [-f S] [-b S] "
                    "[-C S] [-w] FILE\n", argv[0]);
    return 2;
  }
  mode = argv[1];
  for (i = 2; i < argc; i++) {
    const char *a = argv[i];
    if (strcmp(a, "-w") == 0) { weighted = 1; continue; }
    if (a[0] == '-' && a[1] && !a[2] && strchr("rfbC", a[1])) {
      if (++i >= argc) { fprintf(stderr, "missing value for %s\n", a); return 2; }
      switch (a[1]) {
        case 'r': recseps = argv[i]; break;
        case 'f': fldseps = argv[i]; break;
        case 'b': blanks  = argv[i]; break;
        case 'C': comment = argv[i]; break;
      }
      continue;
    }
    if (fname) { fprintf(stderr, "too many arguments\n"); return 2; }
    fname = a;
  }
  if (!fname) { fprintf(stderr, "missing file\n"); return 2; }

  trd = trd_create();
  base = ib_create(0, 0);
  if (!trd || !base) { fprintf(stderr, "not enough memory\n"); return 2; }
  trd_allchs(trd, recseps, fldseps, blanks, "", comment);
  if (trd_open(trd, NULL, fname) != 0) {
    fprintf(stderr, "cannot open file %s\n", fname); return 2; }

  if (strcmp(mode, "fields") == 0) {
    static const char *names[] = { "ERR", "EOF", "FLD", "REC" };
    int d;
    do {
      d = trd_read(trd);
      printf("%s %zu %zu ", names[d + 2], trd_rec(trd), trd_pos(trd));
      put(trd_field(trd), trd_len(trd));
      putchar('\n');
    } while (d >= 0);
  }
  else if (strcmp(mode, "trans") == 0) {
    while ((r = ib_read(base, trd, weighted ? TA_WEIGHT : 0)) == 0) {
      TRACT *t = ib_tract(base);
      ITEM k;
      printf("T %zu %d", trd_rec(trd), (int)t->wgt);
      for (k = 0; k < t->size; k++) { putchar(' '); puts0(ib_name(base, t->items[k])); }
      putchar('\n');
    }
    if (r < 0) dumperr(base, r); else printf("EOF\n");
  }
  else if (strcmp(mode, "sel") == 0) {
    r = ib_readsel(base, trd);
    if (r < 0) dumperr(base, r);
    else {
      ITEM k, n = ib_cnt(base);
      for (k = 0; k < n; k++) { printf("I "); puts0(ib_name(base, k)); putchar('\n'); }
      printf("EOF\n");
    }
  }
  else if (strcmp(mode, "app") == 0) {
    r = ib_readapp(base, trd);
    if (r < 0) dumperr(base, r);
    else {
      ITEM k, n = ib_cnt(base);
      printf("D %d\n", ib_getapp(base, -1));
      for (k = 0; k < n; k++) {
        printf("A "); puts0(ib_name(base, k)); printf(" %d\n", ib_getapp(base, k)); }
      printf("EOF\n");
    }
  }
  else { fprintf(stderr, "unknown mode %s\n", mode); return 2; }

  trd_delete(trd, 1);
  ib_delete(base);
  return 0;
}
