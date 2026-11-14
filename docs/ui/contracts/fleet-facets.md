# Fleet facet rail contract

The console's `/console/fleet` screen carries a faceted filter rail beside the
installation table: one headed group per filterable dimension, each value
selectable, each with its count and a bar proportional to the largest count in
its group. Selecting a value writes the dimension's query parameter into the URL
and the Hub filters the fleet; deselecting removes it.

The composition — a filter pane with headed counts and proportional bars beside
a dense table — is a **tenant observation**, from the 2020 Apollo screenshot at
`docs/ui/evidence/legacy-apollo-2020/environment-events.webp`.
`docs/ui/HISTORICAL-APOLLO-EVIDENCE-2020-2022.md` bounds what that image
supports: composition, and nothing about navigation, labels, routes, palette or
table semantics. Nothing here is a claim about Apollo's wire format.

The response shape is a **Conduit extension**. The pinned Apollo OpenAPI under
`docs/reference-platform/snapshots/` publishes no fleet-installation filter-count operation,
so `facets` is registered as `reporting.FleetFacets` in
`docs/evidence/conduit-inventions.json` and marked at
`internal/reporting/domain/fleet.go`.

## What the console reads

| Route | Used for |
| --- | --- |
| `GET /v1/fleet/installations?…` | the table, the headline, and the rail |

One request serves all three. The rail is a projection of a field already in the
response, not a second read.

## The counts, and what each one is over

The response carries two count structures and they answer different questions.
Confusing them is the failure this contract exists to prevent.

**`summary`** describes the result. `summary.total` is how many installations
match the whole query, and `summary.health` and its siblings break that result
down. Every number in it moves when any filter moves. It feeds the headline
above the table and nothing else.

**`facets`** describes what each filter would give. Each dimension is counted
with **its own filter excluded** from the query, and with every other filter
applied. `internal/reporting/domain/fleet.go` computes this in one walk: an
installation matching the whole query counts toward every facet; one matching
everything except exactly one selected dimension counts toward that dimension's
facet only; one missing two or more counts nowhere.

The consequence, stated plainly because it is the point:

- With `health=UNHEALTHY` applied, `summary.health` is `{UNHEALTHY: n}` and
  `facets.health` still reports `HEALTHY`, `UNKNOWN` and the rest.
- With `health=UNHEALTHY` applied, `facets.environment` **does** narrow, because
  environment is not the excluded dimension. A rail is read one dimension at a
  time: "how many rows would I get if I picked this **instead**".
- `search` is not a dimension and has no facet. An installation the search text
  excludes counts toward nothing, because there is no rail control that would
  bring it back.

The alternative — serving `summary` as the rail and labelling the counts "within
the current selection" — was considered and rejected. It needs no Hub change,
but it produces a rail on which every unselected sibling reads zero the moment
anything is selected, so there is nothing to navigate to and the one number an
operator most wants ("how much of the fleet is actually healthy") reads as none.
The register entry records this.

## Properties this screen is required to preserve

**A facet key is the query parameter it selects on.** The Hub serves each facet
under the name of the parameter that filters it — `rolloutStatus`, not
`rollout` — and the console's `DIMENSIONS` table in
`web/src/features/fleet/facets.ts` uses one string as the facet key, the
`FleetQuery` field and the URL parameter. Three names for one dimension is how a
rail comes to count one thing and filter another, and nothing else in the system
would notice.

**A facet count is a disclosure and is authorized like a row.** The route filters
rows by a per-Environment read check, and the near-miss contributions that feed
the facets pass the same check before they are counted. Without that, an operator
with no grant on an Environment would learn how many installations it holds, and
in what health, from the number beside a filter.

**A selection lives in the URL, not in the rail.** The rail owns no state but
which groups are expanded. A facet click calls the same setter the search box
uses, which deletes the key rather than setting it to undefined — an absent
filter is no parameter at all — and clears the pagination cursor, because a
cursor is only valid for the query it was minted against.

**A selected value stays visible even at zero.** Because a facet excludes its own
dimension, a selection matching nothing is a real state: the Hub sends no count
for it. `orderFacetValues` puts it in the list anyway, marked selected, because
otherwise the only control that could lift the filter is the one the filter just
removed.

**A bounded list says what it withheld.** Environment and Product have open
vocabularies, so a dimension can carry more values than a rail should show. The
list is bounded at `DEFAULT_FACET_LIMIT` and the remainder sits, collapsed but
returned rather than discarded, behind a control labelled with its own count —
"4 more", not "more". The same discipline the console applies to a bounded page:
a truncation the reader cannot see is a truncation that will be read as the end
of the list. A label too long for the column ellipsises and carries its full
value as a `title`, for the same reason. A selected value below the bound is kept
visible, which costs at most one extra row because a dimension carries at most
one selection.

