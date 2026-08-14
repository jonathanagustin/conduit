# Screenshot-derived contract gaps

## Why this review existed

The 2026-08-01 documentation sweep recorded in
[`PUBLIC-DOCUMENTATION-COVERAGE.md`](PUBLIC-DOCUMENTATION-COVERAGE.md) captured
241 pages and extracted **the source Markdown embedded in each rendered page**.
It counted 144,521 words and 1,562 headings, and it never looked at the images.

The Apollo documentation carries **391 distinct screenshots and diagrams across
95 of those 241 pages**. Screenshots are where a UI-first product states its enum
values, its state machines, its field sets, and its gating rules. The prose says
an Environment has connection settings; only the dialog enumerates the four
states and what each one disables.

So the earlier sweep's page coverage was complete and its semantic coverage was
not, and the "Closure record" it ended with was overstated. This note records
what the images contained, what was implemented in response, and what remains
intentionally unclaimed.

## Capture

A corrected capture on 2026-08-02 UTC re-crawled every URL in
[`public-documentation-manifest.tsv`](public-documentation-manifest.tsv) and
extracted every rendered HTML `<img>` reference. The earlier audit inspected
only Markdown image syntax and silently missed 110 raw-HTML images.

| Measure | Result |
| --- | ---: |
| Pages re-fetched | 241 |
| Distinct image files referenced | 391 |
| Total image references | 399 |
| Pages carrying at least one image | 95 |
| Pages with no image | 146 |

The live navigation tree matched the manifest with no page drift, so the text
corpus needed no revision. Image bodies are deliberately not committed; the
findings below are what they established.

Image references per section, highest first: `export-import` 63, `core` 47,
`managing-environments` 45, `managing-products` 30, `managing-modules` 29,
`managing-release-channels` 25, `managing-entities` 23, `managing-labels` 21,
`managing-vulnerabilities` 18, `recalling-releases` 17,
`apollo-getting-started` 16, `debugging` 13, `managing-teams` 12,
`managing-k8s-namespaces` 10, `managing-changes` 9, `managing-secrets` 7,
`managing-notifications` 6, `managing-artifact-registries` 5,
`apollo-references` 3.

## Exhaustive disposition

Every one of the 399 references to the 391 distinct image bodies was assigned
to a contract family. Four bodies are referenced from more than one page. This
table is the coverage index; `Partial` means the image exposed a narrower gap
that is named in the capability map, not that the remaining pixels were left
unreviewed.

| Documentation section | Image references | Disposition | Contract result |
| --- | ---: | --- | --- |
| Export and import | 63 | Implemented | Selection, signing, chunking, deduplication, checkpoints, target versions, security, relayed state, governed Pipeline lifecycle, historical/standalone builds, missing-resource policy, ordered import diagnostics, autonomous validated scheduling, and refreshable pre-build export previews are implemented. |
| Core concepts | 47 | Implemented/boundary | Product/Release/Environment/Entity, dependency, constraint, health, Agent, Plan lifecycle, and bounded structured task/resource diagnostics are covered. Private Hub↔Agent wire compatibility remains unclaimed. |
| Managing Environments | 45 | Implemented | Connection/edit-source transitions, roles, settings, cancellation, artifact assignments, maintenance overrides, suppressions, and revisioned display/accreditation/contact/operational-responsibility profiles map to tested kernel contracts. |
| Managing Products | 30 | Implemented | Immutable catalog coordinates, release publication, maintenance, promotion tracking, ramped rollouts, RBAC, and governed Product/Release tombstones are covered. Platform deliberately preserves history instead of claiming physical Apollo deletion. |
| Managing Modules | 29 | Implemented | Release, installation, composite lock, variable/secret requirements, pause/resume, unlink, ownership, update proposals, blocked reasons, Change Requests, and guarded per-Module automatic approval are implemented. |
| Managing Release Channels | 25 | Implemented | Timed/canary stages, manual/fast-track promotion, labels, default/custom channels, immutable evaluation evidence, and durable governed stage pause/resume are implemented. |
| Managing Entities | 23 | Implemented | Entity settings, dependency overrides, config, secrets, channel holds, commands, reverse-order uninstall, resource restart, explicit unmanage-only ownership handoff, and undo-before-lease are covered. |
| Managing Labels | 21 | Implemented | Typed assignments, filters, bulk application authorization, promotion/export uses, and governed immutable-ID definition revisions/tombstones with accepted-value and reference protection are implemented. |
| Debugging | 13 | Boundary | Terminal profiles and JIT access remain the broker integration described in `reference/debug-access.md`; the Agent is not expanded into a privileged shell service. |
| Getting started | 16 | Mixed | Architecture, CLI onboarding, Product/Release creation, dependencies, maintenance, promotion, recall, and first deployment corroborate the detailed sections; the tenant-distributed CLI and UI onboarding are boundaries. The embedded video is reviewed in `MEDIA-AUDIT.md`. |
| Managing Teams | 12 | Mixed | Team membership, groups, roles, and resource grants are implemented. Team contacts/channels belong to notification integration. |
| Managing Vulnerabilities | 18 | Implemented/boundary | Exact-digest assessments, risk scoring, CVE-recall upgrades, and per-finding suppression scope/approval/expiry provenance are implemented. Provider-side suppression authoring and scanner administration remain external. |
| Recalling Releases | 17 | Implemented/boundary | Range/exclusion/reversal semantics and affected-Release projections are implemented; forms and banners are UI. |
| Managing Kubernetes namespaces | 10 | Boundary | Placement and deletion-safety contracts are implemented; namespace/RBAC reconciliation remains bootstrap-owned. |
| Managing Changes | 9 | Implemented/boundary | The full public Change Type vocabulary, diffs, approvals, rationale, reviewers, and state transitions are implemented; visual review is external. |
| Managing Notifications | 6 | Boundary | Durable domain-event consumers and user/team delivery preferences remain external integrations. |
| Managing Secrets | 7 | Implemented/boundary | Write-only assignment lifecycle and Plan mutations are implemented; value entry/storage stays with an external provider. |
| Managing Artifact Registries | 5 | Implemented/boundary | Assignment, mirrors, digest resolution, and opaque credential preparation are implemented; registry administration is external. |
| Apollo references | 3 | Partial | All three images show the GraphQL explorer. H38 remains blocked and the pinned 13-route OpenAPI adapter is not counted as GraphQL coverage. |

