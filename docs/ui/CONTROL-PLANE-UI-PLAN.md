# Platform control-plane UI plan

Status: proposed upstream-aligned planning baseline, 2026-08-03. This document
describes a first-class Platform console reconstructed from public Apollo visual
evidence and Palantir's open-source Blueprint toolkit. It is not a claim of
compatibility with Palantir's private Apollo UI or GraphQL schema.

**Provenance change, 2026-08-07.** A V0 grade is added below for a live Foundry
tenant inspected at the repository owner's direction. Grades V1-V4 are public and
the reconstruction built on them is clean-room; V0 is not. The two must not be
mixed silently: any component whose design is taken from V0 has to be marked, and
the tenant-neutral / no-private-source language that `foundry-ui` and this plan
carry does not extend to it. Where V0 is used only to sanity-check work already
derived from V1-V4, say that too -- validation is not derivation, but the
distinction is only worth anything if it is written down at the time.

**V0 validation update, 2026-08-09.** A bounded cross-application audit added
seven Foundry applications, seven Ontology Manager states, and eight Developer
Console routes. Sanitized conclusions are in
`TENANT-CROSS-APP-COMPONENT-AUDIT-2026-08-09.md`; captures remain disposable
and uncommitted. The wider Foundry sidebar and secondary resource navigation
are V0 hypotheses, not grounds to silently replace the narrow Apollo rail
supported by V1/V2.

## Upstream visual evidence

The design baseline is evidence-led. Each implementation decision should cite
one of these grades in its visual contract test or story:

The V1 capture was re-run on 2026-08-07 and reproduces exactly: 391 distinct
images, 399 references, 241 pages. All 29 digests in
`upstream-screen-evidence.tsv` matched byte for byte, so the console this plan
describes still exists as described. The image bodies remain uncommitted --
they are upstream copyrighted material, and the digest table is what makes the
evidence auditable without redistributing it. `docs/ui/API-GAPS-DERIVED.md`
records what reading them changes.

| Grade | Source | What it can establish |
| --- | --- | --- |
| V0 | A live Palantir Foundry tenant (`droneconnection.usw-23`), accessed with an operator session at the repository owner's direction, 2026-08-07 | Rendered look and feel of shipped Foundry applications. **Private material.** Not redistributable, not committable, and not clean-room. Anything derived from it must say so and must not be described as a tenant-neutral reconstruction |
| V1 | 391/391 distinct images referenced 399 times across all 241 current public Apollo documentation pages, captured 2026-08-03 with the repository audit script | Current screen hierarchy, labels, repeated components, workflow steps, light-theme appearance, table and form composition |
| V2 | The 271.601-second 2023 Apollo overview video audit: 28 reviewed ten-second frames, transcript, four public thumbnails, and 1280×720 source thumbnails | Dark-theme appearance, Hub concept map, Product Release promotion screen, navigation rail, constraint presentation, animation sequence |
| V3 | `palantir/blueprint` public source at commit `a60d4c92257612808fbfac81cfeee4fcba91a8b4`, including `@blueprintjs/core` 6.18.0, icons 6.13.0, select 6.3.4, and table 6.2.4 | Licensed component behavior, Sass variables, light/dark themes, icons, focus states, desktop-density primitives |
| V4 | Written public Apollo documentation and the repository's implemented semantic contracts | Meaning and consequence of a visible control, not its private DOM or transport |
| V5 | Two exact public Apollo screenshots from the 2020 Palantir SaaS article plus the 2022 public constraint-orchestration article, retained and bounded by `HISTORICAL-APOLLO-EVIDENCE-2020-2022.md` | Historical split-pane, tabs, dense tables, event facets/timelines, desired/reported/catalog inputs, bidirectional dependency and rollback-safety concepts; not current UI or private protocol equivalence |
| Missing | `Apollo Platform Demo bundle.zip` and the local MP4 path currently point to reclaimed `/tmp/workspace-offload/factory-large-assets` bodies | Restore before claiming direct bundle DOM/CSS or full-frame visual inspection in this new design pass |

The raw image and video bodies remain temporary and are not committed. The
canonical corpus counts, capture method, hashes, transcript disposition, and
semantic findings remain in `docs/apollo/IMAGE-DERIVED-GAPS.md` and
`docs/apollo/MEDIA-AUDIT.md`.

