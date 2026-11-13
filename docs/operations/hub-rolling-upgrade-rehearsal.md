# Live Hub image replacement rehearsal

Run `./scripts/test-hub-rolling-upgrade.sh /tmp/hub-upgrade-receipt.json` on Linux with the
current Go compiler, Python 3, Git, tar, Docker, OpenSSL, Helm 4.2.3 and kubectl
1.31.5 available. Fetch the repository tags first. The manual **Hub rolling
upgrade** workflow runs the same test with checksum-pinned tools, read-only
repository permission, a twenty-minute cap and superseded-run cancellation.
This is verification against disposable local services, not a deployment to a
tenant or publication of a product image.

The previous source revision is the newest stable `vX.Y.Z` tag at a different
commit from HEAD. The runner independently archives both commits and builds
their real Hub executables, a current Spoke executable and the same constructor
probe against each version. Both source versions use the current compiler.
Each Hub binary is imported into its own empty-base local fixture image. These image
IDs are immutable test evidence; these are not the published production images,
and this does not validate their packaging, SBOM or provenance. Generated
credentials enter only the disposable containers, never their base images.

The old constructor probe seeds 500 Teams, 1,000 authorization audit events and
100 valid pending Plans in separate seed Environments. It saves their full
repository representations for comparison. These are synthetic foundation
fixtures, not tenant captures or a capacity benchmark. The Plans carry full
valid desired-state payloads so the serving Hubs' normal reconciliation
supervisors produce real decisions and proposals. The execution Agent belongs
only to the separate rolling-test Environment; it cannot execute seed Plans.

Two previous-version Hub containers serve the shared PostgreSQL database behind
a loopback TCP proxy. mTLS remains end-to-end; the proxy neither terminates TLS
nor synthesizes responses or retries. Direct API reads prove both old replicas
serve the same in-flight Plan, and connection counters witness traffic through
each old and replacement backend. A separate mTLS reviewer approves the test
Plan under the ordinary reviewer-team policy.

The production Agent starts real Helm execution. A test wrapper pauses only
the `upgrade` invocation before delegating to the actual Helm binary; all other
commands execute normally. While the Plan remains RUNNING, one old backend at
a time is drained from new proxy connections, stopped and replaced with the
current image. The other replica continues serving. Current replica readiness
time includes constructor migrations, audit adoption, supervisor priming and
HTTP startup. The receipt records each measured elapsed time at the stated
dataset size; it does not isolate DDL duration or establish a production bound.

The execution barrier stays held for at least 45 seconds, more than two
20-second lease periods. Samples require the same Agent, lease attempt and
issue time, a live expiry and at least three observed expiry extensions. No
opaque lease token is written to the receipt. The wrapper then execs real Helm,
which must install the pinned chart into the real TLS-verified Kubernetes API.
The actual ConfigMap value, successful terminal Plan and exact Hub report are
verified after replacement. The Kubernetes fixture uses K3s server-only mode
and a ConfigMap chart; no workload image is pulled or Pod scheduled.

Reconciliation observation uses **one SQL statement per sample**, so all tables
share one MVCC snapshot while writers are active. Nonempty decisions must have
exactly matching audit identities. Every PROPOSE_PLAN decision must have its
deterministically identified proposal either in the outbox or in a durable Plan
receipt, never both or neither. Outbox coordinates must match that specific
decision. Previously observed decision/proposal identities must remain present.
This catches lost records and torn boundaries without incorrectly treating
normal outbox completion as loss. The observer's stable ID rule is explicitly
bound to `internal/delivery/reconciliation/domain/types.go` in both source
versions; a changed identity rule requires reviewing this cross-version oracle.

The current authorization-audit table is inspected before the final verifier
constructor can repair anything. All 1,000 seed audit events must have been
adopted by serving replicas. The current probe then compares every seeded Team,
audit event and pending Plan to the old snapshot. Additive object fields are
allowed; changed values, missing fields or changed entity identities fail.
Fixture processes, containers, images, private network, credentials and
kubeconfig are removed on exit. The receipt records source and executable
identity, images, dataset, startup timing, lease observations, invariant counts
and exact report, with no DSN or private key material.

This is a driver-managed two-replica image replacement rehearsal of application
lease, migration and persistence behavior. It is **not** a Kubernetes Deployment
controller, Pod termination, production load-balancer or production chart
upgrade test. Deployments still need their own rollout policy and capacity
validation. Older audit readers remain incomplete during mixed-version audit
adoption as documented in [authorization audit integrity](authorization-audit-integrity.md).
The [data](old-data-upgrade-rehearsal.md) and
[wire-version](version-skew-rehearsal.md) rehearsals cover separate parts of
#263; this test adds the live image/lease/decision/proposal boundary.
