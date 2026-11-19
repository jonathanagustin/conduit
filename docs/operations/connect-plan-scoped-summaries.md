# Scoped native Plan summaries

`PlanScopedReadService/ListPlanSummariesInSpace` corresponds to native
`GET /v1/plans?view=summary&spaceId=SPACE`. Its source is verified main
`d973b771fd0e230f18ae5478212425e406c076fe`, specifically the Plan index handler,
Space selection/membership helpers, Plan visibility predicate and Space-bound
pagination in `internal/controlplane`. This is a Conduit extension, not a claim
of private Apollo protocol compatibility.

The separate exact procedure is intentional. An older Hub ignores unknown
fields on the existing unscoped request, so adding a selector there would not
establish that selection was applied. An older Hub refuses this new procedure.
The response echoes the validated Space beside the existing summary page;
callers must bind that coordinate before accepting rows. It is a grouping
coordinate, not an authority or a snapshot receipt.

Authentication resolves the original carrier before protobuf decoding. The
required native Space identity is validated and authorized before its private
membership is loaded. The native summary limit is then resolved: absent 100,
present 1 through 200, before any private Plan index read. Every row still uses
native Plan visibility, including Change Request visibility where applicable;
Space membership cannot grant Plan or Environment read permission. Search,
totals and cursors follow visibility and grouping. Current visibility and Space
membership are rechecked on every request; no frozen snapshot is promised.

The old unscoped service and its interface remain unchanged. Both use the same
query normalization, native pagination and summary mapper. Lease tokens and full
Plan/desired-state bodies never become summary fields. The new route accepts
only POST and bounds input at 1 MiB. Invalid selection/limit/cursor is an input
error. Missing Space capabilities are unavailable; configured callback and
repository errors keep native lifecycle categories with diagnostics redacted.
A field-mapping failure returns no partial page. REST keeps its existing status
and body behavior.

The compiled-descriptor guard constrains the distinct unary read-only service,
selector field types/presence/numbers, reused page, Space echo and native 100/200
bounds. Native helper tests cover read order and refusal stages. Actual Hub
fixtures compare full summary/page metadata, grouping, cursor binding, original
delegating carriers and private-field exclusion with REST; transport fixtures
check lifecycle redaction and all-or-error mapping. These tests require executed
runtime qualification; their presence alone is not a pass.

With an explicit selected Space, CLI `plan list --read-transport connect` uses
only this procedure. Unscoped Connect reads keep the existing service. The
configured HTTP/TLS/identity client, timeout and one-MiB response bound are shared;
filters, requested limit and cursor are retained on every page. The client binds
the echoed Space and requires a nonnil page before processing row metadata.
An unsupported procedure, missing/mismatched echo, mapping/refusal failure or
cursor cycle discards the full aggregate. No unscoped or REST fallback occurs.
REST remains default, and console caller migration remains separate.

Actual Hub CLI fixtures compare 201 selected rows across two pages in
JSON/YAML/table/CSV, with an excluded readable Environment and an ungranted
principal. Response controls cover missing/wrong echo or page, later-page scope
mismatch/refusal, cursor cycles and oversized responses. TLS/bearer/redirect,
request deadline/cancellation and unsupported-Hub refusal tests cover the new
procedure. These tests must execute before caller qualification is claimed.
