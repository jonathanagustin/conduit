# Apollo durable reference

This directory turns upstream documentation review into compact, durable input
for future agents. It complements the complete URL/hash inventory in
[`../public-documentation-manifest.tsv`](../public-documentation-manifest.tsv);
it does not duplicate the copyrighted source pages.

Read only the topic relevant to the current change:

| Topic | Note | Local contract surface |
| --- | --- | --- |
| Public documentation inventory refresh | [documentation-inventory.md](documentation-inventory.md) | URL/hash freshness only; no semantic contract update |
| Agent installation and distribution boundary | [agent-installation.md](agent-installation.md) | Managed configuration, secret work, public Install Manager seam, private-wire exclusion |
| Recovered Java object alignment | [../JAVA-OBJECT-ALIGNMENT.md](../JAVA-OBJECT-ALIGNMENT.md) | Exhaustive 42-file direct Apollo closure, exact configuration adapters, Foundry/Marketplace boundaries |
| Immutable Product artifact publication | [artifact-publication.md](artifact-publication.md) | Versioned registry charts, values-based variance, Git/Flux ownership boundary, recall identity |
| Checkpoint Bundle cadence and lineage | [bundle-checkpoints.md](bundle-checkpoints.md) | Snapshot/incremental lineage, default cadence, onboarding and transport boundary |
| Bundle resource label selection | [bundle-selection-labels.md](bundle-selection-labels.md) | Type scoping, presence/value matching, AND/OR and exclusion precedence |
| Bundle data-selection strategies | [bundle-selection.md](bundle-selection.md) | Environment/Module/Product/Release/Team rules, initialization and incremental semantics |
| November 2026 CLI additions | [cli-additions.md](cli-additions.md) | 18-page classification; local Grype implementation and scoped product gaps |
| CLI setup and publication workflows | [cli-getting-started.md](cli-getting-started.md) | Distribution, profiles/auth, Environment output, Helm and Asset publication, byte-equivalence limit |
| Connected publisher admission | [connected-hubs.md](connected-hubs.md) | External OAuth/whitelist ownership, Artifact + Product + Release authorization, outbound-Spoke separation |
| Emergency Entity commands | [commands.md](commands.md) | Priority, constraint bypass, single-flight execution, cancel/abort, uninstall and workload restart |
| Entity ownership handoff | [entity-ownership-handoff.md](entity-ownership-handoff.md) | Uninstall versus unmanage-only, dependency gate, undo-before-lease, observe-only Spoke execution |
| Debug terminal and JIT access | [debug-access.md](debug-access.md) | Four-hour/read-only baseline, immutable profiles, non-self-approved elevation, Teleport broker boundary |
| Entity and Environment configuration | [configuration.md](configuration.md) | Desired state, Plan value resolution, Modules |
| Environment profile and compliance metadata | [environment-profile.md](environment-profile.md) | Display metadata, accreditation, contact/operational responsibility, immutable provenance, explicit CEL projection |
| Health/status ingestion | [health.md](health.md) | Public API adapter, reported state, fleet |
| Authentication and role authorization | [identity-access.md](identity-access.md) | External IdP claims, global/resource roles, additive Environment→Entity inheritance, permission administration, label dual control |
| Module configuration ownership | [modules.md](modules.md) | Module install/update reconciliation |
| Product Definition documents | [product-definition.md](product-definition.md) | manifest.yml traits, configuration.yml, substitution language, cross-document rules |
| Plan orchestration | [plans.md](plans.md) | Ordinary uninstall, priority, supersession, dynamic constraint checks, Flux source boundary |
| Product and Release retirement | [catalog-retirement.md](catalog-retirement.md) | Governed tombstones, dependency/Entity gates, coordinate non-reuse, immutable history |
| Kubernetes namespace lifecycle | [namespaces.md](namespaces.md) | RFC 1123 inventory, default deletion protection, empty-delete gate, immutable Entity placement, Flux ownership |
| Label-definition administration | [label-definitions.md](label-definitions.md) | Immutable-ID revisions and tombstones, accepted values, dual authorization, reference-safe migration |
| Artifact registries and secrets | [registry-secrets.md](registry-secrets.md) | Runtime mirror resolution, external credential and secret boundaries |
| Promotion and ramped rollout | [promotion.md](promotion.md) | Health/soak gates, reachability, pacing, suppression |
| Recall and observed-state roll-off | [recall.md](recall.md) | Ranges, strategies, security-derived recalls |
| Security scanner execution and promotion boundary | [security-scanning.md](security-scanning.md) | External scanner protocol, normalization, exact-digest gates, scanner-owned Release labels |
| Spoke control-plane responsibility map | [spoke-control-plane.md](spoke-control-plane.md) | Agent/Flux split, health/state observation, external admission-controller boundary |

`review-ledger.tsv` is the machine-readable evidence index for pages that
directly informed implementation. The broader manifest remains the discovery
and drift-detection index for all reviewed pages.
