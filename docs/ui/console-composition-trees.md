# Console composition trees

A composition tree for every operator-console screen, in the Foundry
rulebook's format. This is the structural complement to
`console-visual-language.md`: that note measures the console's palette, type and
geometry; this one records, per screen, which regions nest inside which and
which Blueprint primitive draws each leaf.

Every node is `[owner] RegionName (geometry contract) -> [BP] primitives`. The
owner tags are:

- **`[shared]`** -- chrome every screen inherits: `AppFrame`, `AppHeader`,
  `NavigationRail`, and the per-page `ObjectHeader` identity row. Documented once
  below and referenced, not repeated, in each screen.
- **`[console]`** -- the feature's own composition, authored in
  `web/src/features/<screen>/`.
- **`[components]`** -- a reused console component from `web/src/components`
  (`FacetRail`, `IndexPagination`, `ReleaseChannelGraph`, `TransitionDetail`,
  the status tags).
- **`[BP]`** -- actual Blueprint primitives, named as they are imported from
  `@blueprintjs/core`. These are the real primitives, never look-alikes: where a
  screen renders a plain `<section>`/`<ul>`/`<dl>` instead of a Blueprint
  `Section`, the tree says so rather than dressing raw HTML up as a component.

The geometry in parentheses is the measured contract -- widths, grid tracks, row
bands, corners, insets -- read from each screen's CSS (`web/src/**/*.css`) and
`.tsx`, not asserted. Archetype names ("browser", "review queue", "dashboard")
are the ones `console-visual-language.md` section 8 assigns.

**Grounding.** These trees are grounded in the code as of the current `main`
(HEAD merges #285). Every region and primitive was read from the file that
renders it; no region, primitive, or measurement is invented. Where a screen has
no rail its tree has no rail; where a value is not set in the CSS it is not
stated.

**Worklist reconstruction, reconciled.** The worklist screens -- plans,
change-requests, exports, imports, risk, notifications, teams -- were adjusted by
#285 (corners onto Blueprint primitives, the rem ladder onto the decimal
5/10/15/20 rhythm). The trees below reflect that merged state: the plans
diagnostics panel is a real `Section` (4px), and the exports/imports/risk inline
detail insets are on the px ladder. What #285 did **not** change -- and Finding
4 below still records -- is that those three inline detail panels remain plain
`<section>` elements rather than Blueprint `Section`s.

---

## Shared chrome

Mounted by `web/src/app/App.tsx` around every route. `AppHeader` and
`NavigationRail` are the standing chrome; `ObjectHeader` is a shared component
each feature page renders as its own first child (so it is owned `[shared]` but
appears inside the `[console]` subtree of each screen).

```
[shared] AppFrame (console-app-frame; skip-link -> main#main-content, tabIndex -1)
├─ [shared] AppHeader (50px band app-header.css:6; 1px bottom divider :10-11; identity tile 50×50 flush-left with 1px right divider; 24px mark AppLogo.tsx round(50*0.48); title stack pad 0 12px, module title 16px/600, subtitle 12px muted; actions margin-left auto, pad-right 14px, gap 8px)
│  └─ [BP] Navbar (role banner) + Icon/AppLogo(Icon) + b/small titles + optional Tabs/lead/search/actions slots
├─ [shared] NavigationRail (two-state dark rail navigation-rail.css, bp6-dark; width from the shell `--console-rail` track app.css: 230px expanded / 48px collapsed; state from useRailCollapsed.ts, forced collapsed ≤800px)
│  ├─ [console] header — pinned collapse toggle → [BP] Tooltip + Button (variant minimal, double-chevron; absent when the viewport forces the state)
│  ├─ [console] scroll region (the stack's one overflow owner; 10px section gap) with [BP] Divider between the two groups
│  │  ├─ [console] primary group (data-testid rail-primary) → per-dest [BP] NavLink + Icon; expanded: 32px row, 17px inset, Icon 16 + 12px gap + ellipsized Text label; collapsed: 36×36 icon tile (2px corner, 44px @≤600) + per-item Tooltip
│  │  └─ [console] Applications group — expanded: [BP] Text heading "Applications" (12px/600 uppercase, 32px) + 36px NavLink rows (data-testid rail-applications); collapsed: PopoverNext + Menu + MenuItem grid (aria-label "Applications")
│  └─ active = route state (aria-current) → primary-tinted fill + 4px left indicator; hover = neutral fill (not selection)
└─ [shared] ObjectHeader (~50px subheader band; pad 10px 16px, gap 10px object-header.css:9; 1px bottom divider; 30px AppLogo; kind 12px uppercase 0.04em; title 16px/600/20 via EntityTitle)
   └─ [BP] AppLogo(Icon) + EntityTitle + Tag tags (gap 6px) + Popover/Menu/MenuItem "Actions" button (only when actions supplied)
```

