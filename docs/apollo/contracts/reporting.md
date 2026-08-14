# Reporting contract

Reporting projects desired and observed state for operators. It does not own a
second desired-state store and does not infer successful rollout from health
alone.

## Invariants

- Desired state comes from immutable Plan steps; observed state comes from
  attributed Environment reports.
- Product version, release digest, configuration digest, health, rollout,
  configuration, and lifecycle remain separate dimensions.
- A desired installation with no observation is represented as unknown and
  stale rather than omitted.
- Release or configuration mismatch is visible even when an Agent reports a
  favorable status.
- Observation time and maximum age are explicit, so stale data cannot appear
  current.
- Filtering and pagination operate on a deterministic Environment and
  installation identity.

The pure fleet projection lives in
[`internal/reporting/fleet.go`](../../../internal/reporting/fleet.go). The
legacy adapter in
[`internal/controlplane/fleet.go`](../../../internal/controlplane/fleet.go)
converts stored Plan and Environment report records into Reporting's narrow
inputs. Public Apollo status routes translate at the integration edge; they do
not own the canonical report.

## Verification

```bash
go test ./internal/reporting
go test ./internal/controlplane -run 'TestFleet|TestApollo.*Status|TestEnvironmentReport'
```

The projection is Conduit-native. A public-profile claim applies only to the
named `/api/v0` response or request shape covered by its adapter test.
