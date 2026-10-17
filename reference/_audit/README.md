# Independent audit checkers (archived)

Source of the throwaway checkers written by the independent auditors of
`docs/audit/` (baseline `f7bb606`). They are kept so that the audit
evidence can be re-run; they are archived as run, not maintained, and not
built or tested by CI (the leading underscore makes the Go tool ignore
this directory).

Each checker was run from the root of a clean clone of the repository at
the baseline, with the files placed under `_audit/<area>/`; copy them back
there to re-run them. The commands and run counts are in the matching
report.

| Directory | Report | What it is |
| --- | --- | --- |
| `alg/bf/` | `docs/audit/algorithms.md` | Definition-based brute force for the CLI (item sets, closed, maximal, generators, rules, all measures); imports nothing from the repository |
| `alg/numcheck/`, `alg/scale/` | same | Rule-measure checks against exact formulas; large-weight scaling probes (ALG-1) |
| `alg/apicheck/`, `alg/wide/` | same | Public-API brute force; wide-database closed/maximal checks |
| `alg/*.sh` | same | Edge-input and driver scripts |
| `m5/bf/`, `m5/bfs/` | `docs/audit/m5.md` | Independent brute forces for size-limited closed/maximal sets (dense and sparse) |
| `m5/*.sh` | same | Memory/time measurement drivers |
| `api/` | `docs/audit/api.md` | Edge-case tests and a downstream consumer program for the public API |
| `diff/` | `docs/audit/differences.md` | Library claim probes and the closed-stdout probe (DIF-5) |
