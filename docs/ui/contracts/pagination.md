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
- The operator workbench requests one 20-row page for each operational index
  required by its visible screen (Product, Environment, Plan, Fleet, Change
  Request, Promotion and Packaging). It uses summary views for Product, Environment and Plan
  indexes, and expands only that visible page when the existing rich views
  need releases, reports, desired state or approval rows. Expansion has at most
  four concurrent reads; it never traverses the whole catalog.
- Each index has its own filter and cursor parameters in the hash query, such
  as `plans.search`, `plans.status`, `plans.environment` and `plans.cursor`.
  Changing filters resets that index's cursor and selection. Next/Previous use
  the Hub's opaque cursors, including an absent previous cursor for page one.
- `tab`, `plan`, `environment`, `change` and `run` retain the current area tab
  and explicitly selected native object. A linked selection outside the
  current page is read by exact id and identified as outside that page; it
  does not change the page's returned count or filters.
- Counts say "Showing N of total in this page", using only the authorized
  server total. No starting row or complete-catalog count is inferred from a
  seek cursor. Fleet takes its count from the native `summary.total` envelope;
  the other operational indexes expose `total` directly. Failed reads retain
  the exact query for retry.
- Header metrics remain unknown when their inputs are unread, filtered or
  incomplete. A partial page is not reported as a complete Hub count.
- The Space selector is also retained in the hash as `space`. The Hub's Space
  filter applies to the Environment index. Other indexes use their displayed
  server-side filters: a single Environment page is not used to infer Space
  membership of unrelated pages. Changing Space resets the Environment cursor
  and selected Environment. The selector explicitly lists this scope.
- A linked Environment is authorized and loaded before its timeline or blackout
  reads. If it is unavailable, those dependent reads remain unloaded rather
  than describing the first Environment in the current page.
- A route change prevents an older read from publishing into the new view.
  Every refresh still verifies the operator identity before publishing data.

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
