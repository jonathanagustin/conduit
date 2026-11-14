# Preparing Conduit's own Products

Issue #193's preparation slice renders reviewable Hub and Spoke-agent Product
publication inputs from the existing release producer's immutable descriptors.
It also inventories the six CLI archives and binds their actual local bytes to
the checksum index and matching provenance metadata. This performs no network
request, publication, signature creation, subscription or deployment.

## Inputs and invocation

Use assets from one already-published stable release: `platform-oci-manifest.json`,
`platform-helm-charts.json`, CLI `SHA256SUMS`, the six archives and their six
`.slsa.json` provenance metadata files. Put archives and provenance metadata in
separate local directories. The version and full original source commit must
agree across the supplied descriptors. For historically corrected Git history,
retain the source SHA carried by the signed release inputs; the history SHA map
establishes source-tree equivalence, not a new signature over rewritten history.

```sh
node scripts/prepare-self-products.mjs \
  --repository jonathanagustin/conduit \
  --version 0.17.5 \
  --oci-manifest /review/platform-oci-manifest.json \
  --helm-manifest /review/platform-helm-charts.json \
  --cli-checksums /review/SHA256SUMS \
  --cli-archives /review/archives \
  --cli-provenance /review/provenance \
  > /review/self-products.json
```

The preparer reads regular CLI files only, rejects missing/duplicate targets,
mutable or mismatched coordinates, crossed component/architecture receipts,
changed archive bytes and inconsistent provenance metadata. It emits no partial
publication inputs on failure. Identical inputs produce identical output.

## Native preparation format

The output is explicitly `CONDUIT_NATIVE` and `NOT_PUBLISHED`, registered as
`releaseprep.SelfProductDescriptors`. Product coordinates are
`io.github.jonathanagustin.conduit:hub:VERSION` and
`io.github.jonathanagustin.conduit:spoke-agent:VERSION`. These are Conduit choices.
The bodies use the existing `/v1/product-publications/helm-chart` transport;
generated public Apollo interfaces are unchanged.

Chart digests and image digests come from the supplied release descriptors.
Conduit's executable Artifact type also needs a display tag, so the preparer
uses the producer's component-version tag convention while keeping the supplied
digest authoritative. It does not query a registry or establish tag equivalence.
The exact same image reference is pinned in the default Helm configuration and
its schema. Stable channel membership is the catalogue's existing
`DEV`/`RELEASE_CANDIDATE`/`RELEASE` default, verified through the actual Hub.

Consumers supply their own Environment, namespaces, PostgreSQL, identity, trust,
signing and RBAC configuration under the chart's existing constraints. The
preparer supplies no tenant credentials or deployment targets. Input SBOM and
scan receipts remain receipts; they do not become an unperformed vulnerability
or availability assessment.

## CLI and remaining boundaries

The CLI section is `DOWNLOAD_INVENTORY_ONLY`. Its archive checksums and source,
target, version and builder metadata are checked locally. The current service
Product API accepts dependency declarations but does not retain CLI archive
assets. An inventory is not a published CLI Product, an installer or a new Agent
execution path. Completing that catalogue capability remains part of #193;
this preparation slice does not close it.

Signatures, attestations, registry availability, actual release readiness and
tenant-specific configuration still need their existing independent checks.
The output records signature verification and availability as unperformed.
Publication, subscription, rollout policy and deployment require a separate
reviewed manual action. This tool does not change the release workflows or
activate an automatic self-upgrade controller.

The revision-skew, data and rolling replacement rehearsals under #263 remain
separate evidence. This preparer's fixtures prove document import, not a new
runtime upgrade or cluster-readiness result.

## Verification

`node scripts/prepare-self-products.test.mjs` checks deterministic output,
metadata/byte failures, the CLI entry point and absence of partial output.
`TestPreparedSelfProductsImportThroughTheNativeHub` imports the synthetic bodies
through real Hub authorization/publication and checks exact executable Releases,
configuration receipts and channel semantics. The synthetic fixture is labelled
under `testdata/self-product/`; it contains no released artifacts.
