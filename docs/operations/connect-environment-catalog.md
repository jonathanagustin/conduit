# Full native Environment index

`conduit.environments.v1.EnvironmentCatalogService/ListEnvironments` corresponds
to `GET /v1/environments` with absent or `full` view. Its native source is main
`e0be321e42b54972dfdc73040cfa8cfbfab1682d`, specifically `http.go`,
`environment_index_read.go`, `catalog_projection.go` and `pagination.go` in
`internal/controlplane`. This is a Conduit extension, not a private Apollo
protocol claim. Existing CLI and console full-list callers remain REST (#394).

The unary read reuses the full `EnvironmentRecord` from the
[detail contract](connect-environment-detail.md). A separate catalog service
avoids changing existing service interfaces and the detail-to-summary import
relationship. It grants no publication, approval or report-write authority.

Authentication of the original carrier precedes request decoding. Optional
`space_id` preserves absence separately from present empty or invalid values.
Readable Space selection precedes private membership/catalog reads. The native
resource-read decision filters each row; Space membership only narrows it.
Search, totals and cursor pagination operate on this filtered set, so an
ungranted authenticated caller can receive an empty successful index.

Absent `limit` uses 20; present values must be 1 through 20. The summary
index's 100/200 bounds do not apply. Native cursors remain bound to normalized
search and selected Space, with visibility and membership evaluated again on
every page. They do not freeze a snapshot. Full configuration and optional
reports retain the detail mapper's raw values and presence semantics. The
liveness clock is sampled after catalog reads and pagination.

Invalid typed selection, bounds or cursors return Connect `invalid_argument`;
native REST retains its existing 400/422 distinction. Missing Space capabilities
return `unavailable`. Configured callback and catalog failures retain their
lifecycle category, with internal diagnostics redacted by the transport.
Only the exact contracted POST is mounted, with a 1 MiB request bound.

`scripts/verify-environment-catalog-contract.py` checks the compiled descriptor
against the native full page, field widths/cardinality, optional selectors,
read-only unary method and native bound. The proto gate also retains the full
Environment detail guard. Native helper tests cover the full limit, visibility,
pagination, post-read clock and refusal stages; composition tests compare native
REST metadata and full typed detail records. These are required checks, not a
claim that merely adding tests qualifies their behavior.
