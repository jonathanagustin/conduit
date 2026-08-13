# Emergency Entity command contracts

Sources are indexed in `review-ledger.tsv`. This is an Apollo-aligned command
model implemented on Platform's native Plan wire; it is not a private Apollo
command-format compatibility claim.

## Scheduling and governance

- A command targets exactly one Entity, records its issuer and mandatory
  rationale, is immediately execution-eligible, and is prioritized over
  ordinary Plans that overlap that Entity.
- Commands bypass maintenance windows, suppressions, rollout pacing, health,
  and recall eligibility. They never execute concurrently with an already
  leased or running overlapping Plan. Only one pending or executing command may
  exist per Entity.
- Normal Plan evaluation resumes after termination. Consequently, desired
  state can reverse an emergency version/config action unless the operator also
  changes eligibility, recall, or suppression state.
- Pending commands may be cancelled and executing commands are cooperatively
  aborted through the existing lease heartbeat. Applied reversible steps roll
  back. A Pod restart has no truthful inverse, matching the upstream warning
  that abort cannot guarantee the Entity's health.

## Command types

- `INSTALL` reconciles the selected desired Release and configuration for an
  absent or uninstalled Entity.
- `UNINSTALL` removes an installed Helm release with history retained so a
  later failure or abort can restore the exact prior revision.
- `ENFORCE_CONFIG` keeps the observed Release and reapplies the newest
  configuration range that supports that version.
- `ENFORCE_CONFIG_AND_VERSION` reconciles both the desired version and its
  atomically resolved configuration; an explicit immutable target version may
  override normal recall selection for the incident action.
- `RESTART_RESOURCE` is restricted to a named Deployment or StatefulSet owned
  by the Entity. The Spoke performs and waits for a bounded Kubernetes rollout.
- Secret lifecycle operations attributable to the targeted Entity, plus pending
  deletions in its namespace, are included in its command Plan and continue to
  use opaque provider references only.

## Local evidence

- `internal/controlplane/commands.go` builds one-Entity, automatically eligible
  command Plans and enforces type preconditions and per-Entity single flight.
- `internal/controlplane/lifecycle.go` provides priority, overlap exclusion,
  constraint bypass, cooperative cancellation, strict command reports, and
  merge-safe preservation of other observed Entities.
- `internal/agent/helm.go` implements reversible Helm uninstall and bounded
  Deployment/StatefulSet restart without widening the Hub's cluster access.
- `go test ./internal/controlplane ./internal/agent -run 'Test.*Command|TestHelmExecutorEmergency'`
  exercises the contract.
