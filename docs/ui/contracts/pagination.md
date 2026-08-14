# Index pagination contract

The Product, Environment, Plan, and Fleet indexes use server-side opaque seek
cursors. The browser owns the current filter and cursor in the URL so reload,
back, retry, and shared links address the same result window.

## HTTP contract

| Index | Endpoint | Filters | Views and limits |
| --- | --- | --- | --- |
| Products | `GET /v1/products` | `search` | `summary`: default 100, maximum 200; `full` or omitted: maximum 20 |
| Environments | `GET /v1/environments` | `search` | `summary`: default 100, maximum 200; `full` or omitted: maximum 20 |
| Plans | `GET /v1/plans` | `environment`, `status`, `installationId`, `search` | `summary`: default 100, maximum 200; `full`: maximum 20; omitted view resolves to `full` |
| Fleet | `GET /v1/fleet/installations` | `environment`, `product`, `health`, `rolloutStatus`, `configStatus`, `lifecycle`, `search`, `maxAgeSeconds` | default 100, maximum 200 |

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

Source anchors: `internal/controlplane/pagination.go`,
`internal/controlplane/catalog_projection.go`, `internal/controlplane/http.go`,
and the index tests under `internal/controlplane` and `web/src/test/e2e`.