Two more shared contracts every list inherits:

```
[shared] Shared table bands (console-table.css, scoped to .console-app-frame)
└─ [BP] HTMLTable → th 12px/600 muted ~32px (8px top/bottom pad + 16px line); td keep Blueprint 11px pad → ~40px simple / 51–60px rich; all cells left-aligned

[components] IndexPagination (index-pagination.css)
├─ ControlStrip band (form.console-index-controls, pad 10px 20px, gap 8px; InputGroup min-width min(360px,70vw))
└─ Pagination band (pad 10px 20px, gap 16px, space-between) → [BP] Button (Previous/Next)
```

Rail note: the rail carries `overview, environments, products, notifications` as
primary destinations and `change-requests, fleet, plans, promotions, export,
import, risk, teams` as the Applications group (`web/src/app/navigation.ts`).
Expanded, the Applications group is labeled rows under an "Applications" heading;
collapsed, it folds back into the Applications popover, and the labels and
heading give way to icons + tooltips. The primary destinations are the unheaded
core region above the first section divider -- `navigation.ts` names exactly one
group ("Applications"), so a second heading would be inventing a taxonomy the
model does not have. Search and Settings are deliberately absent (no Hub
contract behind them).

---

## overview  ·  dashboard archetype

`web/src/features/overview/OverviewPage.tsx`. A panel grid of only the things
that need a person; an all-clear fleet renders one non-ideal state, not six
empty panels.

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] OverviewPage (dashboard archetype; console-overview pad 16px)
   ├─ [shared] ObjectHeader (icon dashboard, kind "Control plane", title "Overview")
   ├─ [console] Incomplete-read banner  →  [BP] Callout (warning, "This page is incomplete"; only when a source is UNAVAILABLE)
   ├─ [console] Nothing-to-do state  →  [components] AccessibleNonIdealState (tick-circle; only when every count 0 and nothing unreadable)
   └─ [console] Panel grid  →  grid-template-columns: repeat(auto-fill, minmax(280px,1fr)); gap 12px (overview.css:3-6)
      └─ [console] Panel ×6 (Awaiting approval / Failed Plans / Running Plans / Unhealthy Entities / Stale reports / Blocked promotions)
         └─ [BP] Section + SectionCard (4px corner from --bp-surface-border-radius) → title Link + rightElement Tag (round, intent by count) + ul of Link + Tag (minimal round) items; "Clear" / "Could not be read" leaf when empty
```

---

## fleet  ·  browser archetype

Verified template, reproduced verbatim. `web/src/features/fleet/FleetPage.tsx`.

```
[shared] AppFrame
├─ [shared] AppHeader (50px band, 50×50 tile, 24px mark, 3px tab underline, 1px divider)
│  └─ [BP] Icon + Text/Title + Tabs + Button/Menu actions
├─ [shared] NavigationRail (two-state rail: 230px expanded labeled rows / 48px collapsed icon rail; see Shared chrome)
└─ [console] FleetPage (browser archetype)
   ├─ [console] SummaryBar
   │  └─ [BP] Tag (large minimal; per-health buckets + stale, warning intent)
   ├─ [console] ControlStrip (flex row, gap 8px)
   │  └─ [BP] InputGroup (search) + Button (Search, submit) + Button (Refresh, icon, loading)
   ├─ [console] BrowserGrid  →  grid-template-columns: var(--console-rail:240px) minmax(0,1fr); gap 20px
   │  ├─ [console] FleetFacetRail (240px fixed rail)  →  [components] FacetRail
   │  │  └─ [BP] Section + SectionCard + ProgressBar + Tag (count, minimal round) + Button (value select) + Collapse ("N more")
   │  └─ [console] FleetTable (minmax(0,1fr) fluid work; one scroll owner, min-width:0)
   │     └─ [BP] HTMLTable (interactive, striped) → Tag/Icon status cells + Button row actions; row bands from console-table.css: header ~32px/12px muted, rows 40–60px
   └─ [console] IndexPagination (band 10px 20px)
      └─ [BP] Button controls
