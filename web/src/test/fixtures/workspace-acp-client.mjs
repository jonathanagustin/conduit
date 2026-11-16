import assert from "node:assert/strict";
import { createServer } from "vite-plus";

const server = await createServer({
  server: { middlewareMode: true },
  appType: "custom",
});

try {
  const { createWorkspaceAcpClient } = await server.ssrLoadModule(
    "/src/api/workspace-acp.ts",
  );
  const { runAgent } = await server.ssrLoadModule(
    "/src/console/operator/agent-runs.js",
  );

  const origin = process.env.CONDUIT_ACP_FIXTURE_ORIGIN ?? "";
  const workspaceId = process.env.CONDUIT_ACP_FIXTURE_WORKSPACE ?? "";
  const branch = process.env.CONDUIT_ACP_FIXTURE_BRANCH ?? "";
  assert.match(origin, /^http:\/\/127\.0\.0\.1:\d+$/u);
  assert.ok(workspaceId.length > 0);
  assert.ok(branch.length > 0);

  const authedFetch = (input, init = {}) => {
    const url =
      typeof input === "string" || input instanceof URL
        ? new URL(String(input), origin)
        : input;
    return fetch(url, {
      ...init,
      headers: {
        ...init.headers,
        "X-Platform-Subject": "alice",
      },
    });
  };

  const client = createWorkspaceAcpClient({
    workspaceId,
    branch,
    fetchImpl: authedFetch,
  });

  const init = await client.initialize();
  assert.equal(init.protocolVersion, 1);

  const session = await client.newSession();
  assert.ok(session.sessionId.length > 0);
  const threadId = session._meta?.threadId ?? "";
  assert.match(threadId, /^wsp-acp-/u);

  const events = [];
  const settlements = [];
  const { stopReason } = await client.prompt({
    sessionId: session.sessionId,
    runId: "js-acp-run-1",
    prompt: [
      { type: "text", text: "edit notes.txt: hello from real acp bridge" },
    ],
    onEvent: (event) => {
      events.push(event);
      if (event.type === "permission_request") {
        settlements.push(
          client.decidePermission({
            requestId: event.request.requestId,
            sessionId: session.sessionId,
            runId: "js-acp-run-1",
            decision: "confirm",
          }),
        );
      }
    },
  });
  await Promise.all(settlements);
  assert.equal(stopReason, "end_turn");
  assert.ok(events.some((e) => e.type === "permission_request"));

  const exchange = await runAgent(
    {
      threadId,
      runId: "js-console-mirror-1",
      protocolVersion: "1.0",
      messages: [],
    },
    null,
    authedFetch,
  );
  assert.equal(exchange.terminal, true);
  assert.equal(exchange.outcome?.type, "success");
  assert.ok(Array.isArray(exchange.state.edits));
  assert.ok(
    exchange.state.edits.some((edit) => edit.path === "notes.txt"),
    `expected notes.txt in mirrored console edits, got ${JSON.stringify(exchange.state)}`,
  );
} finally {
  await server.close();
}
