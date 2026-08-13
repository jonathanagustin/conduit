# Trusted production technology baseline

Reviewed 2026-08-03. This is an evidence-backed default, not an instruction to
install cluster-wide operators from the Platform runtime. Deployment
repositories retain exact chart/image digests and approve upgrades.

| Boundary | Selected technology | Reviewed stable line | Platform integration |
| --- | --- | --- | --- |
| GitOps reconciliation | Flux | 2.9.3 | Existing digest-pinned `OCIRepository` and bounded `Kustomization`; Flux owns runtime resources, never Product installations also owned by the Spoke |
| Product release reconciliation and runtime packaging | Helm | 4.2.3 plus `oras-go` 2.6.2 (default); 3.21.1 (compatibility) | The release image source-builds hash-pinned Helm 4.2.3 with the upstream ORAS traversal fix; independently installable Hub and Spoke runtime charts plus Product execution have Helm 3/4 gates during the supported transition window |
| Kubernetes execution CLI | kubectl | Kubernetes 1.36.3 plus `x/net` 0.56.0 and `x/text` 0.39.0 | The Spoke image source-builds the hash-pinned stable Kubernetes release with the fixed network/text modules; it receives only the Spoke service account's namespaced authority |
| Go runtime | Go | 1.26.5 | Hub, Spoke, Helm, and kubectl are reproducibly built with the digest-pinned fixed compiler image; `go.mod` carries a matching `toolchain` directive so CI and local builds compile with that same fixed release rather than the older minimum on the `go` line |
| Source vulnerability gate | govulncheck | 1.6.0 | CI scans the module against the toolchain it declares and fails on call-graph-reachable findings. Grype scans the built images and therefore only observes the compiler the Containerfiles pin; this gate catches a module toolchain or dependency that drifts behind the release image |
| Minimal runtime root | Chainguard Static | index `sha256:399c8cb4858f05aaa33f43f02a2e75f28d40f016c0f86e5ba6075769e3303791` | The Spoke runs directly on the reviewed amd64/arm64 static image; the scratch Hub imports only its CA bundle. The mutable `latest` label is discovery metadata and the digest is authority |
| Containerfile static analysis | Hadolint | 2.15.1 | CI downloads the official Linux binary by exact SHA-256 and fails both `Containerfile` inputs at `info`; no rules are ignored |
| Workload identity | SPIFFE/SPIRE with official `go-spiffe` | SPIRE 1.15.2; hardened chart 0.29.0 currently packages SPIRE 1.14.5; `go-spiffe` 2.8.1 | Native Workload API client, rotating X509-SVIDs, exact Hub identity, dynamic trust bundle; deployment uses `overlays/spire` |
| OCI signing | Sigstore Cosign | Cosign 3.1.2; installer action 4.1.2 | GitHub OIDC keyless signatures on immutable Hub, Spoke, and runtime digests, in addition to BuildKit SBOM/provenance; disconnected/private mirrors use organization-controlled KMS keys |
| Kubernetes Secret materialization | External Secrets Operator where a Kubernetes Secret is required | GA chart/application 2.8.0 | Environment-owned implementation of the existing opaque material-preparer contract; Platform does not install ESO or receive values |
| Direct file-mounted secret material | Secrets Store CSI Driver | 1.6.0 | Suitable for workloads that consume mounted files. It is not the default Apollo-secret adapter because the upstream documentation still labels Kubernetes Secret sync as alpha |
| Release SBOM | Syft | 1.50.0 | CI generates native Syft JSON and SPDX JSON for both platform manifests selected from each exact release image index, publishes both as workflow evidence, records per-architecture SPDX/report hashes in the release manifest, and attests each SPDX document to the image |
| Release vulnerability gate | Grype | 0.116.1 | CI updates and validates one cached database per workflow, scans both architecture-specific Syft SBOMs without changing that database, retains JSON including database metadata, fails on High/Critical findings, publishes each report, and attests it to the image; the Hub's optional adapter normalizes exact-image findings but rejects unproven ignores |
| Environment artifact security evidence | Organization-approved Trivy, Grype, Tenable, or commercial scanner | Provider release chosen by the Environment | The bounded scanner protocol normalizes exact-image vulnerability and all-artifact malware PASS/FAIL evidence; Platform does not maintain a CVE database or ingest credentials/raw findings |

## Selection rules

1. Use a standard protocol directly when the implementation already supplies
   rotation and verification. SPIFFE Workload API replaces copied certificate
   files for the preferred production identity profile.
2. Use exact OCI digests plus signatures, SBOMs, and provenance. A mutable tag
   is discovery metadata, never deployment authority.
3. Keep a single controller for every Kubernetes object. Flux owns the Platform
   runtime, the Spoke owns approved Product Helm releases, and the selected
   secret operator owns materialized Secret objects.
4. Prefer provider workload identity over static credentials. KMS/HSM policy,
   SPIRE trust roots, secret-store authentication, and scanner credentials are
   Environment authority and never enter Hub state.
5. Do not select an alpha feature merely to reduce adapter code. The CSI
   driver's stable volume mount is appropriate for file consumers; ESO's GA
   `ExternalSecret` contract is the default reference when an existing named
   Kubernetes Secret is required by a third-party Helm chart.

## Primary upstream references

- SPIRE releases: <https://github.com/spiffe/spire/releases/tag/v1.15.2>
- SPIRE hardened chart: <https://github.com/spiffe/helm-charts-hardened/releases/tag/spire-0.29.0>
- `go-spiffe` API: <https://pkg.go.dev/github.com/spiffe/go-spiffe/v2@v2.8.1/workloadapi>
- Flux release: <https://github.com/fluxcd/flux2/releases/tag/v2.9.3>
- Cosign release: <https://github.com/sigstore/cosign/releases/tag/v3.1.2>
- Cosign installer: <https://github.com/sigstore/cosign-installer/releases/tag/v4.1.2>
- External Secrets release: <https://github.com/external-secrets/external-secrets/releases/tag/helm-chart-2.8.0>
- Secrets Store CSI release: <https://github.com/kubernetes-sigs/secrets-store-csi-driver/releases/tag/v1.6.0>
- Syft release: <https://github.com/anchore/syft/releases/tag/v1.50.0>
- Grype release: <https://github.com/anchore/grype/releases/tag/v0.116.1>
- Kubernetes 1.36.3 release source: <https://dl.k8s.io/v1.36.3/kubernetes-src.tar.gz>
- Helm 4.2.3 release: <https://github.com/helm/helm/releases/tag/v4.2.3>
- ORAS Go 2.6.2 release: <https://github.com/oras-project/oras-go/releases/tag/v2.6.2>
- Chainguard Static image: <https://images.chainguard.dev/directory/image/static/overview>
- Hadolint 2.15.1 release: <https://github.com/hadolint/hadolint/releases/tag/v2.15.1>
- govulncheck 1.6.0 release: <https://github.com/golang/vuln/releases/tag/v1.6.0>
- Palantir Tenable.SC provider: <https://github.com/palantir/terraform-provider-tenablesc>

## Release-gate remediation record

The first remote `v0.13.0` candidate scan (GitHub Actions run
`30775659926`, 2026-08-03) failed closed before tagging. Its retained Grype
reports identified fixed-version findings in the Platform Go runtime,
`x/net`, `x/text`, gRPC, and the published kubectl binary, plus
`CVE-2026-50163` in Helm's `oras-go` 2.6.1. Platform upgraded its own modules,
adopted Go 1.26.5, removed the Alpine tool layer, and source-builds the exact
Helm/Kubernetes releases with the fixed modules above. This is remediation,
not a scanner ignore or severity-threshold exception.
