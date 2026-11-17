import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { createHash } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  realpathSync,
  writeFileSync,
} from "node:fs";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { Readable, Writable } from "node:stream";
import { fileURLToPath } from "node:url";

import {
  AGENT_METHODS,
  CLIENT_METHODS,
  PROTOCOL_VERSION,
  client,
  ndJsonStream,
  type ClientConnection,
  type CreateTerminalRequest,
  type InitializeResponse,
  type KillTerminalRequest,
  type ReadTextFileRequest,
  type ReleaseTerminalRequest,
  type RequestPermissionRequest,
  type SessionNotification,
  type TerminalOutputRequest,
  type WaitForTerminalExitRequest,
  type WriteTextFileRequest,
} from "@agentclientprotocol/sdk";

import {
  PINNED_ACP_PROTOCOL_VERSION,
  approvedOption,
  asText,
  classifyPermissionRisk,
  redact,
  sanitizeApprovalOptions,
  type AcpUpdate,
  type ApprovalOption,
  type EditorActionKind,
  type EditorActivityEvent,
  type PermissionBook,
  type PermissionRequest,
} from "../api/protocol.ts";

export type AgentHandlers = {
  book: PermissionBook;
  onUpdate: (sessionId: string, update: AcpUpdate) => void;
  onPermission: (request: PermissionRequest) => void;
  onActivity?: (event: EditorActivityEvent) => void;
  activeRun: (
    sessionId: string,
  ) => { threadId: string; runId: string } | undefined;
};

export type StartAgentOptions = {
  offlineBundle?: boolean;
};

export type CodexAgent = {
  connection: ClientConnection;
  child: ChildProcessWithoutNullStreams;
  fixture: boolean;
  mode: string;
  offlineBundle: boolean;
  initialized: InitializeResponse;
  close: () => void;
};

const EXTERNAL_ENV_KEYS = [
  "CODEX_API_KEY",
  "OPENAI_API_KEY",
  "ANTHROPIC_API_KEY",
  "GEMINI_API_KEY",
  "GITHUB_TOKEN",
  "GH_TOKEN",
  "HTTP_PROXY",
  "HTTPS_PROXY",
  "ALL_PROXY",
  "http_proxy",
  "https_proxy",
  "all_proxy",
] as const;

export function isLoopbackHost(hostname: string): boolean {
  const normalized = hostname.trim().toLowerCase();
  return (
    normalized === "127.0.0.1" ||
    normalized === "localhost" ||
    normalized === "::1" ||
    normalized === "[::1]"
  );
}

