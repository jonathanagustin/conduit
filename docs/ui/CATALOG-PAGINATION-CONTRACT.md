# Catalog pagination contract

Status: implemented and executable, 2026-08-09.

`GET /v1/environments`, `GET /v1/products`, and
`GET /v1/fleet/installations` share one opaque keyset-pagination mechanism.
Authorization and bounded server search are applied before ordering, counting,
and pagination. A cursor is versioned, base64url encoded, limited to 2,048
bytes, and bound to the endpoint's search/filter fingerprint. Clients must treat
it as opaque.

Environment and Product reads have two projections:

- `view=summary` returns only fields needed by the catalog indexes, defaults to
  100 records, and accepts at most 200.
- omitted `view` and `view=full` preserve the complete compatibility response,
  but are capped at 20 records because nested Entities or Releases can dominate
  payload size.

The operator HTTP adapter explicitly follows every full-view page and rejects a
repeated cursor, retaining its complete legacy return type without weakening the
public bounded transport. Summary projections never include Environment
Entities or Product Releases.

The console submits search deliberately, stores `search` and `cursor` in the
URL, clears the cursor when search changes, and exposes Previous/Next controls.
Desktop tables have mobile definition-list alternatives. Browser interactions
use `EnvironmentIndexPom` and `ProductIndexPom`; `data-testid` identifies stable
structure but does not replace role, visible-name, URL, overflow, or axe gates.

This is keyset traversal over the current authorized projection, not snapshot
isolation. Concurrent insertion or removal can change later pages; the strict
seek boundary prevents the final row of one page from repeating as the first row
of the next.

## Evidence

The synthetic high-nesting fixture measured one Environment at 25,355 bytes in
full form versus 247 bytes in summary form, and one Product at 21,006 bytes
versus 147 bytes. These numbers characterize the fixture, not production
traffic.

```sh
go test ./internal/controlplane ./internal/operator
go test -race ./internal/controlplane ./internal/operator
pnpm --dir web typecheck
pnpm --dir web exec playwright test src/test/e2e/catalog.spec.ts src/test/e2e/accessibility.spec.ts
```
