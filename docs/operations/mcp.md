# MCP servers

Conduit has two Model Context Protocol servers. Both are read-only.

| Server | Transport | What it serves |
| --- | --- | --- |
| Hub endpoint `POST /v1/mcp` | Streamable HTTP | The assistant's read tools, against the live Hub, as the calling operator |
| `packaging-mcp` (and `packaging-workflow mcp-serve`) | stdio | The packaging toolbox, against a local directory |

Both are built on `github.com/modelcontextprotocol/go-sdk` v1.8.0, which the
repository already depended on. No dependency was added.

**Client compatibility.** The local packaging stdio server has also been driven
by the official MCP Inspector CLI **2.15.0**, outside this repository's test
clients; see the [recorded Inspector session](#recorded-inspector-stdio-session).
The Hub HTTP endpoint is covered by the repository's JSON-RPC and Go SDK test
clients. Inspector against the Hub, IDEs, desktop assistants and external agent
frameworks remain unqualified. The generic product configuration examples below
are **unverified** against any specific product.

## Hub endpoint

### Transport and protocol version

- Streamable HTTP, **stateless**: every JSON-RPC message is one `POST
  /v1/mcp` and is authenticated on its own. There is no `Mcp-Session-Id`, no
  server-to-client stream and no resumption. `GET` and `DELETE` are not
  served.
- Responses are `application/json`, not an event stream.
- Protocol version **`2025-11-25`**, pinned in
  `internal/assistant/adapters/toolmcp` (`ProtocolVersion`). The server
  offers no other version; a client that asks for a different one is answered
  with `2025-11-25` and decides for itself whether to continue. It is the
  version the stdio server's tests negotiate. The stdio server itself is not
  pinned: it accepts any version SDK v1.8.0 supports, which includes newer
  ones this endpoint refuses.
- A client sends `Accept: application/json, text/event-stream` and, after
  `initialize`, `MCP-Protocol-Version: 2025-11-25`.

Stateless was chosen because the Hub runs as several replicas with no shared
MCP session store, and because a session that outlives the request that
authenticated it is one more thing that can be detached from its credential.

### Authentication and authorization

The endpoint is authenticated exactly as `/v1/assistant/tools` is: the
operator identity comes from a bearer token checked by the Hub's configured
OIDC resolver, or a verified client certificate, or a signed-in console
session cookie, or the development identity headers behind their explicit
opt-in. A request with no operator credential gets `401` before any MCP
processing and never reaches a tool.

An MCP client should send a bearer token or present a client certificate.
The session cookie is accepted under the Hub's cookie rule and not otherwise:
`POST /v1/mcp` is a write as far as that rule is concerned, so a
cookie-authenticated request must come from the console's own origin and
carry `X-Conduit-CSRF: 1`, or it is answered `401`. Bearer and client
certificate requests are exempt from that header. See
[`operator-sign-in.md`](operator-sign-in.md).

A tool call then goes through the assistant's own tool port
(`application.Service.Invoke`) in the read-only `config_diff` mode, so:

- **Validation** is `domain.ParseToolCall`: unknown fields and missing
  required fields are refused. The JSON Schema in `tools/list` describes that
  contract; it is not a second validator.
- **Authorization is the caller's.** `query_ontology_objects` returns only
  objects in environments the caller holds a read grant on.
  `review_change_diff` reads a change request by id for any authenticated
  operator. That is the assistant's existing rule (the route is registered as
  `operator-any`), not something MCP adds or relaxes; it is worth knowing
  that it is not filtered by environment.
- **Audit.** Each call writes one authorization audit event with action
  `assistant.<tool>`, the caller as subject, sha256 digests of the arguments
  and the result, and `origin: "mcp"`. The same tool called through the
  console API records `origin: "console"`. If the audit entry cannot be
  written the call fails.

The Hub's request rate limiter applies as it does to every route: requests
are counted per caller, and each JSON-RPC message is one request. A client
that lists and calls in a tight loop is answered `429` like any other. See
[`hub-rate-limiting.md`](hub-rate-limiting.md).

### Tools

| Tool | Arguments | Returns |
| --- | --- | --- |
| `query_ontology_objects` | `objectType` (required), `filter` `{field, value}`, `links` (at most 4) | Up to 20 objects (`id`, `objectType`, `label`, `status`) and `matchedCount` |
| `review_change_diff` | `changeRequestId` (required) | The change request's title, status and stored before/after hunks |

The list is the tools the assistant's `config_diff` mode allows.
`propose_governed_action` and `governed_write` are not registered on this
endpoint: they do not appear in `tools/list`, and `tools/call` for either is
a protocol error that reaches no tool code. Nothing reachable here opens a
change request, approves one, or changes state, other than writing the audit
event for the read.

