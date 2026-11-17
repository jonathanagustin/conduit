# Zarf offline-bundle package

This package carries the product-agnostic Conduit Hub, Spoke Agent, and their
runtime manifest to a disconnected site. It deliberately does not contain Product
catalogs, Product charts or images, Environment credentials, application
namespaces, registry assignments, or installation configuration.

The package expects an initialized Zarf cluster (or a supported external Zarf
registry configuration). Initialize Zarf with
`--agent-mutation-policy=labeled`. Zarf otherwise defaults to mutating every
Pod image, which would improperly take over the Spoke's Product registry
contract. The package labels only `platform-system` for Zarf mutation. If an
existing cluster was initialized with the default `all` policy, change that
policy or label every consumer-owned application namespace
`zarf.dev/agent=ignore` before starting Conduit.

The pinned Zarf v1.0.0 init archive already carries the required internal OCI
registry (OCI Distribution `registry` 3.1.1) and Zarf Agent. It also carries
optional K3s 1.34.3+k3s1 and Gitea 1.24.6 components. On a bare supported Linux
host, initialize all three facilities with:

```sh
zarf init --components=k3s,git-server --agent-mutation-policy=labeled
```

On an existing Kubernetes cluster, omit `k3s`; omit `git-server` when an
Environment-owned Git service already exists. The internal registry can also
be replaced by Zarf's supported external-registry flags. These are Zarf-owned
bootstrap services, not resources duplicated by the Conduit runtime package.

The optional `uds` component applies a UDS Package custom resource for the
Hub: a Keycloak client and network allows, including egress to the OIDC
issuer, the chart registry and Temporal. It is not selected by
`zarf package deploy --confirm`. Select it with `--components uds` only on a
cluster that already has UDS Core. A plain Zarf install leaves the component
out. The commands and the limits of that check are in
[the UDS package note](../../docs/operations/uds-package.md).

## Profiles

`REPOSITORY_KIND` is the profile switch. The settings that must move with it
are derived at deploy time by the `runtime` component's `onDeploy` actions, so
they cannot be set inconsistently by hand.

| | `REPOSITORY_KIND=postgres` | `REPOSITORY_KIND=file` |
| --- | --- | --- |
| Intended use | **Production.** This is the profile the Helm chart defaults to. | Development and evaluation only. |
| Hub replicas | 2 with `PLANNING_TEMPORAL_ADDRESS` set, otherwise 1 | 1 |
| Rollout | `RollingUpdate`, `maxUnavailable: 0`, `maxSurge: 1` | `Recreate` |
| Hub state | PostgreSQL, DSN from the `dsn` key of `PACKAGING_POSTGRES_SECRET` | The retained `platform-delivery-state` claim |
| PodDisruptionBudget | Keeps one Hub serving through a voluntary disruption | Present, and permits evicting the one Pod |
| Metrics, `uds-metrics`, `packaging-worker` | Available | Not available |

The variable's default is `file`, so that a package deployed with no variables
still starts on a cluster with no database, and an existing file-backed install
upgrades in place. A production deployment sets `REPOSITORY_KIND=postgres`
and `PLANNING_TEMPORAL_ADDRESS` explicitly; [`uds-config.yaml`](uds-config.yaml)
does.

A second Hub replica needs more than a shared database. Planning sessions
live in the Hub process unless `PLANNING_TEMPORAL_ADDRESS` names a Temporal
frontend, and the Hub refuses to start as one of several replicas without one
(the Helm chart refuses to render that combination). So the replica count is
derived from both variables: `postgres` with a planning frontend runs two
replicas; `postgres` without one runs one, still on PostgreSQL with a rolling
update. The Hub is passed the derived count as `HUB_REPLICA_COUNT`, so its own
startup check sees what actually runs.

The package does not offer app-generation compute. The chart turns it on by
default (`planning.compute`: a Docker image builder, a workspace root and a
45-minute HTTP write timeout); it needs a Docker daemon and a `zarf` binary
beside the Hub, and neither is in this package. The packaged Hub sets none of
those three, and the equivalence gate renders the chart with them emptied.

The actions validate the packaging profile and derive the profile and
operational settings. They run on the deploying host, which therefore needs
POSIX `sh` and `sed`; a Windows deploy host without them is not supported.
Moving an existing install from `file` to
`postgres` does not migrate its data, and the claim is left in place.

