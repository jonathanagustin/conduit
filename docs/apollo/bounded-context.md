# Apollo bounded integration context

Apollo is a foreign system at Conduit's boundary. The integration context keeps
Apollo names and transport rules from becoming Conduit's internal domain model.

## Ownership

The Apollo context owns:

- pinned public contracts and generated transport artifacts;
- sanitized, scoped evidence for observed tenant behavior;
- compatibility labels and conformance checks;
- translation between Apollo transport types and narrow Conduit application
  ports.

It does not own Product lifecycle policy, Environment state, approvals, Plan
execution, fleet truth, bundle persistence, or credentials. Those remain in
Conduit's Catalog, Environments, Governance, Delivery, Execution, Reporting,
Artifacts, and Identity contexts.

An Apollo role or operation name is evidence of vocabulary. It does not grant
authority by itself. Conduit policy decides whether a foreign role maps to a
native operation, and an unmapped role conveys no authority.

## Dependency direction

The intended path is one-way:

```text
pinned OpenAPI
  -> generated DTOs + strict server/client interfaces + router
  -> Apollo adapter
  -> narrow Conduit application ports
  -> native bounded contexts
```

Native contexts do not import generated Apollo types. The adapter resolves
identity before translation, preserves immutable coordinates, and returns the
generated response shape. Persistence stores native records unless a public
contract explicitly requires retaining foreign bytes.

## Current structure

[`internal/apollo`](../../internal/apollo) contains the generated public route
table and the generated role inventory. The public HTTP adapter and handwritten
payload types remain in
[`internal/controlplane/apollo_api.go`](../../internal/controlplane/apollo_api.go).
The route generator in
[`internal/tools/apolloroutegen`](../../internal/tools/apolloroutegen) reads the
pinned snapshot but intentionally emits routes only.

This is a real boundary for vocabulary and evidence, but not yet the final
structural adapter. The remaining extraction must avoid a dependency cycle with
the legacy control-plane package.

## Migration sequence

1. Generate the complete transport surface from the pinned snapshot and add a
   stale-generation gate.
2. Define the smallest application ports needed by each public operation.
3. Move translation and the HTTP adapter behind the generated strict server
   interface.
4. Keep compatibility tests at the adapter boundary and native behavior tests
   inside the owning contexts.
5. Remove handwritten transport types only after every caller uses the
   generated contract.

Each step should be independently releasable. Route and payload behavior must
remain stable unless a new pinned contract and an explicit compatibility record
authorize a change.
