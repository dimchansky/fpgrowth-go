/*----------------------------------------------------------------------
  File    : rngdump.c
  Contents: print sequences of upstream's random number generators
            (util/src/random.c) for the Go port (internal/psp)
  Usage   : rngdump > internal/psp/testdata/rng.c.out
  Output  : for every seed a line "rng <seed>" followed by 40 lines
            "<rng_uint> <rng_dbl as %a>" of one generator object (the
            uint and the double are drawn from two generators seeded
            alike), then "rseed <seed>", 20 lines urand() and, after
            reseeding, 20 lines drand() (%a) of the global generator.
----------------------------------------------------------------------*/
#include <stdio.h>
#include "random.h"

int main (void)
{
  static const unsigned int seeds[] = {
    0u, 1u, 2u, 7u, 8u, 42u, 1000u, 123456789u,
    2147483647u, 2147483648u, 4294967295u, 4294967294u, 1729593600u };
  size_t i;
  int    k;
  RNG    *a, *b;

  for (i = 0; i < sizeof(seeds)/sizeof(*seeds); i++) {
    a = rng_create(seeds[i]);
    b = rng_create(seeds[i]);
    if (!a || !b) return 1;
    printf("rng %u\n", seeds[i]);
    for (k = 0; k < 40; k++)
      printf("%u %a\n", rng_uint(a), rng_dbl(b));
    rng_delete(a); rng_delete(b);
    printf("rseed %u\n", seeds[i]);
    rseed(seeds[i]);
    for (k = 0; k < 20; k++)
      printf("%u\n", urand());
    rseed(seeds[i]);
    for (k = 0; k < 20; k++)
      printf("%a\n", drand());
  }
  return 0;
}