`/metrics` needs both `--set-variables REPOSITORY_KIND=postgres` and
`--set-variables METRICS_ADDRESS=:9090`. With those set, select `uds-metrics`
instead of `uds`: it is the same Package custom resource plus a ServiceMonitor.
A deploy guard refuses that component unless both are set. The `uds` and
`uds-metrics` components share a Zarf choice group: requesting both (including
`--components '*'`) fails selection before any component action or manifest.
The group's default `uds-disabled` member has no assets or actions, preserving
a plain `--confirm` install with no UDS CR. This uses the pinned Zarf v1.0.0
`group` field; it is deprecated upstream and needs migration before a future
schema removes it. No UDS Core installation is implied by selecting a component.

### Typed operational variables

These native deployment names map to the existing Helm settings. They contain
configuration and existing object references; never pass credential contents.
Disabled transports and preparers add no environment entries, mounts or volumes.

| Zarf variables | Helm setting / behavior |
| --- | --- |
| `NOTIFICATION_TRANSPORT` (`none`, `webhook`, `log`, `native`) | `notifications.transport`; enabled delivery requires PostgreSQL. Default `none`. |
| `NOTIFICATION_WEBHOOK_SECRET_NAME`, `NOTIFICATION_WEBHOOK_SECRET_KEY` | `notifications.webhook.urlSecretName` / `urlSecretKey`; the full URL comes from the named Secret via `secretKeyRef`, never a variable. Key defaults to `url`; name is empty. |
| `NOTIFICATION_WEBHOOK_TIMEOUT` | `notifications.webhook.timeout`; empty preserves the Hub default. |
| `NOTIFICATION_NATIVE_TIMEOUT` | `notifications.native.timeout`; a positive `ms` or `s` duration below the Hub's 30s delivery lease. |
| `NOTIFICATION_SMTP_ADDRESS`, `NOTIFICATION_SMTP_FROM`, `NOTIFICATION_SMTP_TLS_MODE` | `notifications.native.smtp.address`, `from`, `tlsMode`; native SMTP requires a sender, and supports STARTTLS or implicit TLS. |
| `NOTIFICATION_SMTP_CREDENTIALS_SECRET_NAME`, `NOTIFICATION_SMTP_USERNAME_KEY`, `NOTIFICATION_SMTP_PASSWORD_KEY` | Native SMTP credentials mounted read-only at `/run/notification-smtp`; keys default to `username` and `password`. Empty Secret name uses no SMTP authentication. |
| `NOTIFICATION_SMTP_CA_CONFIGMAP_NAME`, `NOTIFICATION_SMTP_CA_KEY` | Optional SMTP CA mounted read-only at `/run/notification-smtp-ca/ca.pem`; key defaults to `ca.pem`. |
| `NOTIFICATION_SLACK_TOKEN_SECRET_NAME`, `NOTIFICATION_SLACK_TOKEN_KEY` | Native Slack token mounted read-only at `/run/notification-slack/token`; key defaults to `token`. |
| `PLAN_ORCHESTRATOR`, `PLAN_TEMPORAL_ADDRESS` | `planOrchestrator.driver` / `temporalAddress`; default `native`. Zarf requires an explicit address for `temporal`, with no fallback to another workflow address. The current Hub uses plaintext and Temporal namespace `default`. |
| `MATERIAL_PREPARER_COMMAND`, `MATERIAL_PREPARER_TOOLS_PVC`, `MATERIAL_PREPARER_TOOLS_MOUNT_PATH` | Spoke `materialPreparer.command`, `toolsPersistentVolumeClaimName`, `toolsMountPath`; no executable or PVC is chosen by default. The optional PVC is read-only. Mount path defaults to `/opt/platform-material`. |

All twenty operational variables reject C0/C1 control characters, Unicode line
and paragraph separators, and characters that YAML cannot carry literally.
Valid Unicode sender display names, including nonbreaking spaces, remain
byte-preserving through variable admission, action quoting and YAML parsing;
settings are not silently normalized. The pinned-tool regression exercises
those boundaries for every variable, including NUL supplied as a package
default (it cannot be passed in a process argument or environment).

All referenced objects belong to the operator in `platform-system`. The shipped
Spoke image supplies no material preparer; configure an executable actually
present in the chosen immutable image or mounted tools PVC. A configured path
does not prove that executable exists or works. SMTP, Slack and webhook delivery
still need outbound connectivity; the UDS Package grants no notification
endpoint egress by default. The operator supplies policy for the chosen
endpoints. A custom Plan Temporal port similarly needs matching UDS egress.

