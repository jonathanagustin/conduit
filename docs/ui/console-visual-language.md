# The console's visual language, and where it stands against the Foundry rulebook

This note is the visual complement to `foundry-frontend-architecture.md`. That
file maps how a Foundry-shaped frontend is *organised* -- layering, host/library
split, hydration -- and which of it transfers to a product with one console
embedded in a Go binary. This one maps how the console should *look*: palette,
type, spacing, corners, and the measured geometry of its chrome. It does not
restate the architecture note; it cites it where the two meet.

Two things about the source it reads from, stated once:

- The design law it aligns to is the repository owner's **reverse-engineered
  study of Palantir Foundry's frontend**. Under `docs/operations/TENANT-FIDELITY.md`
  that study is a **Tenant observation** of a third-party product and nothing
  stronger: it establishes what was visible in the captures the owner measured,
  not a Palantir contract, wire format, or compatibility promise. Every rule
  below that begins "Foundry does X" or "the rulebook measures Y" carries that
  class implicitly.
- Every claim that begins "the console does X" is grounded in a `web/src`
  file:line, greppable, not asserted. Those are **Conduit extension** claims --
  local decisions about this console -- unless they say otherwise. Where the
  console departs from the rulebook on purpose, that is recorded as a
  **divergence**, not a gap.

The two 2020 Apollo screenshots under `evidence/legacy-apollo-2020/` and the
conclusions drawn in `HISTORICAL-APOLLO-EVIDENCE-2020-2022.md` bound only
*composition* -- split list/detail, resource-scoped tabs, compact status tags,
filter facets with counts, keeping a selected record's evidence in context.
Their editor note says the suite has since changed, so they justify no palette,
navigation, label, private route, or dark-mode claim. Nothing in this note reads
a colour or a pixel off them.

---

## 1. Palette

The console renders **light only**. It ships no `.bp6-dark` /
`[data-bp-color-scheme=dark]` scope, so the UI library's light `:root` token values
are the ones in effect everywhere. This is a deliberate divergence from
Foundry's dark-capable surfaces and from the 2020 captures' dark tables: the
operator console is a daylight tool and carries one theme.

The rulebook's light palette and the console's use of it:

| Role | Rulebook value | UI-library token (verified in installed `blueprint.css`) | Where the console uses it |
| --- | --- | --- | --- |
| Primary text | `#1c2127` | `--bp-palette-dark-gray-1` (exact) / `--bp-typography-color-default-*` (computed) | header text `app-header.css:8`; frame text `app-frame.css:6`; skip-link bg `app-frame.css:17` |
| Muted text / icons | `#5f6b7c` | `--bp-typography-color-muted` / `--bp-intent-default-rest` (both exact) | 15 sites, e.g. `app-header.css:45`, `object-header.css:26`, `fleet-page.css:33`, `changes.css:19` |
| Link / interactive | `#215db0` | `--bp-intent-primary-hover` (exact) | active tab text `app-header.css:64` |
| Primary fill / focus | `#2d72d2` | `--bp-intent-primary-rest` / `--bp-emphasis-focus-color` (exact) | active tab underline `app-header.css:65`; selected transition border `release-channel-graph.css:41` |
| Canvas / muted fill | `#f6f7f9` | `--bp-palette-light-gray-5` (exact) | canvas `app.css:11`, frame `app-frame.css:7`, rail hover text `navigation-rail.css:32,37`, channel chip `release-channel-graph.css:22` |
| Raised surface | `#fff` | `--bp-palette-white` (exact) / `--bp-surface-background-color-default-rest` (computed) | panels `products.css:7,23`, `plans.css:48`, cards `object-header.css:10`, `summary-bar.css:7` |
| Neutral border | `#d3d8de` | `--bp-palette-light-gray-1` (exact) | panel/card borders, 8 sites e.g. `plans.css:47`, `products.css:6`, `notifications.css:86` |
| Affirmative state | `#1c6e42` | `--bp-typography-color-success-hover` (exact) | "clear" risk result `overview.css:15` |
| Destructive | `#cd4246` | `--bp-intent-danger-rest` (exact) | import failure text `imports.css:30` |
| Rail glyph (rest) | `#abb3bf` | `--bp-palette-gray-4` (exact) | `navigation-rail.css:24` |
| Focus ring | `#8abbff` | `--bp-palette-blue-5` (exact) | rail focus `navigation-rail.css:44` |

