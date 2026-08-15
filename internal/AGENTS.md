# Internal implementation

- `controlplane` owns Products, Releases, Environments, Entities, Plans,
  approvals, leases, observations, channels, recall, and suppression.
- `agent` is outbound-only and environment-scoped. It must verify chart and
  workload-image digests before mutation and report success only after exact
  deployed-state observation.
- Recheck cancellation, recall, suppression, maintenance, and artifact policy
  at the last safe point before execution; stale approval is not authority.
- Persist lifecycle transitions before acknowledging them. A successful report
  must match Product, version, Release digest, namespace, and configuration.
- Identity headers are a development-only escape hatch behind the explicit
  `ALLOW_INSECURE_*_IDENTITY_HEADERS` opt-ins. Production identity is OIDC
  (operators) and mTLS or SPIFFE (Agents); `cmd/platform-delivery` refuses to
  combine the insecure headers with any secure mechanism, and the Helm gate
  aborts if a chart sets any `ALLOW_INSECURE_` variable.
- New Hub wiring belongs in `hub/composition`; executables must not construct
  the legacy `controlplane` HTTP facade directly.
- New bounded contexts use inward dependencies: adapters → application →
  domain/ports. Domain packages do not import HTTP, filesystem, Kubernetes, or
  another context's implementation. Cross-context work uses stable IDs,
  immutable snapshots, narrow ports, and receipts rather than shared mutable
  aggregates.
