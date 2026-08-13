# Authorized Fleet pagination contract

`GET /v1/fleet/installations` is a bounded, server-filtered projection. The Hub
orders installations by `(environment, installationId)`, filters every row by
the effective principal's Environment read authority, computes the summary over
that complete authorized result, and only then selects a page. An unauthorized
row therefore cannot occupy a page slot or affect `summary`, `pageSize`, cursor
availability, or `maxResultAgeSeconds`.

## Request and response

The existing status, Product, Environment, lifecycle, and freshness filters are
joined by case-insensitive `search` over Environment, installation ID, and
Product. `limit` defaults to 100 and must be between 1 and 200. `cursor` is an
opaque continuation token: clients persist and return it but never parse or
construct it.

Every response includes `pageSize`, `limit`, and `hasPrevious`. `nextCursor` is
present only when another page exists. `previousCursor` is present when the
preceding page does not begin at the first result; `hasPrevious: true` with no
`previousCursor` means return to the first page by omitting `cursor`.
`summary.total` and all status counters describe the full authorized filtered
set, while `pageSize` describes this response's `installations` only.

The versioned base64url token binds its seek key to a SHA-256 fingerprint of the
normalized filters, search, and freshness threshold. Malformed, unsupported,
or cross-filter tokens fail with HTTP 422. Encoding makes the transport opaque,
not confidential or authoritative: authorization is always rerun before the
seek. The cursor is keyset-based rather than a database snapshot. Concurrent
inserts or removals may change later pages, but the strict ordered seek avoids
repeating the boundary row and does not retain server-side cursor state.

## Browser ownership

Fleet filters, applied search, and the current cursor live in the console URL.
Changing a filter or search removes the old cursor. Next and Previous update the
URL, so refresh, links, and browser history reproduce the same request. Search
is explicitly submitted to avoid a Hub read for every keystroke. Stable
`data-testid` values anchor the Fleet page and pagination Page Object, while
role, accessible name, URL, visible row, and global-total assertions prove the
behavior independently.

## Evidence

- `go test -race ./internal/controlplane -run TestFleet`
- `pnpm --dir web playwright test src/test/e2e/fleet.spec.ts`
- Go tests cover full traversal without boundary duplication, filter-bound
  cursors, invalid inputs, authorization-before-pagination, and global summary
  semantics.
- A committed-source mutation replay changed the strict installation seek from
  `>` to `>=`; the traversal test failed on the repeated boundary row, then
  passed after the source-only reversal.
- Browser tests cover Hub-side search, URL-owned filters/cursors, both paging
  directions, native disabled states, visible rows, and preserved global total.
