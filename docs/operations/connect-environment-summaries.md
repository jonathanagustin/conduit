# Native Environment summary read

The Hub explicitly registers the POST method
`conduit.environments.v1.EnvironmentReadService/ListEnvironmentSummaries` as
an operator-filtered read. It authenticates before protobuf decoding and uses
the original resolved principal with the native Environment visibility query.
Readable Space selection and membership narrow that query; neither grants
Environment authority. REST remains enabled. The CLI's explicit summary mode
can select Connect; other #394 caller migration remains open. No REST
deprecation or automatic transport fallback is enabled.

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

The adapter's parity tests cover authorized fields and totals, caller
filtering, Space selection, pagination and refusals, including proof that an
unknown method is not served. #394 owns any explicit generated caller and
REST rollback mode. No Plan, approval, lease, Agent or packaging authority is
introduced by this read contract.

## Explicit CLI summary view

`conduit environment list` still returns full records over REST. The narrow RPC
cannot replace that output: omitted configuration must not be presented as
empty configuration. Select the summary explicitly:

```sh
conduit environment list --view summary
conduit environment list --view summary --read-transport connect --search prod
```

REST is the default transport in either view. `--read-transport connect` and
`--search` require `--view summary`; unsupported combinations fail before a
request. The existing `--space-id` selector applies to both transports. Summary
output contains narrow identities, optional report counts/times, Spoke liveness
and the filtered total. JSON, YAML, table and CSV use the same narrow values.
Generation is opaque, missing reports remain absent, and missing times are not
replaced by the current time. Configuration and Entity bodies do not enter the
summary output even when REST supplies a wider native record.

The generated client uses the configured operator HTTP client and its original
credentials, trust, deadline, cancellation, redirect refusal and response-size
bound. It follows native cursors with the same search/Space, rejects repeated
cursors and duplicate identities, validates response envelopes/counts/times and
emits no partial output when a page fails. Errors are never retried through the
other transport. This changes no command, Plan approval or execution authority.

## Verification

Pinned buf lint and FILE breaking checks, generated Go/TypeScript drift checks,
and native reference generation qualify the declaration. Actual Hub REST/RPC
parity tests additionally exercise the registered adapter. Neither establishes
deployed identity, live caller migration or runtime readiness. Keep that
distinction in issue acceptance and release notes.