### Recurring upstream composition

The representative screens consistently use:

- a narrow dark icon rail for global navigation and an Applications popover for
  Change Requests, Entity Map, Export, Import, and Risk Management;
- a resource header with icon, primary name, resource kind, status tags, team
  context, and an **Actions** menu at the right;
- horizontal resource tabs directly below the header;
- a pale page canvas with white, thin-bordered panels in current documentation,
  and the same hierarchy under Blueprint's dark theme in the 2023 video;
- compact searchable/filterable tables, status tags, inline actions, secondary
  left navigation for a resource subsection, and right-hand selection details;
- modal or stepped forms for creation/governance workflows; and
- a distinctive horizontal Release Channel graph whose transition nodes reveal
  promotion status, evidence, and manual actions.

These are the initial layout constraints. Local invention is allowed only when
V1–V4 do not resolve a Platform capability, and must be labelled `LOCAL` in the
wireframe/story metadata.

### Screen-to-source alignment matrix

The V1 filenames are deterministic entries from the temporary output of
`audit-public-documentation-images.mjs`; their URLs, page references, alt text,
and content hashes are retained without image bodies in
[`upstream-screen-evidence.tsv`](upstream-screen-evidence.tsv).

| Platform screen | Upstream visual anchors | Layout/component contract | Platform semantic adaptation |
| --- | --- | --- | --- |
| Global shell | V1 `136-5fc1d5ed7bc1.png`, `268-80c6c16d7a1c.png`; V2 `maxres2.jpg` | Dark icon rail, Applications popover, resource header, tabs, right Actions menu | Platform icons/names; omit unsupported notifications and terminal actions |
| Environment activity | V1 `058-f36c532b33de.png`, `157-859cdb953394.png` | Header status tags, resource tabs, secondary Activity navigation, compact filter row/table | Render native Plans, commands, suppression and label evidence |
| Environment overview | V1 `185-2c975979e5dd.png`, `194-c1c8bcca3c7d.png`, `206-e7b4fe4766e9.png` | Two-column card grid, health summary, README, properties, current Plans, connections, labels, roles, maintenance | Cards map only to implemented profile, report, Modules, Plans, registries, labels and policies |
| Environment changes | V1 `146-8cf90d80555c.png`, `200-787d9cb7c165.png` | Dense filter panel or row, bordered request records, author/reviewer/status metadata | Use native Change Request evaluations, requirements, reviews and execution |
| Product catalog | V1 `059-f4824fd095a0.png`, `061-cfeeda9755f0.png` | Search/filter table; Product header/tabs; pipeline card above Release table | Native catalog reads; preserve immutable Product/Release coordinates |
| Product Release promotion | V1 `306-23be7edbb662.png`, `314-03823957b97b.png`, `337-5246151cd265.png`, `338-a08875602cf9.png`; V2 `maxres2.jpg` | Horizontal channel graph, clickable transition nodes, constraint rows, Entity health, right details/action panel | Typed timed/canary evidence, stage pause/resume, manual promotion preview, recalls and exact scan evidence |
| Plan details | V1 `052-04270811742b.png`, `053-9e0354f8e413.png`, `057-df3a0672731c.png`, `368-7fb8ff69ba08.png` | Expandable Details panel, ordered timeline, bounded raw output, resource diagnostics, rationale comparison | Native execution phases/tasks, rollback results, risk scores, exact immutable inputs |
| Export/Import | V1 `096-745d671922c2.png`, `120-f9912a818379.png`, `130-d4b4b5ab360e.png` | Master-detail list, right properties pane, history tabs, central staged progress card | Native Pipeline previews/runs, signed Bundles, ordered redacted import tasks |
| Modules | V1 `266-4bd713439f0e.png`, `268-80c6c16d7a1c.png`, `270-20a64685b681.png` | Resource tabs, aggregate progress strip, filterable installation table | Native Module releases/installations, update state, policy and ownership |
| Risk Management | V1 `360-a76c6d631aaa.png`, `367-9261dae48169.png`, `368-7fb8ff69ba08.png` | Search, filter tags with counts, vulnerability table, affected-resource detail | Provider-neutral exact-digest evidence; no raw scanner administration claim |
| Teams and roles | V1 `349-dbded510c463.png`, `353-0d3cc061ee6d.png`, `356-773317ad2d21.png` | Searchable team selector, role menus, resource permission panels | Native Teams, grants, roles/operations and authorization audit |