```

Read against the source, `FleetPage` also mounts `[shared] ObjectHeader` (icon
`applications`, kind "Application", title "Fleet") as its first child and a
`[BP] Callout` (warning) when stale installations exist; the `--console-rail`
240px track and 10px/20px bands are set in `fleet-page.css:6,19,68`, and the
per-cell status leaves are the `[components]` status tags (`HealthTag`,
`RolloutTag`, `ConfigTag`, `LifecycleTag`). The fleet table's pagination is
inlined in `FleetPage` rather than imported from the shared `IndexPagination`
component, though it renders the same two-`Button` band.

---

## environments  ·  browser archetype

### list — `EnvironmentsPage.tsx`

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] EnvironmentsPage (browser archetype; --console-rail:260px, pad 16px environments.css:7)
   ├─ [shared] ObjectHeader (icon cloud, kind "Resource collection", title "Environments")
   └─ [console] Index panel  →  [BP] Section (compact, icon cloud, title "Visible Environments" via H2) + SectionCard (padded=false); .console-index margin 12px 16px
      ├─ [console] ControlStrip (form.console-index-controls)  →  [BP] InputGroup + Button (Search)
      ├─ [console] EnvironmentsTable  →  [BP] HTMLTable (interactive, striped) → Link (id) + Tag (connection / "never reported" warning) + Timestamp
      ├─ [console] Mobile records (dl grid, @≤767px)
      └─ [components] IndexPagination
```

### detail — `EnvironmentDetailPage.tsx`

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] EnvironmentDetailPage (console-environment)
   ├─ [shared] ObjectHeader (icon cloud, kind "Environment", title = id, subtitle "reporting via <agent>", tag = connection setting)
   └─ [BP] Tabs / Tab (renderActiveTabPanelOnly): Overview · Plans · Commands
      ├─ [console] Overview tab — BrowserGrid  →  grid-template-columns: var(--console-rail:260px) minmax(0,1fr); gap 20px (environments.css:18-21); rail collapses @≤900px
      │  ├─ [console] OverviewSummary pane (fixed 260px)  →  [BP] Section + SectionCard ("Health", "Generation") + Tag (warning "N drifted") + Timestamp
      │  └─ [console] EntitiesPanel work (minmax(0,1fr), min-width:0)  →  [BP] HTMLTable (striped) → [components] HealthTag + LifecycleTag; [BP] Callout (warning "never reported"); mobile dl records @≤767px
      ├─ [console] Plans tab  →  [BP] HTMLTable (striped) → NavLink + [components] PlanStatusTag; mobile dl records
      └─ [console] Commands tab  →  [BP] HTMLTable (striped) → [components] PlanStatusTag + Timestamp; mobile dl records
```

---

## products  ·  browser archetype

### list — `ProductsPage` in `ProductsPage.tsx`

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] ProductsPage (browser archetype; --console-rail:260px, pad 16px products.css:5)
   ├─ [shared] ObjectHeader (icon cube, kind "Resource collection", title "Software Catalog")
   └─ [console] Index panel  →  [BP] Section (compact, icon cube, title "Visible Products" via H2) + SectionCard (padded=false)
      ├─ [console] ControlStrip  →  [BP] InputGroup + Button (Search)
      ├─ [console] ProductsTable  →  [BP] HTMLTable (interactive, striped) → Link (product) + release/latest cells
      ├─ [console] Mobile records (dl grid)
      └─ [components] IndexPagination
```

### detail — `ProductDetailPage` in `ProductsPage.tsx`

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] ProductDetailPage (console-products)
   ├─ [shared] ObjectHeader (icon cube, kind "Product", title = product, subtitle "N Releases")
   └─ [console] BrowserGrid  →  grid-template-columns: var(--console-rail:260px) minmax(0,1fr); gap 20px (products.css:13-16); rail collapses @≤900px
      ├─ [console] ProductResourceSummary pane (fixed 260px)  →  [BP] Section + SectionCard ("Resource") → dl facts (coordinate mono) + Tag (channels minimal round; "N with active findings" warning)
      └─ [console] Work column (min-width:0, flex column, gap 16px)
         ├─ [console] ProductPromotionPipeline  →  [BP] Section + SectionCard ("Release Channel pipeline") → [components] ReleaseChannelGraph + TransitionDetail
         ├─ [console] Release table  →  [BP] HTMLTable (striped) → Tag (channels) + mono digest cell + AssessmentTag (Tag) wrapped in [BP] Button when findings expandable
         └─ [console] VulnerabilityDetail (inline, revealed by row)  →  [BP] Section + SectionCard ("Vulnerability findings for <ver>") → HTMLTable ×2 (active findings, suppressed)
