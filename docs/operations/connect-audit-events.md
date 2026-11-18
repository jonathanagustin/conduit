# Authorization audit event reads over Connect

This is a Conduit extension, not a private Apollo protocol compatibility claim.
`conduit.governance.v1.AuthorizationAuditReadService/ListAuthorizationAuditEvents`
projects the events view of `GET /v1/authorization-audit`. REST remains supported;
no existing console or CLI caller changes transport in this slice.

The original resolved operator carrier must pass the shared governance
administrator decision before protobuf decoding. The shared read repeats that
decision before parsing the query or reading the repository. Delegation does not
replace the carrier with the represented user's name.

Limits default to 100 and explicit limits must be 1 through 200. Resource, time
bounds, ascending/descending order and opaque forward cursors use the REST parser
and repository path. The lower time bound is inclusive and the upper bound is
exclusive. Cursors cannot be rebound to different filters or the chain view.
There is no previous cursor; `has_previous` records continuation position only.

Request/result/context metadata is JSON in protobuf bytes, preserving native
JSON number lexemes without conversion through protobuf Struct. Empty optional
context and missing recorded timestamps remain absent. Invalid stored timestamps
or unserializable metadata fail the whole response with a generic unavailable
error. Repository diagnostics are never returned.

This method grants no chain export, pruning, retention or archive authority.
Those operations and their custody decisions remain separate.
