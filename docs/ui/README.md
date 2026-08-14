# Console documentation

The living UI contract is intentionally small:

- `contracts/pagination.md` defines the shared index behavior.
- `contracts/overview.md` defines the Hub overview projection and its failure states.
- `contracts/session-capabilities.md` defines capability-driven navigation.

The files under `evidence/` and the dated inventories in this directory are
bounded evidence records. They can support only the observation and provenance
they name; they do not become requirements by proximity.

For Apollo-facing presentation, classify every claim under
`docs/operations/TENANT-FIDELITY.md`. A tenant screen establishes only the
visible state that was captured. Unknown placement, wording, interaction, or
API behavior stays unknown. Conduit-native behavior must be labeled as such.

The frontend currently uses handwritten TypeScript API interfaces. The intended
contract-first boundary is generated types from the pinned OpenAPI where a
public Apollo contract exists, with Conduit-native types for local extensions.
