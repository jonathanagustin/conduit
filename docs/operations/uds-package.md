# UDS package and a plain Zarf install

The Zarf package `platform-runtime` carries the Hub, the Spoke Agent, and an
optional UDS Package custom resource. The `uds` component is not selected by
`zarf package deploy --confirm`. Select it with `--components uds` only after
UDS Core is already installed. The Package CR registers a Keycloak client for
the console, allows the traffic below, and declares a ServiceMonitor.

These steps were run on a connected machine. They pull the Zarf init package
and, for the UDS path, the published `k3d-core-slim-dev` bundle from a public
registry. They are not an offline-bundle install and they do not show a disconnected
cluster operating after the images are already present.

## What the Package CR grants

- SSO client `conduit-hub`, redirect URI `https://conduit.uds.dev/console/*`
  by default (`CONSOLE_PUBLIC_URL`). Authservice is not attached. The Hub
  port is the Agent mTLS listener, and a proxy in front of it would break
  the Spoke.
- Ingress from the tenant gateway (`istio-tenant-gateway` /
  `tenant-ingressgateway`) to the Hub on 8081, plus an `expose` entry on the
  tenant gateway at host `conduit`. The Hub speaks HTTPS on that port. The
  route object is created; a browser session through the gateway is a
  separate check.
- Ingress from the Spoke to the Hub on 8081, and the matching egress.
- Egress from the Hub and the packaging worker to operator-supplied Postgres
  on 5432. The namespace and `app.kubernetes.io/name` value default to
  `postgres` (`POSTGRES_NAMESPACE`, `POSTGRES_NAME_LABEL`). The Zarf baseline
  itself uses the file repository and does not open a database connection.
- Egress from the Spoke to the Kubernetes API, kubelet probes to the Hub, and
  DNS to `kube-dns` in `kube-system`.
- A ServiceMonitor for Service port `metrics` (9090, path `/metrics`). The
  process does not listen on that port in this baseline. `/metrics` starts
  only when `METRICS_ADDRESS` is set and `REPOSITORY_KIND=postgres`. The file
  repository has no queue statistics, and exporting zeroes would report an
  idle fleet. The monitor can be registered while the scrape has no series.

## Tool versions used here

- Zarf CLI `v0.82.0` for package create, lint, and the plain install.
- UDS CLI `v0.41.0`. On the UDS cluster, deploy the Conduit package with
  `uds zarf` so the CLI matches the Zarf agent that slim-dev installed.
- k3d `v5.9.0` for the plain cluster. The UDS path uses the k3d packaged in
  `uds-k3d` 0.21.4 (its offline variant), which the slim-dev bundle pins.
- Slim-dev bundle
  `oci://ghcr.io/defenseunicorns/packages/uds/bundles/k3d-core-slim-dev:1.17.1`.
  Bring it up with `mise run uds-up`. That task creates a user-defined Docker
  network named `uds-k3d` first, which is required when the Docker daemon has
  no default bridge, and passes `--network uds-k3d` plus flannel `host-gw`
  through `K3D_EXTRA_ARGS`. Some kernels cannot create a vxlan device.
  Tear the cluster down with `mise run uds-down`.

`deploy/zarf/uds-bundle.yaml` is the Conduit half of that pair. Slim-dev
embeds its Core packages inside the published bundle, so this file does not
re-pin them. Create it only after `zarf package create` has produced a
package whose version matches `VERSION`.

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
  --set-variables AGENT_ID=spoke-example

uds zarf tools kubectl -n platform-system rollout status deploy/platform-delivery --timeout=180s
uds zarf tools kubectl -n platform-system rollout status deploy/platform-spoke-agent --timeout=180s
uds zarf tools kubectl -n platform-system get package platform-runtime
uds zarf tools kubectl -n platform-system get networkpolicy
uds zarf tools kubectl -n platform-system get virtualservice
uds zarf tools kubectl -n platform-system get secret conduit-hub-sso
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

Optional bundle, after the Zarf package exists:

```sh
uds create deploy/zarf --confirm --output /tmp/conduit-zarf
```

That archive contains the Conduit package with the `uds` component selected.
It does not contain UDS Core. Deploy slim-dev first, then this bundle.


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
