# Gitless Flux runtime source

This directory is the reusable Flux deployment boundary for the Conduit Hub
and Spoke Agent. Production clusters consume it from a signed private OCI
artifact. Git is the authoring and review system, but it is not a production
cluster dependency and no Git credential belongs in the cluster.

The `runtime/` directory is the reconciled path inside the OCI artifact. Its
Kustomization owns only the Hub and Spoke runtime in an existing
`platform-system` namespace. The legacy namespace and binary prefixes remain
during migration; they do not change repository ownership.

Flux installation, the namespace, the Spoke's application-namespace
authorization, Secrets, private-registry identity, and the bounded Flux
reconciliation identity are cluster-owner bootstrap responsibilities. Install
Flux from an integrity-pinned distribution without adding a `GitRepository`.
The example `source.example.yaml` then pulls the Conduit runtime by digest.

## Source verification

The release workflow is manually dispatched for an exact semantic tag. It
publishes `deploy/flux` to private OCI storage, signs the registry-reported
digest with GitHub OIDC, and records the expected issuer and certificate
identity in `platform-runtime.json`.

Before applying `source.example.yaml`, the cluster owner must:

1. replace the example registry and digest with the published private
   coordinate;
2. create `conduit-runtime-pull` through the approved secret manager as a
   read-only `kubernetes.io/dockerconfigjson` Secret;
3. keep the Cosign identity restricted to this repository's publication
   workflow and semantic release tags; and
4. confirm that the resulting cluster contains no Git credential or
   `GitRepository` object for Conduit.

Flux rejects a digest that is unsigned or signed by another identity. A tag is
discovery metadata and never replaces the digest in the source or deployment
receipt. See [Gitless GitOps](../../docs/architecture/gitless-gitops.md).

The deployment Kustomization must provide these `postBuild.substitute` values:

| Variable | Meaning |
| --- | --- |
| `PLATFORM_HUB_IMAGE` | immutable Hub tag-plus-digest reference |
| `PLATFORM_SPOKE_AGENT_IMAGE` | immutable Spoke Agent tag-plus-digest reference |
| `PLATFORM_ENVIRONMENT_ID` | Hub Environment ID assigned to this Spoke |
| `PLATFORM_AGENT_ID` | unique Agent ID within the Environment |
| `PLATFORM_ALLOW_INSECURE_AGENT_IDENTITY_HEADERS` | `true` only for an explicitly local cluster |
| `PLATFORM_ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS` | `true` only for an explicitly local cluster; production uses a trusted resolver or verified client certificate |
| `PLATFORM_BOOTSTRAP_ADMINISTRATORS` | Comma-separated authenticated subjects allowed to create initial Teams, grants, and CEL policies |
| `PLATFORM_HELM_PLAIN_HTTP` | `true` only for an explicitly local registry |

Production overlays must leave both insecure flags `false`. The preferred
SPIRE overlay projects the Workload API socket; a migration overlay may instead
project the Hub CA and Agent client identity through environment-owned Secrets.
The Hub's optional `GITOPS_REPOSITORY_URL` is intentionally absent. Flux
reconciles the runtime source, while Conduit retains Product, Plan, approval,
lease, rollback, and reported-state authority.

When the binary terminates operator mTLS directly, operator certificates use
URI SAN `spiffe://platform.delivery/operator/<subject>`; organizational units
are resolved as external groups. Agent URI SANs are never accepted as operator
identities.

The Hub image includes `/platform-operator` for bootstrap and GitOps Jobs that
need to call authenticated operator routes. It is a thin inbound adapter over
the operator application service; the HTTP/SPIFFE implementation is an
outbound adapter behind a semantic control-plane port. It requires
`CONTROL_PLANE_URL`, `SPIFFE_ENDPOINT_SOCKET`, and
`CONTROL_PLANE_SPIFFE_ID=spiffe://platform.delivery/hub`, and supports:

```text
put-team NAME FILE
create-grant FILE
put-approval-policy ID FILE
propose-plan FILE
approve-plan ID RATIONALE
get-plan ID
get-reported-state ENVIRONMENT
```

Input files are strict, bounded JSON. Production mode has no insecure-header or
plain-HTTP fallback.

The preferred renewable identity profile is `overlays/spire`. It uses
the SPIFFE Workload API instead of static certificate files, pins the Hub peer
to `spiffe://platform.delivery/hub`, refreshes trust bundles per handshake, and
requires a persistent externally owned Bundle signing key. Static certificate
files remain available for migration environments that cannot yet run SPIRE.

