# Promotion and rollout contracts

Sources are indexed in `review-ledger.tsv`. These are reconstructed public
semantics, not private scheduler compatibility claims.

## Promotion

- A stage moves one immutable Product Release from a reachable source channel
  to a distinct target channel. Passed stages form a directed acyclic graph;
  unreachable sources and channel cycles fail closed.
- Evidence is per Environment/installation and has bounded freshness. Healthy
  evidence must satisfy the soak duration; unhealthy evidence is independently
  bounded. Weekend exclusion, Release-label requirements, minimum healthy
  count, maximum unhealthy count, and an overall evaluation timeout are part of
  the immutable decision.
- A failed canary creates an Entity-scoped promotion suppression. A later
  passing evaluation for that Release removes its active promotion
  suppressions. Normal Plan leasing rechecks suppression at the last safe
  point.
- An operator can pause one Product's exact source-to-target stage without
  rewriting earlier decisions. The current control is revisioned and records
  subject, rationale, and time. Pause and resume are idempotent; a pause is
  checked before evaluation and again atomically when decision evidence is
  created, so a committed pause excludes later evaluation even under a race.
- The public page was rechecked on 2026-08-02 UTC. Its summary prose says a
  canary promotes when all conditions are satisfied, while the reviewed UI
  evidence presents separate promote/recall action rules. Platform retains the
  image-derived OR composition for promote rules and makes any met recall rule
  take precedence. This ambiguity is recorded rather than presented as private
  scheduler equivalence.

## Ramped rollout

- Ramping applies only to version upgrades. Installs, config-only reconciles,
  downgrades and recall roll-offs bypass it.
- A passing promotion snapshots its rollout window. Each installation receives
  a stable slot derived from Environment, installation, and promotion identity;
  Plan regeneration and Hub restart therefore cannot reshuffle or accelerate
  the window. Apollo documents first-come/non-deterministic ordering; the local
  deterministic order is an intentional safety-strengthening implementation of
  the same fleet pacing contract.
- Maintenance and suppression constraints remain independent and are checked
  again at lease time. A ramp slot does not bypass either gate.

## Helm execution boundary

Apollo documents two exact strategies. `manageRollout` is the production
default: apply, wait for readiness, and roll back an apply or readiness failure.
`applyChangesNoWait` applies resources and succeeds before readiness, but still
rolls back an apply failure. The public contract also separates the five-minute
Helm operation timeout from install/upgrade readiness timeouts.

Image pre-pull is enabled by default in Apollo with a 15-minute timeout;
per-Product overrides are bounded from 1 to 90 minutes. It happens before the
chart is applied so a slow image transfer does not consume service rollout
availability.

The Platform Agent now:

- keeps `manageRollout` as its fail-safe default and supports the typed
  `applyChangesNoWait` override with explicit rollback on apply failure;
- snapshots Environment install/upgrade readiness defaults and per-Release
  overrides into each immutable Plan;
- reports no-wait success as `UNKNOWN` health and `IN_GRACE_PERIOD` rollout so
  Plan success never fabricates workload readiness; and
- delegates exact-digest image preparation, with a bounded context, to the
  existing Environment-owned material preparer. This lets an Environment use
  its Kubernetes/provider-native image-cache implementation without giving the
  Hub node or registry credentials.

Image pre-pull is enabled by default with the documented 15-minute timeout. An
Environment without a native image-cache implementation must explicitly set
`helmExecution.imagePrePull.enabled=false`; omission never silently weakens the
rollout. A Product timeout override is valid only while pre-pull is enabled.
The Plan snapshots the effective decision and the Agent fails closed when an
enabled Plan has no external preparer.

One private-runtime difference remains explicit. Helm 3 exposes one `--timeout`
for hooks and readiness during an atomic managed rollout, so Platform preserves
distinct policy fields but the selected readiness timeout also bounds Helm
operations in that mode. Exact timeout-phase behavior therefore remains aligned
but does not match the wire format or runtime of the private helm-chart-operator.

## Local evidence

- `internal/controlplane/promotion.go` creates reproducible immutable decisions,
  validates reachability/freshness/timeouts, and reconciles canary suppression.
- `internal/controlplane/plan.go` snapshots stable ramp constraints;
  `lifecycle.go` uses observed state so only upgrades wait for their slot.
- `promotion_test.go` covers multi-stage reachability, cycles, evidence and
  evaluation timeouts, durable governed pause/resume, suppression/recovery,
  and every ramp bypass class.
- `rollout_policy_test.go`, `helm_test.go`, and `material_test.go` cover policy
  validation/snapshotting, the default-on and explicit-opt-out paths,
  managed/no-wait commands, success-state fidelity, apply-failure rollback,
  image-preparer metadata, and timeout bounds.
