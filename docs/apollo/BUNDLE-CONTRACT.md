# Platform-native disconnected Bundle contract

The control plane implements a platform-native first slice of Apollo-aligned
Export/Import semantics. It does not read or write a private Palantir format.

An Export Pipeline selects Environments, Products, Product Releases, or Module
Releases and chooses `NONE`, `ALL`, or `MINIMAL` Product Release inclusion.
Every build produces a deterministic resource set, a build-time security
report, a manifest digest, and an Ed25519 signature.

Pipeline definitions have immutable numbered revisions and an explicit
`ACTIVE` → `ARCHIVED` → `DELETED` lifecycle. Archive and unarchive are governed
changes; deletion is governed, requires prior archive, and leaves a tombstone so
the identifier and historical settings cannot be reused. Only an active
Pipeline can be edited or built. An operator may build an immutable historical
revision or request a standalone build; both are self-contained snapshots that
record the selected revision and do not alter incremental lineage.

An active Pipeline may enable an autonomous five-field cron schedule. An empty
cron expression defaults to midnight daily and an empty timezone defaults to
UTC; custom schedules require a valid IANA timezone. The delivery server runs
the scheduler under its own shutdown context. Before each due build it creates
a durable single-flight record for the exact Pipeline revision and scheduled
minute, then records terminal success or bounded failure and the exact Bundle
IDs. Concurrent schedulers and restarts observe the same claim and do not build
that minute twice. Disabling the schedule preserves its configuration and run
history without creating new claims.

The source-side pre-build preview refreshes the selected resource projection
without signing a manifest, packaging artifacts, creating a Bundle, or changing
incremental lineage. It compares the current selection with the any-Hub lineage
and reports additions, updates, removals, unchanged resources, and the security
outcome. A Pipeline may explicitly disable this projection independently of
manual or scheduled builds.

An Environment selection also carries its latest exact `ENVIRONMENT_REPORT`
when one exists. The target admits that resource only when its locally reviewed
connection setting is `RELAYED_FROM_ANOTHER_HUB`. Agent identity, Plan identity,
per-Entity observation times, and the report observation time survive unchanged;
the target catalog atomically records the signed Bundle and key that supplied
the observation. A newer direct or relayed observation wins without rewriting
its source timestamp. This is a Platform-native resource, not a private Apollo
Bundle-format claim.

Required/excluded label rules are type-scoped. Presence-only and multi-value OR
matching, AND across requirements, and exclusion precedence are enforced for
Environment and Product Release resources. Dependency closure fails rather
than bypassing an applicable label rule.

Missing selected resources, required dependency Releases, or artifact blobs
fail a build closed by default. A Pipeline may explicitly opt into
`buildDespiteMissingResources`; only then are unavailable inputs omitted.

Incremental pipelines behave as follows:

- the first build is a self-contained snapshot;
- subsequent builds depend on the preceding non-checkpoint Bundle and contain
  additions, updates, and signed deletion tombstones;
- a configured checkpoint frequency emits a self-contained checkpoint after
  the corresponding incremental; the checkpoint does not change the next
  incremental's dependency baseline;
- an unspecified checkpoint frequency defaults to 10 successful incrementals,
  matching the public Apollo default;
- a target may begin with a snapshot or checkpoint, but rejects an incremental
  until its declared dependency has been admitted.

Target preview verifies the manifest digest, trusted signing key, signature,
resource digests, unique resource identities, API version, kind, and lineage.
Admission optionally requires the static security outcome to pass. The file
repository durably stores pipelines, signed envelopes, import records, and the
exact desired-state snapshots from which exports are selected.

Every staged import exposes an ordered, durable task record for signature and
manifest verification, artifact staging, catalog admission, and Environment
change application. Task diagnostics contain only resource coordinates and a
bounded redacted failure; artifact bodies and credentials are never copied into
diagnostics.

## HTTP surface

| Method and path | Purpose |
| --- | --- |
| `GET /v1/bundles/signing-key` | Return this Hub's key ID and Ed25519 public key. |
| `PUT /v1/export-pipelines/{id}` | Create or revision-check update an Export Pipeline. |
| `GET /v1/export-pipelines` | List Export Pipelines. |
| `POST /v1/export-pipelines/{id}/preview` | Refresh the unsigned pre-build resource diff when preview is enabled. |
| `GET /v1/export-pipelines/{id}/schedule-runs` | List durable scheduled-minute claims and terminal results. |
| `POST /v1/export-pipelines/{id}/archive` | Propose the governed reversible archive transition. |
| `POST /v1/export-pipelines/{id}/unarchive` | Propose the governed unarchive transition. |
| `DELETE /v1/export-pipelines/{id}` | Propose governed tombstone deletion; prior archive is required. |
| `POST /v1/export-pipelines/{id}/build?targetHub={target}&revision={n}&standalone={bool}` | Build the current lineage, or a self-contained historical/standalone snapshot. |
| `GET /v1/bundles/{id}` | Retrieve a stored envelope. |
| `POST /v1/bundles/preview` | Verify an envelope and report additions, updates, removals, and unchanged resources. |
| `POST /v1/bundles/import` | Verify and durably admit an envelope. |
| `POST /v1/bundles/{id}/transfer-confirmations` | Confirm exact layer receipt for one target Hub. |
| `POST /v1/bundle-transfer-confirmations/{id}/rescind` | Rescind target layer knowledge so subsequent Bundles include bytes again. |
| `GET /v1/bundle-imports` | List durable admissions. |

The current format note inside every manifest explicitly bounds compatibility.
Signing keys and target trust roots are passed into the Handler by its runtime;
the default development Handler generates an ephemeral key.

## Artifact transport and remaining boundary

When artifact packaging is enabled, the signed manifest commits to immutable
layer descriptors and ordered SHA-256 chunk identities. Imports validate and
reconstruct each layer before staging the governed catalog change. Any-Hub
Bundles remain self-contained; target-specific omission requires an explicit,
durable, rescindable receipt confirmation. Malware evidence is normalized at
the exact-artifact boundary. Private Apollo resource types and wire formats
remain intentionally outside the compatibility claim.

Dedup confirmation requires a target-specific Bundle no older than seven days
from materialization and a Pipeline with dedup enabled. Any-Hub confirmation is
rejected. Confirmed layer knowledge is Pipeline-local by default and may be
shared across Pipelines for the same target only through an explicit option.
