# Connect packaging-run read

This slice is the first typed Connect call on the Hub. It proves the layout
the rest of the migration can extend. It does not replace REST.

## What is served

The read is the one `GET /v1/packaging-runs/{runID}` already serves. That REST
route stays mounted. The filtered index is also dual-served over Connect. History, cancel, and rerun stay on REST.

| | |
| --- | --- |
| Proto package | `conduit.packaging.v1` |
| Proto file | `proto/conduit/packaging/v1/packaging_run.proto` |
| RPC | `PackagingRunService.GetPackagingRun` |
| Connect path | `POST /conduit.packaging.v1.PackagingRunService/GetPackagingRun` |
| Go stubs | `gen/go/conduit/packaging/v1` |
| TypeScript stubs | `web/src/api/gen/conduit/packaging/v1` |

The response fields are the REST body: `tier`, `configured`, `authority`,
`planAuthority`, `approvalAuthority`, `runId`, `phase`, `outcome`, `reason`,
`sourceDigest`, `artifactDigest`, `proposalRevision`, `targetPlanRef`,
`planId`, `handoffReconciled`, `lastReview`, and `duplicateReviews`.

Both transports call the same reader and the same operator check. Authority
stays `packaging-run`, plan authority stays `conduit-plan`, and approval
authority stays `conduit-governance`, as in
[ADR 0013](../decisions/0013-temporal-for-packaging-workflows.md). A Hub with
no packaging worker still answers `configured: false` to an authenticated
operator. Anonymous callers are refused. The procedure is `operator-any` in
the route register, the same decision as the REST route.

## Console

The packaging-run detail view is the only console read on this client. It
goes through `usePackagingRunDetail` in `web/src/api/packaging-run.ts`. Every
other console call stays on its current HTTP client. Replacing the body of
`fetchPackagingRunDetail` puts that one read back on REST.

The view is `/console/packaging-runs/{runId}`.

## Regenerate

`task proto` rewrites the Go and TypeScript stubs from `proto/conduit`. The
stubs are committed.

## How the wider migration extends this

The proto module root is `proto/`. Further Hub domains are sibling packages
under `proto/conduit/<domain>/v1/`, with the same `buf.yaml` and
`buf.gen.yaml`.

- **#391** adds the rest of the console-used Hub routes as those packages,
  including pagination and the error statuses the REST routes already return.
- **#392** adds Connect handlers for those methods beside the REST routes,
  reusing each route's existing authorization decision. An unregistered
  method stays unserved.
- **#393** moves the remaining console reads onto the TypeScript client, one
  route family at a time. This slice moves only packaging-run detail.

This is the proof for #395, under #381. REST stays until the cutover in #394
says otherwise.

## Filtered run index

`PackagingRunService.ListPackagingRuns` is the typed twin of
`GET /v1/packaging-runs`. Only its exact POST procedure is exposed. Both call
one native index reader: per-environment visibility runs before filtering,
counting and pagination. A delegated identity retains the original credential
carrier for the native authorization decision. No new approval authority is
introduced.

The request accepts the native view, optional limit, opaque cursor, phase,
outcome, Plan ID and search. Omitting limit selects the native default; zero is
an invalid explicit limit. Native cursors work across both transports with the
same active filters. Invalid inputs are REST 422 and Connect `invalid_argument`
(HTTP 400); authentication remains 401. Reader failures are sanitized as REST
502 and Connect `unavailable` (HTTP 503), following their existing transport
conventions. An unconfigured reader returns `configured: false`, an empty index
and the same effective limit.

The response includes visible redacted rows and native pagination metadata.
Total uses an int64 field, represented as a string in protobuf JSON and as a
bigint in the generated TypeScript API. This change generates the client
contract but leaves the console's existing list path on REST; detail, overview
and Audit reads already use their generated clients. The broader migration
issues #391–#394 remain open.
