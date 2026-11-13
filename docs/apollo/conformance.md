# Apollo conformance boundary

Conduit uses three different labels because they make different claims:

- **Public-profile compatible** means a named, pinned public format is served
  and checked against that exact source.
- **Apollo-aligned** means a Conduit behavior matches reviewed public semantics,
  with the tested scope stated beside it.
- **Conduit-native** means the behavior is a local contract. Similar Apollo
  vocabulary does not turn it into Apollo compatibility.

No public Palantir certification suite is used here. Conduit does not claim
compatibility with Apollo's private Hub-to-Agent protocol, and it is not an
unqualified superset of Apollo.

## Current claims

| Surface | Source and gate | Supported claim |
| --- | --- | --- |
| Public API v0 transport | Pinned OpenAPI snapshot; `scripts/verify-apollo-routes.sh` | Generated DTOs, strict server interface, and router contain exactly the 13 operations in the snapshot |
| Public API v0 payload behavior | `scripts/apollo-docs/verify-public-api-adapter.rb`; `go test ./internal/controlplane -run TestApolloPublicAPI` | The production router uses the generated strict interface and the named adapter implements the tested envelope profile |
| Published Apollo Agent configuration types | Pinned generated OSDK evidence; `go test ./internal/foundryinterop` | Structural compatibility with the named generated types, not a private service protocol |
| CLI Product fixtures | `scripts/apollo-docs/validate-cli-fixtures.rb`; byte comparison fixture | Semantic validation of the reconstructed 0.814.0 shape; byte compatibility is not claimed without a native capture |
| Module examples | `scripts/apollo-docs/validate-module-definitions.rb` | The committed examples satisfy the reviewed public Module constraints |
| Apollo Hub GraphQL surface | `api/graphql/apollo.observed.graphql`; `node scripts/apollo-graphql/verify-observed-schema.mjs` | An observed subset of one authorized Hub's GraphQL contract, recovered by validation probing and bound to its own schema hash. Not Apollo's published schema and not a complete one |
| Apollo-shaped GraphQL interface Conduit serves | `api/graphql/apollo.interface.graphql`; `node scripts/apollo-graphql/verify-apollo-interface.mjs`; `./scripts/verify-apollo-interface.test.sh` | **35% of this interface was established by no capture; 0% is Conduit's invention.** It **declares** 41 `OBSERVED` coordinates plus 22 `DOCUMENTED` ones — Apollo names the field or documents the concept, but no Hub answered for that coordinate — and 0 `CONDUIT_NATIVE`; 62 of the 63 are served, the other 1 refuses. Every field the interface serves carries its tier annotation; declaring a coordinate is not a claim to answer it. **`DOCUMENTED` is not an Apollo compatibility claim**: it says Apollo named the thing, not that a Hub would answer for it. Every citation is verified against the pinned OpenAPI, the documentation manifest, or the recovery record, and the gate fails the build if a capture recovers one of these coordinates or if Apollo publishes a name the interface is not using |
| Delivery, governance, reporting, and bundles | Go tests named in the [contract notes](contracts/delivery.md) | Conduit-native behavior that is Apollo-aligned only where a cited public contract covers it |

## Contract-first completion gate

The OpenAPI transport boundary is generated and active. Completion of the wider
context extraction still requires all of the following:

1. A pinned OpenAPI input produces deterministic DTOs, a strict server
   interface, and router bindings; supported client consumers use a generated
   client rather than copied types.
2. Regeneration reproduces committed bytes without network access.
3. The HTTP adapter implements the generated server interface rather than a
   parallel handwritten route contract.
4. Domain behavior is reached through narrow application ports; generated
   transport types do not enter Delivery, Governance, Reporting, or storage.
5. Frontend or SDK consumers import generated client types instead of declaring
   their own copies.
6. Unknown fields and union variants follow the published rule. If the rule is
   absent, translation fails closed.

## Explicit gaps

- Apollo-to-Conduit translation and duplicate legacy projection structs remain
  in `internal/controlplane`; moving them behind neutral ports is separate work.
- No supported client consumer currently requires a generated client package.
- Native Apollo CLI byte evidence has no captured tenant output in the
  committed fixture set.
- The Hub lease, heartbeat, cancellation, result, and reported-state protocol
  is unpublished and unsupported.
- Public examples and private observations do not authorize guessed defaults
  or a wider compatibility label.
- The observed GraphQL schema covers only what its candidate set named. Its
  three unemittable fields, the absence of any mutation surface, and the fact
  that no field's cardinality was established, are recorded in the completeness
  report rather than filled in.
- One capture is not the surface. A prior capture of a Foundry GraphQL gateway
  contains no Apollo deployment domain at all, while this recovery found one.
  The conflict is recorded in `reference/graphql-api.md` with its three
  unresolved readings; it bounds what may be inferred about Apollo's GraphQL
  surface in general, not what this Hub answered.
- The interface Conduit serves is larger than the schema it recovered, and says
  so per field. Declaring an Apollo-shaped coordinate is not a claim to answer
  it: 1 recovered coordinate is declared and refused, because Conduit holds
  no namespace data behind it. Current refusals are enumerated in
  [ADR 0007](../decisions/0007-registered-invention-over-refusal.md),
  which `TestRefusedCoordinatesMatchTheDecisionRecord` pins against the resolver.

The [evidence index](evidence.md) records the inputs. A green evidence check
proves only the claim attached to that check.
