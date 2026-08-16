# Apollo UI alignment audit

Status: bounded evidence record, 2026-08-16.

How far the Conduit console's composition tracks the recovered Apollo visual
evidence, what is deliberately unaligned, and where the gap is a real one. Read
with `HISTORICAL-APOLLO-EVIDENCE-2020-2022.md`, which bounds what the 2020
images may be used for, and with `docs/operations/TENANT-FIDELITY.md`.

## What the evidence is

Two bodies, different in kind and in force.

| Evidence | Form | Era |
| --- | --- | --- |
| `evidence/legacy-apollo-2020/{product-rollout,environment-events}.webp` | Two committed screenshots, viewed directly for this audit | 2020 |
| `upstream-screen-evidence.tsv` | 29 captures recorded as URL + SHA-256 + alt text; the images are not committed | Current docs |

The 2020 pair is the weaker source and the one most likely to be over-read.
`HISTORICAL-APOLLO-EVIDENCE-2020-2022.md` already states the bound, and this
audit does not reopen it:

> They do **not** justify copying the obsolete navigation, labels, private
> routes, dark-only palette, or visual table semantics.

The article's own editor note says the suite has since changed. So "align with
the images" cannot mean visual alignment with those two; what they legitimately
supply is **composition**. The 29 current-era captures are the stronger target
and are audited separately below.

## Part 1 — composition against the 2020 screenshots

Each row is a pattern visible in one or both images, against what the console
does today.

| Pattern | State | Where |
| --- | --- | --- |
| Compact status pills as the headline | Aligned | `SummaryBar.tsx` — total, per-health buckets, and an always-present STALE count |
| Dense table with per-column status glyphs | Aligned | `FleetPage` — Environment, Entity, Product, Desired, Observed, Health, Rollout, Config, Lifecycle |
| Resource-scoped tabs | Aligned | `EnvironmentDetailPage` — Overview, Plans, Commands |
| Server bounds on every large index | Aligned | `IndexPagination`, `useIndexQuery`, `contracts/pagination.md` |
| Selectable transition carrying promotion constraints | Partial | `ReleaseChannelGraph` renders selectable transitions with status and a blocker count, but from `/v1/promotions`; the dedicated `promotion-stages` endpoint is unread |
| Fixed summary pane beside a tabbed detail region | Absent | The console uses a horizontal `ObjectHeader`; no persistent side summary |
| Selected row opens an in-context detail region | Absent | The console navigates to a separate route. Import, Export and Risk reveal detail from the already-loaded row, which is the nearest equivalent |
| Faceted filters with counts and proportional bars | Absent | No `facet` symbol anywhere under `web/src/features`. Fleet has search and selects, but no counts |
| Event timeline paired with the event table | Absent | Only `PlanDetailPage` carries any timeline; there is no event timeline |
| Stacked version-distribution bar with a percentage | Absent | `SummaryBar` is a tag strip, not proportional |

Four of the ten absences are worth different treatment. The **dark-only palette,
the 2020 navigation, and its table semantics** are out of bounds by the rule
above and should stay unaligned. The **event timeline** has no endpoint to feed
it, so building one would mean inventing the data. **Faceted counts** are the one
2020 pattern that is genuinely good, genuinely absent, and blocked only by the
Hub not returning counts.

## Part 2 — the sharper finding: served but unsurfaced

The Hub registers **45 `GET /v1/*` routes**. The console reads **18**. The
remaining 27 are not a styling question — they are coordinates the Hub can
already answer and the UI does not ask for. Several are exactly the screens the
capture evidence documents.

