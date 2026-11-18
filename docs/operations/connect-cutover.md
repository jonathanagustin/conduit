# Hub REST and Connect cutover

Status: incremental dual serving; no REST deprecation date is set. This plan
records the representative dual-serve milestone in #394: authorized index and
detail reads plus governed Plan approval, with existing generated console reads
and explicit CLI read modes. Remaining contracts (#391), handlers (#392), console
clients (#393) and route-specific CLI migrations remain incremental. This does
not authorize route removal or change an existing caller’s supported protocol.

## Current route map

The following unary methods are present in the committed proto contracts and
HTTP adapters. Connect procedure paths use `/<service>/<method>` and HTTP POST,
including reads. A read encoded as POST is not a governed state change.

| REST route | Connect service and method | Current caller use |
| --- | --- | --- |
| `GET /v1/overview` | `conduit.reporting.v1.ReportingService/GetOverview` | Generated overview client |
| `GET /v1/packaging-runs` | `conduit.packaging.v1.PackagingRunService/ListPackagingRuns` | Generated list for initial, manual and live refreshes |
| `GET /v1/packaging-runs/{runID}` | `conduit.packaging.v1.PackagingRunService/GetPackagingRun` | Generated detail client; CLI `run get RUN_ID --read-transport connect` (REST default) |
| `GET /v1/packaging-runs/{runID}/events` | `conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents` | Generated history client for operator refresh |
| `GET /v1/products?view=summary` | `conduit.catalog.v1.ProductReadService/ListProductSummaries` | Generated legacy name-only summary helper; full-catalog operator reads remain REST |
| `POST /v1/plans/{planID}/approvals` | `conduit.plans.v1.PlanApprovalService/ApprovePlan` | Generated active operator delivery/approval panels and legacy helper; explicit CLI `plan approve --write-transport connect` returns a versioned receipt; CLI default remains REST, no automatic retry |
| `GET /v1/plans?view=summary` | `conduit.plans.v1.PlanReadService/ListPlanSummaries` | Explicit CLI `plan list --read-transport connect`; default remains REST |
| `GET /v1/environments?view=summary` | `conduit.environments.v1.EnvironmentReadService/ListEnvironmentSummaries` | CLI `environment list --view summary --read-transport connect`; full list stays REST |
| `GET /v1/authorization-audit?view=events` | `conduit.governance.v1.AuthorizationAuditReadService/ListAuthorizationAuditEvents` | Bounded dual-serve adapter; existing callers remain REST |
| `GET /v1/notifications/events` | `conduit.notifications.v1.NotificationService/ListNotificationEvents` | Generated Audit notification client |

These are Conduit-native contracts. See [overview](connect-reporting-overview.md),
[packaging runs](connect-packaging-run.md),
[notification events](connect-notification-events.md),
[event history](connect-packaging-events.md) and
[Plan summaries](connect-plan-summaries.md) and
[Product summaries](connect-product-summaries.md) and
[Plan approval](connect-plan-approval.md) for their bounded behavior.
The map does not imply that all Hub routes have RPC equivalents.

## Remaining contracts and callers

The Environment summary adapter reuses the native Environment/Space query and
is explicitly registered. [Its contract note](connect-environment-summaries.md)
records the narrow projection, presence and authority boundaries. The CLI's
explicit summary view can select Connect; console and full Environment callers
remain REST. #394 stays open for the remaining migration.

Known follow-ons are full environment detail/reported-state, fleet
installations, full Plan index/detail/cancel, change-request approve/reject,
and audit-chain export. [Audit event reads](connect-audit-events.md) share the
native administrator gate and pagination; caller migration remains separate. Governed packaging rerun/cancel already have REST
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
read parity. Plan approval adds governed-command parity in
`internal/hub/composition/plan_approval_connect_test.go`, including stored state,
audit, independent review and a failed handoff after approval is recorded. This
completes the representative proof in #394, without claiming all routes or CLI
commands have migrated.

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

### CLI packaging detail

`conduit run get RUN_ID` reads the existing native packaging status; its default
and explicit `--read-transport rest` modes use the REST detail route. Explicit
`--read-transport connect` calls the existing generated `GetPackagingRun` method.
Both retain the configured identity, TLS trust, timeout and one-MiB response
bound. The returned run id must match the requested id; malformed or mismatched
authority fields and negative duplicate-review counts are refused. An
unconfigured worker retains its native `configured: false` response and empty
run coordinates rather than inventing a run.

JSON/YAML keep the native status fields and attachment/source/revision evidence;
table/CSV show the run id, configured flag, phase, outcome, Plan reference and
target. Unknown phase/outcome vocabulary is retained. Plan and approval
authorities remain distinct: a packaging status is not a Plan approval or
execution receipt. Refusals, malformed/oversized responses, timeout and
cancellation fail on the selected transport without automatic fallback or retry.
This is a new CLI read over already dual-served routes, not a REST deprecation
or completion of the remaining environment/Plan/cancellation migration.

### CLI Plan summary index

`conduit plan list --read-transport connect` selects the generated native summary
read. The default and explicit `rest` modes retain the existing REST path. The
client preserves native filters and output fields, exact revisions/timestamps,
identity, TLS trust and timeout, and follows bounded native pages without
interpreting cursors. It rejects invalid evidence and continuation cycles.
Refusals never trigger a replay through REST. See
[Plan summaries](connect-plan-summaries.md) for the transport and conversion
bounds. Plan detail and cancellation remain REST; approval has the explicit
Connect mode documented below.

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

## Explicit CLI approval receipt

CLI `plan approve --write-transport connect` uses the generated governed command
and returns `conduit.plan-approval/v1`, rather than a full Plan document. The REST
default and explicit REST rollback retain the existing full-document output.
See [approval receipt and ambiguous outcomes](../architecture/cli-plan-approval-connect.md).

## Legacy console approval caller

The legacy `data/hub.ts` approval helper calls the generated native Plan approval
client and keeps its `Promise<void>` contract. It does not issue a compensating
read or retry through REST. The existing review UI remains the caller; no second
approval path or client-side authority is added. Active operator Plan, fleet and
change-request reads have not migrated as a consequence of this helper change.

Commands use same-origin credentials, a ten-second deadline, cancellation and
redirect refusal. A successful receipt must identify the requested Plan and
contain consistent review counts and valid native timestamps. Revision/counts
remain bigint and timestamps retain nanoseconds in the typed adapter. Missing,
corrupt or mismatched success evidence is an unknown outcome, not permission to
repeat the command. A single matching recorded-approval error detail preserves
the known recorded state and any failed execution handoff. Missing, conflicting,
wrong-Plan or unrecorded details remain unknown. Error messages direct the caller
to refresh and preserve the mapped status without displaying server diagnostics.
No command failure is automatically replayed over REST, and REST deprecation is
not activated.