`runtime-rbac.yaml` is a bootstrap example for a Flux Kustomization using
`serviceAccountName: platform-runtime-reconciler`. ReplicaSet access is
read-only and is required by Flux's Deployment health evaluator.

## Foundry tenant reconciliation

`foundry-tenant.example.yaml` reconciles one tenant of the `foundry-tenant`
chart per file, from an OCIRepository pinned by exact digest. Everything that
distinguishes a tenant is a value, so every tenant reconciles the same chart
digest and the copy-a-directory-and-rename failure mode has nowhere to hide.

The example also requires a private pull Secret and the Product publisher's
Cosign identity. Replace both example values with the Product repository's
actual bounded authority. Conduit's release workflow is not automatically
trusted to publish a Product chart.

This direct Flux `HelmRelease` is a migration and bootstrap path. The target
tenant-delivery path is Conduit desired state, Governance approval, a leased
Plan, and Spoke execution. Flux must not become a second approval or Product
intent store.

This is a **separate boundary from the Hub/Spoke runtime above, with a separate
identity**, and the distinction is deliberate. The runtime reconciler is
namespaced to `platform-system` and must not create namespaces or hold
cluster-scoped authority. A tenant creates its own Namespace and a cluster-scoped
`ClusterSecretStore`, so it cannot be reconciled by that identity and must not
be given the chance to try.

`foundry-tenant-rbac.yaml` therefore declares `foundry-tenant-reconciler` with
an enumerated ClusterRole — no wildcards, and no `delete` on Namespace, so
pruning a tenant cannot take the namespace and everything inside it. Granting
this identity is a bootstrap decision for the cluster owner, not something the
runtime source assumes.

### Bootstrapping a tenant

Order, because each step is a prerequisite the next one fails on rather than
waits for:

1. **external-secrets**, providing the CRDs and the controller ServiceAccount
   the `ClusterSecretStore` authenticates as. Every resource this chart renders
   is an external-secrets kind, so without the CRDs the first reconcile fails on
   unknown types. `dependsOn` in the example states this.
2. **`foundry-tenant-rbac.yaml`**, a bootstrap grant by the cluster owner.
3. **The tenant**, one `HelmRelease` per tenant.

Set `foundry.createNamespace: true` when Foundry is not installed yet. Every
ExternalSecret lands in the Foundry namespace, so on a fresh cluster there is
nothing to land in and the whole release fails on `namespaces "foundry" not
found`. It is off by default because Foundry's own installer normally owns that
namespace and Helm will not adopt one it does not own.

The release does not wait on secret material. An ExternalSecret only reports
Ready once it has actually fetched from AWS, so waiting would tie the release to
state outside the cluster: one Secrets Manager key that does not exist yet fails
the install, and remediation then removes the `ClusterSecretStore` and every
sibling with it. Provisioning a tenant means creating these objects; whether
each secret has synced is the ExternalSecret's own status to report, which is
what you want to read when bootstrapping against a half-populated account.

Verified end to end against a live cluster — Flux and external-secrets 2.8.0,
the chart pulled by digest from a registry, reconciled under the impersonated
identity. `kubectl auth can-i` confirms that identity can create its Namespace,
ServiceAccounts, `ClusterSecretStore` and ExternalSecrets, and cannot delete a
Namespace, read Secrets in the tenant or Foundry namespaces, or create
RBAC — it provisions the ExternalSecrets without being able to read the Secrets
they produce.

`scripts/verify-flux-runtime.rb` gates the digest pin, the impersonated
ServiceAccount, the absence of wildcard authority, the withheld Namespace
delete, the external-secrets ordering, the disabled wait, and that Helm's
release-storage Secret access stays namespaced to `flux-system`. `scripts/verify-helm-runtime.rb` gates the chart's lint, fail-closed
values, strict schema, and that `secretStore.roleArn` is actually assumed rather
than accepted and dropped.

Manually dispatched Conduit releases publish `deploy/flux` as the legacy OCI
coordinate
`ghcr.io/<owner>/platform-runtime:<version>` and attach
`platform-runtime.json` to the GitHub Release. Consumers select `./runtime` for
the static-certificate base or `./overlays/spire` for the preferred production
profile. Deployment repositories must pin the digest from that descriptor;
they must not rebuild or republish the runtime source under a released
coordinate.
