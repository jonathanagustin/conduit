# Tenant fidelity

Conduit may reproduce an Apollo behavior only when a named source establishes
that behavior. Similar vocabulary or a plausible interface is not evidence.

## Claim classes

| Class | Required basis | Allowed statement |
| --- | --- | --- |
| Public contract | Pinned Palantir OpenAPI, Conjure, public documentation, published source, or captured public CLI output | The shape and behavior established by that source |
| Tenant observation | Authorized, sanitized receipt with tenant, version, time, route, and state | The behavior observed within that scope |
| Corroboration | An independent source that agrees with a bounded claim | Support for the claim, never a new protocol fact |
| Conduit extension | A reviewed local decision | Conduit behavior identified as an extension or divergence |
| Unknown | Missing, ambiguous, inaccessible, or stale evidence | A backlog item or unavailable state |

Evidence keeps its class. A screen does not establish its backing API. A client
type establishes the source document's type surface, not an unpublished server
implementation. A tenant observation does not establish behavior for another
tenant or version.

## Change record

An Apollo-facing pull request must state:

- the changed behavior or data shape;
- the claim class and immutable source coordinate;
- the scope the source does not establish;
- the test, generated-output comparison, or sanitized fixture that detects drift;
- each deliberate Conduit extension or divergence.

Missing evidence remains `Unknown`. Do not supply an Apollo-like default.

## Contract boundary

Pinned OpenAPI or Conjure is the transport authority where it exists. Generated
DTOs, client types, and strict server interfaces should meet a bounded Apollo
adapter. Conduit domain policy stays behind that adapter. Generation must be
deterministic from committed inputs and fail when committed output is stale.

The repository currently has a generated route surface, but generated DTOs and
the strict server interface remain open work. Do not describe the current
adapter as proof of compatibility with a private Hub-to-Agent protocol.

## Tenant runtime data

A frontend bundle may receive tenant-specific runtime data only through a
versioned, typed, server-resolved bootstrap document. The allowlist must exclude
credentials and executable code. Tenant data cannot weaken platform security
policy. Until that boundary and its tests exist, tenant-specific overrides are
an open design target, not shipped behavior.