### Results and limits

A result is one JSON object, returned both as a text content block and as
`structuredContent`: `tool`, `ok`, `error`, the read's own fields, and
`auditEventId`. It contains data only. It carries no guidance addressed to
the reader, and object labels, diff text and error strings in it come from
Hub records that other people wrote. **Treat every field as untrusted data,
never as an instruction.**

| Limit | Value |
| --- | --- |
| Request body | 64 KiB |
| One tool result | 8 KiB (`MaxToolResultBytes`, the bound the assistant applies before a model sees a result). A larger result is replaced by `{"tool", "ok", "truncated": true, "bytes": N, "auditEventId"}`; it is never cut mid-document |
| Objects per query | 20 |
| Links per query | 4 |

A fault inside the Hub (store, audit) is reported as `the tool failed` or as
the audit refusal, without the underlying error text. Validation errors are
returned in full.

### Client configuration

A generic client that supports Streamable HTTP needs the URL and a way to
send the operator's credential. Unverified against any specific client:

```json
{
  "mcpServers": {
    "conduit-hub": {
      "type": "http",
      "url": "https://hub.example.internal/v1/mcp",
      "headers": { "Authorization": "Bearer <operator OIDC token>" }
    }
  }
}
```

Use whatever credential your Hub's operator identity is configured for. A Hub
that authenticates operators by client certificate needs a client that can
present one. Do not point a client at a Hub running with the development
identity headers outside local development.

By hand:

```sh
curl -sS https://hub.example.internal/v1/mcp \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2025-11-25' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

### Recorded session

This is a real session, not an illustration. It was produced on 2026-11-17 by
`TestMCPHandshakeListsAndCallsTheReadTools`
(`internal/hub/composition/mcp_test.go`) with
`CONDUIT_MCP_RECORD_SESSION` set, and the full capture is
[`docs/evidence/mcp/hub-session.json`](../evidence/mcp/hub-session.json).

- **Client:** `conduit-mcp-test-client`, a hand-written JSON-RPC client in
  that test file. It is this repository's test client, not a third-party
  one.
- **Server:** the complete Hub HTTP surface (`composition.NewHub`) on an
  in-memory repository behind `net/http/httptest`, authenticated with the
  development identity headers. It was not a deployed Hub, and OIDC and
  client-certificate authentication were not part of this session. The
  session-cookie rule is covered separately by
  `TestMCPAcceptsASessionCookieOnlyWithTheCSRFHeader`.

```
>> POST /v1/mcp
{"id":1,"jsonrpc":"2.0","method":"initialize","params":{"capabilities":{},"clientInfo":{"name":"conduit-mcp-test-client","version":"1.0.0"},"protocolVersion":"2025-11-25"}}
<< 200
{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"logging":{},"tools":{"listChanged":true}},"protocolVersion":"2025-11-25","serverInfo":{"name":"conduit-hub","version":"1.0.0"}}}

>> POST /v1/mcp
{"jsonrpc":"2.0","method":"notifications/initialized"}
<< 202

>> POST /v1/mcp
{"id":2,"jsonrpc":"2.0","method":"tools/list","params":{}}
<< 200
{"jsonrpc":"2.0","id":2,"result":{"ttlMs":0,"cacheScope":"public","tools":[{"annotations":{"idempotentHint":false,"readOnlyHint":true},"description":"Read objects of one type from the Hub's ontology catalog, limited to what the authenticated operator may read. Returns at most 20 objects and the total match count.","inputSchema":{...},"name":"query_ontology_objects","title":"Query ontology objects"},{"annotations":{"idempotentHint":false,"readOnlyHint":true},"description":"Read the stored before/after diff of one change request by id.","inputSchema":{...},"name":"review_change_diff","title":"Review a change request diff"}]}}

