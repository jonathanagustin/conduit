# Apollo alignment and conformance boundary

The repository continuously tests the public evidence it relies on, but no
public Palantir suite certifies an independent implementation as “Apollo
compliant.” The supported claim is therefore **Apollo-aligned**.

| Layer | Automated check | Claim supported |
|---|---|---|
| Snapshot integrity | CI verifies the pinned upstream commit's exact SHA-256 | Reproducible public contract evidence |
| OpenAPI structure | OpenAPI 3.0.1, all local `$ref`s, security schemes, Helm extension, and dependency schemas are validated | The evidence is internally coherent and contains the expected public vocabulary |
| Public API v0 adapter | CI derives all operations from the pinned OpenAPI and requires an exact 13/13 route match; Go tests exercise every operation family and Apollo response envelope, require identity on all 13 operations, and bind status writers to one Environment | The named adapter conforms to the pinned public route and payload profile without inheriting the historical unauthenticated-metadata failure disclosed in PLTRSEC-2022-05 |
| CLI Product artifacts | Reconstructed 0.814.0 fixtures are validated now; native capture retains exact input/output bytes and binary/output hashes, and `verify-byte-equivalence.sh` compares a candidate with `cmp` without YAML/tar/gzip normalization | Public-documentation format compatibility only; exact byte equivalence remains blocked while native capture count is zero |
| Public Apollo Agent configuration | Go tests cover the pinned `osdk-ts` tagged secret-input variants, exact `secretKeyName` wire field, recursive templates, assignment/key validation, Universal targeting, and all seven ignore reasons | Compatibility with the named public generated configuration types; not with the private Hub control transport |
| Git-managed Modules | Three current-format definitions plus Go conformance cases cover local composition, immutable/flattened locks, keyed repetition, variable overrides, recursive `entityOverrides`, restrictions, ownership, Release Channel tracking, recall exclusion, and pause/resume; a tenant-CLI hook runs `module lock --verify` | Behavioral alignment for the tested public semantics; native lock byte/shape conformance awaits CLI-generated lock files |
| Delivery semantics | Go tests cover immutable releases, dependency groups/overrides/ignores, optional dependencies, incompatibilities, maintenance windows, channels, promotions, recalls, policy gates, approvals, leases, suppressions, cancellation, rollback, and reported state | Behavior is aligned with the reviewed public documentation |
| GitOps and Spoke acceptance | CI publishes and revises Git-directory Module definitions across channel promotion and pause/resume, then separately creates an ephemeral Kubernetes cluster and OCI registry to run the real Hub/Spoke images through approved leasing, digest-pinned chart pull, Helm install/upgrade, and exact reported state | End-to-end conformance for the repository's native GitOps and delivery path; it does not claim compatibility with Apollo's private Agent protocol |
| Artifact content identity | Go race tests verify SHA-256 streaming ingest, expected-digest rejection, idempotent put, durable retrieval, and Product Release digests carried through Plans and reported state | OCI-aligned content identity for platform-owned bytes; external registry conformance remains the registry's responsibility |
| Product manifests | Chart verification tests lint and render the repository's Helm Products; `POST /v1/apollo/product-manifests/validate` accepts current `helm-chart.v1` YAML and tests valid, invalid, and malformed input | The shipped Products satisfy this repository's Apollo-aligned packaging profile; the named import boundary supports the reconstructed 0.814.0 format |
| Native Apollo Hub/Agent control interoperability | Not claimed; public Agent-to-Foundry Install Manager types exist, but the Hub lease/heartbeat/plan/report protocol and an official conformance suite are unavailable | None |

“Apollo-compatible” is attached only to the public API adapter once its pinned
schema tests pass. Native CLI artifact compatibility remains provisional until
the reconstructed fixture is replaced or confirmed by CLI 0.814.0 output. It
must not be used as a blanket claim about Palantir's private Hub-to-Agent
protocol.

The public docs contain examples with uppercase `PRODUCTION`, `MANAGED`, and
`SHUTDOWN`, while the pinned OpenAPI enums are lowercase. The API adapter uses
the lowercase schema values; examples are not treated as a competing wire
contract.
