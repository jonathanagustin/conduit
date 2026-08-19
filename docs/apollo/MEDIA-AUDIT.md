# Public Apollo video audit

## Capture result

The 2026-08-02 rendered-page crawl inspected all 241 URLs then in
[`public-documentation-manifest.tsv`](public-documentation-manifest.tsv) for
YouTube, Vimeo, Wistia, MP4, WebM, MOV, and HLS references. It found one video:

| Page | Video | Placement |
| --- | --- | --- |
| [Welcome to Apollo](https://www.palantir.com/docs/apollo/apollo-getting-started/introduction-welcome/) | [Palantir Apollo 2023 — Mission Control for Software Deployment](https://www.youtube.com/watch?v=XmLVuZKd2L0) | Rendered page embed; not present in the page's source Markdown |

A second video, **Palantir Apollo Demo Day** (`3kSGlg8d5mI`), was supplied
directly by the operator on 2026-08-17 and is **not** referenced from any page
in the manifest, so no crawl would have found it. It is audited in its own
section below.

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

**Vocabulary: observed, and now reconciled (#130).** The values above are a
single 2023 tenant's UI, not a published contract, so they are Tenant
observation under
[`../../operations/TENANT-FIDELITY.md`](../../operations/TENANT-FIDELITY.md)
and none of them is authority to rename anything. What follows maps them onto
what Conduit actually ships; nothing here renames a Conduit value.

> **Correction.** This paragraph used to attribute
> `IN_GRACE_PERIOD`/`REPAIRING`/`WARNING`/`ERROR`/`TERMINAL` to
> `internal/reporting`. They are not there. Conduit ships **two** status
> vocabularies in two bounded contexts, and conflating them is what made the
> comparison look simple:
>
> | | `internal/reporting/domain/fleet.go:44-56` (the fleet projection) | `internal/environments/domain/reporting.go:6-27` (what an Agent reports) |
> | --- | --- | --- |
> | Health | `HEALTHY` `UNHEALTHY` `UNKNOWN` | `HEALTHY` `UNHEALTHY` `INDETERMINATE` `UNKNOWN` `REPAIRING` `WARNING` `ERROR` `TERMINAL` |
> | Rollout | `UP_TO_DATE` `OUT_OF_DATE` | `UP_TO_DATE` `IN_GRACE_PERIOD` `BLOCKED` `RECALLED` `OUT_OF_DATE` |
> | Config | `APPLIED` `DRIFTED` `INDETERMINATE` | `APPLIED` `DRIFTED` `UNAVAILABLE` `INDETERMINATE` |
>
> The fleet projection is deliberately the narrower set — it is a projection,
> and `scripts/verify-console-contract.mjs` pins the console's tables to *it*
> in both directions.

| Observed in the 2023 UI | Conduit counterpart | Reading |
| --- | --- | --- |
| health `Healthy` | `HEALTHY` in both contexts | mapped |
| health `Indeterminate` | `INDETERMINATE` (environments context only; the fleet projection has no such value) | mapped, context-split |
| health `Not fully live` | none | **no counterpart** — closest are `REPAIRING`/`WARNING`, and neither is established as its equivalent |
| health `Not reported` / `Unreported` | not a health value here: absence of a report is `Stale`/`SilenceExpected` plus placeholder statuses (`fleet.go:326-332`) | modelled elsewhere |
| rollout `Up to date` | `UP_TO_DATE` in both | mapped |
| rollout `Recalled` | `RECALLED` (environments context only) | mapped, context-split |
| rollout `Blocked` | `BLOCKED` (environments context only) | mapped, context-split |
| rollout `Current version` (+ "Ensure rolloff") | none | **no counterpart** |
| rollout `IN_GRACE_PERIOD` | — | **no observed counterpart**: Conduit ships it, the 2023 UI never showed it |
| entity lifecycle `Installed` | `INSTALLED` | mapped |
| entity lifecycle `Pending install` | none — Conduit has `INSTALLED`/`UNINSTALLED`/`UNKNOWN` | **no counterpart** |
| config axis (`APPLIED`/`DRIFTED`/`INDETERMINATE`) | — | **no observed counterpart at all**: the 2023 UI showed no config-status facet |

The load-bearing observation stays the one recorded above: the summary bar's
`Blocked` and the facet's `Recalled` count the same four entities, so the two
words are one state in that UI and neither can be taken as the wire value.

This is a reconciliation, not a change: no Conduit value is renamed, added or
removed on the strength of a marketing video, and the four **no counterpart**
rows are recorded as unestablished rather than invented.

The local source, frames, audio, and transcript remain uncommitted audit
artifacts; the digests above identify what was read.

---

# Palantir Apollo Demo Day (`3kSGlg8d5mI`)

## Provenance

Supplied by the operator on 2026-08-17 as a two-volume split archive. Neither
volume is a zip Conduit produced, so both digests are recorded:

| Volume | Bytes | SHA-256 |
| --- | ---: | --- |
| `apollodemo.zip.001` | 29,360,128 | `644a92dbb134e81772df0dbf28f931b50fa395a6307df53f6bd0fc6579c823ff` |
| `apollodemo.zip.002` | 28,565,354 | `d52d6a2be57f5929cd0cba36d0f6667289713193d2cc82b73002630d75d2a60c` |

Concatenated, they yield one **stored** (uncompressed) entry,
`Palantir Apollo Demo Day [3kSGlg8d5mI] (compressed).mp4`, 57,925,238 bytes,
**CRC-32 `0x93838c22` verified against the archive's own header** — so the file
below is the complete original, not a salvaged prefix.

It is **17:40.94 of 1920×1080 AV1 with stereo Opus**, copyright 2022. It is not
referenced from any page in
[`public-documentation-manifest.tsv`](public-documentation-manifest.tsv), so the
media crawl could not have found it.

Reviewed as 10-second contact sheets across the full runtime, selected frames at
native resolution, and a local `faster-whisper` (`base.en`) transcript of the
whole audio track. Video, audio, frames, and transcript remain uncommitted audit
artifacts.

Three chapters:

| Chapter | Title | Speaker |
| --- | --- | --- |
| CH.1 | Why We Built Apollo | S. Sankar, COO |
| CH.2 | The Core Principles That Unlock Autonomous Software Deployment | G. DeArment, Chief Architect and Head of Apollo Platform |
| CH.3 | Apollo in Action: Software Demo | S. Hacker, Forward Deployed Engineer |

Chapters 1 and 2 are interviews with diagram inserts. **Chapter 3 is a narrated
walkthrough of a live Hub UI** in a light theme not seen in any other capture,
and is where most of what follows comes from.

## Chapter 2 — architectural claims on the record

Attributed spoken claims from a published corporate video: **Corroborating**
under [`../../operations/TENANT-FIDELITY.md`](../../operations/TENANT-FIDELITY.md).
Their value is explaining the *semantics* of fields recovered elsewhere as bare
values.

### Schema migration is a compile-time declaration in three parts (≈03:26)

> "at compile time, storage systems are able to declare the set of schemas that
> they are able to read from and write to, specific migration steps they
> support, and a way for the current version of their schemas to be reported at
> runtime […] This makes it possible for Apollo to safely upgrade storage
> services through schema migrations and rollback when necessary with safety
> rails that prevent data corruption."

Names the three components of a surface Conduit does **not** model. The 2023
video's Release view carries `SUPPORTED SCHEMA MIGRATIONS: Unspecified`; until
now that was a label with no stated meaning. It also sharpens the 2022
orchestration article's "validate schema-read compatibility before rollback":
the rollback safety rail is a declared readable-schema set checked against a
runtime-reported current version, not a general heuristic. Neither
`reference/product-definition.md` nor the pinned OpenAPI snapshot carries any of
the three.

### Quorum is a developer-declared availability property (≈05:12)

> "maybe I need a local persistent volume, or I need three replicas of my
> service and quorum for my service is having any replica be available."

Conduit already models this — `Quorum` is in the pinned public OpenAPI with
members `UNKNOWN`, `ANY`, `ALL`, `ALL_BUT_ONE`, `SIMPLE_MAJORITY`,
`NO_GUARANTEE`, and `AggregateHealth` folds entity health through it. The
transcript adds *why*: quorum is declared by the developer alongside replica
count and volumes and states which replicas must be available for the service to
count as available. **Corroboration, no divergence.**

### The SDK's declared properties (≈05:04–05:52)

Local persistent volume, replica count, quorum, a generated certificate with
public and private keys for TLS, and a produced API endpoint exposed over reverse
proxy at a path — obtained "without needing to understand" the Kubernetes
objects behind them and "without making a single line of code change".

### The software catalog's contents (≈06:44–07:11)

> "Apollo starts by modeling software you want to run across your environments
> and enterprise into a single software catalog […] From what is declared by the
> developers — things like cross-service dependencies, supported schema
> versions, resource requests and so forth — to information that is augmented by
> external systems like virus scanners […]"

The declared/augmented split is the seam Conduit implements for scanner
evidence. A catalog entry animation (≈06:56) shows a detail pane with exactly
three groups — **API Dependencies**, **Upgrade Strategy**, **Promotion
Criteria** — with values rendered as blank rules. The structural point stands:
**Upgrade Strategy is a declared attribute of a catalog entry**, not a Plan-time
choice, corroborating the correction landed in #116.

## Chapter 3 — the software demo

### Monitors, and the automatic recall they drive

This is the load-bearing find, and Conduit has no counterpart for it.

> "Monitors allow me to encode service level objectives from my product into
> health checks that Apollo can action […] it looks like one of the **critical**
> monitors that I encoded within my product has failed. A critical monitor
> firing is typically a sign that something has gone seriously wrong with my
> release and the release is not stable enough to be rolled out across the
> fleet." (≈11:48)

> "Apollo has automatically recalled my release, not only ensuring that
> Timekeeper 1.23.0 won't be installed on any new environments, but also kicking
> off an automated downgrade for those canary environments that had already
> upgraded." (≈12:37)

The Release page renders the outcome verbatim:

> **This release has been recalled due to the Deletion Failures monitor**
> Due to a firing Deletion Failures monitor the release has been automatically
> recalled according to monitor SLOs.
> **Strategy: Roll-off to any version newer than 1.23.0.**

with an `Unrecall` action beside it. So a Monitor is a **product-declared,
named, severity-carrying SLO** that Apollo evaluates and *acts on*, and the
recall it produces carries a **stated roll-off strategy expressed as a version
predicate**.

Conduit has none of this. `monitor` appears in the codebase only as the
permission strings `entity:mute-monitors` and `environment:mute-monitors` in the
generated role table, and as `agent.HealthMonitor`, an internal prober. There is
no declared monitor, no monitor severity, and no monitor-triggered recall.
Conduit derives recalls from vulnerability evidence and from canary conditions,
which covers part of the same ground by a different route.

### Adjudication status — a vocabulary with no Conduit counterpart

The Environments panel has `Colour by: Adjudication status` and
`Group by: Version on current`, with a four-member legend:

**Successful** · **Rolling off** · **Upgrading** · **Pending**

`adjudicat` returns nothing in Conduit's Go source. The word is already known
from two other captures — the 2020 Events `Category` facet includes
`Adjudication`, and the 2023 constraint UI uses it — so this is the third
independent sighting of an Apollo concept Conduit does not name.

The Environment table beneath it reads
`Environment name | Version | Upgrade status | Channel | Health | Rollout |
Config`, with `Upgrade status: Rolling off` — so upgrade status and rollout are
**separate columns**, not one axis.

### Release channels here are four, and named differently again

`Develop → Release candidate → Release → Stable`, under the caption "Release
will be adjudicated through this set of release channels", with an
`Add to Stable` action. The 2023 video showed `DEV → STAGING → PRODUCTION`.
Both are tenant configuration rather than a fixed vocabulary, which is worth
recording precisely because two captures disagree.

Narration confirms the automatic promotion rule: a release is adjudicated
against **declared Canary environments** and "set to automatically mark itself as
stable once that rollout is complete and all installations remain healthy
throughout the **declared soak period**" (≈11:09). That matches Conduit's canary
and soak contracts.

### The Activity feed is a domain-event vocabulary

Verbatim entries, newest first:

- Rolling installations off release
- Automatically recalled due to monitor
- Monitor fired
- Adjudicating release on 3 environments
- Release added to Release channel
- Release registered with the catalog
- Team on call has been updated

This bears on notifications: it is a second, independent event vocabulary
alongside the notification event types captured in
[`../evidence/apollo-notification-types.json`](../evidence/apollo-notification-types.json),
and the two do not overlap. Activity is per-resource history; notification types
are what a subscriber opts into. Nothing captured says whether one is derived
from the other, and they are **not** merged.

### Libraries — a per-Release software bill of materials

A `Libraries` tab on both Release and Product, with columns
`Library | License | Version dependency | Direct dependency | Verified` and a
`View source` link per row. Facets: `Type` (Open Source 55, Internal 28) and
`License` (Apache-2.0 23, GNU GPL 22, Internal 15, MIT 5, BSD 5).

> "Apollo provides me with an easy to use software bill of materials for my
> release […] I'm able to quickly filter down and understand which versions of
> Log 4J my release is leveraging." (≈15:42)

Conduit publishes Syft/SPDX SBOMs for its own release images in CI. That is a
different thing from an Apollo-side per-Release library inventory carrying
license, direct-versus-transitive, and a verified flag. Not modelled.

### Release page tabs

`Overview | Environments | Libraries | Monitors (n) | Security | Activity`, on
both a Release (`time-keeper 1.23.0`) and a Product (`time-keeper 1.23.1`), with
an on-call team (`team-transactional-storage`) in the header.

### Continuous rescanning drives recall too

> "Apollo will continue scanning my release as time goes on, ensuring that we
> catch any new vulnerabilities that are discovered in the future and can
> automatically recall this release **or notify me** that a vulnerability needs
> my attention." (≈16:28)

Conduit implements vulnerability-derived recalls. The "or notify me" branch is
the notification path, and is consistent with the two vulnerability notification
types captured on the Product settings page.

## Disposition

**Corroborating where Conduit already builds; four gaps opened where it does
not.** Nothing here exposes a private Hub↔Agent wire format. Quorum, canary
adjudication, soak, declared upgrade strategy, vulnerability-driven recall, and
the declared/augmented catalog split all align with shipped contracts.

The gaps are Monitors as a declared actionable SLO with severity and a
roll-off strategy; the adjudication status vocabulary; the per-Release Libraries
inventory; and the three-part schema-migration declaration. Each is tracked
separately. **None of it is implemented from this video** — it is one 2022
corporate film, and a narrated demo is not a contract.
