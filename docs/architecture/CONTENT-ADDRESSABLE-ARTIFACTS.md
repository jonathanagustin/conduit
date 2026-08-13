# Content-addressable artifacts

The platform separates logical release identity from byte identity:

```text
Module/Product version -> immutable release record -> sha256 descriptors -> bytes
                                              |
                                              +-> signatures, SBOMs and scans
```

`group:product:version`, Module versions, Release Channels, and tags are human-
or policy-facing references. A lowercase `sha256:<64 hex>` digest identifies
raw bytes. Execution Plans contain the computed Product Release digest plus the
exact chart and image digests. The Spoke reports the same Release digest after
observing the deployment, preventing a tag or catalog mutation from silently
changing what an approved Plan means.

The built-in blob API is intentionally narrower than an OCI registry:

- `POST /v1/artifacts/blobs` computes the digest while streaming the body.
- `PUT /v1/artifacts/blobs/{digest}` rejects the body unless its bytes match.
- Repeated uploads of identical bytes are idempotent.
- `GET` and `HEAD` expose size, media type, ETag, and
  `Docker-Content-Digest`.
- Filesystem paths derive only from validated digest text and are immutable.

The current single-replica filesystem backend is suitable for local and Spoke
bootstrap use. Production object storage should preserve the same interface
with conditional create-if-absent writes, strong read-after-write semantics,
independent digest verification, reference leases, and mark-and-sweep garbage
collection. Metadata and reachability belong in a transactional database; S3
object keys should follow `cas/v1/sha256/<first-two>/<hex>`.

Helm charts and images remain OCI objects in ECR or another assigned registry.
OCI descriptors already form a Merkle DAG of manifests, configs, and layers.
The platform records those external digests and verifies them at execution; it
does not duplicate every container layer into the Hub's local blob store.
