# Reporting overview contract

`conduit.reporting.v1.ReportingService.GetOverview` is a contract for the
existing `GET /v1/overview` projection. This change generates clients and
unmounted handler interfaces. It registers no route and changes no runtime
behavior. Handler composition belongs to #392, client migration to #393 and
REST cutover to #394. The broader console contract inventory remains #391.

| Existing route | RPC contract | Native source |
| --- | --- | --- |
| `GET /v1/overview` | `ReportingService.GetOverview` | `internal/reporting/domain/overview.go`, `internal/controlplane/overview.go` and `internal/controlplane/http.go` |
| `GET /v1/packaging-runs/{runID}` | Existing `PackagingRunService.GetPackagingRun` | `proto/conduit/packaging/v1/packaging_run.proto` |

The Reporting source at `838e1257a07b7e157df3d597bc5db27f4b82ad17`
defines independent `CURRENT`/`UNAVAILABLE` panels. The proto retains that open
string vocabulary, authorized count and bounded item list. Empty unavailable
panels cannot be interpreted as a healthy fleet. RFC3339 generation timestamps
map to `google.protobuf.Timestamp`; integer counts use `int64`, whose ProtoJSON
representation is a decimal string. A future adapter must convert these typed
values explicitly rather than returning a REST JSON body verbatim.

Overview is an operator-filtered read. Authentication and delegation remain
transport metadata resolved by native Hub middleware, never caller-selected
request fields. Anonymous access remains refused. This method has no pagination
and accepts no authority, actor or approval fields. It cannot approve a Plan,
submit a packaging run or execute a change. Packaging's separate
`authority`, `planAuthority` and `approvalAuthority` fields remain unchanged.

The existing REST overview returns a successful projection with independently
unavailable panels when a store or panel authorization fails. A future Connect
adapter must preserve that behavior instead of converting every missing panel
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
