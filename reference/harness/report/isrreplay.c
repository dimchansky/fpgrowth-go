/*
 * isrreplay - replay a scripted sequence of item set reporter calls
 * against upstream report.c (compiled exactly like repcm.o for fpgrowth,
 * i.e. with -DISR_PATSPEC -DISR_CLOMAX) and print everything the reporter
 * decides: reported item sets and rules, call results, per-size counts and
 * the pattern spectrum. Used to produce the golden files of
 * internal/report/testdata; the Go test replays the same scripts through
 * internal/report and compares the output.
 *
 * Usage: isrreplay script.isr > script.c.out
 *
 * Script format: one command per line, '#' starts a comment. Base
 * commands (before any reporter command):
 *   items N                  N items with codes and names 0..N-1
 *   total W                  total transaction weight (ib_setwgt)
 *   freqs F0 ... F(N-1)      item frequencies (ib_setfrq)
 * Configuration (the reporter is created by the first such command):
 *   target T [nofilter] [noexpand] [sort] [dir D]
 *                            T = all|closed|maximal|generators|rules,
 *                            D defaults to -1 (isr_settarg)
 *   supp SMIN SMAX           isr_setsupp; SMAX may be "max" (RSUPP_MAX)
 *   size ZMIN ZMAX           isr_setsize; ZMAX may be "max" (ITEM_MAX)
 *   border SIZE SUPP         isr_setbdr
 *   eval logrto DIR THRESH   isr_seteval(rep, isr_logrto, NULL, DIR, THRESH)
 *   psp                      isr_addpsp(rep, NULL)
 *   output count|emit|file   count: no output at all (count-only mode
 *                            unless border/eval); emit: report and rule
 *                            callbacks print every set/rule (normal path);
 *                            file: write to a memory stream with the
 *                            fpgrowth defaults hdr "", sep " ", imp " <- ",
 *                            info " (%a)" (fast output mode if eligible)
 *   setup                    isr_setup
 * Mining:
 *   add I S                  isr_add; prints "add I S -> R"
 *   addpex I                 isr_addpex; prints "addpex I -> R"
 *   addpexpk B               isr_addpexpk(rep, B | TA_END)
 *   remove N                 isr_remove
 *   report                   isr_report; prints "report -> R" if R != 0
 *   reportv V                isr_reportv; prints "reportv -> R" if R != 0
 *   reprule I BODY HEAD E    isr_reprule; prints "reprule -> R" if R != 0
 *   xable N                  prints "xable N -> R"
 *   uses I                   prints "uses I -> R"
 *   tail I1 ... In           isr_tail; prints "tail I1 ... In -> R"
 *   cnt                      prints "cnt -> isr_cnt"
 *   getsupp                  prints "getsupp -> isr_supp"
 *   pexcnt                   prints "pexcnt -> isr_pexcnt"
 *
 * Output lines:
 *   set I1 ... Ik : SUPP w=WGT e=EVAL          (emit mode, repofn)
 *   rule H <- B1 ... Bk : SUPP BODY HEAD e=EVAL (emit mode, rulefn)
 *   line TEXT                                  (file mode, after the run)
 *   all: N / %3d: N                            (isr_prstats, after the run)
 *   psp SIZE SUPP COUNT, psp sigcnt=N total=N  (pattern spectrum, if any)
 * Floating-point values are printed with "%.17g".
 */
#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <limits.h>
#include "report.h"

#define MAXLINE  (1 << 20)      /* maximum script line length */
#define MAXTOK   (1 << 16)      /* maximum number of tokens per line */

static ITEMBASE *base    = NULL;
static ISREPORT *rep     = NULL;
static ITEM      nitems  = -1;
static SUPP      total   = 0;
static SUPP     *freqs   = NULL;
static int       outmode = 0;   /* 0: count, 1: emit, 2: file */
static char     *membuf  = NULL;
static size_t    memsize = 0;
static FILE     *memfile = NULL;
static int       lineno  = 0;

static void die (const char *msg, const char *arg)
{
  fprintf(stderr, "isrreplay: line %d: %s%s%s\n", lineno, msg,
          arg ? " " : "", arg ? arg : "");
  exit(2);
}

static long num (const char *s)
{
  char *end;
  long v;
  if (!s) die("missing number", NULL);
  v = strtol(s, &end, 10);
  if (*end) die("invalid number", s);
  return v;
}

static double dbl (const char *s)
{
  char *end;
  double v;
  if (!s) die("missing number", NULL);
  v = strtod(s, &end);
  if (*end) die("invalid number", s);
  return v;
}

static void repofn (ISREPORT *r, void *data)
{
  ITEM i, n = isr_cnt(r);
  (void)data;
  fputs("set", stdout);
  for (i = 0; i < n; i++) printf(" %d", (int)isr_itemx(r, i));
  printf(" : %d w=%.17g e=%.17g\n", (int)isr_supp(r), isr_wgt(r),
         isr_eval(r));
}

