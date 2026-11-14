# Console documentation

The operator UI served from `/console/` is the local packaging assistant
documented in `web/README.md`. The Blueprint screens this directory describes
(fleet, plans, products, promotions, change requests, notifications, and the
rest) are no longer rendered. The notes below stay as the evidence record of
that console. The tenant bootstrap contract is still in force.

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
- `console-navigation.md` names the six workbench areas and the older hashes that open the same tabs.
- `third-party-assets.md` credits the bundled globe imagery.

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