### Alignment limits

- V1 screenshots span multiple Apollo releases, so a repeated pattern outranks
  a one-off spacing or label variant. The newer public screen wins when patterns
  conflict and its behavior is supported locally.
- V2 proves a first-party dark theme and the promotion screen's composition,
  but marketing animation styling is not automatically an application control.
- Blueprint source gives us supported component DOM and styling, but not
  Apollo's authored component tree, route tree, or private CSS overrides.
- Exact Apollo DOM/CSS alignment remains unclaimed until the demo bundle or an
  authorized Apollo Hub application capture is restored. Our executable target
  is **visual and interaction alignment through public evidence**, not byte- or
  selector-equivalence.

## Product direction

Build an operator console that makes the system's strongest properties visible
through the upstream Apollo interaction grammar:
immutable coordinates, desired-versus-reported state, explicit approval,
outbound-only execution, exact evidence, and rollback. The visual language
should retain Apollo's calm, desktop-dense mission-control structure while using
Platform names and only the capabilities our backend actually implements.

Use public Blueprint components and variables directly where they match V1/V2.
Do not redistribute Palantir/Apollo logos, private bundles, or proprietary
artwork. Product naming and trademarks remain Platform's, while the observable
layout and open-source component system remain aligned upstream.

### Primary users

| Persona | Needs | Default landing view |
| --- | --- | --- |
| Fleet operator | Find unhealthy, stale, drifting, blocked, or recalled installations and safely intervene | Fleet overview |
| Release manager | Understand release reachability, promotion evidence, rollout progress, and recalls | Products / promotion pipeline |
| Approver | Review exact diffs, policy requirements, provenance, and execution consequence | Change Requests inbox |
| Environment owner | Manage one Environment's profile, Entities, maintenance, Modules, and reports | Environment detail |
| Security operator | Review immutable scan evidence, recalls, and suppression provenance | Security posture |
| Platform administrator | Manage Teams, grants, approval policies, labels, and audit events | Governance |

## Information architecture

Use one Hub scope per console. Do not introduce a cosmetic Hub selector until
the backend owns a real multi-Hub query contract.

```text
Global icon rail
  Home / overview
  Search
  Environments
  Software Catalog
  Applications
    Change Requests
    Fleet (upstream Entity Map role)
    Export
    Import
    Risk Management
  Notifications (external integration until implemented)
  Settings

Resource-level tabs
  Environment: Overview · Activity · Entities · Modules · Changes · Security · Config · Settings
  Product: Releases · Entities · Recalls · Changes · Config
  Release: Overview · Security
  Module: Overview · Contents · Releases · Installations
```

Global search should resolve known Product coordinates, Environment IDs,
installation IDs, Change Request IDs, Plan IDs, Module names, and Bundle IDs.
Until a global search contract exists, use only a resource's bounded server
search where one is named. Fleet now searches Environment, installation ID,
and Product at its authorization-filtered endpoint; never silently download
every resource into the browser.

## Screen inventory and priority

### P0: coherent operating loop

| Screen | Operator question | Principal data/actions |
| --- | --- | --- |
| Overview | What needs attention now? | Fleet summary, stale reports, pending approvals, failed/running Plans, blocked promotions, recent activity |
| Fleet | Which installations are unhealthy, drifting, stale, recalled, or behind? | Faceted table, desired/observed comparison, saved URL filters, bulk-action preview |
| Environment index | Which Environments are connected and healthy? | Connection/state-source, profile, counts, latest report, active Plan |
| Environment detail | What is happening in this Environment? | Overview, Entities, Modules, Plans, Commands, maintenance, suppressions, profile and report tabs |
| Entity detail | Why is this installation in its current state? | Desired/reported versions and digests, health detail, config drift, dependencies, Plan history, bounded commands |
| Product index/detail | What releases exist and where are they running? | Immutable coordinates, channels, releases, vulnerabilities, dependencies, recalls, installations |
| Promotion pipeline | Can this Release advance, and why? | Channel graph, active/paused stages, evidence counts, timers, immutable evaluation history |
| Change Requests inbox/detail | What exactly am I being asked to approve? | Status queue, before/after diff, policy evaluations, reviewer requirements, provenance, reviews, consequence |
| Plan detail | What will or did execute? | Status, approvals, ordered steps, task timeline, diagnostics, cancellation and rollback outcome |

