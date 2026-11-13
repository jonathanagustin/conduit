// A stdio ACP agent used by tests. It speaks newline-delimited JSON-RPC and
// never touches Codex. Prompt text "please cancel" waits until session/cancel.
import { createInterface } from "node:readline";

const sessions = new Map();
let nextSession = 1;
let cancelRequested = false;
let pendingPermission;

function send(message) {
  process.stdout.write(`${JSON.stringify(message)}\n`);
}

function notify(method, params) {
  send({ jsonrpc: "2.0", method, params });
}

const input = createInterface({ input: process.stdin });
input.on("line", (line) => {
  if (line.trim().length === 0) return;
  const message = JSON.parse(line);
  if (message.method === undefined) {
    if (message.id === "perm-1") pendingPermission?.(message.result?.outcome ?? { outcome: "cancelled" });
    return;
  }
  if (message.id === undefined) {
    if (message.method === "session/cancel") {
      cancelRequested = true;
      pendingPermission?.({ outcome: "cancelled" });
    }
    return;
  }
  void handle(message).catch((error) => {
    send({
      jsonrpc: "2.0",
      id: message.id,
      error: { code: -32603, message: error instanceof Error ? error.message : "fake agent failed" },
    });
  });
});

async function handle(message) {
  const { id, method, params } = message;
  if (method === "initialize") {
    send({
      jsonrpc: "2.0",
      id,
      result: {
        protocolVersion: params.protocolVersion,
        agentCapabilities: {},
        agentInfo: { name: "fake-codex", version: "0" },
        authMethods: [],
      },
    });
    return;
  }
  if (method === "session/new") {
    const sessionId = `fake-${nextSession}`;
    nextSession += 1;
    sessions.set(sessionId, { cwd: params.cwd, mcpServers: params.mcpServers });
    send({ jsonrpc: "2.0", id, result: { sessionId } });
    return;
  }
  if (method === "session/close" || method === "session/delete") {
    sessions.delete(params.sessionId);
    send({ jsonrpc: "2.0", id, result: {} });
    return;
  }
  if (method === "session/prompt") {
    await prompt(id, params);
    return;
  }
  if (method === "authenticate") {
    send({ jsonrpc: "2.0", id, result: {} });
    return;
  }
  send({ jsonrpc: "2.0", id, error: { code: -32601, message: `method not found: ${method}` } });
}

async function prompt(id, params) {
  cancelRequested = false;
  const text = params.prompt.map((block) => (block.type === "text" ? block.text : "")).join("");
  const secret = "sk-testsecretvalue";
  notify("session/update", {
    sessionId: params.sessionId,
    update: { sessionUpdate: "agent_thought_chunk", content: { type: "text", text: `Considering ${secret}` } },
  });
  notify("session/update", {
    sessionId: params.sessionId,
    update: { sessionUpdate: "agent_message_chunk", content: { type: "text", text: "Working on it. " } },
  });
  notify("session/update", {
    sessionId: params.sessionId,
    update: {
      sessionUpdate: "tool_call",
      toolCallId: "call-1",
      title: "read README",
      name: "read",
      kind: "read",
      status: "pending",
      rawInput: { path: "README.md" },
    },
  });
  if (text.includes("please cancel")) {
    await new Promise((resolve) => {
      const timer = setInterval(() => {
        if (cancelRequested) {
          clearInterval(timer);
          resolve();
        }
      }, 10);
    });
    notify("session/update", {
      sessionId: params.sessionId,
      update: { sessionUpdate: "tool_call_update", toolCallId: "call-1", status: "failed" },
    });
    send({ jsonrpc: "2.0", id, result: { stopReason: "cancelled" } });
    return;
  }

  const outcome = await new Promise((resolve) => {
    pendingPermission = resolve;
    send({
      jsonrpc: "2.0",
      id: "perm-1",
      method: "session/request_permission",
      params: {
        sessionId: params.sessionId,
        toolCall: { toolCallId: "call-1", title: "read README", kind: "read", status: "pending" },
        options: [
          { optionId: "allow", name: "Allow once", kind: "allow_once" },
          { optionId: "reject", name: "Reject", kind: "reject_once" },
        ],
      },
    });
  });
  pendingPermission = undefined;
  if (cancelRequested || outcome.outcome === "cancelled") {
    send({ jsonrpc: "2.0", id, result: { stopReason: "cancelled" } });
    return;
  }
  const allowed = outcome.optionId === "allow";
  notify("session/update", {
    sessionId: params.sessionId,
    update: {
      sessionUpdate: "tool_call_update",
      toolCallId: "call-1",
      status: allowed ? "completed" : "failed",
      rawOutput: allowed ? { ok: true } : { error: "rejected" },
    },
  });
  notify("session/update", {
    sessionId: params.sessionId,
    update: {
      sessionUpdate: "agent_message_chunk",
      content: { type: "text", text: allowed ? "Read complete." : "The read was rejected." },
    },
  });
  notify("session/update", {
    sessionId: params.sessionId,
    update: {
      sessionUpdate: "usage_update",
      used: 12,
      size: 100,
    },
  });
  send({ jsonrpc: "2.0", id, result: { stopReason: "end_turn" } });
}
