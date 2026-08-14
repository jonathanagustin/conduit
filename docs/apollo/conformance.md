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
| Public API v0 routes | Pinned OpenAPI snapshot; `scripts/verify-apollo-routes.sh` | The generated router table contains exactly the 13 operations in the snapshot |
| Public API v0 payload behavior | `scripts/apollo-docs/verify-public-api-adapter.rb`; `go test ./internal/controlplane -run TestApolloPublicAPI` | The named adapter implements the tested route and envelope profile |
| Published Apollo Agent configuration types | Pinned generated OSDK evidence; `go test ./internal/foundryinterop` | Structural compatibility with the named generated types, not a private service protocol |
| CLI Product fixtures | `scripts/apollo-docs/validate-cli-fixtures.rb`; byte comparison fixture | Semantic validation of the reconstructed 0.814.0 shape; byte compatibility is not claimed without a native capture |
| Module examples | `scripts/apollo-docs/validate-module-definitions.rb` | The committed examples satisfy the reviewed public Module constraints |
| Delivery, governance, reporting, and bundles | Go tests named in the [contract notes](contracts/delivery.md) | Conduit-native behavior that is Apollo-aligned only where a cited public contract covers it |

## Contract-first completion gate

The OpenAPI boundary is incomplete while only routes are generated. Completion
requires all of the following:

1. A pinned OpenAPI input produces deterministic DTOs, a strict server
   interface, a client interface, and router bindings.
2. Regeneration reproduces committed bytes without network access.
3. The HTTP adapter implements the generated server interface rather than a
   parallel handwritten transport model.
4. Domain behavior is reached through narrow application ports; generated
   transport types do not enter Delivery, Governance, Reporting, or storage.
5. Frontend or SDK consumers import generated client types instead of declaring
   their own copies.
6. Unknown fields and union variants follow the published rule. If the rule is
   absent, translation fails closed.

## Explicit gaps

- The OpenAPI generator emits routes only. DTOs and strict client/server
  interfaces are still handwritten.
- Native Apollo CLI byte evidence has no captured tenant output in the
  committed fixture set.
- The Hub lease, heartbeat, cancellation, result, and reported-state protocol
  is unpublished and unsupported.
- Public examples and private observations do not authorize guessed defaults
  or a wider compatibility label.

The [evidence index](evidence.md) records the inputs. A green evidence check
proves only the claim attached to that check.
