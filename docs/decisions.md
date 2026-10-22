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

Decided by the maintainer on 2026-10-23 (issue #23; design and its
review in the issue's cycle notes).

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
  (`docs/differences.md` U1-U20). The differential oracle is upstream
  with the smallest source changes that fix them
  (`reference/patches/fpgpsp/`, `build.sh fpgpsp-fixed`), so the port is
  tested byte for byte rather than against unpatched behavior it would
  have to replicate. Upstream's prefilter (`-s`, `-m`) stays part of the
  null model (P8), and so does the item order of the preparation (P7).
- **Concurrency and reproducibility.** Upstream's thread partition is
  the unit of reproducibility: `-Z k` splits `-c` into `k` chains with
  the seeds `S+i`, as upstream's threads; the spectrum depends on the
  seed and the partition only. `cmd/fpgpsp` runs each chain in its own
  goroutine and adds the spectra in chain order; `internal/psp` starts no
  goroutines and the library's guarantee (D6) is unchanged. Defaults are
  upstream's (the time as seed, all CPUs); the log prints the values used
  when they were defaulted (U24), and tests always pass `-S` and `-Z`.
- **Mining.** Every surrogate is mined on its own through the normal
  pipeline (`fpg.Miner`, recoded): that is what the oracle patches P2,
  P2b, P6 and P9 establish for C, and the spectrum does not depend on the
  item codes. The per-surrogate rebuild of the item base and the prefix
  tree costs time (about 1.6 times C's CPU time and 3.3 times its
  instructions for pair swaps on 4,000 transactions, issue #23); a
  direct array-to-tree path is a possible optimization.
- **Floating point.** The estimation follows C compiled with
  `-ffp-contract=off` (explicit rounding of products, `mulAdd`); it is
  compared with C by a derived tolerance of one count per cell, not by
  byte goldens, because `log` and `exp` may differ in the last bit between
  Go and the platform's libm.
- **Command line.** One argument is allowed (output to standard output,
  U6); `-` keeps working. SIGINT stops the program with status 130, as
  fpgrowth.