```

The pipeline and assessment panels are real Blueprint `Section`/`SectionCard`, so
their 1px hairline, white surface and 4px corner come from
`--bp-surface-border-radius`, not from `products.css` (noted at `products.css:26`).

---

## promotions  ·  review-queue archetype

`web/src/features/promotions/PromotionsPage.tsx`. A fleet-wide promotions view,
labelled a Conduit extension in its own banner. No `ObjectHeader`.

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] PromotionsPage (review queue; console-promotions pad 16px)
   ├─ [console] Scope banner  →  [BP] Callout (info-sign, compact — "this fleet-wide view is a Conduit extension")
   ├─ [console] ControlStrip (form.console-index-controls)  →  [BP] InputGroup + Button (Search)
   ├─ [console] Per-Product group ×N  →  [BP] Section (compact, title = product; card margin-bottom 12px, pad 12px promotions.css:2) + SectionCard (padded)
   │  ├─ [components] ReleaseChannelGraph (ordered <ol> of channels; each transition a Button)
   │  └─ [components] TransitionDetail (inline, revealed by selecting a transition)  →  [BP] Callout + Tag + ul of blockers
   └─ [components] IndexPagination
```

---

## plans  ·  browser archetype

### list — `PlansPage.tsx`

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] PlansPage (browser archetype; console-plans)
   ├─ [shared] ObjectHeader (icon gantt-chart, kind "Execution history", title "Plans")
   └─ [console] Index panel  →  [BP] Section (compact, icon gantt-chart, title "Visible Plans" via H2) + SectionCard (padded=false)
      ├─ [console] ControlStrip  →  [BP] InputGroup + Button (Search)
      ├─ [console] PlansTable  →  [BP] HTMLTable (interactive, striped) → Link + [components] PlanStatusTag + Timestamp
      ├─ [console] Mobile records (dl grid)
      └─ [components] IndexPagination
```

### detail — `PlanDetailPage.tsx`

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] PlanDetailPage (console-plan-detail, pad 16px)
   ├─ [shared] ObjectHeader (icon gantt-chart, kind "Plan", title = id, subtitle env/generation/created; tags = PlanStatusTag + Tag "N of M approvals")
   ├─ [console] Failure / cancellation banners  →  [BP] Callout (danger / warning)
   ├─ [console] Steps  →  [BP] H4 + HTMLTable (striped): #/Action/Entity/Product/Version/Namespace
   └─ [console] Task timeline  →  [BP] H4 + <ol>.console-plan-detail__timeline (order is the information)
      └─ per task: [BP] Tag (phase) + Tag (status intent) + Callout (danger, task failure) + Section/SectionCard diagnostics (collapsible, open by default on failure, 4px via --bp-surface-border-radius) + Pre (agent output)
```

---

## change-requests  ·  review-queue archetype

### list — `ChangeRequestsPage.tsx`

The inbox: requests awaiting approval sort first. This index has **no
`ObjectHeader`** and titles its `Section` without an `H2` renderer — see findings.

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] ChangeRequestsPage (review queue)
   └─ [console] Index panel  →  [BP] Section (compact, icon inbox, title "Change Requests"; default heading, no H2 renderer) + SectionCard (padded=false)
      ├─ [console] ControlStrip  →  [BP] InputGroup + Button (Search)
      ├─ [console] ChangeRequestsTable  →  [BP] HTMLTable (interactive, striped) → Link + ChangeStatusTag (Tag) + Tag (outstanding count, warning)
      └─ [components] IndexPagination