**A bar is a proportion of its group's largest count.** `facetBarValue` returns
the 0-1 fraction Blueprint's `ProgressBar` takes. Not of the group's total,
and not of the fleet: the bars compare the values of one dimension against each
other, which is the comparison the rail is for. Against a total, a dimension with
many values renders every bar as an identical sliver and distinguishes nothing.
The count is printed beside the bar in either case — the number is the primary
signal and the bar the secondary one, the way `statusTags` treats colour — and
the bar carries `aria-hidden`, since assistive technology already has the number.

**Empty, loading and failed are three different screens.** A fleet with nothing in
it renders "No installations to summarise", not a rail of zeroes. A failed request
renders the page's failure state with the Hub's own message and a retry, and the
rail is not rendered at all — a rail showing no facets would be read as a fleet
with nothing in it, which is the same class of defect
`docs/operations/PRODUCTION-READINESS.md` records for a failed request reading as
a healthy platform. A 401 renders as "Not signed in".

**The status vocabularies are the Hub's.** `HEALTH`, `ROLLOUT`, `CONFIG` and
`LIFECYCLE` in `web/src/api/types.ts` break ties between equal counts, and are
held to the Go consts in `internal/environments/domain/reporting.go` in both
directions by `scripts/verify-console-contract.mjs`. They were unchecked before
this screen: an invented value could never appear in a facet, and a real one
omitted would sort behind every declared value for no reason. Environment and
Product have no vocabulary because they have no closed one — they are whatever
the deployment installs, which is what the bound above exists for.

## How it is built

Composed from Blueprint, not drawn. The console imports from `@blueprintjs/core`
at every one of its import sites, and the rail adds no new dependency and no new
visual language:

| Part | Blueprint component |
| --- | --- |
| A dimension | `Section` + `SectionCard` — the panel this console already uses eleven times |
| A value row | `Button`, `variant="minimal"`, `fill`, `alignText="start"` |
| The selected state | Blueprint's own `active`, plus `aria-pressed` for the toggle semantics |
| The count | `Tag`, `minimal round` |
| The bar | `ProgressBar` with an explicit `value`, `stripes={false}` and `animate={false}` — a static filled track |
| The bounded tail | `Collapse`, revealed by a `Button` |

`OverflowList` was the other candidate for the tail and is the wrong tool: it
collapses on available width, and this bound is a count of values, not a number
of pixels.

Four rules of bespoke CSS remain, and each is a composition Blueprint does not
offer rather than a restatement of one it does:

- the rail's own place on the page (a flex column, its divider, and where that
  divider moves when the page stacks below 900px);
- `p { margin: 0 }` for the caption, because `normalize.css` gives a paragraph a
  block margin and the rail's `gap` already spaces it;
- three rules turning `.bp6-button-text` into a row, because Blueprint gathers a
  Button's children into one block and a full-width `ProgressBar` would
  otherwise drop onto its own line;
- putting the H6 size back on the H2 (see below).

`Section` renders its title as an `H6`, which under the page's `H1` would skip
five heading levels. The rail substitutes an `H2` through `titleRenderer`, which
is the same substitution `OverviewPage` makes, and restores the size Blueprint
gives `h6.bp6-heading` — the heading *level* is the document's, the heading
*style* is Blueprint's.

## Where the code lives

`FacetRail` and its arithmetic are in `web/src/components/`, not in
`features/fleet`. The rail knows no vocabulary and no endpoint: it is given
groups and reports clicks. A second index screen with server-side filters — the
Environment or Product index — could take it as it stands, and if it lived in
`features/fleet` that second consumer would have to reach across features, which
rule 5 of `scripts/verify-console-boundaries.mjs` forbids and would be right to.
Everything fleet-specific — which dimensions exist, what they are called, which
parameter each writes — is in `web/src/features/fleet/facets.ts`.

## Deliberate omissions

- **No stale facet.** Staleness is decided by `maxAgeSeconds`, not selected on,
  so there is no filter for a count to be a count of. It stays in the headline
  and the callout.
- **No sort control on a group.** The 2020 capture shows one per heading. What it
  would sort between is not visible in the image, and a control whose second
  order nobody can state is a guess with an affordance.
- **No multi-select within a dimension.** `FleetQuery` carries one value per
  dimension and the Hub matches on equality. Offering two would send a parameter
  the Hub reads as one value and quietly return the wrong result.
- **No version-distribution bar.** The 2020 Product view stacks one above its
  installation table. The fleet response carries `desiredVersion` and
  `observedVersion` per row but no version counts, and the page is bounded, so a
  bar computed from the rows would describe a page and claim to describe a
  Product. It needs a facet of its own before it can be honest.

Source anchors: `internal/reporting/domain/fleet.go`,
`internal/controlplane/http.go` (the `GET /v1/fleet/installations` handler),
`web/src/api/types.ts`, `web/src/components/facets.ts`,
`web/src/components/FacetRail.tsx`, `web/src/features/fleet/facets.ts`,
`web/src/features/fleet/FleetFacetRail.tsx`, and
`web/src/test/e2e/fleet-facets.spec.ts`.
