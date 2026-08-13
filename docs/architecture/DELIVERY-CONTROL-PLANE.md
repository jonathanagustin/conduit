# Generic Apollo-aligned delivery control plane

When a Palantir Apollo Hub is unavailable, we operate a generic Apollo-aligned
delivery system. Application catalogs are installed through it but are not
embedded in its public model. We claim
compatibility only for public formats covered by adapter tests, not private
Apollo APIs.

Apollo is more than GitOps. GitOps provides a desired-state ledger and a
reconciliation loop. The missing load-bearing concepts are an immutable Product
catalog, Releases, Environments, installed Entities, dependency-aware Plans,
promotion gates, health/reporting, rollback, and recall.

## Minimum architecture

```text
reviewed Git commit
  exact Environment desired state
       |
       v
platform-delivery (Go control plane)
  validate immutable release coordinates
  resolve Product dependencies
  produce deterministic Plan
       |
       v
pull-based cluster agent -----------------> Helm/Kubernetes
       ^                         |
       |                         v
       +------ observed Entity state + health

OCI registry: immutable Helm packages, images, digests, SBOMs
GitHub: PR review, protected environments, audit and promotion commits
```

The Go kernel is in [`../.`](.././README.md).
It validates the two foundational Releases, orders API before Workspace, and
durably tracks approval, lease, attempt, terminal outcome, and reported Entity
state. Its Go Spoke worker now leases approved Plans outbound-only, renews the
lease, verifies the registry-reported Helm chart digest, performs atomic Helm
reconciliation, observes the installed release, and rolls completed steps back
in reverse dependency order after a later failure.

The read-only analysis of the supplied overview video and frames is in
[`APOLLO-PLATFORM-DEMO-BUNDLE.md`](APOLLO-PLATFORM-DEMO-BUNDLE.md). Its most
important additional constraint is to keep health, rollout, configuration,
recall, and per-installation suppression as separate state dimensions.

The typed installation-input evidence is recorded in
[`APOLLO-INSTALLATION-CONFIGURATION.md`](APOLLO-INSTALLATION-CONFIGURATION.md),
and the historical operator/bootstrap contract in
[`PALANTIR-CLOUDPAK-EVIDENCE.md`](PALANTIR-CLOUDPAK-EVIDENCE.md). Together they
require typed input resolution and an explicit namespace → infrastructure →
delivery → applications bootstrap sequence.

## Build versus adopt

We build only the domain layer that is specific to the reconstructed platform:
Product coordinates, dependency resolution, release policy, plan state, and
the eventual agent protocol. We continue to use OCI, Helm, Kubernetes, Git,
OIDC, and an external secret manager.

If the operational requirement is simply Kubernetes reconciliation, Argo CD is
the preferred ready-made engine. It already provides pull-based desired-state
comparison, Helm rendering, health, drift reporting, RBAC, audit, and manual or
automatic sync. The Go control plane can emit Argo CD Applications rather than
own Kubernetes credentials. A custom Helm executor is justified only for
disconnected or product-policy requirements Argo CD cannot meet.

## Milestones

1. **Kernel (implemented):** strict JSON desired state, immutable tag+digest
   validation, exact Releases/Entities, typed literal/preset/secret inputs,
   Universal namespace targeting, deterministic dependency plan, HTTP
   health/validation/plan endpoints, durable single-replica plan lifecycle,
   expiring leases, exact reported state, channel-based Release selection, and
   recall-range exclusion.
2. **Catalog (content boundary implemented):** immutable OCI identities,
   exact-digest availability, normalized security evidence, authenticated
   identities, and durable file-backed persistence are implemented. Registry
   Platform release images publish BuildKit SBOM/provenance plus Sigstore
   keyless signatures. Organization KMS policy, Product artifact signing, and
   a highly available database remain external integrations.
3. **Agent (execution kernel implemented):** outbound-only polling,
   one-plan-at-a-time execution, lease renewal, chart-digest verification,
   Helm timeout/atomic rollback, release observation, and reported state.
   Per-Environment certificate identity, outbound proxy support, rendered
   workload image verification, typed configuration, external secret/registry
   preparation, and active health probing are implemented.
4. **Safety (implemented):** approval attestations, manual/automatic suppression
   windows, Environment escalation, active Plan cancellation, in-attempt
   rollback, vulnerability constraints, and exact-digest artifact availability
   gates, maintenance windows, rollback behavior, and observed-state-aware
   recall roll-off are implemented.
   Audited emergency commands reuse the same Plan/lease protocol while taking
   priority and deliberately bypassing ordinary eligibility constraints; they
   never overlap active work for the same Entity.
5. **Promotion (implemented):** Entities follow Release Channels and
   stable/RC/snapshot Releases receive Apollo-aligned default memberships.
   Immutable channel updates, canary/soak evaluation, timeouts, suppressions,
   and deterministic ramped rollout are implemented.
6. **Disconnected delivery (implemented):** signed, previewable,
   snapshot/incremental/checkpoint Bundles carry governed catalogs and optional
   verified chunked artifacts with safe per-target deduplication.

The agent must never receive a general-purpose cluster-admin credential. Each
Environment gets a distinct identity and namespace-bounded permissions. Product
charts still create no Namespace, ServiceAccount, cluster RBAC, or secret
material.

The preferred production transport consumes rotating X509-SVIDs from the
SPIFFE Workload API. The Hub must present
`spiffe://platform.delivery/hub`; an Agent must present
`spiffe://platform.delivery/environment/<environment>/agent/<agent>`. TLS
authenticates the trust domain and handlers bind the asserted path to the
requested Environment and active lease. Static certificate files are a
migration profile, not the default production identity mechanism.

## Source contracts

- [Argo CD architecture](https://argo-cd.readthedocs.io/en/stable/operator-manual/architecture/)
- [Argo CD declarative setup and OCI Helm](https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/)
- [Argo CD automated sync semantics](https://argo-cd.readthedocs.io/en/stable/user-guide/auto_sync/)
- [GitHub deployment environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
- [Palantir FedStart Helm charts](https://github.com/palantir/fedstart-helm-charts)
- [Palantir Apollo public OpenAPI](https://github.com/palantir/apollo-openapi/blob/develop/openapi/api-definition.yml)
- [Palantir CloudPak operator bundle](https://github.com/palantir/palantir-cloudpak)
