# Control-plane console — nitpick pass 2

Fresh captures in `platform/web/screenshots/*.png` (re-taken after pass-1 fixes).
Ground truth: `platform/docs/ui/CONTROL-PLANE-UI-PLAN.md`,
`platform/docs/ui/upstream-screen-evidence.tsv` (alt only), Blueprint 6.18.0.

**Out of scope (already fixed — do not re-litigate):** Product-scoped Release
Channel graph with selectable transitions + constraint panel; Change Request
Consequence panel; Environment Overview health/generation lead-in; rail IA
(Home / Environments / Software Catalog; Fleet & Change Requests under
Applications); ObjectHeader on Plan / Change Request; relative dates with exact
UTC on hover.

This pass is the **next layer**: rhythm, hierarchy, density, empty states,
intent colour, masthead competition, and anything that still reads as a
developer placeholder. Ranked by how much each finding degrades an operator’s
first read of the screen.

---

## Findings (worst first)

### 1. Index pages have no page title — “Platform” is the only heading

**Screenshots:** `03-environments.png`, `05-plans.png`, `07-change-requests.png`,
`09-products.png` (also `02-fleet.png`)

**Element:** Content column above the table — absent. Product masthead
(“Platform” / “Control plane”) is the sole heading on the viewport.

**Exact change:** Give every index route a content-column title
(`Environments`, `Plans`, `Change Requests`, `Software Catalog`, `Fleet`) at
resource-title weight, then the table. Keep the product mark smaller than that
title.

**Label:** **PLAN** — wireframes put the operating title in the content column
beside the rail; recurring composition is resource/page header, not a permanent
product masthead as the primary read. **TASTE** on exact H1 vs `ObjectHeader`
for indexes.

---

### 2. Permanent “Platform / Control plane” still outranks the resource

**Screenshots:** all, worst on `04-environment-detail.png`,
`06-plan-detail.png`, `08-change-request-detail.png`, `10-product-detail.png`

**Element:** Top `AppHeader` identity bar (gauge + **Platform** 14px/600 +
**Control plane** 12px) spanning the full width above every route.

**Exact change:** Shrink or demote the product mark (icon-only or single-line
muted identity). On resource routes, the first large type must be the
ObjectHeader title; on indexes, the page title from finding 1. Today an
approver’s eye hits “Platform” before `crq_upgrade · Upgrade payments…`.

**Label:** **PLAN** — recurring composition: resource header with icon, primary
name, kind, tags, Actions — not a competing product H1 on every screen.

---

### 3. Environment and Product detail still skip ObjectHeader

**Screenshots:** `04-environment-detail.png`, `10-product-detail.png`

**Element:** `04` — bare **prod-use1** + `DIRECT` / generation tags.
`10` — bare **app.example:payments** with no icon, kind, or status tags.

**Exact change:** Same ObjectHeader treatment already used on Plan / Change
Request: icon + kind (`ENVIRONMENT` / `PRODUCT`) + primary name + status tags.
Generation / connection stay as tags, not a substitute for kind.

**Label:** **PLAN** — recurring resource header; IA treats Environment and
Product as first-class resources. Pass-1 “resource pages use ObjectHeader” is
incomplete on these two captures.

---

### 4. ObjectHeader has no Actions menu

**Screenshots:** `06-plan-detail.png`, `08-change-request-detail.png`
(and would apply once `04` / `10` adopt ObjectHeader)

**Element:** Right side of the resource header — status tag only; no
**Actions ▾**.

**Exact change:** Add the Actions popover (copy id, open related Plan / CR,
cancel when allowed, etc.), gated by session capability. Wireframe 2 shows
`[Actions ▾]` on the Environment header.

**Label:** **PLAN** — recurring composition ends with “an **Actions** menu at
the right.”

---

### 5. Overview empty cells say “Nothing.”

**Screenshot:** `01-overview.png`

**Element:** **Running Plans** card body — plain gray text `Nothing.`

**Exact change:** Hide zero-count attention groups, or use a designed empty
component (`NonIdealState` / muted “No running Plans”) — never the literal
string `Nothing.` Plan: empty states are designed components, not ad hoc text.

**Label:** **PLAN** (empty-state rule). Residual wording is **TASTE**.

---

### 6. Index tables stretch columns across the full canvas

**Screenshots:** `03-environments.png`, `05-plans.png`, `07-change-requests.png`,
`09-products.png`, `10-product-detail.png`

**Element:** Table layout — e.g. `03` leaves a huge gap between Connection and
Entities reported; `09` parks Releases / Latest at the far right; `05` / `07`
fling Status / Created / Outstanding to the edges.

**Exact change:** Cap table max-width or set content-sized columns with a
trailing flexible spacer; keep identifier + status within one eye-span (~600–
800px). Do not `width: 100%` equal-flex sparse four-column tables.

**Label:** **PLAN** — “Retain the upstream information density… fluid-width
tables” means dense readable columns, not stretching four cells edge-to-edge.
**EVIDENCE** silent on exact column widths → degree of tightening is **TASTE**.

