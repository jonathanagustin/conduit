# Native Product summary read over Connect

`conduit.catalog.v1.ProductReadService/ListProductSummaries` dual-serves
`GET /v1/products?view=summary`. This Conduit-native read returns the existing
Product coordinate, release count and latest semantic version. A publication
without a Release still contributes a Product with zero releases and no latest
version. It does not expose publication payloads, assets, configuration or a
Product write. REST remains mounted; callers have not switched automatically.

## Shared native query

REST and Connect use `listAuthorizedProductCatalog`, then the existing native
filter, seek pagination and summary projection. Visibility is checked before
search, totals and pagination. Bootstrap administrator authority is tested on
the original credential carrier, including delegation; ordinary Product grants
use the effective principal exactly as in REST. A foreign Product is absent
from both the rows and the total. This does not grant Plan approval authority.

The RPC authenticates through the Hub's existing operator claims and principal
resolution before decoding the body or reading the catalog. Development
identity headers require the existing explicit insecure opt-in. Without it,
anonymous requests and untrusted development headers are rejected before body
decoding. When explicitly enabled, the existing REST local-developer fallback
is preserved; this is a development exception, not production authentication.
Only the registered POST
procedure is mounted. Its authorization register class is `operator-filtered`.

## Fields and errors

The optional limit defaults to 100 and accepts explicit values 1–200. Search and
opaque cursors have their existing native meaning: changing the normalized
search invalidates its cursor, and REST cursors can be used through Connect.
Native order, next/previous cursors and `hasPrevious` are preserved. Malformed
stored UTF-8 uses the same replacement as decoded native JSON.

The response carries Product summaries, visible filtered total, page size,
limit, cursors and `hasPrevious`. Release count and total use int64: ProtoJSON
encodes them as strings, while generated TypeScript retains bigint values.
Empty/default JSON representation can differ without changing these values.

Invalid limits and cursors are REST 422 and Connect `invalid_argument` (400).
Authentication and denied delegation retain 401/403. Store failures become a
sanitized Connect `unavailable` (503), rather than exposing native diagnostics;
REST retains its lifecycle-error mapping. Cancellation reaches the native
reader. A failure never authorizes a retry through another transport.

## Verification

`product_summary_connect_test.go` in `internal/controlplane/httpapi` uses a real
generated Go client over HTTP to check large integer counts, native query
inputs, cancellation, error categories and authentication before decoding/read.
The counterpart in `internal/hub/composition` compares actual native REST and
Connect pages, including outsiders, administrators, delegated credentials,
unfiltered visible counts, semantic versions, metadata-only Products,
filter-bound cursors and unknown routes. These tests establish bounded read
parity, not runtime deployment or governed-write parity. See
[the cutover plan](connect-cutover.md); #391, #392 and #394 remain open.

## Console summary helper

`web/src/api/product-summaries.ts` uses the generated Product read client, with
same-origin credentials, cancellation and typed Hub errors. Counts stay bigint;
invalid page evidence is refused instead of silently rounded or discarded.
The legacy `data/hub.ts` `listProducts` helper preserves its name-only DTO and
20-item first page, without following the opaque next cursor. It never retries
an RPC refusal through REST. Full-catalog operator reads still require release
records and remain on REST; this bounded helper change is not their migration.
