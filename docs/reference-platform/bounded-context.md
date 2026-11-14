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
  -> generated DTOs + strict server interface + router
  -> Apollo adapter
  -> narrow Conduit application ports
  -> native bounded contexts
```

Native contexts do not import generated Apollo types. The adapter resolves
identity before translation, preserves immutable coordinates, and returns the
generated response shape. Persistence stores native records unless a public
contract explicitly requires retaining foreign bytes.

## Current structure

[`internal/apollo`](../../internal/apollo) contains the generated public DTOs,
strict server interface, standard-library router, and role inventory. The
production router calls that strict interface through
[`internal/controlplane/apollo_openapi_adapter.go`](../../internal/controlplane/apollo_openapi_adapter.go).
The adapter calls
[`internal/apollo/application`](../../internal/apollo/application) directly.
That package owns generated DTO validation, public projections, and translation
through explicit Catalog and Environment ports. The control-plane package owns
only HTTP identity/governance and the port implementations; there is no Apollo
to control-plane import edge and no generated-request-to-legacy-HTTP bridge.

Catalog Publications and normalized Environment Reports are native domain
types. Public generated DTOs stop inside the Apollo context. The separate `/v1`
Helm bundle adapter may add exact Conduit assets, but those fields are neither
accepted by nor advertised on the public Apollo operation.

## Migration sequence

The extraction sequence is complete for the pinned public API: narrow ports,
direct generated-interface calls, compatibility tests, and removal of
production handwritten DTO duplicates all landed together. A future contract
change must preserve those same gates rather than reopening the facade.

Each step should be independently releasable. Route and payload behavior must
remain stable unless a new pinned contract and an explicit compatibility record
authorize a change.
