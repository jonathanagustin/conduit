# Declared native Environment summary contract

Status: contract and generated stubs only. The Hub does not register
`conduit.environments.v1.EnvironmentReadService/ListEnvironmentSummaries` yet.
A generated client or handler constructor does not establish a served RPC or
an authorization decision. Use REST until #392 supplies an authorized adapter
and #394 qualifies caller migration. No REST deprecation or fallback is enabled.

## Native source and bounded projection

The declaration derives from `GET /v1/environments?view=summary` in
`internal/controlplane/http.go` and `catalog_projection.go`. It carries native
Environment ID/generation, optional report summary and spoke liveness. It
intentionally omits Environment configuration, policy, registry, namespace and
secret-assignment collections, and all reported Entity bodies. It is a narrow
native projection, not an interchangeable full Environment document or a
private reference-platform protocol.

Generation stays an opaque string. A missing report differs from a report with
zero Entities. Report counts and timestamps are observations, not desired state,
readiness or permission to execute. Spoke state remains open native vocabulary;
unknown states and absent observations must not become a fabricated healthy
state or current timestamp.

## Required adapter semantics before serving

The request preserves the existing summary default limit of 100 and explicit
range 1..200, native ID substring search, opaque cursor and optional Space
selection. Native search normalization and cursor fingerprinting must be reused;
a changed search/Space cannot silently reinterpret a cursor. Totals count only
records remaining after current caller authority, Space membership and search
filtering. An empty Space selector does not bypass authorization.

The adapter must reuse authenticated original/effective identity and native
Environment-read decisions, including administrator behavior. It must preserve
unconfigured/unknown/forbidden Space outcomes, invalid limit/cursor refusals,
empty pages and missing report evidence. No request body principal may replace
transport identity. Existing native HTTP statuses must map to typed RPC errors
without an automatic retry through another transport.

Before registration, #392 needs parity tests over authorized fields, caller
filtering, Space selection, pagination and refusals, including proof that an
unregistered method is not served. #394 owns any explicit generated caller and
REST rollback mode. No Plan, approval, lease, Agent or packaging authority is
introduced by this read contract.

## Verification

Pinned buf lint and FILE breaking checks, generated Go/TypeScript drift checks,
and native reference generation qualify the declaration only. They do not
qualify an Environment RPC handler, deployed identity, live caller, or runtime
readiness. Keep that distinction in issue acceptance and release notes.
