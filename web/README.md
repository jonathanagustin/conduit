# Conduit assistant

Goal: a working Conduit assistant shell using default assistant-ui Elements,
Codex as the agent runtime, and minimal custom frontend code.

## Run a simulation

```sh
cd web
pnpm install --frozen-lockfile
CONDUIT_AGENT_MODE=demo pnpm dev
```

Open http://localhost:3000/console/. Send any prompt to see streamed fixture
validation results and an approval request. Approve, deny, or stop the turn.
The persistent banner and transcript label this as a simulation: no model calls,
commands, tests, scans or deployments are performed. `pnpm build` followed by
`CONDUIT_AGENT_MODE=demo pnpm start` runs the production preview.

## Connect Codex

```sh
# Authenticate in the server's normal Codex environment first.
codex login
pnpm dev
```

The custom Node server binds only to 127.0.0.1. The browser opens the same-origin
`/console/acp` WebSocket; a bridge starts one pinned `codex-acp` 2.6.0 process per
connection, and that adapter starts Codex App Server. Credentials stay on the
server. `CONDUIT_AGENT_WORKSPACE` selects the workspace (default: repository root);
`PORT` changes the listening port. Export these environment variables before
starting the custom server; it does not load them from `.env.local`.

The bridge checks Origin and loopback peer, pins the workspace, strips client
MCP server commands, refuses client authentication/mode/config mutation, starts
in read-only mode, limits messages, and terminates the child on disconnect.
Permission requests flow through assistant-ui's tool approval UI. This is a
local developer preview, not a multi-user authenticated gateway. No backend
deployment approvals or Conduit tools are wired. Agent permission is separate
from Conduit's deployment authorization.

The agent owns the ACP transcript. Page reload/disconnect does not promise durable
thread restoration; this bridge terminates the owning process. Session persistence,
remote identity, isolation, reconnect recovery and Conduit tool integration remain
follow-up work. The thread list uses upstream behavior, not a database-backed inbox.

## Elements and provenance

The official assistant-ui CLI 0.0.134 created this default Next.js starter from
`assistant-ui/assistant-ui@995487b02b19dd739389246898181a0284099d2b`,
`templates/default`. Default styling is retained. The available web Elements and
runtime bindings are installed from the base-nova registry;
`elements-manifest.json` lists the 148 relevant entries. These are reusable
components, not 148 live backend features. Provider-specific bindings need the
matching runtime; the AI SDK context binding is retained but not used by Codex.
The unrelated Eve application entrypoint was excluded.

The ACP React adapter under `lib/acp` is vendored from that same immutable source
with its MIT license because the upstream `@assistant-ui/acp` package is
unpublished. Its compile-time version macro is replaced by a local version
constant; no sibling checkout or mutable source link is needed. See its README.
The published Codex adapter is separate:
https://github.com/agentclientprotocol/codex-acp.

## Verification

```sh
pnpm lint
pnpm typecheck
pnpm build
pnpm test:bridge
# With a running demo preview and Playwright Chromium installed:
CONDUIT_DEMO_URL=http://localhost:3000/console/ node --test server/demo-browser.test.mjs
```

Bridge fixtures verify initialization, streamed updates, permission decisions,
cancellation, workspace pinning, blocked mode changes, origin rejection and
malformed-message closure. Browser verification covers streamed demo output,
approval, denial and Stop; it skips unless CONDUIT_DEMO_URL is set.
The real Codex ACP 2.6.0 initialize handshake passed. Live inference was not
verified: the environment's Codex credential was rejected. Lint passes with
three upstream redundant-spread warnings in the vendored adapter.

## Migration boundary and next work

This replaces the Blueprint frontend and its tests, not Conduit's Go control
plane. The Hub still serves its previously committed `internal/consoleui/dist`
assets. The new Next.js app is not a static replacement for that embed. Hosting
and proxy configuration, Hub authentication/tenant bootstrap integration, and
legacy Vite/Blueprint/screen-contract CI gates must be migrated before promotion.
The Go race/vet checks were attempted but unavailable: this environment's
`/usr/bin/go` is not the Go compiler and rejects `test` and `vet`.

Next real slice: authorized proposal → validation evidence → explicit deployment
approval, with backend state authoritative. Temporal is requested follow-up:
first define workflow ownership against Conduit's existing durable leases, then
prove worker restart, bounded retry, cancellation and approval recovery for one
workflow. Temporal, Kubernetes execution, UDS/Zarf packaging and disconnected
operation are not implemented by this UI/ACP change.