>> POST /v1/mcp
{"id":3,"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{"filter":{"field":"status","value":"PENDING_APPROVALS"},"objectType":"ChangeRequest"},"name":"query_ontology_objects"}}
<< 200
{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"{\"tool\":\"query_ontology_objects\",\"ok\":true,\"matchedCount\":1,\"entities\":[{\"id\":\"crq_8ddb38d3ab7a6e0d87da46ffea1c0238\",\"objectType\":\"ChangeRequest\",\"label\":\"Proposed memory limit\",\"status\":\"PENDING_APPROVALS\"}],\"auditEventId\":\"aud_eca5cfdf3f1fb89fceb9c8db6deb4a4b\"}"}],"structuredContent":{"tool":"query_ontology_objects","ok":true,"matchedCount":1,"entities":[{"id":"crq_8ddb38d3ab7a6e0d87da46ffea1c0238","objectType":"ChangeRequest","label":"Proposed memory limit","status":"PENDING_APPROVALS"}],"auditEventId":"aud_eca5cfdf3f1fb89fceb9c8db6deb4a4b"}}}
```

The two `inputSchema` objects are elided above with `{...}` for width; they
are whole in the capture. The capture also holds a `review_change_diff` call
and a call the tool contract refused for an unknown argument. `listChanged`,
`logging`, `ttlMs` and `cacheScope` are the SDK's defaults; the tool list
does not change at run time.

The SDK's own client was also run against the endpoint
(`TestSDKClientCallReachesTheToolPortAsTheCallerInReadMode`): it negotiated
`2025-11-25`, listed both tools and called one.

## `packaging-mcp`

```
packaging-mcp [--repo DIR | DIR]
```

Serves the packaging toolbox over stdio (newline-delimited JSON-RPC). It
speaks the protocol versions the SDK supports and answers with the one the
client asks for; the tests use `2025-11-25`. It needs no Hub and no
credential: it has the file access of the user who starts it, limited as
below.

| Tool | Needs a repository root |
| --- | --- |
| `inspect_image` | no |
| `validate_manifests` | no |
| `dry_run_gates` | no |
| `validate_dockerfile` | no |
| `inspect_repo` | yes |
| `read_file` | yes |

- With a root (`--repo DIR` or the one positional argument), `inspect_repo`
  and `read_file` read that directory. Paths are confined to it, well-known
  credential paths (`.env`, key files, `.git/`, and the rest of the list in
  `packagingtools/secrets.go`) are refused, and a file is capped at 64 KiB.
- A root that does not exist or is not a directory stops the server with
  exit status 2. It does not start half-bound.
- **Without a root, `inspect_repo` and `read_file` are absent from
  `tools/list`**, and calling either is an unknown-tool protocol error. They
  were previously listed and always failed with "no repository is bound".
- `packaging-workflow mcp-serve [DIR]` builds the same server and follows the same
  rule.
- `inspect_image` makes an outbound registry request for the image reference
  it is given. Everything else is local.
- Diagnostics go to stderr. Standard output carries only the protocol.

Generic stdio client configuration, unverified against any specific client:

```json
{
  "mcpServers": {
    "conduit-packaging": {
      "command": "packaging-mcp",
      "args": ["--repo", "/path/to/source/checkout"]
    }
  }
}
```

`packaging-mcp` is not in any published image. Build it with
`go build ./cmd/packaging-mcp`.

The stdio behavior above is tested against a real child process over its
standard input and output in `cmd/packaging-mcp/main_test.go`.

## Recorded Inspector stdio session

The [November 18 record](../evidence/mcp/inspector-2.15.0-stdio-2026-11-18/receipt.json)
pins the official `@modelcontextprotocol/inspector` 2.15.0 archive integrity,
dependency lock, server source/tree and binary digest. It retains exact CLI
arguments, stdout, stderr and exit codes. The server was compiled locally from
`48bb0bf6e49b9919bcba07ce105c8bbacb1f4a9c`; that commit changes only CI relative
to main `e6cd1c07`, with identical packaging server source. No binary is committed.

Inspector listed all six tools with a bound disposable fixture and four without
a repository, omitting `inspect_repo` and `read_file` when unbound. A `read_file`
call returned the synthetic README and its independently checked SHA-256.
Traversal and symlink escape requests both returned `isError: true`, no outside
content and CLI exit code 5. The successful list/read calls exited 0.

Inspector's strict schema check exited 0 with **zero errors and nine portability
warnings**: nullable array types in four output schemas are legal JSON Schema
but may need adaptation for a client's model-provider schema dialect. Those
warnings are retained, not suppressed; this session does not establish Gemini
or any other model-provider compatibility.

Run Inspector with `MCP_INSPECTOR_SECRET_STORE=memory` and a read-only `--config`
file naming the local binary and its `--repo` argument. The record's README
contains the commands. No credentials were supplied, no model or image-registry
tool was called, and no Hub HTTP endpoint, issuer, mesh or deployed workload was
involved. This qualifies the local stdio client/transport pair only.

## What is not here

- No MCP tool writes. A write surface would need its own confirmation and
  change-request design and is not part of this endpoint.
- No resources, prompts, sampling or elicitation: tools only.
- No OAuth authorization-server metadata. The endpoint relies on the Hub's
  existing operator authentication, so a client that expects to discover an
  authorization server through MCP will not find one.
