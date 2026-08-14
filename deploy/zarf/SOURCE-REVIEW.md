# Zarf source review

Review date: 2026-08-02

The upstream repository was cloned with `--depth=1` at both the default branch
(`3a16b306d9a57e4eb346b11d07e206a7b0d42f39`) and the pinned v0.82.0 release
(`356f4b6041346854e42c71cb93c9837b48f977eb`). Conduit builds packages only
with v0.82.0.

## Surface reviewed

- `src/api/v1alpha1`, generated schemas, lint rules, migrations, and package
  examples for the accepted package structure and template constraints.
- Package create/load/layout code for archive checksums, architecture
  selection, OCI image indexes, SBOMs, version requirements, and split archive
  behavior.
- Image parsing, collection, storage, push, host transformation, registry
  credentials, and the admission hook for exact-digest rewriting.
- Init/state, namespace adoption, mutation modes, connected/external registry
  modes, package deploy/remove, generated Helm charts, server-side apply,
  health checks, and failure cleanup.
- Native signing, keyless GitHub OIDC providers, embedded Sigstore bundles,
  trusted roots, checksum validation, and offline verification paths.
- Release workflows, release checksums/SBOMs, docs, tutorials, integration
  packages, and the v0.82.0 tests covering the above behavior.

## Decisions and boundaries

| Finding | Conduit decision |
| --- | --- |
| Init defaults to admission mutation policy `all` | Require `--agent-mutation-policy=labeled`; label only `platform-system` as `mutate`. Product namespaces and their registry policy remain consumer-owned. |
| Digest references are rewritten to the Zarf registry with their repository path and digest intact; an index digest causes every platform to be carried | Resolve each registry-reported Hub and Agent index to one exact Linux platform-manifest digest, then build and lint separate amd64/arm64 packages. This avoids silently bloating either transfer with unrelated architectures and attestations. |
| Package templates apply to `zarf.yaml`; deploy variables apply to manifests | Registry-reported Hub/Agent references are create-time constants. Environment/Agent IDs and Secret names remain deploy-time variables and never enter released bytes as credentials. |
| Zarf uses a generated Helm release and supports explicit health checks | The runtime component checks both Deployments. The durable Hub PVC carries `helm.sh/resource-policy: keep` so package removal cannot silently erase control-plane state. |
| v0.82.0 embeds a Sigstore bundle in the package and supports ambient GitHub OIDC | The tag workflow signs with native `zarf package sign --keyless`, verifies the exact workflow identity, and computes the transport checksum only after signing. Disconnected deploys require `--verify=always` with that identity. |
| Package deployment expects Zarf state unless connected/YOLO mode is chosen | Conduit does not use YOLO. Operators transfer the separately pinned Zarf CLI/init artifact or configure a supported external registry before deploying the Conduit package. |
| The pinned init package requires the injector, registry, and Agent and offers optional K3s and Gitea components | Reuse those Zarf-owned bootstrap services. Select K3s only on a bare supported Linux host, select Gitea only when an internal Git service is wanted, and do not duplicate them in the Conduit package. |
| Zarf can carry arbitrary OCI artifacts, but Product artifacts are independently owned | Product repositories create their own disconnected transfer packages or mirror exact charts/images into the assigned target registry. Conduit's package must not absorb Product catalogs, charts, images, credentials, or installation values. |
| Zarf package removal reverses component deployment | Runtime removal is explicit and may remove Deployments and Service; the retained PVC requires a separately authorized cleanup decision. |
| Hub identity is not ready until its HTTPS listener starts | Carry the base runtime's bounded startup window into the disconnected manifest and require HTTPS for startup, readiness, and liveness probes. |

Pinned source references:

- [v0.82.0 package API](https://github.com/zarf-dev/zarf/tree/356f4b6041346854e42c71cb93c9837b48f977eb/src/api/v1alpha1)
- [v0.82.0 package deployment](https://github.com/zarf-dev/zarf/blob/356f4b6041346854e42c71cb93c9837b48f977eb/src/pkg/packager/deploy.go)
- [v0.82.0 image transformation](https://github.com/zarf-dev/zarf/blob/356f4b6041346854e42c71cb93c9837b48f977eb/src/pkg/transform/image.go)
- [v0.82.0 mutation policy](https://github.com/zarf-dev/zarf/blob/356f4b6041346854e42c71cb93c9837b48f977eb/src/internal/agent/operations/mutate.go)
- [v0.82.0 package signing](https://github.com/zarf-dev/zarf/blob/356f4b6041346854e42c71cb93c9837b48f977eb/src/pkg/packager/layout/package.go)
