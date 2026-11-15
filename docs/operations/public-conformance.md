# Public-only inbound conformance coverage

Issue [#429](https://github.com/jonathanagustin/conduit/issues/429). This suite
constructs a composed local Hub with invented in-memory Conduit records and
drives its real HTTP handlers. It makes no network calls, cannot select a
remote target, and never connects to a reference-platform Hub.

Run `go test -v ./internal/apollo/conformance`. The full Go race gate also runs
this package. Each case logs its public citation. The source inventory is
[`public-sources.json`](../../internal/apollo/conformance/testdata/public-sources.json).

## Source boundary

REST expectations come only from the unmodified public Apache-2.0 OpenAPI
snapshot at commit `c663874bd7467a01e719d039509b0f206d05fe2c`, SHA-256
`9e52d7fe6edb29c2f8020ac1179c582cdc89163ccb83a8c17dd803c4f651ed80`.
The suite pins the bytes, resolves only local component references, verifies
each citation's operationId line, and rejects an incomplete or duplicate
operation inventory. The validator checks request data and the actual served
response status/body, including required properties and enum values. A harness
control deletes the required Product.id and must be rejected by that same
validator. It does not just decode permissively into generated Go structs.

The upstream document has `description` next to some `$ref` values and invalid
uppercase enum values in examples. Parsing allows only those description
siblings and disables validation of upstream examples. The document bytes are
not edited; runtime request/response schema validation remains enabled. The
synthetic requests do not copy those invalid examples.

The two GraphQL selections were freshly checked against the unauthenticated
[public API Explorer documentation](https://www.palantir.com/docs/apollo/apollo-references/apollo-api-documentation/).
The retrieved page's raw HTML hash is recorded in the inventory; the page itself
remains outside this repository. The excerpts establish these named selections,
not a complete SDL, endpoint pathname, nullability contract or identity mapping.
The suite uses Conduit's existing `/v1/apollo/graphql` mounting, not a claimed
reference-platform endpoint. It does not read the observed SDL, captured client
operations, tenant receipts, CLI captures or mixed-source reference notes.

## Covered REST operations

All 13 operations in the pinned document have a successful local request and
response-schema case. These are transport-shape checks, not all behaviors of
each operation. Publication cases write only the local memory repository; no
registry publication or deployment occurs.

| Operation | Public source | Coverage |
| --- | --- | --- |
| `publishHelmChart` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L595) | Request and successful response schema |
| `publishService` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L633) | Request and successful response schema |
| `listProducts` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L670) | Request and successful response schema |
| `getProduct` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L691) | Request and successful response schema |
| `listReleases` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L719) | Request and successful response schema |
| `getRelease` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L754) | Request and successful response schema |
| `listEnvironments` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L795) | Request and successful response schema |
| `getEnvironment` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L817) | Request and successful response schema |
| `listInstallations` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L846) | Request and successful response schema |
| `getInstallation` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L879) | Request and successful response schema |
| `listNamespaces` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L1010) | Request and successful response schema |
| `putStatusEntitiesInfo` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L918) | Request and successful response schema |
| `putStatusEntitiesHealth` | [Pinned operation](https://github.com/palantir/apollo-openapi/blob/c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml#L961) | Request and successful response schema |

## GraphQL coverage and gaps

| Public example | Covered selections | Status |
| --- | --- | --- |
| `GetEnvironments` | `apollo.environments(pageSize: 100).environments.id` | Covered against a nonempty synthetic environment |
| `GetCurrentUser` | `me.id`, `me.fullName` | Covered; identity values and nullable type choices are explicitly Conduit-native under #644 |

Both public examples now run as positive conformance cases, with no known-gap
skip. The current-user case checks the documented selections; separate identity
tests verify credential-owner binding, delegation and the unavailable full name.
The username is Conduit's resolved identity and `fullName` is null when the
identity carrier has no display-name claim. Schema/response provenance and the
invention register disclose those native choices; the public example establishes
neither those values nor a complete nullable type definition.
The full GraphQL operation universe is unknown because this public page does
not publish it; observed operations do not expand this denominator.

## Not established

OAuth provider compatibility, private wire protocols, tenant-derived semantics,
all failure conditions, pagination exhaustion, binary artifact round trips and
all possible enum branches remain outside this suite's coverage. The public
OpenAPI does not list error HTTP statuses; this suite does not invent them as
public requirements. Other repository tests may constrain Conduit-specific
behavior, but their evidence is not imported into this suite.

The fixture identity carrier is the existing explicit insecure-development
opt-in; its credentials are synthetic. It is not a reference token or an OAuth
authentication-conformance result. Native environment, installation, namespace
and Product identifiers are setup data, not observed platform values.
