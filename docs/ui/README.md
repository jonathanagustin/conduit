# Console documentation

The `web/` source is now an assistant-ui default Next.js scaffold. See
[`web/README.md`](../../web/README.md) for provenance, verification commands,
and the hosting/backend integration boundary. The Go Hub still serves its
previously embedded console. The contracts below describe that former console;
they are evidence for future integration, not capabilities of the new scaffold.

The living UI contract is intentionally small:

- `contracts/pagination.md` defines the shared index behavior.
- `contracts/overview.md` defines the Hub overview projection and its failure states.
- `contracts/session-capabilities.md` defines capability-driven navigation.
- `contracts/runtime-bootstrap.md` defines the closed tenant-branding runtime surface.
- `contracts/notification-inbox.md` defines the per-principal inbox screen and the
  preference it edits, both Conduit extensions with no Apollo shape to be faithful to.
- `contracts/fleet-facets.md` defines the fleet's faceted filter rail, and in
  particular what each of its counts is over -- the one thing about a facet that
  cannot be inferred from looking at it.

The files under `evidence/` and the dated inventories in this directory are
bounded evidence records. They can support only the observation and provenance
they name; they do not become requirements by proximity.

For Apollo-facing presentation, classify every claim under
`docs/operations/TENANT-FIDELITY.md`. A tenant screen establishes only the
visible state that was captured. Unknown placement, wording, interaction, or
API behavior stays unknown. Conduit-native behavior must be labeled as such.

The Apollo public transport uses generated Go DTOs, router, and strict server
interfaces from the pinned OpenAPI snapshot. The console's Conduit-native
runtime bootstrap is a separate closed contract and cannot alter API or session
capabilities.

`foundry-frontend-architecture.md` is not a contract. It maps how the Foundry
reconstruction organises its frontend, says which of that transfers to a product
with one console embedded in a Go binary, and stages the parts that do. Its
claims about Foundry are read off a reconstruction, are corroborating at best,
and it marks its own limits. The boundaries it argues for are enforced by
`scripts/verify-console-boundaries.mjs`.
