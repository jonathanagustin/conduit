# Trusted technology baseline

Conduit reuses maintained security and delivery primitives at narrow adapters.
The repository does not implement its own cryptography, identity issuer,
secret store, vulnerability database, container registry, or Kubernetes
reconciler.

| Need | Repository boundary | Deployment-owned facts |
| --- | --- | --- |
| Workload identity | SPIFFE URI identities and verified mTLS client certificates at Hub and Spoke boundaries | Issuer, trust domain, rotation policy, and trust bundle |
| Artifact identity | OCI digest verification plus Sigstore-compatible signing and attestation workflows | Registry policy, identity policy, transparency-log policy, and verification roots |
| Software inventory | Syft-generated native and SPDX SBOM artifacts | Retention, ingestion, and downstream policy |
| Vulnerability evidence | Provider-neutral normalized assessment; optional Grype adapter | Scanner engine, database freshness, credentials, exceptions, and scheduling |
| Kubernetes delivery | Helm execution by the outbound Spoke; Flux and Zarf distribution assets | Cluster bootstrap, operators, namespaces, credentials, and admission policy |
| Secrets | Opaque provider references and bounded external material-preparer calls | Secret values, provider authentication, KMS policy, and materialization |

Repository presence proves an integration surface, not production enablement.
Before relying on one of these controls, verify its pinned version or digest,
the owning deployment configuration, a failing negative case, and the emitted
receipt or attestation.

## Selection rules

- Prefer an open standard and a vendor-supported or graduated implementation.
- Pin executable downloads and base images by reviewed digest or checksum.
- Give adapters the minimum data and authority needed for one operation.
- Treat provider output as untrusted and bound its size, shape, and timeout.
- Keep authorization in Conduit even when another system supplies identity or evidence.
- Fail closed when required identity, digest, evidence, or trust configuration is absent.

The current production issuers, registry controls, scanner configuration,
secret backends, and trust roots are deployment facts and are intentionally not
asserted in this repository.