### P1: administration and disconnected operations

| Screen | Scope |
| --- | --- |
| Module catalog/installation | Releases, variables, lock identity, Environment installations, updates, pause/resume/unlink, auto-update policy |
| Export Pipelines | Selections, schedule, pre-build diff, target versions, build history, archive lifecycle. Three-pane: a filterable Bundle list of numbered ~2 GB builds beside a detail pane. Build outcome is tri-state -- succeeded, succeeded with advisories, failed -- and an advisory such as "no unrecalled Product Releases on their specified Release Channels" means a Bundle ships without a Product the operator expected |
| Bundle import detail | Signature/security result, added/updated/removed resources, ordered import tasks, failure diagnostics. The eight tasks are individually named and statused, and a failed task does not stop the ones behind it: upstream shows a run reaching 100% with a mid-list failure and later tasks succeeding |
| Security posture | Exact-digest assessments, active/suppressed findings, recall state, suppression provenance and expiry |
| Teams and resource grants | Team membership, resource relationships/roles/operations, inherited versus exact scope |
| Approval policies | CEL expression, typed variables, enabled state, affected changes, validation result |
| Labels | Definition revisions, accepted values, resource guidance, permissions, tombstone state |
| Authorization audit | Subject, action, decision, resource, reason, timestamp; filterable and exportable |

### Explicitly deferred

- An Apollo-compatible GraphQL Explorer. H38 lacks an authorized schema.
- A browser terminal, `kubectl exec`, or embedded SSH experience.
- Cluster administration, Namespace creation, CRDs, and Product Helm ownership.
- Raw secret values, registry credentials, scanner credentials, or Agent lease
  tokens.
- A topology animation that implies private Hub-to-Agent wire compatibility.

## Low-fidelity wireframes

The first visual prototype should cover these five frames at 1440 × 1024. They
exercise the reusable shell, tables, detail layouts, decision flows, and
evidence presentation before high-fidelity image generation. Each frame should
include a small design annotation outside the viewport naming its V1/V2 source
screen(s); the application viewport itself should not contain provenance labels.

### 1. Fleet overview

```text
┌──────────────┬─────────────────────────────────────────────────────────────┐
│ icon rail    │ Fleet / Entity Map                         Search     User │
│              ├─────────────────────────────────────────────────────────────┤
│              │ HEALTHY 842  WARNING 17  ERROR 4  STALE 9  PENDING CR 12  │
│ home         ├──────────────────────────────┬──────────────────────────────┤
│ search       │ Attention                    │ Rollout posture              │
│ environments │ ! prod/payments  3 unhealthy│ RELEASE ███████░ 84%         │
│ catalog      │ ! env/eu-west   report stale│ STABLE  █████████ 97%         │
│ applications │ ⏸ api  RELEASE→STABLE paused│ CANARY  ███░░░░░░ 31%         │
│ settings     ├──────────────────────────────┴──────────────────────────────┤
│              │ Fleet installations   [filters] [columns]                  │
│              │ Env       Entity       Desired  Observed Health  Config    │
│              │ prod-eu   payments     2.4.1    2.4.0   WARN    APPLIED   │
│              │ prod-us   search       8.1.0    8.1.0   OK      DRIFTED   │
└──────────────┴─────────────────────────────────────────────────────────────┘
```

### 2. Environment detail