```

### detail — `ChangeRequestDetailPage.tsx`

Routes to a full page (not an inline detail pane); the decision actions live here.

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] ChangeRequestDetailPage (console-change, pad 16px)
   ├─ [shared] ObjectHeader (icon inbox, kind "Change Request", title "id · title", subtitle env/author; tag = ChangeStatusTag)
   ├─ [console] Description  →  <p> (muted, max 70ch) ; evaluation error  →  [BP] Callout (danger)
   ├─ [console] Who must review  →  [BP] H4 + HTMLTable (striped) → Tag (outstanding warning)
   ├─ [console] What the policies said  →  [BP] H4 + HTMLTable (striped)
   ├─ [console] What changes  →  [BP] H4 + Card per change (console-change__diff pad 12px) → Tag + HTMLTable (compact, striped) field diff
   ├─ [console] Reviews  →  [BP] H4 + <ul> → Tag (decision)
   ├─ [console] Consequence  →  [BP] H4 + Callout (gantt-chart / warning / info-sign)
   └─ [console] Review form  →  [BP] H4 + Callout (danger, on error) + TextArea (rationale) + action row [BP] Button (Reject danger / Approve primary / Cancel) ; separate [BP] Button (Reopen, no rationale)
```

---

## exports  ·  review-queue archetype (row → inline detail)

`web/src/features/exports/ExportPage.tsx`. Selecting a row reveals its detail
inline; there is no build-history endpoint, so none is shown.

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] ExportPage (review queue)
   ├─ [shared] ObjectHeader (icon export, kind "Resource collection", title "Export")
   └─ [console] Index panel  →  [BP] Section (compact, icon export, title "Export pipelines" via H2) + SectionCard (padded=false)
      ├─ [console] ControlStrip  →  [BP] InputGroup + Button (Search, local filter)
      ├─ [console] PipelinesTable  →  [BP] HTMLTable (interactive, striped) → Tag (status / build strategy / success guarantees); rows selectable, selected bg rgba(45,114,210,.15)
      └─ [console] DetailPanel (inline, nonmodal; plain <section>.console-exports__detail, pad 20px, 1px top border)
         └─ raw h2/h3 + <ul> selections (each with [BP] Tag) + <dl> meta grid minmax(16rem,1fr), gap 1rem
```

---

## imports  ·  review-queue archetype (row → inline detail)

`web/src/features/imports/ImportPage.tsx`.

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] ImportPage (review queue)
   ├─ [shared] ObjectHeader (icon import, kind "Resource collection", title "Import")
   └─ [console] Index panel  →  [BP] Section (compact, icon import, title "Bundle imports" via H2) + SectionCard (padded=false)
      ├─ [console] ControlStrip  →  [BP] InputGroup + Button (Search, local filter)
      ├─ [console] ImportsTable  →  [BP] HTMLTable (interactive, striped) → mono bundle cell + Link (environment) + Tag (status intent) + Timestamp; rows selectable
      └─ [console] DetailPanel (inline, nonmodal; plain <section>.console-imports__detail, pad 20px, 1px top border)
         └─ <dl> meta grid minmax(18rem,1fr) + Link (change request) + Timestamp; danger error <p> (#cd4246); h3 "Tasks" + <ol> → [BP] Tag (status) per stage
```

---

## risk  ·  review-queue archetype (row → inline detail)

`web/src/features/risk/RiskPage.tsx`. Recalls that withdraw Release ranges.

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] RiskPage (review queue)
   ├─ [shared] ObjectHeader (icon shield, kind "Resource collection", title "Risk Management")
   └─ [console] Index panel  →  [BP] Section (compact, icon shield, title "Recalls" via H2) + SectionCard (padded=false)
      ├─ [console] ControlStrip  →  [BP] InputGroup + Button (Search, local filter)
      ├─ [console] RecallsTable  →  [BP] HTMLTable (interactive, striped) → SourceTag (Tag: SECURITY danger / MANUAL primary) + Tag (active warning / reverted); rows selectable
      └─ [console] DetailPanel (inline, nonmodal; plain <section>.console-risk__detail, pad 20px, 1px top border)
         └─ <dl> meta grid minmax(16rem,1fr) + Timestamp (freeze-until)
