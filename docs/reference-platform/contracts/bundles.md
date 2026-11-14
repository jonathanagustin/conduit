# Bundle contract

Conduit Bundles move reviewed delivery state and exact artifacts between Hubs,
including disconnected environments. The format is Conduit-native. It does not
read or write an unpublished Palantir Apollo bundle format.

## Source-side rules

- An Export Pipeline has immutable numbered revisions and explicit active,
  archived, and deleted lifecycle behavior.
- Selection is type-scoped and dependency closure fails closed when a required
  resource or artifact is absent.
- A build emits a signed manifest over deterministic resource identities and
  exact content digests.
- Incremental lineage begins with a self-contained snapshot. A checkpoint is
  also self-contained and does not silently replace the next incremental
  baseline.
- Scheduled builds use durable single-flight claims for a Pipeline revision and
  scheduled minute.
- Preview computes a diff without signing, creating a Bundle, or advancing
  lineage.

## Target-side rules

- Preview and admission verify the signing key, signature, manifest digest,
  resource digests, resource identity, lineage, and supported schema.
- Artifact layers are reconstructed only after their ordered chunk identities
  and exact digest validate.
- Admission is staged and records ordered task evidence. Failures carry bounded
  coordinates, not artifact bodies or credentials.
- Target-specific transfer receipts may suppress bytes only for the target and
  scope they prove; receipts are durable and rescindable.
- Relayed Environment reports are admitted only when the target Environment is
  configured to accept that state source, and the original observation time
  and attribution are retained.

The current model, service, and repository ports are in
[`internal/controlplane/bundles.go`](../../../internal/controlplane/bundles.go).
Extraction into Artifacts and Federation remains part of the broader DDD
migration.

## Verification

```bash
go test ./internal/controlplane -run 'TestBundle|TestExportPipeline|TestScheduledExport'
```

These tests establish the Conduit format and its admission behavior. Similarity
to Apollo Export Pipelines or Bundles is alignment only where a pinned public
document states the same semantic.
