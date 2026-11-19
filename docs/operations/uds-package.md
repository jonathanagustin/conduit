# UDS package and a plain Zarf install

The Zarf package `platform-runtime` carries the Hub, the Spoke Agent, and an
optional UDS Package custom resource. The `uds` component is not selected by
`zarf package deploy --confirm`. Select it with `--components uds` only after
UDS Core is already installed. The Package CR registers a Keycloak client for
the console and allows the traffic below. A ServiceMonitor is declared only
when `UDS_METRICS` is true on that same component; see [Metrics](#metrics).

The connected walkthroughs below use historical v1alpha1 release archives.
Their recorded variable flags are retained as evidence, not instructions for
the current beta source. For current native package/bundle commands, use the
[package README](../../deploy/zarf/README.md) and
[qualification record](zarf-1.0-qualification.md).

Those historical steps were run on a connected machine. They pull the Zarf init package
and, for the UDS path, the published `k3d-core-slim-dev` bundle from a public
registry. They are not an offline-bundle install and they do not show a disconnected
cluster operating after the images are already present.

## What the Package CR grants

- SSO client `conduit-hub`, a confidential client whose one redirect URI is
  `https://conduit.uds.dev/v1/auth/callback` by default
  (`CONSOLE_PUBLIC_URL` + `/v1/auth/callback`), registered exactly and with
  no wildcard. UDS Core writes the generated client secret to the
  `conduit-hub-sso` Secret under the key `client-secret`; the Hub mounts
  that key read-only at `/run/conduit-sso`. Authservice is not attached.
  The Hub port is the Agent mTLS listener, and a proxy in front of it would
  break the Spoke.
- Ingress from the tenant gateway (`istio-tenant-gateway` /
  `tenant-ingressgateway`) to the Hub on 8081, plus an `expose` entry on the
  tenant gateway at host `conduit`. The Hub speaks HTTPS on that port. The
  route object is created; a browser session through the gateway is a
  separate check.
- Ingress from the Spoke to the Hub on 8081, and the matching egress.
- Egress from the Hub and the packaging worker to operator-supplied Postgres
  on 5432. The namespace and `app.kubernetes.io/name` value default to
  `postgres` (`POSTGRES_NAMESPACE`, `POSTGRES_NAME_LABEL`). The Hub opens a
  database connection only with `REPOSITORY_KIND=postgres`, the production
  profile; the default `file` profile is for development and evaluation and
  never uses this allow. See
  [Profiles](../../deploy/zarf/README.md#profiles).
- Egress from the Hub to the OIDC issuer, by two in-cluster routes. One is the
  tenant ingress gateway Pods on 443 and 8443, which is where an issuer such
  as `https://sso.uds.dev/realms/uds` resolves inside the cluster. The other
  is the Keycloak Pods on 8080 (`OIDC_ISSUER_NAMESPACE`,
  `OIDC_ISSUER_NAME_LABEL`, both defaulting to `keycloak`). An issuer outside
  the cluster is not covered and needs its own `remoteHost` allow.
- Egress from the Spoke to the OCI registry it runs `helm pull oci://`
  against, on 443 and 5000 (`CHART_REGISTRY_NAMESPACE`,
  `CHART_REGISTRY_LABEL_KEY`, `CHART_REGISTRY_LABEL_VALUE`, defaulting to
  `zarf` and `app: docker-registry`). Set them to wherever the Plans' charts
  live.
- Egress from the Hub to that same Temporal on 7233, for the planning
  sessions two Hub replicas share (`PLANNING_TEMPORAL_ADDRESS`).
- Egress from the packaging worker to operator-supplied Temporal on 7233
  (`TEMPORAL_NAMESPACE`, `TEMPORAL_NAME_LABEL`, both defaulting to
  `temporal`).
- Egress from the Hub to an in-cluster model endpoint
  (`MODEL_SERVICE_NAMESPACE`, `MODEL_SERVICE_NAME_LABEL`). Both default to
  `conduit-model-unconfigured`, which names no namespace, so the policy grants
  nothing until an operator sets them. The entry lists no port. See
  [HTTP model endpoint](model-http.md#zarf-and-uds).
- Egress from the Spoke to the Kubernetes API, kubelet probes to the Hub, and
  DNS to `kube-dns` in `kube-system`.

The issuer, registry, Temporal and model allows are checked for shape only: they
validate against the UDS Core 1.17.1 Package schema and Zarf substitutes
their variables, but no cluster run has shown the generated NetworkPolicies
admitting that traffic. Their default selectors (the Keycloak namespace, label
and port; the Zarf registry namespace, label and port; which gateway port a
policy must name) are recalled UDS Core and Zarf defaults, not values read
from this repository. Confirm them against the cluster with
`uds zarf tools kubectl get pods -A --show-labels` before relying on them.
- No metrics monitor. The default Hub uses the file repository, which serves
  no `/metrics`, so a ServiceMonitor would be a target that is down for the
  life of the deployment.

## Metrics

`/metrics` starts only when `METRICS_ADDRESS` is set and
`REPOSITORY_KIND=postgres`. The file repository has no queue statistics, and
exporting zeroes would report an idle fleet. Both are deploy variables, and
both default to off:

For current beta packages, put these settings in a native YAML values file:

```yaml
ENVIRONMENT_ID: example
AGENT_ID: spoke-example
REPOSITORY_KIND: postgres
METRICS_ADDRESS: ':9090'
PACKAGING_POSTGRES_SECRET: platform-postgres
UDS_METRICS: true
```

Select `--components uds` and pass the file with `--values`, retaining the
release signature policy from the README. One optional component owns the
Package CR; its metrics branch adds the ServiceMonitor. There is no choice
group or second UDS owner. The deploy guard requires PostgreSQL and the metrics
listener. This does not make all component deployments atomic. Optional
`uds-observability` requires the same typed metrics setting.

Native template and action-only gates cover selection, admission and manifest
shape. They contain no cluster resources and do not establish a live scrape.
The remaining #838 acceptance still requires a recorded UDS Core scrape.

## Alert rules and dashboard

Add `uds-observability` to `--components` beside `uds-metrics` to apply the
Hub's `PrometheusRule` and its dashboard ConfigMap (labelled
`grafana_dashboard: "1"`) into `platform-system`. The series, the alerts and
their runbooks are described in [`hub-observability.md`](hub-observability.md)
and [`runbooks.md`](runbooks.md).

This is also checked by rendering only. The manifest is generated from
`deploy/observability` and a gate holds it to its sources and to the series
the Hub exports. That UDS Core's Prometheus loads a rule, and its Grafana a
dashboard, from this namespace is assumed from the kube-prometheus-stack
conventions; no cluster run has shown either, and no rule has been evaluated.

## Tool versions used here

- Zarf CLI `v1.0.0` for package create, lint, and the plain install.
- UDS CLI `v0.41.0`, pinned by checksum in the Trunk gate
  (`Install pinned UDS CLI` in `.github/workflows/platform-ci.yml`). On the
  UDS cluster, deploy the Conduit package with `uds zarf` so the CLI matches
  the Zarf agent that slim-dev installed. That vendored Zarf is `v0.89.0`
  (`uds zarf version`), not the `v1.0.0` that builds the package; the gate
  shows the newer one reading a package the older one built, and nothing
  shows it deploying one. `mise.toml` does not pin the UDS CLI: adding it
  needs `mise lock` against the GitHub API to record release asset IDs.
- k3d `v5.9.0` for the plain cluster. The UDS path uses the k3d packaged in
  `uds-k3d` 0.21.4 (its offline variant), which the slim-dev bundle pins.
- Slim-dev bundle
  `oci://ghcr.io/defenseunicorns/packages/uds/bundles/k3d-core-slim-dev:1.17.1`.
  Bring it up with `mise run uds-up`. See [local cluster selection](local-uds-cluster.md)
  for Podman on macOS, the Docker API endpoint and isolated task kubeconfigs.
  That task creates a user-defined network named `uds-k3d` by default,
  which is required when the Docker daemon has
  no default bridge, and passes `--network uds-k3d` plus flannel `host-gw`
  through `K3D_EXTRA_ARGS`. Some kernels cannot create a vxlan device.
  After deploy, the task clears hostPorts on the nginx DaemonSet and sets
  Istio CNI `AMBIENT_ENABLED=false`, which this kernel has required.
  Tear the cluster down with `mise run uds-down`.

`deploy/zarf/bundle.uds.hcl` is the current native Conduit half of that pair. Slim-dev
embeds its Core packages inside the published bundle, so this file does not
re-pin them. Create it only after `zarf package create` has produced a
package whose version matches `VERSION`.

### UDS Core version

The deploy path targets one UDS Core version, **1.17.1**. `scripts/uds-up.sh`
pins the slim-dev bundle at 1.17.1, `workspace-schemas/v1` vendors the 1.17.1
Package schema, and both Package CRs in `deploy/zarf/manifests` are written
against it. That is a pairing, not a tested matrix: no other Core version has
been deployed with this package, and the 1.17.1 pairing itself has only the
connected-machine evidence this page describes.

`docs/evidence/uds-packaging/` names UDS Core **1.15.0** instead. It is a
frozen reference corpus for a different question, what a generated tenant
package must satisfy, and 1.15.0 is the normative target its sources were
pinned at. It is not a second deploy target for this package and is left as
recorded.

## Images

Package create needs digest-pinned Hub and Spoke images. A release package
uses the registry-reported digests from the image workflow. A local package
can use images built from `Containerfile` and `Containerfile.agent` and
pushed to a registry the create step can read. `--plain-http` is only for
that local registry.

```sh
VERSION="$(tr -d '\n' < VERSION)"
sudo docker build -f Containerfile -t "conduit-hub:${VERSION}" \
  --build-arg "VERSION=${VERSION}" \
  --build-arg COMMIT="$(git rev-parse HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" .
sudo docker build -f Containerfile.agent -t "conduit-spoke-agent:${VERSION}" \
  --build-arg "VERSION=${VERSION}" \
  --build-arg COMMIT="$(git rev-parse HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --build-arg GO_BUILD_JOBS=1 .

sudo docker run -d --name conduit-registry -p 5000:5000 registry:2
sudo docker tag "conduit-hub:${VERSION}" "localhost:5000/conduit-hub:${VERSION}"
sudo docker tag "conduit-spoke-agent:${VERSION}" "localhost:5000/conduit-spoke-agent:${VERSION}"
sudo docker push "localhost:5000/conduit-hub:${VERSION}"
sudo docker push "localhost:5000/conduit-spoke-agent:${VERSION}"
HUB_IMAGE="$(sudo docker buildx imagetools inspect --format '{{json .Manifest}}' "localhost:5000/conduit-hub:${VERSION}" | python3 -c 'import json,sys; print("localhost:5000/conduit-hub@"+json.load(sys.stdin)["digest"])')"
```

`imagetools inspect` against a plain HTTP registry needs the daemon's
registry config or a `crane` digest read. The command that produced the
digests in the evidence log is the one to copy; do not invent a digest.

```sh
zarf package create deploy/zarf \
  --architecture amd64 \
  --confirm \
  --plain-http \
  --skip-sbom \
  --output /tmp/conduit-zarf \
  --set "PLATFORM_VERSION=${VERSION}" \
  --set "HUB_IMAGE=${HUB_IMAGE}" \
  --set "SPOKE_AGENT_IMAGE=${SPOKE_IMAGE}"
```

`--skip-sbom` is a local-validation shortcut. A release package still
generates the SBOM in the image workflow. The resulting archive is not a
signed release artifact.

## Plain Zarf install

Use a new k3d cluster. Do not point this at a cluster that already has
workloads you need.

```sh
k3d cluster delete conduit-zarf >/dev/null 2>&1 || true
k3d cluster create conduit-zarf --wait
export KUBECONFIG="$(k3d kubeconfig write conduit-zarf)"

zarf init --confirm --agent-mutation-policy=labeled

WORKDIR="$(mktemp -d)"
(
  cd "${WORKDIR}"
  openssl req -x509 -newkey rsa:2048 -nodes -keyout ca.key -out ca.crt -days 30 \
    -subj "/CN=conduit-dev-ca"
  openssl req -newkey rsa:2048 -nodes -keyout hub.key -out hub.csr \
    -subj "/CN=platform-delivery.platform-system.svc.cluster.local"
  printf 'subjectAltName=DNS:platform-delivery.platform-system.svc.cluster.local,DNS:platform-delivery.platform-system.svc,DNS:platform-delivery\nextendedKeyUsage=serverAuth\n' > hub.ext
  openssl x509 -req -in hub.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out hub.crt -days 30 -extfile hub.ext
  openssl req -newkey rsa:2048 -nodes -keyout agent.key -out agent.csr \
    -subj "/CN=spoke-example"
  printf 'extendedKeyUsage=clientAuth\n' > agent.ext
  openssl x509 -req -in agent.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out agent.crt -days 30 -extfile agent.ext
  openssl genpkey -algorithm Ed25519 -out bundle-signing-key.pem
)
kubectl create namespace platform-system
kubectl label namespace platform-system zarf.dev/agent=mutate
kubectl -n platform-system create secret generic platform-hub-tls \
  --from-file=ca.crt="${WORKDIR}/ca.crt" \
  --from-file=tls.crt="${WORKDIR}/hub.crt" \
  --from-file=tls.key="${WORKDIR}/hub.key"
kubectl -n platform-system create secret generic platform-agent-tls \
  --from-file=ca.crt="${WORKDIR}/ca.crt" \
  --from-file=tls.crt="${WORKDIR}/agent.crt" \
  --from-file=tls.key="${WORKDIR}/agent.key"
kubectl -n platform-system create secret generic platform-bundle-signing \
  --from-file=bundle-signing-key.pem="${WORKDIR}/bundle-signing-key.pem"
rm -rf "${WORKDIR}"

zarf package deploy /tmp/conduit-zarf/zarf-package-platform-runtime-amd64-0.17.5.tar.zst \
  --confirm \
  --set-variables ENVIRONMENT_ID=example \
  --set-variables AGENT_ID=spoke-example

kubectl -n platform-system rollout status deploy/platform-delivery --timeout=180s
kubectl -n platform-system rollout status deploy/platform-spoke-agent --timeout=180s
kubectl -n platform-system get deploy,pods
kubectl -n platform-system get package
```

The last command must fail or return nothing on this cluster: the `uds`
component was not selected, and the UDS CRDs are absent. Delete the cluster
when the check is done: `k3d cluster delete conduit-zarf`.

## UDS Core slim-dev, then the Package CR

Run this only after the plain cluster is gone. Slim-dev creates its own k3d
cluster, initializes Zarf, and installs Istio, Keycloak, and Pepr. It does
not install the Portal layer. See [Portal tiles](#portal-tiles).

```sh
mise run uds-up

uds zarf package deploy /tmp/conduit-zarf/zarf-package-platform-runtime-amd64-0.17.5.tar.zst \
  --confirm \
  --components uds \
  --set-variables ENVIRONMENT_ID=example \
  --set-variables AGENT_ID=spoke-example \
  --set-variables BOOTSTRAP_ADMINISTRATORS=conduit-author,conduit-reviewer \
  --set-variables OPERATOR_OIDC_ISSUER=https://sso.uds.dev/realms/uds \
  --set-variables OPERATOR_OIDC_CLIENT_ID=conduit-hub \
  --set-variables OPERATOR_OIDC_USERNAME_CLAIM=preferred_username \
  --set-variables OPERATOR_OIDC_GROUPS_CLAIM=groups \
  --set-variables OPERATOR_OIDC_CA_FILE=/etc/ssl/platform-ca/ca-bundle.pem \
  --set-variables OPERATOR_OIDC_CLIENT_SECRET_FILE=/run/conduit-sso/client-secret

uds zarf tools kubectl -n platform-system rollout status deploy/platform-delivery --timeout=180s
uds zarf tools kubectl -n platform-system rollout status deploy/platform-spoke-agent --timeout=180s
uds zarf tools kubectl -n platform-system get package platform-runtime
uds zarf tools kubectl -n platform-system get networkpolicy
uds zarf tools kubectl -n platform-system get virtualservice
uds zarf tools kubectl -n platform-system get secret conduit-hub-sso
# Lists a Conduit monitor only when `uds-metrics` was selected instead of `uds`.
uds zarf tools kubectl get servicemonitor -A
```

When Identity and Authorization is installed, the Keycloak client is the
SSO proof. Read the admin password from the secret slim-dev creates and
query the `uds` realm. Do not copy the password into a document. If that
package is not installed, the operator refuses the Package and records
`Identity & Authorization is not deployed`.

```sh
# TOKEN from the Keycloak admin password grant. Do not print the password.
curl -sk "https://keycloak.uds.dev/realms/uds/protocol/openid-connect/token" \
  -d grant_type=password -d client_id=admin-cli \
  -d username=admin -d "password=${KEYCLOAK_ADMIN_PASSWORD}"
# Then, with the access token:
curl -sk "https://keycloak.uds.dev/admin/realms/uds/clients?clientId=conduit-hub" \
  -H "Authorization: Bearer ${TOKEN}"
```

`keycloak.uds.dev` resolves only if the slim-dev host alias is installed.
From inside the cluster, use the Keycloak Service instead. The 2026-11-14
evidence did not reach this query: Keycloak was not installed.

For a current beta runtime bundle, use the authenticated HCL build and signed
NextMode deployment procedure in the [package README](../../deploy/zarf/README.md#uds-bundle).
The old YAML bundle/config files are retired. The generated legacy YAML adapters
remain supported separately; their JSON-schema reference is a frozen test
fixture, not the current owned runtime bundle.

## Delivering a tenant's Zarf package or UDS bundle through the Spoke

Everything above installs Conduit itself. This section is the other
direction: a Plan step whose Release declares `chart.kind: zarf-package` or
`uds-bundle` (see [Artifacts and releases](../architecture/artifacts-and-releases.md)).
It is unit-tested against a recorded command model. It has not been run
against a cluster.

The Spoke picks an executor per step from the artifact kind. Helm steps run
exactly as before. A package step runs only when the Spoke was configured
for it; otherwise the step fails with a `Blocked` diagnostic that names the
switch, and the Plan fails. It is never handed to Helm.

The desired state's Environment must declare `packageArchitecture: amd64` or
`packageArchitecture: arm64` before a package can be planned. Each package
artifact names that one architecture and its exact platform manifest digest;
multiarchitecture declarations and OCI index resolution are unsupported.
Channel selection skips incompatible packages. This declaration is checked
again against the Spoke's configured target before execution; it does not
replace the Spoke check or assert that cluster hardware was discovered.

| Variable | Meaning |
| --- | --- |
| `PACKAGE_EXECUTOR_ENABLED` | `true` turns the executor on. Package-only settings require it; `KUBECTL_BIN` remains shared with Helm. |
| `PACKAGE_PUBLISHER_CERTIFICATE_ISSUER`, `PACKAGE_PUBLISHER_CERTIFICATE_IDENTITY` | Keyless publisher policy: two anchored regular expressions, passed to `zarf --certificate-oidc-issuer-regexp` and `--certificate-identity-regexp`. |
| `PACKAGE_VERIFICATION_KEY_PATH` | Absolute path of a public key file, the alternative to the publisher pair. |
| `PACKAGE_ARCHITECTURE` | `amd64` or `arm64`. Defaults to the Spoke binary's own. |
| `UDS_REQUIRE_PACKAGE_STATUS` | Optional `true` requires the UDS Package CRD and observed Package status; defaults to `false`. Does not enable manifest application. |
| `ZARF_BIN`, `UDS_BIN`, `KUBECTL_BIN` | Binary paths. Default: looked up on `PATH`. |

`UDS_REQUIRE_PACKAGE_STATUS=true` requires an existing namespaced UDS Package
with the admitted Zarf package name in the reviewed step namespace. Health and
rollout include its phase and observed generation. Returned name/namespace must
match the queried Package and its generation must be positive. Missing, malformed or
unavailable status fails the observation; a stale Ready generation stays in
grace. Removal is reported only after both Zarf and the UDS Package are absent.
The option neither applies nor deletes a Package CR and never sets a mutable
manifest path. Package configuration must already come from the admitted
artifact or its reviewed deployment setup. Differently named Package CRs are
not selected by this CLI option. Source and fake-command tests qualify this
wiring; actual UDS Core convergence receipts remain tracked in #427/#619.

`UDS_NATIVE_BUNDLES=true` explicitly selects UDS 0.41's NextMode HCL bundle
contract on this Spoke. The default retains legacy bundles. There is no format
fallback after verification failure. Native bundle verify, deploy and remove
all receive the configured public key and admitted architecture. Keyless bundle
execution remains outside this adapter's supported policy.

Native bundle inputs are global scalar HCL variables supplied through a private
absolute `config.uds.hcl`, preserving JSON string/boolean/number types and
escaping HCL interpolation as literal data. Legacy package-scoped keys are
refused before authentication in native mode. Bundle authors must connect their
variables through `values_files`; declaring a variable alone does not forward it
to a beta package. The private configuration is removed after Apply finishes.

Exactly one of the publisher pair and the key path is required. `uds deploy`
has no verification flags, so a bundle is checked with `uds inspect --key`
first and can only be deployed by a Spoke configured with a key.

Zarf packages first pass `zarf package inspect definition --verify=always`
with that same admitted key or certificate policy. The authenticated definition
must name the reviewed package and use a supported API version. This runs before
any cluster probe; a rejected signature or definition cannot enter rollback.
The later deploy still requires `--verify=always` against the same digest-pinned
source. A failure after deploy starts retains the existing rollback behavior.

For v1alpha1 packages, reviewed scalar inputs retain the existing variable flags.
For v1beta1 packages, the Spoke writes those reviewed JSON scalar types to a
private `.yaml` values file. Strings such as `"0012"` and `"false"` remain strings;
native booleans and numbers retain their types. Native schema validation may
therefore refuse a numeric input where the package requires a string. The file
is absolute so isolated CLI working directories can read it; its directory is
private, and it is removed after success, preflight refusal or deployment
failure. Nested inputs and characters outside the existing scalar admission
contract remain refused. Legacy bundle artifacts are not automatically converted to NextMode. Select
the explicit native bundle option only for artifacts authored for that format.

The Spoke image does not contain `zarf` or `uds`. Mount pinned binaries and
point `ZARF_BIN` and `UDS_BIN` at them. The chart in `deploy/helm/platform-spoke`
does not yet expose these variables or the extra RBAC below.

### What the Spoke needs to be allowed to do

The Spoke chart grants namespaced Roles in the managed namespaces only. The
executor's own calls need, in addition:

- `get` on `secrets` in namespace `zarf` (`zarf-state` and
  `zarf-package-<name>`);
- `get` on the cluster-scoped `customresourcedefinitions` named
  `packages.uds.dev`, when a bundle is deployed;
- `get`, `create`, `delete` on `configmaps` in the step's namespace for the
  applied record. The existing Role already grants this.

`zarf package deploy` and `uds deploy` need far more than that, with the
Spoke's token: they write `zarf-package-*` Secrets in `zarf`, create
namespaces, and apply whatever the package contains. That is the same open
bound the Helm path has, and it is the tenant's package that sets it.

### Reviewed configuration and CLI isolation

Reviewed scalar JSON values become ordered `--set-variables KEY=value` arguments
for Zarf and `--set KEY=value` arguments for UDS. A UDS variable may be scoped as
`package-name.VARIABLE`. Duplicate JSON keys (including escaped spellings),
trailing JSON, nested/null values, and strings the CLI cannot carry exactly are
refused. A global variable and a package-scoped spelling of that same variable
are refused together: the pinned UDS implementation iterates its set map and
could otherwise apply either value last. Signed package defaults remain part of
the approved artifact; ambient configuration does not add deployment variables.

The adapter's default command runner uses a private working directory and home,
with empty `uds-config.yaml` and `zarf-config.yaml` files at the CLIs' default
lookup locations. It supplies no `UDS_*`, `ZARF_*`, `CLI_FEATURES`, `COSIGN_*` or
unrelated credential environment variables. In particular, setting `UDS_CONFIG`
to an empty file would itself add a `CONFIG` package variable, so the runner
uses the private default file instead.

Explicit `KUBECONFIG` (including its path list), `DOCKER_CONFIG`, `SSL_CERT_FILE`
and `SSL_CERT_DIR` paths are resolved before changing directory and retained.
Standard in-cluster Kubernetes endpoint variables and its service-account mount
remain usable. The runner retains `PATH` for the configured tools and admitted
credential plugins; executable, key and manifest paths retain their original
meaning. Ambient home credentials, cloud-token environment variables and CLI
config files are not inherited. An explicit Docker config supplies credentials
to the approved package operation; it does not authorize another source or
prepare a Plan's `RegistryAccess`. Steps carrying `RegistryAccess` or
`SecretAccess` still fail closed because this executor cannot realize them.

This is process configuration isolation, not an OS sandbox. A signed package
and its tools execute with the Spoke's authority. A caller injecting `Runner`
supplies a trusted command port and must preserve the same isolation boundary;
the adapter cannot constrain arbitrary in-process implementations.

The relevant pinned implementation evidence is
[UDS variable precedence](https://github.com/defenseunicorns/uds-cli/blob/3190b8d5d4b066a1df04ae7dcc66c30ee79d7fe6/pkg/legacy/bundle/overrides.go),
[UDS config lookup](https://github.com/defenseunicorns/uds-cli/blob/3190b8d5d4b066a1df04ae7dcc66c30ee79d7fe6/internal/legacy/cli/viper.go),
[bundle signature validation](https://github.com/defenseunicorns/uds-cli/blob/3190b8d5d4b066a1df04ae7dcc66c30ee79d7fe6/pkg/legacy/bundle/common.go),
and [Zarf config lookup](https://github.com/zarf-dev/zarf/blob/356f4b6041346854e42c71cb93c9837b48f977eb/src/cmd/viper.go).

Run the bounded native conformance check with previously verified binaries:

```sh
ZARF_BIN=/verified/zarf-v1.0.0 UDS_BIN=/verified/uds-v0.41.0 \
  scripts/test-uds-executor.sh
```

The existing `test-uds-bundle-create.sh` gate invokes this check with the same
pinned binaries, so the hosted/local Zarf verification path cannot skip it.
The gate requires exact JSON run/PASS pairs for each crypto/policy/environment proof;
a zero exit before test execution is a failure, including under emulation.
The existing pinned Node checks also reject omitted/wrong config-loaded evidence
while accepting UDS's wrapped diagnostic/path formatting.

It creates one tiny image-free package and bundle, generates an ECDSA key only
in memory, and checks matching-key acceptance, wrong-key/unsigned rejection,
reviewed Zarf variable rendering and poisoned ambient config isolation. The
executor test intercepts every deployment; it opens no cluster and publishes
nothing. The wrong certificate-identity regression uses an injected recorded
signer contract to test policy forwarding and pre-cluster ordering. It is
distinct from the real UDS key-signature checks: no live Fulcio/OIDC/Rekor
keyless verification is claimed. Zarf's certificate-policy semantics are bounded
by its pinned [verification implementation](https://github.com/zarf-dev/zarf/blob/356f4b6041346854e42c71cb93c9837b48f977eb/src/pkg/signing/cosign.go).

### What `APPLIED` means for a package

Zarf records which package is deployed and at which generation. It does not
record the variables a deployment was given, the Release, or the bundle a
package came from. So after a deploy succeeds, and after Zarf's own record has
been read back, the executor writes a ConfigMap
`conduit-applied-<zarf package name>` in the step's namespace. It holds the
Release digest, version, config digest, a digest of the exact `KEY=value`
arguments passed, the artifact digest, and the digest and generation Zarf
recorded for that deploy.

The Spoke reports `configStatus: APPLIED`, and a Release and config digest at
all, only from that record and only while the record's Zarf digest and
generation equal what Zarf currently holds. The claim is:

> Conduit deployed this Release with these values, the deploy succeeded, and
> the Zarf package it produced has not been deployed again since.

It is not an echo of the Plan. With no record, a record from an earlier
generation, or another installation's record, the digests are empty and the
status is `INDETERMINATE`, which the Hub refuses as a success. A record whose
values differ from the step's reports `DRIFTED`.

It is also not more than that:

- It does not read live configuration. A `kubectl edit` or a `helm upgrade`
  against what the package installed changes neither Zarf's generation nor
  the record, so `APPLIED` does not detect it.
- It is not tamper-evident. Whoever can write ConfigMaps in the namespace can
  write this one.
- For a bundle, only the one package named by `packageName` is observed. The
  bundle's own digest cannot be read from the cluster and comes from the
  record.
- A rollback re-deploys the prior package with its own defaults, which moves
  the generation, so the installation reports `INDETERMINATE` afterwards.

The step's namespace must exist when the deploy finishes, because the record
is written there. If the record cannot be written the step fails and is
rolled back.

## Operator identity on a UDS cluster

The packaged Hub does not accept insecure identity headers. On a UDS
cluster, operator identity comes from a verified token issued by the
Keycloak realm registered in the Package CR (`conduit-hub`). The UDS
component leaves `OPERATOR_OIDC_*` empty by default so a plain Zarf
install, which has no Keycloak, can still start. The values above turn
OIDC on when the package is deployed with the `uds` component.

The Package CR does not set `enableAuthserviceSelector`. Authservice in
front of the Hub pods would also sit on the Spoke's mTLS port, which
shares the Hub process. The Hub runs the browser sign-in itself: it is
the OAuth client, it redirects to Keycloak from `/v1/auth/login`, receives
the code at `/v1/auth/callback`, and keeps a server-side session named by
an `HttpOnly` cookie. That happens only when `OPERATOR_OIDC_ISSUER` is set;
`CONSOLE_PUBLIC_URL` must then be the address people use, because the
redirect URI and the cookie's accepted origin are built from it and from
nothing in a request. `OPERATOR_OIDC_CLIENT_SECRET_FILE` makes the Hub
present the `conduit-hub-sso` secret; with it set and the Secret absent the
Hub fails at start instead of signing in as a public client. The Hub maps
the ID token's `preferred_username` and `groups` claims onto the operator
principal. See [`operator-sign-in.md`](operator-sign-in.md).

Not yet observed: a person signing in through a browser against the
slim-dev Keycloak with insecure headers off. The sign-in is exercised
against an in-process OIDC provider in the Go tests; the Package CR's
redirect URI and Secret key are checked statically by
`scripts/verify-zarf-package.rb`. Whether the tenant gateway route carries
a browser session to the Hub's own TLS listener is the open question
recorded under [What the Package CR grants](#what-the-package-cr-grants).

## Second-person approval users

`mise run uds-keycloak-users` creates two users in the `uds` realm,
`conduit-author` and `conduit-reviewer`, and adds both to the
`conduit-operators` group. Their passwords are written only to
`$HOME/.config/conduit/uds-local-users` (or `CONDUIT_UDS_USERS_FILE`)
with mode 0600, outside the repository. The command does not print the
passwords. A second run reuses the file unless `--rotate` is passed.

Set `PLATFORM_BOOTSTRAP_ADMINISTRATORS` to those usernames, as in the
deploy command above, so each user can establish initial governance. A
change request still needs a second person: the author cannot approve
their own request.

The admin password is read from the `keycloak-admin-password` Secret in
the `keycloak` namespace and is never copied into the users file or into
a document. If that Secret is absent, bootstrap a Keycloak admin first.

## Portal tiles

UDS Core 1.17.1, the slim-dev version this package pairs with, defines
per-endpoint tile metadata on the Package CR at
`spec.network.expose[].annotations`. The keys the Portal reads are
`uds.dev/title`, `uds.dev/icon`, and `uds.dev/visible`, with
`portal.uds.dev/*` variants taking precedence. A package-level default can
also be set in `zarf.yaml` as `metadata.annotations.dev.uds.title` and
`dev.uds.icon`. This matches the Package CRD schema for v1.17.1 and the
UDS Core guide "Customize UDS Portal app tiles".

The Portal builds one tile per `network.expose` entry, and only when the
Package also has an `sso` section. Wildcard hosts are omitted. A quoted
`"false"` on `uds.dev/visible` hides that endpoint.

Slim-dev does not ship the Portal layer (`core-portal`). Tiles appear only
after that layer is deployed, on the `upstream` or `unicorn` flavor, at
`https://portal.<domain>`. Do not invent another field for onboarded
application Package CRs; use `spec.network.expose[].annotations`.

## Evidence

Text logs from the run that produced this note:

- `docs/evidence/zarf-k3d-install-2026-11-14.txt`
- `docs/evidence/uds-k3d-install-2026-11-14.txt`

They list the commands, the health output, and any step that did not
complete. They contain no private keys and no admin passwords.