Every value above now routes through its UI-library variable with the hex as
fallback (see section 9). The tokens chosen are ones whose value **equals** the
hex in the installed toolkit and is **not** recomputed by UI library v6's
`@supports (color: oklch(...))` block -- so the wrapped value is bit-identical to
the literal it replaced. Section 9 records why that qualifier matters.

### Bespoke values (no exact UI-library token -- left as literals)

These are correct as bespoke and are noted so the reconstruction does not
"tidy" them into the wrong token:

- **Standard hairline `rgba(17, 20, 24, .15)`** -- the rulebook table pairs this
  with `--bp-surface-border-color-default`, but that token's value in the
  installed UI library v6 is `#5f6b7c1f` (a translucent slate), *not*
  `rgba(17,20,24,.15)` (translucent near-black, a UI library v5-era value). They
  are different colours. The console already wraps this literal in
  `var(--bp-surface-border-color-default, rgba(17,20,24,.15))` at
  `app-header.css:11,17`, `object-header.css:11`, `summary-bar.css:8`,
  `transition-detail.css:6` -- meaning those hairlines already render the toolkit
  slate, and the `rgba` is a dead fallback. This is a pre-existing choice, left
  untouched; it is called out because the rulebook's own table is the trap the
  reconstruction must not copy.
- **Primary-tint row selection `rgba(45, 114, 210, .15)`** -- primary at 15%
  alpha. the UI library's nearest surface layer token is `#2d72d20d` (~5% alpha), a
  different value, so this stays bespoke: `exports.css:6`, `risk.css:6`,
  `imports.css:6`, `notifications.css:46`.
- **`rgba(45,114,210,.10)`** selected-transition fill `release-channel-graph.css:42`;
  **`rgba(255,255,255,.16 / .10)`** rail active/hover fills
  `navigation-rail.css:33,38`; **`rgba(17,20,24,.05)`** transition hover
  `release-channel-graph.css:38` -- alpha tints with no exact token.

---

## 2. Type

Rulebook: 14px/400 body; 12px/400-600 metadata and table headings (the most
common size); 16px/600 local panel/section titles; 20-28px/600 genuine page
headings, sparingly; 600 is the emphasis weight; muted colour for column labels.

The console inherits Source Sans / body sizing from the UI library and sets type only
where it owns a title or a label:

- Body/base type is the UI library's; no console rule overrides the 14px/400 base.
- **Module title 16px/600** `app-header.css:40-41` -- matched to the rulebook
  this PR (was 14px; see section 10).
- **Section titles 16px/600** on panels: `products.css:9,25` (`h2` 16px),
  `transition-detail.css:9` (`h3` 16px) -- Aligned.
- **Metadata/labels 12px** muted: subtitle `app-header.css:44-45`,
  diagnostics `plans.css:85-86`, provenance `products.css:29-30`,
  digest `products.css:13` -- Aligned.
- **Object title 18px/600** `object-header.css:34` -- between the rulebook's
  16px panel title and its 20-28px page heading. A detail header is arguably
  either; recorded as a divergence to resolve in reconstruction, not a defect.
- **Kind line 11px** uppercase, letter-spaced `object-header.css:27` -- one step
  below the rulebook's 12px metadata floor; divergence.

---

## 3. Spacing

Rulebook: the UI library's 4px control rhythm coexists with Foundry's 5/10/15/20
decimal rhythm; do **not** normalise measured 5/10/15 to the nearest 8.

The console runs a mostly-4px/16px ladder with 10/12 decimal accents, which is
faithful to the "two rhythms coexist" law:

- Region gutters 12px/16px: `fleet-page.css:11`, `index-pagination.css:3,15`,
  `object-header.css:9`, `summary-bar.css:5`.
- Row/section internals 8px/10px/12px: `object-header.css:12` (gap 10),
  `plans.css:66` (10px 12px), `app-header.css:51` (tab gap 18).
- Page padding 16px: `changes.css:2`, `products.css:1`, `plans.css:12`,
  `overview.css:1`.
