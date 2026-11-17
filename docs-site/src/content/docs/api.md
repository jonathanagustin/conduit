---
title: API guide
description: Navigate the native OpenAPI schemas, protobuf messages, registered routes and current contract-coverage limits.
---

Use the contract that belongs to the deployed version. This site generates the
reference from committed native sources and distinguishes a registered HTTP
route from a typed operation. A route's presence does not establish its request
schema, authorize a caller or promise a configured backing service.

- [Native OpenAPI reference](/conduit/reference/native-api/) contains the
  operation, parameter, request, response and shared-schema material currently
  committed for administrator status, planning sessions, packaging runs and the
  ontology read/action adapter.
- [Typed RPC contracts](/conduit/reference/rpc/) render the exact protobuf
  messages and service methods for the currently modelled native reads and
  approval command.
- [Hub route coverage](/conduit/reference/route-coverage/) lists every registered
  pattern and authorization decision, explicitly marking routes without a
  native OpenAPI schema.

## Authentication and authority

Production operators use the configured bearer or certificate identity
mechanism. Agents use the separately scoped Agent identity path. Public health
and asset routes carry no operator grant. Filtered collection reads authorize
the rows they return; grant-protected operations require the specific native
resource or administrator decision. Consult the route register and actual
contract rather than inventing an identity header or role.

Keep TLS verification enabled. Development identity-header opt-ins are not a
production integration mechanism. A model's proposed action, a client's copied
resource id and the existence of a generated method do not add authority.

## Pagination and errors

Follow the cursor and limit rules of each declared operation. Do not synthesize
offsets, reuse a cursor with different filters or interpret one visible page as
the complete fleet. Different surfaces have distinct pagination contracts until
their typed migration is complete.

Preserve error status and structured outcome details where the contract defines
them. A lost approval response is an unknown outcome, not permission to replay a
write through another transport. Read the authoritative resource before retrying
an ambiguous mutation. Plan approval, packaging approval and execution leasing
are separate operations.

## Coverage and migration

The [readiness page](/conduit/readiness/) reports generated counts and the reviewed
baseline. Only the available native OpenAPI operations are rendered as schemas.
The remaining route contracts are tracked in
[#391](https://github.com/jonathanagustin/conduit/issues/391), with handlers,
browser clients and cutover tracked separately in #392–#394. This reference
cannot be an exhaustive typed Hub API until those contracts exist.

No private tenant captures, upstream client binaries or reference-platform
internal documents are copied into this site. Compatibility adapter paths are
identified by the registered names the Hub serves; they do not claim support for
a private upstream protocol.
