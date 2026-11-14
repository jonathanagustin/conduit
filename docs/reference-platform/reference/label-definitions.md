# Label-definition administration

## Public source

- `https://www.palantir.com/docs/apollo/managing-labels/create-labels/`
- Reviewed 2026-08-02; source hash and disposition are recorded in
  [`review-ledger.tsv`](review-ledger.tsv).

## Derived contract

A label definition has a unique identifier that does not change, a description,
suggested resource types, a non-empty accepted-value vocabulary, and
resource-scoped administration permissions. Applying a governed label is a
two-key authorization decision: the caller needs contributor authority for the
label and write authority for the target resource.

Platform models updates as immutable revisions and deletion as a tombstone. A
tombstone prevents identifier reuse while preserving historical Change Request
and execution evidence. A value cannot be removed, and a definition cannot be
tombstoned, while the latest desired state, a Team, or an Export Pipeline still
references it.

Existing arbitrary labels predate the definition catalog, so migration is
incremental. An unknown label remains valid. Introducing a definition succeeds
only when its accepted values cover every current reference; after that point,
new assignments and selector values fail closed against the definition.

## Deliberate bounds

Suggested resource types are guidance, not an assignment allowlist; the public
source describes them as suggestions. Platform does not claim private Apollo
storage, GraphQL, or UI compatibility. It adapts the public semantics through
its own typed HTTP and persistence contracts.

## Implementation and gates

- Domain/application contract: `internal/controlplane/label_definitions.go`
- Transport adapter: `PUT`/`DELETE`/`GET /v1/labels...`
- Persistence adapters: revisioned memory and filesystem repositories
- Gate: `go test -race ./internal/controlplane -run
  'TestLabelDefinition|TestDefinedLabel|TestTeamLabelMutation'`
