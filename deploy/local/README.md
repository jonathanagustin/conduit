# Local multi-node platform Spoke

This directory creates a disposable, multi-node k3d Spoke for exercising the
same bootstrap boundary used by the portable/Rubix charts. It is not a claim to
reproduce Rubix itself.

The topology is one k3s server plus three worker nodes. A bootstrap identity
creates the fixed namespaces and service accounts. The steady-state delivery
agent can manage ordinary namespaced resources only in the `example-product`
namespace; it cannot
create namespaces, CRDs, admission webhooks, or cluster-scoped RBAC.

```bash
./deploy/local/bootstrap-cluster.sh
./deploy/local/install-delivery-plane.sh
```

The generic delivery control plane runs in `platform-system` with one persistent volume.
The pull agent runs beside it, but its Kubernetes authorization is granted only
through a RoleBinding in the application namespace. Local registry traffic is
plain HTTP by design; production must use TLS and per-Environment cryptographic
identity rather than the current protocol headers.

Application Products are published and installed in a later step because the
API readiness contract requires PostgreSQL. The cluster and delivery plane are
kept independent of that data-plane choice.

k3d v5.7.4/k3s v1.30.4 in this workspace fails before process exec when a
non-root container also sets `capabilities.drop: [ALL]`, even for a world-
executable static binary. The local image runtime also materializes `/` with
mode `0754`; the local Pods therefore retain UID 65532 but use GID 0 so they can
traverse `/etc` for Kubernetes DNS. The manifests retain `runAsNonRoot`,
`allowPrivilegeEscalation: false`, seccomp, and a read-only root filesystem,
while omitting the capability mutation. Portable Product charts keep their
stricter UID/GID and drop-all posture; these exceptions must not be copied into
Rubix or production overlays.
