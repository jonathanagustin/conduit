# Conduit console

The Hub serves this Vite application at `/console/`. The operator workbench
includes an assistant rail whose turns go to `POST /v1/assistant/turns` on the
Hub. There is no Next application or local hosted-model chat route.

Use the repository's pinned Node and pnpm versions (`mise install`):

```bash
cd web
pnpm install --frozen-lockfile
pnpm dev
```

Vite serves the development console at `http://localhost:5173/console/`.
The Hub must be configured separately for live reads and model-backed turns;
setting an API key in a browser starter environment does not configure the Hub.
`pnpm build` writes the embed to `internal/consoleui/dist` by default.

`pnpm check` checks dependency policy, the pinned toolchain, generated event
validation, source-module reachability, navigation documentation and lint.
`pnpm typecheck`, `pnpm test:unit`, and `pnpm test:e2e` run the additional
type, unit and browser gates. `pnpm docs:navigation` regenerates the area/tab
inventory after edits to `src/console/operator/navigation.js`.

See [console composition](../docs/ui/console-composition-trees.md) for the active
entry points, retained component boundaries and the source reachability check.
The assistant-ui thread and its imports remain; the unmounted registry catalog
and template model route have been removed. The loopback workspace ACP bridge
in `src/server` is a separate Hub-mediated service.

## What the console writes to the Hub

`src/console/Workbench.tsx` mounts the operator engine in
`src/console/operator/engine.js`. Its native read model lives in `live-hub.js`.
The retained `src/console/data/hub.ts` adapter serves assistant cards, the explicit
packaging detail route and its focused tests; it is not the workbench read cache.

The delivery journey's writes live in `src/console/operator/delivery-actions.js` (request shapes and state) and `delivery-journey.js` (panels). Each one shows a confirmation with the exact request, disables its controls while the request is in flight, reports only what the Hub answered, and then reads the Hub indexes again.

| Action | Where | Request |
| --- | --- | --- |
| Propose a plan | Releases / Environments | `POST /v1/plans` with the next desired state; title and reason in `X-Change-Title` and `X-Change-Description` |
| Approve a plan | Releases / Plans, Change requests / Approvals | `POST /v1/plans/{planID}/approvals` `{rationale}` |
| Reject a plan | same | `POST /v1/change-requests/{changeRequestID}/reject` `{rationale}` when the plan has a change request, `POST /v1/plans/{planID}/cancel` `{reason}` otherwise |
| Evaluate a promotion | Releases / Promotion | `POST /v1/promotions/evaluations` `{release, fromChannel, toChannel, policy, evidence}` |
| Approve or reject a change request | Change requests | `POST /v1/change-requests/{changeRequestID}/approve` or `/reject` `{rationale}` |

Limits that are the Hub API's, not omissions in the table:

- A proposal edits a desired state the Hub already accepted (the newest plan of that environment) and moves one installation to a release the catalog lists. The first desired state of an environment is proposed with `conduit plan propose`.
- `POST /v1/plans` refuses an invalid document with only a count. The console then asks `POST /v1/validate`, which records nothing, for the rule that failed.
- A promotion is evaluated only across a stage the Hub already holds a policy for: one declared in a loaded desired state, or the policy of a recorded evaluation. The console does not author a promotion policy.
- The author of a change is shown why they cannot approve it and is offered no live button. The Hub enforces the rule either way.
- The Space selector in the top bar sends `spaceId` to `GET /v1/environments`, the only index route that reads it, and narrows installations, plans and change requests to the environments that came back. Products, promotions, channels, packaging, bundles and audit are not Space-scoped.
- Approval policies, teams and resource grants are read-only in the console. Creating a Space, publishing a Product Release, and the suppressions, labels, export pipelines, commands, modules and artifact scans surfaces are CLI-only.

`src/console/operator/planning-flow.js` keeps the planning session id in the URL as `#s01-intake/<sessionId>` and reads the session and its events again on load. Stop aborts the stream in the browser. The Hub has no route that cancels a planning run or lists planning sessions, so Stop does not cancel the run on the Hub and there is no session list; the page says both. It lists the caller's app generations from `GET /v1/app-generations`.

`scripts/simulate-console.sh release-write-journey.spec.ts` runs the propose, approve, blocked-promotion and Space checks in `src/test/live/` against a real Hub.
