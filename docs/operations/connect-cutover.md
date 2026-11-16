# Hub REST and Connect cutover

Status: incremental dual serving; no REST deprecation date is set. This plan
addresses the documentation portion of #394. Contracts (#391), handlers (#392),
console clients (#393), governed-write parity and CLI migration remain tracked
separately. It does not authorize route removal or change the supported protocol
of an existing caller.

## Current route map

The following unary methods are present in the committed proto contracts and
HTTP adapters. Connect procedure paths use `/<service>/<method>` and HTTP POST,
including reads. A read encoded as POST is not a governed state change.

| REST route | Connect service and method | Current caller use |
| --- | --- | --- |
| `GET /v1/overview` | `conduit.reporting.v1.ReportingService/GetOverview` | Generated overview client |
| `GET /v1/packaging-runs` | `conduit.packaging.v1.PackagingRunService/ListPackagingRuns` | Generated list for initial, manual and live refreshes |
| `GET /v1/packaging-runs/{runID}` | `conduit.packaging.v1.PackagingRunService/GetPackagingRun` | Generated detail client |
| `GET /v1/packaging-runs/{runID}/events` | `conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents` | Generated history client for operator refresh |
| `GET /v1/plans?view=summary` | `conduit.plans.v1.PlanReadService/ListPlanSummaries` | Explicit CLI `plan list --read-transport connect`; default remains REST |
| `GET /v1/notifications/events` | `conduit.notifications.v1.NotificationService/ListNotificationEvents` | Generated Audit notification client |

These are Conduit-native contracts. See [overview](connect-reporting-overview.md),
[packaging runs](connect-packaging-run.md),
[notification events](connect-notification-events.md),
[event history](connect-packaging-events.md) and
[Plan summaries](connect-plan-summaries.md) for their bounded behavior.
The map does not imply that all Hub routes have RPC equivalents.

Known follow-ons are environment index/detail/reported-state, fleet
installations, full Plan index/detail/approval/cancel, change-request approve/reject,
and authorization audit. Governed packaging rerun/cancel already have REST
routes but no methods in the current packaging proto.
Derive their RPC messages from those routes before migrating their callers;
do not infer a method from a similar name in another service.

## Dual-serve rules and evidence

Keep REST and Connect mounted against the same application services and
repositories. Share native principal resolution, delegation carrier, resource
visibility, validation and command authorization. Never replace a delegating
carrier with the represented user's name. Authentication must precede body
parsing and store access; an invalid RPC body is not proof of authorization.

For each migrated route, compare the same seeded state through both transports:

- Successful index and detail reads, including filters, cursor boundaries,
  stable ordering, empty results and independently unavailable overview panels.
- Anonymous, forbidden, delegated, outsider and administrator callers, including
  production refusal of development identity headers and unknown RPC methods.
- Validation and missing-resource failures. Compare typed values and mapped
  error categories, not raw JSON bytes: ProtoJSON int64 strings, timestamps and
  default-list representation differ from REST JSON.
- For governed commands, authorized and denied transitions, stale revisions,
  required approvals, cancellation and audit attribution. Assert the same
  persisted state and no mutation on refusal. A unary read's POST does not
  satisfy this command test.

Existing examples are `internal/packagingworkflow/httpapi/connect_list_test.go`,
`internal/packagingworkflow/httpapi/connect_test.go`,
`internal/hub/composition/packaging_index_test.go`,
`internal/hub/composition/reporting_connect_test.go` and
`internal/hub/composition/notification_connect_test.go`. They establish bounded
read parity. Representative governed-command parity remains unfinished; #394
must stay open until that proof and the CLI work are complete.

During this period, a new route in a family being migrated needs its REST
registration, proto contract, RPC adapter, authorization-register entry and
parity tests in the same change. If the RPC work is deferred, record the gap in
#391/#392 and this map; keep that family on REST and block its deprecation.
Run the existing proto, route-authorization, Go and relevant console/CLI gates.
Do not count generated descriptors alone as a mounted or authorized procedure.

## Caller migration and rollback

Migrate one read path at a time using generated clients and explicit error
handling. Keep REST enabled throughout the transition. Revert the caller's
transport selection to roll back; do not require data migration or deleting the
RPC handler. A Connect permission failure must remain a permission failure,
not trigger a REST retry that might use different credentials.

The CLI migration must cover `env list`, `plan list/show/approve/cancel` and
`run list`. Existing REST behavior is exercised by
`internal/conduitcli/hub_client_test.go`. Add generated Go Connect-client tests
for each migrated command and retain an explicit REST mode until cutover is
complete. A compatibility fallback may select REST only when the operation is
known not to have been sent (or a read-only capability check has established
that its RPC is unavailable). Never retry an approval or cancel over REST after
a timeout, connection reset, ambiguous server error or permission refusal:
the first command may already have committed. Read the resulting native state
before an operator decides whether another command is needed.

### CLI packaging index

`conduit run list --read-transport connect` explicitly selects the generated Go
client for the packaging-run index. The default remains `rest`; pass
`--read-transport rest` to select the existing REST read for rollback. Both modes
reuse the configured identity, TLS trust and HTTP timeout. Connect follows native
opaque cursors with the same summary filters, preserves attachment evidence and
authority fields, and refuses totals outside the local integer range. Responses
retain the existing one-MiB per-message bound. A permission refusal, unavailable
procedure, timeout or canceled read fails on the selected transport; it never
silently replays through REST. Other reads and governed commands remain REST.

### CLI Plan summary index

`conduit plan list --read-transport connect` selects the generated native summary
read. The default and explicit `rest` modes retain the existing REST path. The
client preserves native filters and output fields, exact revisions/timestamps,
identity, TLS trust and timeout, and follows bounded native pages without
interpreting cursors. It rejects invalid evidence and continuation cycles.
Refusals never trigger a replay through REST. See
[Plan summaries](connect-plan-summaries.md) for the transport and conversion
bounds. Plan detail and governed approve/cancel commands remain REST.

## Live updates

[ADR 0016](../decisions/0016-console-live-events.md) selects same-origin SSE for
the general console stream. Keep that stream and its authorization, envelope,
cursor/resume and resync semantics independent of unary REST-read removal.
SSE does not become Connect streaming merely because reads migrate. A later
streaming change needs a separate decision and equivalence proof over the same
committed event vocabulary. Events remain hints to reload authorized state,
not approval authority or proof of execution.

## Deprecation and removal

Do not emit deprecation signals yet. A family becomes eligible only after its
complete route map is implemented, index/detail/governed-command authorization
parity passes, console and CLI callers have migrated with rollback coverage,
and supported external callers have an inventoried migration path. Unmapped
routes and unresolved errors block that family's cutover.

A reviewed follow-up must name the affected routes, caller owners, supported
client versions, announcement date, migration window, concrete removal date and
removal issue. Only then add standards-based `Deprecation`, `Sunset` and a
migration-document `Link` to those REST responses, without changing their normal
semantics. No date is implied by this plan. Confirm caller migration and error
rates over the agreed window without logging credentials or tenant payloads;
unknown caller usage is not evidence that usage is zero.

Removal requires that issue's explicit approval, completed migration evidence,
and a tested way to restore REST for the supported rollback window. Remove no
REST routes as part of #394 and never silently redirect a governed command to
a different protocol.
