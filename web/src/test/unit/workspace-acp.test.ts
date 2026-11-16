import { describe, expect, it, vi } from "vite-plus/test";

import {
  createWorkspaceAcpClient,
  hubWorkspaceAcpPath,
} from "../../api/workspace-acp.js";

describe("workspace editor Hub ACP client", () => {
  it("confines every editor ACP request to the Hub workspace endpoint", () => {
    expect(hubWorkspaceAcpPath("wsp-alpha")).toBe(
      "/v1/dev-workspaces/wsp-alpha/acp",
    );
    expect(() => hubWorkspaceAcpPath("")).toThrow();
    expect(() => hubWorkspaceAcpPath("../escape")).toThrow();
    expect(() => hubWorkspaceAcpPath("http://127.0.0.1:8787/acp")).toThrow();
  });

  it("checks the pinned ACP protocol version during initialize and streams Hub turns", async () => {
    const calls: { url: string; body: Record<string, unknown> }[] = [];
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.toString()
              : input.url;
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const body = JSON.parse(rawBody) as Record<string, unknown>;
        calls.push({ url, body });
        if (url.endsWith("/acp") && body["method"] === "initialize") {
          return new Response(
            JSON.stringify({
              jsonrpc: "2.0",
              id: body["id"],
              result: { protocolVersion: 1 },
            }),
            { status: 200, headers: { "content-type": "application/json" } },
          );
        }
        if (url.endsWith("/acp") && body["method"] === "session/new") {
          return new Response(
            JSON.stringify({
              jsonrpc: "2.0",
              id: body["id"],
              result: {
                sessionId: "sess-1",
                _meta: {
                  threadId: "wsp-acp-sess-1",
                  branch: "workspace/alice/wsp-1",
                },
              },
            }),
            { status: 200, headers: { "content-type": "application/json" } },
          );
        }
        if (url.endsWith("/acp") && body["method"] === "session/prompt") {
          const sse = [
            `data: ${JSON.stringify({
              type: "permission_request",
              request: {
                requestId: "req-1",
                sessionId: "sess-1",
                threadId: "wsp-acp-sess-1",
                runId: "run-1",
                toolCallId: "call-1",
                title: "write README.md",
                riskTier: "low_confirm",
                status: "PENDING_CONFIRM",
                options: [
                  { optionId: "allow", name: "Allow once", kind: "allow_once" },
                ],
              },
            })}\n\n`,
            `data: ${JSON.stringify({
              type: "rpc_response",
              response: {
                jsonrpc: "2.0",
                id: body["id"],
                result: { stopReason: "end_turn" },
              },
            })}\n\n`,
          ].join("");
          return new Response(sse, {
            status: 200,
            headers: { "content-type": "text/event-stream" },
          });
        }
        if (url.includes("/acp/permissions/")) {
          return new Response(
            JSON.stringify({ ok: true, status: "APPROVED" }),
            {
              status: 200,
              headers: { "content-type": "application/json" },
            },
          );
        }
        throw new Error(`unexpected URL ${url}`);
      },
    );

    const client = createWorkspaceAcpClient({
      workspaceId: "wsp-1",
      branch: "workspace/alice/wsp-1",
      fetchImpl: fetchMock as unknown as typeof fetch,
    });
    const init = await client.initialize();
    expect(init.protocolVersion).toBe(1);

    const session = await client.newSession();
    expect(session.sessionId).toBe("sess-1");

    const seenPermissions: string[] = [];
    const done = await client.prompt({
      sessionId: "sess-1",
      runId: "run-1",
      prompt: [{ type: "text", text: "edit README.md: hello" }],
      onEvent(event) {
        if (event.type === "permission_request") {
          seenPermissions.push(event.request.requestId);
        }
      },
    });
    expect(done.stopReason).toBe("end_turn");
    expect(seenPermissions).toEqual(["req-1"]);

    await client.decidePermission({
      requestId: "req-1",
      sessionId: "sess-1",
      runId: "run-1",
      decision: "confirm",
      optionId: "allow",
    });

    expect(
      calls.every((call) =>
        call.url.startsWith("/v1/dev-workspaces/wsp-1/acp"),
      ),
    ).toBe(true);
  });

  it("fails closed when the Hub reports an unexpected ACP protocolVersion", async () => {
    const client = createWorkspaceAcpClient({
      workspaceId: "wsp-1",
      fetchImpl: (async () =>
        new Response(
          JSON.stringify({
            jsonrpc: "2.0",
            id: 1,
            result: { protocolVersion: 2 },
          }),
          { status: 200, headers: { "content-type": "application/json" } },
        )) as unknown as typeof fetch,
    });
    await expect(client.initialize()).rejects.toThrow(
      /unsupported ACP protocolVersion 2/,
    );
  });
});
