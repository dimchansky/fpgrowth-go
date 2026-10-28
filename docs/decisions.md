# Architecture decisions

Each entry records a decision, its rationale, and the alternatives that were
rejected. Newer entries may supersede older ones; superseded entries are
kept and marked.

## D1. Upstream reference version: FP-Growth 6.21 (2022.11.22)

The newest published source release on 2026-10-16. Vendored verbatim under
`reference/upstream/fpgrowth-6.21` with a SHA-256 manifest, because the
download URL is not versioned (the archive was repacked in August 2026
without a version change). See `reference/upstream/PROVENANCE.md`.

## D2. Development tooling lives in a nested module

`reference/` has its own `go.mod`. Go excludes nested modules from the
parent module's zip, so the ~28k lines of upstream C, the reference build
scripts and the differential tests are not downloaded by library users, and
`go test ./...` at the root never needs a C compiler. The nested module uses
a `replace` directive to test the working-tree copy of the root module.

Rejected: `testdata/` or `_reference/` directories (both are still shipped in
the module zip); build tags in the root module (C sources would ship).

## D3. Go version policy

- Development and CI toolchain: Go 1.27.2 (latest stable on 2026-10-16),
  pinned with the `toolchain` directive.
- Minimum supported language version (`go` directive): 1.26.0, so that both
  currently supported Go releases (1.26 and 1.27) can import the library.
  `go vet`'s `stdversion` check guards against accidental use of newer APIs.
- Revisit if a Go 1.27-only feature (e.g. generic methods) materially
  improves the public API; that would be a deliberate minimum-version bump.

## D4. License and attribution

The project is MIT-licensed like upstream. `LICENSE` carries both the
original copyright (Christian Borgelt, 2004-2022) and the port's copyright.
The project is described as a port and further development of Borgelt's
implementation, not as an independent invention.

## D5. The CLI drives `internal/fpg`, not the public API

`cmd/fpgrowth` configures `internal/fpg` in upstream notation (percent or
negative absolute thresholds, `-q`, `-l`, mode flags) because byte-for-byte
compatibility with upstream depends on details the public API deliberately
hides (float threshold notation, appearance-file registration order,
upstream error for "no items"). Equivalence of both front ends is enforced
by the root package's `TestEquivalenceCLI*` tests (identical results and
order). Stage 2 review finding L9 accepted on this basis.

## D6. Public API design

See `docs/api-design.md`: generic items, replayable `Source`s, callback
core with iterator and collect adapters, synchronous mining without
goroutines, explicit evaluation filtering.

## D7. fpgpsp: a separate command with upstream's defects fixed (issue #23)

Proposed on 2026-10-23 for issue #23 and reviewed by two independent
critics (`docs/reviews/post-v0.1.0/issue-23-*.md`); awaiting the
maintainer's sign-off.

- **Shape.** A second command, `cmd/fpgpsp`, with upstream's options,
  formats, messages and exit codes; no library API for now (a later
  sequential `PatternSpectrum` function would fit the design). It reuses
  the internal packages (`tabread`, `tract`, `fpg`, `report`, `isrout`,
  `cconv`); the new `internal/psp` ports the random numbers, the
  surrogate generators, the estimation and the decision border. Both
  programs ship in the same release archives. The input helpers of
  `cmd/fpgrowth` (spooling of standard input, cancellation, error
  messages) are copied into `cmd/fpgpsp/input.go`; a shared package is a
  possible later refactoring.
- **Semantics.** Upstream's defects that change results are fixed
  (`docs/differences.md` U1-U20, U25). The differential oracle is upstream
  with the smallest source changes that fix them
  (`reference/patches/fpgpsp/`, `build.sh fpgpsp-fixed`), so the port is
  tested byte for byte rather than against unpatched behavior it would
  have to replicate. Upstream's prefilter (`-s`, `-m`) stays part of the
  null model (P8), and so does the item order of the preparation (P7).
- **Concurrency and reproducibility.** Upstream's thread partition is
  the unit of reproducibility: `-Z k` splits `-c` into `k` chains with
  the seeds `S+i`, as upstream's threads; the spectrum depends on the
  seed and the partition only. `cmd/fpgpsp` runs the chains in at most
  GOMAXPROCS goroutines (one per chain would keep every chain's surrogate
  and miner at once: -Z20000 used gigabytes) and adds the spectra in chain
  order; `internal/psp` starts no
  goroutines and the library's guarantee (D6) is unchanged. Defaults are
  upstream's (the time as seed, all CPUs); the log prints the values used
  when they were defaulted (U24), and tests always pass `-S` and `-Z`.
