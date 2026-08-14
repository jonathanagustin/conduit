# Delivery contract

Delivery is a Conduit-native bounded context. It owns rollout intent and the
resolved Plan handed to a Spoke. Apollo evidence may constrain selected names
or behavior, but Apollo transport types do not enter the Delivery model.

## Invariants

- A Plan names its Environment by stable identifier and immutable generation.
- Each step carries the exact Product Release version, release digest,
  configuration digest, namespace, artifacts, and ordered dependencies needed
  for execution.
- Maintenance windows and bounded overrides are evaluated before work becomes
  eligible.
- Governance approval and policy evidence are prerequisites to leasing; a
  Spoke cannot approve or rewrite a Plan.
- Execution uses bounded leases, supports cancellation at the last safe point,
  and records rollback and reported-state evidence.
- Optimization may recommend a schedule but cannot authorize or execute it.

The native Plan wire and persistence shape lives in
[`internal/delivery/plan`](../../../internal/delivery/plan). Maintenance rules
live in
[`internal/delivery/maintenance`](../../../internal/delivery/maintenance), and
the rollout compiler uses ports declared under
[`internal/delivery/rollout`](../../../internal/delivery/rollout).

## Current migration state

The extracted packages own new Delivery types and rollout logic. Much of the
existing lifecycle, promotion, recall, and persistence implementation still
resides in [`internal/controlplane`](../../../internal/controlplane). That
facade is a migration constraint, not the target boundary.

## Verification

```bash
go test ./internal/delivery/...
go test ./internal/controlplane -run 'TestPlan|TestPromotion|TestRecall|TestMaintenance|TestLifecycle'
go test ./internal/agent -run 'TestWorker|TestHelmExecutor'
```

These tests support Conduit-native Delivery behavior. Apollo alignment must be
attached to a separately pinned public semantic, never to this entire context.
