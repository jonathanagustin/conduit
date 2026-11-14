import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { mkdirSync, realpathSync } from "node:fs";
import { join } from "node:path";
import { Readable, Writable } from "node:stream";
import { fileURLToPath } from "node:url";

import {
  AGENT_METHODS,
  CLIENT_METHODS,
  PROTOCOL_VERSION,
  client,
  ndJsonStream,
  type ClientConnection,
  type RequestPermissionRequest,
  type SessionNotification,
} from "@agentclientprotocol/sdk";

import {
  redact,
  type AcpUpdate,
  type PermissionBook,
  type PermissionRequest,
} from "../api/protocol.ts";

export type AgentHandlers = {
  book: PermissionBook;
  onUpdate: (sessionId: string, update: AcpUpdate) => void;
  onPermission: (request: PermissionRequest) => void;
  activeRun: (
    sessionId: string,
  ) => { threadId: string; runId: string } | undefined;
};

export type CodexAgent = {
  connection: ClientConnection;
  child: ChildProcessWithoutNullStreams;
  fixture: boolean;
  mode: string;
  close: () => void;
};

function agentCommand(): { command: string; args: string[]; fixture: boolean } {
  if (process.env["ASSISTANT_ACP_COMMAND"]) {
    const args = process.env["ASSISTANT_ACP_ARGS"]
      ? (JSON.parse(process.env["ASSISTANT_ACP_ARGS"]) as string[])
      : [];
    return {
      command: process.env["ASSISTANT_ACP_COMMAND"],
      args,
      fixture: process.env["ASSISTANT_ACP_FIXTURE"] === "1",
    };
  }
  const command = realpathSync(
    fileURLToPath(
      new URL("../../node_modules/.bin/codex-acp", import.meta.url),
    ),
  );
  return { command, args: [], fixture: false };
}

function childEnv(workspace: string): NodeJS.ProcessEnv {
  const mode = process.env["INITIAL_AGENT_MODE"] ?? "workspace-write";
  if (mode === "agent-full-access") {
    throw new Error(
      "INITIAL_AGENT_MODE=agent-full-access is refused; a tool approval is not deployment authority",
    );
  }
  const codexHome =
    process.env["ASSISTANT_CODEX_HOME"] ?? join(workspace, ".codex");
  mkdirSync(codexHome, { recursive: true });
  return {
    ...process.env,
    CODEX_HOME: codexHome,
    INITIAL_AGENT_MODE: mode,
  };
}

function methodId(method: { id?: string }): string | undefined {
  return method.id;
}

export async function startAgent(
  workspace: string,
  handlers: AgentHandlers,
): Promise<CodexAgent> {
  const launch = agentCommand();
  const child = spawn(launch.command, launch.args, {
    env: childEnv(workspace),
    stdio: ["pipe", "pipe", "pipe"],
  });
  let stderr = "";
  child.stderr.setEncoding("utf8");
  child.stderr.on("data", (chunk: string) => {
    stderr = redact(`${stderr}${chunk}`).slice(-4_000);
  });
  const exited = new Promise<void>((resolve) => {
    child.once("exit", () => resolve());
  });

  const stream = ndJsonStream(
    Writable.toWeb(child.stdin) as WritableStream<Uint8Array>,
    Readable.toWeb(child.stdout) as ReadableStream<Uint8Array>,
  );
  const app = client({ name: "conduit-assistant" })
    .onNotification(
      CLIENT_METHODS.session_update,
      (ctx: { params: SessionNotification }) => {
        handlers.onUpdate(ctx.params.sessionId, ctx.params.update as AcpUpdate);
      },
    )
    .onRequest(
      CLIENT_METHODS.session_request_permission,
      async (ctx: { params: RequestPermissionRequest; requestId: unknown }) => {
        const active = handlers.activeRun(ctx.params.sessionId);
        if (!active) return { outcome: { outcome: "cancelled" as const } };
        const request: PermissionRequest = {
          requestId: `${active.threadId}::${active.runId}::${String(ctx.requestId)}`,
          threadId: active.threadId,
          runId: active.runId,
          toolCallId: ctx.params.toolCall.toolCallId,
          title: ctx.params.toolCall.title ?? ctx.params.toolCall.toolCallId,
          options: ctx.params.options.map((option) => ({
            optionId: option.optionId,
            name: option.name,
            kind: option.kind,
          })),
        };
        handlers.onPermission(request);
        const outcome = await handlers.book.open(request);
        if (outcome.outcome === "cancelled")
          return { outcome: { outcome: "cancelled" as const } };
        return {
          outcome: { outcome: "selected" as const, optionId: outcome.optionId },
        };
      },
    );

  const connection = app.connect(stream);
  const initialized = await Promise.race([
    connection.agent.request(AGENT_METHODS.initialize, {
      protocolVersion: PROTOCOL_VERSION,
      clientInfo: { name: "conduit-assistant", version: "0.0.0" },
      clientCapabilities: {},
    }),
    exited.then(() => {
      throw new Error(`codex-acp exited before initialize. ${stderr}`.trim());
    }),
  ]);
  if (initialized.protocolVersion !== PROTOCOL_VERSION) {
    child.kill();
    throw new Error(
      `codex-acp negotiated protocol ${initialized.protocolVersion}, expected ${PROTOCOL_VERSION}`,
    );
  }

  const apiKey = process.env["CODEX_API_KEY"] ?? process.env["OPENAI_API_KEY"];
  const methods = initialized.authMethods ?? [];
  const apiMethod = methods.find((method) => methodId(method) === "api-key");
  if (apiKey && apiMethod?.id) {
    await connection.agent.request(AGENT_METHODS.authenticate, {
      methodId: apiMethod.id,
    });
  }

  return {
    connection,
    child,
    fixture: launch.fixture,
    mode: process.env["INITIAL_AGENT_MODE"] ?? "workspace-write",
    close() {
      child.kill();
    },
  };
}