```text
┌──────────────┬─────────────────────────────────────────────────────────────┐
│ navigation   │ Production EU       CONNECTED · current report 32s ago    │
│              │ Owner: Runtime Ops  Accreditation: IL5        [Actions ▾] │
│              ├─────────────────────────────────────────────────────────────┤
│              │ Overview  Entities  Modules  Plans  Activity  Settings     │
│              ├───────────────────────────────────┬─────────────────────────┤
│              │ Health  38/40 healthy             │ Current Plan            │
│              │ Desired generation  a8c2…         │ RUNNING · 4/6 tasks     │
│              │ Reported generation a8c2…         │ Apply payments 2.4.1    │
│              │ Config drift       1 Entity       │ [Open timeline]         │
│              ├───────────────────────────────────┴─────────────────────────┤
│              │ Entities [health] [rollout] [config] [search]              │
│              │ name       product     version/channel       state         │
└──────────────┴─────────────────────────────────────────────────────────────┘
```

### 3. Change Request review

```text
┌──────────────┬─────────────────────────────────────────────────────────────┐
│ navigation   │ CR-4821 · Upgrade payments to 2.4.1       PENDING REVIEW  │
│              │ by release-bot · 8 minutes ago                            │
│              ├───────────────────────────────┬─────────────────────────────┤
│              │ Changes (3)                   │ Approval requirements       │
│              │ payments/version              │ ✓ Product editor            │
│              │ - 2.4.0                       │ ○ Environment operator       │
│              │ + 2.4.1                       │ ✓ Security policy            │
│              │                               │                             │
│              │ artifacts                     │ Consequence                 │
│              │ + exact OCI digest            │ Creates Plan with 2 steps   │
│              │ + scan PASS · 0 critical      │ Downtime: none              │
│              ├───────────────────────────────┴─────────────────────────────┤
│              │ Rationale [_____________________________________________]  │
│              │                         [Reject]  [Approve Change Request] │
└──────────────┴─────────────────────────────────────────────────────────────┘
```

### 4. Plan execution

```text
┌──────────────┬─────────────────────────────────────────────────────────────┐
│ navigation   │ Plan pln_83…       RUNNING       Attempt 1       [Cancel] │
│              │ Environment prod-eu · approved by 2 reviewers              │
│              ├─────────────────────────────────────────────────────────────┤
│              │ ✓ Validate  ✓ Lease  ✓ Secrets  ● Apply  ○ Observe         │
│              │                                             ○ Rollback      │
│              ├──────────────────────────────┬──────────────────────────────┤
│              │ Ordered steps                │ Selected task                │
│              │ 1 upgrade database   done    │ Deployment/payments          │
│              │ 2 upgrade payments   running │ namespace runtime            │
│              │                              │ Progressing: 2/3 replicas    │
│              │                              │ [bounded Agent output]       │
│              ├──────────────────────────────┴──────────────────────────────┤
│              │ Immutable inputs: release digest · config digest · images │
└──────────────┴─────────────────────────────────────────────────────────────┘
```

### 5. Product promotion

```text
┌──────────────┬─────────────────────────────────────────────────────────────┐
│ navigation   │ com.example:payments                         [New Release] │
│              │ Releases  Installations  Promotion  Security  Settings    │
│              ├─────────────────────────────────────────────────────────────┤
│              │ RELEASE ── 1h soak ──▶ STABLE ── manual ──▶ PRODUCTION    │
│              │   18 entities             96 entities          728 entities│
│              │                    [PAUSED by j.smith]                      │
│              ├──────────────────────────────┬──────────────────────────────┤
│              │ Release 2.4.1               │ Stage evidence               │
│              │ Scan PASS · signed · SBOM    │ Healthy 17/18 · min 16      │
│              │ RELEASE: installed 17/18     │ Soak 43m / 60m              │
│              │ STABLE: pending              │ Recall threshold 1          │
│              │                              │ [View evaluation history]   │
└──────────────┴─────────────────────────────────────────────────────────────┘
```

## Visual system

### Recommended direction: upstream Blueprint light and dark

- Use `@blueprintjs/core`, `@blueprintjs/icons`, `@blueprintjs/select`, and
  `@blueprintjs/table` rather than redrawing matching controls. Pin exact package
  versions and record their license and source commit in the release SBOM.
- Use Blueprint's light theme as the current-documentation baseline and its
  `.bp6-dark` theme as the 2023-video baseline. Theme choice follows the user's
  explicit preference, then system preference; both are first-class.
- Retain the upstream blue primary intent and green/orange/red semantic intents.
  Every status is still paired with text/icon/shape.
