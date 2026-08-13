# Conduit bootstrap and distribution

## Decision

`conduit` is Platform's only supported public operator CLI. The Hub, Spoke,
optimizer, and reconciliation binaries remain internal runtime components.
Platform distribution therefore has two deliberately separate phases:

1. **local bootstrap**, before a Hub exists, verifies and installs an immutable
   Platform release; and
2. **governed operation**, after the Hub exists, uses authenticated Conduit API
   commands, Change Requests, and Plans.

Palantir's public documentation says that `apollo-cli` is downloaded from an
existing Hub UI and documents `environment manifest generate` for installing a
Spoke. It does not document how Palantir installs the first Apollo Hub. The
bootstrap design below is consequently Platform-native and makes no Apollo Hub
installation compatibility claim.

## Distributed artifacts

A semantic Platform release must be usable without an existing Hub and bind:

- the `conduit` archives for Linux, macOS, and Windows on amd64 and arm64;
- the independently installable Hub and Spoke Helm chart OCI digests;
- the multi-architecture Hub and Spoke image-index digests;
- Git commit, source provenance, signatures, SBOMs, vulnerability results, and
  supported architectures; and
- the signed Zarf bundle carrying the same semantic release for disconnected
  installation.

Tags are discovery hints only. Bootstrap accepts the release manifest and its
exact registry-reported digests; it never resolves a mutable tag into authority.

## Intended workflow

```text
conduit hub install --release-manifest RELEASE.json --values hub-values.yaml \
  --namespace platform-system
conduit hub verify --receipt HUB-RECEIPT.json

conduit environment create ENVIRONMENT_NAME ...
conduit environment manifest generate ENVIRONMENT_ID ... > spoke-list.yaml
conduit spoke install --manifest spoke-list.yaml \
  --namespace ENVIRONMENT_NAMESPACE
conduit spoke verify --receipt SPOKE-RECEIPT.json
```

`hub install` and `spoke install` are local bootstrap commands. They do not
pretend to call a Hub that does not exist. They preflight an existing Namespace,
identity material, storage selection, chart/image correspondence, architecture,
and least-privilege RBAC; then invoke the same Helm release boundary CI tests.
They do not create credentials, storage classes, application Namespaces, CRDs,
cluster-admin, or Product delivery RBAC.

Every dry run emits a deterministic secret-free proposal receipt. Every
mutation emits an immutable receipt binding the semantic release, chart/image
digests, values digest, target cluster identity, namespace, Helm revision, and
observed rollout evidence. Verification proves TLS/mTLS and exact revision,
then exercises Hub retained-state recovery or Spoke outbound identity and
reported-state freshness as appropriate. Helm success alone is insufficient.

## Flux exit

Flux is currently a reviewed runtime distribution adapter. It has no Plan,
approval, health, or execution authority, but it is still active and must not be
deleted before a replacement exists. Exit requires one migration E2E that:

1. imports exact currently deployed Hub/Spoke coordinates and configuration;
2. proves a Conduit receipt equivalent to the imported state;
3. transfers reconciliation ownership without changing Product desired state;
4. upgrades and rolls back through Conduit using the same immutable release;
5. disables Flux reconciliation; and
6. proves a rollback path that restores the prior owner without dual writers.

Only after that gate passes may the reusable Flux manifests and CI verifier be
removed. Fleet and Argo remain research inputs rather than additional runtime
controllers; their hub/spoke and inventory strategies may be adapted as code,
but Platform retains one owner for reconciliation and one Hub authority model.

## Acceptance gate

Issue [#212](https://github.com/jonathanagustin/palantir/issues/212) tracks the
implementation. The closing E2E starts with only a Kubernetes cluster and a
downloaded, verified `conduit`; installs and recovers the Hub; creates an
Environment; installs its outbound Spoke; executes an approved exact-digest
Product Plan; verifies deep health; upgrades; rolls back; and repeats using the
air-gapped bundle with semantically equivalent receipts.
