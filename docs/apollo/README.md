# Apollo integration documentation

Apollo is a bounded integration context in Conduit. It owns the public Apollo
vocabulary that Conduit can prove, the evidence behind each compatibility
claim, and translation at the edge of Conduit's native domains. It does not own
delivery policy, governance, execution, persistence, or the Spoke lifecycle.

Conduit currently implements a bounded public-profile adapter. The pinned
OpenAPI snapshot generates the 13-route registration table, while request and
response types and the server handlers remain handwritten. The contract-first
target is stricter: the pinned document generates DTOs, strict server and
client interfaces, and the router; an adapter then translates those generated
types through narrow native ports. Until that target lands, the compatibility
claim stays limited to the routes and payload behavior covered by the current
tests.

## Read this set

| Document | Purpose |
| --- | --- |
| [Bounded context](bounded-context.md) | Ownership, dependency direction, and the extraction sequence |
| [Public API](public-api.md) | Pinned OpenAPI profile, current implementation, and generation target |
| [Conformance](conformance.md) | Claims that are supported now, their gates, and open gaps |
| [Private interop](private-interop.md) | Fail-closed boundary for unpublished Hub-to-Agent behavior |
| [Delivery contract](contracts/delivery.md) | Conduit-native Plans, rollout intent, and execution handoff |
| [Governance contract](contracts/governance.md) | Authorization, review, and audit ownership |
| [Reporting contract](contracts/reporting.md) | Desired-versus-observed fleet projection |
| [Bundle contract](contracts/bundles.md) | Conduit-native disconnected transfer format and admission rules |
| [Evidence index](evidence.md) | Pinned public inputs, sanitized observations, and immutable receipts |

The source-derived topic notes under [reference](reference/README.md), the
[OpenAPI snapshot](snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/README.md),
the [CLI fixtures](cli-fixtures/README.md), and the immutable evidence records
remain inputs. They are not product architecture documents.

## Local checks

Run these from the repository root:

```bash
ruby scripts/apollo-docs/validate-public-api.rb \
  docs/apollo/snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml
scripts/verify-apollo-routes.sh
ruby scripts/apollo-docs/verify-public-api-adapter.rb
ruby scripts/apollo-docs/validate-cli-fixtures.rb
ruby scripts/apollo-docs/validate-module-definitions.rb
```

The documentation inventory and media audit tools also live under
[`scripts/apollo-docs`](../../scripts/apollo-docs). They fetch public material
and should be run deliberately; their output does not expand a compatibility
claim without review and a matching executable gate.
