# Control-plane console — fidelity alignment

Status: judgement against the 11 captures in this directory (1440×1024),
`docs/ui/CONTROL-PLANE-UI-PLAN.md`, `docs/ui/upstream-screen-evidence.tsv`,
`docs/ui/API-GAPS-DERIVED.md`, Blueprint 6.18.0 (V3), and
`internal/controlplane/http.go`. Not a bug list. Not a claim of private Apollo
UI parity.

**Thesis.** The console has the right domain nouns and a usable operating loop
for Fleet, Plans, and Change Requests, but an Apollo-fluent operator would
misread **where promotion lives** and **how the shell is organised**. Those two
divergences outweigh table polish.

Evidence grades used below: **V1** = catalogued public-doc screenshots (alt
authoritative; bodies deliberately absent); **V2** = 2023 overview video audit;
**V3** = public Blueprint; **V4** = written public docs + this repo’s contracts.
Where a claim rests on the plan’s wireframes rather than a V1 alt, it is marked
**PLAN** (product direction), not upstream proof.

---

## Ranked by leverage

### 1. Promotion is a list of transition cards, not a Release Channel graph

**(a) Misleading divergence.** Upstream’s distinctive promotion grammar is an
ordered horizontal channel pipeline with clickable transition nodes that open
constraint / evidence / manual-action detail. The console renders a global stack
of product cards with `RELEASE → STAGING` tags and PASS/BLOCKED callouts
(`11-promotions.png`; `PromotionsPage.tsx` 28–57).

| Evidence | What it establishes |
| --- | --- |
| V1 `306-23be7edbb662.png` alt | Product Release Overview shows “Release promotion pipeline progress and results” |
| V1 `314-03823957b97b.png` alt | Selecting a **transition node** reveals promotion constraints between channels |
| V1 `337-5246151cd265.png` alt | Pipeline that all Product Releases follow, on the Product overview |
| V1 `338-a08875602cf9.png` alt | Manual promotion sidebar: label requirements, Entities affected, reason |
| V2 (plan) | Dark-theme promotion screen + navigation rail |
| PLAN recurring composition / wireframe 5 | `<ol>` of channels + button transition nodes; right evidence panel |
| V4 `API-GAPS-DERIVED.md` §Product Release promotion | Graph projection missing; stage-control routes exist |
| V4 `http.go` 1729–1821 | `GET /v1/promotions`, stage pause/resume, stage GET — capability exists |

**(b) Invention.** A first-class **global Promotions inbox** on the primary rail
(`navigation.ts` 13; `App.tsx` 33) is not what V1 places: promotion is a
**Product / Release Overview** surface (`306`, `337`), not a fleet-wide queue.
Keep a Platform “blocked promotions” attention feed if useful, but label it
`LOCAL` — do not present it as the Apollo pipeline.

**(d) Highest-leverage next change.** Rebuild promotion as a Product-scoped
`ReleaseChannelGraph` (semantic `<ol>` + transition buttons per plan DOM rules)
with a right details/action panel bound to existing promotion + stage-control
reads/writes. Demote or nest the current card list. Why this one: it is the
single most Apollo-characteristic composition the evidence establishes, the
release manager’s primary question (“can this Release advance, and why?”), and
the place where the current UI most confidently teaches the wrong model.

---

### 2. Shell IA teaches the wrong map of the product

**(a) Misleading divergence.**

- Primary rail: Overview, Fleet, Environments, Products, Plans, Promotions
  (`navigation.ts` 7–14). PLAN IA puts **Search**, Environments, Software
  Catalog on the rail; **Fleet under Applications** (Entity Map role); Plans /
  Promotions are not rail peers (`CONTROL-PLANE-UI-PLAN.md` 120–133).
- Change Requests — the approver’s home — sit behind the Applications popover
  (`navigation.ts` 16–17), while Plans/Promotions occupy rail slots. V1
  `136-5fc1d5ed7bc1.png` alt is “The Applications menu”; V4
  `API-GAPS-DERIVED.md` 370–383 confirms the menu is capability-gated
  (Change Requests, Entity Map, Export, Import, Risk Management), not a fixed
  six-item operating loop.
- Permanent product masthead (`AppFrame.tsx` 16–34; every screenshot) sits above
  a truncated content column. Upstream recurring composition (PLAN + V1
  resource screens) is: dark icon rail → **resource header** (icon, name, kind,
  status tags, team, **Actions**) → horizontal tabs. `ObjectHeader` already
  implements Actions (`ObjectHeader.tsx` 21–37, 63–78) but resource pages pass
  no `actions` (e.g. `ChangeRequestDetailPage.tsx` 75–81; Environment uses a
  bare `H3` at `EnvironmentDetailPage.tsx` 34–44).

**(b) Invention.** Promoting Plans and Promotions to rail destinations is a
Platform information-architecture choice presented as Apollo grammar. Mark
`LOCAL` or move them under Product / Applications.

