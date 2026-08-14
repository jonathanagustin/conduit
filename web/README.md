# Conduit console

The console is built as static assets, embedded in the Hub binary, and served
under `/console/` from the same origin as its APIs. The current UI contracts
are indexed in `../docs/ui/README.md`.

## Decisions this scaffold makes

**Blueprint 6.18.0.** The package pins `@blueprintjs/core` 6.18.0, icons
6.13.0, select 6.3.4, and table 6.2.4.

**Ported, not shared, primitives.** `packages/foundry/packages/foundry-ui` has
the component vocabulary this console wants -- `AppFrame`, `AppHeader`,
`SideNavigation`, `ResourceTable`, `ObjectDetails`, `DataSurface`, `FormDialog`,
`SettingsLayout` -- but it peers Blueprint **5.19.1** and React **18.2.0**, and
its other consumers (`workspace-app`, `gotham`) are on that major too. Those
components are therefore ported into this package rather than depended on, and
foundry-ui is left alone so its existing consumers keep building. Ported files
must keep the provenance note they arrive with.

Nothing here resolves a workspace sibling. Conduit builds its own console and
the Hub embed remains a repository-local build product.

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

`internal/consoleui/handler.go` owns the embed and the Hub composition mounts
the handler. A production build must regenerate `dist/` and run the console
gate so the embedded assets match this source.