Runtime deploy actions check cross-setting requirements before the runtime
manifests. The metrics guard runs before its own UDS component manifests;
this is not an atomic rollback of a runtime deployed earlier in that command.

`uds-observability` is a further optional component: a `PrometheusRule` with
the Hub's alert rules and a ConfigMap, labelled `grafana_dashboard: "1"`, with
the reference dashboard. Select it together with `uds-metrics`; without the
Hub's metrics every rule but `ConduitMetricsAbsent` is blind. Its manifest is
generated from [`deploy/observability`](../observability) by
`scripts/verify-observability-assets.rb --write`. It has not been applied to a
cluster: that UDS Core's Prometheus and Grafana pick both up from
`platform-system` is assumed, not verified.

The six `RATE_LIMIT_<CLASS>_RPS` and `RATE_LIMIT_<CLASS>_BURST` variables set
the Hub's request limits for the operator, agent and anonymous classes. Each
defaults to empty, which keeps the Hub's built-in limit; none turns the
limiter off.

### One definition, checked against the other

The package deploys `manifests/runtime.yaml`, not the Helm charts. Seven
baseline and operational profiles compare the real Zarf substitution with Helm,
including webhook Secret references, native SMTP/Slack file mounts, Plan
orchestration, and the preparer with and without a PVC. No new differences are
allowlisted for those settings. `scripts/test-zarf-operational-settings.rb`
creates an action-only package with the source variables, selection metadata
and guards, and runs pinned Zarf to check defaults, conflicts and invalid
settings without contacting a cluster. These checks do not prove a scrape or
notification delivery.

The package deploys the runtime directly. Switching
the components to `charts:` was considered and rejected for now: the charts
read the mTLS CA from a ConfigMap where this package's Secret contract carries
it as `ca.crt`, have no mount for the UDS trust bundle, and select Pods with
labels an installed package's immutable Deployment selectors do not have, so an
existing install could not upgrade.

Instead `scripts/verify-zarf-chart-equivalence.rb` resolves the manifests for
each profile exactly as a deployment would, renders `deploy/helm/platform-hub`
and `deploy/helm/platform-spoke` with the values that profile stands for, and
fails on any difference not listed with a reason in
[`chart-equivalence.yaml`](chart-equivalence.yaml). A listed difference that no
longer occurs fails too. Packaging's enabled profile compares a non-default
frontend, namespace and task queue on both the Hub and worker. The disabled
profiles keep an empty Hub address, so a plain install starts no packaging
Temporal client.

### Packaging workflows

Packaging is configured separately from planning. `PACKAGING_TEMPORAL_ADDRESS`
now defaults to empty, replacing the old worker-only `127.0.0.1:7233` default.
An empty address disables the Hub's packaging workflow status and governed
cancel/rerun client. It does not start a worker or submit an application. The
optional `packaging-worker` executes activities for workflows submitted through
the existing packaging entry points.

To select that worker, explicitly set `REPOSITORY_KIND=postgres` and a
nonempty `PACKAGING_TEMPORAL_ADDRESS`; a deploy-time action refuses an absent
frontend or a file-backed Hub. The Hub and worker read the same address,
`PACKAGING_TEMPORAL_NAMESPACE` (default `default`) and
`PACKAGING_TEMPORAL_TASK_QUEUE` (default `packaging-localhost`). DNS/IPv4 and
bracketed numeric IPv6 endpoints are supported, including IPv4-mapped IPv6 and
interface zones such as `[fe80::1%eth0]:7233`; ports must be `1` through
`65535`. Existing deployments that explicitly set such an address keep that
endpoint. Deployments
that relied on the old implicit loopback endpoint must now name their real
frontend. These settings do not change the Hub's replica count or planning
frontend.

Namespace and task-queue variables have no package-specific length or
alphanumeric restriction. They must be nonempty and exclude double quotes,
backslashes and Unicode `Cc` control characters, which cannot be copied safely into these
quoted manifests. These are Conduit package-input restrictions, not a complete
Temporal naming contract; the configured server enforces its own names.

