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

The pinned Zarf v0.82.0 init archive already carries the required internal OCI
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

`REPOSITORY_KIND` is the one profile switch. The settings that must move with
it are derived from it at deploy time by the `runtime` component's `onDeploy`
actions, so they cannot be set inconsistently by hand.

| | `REPOSITORY_KIND=postgres` | `REPOSITORY_KIND=file` |
| --- | --- | --- |
| Intended use | **Production.** This is the profile the Helm chart defaults to. | Development and evaluation only. |
| Hub replicas | 2 | 1 |
| Rollout | `RollingUpdate`, `maxUnavailable: 0`, `maxSurge: 1` | `Recreate` |
| Hub state | PostgreSQL, DSN from the `dsn` key of `PACKAGING_POSTGRES_SECRET` | The retained `platform-delivery-state` claim |
| PodDisruptionBudget | Keeps one Hub serving through a voluntary disruption | Present, and permits evicting the one Pod |
| Metrics, `uds-metrics`, `packaging-worker` | Available | Not available |

The variable's default is `file`, so that a package deployed with no variables
still starts on a cluster with no database, and an existing file-backed install
upgrades in place. A production deployment sets `REPOSITORY_KIND=postgres`
explicitly; [`uds-config.yaml`](uds-config.yaml) does.

The actions are three POSIX `sh` `case` statements. They run on the host that
runs `zarf package deploy`, which therefore needs `sh`; a Windows deploy host
without one is not supported. Moving an existing install from `file` to
`postgres` does not migrate its data, and the claim is left in place.

`/metrics` needs both `--set-variables REPOSITORY_KIND=postgres` and
`--set-variables METRICS_ADDRESS=:9090`. With those set, select `uds-metrics`
instead of `uds`: it is the same Package custom resource plus a ServiceMonitor.
Selecting it without them registers a scrape target that stays down. The
notification, plan-orchestrator and material-preparer settings the Helm charts
expose are not Zarf variables.

### One definition, checked against the other

The package deploys `manifests/runtime.yaml`, not the Helm charts. Switching
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
longer occurs fails too. One listed difference is a known gap rather than a
choice: the packaged Hub is given no Temporal frontend address, so it cannot
start packaging workflows; the `packaging-worker` component only executes
workflows another client starts.

## Declared external dependencies

The package carries exactly two images, the Hub and the Spoke Agent, each by
platform-manifest digest. Everything else a full system needs is supplied by
the Environment and is declared here. Nothing in this table is bundled, pulled
or version-pinned by the package.

| Dependency | Needed when | How it is supplied on UDS | Contract |
| --- | --- | --- | --- |
| PostgreSQL | `REPOSITORY_KIND=postgres` (production), `packaging-worker`, metrics | An in-cluster PostgreSQL operator or package the Environment already runs, or a managed database outside the cluster. This package names no package coordinates because none has been tested with it. | Secret named by `PACKAGING_POSTGRES_SECRET` (default `platform-postgres`) in `platform-system` with key `dsn`, one line. With the `uds` component, in-cluster Pods are selected by `POSTGRES_NAMESPACE` and `POSTGRES_NAME_LABEL`; a database outside the cluster needs its own `remoteHost` allow. |
| Temporal | `packaging-worker` only | A Temporal frontend the Environment already runs, in-cluster or managed. No package coordinates are named, for the same reason. | `PACKAGING_TEMPORAL_ADDRESS` is the frontend `host:port`, plaintext, namespace `default`. With the `uds` component, in-cluster Pods are selected by `TEMPORAL_NAMESPACE` and `TEMPORAL_NAME_LABEL`. |
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

Conduit pins Zarf v0.82.0 for package validation and construction. Package
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
