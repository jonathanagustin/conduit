# Entity uninstall, unmanage, and ownership handoff

Source reviewed:

- [Uninstalling and unmanaging Entities](https://www.palantir.com/docs/apollo/managing-entities/uninstalling-and-unmanaging-entities/)

## Public semantics adapted

- Uninstall removes an Entity's underlying Kubernetes resources; unmanage
  removes it from Environment settings and stops future Plans.
- The default workflow uninstalls and then unmanages. An explicit advanced
  unmanage-only choice leaves the Kubernetes resources installed and is
  intentionally discouraged.
- Required dependents block removal; optional dependents do not. An approved
  removal can wait for those constraints.
- A pending uninstall can be undone before execution, after which Apollo stops
  trying to uninstall it.

## Platform contract and evidence

`DesiredState.ownershipHandoffs` is an explicit, rationale-bearing declaration
that an Entity has left Platform ownership without being deleted. It cannot
coexist with a managed declaration for the same installation. The control
plane accepts a handoff only for a previously managed Entity that is exactly
reported as installed. Normal dependency validation prevents a required
dependent from remaining managed.

The resulting reviewed Plan uses the published `DELETE_ENTITY` Change Type but
records `unmanageOnly: true`, an explicit managed-state diff, and a native
`unmanage` action. A dedicated undo transition cancels only an awaiting or
approved handoff Plan that has never been leased; it cannot race an Agent after
execution authority has crossed the lease boundary.

The Spoke treats `unmanage` as observe-only: it never invokes Helm apply,
uninstall, secret deletion, or rollback for that step. It reports the exact
still-installed Product, Release, digest, namespace, and configuration back to
the Hub. Successful handoff history prevents later generations from synthesizing
an uninstall for the now externally owned resources; explicit re-adoption as a
managed Entity is required to restore Platform ownership.

Tests in `internal/controlplane/ownership_handoff_test.go` and
`internal/agent/worker_test.go` cover required dependency rejection, mixed
reverse-order uninstall/handoff, exact reported-state retention, durable
non-repetition, undo before lease, rejection after lease, and zero executor
apply/rollback calls for handed-off resources.

This is Apollo-aligned behavior over Platform's native desired state, Plan, and
Spoke wire. It does not claim compatibility with private Apollo management or
Agent protocols.