## What the first image pass established, and what was done

Each row names the contract, why it mattered rather than merely differed, and the
executable evidence. These ten findings are implemented; the exhaustive passes
then implemented H40–H42, preserved H43 as an external boundary, opened H46–H52
with explicit acceptance gates, and corrected the earlier closure claim.

### 1. Change Type taxonomy — 3 of 34 implemented

The CEL input reference enumerates 34 Change Types and, per type, which keys a
policy may read from `referencedResources`. Platform emitted three.

This is not cosmetic: a policy dispatches on `type`. A policy authored upstream to
guard `UPDATE_ENVIRONMENT_SETTINGS` matched nothing here, and silence from a
policy reads as approval. The taxonomy is now validated with its
referenced-resource requirements and fails closed outside itself. Bulk actions
Apollo does not publish are enumerated as platform-native rather than accepted
implicitly, so a typo in a published identifier still fails.

The missing identifiers also revealed concepts with no representation at all:
Space settings with per-resource overrides, egress policies, terminal access
grants, management overrides, bootstrap-mode exit, and pipeline
archive/unarchive.

Evidence: `go test ./internal/controlplane -run 'ChangeType|DesiredStateRemoval'`

### 2. CEL predicate library — 7 of 12 implemented

Missing: `diff_only_modifies_paths`, `is_author_service_user`,
`should_auto_approve_service_user`, `is_trusted_module_request`,
`is_trusted_import_request`.

`diff_only_modifies_paths` is an allowlist gate, so `**` is read as **one or
more** segments: permitting `foo/**` must not silently permit replacing `foo`
wholesale, which is a strictly broader change. Every trusted-request predicate
withdraws auto-approval once anybody other than the automation edits the request.
Trusted service accounts are operator-declared — Platform has no standing to
hardcode identities it does not own — and an empty configuration auto-approves
nothing.

Evidence: `go test ./internal/controlplane -run 'DiffOnlyModifies|ServiceUserPredicates|TrustedAutomation'`

### 3. Environment connection setting — absent

Four states: `UNKNOWN`, `AGENTS_CONNECTED`, `NO_CONNECTION_EXPECTED`,
`RELAYED_FROM_ANOTHER_HUB`. Each independently gates whether Agents may connect,
whether **Plans are issued**, whether health is tracked, whether reported state
may be imported from a Bundle, and whether the Environment may contribute to a
promotion decision.

Two consequences carry real risk. Issuing Plans for an Environment another Hub
drives creates two writers for one Environment. And an Environment holding no
observed state cannot vouch for a Release, so counting its evidence would let a
canary pass on health nobody measured.

