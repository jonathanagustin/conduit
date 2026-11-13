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

## Run the Temporal rehearsal

Start three terminals in `web/`:

```sh
pnpm temporal:server
pnpm temporal:worker
CONDUIT_AGENT_MODE=temporal-demo pnpm dev
```

The developer service uses localhost:7233, a Temporal UI at
http://localhost:8233, and SQLite under `.cache/temporal/` at the repository
root. First use downloads the SDK-managed Temporal CLI and needs network access.
This does not establish offline readiness. Stop services with Ctrl+C. The worker
and client accept TEMPORAL_ADDRESS and TEMPORAL_NAMESPACE for another service.

Send a proposal to start a real workflow with fixture activities. Approve or deny
in the tool card; Stop requests cancellation. Send `fail-validation` for the
failure path. Copy the displayed workflow ID, reload, and send
`resume <workflow-id>` to reconnect to durable workflow state, without restoring
conversation history. Approval expires after 24 hours; retries stop after three
attempts. Neither rehearsal mode runs commands or cluster operations.

```sh
pnpm test:temporal
CONDUIT_TEMPORAL_DEMO_URL=http://localhost:3000/console/ node --test server/temporal-browser.test.mjs
```


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
approval, with backend state authoritative. Temporal now coordinates the optional rehearsal with fixture activities and
worker-recovery tests. Real Kubernetes execution, Hub integration, UDS/Zarf
packaging, authenticated remote hosting and disconnected operation remain
unimplemented. See the job-alignment note for the next acceptance gates.
