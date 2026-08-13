# Artifact registry and secret contracts

Sources are indexed in `review-ledger.tsv`. These notes preserve public
semantics without copying upstream page bodies or treating private protocols as
available contracts.

The FedStart Keycloak example is pinned at `palantir/fedstart-helm-charts`
commit `1d27354123f147c6b8840e8a286915f910bad489`; its README content SHA-256 is
`b34d70704ba73a2c7a08404e9c6595ebbbb9f8fe917b5200a5795ae899df7207`.

## Artifact registries

- A registry is a revisioned Environment assignment with a unique runtime URL.
  It may represent the source registry itself or a deterministic mirror.
- Registry authentication may be absent or externally resolved. Credential
  values are never readable from the control-plane object or Plan.
- A Plan rewrites each chart and image repository to its assigned runtime
  registry while preserving path, tag, and digest. It carries the assignment
  ID/revision, source/runtime hosts, an opaque credential reference, and the
  Kubernetes image-pull Secret name.
- The Spoke prepares registry access before any Helm command. Missing or failed
  preparation fails closed and is attributable to the immutable assignment
  revision.

## Secrets

- Secret values become write-only immediately. The Hub may retain requirements,
  names, metadata, and opaque provider references, but it must never make
  plaintext retrievable or embed it in a Plan.
- Values are resolved only in the target Environment through an externally
  owned provider and the Agent's Environment-scoped identity. Apollo's public
  model additionally exposes key metadata and lifecycle operations while
  withholding values.
- Create, replacement, and deletion are governed changes. Credential updates
  replace a revision rather than mutating an already authorized value in place.
- A Product configuration can map an opaque secret requirement to a Kubernetes
  Secret reference. Rotation must produce an attributable Plan without sending
  the value through the Hub.
- FedStart charts resolve Kubernetes Secret names with
  `{{ preprocess .Values.apollo.secrets.<assignment>.k8sSecretName }}` during
  Apollo preprocessing. Charts select a key from that Secret for a volume,
  environment variable, or application-specific reference. The Helm metadata
  contains names and declared key metadata only, never provider
  references, keys' values, or credentials; an unknown assignment fails.

## Local evidence and boundary

- `internal/controlplane/plan.go` resolves mirror coordinates and emits only
  opaque `RegistryAccess`; `configuration.go` injects the runtime registry and
  pull-Secret name into the authoritative Apollo Helm block.
- `internal/agent/helm.go` invokes an Environment-side `RegistryPreparer` before
  Helm. Control-plane and Agent tests prove fidelity, non-disclosure, and
  fail-closed ordering.
- `internal/controlplane/model.go` binds Entity requirements to revisioned
  Environment assignments. The revision participates in the configuration
  digest, and Change Requests expose only reviewable names, revisions, target
  Secret names, and key names.
- `internal/agent/material.go` is the provider-neutral executable adapter. It
  sends one create/verify/replace/delete mutation per assignment and namespace,
  with versioned metadata on stdin, and discards provider output. The Worker
  runs this once before workload reconciliation and rolls it back after workload
  rollback; Helm does not repeat provider preparation per installation. The
  external provider remains responsible for values; this repository does not
  become a secret store.
