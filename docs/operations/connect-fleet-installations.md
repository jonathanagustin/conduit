# Native Fleet installation read

The Hub dual-serves `GET /v1/fleet/installations` through the exact POST
`conduit.fleet.v1.FleetReadService/ListFleetInstallations`. This is a Conduit
extension derived from the native projection and paginator at main commit
`d17bf007c5f2c2303148da1bd88580618b95615d`, not a private Apollo protocol.
Existing CLI and console Fleet callers remain on REST.

## Authority and query

Authentication precedes decoding. The original resolved carrier determines
administrator status; delegated Environment reads use the effective principal.
Readable Space membership narrows Environment authority and never grants it.
Both filters apply before rows, summaries and counterfactual facets are built.
A Product binding cannot substitute for an Environment binding. A caller with
no visible Environments receives an empty filtered projection.

The request carries Environment, installation, Product, health, rollout,
configuration and lifecycle filters, search, cursor and optional Space ID.
Absent limit uses 100; explicit values must be 1..200. Absent maximum age uses
the native reporting policy; explicit seconds must be 1..86400. Explicit zero
is invalid. Space selection and membership resolve before query validation.
Cursors retain the native filter and Space fingerprint, including maximum age;
changing page size does not change that fingerprint. The Hub samples its clock
after constructing visibility. Callers cannot supply that clock or declare
that reporting silence is expected. The existing Plan selector and intermittent
reporting behavior remain authoritative.

Only the exact read method is mounted, with a 1 MiB request bound and typed,
sanitized failures. Cookie-authenticated POST reads retain the existing Origin
and CSRF policy. No write, approval, lease or packaging authority is introduced.
No transport retry or REST fallback is added.

## Projection boundary

The response carries all REST-visible installation fields: Environment,
installation, Product, namespace, desired and observed versions, release and
configuration digests, open native health/rollout/configuration/lifecycle
strings, observation time and age, and stale state. These values are observations,
not execution permission or a new readiness decision.

The summary counts all matching visible rows before pagination. Facets preserve
the native counterfactual calculation: each dimension excludes its own filter,
while retaining caller and Space restrictions. There is no invented outer total;
`summary.total` is the native total. Internal facet-contribution scaffolding is
not serialized. Page size, limit, next/previous cursor and previous-page presence
retain the native paginator semantics, including an empty previous cursor when
returning to the first page.

Counts and ages use exact protobuf int64 values rather than floating-point
Struct values; protobuf JSON represents those integers as strings. Timestamps
preserve UTC instants and nanoseconds. A missing observation remains absent,
never the current time. Invalid timestamps, negative counts/ages and ambiguous
UTF-8-normalized count keys refuse the whole result. Open status strings remain
open. Protobuf collections do not retain Go nil-versus-empty distinctions.
