# Native Plan summary read over Connect

`conduit.plans.v1.PlanReadService/ListPlanSummaries` dual-serves the existing
`GET /v1/plans?view=summary` projection. This is a Conduit-native read contract.
It does not implement full Plan reads, approval, cancellation or execution.
REST remains mounted. The console remains on REST; CLI `plan list` can explicitly
select Connect as described below.

## Shared native query

Both transports use the same authorized Plan index query. Resolve the original
operator carrier, including delegation, before decoding an RPC body or reading
Plans. Each Plan is checked with `canReadPlan` before filters and pagination:
Change Request visibility governs a Plan carrying a Change Request ID; otherwise
native Environment visibility applies. A missing or unreadable Change Request
withholds its row. Counts cover only authorized, filtered matches.

The RPC fixes `view` to `summary`. `environment`, `status` and `installationId`
retain native trimming, and `search` retains its native normalization and cursor
fingerprint. An absent optional `limit` selects 100; explicit limits must be
1 through 200. Native opaque next/previous cursors, ordering and filter binding
are retained. Invalid limits fail before the reader; cursor validation retains
its existing native position after authorized rows are read.

## Projection mapping

| Native field | Proto field/type |
| --- | --- |
| `plans[].id`, `environment`, `status` | Strings; status stays open native vocabulary |
| `stepCount`, `approvalCount`, `requiredApprovals` | `int64`, preserving reported counts without approval authority |
| `createdAt`, `updatedAt` | Checked `google.protobuf.Timestamp`, preserving instant and nanoseconds |
| `revision` | `uint64`, preserving the persisted revision exactly |
| `total` | `int64`, authorized and filtered matches |
| `pageSize`, `limit` | `int32`, bounded page metadata |
| `nextCursor`, `previousCursor`, `hasPrevious` | Native strings/boolean |

ProtoJSON represents 64-bit integers as strings; generated clients retain exact
values. Empty-list/default representation and timestamp timezone rendering can
differ from REST JSON. Malformed stored UTF-8 uses the same replacement as
decoded native JSON. Unrepresentable stored timestamps fail instead of becoming
an invented time. No lease token, full Plan step, desired-state payload or
execution credential can be represented by this summary message.

## Refusals and verification

Authentication/permission refusals remain unauthenticated/permission-denied
errors. Native 422 query/cursor failures map to `INVALID_ARGUMENT`; repository
failures map to sanitized `INTERNAL` without private diagnostics. Missing read
wiring is unavailable, not a successful empty index. Cancellation reaches the
native reader. Only the explicit POST read is mounted; GET on its RPC path and
unknown command procedures are not additional interfaces.

The route register names this `operator-filtered`. Tests compare seeded native
and RPC pages, exact revision/timestamp values, unfiltered cross-tenant counts,
opaque cursors and Change Request visibility. Valid and invalid delegated
carriers are tested independently. Existing native full/summary index tests
remain applicable to the shared query. Run `scripts/verify-proto.sh`, native/RPC
race tests, vet, route-authorization verification and complete exact-head local
CI before merge. Broader migration remains #391, #392, #393 and #394.

## CLI summary index

`conduit plan list --read-transport connect` selects this generated Go client.
The default is `rest`, and `--read-transport rest` explicitly selects the existing
REST path. Both modes use the configured operator identity, trust roots and HTTP
timeout, retain the native summary filters and follow opaque cursors. Typed
refusals, cancellation, deadlines and unsupported methods do not replay through
the other transport. A repeated continuation is an error.

The RPC adapter returns the existing CLI summary DTO and output formats, with
exact uint64 revisions and nanosecond timestamps. Page metadata, timestamps and
native integer conversions are checked before output. Each response retains
the existing one-MiB read bound. Plan detail, approve and cancel remain REST.

## Selected Space in the CLI

The current Plan summary proto has no Space selector. A CLI/operator caller with
an explicit selected Space refuses Connect before making a network request or
emitting output; it does not silently issue an unscoped read or replay over
REST. Select REST explicitly for Space-scoped Plan lists. That native client
preserves the selected Space together with filters and cursor on every page.
Adding a scoped typed selector and authorization/grouping parity remains follow-on
contract work; this refusal is not a claim that scoped RPC is implemented.
