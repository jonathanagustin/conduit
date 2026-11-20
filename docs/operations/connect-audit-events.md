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

## Filtered chain projection

`conduit.governance.v1.AuthorizationAuditChainReadService/ListAuthorizationAuditChain`
serves the existing REST `view=chain` read with the same original-carrier
administrator decision, bounds, filters, order and forward-only cursors. Its
separate procedure makes an older Hub explicitly refuse an unsupported method;
clients must select REST explicitly rather than silently retry another transport.
No existing caller changes transport.

Each entry retains the exact native sequence, previous digest, digest, resource,
origin and payload bytes. Sequence is protobuf uint64, which uses a decimal
string in protobuf JSON. Optional display context is JSON bytes and is not a
digest input. Missing recorded time remains absent. Page boundaries carry the
native projection checkpoint in a cursor bound to the view and filters.

This is a Conduit extension backed by the native chain algorithm, not a private
Apollo protocol. A filtered reconstruction is not persisted ledger commit order,
an independently retained trust anchor, a verified raw archive export or archive
custody authority. The read adds no mutation, retention, pruning or publication.
The separate offline database verifier and its explicit checkpoint choices are
unchanged. REST/protobuf pagination and exact digest material, cursor rebinding,
original delegated carrier and refusal-before-decoding tests constrain the adapter.
