// Package psp generates and estimates pattern spectra for the fpgpsp
// program (cmd/fpgpsp): a port of fpgpsp.c (fpg_genpsp, fpg_estpsp, the
// decision border of -D), of the surrogate generators of tract.c compiled
// with TA_SURR (tbg_ident, tbg_random, tbg_swap, tbg_shuffle), of the
// estimation of patspec.c compiled with PSP_ESTIM (psp_tbgest, simpest,
// cplxest, samplelp, taslots) and of the random number generator of
// util/src/random.c. See docs/upstream/fpgpsp.md.
//
// Upstream's defects are fixed; the result equals upstream with the
// oracle patches of reference/patches/fpgpsp (the fpgpsp-fixed build),
// byte for byte for the surrogate methods. The deviations are rows
// U1-U16 of docs/differences.md.
//
// # Pipeline
//
//	ib.Count(scan)                    // tbg_read
//	m, d, err := Prepare(ib, scan, c) // fpg_data of fpg_genpsp/fpg_estpsp (P1)
//	recs, _ := Records(ib, scan)      // Shuffle: the unprepared records
//	d := TableData(recs, n, zmin)     //   (FPG_NORECODE|FPG_NOSORT)
//	parts := Partition(seed, cnt, z)  // the threads of fpg_genpsp
//	spec, err := RunChain(ctx, d, method, parts[i], c, smin, smax, done)
//	spec.Merge(...)                   // psp_addpsp, in part order
//	Estimate(d, spec, zmin, ec)       // psp_tbgest (P1, P5)
//	b := Border(spec, zmin)           // -D (P3)
//
// # Upstream to Go
//
//	RNG, rng_create/rng_seed, rseed   RNG, NewRNG, RNG.Seed
//	rng_uint/urand, rng_dbl/drand     RNG.Uint32, RNG.Float64
//	(unsigned)(seed+n)                SeedOf
//	TABAG after fpg_data              Data
//	tbg_istab                         IsTable
//	tasur, dst->buf, the RNG          Chain
//	tbg_ident ... tbg_shuffle, iswap  Chain.Next (Identity ... Shuffle)
//	fpg_data(FPG_SURR) + fpg_mine     Mine (P2, P2b, P6: each data set
//	                                  is mined on its own)
//	worker, the loop of fpg_genpsp    RunChain
//	thread partition of fpg_genpsp    Partition
//	taslots, choose, dchoose          taslots, choose, dchoose
//	simpest, cplxest, samplelp        simpest, cplxest, samplelp
//	dbl_select, dbl_shuffle           dblSelect
//	decision border of main           Border, FormatBorder
//
// Nothing in this package starts goroutines; a chain is sequential and
// the command runs the chains of a partition concurrently.
package psp
