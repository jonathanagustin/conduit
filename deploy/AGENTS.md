# Reusable deployment sources

- Runtime manifests are product-agnostic. Product charts, credentials,
  environment IDs, registry mirrors, and cluster overlays come from consumers.
- Never create application namespaces or grant cluster-admin. Runtime RBAC must
  remain bounded to its declared reconciliation role.
- Consumers pin source revisions and image digests. Do not introduce mutable
  runtime tags or an implicit checkout-HEAD dependency.
- Flux distributes the reviewed runtime source; it does not replace Platform
  Plans, approvals, health gates, or the Spoke execution contract.
- Verify Flux changes with `ruby scripts/verify-flux-runtime.rb`, Helm changes
  with `ruby scripts/verify-helm-runtime.rb`, and use the Spoke E2E gate for
  runtime behavior.
