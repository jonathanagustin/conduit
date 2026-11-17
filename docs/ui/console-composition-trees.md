# Console composition and component ownership

This describes the shipped Vite console at `/console/`. The earlier React
Router screen catalog and React Query shell have been removed; their former
`App.tsx`, `AppFrame`, `NavigationRail` and `useIndexQuery` are not runtime
components. Historical screenshots remain evidence of the state they captured.

## Entry and composition

```text
web/index.html
└─ src/embed/main.tsx
   ├─ installClientErrorHandlers (one structured browser error record)
   ├─ loadRuntimeBootstrap (validated tenant/brand meta document)
   └─ ConsoleErrorBoundary
      └─ Providers (BrowserRouter, basename=/console/)
         └─ Routes
            ├─ packaging-runs/:runId → PackagingRunPage
            ├─ notifications → NotificationsRoute → Assistant
            └─ * → Assistant
               └─ AssistantRuntimeProvider (useWorkbenchRuntime)
                  ├─ ContextToolUIs (assistant tool-result cards)
                  └─ Workbench
                     ├─ mountOperator → operator/engine.js
                     │  ├─ shell, area navigation, tabs and inspectors
                     │  ├─ native read model → live-hub.js
                     │  └─ feature renderers in operator/*
                     └─ React portal into the engine's assistant slot
                        └─ ConsoleErrorBoundary → thread.aui.tsx
```

A bootstrap failure renders the configuration-unavailable alert instead of a
partially mounted console. In development, `/console/dev/components` imports
`src/console/gallery.tsx`; that gallery is not a production route.

## Component boundary

The existing operator workbench is the composition owner. New navigation and
feature integration belong in its area table and renderers rather than a new
application shell. Shared operator markup and tokens live under
`src/console/operator/`; use the established primitives there for operator views.

The assistant thread is a React leaf using the retained assistant-ui components
under `components/assistant-ui/`. `src/console/components/{base,domain}` supplies
Blueprint-based assistant tool cards and the development gallery.
`src/components/` still serves the explicit `PackagingRunPage` route. Those
mounted consumers remain; the application does not publish a component-library
package (`web/package.json` is private and exports no module API).

The unmounted assistant registry catalog and Next starter have been removed.
`src/console/Assistant.tsx` and `src/styles/assistant.css` are the active runtime
and styles moved out of the old Next directory. This cleanup does not complete
#825's single-component-system migration or token-only light-theme acceptance.
Those require reconciling the mounted assistant cards and packaging route with
the operator primitives and validating their light-mode presentation.

## State and transport

The operator engine keeps its loaded native read model and view state in its
mount. `live-hub.js` performs authorized reads and integrates `LiveSubscription`;
SSE frames invalidate that model and never become business state directly.
See [Console live updates](../operations/console-live-updates.md).

Area and screen selection use the hash. The source table is
`operator/navigation.js`, imported by the engine and by the
[navigation documentation generator](../../web/scripts/console-navigation.mjs).
The generated [area/tab inventory](console-navigation.md) is checked by
`pnpm --dir web check:source`.

The assistant runtime sends turns through `POST /v1/assistant/turns` on the Hub.
Workspace ACP traffic uses the Hub-mediated workspace routes; the browser has
no direct route to the local coding-agent process. `src/server/` is the
separate loopback ACP bridge, not a model route in the browser build.

## Keeping the source tree bounded

`web/scripts/check-unused.mjs` walks parsed static imports, exports, literal
dynamic imports and literal `new URL` module references from the browser entry,
bridge and development entries, tooling, and tests. It also checks the retained
assistant components, hooks and utilities, so an unreachable group cannot keep
itself alive by importing its own members. Generated transport files under
`src/api/gen/` stay complete and have separate generation drift checks.

Tests are roots because test harnesses and independently tested adapters are
intentional consumers. Passing this check establishes import reachability, not
that every exported symbol or branch runs in the production browser. New
nonliteral module loaders need an explicit check update; do not use them to
hide an unmaintained component catalog.

## Cleanup evidence (2026-11-16)

The #825 cleanup was checked against `bc27fd86b9fa8eef484b1e4d9e57ec400973aee6`.
The private web package exposes no public module API; a repository search found
no `import.meta.glob` or `require.context` loader and only the literal gallery
import in the browser. The parsed graph identified 145 unreachable registry,
helper and legacy-settings modules, which were removed along with the named
unused hooks, starter route, Next configuration and unreachable engine fixtures.
The active assistant import closure, Blueprint cards, explicit packaging route,
bridge fixtures and complete generated protocol sources were retained.

The cleanup passed the 692-test unit suite, all 51 Chromium browser cases,
TypeScript, `pnpm check` (0 errors, 193 warnings), and the console boundary,
contract, token and CSS-variable gates. These are local fixture checks. They do
not establish the remaining light-theme or component-unification acceptance.
