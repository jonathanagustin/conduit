import { timingSafeEqual } from "node:crypto";
import {
  createServer,
  type IncomingMessage,
  type ServerResponse,
} from "node:http";
import { mkdirSync, realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { AddressInfo } from "node:net";

import { AGENT_METHODS } from "@agentclientprotocol/sdk";

import {
  PINNED_ACP_PROTOCOL_VERSION,
  applyUpdate,
  approvedOption,
  attachApproval,
  createPermissionBook,
  emptySnapshot,
  redact,
  redactValue,
  sessionConfig,
  settleApproval,
  type AcpUpdate,
  type EditorActivityEvent,
  type PermissionRequest,
  type RunSnapshot,
  type SessionConfigOption,
  type WorkspaceScope,
} from "../api/protocol.ts";
import { createOfflineFetch, startAgent, type CodexAgent } from "./agent.ts";

type Thread = {
  id: string;
  title: string;
  archived: boolean;
  updatedAt: string;
  sessionId: string;
  userTexts: string[];
  configOptions: SessionConfigOption[];
  activeRun?: string;
};

type Active = {
  threadId: string;
  runId: string;
  snapshot: RunSnapshot;
  write: (snapshot: RunSnapshot) => void;
  writeEvent?: (event: Record<string, unknown>) => void;
};

export type BridgeOptions = {
  workspace?: string;
  port?: number;
  bridgeToken?: string;
  scope?: WorkspaceScope;
  offlineBundle?: boolean;
};

export type BridgeServer = {
  port: number;
  workspace: string;
  offlineBundle: boolean;
  outboundFetch: typeof globalThis.fetch;
  close: () => Promise<void>;
};

function workspaceDir(): string {
  const requested =
    process.env["ASSISTANT_WORKSPACE"] ?? join(tmpdir(), "conduit-assistant");
  mkdirSync(requested, { recursive: true });
  return realpathSync(requested);
}

function envScope(): WorkspaceScope | undefined {
  const owner = process.env["CONDUIT_WORKSPACE_OWNER"]?.trim() ?? "";
  const workspaceId = process.env["CONDUIT_WORKSPACE_ID"]?.trim() ?? "";
  const branch = process.env["CONDUIT_WORKSPACE_BRANCH"]?.trim() ?? "";
  if (owner.length === 0 && workspaceId.length === 0 && branch.length === 0) {
    return undefined;
  }
  if (owner.length === 0 || workspaceId.length === 0 || branch.length === 0) {
    throw new Error(
      "CONDUIT_WORKSPACE_OWNER, CONDUIT_WORKSPACE_ID, and CONDUIT_WORKSPACE_BRANCH must be set together",
    );
  }
  return { owner, workspaceId, branch };
}

function headerString(
  request: IncomingMessage,
  name: string,
): string | undefined {
  const value = request.headers[name.toLowerCase()];
  if (Array.isArray(value)) return value[0];
  return value;
}

function hasBrowserHeaders(request: IncomingMessage): boolean {
  return (
    headerString(request, "origin") !== undefined ||
    headerString(request, "sec-fetch-site") !== undefined ||
    headerString(request, "sec-fetch-dest") !== undefined
  );
}

function constantTimeEquals(expected: string, actual: string): boolean {
  const left = Buffer.from(expected, "utf8");
  const right = Buffer.from(actual, "utf8");
  if (left.length !== right.length) return false;
  return timingSafeEqual(left, right);
}

function json(response: ServerResponse, status: number, body: unknown): void {
  const payload = JSON.stringify(body);
  response.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "content-length": Buffer.byteLength(payload),
    "cache-control": "no-store",
  });
  response.end(payload);
}

function rpcResult(
  response: ServerResponse,
  id: unknown,
  result: unknown,
): void {
  json(response, 200, { jsonrpc: "2.0", id: id ?? null, result });
}

function rpcError(
  response: ServerResponse,
  httpStatus: number,
  id: unknown,
  code: number,
  message: string,
): void {
  json(response, httpStatus, {
    jsonrpc: "2.0",
    id: id ?? null,
    error: { code, message: redact(message) },
  });
}

function readBody(request: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let size = 0;
    request.on("data", (chunk: Buffer) => {
      size += chunk.length;
      if (size > 2_000_000) {
        reject(new Error("request body exceeds 2MB"));
        request.destroy();
        return;
      }
      chunks.push(chunk);
    });
    request.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    request.on("error", reject);
  });
}

