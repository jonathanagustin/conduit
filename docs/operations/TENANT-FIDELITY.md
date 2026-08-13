# Tenant fidelity contract

Conduit reconstructs observable Apollo behavior while remaining an independently
owned superset. Fidelity means matching a demonstrated contract within its
demonstrated scope. It does not mean inferring private implementation details or
making every Conduit extension look native to Apollo.

## Claim classes

| Class | Admissible source | What may be claimed |
| --- | --- | --- |
| Public contract | Pinned Palantir documentation, OpenAPI, Conjure, published source, or documented CLI output | Only the semantics and shape the source actually establishes |
| Tenant observation | Authorized, time-bounded observation reduced to a sanitized receipt or fixture | Only the observed tenant, version, route, state, and field behavior |
| Corroborating evidence | A separate source consistent with a claim but not authoritative for it | Support for an already bounded claim; never a protocol claim by itself |
| Conduit extension | A reviewed local product decision | Conduit behavior, labeled as an extension or deliberate divergence |
| Unknown | Missing, ambiguous, stale, or inaccessible evidence | A hypothesis, gap, or boundary; never implemented or documented as Apollo fact |

Evidence does not silently change class. A generated client proves the published
type surface it was generated from, not an unpublished server implementation. A
screen proves visible composition in that state, not a hidden API. A role name
proves vocabulary, not authority, until an authorization graph establishes the
relationship.

## Required change record

Every Apollo-facing pull request records:

1. the exact behavior or shape being changed;
2. its claim class and immutable source coordinate or sanitized receipt;
3. the scope the evidence does not establish;
4. the executable test, generator check, or fixture comparison that fails on
   drift;
5. every intentional Conduit extension or divergence.

If any item is unavailable, the change remains a documented `Unknown` in the
hypothesis backlog. It does not cross into production as a guessed default.

## Contract-first boundary

Where Apollo publishes OpenAPI or Conjure, the pinned document is the transport
authority. Generation produces the frontend interfaces and server adapter
surface. Domain behavior remains behind a bounded Apollo adapter so Conduit can
extend its own domain without changing what is represented as Apollo-compatible.

Generation must be deterministic, offline-verifiable from committed inputs, and
checked for stale output. Unknown union variants and unknown fields follow the
published compatibility rule; absent evidence, they fail closed rather than
being assigned guessed meaning.

## Tenant-specific runtime behavior

The frontend bundle remains tenant-neutral. Tenant-specific runtime bootstrap
data is typed, versioned, server-resolved from trusted enrollment or routing
context, and limited to an allowlist. It must not contain credentials or load
tenant-supplied executable code. Provider safety policy cannot be overridden by
a tenant. A visual or behavioral override is labeled as a Conduit extension
unless tenant evidence establishes the corresponding Apollo behavior.

## Review rule

When implementation convenience conflicts with evidence, evidence wins. Keep
the behavior unavailable, expose a truthful unsupported state, or implement it
under a Conduit-native name. Do not invent a plausible Apollo default.
