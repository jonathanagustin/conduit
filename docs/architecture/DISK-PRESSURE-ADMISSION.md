# Disk-pressure admission evidence

Status: **implemented advisory core; Hub admission remains open**.

`platform-optimizer compile-substrate-preflight` accepts a content-addressed,
credential-free observation whose normalized facts remain bound to exact
read-only provider-call request/response digests, collector identity, source
revision, and observation time. Runtime filesystem evidence is intentionally a
different call from scheduler capacity and post-change node-pool projection.
Those authorities are not interchangeable.

The storage contract fails closed unless it receives all of the following:

- one explicit Kubernetes `DiskPressure` condition for every observed node;
- explicit current free-byte and free-inode observations for every node (an
  observed zero is valid evidence and blocks via the capacity check);
- exact per-image OCI digest evidence for compressed bytes, unpacked bytes,
  unpacked inodes, and projected peak node-local copies;
- positive writable-layer and log byte/inode reserves; and
- positive minimum post-rollout byte and inode headroom.

Any node reporting `DiskPressure=TRUE` blocks eligibility. An `UNKNOWN`
condition also blocks rather than being interpreted as healthy. Missing
conditions, inode observations, image-size evidence, reserves, or headroom are
invalid requests rather than zero demand.

## Peak calculation and deduplication

The existing incremental `additional` vector is the complete rollout surge or
blue/green workload request. The optimizer adds `daemonSetPerNewNode *
newNodes` once. An explicit placement projection must distribute exactly that
peak ephemeral-storage total across target nodes. Each target-node check then
adds its unique image `(compressed + unpacked)` bytes, writable-layer reserve,
log reserve, and minimum retained headroom. Its inode check similarly adds
unpacked image inodes plus inode reserves and minimum headroom.

Image records are unique by exact OCI digest, and a digest may appear at most
once in each node placement. The declared `copies` count must equal the number
of projected nodes that reference the digest. The workload-byte placements
must sum exactly to surge plus DaemonSet demand. These equality checks prevent
both omission and double counting. Checked integral arithmetic rejects all
overflow.

This is conservative by design: scheduler ephemeral-storage headroom and live
filesystem free space are checked independently because satisfying one does not
prove the other. Per-node checks also prevent aggregate capacity on unrelated
nodes from admitting an image unpack on a full target node.

Network enforcement and observability are separate requirements. A caller may
require healthy `NETWORK_POLICY` coverage, healthy `NETWORK_TELEMETRY`
coverage, both, or neither; absence of one is never reported as absence of the
other.

## Authority boundary

The implementation evaluates and persists immutable advisory receipts only. It
does not approve a Plan, change leaseability, mutate a cluster, collect provider
credentials, or infer missing provider state. H77 therefore remains open until
the Hub accepts a neutral evidence reference, revalidates freshness and exact
proposal binding, and refuses approval/lease before a live rollout when this
assessment is absent or ineligible.

Executable evidence:

```text
go test -race ./internal/optimization ./cmd/platform-optimizer -run 'SubstrateObservation|SubstratePreflight|CompileAndAssess'
```

Targeted tests constrain DiskPressure `TRUE`/`UNKNOWN`, missing conditions,
missing versus observed-zero free inodes, stranded per-node capacity,
missing/invalid image sizes, unique-image accounting,
writable/log reserves, retained byte/inode headroom, DaemonSet amplification,
overflow, distinct network enforcement/telemetry requirements, distinct
provider authorities, exact receipt replay, and the CLI compile/assess path.
