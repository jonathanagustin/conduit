# Plan index pagination contract

Status: implemented and executable, 2026-08-09.

`GET /v1/plans` reads candidate records, fails closed on every record whose
Change Request cannot be authorized, and only then applies Environment, status,
Installation, and bounded search filters. Counting and opaque keyset pagination
therefore cannot reveal unauthorized Plans. Lease tokens are redacted before
projection.

The stable key is newest-first `(createdAt, id)`. Its cursor is versioned,
base64url encoded, bounded to 2,048 bytes, and bound to all filters plus the
projection view. Traversal uses a strict seek, so the last Plan on one page
cannot repeat on the next. This is current-state keyset traversal, not snapshot
isolation.

`view=summary` returns ID, Environment, status, step/approval counts,
timestamps, and revision. It defaults to 100 and accepts at most 200. The
compatibility `view=full` retains nested steps, desired state, execution,
diagnostics, and cancellation but is capped at 20. Exact detail remains at
`GET /v1/plans/{id}`.

The console uses only summaries for both the global and Environment-scoped
indexes. Submitted search and cursor are URL-owned, mobile uses definition-list
records, and `PlanIndexPom` owns stable structural locators without replacing
role, text, URL, axe, or overflow assertions.

The synthetic 100-step/100-task fixture measured 47,842 bytes full versus 276
bytes summary. This describes the fixture, not production traffic.

```sh
go test -race ./internal/controlplane -run 'TestPlanIndex|TestPlanSummary'
pnpm --dir web exec playwright test src/test/e2e/plans.spec.ts src/test/e2e/catalog.spec.ts src/test/e2e/accessibility.spec.ts
```
