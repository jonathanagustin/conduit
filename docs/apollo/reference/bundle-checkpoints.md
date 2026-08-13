# Checkpoint Bundle contract

Source identity is recorded in `review-ledger.tsv`. This note paraphrases the
public behavior and bounds the local implementation; it is not a copy of the
upstream page or a private Bundle-format claim.

## Derived contract

- The first build of every incremental Export Pipeline is a self-contained
  snapshot. Later incrementals depend on the preceding incremental lineage and
  contain only changes since that baseline.
- After 10 successful incremental Bundles by default, the pipeline emits a
  checkpoint: a self-contained snapshot of the resources selected at that
  time. Operators may configure a different positive cadence; a high cadence
  trades slower target onboarding for lower source storage use.
- A checkpoint does not advance or otherwise alter the dependency or content
  baseline for the next incremental Bundle.
- A newly added transfer target starts from the most recent snapshot, including
  a checkpoint, then catches up through later incrementals. Checkpoints are
  onboarding artifacts and are not sent to transfer targets that already have
  an established lineage. Import admission treats checkpoints like snapshots.

## Platform implementation and boundary

Incremental pipelines now persist the documented default cadence of 10 when no
cadence is supplied. Signed checkpoint manifests are dependency-free, record
the incremental that triggered them, and are excluded from subsequent delta
baseline calculation. Snapshot/checkpoint import is already supported.

Platform deliberately keeps separate target-specific lineages so artifact
layer confirmation and rescission cannot leak knowledge between disconnected
targets. It has no automatic transfer-target scheduler: `BuildForTarget`
creates/retrieves the target lineage, while the external transport decides
which stored envelopes to move. Therefore the public "do not send checkpoints
to existing targets" routing behavior is a transport integration requirement,
not a claim about a private Apollo scheduler.

The Target Hub versions page was rechecked on 2026-08-02 UTC against snapshot
`233e3b16...aae4`. The target exports its exact installed Product versions at
`GET /v1/environments/{environmentID}/target-hub-versions`; the source records
that payload at
`PUT /v1/export-pipelines/{pipelineID}/target-hub-versions/{targetHub}`. Both
the decoder and direct service mutation validate the payload before it can
shrink an incremental Bundle.

Executable evidence:

```text
go test ./internal/controlplane -run 'TestBundleSnapshotIncrementalCheckpoint|TestIncrementalPipelineDefaultsToCheckpoint'
```
