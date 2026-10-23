package psp

// rnorm is random.c's RNORM, 1/(UINT_MAX+1) = 2^-32: it maps an unsigned
// 32-bit value to [0, 1).
const rnorm = 0x1p-32

// RNG is a port of upstream's random number generator (util/src/random.c:
// the RNG object of rng_create, rng_uint and rng_dbl, and the global
// generator of rseed, urand and drand, which uses the same algorithm and
// seeding). It is George Marsaglia's xorshift generator with a Weyl-like
// output step ("xorwow" family), on 32-bit unsigned arithmetic. The zero
// value is not seeded; use NewRNG or Seed. An RNG is not safe for
// concurrent use.
type RNG struct {
	s [5]uint32
}

// NewRNG returns a generator seeded like rng_create(seed).
func NewRNG(seed uint32) *RNG {
	r := new(RNG)
	r.Seed(seed)
	return r
}

// Seed is a port of rng_seed (and of rseed): it fills the state with a
// linear congruential sequence started at seed and discards the first
// five outputs.
func (r *RNG) Seed(seed uint32) {
	r.s[0] = seed
	for i := 1; i < 5; i++ {
		seed = 69069*seed + 362437
		r.s[i] = seed
	}
	for range 5 {
		r.Uint32()
	}
}

// Uint32 is a port of rng_uint (and urand).
func (r *RNG) Uint32() uint32 {
	s := &r.s
	t := s[0] ^ (s[0] >> 7)
	s[0], s[1], s[2], s[3] = s[1], s[2], s[3], s[4]
	s[4] = (s[4] ^ (s[4] << 6)) ^ (t ^ (t << 13))
	return (s[1] + s[1] + 1) * s[4]
}

// Float64 is a port of rng_dbl (and drand): a value in [0, 1) with a
// resolution of 2^-32. The product is exact.
func (r *RNG) Float64() float64 { return float64(r.Uint32()) * rnorm }

// SeedOf returns the seed of chain n for the base seed of option -S:
// fpg_genpsp's rng_create((unsigned int)(seed+n)) with a 64-bit long, so
// the sum is truncated to its low 32 bits.
func SeedOf(seed int64, n int) uint32 { return uint32(seed + int64(n)) }
