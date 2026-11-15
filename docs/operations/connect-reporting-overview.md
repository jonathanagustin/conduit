# Reporting overview contract

`conduit.reporting.v1.ReportingService.GetOverview` is a contract for the
existing `GET /v1/overview` projection, mounted beside REST by the read adapter
described below. The overview slice implements part of #392; client migration
remains #393 and REST cutover remains #394. The broader console contract
inventory remains #391.

| Existing route | RPC contract | Native source |
| --- | --- | --- |
| `GET /v1/overview` | `ReportingService.GetOverview` | `internal/reporting/domain/overview.go`, `internal/controlplane/overview.go` and `internal/controlplane/http.go` |
| `GET /v1/packaging-runs/{runID}` | Existing `PackagingRunService.GetPackagingRun` | `proto/conduit/packaging/v1/packaging_run.proto` |

The Reporting source at `838e1257a07b7e157df3d597bc5db27f4b82ad17`
defines independent `CURRENT`/`UNAVAILABLE` panels. The proto retains that open
string vocabulary, authorized count and bounded item list. Empty unavailable
panels cannot be interpreted as a healthy fleet. RFC3339 generation timestamps
map to `google.protobuf.Timestamp`; integer counts use `int64`, whose ProtoJSON
representation is a decimal string. The adapter converts these typed
values explicitly rather than returning a REST JSON body verbatim.

Overview is an operator-filtered read. Authentication and delegation remain
transport metadata resolved by native Hub middleware, never caller-selected
request fields. Anonymous access remains refused. This method has no pagination
and accepts no authority, actor or approval fields. It cannot approve a Plan,
submit a packaging run or execute a change. Packaging's separate
`authority`, `planAuthority` and `approvalAuthority` fields remain unchanged.

The existing REST overview returns a successful projection with independently
unavailable panels when a store or panel authorization fails. The Connect
adapter preserves that behavior instead of converting every missing panel
into a failed RPC or successful zero. Authentication refusal maps to
`unauthenticated`; row and resource authorization must stay in the existing
projection. General 403/404/422 mappings for other route families remain work
in #391, not behavior established by this one read contract.

No stream method is invented here. Event subscription methods must be added
against the selected live-update contract, with per-topic authorization,
cursors and bounded buffering. The remaining initial families in #391 are
environments/reported-state, fleet installations, Plans/approvals, change-request
approve/reject, authorization audit and notification events; their contracts
must be derived from their registered routes and pagination rules.

`task proto` now formats generated TypeScript as part of regeneration.
`scripts/verify-proto.sh` authenticates the pinned buf binary, installs the
pinned Go plugins, lints the module, checks FILE compatibility against the
specified base (or the merge base with `origin/main`), and regenerates into a
temporary directory to compare committed output. The existing console CI lane
and complete local verifier run it. No additional workflow is introduced.
Regeneration also corrects the existing packaging TypeScript descriptor's
missing `templateVersion` and `toolVersions` fields already present in its
proto and Go output.

## Mounted overview adapter

The follow-up handler mounts only
`POST /conduit.reporting.v1.ReportingService/GetOverview` beside REST. It uses
`operatorClaims`, native `ResolvePrincipal` and the same
`BuildOverviewProjection` as `GET /v1/overview`. The resolved principal travels
in the request context, so an identity in the JSON body cannot widen scope.
Production ignores development identity headers unless explicitly enabled.
The route is `operator-filtered` in the shared authorization register; the
route gate scans its adapter and rejects missing or stale registrations.

Authentication runs before reading the request body or projection. Anonymous
callers receive unauthenticated, forbidden delegation receives permission-denied,
and unexpected identity-backend failures remain internal errors with sanitized
messages. Store and panel failures retain the native independent availability
rather than failing the whole projection. ProtoJSON's canonical int64 strings
and default/empty list representation differ from raw REST JSON but preserve
the typed values. No stream or mutation method is served, and an unregistered
method remains unavailable through the existing Hub guard.

Parity tests compare filtered Plans, installations, stale-report summaries and
unavailable panels for a viewer, an outsider and an administrator. Separate
tests compare identity refusals to REST, verify authentication precedes body
reads, and compare every converted native panel field with its proto value.

Delegation parity preserves the complete native principal, including its actor,
service-account flag and represented identity. The adapter does not substitute
the represented user for the actor or redefine bootstrap-administrator policy.
