# Helm Product Release publishing conformance

Status: partial implementation, assessed 2026-08-09 against the operator-supplied
current Apollo “Creating Helm chart Releases” documentation.

## Current coverage

| Documented behavior | Current evidence | Verdict |
| --- | --- | --- |
| `helm-chart.v1` manifest with group/name/Product version and chart name/version/repository URL | `internal/controlplane/apollo_manifest.go`; manifest/schema tests | Implemented |
| `https://` and `oci://` chart repositories | validator and embedded JSON Schema | Implemented |
| Orderable Product versions | manifest validator | Implemented |
| Product dependencies with inclusive minimum/maximum, optional and recommended versions | manifest model plus range validation tests | Implemented |
| Associated OCI artifacts | exact digest-pinned artifact extension; stronger than the documented tag example | Implemented with stricter integrity |
| Immutable publication and no coordinate overwrite | connected publisher/API/repository tests | Implemented |
| CI-safe two-stage init→create workflow | `scripts/test-spoke-e2e.sh`; Conduit command tests | Implemented for the Platform-native subset |
| Create Release from the Product page | no console mutation route, form, or browser test | Missing |
| `helm-chart init --as-tgz` archive with `deployment/manifest.yml`, `values.yaml`, optional `values.schema.json` | deterministic generator uses one `<product>-<Product-version>/deployment` root and never overwrites an existing archive; round-trip test | Implemented as a canonical semantic archive; no native byte-equivalence claim |
| Initialize from `--chart-path` | derives name/version from a bounded Helm v2 application `Chart.yaml`, requires `values.yaml`, includes optional `values.schema.json`, and rejects ambiguous duplicate flags/library charts | Implemented |
| Distinct Product version and Helm chart version | `initializeHelmChartManifest` preserves the Maven-coordinate Product version independently from `--version`; focused CLI test uses Product `2.0.0` and chart `1.2.3` | Implemented |
| `product-release create --product-tgz` | bounded TGZ decoder feeds the same `HelmProductReleaseBundle`; rejects traversal, absolute paths, links, duplicates, unexpected entries, multiple roots/streams, corrupt checksums, excess size/count, and root/manifest mismatch | Implemented as canonical semantic ingestion |
| Separate `--default-config` and `--config-schema` publication | `conduit product-release create`; `HelmProductReleaseBundle`; Hub content-addressed blob descriptors in the immutable publication; focused CLI/adapter/Hub tests | Implemented as a Platform-native semantic bundle; no Apollo archive-byte claim |
| `--space-id` enrollment targeting | present in the pinned native command inventory, absent from executable Conduit publication | Missing / authority boundary undecided |
| Apollo token or client ID/secret flags | Conduit uses its Platform profile/transport identity instead | Intentional non-native authentication boundary; not CLI-compatible |

## Consequence

We do the safety-critical server half and a narrower Platform-native CLI path,
but not the complete documented workflow. Configuration and schema bytes are
now part of the Platform-native release input. The Hub size-bounds and validates
each document, content-addresses the exact bytes, and binds stable media
type/digest/size descriptors into the immutable publication coordinate. Exact
replays are idempotent and changed configuration at the same coordinate
conflicts. Runtime chart availability remains separate from metadata
publication.

Archive generation and ingestion now produce the same semantic bundle as the
separate-file workflow. The archive boundary validates paths, duplicates, link
types, entry count, compressed and expanded size, gzip integrity and stream
count, the finite file allowlist, and the root-to-manifest identity. Generation
is deterministic for the same inputs and refuses overwrite. This is format
alignment, not Apollo archive-byte equivalence; the native-capture gate remains
the only authority for any future byte-equivalence claim.

The web workflow should be built only after that server contract exists. It
must use the same validator and immutable publication operation, expose Product
and chart versions as distinct fields, accept only supported repository schemes,
and return the created durable Release evidence. It must not accept registry
credentials into browser or Hub persistence.
