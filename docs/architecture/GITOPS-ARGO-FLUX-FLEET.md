# GitOps architecture study: Argo CD, Flux, Fleet, and Modules

Reviewed against the projects' current official documentation on 2026-08-01.
This document distinguishes the Git source/reconciliation problem from the
Apollo-aligned Product, Release, Module, constraint, approval, and promotion
problem.

## Executive decision

Use Git as an auditable **source of Module definitions**, but do not make a Git
revision the cluster's immediately actionable target state. Publication turns
reviewed definitions into immutable Module Releases and replaces every
`version: local` reference with an exact version in a deterministic lock. An
Environment installation then follows a Module Release through the existing
Plan/constraint/approval machinery.

The implementation should borrow:

- Flux's source/artifact split, digest observation, composable controllers,
  dependency readiness, and service-account impersonation;
- Fleet's manager/agent topology, label-based multi-cluster targeting, and
  readiness-gated rollout partitions;
- Argo CD's application health/status presentation, explicit sync windows, and
  ordered health gates.

It should not install all three systems, adopt three overlapping CRD models, or
replace Apollo-style Plans with unconditional desired-state reconciliation.

## What the projects actually model

| System | Primary desired-state unit | Execution topology | Multi-cluster primitive | Ordering / rollout | Drift behavior |
| --- | --- | --- | --- | --- | --- |
| Argo CD | `Application` generated from Git/Helm/Kustomize/other sources | Central API, repository server, and application controller hold target-cluster credentials | `ApplicationSet` generators create Applications from cluster lists or Git data | Sync phases/waves within an Application; ApplicationSet RollingSync groups Applications and waits for health | Compares live resources with rendered target state; automated self-heal is optional |
| Flux | `Kustomization` and `HelmRelease`, backed by a Source artifact | Composable controllers normally run in each target cluster | Bootstrap/reconcile Flux resources per cluster; repository layout and automation select cluster state | `dependsOn`, health checks, CEL readiness, Helm remediation; Flagger is separate for progressive traffic delivery | Continuous interval-based reconciliation and pruning; suspension pauses source application and drift correction |
| Fleet | `Bundle`, compiled from a `GitRepo` path and deployed as `BundleDeployment` | Manager creates bundles; downstream cluster agents deploy them | Cluster selectors, groups, targets, and per-target customization | Ordered/manual or automatic partitions, `maxNew`, `maxUnavailable`, readiness gates | Agent reports bundle readiness and modified/out-of-sync state; bundle remains desired state |
| This platform | Immutable Product and Module Releases selected for an Environment installation | Hub evaluates constraints; outbound-only Spoke agent leases and executes Plans | Environment/namespace metadata and Module installation targeting | Product dependencies, maintenance/suppression windows, approvals, promotions, recalls; Fleet-like partitions are a future scheduler feature | Reported state is evidence used to propose Plans; drift does not silently bypass approvals |

## Argo CD findings

Argo CD's repository server renders manifests for a repository URL, revision,
path, and template inputs. Its application controller compares those manifests
to live cluster state and can correct drift. Its API server owns application
operations, credentials, authentication, and RBAC.

`ApplicationSet` is useful prior art for generating per-cluster application
instances from cluster inventory and Git. Sync phases and waves provide
resource ordering and health barriers inside an Application. Progressive Syncs
provide label-selected application groups and `maxUpdate`, but their official
documentation still describes changing feature maturity. They should not be a
hard dependency for our promotion contract.

Design consequence: copy the clear status tree and health-gated ordering, not
the requirement that the Hub retain broad Kubernetes credentials for every
target. Our Spoke retains the execution boundary.

Official references:

- <https://argo-cd.readthedocs.io/en/stable/operator-manual/architecture/>
- <https://argo-cd.readthedocs.io/en/stable/user-guide/application-set/>
- <https://argo-cd.readthedocs.io/en/release-3.0/user-guide/sync-waves/>
- <https://argo-cd.readthedocs.io/en/release-3.1/operator-manual/applicationset/Progressive-Syncs/>

## Flux findings

Flux has the cleanest reusable decomposition. Source controllers turn Git,
Helm, OCI, or bucket sources into versioned artifacts. Reconcilers consume
those artifacts through `Kustomization` or `HelmRelease`. `OCIRepository`
records both the upstream tag/revision and SHA-256 digest, and supports an AWS
OIDC provider. `Kustomization.dependsOn` can wait for readiness and optionally
use CEL for lockstep version checks.

Flux also provides the strongest directly applicable tenant boundary: a
reconciler can impersonate a named Kubernetes ServiceAccount, cross-namespace
references and remote bases can be disabled, and namespace-scoped RBAC limits
what a tenant-controlled source can apply.

