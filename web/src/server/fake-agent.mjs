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
let nextClientRequest = 1;
let authenticated = false;
const pendingClientRequests = new Map();

function send(message) {
  process.stdout.write(`${JSON.stringify(message)}\n`);
}

function notify(method, params) {
  send({ jsonrpc: "2.0", method, params });
}

function requestClient(method, params) {
  const id = `client-${nextClientRequest}`;
  nextClientRequest += 1;
  return new Promise((resolve, reject) => {
    pendingClientRequests.set(id, { resolve, reject });
    send({ jsonrpc: "2.0", id, method, params });
  });
}

const input = createInterface({ input: process.stdin });
input.on("line", (line) => {
  if (line.trim().length === 0) return;
  const message = JSON.parse(line);
  if (message.method === undefined) {
    if (message.id === "perm-1") {
      pendingPermission?.(message.result?.outcome ?? { outcome: "cancelled" });
      return;
    }
    const clientPending = pendingClientRequests.get(message.id);
    if (clientPending) {
      pendingClientRequests.delete(message.id);
      if (message.error) {
        clientPending.reject(
          new Error(message.error.message ?? "client request failed"),
        );
      } else {
        clientPending.resolve(message.result ?? {});
      }
    }
    return;
  }
  if (message.id === undefined) {
    if (message.method === "session/cancel") {
      cancelRequested = true;
      pendingPermission?.({ outcome: "cancelled" });
      for (const [, waiter] of pendingClientRequests) {
        waiter.reject(new Error("cancelled"));
      }
      pendingClientRequests.clear();
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
    const overrideVersion = process.env["FAKE_ACP_PROTOCOL_VERSION"];
    const protocolVersion =
      overrideVersion !== undefined
        ? Number.parseInt(overrideVersion, 10)
        : params.protocolVersion;
    const authMethods =
      process.env["FAKE_ACP_AUTH_METHODS"] === "api-key"
        ? [{ id: "api-key", name: "API key" }]
        : [];
    send({
      jsonrpc: "2.0",
      id,
      result: {
        protocolVersion,
        agentCapabilities: {},
        agentInfo: { name: "fake-codex", version: "0" },
        authMethods,
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
    authenticated = true;
    send({ jsonrpc: "2.0", id, result: {} });
    return;
  }
  send({
    jsonrpc: "2.0",
    id,
    error: { code: -32601, message: `method not found: ${method}` },
  });
}

async function askPermission(sessionId, toolCall, options) {
  const outcome = await new Promise((resolve) => {
    pendingPermission = resolve;
    send({
      jsonrpc: "2.0",
      id: "perm-1",
      method: "session/request_permission",
      params: {
        sessionId,
        toolCall,
        options,
      },
    });
  });
  pendingPermission = undefined;
  return outcome;
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
  if (text.trim() === "check offline env") {
    const summary = JSON.stringify({
      authenticated,
      hasOpenAiKey: Boolean(process.env["OPENAI_API_KEY"]),
      hasCodexKey: Boolean(process.env["CODEX_API_KEY"]),
      hasGeminiKey: Boolean(process.env["GEMINI_API_KEY"]),
      offlineBundle: process.env["CONDUIT_OFFLINE_BUNDLE"] ?? "",
      noProxy: process.env["NO_PROXY"] ?? "",
    });
    notify("session/update", {
      sessionId: params.sessionId,
      update: {
        sessionUpdate: "agent_message_chunk",
        content: { type: "text", text: summary },
      },
    });
    send({ jsonrpc: "2.0", id, result: { stopReason: "end_turn" } });
    return;
  }
  const editMatch = /^edit\s+(\S+)(?:\s+->\s+(\S+))?:\s*([\s\S]+)$/i.exec(
    text.trim(),
  );
  if (editMatch) {
    const targetPath = editMatch[1];
    const actualPath = editMatch[2] ?? targetPath;
    const nextContent = editMatch[3];
    notify("session/update", {
      sessionId: params.sessionId,
      update: {
        sessionUpdate: "tool_call",
        toolCallId,
        title: `write ${targetPath}`,
        name: "write_file",
        kind: "edit",
        status: "pending",
        locations: [{ path: targetPath }],
        rawInput: { path: targetPath, content: nextContent },
      },
    });
    const outcome = await askPermission(
      params.sessionId,
      {
        toolCallId,
        title: `write ${targetPath}`,
        kind: "edit",
        status: "pending",
        locations: [{ path: targetPath }],
        rawInput: { path: targetPath },
      },
      [
        { optionId: "allow", name: "Allow once", kind: "allow_once" },
        { optionId: "always", name: "Allow always", kind: "allow_always" },
        { optionId: "reject", name: "Reject", kind: "reject_once" },
      ],
    );
    if (cancelRequested || outcome.outcome === "cancelled") {
      notify("session/update", {
        sessionId: params.sessionId,
        update: {
          sessionUpdate: "tool_call_update",
          toolCallId,
          status: "failed",
          rawOutput: { error: "cancelled" },
        },
      });
      send({ jsonrpc: "2.0", id, result: { stopReason: "cancelled" } });
      return;
    }
    if (outcome.optionId !== "allow") {
      notify("session/update", {
        sessionId: params.sessionId,
        update: {
          sessionUpdate: "tool_call_update",
          toolCallId,
          status: "failed",
          rawOutput: { error: "rejected" },
        },
      });
      notify("session/update", {
        sessionId: params.sessionId,
        update: {
          sessionUpdate: "agent_message_chunk",
          content: { type: "text", text: `Rejected edit to ${targetPath}.` },
        },
      });
      send({ jsonrpc: "2.0", id, result: { stopReason: "end_turn" } });
      return;
    }
    await requestClient("fs/write_text_file", {
      sessionId: params.sessionId,
      path: actualPath,
      content: nextContent,
    });
    if (session) session.turns += 1;
    notify("session/update", {
      sessionId: params.sessionId,
      update: {
        sessionUpdate: "tool_call_update",
        toolCallId,
        status: "completed",
        locations: [{ path: targetPath }],
        rawOutput: { path: targetPath, written: true },
      },
    });
    notify("session/update", {
      sessionId: params.sessionId,
      update: {
        sessionUpdate: "agent_message_chunk",
        content: { type: "text", text: `Edited ${targetPath}.` },
      },
    });
    send({ jsonrpc: "2.0", id, result: { stopReason: "end_turn" } });
    return;
  }
  const cmdMatch = /^run command:\s*([\s\S]+)$/i.exec(text.trim());
  if (cmdMatch) {
    const commandLine = cmdMatch[1].trim();
    notify("session/update", {
      sessionId: params.sessionId,
      update: {
        sessionUpdate: "tool_call",
        toolCallId,
        title: `command ${commandLine}`,
        name: "terminal",
        kind: "execute",
        status: "pending",
        rawInput: { command: commandLine },
      },
    });
    const outcome = await askPermission(
      params.sessionId,
      {
        toolCallId,
        title: `command ${commandLine}`,
        kind: "execute",
        status: "pending",
        rawInput: { command: commandLine },
      },
      [
        { optionId: "allow", name: "Allow once", kind: "allow_once" },
        { optionId: "reject", name: "Reject", kind: "reject_once" },
      ],
    );
    if (cancelRequested || outcome.outcome === "cancelled") {
      notify("session/update", {
        sessionId: params.sessionId,
        update: {
          sessionUpdate: "tool_call_update",
          toolCallId,
          status: "failed",
          rawOutput: { error: "cancelled" },
        },
      });
      send({ jsonrpc: "2.0", id, result: { stopReason: "cancelled" } });
      return;
    }
    if (outcome.optionId !== "allow") {
      notify("session/update", {
        sessionId: params.sessionId,
        update: {
          sessionUpdate: "tool_call_update",
          toolCallId,
          status: "failed",
          rawOutput: { error: "rejected" },
        },
      });
      notify("session/update", {
        sessionId: params.sessionId,
        update: {
          sessionUpdate: "agent_message_chunk",
          content: { type: "text", text: `Rejected command ${commandLine}.` },
        },
      });
      send({ jsonrpc: "2.0", id, result: { stopReason: "end_turn" } });
      return;
    }
    const execCommand = process.execPath;
    const execArgs = [
      "-e",
      "process.stdout.write(process.argv[1])",
      `ran:${commandLine}`,
    ];
    const created = await requestClient("terminal/create", {
      sessionId: params.sessionId,
      command: execCommand,
      args: execArgs,
    });
    await requestClient("terminal/wait_for_exit", {
      sessionId: params.sessionId,
      terminalId: created.terminalId,
    });
    const out = await requestClient("terminal/output", {
      sessionId: params.sessionId,
      terminalId: created.terminalId,
    });
    await requestClient("terminal/release", {
      sessionId: params.sessionId,
      terminalId: created.terminalId,
    });
    if (session) session.turns += 1;
    notify("session/update", {
      sessionId: params.sessionId,
      update: {
        sessionUpdate: "tool_call_update",
        toolCallId,
        status: "completed",
        rawOutput: { output: out.output ?? "" },
      },
    });
    notify("session/update", {
      sessionId: params.sessionId,
      update: {
        sessionUpdate: "agent_message_chunk",
        content: { type: "text", text: `Ran ${commandLine}.` },
      },
    });
    send({ jsonrpc: "2.0", id, result: { stopReason: "end_turn" } });
    return;
  }
  if (/\bshow packaging run\b/i.test(text) || /\bshow plan\b/i.test(text)) {
    const runId = text.match(/run\s+(\S+)/i)?.[1] ?? "sample-app-1";
    const planId = text.match(/plan\s+(\S+)/i)?.[1] ?? "plan-1";
    if (/\bshow packaging run\b/i.test(text)) {
      notify("session/update", {
        sessionId: params.sessionId,
        update: {
          sessionUpdate: "tool_call",
          toolCallId: "packaging-run",
          title: "Packaging run",
          name: "packaging_run",
          kind: "other",
          status: "completed",
          rawInput: { runId, phase: "review", configured: true, planId },
        },
      });
    }
    if (/\bshow plan\b/i.test(text)) {
      notify("session/update", {
        sessionId: params.sessionId,
        update: {
          sessionUpdate: "tool_call",
          toolCallId: "plan-card",
          title: "Plan",
          name: "plan",
          kind: "other",
          status: "completed",
          rawInput: {
            id: planId,
            status: "AWAITING_APPROVAL",
            environment: "lab",
            requiredApprovals: 1,
            approvalCount: 0,
          },
        },
      });
    }
    notify("session/update", {
      sessionId: params.sessionId,
      update: {
        sessionUpdate: "agent_message_chunk",
        content: { type: "text", text: "Opened the referenced record." },
      },
    });
    send({ jsonrpc: "2.0", id, result: { stopReason: "end_turn" } });
    return;
  }
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
