# Bundle artifact transport

## Public semantic baseline

- Large Bundle payloads may be split into independently integrity-checked
  chunks and reassembled during import.
- Layer deduplication is destination-specific. A later Bundle may omit a layer
  only after the source records that the same target Hub imported it; that
  knowledge can be rescinded. Different targets therefore receive different
  Bundle contents.
- Bundle security evidence includes vulnerability and virus/malware scanning.

The implementation deliberately does not claim compatibility with Palantir's
private Bundle wire format.

## Platform contract

- `ExportPipeline.includeArtifacts` opts into immutable artifact packaging;
  `chunkSizeBytes` defaults to 4 MiB.
- The signed manifest commits to every layer's digest, media type, size, ordered
  chunk digests, target Hub, and whether prior transfer was confirmed. Chunk
  payloads live in the envelope and are content-addressed by SHA-256.
- Repeated artifact digests and repeated chunks occur once per Bundle.
- An untargeted build is always self-contained. With
  `enableLayerDedup`, only an active, explicit confirmation for an exact
  target-specific Bundle materialized within the preceding seven days permits
  omission. Any-Hub Bundles and Pipelines without deduplication cannot be
  confirmed. Confirmations are pipeline-local unless
  `shareLayerDedupAcrossPipelines` explicitly enables target-scoped sharing.
  Imports fail closed if an omitted layer is not already present at that
  target.
- Imports verify the signature, manifest, chunk digests and sizes, reconstructed
  layer digest and size, then write immutable blobs before staging catalog
  review.
- `requireMalwarePass` requires complete PASS evidence with zero findings and
  exact coverage of every Release artifact digest.

## Executable evidence

`go test ./internal/controlplane -run 'TestBundleArtifacts|TestBundleArtifactExportRequiresExactMalwareEvidence'`