Design consequence: model Git and ECR as revisioned Source Artifacts with an
observed digest. Feed them to Module publication. Preserve the Spoke's existing
namespace-scoped ServiceAccount instead of granting a cluster-admin controller.

Official references:

- <https://fluxcd.io/flux/concepts/>
- <https://fluxcd.io/flux/components/source/ocirepositories/>
- <https://fluxcd.io/flux/components/kustomize/kustomizations/>
- <https://fluxcd.io/flux/installation/configuration/multitenancy/>

## Fleet findings

Fleet is the closest operational comparison to an Apollo Spoke fleet. A
`GitRepo` is scanned into one or more Bundles; a Bundle becomes a
`BundleDeployment` for each selected downstream cluster. Target customizations
change deployment inputs by cluster. Rollouts can partition clusters in an
explicit order and gate progress on readiness, `maxUnavailable`, and
`maxUnavailablePartitions`; `maxNew` limits how many new deployments are
staged per reconciliation.

Design consequence: adopt Fleet-style rollout partitions above Environments,
not inside a Module definition. A Module says what Products compose a release;
an Environment fleet policy says where and how quickly that release may move.
This also mitigates ECR image-pull storms.

Official references:

- <https://fleet.rancher.io/explanations/gitrepo-content>
- <https://fleet.rancher.io/reference/ref-crds>
- <https://fleet.rancher.io/how-tos-for-users/rollout>
- <https://fleet.rancher.io/next/explanations/target-customization>

## Repository contract

The current implementation follows this flow:

1. Git contains current-format Module YAML under
   `examples/modules/`.
2. CI validates the closed local-composition graph and YAML configuration.
3. Publication sends the definitions as one batch. Dependency order is
   deterministic; local submodules publish first.
4. The Hub creates immutable versions beginning at `0.1.0`. Identical content
   is idempotent; changed content increments the minor version.
5. The generated lock contains exact submodule versions, a flattened Entity
   list, inherited variables, keyed instance paths, recursively merged Entity
   overrides, and a content digest.
6. Installation resolves defaults and constraints, renders the lock, takes
   ownership of unmanaged Entities, and rejects Entities already owned by a
   different active Module installation.
7. Installations track a Release Channel. A later publication creates an
   update proposal only after that Module Release is promoted to the tracked
   channel; recalled targets are excluded. Updates may be paused and resumed.
8. Removing an Entity relinquishes ownership without deleting it. Unlinking a
   Module also leaves its Entities installed.

The Module runtime endpoints are intentionally platform-neutral:

- `POST /v1/modules/releases`
- `GET /v1/modules/{moduleName}/releases`
- `POST /v1/modules/{moduleName}/releases/{version}/promote`
- `POST /v1/modules/{moduleName}/releases/{version}/recall`
- `POST|GET /v1/environments/{environmentID}/module-installations`
- `GET|POST /v1/module-installations/{installationID}/update`
- `POST /v1/module-installations/{installationID}/pause`
- `POST /v1/module-installations/{installationID}/resume`
- `POST /v1/module-installations/{installationID}/unlink`

The compatibility model keeps a declared Entity name separate from its
flattened `instancePath`. Repeating the same submodule requires a unique `key`;
that key scopes ownership without rewriting the author-provided Entity name.
Unkeyed composition retains the underlying Environment Entity identity, so a
second Module cannot accidentally take it over.

## AWS mapping

AWS is an adapter, not a domain fork:

- GitHub Actions assumes a narrowly scoped AWS role through OIDC.
- Workload images and packaged Helm charts are pushed to private ECR
  repositories and admitted by digest.
- EKS runs the namespace-scoped outbound Spoke agent using EKS Pod Identity or
  IRSA. The Hub does not need an inbound Kubernetes API route.
- ECR pull permissions belong to the Spoke workload identity. Cross-account
  distribution uses repository policies and per-Environment registry
  assignments.
- AWS Secrets Manager remains the secret authority; External Secrets or a
  similarly bounded projection controller materializes named Kubernetes
  Secrets required by the Module.
- Namespace, ServiceAccount/RBAC, network policy, ECR assignment, and secret
  projection are Environment bootstrap. Product Modules do not create or own
  them. Platform validates the reviewed namespace inventory, immutable Entity
  placement, default deletion protection, and empty-before-delete evidence;
  Flux applies the separately authorized Namespace lifecycle described in
  `docs/apollo/reference/namespaces.md`.

If Flux is later used as the execution backend, use `OCIRepository` with
`provider: aws` and a namespace-scoped ServiceAccount. The Module/Plan layer
still owns selection, approval, promotion, and recall; Flux only performs the
authorized in-cluster reconciliation.