| Unread endpoint | Upstream capture |
| --- | --- |
| `/v1/modules`, `/v1/modules/{name}/releases`, `/v1/modules/{module}/auto-update-policy`, `/v1/module-installations/{id}/update` | **266** Contents, **268** Installations, **270** Overview — three Module screens; the console has no Modules route at all |
| `/v1/environments/{id}/suppressions` | Suppressions is a tab in the 2020 Environment view |
| `/v1/products/{product}/releases/{version}` | **306** — the Release Overview tab; no Release detail route exists |
| `/v1/products/{product}/promotion-stages/{from}/{to}` | **314** — per-transition promotion constraints |
| `/v1/resource-grants`, `/v1/authorization-audit` | **353** Edit team roles, **356** Team settings |
| `/v1/labels`, `/v1/labels/{id}`, `/v1/labels/{id}/revisions` | **338** — label requirements in the manual-promotion sidebar |
| `/v1/approval-policies` | The governance surface behind Change Requests |
| `/v1/environments/{id}/` `profile`, `profile/revisions`, `health-targets`, `reported-state`, `target-hub-versions`, `module-installations` | **194** — metadata updates in the Environment Overview |
| `/v1/promotions/{id}`, `/v1/products/{product}/publications`, `/v1/export-pipelines/{id}/schedule-runs`, `/v1/bundles/{id}` | — |

This matters more than the 2020 styling, and it is the same discipline the
Apollo GraphQL interface already applies: MAPPED-or-REFUSED. Where the Hub holds
no data, an absent screen is the honest outcome. Here the data exists and the
console simply does not read it.

### Correctly absent

Two captures have **no Hub backing**, so their absence is the rule working
rather than a gap:

- **367** — the Vulnerabilities tab of the Risk Management application. The
  console's `RiskPage` shows *recalls*; `SECURITY` appears there only as a
  recall **source**, not as a vulnerability inventory. No endpoint serves one.
- **368** — current and target Release risk scores as Plan metadata. Neither
  `risk` nor `score` appears in `PlanDetailPage`, and nothing serves them.

Building either would mean inventing the data behind a screen that looks
authoritative, which is the failure the provenance model exists to prevent.

## Part 3 — a defect found while auditing

`web/src/features/environments/EnvironmentDetailPage.tsx` claims its tab set is
evidence-backed:

> The tab set is the upstream one, evidenced by captures 070 (Overview), 069
> (Changes) and 066 (Commands) in docs/ui/upstream-screen-evidence.tsv.

**None of 066, 069 or 070 exist in that file.** The captures that carry those
descriptions are **206** ("The Environment Overview tab is displayed"), **200**
("Changes Tab") and **157** ("The Commands tab displays all commands that were
executed for this Entity"). The same file cites 206 correctly seventy lines
later, so the stale identifiers are a leftover from an earlier numbering rather
than a different claim. Corrected in the same change as this audit.

The comment is also right about something the console does not do. Upstream's
Environment tab set is Overview / **Changes** / Commands. The console ships
Overview / **Plans** / Commands. Plans is independently evidenced (capture 058,
an Environment-level Plans view), so it belongs — but **Changes is absent**,
while `/v1/change-requests` exists and is already read for the global index. The
gap is scoping those to an Environment, not new data.

## Ranked recommendations

1. **Modules.** Three upstream screens, four live endpoints, no UI. The largest
   single gap and the one least dependent on judgement.
2. **The Environment Changes tab**, plus the citation fix. Both small; the data
   is already read elsewhere.
3. **A Release detail route** (`/products/:product/releases/:version`) for
   capture 306, whose endpoint is live and unread.
4. **Faceted filter counts.** Needs `/v1/fleet/installations` to return counts
   before the UI can show them, so it is a contract change first.
5. **Leave alone:** the dark-only palette, the 2020 navigation and labels, and
   an event timeline. The first two are out of bounds by the rule this document
   opens with; the third has no data behind it.

## Method

Both `.webp` files were viewed directly rather than read through the existing
prose summary. Console composition was read from `web/src/features/**` and
`web/src/components/**`. The route surface was extracted from
`internal/controlplane/http.go` and diffed against the endpoints reached from
`web/src/api/*.ts`. Every "absent" above was checked against the Hub before
being called a gap; the two that are correctly absent are listed as such.
