# Deployment claims and evidence

This matrix bounds claims about Platform's deployment model. A package name,
header, Maven coordinate, annotation, DNS name, or public product statement is
evidence only for the narrow fact it directly supports.

## Confidence vocabulary

- **Primary:** an authenticated/hash-recorded artifact, generated output, or
  official product requirement with a recorded origin.
- **Derived:** a stated inference from primary evidence.
- **Corroborating:** consistent source or documentation that cannot prove a
  private deployment fact.
- **Unknown:** no committed evidence proves the claim.
- **Local choice:** Platform's design; never a claim about Palantir internals.

## Claim matrix

| Claim | Status | Evidence | Limit |
| --- | --- | --- | --- |
| Rubix is a hardened Kubernetes substrate used by AIP, Foundry, and Apollo. | Public primary | [Rubix substrate](https://www.palantir.com/docs/foundry/architecture-center/rubix) | Does not prove a private tenant's provider, cluster topology, version, namespaces, or Pod layout. |
| Rubix App Platform is not a proprietary Linux or Kubernetes distribution; it combines an open-source or COTS Linux OS and Kubernetes distribution as one hardened system. | Public primary | [Palantir Rubix, “Highly opinionated Kubernetes OS”](https://www.palantir.com/rubix/) | Does not identify the selected distribution, version, bootstrap mechanism, or private configuration for any enrollment. |
| Rubix publicly names Cilium, Envoy, Cert-Manager, Vault, a Kubernetes compliance operator, and Terraform-backed cloud provisioning as substrate components. | Public primary | [Palantir Rubix, control-plane and networking sections](https://www.palantir.com/rubix/) | A named component is not evidence that Platform should install or own it; cluster bootstrap remains Environment-owned. |
| Apollo computes installation, upgrade, and rollback Plans and translates intent through Rubix APIs. | Public primary | [Rubix substrate, “Apollo and Day 2 operations”](https://www.palantir.com/docs/foundry/architecture-center/rubix#apollo-and-day-2-operations) | Does not disclose the private Hub↔Agent wire or authorize a compatibility claim. |
| Software intended for Rubix is packaged as one or more versioned Helm charts with explicit non-admin Kubernetes permissions and externally selected namespaces. | Public primary | [Rubix technical specifications](https://www.palantir.com/rubix-specs/) | Does not make any arbitrary chart Rubix-compatible or transfer namespace/bootstrap ownership to Platform. |
| Immutable versioned charts and images in registries are Apollo deployment inputs. | Public primary | [Publishing versioned Helm charts](https://www.palantir.com/docs/apollo/core/publishing-versioned-helm-charts/) | Does not prove private archive bytes or runtime chart ownership. |
| Platform ships separate Hub and Spoke Helm releases. | Local choice, executable evidence | `ruby scripts/verify-helm-runtime.rb`; `./scripts/test-spoke-e2e.sh` | This is Platform's trust/lifecycle split, not proof that Palantir uses two charts. |
| Platform's Rubix example uses Pod-certificate/SPP annotations, external CA mounts, HTTPS probes, and instance-group affinity. | Corroborating adaptation | `deploy/helm/*/rubix-values.example.yaml`; `ruby scripts/verify-helm-runtime.rb` | Annotation and affinity shapes do not prove certificate issuance, scheduling capacity, SLS payloads, or private Rubix integration. |
| The current filesystem-backed Hub is highly available. | Refuted local claim | The Hub chart enforces one replica/Recreate around a single-writer retained PVC; root persistence documentation requires a future transactional repository for multiple replicas. | A consumer must not describe this backend as disruption-tolerant or satisfying a strong-availability Rubix profile. |
| Maven coordinate names reveal deployed services, Pod composition, or Apollo protocols. | Refuted evidence grain | `docs/apollo/APOLLO-MAVEN-INVENTORY-EVIDENCE.md` | Coordinates establish version vocabulary at most; they do not preserve dependency edges, runtime inclusion, topology, or behavior. |
| A Rubix-looking DNS name, Envoy header, SLS annotation, or scheduler label proves an Envoy sidecar or one-service-per-Pod topology. | Unknown | No Platform evidence captures a private workload specification. | Such signals cannot establish hop count, ingress versus sidecar, container count, or cluster ownership. |
| AWS EKS, RDS, S3, EFS, KMS, or another managed service is Palantir's implementation. | Unknown; consumer use is a local choice | Rubix publicly supports multiple clouds and permits external managed dependencies. | A Platform deployment may choose these services, but must describe them as consumer architecture. |
| Rubix has one universal hard-coded workload recycle duration. | Refuted as a current public claim | The current [Rubix substrate](https://www.palantir.com/docs/foundry/architecture-center/rubix) says nodes cannot live beyond 48 hours, while the current [Multimodal Data Plane](https://www.palantir.com/docs/foundry/architecture-center/multimodal-data-plane) says containers are cycled within 72 hours. | Preserve bounded aggressive cycling as the supported invariant; treat node and container/profile durations separately until an authoritative profile resolves the distinction. |

## Platform consequences

- Product charts, registry coordinates, credentials, Environment inventory,
  namespaces, storage services, network policy, and cluster operators remain
  consumer-owned.
- Runtime charts keep exact image digests, Restricted Pod security, HTTPS/mTLS,
  namespaced identities, and no namespace/CRD/cluster-admin ownership.
- A future multi-replica Hub requires a transactional repository and an
  independent failover gate before any high-availability claim.
- Private Apollo/Rubix compatibility requires authorized protocol or workload
  evidence; public architecture and circumstantial source are not substitutes.
- Bootstrap-owned Linux/Kubernetes selection, Cilium, Envoy, certificate,
  compliance, Vault, node-lifecycle, and Terraform controllers remain external
  providers. Platform consumes their versioned realization receipts and does
  not become a second cluster operator.

Reviewed 2026-08-11.
