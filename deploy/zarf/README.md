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
Hub: a Keycloak client, network allows, and a metrics ServiceMonitor. It is
not selected by `zarf package deploy --confirm`. Select it with
`--components uds` only on a cluster that already has UDS Core. A plain Zarf
install leaves the component out. The commands and the limits of that check
are in [the UDS package note](../../docs/operations/uds-package.md).

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
