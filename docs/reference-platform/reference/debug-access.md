# Debug terminal and JIT access boundary

Apollo source identity is recorded in `review-ledger.tsv`. Platform does not
embed a browser shell or turn the delivery Agent into a privileged access
proxy. This note defines the integration contract for an Environment-owned
access broker.

## Derived contract

- A base debug session is isolated per user, expires within four hours, and is
  read-only over a bounded set of namespaced Kubernetes resources. Environment
  Operator authority is required to start one.
- A debug profile has an immutable name and selects exactly one installed
  Artifact Registry plus a versioned image. Platform deployments should
  strengthen the upstream tag requirement to an admitted digest. Images must
  provide `/bin/bash`, use a numeric non-root UID, and match the target node
  architecture; the broker must fail before session creation when preflight
  cannot prove those properties.
- Elevated access is a separately governed, time-bound policy grant. The
  requester cannot approve their own grant. Approval, activation, extension,
  rejection, and revocation are durable audit events; revocation applies to
  active and future sessions.
- Session credentials and Kubernetes impersonation material never enter
  Product desired state, Plans, or the delivery Agent.

## Preferred existing technology

Use an existing Teleport Kubernetes Access deployment as the session broker.
Its Kubernetes service maps authenticated roles to Kubernetes users/groups and
resource verbs, supports time-limited Access Requests, and records interactive
`kubectl exec` sessions. Native Kubernetes RBAC remains the final enforcement
layer. Environment bootstrap owns Teleport enrollment, audit storage, session
recording policy, and any toolbox Pod/profile controller.

Official integration references:

- <https://goteleport.com/docs/enroll-resources/kubernetes-access/controls/>
- <https://goteleport.com/docs/connect-your-client/request-access/>
- <https://goteleport.com/docs/reference/architecture/session-recording/>

The existing Platform Change Request/RBAC model can authorize profile and JIT
policy changes, but a broker adapter and consumer-owned deployment are still
required before this row can be called implemented. This is deliberately
reported as a boundary rather than simulated with an unaudited shell Pod.