static void rulefn (ISREPORT *r, void *data, ITEM item, RSUPP body,
                    RSUPP head)
{
  ITEM i, n = isr_cnt(r);
  (void)data;
  printf("rule %d <-", (int)item);
  for (i = 0; i < n; i++)
    if (isr_itemx(r, i) != item) printf(" %d", (int)isr_itemx(r, i));
  printf(" : %d %d %d e=%.17g\n", (int)isr_supp(r), (int)body, (int)head,
         isr_eval(r));
}

static void need_rep (void)
{
  ITEM i;
  char name[32];
  if (rep) return;
  if (nitems < 0) die("'items' must come first", NULL);
  base = ib_create(0, 0);
  if (!base) die("out of memory", NULL);
  for (i = 0; i < nitems; i++) {
    snprintf(name, sizeof(name), "%d", (int)i);
    if (ib_add(base, name) != i) die("cannot add item", name);
    ib_setfrq(base, i, freqs ? freqs[i] : 0);
  }
  ib_setwgt(base, total);
  rep = isr_create(base);
  if (!rep) die("out of memory", NULL);
}

static void print_psp (PATSPEC *psp)
{
  ITEM   size;
  RSUPP  supp;
  size_t frq;
  PSPROW *row;
  for (size = psp->minsize; size <= psp->max; size++) {
    row = psp->rows +size;      /* same traversal as psp_report() */
    if (!row->frqs) continue;
    for (supp = row->min; supp <= row->max; supp++) {
      if ((frq = row->frqs[supp -row->min]) <= 0) continue;
      printf("psp %d %d %zu\n", (int)size, (int)supp, frq);
    }
  }
  printf("psp sigcnt=%zu total=%zu\n", psp_sigcnt(psp), psp_total(psp));
}

static void finish (void)
{
  char *s, *e;
  need_rep();
  if (outmode == 2) {
    if (isr_close(rep) != 0) die("isr_close failed", NULL);
    for (s = membuf; s && (s < membuf +memsize); s = e+1) {
      e = memchr(s, '\n', (size_t)(membuf +memsize -s));
      if (!e) e = membuf +memsize;
      printf("line %.*s\n", (int)(e-s), s);
    }
  }
  isr_prstats(rep, stdout, 0);
  if (isr_getpsp(rep)) print_psp(isr_getpsp(rep));
}

