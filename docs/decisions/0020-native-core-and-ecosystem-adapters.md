---
status: proposed
date: 2026-11-18
---

# Keep the core Conduit-native and isolate ecosystem adapters

## Context

Plans, Environments, Products, Releases and Bundles carry Conduit's own intent,
authority and evidence. Public reference-platform observations inform bounded
compatibility projections; they do not make those projections the core model.
UDS/Zarf and Flux are other external ecosystems with their own contracts.

The legacy `internal/controlplane` facade still mixes native composition,
repository bridging and compatibility adapters. For example,
`apollo_openapi_adapter.go` serves a generated compatibility surface, while
the native projection bridge files adapt narrowed legacy repository ports into
extracted application ports. The earlier mixed `apollo_ports.go` is removed.
Moving those files mechanically would create import cycles or move policy into
an adapter. A filename change alone would not establish a boundary.

## Decision

Keep native policy, lifecycle and evidence ownership in Conduit's bounded
contexts. Domain packages own native values and decisions; application packages
coordinate them through context-owned ports. External generated schemas,
protocol types, HTTP routes, Kubernetes resources and tool invocations belong at
the adapter edge. The Hub composition root selects implementations.

This extends the ports-and-adapters decision in [ADR 0004](0004-gitless-gitops-ports-and-adapters.md):

- Reference-platform REST/GraphQL projections translate native results at their
  adapter boundary. They must preserve native authorization and cannot turn a
  packaging-run result into Plan approval authority.
- UDS/Zarf adapters translate admitted native package and delivery operations;
  Flux adapters realize native convergence intent. Neither ecosystem owns the
  core approval or evidence-admission policy.
- A core domain or application package must not import an ecosystem adapter or
  generated external protocol package. Ports use native values and typed native
  errors, with explicit handling for unsupported or unknown observations.
- Compatibility observations remain classified under
  [ADR 0007](0007-registered-invention-over-refusal.md). A Conduit extension is
  registered as such, rather than represented as recovered private semantics.

## Migration and acceptance

Extract one coherent adapter at a time. First narrow any dependency on the
legacy aggregate repository into an application-owned port and compose its
implementation outside the domain. Then relocate the protocol adapter, update
composition and route registration, and remove the obsolete facade symbols.
Keep REST/RPC authorization, error mapping, exact identities, read filtering,
receipts and state/audit effects unchanged. Preserve existing tests and add
adapter equivalence coverage where a boundary moves.

Extend `scripts/verify-domain-boundaries.mjs` with the migrated core packages and
forbidden adapter dependencies, including generated external types. Demonstrate
that the check rejects a deliberate prohibited import after committing safe
source; restore that source and rerun the check and affected tests. A regex over
filenames alone cannot establish dependency direction.

Completion of #428 requires removing external-platform-named files and exported
identifiers from `internal/controlplane`, the new enforced dependency checks,
and unchanged behavior under the affected tests. This ADR supplies the decision
record only. It does not claim that extraction or those checks are complete.

## Current enforcement and consequences

The existing verifier rejects infrastructure imports in domain packages and
control-plane backedges from the Apollo and reconciliation cores. It also
requires Hub composition and rejects one legacy-facade reentry. It still reads
`internal/controlplane/apollo_openapi_adapter.go`; therefore its current PASS is
not proof that the facade has been cleared or that every application-to-adapter
import is forbidden.

The migration keeps temporary legacy bridges explicit instead of building a
second policy implementation. Public compatibility routes can remain available
while native internals evolve. No wire rename, REST deprecation, new execution
authority, publication or deployment follows from this decision.

## Publication catalog port prerequisite

The legacy publication catalog bridge now accepts only three native repository
methods: listing and creating Product publications and reading a retirement.
`internal/controlplane/catalog_publication_projection.go` owns that bridge and
its native error classification; it cannot access Plans, approvals or delivery
execution through its stored dependency. Adapter imports into this bridge are
rejected by the domain boundary check.

This narrows a migration seam; the bridge still uses the legacy retirement value
and the public compatibility facade still composes it. The complete protocol
adapter has not moved, and the remaining external-named facade files/identifiers
keep #428 open. No route, authorization, persistence or wire behavior changes.

The Environment inventory and release-lifecycle projections share a read-only
`ListPlans` dependency rather than the aggregate repository. Inventory remains
in `environment_inventory_projection.go` and includes command Plans; lifecycle
continues to exclude them. Their projection and ordering logic is unchanged.
A read-only fixture with no write, transaction or approval methods constrains
both stored dependencies at compile time. These bridges still consume legacy
PlanRecord snapshots; native source snapshots and the complete protocol adapter
remain later extraction steps.


The Environment reporting bridge stores only ListPlans, GetEnvironmentReport
and PutEnvironmentReport capabilities. Its raw compatibility report update still
requires the original optional atomic-store interface; unsupported stores remain
unavailable rather than receiving a non-atomic fallback. Native projection
methods live in `environment_reporting_projection.go`, with adapter imports
rejected. Raw compatibility snapshots and their legacy persistence remain a
separate extraction step, and the protocol facade still composes these bridges.


Inventory ordering and release-lifecycle aggregation now live behind the native
Environment application's ProjectionSource contract. It receives fresh native
per-Environment snapshots, not PlanRecord or protocol objects. The legacy source
adapter keeps latestDesiredStates/latestDeclaredStates selection and cloning of
selected desired states; it does not clone every historical Plan. The application
owns deterministic source-key ordering and returned slice ownership. Boundary
checks reject legacy/reference imports into this application. Latest-Plan storage
selection and raw compatibility persistence remain later extraction steps.