The connection this package exposes is **plaintext and has no Temporal API
key**. Supply an in-cluster frontend whose network path is protected by the
deployment, for example by its service mesh. Do not put credentials or a URI
scheme in the address. TLS/mTLS and API-key file settings exist in the Hub and
Helm chart but are not exposed by this package; an authenticated or TLS-only
Temporal endpoint needs those deployment mechanisms instead. This change adds
no egress policy: the existing Hub/worker allows select the in-cluster frontend
named by `TEMPORAL_NAMESPACE` and `TEMPORAL_NAME_LABEL`, on port `7233`. A
different destination or port needs a separately reviewed policy.

An explicitly configured Hub can also use an external worker without selecting
the optional component. The operator must give that worker the same Temporal
namespace, task queue and PostgreSQL Plan store. The example UDS bundle does not
select the worker and leaves its packaging address empty.

## Declared external dependencies

The package carries exactly two images, the Hub and the Spoke Agent, each by
platform-manifest digest. Everything else a full system needs is supplied by
the Environment and is declared here. Nothing in this table is bundled, pulled
or version-pinned by the package.

| Dependency | Needed when | How it is supplied on UDS | Contract |
| --- | --- | --- | --- |
| PostgreSQL | `REPOSITORY_KIND=postgres` (production), `packaging-worker`, metrics | An in-cluster PostgreSQL operator or package the Environment already runs, or a managed database outside the cluster. This package names no package coordinates because none has been tested with it. | Secret named by `PACKAGING_POSTGRES_SECRET` (default `platform-postgres`) in `platform-system` with key `dsn`, one line. With the `uds` component, in-cluster Pods are selected by `POSTGRES_NAMESPACE` and `POSTGRES_NAME_LABEL`; a database outside the cluster needs its own `remoteHost` allow. |
| Temporal | Two Hub replicas (production), and explicitly enabled packaging client/worker | A Temporal frontend the Environment already runs, in-cluster or managed, which must persist its own history. No package coordinates are named, for the same reason. | `PLANNING_TEMPORAL_ADDRESS` is the frontend `host:port` the Hub replicas share for planning sessions (`PLANNING_TEMPORAL_NAMESPACE`, `PLANNING_TEMPORAL_TASK_QUEUE`). Packaging's Hub client and optional worker share `PACKAGING_TEMPORAL_ADDRESS`, `PACKAGING_TEMPORAL_NAMESPACE` and `PACKAGING_TEMPORAL_TASK_QUEUE`; the address is empty by default and this package supports plaintext without API credentials only. With the `uds` component, in-cluster Pods are selected by `TEMPORAL_NAMESPACE` and `TEMPORAL_NAME_LABEL` for both the Hub and the worker. |
| Workspace editor image | Never, in this package | Not applicable. | The Hub's developer-workspace Pods default to the image `workspace-editor:local`, which is not in the package. The packaged Hub does not set `DEVWORKSPACE_PODS`, so it never starts one, and the validator refuses a manifest that does. |
| Product charts and images | Every Plan the Spoke executes | The OCI registry the Plans name, normally the Zarf registry. | Consumer-owned; see the first paragraph. |

`packaging-worker` requires `REPOSITORY_KIND=postgres`. The worker writes Plans
to PostgreSQL and a file-backed Hub never reads them, so the component's
`onDeploy` action refuses the deployment, as the Helm chart refuses
`packagingWorkflow.worker.enabled` with `repository.kind=file`.

## UDS bundle

[`uds-bundle.yaml`](uds-bundle.yaml) wraps this package with the `uds`
component selected, for a cluster that already has UDS Core 1.17.1. It does
not contain UDS Core. [`uds-config.yaml`](uds-config.yaml) is the example
deployment configuration: it carries `ENVIRONMENT_ID`, `AGENT_ID`,
`BOOTSTRAP_ADMINISTRATORS`, the profile, the Secret names and the
`OPERATOR_OIDC_*` settings as package variables, and no secret values.

```sh
uds create deploy/zarf --confirm
UDS_CONFIG=uds-config.yaml uds deploy uds-bundle-conduit-amd64-<version>.tar.zst --confirm
```

The UDS CLI is pinned at v0.41.0 by checksum in the Trunk gate, which runs
`uds create` on the bundle without a cluster. Release Please bumps the
bundle's version with `VERSION`. The limits of what has been run are in
[the UDS package note](../../docs/operations/uds-package.md).

## Environment-owned Secrets

The model provider is configured with the `MODEL_*` variables, all of which
default to no model call. The API key is the `api-key` key of
`MODEL_API_KEY_SECRET`, mounted as a file. The variables, the UDS egress allow
for an in-cluster model, and the AI capabilities the shipped image cannot run
are in [HTTP model endpoint](../../docs/operations/model-http.md).