- rem-based accents in the review/detail panels (`1rem`, `1.25rem`) at
  `imports.css:16,36`, `risk.css`, `exports.css` -- a second, unit-inconsistent
  ladder that the reconstruction should reconcile toward the px decimal rhythm.

No rule normalises a measured 10/12/18 up to 16/24; the ladder is left as-is.

---

## 4. Corners

Rulebook: 4px normal surfaces/dialogs; 2-3px compact controls/tiles; 0 where
panes form one continuous frame; round for avatars/dots.

- **0** where the identity tile joins the header as one frame:
  `app-logo.css:9` (`border-radius: 0`) -- Aligned.
- **2px** compact tiles/controls: rail items `navigation-rail.css:27`, channel
  chip `release-channel-graph.css:24`, mobile record cards
  `index-pagination.css:63`, notification fieldset `notifications.css:87` --
  Aligned with the 2-3px compact band.
- Panels (`products.css:6,22`, `plans.css:47`) set a 1px border and **no**
  radius, i.e. 0 -- the rulebook's 4px "normal card" is a Gap here; the panels
  read as squared regions rather than rounded cards. Left for reconstruction.
- No rounded+elevated cards; no drop shadows on cards -- consistent with the
  rulebook's "cards flat (elevation-0)".

---

## 5. Header geometry

The header already implements the rulebook's band grammar; `app-header.css` and
`AppLogo.tsx` are the reference:

- 50px band ending in a 1px divider -- `app-header.css:6`, `:10-11`.
- Flush-left 50x50 identity tile with the divider on its right edge --
  `app-logo.css:7-8`, `app-header.css:14-17`; `AppHeader.tsx:54` passes `size={50}`.
- Left identity/title, flexible spacer, right non-shrinking actions --
  `app-header.css:20` (titles), `:71` (search flex), `:73`/`:74`
  (`actions` `margin-left:auto`).
- Three distinct shell layers kept separate: product mark (in the tile) ->
  module title (same band, `app-header.css:39` `<b>`) -> page-local subheader
  (`ObjectHeader`, `SummaryBar`) below -- not collapsed into one heading.

The measured divergences (product mark 25px vs 24px; title offset 14px vs 12px;
compact band 53px vs 50px) are in the section 10 table.

---

## 6. Tables

Rulebook: shared column tracks header<->body; header 30-34px, 12px muted/
semibold; simple row ~40px; rich two-line row 51-60px; left-align by default;
names truncate with `min-width:0`; 1px hairlines; do not force every list into a
`<table>`.

The console composes UI-library `HTMLTable` on every index and detail list (13
call sites, `git grep HTMLTable web/src/features`). It sets column behaviour and
truncation but **not** row heights -- those are UI-library `HTMLTable` defaults,
so the rulebook's 30/40/51-60px targets are inherited, not console-controlled,
and are marked Gap (unmeasured) in section 10 until the reconstruction measures
them against the UI library's rendered output.

Console-owned table decisions that *are* faithful:

- Fixed column tracks with natural widths on the notification table
  (`notifications.css:7,12,17,29,34`) -- the rulebook's "shared column tracks"
  and "a resource coordinate has no spaces to break on" reasoning.
- Truncation with `min-width:0` on names, wrap on identifiers/coordinates
  (`notifications.css:38,53`) -- the rulebook's truncate-names / keep-identifiers
  split.
- Mobile card fallback below 767px (`index-pagination.css:44-86`,
  `environments.css:17-46`) -- a progressive-subtraction reflow, not an invented
  mobile card idiom, and scoped to the narrowest breakpoint.

---

## 7. Dialogs

Rulebook: bounded task surfaces, fixed outer geometry (e.g. 550x600),
header/body/footer split, ~20px body padding, right-aligned actions, one overlay
shadow.

**The console has no dialog surface.** `git grep -n "Dialog\|Drawer\|Overlay"
web/src` is empty. Detail and evidence render as **inline panels** below the row
they concern -- an explicit divergence, stated at `products.css:16-18`: "It sits
inside the page rather than in a dialog because it is evidence about a row above
it, and losing sight of that row while reading its findings is the wrong trade."
The rulebook's dialog geometry is therefore a deliberate Gap, not a defect; if
the reconstruction introduces a true modal task surface it should adopt the
550x600/20px/right-aligned grammar then.