**(c) Evidence silent.** Exact icon set, tooltip copy, and whether Home is a
fleet overview or a Hub concept map: V2 proves a Hub concept map in marketing
frames; do not animate topology (explicitly deferred). Prefer PLAN’s one-Hub
scope and no cosmetic Hub selector until a multi-Hub contract exists
(`CONTROL-PLANE-UI-PLAN.md` 117–118).

**Choose:** Align the rail to PLAN IA; put Fleet in Applications; put Search
(page-local Omnibar until `/v1/search`); keep Export/Import/Risk/Teams as
Applications entries with the existing honest Placeholder (`Placeholder.tsx`
6–12) — that stub is fine fidelity, not invention.

---

### 3. Environment “Overview” is the Entities table under the wrong name

**(a) Misleading divergence.** `04-environment-detail.png` /
`EnvironmentDetailPage.tsx` 52–56: Overview panel is `EntitiesPanel` — a five-
column Entity table. V1 `206-e7b4fe4766e9.png` alt: Overview with **numbered
sections**. PLAN alignment matrix + wireframe 2: two-column health / Current
Plan, then README, properties, Modules, connections, labels, roles, maintenance.
Tabs are only Overview · Plans · Commands (46–67); PLAN IA lists Overview ·
Activity · Entities · Modules · Changes · Security · Config · Settings.

**(b) Partial invention / wrong negative.** Comment at 15–18 argues unsupported
tabs must be omitted so as not to claim Hub capability. That inverts the plan’s
own empty/forbidden-state rule and the Apollo operator’s tab memory. V4
endpoints already cover much of the surface (`http.go`: profile 168+, entities
523+, commands 1381+, suppressions 2027+, module-installations 1189+, plans
1822+). Omitting the tab is stronger than “not yet projected.”

**(c) Evidence silent / free.** Exact card order and which Overview sections
collapse when empty: free. Prefer one bounded Environment summary when it
exists; until then, client-join only fields the Hub returns and show
empty/forbidden for the rest — do not fabricate README/roles.

**Choose:** Rename current table to **Entities**; rebuild Overview as the panel
grid from wireframe 2 using real profile/report/plan data; ship tab labels that
operators expect, with NonIdealState when the projection is missing.

---

### 4. Approver loop is incomplete without Consequence; inbox lacks Apollo filters

**(a) Misleading divergence.**

- Detail (`08-change-request-detail.png`; `ChangeRequestDetailPage.tsx`):
  requirements, policy outcomes, field diff, rationale, Approve/Reject — but
  **no Consequence** pane (Plan that will be created, step count, blast radius).
  PLAN wireframe 3 / P0 CR row name consequence beside approval requirements.
  An Apollo approver expects to know execution consequence before affirming.
- Inbox (`07-change-requests.png`; `ChangeRequestsPage.tsx` 13–82): bare table,
  no filters. V1 `146-8cf90d80555c.png` alt “Change Requests”; V4
  `API-GAPS-DERIVED.md` 66–75 lists nine filters vs `GET /v1/change-requests`
  accepting only `environment` (`http.go` 1522+).

**(b) Invention.** Four equal decision buttons including Cancel/Reopen inline
(`ChangeRequestDetailPage.tsx` 220–245) flatten status-gated Actions into a
footer; PLAN wireframe places `[Reject] [Approve Change Request]`. Not upstream-
proven chrome, but it invents a decision grammar that buries the primary path.

**(c) Evidence silent.** Exact Approve intent colour (success vs primary): V3
allows either; prefer primary affirming action last (Blueprint dialog convention)
unless documenting a success-semantic exception. Filter UI for Hub-unsupported
dimensions: show disabled controls with “Hub does not filter on X yet” rather
than inventing client-only filters that look global.

---

### 5. Overview is a six-panel attention dashboard, not the Apollo Home / Fleet composition

**(a) Misleading divergence.** `01-overview.png` / `OverviewPage.tsx` 77–138:
six `Section` panels (Awaiting approval, Failed/Running Plans, Unhealthy,
Stale, Blocked promotions). PLAN visual system forbids oversized dashboard cards
(comment at 156–159 correctly rejected `Card`, but the **composition** remains
a dashboard). Wireframe 1: metric strip → Attention | Rollout posture → Fleet
table. Empty “Nothing.” panels teach operators to ignore the page.

**(b) Invention.** Client-joining four list endpoints into a synthetic Home
(`OverviewPage.tsx` 18–22, 40–51) is honest about missing `/v1/overview` (good —
does not invent a Hub capability) but invents an Apollo Home layout the V1
corpus does not establish for Platform’s Overview. Mark `LOCAL`.

**(c) Evidence silent.** Exact Home contents: V1 set does not include a catalogued
Home capture among the 29. Free to follow PLAN product direction (fleet
operator landing = attention + posture), not to invent Hub concept-map chrome
(V2 marketing).

