# API gaps derived from the verified captures

`CONTROL-PLANE-UI-PLAN.md` carries an API coverage table written from a reading
of the upstream console. This document re-derives it from the captures
themselves, screen by screen, after confirming they are what the plan says they
are.

## Provenance

The 29 captures `upstream-screen-evidence.tsv` names were re-fetched from the
`image_url` each row records, using
`docs/apollo/audit-public-documentation-images.mjs` against
`docs/apollo/public-documentation-manifest.tsv`.

All 29 SHA-256 digests matched: nothing changed, nothing was absent. The
upstream console is byte-identical to when the audit was taken, so the plan
describes a UI that still exists.

The run fetched 391 distinct images across 241 pages. The plan draws on 29 of
them; the remaining 362 are unexamined and may answer questions this document
leaves open.

The image bodies are deliberately not committed. They are upstream copyrighted
material, and the TSV of digests is what makes the evidence auditable without
redistributing it. Re-run the capture to look at them.

## What the plan understated

The failed-rollout Details panel (`052`) renders exactly the model this
repository already stores. Each row is one `ResourceDiagnostic`:

| On screen | Field |
| --- | --- |
| `Pod unschedulable`, `Resource not ready`, `Job failed` | `Reason` |
| `Pod/fake-pod-234 (v1)` | `Kind`, `Name`, `APIVersion` |
| `in namespace fake-namespace` | `Namespace` |
| `0/3 nodes are available: 3 Insufficient memory.` | `Message` |

The collapsed `Plan output` panel with its Copy control is the bounded,
redacted `AgentOutput` already covered by
`TestPlanExecutionDiagnosticsAreRedactedPersistedAndProjected`.

This screen needs no new domain modelling. It needs a read projection.

## What the plan understates in the other direction

`GET /v1/plans` was added against the plan's one-line description --
"Environment/status/installation filters and pagination". The Activity → Plans
capture (`058`) shows the console asks for more than that:

| On screen | In the endpoint today |
| --- | --- |
| Entity filter | `installationId` |
| Status filter | `status` |
| **Plan type filter** | absent |
| **`Before` date filter** | absent |
| **`DURATION` column** | absent from the projection; derivable from `PlanExecution.StartedAt`/`FinishedAt` |
| **`STARTED` column, sortable** | ordering is fixed newest-first and not selectable |
| Pagination | absent |

`TYPE` is not a stored field. The screen shows `Install 1.635.0`,
`Upgrade from 0.508.0 to 0.509.0`, `Install Artifact Registry` -- a projection
over the Plan's steps, not a column to filter on directly. Deciding where that
derivation lives is a design question the plan does not raise.

## Change Requests

The index (`146`) offers nine filters: assigned team, pending response from
team, Product, namespace, Environments (multi-select), request status, labels,
`Opened by me`, `Reviewed by me`.

`GET /v1/change-requests` accepts one query parameter, `environment`, and
returns every visible record unpaginated. Every other filter on that screen is
absent, and the two "by me" toggles need the session identity that
`GET /v1/session` would provide.

## Environment Overview

The Overview tab (`206`) composes at least eleven sources: an entity count with
an up-to-date roll-up, README, Environment properties including the RID and
last reported state, installed Modules with versions, Agent connection
liveness, Release Channel, recent Plans with status, Artifact Registries with
connection state, Labels, Roles, Contact, and a weekly maintenance window
calendar.

Most have endpoints. None have a single bounded projection, so a console
renders this screen by fanning out and joining client-side -- which is what the
plan's "one bounded Environment summary projection" is for.

## Product Release promotion

The promotion pipeline (`306`, `314`) is the most stateful screen in the
product: an ordered channel graph with transition nodes, per-channel entity
counts split by health, and live constraint evaluation -- maintenance window
`Within window`, health check `Evaluating`, canary soak timers counting up,
per-entity health percentages.

`GET /v1/promotions` and the stage-control routes exist. The graph projection
does not, and the plan is right that the alternative is an excessive client
join. This screen also sets the polling requirements for the whole console.

## Export and Import

`export-import` is the largest section of the upstream documentation by some
distance -- 61 of the 391 captured images, against two rows in the plan's
secondary screen inventory. Reading it changes several things.

### The layout is three-pane, not two-column

