# Apollo evidence index

Evidence in this directory is immutable input to a bounded claim. It does not
become product architecture, private protocol documentation, or permission to
infer missing behavior.

## Pinned public contracts

- [Apollo OpenAPI snapshot](snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/README.md)
- [Public documentation coverage](PUBLIC-DOCUMENTATION-COVERAGE.md) and
  [hashed manifest](public-documentation-manifest.tsv)
- [Source-derived topic index](reference/README.md)
- [OSDK Conjure generation record](OSDK-CONJURE-GO-GENERATION.md) and
  [pinned OSDK evidence](osdk-client-unstable-2.43.0-conjure-evidence.json)
- [CLI command contract](cli-command-contract.json) and
  [CLI fixture protocol](cli-fixtures/README.md)

## Sanitized or derived observations

- [Apollo client DLL evidence](APOLLO-CLIENT-DLL-EVIDENCE.md),
  [receipt](apollo-client-dll-receipt-2026-08-02.json), and
  [Java object closure](apollo-java-object-closure-2026-08-02.json)
- [Frontend CAS closure capture](FRONTEND-CAS-CAPTURE.md) — the served
  `apollo-control-panel` 6.533.15 bytes. Read its Finding 1 before mining it:
  the app is Foundry **Control Panel** under an Apollo skin, so it authorizes
  nothing about the Apollo deployment console. With it, the API surface that
  closure calls — [438 Conjure REST endpoints](apollo-control-panel-rest-endpoints-6.533.15.json)
  across 34 services and [24 GraphQL operations](apollo-control-panel-graphql-operations-6.533.15.json),
  both recovered statically from the bundles and both **bundle-derived, not
  type-verified against a live response**. Not one of the 438 is a deployment
  endpoint, which is Finding 1 established from the wire instead of the strings.
- [Foundry gateway observed-contract schema](foundry-gateway-observed-schema-6.533.15.graphql)
  and its [completeness report](foundry-gateway-observed-schema-completeness-6.533.15.json)
  — the same 24 operations projected into SDL, with live validation-only field
  and argument signatures. Ten `Query` roots, nine `Mutation` roots, and the
  string `apollo` zero times in 383 lines. Read it with
  [`reference/graphql-api.md`](reference/graphql-api.md), which files it against
  the unresolved captured-gateway conflict rather than as a resolution of it.
- [Maven inventory evidence](APOLLO-MAVEN-INVENTORY-EVIDENCE.md)
- [Java object alignment](JAVA-OBJECT-ALIGNMENT.md)
- [Role/interface inventory](ROLE-INTERFACE-INVENTORY.md)
- [Foundry audit.3 evidence](FOUNDRY-AUDIT3-EVIDENCE.md)
- [Demo bundle evidence](DEMO-BUNDLE-EVIDENCE.md)

## Public media review

- [Media audit](MEDIA-AUDIT.md)
- [Image-derived gaps](IMAGE-DERIVED-GAPS.md)
- [Third-party evidence manifest](third-party/evidence-manifest.json)

Use the source class recorded in each file. A public schema can authorize a
public transport shape. A sanitized tenant receipt can authorize only its
observed tenant, version, and behavior. Corroborating evidence can support a
claim but cannot create one. Missing evidence leaves the behavior unknown.
