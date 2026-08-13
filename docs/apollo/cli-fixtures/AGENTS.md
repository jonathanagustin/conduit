# Apollo CLI fixture boundary

The parent Apollo evidence rules apply. This folder contains only credential-
isolated fixtures and comparison tooling for the documented public CLI surface.

- `reconstructed/` is semantic evidence only; never call it byte-equivalent.
- `captured/` may contain reviewed output from a tenant-downloaded native CLI,
  but never the CLI binary, profiles, credentials, or tenant URLs.
- Exact parity requires `verify-byte-equivalence.sh` to pass against a named
  native capture. Formatting-normalized or archive-inventory comparisons do not
  establish byte equivalence.
- Run CLI processes with a temporary per-process `HOME`; do not export or
  overwrite the caller's home directory.
