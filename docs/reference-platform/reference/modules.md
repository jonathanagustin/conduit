# Module configuration ownership

Source is indexed in `review-ledger.tsv`.

- `syncConfigOverrides` defaults to `ENABLED`.
- For a newly created Entity, every mode applies the Module's config.
- `ENABLED` replaces existing config with the Module config.
- `MERGE` preserves Entity-owned keys while Module keys win recursively.
  `{{ unset }}` removes a key controlled by the Module. Removing a key from a
  later Module definition does not remove the existing Entity-owned value.
- `DISABLED` preserves all existing Entity config when an existing Entity is
  taken under management or a Module is updated.
- `installConfigOverrides` is valid only with `MERGE`, applies on first Entity
  creation, then becomes Entity-owned. Later Module releases do not reapply new
  install-only defaults.
- Removing an Entity from a Module relinquishes management without implicitly
  uninstalling it; explicit `markedForUninstallation` is a distinct workflow.

## Automatic update approval

- A revisioned policy is configured on the Module, not on an individual
  Environment installation. Only a Module Change Manager can enable or disable
  it, and every revision records the operator and required rationale.
- The policy applies only when an installed Module tracks a newer valid Release.
  Initial installation is never an update and cannot use this approval path.
- Update shapes such as adding an Entity or marking one for uninstallation may
  be approved automatically. If reconciliation would change the resolved config
  overrides of an existing Entity, the proposal remains manual-review-only.
- A qualifying update creates an immutable approved Change Request with the
  policy rationale, policy revision, exact versions, classified Entity changes,
  and service identity. The installed record retains that approval coordinate;
  a rejected shape creates neither approval evidence nor a version mutation.

## Local evidence

`internal/controlplane/modules.go` reconciles effective config before producing
an update diff and before persistence. Tests cover all three modes, recursive
unset behavior, install-only precedence, later user customization, and
relinquishment. Automatic-approval tests cover Module-scoped RBAC, policy
persistence and optimistic revisions, allowed structural updates, rejected
config changes, and initial-install exclusion. Semantic YAML equality
intentionally ignores comments.