- Retain the upstream information density: compact controls, thin dividers,
  modest radii, restrained elevation, and fluid-width tables. Do not introduce
  oversized dashboard cards, glassmorphism, gradients, or decorative glow.
- Use Blueprint typography metrics initially. A product-owned monospace face may
  be added for coordinates, digests, versions, durations, and bounded output if
  visual comparison proves it does not disturb the upstream hierarchy.
- Implement the narrow icon rail, resource header/tabs, panel grid, split list
  and detail pane, and Release Channel graph as a small Platform layout layer
  over Blueprint primitives.

The product must meet WCAG 2.2 AA in both themes and remain usable at 200% zoom.

### Token families

```text
blueprint.light.* / blueprint.dark.* / blueprint.intent.*
platform.rail / resource-header / canvas / selected-row / pipeline-edge
platform.status.unknown / stale / recalled / governance
space.compact / standard / section
type.coordinate / digest / output
layer.rail / header / popover / dialog / drawer / toast
```

## DOM and component architecture

Preserve native semantics before adding ARIA:

```html
<body>
  <div id="app">
    <header><!-- product, search, notifications, identity --></header>
    <nav aria-label="Primary"><!-- grouped route links --></nav>
    <main id="main-content">
      <header><!-- breadcrumb, resource title, status, actions --></header>
      <nav aria-label="Resource"><!-- tabs where applicable --></nav>
      <section aria-labelledby="..."><!-- route content --></section>
    </main>
    <div aria-live="polite"><!-- bounded async announcements --></div>
  </div>
</body>
```

Recommended reusable component layers (Blueprint primitive in parentheses):

```text
AppShell
├─ IconRail / ApplicationsMenu (Icon, Button, Menu, Popover)
├─ GlobalSearch (Omnibar/Select composition)
├─ ResourceHeader / ResourceTabs (EntityTitle, Tag, Tabs, Menu)
├─ StatusTag / EvidenceTag / Coordinate / Timestamp (Tag, Tooltip)
├─ MetricStrip / AttentionQueue (Card, Callout)
├─ FilterBar / DataTable / Pagination (InputGroup, Select, HTMLTable/Table)
├─ DesiredReportedDiff / StructuredDiff / DigestComparison
├─ ReleaseChannelGraph / ExecutionTimeline / ActivityTimeline
├─ RequirementChecklist / ReviewHistory / RationaleField (FormGroup, TextArea)
└─ ActionPanel / ConfirmDialog / ToastRegion (Drawer, Dialog, OverlayToaster)
```

Rules:

- URLs own filters, sort, tab, cursor, and selected resource where practical.
- Tables use actual table markup, persistent headers, keyboard-operable sort,
  and a stacked definition-list alternative below the mobile breakpoint.
- Destructive/governed operations show the exact affected resources and
  required rationale before submission. Never rely on color-only confirmation.
- Pending, empty, forbidden, stale, partial, and failed states are designed
  components, not ad hoc text.
- Polling must preserve focus and row position; no full-page refreshes.
- Server authorization is authoritative. Client capability checks only hide or
  explain unavailable actions and never substitute for enforcement.

The Release Channel graph is a semantic ordered graph, not a screenshot or one
opaque canvas. Linear pipelines use an `<ol>` of channel `<li>` elements with
button transition nodes between them. Branches retain a keyboard-readable
adjacency list; SVG/CSS edges are decorative and hidden from accessibility APIs.
Selecting a channel or transition opens the evidenced details/action panel and
updates a labelled status region.

## Frontend architecture

Recommended shape:

```text
web/
├─ src/app/             route tree, providers, error boundaries
├─ src/features/        fleet, environments, catalog, delivery, governance…
├─ src/components/      reusable semantic components
├─ src/api/             generated transport types plus handwritten adapters
├─ src/styles/          tokens, reset, themes, component layers
├─ src/test/            deterministic API fixtures and browser helpers
└─ public/              product-owned static assets only
```

Use TypeScript and the React versions supported by the pinned Blueprint release,
a small declarative router, and a query cache for server state. Import Blueprint
CSS/Sass as the foundation; keep Platform layout overrides in narrowly scoped
CSS modules and semantic custom properties. Avoid a general client-side state
store until a workflow proves it is needed. Form state is local; server
resources stay in the query cache; route state stays in the URL.

