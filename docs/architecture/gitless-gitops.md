# Gitless GitOps

Production clusters consume Conduit deployment sources from a private OCI
registry. Git remains the authoring and review system, but it is not a runtime
dependency and no Git credential is installed in a production cluster.

## Trust chain

The deployment path is deliberately split into separate decisions:

1. A person dispatches the publication workflow for an exact semantic release
   tag. A merge does not publish anything.
2. The workflow packages the reviewed Flux source reproducibly, obtains the
   registry-reported digest, and signs that digest with GitHub's OIDC identity.
3. The release descriptor records the source commit, semantic version, exact
   OCI reference, digest, certificate issuer, and certificate identity.
4. A cluster owner supplies a read-only private-registry credential or an
   approved workload identity. The credential is not stored in this repository.
5. Flux pulls the source by digest and verifies the Cosign signature against the
   expected Conduit repository, workflow, and semantic tag identity.
6. Flux applies only the bounded Conduit runtime resources under the named
   reconciliation service account.

A tag helps a person find a release. The digest is the deployment authority.
Moving a tag does not change an approved Plan or an installed Flux source.

The publisher emits the Conduit-owned
`conduit.delivery/runtime-source/v1` contract. It has five bounded sections:
contract version, kind, stable release version, source repository and commit,
artifact reference and digest, and the Cosign publisher policy. The checked-in
renderer rejects unknown inputs, mutable versions, mismatched reference and
digest pairs, shortened commits, and a signer identity that is not the release
workflow for the same repository and tag.

## Reconciliation boundary

Gitless delivery does not transfer Conduit semantics to Flux. Flux owns
Kubernetes convergence for the Conduit runtime and separately authorized
cluster prerequisites. Conduit owns Product intent, artifact admission, Plans,
approval, leases, rollback intent, and attributed reported state. The Spoke
executes approved tenant work through its Helm adapter.

The direct tenant `HelmRelease` example under `deploy/flux` is retained only as
a bounded migration and bootstrap example. Production PostgreSQL Hubs now run
native Conduit reconciliation, so the example is not an owner of tenant
delivery.

## Ports and adapters

Gitless GitOps is an adapter choice around Conduit-owned ports. Domain and
application packages must not import Flux, Kubernetes, Helm, ORAS, Cosign, or a
registry client.

The Artifacts and Delivery contexts need narrow capabilities expressed in
their own language:

- resolve an admitted immutable coordinate to a descriptor;
- verify that content and publisher evidence satisfy an admission policy;
- fetch or restore bytes by digest into a bounded cache;
- execute an approved release under a lease; and
- return exact observations and receipts.

Private OCI/ORAS, Cosign, the local content cache, Helm, and cluster observation
implement those ports as outbound adapters. The declarative resources under
`deploy/flux` are an external deployment adapter for the Conduit runtime. They
do not become domain models, and the Hub does not call Flux as an application
service.

Do not add an interface until an application service consumes it. The next
runtime slice should place ports beside the owning application package, keep
registry and Helm implementations under adapter packages, and wire them only
from the Hub or Spoke composition root. This avoids creating unused abstraction
layers while preserving the required dependency direction.

## Private registry and disk pressure

The private OCI registry is durable artifact storage. Files extracted under a
workspace, `/tmp`, or a node cache are reconstructible copies. Local eviction
is safe only after the exact remote digest has been read back and an independent
restore has reproduced the expected bytes. Durable Hub state, audit records,
and unpublished source are not caches and must never be deleted as part of OCI
offload.

Disconnected environments mirror the same digest and verification material
through the approved transfer path. A mirror changes the registry location,
not the admitted content identity.

## Implemented and verified

The repository provides a manually dispatched publisher, keyless
runtime signing, a digest-bearing release descriptor, digest-pinned Flux
sources, private pull-secret references, signer-policy examples, and structural
gates for those properties. The descriptor renderer has one accepted fixture
and rejection cases for a digest mismatch, wrong signer, and mutable version.

`scripts/test-gitless-clean-cluster.sh` closes the runtime proof with a
disposable authenticated Zot registry and a clean k3d cluster. It installs only
Flux's OCI source and Kustomize controllers, gives the cluster a synthetic
pull-only credential and one trusted public key, reconciles the exact signed
digest, rejects byte-identical content signed by an untrusted key, and proves
that neither Git credentials nor `GitRepository` objects exist. Its toolchain
is checksum-pinned, its credentials and signing keys are test-only, and its
workflow is manually dispatched so the cluster cost is never incurred on a
push or pull request.