```

---

## notifications  ·  review-queue archetype (row → inline detail)

`web/src/features/notifications/NotificationsPage.tsx`. An inbox above a
per-resource preference editor.

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] NotificationsPage (review queue)
   ├─ [shared] ObjectHeader (icon notifications, kind "Operator", title "Notifications", subtitle = owner)
   ├─ [console] Inbox panel  →  [BP] Section (compact, icon inbox, title "Inbox" via H2, rightElement = HTMLSelect channel) + SectionCard (padded=false)
   │  ├─ [console] InboxTable  →  [BP] HTMLTable (fixed layout, interactive, striped; fixed column tracks 8rem/20rem/22rem/…/10rem notifications.css:12-40) → Timestamp + Tag (type, wraps) + mono rid cell (0.85em) + Tag (environment); rows selectable
   │  └─ [console] Bounded note  →  [BP] Callout (primary, "most recent N entries")
   └─ [console] PreferenceSection  →  [BP] Section (compact, icon cog, title "Your preference for a resource" via H2) + SectionCard (padded=false)
      ├─ [console] ControlStrip (form)  →  [BP] InputGroup (resource RID) + Button (Load preference)
      └─ [console] PreferenceEditor (console-notifications__preference, pad 12px 16px)
         └─ [BP] Callout (absent/danger) + dl meta + Switch (all future types) + fieldset.console-notifications__types (1px border, 2px corner) of Checkbox + Callout (unknown types) + Button (Save preference)
```

---

## teams  ·  browser archetype

### list — `TeamsPage` in `TeamsPage.tsx`

Bounded named array with no server cursor, so **no `IndexPagination`** and no
mobile-record reflow — see findings.

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] TeamsPage (browser archetype)
   ├─ [shared] ObjectHeader (icon people, kind "Resource collection", title "Teams")
   └─ [console] Index panel  →  [BP] Section (compact, icon people, title "Visible Teams" via H2) + SectionCard (padded=false)
      ├─ [console] ControlStrip  →  [BP] InputGroup + Button (Search, local filter)
      └─ [console] TeamsTable  →  [BP] HTMLTable (interactive, striped) → Link (name) + mono coordinate span (0.85em teams.css:2-4)
```

### detail — `TeamDetailPage` in `TeamsPage.tsx`

```
[shared] AppFrame / AppHeader / NavigationRail
└─ [console] TeamDetailPage (console-teams)
   ├─ [shared] ObjectHeader (icon people, kind "Team", title = display name, subtitle = coordinate)
   ├─ [console] Description  →  <p> (max 60ch)
   ├─ [console] RoleList ×2 (Administrators / Members)  →  plain <section> + h2 + <ul> of [BP] Tag (minimal round, icon person/people)
   ├─ [console] Labels  →  plain <section> + h2 + [BP] Tag (minimal round) per label
   └─ [console] Meta  →  <dl> grid minmax(12rem,1fr) (Revision / Created / Updated) + Timestamp
```

---

## Findings — where the code departs from the section-8 archetype map

These are read off the code, not the audit; each is a real divergence a reader
of `console-visual-language.md` section 8 would not predict.

1. **change-requests list has no `ObjectHeader`, and titles its `Section`
   without the shared `H2` renderer.** Every other index screen (environments,
   products, plans, exports, imports, risk, teams) mounts an `ObjectHeader` and
   passes `titleRenderer={H2}` to its panel; `ChangeRequestsPage` does neither,
   so its heading order differs from its siblings. (`ChangeRequestsPage.tsx`.)

2. **change-requests is grouped under "review queue (row → inline detail)" in
   section 8, but the code routes to a full detail page.** The row is a `Link`
   to `/change-requests/:id` → `ChangeRequestDetailPage`, a separate page that
   carries the decision actions — an index → detail-route (browser) shape, with
   the review surface on the detail page rather than inline. The genuine
   inline-detail review queues are exports, imports, risk and notifications.

3. **teams list is a browser/worklist screen with no pagination.** `GET
   /v1/teams` returns one bounded named array with no server cursor, so
   `TeamsPage` renders neither `IndexPagination` nor the `@≤767px` mobile-record
   reflow that the other list screens carry. It filters the loaded set locally.

4. **The inline detail panels on exports, imports and risk are plain `<section>`
   elements, not Blueprint `Section`/`SectionCard`.** They are correctly inline
   and nonmodal (never a `Dialog` or docked pane), but their surface, border and
   corners come from feature CSS on a raw `<section>` rather than from the
   primitive — unlike the products detail panels, which are real Blueprint
   `Section`s. Teams detail and the notifications preference metadata likewise
   compose raw `<section>`/`<dl>` rather than Blueprint panels.

5. **The `Dialog` gap is real and deliberate.** No screen renders a Blueprint
   `Dialog`; every contextual detail (products, exports, imports, risk,
   notifications, promotions, product vulnerability findings) is revealed inline
   from data already loaded. This matches the section-10 "inline panels" note and
   is depicted above as `DetailPanel (inline, nonmodal)`, never as a dialog.
