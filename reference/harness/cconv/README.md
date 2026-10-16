# cconv harness: C library strtol/strtod results

Produces `internal/cconv/testdata/expected.txt`, the golden results for the
Go emulation of `strtol`, `(int)strtol` and `strtod` in `internal/cconv`.

```bash
reference/harness/cconv/gen.sh /path/to/scratch/build   # any directory outside the repo
```

`gen.sh` compiles `cconvdump.c` (`cc -std=c99 -O1`, override with `CC`)
into the build directory, runs it on `internal/cconv/testdata/corpus.txt`
and rewrites `expected.txt` with a header that records platform and
compiler. The program never calls `setlocale`, so the "C" locale applies,
as in upstream fpgrowth.

Corpus format: `#` comment lines, otherwise one case per line starting with
`>`; escapes `\\`, `\xHH` and `\R<count>:<HH>` (byte repeated). Output per
case: the case line, a TAB, then `l<base>=value,consumed,ERANGE` for bases
0 10 16 8 2 36, `i=(int)strtol(s,0)` and `d=<IEEE bits>,consumed,ERANGE`.

The committed results come from macOS 26 (Darwin 25.6.0) arm64, Apple
clang 21. Known libc differences, handled in the Go tests: Apple libc
accepts any bytes up to `)` in `nan(...)` (glibc/ISO C: letters, digits,
`_` only; the Go code follows ISO C), and NaN payloads differ between
libraries (not reproduced). Regenerate after changing the corpus.
