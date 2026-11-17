# Index pagination contract

The Product, Environment, Plan, Fleet, Change Request, and Promotion indexes use server-side opaque seek
cursors. The browser owns the current filter and cursor in the URL so reload,
back, retry, and shared links address the same result window.

## HTTP contract

| Index | Endpoint | Filters | Views and limits |
| --- | --- | --- | --- |
| Products | `GET /v1/products` | `search` | `summary`: default 100, maximum 200; `full` or omitted: maximum 20 |
| Environments | `GET /v1/environments` | `search` | `summary`: default 100, maximum 200; `full` or omitted: maximum 20 |
| Plans | `GET /v1/plans` | `environment`, `status`, `installationId`, `search` | `summary`: default 100, maximum 200; `full`: maximum 20; omitted view resolves to `full` |
| Fleet | `GET /v1/fleet/installations` | `environment`, `product`, `health`, `rolloutStatus`, `configStatus`, `lifecycle`, `search`, `maxAgeSeconds` | default 100, maximum 200 |
| Change Requests | `GET /v1/change-requests` | `environment`, `status`, `search` | default 100, maximum 200 |
| Promotions | `GET /v1/promotions` | `product`, `status`, `search` | default 100, maximum 200 |
| Packaging runs | `GET /v1/packaging-runs` | `phase`, `outcome`, `planId`, `search` | `summary`: default 100, maximum 200; `full` or omitted: maximum 20. Rows are ordered by run id. |
| Packaging-run events | `GET /v1/packaging-runs/{runID}/events` | the run id; `view` and `limit` only | same view and limit rules. Events are ordered by sequence. A step that has not been reached is absent, not a pass. |
| Authorization audit | `GET /v1/authorization-audit` | `resourceRid`, `since`, `until`, `view` (`events` or omitted, or `chain`) | default 100, maximum 200. Oldest first. Forward-only: `previousCursor` is never issued. |

The authorization audit is the one index bounded in the repository query
rather than after a full read, because the log is retained indefinitely.
`since` (inclusive) and `until` (exclusive) are RFC 3339 timestamps compared
with each event's `createdAt`; PostgreSQL compares them at microsecond
resolution. PostgreSQL pages by chain sequence, which is the hash-chain order;
the memory and file stores have no chain and page by event time and then ID.
A `view=chain` cursor also carries the projection checkpoint its page continues
from, so concatenated pages verify from genesis exactly as one read would. A
request with no `limit` returns the first 100 events and a `nextCursor`; before
issue #841 it returned the whole log. An unparseable `since`, `until` or `view`
returns `422`.

Every page returns `pageSize`, `limit`, `hasPrevious`, and optional
`nextCursor` and `previousCursor`. Index envelopes also return the authorized,
filtered `total`; Fleet returns its authorized summary.

Authorization happens before search, counting, and pagination. Cursors are
opaque, versioned, bounded to 2,048 bytes, and tied to a fingerprint of the
active filter set. A malformed cursor, a cursor reused with different filters,
or an out-of-range limit returns `422`.

## Browser behavior

- Reset the cursor when a filter changes.
- Request `view=summary` for Product, Environment, and Plan indexes.
- Disable Next when `nextCursor` is absent.
- Disable Previous only when `hasPrevious` is false; an absent
  `previousCursor` represents the first page.
- Keep the last requested URL when a read fails so retry does not widen the query.
- Do not infer totals or previous cursors from the rows in the current page.
- The operator workbench's background refresh
  (`web/src/console/operator/live-hub.js`) is a bounded catch-up, not a
  cursor-owning index view: it follows `nextCursor` for at most five pages per
  index. Reaching that bound is a truncated read, not a failed one. The rows read
  are kept and the affected views state "Showing the first N of `total`", using
  the envelope's `total` when present and no count otherwise.
- That refresh still requests the `full` view for Products, Environments, and
  Plans, because its views read fields the summaries omit (`plan.steps`,
  `desiredState`, `approvals`, `reported.entities`, `releases`). With the 20-row
  full-view maximum, the bound is 100 rows for those three indexes.

Packaging-run pages use the same cursor shape: version, filter fingerprint,
and seek key, capped at 2,048 bytes. The index fingerprint is `phase`,
`outcome`, `planId`, `search`, and the resolved view. Changing `limit` does
not invalidate a cursor. The transport description is
`docs/operations/packaging-runs.openapi.yaml`.

Source anchors: `internal/controlplane/pagination.go`,
`internal/controlplane/catalog_projection.go`,
`internal/controlplane/operator_index_projection.go`,
`internal/controlplane/http.go`,
`internal/packagingworkflow/httpapi/page.go`, and the index tests under
`internal/controlplane` and `web/src/test/e2e`.