Build the console as static assets and embed the production output in the Hub
binary under `/console/`. Keep APIs same-origin. The Go HTTP composition root
mounts the console, but domain packages do not import frontend or HTTP types.
Frontend transport DTOs belong to an inbound console adapter and call existing
application services through ports, preserving the repository's hexagonal
boundary.

Start with request/response REST. Poll attention summaries and active Plans at
a bounded cadence with visibility-aware backoff. Add server-sent events only
after measurements show polling is inadequate; do not invent WebSockets merely
for visual liveliness.

## API coverage and gaps

Existing endpoints already cover much of the operating loop. The UI should not
use Agent mutation routes or private repository access.

| UI need | Existing contract | Required addition or decision |
| --- | --- | --- |
| Fleet summary/table | `GET /v1/fleet/installations` has authorization-before-pagination, stable `(environment, installationId)` keyset order, filter-bound opaque cursors, bounded server search, 100/200 default/maximum page size, global authorized summaries, and URL-owned console state | Scale contract implemented and bounded by `FLEET-PAGINATION-CONTRACT.md`; bulk-action selection across pages remains a separate governed workflow |
| Product and Environment indexes | Native `GET /v1/products` and `GET /v1/environments` authorize and search before stable keyset pagination; `view=summary` is bounded at 100/200 and omits nested Releases/Entities, while compatibility `view=full` is capped at 20 and explicitly traversed by the operator adapter | Scale contract implemented in `CATALOG-PAGINATION-CONTRACT.md`; bulk selection and independently paged nested detail tables remain separate workflows |
| Product/Release detail | Native `GET /v1/products/{product}/releases` plus Product-filtered `GET /v1/promotions` | Add recall/security summary and installation counts without turning browser joins into authority |
| Environment detail | Native `GET /v1/environments/{id}` plus filtered Plans and Commands | Keep heavy tables independently paged; the UI now provides equivalent mobile definitions for Entities, Plans, and Commands |
| Entity detail | fleet row plus public installation route | Add native installation detail and filtered Plan/activity reads |
| Change Requests | Existing list/detail/review routes; the list accepts `environment` alone | The index offers nine filters -- assigned team, pending response from team, Product, namespace, Environments (multi), status, labels, opened-by-me, reviewed-by-me -- plus cursor pagination and optimistic revision support. The two by-me toggles require the session identity |
| Plans | `GET /v1/plans` authorizes before Environment/status/Installation/search filtering, counting, and opaque newest-first keyset pagination; narrow summaries are bounded at 100/200 and full compatibility pages at 20 | Scale contract implemented in `PLAN-PAGINATION-CONTRACT.md`; Activity still needs Plan-type and `Before` filters, execution duration, selectable started-at sort, and independently bounded activity/event reads. Plan type is a projection over steps, not a stored field |
| Promotions | Existing list/detail/stage control | Add Product/channel/version filters consistently and a graph projection if client joins become excessive |
| Export Pipelines | Existing list, preview, runs, build/lifecycle | Add pipeline detail and Bundle/build history index |
| Bundle imports | Existing import list and Bundle detail | Add import detail route if list payload is intentionally summarized |
| Security | Scan ingestion and Environment suppressions | Add read-only exact-digest assessment/recall projection; raw provider data remains external |
| Governance | Teams, grants, policies, labels, audit | Add effective capability projection and pagination/filtering |
| Current user/actions | Authenticated `GET /v1/session` returns bounded actor/effective identity and fixed global/resource capabilities | Keep endpoint authorization authoritative and never expose credentials or arbitrary stored operation strings |
| Overview/search | Native bounded `GET /v1/overview` with six independently current/unavailable panels | Add indexed `/v1/search` only after resource-level authorization tests; never infer clear from an unavailable Overview source |

### Cross-cutting API requirements

- Define an internal OpenAPI 3.1 contract for console-facing `/v1` reads and
  governed writes, then generate TypeScript transport types from the pinned
  document. The Go domain model remains authoritative; transport DTOs may be
  deliberately narrower.
- Standardize errors as `{code, message, requestId, details?}` and never place
  credentials, secret values, Agent tokens, or unbounded output in `details`.
