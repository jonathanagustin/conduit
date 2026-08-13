# Bounded Overview projection

`GET /v1/overview` answers the operator's first question—what needs attention
now—from one authorization-filtered Hub snapshot. It replaces four independent
browser reads without becoming an authorization boundary or claiming that a
failed source is empty.

## Shape and bounds

The response has six fixed panels: awaiting approvals, failed Plans, running
Plans, unhealthy Entities, stale reports, and blocked promotions. Every panel
contains:

- `status`: `CURRENT` or `UNAVAILABLE`;
- `count`: the exact count visible to the effective principal when current;
- `items`: at most five narrow, linkable examples.

Stale reports have no item payload because the actionable destination is the
authorization-filtered Fleet. Items omit full Change Requests, Plans, reports,
promotion evidence, lease tokens, credentials, and arbitrary provider errors.
`generatedAt` is exact UTC snapshot time. `staleAfterSeconds` explicitly records
the 300-second observation-age threshold used by the stale panel; the threshold
is never left as an implicit browser assumption.

## Snapshot and failure semantics

The Hub reads Plans once and reuses that snapshot for failed/running Plan
panels and Fleet derivation. Reports, Change Requests, and promotions are each
read once. A failure remains local to its dependent panels: for example, a
report failure makes Fleet panels unavailable while readable Change Requests
remain current. If the HTTP projection itself cannot be read, the client marks
all six panels unknown and never renders “Nothing needs attention.”

Plan, Change Request, and Fleet records retain their existing per-record
authorization checks. Promotions retain the existing platform-read boundary.
An unavailable or forbidden source has zero items only as transport shape; its
`UNAVAILABLE` status means the zero is never interpreted as an exact count.

## Evidence

- `go test -race ./internal/controlplane -run TestOverview`
- The repository-call-count test fixes the successful snapshot at one Plan,
  report, Change Request, and promotion list call.
- `pnpm --dir web playwright test src/test/e2e/overview.spec.ts`
- Browser evidence proves one `/v1/overview` read per refresh, bounded rendering,
  partial-source unknown state, and total-projection failure state.