Export (`083`) and Import (`120`) both use icon rail, then a master list, then
a detail pane. The master list is its own filterable surface: Export shows
`Add filter` above a scrolling list of Bundles, each row carrying a build
number, a relative timestamp, a size and a status glyph. Selecting a row
changes only the detail pane.

The plan's wireframes show a single content column throughout. That is right
for Fleet and Environment detail and wrong for these two.

### Bundles are numbered builds, and they are large

The list runs 84, 83, 82, 81 downwards -- monotonic build numbers rather than
identifiers -- at 1.98 GB to 2.09 GB each. Chunking is user-visible: the
status card reads "Bundle successfully built with 3 chunks".

This repository already models that. `ChunkSizeBytes` is a pipeline setting
and `ChunkDigests` is on the manifest.

### Advisories are a third outcome (now modelled, recall half included)

The status card shows a green check and "Bundle successfully built", and
directly beneath it an Advisories panel:

> This bundle was successfully built with 1 advisory. This Bundle can be
> transferred to another Hub, but it may not contain all of the data you
> expected.
>
> **No Product Releases** -- The following Products have no unrecalled Product
> Releases on their specified Release Channels

So a Bundle build has three outcomes, not two: succeeded, succeeded with
advisories, failed. The advisory shown is recall-aware -- a Product whose
Release Channel holds only recalled Releases contributes nothing, and the
build succeeds anyway.

A Bundle that silently omits a Product an operator believes they are shipping
is exactly the failure an advisory exists to prevent, and the omission is
invisible without one.

`BundleAdvisory{Kind, Message, Subjects}` now carries this, on
`BundleManifest.Advisories`, so it crosses to the receiving Hub with the
Bundle rather than living only in the building Hub's screen. `omitempty`
keeps a clean build's manifest unchanged, which is what makes the three
outcomes distinguishable: no field, the field, or an error.

The one rule implemented is `NO_PRODUCT_RELEASES`. Deciding it needs both
halves of the build: `selectReleases` reports the Products a rule came up
empty on, and only after the label filters and dependency closure have run is
that reconciled against the Bundle that actually resulted -- because a later
rule may supply what an earlier one missed. Skipping either half produces a
confidently wrong advisory, which is worse than none.

**The recall half is now closed too.** Upstream's advisory fires for a Product
whose Channels hold only *unrecalled* Releases. The bundle path did not
mention recalls at all, so a recalled Release was still selected and still
shipped -- and the receiving Hub, which trusts the Bundle as its catalog,
learned nothing about the recall.

Deferring this looked right from the signature: `releaseRecalled` takes an
Environment, and Bundle export is not Environment-scoped, so the two seemed
hard to reconcile. The body settles it -- the parameter is discarded, with a
comment saying a recall stops a Release being selected for a new installation
*anywhere*. Global exclusion is exactly what a Bundle needs.

Selection now skips recalled Releases, and a Product left with nothing raises
the advisory above, which is why its wording matches upstream's. Two details
carried more weight than the selection change itself:

- **Dependency closure was the back door.** `latestBundleDependency` reads the
  catalog directly, so a Release excluded from selection walked straight back
  in as somebody else's requirement. It now skips recalled candidates rather
  than failing on them: an older version in the same range is usually still
  there, and taking it beats both shipping the recalled Release and refusing
  to build. Only when the entire range is recalled does the build fail, and
  it fails naming the dependency as unavailable -- which it is.
