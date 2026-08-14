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

Repository cutover completed when the private Conduit repository and protected
trunk were created. Conduit is now authoritative for its architecture,
contracts, implementation, documentation, and release workflow. The Palantir
commit above is immutable migration provenance, not a live source tree.

Changes that landed in Palantir after the extraction are candidates for an
explicit, reviewed migration. They do not overwrite Conduit automatically and
must not be implemented by two active writers. Compare such a candidate against
the current Conduit contract, preserve its provenance, and land it through the
Conduit trunk workflow if it still belongs here.
