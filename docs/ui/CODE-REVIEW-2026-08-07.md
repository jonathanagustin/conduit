# Control-plane console review (final pass)

## Findings (ranked by severity)

### [P0] Change-request review actions are wired to an impossible request contract — `platform/web/src/api/resources.ts:214-218`
The UI sends both `rationale` and `reason` for **every** action. The server explicitly rejects unknown JSON fields (`DisallowUnknownFields`) and each endpoint expects a different body: approve/reject accept only `rationale`, cancel accepts only `reason`, and reopen requires an empty body (`platform/internal/controlplane/http.go:1556-1563`, `1584-1591`, `1617-1619`, `1642-1644`, `2495-2497`).
Result: approve/reject/cancel/reopen can all fail with 400 on a real backend even when the UI allows the action.

### [P1] Environment Commands tab is decoding the wrong payload shape, so non-empty data will be misreported — `platform/web/src/api/catalog.ts:59-66`, `platform/web/src/features/environments/EnvironmentDetailPage.tsx:241-247`
Frontend expects a flat `EntityCommand` object (`installationId`, `type`, `status`, `createdAt`, `requestedBy`), but `/v1/environments/{id}/commands` actually returns an array of `PlanRecord` values (`platform/internal/controlplane/http.go:1392-1398`), where command details are nested under `command` (`platform/internal/controlplane/lifecycle.go:61-79`, `129-137`).
In practice this yields wrong columns (`Entity`/`Type` become `—`, and `Status` is plan status, not command type/status), i.e. wrong operational data shown.

### [P1] Promotions graph can show the wrong transition state when a product has multiple promotion records — `platform/web/src/features/promotions/PromotionsPage.tsx:33-39`, `platform/web/src/features/promotions/ReleaseChannelGraph.tsx:34-35`
The page groups all promotions by product, then chooses one transition per channel with `find(...)`. Promotion records are immutable historical evidence (`platform/internal/controlplane/promotion.go:189-209`), and list endpoints return all records sorted by ID, not “latest per channel” (`platform/internal/controlplane/http.go:1804-1814`, `platform/internal/controlplane/repository.go:734-746`).
Inference: once multiple versions/evaluations exist on the same channel boundary, the graph can surface an arbitrary older record and imply the wrong current blocking state.

### [P2] Fleet filter vocabularies diverge from backend enums, so filtering can silently lie — `platform/web/src/api/types.ts:53-56`, `platform/web/src/features/fleet/FleetPage.tsx:85-107`
Examples: backend rollout statuses include `IN_GRACE_PERIOD`, `BLOCKED`, `RECALLED` (`platform/internal/controlplane/lifecycle.go:173-178`) but UI filter options do not; backend lifecycle includes `UNKNOWN` (`platform/internal/controlplane/lifecycle.go:184-187`) but UI exposes `RELINQUISHED`/`PENDING` instead.
Operators cannot filter some real failure states and can select some filters that will never match backend data.

### [P2] Consequence panel makes a hidden `/plans/` request even when there is no plan — `platform/web/src/features/changes/ChangeRequestDetailPage.tsx:274-278`, `platform/web/src/api/resources.ts:176`
`Consequence` calls `usePlan(planId)` before checking `planId === ""`. For no-plan requests, it still issues `GET /v1/plans/` and then suppresses that failure by early-returning a callout.
This is unnecessary backend noise and can pollute logs/telemetry while looking “clean” in UI.

### [P2] Unit tests for sorting/search don’t constrain the code that actually renders pages — `platform/web/src/test/unit/logic.test.ts:3-7`, `platform/web/src/features/fleet/FleetPage.tsx:43-55`, `platform/web/src/features/changes/ChangeRequestsPage.tsx:24-28`
`logic.test.ts` tests helper functions in `features/fleet/filter.ts` and `features/changes/order.ts`, but production pages re-implement the logic inline and do not import those helpers (same for outstanding-requirement count at `ChangeRequestsPage.tsx:46-48`).
So the mutation-focused unit suite can stay green while real UI behavior drifts.

## Test quality misses (specific)

- `change-requests.spec.ts` only asserts that a POST happened, not body contract or resulting state (`platform/web/src/test/e2e/change-requests.spec.ts:104-115`); harness `post(...)` has no body assertions (`platform/web/src/test/e2e/harness.ts:71-73`), which is why the P0 payload bug survives.
- Commands coverage asserts only the empty state (`platform/web/src/test/e2e/catalog.spec.ts:102-113`); there is no non-empty `/commands` fixture to validate field mapping, which is why the P1 projection bug survives.
- No test asserts “no plan id => no plan request”; current test checks text only (`platform/web/src/test/e2e/change-requests.spec.ts:206-211`), allowing hidden backend chatter.

## Missing for production-readiness (not polish)

- **API contract tests at the UI boundary:** request/response shape checks against real handler contracts are missing; current suite stubs too loosely and missed both the review write-path break and commands read-path mismatch.
- **Enum/source-of-truth synchronization:** status filter vocabularies are hard-coded in UI without contract pinning to backend enums, creating silent operator misfilters as statuses evolve.
- **Mutation conflict/capability model:** review actions lack explicit revision/capability handling in the UI path, so operators get late generic failures instead of preflighted action availability and conflict-aware outcomes.
