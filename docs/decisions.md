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