---

## 8. Archetypes, as applied

The rulebook names nine whole-screen archetypes. The console shell is one
**console** archetype: a 48px dark left rail (`NavigationRail`) beside a fluid
`minmax(0,1fr)` canvas (`app.css:7-11`), with a 50px identity/module header per
surface. It is not an equal-width master/detail and it does not centre the whole
chrome -- both rulebook anti-patterns avoided.

Within that shell the feature screens map to a small set of archetypes:

| Archetype | Screens |
| --- | --- |
| **browser / worklist** (index table -> detail route) | fleet, environments, products, plans, changes, teams |
| **review queue** (row -> inline detail, decision actions) | change-requests, promotions, exports, imports, risk, notifications |
| **dashboard** (panel grid) | overview (`overview.css:2-7`, `auto-fill minmax(280px,1fr)`) |

The rulebook's 230-260px category rail and ~300px detail pane are **not**
present: the console uses a 48px icon rail and routes to full-width detail pages
rather than docking a fixed detail pane. That is the largest structural
divergence and the primary subject of the sequenced reconstruction.

---

## 9. Token alignment record (this PR, deliverable 2)

Scope: `web/src/**/*.css`. Every bare hex that **equals a specific UI-library
token's value in the installed `@blueprintjs/core@6.18.0` `blueprint.css`** was
routed through that token with the hex as fallback, matching the
`var(--bp-…, #hex)` form already at `app-header.css:8-9`. No rendered colour
changes: the chosen tokens resolve to exactly their fallback hex (see the oklch
note below), and the built bundle's colour-literal multiset is unchanged.

**Verification method.** Each hex was grepped in the installed `blueprint.css`;
the `--bp-*` variable it defines was read directly, not trusted from the
rulebook table. Two table entries were rejected on that basis: the standard
hairline (`rgba(17,20,24,.15)` != the `#5f6b7c1f` border token) and the
primary-tint selection (`rgba(45,114,210,.15)` != the `#2d72d20d` layer token) --
both left bespoke (section 1).

**Why palette / non-recomputed tokens, not the semantic surface tokens.**
UI library v6 defines a second `:root` inside `@supports (color: oklch(...))` that
**recomputes** `--bp-surface-background-color-default-rest`,
`--bp-typography-color-default-*`, and the border tokens as `oklch(from ...)`
expressions. In a modern browser those resolve to *near*-white / *near*-`#1c2127`,
not the exact hex. Routing `#fff`/`#1c2127` through them would shift the pixel.
So exact-white/exact-text sites use `--bp-palette-white` / `--bp-palette-dark-gray-1`
(never recomputed), and the intent/palette tokens chosen for the rest are all
absent from the oklch block -- verified by grepping lines 211-232 of
`blueprint.css`. This keeps the change bit-exact while still themeable.

**Counts.** 11 truly bare hexes converted; 20 dead-variable wrappers repaired
(sites that wrapped a hex in a **non-existent** UI-library variable such as
`--bp-border-color`, `--bp-text-color-muted`, `--pt-intent-danger`, so they
always rendered the fallback -- now pointed at a real, equal-value token); 6
literal families left bespoke (the `rgba(...)` tints above). Left untouched:
sites already wrapping a *real* token (`app-header.css:8,9`, `object-header.css:10`,
`summary-bar.css:7`, `navigation-rail.css:11`) and the `--surface-0` console
override hook (`app-frame.css:7`), which are pre-existing decisions, not
hardcodes.

---

## 10. Geometry-conformance audit (reconstruction backlog, deliverable 3)

This is the per-region backlog for the sequenced Foundry-geometry reconstruction.
Every row cites a `web/src` file:line and the rulebook's measured value.
**No geometry below was changed except the one marked "fixed inline"** -- the
values are recorded so the owner can direct which to close. Several are
deliberate console choices, marked as such.

Verdicts: **Aligned** = matches the rulebook; **Divergent** = differs by a
concrete, closeable amount; **Gap** = the rulebook names a value the console does
not set (often inherited from the UI library) and the reconstruction must measure or
introduce it.

### Shared chrome

