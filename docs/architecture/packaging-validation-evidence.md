# Packaging validation evidence boundary

Issue #343 distinguishes structural validation from trusted receipt admission
and freshness. The existing unsigned `schema-helm/v1` receipt and caller-supplied
artifact assessments do not establish an independently authenticated producer.
The proposed held-out profile and its matrix remain in
[`uds-packaging`](../evidence/uds-packaging/README.md), with runtime checks
`NOT_RUN`. This boundary supplies native verification and admission primitives.
It is not composed into a Hub route, worker, scanner, approval or deployment path.

## Authenticated claims

`internal/packagevalidation/domain` verifies fixed-version Ed25519 attestations
against trusted operator policy. The policy is supplied by the receiving
host's authority adapter, never accepted from the attestation or generated
output. It names the enabled profile digest, required check IDs, exact tool
versions/executable/database pins and per-check input digests. Producer keys
have derived fingerprints, allowed profile digests and validity bounds.
Changing policy content changes its identity and invalidates earlier claims.

One manifest binds run/proposal identity, source, template, profile, policy,
artifact set, execution-toolchain recipe, target/configuration and a fresh
256-bit receiver-issued challenge. The toolchain digest must include the
operator-owned execution recipe (arguments, sanitized environment policy,
timeouts and validator code), rather than just a tool's display name. Current
manifest/input digests must be computed from authoritative immutable inputs;
reading the values back from generated output would defeat this boundary.

Every configured check must be present exactly once, PASS with exit zero, match
its configured tool/input pins and retain output/diagnostics digests. UNKNOWN,
BLOCKED, NOT_RUN, missing checks/tools/databases, changed inputs and nonzero exit
statuses cannot produce verified evidence. The verifier does not infer whether
a generic profile's configured recipe is sufficient for a particular app; a
trusted profile compiler must enforce that mapping before production composition.

The signing payload is `conduit.packaging-validation/v1` plus a newline and
Go's typed JSON encoding, with an empty signature field. Checks and producer
profile sets are sorted where order is semantically irrelevant; times are
normalized to UTC. This is a native contract, not a claim of JCS or private
protocol compatibility. Documents are bounded to 128 KiB and 64 checks, with
strict duplicate/unknown-field rejection. The code supplies a public payload
builder/verifier and no production signing-key API. Unit signing keys are
runtime-generated synthetic fixtures, never committed credentials.

Producer authentication attests who made a claim; it does not independently
prove an honest execution, workload correctness or a compliance certification.
The trusted producer must actually run the operator-owned checks and bind their
real artifacts/logs. This library neither invokes those tools nor creates a key,
secret, approval or publication authority.

## Freshness and one-use admission

Receipt lifetime and configured maximum age are positive and at most one hour.
Issued timestamps cannot be in the future, end boundaries are exclusive, and
old checks cannot be refreshed merely by signing a new wrapper timestamp.
Evidence validity is capped by check age, receipt expiry and key expiry. A
current-policy change, revoked/expired key, foreign profile or changed manifest
fails verification. The opaque domain observation serializes as `{}` and is
not an execution capability.

The application `Admit` operation asks a narrow authority port for authenticated,
authorized current attempt facts under a serialization boundary. It verifies
inside that callback, rejects cancellation/consumption, guards missing/replayed
callbacks and ignored errors, and checks the returned admission record and
expiry. Admission stores the canonical signed document, including claimed tool
versions, input/output/diagnostics digests, exit statuses and timestamps. Its
state is `VERIFIED_EVIDENCE`, never a governance approval.

The authority adapter must atomically record evidence, audit and challenge
consumption before acknowledging success. A rejected receiver call does not
consume the challenge. Workflow terminal-failure/revision policy and any
scheduling/retry of actual checks remain the owning workflow's responsibility.
The service has no executor or governance-approval port. Defenses against a
broken authority callback do not make an incorrect adapter transactional.

`adapters/memory` is a process-local reference authority. It requires an
injected authorizer, snapshots trusted setup without mutable aliases,
re-verifies the signed document/current facts at commit, and records admission,
audit and nonce consumption under one lock. Cancellation and trusted current
policy/target updates invalidate stale results; consumed challenges cannot be
reset or rotated in place. Historical reads repeat authorization and return
copies. They do not refresh evidence. The reference is not durable, shared
across replicas or backed by live Hub policy/artifact lookups.

## Required integration before production use

A real adapter must read current operator-owned profile/trust configuration,
authorize the caller and derive current source/template/artifact/toolchain and
target/configuration digests from immutable authoritative state. Its durable
transaction must prove audit failure rollback, cancellation, one-use challenge
consumption, multiple-replica contention and restart behavior. A historical
record must never substitute for fresh current policy/target validation.

The final handoff still needs separate current Conduit governance authority,
artifact/recall/suppression controls and last-safe-point revalidation. These
primitives change neither the legacy fixture workflow nor scanner assessment
trust. No fixture boolean or unsigned structural receipt becomes authenticated
by being wrapped in a new name. No producer, signer, endpoint or deployment is
automatically configured by this change.

Tests cover signature/protocol/claim tampering, exact scope, profile/key bounds,
required result/tool/database matching, exclusive time limits, strict codec,
callback corruption, current-fact drift and one-use concurrent admission. The
reference-store tests cover 24 contenders, audit/commit-expiry failure,
canonical signed-document retention and mutable-alias rejection. They are
synthetic protocol tests, not generated-application runtime or held-out results.
