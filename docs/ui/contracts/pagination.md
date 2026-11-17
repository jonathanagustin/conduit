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
| Fleet | `GET /v1/fleet/installations` | `environment`, `installationId`, `product`, `health`, `rolloutStatus`, `configStatus`, `lifecycle`, `search`, `maxAgeSeconds` | default 100, maximum 200 |
| Change Requests | `GET /v1/change-requests` | `environment`, `status`, `search` | default 100, maximum 200 |
| Promotions | `GET /v1/promotions` | `product`, `status`, `search` | default 100, maximum 200 |
| Packaging runs | `GET /v1/packaging-runs` | `phase`, `outcome`, `planId`, `search` | `summary`: default 100, maximum 200; `full` or omitted: maximum 20. Rows are ordered by run id. |
| Packaging-run events | `GET /v1/packaging-runs/{runID}/events` | the run id; `view` and `limit` only | same view and limit rules. Events are ordered by sequence. A step that has not been reached is absent, not a pass. |
| Authorization audit | `GET /v1/authorization-audit` | `resourceRid`, `since`, `until`, `view` (`events` or omitted, or `chain`), `order` (`asc` or omitted, or `desc`) | default 100, maximum 200. Ascending recorded order by default. Forward-only: `previousCursor` is never issued. |

The Fleet `installationId` filter is an additive Conduit-native exact,
case-sensitive identity filter, distinct from substring `search`. Combined
with `environment` and `limit=1`, it resolves one PackagingRun action target
without depending on the visible index page. Authorization still precedes
pagination and counts; an unreadable or missing target returns an empty page.
The identity filter also narrows facets. Nonempty identities bind cursors to
that target; omitting the filter preserves the prior cursor encoding. No
Palantir protocol claim or generated public API change is introduced.

Before a packaging assistant proposal, the console re-reads the selected run
and its exact target. A changed run binding, missing Product, observed-only
installation, refused read or selection change prevents the tool POST. These
checks include removed/added context chips, and recheck current membership at
the final POST boundary after the target resolver returns. Removing a chip
while its target read is pending cancels that proposal intent. These
context reads do not replace displayed index rows, add off-page rows to a
count, or approve or execute the proposal. The Hub remains the authority for
action rules and independent change review.

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

Each PostgreSQL audit page reads the native ledger and pending legacy records
from one repeatable database snapshot. During a mixed Hub rollout, pending
legacy records follow the native ledger in event-time/ID order. A cursor in
that legacy portion is bound to the complete matching pending set. Adoption or
another legacy write that changes that set invalidates the cursor and returns
`422`; restart without the cursor to read the authoritative order. Legacy
cursors issued before this binding existed are also refused. Native sequence
cursors continue to seek the ledger as before. No snapshot spans HTTP requests.

`order=desc` reverses that read order and returns `order: "desc"` in each
response. PostgreSQL traverses native chain sequence in descending order;
the development stores traverse event time and ID in descending order. During
a mixed rollout the reverse compatibility order is pending legacy records
first, then native records. A cursor is bound to its order as well as its
filters. Omitted order and explicit `order=asc` keep existing ascending
cursors usable. Any other order value returns `422`.

For `view=chain`, projection sequence still increases from 1 in returned order,
including a descending read. The hashes describe that reconstructed read
projection; they are not the stored ledger's sequence or digests. Concatenated
pages verify from projection genesis. The console requests descending pages
to retain the most recently recorded events inside its catch-up bound, keeps
their projection order for proof checks, and displays newest recorded events
first. A backdated event can therefore appear before a later event timestamp.

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
- The Space selector is retained in the hash as `space`. Persisted Environment,
  Product and Bundle anchors scope the Environment, Product, Plan, Fleet,
  Change Request, Promotion and Bundle REST indexes before search, counts,
  facets and pagination. Every row retains its existing read authority.
  The Space ID is part of each scoped cursor fingerprint; omitted selection
  preserves prior unscoped fingerprints. Changing Space resets affected cursors
  and linked selections while preserving independent filters and planning state.
  Page expansion and exact-id linked reads carry the same selection; a record
  outside it returns not-found instead of rejoining the selected view.
  Missing, denied or malformed remembered selections and failed Space reads
  never implicitly clear selection. Packaging, channels, workspaces, rollouts,
  upgrades and mixed-resource audit remain explicitly unscoped.
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