---

### 7. Index tables sit flush on the pale canvas

**Screenshots:** `03-environments.png`, `05-plans.png`, `07-change-requests.png`,
`09-products.png`, `02-fleet.png`

**Element:** Table chrome — header row on `#f6f7f9` canvas, no white
thin-bordered panel; large dead field below the single row.

**Exact change:** Wrap each index table in a white, thin-bordered panel
(Blueprint `Section` / panel token). Add a compact footer row
(`1 Environment`, pagination, or explicit end-of-list) so the empty lower
canvas does not read as an unfinished page.

**Label:** **PLAN** — “pale page canvas with white, thin-bordered panels.”

---

### 8. Rail active state lies about where you are

**Screenshots:** `03-environments.png` (cube / Software Catalog highlighted on
Environments), `05-plans.png` / `07-change-requests.png` (Home active while on
Applications destinations), `02-fleet.png` (cube active for Fleet)

**Element:** NavigationRail selection highlight.

**Exact change:** Highlight Environments (cloud) on `/environments*`; highlight
the Applications control when the route is under Applications (Fleet, Change
Requests, Plans if nested there); highlight Software Catalog (cube) only on
catalog routes. Never leave Home selected on a non-Home route.

**Label:** **TASTE** as product polish; navigation truthfulness is operator
orientation, not upstream-alt-proven. No PLAN sentence names the highlight
rule, but wrong selection actively misleads.

---

### 9. Change Request actions put Approve before Reject

**Screenshot:** `08-change-request-detail.png`

**Element:** Footer buttons — green **Approve**, then **Reject**, then Cancel /
Reopen.

**Exact change:** Reorder to **Reject** then **Approve Change Request** (primary
affirming action last). Match wireframe 3: `[Reject]  [Approve Change Request]`.

**Label:** **PLAN** (wireframe 3 button order). Do not label this BLUEPRINT —
the package does not prescribe Approve/Reject order.

---

### 10. Solid round intent Tags shout on every status cell

**Screenshots:** `02-fleet.png` (HEALTHY / UNHEALTHY / OUT OF DATE / DRIFTED /
INSTALLED), `05-plans.png` (FAILED), `07-change-requests.png`
(PENDING APPROVALS), `04-environment-detail.png` (HEALTHY / INSTALLED)

**Element:** Status column pills — solid fill + `round` on almost every value.

**Exact change:** `minimal` (or `none` intent) for calm states (HEALTHY,
INSTALLED, UP TO DATE, APPLIED); reserve solid intent for UNHEALTHY / FAILED /
PENDING APPROVALS / BLOCKED. Drop `round` unless the control is a count badge.

**Label:** **PLAN** — modest radii; status paired with text/icon/shape.
**BLUEPRINT** — `minimal` / `round` are opt-in. Roundness degree residual
**TASTE**.

---

### 11. Fleet STALE filter chip is a solid orange slab

**Screenshot:** `02-fleet.png`

**Element:** Summary filter row — **STALE 1** selected as solid warning fill;
unselected chips are light gray. Below it, a second orange Callout repeats the
stale message.

**Exact change:** Selected filter = outlined / minimal warning, not a filled
banner-weight chip. Keep one stale explanation (Callout **or** selected chip
copy), not both at full warning chroma.

**Label:** **TASTE** on chip chrome; **PLAN** intent should stay calm unless
the row itself is the alarm. Double-warning is the operator-read cost.

---

### 12. Overview attention titles and badges disagree on intent

**Screenshot:** `01-overview.png`

**Element:** **Blocked promotions** — blue section title, orange count badge;
**Running Plans** — blue title, gray `0` badge. Failed / Unhealthy correctly
pair red title + red badge; Stale pairs orange + orange.

**Exact change:** Drive title colour from the same intent as the count Tag
(warning for blocked/stale, danger for failed/unhealthy, none/muted when
count is 0). Or drop coloured titles entirely and keep intent only on the
badge.

**Label:** **TASTE** (intent consistency). Plan requires semantic intents but
does not specify section-title colouring.

---

### 13. Promotions “Select a transition…” is coach-mark copy on every card

**Screenshot:** `11-promotions.png`

**Element:** Under each product graph — muted paragraph
`Select a transition to see what governs it.` Constraint panel absent until
selection.

**Exact change:** Remove per-card instructional prose. Show a persistent empty
detail region (“No transition selected”) once, or auto-select the first
blocking transition so the constraint panel is never a blank. Keep the LOCAL
scope Callout if needed; it already explains the fleet-wide extension.

**Label:** **PLAN** — selecting a transition opens the evidenced details panel
(empty state should be a designed region). Instructional sentence is **TASTE**
/ placeholder tone.

---

### 14. Consequence panel apologises instead of stating downtime

**Screenshot:** `08-change-request-detail.png`

**Element:** Consequence Callout —
`… The Hub publishes no downtime estimate, so none is shown.`