**Choose:** MetricStrip + AttentionQueue (hide zero groups) + link into Fleet /
CR / Plans / Product promotion; leave Rollout posture until channel graph
exists (item 1), or show a honest “no channel posture projection” empty.

---

### 6. Plan failure evidence drops ResourceDiagnostics the Hub already stores

**(a) Misleading divergence.** `06-plan-detail.png` / `PlanDetailPage.tsx`:
failure Callout + steps table + task timeline with optional Agent output. V1
`052-04270811742b.png` alt: Details panel **listing Kubernetes resources that
failed to become ready**. V1 `053-9e0354f8e413.png` alt: timeline **and** raw
Plan output. V4 `API-GAPS-DERIVED.md` 28–43 and `lifecycle.go` 102–112:
`ResourceDiagnostic` is already on `PlanExecutionTask.Resources`. Frontend
`PlanExecutionTask` (`resources.ts` 54–64) **omits `resources`** — a projection
gap, not a Hub gap.

**(b) Not invention** — under-rendering of a real contract. Fix by typing and
listing diagnostics; do not invent a Helm-specific panel.

**(c) Evidence silent.** Phase strip chrome vs timeline list: PLAN wireframe 4
wants Validate · Lease · …; V1 proves timeline + output + resource list. Prefer
V1 structure; phase strip is PLAN-optional if phases already appear as task rows.

---

### 7. Product surfaces are release tables without pipeline or resource tabs

**(a) Misleading divergence.** `09-products.png` / `10-product-detail.png`:
searchless catalog; detail is version/channel/digest/assessment only
(`ProductsPage.tsx` 25–104). V1 `059` alt: catalog + filter by contact team;
V1 `061` alt: Product page with Releases; V1 `337` / PLAN: pipeline card above
Release table; PLAN IA Product tabs: Releases · Entities · Recalls · Changes ·
Config.

**(b) Invention.** None severe — this is omission. Digests and assessment tags
are V4-correct and should stay.

**(c) Evidence silent.** Contact-team filter when Hub has no team field on
`GET /v1/products`: page-local only if the catalog payload carries owner/team;
otherwise disabled filter labelled Hub gap (do not invent teams).

---

### 8. Provenance comments invent capture IDs

**(b) Invention (process, not pixels).** Several files cite captures that are
not the TSV filenames (e.g. `PromotionsPage.tsx` 9 “Capture 048”;
`ProductsPage.tsx` 9 “043/044”; `EnvironmentDetailPage.tsx` 15–16 “070/069/066”;
`NavigationRail.tsx` 11 “060”). The authoritative digests are `052`…`368` in
`upstream-screen-evidence.tsv`. Wrong citations will mislead the next
reconstruction pass. Fix citations to TSV names or stop numbering.

---

## (c) Where evidence is silent — recommended choices

| Gap | Recommendation | Why |
| --- | --- | --- |
| Exact Tag `round` / solid density | Prefer `minimal` for secondary metadata; solid intent for FAILED / UNHEALTHY / BLOCKED | PLAN visual system (modest radii); V3 offers both; V1 silent on chrome |
| Light vs dark default | Keep light as docs baseline; ship `.bp6-dark` as first-class | V1 = light docs; V2 = dark video; PLAN both |
| Global Search without `/v1/search` | Omnibar over **already-loaded** page datasets + known IDs; never silent full-fleet download | PLAN IA 142–145 |
| Applications menu membership | Capability-gated list from session when available; until then hardcode the five PLAN apps + honest placeholders | V4 `API-GAPS` 370–383; GraphQL Explorer deferred |
| Export/Import layout when built | Three-pane master/detail, not single column | V4 `API-GAPS` 108–117 from V1 `120` / export captures |
| LOCAL Platform extensions | Attention overview composition; global blocked-promotions feed; Teams in Applications | Label in story/wireframe metadata per PLAN 51–53 |

---

## (d) Single highest-leverage change

**Replace the Promotions card inbox with a Product-scoped Release Channel graph
and transition detail panel**, wired to `GET /v1/promotions` and the existing
stage pause/resume/get routes, and reachable from Product detail (where V1 puts
it).

Why not shell-first or Environment Overview-first: those matter, but an
Apollo-fluent release manager’s recognition test fails hardest on promotion.
Fixing the graph also forces Product tabs, demotes the invented global
Promotions rail item, and gives Overview a real “Rollout posture” to link into.

Confidence: **high** on promotion/shell/Environment Overview (direct V1 alts +
PLAN + screenshots); **medium** on Consequence pane field inventory (PLAN
wireframe; Hub may need a consequence projection); **high** that
ResourceDiagnostics are Hub-ready and UI-dropped (Go types + API-GAPS vs
`resources.ts`).

---

## Out of scope (deferred by decision — do not “align” into)

GraphQL explorer, browser terminal / kubectl exec, cluster administration, raw
secret display, topology animation — PLAN explicit deferrals; V1 Applications
menu variants that include API Explorer are capability noise, not a
reconstruction target.
