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