| Region | Property | Console (file:line) | Rulebook target | Verdict |
| --- | --- | --- | --- | --- |
| AppHeader | band height | 50px `app-header.css:6` | 50px | Aligned |
| AppHeader | bottom divider | 1px `app-header.css:10-11` | 1px hairline | Aligned |
| AppHeader | identity tile | 50x50 `app-logo.css:7-8`, `AppHeader.tsx:54` | 50x50 flush-left | Aligned |
| AppHeader | tile corner | 0 `app-logo.css:9` | 0 (continuous frame) | Aligned |
| AppHeader | product mark | 25px `AppLogo.tsx:40` (`round(50*0.5)`) | 24x24 centered | Divergent (25 vs 24) |
| AppHeader | module title | 16px/600 `app-header.css:40-41` | 16px/600 (7+36+7=50) | Aligned (**fixed inline**, was 14px) |
| AppHeader | title left offset | 14px `app-header.css:24` | 12px after tile | Divergent (14 vs 12) |
| AppHeader | subtitle | 12px muted `app-header.css:44-45` | 12px metadata | Aligned |
| AppHeader | tab underline | 2px `app-header.css:60,65` | selected emphasis underline | Aligned (idiom) |
| AppHeader | compact band | 53px `app-header.css:90` | 50px (some 48) | Divergent (53 vs 50) |
| NavigationRail | rail width | 230px expanded / 48px collapsed, via the shell `--console-rail` track `app.css:9,17` | 230-260px category rail; 50px collapsed | Aligned (reconstructed: two-state platform sidebar; 230px sits in the band, 48px is the icon rail) |
| NavigationRail | expanded row | 32px core / 36px application `navigation-rail.css:129,142`; 17px inset, Icon 16 + 12px gap + ellipsized label | 32px core / ~36px application rows; 17px pad; 16px icon, 12px gap, min-width:0 | Aligned (reconstructed) |
| NavigationRail | group heading | 32px, 12px/600 uppercase `navigation-rail.css:66-75` | 32px, 12px/600 uppercase | Aligned (reconstructed) |
| NavigationRail | active indicator | primary-tint fill + 4px left bar `navigation-rail.css:186-199` | low-opacity primary fill + 4px left indicator | Aligned (reconstructed: Control Panel rule; hover stays neutral) |
| NavigationRail | collapsed item box | 36x36, 2px corner `navigation-rail.css:157-165` | 30px medium / compact tile | Divergent (36 vs 30, unchanged) |
| NavigationRail | touch target | 44px @≤600 `navigation-rail.css:216-219` | enlarge for coarse pointer | Aligned |
| NavigationRail | collapse density | labels + heading hidden, tooltips restored, Applications popover `NavigationRail.tsx` | collapse changes density, not only width | Aligned (reconstructed) |
| ObjectHeader | padding | 12px 16px `object-header.css:9` | 10px around 30px control (~50px subheader) | Divergent |
| ObjectHeader | title | 18px/600/23 `object-header.css:34` | 16px panel / 20-28 page | Divergent (18 between) |
| ObjectHeader | kind line | 11px uppercase `object-header.css:27` | 12px metadata | Divergent (11 vs 12) |
| Shared table | row heights | not set; UI-library `HTMLTable` default (13 call sites) | header 30-34 / simple ~40 / rich 51-60 | Gap (inherited, unmeasured) |
| Shared table | column align | left default; wrap identifiers `notifications.css:38,53` | left-align default; truncate names | Aligned |
| IndexPagination | control band | 12px 16px `index-pagination.css:3,15` | 20-24 region framing / decimal | Divergent |
| Dialog | modal surface | none (`grep Dialog` empty) | 550x600, hdr/body/ftr, ~20px body | Gap (deliberate: inline panels, `products.css:16-18`) |
| Shell canvas | work area | grid `var(--console-rail) minmax(0,1fr)` `app.css:8-14` | fluid `minmax(0,1fr)` beside fixed rail | Aligned (reconstructed: the shell is the layout-vocabulary grid; the rail track is `--console-rail`, the work column `minmax(0,1fr)`) |

### Feature screens

Archetype per the section 8 map. "Layout" is the region-framing verdict; row
heights fold into the shared-table Gap above and are not repeated per screen.