static void command (char **tok, int n)
{
  const char *c = tok[0];
  int i, r;

  if      (strcmp(c, "items") == 0) {
    if (rep) die("'items' after reporter creation", NULL);
    nitems = (ITEM)num(tok[1]); }
  else if (strcmp(c, "total") == 0) {
    if (rep) die("'total' after reporter creation", NULL);
    total = (SUPP)num(tok[1]); }
  else if (strcmp(c, "freqs") == 0) {
    if (rep || (nitems < 0)) die("misplaced 'freqs'", NULL);
    if (n-1 != nitems) die("wrong number of frequencies", NULL);
    freqs = (SUPP*)malloc((size_t)(nitems > 0 ? nitems : 1) *sizeof(SUPP));
    if (!freqs) die("out of memory", NULL);
    for (i = 1; i < n; i++) freqs[i-1] = (SUPP)num(tok[i]); }
  else if (strcmp(c, "target") == 0) {
    int t, m = 0, d = -1;
    need_rep();
    if      (!tok[1]) die("missing target", NULL);
    if      (strcmp(tok[1], "all")        == 0) t = ISR_ALL;
    else if (strcmp(tok[1], "closed")     == 0) t = ISR_CLOSED;
    else if (strcmp(tok[1], "maximal")    == 0) t = ISR_MAXIMAL;
    else if (strcmp(tok[1], "generators") == 0) t = ISR_GENERAS;
    else if (strcmp(tok[1], "rules")      == 0) t = ISR_RULES;
    else die("unknown target", tok[1]);
    for (i = 2; i < n; i++) {
      if      (strcmp(tok[i], "nofilter") == 0) m |= ISR_NOFILTER;
      else if (strcmp(tok[i], "noexpand") == 0) m |= ISR_NOEXPAND;
      else if (strcmp(tok[i], "sort")     == 0) m |= ISR_SORT;
      else if (strcmp(tok[i], "dir")      == 0) d = (int)num(tok[++i]);
      else die("unknown mode", tok[i]);
    }
    if (isr_settarg(rep, t, m, d) != 0) die("isr_settarg failed", NULL); }
  else if (strcmp(c, "supp") == 0) {
    need_rep();
    isr_setsupp(rep, (RSUPP)num(tok[1]),
      (tok[2] && strcmp(tok[2], "max") == 0) ? RSUPP_MAX
                                             : (RSUPP)num(tok[2])); }
  else if (strcmp(c, "size") == 0) {
    need_rep();
    isr_setsize(rep, (ITEM)num(tok[1]),
      (tok[2] && strcmp(tok[2], "max") == 0) ? ITEM_MAX
                                             : (ITEM)num(tok[2])); }
  else if (strcmp(c, "border") == 0) {
    need_rep();
    if (isr_setbdr(rep, (ITEM)num(tok[1]), (RSUPP)num(tok[2])) < 0)
      die("isr_setbdr failed", NULL); }
  else if (strcmp(c, "eval") == 0) {
    need_rep();
    if (!tok[1] || strcmp(tok[1], "logrto") != 0)
      die("unknown evaluation", tok[1]);
    isr_seteval(rep, isr_logrto, NULL, (int)num(tok[2]), dbl(tok[3])); }
  else if (strcmp(c, "psp") == 0) {
    need_rep();
    if (isr_addpsp(rep, NULL) < 0) die("isr_addpsp failed", NULL); }
  else if (strcmp(c, "output") == 0) {
    need_rep();
    if      (!tok[1]) die("missing output mode", NULL);
    if      (strcmp(tok[1], "count") == 0) outmode = 0;
    else if (strcmp(tok[1], "emit")  == 0) {
      outmode = 1;
      isr_setrepo(rep, repofn, NULL);
      isr_setrule(rep, rulefn, NULL); }
    else if (strcmp(tok[1], "file")  == 0) {
      outmode = 2;
      memfile = open_memstream(&membuf, &memsize);
      if (!memfile) die("open_memstream failed", NULL);
      if (isr_setfmt(rep, 0, "", " ", " <- ", " (%a)") != 0)
        die("isr_setfmt failed", NULL);
      if (isr_open(rep, memfile, "<memory>") != 0)
        die("isr_open failed", NULL); }
    else die("unknown output mode", tok[1]); }
  else if (strcmp(c, "setup") == 0) {
    need_rep();
    if (isr_setup(rep) != 0) die("isr_setup failed", NULL); }
  else if (strcmp(c, "add") == 0) {
    r = isr_add(rep, (ITEM)num(tok[1]), (RSUPP)num(tok[2]));
    printf("add %s %s -> %d\n", tok[1], tok[2], r); }
  else if (strcmp(c, "addpex") == 0) {
    r = isr_addpex(rep, (ITEM)num(tok[1]));
    printf("addpex %s -> %d\n", tok[1], r); }
  else if (strcmp(c, "addpexpk") == 0) {
    isr_addpexpk(rep, (ITEM)num(tok[1]) | TA_END); }
  else if (strcmp(c, "remove") == 0) {
    isr_remove(rep, (ITEM)num(tok[1])); }
  else if (strcmp(c, "report") == 0) {
    if ((r = isr_report(rep)) != 0) printf("report -> %d\n", r); }
  else if (strcmp(c, "reportv") == 0) {
    if ((r = isr_reportv(rep, dbl(tok[1]))) != 0)
      printf("reportv -> %d\n", r); }
  else if (strcmp(c, "reprule") == 0) {
    r = isr_reprule(rep, (ITEM)num(tok[1]), (RSUPP)num(tok[2]),
                    (RSUPP)num(tok[3]), dbl(tok[4]));
    if (r != 0) printf("reprule -> %d\n", r); }
  else if (strcmp(c, "xable") == 0) {
    printf("xable %s -> %d\n", tok[1], isr_xable(rep, (ITEM)num(tok[1]))); }
  else if (strcmp(c, "uses") == 0) {
    printf("uses %s -> %d\n", tok[1], isr_uses(rep, (ITEM)num(tok[1]))); }
  else if (strcmp(c, "tail") == 0) {
    ITEM *items = (ITEM*)malloc((size_t)(n > 1 ? n-1 : 1) *sizeof(ITEM));
    if (!items) die("out of memory", NULL);
    for (i = 1; i < n; i++) items[i-1] = (ITEM)num(tok[i]);
    r = isr_tail(rep, items, (ITEM)(n-1));
    fputs("tail", stdout);
    for (i = 1; i < n; i++) printf(" %s", tok[i]);
    printf(" -> %d\n", r);
    free(items); }
  else if (strcmp(c, "cnt") == 0) {
    printf("cnt -> %d\n", (int)isr_cnt(rep)); }
  else if (strcmp(c, "getsupp") == 0) {
    printf("getsupp -> %d\n", (int)isr_supp(rep)); }
  else if (strcmp(c, "pexcnt") == 0) {
    printf("pexcnt -> %d\n", (int)isr_pexcnt(rep)); }
  else die("unknown command", c);
}

int main (int argc, char *argv[])
{
  FILE  *in;
  char  *line, *s, *h;
  char **tok;
  int    n;

  if (argc != 2) {
    fprintf(stderr, "usage: %s script.isr\n", argv[0]);
    return 2;
  }
  in = fopen(argv[1], "r");
  if (!in) { perror(argv[1]); return 2; }
  line = (char*)malloc(MAXLINE);
  tok  = (char**)malloc((MAXTOK+4) *sizeof(char*));
  if (!line || !tok) die("out of memory", NULL);
  while (fgets(line, MAXLINE, in)) {
    lineno++;
    if ((h = strchr(line, '#')) != NULL) *h = 0;
    for (n = 0, s = strtok(line, " \t\r\n"); s && (n < MAXTOK);
         s = strtok(NULL, " \t\r\n"))
      tok[n++] = s;
    tok[n] = tok[n+1] = tok[n+2] = tok[n+3] = NULL;
    if (n > 0) command(tok, n);
  }
  fclose(in);
  finish();
  return 0;
}
