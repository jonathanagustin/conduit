# Platform console

The control-plane console described by `../docs/ui/CONTROL-PLANE-UI-PLAN.md`.
Built as static assets and embedded in the Hub binary under `/console/`, served
same-origin with the APIs.

## Decisions this scaffold makes

**Blueprint 6.18.0.** The plan's V3 evidence grade cites `@blueprintjs/core`
6.18.0, icons 6.13.0, select 6.3.4 and table 6.2.4, and those are the pins here.

**Ported, not shared, primitives.** `packages/foundry/packages/foundry-ui` has
the component vocabulary this console wants -- `AppFrame`, `AppHeader`,
`SideNavigation`, `ResourceTable`, `ObjectDetails`, `DataSurface`, `FormDialog`,
`SettingsLayout` -- but it peers Blueprint **5.19.1** and React **18.2.0**, and
its other consumers (`workspace-app`, `gotham`) are on that major too. Those
components are therefore ported into this package rather than depended on, and
foundry-ui is left alone so its existing consumers keep building. Ported files
must keep the provenance note they arrive with.

That choice is also what makes this package's location work: nothing here
resolves a workspace sibling, so `platform/` stays a self-contained repository
that builds its own console, and the Hub embed stays a local file copy rather
than a cross-repository build step.

**No client-side store.** Server state lives in the query cache, route state in
the URL, form state local. Add a store when a workflow proves it is needed.

**Polling, not sockets.** Request/response REST with bounded, visibility-aware
polling. Server-sent events only if measurement shows polling is inadequate.

## The base path is load-bearing

`vite.config.ts` sets `base: "/console/"` and the router sets a matching
`basename`. Both are required: with the default base the app works under
`vite dev` and 404s on every asset once embedded in the Hub.

## Commands

    pnpm install
    pnpm dev        # http://localhost:5173/console/, proxying /v1 to the Hub
    pnpm build      # typecheck, then emit dist/
    pnpm typecheck

## Not done yet

`dist/` is not embedded in the Hub binary. That is a Go change -- an embed
directive and a route mounting the assets under `/console/` -- and belongs with
the composition root, not here.