| Screen | Archetype | Current geometry (file:line) | Rulebook target | Verdict |
| --- | --- | --- | --- | --- |
| fleet | browser / worklist | `--console-rail:240px` `fleet-page.css:6`; grid `var(--console-rail) minmax(0,1fr)` `:46`; controls/pagination 10px 20px `:19,68` | rail+detail beside fluid work area; 20-24 gutters | Aligned (reconstructed: facet rail re-seated as the 240px fixed rail beside the fluid table; no docked detail -- the fleet route exercises no selection-detail, so none was invented) |
| environments | browser / worklist | `--console-rail:260px` `environments.css:7`; Overview grid `var(--console-rail) minmax(0,1fr)` `:18-21`; rail collapses ≤900px `:48-50` | split summary/filter pane + dense table (2020 corroboration) | Aligned (reconstructed: the Overview's responsive summary grid is now a fixed 260px summary pane beside the dense Entities table; the 767px card reflow is kept) |
| products | browser / worklist | `--console-rail:260px` `products.css:5`; browser grid `:13-16`; pipeline/assessment are UI-library `Section`/`SectionCard` (4px `--bp-surface-border-radius`) `:32` | fixed resource summary + release/change feed + dense install table; 4px card corners | Aligned (reconstructed: fixed resource-summary pane beside the release/change feed and install table; squared `<section>` divs replaced by real UI-library Sections, so 4px corners come from the primitive) |
| plans | browser / worklist | detail 16px `plans.css:12`; timeline as `<ol>` `:29-36`; diagnostics panel a UI-library `Section`/`SectionCard` (4px `--bp-surface-border-radius`) `PlanDetailPage.tsx` | list -> detail; keep record evidence in context | Aligned (reconstructed: the squared `<details>` panel is a real UI-library Section now, so its 4px corner comes from the primitive, not a radius rule on a raw element; native disclosure kept via the Section's collapsible) |
| change-requests | review queue | page 16px `changes.css:2`; decision/reopen actions `ChangeRequestDetailPage.tsx` | review queue; one primary action per group | Aligned (reconstructed: one solid fill per group -- Approve is the single principal action; Reject keeps its danger meaning but is outlined, Cancel is minimal, Reopen outlined) |
| promotions | review queue | card 12px `promotions.css:2`; pipeline/transition panels are UI-library `Section`/`SectionCard` | transition review; constraint evidence in context | Aligned |
| exports | review queue (row->inline detail) | selectable rows `exports.css:1-7`; inline detail a UI-library `Section`/`SectionCard` (4px `--bp-surface-border-radius`) `ExportPage.tsx`; meta grid `minmax(256px,1fr)` `:18` | master/detail (never equal width); ~20px body; 4px card corners | Aligned (reconstructed: the squared `<section>` detail is a real UI-library Section now, so its 4px corner comes from the primitive, not a border/radius rule on raw HTML; 16px gap and 256px column min kept exact; inline detail is the contextual-detail-panel family per the interiors taxonomy, kept nonmodal, not converted to a docked pane) |
| imports | review queue (row->inline detail) | selectable rows `imports.css:1-7`; inline detail a UI-library `Section`/`SectionCard` (4px `--bp-surface-border-radius`) `ImportPage.tsx`; danger text `:30` | master/detail; status colour = meaning; 4px card corners | Aligned (reconstructed: the squared `<section>` detail is a real UI-library Section now, so its 4px corner comes from the primitive; inline detail kept nonmodal) |
| risk | review queue (row->inline detail) | selectable rows `risk.css:1-7`; inline detail a UI-library `Section`/`SectionCard` (4px `--bp-surface-border-radius`) `RiskPage.tsx`; meta `minmax(256px,1fr)` `:14` | master/detail; facet/severity in context; 4px card corners | Aligned (reconstructed: the squared `<section>` detail is a real UI-library Section now, so its 4px corner comes from the primitive; inline detail kept nonmodal) |
| notifications | review queue (row->inline detail) | fixed table tracks `notifications.css:7,12,17,29,34`; fieldset 4px corner `:87`; preference grid `:58-62` | shared column tracks; truncate names / keep coordinates | Aligned (reconstructed: preference `__meta`/`__types` grids on the decimal px ladder; fieldset grouping takes the 4px surface corner; the aligned table tracks are left in rem) |
| teams | browser / worklist | coordinate 12px mono muted `teams.css:2-4`; meta grid `minmax(192px,1fr)` `:25-29` | list -> detail; muted identifiers | Aligned (reconstructed: coordinate on the 12px metadata type-scale; spacing on the decimal px ladder -- 24px section band, 8px cluster gap, 16px meta gap) |
| overview | dashboard | page 16px `overview.css:1`; grid `auto-fill minmax(280px,1fr)` `:2-7`; clear=success `:14-16` | dashboard panel grid; affirmative state colour | Aligned |

### The through-line for the reconstruction

Three divergences recur and are the substance of the sequenced work: (1) the
console uses a **48px icon rail + full-width detail routes** where the rulebook
and the 2020 composition evidence show a **fixed category rail + docked detail
pane**; (2) the review-queue screens carry a **rem spacing ladder** parallel to
the px decimal ladder and should be reconciled onto the latter; (3) panels are
**squared (0 corner) flat regions** where the rulebook's normal card is 4px.
None of these is a defect to hot-fix -- each is a per-region PR in the backlog,
and this table is its spec.

Divergences (2) and (3) are now closed across the worklist and review-queue
screens: the rem spacing ladder on exports, imports, risk, notifications and
teams is reconciled onto the decimal px ladder, and the squared detail panels
now take the 4px surface corner by becoming real UI-library `Section`s rather than
by a radius rule on a raw element -- the plans diagnostics `<details>`, the
exports/imports/risk inline detail panels, with the notifications type fieldset
taking the same 4px surface corner. The interiors taxonomy also resolves the
tentative "inline detail vs docked pane" note against exports/imports/risk: the
inline row->detail is the contextual-detail-panel family (nonmodal, preserves
the list context), a legitimate interior, so the primitive was swapped to a real
UI-library `Section` while the family was kept nonmodal, not converted to a docked
pane.

Divergence (1) is now closed for the rail itself: the NavigationRail is the
two-state Foundry platform sidebar -- a 230px expanded labeled rail (rows, an
"Applications" group heading, a pinned collapse toggle, a scrolling middle) that
collapses to the 48px icon rail (labels and heading gone, per-item tooltips and
the Applications popover restored), with the shell recomposed as the
`var(--console-rail) minmax(0,1fr)` layout-vocabulary grid. The active route row
takes the low-opacity primary fill and the 4px left indicator (the Control Panel
rule); hover stays a neutral fill. What remains of (1) is the **docked detail
pane**: the console still routes to full-width detail screens rather than a
~300px docked pane beside a preserved list, which the per-screen rows above
record as reconstructed to fixed-rail + fluid-work without a docked detail,
since the routes exercise no selection-detail to dock.

## Console asset budget

Issue #827 adds a build-time gzip budget for all emitted JavaScript and CSS,
including lazy chunks: 2,359,296 bytes total and 1,310,720 bytes per asset.
The Linux Node 26.13.1 build at `22fb2f7` measured about 2.14 MB total and
1.23 MB for its largest script with gzip defaults. The gate uses level 9 with
a fixed timestamp and reports exact byte totals; these are compressed transfer
sizes, not heap, parse time, fonts, or a production performance claim.

`pnpm build` enforces the budget for normal and alternate output directories;
`scripts/build-console.sh` also checks cached output before accepting its stamp.
Missing output, a missing JS/CSS class, symbolic links, an oversized chunk or
an oversized aggregate fail closed. Chunk splitting cannot bypass the total.
Raising a limit requires a reviewed source change with a fresh measurement;
there is no environment override.

## Keyboard release journey

The top-bar search is a button: Tab focuses it without opening a dialog;
Enter, Space, clicking, or the existing Ctrl/Cmd+K shortcut opens the palette.
Moving focus alone must not change context or intercept navigation.

`delivery-journey.spec.ts` walks proposal and independent approval using only
Tab and keyboard activation/input. It checks both request previews, no write
before confirmation, the author's disabled approval, and exact reviewer payload.
The Hub answers are fixtures; the reviewer is a fresh fixture session after
reload. This is keyboard interaction coverage, not a real-issuer or full
accessibility audit, and does not replace live-Hub journey tests.