- **Mining.** Every surrogate is mined on its own through the normal
  pipeline (`fpg.Miner`, recoded): that is what the oracle patches P2,
  P2b, P6 and P9 establish for C, and the spectrum does not depend on the
  item codes. The per-surrogate rebuild of the item base, the recoding
  and the prefix tree costs time and memory: measured against the C
  oracle (-O3) on sparse, table and dense data, Go needs 1.8-3.3 times
  C's instructions and 1.4-2.5 times its user time, and about twice its
  maximum resident memory at -Z64 (80-92 MB against 41 MB, measured
before the chains were bounded by GOMAXPROCS; afterwards 62-83 MB at
-Z128 in the review). Reusing the
  item base and the recoding across surrogates is the follow-up, issue
  #39.
  *Note of 2026-10-27 (#39):* the storage of the item base, the bag and
  the miner is reused per worker (`psp.Workspace`), every surrogate is
  still counted and recoded on its own (the codes need not be the same
  across surrogates, see `docs/upstream/fpgpsp.md` §7), and `-gr` draws
  through a Fenwick tree. Measured against main: -16 % to -68 %
  instructions on the shapes above (`-gr` now 0.61 times C's, the
  others 1.8-2.4 times), `-ge` unchanged, 0.09 MB instead of 3.3 MB
  allocated per surrogate
  (`docs/reviews/post-v0.1.0/issue-39-design.md`).
- **Floating point.** The estimation follows C compiled with
  `-ffp-contract=off` (explicit rounding of products, `mulAdd`). Because
  `log` and `exp` may differ in the last bit between Go and the
  platform's libm, its unrounded values are compared with a C trace to a
  relative 1e-10, and printed counts may differ from C's by one count in
  at most one cell of a rare estimation (`docs/upstream/fpgpsp.md` §7).
- **Command line.** One argument is allowed (output to standard output,
  U6); `-` keeps working. SIGINT stops the program with status 130, as
  fpgrowth.

## D8. The command-line programs read the database once if it fits (issue #48)

Proposed on 2026-10-28 for issue #48; approved by the maintainer on 2026-10-28.

- **Context.** The library reads every source twice and never keeps the
  records (D6, `docs/api-design.md`, `TestLargeFileStreaming`). The CLIs
  used the same two passes, so they parsed the input and hashed every
  item name twice. On 20,000 sparse transactions replicated 30 times
  (16 MB) the second pass was half of the 6.3G instructions of
  `fpgrowth -s50`, against 1.4G for the C program, which reads the file
  once into its transaction bag.
- **Decision.** `tract.ItemBase.Retain(limit)` makes `Count` keep the
  distinct identifiers of every counted record (4 bytes per item
  occurrence and per record, 8 more per record once a weight other than
  1 occurs, in 128 KiB blocks that are never copied). The next
  `BuildBag` builds the bag from them through the recoding table instead
  of calling its scan function, and releases them. If the records exceed
  the budget, `Count` releases them at once and `BuildBag` scans as
  before. Both CLIs retain up to 128 MiB (`retainLimit`), except
  `fpgpsp -gs`, which takes its records from a second scan
  (`psp.Records`) and builds no bag from the item base; the library
  does not call `Retain`.
- **Consequences.** Within the budget, the CLIs read the input once,
  like upstream, and maximum resident memory grows by the retained
  records (B×30: 32 MB against 6 MB before and 44 MB for C); above it,
  memory is bounded by the budget and the input is read twice. The
  output is the same on both paths (`TestGoldenRetention` runs every
  golden case of both programs with retention, without it, and over
  budget; `TestItemBase_Retain_matchesScan` compares the bags). Within
  the budget, a file that changes between the passes is no longer
  reported as "input changed while reading" (`ErrNotReplayable`); the
  programs use the records they read, as upstream does. The library keeps
  its replay check.
- **Rejected.** Retaining without a budget (memory proportional to the
  input, as in C); a budget derived from the file size (standard input
  has none, and the fallback costs little: storing the records is about
  4 % of pass 1); dense integer identifiers from the reader to a
  slice-indexed item base (candidate 5 of `docs/benchmarks.md`), which
  would change the generic item base and every caller, while
  `tract.CountBytes` already saves the second lookup of pass 1.