export function createOfflineFetch(
  baseFetch: typeof globalThis.fetch = globalThis.fetch,
): typeof globalThis.fetch {
  return async (input: string | URL | Request, init?: RequestInit) => {
    const rawUrl =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.toString()
          : input.url;
    const parsed = new URL(rawUrl);
    if (
      (parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
      !isLoopbackHost(parsed.hostname)
    ) {
      throw new Error(
        `OFFLINE_BUNDLE_EGRESS_DENIED: external network call to ${parsed.origin} is forbidden in offline-bundle mode`,
      );
    }
    return baseFetch(input, init);
  };
}

export function confineWorkspacePath(
  workspaceRoot: string,
  requestedPath: string,
): { absolute: string; relative: string } {
  const trimmed = requestedPath.trim();
  if (trimmed.length === 0 || trimmed.includes("\0")) {
    throw new Error("invalid workspace path");
  }
  const realRoot = realpathSync(workspaceRoot);
  const candidate = isAbsolute(trimmed)
    ? resolve(trimmed)
    : resolve(realRoot, trimmed);
  const relCandidate = relative(realRoot, candidate);
  if (
    relCandidate.startsWith("..") ||
    isAbsolute(relCandidate) ||
    relCandidate === ""
  ) {
    if (relCandidate === "") {
      return { absolute: realRoot, relative: "." };
    }
    throw new Error("path is outside the workspace");
  }
  let probe = candidate;
  while (!existsSync(probe)) {
    const parent = dirname(probe);
    if (parent === probe) break;
    probe = parent;
  }
  const realExisting = realpathSync(probe);
  if (
    realExisting !== realRoot &&
    !realExisting.startsWith(`${realRoot}${sep}`)
  ) {
    throw new Error("path escapes the workspace through a symlink");
  }
  return {
    absolute: candidate,
    relative: relCandidate.split(sep).join("/"),
  };
}

function sha256Digest(content: string): string {
  return `sha256:${createHash("sha256").update(content, "utf8").digest("hex")}`;
}

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

export function childEnv(
  workspace: string,
  offlineBundle: boolean,
): NodeJS.ProcessEnv {
  const mode = process.env["INITIAL_AGENT_MODE"] ?? "workspace-write";
  if (mode === "agent-full-access") {
    throw new Error(
      "INITIAL_AGENT_MODE=agent-full-access is refused; a tool approval is not deployment authority",
    );
  }
  const codexHome =
    process.env["ASSISTANT_CODEX_HOME"] ?? join(workspace, ".codex");
  mkdirSync(codexHome, { recursive: true });
  const env: NodeJS.ProcessEnv = {
    ...process.env,
    CODEX_HOME: codexHome,
    INITIAL_AGENT_MODE: mode,
  };
  if (offlineBundle) {
    for (const key of EXTERNAL_ENV_KEYS) {
      delete env[key];
    }
    env["CONDUIT_OFFLINE_BUNDLE"] = "1";
    env["NO_PROXY"] = "*";
    env["no_proxy"] = "*";
  }
  return env;
}

function methodId(method: { id?: string }): string | undefined {
  return method.id;
}

function inferActionKind(input: {
  kind?: string | null | undefined;
  name?: string | null | undefined;
  title?: string | null | undefined;
}): EditorActionKind {
  const combined =
    `${input.kind ?? ""} ${input.name ?? ""} ${input.title ?? ""}`.toLowerCase();
  if (
    /\b(?:write|edit|patch|create_file|fs\/write_text_file)\b/.test(combined)
  ) {
    return "file_write";
  }
  if (
    /\b(?:exec|execute|bash|sh|shell|terminal|command|run_command)\b/.test(
      combined,
    )
  ) {
    return "command";
  }
  return "tool_call";
}

type ManagedTerminal = {
  child: ChildProcessWithoutNullStreams;
  output: string;
  truncated: boolean;
  limit: number;
  exitCode: number | null;
  signal: string | null;
  exited: Promise<{ exitCode: number | null; signal: string | null }>;
};

export async function startAgent(
  workspace: string,
  handlers: AgentHandlers,
  options?: StartAgentOptions,
): Promise<CodexAgent> {
  const sdkProtocolVersion: number = PROTOCOL_VERSION;
  if (sdkProtocolVersion !== PINNED_ACP_PROTOCOL_VERSION) {
    throw new Error(
      `ACP SDK protocol constant drifted to ${sdkProtocolVersion}, expected ${PINNED_ACP_PROTOCOL_VERSION}`,
    );
  }
  const realWorkspace = realpathSync(workspace);
  const offlineBundle =
    options?.offlineBundle ?? process.env["CONDUIT_OFFLINE_BUNDLE"] === "1";
  const launch = agentCommand();
  const spawnEnv = childEnv(realWorkspace, offlineBundle);
  const child = spawn(launch.command, launch.args, {
    cwd: realWorkspace,
    env: spawnEnv,
    stdio: ["pipe", "pipe", "pipe"],
  });
  let stderr = "";
  child.stderr.setEncoding("utf8");
  child.stderr.on("data", (chunk: string) => {
    stderr = redact(`${stderr}${chunk}`).slice(-4_000);
  });
  const exited = new Promise<void>((resolveExit) => {
    child.once("exit", () => resolveExit());
  });

  const toolMeta = new Map<
    string,
    {
      name: string;
      kind: EditorActionKind;
      path?: string;
      command?: string;
      rawInput?: unknown;
    }
  >();
  const terminals = new Map<string, ManagedTerminal>();
  let syntheticPermissionSeq = 0;

  async function requireGovernedMutation(
    sessionId: string,
    detail: {
      kind: EditorActionKind;
      title: string;
      path?: string;
      command?: string;
    },
  ): Promise<{ threadId: string; runId: string }> {
    const active = handlers.activeRun(sessionId);
    if (!active) {
      throw new Error("no active governed run for ACP session");
    }
    syntheticPermissionSeq += 1;
    const toolCallId = `gov-${syntheticPermissionSeq}`;
    const defaultOptions: ApprovalOption[] = [
      { optionId: "allow", name: "Allow once", kind: "allow_once" },
      { optionId: "reject", name: "Reject", kind: "reject_once" },
    ];
    const riskTier = classifyPermissionRisk({
      kind: detail.kind,
      title: detail.title,
      path: detail.path,
      command: detail.command,
    });
    const request: PermissionRequest = {
      requestId: `${active.threadId}::${active.runId}::gov-${syntheticPermissionSeq}`,
      threadId: active.threadId,
      runId: active.runId,
      toolCallId,
      title: detail.title,
      options: defaultOptions,
      kind: detail.kind,
      riskTier,
      ...(detail.path !== undefined ? { path: detail.path } : {}),
      ...(detail.command !== undefined ? { command: detail.command } : {}),
    };
    handlers.onPermission(request);
    handlers.onActivity?.({
      type: "activity",
      sessionId,
      threadId: active.threadId,
      runId: active.runId,
      toolCallId,
      kind: "permission",
      name: detail.title,
      status: "pending",
      riskTier,
      requestId: request.requestId,
      ...(detail.path !== undefined ? { path: detail.path } : {}),
      ...(detail.command !== undefined ? { command: detail.command } : {}),
    });
    const outcome = await handlers.book.open(request);
    if (outcome.outcome === "cancelled") {
      throw new Error("governed permission was cancelled");
    }
    const chosen = defaultOptions.find(
      (option) => option.optionId === outcome.optionId,
    );
    if (!chosen || !approvedOption(chosen.kind)) {
      throw new Error("governed permission was denied");
    }
    return active;
  }

  const stream = ndJsonStream(
    Writable.toWeb(child.stdin) as WritableStream<Uint8Array>,
    Readable.toWeb(child.stdout) as ReadableStream<Uint8Array>,
  );
  const app = client({ name: "conduit-assistant" })
    .onNotification(
      CLIENT_METHODS.session_update,
      (ctx: { params: SessionNotification }) => {
        const sessionId = ctx.params.sessionId;
        const update = ctx.params.update as AcpUpdate;
        handlers.onUpdate(sessionId, update);
        const active = handlers.activeRun(sessionId);
        if (!active) return;

        if (
          update.sessionUpdate === "agent_message_chunk" &&
          !Array.isArray(update.content) &&
          update.content?.type === "text" &&
          update.content.text
        ) {
          handlers.onActivity?.({
            type: "activity",
            sessionId,
            threadId: active.threadId,
            runId: active.runId,
            toolCallId: "",
            kind: "message",
            name: "assistant",
            status: "in_progress",
            text: redact(update.content.text),
          });
          return;
        }

        if (
          (update.sessionUpdate === "tool_call" ||
            update.sessionUpdate === "tool_call_update") &&
          update.toolCallId
        ) {
          const mapKey = `${sessionId}::${update.toolCallId}`;
          const prev = toolMeta.get(mapKey);
          const record =
            update.rawInput !== null && typeof update.rawInput === "object"
              ? (update.rawInput as Record<string, unknown>)
              : undefined;
          const pathVal =
            typeof record?.["path"] === "string"
              ? record["path"]
              : (update.locations?.[0]?.path ?? prev?.path);
          const cmdVal =
            typeof record?.["command"] === "string"
              ? record["command"]
              : prev?.command;
          const inferredKind =
            update.sessionUpdate === "tool_call"
              ? inferActionKind({
                  kind: update.kind,
                  name: update.name,
                  title: update.title,
                })
              : (prev?.kind ??
                inferActionKind({
                  kind: update.kind,
                  name: update.name,
                  title: update.title,
                }));
          const resolvedName =
            update.name ?? update.kind ?? prev?.name ?? "tool";
          const nextMeta = {
            name: resolvedName,
            kind: inferredKind,
            ...(pathVal !== undefined ? { path: pathVal } : {}),
            ...(cmdVal !== undefined ? { command: cmdVal } : {}),
            rawInput: update.rawInput ?? prev?.rawInput,
          };
          toolMeta.set(mapKey, nextMeta);

          const status =
            update.status === "completed" ||
            update.status === "failed" ||
            update.status === "in_progress"
              ? update.status
              : "pending";
          handlers.onActivity?.({
            type: "activity",
            sessionId,
            threadId: active.threadId,
            runId: active.runId,
            toolCallId: update.toolCallId,
            kind: inferredKind,
            name: redact(resolvedName),
            status,
            ...(pathVal !== undefined ? { path: redact(pathVal) } : {}),
            ...(cmdVal !== undefined ? { command: redact(cmdVal) } : {}),
            ...(update.rawInput !== undefined
              ? { argsText: asText(update.rawInput) }
              : {}),
            ...(update.rawOutput !== undefined
              ? { resultText: asText(update.rawOutput) }
              : {}),
          });
        }
      },
    )
    .onRequest(
      CLIENT_METHODS.session_request_permission,
      async (ctx: { params: RequestPermissionRequest; requestId: unknown }) => {
        const sessionId = ctx.params.sessionId;
        const active = handlers.activeRun(sessionId);
        if (!active) return { outcome: { outcome: "cancelled" as const } };

        const meta = toolMeta.get(
          `${sessionId}::${ctx.params.toolCall.toolCallId}`,
        );
        const sanitized = sanitizeApprovalOptions(
          ctx.params.options.map((option) => ({
            optionId: option.optionId,
            name: option.name,
            kind: option.kind,
          })),
        );
        const kind =
          meta?.kind ??
          inferActionKind({
            kind: ctx.params.toolCall.kind,
            title: ctx.params.toolCall.title,
          });
        const pathVal = ctx.params.toolCall.locations?.[0]?.path ?? meta?.path;
        const cmdVal = meta?.command;
        const riskTier = classifyPermissionRisk({
          kind: ctx.params.toolCall.kind ?? kind,
          name: meta?.name,
          title: ctx.params.toolCall.title,
          path: pathVal,
          command: cmdVal,
          rawInput: ctx.params.toolCall.rawInput ?? meta?.rawInput,
        });
        const request: PermissionRequest = {
          requestId: `${active.threadId}::${active.runId}::${String(ctx.requestId)}`,
          threadId: active.threadId,
          runId: active.runId,
          toolCallId: ctx.params.toolCall.toolCallId,
          title: ctx.params.toolCall.title ?? ctx.params.toolCall.toolCallId,
          options: sanitized,
          kind,
          riskTier,
          ...(pathVal !== undefined ? { path: pathVal } : {}),
          ...(cmdVal !== undefined ? { command: cmdVal } : {}),
        };
        handlers.onPermission(request);
        handlers.onActivity?.({
          type: "activity",
          sessionId,
          threadId: active.threadId,
          runId: active.runId,
          toolCallId: request.toolCallId,
          kind: "permission",
          name: redact(request.title),
          status: "pending",
          riskTier,
          requestId: request.requestId,
          ...(pathVal !== undefined ? { path: redact(pathVal) } : {}),
          ...(cmdVal !== undefined ? { command: redact(cmdVal) } : {}),
        });
        const outcome = await handlers.book.open(request);
        if (outcome.outcome === "cancelled") {
          return { outcome: { outcome: "cancelled" as const } };
        }
        return {
          outcome: { outcome: "selected" as const, optionId: outcome.optionId },
        };
      },
    )
    .onRequest(
      CLIENT_METHODS.fs_read_text_file,
      async (ctx: { params: ReadTextFileRequest }) => {
        const confined = confineWorkspacePath(realWorkspace, ctx.params.path);
        if (confined.relative === ".") {
          throw new Error("cannot read workspace root as a text file");
        }
        const raw = readFileSync(confined.absolute, "utf8");
        const lines = raw.split("\n");
        const start =
          typeof ctx.params.line === "number" && ctx.params.line > 1
            ? ctx.params.line - 1
            : 0;
        const slice =
          typeof ctx.params.limit === "number" && ctx.params.limit > 0
            ? lines.slice(start, start + ctx.params.limit)
            : lines.slice(start);
        const content = slice.join("\n");
        const active = handlers.activeRun(ctx.params.sessionId);
        if (active) {
          handlers.onActivity?.({
            type: "activity",
            sessionId: ctx.params.sessionId,
            threadId: active.threadId,
            runId: active.runId,
            toolCallId: `fs-read-${confined.relative}`,
            kind: "tool_call",
            name: "fs/read_text_file",
            status: "completed",
            path: confined.relative,
            digest: sha256Digest(raw),
          });
        }
        return { content };
      },
    )
    .onRequest(
      CLIENT_METHODS.fs_write_text_file,
      async (ctx: { params: WriteTextFileRequest }) => {
        const confined = confineWorkspacePath(realWorkspace, ctx.params.path);
        if (confined.relative === ".") {
          throw new Error("cannot overwrite workspace root");
        }
        if (Buffer.byteLength(ctx.params.content, "utf8") > 256 * 1024) {
          throw new Error("file write exceeds 256 KiB workspace bound");
        }
        const active = await requireGovernedMutation(ctx.params.sessionId, {
          kind: "file_write",
          title: `write ${confined.relative}`,
          path: confined.relative,
        });
        mkdirSync(dirname(confined.absolute), { recursive: true });
        confineWorkspacePath(realWorkspace, confined.relative);
        writeFileSync(confined.absolute, ctx.params.content, "utf8");
        const digest = sha256Digest(ctx.params.content);
        handlers.onActivity?.({
          type: "activity",
          sessionId: ctx.params.sessionId,
          threadId: active.threadId,
          runId: active.runId,
          toolCallId: `fs-write-${confined.relative}`,
          kind: "file_write",
          name: "fs/write_text_file",
          status: "completed",
          path: confined.relative,
          digest,
          content: ctx.params.content,
        });
        return {};
      },
    )
    .onRequest(
      CLIENT_METHODS.terminal_create,
      async (ctx: { params: CreateTerminalRequest }) => {
        const cwd = ctx.params.cwd
          ? confineWorkspacePath(realWorkspace, ctx.params.cwd).absolute
          : realWorkspace;
        const args = ctx.params.args ?? [];
        const commandDisplay = [ctx.params.command, ...args].join(" ").trim();
        const active = await requireGovernedMutation(ctx.params.sessionId, {
          kind: "command",
          title: `command ${commandDisplay}`,
          command: commandDisplay,
        });
        const terminalId = `term-${crypto.randomUUID()}`;
        const limit = Math.min(
          Math.max(ctx.params.outputByteLimit ?? 65_536, 1_024),
          65_536,
        );
        const customEnv: NodeJS.ProcessEnv = { ...spawnEnv };
        for (const item of ctx.params.env ?? []) {
          if (offlineBundle && EXTERNAL_ENV_KEYS.includes(item.name as never)) {
            continue;
          }
          customEnv[item.name] = item.value;
        }
        const proc = spawn(ctx.params.command, args, {
          cwd,
          env: customEnv,
          shell: false,
          stdio: ["pipe", "pipe", "pipe"],
        });
        const managed: ManagedTerminal = {
          child: proc,
          output: "",
          truncated: false,
          limit,
          exitCode: null,
          signal: null,
          exited: new Promise((resolveTerm) => {
            proc.once("exit", (code, sig) => {
              managed.exitCode = code;
              managed.signal = sig;
              handlers.onActivity?.({
                type: "activity",
                sessionId: ctx.params.sessionId,
                threadId: active.threadId,
                runId: active.runId,
                toolCallId: terminalId,
                kind: "command",
                name: "terminal/create",
                status: code === 0 ? "completed" : "failed",
                command: redact(commandDisplay),
                resultText: redact(managed.output),
              });
              resolveTerm({ exitCode: code, signal: sig });
            });
          }),
        };
        const appendOutput = (chunk: Buffer) => {
          managed.output = redact(`${managed.output}${chunk.toString("utf8")}`);
          if (Buffer.byteLength(managed.output, "utf8") > managed.limit) {
            managed.truncated = true;
            managed.output = managed.output.slice(-managed.limit);
          }
        };
        proc.stdout.on("data", appendOutput);
        proc.stderr.on("data", appendOutput);
        terminals.set(terminalId, managed);
        handlers.onActivity?.({
          type: "activity",
          sessionId: ctx.params.sessionId,
          threadId: active.threadId,
          runId: active.runId,
          toolCallId: terminalId,
          kind: "command",
          name: "terminal/create",
          status: "in_progress",
          command: redact(commandDisplay),
        });
        return { terminalId };
      },
    )
    .onRequest(
      CLIENT_METHODS.terminal_output,
      async (ctx: { params: TerminalOutputRequest }) => {
        const term = terminals.get(ctx.params.terminalId);
        if (!term) throw new Error("unknown terminal");
        return {
          output: term.output,
          truncated: term.truncated,
          ...(term.exitCode !== null || term.signal !== null
            ? { exitStatus: { exitCode: term.exitCode, signal: term.signal } }
            : {}),
        };
      },
    )
    .onRequest(
      CLIENT_METHODS.terminal_wait_for_exit,
      async (ctx: { params: WaitForTerminalExitRequest }) => {
        const term = terminals.get(ctx.params.terminalId);
        if (!term) throw new Error("unknown terminal");
        return term.exited;
      },
    )
    .onRequest(
      CLIENT_METHODS.terminal_release,
      async (ctx: { params: ReleaseTerminalRequest }) => {
        const term = terminals.get(ctx.params.terminalId);
        if (term) {
          term.child.kill();
          terminals.delete(ctx.params.terminalId);
        }
        return {};
      },
    )
    .onRequest(
      CLIENT_METHODS.terminal_kill,
      async (ctx: { params: KillTerminalRequest }) => {
        const term = terminals.get(ctx.params.terminalId);
        if (term) term.child.kill();
        return {};
      },
    );

  const connection = app.connect(stream);
  const initialized = await Promise.race([
    connection.agent.request(AGENT_METHODS.initialize, {
      protocolVersion: PINNED_ACP_PROTOCOL_VERSION,
      clientInfo: { name: "conduit-assistant", version: "0.0.0" },
      clientCapabilities: {
        fs: { readTextFile: true, writeTextFile: true },
        terminal: true,
      },
    }),
    exited.then(() => {
      throw new Error(`codex-acp exited before initialize. ${stderr}`.trim());
    }),
  ]);
  if (initialized.protocolVersion !== PINNED_ACP_PROTOCOL_VERSION) {
    child.kill();
    throw new Error(
      `codex-acp negotiated protocol ${initialized.protocolVersion}, expected ${PINNED_ACP_PROTOCOL_VERSION}`,
    );
  }

  if (!offlineBundle) {
    const apiKey =
      process.env["CODEX_API_KEY"] ?? process.env["OPENAI_API_KEY"];
    const methods = initialized.authMethods ?? [];
    const apiMethod = methods.find((method) => methodId(method) === "api-key");
    if (apiKey && apiMethod?.id) {
      await connection.agent.request(AGENT_METHODS.authenticate, {
        methodId: apiMethod.id,
      });
    }
  }

  return {
    connection,
    child,
    fixture: launch.fixture,
    mode: process.env["INITIAL_AGENT_MODE"] ?? "workspace-write",
    offlineBundle,
    initialized,
    close() {
      for (const term of terminals.values()) {
        term.child.kill();
      }
      terminals.clear();
      child.kill();
    },
  };
}
