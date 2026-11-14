# Preparing Conduit's own Products

Issue #193's preparation slice renders reviewable Hub and Spoke-agent Product
publication inputs from the existing release producer's immutable descriptors.
It also prepares a native CLI download-asset publication declaration and binds the six archives' actual local bytes to
the checksum index and matching provenance metadata. This performs no network
request, publication, signature creation, subscription or deployment.

## Inputs and invocation

Use assets from one already-published stable release: `platform-oci-manifest.json`,
`platform-helm-charts.json`, CLI `SHA256SUMS`, the six archives and their six
`.slsa.json` provenance metadata files. Put archives and provenance metadata in
separate local directories. Each stable version component must fit the catalogue's unsigned 32-bit ordering
range. The version and full original source commit must
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
`releaseprep.SelfProductDescriptors`, with preparation format version 2. Product coordinates are
`io.github.jonathanagustin.conduit:hub:VERSION` and
`io.github.jonathanagustin.conduit:spoke-agent:VERSION`. The CLI coordinate is
`io.github.jonathanagustin.conduit:cli:VERSION`. These are Conduit choices.
The bodies use the existing `/v1/product-publications/helm-chart` transport;
generated public interfaces are unchanged.

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

The CLI section is a `DOWNLOAD_ASSET_DECLARATION` with a publication body for
`POST /v1/product-publications/download-assets`. It contains six immutable
archive declarations, exact byte sizes and SHA-256 hashes, OS/architecture,
the original release source commit and producer-convention signature/provenance
bundle URLs. Local archive bytes and source/target/version/builder metadata are
checked. The signature bundle URLs are declared coordinates, not proof that a
signature or registry check ran.

The native Hub stores this declaration independently of delivery snapshots and
binds published reads to a matching immutable catalogue receipt. It requires
the existing artifact/Product/Release publication operations; selection of a
readable Space grants no additional permission. A CLI Product remains
`CATALOGUED_NOT_EXECUTABLE` and `PUBLISHER_DECLARED`, with no Agent Release.

The native CLI supports `product-release download-assets publish FILE`,
`get PRODUCT VERSION` and `list PRODUCT`. Publish accepts one strict JSON
document (`-` reads stdin). `--dry-run` validates and renders locally without
contacting the Hub. `--space-id` is forwarded on publication. For review:

```sh
jq '.cli.publication.body' /review/self-products.json > /review/cli-downloads.json
conduit product-release download-assets publish /review/cli-downloads.json --dry-run
```

An actual publication needs the separately reviewed manual action below.
Preparation format version 1's recorded inventory remains historical evidence;
it is not retroactively treated as a catalogue-ready CLI Product.

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
through real Hub authorization/publication and checks exact Hub/Spoke executable
Releases, configuration receipts, channel semantics and a non-executable CLI
catalogue declaration. The synthetic fixture is labelled
under `testdata/self-product/`; it contains no released artifacts.

## Recorded v0.17.5 preparation (version 1)

The actual release assets were read on 2026-11-14. Both native Product bodies
and all six CLI archive byte/metadata checks passed. The prepared documents are
in `docs/evidence/self-product-preparation-v0.17.5.json`; the adjacent receipt
records input metadata hashes, the tool revision and the exact descriptor hash.
No archive or binary is committed.

The source SHA is the original released `44e1e8845fe01c2a461061f6557a128ebf716af6`,
retained from the release metadata. The corrected tag points to
`92e0c6f6258b68f7bebf5da04978ac54ec337844`; the history map records the prior
tree-preserving correction. This is not a new attestation over rewritten history.
Signatures, registry availability, vulnerability reassessment, live publication
and deployment were not performed. The synthetic test fixture remains separate
from this actual-release preparation.

## Recorded v0.17.5 download declaration (version 2)

The same retained release inputs were rechecked on 2026-11-14 with the native
CLI catalogue preparer. The nine input metadata hashes still match the original
acquisition receipt, all six archive byte checks passed, and the original
released source SHA is preserved. Version-2 documents are recorded separately
in `docs/evidence/self-product-preparation-v0.17.5-downloads.json`; the adjacent
`-downloads-receipt.json` pins their hash, the tool commit and script hash.
This preparation adds the CLI publication body without changing the historical
version-1 record. No live publication, signature authentication or deployment
was performed.
