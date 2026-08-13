# Platform domain boundaries

Status: **incremental migration started; legacy compatibility facade remains**.

Platform is a modular monolith organized around DDD bounded contexts. Process
and database separation are not prerequisites: the first objective is explicit
ownership and inward dependency direction while retaining every existing route,
identity check, persistence format, and Hub/Spoke invariant.

## Context ownership

| Context | Owns |
|---|---|
| Catalog | Products, Releases, Modules, channels, immutable coordinates |
| Environments | Environment inventory, profiles, labels, desired configuration |
| Governance | Policies, grants, approvals, Change Requests, authorization audit |
| Delivery | Plans, promotions, recall, suppression, maintenance, rollback intent |
| Execution | Leases, commands, tasks, last-safe-point authorization |
| Reporting | Reported state, observations, health, fleet and overview projections |
| Artifacts | Bundles, signatures, scans, publication and transfer receipts |
| Federation | Cross-environment exchange and immutable transfer receipts |
| Identity | Operator and Agent authentication and principal translation |
| Optimization | State twin and advisory cost/value/risk recommendations |
| Hub | HTTP composition and explicit cross-context workflows, never domain rules |

Each context converges on `domain`, `application`, `ports`, and `adapters`.
Adapters depend inward; domains do not import HTTP, filesystems, Kubernetes, or
another context's implementation. Contexts exchange stable identifiers,
immutable snapshots, events, or narrow query ports—not mutable aggregates.

Hub and Spoke are trust/deployment boundaries independent of the domain split.
Their versioned protocol contains messages and receipts, never shared domain
objects. Apollo is a bounded integration context around its generated public
contract, evidence, conformance rules, and anti-corruption translation. Foundry
compatibility remains a separate anti-corruption adapter.

Apollo compatibility is not yet an adapter in the structural sense, and cannot
become one before the model moves. Its *vocabulary* is already held apart —
`ApolloProductManifest` is deliberately not the internal `Release` — but the
translation between them lives inside `internal/controlplane`, and functions
like `ExecutableReleaseFromApolloManifest` return control-plane types. Lifting
those 4,675 lines into an `internal/apollo` package would make `controlplane`
import it while it still imports `controlplane` back, which is a cycle rather
than a layering. Only `apollo_substitution.go` is free of control-plane types
today, and extracting one file in eight would move 8% of the surface while
implying all of it had moved.

So the sequence already written above is the constraint: the shared model has to
reach a package both sides can see, which is what separating Catalog from
Environments does, before the anti-corruption layer can have a package of its
own. Apollo must not become the name for Conduit's entire domain. Its bounded
context is the foreign/public language and translation edge; Delivery,
Governance, Execution, and persistence continue to own Conduit policy. The edge
is complete only when pinned OpenAPI generates the DTOs and strict server
interface used by the adapter, rather than only its route table.

## Migration sequence

1. Characterize routes, persistence, isolation, audit, immutable coordinates,
   and last-safe-point behavior; enforce dependency direction.
2. Route executable construction through `internal/hub/composition`, retaining
   the legacy facade temporarily.
3. Extract Reporting, then Identity and Artifacts.
4. Separate Catalog from Environments, Governance from Delivery, and Delivery
   from Execution.
5. Replace the universal repository with context-specific ports whose first
   adapters may preserve the existing physical file layout.
6. Formalize idempotent cross-context workflows and remove migrated behavior
   from `internal/controlplane`.

The first executable seam is `composition.NewHubHandler`. The gate
`node scripts/verify-domain-boundaries.mjs` rejects direct executable bypass and
forbidden infrastructure dependencies in future domain packages; its focused
regression catches three planted violations. Completion
still requires the Reporting extraction and retirement of the shared facade;
this initial seam alone is not a completed DDD migration.

Delivery's first slice is `internal/delivery/rollout`, built rather than
migrated: it describes a rollout as a scheduling problem in Delivery's own
vocabulary, reads the fleet through narrow query ports, and hands the result to
Optimization through an adapter. No behaviour moved out of
`internal/controlplane` for it — the point was to stop adding to the monolith,
not to start dismantling it out of sequence. The domain is covered by the
existing `verify-domain-boundaries.mjs` gate like any other.

## Plan and Environment are not cyclic

A Plan names its Environment by identifier. `Plan.Environment` is a `string`,
and `Environment` has no `Plan` field. There is no cycle between them and no
design decision outstanding, though an earlier reading of this migration
recorded one: a closure analysis that matched the *word* `Environment` — the
name of a field — as though it were a type reference reported a mutual
dependency that does not exist. The corrected measurement, reading field types
only, gives `Plan` a closure of 8 and `Environment` a closure of 3.

The identifier is also the right seam, not merely the existing one. A Plan is
persisted and served to Agents, and it outlives any particular view of the
Environment it targets. Embedding the Environment would freeze a copy of its
configuration into every Plan, so a Plan replayed after the Environment changed
would describe a world that no longer exists, and two Plans for one Environment
would each carry their own answer to what that Environment is. Environments
resolves the identifier; Delivery does not hold the answer.

Delivery therefore depends on Catalog and Environments, and neither depends
back. A Plan step carries the artifacts a Release ships and the registry access
an Environment grants: it is where catalogue content and environment
configuration are combined into one instruction, and that direction is inherent
to what a Plan is.

## Upgrade readiness workflow

Upgrade readiness is a cross-context workflow, not a second deployment engine:

1. Catalog owns immutable Products, Releases, dependency constraints, deprecations, and recalls.
2. Environments owns desired channels and deployment profiles; Reporting owns observed revisions,
   health, usage, and drift.
3. Delivery compiles a candidate upgrade Plan and incompatibilities from those immutable snapshots.
4. Governance owns review, approval, waiver, and policy evidence.
5. Execution leases the approved Plan to the Spoke; the Spoke reports receipts but cannot revise it.
6. Hub exposes two anti-corruption projections over the same versioned assessment: an
   operator-facing Apollo-compatible plan/rollout view and a read-only Foundry Upgrade Assistant
   impact/remediation view. Only the operator workflow may authorize execution.

The recovered Foundry Upgrade Assistant package establishes its application identity, filters, and
`/interventions/api` plus GraphQL mounts; it does not yet establish authored server behavior. Public
Apollo evidence establishes install/upgrade/rollback planning and Rubix reconciliation, not a shared
private implementation with Upgrade Assistant. Sharing one local assessment contract is therefore an
explicit reconstruction decision that prevents two compatibility engines from disagreeing; it remains
INFERRED until stronger source evidence proves the internal join.
