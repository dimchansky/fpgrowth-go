# Security policy

## Supported versions

The project is in the v0 series. Security fixes go into the latest
release only; there are no maintenance branches.

| Version | Supported |
| --- | --- |
| Latest `v0.x.y` release | Yes |
| Older releases | No; upgrade to the latest release |
| `main` | Fixes land here first |

Supported Go versions follow the Go release policy: the two most recent
major Go releases (currently 1.26 and 1.27). Vulnerabilities in the Go
standard library are fixed by Go itself. When one affects the release
binaries, a new patch release is cut with the fixed toolchain (released
versions and their artifacts are never rebuilt in place). `main` is
scanned with govulncheck on every change and weekly.

## Reporting a vulnerability

Please do **not** open a public issue, pull request or discussion for a
suspected vulnerability.

Report it privately through GitHub's private vulnerability reporting:
open <https://github.com/dimchansky/fpgrowth-go/security/advisories/new>
(the "Report a vulnerability" button on the repository's Security tab).

Include, as far as you can:

- the affected version or commit and the Go version and platform;
- whether the library API or the `fpgrowth` command is affected;
- a minimal input (data file, options or program) that reproduces it;
- the impact you expect (crash, excessive memory or time, wrong file
  access, ...).

You can expect an acknowledgement within 7 days and an assessment within
30 days. Fixes are coordinated in a draft security advisory; reporters are
credited unless they prefer otherwise.

## Scope

In scope: the published module `github.com/dimchansky/fpgrowth-go`
(library and `cmd/fpgrowth`) and the release artifacts built by this
repository's workflows.

Out of scope: the development-only module `reference/` and the vendored
upstream C sources in `reference/upstream/`. Defects in the upstream C
program are documented in `docs/upstream/reference-build.md` and
`docs/differences.md`; please report those to the upstream author.

Mining is exponential in the worst case by nature: an input and options
that produce a huge number of item sets is expected behavior, not a
vulnerability. Use support and size limits, the context for cancellation
and an early stop for untrusted inputs (see the README, "Memory and
performance"). Crashes, panics, unbounded memory use not explained by the
result size, and file access outside the paths given by the caller are
in scope.
