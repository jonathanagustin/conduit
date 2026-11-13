// A stdio ACP agent used by tests. It speaks newline-delimited JSON-RPC and
// never touches Codex. Prompt text "please cancel" waits until session/cancel.
import { createInterface } from "node:readline";

const sessions = new Map();
let nextSession = 1;

function configOptions(model, effort) {
  return [
    {
      id: "model",
      name: "Model",
      description: "Model Codex uses for the session",
      category: "model",
      type: "select",
      currentValue: model,
      options: [
        {
          value: "codex-demo",
          name: "Codex demo",
          description: "Fake agent model",
        },
        {
          value: "codex-fast",
          name: "Codex fast",
          description: "Fake agent model",
        },
      ],
    },
    {
      id: "reasoning_effort",
      name: "Reasoning effort",
      description: "How much reasoning effort the model should use",
      category: "thought_level",
      type: "select",
      currentValue: effort,
      options: [
        { value: "low", name: "Low" },
        { value: "medium", name: "Medium" },
        { value: "high", name: "High" },
      ],
    },
  ];
}
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
    if (message.id === "perm-1")
      pendingPermission?.(message.result?.outcome ?? { outcome: "cancelled" });
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
      error: {
        code: -32603,
        message: error instanceof Error ? error.message : "fake agent failed",
      },
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
    const session = {
      cwd: params.cwd,
      mcpServers: params.mcpServers,
      model: "codex-demo",
      effort: "medium",
      turns: 0,
    };
    sessions.set(sessionId, session);
    send({
      jsonrpc: "2.0",
      id,
      result: {
        sessionId,
        configOptions: configOptions(session.model, session.effort),
      },
    });
    return;
  }
  if (method === "session/set_config_option") {
    const session = sessions.get(params.sessionId);
    if (!session) {
      send({
        jsonrpc: "2.0",
        id,
        error: { code: -32602, message: "unknown session" },
      });
      return;
    }
    const options = configOptions(session.model, session.effort);
    const option = options.find((entry) => entry.id === params.configId);
    const allowed = option?.options?.some(
      (entry) => entry.value === params.value,
    );
    if (!option || !allowed) {
      send({
        jsonrpc: "2.0",
        id,
        error: { code: -32602, message: "unknown config option" },
      });
      return;
    }
    if (params.configId === "model") session.model = params.value;
    if (params.configId === "reasoning_effort") session.effort = params.value;
    send({
      jsonrpc: "2.0",
      id,
      result: { configOptions: configOptions(session.model, session.effort) },
    });
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
  send({
    jsonrpc: "2.0",
    id,
    error: { code: -32601, message: `method not found: ${method}` },
  });
}

async function prompt(id, params) {
  cancelRequested = false;
  const session = sessions.get(params.sessionId);
  const toolCallId = `call-${(session?.turns ?? 0) + 1}`;
  const text = params.prompt
    .map((block) => (block.type === "text" ? block.text : ""))
    .join("");
  const sawAttachment = params.prompt.some(
    (block) =>
      block.type === "image" ||
      (block.type === "text" && block.text.startsWith("Attachment ")),
  );
  const secret = "sk-testsecretvalue";
  notify("session/update", {
    sessionId: params.sessionId,
    update: {
      sessionUpdate: "agent_thought_chunk",
      content: { type: "text", text: `Considering ${secret}` },
    },
  });
  notify("session/update", {
    sessionId: params.sessionId,
    update: {
      sessionUpdate: "agent_message_chunk",
      content: { type: "text", text: "Working on it. " },
    },
  });
  notify("session/update", {
    sessionId: params.sessionId,
    update: {
      sessionUpdate: "tool_call",
      toolCallId,
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
      update: {
        sessionUpdate: "tool_call_update",
        toolCallId,
        status: "failed",
      },
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
        toolCall: {
          toolCallId,
          title: "read README",
          kind: "read",
          status: "pending",
        },
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
      toolCallId,
      status: allowed ? "completed" : "failed",
      rawOutput: allowed ? { ok: true } : { error: "rejected" },
    },
  });
  notify("session/update", {
    sessionId: params.sessionId,
    update: {
      sessionUpdate: "agent_message_chunk",
      content: {
        type: "text",
        text: allowed
          ? `${sawAttachment ? "Attachment received.\n\n" : ""}Using ${session?.model ?? "codex-demo"} at ${session?.effort ?? "medium"} effort.\n\nRead complete.\n\n\`\`\`ts\nexport function allow(tool: string) {\n  return tool !== "deploy";\n}\n\`\`\`\n\n\`\`\`mermaid\ngraph TD\n  Ask --> Read\n  Read --> Answer\n\`\`\``
          : "The read was rejected.",
      },
    },
  });
  notify("session/update", {
    sessionId: params.sessionId,
    update: {
      sessionUpdate: "plan",
      entries: [
        {
          content: "Read the workspace",
          status: "completed",
          priority: "high",
        },
        {
          content: "Report what is unknown",
          status: "pending",
          priority: "medium",
        },
      ],
    },
  });
  notify("session/update", {
    sessionId: params.sessionId,
    update: {
      sessionUpdate: "tool_call_update",
      toolCallId,
      content: [
        {
          type: "resource_link",
          uri: "https://example.com/docs",
          name: "Docs",
        },
      ],
    },
  });
  notify("session/update", {
    sessionId: params.sessionId,
    update: {
      sessionUpdate: "usage_update",
      used: session ? (session.turns += 1) * 12 : 12,
      size: 100,
    },
  });
  send({ jsonrpc: "2.0", id, result: { stopReason: "end_turn" } });
}
