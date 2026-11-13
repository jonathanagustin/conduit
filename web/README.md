# Conduit assistant

The operator console is a local packaging assistant. It is an
[assistant-ui](https://www.assistant-ui.com/) chat, served under `/console/`
from the Hub binary, and a Node bridge that speaks the
[Agent Client Protocol](https://agentclientprotocol.com/) to Codex.

The bridge is a local process. It is not a hosted Codex service, and a tool
approval in the chat is not a Conduit deployment approval.

## Run it locally

Prerequisites:

- Node and pnpm from `mise.toml` (`node` 24.22.0, `pnpm` 11.28.6).
- A Codex login. The pinned adapter is `@agentclientprotocol/codex-acp` 2.6.0,
  which starts Codex app-server itself. Authenticate with one of:
  - `CODEX_API_KEY` or `OPENAI_API_KEY` in the environment, or
  - an existing Codex login. By default the bridge gives the agent its own
    `CODEX_HOME` under the workspace so it does not load your personal MCP
    servers. Point `ASSISTANT_CODEX_HOME` at your Codex home only when you
    accept that this also loads that home's configuration.

```bash
pnpm --dir web install --frozen-lockfile
pnpm --dir web dev
```

`pnpm dev` starts the bridge on `127.0.0.1:8787` and Vite on
`http://127.0.0.1:5173/console/`. The dev server proxies `/api/assistant` to
the bridge. Open that URL.

The Hub embed at `/console/` is the same UI. It only reaches Codex when the
bridge is running on the same machine and something proxies `/api/assistant`
to it. Without the bridge the page says so.

## Environment

| Variable | Meaning |
| --- | --- |
| `CODEX_API_KEY` / `OPENAI_API_KEY` | API-key auth for the adapter. The key is not sent to the browser. |
| `ASSISTANT_CODEX_HOME` | Codex home directory. Default: `<workspace>/.codex`, created on start. |
| `ASSISTANT_WORKSPACE` | Directory passed as the ACP session `cwd`. Default: a directory under the system temp dir. |
| `INITIAL_AGENT_MODE` | Codex mode. Default `workspace-write`. `agent-full-access` is refused. |
| `CODEX_PATH` | Optional Codex binary. Leave unset to use the binary the adapter bundles. The `codex` pin in `mise.toml` is the developer CLI and is not what the bridge spawns. |
| `ASSISTANT_BRIDGE_PORT` | Bridge listen port. Default `8787`. Bound to `127.0.0.1` only. |
| `ASSISTANT_ACP_COMMAND` | Replacement agent executable. Tests point this at `src/server/fake-agent.mjs`. |
| `ASSISTANT_ACP_ARGS` | JSON array of arguments for that executable. |

The bridge does not register client filesystem or terminal methods, and it
passes an empty MCP server list. Tool calls Codex makes are shown in the
thread and wait for an explicit allow or reject. Disconnect, cancel, an
unknown option, a second answer, or an answer for a different run all fail
closed.

## Commands

```bash
pnpm --dir web dev          # Vite and the ACP bridge
pnpm --dir web bridge       # bridge only
pnpm --dir web test:unit    # protocol and bridge tests, using the fake agent
pnpm --dir web build        # typecheck and emit the embedded bundle
```

`pnpm build` writes `internal/consoleui/dist`. A production Hub build has to
regenerate that directory; `scripts/verify-console-assets.sh` rejects drift.

## What the previous console did

The Blueprint console (fleet, plans, products, promotions, change requests,
notifications, imports, exports, teams, risk) is no longer served. The Hub
APIs those screens called are unchanged. The tenant bootstrap contract is
unchanged: the assistant still refuses to start unless the Hub rendered
`x-conduit-console-bootstrap`.
