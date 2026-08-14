# Contract-first API

Transport contracts are source artifacts. Generated code implements their
wire boundary; handwritten code implements Conduit behavior behind that
boundary.

## Required pipeline

For an OpenAPI contract, the repository should generate:

- request and response DTOs;
- the strict server interface;
- router registration and parameter decoding;
- a typed client used by contract tests and supported consumers; and
- deterministic metadata needed for route and security checks.

Conjure follows the same rule when it is the authoritative source. The pinned
contract and generator version are inputs to generation. Generated output is
checked in when the build or release process needs offline reproducibility.

```text
pinned OpenAPI or Conjure
          |
          v
 deterministic generator
          |
          +-- DTOs
          +-- strict server interface
          +-- router
          +-- typed client
          |
          v
 Apollo adapter -> Conduit application ports -> native domain model
```

## Domain separation

Generated DTOs are transport types. They may preserve optional fields, unions,
error envelopes, and names required by the contract. They must not become the
types used to decide approval, rollout, persistence, or execution policy.

The adapter performs explicit translation and validation. A transport field
with no native meaning is either preserved as bounded opaque data, rejected, or
recorded as an unsupported contract area. It is never assigned a guessed
meaning. Native types may be stricter than the wire shape when Conduit owns the
rule; the adapter returns a contract-shaped error when validation fails.

## Apollo scope

Apollo conformance is limited to the pinned public contract and authorized
tenant evidence described by
[`internal/apollo`](../../internal/apollo/). A generated route proves that
Conduit has a handler slot for that operation. It does not prove compatibility
with private Apollo services, authentication flows, or Hub-to-Agent protocols.

Apollo role vocabulary also remains separate from Conduit authority. Generated
Apollo roles describe the observed or public source. The governance context
decides which exact roles, if any, map to native permissions. See
[Identity and governance](identity-and-governance.md).

## Current state and migration target

Current code generates Apollo route and role metadata, while much of the HTTP
surface, DTO layer, and service composition remains handwritten inside
[`internal/controlplane`](../../internal/controlplane/). Contract-first
generation is therefore incomplete.

The migration target is a generated strict transport boundary calling narrow
application ports. Work should proceed operation by operation:

1. pin the source contract and generator;
2. generate the complete transport slice for the chosen operations;
3. add an adapter into an existing native application port;
4. run fixture, round-trip, and negative conformance tests; and
5. remove the duplicate handwritten transport code.

Do not regenerate a broad surface and declare completion while the generated
interface is bypassed in production.

## Change gate

Review the semantic contract diff before generated files. A contract change
must identify its source, scope, and compatibility effect. Generation must be
reproducible from a clean tree, and tests must call the generated client against
the generated server boundary. CI remains manually dispatched; see
[Deployment invariants](deployment-invariants.md).
