# Conduit Helm packaging

Conduit ships two independently installable charts:

- [`platform-hub`](platform-hub) installs the durable control-plane Hub and its
  ClusterIP service into an existing control-plane namespace.
- [`platform-spoke`](platform-spoke) installs one outbound-only Agent into an
  existing Environment namespace.

There is deliberately no production umbrella chart. A Hub and a Spoke have
different trust authorities, credentials, persistence, release cadence, and
usually cluster placement. Coupling them into one Helm release would also make
an Environment Agent depend on direct in-cluster Hub discovery, contrary to the
outbound Spoke contract.

Both charts package the same binaries and security contract as the Flux and
Zarf runtime sources. They do not install Products, Product charts, application
namespaces, credentials, cluster operators, CRDs, schedulers, scalers, or
cluster-scoped RBAC. The Spoke chart does render a namespaced Role and
RoleBinding per managed application namespace — a floor, not the whole grant.
Read [The Agent's RBAC floor](#the-agents-rbac-floor) before treating it as a
bound on what the Agent can do.

The `foundry-tenant` chart previously lived here and now lives in the product repository
(`jonathanagustin/palantir`, `deployment/helm/foundry-tenant`). Two reasons, neither of which is a
matter of how this README happens to be worded:

- It carries Product knowledge. The chart names Foundry's services — multipass, compass,
  foundry-metadata, ontology-metadata-service and the rest — and provisions their TLS secrets and
  namespace. Conduit is product-agnostic and independently versioned, and a chart that has to
  change when Foundry gains a service is not either of those things.
- Its service roster DUPLICATED that repository's `deployment/mvf-products.json` exactly. A copy
  that is byte-identical stays correct until the day it does not, and the divergence would not
  announce itself: a service would build, publish and deploy while silently having no TLS secret
  and no namespace entry. A gate there now fails if the two lists differ in either direction.

The Flux wiring stays here: `deploy/flux/foundry-tenant.example.yaml` and
`foundry-tenant-rbac.yaml` reference the chart by OCI URL and reconciliation identity, which is the
delivery plane's side of the contract and never depended on the chart source living in this repo.

The charts are Conduit-native and Apollo-aligned. They do not claim
compatibility with Palantir's private Apollo or Rubix protocols.
The evidence grain and explicit non-claims behind that statement are recorded
in [`docs/architecture/deployment-invariants.md`](../../docs/architecture/deployment-invariants.md).

## Installation

Each deployment supplies exact release-image digests and externally owned
identity material:

```sh
helm upgrade --install platform-hub ./deploy/helm/platform-hub \
  --namespace platform-system \
  --values hub-values.yaml

helm upgrade --install platform-spoke ./deploy/helm/platform-spoke \
  --namespace environment-system \
  --values spoke-values.yaml
```

The Hub requires at least one bootstrap administrator, a server TLS Secret, an
external client CA ConfigMap, an Ed25519 PKCS#8 bundle-signing Secret, and a
deployment-owned Secret containing one PostgreSQL DSN. The default profile runs
two replicas against that transactional repository; the credential is mounted
read-only and never enters chart values. Explicit `repository.kind=file` is a
single-replica development/migration profile whose PVC retains Helm's `keep`
policy.
`consoleBootstrap` supplies only evidence-bound tenant identity and branding;
the chart schema rejects unknown fields and the Hub validates the rendered
read-only ConfigMap again before startup. It cannot configure authentication,
capabilities, API origins, scripts, or security policy. See
[`docs/ui/contracts/runtime-bootstrap.md`](../../docs/ui/contracts/runtime-bootstrap.md).

The Spoke requires a stable Environment/Agent identity, an explicit HTTPS Hub
URL, a client TLS Secret whose URI SAN matches Conduit's Agent identity
contract, the Hub CA ConfigMap, and `rbac.managedNamespaces` — the exact
application namespaces its Plans target. The chart refuses to render without
that list rather than defaulting to the release namespace, which is not where an
Agent installs. Both charts accept references to deployment-owned image-pull
Secrets without creating or receiving registry credentials.

Production leaves `runtimeSupplementalGroups` empty. A container runtime that
materializes image roots without world-execute permission can require an
explicit traversal group; the k3d acceptance test sets `[0]` for that local
runtime only while retaining primary UID/GID 65532, dropped capabilities,
read-only roots, and no privilege escalation.

Production also leaves `helmPlainHTTP=false`. The k3d acceptance test enables
it only for its loopback, disposable OCI registry, which does not serve TLS.
It is not a supported production registry configuration.

Setting `serviceAccount.create=false` requires an explicit pre-existing
ServiceAccount name. Neither chart silently invents an external identity.

## Optional features

Neither chart has a free-form `extraEnv`; both schemas reject unknown keys.
Each feature below is a typed value that is off by default, and the default
render is unchanged by their existence.

| Value | Sets | Notes |
| --- | --- | --- |
| `metrics.enabled`, `metrics.port`, `metrics.service.port` | `METRICS_ADDRESS`, a `metrics` container port and Service port | Requires `repository.kind=postgres`: the Hub serves `/metrics` only for the PostgreSQL repository, so the chart refuses the file profile rather than render a port nothing listens on. Plain HTTP. |
| `metrics.serviceMonitor.enabled` | a `monitoring.coreos.com/v1` ServiceMonitor | Requires `metrics.enabled` and the Prometheus Operator CRDs, which the chart does not install. |
| `notifications.transport` = `webhook` | `NOTIFICATION_WEBHOOK_URL` from `notifications.webhook.urlSecretName`/`urlSecretKey` via `secretKeyRef`, optional `NOTIFICATION_WEBHOOK_TIMEOUT` | The URL is never a value: webhook URLs commonly embed a token. |
| `notifications.transport` = `log` | `NOTIFICATION_DELIVERY_LOG=true` | For a deployment whose logs somebody already watches. |
| `notifications.transport` = `native` | `NOTIFICATION_SMTP_*` and/or `NOTIFICATION_SLACK_TOKEN_FILE`, optional `NOTIFICATION_NATIVE_TIMEOUT` | SMTP username/password and the Slack token are Secrets mounted as read-only files; an optional SMTP CA is a ConfigMap. At least one of `native.smtp.address` (with `from`) or `native.slack.tokenSecretName` is required. |
| `planOrchestrator.driver` = `temporal` | `PLAN_ORCHESTRATOR=temporal`, `PLAN_TEMPORAL_ADDRESS` | The Hub dials this frontend without TLS or an API key, in Temporal namespace `default`. An empty `temporalAddress` falls back to `packagingWorkflow.address`. |
| Spoke `materialPreparer.command` | `EXTERNAL_MATERIAL_PREPARER` | A clean absolute path. The Agent image ships no preparer; `materialPreparer.toolsPersistentVolumeClaimName` mounts an existing PVC read-only at `toolsMountPath` to supply one. |

Every notification transport requires `repository.kind=postgres`, whose
durable queue delivery leases from; the Hub refuses to start otherwise and so
does the chart. The Hub accepts exactly one transport, which is why
`notifications.transport` is a single choice rather than three switches.

`CONDUIT_TEMPORAL_ADDRESS` (planning workflows) is deliberately not a value.
The Hub refuses any non-loopback address for it, and this chart deploys no
Temporal sidecar, so no value could make it work.

These values are checked by rendering. No Hub or Agent was started against
them here: delivery to a real webhook, SMTP server or Slack workspace, a
Prometheus scrape, a Temporal-sequenced Plan, and a preparer run on a mounted
PVC are each unexercised by `verify-helm-runtime.rb`.

## The Agent's RBAC floor

The Spoke Agent mounts a ServiceAccount token and mutates a tenant's cluster
with it. Until #264 nothing in this repository said what that token was bound
to; the answer was whatever binding the tenant happened to create. The chart now
renders one namespaced `Role` and `RoleBinding` per entry in
`rbac.managedNamespaces`, bound to the ServiceAccount the Deployment actually
runs as.

**Every rule is derived from a call site, and the derivation is in the template
next to the rule** (`platform-spoke/templates/rbac.yaml`). In summary:

| Rule | Why the Agent needs it |
| --- | --- |
| `""` secrets, full verbs | Helm's release ledger. `internal/agent/helm.go` runs `helm list`, `upgrade --install`, `uninstall --keep-history`, `rollback` and `status`; nothing sets `HELM_DRIVER`, so the default `secret` driver keeps that ledger as Secrets in the target namespace. Also the `kubernetesSecretName` half of `internal/agent/material.go`. |
| `""` pods, replicationcontrollers; `apps` deployments, statefulsets, daemonsets, replicasets; `batch` jobs, cronjobs — full verbs | The workload kinds Conduit's own post-renderer expects in a rendered chart (`apolloPodTemplatePaths` and the bare-Pod branch in `internal/apollo/workloadmetadata/annotations.go`). `patch` on deployments and statefulsets is what `kubectl rollout restart` issues, and `internal/controlplane/commands.go` admits no other kind for a restart. |
| `networking.k8s.io` ingresses, full verbs | `ingressReferences` in `internal/agent/ingress.go` admits `networking.k8s.io/v1` Ingress out of a deployed manifest; `observeIngressReferences` reads each back for a load-balancer address. |
| `""` events, read-only | `ingressDiagnostic` attaches controller Events to an unrealized Ingress. The Agent never writes one. |
| `""` configmaps, full verbs | The one chart this repository installs end to end — `testdata/e2e-chart`, applied by `scripts/test-spoke-e2e.sh` — renders a ConfigMap. |

**Namespaced, with no cluster-scoped exception.** Every Kubernetes call in
`internal/agent` carries an explicit namespace: `helm.go` passes `--namespace`
to each `helm` and `kubectl` invocation, and so do `health.go` and `ingress.go`.
Two things would have forced cluster scope and neither applies:

- **Cilium.** `CiliumPolicyStore.HasNamespacedPolicyCRD` in
  `internal/agent/cilium.go` would need a cluster-scoped read of
  `apiextensions.k8s.io` CustomResourceDefinitions. `cmd/platform-spoke-agent/main.go`
  constructs no `CiliumBackend` — the port has no production adapter — and the
  interface's own contract says an implementation "must reject
  `CiliumClusterwideNetworkPolicy` and must not use cluster-admin credentials".
  So the chart grants none of it. A deployment that wires a Cilium backend owns
  that grant, and it is the one place where namespaced is not enough.
- **Charts that carry cluster-scoped objects.** See below. That is a property of
  the tenant's chart, not of the Agent.

One Role per namespace rather than one ClusterRole is also the narrower shape: a
ClusterRole would grant the same rules in every namespace, including ones no
Plan will ever name.

### What this chart does not bound

**It is a floor. It is not a ceiling, and it is not the whole grant.**

`helm upgrade --install` applies whatever the tenant's chart rendered.
`internal/agent/helm.go` validates the rendered images against the Plan's
declared artifacts and validates nothing about the *kinds* — and
`apollo_annotations.go` says so in as many words: "Kinds outside this table
still receive object annotations." A product chart carrying `Service`,
`ServiceAccount`, `PersistentVolumeClaim`, `PodDisruptionBudget`,
`NetworkPolicy`, a CRD, or any cluster-scoped object will fail to install under
this Role alone, and **the tenant's cluster admin must grant the difference**.
This repository cannot derive it, because the chart is a Plan input rather than
a fact about Conduit. `deploy/local/prerequisites.yaml` is one such extended
grant — hand-written, wider than this floor, and what the E2E cluster uses.

**The blast radius, stated rather than implied.** Within each managed namespace
the Agent can read every Secret. Kubernetes RBAC scopes `get`, `update` and
`delete` by `resourceName` but never `list` or `create`, and a Helm release
Secret's name carries a revision number that does not exist when the Role is
written, so the rule cannot be confined to Helm's own objects. An Agent
permitted to manage one release in a namespace can read every credential in it.
Set `rbac.create=false` and bind a narrower Role yourself if that is
unacceptable; nothing else in this chart makes it smaller.

**What is out of scope of this section entirely.** Whether one tenant's
compromised Agent can pull another tenant's Bundles is a Hub-side authorization
question, not an RBAC one, and it is not answered here — see gap 8 in
[`docs/operations/PRODUCTION-READINESS.md`](../../docs/operations/PRODUCTION-READINESS.md).

The gate is `ruby scripts/verify-helm-runtime.rb`: it fails unless the
RoleBinding names the ServiceAccount the Deployment uses, the `roleRef` is a
namespaced `Role` that the same render produced, no rule carries a wildcard
apiGroup, resource or verb without a `# wildcard-justified:` line in the
template naming that exact rule, no rule grants `escalate`, `bind` or
`impersonate`, and the chart fails closed when `rbac.managedNamespaces` is
absent or empty.

Human and service-user Hub access can use `operatorOIDC`. Prefer `multipass`
when a tenant already has Palantir identity infrastructure; `generic` supports
standard issuers such as tenant-local Keycloak, and `cognito-access` is the
low-cost AWS fallback. Values contain only public trust configuration, never a
browser cookie, bearer token, client secret, or password. See
`docs/architecture/identity-and-governance.md`.

## Bounded Rubix profile

Current public Rubix documentation sharpens the boundary: Rubix App Platform
is not a proprietary Kubernetes distribution. It pairs an open-source or COTS
Linux OS and Kubernetes distribution with a hardened operating profile, and
publicly names Cilium, Envoy, Cert-Manager, Vault, a compliance operator, and
Terraform-backed cloud provisioning. These are Environment-bootstrap owners,
not dependencies that the Hub or Spoke charts install. The charts consume only
their explicit namespaced products and independently verifiable receipts.

The two `rubix-values.example.yaml` files demonstrate only evidenced,
consumer-supplied integration points:

- Hub Service annotation `com.palantir.rubix.service/pod-cert`;
- optional Hub SPP v3 endpoint metadata with explicit context path and domain
  alias;
- external `tls-external-ca-pem-bundle` ConfigMaps;
- required `com.palantir.rubix/instance-group` node affinity;
- HTTPS Hub probes and restricted Pod security.

The Hub Pod-certificate Secret name remains explicit because public examples
show the annotation/mount convention but not a portable issuance contract. The
Spoke identity stays externally issued: available evidence does not establish
that a Rubix Pod certificate carries Conduit's Environment/Agent URI SAN.

Affinity expresses placement intent only. It does not create capacity, reserve
nodes, or guarantee an availability zone. Conduit does not install the
archived Spark scheduler/library controllers, Demand or ResourceReservation
CRDs, conversion webhooks, node mutation, or cluster-wide RBAC.

No chart assumes SSH, interactive `kubectl exec`, host networking, host
PID/IPC, host paths, privileged containers, namespace creation, cluster-admin,
or mutable image tags.

The public Rubix technical specifications also require disruption tolerance
for software claiming strong availability. Conduit's production profile now
uses two PostgreSQL-backed Hub replicas, a zero-unavailable rolling update, and
a one-replica disruption budget. The integration gate proves transactional
concurrency and rollback, but no node-loss or database failover exercise has
established a Rubix availability claim. The outbound Spoke is restart-safe,
but its chart likewise makes no private Rubix availability claim.

Public pages currently state two different cycling bounds: a 48-hour maximum
for nodes and a 72-hour bound for containers. The chart therefore does not
encode one universal duration. A future substrate profile must distinguish
node-image recycling from workload/container recycling and bind each configured
value to a versioned provider receipt before admission.

## Verification

```sh
ruby scripts/verify-helm-runtime.rb
```

CI runs that gate under pinned Helm 3.21.1 and 4.2.3. The Spoke E2E installs
both actual charts with generated TLS/mTLS identities before exercising the
approved Plan. These gates verify the two
release boundaries, exact image digests, required identity/signing inputs,
HTTPS transport, retained state, restricted Pod security, Rubix annotations and
affinity, the Spoke's namespaced RBAC floor, and the absence of
namespace/CRD/cluster-RBAC/host ownership.
Semantic-tag publication packages both charts with the Conduit version, pushes
each to GHCR, resolves and signs its registry-reported OCI digest, and attaches
the chart archives, SHA-256 files, SPDX SBOMs, and immutable-reference manifest
to the GitHub release.

Evidence reviewed 2026-08-02:

- Palantir FedStart Helm charts commit
  `1d27354123f147c6b8840e8a286915f910bad489`;
- Palantir Spark scheduler commit
  `acc007d3a5d7a6b863820f20b194d681f1058a06`;
- Palantir Spark scheduler library commit
  `0e821b8dbe3f0fb1c1d9ea7c366fa4c9a911d414`;
- Palantir Cloud Pak CASE/operator commit
  `4d087b0b78e0cbd1bc9856d3d830186acac51c6f`;
- bounded Maven inventory observations in
  [`REFERENCE-MAVEN-INVENTORY-EVIDENCE.md`](../../docs/reference-platform/REFERENCE-MAVEN-INVENTORY-EVIDENCE.md).