The final audit found that the first implementation stopped at the predicate:
there was no report resource in a Bundle and no normal way to record a
non-executing topology generation. Environment selections now include a signed
exact `ENVIRONMENT_REPORT`; only an explicitly relayed target admits it, stores
Bundle/key provenance atomically with the catalog, and retains original
Agent/Plan/observation attribution. Recording any non-executing connection state
cancels pending through running work from the previous writer.

Evidence: `go test ./internal/controlplane -run 'ConnectionSetting|NonParticipating|BundleRelays'`

### 4. Environment edit source — absent

Three states: `EDIT_ON_THIS_HUB`, `EDIT_ON_ANOTHER_HUB`, `COPY_OF_ANOTHER_HUB`.
An authoritative target rejects a Bundle that would overwrite the settings it
owns, reporting the published `InvalidTargetDeclaredStateSource` or
`CannotImportInEnvironmentWithAuthoritativeDeclaredStateSource`. A read-only copy
refuses even a single local Entity config override, which is exactly what lets a
standby Hub take over unchanged.

An Environment this Hub has never reviewed is admitted: the first Bundle carrying
it creates it, and only a locally authored Environment has settings to protect.

Evidence: `go test ./internal/controlplane -run 'SettingsImport|CopyOfAnotherHub|Federation'`

### 5. Environment settings document — absent

Apollo's settings are one YAML document (`managed-helm-charts`,
`entity-locator`, `k8s-namespace`, `release-channel`,
`global-maintenance-windows` with `downtime`/`no-downtime`,
`override-product-maintenance-windows`, `dependency-stack-overrides`,
`ignored-dependencies`).

It is coupled to finding 2: CEL diff paths are expressed against **this**
document's vocabulary. Without the shape, an upstream policy either matches
nothing or matches a differently named path and permits more than intended. The
adapter converts both directions and projects a change into the document's path
vocabulary so `diff_only_modifies_paths` evaluates as it would upstream.

Evidence: `go test ./internal/controlplane -run 'ApolloEnvironmentSettings|ApolloSettingsDiff'`

### 6. Vulnerability risk score and the CVE-recall exception — absent

A 0–10 score over active, unsuppressed, out-of-grace findings, gating an
Environment-overridable Hub setting that permits upgrading from one
vulnerability-recalled Release to a less vulnerable one. Without it, an Entity on
a CVE-recalled Release was stranded there, which blocks the step-through upgrades
that would eventually reach a clean Release.

Eligibility requires all of: opt-in, every recall on the target being a
vulnerability recall, both Releases scanned inside the re-scan SLA, target risk
no higher, and no malware findings on the target. Absent malware or scan evidence
counts against a target rather than reading as clean. A manual recall is never
overridden, because an operator who recalled a Release for a reason no scanner can
see must not be second-guessed by evidence that only describes vulnerabilities.

Palantir manages its formula and does not publish it, so the scoring function is
Platform's own. What the exception depends on is comparing two Releases scored the
same way; absolute parity is neither claimed nor needed.

Evidence: `go test ./internal/controlplane -run 'RiskScore|VulnerabilityRecalledUpgrade|PlanCarriesRisk'`

### 7. Promotion stages are two typed shapes — collapsed into one

A timed stage asks whether a Release held long enough; a canary stage asks
whether a *named subset* stayed healthy, measured as a **proportion**, and it can
react by recalling. Platform had one blended policy with absolute counts.

Conditions compose with OR, so one satisfied promote condition passes even when a
stricter one does not, while a met recall condition blocks regardless: evidence
that a Release is harming canaries outranks evidence that enough are healthy. A
minimum entity count stops a filter matching one Entity from carrying a
fleet-wide decision, and an empty canary set cannot pass by vacuous truth. Label
requirements now accept a set of values per label.

Evidence: `go test ./internal/controlplane -run 'Canary|TimedStage|LabelRequirements|PromotionStage'`

### 8. Entity advanced settings — absent

A Helm release name override adopts a pre-existing release Platform did not name.
Dependency stacks scope resolution: a dependency is satisfied only by an Entity on
the same stack, and an override names where a particular dependency lives. Because
an override is an explicit statement that the dependency is *not* co-located, it
replaces the same-namespace requirement rather than adding to it — otherwise the
override would be unusable.

Evidence: `go test ./internal/controlplane -run 'HelmReleaseName|DependencyStack'`

### 9. Target Hub versions — absent

A disconnected source Hub cannot observe its target, so its only incremental
baseline was its own record of what it previously built. That record is wrong
whenever the two diverge. Guessing wrong is asymmetric: over-sending wastes
bytes, under-sending produces a Bundle the target cannot apply because it
references catalog resources the target never received.

Only exact Releases the target runs are suppressed. Running a version says nothing
about which Teams, Modules, or Environment settings the target holds.

