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
cluster-scoped RBAC. Application namespace Roles and RoleBindings for a Spoke
remain deployment-owned.

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
storage class choice. Its single-writer PVC has Helm's `keep` policy.
`consoleBootstrap` supplies only evidence-bound tenant identity and branding;
the chart schema rejects unknown fields and the Hub validates the rendered
read-only ConfigMap again before startup. It cannot configure authentication,
capabilities, API origins, scripts, or security policy. See
[`docs/ui/contracts/runtime-bootstrap.md`](../../docs/ui/contracts/runtime-bootstrap.md).

The Spoke requires a stable Environment/Agent identity, an explicit HTTPS Hub
URL, a client TLS Secret whose URI SAN matches Conduit's Agent identity
contract, and the Hub CA ConfigMap. Its ServiceAccount is namespaced and needs
only deployment-owned access to the exact application namespaces it manages.
Both charts accept references to deployment-owned image-pull Secrets without
creating or receiving registry credentials.

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
for software claiming strong availability. The current filesystem-backed Hub
is deliberately one replica with `Recreate` and a retained single-writer PVC;
it does **not** claim that profile. Multiple replicas require a transactional
repository and a separate failover gate. The outbound Spoke is restart-safe,
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
affinity, and the absence of namespace/CRD/cluster-RBAC/host ownership.
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
  [`APOLLO-MAVEN-INVENTORY-EVIDENCE.md`](../../docs/apollo/APOLLO-MAVEN-INVENTORY-EVIDENCE.md).
