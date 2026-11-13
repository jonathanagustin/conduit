import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { mkdirSync, realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { AddressInfo } from "node:net";

import { AGENT_METHODS } from "@agentclientprotocol/sdk";

import {
  applyUpdate,
  attachApproval,
  createPermissionBook,
  emptySnapshot,
  redact,
  settleApproval,
  type AcpUpdate,
  type PermissionRequest,
  type RunSnapshot,
} from "../api/protocol.ts";
import { startAgent, type CodexAgent } from "./agent.ts";

type Thread = {
  id: string;
  title: string;
  archived: boolean;
  updatedAt: string;
  sessionId: string;
  userTexts: string[];
  activeRun?: string;
};

type Active = { threadId: string; runId: string; snapshot: RunSnapshot; write: (snapshot: RunSnapshot) => void };

export type BridgeServer = {
  port: number;
  workspace: string;
  close: () => Promise<void>;
};

function workspaceDir(): string {
  const requested = process.env["ASSISTANT_WORKSPACE"] ?? join(tmpdir(), "conduit-assistant");
  mkdirSync(requested, { recursive: true });
  return realpathSync(requested);
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

async function readJson(request: IncomingMessage): Promise<Record<string, unknown>> {
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

export async function createBridge(options?: { workspace?: string; port?: number }): Promise<BridgeServer> {
  const workspace = options?.workspace ?? workspaceDir();
  const threads = new Map<string, Thread>();
  const active = new Map<string, Active>();
  const book = createPermissionBook();
  let agent: CodexAgent;

  const onUpdate = (sessionId: string, update: AcpUpdate) => {
    const run = active.get(sessionId);
    if (!run) return;
    run.snapshot = applyUpdate(run.snapshot, update);
    run.write(run.snapshot);
  };
  const onPermission = (request: PermissionRequest) => {
    const thread = threads.get(request.threadId);
    if (!thread) return;
    const run = active.get(thread.sessionId);
    if (!run || run.runId !== request.runId) return;
    run.snapshot = attachApproval(run.snapshot, request);
    run.write(run.snapshot);
  };

  agent = await startAgent(workspace, {
    book,
    onUpdate,
    onPermission,
    activeRun(sessionId) {
      const run = active.get(sessionId);
      return run ? { threadId: run.threadId, runId: run.runId } : undefined;
    },
  });

  async function newSession(): Promise<string> {
    const created = await agent.connection.agent.request(AGENT_METHODS.session_new, {
      cwd: workspace,
      mcpServers: [],
    });
    return created.sessionId;
  }

  const server = createServer((request, response) => {
    void handle(request, response).catch((error: unknown) => {
      const message = redact(error instanceof Error ? error.message : "assistant bridge failed");
      if (!response.headersSent) json(response, 500, { error: message });
      else response.end();
    });
  });

  async function handle(request: IncomingMessage, response: ServerResponse): Promise<void> {
    const url = new URL(request.url ?? "/", "http://127.0.0.1");
    const path = url.pathname;
    const method = request.method ?? "GET";

    if (method === "GET" && path === "/api/assistant/health") {
      json(response, 200, {
        ok: true,
        runtime: "codex-acp",
        fixture: agent.fixture,
        workspace,
        mode: agent.mode,
        adapter: "@agentclientprotocol/codex-acp",
      });
      return;
    }

    if (method === "GET" && path === "/api/assistant/threads") {
      json(response, 200, { threads: [...threads.values()].map(threadView) });
      return;
    }

    if (method === "POST" && path === "/api/assistant/threads") {
      const sessionId = await newSession();
      const thread: Thread = {
        id: crypto.randomUUID(),
        title: "New chat",
        archived: false,
        updatedAt: new Date().toISOString(),
        sessionId,
        userTexts: [],
      };
      threads.set(thread.id, thread);
      json(response, 201, threadView(thread));
      return;
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
        if (typeof body["title"] === "string") thread.title = redact(body["title"]).slice(0, 80);
        if (typeof body["archived"] === "boolean") thread.archived = body["archived"];
        thread.updatedAt = new Date().toISOString();
        json(response, 200, threadView(thread));
        return;
      }
      if (method === "DELETE") {
        await agent.connection.agent.request(AGENT_METHODS.session_close, { sessionId: thread.sessionId }).catch(() => undefined);
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

    const cancelMatch = /^\/api\/assistant\/threads\/([^/]+)\/cancel$/.exec(path);
    if (method === "POST" && cancelMatch) {
      const thread = threads.get(decodeURIComponent(cancelMatch[1] ?? ""));
      const body = await readJson(request);
      const runId = typeof body["runId"] === "string" ? body["runId"] : "";
      if (!thread || thread.activeRun !== runId) {
        json(response, 409, { error: "that run is not active" });
        return;
      }
      book.cancelRun(runId);
      await agent.connection.agent.notify(AGENT_METHODS.session_cancel, { sessionId: thread.sessionId });
      json(response, 202, { ok: true });
      return;
    }

    const permissionMatch = /^\/api\/assistant\/permissions\/([^/]+)$/.exec(path);
    if (method === "POST" && permissionMatch) {
      const body = await readJson(request);
      const decision = book.decide({
        requestId: decodeURIComponent(permissionMatch[1] ?? ""),
        threadId: String(body["threadId"] ?? ""),
        runId: String(body["runId"] ?? ""),
        optionId: String(body["optionId"] ?? ""),
      });
      if (!decision.ok) {
        json(response, decision.status, { error: decision.error });
        return;
      }
      const thread = threads.get(String(body["threadId"] ?? ""));
      const run = thread ? active.get(thread.sessionId) : undefined;
      if (run) {
        const option = run.snapshot.tools
          .flatMap((tool) => tool.approval?.options ?? [])
          .find((candidate) => candidate.optionId === String(body["optionId"]));
        const approved = option?.kind === "allow_once" || option?.kind === "allow_always";
        run.snapshot = settleApproval(run.snapshot, decodeURIComponent(permissionMatch[1] ?? ""), {
          optionId: String(body["optionId"]),
          approved,
        });
        run.write(run.snapshot);
      }
      json(response, 200, { ok: true });
      return;
    }

    json(response, 404, { error: "unknown assistant route" });
  }

  async function runTurn(threadId: string, request: IncomingMessage, response: ServerResponse): Promise<void> {
    const thread = threads.get(threadId);
    if (!thread) {
      json(response, 404, { error: "unknown conversation" });
      return;
    }
    if (thread.activeRun) {
      json(response, 409, { error: "a run is already active on this conversation" });
      return;
    }
    const body = await readJson(request);
    const runId = String(body["runId"] ?? "");
    const userTexts = Array.isArray(body["userTexts"]) ? body["userTexts"].filter((entry): entry is string => typeof entry === "string") : [];
    const prompt = userTexts.at(-1) ?? "";
    if (runId.length === 0 || prompt.length === 0) {
      json(response, 400, { error: "runId and a user message are required" });
      return;
    }
    const previous = thread.userTexts;
    const extended =
      userTexts.length === previous.length + 1 && previous.every((text, index) => text === userTexts[index]);
    const same = userTexts.length === previous.length && previous.every((text, index) => text === userTexts[index]);
    if (!extended && !same && previous.length > 0) {
      await agent.connection.agent.request(AGENT_METHODS.session_close, { sessionId: thread.sessionId }).catch(() => undefined);
      thread.sessionId = await newSession();
    }

    thread.activeRun = runId;
    thread.updatedAt = new Date().toISOString();
    response.writeHead(200, {
      "content-type": "text/event-stream; charset=utf-8",
      "cache-control": "no-store",
      connection: "keep-alive",
    });
    const write = (snapshot: RunSnapshot) => {
      if (!response.writableEnded) response.write(`data: ${JSON.stringify({ type: "snapshot", snapshot })}\n\n`);
    };
    const run: Active = { threadId: thread.id, runId, snapshot: emptySnapshot(), write };
    active.set(thread.sessionId, run);
    let cancelled = false;
    request.on("close", () => {
      if (response.writableEnded || cancelled) return;
      cancelled = true;
      book.cancelRun(runId);
      void agent.connection.agent.notify(AGENT_METHODS.session_cancel, { sessionId: thread.sessionId });
    });

    try {
      const blocks: ({ type: "text"; text: string } | { type: "image"; mimeType: string; data: string })[] = [
        { type: "text", text: prompt },
      ];
      const attachments = Array.isArray(body["attachments"]) ? body["attachments"] : [];
      for (const attachment of attachments) {
        if (!attachment || typeof attachment !== "object") continue;
        const record = attachment as Record<string, unknown>;
        const mimeType = typeof record["mimeType"] === "string" ? record["mimeType"] : "application/octet-stream";
        const data = typeof record["data"] === "string" ? record["data"] : "";
        const name = typeof record["name"] === "string" ? record["name"] : "attachment";
        if (mimeType.startsWith("image/") && data.length > 0) blocks.push({ type: "image", mimeType, data });
        else if (data.length > 0) blocks.push({ type: "text", text: `Attachment ${name} (${mimeType})\n${data.slice(0, 100_000)}` });
      }
      const result = (await agent.connection.agent.request(AGENT_METHODS.session_prompt, {
        sessionId: thread.sessionId,
        prompt: blocks,
      })) as { stopReason?: string };
      thread.userTexts = userTexts;
      write(run.snapshot);
      const stopReason = typeof result.stopReason === "string" ? result.stopReason : "end_turn";
      response.write(`data: ${JSON.stringify({ type: "done", stopReason })}\n\n`);
    } catch (error) {
      const message = redact(error instanceof Error ? error.message : "the Codex run failed");
      book.cancelRun(runId);
      if (!response.writableEnded) {
        response.write(`data: ${JSON.stringify({ type: "error", message })}\n\n`);
      }
    } finally {
      active.delete(thread.sessionId);
      if (thread.activeRun === runId) delete thread.activeRun;
      if (!response.writableEnded) response.end();
    }
  }

  await new Promise<void>((resolve) => {
    server.listen(options?.port ?? 0, "127.0.0.1", () => resolve());
  });
  const address = server.address() as AddressInfo;

  return {
    port: address.port,
    workspace,
    async close() {
      agent.close();
      await new Promise<void>((resolve, reject) => {
        server.close((error) => (error ? reject(error) : resolve()));
      });
    },
  };
}
