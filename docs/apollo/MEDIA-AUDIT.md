# Public Apollo video audit

## Capture result

The 2026-08-02 rendered-page crawl inspected all 241 URLs then in
[`public-documentation-manifest.tsv`](public-documentation-manifest.tsv) for
YouTube, Vimeo, Wistia, MP4, WebM, MOV, and HLS references. It found one video:

| Page | Video | Placement |
| --- | --- | --- |
| [Welcome to Apollo](https://www.palantir.com/docs/apollo/apollo-getting-started/introduction-welcome/) | [Palantir Apollo 2023 — Mission Control for Software Deployment](https://www.youtube.com/watch?v=XmLVuZKd2L0) | Rendered page embed; not present in the page's source Markdown |

The inventory is reproducible with:

```text
node scripts/apollo-docs/audit-public-documentation-media.mjs \
  docs/apollo/public-documentation-manifest.tsv OUTPUT_JSON
```

The output contains source URLs and metadata only. Video, audio, captions, and
frame bodies are not committed.

## Local evidence and frame review

YouTube denied the initial unauthenticated `yt-dlp` stream request with its
automated-traffic challenge. The operator then supplied the downloaded MP4 for
direct local review. It is 271.601 seconds of 1902×1080 H.264 video with stereo
Opus audio and has SHA-256
`672167ff253ced15281ba751ab962c889e67638ccdcc678f5f332b5ea70e50a1`.

Twenty-eight frames at ten-second intervals from 00:00 through 04:30 were
extracted and visually inspected, in addition to the four public thumbnails.
The full-duration sequence shows:

- the Hub/Agent topology animation and its report/change directions;
- the Apollo overview and managed-environment inventory;
- Grafana's DEV → STAGING → PRODUCTION promotion pipeline, contributing Entity
  health, soak progress, adjudication constraints, and channel promotion;
- the Product-side constraint inventory including labels, dependencies,
  vulnerability/security evidence, recalls, and maintenance windows;
- the Environment-side release-channel and maintenance requirements; and
- an Environment change history followed by the closing title card.

These frames corroborate the architecture, Product/Release/Channel, promotion,
Environment-constraint, and audit contracts already indexed in the capability
map. They expose no private Hub-to-Agent wire format.

## Spoken-content transcript

The supplied audio was transcribed locally with `faster-whisper` 1.2.1 and the
English `base.en` model, then checked against the reviewed frames. The 55-line,
762-word timestamp output covers speech from 00:02.860 through 04:21.620 and has
SHA-256 `15f9a15cca5d0073522eaa5628d4851f6ea140d6d215fd9611eaee2c452731b2`.
The transcript body remains an uncommitted audit artifact. This is its compact
paraphrase:

| Time | Contract-bearing content |
| --- | --- |
| 00:03–00:41 | Apollo is a central delivery platform. A Hub is the control plane; an Agent in each managed Kubernetes cluster or Environment reports status and executes Hub-recommended changes. |
| 00:41–01:16 | Managed Environments run Product Releases such as Helm charts. Application owners publish to Release Channels and Environment owners subscribe according to risk tolerance. |
| 01:16–01:50 | A configurable DEV → STAGING → PRODUCTION promotion pipeline is illustrated with a Grafana Release. DEV subscribers receive a new Release first. |
| 01:50–02:27 | Health gathered from installed Environments, soak time, and configured adjudication thresholds determine automatic channel promotion. |
| 02:27–02:58 | Promotion is one part of constraint-based deployment; labels, dependencies, vulnerabilities, recalls, and maintenance windows also gate an upgrade recommendation. |
| 02:58–03:46 | Environment owners add release-channel and maintenance requirements. An upgrade is eligible only when the Product-owner and Environment-owner requirements intersect successfully. |
| 03:46–04:22 | Constraints are evaluated continuously. Autonomous execution remains inspectable through an auditable Environment change history, balancing stability and velocity. |

## Second pass, 2026-08-17 — the first pass under-read this video

The operator re-supplied the same file; its SHA-256 is byte-identical to the
one above, so this is not a second capture. It was re-examined anyway, because
the failure recorded in
[`../evidence/apollo-notification-types.json`](../evidence/apollo-notification-types.json)
was precisely this shape: an image corpus counted, dispositioned, and never
read. The first pass's disposition — "corroborating explanation, not a new
protocol or domain state" — is true of the *architecture* and false of the
*vocabulary*.

Sampling was raised from 28 frames at ten-second intervals to a full 5-second
contact sheet of all 271 seconds, with the densest screens pulled at native
resolution. Two hard cuts exist; the rest is a continuous screen recording, so
scene-change extraction alone would have found almost nothing.

What the first pass missed is that roughly the last third of the video is a
live Hub UI at legible resolution, carrying enumerable vocabularies:

### Environment entity view (≈03:32)

Filter facets with counts, which is the settings-dialog situation again — the
facet list *is* the enum:

| Facet | Observed values |
| --- | --- |
| Product Type | K8s app, Helm chart |
| Hosting Type | Other |
| Health Status | Healthy, Indeterminate |
| Rollout Statuses | Up to date, Recalled |
| Environment Lifecycle Status | Active |
| Entity Lifecycle Status | Installed, Pending install |

The entity rows carry values the facet list does not: health `Not fully live`
and `Not reported`/`Unreported`; rollout `Current version` (with the secondary
line "Ensure rolloff") and `Blocked`. The summary bar reads
`Up to date 11  Blocked 4  Other 1` while the Rollout Statuses facet reads
`Up to date 11  Recalled 4`, so **`Blocked` in the summary and `Recalled` in
the facet count the same four entities** — a naming split worth knowing before
either word is treated as the wire value.

Also legible: `AGENT CONNECTIONS` listing `Helm chart operator` and
`Palantir operator`; a named maintenance window pair `Regular` /
`Downtime` with an `Open` badge and a named window `friday-night`.

### Plans view (≈04:18)

Filters are `status`, `entity`, `Select plan type`, and `Prior to <date>`.
The summary reads "47 succeeded, 3 failed. Currently, 0 in progress", giving
plan states `succeeded`, `failed`, `in progress`. The Type column renders as
sentences rather than enum tokens:

- `Change version to 1.15.1`
- `Installing entity version 11.3.5`
- `Modify config`
- `Modify 1 secret`
- `Modify config and modify 1 secret`

The composite form is the interesting one: config and secret changes combine
into a single Plan with a single duration, rather than being separate Plans.

Each row also shows a Product and Entity coordinate pair — `com.helmcharts.coredns`
against `com.helmcharts.coredns:coredns`, `com.palantir.deployability` against
`com.palantir.deployability:palantir-operator` — so an Entity coordinate is the
Product coordinate with `:name` appended. That is a different grammar from the
`aeid:` form in
[`../evidence/apollo-identifier-grammar.json`](../evidence/apollo-identifier-grammar.json)
and neither displaces the other.

### Release view (≈02:10)

`Product type: helm-chart.v1`, `Quorum: Any`, `Supported schema migrations:
Unspecified`, a `Security scan: Failed` panel, and a promotion-constraint table
whose rows pair a constraint with a requirement and an outcome —
`Product maintenance window` / "Releases can only be promoted during product
maintenance windows." / `Within window`, and `Health status check` /
"At least 90% of peak time" / `Passed`.

## Disposition

**Architecture: corroborating, unchanged.** Hub/Agent topology, outbound Agent
reporting and execution, promotion evaluation, constraint intersection, and
auditability are covered by executable tests. No private Hub↔Agent wire format
is exposed.

**Vocabulary: observed, and not yet reconciled.** The values above are a
single 2023 tenant's UI, not a published contract, so they are Tenant
observation under
[`../../operations/TENANT-FIDELITY.md`](../../operations/TENANT-FIDELITY.md)
and none of them is authority to rename anything. They are recorded because
Conduit ships vocabularies covering the same ground — `internal/reporting`
carries `UP_TO_DATE`/`OUT_OF_DATE`/`IN_GRACE_PERIOD` and
`HEALTHY`/`UNHEALTHY`/`INDETERMINATE`/`UNKNOWN`/`REPAIRING`/`WARNING`/`ERROR`/`TERMINAL`
— and nobody has ever compared the two. `Recalled`, `Blocked`, `Not fully
live` and `Current version` have no obvious counterpart, and
`In grace period` has no observed one. That comparison is follow-up work, not
something to settle from a marketing video.

The local source, frames, audio, and transcript remain uncommitted audit
artifacts; the digests above identify what was read.
