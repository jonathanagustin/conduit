# Bounded contexts

Conduit is a delivery control plane with explicit domain boundaries. Its Apollo
integration context targets the pinned public compatibility profile; full
Apollo conformance is not yet established. Apollo-facing behavior is translated
into Conduit-owned concepts before it can affect delivery.

## Context map

| Context | Owns | Does not own |
|---|---|---|
| Apollo integration | Pinned Apollo vocabulary, generated route metadata, conformance evidence, and translation into narrow Conduit ports | Private Hub-to-Agent behavior, Conduit authorization policy, or persisted delivery state |
| Delivery | Plans, rollout rules, maintenance decisions, bounded reviewed Entity windows, ephemeral maintenance-aware lease admission, desired-state compilation, and rollback intent | Cluster access or transport-specific request shapes |
| Product assets | Immutable publisher-declared download metadata and its binding to catalogue publication receipts | Downloading bytes, signature verification, Agent execution, or publication permission policy |
| Spaces | Native Space identity and immutable resource grouping anchors | Authentication, resource grants, deployment authority, or private Apollo Space identity |
| Installation override review | Native immutable override intent and exact review/admission binding; Module metadata admission through caller-bound Hub adapters | Entity Plan/Agent bypass, Module-to-Plan execution or cluster access |
| Change Request activity | Immutable discussion records and native discussion/label result projections | Approval requirements, review rationale, request lifecycle decisions, or Plan execution |
| Governance | Principals, grants, roles, the approval-policy language and its predicates, approval requirements, the audit contract a decision is filed under, and authorization decisions | Authentication, execution, or the Change Request lifecycle that asks it for a decision |
| Identity | Conversion of verified credentials into operator or Spoke identity, and the policy a token must satisfy to be one | Permission grants, or how a token's signature is checked and how a credential arrives |
| Execution | Lease ownership, attempt numbering, expiry, and execution state | Approval policy or cluster mechanics |
| Artifacts | Signed Bundle envelopes, content identity, chunks, lineage, and staging a Bundle's layers into a blob store | Product selection, rollout policy, or where the bytes are actually kept |
| Optimization | Read-only evidence, advisory proposals, solver admission, and outcome calibration | Approval, leasing, or mutation of delivery state |
| Reporting | The fleet projection and the operator overview: what a panel shows, how much of it, how it is ordered, and what an unanswered store looks like | Authorization, or what makes a plan failed, a change awaiting approval, or a promotion blocked — those arrive already classified |
| Hub composition | Wiring HTTP handlers, repositories, and domain services | New domain rules |

Delivery, governance, execution, identity, artifacts, catalog, environments,
and persistence are Conduit-native contexts. They may use terms that also
appear in public Apollo contracts, but that does not transfer ownership of
their behavior to Apollo.

## Apollo integration boundary

[`internal/apollo`](../../internal/apollo/) is the anti-corruption boundary for
Apollo-derived material. Inputs must be one of:

1. a pinned public contract;
2. authorized, sanitized tenant evidence with a recorded scope; or
3. an explicit Conduit extension.

Unknown behavior stays unknown. A captured role name does not grant authority,
a route name does not prove private protocol behavior, and a matching JSON
shape does not make a Conduit object an Apollo domain object. Translation into
native ports is the only supported path from the integration context to the
rest of the system. See [Contract-first API](contract-first-api.md).

## Dependency direction

Domain packages may depend on their own value types and narrow ports. Adapters
depend inward on those ports. Composition code may depend on both so it can
wire them together. Domain code must not import HTTP handlers, file stores,
Helm runners, provider clients, or generated transport clients.

The intended flow is:

```text
transport or provider input
        |
        v
bounded adapter -> application port -> domain rule
        ^                              |
        |                              v
outbound adapter <- declared port <- decision or evidence
```

This keeps generated API changes, storage changes, and provider changes from
rewriting the domain model.

## The `controlplane` migration facade

[`internal/controlplane`](../../internal/controlplane/) is a temporary
compatibility facade. It still contains a large share of HTTP, persistence,
and legacy domain behavior. [`internal/hub/composition`](../../internal/hub/composition/)
currently delegates to it so extraction can proceed without changing external
behavior in one step.

New domain behavior belongs in its owning context. A migration is complete
only when the facade calls a narrow port and no longer owns a second copy of
the rule. Moving a type without moving authority is not a completed boundary.

`node scripts/report-migration-progress.mjs` reports how far this has actually
got, and distinguishes a context that holds only extracted types from one that
owns a domain. All eight do. What remains in the facade is transport,
persistence and the Change Request lifecycle -- not a second copy of any
context's rules.

A context that owns its decisions does not necessarily shrink the facade by the
same amount. Where the moved rule needed a lookup, what replaces it is an
adapter, and an adapter is longer than the straight-line code it replaces. Line
count was never a measure of how much moved.

There was a line ceiling here — a checked-in maximum for `internal/controlplane`
that could only move down, enforced as a gate. It was removed on 2026-08-19.
It did work: it is what forced the Apollo substitution language out into
`internal/apollo/substitution`. But the cost landed on the wrong work. At zero
headroom it blocked changes with nothing to do with the monolith's size — a
two-line fix preserving a rejected change request's rationale, a one-line fix to
an export-pipeline identifier — and taxing unrelated correctness work is not
what this migration is for. The direction still holds; it is held by the
boundary test below and by `scripts/verify-domain-boundaries.mjs`, which
enforces something a line count never could: that a context cannot reach back
into the facade.

## Boundary test

A change belongs in a context only if that context can name the invariant in
its own language. If a rule needs facts from another context, request those
facts through a port. If it needs to know how another context stores or
transports them, the boundary is wrong.

Related documents:

- [Hub and Spoke](hub-spoke.md)
- [Identity and governance](identity-and-governance.md)
- [Artifacts and releases](artifacts-and-releases.md)
- [Gitless GitOps](gitless-gitops.md)
- [Optimization](optimization.md)
- [Conduit reconciliation](reconciliation.md)
- [Deployment invariants](deployment-invariants.md)


The `planreview/domain` boundary owns bounded native proposal review metadata,
not DesiredState or approval authority. Existing control-plane adapters compose
it with governed membership-only proposals. See
[channel-membership-proposals.md](channel-membership-proposals.md).
