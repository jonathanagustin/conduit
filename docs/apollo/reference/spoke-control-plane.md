# Spoke control-plane boundary

Source identity is recorded in `review-ledger.tsv`. Public Apollo component
names describe responsibilities, not public binaries or a reusable private
wire protocol.

## Responsibility mapping

- The namespace-scoped Platform Agent performs outbound Plan polling, lease
  renewal, exact Helm lifecycle work, rollback, reported-state submission, and
  active Product health probing. External material preparers broker registry
  and secret references without exposing values to the Hub or Plan.
- Flux owns installation and reconciliation of the Hub/Spoke runtime itself.
  This corresponds to bootstrap/control-plane delivery, while Product changes
  remain governed Platform Plans.
- Kubernetes remains the immediate controller for Deployment/StatefulSet
  replica convergence. The Agent reports exact release/config/lifecycle state
  after work and continuously reports annotated Product health. Environments
  that need cluster-wide Kubernetes object telemetry should consume a
  bootstrap-owned, namespace-filtered `kube-state-metrics` deployment rather
  than giving the delivery Agent cluster-reader authority.

## Admission-controller boundary

The Agent resolves immutable chart/image mirrors and prepares pull credentials
for its own Helm work. Dynamically created Pods need API-server admission-time
mutation. That capability must be implemented by the Environment's existing,
bootstrap-owned admission controller (Kyverno is the preferred policy-engine
adapter), limited to namespaces bearing the Environment label and honoring an
explicit skip label. Platform does not install the controller, own its CRDs, or
receive cluster-admin permissions. The bootstrap repository also owns the
API-server-to-webhook network policy and failure policy.

This is intentional separation, not silent feature loss: building a second
mutating webhook into the Agent would duplicate a security-critical cluster
controller and violate the repository's tenant/RBAC boundary.
