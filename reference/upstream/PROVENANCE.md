# Upstream provenance

This directory contains the **unmodified** C source package of Christian
Borgelt's FP-Growth implementation. It is the behavioral reference for the
Go port and is used only by development tooling (reference builds and
differential tests). It is not part of the published Go module (the
enclosing `reference/` directory is a separate nested module).

| Field | Value |
| --- | --- |
| Program | fpgrowth — find frequent item sets with the FP-growth algorithm |
| Author | Christian Borgelt |
| Version | 6.21 (2022.11.22), from `VERSION` in `fpgrowth/src/fpgrowth.c` |
| Project page | https://borgelt.net/fpgrowth.html |
| Download URL | https://borgelt.net/src/fpgrowth.tar.gz |
| Downloaded | 2026-10-16 |
| HTTP `Last-Modified` | Thu, 13 Aug 2026 16:26:18 GMT (archive repacked; source version unchanged) |
| Archive size | 264104 bytes |
| SHA-256 (`fpgrowth.tar.gz`) | `2eeb0ef26d53872e40f8f4d7699e742534b11e68cbf779d496c1d2693e0898f0` |
| SHA-256 (`fpgrowth.zip`, same URL prefix) | `fa9e32dd697a22f255886ed5cbc1f99ad841102e2e12c8ed816fe79ea5e2c7e8` |

Newer version check (2026-10-16): the project page still advertises
version 6.21 (2022.11.22), and the `VERSION` string inside the downloaded
archive matches. No newer source release was found.

## Layout

`fpgrowth-6.21/` is the archive extracted verbatim:

| Directory | Contents |
| --- | --- |
| `fpgrowth/src` | FP-growth mining (`fpgrowth.c`), pattern-spectrum generator front end (`fpgpsp.c`) |
| `fpgrowth/ex` | Example data sets and conversion scripts |
| `fpgrowth/doc` | HTML documentation and the MIT license text |
| `tract/src` | Transactions and item base (`tract.c`), item set reporter (`report.c`), closed/maximal filter (`clomax.c`), pattern spectrum (`patspec.c`), 16-items machine (`fim16.c`) |
| `apriori/src` | Item set tree (`istree.c`), used for association rules and evaluation |
| `math/src` | Rule evaluation measures (`ruleval.c`), chi² and gamma functions |
| `util/src` | Table reader/scanner, symbol table, arrays, memory system, escapes |

Note: `fpgrowth/src/makefile` also describes an `fpgpsp` program that needs
`tract/src/tars.c` and `tract/src/pspest.c`. Those files are **not**
included in this archive, so `fpgpsp` cannot be built from it.

## License

Every module directory carries `doc/mit-license.txt` (the MIT/Expat
license template), and `fpgrowth/doc/fpgrowth.html` states:
"MIT license, or more precisely Expat License" with
"© 2004-2017 Christian Borgelt". The program's startup banner reads
"(c) 2004-2022 Christian Borgelt". No third-party code is bundled:
all modules (util, math, tract, apriori, fpgrowth) are by the same author
under the same license.

## Building the reference executable

The original build is `make` in `fpgrowth/src` (GNU make, `gcc -std=c99`).
See `reference/README.md` for the scripted, out-of-tree build used by this
project (clang works without source changes).