- Use opaque cursor pagination, explicit sort keys, server-side filtering, UTC
  timestamps, and exact string coordinates/digests.
- Expose `revision`/ETag and require `If-Match` for edits where concurrent
  governance matters. Surface conflicts as reviewable refresh/diff states.
- For browser sessions use the external identity boundary already trusted by
  the Hub, secure same-site cookies or trusted proxy authentication, CSRF
  protection on mutations, strict origin checks, and the existing secure
  response headers. Never enable insecure identity headers in production.
- Every action response returns the created Change Request, Plan, import, or
  other durable evidence rather than optimistic success with no coordinate.

## Responsive and accessibility contract

- Desktop target: 1280–1920 px. Tablet: 768–1279 px with collapsible nav and
  reduced columns. Mobile: read/triage and safe approvals; complex policy,
  pipeline, and bulk editors may use a guided full-screen flow.
- Full keyboard navigation, visible focus, skip link, landmark headings, and
  predictable focus restoration after dialogs and route changes.
- Respect reduced motion. Timeline movement is discrete and never required to
  understand status.
- Charts have equivalent tables or lists. Every status has text, not just hue.
- Dates show relative and exact UTC values; user locale/timezone presentation
  never changes stored values.

## Delivery sequence and acceptance gates

### Phase 0 — decisions and prototype

1. Approve the upstream icon rail/resource-tab navigation, Blueprint light/dark
   themes, and the five P0 frames.
2. Restore the dangling demo bundle/MP4 bodies if available; otherwise mark the
   V2 frame coverage as inherited from the prior hash-bearing audit.
3. Create monochrome wireframes first, then one high-fidelity Product Release
   promotion image using realistic but synthetic data. Compare it side by side
   with V1 image `306-23be7edbb662.png` and V2 thumbnail `maxres2.jpg`.
4. Review hierarchy, density, component usage, state coverage, and unsupported
   capabilities. Pixel differences require an intentional upstream-version,
   accessibility, or Platform-semantic explanation.

Gate: the five wireframes cover every P0 workflow, and the high-fidelity image
has no component that implies an unsupported backend capability.

### Phase 1 — read-only vertical slice

Implement App Shell → Fleet Overview → Environment detail → Plan detail using
real APIs and synthetic test fixtures. Add session/capability, overview,
Environment index/detail, native catalog reads, and Plan index endpoints.

Gate: TypeScript checks, component tests, Go race/vet, Playwright desktop/mobile
flows, automated accessibility scan, authorization tests, and a production Hub
binary serving hashed embedded assets without network-time dependencies.

### Phase 2 — governed decisions

Add Change Request review, Plan approval/cancellation, promotion pause/resume,
commands, and bulk-action previews. Every mutation requires rationale where the
domain requires it and navigates to returned durable evidence.

Gate: no UI action bypasses server authorization or approval; stale revision,
double-submit, partial failure, forbidden, and cancellation races have browser
tests.

### Phase 3 — catalogs, transfer, security, governance

Add Product/Module management views, promotions, Export/Import, security
posture, Teams/grants, policies, labels, and audit.

Gate: each screen has empty/loading/error/forbidden/stale states, server-side
pagination, keyboard coverage, and a named API contract test.

### Phase 4 — operational refinement

Measure page-load size, interaction latency, polling volume, table scale, and
operator task completion. Add SSE, virtualization, saved views, or command
search only in response to measured need.

## Image-generation brief after wireframe approval

Generate one 16:10 desktop Product Release promotion screenshot, not a marketing
collage. It should use synthetic names and values and reproduce the evidenced
composition: dark icon rail, resource header and tabs, success/evaluation
callout, horizontal channel nodes and transitions, promotion constraints table,
test Entity health, and right-side Release details. Render it at a resolution
suitable for inspecting typography and spacing. The prompt should name
Blueprint's public dark-theme visual language and explicitly exclude Palantir
logos, Apollo branding, proprietary artwork, gradients, glassmorphism, and
illegible decorative text.

After the first image, derive rather than independently regenerate the
Environment Overview, Change Requests, Plan details, Product catalog, Export,
and Risk Management screens so the upstream shell and component system remain
visually coherent.