async function readJson(
  request: IncomingMessage,
): Promise<Record<string, unknown>> {
  const raw = await readBody(request);
  if (raw.length === 0) return {};
  const parsed = JSON.parse(raw) as unknown;
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("request body must be an object");
  }
  return parsed as Record<string, unknown>;
}

function threadView(thread: Thread) {
  return {
    id: thread.id,
    title: thread.title,
    archived: thread.archived,
    updatedAt: thread.updatedAt,
  };
}

export async function createBridge(
  options?: BridgeOptions,
): Promise<BridgeServer> {
  const workspace = realpathSync(options?.workspace ?? workspaceDir());
  const bridgeToken =
    options?.bridgeToken ?? process.env["CONDUIT_BRIDGE_TOKEN"];
  const scope = options?.scope ?? envScope();
  const offlineBundle =
    options?.offlineBundle ?? process.env["CONDUIT_OFFLINE_BUNDLE"] === "1";
  const outboundFetch = offlineBundle
    ? createOfflineFetch(globalThis.fetch)
    : globalThis.fetch;

  const threads = new Map<string, Thread>();
  const sessionToThread = new Map<string, string>();
  const active = new Map<string, Active>();
  const book = createPermissionBook();

  const onUpdate = (sessionId: string, update: AcpUpdate) => {
    const run = active.get(sessionId);
    if (!run) return;
    run.snapshot = applyUpdate(run.snapshot, update);
    run.writeEvent?.({
      type: "acp_notification",
      method: "session/update",
      params: { sessionId, update: redactValue(update) },
    });
    run.write(run.snapshot);
  };
  const onPermission = (request: PermissionRequest) => {
    const thread = threads.get(request.threadId);
    if (!thread) return;
    const run = active.get(thread.sessionId);
    if (!run || run.runId !== request.runId) return;
    run.snapshot = attachApproval(run.snapshot, request);
    run.writeEvent?.({
      type: "permission_request",
      request: {
        ...request,
        title: redact(request.title),
      },
    });
    run.write(run.snapshot);
  };
  const onActivity = (event: EditorActivityEvent) => {
    const run = active.get(event.sessionId);
    if (!run || run.runId !== event.runId) return;
    run.writeEvent?.(event);
  };

  let defaults: SessionConfigOption[] = [];
  const agent: CodexAgent = await startAgent(
    workspace,
    {
      book,
      onUpdate,
      onPermission,
      onActivity,
      activeRun(sessionId) {
        const run = active.get(sessionId);
        return run ? { threadId: run.threadId, runId: run.runId } : undefined;
      },
    },
    { offlineBundle },
  );

  async function newSession(): Promise<{
    sessionId: string;
    configOptions: SessionConfigOption[];
  }> {
    const created = (await agent.connection.agent.request(
      AGENT_METHODS.session_new,
      {
        cwd: workspace,
        mcpServers: [],
      },
    )) as { sessionId: string; configOptions?: unknown };
    return {
      sessionId: created.sessionId,
      configOptions: sessionConfig(created.configOptions),
    };
  }

  const server = createServer((request, response) => {
    void handle(request, response).catch((error: unknown) => {
      const message = redact(
        error instanceof Error ? error.message : "assistant bridge failed",
      );
      if (!response.headersSent) json(response, 500, { error: message });
      else response.end();
    });
  });

  function verifyHubCaller(
    request: IncomingMessage,
    response: ServerResponse,
  ): boolean {
    if (hasBrowserHeaders(request)) {
      json(response, 403, {
        error:
          "direct browser access to the ACP bridge is forbidden; connect through the Hub",
      });
      return false;
    }
    if (bridgeToken && bridgeToken.length > 0) {
      const supplied = headerString(request, "x-conduit-bridge-token") ?? "";
      if (!constantTimeEquals(bridgeToken, supplied)) {
        json(response, 401, { error: "missing or invalid Hub bridge token" });
        return false;
      }
    }
    if (scope) {
      const owner = headerString(request, "x-conduit-workspace-owner") ?? "";
      const workspaceId = headerString(request, "x-conduit-workspace-id") ?? "";
      const branch = headerString(request, "x-conduit-workspace-branch") ?? "";
      if (
        owner !== scope.owner ||
        workspaceId !== scope.workspaceId ||
        branch !== scope.branch
      ) {
        json(response, 403, { error: "workspace scope mismatch" });
        return false;
      }
    }
    return true;
  }

  function asString(value: unknown): string {
    return typeof value === "string" ? value : "";
  }

  async function settleBridgePermission(
    requestId: string,
    body: Record<string, unknown>,
    response: ServerResponse,
  ): Promise<void> {
    const threadId = asString(body["threadId"]);
    const runId = asString(body["runId"]);
    if (body["cancelled"] === true) {
      const cancelled = book.cancelRequest({ requestId, threadId, runId });
      if (!cancelled.ok) {
        json(response, cancelled.status, { error: cancelled.error });
        return;
      }
      const thread = threads.get(threadId);
      const run = thread ? active.get(thread.sessionId) : undefined;
      if (run) {
        run.snapshot = settleApproval(run.snapshot, requestId, {
          resolution: "cancelled",
        });
        run.write(run.snapshot);
      }
      json(response, 200, { ok: true });
      return;
    }
    const optionId = asString(body["optionId"]);
    const decision = book.decide({ requestId, threadId, runId, optionId });
    if (!decision.ok) {
      json(response, decision.status, { error: decision.error });
      return;
    }
    const thread = threads.get(threadId);
    const run = thread ? active.get(thread.sessionId) : undefined;
    if (run) {
      const option = run.snapshot.tools
        .flatMap((tool) => tool.approval?.options ?? [])
        .find((candidate) => candidate.optionId === optionId);
      const approved = option ? approvedOption(option.kind) : false;
      run.snapshot = settleApproval(run.snapshot, requestId, {
        optionId,
        approved,
      });
      run.write(run.snapshot);
    }
    json(response, 200, { ok: true });
  }

  async function handleAcpRpc(
    request: IncomingMessage,
    response: ServerResponse,
  ): Promise<void> {
    const body = await readJson(request);
    if (body["jsonrpc"] !== "2.0" || typeof body["method"] !== "string") {
      rpcError(
        response,
        400,
        body["id"],
        -32600,
        "invalid ACP JSON-RPC 2.0 envelope",
      );
      return;
    }
    const id = body["id"];
    const method = body["method"];
    const params =
      body["params"] !== null && typeof body["params"] === "object"
        ? (body["params"] as Record<string, unknown>)
        : {};

    if (method === AGENT_METHODS.initialize) {
      if (params["protocolVersion"] !== PINNED_ACP_PROTOCOL_VERSION) {
        const reported =
          typeof params["protocolVersion"] === "number" ||
          typeof params["protocolVersion"] === "string"
            ? String(params["protocolVersion"])
            : "invalid";
        rpcError(
          response,
          400,
          id,
          -32602,
          `unsupported ACP protocolVersion ${reported}; expected ${PINNED_ACP_PROTOCOL_VERSION}`,
        );
        return;
      }
      rpcResult(response, id, {
        protocolVersion: PINNED_ACP_PROTOCOL_VERSION,
        agentCapabilities: agent.initialized.agentCapabilities ?? {},
        agentInfo: agent.initialized.agentInfo ?? {
          name: "conduit-acp",
          version: "1.0.0",
        },
        authMethods: [],
      });
      return;
    }

    if (method === AGENT_METHODS.session_new) {
      const session = await newSession();
      const meta =
        params["_meta"] !== null && typeof params["_meta"] === "object"
          ? (params["_meta"] as Record<string, unknown>)
          : {};
      const threadId =
        typeof meta["threadId"] === "string" && meta["threadId"].length > 0
          ? meta["threadId"]
          : `wsp-acp-${crypto.randomUUID()}`;
      const thread: Thread = {
        id: threadId,
        title: "Workspace coding agent",
        archived: false,
        updatedAt: new Date().toISOString(),
        sessionId: session.sessionId,
        userTexts: [],
        configOptions: session.configOptions,
      };
      threads.set(thread.id, thread);
      sessionToThread.set(session.sessionId, thread.id);
      rpcResult(response, id, {
        sessionId: session.sessionId,
        configOptions: session.configOptions,
        _meta: { threadId: thread.id, workspace },
      });
      return;
    }

    if (method === AGENT_METHODS.session_set_config_option) {
      const sessionId = asString(params["sessionId"]);
      const threadId = sessionToThread.get(sessionId);
      const thread = threadId ? threads.get(threadId) : undefined;
      if (!thread) {
        rpcError(response, 404, id, -32602, "unknown ACP session");
        return;
      }
      const configId =
        typeof params["configId"] === "string" ? params["configId"] : "";
      const value = params["value"];
      if (
        configId.length === 0 ||
        (typeof value !== "string" && typeof value !== "boolean")
      ) {
        rpcError(
          response,
          400,
          id,
          -32602,
          "configId and a string or boolean value are required",
        );
        return;
      }
      const updated = (await agent.connection.agent.request(
        AGENT_METHODS.session_set_config_option,
        {
          sessionId: thread.sessionId,
          configId,
          value,
        },
      )) as { configOptions?: unknown };
      thread.configOptions = sessionConfig(updated.configOptions);
      rpcResult(response, id, { configOptions: thread.configOptions });
      return;
    }

    if (method === AGENT_METHODS.session_cancel) {
      const sessionId = asString(params["sessionId"]);
      const threadId = sessionToThread.get(sessionId);
      const thread = threadId ? threads.get(threadId) : undefined;
      if (!thread) {
        rpcError(response, 404, id, -32602, "unknown ACP session");
        return;
      }
      if (thread.activeRun) {
        book.cancelRun(thread.activeRun);
      }
      await agent.connection.agent.notify(AGENT_METHODS.session_cancel, {
        sessionId: thread.sessionId,
      });
      rpcResult(response, id, {});
      return;
    }

    if (
      method === AGENT_METHODS.session_close ||
      method === AGENT_METHODS.session_delete
    ) {
      const sessionId = asString(params["sessionId"]);
      const threadId = sessionToThread.get(sessionId);
      if (threadId) {
        const thread = threads.get(threadId);
        if (thread?.activeRun) {
          book.cancelRun(thread.activeRun);
        }
        threads.delete(threadId);
        sessionToThread.delete(sessionId);
      }
      await agent.connection.agent
        .request(AGENT_METHODS.session_close, { sessionId })
        .catch(() => undefined);
      rpcResult(response, id, {});
      return;
    }

    if (method === AGENT_METHODS.session_prompt) {
      const sessionId = asString(params["sessionId"]);
      const threadId = sessionToThread.get(sessionId);
      const thread = threadId ? threads.get(threadId) : undefined;
      if (!thread) {
        rpcError(response, 404, id, -32602, "unknown ACP session");
        return;
      }
      if (thread.activeRun) {
        rpcError(
          response,
          409,
          id,
          -32000,
          "a run is already active on this ACP session",
        );
        return;
      }
      const meta =
        params["_meta"] !== null && typeof params["_meta"] === "object"
          ? (params["_meta"] as Record<string, unknown>)
          : {};
      const runId =
        typeof meta["runId"] === "string" && meta["runId"].length > 0
          ? meta["runId"]
          : `run-${crypto.randomUUID()}`;
      const rawPrompt = Array.isArray(params["prompt"]) ? params["prompt"] : [];
      const blocks: (
        | { type: "text"; text: string }
        | { type: "image"; mimeType: string; data: string }
      )[] = [];
      for (const item of rawPrompt) {
        if (!item || typeof item !== "object") continue;
        const block = item as Record<string, unknown>;
        if (block["type"] === "text" && typeof block["text"] === "string") {
          blocks.push({ type: "text", text: block["text"] });
        } else if (
          block["type"] === "image" &&
          typeof block["mimeType"] === "string" &&
          typeof block["data"] === "string"
        ) {
          blocks.push({
            type: "image",
            mimeType: block["mimeType"],
            data: block["data"],
          });
        }
      }
      if (blocks.length === 0) {
        rpcError(
          response,
          400,
          id,
          -32602,
          "session/prompt requires at least one content block",
        );
        return;
      }

      thread.activeRun = runId;
      thread.updatedAt = new Date().toISOString();
      response.writeHead(200, {
        "content-type": "text/event-stream; charset=utf-8",
        "cache-control": "no-store",
        connection: "keep-alive",
      });
      const writeEvent = (event: Record<string, unknown>) => {
        if (!response.writableEnded) {
          response.write(`data: ${JSON.stringify(event)}\n\n`);
        }
      };
      const write = (snapshot: RunSnapshot) => {
        writeEvent({ type: "snapshot", snapshot });
      };
      const run: Active = {
        threadId: thread.id,
        runId,
        snapshot: emptySnapshot(),
        write,
        writeEvent,
      };
      active.set(thread.sessionId, run);
      let cancelled = false;
      request.on("close", () => {
        if (response.writableEnded || cancelled) return;
        cancelled = true;
        book.cancelRun(runId);
        void agent.connection.agent.notify(AGENT_METHODS.session_cancel, {
          sessionId: thread.sessionId,
        });
      });

      try {
        const result = (await agent.connection.agent.request(
          AGENT_METHODS.session_prompt,
          {
            sessionId: thread.sessionId,
            prompt: blocks,
          },
        )) as { stopReason?: string };
        write(run.snapshot);
        const stopReason =
          typeof result.stopReason === "string"
            ? result.stopReason
            : "end_turn";
        writeEvent({
          type: "rpc_response",
          response: {
            jsonrpc: "2.0",
            id: id ?? null,
            result: { stopReason },
          },
        });
      } catch (error) {
        const message = redact(
          error instanceof Error ? error.message : "the ACP run failed",
        );
        book.cancelRun(runId);
        writeEvent({
          type: "rpc_error",
          response: {
            jsonrpc: "2.0",
            id: id ?? null,
            error: { code: -32603, message },
          },
        });
      } finally {
        active.delete(thread.sessionId);
        if (thread.activeRun === runId) delete thread.activeRun;
        if (!response.writableEnded) response.end();
      }
      return;
    }

    rpcError(response, 404, id, -32601, `unsupported ACP method: ${method}`);
  }

  async function handle(
    request: IncomingMessage,
    response: ServerResponse,
  ): Promise<void> {
    if (!verifyHubCaller(request, response)) {
      return;
    }
    const url = new URL(request.url ?? "/", "http://127.0.0.1");
    const path = url.pathname;
    const method = request.method ?? "GET";

    if (method === "POST" && path === "/acp") {
      await handleAcpRpc(request, response);
      return;
    }

    const acpPermissionMatch = /^\/acp\/permissions\/([^/]+)$/.exec(path);
    if (method === "POST" && acpPermissionMatch) {
      const body = await readJson(request);
      await settleBridgePermission(
        decodeURIComponent(acpPermissionMatch[1] ?? ""),
        body,
        response,
      );
      return;
    }

    if (method === "GET" && path === "/api/assistant/health") {
      json(response, 200, {
        ok: true,
        runtime: "codex-acp",
        fixture: agent.fixture,
        workspace,
        mode: agent.mode,
        offlineBundle,
        protocolVersion: PINNED_ACP_PROTOCOL_VERSION,
        adapter: "@agentclientprotocol/codex-acp",
      });
      return;
    }

    if (method === "GET" && path === "/api/assistant/config") {
      json(response, 200, { options: defaults });
      return;
    }

    if (method === "GET" && path === "/api/assistant/threads") {
      json(response, 200, { threads: [...threads.values()].map(threadView) });
      return;
    }

    if (method === "POST" && path === "/api/assistant/threads") {
      const session = await newSession();
      const thread: Thread = {
        id: crypto.randomUUID(),
        title: "New chat",
        archived: false,
        updatedAt: new Date().toISOString(),
        sessionId: session.sessionId,
        userTexts: [],
        configOptions: session.configOptions,
      };
      threads.set(thread.id, thread);
      sessionToThread.set(session.sessionId, thread.id);
      json(response, 201, threadView(thread));
      return;
    }

    const configMatch = /^\/api\/assistant\/threads\/([^/]+)\/config$/.exec(
      path,
    );
    if (configMatch) {
      const thread = threads.get(decodeURIComponent(configMatch[1] ?? ""));
      if (!thread) {
        json(response, 404, { error: "unknown conversation" });
        return;
      }
      if (method === "GET") {
        json(response, 200, { options: thread.configOptions });
        return;
      }
      if (method === "POST") {
        const body = await readJson(request);
        const configId =
          typeof body["configId"] === "string" ? body["configId"] : "";
        const value = body["value"];
        if (
          configId.length === 0 ||
          (typeof value !== "string" && typeof value !== "boolean")
        ) {
          json(response, 400, {
            error: "configId and a string or boolean value are required",
          });
          return;
        }
        try {
          const updated = (await agent.connection.agent.request(
            AGENT_METHODS.session_set_config_option,
            {
              sessionId: thread.sessionId,
              configId,
              value,
            },
          )) as { configOptions?: unknown };
          thread.configOptions = sessionConfig(updated.configOptions);
          json(response, 200, { options: thread.configOptions });
        } catch (error) {
          const message = redact(
            error instanceof Error
              ? error.message
              : "session config was rejected",
          );
          json(response, 501, { error: message });
        }
        return;
      }
    }

    const threadMatch = /^\/api\/assistant\/threads\/([^/]+)$/.exec(path);
    if (threadMatch) {
      const thread = threads.get(decodeURIComponent(threadMatch[1] ?? ""));
      if (!thread) {
        json(response, 404, { error: "unknown conversation" });
        return;
      }
      if (method === "PATCH") {
        const body = await readJson(request);
        if (typeof body["title"] === "string")
          thread.title = redact(body["title"]).slice(0, 80);
        if (typeof body["archived"] === "boolean")
          thread.archived = body["archived"];
        thread.updatedAt = new Date().toISOString();
        json(response, 200, threadView(thread));
        return;
      }
      if (method === "DELETE") {
        await agent.connection.agent
          .request(AGENT_METHODS.session_close, { sessionId: thread.sessionId })
          .catch(() => undefined);
        sessionToThread.delete(thread.sessionId);
        threads.delete(thread.id);
        json(response, 200, { ok: true });
        return;
      }
    }

    const runMatch = /^\/api\/assistant\/threads\/([^/]+)\/runs$/.exec(path);
    if (method === "POST" && runMatch) {
      await runTurn(decodeURIComponent(runMatch[1] ?? ""), request, response);
      return;
    }

    const cancelMatch = /^\/api\/assistant\/threads\/([^/]+)\/cancel$/.exec(
      path,
    );
    if (method === "POST" && cancelMatch) {
      const thread = threads.get(decodeURIComponent(cancelMatch[1] ?? ""));
      const body = await readJson(request);
      const runId = typeof body["runId"] === "string" ? body["runId"] : "";
      if (!thread || thread.activeRun !== runId) {
        json(response, 409, { error: "that run is not active" });
        return;
      }
      book.cancelRun(runId);
      await agent.connection.agent.notify(AGENT_METHODS.session_cancel, {
        sessionId: thread.sessionId,
      });
      json(response, 202, { ok: true });
      return;
    }

    const permissionMatch = /^\/api\/assistant\/permissions\/([^/]+)$/.exec(
      path,
    );
    if (method === "POST" && permissionMatch) {
      const body = await readJson(request);
      await settleBridgePermission(
        decodeURIComponent(permissionMatch[1] ?? ""),
        body,
        response,
      );
      return;
    }

    json(response, 404, { error: "unknown assistant route" });
  }

  async function runTurn(
    threadId: string,
    request: IncomingMessage,
    response: ServerResponse,
  ): Promise<void> {
    const thread = threads.get(threadId);
    if (!thread) {
      json(response, 404, { error: "unknown conversation" });
      return;
    }
    if (thread.activeRun) {
      json(response, 409, {
        error: "a run is already active on this conversation",
      });
      return;
    }
    const body = await readJson(request);
    const runId = String(body["runId"] ?? "");
    const userTexts = Array.isArray(body["userTexts"])
      ? body["userTexts"].filter(
          (entry): entry is string => typeof entry === "string",
        )
      : [];
    const prompt = userTexts.at(-1) ?? "";
    if (runId.length === 0 || prompt.length === 0) {
      json(response, 400, { error: "runId and a user message are required" });
      return;
    }
    await applyChoice(
      thread,
      "model",
      typeof body["model"] === "string" ? body["model"] : undefined,
    );
    await applyChoice(
      thread,
      "reasoning_effort",
      typeof body["effort"] === "string" ? body["effort"] : undefined,
    );
    const previous = thread.userTexts;
    const extended =
      userTexts.length === previous.length + 1 &&
      previous.every((text, index) => text === userTexts[index]);
    const same =
      userTexts.length === previous.length &&
      previous.every((text, index) => text === userTexts[index]);
    if (!extended && !same && previous.length > 0) {
      sessionToThread.delete(thread.sessionId);
      await agent.connection.agent
        .request(AGENT_METHODS.session_close, { sessionId: thread.sessionId })
        .catch(() => undefined);
      const session = await newSession();
      thread.sessionId = session.sessionId;
      thread.configOptions = session.configOptions;
      sessionToThread.set(session.sessionId, thread.id);
    }

    thread.activeRun = runId;
    thread.updatedAt = new Date().toISOString();
    response.writeHead(200, {
      "content-type": "text/event-stream; charset=utf-8",
      "cache-control": "no-store",
      connection: "keep-alive",
    });
    const write = (snapshot: RunSnapshot) => {
      if (!response.writableEnded)
        response.write(
          `data: ${JSON.stringify({ type: "snapshot", snapshot })}\n\n`,
        );
    };
    const run: Active = {
      threadId: thread.id,
      runId,
      snapshot: emptySnapshot(),
      write,
    };
    active.set(thread.sessionId, run);
    let cancelled = false;
    request.on("close", () => {
      if (response.writableEnded || cancelled) return;
      cancelled = true;
      book.cancelRun(runId);
      void agent.connection.agent.notify(AGENT_METHODS.session_cancel, {
        sessionId: thread.sessionId,
      });
    });

    try {
      const blocks: (
        | { type: "text"; text: string }
        | { type: "image"; mimeType: string; data: string }
      )[] = [{ type: "text", text: prompt }];
      const attachments = Array.isArray(body["attachments"])
        ? body["attachments"]
        : [];
      for (const attachment of attachments) {
        if (!attachment || typeof attachment !== "object") continue;
        const record = attachment as Record<string, unknown>;
        const mimeType =
          typeof record["mimeType"] === "string"
            ? record["mimeType"]
            : "application/octet-stream";
        const data = typeof record["data"] === "string" ? record["data"] : "";
        const name =
          typeof record["name"] === "string" ? record["name"] : "attachment";
        if (mimeType.startsWith("image/") && data.length > 0)
          blocks.push({ type: "image", mimeType, data });
        else if (data.length > 0)
          blocks.push({
            type: "text",
            text: `Attachment ${name} (${mimeType})\n${data.slice(0, 100_000)}`,
          });
      }
      const result = (await agent.connection.agent.request(
        AGENT_METHODS.session_prompt,
        {
          sessionId: thread.sessionId,
          prompt: blocks,
        },
      )) as { stopReason?: string };
      thread.userTexts = userTexts;
      write(run.snapshot);
      const stopReason =
        typeof result.stopReason === "string" ? result.stopReason : "end_turn";
      response.write(
        `data: ${JSON.stringify({ type: "done", stopReason })}\n\n`,
      );
    } catch (error) {
      const message = redact(
        error instanceof Error ? error.message : "the Codex run failed",
      );
      book.cancelRun(runId);
      if (!response.writableEnded) {
        response.write(
          `data: ${JSON.stringify({ type: "error", message })}\n\n`,
        );
      }
    } finally {
      active.delete(thread.sessionId);
      if (thread.activeRun === runId) delete thread.activeRun;
      if (!response.writableEnded) response.end();
    }
  }

  async function applyChoice(
    thread: Thread,
    configId: string,
    value: string | undefined,
  ): Promise<void> {
    if (!value) return;
    const current = thread.configOptions.find(
      (option) => option.id === configId,
    );
    if (!current || current.currentValue === value) return;
    if (!current.options?.some((option) => option.value === value)) return;
    const updated = (await agent.connection.agent.request(
      AGENT_METHODS.session_set_config_option,
      {
        sessionId: thread.sessionId,
        configId,
        value,
      },
    )) as { configOptions?: unknown };
    thread.configOptions = sessionConfig(updated.configOptions);
  }

  const probe = await newSession();
  defaults = probe.configOptions;
  await agent.connection.agent
    .request(AGENT_METHODS.session_close, { sessionId: probe.sessionId })
    .catch(() => undefined);

  await new Promise<void>((resolve) => {
    server.listen(options?.port ?? 0, "127.0.0.1", () => resolve());
  });
  const address = server.address() as AddressInfo;

  return {
    port: address.port,
    workspace,
    offlineBundle,
    outboundFetch,
    async close() {
      agent.close();
      await new Promise<void>((resolve, reject) => {
        server.close((error) => (error ? reject(error) : resolve()));
      });
    },
  };
}
