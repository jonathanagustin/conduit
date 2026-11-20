# Change Request discussion reads over Connect

Classification: Conduit extension. This read-only contract dual serves native
`GET /v1/change-requests/{requestID}/comments` through
`conduit.changeactivity.v1.ChangeRequestDiscussionReadService/ListChangeRequestComments`.
The native `changerequest comments REQUEST_RID --read-transport connect` CLI
reader opts in explicitly; REST remains its default and console callers remain REST.
There is no automatic fallback. The reader rejects foreign request IDs, invalid
records or timestamps, wrong value tiers, oversized/short continuation pages,
duplicate comment IDs, non-forward ordering and repeated cursors without partial
output. It preserves the configured TLS, credential and timeout transport.
An unconfigured or older Hub may return 404; that is not an empty discussion.

The mounted adapter binds the original authenticated request before decoding the
RPC body. It reuses native Change Request read authorization, including the
original actor and represented subject. Reader identity never replaces stored
comment author or actor. Production does not enable development identity headers.

The typed response preserves all seven native comment fields: ID, Change Request
ID, author, actor, creation timestamp, text and value tier. Creation timestamps
retain their instant and nanosecond precision. An unrepresentable timestamp fails
the whole response; it is not replaced with the current time.

REST and Connect share an eight-comment response page and the same opaque,
request-bound forward cursor, ordered by creation timestamp and then ID. A cursor
from another request is invalid. This is not a snapshot across concurrent appends.
The existing store still reads the request history before slicing: response
pagination does not promise a bounded database query.

Native invalid, unauthenticated, forbidden, missing and conflict errors map to
400, 401, 403, 404 and 409. Unexpected backend failures return unavailable without
private backend details. Authentication precedes malformed body decoding.

This contract adds no append, approval, review, execution or other mutation.
