# Artifacts and releases

Conduit identifies release inputs by content, then carries that identity
through admission, delivery, and audit. A tag is a human selection aid. The
digest is the artifact identity used by a Plan.

## Artifact identity

An executable image, chart, Bundle layer, solver binary, SBOM, signature, or
attestation must be tied to an immutable digest. A semantic version may point
to that digest, but it cannot replace it in an execution record. The same name
and version with different bytes is a different artifact.

[`internal/artifacts`](../../internal/artifacts/) owns signed Bundle envelopes.
The envelope binds its manifest, resources, layer digests, lineage, security
report, and source-format note. Payloads remain opaque to that context. A
consumer verifies the digest and signature before interpreting the payload.

Snapshot, incremental, and checkpoint Bundles have different lineage rules.
An incremental Bundle names its dependency. A checkpoint collapses a chain
into a complete state. Missing lineage is not repaired by guessing which prior
Bundle the sender intended.

## The deployable artifact and its kind

A Release names one deployable artifact, in the field `chart`. The field keeps
the name of the only kind that existed when it was introduced; `chart.kind` says
what the artifact is:

| `kind` | Reference | Executed by |
| --- | --- | --- |
| absent or `helm-chart` | `repository`, a non-`latest` `tag` and `digest` | Helm, in the Spoke |
| `zarf-package` | `repository` and `digest`; no tag | `zarf package deploy` |
| `uds-bundle` | `repository` and `digest`; no tag | `uds deploy` |

A Release with no `kind` is a Helm chart. Its document and its Release digest
are byte-for-byte what they were before the field existed, so nothing already
stored or approved changes meaning.

A `zarf-package` or `uds-bundle` also declares:

- `architectures`: exactly one target architecture, `amd64` or `arm64`.
  Multiarchitecture declarations are refused before a Plan or executor command
  can run; platform selection from an OCI index is not supported.
- `packageName`: the Zarf package name (`metadata.name` in `zarf.yaml`). Zarf
  records a deployment under that name only, so this is how the Spoke finds
  what it deployed. For a bundle it names the bundled package the Spoke
  observes.

```json
"chart": {
  "kind": "zarf-package",
  "repository": "registry.example/packages/excalidraw",
  "digest": "sha256:…",
  "architectures": ["amd64"],
  "packageName": "excalidraw"
}
```

The Hub refuses, at proposal, a package artifact that has no sha256 digest, that
carries a tag beside the digest, or whose `repository` embeds a tag or digest. A
tag names whatever the registry currently holds. Helm rollout settings
(`rolloutStrategy`, `resourceReadinessTimeoutSeconds`,
`imagePrePullTimeoutSeconds`) are refused on such a Release, because nothing
would apply them. `images[]` never carry a kind.

The Plan step carries the whole artifact. The CLI Plan table has an `artifacts`
column, and the JSON and YAML bodies carry every field. The console shows the
kind, full repository and digest, architecture and package name in both the
Plan inspector and the newest release in Products and versions. These are
declared coordinates, not evidence that an artifact is deployed.

When selecting a package, the native desired-state Environment must declare
`packageArchitecture: amd64` or `packageArchitecture: arm64`. This field belongs
only to that native document, not an Apollo API or Environment profile. This is a
Conduit-native operator declaration, not discovered cluster inventory. A
pinned incompatible Release is refused at proposal; channel selection excludes
incompatible Releases and can select an older compatible one. A catalog may
still contain packages for other targets. Helm-only deployments need no
declaration. The Spoke independently checks its configured architecture before
running a package command, so an incorrect operator declaration grants no
execution authority. Ineligibility reason `PACKAGE_ARCHITECTURE` is also
Conduit-native.

`digest` must name the platform manifest for that one architecture. An OCI index
digest is unsupported. A false declaration of one architecture is not verified
by a registry read during planning: if Zarf resolves a different digest, exact
post-deployment readback still fails and invokes rollback. No platform digest
is inferred from a tag, rewritten into a reviewed Release, or accepted merely
because a deployment command succeeded.

These fields are Conduit-native and registered as
`catalog.PackageArtifactKind` and `environments.PackageTargetArchitecture` in
[`conduit-inventions.json`](../evidence/conduit-inventions.json). The Apollo
manifest publication routes still produce Helm Releases only.

## OCI release record

A publish operation should produce one immutable release record containing:

- semantic version and source revision;
- registry-reported manifest or index digest;
- per-platform image manifests where applicable;
- chart and executable digests;
- SBOM digests;
- signature and attestation references; and
- the exact verification results used for admission.

Publishing is a separate authorized action. A merge or a green test does not
publish, promote, or deploy anything. Repository workflows are manually
dispatched so the operator chooses when paid CI and publication run.

The Gitless runtime publisher emits
`conduit.delivery/runtime-source/v1`. Its renderer validates the exact private
OCI reference and digest, source repository and commit, stable semantic
version, and expected Cosign certificate issuer and identity before writing the
descriptor. Flux configuration remains an adapter; the descriptor is a
Conduit-owned contract.

## Storage and disk pressure

Content-addressed blobs may be offloaded to a private OCI registry so local
disk is a cache rather than the only copy. A safe offload sequence is:

1. compute and verify the local digest;
2. publish to the private repository;
3. read the registry descriptor back and compare its digest;
4. record the immutable remote reference and restoration metadata;
5. prove a separate restore can reproduce the local digest; and
6. only then permit local cache eviction under an explicit retention policy.

Credentials remain external and are supplied by reference. They are never
written into a manifest, receipt, log, or repository file.

The current tree has content-addressed Bundle and evidence stores plus OCI
release workflows. It does not yet prove an automatic, end-to-end offload and
restore path for all durable Hub state under disk pressure. Until that path is
implemented and tested, local deletion must not be presented as safe offload.

## Admission rule

Before a digest enters an executable Plan, Conduit must verify that the
artifact is the admitted artifact, that required security evidence covers that
exact digest, and that the target architecture is present. Mutable references,
missing evidence, or an unavailable registry fail closed. Candidate optimizer
artifacts have additional admission gates described in
[Optimization](optimization.md).
