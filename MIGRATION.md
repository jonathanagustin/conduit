# Conduit repository extraction

This repository was extracted from `platform/` in
`jonathanagustin/palantir` at source commit
`945f7be95accde4053c77788cb90e687103ed640` on 2026-08-13.

The initial extraction deliberately includes only committed content from
`origin/develop`; the pre-existing uncommitted Palantir worktree overlay was
not copied. Platform-specific workflows formerly stored at the Palantir
repository root were copied into `.github/workflows/`, flattened for this
repository root, and constrained to manual `workflow_dispatch` triggers.

The Go module and internal import paths were migrated from
`github.com/jonathanagustin/platform` to `github.com/jonathanagustin/conduit`.

Until the cutover is explicitly completed, Palantir remains the source of
truth for changes that landed after the source commit above. Do not run two
writers against the same logical change; replay or rebase the active DDD
refactor into this repository before declaring Conduit authoritative.