**Grounded since, and one gap it exposed.** The exclusion itself is upstream behaviour, not an
inference from the screen text: "by default, Apollo will not upgrade an Entity to a recalled
Release", and the behaviour is "also available on Export Pipelines"
([CVE-recalled Releases and upgrades](https://www.palantir.com/docs/apollo/managing-vulnerabilities/cve-recalled-releases-upgrades/),
reviewed 2026-08-07).

The same page names an exception this repository implements nowhere: "CVE-recalled Releases are
only considered when there is no eligible non-recalled target", where that target improves the
Entity's security profile. Here a recall is absolute, so a Product whose every Release is
CVE-recalled resolves to nothing at all -- in selection, in channel resolution and in export --
where upstream would still offer the least-bad one. `RecallSourceSecurity` already distinguishes
those recalls, so the input exists; the fallback does not. Recorded at `releaseRecalled`, the one
predicate all three paths route through. Adding it changes what gets installed and shipped, not
what is reported about it.

- **An explicitly named Release is still exported.** `BundleSelectRelease`
  names an exact coordinate, which is an operator overriding the catalog.
  Dropping that silently is the failure this whole section is about. Whether
  such an override should have to be declared is a separate question from
  whether selection should honour recalls, and is not answered here.

### Import tasks are named, ordered, and do not abort

The failed import (`120`) lists eight tasks, each with its own status and a
sentence describing what it does:

| Task | Description |
| --- | --- |
| Catalog delete operation import | Processing `catalog_delete_operation` bundle |
| Container image import | Uploading container images to artifact store |
| Module import | Creating new apollo modules |
| Product binary import | Uploading product binaries to artifact store |
| Product release import | Processing `product_release` bundle |
| **Product release metadata import** | Creating new apollo products -- **failed** |
| Sls config import | Uploading product configurations to artifact store |
| Vulnerability suppression import | Processing `vulnerability_suppression` bundle |

Two things follow. The overall result is "Bundle processing failed" at 100%
progress, so the run completed rather than aborting. And the two tasks after
the failed one both succeeded, so a failed task does not stop the ones behind
it.

`BundleImportTask` here already carries `Sequence`, `Stage`, `Status`,
`Resources`, `Failure`, `StartedAt` and `FinishedAt`, which is the right
shape.

**It does not continue.** Every failure path in `AdmitImport` returns, and the
deferred handler marks only the stage that was active. That is deliberate and
already tested elsewhere -- the catalog is applied atomically per Environment,
in a single `PutImportedCatalog`, so there is no partial state to report.
Upstream's tasks are independent uploads to artifact stores, which genuinely
can half-succeed; ours is one transaction. Copying the continue-past-failure
behaviour would mean giving that up, and the divergence is the right one.

What was wrong is what the operator saw. A task behind the failure was left
`PENDING` on an import that is terminal, so the one screen opened after a
failed import could not distinguish a task that was abandoned from one that
is queued. `SKIPPED` now says it, and says only it: a task that ran keeps its
result, a staged import awaiting approval still reads `PENDING`, and a clean
import produces none.

### Selection rules are faithful; one strategy does not do what it says

The five rule types and three release strategies match the published contract exactly
([data selection rules](https://www.palantir.com/docs/apollo/export-import/data-selection-rules/),
reviewed 2026-08-07): Select Environments, Modules, Products, Releases, Teams; and No / All /
Minimal Product Releases. `BundleSelect*` and `BundleReleases*` are the same five and three.

**`MINIMAL` is not implemented.** Upstream defines it as "only the minimal set of Product Releases
needed to satisfy upgrade paths for each Product". Here the constant is declared and accepted by
validation, and `selectReleases` never branches on it -- it falls through to latest-per-channel and
is indistinguishable from `ALL` without history.

That matters directly to this plan, because a pipeline editor renders these three as a choice. An
operator picking Minimal for a stepwise upgrade would be told they are getting the set that
satisfies the upgrade path and would get the newest Release alone; the build reports success and
the Bundle fails at the far end, where nothing points back at the pipeline. Until it is
implemented, a UI offering the option is offering a promise the Hub does not keep -- so either
implement it, or do not present the third choice.

Recorded as A66 in the assumptions ledger, and bounded: a sweep of all 278 exported string
constants in `internal/controlplane` found MINIMAL to be the only behavioural selector that is
validated and never branched on.

### Bundle contents is a searchable document

The contents panel is a line-numbered, syntax-highlighted viewer grouped by
resource type (`CONTAINER_IMAGE:` heading a list), with Search and Expand
controls. It is not a table.

## Plans and constraints

The `core/plans-and-constraints` captures are the most consequential read so
far, because they describe evaluation results this repository computes but
does not surface in a usable shape.

### Blocking constraints are a list, not a sentence

`034` and `060` both head an Entity's Activity tab with the attempted action
and a count: "Unable to upgrade example-product to 1.2.3 -- 1 Blocking
constraint", "Unable to install example-product 1.10.0 -- 3 Blocking
constraints". Beneath is a flat list of independently collapsible findings.

The same constraint type repeats: `060` shows three separate "Dependencies
not reporting" panels, one expanded to "The required dependencies
example-dependency are declared but not yet reporting", plus a fourth panel
headed "Dependencies are blocking this plan".

This repository computed these reasons and then flattened them. The pattern
turned out to repeat four times, and all four are now typed findings -- a
kind, a message, and whichever detail that kind carries:

| Where | Type | Was |
| --- | --- | --- |
| Module updates | `UpdateBlocker` | `Reasons` as `path + ": " + message` |
| Promotions | `PromotionBlocker` | `Reasons`, nineteen causes, five producers |
| Bundle builds | `BundleAdvisory` | nothing at all -- silent omission |
| Release selection | `ReleaseIneligibility` | nothing at all -- `has no eligible Release` |

The last was the worst of them: resolution examined every Release, worked out
exactly why it rejected each, and returned "Entity X has no eligible Release
for Product Y on channel Z" -- true, and useless. It now says which Releases,
for which reason, naming the artifacts.

### Constraints here are inputs; upstream shows them as outputs

`PlanStep` carries `RampedRollout`, `Maintenance` and `DependsOn`: constraints
as inputs to constructing a step. Upstream additionally presents constraints
as evaluated outputs explaining why an action did not produce a step at all.
Both are real, and only the second is on screen.

### Artifact availability is per-artifact and tri-state

`034` groups a release's artifacts under three headings with counts, each
carrying its own explanation:

| Group | Upstream wording | Blocks? |
| --- | --- | --- |
| Unavailable | "Apollo didn't find these artifacts" | yes -- "The plan is blocked until they are available" |
| Available | "These artifacts are available. No action needed." | no |
| Unchecked | "Apollo skipped these artifacts because no matching registry was found or the registry isn't accessible" | **no** -- "The plan won't block on them" |

Each artifact is listed by exact reference with its type and registry, and an
unchecked one carries the reason inline: `No matching registry found`.

Two differences from this repository, both worth deciding deliberately rather
than inheriting.

`ArtifactAvailabilityAssessment` carries one `Outcome` for the whole release
and a flat `ArtifactDigests` list. There is no per-artifact status and no
reason, so the three-way grouping above cannot be rendered from it.

The half of this that is derivable without a new producer is now derived.
`ReleaseIneligibility{Version, Kind, Message, Subjects}` says why a Release
was withheld, and `Subjects` names the artifacts -- an assessment covering
four of a Release's five artifacts withholds it exactly as firmly as one
reporting a failure, and said nothing about which. Coverage is scoped
per policy: a vulnerability assessment reports on images, because a chart is
not scanned for CVEs, and an availability assessment on every artifact,
because any of them can be missing from a registry.

The rendered grouping still needs what only a provider can supply -- a status
and a reason *per artifact*, which is what separates upstream's Unavailable
from its Unchecked. Deriving that is not possible from a digest list, so it
is not attempted.

More importantly, the defaults are opposite. `catalog.go` requires
`Status == "COMPLETE"`, `Outcome == "AVAILABLE"` and
`assessmentCoversRelease`, so an artifact the assessment does not cover
withholds the release: this repository fails closed. Upstream's Unchecked
group fails open, and says so on screen. Ours is the safer default; an
operator arriving from Apollo will not expect it.

### Entity tabs vary by Entity type

`034` shows Overview, Health, Activity, Resources, Config, Changes, Monitors,
Diagnostics, Settings. `060` shows Overview, Activity, Config, Changes,
Events, Monitors, Diagnostics, Security, Settings. Health and Resources
against Events and Security: the tab set is a property of the Entity, not of
the shell.

The Activity sub-navigation also carries a pending action as its first item --
"Upgrade: 1 version behind" with a warning glyph -- above Plans, Commands,
Overrides and Suppressions, and Labels.

## The application shell is capability-gated

Two captures of the same Applications menu disagree: `136` shows five apps --
Change Requests, Entity Map, Export, Import, Risk Management -- and `017`
shows seven, the extra ones including an API Explorer described as "Explore
the Apollo GraphQL schema".

The menu is therefore a projection of what a deployment has, not a fixed list.
That is direct evidence for the plan's `GET /v1/session` returning bounded
capabilities: a shell cannot hardcode its own navigation.

The plan already names the five and maps Entity Map onto Fleet deliberately,
and it already lists an Apollo-compatible GraphQL Explorer as a non-goal
because "H38 lacks an authorized schema". Both hold up.

## Suppressions and maintenance overrides

These two screens are the same screen twice, and this repository already has
almost everything they need.

### The domain model is a superset of what is shown

The Suppressions table (`177`) has columns Scope, Type, Status, Created by,
Start and End, filtered by scope, type and status, with a bulk "Remove 39
suppressions" carrying its count. Rows carry a reason beneath the scope, and
an automatic row names its cause as a link -- "Failed plan", "Release
promotion" -- with "Apollo" as the creator.

`SuppressionRecord` covers every column and more. `Source` distinguishes
`MANUAL`, `PLAN_FAILURE`, `PROMOTION` and `ENVIRONMENT_ESCALATION`, which is
one more kind than the screen shows. `PlanID` and `PromotionID` are exactly
the cause links. `Scope` is `ENVIRONMENT` or `ENTITY`, matching the scope
filter. Status is derivable from `StartsAt` and `EndsAt`.

What is missing is the query. `GET /v1/environments/{id}/suppressions` takes
no parameters at all, so scope, type and status filtering would happen in the
browser over an unpaginated list.

### Maintenance overrides mirror suppressions exactly

`212` shows Maintenance Overrides with the same columns, the same scope and
status filters, and the same Environment-or-Entity scoping -- one row scoped
to `helm-chart-operator` and one to "This environment". `MaintenanceOverride`
carries `InstallationID`, `Type`, `StartsAt`, `EndsAt`, `Reason` and
`ApprovedBy`, which is the same shape again.

Two windowed override records with identical presentation is one component,
not two. The plan lists Labels, Approval policies and Teams as separate
secondary screens without noticing that this pair collapses.

`MaintenanceOverride` is only reachable nested inside a profile or a Plan
step. There is no route that lists the overrides in force on an Environment,
which is what the screen is.

### Two shell behaviours the plan does not describe

An Environment with an override in force raises a persistent banner above the
content -- "There is an active maintenance window override on this
Environment" -- with a count control that opens the list. Suppressions do the
same on an Entity (`198`).

And a tab can carry a badge: `212` shows "Changes" with a filled dot marking
pending items. The plan's AppShell wireframe has tabs without any notion of
unread or pending state.

## Products, upgrades and promotion criteria

The Product upgrade settings (`334`) are the densest semantic screen in the
corpus, and they describe a promotion model meaningfully different from this
one.

### Health is measured as a fraction of time, not a count of Entities

Upstream configures two thresholds, both in `[0, 1]`:

- `healthyThreshold` at 0.9 -- once the soak time has passed and health
  exceeds it, the release may be promoted
- `unhealthyThreshold` at 0.4 -- "the percentage of time the service is
  reported as unhealthy"

`PromotionPolicy` here counts Entities instead: `MinimumHealthyEntities` and
`MaximumUnhealthyEntities`, with `MinimumHealthyDurationSeconds` as the soak.
"At least N canaries are healthy" and "the service reported healthy for at
least ninety percent of the window" are different axes, and neither is a
refinement of the other.

Upstream also aborts early on the unhealthy side: the release "will be
recalled, even if the entire soak time has not passed, to avoid waiting for a
promotion that would not succeed".

### Both thresholds are declared irrelevant to Helm charts

The screen says so twice, once per threshold: "Note that this is not relevant
for helm-charts."

That matters more here than anywhere else in this document. Helm charts are
this platform's delivery mechanism, so the most prominent controls on the
upstream promotion screen are the ones upstream says do not apply to how we
deliver. Copying that threshold model because it is what the console shows
would be a mistake, and the screen itself is the evidence.

### Ramped rollout: deterministic here, random upstream

Upstream describes ramped rollout as upgrading "gradually in random order,
not all at once".

`rampedRolloutConstraint` derives each installation's slot from
`sha256(environment + installation + promotion)`. That is deterministic
pseudo-randomness: the same installation always lands in the same position in
the same rollout, so its `NotBefore` does not move between evaluations.

Genuine per-evaluation randomness would let an installation's turn drift each
time the Plan is recomputed. This repository's behaviour is the better one,
and it is worth keeping deliberately rather than being replaced with the
literal reading of the screen.

### Maintenance windows agree

Upstream takes a timezone and one or more weekly ranges in the form
`Mon/10:00-Mon/17:00`, with a "Set to all-time" shortcut.
`WeeklyMaintenanceWindow` is `TimeZone` plus `Intervals []string`, which is
the same shape.

## Modules and Entities

### Blocked updates are the constraint pattern a second time

The Module installations table (`272`) marks a row "Update Blocked" and opens
a popover: a version, a count -- "1 blocker" -- and beneath it a typed
finding, "Variables cannot be inferred", with the offending variable named
(`environmentId`) and a remediation control, "Update manually".

That is the same shape as the Entity constraint list, from a different part of
the product. Two independent screens agreeing on count, typed heading, named
detail and remediation is a component, not a coincidence.

This repository is closer here than the earlier constraint finding suggested.
`ModuleUpdateProposal` already returns `Reasons` as a list over
`GET /v1/module-installations/{id}/update`, alongside `Added`, `Changed`,
`Relinquished` and `ConfigOverrideChanges`. The flattening into
`strings.Join(reasons, "; ")` happens only on the write path, when an update
is attempted and refused.

So the read projection exists and the write path discards it. What `Reasons`
lacks is the type: each entry is `path + ": " + message`, which cannot be
grouped under a heading or attached to a remediation without parsing the
string back apart.

### Module versions are opaque, not semantic

Upstream shows `V98` in a version selector. `ModuleRelease.Version` is a string
here and the *type* does not constrain it -- but the two do not agree, and the
first reading of this was wrong in the opposite direction to the one it warned
about.

`PublishBatch` is the only producer of a `ModuleRelease`, and it always assigns
`nextModuleVersion`, which returns `0.N.0`. So this repository does not accept
opaque Module versions in practice; it mints semver ones. The plan's screen
inventory describing Module versions alongside Product Releases is therefore an
accurate description of what this repository does, and an inaccurate
description of upstream.

That has one consequence worth recording. `compareVersions` falls back to a
lexicographic comparison for versions it cannot parse, which orders `V10`
before `V9`. Any Module release arriving with a version this repository did not
mint -- the interop case the Foundry adapter exists for -- would be compared
that way, and a genuine update would be answered "already at the latest
version" and silently skipped. Module update now falls back to publication
order when either version is unorderable, which is the ordering resolution
already used to pick the target.

Production persistence admits opaque Module coordinates through a bounded,
path-safe exact-identifier grammar (the observed `V98` is the pinned fixture),
preserves them byte-for-byte, and orders them only by immutable publication
time. This is a storage and update contract, not a claim that a current adapter
imports upstream Module releases or that Platform understands an opaque
coordinate's spelling.

### Destructive Entity operations are a staged wizard, not a dialog

Uninstall (`173`) is three steps: Configure details, **Check dependencies**,
Review impact. The plan calls for "destructive/governed operations show the
exact affected resources and required rationale before submission", which is
right, but a dialog is not what upstream uses -- a dependency check sits
between choosing and confirming, as its own step.

Two more things from that screen.

Uninstall and unmanage default to happening together, and the copy is careful
about when: "Apollo will issue a plan to uninstall this entity from the
environment **when it is safe to do so**". Uninstalling is scheduled through
planning rather than executed on submit. `POST /v1/plans/{planID}/undo-unmanage`
exists here, so the pairing is modelled; whether uninstall is planned the same
way is not established by the captures.

And the wizard applies labels to the Change Request it will raise -- labels
are attached at the moment of acting, not only to resources afterwards.
`ChangeRequest.Labels` already exists, so this is a matter of the action
payload accepting them.

## Risk Management

The Vulnerabilities tab (`367`) is the clearest statement of the vulnerability
lifecycle anywhere in the corpus, and this repository already implements it.

### Four states, with live counts in the filter bar

The filter chips are also the summary: `Active 30`, `In grace period 0`,
`Suppressed 0`, `Pending suppression 8`, beside a multi-select
`Severity: Critical, High`. Each chip carries its own count, so filtering and
counting are one control rather than two.

`VulnerabilityAssessment` has `ActiveVulnerabilities`, `GracePeriodFindings`
and `SuppressedFindings` with `SuppressedFindingDetails`. The fourth state,
Pending suppression, is a suppression raised but not yet approved -- which is
what `ChangeTypeModifyVulnerabilitySuppressions` produces, since suppressing a
finding here goes through a Change Request.

So all four states exist. What does not exist is a projection that counts them
together for a filter bar to render.

### Findings are counted across two dimensions

Each row reports affected resources as "8 environments • 8 releases". A CVE is
not scoped to one or the other; the console shows both, and the largest row in
the capture reads "51 environments • 25 releases".

`ArtifactDigests` and `ActiveFindings` are per-assessment, so a
CVE-to-environments-and-releases rollup is a query across assessments that no
route performs.

### Per-row badges repeat the state counts

A row carries `8 active` and `7 Pending suppressions` inline, which is the
same taxonomy again at row scope. A finding's state is therefore per
CVE-per-resource, not per CVE.

## Labels

Labels apply to Environments, Entities, Modules, Products, Product Releases
and Teams -- six resource kinds across the section's 21 captures -- with bulk
application (`236` selects three Entities to label at once) and label-based
filtering on the indexes.

`LabelDefinition` and the label routes exist here, and Release Channels take
`BundleLabelRequirement`. What the captures add is that labels are a
cross-cutting filter dimension on every index, not only a property to display:
the Environment index, the Change Request index and the Release Channel
definition all filter by them.

## Change Request detail

The detail view (`149`) is a line-numbered side-by-side diff of the
desired-state document, not a summary of intent. Added lines are highlighted
and marked, and the corresponding gap on the left is hatched.

The document being diffed is legible in the capture, and several of its keys
are this repository's model under different names:
`suppress-deployment-after-num-autosuppressions: 8` is
`AutoSuppressionThreshold`; `release-channel`, `entity-locator` and
`override-product-maintenance-windows` all have counterparts here.
`require-maintenance-windows-for-no-downtime-recalled-release-roll-offs` is
the recall roll-off and maintenance interaction this repository also models.

The right rail carries Created by and Last updated, **Requested reviewers**
with a role -- "Entity owner" -- resolved to a team and marked satisfied,
**Review history** as a separate list of who actually acted, and Labels.

`ChangeRequest` covers all of it and more. `Requirements` is
`[]ReviewerRequirement` carrying `Kind`, `Team`, `Role`, `Operation`,
`Reason` and, notably, `Alternatives` -- so a requirement can be satisfied
more than one way, which the capture does not show. `RequirementsSatisfied`
is the tick. `Reviews` is the history. `Evaluations` carries the policy
results the plan asks for. `Revision` is already there for the `If-Match`
concurrency the plan wants.

What is missing is the diff itself: `Changes []EntityChange` records what
changes, not a rendering of the document before and after. Producing a
line-level diff is a projection over the desired-state revisions, which
`GET /v1/environments/{id}/profile/revisions` already exposes.

## Debugging is out of scope, and the plan already says so

The `debugging` section is JIT access requests, access-policy checks, and an
in-browser terminal with session profiles. The plan lists "a browser
terminal, `kubectl exec`, or embedded SSH experience" among its non-goals,
and nothing in this repository implements JIT access.

Twelve captures confirming a non-goal is a useful result: it means the
section can be set aside rather than revisited.

## Teams, secrets, notifications

Teams (`351`, `356`) cover creation, roles, settings and contacts, with a
contact assignable to a team. `Team`, the grant routes and the team roles
here cover the model; the contact concept appears on the Environment Overview
as well and has no counterpart.

Secrets (`346`) show a Plan to modify a secret, which is this repository's
`SecretMutation` inside a Plan -- the same design, where changing a secret is
a planned operation rather than a direct write. The forms cover create, edit
and delete of user-defined secrets.

Notifications (`282`) attach a contact to a team for delivery. There is no
notification model here at all, and the plan does not list one.

## Study coverage

All nineteen documentation sections have been surveyed and the densest
screens in each read closely: export-import, core, managing-environments,
managing-products, managing-release-channels, managing-modules,
managing-entities, managing-labels, managing-vulnerabilities,
recalling-releases, managing-changes, managing-teams, managing-secrets,
debugging, managing-k8s-namespaces, managing-artifact-registries,
managing-notifications, apollo-getting-started and apollo-references.

Roughly forty screens were read in detail out of 299 that are screens rather
than diagrams or icons. The inventory carries page, alt text, size and digest
for every one of the 391, so any specific screen can be pulled up directly
without re-deriving the corpus.

## Still unexamined

The individual screens not read closely are mostly repetition within a
section -- one create form differs from the next by its fields. The
substantive ones left are the Entity Map, which the plan maps onto Fleet
without a capture of its own, and the Bundle transfer diagram (`097`) at 17 MB,
which is documentation rather than a screen.
