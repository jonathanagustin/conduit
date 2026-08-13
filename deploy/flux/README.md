# Flux runtime source

This directory is the reusable Flux deployment boundary for the platform Hub
and Spoke Agent. It belongs to the platform technology repository; a cluster
deployment repository owns the Flux Source, immutable source revision, target
Environment identity, registry mirror references, and credentials.

The `runtime/` directory is suitable as the root of a Flux OCI artifact or as a
path within a Flux Git source. Its Kustomization deliberately owns only the
Hub/Spoke runtime in an existing `platform-system` namespace. Flux installation,
the namespace, the Spoke's application-namespace authorization, Secrets, and
the bounded Flux reconciliation identity are bootstrap responsibilities.

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
The Hub's optional `GITOPS_REPOSITORY_URL` is intentionally absent: Flux is the
source reconciler, while the platform retains Product, Plan, approval, lease,
and reported-state authority.

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

Tagged platform releases publish `deploy/flux` as
`ghcr.io/<owner>/platform-runtime:<version>` and attach
`platform-runtime.json` to the GitHub Release. Consumers select `./runtime` for
the static-certificate base or `./overlays/spire` for the preferred production
profile. Deployment repositories must pin the digest from that descriptor;
they must not rebuild or republish the runtime source under a released
coordinate.
