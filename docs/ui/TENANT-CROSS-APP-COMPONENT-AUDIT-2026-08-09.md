# Tenant cross-application component audit

Status: private V0 validation evidence, 2026-08-09.

At the repository owner's direction, an authorized operator session inspected
read-only states in seven Foundry applications plus Ontology Manager and one
Developer Console application. The tenant origin, application identifier, raw
DOM, responses, and screenshots are intentionally not committed. Temporary
captures lived under `/tmp` and may disappear on Codespace restart. This note is
the durable, sanitized conclusion; it is not redistributable tenant evidence and
does not establish private Apollo compatibility.

## Observed states

The cross-app sample covered Control Panel, Ontology Manager, Compass, Data
Connection, Pipeline Builder, Workshop, and Contour. Ontology Manager added
Discover, Proposals, History, Object Types, Properties, Functions, and Health
Issues. Developer Console added Overview, Ontology SDK, Platform SDK, MCP,
Hosting, OAuth, Permissions, and Telemetry. Each route used a fresh browser
context after one earlier long-lived renderer crashed; no create, edit,
configure, save, or cleanup action was invoked.

Across the sample, recurring composition included a 230px dark global sidebar,
an 18px classification banner, an approximately 50px application header,
Source Sans Pro at 14px body size, pale canvases, bordered white panels, dense
tables or lists, compact search/filter toolbars, tags/counts, cards, and green
primary creation actions. Ontology and Developer Console both use a second
resource navigation column. Developer Console consistently groups routes by
SDK, Development, Deployment, Permissions, and Analytics; detail states combine
resource headers, status tags, tabs, bordered panels, banners, tables, action
menus, and bounded empty states. First-load dialogs and feature callouts are
real states that can obscure baseline captures and therefore require explicit
audit coverage.

Ontology Proposals uses role-like inbox tabs (assigned, in review, merged, and
closed) with search and sort. Dense object/property/function indexes combine
counts, status or visibility tags, filters, selectable rows, and column
configuration. Health Issues did not call an unloaded source globally clear: it
said no loaded issues were found while warning that more items might exist on
the server. Developer Console Telemetry pairs Overview/Analytics tabs with
bounded time ranges and a truthful no-data action.

## Decisions and boundaries

- Retain semantic `h1`/`h2`, landmarks, native tables, mobile definition lists,
  keyboard behavior, and axe gates even where tenant DOM uses weak heading or
  grid semantics. Visual resemblance does not justify copying accessibility
  defects.
- Retain dense, searchable, server-paged indexes and explicit partial/unknown
  states. An unloaded source is never evidence that the whole system is clear.
- Use the proposal inbox composition as evidence for the Change Request index,
  but do not imply unsupported assignments, counts, or transitions.
- Treat secondary navigation, count badges, column configuration, and the wide
  global sidebar as measured hypotheses. Public Apollo evidence still supports
  a narrow rail, so V0 Foundry validation alone does not displace it.
- A screenshot or class name establishes rendered behavior, not ownership by a
  particular UI-library component. Component choices require the exact pinned
  public source or runtime evidence.
- Tenant-specific routes and identifiers remain test inputs supplied by the
  operator, never product defaults or fixtures.