**Exact change:** When Hub omits downtime, render a calm structured field
`Downtime: not published` (or omit the row). Delete the meta sentence about
what the Hub publishes — it reads like a stub comment, not product UI.
Wireframe 3 shows `Downtime: none`.

**Label:** **PLAN** (wireframe Consequence shape). Apology wording is **TASTE**.

---

### 15. Plan failure is triple-red

**Screenshot:** `06-plan-detail.png`

**Element:** Header **FAILED** solid tag + Callout **This Plan failed** +
nested timeline Callout (`0/3 nodes…`) + **FAILED** on APPLY.

**Exact change:** Keep one page-level failure Callout with attributed Entity;
keep per-task status Tags. Drop the redundant “This Plan failed” title if the
header tag already says FAILED, or make the Callout the sole alarm and use a
`minimal` header tag. Collapse duplicate chrome so diagnostics (nodes /
Plan output) win the eye.

**Label:** **TASTE** on how many red surfaces; **EVIDENCE** `052` / `053` want
failed-resource detail and timeline+output, not louder banners.
**BLUEPRINT** Callout is for the primary alert, not every nested line.

---

### 16. Diff digests truncate to unusable stubs

**Screenshot:** `08-change-request-detail.png`

**Element:** What changes → `releaseDigest` Before/After —
`sha256:9f1c` / `sha256:be27` (same pattern on `10-product-detail.png`
chart digests at ~10–12 hex chars).

**Exact change:** Show a monospace coordinate long enough to discriminate
(e.g. `sha256:be27f1a0c3…` with full digest in `title` / copy action). Do not
truncate to four hex characters in the primary cell — operators cannot confirm
sameness.

**Label:** **PLAN** — type family for digests/coordinates; exact evidence.
Truncation length residual **TASTE**.

---

### 17. Environment Overview repeats generation in two competing places

**Screenshot:** `04-environment-detail.png`

**Element:** Header tag `generation 0123456789abcdef` and Generation card body
`desired 0123456789abcdef · last report…`

**Exact change:** Header keeps connection + health/freshness tags only;
Generation card owns the desired/reported coordinate and last-report relative
time. One generation string on screen at title weight.

**Label:** **TASTE**. **PLAN** wireframe 2 puts generation in the health/
generation summary, not duplicated as a title chip.

---

### 18. Fleet filter placeholder truncates mid-word

**Screenshot:** `02-fleet.png`

**Element:** Search `InputGroup` placeholder —
`Filter by Entity, Environm…`

**Exact change:** Shorten the placeholder to fit the control
(`Filter installations…`) or widen the input so “Environment” does not clip.
Truncated chrome reads unfinished.

**Label:** **TASTE**.

---

### 19. Snake_case fixture IDs read as developer placeholders in the hero

**Screenshots:** `06-plan-detail.png` (`pln_deploy`), `08-change-request-detail.png`
(`crq_upgrade · …`), `05-plans.png` / `07-change-requests.png` index links

**Element:** Primary title / Request column using `pln_*` / `crq_*` as the
loudest string.

**Exact change:** Prefer human title first where one exists
(`Upgrade payments to 2.4.1` with `crq_upgrade` as muted coordinate tag);
for Plans, show a derived action summary as title and keep `pln_deploy` as
secondary coordinate. Wireframe 3 leads with `CR-4821 · Upgrade…` — coordinate
plus title, not snake_case alone as the brand of the page.

**Label:** **PLAN** on CR title shape; Plan title treatment **TASTE** when Hub
only stores an id.

---

### 20. “not assessed” vs PASS is typographically orphaned

**Screenshot:** `10-product-detail.png`

**Element:** Vulnerability assessment column — green solid **PASS** pill vs
light gray plain text `not assessed` (no Tag).

**Exact change:** Render unknown assessment as a `minimal` `none` Tag
(`NOT ASSESSED`) at the same metrics as PASS, or both as text + icon. Do not
mix pill and naked muted copy in one column — the eye reads “missing UI” on
row 2.

**Label:** **TASTE**. Consistent status chrome supports **PLAN** “every status
is still paired with text/icon/shape.”

---

## Summary counts

| Label | Count |
| --- | ---: |
| PLAN (primary) | 12 |
| TASTE (primary or residual) | 11 |
| BLUEPRINT | 2 |
| EVIDENCE | 1 (supports #15) |

Ten highest-leverage fixes if capacity is limited: **1, 2, 3, 5, 6, 8, 9, 10,
13, 16**.

## Notes

- Upstream image bodies remain uncommitted; EVIDENCE claims use TSV `alt` only.
- Pass-1 structural wins are visible in `08`, `11`, `04` (health/generation),
  and the rail icon set — this list deliberately starts where those leave off.
- `01-overview.png` still uses a six-card attention grid rather than MetricStrip
  + Attention / Rollout posture; that remains a PLAN composition gap, but it is
  not re-argued here beyond empty-state and intent-colour nits (#5, #12).