Create three Environment-owned Secrets in `platform-system`:

| Secret | Required keys | Ownership |
| --- | --- | --- |
| `platform-hub-tls` | `ca.crt`, `tls.crt`, `tls.key` | Hub server identity and trusted Agent-client CA |
| `platform-agent-tls` | `ca.crt`, `tls.crt`, `tls.key` | Hub CA and Agent client identity |
| `platform-bundle-signing` | `bundle-signing-key.pem` | Persistent Ed25519 PKCS#8 Bundle signing key |

Names can be overridden at deploy time. The Hub certificate must cover
`platform-delivery.platform-system.svc.cluster.local`; the Agent certificate
must carry the Environment/Agent identity required by the control-plane TLS
contract. Secret bytes are never baked into the package.

Each application namespace remains consumer-owned and needs a bounded Role and
RoleBinding for the `platform-system/platform-spoke-agent` ServiceAccount.
Deploying this package does not grant cluster-admin or create application
namespaces.

## Release artifacts

Tagged releases attach one package per supported architecture and its SHA-256
file. Each archive contains Zarf's native `zarf.bundle.sig` Sigstore bundle,
signed keylessly by the release workflow. Transfer those files, the pinned Zarf CLI/init
package appropriate for the destination, and any Product OCI artifacts through
the approved media process. Verify the package before deployment, initialize
the destination if needed, then deploy:

```sh
sha256sum --check zarf-package-platform-runtime-amd64-0.11.0.tar.zst.sha256
zarf package verify zarf-package-platform-runtime-amd64-0.11.0.tar.zst \
  --certificate-identity 'https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v0.11.0' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
zarf package deploy zarf-package-platform-runtime-amd64-0.11.0.tar.zst \
  --verify=always \
  --certificate-identity 'https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v0.11.0' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --set-variables ENVIRONMENT_ID=example \
  --set-variables AGENT_ID=spoke-example \
  --confirm
```

The publication workflow also pushes the same signed packages to the
repository's OCI namespace, after verifying each against the workflow's keyless
identity, and verifies them again as read back from the registry:

```sh
zarf package deploy oci://ghcr.io/jonathanagustin/conduit/packages/platform-runtime:0.11.0 \
  --verify=always \
  --certificate-identity 'https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v0.11.0' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --set-variables ENVIRONMENT_ID=example \
  --set-variables AGENT_ID=spoke-example \
  --confirm
```

That job was added after the last release, so no release has been published
this way yet: the first tag cut with it is the first test of the push, of
whether the registry keeps both architectures under one version tag, and of
whether the signature survives the round trip. The job fails if either
read-back verification does.

The dispatch-only `UDS package lifecycle` workflow
(`scripts/test-zarf-package-lifecycle.sh`) installs release N-1 from its
Release asset, upgrades to N from the registry, removes the package and
asserts the Hub state claim is the same object throughout, verifying each
signature against its own release's identity. **It has never been run.** It
needs a published release N and a cluster, and it does not block node egress
or show a Spoke completing a lease.

Only one flavor of the package exists;
[ADR 0017](../../docs/decisions/0017-uds-package-flavors.md) records that and
what another would require, without deciding. `task zarf:lint`,
`task zarf:create`, `task uds:create` and `task zarf:lifecycle` wrap the
commands on this page.

Conduit pins Zarf v1.0.0 for package validation and construction. Package
creation runs once for `amd64` and once for `arm64`; both bundled images
are addressed by the exact platform-manifest digest selected from a pinned OCI
index, and Zarf produces its normal image SBOM inventory. The release workflow
derives Hub and Spoke indexes from registry-reported digests rather than tags.

Upstream references, reviewed 2026-08-02:

- [Zarf packages](https://docs.zarf.dev/ref/packages/)
- [Creating a package](https://docs.zarf.dev/commands/zarf_package_create/)
- [Components, images, and manifests](https://docs.zarf.dev/ref/components/)
- [v1alpha1 package schema](https://docs.zarf.dev/schema/v1alpha1-package/)
- [Package signing and offline verification](https://docs.zarf.dev/ref/package-signing/)

See [SOURCE-REVIEW.md](SOURCE-REVIEW.md) for the pinned source audit and the
load-bearing package decisions derived from it.