Evidence: `go test ./internal/controlplane -run 'TargetHub|EnvironmentEntityVersions'`

### 10. Ramped rollout is a Product setting — was stage-scoped

Windows are declared per Release Channel as a Product setting and may name a
channel no promotion pipeline targets. The previous stage-scoped window could only
pace pipeline channels, so Entities following any other channel were never paced.

Evidence: `go test ./internal/controlplane -run RampedRollout`

## What the corrected rendered-image pass implemented

The 110 newly visible raw-HTML images were reviewed individually and then
cross-checked against their page text. Three contract families moved from open
to executable:

- **H41 — Export Pipeline lifecycle and alternate builds.** Pipeline definitions
  are revisioned immutably. Governed HTTP changes archive, unarchive, and
  tombstone-delete a Pipeline; deletion requires prior archive, archived
  Pipelines cannot be edited or built, and IDs cannot be silently reused.
  Historical-revision and standalone builds are self-contained snapshots and
  record the exact Pipeline revision in the signed manifest.
- **H42 — Bundle import task diagnostics.** Every staged import records an
  ordered verify/artifact/catalog/Environment task sequence. Admission finishes
  the sequence; a failed stage persists a bounded, credential-redacted local
  error without storing artifact bodies or credentials in diagnostics.
- **H46 — Export Pipeline scheduling and pre-build preview.** Pipeline
  revisions persist enabled/disabled default or custom five-field cron schedules
  with IANA timezones. The production server runs due builds under a durable
  per-minute single-flight claim that survives restart and records exact Bundle
  IDs or bounded failure. A refreshable source-side preview reports the unsigned
  resource diff without mutating Bundle lineage and can be disabled explicitly.
- **Missing-resource build policy.** Strict builds fail closed. A Pipeline that
  explicitly opts into best-effort behavior omits absent selected resources,
  dependency artifacts, or unavailable blobs instead of silently applying that
  behavior to every Pipeline.

Evidence: `go test ./internal/controlplane -run
'TestExportPipelineRevisioned|TestBundleImportPersistsFailed|TestBundleBuildMissing'`

## Recorded divergences

These are deliberate, and each is a consequence of Platform's architecture rather
than a shortfall.

* **Undeclared connection setting resolves to agents-connected.** Apollo defaults
  a new Environment to Unknown and transitions it in place when an Agent first
  connects. Platform's Environment state is Git-owned and immutable per
  generation, so there is nowhere to record such a transition. Declaring
  `UNKNOWN` opts into the bootstrap gate explicitly, and clearing it is an
  authoring action.
* **Hub-wide settings resolve at authoring time.** A Hub setting that changed
  underneath a reviewed generation would silently alter what that generation
  means, so `SpaceSettings` resolution produces a value recorded in the
  Environment's own desired state.
* **Risk scoring is Platform's formula.** Palantir's is unpublished and
  explicitly not configurable.
* **`**` matches one or more segments, not zero.** Fail-closed, because the
  function is a security allowlist.
* **Canary filters do not select by Entity label.** Health evidence carries no
  Entity labels, and matching on labels the evaluator cannot see would silently
  select nothing.

## Still unclaimed

* **The primary Apollo API is GraphQL.** The API Explorer screenshot reads
  "Explore the Apollo GraphQL schema". Platform's compatibility rests on a pinned
  13-route public OpenAPI adapter; the GraphQL surface is neither implemented nor
  claimed, and the 13/13 figure must not be read as broader coverage.
* **Notifications, Entity Map, and Risk Management remain UI/boundary
  integrations**, per the existing contract matrix.
* **No native CLI capture and no private Hub↔Agent wire evidence**, unchanged
  from H17 and H18 in the capability map.
* **Provider-side vulnerability suppression administration** remains external.
  H43 now requires and preserves image/prefix/distribution/scanner/global scope,
  category, rationale, requester, approver, Change Request, approval time, and
  active expiry for each normalized suppressed finding; this evidence is not a
  claim that Platform implements the authoring UI or private provider API.

## Method note

Findings were established from all 391 screenshot/diagram bodies plus the surrounding
documentation text, then checked against the implementation by searching for each
concept before implementing. The reproducible capture command is
`node scripts/apollo-docs/audit-public-documentation-images.mjs docs/apollo/public-documentation-manifest.tsv OUTPUT_DIR`.
It writes image bodies and a URL/page/alt/content-hash inventory only to the
operator-selected output directory; copyrighted image bodies are not committed.
Code comments that depend on a public contract now carry a canonical source URL
and review date, while `reference/review-ledger.tsv` retains the captured content
hash because a live link is not immutable provenance.
